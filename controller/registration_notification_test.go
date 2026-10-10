package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func registrationNotificationRequest(t *testing.T, userID int, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/registration-notification/test", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.RemoteAddr = "198.51.100.42:12345"
	c.Set("id", userID)
	handler(c)
	return recorder
}

func setupRegistrationNotificationTransport(t *testing.T) {
	t.Helper()
	fetch := system_setting.GetFetchSetting()
	previousFetch := *fetch
	fetch.EnableSSRFProtection = false // Only loopback HTTP servers are used by these tests.
	service.InitHttpClient()
	t.Cleanup(func() { *fetch = previousFetch })
}

func TestRegistrationNotificationSettingsAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name    string
		role    int
		initial bool
		patch   string
		want    bool
		status  int
	}{
		{"admin enable", common.RoleAdminUser, false, `,"new_user_registration_notify_enabled":true`, true, 200},
		{"root disable", common.RoleRootUser, true, `,"new_user_registration_notify_enabled":false`, false, 200},
		{"old client preserves subscription", common.RoleAdminUser, true, "", true, 200},
		{"ordinary user cannot subscribe", common.RoleCommonUser, false, `,"new_user_registration_notify_enabled":true`, false, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupManageUserTestDB(t)
			user := model.User{Username: "notification-owner", AffCode: "notification-owner-aff", Role: tc.role, Status: common.UserStatusEnabled}
			user.SetSetting(dto.UserSetting{NewUserRegistrationNotifyEnabled: tc.initial})
			require.NoError(t, db.Create(&user).Error)
			body := `{"notify_type":"email","quota_warning_threshold":1000` + tc.patch + `}`
			response := registrationNotificationRequest(t, user.Id, body, UpdateUserSetting)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			if tc.status == 200 {
				require.Contains(t, response.Body.String(), `"success":true`)
			}
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, tc.want, user.GetSetting().NewUserRegistrationNotifyEnabled)
		})
	}
}

func TestRegistrationNotificationUsesSavedAdminSettings(t *testing.T) {
	db := setupManageUserTestDB(t)
	setupRegistrationNotificationTransport(t)
	var requests atomic.Int32
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		assert.Contains(t, r.URL.Path, "Registration notification test")
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(server.Close)
	user := model.User{Username: "saved-admin", AffCode: "saved-admin-aff", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
	user.SetSetting(dto.UserSetting{NotifyType: dto.NotifyTypeBark, BarkUrl: server.URL + "/secret-device-key/{{title}}/{{content}}"})
	require.NoError(t, db.Create(&user).Error)
	response := registrationNotificationRequest(t, user.Id, `{"bark_url":"https://untrusted.invalid/ignored"}`, TestRegistrationNotification)
	assert.Contains(t, response.Body.String(), `"success":true`)
	assert.EqualValues(t, 1, requests.Load())
	fail.Store(true)
	response = registrationNotificationRequest(t, user.Id, "", TestRegistrationNotification)
	assert.Contains(t, response.Body.String(), `"success":false`)
	assert.NotContains(t, response.Body.String(), "secret-device-key")
	assert.NotContains(t, response.Body.String(), server.URL)
	for _, patch := range []map[string]any{{"role": common.RoleCommonUser}, {"role": common.RoleAdminUser, "status": common.UserStatusDisabled}} {
		require.NoError(t, db.Model(&user).Updates(patch).Error)
		response = registrationNotificationRequest(t, user.Id, "", TestRegistrationNotification)
		assert.Equal(t, http.StatusForbidden, response.Code)
	}
	assert.EqualValues(t, 2, requests.Load())
}

func TestNewUserRegistrationNotifications(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.UserOAuthBinding{}))
	setupRegistrationNotificationTransport(t)
	previousRegister, previousPassword, previousVerification := common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled
	previousDefaultToken, previousLimit := constant.GenerateDefaultToken, constant.NotifyLimitCount
	common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled = true, true, false
	constant.GenerateDefaultToken, constant.NotifyLimitCount = false, 0
	t.Cleanup(func() {
		common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled = previousRegister, previousPassword, previousVerification
		constant.GenerateDefaultToken, constant.NotifyLimitCount = previousDefaultToken, previousLimit
	})
	// Block the first external response until the registration handler has returned.
	release := make(chan struct{})
	defer close(release)
	messages := make(chan service.WebhookPayload, 16)
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload service.WebhookPayload
		if err := common.DecodeJson(r.Body, &payload); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		n := requestCount.Add(1)
		messages <- payload
		if n == 1 {
			<-release
			w.WriteHeader(http.StatusServiceUnavailable) // Exercise the single retry.
		}
	}))
	t.Cleanup(server.Close)
	for i, recipient := range []struct {
		role, status int
		subscribed   bool
	}{
		{common.RoleRootUser, common.UserStatusEnabled, true},
		{common.RoleAdminUser, common.UserStatusEnabled, false},
		{common.RoleCommonUser, common.UserStatusEnabled, true},
		{common.RoleAdminUser, common.UserStatusDisabled, true},
	} {
		user := model.User{Username: fmt.Sprintf("watcher-%d", i), AffCode: fmt.Sprintf("watcher-aff-%d", i), Role: recipient.role, Status: recipient.status}
		user.SetSetting(dto.UserSetting{NotifyType: dto.NotifyTypeWebhook, WebhookUrl: server.URL, NewUserRegistrationNotifyEnabled: recipient.subscribed})
		require.NoError(t, db.Create(&user).Error)
	}
	response := registrationNotificationRequest(t, 0, `{"username":"registered-user","password":"SecretPassword123"}`, Register)
	require.Contains(t, response.Body.String(), `"success":true`, "notification transport must not block registration")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	message := awaitRegistrationNotification(t, ctx, messages)
	assert.Equal(t, "new_user_registration", message.Type)
	assert.Contains(t, message.Content, "registered-user")
	assert.Contains(t, message.Content, "Registration method: password")
	assert.Contains(t, message.Content, "IP: 198.51.100.42")
	assert.Contains(t, message.Content, "Group: default")
	assert.NotContains(t, message.Content, "SecretPassword123")
	// SendRegistrationNotification bypasses the generic cap, even when it is zero.
	var watcher model.User
	require.NoError(t, db.First(&watcher, "username = ?", "watcher-0").Error)
	assert.Error(t, service.NotifyUser(watcher.Id, watcher.Email, watcher.GetSetting(), dto.NewNotify("new_user_registration", "test", "test", nil)))
	release <- struct{}{}
	retry := awaitRegistrationNotification(t, ctx, messages)
	assert.Equal(t, message.Content, retry.Content)

	provider := oauth.NewGenericOAuthProvider(&model.CustomOAuthProvider{Id: 7, Name: "Google", Slug: "google", Enabled: true})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/api/oauth/google", nil)
	c.Request.RemoteAddr = "198.51.100.43:12345"
	user, _, err := findOrCreateOAuthUser(c, provider, &oauth.OAuthUser{ProviderUserID: "google-subject", Username: "google-user"}, nil, "")
	require.NoError(t, err)
	message = awaitRegistrationNotification(t, ctx, messages)
	assert.Contains(t, message.Content, "Registration method: Google")
	assert.Contains(t, message.Content, "IP: 198.51.100.43")
	assert.Contains(t, message.Content, fmt.Sprintf("User ID: %d", user.Id))
	assert.Contains(t, message.Content, time.Unix(user.CreatedAt, 0).UTC().Format(time.RFC3339))
	common.RegisterEnabled = false
	existing, _, err := findOrCreateOAuthUser(c, provider, &oauth.OAuthUser{ProviderUserID: "google-subject"}, nil, "")
	require.NoError(t, err)
	assert.Equal(t, user.Id, existing.Id)
	_, _, err = findOrCreateOAuthUser(c, provider, &oauth.OAuthUser{ProviderUserID: "disabled-registration"}, nil, "")
	assert.Error(t, err)
	response = registrationNotificationRequest(t, 0, `{"username":"rejected-user","password":"SecretPassword123"}`, Register)
	assert.Contains(t, response.Body.String(), `"success":false`)
	common.RegisterEnabled = true
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("notification_binding_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "user_oauth_bindings" {
			tx.AddError(errors.New("binding failed"))
		}
	}))
	_, _, err = findOrCreateOAuthUser(c, provider, &oauth.OAuthUser{ProviderUserID: "failed-registration", Username: "rolled-back-user"}, nil, "")
	assert.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&model.User{}).Where("username IN ?", []string{"rolled-back-user", "rejected-user"}).Count(&count).Error)
	assert.Zero(t, count)
	assert.EqualValues(t, 3, requestCount.Load(), "only new, committed accounts notify subscribed active admins")
}

func awaitRegistrationNotification(t *testing.T, ctx context.Context, messages <-chan service.WebhookPayload) service.WebhookPayload {
	t.Helper()
	select {
	case message := <-messages:
		return message
	case <-ctx.Done():
		t.Fatal("registration notification was not delivered")
		return service.WebhookPayload{}
	}
}

func TestRegistrationNotificationTransportCancellation(t *testing.T) {
	setupRegistrationNotificationTransport(t)
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	user := model.User{Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
	user.SetSetting(dto.UserSetting{NotifyType: dto.NotifyTypeBark, BarkUrl: server.URL + "/{{content}}"})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- service.SendRegistrationNotification(ctx, user, dto.NewNotify("test", "test", "test", nil))
	}()
	<-started
	cancel()
	require.Error(t, <-result, "cancellation must close the actual transport, not leave a sending goroutine behind")
}

func TestRegistrationNotificationHTTPMethodsAndWorker(t *testing.T) {
	setupRegistrationNotificationTransport(t)
	previousWorker, previousKey, previousHTTP := system_setting.WorkerUrl, system_setting.WorkerValidKey, system_setting.WorkerAllowHttpImageRequestEnabled
	t.Cleanup(func() {
		system_setting.WorkerUrl, system_setting.WorkerValidKey, system_setting.WorkerAllowHttpImageRequestEnabled = previousWorker, previousKey, previousHTTP
	})
	const content = "Account name: Alice & Bob\nIP: 198.51.100.42"
	const title = "New user registered"
	for _, worker := range []bool{false, true} {
		for _, method := range []string{dto.NotifyTypeBark, dto.NotifyTypeWebhook, dto.NotifyTypeGotify} {
			t.Run(fmt.Sprintf("%s/worker=%t", method, worker), func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if worker {
						assert.Equal(t, http.MethodPost, r.Method)
						var request service.WorkerRequest
						require.NoError(t, common.DecodeJson(r.Body, &request))
						assert.Equal(t, "mock-worker-key", request.Key)
						if method == dto.NotifyTypeBark {
							assert.Equal(t, http.MethodGet, request.Method)
							parsed, err := url.Parse(request.URL)
							require.NoError(t, err)
							assert.Equal(t, content, parsed.Query().Get("body"))
							return
						}
						assert.Contains(t, request.Headers["Content-Type"], "application/json")
						assert.Equal(t, http.MethodPost, request.Method)
						assert.Contains(t, string(request.Body), "198.51.100.42")
						return
					}
					if method == dto.NotifyTypeBark {
						assert.Equal(t, http.MethodGet, r.Method)
						assert.Equal(t, content, r.URL.Query().Get("body"))
						assert.Equal(t, title, r.URL.Query().Get("title"))
						return
					}
					assert.Equal(t, http.MethodPost, r.Method)
					if method == dto.NotifyTypeWebhook {
						var payload service.WebhookPayload
						require.NoError(t, common.DecodeJson(r.Body, &payload))
						assert.Equal(t, content, payload.Content)
						assert.NotEmpty(t, r.Header.Get("X-Webhook-Signature"))
					} else {
						var payload struct {
							Title, Message string
							Priority       int
						}
						require.NoError(t, common.DecodeJson(r.Body, &payload))
						assert.Equal(t, content, payload.Message)
						assert.Equal(t, title, payload.Title)
						assert.Equal(t, 5, payload.Priority)
						assert.Equal(t, "mock-gotify-token", r.URL.Query().Get("token"))
					}
				}))
				defer server.Close()
				system_setting.WorkerUrl = ""
				settings := dto.UserSetting{NotifyType: method, BarkUrl: server.URL + "/mock-device?body={{content}}&title={{title}}", WebhookUrl: server.URL, WebhookSecret: "mock-webhook-secret", GotifyUrl: server.URL, GotifyToken: "mock-gotify-token", GotifyPriority: 5}
				if worker {
					system_setting.WorkerUrl, system_setting.WorkerValidKey, system_setting.WorkerAllowHttpImageRequestEnabled = server.URL, "mock-worker-key", true
				}
				user := model.User{Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
				user.SetSetting(settings)
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				require.NoError(t, service.SendRegistrationNotification(ctx, user, dto.NewNotify("new_user_registration", title, content, nil)))
				assert.EqualValues(t, 1, calls.Load())
			})
		}
	}
}
