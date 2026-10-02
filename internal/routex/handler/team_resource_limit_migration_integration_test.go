package handler

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// Only the released policy identity is reconstructed; evolving business models
// must not define the old column when simulating interrupted migration prefixes.
type teamLimitScopeV38Fixture struct {
	ScopeKind string `gorm:"primaryKey;size:20"`
	ScopeID   string `gorm:"primaryKey;size:30;not null"`
}

func (teamLimitScopeV38Fixture) TableName() string { return "resource_limits" }

func testTeamResourceLimitMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	tokens := int64(17)
	money := "12.3400"
	legacy := entity.ResourceLimit{ScopeKind: "user", ScopeID: "usr_team_policy_upgrade", ETag: "legacy-limit-revision", PreviousETag: "previous", ActorID: "usr_historical", Reason: "Preserve complete historical policy", TokensMonth: &tokens, MoneyMonth: &money, Currency: "USD", IPMode: "none", IPRangesJSON: "[]", UpdatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	var before entity.ResourceLimit
	if err := db.First(&before, "scope_kind = ? AND scope_id = ?", legacy.ScopeKind, legacy.ScopeID).Error; err != nil {
		t.Fatal(err)
	}
	clearLedger := func() {
		t.Helper()
		if err := db.Table("schema_migrations").Where("version = ?", 39).Delete(&struct{}{}).Error; err != nil {
			t.Fatal(err)
		}
	}
	assert := func() {
		t.Helper()
		var after entity.ResourceLimit
		if err := db.First(&after, "scope_kind = ? AND scope_id = ?", legacy.ScopeKind, legacy.ScopeID).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatal("Team migration changed historical policy or timestamp", before, after)
		}
		for _, permission := range []string{"teams.tokens.write", "teams.money.write", "teams.rates.write"} {
			var count int64
			if err := db.Table("role_permissions").Where("role_id = ? AND permission = ?", "rol_admin", permission).Count(&count).Error; err != nil || count != 1 {
				t.Fatal("independent Team authority seed", permission, count, err)
			}
		}
		columns, err := db.Migrator().ColumnTypes(&entity.ResourceLimit{})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, column := range columns {
			if column.Name() == "scope_id" {
				size, ok := column.Length()
				if !ok || size != 64 {
					t.Fatal("stable policy identity must fit full pair", size, ok)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("missing policy identity")
		}
	}
	if err := db.Migrator().AlterColumn(&teamLimitScopeV38Fixture{}, "ScopeID"); err != nil {
		t.Fatal(err)
	}
	clearLedger()
	var group sync.WaitGroup
	failures := make(chan error, 2)
	for range 2 {
		group.Go(func() { failures <- database.Migrate(context.Background(), db) })
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	assert()
	pair := entity.ResourceLimit{ScopeKind: "team_member", ScopeID: strings.Repeat("A", 52), ETag: "stable-pair", PreviousETag: "0", ActorID: "usr_historical", Reason: "Stable Team User pair policy", TokensMonth: &tokens, IPMode: "none", IPRangesJSON: "[]"}
	if err := db.Create(&pair).Error; err != nil {
		t.Fatal("full pair must fit widened scope", err)
	}
	tooLong := pair
	tooLong.ScopeID = strings.Repeat("B", 65)
	if err := db.Create(&tooLong).Error; err == nil {
		t.Fatal("policy scope exceeded the frozen identity bound")
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	for _, permission := range []string{"teams.tokens.write", "teams.money.write", "teams.rates.write"} {
		// Reconstruct a partial prefix after DDL but before each permission seed.
		if err := db.Table("role_permissions").Where("role_id = ? AND permission = ?", "rol_admin", permission).Delete(&struct{}{}).Error; err != nil {
			t.Fatal(err)
		}
		clearLedger()
		if err := database.Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		assert()
		var saved entity.ResourceLimit
		if err := db.First(&saved, "scope_kind = ? AND scope_id = ?", pair.ScopeKind, pair.ScopeID).Error; err != nil || saved.ETag != pair.ETag || saved.TokensMonth == nil || *saved.TokensMonth != tokens {
			t.Fatal("partial seed repair changed stable pair policy", saved, err)
		}
	}
}
