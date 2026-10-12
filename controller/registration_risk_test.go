package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"
)

type registrationRiskTransport func(*http.Request) (*http.Response, error)

func TestRegistrationRiskRecordSnapshot(t *testing.T) {
	_, cfg := setupRegistrationRisk(t, false)
	group := cfg.TargetGroup
	for _, tc := range []struct {
		outcome                           string
		created, applied, synced, success bool
	}{
		{"allowed", true, true, true, true},
		{"restricted", true, true, true, true},
		{"restricted_incomplete", true, true, false, true},
		{"registration_failed", false, false, false, false},
		{"processing_failed", true, true, false, false},
	} {
		t.Run(tc.outcome, func(t *testing.T) {
			record := service.RegistrationRiskRecord{Source: "registration", Policy: cfg,
				Assessment: service.RegistrationRiskAssessment{Restricted: true, Reasons: []string{"proxy_ip"}},
				Outcome:    tc.outcome, AccountCreated: tc.created, GroupApplied: tc.applied, CacheSynced: tc.synced,
				OriginalGroup: "default", FinalGroup: &group, RelatedUserIDs: []int{7, 8}}
			entry := service.NewRegistrationRiskLog(&model.User{Id: 8, Username: "new-account", Password: "must-not-be-recorded", AccessToken: nil}, record)
			assert.Equal(t, tc.success, entry.Success)
			if !tc.created {
				assert.Zero(t, entry.UserId)
			}
			body, err := common.Marshal(entry)
			require.NoError(t, err)
			var loaded model.RegistrationRiskLog
			require.NoError(t, common.Unmarshal(body, &loaded))
			assert.Equal(t, tc.outcome, loaded.Details.Outcome)
			assert.Equal(t, tc.applied, loaded.Details.GroupApplied)
			assert.Equal(t, tc.synced, loaded.Details.CacheSynced)
			assert.Contains(t, string(body), `"risk_threshold":50`)
			assert.NotContains(t, string(body), "test-secret-key")
			assert.NotContains(t, string(body), "must-not-be-recorded")
			assert.NotContains(t, string(body), `"other"`)
			encoded, err := entry.Details.Value()
			require.NoError(t, err)
			for _, input := range []any{encoded, []byte(encoded.(string))} {
				var stored model.RegistrationRiskRecord
				require.NoError(t, stored.Scan(input))
				assert.Equal(t, record, stored)
			}
		})
	}
	entry := service.NewRegistrationRiskLog(&model.User{Id: 7, Username: "earlier"}, service.RegistrationRiskRecord{
		Source: "association", AccountCreated: true, TriggerUserID: 8, Outcome: "restricted",
		Assessment: service.RegistrationRiskAssessment{Restricted: true, Reasons: []string{"ip_association"}},
	})
	body, err := common.Marshal(entry)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"source":"association"`)
	assert.Contains(t, string(body), `"trigger_user_id":8`)
	assert.NotContains(t, string(body), "ip_result")
	var details model.RegistrationRiskRecord
	require.NoError(t, details.Scan(nil))
	assert.Equal(t, model.RegistrationRiskRecord{}, details)
	assert.Error(t, details.Scan(123))
	assert.Error(t, details.Scan("{invalid"))
	parsed, err := schema.Parse(&model.RegistrationRiskLog{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	assert.Equal(t, "registration_risk_logs", parsed.Table)
	assert.Equal(t, schema.DataType("text"), parsed.LookUpField("Details").DataType)
}

func TestRegistrationRiskRecordFilters(t *testing.T) {
	for _, query := range []string{"?user_id=-1", "?user_id=abc", "?success=maybe", "?start_timestamp=x", "?start_timestamp=100&end_timestamp=10", "?username=" + strings.Repeat("a", 65)} {
		c, r := registrationRiskContext("8.8.4.4")
		c.Request = httptest.NewRequest(http.MethodGet, "/api/option/registration_risk/records"+query, nil)
		c.Set("role", common.RoleRootUser)
		GetRegistrationRiskRecords(c)
		assert.Contains(t, r.Body.String(), `"success":false`)
		assert.Contains(t, r.Body.String(), "Invalid audit")
	}
	for _, role := range []int{0, common.RoleCommonUser, common.RoleAdminUser} {
		c, r := registrationRiskContext("8.8.4.4")
		c.Set("role", role)
		GetRegistrationRiskRecords(c)
		assert.Equal(t, http.StatusForbidden, r.Code)
	}
}

func (f registrationRiskTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func setupRegistrationRisk(t *testing.T, withDatabase bool) (*miniredis.Miniredis, setting.RegistrationRiskConfig) {
	t.Helper()
	if withDatabase {
		if os.Getenv("TEST_REGISTRATION_RISK_DB") != "1" {
			t.Skip("database integration verification is opt-in")
		}
		setupManageUserTestDB(t)
		require.NoError(t, model.DB.AutoMigrate(&model.Option{}, &model.RegistrationRiskLog{}))
	}
	previousConfig, previousKey := setting.GetRegistrationRiskConfig()
	previousRatios := ratio_setting.GroupRatio2JSONString()
	previousLimits := setting.UserGroupRateLimitConfigJSON()
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	previousOptions := common.OptionMap
	previousTransport := http.DefaultTransport
	common.OptionMap = map[string]string{}
	server := miniredis.RunT(t)
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	common.RedisEnabled = true
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
		common.OptionMap = previousOptions
		http.DefaultTransport = previousTransport
		raw, _ := common.Marshal(previousConfig)
		require.NoError(t, setting.UpdateRegistrationRiskConfig(string(raw)))
		require.NoError(t, setting.UpdateRegistrationRiskAPIKey(previousKey))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousRatios))
		require.NoError(t, setting.UpdateUserGroupRateLimitConfig(previousLimits))
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"High Risk":1}`))
	require.NoError(t, setting.UpdateUserGroupRateLimitConfig(`{"enabled":true,"groups":{"High Risk":{"duration_seconds":3600,"max_requests":0}}}`))
	cfg := setting.DefaultRegistrationRiskConfig()
	cfg.Enabled, cfg.TargetGroup = true, "High Risk"
	require.NoError(t, setting.UpdateRegistrationRiskAPIKey("test-secret-key"))
	setRegistrationRiskConfig(t, cfg)
	return server, cfg
}

func setRegistrationRiskConfig(t *testing.T, cfg setting.RegistrationRiskConfig) {
	t.Helper()
	raw, err := common.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, setting.UpdateRegistrationRiskConfig(string(raw)))
}

func registrationRiskContext(ip string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	r := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(r)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/register", nil)
	c.Request.RemoteAddr = ip + ":12345"
	return c, r
}

const safeRegistrationIP = `{"network":{"type":"Residential"},"detections":{"proxy":false,"vpn":false,"tor":false,"hosting":false,"scraper":false,"risk":0,"confidence":100}}`

func mockRegistrationIntelligence(t *testing.T, ipResult, emailResult string, calls *atomic.Int32) {
	t.Helper()
	http.DefaultTransport = registrationRiskTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		body := ipResult
		address := strings.TrimPrefix(r.URL.Path, "/v3/")
		if strings.Contains(address, "@") {
			body = emailResult
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"status":"ok",%q:%s}`, address, body))), Header: http.Header{}, Request: r}, nil
	})
}

func TestRegistrationRiskIntelligence(t *testing.T) {
	t.Run("v3 email detections distinguish safe disposable and unknown results", func(t *testing.T) {
		ordinary, disposable := false, true
		for _, tc := range []struct {
			name, emailResult string
			disposable        *bool
		}{
			{"ordinary mailbox", `{"detections":{"disposable":false}}`, &ordinary},
			{"disposable mailbox", `{"detections":{"disposable":true}}`, &disposable},
			{"missing field", `{"detections":{}}`, nil},
			{"null field", `{"detections":{"disposable":null}}`, nil},
			{"invalid type", `{"detections":{"disposable":"false"}}`, nil},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, cfg := setupRegistrationRisk(t, false)
				var calls atomic.Int32
				mockRegistrationIntelligence(t, safeRegistrationIP, tc.emailResult, &calls)
				result := service.EvaluateRegistrationRisk(context.Background(), "8.8.4.4", "mailbox@163.com", cfg, "test-secret-key")
				assert.Equal(t, tc.disposable, result.Disposable)
				assert.Equal(t, tc.disposable == nil, result.Incomplete)
				if tc.disposable == nil {
					assert.True(t, result.Restricted)
					assert.Equal(t, []string{"email_assessment_unavailable"}, result.Reasons)
				} else {
					assert.Equal(t, *tc.disposable, result.Restricted)
					if *tc.disposable {
						assert.Equal(t, []string{"disposable_email"}, result.Reasons)
					} else {
						assert.Empty(t, result.Reasons)
					}
				}
			})
		}
	})
	t.Run("unavailable intelligence never becomes a clean assessment", func(t *testing.T) {
		_, cfg := setupRegistrationRisk(t, false)
		cfg.DisposableEmail = false
		cfg.CacheSeconds = 0
		for _, tc := range []struct {
			name   string
			status int
			body   string
		}{
			{"quota exhausted", 429, `{}`},
			{"invalid credentials", 403, `{}`},
			{"missing entry", 200, `{"status":"ok"}`},
			{"malformed data", 200, `not-json`},
			{"partial fields", 200, `{"status":"ok","8.8.4.4":{}}`},
		} {
			t.Run(tc.name, func(t *testing.T) {
				http.DefaultTransport = registrationRiskTransport(func(r *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{}, Request: r}, nil
				})
				result := service.EvaluateRegistrationRisk(context.Background(), "8.8.4.4", "", cfg, "key")
				assert.True(t, result.Restricted)
				assert.True(t, result.Incomplete)
			})
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		http.DefaultTransport = registrationRiskTransport(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })
		result := service.EvaluateRegistrationRisk(ctx, "8.8.4.4", "", cfg, "key")
		assert.True(t, result.Incomplete)
		assert.True(t, result.Restricted)
	})
	for _, tc := range []struct {
		name, ip, email, reason string
		restricted, incomplete  bool
	}{
		{"safe residential", safeRegistrationIP, `{"detections":{"disposable":false}}`, "", false, false},
		{"proxy", strings.Replace(safeRegistrationIP, `"proxy":false`, `"proxy":true`, 1), `{"detections":{"disposable":false}}`, "proxy_ip", true, false},
		{"vpn", strings.Replace(safeRegistrationIP, `"vpn":false`, `"vpn":true`, 1), `{"detections":{"disposable":false}}`, "vpn_ip", true, false},
		{"tor", strings.Replace(safeRegistrationIP, `"tor":false`, `"tor":true`, 1), `{"detections":{"disposable":false}}`, "tor_ip", true, false},
		{"hosting network", strings.Replace(safeRegistrationIP, "Residential", "Hosting", 1), `{"detections":{"disposable":false}}`, "hosting_network", true, false},
		{"hosting flag", strings.Replace(safeRegistrationIP, `"hosting":false`, `"hosting":true`, 1), `{"detections":{"disposable":false}}`, "hosting_ip", true, false},
		{"scraper", strings.Replace(safeRegistrationIP, `"scraper":false`, `"scraper":true`, 1), `{"detections":{"disposable":false}}`, "scraper_ip", true, false},
		{"score boundary", strings.Replace(safeRegistrationIP, `"risk":0`, `"risk":50`, 1), `{"detections":{"disposable":false}}`, "ip_risk_score", true, false},
		{"disposable", safeRegistrationIP, `{"detections":{"disposable":true}}`, "disposable_email", true, false},
		{"missing ip fields", `{}`, `{"detections":{"disposable":false}}`, "proxy_ip_unknown", true, true},
		{"missing email result", safeRegistrationIP, `{}`, "email_assessment_unavailable", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, cfg := setupRegistrationRisk(t, false)
			var calls atomic.Int32
			mockRegistrationIntelligence(t, tc.ip, tc.email, &calls)
			result := service.EvaluateRegistrationRisk(context.Background(), "8.8.4.4", "user@example.com", cfg, "test-secret-key")
			assert.Equal(t, tc.restricted, result.Restricted)
			assert.Equal(t, tc.incomplete, result.Incomplete)
			if tc.reason != "" {
				assert.Contains(t, result.Reasons, tc.reason)
			}
		})
	}
	t.Run("cache avoids repeated queries and isolates api keys", func(t *testing.T) {
		server, cfg := setupRegistrationRisk(t, false)
		var calls atomic.Int32
		mockRegistrationIntelligence(t, safeRegistrationIP, `{"detections":{"disposable":false}}`, &calls)
		for range 2 {
			assert.False(t, service.EvaluateRegistrationRisk(context.Background(), "8.8.4.4", "user@example.com", cfg, "test-secret-key").Restricted)
		}
		assert.EqualValues(t, 2, calls.Load())
		for _, key := range server.Keys() {
			assert.NotContains(t, key, "test-secret-key")
			assert.NotContains(t, key, "user@example.com")
		}
		service.EvaluateRegistrationRisk(context.Background(), "8.8.4.4", "user@example.com", cfg, "new-key")
		assert.EqualValues(t, 4, calls.Load())
	})
	t.Run("provider failure is strict by default and configurable", func(t *testing.T) {
		_, cfg := setupRegistrationRisk(t, false)
		http.DefaultTransport = registrationRiskTransport(func(r *http.Request) (*http.Response, error) { return nil, errors.New("secret upstream failure") })
		result := service.EvaluateRegistrationRisk(context.Background(), "8.8.4.4", "user@example.com", cfg, "key")
		assert.True(t, result.Restricted)
		assert.True(t, result.Incomplete)
		assert.NotContains(t, fmt.Sprint(result), "secret upstream failure")
		cfg.FailClosed = false
		assert.False(t, service.EvaluateRegistrationRisk(context.Background(), "8.8.4.4", "user@example.com", cfg, "key").Restricted)
	})
}

func TestRegistrationRiskAssociations(t *testing.T) {
	for _, subnet := range []bool{false, true} {
		t.Run(fmt.Sprintf("related accounts subnet=%t", subnet), func(t *testing.T) {
			server, cfg := setupRegistrationRisk(t, true)
			cfg.IPRule.Enabled, cfg.SubnetRule.Enabled = !subnet, subnet
			cfg.SubnetRule.TriggerCount = 2
			setRegistrationRiskConfig(t, cfg)
			var calls atomic.Int32
			mockRegistrationIntelligence(t, safeRegistrationIP, `{"detections":{"disposable":false}}`, &calls)
			create := func(username, ip string) *model.User {
				user := &model.User{Username: username, Email: username + "@example.com", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, AffCode: username}
				c, _ := registrationRiskContext(ip)
				require.NoError(t, service.CreateUserWithRegistrationRisk(c, user, func() error { return model.DB.Create(user).Error }))
				return user
			}
			first := create("first", "8.8.4.4")
			assert.Equal(t, "default", first.Group)
			_, err := model.GetUserCache(first.Id)
			require.NoError(t, err)
			require.NoError(t, common.RDB.HSet(context.Background(), fmt.Sprintf("user:%d", first.Id), "Quota", 37).Err())
			session := &model.UserSession{SID: "related-session", UserID: first.Id, Version: 1, UserAuthVersion: 1, Status: model.UserSessionStatusActive, RefreshHash: "hash", ExpiresAt: time.Now().Unix() + 3600}
			require.NoError(t, model.CreateUserSession(session))
			secondIP := "8.8.4.4"
			if subnet {
				secondIP = "8.8.4.5"
			}
			second := create("second", secondIP)
			for _, user := range []*model.User{first, second} {
				cached, err := model.GetUserCache(user.Id)
				require.NoError(t, err)
				assert.Equal(t, "High Risk", cached.Group)
				assert.EqualValues(t, 1, cached.AuthVersion)
			}
			cached, err := model.GetUserCache(first.Id)
			require.NoError(t, err)
			assert.Equal(t, 37, cached.Quota)
			_, _, err = service.ValidateLoginSession(service.AuthIdentity{UserID: first.Id, SessionID: session.SID, SessionVersion: 1, UserAuthVersion: 1})
			require.NoError(t, err, "association changes must not revoke sessions")
			other := create("unrelated", "9.9.9.9")
			assert.Equal(t, "default", other.Group)
			server.SetTime(time.Now().Add(2 * time.Hour))
			server.FastForward(2 * time.Hour)
			third := create("third", "8.8.4.4")
			assert.Equal(t, "default", third.Group)
			cached, err = model.GetUserCache(first.Id)
			require.NoError(t, err)
			assert.Equal(t, "High Risk", cached.Group, "expiry never releases old risk accounts")
		})
	}
	t.Run("failed creation does not count", func(t *testing.T) {
		_, cfg := setupRegistrationRisk(t, true)
		cfg.DisposableEmail = false
		setRegistrationRiskConfig(t, cfg)
		var calls atomic.Int32
		mockRegistrationIntelligence(t, safeRegistrationIP, `{}`, &calls)
		c, _ := registrationRiskContext("8.8.4.4")
		err := service.CreateUserWithRegistrationRisk(c, &model.User{}, func() error { return errors.New("creation failed") })
		require.Error(t, err)
		user := &model.User{Username: "successful", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1, AffCode: "success"}
		require.NoError(t, service.CreateUserWithRegistrationRisk(c, user, func() error { return model.DB.Create(user).Error }))
		assert.Equal(t, "default", user.Group)
	})
	t.Run("parallel successful registrations both end restricted", func(t *testing.T) {
		_, cfg := setupRegistrationRisk(t, true)
		cfg.DisposableEmail = false
		setRegistrationRiskConfig(t, cfg)
		var calls atomic.Int32
		mockRegistrationIntelligence(t, safeRegistrationIP, `{}`, &calls)
		var wg sync.WaitGroup
		users := []*model.User{{Username: "parallel-a", Role: common.RoleCommonUser, AuthVersion: 1, AffCode: "a"}, {Username: "parallel-b", Role: common.RoleCommonUser, AuthVersion: 1, AffCode: "b"}}
		// Keep SQLite writes deterministic; the finalization paths start together.
		var createMutex sync.Mutex
		errs := make([]error, len(users))
		for i, user := range users {
			wg.Go(func() {
				c, _ := registrationRiskContext("8.8.4.4")
				errs[i] = service.CreateUserWithRegistrationRisk(c, user, func() error { createMutex.Lock(); defer createMutex.Unlock(); return model.DB.Create(user).Error })
			})
		}
		wg.Wait()
		for i, user := range users {
			require.NoError(t, errs[i])
			cached, err := model.GetUserCache(user.Id)
			require.NoError(t, err)
			assert.Equal(t, "High Risk", cached.Group)
		}
	})
}

func TestRegistrationRiskRedisAssociations(t *testing.T) {
	for _, tc := range []struct {
		name, firstIP, nextIP string
		subnet                bool
	}{
		{"same ip", "8.8.4.4", "8.8.4.4", false},
		{"ipv4 subnet", "8.8.4.4", "8.8.4.5", true},
		{"ipv6 subnet", "2001:4860:4860::8888", "2001:4860:4860::8844", true},
		{"mapped ipv4", "::ffff:8.8.4.4", "8.8.4.4", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, cfg := setupRegistrationRisk(t, false)
			cfg.IPRule.Enabled, cfg.SubnetRule.Enabled = !tc.subnet, tc.subnet
			cfg.SubnetRule.TriggerCount = 2
			ctx := context.Background()
			ids, _, err := service.RegisterRegistrationAssociations(ctx, 1, tc.firstIP, cfg)
			require.NoError(t, err)
			assert.Empty(t, ids)
			ids, _, err = service.RegisterRegistrationAssociations(ctx, 1, tc.firstIP, cfg)
			require.NoError(t, err)
			assert.Empty(t, ids, "the same account cannot count twice")
			ids, reasons, err := service.RegisterRegistrationAssociations(ctx, 2, tc.nextIP, cfg)
			require.NoError(t, err)
			assert.ElementsMatch(t, []int{1, 2}, ids)
			assert.Len(t, reasons, 1)
			ids, _, err = service.RegisterRegistrationAssociations(ctx, 3, tc.nextIP, cfg)
			require.NoError(t, err)
			assert.ElementsMatch(t, []int{1, 2, 3}, ids, "all related accounts must be returned")
			ids, _, err = service.RegisterRegistrationAssociations(ctx, 4, "9.9.9.9", cfg)
			require.NoError(t, err)
			assert.Empty(t, ids)
			server.SetTime(time.Now().Add(2 * time.Hour))
			server.FastForward(2 * time.Hour)
			ids, _, err = service.RegisterRegistrationAssociations(ctx, 5, tc.firstIP, cfg)
			require.NoError(t, err)
			assert.Empty(t, ids, "expired accounts do not count toward the new window")
		})
	}
	t.Run("concurrent distinct accounts count atomically", func(t *testing.T) {
		_, cfg := setupRegistrationRisk(t, false)
		results := make([][]int, 2)
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i := range 2 {
			wg.Go(func() {
				results[i], _, errs[i] = service.RegisterRegistrationAssociations(context.Background(), i+1, "8.8.4.4", cfg)
			})
		}
		wg.Wait()
		require.NoError(t, errs[0])
		require.NoError(t, errs[1])
		if len(results[0]) == 0 {
			assert.ElementsMatch(t, []int{1, 2}, results[1])
		} else {
			assert.Empty(t, results[1])
			assert.ElementsMatch(t, []int{1, 2}, results[0])
		}
	})
	t.Run("disabled wrapper bypasses intelligence and redis", func(t *testing.T) {
		server, cfg := setupRegistrationRisk(t, false)
		cfg.Enabled = false
		setRegistrationRiskConfig(t, cfg)
		var calls atomic.Int32
		mockRegistrationIntelligence(t, safeRegistrationIP, `{}`, &calls)
		created := false
		c, _ := registrationRiskContext("8.8.4.4")
		user := &model.User{Group: "default"}
		require.NoError(t, service.CreateUserWithRegistrationRisk(c, user, func() error { created = true; return nil }))
		assert.True(t, created)
		assert.Equal(t, "default", user.Group)
		assert.Zero(t, calls.Load())
		assert.Empty(t, server.Keys())
	})
}

func TestRegistrationRiskSettings(t *testing.T) {
	_, cfg := setupRegistrationRisk(t, true)
	request := func(config setting.RegistrationRiskConfig, key *string) *httptest.ResponseRecorder {
		body, err := common.Marshal(map[string]any{"config": config, "api_key": key})
		require.NoError(t, err)
		c, r := registrationRiskContext("8.8.4.4")
		c.Request.Body = io.NopCloser(strings.NewReader(string(body)))
		c.Set("role", common.RoleRootUser)
		UpdateRegistrationRiskSettings(c)
		return r
	}
	cfg.RiskThreshold = 60
	r := request(cfg, nil)
	require.Equal(t, 200, r.Code)
	assert.Contains(t, r.Body.String(), `"api_key_configured":true`)
	assert.NotContains(t, r.Body.String(), "test-secret-key")
	loaded, key := setting.GetRegistrationRiskConfig()
	assert.Equal(t, 60, loaded.RiskThreshold)
	assert.Equal(t, "test-secret-key", key)
	cfg.TargetGroup = "missing"
	assert.Equal(t, 400, request(cfg, nil).Code)
	cfg.TargetGroup = "High Risk"
	cfg.IPRule.TriggerCount = 1
	assert.Equal(t, 400, request(cfg, nil).Code)
	cfg.IPRule.TriggerCount = 2
	require.NoError(t, setting.UpdateUserGroupRateLimitConfig(`{"enabled":false,"groups":{}}`))
	assert.Equal(t, 400, request(cfg, nil).Code)
}

func TestRegistrationRiskRegistrationPaths(t *testing.T) {
	_, cfg := setupRegistrationRisk(t, true)
	previousRegister, previousPassword, previousEmail, previousDefaultToken := common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled, constant.GenerateDefaultToken
	common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled, constant.GenerateDefaultToken = true, true, false, false
	t.Cleanup(func() {
		common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled, constant.GenerateDefaultToken = previousRegister, previousPassword, previousEmail, previousDefaultToken
	})
	var calls atomic.Int32
	mockRegistrationIntelligence(t, strings.Replace(safeRegistrationIP, `"proxy":false`, `"proxy":true`, 1), `{"detections":{"disposable":false}}`, &calls)
	c, r := registrationRiskContext("8.8.4.4")
	c.Request.Body = io.NopCloser(strings.NewReader(`{"username":"risk-password","password":"Password123!"}`))
	Register(c)
	require.Contains(t, r.Body.String(), `"success":true`)
	var passwordUser model.User
	require.NoError(t, model.DB.Where("username = ?", "risk-password").First(&passwordUser).Error)
	assert.Equal(t, "High Risk", passwordUser.Group)
	c, _ = registrationRiskContext("8.8.4.4")
	user, _, err := findOrCreateOAuthUser(c, &authFlowTestOAuthProvider{}, &oauth.OAuthUser{ProviderUserID: "oauth-id", Username: "risk-oauth", Email: "oauth@example.com"}, &oauth.OAuthToken{}, "")
	require.NoError(t, err)
	assert.Equal(t, "High Risk", user.Group)
	cfg.Enabled = false
	setRegistrationRiskConfig(t, cfg)
	before := calls.Load()
	user, _, err = findOrCreateOAuthUser(c, &authFlowTestOAuthProvider{}, &oauth.OAuthUser{ProviderUserID: "normal-id", Username: "normal-oauth", Email: "normal@example.com"}, &oauth.OAuthToken{}, "")
	require.NoError(t, err)
	assert.Equal(t, before, calls.Load())
	assert.NotEqual(t, "High Risk", user.Group)
	common.RegisterEnabled = false
	_, _, err = findOrCreateOAuthUser(c, &authFlowTestOAuthProvider{}, &oauth.OAuthUser{ProviderUserID: "blocked", Username: "blocked"}, &oauth.OAuthToken{}, "")
	require.ErrorAs(t, err, new(*OAuthRegistrationDisabledError))
	assert.Equal(t, before, calls.Load())
}
