package service

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

func TestProjectMonthlyBehaviorStoredScopeAndSparseApproval(t *testing.T) {
	money := "10.000000000000000001"
	row := entity.ResourceLimit{ScopeKind: "project", ScopeID: "prj_exact", TokensMonth: limitNumber(100), MoneyMonth: &money, Currency: "USD", TokensMonthBehavior: "alert_only", MoneyMonthBehavior: "stop", RPM: limitNumber(4)}
	policy, err := policyFromRow(row)
	if err != nil {
		t.Fatal("exact Project soft policy rejected", err)
	}
	for _, change := range []struct{ kind, raw string }{
		{entity.ProjectRequestQuota, `{"tokens_month":200}`},
		{entity.ProjectRequestQuota, `{"money_month":"20.000000000000000002","currency":"USD"}`},
		{entity.ProjectRequestRateLimit, `{"rpm":2}`},
	} {
		after, err := applyProjectLimitPatch(change.kind, change.raw, policy, "USD")
		if err != nil || after.TokensMonthBehavior != policy.TokensMonthBehavior || after.MoneyMonthBehavior != policy.MoneyMonthBehavior {
			t.Fatal("numeric approval reset modes", err, after)
		}
		raw, _ := json.Marshal(after)
		saved := entity.ProjectModelRequest{ApprovedPolicyETag: "approved", ApprovedPolicyJSON: string(raw)}
		replay, err := approvedProjectQuotaPolicy(&saved)
		if err != nil || !reflect.DeepEqual(replay, after) {
			t.Fatal("approved replay changed original modes", err)
		}
	}
	for _, kind := range []string{"key", "team_member", "Project", "project "} {
		row.ScopeKind = kind
		if _, err := policyFromRow(row); err == nil {
			t.Fatal("foreign or malformed soft scope", kind)
		}
	}
}

func TestProjectMonthlyBehaviorFullReplacementAndReviewIdentity(t *testing.T) {
	var input LimitInput
	if err := json.Unmarshal([]byte(`{"tokens_month":0,"money_month":null,"reason":"review"}`), &input); err != nil {
		t.Fatal(err)
	}
	policy, err := limits.Normalize(input.Policy)
	if err != nil || policy.TokensMonthBehavior != "" || policy.MoneyMonthBehavior != "" {
		t.Fatal("omission was not stop", err, policy)
	}
	before := limits.Policy{TokensMonth: limitNumber(0), TokensMonthBehavior: "alert_only", MoneyMonthBehavior: "alert_only"}
	row := entity.ResourceLimit{ScopeKind: "project", ScopeID: "prj_exact", ETag: "policy"}
	setting := entity.PricingSetting{ETag: "currency", PlatformCurrency: "USD"}
	a, _ := projectQuotaReviewETag(row.ScopeID, row, before, setting, nil)
	before.TokensMonthBehavior = ""
	b, _ := projectQuotaReviewETag(row.ScopeID, row, before, setting, nil)
	if a == b {
		t.Fatal("mode change reused quota review")
	}
	if !validMonthlyBehaviorAudit("project", limits.Policy{TokensMonthBehavior: "alert_only"}) {
		t.Fatal("typed Project audit lost mode")
	}
}

func TestProjectMonthlyBehaviorPublicationRequiresExactRetainedIdentity(t *testing.T) {
	row := entity.ResourceLimit{ScopeKind: "project", ScopeID: "prj_exact", TokensMonth: limitNumber(0), TokensMonthBehavior: "alert_only"}
	project := entity.Project{ID: row.ScopeID, CreatedAt: time.Unix(1, 0)}
	for _, test := range []struct {
		name     string
		projects []entity.Project
		valid    bool
	}{
		{"exact", []entity.Project{project}, true}, {"missing", nil, false}, {"duplicate", []entity.Project{project, project}, false}, {"zero birth", []entity.Project{{ID: row.ScopeID}}, false}, {"alias", []entity.Project{{ID: "prj_Exact", CreatedAt: project.CreatedAt}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := &runtimeData{Limits: []entity.ResourceLimit{row}, ProjectData: &projectRuntimeData{Projects: test.projects}}
			err := compileRuntimeLimits(data)
			if (err == nil) != test.valid {
				t.Fatal("publication identity fence", err)
			}
		})
	}
}
