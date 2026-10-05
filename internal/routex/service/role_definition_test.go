package service

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func roleDefinitionTestInput() RoleDefinitionInput {
	return RoleDefinitionInput{Name: "Reviewed role", Permissions: []string{"members.read", "prices.read"}, IdentityETag: strings.Repeat("a", 64), Reason: "Controlled permission replacement"}
}

func TestRoleDefinitionStrictCompleteInput(t *testing.T) {
	input := roleDefinitionTestInput()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var decoded RoleDefinitionInput
	if err := json.Unmarshal(raw, &decoded); err != nil || !reflect.DeepEqual(input, decoded) {
		t.Fatal("valid complete replacement", err, decoded)
	}
	base := string(raw)
	cases := map[string]string{
		"duplicate_field":       strings.Replace(base, `"name":`, `"name":"Other","name":`, 1),
		"unknown_field":         strings.TrimSuffix(base, "}") + `,"role_id":"rol_override"}`,
		"missing_field":         strings.Replace(base, `"name":"Reviewed role",`, "", 1),
		"trailing_object":       base + `{}`,
		"null_name":             strings.Replace(base, `"Reviewed role"`, `null`, 1),
		"null_permissions":      strings.Replace(base, `["members.read","prices.read"]`, `null`, 1),
		"null_entry":            strings.Replace(base, `["members.read","prices.read"]`, `[null]`, 1),
		"unsorted_permissions":  strings.Replace(base, `["members.read","prices.read"]`, `["prices.read","members.read"]`, 1),
		"duplicate_permission":  strings.Replace(base, `["members.read","prices.read"]`, `["members.read","members.read"]`, 1),
		"unsafe_permission":     strings.Replace(base, `"members.read"`, `"members/read"`, 1),
		"wrong_permission_type": strings.Replace(base, `"members.read"`, `true`, 1),
		"whitespace_name":       strings.Replace(base, `"Reviewed role"`, `" Reviewed role"`, 1),
		"empty_name":            strings.Replace(base, `"Reviewed role"`, `""`, 1),
		"control_name":          strings.Replace(base, `"Reviewed role"`, `"Bad\nname"`, 1),
		"too_long_name":         strings.Replace(base, `"Reviewed role"`, `"`+strings.Repeat("界", 101)+`"`, 1),
		"surrogate_name":        strings.Replace(base, `"Reviewed role"`, `"\ud800"`, 1),
		"null_identity":         strings.Replace(base, `"`+input.IdentityETag+`"`, `null`, 1),
		"uppercase_identity":    strings.Replace(base, input.IdentityETag, strings.Repeat("A", 64), 1),
		"short_identity":        strings.Replace(base, input.IdentityETag, "a", 1),
		"empty_reason":          strings.Replace(base, `"Controlled permission replacement"`, `""`, 1),
		"whitespace_reason":     strings.Replace(base, `"Controlled permission replacement"`, `" trailing "`, 1),
		"control_reason":        strings.Replace(base, `"Controlled permission replacement"`, `"Bad\nreason"`, 1),
		"too_long_reason_bytes": strings.Replace(base, `"Controlled permission replacement"`, `"`+strings.Repeat("界", 342)+`"`, 1),
		"null_reason":           strings.Replace(base, `"Controlled permission replacement"`, `null`, 1),
		"malformed_utf8":        strings.Replace(base, "Reviewed role", string([]byte{0xff}), 1),
		"oversized_body":        strings.Repeat(" ", 64*1024) + base,
		"array_body":            `[]`,
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			preserved := roleDefinitionTestInput()
			// Match the HTTP decoder: encoding/json strips outer whitespace before invoking this method.
			if err := preserved.UnmarshalJSON([]byte(value)); err == nil || !reflect.DeepEqual(preserved, input) {
				t.Fatal("ambiguous input accepted or partially replaced receiver", err)
			}
		})
	}
	for _, permissions := range [][]string{{}, {"Historical.Mixed_CASE"}} {
		input.Permissions = permissions
		raw, err := json.Marshal(input)
		if err != nil || json.Unmarshal(raw, &decoded) != nil || !reflect.DeepEqual(decoded, input) {
			t.Fatal("explicit empty or safe recorded syntax rejected before current-authority check", err)
		}
	}
	input.Permissions = make([]string, 101)
	if err := validateRoleDefinitionInput(input); err == nil {
		t.Fatal("unbounded request")
	}
}

func roleDefinitionTestSnapshot(t *testing.T, permissions []string) *roleDefinitionSnapshot {
	t.Helper()
	birth := time.Date(2026, 10, 5, 0, 0, 0, 123456000, time.UTC)
	actor := entity.User{ID: "usr_admin", Role: entity.RoleAdmin, CreatedAt: birth, MemberRoleRevision: memberRoleBaseline}
	role := entity.Role{ID: "rol_legacyExact", Name: "Reviewed role", CreatedAt: birth, DefinitionRevision: memberRoleBaseline}
	result, err := projectRoleDefinition(actor, runtimeAdmissionProof{CreatedAt: birth, State: "not_required", Eligible: true}, role, permissions)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestRoleDefinitionPrivateGenerationBirthAndExactReadShape(t *testing.T) {
	first := roleDefinitionTestSnapshot(t, []string{"Historical.Mixed_CASE", "members.read"})
	if !slices.Equal(first.Record.Permissions, []string{"Historical.Mixed_CASE", "members.read"}) || !first.Record.CanEdit || first.Record.IdentityETag == nil {
		t.Fatal("safe recorded case or birth proof lost")
	}
	for _, reserved := range []string{"roles.write", "registration.write", "members.approvals.write"} {
		if slices.Contains(first.Record.AvailablePermissions, reserved) {
			t.Fatal("protected capability became assignable", reserved)
		}
	}
	raw, err := json.Marshal(first.Record)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil || len(body) != 9 {
		t.Fatal("noncontract read fields", string(raw))
	}
	for _, name := range []string{"id", "name", "builtin", "permissions", "available_permissions", "definition_etag", "identity_etag", "review_etag", "can_edit"} {
		if body[name] == nil {
			t.Fatal("missing read field", name)
		}
	}
	role := first.Role
	role.DefinitionRevision = strings.Repeat("b", 64)
	second, err := projectRoleDefinition(first.Actor, runtimeAdmissionProof{CreatedAt: first.Actor.CreatedAt, State: "not_required", Eligible: true}, role, first.Record.Permissions)
	if err != nil || second.Record.ReviewETag == first.Record.ReviewETag || second.Record.DefinitionETag == first.Record.DefinitionETag || *second.Record.IdentityETag != *first.Record.IdentityETag {
		t.Fatal("private revision ABA lost or immutable identity changed", err)
	}
	role.CreatedAt = role.CreatedAt.Add(time.Microsecond)
	recreated, err := projectRoleDefinition(first.Actor, runtimeAdmissionProof{Eligible: true}, role, first.Record.Permissions)
	if err != nil || *recreated.Record.IdentityETag == *first.Record.IdentityETag {
		t.Fatal("recreated exact ID borrowed old birth", err)
	}
	role.CreatedAt = time.Time{}
	unknown, err := projectRoleDefinition(first.Actor, runtimeAdmissionProof{Eligible: true}, role, first.Record.Permissions)
	if err != nil || unknown.Record.IdentityETag != nil || unknown.Record.CanEdit {
		t.Fatal("unknown provenance invented writable identity", err)
	}
	birth := first.Role
	birth.CreatedAt = birth.CreatedAt.In(time.FixedZone("retained offset", 8*3600))
	identity, err := roleDefinitionIdentity(birth)
	if err != nil || *identity != *first.Record.IdentityETag {
		t.Fatal("stored instant normalized inconsistently", err)
	}
	role.Builtin = true
	builtin, err := projectRoleDefinition(first.Actor, runtimeAdmissionProof{Eligible: true}, role, first.Record.Permissions)
	if err != nil || builtin.Record.CanEdit {
		t.Fatal("builtin became editable", err)
	}
	actor := first.Actor
	actor.MemberRoleRevision = "invalid"
	if _, err := projectRoleDefinition(actor, runtimeAdmissionProof{Eligible: true}, first.Role, first.Record.Permissions); err != roleDefinitionUnavailable {
		t.Fatal("unsafe actor generation invented review", err)
	}
	for _, permissions := range [][]string{{"members.read", "members.read"}, {"bad/code"}, make([]string, 101)} {
		if _, err := projectRoleDefinition(first.Actor, runtimeAdmissionProof{Eligible: true}, first.Role, permissions); err == nil {
			t.Fatal("invalid complete stored projection accepted")
		}
	}
}

func TestRoleDefinitionCurrentOnlyRetryCannotChangeStaleOrRecreatedTarget(t *testing.T) {
	snapshot := roleDefinitionTestSnapshot(t, []string{"members.read", "prices.read"})
	input := roleDefinitionTestInput()
	input.IdentityETag = *snapshot.Record.IdentityETag
	old := strings.Repeat("f", 64)
	if err := reviewRoleDefinition(snapshot, old, input); err != nil {
		t.Fatal("exact same incarnation current state cannot confirm", err)
	}
	input.Name = "Different proposal"
	if err := reviewRoleDefinition(snapshot, old, input); err != catalogConflict {
		t.Fatal("stale proposal changed current definition", err)
	}
	if err := reviewRoleDefinition(snapshot, snapshot.Record.ReviewETag, input); err != nil {
		t.Fatal("fresh exact complete review rejected", err)
	}
	input.Name = snapshot.Role.Name
	input.IdentityETag = strings.Repeat("e", 64)
	if err := reviewRoleDefinition(snapshot, old, input); err != catalogConflict {
		t.Fatal("equal content reconciled a different birth", err)
	}
	input.IdentityETag = *snapshot.Record.IdentityETag
	snapshot.Record.IdentityETag = nil
	if err := reviewRoleDefinition(snapshot, old, input); err != catalogConflict {
		t.Fatal("unknown birth reconciled", err)
	}
	snapshot = roleDefinitionTestSnapshot(t, []string{"Historical.Mixed_CASE"})
	input.IdentityETag = *snapshot.Record.IdentityETag
	input.Permissions = []string{"Historical.Mixed_CASE"}
	if err := reviewRoleDefinition(snapshot, old, input); err != apperrors.ErrBadRequest {
		t.Fatal("recorded nonassignable code gained equality authority", err)
	}
	snapshot.Role.Builtin = true
	if err := reviewRoleDefinition(snapshot, old, input); err != apperrors.ErrForbidden {
		t.Fatal("builtin equality bypass", err)
	}
}

func TestRoleDefinitionTypedAuditExcludesUnknownAndPrivateData(t *testing.T) {
	before := roleDefinitionAuditValues{"Retained name", []string{"Historical.Mixed_CASE"}}
	after := roleDefinitionAuditValues{"Reviewed name", []string{"members.read"}}
	raw, err := encodeRoleDefinitionAudit("rol_legacy", "Reviewed replacement", before, after)
	if err != nil {
		t.Fatal(err)
	}
	row := entity.AuditEvent{ResourceID: "rol_legacy", ResourceType: "role", Action: "role.definition.update", DetailsJSON: &raw}
	changes, valid := roleDefinitionAuditProjection(row)
	if !valid || changes.Before.Name != before.Name || !slices.Equal(changes.Before.Permissions, before.Permissions) {
		t.Fatal("recorded safe audit facts lost")
	}
	public := auditRecord(row)
	if public.Changes == nil || strings.Contains(string(public.Changes), "definition_revision") || strings.Contains(string(public.Changes), "created_at") {
		t.Fatal("safe projection missing or leaked private proofs")
	}
	for _, altered := range []string{
		strings.TrimSuffix(raw, "}") + `,"password":"opaque"}`,
		strings.Replace(raw, `"before":{`, `"before":{"secret":"opaque",`, 1),
		strings.Replace(raw, `"reason":`, `"reason":"Other","reason":`, 1),
		strings.Replace(raw, `"Reviewed replacement"`, `"\ud800"`, 1),
		strings.Replace(raw, `"role_id":"rol_legacy"`, `"role_id":"rol_OTHER"`, 1),
	} {
		row.DetailsJSON = &altered
		if _, valid := roleDefinitionAuditProjection(row); valid || auditRecord(row).Changes != nil {
			t.Fatal("unsafe audit projected", altered)
		}
	}
}
