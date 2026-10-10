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

func testPersonalKeyRollingQuotaWarningMigration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	models := []any{&entity.PersonalKeyRollingQuotaWarningInbox{}, &entity.PersonalKeyRollingQuotaWarningObservation{}, &entity.PersonalKeyRollingQuotaWarningState{}}
	type ledger struct {
		Version   int
		AppliedAt string
	}
	var original []ledger
	if e := db.Table("schema_migrations").Order("version").Find(&original).Error; e != nil || len(original) != 96 || original[95].Version != 96 || original[94].Version != 95 || original[93].Version != 94 || original[92].Version != 93 || original[91].Version != 92 || original[90].Version != 91 || original[89].Version != 90 || original[88].Version != 89 || original[87].Version != 88 || original[86].Version != 87 || original[85].Version != 86 || original[84].Version != 85 || original[83].Version != 84 || original[82].Version != 83 || original[80].Version != 81 || original[81].Version != 82 {
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
		r := db.Table("schema_migrations").Where("version = ?", 82).Delete(&struct{}{})
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
		for _, name := range []string{"ck_pkrqw_root", "ck_pkrqw_name", "ck_pkrqw_owner", "ck_pkrqw_window", "ck_pkrqw_window_time", "ck_pkrqw_amount", "ck_pkrqw_level", "ck_pkrqw_generation"} {
			if !db.Migrator().HasConstraint(models[1], name) {
				t.Fatal("missing immutable fact constraint", name)
			}
		}
		if !db.Migrator().HasIndex(models[1], "uq_pkrqw_episode_level") || !db.Migrator().HasIndex(models[0], "uq_pkrqw_inbox") || !db.Migrator().HasIndex(models[0], "idx_pkrqw_recipient_created") {
			t.Fatal("dedup/page index missing")
		}
	}
	assertSchema()
	now := time.Now().UTC().Truncate(time.Microsecond)
	birth := now.Add(-time.Hour)
	observation := entity.PersonalKeyRollingQuotaWarningObservation{ID: "kro_retained", RootKeyID: "key_retained", RootKeyName: "Retained root", OwnerCreatedAt: birth.Add(-time.Hour), OwnerID: "usr_retained", ResourceCreatedAt: birth, WindowKind: "5h", EpisodeID: "rwe_retained", PolicyRevision: "lim_retained", WindowStart: now.Add(-5 * time.Hour), WindowEnd: now, AsOf: now, CoverageStart: birth, TimeZone: "UTC", Limit: 100, Settled: 80, Level: "near", Threshold: 80, ThresholdGeneration: "personal-key-rolling-80-90-v1"}
	state := entity.PersonalKeyRollingQuotaWarningState{RootKeyID: observation.RootKeyID, OwnerCreatedAt: observation.OwnerCreatedAt, OwnerID: observation.OwnerID, ResourceCreatedAt: birth, WindowKind: "5h", Cap: 100, EpisodeID: observation.EpisodeID, NearSent: true, LastAsOf: now}
	read := now
	inbox := entity.PersonalKeyRollingQuotaWarningInbox{ID: "kri_retained", ObservationID: observation.ID, RecipientID: observation.OwnerID, RecipientCreatedAt: observation.OwnerCreatedAt, ReadAt: &read, CreatedAt: now}
	for _, v := range []any{&state, &observation, &inbox} {
		if e := db.Create(v).Error; e != nil {
			t.Fatal(e)
		}
	}
	// Critical facts remain legal when sampling skips directly to100+.
	for i, amount := range []int64{100, 101, math.MaxInt64} {
		v := observation
		v.ID = fmt.Sprintf("kro_atcap_%d", i)
		v.EpisodeID = fmt.Sprintf("rwe_atcap_%d", i)
		v.Settled = amount
		v.Level = "critical"
		v.Threshold = 90
		if e := db.Create(&v).Error; e != nil {
			t.Fatal("100+ critical rejected by schema", e)
		}
	}
	for _, kind := range []string{"window_case", "level_case", "threshold", "zero", "negative", "window_end", "birth", "owner_birth", "root", "name", "generation", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			v := observation
			v.ID = "kro_bad_" + kind
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
			case "owner_birth":
				v.OwnerCreatedAt = now.Add(time.Second)
			case "root":
				v.RootKeyID = ""
			case "name":
				v.RootKeyName = ""
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
	var storedObservation entity.PersonalKeyRollingQuotaWarningObservation
	var storedState entity.PersonalKeyRollingQuotaWarningState
	var storedInbox entity.PersonalKeyRollingQuotaWarningInbox
	if e := db.Where("id = ?", observation.ID).Take(&storedObservation).Error; e != nil || !personalKeyRollingRetainedFactsEqual(storedObservation, observation) {
		t.Fatal("inserted observation differs", e)
	}
	if e := db.Where("owner_id = ? AND resource_created_at = ? AND window_kind = ?", state.OwnerID, state.ResourceCreatedAt, state.WindowKind).Take(&storedState).Error; e != nil || !personalKeyRollingRetainedFactsEqual(storedState, state) {
		t.Fatal("inserted episode differs", e)
	}
	if e := db.Where("id = ?", inbox.ID).Take(&storedInbox).Error; e != nil || !personalKeyRollingRetainedFactsEqual(storedInbox, inbox) {
		t.Fatal("inserted read receipt differs", e)
	}
	remove()
	migrate()
	var after []ledger
	if e := db.Table("schema_migrations").Order("version").Find(&after).Error; e != nil || len(after) != 96 || after[95].Version != 96 || after[94].Version != 95 || after[93].Version != 94 || after[92].Version != 93 || after[91].Version != 92 || after[90].Version != 91 || after[89].Version != 90 || after[88].Version != 89 || after[87].Version != 88 || after[86].Version != 87 || after[85].Version != 86 || after[84].Version != 85 || after[83].Version != 84 || after[82].Version != 83 || !reflect.DeepEqual(after[:81], original[:81]) || !reflect.DeepEqual(after[82:], original[82:]) {
		t.Fatal("original80 ledger changed", e)
	}
	for i, row := range after {
		if row.Version != i+1 {
			t.Fatal("noncontiguous complete V87 ledger")
		}
	}
	var saved entity.PersonalKeyRollingQuotaWarningObservation
	if e := db.Where("id = ?", observation.ID).Take(&saved).Error; e != nil || !personalKeyRollingRetainedFactsEqual(saved, storedObservation) {
		t.Fatal("immutable observation rewritten", e)
	}
	var savedState entity.PersonalKeyRollingQuotaWarningState
	if e := db.Where("owner_id = ? AND resource_created_at = ? AND window_kind = ?", state.OwnerID, state.ResourceCreatedAt, state.WindowKind).Take(&savedState).Error; e != nil || !personalKeyRollingRetainedFactsEqual(savedState, storedState) {
		t.Fatal("episode rewritten on repeat", e)
	}
	var savedInbox entity.PersonalKeyRollingQuotaWarningInbox
	if e := db.Where("id = ?", inbox.ID).Take(&savedInbox).Error; e != nil || !personalKeyRollingRetainedFactsEqual(savedInbox, storedInbox) {
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
	if e := db.Where("id = ?", observation.ID).Take(&saved).Error; e != nil || !personalKeyRollingRetainedFactsEqual(saved, storedObservation) {
		t.Fatal("partial DDL lost retained facts", e)
	}
}

func personalKeyRollingWarningRegistryParent(names []string) ([]string, bool) {
	if len(names) == 160 || len(names) == 162 || len(names) == 164 || len(names) == 166 || len(names) == 168 || len(names) == 170 || len(names) == 172 || len(names) == 174 || len(names) == 176 || len(names) == 177 || len(names) == 179 {
		parent, ok := projectRollingWarningRegistryParent(names)
		if !ok {
			return nil, false
		}
		names = parent
	}
	if len(names) != 158 || names[156] != "personal_key_rolling_quota_warning_migration:testPersonalKeyRollingQuotaWarningMigration" || names[157] != "personal_key_rolling_quota_warnings:testPersonalKeyRollingQuotaWarningLifecycle" {
		return nil, false
	}
	digest := sha256.Sum256([]byte(strings.Join(names[:156], "\n")))
	if hex.EncodeToString(digest[:]) != "55242b8d60a395745866cc48af3efa9e3838cfb7777c898bdb6212e019c147e2" {
		return nil, false
	}
	return names[:156], true
}
func personalKeyRollingRetainedFactsEqual(a, b any) bool {
	switch a.(type) {
	case entity.PersonalKeyRollingQuotaWarningObservation, entity.PersonalKeyRollingQuotaWarningState, entity.PersonalKeyRollingQuotaWarningInbox:
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

func TestPersonalKeyRollingWarningExact158RegistryAnd156Prefix(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range regexp.MustCompile(`\{"([^"\n]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1) {
		names = append(names, m[1]+":"+m[2])
	}
	if !ldapRegistry191Current(names) {
		t.Fatal("exact191 LDAP successor changed")
	}
	names = names[:181]
	if !credentialAttemptStatisticsRegistry181Current(names) {
		t.Fatal("exact V93/181 successor changed")
	}
	names = names[:179]
	if len(names) != 179 || !strings.Contains(string(raw), "versions != 96") {
		t.Fatal("current exact172 registry/V89 ledger changed")
	}
	if _, ok := personalKeyRollingWarningRegistryParent(names); !ok {
		t.Fatal("exact retained156 plus new pair required")
	}
	for _, mutate := range []func([]string) []string{func(x []string) []string { return x[:157] }, func(x []string) []string { x[156], x[157] = x[157], x[156]; return x }, func(x []string) []string { x[0] = x[1]; return x }, func(x []string) []string { return append(x, "extra:unreviewed") }} {
		if _, ok := personalKeyRollingWarningRegistryParent(mutate(append([]string(nil), names...))); ok {
			t.Fatal("changed prefix or tail accepted")
		}
	}
}
