package setting

import (
	"crypto/sha256"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var (
	UsdtEnabled         = false
	UsdtMinTopUp        = 10
	UsdtReceiveAddress  = ""
	UsdtTrongridApiKey  = ""
	UsdtNetwork         = "TRC20" // 默认使用TRC20网络
	UsdtCheckInterval   = 30      // 检查支付状态的间隔（秒）
	UsdtOrderExpireTime = 1800    // 订单过期时间（秒），默认30分钟
)

func IsUsdtTopUpEnabled() bool {
	c := GetUsdtConfig()
	return c.Enabled && ValidTronAddress(c.Address) && c.Network == "TRC20"
}

type UsdtConfig struct {
	Enabled                                  bool
	Address, APIKey, Network                 string
	MinTopUp, CheckInterval, OrderExpireTime int
}

func GetUsdtConfig() UsdtConfig {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return UsdtConfig{UsdtEnabled, UsdtReceiveAddress, UsdtTrongridApiKey, UsdtNetwork, UsdtMinTopUp, UsdtCheckInterval, UsdtOrderExpireTime}
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
