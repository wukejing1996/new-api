package controller

import (
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type UsdtPayRequest struct {
	Amount  int64  `json:"amount"`
	Network string `json:"network"`
}
type UsdtCheckRequest struct {
	TradeNo string `json:"trade_no"`
}

func isUsdtTopUpEnabled() bool {
	return operation_setting.IsPaymentComplianceConfirmed() && setting.IsUsdtTopUpEnabled()
}

func RequestUsdtPay(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var req UsdtPayRequest
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "Invalid USDT request"})
		return
	}
	config := setting.GetUsdtNetworkConfig(req.Network)
	if !operation_setting.IsPaymentComplianceConfirmed() || !config.Available() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "USDT payment is unavailable"})
		return
	}
	if req.Amount < int64(max(1, config.MinTopUp)) || req.Amount > 10000 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "Invalid USDT top-up amount"})
		return
	}
	var startBlock int64
	if config.Network == "BSC" {
		var err error
		startBlock, err = service.ValidateBscProvider(c.Request.Context(), config.RPCURL)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"message": "error", "data": "BSC payment lookup is unavailable; please try again later"})
			return
		}
	}
	// USDT purchases USD wallet units, independently of display currency and fiat pricing.
	quota, err := common.WalletQuotaFromDecimalStrict(decimal.NewFromInt(req.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "Invalid wallet quota"})
		return
	}
	if rejectInvalidCreditedQuota(c, c.GetInt("id"), decimal.NewFromInt(int64(quota))) {
		return
	}
	now := time.Now().Unix()
	order, err := model.CreateUsdtOrder(&model.TopUp{
		UserId: c.GetInt("id"), Amount: req.Amount, UsdtQuota: quota, UsdtAddress: config.Address,
		UsdtNetwork: config.Network, UsdtStartBlock: startBlock,
		TradeNo:       fmt.Sprintf("USDT%d%s", c.GetInt("id"), common.GetRandomString(24)),
		PaymentMethod: model.PaymentMethodUsdt, PaymentProvider: model.PaymentProviderUsdt,
		CreateTime: now, ExpireTime: now + int64(max(300, min(86400, config.OrderExpireTime))), Status: common.TopUpStatusPending,
	})
	if err != nil {
		logger.LogError(c.Request.Context(), "USDT order creation failed: "+err.Error())
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "Unable to create payment order"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{
		"trade_no": order.TradeNo, "address": order.UsdtAddress, "amount": order.Money,
		"credited_amount": order.Money,
		"network":         config.Network, "expire_time": order.ExpireTime,
	}})
}

func CheckUsdtPayment(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var req UsdtCheckRequest
	if c.ShouldBindJSON(&req) != nil || req.TradeNo == "" || len(req.TradeNo) > 255 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "Invalid order"})
		return
	}
	order := model.GetTopUpByTradeNo(req.TradeNo)
	if order == nil || order.UserId != c.GetInt("id") || order.PaymentProvider != model.PaymentProviderUsdt || order.PaymentMethod != model.PaymentMethodUsdt {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "Order not found"})
		return
	}
	if order.Status != common.TopUpStatusSuccess {
		if err := service.CheckUsdtOrder(c.Request.Context(), order); err != nil {
			logger.LogError(c.Request.Context(), "USDT verification failed: "+err.Error())
			c.JSON(http.StatusOK, gin.H{"message": "error", "data": "Unable to verify payment; please try again or contact support"})
			return
		}
		order = model.GetTopUpByTradeNo(req.TradeNo)
	}
	if order != nil && order.Status == common.TopUpStatusSuccess {
		c.JSON(http.StatusOK, gin.H{"message": "success", "data": "Payment credited"})
		return
	}
	if order != nil && order.Status == "expired" {
		c.JSON(http.StatusOK, gin.H{"message": "expired", "data": "Payment window has ended. Do not send funds to this order."})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "pending", "data": "Payment not confirmed yet"})
}
