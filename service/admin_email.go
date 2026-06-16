package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	EmailBroadcastTargetAll      = "all"
	EmailBroadcastTargetSelected = "selected"
	MaxEmailBroadcastRecipients  = 1000
)

type EmailBroadcastTarget struct {
	Type    string `json:"type"`
	UserIds []int  `json:"user_ids"`
}

type EmailBroadcastRequest struct {
	Target  EmailBroadcastTarget `json:"target"`
	Subject string               `json:"subject"`
	Content string               `json:"content"`
	DryRun  bool                 `json:"dry_run"`
}

type EmailBroadcastFailure struct {
	UserId int    `json:"user_id"`
	Email  string `json:"email"`
	Error  string `json:"error"`
}

type EmailBroadcastResult struct {
	Total    int                     `json:"total"`
	Sent     int                     `json:"sent"`
	Skipped  int                     `json:"skipped"`
	Failed   int                     `json:"failed"`
	Failures []EmailBroadcastFailure `json:"failures,omitempty"`
	DryRun   bool                    `json:"dry_run"`
}

func SendEmailBroadcast(req EmailBroadcastRequest) (EmailBroadcastResult, error) {
	result := EmailBroadcastResult{
		DryRun: req.DryRun,
	}

	users, err := getEmailBroadcastUsers(req.Target)
	if err != nil {
		return result, err
	}

	if len(users) == 0 {
		return result, errors.New("no users with email addresses matched the target")
	}
	if len(users) > MaxEmailBroadcastRecipients {
		return result, fmt.Errorf("too many recipients: %d, maximum is %d", len(users), MaxEmailBroadcastRecipients)
	}

	result.Total = len(users)
	if req.DryRun {
		return result, nil
	}

	content, err := common.BuildNotificationEmailContent(req.Subject, req.Content)
	if err != nil {
		return result, err
	}

	for _, user := range users {
		email := strings.TrimSpace(user.Email)
		if email == "" {
			result.Skipped++
			continue
		}
		if err := common.SendEmail(req.Subject, email, content); err != nil {
			result.Failed++
			result.Failures = append(result.Failures, EmailBroadcastFailure{
				UserId: user.Id,
				Email:  common.MaskEmail(email),
				Error:  err.Error(),
			})
			continue
		}
		result.Sent++
	}

	return result, nil
}

func getEmailBroadcastUsers(target EmailBroadcastTarget) ([]model.User, error) {
	targetType := strings.TrimSpace(target.Type)
	if targetType == "" {
		targetType = EmailBroadcastTargetAll
	}

	query := model.DB.
		Model(&model.User{}).
		Select("id", "email").
		Where("status = ? AND email <> ?", common.UserStatusEnabled, "").
		Order("id asc")

	switch targetType {
	case EmailBroadcastTargetAll:
	case EmailBroadcastTargetSelected:
		userIds := uniquePositiveUserIds(target.UserIds)
		if len(userIds) == 0 {
			return nil, errors.New("selected users cannot be empty")
		}
		query = query.Where("id IN ?", userIds)
	default:
		return nil, fmt.Errorf("unsupported email broadcast target: %s", targetType)
	}

	var users []model.User
	if err := query.Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

func uniquePositiveUserIds(userIds []int) []int {
	seen := make(map[int]struct{}, len(userIds))
	result := make([]int, 0, len(userIds))
	for _, id := range userIds {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
