package service

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/shopspring/decimal"
)

const bscTransferTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

var (
	errEvmRPCNoResult    = errors.New("RPC result is unavailable")
	errEvmRPCRateLimited = errors.New("RPC returned HTTP 429")
	errEvmRPCUnavailable = errors.New("RPC is temporarily unavailable")
)

type bscBlock struct {
	Number    string `json:"number"`
	Hash      string `json:"hash"`
	Timestamp string `json:"timestamp"`
}

type bscLog struct {
	Address         string   `json:"address"`
	Topics          []string `json:"topics"`
	Data            string   `json:"data"`
	BlockNumber     string   `json:"blockNumber"`
	BlockHash       string   `json:"blockHash"`
	TransactionHash string   `json:"transactionHash"`
	LogIndex        string   `json:"logIndex"`
	Removed         bool     `json:"removed"`
}

type bscReceipt struct {
	Status          string   `json:"status"`
	TransactionHash string   `json:"transactionHash"`
	BlockHash       string   `json:"blockHash"`
	BlockNumber     string   `json:"blockNumber"`
	Logs            []bscLog `json:"logs"`
}

func bscRPC(ctx context.Context, endpoint, method string, params any, result any) error {
	return evmRPC(ctx, "BSC", endpoint, method, params, result)
}

func evmRPC(ctx context.Context, network, endpoint, method string, params any, result any) error {
	if !setting.ValidBscRPCURL(endpoint) {
		return fmt.Errorf("configure a %s HTTPS RPC URL", network)
	}
	body, err := common.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("invalid %s RPC request", network)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := usdtHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %w", network, errEvmRPCUnavailable)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("%s %w", network, errEvmRPCRateLimited)
	}
	if resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode >= http.StatusInternalServerError {
		return fmt.Errorf("%s %w (HTTP %d)", network, errEvmRPCUnavailable, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s RPC returned HTTP %d", network, resp.StatusCode)
	}
	var envelope struct {
		Version string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := common.DecodeJson(io.LimitReader(resp.Body, 4<<20), &envelope); err != nil {
		return fmt.Errorf("invalid %s RPC response", network)
	}
	if envelope.Error != nil {
		if envelope.Error.Code == http.StatusTooManyRequests || envelope.Error.Code == -32005 || strings.Contains(strings.ToLower(envelope.Error.Message), "rate limit") {
			return fmt.Errorf("%s %w", network, errEvmRPCRateLimited)
		}
		return fmt.Errorf("%s RPC %s failed (%d)", network, method, envelope.Error.Code)
	}
	if envelope.ID != 1 || envelope.Version != "2.0" || len(envelope.Result) == 0 {
		return fmt.Errorf("invalid %s RPC response", network)
	}
	if string(envelope.Result) == "null" {
		return fmt.Errorf("%s %w", network, errEvmRPCNoResult)
	}
	if err := common.Unmarshal(envelope.Result, result); err != nil {
		return fmt.Errorf("invalid %s RPC result", network)
	}
	return nil
}

func bscQuantity(value string) (int64, error) {
	if !strings.HasPrefix(value, "0x") || len(value) <= 2 {
		return 0, errors.New("invalid BSC quantity")
	}
	n, err := strconv.ParseInt(value[2:], 16, 64)
	if err != nil || n < 0 {
		return 0, errors.New("invalid BSC quantity")
	}
	return n, nil
}

func bscHash(value string) bool {
	if len(value) != 66 || !strings.HasPrefix(value, "0x") {
		return false
	}
	_, err := hex.DecodeString(value[2:])
	return err == nil
}

func bscHeader(ctx context.Context, endpoint, tag string) (bscBlock, int64, int64, error) {
	return evmHeader(ctx, "BSC", endpoint, tag)
}

func evmHeader(ctx context.Context, network, endpoint, tag string) (bscBlock, int64, int64, error) {
	var block bscBlock
	if err := evmRPC(ctx, network, endpoint, "eth_getBlockByNumber", []any{tag, false}, &block); err != nil {
		return block, 0, 0, err
	}
	number, err := bscQuantity(block.Number)
	stamp, stampErr := bscQuantity(block.Timestamp)
	if err != nil || stampErr != nil || stamp <= 0 || !bscHash(block.Hash) {
		return block, 0, 0, fmt.Errorf("invalid %s block", network)
	}
	if strings.HasPrefix(tag, "0x") {
		requested, err := bscQuantity(tag)
		if err != nil || requested != number {
			return block, 0, 0, fmt.Errorf("%s returned the wrong block", network)
		}
	}
	return block, number, stamp, nil
}

// URL credentials never appear in returned errors or public configuration.
func ValidateBscProvider(ctx context.Context, endpoint string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var chain, precision string
	if err := bscRPC(ctx, endpoint, "eth_chainId", []any{}, &chain); err != nil {
		return 0, err
	}
	if n, err := bscQuantity(chain); err != nil || n != 56 {
		return 0, errors.New("RPC must use BSC mainnet (56)")
	}
	if err := bscRPC(ctx, endpoint, "eth_call", []any{map[string]string{"to": setting.BscUsdtContract, "data": "0x313ce567"}, "latest"}, &precision); err != nil {
		return 0, err
	}
	if n, err := bscQuantity(precision); err != nil || n != 18 {
		return 0, errors.New("unexpected BSC USDT precision")
	}
	_, latest, stamp, err := bscHeader(ctx, endpoint, "latest")
	if err != nil {
		return 0, err
	}
	if latest <= 0 || stamp < time.Now().Unix()-120 || stamp > time.Now().Unix()+30 {
		return 0, errors.New("BSC RPC is not synchronized")
	}
	return latest, nil
}

func CheckBscUsdtPayment(ctx context.Context, order *model.TopUp, txHash string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if order.UsdtNetwork != "BSC" || !setting.ValidBscAddress(order.UsdtAddress) || order.UsdtContract != setting.BscUsdtContract || order.UsdtDecimals != 18 {
		return errors.New("invalid BSC order snapshot")
	}
	hash := strings.ToLower(strings.TrimSpace(txHash))
	hash = strings.TrimPrefix(hash, "0x")
	if !bscHash("0x" + hash) {
		return errors.New("invalid BSC transaction hash")
	}
	if order.Status == common.TopUpStatusSuccess {
		if order.UsdtTxHash != nil && *order.UsdtTxHash == hash {
			return nil
		}
		return model.ErrTopUpStatusInvalid
	}
	if order.Status == "expired" {
		return model.ErrUsdtOrderExpired
	}
	if order.Status != common.TopUpStatusPending {
		return model.ErrTopUpStatusInvalid
	}
	if time.Now().Unix() >= order.ExpireTime {
		if err := model.ExpireUsdtOrder(order.Id); err != nil {
			return err
		}
		return model.ErrUsdtOrderExpired
	}
	endpoint := setting.GetUsdtNetworkConfig("BSC").RPCURL
	var chain string
	if err := bscRPC(ctx, endpoint, "eth_chainId", []any{}, &chain); err != nil {
		return err
	}
	if n, err := bscQuantity(chain); err != nil || n != 56 {
		return errors.New("RPC must use BSC mainnet (56)")
	}
	var receipt bscReceipt
	canonicalHash := "0x" + hash
	if err := bscRPC(ctx, endpoint, "eth_getTransactionReceipt", []any{canonicalHash}, &receipt); err != nil {
		return err
	}
	if receipt.Status != "0x1" || !strings.EqualFold(receipt.TransactionHash, canonicalHash) || !bscHash(receipt.BlockHash) || receipt.BlockNumber == "" {
		return errors.New("BSC transaction receipt is not successful")
	}
	block, _, stamp, err := bscHeader(ctx, endpoint, receipt.BlockNumber)
	if err != nil {
		return err
	}
	if !strings.EqualFold(block.Hash, receipt.BlockHash) {
		return errors.New("BSC transaction is not canonical")
	}
	if stamp < order.CreateTime || stamp >= order.ExpireTime {
		return errors.New("BSC transfer is outside the order window")
	}
	target := decimal.NewFromFloat(order.Money).Shift(18)
	if target.Sign() <= 0 || !target.Equal(target.Truncate(0)) {
		return errors.New("invalid BSC amount")
	}
	expected := "0x" + strings.Repeat("0", 24) + strings.ToLower(order.UsdtAddress[2:])
	var amountMismatch bool
	var logIndex uint64
	for _, entry := range receipt.Logs {
		if entry.Removed || !strings.EqualFold(entry.Address, order.UsdtContract) || len(entry.Topics) != 3 || !strings.EqualFold(entry.Topics[0], bscTransferTopic) || !bscHash(entry.Topics[1]) || !strings.EqualFold(entry.Topics[2], expected) || len(entry.Data) != 66 || !strings.HasPrefix(entry.Data, "0x") {
			continue
		}
		if !strings.EqualFold(entry.TransactionHash, canonicalHash) || !strings.EqualFold(entry.BlockHash, receipt.BlockHash) || entry.BlockNumber != receipt.BlockNumber || !bscHash(entry.TransactionHash) || !bscHash(entry.BlockHash) {
			return errors.New("invalid BSC transfer proof")
		}
		value, ok := new(big.Int).SetString(entry.Data[2:], 16)
		if !ok {
			continue
		}
		if value.Cmp(target.BigInt()) != 0 {
			amountMismatch = true
			continue
		}
		index, err := bscQuantity(entry.LogIndex)
		if err != nil || index < 0 {
			return errors.New("invalid BSC transfer log index")
		}
		logIndex = uint64(index)
		return model.RechargeUsdt(order.TradeNo, hash, logIndex)
	}
	if amountMismatch {
		return errors.New("BSC transfer amount does not match the order")
	}
	return errors.New("BSC receipt does not contain the matching USDT transfer")
}
