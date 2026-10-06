package service

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func TestRoleDescriptionStrictTextAndPresence(t *testing.T) {
	for _, value := range []string{"Recorded purpose", "First line\n第二行", strings.Repeat("界", 666) + "ab"} {
		if !validRoleDescription(value, false) {
			t.Fatal("valid byte-bounded description rejected")
		}
		input := roleDefinitionTestInput()
		input.Description = value
		raw, _ := json.Marshal(input)
		var decoded RoleDefinitionInput
		if err := decoded.UnmarshalJSON(raw); err != nil || decoded.Description != value {
			t.Fatal("reviewed value changed", err)
		}
	}
	for name, value := range map[string]string{"empty": "", "boundary": " trailing ", "lf_boundary": "first\n", "tab": "first\tsecond", "cr": "first\rsecond", "nul": "first\x00second", "del": "first\x7fsecond", "overbytes": strings.Repeat("界", 667), "invalid_utf8": string([]byte{0xff})} {
		t.Run(name, func(t *testing.T) {
			if validRoleDescription(value, false) {
				t.Fatal("invalid description accepted")
			}
			input := roleDefinitionTestInput()
			input.Description = value
			if validateRoleDefinitionInput(input) != apperrors.ErrBadRequest {
				t.Fatal("review did not reject invalid text")
			}
		})
	}
	base := `{"name":"Custom role","description":"Recorded purpose","permissions":[]}`
	for _, raw := range []string{strings.Replace(base, `"Recorded purpose"`, `null`, 1), strings.Replace(base, `"Recorded purpose"`, `""`, 1), strings.Replace(base, `"Recorded purpose"`, `"\ud800"`, 1), strings.Replace(base, `"Recorded purpose"`, `" trailing "`, 1), strings.Replace(base, `"description"`, `"Description"`, 1), strings.Replace(base, `"description":`, `"description":"Other","description":`, 1)} {
		preserved := RoleCreationInput{Name: "preserved"}
		if preserved.UnmarshalJSON([]byte(raw)) == nil || preserved.Name != "preserved" {
			t.Fatal("explicit invalid creation description fell back to legacy or changed receiver")
		}
	}
	var legacy RoleCreationInput
	if legacy.UnmarshalJSON([]byte(`{"name":"Legacy","permissions":[]}`)) != nil || legacy.Description != nil || legacy.Permissions == nil {
		t.Fatal("legacy creation broken")
	}
	input := roleDefinitionTestInput()
	raw, _ := json.Marshal(input)
	for _, body := range []string{strings.Replace(string(raw), `"description":"Recorded purpose",`, "", 1), strings.Replace(string(raw), `"Recorded purpose"`, `null`, 1)} {
		if input.UnmarshalJSON([]byte(body)) == nil {
			t.Fatal("incomplete reviewed description accepted")
		}
	}
}

func TestRoleDescriptionGenerationAndHistoricalAudit(t *testing.T) {
	before := roleDefinitionTestSnapshot(t, []string{"members.read"})
	oldReview, oldDefinition := before.Record.ReviewETag, before.Record.DefinitionETag
	role := before.Role
	role.Description = "Changed purpose"
	changed, err := projectRoleDefinition(before.Actor, runtimeAdmissionProof{CreatedAt: before.Actor.CreatedAt, State: "not_required", Eligible: true}, role, before.Record.Permissions)
	if err != nil || changed.Record.ReviewETag == oldReview || changed.Record.DefinitionETag == oldDefinition {
		t.Fatal("description missing from complete definition or review", err)
	}
	input := roleDefinitionTestInput()
	input.Name = before.Role.Name
	input.Permissions = before.Record.Permissions
	input.IdentityETag = *before.Record.IdentityETag
	if err := reviewRoleDefinition(changed, oldReview, input); err != catalogConflict {
		t.Fatal("stale description review accepted", err)
	}
	historical := `{"role_id":"rol_legacy","reason":"Recorded replacement","before":{"name":"Original","permissions":[]},"after":{"name":"Updated","permissions":["members.read"]}}`
	row := entity.AuditEvent{Action: "role.definition.update", ResourceType: "role", ResourceID: "rol_legacy", DetailsJSON: &historical}
	projection, ok := roleDefinitionAuditProjection(row)
	if !ok || projection.Version != 0 || projection.Before.Description != nil || projection.After.Description != nil {
		t.Fatal("historical exact format lost or description invented")
	}
	for _, raw := range []string{strings.TrimSuffix(historical, "}") + `,"version":1}`, strings.Replace(historical, `"before":{`, `"before":{"description":"invented",`, 1)} {
		row.DetailsJSON = &raw
		if _, ok := roleDefinitionAuditProjection(row); ok {
			t.Fatal("mixed historical/new audit accepted")
		}
	}
}

func TestRoleDescriptionSQLAtomicPreservationAndCurrentRetry(t *testing.T) {
	for _, mode := range []string{"description_only", "audit_failure", "uncertain_retry", "trusted_omission"} {
		t.Run(mode, func(t *testing.T) {
			s, fixture, _ := roleDefinitionSQLService(t)
			record, err := s.GetRoleDefinition(context.Background(), "usr_admin", "rol_00000")
			if err != nil {
				t.Fatal(err)
			}
			baseline := fixture.data.clone()
			fixture.writes = nil
			if mode == "trusted_omission" {
				saved, err := s.SaveRole(context.Background(), "usr_admin", record.ID, "Renamed trusted role", record.Permissions)
				if err != nil || saved.Role.Description != record.Description || fixture.data.roles[record.ID].Description != record.Description {
					t.Fatal("trusted omission erased description", err)
				}
				return
			}
			input := RoleDefinitionInput{Name: record.Name, Description: "Changed purpose\n第二行", Permissions: record.Permissions, IdentityETag: *record.IdentityETag, Reason: "Reviewed description only"}
			fixture.failAudit = mode == "audit_failure"
			fixture.failConfirmation = mode == "uncertain_retry"
			result, err := s.SetReviewedRoleDefinition(context.Background(), "usr_admin", record.ID, record.ReviewETag, input)
			if mode == "audit_failure" {
				if err == nil || result != nil || !reflect.DeepEqual(fixture.data, baseline) {
					t.Fatal("description audit failure did not rollback all facts", err)
				}
				return
			}
			changed := fixture.data.roles[record.ID]
			if changed.Description != input.Description || changed.Name != baseline.roles[record.ID].Name || changed.DefinitionRevision == baseline.roles[record.ID].DefinitionRevision || !changed.CreatedAt.Equal(baseline.roles[record.ID].CreatedAt) || !reflect.DeepEqual(fixture.data.permissions, baseline.permissions) || !reflect.DeepEqual(fixture.data.users, baseline.users) || !reflect.DeepEqual(fixture.data.assignments, baseline.assignments) || len(fixture.data.audits) != 1 {
				t.Fatal("description-only mutation changed unrelated facts")
			}
			for _, write := range fixture.writes {
				if strings.Contains(write, "role_permissions") {
					t.Fatal("unchanged permissions rewritten")
				}
			}
			audit, ok := roleDefinitionAuditProjection(fixture.data.audits[0])
			if !ok || audit.Version != 2 || audit.Before.Description == nil || *audit.Before.Description != record.Description || audit.After.Description == nil || *audit.After.Description != input.Description {
				t.Fatal("typed versioned description audit missing")
			}
			if mode == "uncertain_retry" {
				if err != roleDefinitionUnavailable || result != nil {
					t.Fatal("uncertain commit falsely confirmed", err)
				}
				fixture.failConfirmation = false
				current := fixture.data.clone()
				fixture.writes = nil
				result, err = s.SetReviewedRoleDefinition(context.Background(), "usr_admin", record.ID, record.ReviewETag, input)
				if err != nil || result == nil || result.Description != input.Description || len(fixture.writes) != 0 || !reflect.DeepEqual(current, fixture.data) {
					t.Fatal("immutable old retry wrote or lost description", err)
				}
			} else if err != nil || result == nil || result.Description != input.Description {
				t.Fatal("description not confirmed", err)
			}
		})
	}
}
