package handler

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

// Root registers V58 and this case after the complete accepted predecessor.
// Historical projections avoid prepared SELECT * result-shape changes across DDL.
type registrationDomainHistoricalPolicy struct {
	ID                           int
	RegistrationEnabled          bool
	RegistrationApprovalRequired bool
	RegistrationPolicyRevision   string
}

func (registrationDomainHistoricalPolicy) TableName() string { return "governance_settings" }

type registrationDomainPartialPolicy struct {
	ID                              int     `gorm:"primaryKey"`
	RegistrationAllowedEmailDomains *string `gorm:"size:4096"`
}

func (registrationDomainPartialPolicy) TableName() string { return "governance_settings" }
func testRegistrationEmailDomainsMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Initialize(ctx, "domain-migration@example.invalid", "test-only-domain-migration-password", "Domain migration"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.GovernanceSetting{}).Where("id = ?", 1).UpdateColumns(map[string]any{"RegistrationEnabled": true, "RegistrationApprovalRequired": true, "RegistrationPolicyRevision": strings.Repeat("a", 64)}).Error; err != nil {
		t.Fatal(err)
	}
	historical := func() registrationDomainHistoricalPolicy {
		t.Helper()
		var value registrationDomainHistoricalPolicy
		if err := db.Select("id", "registration_enabled", "registration_approval_required", "registration_policy_revision").First(&value, "id = ?", 1).Error; err != nil {
			t.Fatal(err)
		}
		return value
	}
	baseline := historical()
	var usersBefore, usersAfter []entity.User
	var sessionsBefore, sessionsAfter []entity.Session
	if err := db.Order("id").Find(&usersBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&sessionsBefore).Error; err != nil {
		t.Fatal(err)
	}
	assertHistory := func() {
		t.Helper()
		if !reflect.DeepEqual(historical(), baseline) {
			t.Fatal("V58 changed surrounding policy fields")
		}
		if err := db.Order("id").Find(&usersAfter).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Order("id").Find(&sessionsAfter).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(usersBefore, usersAfter) || !reflect.DeepEqual(sessionsBefore, sessionsAfter) {
			t.Fatal("V58 changed retained User/Session data")
		}
	}
	ledger := func() int64 {
		t.Helper()
		var n int64
		if err := db.Table("schema_migrations").Where("version = ?", 58).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	removeLedger := func() {
		t.Helper()
		q := db.Table("schema_migrations").Where("version = ?", 58).Delete(&struct{}{})
		if q.Error != nil || q.RowsAffected != 1 {
			t.Fatal("V58 ledger reconstruction", q.Error)
		}
	}
	// This fixture changes a retained column's type/width deliberately. No
	// transaction or migration goroutine is active at these drain boundaries.
	// Closing idle physical connections clears driver-owned prepared descriptions;
	// restore database/sql's default idle allowance, without changing production Open.
	drainMigrationCache := func() {
		t.Helper()
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		pool.SetMaxIdleConns(0)
		pool.SetMaxIdleConns(2)
	}
	migrate := func() {
		t.Helper()
		drainMigrationCache()
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
		drainMigrationCache()
		if ledger() != 1 {
			t.Fatal("V58 candidate was not registered exactly once")
		}
		assertHistory()
	}
	if ledger() != 1 {
		t.Fatal("root must register V58 before this actual fixture")
	}
	removeLedger()
	if err := db.Migrator().DropColumn(&registrationDomainPartialPolicy{}, "RegistrationAllowedEmailDomains"); err != nil {
		t.Fatal(err)
	}
	assertHistory()
	migrate()
	var current entity.GovernanceSetting
	read := func() {
		t.Helper()
		if err := db.Select("id", "registration_enabled", "registration_approval_required", "registration_policy_revision", "registration_allowed_email_domains").First(&current, "id = ?", 1).Error; err != nil {
			t.Fatal(err)
		}
	}
	read()
	if current.RegistrationAllowedEmailDomains != "[]" {
		t.Fatal("historical unrestricted baseline not retained")
	}
	saved := `["a.invalid","b.invalid"]`
	if err := db.Model(&entity.GovernanceSetting{}).Where("id = ?", 1).UpdateColumn("RegistrationAllowedEmailDomains", saved).Error; err != nil {
		t.Fatal(err)
	}
	migrate()
	read()
	if current.RegistrationAllowedEmailDomains != saved {
		t.Fatal("repeat migration reset saved domain policy")
	}
	removeLedger()
	var group sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		group.Go(func() { results <- database.Migrate(ctx, db) })
	}
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if ledger() != 1 {
		t.Fatal("concurrent V58 duplicated ledger")
	}
	assertHistory()
	read()
	if current.RegistrationAllowedEmailDomains != saved {
		t.Fatal("concurrent V58 reset saved policy")
	}
	// Simulate MySQL-compatible partial DDL: column exists, NULL, wrong width/default.
	removeLedger()
	if err := db.Migrator().DropColumn(&registrationDomainPartialPolicy{}, "RegistrationAllowedEmailDomains"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().AddColumn(&registrationDomainPartialPolicy{}, "RegistrationAllowedEmailDomains"); err != nil {
		t.Fatal(err)
	}
	migrate()
	read()
	if current.RegistrationAllowedEmailDomains != "[]" {
		t.Fatal("partial NULL backfill incorrect")
	}
	if err := db.Model(&entity.GovernanceSetting{}).Where("id = ?", 1).UpdateColumn("RegistrationAllowedEmailDomains", nil).Error; err == nil {
		t.Fatal("domain column not null constraint absent")
	}
	removeLedger()
	invalid := `["A.invalid"]`
	if err := db.Model(&entity.GovernanceSetting{}).Where("id = ?", 1).UpdateColumn("RegistrationAllowedEmailDomains", invalid).Error; err != nil {
		t.Fatal(err)
	}
	drainMigrationCache()
	if err := database.Migrate(ctx, db); err == nil || err.Error() != "migration 58: invalid retained registration domain policy" || ledger() != 0 {
		t.Fatal("corrupt retained policy must fail its validation without recording a ledger row", err)
	}
	read()
	if current.RegistrationAllowedEmailDomains != invalid {
		t.Fatal("failed migration rewrote retained invalid bytes")
	}
	assertHistory()
	if err := db.Model(&entity.GovernanceSetting{}).Where("id = ?", 1).UpdateColumn("RegistrationAllowedEmailDomains", saved).Error; err != nil {
		t.Fatal(err)
	}
	migrate()
}
