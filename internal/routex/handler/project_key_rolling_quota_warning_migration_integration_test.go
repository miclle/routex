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

func testProjectKeyRollingQuotaWarningMigration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	models := []any{&entity.ProjectKeyRollingQuotaWarningInbox{}, &entity.ProjectKeyRollingQuotaWarningObservation{}, &entity.ProjectKeyRollingQuotaWarningState{}}
	type ledger struct {
		Version   int
		AppliedAt string
	}
	var original []ledger
	if e := db.Table("schema_migrations").Order("version").Find(&original).Error; e != nil || len(original) != 97 || original[96].Version != 97 || original[95].Version != 96 || original[94].Version != 95 || original[93].Version != 94 || original[92].Version != 93 || original[91].Version != 92 || original[90].Version != 91 || original[89].Version != 90 || original[88].Version != 89 || original[87].Version != 88 || original[86].Version != 87 || original[85].Version != 86 || original[84].Version != 85 || original[83].Version != 84 {
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
		r := db.Table("schema_migrations").Where("version = ?", 85).Delete(&struct{}{})
		if r.Error != nil || r.RowsAffected != 1 {
			t.Fatal("V85 removal", r.Error)
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
		for _, name := range []string{"ck_jkrqw_root", "ck_jkrqw_name", "ck_jkrqw_owner", "ck_jkrqw_window", "ck_jkrqw_window_time", "ck_jkrqw_amount", "ck_jkrqw_level", "ck_jkrqw_generation"} {
			if !db.Migrator().HasConstraint(models[1], name) {
				t.Fatal("missing immutable fact constraint", name)
			}
		}
		if !db.Migrator().HasIndex(models[1], "uq_jkrqw_episode_level") || !db.Migrator().HasIndex(models[0], "uq_jkrqw_inbox") || !db.Migrator().HasIndex(models[0], "idx_jkrqw_recipient_created") {
			t.Fatal("dedup/page index missing")
		}
	}
	assertSchema()
	now := time.Now().UTC().Truncate(time.Microsecond)
	birth := now.Add(-time.Hour)
	observation := entity.ProjectKeyRollingQuotaWarningObservation{ID: "qro_retained", RootKeyID: "pky_retained", RootKeyName: "Retained root", ProjectCreatedAt: birth.Add(-time.Hour), ProjectID: "prj_retained", ResourceCreatedAt: birth, WindowKind: "5h", EpisodeID: "rwe_retained", PolicyRevision: "lim_retained", WindowStart: now.Add(-5 * time.Hour), WindowEnd: now, AsOf: now, CoverageStart: birth, TimeZone: "UTC", Limit: 100, Settled: 80, Level: "near", Threshold: 80, ThresholdGeneration: "project-key-rolling-80-90-v1"}
	state := entity.ProjectKeyRollingQuotaWarningState{RootKeyID: observation.RootKeyID, ProjectCreatedAt: observation.ProjectCreatedAt, ProjectID: observation.ProjectID, ResourceCreatedAt: birth, WindowKind: "5h", Cap: 100, EpisodeID: observation.EpisodeID, NearSent: true, LastAsOf: now}
	read := now
	inbox := entity.ProjectKeyRollingQuotaWarningInbox{ID: "qri_retained", ObservationID: observation.ID, RecipientID: observation.ProjectID, RecipientCreatedAt: observation.ProjectCreatedAt, ReadAt: &read, CreatedAt: now}
	for _, v := range []any{&state, &observation, &inbox} {
		if e := db.Create(v).Error; e != nil {
			t.Fatal(e)
		}
	}
	// Critical facts remain legal when sampling skips directly to100+.
	for i, amount := range []int64{100, 101, math.MaxInt64} {
		v := observation
		v.ID = fmt.Sprintf("qro_atcap_%d", i)
		v.EpisodeID = fmt.Sprintf("rwe_atcap_%d", i)
		v.Settled = amount
		v.Level = "critical"
		v.Threshold = 90
		if e := db.Create(&v).Error; e != nil {
			t.Fatal("100+ critical rejected by schema", e)
		}
	}
	for _, kind := range []string{"window_case", "level_case", "threshold", "zero", "negative", "window_end", "birth", "project_birth", "root", "name", "generation", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			v := observation
			v.ID = "qro_bad_" + kind
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
			case "project_birth":
				v.ProjectCreatedAt = now.Add(time.Second)
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
	var storedObservation entity.ProjectKeyRollingQuotaWarningObservation
	var storedState entity.ProjectKeyRollingQuotaWarningState
	var storedInbox entity.ProjectKeyRollingQuotaWarningInbox
	if e := db.Where("id = ?", observation.ID).Take(&storedObservation).Error; e != nil || !projectKeyRollingRetainedFactsEqual(storedObservation, observation) {
		t.Fatal("inserted observation differs", e)
	}
	if e := db.Where("project_id = ? AND resource_created_at = ? AND window_kind = ?", state.ProjectID, state.ResourceCreatedAt, state.WindowKind).Take(&storedState).Error; e != nil || !projectKeyRollingRetainedFactsEqual(storedState, state) {
		t.Fatal("inserted episode differs", e)
	}
	if e := db.Where("id = ?", inbox.ID).Take(&storedInbox).Error; e != nil || !projectKeyRollingRetainedFactsEqual(storedInbox, inbox) {
		t.Fatal("inserted read receipt differs", e)
	}
	remove()
	migrate()
	var after []ledger
	if e := db.Table("schema_migrations").Order("version").Find(&after).Error; e != nil || len(after) != 97 || after[96].Version != 97 || after[95].Version != 96 || after[94].Version != 95 || after[93].Version != 94 || after[92].Version != 93 || after[91].Version != 92 || after[90].Version != 91 || after[89].Version != 90 || after[88].Version != 89 || after[87].Version != 88 || after[86].Version != 87 || after[85].Version != 86 || after[84].Version != 85 || !reflect.DeepEqual(after[:84], original[:84]) || !reflect.DeepEqual(after[85:], original[85:]) {
		t.Fatal("original80 ledger changed", e)
	}
	for i, row := range after {
		if row.Version != i+1 {
			t.Fatal("noncontiguous complete V87 ledger")
		}
	}
	var saved entity.ProjectKeyRollingQuotaWarningObservation
	if e := db.Where("id = ?", observation.ID).Take(&saved).Error; e != nil || !projectKeyRollingRetainedFactsEqual(saved, storedObservation) {
		t.Fatal("immutable observation rewritten", e)
	}
	var savedState entity.ProjectKeyRollingQuotaWarningState
	if e := db.Where("project_id = ? AND resource_created_at = ? AND window_kind = ?", state.ProjectID, state.ResourceCreatedAt, state.WindowKind).Take(&savedState).Error; e != nil || !projectKeyRollingRetainedFactsEqual(savedState, storedState) {
		t.Fatal("episode rewritten on repeat", e)
	}
	var savedInbox entity.ProjectKeyRollingQuotaWarningInbox
	if e := db.Where("id = ?", inbox.ID).Take(&savedInbox).Error; e != nil || !projectKeyRollingRetainedFactsEqual(savedInbox, storedInbox) {
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
	if e := db.Where("id = ?", observation.ID).Take(&saved).Error; e != nil || !projectKeyRollingRetainedFactsEqual(saved, storedObservation) {
		t.Fatal("partial DDL lost retained facts", e)
	}
}

func projectKeyRollingRetainedFactsEqual(a, b any) bool {
	switch a.(type) {
	case entity.ProjectKeyRollingQuotaWarningObservation, entity.ProjectKeyRollingQuotaWarningState, entity.ProjectKeyRollingQuotaWarningInbox:
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

// The current successor is exact; the released 176-case prefix stays unchanged.
func connectionDiagnosticRegistry177Current(names []string) bool {
	if len(names) != 177 || names[176] != "connection_diagnostic:testConnectionDiagnosticLifecycle" {
		return false
	}
	digest := sha256.Sum256([]byte(strings.Join(names[:176], "\n")))
	return hex.EncodeToString(digest[:]) == "3423c0e62eae421d79195d122d0b5d2f707d3437bf89302a8b55937723c7418a"
}

func connectionTransportRegistry179Current(names []string) bool {
	return len(names) == 179 && names[177] == "connection_transport_migration:testConnectionTransportMigration" && names[178] == "connection_transport:testConnectionTransportLifecycle" && connectionDiagnosticRegistry177Current(names[:177])
}

func projectKeyRollingWarningRegistryParent(names []string) ([]string, bool) {
	if len(names) == 179 {
		if !connectionTransportRegistry179Current(names) {
			return nil, false
		}
		names = names[:177]
	}
	if len(names) == 177 {
		if !connectionDiagnosticRegistry177Current(names) {
			return nil, false
		}
		names = names[:176]
	}
	if len(names) == 176 {
		if names[174] != "provider_enablement_migration:testProviderEnablementMigration" || names[175] != "provider_status:testProviderStatusLifecycle" {
			return nil, false
		}
		names = names[:174]
	}
	if len(names) == 174 {
		if names[172] != "model_weight_history_migration:testModelWeightHistoryMigration" || names[173] != "model_weight_history:testModelWeightHistoryLifecycle" {
			return nil, false
		}
		names = names[:172]
	}
	if len(names) == 172 {
		if names[170] != "credential_source_closed_migration:testCredentialSourceClosedMigration" || names[171] != "credential_source_closed:testCredentialSourceClosedLifecycle" {
			return nil, false
		}
		names = names[:170]
	}
	if len(names) == 170 {
		if names[168] != "credential_source_drain_migration:testCredentialSourceDrainMigration" || names[169] != "credential_published_cleanup:testCredentialPublishedCleanupLifecycle" {
			return nil, false
		}
		names = names[:168]
	}
	// Each reviewed successor peels only its exact pair; historical prefix checks remain below.
	if len(names) == 168 {
		if names[166] != "runtime_application_migration:testRuntimeApplicationMigration" || names[167] != "runtime_applications:testRuntimeApplicationLifecycle" {
			return nil, false
		}
		names = names[:166]
	}
	if len(names) == 166 {
		if names[164] != "model_recorded_metadata_migration:testModelRecordedMetadataMigration" || names[165] != "model_recorded_metadata:testModelRecordedMetadataLifecycle" {
			return nil, false
		}
		names = names[:164]
	}
	if len(names) != 164 || names[162] != "project_key_rolling_quota_warning_migration:testProjectKeyRollingQuotaWarningMigration" || names[163] != "project_key_rolling_quota_warnings:testProjectKeyRollingQuotaWarningLifecycle" {
		return nil, false
	}
	digest := sha256.Sum256([]byte(strings.Join(names[:162], "\n")))
	if hex.EncodeToString(digest[:]) != "6842f30b7f5950f294e8f07e9da73b03b9f77f60b20b6b44816b7dff65f8c004" {
		return nil, false
	}
	return names[:162], true
}

func TestConnectionDiagnosticRegistry177Preserves176AndRejectsUnreviewedSuffix(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, match := range regexp.MustCompile(`\{"([^"\n]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1) {
		names = append(names, match[1]+":"+match[2])
	}
	if !samlRegistry194Current(names) {
		t.Fatal("exact194 SAML successor changed")
	}
	names = names[:181]
	if !credentialAttemptStatisticsRegistry181Current(names) {
		t.Fatal("exact V93/181 successor changed")
	}
	names = names[:179]
	if !connectionTransportRegistry179Current(names) {
		t.Fatal("current registry must be exact179")
	}
	names = names[:177]
	if !connectionDiagnosticRegistry177Current(names) || !strings.Contains(string(raw), "versions != 97") {
		t.Fatal("historical registry must be exact177 beneath current migration92")
	}
	currentParent, currentOK := projectKeyRollingWarningRegistryParent(names)
	historicalParent, historicalOK := projectKeyRollingWarningRegistryParent(names[:176])
	if !currentOK || !historicalOK || strings.Join(currentParent, "\n") != strings.Join(historicalParent, "\n") {
		t.Fatal("reviewed current successor changed the historical parent")
	}
	if connectionDiagnosticRegistry177Current(names[:176]) {
		t.Fatal("historical176 prefix fabricated current177 acceptance")
	}
	for index := range names {
		changed := append([]string(nil), names...)
		changed[index] = "unreviewed:replacement"
		if connectionDiagnosticRegistry177Current(changed) {
			t.Fatal("changed current or historical identity accepted", index)
		}
		if _, ok := projectKeyRollingWarningRegistryParent(changed); ok {
			t.Fatal("changed successor reached historical parent", index)
		}
	}
	for _, mutate := range []func([]string) []string{
		func(x []string) []string { return append(x, "extra:unreviewed") },
		func(x []string) []string { return append(x, x[176]) },
		func(x []string) []string { x[175], x[176] = x[176], x[175]; return x },
		func(x []string) []string { x[176] = x[175]; return x },
		func(x []string) []string { x[176] = "connection_diagnostic:unreviewed"; return x },
		func(x []string) []string { x[176] = "unreviewed:testConnectionDiagnosticLifecycle"; return x },
	} {
		changed := mutate(append([]string(nil), names...))
		if connectionDiagnosticRegistry177Current(changed) {
			t.Fatal("extra, duplicate, reordered or malformed current suffix accepted")
		}
		if _, ok := projectKeyRollingWarningRegistryParent(changed); ok {
			t.Fatal("unreviewed suffix reached historical parent")
		}
	}
}
func TestProjectKeyRollingWarningExact164RegistryAnd162Prefix(t *testing.T) {
	raw, e := os.ReadFile("auth_integration_test.go")
	if e != nil {
		t.Fatal(e)
	}
	var names []string
	for _, m := range regexp.MustCompile(`\{"([^"\n]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1) {
		names = append(names, m[1]+":"+m[2])
	}
	if !samlRegistry194Current(names) {
		t.Fatal("exact194 SAML successor changed")
	}
	names = names[:181]
	if !credentialAttemptStatisticsRegistry181Current(names) {
		t.Fatal("exact V93/181 successor changed")
	}
	names = names[:179]
	if len(names) != 179 || !strings.Contains(string(raw), "versions != 97") {
		t.Fatal("current exact172 registry/V89 ledger changed")
	}
	if _, ok := projectKeyRollingWarningRegistryParent(names); !ok {
		t.Fatal("exact162 prefix plus own pair required")
	}
	for _, mutate := range []func([]string) []string{func(x []string) []string { return x[:163] }, func(x []string) []string { x[162], x[163] = x[163], x[162]; return x }, func(x []string) []string { x[0] = x[1]; return x }, func(x []string) []string { return append(x, "extra:unreviewed") }} {
		if _, ok := projectKeyRollingWarningRegistryParent(mutate(append([]string(nil), names...))); ok {
			t.Fatal("changed prefix/tail accepted")
		}
	}
}

// Full current guards remain exact while reviewed historical prefixes retain their original identities.
func TestReviewed168RegistryPreservesHistoricalPrefixesAndRejectsSuffixDrift(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, match := range regexp.MustCompile(`\{"([^"\n]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1) {
		names = append(names, match[1]+":"+match[2])
	}
	if !samlRegistry194Current(names) {
		t.Fatal("exact194 SAML successor changed")
	}
	names = names[:181]
	if !credentialAttemptStatisticsRegistry181Current(names) {
		t.Fatal("exact V93/181 successor changed")
	}
	names = names[:179]
	if len(names) != 179 || !strings.Contains(string(raw), "versions != 97") {
		t.Fatal("current registry/ledger must be exactly172/V89")
	}
	guards := []struct {
		name  string
		valid func([]string) bool
	}{
		{"personal_key_monthly", personalKeyBehaviorRegistryMatches},
		{"project_key_monthly", projectKeyMonthlyBehaviorRegistryMatches},
		{"team_member_monthly", teamMemberMonthlyRegistryTail},
		{"connection", connectionEnablementRegistryPrefix},
		{"vault", func(x []string) bool { _, ok := vaultAppRoleRegistryParent(x); return ok }},
		{"azure", func(x []string) bool { _, ok := azureDeploymentRegistryParent(x); return ok }},
		{"cleanup", func(x []string) bool { _, ok := providerCleanupRegistryParent(x); return ok }},
		{"personal_rolling", func(x []string) bool { _, ok := personalRollingWarningRegistryParent(x); return ok }},
		{"personal_key_rolling", func(x []string) bool { _, ok := personalKeyRollingWarningRegistryParent(x); return ok }},
		{"project_rolling", func(x []string) bool { _, ok := projectRollingWarningRegistryParent(x); return ok }},
		{"team_rolling", func(x []string) bool { _, ok := teamRollingWarningRegistryParent(x); return ok }},
		{"project_key_rolling", func(x []string) bool { _, ok := projectKeyRollingWarningRegistryParent(x); return ok }},
	}
	for _, guard := range guards {
		t.Run(guard.name, func(t *testing.T) {
			if !guard.valid(names) {
				t.Fatal("complete current registry rejected")
			}
			for _, prefix := range []int{164, 166, 168} {
				if !guard.valid(names[:prefix]) {
					t.Fatal("exact reviewed historical prefix rejected", prefix)
				}
			}
			for index := range names {
				changed := append([]string(nil), names...)
				changed[index] = "unreviewed:replacement"
				if guard.valid(changed) {
					t.Fatal("changed historical or appended identity accepted", index)
				}
			}
			for _, mutate := range []func([]string) []string{
				func(x []string) []string { return x[:167] },
				func(x []string) []string { x[164], x[165] = x[165], x[164]; return x },
				func(x []string) []string { x[166], x[167] = x[167], x[166]; return x },
				func(x []string) []string { x[167] = x[166]; return x },
				func(x []string) []string { return x[:169] },
				func(x []string) []string { x[168], x[169] = x[169], x[168]; return x },
				func(x []string) []string { x[169] = x[168]; return x },
				func(x []string) []string { return x[:175] },
				func(x []string) []string { x[174], x[175] = x[175], x[174]; return x },
				func(x []string) []string { x[174] = "provider_enablement_migration:unreviewed"; return x },
				func(x []string) []string { x[175] = "provider_status:unreviewed"; return x },
				func(x []string) []string { return append(x, "unreviewed:extra") },
			} {
				if guard.valid(mutate(append([]string(nil), names...))) {
					t.Fatal("missing/reordered/duplicate/extra suffix accepted")
				}
			}
		})
	}
}

func TestConnectionTransportRegistry179Preserves177AndRejectsUnreviewedSuffix(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, match := range regexp.MustCompile(`\{"([^"\n]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1) {
		names = append(names, match[1]+":"+match[2])
	}
	if !samlRegistry194Current(names) {
		t.Fatal("exact194 SAML successor changed")
	}
	names = names[:181]
	if !credentialAttemptStatisticsRegistry181Current(names) {
		t.Fatal("exact V93/181 successor changed")
	}
	names = names[:179]
	if !connectionTransportRegistry179Current(names) || !strings.Contains(string(raw), "versions != 97") {
		t.Fatal("current registry must be exact179 with migration92")
	}
	currentParent, currentOK := projectKeyRollingWarningRegistryParent(names)
	historicalParent, historicalOK := projectKeyRollingWarningRegistryParent(names[:177])
	if !currentOK || !historicalOK || strings.Join(currentParent, "\n") != strings.Join(historicalParent, "\n") {
		t.Fatal("reviewed current successor changed the historical parent")
	}
	if connectionTransportRegistry179Current(names[:177]) || !connectionDiagnosticRegistry177Current(names[:177]) {
		t.Fatal("historical177 must remain valid without proving current179")
	}
	for index := range names {
		changed := append([]string(nil), names...)
		changed[index] = "unreviewed:replacement"
		if connectionTransportRegistry179Current(changed) {
			t.Fatal("changed current or historical identity accepted", index)
		}
		if _, ok := projectKeyRollingWarningRegistryParent(changed); ok {
			t.Fatal("changed successor reached historical parent", index)
		}
	}
	for _, mutate := range []func([]string) []string{
		func(x []string) []string { return x[:178] },
		func(x []string) []string { return append(x, "extra:unreviewed") },
		func(x []string) []string { return append(x, x[178]) },
		func(x []string) []string { x[177], x[178] = x[178], x[177]; return x },
		func(x []string) []string { x[178] = x[177]; return x },
		func(x []string) []string { x[177] = "connection_transport_migration:unreviewed"; return x },
		func(x []string) []string { x[178] = "connection_transport:unreviewed"; return x },
		func(x []string) []string { x[177] = "unreviewed:testConnectionTransportMigration"; return x },
		func(x []string) []string { x[178] = "unreviewed:testConnectionTransportLifecycle"; return x },
	} {
		changed := mutate(append([]string(nil), names...))
		if connectionTransportRegistry179Current(changed) {
			t.Fatal("extra, duplicate, reordered or malformed current suffix accepted")
		}
		if _, ok := projectKeyRollingWarningRegistryParent(changed); ok {
			t.Fatal("unreviewed suffix reached historical parent")
		}
	}
}
