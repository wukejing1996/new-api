package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/setting"
)

type TRC20Transaction struct {
	TransactionID   string `json:"transaction_id"`
	TokenInfo       TokenInfo `json:"token_info"`
	From            string `json:"from"`
	To              string `json:"to"`
	Type            string `json:"type"`
	Value           string `json:"value"`
	BlockTimestamp  int64  `json:"block_timestamp"`
}

type TokenInfo struct {
	Symbol   string `json:"symbol"`
	Address  string `json:"address"`
	Decimals int    `json:"decimals"`
	Name     string `json:"name"`
}

type TrongridResponse struct {
	Data    []TRC20Transaction `json:"data"`
	Success bool               `json:"success"`
}

// GetTRC20Transactions 查询TRC20 USDT交易记录
func GetTRC20Transactions(address string, minTimestamp int64) ([]TRC20Transaction, error) {
	// USDT合约地址 (TRC20)
	usdtContract := "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"

	url := fmt.Sprintf("https://api.trongrid.io/v1/accounts/%s/transactions/trc20?only_to=true&limit=200&contract_address=%s&min_timestamp=%d",
		address, usdtContract, minTimestamp)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// 如果配置了API Key，添加到请求头
	if setting.UsdtTrongridApiKey != "" {
		req.Header.Set("TRON-PRO-API-KEY", setting.UsdtTrongridApiKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trongrid API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result TrongridResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	// 检查 API 返回的 success 字段
	if !result.Success {
		return nil, fmt.Errorf("trongrid API returned success=false")
	}

	return result.Data, nil
}

// FindMatchingTransaction 查找匹配金额的交易
func FindMatchingTransaction(address string, amount float64, minTimestamp int64) (*TRC20Transaction, error) {
	transactions, err := GetTRC20Transactions(address, minTimestamp)
	if err != nil {
		return nil, err
	}

	// USDT使用6位小数
	targetValue := int64(amount * 1000000)
	// 允许 ±0.001 USDT 的误差容忍度（1000 最小单位）
	tolerance := int64(1000)

	for _, tx := range transactions {
		if tx.To != address {
			continue
		}

		// 解析交易金额
		var txValue int64
		fmt.Sscanf(tx.Value, "%d", &txValue)

		// 计算差值绝对值
		diff := txValue - targetValue
		if diff < 0 {
			diff = -diff
		}

		// 金额在误差范围内且在时间窗口内
		if diff <= tolerance && tx.BlockTimestamp >= minTimestamp {
			return &tx, nil
		}
	}

	return nil, nil
}
