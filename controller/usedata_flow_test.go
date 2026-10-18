package controller

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type flowQuotaResponse struct {
	Success bool                  `json:"success"`
	Message string                `json:"message"`
	Data    []model.FlowQuotaData `json:"data"`
}

func setupFlowControllerTestDB(t *testing.T) {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.QuotaData{}))
	require.NoError(t, model.DB.Create(&model.Channel{Id: 1, Name: "east"}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 11, UserId: 1, Key: "sk-primary", Name: "primary"}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 22, UserId: 2, Key: "sk-backup", Name: "backup"}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:    1,
		Username:  "alice",
		NodeName:  "node-a",
		TokenID:   11,
		UseGroup:  "default",
		ChannelID: 1,
		ModelName: "gpt-a",
		CreatedAt: 1100,
		Count:     2,
		Quota:     100,
		TokenUsed: 40,
	}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:    2,
		Username:  "bob",
		NodeName:  "node-b",
		TokenID:   22,
		UseGroup:  "vip",
		ChannelID: 1,
		ModelName: "gpt-b",
		CreatedAt: 1200,
		Count:     1,
		Quota:     70,
		TokenUsed: 30,
	}).Error)
}

func decodeFlowQuotaResponse(t *testing.T, recorder *httptest.ResponseRecorder) flowQuotaResponse {
	t.Helper()
	require.Equal(t, http.StatusOK, recorder.Code)
	var payload flowQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success, payload.Message)
	return payload
}

func TestGetAllFlowQuotaDatesUsesAdminDimensions(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow?start_timestamp=1000&end_timestamp=2000&username=bob", nil)

	GetAllFlowQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "bob", payload.Data[0].Username)
	require.Equal(t, "vip", payload.Data[0].UseGroup)
	require.Equal(t, "east", payload.Data[0].ChannelName)
	require.Empty(t, payload.Data[0].TokenName)
	require.Empty(t, payload.Data[0].NodeName)
}

func TestGetAllFlowQuotaDatesUsesRootDimensions(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleRootUser)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow?start_timestamp=1000&end_timestamp=2000&username=alice", nil)

	GetAllFlowQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "alice", payload.Data[0].Username)
	require.Equal(t, "node-a", payload.Data[0].NodeName)
	require.Equal(t, "primary", payload.Data[0].TokenName)
	require.Equal(t, "default", payload.Data[0].UseGroup)
	require.Equal(t, "east", payload.Data[0].ChannelName)
}

func TestGetUserFlowQuotaDatesRestrictsToAuthenticatedUser(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow/self?start_timestamp=1000&end_timestamp=2000", nil)

	GetUserFlowQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Empty(t, payload.Data[0].Username)
	require.Equal(t, "primary", payload.Data[0].TokenName)
	require.Equal(t, "default", payload.Data[0].UseGroup)
	require.Empty(t, payload.Data[0].ChannelName)
}

func TestGetUserFlowQuotaDatesRejectsInvalidTimeRange(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow/self?start_timestamp=bad&end_timestamp=2000", nil)

	GetUserFlowQuotaDates(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload flowQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.False(t, payload.Success)
	require.Equal(t, "invalid start_timestamp", payload.Message)
}

type tokenSummaryResponse struct {
	Success bool `json:"success"`
	Data    struct {
		TotalTokens int64 `json:"total_tokens"`
	} `json:"data"`
}

func requestTokenSummary(t *testing.T, userID int) (int, tokenSummaryResponse) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", userID)
	ctx.Request = httptest.NewRequest(http.MethodGet,
		"/api/data/self/summary?user_id=2&username=bob&start_timestamp=9999999", nil)
	GetUserTokenSummary(ctx)
	var payload tokenSummaryResponse
	if recorder.Body.Len() > 0 {
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	}
	return recorder.Code, payload
}

func TestUserTokenSummaryDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			versionQuery := "SELECT version()"
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(filepath.Join(t.TempDir(), "tokens.db"))
				versionQuery = "SELECT sqlite_version()"
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				driver = postgres.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			oldDB := model.DB
			model.DB = db
			model.CacheQuotaDataLock.Lock()
			oldCache := model.CacheQuotaData
			model.CacheQuotaData = make(map[string]*model.QuotaData)
			model.CacheQuotaDataLock.Unlock()
			t.Cleanup(func() {
				model.DB = oldDB
				model.CacheQuotaDataLock.Lock()
				model.CacheQuotaData = oldCache
				model.CacheQuotaDataLock.Unlock()
			})
			// Use only the disposable test databases supplied through TEST_*_DSN.
			require.NoError(t, db.Migrator().DropTable(&model.QuotaData{}))
			require.NoError(t, db.AutoMigrate(&model.QuotaData{}))
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database version: %s", version)
			status, payload := requestTokenSummary(t, 0)
			assert.Equal(t, http.StatusUnauthorized, status)
			assert.False(t, payload.Success)
			status, payload = requestTokenSummary(t, 1)
			require.Equal(t, http.StatusOK, status)
			require.True(t, payload.Success)
			assert.Zero(t, payload.Data.TotalTokens)

			rows := []model.QuotaData{
				{UserID: 1, Username: "old-name", ModelName: "legacy", CreatedAt: 1, TokenUsed: 2_000_000_000},
				{UserID: 1, Username: "alice", ModelName: "new-model", CreatedAt: 3600, TokenUsed: 2_000_000_000},
				{UserID: 1, Username: "alice", ModelName: "recent", CreatedAt: 7200, TokenUsed: 45},
				{UserID: 2, Username: "bob", ModelName: "other", CreatedAt: 7200, TokenUsed: 9000},
			}
			require.NoError(t, db.Create(&rows).Error)
			model.LogQuotaData(model.QuotaDataLogParams{UserID: 1, Username: "alice", ModelName: "recent", CreatedAt: 7201, TokenUsed: 25})
			model.LogQuotaData(model.QuotaDataLogParams{UserID: 2, Username: "bob", ModelName: "other", CreatedAt: 7201, TokenUsed: 7777})
			model.LogQuotaData(model.QuotaDataLogParams{UserID: 3, Username: "cache-only", ModelName: "recent", CreatedAt: 7201, TokenUsed: 80})
			status, payload = requestTokenSummary(t, 1)
			require.Equal(t, http.StatusOK, status)
			require.True(t, payload.Success)
			assert.Equal(t, int64(4_000_000_070), payload.Data.TotalTokens)
			_, cachedOnly := requestTokenSummary(t, 3)
			require.True(t, cachedOnly.Success)
			assert.Equal(t, int64(80), cachedOnly.Data.TotalTokens)

			// Both an update of an existing hour and creation of a new hour preserve
			// the sum; repeated flushing must not count either row a second time.
			model.SaveQuotaDataCache()
			model.SaveQuotaDataCache()
			_, payload = requestTokenSummary(t, 1)
			require.True(t, payload.Success)
			assert.Equal(t, int64(4_000_000_070), payload.Data.TotalTokens)
			_, cachedOnly = requestTokenSummary(t, 3)
			assert.Equal(t, int64(80), cachedOnly.Data.TotalTokens)
			model.LogQuotaData(model.QuotaDataLogParams{UserID: 1, CreatedAt: 10801, TokenUsed: 7})
			_, payload = requestTokenSummary(t, 1)
			assert.Equal(t, int64(4_000_000_077), payload.Data.TotalTokens)
			_, empty := requestTokenSummary(t, 4)
			require.True(t, empty.Success)
			assert.Zero(t, empty.Data.TotalTokens)

			require.NoError(t, db.Migrator().DropTable(&model.QuotaData{}))
			_, failed := requestTokenSummary(t, 1)
			assert.False(t, failed.Success, "database failure must not appear as a valid zero total")
		})
	}
}
