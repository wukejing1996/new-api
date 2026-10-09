package setting

import (
	"fmt"
	"strings"
	"sync"
	"unicode"

	"github.com/QuantumNous/new-api/common"
)

const UserGroupRateLimitOptionKey = "UserGroupRateLimit"
const MaxUserGroupRateLimitDurationSeconds = 30 * 24 * 60 * 60

type UserGroupRateLimitRule struct {
	DurationSeconds int64 `json:"duration_seconds"`
	MaxRequests     int   `json:"max_requests"`
}

type UserGroupRateLimitConfig struct {
	Enabled bool                              `json:"enabled"`
	Groups  map[string]UserGroupRateLimitRule `json:"groups"`
}

var userGroupRateLimitMutex sync.RWMutex
var userGroupRateLimitConfig = UserGroupRateLimitConfig{Groups: map[string]UserGroupRateLimitRule{}}

func ParseUserGroupRateLimitConfig(raw string) (UserGroupRateLimitConfig, error) {
	var parsed UserGroupRateLimitConfig
	if len(raw) > 65536 {
		return parsed, fmt.Errorf("user group rate limit configuration is too large")
	}
	if err := common.UnmarshalJsonStr(raw, &parsed); err != nil {
		return parsed, err
	}
	if parsed.Groups == nil || len(parsed.Groups) > 1000 {
		return parsed, fmt.Errorf("user group rate limit groups must be an object with at most 1000 entries")
	}
	for group, rule := range parsed.Groups {
		if group == "" || strings.TrimSpace(group) != group || strings.ContainsFunc(group, unicode.IsControl) {
			return parsed, fmt.Errorf("invalid user group name %q", group)
		}
		if rule.DurationSeconds < 1 || rule.DurationSeconds > MaxUserGroupRateLimitDurationSeconds || rule.MaxRequests < 1 || rule.MaxRequests > 10000 {
			return parsed, fmt.Errorf("group %q requires a period of 1–2592000 seconds and 1–10000 requests", group)
		}
	}
	return parsed, nil
}

func UpdateUserGroupRateLimitConfig(raw string) error {
	parsed, err := ParseUserGroupRateLimitConfig(raw)
	if err != nil {
		return err
	}
	userGroupRateLimitMutex.Lock()
	defer userGroupRateLimitMutex.Unlock()
	userGroupRateLimitConfig = parsed
	return nil
}

func UserGroupRateLimitConfigJSON() string {
	userGroupRateLimitMutex.RLock()
	defer userGroupRateLimitMutex.RUnlock()
	encoded, _ := common.Marshal(userGroupRateLimitConfig)
	return string(encoded)
}

func GetUserGroupRateLimit(group string) (UserGroupRateLimitRule, bool) {
	userGroupRateLimitMutex.RLock()
	defer userGroupRateLimitMutex.RUnlock()
	rule, found := userGroupRateLimitConfig.Groups[group]
	return rule, userGroupRateLimitConfig.Enabled && found
}
