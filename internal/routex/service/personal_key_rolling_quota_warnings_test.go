package service

import (
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"math"
	"testing"
	"time"
)

func keyRollingFixture() (entity.ResourceLimit, entity.APIKey, entity.User, *eventqueue.QuotaUsageProofBatch) {
	row, root, owner, frame := personalKeyWarningFixture()
	cap := int64(100)
	row.Tokens5H, row.Tokens7D = &cap, &cap
	proof := frame.Accounts[limitAccount("key", root.ID)]
	proof.Usage.FiveHours.TokensUsed = 80
	proof.Usage.SevenDays.TokensUsed = 90
	frame.Accounts[limitAccount("key", root.ID)] = proof
	return row, root, owner, frame
}
func TestPersonalKeyRollingWarningOwnRootAndCoveredFacts(t *testing.T) {
	for _, kind := range []string{"valid", "finite_held", "5h_unknown", "7d_unknown", "unregistered", "root_birth", "owner_birth", "owner", "child", "missing", "wrong_scope", "incomplete_coverage", "zero", "null"} {
		t.Run(kind, func(t *testing.T) {
			row, root, owner, frame := keyRollingFixture()
			key := limitAccount("key", root.ID)
			proof := frame.Accounts[key]
			want := 2
			switch kind {
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
				root.UserID = "usr_other"
				want = 0
			case "child":
				p := "key_00000000000000000000000002"
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
			if kind != "missing" {
				frame.Accounts[key] = proof
			}
			samples := personalKeyRollingWarningSamples(row, root, owner, frame)
			if len(samples) != want {
				t.Fatalf("%s samples=%d want=%d", kind, len(samples), want)
			}
			if kind == "finite_held" && samples[0].Rolling.Observation.Settled != 80 {
				t.Fatal("finite held became settled")
			}
			if kind == "zero" || kind == "null" {
				_, notice, err := advancePersonalKeyRollingWarning(entity.PersonalKeyRollingQuotaWarningState{}, samples[0])
				if err != nil || notice != nil {
					t.Fatal("no denominator emitted warning")
				}
			}
		})
	}
}
func TestPersonalKeyRollingWarningEpisodesAndUnchangedEdits(t *testing.T) {
	row, root, owner, frame := keyRollingFixture()
	sample := personalKeyRollingWarningSamples(row, root, owner, frame)[0]
	var state entity.PersonalKeyRollingQuotaWarningState
	step := func(settled int64, expect string) {
		t.Helper()
		sample.Rolling.Observation.AsOf = sample.Rolling.Observation.AsOf.Add(time.Second)
		sample.Rolling.Observation.WindowEnd = sample.Rolling.Observation.AsOf
		sample.Rolling.Observation.WindowStart = sample.Rolling.Observation.AsOf.Add(-5 * time.Hour)
		sample.Rolling.Observation.Settled = settled
		var notice *entity.PersonalKeyRollingQuotaWarningObservation
		var err error
		state, notice, err = advancePersonalKeyRollingWarning(state, sample)
		if err != nil || expect == "" && notice != nil || expect != "" && (notice == nil || notice.Level != expect || notice.RootKeyID != root.ID || notice.OwnerID != owner.ID || notice.RootKeyName != root.Name) {
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
	sample.Rolling.ResetReview = "lim_explicit_reset"
	step(100, "critical")
	step(100, "")
	sample.Rolling.ResetReview = ""
	step(100, "")
	sample.Rolling.Observation.Limit = 101
	step(100, "critical")
	old := state
	owner.CreatedAt = owner.CreatedAt.Add(time.Microsecond)
	sample.Owner = owner
	if _, _, err := advancePersonalKeyRollingWarning(state, sample); err == nil {
		t.Fatal("owner rebirth accepted")
	}
	if state != old {
		t.Fatal("state changed on identity rejection")
	}
}

func TestPersonalKeyRollingWarningHistoricalInboxIdentity(t *testing.T) {
	row, root, owner, frame := keyRollingFixture()
	sample := personalKeyRollingWarningSamples(row, root, owner, frame)[0]
	_, v, err := advancePersonalKeyRollingWarning(entity.PersonalKeyRollingQuotaWarningState{}, sample)
	if err != nil || v == nil {
		t.Fatal(err)
	}
	v.ID = "kro_retained"
	access := quotaInboxAccess{ActorID: owner.ID, ActorCreatedAt: owner.CreatedAt}
	before := personalKeyRollingWarningInboxRow{RecipientCreatedAt: owner.CreatedAt, CurrentRootKeyID: root.ID, CurrentRootCreatedAt: root.CreatedAt, CurrentRootUserID: owner.ID, ID: "kri_retained", RecipientID: owner.ID, ObservationID: v.ID, CreatedAt: v.AsOf, Observation: *v}
	if !validPersonalKeyRollingWarningInbox(before, access) {
		t.Fatal("valid retained original root denied")
	}
	record := personalKeyRollingWarningRecord(before)
	if record.PersonalKeyRollingQuotaWarning == nil || record.SubjectName != root.Name || record.Kind != "personal_key_rolling_quota_warning" || record.RollingQuotaWarning != nil || record.DeliveryStatus != "" {
		t.Fatal("key facts or family mixed")
	}
	for _, name := range []string{"actor", "owner_birth", "root_birth", "root_owner", "child", "owner_reborn", "root_name", "generation", "window", "threshold", "coverage"} {
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
				r.CurrentRootUserID = "usr_other"
			case "child":
				parent := "key_other"
				r.CurrentRootParent = &parent
			case "owner_reborn":
				r.Observation.OwnerCreatedAt = r.Observation.OwnerCreatedAt.Add(time.Microsecond)
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
			if validPersonalKeyRollingWarningInbox(r, a) {
				t.Fatal("invalid historical private identity accepted")
			}
		})
	}
}
