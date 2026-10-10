package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// UserGroupRateLimitResponseSinkKey lets the Responses WebSocket transport
// receive local model events without entering channel selection or billing.
type UserGroupRateLimitResponseSinkKey struct{}

func respondUserGroupRateLimit(c *gin.Context, message string) {
	path := c.Request.URL.Path
	target := types.RelayFormatOpenAI
	legacy := false
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
	case strings.HasSuffix(path, "/completions"):
		legacy = true
	case strings.HasSuffix(path, "/messages"):
		target = types.RelayFormatClaude
	case strings.HasSuffix(path, "/responses"):
		target = types.RelayFormatOpenAIResponses
	case strings.Contains(path, "/models/") && (strings.HasSuffix(path, ":generateContent") || strings.HasSuffix(path, ":streamGenerateContent")):
		target = types.RelayFormatGemini
	default:
		// Non-text APIs cannot represent an assistant message (e.g. embeddings).
		c.JSON(http.StatusOK, gin.H{"message": message})
		return
	}
	var request struct {
		Model         string `json:"model"`
		Stream        bool   `json:"stream"`
		StreamOptions struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
		Response *struct {
			Model string `json:"model"`
		} `json:"response"`
	}
	// Only read the body after denial; admitted requests remain untouched.
	_ = common.UnmarshalBodyReusable(c, &request)
	defer common.CleanupBodyStorage(c)
	sink, websocket := c.Request.Context().Value(UserGroupRateLimitResponseSinkKey{}).(func(*dto.ResponsesStreamResponse) error)
	if websocket {
		request.Stream = true
		if request.Response != nil {
			request.Model = request.Response.Model
		}
	}
	if target == types.RelayFormatGemini {
		_, modelPath, _ := strings.Cut(path, "/models/")
		request.Model, _, _ = strings.Cut(modelPath, ":")
		request.Stream = strings.HasSuffix(path, ":streamGenerateContent")
	}
	id := "chatcmpl-" + common.GetUUID()
	if target == types.RelayFormatOpenAIResponses {
		id = "resp_" + common.GetUUID()
	}
	created := time.Now().Unix()
	if legacy {
		response := gin.H{"id": id, "object": "text_completion", "created": created, "model": request.Model,
			"choices": []gin.H{{"index": 0, "text": message, "logprobs": nil, "finish_reason": "stop"}}}
		if !request.Stream {
			response["usage"] = &dto.Usage{}
			c.JSON(http.StatusOK, response)
			return
		}
		data, _ := common.Marshal(response)
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("X-Accel-Buffering", "no")
		_, _ = fmt.Fprintf(c.Writer, "data: %s\n\ndata: [DONE]\n\n", data)
		c.Writer.Flush()
		return
	}
	info := &convmeta.Values{}
	if !request.Stream {
		response := &dto.OpenAITextResponse{
			Id: id, Object: "chat.completion", Created: created, Model: request.Model,
			Choices: []dto.OpenAITextResponseChoice{{Index: 0, Message: dto.Message{Role: "assistant", Content: message}, FinishReason: "stop"}},
		}
		converted, err := relayconvert.ConvertResponse(c.Request.Context(), info, target, response)
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "user_group_rate_limit_response_failed")
			return
		}
		c.JSON(http.StatusOK, converted.Value)
		return
	}
	state, err := relayconvert.NewResponseStreamState(types.RelayFormatOpenAI, target, relayconvert.ResponseStreamOptions{
		ID: id, Model: request.Model, Created: created, IncludeUsage: request.StreamOptions.IncludeUsage, EmitSequenceNumber: true,
	})
	if err != nil {
		abortWithOpenAiMessage(c, http.StatusInternalServerError, "user_group_rate_limit_response_failed")
		return
	}
	stop := "stop"
	chunks := []*dto.ChatCompletionsStreamResponse{
		{Id: id, Object: "chat.completion.chunk", Created: created, Model: request.Model, Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Role: "assistant", Content: &message}}}},
		{Id: id, Object: "chat.completion.chunk", Created: created, Model: request.Model, Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0, FinishReason: &stop}}},
	}
	if request.StreamOptions.IncludeUsage {
		chunks = append(chunks, &dto.ChatCompletionsStreamResponse{Id: id, Object: "chat.completion.chunk", Created: created, Model: request.Model, Choices: []dto.ChatCompletionsStreamResponseChoice{}, Usage: &dto.Usage{}})
	}
	var events []relayconvert.ResponseResult
	for _, chunk := range chunks {
		converted, convertErr := relayconvert.ConvertStreamResponseChunk(c.Request.Context(), info, state, chunk)
		if convertErr != nil {
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "user_group_rate_limit_response_failed")
			return
		}
		events = append(events, converted...)
	}
	final, err := relayconvert.FinalizeStreamResponse(c.Request.Context(), info, state)
	if err != nil {
		abortWithOpenAiMessage(c, http.StatusInternalServerError, "user_group_rate_limit_response_failed")
		return
	}
	events = append(events, final...)
	for i := range events {
		if event, ok := events[i].Value.(relayconvert.ChatToResponsesStreamEvent); ok {
			events[i].Value = &event.Payload
		}
	}
	if websocket {
		for _, event := range events {
			if response, ok := event.Value.(*dto.ResponsesStreamResponse); ok {
				if err := sink(response); err != nil {
					return
				}
			}
		}
		c.Status(http.StatusOK)
		return
	}
	if target == types.RelayFormatGemini && c.Query("alt") != "sse" {
		values := make([]any, 0, len(events))
		for _, event := range events {
			values = append(values, event.Value)
		}
		c.JSON(http.StatusOK, values)
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	for _, event := range events {
		data, marshalErr := common.Marshal(event.Value)
		if marshalErr != nil {
			return
		}
		switch response := event.Value.(type) {
		case *dto.ClaudeResponse:
			_, err = fmt.Fprintf(c.Writer, "event: %s\n", response.Type)
		case *dto.ResponsesStreamResponse:
			_, err = fmt.Fprintf(c.Writer, "event: %s\n", response.Type)
		}
		if err != nil {
			return
		}
		if _, err = fmt.Fprintf(c.Writer, "data: %s\n\n", data); err != nil {
			return
		}
	}
	if target == types.RelayFormatOpenAI {
		_, _ = fmt.Fprint(c.Writer, "data: [DONE]\n\n")
	}
	c.Writer.Flush()
}
