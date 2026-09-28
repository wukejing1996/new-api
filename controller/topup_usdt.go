package controller

import (
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type UsdtPayRequest struct {
	Amount int64 `json:"amount"`
}

type UsdtCheckRequest struct {
	TradeNo string `json:"trade_no"`
}

func isUsdtTopUpEnabled() bool {
	return setting.IsUsdtTopUpEnabled()
}

// GenerateUniqueAmount 生成唯一的随机金额（3位小数，0.001-0.500范围）
func GenerateUniqueAmount(userId int, baseAmount int64) (int64, float64) {
	maxAttempts := 10
	timeWindow := int64(300) // 5分钟窗口

	for i := 0; i < maxAttempts; i++ {
		// 生成随机数：0.001 到 0.500（3位小数）
		random := rand.Float64() * 0.500 + 0.001
		randomCents := int64(random * 1000) // 转为千分位

		finalAmount := baseAmount + randomCents
		displayAmount := float64(finalAmount) / 1000.0

		// 检查最近5分钟内是否有相同金额的pending订单
		minTimestamp := time.Now().Unix() - timeWindow
		existingOrders := model.GetPendingOrdersByAmount(displayAmount, minTimestamp)

		if len(existingOrders) == 0 {
			return finalAmount, displayAmount
		}
	}

	// 如果10次都冲突（极小概率），使用最后一次生成的随机数
	random := rand.Float64() * 0.500 + 0.001
	randomCents := int64(random * 1000)
	finalAmount := baseAmount + randomCents
	displayAmount := float64(finalAmount) / 1000.0

	return finalAmount, displayAmount
}

// RequestUsdtPay 创建USDT充值订单
func RequestUsdtPay(c *gin.Context) {
	if !isUsdtTopUpEnabled() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "USDT支付未启用"})
		return
	}

	var req UsdtPayRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}

	if req.Amount < int64(setting.UsdtMinTopUp) {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值金额不能小于 %d", setting.UsdtMinTopUp)})
		return
	}

	userId := c.GetInt("id")
	if rejectInvalidTopUpQuota(c, userId, req.Amount) {
		return
	}

	group, err := model.GetUserGroup(userId, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}

	// 生成随机金额（避免冲突）
	_, displayAmount := GenerateUniqueAmount(userId, req.Amount)

	// 计算实际应付金额（USDT 1:1，应用折扣）
	payMoney := getPayMoney(req.Amount, group)
	if payMoney < 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	// 将随机金额应用到payMoney（保留3位小数）
	finalPayMoney := payMoney + (displayAmount - float64(req.Amount))

	tradeNo := fmt.Sprintf("%s%d", common.GetRandomString(6), time.Now().Unix())
	tradeNo = fmt.Sprintf("USR%dNO%s", userId, tradeNo)

	amount := req.Amount
	topUp := &model.TopUp{
		UserId:          userId,
		Amount:          amount,
		Money:           finalPayMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodUsdt,
		PaymentProvider: model.PaymentProviderUsdt,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
		ExpireTime:      time.Now().Unix() + int64(setting.UsdtOrderExpireTime),
	}
	err = topUp.Insert()
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("USDT 创建充值订单失败 user_id=%d trade_no=%s amount=%d error=%q", userId, tradeNo, req.Amount, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("USDT 充值订单创建成功 user_id=%d trade_no=%s amount=%d money=%.3f", userId, tradeNo, req.Amount, finalPayMoney))

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"trade_no":    tradeNo,
			"address":     setting.UsdtReceiveAddress,
			"amount":      finalPayMoney,
			"network":     setting.UsdtNetwork,
			"expire_time": topUp.ExpireTime,
		},
	})
}

// CheckUsdtPayment 检查USDT支付状态
func CheckUsdtPayment(c *gin.Context) {
	if !isUsdtTopUpEnabled() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "USDT支付未启用"})
		return
	}

	var req UsdtCheckRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}

	// 查询订单
	order := model.GetTopUpByTradeNo(req.TradeNo)
	if order == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "订单不存在"})
		return
	}

	// 验证订单归属
	userId := c.GetInt("id")
	if order.UserId != userId {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "无权限"})
		return
	}

	// 检查订单状态
	if order.Status == common.TopUpStatusSuccess {
		c.JSON(http.StatusOK, gin.H{"message": "success", "data": "已充值成功"})
		return
	}

	// 检查是否过期
	if time.Now().Unix() > order.ExpireTime {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "订单已过期"})
		return
	}

	// 查询区块链交易
	tx, err := service.FindMatchingTransaction(
		setting.UsdtReceiveAddress,
		order.Money,
		order.CreateTime*1000, // 转为毫秒
	)

	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("USDT 查询区块链交易失败 trade_no=%s error=%q", req.TradeNo, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "查询支付状态失败，请稍后再试"})
		return
	}

	if tx == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "未检测到支付，请确认已转账"})
		return
	}

	// 检查TxHash是否已被使用
	if model.IsTxHashUsed(tx.TransactionID) {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("USDT 交易哈希已被使用 trade_no=%s tx_hash=%s", req.TradeNo, tx.TransactionID))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "该交易已被使用"})
		return
	}

	// 原子性认领订单
	success := model.ClaimOrderWithTxHash(order.Id, tx.TransactionID)
	if !success {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("USDT 订单认领失败（已被处理） trade_no=%s tx_hash=%s", req.TradeNo, tx.TransactionID))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "订单已被处理"})
		return
	}

	// 执行充值
	LockOrder(req.TradeNo)
	defer UnlockOrder(req.TradeNo)

	err = model.RechargeUsdt(req.TradeNo, tx.TransactionID)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("USDT 充值处理失败 trade_no=%s tx_hash=%s error=%q", req.TradeNo, tx.TransactionID, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("USDT 充值成功 trade_no=%s tx_hash=%s amount=%.3f", req.TradeNo, tx.TransactionID, order.Money))
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": "充值成功"})
}
