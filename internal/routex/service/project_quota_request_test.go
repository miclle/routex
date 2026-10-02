package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

func TestProjectQuotaPatchPreservesFinitePresenceAndExactMoney(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `{"tokens_month":null}`, `{"money_month":null,"currency":"USD"}`, `{"tokens_month":-1}`, `{"tokens_month":1.5}`, `{"tokens_month":9007199254740992}`, `{"money_month":1,"currency":"USD"}`, `{"money_month":"1"}`, `{"tokens_month":5,"currency":"USD"}`, `{"rpm":10}`, `{"tokens_month":5,"currency":null}`} {
		var patch ProjectQuotaPatch
		if json.Unmarshal([]byte(raw), &patch) == nil {
			t.Fatalf("accepted invalid finite patch %s", raw)
		}
	}
	var patch ProjectQuotaPatch
	if err := json.Unmarshal([]byte(`{"tokens_month":0,"money_month":"1.000000000000000001","currency":"USD"}`), &patch); err != nil {
		t.Fatal(err)
	}
	if patch.TokensMonth == nil || *patch.TokensMonth != 0 || patch.MoneyMonth == nil || *patch.MoneyMonth != "1.000000000000000001" {
		t.Fatal("lost explicit zero or decimal precision")
	}
	var tokens ProjectQuotaPatch
	if err := json.Unmarshal([]byte(`{"tokens_month":10}`), &tokens); err != nil || tokens.MoneyMonth != nil || tokens.Currency != "" {
		t.Fatal("omitted fields were synthesized", err)
	}
}

// This statement-only dialector has no connection or SQL driver. It exercises
// GORM condition grouping before pagination independently of a database server.
type projectQuotaScopeDialector struct{}

func (projectQuotaScopeDialector) Name() string                    { return "postgres" }
func (projectQuotaScopeDialector) Initialize(*gorm.DB) error       { return nil }
func (projectQuotaScopeDialector) Migrator(*gorm.DB) gorm.Migrator { return nil }
func (projectQuotaScopeDialector) DataTypeOf(*schema.Field) string { return "" }
func (projectQuotaScopeDialector) DefaultValueOf(*schema.Field) clause.Expression {
	return clause.Expr{}
}
func (projectQuotaScopeDialector) BindVarTo(writer clause.Writer, _ *gorm.Statement, _ any) {
	_ = writer.WriteByte('?')
}
func (projectQuotaScopeDialector) QuoteTo(writer clause.Writer, value string) {
	_, _ = writer.WriteString(`"` + value + `"`)
}
func (projectQuotaScopeDialector) Explain(value string, _ ...any) string { return value }

func TestProjectQuotaKindGroupingKeepsScopeStatusAndCursorConjunctive(t *testing.T) {
	for _, access := range []projectRequestAccess{{Model: true}, {Limits: true}, {Model: true, Limits: true}} {
		db, err := gorm.Open(projectQuotaScopeDialector{}, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
		if err != nil {
			t.Fatal(err)
		}
		query := db.Table("project_model_requests").Where(clause.Eq{Column: clause.Column{Name: "project_id"}, Value: "prj_exact"})
		query = projectRequestKindScope(query, access).Where("status = ?", "approved").Where("id < ?", "pmr_cursor")
		query.Statement.Clauses["WHERE"].Build(query.Statement)
		sql := query.Statement.SQL.String()
		if strings.Count(sql, `"project_id"`) != 1 || !strings.HasSuffix(sql, " AND status = ? AND id < ?") {
			t.Fatalf("kind filter bypasses outer scope or pagination: %s", sql)
		}
		if access.Model && !strings.Contains(sql, `AND ("kind" = ? OR `) {
			t.Fatalf("kind union is not a parenthesized expression: %s", sql)
		}
		vars := query.Statement.Vars
		if vars[0] != "prj_exact" || vars[len(vars)-2] != "approved" || vars[len(vars)-1] != "pmr_cursor" {
			t.Fatalf("scope/status/cursor values lost their original bindings: %#v", vars)
		}
	}
}

func TestProjectQuotaReviewValidatorBoundsOptionalDecisionHeader(t *testing.T) {
	for _, value := range []string{"", "0", strings.Repeat("a", 65), strings.Repeat("A", 64), strings.Repeat("z", 64), strings.Repeat("0", 63) + " "} {
		if projectQuotaReviewValidator(value) {
			t.Fatal("invalid composite review validator accepted", value)
		}
	}
	if !projectQuotaReviewValidator(strings.Repeat("0123456789abcdef", 4)) {
		t.Fatal("canonical review validator rejected")
	}
}

func TestProjectQuotaReviewBindsPolicyCurrencyAndRequestIntent(t *testing.T) {
	policy, err := limits.Normalize(limits.Policy{TokensMonth: limitNumber(10)})
	if err != nil {
		t.Fatal(err)
	}
	row := entity.ResourceLimit{ETag: "lim_current"}
	currency := entity.PricingSetting{ETag: "pricing_current", PlatformCurrency: "USD"}
	request := entity.ProjectModelRequest{ID: "pmr_one", RequestHash: "intent", BaselinePolicyETag: "0", RequestedJSON: `{"tokens_month":10}`, Status: entity.ProjectRequestPending}
	base, err := projectQuotaReviewETag("prj_one", row, policy, currency, &request)
	if err != nil || len(base) != 64 {
		t.Fatal(base, err)
	}
	for _, modify := range []func(*entity.ResourceLimit, *limits.Policy, *entity.PricingSetting, *entity.ProjectModelRequest){
		func(r *entity.ResourceLimit, _ *limits.Policy, _ *entity.PricingSetting, _ *entity.ProjectModelRequest) {
			r.ETag = "lim_new"
		},
		func(_ *entity.ResourceLimit, p *limits.Policy, _ *entity.PricingSetting, _ *entity.ProjectModelRequest) {
			p.RPM = limitNumber(20)
		},
		func(_ *entity.ResourceLimit, _ *limits.Policy, c *entity.PricingSetting, _ *entity.ProjectModelRequest) {
			c.ETag = "pricing_new"
		},
		func(_ *entity.ResourceLimit, _ *limits.Policy, c *entity.PricingSetting, _ *entity.ProjectModelRequest) {
			c.PlatformCurrency = "EUR"
		},
		func(_ *entity.ResourceLimit, _ *limits.Policy, _ *entity.PricingSetting, r *entity.ProjectModelRequest) {
			r.RequestedJSON = `{"tokens_month":20}`
		},
	} {
		r, p, c, requestCopy := row, policy, currency, request
		modify(&r, &p, &c, &requestCopy)
		changed, err := projectQuotaReviewETag("prj_one", r, p, c, &requestCopy)
		if err != nil || changed == base {
			t.Fatal("review validator did not bind a reviewed generation", err)
		}
	}
	creation, err := projectQuotaReviewETag("prj_one", row, policy, currency, nil)
	if err != nil || creation == base {
		t.Fatal("creation and decision reviews were conflated", err)
	}
}

func TestProjectQuotaApplicationRequiresExactFreshApprovedPolicy(t *testing.T) {
	money := "1.000000000000000001"
	policy, err := limits.Normalize(limits.Policy{TokensMonth: limitNumber(10), MoneyMonth: &money, Currency: "USD", RPM: limitNumber(50)})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(policy)
	row := entity.ProjectModelRequest{ProjectID: "prj_one", Status: entity.ProjectRequestApproved, ApprovedPolicyETag: "lim_saved", ApprovedPolicyJSON: string(raw)}
	current := entity.ResourceLimit{ETag: row.ApprovedPolicyETag}
	account := limitAccount("project", row.ProjectID)
	for _, name := range []string{"applied", "expired", "wrong revision", "wrong published policy", "currency drift", "current currency drift", "project denied", "policy denied", "inactive", "superseded"} {
		t.Run(name, func(t *testing.T) {
			auth := &runtimeAuthorization{ValidUntil: time.Now().Add(time.Minute), LimitPolicies: map[string]limits.Policy{account: policy}, Quota: &runtimeQuotaData{Currency: "USD", Revisions: map[string]string{account: row.ApprovedPolicyETag}}}
			svc := &Service{runtime: &gatewayRuntime{}}
			svc.runtime.auth.Store(auth)
			result := &ProjectRequestRecord{PlatformCurrency: "USD"}
			c, status := current, entity.ResourceActive
			switch name {
			case "expired":
				auth.ValidUntil = time.Now().Add(-time.Minute)
			case "wrong revision":
				auth.Quota.Revisions[account] = "lim_other"
			case "wrong published policy":
				p := policy
				p.RPM = limitNumber(100)
				auth.LimitPolicies[account] = p
			case "currency drift":
				auth.Quota.Currency = "EUR"
			case "current currency drift":
				result.PlatformCurrency = "EUR"
			case "project denied":
				svc.runtime.deniedProjects.Store(row.ProjectID, uint64(1))
			case "policy denied":
				svc.runtime.deniedLimits.Store(account, uint64(1))
			case "inactive":
				status = entity.ResourceDisabled
			case "superseded":
				c.ETag = "lim_later"
			}
			if err := svc.projectQuotaApplication(&row, result, c, policy, status); err != nil {
				t.Fatal(err)
			}
			want := "pending"
			switch name {
			case "applied":
				want = "applied"
			case "superseded":
				want = "superseded"
			}
			if result.ApplicationStatus != want || result.RuntimeApplied == nil || *result.RuntimeApplied != (name == "applied") {
				t.Fatalf("incorrect application proof: %+v", result)
			}
		})
	}
	baseline, _ := json.Marshal(projectQuotaBaseline{ProjectQuotaValues: *projectQuotaValues(policy), PlatformCurrency: "USD"})
	row.Kind, row.BaselineJSON, row.RequestedJSON = entity.ProjectRequestQuota, string(baseline), `{"tokens_month":10}`
	immutable, err := projectRequestRecord(&row)
	if err != nil || immutable.RuntimeApplied != nil || immutable.ApplicationStatus != "" || !reflect.DeepEqual(immutable.ApprovedQuota, projectQuotaValues(policy)) {
		t.Fatal("immutable list record fabricated live application", err)
	}
	encoded, _ := json.Marshal(immutable)
	if strings.Contains(string(encoded), "runtime_applied") {
		t.Fatal("list record leaked a live application claim")
	}
}
