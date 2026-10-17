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

func bscRPC(ctx context.Context, endpoint, method string, params any, result any) error {
	if !setting.ValidBscRPCURL(endpoint) {
		return errors.New("configure a BSC HTTPS RPC URL")
	}
	body, err := common.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("invalid BSC RPC request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := usdtHTTPClient.Do(req)
	if err != nil {
		return errors.New("BSC RPC connection failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("BSC RPC returned HTTP %d", resp.StatusCode)
	}
	var envelope struct {
		Version string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := common.DecodeJson(io.LimitReader(resp.Body, 4<<20), &envelope); err != nil {
		return errors.New("invalid BSC RPC response")
	}
	if envelope.Error != nil {
		return fmt.Errorf("BSC RPC %s failed (%d)", method, envelope.Error.Code)
	}
	if envelope.ID != 1 || envelope.Version != "2.0" || len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return errors.New("BSC RPC result is unavailable")
	}
	return common.Unmarshal(envelope.Result, result)
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
	var block bscBlock
	if err := bscRPC(ctx, endpoint, "eth_getBlockByNumber", []any{tag, false}, &block); err != nil {
		return block, 0, 0, err
	}
	number, err := bscQuantity(block.Number)
	stamp, stampErr := bscQuantity(block.Timestamp)
	if err != nil || stampErr != nil || stamp <= 0 || !bscHash(block.Hash) {
		return block, 0, 0, errors.New("invalid BSC block")
	}
	if strings.HasPrefix(tag, "0x") {
		requested, err := bscQuantity(tag)
		if err != nil || requested != number {
			return block, 0, 0, errors.New("BSC returned the wrong block")
		}
	}
	return block, number, stamp, nil
}

// The configured endpoint must support historical logs and finalized blocks.
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
	finalized, height, _, err := bscHeader(ctx, endpoint, "finalized")
	if err != nil {
		return 0, err
	}
	if height > latest {
		return 0, errors.New("invalid BSC finalized block")
	}
	var logs []bscLog
	if err := bscRPC(ctx, endpoint, "eth_getLogs", []any{map[string]any{"address": setting.BscUsdtContract, "fromBlock": finalized.Number, "toBlock": finalized.Number, "topics": []any{bscTransferTopic}}}, &logs); err != nil {
		return 0, err
	}
	return latest, nil
}

// Each successful range persists its cursor so retries and restarts resume safely.
// Expired orders remain eligible until all blocks in their original window are final.
func CheckBscUsdtOrder(ctx context.Context, order *model.TopUp) error {
	if !setting.ValidBscAddress(order.UsdtAddress) || order.UsdtContract != setting.BscUsdtContract || order.UsdtDecimals != 18 || order.UsdtStartBlock <= 0 || order.UsdtScanBlock < order.UsdtStartBlock {
		return errors.New("invalid BSC order snapshot")
	}
	if order.UsdtScanComplete {
		return nil
	}
	endpoint := setting.GetUsdtNetworkConfig("BSC").RPCURL
	var chain string
	if err := bscRPC(ctx, endpoint, "eth_chainId", []any{}, &chain); err != nil {
		return err
	}
	if n, err := bscQuantity(chain); err != nil || n != 56 {
		return errors.New("RPC must use BSC mainnet (56)")
	}
	_, finalHeight, finalTime, err := bscHeader(ctx, endpoint, "finalized")
	if err != nil {
		return err
	}
	if finalTime > time.Now().Unix()+30 {
		return errors.New("invalid BSC finalized timestamp")
	}
	target := decimal.NewFromFloat(order.Money).Shift(18)
	if target.Sign() <= 0 || !target.Equal(target.Truncate(0)) {
		return errors.New("invalid BSC amount")
	}
	expected := "0x" + strings.Repeat("0", 24) + strings.ToLower(order.UsdtAddress[2:])
	for range 20 {
		from := order.UsdtScanBlock + 1
		if from > finalHeight {
			return nil
		}
		to := min(from+199, finalHeight)
		var logs []bscLog
		if err := bscRPC(ctx, endpoint, "eth_getLogs", []any{map[string]any{"address": order.UsdtContract, "fromBlock": fmt.Sprintf("0x%x", from), "toBlock": fmt.Sprintf("0x%x", to), "topics": []any{bscTransferTopic, nil, expected}}}, &logs); err != nil {
			return err
		}
		for _, entry := range logs {
			if entry.Removed || !strings.EqualFold(entry.Address, order.UsdtContract) || len(entry.Topics) != 3 || !strings.EqualFold(entry.Topics[0], bscTransferTopic) || !strings.EqualFold(entry.Topics[2], expected) || !bscHash(entry.Topics[1]) || len(entry.Data) != 66 || !strings.HasPrefix(entry.Data, "0x") {
				continue
			}
			value, ok := new(big.Int).SetString(entry.Data[2:], 16)
			if !ok || value.Cmp(target.BigInt()) != 0 {
				continue
			}
			height, err := bscQuantity(entry.BlockNumber)
			index, indexErr := bscQuantity(entry.LogIndex)
			if err != nil || indexErr != nil || height < from || height > to || !bscHash(entry.TransactionHash) || !bscHash(entry.BlockHash) {
				return errors.New("invalid BSC transfer proof")
			}
			block, _, stamp, err := bscHeader(ctx, endpoint, entry.BlockNumber)
			if err != nil {
				return err
			}
			if !strings.EqualFold(block.Hash, entry.BlockHash) {
				return errors.New("BSC block changed; retry verification")
			}
			if stamp < order.CreateTime || stamp >= order.ExpireTime {
				continue
			}
			var receipt struct {
				Status          string   `json:"status"`
				TransactionHash string   `json:"transactionHash"`
				BlockHash       string   `json:"blockHash"`
				BlockNumber     string   `json:"blockNumber"`
				Logs            []bscLog `json:"logs"`
			}
			if err := bscRPC(ctx, endpoint, "eth_getTransactionReceipt", []any{entry.TransactionHash}, &receipt); err != nil {
				return err
			}
			if receipt.Status != "0x1" || !strings.EqualFold(receipt.TransactionHash, entry.TransactionHash) || !strings.EqualFold(receipt.BlockHash, entry.BlockHash) || receipt.BlockNumber != entry.BlockNumber {
				return errors.New("BSC transfer receipt is not successful or canonical")
			}
			proven := false
			for _, proof := range receipt.Logs {
				if !proof.Removed && proof.LogIndex == entry.LogIndex && strings.EqualFold(proof.TransactionHash, entry.TransactionHash) && strings.EqualFold(proof.BlockHash, entry.BlockHash) && proof.BlockNumber == entry.BlockNumber && strings.EqualFold(proof.Address, entry.Address) && strings.EqualFold(proof.Data, entry.Data) && len(proof.Topics) == 3 && strings.EqualFold(proof.Topics[0], entry.Topics[0]) && strings.EqualFold(proof.Topics[1], entry.Topics[1]) && strings.EqualFold(proof.Topics[2], entry.Topics[2]) {
					proven = true
					break
				}
			}
			if !proven {
				return errors.New("BSC receipt does not contain the transfer")
			}
			return model.RechargeUsdt(order.TradeNo, entry.TransactionHash[2:], uint64(index))
		}
		endTime := finalTime
		if to != finalHeight {
			_, _, endTime, err = bscHeader(ctx, endpoint, fmt.Sprintf("0x%x", to))
			if err != nil {
				return err
			}
		}
		complete := endTime >= order.ExpireTime
		if err := model.AdvanceBscScan(order.Id, to, complete); err != nil {
			return err
		}
		order.UsdtScanBlock = to
		if complete {
			return nil
		}
	}
	return nil
}
