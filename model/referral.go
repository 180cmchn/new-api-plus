package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"gorm.io/gorm"
)

const (
	RedemptionTypeLegacy       = ""
	RedemptionTypeRecharge     = "recharge"
	RedemptionTypeCompensation = "compensation"
)

// RedemptionRecord is immutable settlement evidence, except for the one-way
// revocation marker. It survives deletion of a code or either user account.
type RedemptionRecord struct {
	Id              int    `json:"id"`
	RedemptionId    int    `json:"redemption_id" gorm:"uniqueIndex"`
	UserId          int    `json:"user_id" gorm:"index"`
	InviterId       int    `json:"inviter_id" gorm:"index"`
	Type            string `json:"type" gorm:"type:varchar(20)"`
	Wallet          string `json:"wallet" gorm:"type:varchar(20);not null;default:''"`
	Quota           int    `json:"quota" gorm:"type:bigint"`
	RateBasisPoints int64  `json:"rate_basis_points"`
	RewardQuota     int    `json:"reward_quota" gorm:"type:bigint"`
	RedeemedAt      int64  `json:"redeemed_at"`
	RevokedAt       int64  `json:"revoked_at" gorm:"not null;default:0"`
	RevokedBy       int    `json:"revoked_by"`
}

// insertWithReferral binds only on creation, in the user creation transaction.
func (user *User) insertWithReferral(tx *gorm.DB, inviterId int) error {
	user.InviterId = 0
	if inviterId > 0 && inviterId != user.Id {
		var inviter User
		err := tx.Select("id").First(&inviter, inviterId).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		if err == nil {
			user.InviterId = inviter.Id
		}
	}
	if err := tx.Create(user).Error; err != nil {
		return err
	}
	if user.InviterId == 0 {
		return nil
	}
	return tx.Model(&User{}).Where("id = ?", user.InviterId).
		UpdateColumn("aff_count", gorm.Expr("aff_count + ?", 1)).Error
}

func settleRedemptionReferral(tx *gorm.DB, redemption *Redemption, userId int) (*RedemptionRecord, error) {
	var user User
	if err := tx.Select("id", "inviter_id").First(&user, userId).Error; err != nil {
		return nil, err
	}
	record := &RedemptionRecord{
		RedemptionId: redemption.Id, UserId: userId, InviterId: user.InviterId,
		Type: redemption.Type, Quota: redemption.Quota, RedeemedAt: common.GetTimestamp(),
	}
	if record.InviterId == userId {
		record.InviterId = 0
	}
	record.Wallet = WalletOrdinary
	if redemption.Type == RedemptionTypeCompensation {
		record.Wallet = WalletGift
	}
	if err := tx.Create(record).Error; err != nil {
		return nil, err
	}
	return record, nil
}

func RevokeRedemption(id, adminId int) error {
	var record RedemptionRecord
	err := DB.Transaction(func(tx *gorm.DB) error {
		var code Redemption
		if err := lockForUpdate(tx.Unscoped()).First(&code, id).Error; err != nil {
			return err
		}
		if code.Status != common.RedemptionCodeStatusUsed {
			return common.NewMessage("Only a used redemption code can be revoked")
		}
		if err := tx.Where("redemption_id = ?", id).First(&record).Error; err != nil {
			if err != gorm.ErrRecordNotFound {
				return err
			}
			// Historical redemptions have no referral award to reverse.
			record = RedemptionRecord{RedemptionId: id, UserId: code.UsedUserId, Type: code.Type, Quota: code.Quota, RedeemedAt: code.RedeemedTime}
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
		}
		if record.RevokedAt != 0 || record.Quota <= 0 || record.RewardQuota < 0 || record.RewardQuota > record.Quota {
			return common.NewMessage("Invalid redemption settlement")
		}
		if err := common.ValidateWalletQuota(record.Quota); err != nil {
			return err
		}
		result := tx.Unscoped().Model(&Redemption{}).Where("id = ? AND status = ?", id, common.RedemptionCodeStatusUsed).
			Update("status", common.RedemptionCodeStatusRevoked)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return common.NewMessage("Only a used redemption code can be revoked")
		}
		// Keep every balance in the JavaScript-safe wallet range while allowing
		// a negative balance. SQL increments preserve concurrent wallet changes.
		column := "quota"
		if record.Wallet == WalletGift {
			column = "gift_quota"
		}
		debits := []struct {
			id, quota int
			column    string
		}{{record.UserId, record.Quota, column}}
		if record.RewardQuota > 0 {
			debits = append(debits, struct {
				id, quota int
				column    string
			}{record.InviterId, record.RewardQuota, "quota"})
		}
		for _, debit := range debits {
			result = tx.Unscoped().Model(&User{}).Where("id = ? AND "+debit.column+" >= ?", debit.id, -common.MaxWalletQuota+debit.quota).
				UpdateColumn(debit.column, gorm.Expr(debit.column+" - ?", debit.quota))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return common.NewMessage("Unable to reverse the wallet credit")
			}
		}
		record.RevokedAt, record.RevokedBy = common.GetTimestamp(), adminId
		return tx.Model(&record).Updates(map[string]any{"revoked_at": record.RevokedAt, "revoked_by": adminId}).Error
	})
	if err != nil {
		return err
	}
	if record.Wallet != WalletGift {
		if err := cacheDecrUserQuota(record.UserId, int64(record.Quota)); err != nil {
			common.SysError(err.Error())
		}
	}
	RecordLog(record.UserId, LogTypeSystem, common.NewMessage("Redemption credit reversed: {{quota}}", map[string]any{"quota": logger.FormatQuota(record.Quota)}))
	if record.RewardQuota > 0 {
		if err := cacheDecrUserQuota(record.InviterId, int64(record.RewardQuota)); err != nil {
			common.SysError(err.Error())
		}
		RecordLog(record.InviterId, LogTypeSystem, common.NewMessage("Referral reward reversed: {{quota}}", map[string]any{"quota": logger.FormatQuota(record.RewardQuota)}))
	}
	return nil
}

type ReferralSummary struct {
	InviteCount int64 `json:"invite_count"`
	RewardQuota int64 `json:"reward_quota"`
}

// loadCurrentInviteCounts keeps legacy user-list/profile fields consistent with
// the referral summary after an administrator rebinds an existing account.
func loadCurrentInviteCounts(tx *gorm.DB, users []*User) error {
	if len(users) == 0 {
		return nil
	}
	ids := make([]int, 0, len(users))
	byID := make(map[int]*User, len(users))
	for _, user := range users {
		ids = append(ids, user.Id)
		byID[user.Id] = user
		user.AffCount = 0
	}
	var counts []struct {
		InviterID int
		Total     int
	}
	if err := tx.Unscoped().Model(&User{}).Select("inviter_id, COUNT(*) AS total").
		Where("inviter_id IN ?", ids).Group("inviter_id").Scan(&counts).Error; err != nil {
		return err
	}
	for _, count := range counts {
		byID[count.InviterID].AffCount = count.Total
	}
	return nil
}

func GetReferralSummary(userId int) (ReferralSummary, error) {
	var summary ReferralSummary
	if err := DB.Unscoped().Model(&User{}).Where("inviter_id = ?", userId).Count(&summary.InviteCount).Error; err != nil {
		return summary, err
	}
	err := DB.Model(&RedemptionRecord{}).Select("COALESCE(SUM(reward_quota), 0)").
		Where("inviter_id = ? AND revoked_at = 0", userId).Scan(&summary.RewardQuota).Error
	if err != nil {
		return summary, err
	}
	var commissions int64
	err = DB.Model(&CommissionSettlement{}).Select("COALESCE(SUM(reward_quota), 0)").Where("inviter_id = ?", userId).Scan(&commissions).Error
	summary.RewardQuota += commissions
	return summary, err
}

type ReferralDetail struct {
	InviterId     int    `json:"inviter_id"`
	InviterName   string `json:"inviter_name"`
	UserId        int    `json:"user_id"`
	Username      string `json:"username"`
	Current       bool   `json:"current"`
	ConsumedQuota int64  `json:"consumed_quota"`
	RewardQuota   int64  `json:"reward_quota"`
}

func GetReferralDetails(keyword string, offset, limit int) ([]ReferralDetail, int64, error) {
	// UNION retains old earning relationships after an administrator rebinds a
	// user; a current binding without spending remains visible with zero totals.
	relations := DB.Raw(`SELECT inviter_id, id AS user_id FROM users WHERE inviter_id > 0
 UNION SELECT inviter_id, user_id FROM wallet_charges WHERE consumed_at > 0 AND inviter_id > 0 AND refunded_at = 0
 UNION SELECT inviter_id, user_id FROM commission_settlements
 UNION SELECT inviter_id, user_id FROM redemption_records WHERE inviter_id > 0 AND reward_quota > 0`)
	query := DB.Table("(?) AS relation", relations).
		Joins("LEFT JOIN users AS invitee ON invitee.id = relation.user_id").
		Joins("LEFT JOIN users AS inviter ON inviter.id = relation.inviter_id")
	if keyword != "" {
		query = query.Where("inviter.username LIKE ? OR invitee.username LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var details []ReferralDetail
	err := query.Select(`relation.inviter_id, COALESCE(inviter.username, '') AS inviter_name,
 relation.user_id, COALESCE(invitee.username, '') AS username,
 CASE WHEN invitee.inviter_id = relation.inviter_id THEN 1 ELSE 0 END AS current,
 (SELECT COALESCE(SUM(c.ordinary_quota), 0) FROM wallet_charges c WHERE c.user_id = relation.user_id AND c.inviter_id = relation.inviter_id AND c.consumed_at > 0 AND c.refunded_at = 0) AS consumed_quota,
 (SELECT COALESCE(SUM(s.reward_quota), 0) FROM commission_settlements s WHERE s.user_id = relation.user_id AND s.inviter_id = relation.inviter_id) +
 (SELECT COALESCE(SUM(r.reward_quota), 0) FROM redemption_records r WHERE r.user_id = relation.user_id AND r.inviter_id = relation.inviter_id AND r.revoked_at = 0) AS reward_quota`).
		Order("relation.inviter_id DESC, relation.user_id DESC").Offset(offset).Limit(limit).Scan(&details).Error
	return details, total, err
}

func GetReferralRecords(userId, inviterId, offset, limit int) ([]CommissionSettlement, int64, error) {
	query := DB.Model(&CommissionSettlement{}).Where("user_id = ?", userId)
	if inviterId > 0 {
		query = query.Where("inviter_id = ?", inviterId)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var records []CommissionSettlement
	err := query.Order("day DESC, id DESC").Offset(offset).Limit(limit).Find(&records).Error
	return records, total, err
}
