package service

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

const teamModelTestRequest = "tmr_01arz3ndektsv4rrffq69g5fav"

func teamModelTestSubject() *teamModelSubject {
	return &teamModelSubject{Team: entity.Team{ID: "tem_legacy", Name: "Team", Status: entity.ResourceActive}, User: &entity.User{ID: "usr_legacy"}, Member: &entity.TeamMembership{ID: "tmm_original", TeamID: "tem_legacy", UserID: "usr_legacy", Status: entity.ResourceActive, Role: entity.TeamMember}, Model: &entity.Model{ID: "mdl_legacy", Status: entity.ResourceActive}, Name: &entity.ModelName{Name: "public-model"}, Pending: &entity.TeamModelRequestPendingSlot{TeamID: "tem_legacy", ModelID: "mdl_legacy", RequestID: teamModelTestRequest}}
}
func teamModelTestRow() entity.TeamModelRequest {
	return entity.TeamModelRequest{ID: teamModelTestRequest, TeamID: "tem_legacy", ApplicantUserID: "usr_legacy", ApplicantMembershipID: "tmm_original", ModelID: "mdl_legacy", Status: entity.TeamModelRequestPending}
}
func TestTeamModelRequestStrictIntentAndRawReasonBounds(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `[]`, `{"request_id":"x","team_id":"one","team_id":"two","model_id":"model","reason":"why"}`, `{"request_id":"x","team_id":"team","model_id":"model","reason":null}`, `{"request_id":"x","team_id":"team","model_id":"model","reason":"why","provider":"private"}`, "{\"request_id\":\"x\",\"team_id\":\"team\",\"model_id\":\"model\",\"reason\":\"\xff\"}"} {
		var input TeamModelRequestInput
		if json.Unmarshal([]byte(raw), &input) == nil {
			t.Fatal("discarded/altered Team creation intent accepted", raw)
		}
	}
	for _, raw := range []string{`null`, `{}`, `{"decision_id":"x","action":"approve","action":"reject"}`, `{"decision_id":"x","action":"approve","reason":null}`, `{"decision_id":"x","action":"approve","team_id":"other"}`} {
		var input TeamModelDecisionInput
		if json.Unmarshal([]byte(raw), &input) == nil {
			t.Fatal("discarded/altered Team decision intent accepted", raw)
		}
	}
	for _, reason := range []string{"", "   ", strings.Repeat(" ", 1025) + "why", strings.Repeat("界", 342), "\twhy", "why\n", string([]byte{0xff})} {
		_, _, err := (&Service{}).CreateTeamModelRequest(context.Background(), "usr_legacy", strings.Repeat("a", 64), TeamModelRequestInput{RequestID: personalTestIntent, TeamID: "tem_legacy", ModelID: "mdl_legacy", Reason: reason})
		if err != apperrors.ErrBadRequest {
			t.Fatal("invalid raw reason reached persistence", reason, err)
		}
	}
	var input TeamModelRequestInput
	if err := json.Unmarshal([]byte(`{"request_id":"id","team_id":"tem_legacy","model_id":"mdl_legacy","reason":"  中文 reason  "}`), &input); err != nil || input.Reason != "  中文 reason  " {
		t.Fatal("valid exact input changed", input, err)
	}
}
func TestTeamModelRequestReviewRejectsSelfAndReplacedMembership(t *testing.T) {
	row, subject := teamModelTestRow(), teamModelTestSubject()
	if !reflect.DeepEqual(teamModelAllowed("usr_reviewer", row, subject, true), []string{"approve", "reject"}) {
		t.Fatal("exact eligible Team request cannot be independently reviewed")
	}
	if len(teamModelAllowed(row.ApplicantUserID, row, subject, true)) != 0 {
		t.Fatal("Team role action union permitted self-review")
	}
	if !reflect.DeepEqual(teamModelAllowed(row.ApplicantUserID, row, nil, false), []string{"withdraw"}) {
		t.Fatal("recorded applicant needs Team directory authority to withdraw")
	}
	for _, mutate := range []func(*teamModelSubject){
		func(s *teamModelSubject) { s.Member.ID = "tmm_rejoined" },
		func(s *teamModelSubject) { s.Member.UserID = "USR_LEGACY" },
		func(s *teamModelSubject) { s.Member.TeamID = "TEM_LEGACY" },
		func(s *teamModelSubject) { s.Member.Status = entity.ResourceDisabled },
		func(s *teamModelSubject) { s.User.Disabled = true },
		func(s *teamModelSubject) { now := time.Now(); s.User.OffboardedAt = &now },
		func(s *teamModelSubject) { s.Team.Status = entity.ResourceDisabled },
		func(s *teamModelSubject) { s.Model.Status = entity.ResourceDisabled },
		func(s *teamModelSubject) { s.Model = nil; s.Name = nil },
	} {
		subject = teamModelTestSubject()
		mutate(subject)
		if !reflect.DeepEqual(teamModelAllowed("usr_reviewer", row, subject, true), []string{"reject"}) {
			t.Fatal("ineligible generation still permits approval or traps rejection", subject)
		}
	}
	subject = teamModelTestSubject()
	subject.Pending.RequestID = "tmr_other"
	if len(teamModelAllowed("usr_reviewer", row, subject, true)) != 0 {
		t.Fatal("another shared pending slot borrowed the reviewed request")
	}
	row.Status = entity.TeamModelRequestApproved
	if len(teamModelAllowed(row.ApplicantUserID, row, subject, false)) != 0 {
		t.Fatal("approved shared grant can be withdrawn after commitment")
	}
}
func TestTeamModelRequestValidatorsBindGenerationWithoutPrivateAccountFacts(t *testing.T) {
	subject := teamModelTestSubject()
	before := teamModelCandidateETag("usr_legacy", subject)
	subject.User.PasswordHash, subject.User.Email, subject.User.Name = "private-proof", "private@example.invalid", "Private name"
	if teamModelCandidateETag("usr_legacy", subject) != before {
		t.Fatal("public validator incorporates unrelated private account fields")
	}
	subject.Member.ID = "tmm_rejoined"
	if teamModelCandidateETag("usr_legacy", subject) == before {
		t.Fatal("membership rejoin reused old reviewed candidate")
	}
	subject = teamModelTestSubject()
	if teamModelCandidateETag("usr_other", subject) == before {
		t.Fatal("candidate review borrowed another actor")
	}
	source := teamModelTestRequest
	subject.Grant = &entity.TeamModelGrant{TeamID: subject.Team.ID, ModelID: subject.Model.ID, SourceRequestID: &source}
	before = teamModelCandidateETag("usr_legacy", subject)
	subject.Grant.SourceRequestID = nil
	if teamModelCandidateETag("usr_legacy", subject) == before {
		t.Fatal("ordinary readd borrowed request grant provenance")
	}
}
func TestTeamModelKnownReceiptIsExactAndDoesNotRewriteSavedDecision(t *testing.T) {
	input := TeamModelDecisionInput{DecisionID: personalTestIntent, Action: "approve", Reason: "reviewed"}
	actor, etag := "usr_reviewer", strings.Repeat("a", 64)
	hash := teamModelDecisionHash(actor, teamModelTestRequest, etag, input)
	row := teamModelTestRow()
	row.Status, row.DecisionAction, row.DecisionReason = entity.TeamModelRequestApproved, input.Action, input.Reason
	row.DecisionID, row.DecisionActorID, row.DecisionRequestHash = &input.DecisionID, &actor, hash
	before := row
	if !teamModelKnownDecision(row, actor, hash, input) {
		t.Fatal("original frozen receipt rejected")
	}
	for _, other := range []TeamModelDecisionInput{{DecisionID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Action: input.Action, Reason: input.Reason}, {DecisionID: input.DecisionID, Action: "reject", Reason: input.Reason}, {DecisionID: input.DecisionID, Action: input.Action, Reason: "different"}} {
		if teamModelKnownDecision(row, actor, teamModelDecisionHash(actor, row.ID, etag, other), other) {
			t.Fatal("historical receipt accepted an altered intent", other)
		}
	}
	if teamModelKnownDecision(row, "usr_other", hash, input) || teamModelKnownDecision(row, actor, teamModelDecisionHash(actor, row.ID, strings.Repeat("b", 64), input), input) || !reflect.DeepEqual(row, before) {
		t.Fatal("receipt borrowed actor/validator or rewrote historical decision")
	}
}

func TestTeamModelKnownCreationKeepsHistoricalIntentWithoutCurrentMembership(t *testing.T) {
	actor, etag := "usr_legacy", strings.Repeat("a", 64)
	input := TeamModelRequestInput{RequestID: personalTestIntent, TeamID: "tem_legacy", ModelID: "mdl_legacy", Reason: "reviewed"}
	row := teamModelTestRow()
	row.RequestID, row.RequestHash = input.RequestID, teamModelCreationHash(actor, etag, input)
	for _, status := range []string{entity.TeamModelRequestPending, entity.TeamModelRequestApproved, entity.TeamModelRequestCancelled} {
		row.Status = status
		before := row
		if !teamModelKnownCreation(row, actor, teamModelCreationHash(actor, etag, input), input) || !reflect.DeepEqual(row, before) {
			t.Fatal("saved creation depends on current membership or changes historical state", status)
		}
	}
	for _, change := range []func(*TeamModelRequestInput){
		func(i *TeamModelRequestInput) { i.RequestID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb" },
		func(i *TeamModelRequestInput) { i.TeamID = "tem_other" },
		func(i *TeamModelRequestInput) { i.ModelID = "mdl_other" },
		func(i *TeamModelRequestInput) { i.Reason = "altered intent" },
	} {
		other := input
		change(&other)
		if teamModelKnownCreation(row, actor, teamModelCreationHash(actor, etag, other), other) {
			t.Fatal("saved creation accepted a changed target or intent", other)
		}
	}
	if teamModelKnownCreation(row, "USR_LEGACY", row.RequestHash, input) || teamModelKnownCreation(row, actor, teamModelCreationHash(actor, strings.Repeat("b", 64), input), input) {
		t.Fatal("saved creation borrowed actor alias or another reviewed validator")
	}
}

func TestTeamModelRequestCursorBindsOwnReviewTargetAndFilters(t *testing.T) {
	filter := TeamModelRequestFilter{TeamID: "tem_legacy", ModelID: "mdl_legacy", Status: "pending"}
	scope := teamModelCursorScope("usr_legacy", "", filter, false)
	cursor := teamModelRequestCursor{Scope: scope, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), ID: teamModelTestRequest}
	raw := encodeTeamModelRequestCursor(cursor)
	if got, err := decodeTeamModelRequestCursor(raw, scope); err != nil || !reflect.DeepEqual(*got, cursor) {
		t.Fatal("valid Team request cursor rejected", got, err)
	}
	for _, other := range []string{teamModelCursorScope("usr_other", "", filter, false), teamModelCursorScope("usr_legacy", "tem_legacy", filter, true), teamModelCursorScope("usr_legacy", "", TeamModelRequestFilter{TeamID: "tem_other", ModelID: filter.ModelID, Status: filter.Status}, false), teamModelCursorScope("usr_legacy", "", TeamModelRequestFilter{TeamID: filter.TeamID, ModelID: "mdl_other", Status: filter.Status}, false), teamModelCursorScope("usr_legacy", "", TeamModelRequestFilter{TeamID: filter.TeamID, ModelID: filter.ModelID, Status: "approved"}, false)} {
		if _, err := decodeTeamModelRequestCursor(raw, other); err == nil {
			t.Fatal("cursor crossed actor/Team/model/view/status scope")
		}
	}
}
func TestTeamModelRequestQueryKeepsExactHistoricalOwnerScope(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
		if err != nil {
			t.Fatal(err)
		}
		query := teamModelRequestQuery(db, "usr_left", "", TeamModelRequestFilter{TeamID: "tem_archived", ModelID: "mdl_legacy", Status: "approved"}, false)
		query.Statement.Clauses["WHERE"].Build(query.Statement)
		if !reflect.DeepEqual(query.Statement.Vars, []any{"usr_left", "tem_archived", "mdl_legacy", "approved"}) || strings.Contains(query.Statement.SQL.String(), " OR ") || strings.Contains(query.Statement.SQL.String(), "team_memberships") {
			t.Fatal("historical owner view borrowed another applicant or required current directory", query.Statement.SQL.String(), query.Statement.Vars)
		}
		if dialect.Name() == "mysql" && strings.Count(query.Statement.SQL.String(), "AS BINARY") != 8 {
			t.Fatal("historical owner filters lost exact MySQL identity", query.Statement.SQL.String())
		}
	}
}
