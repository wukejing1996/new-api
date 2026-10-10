package controller

import (
	"context"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func applyRegistrationNotificationSetting(c *gin.Context, user *model.User, req UpdateUserSettingRequest, settings *dto.UserSetting) bool {
	if user.Role < common.RoleAdminUser {
		if req.NewUserRegistrationNotifyEnabled != nil && *req.NewUserRegistrationNotifyEnabled {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "Administrator access required"})
			return false
		}
		return true
	}
	settings.NewUserRegistrationNotifyEnabled = user.GetSetting().NewUserRegistrationNotifyEnabled
	if req.NewUserRegistrationNotifyEnabled != nil {
		settings.NewUserRegistrationNotifyEnabled = *req.NewUserRegistrationNotifyEnabled
	}
	return true
}

func TestRegistrationNotification(c *gin.Context) {
	user, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if user.Role < common.RoleAdminUser || user.Status != common.UserStatusEnabled {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "Administrator access required"})
		return
	}
	title, content := "Registration notification test", "Your new user registration notifications are configured successfully."
	if language := user.GetSetting().Language; language == "zh" || language == "zh-TW" {
		title, content = "注册通知测试", "您的新用户注册通知已配置成功。"
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	if err := service.SendRegistrationNotification(ctx, *user, dto.NewNotify("new_user_registration_test", title, content, nil)); err != nil {
		// Never expose endpoint URLs, device keys, SMTP credentials, or response bodies.
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "Failed to send test notification. Check your saved notification settings and server connectivity."})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
