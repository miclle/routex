package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
)

func TestMemberModelsStrictCompleteIntent(t *testing.T) {
	for _, raw := range []string{`{}`, `{"model_ids":null,"reason":"review"}`, `{"model_ids":[],"reason":null}`, `{"model_ids":[],"reason":""}`, `{"model_ids":[],"reason":" review"}`, `{"model_ids":[],"reason":"review\n"}`, `{"model_ids":[],"reason":"\ud800"}`, `{"model_ids":[],"reason":"\udfff"}`, `{"model_ids":[],"reason":"a","reason":"b"}`, `{"model_ids":[],"reason":"a","re\u0061son":"b"}`, `{"model_ids":[],"Reason":"a"}`, `{"model_ids":[],"reason":"a","user_id":"foreign"}`, `{"model_ids":[],"reason":"a","request_id":"uuid"}`, `{"model_ids":["mdl_one","mdl_one"],"reason":"a"}`, `{"model_ids":["mdl_one "],"reason":"a"}`, `{"model_ids":[],"reason":"a"} {}`, "{\"model_ids\":[],\"reason\":\"\xff\"}"} {
		var input MemberModelsWriteInput
		if json.Unmarshal([]byte(raw), &input) == nil {
			t.Fatal("ambiguous or unsafe intent accepted", raw)
		}
	}
	for _, reason := range []string{strings.Repeat("a", 1024), strings.Repeat("界", 341), "Review 😀"} {
		raw, _ := json.Marshal(MemberModelsWriteInput{ModelIDs: []string{}, Reason: reason})
		var input MemberModelsWriteInput
		if err := json.Unmarshal(raw, &input); err != nil || input.ModelIDs == nil || input.Reason != reason {
			t.Fatal(input, err)
		}
	}
	for _, reason := range []string{strings.Repeat("a", 1025), strings.Repeat("界", 342), "x\x00y"} {
		if _, err := normalizeMemberModels(MemberModelsWriteInput{ModelIDs: []string{}, Reason: reason}); err == nil {
			t.Fatal("reason bound lost")
		}
	}
	raw := []byte(`{"model_ids":["mdl_z","mdl_a"],"reason":"review"}`)
	var input MemberModelsWriteInput
	if json.Unmarshal(raw, &input) != nil || !slices.Equal(input.ModelIDs, []string{"mdl_a", "mdl_z"}) {
		t.Fatal(input)
	}
	ids := make([]string, 1001)
	if _, err := normalizeMemberModels(MemberModelsWriteInput{ModelIDs: ids, Reason: "review"}); err == nil {
		t.Fatal("complete set bound lost")
	}
}

func TestMemberModelsExactPriceStates(t *testing.T) {
	data := &memberModelsData{Prices: []entity.ModelPrice{{ID: "price_one", ProviderModelID: "pmd_one"}, {ID: "price_two", ProviderModelID: "pmd_two"}}, Rates: []entity.PriceRate{}}
	eligible := []string{"pmd_one", "pmd_two"}
	cases := []struct {
		label      string
		authorized bool
		eligible   []string
		rates      []entity.PriceRate
		state      string
		amount     string
	}{
		{"permission", false, eligible, nil, "unauthorized", ""}, {"no route", true, nil, nil, "unavailable", ""}, {"missing", true, eligible, nil, "missing", ""},
		{"partial", true, eligible, []entity.PriceRate{{ModelPriceID: "price_one", Metric: pricing.Input, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0", Enabled: true}}, "heterogeneous", ""},
		{"zero", true, eligible, []entity.PriceRate{{ModelPriceID: "price_one", Metric: pricing.Input, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0.000", Enabled: true}, {ModelPriceID: "price_two", Metric: pricing.Input, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0", Enabled: true}}, "priced", "0"},
		{"disabled exact", true, eligible, []entity.PriceRate{{ModelPriceID: "price_one", Metric: pricing.Input, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "999999999999999999.000000000000000001"}, {ModelPriceID: "price_two", Metric: pricing.Input, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "999999999999999999.000000000000000001"}}, "disabled", "999999999999999999.000000000000000001"},
	}
	for _, test := range cases {
		t.Run(test.label, func(t *testing.T) {
			data.Rates = test.rates
			cell := memberModelsPrice(pricing.Input, test.authorized, test.eligible, data)
			if cell.State != test.state || test.amount == "" && cell.Rate != nil || test.amount != "" && (cell.Rate == nil || cell.Rate.Amount != test.amount) {
				t.Fatal(cell)
			}
		})
	}
	for _, change := range []func(*entity.PriceRate){func(r *entity.PriceRate) { r.Amount = "1" }, func(r *entity.PriceRate) { r.Currency = "CNY" }, func(r *entity.PriceRate) { r.Enabled = false }, func(r *entity.PriceRate) { r.Amount = "1e3" }} {
		data.Rates = slices.Clone(cases[4].rates)
		change(&data.Rates[1])
		if got := memberModelsPrice(pricing.Input, true, eligible, data); got.State != "heterogeneous" || got.Rate != nil {
			t.Fatal(got)
		}
	}
	data.Prices = append(data.Prices, entity.ModelPrice{ID: "third_schedule", ProviderModelID: "pmd_one"})
	data.Rates = slices.Clone(cases[4].rates)
	if got := memberModelsPrice(pricing.Input, true, eligible, data); got.State != "heterogeneous" {
		t.Fatal("second missing schedule silently ignored", got)
	}
}
func TestMemberModelsGrantDiffPreservesProvenanceAndExplicitEmpty(t *testing.T) {
	source := "mar_source"
	created := time.Now()
	current := []entity.UserModelGrant{{UserID: "usr_one", ModelID: "mdl_one", CreatedAt: created, SourceRequestID: &source}, {UserID: "usr_one", ModelID: "mdl_two", CreatedAt: created}}
	before := personalGrantHash(current)
	removed, added := memberModelsGrantDiff(current, []string{"mdl_one", "mdl_three"})
	if !slices.Equal(removed, []string{"mdl_two"}) || !slices.Equal(added, []string{"mdl_three"}) || before != personalGrantHash(current) || current[0].SourceRequestID != &source || !current[0].CreatedAt.Equal(created) {
		t.Fatal(removed, added, current)
	}
	removed, added = memberModelsGrantDiff(current, []string{})
	if len(removed) != 2 || len(added) != 0 {
		t.Fatal(removed, added)
	}
	sourceEmpty := ""
	changed := slices.Clone(current)
	changed[0].SourceRequestID = &sourceEmpty
	if personalGrantHash(changed) == before {
		t.Fatal("null/empty source conflated")
	}
	changed[0].SourceRequestID = nil
	if personalGrantHash(changed) == before {
		t.Fatal("request source omitted")
	}
	if personalGrantHash(nil) != personalGrantHash([]entity.UserModelGrant{}) {
		t.Fatal("empty set not canonical")
	}
}
func TestMemberModelsCompletePublicationProof(t *testing.T) {
	now := time.Now().UTC()
	user := entity.User{ID: "usr_subject", Role: entity.RoleMember, CreatedAt: now.Add(-time.Hour), PersonalGrantRevision: strings.Repeat("a", 64)}
	svc := &Service{runtime: &gatewayRuntime{}}
	publish := func(grants []entity.UserModelGrant) *runtimeAuthorization {
		auth := buildRuntimeAuthorization(&runtimeData{Users: []entity.User{user}, Grants: grants}, time.Now().Add(time.Minute))
		svc.runtime.auth.Store(auth)
		return auth
	}
	auth := publish(nil)
	if status, applied := svc.memberModelsApplication(user, nil, auth); status != "applied" || applied == nil || !*applied {
		t.Fatal(status, applied)
	}
	grant := entity.UserModelGrant{UserID: user.ID, ModelID: "mdl_retained", CreatedAt: now}
	auth = publish([]entity.UserModelGrant{grant})
	if status, applied := svc.memberModelsApplication(user, []entity.UserModelGrant{grant}, auth); status != "applied" || !*applied {
		t.Fatal(status, applied)
	}
	for _, change := range []func(*entity.User){func(u *entity.User) { u.ID = "USR_subject" }, func(u *entity.User) { u.CreatedAt = u.CreatedAt.Add(time.Second) }, func(u *entity.User) { u.PersonalGrantRevision = strings.Repeat("b", 64) }, func(u *entity.User) { u.Disabled = true }, func(u *entity.User) { u.OffboardedAt = &now }} {
		subject := user
		change(&subject)
		if status, applied := svc.memberModelsApplication(subject, []entity.UserModelGrant{grant}, auth); status != "not_applied" || applied == nil || *applied {
			t.Fatal(status, applied)
		}
	}
	source := "mar_proof"
	changed := grant
	changed.SourceRequestID = &source
	if _, value := svc.memberModelsApplication(user, []entity.UserModelGrant{changed}, auth); value == nil || *value {
		t.Fatal("grant provenance not covered")
	}
	svc.invalidatePersonalModelGrants(user.ID)
	if runtimeDenied(&svc.runtime.deniedUsers, user.ID) || !runtimeDenied(&svc.runtime.deniedPersonalGrants, user.ID) {
		t.Fatal("Personal reduction leaked into Team User authority")
	}
	if _, value := svc.memberModelsApplication(user, []entity.UserModelGrant{grant}, auth); value == nil || *value {
		t.Fatal("reduction fence omitted")
	}
	generation := svc.runtime.epoch.Load()
	svc.invalidatePersonalModelGrants(user.ID)
	clearRuntimeTombstones(&svc.runtime.deniedPersonalGrants, generation)
	if !runtimeDenied(&svc.runtime.deniedPersonalGrants, user.ID) {
		t.Fatal("older publication cleared newer reduction")
	}
	auth.ValidUntil = now.Add(-time.Second)
	if status, value := svc.memberModelsApplication(user, nil, auth); status != "unavailable" || value != nil {
		t.Fatal(status, value)
	}
}
func TestMemberModelsExactSelectorSQLDoesNotBorrowInitializedStatement(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
		if err != nil {
			t.Fatal(err)
		}
		origin := db.Clauses(clause.Locking{Strength: "UPDATE"})
		query := modelCreationDB(origin).Model(&entity.Model{}).Where(memberModelsExactIDs(origin, "id", []string{"mdl_A", "mdl_A "})).Find(&[]entity.Model{})
		query.Statement.BuildClauses = []string{"SELECT", "FROM", "WHERE", "FOR"}
		callbacks.BuildQuerySQL(query)
		if !reflect.DeepEqual(query.Statement.Vars, []any{"mdl_A", "mdl_A "}) || strings.Contains(query.Statement.SQL.String(), "mdl_A") || origin.Statement.Model != nil {
			t.Fatal(query.Statement.SQL.String(), query.Statement.Vars)
		}
		if dialect.Name() == "mysql" && strings.Count(query.Statement.SQL.String(), "AS BINARY") != 4 {
			t.Fatal("exact selector lost binary equality", query.Statement.SQL.String())
		}
	}
}
func TestMemberModelsPreparedPersonalReductionBlocksFourNativeProtocols(t *testing.T) {
	for _, protocol := range []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		t.Run(protocol, func(t *testing.T) {
			var dispatched atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				dispatched.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{}`)
			}))
			defer server.Close()
			svc, data, bearer := runtimeFixture(t, server.URL+"/v1")
			data.Users[0].PersonalGrantRevision = strings.Repeat("a", 64)
			data.Connections[0].Protocol = protocol
			svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
			routes, err := svc.buildRuntimeRoutes(data)
			if err != nil {
				t.Fatal(err)
			}
			svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_revision", Models: routes})
			svc.afterGatewayAdmission = func() {
				data.Users[0].PersonalGrantRevision = strings.Repeat("b", 64)
				svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
			}
			var result *GatewayResult
			switch protocol {
			case entity.ProtocolOpenAIChat:
				result, err = svc.GatewayChat(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`), "req_revision")
			case entity.ProtocolOpenAIResponses:
				result, err = svc.GatewayResponses(context.Background(), bearer, []byte(teamNativeInput(protocol)), "req_revision")
			case entity.ProtocolAnthropicMessages:
				result, err = svc.GatewayMessages(context.Background(), bearer, []byte(teamNativeInput(protocol)), "req_revision", MessagesHeaders{Version: "2023-06-01"})
			default:
				result, err = svc.GatewayGemini(context.Background(), bearer, []byte(teamNativeInput(protocol)), "req_revision", "public-model", false)
			}
			if err == nil || result == nil || !result.Admitted || !result.NoUpstreamWork() || result.AttemptID != "" || dispatched.Load() != 0 {
				t.Fatal("old prepared grant generation dispatched", result, err, dispatched.Load())
			}
		})
	}
}
func TestMemberModelsPersonalTombstoneDoesNotBlockTeamProtocols(t *testing.T) {
	for _, protocol := range []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		t.Run(protocol, func(t *testing.T) {
			var dispatched atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				dispatched.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{}`)
			}))
			defer server.Close()
			svc, identity := teamNativeFixture(t, server.URL+"/v1", protocol)
			svc.invalidatePersonalModelGrants(identity.UserID)
			var result *GatewayResult
			var err error
			if protocol == entity.ProtocolOpenAIChat {
				result, err = svc.TeamGatewayChat(context.Background(), identity, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`), "req_team_independent")
			} else {
				result, err = teamNativeInvoke(svc, identity, protocol, teamNativeInput(protocol), "req_team_independent")
			}
			if err != nil || dispatched.Load() != 1 || result == nil || result.TeamID != identity.TeamID || result.KeyID != "" {
				t.Fatal("Personal grant fence affected Team", result, err, dispatched.Load())
			}
			_ = result.Response.Body.Close()
		})
	}
}
