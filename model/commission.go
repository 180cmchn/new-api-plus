package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CommissionDay struct {
	Id              int
	Day             int64 `gorm:"uniqueIndex;not null"`
	RateBasisPoints int64
	CreatedAt       int64 `gorm:"autoCreateTime:false"` // Zero until the automatic daily rate is captured.
}

type CommissionSettlement struct {
	Id              int   `json:"id"`
	Day             int64 `json:"day" gorm:"uniqueIndex:idx_commission_relation_rate;not null"`
	InviterID       int   `json:"inviter_id" gorm:"uniqueIndex:idx_commission_relation_rate;index;not null"`
	UserID          int   `json:"user_id" gorm:"uniqueIndex:idx_commission_relation_rate;index;not null"`
	ConsumedQuota   int   `json:"consumed_quota" gorm:"type:bigint;not null;default:0"`
	RateBasisPoints int64 `json:"rate_basis_points" gorm:"uniqueIndex:idx_commission_relation_rate"`
	RewardQuota     int   `json:"reward_quota" gorm:"type:bigint;not null;default:0"`
	SettledAt       int64 `json:"settled_at"`
}

// AutoMigrate creates the replacement index first, preserving uniqueness even
// when startup is interrupted before the old day-only index is removed.
func migrateCommissionSettlementRateIndex(db *gorm.DB) error {
	if db.Migrator().HasIndex(&CommissionSettlement{}, "idx_commission_relation_day") {
		return db.Migrator().DropIndex(&CommissionSettlement{}, "idx_commission_relation_day")
	}
	return nil
}

type CommissionTransfer struct {
	Id             int    `json:"id"`
	UserID         int    `json:"user_id" gorm:"uniqueIndex:idx_commission_transfer_request;index;not null"`
	RequestID      string `json:"request_id" gorm:"type:varchar(64);uniqueIndex:idx_commission_transfer_request;not null"`
	Quota          int    `json:"quota" gorm:"type:bigint"`
	FeeBasisPoints int64  `json:"fee_basis_points"`
	FeeQuota       int    `json:"fee_quota" gorm:"type:bigint"`
	ReceivedQuota  int    `json:"received_quota" gorm:"type:bigint"`
	CreatedAt      int64  `json:"created_at"`
}

type ReferralBindingChange struct {
	Id                int   `json:"id"`
	UserID            int   `json:"user_id" gorm:"index"`
	PreviousInviterID int   `json:"previous_inviter_id"`
	InviterID         int   `json:"inviter_id"`
	AdminID           int   `json:"admin_id"`
	CreatedAt         int64 `json:"created_at"`
}

func ChangeUserInviter(userID, inviterID, adminID, operatorRole int) (*ReferralBindingChange, error) {
	if userID <= 0 || inviterID <= 0 || userID == inviterID {
		return nil, common.NewMessage("Invalid inviter")
	}
	var change ReferralBindingChange
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).First(&user, userID).Error; err != nil {
			return err
		}
		if operatorRole < common.RoleAdminUser || operatorRole != common.RoleRootUser && operatorRole <= user.Role {
			return ErrUserQuotaPermission
		}
		var inviter User
		if err := tx.Select("id", "status").First(&inviter, inviterID).Error; err != nil || inviter.Status != common.UserStatusEnabled {
			return common.NewMessage("Invalid inviter")
		}
		change = ReferralBindingChange{UserID: userID, PreviousInviterID: user.InviterId, InviterID: inviterID, AdminID: adminID, CreatedAt: time.Now().Unix()}
		if user.InviterId == inviterID {
			return nil
		}
		if err := tx.Model(&user).Update("inviter_id", inviterID).Error; err != nil {
			return err
		}
		// Invitation counts are queried from current bindings; historical commission
		// rows retain their own inviter, regardless of subsequent binding changes.
		return tx.Create(&change).Error
	})
	if err != nil {
		return nil, err
	}
	return &change, nil
}

func TransferCommission(userID, quota int, expectedFee int64, requestID string) (*CommissionTransfer, error) {
	if quota <= 0 || len(requestID) < 16 || len(requestID) > 64 {
		return nil, common.NewMessage("Invalid parameters")
	}
	if err := common.ValidateWalletQuota(quota); err != nil {
		return nil, err
	}
	var transfer CommissionTransfer
	credited := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).First(&user, userID).Error; err != nil {
			return err
		}
		err := tx.Where("user_id = ? AND request_id = ?", userID, requestID).First(&transfer).Error
		if err == nil {
			if transfer.Quota != quota || transfer.FeeBasisPoints != expectedFee {
				return common.NewMessage("Transfer request has already been used")
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		feeRate := common.CommissionTransferFeeBasisPoints.Load()
		if feeRate != expectedFee {
			return common.NewMessage("The transfer fee has changed. Please review and confirm again.")
		}
		if user.CommissionQuota < quota {
			return common.NewMessage("Insufficient commission balance")
		}
		// Round the fee up to a quota unit so splitting transfers cannot evade it.
		fee, err := common.WalletQuotaFromDecimalStrict(decimal.NewFromInt(int64(quota)).Mul(decimal.NewFromInt(feeRate)).Div(decimal.NewFromInt(10000)).Ceil())
		if err != nil || fee < 0 || fee > quota {
			return ErrInvalidTopUpQuota
		}
		received := quota - fee
		if received <= 0 {
			return common.NewMessage("The amount received must be greater than zero")
		}
		if err := CreditWalletQuota(tx, userID, received, WalletOrdinary); err != nil {
			return err
		}
		if err := tx.Model(&user).UpdateColumn("commission_quota", gorm.Expr("commission_quota - ?", quota)).Error; err != nil {
			return err
		}
		transfer = CommissionTransfer{UserID: userID, RequestID: requestID, Quota: quota, FeeBasisPoints: feeRate, FeeQuota: fee, ReceivedQuota: received, CreatedAt: time.Now().Unix()}
		if err := tx.Create(&transfer).Error; err != nil {
			return err
		}
		credited = received
		return nil
	})
	if err != nil {
		return nil, err
	}
	syncCreditUserQuotaCache(userID, credited, "commission transfer")
	return &transfer, nil
}

type commissionRelation struct {
	ConsumptionDay int64
	InviterID      int
	UserID         int
}

type CommissionSettlementResult struct {
	Day             int64 `json:"day"`
	Cutoff          int64 `json:"cutoff"`
	RateBasisPoints int64 `json:"rate_basis_points"`
	SettledCharges  int   `json:"settled_charges"`
}

// SettleTodayCommissions pays only finalized, unpaid ordinary-wallet charges
// from Beijing midnight through the captured cutoff, using this run's rate.
// It does not capture the rate of the later automatic daily settlement.
func SettleTodayCommissions(now int64) (*CommissionSettlementResult, error) {
	rate := common.ReferralRateBasisPoints.Load()
	result := &CommissionSettlementResult{Day: BeijingDay(now), Cutoff: now, RateBasisPoints: rate}
	if !common.ReferralEnabled.Load() || !operation_setting.IsPaymentComplianceConfirmed() || rate <= 0 || rate > 10000 {
		return result, common.NewMessage("Enable referral rewards and set a positive rate before settling")
	}
	var groups []commissionRelation
	err := DB.Model(&WalletCharge{}).
		Select("consumption_day, inviter_id, user_id").
		Where("consumption_day = ? AND consumed_at >= ? AND consumed_at <= ? AND inviter_id > 0 AND ordinary_quota > 0 AND refunded_at = 0 AND commission_settlement_id = 0", result.Day, result.Day, now).
		Group("consumption_day, inviter_id, user_id").Order("inviter_id, user_id").Scan(&groups).Error
	if err != nil {
		return result, err
	}
	for _, group := range groups {
		count, err := settleCommissionRelation(group.ConsumptionDay, group.InviterID, group.UserID, now, &rate)
		if err != nil {
			return result, err
		}
		result.SettledCharges += count
	}
	return result, nil
}

// SettleDailyCommissions drains a bounded page of due relationships. Call again
// until it returns zero. Each transaction locks a durable day and stamps every
// included charge, so concurrent nodes and crash/retry cannot credit twice.
func SettleDailyCommissions(now int64, limit int) (int, error) {
	var groups []commissionRelation
	err := DB.Model(&WalletCharge{}).
		Select("consumption_day, inviter_id, user_id").
		Where("consumed_at > 0 AND consumption_day < ? AND inviter_id > 0 AND ordinary_quota > 0 AND refunded_at = 0 AND commission_settlement_id = 0", BeijingDay(now)).
		Group("consumption_day, inviter_id, user_id").Order("consumption_day, inviter_id, user_id").Limit(limit).Scan(&groups).Error
	if err != nil {
		return 0, err
	}
	for _, group := range groups {
		if _, err := settleCommissionRelation(group.ConsumptionDay, group.InviterID, group.UserID, now, nil); err != nil {
			return 0, err
		}
	}
	return len(groups), nil
}

func settleCommissionRelation(day int64, inviterID, userID int, now int64, manualRate *int64) (int, error) {
	settled := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		// Both manual and scheduled workers lock the same durable day before
		// checking charge markers, including workers on different server nodes.
		snapshot := CommissionDay{Day: day}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&snapshot).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).Where("day = ?", day).First(&snapshot).Error; err != nil {
			return err
		}
		rate := snapshot.RateBasisPoints
		if manualRate != nil {
			rate = *manualRate
		} else if snapshot.CreatedAt == 0 {
			rate = common.ReferralRateBasisPoints.Load()
			if !common.ReferralEnabled.Load() || !operation_setting.IsPaymentComplianceConfirmed() {
				rate = 0
			}
			if err := tx.Model(&snapshot).Updates(map[string]any{"rate_basis_points": rate, "created_at": now}).Error; err != nil {
				return err
			}
		}
		var records []WalletCharge
		if err := lockForUpdate(tx).Where("consumption_day = ? AND inviter_id = ? AND user_id = ? AND consumed_at >= ? AND consumed_at <= ? AND ordinary_quota > 0 AND refunded_at = 0 AND commission_settlement_id = 0", day, inviterID, userID, day, now).Find(&records).Error; err != nil {
			return err
		}
		if len(records) == 0 {
			return nil
		}
		settlement := CommissionSettlement{}
		err := tx.Where("day = ? AND inviter_id = ? AND user_id = ? AND rate_basis_points = ?", day, inviterID, userID, rate).First(&settlement).Error
		exists := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if !exists {
			settlement = CommissionSettlement{Day: day, InviterID: inviterID, UserID: userID, RateBasisPoints: rate}
		}
		consumed := decimal.NewFromInt(int64(settlement.ConsumedQuota))
		ids := make([]int, 0, len(records))
		for _, record := range records {
			consumed = consumed.Add(decimal.NewFromInt(int64(record.OrdinaryQuota)))
			ids = append(ids, record.Id)
		}
		amount, err := common.WalletQuotaFromDecimalStrict(consumed)
		if err != nil {
			return err
		}
		reward, err := common.WalletQuotaFromDecimalStrict(consumed.Mul(decimal.NewFromInt(settlement.RateBasisPoints)).Div(decimal.NewFromInt(10000)).Floor())
		if err != nil || reward < settlement.RewardQuota {
			return ErrInvalidTopUpQuota
		}
		var inviter User
		if err := lockForUpdate(tx.Unscoped()).First(&inviter, inviterID).Error; err != nil {
			return err
		}
		// Disabled/soft-deleted accounts keep earned funds and history. Permanent
		// deletion cannot silently discard a commission; the job reports an error.
		if err := tx.Unscoped().Model(&User{}).Where("id = ? AND commission_quota <= ?", inviterID, common.MaxWalletQuota-(reward-settlement.RewardQuota)).
			UpdateColumn("commission_quota", gorm.Expr("commission_quota + ?", reward-settlement.RewardQuota)).Error; err != nil {
			return err
		}
		if inviter.CommissionQuota > common.MaxWalletQuota-(reward-settlement.RewardQuota) {
			return ErrWalletQuotaLimitExceeded
		}
		settlement.ConsumedQuota, settlement.RewardQuota, settlement.SettledAt = amount, reward, now
		if exists {
			err = tx.Save(&settlement).Error
		} else {
			err = tx.Create(&settlement).Error
		}
		if err != nil {
			return err
		}
		// Stay below parameter limits on supported SQLite/PostgreSQL versions.
		for start := 0; start < len(ids); start += 300 {
			if err := tx.Model(&WalletCharge{}).Where("id IN ?", ids[start:min(start+300, len(ids))]).UpdateColumn("commission_settlement_id", settlement.Id).Error; err != nil {
				return err
			}
		}
		settled = len(records)
		return nil
	})
	return settled, err
}
