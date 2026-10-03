package service

import (
	"encoding/json"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
	"strings"
	"testing"
	"time"
)

func projectCreationPublicationFixture(t *testing.T) (*Service, entity.ProjectCreationReceipt, *ResourceRecord, *runtimeAuthorization) {
	t.Helper()
	policy, err := limits.Normalize(limits.Policy{IPMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := ProjectCreationSnapshot{ProjectID: "prj_proof", Name: "Recorded creation", CreatorID: "usr_proof", Managers: []ProjectCreationManagerSnapshot{{ID: "pmg_proof", UserID: "usr_proof"}}, ModelIDs: []string{"mdl_proof"}, PolicyETag: "0", Policy: policy, InitialRequestIDs: []string{}}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	receipt := entity.ProjectCreationReceipt{CreationID: "11111111-1111-4111-8111-111111111145", ActorID: snapshot.CreatorID, ProjectID: snapshot.ProjectID, RequestHash: strings.Repeat("a", 64), ReviewETag: strings.Repeat("b", 64), SnapshotJSON: string(raw), CreatedAt: time.Now()}
	auth := &runtimeAuthorization{ValidUntil: time.Now().Add(time.Minute), Models: map[string]bool{"mdl_proof": true}, LimitPolicies: map[string]limits.Policy{}}
	addProjectCreationAuthorization(auth, &projectRuntimeData{Projects: []entity.Project{{ID: snapshot.ProjectID, Name: snapshot.Name, CreatorID: snapshot.CreatorID, Status: entity.ResourceActive}}, Managers: []entity.ProjectManager{{ID: "pmg_proof", ProjectID: snapshot.ProjectID, UserID: snapshot.CreatorID}}, Grants: []entity.ProjectModelGrant{{ProjectID: snapshot.ProjectID, ModelID: "mdl_proof"}}}, map[string]bool{snapshot.CreatorID: true})
	s := &Service{runtime: &gatewayRuntime{}}
	s.runtime.auth.Store(auth)
	return s, receipt, &ResourceRecord{ID: snapshot.ProjectID, Status: entity.ResourceActive}, auth
}

func TestProjectCreationPublicationBoundary(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Service, *entity.ProjectCreationReceipt, **ResourceRecord, *runtimeAuthorization)
		status string
	}{
		{"original configuration", func(*Service, *entity.ProjectCreationReceipt, **ResourceRecord, *runtimeAuthorization) {}, "applied"},
		{"unrelated metadata", func(_ *Service, _ *entity.ProjectCreationReceipt, _ **ResourceRecord, a *runtimeAuthorization) {
			state := a.ProjectCreationStates["prj_proof"]
			state.Project.Name = "New reviewed name"
			a.ProjectCreationStates["prj_proof"] = state
		}, "applied"},
		{"no current read", func(_ *Service, _ *entity.ProjectCreationReceipt, r **ResourceRecord, _ *runtimeAuthorization) {
			*r = nil
		}, "unavailable"},
		{"wrong current target", func(_ *Service, _ *entity.ProjectCreationReceipt, r **ResourceRecord, _ *runtimeAuthorization) {
			(*r).ID = "prj_other"
		}, "unavailable"},
		{"archived", func(_ *Service, _ *entity.ProjectCreationReceipt, r **ResourceRecord, _ *runtimeAuthorization) {
			(*r).Status = entity.ResourceArchived
		}, "superseded"},
		{"expired lease", func(_ *Service, _ *entity.ProjectCreationReceipt, _ **ResourceRecord, a *runtimeAuthorization) {
			a.ValidUntil = time.Now().Add(-time.Second)
		}, "pending"},
		{"unpublished", func(s *Service, _ *entity.ProjectCreationReceipt, _ **ResourceRecord, _ *runtimeAuthorization) {
			s.runtime.auth.Store(nil)
		}, "pending"},
		{"project tombstone", func(s *Service, _ *entity.ProjectCreationReceipt, _ **ResourceRecord, _ *runtimeAuthorization) {
			s.runtime.deniedProjects.Store("prj_proof", uint64(1))
		}, "pending"},
		{"manager replaced", func(_ *Service, _ *entity.ProjectCreationReceipt, _ **ResourceRecord, a *runtimeAuthorization) {
			a.ProjectCreationStates["prj_proof"].Managers["usr_proof"] = "pmg_rejoined"
		}, "pending"},
		{"manager disabled", func(_ *Service, _ *entity.ProjectCreationReceipt, _ **ResourceRecord, a *runtimeAuthorization) {
			a.ProjectCreationStates["prj_proof"].EnabledManagers["usr_proof"] = false
		}, "pending"},
		{"user tombstone", func(s *Service, _ *entity.ProjectCreationReceipt, _ **ResourceRecord, _ *runtimeAuthorization) {
			s.runtime.deniedUsers.Store("usr_proof", uint64(1))
		}, "pending"},
		{"grant removed", func(_ *Service, _ *entity.ProjectCreationReceipt, _ **ResourceRecord, a *runtimeAuthorization) {
			state := a.ProjectCreationStates["prj_proof"]
			state.Models = []string{}
			a.ProjectCreationStates["prj_proof"] = state
		}, "pending"},
		{"inactive model", func(_ *Service, _ *entity.ProjectCreationReceipt, _ **ResourceRecord, a *runtimeAuthorization) {
			a.Models["mdl_proof"] = false
		}, "pending"},
		{"limit tombstone", func(s *Service, _ *entity.ProjectCreationReceipt, _ **ResourceRecord, _ *runtimeAuthorization) {
			s.runtime.deniedLimits.Store(limitAccount("project", "prj_proof"), uint64(1))
		}, "pending"},
		{"policy changed", func(_ *Service, _ *entity.ProjectCreationReceipt, _ **ResourceRecord, a *runtimeAuthorization) {
			zero := int64(0)
			a.LimitPolicies[limitAccount("project", "prj_proof")] = limits.Policy{TokensMonth: &zero}
		}, "pending"},
		{"corrupt receipt", func(_ *Service, r *entity.ProjectCreationReceipt, _ **ResourceRecord, _ *runtimeAuthorization) {
			r.SnapshotJSON = "{}"
		}, "unavailable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, receipt, current, auth := projectCreationPublicationFixture(t)
			c.change(s, &receipt, &current, auth)
			applied, status := s.projectCreationApplication(receipt, current)
			if status != c.status || applied != (c.status == "applied") {
				t.Fatalf("got %v %s", applied, status)
			}
		})
	}
}

func TestProjectCreationMoneyPublicationRequiresExactRevision(t *testing.T) {
	for _, mode := range []string{"exact", "wrong_revision", "wrong_currency", "no_quota"} {
		t.Run(mode, func(t *testing.T) {
			s, r, current, a := projectCreationPublicationFixture(t)
			snapshot, err := readProjectCreationSnapshot(r)
			if err != nil {
				t.Fatal(err)
			}
			money := "0.01"
			snapshot.Policy.MoneyMonth = &money
			snapshot.Policy.Currency = "USD"
			snapshot.PolicyETag = strings.Repeat("c", 64)
			raw, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			r.SnapshotJSON = string(raw)
			account := limitAccount("project", r.ProjectID)
			a.LimitPolicies[account] = snapshot.Policy
			a.Quota = &runtimeQuotaData{Currency: "USD", Revisions: map[string]string{account: snapshot.PolicyETag}}
			switch mode {
			case "wrong_revision":
				a.Quota.Revisions[account] = "other"
			case "wrong_currency":
				a.Quota.Currency = "EUR"
			case "no_quota":
				a.Quota = nil
			}
			applied, status := s.projectCreationApplication(r, current)
			if applied != (mode == "exact") || mode == "exact" && status != "applied" || mode != "exact" && status != "pending" {
				t.Fatal(applied, status)
			}
		})
	}
}
