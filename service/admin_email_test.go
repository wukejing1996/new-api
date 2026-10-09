package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmailBroadcastIncludesDisabledUsers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target EmailBroadcastTarget
		ids    []int
	}{
		{name: "all", target: EmailBroadcastTarget{Type: EmailBroadcastTargetAll}, ids: []int{1, 2}},
		{name: "selected disabled", target: EmailBroadcastTarget{Type: EmailBroadcastTargetSelected, UserIds: []int{2, 2, 3, 4, 999}}, ids: []int{2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			truncate(t)
			users := []model.User{
				{Id: 1, Username: "enabled", AffCode: "enabled", Status: common.UserStatusEnabled, Email: "enabled@example.com"},
				{Id: 2, Username: "disabled", AffCode: "disabled", Status: common.UserStatusDisabled, Email: "disabled@example.com"},
				{Id: 3, Username: "no_email", AffCode: "no_email", Status: common.UserStatusDisabled},
				{Id: 4, Username: "deleted", AffCode: "deleted", Status: common.UserStatusDisabled, Email: "deleted@example.com"},
			}
			require.NoError(t, model.DB.Create(&users).Error)
			require.NoError(t, model.DB.Delete(&users[3]).Error)

			recipients, err := getEmailBroadcastUsers(tc.target)
			require.NoError(t, err)
			ids := make([]int, 0, len(recipients))
			for _, user := range recipients {
				ids = append(ids, user.Id)
			}
			assert.Equal(t, tc.ids, ids)

			req := EmailBroadcastRequest{Target: tc.target, Subject: "Account notice", Content: "<p>Account notice</p>", DryRun: true}
			preview, err := SendEmailBroadcast(req)
			require.NoError(t, err)
			assert.Equal(t, len(tc.ids), preview.Total)
			req.DryRun = false
			queued, err := EnqueueEmailBroadcast(req)
			require.NoError(t, err)
			assert.Equal(t, preview.Total, queued.Total)
			assert.True(t, queued.Queued)
		})
	}
}

func TestEnqueueEmailBroadcastDeduplicatesActiveTask(t *testing.T) {
	truncate(t)

	require.NoError(t, model.DB.Create(&model.User{
		Id:       1,
		Username: "email_user",
		Status:   common.UserStatusEnabled,
		Email:    "user@example.com",
	}).Error)
	req := EmailBroadcastRequest{
		Target:  EmailBroadcastTarget{Type: EmailBroadcastTargetAll},
		Subject: "Subject",
		Content: "<p>Content</p>",
	}

	first, err := EnqueueEmailBroadcast(req)
	require.NoError(t, err)
	assert.True(t, first.Queued)
	assert.NotEmpty(t, first.TaskID)
	assert.Equal(t, 1, first.Total)

	second, err := EnqueueEmailBroadcast(req)
	require.NoError(t, err)
	assert.False(t, second.Queued)
	assert.Equal(t, first.TaskID, second.TaskID)
}
