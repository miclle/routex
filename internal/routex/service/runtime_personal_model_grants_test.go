package service

import (
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestPersonalModelGrantPublicationRequiresOriginalSource(t *testing.T) {
	source := "mar_original"
	createdAt := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	data := &runtimeData{Users: []entity.User{{ID: "usr_one", CreatedAt: createdAt}}, Models: []entity.Model{{ID: "mdl_one", Status: "active"}}, Grants: []entity.UserModelGrant{{UserID: "usr_one", ModelID: "mdl_one", SourceRequestID: &source}}}
	s := &Service{runtime: &gatewayRuntime{}}
	publish := func() { s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute))) }
	publish()
	if !s.RuntimePersonalModelGrantApplied("usr_one", "mdl_one", source) {
		t.Fatal("original source was not published")
	}
	if s.RuntimePersonalModelGrantApplied("usr_one", "mdl_one", "mar_other") {
		t.Fatal("different receipt borrowed original source")
	}
	data.Users[0].CreatedAt = time.Time{}
	publish()
	if s.RuntimePersonalModelGrantApplied("usr_one", "mdl_one", source) {
		t.Fatal("missing account birth retained publication proof")
	}
	data.Users[0].CreatedAt = createdAt
	applicationID := "raa_01j00000000000000000000000"
	data.Users[0].ApprovalApplicationID = &applicationID
	data.ApprovalApplications = []entity.RegistrationApprovalApplication{{ID: applicationID, UserID: "usr_one", UserCreatedAt: createdAt, CreatedAt: createdAt, State: "pending", Revision: memberRoleBaseline}}
	publish()
	if s.RuntimePersonalModelGrantApplied("usr_one", "mdl_one", source) {
		t.Fatal("pending applicant retained publication proof")
	}
	data.Users[0].ApprovalApplicationID = nil
	data.ApprovalApplications = nil
	publish()
	if !s.RuntimePersonalModelGrantApplied("usr_one", "mdl_one", source) {
		t.Fatal("restored unmanaged account lost original-source publication proof")
	}
	data.Grants[0].SourceRequestID = nil
	publish()
	if s.RuntimePersonalModelGrantApplied("usr_one", "mdl_one", source) {
		t.Fatal("ordinary removed-and-readded grant revived receipt proof")
	}
	data.Grants[0].SourceRequestID = &source
	data.Users[0].Disabled = true
	publish()
	if s.RuntimePersonalModelGrantApplied("usr_one", "mdl_one", source) {
		t.Fatal("disabled applicant retained publication proof")
	}
	data.Users[0].Disabled = false
	data.Models[0].Status = "disabled"
	publish()
	if s.RuntimePersonalModelGrantApplied("usr_one", "mdl_one", source) {
		t.Fatal("disabled Model retained publication proof")
	}
	data.Models[0].Status = "active"
	publish()
	s.InvalidateRuntimeUser("usr_one")
	if s.RuntimePersonalModelGrantApplied("usr_one", "mdl_one", source) {
		t.Fatal("user tombstone was ignored")
	}
	s.runtime.deniedUsers.Delete("usr_one")
	s.InvalidateRuntimeModel("mdl_one")
	if s.RuntimePersonalModelGrantApplied("usr_one", "mdl_one", source) {
		t.Fatal("Model tombstone was ignored")
	}
	s.runtime.deniedModels.Delete("mdl_one")
	auth := *s.runtime.auth.Load()
	auth.ValidUntil = time.Now().Add(-time.Second)
	s.runtime.auth.Store(&auth)
	if s.RuntimePersonalModelGrantApplied("usr_one", "mdl_one", source) {
		t.Fatal("expired lease was reported applied")
	}
	if (&Service{}).RuntimePersonalModelGrantApplied("usr_one", "mdl_one", source) {
		t.Fatal("missing runtime was reported applied")
	}
}
