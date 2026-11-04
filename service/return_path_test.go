package service

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
)

func TestPaymentReturnURLUsesSuppliedDefaultDashboardPath(t *testing.T) {
	previousAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://dashboard.example.com/"
	t.Cleanup(func() { system_setting.ServerAddress = previousAddress })

	assert.Equal(t, "https://dashboard.example.com/wallet", PaymentReturnURL("/wallet"))
	for lang, locale := range map[string]string{"en-US": "en", "zh-CN": "en", "fr-FR": "fr", "ja": "ja", "ko_KR": "ko", "ru-RU": "ru", "vi-VN": "vi", "unknown": "en"} {
		assert.Equal(t, "https://dashboard.example.com/"+locale+"/dashboard#wallet", WalletReturnURL(lang))
	}
}
