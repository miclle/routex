package service

import (
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

func TestProjectKeyMonthlyBehaviorExactRoot(t *testing.T) {
	row, root, project, _ := projectKeyWarningFixture()
	row.TokensMonthBehavior = "alert_only"
	row.MoneyMonthBehavior = "stop"
	p, err := projectKeyPolicyFromRow(row, root, project)
	if err != nil || p.TokensMonthBehavior != "alert_only" || p.MoneyMonthBehavior != "" {
		t.Fatal(p, err)
	}
	if _, err := policyFromRow(row); err == nil {
		t.Fatal("generic key row acquired soft authority")
	}
	for _, name := range []string{"wrong_Project", "Project_alias", "zero_Project_birth", "root_before_Project", "zero_root_birth", "successor", "scope_alias", "root_alias", "invalid_mode"} {
		t.Run(name, func(t *testing.T) {
			r, k, o := row, root, project
			switch name {
			case "wrong_Project":
				o.ID = "prj_other"
			case "Project_alias":
				o.ID = "PRJ_wrong"
			case "zero_Project_birth":
				o.CreatedAt = time.Time{}
			case "root_before_Project":
				o.CreatedAt = k.CreatedAt.Add(time.Millisecond)
			case "zero_root_birth":
				k.CreatedAt = time.Time{}
			case "successor":
				k.ReplacesKeyID = &root.ID
			case "scope_alias":
				r.ScopeKind = "Key"
			case "root_alias":
				r.ScopeID = "pky_alias"
			case "invalid_mode":
				r.MoneyMonthBehavior = "ALERT_ONLY"
			}
			if _, err := projectKeyPolicyFromRow(r, k, o); err == nil {
				t.Fatal("unproved root admitted")
			}
		})
	}
}

func TestProjectKeyMonthlyBehaviorPublicationExactGraph(t *testing.T) {
	row, root, project, _ := projectKeyWarningFixture()
	row.TokensMonthBehavior = "alert_only"
	child := root
	child.ID = "pky_child"
	child.ReplacesKeyID = &root.ID
	child.CreatedAt = root.CreatedAt.Add(time.Second)
	for _, name := range []string{"retained_revoked_root", "missing_Project", "duplicate_Project", "missing_root", "cross_Project", "duplicate_key", "Personal_collision", "Project_birth", "orphan_link", "root_alias"} {
		t.Run(name, func(t *testing.T) {
			root.Status = entity.KeyRevoked
			d := &runtimeData{Limits: []entity.ResourceLimit{row}, ProjectData: &projectRuntimeData{Projects: []entity.Project{project}, Keys: []entity.ProjectKey{root, child}}}
			switch name {
			case "missing_Project":
				d.ProjectData.Projects = nil
			case "duplicate_Project":
				d.ProjectData.Projects = append(d.ProjectData.Projects, project)
			case "missing_root":
				d.ProjectData.Keys = d.ProjectData.Keys[1:]
			case "cross_Project":
				d.ProjectData.Keys[1].ProjectID = "prj_other"
			case "duplicate_key":
				d.ProjectData.Keys = append(d.ProjectData.Keys, child)
			case "Personal_collision":
				d.Keys = []entity.APIKey{{ID: root.ID, UserID: "usr_owner"}}
			case "Project_birth":
				d.ProjectData.Projects[0].CreatedAt = time.Time{}
			case "orphan_link":
				v := "pky_missing"
				d.ProjectData.Keys[1].ReplacesKeyID = &v
			case "root_alias":
				d.Limits[0].ScopeID = "PKY_alias"
			}
			err := compileRuntimeLimits(d)
			if (err == nil) != (name == "retained_revoked_root") {
				t.Fatal(err)
			}
			if err == nil && d.LimitRoots[child.ID] != root.ID {
				t.Fatal("rotation root reset")
			}
		})
	}
}

func TestProjectKeyMonthlyBehaviorGatewayProof(t *testing.T) {
	_, root, project, _ := projectKeyWarningFixture()
	child := entity.APIKey{ID: "pky_child", Status: entity.KeyActive, CreatedAt: root.CreatedAt.Add(time.Second), ReplacesKeyID: &root.ID}
	makeAuth := func() *runtimeAuthorization {
		return &runtimeAuthorization{
			LimitRoots: map[string]string{child.ID: root.ID}, ProjectLimitOwners: map[string]projectKeyMonthlyIdentity{root.ID: {Root: root, Project: project}},
			KeysByID:              map[string]runtimeKey{child.ID: {Key: child, ProjectID: project.ID}},
			ProjectCreationStates: map[string]runtimeProjectCreationState{project.ID: {Project: project}},
			Quota:                 &runtimeQuotaData{Created: map[string]time.Time{limitAccount("key", root.ID): root.CreatedAt, limitAccount("key", child.ID): child.CreatedAt}},
		}
	}
	result := GatewayResult{KeyID: child.ID, ProjectID: project.ID}
	account := limitAccount("key", root.ID)
	for _, name := range []string{"valid", "manager_Personal", "wrong_Project", "child_account", "root_birth", "Project_birth", "archived", "wrong_current_owner", "no_root", "no_current"} {
		t.Run(name, func(t *testing.T) {
			a, r, acc := makeAuth(), result, account
			switch name {
			case "manager_Personal":
				r.UserID = "usr_manager"
			case "wrong_Project":
				r.ProjectID = "prj_other"
			case "child_account":
				acc = limitAccount("key", child.ID)
			case "root_birth":
				a.Quota.Created[account] = root.CreatedAt.Add(time.Millisecond)
			case "Project_birth":
				o := project
				o.CreatedAt = o.CreatedAt.Add(time.Millisecond)
				a.ProjectCreationStates[project.ID] = runtimeProjectCreationState{Project: o}
			case "archived":
				o := project
				o.Status = entity.ResourceArchived
				a.ProjectCreationStates[project.ID] = runtimeProjectCreationState{Project: o}
			case "wrong_current_owner":
				a.KeysByID[child.ID] = runtimeKey{Key: child, ProjectID: "prj_other"}
			case "no_root":
				delete(a.ProjectLimitOwners, root.ID)
			case "no_current":
				delete(a.KeysByID, child.ID)
			}
			if projectKeyQuotaProof(a, &r, acc) != (name == "valid") {
				t.Fatal("proof mismatch")
			}
			if personalKeyQuotaProof(a, &r, acc) {
				t.Fatal("Project borrowed Personal proof")
			}
		})
	}
}

func TestProjectKeyMonthlyBehaviorSettledWarningsAndAudit(t *testing.T) {
	row, root, project, frame := projectKeyWarningFixture()
	row.TokensMonthBehavior = "alert_only"
	row.MoneyMonthBehavior = "alert_only"
	if got := projectKeyMonthlyWarnings(row, root, project, frame, "USD"); len(got) != 2 {
		t.Fatal("soft policy suppressed exact settled warnings", got)
	}
	proof := frame.Accounts[limitAccount("key", root.ID)]
	proof.Usage.Month.TokensUnknown = 1
	proof.Usage.Month.MoneyUnknown = 1
	frame.Accounts[limitAccount("key", root.ID)] = proof
	if got := projectKeyMonthlyWarnings(row, root, project, frame, "USD"); len(got) != 0 {
		t.Fatal("unknown became warning", got)
	}
	audit := entity.AuditEvent{ResourceType: "key", ResourceID: root.ID}
	policy := limits.Policy{TokensMonthBehavior: "alert_only"}
	if !validLimitAuditBehavior(audit, "project", policy) || validLimitAuditBehavior(audit, "", policy) {
		t.Fatal("typed Project parent audit lost or inferred")
	}
	audit.ResourceID = "key_personal"
	if validLimitAuditBehavior(audit, "project", policy) {
		t.Fatal("foreign Key audit accepted")
	}
}
