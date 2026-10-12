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

type RegistrationIPResult = model.RegistrationIPResult
type RegistrationRiskAssessment = model.RegistrationRiskAssessment

var registrationRiskHTTPClient = &http.Client{
	Timeout:       15 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func queryRegistrationRisk(ctx context.Context, address, apiKey string, cfg setting.RegistrationRiskConfig) ([]byte, error) {
	// Never retain complete addresses or credentials in cache keys or error text.
	digest := sha256.Sum256([]byte(apiKey + "\x00" + address))
	cacheKey := fmt.Sprintf("registrationRisk:proxycheck:v1:%x", digest)
	if cfg.CacheSeconds > 0 && common.RedisEnabled && common.RDB != nil {
		cached, err := common.RDB.Get(ctx, cacheKey).Bytes()
		if err == nil {
			return cached, nil
		}
	}
	requestURL := "https://proxycheck.io/v3/" + url.PathEscape(address) + "?" + url.Values{"key": {apiKey}, "tag": {"0"}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("proxycheck_request_invalid")
	}
	response, err := registrationRiskHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("proxycheck_unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxycheck_http_%d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(body) > 65536 {
		return nil, fmt.Errorf("proxycheck_response_invalid")
	}
	var envelope map[string]any
	if common.Unmarshal(body, &envelope) != nil || (envelope["status"] != "ok" && envelope["status"] != "warning") || envelope[address] == nil {
		return nil, fmt.Errorf("proxycheck_result_missing")
	}
	result, err := common.Marshal(envelope[address])
	if err != nil {
		return nil, fmt.Errorf("proxycheck_response_invalid")
	}
	if cfg.CacheSeconds > 0 && common.RedisEnabled && common.RDB != nil {
		_ = common.RDB.Set(ctx, cacheKey, result, time.Duration(cfg.CacheSeconds)*time.Second).Err()
	}
	return result, nil
}

func EvaluateRegistrationRisk(ctx context.Context, ip, email string, cfg setting.RegistrationRiskConfig, apiKey string) RegistrationRiskAssessment {
	result := RegistrationRiskAssessment{Reasons: []string{}}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	if cfg.Proxy || cfg.VPN || cfg.Tor || cfg.Hosting || cfg.Scraper || cfg.RiskScoreEnabled {
		address, err := netip.ParseAddr(ip)
		if err != nil || !address.IsGlobalUnicast() || address.IsPrivate() {
			result.Incomplete = true
			result.Reasons = append(result.Reasons, "client_ip_unavailable")
		} else {
			body, err := queryRegistrationRisk(ctx, address.Unmap().String(), apiKey, cfg)
			var data RegistrationIPResult
			if err != nil || common.Unmarshal(body, &data) != nil {
				result.Incomplete = true
				result.Reasons = append(result.Reasons, "ip_assessment_unavailable")
			} else {
				result.IP = &data
				for _, check := range []struct {
					enabled bool
					value   *bool
					reason  string
				}{
					{cfg.Proxy, data.Detections.Proxy, "proxy_ip"}, {cfg.VPN, data.Detections.VPN, "vpn_ip"},
					{cfg.Tor, data.Detections.Tor, "tor_ip"}, {cfg.Hosting, data.Detections.Hosting, "hosting_ip"},
					{cfg.Scraper, data.Detections.Scraper, "scraper_ip"},
				} {
					if !check.enabled {
						continue
					}
					if check.value == nil {
						result.Incomplete = true
						result.Reasons = append(result.Reasons, check.reason+"_unknown")
					} else if *check.value {
						result.Restricted = true
						result.Reasons = append(result.Reasons, check.reason)
					}
				}
				if cfg.Hosting {
					if data.Network.Type == nil {
						result.Incomplete = true
						result.Reasons = append(result.Reasons, "network_type_unknown")
					} else if strings.EqualFold(*data.Network.Type, "Hosting") {
						result.Restricted = true
						result.Reasons = append(result.Reasons, "hosting_network")
					}
				}
				if cfg.RiskScoreEnabled {
					if data.Detections.Risk == nil || *data.Detections.Risk < 0 || *data.Detections.Risk > 100 {
						result.Incomplete = true
						result.Reasons = append(result.Reasons, "risk_score_unknown")
					} else if *data.Detections.Risk >= cfg.RiskThreshold {
						result.Restricted = true
						result.Reasons = append(result.Reasons, "ip_risk_score")
					}
				}
			}
		}
	}
	if cfg.DisposableEmail {
		email = strings.ToLower(strings.TrimSpace(email))
		if email == "" || !strings.Contains(email, "@") {
			result.Incomplete = true
			result.Reasons = append(result.Reasons, "email_unavailable")
		} else {
			body, err := queryRegistrationRisk(ctx, email, apiKey, cfg)
			var data struct {
				Detections struct {
					Disposable *bool `json:"disposable"`
				} `json:"detections"`
			}
			if err != nil || common.Unmarshal(body, &data) != nil || data.Detections.Disposable == nil {
				result.Incomplete = true
				result.Reasons = append(result.Reasons, "email_assessment_unavailable")
			} else {
				result.Disposable = data.Detections.Disposable
				if *result.Disposable {
					result.Restricted = true
					result.Reasons = append(result.Reasons, "disposable_email")
				}
			}
		}
	}
	result.Restricted = result.Restricted || (result.Incomplete && cfg.FailClosed)
	return result
}
