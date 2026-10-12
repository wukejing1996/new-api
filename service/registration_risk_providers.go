package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

// All enabled sources share one deadline. A clean result never overrides another
// source's risk or incomplete result. Keep the legacy proxycheck fields for old records.
func EvaluateRegistrationRisk(ctx context.Context, ip, email string, cfg setting.RegistrationRiskConfig, apiKey string) RegistrationRiskAssessment {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	keys := setting.GetRegistrationRiskProviderKeys()
	type sourceResult struct {
		index      int
		assessment RegistrationRiskAssessment
		provider   model.RegistrationRiskProviderResult
	}
	sources := []struct {
		name, key string
		enabled   bool
	}{
		{"proxycheck.io", apiKey, cfg.ProxycheckEnabled},
		{"ipapi.is", keys.IPAPI, cfg.IPAPIEnabled},
		{"VPNAPI.io", keys.VPNAPI, cfg.VPNAPIEnabled},
	}
	results := make(chan sourceResult, len(sources))
	count := 0
	for i, source := range sources {
		if !source.enabled {
			continue
		}
		count++
		go func() {
			entry := sourceResult{index: i}
			if source.name == "proxycheck.io" {
				providerConfig := cfg
				providerConfig.FailClosed = false
				entry.assessment = evaluateProxycheckRegistrationRisk(ctx, ip, email, providerConfig, source.key)
				a := entry.assessment
				// Incomplete alone is recorded separately from an observed risk signal.
				p := model.RegistrationRiskProviderResult{Provider: source.name, Restricted: a.Restricted, Incomplete: a.Incomplete, Reasons: a.Reasons}
				if a.IP != nil {
					d := a.IP.Detections
					p.Signals = map[string]*bool{"proxy": d.Proxy, "vpn": d.VPN, "tor": d.Tor, "hosting": d.Hosting, "scraper": d.Scraper}
					p.Risk, p.NetworkType = d.Risk, a.IP.Network.Type
				}
				if a.Disposable != nil {
					if p.Signals == nil {
						p.Signals = map[string]*bool{}
					}
					p.Signals["disposable_email"] = a.Disposable
				}
				entry.provider = p
			} else {
				entry.provider = queryRegistrationRiskProvider(ctx, source.name, source.key, ip, cfg)
			}
			results <- entry
		}()
	}
	ordered := make([]sourceResult, len(sources))
	for range count {
		entry := <-results
		ordered[entry.index] = entry
	}
	result := RegistrationRiskAssessment{Reasons: []string{}}
	for i, source := range sources {
		if !source.enabled {
			continue
		}
		entry := ordered[i]
		p := entry.provider
		result.Providers = append(result.Providers, p)
		result.Restricted = result.Restricted || p.Restricted
		result.Incomplete = result.Incomplete || p.Incomplete
		if source.name == "proxycheck.io" {
			result.IP, result.Disposable = entry.assessment.IP, entry.assessment.Disposable
			result.Reasons = append(result.Reasons, p.Reasons...)
		} else {
			for _, reason := range p.Reasons {
				result.Reasons = append(result.Reasons, source.name+":"+reason)
			}
		}
	}
	result.Restricted = result.Restricted || (result.Incomplete && cfg.FailClosed)
	return result
}

func queryRegistrationRiskProvider(ctx context.Context, provider, key, ip string, cfg setting.RegistrationRiskConfig) model.RegistrationRiskProviderResult {
	failed := model.RegistrationRiskProviderResult{Provider: provider, Incomplete: true, Reasons: []string{"ip_assessment_unavailable"}}
	address, err := netip.ParseAddr(ip)
	if err != nil || !address.IsGlobalUnicast() || address.IsPrivate() {
		failed.Error = "client_ip_unavailable"
		return failed
	}
	if key == "" {
		failed.Error = "api_key_missing"
		return failed
	}
	ip = address.Unmap().String()
	digest := sha256.Sum256([]byte(key + "\x00" + ip))
	cacheKey := fmt.Sprintf("registrationRisk:%s:v1:%x", provider, digest)
	if cfg.CacheSeconds > 0 && common.RedisEnabled && common.RDB != nil {
		if body, err := common.RDB.Get(ctx, cacheKey).Bytes(); err == nil {
			return parseRegistrationRiskProvider(provider, ip, body, cfg)
		}
	}
	endpoint := "https://api.ipapi.is/?" + url.Values{"q": {ip}, "key": {key}}.Encode()
	if provider == "VPNAPI.io" {
		endpoint = "https://vpnapi.io/api/" + url.PathEscape(ip) + "?" + url.Values{"key": {key}}.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		failed.Error = "request_invalid"
		return failed
	}
	response, err := registrationRiskHTTPClient.Do(request)
	if err != nil {
		failed.Error = "network_or_timeout"
		return failed
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		failed.Error = fmt.Sprintf("http_%d", response.StatusCode)
		return failed
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(body) > 65536 {
		failed.Error = "response_invalid"
		return failed
	}
	result := parseRegistrationRiskProvider(provider, ip, body, cfg)
	if !result.Incomplete && cfg.CacheSeconds > 0 && common.RedisEnabled && common.RDB != nil {
		_ = common.RDB.Set(ctx, cacheKey, body, time.Duration(cfg.CacheSeconds)*time.Second).Err()
	}
	return result
}

func parseRegistrationRiskProvider(provider, ip string, body []byte, cfg setting.RegistrationRiskConfig) model.RegistrationRiskProviderResult {
	result := model.RegistrationRiskProviderResult{Provider: provider, Reasons: []string{}, Signals: map[string]*bool{}}
	var envelope struct {
		IP      string `json:"ip"`
		Error   any    `json:"error"`
		Success *bool  `json:"success"`
	}
	decodeErr := common.Unmarshal(body, &envelope)
	returnedIP, parseErr := netip.ParseAddr(envelope.IP)
	expectedIP, _ := netip.ParseAddr(ip)
	if decodeErr != nil || parseErr != nil || returnedIP.Unmap() != expectedIP.Unmap() || envelope.Error != nil || (envelope.Success != nil && !*envelope.Success) {
		result.Incomplete, result.Error, result.Reasons = true, "response_invalid", []string{"ip_assessment_unavailable"}
		return result
	}
	type signal struct {
		field, reason     string
		enabled, optional bool
	}
	checks := []signal{}
	if provider == "ipapi.is" {
		var data struct {
			Proxy      *bool `json:"is_proxy"`
			VPN        *bool `json:"is_vpn"`
			Tor        *bool `json:"is_tor"`
			Datacenter *bool `json:"is_datacenter"`
			Abuser     *bool `json:"is_abuser"`
			Company    struct {
				Type *string `json:"type"`
			} `json:"company"`
		}
		if common.Unmarshal(body, &data) != nil {
			result.Incomplete, result.Error, result.Reasons = true, "response_invalid", []string{"ip_assessment_unavailable"}
			return result
		}
		result.Signals = map[string]*bool{"proxy": data.Proxy, "vpn": data.VPN, "tor": data.Tor, "hosting": data.Datacenter, "abuser": data.Abuser}
		result.NetworkType = data.Company.Type
		checks = []signal{{"proxy", "proxy_ip", cfg.Proxy, false}, {"vpn", "vpn_ip", cfg.VPN, false}, {"tor", "tor_ip", cfg.Tor, false}, {"hosting", "hosting_ip", cfg.Hosting, false}, {"abuser", "abuser_ip", cfg.Abuser, false}}
		if cfg.Hosting && data.Company.Type != nil && strings.EqualFold(*data.Company.Type, "hosting") {
			result.Restricted = true
			result.Reasons = append(result.Reasons, "hosting_network")
		}
	} else {
		var data struct {
			Security map[string]*bool `json:"security"`
		}
		if common.Unmarshal(body, &data) != nil {
			result.Incomplete, result.Error, result.Reasons = true, "response_invalid", []string{"ip_assessment_unavailable"}
			return result
		}
		checks = []signal{{"proxy", "proxy_ip", cfg.Proxy, false}, {"vpn", "vpn_ip", cfg.VPN, false}, {"tor", "tor_ip", cfg.Tor, false}, {"relay", "relay_ip", cfg.Relay, false}, {"residential_proxy", "residential_proxy_ip", cfg.Proxy, true}, {"crawler", "scraper_ip", cfg.Scraper, true}}
		for _, check := range checks {
			result.Signals[check.field] = data.Security[check.field]
		}
	}
	for _, check := range checks {
		if !check.enabled {
			continue
		}
		value := result.Signals[check.field]
		if value == nil {
			if !check.optional {
				result.Incomplete = true
				result.Reasons = append(result.Reasons, check.reason+"_unknown")
			}
		} else if *value {
			result.Restricted = true
			result.Reasons = append(result.Reasons, check.reason)
		}
	}
	return result
}
