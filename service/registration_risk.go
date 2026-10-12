package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

var registrationAssociationScript = redis.NewScript(`
local clock = redis.call('TIME')
local now = tonumber(clock[1]) * 1000 + math.floor(tonumber(clock[2]) / 1000)
local ids = {}
local reasons = {}
for i = 1, #KEYS do
  local window = tonumber(ARGV[i * 2])
  local threshold = tonumber(ARGV[i * 2 + 1])
  redis.call('ZREMRANGEBYSCORE', KEYS[i], '-inf', now - window * 1000)
  redis.call('ZADD', KEYS[i], now, ARGV[1])
  redis.call('EXPIRE', KEYS[i], window)
  if redis.call('ZCARD', KEYS[i]) >= threshold then
    table.insert(reasons, tostring(i))
    for _, id in ipairs(redis.call('ZRANGE', KEYS[i], 0, -1)) do ids[id] = true end
  end
end
local result = {table.concat(reasons, ',')}
for id, _ in pairs(ids) do table.insert(result, id) end
return result
`)

// Keep account creation and OAuth binding in their existing atomic transaction;
// intelligence queries and risk finalization run outside that transaction.
func CreateOAuthUserWithRegistrationRisk(c *gin.Context, user *model.User, create func(*gorm.DB) error) error {
	return CreateUserWithRegistrationRisk(c, user, func() error {
		return model.DB.Transaction(create)
	})
}

// RegisterRegistrationAssociations counts a committed account and returns every
// related account that must be restricted. Failed creates must never call it.
func RegisterRegistrationAssociations(ctx context.Context, id int, ip string, cfg setting.RegistrationRiskConfig) ([]int, []string, error) {
	if !cfg.IPRule.Enabled && !cfg.SubnetRule.Enabled {
		return nil, nil, nil
	}
	if !common.RedisEnabled || common.RDB == nil {
		return nil, nil, fmt.Errorf("association_store_unavailable")
	}
	address, err := netip.ParseAddr(ip)
	if err != nil {
		return nil, nil, fmt.Errorf("association_ip_unavailable")
	}
	address = address.Unmap()
	keys, labels := []string{}, []string{}
	args := []any{strconv.Itoa(id)}
	for _, check := range []struct {
		rule         setting.RegistrationAssociationRule
		label, value string
	}{
		{cfg.IPRule, "ip_association", address.String()},
		{cfg.SubnetRule, "subnet_association", registrationSubnet(address, cfg)},
	} {
		if !check.rule.Enabled {
			continue
		}
		// Include rule parameters so changed windows never reuse truncated history.
		digest := sha256.Sum256([]byte(check.value))
		keys = append(keys, fmt.Sprintf("registrationRisk:association:v1:%s:%x:%d:%d", check.label, digest, check.rule.WindowSeconds, check.rule.TriggerCount))
		labels = append(labels, check.label)
		args = append(args, check.rule.WindowSeconds, check.rule.TriggerCount)
	}
	values, err := registrationAssociationScript.Run(ctx, common.RDB, keys, args...).StringSlice()
	if err != nil || len(values) == 0 {
		return nil, nil, fmt.Errorf("association_store_unavailable")
	}
	ids := make([]int, 0, len(values)-1)
	for _, value := range values[1:] {
		userID, err := strconv.Atoi(value)
		if err != nil || userID <= 0 {
			return nil, nil, fmt.Errorf("association_result_invalid")
		}
		ids = append(ids, userID)
	}
	reasons := []string{}
	for i, label := range labels {
		if slices.Contains(strings.Split(values[0], ","), strconv.Itoa(i+1)) {
			reasons = append(reasons, label)
		}
	}
	return ids, reasons, nil
}

func registrationSubnet(address netip.Addr, cfg setting.RegistrationRiskConfig) string {
	bits := cfg.IPv6Prefix
	if address.Is4() {
		bits = cfg.IPv4Prefix
	}
	return netip.PrefixFrom(address, bits).Masked().String()
}

// Self-service registrations use this wrapper; existing-account login and
// administrator-created accounts never enter it. No network call runs in a DB transaction.
func CreateUserWithRegistrationRisk(c *gin.Context, user *model.User, create func() error) error {
	cfg, apiKey := setting.GetRegistrationRiskConfig()
	if !cfg.Enabled {
		return create()
	}
	if err := model.ValidateRegistrationRiskTarget(cfg); err != nil {
		return err
	}
	assessment := EvaluateRegistrationRisk(c.Request.Context(), c.ClientIP(), user.Email, cfg, apiKey)
	originalGroup := user.Group
	if originalGroup == "" {
		originalGroup = "default"
	}
	record := RegistrationRiskRecord{Source: "registration", Assessment: assessment, Policy: cfg,
		OriginalGroup: originalGroup, Outcome: "registration_failed", ProcessingError: "account_creation_failed"}
	var relatedRecords []struct {
		user   model.User
		record RegistrationRiskRecord
	}
	// Every completed assessment is audited, even if the account transaction fails.
	// Deferred finalization releases the association lock before log writes.
	defer func() {
		recordRegistrationRisk(c, user, record)
		for _, related := range relatedRecords {
			recordRegistrationRisk(c, &related.user, related.record)
		}
	}()
	// Keep a newly committed account restricted until association finalization succeeds.
	user.Group = cfg.TargetGroup
	if err := create(); err != nil {
		return err
	}
	record.AccountCreated = true
	record.FinalGroup = &cfg.TargetGroup
	record.ProcessingError = ""
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 15*time.Second)
	defer cancel()
	if cfg.IPRule.Enabled || cfg.SubnetRule.Enabled {
		release, err := acquireRegistrationFinalization(ctx)
		if err != nil {
			assessment.Restricted, assessment.Incomplete = true, true
			assessment.Reasons = append(assessment.Reasons, "association_store_unavailable")
			// The account is already committed in the restrictive group. Do not
			// report a failed registration and encourage duplicate-account retries.
			record.Assessment = assessment
			record.Outcome = "restricted_incomplete"
			record.GroupApplied = true
			record.ProcessingError = "association_store_unavailable"
			return nil
		}
		defer release()
	}
	ids, reasons, associationErr := RegisterRegistrationAssociations(ctx, user.Id, c.ClientIP(), cfg)
	assessment.Reasons = append(assessment.Reasons, reasons...)
	if associationErr != nil {
		assessment.Incomplete = true
		assessment.Reasons = append(assessment.Reasons, "association_assessment_unavailable")
	}
	assessment.Restricted = assessment.Restricted || len(ids) > 0 || (assessment.Incomplete && cfg.FailClosed)
	record.Assessment = assessment
	targetGroup := originalGroup
	if assessment.Restricted {
		ids = append(ids, user.Id)
		ids = slices.Compact(slices.Sorted(slices.Values(ids)))
		targetGroup = cfg.TargetGroup
		record.Outcome = "restricted"
		if assessment.Incomplete {
			record.Outcome = "restricted_incomplete"
		}
	} else {
		ids = []int{user.Id}
		record.Outcome = "allowed"
		if assessment.Incomplete {
			record.Outcome = "allowed_incomplete"
		}
	}
	// Association registration and all group publications share a lock so an
	// earlier release cannot undo a later restriction.
	result, err := model.SetRegistrationRiskUserGroups(ctx, ids, targetGroup)
	record.RelatedUserIDs = ids
	record.GroupApplied, record.CacheSynced = result.GroupApplied, result.CacheSynced
	if result.GroupApplied {
		record.FinalGroup = &targetGroup
		user.Group = targetGroup
	}
	if err != nil {
		record.Outcome, record.ProcessingError = "processing_failed", err.Error()
	}
	for _, related := range result.Users {
		if related.Id == user.Id {
			continue
		}
		linked := record
		linked.Source, linked.TriggerUserID = "association", user.Id
		linked.OriginalGroup = related.Group
		linked.Assessment = RegistrationRiskAssessment{Restricted: true, Reasons: reasons}
		linked.FinalGroup = &related.Group
		if result.GroupApplied {
			linked.FinalGroup = &targetGroup
		}
		relatedRecords = append(relatedRecords, struct {
			user   model.User
			record RegistrationRiskRecord
		}{related, linked})
	}
	if err != nil {
		return err
	}
	return nil
}

func acquireRegistrationFinalization(ctx context.Context) (func(), error) {
	if !common.RedisEnabled || common.RDB == nil {
		return nil, fmt.Errorf("association_store_unavailable")
	}
	const key = "registrationRisk:finalize:v1"
	token := common.GetRandomString(32)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		acquired, err := common.RDB.SetNX(ctx, key, token, time.Minute).Result()
		if err != nil {
			return nil, fmt.Errorf("association_store_unavailable")
		}
		if acquired {
			return func() {
				releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				_ = common.RDB.Eval(releaseCtx, "if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('DEL', KEYS[1]) end return 0", []string{key}, token).Err()
			}, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("association_store_unavailable")
		case <-ticker.C:
		}
	}
}

type RegistrationRiskRecord = model.RegistrationRiskRecord

func NewRegistrationRiskLog(user *model.User, record RegistrationRiskRecord) model.RegistrationRiskLog {
	id := user.Id
	username := user.Username
	if username == "" {
		username = "(unavailable)" // Do not look up a nonexistent/rolled-back account.
	}
	if !record.AccountCreated {
		id = 0 // A rolled-back insert may have assigned an ID to the in-memory user.
	}
	return model.RegistrationRiskLog{
		UserId: id, Username: username,
		Success: record.AccountCreated && record.Outcome != "processing_failed",
		Details: record,
	}
}

func recordRegistrationRisk(c *gin.Context, user *model.User, record RegistrationRiskRecord) {
	entry := NewRegistrationRiskLog(user, record)
	model.RecordRegistrationRiskLog(c, entry)
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("CostRouter registration risk user=%d ip=%s outcome=%s restricted=%t incomplete=%t reasons=%v related=%v", entry.UserId, c.ClientIP(), record.Outcome, record.Assessment.Restricted, record.Assessment.Incomplete, record.Assessment.Reasons, record.RelatedUserIDs))
}
