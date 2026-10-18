package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func SettleTodayCommissions(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	result, err := model.SettleTodayCommissions(time.Now().Unix())
	params := map[string]any{
		"day": result.Day, "cutoff": result.Cutoff, "rate_basis_points": result.RateBasisPoints,
		"settled_charges": result.SettledCharges, "completed": err == nil,
	}
	if err != nil {
		common.ApiError(c, err)
	} else {
		common.ApiSuccess(c, result)
	}
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), auditContentEN("referral.settle_today", params), c.ClientIP(), "referral.settle_today", params, auditOperatorInfo(c), &model.AuditRequestInfo{
		Method: c.Request.Method, Route: c.FullPath(), Path: c.FullPath(), Status: c.Writer.Status(), Success: err == nil,
	}, c)
	markAuditLogged(c)
}

func GetSelfReferral(c *gin.Context) {
	summary, err := model.GetReferralSummary(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, summary)
}

func GetReferralDetails(c *gin.Context) {
	page := common.GetPageQuery(c)
	rows, total, err := model.GetReferralDetails(c.Query("keyword"), page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(rows)
	common.ApiSuccess(c, page)
}

func GetReferralRecords(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	page := common.GetPageQuery(c)
	inviterId, _ := strconv.Atoi(c.Query("inviter_id"))
	rows, total, err := model.GetReferralRecords(userId, inviterId, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(rows)
	common.ApiSuccess(c, page)
}

func RevokeRedemption(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if err := model.RevokeRedemption(id, c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "redemption.revoke", map[string]any{"redemption_id": id})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func GetSelfWallets(c *gin.Context) {
	user, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"ordinary_quota": user.Quota, "commission_quota": user.CommissionQuota, "gift_quota": user.GiftQuota,
		"wallet_order":              model.NormalizeWalletOrder(user.GetSetting().WalletOrder),
		"transfer_fee_basis_points": common.CommissionTransferFeeBasisPoints.Load(),
	})
}

func UpdateWalletOrder(c *gin.Context) {
	var req struct {
		Order []string `json:"wallet_order"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if err := model.UpdateWalletOrder(c.GetInt("id"), req.Order); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func TransferCommission(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	var req struct {
		Quota          int    `json:"quota"`
		FeeBasisPoints int64  `json:"fee_basis_points"`
		RequestID      string `json:"request_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	transfer, err := model.TransferCommission(c.GetInt("id"), req.Quota, req.FeeBasisPoints, req.RequestID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "wallet.commission_transfer", map[string]any{"transfer_id": transfer.Id, "quota": transfer.Quota, "fee_quota": transfer.FeeQuota})
	common.ApiSuccess(c, transfer)
}

func ChangeUserInviter(c *gin.Context) {
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil || userID <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	var req struct {
		InviterID int `json:"inviter_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	change, err := model.ChangeUserInviter(userID, req.InviterID, c.GetInt("id"), c.GetInt("role"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "user.inviter_change", map[string]any{"user_id": userID, "previous_inviter_id": change.PreviousInviterID, "inviter_id": change.InviterID})
	common.ApiSuccess(c, change)
}

func GetCommissionTransfers(c *gin.Context) {
	page := common.GetPageQuery(c)
	query := model.DB.Model(&model.CommissionTransfer{})
	if userID, err := strconv.Atoi(c.Query("user_id")); err == nil && userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var rows []model.CommissionTransfer
	if err := query.Order("id DESC").Offset(page.GetStartIdx()).Limit(page.GetPageSize()).Find(&rows).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(rows)
	common.ApiSuccess(c, page)
}
