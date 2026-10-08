package service

import (
	"context"
	"math"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
)

func teamRollingFixture() (entity.ResourceLimit, entity.Team, *eventqueue.QuotaUsageProofBatch) {
	row, birth, proof := rollingWarningFixture()
	team := entity.Team{ID: "tea_00000000000000000000000001", Name: "Team rolling", Status: entity.ResourceActive, CreatedAt: birth}
	row.ScopeKind = "team"
	row.ScopeID = team.ID
	proof.Usage.SevenDays.TokensUsed = 90
	return row, team, &eventqueue.QuotaUsageProofBatch{Active: true, AsOf: proof.Usage.AsOf, CoverageStart: proof.Usage.CoverageStart, TimeZone: proof.Usage.TimeZone, Accounts: map[string]eventqueue.QuotaUsageAccountProof{limitAccount("team", team.ID): proof}}
}
func TestTeamRollingWarningOwnAccountAndCoveredFacts(t *testing.T) {
	for _, kind := range []string{"valid", "finite_held", "5h_unknown", "7d_unknown", "unregistered", "birth", "inactive_team", "case_id", "missing", "wrong_scope", "incomplete_coverage", "zero", "null", "foreign_account", "reset_review"} {
		t.Run(kind, func(t *testing.T) {
			row, team, frame := teamRollingFixture()
			key := limitAccount("team", team.ID)
			proof := frame.Accounts[key]
			want := 2
			switch kind {
			case "finite_held":
				proof.Usage.FiveHours.TokensHeld = math.MaxInt64
			case "5h_unknown":
				proof.Usage.FiveHours.TokensUnknown = 1
				want = 1
			case "7d_unknown":
				proof.Usage.SevenDays.TokensUnknown = 1
				want = 1
			case "unregistered":
				proof.Registered = false
				want = 0
			case "birth":
				proof.CreatedAt = proof.CreatedAt.Add(time.Microsecond)
				want = 0
			case "inactive_team":
				team.Status = entity.ResourceDisabled
				want = 0
			case "case_id":
				team.ID = "TEA_00000000000000000000000001"
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
			case "foreign_account":
				delete(frame.Accounts, key)
				frame.Accounts[limitAccount("key", team.ID)] = proof
				want = 0
			case "reset_review":
				reset := "lim_reviewed_reset"
				row.DefaultResetETag = &reset
			}
			if kind != "missing" && kind != "foreign_account" {
				frame.Accounts[key] = proof
			}
			samples := teamRollingWarningSamples(row, team, frame)
			if len(samples) != want {
				t.Fatalf("samples%d want%d", len(samples), want)
			}
			if kind == "finite_held" && samples[0].Rolling.Observation.Settled != 80 {
				t.Fatal("hold converted to settled")
			}
			if kind == "zero" || kind == "null" {
				_, notice, err := advanceTeamRollingWarning(entity.TeamRollingQuotaWarningState{}, samples[0])
				if err != nil || notice != nil {
					t.Fatal("no denominator emitted")
				}
			}
		})
	}
}
func TestTeamRollingWarningEpisodesAndUnrelatedPolicyEdits(t *testing.T) {
	row, team, frame := teamRollingFixture()
	sample := teamRollingWarningSamples(row, team, frame)[0]
	var state entity.TeamRollingQuotaWarningState
	step := func(used int64, want string) {
		t.Helper()
		sample.Rolling.Observation.AsOf = sample.Rolling.Observation.AsOf.Add(time.Second)
		sample.Rolling.Observation.WindowEnd = sample.Rolling.Observation.AsOf
		sample.Rolling.Observation.WindowStart = sample.Rolling.Observation.AsOf.Add(-5 * time.Hour)
		sample.Rolling.Observation.Settled = used
		var v *entity.TeamRollingQuotaWarningObservation
		var err error
		state, v, err = advanceTeamRollingWarning(state, sample)
		if err != nil || want == "" && v != nil || want != "" && (v == nil || v.Level != want || v.TeamID != team.ID || v.TeamName != team.Name) {
			t.Fatal("episode transition", err)
		}
	}
	step(79, "")
	step(100, "critical")
	episode := state.EpisodeID
	step(110, "")
	sample.Rolling.Observation.PolicyRevision = "lim_unrelated_edit"
	step(110, "")
	if episode != state.EpisodeID {
		t.Fatal("unrelated edit rearmed")
	}
	step(79, "")
	step(80, "near")
	step(90, "critical")
	if episode == state.EpisodeID {
		t.Fatal("below80 failed rearm")
	}
	sample.Rolling.Observation.Limit = 200
	step(180, "critical")
	old := state
	sample.Rolling.Observation.ResourceCreatedAt = sample.Rolling.Observation.ResourceCreatedAt.Add(time.Microsecond)
	if _, _, err := advanceTeamRollingWarning(state, sample); err == nil {
		t.Fatal("reborn identity admitted")
	}
	if old != state {
		t.Fatal("state changed after rejected birth")
	}
}
func TestTeamRollingWarningHistoricalRecipientAndTeamBirth(t *testing.T) {
	row, team, frame := teamRollingFixture()
	_, v, err := advanceTeamRollingWarning(entity.TeamRollingQuotaWarningState{}, teamRollingWarningSamples(row, team, frame)[0])
	if err != nil || v == nil {
		t.Fatal(err)
	}
	v.ID = "tro_retained"
	birth := team.CreatedAt.Add(-time.Hour)
	access := quotaInboxAccess{ActorID: "usr_original", ActorCreatedAt: birth, TeamIDs: []string{team.ID}}
	before := teamRollingWarningInboxRow{RecipientCreatedAt: birth, CurrentTeamID: team.ID, CurrentTeamCreatedAt: team.CreatedAt, CurrentTeamStatus: entity.ResourceActive, ID: "tri_retained", RecipientID: access.ActorID, ObservationID: v.ID, CreatedAt: v.AsOf, Observation: *v}
	if !validTeamRollingWarningInbox(before, access) {
		t.Fatal("original current member denied")
	}
	record := teamRollingWarningRecord(before)
	if record.TeamRollingQuotaWarning == nil || record.Kind != "team_rolling_quota_warning" || record.SubjectName != team.Name || record.QuotaWarning != nil || record.DeliveryStatus != "" {
		t.Fatal("mixed family")
	}
	for _, kind := range []string{"new_member", "removed_member", "recipient_birth", "team_birth", "team_state", "foreign_team", "case_team", "generation", "coverage", "threshold"} {
		t.Run(kind, func(t *testing.T) {
			r := before
			a := access
			switch kind {
			case "new_member":
				a.ActorID = "usr_later"
			case "removed_member":
				a.TeamIDs = nil
			case "recipient_birth":
				a.ActorCreatedAt = a.ActorCreatedAt.Add(time.Microsecond)
			case "team_birth":
				r.CurrentTeamCreatedAt = r.CurrentTeamCreatedAt.Add(time.Microsecond)
			case "team_state":
				r.CurrentTeamStatus = entity.ResourceDisabled
			case "foreign_team":
				r.CurrentTeamID = "tea_00000000000000000000000002"
			case "case_team":
				r.CurrentTeamID = "TEA_00000000000000000000000001"
			case "generation":
				r.Observation.ThresholdGeneration = "personal-rolling-80-90-v1"
			case "coverage":
				r.Observation.CoverageStart = r.Observation.AsOf
			case "threshold":
				r.Observation.Threshold = 90
			}
			if validTeamRollingWarningInbox(r, a) {
				t.Fatal("foreign or invalid history admitted")
			}
		})
	}
}

func TestTeamRollingWarningLiveAppliedProof(t *testing.T) {
	for _, name := range []string{"valid", "stopped", "nil_runtime", "pointer", "birth", "missing_birth", "lease", "revision", "policy_fence", "team_fence", "cancel"} {
		t.Run(name, func(t *testing.T) {
			s, auth, row, team, policy, calendar := teamWarningRuntimeFixture(t)
			s.runtime.done = make(chan struct{})
			auth.Quota.Created = map[string]time.Time{limitAccount("team", team.ID): team.CreatedAt}
			ctx := context.Background()
			switch name {
			case "stopped":
				close(s.runtime.done)
			case "nil_runtime":
				s.runtime = nil
			case "pointer":
				s.runtime.auth.Store(&runtimeAuthorization{})
			case "birth":
				auth.Quota.Created[limitAccount("team", team.ID)] = team.CreatedAt.Add(time.Microsecond)
			case "missing_birth":
				auth.Quota.Created = nil
			case "lease":
				auth.ValidUntil = time.Now().Add(-time.Second)
			case "revision":
				auth.Quota.Revisions[limitAccount("team", team.ID)] += "x"
			case "policy_fence":
				s.runtime.deniedLimits.Store(limitAccount("team", team.ID), true)
			case "team_fence":
				s.runtime.deniedTeams.Store(team.ID, true)
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if s.teamRollingWarningApplied(ctx, auth, row, team, policy, calendar, "USD") != (name == "valid") {
				t.Fatal("non-current or stopped publisher accepted", name)
			}
		})
	}
}

// Missing final usage is either a retained finite hold or an unbounded unknown
// counter. Use the real journal, then the real sampler and episode transition.
func TestTeamRollingWarningRealJournalHeldAndUnknown(t *testing.T) {
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
			row := entity.ResourceLimit{ScopeKind: "team", ScopeID: "tea_00000000000000000000000001", ETag: "lim_journal", Tokens5H: &cap, Tokens7D: &cap}
			account := limitAccount("team", row.ScopeID)
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
			team := entity.Team{ID: row.ScopeID, Name: "Journal Team", Status: entity.ResourceActive, CreatedAt: birth}
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
			samples := teamRollingWarningSamples(row, team, proofAt(birth.Add(time.Second)))
			if len(samples) != 2 {
				t.Fatal("known window samples")
			}
			states := make([]entity.TeamRollingQuotaWarningState, 2)
			for i, sample := range samples {
				state, notice, err := advanceTeamRollingWarning(states[i], sample)
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
			before := append([]entity.TeamRollingQuotaWarningState(nil), states...)
			samples = teamRollingWarningSamples(row, team, proof)
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
				after, notice, err := advanceTeamRollingWarning(states[i], sample)
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

func TestTeamRollingWarningExplicitResetReviewIsConsumedOnce(t *testing.T) {
	row, team, frame := teamRollingFixture()
	sample := teamRollingWarningSamples(row, team, frame)[0]
	sample.Rolling.Observation.Settled = 90
	state, notice, err := advanceTeamRollingWarning(entity.TeamRollingQuotaWarningState{}, sample)
	if err != nil || notice == nil || notice.Level != "critical" {
		t.Fatal("initial critical", err)
	}
	previous := state.EpisodeID
	reset := "lim_reviewed_team_reset"
	row.DefaultResetETag = &reset
	row.AppliedDefaultETag = &reset
	frame.AsOf = frame.AsOf.Add(time.Second)
	key := limitAccount("team", team.ID)
	proof := frame.Accounts[key]
	proof.Usage.AsOf = frame.AsOf
	proof.Usage.FiveHours.TokensUsed = 90
	frame.Accounts[key] = proof
	samples := teamRollingWarningSamples(row, team, frame)
	if len(samples) != 2 || samples[0].Rolling.ResetReview != reset {
		t.Fatal("exact reset provenance omitted")
	}
	state, notice, err = advanceTeamRollingWarning(state, samples[0])
	if err != nil || notice == nil || state.EpisodeID == previous || state.LastResetReview != reset {
		t.Fatal("reviewed reset did not begin distinct episode", err)
	}
	previous = state.EpisodeID
	sample = samples[0]
	sample.Rolling.Observation.AsOf = sample.Rolling.Observation.AsOf.Add(time.Second)
	sample.Rolling.Observation.WindowEnd = sample.Rolling.Observation.AsOf
	sample.Rolling.Observation.WindowStart = sample.Rolling.Observation.AsOf.Add(-5 * time.Hour)
	state, notice, err = advanceTeamRollingWarning(state, sample)
	if err != nil || notice != nil || state.EpisodeID != previous {
		t.Fatal("same reviewed reset emitted twice", err)
	}
	sample.Rolling.ResetReview = ""
	sample.Rolling.Observation.AsOf = sample.Rolling.Observation.AsOf.Add(time.Second)
	sample.Rolling.Observation.WindowEnd = sample.Rolling.Observation.AsOf
	sample.Rolling.Observation.WindowStart = sample.Rolling.Observation.AsOf.Add(-5 * time.Hour)
	state, notice, err = advanceTeamRollingWarning(state, sample)
	if err != nil || notice != nil || state.LastResetReview != reset || state.EpisodeID != previous {
		t.Fatal("ordinary save erased reset provenance or rearmed", err)
	}
	malformed := "invalid reset"
	row.DefaultResetETag = &malformed
	if len(teamRollingWarningSamples(row, team, frame)) != 0 {
		t.Fatal("unreviewed malformed reset admitted")
	}
}
