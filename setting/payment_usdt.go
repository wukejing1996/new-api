package setting

import (
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

func init() {
	common.RegisterSetting("UsdtEnabled", &UsdtEnabled, func(value string) (err error) {
		UsdtEnabled, err = common.String2Bool(value)
		return err
	})
	common.RegisterSetting("UsdtMinTopUp", &UsdtMinTopUp, func(value string) (err error) {
		UsdtMinTopUp, err = common.String2Int(value)
		return err
	})
	common.RegisterSetting("UsdtReceiveAddress", &UsdtReceiveAddress, nil)
	common.RegisterSetting("UsdtTrongridApiKey", &UsdtTrongridApiKey, nil)
	common.RegisterSetting("UsdtNetwork", &UsdtNetwork, nil)
	common.RegisterSetting("UsdtCheckInterval", &UsdtCheckInterval, func(value string) (err error) {
		UsdtCheckInterval, err = common.String2Int(value)
		return err
	})
	common.RegisterSetting("UsdtOrderExpireTime", &UsdtOrderExpireTime, func(value string) (err error) {
		UsdtOrderExpireTime, err = common.String2Int(value)
		return err
	})
}

func IsUsdtTopUpEnabled() bool {
	return UsdtEnabled && UsdtReceiveAddress != ""
}
