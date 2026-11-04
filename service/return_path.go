package service

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/system_setting"
)

func PaymentReturnURL(suffix string) string {
	base := strings.TrimRight(system_setting.ServerAddress, "/")
	return base + suffix
}

// WalletReturnURL uses a public locale route supported by the independent frontend.
func WalletReturnURL(lang string) string {
	locale := strings.ToLower(strings.Split(strings.ReplaceAll(lang, "_", "-"), "-")[0])
	switch locale {
	case "fr", "ja", "ko", "ru", "vi":
	default:
		locale = "en"
	}
	return PaymentReturnURL("/" + locale + "/dashboard#wallet")
}
