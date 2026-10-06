package service

import (
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

type memberRoleDefinition struct {
	Summary     MemberRoleSummary
	Permissions []string
}
type memberRolesSnapshot struct {
	Page           MemberRolesWorkspace
	Actor, Subject entity.User
	Definitions    map[string]memberRoleDefinition
	Catalogue      []string
	Assigned       []string
}

// Retained unknown permission codes preserve their exact recorded case. Only
// exact catalogue membership may contribute to the effective permission union.
func memberRolePermissionCode(value string) bool {
	if value == "" || len(value) > 80 {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			if r < '0' || r > '9' {
				if r != '.' && r != '_' && r != '-' {
					return false
				}
			}
		}
	}
	return true
}
func projectMemberRoleDefinition(role entity.Role, permissions []string) (memberRoleDefinition, error) {
	if !memberRoleID(role.ID) || !memberAccessLabel(role.Name) || !validMemberRoleDigest(role.DefinitionRevision) || len(permissions) > 100 {
		return memberRoleDefinition{}, memberRolesUnavailable
	}
	kind, err := roleAssignmentKind(role)
	if err != nil {
		return memberRoleDefinition{}, err
	}
	permissions = slices.Clone(permissions)
	slices.Sort(permissions)
	for i, code := range permissions {
		if !memberRolePermissionCode(code) || i > 0 && permissions[i-1] == code {
			return memberRoleDefinition{}, memberRolesUnavailable
		}
	}
	if permissions == nil {
		permissions = []string{}
	}
	etag, err := teamQuotaHash(struct {
		Version            string
		ID, Name, Revision string
		Builtin            bool
		Permissions        []string
	}{"member.role.definition.v1", role.ID, role.Name, role.DefinitionRevision, role.Builtin, permissions})
	return memberRoleDefinition{MemberRoleSummary{ID: role.ID, Name: role.Name, Builtin: role.Builtin, AssignmentKind: kind, PermissionCount: len(permissions), DefinitionETag: etag}, permissions}, err
}
func memberRolesUnion(definitions map[string]memberRoleDefinition, ids []string) []string {
	result := []string{}
	for _, id := range ids {
		for _, p := range definitions[id].Permissions {
			if slices.Contains(AvailablePermissions, p) && !slices.Contains(result, p) {
				result = append(result, p)
			}
		}
	}
	slices.Sort(result)
	return result
}
func projectMemberRoles(actor, subject entity.User, assigned, catalogue []string, definitions map[string]memberRoleDefinition, catalogueOverflow bool) (*memberRolesSnapshot, error) {
	if subject.Role != entity.RoleAdmin && subject.Role != entity.RoleMember || !validMemberRoleDigest(subject.MemberRoleRevision) {
		return nil, memberRolesUnavailable
	}
	builtin := "rol_member"
	if subject.Role == entity.RoleAdmin {
		builtin = "rol_admin"
	}
	implicit, ok := definitions[builtin]
	if !ok || implicit.Summary.AssignmentKind != RoleAssignmentIntrinsic {
		return nil, memberRolesUnavailable
	}
	if len(assigned) > memberRolesReadBudget {
		return nil, memberRolesOverflow
	}
	assigned = slices.Clone(assigned)
	catalogue = slices.Clone(catalogue)
	slices.Sort(assigned)
	slices.Sort(catalogue)
	page := MemberRolesWorkspace{UserID: subject.ID, ObservedAt: time.Now().UTC(), IdentityRole: subject.Role, SubjectStatus: "active", BuiltinRole: implicit.Summary, AssignedRoles: []MemberRoleSummary{}, PermissionUse: "active", EditBlockers: []string{}, CandidateStatus: "available"}
	for i, id := range assigned {
		d, ok := definitions[id]
		if !ok || d.Summary.AssignmentKind != RoleAssignmentExplicit || i > 0 && assigned[i-1] == id {
			return nil, memberRolesUnavailable
		}
		page.AssignedRoles = append(page.AssignedRoles, d.Summary)
	}
	for i, id := range catalogue {
		d, ok := definitions[id]
		if !ok || d.Summary.AssignmentKind != RoleAssignmentExplicit || i > 0 && catalogue[i-1] == id {
			return nil, memberRolesUnavailable
		}
	}
	page.EffectivePermissions = memberRolesUnion(definitions, append(slices.Clone(assigned), builtin))
	if subject.Disabled {
		page.SubjectStatus = "disabled"
		page.PermissionUse = "inactive"
	}
	if subject.OffboardedAt != nil {
		page.SubjectStatus = "offboarded"
		page.PermissionUse = "inactive"
		page.EditBlockers = append(page.EditBlockers, "offboarded")
	}
	if actor.Role != entity.RoleAdmin {
		page.EditBlockers = append(page.EditBlockers, "not_platform_admin")
		page.CandidateStatus = "not_authorized"
	}
	if len(assigned) > memberRolesWriteBeforeBudget {
		page.EditBlockers = append(page.EditBlockers, "assignment_audit_bound")
	}
	if catalogueOverflow {
		page.EditBlockers = append(page.EditBlockers, "candidate_catalogue_bound")
		page.CandidateStatus = "overflow"
	}
	page.CanEdit = len(page.EditBlockers) == 0
	candidateProof := []MemberRoleSummary{}
	for _, id := range catalogue {
		candidateProof = append(candidateProof, definitions[id].Summary)
	}
	type identity struct {
		ID, Role, Revision string
		Disabled           bool
		OffboardedAt       *time.Time
		CreatedAt          time.Time
	}
	subjectProof := identity{subject.ID, subject.Role, subject.MemberRoleRevision, subject.Disabled, utcMemberMetadataTime(subject.OffboardedAt), subject.CreatedAt.UTC()}
	// Only current actor identity/admin eligibility is relevant to independent write authority.
	actorProof := identity{actor.ID, actor.Role, "", actor.Disabled, utcMemberMetadataTime(actor.OffboardedAt), actor.CreatedAt.UTC()}
	etag, err := teamQuotaHash(struct {
		Version             string
		Actor, Subject      identity
		Builtin             MemberRoleSummary
		Assigned, Catalogue []MemberRoleSummary
		CanEdit             bool
		CandidateStatus     string
	}{"member.roles.v1", actorProof, subjectProof, implicit.Summary, page.AssignedRoles, candidateProof, page.CanEdit, page.CandidateStatus})
	if err != nil {
		return nil, err
	}
	page.ETag = etag
	return &memberRolesSnapshot{page, actor, subject, definitions, catalogue, assigned}, nil
}
func memberRolesReview(snapshot *memberRolesSnapshot, etag string, input MemberRolesInput) error {
	if !snapshot.Page.CanEdit {
		return catalogConflict
	}
	if snapshot.Page.BuiltinRole.DefinitionETag != input.BuiltinDefinitionETag {
		return catalogConflict
	}
	for _, proof := range input.RoleDefinitions {
		d, ok := snapshot.Definitions[proof.ID]
		if !ok || d.Summary.AssignmentKind != RoleAssignmentExplicit || !slices.Contains(snapshot.Catalogue, proof.ID) {
			return apperrors.ErrBadRequest
		}
		if d.Summary.DefinitionETag != proof.ETag {
			return catalogConflict
		}
	}
	// Identical current IDs and exact definitions confirm current database state only.
	if slices.Equal(snapshot.Assigned, input.RoleIDs) {
		return nil
	}
	if snapshot.Page.ETag != etag {
		return catalogConflict
	}
	return nil
}
func memberRolesResult(snapshot *memberRolesSnapshot) *MemberRolesResult {
	return &MemberRolesResult{snapshot.Subject.ID, slices.Clone(snapshot.Assigned), snapshot.Page.ETag, "current_member_roles", "current_database"}
}
