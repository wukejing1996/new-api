package service

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// Only non-secret, committed account details are retained in the bounded queue.
type registrationNotification struct {
	UserID                      int
	Username, Method, IP, Group string
	CreatedAt                   int64
}

var registrationNotifications = make(chan registrationNotification, 128)
var registrationNotificationWorkers sync.Once

// QueueNewUserRegistrationNotification must be called only after account creation commits.
// Delivery is best effort: overload or a process restart may discard queued events.
func QueueNewUserRegistrationNotification(user *model.User, method, ip string) {
	registrationNotificationWorkers.Do(func() {
		for range 2 {
			go func() {
				for event := range registrationNotifications {
					deliverRegistrationNotification(event)
				}
			}()
		}
	})
	select {
	case registrationNotifications <- registrationNotification{
		UserID: user.Id, Username: user.Username, Method: method,
		IP: ip, Group: user.Group, CreatedAt: user.CreatedAt,
	}:
	default:
		common.SysLog(fmt.Sprintf("registration notification queue full: user=%d", user.Id))
	}
}

func deliverRegistrationNotification(event registrationNotification) {
	defer func() {
		if recover() != nil {
			common.SysLog(fmt.Sprintf("registration notification failed unexpectedly: user=%d", event.UserID))
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	var admins []model.User
	err := model.DB.WithContext(ctx).Select("id", "email", "role", "status", "setting").
		Where("status = ? AND role >= ?", common.UserStatusEnabled, common.RoleAdminUser).Find(&admins).Error
	cancel()
	if err != nil {
		common.SysLog(fmt.Sprintf("registration notification recipient query failed: user=%d", event.UserID))
		return
	}
	for _, admin := range admins {
		settings := admin.GetSetting()
		if !settings.NewUserRegistrationNotifyEnabled {
			continue
		}
		title := "New user registered"
		content := fmt.Sprintf("User ID: %d\nUsername: %s\nRegistration method: %s\nRegistered at: %s\nIP: %s\nGroup: %s\nUsers: %s/users",
			event.UserID, event.Username, event.Method, time.Unix(event.CreatedAt, 0).UTC().Format(time.RFC3339), event.IP, event.Group, strings.TrimRight(system_setting.ServerAddress, "/"))
		if strings.HasPrefix(settings.Language, "zh") {
			title = "新用户注册"
			content = fmt.Sprintf("用户 ID：%d\n用户名：%s\n注册方式：%s\n注册时间：%s\nIP：%s\n分组：%s\n用户管理：%s/users",
				event.UserID, event.Username, event.Method, time.Unix(event.CreatedAt, 0).UTC().Format(time.RFC3339), event.IP, event.Group, strings.TrimRight(system_setting.ServerAddress, "/"))
		}
		data := dto.NewNotify("new_user_registration", title, content, nil)
		// At most one retry; HTTP/SMTP requests have an actual transport deadline.
		for attempt := range 2 {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := SendRegistrationNotification(ctx, admin, data)
			cancel()
			if err == nil {
				break
			}
			// Transport errors may contain Bark keys or other credentials. Never log them.
			common.SysLog(fmt.Sprintf("registration notification delivery failed: user=%d admin=%d attempt=%d", event.UserID, admin.Id, attempt+1))
		}
	}
}

// SendRegistrationNotification reuses existing transports without the balance-alert cap.
// It also serves the admin-only test endpoint with the administrator's saved settings.
func SendRegistrationNotification(ctx context.Context, admin model.User, data dto.Notify) error {
	if admin.Role < common.RoleAdminUser || admin.Status != common.UserStatusEnabled {
		return fmt.Errorf("active administrator required")
	}
	settings := admin.GetSetting()
	switch settings.NotifyType {
	case "", dto.NotifyTypeEmail:
		if settings.NotificationEmail == "" && admin.Email == "" {
			return fmt.Errorf("notification email is not configured")
		}
		// Account names are untrusted; the existing email template accepts HTML.
		data.Content = strings.ReplaceAll(html.EscapeString(data.Content), "\n", "<br>")
	case dto.NotifyTypeBark:
		if settings.BarkUrl == "" || !strings.Contains(settings.BarkUrl, "{{content}}") {
			return fmt.Errorf("Bark notification content template is not configured")
		}
		// Bark uses path segments for messages; QueryEscape would display '+' for spaces.
		path, query, hasQuery := strings.Cut(settings.BarkUrl, "?")
		path = strings.ReplaceAll(path, "{{title}}", url.PathEscape(data.Title))
		path = strings.ReplaceAll(path, "{{content}}", url.PathEscape(data.Content))
		settings.BarkUrl = path
		if hasQuery {
			query = strings.ReplaceAll(query, "{{title}}", url.QueryEscape(data.Title))
			query = strings.ReplaceAll(query, "{{content}}", url.QueryEscape(data.Content))
			settings.BarkUrl += "?" + query
		}
	case dto.NotifyTypeWebhook:
		if settings.WebhookUrl == "" {
			return fmt.Errorf("notification webhook is not configured")
		}
	case dto.NotifyTypeGotify:
		if settings.GotifyUrl == "" || settings.GotifyToken == "" {
			return fmt.Errorf("Gotify notification is not configured")
		}
	default:
		return fmt.Errorf("unsupported notification method")
	}
	return sendUserNotification(ctx, admin.Id, admin.Email, settings, data)
}
