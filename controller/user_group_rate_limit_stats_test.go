package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserGroupRateLimitStats(t *testing.T) {
	previousRedis, previousClient := common.RedisEnabled, common.RDB
	t.Cleanup(func() { common.RedisEnabled, common.RDB = previousRedis, previousClient })
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	common.RedisEnabled, common.RDB = true, client
	engine := gin.New()
	engine.GET("/stats", GetUserGroupRateLimitStats)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/stats", nil))
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	assert.JSONEq(t, `{"success":true,"message":"","data":{"redis_enabled":true,"counts":{},"rejected_counts":{}}}`, response.Body.String())

	// Simultaneous completed replies for the same group must both be counted.
	ctx := context.Background()
	var writers sync.WaitGroup
	start := make(chan struct{})
	for range 2 {
		writers.Go(func() { <-start; service.RecordUserGroupRateLimitResult(ctx, "High Risk", http.StatusOK) })
	}
	close(start)
	writers.Wait()
	service.RecordUserGroupRateLimitResult(ctx, "High Risk", http.StatusTooManyRequests)
	service.RecordUserGroupRateLimitResult(ctx, "Other Risk", http.StatusTooManyRequests)
	service.RecordUserGroupRateLimitResult(ctx, "Other Risk", http.StatusInternalServerError)
	server.FastForward(31 * 24 * time.Hour)
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/stats", nil))
	assert.JSONEq(t, `{"success":true,"message":"","data":{"redis_enabled":true,"counts":{"High Risk":2},"rejected_counts":{"High Risk":1,"Other Risk":1}}}`, response.Body.String(), "counts remain cumulative after rate limit windows expire")

	// A statistics storage failure must not break response delivery.
	server.SetError("ERR statistics unavailable")
	service.RecordUserGroupRateLimitResult(ctx, "High Risk", http.StatusOK)
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/stats", nil))
	assert.Equal(t, http.StatusServiceUnavailable, response.Code)
	assert.JSONEq(t, `{"success":false,"message":"Failed to load rate limit counts"}`, response.Body.String())
	common.RDB = nil
	_, err := service.GetUserGroupRateLimitStats(ctx)
	assert.Error(t, err)

	common.RedisEnabled = false
	service.RecordUserGroupRateLimitResult(ctx, "High Risk", http.StatusTooManyRequests)
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/stats", nil))
	assert.Equal(t, http.StatusOK, response.Code)
	assert.JSONEq(t, `{"success":true,"message":"","data":{"redis_enabled":false,"counts":{},"rejected_counts":{}}}`, response.Body.String(), "disabled statistics must not pretend the counts are zero")
}
