package controller

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

type registrationPartnerEntry struct {
	setting.RegistrationPartnerMapping
	Link string `json:"link"`
}

func GetRegistrationPartnerMappings(c *gin.Context) {
	if _, ok := middleware.GetSessionAuthIdentity(c); !ok {
		common.ApiErrorStatus(c, http.StatusForbidden, common.NewMessage("Sign in with a dashboard session"))
		return
	}
	mappings := setting.GetRegistrationPartnerMappings()
	entries := make([]registrationPartnerEntry, 0, len(mappings))
	common.OptionMapRWMutex.RLock()
	serverAddress := system_setting.ServerAddress
	common.OptionMapRWMutex.RUnlock()
	base, err := url.Parse(strings.TrimSpace(serverAddress))
	for _, mapping := range mappings {
		entry := registrationPartnerEntry{RegistrationPartnerMapping: mapping}
		if err == nil && base.Hostname() != "" && base.User == nil &&
			(base.Scheme == "https" || (base.Scheme == "http" && (base.Hostname() == "localhost" || base.Hostname() == "127.0.0.1" || base.Hostname() == "::1"))) &&
			ratio_setting.ContainsGroupRatio(mapping.Group) {
			link := *base
			link.Path = strings.TrimRight(link.Path, "/") + "/en/register"
			link.RawPath, link.Fragment, link.RawFragment = "", "", ""
			link.RawQuery = url.Values{"partner": {mapping.Code}}.Encode()
			entry.Link = link.String()
		}
		entries = append(entries, entry)
	}
	common.ApiSuccess(c, entries)
}

func UpdateRegistrationPartnerMappings(c *gin.Context) {
	if _, ok := middleware.GetSessionAuthIdentity(c); !ok {
		common.ApiErrorStatus(c, http.StatusForbidden, common.NewMessage("Sign in with a dashboard session"))
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	var request struct {
		Mappings []setting.RegistrationPartnerMapping `json:"mappings"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.Mappings == nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid partner registration mappings"))
		return
	}
	raw, err := common.Marshal(request.Mappings)
	if err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid partner registration mappings"))
		return
	}
	mappings, err := setting.ParseRegistrationPartnerMappings(string(raw))
	if err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, err)
		return
	}
	for _, mapping := range mappings {
		if !ratio_setting.ContainsGroupRatio(mapping.Group) {
			common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Select an existing user group"))
			return
		}
	}
	if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeRegistrationPartners}) == nil {
		return
	}
	if err := model.UpdateOptionsBulk(map[string]string{setting.RegistrationPartnerOptionKey: string(raw)}); err != nil {
		common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Partner registration mappings could not be saved"))
		return
	}
	recordManageAudit(c, "CostRouter.registration_partner.settings.update", map[string]any{"mapping_count": len(mappings)})
	GetRegistrationPartnerMappings(c)
}
