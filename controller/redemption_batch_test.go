package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestDeleteRedemptionBatch(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver, logDriver gorm.Dialector
			dbType := common.DatabaseTypeSQLite
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
				logDriver = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				driver = mysql.Open(dsn)
				logDSN := os.Getenv("TEST_MYSQL_LOG_DSN")
				if logDSN == "" {
					logDSN = dsn
				}
				logDriver = mysql.Open(logDSN)
				dbType = common.DatabaseTypeMySQL
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				driver = postgres.Open(dsn)
				logDSN := os.Getenv("TEST_POSTGRES_LOG_DSN")
				if logDSN == "" {
					logDSN = dsn
				}
				logDriver = postgres.Open(logDSN)
				dbType = common.DatabaseTypePostgreSQL
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			var version string
			query := "SELECT version()"
			if dialect == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("database version: %s", version)

			logDB, err := gorm.Open(logDriver, &gorm.Config{})
			require.NoError(t, err)
			logSQL, err := logDB.DB()
			require.NoError(t, err)
			logSQL.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, logSQL.Close()) })
			previousDB, previousLogDB := model.DB, model.LOG_DB
			previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
			previousRedis := common.RedisEnabled
			model.DB, model.LOG_DB = db, logDB
			common.SetDatabaseTypes(dbType, dbType)
			common.RedisEnabled = false
			t.Cleanup(func() {
				model.DB, model.LOG_DB = previousDB, previousLogDB
				common.SetDatabaseTypes(previousMain, previousLog)
				common.RedisEnabled = previousRedis
			})
			for _, table := range []any{&model.User{}, &model.Redemption{}} {
				require.False(t, db.Migrator().HasTable(table), "use an empty test database")
				require.NoError(t, db.AutoMigrate(table))
				t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(table)) })
			}
			require.False(t, logDB.Migrator().HasTable(&model.AuditLog{}), "use an empty test log database")
			require.NoError(t, logDB.AutoMigrate(&model.AuditLog{}))
			t.Cleanup(func() { require.NoError(t, logDB.Migrator().DropTable(&model.AuditLog{})) })
			token := "redemption-audit-test-token"
			admin := model.User{Username: "redemption-audit-admin", Password: "unused", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Group: "default", AccessToken: &token}
			require.NoError(t, db.Create(&admin).Error)
			codes := make([]model.Redemption, 16)
			for index := range codes {
				codes[index] = model.Redemption{Name: "selected", Key: fmt.Sprintf("%032d", index+1), Quota: 100, Status: common.RedemptionCodeStatusEnabled}
			}
			codes[1].Status = common.RedemptionCodeStatusUsed
			codes[15].Name = "unselected"
			codes[15].Status = common.RedemptionCodeStatusDisabled
			require.NoError(t, model.DB.Create(&codes).Error)
			router := gin.New()
			router.Use(middleware.RequestId())
			router.POST("/api/redemption/batch", middleware.AdminAuth(), DeleteRedemptionBatch)

			overLimit := make([]int, 1001)
			for index := range overLimit {
				overLimit[index] = codes[0].Id
			}
			oversized, err := common.Marshal(map[string]any{"ids": overLimit})
			require.NoError(t, err)
			for _, body := range []string{"{}", `{"ids":[]}`, `{"ids":null}`, `{"ids":[0]}`, `{"ids":[1,-1]}`, `{"ids":["1"]}`, "{", string(oversized)} {
				t.Run("invalid_"+body[:min(len(body), 30)], func(t *testing.T) {
					response := httptest.NewRecorder()
					request := httptest.NewRequest(http.MethodPost, "/api/redemption/batch", bytes.NewBufferString(body))
					request.Header.Set("Authorization", "Bearer "+token)
					router.ServeHTTP(response, request)
					var result struct {
						Success bool `json:"success"`
					}
					require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
					assert.False(t, result.Success)
					var count int64
					require.NoError(t, model.DB.Model(&model.Redemption{}).Count(&count).Error)
					assert.EqualValues(t, 16, count)
					var events []model.AuditLog
					require.NoError(t, logDB.Where("request_id = ? AND category = ?", response.Header().Get(common.RequestIdKey), model.AuditCategoryOperation).Find(&events).Error)
					require.Len(t, events, 1)
					assert.False(t, events[0].Success)
					assert.Equal(t, "redemption.delete_batch", events[0].Action)
				})
			}
			_, err = model.BatchDeleteRedemptions(nil)
			require.Error(t, err)
			requestedIDs := make([]int, 0, 17)
			for _, code := range codes[:15] {
				requestedIDs = append(requestedIDs, code.Id)
			}
			requestedIDs = append(requestedIDs, codes[0].Id, 999999)
			payload, err := common.Marshal(map[string]any{"ids": requestedIDs})
			require.NoError(t, err)
			for _, expectedCount := range []int64{15, 0} {
				response := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, "/api/redemption/batch", bytes.NewReader(payload))
				request.Header.Set("Authorization", "Bearer "+token)
				router.ServeHTTP(response, request)
				assert.Equal(t, http.StatusOK, response.Code)
				var result struct {
					Success bool  `json:"success"`
					Data    int64 `json:"data"`
				}
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
				assert.True(t, result.Success)
				assert.Equal(t, expectedCount, result.Data)
				var events []model.AuditLog
				require.NoError(t, logDB.Where("request_id = ? AND category = ?", response.Header().Get(common.RequestIdKey), model.AuditCategoryOperation).Find(&events).Error)
				require.Len(t, events, 1, "one operation event, without a duplicate single-delete fallback")
				event := events[0]
				assert.Equal(t, "redemption.delete_batch", event.Action)
				assert.Equal(t, fmt.Sprintf("Batch deleted %d redemption codes", expectedCount), event.Content)
				assert.True(t, event.Success)
				assert.Equal(t, admin.Id, event.UserId)
				assert.Equal(t, "/api/redemption/batch", event.Route)
				require.NotNil(t, event.Other.Op)
				encoded, err := common.Marshal(event.Other.Op.Params)
				require.NoError(t, err)
				var params struct {
					Count int64 `json:"count"`
					Total int   `json:"total"`
					IDs   []int `json:"requested_redemption_ids"`
				}
				require.NoError(t, common.Unmarshal(encoded, &params))
				assert.Equal(t, expectedCount, params.Count)
				assert.Equal(t, len(requestedIDs), params.Total)
				assert.Equal(t, requestedIDs, params.IDs)
				encoded, err = common.Marshal(event)
				require.NoError(t, err)
				assert.NotContains(t, string(encoded), token)
				for _, code := range codes {
					assert.NotContains(t, string(encoded), code.Key)
				}
			}
			var active []model.Redemption
			require.NoError(t, model.DB.Find(&active).Error)
			require.Len(t, active, 1)
			assert.Equal(t, codes[15], active[0])
			var all []model.Redemption
			require.NoError(t, model.DB.Unscoped().Order("id").Find(&all).Error)
			require.Len(t, all, 16)
			for _, code := range all[:15] {
				assert.True(t, code.DeletedAt.Valid)
			}
			assert.False(t, all[15].DeletedAt.Valid)
		})
	}
}

func TestReferralEndpointAuthorizationAndPrivacy(t *testing.T) {
	admin, _ := setupAccessTokenAudit(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Redemption{}, &model.RedemptionRecord{}, &model.WalletCharge{}, &model.CommissionSettlement{}, &model.CommissionTransfer{}, &model.ReferralBindingChange{}))
	inviter := model.User{Username: "referral-user", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1, AffCode: "ref-a"}
	require.NoError(t, model.DB.Create(&inviter).Error)
	invitee := model.User{Username: "private-invitee", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1, AffCode: "ref-b", InviterId: inviter.Id}
	require.NoError(t, model.DB.Create(&invitee).Error)
	code := model.Redemption{Key: "usable-code-must-not-be-exposed", Name: "test", Type: model.RedemptionTypeRecharge, Quota: 500, Status: common.RedemptionCodeStatusUsed, UsedUserId: invitee.Id}
	require.NoError(t, model.DB.Create(&code).Error)
	record := model.RedemptionRecord{RedemptionId: code.Id, UserId: invitee.Id, InviterId: inviter.Id, Type: model.RedemptionTypeRecharge, Quota: 500, RateBasisPoints: 1000, RewardQuota: 50}
	require.NoError(t, model.DB.Create(&record).Error)
	_, adminToken := createAccessTokenTestSession(t, admin.Id, "referral-admin-session")
	_, userToken := createAccessTokenTestSession(t, inviter.Id, "referral-user-session")
	root := model.User{Username: "referral-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AuthVersion: 1}
	require.NoError(t, model.DB.Create(&root).Error)
	_, rootToken := createAccessTokenTestSession(t, root.Id, "referral-root-session")
	router := gin.New()
	router.Use(middleware.RequestId())
	router.GET("/api/user/referral", middleware.UserAuth(), GetSelfReferral)
	router.GET("/api/referral/", middleware.RootAuth(), GetReferralDetails)
	router.GET("/api/referral/:id/records", middleware.RootAuth(), GetReferralRecords)
	router.POST("/api/redemption/:id/revoke", middleware.AdminAuth(), RevokeRedemption)
	router.GET("/api/user/wallet", middleware.UserAuth(), GetSelfWallets)
	router.PUT("/api/user/wallet/order", middleware.UserAuth(), UpdateWalletOrder)
	router.POST("/api/user/wallet/transfer", middleware.UserAuth(), TransferCommission)
	router.PUT("/api/user/:id/inviter", middleware.AdminAuth(), ChangeUserInviter)
	router.GET("/api/referral/transfers", middleware.RootAuth(), GetCommissionTransfers)
	paths := []struct{ method, path string }{
		{http.MethodGet, "/api/referral/"},
		{http.MethodGet, fmt.Sprintf("/api/referral/%d/records", invitee.Id)},
		{http.MethodPost, fmt.Sprintf("/api/redemption/%d/revoke", code.Id)},
	}
	for _, endpoint := range paths {
		for _, credential := range []struct {
			token  string
			status int
		}{{"", http.StatusUnauthorized}, {"invalid", http.StatusUnauthorized}, {userToken, http.StatusForbidden}} {
			response := accessTokenRequest(router, endpoint.method, endpoint.path, credential.token, "", "")
			assert.Equal(t, credential.status, response.Code)
			assert.NotContains(t, response.Body.String(), invitee.Username)
		}
	}
	response := accessTokenRequest(router, http.MethodGet, "/api/user/referral?id=999", userToken, "", "")
	require.Equal(t, http.StatusOK, response.Code)
	var summary struct {
		Success bool
		Data    map[string]any
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &summary))
	assert.True(t, summary.Success)
	assert.Equal(t, map[string]any{"invite_count": float64(1), "reward_quota": float64(50)}, summary.Data)
	for _, endpoint := range paths[:2] {
		response = accessTokenRequest(router, endpoint.method, endpoint.path, adminToken, "", "")
		assert.Equal(t, http.StatusForbidden, response.Code)
		response = accessTokenRequest(router, endpoint.method, endpoint.path, rootToken, "", "")
		assert.Equal(t, http.StatusOK, response.Code)
		assert.NotContains(t, response.Body.String(), code.Key)
		var body struct{ Success bool }
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
		assert.True(t, body.Success)
	}
	restricted, _ := createScopedAccessToken(t, admin.Id, 0, "profile:read")
	response = accessTokenRequest(router, http.MethodGet, "/api/referral/", restricted, "", "")
	assert.Equal(t, http.StatusForbidden, response.Code)
	response = accessTokenRequest(router, http.MethodPost, paths[2].path, adminToken, "", "")
	require.Equal(t, http.StatusOK, response.Code)
	var result struct{ Success bool }
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	require.True(t, result.Success, response.Body.String())
	require.NoError(t, model.DB.First(&inviter, inviter.Id).Error)
	require.NoError(t, model.DB.First(&invitee, invitee.Id).Error)
	assert.Equal(t, -50, inviter.Quota)
	assert.Equal(t, -500, invitee.Quota)
	response = accessTokenRequest(router, http.MethodPost, paths[2].path, adminToken, "", "")
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	assert.False(t, result.Success)
	var events []model.AuditLog
	require.NoError(t, model.LOG_DB.Where("action = ? AND success = ?", "redemption.revoke", true).Find(&events).Error)
	require.Len(t, events, 1)

	// Wallet mutations are scoped to the authenticated account, even when the
	// caller supplies another user ID. Commission details remain root-only.
	oldPayment := *operation_setting.GetPaymentSetting()
	oldFee := common.CommissionTransferFeeBasisPoints.Load()
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	common.CommissionTransferFeeBasisPoints.Store(1000)
	t.Cleanup(func() {
		*operation_setting.GetPaymentSetting() = oldPayment
		common.CommissionTransferFeeBasisPoints.Store(oldFee)
	})
	require.NoError(t, model.DB.Model(&inviter).Updates(map[string]any{"commission_quota": 1000, "gift_quota": 500}).Error)
	response = accessTokenRequest(router, http.MethodGet, fmt.Sprintf("/api/user/wallet?id=%d", invitee.Id), userToken, "", "")
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &summary))
	assert.Equal(t, float64(1000), summary.Data["commission_quota"])
	for _, token := range []string{userToken, adminToken} {
		response = accessTokenRequest(router, http.MethodGet, "/api/referral/transfers", token, "", "")
		assert.Equal(t, http.StatusForbidden, response.Code)
	}
	response = accessTokenRequest(router, http.MethodPut, "/api/user/wallet/order", userToken, "", `{"wallet_order":[]}`)
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	assert.True(t, result.Success)
	response = accessTokenRequest(router, http.MethodPut, "/api/user/wallet/order", userToken, "", `{"wallet_order":["ordinary","ordinary"]}`)
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	assert.False(t, result.Success)
	transferBody := fmt.Sprintf(`{"quota":100,"fee_basis_points":1000,"request_id":"wallet-http-request","user_id":%d}`, invitee.Id)
	for range 2 {
		response = accessTokenRequest(router, http.MethodPost, "/api/user/wallet/transfer", userToken, "", transferBody)
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		require.True(t, result.Success, response.Body.String())
	}
	require.NoError(t, model.DB.First(&inviter, inviter.Id).Error)
	require.NoError(t, model.DB.First(&invitee, invitee.Id).Error)
	assert.Equal(t, 40, inviter.Quota)
	assert.Equal(t, 900, inviter.CommissionQuota)
	assert.Equal(t, 500, inviter.GiftQuota)
	assert.Equal(t, -500, invitee.Quota)
	response = accessTokenRequest(router, http.MethodPost, "/api/user/wallet/transfer", restricted, "", transferBody)
	assert.Equal(t, http.StatusForbidden, response.Code)
	bindPath := fmt.Sprintf("/api/user/%d/inviter", invitee.Id)
	bindBody := fmt.Sprintf(`{"inviter_id":%d}`, admin.Id)
	response = accessTokenRequest(router, http.MethodPut, bindPath, userToken, "", bindBody)
	assert.Equal(t, http.StatusForbidden, response.Code)
	response = accessTokenRequest(router, http.MethodPut, bindPath, adminToken, "", bindBody)
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	assert.True(t, result.Success, response.Body.String())
	require.NoError(t, model.DB.First(&invitee, invitee.Id).Error)
	assert.Equal(t, admin.Id, invitee.InviterId)
	response = accessTokenRequest(router, http.MethodPut, fmt.Sprintf("/api/user/%d/inviter", root.Id), adminToken, "", bindBody)
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	assert.False(t, result.Success, "an administrator cannot change a root account")
}
