package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// Root registers candidate V65 after V64 and owns actual driver resets.
func testProjectKeyMonthlyQuotaWarningMigration(t *testing.T, db *gorm.DB) {
	const version = 65
	ctx := context.Background()
	observation, inbox := &entity.ProjectKeyQuotaWarningObservation{}, &entity.ProjectKeyQuotaWarningInbox{}
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	asOf, read := start.Add(time.Hour), start.Add(2*time.Hour)
	user := entity.User{ID: "usr_jkw_history", Email: "jkw-history@example.invalid", Name: "Retained manager", PasswordHash: "retained-hash", Role: entity.RoleMember, CreatedAt: start}
	project := entity.Project{ID: "prj_jkw_history", Name: "Retained Project", Status: entity.ResourceActive, CreatorID: user.ID, CreatedAt: start}
	key := entity.ProjectKey{ID: "pky_jkw_history", ProjectID: project.ID, CreatorID: user.ID, DeliveryMode: "manual", Name: "Retained original root", Prefix: "rx_masked", TokenHash: strings.Repeat("a", 64), Status: entity.KeyRevoked, CreatedAt: start}
	memberHistory := entity.TeamMemberQuotaWarningObservation{ID: "mwo_v64_history", TeamID: "tem_v64_history", MemberUserID: user.ID, ScopeID: teamMemberWarningFixtureScope("tem_v64_history", user.ID), UserCreatedAt: start, Dimension: "tokens", MonthStart: start, MonthEnd: start.AddDate(0, 1, 0), PolicyRevision: "lim_v64_history", Level: "near", Threshold: 80, ThresholdGeneration: "team-member-monthly-80-90-v1", TimeZone: "UTC", AsOf: asOf, Limit: "10", Settled: "8", CoverageStart: start, ResourceCreatedAt: start}
	memberInbox := entity.TeamMemberQuotaWarningInbox{ID: "mwi_v64_history", ObservationID: memberHistory.ID, RecipientID: user.ID, RecipientCreatedAt: start, ReadAt: &read, CreatedAt: asOf}
	receipt := entity.TeamCreationReceipt{CreationID: "018f0000-0000-4000-8000-000000000065", ActorID: user.ID, ActorCreatedAt: start, TeamID: "tem_v64_receipt", TeamCreatedAt: start, RequestHash: strings.Repeat("a", 64), ReviewETag: strings.Repeat("b", 64), SnapshotJSON: `{"historical":"retained"}`, CreatedAt: asOf}
	for _, row := range []any{&user, &project, &key, &memberHistory, &memberInbox, &receipt} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal("predecessor setup", err)
		}
	}
	// Preserve all scalar fields, not just row counts or selected timestamps.
	tables := []struct{ name, order string }{
		{"users", "id"}, {"projects", "id"}, {"project_api_keys", "id"}, {"project_api_key_models", "key_id,model_id"}, {"personal_key_quota_warning_observations", "id"}, {"personal_key_quota_warning_inboxes", "id"}, {"resource_limits", "scope_kind,scope_id"},
		{"team_creation_receipts", "creation_id"}, {"team_member_quota_warning_observations", "id"}, {"team_member_quota_warning_inboxes", "id"},
		{"quota_warning_observations", "id"}, {"quota_warning_inboxes", "id"}, {"team_quota_warning_observations", "id"}, {"team_quota_warning_inboxes", "id"},
		{"project_quota_warning_observations", "id"}, {"project_quota_warning_inboxes", "id"}, {"quota_notification_observations", "id"}, {"quota_notification_inboxes", "id"},
	}
	capture := func() map[string][]byte {
		t.Helper()
		result := map[string][]byte{}
		for _, table := range tables {
			var rows []map[string]any
			if err := db.Table(table.name).Order(table.order).Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(rows)
			if err != nil {
				t.Fatal(err)
			}
			result[table.name] = encoded
		}
		return result
	}
	before := capture()
	assertHistory := func() {
		t.Helper()
		after := capture()
		for table, raw := range before {
			if !bytes.Equal(raw, after[table]) {
				t.Fatal("V65 changed predecessor scalar/history fields", table)
			}
		}
	}
	var baseline []int
	if err := db.Table("schema_migrations").Order("version").Pluck("version", &baseline).Error; err != nil {
		t.Fatal(err)
	}
	without := []int{}
	occurrences := 0
	for i, v := range baseline {
		if i > 0 && v <= baseline[i-1] {
			t.Fatal("incoming ledger is not ordered unique")
		}
		if v == version {
			occurrences++
		} else {
			without = append(without, v)
		}
	}
	if occurrences != 1 {
		t.Fatal("V65 must be registered exactly once")
	}
	assertLedger := func(want []int) {
		t.Helper()
		var got []int
		if err := db.Table("schema_migrations").Order("version").Pluck("version", &got).Error; err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("unrelated migration ledger changed", err, got, want)
		}
	}
	remove := func() {
		t.Helper()
		r := db.Table("schema_migrations").Where("version = ?", version).Delete(&struct{}{})
		if r.Error != nil || r.RowsAffected != 1 {
			t.Fatal("own migration ledger absent/duplicated", r.Error)
		}
		assertLedger(without)
	}
	migrate := func() {
		t.Helper()
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
		assertLedger(baseline)
		assertHistory()
	}
	concurrent := func() {
		t.Helper()
		var wg sync.WaitGroup
		failures := make(chan error, 2)
		for range 2 {
			wg.Go(func() { failures <- database.Migrate(ctx, db) })
		}
		wg.Wait()
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		assertLedger(baseline)
		assertHistory()
	}
	for _, model := range []any{inbox, observation} {
		if err := db.Migrator().DropTable(model); err != nil {
			t.Fatal(err)
		}
	}
	remove()
	concurrent()
	migrate()
	constraints := []string{"ck_project_key_quota_warning_root", "ck_project_key_quota_warning_project", "ck_project_key_quota_warning_project_birth", "ck_project_key_quota_warning_dimension", "ck_project_key_quota_warning_calendar", "ck_project_key_quota_warning_currency", "ck_project_key_quota_warning_level", "ck_project_key_quota_warning_generation"}
	if !db.Migrator().HasIndex(&entity.ProjectKey{}, "idx_project_keys_project") || !db.Migrator().HasIndex(&entity.ProjectKey{}, "idx_project_keys_replacement") {
		t.Fatal("released Project candidate indexes missing")
	}

	assertSchema := func() {
		t.Helper()
		for _, model := range []any{observation, inbox} {
			if !db.Migrator().HasTable(model) {
				t.Fatal("warning table missing")
			}
			columns, err := db.Migrator().ColumnTypes(model)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range columns {
				if nullable, known := c.Nullable(); known && nullable && c.Name() != "read_at" {
					t.Fatal("immutable warning column nullable", c.Name())
				}
			}
		}
		if !db.Migrator().HasIndex(&entity.ProjectKey{}, "idx_project_keys_project") || !db.Migrator().HasIndex(&entity.ProjectKey{}, "idx_project_keys_replacement") {
			t.Fatal("V65 changed released Project indexes")
		}

		for _, name := range constraints {
			if !db.Migrator().HasConstraint(observation, name) {
				t.Fatal("warning constraint missing", name)
			}
		}
		if !db.Migrator().HasConstraint(inbox, "ck_project_key_quota_warning_recipient_birth") {
			t.Fatal("recipient birth constraint missing")
		}
		for _, item := range []struct {
			model any
			name  string
		}{{observation, "uq_project_key_quota_warning"}, {inbox, "uq_project_key_quota_warning_inbox"}, {inbox, "idx_project_key_quota_warning_recipient_created"}} {
			if !db.Migrator().HasIndex(item.model, item.name) {
				t.Fatal("warning index missing", item.name)
			}
		}
	}
	assertSchema()
	original := entity.ProjectKeyQuotaWarningObservation{ID: "jwo_retained_warning", RootKeyID: "pky_orphan_history", RootKeyName: "Original historical root", ProjectID: "prj_orphan_history", ProjectCreatedAt: start, Dimension: "tokens", MonthStart: start, MonthEnd: start.AddDate(0, 1, 0), PolicyRevision: "lim_retained_warning", Currency: "", Level: "near", Threshold: 80, ThresholdGeneration: "project-key-monthly-80-90-v1", TimeZone: "UTC", AsOf: asOf, Limit: "10", Settled: "8", CoverageStart: start, ResourceCreatedAt: start}
	saved := entity.ProjectKeyQuotaWarningInbox{ID: "jwi_retained_warning", ObservationID: original.ID, RecipientID: "usr_original_manager", RecipientCreatedAt: start, ReadAt: &read, CreatedAt: asOf}
	for _, row := range []any{&original, &saved} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal("retained history must not require live Key/User foreign keys", err)
		}
	}
	if err := db.Take(&original, "id = ?", original.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&saved, "id = ?", saved.ID).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := original
	duplicate.ID = "jwo_duplicate"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("ten-field warning dedup absent")
	}
	duplicateInbox := saved
	duplicateInbox.ID = "jwi_duplicate"
	if err := db.Create(&duplicateInbox).Error; err == nil {
		t.Fatal("recipient dedup absent")
	}
	for _, name := range []string{"root", "project", "project_birth", "dimension", "calendar", "currency", "level", "generation"} {
		t.Run("invalid_"+name, func(t *testing.T) {
			v := original
			v.ID = "jwo_bad_" + name
			v.PolicyRevision = "lim_bad_" + name
			switch name {
			case "root":
				v.RootKeyID = ""
			case "project":
				v.ProjectID = ""
			case "project_birth":
				v.ProjectCreatedAt = v.ResourceCreatedAt.Add(time.Millisecond)
			case "dimension":
				v.Dimension = "requests"
			case "calendar":
				v.AsOf = v.MonthEnd
			case "currency":
				v.Currency = "USD"
			case "level":
				v.Threshold = 90
			case "generation":
				v.ThresholdGeneration = "future"
			}
			if err := db.Create(&v).Error; err == nil {
				t.Fatal("invalid warning accepted", name)
			}
		})
	}
	for _, name := range []string{"root", "project", "project_birth", "resource_birth", "dimension", "month", "revision", "currency", "level"} {
		t.Run("distinct_"+name, func(t *testing.T) {
			v := original
			v.ID = "jwo_distinct_" + name
			switch name {
			case "root":
				v.RootKeyID = "pky_other_history"
			case "project":
				v.ProjectID = "prj_other_history"
			case "project_birth":
				v.ProjectCreatedAt = start.Add(-time.Millisecond)
			case "resource_birth":
				v.ResourceCreatedAt = start.Add(time.Millisecond)
			case "dimension":
				v.Dimension = "money"
				v.Currency = "USD"
			case "month":
				v.MonthStart = v.MonthEnd
				v.MonthEnd = v.MonthStart.AddDate(0, 1, 0)
				v.AsOf = v.MonthStart.Add(time.Hour)
			case "revision":
				v.PolicyRevision = "lim_distinct"
			case "currency":
				v.Dimension = "money"
				v.Currency = "EUR"
			case "level":
				v.Level = "critical"
				v.Threshold = 90
				v.Settled = "9"
			}
			if err := db.Create(&v).Error; err != nil {
				t.Fatal("distinct immutable identity collapsed", err)
			}
		})
	}
	if err := db.Model(observation).Where("id = ?", original.ID).UpdateColumn("policy_revision", nil).Error; err == nil {
		t.Fatal("revision accepted NULL")
	}
	future := saved
	future.ID = "jwi_future"
	future.RecipientID = "usr_future"
	future.RecipientCreatedAt = future.CreatedAt.Add(time.Millisecond)
	if err := db.Create(&future).Error; err == nil {
		t.Fatal("future recipient accepted")
	}
	if err := db.Model(inbox).Where("id = ?", saved.ID).UpdateColumn("recipient_created_at", nil).Error; err == nil {
		t.Fatal("recipient birth accepted NULL")
	}
	// Partial MySQL DDL is repaired without assuming transactional DDL rollback.
	if err := db.Migrator().DropTable(inbox); err != nil {
		t.Fatal(err)
	}
	for _, name := range constraints {
		if err := db.Migrator().DropConstraint(observation, name); err != nil {
			t.Fatal(err)
		}
	}
	remove()
	concurrent()
	assertSchema()
	if err := db.Create(&saved).Error; err != nil {
		t.Fatal(err)
	}
	migrate()
	assertOwn := func() {
		t.Helper()
		var v entity.ProjectKeyQuotaWarningObservation
		var r entity.ProjectKeyQuotaWarningInbox
		if err := db.Take(&v, "id = ?", original.ID).Error; err != nil || !projectKeyWarningObservationEqual(v, original) {
			t.Fatal("observation changed", err)
		}
		if err := db.Take(&r, "id = ?", saved.ID).Error; err != nil || !projectKeyWarningInboxEqual(r, saved) {
			t.Fatal("read receipt changed", err)
		}
	}
	assertOwn()
	for _, model := range []any{inbox, observation} {
		if err := db.Migrator().DropTable(model); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrator().CreateTable(&projectKeyWarningPartialObservation{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().CreateTable(&projectKeyWarningPartialInbox{}); err != nil {
		t.Fatal(err)
	}
	remove()
	migrate()
	assertSchema()
	for _, row := range []any{&original, &saved} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	concurrent()
	migrate()
	assertOwn()
	assertHistory()
	assertLedger(baseline)
}

type projectKeyWarningPartialObservation struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (projectKeyWarningPartialObservation) TableName() string {
	return "project_key_quota_warning_observations"
}

type projectKeyWarningPartialInbox struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (projectKeyWarningPartialInbox) TableName() string { return "project_key_quota_warning_inboxes" }

func projectKeyWarningObservationEqual(a, b entity.ProjectKeyQuotaWarningObservation) bool {
	if !a.MonthStart.Equal(b.MonthStart) || !a.MonthEnd.Equal(b.MonthEnd) || !a.AsOf.Equal(b.AsOf) || !a.CoverageStart.Equal(b.CoverageStart) || !a.ResourceCreatedAt.Equal(b.ResourceCreatedAt) || !a.ProjectCreatedAt.Equal(b.ProjectCreatedAt) {
		return false
	}
	a.MonthStart, a.MonthEnd, a.AsOf, a.CoverageStart, a.ResourceCreatedAt, a.ProjectCreatedAt = b.MonthStart, b.MonthEnd, b.AsOf, b.CoverageStart, b.ResourceCreatedAt, b.ProjectCreatedAt
	return reflect.DeepEqual(a, b)
}
func projectKeyWarningInboxEqual(a, b entity.ProjectKeyQuotaWarningInbox) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) || !a.RecipientCreatedAt.Equal(b.RecipientCreatedAt) || (a.ReadAt == nil) != (b.ReadAt == nil) {
		return false
	}
	if a.ReadAt != nil && !a.ReadAt.Equal(*b.ReadAt) {
		return false
	}
	a.CreatedAt, a.RecipientCreatedAt, a.ReadAt = b.CreatedAt, b.RecipientCreatedAt, b.ReadAt
	return reflect.DeepEqual(a, b)
}
func TestProjectKeyWarningFixtureImmutableBirthAndSnapshot(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 123000000, time.UTC)
	other := now.In(time.FixedZone("same-instant", 3600))
	a := entity.ProjectKeyQuotaWarningObservation{RootKeyID: "pky_exact", ProjectID: "prj_exact", ProjectCreatedAt: now, MonthStart: now, MonthEnd: now.AddDate(0, 1, 0), AsOf: now, CoverageStart: now, ResourceCreatedAt: now, Settled: "8"}
	b := a
	b.ProjectCreatedAt, b.MonthStart, b.MonthEnd, b.AsOf, b.CoverageStart, b.ResourceCreatedAt = other, other, other.AddDate(0, 1, 0), other, other, other
	if !projectKeyWarningObservationEqual(a, b) {
		t.Fatal("equal birth instants rejected")
	}
	for _, field := range []string{"root", "project", "birth", "settled", "name"} {
		bad := b
		switch field {
		case "root":
			bad.RootKeyID = "PKY_EXACT"
		case "project":
			bad.ProjectID = "PRJ_EXACT"
		case "birth":
			bad.ProjectCreatedAt = other.Add(time.Millisecond)
		case "settled":
			bad.Settled = "9"
		case "name":
			bad.RootKeyName = "New name"
		}
		if projectKeyWarningObservationEqual(a, bad) {
			t.Fatal("immutable fact lost", field)
		}
	}
	read := now.Add(time.Hour)
	x := entity.ProjectKeyQuotaWarningInbox{ID: "jwi_exact", RecipientID: "usr_original_manager", RecipientCreatedAt: now, CreatedAt: now, ReadAt: &read}
	y := x
	localRead := read.In(other.Location())
	y.ReadAt = &localRead
	y.CreatedAt = other
	y.RecipientCreatedAt = other
	if !projectKeyWarningInboxEqual(x, y) {
		t.Fatal("equal receipt instants rejected")
	}
	y.RecipientCreatedAt = other.Add(time.Millisecond)
	if projectKeyWarningInboxEqual(x, y) {
		t.Fatal("recipient birth comparison lost")
	}
}
