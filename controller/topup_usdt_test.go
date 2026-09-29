package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

type usdtTransport func(*http.Request) (*http.Response, error)

func (f usdtTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func usdtFixture(t *testing.T) {
	t.Helper()
	oldDB, oldLog, oldTransport := model.DB, model.LOG_DB, http.DefaultTransport
	oldConfig := setting.GetUsdtConfig()
	oldPayment := *operation_setting.GetPaymentSetting()
	oldQuota, oldReward := common.QuotaPerUnit, common.InviterTopUpRewardRatio
	oldRedis := common.RedisEnabled
	oldOptions := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.RedisEnabled = false
	var driver gorm.Dialector = sqlite.Open(":memory:")
	dbType := common.DatabaseTypeSQLite
	if dsn := os.Getenv("USDT_TEST_MYSQL_DSN"); dsn != "" {
		driver, dbType = mysql.Open(dsn), common.DatabaseTypeMySQL
	}
	if dsn := os.Getenv("USDT_TEST_POSTGRES_DSN"); dsn != "" {
		require.Empty(t, os.Getenv("USDT_TEST_MYSQL_DSN"), "configure one database at a time")
		driver, dbType = postgres.Open(dsn), common.DatabaseTypePostgreSQL
	}
	db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("usdt_test_%d_", time.Now().UnixNano())}})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	if dbType != common.DatabaseTypeSQLite {
		sqlDB.SetMaxOpenConns(4)
	}
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(dbType, dbType)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}, &model.Option{}))
	require.NoError(t, db.AutoMigrate(&model.TopUp{}))
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "payer", Quota: 0}).Error)
	common.QuotaPerUnit, common.InviterTopUpRewardRatio = 500000, 0
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = "v1"
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	setting.UsdtEnabled, setting.UsdtReceiveAddress, setting.UsdtNetwork = true, "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", "TRC20"
	setting.UsdtMinTopUp, setting.UsdtOrderExpireTime = 10, 1800
	t.Cleanup(func() {
		require.NoError(t, db.Migrator().DropTable(&model.User{}, &model.TopUp{}, &model.Log{}, &model.Option{}))
		common.OptionMap = oldOptions
		common.RedisEnabled = oldRedis
		model.DB, model.LOG_DB, http.DefaultTransport = oldDB, oldLog, oldTransport
		common.QuotaPerUnit, common.InviterTopUpRewardRatio = oldQuota, oldReward
		*operation_setting.GetPaymentSetting() = oldPayment
		setting.UsdtEnabled, setting.UsdtReceiveAddress, setting.UsdtNetwork = oldConfig.Enabled, oldConfig.Address, oldConfig.Network
		setting.UsdtMinTopUp, setting.UsdtOrderExpireTime = oldConfig.MinTopUp, oldConfig.OrderExpireTime
		sqlDB.Close()
		common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	})
}

func usdtRequest(t *testing.T, handler gin.HandlerFunc, user int, body string) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", user)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	handler(c)
	var result map[string]any
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
	return result
}

func newUsdtOrder(t *testing.T) *model.TopUp {
	t.Helper()
	result := usdtRequest(t, RequestUsdtPay, 1, `{"amount":10}`)
	require.Equal(t, "success", result["message"])
	order := model.GetTopUpByTradeNo(result["data"].(map[string]any)["trade_no"].(string))
	require.NotNil(t, order)
	return order
}

func TestUsdtOrderAndAtomicCredit(t *testing.T) {
	usdtFixture(t)
	order := newUsdtOrder(t)
	assert.Greater(t, order.Money, 10.0)
	assert.LessOrEqual(t, order.Money, 10.5)
	assert.Equal(t, decimal.NewFromFloat(order.Money).Truncate(3), decimal.NewFromFloat(order.Money))
	assert.Equal(t, order.Id, newUsdtOrder(t).Id, "repeat checkout resumes the same unpaid order")
	otherAmount := usdtRequest(t, RequestUsdtPay, 1, `{"amount":20}`)
	require.Equal(t, "success", otherAmount["message"])
	assert.NotEqual(t, order.TradeNo, otherAmount["data"].(map[string]any)["trade_no"])
	require.NoError(t, model.DB.Model(&model.TopUp{}).Where("id = ?", order.Id).Update("expire_time", time.Now().Unix()-1).Error)
	next := newUsdtOrder(t)
	assert.Nil(t, model.GetTopUpByTradeNo(order.TradeNo).UsdtAmountKey)
	hash := strings.Repeat("a", 64)
	// A failed wallet credit must release the hash and leave the order retryable.
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = 1").Update("quota", common.MaxWalletQuota).Error)
	require.Error(t, model.RechargeUsdt(order.TradeNo, hash))
	unchanged := model.GetTopUpByTradeNo(order.TradeNo)
	assert.Equal(t, "expired", unchanged.Status)
	assert.Nil(t, unchanged.UsdtTxHash)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = 1").Update("quota", 0).Error)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { errs <- model.RechargeUsdt(order.TradeNo, hash) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Error(t, model.RechargeUsdt(next.TradeNo, hash), "hash cannot credit another order")
	require.NoError(t, model.ExpireUsdtOrder(order.Id))
	assert.Equal(t, common.TopUpStatusSuccess, model.GetTopUpByTradeNo(order.TradeNo).Status)
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.Equal(t, order.UsdtQuota, user.Quota)
}

func TestUsdtConfirmedTransfersAndPagination(t *testing.T) {
	usdtFixture(t)
	order := newUsdtOrder(t)
	hash := strings.Repeat("b", 64)
	micros := strings.Split(*order.UsdtAmountKey, ":")[1]
	valid := service.TRC20Transaction{TransactionID: hash, To: order.UsdtAddress, Type: "Transfer", Value: micros, BlockTimestamp: order.CreateTime*1000 + 1000, TokenInfo: service.TokenInfo{Address: order.UsdtAddress, Decimals: 6}}
	for _, tc := range []struct {
		name   string
		mutate func(*service.TRC20Transaction)
	}{
		{"counterfeit token", func(v *service.TRC20Transaction) { v.TokenInfo.Address = "fake" }},
		{"wrong decimals", func(v *service.TRC20Transaction) { v.TokenInfo.Decimals = 3 }},
		{"wrong recipient", func(v *service.TRC20Transaction) { v.To = "wrong" }},
		{"approval", func(v *service.TRC20Transaction) { v.Type = "Approval" }},
		{"wrong amount", func(v *service.TRC20Transaction) { v.Value = "10000000" }},
		{"malformed amount", func(v *service.TRC20Transaction) { v.Value = micros + "junk" }},
		{"old transfer", func(v *service.TRC20Transaction) { v.BlockTimestamp = order.CreateTime*1000 - 1 }},
		{"late transfer", func(v *service.TRC20Transaction) { v.BlockTimestamp = order.ExpireTime*1000 + 1 }},
		{"invalid hash", func(v *service.TRC20Transaction) { v.TransactionID = "bad" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transfer := valid
			tc.mutate(&transfer)
			http.DefaultTransport = usdtTransport(func(r *http.Request) (*http.Response, error) {
				assert.Equal(t, "true", r.URL.Query().Get("only_confirmed"))
				body, err := common.Marshal(map[string]any{"success": true, "data": []service.TRC20Transaction{transfer}})
				require.NoError(t, err)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})
			found, err := service.FindMatchingTransaction(context.Background(), order.UsdtAddress, order.Money, order.CreateTime*1000, order.ExpireTime*1000)
			require.NoError(t, err)
			assert.Nil(t, found)
		})
	}
	calls := 0
	http.DefaultTransport = usdtTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		response := map[string]any{"success": true, "data": []service.TRC20Transaction{}, "meta": map[string]string{"fingerprint": "page2"}}
		if r.URL.Query().Get("fingerprint") == "page2" {
			response = map[string]any{"success": true, "data": []service.TRC20Transaction{valid}}
		}
		body, err := common.Marshal(response)
		require.NoError(t, err)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	// Disabling the gateway or changing its address must not prevent existing payment settlement.
	setting.UsdtEnabled = false
	setting.UsdtReceiveAddress = ""
	result := usdtRequest(t, CheckUsdtPayment, 1, fmt.Sprintf(`{"trade_no":%q}`, order.TradeNo))
	assert.Equal(t, "success", result["message"])
	assert.Equal(t, 2, calls)
	assert.Equal(t, hash, model.GetTopUpByTradeNo(order.TradeNo).TxHash)
}

func TestUsdtValidationAndOwnership(t *testing.T) {
	usdtFixture(t)
	for _, body := range []string{`{"amount":0}`, `{"amount":-1}`, `{"amount":9}`, `{"amount":10001}`, `{"amount":10.1}`, `{"amount":9223372036854775807}`} {
		assert.Equal(t, "error", usdtRequest(t, RequestUsdtPay, 1, body)["message"])
	}
	order := newUsdtOrder(t)
	assert.Equal(t, "error", usdtRequest(t, CheckUsdtPayment, 2, fmt.Sprintf(`{"trade_no":%q}`, order.TradeNo))["message"])
	require.NoError(t, model.DB.Model(order).Update("payment_provider", model.PaymentProviderStripe).Error)
	assert.Equal(t, "error", usdtRequest(t, CheckUsdtPayment, 1, fmt.Sprintf(`{"trade_no":%q}`, order.TradeNo))["message"])
	for key, value := range map[string]string{"UsdtReceiveAddress": "T1111111111111111111111111111111111", "UsdtNetwork": "ERC20", "UsdtCheckInterval": "0", "UsdtOrderExpireTime": "-1", "UsdtMinTopUp": "NaN", "UsdtEnabled": "yes"} {
		require.Error(t, model.UpdateOption(key, value), key)
	}
	require.NoError(t, model.UpdateOption("UsdtEnabled", "false"))
	assert.False(t, setting.IsUsdtTopUpEnabled())
}

func TestUsdtLateConfirmationAndUniqueReservations(t *testing.T) {
	usdtFixture(t)
	order := newUsdtOrder(t)
	duplicate := *order
	duplicate.Id = 0
	duplicate.TradeNo = "duplicate-reservation"
	require.Error(t, model.DB.Create(&duplicate).Error, "database must reject duplicate address and amount")
	require.NoError(t, model.DB.Model(order).Update("expire_time", time.Now().Unix()-1).Error)
	next := newUsdtOrder(t)
	hash := strings.Repeat("c", 64)
	require.NoError(t, model.RechargeUsdt(order.TradeNo, hash))
	require.Error(t, model.DB.Model(next).Update("usdt_tx_hash", hash).Error, "database must reject duplicate transaction hashes")
	require.NoError(t, model.ExpireUsdtOrder(next.Id))
	common.QuotaPerUnit = 1
	require.NoError(t, model.ManualCompleteTopUp(next.TradeNo, "127.0.0.1"))
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.Equal(t, order.UsdtQuota+next.UsdtQuota, user.Quota, "manual reconciliation uses the original quota snapshot")
}

func TestUsdtActualPaymentAndReferral(t *testing.T) {
	usdtFixture(t)
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{10: 0.9}
	require.NoError(t, model.DB.Create(&model.User{Id: 2, Username: "inviter", AffCode: "inviter-code"}).Error)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = 1").Update("inviter_id", 2).Error)
	common.InviterTopUpRewardRatio = 0.1
	order := newUsdtOrder(t)
	assert.Greater(t, order.Money, 10.0)
	assert.LessOrEqual(t, order.Money, 10.5)
	assert.Equal(t, decimal.NewFromFloat(order.Money).Mul(decimal.NewFromInt(500000)).IntPart(), int64(order.UsdtQuota))
	require.NoError(t, model.RechargeUsdt(order.TradeNo, strings.Repeat("d", 64)))
	require.NoError(t, model.RechargeUsdt(order.TradeNo, strings.Repeat("d", 64)))
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.Equal(t, order.UsdtQuota, user.Quota)
	var inviter model.User
	require.NoError(t, model.DB.First(&inviter, 2).Error)
	assert.Greater(t, inviter.Quota, 500000)
	assert.LessOrEqual(t, inviter.Quota, 525000)
}

func TestUsdtConfigurationToCredit(t *testing.T) {
	usdtFixture(t)
	require.NoError(t, model.UpdateOption("UsdtEnabled", "false"))
	info := usdtRequest(t, GetTopUpInfo, 1, "")["data"].(map[string]any)
	assert.Equal(t, false, info["enable_usdt_topup"])
	assert.Equal(t, "error", usdtRequest(t, RequestUsdtPay, 1, `{"amount":20}`)["message"])

	require.NoError(t, model.DB.Create(&model.User{Id: 2, Username: "inviter", AffCode: "flow-inviter"}).Error)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = 1").Update("inviter_id", 2).Error)
	common.InviterTopUpRewardRatio = 0.1
	require.NoError(t, model.UpdateOption("UsdtEnabled", "true"))
	info = usdtRequest(t, GetTopUpInfo, 1, "")["data"].(map[string]any)
	assert.Equal(t, true, info["enable_usdt_topup"])
	var hasUsdt bool
	for _, method := range info["pay_methods"].([]any) {
		if method.(map[string]any)["type"] == "usdt" {
			hasUsdt = true
		}
	}
	require.True(t, hasUsdt)
	result := usdtRequest(t, RequestUsdtPay, 1, `{"amount":20}`)
	require.Equal(t, "success", result["message"])
	data := result["data"].(map[string]any)
	assert.Equal(t, data["amount"], data["credited_amount"])
	order := model.GetTopUpByTradeNo(data["trade_no"].(string))
	require.NotNil(t, order)

	http.DefaultTransport = usdtTransport(func(_ *http.Request) (*http.Response, error) {
		transfer := service.TRC20Transaction{TransactionID: strings.Repeat("b", 64), To: order.UsdtAddress, Type: "Transfer", Value: decimal.NewFromFloat(order.Money).Mul(decimal.NewFromInt(1000000)).StringFixed(0), BlockTimestamp: order.UsdtStartTime, TokenInfo: service.TokenInfo{Address: setting.UsdtReceiveAddress, Decimals: 6}}
		body, err := common.Marshal(map[string]any{"success": true, "data": []service.TRC20Transaction{transfer}})
		require.NoError(t, err)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	// Disabling new checkouts must still allow an existing payment to settle.
	require.NoError(t, model.UpdateOption("UsdtEnabled", "false"))
	for range 2 {
		assert.Equal(t, "success", usdtRequest(t, CheckUsdtPayment, 1, fmt.Sprintf(`{"trade_no":%q}`, order.TradeNo))["message"])
	}
	var payer, inviter model.User
	require.NoError(t, model.DB.First(&payer, 1).Error)
	require.NoError(t, model.DB.First(&inviter, 2).Error)
	assert.Equal(t, order.UsdtQuota, payer.Quota)
	reward, err := common.WalletQuotaFromDecimalStrict(decimal.NewFromFloat(order.Money).Mul(decimal.NewFromFloat(0.1)).Mul(decimal.NewFromInt(500000)))
	require.NoError(t, err)
	assert.Equal(t, reward, inviter.Quota)
	settled := model.GetTopUpByTradeNo(order.TradeNo)
	assert.Equal(t, common.TopUpStatusSuccess, settled.Status)
	assert.True(t, settled.InviterRewarded)
	assert.Nil(t, settled.UsdtAmountKey)
}

func TestUsdtMigrationPreservesLegacyOrders(t *testing.T) {
	usdtFixture(t)
	var version string
	versionSQL := "SELECT version()"
	if model.DB.Dialector.Name() == "sqlite" {
		versionSQL = "SELECT sqlite_version()"
	}
	require.NoError(t, model.DB.Raw(versionSQL).Scan(&version).Error)
	t.Log(model.DB.Dialector.Name(), version)
	// Reconstruct the pre-fix table by removing only the newly introduced columns.
	for _, field := range []string{"UsdtAmountKey", "UsdtTxHash", "UsdtAddress", "UsdtQuota", "UsdtStartTime"} {
		require.NoError(t, model.DB.Migrator().DropColumn(&model.TopUp{}, field))
	}
	statement := &gorm.Statement{DB: model.DB}
	require.NoError(t, statement.Parse(&model.TopUp{}))
	require.NoError(t, model.DB.Table(statement.Schema.Table).Create(map[string]any{"user_id": 1, "amount": 10, "money": 0.2, "trade_no": "legacy-usdt", "payment_method": "usdt", "payment_provider": "usdt", "status": "processing", "tx_hash": strings.Repeat("e", 64)}).Error)
	for range 2 {
		require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}))
	}
	legacy := model.GetTopUpByTradeNo("legacy-usdt")
	require.NotNil(t, legacy)
	assert.Equal(t, 0.2, legacy.Money)
	assert.Nil(t, legacy.UsdtAmountKey)
	require.Error(t, model.RechargeUsdt(legacy.TradeNo, legacy.TxHash), "legacy amounts must not be silently auto-credited")
	newUsdtOrder(t)
	require.NoError(t, model.ManualCompleteTopUp(legacy.TradeNo, "127.0.0.1"))
}

func TestUsdtProviderFailureAndDelayedConfirmation(t *testing.T) {
	usdtFixture(t)
	order := newUsdtOrder(t)
	order.CreateTime = time.Now().Unix() - 3*86400
	order.UsdtStartTime = order.CreateTime * 1000
	order.ExpireTime = time.Now().Unix() - 2*86400
	require.NoError(t, model.DB.Save(order).Error)
	http.DefaultTransport = usdtTransport(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader(`{"success":false}`))}, nil
	})
	require.Error(t, service.CheckUsdtOrder(context.Background(), order))
	assert.Equal(t, "expired", model.GetTopUpByTradeNo(order.TradeNo).Status)
	assert.Nil(t, model.GetTopUpByTradeNo(order.TradeNo).UsdtAmountKey)
	http.DefaultTransport = usdtTransport(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success":true,"data":[]}`))}, nil
	})
	require.NoError(t, service.CheckUsdtOrder(context.Background(), order))
	order = model.GetTopUpByTradeNo(order.TradeNo)
	assert.Equal(t, "expired", order.Status)
	http.DefaultTransport = usdtTransport(func(_ *http.Request) (*http.Response, error) {
		transfer := service.TRC20Transaction{TransactionID: strings.Repeat("f", 64), To: order.UsdtAddress, Type: "Transfer", Value: decimal.NewFromFloat(order.Money).Mul(decimal.NewFromInt(1000000)).StringFixed(0), BlockTimestamp: order.CreateTime*1000 + 1000, TokenInfo: service.TokenInfo{Address: order.UsdtAddress, Decimals: 6}}
		body, err := common.Marshal(map[string]any{"success": true, "data": []service.TRC20Transaction{transfer}})
		require.NoError(t, err)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	require.NoError(t, service.CheckUsdtOrder(context.Background(), order))
	assert.Equal(t, common.TopUpStatusSuccess, model.GetTopUpByTradeNo(order.TradeNo).Status)
}

func TestUsdtSettingSaveFailureDoesNotChangeRuntime(t *testing.T) {
	usdtFixture(t)
	require.NoError(t, model.DB.Migrator().DropTable(&model.Option{}))
	require.Error(t, model.UpdateOption("UsdtEnabled", "false"))
	assert.True(t, setting.GetUsdtConfig().Enabled)
}

func TestUsdtAmountSlotsExhaustionAndRelease(t *testing.T) {
	usdtFixture(t)
	require.NoError(t, model.DB.Create(&model.User{Id: 2, Username: "other-payer", AffCode: "other-code"}).Error)
	now := time.Now().Unix()
	orders := make([]model.TopUp, 500)
	for i := range 500 {
		key := fmt.Sprintf("%s:%d", setting.UsdtReceiveAddress, 10000000+int64(i+1)*1000)
		orders[i] = model.TopUp{UserId: 2, Amount: 10, Money: 10 + float64(i+1)/1000, TradeNo: fmt.Sprintf("occupied-%d", i), PaymentMethod: "usdt", PaymentProvider: "usdt", Status: "pending", CreateTime: now, ExpireTime: now + 1800, UsdtAddress: setting.UsdtReceiveAddress, UsdtQuota: 5000000, UsdtAmountKey: &key}
	}
	require.NoError(t, model.DB.CreateInBatches(orders, 25).Error)
	assert.Equal(t, "error", usdtRequest(t, RequestUsdtPay, 1, `{"amount":10}`)["message"])
	require.NoError(t, model.DB.Model(&orders[136]).Update("expire_time", now-1).Error)
	order := newUsdtOrder(t)
	assert.Equal(t, 10.137, order.Money)
	assert.Equal(t, 5068500, order.UsdtQuota)
	assert.Nil(t, model.GetTopUpByTradeNo(orders[136].TradeNo).UsdtAmountKey)
	require.NoError(t, model.RechargeUsdt(order.TradeNo, strings.Repeat("1", 64)))
	settled := model.GetTopUpByTradeNo(order.TradeNo)
	assert.Nil(t, settled.UsdtAmountKey)
	require.NotNil(t, settled.UsdtTxHash)
	reused := newUsdtOrder(t)
	assert.Equal(t, 10.137, reused.Money)
	require.Error(t, model.RechargeUsdt(reused.TradeNo, strings.Repeat("1", 64)))
}
