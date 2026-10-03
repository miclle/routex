package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func TestModelAccessCandidateInputHasLiteralBoundedSearch(t *testing.T) {
	pattern, err := candidatePattern("  A%_!B  ")
	if err != nil || pattern != "%a!%!_!!b%" {
		t.Fatal("candidate search expanded literal wildcard input", pattern, err)
	}
	for _, filter := range []ModelAccessCandidateFilter{{Query: strings.Repeat("a", 201)}, {Query: string([]byte{0xff})}, {Limit: 51}, {Limit: -1}, {Cursor: strings.ToUpper(personalTestModel)}, {Cursor: personalTestModel + " "}} {
		if _, err := (&Service{}).ListModelAccessCandidates(context.Background(), personalTestUser, filter); err != apperrors.ErrBadRequest {
			t.Fatal("invalid candidate input reached persistence", filter, err)
		}
	}
	if _, err := (&Service{}).GetModelAccessCandidate(context.Background(), personalTestUser, strings.ToUpper(personalTestModel)); err != apperrors.ErrBadRequest {
		t.Fatal("aliased candidate lookup reached persistence", err)
	}
}

func TestPersonalModelCandidateValidatorDetectsEligibilityAndProvenanceChanges(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	original := personalModelSubject{User: entity.User{ID: personalTestUser, CreatedAt: now, UpdatedAt: now}, Model: entity.Model{ID: personalTestModel, Status: entity.ResourceActive, CreatedAt: now}, Name: entity.ModelName{Name: "Current", CreatedAt: now}}
	base := personalModelCandidateETag(&original)
	cases := []personalModelSubject{original, original, original, original, original, original, original}
	cases[0].User.Disabled = true
	cases[1].User.OffboardedAt = &now
	cases[2].Model.Status = entity.ResourceDisabled
	cases[3].Name.Name = "Renamed"
	cases[4].Grant = &entity.UserModelGrant{CreatedAt: now}
	cases[5].Pending = &entity.PersonalModelRequestPendingSlot{RequestID: personalTestRequest}
	cases[6].User.UpdatedAt = now.Add(time.Microsecond)
	for _, candidate := range cases {
		if personalModelCandidateETag(&candidate) == base {
			t.Fatal("current eligibility change reused reviewed validator", candidate)
		}
	}
	source := personalTestRequest
	original.Grant = &entity.UserModelGrant{CreatedAt: now, SourceRequestID: &source}
	base = personalModelCandidateETag(&original)
	copy := original
	copy.Grant = &entity.UserModelGrant{CreatedAt: now}
	if personalModelCandidateETag(&copy) == base {
		t.Fatal("ordinary grant can reuse original request grant validator")
	}
}

func TestModelAccessCandidateAndReviewWorkspaceExposeOnlyPurposeFields(t *testing.T) {
	values := []struct {
		value any
		keys  []string
	}{
		{ModelAccessCandidate{}, []string{"id", "name", "status", "created_at", "protocols", "input_capabilities", "personal_granted", "pending_request_id", "review_etag"}},
		{MemberModelAccessWorkspace{}, []string{"user_id", "models", "model_count", "can_review_requests"}},
	}
	for _, v := range values {
		data, err := json.Marshal(v.value)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		if len(fields) != len(v.keys) {
			t.Fatal("purpose API exposed extra catalog or member facts", string(data))
		}
		for _, key := range v.keys {
			if _, ok := fields[key]; !ok {
				t.Fatal("purpose API lost its declared field", key)
			}
		}
	}
}
