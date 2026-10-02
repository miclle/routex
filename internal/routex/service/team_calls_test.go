package service

import (
	"testing"
	"time"
)

func TestCallTeamAttributionIsSeparate(t *testing.T) {
	now := time.Now()
	base := CallFact{RequestID: "req_team", UserID: "usr_actor", TeamID: "tem_history", TeamMembershipID: "tmm_removed", Protocol: "openai_chat", Status: "success", StartedAt: now, CompletedAt: now}
	if err := validateCallFact(base); err != nil {
		t.Fatal("content-free Team actor history rejected", err)
	}
	for _, mutate := range []func(*CallFact){
		func(f *CallFact) { f.KeyID = "key_personal" },
		func(f *CallFact) { f.ProjectID = "prj_other" },
		func(f *CallFact) { f.UserID = "" },
		func(f *CallFact) { f.TeamMembershipID = "" },
		func(f *CallFact) { f.TeamID = "" },
		func(f *CallFact) { f.TeamID = "../team" },
	} {
		fact := base
		mutate(&fact)
		if err := validateCallFact(fact); err == nil {
			t.Fatal("contradictory or malformed Team subject accepted", fact)
		}
	}
}
