package controller

import (
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

// Mounted under RootAuth; the dedicated records never enter the self-audit API.
func GetRegistrationRiskRecords(c *gin.Context) {
	if c.GetInt("role") < common.RoleRootUser {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "Permission denied"})
		return
	}
	page := common.GetPageQuery(c)
	filter := model.RegistrationRiskLogFilter{Username: c.Query("username"), RequestId: c.Query("request_id")}
	if page.Page < 1 || page.Page > 100000000 || page.PageSize < 1 || len(filter.Username) > 64 || len(filter.RequestId) > 64 {
		common.ApiErrorMsg(c, "Invalid audit filters")
		return
	}
	if raw := c.Query("user_id"); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil || id < 1 {
			common.ApiErrorMsg(c, "Invalid audit filters")
			return
		}
		filter.UserId = id
	}
	for name, target := range map[string]*int64{"start_timestamp": &filter.StartTimestamp, "end_timestamp": &filter.EndTimestamp} {
		if raw := c.Query(name); raw != "" {
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || value < 0 {
				common.ApiErrorMsg(c, "Invalid audit time range")
				return
			}
			*target = value
		}
	}
	if filter.EndTimestamp > 0 && filter.EndTimestamp < filter.StartTimestamp {
		common.ApiErrorMsg(c, "Invalid audit time range")
		return
	}
	if raw := c.Query("success"); raw != "" {
		if raw != "true" && raw != "false" {
			common.ApiErrorMsg(c, "Invalid audit result")
			return
		}
		success := raw == "true"
		filter.Success = &success
	}
	if model.DB == nil {
		common.ApiErrorMsg(c, "Audit storage is unavailable")
		return
	}
	items, total, err := model.GetRegistrationRiskLogs(filter, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiErrorMsg(c, "Failed to load audit records")
		return
	}
	page.SetItems(items)
	page.SetTotal(int(total))
	common.ApiSuccess(c, page)
}

func GetRegistrationRiskSettings(c *gin.Context) {
	cfg, apiKey := setting.GetRegistrationRiskConfig()
	keys := setting.GetRegistrationRiskProviderKeys()
	rule, active := setting.GetUserGroupRateLimit(cfg.TargetGroup)
	common.ApiSuccess(c, gin.H{
		"config": cfg, "api_key_configured": apiKey != "", "redis_enabled": common.RedisEnabled && common.RDB != nil,
		"ipapi_key_configured": keys.IPAPI != "", "vpnapi_key_configured": keys.VPNAPI != "",
		"target_limit_enabled": active, "target_max_requests": rule.MaxRequests,
	})
}

func UpdateRegistrationRiskSettings(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	var request struct {
		Config    setting.RegistrationRiskConfig `json:"config"`
		APIKey    *string                        `json:"api_key"`
		IPAPIKey  *string                        `json:"ipapi_key"`
		VPNAPIKey *string                        `json:"vpnapi_key"`
	}
	request.Config = setting.DefaultRegistrationRiskConfig()
	if common.DecodeJson(c.Request.Body, &request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid registration risk settings"})
		return
	}
	raw, _ := common.Marshal(request.Config)
	cfg, err := setting.ParseRegistrationRiskConfig(string(raw))
	if err == nil {
		err = model.ValidateRegistrationRiskTarget(cfg)
	}
	_, currentKey := setting.GetRegistrationRiskConfig()
	if request.APIKey != nil {
		currentKey = strings.TrimSpace(*request.APIKey)
	}
	if err == nil && (len(currentKey) > 256 || strings.ContainsAny(currentKey, "\r\n\t ")) {
		err = errors.New("Invalid proxycheck API key")
	}
	keys := setting.GetRegistrationRiskProviderKeys()
	if request.IPAPIKey != nil {
		keys.IPAPI = strings.TrimSpace(*request.IPAPIKey)
	}
	if request.VPNAPIKey != nil {
		keys.VPNAPI = strings.TrimSpace(*request.VPNAPIKey)
	}
	if err == nil {
		err = setting.ValidateRegistrationRiskProviderKeys(cfg, currentKey, keys)
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	values := map[string]string{setting.RegistrationRiskOptionKey: string(raw)}
	if request.APIKey != nil {
		values[setting.RegistrationRiskAPIKeyOptionKey] = currentKey
	}
	if request.IPAPIKey != nil {
		values[setting.RegistrationRiskIPAPIKeyOptionKey] = keys.IPAPI
	}
	if request.VPNAPIKey != nil {
		values[setting.RegistrationRiskVPNAPIKeyOptionKey] = keys.VPNAPI
	}
	if err := model.UpdateOptionsBulk(values); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Registration risk settings could not be saved"})
		return
	}
	recordManageAudit(c, "CostRouter.registration_risk.settings.update", nil)
	GetRegistrationRiskSettings(c)
}

func TestRegistrationRiskQuery(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var request struct {
		IP    string `json:"ip"`
		Email string `json:"email"`
	}
	if common.DecodeJson(c.Request.Body, &request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid test request"})
		return
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(request.IP))
	if err != nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || len(request.Email) > 254 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Enter a public IP address and a valid email address"})
		return
	}
	cfg, apiKey := setting.GetRegistrationRiskConfig()
	if !cfg.ProxycheckEnabled && !cfg.IPAPIEnabled && !cfg.VPNAPIEnabled {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Enable at least one intelligence provider"})
		return
	}
	// A test only evaluates intelligence; it never creates associations or moves users.
	common.ApiSuccess(c, service.EvaluateRegistrationRisk(c.Request.Context(), ip.Unmap().String(), request.Email, cfg, apiKey))
}
