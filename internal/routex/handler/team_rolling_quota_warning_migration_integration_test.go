package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"math"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func testTeamRollingQuotaWarningMigration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	models := []any{&entity.TeamRollingQuotaWarningInbox{}, &entity.TeamRollingQuotaWarningObservation{}, &entity.TeamRollingQuotaWarningState{}}
	type ledger struct {
		Version   int
		AppliedAt string
	}
	var original []ledger
	if e := db.Table("schema_migrations").Order("version").Find(&original).Error; e != nil || len(original) != 91 || original[90].Version != 91 || original[89].Version != 90 || original[88].Version != 89 || original[87].Version != 88 || original[86].Version != 87 || original[85].Version != 86 || original[84].Version != 85 || original[82].Version != 83 || original[83].Version != 84 {
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
		r := db.Table("schema_migrations").Where("version = ?", 84).Delete(&struct{}{})
		if r.Error != nil || r.RowsAffected != 1 {
			t.Fatal("V84 removal", r.Error)
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
		for _, name := range []string{"ck_trqw_team", "ck_trqw_name", "ck_trqw_window", "ck_trqw_window_time", "ck_trqw_amount", "ck_trqw_level", "ck_trqw_generation"} {
			if !db.Migrator().HasConstraint(models[1], name) {
				t.Fatal("missing immutable fact constraint", name)
			}
		}
		if !db.Migrator().HasIndex(models[1], "uq_trqw_episode_level") || !db.Migrator().HasIndex(models[0], "uq_trqw_inbox") || !db.Migrator().HasIndex(models[0], "idx_trqw_recipient_created") {
			t.Fatal("dedup/page index missing")
		}
	}
	assertSchema()
	now := time.Now().UTC().Truncate(time.Microsecond)
	birth := now.Add(-time.Hour)
	observation := entity.TeamRollingQuotaWarningObservation{ID: "tro_retained", TeamID: "tea_retained", TeamName: "Retained root", ResourceCreatedAt: birth, WindowKind: "5h", EpisodeID: "rwe_retained", PolicyRevision: "lim_retained", WindowStart: now.Add(-5 * time.Hour), WindowEnd: now, AsOf: now, CoverageStart: birth, TimeZone: "UTC", Limit: 100, Settled: 80, Level: "near", Threshold: 80, ThresholdGeneration: "team-rolling-80-90-v1"}
	state := entity.TeamRollingQuotaWarningState{TeamID: observation.TeamID, ResourceCreatedAt: birth, WindowKind: "5h", Cap: 100, EpisodeID: observation.EpisodeID, NearSent: true, LastAsOf: now}
	read := now
	inbox := entity.TeamRollingQuotaWarningInbox{ID: "tri_retained", ObservationID: observation.ID, RecipientID: "usr_retained", RecipientCreatedAt: birth.Add(-time.Hour), ReadAt: &read, CreatedAt: now}
	for _, v := range []any{&state, &observation, &inbox} {
		if e := db.Create(v).Error; e != nil {
			t.Fatal(e)
		}
	}
	// Critical facts remain legal when sampling skips directly to100+.
	for i, amount := range []int64{100, 101, math.MaxInt64} {
		v := observation
		v.ID = fmt.Sprintf("tro_atcap_%d", i)
		v.EpisodeID = fmt.Sprintf("rwe_atcap_%d", i)
		v.Settled = amount
		v.Level = "critical"
		v.Threshold = 90
		if e := db.Create(&v).Error; e != nil {
			t.Fatal("100+ critical rejected by schema", e)
		}
	}
	for _, kind := range []string{"window_case", "level_case", "threshold", "zero", "negative", "window_end", "birth", "root", "name", "generation", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			v := observation
			v.ID = "tro_bad_" + kind
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
			case "root":
				v.TeamID = ""
			case "name":
				v.TeamName = ""
			case "generation":
				v.ThresholdGeneration = "personal-rolling-80-90-v1"
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
	var storedObservation entity.TeamRollingQuotaWarningObservation
	var storedState entity.TeamRollingQuotaWarningState
	var storedInbox entity.TeamRollingQuotaWarningInbox
	if e := db.Where("id = ?", observation.ID).Take(&storedObservation).Error; e != nil || !teamRollingRetainedFactsEqual(storedObservation, observation) {
		t.Fatal("inserted observation differs", e)
	}
	if e := db.Where("team_id = ? AND resource_created_at = ? AND window_kind = ?", state.TeamID, state.ResourceCreatedAt, state.WindowKind).Take(&storedState).Error; e != nil || !teamRollingRetainedFactsEqual(storedState, state) {
		t.Fatal("inserted episode differs", e)
	}
	if e := db.Where("id = ?", inbox.ID).Take(&storedInbox).Error; e != nil || !teamRollingRetainedFactsEqual(storedInbox, inbox) {
		t.Fatal("inserted read receipt differs", e)
	}
	remove()
	migrate()
	var after []ledger
	if e := db.Table("schema_migrations").Order("version").Find(&after).Error; e != nil || len(after) != 91 || after[90].Version != 91 || after[89].Version != 90 || after[88].Version != 89 || after[87].Version != 88 || after[86].Version != 87 || after[85].Version != 86 || after[84].Version != 85 || !reflect.DeepEqual(after[:83], original[:83]) || !reflect.DeepEqual(after[84:], original[84:]) {
		t.Fatal("original83 ledger changed", e)
	}
	for i, row := range after {
		if row.Version != i+1 {
			t.Fatal("noncontiguous complete V87 ledger")
		}
	}
	var saved entity.TeamRollingQuotaWarningObservation
	if e := db.Where("id = ?", observation.ID).Take(&saved).Error; e != nil || !teamRollingRetainedFactsEqual(saved, storedObservation) {
		t.Fatal("immutable observation rewritten", e)
	}
	var savedState entity.TeamRollingQuotaWarningState
	if e := db.Where("team_id = ? AND resource_created_at = ? AND window_kind = ?", state.TeamID, state.ResourceCreatedAt, state.WindowKind).Take(&savedState).Error; e != nil || !teamRollingRetainedFactsEqual(savedState, storedState) {
		t.Fatal("episode rewritten on repeat", e)
	}
	var savedInbox entity.TeamRollingQuotaWarningInbox
	if e := db.Where("id = ?", inbox.ID).Take(&savedInbox).Error; e != nil || !teamRollingRetainedFactsEqual(savedInbox, storedInbox) {
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
	if e := db.Where("id = ?", observation.ID).Take(&saved).Error; e != nil || !teamRollingRetainedFactsEqual(saved, storedObservation) {
		t.Fatal("partial DDL lost retained facts", e)
	}
}

func teamRollingWarningRegistryParent(names []string) ([]string, bool) {
	if len(names) == 164 || len(names) == 166 || len(names) == 168 || len(names) == 170 || len(names) == 172 || len(names) == 174 || len(names) == 176 {
		parent, ok := projectKeyRollingWarningRegistryParent(names)
		if !ok {
			return nil, false
		}
		names = parent
	}

	if len(names) != 162 || names[160] != "team_rolling_quota_warning_migration:testTeamRollingQuotaWarningMigration" || names[161] != "team_rolling_quota_warnings:testTeamRollingQuotaWarningLifecycle" {
		return nil, false
	}
	digest := sha256.Sum256([]byte(strings.Join(names[:160], "\n")))
	if hex.EncodeToString(digest[:]) != "69887063f7d2754164b9a07d1ecfc7565a7db756e414f239882aa653859a4a9f" {
		return nil, false
	}
	return names[:160], true
}
func teamRollingRetainedFactsEqual(a, b any) bool {
	switch a.(type) {
	case entity.TeamRollingQuotaWarningObservation, entity.TeamRollingQuotaWarningState, entity.TeamRollingQuotaWarningInbox:
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

func TestTeamRollingWarningExact162RegistryAnd160Prefix(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range regexp.MustCompile(`\{"([^"\n]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1) {
		names = append(names, m[1]+":"+m[2])
	}
	if len(names) != 176 || !strings.Contains(string(raw), "versions != 91") {
		t.Fatal("current exact172 registry/V89 ledger changed")
	}
	if _, ok := teamRollingWarningRegistryParent(names); !ok {
		t.Fatal("exact retained160 plus new pair required")
	}
	for _, mutate := range []func([]string) []string{func(x []string) []string { return x[:161] }, func(x []string) []string { x[160], x[161] = x[161], x[160]; return x }, func(x []string) []string { x[0] = x[1]; return x }, func(x []string) []string { return append(x, "extra:unreviewed") }} {
		if _, ok := teamRollingWarningRegistryParent(mutate(append([]string(nil), names...))); ok {
			t.Fatal("changed prefix or tail accepted")
		}
	}
}
