package service

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/limits"
)

const projectCreationTestUUID = "20d23b8b-7022-4b92-8a19-378997bfaf92"

func projectCreationTestInput() ProjectCreationInput {
	managers := []string{"usr_other", "usr_applicant"}
	tokens, rpm := int64(0), int64(7)
	money := "12.340000000000000001"
	return ProjectCreationInput{Name: "Initial Project", Description: "Purpose", ManagerIDs: &managers, CreationID: projectCreationTestUUID, ReviewETag: strings.Repeat("a", 64), InitialResources: &ProjectInitialResources{ModelIDs: []string{"mdl_second", "mdl_first"}, TokensMonth: &tokens, MoneyMonth: &money, Currency: "USD", RPM: &rpm, Reason: "Reviewed initial resources"}}
}

func TestProjectCreationEnhancedStrictJSON(t *testing.T) {
	valid := `{"name":"Initial","description":"Purpose","manager_ids":["usr_member"],"creation_id":"` + projectCreationTestUUID + `","initial_request":{"model_ids":["mdl_one"],"tokens_month":0,"money_month":"1.25","currency":"USD","rpm":0,"tpm":1,"concurrency":2,"reason":"Initial request"}}`
	var input ProjectCreationInput
	if err := json.Unmarshal([]byte(valid), &input); err != nil || input.InitialRequest == nil || input.InitialRequest.TokensMonth == nil || *input.InitialRequest.TokensMonth != 0 || input.InitialRequest.MoneyMonth == nil || *input.InitialRequest.MoneyMonth != "1.25" {
		t.Fatal("initial sparse values were lost", input, err)
	}
	for _, body := range []string{
		`{"creation_id":null}`, `{"creation_id":""}`, `{"initial_request":null}`, `{"initial_resources":[]}`,
		`{"manager_ids":[null]}`,
		`{"creation_id":"a","creation_id":"b"}`, `{"review_etag":"client"}`,
		`{"initial_request":{"tokens_month":null}}`, `{"initial_request":{"money_month":1.2}}`,
		`{"initial_request":{"tokens_month":1.2}}`, `{"initial_request":{"rpm":9223372036854775808}}`,
		`{"initial_request":{"currency":null}}`, `{"initial_request":{"reason":null}}`,
		`{"initial_request":{"model_ids":null}}`, `{"initial_request":{"model_ids":[null]}}`,
		`{"initial_request":{"reason":"a","reason":"b"}}`, `{"initial_resources":{"rpm":1,"rpm":2}}`,
		`{"initial_request":{"quota":{"tokens_month":1}}}`, `{"initial_request":{"tokens_5h":1}}`,
		`{"initial_resources":{"ip_mode":"none"}}`, `{"initial_resources":{"manager_ids":[]}}`,
		`{"initial_request":{"reason":"a"} } {}`,
	} {
		var decoded ProjectCreationInput
		if json.Unmarshal([]byte(body), &decoded) == nil {
			t.Fatal("accepted malformed initial intent", body)
		}
	}
	for _, raw := range [][]byte{append([]byte(`{"name":"`), append([]byte{0xff}, []byte(`"}`)...)...), append([]byte(`{"initial_request":{"reason":"`), append([]byte{0xff}, []byte(`"}}`)...)...)} {
		if json.Unmarshal(raw, &input) == nil {
			t.Fatal("raw invalid UTF-8 changed frozen creation intent")
		}
	}
}

func TestProjectCreationLegacyEntryCannotDiscardEnhancedIntent(t *testing.T) {
	for _, input := range []ProjectCreationInput{
		{Name: "Legacy", CreationID: projectCreationTestUUID},
		{Name: "Legacy", ReviewETag: strings.Repeat("a", 64)},
		{Name: "Legacy", InitialResources: &ProjectInitialResources{}},
		{Name: "Legacy", InitialRequest: &ProjectInitialResources{}},
	} {
		if _, err := (&Service{}).CreateProject(context.Background(), "usr_creator", input); err != apperrors.ErrBadRequest {
			t.Fatal("legacy entry discarded enhanced intent", input, err)
		}
	}
}

func TestProjectCreationEnhancedNormalizationBoundsAndPresence(t *testing.T) {
	input := projectCreationTestInput()
	before, _ := json.Marshal(input)
	normalized, err := normalizeProjectCreationResources(input)
	if err != nil || !slices.Equal(*normalized.ManagerIDs, []string{"usr_applicant", "usr_other"}) || !slices.Equal(normalized.InitialResources.ModelIDs, []string{"mdl_first", "mdl_second"}) || *normalized.InitialResources.TokensMonth != 0 || *normalized.InitialResources.MoneyMonth != "12.340000000000000001" {
		t.Fatal("normalization lost reviewed sparse/exact values", normalized, err)
	}
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("normalization mutated submitted intent")
	}
	normalized.InitialResources.ModelIDs[0] = "mdl_changed"
	if input.InitialResources.ModelIDs[0] != "mdl_second" {
		t.Fatal("normalized intent borrowed model selection")
	}
	for name, change := range map[string]func(*ProjectCreationInput){
		"invalid_uuid":           func(v *ProjectCreationInput) { v.CreationID = "saved" },
		"uuid_alias":             func(v *ProjectCreationInput) { v.CreationID = strings.ToUpper(v.CreationID) },
		"wrong_uuid_version":     func(v *ProjectCreationInput) { v.CreationID = "20d23b8b-7022-1b92-8a19-378997bfaf92" },
		"etag_alias":             func(v *ProjectCreationInput) { v.ReviewETag = strings.Repeat("A", 64) },
		"missing_context":        func(v *ProjectCreationInput) { v.ReviewETag = "" },
		"mixed_modes":            func(v *ProjectCreationInput) { v.InitialRequest = v.InitialResources },
		"empty_resources":        func(v *ProjectCreationInput) { v.InitialResources = &ProjectInitialResources{Reason: "Reason"} },
		"duplicate_models":       func(v *ProjectCreationInput) { v.InitialResources.ModelIDs = []string{"mdl_one", "mdl_one"} },
		"wrong_subject":          func(v *ProjectCreationInput) { v.InitialResources.ModelIDs = []string{"usr_one"} },
		"model_space_alias":      func(v *ProjectCreationInput) { v.InitialResources.ModelIDs = []string{"mdl_one "} },
		"negative":               func(v *ProjectCreationInput) { n := int64(-1); v.InitialResources.TPM = &n },
		"oversized_integer":      func(v *ProjectCreationInput) { n := limits.MaxInteger + 1; v.InitialResources.Concurrency = &n },
		"currency_without_money": func(v *ProjectCreationInput) { v.InitialResources.MoneyMonth = nil },
		"money_without_currency": func(v *ProjectCreationInput) { v.InitialResources.Currency = "" },
		"nondecimal":             func(v *ProjectCreationInput) { n := "1e3"; v.InitialResources.MoneyMonth = &n },
		"negative_money":         func(v *ProjectCreationInput) { n := "-1"; v.InitialResources.MoneyMonth = &n },
		"empty_reason":           func(v *ProjectCreationInput) { v.InitialResources.Reason = " \t\n" },
		"invalid_reason":         func(v *ProjectCreationInput) { v.InitialResources.Reason = "x\x00" },
		"invalid_raw_reason":     func(v *ProjectCreationInput) { v.InitialResources.Reason = string([]byte{0xff}) },
		"reason_bounds":          func(v *ProjectCreationInput) { v.InitialResources.Reason = strings.Repeat("界", 2001) },
		"unsafe_manager":         func(v *ProjectCreationInput) { ids := []string{"usr_one "}; v.ManagerIDs = &ids },
	} {
		t.Run(name, func(t *testing.T) {
			v := projectCreationTestInput()
			change(&v)
			if _, err := normalizeProjectCreationResources(v); err != apperrors.ErrBadRequest {
				t.Fatal("invalid enhanced intent accepted", err)
			}
		})
	}
	plain := projectCreationTestInput()
	plain.InitialResources = nil
	if _, err := normalizeProjectCreationResources(plain); err != nil {
		t.Fatal("UUID-only creation requires no invented initial grant", err)
	}
	max := projectCreationTestInput()
	n := limits.MaxInteger
	max.InitialResources.RPM = &n
	max.InitialResources.Reason = strings.Repeat("界", 2000)
	if _, err := normalizeProjectCreationResources(max); err != nil {
		t.Fatal("valid integer and UTF-8 character bound rejected", err)
	}
}

func TestProjectCreationDirectPermissionsStayIndependent(t *testing.T) {
	n := int64(0)
	money := "0"
	for _, test := range []struct {
		models, policy bool
		resources      *ProjectInitialResources
		allowed        bool
	}{
		{false, false, nil, true}, {false, false, &ProjectInitialResources{ModelIDs: []string{"mdl_one"}}, false},
		{true, false, &ProjectInitialResources{ModelIDs: []string{"mdl_one"}}, true},
		{false, true, &ProjectInitialResources{TokensMonth: &n}, true},
		{true, false, &ProjectInitialResources{RPM: &n}, false},
		{false, true, &ProjectInitialResources{ModelIDs: []string{"mdl_one"}}, false},
		{true, false, &ProjectInitialResources{ModelIDs: []string{"mdl_one"}, TPM: &n}, false},
		{false, true, &ProjectInitialResources{ModelIDs: []string{"mdl_one"}, MoneyMonth: &money}, false},
		{true, true, &ProjectInitialResources{ModelIDs: []string{"mdl_one"}, Concurrency: &n}, true},
	} {
		if got := projectCreationDirectAllowed(ProjectCreationContext{CanSetModels: test.models, CanSetLimits: test.policy}, test.resources); got != test.allowed {
			t.Fatal("independent creation authority collapsed", test, got)
		}
	}
}

func TestProjectCreationInitialRequestsAreIndependentSparseChildren(t *testing.T) {
	n := int64(0)
	money := "5.25"
	for mask := 0; mask < 8; mask++ {
		resources := ProjectInitialResources{Reason: "Requested initial resources"}
		want := []string{}
		if mask&1 != 0 {
			resources.ModelIDs = []string{"mdl_one"}
			want = append(want, entity.ProjectRequestModelAccess)
		}
		if mask&2 != 0 {
			resources.TokensMonth = &n
			resources.MoneyMonth = &money
			resources.Currency = "USD"
			want = append(want, entity.ProjectRequestQuota)
		}
		if mask&4 != 0 {
			resources.RPM = &n
			resources.TPM = &n
			resources.Concurrency = &n
			want = append(want, entity.ProjectRequestRateLimit)
		}
		children := projectInitialRequestInputs(projectCreationTestUUID, resources)
		actual := []string{}
		intentIDs := map[string]bool{}
		for _, child := range children {
			actual = append(actual, child.Kind)
			if !safeCallID.MatchString(child.RequestID) || intentIDs[child.RequestID] || child.Reason != resources.Reason {
				t.Fatal("child identity/reason not frozen", child)
			}
			intentIDs[child.RequestID] = true
			switch child.Kind {
			case entity.ProjectRequestModelAccess:
				if len(child.ModelIDs) != 1 || child.Quota != nil || child.RateLimit != nil {
					t.Fatal("models borrowed quota/rates", child)
				}
			case entity.ProjectRequestQuota:
				if child.Quota == nil || child.RateLimit != nil || len(child.ModelIDs) != 0 || *child.Quota.TokensMonth != 0 || *child.Quota.MoneyMonth != "5.25" {
					t.Fatal("quota lost sparse values", child)
				}
			case entity.ProjectRequestRateLimit:
				if child.RateLimit == nil || child.Quota != nil || len(child.ModelIDs) != 0 || *child.RateLimit.Concurrency != 0 {
					t.Fatal("rates borrowed quota", child)
				}
			}
		}
		if !slices.Equal(actual, want) {
			t.Fatal("initial request families not independent", mask, actual, want)
		}
	}
}

func TestProjectCreationRetryHashAndReceiptDoNotBorrowAuthority(t *testing.T) {
	input, err := normalizeProjectCreationResources(projectCreationTestInput())
	if err != nil {
		t.Fatal(err)
	}
	hash := projectCreationHash("usr_applicant", input)
	row := entity.ProjectCreationReceipt{CreationID: input.CreationID, ActorID: "usr_applicant", ProjectID: "prj_original", RequestHash: hash, ReviewETag: input.ReviewETag}
	if !projectCreationReceiptMatches(row, "usr_applicant", hash, input) {
		t.Fatal("matching saved receipt rejected")
	}
	for _, change := range []func(*entity.ProjectCreationReceipt){func(v *entity.ProjectCreationReceipt) { v.ActorID = "usr_other" }, func(v *entity.ProjectCreationReceipt) { v.CreationID = strings.ToUpper(v.CreationID) }, func(v *entity.ProjectCreationReceipt) { v.RequestHash = strings.Repeat("b", 64) }, func(v *entity.ProjectCreationReceipt) { v.ReviewETag = strings.Repeat("b", 64) }} {
		alias := row
		change(&alias)
		if projectCreationReceiptMatches(alias, "usr_applicant", hash, input) {
			t.Fatal("receipt borrowed foreign/changed identity", alias)
		}
	}
	for _, change := range []func(*ProjectCreationInput){func(v *ProjectCreationInput) { v.Description = "Changed" }, func(v *ProjectCreationInput) { v.ManagerIDs = nil }, func(v *ProjectCreationInput) { v.ReviewETag = strings.Repeat("b", 64) }, func(v *ProjectCreationInput) { v.InitialResources.Reason = "Changed" }, func(v *ProjectCreationInput) { v.InitialResources.ModelIDs = []string{"mdl_third"} }, func(v *ProjectCreationInput) { v.InitialRequest = v.InitialResources; v.InitialResources = nil }} {
		changed, _ := normalizeProjectCreationResources(projectCreationTestInput())
		change(&changed)
		if projectCreationHash("usr_applicant", changed) == hash {
			t.Fatal("changed intent reused receipt")
		}
	}
	if projectCreationHash("usr_other", input) == hash {
		t.Fatal("actor not bound to creation intent")
	}
}

func TestProjectCreationReceiptSnapshotRejectsMalformedCurrentProof(t *testing.T) {
	policy, _ := limits.Normalize(limits.Policy{})
	snapshot := ProjectCreationSnapshot{ProjectID: "prj_original", CreatorID: "usr_applicant", Name: "Original", Managers: []ProjectCreationManagerSnapshot{{ID: "pmg_original", UserID: "usr_applicant"}}, ModelIDs: []string{"mdl_original"}, PolicyETag: "0", Policy: policy, InitialRequestIDs: []string{}}
	row := entity.ProjectCreationReceipt{CreationID: projectCreationTestUUID, ActorID: snapshot.CreatorID, ProjectID: snapshot.ProjectID, RequestHash: strings.Repeat("a", 64), ReviewETag: strings.Repeat("b", 64), CreatedAt: time.Now().UTC()}
	raw, _ := json.Marshal(snapshot)
	row.SnapshotJSON = string(raw)
	got, err := readProjectCreationSnapshot(row)
	if err != nil || !reflect.DeepEqual(got, snapshot) {
		t.Fatal("original proof did not survive receipt encoding", err)
	}
	for name, change := range map[string]func(*ProjectCreationSnapshot){
		"borrowed_project":    func(v *ProjectCreationSnapshot) { v.ProjectID = "prj_other" },
		"borrowed_actor":      func(v *ProjectCreationSnapshot) { v.CreatorID = "usr_other" },
		"duplicate_manager":   func(v *ProjectCreationSnapshot) { v.Managers = append(v.Managers, v.Managers[0]) },
		"unsafe_manager":      func(v *ProjectCreationSnapshot) { v.Managers[0].ID = "pmg_original " },
		"duplicate_models":    func(v *ProjectCreationSnapshot) { v.ModelIDs = append(v.ModelIDs, v.ModelIDs[0]) },
		"unsorted_models":     func(v *ProjectCreationSnapshot) { v.ModelIDs = []string{"mdl_z", "mdl_a"} },
		"unsafe_child":        func(v *ProjectCreationSnapshot) { v.InitialRequestIDs = []string{"pmr_original "} },
		"invalid_policy":      func(v *ProjectCreationSnapshot) { n := int64(-1); v.Policy.RPM = &n },
		"unnormalized_policy": func(v *ProjectCreationSnapshot) { v.Policy.IPRanges = nil },
	} {
		t.Run(name, func(t *testing.T) {
			copy := snapshot
			copy.Managers = slices.Clone(snapshot.Managers)
			change(&copy)
			raw, _ := json.Marshal(copy)
			altered := row
			altered.SnapshotJSON = string(raw)
			if _, err := readProjectCreationSnapshot(altered); err != apperrors.ErrInternal {
				t.Fatal("malformed receipt made current application proof", err)
			}
		})
	}
}

func TestProjectCreationModelFilterBoundsAndLiteralSearch(t *testing.T) {
	filter, pattern, err := normalizeProjectCreationModelFilter(ProjectCreationModelFilter{Query: " %_! ", Cursor: "mdl_cursor"})
	if err != nil || filter.Limit != 50 || pattern != "%!%!_!!%" {
		t.Fatal("creation model search changed literal semantics", filter, pattern, err)
	}
	for _, input := range []ProjectCreationModelFilter{{Limit: -1}, {Limit: 51}, {Cursor: "bad"}, {Cursor: "mdl_cursor "}, {Cursor: "mdl_/secret"}, {Query: strings.Repeat("界", 201)}, {Query: string([]byte{0xff})}} {
		if _, _, err := normalizeProjectCreationModelFilter(input); err != apperrors.ErrBadRequest {
			t.Fatal("unsafe/unbounded purpose-specific picker accepted", input, err)
		}
	}
}
