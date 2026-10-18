package router

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTodayCommissionSettlementRequiresAdminSession(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.UserAccessToken{}, &model.Option{}, &model.AuditLog{}, &model.WalletCharge{}, &model.CommissionDay{}, &model.CommissionSettlement{}))
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldMainKind, oldLogKind := common.MainDatabaseType(), common.LogDatabaseType()
	oldSecret, oldRedis := common.SessionSecret, common.RedisEnabled
	oldEnabled, oldRate := common.ReferralEnabled.Load(), common.ReferralRateBasisPoints.Load()
	payment := operation_setting.GetPaymentSetting()
	oldPayment := *payment
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.SessionSecret, common.RedisEnabled = "manual-commission-route-test-secret", false
	common.ReferralEnabled.Store(true)
	common.ReferralRateBasisPoints.Store(1000)
	payment.ComplianceConfirmed, payment.ComplianceTermsVersion = true, operation_setting.CurrentComplianceTermsVersion
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.SetDatabaseTypes(oldMainKind, oldLogKind)
		common.SessionSecret, common.RedisEnabled = oldSecret, oldRedis
		common.ReferralEnabled.Store(oldEnabled)
		common.ReferralRateBasisPoints.Store(oldRate)
		*payment = oldPayment
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, model.EnsureLegacyAccessTokenRetireAt(time.Now().Unix()))
	users := []model.User{
		{Username: "settlement-user", AffCode: "settlement-user", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1},
		{Username: "settlement-admin", AffCode: "settlement-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AuthVersion: 1},
		{Username: "settlement-root", AffCode: "settlement-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AuthVersion: 1},
	}
	require.NoError(t, db.Create(&users).Error)
	bundles := make([]*service.AuthBundle, len(users))
	for i := range users {
		bundles[i], err = service.CreateLoginSession(users[i].Id, "password", "127.0.0.1", "commission-route-test")
		require.NoError(t, err)
	}
	legacyToken := "manual-commission-admin-legacy-token"
	require.NoError(t, db.Model(&users[1]).Update("access_token", legacyToken).Error)
	revoked, err := service.CreateLoginSession(users[1].Id, "password", "127.0.0.1", "revoked-session")
	require.NoError(t, err)
	_, err = model.RevokeUserSession(users[1].Id, revoked.Session.SID, "test")
	require.NoError(t, err)
	expired, err := service.CreateLoginSession(users[1].Id, "password", "127.0.0.1", "expired-session")
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.UserSession{}).Where("sid = ?", expired.Session.SID).Update("expires_at", time.Now().Unix()-1).Error)
	now := time.Now().Unix()
	charge := model.WalletCharge{RequestID: "admin-only-settlement", UserID: users[0].Id, InviterID: users[2].Id, OrdinaryQuota: 100, ConsumptionDay: model.BeijingDay(now), ConsumedAt: now}
	require.NoError(t, db.Create(&charge).Error)
	engine := gin.New()
	SetApiRouter(engine)
	for _, tc := range []struct {
		name   string
		token  string
		status int
		paid   int
	}{
		{name: "anonymous", status: http.StatusUnauthorized},
		{name: "ordinary user", token: bundles[0].AccessToken, status: http.StatusForbidden},
		{name: "admin personal token", token: legacyToken, status: http.StatusForbidden},
		{name: "revoked admin session", token: revoked.AccessToken, status: http.StatusUnauthorized},
		{name: "expired admin session", token: expired.AccessToken, status: http.StatusUnauthorized},
		{name: "admin session", token: bundles[1].AccessToken, status: http.StatusOK, paid: 1},
		{name: "root session replay", token: bundles[2].AccessToken, status: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/referral/settle_today", strings.NewReader(`{}`))
			request.Header.Set("Content-Type", "application/json")
			if tc.token != "" {
				request.Header.Set("Authorization", "Bearer "+tc.token)
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			if tc.status == http.StatusOK {
				var body struct {
					Success bool                             `json:"success"`
					Data    model.CommissionSettlementResult `json:"data"`
				}
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
				assert.True(t, body.Success, response.Body.String())
				assert.Equal(t, tc.paid, body.Data.SettledCharges)
			}
		})
	}
	var inviter model.User
	require.NoError(t, db.First(&inviter, users[2].Id).Error)
	assert.Equal(t, 10, inviter.CommissionQuota)
	var audit model.AuditLog
	require.NoError(t, db.Where("action = ?", "referral.settle_today").Order("id DESC").First(&audit).Error)
	assert.True(t, audit.Success)
	assert.NotContains(t, audit.Content, bundles[2].AccessToken)
}

// accessTokenExemptRoutes are /api routes that never reach dashboard
// authentication, so a personal access token has no rule to satisfy there.
var accessTokenExemptRoutes = []string{
	// Anonymous pages and setup.
	"GET /api/setup",
	"POST /api/setup",
	"GET /api/status",
	"GET /api/uptime/status",
	"GET /api/notice",
	"GET /api/user-agreement",
	"GET /api/privacy-policy",
	"GET /api/about",
	"GET /api/home_page_content",
	"GET /api/ratio_config",
	"GET /api/user/groups",

	// Anonymous account entry points.
	"GET /api/verification",
	"GET /api/reset_password",
	"POST /api/user/reset",
	"POST /api/user/register",
	"GET /api/user/login/encryption-key",
	"POST /api/user/login",
	"POST /api/user/login/2fa",
	"POST /api/user/login/verify",
	"POST /api/user/login/passkey/begin",
	"POST /api/user/login/passkey/finish",
	"POST /api/user/passkey/login/begin",
	"POST /api/user/passkey/login/finish",
	"GET /api/oauth/wechat",
	"GET /api/oauth/telegram/login",
	"GET /api/oauth/telegram/bind/:flow_token",

	// Browser session cookie endpoints.
	"POST /api/user/auth/refresh",
	"POST /api/user/auth/logout",

	// Payment callbacks.
	"POST /api/stripe/webhook",
	"POST /api/creem/webhook",
	"POST /api/waffo/webhook",
	"POST /api/waffo-pancake/webhook/:env",
	"GET /api/user/epay/notify",
	"POST /api/user/epay/notify",
	"GET /api/subscription/epay/notify",
	"POST /api/subscription/epay/notify",
	"GET /api/subscription/epay/return",
	"POST /api/subscription/epay/return",

	// API key authentication (TokenAuthReadOnly).
	"GET /api/usage/token/",
	"GET /api/log/token",
}

// accessTokenHelperRoutes are declared through handlePermissionRoute outside
// channelPermissionRoutes.
var accessTokenHelperRoutes = map[string]authz.Permission{
	"GET /api/task_plugin_options": authz.TaskPluginBind,
	"GET /api/audit":               authz.AuditRead,
}

func newAccessTokenScopeTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// A permission route that is also in the static table panics here.
	require.NotPanics(t, func() {
		SetApiRouter(engine)
		SetDashboardRouter(engine)
		SetRelayRouter(engine)
		SetTaskPluginProtocolRouter(engine)
		SetVideoRouter(engine)
		SetTaskRouter(engine)
	})
	return engine
}

func TestAccessTokenRouteRulesCoverEveryDashboardRoute(t *testing.T) {
	engine := newAccessTokenScopeTestEngine(t)
	exempt := map[string]bool{}
	for _, key := range accessTokenExemptRoutes {
		exempt[key] = true
	}
	registered := map[string]bool{}
	panel := map[string]bool{}
	for _, route := range engine.Routes() {
		if !strings.HasPrefix(route.Path, "/api/") && !strings.HasPrefix(route.Path, "/pg/") {
			continue
		}
		key := route.Method + " " + route.Path
		registered[key] = true
		if !exempt[key] {
			panel[key] = true
		}
	}
	for _, key := range accessTokenExemptRoutes {
		assert.True(t, registered[key], "exempt route %s is no longer registered", key)
	}

	declared := map[string]int{}
	for _, key := range middleware.AccessTokenRouteRuleKeys() {
		declared[key]++
	}
	for key := range panel {
		assert.Equal(t, 1, declared[key], "panel route %s must be declared exactly once", key)
	}
	for key := range declared {
		assert.True(t, panel[key], "declared route %s is not a registered panel route", key)
	}

	permissionRoutes := map[string]authz.Permission{}
	for _, route := range channelPermissionRoutes {
		permissionRoutes[route.method+" "+joinPaths("/api/channel", route.path)] = route.permission
	}
	for key, permission := range accessTokenHelperRoutes {
		permissionRoutes[key] = permission
	}
	for key, permission := range permissionRoutes {
		rule, ok := middleware.AccessTokenRouteRule(key)
		require.True(t, ok, key)
		assert.Equal(t, "scope", rule.Kind(), key)
		assert.Equal(t, service.AccessTokenScopeOf(permission), rule.Scope(), key)
	}
}

func TestAccessTokenCatalogCoversEveryPermission(t *testing.T) {
	catalog := service.AccessTokenCatalog(1, common.RoleRootUser)
	scopes := map[string]bool{}
	resources := map[string]int{}
	for _, group := range catalog {
		for _, resource := range group.Resources {
			resources[resource.Resource]++
			for _, action := range resource.Actions {
				scopes[resource.Resource+":"+action.Action] = true
			}
		}
	}
	for _, permission := range authz.AllPermissions() {
		assert.True(t, scopes[service.AccessTokenScopeOf(permission)], "root catalog lacks %s", service.AccessTokenScopeOf(permission))
	}
	for resource, count := range resources {
		assert.Equal(t, 1, count, "resource %s appears in more than one catalog entry", resource)
	}
}

func TestAccessTokenVerificationScopesAreAllListed(t *testing.T) {
	files, err := filepath.Glob("../service/*.go")
	require.NoError(t, err)
	fileSet := token.NewFileSet()
	var scopes []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fileSet, file, nil, 0)
		require.NoError(t, err)
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, spec := range general.Specs {
				value := spec.(*ast.ValueSpec)
				for i, name := range value.Names {
					if !strings.HasPrefix(name.Name, "VerificationScope") || i >= len(value.Values) {
						continue
					}
					literal, ok := value.Values[i].(*ast.BasicLit)
					require.True(t, ok, name.Name)
					scope, err := strconv.Unquote(literal.Value)
					require.NoError(t, err)
					scopes = append(scopes, scope)
				}
			}
		}
	}
	require.NotEmpty(t, scopes)
	for _, scope := range scopes {
		_, listed := service.AccessTokenVerificationScope(scope)
		assert.True(t, listed, "verification scope %s has no access token rule", scope)
	}
}
