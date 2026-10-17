package service

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/shopspring/decimal"
)

const usdtContract = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"

type TRC20Transaction struct {
	TransactionID  string    `json:"transaction_id"`
	TokenInfo      TokenInfo `json:"token_info"`
	From           string    `json:"from"`
	To             string    `json:"to"`
	Type           string    `json:"type"`
	Value          string    `json:"value"`
	BlockTimestamp int64     `json:"block_timestamp"`
}
type TokenInfo struct {
	Address  string `json:"address"`
	Decimals int    `json:"decimals"`
}
type TrongridResponse struct {
	Data    []TRC20Transaction `json:"data"`
	Success bool               `json:"success"`
	Meta    struct {
		Fingerprint string `json:"fingerprint"`
	} `json:"meta"`
}

var usdtHTTPClient = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}

func FindMatchingTransaction(ctx context.Context, address string, amount float64, minTimestamp, maxTimestamp int64) (*TRC20Transaction, error) {
	if !setting.ValidTronAddress(address) || math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 || amount > 1000001 || maxTimestamp < minTimestamp {
		return nil, errors.New("invalid USDT order")
	}
	target := decimal.NewFromFloat(amount).Mul(decimal.NewFromInt(1000000))
	if !target.Equal(target.Truncate(0)) {
		return nil, errors.New("invalid USDT precision")
	}
	var found *TRC20Transaction
	err := scanTrc20Transactions(ctx, address, minTimestamp, maxTimestamp, func(transfer *TRC20Transaction) (bool, error) {
		if transfer.Value != target.StringFixed(0) {
			return false, nil
		}
		found = transfer
		return true, nil
	})
	return found, err
}

// visit can stop a scan once all needed orders are settled. It only receives
// validated USDT transfers; the caller still enforces each order's own window.
func scanTrc20Transactions(ctx context.Context, address string, minTimestamp, maxTimestamp int64, visit func(*TRC20Transaction) (bool, error)) error {
	if !setting.ValidTronAddress(address) || minTimestamp < 0 || maxTimestamp < minTimestamp {
		return errors.New("invalid TRC20 query window")
	}
	params := url.Values{
		"only_to": {"true"}, "only_confirmed": {"true"}, "limit": {"200"},
		"contract_address": {usdtContract}, "min_timestamp": {strconv.FormatInt(minTimestamp, 10)},
		"max_timestamp": {strconv.FormatInt(maxTimestamp, 10)}, "order_by": {"block_timestamp,asc"},
	}
	apiKey := setting.GetUsdtConfig().APIKey
	fingerprints := make(map[string]bool)
	for range 100 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.trongrid.io/v1/accounts/"+address+"/transactions/trc20?"+params.Encode(), nil)
		if err != nil {
			return err
		}
		if apiKey != "" {
			req.Header.Set("TRON-PRO-API-KEY", apiKey)
		}
		if err := waitForTrongrid(ctx, apiKey); err != nil {
			return err
		}
		resp, err := usdtHTTPClient.Do(req)
		if err != nil {
			if ctx.Err() == nil {
				recordTrongridResult(apiKey, 0, "", true)
			}
			return err
		}
		var result TrongridResponse
		err = common.DecodeJson(io.LimitReader(resp.Body, 2<<20), &result)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			recordTrongridResult(apiKey, resp.StatusCode, resp.Header.Get("Retry-After"), true)
			return fmt.Errorf("TronGrid returned %d", resp.StatusCode)
		}
		if err != nil {
			recordTrongridResult(apiKey, resp.StatusCode, "", true)
			return err
		}
		if !result.Success {
			recordTrongridResult(apiKey, resp.StatusCode, "", true)
			return errors.New("TronGrid query unsuccessful")
		}
		recordTrongridResult(apiKey, resp.StatusCode, "", false)
		for _, transfer := range result.Data {
			if transfer.To != address || transfer.TokenInfo.Address != usdtContract || transfer.TokenInfo.Decimals != 6 || transfer.Type != "Transfer" {
				continue
			}
			if transfer.BlockTimestamp < minTimestamp || transfer.BlockTimestamp > maxTimestamp {
				continue
			}
			value, err := strconv.ParseInt(transfer.Value, 10, 64)
			if err != nil || value <= 0 || strconv.FormatInt(value, 10) != transfer.Value {
				continue
			}
			hash, err := hex.DecodeString(transfer.TransactionID)
			if err != nil || len(hash) != 32 {
				continue
			}
			transfer.TransactionID = hex.EncodeToString(hash)
			stop, err := visit(&transfer)
			if err != nil || stop {
				return err
			}
		}
		if result.Meta.Fingerprint == "" {
			return nil
		}
		if fingerprints[result.Meta.Fingerprint] {
			return errors.New("TronGrid pagination did not advance")
		}
		fingerprints[result.Meta.Fingerprint] = true
		params.Set("fingerprint", result.Meta.Fingerprint)
	}
	return errors.New("TronGrid pagination limit reached; retry reconciliation")
}

// Both manual checks and the monitor use the immutable order snapshot.
func CheckUsdtOrder(ctx context.Context, order *model.TopUp) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if order.UsdtQuota <= 0 || order.UsdtAddress == "" {
		return errors.New("legacy USDT order requires manual reconciliation")
	}
	if order.Status == common.TopUpStatusSuccess {
		return nil
	}
	if order.Status != common.TopUpStatusPending && order.Status != "expired" {
		return model.ErrTopUpStatusInvalid
	}
	if time.Now().Unix() >= order.ExpireTime {
		if err := model.ExpireUsdtOrder(order.Id); err != nil {
			return err
		}
	}
	if order.UsdtNetwork == "BSC" {
		return CheckBscUsdtOrder(ctx, order)
	}
	return reconcileTrc20Orders(ctx, []*model.TopUp{order}, time.Now().UnixMilli())
}

// Rescan the full eligible window every time: a timestamp cursor would skip
// transfers that were not confirmed or not indexed in an earlier response.
func reconcileTrc20Orders(ctx context.Context, orders []*model.TopUp, nowMillis int64) error {
	byAmount := make(map[string][]*model.TopUp)
	starts := make(map[int]int64)
	remaining := make(map[int]bool)
	minTimestamp, maxTimestamp := nowMillis, int64(0)
	var failures []error
	address := orders[0].UsdtAddress
	for _, order := range orders {
		if math.IsNaN(order.Money) || math.IsInf(order.Money, 0) {
			failures = append(failures, fmt.Errorf("%s: invalid USDT amount", order.TradeNo))
			continue
		}
		start := order.UsdtStartTime
		if start == 0 {
			start = order.CreateTime * 1000
		}
		end := min(order.ExpireTime*1000-1, nowMillis)
		target := decimal.NewFromFloat(order.Money).Shift(6)
		if order.UsdtQuota <= 0 || !setting.ValidTronAddress(order.UsdtAddress) || order.UsdtAddress != address ||
			(order.UsdtNetwork != "" && order.UsdtNetwork != "TRC20") ||
			(order.UsdtContract != "" && (order.UsdtContract != setting.TronUsdtContract || order.UsdtDecimals != 6)) ||
			(order.Status != common.TopUpStatusPending && order.Status != "expired") ||
			start <= 0 || end < start || target.Sign() <= 0 || target.GreaterThan(decimal.NewFromInt(1000001000000)) || !target.Equal(target.Truncate(0)) {
			failures = append(failures, fmt.Errorf("%s: invalid TRC20 order snapshot", order.TradeNo))
			continue
		}
		key := target.StringFixed(0)
		byAmount[key] = append(byAmount[key], order)
		starts[order.Id], remaining[order.Id] = start, true
		minTimestamp, maxTimestamp = min(minTimestamp, start), max(maxTimestamp, end)
	}
	if len(remaining) == 0 {
		return errors.Join(failures...)
	}
	err := scanTrc20Transactions(ctx, address, minTimestamp, maxTimestamp, func(transfer *TRC20Transaction) (bool, error) {
		for _, order := range byAmount[transfer.Value] {
			if !remaining[order.Id] || transfer.BlockTimestamp < starts[order.Id] || transfer.BlockTimestamp >= order.ExpireTime*1000 {
				continue
			}
			err := model.RechargeUsdt(order.TradeNo, transfer.TransactionID)
			if errors.Is(err, model.ErrUsdtTransactionAlreadyCredited) {
				// Do not stop scanning: a later transfer can belong to this order.
				break
			}
			delete(remaining, order.Id)
			if err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", order.TradeNo, err))
			}
			break
		}
		return len(remaining) == 0, nil
	})
	return errors.Join(append(failures, err)...)
}
