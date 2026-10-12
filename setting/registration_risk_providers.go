package setting

import (
	"fmt"
	"strings"
)

const RegistrationRiskIPAPIKeyOptionKey = "RegistrationRiskIPAPIKey"
const RegistrationRiskVPNAPIKeyOptionKey = "RegistrationRiskVPNAPIKey"

type RegistrationRiskProviderKeys struct {
	IPAPI  string
	VPNAPI string
}

var registrationRiskProviderKeys RegistrationRiskProviderKeys

func ValidateRegistrationRiskProviderKey(value string) error {
	if len(value) > 256 || strings.ContainsAny(value, "\r\n\t ") {
		return fmt.Errorf("invalid registration risk API key")
	}
	return nil
}

func UpdateRegistrationRiskProviderKey(option, value string) error {
	value = strings.TrimSpace(value)
	if err := ValidateRegistrationRiskProviderKey(value); err != nil {
		return err
	}
	registrationRiskMutex.Lock()
	defer registrationRiskMutex.Unlock()
	switch option {
	case RegistrationRiskIPAPIKeyOptionKey:
		registrationRiskProviderKeys.IPAPI = value
	case RegistrationRiskVPNAPIKeyOptionKey:
		registrationRiskProviderKeys.VPNAPI = value
	default:
		return fmt.Errorf("unknown registration risk provider")
	}
	return nil
}

func GetRegistrationRiskProviderKeys() RegistrationRiskProviderKeys {
	registrationRiskMutex.RLock()
	defer registrationRiskMutex.RUnlock()
	return registrationRiskProviderKeys
}

func ValidateRegistrationRiskProviderKeys(cfg RegistrationRiskConfig, proxycheck string, keys RegistrationRiskProviderKeys) error {
	for _, provider := range []struct {
		enabled   bool
		name, key string
	}{
		{cfg.ProxycheckEnabled && (cfg.Proxy || cfg.VPN || cfg.Tor || cfg.Hosting || cfg.Scraper || cfg.RiskScoreEnabled || cfg.DisposableEmail), "proxycheck.io", proxycheck},
		{cfg.IPAPIEnabled, "ipapi.is", keys.IPAPI},
		{cfg.VPNAPIEnabled, "VPNAPI.io", keys.VPNAPI},
	} {
		if err := ValidateRegistrationRiskProviderKey(provider.key); err != nil {
			return err
		}
		if cfg.Enabled && provider.enabled && provider.key == "" {
			return fmt.Errorf("%s API key is required", provider.name)
		}
	}
	if cfg.Enabled && cfg.DisposableEmail && !cfg.ProxycheckEnabled {
		return fmt.Errorf("enable proxycheck.io for disposable email checks")
	}
	return nil
}
