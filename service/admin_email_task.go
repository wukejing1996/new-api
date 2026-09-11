package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

type emailBroadcastTaskPayload struct {
	Request EmailBroadcastRequest `json:"request"`
}

type emailBroadcastTaskHandler struct{}

func (emailBroadcastTaskHandler) Type() string { return model.SystemTaskTypeEmailBroadcast }

func (emailBroadcastTaskHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := emailBroadcastTaskPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		finishEmailBroadcastTask(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}

	users, err := getEmailBroadcastUsers(payload.Request.Target)
	if err != nil {
		finishEmailBroadcastTask(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	if len(users) == 0 {
		finishEmailBroadcastTask(task, runnerID, model.SystemTaskStatusFailed, nil, errors.New("no users with email addresses matched the target"))
		return
	}
	if len(users) > MaxEmailBroadcastRecipients {
		finishEmailBroadcastTask(task, runnerID, model.SystemTaskStatusFailed, nil, fmt.Errorf("too many recipients: %d, maximum is %d", len(users), MaxEmailBroadcastRecipients))
		return
	}

	content, err := buildEmailBroadcastContent(payload.Request)
	if err != nil {
		finishEmailBroadcastTask(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}

	result, err := sendEmailBroadcastUsers(ctx, payload.Request, users, content, NewSystemTaskProgressReporter(task, runnerID))
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		finishEmailBroadcastTask(task, runnerID, model.SystemTaskStatusFailed, result, err)
		return
	}
	result.TaskID = task.TaskID
	finishEmailBroadcastTask(task, runnerID, model.SystemTaskStatusSucceeded, result, nil)
}

func finishEmailBroadcastTask(task *model.SystemTask, runnerID string, status model.SystemTaskStatus, result any, runErr error) {
	errorMessage := ""
	if runErr != nil {
		errorMessage = runErr.Error()
	}
	if err := model.FinishSystemTask(task.TaskID, runnerID, status, result, errorMessage); err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("system task %s failed to persist email broadcast result: %v", task.TaskID, err))
	}
}

func init() {
	RegisterSystemTaskHandler(emailBroadcastTaskHandler{})
}
