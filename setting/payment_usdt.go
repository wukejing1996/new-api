package setting

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var (
	UsdtEnabled           = false
	UsdtMinTopUp          = 10
	UsdtReceiveAddress    = ""
	UsdtTrongridApiKey    = ""
	UsdtNetwork           = "TRC20" // 默认使用TRC20网络
	UsdtCheckInterval     = 30      // 检查支付状态的间隔（秒）
	UsdtOrderExpireTime   = 1800    // 订单过期时间（秒），默认30分钟
	UsdtBscEnabled        = false
	UsdtBscReceiveAddress = ""
	UsdtBscRpcURL         = ""
)

const BscUsdtContract = "0x55d398326f99059ff775485246999027b3197955"
const TronUsdtContract = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"

func IsUsdtTopUpEnabled() bool {
	c := GetUsdtConfig()
	return c.Available() || GetUsdtNetworkConfig("BSC").Available()
}

type UsdtConfig struct {
	Enabled                                  bool
	Address, APIKey, Network                 string
	MinTopUp, CheckInterval, OrderExpireTime int
	RPCURL                                   string
}

func (c UsdtConfig) Available() bool {
	if c.Network == "BSC" {
		return c.Enabled && ValidBscAddress(c.Address) && ValidBscRPCURL(c.RPCURL)
	}
	return c.Enabled && c.Network == "TRC20" && ValidTronAddress(c.Address)
}

func ValidBscAddress(address string) bool {
	if len(address) != 42 || !strings.HasPrefix(address, "0x") {
		return false
	}
	b, err := hex.DecodeString(address[2:])
	return err == nil && new(big.Int).SetBytes(b).Sign() > 0
}

func ValidBscRPCURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == "" && len(value) <= 2048
}

func GetUsdtNetworkConfig(network string) UsdtConfig {
	c := GetUsdtConfig()
	if network == "" || network == "TRC20" {
		return c
	}
	if network != "BSC" {
		return UsdtConfig{}
	}
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	c.Enabled, c.Address, c.Network, c.RPCURL = UsdtBscEnabled, strings.ToLower(UsdtBscReceiveAddress), "BSC", UsdtBscRpcURL
	c.APIKey = ""
	return c
}

func GetUsdtConfig() UsdtConfig {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return UsdtConfig{Enabled: UsdtEnabled, Address: UsdtReceiveAddress, APIKey: UsdtTrongridApiKey, Network: UsdtNetwork, MinTopUp: UsdtMinTopUp, CheckInterval: UsdtCheckInterval, OrderExpireTime: UsdtOrderExpireTime}
}

func ValidTronAddress(address string) bool {
	if len(address) != 34 || address[0] != 'T' {
		return false
	}
	n := new(big.Int)
	for _, c := range address {
		i := strings.IndexRune("123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz", c)
		if i < 0 {
			return false
		}
		n.Mul(n, big.NewInt(58)).Add(n, big.NewInt(int64(i)))
	}
	b := n.Bytes()
	if len(b) != 25 || b[0] != 0x41 {
		return false
	}
	h := sha256.Sum256(b[:21])
	h = sha256.Sum256(h[:])
	return string(h[:4]) == string(b[21:])
}

func ValidateUsdtOption(key, value string) error {
	switch key {
	case "UsdtBscEnabled":
		if value != "true" && value != "false" {
			return fmt.Errorf("invalid BSC enabled value")
		}
		c := GetUsdtNetworkConfig("BSC")
		if value == "true" && (!ValidBscAddress(c.Address) || !ValidBscRPCURL(c.RPCURL)) {
			return fmt.Errorf("configure a BSC receive address and HTTPS RPC URL before enabling BSC")
		}
	case "UsdtBscReceiveAddress":
		if value != "" && !ValidBscAddress(value) {
			return fmt.Errorf("invalid BSC receive address")
		}
	case "UsdtBscRpcURL":
		if value != "" && !ValidBscRPCURL(value) {
			return fmt.Errorf("invalid BSC HTTPS RPC URL")
		}
	case "UsdtEnabled":
		if value != "true" && value != "false" {
			return fmt.Errorf("invalid USDT enabled value")
		}
		if value == "true" && !ValidTronAddress(GetUsdtConfig().Address) {
			return fmt.Errorf("configure a valid TRON receive address before enabling USDT")
		}
	case "UsdtReceiveAddress":
		if value != "" && !ValidTronAddress(value) {
			return fmt.Errorf("invalid TRON receive address")
		}
	case "UsdtNetwork":
		if value != "TRC20" {
			return fmt.Errorf("USDT only supports TRC20")
		}
	case "UsdtMinTopUp", "UsdtCheckInterval", "UsdtOrderExpireTime":
		minValue, maxValue := 1, 10000
		if key == "UsdtCheckInterval" {
			minValue, maxValue = 10, 3600
		}
		if key == "UsdtOrderExpireTime" {
			minValue, maxValue = 300, 86400
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < minValue || n > maxValue {
			return fmt.Errorf("%s must be an integer between %d and %d", key, minValue, maxValue)
		}
	}
	return nil
}
