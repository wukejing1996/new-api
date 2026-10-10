package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestModelRedisRateLimitUsesUTCRegardlessOfLocalTimezone(t *testing.T) {
	redisServer, redisClient := useRateLimitMiniRedis(t)
	previousLocation := time.Local
	time.Local = time.FixedZone("test-utc-plus-eight", 8*60*60)
	t.Cleanup(func() { time.Local = previousLocation })

	ctx := context.Background()
	recordKey := "rateLimit:model-utc-record"
	recordRedisRequest(ctx, redisClient, recordKey, 2)
	recorded, err := redisClient.LIndex(ctx, recordKey, 0).Result()
	require.NoError(t, err)
	recordedAt, err := time.Parse(modelRateLimitTimeFormat, recorded)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().UTC(), recordedAt, 2*time.Second)

	checkKey := "rateLimit:model-utc-check"
	withinWindow := time.Now().UTC().Add(-30 * time.Second).Format(modelRateLimitTimeFormat)
	_, err = redisServer.Push(checkKey, withinWindow, withinWindow)
	require.NoError(t, err)
	allowed, err := checkRedisRateLimit(ctx, redisClient, checkKey, 2, 60)
	require.NoError(t, err)
	assert.False(t, allowed, "an existing UTC timestamp inside the window must remain limited on a non-UTC host")
}

func TestUserGroupRateLimitAdmissionAndPriority(t *testing.T) {
	previousConfig := setting.UserGroupRateLimitConfigJSON()
	previousEnabled := setting.ModelRequestRateLimitEnabled
	previousRedis := common.RedisEnabled
	previousTotal, previousSuccess := setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserGroupRateLimitConfig(previousConfig))
		setting.ModelRequestRateLimitEnabled = previousEnabled
		setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount = previousTotal, previousSuccess
		common.RedisEnabled = previousRedis
	})
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			common.RedisEnabled = false
			if backend == "redis" {
				useRateLimitMiniRedis(t)
			}
			require.NoError(t, setting.UpdateUserGroupRateLimitConfig(`{"enabled":true,"groups":{"High Risk":{"duration_seconds":3600,"max_requests":1}}}`))
			setting.ModelRequestRateLimitEnabled = false
			userID := 7300000 + int(modelRateLimitTestUsers.Add(1))
			entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var reached atomic.Int64
			router := gin.New()
			router.GET("/:token/:outcome", func(c *gin.Context) {
				c.Set("id", userID)
				common.SetContextKey(c, constant.ContextKeyUserGroup, "High Risk")
				common.SetContextKey(c, constant.ContextKeyTokenGroup, c.Param("token"))
			}, ModelRequestRateLimit(), func(c *gin.Context) {
				reached.Add(1)
				if c.Param("outcome") == "pending" {
					close(entered)
					<-release
				}
				c.Status(http.StatusBadGateway)
			})
			go func() {
				defer close(finished)
				assert.Equal(t, http.StatusBadGateway, performRateLimitRequest(router, "/auto/pending", "127.0.0.1:1000").Code)
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				close(release)
				t.Fatal("the first request did not reach the model handler")
			}
			limited := performRateLimitRequest(router, "/default/completed", "127.0.0.1:1000")
			assert.Equal(t, http.StatusTooManyRequests, limited.Code, "a second key is rejected while the first request is still pending")
			assert.Empty(t, limited.Header().Get("Retry-After"), "the response must not expose the policy window")
			assert.JSONEq(t, `{"error":{"message":"Rate limit exceeded","type":"new_api_error","code":""}}`, limited.Body.String())
			assert.EqualValues(t, 1, reached.Load(), "rejection must not reach downstream processing or its usage logging")
			close(release)
			<-finished
			assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/auto/completed", "127.0.0.1:1000").Code, "a failed admitted request still consumes its slot")

			otherID := userID + 1000000
			otherRouter := gin.New()
			otherRouter.GET("/:group", func(c *gin.Context) {
				c.Set("id", otherID)
				group := "High Risk"
				if c.Param("group") == "normal" {
					group = "default"
				}
				common.SetContextKey(c, constant.ContextKeyUserGroup, group)
				common.SetContextKey(c, constant.ContextKeyTokenGroup, "auto")
			}, ModelRequestRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
			assert.Equal(t, http.StatusNoContent, performRateLimitRequest(otherRouter, "/other", "127.0.0.1:1000").Code, "users in the same group must not share a quota")
			assert.Equal(t, http.StatusNoContent, performRateLimitRequest(otherRouter, "/normal", "127.0.0.1:1000").Code)
			assert.Equal(t, http.StatusNoContent, performRateLimitRequest(otherRouter, "/normal", "127.0.0.1:1000").Code, "normal Auto users remain unlimited when the legacy switch is off")
		})
	}

	t.Run("fallback and replacement", func(t *testing.T) {
		common.RedisEnabled = false
		setting.ModelRequestRateLimitEnabled = true
		setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount = 0, 1
		require.NoError(t, setting.UpdateUserGroupRateLimitConfig(`{"enabled":true,"groups":{"High Risk":{"duration_seconds":3600,"max_requests":2}}}`))
		router := gin.New()
		baseID := 7300000 + int(modelRateLimitTestUsers.Add(1))
		router.GET("/:group", func(c *gin.Context) {
			group := c.Param("group")
			id := baseID
			if group == "normal" {
				id += 1000000
			}
			c.Set("id", id)
			common.SetContextKey(c, constant.ContextKeyUserGroup, group)
			common.SetContextKey(c, constant.ContextKeyTokenGroup, "auto")
		}, ModelRequestRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
		assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/High%20Risk", "127.0.0.1:1000").Code)
		assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/High%20Risk", "127.0.0.1:1000").Code, "matched rules replace the stricter legacy success limit")
		assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/High%20Risk", "127.0.0.1:1000").Code)
		assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/normal", "127.0.0.1:1000").Code)
		assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/normal", "127.0.0.1:1000").Code, "unmatched users keep legacy behavior")
		require.NoError(t, setting.UpdateUserGroupRateLimitConfig(`{"enabled":false,"groups":{"High Risk":{"duration_seconds":3600,"max_requests":2}}}`))
		assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/High%20Risk", "127.0.0.1:1000").Code, "disabled user-group rules fall through to the legacy limiter")
		assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/High%20Risk", "127.0.0.1:1000").Code)
		setting.ModelRequestRateLimitEnabled = false
		assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/normal", "127.0.0.1:1000").Code)
	})

	t.Run("Redis unavailable", func(t *testing.T) {
		require.NoError(t, setting.UpdateUserGroupRateLimitConfig(`{"enabled":true,"groups":{"High Risk":{"duration_seconds":3600,"max_requests":1,"custom_response_enabled":true,"custom_response_message":"Contact support"}}}`))
		previousClient := common.RDB
		common.RDB = nil
		common.RedisEnabled = true
		t.Cleanup(func() { common.RDB = previousClient })
		router := gin.New()
		router.GET("/limited", func(c *gin.Context) {
			c.Set("id", 7300000+int(modelRateLimitTestUsers.Add(1)))
			common.SetContextKey(c, constant.ContextKeyUserGroup, "High Risk")
		}, ModelRequestRateLimit(), func(c *gin.Context) { t.Error("a failed limit check must not forward the request") })
		assert.Equal(t, http.StatusInternalServerError, performRateLimitRequest(router, "/limited", "127.0.0.1:1000").Code)
	})
}

func TestUserGroupRateLimitSlidingWindowAndConfiguration(t *testing.T) {
	previousConfig := setting.UserGroupRateLimitConfigJSON()
	t.Cleanup(func() { require.NoError(t, setting.UpdateUserGroupRateLimitConfig(previousConfig)) })
	valid := `{"enabled":true,"groups":{"High Risk":{"duration_seconds":3600,"max_requests":1}}}`
	require.NoError(t, setting.UpdateUserGroupRateLimitConfig(valid))
	for _, raw := range []string{"null", `{"groups":null}`, `{"groups":{"High Risk":{"duration_seconds":0,"max_requests":1}}}`, `{"groups":{"High Risk":{"duration_seconds":2592001,"max_requests":1}}}`, `{"groups":{"High Risk":{"duration_seconds":3600,"max_requests":0}}}`, `{"groups":{"High Risk":{"duration_seconds":3600,"max_requests":10001}}}`, `{"groups":{" High Risk ":{"duration_seconds":3600,"max_requests":1}}}`} {
		assert.Error(t, setting.UpdateUserGroupRateLimitConfig(raw))
		assert.JSONEq(t, valid, setting.UserGroupRateLimitConfigJSON(), "invalid saves leave the previous rule intact")
	}
	server, client := useRateLimitMiniRedis(t)
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	server.SetTime(now)
	key := "rateLimit:userGroupModel:v1:test"
	ctx := context.Background()
	first, err := userGroupRateLimitScript.Run(ctx, client, []string{key}, 3600, 2).Int64Slice()
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 0}, first)
	server.SetTime(now.Add(30 * time.Minute))
	second, err := userGroupRateLimitScript.Run(ctx, client, []string{key}, 3600, 2).Int64Slice()
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 0}, second)
	denied, err := userGroupRateLimitScript.Run(ctx, client, []string{key}, 3600, 2).Int64Slice()
	require.NoError(t, err)
	assert.Equal(t, []int64{0, 1800}, denied)
	server.SetTime(now.Add(time.Hour))
	third, err := userGroupRateLimitScript.Run(ctx, client, []string{key}, 3600, 2).Int64Slice()
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 0}, third, "only the first timestamp expires, rather than resetting the whole window")
	denied, err = userGroupRateLimitScript.Run(ctx, client, []string{key}, 3600, 2).Int64Slice()
	require.NoError(t, err)
	assert.Equal(t, []int64{0, 1800}, denied)
	assert.Equal(t, time.Hour, server.TTL(key), "idle rate limit counters must expire")
}

var modelRateLimitTestUsers atomic.Int64

func TestUserGroupRateLimitCustomResponse(t *testing.T) {
	previousConfig := setting.UserGroupRateLimitConfigJSON()
	previousRedis, previousEnabled := common.RedisEnabled, setting.ModelRequestRateLimitEnabled
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserGroupRateLimitConfig(previousConfig))
		common.RedisEnabled, setting.ModelRequestRateLimitEnabled = previousRedis, previousEnabled
	})
	message := "风险提示 \"retry\"\n请联系支持"
	cases := []struct {
		name, path, textPath, terminal string
		stream                         bool
	}{
		{"chat", "/v1/chat/completions", "choices.0.message.content", "", false},
		{"chat stream", "/v1/chat/completions", "choices.0.delta.content", "[DONE]", true},
		{"completions", "/v1/completions", "choices.0.text", "", false},
		{"completions stream", "/v1/completions", "choices.0.text", "[DONE]", true},
		{"claude", "/v1/messages", "content.0.text", "", false},
		{"claude stream", "/v1/messages", "delta.text", "message_stop", true},
		{"responses", "/v1/responses", "output.0.content.0.text", "", false},
		{"responses stream", "/v1/responses", "delta", "response.completed", true},
		{"gemini", "/v1beta/models/gemini-test:generateContent", "candidates.0.content.parts.0.text", "", false},
		{"gemini SSE", "/v1beta/models/gemini-test:streamGenerateContent?alt=sse", "candidates.0.content.parts.0.text", "STOP", true},
		{"gemini JSON stream", "/v1beta/models/gemini-test:streamGenerateContent", "0.candidates.0.content.parts.0.text", "", false},
		{"non-text", "/v1/embeddings", "message", "", false},
	}
	for _, backend := range []string{"memory", "redis"} {
		for _, tc := range cases {
			t.Run(backend+"/"+tc.name, func(t *testing.T) {
				common.RedisEnabled, setting.ModelRequestRateLimitEnabled = false, false
				var ttl func() time.Duration
				var advance func()
				userID := 7400000 + int(modelRateLimitTestUsers.Add(1))
				if backend == "redis" {
					server, _ := useRateLimitMiniRedis(t)
					now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
					server.SetTime(now)
					advance = func() {
						server.FastForward(time.Minute)
						server.SetTime(now.Add(time.Minute))
					}
					ttl = func() time.Duration { return server.TTL(fmt.Sprintf("rateLimit:userGroupModel:v1:%d", userID)) }
				}
				config := setting.UserGroupRateLimitConfig{Enabled: true, Groups: map[string]setting.UserGroupRateLimitRule{
					"High Risk": {DurationSeconds: 3600, MaxRequests: 1, CustomResponseEnabled: true, CustomResponseMessage: message},
				}}
				encoded, err := common.Marshal(config)
				require.NoError(t, err)
				require.NoError(t, setting.UpdateUserGroupRateLimitConfig(string(encoded)))
				called := 0
				router := gin.New()
				router.POST("/*path", func(c *gin.Context) {
					c.Set("id", userID)
					common.SetContextKey(c, constant.ContextKeyUserGroup, "High Risk")
					common.SetContextKey(c, constant.ContextKeyTokenGroup, "auto")
				}, ModelRequestRateLimit(), func(c *gin.Context) { called++; c.String(http.StatusOK, "upstream") })
				body := fmt.Sprintf(`{"model":"test-model","stream":%t,"stream_options":{"include_usage":true}}`, tc.stream)
				request := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				first := httptest.NewRecorder()
				router.ServeHTTP(first, request)
				require.Equal(t, "upstream", first.Body.String(), "enabled custom replies must never intercept requests within the limit")
				if advance != nil {
					advance()
				}
				for range 2 {
					request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(body))
					request.Header.Set("Content-Type", "application/json")
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)
					require.Equal(t, http.StatusOK, response.Code)
					assert.Empty(t, response.Header().Get("Retry-After"))
					if !tc.stream {
						assert.Equal(t, message, gjson.Get(response.Body.String(), tc.textPath).String())
					} else {
						assert.Contains(t, response.Header().Get("Content-Type"), "text/event-stream")
						var text strings.Builder
						for line := range strings.SplitSeq(response.Body.String(), "\n") {
							data, ok := strings.CutPrefix(line, "data: ")
							if !ok || data == "[DONE]" {
								continue
							}
							assert.True(t, gjson.Valid(data), "SSE data must be valid JSON")
							text.WriteString(gjson.Get(data, tc.textPath).String())
						}
						assert.Equal(t, message, text.String())
						assert.Contains(t, response.Body.String(), tc.terminal, "clients must receive a terminal event")
					}
				}
				assert.Equal(t, 1, called, "local replies never enter upstream or usage logging handlers")
				if ttl != nil {
					assert.Equal(t, time.Hour-time.Minute, ttl(), "local replies must not refresh the rate limit expiry")
				}
				config.Groups["High Risk"] = setting.UserGroupRateLimitRule{DurationSeconds: 3600, MaxRequests: 1}
				encoded, err = common.Marshal(config)
				require.NoError(t, err)
				require.NoError(t, setting.UpdateUserGroupRateLimitConfig(string(encoded)))
				request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				denied := httptest.NewRecorder()
				router.ServeHTTP(denied, request)
				assert.Equal(t, http.StatusTooManyRequests, denied.Code, "disabling custom replies immediately restores 429 without resetting the quota")
				assert.JSONEq(t, `{"error":{"message":"Rate limit exceeded","type":"new_api_error","code":""}}`, denied.Body.String())
				config.Groups["High Risk"] = setting.UserGroupRateLimitRule{DurationSeconds: 3600, MaxRequests: 2}
				encoded, err = common.Marshal(config)
				require.NoError(t, err)
				require.NoError(t, setting.UpdateUserGroupRateLimitConfig(string(encoded)))
				request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(body))
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				assert.Equal(t, "upstream", response.Body.String(), "local replies and 429 rejections must not consume quota slots")
			})
		}
	}
}

func TestUserGroupRateLimitCustomResponseValidation(t *testing.T) {
	previous := setting.UserGroupRateLimitConfigJSON()
	t.Cleanup(func() { require.NoError(t, setting.UpdateUserGroupRateLimitConfig(previous)) })
	for _, message := range []string{"", " \n\t", strings.Repeat("字", 4001)} {
		raw, err := common.Marshal(setting.UserGroupRateLimitConfig{Groups: map[string]setting.UserGroupRateLimitRule{
			"High Risk": {DurationSeconds: 3600, MaxRequests: 1, CustomResponseEnabled: true, CustomResponseMessage: message},
		}})
		require.NoError(t, err)
		assert.Error(t, setting.UpdateUserGroupRateLimitConfig(string(raw)))
		assert.JSONEq(t, previous, setting.UserGroupRateLimitConfigJSON())
	}
	_, err := setting.ParseUserGroupRateLimitConfig(`{"enabled":true,"groups":{"High Risk":{"duration_seconds":3600,"max_requests":1,"custom_response_message":"saved draft"}}}`)
	assert.NoError(t, err, "disabled replies can retain saved text")
}

func TestModelRateLimitStreamFailuresDoNotConsumeSuccessLimit(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		for _, totalLimit := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/total=%d", backend, totalLimit), func(t *testing.T) {
				userID := 7200000 + int(modelRateLimitTestUsers.Add(1))
				handler := memoryRateLimitHandler(60, totalLimit, 1)
				if backend == "redis" {
					useRateLimitMiniRedis(t)
					handler = redisRateLimitHandler(60, totalLimit, 1)
				}
				router := gin.New()
				router.GET("/:outcome", func(c *gin.Context) { c.Set("id", userID) }, handler, func(c *gin.Context) {
					status := relaycommon.NewStreamStatus()
					if c.Param("outcome") == "failed" {
						status.MarkFailed("server_error", "", 0)
					} else {
						status.MarkCompleted()
					}
					common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, status)
					c.Status(http.StatusOK)
				})
				assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/failed", "127.0.0.1:1000").Code)
				if totalLimit > 0 {
					assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/failed", "127.0.0.1:1000").Code)
				} else {
					assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
				}
				assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
			})
		}
	}
}

func TestModelMemoryRateLimitReservesConcurrentSuccessAdmission(t *testing.T) {
	userID := 7200000 + int(modelRateLimitTestUsers.Add(1))
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	router := gin.New()
	router.GET("/:outcome", func(c *gin.Context) { c.Set("id", userID) }, memoryRateLimitHandler(60, 0, 1), func(c *gin.Context) {
		if c.Param("outcome") == "failed" {
			close(entered)
			<-release
			status := relaycommon.NewStreamStatus()
			status.MarkFailed("server_error", "", 0)
			common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, status)
		}
		c.Status(http.StatusOK)
	})
	go func() {
		defer close(finished)
		assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/failed", "127.0.0.1:1000").Code)
	}()
	<-entered
	assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
	close(release)
	<-finished
	assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
	assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
}
