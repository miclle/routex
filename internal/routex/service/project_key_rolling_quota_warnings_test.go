package service

import (
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
)

func projectKeyRollingFixture() (entity.ResourceLimit, entity.ProjectKey, entity.Project, *eventqueue.QuotaUsageProofBatch) {
	row, root, owner, frame := projectKeyWarningFixture()
	cap := int64(100)
	row.Tokens5H, row.Tokens7D = &cap, &cap
	proof := frame.Accounts[limitAccount("key", root.ID)]
	proof.Usage.FiveHours.TokensUsed = 80
	proof.Usage.SevenDays.TokensUsed = 90
	frame.Accounts[limitAccount("key", root.ID)] = proof
	return row, root, owner, frame
}
func TestProjectKeyRollingWarningOwnRootAndCoveredFacts(t *testing.T) {
	for _, kind := range []string{"valid", "finite_held", "5h_unknown", "7d_unknown", "unregistered", "root_birth", "owner_birth", "owner", "child", "missing", "wrong_scope", "incomplete_coverage", "zero", "null", "project_account_only", "project_inactive", "unexpected_reset", "unexpected_default"} {
		t.Run(kind, func(t *testing.T) {
			row, root, owner, frame := projectKeyRollingFixture()
			key := limitAccount("key", root.ID)
			proof := frame.Accounts[key]
			want := 2
			switch kind {
			case "project_account_only":
				delete(frame.Accounts, key)
				frame.Accounts[limitAccount("project", owner.ID)] = proof
				want = 0
			case "project_inactive":
				owner.Status = entity.ResourceDisabled
				want = 0
			case "unexpected_reset":
				token := "review_other"
				row.DefaultResetETag = &token
				want = 0
			case "unexpected_default":
				token := "rule_other"
				row.AppliedDefaultETag = &token
				want = 0
			case "finite_held":
				proof.Usage.FiveHours.TokensHeld = math.MaxInt64
				proof.Usage.Active.TokensHeld = math.MaxInt64
			case "5h_unknown":
				proof.Usage.FiveHours.TokensUnknown = 1
				want = 1
			case "7d_unknown":
				proof.Usage.SevenDays.TokensUnknown = 1
				want = 1
			case "unregistered":
				proof.Registered = false
				want = 0
			case "root_birth":
				proof.CreatedAt = proof.CreatedAt.Add(time.Microsecond)
				want = 0
			case "owner_birth":
				owner.CreatedAt = root.CreatedAt.Add(time.Microsecond)
				want = 0
			case "owner":
				root.ProjectID = "usr_other"
				want = 0
			case "child":
				p := "pky_00000000000000000000000002"
				root.ReplacesKeyID = &p
				want = 0
			case "missing":
				delete(frame.Accounts, key)
				want = 0
			case "wrong_scope":
				row.ScopeKind = "user"
				want = 0
			case "incomplete_coverage":
				frame.CoverageStart = frame.AsOf.Add(-time.Minute)
				proof.Usage.CoverageStart = frame.CoverageStart
				want = 0
			case "zero":
				z := int64(0)
				row.Tokens5H = &z
			case "null":
				row.Tokens5H = nil
			}
			if kind != "missing" && kind != "project_account_only" {
				frame.Accounts[key] = proof
			}
			samples := projectKeyRollingWarningSamples(row, root, owner, frame)
			if len(samples) != want {
				t.Fatalf("%s samples=%d want=%d", kind, len(samples), want)
			}
			if kind == "finite_held" && samples[0].Rolling.Observation.Settled != 80 {
				t.Fatal("finite held became settled")
			}
			if kind == "zero" || kind == "null" {
				_, notice, err := advanceProjectKeyRollingWarning(entity.ProjectKeyRollingQuotaWarningState{}, samples[0])
				if err != nil || notice != nil {
					t.Fatal("no denominator emitted warning")
				}
			}
		})
	}
}
func TestProjectKeyRollingWarningEpisodesAndUnchangedEdits(t *testing.T) {
	row, root, owner, frame := projectKeyRollingFixture()
	sample := projectKeyRollingWarningSamples(row, root, owner, frame)[0]
	var state entity.ProjectKeyRollingQuotaWarningState
	step := func(settled int64, expect string) {
		t.Helper()
		sample.Rolling.Observation.AsOf = sample.Rolling.Observation.AsOf.Add(time.Second)
		sample.Rolling.Observation.WindowEnd = sample.Rolling.Observation.AsOf
		sample.Rolling.Observation.WindowStart = sample.Rolling.Observation.AsOf.Add(-5 * time.Hour)
		sample.Rolling.Observation.Settled = settled
		var notice *entity.ProjectKeyRollingQuotaWarningObservation
		var err error
		state, notice, err = advanceProjectKeyRollingWarning(state, sample)
		if err != nil || expect == "" && notice != nil || expect != "" && (notice == nil || notice.Level != expect || notice.RootKeyID != root.ID || notice.ProjectID != owner.ID || notice.RootKeyName != root.Name) {
			t.Fatalf("settled%d expected%s notice%+v err%v", settled, expect, notice, err)
		}
	}
	step(79, "")
	step(80, "near")
	episode := state.EpisodeID
	sample.Rolling.Observation.PolicyRevision = "lim_reason_edit"
	step(80, "")
	step(90, "critical")
	step(100, "")
	if state.EpisodeID != episode {
		t.Fatal("ordinary edit rearms")
	}
	step(79, "")
	step(100, "critical")
	if state.EpisodeID == episode {
		t.Fatal("known below80 did not rearm")
	}
	// Project Key has no default/reset endpoint; unrelated edits do not rearm.
	sample.Rolling.Observation.Limit = 101
	step(100, "critical")
	old := state
	owner.CreatedAt = owner.CreatedAt.Add(time.Microsecond)
	sample.Project = owner
	if _, _, err := advanceProjectKeyRollingWarning(state, sample); err == nil {
		t.Fatal("owner rebirth accepted")
	}
	if state != old {
		t.Fatal("state changed on identity rejection")
	}
}

func TestProjectKeyRollingWarningHistoricalInboxIdentity(t *testing.T) {
	row, root, owner, frame := projectKeyRollingFixture()
	sample := projectKeyRollingWarningSamples(row, root, owner, frame)[0]
	_, v, err := advanceProjectKeyRollingWarning(entity.ProjectKeyRollingQuotaWarningState{}, sample)
	if err != nil || v == nil {
		t.Fatal(err)
	}
	v.ID = "qro_retained"
	access := quotaInboxAccess{ActorID: "usr_manager", ActorCreatedAt: owner.CreatedAt.Add(-time.Hour), ProjectIDs: []string{owner.ID}}
	before := projectKeyRollingWarningInboxRow{RecipientCreatedAt: access.ActorCreatedAt, CurrentProjectID: owner.ID, CurrentProjectCreatedAt: owner.CreatedAt, CurrentProjectStatus: entity.ResourceActive, CurrentRootKeyID: root.ID, CurrentRootCreatedAt: root.CreatedAt, CurrentRootProjectID: owner.ID, ID: "qri_retained", RecipientID: access.ActorID, ObservationID: v.ID, CreatedAt: v.AsOf, Observation: *v}
	if !validProjectKeyRollingWarningInbox(before, access) {
		t.Fatal("valid retained original root denied")
	}
	record := projectKeyRollingWarningRecord(before)
	if record.ProjectKeyRollingQuotaWarning == nil || record.SubjectName != root.Name || record.Kind != "project_key_rolling_quota_warning" || record.RollingQuotaWarning != nil || record.DeliveryStatus != "" {
		t.Fatal("key facts or family mixed")
	}
	for _, name := range []string{"actor", "owner_birth", "root_birth", "root_owner", "child", "owner_reborn", "manager_removed", "project_reborn", "project_archived", "root_name", "generation", "window", "threshold", "coverage"} {
		t.Run(name, func(t *testing.T) {
			r := before
			a := access
			switch name {
			case "actor":
				a.ActorID = "usr_other"
			case "owner_birth":
				a.ActorCreatedAt = a.ActorCreatedAt.Add(time.Microsecond)
			case "root_birth":
				r.CurrentRootCreatedAt = r.CurrentRootCreatedAt.Add(time.Microsecond)
			case "root_owner":
				r.CurrentRootProjectID = "usr_other"
			case "child":
				parent := "pky_other"
				r.CurrentRootParent = &parent
			case "manager_removed":
				a.ProjectIDs = nil
			case "project_reborn":
				r.CurrentProjectCreatedAt = r.CurrentProjectCreatedAt.Add(time.Microsecond)
			case "project_archived":
				r.CurrentProjectStatus = entity.ResourceArchived
			case "owner_reborn":
				r.Observation.ProjectCreatedAt = r.Observation.ProjectCreatedAt.Add(time.Microsecond)
			case "root_name":
				r.Observation.RootKeyName = ""
			case "generation":
				r.Observation.ThresholdGeneration = personalRollingWarningGeneration
			case "window":
				r.Observation.WindowKind = "5H"
			case "threshold":
				r.Observation.Threshold = 90
			case "coverage":
				r.Observation.CoverageStart = r.Observation.AsOf
			}
			if validProjectKeyRollingWarningInbox(r, a) {
				t.Fatal("invalid historical private identity accepted")
			}
		})
	}
}

func TestProjectKeyRollingWarningSampledCriticalKnownBoundsAndUnknownNoRearm(t *testing.T) {
	for _, cap := range []int64{1, 100, limits.MaxInteger} {
		t.Run(fmt.Sprint(cap), func(t *testing.T) {
			row, root, project, frame := projectKeyRollingFixture()
			row.Tokens5H = &cap
			row.Tokens7D = &cap
			account := limitAccount("key", root.ID)
			proof := frame.Accounts[account]
			proof.Usage.FiveHours.TokensUsed = cap
			proof.Usage.SevenDays.TokensUsed = cap
			frame.Accounts[account] = proof
			samples := projectKeyRollingWarningSamples(row, root, project, frame)
			if len(samples) != 2 {
				t.Fatal("accepted cap lacks samples")
			}
			sample := samples[0]
			state, notice, err := advanceProjectKeyRollingWarning(entity.ProjectKeyRollingQuotaWarningState{}, sample)
			if err != nil || notice == nil || notice.Level != "critical" || notice.Threshold != 90 || state.NearSent != true {
				t.Fatal("first atcap must emit critical-only", err)
			}
			repeated, v, err := advanceProjectKeyRollingWarning(state, sample)
			if err != nil || v != nil || repeated.EpisodeID != state.EpisodeID {
				t.Fatal("atcap repeated episode", err)
			}
			proof.Usage.FiveHours.TokensUnknown = 1
			proof.Usage.SevenDays.TokensUnknown = 1
			frame.Accounts[account] = proof
			row.ETag = "lim_unrelated"
			if samples := projectKeyRollingWarningSamples(row, root, project, frame); len(samples) != 0 {
				t.Fatal("unknown current window inferred or rearmed")
			}
			proof.Usage.FiveHours.TokensUnknown = 0
			proof.Usage.SevenDays.TokensUnknown = 0
			frame.Accounts[account] = proof
			sample = projectKeyRollingWarningSamples(row, root, project, frame)[0]
			preserved, v, err := advanceProjectKeyRollingWarning(state, sample)
			if err != nil || v != nil || preserved.EpisodeID != state.EpisodeID || preserved.NearSent != state.NearSent || preserved.CriticalSent != state.CriticalSent {
				t.Fatal("unknown gap or unrelated policy edit rearmed", err)
			}
		})
	}
}

func TestProjectKeyRollingWarningRealJournalHeldAndUnknown(t *testing.T) {
	for _, bounded := range []bool{true, false} {
		name := "historical_unbounded"
		if bounded {
			name = "finite_held"
		}
		t.Run(name, func(t *testing.T) {
			birth := time.Date(2026, 10, 7, 1, 2, 3, 0, time.UTC)
			q, err := eventqueue.Open(filepath.Join(t.TempDir(), "quota.db"), 100, 1024)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := q.Close(); err != nil {
					t.Error(err)
				}
			}()
			if err := q.EnableQuota("UTC", birth); err != nil {
				t.Fatal(err)
			}
			cap, known, held := int64(100), int64(90), int64(2)
			row, root, project, _ := projectKeyRollingFixture()
			root.CreatedAt = birth
			project.CreatedAt = birth.Add(-time.Hour)
			row.Tokens5H, row.Tokens7D = &cap, &cap
			account := limitAccount("key", root.ID)
			policy := eventqueue.QuotaLimit{Limit: eventqueue.Limit{Account: account}, Revision: row.ETag, CreatedAt: birth}
			if bounded {
				policy.Tokens5H, policy.Tokens7D = &cap, &cap
			}
			reserve := func(request string, bound eventqueue.QuotaBound, at time.Time) {
				t.Helper()
				if err := q.ReserveWithQuota(request, []byte("pending"), []eventqueue.QuotaLimit{policy}, bound, at); err != nil {
					t.Fatal(err)
				}
			}
			complete := func(request string, actual eventqueue.QuotaSettlement, at time.Time) {
				t.Helper()
				if _, err := q.CompleteQuota(request, []byte("final"), actual, at); err != nil {
					t.Fatal(err)
				}
			}
			proofAt := func(at time.Time) *eventqueue.QuotaUsageProofBatch {
				t.Helper()
				proof, err := q.AccountQuotaUsageProofBatch([]string{account}, at)
				if err != nil {
					t.Fatal(err)
				}
				return proof
			}
			reserve("known", eventqueue.QuotaBound{Tokens: &known, Revision: "bound_journal"}, birth)
			complete("known", eventqueue.QuotaSettlement{Tokens: &known}, birth)
			samples := projectKeyRollingWarningSamples(row, root, project, proofAt(birth.Add(time.Second)))
			if len(samples) != 2 {
				t.Fatal("known window samples")
			}
			states := make([]entity.ProjectKeyRollingQuotaWarningState, 2)
			for i, sample := range samples {
				state, notice, err := advanceProjectKeyRollingWarning(states[i], sample)
				if err != nil || notice == nil || notice.Threshold != 90 {
					t.Fatal("initial critical")
				}
				states[i] = state
			}
			bound := eventqueue.QuotaBound{}
			if bounded {
				bound = eventqueue.QuotaBound{Tokens: &held, Revision: "bound_journal"}
			}
			reserve("missing_usage", bound, birth.Add(2*time.Second))
			complete("missing_usage", eventqueue.QuotaSettlement{}, birth.Add(2*time.Second))
			proof := proofAt(birth.Add(3 * time.Second))
			for _, window := range []eventqueue.QuotaUsage{proof.Accounts[account].Usage.FiveHours, proof.Accounts[account].Usage.SevenDays} {
				if window.TokensUsed != 90 {
					t.Fatal("settled subtotal changed")
				}
				if bounded && (window.TokensHeld != 2 || window.TokensUnknown != 0) || !bounded && (window.TokensHeld != 0 || window.TokensUnknown != 1) {
					t.Fatal("hold and unknown conflated")
				}
			}
			before := append([]entity.ProjectKeyRollingQuotaWarningState(nil), states...)
			samples = projectKeyRollingWarningSamples(row, root, project, proof)
			if !bounded {
				// Retained unknown history blocks sampling after finite policy review;
				// no replay through a quota gate or fabricated usage is performed.
				if len(samples) != 0 || !reflect.DeepEqual(before, states) {
					t.Fatal("unknown coverage sampled or rearmed")
				}
				return
			}
			if len(samples) != 2 {
				t.Fatal("known settled coverage discarded")
			}
			for i, sample := range samples {
				after, notice, err := advanceProjectKeyRollingWarning(states[i], sample)
				if err != nil || notice != nil || !after.LastAsOf.After(before[i].LastAsOf) {
					t.Fatal("held sample emitted or time regressed")
				}
				want := before[i]
				want.LastAsOf = after.LastAsOf
				if !reflect.DeepEqual(want, after) {
					t.Fatal("held sample changed episode identity or sent state")
				}
			}
		})
	}
}
