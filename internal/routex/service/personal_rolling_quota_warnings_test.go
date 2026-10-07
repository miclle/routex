package service

import (
	"context"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
)

func rollingWarningFixture() (entity.ResourceLimit, time.Time, eventqueue.QuotaUsageAccountProof) {
	row, created, u := warningFixture()
	cap := int64(100)
	row.Tokens5H = &cap
	row.Tokens7D = &cap
	u.FiveHours.TokensUsed = 80
	u.SevenDays.TokensUsed = 90
	return row, created, eventqueue.QuotaUsageAccountProof{Usage: *u, Registered: true, CreatedAt: created}
}
func TestPersonalRollingWarningKnownWindowsAndSource(t *testing.T) {
	row, birth, proof := rollingWarningFixture()
	before := proof
	samples := personalRollingWarningSamples(row, birth, proof)
	if len(samples) != 2 || samples[0].Observation.Settled != 80 || samples[1].Observation.Settled != 90 || samples[0].Observation.PolicyRevision != row.ETag || samples[0].Observation.WindowEnd.Sub(samples[0].Observation.WindowStart) != 5*time.Hour || samples[1].Observation.WindowEnd.Sub(samples[1].Observation.WindowStart) != 7*24*time.Hour || !reflect.DeepEqual(proof, before) {
		t.Fatal("exact window facts lost")
	}
	for _, kind := range []string{"5h_unknown", "7d_unknown", "coverage", "unregistered", "birth", "case", "key", "future", "negative", "zero", "null", "holds", "bad_zone"} {
		t.Run(kind, func(t *testing.T) {
			r, b, p := rollingWarningFixture()
			want := 2
			switch kind {
			case "5h_unknown":
				p.Usage.FiveHours.TokensUnknown = 1
				want = 1
			case "7d_unknown":
				p.Usage.SevenDays.TokensUnknown = 1
				want = 1
			case "coverage":
				p.Usage.CoverageStart = p.Usage.AsOf.Add(-time.Hour)
				want = 0
			case "unregistered":
				p.Registered = false
				want = 0
			case "birth":
				p.CreatedAt = b.Add(time.Microsecond)
				want = 0
			case "case":
				r.ScopeKind = "USER"
				want = 0
			case "key":
				r.ScopeKind = "key"
				want = 0
			case "future":
				b = p.Usage.AsOf.Add(time.Second)
				p.CreatedAt = b
				want = 0
			case "negative":
				p.Usage.FiveHours.TokensUsed = -1
				want = 1
			case "zero":
				z := int64(0)
				r.Tokens5H = &z
			case "null":
				r.Tokens5H = nil
			case "holds":
				p.Usage.FiveHours.TokensHeld = math.MaxInt64
			case "bad_zone":
				p.Usage.TimeZone = "invalid/zone"
				want = 0
			}
			got := personalRollingWarningSamples(r, b, p)
			if len(got) != want {
				t.Fatal("unsafe sample", kind, len(got))
			}
			if kind == "holds" && got[0].Observation.Settled != 80 {
				t.Fatal("hold became settled")
			}
			if kind == "zero" || kind == "null" {
				_, notice, e := advancePersonalRollingWarning(entity.PersonalRollingQuotaWarningState{}, got[0])
				if e != nil || notice != nil {
					t.Fatal("zero denominator notice")
				}
			}
		})
	}
}
func TestPersonalRollingWarningEpisodeRearmAndPolicyLineage(t *testing.T) {
	r, b, p := rollingWarningFixture()
	sample := personalRollingWarningSamples(r, b, p)[0]
	state, notice, e := advancePersonalRollingWarning(entity.PersonalRollingQuotaWarningState{}, sample)
	if e != nil || notice == nil || notice.Threshold != 80 {
		t.Fatal(e)
	}
	first := *notice
	advance := func(used, cap int64, reset, revision string, want int) {
		t.Helper()
		sample.Observation.AsOf = sample.Observation.AsOf.Add(time.Second)
		sample.Observation.Settled = used
		sample.Observation.Limit = cap
		sample.Observation.PolicyRevision = revision
		sample.ResetReview = reset
		var n *entity.PersonalRollingQuotaWarningObservation
		state, n, e = advancePersonalRollingWarning(state, sample)
		if e != nil || want == 0 && n != nil || want != 0 && (n == nil || n.Threshold != want) {
			t.Fatal("episode transition", used, cap, want, e)
		}
		if n != nil && want == 90 && n.EpisodeID != first.EpisodeID {
			t.Fatal("escalation changed episode")
		}
	}
	advance(85, 100, "", "lim_unrelated_money", 0)
	advance(90, 100, "", "lim_unrelated_rate", 90)
	advance(85, 100, "", "lim_downgrade", 0)
	advance(100, 100, "", "lim_at_cap", 0)
	advance(79, 100, "", "lim_below", 0)
	advance(80, 100, "", "lim_above", 80)
	if state.EpisodeID == first.EpisodeID {
		t.Fatal("known below80 did not rearm")
	}
	advance(80, 200, "", "lim_cap_change", 0)
	advance(80, 100, "", "lim_cap_return", 80)
	before := state.EpisodeID
	advance(80, 100, "review_reset", "lim_reset", 80)
	if state.EpisodeID == before {
		t.Fatal("explicit reset not consumed")
	}
	before = state.EpisodeID
	advance(85, 100, "", "lim_ordinary_after_reset", 0)
	advance(85, 100, "review_reset", "lim_same_reset", 0)
	if state.EpisodeID != before {
		t.Fatal("provenance clearing or replay rearmed")
	}
	stale := sample
	stale.Observation.AsOf = state.LastAsOf.Add(-time.Second)
	stale.Observation.Settled = 0
	after, n, e := advancePersonalRollingWarning(state, stale)
	if e != nil || n != nil || !reflect.DeepEqual(after, state) {
		t.Fatal("late sample rearmed")
	}
	alias := sample
	alias.Observation.OwnerID = strings.ToUpper(state.OwnerID)
	if _, _, e := advancePersonalRollingWarning(state, alias); e == nil {
		t.Fatal("collation alias borrowed episode")
	}
}
func TestPersonalRollingWarningCriticalFirstAndUnknownPreserve(t *testing.T) {
	r, b, p := rollingWarningFixture()
	p.Usage.FiveHours.TokensUsed = 90
	s := personalRollingWarningSamples(r, b, p)[0]
	state, n, e := advancePersonalRollingWarning(entity.PersonalRollingQuotaWarningState{}, s)
	if e != nil || n == nil || n.Threshold != 90 {
		t.Fatal(e)
	}
	s.Observation.AsOf = s.Observation.AsOf.Add(time.Second)
	s.Observation.Settled = 85
	next, n, e := advancePersonalRollingWarning(state, s)
	if e != nil || n != nil || !next.CriticalSent || !next.NearSent {
		t.Fatal("manufactured lower reminder")
	}
	p.Usage.FiveHours.TokensUsed = 0
	p.Usage.FiveHours.TokensUnknown = 1
	samples := personalRollingWarningSamples(r, b, p)
	for _, sample := range samples {
		if sample.Observation.WindowKind == "5h" {
			t.Fatal("unknown window can mutate state")
		}
	}
	if state.EpisodeID != next.EpisodeID {
		t.Fatal("unknown changed episode")
	}
}
func TestPersonalRollingWarningInboxExactBirthAndFacts(t *testing.T) {
	r, b, p := rollingWarningFixture()
	s := personalRollingWarningSamples(r, b, p)[0]
	_, o, e := advancePersonalRollingWarning(entity.PersonalRollingQuotaWarningState{}, s)
	if e != nil {
		t.Fatal(e)
	}
	o.ID = "rwo_fixture"
	row := personalRollingWarningInboxRow{ID: "rwi_fixture", RecipientID: r.ScopeID, ObservationID: o.ID, CreatedAt: o.AsOf, Observation: *o}
	access := quotaInboxAccess{ActorID: r.ScopeID, ActorCreatedAt: b}
	if !validPersonalRollingWarningInbox(row, access) {
		t.Fatal("valid private notice rejected")
	}
	for _, kind := range []string{"recipient", "owner", "birth", "window", "duration", "coverage", "zero", "level", "revision", "episode"} {
		t.Run(kind, func(t *testing.T) {
			v := row
			switch kind {
			case "recipient":
				v.RecipientID = "usr_other"
			case "owner":
				v.Observation.OwnerID = strings.ToUpper(r.ScopeID)
			case "birth":
				v.Observation.ResourceCreatedAt = b.Add(time.Microsecond)
			case "window":
				v.Observation.WindowKind = "5H"
			case "duration":
				v.Observation.WindowStart = v.Observation.WindowStart.Add(time.Second)
			case "coverage":
				v.Observation.CoverageStart = v.Observation.AsOf
			case "zero":
				v.Observation.Limit = 0
			case "level":
				v.Observation.Level = "critical"
			case "revision":
				v.Observation.PolicyRevision = "bad revision"
			case "episode":
				v.Observation.EpisodeID = ""
			}
			if validPersonalRollingWarningInbox(v, access) {
				t.Fatal("unsafe private snapshot", kind)
			}
		})
	}
	record := personalRollingWarningRecord(row)
	if record.Kind != "personal_rolling_quota_warning" || record.Severity != "medium" || record.RollingQuotaWarning.ScopeID != r.ScopeID || record.RollingQuotaWarning.Settled != "80" || record.DeliveryStatus != "" {
		t.Fatal("projection mismatch")
	}
}

func TestPersonalRollingWarningExactTinyAndMaximumCaps(t *testing.T) {
	for _, v := range []struct {
		cap, used int64
		threshold int
	}{{1, 0, 0}, {1, 1, 90}, {5, 4, 80}, {10, 9, 90}, {math.MaxInt64, 8301034833169298226, 80}, {math.MaxInt64, 8301034833169298227, 90}, {math.MaxInt64, math.MaxInt64, 90}} {
		r, b, p := rollingWarningFixture()
		r.Tokens5H = &v.cap
		p.Usage.FiveHours.TokensUsed = v.used
		s := personalRollingWarningSamples(r, b, p)[0]
		_, o, e := advancePersonalRollingWarning(entity.PersonalRollingQuotaWarningState{}, s)
		if e != nil || v.threshold == 0 && o != nil || v.threshold != 0 && (o == nil || o.Threshold != v.threshold) {
			t.Fatal("overflow-safe exact threshold", v, e)
		}
	}
}

func TestPersonalRollingWarningRequiresExactAppliedOwnCap(t *testing.T) {
	r, b, _ := rollingWarningFixture()
	user := entity.User{ID: r.ScopeID, CreatedAt: b}
	user, apps := managedAdvisorySubject(user)
	_, admission := registrationAdmission(user, apps)
	policy, e := policyFromRow(r)
	if e != nil {
		t.Fatal(e)
	}
	calendar := entity.QuotaSetting{TimeZone: "UTC", ETag: "qcs_applied", AccountingStarted: true}
	auth := &runtimeAuthorization{ValidUntil: time.Now().Add(time.Minute), UserAdmissions: map[string]runtimeAdmissionProof{user.ID: admission}, Quota: &runtimeQuotaData{Setting: calendar, Currency: "USD", Revisions: map[string]string{limitAccount("user", r.ScopeID): r.ETag}}, LimitPolicies: map[string]limits.Policy{limitAccount("user", r.ScopeID): policy}}
	svc := &Service{runtime: &gatewayRuntime{}}
	svc.runtime.auth.Store(auth)
	if !svc.quotaWarningApplied(context.Background(), r, user, apps, policy, calendar, "USD") {
		t.Fatal("current stored source not accepted")
	}
	copied := policy
	different := int64(99)
	copied.Tokens5H = &different
	auth.LimitPolicies[limitAccount("user", r.ScopeID)] = copied
	if svc.quotaWarningApplied(context.Background(), r, user, apps, policy, calendar, "USD") {
		t.Fatal("borrowed/different applied cap accepted")
	}
	auth.LimitPolicies[limitAccount("user", r.ScopeID)] = policy
	r.ETag = "lim_unpublished"
	if svc.quotaWarningApplied(context.Background(), r, user, apps, policy, calendar, "USD") {
		t.Fatal("same cap without current application accepted")
	}
}

func TestPersonalRollingWarningFirstAtOrAboveCap(t *testing.T) {
	for _, v := range []struct {
		name      string
		cap, used int64
	}{
		{"exact100", 100, 100}, {"above100", 100, 101}, {"tiny", 1, 1}, {"tiny_above", 1, math.MaxInt64}, {"maximum", math.MaxInt64, math.MaxInt64},
	} {
		t.Run(v.name, func(t *testing.T) {
			r, b, p := rollingWarningFixture()
			r.Tokens5H = &v.cap
			p.Usage.FiveHours.TokensUsed = v.used
			sample := personalRollingWarningSamples(r, b, p)[0]
			state, n, e := advancePersonalRollingWarning(entity.PersonalRollingQuotaWarningState{}, sample)
			if e != nil || n == nil || n.Level != "critical" || n.Threshold != 90 || !state.CriticalSent || !state.NearSent {
				t.Fatal("first >=90 omitted critical", e)
			}
			n.ID = "rwo_at_cap"
			row := personalRollingWarningInboxRow{ID: "rwi_at_cap", RecipientID: r.ScopeID, ObservationID: n.ID, CreatedAt: n.AsOf, Observation: *n}
			if !validPersonalRollingWarningInbox(row, quotaInboxAccess{ActorID: r.ScopeID, ActorCreatedAt: b}) {
				t.Fatal("inbox rejected above-cap critical")
			}
			episode := state.EpisodeID
			sample.Observation.AsOf = sample.Observation.AsOf.Add(time.Second)
			again, n, e := advancePersonalRollingWarning(state, sample)
			if e != nil || n != nil || again.EpisodeID != episode || !again.CriticalSent {
				t.Fatal("above-cap duplicate", e)
			}
			// Unknown settled coverage at100 cannot invent a new percentage or rearm.
			p.Usage.AsOf = sample.Observation.AsOf.Add(time.Second)
			p.Usage.FiveHours.TokensUsed = v.cap
			p.Usage.FiveHours.TokensUnknown = 1
			before := again
			for _, candidate := range personalRollingWarningSamples(r, b, p) {
				if candidate.Observation.WindowKind == "5h" {
					again, n, e = advancePersonalRollingWarning(again, candidate)
				}
			}
			if e != nil || n != nil || !reflect.DeepEqual(again, before) {
				t.Fatal("unknown100 changed episode", e)
			}
			sample.Observation.AsOf = sample.Observation.AsOf.Add(2 * time.Second)
			sample.Observation.Settled = 0
			rearmed, n, e := advancePersonalRollingWarning(again, sample)
			if e != nil || n != nil || rearmed.CriticalSent || rearmed.NearSent || rearmed.EpisodeID != "" {
				t.Fatal("known below80 failed rearm", e)
			}
			sample.Observation.AsOf = sample.Observation.AsOf.Add(time.Second)
			sample.Observation.Settled = v.used
			resumed, n, e := advancePersonalRollingWarning(rearmed, sample)
			if e != nil || n == nil || n.Threshold != 90 || resumed.EpisodeID == episode {
				t.Fatal("new above-cap episode omitted critical", e)
			}
		})
	}
}

// Missing final usage is either a retained finite hold or an unbounded unknown
// counter. Use the real journal, then the real sampler and episode transition.
func TestPersonalRollingWarningRealJournalHeldAndUnknown(t *testing.T) {
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
			row := entity.ResourceLimit{ScopeKind: "user", ScopeID: "usr_journal", ETag: "lim_journal", Tokens5H: &cap, Tokens7D: &cap}
			account := limitAccount("user", row.ScopeID)
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
			proofAt := func(at time.Time) eventqueue.QuotaUsageAccountProof {
				t.Helper()
				proof, err := q.AccountQuotaUsageProofBatch([]string{account}, at)
				if err != nil {
					t.Fatal(err)
				}
				return proof.Accounts[account]
			}
			reserve("known", eventqueue.QuotaBound{Tokens: &known, Revision: "bound_journal"}, birth)
			complete("known", eventqueue.QuotaSettlement{Tokens: &known}, birth)
			samples := personalRollingWarningSamples(row, birth, proofAt(birth.Add(time.Second)))
			if len(samples) != 2 {
				t.Fatal("known window samples")
			}
			states := make([]entity.PersonalRollingQuotaWarningState, 2)
			for i, sample := range samples {
				state, notice, err := advancePersonalRollingWarning(states[i], sample)
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
			for _, window := range []eventqueue.QuotaUsage{proof.Usage.FiveHours, proof.Usage.SevenDays} {
				if window.TokensUsed != 90 {
					t.Fatal("settled subtotal changed")
				}
				if bounded && (window.TokensHeld != 2 || window.TokensUnknown != 0) || !bounded && (window.TokensHeld != 0 || window.TokensUnknown != 1) {
					t.Fatal("hold and unknown conflated")
				}
			}
			before := append([]entity.PersonalRollingQuotaWarningState(nil), states...)
			samples = personalRollingWarningSamples(row, birth, proof)
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
				after, notice, err := advancePersonalRollingWarning(states[i], sample)
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
