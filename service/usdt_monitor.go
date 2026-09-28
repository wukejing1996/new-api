package service

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

var monitorCtx context.Context
var monitorCancel context.CancelFunc

// StartUsdtOrderMonitor 启动USDT订单监控定时任务
func StartUsdtOrderMonitor() {
	if !setting.IsUsdtTopUpEnabled() {
		common.SysLog("USDT payment not enabled, monitor not started")
		return
	}

	if monitorCancel != nil {
		common.SysLog("USDT monitor already running")
		return
	}

	monitorCtx, monitorCancel = context.WithCancel(context.Background())

	// 每60秒检查一次
	ticker := time.NewTicker(60 * time.Second)

	go func() {
		// 启动时立即执行一次
		checkPendingUsdtOrders(monitorCtx)

		for {
			select {
			case <-ticker.C:
				checkPendingUsdtOrders(monitorCtx)
			case <-monitorCtx.Done():
				ticker.Stop()
				common.SysLog("USDT order monitor stopped")
				return
			}
		}
	}()

	common.SysLog("USDT order monitor started")
}

// StopUsdtOrderMonitor 停止USDT订单监控
func StopUsdtOrderMonitor() {
	if monitorCancel != nil {
		monitorCancel()
	}
}

// checkPendingUsdtOrders 检查所有pending状态的USDT订单
func checkPendingUsdtOrders(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("USDT monitor panic: %v", r))
		}
	}()

	now := time.Now().Unix()

	// 获取所有pending状态的USDT订单
	orders := model.GetPendingUsdtOrders()
	if len(orders) == 0 {
		return
	}

	common.SysLog(fmt.Sprintf("USDT monitor: checking %d pending orders", len(orders)))

	expiredCount := 0
	processedCount := 0

	for _, order := range orders {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// 检查订单是否过期
		if now > order.ExpireTime {
			err := model.ExpireUsdtOrder(order.Id)
			if err != nil {
				common.SysError(fmt.Sprintf("USDT monitor: failed to expire order %s: %s", order.TradeNo, err.Error()))
			} else {
				common.SysLog(fmt.Sprintf("USDT monitor: expired order %s", order.TradeNo))
				expiredCount++
			}
			continue
		}

		// 查询区块链交易
		tx, err := FindMatchingTransaction(
			setting.UsdtReceiveAddress,
			order.Money,
			order.CreateTime*1000, // 转为毫秒
		)

		if err != nil {
			common.SysError(fmt.Sprintf("USDT monitor: failed to query blockchain for order %s: %s", order.TradeNo, err.Error()))
			continue
		}

		if tx == nil {
			// 未找到匹配交易，继续等待
			continue
		}

		// 原子性认领订单（数据库层面已检查 TxHash 唯一性）
		success := model.ClaimOrderWithTxHash(order.Id, tx.TransactionID)
		if !success {
			common.SysLog(fmt.Sprintf("USDT monitor: failed to claim order %s (already processed or tx_hash already used), tx_hash=%s", order.TradeNo, tx.TransactionID))
			continue
		}

		// 执行充值
		err = model.RechargeUsdt(order.TradeNo, tx.TransactionID)
		if err != nil {
			common.SysError(fmt.Sprintf("USDT monitor: failed to recharge order %s, tx_hash=%s, error=%s", order.TradeNo, tx.TransactionID, err.Error()))
			continue
		}

		common.SysLog(fmt.Sprintf("USDT monitor: successfully processed order %s, amount=%.3f, tx_hash=%s", order.TradeNo, order.Money, tx.TransactionID))
		processedCount++
	}

	if expiredCount > 0 || processedCount > 0 {
		common.SysLog(fmt.Sprintf("USDT monitor: processed=%d, expired=%d", processedCount, expiredCount))
	}
}
