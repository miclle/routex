package service

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func TestMemberStateStrictInput(t *testing.T) {
	good := []string{`{"role":"member","reason":"Reviewed"}`, `{"disabled":false,"reason":"启用"}`}
	bad := []string{`{}`, `{"reason":"R"}`, `{"role":"member","disabled":false,"reason":"R"}`, `{"Role":"member","reason":"R"}`, `{"role":"Admin","reason":"R"}`, `{"role":null,"reason":"R"}`, `{"disabled":null,"reason":"R"}`, `{"disabled":"false","reason":"R"}`, `{"disabled":true,"reason":null}`, `{"disabled":true,"reason":" R"}`, `{"disabled":true,"reason":"R\n"}`, `{"role":"member","reason":"R","reason":"X"}`, `{"role":"member","\u0072ole":"admin","reason":"R"}`, `{"role":"member","reason":"\ud800"}`, `{"role":"member","reason":"R","email":"x"}`, `[]`, `{"disabled":true,"reason":"R"} {}`, `{"disabled":true,"reason":"` + strings.Repeat("界", 342) + `"}`, "{\"disabled\":true,\"reason\":\"\xff\"}"}
	for _, raw := range good {
		var input MemberStateInput
		if err := json.Unmarshal([]byte(raw), &input); err != nil {
			t.Fatal(raw, err)
		}
	}
	for i, raw := range bad {
		var input MemberStateInput
		if err := json.Unmarshal([]byte(raw), &input); err == nil {
			t.Fatal("accepted invalid case", i)
		}
	}
	var boundary MemberStateInput
	if err := json.Unmarshal([]byte(`{"disabled":true,"reason":"`+strings.Repeat("a", 1024)+`"}`), &boundary); err != nil {
		t.Fatal("byte boundary", err)
	}
}
func TestMemberStateRevisionMetadataABAAndCurrentNoop(t *testing.T) {
	s, f := roleSQLService(t, 0)
	actor, target := f.data.users["usr_admin"], f.data.users["usr_target"]
	original := memberStateRecord(actor, target, true)
	for _, field := range []string{"actor_revision", "target_revision", "created", "name", "updated", "disabled", "role", "offboarded"} {
		a, b := actor, target
		switch field {
		case "actor_revision":
			a.MemberRoleRevision = strings.Repeat("a", 64)
		case "target_revision":
			b.MemberRoleRevision = strings.Repeat("a", 64)
		case "created":
			b.CreatedAt = b.CreatedAt.Add(time.Microsecond)
		case "name":
			b.Name = "changed"
		case "updated":
			b.UpdatedAt = b.UpdatedAt.Add(time.Microsecond)
		case "disabled":
			b.Disabled = true
		case "role":
			b.Role = entity.RoleAdmin
		case "offboarded":
			b.Disabled = true
			now := b.CreatedAt
			b.OffboardedAt = &now
		}
		if memberStateRecord(a, b, true).ETag == original.ETag {
			t.Fatal("review missed", field)
		}
	}
	value := entity.RoleMember
	snapshot := f.data.clone()
	result, err := s.SetReviewedMemberState(context.Background(), actor.ID, target.ID, strings.Repeat("f", 64), MemberStateInput{Role: &value, Reason: "Original uncertain request"})
	if err != nil || result.Confirmation != "current_member_state" || result.Effect != "current_base_identity" || result.AccountAccessRuntimeApplied || len(f.writes) != 0 || !reflect.DeepEqual(snapshot, f.data) {
		t.Fatal("current no-op wrote or invented runtime", err)
	}
	changed := entity.RoleAdmin
	if result, err := s.SetReviewedMemberState(context.Background(), actor.ID, target.ID, strings.Repeat("f", 64), MemberStateInput{Role: &changed, Reason: "R"}); err != catalogConflict || result != nil || len(f.writes) != 0 {
		t.Fatal("stale changing review", err)
	}
}
func TestMemberStateSQLSnapshotBoundAndExactAuthority(t *testing.T) {
	for _, mode := range []string{"reader", "writer", "denied", "actor_alias", "target_alias", "disabled_actor", "offboarded_actor", "inconsistent_target"} {
		t.Run(mode, func(t *testing.T) {
			s, f := roleSQLService(t, 0)
			switch mode {
			case "reader":
				f.deny["members.write"] = true
			case "denied":
				f.deny["members.write"] = true
				f.deny["members.read"] = true
			case "actor_alias":
				f.actorAlias = true
			case "target_alias":
				f.subjectAlias = true
			case "disabled_actor", "offboarded_actor":
				u := f.data.users["usr_admin"]
				u.Disabled = true
				if mode == "offboarded_actor" {
					now := u.CreatedAt
					u.OffboardedAt = &now
				}
				f.data.users[u.ID] = u
			case "inconsistent_target":
				u := f.data.users["usr_target"]
				now := u.CreatedAt
				u.OffboardedAt = &now
				f.data.users[u.ID] = u
			}
			result, err := s.GetMemberState(context.Background(), "usr_admin", "usr_target")
			if mode == "reader" || mode == "writer" {
				expected := 4
				if mode == "reader" {
					expected = 6
				}
				if err != nil || result == nil || len(f.queries) != expected || len(f.writes) != 0 || len(f.transactions) != 1 || !f.transactions[0].ReadOnly {
					t.Fatal("bounded RR read", err, len(f.queries))
				}
				if result.CanChangeStatus != (mode == "writer") {
					t.Fatal("write leaked")
				}
				return
			}
			if err == nil || result != nil || len(f.writes) != 0 {
				t.Fatal("invalid authority/state read")
			}
			if mode == "denied" {
				userReads := 0
				for _, q := range f.queries {
					if strings.Contains(q, `FROM "users"`) {
						userReads++
					}
				}
				if userReads != 3 || len(f.queries) != 5 {
					t.Fatal("private target read after denied permissions", userReads, len(f.queries))
				}
			}
		})
	}
}
func TestMemberStateAtomicAuditAndFreshConfirmation(t *testing.T) {
	for _, mode := range []string{"success", "audit_failure", "confirmation_outage"} {
		t.Run(mode, func(t *testing.T) {
			s, f := roleSQLService(t, 0)
			original := f.data.clone()
			review, err := s.GetMemberState(context.Background(), "usr_admin", "usr_target")
			if err != nil {
				t.Fatal(err)
			}
			next := entity.RoleAdmin
			input := MemberStateInput{Role: &next, Reason: "Reviewed promotion"}
			f.failAudit = mode == "audit_failure"
			f.failConfirmation = mode == "confirmation_outage"
			result, err := s.SetReviewedMemberState(context.Background(), "usr_admin", "usr_target", review.ETag, input)
			if mode == "audit_failure" {
				if err != memberStateUnavailable || result != nil || !reflect.DeepEqual(original, f.data) {
					t.Fatal("non-atomic audit failure", err)
				}
				return
			}
			if len(f.data.audits) != 1 || f.data.users["usr_target"].Role != entity.RoleAdmin || f.data.users["usr_target"].MemberRoleRevision == memberRoleBaseline {
				t.Fatal("missing durable change", err)
			}
			if mode == "confirmation_outage" {
				if err != memberStateUnavailable || result != nil {
					t.Fatal("invented current confirmation", err)
				}
				f.failConfirmation = false
				writes := len(f.writes)
				result, err = s.SetReviewedMemberState(context.Background(), "usr_admin", "usr_target", review.ETag, input)
				if len(f.writes) != writes || len(f.data.audits) != 1 {
					t.Fatal("retry mutation replay")
				}
			}
			if err != nil || result == nil || result.BaseRole != entity.RoleAdmin || result.Effect != "current_base_identity" {
				t.Fatal(result, err)
			}
			if got := auditRecord(f.data.audits[0]); len(got.Changes) == 0 {
				t.Fatal("typed audit not projected")
			}
		})
	}
}
func TestMemberStateSelfRoleLossAndAccountUnknown(t *testing.T) {
	s, f := roleSQLService(t, 0)
	u := f.data.users["usr_admin"]
	u.Role = entity.RoleMember
	f.data.users[u.ID] = u
	next := entity.RoleMember
	result, err := s.confirmMemberState(context.Background(), u.ID, u.ID, MemberStateInput{Role: &next, Reason: "Original demotion"})
	if result != nil || err != apperrors.ErrForbidden {
		t.Fatal("fresh intrinsic role lost", err)
	}
	s, f = roleSQLService(t, 0)
	disabled := false
	result, err = s.SetReviewedMemberState(context.Background(), "usr_admin", "usr_target", strings.Repeat("f", 64), MemberStateInput{Disabled: &disabled, Reason: "Uncertain enable"})
	if result != nil || err != memberStateUnavailable || len(f.writes) != 0 {
		t.Fatal("no runtime invented lifecycle confirmation", err)
	}
}
func TestMemberStateAuditRejectsForeignUnknownAndMalformed(t *testing.T) {
	_, f := roleSQLService(t, 0)
	before := f.data.users["usr_target"]
	after := before
	after.Role = entity.RoleAdmin
	role := entity.RoleAdmin
	raw, _ := json.Marshal(memberStateAuditDetails(before, after, MemberStateInput{Role: &role, Reason: "R"}))
	detail := string(raw)
	row := entity.AuditEvent{Action: "member.state.update", ResourceType: "user", ResourceID: before.ID, DetailsJSON: &detail}
	if _, ok := memberStateAuditProjection(row); !ok {
		t.Fatal("valid audit")
	}
	for _, variant := range []string{strings.Replace(detail, `"operation":"base_role"`, `"operation":"enable"`, 1), strings.Replace(detail, `"base_role":"member"`, `"base_role":null`, 1), strings.Replace(detail, `"disabled":false`, `"disabled":false,"password":"secret"`, 1), strings.Replace(detail, `"reason":"R"`, `"reason":"R","reason":"X"`, 1), strings.Replace(detail, before.ID, "usr_foreign", 1)} {
		copy := row
		copy.DetailsJSON = &variant
		if _, ok := memberStateAuditProjection(copy); ok || len(auditRecord(copy).Changes) > 0 {
			t.Fatal("unsafe audit exposed")
		}
	}
}
