package service

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"sync"
	"time"
)

var usdtMonitorMu sync.Mutex
var monitorCancel context.CancelFunc

func StartUsdtOrderMonitor() {
	usdtMonitorMu.Lock()
	defer usdtMonitorMu.Unlock()
	if monitorCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	monitorCancel = cancel
	go func() {
		for {
			checkPendingUsdtOrders(ctx)
			timer := time.NewTimer(time.Duration(max(10, min(3600, setting.GetUsdtConfig().CheckInterval))) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

func StopUsdtOrderMonitor() {
	usdtMonitorMu.Lock()
	defer usdtMonitorMu.Unlock()
	if monitorCancel != nil {
		monitorCancel()
		monitorCancel = nil
	}
}

func checkPendingUsdtOrders(ctx context.Context) {
	// ponytail: one query per order; batch by recipient when volume exceeds TronGrid rate limits.
	orders, err := model.GetPendingUsdtOrders()
	if err != nil {
		common.SysError("USDT monitor: " + err.Error())
		return
	}
	for _, order := range orders {
		if ctx.Err() != nil {
			return
		}
		if err := CheckUsdtOrder(ctx, order); err != nil {
			common.SysError("USDT monitor: " + order.TradeNo + ": " + err.Error())
		}
	}
}
