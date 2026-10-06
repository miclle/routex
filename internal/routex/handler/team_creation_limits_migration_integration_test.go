package handler

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/limits"
	"gorm.io/gorm"
)

// Frozen test schema isolates V63 reconstruction from later additive columns.
type teamCreationReceiptFixtureV63 struct {
	CreationID     string    `gorm:"primaryKey;size:36;check:ck_team_creation_intent,CHAR_LENGTH(creation_id) = 36 AND CHAR_LENGTH(request_hash) = 64 AND CHAR_LENGTH(review_etag) = 64"`
	ActorID        string    `gorm:"size:30;not null;index:idx_team_creation_actor"`
	ActorCreatedAt time.Time `gorm:"precision:6;not null"`
	TeamID         string    `gorm:"size:30;not null;uniqueIndex:uq_team_creation_team"`
	TeamCreatedAt  time.Time `gorm:"precision:6;not null"`
	RequestHash    string    `gorm:"size:64;not null"`
	ReviewETag     string    `gorm:"column:review_etag;size:64;not null"`
	SnapshotJSON   string    `gorm:"size:131072;not null;check:ck_team_creation_snapshot,OCTET_LENGTH(snapshot_json) <= 131072"`
	CreatedAt      time.Time `gorm:"precision:6;not null"`
}

func (teamCreationReceiptFixtureV63) TableName() string { return "team_creation_receipts" }

// Root assigns V63 after all checked predecessors; the fixture never invokes an
// unexported migration or registers a candidate on its own.
func testTeamCreationLimitsMigration(t *testing.T, db *gorm.DB) {
	const candidateVersion = 63
	// An explicit frozen projection prevents prepared SELECT * result shapes
	// from changing when V66 adds columns after the V63 reconstruction.
	receiptColumns := []string{"creation_id", "actor_id", "actor_created_at", "team_id", "team_created_at", "request_hash", "review_etag", "snapshot_json", "created_at"}
	ctx := context.Background()
	var baseline []int
	if err := db.Table("schema_migrations").Order("version").Pluck("version", &baseline).Error; err != nil {
		t.Fatal(err)
	}
	without := []int{}
	count := 0
	for i, version := range baseline {
		if i > 0 && version <= baseline[i-1] {
			t.Fatal("incoming ledger not ordered unique")
		}
		if version == candidateVersion {
			count++
		} else {
			without = append(without, version)
		}
	}
	if count != 1 {
		t.Fatal("root must register V63 exactly once")
	}
	ledger := func(want []int) {
		t.Helper()
		var got []int
		if err := db.Table("schema_migrations").Order("version").Pluck("version", &got).Error; err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("unrelated ledger changed", err, got, want)
		}
	}
	remove := func() {
		t.Helper()
		res := db.Table("schema_migrations").Where("version = ?", candidateVersion).Delete(&struct{}{})
		if res.Error != nil || res.RowsAffected != 1 {
			t.Fatal("candidate ledger removal", res.Error)
		}
		ledger(without)
	}
	migrate := func() {
		t.Helper()
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
		ledger(baseline)
	}
	concurrent := func() {
		t.Helper()
		failures := make(chan error, 2)
		var wg sync.WaitGroup
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
		ledger(baseline)
	}
	// Populate retained identity, policy, relationship and warning history before
	// reconstructing the receipt schema; the candidate cannot rewrite them.
	stamp := time.Date(2026, 4, 5, 6, 7, 8, 123000000, time.UTC)
	owner := entity.User{ID: "usr_tc63_history", Email: "tc63-history@example.invalid", Name: "Retained identity", Role: entity.RoleMember, PasswordHash: "test-only-not-authenticated", CreatedAt: stamp, UpdatedAt: stamp}
	team := entity.Team{ID: "tea_tc63_history", Name: "Retained Team", Status: entity.ResourceActive, CreatedAt: stamp, UpdatedAt: stamp}
	joined := stamp
	membership := entity.TeamMembership{ID: "tmm_tc63_history", TeamID: team.ID, UserID: owner.ID, Role: entity.TeamOwner, Status: entity.ResourceActive, JoinedAt: &joined}
	zero := int64(0)
	limit := entity.ResourceLimit{ScopeKind: "team", ScopeID: team.ID, TokensMonth: &zero, ETag: "lim_tc63_history", PreviousETag: "", ActorID: owner.ID, Reason: "Retained zero", IPMode: "none", IPRangesJSON: "[]", UpdatedAt: stamp}
	warning := entity.TeamMemberQuotaWarningObservation{ID: "mwo_tc63_history", TeamID: team.ID, MemberUserID: owner.ID, ScopeID: teamCreationFixturePairScope(team.ID, owner.ID), UserCreatedAt: stamp, Dimension: "tokens", MonthStart: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), MonthEnd: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), PolicyRevision: "lim_tc63_history", Level: "near", Threshold: 80, ThresholdGeneration: "team-member-monthly-80-90-v1", TimeZone: "UTC", AsOf: stamp, Limit: "10", Settled: "8", CoverageStart: stamp, ResourceCreatedAt: stamp}
	readAt := stamp.Add(time.Hour)
	inbox := entity.TeamMemberQuotaWarningInbox{ID: "mwi_tc63_history", ObservationID: warning.ID, RecipientID: owner.ID, RecipientCreatedAt: stamp, ReadAt: &readAt, CreatedAt: stamp}
	for _, row := range []any{&owner, &team, &membership, &limit, &warning, &inbox} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Compare persisted values rather than local timezone/precision representation.
	if err := db.Take(&owner, "id = ?", owner.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&team, "id = ?", team.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&membership, "id = ?", membership.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&limit, "scope_kind = ? AND scope_id = ?", "team", team.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&warning, "id = ?", warning.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&inbox, "id = ?", inbox.ID).Error; err != nil {
		t.Fatal(err)
	}
	history := func() {
		t.Helper()
		var a entity.User
		var b entity.Team
		var c entity.TeamMembership
		var d entity.ResourceLimit
		var e entity.TeamMemberQuotaWarningObservation
		var f entity.TeamMemberQuotaWarningInbox
		for _, read := range []struct {
			out   any
			where string
			args  []any
		}{{&a, "id = ?", []any{owner.ID}}, {&b, "id = ?", []any{team.ID}}, {&c, "id = ?", []any{membership.ID}}, {&d, "scope_kind = ? AND scope_id = ?", []any{"team", team.ID}}, {&e, "id = ?", []any{warning.ID}}, {&f, "id = ?", []any{inbox.ID}}} {
			if err := db.Where(read.where, read.args...).Take(read.out).Error; err != nil {
				t.Fatal(err)
			}
		}
		if !teamCreationFixtureHistoryEqual(a, owner) || !teamCreationFixtureHistoryEqual(b, team) || !teamCreationFixtureHistoryEqual(c, membership) || !teamCreationFixtureHistoryEqual(d, limit) || !teamCreationFixtureHistoryEqual(e, warning) || !teamCreationFixtureHistoryEqual(f, inbox) {
			t.Fatal("V63 modified predecessor history")
		}
	}
	model := &teamCreationReceiptFixtureV63{}
	if err := db.Migrator().DropTable(model); err != nil {
		t.Fatal(err)
	}
	remove()
	concurrent()
	migrate()
	history()
	assertSchema := func() {
		t.Helper()
		if !db.Migrator().HasTable(model) {
			t.Fatal("receipt absent")
		}
		for _, index := range []string{"uq_team_creation_team", "idx_team_creation_actor"} {
			if !db.Migrator().HasIndex(model, index) {
				t.Fatal("receipt index absent", index)
			}
		}
		for _, constraint := range []string{"ck_team_creation_intent", "ck_team_creation_snapshot"} {
			if !db.Migrator().HasConstraint(model, constraint) {
				t.Fatal("receipt constraint absent", constraint)
			}
		}
		cols, err := db.Migrator().ColumnTypes(model)
		if err != nil {
			t.Fatal(err)
		}
		for _, column := range cols {
			if nullable, known := column.Nullable(); known && nullable {
				t.Fatal("receipt immutable column nullable", column.Name())
			}
		}
	}
	assertSchema()
	// The supported 1000-owner compact snapshot exceeds MySQL TEXT capacity. Use
	// production encoding, Unicode byte counts and persisted time instants.
	snapshot := service.TeamCreationSnapshot{Name: "Large retained receipt", Description: "", OwnerJoinedAt: stamp, DefaultRuleETag: strings.Repeat("a", 64), PolicyETag: "lim_tc63_snapshot", SubmittedFields: []string{}, Policy: limits.Policy{IPMode: "none", IPRanges: []string{}}}
	for i := range 1000 {
		suffix := strings.Repeat("0", 26-len(teamCreationFixtureDecimal(i))) + teamCreationFixtureDecimal(i)
		snapshot.Owners = append(snapshot.Owners, service.TeamCreationOwnerSnapshot{UserID: "usr_" + suffix, MembershipID: "tmm_" + suffix, UserCreatedAtMicro: stamp.UnixMicro()})
	}
	raw, err := json.Marshal(snapshot)
	if err != nil || len(raw) <= 65535 || len(raw) > 131072 {
		t.Fatal("1000-owner production snapshot boundary", err, len(raw))
	}
	original := teamCreationReceiptFixtureV63{CreationID: "63000000-0000-4000-8000-000000000001", ActorID: "usr_deleted_tc63", ActorCreatedAt: stamp, TeamID: "tea_deleted_tc63", TeamCreatedAt: stamp, RequestHash: strings.Repeat("b", 64), ReviewETag: strings.Repeat("c", 64), SnapshotJSON: string(raw), CreatedAt: stamp.Add(time.Hour)}
	if err := db.Create(&original).Error; err != nil {
		t.Fatal("receipt must survive absent live actors/Teams", err)
	}
	if err := db.Select(receiptColumns).Take(&original, "creation_id = ?", original.CreationID).Error; err != nil {
		t.Fatal(err)
	}
	persisted := func() {
		t.Helper()
		var got teamCreationReceiptFixtureV63
		if err := db.Select(receiptColumns).Take(&got, "creation_id = ?", original.CreationID).Error; err != nil || !teamCreationFixtureHistoryEqual(got, original) {
			t.Fatal("immutable receipt changed", err)
		}
	}
	duplicate := original
	duplicate.CreationID = "63000000-0000-4000-8000-000000000002"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("Team receipt uniqueness absent")
	}
	duplicate = original
	duplicate.TeamID = "tea_tc63_duplicate"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("creation intent primary key absent")
	}
	for i, sample := range []struct {
		name   string
		modify func(*teamCreationReceiptFixtureV63)
	}{{"request_hash", func(r *teamCreationReceiptFixtureV63) { r.RequestHash = "short" }}, {"review_etag", func(r *teamCreationReceiptFixtureV63) { r.ReviewETag = "short" }}, {"creation_id", func(r *teamCreationReceiptFixtureV63) { r.CreationID = "short" }}, {"snapshot_bytes", func(r *teamCreationReceiptFixtureV63) { r.SnapshotJSON = strings.Repeat("界", 43691) }}} {
		bad := original
		bad.CreationID = "63000000-0000-4000-8000-0000000000" + teamCreationFixtureDecimal(10+i)
		bad.TeamID = "tea_tc63_bad_" + sample.name
		sample.modify(&bad)
		if err := db.Create(&bad).Error; err == nil {
			t.Fatal("portable receipt constraint accepted", sample.name)
		}
	}
	// Repeated and concurrent startup preserve the existing receipt and every
	// predecessor ledger record. Partial MySQL DDL is repaired without rollback.
	remove()
	concurrent()
	migrate()
	persisted()
	history()
	if !db.Migrator().HasIndex(model, "idx_team_creation_actor") {
		t.Fatal("creation receipt fault-injection index absent before removal")
	}
	if db.Name() == "postgres" {
		// Pinned GORM DropIndex renders invalid CURRENT_SCHEMA().index SQL.
		// This fixed test-only statement reconstructs partial DDL; production
		// creation and repair remain entirely GORM Migrator operations.
		if err := db.Exec("DROP INDEX idx_team_creation_actor").Error; err != nil {
			t.Fatal(err)
		}
	} else if err := db.Migrator().DropIndex(model, "idx_team_creation_actor"); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasIndex(model, "idx_team_creation_actor") {
		t.Fatal("creation receipt fault injection retained index")
	}
	if err := db.Migrator().DropConstraint(model, "ck_team_creation_snapshot"); err != nil {
		t.Fatal(err)
	}
	remove()
	concurrent()
	migrate()
	assertSchema()
	persisted()
	history()
	var receipts []teamCreationReceiptFixtureV63
	if err := db.Select(receiptColumns).Find(&receipts).Error; err != nil || len(receipts) != 1 {
		t.Fatal("invalid inserts/DDL duplicated receipt", err, len(receipts))
	}
	ledger(baseline)
	// Reconstructing V63 deliberately used its frozen schema. Later additive
	// columns are restored through their own immutable version before any current
	// entity or following lifecycle is used; no predecessor ledger is discarded.
	if slices.Contains(baseline, 66) {
		res := db.Table("schema_migrations").Where("version = ?", 66).Delete(&struct{}{})
		if res.Error != nil || res.RowsAffected != 1 {
			t.Fatal("restore additive model schema", res.Error)
		}
		migrate()
		var current entity.TeamCreationReceipt
		if err := db.Session(&gorm.Session{QueryFields: true}).Take(&current, "creation_id = ?", original.CreationID).Error; err != nil || current.ModelSnapshotVersion != 0 || current.ModelCount != 0 || current.ModelDigest != nil {
			t.Fatal("historical empty receipt compatibility", err)
		}
	}
}

func teamCreationFixtureDecimal(value int) string {
	if value == 0 {
		return "0"
	}
	digits := []byte{}
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	slices.Reverse(digits)
	return string(digits)
}
func teamCreationReceiptFixtureEqual(a, b entity.TeamCreationReceipt) bool {
	if !a.ActorCreatedAt.Equal(b.ActorCreatedAt) || !a.TeamCreatedAt.Equal(b.TeamCreatedAt) || !a.CreatedAt.Equal(b.CreatedAt) {
		return false
	}
	a.ActorCreatedAt = b.ActorCreatedAt
	a.TeamCreatedAt = b.TeamCreatedAt
	a.CreatedAt = b.CreatedAt
	return reflect.DeepEqual(a, b)
}

func TestTeamCreationLimitsFixtureReceiptEquality(t *testing.T) {
	stamp := time.Date(2026, 4, 5, 6, 7, 8, 123456000, time.UTC)
	base := entity.TeamCreationReceipt{CreationID: "63000000-0000-4000-8000-000000000001", ActorID: "usr_fixture", ActorCreatedAt: stamp, TeamID: "tea_fixture", TeamCreatedAt: stamp, RequestHash: strings.Repeat("a", 64), ReviewETag: strings.Repeat("b", 64), SnapshotJSON: `{"owners":[]}`, CreatedAt: stamp}
	zone := base
	zone.ActorCreatedAt = stamp.In(time.FixedZone("same instant", 3600))
	zone.TeamCreatedAt = zone.ActorCreatedAt
	zone.CreatedAt = zone.ActorCreatedAt
	if !teamCreationReceiptFixtureEqual(base, zone) {
		t.Fatal("same timestamp instant falsely changed")
	}
	for _, mutate := range []func(*entity.TeamCreationReceipt){func(r *entity.TeamCreationReceipt) { r.CreatedAt = r.CreatedAt.Add(time.Microsecond) }, func(r *entity.TeamCreationReceipt) { r.ActorCreatedAt = r.ActorCreatedAt.Add(time.Millisecond) }, func(r *entity.TeamCreationReceipt) { r.TeamCreatedAt = r.TeamCreatedAt.Add(time.Millisecond) }, func(r *entity.TeamCreationReceipt) { r.RequestHash = strings.Repeat("c", 64) }, func(r *entity.TeamCreationReceipt) { r.SnapshotJSON = `{"owners":[1]}` }, func(r *entity.TeamCreationReceipt) { r.ReviewETag = strings.Repeat("d", 64) }, func(r *entity.TeamCreationReceipt) { r.ActorID = "usr_other" }, func(r *entity.TeamCreationReceipt) { r.TeamID = "tea_other" }} {
		changed := base
		mutate(&changed)
		if teamCreationReceiptFixtureEqual(base, changed) {
			t.Fatal("immutable change accepted")
		}
	}
}

// Persisted driver representations may use different timezone objects; compare
// actual instants while preserving every scalar, nil pointer and array element.
func teamCreationFixtureHistoryEqual(a, b any) bool {
	return teamCreationFixtureValueEqual(reflect.ValueOf(a), reflect.ValueOf(b))
}
func teamCreationFixtureValueEqual(a, b reflect.Value) bool {
	if !a.IsValid() || !b.IsValid() {
		return a.IsValid() == b.IsValid()
	}
	if a.Type() != b.Type() {
		return false
	}
	if a.Type() == reflect.TypeFor[time.Time]() {
		return a.Interface().(time.Time).Equal(b.Interface().(time.Time))
	}
	switch a.Kind() {
	case reflect.Pointer, reflect.Interface:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() == b.IsNil()
		}
		return teamCreationFixtureValueEqual(a.Elem(), b.Elem())
	case reflect.Struct:
		for i := range a.NumField() {
			if !teamCreationFixtureValueEqual(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Array, reflect.Slice:
		if a.Kind() == reflect.Slice && a.IsNil() != b.IsNil() {
			return false
		}
		if a.Len() != b.Len() {
			return false
		}
		for i := range a.Len() {
			if !teamCreationFixtureValueEqual(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a.Interface(), b.Interface())
	}
}
func TestTeamCreationLimitsFixtureHistoryEquality(t *testing.T) {
	stamp := time.Date(2026, 4, 5, 6, 7, 8, 123000000, time.UTC)
	a := entity.TeamMembership{ID: "tmm_retained", TeamID: "tea_retained", UserID: "usr_retained", Role: entity.TeamOwner, Status: entity.ResourceActive, JoinedAt: &stamp}
	zone := stamp.In(time.FixedZone("equivalent", 3600))
	b := a
	b.JoinedAt = &zone
	if !teamCreationFixtureHistoryEqual(a, b) {
		t.Fatal("instant comparison rejects portable timestamp")
	}
	b.UserID = "usr_alias"
	if teamCreationFixtureHistoryEqual(a, b) {
		t.Fatal("different history identity accepted")
	}
	b = a
	b.JoinedAt = nil
	if teamCreationFixtureHistoryEqual(a, b) {
		t.Fatal("nil timestamp history accepted")
	}
	b = a
	later := stamp.Add(time.Millisecond)
	b.JoinedAt = &later
	if teamCreationFixtureHistoryEqual(a, b) {
		t.Fatal("changed relationship instant accepted")
	}
}
func TestTeamCreationLimitsFixtureSnapshotBounds(t *testing.T) {
	stamp := time.Date(2026, 4, 5, 6, 7, 8, 123000000, time.UTC)
	snapshot := service.TeamCreationSnapshot{Name: strings.Repeat("n", 100), Description: strings.Repeat("d", 2000), OwnerJoinedAt: stamp, DefaultRuleETag: strings.Repeat("a", 64), PolicyETag: "lim_01ARZ3NDEKTSV4RRFFQ69G5FAV", SubmittedFields: []string{"tokens_month"}, Reason: strings.Repeat("r", 2000), Policy: limits.Policy{IPMode: "none", IPRanges: []string{}}}
	for i := range 1000 {
		suffix := strings.Repeat("0", 26-len(teamCreationFixtureDecimal(i))) + teamCreationFixtureDecimal(i)
		snapshot.Owners = append(snapshot.Owners, service.TeamCreationOwnerSnapshot{UserID: "usr_" + suffix, MembershipID: "tmm_" + suffix, UserCreatedAtMicro: stamp.UnixMicro()})
	}
	raw, err := json.Marshal(snapshot)
	if err != nil || len(raw) <= 65535 || len(raw) > 131072 {
		t.Fatal("supported receipt cannot fit portable bound", err, len(raw))
	}
	var decoded service.TeamCreationSnapshot
	if err := json.Unmarshal(raw, &decoded); err != nil || len(decoded.Owners) != 1000 || !teamCreationFixtureHistoryEqual(snapshot, decoded) {
		t.Fatal("production compact triples lose facts", err)
	}
	if len(strings.Repeat("界", 43691)) <= 131072 {
		t.Fatal("constraint oracle must measure UTF-8 bytes")
	}
}

func teamCreationFixturePairScope(teamID, userID string) string {
	raw, _ := json.Marshal([2]string{teamID, userID})
	sum := sha256.Sum256(raw)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:])
}

func TestTeamCreationContextRejectsQueryPrivatelyBeforeService(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/teams/creation-context", ctrl.GetTeamCreationContext)
	for _, query := range []string{"owner_id=private", "q=one", "cursor=other", "q=one&q=two", "q=bad%zz"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/teams/creation-context?"+query, nil))
		if response.Code != http.StatusBadRequest || response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("ETag") != "" {
			t.Fatal("entered creation-context denial changed privacy or scope", response.Code, response.Header().Get("Cache-Control"))
		}
	}
}

// Exercise the product registry so a pre-handler Session denial cannot bypass
// the private context response header. No service or database is consulted.
func TestTeamCreationContextRegisteredUnauthenticatedResponsesArePrivate(t *testing.T) {
	router := fox.New()
	New(nil).RegisterRoutes(router)
	for _, suffix := range []string{"", "?user_id=private", "?q=one&q=two", "?q=bad%zz"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/teams/creation-context"+suffix, nil))
		assertAPIErrorResponse(t, response, suffix, ErrorResponse{Code: http.StatusUnauthorized, Message: "unauthorized"})
		if response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("ETag") != "" {
			t.Fatal("registered creation context unauthenticated privacy mismatch", response.Header().Get("Cache-Control"))
		}
	}
}

// Reject review-header failures before authentication lookup or service writes.
func TestTeamCreationRequiredReviewHeaderBeforeService(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.POST("/teams", ctrl.CreateTeam)
	body := `{"creation_id":"63000000-0000-4000-8000-000000000099","name":"Reviewed Team","owner_ids":["usr_fixture"]}`
	for _, sample := range []struct {
		name    string
		values  []string
		status  int
		message string
	}{
		{"missing", nil, http.StatusPreconditionRequired, "If-Match is required"},
		{"empty", []string{""}, http.StatusBadRequest, "bad request"},
		{"bare", []string{strings.Repeat("a", 64)}, http.StatusBadRequest, "bad request"},
		{"weak", []string{`W/"` + strings.Repeat("a", 64) + `"`}, http.StatusBadRequest, "bad request"},
		{"wildcard", []string{"*"}, http.StatusBadRequest, "bad request"},
		{"duplicate", []string{`"` + strings.Repeat("a", 64) + `"`, `"` + strings.Repeat("a", 64) + `"`}, http.StatusBadRequest, "bad request"},
		{"list", []string{`"` + strings.Repeat("a", 64) + `", "` + strings.Repeat("a", 64) + `"`}, http.StatusBadRequest, "bad request"},
		{"uppercase", []string{`"` + strings.Repeat("A", 64) + `"`}, http.StatusBadRequest, "bad request"},
		{"short", []string{`"abc"`}, http.StatusBadRequest, "bad request"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/teams", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			for _, value := range sample.values {
				req.Header.Add("If-Match", value)
			}
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			assertAPIErrorResponse(t, res, sample.name, ErrorResponse{Code: sample.status, Message: sample.message})
		})
	}
}
