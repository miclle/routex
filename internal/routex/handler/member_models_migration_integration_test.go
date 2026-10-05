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
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
)

// Private projections reconstruct V53 without evolving business schema tags.
type memberModelsRevisionV54Fixture struct {
	ID                    string `gorm:"primaryKey;size:30"`
	PersonalGrantRevision string `gorm:"size:64"`
}

func (memberModelsRevisionV54Fixture) TableName() string { return "users" }

type memberModelsPartialV54Fixture struct {
	ID                    string  `gorm:"primaryKey;size:30"`
	PersonalGrantRevision *string `gorm:"size:64"`
}

func (memberModelsPartialV54Fixture) TableName() string { return "users" }

// The root-owned identity harness supplies a disposable current-schema database.
func testMemberModelsMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	readLedger := func() []int64 {
		t.Helper()
		var versions []int64
		if err := db.Table("schema_migrations").Order("version").Pluck("version", &versions).Error; err != nil {
			t.Fatal("cannot read exact incoming migration ledger", err)
		}
		return versions
	}
	baselineLedger := readLedger()
	withoutV54 := []int64{}
	var v54Rows int
	for index, version := range baselineLedger {
		if version <= 0 || index > 0 && baselineLedger[index-1] >= version {
			t.Fatal("incoming migration versions are not ordered and unique", baselineLedger)
		}
		if version == 54 {
			v54Rows++
		} else {
			withoutV54 = append(withoutV54, version)
		}
	}
	if v54Rows != 1 {
		t.Fatal("incoming migration ledger must contain V54 exactly once", v54Rows)
	}
	assertLedger := func(want []int64) {
		t.Helper()
		if got := readLedger(); !reflect.DeepEqual(got, want) {
			t.Fatal("V54 replay changed the exact migration version list", got, want)
		}
	}
	column := &memberModelsRevisionV54Fixture{}
	guard := "ck_users_personal_grant_revision"
	initial := strings.Repeat("0", 64)
	if !db.Migrator().HasColumn(column, "PersonalGrantRevision") || !db.Migrator().HasConstraint(column, guard) {
		t.Fatal("fresh V54 omitted private revision or shape guard")
	}
	newID := func(prefix string) string {
		t.Helper()
		value, err := id.NewPrefixed(prefix)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 123000000, time.UTC)
	users := []entity.User{}
	models := []entity.Model{}
	for index, suffix := range []string{"enabled", "disabled", "offboarded"} {
		user := entity.User{ID: newID("usr"), Email: "member-model-migration-" + suffix + "@example.invalid", Name: "Historical " + suffix, PasswordHash: "historical-fixture-hash", Role: entity.RoleMember, Disabled: index > 0, CreatedAt: stamp, UpdatedAt: stamp}
		if index == 2 {
			date := stamp.Add(time.Hour)
			user.OffboardedAt = &date
		}
		if err := db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
		model := entity.Model{ID: newID("mdl"), Status: entity.ResourceActive, CreatedAt: stamp}
		if err := db.Create(&model).Error; err != nil {
			t.Fatal(err)
		}
		grant := entity.UserModelGrant{UserID: user.ID, ModelID: model.ID, CreatedAt: stamp}
		if index == 1 {
			source := newID("mar")
			grant.SourceRequestID = &source
		}
		if err := db.Create(&grant).Error; err != nil {
			t.Fatal(err)
		}
		users = append(users, user)
		models = append(models, model)
	}
	userIDs := []string{users[0].ID, users[1].ID, users[2].ID}
	// Always explicit: dropping/readding the revision cannot change a cached
	// SELECT * result shape, and complete historical fields remain compared.
	readHistory := func() ([]entity.User, []entity.UserModelGrant) {
		t.Helper()
		var rows []entity.User
		var grants []entity.UserModelGrant
		if err := db.Select("id", "email", "name", "password_hash", "role", "disabled", "offboarded_at", "created_at", "updated_at").Where("id IN ?", userIDs).Order("id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Select("user_id", "model_id", "created_at", "source_request_id").Where("user_id IN ?", userIDs).Order("user_id, model_id").Find(&grants).Error; err != nil {
			t.Fatal(err)
		}
		return rows, grants
	}
	baselineUsers, baselineGrants := readHistory()
	assertHistory := func() {
		t.Helper()
		currentUsers, currentGrants := readHistory()
		if !reflect.DeepEqual(currentUsers, baselineUsers) || !reflect.DeepEqual(currentGrants, baselineGrants) {
			t.Fatal("revision migration changed retained user fields or grant provenance/timestamps")
		}
	}
	readRevision := func(userID string) string {
		t.Helper()
		var current memberModelsRevisionV54Fixture
		if err := db.Select("id", "personal_grant_revision").First(&current, "id = ?", userID).Error; err != nil {
			t.Fatal(err)
		}
		if current.ID != userID {
			t.Fatal("revision read borrowed another user")
		}
		return current.PersonalGrantRevision
	}
	for _, userID := range userIDs {
		if readRevision(userID) != initial {
			t.Fatal("new retained user did not receive explicit historical sentinel")
		}
	}
	removeLedger := func() {
		t.Helper()
		result := db.Table("schema_migrations").Where("version = ?", 54).Delete(&struct{}{})
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("cannot reconstruct V53 ledger", result.RowsAffected, result.Error)
		}
		assertLedger(withoutV54)
	}
	concurrent := func() {
		t.Helper()
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for range 2 {
			wg.Go(func() { results <- database.Migrate(ctx, db) })
		}
		wg.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatal("concurrent V54 failed", err)
			}
		}
		assertLedger(baselineLedger)
	}
	removeLedger()
	if err := db.Migrator().DropConstraint(column, guard); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(column, "PersonalGrantRevision"); err != nil {
		t.Fatal(err)
	}
	assertHistory()
	concurrent()
	assertHistory()
	for _, userID := range userIDs {
		if readRevision(userID) != initial {
			t.Fatal("upgrade invented a historical grant operation")
		}
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	assertLedger(baselineLedger)
	assertHistory()
	// Partial DDL: existing nullable/blank values are baselines; an already
	// recorded nonzero generation is immutable and must not be reset.
	removeLedger()
	if err := db.Migrator().DropConstraint(column, guard); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(column, "PersonalGrantRevision"); err != nil {
		t.Fatal(err)
	}
	partial := &memberModelsPartialV54Fixture{}
	if err := db.Migrator().AddColumn(partial, "PersonalGrantRevision"); err != nil {
		t.Fatal(err)
	}
	nonzero := strings.Repeat("ab", 32)
	for index, value := range []any{"", nil, nonzero} {
		if err := db.Model(partial).Where("id = ?", userIDs[index]).UpdateColumns(map[string]any{"personal_grant_revision": value}).Error; err != nil {
			t.Fatal(err)
		}
	}
	assertHistory()
	concurrent()
	assertHistory()
	for index, userID := range userIDs {
		want := initial
		if index == 2 {
			want = nonzero
		}
		if readRevision(userID) != want {
			t.Fatal("partial DDL erased current revision or invented historical operation")
		}
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	assertLedger(baselineLedger)
	assertHistory()
	if !db.Migrator().HasConstraint(column, guard) {
		t.Fatal("partial DDL did not restore revision guard")
	}
	for _, invalid := range []any{nil, "", strings.Repeat("0", 63), strings.Repeat("0", 65), strings.Repeat("A", 64), "g" + strings.Repeat("0", 63), strings.Repeat("0", 63) + " "} {
		if err := db.Model(column).Where("id = ?", users[0].ID).UpdateColumns(map[string]any{"personal_grant_revision": invalid}).Error; err == nil {
			t.Fatal("revision constraint accepted invalid shape")
		}
		if readRevision(users[0].ID) != initial {
			t.Fatal("rejected revision update changed baseline")
		}
		assertHistory()
	}
	assertLedger(baselineLedger)
	if err := db.Where("user_id IN ?", userIDs).Delete(&entity.UserModelGrant{}).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range models {
		if err := db.Delete(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Where("id IN ?", userIDs).Delete(&entity.User{}).Error; err != nil {
		t.Fatal(err)
	}
}
