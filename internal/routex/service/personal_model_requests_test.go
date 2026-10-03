package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

const personalTestUser = "usr_01arz3ndektsv4rrffq69g5fav"
const personalTestModel = "mdl_01arz3ndektsv4rrffq69g5fav"
const personalTestRequest = "mar_01arz3ndektsv4rrffq69g5fav"
const personalTestIntent = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

func TestPersonalModelInputsPreserveExactIntent(t *testing.T) {
	for _, raw := range []string{
		`null`, `[]`, `{}`, `{"request_id":"x","model_id":"y","reason":null}`,
		`{"request_id":"x","model_id":1,"reason":"why"}`,
		`{"request_id":"x","model_id":"y","reason":"why","provider_id":"private"}`,
		`{"request_id":"x","request_id":"z","model_id":"y","reason":"why"}`,
		`{"request_id":"x","model_id":"y","reason":"why","reason":"different"}`,
		"{\"request_id\":\"x\",\"model_id\":\"y\",\"reason\":\"\xff\"}",
	} {
		var input PersonalModelRequestInput
		if json.Unmarshal([]byte(raw), &input) == nil {
			t.Fatalf("accepted changed or discarded creation intent: %q", raw)
		}
	}
	for _, raw := range []string{
		`null`, `{}`, `{"decision_id":"x","action":"approve","reason":null}`,
		`{"decision_id":"x","action":"approve","action":"reject"}`,
		`{"decision_id":"x","action":"approve","model_id":"other"}`,
		`{"decision_id":"x","action":true}`,
	} {
		var input PersonalModelDecisionInput
		if json.Unmarshal([]byte(raw), &input) == nil {
			t.Fatalf("accepted changed or discarded decision intent: %q", raw)
		}
	}
	var request PersonalModelRequestInput
	if err := json.Unmarshal([]byte(`{"request_id":"id","model_id":"model","reason":"  需要模型 accès  "}`), &request); err != nil || request.Reason != "  需要模型 accès  " {
		t.Fatal("valid string intent was changed", request, err)
	}
	var decision PersonalModelDecisionInput
	if err := json.Unmarshal([]byte(`{"decision_id":"id","action":"approve"}`), &decision); err != nil || decision.Reason != "" {
		t.Fatal("optional reason gained an invented value", decision, err)
	}
}

func TestPersonalModelIdentityAndValidatorRejectAliases(t *testing.T) {
	if !personalModelID(personalTestUser, "usr") || !personalModelID(personalTestModel, "mdl") || !personalModelID(personalTestRequest, "mar") {
		t.Fatal("canonical identity rejected")
	}
	for _, value := range []string{strings.ToUpper(personalTestUser), personalTestUser + " ", personalTestModel, "usr_81arz3ndektsv4rrffq69g5fav", "usr_01arz3ndektsv4rrffq69g5fa!", "usr_unknown", ""} {
		if personalModelID(value, "usr") {
			t.Fatal("aliased or malformed identity accepted", value)
		}
	}
	if !personalModelETag(strings.Repeat("0123456789abcdef", 4)) {
		t.Fatal("canonical review validator rejected")
	}
	for _, value := range []string{"", strings.Repeat("A", 64), strings.Repeat("g", 64), strings.Repeat("0", 63), strings.Repeat("0", 65), strings.Repeat("0", 63) + " "} {
		if personalModelETag(value) {
			t.Fatal("unreviewed validator accepted", value)
		}
	}
}

func TestPersonalModelReasonBoundsApplyBeforeNormalization(t *testing.T) {
	for _, reason := range []string{"", "   ", strings.Repeat(" ", 1025) + "why", strings.Repeat("界", 342), "why\n", "\twhy", "why\x00", string([]byte{0xff})} {
		s := &Service{}
		_, _, err := s.CreatePersonalModelRequest(context.Background(), personalTestUser, strings.Repeat("a", 64), PersonalModelRequestInput{RequestID: personalTestIntent, ModelID: personalTestModel, Reason: reason})
		if err != apperrors.ErrBadRequest {
			t.Fatalf("invalid reason reached persistence: %q, %v", reason, err)
		}
		_, err = s.DecidePersonalModelRequest(context.Background(), personalTestUser, personalTestUser, personalTestRequest, strings.Repeat("a", 64), PersonalModelDecisionInput{DecisionID: personalTestIntent, Action: "reject", Reason: reason}, true)
		if err != apperrors.ErrBadRequest {
			t.Fatalf("invalid decision reason reached persistence: %q, %v", reason, err)
		}
	}
	if !personalModelReason(strings.Repeat("界", 341)+"x", true) || personalModelReason(strings.Repeat("界", 341)+"xx", true) || !personalModelReason("", false) {
		t.Fatal("reason UTF-8 byte boundary or optional presence changed")
	}
}

func TestPersonalModelFrozenHashesBindEveryAuthorizedIntentField(t *testing.T) {
	etag := strings.Repeat("a", 64)
	input := PersonalModelRequestInput{RequestID: personalTestIntent, ModelID: personalTestModel, Reason: "why"}
	base := personalCreationHash(personalTestUser, etag, input)
	for _, candidate := range []string{
		personalCreationHash("usr_other", etag, input),
		personalCreationHash(personalTestUser, strings.Repeat("b", 64), input),
		personalCreationHash(personalTestUser, etag, PersonalModelRequestInput{RequestID: "other", ModelID: input.ModelID, Reason: input.Reason}),
		personalCreationHash(personalTestUser, etag, PersonalModelRequestInput{RequestID: input.RequestID, ModelID: "mdl_other", Reason: input.Reason}),
		personalCreationHash(personalTestUser, etag, PersonalModelRequestInput{RequestID: input.RequestID, ModelID: input.ModelID, Reason: "different"}),
	} {
		if base == candidate {
			t.Fatal("creation receipt can be reused for another intent")
		}
	}
	decision := PersonalModelDecisionInput{DecisionID: personalTestIntent, Action: "approve", Reason: "reviewed"}
	base = personalDecisionHash(personalTestUser, personalTestRequest, etag, decision)
	for _, candidate := range []string{
		personalDecisionHash("usr_other", personalTestRequest, etag, decision),
		personalDecisionHash(personalTestUser, "mar_other", etag, decision),
		personalDecisionHash(personalTestUser, personalTestRequest, strings.Repeat("b", 64), decision),
		personalDecisionHash(personalTestUser, personalTestRequest, etag, PersonalModelDecisionInput{DecisionID: "other", Action: decision.Action, Reason: decision.Reason}),
		personalDecisionHash(personalTestUser, personalTestRequest, etag, PersonalModelDecisionInput{DecisionID: decision.DecisionID, Action: "reject", Reason: decision.Reason}),
		personalDecisionHash(personalTestUser, personalTestRequest, etag, PersonalModelDecisionInput{DecisionID: decision.DecisionID, Action: decision.Action, Reason: "different"}),
	} {
		if base == candidate {
			t.Fatal("decision receipt can be reused for another actor, target or intent")
		}
	}
}

func TestPersonalModelCursorCannotCrossActorTargetOrFilter(t *testing.T) {
	scope := personalTestUser + ":" + personalTestUser + ":pending"
	value := personalModelCursor{Scope: scope, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), ID: personalTestRequest}
	raw := encodePersonalModelCursor(value)
	if decoded, err := decodePersonalModelCursor(raw, scope); err != nil || !reflect.DeepEqual(*decoded, value) {
		t.Fatal("canonical page cursor rejected", decoded, err)
	}
	for _, other := range []string{"usr_other:" + personalTestUser + ":pending", personalTestUser + ":usr_other:pending", personalTestUser + ":" + personalTestUser + ":approved"} {
		if _, err := decodePersonalModelCursor(raw, other); err == nil {
			t.Fatal("cursor crossed an authority/filter boundary", other)
		}
	}
	for _, invalid := range []string{"!", strings.Repeat("a", 513), base64.RawURLEncoding.EncodeToString([]byte(`null`)), encodePersonalModelCursor(personalModelCursor{Scope: scope, ID: value.ID}), encodePersonalModelCursor(personalModelCursor{Scope: scope, CreatedAt: value.CreatedAt, ID: strings.ToUpper(value.ID)})} {
		if _, err := decodePersonalModelCursor(invalid, scope); err == nil {
			t.Fatal("malformed cursor accepted", invalid)
		}
	}
}

func TestPersonalModelAllowedActionsKeepInactiveRequestsResolvable(t *testing.T) {
	row := entity.PersonalModelRequest{ID: personalTestRequest, ApplicantUserID: personalTestUser, ModelID: personalTestModel, Status: entity.PersonalModelRequestPending}
	subject := &personalModelSubject{User: entity.User{ID: personalTestUser}, Model: entity.Model{ID: personalTestModel, Status: entity.ResourceActive}, Name: entity.ModelName{Name: "Current"}, Pending: &entity.PersonalModelRequestPendingSlot{RequestID: row.ID}}
	reviewer := "usr_01arz3ndektsv4rrffq69g5faw"
	if got := personalModelAllowed(reviewer, row, subject, true); !reflect.DeepEqual(got, []string{"approve", "reject"}) {
		t.Fatal("eligible independent reviewer actions", got)
	}
	if got := personalModelAllowed(personalTestUser, row, subject, true); len(got) != 0 {
		t.Fatal("self-review exposed a terminal action", got)
	}
	for _, status := range []string{entity.ResourceDisabled, entity.ResourceArchived} {
		subject.Model.Status = status
		if got := personalModelAllowed(reviewer, row, subject, true); !reflect.DeepEqual(got, []string{"reject"}) {
			t.Fatal("inactive Model cannot be rejected or can be approved", got)
		}
		if got := personalModelAllowed(personalTestUser, row, subject, false); !reflect.DeepEqual(got, []string{"withdraw"}) {
			t.Fatal("inactive Model trapped own pending request", got)
		}
	}
	subject.User.Disabled = true
	if len(personalModelAllowed(reviewer, row, subject, true)) != 0 {
		t.Fatal("disabled target allowed a fresh decision")
	}
	subject.User.Disabled = false
	subject.Pending.RequestID = "mar_other"
	if len(personalModelAllowed(personalTestUser, row, subject, false)) != 0 {
		t.Fatal("another pending slot borrowed the request")
	}
	subject.Pending.RequestID = row.ID
	subject.User.ID = strings.ToUpper(personalTestUser)
	if len(personalModelAllowed(reviewer, row, subject, true)) != 0 {
		t.Fatal("aliased target borrowed request authority")
	}
	subject.User.ID = personalTestUser
	row.Status = entity.PersonalModelRequestApproved
	if len(personalModelAllowed(reviewer, row, subject, true)) != 0 || len(personalModelAllowed(reviewer, row, nil, true)) != 0 {
		t.Fatal("terminal/missing subject exposed another decision")
	}
}
