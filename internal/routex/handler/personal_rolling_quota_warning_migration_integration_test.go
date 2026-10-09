package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// Both drivers exercise the normal immutable ledger, empty/partial DDL,
// concurrent startup, retained rows and portable constraints/indexes.
func testPersonalRollingQuotaWarningMigration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	models := []any{&entity.PersonalRollingQuotaWarningInbox{}, &entity.PersonalRollingQuotaWarningObservation{}, &entity.PersonalRollingQuotaWarningState{}}
	type ledger struct {
		Version   int
		AppliedAt string
	}
	var original []ledger
	if e := db.Table("schema_migrations").Order("version").Find(&original).Error; e != nil || len(original) != 90 || original[89].Version != 90 || original[88].Version != 89 || original[87].Version != 88 || original[86].Version != 87 || original[85].Version != 86 || original[84].Version != 85 || original[83].Version != 84 || original[82].Version != 83 || original[80].Version != 81 || original[81].Version != 82 {
		t.Fatal("exact87 ledger", e)
	}
	for i, row := range original {
		if row.Version != i+1 {
			t.Fatal("noncontiguous complete V87 ledger")
		}
	}
	migrate := func() {
		t.Helper()
		if e := database.Migrate(ctx, db); e != nil {
			t.Fatal(e)
		}
	}
	remove := func() {
		t.Helper()
		r := db.Table("schema_migrations").Where("version = ?", 81).Delete(&struct{}{})
		if r.Error != nil || r.RowsAffected != 1 {
			t.Fatal("V81 removal", r.Error)
		}
	}
	for _, model := range models {
		if e := db.Migrator().DropTable(model); e != nil {
			t.Fatal(e)
		}
	}
	remove()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { errs <- database.Migrate(ctx, db) })
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	migrate()
	assertSchema := func() {
		t.Helper()
		for _, m := range models {
			if !db.Migrator().HasTable(m) {
				t.Fatal("missing rolling table")
			}
		}
		for _, name := range []string{"ck_prqw_owner", "ck_prqw_window", "ck_prqw_window_time", "ck_prqw_amount", "ck_prqw_level", "ck_prqw_generation"} {
			if !db.Migrator().HasConstraint(models[1], name) {
				t.Fatal("missing immutable fact constraint", name)
			}
		}
		if !db.Migrator().HasIndex(models[1], "uq_prqw_episode_level") || !db.Migrator().HasIndex(models[0], "uq_prqw_inbox") || !db.Migrator().HasIndex(models[0], "idx_prqw_recipient_created") {
			t.Fatal("dedup/page index missing")
		}
	}
	assertSchema()
	now := time.Now().UTC().Truncate(time.Microsecond)
	birth := now.Add(-time.Hour)
	observation := entity.PersonalRollingQuotaWarningObservation{ID: "rwo_retained", OwnerID: "usr_retained", ResourceCreatedAt: birth, WindowKind: "5h", EpisodeID: "rwe_retained", PolicyRevision: "lim_retained", WindowStart: now.Add(-5 * time.Hour), WindowEnd: now, AsOf: now, CoverageStart: birth, TimeZone: "UTC", Limit: 100, Settled: 80, Level: "near", Threshold: 80, ThresholdGeneration: "personal-rolling-80-90-v1"}
	state := entity.PersonalRollingQuotaWarningState{OwnerID: observation.OwnerID, ResourceCreatedAt: birth, WindowKind: "5h", Cap: 100, EpisodeID: observation.EpisodeID, NearSent: true, LastAsOf: now}
	read := now
	inbox := entity.PersonalRollingQuotaWarningInbox{ID: "rwi_retained", ObservationID: observation.ID, RecipientID: observation.OwnerID, ReadAt: &read, CreatedAt: now}
	for _, v := range []any{&state, &observation, &inbox} {
		if e := db.Create(v).Error; e != nil {
			t.Fatal(e)
		}
	}
	// Critical facts remain legal when sampling skips directly to100+.
	for i, amount := range []int64{100, 101, math.MaxInt64} {
		v := observation
		v.ID = fmt.Sprintf("rwo_atcap_%d", i)
		v.EpisodeID = fmt.Sprintf("rwe_atcap_%d", i)
		v.Settled = amount
		v.Level = "critical"
		v.Threshold = 90
		if e := db.Create(&v).Error; e != nil {
			t.Fatal("100+ critical rejected by schema", e)
		}
	}
	for _, kind := range []string{"window_case", "level_case", "threshold", "zero", "negative", "window_end", "birth", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			v := observation
			v.ID = "rwo_bad_" + kind
			v.EpisodeID = "rwe_bad_" + kind
			switch kind {
			case "window_case":
				v.WindowKind = "5H"
			case "level_case":
				v.Level = "NEAR"
			case "threshold":
				v.Threshold = 90
			case "zero":
				v.Limit = 0
			case "negative":
				v.Settled = -1
			case "window_end":
				v.WindowEnd = now.Add(time.Second)
			case "birth":
				v.ResourceCreatedAt = now.Add(time.Second)
			case "duplicate":
				v.EpisodeID = observation.EpisodeID
			}
			if e := db.Create(&v).Error; e == nil {
				t.Fatal("invalid or duplicate rolling fact accepted")
			}
		})
	}

	// Compare actual persisted facts before and after replay. Timestamp locations
	// are driver presentation; exact instants and every other field are immutable.
	var storedObservation entity.PersonalRollingQuotaWarningObservation
	var storedState entity.PersonalRollingQuotaWarningState
	var storedInbox entity.PersonalRollingQuotaWarningInbox
	if e := db.Where("id = ?", observation.ID).Take(&storedObservation).Error; e != nil || !personalRollingRetainedFactsEqual(storedObservation, observation) {
		t.Fatal("inserted observation differs", e)
	}
	if e := db.Where("owner_id = ? AND resource_created_at = ? AND window_kind = ?", state.OwnerID, state.ResourceCreatedAt, state.WindowKind).Take(&storedState).Error; e != nil || !personalRollingRetainedFactsEqual(storedState, state) {
		t.Fatal("inserted episode differs", e)
	}
	if e := db.Where("id = ?", inbox.ID).Take(&storedInbox).Error; e != nil || !personalRollingRetainedFactsEqual(storedInbox, inbox) {
		t.Fatal("inserted read receipt differs", e)
	}
	remove()
	migrate()
	var after []ledger
	if e := db.Table("schema_migrations").Order("version").Find(&after).Error; e != nil || len(after) != 90 || after[89].Version != 90 || after[88].Version != 89 || after[87].Version != 88 || after[86].Version != 87 || after[85].Version != 86 || after[84].Version != 85 || after[83].Version != 84 || after[82].Version != 83 || !reflect.DeepEqual(after[:80], original[:80]) || !reflect.DeepEqual(after[81:], original[81:]) {
		t.Fatal("original80 ledger changed", e)
	}
	for i, row := range after {
		if row.Version != i+1 {
			t.Fatal("noncontiguous complete V87 ledger")
		}
	}
	var saved entity.PersonalRollingQuotaWarningObservation
	if e := db.Where("id = ?", observation.ID).Take(&saved).Error; e != nil || !personalRollingRetainedFactsEqual(saved, storedObservation) {
		t.Fatal("immutable observation rewritten", e)
	}
	var savedState entity.PersonalRollingQuotaWarningState
	if e := db.Where("owner_id = ? AND resource_created_at = ? AND window_kind = ?", state.OwnerID, state.ResourceCreatedAt, state.WindowKind).Take(&savedState).Error; e != nil || !personalRollingRetainedFactsEqual(savedState, storedState) {
		t.Fatal("episode rewritten on repeat", e)
	}
	var savedInbox entity.PersonalRollingQuotaWarningInbox
	if e := db.Where("id = ?", inbox.ID).Take(&savedInbox).Error; e != nil || !personalRollingRetainedFactsEqual(savedInbox, storedInbox) {
		t.Fatal("read receipt rewritten", e)
	}
	// A real interrupted first DDL can leave one complete frozen table while the
	// others are absent. Preserve the existing rows and finish the bounded version.
	for _, m := range []any{models[0], models[2]} {
		if e := db.Migrator().DropTable(m); e != nil {
			t.Fatal(e)
		}
	}
	remove()
	migrate()
	assertSchema()
	if e := db.Where("id = ?", observation.ID).Take(&saved).Error; e != nil || !personalRollingRetainedFactsEqual(saved, storedObservation) {
		t.Fatal("partial DDL lost retained facts", e)
	}
}

func personalRollingWarningRegistryParent(names []string) ([]string, bool) {
	if len(names) == 158 || len(names) == 160 || len(names) == 162 || len(names) == 164 || len(names) == 166 || len(names) == 168 || len(names) == 170 || len(names) == 172 || len(names) == 174 {
		parent, ok := personalKeyRollingWarningRegistryParent(names)
		if !ok {
			return nil, false
		}
		names = parent
	}
	if len(names) != 156 || names[154] != "personal_rolling_quota_warning_migration:testPersonalRollingQuotaWarningMigration" || names[155] != "personal_rolling_quota_warnings:testPersonalRollingQuotaWarningLifecycle" {
		return nil, false
	}
	digest := sha256.Sum256([]byte(strings.Join(names[:154], "\n")))
	if hex.EncodeToString(digest[:]) != "63290538b081dcb074a3f298022743e5b34de84e863cf8aec21916150cbe5db0" {
		return nil, false
	}
	return names[:154], true
}
func TestPersonalRollingWarningExact156RegistryAnd154Prefix(t *testing.T) {
	raw, e := os.ReadFile("auth_integration_test.go")
	if e != nil {
		t.Fatal(e)
	}
	var names []string
	for _, m := range regexp.MustCompile(`\{"([^"\n]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1) {
		names = append(names, m[1]+":"+m[2])
	}
	if len(names) != 174 || !strings.Contains(string(raw), "versions != 90") {
		t.Fatal("current exact172 registry/V89 ledger changed")
	}
	if _, ok := personalRollingWarningRegistryParent(names); !ok {
		t.Fatal("unreviewed current156 registry")
	}
	for _, mutate := range []func([]string) []string{func(x []string) []string { return x[:155] }, func(x []string) []string { x[154], x[155] = x[155], x[154]; return x }, func(x []string) []string { x[0] = x[1]; return x }, func(x []string) []string { return append(x, "extra:unreviewed") }} {
		if _, ok := personalRollingWarningRegistryParent(mutate(append([]string(nil), names...))); ok {
			t.Fatal("changed prefix/tail accepted")
		}
	}
}

// Only the three frozen V81 fact shapes are admitted. Every field is compared;
// time.Equal preserves the exact instant without demanding a driver location.
func personalRollingRetainedFactsEqual(a, b any) bool {
	switch a.(type) {
	case entity.PersonalRollingQuotaWarningObservation, entity.PersonalRollingQuotaWarningState, entity.PersonalRollingQuotaWarningInbox:
	default:
		return false
	}
	x, y := reflect.ValueOf(a), reflect.ValueOf(b)
	if x.Type() != y.Type() {
		return false
	}
	for i := range x.NumField() {
		switch v := x.Field(i).Interface().(type) {
		case time.Time:
			if !v.Equal(y.Field(i).Interface().(time.Time)) {
				return false
			}
		case *time.Time:
			w := y.Field(i).Interface().(*time.Time)
			if (v == nil) != (w == nil) || v != nil && !v.Equal(*w) {
				return false
			}
		default:
			if !reflect.DeepEqual(x.Field(i).Interface(), y.Field(i).Interface()) {
				return false
			}
		}
	}
	return true
}

func TestPersonalRollingWarningRetainedFactsInstantEquality(t *testing.T) {
	instant := time.Date(2026, 10, 7, 1, 2, 3, 456789000, time.UTC)
	loaded := instant.In(time.FixedZone("database-local", 8*60*60))
	if !loaded.Equal(instant) || reflect.DeepEqual(loaded, instant) {
		t.Fatal("instant/location proof")
	}
	facts := []any{
		entity.PersonalRollingQuotaWarningObservation{ID: "rwo_retained", OwnerID: "usr_retained", ResourceCreatedAt: instant, WindowKind: "5h", EpisodeID: "rwe_retained", PolicyRevision: "lim_retained", WindowStart: instant.Add(-5 * time.Hour), WindowEnd: instant, AsOf: instant, CoverageStart: instant.Add(-time.Hour), TimeZone: "UTC", Limit: 100, Settled: 80, Level: "near", Threshold: 80, ThresholdGeneration: "personal-rolling-80-90-v1"},
		entity.PersonalRollingQuotaWarningState{OwnerID: "usr_retained", ResourceCreatedAt: instant, WindowKind: "5h", Cap: 100, LastResetReview: "lim_reset", EpisodeID: "rwe_retained", NearSent: true, LastAsOf: instant},
		entity.PersonalRollingQuotaWarningInbox{ID: "rwi_retained", ObservationID: "rwo_retained", RecipientID: "usr_retained", ReadAt: &instant, CreatedAt: instant},
	}
	for _, original := range facts {
		t.Run(reflect.TypeOf(original).Name(), func(t *testing.T) {
			v := reflect.ValueOf(original)
			persisted := reflect.New(v.Type()).Elem()
			persisted.Set(v)
			for i := range v.NumField() {
				switch f := v.Field(i).Interface().(type) {
				case time.Time:
					persisted.Field(i).Set(reflect.ValueOf(f.In(loaded.Location())))
				case *time.Time:
					local := f.In(loaded.Location())
					persisted.Field(i).Set(reflect.ValueOf(&local))
				}
			}
			if reflect.DeepEqual(original, persisted.Interface()) {
				t.Fatal("old raw comparator did not reproduce RED")
			}
			if !personalRollingRetainedFactsEqual(original, persisted.Interface()) {
				t.Fatal("same persisted instants rejected")
			}
			for i := range v.NumField() {
				t.Run(v.Type().Field(i).Name, func(t *testing.T) {
					bad := reflect.New(v.Type()).Elem()
					bad.Set(persisted)
					switch f := bad.Field(i).Interface().(type) {
					case time.Time:
						bad.Field(i).Set(reflect.ValueOf(f.Add(time.Microsecond)))
					case *time.Time:
						changed := f.Add(time.Microsecond)
						bad.Field(i).Set(reflect.ValueOf(&changed))
					case string:
						bad.Field(i).SetString(f + "changed")
					case bool:
						bad.Field(i).SetBool(!f)
					case int:
						bad.Field(i).SetInt(int64(f) + 1)
					case int64:
						bad.Field(i).SetInt(f + 1)
					default:
						t.Fatal("uncovered immutable field type")
					}
					if personalRollingRetainedFactsEqual(original, bad.Interface()) {
						t.Fatal("changed immutable field accepted")
					}
				})
			}
		})
	}
	inbox := facts[2].(entity.PersonalRollingQuotaWarningInbox)
	unread := inbox
	unread.ReadAt = nil
	if personalRollingRetainedFactsEqual(inbox, unread) || personalRollingRetainedFactsEqual(unread, inbox) || personalRollingRetainedFactsEqual(facts[0], facts[1]) || personalRollingRetainedFactsEqual(instant, instant) {
		t.Fatal("type or pointer presence weakened")
	}
}
