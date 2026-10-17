package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

var usdtMonitorMu sync.Mutex
var monitorCancel context.CancelFunc

func StartUsdtOrderMonitor() {
	if !common.IsMasterNode {
		return
	}
	usdtMonitorMu.Lock()
	defer usdtMonitorMu.Unlock()
	if monitorCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	monitorCancel = cancel
	go func() {
		for {
			if err := CheckPendingUsdtOrders(ctx); err != nil {
				common.SysError("USDT monitor: " + err.Error())
			}
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

func CheckPendingUsdtOrders(ctx context.Context) error {
	// Expire before querying providers, including when they are unavailable.
	now := time.Now()
	if err := model.ExpireElapsedUsdtOrders(now.Unix()); err != nil {
		return err
	}
	orders, err := model.GetPendingUsdtOrders()
	if err != nil {
		return err
	}
	groups := make(map[string][]*model.TopUp)
	var addresses []string
	var failures []error
	for _, order := range orders {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if order.UsdtNetwork == "BSC" {
			if err := CheckUsdtOrder(ctx, order); err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", order.TradeNo, err))
			}
			continue
		}
		if _, exists := groups[order.UsdtAddress]; !exists {
			addresses = append(addresses, order.UsdtAddress)
		}
		groups[order.UsdtAddress] = append(groups[order.UsdtAddress], order)
	}
	for _, address := range addresses {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		batchCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err := reconcileTrc20Orders(batchCtx, groups[address], now.UnixMilli())
		cancel()
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", address, err))
		}
	}
	return errors.Join(failures...)
}
