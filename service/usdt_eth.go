package service

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/shopspring/decimal"
)

var ErrEthUsdtPending = errors.New("Ethereum payment not confirmed yet")

// An older finalized height can only defer a payment. A changed RPC URL must be verified again.
var ethFinalityCache struct {
	sync.Mutex
	endpoint string
	block    bscBlock
	height   int64
	stamp    int64
	readAt   time.Time
	chainAt  time.Time
}

func checkEthChain(ctx context.Context, endpoint string) error {
	var chain string
	if err := evmRPC(ctx, "Ethereum", endpoint, "eth_chainId", []any{}, &chain); err != nil {
		return err
	}
	if chain != "0x1" {
		return errors.New("RPC must use Ethereum mainnet (1)")
	}
	return nil
}

func ValidateEthProvider(ctx context.Context, endpoint string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := checkEthChain(ctx, endpoint); err != nil {
		return err
	}
	var contractCode string
	if err := evmRPC(ctx, "Ethereum", endpoint, "eth_getCode", []any{setting.EthUsdtContract, "latest"}, &contractCode); err != nil {
		return err
	}
	if len(contractCode) <= 2 || !strings.HasPrefix(contractCode, "0x") {
		return errors.New("Ethereum USDT contract is unavailable")
	}
	var precision string
	if err := evmRPC(ctx, "Ethereum", endpoint, "eth_call", []any{map[string]string{"to": setting.EthUsdtContract, "data": "0x313ce567"}, "latest"}, &precision); err != nil {
		return err
	}
	if n, err := bscQuantity(precision); err != nil || n != 6 {
		return errors.New("unexpected Ethereum USDT precision")
	}
	_, latest, latestStamp, err := evmHeader(ctx, "Ethereum", endpoint, "latest")
	if err != nil {
		return err
	}
	now := time.Now()
	if latest <= 0 || latestStamp < now.Unix()-120 || latestStamp > now.Unix()+30 {
		return errors.New("Ethereum RPC is not synchronized")
	}
	block, height, stamp, err := evmHeader(ctx, "Ethereum", endpoint, "finalized")
	if err != nil {
		return err
	}
	if height <= 0 || height > latest || stamp > latestStamp {
		return errors.New("invalid Ethereum finalized block")
	}
	ethFinalityCache.Lock()
	ethFinalityCache.endpoint, ethFinalityCache.block, ethFinalityCache.height, ethFinalityCache.stamp = endpoint, block, height, stamp
	ethFinalityCache.readAt, ethFinalityCache.chainAt = now, now
	ethFinalityCache.Unlock()
	return nil
}

func ethFinalizedBlock(ctx context.Context, endpoint string) (bscBlock, int64, int64, error) {
	ethFinalityCache.Lock()
	matches := ethFinalityCache.endpoint == endpoint
	if matches && time.Since(ethFinalityCache.readAt) < 15*time.Second {
		block, height, stamp := ethFinalityCache.block, ethFinalityCache.height, ethFinalityCache.stamp
		ethFinalityCache.Unlock()
		return block, height, stamp, nil
	}
	var chainAt time.Time
	if matches {
		chainAt = ethFinalityCache.chainAt
	}
	ethFinalityCache.Unlock()

	if time.Since(chainAt) >= time.Minute {
		if err := checkEthChain(ctx, endpoint); err != nil {
			return bscBlock{}, 0, 0, err
		}
		chainAt = time.Now()
	}
	block, height, stamp, err := evmHeader(ctx, "Ethereum", endpoint, "finalized")
	if err != nil {
		return bscBlock{}, 0, 0, err
	}
	if height <= 0 || stamp > time.Now().Unix()+30 {
		return bscBlock{}, 0, 0, errors.New("invalid Ethereum finalized block")
	}
	ethFinalityCache.Lock()
	ethFinalityCache.endpoint, ethFinalityCache.block, ethFinalityCache.height, ethFinalityCache.stamp = endpoint, block, height, stamp
	ethFinalityCache.readAt, ethFinalityCache.chainAt = time.Now(), chainAt
	ethFinalityCache.Unlock()
	return block, height, stamp, nil
}

func CheckEthUsdtPayment(ctx context.Context, order *model.TopUp, txHash string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if order.UsdtNetwork != "ETH" || !setting.ValidBscAddress(order.UsdtAddress) ||
		order.UsdtContract != setting.EthUsdtContract || order.UsdtDecimals != 6 || order.UsdtStartTime <= 0 {
		return errors.New("invalid Ethereum order snapshot")
	}
	hash := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(txHash)), "0x")
	canonicalHash := "0x" + hash
	if !bscHash(canonicalHash) {
		return errors.New("invalid Ethereum transaction hash")
	}
	if order.Status == common.TopUpStatusSuccess && order.UsdtTxHash != nil && *order.UsdtTxHash == hash {
		return nil
	}
	if order.Status != common.TopUpStatusPending && order.Status != "expired" {
		return model.ErrTopUpStatusInvalid
	}

	endpoint := setting.GetUsdtNetworkConfig("ETH").RPCURL
	var receipt bscReceipt
	if err := evmRPC(ctx, "Ethereum", endpoint, "eth_getTransactionReceipt", []any{canonicalHash}, &receipt); err != nil {
		if errors.Is(err, errEvmRPCNoResult) || errors.Is(err, errEvmRPCRateLimited) {
			return ErrEthUsdtPending
		}
		return err
	}
	height, err := bscQuantity(receipt.BlockNumber)
	if err != nil || height <= 0 || receipt.Status != "0x1" ||
		!strings.EqualFold(receipt.TransactionHash, canonicalHash) || !bscHash(receipt.BlockHash) {
		return errors.New("Ethereum transaction receipt is not successful")
	}
	target := decimal.NewFromFloat(order.Money).Shift(6)
	if target.Sign() <= 0 || !target.IsInteger() {
		return errors.New("invalid Ethereum USDT amount")
	}
	index, err := matchingEthUsdtTransfer(order, receipt, target.BigInt())
	if err != nil {
		return err
	}

	block, finalizedHeight, stamp, err := ethFinalizedBlock(ctx, endpoint)
	if err != nil {
		if errors.Is(err, errEvmRPCNoResult) || errors.Is(err, errEvmRPCRateLimited) {
			return ErrEthUsdtPending
		}
		return err
	}
	if height > finalizedHeight {
		return ErrEthUsdtPending
	}
	if height != finalizedHeight {
		block, _, stamp, err = evmHeader(ctx, "Ethereum", endpoint, receipt.BlockNumber)
		if err != nil {
			if errors.Is(err, errEvmRPCNoResult) || errors.Is(err, errEvmRPCRateLimited) {
				return ErrEthUsdtPending
			}
			return err
		}
	}
	if !strings.EqualFold(block.Hash, receipt.BlockHash) {
		return ErrEthUsdtPending
	}
	// Ethereum block times have whole-second precision. Exclude blocks before the actual reservation.
	if stamp < (order.UsdtStartTime+999)/1000 || stamp >= order.ExpireTime {
		return model.ErrUsdtOrderExpired
	}
	return model.RechargeEthUsdt(order.TradeNo, hash, index, model.EthUsdtBlockProof{Number: height, Hash: block.Hash})
}

func matchingEthUsdtTransfer(order *model.TopUp, receipt bscReceipt, target *big.Int) (uint64, error) {
	expected := "0x" + strings.Repeat("0", 24) + strings.ToLower(order.UsdtAddress[2:])
	var index int64 = -1
	for _, entry := range receipt.Logs {
		if entry.Removed || !strings.EqualFold(entry.Address, order.UsdtContract) ||
			len(entry.Topics) != 3 || !strings.EqualFold(entry.Topics[0], bscTransferTopic) ||
			!bscHash(entry.Topics[1]) || !strings.EqualFold(entry.Topics[2], expected) || !bscHash(entry.Data) {
			continue
		}
		if !strings.EqualFold(entry.TransactionHash, receipt.TransactionHash) ||
			!strings.EqualFold(entry.BlockHash, receipt.BlockHash) || entry.BlockNumber != receipt.BlockNumber {
			return 0, errors.New("invalid Ethereum transfer proof")
		}
		value, ok := new(big.Int).SetString(entry.Data[2:], 16)
		if !ok || value.Cmp(target) != 0 {
			continue
		}
		if index >= 0 {
			return 0, errors.New("Ethereum receipt contains multiple matching transfers")
		}
		var err error
		index, err = bscQuantity(entry.LogIndex)
		if err != nil {
			return 0, errors.New("invalid Ethereum transfer log index")
		}
	}
	if index < 0 {
		return 0, errors.New("Ethereum receipt does not contain a matching USDT transfer")
	}
	return uint64(index), nil
}
