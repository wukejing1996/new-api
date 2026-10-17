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
	oldBsc := setting.GetUsdtNetworkConfig("BSC")
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
	setting.UsdtTrongridApiKey = t.Name()
	setting.UsdtMinTopUp, setting.UsdtOrderExpireTime = 10, 1800
	setting.UsdtBscEnabled, setting.UsdtBscReceiveAddress, setting.UsdtBscRpcURL = false, "", ""
	t.Cleanup(func() {
		require.NoError(t, db.Migrator().DropTable(&model.User{}, &model.TopUp{}, &model.Log{}, &model.Option{}))
		common.OptionMap = oldOptions
		common.RedisEnabled = oldRedis
		model.DB, model.LOG_DB, http.DefaultTransport = oldDB, oldLog, oldTransport
		common.QuotaPerUnit, common.InviterTopUpRewardRatio = oldQuota, oldReward
		*operation_setting.GetPaymentSetting() = oldPayment
		setting.UsdtEnabled, setting.UsdtReceiveAddress, setting.UsdtNetwork = oldConfig.Enabled, oldConfig.Address, oldConfig.Network
		setting.UsdtTrongridApiKey = oldConfig.APIKey
		setting.UsdtMinTopUp, setting.UsdtOrderExpireTime = oldConfig.MinTopUp, oldConfig.OrderExpireTime
		setting.UsdtBscEnabled, setting.UsdtBscReceiveAddress, setting.UsdtBscRpcURL = oldBsc.Enabled, oldBsc.Address, oldBsc.RPCURL
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
	valid := service.TRC20Transaction{TransactionID: hash, To: order.UsdtAddress, Type: "Transfer", Value: micros, BlockTimestamp: order.UsdtStartTime, TokenInfo: service.TokenInfo{Address: order.UsdtAddress, Decimals: 6}}
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
	// Both paths honor the provider cooldown without another outgoing request.
	http.DefaultTransport = usdtTransport(func(_ *http.Request) (*http.Response, error) {
		t.Fatal("a blocked credential must not make another request")
		return nil, nil
	})
	require.ErrorContains(t, service.CheckUsdtOrder(context.Background(), order), "paused")
	active := newUsdtOrder(t)
	require.ErrorContains(t, service.CheckPendingUsdtOrders(context.Background()), "paused")
	require.NoError(t, model.ExpireUsdtOrder(active.Id))
	// Rotating the blocked credential restores queries; production otherwise
	// retries after the shared cooldown elapses.
	setting.UsdtTrongridApiKey += "-recovered"
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

func TestUsdtBatchReconciliation(t *testing.T) {
	usdtFixture(t)
	now := time.Now().Unix()
	paid := newUsdtOrder(t)
	paid.Money, paid.UsdtQuota = 10.4, 5200000
	paidKey := fmt.Sprintf("%s:10400000", paid.UsdtAddress)
	paid.UsdtAmountKey = &paidKey
	require.NoError(t, model.DB.Save(paid).Error)
	require.NoError(t, model.RechargeUsdt(paid.TradeNo, strings.Repeat("a", 64)))
	reused := newUsdtOrder(t)
	// Deterministically reuse the released amount, including 10.400 == 10.4.
	reused.Money, reused.UsdtQuota = 10.4, 5200000
	key := fmt.Sprintf("%s:10400000", reused.UsdtAddress)
	reused.UsdtAmountKey = &key
	reused.CreateTime, reused.UsdtStartTime, reused.ExpireTime = now-60, (now-60)*1000, now+600
	require.NoError(t, model.DB.Save(reused).Error)
	orders := []*model.TopUp{reused}
	for _, amount := range []int{20, 30, 40, 50} {
		result := usdtRequest(t, RequestUsdtPay, 1, fmt.Sprintf(`{"amount":%d}`, amount))
		require.Equal(t, "success", result["message"])
		order := model.GetTopUpByTradeNo(result["data"].(map[string]any)["trade_no"].(string))
		order.CreateTime, order.UsdtStartTime, order.ExpireTime = now-300, (now-300)*1000, now+600
		if amount == 30 {
			order.CreateTime, order.UsdtStartTime, order.ExpireTime = now-600, (now-600)*1000, now-1
		}
		if amount == 40 {
			order.CreateTime, order.UsdtStartTime, order.ExpireTime = now-7200, (now-7200)*1000, now-601
		}
		require.NoError(t, model.DB.Save(order).Error)
		orders = append(orders, order)
	}
	// A legacy malformed order must still expire even without a reservation.
	legacy := &model.TopUp{UserId: 1, TradeNo: "old-invalid", PaymentProvider: "usdt", Status: "pending", ExpireTime: now - 7200}
	require.NoError(t, model.DB.Create(legacy).Error)
	transfers := make([]service.TRC20Transaction, 0)
	for i, order := range orders {
		if i == 3 || i == 4 { // old order excluded; last order not yet paid
			continue
		}
		transfers = append(transfers, service.TRC20Transaction{
			TransactionID: strings.Repeat(fmt.Sprintf("%x", i+11), 64), To: order.UsdtAddress, Type: "Transfer",
			Value: decimal.NewFromFloat(order.Money).Shift(6).StringFixed(0), BlockTimestamp: order.UsdtStartTime + 1000,
			TokenInfo: service.TokenInfo{Address: setting.TronUsdtContract, Decimals: 6},
		})
	}
	replayed := transfers[0]
	replayed.TransactionID = strings.Repeat("a", 64)
	tooEarly := transfers[0]
	tooEarly.TransactionID, tooEarly.BlockTimestamp = strings.Repeat("e", 64), reused.UsdtStartTime-1
	tooLate := transfers[2]
	tooLate.TransactionID, tooLate.BlockTimestamp = strings.Repeat("f", 64), orders[2].ExpireTime*1000
	calls := 0
	http.DefaultTransport = usdtTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		q := r.URL.Query()
		assert.Equal(t, fmt.Sprint((now-600)*1000), q.Get("min_timestamp"), "ancient expired orders do not widen the query")
		assert.Equal(t, "true", q.Get("only_confirmed"))
		assert.Equal(t, "true", q.Get("only_to"))
		assert.Equal(t, "200", q.Get("limit"))
		assert.Equal(t, setting.TronUsdtContract, q.Get("contract_address"))
		// Expiration and amount release happen before any provider request.
		assert.Equal(t, "expired", model.GetTopUpByTradeNo(orders[2].TradeNo).Status)
		assert.Nil(t, model.GetTopUpByTradeNo(orders[2].TradeNo).UsdtAmountKey)
		assert.Equal(t, "expired", model.GetTopUpByTradeNo(legacy.TradeNo).Status)
		response := map[string]any{"success": true, "data": []service.TRC20Transaction{replayed, tooEarly, tooLate}, "meta": map[string]string{"fingerprint": "second"}}
		if q.Get("fingerprint") == "second" {
			response = map[string]any{"success": true, "data": transfers}
		}
		body, err := common.Marshal(response)
		require.NoError(t, err)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	require.NoError(t, service.CheckPendingUsdtOrders(context.Background()))
	assert.Equal(t, 2, calls, "all orders at one address share the same paginated query")
	for _, order := range orders[:3] {
		settled := model.GetTopUpByTradeNo(order.TradeNo)
		assert.Equal(t, "success", settled.Status)
		assert.NotEqual(t, replayed.TransactionID, settled.TxHash)
		assert.Nil(t, settled.UsdtAmountKey)
	}
	assert.Equal(t, "expired", model.GetTopUpByTradeNo(orders[3].TradeNo).Status)
	assert.Equal(t, "pending", model.GetTopUpByTradeNo(orders[4].TradeNo).Status)
	var payer model.User
	require.NoError(t, model.DB.First(&payer, 1).Error)
	expectedQuota := paid.UsdtQuota + reused.UsdtQuota + orders[1].UsdtQuota + orders[2].UsdtQuota
	assert.Equal(t, expectedQuota, payer.Quota)
	// Subsequent rounds and manual checks cannot re-credit successful payments.
	http.DefaultTransport = usdtTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		assert.Equal(t, fmt.Sprint(orders[4].UsdtStartTime), r.URL.Query().Get("min_timestamp"))
		body, err := common.Marshal(map[string]any{"success": true, "data": transfers})
		require.NoError(t, err)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	require.NoError(t, service.CheckPendingUsdtOrders(context.Background()))
	assert.Equal(t, 3, calls)
	assert.Equal(t, "success", usdtRequest(t, CheckUsdtPayment, 1, fmt.Sprintf(`{"trade_no":%q}`, reused.TradeNo))["message"])
	assert.Equal(t, 3, calls, "successful manual checks read stored status")
	// With only historical orders left, the monitor makes no chain request.
	require.NoError(t, model.DB.Model(orders[4]).Update("expire_time", now-601).Error)
	require.NoError(t, service.CheckPendingUsdtOrders(context.Background()))
	assert.Equal(t, 3, calls)
	require.NoError(t, model.DB.First(&payer, 1).Error)
	assert.Equal(t, expectedQuota, payer.Quota)
}

func TestUsdtBatchDelayedConfirmationAndManualConcurrency(t *testing.T) {
	usdtFixture(t)
	now := time.Now().Unix()
	order := newUsdtOrder(t)
	order.CreateTime, order.UsdtStartTime, order.ExpireTime = now-300, (now-300)*1000, now-1
	require.NoError(t, model.DB.Save(order).Error)
	calls := 0
	http.DefaultTransport = usdtTransport(func(_ *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success":true,"data":[]}`))}, nil
	})
	require.NoError(t, service.CheckPendingUsdtOrders(context.Background()))
	assert.Equal(t, "expired", model.GetTopUpByTradeNo(order.TradeNo).Status)
	assert.Equal(t, 1, calls)
	// The same original window is queried again: confirmation appearing later
	// must not be skipped by advancing a timestamp cursor.
	http.DefaultTransport = usdtTransport(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, fmt.Sprint(order.UsdtStartTime), r.URL.Query().Get("min_timestamp"))
		assert.Equal(t, fmt.Sprint(order.ExpireTime*1000-1), r.URL.Query().Get("max_timestamp"))
		transfer := service.TRC20Transaction{TransactionID: strings.Repeat("d", 64), To: order.UsdtAddress, Type: "Transfer", Value: decimal.NewFromFloat(order.Money).Shift(6).StringFixed(0), BlockTimestamp: order.ExpireTime*1000 - 1, TokenInfo: service.TokenInfo{Address: setting.TronUsdtContract, Decimals: 6}}
		body, err := common.Marshal(map[string]any{"success": true, "data": []service.TRC20Transaction{transfer}})
		require.NoError(t, err)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Go(func() { errs <- service.CheckPendingUsdtOrders(context.Background()) })
	wg.Go(func() { errs <- service.CheckUsdtOrder(context.Background(), order) })
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, "success", model.GetTopUpByTradeNo(order.TradeNo).Status)
	var payer model.User
	require.NoError(t, model.DB.First(&payer, 1).Error)
	assert.Equal(t, order.UsdtQuota, payer.Quota)
}

func TestUsdtBatchUsesOrderAddressSnapshots(t *testing.T) {
	usdtFixture(t)
	first := newUsdtOrder(t)
	second := &model.TopUp{UserId: 1, Amount: 20, Money: 20.1, TradeNo: "previous-address", PaymentMethod: "usdt", PaymentProvider: "usdt", Status: "pending", CreateTime: time.Now().Unix() - 60, ExpireTime: time.Now().Unix() + 600, UsdtAddress: "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE", UsdtQuota: 10050000}
	require.True(t, setting.ValidTronAddress(second.UsdtAddress))
	key := second.UsdtAddress + ":20100000"
	second.UsdtAmountKey = &key
	require.NoError(t, model.DB.Create(second).Error)
	setting.UsdtReceiveAddress, setting.UsdtEnabled = "", false
	queries := make(map[string]int)
	http.DefaultTransport = usdtTransport(func(r *http.Request) (*http.Response, error) {
		address := strings.Split(r.URL.Path, "/")[3]
		queries[address]++
		order, hash := first, strings.Repeat("1", 64)
		if address == second.UsdtAddress {
			order, hash = second, strings.Repeat("2", 64)
		}
		start := order.UsdtStartTime
		if start == 0 {
			start = order.CreateTime * 1000
		}
		transfer := service.TRC20Transaction{TransactionID: hash, To: address, Type: "Transfer", Value: decimal.NewFromFloat(order.Money).Shift(6).StringFixed(0), BlockTimestamp: start, TokenInfo: service.TokenInfo{Address: setting.TronUsdtContract, Decimals: 6}}
		body, err := common.Marshal(map[string]any{"success": true, "data": []service.TRC20Transaction{transfer}})
		require.NoError(t, err)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	require.NoError(t, service.CheckPendingUsdtOrders(context.Background()))
	assert.Equal(t, map[string]int{first.UsdtAddress: 1, second.UsdtAddress: 1}, queries)
	assert.Equal(t, "success", model.GetTopUpByTradeNo(first.TradeNo).Status)
	assert.Equal(t, "success", model.GetTopUpByTradeNo(second.TradeNo).Status)
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

// Only the HTTP boundary is mocked: requests, scanning, proof checks and credit use production code.
func bscFixture(t *testing.T, response func(string, []any) any) {
	t.Helper()
	http.DefaultTransport = usdtTransport(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		require.NoError(t, common.DecodeJson(r.Body, &request))
		result := response(request.Method, request.Params)
		body, err := common.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
		require.NoError(t, err)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
}

func TestUsdtBscConfigurationOrdersAndEventCredit(t *testing.T) {
	usdtFixture(t)
	require.Error(t, model.UpdateOption("UsdtBscEnabled", "true"))
	for _, value := range []string{"0x0", "0x" + strings.Repeat("0", 40), "T1111111111111111111111111111111111"} {
		require.Error(t, model.UpdateOption("UsdtBscReceiveAddress", value))
	}
	require.Error(t, model.UpdateOption("UsdtBscRpcURL", "http://example.com"))
	require.NoError(t, model.UpdateOption("UsdtBscReceiveAddress", "0x3932AF6Fdbc450446bdb4eb4d2d13335c42a0417"))
	require.NoError(t, model.UpdateOption("UsdtBscRpcURL", "https://example.com/private-test-key"))
	bscFixture(t, func(method string, _ []any) any {
		switch method {
		case "eth_chainId":
			return "0x38"
		case "eth_call":
			return "0x12"
		case "eth_getBlockByNumber":
			return map[string]string{"number": "0x64", "hash": "0x" + strings.Repeat("a", 64), "timestamp": fmt.Sprintf("0x%x", time.Now().Unix())}
		case "eth_getLogs":
			return []any{}
		default:
			t.Fatalf("unexpected method %s", method)
			return nil
		}
	})
	assert.Equal(t, "error", usdtRequest(t, RequestUsdtPay, 1, `{"amount":10,"network":"BSC"}`)["message"])
	assert.Equal(t, true, usdtRequest(t, UpdateOption, 1, `{"key":"UsdtBscEnabled","value":true}`)["success"])
	info := usdtRequest(t, GetTopUpInfo, 1, "")["data"].(map[string]any)
	assert.Equal(t, []any{"TRC20", "BSC"}, info["usdt_networks"])
	tron := newUsdtOrder(t)
	result := usdtRequest(t, RequestUsdtPay, 1, `{"amount":10,"network":"BSC"}`)
	require.Equal(t, "success", result["message"])
	data := result["data"].(map[string]any)
	assert.Equal(t, "BSC", data["network"])
	order := model.GetTopUpByTradeNo(data["trade_no"].(string))
	assert.NotEqual(t, tron.Id, order.Id)
	assert.Equal(t, setting.BscUsdtContract, order.UsdtContract)
	assert.Equal(t, 18, order.UsdtDecimals)
	assert.Equal(t, int64(100), order.UsdtStartBlock)
	assert.Equal(t, strings.ToLower(setting.UsdtBscReceiveAddress), order.UsdtAddress)
	assert.NotEqual(t, *tron.UsdtAmountKey, *order.UsdtAmountKey)
	// Changed receive configuration must not change the resumed order.
	require.NoError(t, model.UpdateOption("UsdtBscReceiveAddress", "0x"+strings.Repeat("2", 40)))
	resumed := usdtRequest(t, RequestUsdtPay, 1, `{"amount":10,"network":"BSC"}`)["data"].(map[string]any)
	assert.Equal(t, order.TradeNo, resumed["trade_no"])
	assert.Equal(t, order.UsdtAddress, resumed["address"])
	require.NoError(t, model.ExpireUsdtOrder(order.Id))
	secondResult := usdtRequest(t, RequestUsdtPay, 1, `{"amount":10,"network":"BSC"}`)["data"].(map[string]any)
	second := model.GetTopUpByTradeNo(secondResult["trade_no"].(string))
	hash := strings.Repeat("9", 64)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = 1").Update("quota", common.MaxWalletQuota).Error)
	require.Error(t, model.RechargeUsdt(order.TradeNo, hash, 0))
	assert.Nil(t, model.GetTopUpByTradeNo(order.TradeNo).UsdtEventKey)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = 1").Update("quota", 0).Error)
	require.NoError(t, model.RechargeUsdt(order.TradeNo, hash, 0))
	require.NoError(t, model.RechargeUsdt(order.TradeNo, hash, 0))
	require.Error(t, model.RechargeUsdt(second.TradeNo, hash, 0))
	require.NoError(t, model.RechargeUsdt(second.TradeNo, hash, 1), "different transfer in one transaction can credit its own order")
	require.NoError(t, model.RechargeUsdt(tron.TradeNo, hash), "TRON and BSC have separate payment identities")
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.Equal(t, order.UsdtQuota+second.UsdtQuota+tron.UsdtQuota, user.Quota)
	require.NoError(t, model.UpdateOption("UsdtEnabled", "false"))
	assert.True(t, setting.IsUsdtTopUpEnabled(), "BSC remains available with TRC20 disabled")
	assert.Equal(t, "error", usdtRequest(t, RequestUsdtPay, 1, `{"amount":10}`)["message"], "legacy request never silently selects BSC")
	assert.Equal(t, "error", usdtRequest(t, RequestUsdtPay, 1, `{"amount":10,"network":"ETH"}`)["message"])
}

func TestUsdtBscTransferProofAndCursor(t *testing.T) {
	for _, scenario := range []string{"valid", "wrong network", "wrong token", "wrong recipient", "wrong amount", "approval", "removed", "before order", "after expiry", "failed receipt", "receipt missing transfer", "noncanonical", "unfinalized", "query failure", "delayed confirmation", "expired unpaid"} {
		t.Run(scenario, func(t *testing.T) {
			usdtFixture(t)
			setting.UsdtBscRpcURL = "https://example.com/bsc"
			now := time.Now().Unix()
			order, err := model.CreateUsdtOrder(&model.TopUp{UserId: 1, Amount: 20, TradeNo: "bsc-proof", PaymentMethod: "usdt", PaymentProvider: "usdt", Status: "pending", CreateTime: now - 60, ExpireTime: now + 120, UsdtAddress: "0x" + strings.Repeat("1", 40), UsdtNetwork: "BSC", UsdtStartBlock: 100})
			require.NoError(t, err)
			if scenario == "delayed confirmation" || scenario == "expired unpaid" {
				require.NoError(t, model.DB.Model(order).Update("expire_time", now-1).Error)
				order.ExpireTime = now - 1
			}
			blockHash, txHash := "0x"+strings.Repeat("a", 64), "0x"+strings.Repeat("b", 64)
			entry := map[string]any{"address": setting.BscUsdtContract, "topics": []string{"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef", "0x" + strings.Repeat("0", 64), "0x" + strings.Repeat("0", 24) + order.UsdtAddress[2:]}, "data": "0x" + fmt.Sprintf("%064x", decimal.NewFromFloat(order.Money).Shift(18).BigInt()), "blockNumber": "0x65", "blockHash": blockHash, "transactionHash": txHash, "logIndex": "0x0", "removed": false}
			stamp := now - 10
			switch scenario {
			case "wrong token":
				entry["address"] = "0x" + strings.Repeat("2", 40)
			case "wrong recipient":
				entry["topics"].([]string)[2] = "0x" + strings.Repeat("0", 64)
			case "wrong amount":
				entry["data"] = "0x" + strings.Repeat("0", 63) + "1"
			case "approval":
				entry["topics"].([]string)[0] = "0x" + strings.Repeat("0", 64)
			case "removed":
				entry["removed"] = true
			case "before order":
				stamp = order.CreateTime - 1
			case "after expiry":
				stamp = order.ExpireTime
			}
			logCalls := 0
			bscFixture(t, func(method string, params []any) any {
				switch method {
				case "eth_chainId":
					if scenario == "wrong network" {
						return "0x1"
					}
					return "0x38"
				case "eth_getBlockByNumber":
					if params[0] == "finalized" {
						height := "0x65"
						if scenario == "unfinalized" {
							height = "0x64"
						}
						return map[string]string{"number": height, "hash": blockHash, "timestamp": fmt.Sprintf("0x%x", now)}
					}
					hash := blockHash
					if scenario == "noncanonical" {
						hash = "0x" + strings.Repeat("c", 64)
					}
					return map[string]string{"number": "0x65", "hash": hash, "timestamp": fmt.Sprintf("0x%x", stamp)}
				case "eth_getLogs":
					logCalls++
					if scenario == "expired unpaid" {
						return []any{}
					}
					assert.Equal(t, "0x65", params[0].(map[string]any)["fromBlock"])
					if scenario == "query failure" {
						return nil
					}
					return []any{entry}
				case "eth_getTransactionReceipt":
					status := "0x1"
					if scenario == "failed receipt" {
						status = "0x0"
					}
					logs := []any{entry}
					if scenario == "receipt missing transfer" {
						logs = []any{}
					}
					return map[string]any{"status": status, "blockNumber": "0x65", "blockHash": blockHash, "transactionHash": txHash, "logs": logs}
				default:
					t.Fatalf("unexpected method %s", method)
					return nil
				}
			})
			if scenario == "valid" {
				body := fmt.Sprintf(`{"trade_no":%q}`, order.TradeNo)
				assert.Equal(t, "success", usdtRequest(t, CheckUsdtPayment, 1, body)["message"])
				assert.Equal(t, "success", usdtRequest(t, CheckUsdtPayment, 1, body)["message"])
			} else {
				err = service.CheckUsdtOrder(context.Background(), order)
			}
			stored := model.GetTopUpByTradeNo(order.TradeNo)
			switch scenario {
			case "valid", "delayed confirmation":
				require.NoError(t, err)
				assert.Equal(t, "success", stored.Status)
				require.NotNil(t, stored.UsdtEventKey)
			case "expired unpaid":
				require.NoError(t, err)
				assert.Equal(t, "expired", stored.Status)
				assert.True(t, stored.UsdtScanComplete)
				pending, err := model.GetPendingUsdtOrders()
				require.NoError(t, err)
				assert.Empty(t, pending, "fully checked expired BSC orders leave the monitor queue")
			case "wrong network", "query failure", "failed receipt", "receipt missing transfer", "noncanonical":
				require.Error(t, err)
				assert.Equal(t, int64(100), stored.UsdtScanBlock)
				assert.Equal(t, "pending", stored.Status)
			default:
				require.NoError(t, err)
				assert.Equal(t, "pending", stored.Status)
			}
			if scenario == "wrong amount" {
				assert.Equal(t, int64(101), stored.UsdtScanBlock)
				require.NoError(t, service.CheckUsdtOrder(context.Background(), stored))
				assert.Equal(t, 1, logCalls, "persisted cursor prevents repeating an already checked range")
			}
		})
	}
}

func TestUsdtBscMigrationPreservesTronReservationsAndClaims(t *testing.T) {
	usdtFixture(t)
	pending := newUsdtOrder(t)
	paidResult := usdtRequest(t, RequestUsdtPay, 1, `{"amount":20}`)["data"].(map[string]any)
	paidTrade := paidResult["trade_no"].(string)
	oldHash := strings.Repeat("7", 64)
	require.NoError(t, model.RechargeUsdt(paidTrade, oldHash))
	require.NoError(t, model.DB.Model(pending).Update("usdt_network", "").Error)
	for _, field := range []string{"UsdtNetwork", "UsdtContract", "UsdtDecimals", "UsdtStartBlock", "UsdtScanBlock", "UsdtScanComplete", "UsdtEventKey"} {
		require.NoError(t, model.DB.Migrator().DropColumn(&model.TopUp{}, field))
	}
	statement := &gorm.Statement{DB: model.DB}
	require.NoError(t, statement.Parse(&model.TopUp{}))
	legacyAddress := struct {
		UsdtAddress string `gorm:"type:varchar(34)"`
	}{}
	require.NoError(t, model.DB.Table(statement.Schema.Table).Migrator().AlterColumn(&legacyAddress, "UsdtAddress"))
	for range 2 {
		require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}))
	}
	restored := model.GetTopUpByTradeNo(pending.TradeNo)
	assert.Equal(t, pending.Money, restored.Money)
	require.NotNil(t, restored.UsdtAmountKey)
	assert.Equal(t, *pending.UsdtAmountKey, *restored.UsdtAmountKey)
	assert.Equal(t, oldHash, model.GetTopUpByTradeNo(paidTrade).TxHash)
	require.Error(t, model.RechargeUsdt(pending.TradeNo, oldHash), "legacy claimed TRON transfers remain protected after migration")
	require.NoError(t, model.RechargeUsdt(pending.TradeNo, strings.Repeat("3", 64)))
	setting.UsdtBscRpcURL = "https://example.com"
	order, err := model.CreateUsdtOrder(&model.TopUp{UserId: 1, Amount: 10, TradeNo: "bsc-migration", PaymentMethod: "usdt", PaymentProvider: "usdt", Status: "pending", CreateTime: time.Now().Unix(), ExpireTime: time.Now().Unix() + 1800, UsdtAddress: "0x" + strings.Repeat("1", 40), UsdtNetwork: "BSC", UsdtStartBlock: 100})
	require.NoError(t, err)
	require.NoError(t, model.RechargeUsdt(order.TradeNo, strings.Repeat("4", 64), 2))
	duplicate := *model.GetTopUpByTradeNo(order.TradeNo)
	duplicate.Id = 0
	duplicate.TradeNo = "duplicate-event"
	require.Error(t, model.DB.Create(&duplicate).Error, "database enforces unique BSC transfer claims")
}
