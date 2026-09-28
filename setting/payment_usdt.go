package setting

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
	return UsdtEnabled && UsdtReceiveAddress != ""
}
