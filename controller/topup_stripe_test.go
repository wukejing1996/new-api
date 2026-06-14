package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetStripePayMoneyAppliesAmountDiscount(t *testing.T) {
	originalUnitPrice := setting.StripeUnitPrice
	originalQuotaDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalDiscounts := make(map[int]float64, len(operation_setting.GetPaymentSetting().AmountDiscount))
	for k, v := range operation_setting.GetPaymentSetting().AmountDiscount {
		originalDiscounts[k] = v
	}
	originalTopupGroupRatio := common.TopupGroupRatio2JSONString()

	t.Cleanup(func() {
		setting.StripeUnitPrice = originalUnitPrice
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalQuotaDisplayType
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscounts
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(originalTopupGroupRatio))
	})

	setting.StripeUnitPrice = 1
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{
		100: 0.97,
		200: 0,
	}
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1}`))

	require.InDelta(t, 97, getStripePayMoney(100, "default"), 0.000001)
	require.InDelta(t, 200, getStripePayMoney(200, "default"), 0.000001)
	require.InDelta(t, 50, getStripePayMoney(50, "default"), 0.000001)
}

func TestGetStripeCheckoutQuantity(t *testing.T) {
	originalUnitPrice := setting.StripeUnitPrice
	t.Cleanup(func() {
		setting.StripeUnitPrice = originalUnitPrice
	})

	t.Run("unit price one preserves pay money as quantity", func(t *testing.T) {
		setting.StripeUnitPrice = 1
		quantity, err := getStripeCheckoutQuantity(97)
		require.NoError(t, err)
		require.EqualValues(t, 97, quantity)
	})

	t.Run("unit price conversion preserves current behavior", func(t *testing.T) {
		setting.StripeUnitPrice = 8
		quantity, err := getStripeCheckoutQuantity(776)
		require.NoError(t, err)
		require.EqualValues(t, 97, quantity)
	})

	t.Run("rejects non integer checkout quantity", func(t *testing.T) {
		setting.StripeUnitPrice = 1
		_, err := getStripeCheckoutQuantity(96.03)
		require.Error(t, err)
	})

	t.Run("rejects invalid unit price", func(t *testing.T) {
		setting.StripeUnitPrice = 0
		_, err := getStripeCheckoutQuantity(97)
		require.Error(t, err)
	})
}

func TestValidateStripeTopUpPaidAmount(t *testing.T) {
	originalDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.TopUp{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
	})

	topUp := &model.TopUp{
		UserId:          1,
		Amount:          100,
		Money:           97,
		TradeNo:         "stripe-paid-amount-check",
		PaymentMethod:   model.PaymentMethodStripe,
		PaymentProvider: model.PaymentProviderStripe,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	require.NoError(t, validateStripeTopUpPaidAmount("stripe-paid-amount-check", "9700"))
	require.Error(t, validateStripeTopUpPaidAmount("stripe-paid-amount-check", "9600"))
}
