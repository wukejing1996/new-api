package model

import (
	"crypto/rand"
	"errors"
	"fmt"
	"maps"
	"math"
	"math/big"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type TopUp struct {
	Id              int     `json:"id"`
	UserId          int     `json:"user_id" gorm:"index"`
	Amount          int64   `json:"amount"`
	Money           float64 `json:"money"`
	TradeNo         string  `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod   string  `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider string  `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	CreateTime      int64   `json:"create_time"`
	CompleteTime    int64   `json:"complete_time"`
	Status          string  `json:"status"`
	InviterRewarded bool    `json:"-" gorm:"column:inviter_rewarded"`
	TxHash          string  `json:"tx_hash" gorm:"type:varchar(255);index;default:''"`
	ExpireTime      int64   `json:"expire_time" gorm:"default:0"`
	// Nullable unique keys leave other providers and legacy orders untouched.
	UsdtAmountKey *string `json:"-" gorm:"type:varchar(100);uniqueIndex"`
	UsdtTxHash    *string `json:"-" gorm:"type:varchar(64);uniqueIndex"`
	UsdtAddress   string  `json:"usdt_address,omitempty" gorm:"type:varchar(34)"`
	UsdtQuota     int     `json:"-"`
	UsdtStartTime int64   `json:"-"`
}

const (
	PaymentMethodStripe       = "stripe"
	PaymentMethodCreem        = "creem"
	PaymentMethodWaffo        = "waffo"
	PaymentMethodWaffoPancake = "waffo_pancake"
	PaymentMethodBalance      = "balance"
	PaymentMethodUsdt         = "usdt"
)

const (
	PaymentProviderEpay         = "epay"
	PaymentProviderStripe       = "stripe"
	PaymentProviderCreem        = "creem"
	PaymentProviderWaffo        = "waffo"
	PaymentProviderWaffoPancake = "waffo_pancake"
	PaymentProviderBalance      = "balance"
	PaymentProviderUsdt         = "usdt"
)

var (
	ErrPaymentMethodMismatch    = errors.New("payment method mismatch")
	ErrTopUpNotFound            = errors.New("topup not found")
	ErrTopUpStatusInvalid       = errors.New("topup status invalid")
	ErrInvalidTopUpQuota        = errors.New("invalid top-up quota")
	ErrTopUpQuotaLimitExceeded  = errors.New("top-up quota limit exceeded")
	ErrWalletQuotaLimitExceeded = errors.New("wallet quota limit exceeded")
)

func (topUp *TopUp) Insert() error {
	var err error
	err = DB.Create(topUp).Error
	return err
}

func topUpQuotaMaxCurrent(creditedQuota int) (int, error) {
	if creditedQuota <= 0 || creditedQuota > common.MaxWalletQuota {
		return 0, ErrInvalidTopUpQuota
	}
	return common.MaxWalletQuota - creditedQuota, nil
}

// ValidateTopUpQuotaCapacity performs the user-facing pre-payment check. The
// settlement path repeats the same invariant with an atomic conditional
// update, because the wallet balance can change after checkout creation.
func ValidateTopUpQuotaCapacity(userId int, creditedQuota int) error {
	maxCurrentQuota, err := topUpQuotaMaxCurrent(creditedQuota)
	if err != nil {
		return err
	}

	var user User
	if err := DB.Select("quota").Where("id = ?", userId).First(&user).Error; err != nil {
		return err
	}
	if user.Quota > maxCurrentQuota {
		return ErrTopUpQuotaLimitExceeded
	}
	return nil
}

// creditTopUpQuota atomically enforces the wallet ceiling while adding quota.
// Keeping the predicate and increment in one UPDATE prevents two
// concurrent callbacks from both passing a separate read/check.
func creditTopUpQuota(tx *gorm.DB, userId int, creditedQuota int, updates map[string]any) error {
	maxCurrentQuota, err := topUpQuotaMaxCurrent(creditedQuota)
	if err != nil {
		return err
	}

	updateFields := make(map[string]any, len(updates)+1)
	maps.Copy(updateFields, updates)
	updateFields["quota"] = gorm.Expr("quota + ?", creditedQuota)

	result := tx.Model(&User{}).
		Where("id = ? AND quota <= ?", userId, maxCurrentQuota).
		Updates(updateFields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}

	var count int64
	if err := tx.Model(&User{}).Where("id = ?", userId).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return ErrTopUpQuotaLimitExceeded
}

func (topUp *TopUp) Update() error {
	var err error
	err = DB.Save(topUp).Error
	return err
}

func GetTopUpById(id int) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("id = ?", id).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func GetTopUpByTradeNo(tradeNo string) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("trade_no = ?", tradeNo).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func UpdatePendingTopUpStatus(tradeNo string, expectedPaymentProvider string, targetStatus string) error {
	if tradeNo == "" {
		return errors.New("payment number is required")
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		topUp.Status = targetStatus
		return tx.Save(topUp).Error
	})
}

func GetUserPendingTopUpByTradeNo(userId int, tradeNo string) (*TopUp, error) {
	if tradeNo == "" {
		return nil, errors.New("tradeNo is empty")
	}

	topUp := &TopUp{}
	err := DB.Where("user_id = ? AND trade_no = ? AND status = ?", userId, tradeNo, common.TopUpStatusPending).First(topUp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTopUpNotFound
	}
	if err != nil {
		return nil, err
	}
	return topUp, nil
}

func CreateRetryTopUpFromPending(userId int, oldTradeNo string, newTopUp *TopUp) error {
	if oldTradeNo == "" || newTopUp == nil || newTopUp.TradeNo == "" {
		return errors.New("invalid retry top-up")
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		oldTopUp := &TopUp{}
		if err := tx.Where("user_id = ? AND trade_no = ?", userId, oldTradeNo).First(oldTopUp).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTopUpNotFound
			}
			return err
		}
		if oldTopUp.PaymentProvider != PaymentProviderStripe {
			return ErrPaymentMethodMismatch
		}
		if oldTopUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		newTopUp.UserId = oldTopUp.UserId
		newTopUp.Amount = oldTopUp.Amount
		newTopUp.Money = oldTopUp.Money
		newTopUp.PaymentMethod = PaymentMethodStripe
		newTopUp.PaymentProvider = PaymentProviderStripe
		newTopUp.Status = common.TopUpStatusPending

		result := tx.Model(&TopUp{}).
			Where("user_id = ? AND trade_no = ? AND payment_provider = ? AND status = ?", userId, oldTradeNo, PaymentProviderStripe, common.TopUpStatusPending).
			Updates(map[string]interface{}{
				"status":        common.TopUpStatusExpired,
				"complete_time": newTopUp.CreateTime,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTopUpStatusInvalid
		}

		if err := tx.Create(newTopUp).Error; err != nil {
			return err
		}

		return nil
	})
}

func ExpirePendingTopUpsOlderThan(now int64, ttlSeconds int64) error {
	if ttlSeconds <= 0 {
		return nil
	}

	cutoff := now - ttlSeconds
	if cutoff <= 0 {
		return nil
	}

	return DB.Model(&TopUp{}).
		Where("status = ? AND payment_provider = ? AND create_time < ?", common.TopUpStatusPending, PaymentProviderStripe, cutoff).
		Updates(map[string]interface{}{
			"status":        common.TopUpStatusExpired,
			"complete_time": now,
		}).Error
}

// RechargeEpay 原子完成易支付订单：订单行锁、状态校验、成功更新与用户额度增加
// 在同一个事务内完成，因此同一订单的并发/重复回调（包括多实例部署下）最多充值一次。
// alreadyDone=true 表示订单此前已完成，本次为幂等重复回调。
// 进程内的 LockOrder 只是优化，正确性由本函数的数据库行锁保证。
func RechargeEpay(tradeNo string, actualPaymentMethod string, callerIp string) (alreadyDone bool, err error) {
	if tradeNo == "" {
		return false, errors.New("未提供支付单号")
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	var quotaToAdd int
	topUp := &TopUp{}
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if topUp.PaymentProvider != PaymentProviderEpay {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status == common.TopUpStatusSuccess {
			alreadyDone = true
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}
		if actualPaymentMethod != "" && topUp.PaymentMethod != actualPaymentMethod {
			topUp.PaymentMethod = actualPaymentMethod
		}
		var quotaErr error
		quotaToAdd, quotaErr = common.WalletQuotaFromDecimalStrict(
			decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
		)
		if quotaErr != nil || quotaToAdd <= 0 {
			return ErrInvalidTopUpQuota
		}
		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}
		return creditTopUpQuota(tx, topUp.UserId, quotaToAdd, nil)
	})
	if err != nil {
		if !errors.Is(err, ErrTopUpNotFound) && !errors.Is(err, ErrPaymentMethodMismatch) && !errors.Is(err, ErrTopUpStatusInvalid) {
			common.SysError("epay topup failed: " + err.Error())
		}
		return false, err
	}
	if alreadyDone {
		return true, nil
	}
	syncCreditUserQuotaCache(topUp.UserId, quotaToAdd, "epay topup")

	common.SysLog(fmt.Sprintf("易支付充值成功 trade_no=%s user_id=%d quota_to_add=%d money=%.2f", topUp.TradeNo, topUp.UserId, quotaToAdd, topUp.Money))
	RecordTopupLog(topUp.UserId, fmt.Sprintf("使用在线充值成功，充值金额: %v，支付金额：%f", logger.LogQuota(quotaToAdd), topUp.Money), callerIp, topUp.PaymentMethod, PaymentProviderEpay)
	return false, nil
}

func Recharge(referenceId string, customerId string, callerIp string) (err error) {
	if referenceId == "" {
		return errors.New("payment number is required")
	}

	var quota int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("top-up order does not exist")
		}

		if topUp.PaymentProvider != PaymentProviderStripe {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("invalid top-up order status")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		err = tx.Save(topUp).Error
		if err != nil {
			return err
		}

		quota, err = common.WalletQuotaFromDecimalStrict(
			decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
		)
		if err != nil || quota <= 0 {
			return ErrInvalidTopUpQuota
		}
		return creditTopUpQuota(tx, topUp.UserId, quota, map[string]any{
			"stripe_customer": customerId,
		})
	})

	if err != nil {
		common.SysError("topup failed: " + err.Error())
		return errors.New("top-up failed. Please try again later.")
	}
	syncCreditUserQuotaCache(topUp.UserId, quota, "stripe topup")

	RecordTopupLog(topUp.UserId, fmt.Sprintf("Online top-up succeeded, quota: %v, payment amount: %.2f", logger.FormatQuota(quota), topUp.Money), callerIp, topUp.PaymentMethod, PaymentMethodStripe)
	RewardInviterForStripeTopUp(topUp.UserId, topUp.Id, topUp.Money)

	return nil
}

func RewardInviterForStripeTopUp(userId int, topUpId int, paidAmount float64) {
	ratio := common.InviterTopUpRewardRatio
	if userId <= 0 || topUpId <= 0 || paidAmount <= 0 || math.IsNaN(paidAmount) || math.IsInf(paidAmount, 0) ||
		ratio <= 0 || ratio > 1 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return
	}

	var inviterId int
	var rewardQuota int
	var rewardAmount string
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := tx.Select("id, inviter_id").Where("id = ?", userId).First(&user).Error; err != nil {
			return err
		}
		if user.InviterId <= 0 {
			return nil
		}
		inviterId = user.InviterId

		var topUp TopUp
		if err := lockForUpdate(tx).
			Where("id = ? AND user_id = ? AND payment_provider IN ? AND status = ? AND inviter_rewarded = ?", topUpId, userId, []string{PaymentProviderStripe, PaymentProviderUsdt}, common.TopUpStatusSuccess, false).
			First(&topUp).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}

		if topUp.Money <= 0 || math.IsNaN(topUp.Money) || math.IsInf(topUp.Money, 0) ||
			math.Abs(topUp.Money-paidAmount) > 0.005 {
			return errors.New("stripe top-up payment amount mismatch")
		}
		if common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
			return errors.New("invalid quota per unit")
		}
		rewardDecimal := decimal.NewFromFloat(paidAmount).Mul(decimal.NewFromFloat(ratio))
		rewardAmount = rewardDecimal.StringFixed(2)
		rewardDecimal = rewardDecimal.Mul(decimal.NewFromFloat(common.QuotaPerUnit))
		var err error
		rewardQuota, err = common.WalletQuotaFromDecimalStrict(rewardDecimal)
		if err != nil {
			return err
		}
		if rewardQuota > 0 {
			if err := creditTopUpQuota(tx, inviterId, rewardQuota, nil); err != nil {
				return err
			}
		}

		if err := tx.Model(&TopUp{}).
			Where("id = ? AND inviter_rewarded = ?", topUp.Id, false).
			Update("inviter_rewarded", true).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		common.SysError(fmt.Sprintf("stripe inviter top-up reward failed: user_id=%d, error=%v", userId, err))
		return
	}
	if rewardQuota > 0 {
		syncCreditUserQuotaCache(inviterId, rewardQuota, "stripe inviter reward")
		RecordLog(inviterId, LogTypeTopup, fmt.Sprintf("Received an invitation reward of %s from a referred user's top-up (paid amount: %.2f)", rewardAmount, paidAmount))
	}
}

// topUpQueryWindowSeconds 限制充值记录查询的时间窗口（秒）。
const topUpQueryWindowSeconds int64 = 30 * 24 * 60 * 60

// topUpQueryCutoff 返回允许查询的最早 create_time（秒级 Unix 时间戳）。
func topUpQueryCutoff() int64 {
	return common.GetTimestamp() - topUpQueryWindowSeconds
}

func GetUserTopUps(userId int, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	// Start transaction
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	cutoff := topUpQueryCutoff()

	// Get total count within transaction
	err = tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, cutoff).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated topups within same transaction
	err = tx.Where("user_id = ? AND create_time >= ?", userId, cutoff).Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Commit transaction
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// GetAllTopUps 获取全平台的充值记录（管理员使用，不限制时间窗口）
func GetAllTopUps(pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err = tx.Model(&TopUp{}).Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// searchTopUpCountHardLimit 搜索充值记录时 COUNT 的安全上限，
// 防止对超大表执行无界 COUNT 触发 DoS。
const searchTopUpCountHardLimit = 10000

// SearchUserTopUps 按订单号搜索某用户的充值记录
func SearchUserTopUps(userId int, keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, topUpQueryCutoff())
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("failed to search top-up records")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("failed to search top-up records")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// SearchAllTopUps 按订单号搜索全平台充值记录（管理员使用，不限制时间窗口）
func SearchAllTopUps(keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{})
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("failed to search top-up records")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("failed to search top-up records")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// ManualCompleteTopUp 管理员手动完成订单并给用户充值
func ManualCompleteTopUp(tradeNo string, callerIp string) error {
	if tradeNo == "" {
		return errors.New("order number is required")
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	var userId int
	var quotaToAdd int
	var payMoney float64
	var paymentMethod string

	err := DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		// 行级锁，避免并发补单
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return errors.New("top-up order does not exist")
		}

		// 幂等处理：已成功直接返回
		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}

		if topUp.Status != common.TopUpStatusPending && !(topUp.PaymentProvider == PaymentProviderUsdt && (topUp.Status == "expired" || topUp.Status == common.TopUpStatusProcessing)) {
			return errors.New("order is not pending payment and cannot be repaired")
		}

		var quotaErr error
		quotaToAdd, quotaErr = common.WalletQuotaFromDecimalStrict(decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)))
		if topUp.PaymentProvider == PaymentProviderUsdt && topUp.UsdtQuota > 0 {
			quotaToAdd, quotaErr = topUp.UsdtQuota, nil
		}
		if quotaErr != nil || quotaToAdd <= 0 {
			return ErrInvalidTopUpQuota
		}

		// 标记完成
		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if topUp.PaymentProvider == PaymentProviderUsdt {
			topUp.UsdtAmountKey = nil
		}
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		// 增加用户额度（立即写库，保持一致性）
		if err := creditTopUpQuota(tx, topUp.UserId, quotaToAdd, nil); err != nil {
			return err
		}

		userId = topUp.UserId
		payMoney = topUp.Money
		paymentMethod = topUp.PaymentMethod
		return nil
	})

	if err != nil {
		return err
	}

	// 事务外记录日志，避免阻塞
	syncCreditUserQuotaCache(userId, quotaToAdd, "manual topup")
	RecordTopupLog(userId, fmt.Sprintf("Admin completed top-up order, quota: %v, payment amount: %f", logger.FormatQuota(quotaToAdd), payMoney), callerIp, paymentMethod, "admin")
	return nil
}
func RechargeCreem(referenceId string, customerEmail string, customerName string, callerIp string) (err error) {
	if referenceId == "" {
		return errors.New("payment number is required")
	}

	var quota int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("top-up order does not exist")
		}

		if topUp.PaymentProvider != PaymentProviderCreem {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("invalid top-up order status")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		err = tx.Save(topUp).Error
		if err != nil {
			return err
		}

		// Creem 直接使用 Amount 作为充值额度（整数）
		quota, err = common.WalletQuotaFromDecimalStrict(decimal.NewFromInt(topUp.Amount))
		if err != nil || quota <= 0 {
			return ErrInvalidTopUpQuota
		}

		// 构建更新字段，优先使用邮箱，如果邮箱为空则使用用户名
		updateFields := map[string]any{}

		// 如果有客户邮箱，尝试更新用户邮箱（仅当用户邮箱为空时）
		if customerEmail != "" {
			// 先检查用户当前邮箱是否为空
			var user User
			err = tx.Where("id = ?", topUp.UserId).First(&user).Error
			if err != nil {
				return err
			}

			// 如果用户邮箱为空，则更新为支付时使用的邮箱
			if user.Email == "" {
				updateFields["email"] = customerEmail
			}
		}

		return creditTopUpQuota(tx, topUp.UserId, quota, updateFields)
	})

	if err != nil {
		common.SysError("creem topup failed: " + err.Error())
		return errors.New("top-up failed. Please try again later.")
	}
	syncCreditUserQuotaCache(topUp.UserId, quota, "creem topup")

	RecordTopupLog(topUp.UserId, fmt.Sprintf("Creem top-up succeeded, quota: %v, payment amount: %.2f", quota, topUp.Money), callerIp, topUp.PaymentMethod, PaymentMethodCreem)

	return nil
}

func RechargeWaffo(tradeNo string, callerIp string) (err error) {
	if tradeNo == "" {
		return errors.New("payment number is required")
	}

	var quotaToAdd int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return errors.New("top-up order does not exist")
		}

		if topUp.PaymentProvider != PaymentProviderWaffo {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil // 幂等：已成功直接返回
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("invalid top-up order status")
		}

		quotaToAdd, err = common.WalletQuotaFromDecimalStrict(
			decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
		)
		if err != nil || quotaToAdd <= 0 {
			return ErrInvalidTopUpQuota
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		return creditTopUpQuota(tx, topUp.UserId, quotaToAdd, nil)
	})

	if err != nil {
		common.SysError("waffo topup failed: " + err.Error())
		return errors.New("top-up failed. Please try again later.")
	}
	syncCreditUserQuotaCache(topUp.UserId, quotaToAdd, "waffo topup")

	if quotaToAdd > 0 {
		RecordTopupLog(topUp.UserId, fmt.Sprintf("Waffo top-up succeeded, quota: %v, payment amount: %.2f", logger.FormatQuota(quotaToAdd), topUp.Money), callerIp, topUp.PaymentMethod, PaymentMethodWaffo)
	}

	return nil
}

func RechargeWaffoPancake(tradeNo string) (err error) {
	if tradeNo == "" {
		return errors.New("payment number is required")
	}

	var quotaToAdd int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return errors.New("top-up order does not exist")
		}

		if topUp.PaymentProvider != PaymentProviderWaffoPancake {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("invalid top-up order status")
		}

		quotaToAdd, err = common.WalletQuotaFromDecimalStrict(
			decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
		)
		if err != nil || quotaToAdd <= 0 {
			return ErrInvalidTopUpQuota
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		return creditTopUpQuota(tx, topUp.UserId, quotaToAdd, nil)
	})

	if err != nil {
		common.SysError("waffo pancake topup failed: " + err.Error())
		return errors.New("top-up failed. Please try again later.")
	}
	syncCreditUserQuotaCache(topUp.UserId, quotaToAdd, "waffo pancake topup")

	if quotaToAdd > 0 {
		RecordLog(topUp.UserId, LogTypeTopup, fmt.Sprintf("Waffo Pancake top-up succeeded, quota: %v, payment amount: %.2f", logger.FormatQuota(quotaToAdd), topUp.Money))
	}

	return nil
}

// CreateUsdtOrder reserves an exact payment amount only for the active order.
// Expired amounts are reusable by design; late transfers cannot identify their original payer.
func CreateUsdtOrder(order *TopUp) (*TopUp, error) {
	if order.Amount <= 0 || order.Amount > 10000 {
		return nil, ErrInvalidTopUpQuota
	}
	// Release elapsed reservations even if the monitor has not reached them yet.
	if err := DB.Model(&TopUp{}).Where("payment_provider = ? AND status = ? AND expire_time <= ? AND usdt_amount_key IS NOT NULL", PaymentProviderUsdt, common.TopUpStatusPending, time.Now().Unix()).Updates(map[string]any{"status": "expired", "usdt_amount_key": nil}).Error; err != nil {
		return nil, err
	}
	start, err := rand.Int(rand.Reader, big.NewInt(500))
	if err != nil {
		return nil, err
	}
	for attempt := range 500 {
		micros := order.Amount*1000000 + ((start.Int64()+int64(attempt))%500+1)*1000
		key := fmt.Sprintf("%s:%d", order.UsdtAddress, micros)
		err := DB.Transaction(func(tx *gorm.DB) error {
			var user User
			if err := lockForUpdate(tx).Select("id", "quota").First(&user, order.UserId).Error; err != nil {
				return err
			}
			var pending TopUp
			err := tx.Where("user_id = ? AND amount = ? AND payment_provider = ? AND status = ? AND expire_time > ? AND usdt_amount_key IS NOT NULL", order.UserId, order.Amount, PaymentProviderUsdt, common.TopUpStatusPending, time.Now().Unix()).First(&pending).Error
			if err == nil {
				*order = pending
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			var count int64
			if err := tx.Model(&TopUp{}).Where("usdt_amount_key = ?", key).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return errUsdtAmountCollision
			}
			money := decimal.NewFromInt(micros).Div(decimal.NewFromInt(1000000))
			quota, err := common.WalletQuotaFromDecimalStrict(money.Mul(decimal.NewFromFloat(common.QuotaPerUnit)))
			if err != nil || quota <= 0 {
				return ErrInvalidTopUpQuota
			}
			if user.Quota > common.MaxWalletQuota-quota {
				return ErrWalletQuotaLimitExceeded
			}
			order.UsdtAmountKey = &key
			order.Money = money.InexactFloat64()
			order.UsdtQuota = quota
			order.UsdtStartTime = time.Now().UnixMilli()
			order.Id = 0
			return tx.Create(order).Error
		})
		if err == nil {
			return order, nil
		}
		if !errors.Is(err, errUsdtAmountCollision) {
			var count int64
			if DB.Model(&TopUp{}).Where("usdt_amount_key = ?", key).Count(&count).Error != nil || count == 0 {
				return nil, err
			}
		}
	}
	return nil, errors.New("all USDT payment amounts are currently reserved; please try again later")
}

var errUsdtAmountCollision = errors.New("USDT amount already reserved")

// RechargeUsdt commits the transaction claim, order status and wallet credit
// together. Unique database constraints also protect separate server instances.
func RechargeUsdt(tradeNo, txHash string) error {
	if len(txHash) != 64 {
		return errors.New("invalid transaction hash")
	}
	topUp := &TopUp{}
	quotaToAdd := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("trade_no = ?", tradeNo).First(topUp).Error; err != nil {
			return err
		}
		if topUp.PaymentProvider != PaymentProviderUsdt || topUp.PaymentMethod != PaymentMethodUsdt {
			return ErrPaymentMethodMismatch
		}
		if topUp.UsdtAddress == "" || topUp.UsdtQuota <= 0 {
			return errors.New("legacy USDT order requires manual reconciliation")
		}
		if topUp.Status == common.TopUpStatusSuccess {
			if topUp.TxHash != txHash {
				return ErrTopUpStatusInvalid
			}
			return nil
		}
		if topUp.Status != common.TopUpStatusPending && topUp.Status != "expired" {
			return ErrTopUpStatusInvalid
		}
		var used int64
		if err := tx.Model(&TopUp{}).Where("tx_hash = ? AND id <> ?", txHash, topUp.Id).Count(&used).Error; err != nil {
			return err
		}
		if used > 0 {
			return errors.New("transaction already credited")
		}
		result := tx.Model(topUp).Where("status = ?", topUp.Status).Updates(map[string]any{
			"status": common.TopUpStatusSuccess, "tx_hash": txHash, "usdt_tx_hash": txHash, "complete_time": common.GetTimestamp(), "usdt_amount_key": nil,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTopUpStatusInvalid
		}
		quotaToAdd = topUp.UsdtQuota
		return creditTopUpQuota(tx, topUp.UserId, quotaToAdd, nil)
	})
	if err != nil {
		return err
	}
	if quotaToAdd > 0 {
		syncCreditUserQuotaCache(topUp.UserId, quotaToAdd, "usdt topup")
		RecordLog(topUp.UserId, LogTypeTopup, fmt.Sprintf("USDT top-up: quota %v, paid %.3f USDT, tx %s", logger.FormatQuota(quotaToAdd), topUp.Money, txHash))
		RewardInviterForStripeTopUp(topUp.UserId, topUp.Id, topUp.Money)
	}
	return nil
}

func GetPendingUsdtOrders() ([]*TopUp, error) {
	var orders []*TopUp
	err := DB.Where("status = ? AND payment_provider = ? AND usdt_amount_key IS NOT NULL", common.TopUpStatusPending, PaymentProviderUsdt).Order("id ASC").Find(&orders).Error
	return orders, err
}

func ExpireUsdtOrder(orderId int) error {
	return DB.Model(&TopUp{}).Where("id = ? AND status = ? AND payment_provider = ?", orderId, common.TopUpStatusPending, PaymentProviderUsdt).Updates(map[string]any{"status": "expired", "usdt_amount_key": nil}).Error
}
