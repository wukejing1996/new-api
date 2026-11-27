package controller

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUsdtEthConfigurationAndPublishedNetwork(t *testing.T) {
	usdtFixture(t)
	require.Equal(t, "error", usdtRequest(t, RequestUsdtPay, 1, `{"amount":10,"network":"ETH"}`)["message"])
	require.Error(t, model.UpdateOption("UsdtEthEnabled", "true"))
	require.Error(t, model.UpdateOption("UsdtEthRpcURL", "http://example.com"))
	require.Error(t, model.UpdateOption("UsdtEthReceiveAddress", "0x"+strings.Repeat("0", 40)))
	require.NoError(t, model.UpdateOption("UsdtEthReceiveAddress", "0x"+strings.Repeat("A", 40)))
	endpoint := "https://example.com/eth-config"
	require.NoError(t, model.UpdateOption("UsdtEthRpcURL", endpoint))

	chainID, precision, finalizedSupported := "0x1", "0x6", false
	bscFixture(t, func(method string, params []any) any {
		switch method {
		case "eth_chainId":
			return chainID
		case "eth_getCode":
			assert.Equal(t, setting.EthUsdtContract, params[0])
			return "0x1234"
		case "eth_call":
			return precision
		case "eth_getBlockByNumber":
			if params[0] == "finalized" && !finalizedSupported {
				return nil
			}
			return map[string]string{"number": "0x64", "hash": "0x" + strings.Repeat("a", 64), "timestamp": fmt.Sprintf("0x%x", time.Now().Unix())}
		default:
			t.Fatalf("unexpected method %s", method)
			return nil
		}
	})
	require.NoError(t, service.ValidateEthProvider(context.Background(), endpoint))
	chainID = "0x38"
	require.Error(t, service.ValidateEthProvider(context.Background(), endpoint))
	chainID, precision = "0x1", "0x12"
	require.Error(t, service.ValidateEthProvider(context.Background(), endpoint))
	precision, finalizedSupported = "0x6", false
	require.NoError(t, service.ValidateEthProvider(context.Background(), endpoint))

	require.NoError(t, model.UpdateOption("UsdtEthEnabled", "true"))
	info := usdtRequest(t, GetTopUpInfo, 1, "{}")["data"].(map[string]any)
	assert.Equal(t, []any{"TRC20", "ETH"}, info["usdt_networks"])
	order := newEthUsdtOrder(t, 1)
	assert.Equal(t, order.TradeNo, newEthUsdtOrder(t, 1).TradeNo)
	assert.Equal(t, setting.EthUsdtContract, order.UsdtContract)
	assert.Equal(t, 6, order.UsdtDecimals)
	assert.Equal(t, "0x"+strings.Repeat("a", 40), order.UsdtAddress)
	assert.NotEqual(t, order.Money, newUsdtOrder(t).Money)
	require.NoError(t, model.UpdateOption("UsdtEthEnabled", "false"))
	require.Equal(t, "error", usdtRequest(t, RequestUsdtPay, 1, `{"amount":10,"network":"ETH"}`)["message"])
}

func newEthUsdtOrder(t *testing.T, user int) *model.TopUp {
	t.Helper()
	result := usdtRequest(t, RequestUsdtPay, user, `{"amount":10,"network":"ETH"}`)
	require.Equal(t, "success", result["message"])
	order := model.GetTopUpByTradeNo(result["data"].(map[string]any)["trade_no"].(string))
	require.NotNil(t, order)
	return order
}

func TestUsdtEthReceiptVerification(t *testing.T) {
	for _, scenario := range []struct{ name, result string }{
		{"valid after order expiration", "success"},
		{"pending receipt", "pending"},
		{"canonical before finalized", "success"},
		{"noncanonical", "pending"},
		{"wrong chain", "error"},
		{"wrong recipient", "error"},
		{"wrong amount", "error"},
		{"wrong token", "error"},
		{"failed receipt", "error"},
		{"ambiguous transfers", "error"},
		{"before reservation", "expired"},
		{"at expiration", "expired"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			usdtFixture(t)
			setting.UsdtEthEnabled, setting.UsdtEthReceiveAddress = true, "0x"+strings.Repeat("a", 40)
			setting.UsdtEthRpcURL = "https://example.com/" + t.Name()
			order := newEthUsdtOrder(t, 1)
			now := time.Now().Unix()
			require.NoError(t, model.DB.Model(order).Updates(map[string]any{
				"create_time": now - 120, "usdt_start_time": (now-120)*1000 + 100, "expire_time": now - 30,
			}).Error)
			require.NoError(t, model.ExpireElapsedUsdtOrders(now))
			require.NoError(t, model.DB.Create(&model.User{Id: 2, Username: "another-payer", AffCode: "eth2"}).Error)
			require.NoError(t, model.UpdateOption("UsdtEthEnabled", "false"))
			require.NoError(t, model.UpdateOption("UsdtEthReceiveAddress", "0x"+strings.Repeat("f", 40)))
			hash := "0x" + strings.Repeat("c", 64)
			blockHash := "0x" + strings.Repeat("b", 64)
			body := fmt.Sprintf(`{"trade_no":%q,"tx_hash":%q}`, order.TradeNo, hash)
			stamp := now - 60
			receiptStatus, chainID, finalized := "0x1", "0x1", "0x66"
			entry := map[string]any{
				"address":     setting.EthUsdtContract,
				"topics":      []string{testBscTransferTopic, "0x" + strings.Repeat("0", 64), "0x" + strings.Repeat("0", 24) + order.UsdtAddress[2:]},
				"data":        fmt.Sprintf("0x%064x", decimal.NewFromFloat(order.Money).Shift(6).BigInt()),
				"blockNumber": "0x65", "blockHash": blockHash, "transactionHash": hash,
				"logIndex": "0x1",
			}
			logs := []any{entry}
			switch scenario.name {
			case "canonical before finalized":
				finalized = "0x64"
			case "wrong chain":
				chainID = "0x38"
			case "wrong recipient":
				entry["topics"].([]string)[2] = "0x" + strings.Repeat("0", 64)
			case "wrong amount":
				entry["data"] = fmt.Sprintf("0x%064x", decimal.NewFromFloat(order.Money).Shift(6).IntPart()+1)
			case "wrong token":
				entry["address"] = setting.BscUsdtContract
			case "failed receipt":
				receiptStatus = "0x0"
			case "ambiguous transfers":
				logs = append(logs, entry)
			case "before reservation":
				stamp = now - 120
			case "at expiration":
				stamp = now - 30
			}
			var calls []string
			bscFixture(t, func(method string, params []any) any {
				calls = append(calls, method)
				switch method {
				case "eth_chainId":
					return chainID
				case "eth_getTransactionReceipt":
					require.Equal(t, []any{hash}, params)
					if scenario.name == "pending receipt" {
						return nil
					}
					return map[string]any{"transactionHash": hash, "status": receiptStatus, "blockNumber": "0x65", "blockHash": blockHash, "logs": logs}
				case "eth_getBlockByNumber":
					if params[0] == "finalized" {
						return map[string]string{"number": finalized, "hash": "0x" + strings.Repeat("e", 64), "timestamp": fmt.Sprintf("0x%x", now)}
					}
					require.Equal(t, []any{"0x65", false}, params)
					canonical := blockHash
					if scenario.name == "noncanonical" {
						canonical = "0x" + strings.Repeat("d", 64)
					}
					return map[string]string{"number": "0x65", "hash": canonical, "timestamp": fmt.Sprintf("0x%x", stamp)}
				default:
					t.Fatalf("unexpected RPC method %s", method)
					return nil
				}
			})
			assert.Equal(t, "error", usdtRequest(t, CheckEthUsdtPayment, 2, body)["message"])
			assert.Equal(t, "error", usdtRequest(t, CheckBscUsdtPayment, 1, body)["message"])
			assert.Equal(t, "error", usdtRequest(t, CheckUsdtPayment, 1, body)["message"])
			require.Error(t, model.ManualCompleteTopUp(order.TradeNo, ""))
			assert.Empty(t, calls)
			assert.Equal(t, scenario.result, usdtRequest(t, CheckEthUsdtPayment, 1, body)["message"])
			require.NotEmpty(t, calls)
			assert.Equal(t, "eth_chainId", calls[0])
			if len(calls) > 1 {
				assert.Equal(t, "eth_getTransactionReceipt", calls[1])
			}
			if scenario.name == "pending receipt" || scenario.name == "wrong amount" || scenario.name == "failed receipt" {
				assert.Len(t, calls, 2)
			}
			stored := model.GetTopUpByTradeNo(order.TradeNo)
			var user model.User
			require.NoError(t, model.DB.First(&user, 1).Error)
			if scenario.result == "success" {
				callCount := len(calls)
				assert.Equal(t, "success", usdtRequest(t, CheckEthUsdtPayment, 1, body)["message"])
				assert.Len(t, calls, callCount)
				assert.Equal(t, common.TopUpStatusSuccess, stored.Status)
				require.NotNil(t, stored.UsdtEventKey)
				assert.Equal(t, "1:"+hash[2:]+":1", *stored.UsdtEventKey)
				assert.Equal(t, order.UsdtQuota, user.Quota)
				other := newUsdtOrder(t)
				require.ErrorIs(t, model.RechargeUsdt(other.TradeNo, hash[2:]), model.ErrUsdtTransactionAlreadyCredited)
			} else {
				assert.Equal(t, "expired", stored.Status)
				assert.Nil(t, stored.UsdtTxHash)
				assert.Zero(t, user.Quota)
			}
		})
	}
}

func TestUsdtEthConcurrentCreditAndRollback(t *testing.T) {
	usdtFixture(t)
	setting.UsdtEthEnabled, setting.UsdtEthReceiveAddress = true, "0x"+strings.Repeat("a", 40)
	setting.UsdtEthRpcURL = "https://example.com/eth"
	first := newEthUsdtOrder(t, 1)
	hash := strings.Repeat("b", 64)
	block := model.EthUsdtBlockProof{Number: 100, Hash: "0x" + strings.Repeat("c", 64)}
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = 1").Update("quota", common.MaxWalletQuota).Error)
	require.Error(t, model.RechargeEthUsdt(first.TradeNo, hash, 0, block))
	unchanged := model.GetTopUpByTradeNo(first.TradeNo)
	assert.Equal(t, common.TopUpStatusPending, unchanged.Status)
	assert.Nil(t, unchanged.UsdtTxHash)
	assert.NotNil(t, unchanged.UsdtAmountKey)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = 1").Update("quota", 0).Error)

	var checks sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		checks.Go(func() { results <- model.RechargeEthUsdt(first.TradeNo, hash, 0, block) })
	}
	checks.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	require.NoError(t, model.DB.Create(&model.User{Id: 2, Username: "other-payer", AffCode: "eth2"}).Error)
	second := newEthUsdtOrder(t, 2)
	require.ErrorIs(t, model.RechargeEthUsdt(second.TradeNo, hash, 1, block), model.ErrUsdtTransactionAlreadyCredited)
	assert.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(second.TradeNo).Status)
	var firstUser, secondUser model.User
	require.NoError(t, model.DB.First(&firstUser, 1).Error)
	require.NoError(t, model.DB.First(&secondUser, 2).Error)
	assert.Equal(t, first.UsdtQuota, firstUser.Quota)
	assert.Zero(t, secondUser.Quota)
}
