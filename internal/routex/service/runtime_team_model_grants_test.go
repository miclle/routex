package service

import (
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestTeamModelGrantPublicationRetainsSharedOriginalSource(t *testing.T) {
	s, data, _, _ := teamSessionFixture(t)
	source := "tmr_original"
	data.TeamSessionData.Grants[0].SourceRequestID = &source
	publish := func() { s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute))) }
	applied := func() bool { return s.RuntimeTeamModelGrantApplied("tem_one", "mdl_one", source) }
	publish()
	if !applied() || s.RuntimeTeamModelGrantApplied("tem_one", "mdl_one", "tmr_other") || s.RuntimeTeamModelGrantApplied("TEM_one", "mdl_one", source) {
		t.Fatal("source or exact Team scope was not preserved")
	}
	// Shared approval survives the applicant leaving; it is not membership proof.
	data.TeamSessionData.Memberships = nil
	data.Users[0].Disabled = true
	publish()
	if !applied() {
		t.Fatal("applicant departure revoked the shared approval provenance")
	}
	data.TeamSessionData.Grants[0].SourceRequestID = nil
	publish()
	if applied() {
		t.Fatal("ordinary re-added grant borrowed historical receipt")
	}
	data.TeamSessionData.Grants[0].SourceRequestID = &source
	data.TeamSessionData.Teams[0].Status = entity.ResourceDisabled
	publish()
	if applied() {
		t.Fatal("disabled Team reported applied")
	}
	data.TeamSessionData.Teams[0].Status = entity.ResourceActive
	data.Models[0].Status = entity.ResourceDisabled
	publish()
	if applied() {
		t.Fatal("disabled Model reported applied")
	}
	data.Models[0].Status = entity.ResourceActive
	publish()
	s.invalidateRuntimeTeam("tem_one")
	if applied() {
		t.Fatal("Team tombstone ignored")
	}
	s.runtime.deniedTeams.Delete("tem_one")
	s.InvalidateRuntimeModel("mdl_one")
	if applied() {
		t.Fatal("Model tombstone ignored")
	}
	s.runtime.deniedModels.Delete("mdl_one")
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(-time.Second)))
	if applied() {
		t.Fatal("expired lease reported applied")
	}
	if (&Service{}).RuntimeTeamModelGrantApplied("tem_one", "mdl_one", source) {
		t.Fatal("missing runtime reported applied")
	}
}
