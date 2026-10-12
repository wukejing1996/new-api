package setting

import (
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

const RegistrationRiskOptionKey = "RegistrationRisk"
const RegistrationRiskAPIKeyOptionKey = "RegistrationRiskAPIKey"

type RegistrationAssociationRule struct {
	Enabled       bool  `json:"enabled"`
	WindowSeconds int64 `json:"window_seconds"`
	TriggerCount  int   `json:"trigger_count"`
}

type RegistrationRiskConfig struct {
	Enabled           bool                        `json:"enabled"`
	ProxycheckEnabled bool                        `json:"proxycheck_enabled"`
	IPAPIEnabled      bool                        `json:"ipapi_enabled"`
	VPNAPIEnabled     bool                        `json:"vpnapi_enabled"`
	Abuser            bool                        `json:"abuser"`
	Relay             bool                        `json:"relay"`
	TargetGroup       string                      `json:"target_group"`
	Proxy             bool                        `json:"proxy"`
	VPN               bool                        `json:"vpn"`
	Tor               bool                        `json:"tor"`
	Hosting           bool                        `json:"hosting"`
	Scraper           bool                        `json:"scraper"`
	RiskScoreEnabled  bool                        `json:"risk_score_enabled"`
	RiskThreshold     int                         `json:"risk_threshold"`
	DisposableEmail   bool                        `json:"disposable_email"`
	FailClosed        bool                        `json:"fail_closed"`
	TimeoutSeconds    int                         `json:"timeout_seconds"`
	CacheSeconds      int                         `json:"cache_seconds"`
	IPRule            RegistrationAssociationRule `json:"ip_rule"`
	SubnetRule        RegistrationAssociationRule `json:"subnet_rule"`
	IPv4Prefix        int                         `json:"ipv4_prefix"`
	IPv6Prefix        int                         `json:"ipv6_prefix"`
}

var registrationRiskMutex sync.RWMutex
var registrationRiskConfig = DefaultRegistrationRiskConfig()
var registrationRiskAPIKey string

func DefaultRegistrationRiskConfig() RegistrationRiskConfig {
	return RegistrationRiskConfig{
		ProxycheckEnabled: true, Abuser: true, Relay: true,
		Proxy: true, VPN: true, Tor: true, Hosting: true, Scraper: true,
		RiskScoreEnabled: true, RiskThreshold: 50, DisposableEmail: true, FailClosed: true,
		TimeoutSeconds: 5, CacheSeconds: 300, IPv4Prefix: 24, IPv6Prefix: 64,
		IPRule:     RegistrationAssociationRule{Enabled: true, WindowSeconds: 3600, TriggerCount: 2},
		SubnetRule: RegistrationAssociationRule{WindowSeconds: 3600, TriggerCount: 3},
	}
}

func ParseRegistrationRiskConfig(raw string) (RegistrationRiskConfig, error) {
	cfg := DefaultRegistrationRiskConfig()
	if len(raw) > 8192 {
		return cfg, fmt.Errorf("registration risk configuration is too large")
	}
	if err := common.UnmarshalJsonStr(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("invalid registration risk configuration")
	}
	if len(cfg.TargetGroup) > 64 || strings.TrimSpace(cfg.TargetGroup) != cfg.TargetGroup || strings.ContainsAny(cfg.TargetGroup, "\r\n\t") || (cfg.Enabled && cfg.TargetGroup == "") {
		return cfg, fmt.Errorf("a valid target user group is required")
	}
	if cfg.RiskThreshold < 0 || cfg.RiskThreshold > 100 || cfg.TimeoutSeconds < 1 || cfg.TimeoutSeconds > 15 || cfg.CacheSeconds < 0 || cfg.CacheSeconds > 3600 || cfg.IPv4Prefix < 16 || cfg.IPv4Prefix > 32 || cfg.IPv6Prefix < 32 || cfg.IPv6Prefix > 128 {
		return cfg, fmt.Errorf("invalid risk threshold, timeout, cache duration or network prefix")
	}
	for _, rule := range []RegistrationAssociationRule{cfg.IPRule, cfg.SubnetRule} {
		if rule.WindowSeconds < 1 || rule.WindowSeconds > 86400 || rule.TriggerCount < 2 || rule.TriggerCount > 100 {
			return cfg, fmt.Errorf("association rules require 1–86400 seconds and a trigger count of 2–100")
		}
	}
	return cfg, nil
}

func UpdateRegistrationRiskConfig(raw string) error {
	cfg, err := ParseRegistrationRiskConfig(raw)
	if err != nil {
		return err
	}
	registrationRiskMutex.Lock()
	registrationRiskConfig = cfg
	registrationRiskMutex.Unlock()
	return nil
}

func UpdateRegistrationRiskAPIKey(value string) error {
	value = strings.TrimSpace(value)
	if len(value) > 256 || strings.ContainsAny(value, "\r\n\t ") {
		return fmt.Errorf("invalid proxycheck API key")
	}
	registrationRiskMutex.Lock()
	registrationRiskAPIKey = value
	registrationRiskMutex.Unlock()
	return nil
}

func GetRegistrationRiskConfig() (RegistrationRiskConfig, string) {
	registrationRiskMutex.RLock()
	defer registrationRiskMutex.RUnlock()
	return registrationRiskConfig, registrationRiskAPIKey
}

func RegistrationRiskConfigJSON() string {
	cfg, _ := GetRegistrationRiskConfig()
	data, _ := common.Marshal(cfg)
	return string(data)
}
