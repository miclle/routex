package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/limits"
)

func teamCreationTestInput() TeamCreationInput {
	return TeamCreationInput{Name: "Reviewed Team", Description: "Exact intent", OwnerIDs: []string{"usr_two", "usr_one"}, CreationID: "11111111-1111-4111-8111-111111111163", ReviewETag: strings.Repeat("a", 64)}
}
func TestTeamCreationStrictPresenceAndSparseWire(t *testing.T) {
	valid := `{"name":"New Team","description":"","owner_ids":["usr_one"],"creation_id":"11111111-1111-4111-8111-111111111163","initial_limits":{"tokens_5h":null,"tokens_month":0,"money_month":"12.500000000000000001","currency":"USD","reason":"Reviewed controls"}}`
	var input TeamCreationInput
	if err := json.Unmarshal([]byte(valid), &input); err != nil {
		t.Fatal(err)
	}
	input.ReviewETag = strings.Repeat("a", 64)
	normalized, err := normalizeTeamCreationInput(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(normalized.InitialLimits.Fields["tokens_5h"]) != "null" || string(normalized.InitialLimits.Fields["tokens_month"]) != "0" || string(normalized.InitialLimits.Fields["money_month"]) != `"12.500000000000000001"` {
		t.Fatal("sparse exact intent lost", normalized)
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	var again TeamCreationInput
	if json.Unmarshal(raw, &again) != nil || !reflect.DeepEqual(again.InitialLimits, normalized.InitialLimits) {
		t.Fatal("input marshaled internal fields instead of sparse wire", string(raw))
	}
	for _, body := range []string{`null`, `[]`, `{"name":null}`, `{"description":null}`, `{"owner_ids":null}`, `{"owner_ids":[null]}`, `{"owner_ids":[]}`, `{"owner_ids":[1]}`, `{"creation_id":null}`, `{"creation_id":""}`, `{"creation_id":"invalid"}`, `{"creation_id":"11111111-1111-1111-8111-111111111163"}`, `{"initial_limits":null}`, `{"name":"a","name":"b"}`, `{"review_etag":"client"}`, `{"initial_limits":{"rpm":1,"rpm":2}}`, `{"initial_limits":{"ip_mode":"none"}}`, `{"model_ids":[]}`, `{} {}`, string(append([]byte(`{"name":"`), append([]byte{255}, []byte(`"}`)...)...))} {
		var value TeamCreationInput
		if json.Unmarshal([]byte(body), &value) == nil {
			t.Fatal("strict body accepted", body)
		}
	}
}
func TestTeamCreationIndependentAuthorityAndOverrideSemantics(t *testing.T) {
	n := int64(7)
	money := "19.000000000000000001"
	before, _ := limits.Normalize(limits.Policy{TokensMonth: &n, RPM: &n, MoneyMonth: &money, Currency: "USD"})
	for _, test := range []struct {
		body     string
		editable []string
		ok       bool
	}{
		{`{"tokens_month":null,"reason":"Clear"}`, []string{"tokens_month"}, true},
		{`{"tokens_month":0,"reason":"Zero"}`, []string{"rpm"}, false},
		{`{"tokens_month":7,"reason":"Same"}`, nil, false},
		{`{"money_month":null,"reason":"Clear"}`, []string{"money_month"}, true},
		{`{"money_month":"0","currency":"USD","reason":"Zero"}`, []string{"money_month"}, true},
		{`{"rpm":0,"reason":"Zero"}`, []string{"rpm"}, true},
	} {
		var patch TeamLimitInput
		if json.Unmarshal([]byte(test.body), &patch) != nil {
			t.Fatal(test.body)
		}
		if teamCreationSubmittedAllowed(&patch, test.editable) != test.ok {
			t.Fatal("dimension authority inferred from another cap", test)
		}
		policy, err := applyTeamLimitInput(before, patch, "team", test.editable, "USD")
		if test.ok && err != nil || !test.ok && err == nil {
			t.Fatal("override authority incorrect", test, err)
		}
		if test.ok && strings.Contains(test.body, `"tokens_month":null`) && (policy.TokensMonth != nil || *policy.MoneyMonth != money || *policy.RPM != 7) {
			t.Fatal("null mutated omitted default groups", policy)
		}
		if test.ok && strings.Contains(test.body, `"money_month":null`) && (policy.MoneyMonth != nil || policy.Currency != "") {
			t.Fatal("null money retained currency")
		}
	}
	for _, body := range []string{`{}`, `{"reason":"Only"}`, `{"rpm":1}`, `{"rpm":1,"reason":null}`, `{"rpm":1,"reason":" "}`, `{"currency":"USD","reason":"Only"}`, `{"money_month":null,"currency":"USD","reason":"Clear"}`, `{"money_month":"1","reason":"No currency"}`, `{"money_month":1,"currency":"USD","reason":"Number"}`, `{"tokens_month":-1,"reason":"Negative"}`, `{"tpm":1.1,"reason":"Fraction"}`, `{"tokens_month":9223372036854775808,"reason":"Overflow"}`, `{"tokens_month":9007199254740992,"reason":"Above supported"}`, `{"currency":null,"money_month":"1","reason":"Null"}`, `{"rpm":1,"reason":"x\ny"}`} {
		input := teamCreationTestInput()
		input.InitialLimits = &TeamLimitInput{}
		if json.Unmarshal([]byte(body), input.InitialLimits) != nil {
			continue
		}
		if _, err := normalizeTeamCreationInput(input); err != apperrors.ErrBadRequest {
			t.Fatal("malformed override accepted", body, err)
		}
	}
}
func TestTeamCreationSelectionBoundsAndImmutableIdentity(t *testing.T) {
	input := teamCreationTestInput()
	before, _ := json.Marshal(input)
	normalized, err := normalizeTeamCreationInput(input)
	if err != nil || !slices.Equal(normalized.OwnerIDs, []string{"usr_one", "usr_two"}) {
		t.Fatal(normalized, err)
	}
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("normalization mutated reviewed input")
	}
	normalized.OwnerIDs[0] = "usr_changed"
	if input.OwnerIDs[0] == "usr_changed" {
		t.Fatal("owner slice borrowed")
	}
	for _, owners := range [][]string{nil, {}, {"usr_one", "usr_one"}, {"usr_one "}, {"USR_one"}, {"tea_one"}} {
		v := teamCreationTestInput()
		v.OwnerIDs = owners
		if _, err := normalizeTeamCreationInput(v); err != apperrors.ErrBadRequest {
			t.Fatal("invalid exact owner selection", owners, err)
		}
	}
	full := teamCreationTestInput()
	full.OwnerIDs = make([]string, 1000)
	for i := range full.OwnerIDs {
		full.OwnerIDs[i] = fmt.Sprintf("usr_%026d", i)
	}
	if _, err := normalizeTeamCreationInput(full); err != nil {
		t.Fatal("supported 1000 owners reduced", err)
	}
	full.OwnerIDs = append(full.OwnerIDs, "usr_overflow")
	if _, err := normalizeTeamCreationInput(full); err != apperrors.ErrBadRequest {
		t.Fatal("1001 owners not rejected")
	}
	for name, change := range map[string]func(*TeamCreationInput){"uuid": func(v *TeamCreationInput) { v.CreationID = "old" }, "uppercase": func(v *TeamCreationInput) { v.ReviewETag = strings.Repeat("A", 64) }, "missing": func(v *TeamCreationInput) { v.ReviewETag = "" }, "description": func(v *TeamCreationInput) { v.Description = "x\x00" }} {
		t.Run(name, func(t *testing.T) {
			v := teamCreationTestInput()
			change(&v)
			if _, err := normalizeTeamCreationInput(v); err != apperrors.ErrBadRequest {
				t.Fatal(err)
			}
		})
	}
	base, _ := normalizeTeamCreationInput(teamCreationTestInput())
	hash := teamCreationHash("usr_creator", base)
	for _, body := range []string{`{"tokens_month":null,"reason":"Clear"}`, `{"tokens_month":0,"reason":"Clear"}`, `{"tokens_month":1,"reason":"Clear"}`} {
		v := base
		var patch TeamLimitInput
		_ = json.Unmarshal([]byte(body), &patch)
		v.InitialLimits = &patch
		if hash == teamCreationHash("usr_creator", v) {
			t.Fatal("presence aliased copied default")
		}
	}
	actor := entity.User{ID: "usr_creator", CreatedAt: time.Unix(1700000000, 0)}
	receipt := entity.TeamCreationReceipt{CreationID: base.CreationID, ActorID: actor.ID, ActorCreatedAt: actor.CreatedAt, RequestHash: hash, ReviewETag: base.ReviewETag}
	if !teamCreationReceiptMatches(receipt, actor, base) {
		t.Fatal("original receipt mismatch")
	}
	actor.CreatedAt = actor.CreatedAt.Add(time.Millisecond)
	if teamCreationReceiptMatches(receipt, actor, base) {
		t.Fatal("recreated actor borrowed receipt")
	}
}
func TestTeamCreationPreviewRedactionExactIntegersAndCoherentFence(t *testing.T) {
	high := int64(limits.MaxInteger)
	zero := int64(0)
	money := "1.000000000000000001"
	policy, _ := limits.Normalize(limits.Policy{TokensMonth: &high, MoneyMonth: &money, Currency: "USD", RPM: &zero})
	for mask := 0; mask < 8; mask++ {
		editable := []string{}
		for _, field := range teamLimitFields {
			permission := teamLimitPermission(field)
			bit := map[string]int{"teams.tokens.write": 1, "teams.money.write": 2, "teams.rates.write": 4}[permission]
			if mask&bit != 0 {
				editable = append(editable, field)
			}
		}
		view := teamCreationPreview(policy, editable)
		for _, field := range teamLimitFields {
			_, present := view[field]
			if present != slices.Contains(editable, field) {
				t.Fatal("redaction represented as null", mask, field)
			}
		}
		if mask&1 != 0 && *view["tokens_month"] != "9007199254740991" {
			t.Fatal("high-range preview rounded")
		}
		if mask&2 != 0 && (*view["money_month"] != money || *view["currency"] != "USD") {
			t.Fatal("money decimal converted")
		}
	}
	actor := entity.User{ID: "usr_actor", Role: entity.RoleAdmin, CreatedAt: time.Unix(1700000000, 0), MemberRoleRevision: strings.Repeat("a", 64)}
	proof := runtimeAdmissionProof{CreatedAt: actor.CreatedAt, Eligible: true, State: "not_required"}
	rule := entity.DefaultLimitRule{RuleETag: strings.Repeat("b", 64)}
	pricing := entity.PricingSetting{ETag: strings.Repeat("c", 64), PlatformCurrency: "USD"}
	tag := teamCreationReviewHash(actor, proof, rule, policy, pricing, nil, false)
	for name, change := range map[string]func(*entity.User, *runtimeAdmissionProof, *entity.DefaultLimitRule, *limits.Policy, *entity.PricingSetting, *[]string){
		"birth": func(a *entity.User, _ *runtimeAdmissionProof, _ *entity.DefaultLimitRule, _ *limits.Policy, _ *entity.PricingSetting, _ *[]string) {
			a.CreatedAt = a.CreatedAt.Add(time.Millisecond)
		},
		"admission": func(_ *entity.User, p *runtimeAdmissionProof, _ *entity.DefaultLimitRule, _ *limits.Policy, _ *entity.PricingSetting, _ *[]string) {
			p.Revision = "changed"
		},
		"identity": func(a *entity.User, _ *runtimeAdmissionProof, _ *entity.DefaultLimitRule, _ *limits.Policy, _ *entity.PricingSetting, _ *[]string) {
			a.MemberRoleRevision = strings.Repeat("d", 64)
		},
		"rule": func(_ *entity.User, _ *runtimeAdmissionProof, r *entity.DefaultLimitRule, _ *limits.Policy, _ *entity.PricingSetting, _ *[]string) {
			r.RuleETag = strings.Repeat("d", 64)
		},
		"pricing": func(_ *entity.User, _ *runtimeAdmissionProof, _ *entity.DefaultLimitRule, _ *limits.Policy, p *entity.PricingSetting, _ *[]string) {
			p.ETag = strings.Repeat("d", 64)
		},
		"hidden_default": func(_ *entity.User, _ *runtimeAdmissionProof, _ *entity.DefaultLimitRule, p *limits.Policy, _ *entity.PricingSetting, _ *[]string) {
			n := int64(1)
			p.TokensMonth = &n
		},
		"authority": func(_ *entity.User, _ *runtimeAdmissionProof, _ *entity.DefaultLimitRule, _ *limits.Policy, _ *entity.PricingSetting, e *[]string) {
			*e = []string{"rpm"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			a, p, r, v, c := actor, proof, rule, policy, pricing
			var e []string
			change(&a, &p, &r, &v, &c, &e)
			if teamCreationReviewHash(a, p, r, v, c, e, false) == tag {
				t.Fatal("coherent fence ignored changed generation")
			}
		})
	}
}
