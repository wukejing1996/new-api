package setting

import (
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

const RegistrationPartnerOptionKey = "RegistrationPartnerSecret"

type RegistrationPartnerMapping struct {
	Code  string `json:"code"`
	Group string `json:"group"`
}

func ParseRegistrationPartnerMappings(raw string) ([]RegistrationPartnerMapping, error) {
	mappings := []RegistrationPartnerMapping{}
	if err := common.UnmarshalJsonStr(raw, &mappings); err != nil || mappings == nil || len(mappings) > 100 {
		return nil, common.NewMessage("Invalid partner registration mappings")
	}
	codes := make(map[string]struct{}, len(mappings))
	for _, mapping := range mappings {
		if strings.TrimSpace(mapping.Code) == "" {
			return nil, common.NewMessage("Partner code is required")
		}
		if _, exists := codes[mapping.Code]; exists {
			return nil, common.NewMessage("Partner registration codes must be unique")
		}
		if mapping.Group == "" || strings.TrimSpace(mapping.Group) != mapping.Group || utf8.RuneCountInString(mapping.Group) > 64 || mapping.Group == "auto" || mapping.Group == "all" {
			return nil, common.NewMessage("Select an existing user group")
		}
		codes[mapping.Code] = struct{}{}
	}
	return mappings, nil
}

func GetRegistrationPartnerMappings() []RegistrationPartnerMapping {
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[RegistrationPartnerOptionKey]
	common.OptionMapRWMutex.RUnlock()
	mappings, err := ParseRegistrationPartnerMappings(raw)
	if err != nil {
		return []RegistrationPartnerMapping{}
	}
	return mappings
}

func GetRegistrationPartnerGroup(code string) string {
	if code == "" {
		return ""
	}
	for _, mapping := range GetRegistrationPartnerMappings() {
		if mapping.Code == code && ratio_setting.ContainsGroupRatio(mapping.Group) {
			return mapping.Group
		}
	}
	return ""
}
