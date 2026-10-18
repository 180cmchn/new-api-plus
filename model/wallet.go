package model

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	WalletOrdinary   = "ordinary"
	WalletCommission = "commission"
	WalletGift       = "gift"
)

var ErrInsufficientWalletQuota = errors.New("wallet quota insufficient")

// WalletCharge records the exact sources of a reservation. Only final charges
// enter daily commissions; failure refunds restore those sources. It lives in
// the main database, independently of optional/retained consumption logs.
type WalletCharge struct {
	Id                     int    `json:"id"`
	RequestID              string `json:"request_id" gorm:"type:varchar(128);uniqueIndex;not null"`
	UserID                 int    `json:"user_id" gorm:"index;not null"`
	WalletOrder            string `json:"-" gorm:"type:varchar(64)"`
	OrdinaryQuota          int    `json:"ordinary_quota" gorm:"type:bigint;not null;default:0"`
	CommissionQuota        int    `json:"commission_quota" gorm:"type:bigint;not null;default:0"`
	GiftQuota              int    `json:"gift_quota" gorm:"type:bigint;not null;default:0"`
	CreatedAt              int64  `json:"created_at"`
	ConsumedAt             int64  `json:"consumed_at" gorm:"index;not null;default:0"`
	ConsumptionDay         int64  `json:"consumption_day" gorm:"index;not null;default:0"`
	InviterID              int    `json:"inviter_id" gorm:"index;not null;default:0"`
	RefundedAt             int64  `json:"refunded_at" gorm:"not null;default:0"`
	CommissionSettlementID int    `json:"commission_settlement_id" gorm:"index;not null;default:0"`
}

func (charge WalletCharge) Total() int {
	return charge.OrdinaryQuota + charge.CommissionQuota + charge.GiftQuota
}

func ValidateWalletOrder(order []string) error {
	if len(order) > 3 {
		return common.NewMessage("Invalid wallet spending order")
	}
	for i, wallet := range order {
		if wallet != WalletOrdinary && wallet != WalletCommission && wallet != WalletGift || slices.Contains(order[:i], wallet) {
			return common.NewMessage("Invalid wallet spending order")
		}
	}
	return nil
}

func NormalizeWalletOrder(order []string) []string {
	if order == nil {
		return []string{WalletOrdinary, WalletCommission, WalletGift}
	}
	return order
}

func GetWalletAvailableQuota(userID int) (int, error) {
	var user User
	if err := DB.Select("id", "quota", "commission_quota", "gift_quota", "setting").First(&user, userID).Error; err != nil {
		return 0, err
	}
	total := 0
	for _, wallet := range NormalizeWalletOrder(user.GetSetting().WalletOrder) {
		switch wallet {
		case WalletOrdinary:
			total += max(0, user.Quota)
		case WalletCommission:
			total += max(0, user.CommissionQuota)
		case WalletGift:
			total += max(0, user.GiftQuota)
		}
	}
	return min(total, common.MaxWalletQuota), nil
}

// BeijingDay uses a fixed UTC+8 boundary, independent of the server's TZ and
// installed timezone data. No historical usage is imported into this ledger.
func BeijingDay(timestamp int64) int64 {
	return (timestamp+8*3600)/86400*86400 - 8*3600
}

// SetWalletCharge sets an absolute reserved/final amount. Replays after final
// settlement cannot charge again. strict reservations cannot overdraw; final
// usage may put the first enabled wallet into arrears, as existing billing did.
func SetWalletCharge(userID int, requestID string, amount int, final, strict bool) (*WalletCharge, error) {
	var charge WalletCharge
	ordinaryDelta := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		return setWalletCharge(tx, userID, requestID, amount, final, strict, &charge, &ordinaryDelta)
	})
	if err != nil {
		return nil, err
	}
	if ordinaryDelta != 0 {
		if err := cacheIncrUserQuota(userID, int64(ordinaryDelta)); err != nil {
			common.SysError(err.Error())
		}
	}
	return &charge, nil
}

// setWalletCharge also lets task completion commit its status and wallet allocation
// together. Cache updates must be deferred until the outer transaction commits.
func setWalletCharge(tx *gorm.DB, userID int, requestID string, amount int, final, strict bool, charge *WalletCharge, ordinaryDelta *int) error {
	if userID <= 0 || requestID == "" || len(requestID) > 128 || amount < 0 {
		return errors.New("invalid wallet charge")
	}
	if err := common.ValidateWalletQuota(amount); err != nil {
		return err
	}
	// The same user lock serializes spending, transfers, manual balance edits,
	// and inviter changes. Cache values are never used to authorize funds.
	var user User
	if err := lockForUpdate(tx).First(&user, userID).Error; err != nil {
		return err
	}
	err := lockForUpdate(tx).Where("request_id = ?", requestID).First(charge).Error
	exists := err == nil
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if !exists {
		order := NormalizeWalletOrder(user.GetSetting().WalletOrder)
		if err := ValidateWalletOrder(order); err != nil {
			return err
		}
		*charge = WalletCharge{RequestID: requestID, UserID: userID, WalletOrder: strings.Join(order, ","), CreatedAt: time.Now().Unix()}
	}
	if charge.UserID != userID {
		return errors.New("wallet charge belongs to another user")
	}
	if charge.RefundedAt > 0 {
		return errors.New("wallet reservation already refunded")
	}
	if charge.ConsumedAt > 0 {
		if final && charge.Total() == amount {
			return nil
		}
		return errors.New("wallet charge already finalized")
	}
	var order []string
	if charge.WalletOrder != "" {
		order = strings.Split(charge.WalletOrder, ",")
	}
	if len(order) == 0 && amount > 0 {
		return ErrInsufficientWalletQuota
	}
	balances := map[string]int{WalletOrdinary: user.Quota, WalletCommission: user.CommissionQuota, WalletGift: user.GiftQuota}
	spent := map[string]int{WalletOrdinary: charge.OrdinaryQuota, WalletCommission: charge.CommissionQuota, WalletGift: charge.GiftQuota}
	delta := amount - charge.Total()
	if delta > 0 {
		remaining := delta
		for _, wallet := range order {
			take := min(remaining, max(0, balances[wallet]))
			balances[wallet] -= take
			spent[wallet] += take
			remaining -= take
		}
		if remaining > 0 {
			if strict {
				return ErrInsufficientWalletQuota
			}
			wallet := order[0]
			balances[wallet] -= remaining
			spent[wallet] += remaining
		}
	} else if delta < 0 {
		remaining := -delta
		for i := len(order) - 1; i >= 0; i-- {
			wallet := order[i]
			refund := min(remaining, spent[wallet])
			balances[wallet] += refund
			spent[wallet] -= refund
			remaining -= refund
		}
		if remaining != 0 {
			return errors.New("invalid wallet reservation allocation")
		}
	}
	for _, balance := range balances {
		if balance > common.MaxWalletQuota || balance < -common.MaxWalletQuota {
			return ErrWalletQuotaLimitExceeded
		}
	}
	*ordinaryDelta = balances[WalletOrdinary] - user.Quota
	if delta != 0 {
		result := tx.Model(&User{}).Where("id = ? AND quota = ? AND commission_quota = ? AND gift_quota = ?", userID, user.Quota, user.CommissionQuota, user.GiftQuota).
			Updates(map[string]any{"quota": balances[WalletOrdinary], "commission_quota": balances[WalletCommission], "gift_quota": balances[WalletGift]})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("wallet balance changed concurrently")
		}
	}
	charge.OrdinaryQuota, charge.CommissionQuota, charge.GiftQuota = spent[WalletOrdinary], spent[WalletCommission], spent[WalletGift]
	if final {
		charge.ConsumedAt = time.Now().Unix()
		charge.ConsumptionDay = BeijingDay(charge.ConsumedAt)
		charge.InviterID = user.InviterId
		if charge.InviterID == userID {
			charge.InviterID = 0
		}
	}
	if !exists {
		return tx.Create(charge).Error
	}
	return tx.Save(charge).Error
}

// RefundWalletCharge is idempotent, including across processes. A completed
// request cannot be refunded as a failed reservation.
func RefundWalletCharge(userID int, requestID string) error {
	var ordinaryRefund int
	err := DB.Transaction(func(tx *gorm.DB) error { return refundWalletCharge(tx, userID, requestID, &ordinaryRefund) })
	if err == nil && ordinaryRefund > 0 {
		syncCreditUserQuotaCache(userID, ordinaryRefund, "wallet refund")
	}
	return err
}

func refundWalletCharge(tx *gorm.DB, userID int, requestID string, ordinaryRefund *int) error {
	var user User
	if err := lockForUpdate(tx.Unscoped()).First(&user, userID).Error; err != nil {
		return err
	}
	var charge WalletCharge
	if err := lockForUpdate(tx).Where("request_id = ? AND user_id = ?", requestID, userID).First(&charge).Error; err != nil {
		return err
	}
	if charge.RefundedAt != 0 {
		return nil
	}
	if charge.ConsumedAt != 0 {
		return errors.New("wallet charge already finalized")
	}
	if user.Quota > common.MaxWalletQuota-charge.OrdinaryQuota || user.CommissionQuota > common.MaxWalletQuota-charge.CommissionQuota || user.GiftQuota > common.MaxWalletQuota-charge.GiftQuota {
		return ErrWalletQuotaLimitExceeded
	}
	if err := tx.Unscoped().Model(&user).Updates(map[string]any{
		"quota":            gorm.Expr("quota + ?", charge.OrdinaryQuota),
		"commission_quota": gorm.Expr("commission_quota + ?", charge.CommissionQuota),
		"gift_quota":       gorm.Expr("gift_quota + ?", charge.GiftQuota),
	}).Error; err != nil {
		return err
	}
	*ordinaryRefund = charge.OrdinaryQuota
	return tx.Model(&charge).Update("refunded_at", time.Now().Unix()).Error
}

// CreditWalletQuota is used inside an existing transaction by typed credits.
func CreditWalletQuota(tx *gorm.DB, userID, quota int, wallet string) error {
	if quota < 0 {
		return errors.New("quota cannot be negative")
	}
	if err := common.ValidateWalletQuota(quota); err != nil {
		return err
	}
	column := "quota"
	switch wallet {
	case WalletOrdinary:
	case WalletCommission:
		column = "commission_quota"
	case WalletGift:
		column = "gift_quota"
	default:
		return errors.New("invalid wallet type")
	}
	if quota == 0 {
		return nil
	}
	result := tx.Model(&User{}).Where("id = ? AND "+column+" <= ?", userID, common.MaxWalletQuota-quota).
		UpdateColumn(column, gorm.Expr(column+" + ?", quota))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrWalletQuotaLimitExceeded
	}
	return nil
}

func UpdateWalletOrder(userID int, order []string) error {
	if order == nil {
		return common.NewMessage("Invalid wallet spending order")
	}
	if err := ValidateWalletOrder(order); err != nil {
		return err
	}
	var settingValue string
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).First(&user, userID).Error; err != nil {
			return err
		}
		settings := user.GetSetting()
		settings.WalletOrder = order
		encoded, err := common.Marshal(settings)
		if err != nil {
			return err
		}
		settingValue = string(encoded)
		return tx.Model(&user).Update("setting", settingValue).Error
	})
	if err != nil {
		return err
	}
	return updateUserSettingCache(userID, settingValue)
}
