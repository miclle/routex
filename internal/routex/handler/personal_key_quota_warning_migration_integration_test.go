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

// The root harness registers V64 after checked V63 and owns database reset.
func testPersonalKeyMonthlyQuotaWarningMigration(t *testing.T, db *gorm.DB) {
	const version = 64
	ctx := context.Background()
	observation, inbox := &entity.PersonalKeyQuotaWarningObservation{}, &entity.PersonalKeyQuotaWarningInbox{}
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	asOf, read := start.Add(time.Hour), start.Add(2*time.Hour)
	user := entity.User{ID: "usr_pkw_history", Email: "pkw-history@example.invalid", Name: "Retained owner", PasswordHash: "retained-hash", Role: entity.RoleMember, CreatedAt: start}
	key := entity.APIKey{ID: "key_pkw_history", UserID: user.ID, Name: "Retained original root", Prefix: "rx_masked", TokenHash: strings.Repeat("a", 64), Status: entity.KeyRevoked, LifecycleRevision: "kvr_pkw_history", CreatedAt: start}
	memberHistory := entity.TeamMemberQuotaWarningObservation{ID: "mwo_v64_history", TeamID: "tem_v64_history", MemberUserID: user.ID, ScopeID: teamMemberWarningFixtureScope("tem_v64_history", user.ID), UserCreatedAt: start, Dimension: "tokens", MonthStart: start, MonthEnd: start.AddDate(0, 1, 0), PolicyRevision: "lim_v64_history", Level: "near", Threshold: 80, ThresholdGeneration: "team-member-monthly-80-90-v1", TimeZone: "UTC", AsOf: asOf, Limit: "10", Settled: "8", CoverageStart: start, ResourceCreatedAt: start}
	memberInbox := entity.TeamMemberQuotaWarningInbox{ID: "mwi_v64_history", ObservationID: memberHistory.ID, RecipientID: user.ID, RecipientCreatedAt: start, ReadAt: &read, CreatedAt: asOf}
	receipt := entity.TeamCreationReceipt{CreationID: "018f0000-0000-4000-8000-000000000064", ActorID: user.ID, ActorCreatedAt: start, TeamID: "tem_v64_receipt", TeamCreatedAt: start, RequestHash: strings.Repeat("a", 64), ReviewETag: strings.Repeat("b", 64), SnapshotJSON: `{"historical":"retained"}`, CreatedAt: asOf}
	for _, row := range []any{&user, &key, &memberHistory, &memberInbox, &receipt} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal("predecessor setup", err)
		}
	}
	// Preserve all scalar fields, not just row counts or selected timestamps.
	tables := []struct{ name, order string }{
		{"users", "id"}, {"api_keys", "id"}, {"api_key_models", "key_id,model_id"}, {"resource_limits", "scope_kind,scope_id"},
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
				t.Fatal("V64 changed predecessor scalar/history fields", table)
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
		t.Fatal("V64 must be registered exactly once")
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
	// MySQL may use the new owner index to support its existing user foreign key.
	// Supply a test-owned GORM index while reconstructing the missing V64 index.
	fallback := &personalKeyWarningFixtureOwnerIndex{}
	if db.Name() == "mysql" {
		if db.Migrator().HasIndex(fallback, "idx_pkw_fixture_owner_fk") {
			t.Fatal("fixture index unexpectedly exists")
		}
		if err := db.Migrator().CreateIndex(fallback, "idx_pkw_fixture_owner_fk"); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if db.Migrator().HasIndex(fallback, "idx_pkw_fixture_owner_fk") {
				if err := db.Migrator().DropIndex(fallback, "idx_pkw_fixture_owner_fk"); err != nil {
					t.Error("fixture-only index cleanup", err)
				}
			}
		}()
	}
	// Removing the new owner index never changes retained Key rows or other indexes.
	if db.Name() == "postgres" {
		// Pinned PostgreSQL GORM DropIndex renders invalid CURRENT_SCHEMA().index.
		// This one fixed test-only DDL reconstructs a missing index; production stays GORM.
		if err := db.Exec(`DROP INDEX "idx_personal_key_warning_owner"`).Error; err != nil {
			t.Fatal(err)
		}
	} else if err := db.Migrator().DropIndex(&entity.APIKey{}, "idx_personal_key_warning_owner"); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasIndex(&entity.APIKey{}, "idx_personal_key_warning_owner") {
		t.Fatal("owner index fault was not applied")
	}

	remove()
	concurrent()
	migrate()
	constraints := []string{"ck_personal_key_quota_warning_root", "ck_personal_key_quota_warning_owner", "ck_personal_key_quota_warning_owner_birth", "ck_personal_key_quota_warning_dimension", "ck_personal_key_quota_warning_calendar", "ck_personal_key_quota_warning_currency", "ck_personal_key_quota_warning_level", "ck_personal_key_quota_warning_generation"}
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
		for _, name := range constraints {
			if !db.Migrator().HasConstraint(observation, name) {
				t.Fatal("warning constraint missing", name)
			}
		}
		if !db.Migrator().HasConstraint(inbox, "ck_personal_key_quota_warning_recipient_birth") {
			t.Fatal("recipient birth constraint missing")
		}
		for _, item := range []struct {
			model any
			name  string
		}{{observation, "uq_personal_key_quota_warning"}, {inbox, "uq_personal_key_quota_warning_inbox"}, {inbox, "idx_personal_key_quota_warning_recipient_created"}, {&entity.APIKey{}, "idx_personal_key_warning_owner"}} {
			if !db.Migrator().HasIndex(item.model, item.name) {
				t.Fatal("warning index missing", item.name)
			}
		}
		if columns := personalKeyWarningFixtureOwnerIndexColumns(t, db); !reflect.DeepEqual(columns, []string{"user_id", "id"}) {
			t.Fatalf("owner index must preserve exact owner/id order: %v", columns)
		}
	}
	assertSchema()
	original := entity.PersonalKeyQuotaWarningObservation{ID: "kwo_retained_warning", RootKeyID: "key_orphan_history", RootKeyName: "Original historical root", OwnerID: "usr_orphan_history", OwnerCreatedAt: start, Dimension: "tokens", MonthStart: start, MonthEnd: start.AddDate(0, 1, 0), PolicyRevision: "lim_retained_warning", Currency: "", Level: "near", Threshold: 80, ThresholdGeneration: "personal-key-monthly-80-90-v1", TimeZone: "UTC", AsOf: asOf, Limit: "10", Settled: "8", CoverageStart: start, ResourceCreatedAt: start}
	saved := entity.PersonalKeyQuotaWarningInbox{ID: "kwi_retained_warning", ObservationID: original.ID, RecipientID: original.OwnerID, RecipientCreatedAt: start, ReadAt: &read, CreatedAt: asOf}
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
	duplicate.ID = "kwo_duplicate"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("ten-field warning dedup absent")
	}
	duplicateInbox := saved
	duplicateInbox.ID = "kwi_duplicate"
	if err := db.Create(&duplicateInbox).Error; err == nil {
		t.Fatal("recipient dedup absent")
	}
	for _, name := range []string{"root", "owner", "owner_birth", "dimension", "calendar", "currency", "level", "generation"} {
		t.Run("invalid_"+name, func(t *testing.T) {
			v := original
			v.ID = "kwo_bad_" + name
			v.PolicyRevision = "lim_bad_" + name
			switch name {
			case "root":
				v.RootKeyID = ""
			case "owner":
				v.OwnerID = ""
			case "owner_birth":
				v.OwnerCreatedAt = v.ResourceCreatedAt.Add(time.Millisecond)
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
	for _, name := range []string{"root", "owner", "owner_birth", "resource_birth", "dimension", "month", "revision", "currency", "level"} {
		t.Run("distinct_"+name, func(t *testing.T) {
			v := original
			v.ID = "kwo_distinct_" + name
			switch name {
			case "root":
				v.RootKeyID = "key_other_history"
			case "owner":
				v.OwnerID = "usr_other_history"
			case "owner_birth":
				v.OwnerCreatedAt = start.Add(-time.Millisecond)
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
	future.ID = "kwi_future"
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
		var v entity.PersonalKeyQuotaWarningObservation
		var r entity.PersonalKeyQuotaWarningInbox
		if err := db.Take(&v, "id = ?", original.ID).Error; err != nil || !personalKeyWarningObservationEqual(v, original) {
			t.Fatal("observation changed", err)
		}
		if err := db.Take(&r, "id = ?", saved.ID).Error; err != nil || !personalKeyWarningInboxEqual(r, saved) {
			t.Fatal("read receipt changed", err)
		}
	}
	assertOwn()
	for _, model := range []any{inbox, observation} {
		if err := db.Migrator().DropTable(model); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrator().CreateTable(&personalKeyWarningPartialObservation{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().CreateTable(&personalKeyWarningPartialInbox{}); err != nil {
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

type personalKeyWarningPartialObservation struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (personalKeyWarningPartialObservation) TableName() string {
	return "personal_key_quota_warning_observations"
}

type personalKeyWarningPartialInbox struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (personalKeyWarningPartialInbox) TableName() string { return "personal_key_quota_warning_inboxes" }

func personalKeyWarningObservationEqual(a, b entity.PersonalKeyQuotaWarningObservation) bool {
	if !a.MonthStart.Equal(b.MonthStart) || !a.MonthEnd.Equal(b.MonthEnd) || !a.AsOf.Equal(b.AsOf) || !a.CoverageStart.Equal(b.CoverageStart) || !a.ResourceCreatedAt.Equal(b.ResourceCreatedAt) || !a.OwnerCreatedAt.Equal(b.OwnerCreatedAt) {
		return false
	}
	a.MonthStart, a.MonthEnd, a.AsOf, a.CoverageStart, a.ResourceCreatedAt, a.OwnerCreatedAt = b.MonthStart, b.MonthEnd, b.AsOf, b.CoverageStart, b.ResourceCreatedAt, b.OwnerCreatedAt
	return reflect.DeepEqual(a, b)
}
func personalKeyWarningInboxEqual(a, b entity.PersonalKeyQuotaWarningInbox) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) || !a.RecipientCreatedAt.Equal(b.RecipientCreatedAt) || (a.ReadAt == nil) != (b.ReadAt == nil) {
		return false
	}
	if a.ReadAt != nil && !a.ReadAt.Equal(*b.ReadAt) {
		return false
	}
	a.CreatedAt, a.RecipientCreatedAt, a.ReadAt = b.CreatedAt, b.RecipientCreatedAt, b.ReadAt
	return reflect.DeepEqual(a, b)
}
func TestPersonalKeyWarningFixtureImmutableBirthAndSnapshot(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 123000000, time.UTC)
	other := now.In(time.FixedZone("same-instant", 3600))
	a := entity.PersonalKeyQuotaWarningObservation{RootKeyID: "key_exact", OwnerID: "usr_exact", OwnerCreatedAt: now, MonthStart: now, MonthEnd: now.AddDate(0, 1, 0), AsOf: now, CoverageStart: now, ResourceCreatedAt: now, Settled: "8"}
	b := a
	b.OwnerCreatedAt, b.MonthStart, b.MonthEnd, b.AsOf, b.CoverageStart, b.ResourceCreatedAt = other, other, other.AddDate(0, 1, 0), other, other, other
	if !personalKeyWarningObservationEqual(a, b) {
		t.Fatal("equal birth instants rejected")
	}
	for _, field := range []string{"root", "owner", "birth", "settled", "name"} {
		bad := b
		switch field {
		case "root":
			bad.RootKeyID = "KEY_EXACT"
		case "owner":
			bad.OwnerID = "USR_EXACT"
		case "birth":
			bad.OwnerCreatedAt = other.Add(time.Millisecond)
		case "settled":
			bad.Settled = "9"
		case "name":
			bad.RootKeyName = "New name"
		}
		if personalKeyWarningObservationEqual(a, bad) {
			t.Fatal("immutable fact lost", field)
		}
	}
	read := now.Add(time.Hour)
	x := entity.PersonalKeyQuotaWarningInbox{ID: "kwi_exact", RecipientID: a.OwnerID, RecipientCreatedAt: now, CreatedAt: now, ReadAt: &read}
	y := x
	localRead := read.In(other.Location())
	y.ReadAt = &localRead
	y.CreatedAt = other
	y.RecipientCreatedAt = other
	if !personalKeyWarningInboxEqual(x, y) {
		t.Fatal("equal receipt instants rejected")
	}
	y.RecipientCreatedAt = other.Add(time.Millisecond)
	if personalKeyWarningInboxEqual(x, y) {
		t.Fatal("recipient birth comparison lost")
	}
}

// Frozen fixture-only index keeps the released MySQL foreign key supported.
type personalKeyWarningFixtureOwnerIndex struct {
	UserID string `gorm:"size:30;not null;index:idx_pkw_fixture_owner_fk"`
}

func (personalKeyWarningFixtureOwnerIndex) TableName() string { return "api_keys" }

func personalKeyWarningFixtureOwnerIndexColumns(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	if db.Name() == "postgres" {
		// Pinned GORM PostgreSQL GetIndexes joins pg_attribute via ANY(indkey)
		// without key ordinality/order. Read the physical index keys in order;
		// fixed table/index names are bound values, as in existing migration fixtures.
		var ordered []string
		if err := db.Raw(`SELECT a.attname FROM pg_index i
			JOIN pg_class x ON x.oid = i.indexrelid
			JOIN pg_class t ON t.oid = i.indrelid
			JOIN pg_namespace n ON n.oid = t.relnamespace
			CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS k(attnum, position)
			JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
			WHERE n.nspname = current_schema() AND t.relname = ? AND x.relname = ?
			ORDER BY k.position`, "api_keys", "idx_personal_key_warning_owner").Scan(&ordered).Error; err != nil {
			t.Fatal(err)
		}
		return ordered
	}
	indexes, err := db.Migrator().GetIndexes(&entity.APIKey{})
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range indexes {
		if index.Name() == "idx_personal_key_warning_owner" {
			return index.Columns()
		}
	}
	t.Fatal("owner index introspection absent")
	return nil
}
