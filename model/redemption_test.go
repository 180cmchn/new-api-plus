package model

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestSearchRedemptionsFiltersAndPaginates(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Redemption{}, &RedemptionRecord{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	})

	now := common.GetTimestamp()
	redemptions := []Redemption{
		{Id: 1, Name: "alpha-active", Key: "00000000000000000000000000000001", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: 0},
		{Id: 2, Name: "alpha-future", Key: "00000000000000000000000000000002", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: now + 3600},
		{Id: 3, Name: "alpha-expired", Key: "00000000000000000000000000000003", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: now - 10},
		{Id: 4, Name: "beta-disabled", Key: "00000000000000000000000000000004", Status: common.RedemptionCodeStatusDisabled, ExpiredTime: 0},
		{Id: 5, Name: "beta-used", Key: "00000000000000000000000000000005", Status: common.RedemptionCodeStatusUsed, ExpiredTime: 0},
	}
	require.NoError(t, DB.Create(&redemptions).Error)

	tests := []struct {
		name      string
		keyword   string
		status    string
		startIdx  int
		num       int
		wantTotal int64
		wantIds   []int
	}{
		{
			name:      "no filters returns all rows",
			num:       10,
			wantTotal: 5,
			wantIds:   []int{5, 4, 3, 2, 1},
		},
		{
			name:      "keyword filters by name prefix",
			keyword:   "alpha",
			num:       10,
			wantTotal: 3,
			wantIds:   []int{3, 2, 1},
		},
		{
			name:      "enabled status excludes expired rows",
			status:    "1",
			num:       10,
			wantTotal: 2,
			wantIds:   []int{2, 1},
		},
		{
			name:      "expired status returns enabled expired rows",
			status:    "expired",
			num:       10,
			wantTotal: 1,
			wantIds:   []int{3},
		},
		{
			name:      "disabled status",
			status:    "2",
			num:       10,
			wantTotal: 1,
			wantIds:   []int{4},
		},
		{
			name:      "used status",
			status:    "3",
			num:       10,
			wantTotal: 1,
			wantIds:   []int{5},
		},
		{
			name:      "pagination keeps unpaged total",
			startIdx:  1,
			num:       2,
			wantTotal: 5,
			wantIds:   []int{4, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, total, err := SearchRedemptions(tt.keyword, tt.status, tt.startIdx, tt.num)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTotal, total)
			gotIds := make([]int, 0, len(rows))
			for _, row := range rows {
				gotIds = append(gotIds, row.Id)
			}
			assert.Equal(t, tt.wantIds, gotIds)
		})
	}
}

func setupRedeemFixture(t *testing.T, quota int) (userId int, key string) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&Redemption{}, &RedemptionRecord{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM logs")
	})

	user := &User{Username: "redeem-user", Password: "password", Status: common.UserStatusEnabled, Quota: 0}
	require.NoError(t, DB.Create(user).Error)

	key = "10000000000000000000000000000001"
	redemption := &Redemption{
		Name:        "redeem-test",
		Key:         key,
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       quota,
		CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(redemption).Error)
	return user.Id, key
}

func TestRedeemCreditsQuotaExactlyOnce(t *testing.T) {
	userId, key := setupRedeemFixture(t, 500)

	quota, err := Redeem(key, userId)
	require.NoError(t, err)
	assert.Equal(t, 500, quota)

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 500, user.Quota)

	var redemption Redemption
	require.NoError(t, DB.First(&redemption, "name = ?", "redeem-test").Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, redemption.Status)
	assert.Equal(t, userId, redemption.UsedUserId)

	// Redeeming the same code again must fail and must not credit quota.
	_, err = Redeem(key, userId)
	require.Error(t, err)
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 500, user.Quota)
}

func TestRedeemRejectsWalletOverflow(t *testing.T) {
	userId, key := setupRedeemFixture(t, 11)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userId).Update("quota", common.MaxWalletQuota-10).Error)

	_, err := Redeem(key, userId)
	require.ErrorIs(t, err, ErrRedeemFailed)

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, common.MaxWalletQuota-10, user.Quota)

	var redemption Redemption
	require.NoError(t, DB.First(&redemption, "key = ?", key).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, redemption.Status)
}

func TestRedemptionQuotaRejectsWalletOverflow(t *testing.T) {
	setupRedeemFixture(t, 500)

	redemption := &Redemption{
		Name:        "overflow-redemption",
		Key:         "10000000000000000000000000000002",
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       common.MaxWalletQuota + 1,
		CreatedTime: common.GetTimestamp(),
	}
	require.Error(t, redemption.Insert())
}

// Exactly one of several concurrent redeems of the same code may win, and
// quota must be credited exactly once.
func TestRedeemConcurrentSingleSuccess(t *testing.T) {
	userId, key := setupRedeemFixture(t, 300)

	const goroutines = 5
	successes := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			if _, err := Redeem(key, userId); err == nil {
				successes[idx] = true
			}
		}(i)
	}
	wg.Wait()

	successCount := 0
	for _, ok := range successes {
		if ok {
			successCount++
		}
	}
	assert.Equal(t, 1, successCount, "exactly one concurrent redeem should succeed")

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 300, user.Quota, "quota must be credited exactly once")
}

// This fixture is the rc45 redemption schema, before type and settlement history.
type legacyReferralRedemption struct {
	Id           int
	UserId       int
	Key          string `gorm:"type:char(32);uniqueIndex"`
	Status       int    `gorm:"default:1"`
	Name         string `gorm:"index"`
	Quota        int    `gorm:"default:100"`
	CreatedTime  int64  `gorm:"bigint"`
	RedeemedTime int64  `gorm:"bigint"`
	UsedUserId   int
	DeletedAt    gorm.DeletedAt `gorm:"index"`
	ExpiredTime  int64          `gorm:"bigint"`
}

func (legacyReferralRedemption) TableName() string { return "redemptions" }

type legacyWalletUser struct {
	User
	Quota int `gorm:"type:int"`
}

func (legacyWalletUser) TableName() string { return "users" }

type legacyDailyCommissionSettlement struct {
	Id              int
	Day             int64 `gorm:"uniqueIndex:idx_commission_relation_day;not null"`
	InviterID       int   `gorm:"uniqueIndex:idx_commission_relation_day;index;not null"`
	UserID          int   `gorm:"uniqueIndex:idx_commission_relation_day;index;not null"`
	ConsumedQuota   int   `gorm:"type:bigint;not null;default:0"`
	RateBasisPoints int64
	RewardQuota     int `gorm:"type:bigint;not null;default:0"`
	SettledAt       int64
}

func (legacyDailyCommissionSettlement) TableName() string { return "commission_settlements" }

func runManualCommissionCases(t *testing.T, db *gorm.DB) {
	t.Helper()
	day := BeijingDay(time.Date(2028, 1, 2, 0, 0, 0, 0, time.UTC).Unix())
	now := day + 3600
	inviter := User{Username: "manual-inviter", AffCode: "manual-A", Status: common.UserStatusEnabled}
	nextInviter := User{Username: "manual-next-inviter", AffCode: "manual-C", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&inviter).Error)
	require.NoError(t, db.Create(&nextInviter).Error)
	buyer := User{Username: "manual-buyer", AffCode: "manual-B", InviterId: nextInviter.Id, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&buyer).Error)
	charges := []WalletCharge{
		{RequestID: "manual-midnight", UserID: buyer.Id, InviterID: inviter.Id, OrdinaryQuota: 100, GiftQuota: 50, CommissionQuota: 50, ConsumptionDay: day, ConsumedAt: day},
		{RequestID: "manual-cutoff", UserID: buyer.Id, InviterID: nextInviter.Id, OrdinaryQuota: 200, ConsumptionDay: day, ConsumedAt: now},
		{RequestID: "manual-yesterday", UserID: buyer.Id, InviterID: inviter.Id, OrdinaryQuota: 300, ConsumptionDay: day - 86400, ConsumedAt: day - 1},
		{RequestID: "manual-after-cutoff", UserID: buyer.Id, InviterID: inviter.Id, OrdinaryQuota: 400, ConsumptionDay: day, ConsumedAt: now + 1},
		{RequestID: "manual-tomorrow", UserID: buyer.Id, InviterID: inviter.Id, OrdinaryQuota: 500, ConsumptionDay: day + 86400, ConsumedAt: day + 86400},
		{RequestID: "manual-gift", UserID: buyer.Id, InviterID: inviter.Id, GiftQuota: 100, ConsumptionDay: day, ConsumedAt: now},
		{RequestID: "manual-commission", UserID: buyer.Id, InviterID: inviter.Id, CommissionQuota: 100, ConsumptionDay: day, ConsumedAt: now},
		{RequestID: "manual-unbound", UserID: buyer.Id, OrdinaryQuota: 100, ConsumptionDay: day, ConsumedAt: now},
		{RequestID: "manual-reserved", UserID: buyer.Id, InviterID: inviter.Id, OrdinaryQuota: 100},
		{RequestID: "manual-refunded", UserID: buyer.Id, InviterID: inviter.Id, OrdinaryQuota: 100, ConsumptionDay: day, ConsumedAt: now, RefundedAt: now},
	}
	require.NoError(t, db.Create(&charges).Error)
	common.ReferralEnabled.Store(true)
	common.ReferralRateBasisPoints.Store(1000)
	result, err := SettleTodayCommissions(now)
	require.NoError(t, err)
	assert.Equal(t, &CommissionSettlementResult{Day: day, Cutoff: now, RateBasisPoints: 1000, SettledCharges: 2}, result)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	require.NoError(t, db.First(&nextInviter, nextInviter.Id).Error)
	assert.Equal(t, 10, inviter.CommissionQuota, "use the inviter captured at consumption, not the current binding")
	assert.Equal(t, 20, nextInviter.CommissionQuota)
	assert.Zero(t, inviter.Quota)
	var excluded []WalletCharge
	require.NoError(t, db.Where("id >= ? AND id <= ?", charges[2].Id, charges[9].Id).Find(&excluded).Error)
	for _, charge := range excluded {
		assert.Zero(t, charge.CommissionSettlementID, charge.RequestID)
	}

	common.ReferralRateBasisPoints.Store(2000)
	result, err = SettleTodayCommissions(now)
	require.NoError(t, err)
	assert.Zero(t, result.SettledCharges, "a changed rate must not recalculate paid consumption")
	result, err = SettleTodayCommissions(now + 60)
	require.NoError(t, err)
	assert.Equal(t, 1, result.SettledCharges)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 90, inviter.CommissionQuota)
	var records []CommissionSettlement
	require.NoError(t, db.Where("day = ? AND inviter_id = ?", day, inviter.Id).Order("rate_basis_points").Find(&records).Error)
	require.Len(t, records, 2)
	assert.Equal(t, []int64{1000, 2000}, []int64{records[0].RateBasisPoints, records[1].RateBasisPoints})
	assert.Equal(t, []int{10, 80}, []int{records[0].RewardQuota, records[1].RewardQuota})
	var snapshot CommissionDay
	require.NoError(t, db.Where("day = ?", day).First(&snapshot).Error)
	assert.Zero(t, snapshot.CreatedAt, "manual settlement must not freeze the midnight rate")

	concurrent := WalletCharge{RequestID: "manual-concurrent", UserID: buyer.Id, InviterID: inviter.Id, OrdinaryQuota: 50, ConsumptionDay: day, ConsumedAt: now + 120}
	require.NoError(t, db.Create(&concurrent).Error)
	common.ReferralRateBasisPoints.Store(2500)
	start := make(chan struct{})
	errors := make(chan error, 2)
	counts := make(chan int, 2)
	for range 2 {
		go func() {
			<-start
			settled, err := SettleTodayCommissions(now + 180)
			counts <- settled.SettledCharges
			errors <- err
		}()
	}
	close(start)
	for range 2 {
		require.NoError(t, <-errors)
	}
	assert.Equal(t, 1, <-counts+<-counts)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 102, inviter.CommissionQuota)

	late := WalletCharge{RequestID: "manual-later-auto", UserID: buyer.Id, InviterID: inviter.Id, OrdinaryQuota: 60, ConsumptionDay: day, ConsumedAt: day + 86399}
	require.NoError(t, db.Create(&late).Error)
	common.ReferralRateBasisPoints.Store(3000)
	_, err = SettleDailyCommissions(day+86400, 100)
	require.NoError(t, err)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 210, inviter.CommissionQuota, "midnight pays yesterday's pending 300 and today's remaining 60 at 30%, without paying manual charges again")
	late.Id, late.RequestID, late.OrdinaryQuota = 0, "manual-delayed-auto", 10
	require.NoError(t, db.Create(&late).Error)
	common.ReferralRateBasisPoints.Store(5000)
	_, err = SettleDailyCommissions(day+86400, 100)
	require.NoError(t, err)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 213, inviter.CommissionQuota, "automatic retries retain their original daily rate")

	// A manual payout and the midnight worker may overlap; both share the day lock.
	start = make(chan struct{})
	go func() { <-start; _, err := SettleTodayCommissions(day + 86400 + 60); errors <- err }()
	go func() { <-start; _, err := SettleDailyCommissions(day+2*86400, 100); errors <- err }()
	close(start)
	for range 2 {
		require.NoError(t, <-errors)
	}
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 463, inviter.CommissionQuota, "the next day's 500 is paid exactly once")

	overflow := WalletCharge{RequestID: "manual-overflow", UserID: buyer.Id, InviterID: nextInviter.Id, OrdinaryQuota: 100, ConsumptionDay: day, ConsumedAt: now}
	require.NoError(t, db.Create(&overflow).Error)
	common.ReferralEnabled.Store(false)
	_, err = SettleTodayCommissions(now)
	require.ErrorContains(t, err, "Enable referral rewards")
	common.ReferralEnabled.Store(true)
	common.ReferralRateBasisPoints.Store(0)
	_, err = SettleTodayCommissions(now)
	require.ErrorContains(t, err, "positive rate")
	common.ReferralRateBasisPoints.Store(1000)
	require.NoError(t, db.Model(&nextInviter).Update("commission_quota", common.MaxWalletQuota).Error)
	_, err = SettleTodayCommissions(now)
	require.ErrorIs(t, err, ErrWalletQuotaLimitExceeded)
	require.NoError(t, db.First(&overflow, overflow.Id).Error)
	assert.Zero(t, overflow.CommissionSettlementID)
	require.NoError(t, db.Model(&nextInviter).Update("commission_quota", 20).Error)
	result, err = SettleTodayCommissions(now)
	require.NoError(t, err)
	assert.Equal(t, 1, result.SettledCharges)
	require.NoError(t, db.First(&nextInviter, nextInviter.Id).Error)
	assert.Equal(t, 30, nextInviter.CommissionQuota)
}

func TestReferralRedemptionDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			kind := common.DatabaseTypeSQLite
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(filepath.Join(t.TempDir(), "wallet.db") + "?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN not configured")
				}
				driver, kind = mysql.Open(dsn), common.DatabaseTypeMySQL
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN not configured")
				}
				driver, kind = postgres.Open(dsn), common.DatabaseTypePostgreSQL
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(4)
			previousDB, previousLog := DB, LOG_DB
			previousMainKind, previousLogKind := common.MainDatabaseType(), common.LogDatabaseType()
			DB, LOG_DB = db, db
			common.SetDatabaseTypes(kind, kind)
			oldNew, oldInviter, oldInvitee := common.QuotaForNewUser, common.QuotaForInviter, common.QuotaForInvitee
			common.QuotaForNewUser, common.QuotaForInviter, common.QuotaForInvitee = 0, 123, 456
			t.Cleanup(func() {
				common.QuotaForNewUser, common.QuotaForInviter, common.QuotaForInvitee = oldNew, oldInviter, oldInvitee
			})
			oldEnabled, oldRate := common.ReferralEnabled.Load(), common.ReferralRateBasisPoints.Load()
			payment := operation_setting.GetPaymentSetting()
			oldPayment := *payment
			payment.ComplianceConfirmed, payment.ComplianceTermsVersion = true, operation_setting.CurrentComplianceTermsVersion
			common.ReferralEnabled.Store(true)
			common.ReferralRateBasisPoints.Store(1000)
			t.Cleanup(func() {
				DB, LOG_DB = previousDB, previousLog
				common.SetDatabaseTypes(previousMainKind, previousLogKind)
				common.ReferralEnabled.Store(oldEnabled)
				common.ReferralRateBasisPoints.Store(oldRate)
				*payment = oldPayment
				require.NoError(t, sqlDB.Close())
			})
			tables := []any{&WalletCharge{}, &CommissionDay{}, &CommissionSettlement{}, &CommissionTransfer{}, &ReferralBindingChange{}, &RedemptionRecord{}, &Redemption{}, &Task{}, &Midjourney{}, &User{}, &Log{}}
			for _, table := range tables {
				require.False(t, db.Migrator().HasTable(table), "use a dedicated empty test database")
			}
			t.Cleanup(func() {
				for _, table := range tables {
					require.NoError(t, db.Migrator().DropTable(table))
				}
			})
			var version string
			versionQuery := "SELECT version()"
			if dialect == "sqlite" {
				versionQuery = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("%s", version)
			// Fresh installation and repeat startup preserve the code and its unique key.
			for range 2 {
				require.NoError(t, db.AutoMigrate(tables...))
				require.NoError(t, migrateCommissionSettlementRateIndex(db))
			}
			fresh := Redemption{Key: "fresh", Name: "fresh", Type: RedemptionTypeRecharge, Quota: 123, Status: 1}
			require.NoError(t, fresh.Insert())
			require.NoError(t, db.AutoMigrate(tables...))
			duplicate := Redemption{Key: "fresh", Name: "duplicate", Quota: 123, Status: 1}
			require.Error(t, duplicate.Insert())
			require.NoError(t, db.First(&fresh, fresh.Id).Error)
			assert.Equal(t, RedemptionTypeRecharge, fresh.Type)
			assert.Equal(t, 123, fresh.Quota)
			// Upgrade the deployed daily-only commission index without changing
			// historical payouts or the charge markers that prevent duplicate credit.
			require.NoError(t, db.Migrator().DropIndex(&CommissionSettlement{}, "idx_commission_relation_rate"))
			require.NoError(t, db.Migrator().CreateIndex(&legacyDailyCommissionSettlement{}, "idx_commission_relation_day"))
			oldSettlement := CommissionSettlement{Day: 100, InviterID: 1, UserID: 2, ConsumedQuota: 100, RateBasisPoints: 1000, RewardQuota: 10, SettledAt: 200}
			require.NoError(t, db.Create(&oldSettlement).Error)
			oldCharge := WalletCharge{RequestID: "settled-before-index-upgrade", UserID: 2, CommissionSettlementID: oldSettlement.Id}
			require.NoError(t, db.Create(&oldCharge).Error)
			for range 2 {
				require.NoError(t, db.AutoMigrate(tables...))
				require.NoError(t, migrateCommissionSettlementRateIndex(db))
			}
			var preserved CommissionSettlement
			require.NoError(t, db.First(&preserved, oldSettlement.Id).Error)
			assert.Equal(t, oldSettlement, preserved)
			require.NoError(t, db.First(&oldCharge, oldCharge.Id).Error)
			assert.Equal(t, oldSettlement.Id, oldCharge.CommissionSettlementID)
			sameRate := oldSettlement
			sameRate.Id = 0
			require.Error(t, db.Create(&sameRate).Error)
			otherRate := oldSettlement
			otherRate.Id, otherRate.RateBasisPoints = 0, 2000
			require.NoError(t, db.Create(&otherRate).Error)
			for _, table := range tables {
				require.NoError(t, db.Migrator().DropTable(table))
			}
			// Simulate a separate deployment so driver statement caches cannot
			// reuse the fresh installation's result types after dropping tables.
			require.NoError(t, sqlDB.Close())
			db, err = gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err = db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(4)
			DB, LOG_DB = db, db
			require.NoError(t, db.AutoMigrate(&legacyWalletUser{}, &Log{}, &legacyReferralRedemption{}, &Task{}, &Midjourney{}))
			legacy := legacyReferralRedemption{Key: "legacy-referral-code", Name: "legacy", Quota: 50, Status: 1}
			require.NoError(t, db.Create(&legacy).Error)
			historical := legacyReferralRedemption{Key: "already-used", Name: "historical", Quota: 777, Status: 3, UsedUserId: 999, RedeemedTime: 100}
			require.NoError(t, db.Create(&historical).Error)
			oldUser := User{Username: "pre-upgrade", AffCode: "old-user", Quota: 12345, UsedQuota: 678, Setting: `{"billing_preference":"wallet_only"}`}
			require.NoError(t, db.Create(&oldUser).Error)
			oldTask := Task{TaskID: "pre-upgrade-task", UserId: oldUser.Id, Quota: 70, Status: TaskStatusSuccess}
			require.NoError(t, db.Create(&oldTask).Error)
			oldMJ := Midjourney{UserId: oldUser.Id, MjId: "pre-upgrade-mj", Quota: 80, Status: "SUCCESS"}
			require.NoError(t, db.Create(&oldMJ).Error)
			require.NoError(t, db.Migrator().DropColumn(&Midjourney{}, "WalletChargeID"))
			require.NoError(t, db.Migrator().DropColumn(&User{}, "CommissionQuota"))
			require.NoError(t, db.Migrator().DropColumn(&User{}, "GiftQuota"))
			for range 2 {
				require.NoError(t, db.AutoMigrate(tables...))
				require.NoError(t, migrateCommissionSettlementRateIndex(db))
			}
			require.NoError(t, db.First(&oldUser, oldUser.Id).Error)
			assert.Equal(t, 12345, oldUser.Quota)
			assert.Equal(t, 678, oldUser.UsedQuota)
			assert.Zero(t, oldUser.CommissionQuota)
			assert.Zero(t, oldUser.GiftQuota)
			require.NoError(t, db.First(&oldTask, oldTask.ID).Error)
			assert.Equal(t, 70, oldTask.Quota)
			require.NoError(t, db.First(&oldMJ, oldMJ.Id).Error)
			assert.Equal(t, 80, oldMJ.Quota)
			assert.Empty(t, oldMJ.WalletChargeID)
			var historicalCharges int64
			require.NoError(t, db.Model(&WalletCharge{}).Count(&historicalCharges).Error)
			assert.Zero(t, historicalCharges, "pre-upgrade usage is not imported")
			var migrated Redemption
			require.NoError(t, db.First(&migrated, legacy.Id).Error)
			assert.Equal(t, "", migrated.Type)
			assert.Equal(t, 50, migrated.Quota)
			var recordCount int64
			require.NoError(t, db.Model(&RedemptionRecord{}).Count(&recordCount).Error)
			assert.Zero(t, recordCount)

			inviter := User{Username: "inviter", AffCode: "A-ref", Quota: 0, Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&inviter).Error)
			users := make([]User, 3)
			for i := range users {
				users[i] = User{Username: fmt.Sprintf("invitee-%d", i), Status: common.UserStatusEnabled}
				require.NoError(t, users[i].Insert(inviter.Id))
				assert.Equal(t, inviter.Id, users[i].InviterId)
				assert.Zero(t, users[i].Quota, "no fixed invitee award")
			}
			summary, err := GetReferralSummary(inviter.Id)
			require.NoError(t, err)
			assert.EqualValues(t, 3, summary.InviteCount)
			assert.Zero(t, summary.RewardQuota)
			require.NoError(t, db.First(&inviter, inviter.Id).Error)
			assert.Zero(t, inviter.AffQuota, "no fixed inviter award")
			assert.Equal(t, 3, inviter.AffCount)
			codes := []Redemption{
				{Name: "B", Key: "recharge-B", Type: RedemptionTypeRecharge, Quota: 1000, Status: 1},
				{Name: "C", Key: "recharge-C", Type: RedemptionTypeRecharge, Quota: 2000, Status: 1},
				{Name: "D", Key: "compensation-D", Type: RedemptionTypeCompensation, Quota: 500, Status: 1},
			}
			for i := range codes {
				require.NoError(t, codes[i].Insert())
				_, err := Redeem(codes[i].Key, users[i].Id)
				require.NoError(t, err)
			}
			summary, err = GetReferralSummary(inviter.Id)
			require.NoError(t, err)
			assert.Zero(t, summary.RewardQuota, "redemption no longer pays a referral reward")
			require.NoError(t, db.First(&users[2], users[2].Id).Error)
			assert.Zero(t, users[2].Quota)
			assert.Equal(t, 500, users[2].GiftQuota)
			_, err = Redeem(codes[0].Key, users[0].Id)
			require.Error(t, err)
			codes[0].Type = RedemptionTypeCompensation
			require.Error(t, codes[0].Update())
			_, err = Redeem(legacy.Key, users[0].Id)
			require.NoError(t, err)
			require.NoError(t, RevokeRedemption(codes[2].Id, inviter.Id))
			require.Error(t, RevokeRedemption(codes[2].Id, inviter.Id))
			require.NoError(t, db.First(&users[2], users[2].Id).Error)
			assert.Zero(t, users[2].GiftQuota)
			assert.Zero(t, users[2].Quota)
			runWalletAccountingCases(t, db, inviter, users[0], users[1])
			runManualCommissionCases(t, db)

		})
	}
}

func TestReferralRateValidation(t *testing.T) {
	for _, input := range []string{"", "NaN", "Inf", "-1", "100.01", "1.001", "1e999999999", "invalid"} {
		assert.Error(t, validateOptionValue("ReferralRewardPercent", input), input)
	}
	for input, expected := range map[string]int64{"0": 0, "100": 10000, "12.34": 1234, "0.01": 1} {
		actual, err := common.ParseReferralPercent(input)
		require.NoError(t, err)
		assert.Equal(t, expected, actual)
	}
	assert.Error(t, validateOptionValue("ReferralEnabled", "invalid"))
}

func TestReferralRedemptionSynchronizesCachedBalances(t *testing.T) {
	userId, key := setupRedeemFixture(t, 500)
	useUserCacheMiniRedis(t)
	oldEnabled, oldRate := common.ReferralEnabled.Load(), common.ReferralRateBasisPoints.Load()
	payment := operation_setting.GetPaymentSetting()
	oldPayment := *payment
	payment.ComplianceConfirmed, payment.ComplianceTermsVersion = true, operation_setting.CurrentComplianceTermsVersion
	common.ReferralEnabled.Store(true)
	common.ReferralRateBasisPoints.Store(1000)
	t.Cleanup(func() {
		common.ReferralEnabled.Store(oldEnabled)
		common.ReferralRateBasisPoints.Store(oldRate)
		*payment = oldPayment
	})
	inviter := User{Username: "cache-inviter", AuthVersion: 1, Status: common.UserStatusEnabled, AffCode: "cache-inviter"}
	require.NoError(t, DB.Create(&inviter).Error)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userId).Update("inviter_id", inviter.Id).Error)
	require.NoError(t, DB.Model(&Redemption{}).Where("key = ?", key).Update("type", RedemptionTypeRecharge).Error)
	var user User
	require.NoError(t, DB.First(&user, userId).Error)
	require.NoError(t, populateUserCache(user))
	require.NoError(t, populateUserCache(inviter))
	_, err := Redeem(key, userId)
	require.NoError(t, err)
	cached, err := cacheGetUserBase(inviter.Id)
	require.NoError(t, err)
	assert.Zero(t, cached.Quota)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", inviter.Id).Update("quota", 0).Error)
	var code Redemption
	require.NoError(t, DB.Where("key = ?", key).First(&code).Error)
	require.NoError(t, RevokeRedemption(code.Id, inviter.Id))
	require.Error(t, RevokeRedemption(code.Id, inviter.Id))
	for id, expected := range map[int]int{inviter.Id: 0, userId: 0} {
		cached, err = cacheGetUserBase(id)
		require.NoError(t, err)
		assert.Equal(t, expected, cached.Quota)
		var persisted User
		require.NoError(t, DB.First(&persisted, id).Error)
		assert.Equal(t, expected, persisted.Quota)
	}
}

func runWalletAccountingCases(t *testing.T, db *gorm.DB, inviter, buyer, nextInviter User) {
	t.Helper()
	require.NoError(t, db.Model(&User{}).Where("id = ?", buyer.Id).Updates(map[string]any{"quota": 1000, "commission_quota": 200, "gift_quota": 300}).Error)
	require.NoError(t, UpdateWalletOrder(buyer.Id, []string{WalletGift, WalletOrdinary, WalletCommission}))
	reservation, err := SetWalletCharge(buyer.Id, "mixed-request", 500, false, true)
	require.NoError(t, err)
	assert.Equal(t, 200, reservation.OrdinaryQuota)
	assert.Equal(t, 300, reservation.GiftQuota)
	assert.Zero(t, reservation.ConsumedAt)
	nextDay := BeijingDay(time.Now().Unix()) + 86400
	count, err := SettleDailyCommissions(nextDay, 100)
	require.NoError(t, err)
	assert.Zero(t, count, "reservations are not consumption")
	settled, err := SetWalletCharge(buyer.Id, "mixed-request", 400, true, false)
	require.NoError(t, err)
	assert.Equal(t, 100, settled.OrdinaryQuota)
	assert.Equal(t, 300, settled.GiftQuota)
	_, err = SetWalletCharge(buyer.Id, "mixed-request", 400, true, false)
	require.NoError(t, err)
	require.Error(t, RefundWalletCharge(buyer.Id, "mixed-request"))
	_, err = SetWalletCharge(buyer.Id, "failed-request", 250, false, true)
	require.NoError(t, err)
	require.NoError(t, RefundWalletCharge(buyer.Id, "failed-request"))
	require.NoError(t, RefundWalletCharge(buyer.Id, "failed-request"))
	require.NoError(t, db.First(&buyer, buyer.Id).Error)
	assert.Equal(t, 900, buyer.Quota)
	assert.Equal(t, 200, buyer.CommissionQuota)
	assert.Zero(t, buyer.GiftQuota)

	// The old inviter keeps already consumed usage even before daily settlement.
	_, err = ChangeUserInviter(buyer.Id, nextInviter.Id, inviter.Id, common.RoleRootUser)
	require.NoError(t, err)
	oldSummary, err := GetReferralSummary(inviter.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 2, oldSummary.InviteCount)
	newSummary, err := GetReferralSummary(nextInviter.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 1, newSummary.InviteCount)
	profile, err := GetSelfUserById(inviter.Id)
	require.NoError(t, err)
	assert.Equal(t, 2, profile.AffCount, "profile follows the current binding")
	listed, _, err := GetAllUsers(&common.PageInfo{Page: 1, PageSize: 100})
	require.NoError(t, err)
	counts := make(map[int]int, len(listed))
	for _, user := range listed {
		counts[user.Id] = user.AffCount
	}
	assert.Equal(t, 2, counts[inviter.Id])
	assert.Equal(t, 1, counts[nextInviter.Id])
	filtered, _, err := SearchUsers(nextInviter.Username, "", nil, nil, 0, 100)
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, 1, filtered[0].AffCount)
	_, err = ChangeUserInviter(buyer.Id, buyer.Id, inviter.Id, common.RoleRootUser)
	require.Error(t, err)
	_, err = ChangeUserInviter(buyer.Id, inviter.Id, buyer.Id, common.RoleCommonUser)
	require.Error(t, err)
	require.NoError(t, UpdateWalletOrder(buyer.Id, []string{WalletOrdinary}))
	_, err = SetWalletCharge(buyer.Id, "after-rebinding", 200, true, true)
	require.NoError(t, err)
	common.ReferralRateBasisPoints.Store(2000)
	count, err = SettleDailyCommissions(nextDay-1, 100)
	require.NoError(t, err)
	assert.Zero(t, count, "no settlement before Beijing midnight")
	count, err = SettleDailyCommissions(nextDay, 100)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	count, err = SettleDailyCommissions(nextDay, 100)
	require.NoError(t, err)
	assert.Zero(t, count)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	require.NoError(t, db.First(&nextInviter, nextInviter.Id).Error)
	assert.Equal(t, 20, inviter.CommissionQuota)
	assert.Equal(t, 40, nextInviter.CommissionQuota)
	assert.Zero(t, inviter.Quota, "commission is not credited to ordinary balance")
	records, _, err := GetReferralRecords(buyer.Id, inviter.Id, 0, 10)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.EqualValues(t, 2000, records[0].RateBasisPoints)
	assert.Equal(t, 100, records[0].ConsumedQuota)
	details, _, err := GetReferralDetails("invitee-0", 0, 10)
	require.NoError(t, err)
	require.Len(t, details, 2, "historical and current inviter both remain visible")

	// Cash refunds are manual balance changes, not reversals of consumption.
	_, err = AdjustUserQuota(buyer.Id, common.RoleRootUser, "override", 0)
	require.NoError(t, err)
	oldSummary, err = GetReferralSummary(inviter.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 20, oldSummary.RewardQuota)
	transfer, err := TransferCommission(nextInviter.Id, 40, 1000, "transfer-request-0001")
	require.NoError(t, err)
	assert.Equal(t, 4, transfer.FeeQuota)
	assert.Equal(t, 36, transfer.ReceivedQuota)
	_, err = TransferCommission(nextInviter.Id, 40, 1000, "transfer-request-0001")
	require.NoError(t, err, "a replay returns the original result")
	_, err = TransferCommission(nextInviter.Id, 20, 1000, "transfer-request-0001")
	require.Error(t, err)
	_, err = TransferCommission(buyer.Id, 10, 500, "stale-fee-request")
	require.Error(t, err)
	require.NoError(t, db.First(&nextInviter, nextInviter.Id).Error)
	assert.Zero(t, nextInviter.CommissionQuota)
	assert.Equal(t, 2036, nextInviter.Quota)
	require.NoError(t, UpdateWalletOrder(nextInviter.Id, []string{WalletOrdinary}))
	earned, err := SetWalletCharge(nextInviter.Id, "transferred-commission-spend", 36, true, true)
	require.NoError(t, err)
	assert.Equal(t, 36, earned.OrdinaryQuota)
	assert.Equal(t, inviter.Id, earned.InviterID)
	common.ReferralRateBasisPoints.Store(5000)
	count, err = SettleDailyCommissions(nextDay, 100)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 27, inviter.CommissionQuota, "one day uses its persisted settlement rate, flooring fractional quota")

	// Disabled wallets never fund requests, even if their balances are positive.
	require.NoError(t, UpdateWalletOrder(buyer.Id, []string{}))
	_, err = SetWalletCharge(buyer.Id, "all-disabled", 1, false, true)
	require.ErrorIs(t, err, ErrInsufficientWalletQuota)
	require.Error(t, UpdateWalletOrder(buyer.Id, []string{WalletGift, WalletGift}))
	require.NoError(t, UpdateWalletOrder(buyer.Id, []string{WalletCommission}))
	_, err = SetWalletCharge(buyer.Id, "commission-consumption", 100, true, true)
	require.NoError(t, err)
	count, err = SettleDailyCommissions(nextDay, 100)
	require.NoError(t, err)
	assert.Zero(t, count)

	// Concurrent requests cannot overspend a wallet or duplicate a transfer.
	require.NoError(t, db.Model(&User{}).Where("id = ?", buyer.Id).Update("commission_quota", 100).Error)
	results := make(chan error, 2)
	start := make(chan struct{})
	for _, key := range []string{"concurrent-a", "concurrent-b"} {
		go func() { <-start; _, e := SetWalletCharge(buyer.Id, key, 100, false, true); results <- e }()
	}
	close(start)
	successful := 0
	for range 2 {
		if <-results == nil {
			successful++
		}
	}
	assert.Equal(t, 1, successful)
	require.NoError(t, db.First(&buyer, buyer.Id).Error)
	assert.Zero(t, buyer.CommissionQuota)

	// Legacy awards remain reversible from their original ordinary wallet.
	code := Redemption{Name: "legacy-award", Key: "legacy-award", Type: RedemptionTypeRecharge, Quota: 100, Status: common.RedemptionCodeStatusUsed, UsedUserId: buyer.Id}
	require.NoError(t, code.Insert())
	legacy := RedemptionRecord{RedemptionId: code.Id, UserId: buyer.Id, InviterId: inviter.Id, Quota: 100, RewardQuota: 10, RateBasisPoints: 1000, Type: RedemptionTypeRecharge}
	require.NoError(t, db.Create(&legacy).Error)
	require.NoError(t, RevokeRedemption(code.Id, inviter.Id))
	require.Error(t, RevokeRedemption(code.Id, inviter.Id))
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, -10, inviter.Quota)
	assert.Equal(t, 27, inviter.CommissionQuota)
	require.NoError(t, db.First(&buyer, buyer.Id).Error)
	assert.Equal(t, -100, buyer.Quota)

	// A binding created later must never capture earlier unbound consumption.
	unbound := User{Username: "unbound-wallet", Status: common.UserStatusEnabled, Quota: 100}
	require.NoError(t, db.Create(&unbound).Error)
	beforeBinding, err := SetWalletCharge(unbound.Id, "before-any-binding", 10, true, true)
	require.NoError(t, err)
	_, err = ChangeUserInviter(unbound.Id, inviter.Id, inviter.Id, common.RoleRootUser)
	require.NoError(t, err)
	require.NoError(t, db.First(&beforeBinding, beforeBinding.Id).Error)
	assert.Zero(t, beforeBinding.InviterID)

	// Terminal async state and its actual wallet allocation commit together.
	_, err = SetWalletCharge(unbound.Id, "async-wallet", 10, false, true)
	require.NoError(t, err)
	task := Task{TaskID: "async-wallet", UserId: unbound.Id, Quota: 10, Status: TaskStatusSubmitted,
		PrivateData: TaskPrivateData{WalletChargeID: "async-wallet"}}
	require.NoError(t, task.Insert())
	task.Status = TaskStatusSuccess
	task.WalletCompletion = &WalletTaskCompletion{Quota: 20}
	require.NoError(t, db.Model(&unbound).Update("quota", -common.MaxWalletQuota).Error)
	won, err := task.UpdateWithStatus(TaskStatusSubmitted)
	require.ErrorIs(t, err, ErrWalletQuotaLimitExceeded)
	assert.False(t, won)
	var persistedTask Task
	require.NoError(t, db.First(&persistedTask, task.ID).Error)
	assert.EqualValues(t, TaskStatusSubmitted, persistedTask.Status, "failed funding keeps the task retryable")
	require.NoError(t, db.Model(&unbound).Update("quota", 80).Error)
	won, err = task.UpdateWithStatus(TaskStatusSubmitted)
	require.NoError(t, err)
	assert.True(t, won)
	won, err = task.UpdateWithStatus(TaskStatusSubmitted)
	require.NoError(t, err)
	assert.False(t, won)
	require.NoError(t, db.First(&unbound, unbound.Id).Error)
	assert.Equal(t, 70, unbound.Quota)
	var asyncCharge WalletCharge
	require.NoError(t, db.Where("request_id = ?", "async-wallet").First(&asyncCharge).Error)
	assert.Equal(t, 20, asyncCharge.OrdinaryQuota)
	assert.Positive(t, asyncCharge.ConsumedAt)
	assert.Equal(t, inviter.Id, asyncCharge.InviterID)

	require.NoError(t, db.Model(&unbound).Update("gift_quota", 30).Error)
	require.NoError(t, UpdateWalletOrder(unbound.Id, []string{WalletGift, WalletOrdinary}))
	_, err = SetWalletCharge(unbound.Id, "failed-async-wallet", 50, false, true)
	require.NoError(t, err)
	failed := Task{TaskID: "failed-async-wallet", UserId: unbound.Id, Quota: 50, Status: TaskStatusSubmitted,
		PrivateData: TaskPrivateData{WalletChargeID: "failed-async-wallet"}}
	require.NoError(t, failed.Insert())
	failed.Status = TaskStatusFailure
	won, err = failed.UpdateWithStatus(TaskStatusSubmitted)
	require.NoError(t, err)
	assert.True(t, won)
	require.NoError(t, RefundWalletCharge(unbound.Id, "failed-async-wallet"))
	require.NoError(t, db.First(&unbound, unbound.Id).Error)
	assert.Equal(t, 70, unbound.Quota)
	assert.Equal(t, 30, unbound.GiftQuota, "failure restores the original sources once")

	// A transfer that cannot credit the ordinary wallet must not debit commission.
	require.NoError(t, db.Model(&unbound).Updates(map[string]any{"quota": common.MaxWalletQuota, "commission_quota": 100}).Error)
	_, err = TransferCommission(unbound.Id, 100, 1000, "overflow-transfer-rollback")
	require.ErrorIs(t, err, ErrWalletQuotaLimitExceeded)
	require.NoError(t, db.First(&unbound, unbound.Id).Error)
	assert.Equal(t, 100, unbound.CommissionQuota)
	assert.Equal(t, common.MaxWalletQuota, unbound.Quota)

	// Two settlement workers racing over the same finalized charge credit once.
	settlementErrors := make(chan error, 2)
	settlementStart := make(chan struct{})
	for range 2 {
		go func() { <-settlementStart; _, e := SettleDailyCommissions(nextDay, 100); settlementErrors <- e }()
	}
	close(settlementStart)
	for range 2 {
		require.NoError(t, <-settlementErrors)
	}
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 31, inviter.CommissionQuota)

	// A payout limit failure rolls back the settlement marker and can retry.
	require.NoError(t, UpdateWalletOrder(unbound.Id, []string{WalletOrdinary}))
	late, err := SetWalletCharge(unbound.Id, "late-wallet-usage", 20, true, true)
	require.NoError(t, err)
	require.NoError(t, db.Model(&inviter).Update("commission_quota", common.MaxWalletQuota).Error)
	_, err = SettleDailyCommissions(nextDay, 100)
	require.ErrorIs(t, err, ErrWalletQuotaLimitExceeded)
	require.NoError(t, db.First(&late, late.Id).Error)
	assert.Zero(t, late.CommissionSettlementID)
	require.NoError(t, db.Model(&inviter).Update("commission_quota", 31).Error)
	_, err = SettleDailyCommissions(nextDay, 100)
	require.NoError(t, err)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 35, inviter.CommissionQuota)

	// Different concurrent transfers still cannot overspend commission.
	require.NoError(t, db.Model(&unbound).Update("quota", 0).Error)
	transferStart := make(chan struct{})
	for _, key := range []string{"parallel-transfer-a", "parallel-transfer-b"} {
		go func() { <-transferStart; _, e := TransferCommission(unbound.Id, 100, 1000, key); results <- e }()
	}
	close(transferStart)
	successful = 0
	for range 2 {
		if <-results == nil {
			successful++
		}
	}
	assert.Equal(t, 1, successful)
	require.NoError(t, db.First(&unbound, unbound.Id).Error)
	assert.Equal(t, 90, unbound.Quota)
	assert.Zero(t, unbound.CommissionQuota)

	// Disabling rewards at settlement closes that day's charges without backfill.
	closed, err := SetWalletCharge(unbound.Id, "disabled-day-usage", 10, true, true)
	require.NoError(t, err)
	require.NoError(t, db.Model(&closed).Updates(map[string]any{"consumed_at": nextDay + 60, "consumption_day": nextDay}).Error)
	common.ReferralEnabled.Store(false)
	_, err = SettleDailyCommissions(nextDay+86400, 100)
	require.NoError(t, err)
	common.ReferralEnabled.Store(true)
	count, err = SettleDailyCommissions(nextDay+86400, 100)
	require.NoError(t, err)
	assert.Zero(t, count)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 35, inviter.CommissionQuota)
}
