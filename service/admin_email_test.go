package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
