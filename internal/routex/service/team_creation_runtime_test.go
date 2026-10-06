package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

func teamCreationPublicationFixture(t *testing.T) (*Service, entity.TeamCreationReceipt, *ResourceRecord, *runtimeAuthorization) {
	t.Helper()
	born := time.Unix(1700000000, 123000000).UTC()
	policy, err := limits.Normalize(limits.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := TeamCreationSnapshot{Name: "Original Team", Description: "Original description", Owners: []TeamCreationOwnerSnapshot{{"usr_owner", "tmm_owner", born.UnixMicro()}}, OwnerJoinedAt: born, DefaultRuleETag: strings.Repeat("a", 64), SubmittedFields: []string{}, PolicyETag: "lim_original", Policy: policy}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	receipt := entity.TeamCreationReceipt{CreationID: teamCreationTestInput().CreationID, ActorID: "usr_actor", ActorCreatedAt: born, TeamID: "tea_original", TeamCreatedAt: born, RequestHash: strings.Repeat("b", 64), ReviewETag: strings.Repeat("c", 64), SnapshotJSON: string(raw), CreatedAt: born}
	auth := &runtimeAuthorization{TeamCreationGrants: map[string]map[string]runtimeTeamCreationGrant{"tea_original": {}}, ValidUntil: time.Now().Add(time.Minute), Teams: map[string]runtimeTeam{"tea_original": {CreatedAt: born, Members: map[string]string{"usr_owner": "tmm_owner"}, Models: map[string]bool{}}}, UserAdmissions: map[string]runtimeAdmissionProof{"usr_owner": {CreatedAt: born, Eligible: true}, "usr_actor": {CreatedAt: born, Eligible: true}}, LimitPolicies: map[string]limits.Policy{limitAccount("team", "tea_original"): policy}, Quota: &runtimeQuotaData{Revisions: map[string]string{limitAccount("team", "tea_original"): snapshot.PolicyETag}, Currency: "USD"}}
	s := &Service{runtime: &gatewayRuntime{}, recorder: &callRecorder{}}
	s.runtime.auth.Store(auth)
	current := &ResourceRecord{ID: receipt.TeamID, Name: snapshot.Name, Description: snapshot.Description, CreatedAt: born, Status: entity.ResourceActive}
	return s, receipt, current, auth
}
func TestTeamCreationSnapshotComplete1000AndEncodedBounds(t *testing.T) {
	_, receipt, _, _ := teamCreationPublicationFixture(t)
	snapshot, err := readTeamCreationSnapshot(receipt)
	if err != nil {
		t.Fatal(err)
	}
	// Maximum allowed metadata and safe 30-byte identities, not approximate
	// per-owner arithmetic. Escaped separators expand JSON beyond UTF-8 input.
	snapshot.Name = "N" + strings.Repeat("\u2028", 98) + "N"
	snapshot.Description = strings.Repeat("\u2028", 2000)
	snapshot.Reason = strings.Repeat("界", 666)
	snapshot.SubmittedFields = []string{"concurrency", "money_month", "rpm", "tokens_5h", "tokens_7d", "tokens_month", "tpm"}
	snapshot.Owners = make([]TeamCreationOwnerSnapshot, 1000)
	for i := range snapshot.Owners {
		snapshot.Owners[i] = TeamCreationOwnerSnapshot{fmt.Sprintf("usr_%026d", i), fmt.Sprintf("tmm_%026d", i), receipt.ActorCreatedAt.UnixMicro()}
	}
	max := int64(limits.MaxInteger)
	money := "999999999999999999.999999999999999999"
	snapshot.Policy, err = limits.Normalize(limits.Policy{Tokens5H: &max, Tokens7D: &max, TokensMonth: &max, RPM: &max, TPM: &max, Concurrency: &max, MoneyMonth: &money, Currency: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("1000 exact owner triples and maximum admitted metadata/policy encode %d bytes (limit %d)", len(raw), teamCreationSnapshotBytes)
	if len(raw) > teamCreationSnapshotBytes || len(raw) <= 65535 {
		t.Fatal("supported1000 receipt does not exercise portable large storage", len(raw))
	}
	receipt.SnapshotJSON = string(raw)
	decoded, err := readTeamCreationSnapshot(receipt)
	if err != nil || len(decoded.Owners) != 1000 || decoded.Owners[999] != snapshot.Owners[999] || !decoded.OwnerJoinedAt.Equal(snapshot.OwnerJoinedAt) {
		t.Fatal("complete historical identity lost", err)
	}
	before := receipt
	for name, change := range map[string]func(*entity.TeamCreationReceipt){
		"overflow": func(r *entity.TeamCreationReceipt) { r.SnapshotJSON = strings.Repeat(" ", teamCreationSnapshotBytes+1) },
		"unknown": func(r *entity.TeamCreationReceipt) {
			r.SnapshotJSON = strings.Replace(r.SnapshotJSON, `"name":`, `"unknown":1,"name":`, 1)
		},
		"triple": func(r *entity.TeamCreationReceipt) {
			r.SnapshotJSON = strings.Replace(r.SnapshotJSON, `"usr_00000000000000000000000000"`, `null`, 1)
		},
		"birth": func(r *entity.TeamCreationReceipt) { r.ActorCreatedAt = time.Time{} },
		"duplicate": func(r *entity.TeamCreationReceipt) {
			bad := snapshot
			bad.Owners = append([]TeamCreationOwnerSnapshot(nil), snapshot.Owners...)
			bad.Owners[1] = bad.Owners[0]
			raw, _ := json.Marshal(bad)
			r.SnapshotJSON = string(raw)
		},
		"wrong_relation": func(r *entity.TeamCreationReceipt) {
			bad := snapshot
			bad.Owners = append([]TeamCreationOwnerSnapshot(nil), snapshot.Owners...)
			bad.Owners[0].MembershipID = "pmg_wrong"
			raw, _ := json.Marshal(bad)
			r.SnapshotJSON = string(raw)
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := before
			change(&r)
			if _, err := readTeamCreationSnapshot(r); err == nil {
				t.Fatal("corrupt historical proof accepted")
			}
		})
	}
}
func TestTeamCreationCurrentRuntimeProofCannotRestoreHistory(t *testing.T) {
	tests := []struct {
		name, status string
		change       func(*Service, *entity.TeamCreationReceipt, **ResourceRecord, *runtimeAuthorization)
	}{
		{"original", "applied", func(*Service, *entity.TeamCreationReceipt, **ResourceRecord, *runtimeAuthorization) {}},
		{"unavailable", "unavailable", func(_ *Service, _ *entity.TeamCreationReceipt, c **ResourceRecord, _ *runtimeAuthorization) { *c = nil }},
		{"metadata", "superseded", func(_ *Service, _ *entity.TeamCreationReceipt, c **ResourceRecord, _ *runtimeAuthorization) {
			(*c).Name = "Changed"
		}},
		{"lifecycle", "superseded", func(_ *Service, _ *entity.TeamCreationReceipt, c **ResourceRecord, _ *runtimeAuthorization) {
			(*c).Status = entity.ResourceArchived
		}},
		{"SQLbirth", "superseded", func(_ *Service, _ *entity.TeamCreationReceipt, c **ResourceRecord, _ *runtimeAuthorization) {
			(*c).CreatedAt = (*c).CreatedAt.Add(time.Millisecond)
		}},
		{"no_recorder", "pending", func(s *Service, _ *entity.TeamCreationReceipt, _ **ResourceRecord, _ *runtimeAuthorization) {
			s.recorder = nil
		}},
		{"lease", "pending", func(_ *Service, _ *entity.TeamCreationReceipt, _ **ResourceRecord, a *runtimeAuthorization) {
			a.ValidUntil = time.Now().Add(-time.Second)
		}},
		{"Team_birth", "pending", func(_ *Service, _ *entity.TeamCreationReceipt, _ **ResourceRecord, a *runtimeAuthorization) {
			team := a.Teams["tea_original"]
			team.CreatedAt = team.CreatedAt.Add(time.Millisecond)
			a.Teams["tea_original"] = team
		}},
		{"owner_birth", "pending", func(_ *Service, _ *entity.TeamCreationReceipt, _ **ResourceRecord, a *runtimeAuthorization) {
			p := a.UserAdmissions["usr_owner"]
			p.CreatedAt = p.CreatedAt.Add(time.Millisecond)
			a.UserAdmissions["usr_owner"] = p
		}},
		{"actor_birth", "pending", func(_ *Service, _ *entity.TeamCreationReceipt, _ **ResourceRecord, a *runtimeAuthorization) {
			p := a.UserAdmissions["usr_actor"]
			p.CreatedAt = p.CreatedAt.Add(time.Millisecond)
			a.UserAdmissions["usr_actor"] = p
		}},
		{"pending_admission", "pending", func(_ *Service, _ *entity.TeamCreationReceipt, _ **ResourceRecord, a *runtimeAuthorization) {
			p := a.UserAdmissions["usr_owner"]
			p.Eligible = false
			a.UserAdmissions["usr_owner"] = p
		}},
		{"relationship", "pending", func(_ *Service, _ *entity.TeamCreationReceipt, _ **ResourceRecord, a *runtimeAuthorization) {
			a.Teams["tea_original"].Members["usr_owner"] = "tmm_rejoined"
		}},
		{"extra_member", "pending", func(_ *Service, _ *entity.TeamCreationReceipt, _ **ResourceRecord, a *runtimeAuthorization) {
			a.Teams["tea_original"].Members["usr_extra"] = "tmm_extra"
		}},
		{"models", "pending", func(_ *Service, _ *entity.TeamCreationReceipt, _ **ResourceRecord, a *runtimeAuthorization) {
			a.Teams["tea_original"].Models["mdl_new"] = true
		}},
		{"policy_revision", "pending", func(_ *Service, _ *entity.TeamCreationReceipt, _ **ResourceRecord, a *runtimeAuthorization) {
			a.Quota.Revisions[limitAccount("team", "tea_original")] = "lim_new"
		}},
		{"policy", "pending", func(_ *Service, _ *entity.TeamCreationReceipt, _ **ResourceRecord, a *runtimeAuthorization) {
			n := int64(0)
			a.LimitPolicies[limitAccount("team", "tea_original")] = limits.Policy{TokensMonth: &n}
		}},
	}
	for _, which := range []string{"user", "actor", "session", "member", "team", "limit", "quota"} {
		scope := which
		tests = append(tests, struct {
			name, status string
			change       func(*Service, *entity.TeamCreationReceipt, **ResourceRecord, *runtimeAuthorization)
		}{"tombstone_" + scope, "pending", func(s *Service, _ *entity.TeamCreationReceipt, _ **ResourceRecord, _ *runtimeAuthorization) {
			switch scope {
			case "user":
				s.runtime.deniedUsers.Store("usr_owner", uint64(1))
			case "actor":
				s.runtime.deniedUsers.Store("usr_actor", uint64(1))
			case "session":
				s.runtime.deniedSessionUsers.Store("usr_owner", uint64(1))
			case "member":
				s.runtime.deniedTeamMembers.Store(teamMemberRuntimeKey("tea_original", "usr_owner"), uint64(1))
			case "team":
				s.runtime.deniedTeams.Store("tea_original", uint64(1))
			case "limit":
				s.runtime.deniedLimits.Store(limitAccount("team", "tea_original"), uint64(1))
			case "quota":
				s.runtime.deniedLimits.Store("quota_settings", uint64(1))
			}
		}})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, r, c, a := teamCreationPublicationFixture(t)
			test.change(s, &r, &c, a)
			rawBefore := r.SnapshotJSON
			applied, status := s.teamCreationApplication(r, c)
			if status != test.status || applied != (status == "applied") || r.SnapshotJSON != rawBefore {
				t.Fatal("runtime used historical receipt as desired state", applied, status, test.status)
			}
		})
	}
}

func TestTeamCreationRuntimeExactMoneyCurrencyAndTimeEquality(t *testing.T) {
	s, receipt, current, auth := teamCreationPublicationFixture(t)
	snapshot, err := readTeamCreationSnapshot(receipt)
	if err != nil {
		t.Fatal(err)
	}
	money := "0.000000000000000001"
	snapshot.Policy, err = limits.Normalize(limits.Policy{MoneyMonth: &money, Currency: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	receipt.SnapshotJSON = string(raw)
	account := limitAccount("team", receipt.TeamID)
	auth.LimitPolicies[account] = snapshot.Policy
	// Equal instants in another location must not lose receipt/runtime authority.
	other := time.FixedZone("offset", 3600)
	team := auth.Teams[receipt.TeamID]
	team.CreatedAt = team.CreatedAt.In(other)
	auth.Teams[receipt.TeamID] = team
	owner := auth.UserAdmissions["usr_owner"]
	owner.CreatedAt = owner.CreatedAt.In(other)
	auth.UserAdmissions["usr_owner"] = owner
	if applied, status := s.teamCreationApplication(receipt, current); !applied || status != "applied" {
		t.Fatal("exact money/time proof not applied", applied, status)
	}
	auth.Quota.Currency = "EUR"
	if applied, _ := s.teamCreationApplication(receipt, current); applied {
		t.Fatal("current denomination ignored")
	}
	auth.Quota.Currency = "USD"
	wrong := "0.000000000000000002"
	policy := snapshot.Policy
	policy.MoneyMonth = &wrong
	auth.LimitPolicies[account] = policy
	if applied, _ := s.teamCreationApplication(receipt, current); applied {
		t.Fatal("decimal distinction rounded away")
	}
}
