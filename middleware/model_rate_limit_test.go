package middleware

import (
	"context"
	"fmt"
	"net/http"
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
		require.NoError(t, setting.UpdateUserGroupRateLimitConfig(`{"enabled":true,"groups":{"High Risk":{"duration_seconds":3600,"max_requests":1}}}`))
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
