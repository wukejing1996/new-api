package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

// Check and admission are one atomic operation. Only admitted requests enter
// the sliding window; rejected requests neither extend it nor consume slots.
var userGroupRateLimitScript = redis.NewScript(`
local clock = redis.call('TIME')
local now = tonumber(clock[1]) * 1000 + math.floor(tonumber(clock[2]) / 1000)
local window = tonumber(ARGV[1]) * 1000
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now - window)
local count = redis.call('ZCARD', KEYS[1])
if count >= tonumber(ARGV[2]) then
  local oldest = redis.call('ZRANGE', KEYS[1], 0, 0, 'WITHSCORES')
  return {0, math.max(1, math.ceil((tonumber(oldest[2]) + window - now) / 1000))}
end
redis.call('ZADD', KEYS[1], now, tostring(now) .. ':' .. tostring(count))
redis.call('EXPIRE', KEYS[1], ARGV[1])
return {1, 0}
`)

var userGroupMemoryRateLimiter common.InMemoryRateLimiter

// A matching user rule replaces the legacy token-group limiter, including
// when that limiter is disabled. The caller must return when this is true.
func handleUserGroupRateLimit(c *gin.Context) bool {
	group := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	rule, matched := setting.GetUserGroupRateLimit(group)
	if !matched {
		return false
	}
	userID := c.GetInt("id")
	if userID <= 0 {
		abortWithOpenAiMessage(c, http.StatusInternalServerError, "user_group_rate_limit_user_missing")
		return true
	}
	// All keys and models for this user share a counter, including Auto keys.
	key := fmt.Sprintf("rateLimit:userGroupModel:v1:%d", userID)
	allowed := false
	if common.RedisEnabled {
		if common.RDB == nil {
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "user_group_rate_limit_check_failed")
			return true
		}
		values, err := userGroupRateLimitScript.Run(c.Request.Context(), common.RDB, []string{key}, rule.DurationSeconds, rule.MaxRequests).Int64Slice()
		if err != nil || len(values) != 2 {
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "user_group_rate_limit_check_failed")
			return true
		}
		allowed = values[0] == 1
	} else {
		// Cleanup must never evict an active, long-period rule early.
		userGroupMemoryRateLimiter.Init(time.Duration(setting.MaxUserGroupRateLimitDurationSeconds) * time.Second)
		allowed = userGroupMemoryRateLimiter.Request(key, rule.MaxRequests, rule.DurationSeconds)
	}
	if !allowed {
		if rule.CustomResponseEnabled {
			if respondUserGroupRateLimit(c, rule.CustomResponseMessage, group) {
				service.RecordUserGroupRateLimitResult(c.Request.Context(), group, http.StatusOK)
			}
			c.Abort()
			logger.LogError(c.Request.Context(), fmt.Sprintf("user %d | user group %q request limit reached: custom response returned", userID, group))
			return true
		}
		// Keep policy details in one runtime log, never in the public response
		// or a model usage/error log. No Retry-After reveals the policy window.
		_, preparedPluginRoute := c.Get(pluginruntime.ContextKeyRouteRequest)
		if !preparedPluginRoute || !RespondTaskPluginError(c, &dto.TaskError{
			Message:    "Rate limit exceeded",
			StatusCode: http.StatusTooManyRequests,
		}) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": gin.H{
					"message": "Rate limit exceeded",
					"type":    "new_api_error",
					"code":    "",
				},
			})
		}
		c.Abort()
		if c.Writer.Status() == http.StatusTooManyRequests && len(c.Errors) == 0 && c.Request.Context().Err() == nil {
			service.RecordUserGroupRateLimitResult(c.Request.Context(), group, http.StatusTooManyRequests)
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("user %d | user group %q request limit reached: at most %d requests within %d seconds", userID, group, rule.MaxRequests, rule.DurationSeconds))
		return true
	}
	c.Next()
	return true
}
