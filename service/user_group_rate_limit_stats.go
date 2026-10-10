package service

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
)

// Independent from admission counters; cumulative counts have no window TTL.
const userGroupCustomResponseCountKey = "stats:userGroupCustomResponse:v1"
const userGroupRateLimitRejectionCountKey = "stats:userGroupRateLimit429:v1"

type UserGroupRateLimitStats struct {
	RedisEnabled   bool             `json:"redis_enabled"`
	Counts         map[string]int64 `json:"counts"`
	RejectedCounts map[string]int64 `json:"rejected_counts"`
}

func RecordUserGroupRateLimitResult(ctx context.Context, group string, status int) {
	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	key := userGroupCustomResponseCountKey
	if status == http.StatusTooManyRequests {
		key = userGroupRateLimitRejectionCountKey
	} else if status != http.StatusOK {
		return
	}
	// The response has already been sent. A client disconnect must not cancel
	// this bounded write, and a statistics failure must not change its reply.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := common.RDB.HIncrBy(ctx, key, group, 1).Err(); err != nil {
		logger.LogWarn(ctx, "user group rate limit count failed: "+err.Error())
	}
}

func GetUserGroupRateLimitStats(ctx context.Context) (UserGroupRateLimitStats, error) {
	stats := UserGroupRateLimitStats{RedisEnabled: common.RedisEnabled, Counts: map[string]int64{}, RejectedCounts: map[string]int64{}}
	if !common.RedisEnabled {
		return stats, nil
	}
	if common.RDB == nil {
		return stats, fmt.Errorf("Redis client is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	pipe := common.RDB.Pipeline()
	replies := pipe.HGetAll(ctx, userGroupCustomResponseCountKey)
	rejections := pipe.HGetAll(ctx, userGroupRateLimitRejectionCountKey)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return stats, err
	}
	for i, source := range []map[string]string{replies.Val(), rejections.Val()} {
		target := stats.Counts
		if i == 1 {
			target = stats.RejectedCounts
		}
		for group, raw := range source {
			count, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || count < 0 {
				return stats, fmt.Errorf("invalid user group rate limit count")
			}
			target[group] = count
		}
	}
	return stats, nil
}
