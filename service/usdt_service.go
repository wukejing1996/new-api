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
	params := url.Values{
		"only_to": {"true"}, "only_confirmed": {"true"}, "limit": {"200"},
		"contract_address": {usdtContract}, "min_timestamp": {strconv.FormatInt(minTimestamp, 10)},
		"max_timestamp": {strconv.FormatInt(maxTimestamp, 10)}, "order_by": {"block_timestamp,asc"},
	}
	apiKey := setting.GetUsdtConfig().APIKey
	for range 100 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.trongrid.io/v1/accounts/"+address+"/transactions/trc20?"+params.Encode(), nil)
		if err != nil {
			return nil, err
		}
		if apiKey != "" {
			req.Header.Set("TRON-PRO-API-KEY", apiKey)
		}
		resp, err := usdtHTTPClient.Do(req)
		if err != nil {
			return nil, err
		}
		var result TrongridResponse
		err = common.DecodeJson(io.LimitReader(resp.Body, 2<<20), &result)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("TronGrid returned %d", resp.StatusCode)
		}
		if err != nil {
			return nil, err
		}
		if !result.Success {
			return nil, errors.New("TronGrid query unsuccessful")
		}
		for _, transfer := range result.Data {
			if transfer.To != address || transfer.TokenInfo.Address != usdtContract || transfer.TokenInfo.Decimals != 6 || transfer.Type != "Transfer" {
				continue
			}
			if transfer.BlockTimestamp < minTimestamp || transfer.BlockTimestamp > maxTimestamp {
				continue
			}
			value, err := strconv.ParseInt(transfer.Value, 10, 64)
			if err != nil || value <= 0 || strconv.FormatInt(value, 10) != transfer.Value || value != target.IntPart() {
				continue
			}
			hash, err := hex.DecodeString(transfer.TransactionID)
			if err != nil || len(hash) != 32 {
				continue
			}
			transfer.TransactionID = hex.EncodeToString(hash)
			return &transfer, nil
		}
		if result.Meta.Fingerprint == "" {
			return nil, nil
		}
		if result.Meta.Fingerprint == params.Get("fingerprint") {
			return nil, errors.New("TronGrid pagination did not advance")
		}
		params.Set("fingerprint", result.Meta.Fingerprint)
	}
	return nil, errors.New("TronGrid pagination limit reached; retry reconciliation")
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
	startTime := order.UsdtStartTime
	if startTime == 0 {
		startTime = order.CreateTime * 1000
	}
	transfer, err := FindMatchingTransaction(ctx, order.UsdtAddress, order.Money, startTime, order.ExpireTime*1000-1)
	if err != nil {
		return err
	}
	if transfer != nil {
		return model.RechargeUsdt(order.TradeNo, transfer.TransactionID)
	}
	return nil
}
