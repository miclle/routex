package service

import (
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestPersonalModelGrantPublicationRequiresOriginalSource(t *testing.T) {
	source := "mar_original"
	data := &runtimeData{Users: []entity.User{{ID: "usr_one"}}, Models: []entity.Model{{ID: "mdl_one", Status: "active"}}, Grants: []entity.UserModelGrant{{UserID: "usr_one", ModelID: "mdl_one", SourceRequestID: &source}}}
	s := &Service{runtime: &gatewayRuntime{}}
	publish := func() { s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute))) }
	publish()
	if !s.RuntimePersonalModelGrantApplied("usr_one", "mdl_one", source) {
		t.Fatal("original source was not published")
	}
	if s.RuntimePersonalModelGrantApplied("usr_one", "mdl_one", "mar_other") {
		t.Fatal("different receipt borrowed original source")
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
