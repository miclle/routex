package service

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func TestTeamCreationModelsStrictReviewAndLegacyHash(t *testing.T) {
	old := teamCreationTestInput()
	old, _ = normalizeTeamCreationInput(old)
	tag := teamCreationHash("usr_actor", old)
	raw, _ := json.Marshal(old)
	var decoded TeamCreationInput
	if json.Unmarshal(raw, &decoded) != nil {
		t.Fatal("old body rejected")
	}
	decoded.ReviewETag = old.ReviewETag
	decoded, _ = normalizeTeamCreationInput(decoded)
	if teamCreationHash("usr_actor", decoded) != tag {
		t.Fatal("legacy hash changed")
	}
	decoded.ModelsPresent = true
	decoded.ModelIDs = []string{}
	if teamCreationHash("usr_actor", decoded) != tag {
		t.Fatal("empty did not preserve legacy hash")
	}
	decoded.ModelIDs = []string{"mdl_two", "mdl_one"}
	decoded.ModelReviewToken = strings.Repeat("a", 64)
	normalized, err := normalizeTeamCreationInput(decoded)
	if err != nil || !slices.Equal(normalized.ModelIDs, []string{"mdl_one", "mdl_two"}) {
		t.Fatal("selected normalization", err)
	}
	if teamCreationHash("usr_actor", normalized) == tag {
		t.Fatal("Models not bound")
	}
	again := normalized
	again.ModelReviewToken = strings.Repeat("b", 64)
	if teamCreationHash("usr_actor", again) == teamCreationHash("usr_actor", normalized) {
		t.Fatal("token not bound")
	}
	decoded.ModelIDs[0] = "mdl_changed"
	if normalized.ModelIDs[0] != "mdl_one" {
		t.Fatal("normalized selection borrowed source array")
	}
	for _, body := range []string{`{}`, `{"model_ids":null}`, `{"model_ids":[]}`, `{"model_ids":["mdl_one","mdl_one"]}`, `{"model_ids":["mdl_one " ]}`, `{"model_ids":["mdl_one"],"x":1}`, `{"model_ids":["mdl_one"],"model_ids":["mdl_two"]}`, `{"model_ids":["mdl_one"]} {}`} {
		var in TeamCreationModelReviewInput
		if in.UnmarshalJSON([]byte(body)) != apperrors.ErrBadRequest {
			t.Fatal("invalid selected review admitted", body)
		}
	}
	var review TeamCreationModelReviewInput
	if review.UnmarshalJSON([]byte(`{"model_ids":["mdl_two","mdl_one"]}`)) != nil || !slices.Equal(review.ModelIDs, []string{"mdl_one", "mdl_two"}) {
		t.Fatal("valid complete review lost")
	}
}
func TestTeamCreationModelReviewTokenIdentityAndSubsetBinding(t *testing.T) {
	born := time.Now().UTC().Truncate(time.Microsecond)
	review := &teamCreationReview{Actor: entity.User{ID: "usr_actor", CreatedAt: born}, Public: TeamCreationContext{ReviewETag: strings.Repeat("a", 64), CanSetModels: true}}
	models := []entity.Model{{ID: "mdl_original", Status: entity.ResourceActive, CreatedAt: born}}
	proofs := []string{strings.Repeat("b", 64)}
	tag := teamCreationModelReviewHash(review, models, proofs)
	if !teamSessionDigest.MatchString(tag) {
		t.Fatal("token grammar")
	}
	for name, mutate := range map[string]func(*teamCreationReview, []entity.Model, []string){
		"actor": func(r *teamCreationReview, _ []entity.Model, _ []string) { r.Actor.ID = "usr_other" },
		"actor birth": func(r *teamCreationReview, _ []entity.Model, _ []string) {
			r.Actor.CreatedAt = r.Actor.CreatedAt.Add(time.Millisecond)
		},
		"context": func(r *teamCreationReview, _ []entity.Model, _ []string) {
			r.Public.ReviewETag = strings.Repeat("c", 64)
		},
		"Model birth": func(_ *teamCreationReview, m []entity.Model, _ []string) {
			m[0].CreatedAt = m[0].CreatedAt.Add(time.Millisecond)
		},
		"eligibility": func(_ *teamCreationReview, _ []entity.Model, p []string) { p[0] = strings.Repeat("d", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			r := *review
			m := slices.Clone(models)
			p := slices.Clone(proofs)
			mutate(&r, m, p)
			if teamCreationModelReviewHash(&r, m, p) == tag {
				t.Fatal("review not bound")
			}
		})
	}
	local := slices.Clone(models)
	local[0].CreatedAt = born.In(time.FixedZone("local", 3600))
	if teamCreationModelReviewHash(review, local, proofs) != tag {
		t.Fatal("time location changed same incarnation")
	}
}
func TestTeamCreationPublishedModelsCompleteOriginalProvenanceNotAvailability(t *testing.T) {
	born := time.Now().UTC().Truncate(time.Microsecond)
	source := "11111111-1111-4111-8111-111111111166"
	children := []entity.TeamCreationReceiptModel{{CreationID: source, ModelID: "mdl_original", ModelCreatedAt: born}}
	digest := teamCreationModelsDigest(children)
	receipt := entity.TeamCreationReceipt{CreationID: source, TeamID: "tea_original", ModelSnapshotVersion: 1, ModelCount: 1, ModelDigest: &digest}
	auth := &runtimeAuthorization{Models: map[string]bool{"mdl_original": false}, TeamCreationGrants: map[string]map[string]runtimeTeamCreationGrant{"tea_original": {"mdl_original": {ModelCreatedAt: born, CreationID: source}}}}
	if !teamCreationPublishedModelsMatch(auth, receipt, children) {
		t.Fatal("disabled routing erased configured grant proof")
	}
	tests := map[string]func(*runtimeAuthorization){
		"missing projection": func(a *runtimeAuthorization) { delete(a.TeamCreationGrants, "tea_original") },
		"extra inactive": func(a *runtimeAuthorization) {
			a.TeamCreationGrants["tea_original"]["mdl_extra"] = runtimeTeamCreationGrant{ModelCreatedAt: born}
		},
		"ordinary readd": func(a *runtimeAuthorization) {
			a.TeamCreationGrants["tea_original"]["mdl_original"] = runtimeTeamCreationGrant{ModelCreatedAt: born}
		},
		"approval readd": func(a *runtimeAuthorization) {
			a.TeamCreationGrants["tea_original"]["mdl_original"] = runtimeTeamCreationGrant{ModelCreatedAt: born, RequestID: "tma_actual"}
		},
		"reincarnation": func(a *runtimeAuthorization) {
			a.TeamCreationGrants["tea_original"]["mdl_original"] = runtimeTeamCreationGrant{ModelCreatedAt: born.Add(time.Millisecond), CreationID: source}
		},
		"source alias": func(a *runtimeAuthorization) {
			a.TeamCreationGrants["tea_original"]["mdl_original"] = runtimeTeamCreationGrant{ModelCreatedAt: born, CreationID: source + " "}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			copy := *auth
			copy.TeamCreationGrants = map[string]map[string]runtimeTeamCreationGrant{"tea_original": {"mdl_original": auth.TeamCreationGrants["tea_original"]["mdl_original"]}}
			mutate(&copy)
			if teamCreationPublishedModelsMatch(&copy, receipt, children) {
				t.Fatal("incomplete or readded grant confirmed")
			}
		})
	}
	damaged := slices.Clone(children)
	damaged[0].ModelCreatedAt = born.Add(time.Millisecond)
	if teamCreationPublishedModelsMatch(auth, receipt, damaged) {
		t.Fatal("child digest corruption confirmed")
	}
}

func TestTeamCreationModelCommitAuditVersionIsPrivateAndLegacyStable(t *testing.T) {
	base := teamCreationCommitAudit{CreationID: "66000000-0000-4000-8000-000000000001", OwnerCount: 1, Fields: []string{}, Reason: ""}
	legacy, err := json.Marshal(base)
	if err != nil || string(legacy) != `{"creation_id":"66000000-0000-4000-8000-000000000001","owner_count":1,"submitted_fields":[],"reason":""}` {
		t.Fatal("historical empty commit shape changed", err)
	}
	digest := strings.Repeat("d", 64)
	base.ModelSnapshotVersion = 1
	base.ModelCount = 1
	base.ModelDigest = &digest
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || string(fields["model_snapshot_version"]) != "1" || string(fields["model_count"]) != "1" || string(fields["model_digest"]) != `"`+digest+`"` {
		t.Fatal("selected commit lacks explicit bounded version")
	}
	details := string(raw)
	record := auditRecord(entity.AuditEvent{Action: "team.creation.commit", DetailsJSON: &details})
	if len(record.Changes) != 0 {
		t.Fatal("private commit metadata exposed as arbitrary audit JSON")
	}
}
