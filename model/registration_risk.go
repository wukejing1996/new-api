package model

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
)

func ValidateRegistrationRiskTarget(cfg setting.RegistrationRiskConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if _, exists := ratio_setting.GetGroupRatioCopy()[cfg.TargetGroup]; !exists {
		return fmt.Errorf("the registration risk target group does not exist")
	}
	if _, enabled := setting.GetUserGroupRateLimit(cfg.TargetGroup); !enabled {
		return fmt.Errorf("enable a user group rate limit for the registration risk target group first")
	}
	if (cfg.IPRule.Enabled || cfg.SubnetRule.Enabled) && (!common.RedisEnabled || common.RDB == nil) {
		return fmt.Errorf("registration association detection requires Redis")
	}
	return nil
}

type RegistrationRiskGroupResult struct {
	Users        []User // Pre-update snapshots, also used to audit associated accounts.
	GroupApplied bool
	CacheSynced  bool
}

// Only the group changes: quota, credentials, auth version and sessions remain intact.
func SetRegistrationRiskUserGroups(ctx context.Context, ids []int, group string) (RegistrationRiskGroupResult, error) {
	result := RegistrationRiskGroupResult{}
	ids = slices.Compact(slices.Sorted(slices.Values(ids)))
	if len(ids) == 0 {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var users []User
	if err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id IN ? AND role = ?", ids, common.RoleCommonUser).Find(&users).Error; err != nil {
			return err
		}
		result.Users = users
		return tx.Model(&User{}).Where("id IN ? AND role = ?", ids, common.RoleCommonUser).Update("group", group).Error
	}); err != nil {
		return result, fmt.Errorf("registration_group_update_failed")
	}
	result.Users, result.GroupApplied = users, true
	if common.RedisEnabled && common.RDB == nil {
		return result, fmt.Errorf("registration_group_cache_sync_failed")
	}
	var syncErr error
	for _, user := range users {
		if err := PublishUserAuthCache(user.Id); err != nil {
			// A failed refresh must not leave a readable pre-update user snapshot.
			if invalidateUserCache(user.Id) != nil {
				syncErr = fmt.Errorf("registration_group_cache_sync_failed")
			}
		}
	}
	result.CacheSynced = syncErr == nil
	return result, syncErr
}
