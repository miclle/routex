package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

func TestProjectRateLimitPatchStrictFinitePresence(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"rpm":null}`, `{"tpm":null}`, `{"concurrency":null}`, `{"rpm":-1}`, `{"tpm":1.5}`, `{"concurrency":9007199254740992}`, `{"rpm":"5"}`, `{"currency":"USD","rpm":5}`, `{"tokens_month":5}`} {
		var patch ProjectRateLimitPatch
		if json.Unmarshal([]byte(raw), &patch) == nil {
			t.Fatalf("accepted invalid patch %s", raw)
		}
	}
	for _, raw := range []string{`{"rpm":0}`, `{"tpm":9007199254740991}`, `{"concurrency":0}`, `{"rpm":1,"tpm":5,"concurrency":2}`} {
		var patch ProjectRateLimitPatch
		if err := json.Unmarshal([]byte(raw), &patch); err != nil {
			t.Fatal(raw, err)
		}
		encoded, _ := json.Marshal(patch)
		if string(encoded) != raw {
			t.Fatalf("finite zero or omitted fields changed: %s", encoded)
		}
	}
}

func TestProjectRateLimitPatchPreservesFullPolicyAndDenomination(t *testing.T) {
	money := "1.000000000000000001"
	before, err := limits.Normalize(limits.Policy{Tokens5H: limitNumber(500), Tokens7D: limitNumber(1000), TokensMonth: limitNumber(2000), MoneyMonth: &money, Currency: "USD", RPM: limitNumber(100), TPM: limitNumber(200), Concurrency: limitNumber(3), IPMode: "allowlist", IPRanges: []string{"127.0.0.1"}})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := applyProjectLimitPatch(entity.ProjectRequestRateLimit, `{"rpm":0,"concurrency":2}`, before, "USD")
	if err != nil {
		t.Fatal(err)
	}
	expected := before
	expected.RPM = limitNumber(0)
	expected.Concurrency = limitNumber(2)
	if !reflect.DeepEqual(approved, expected) {
		t.Fatalf("rate patch replaced unrelated policy: %+v", approved)
	}
	if _, err := applyProjectLimitPatch(entity.ProjectRequestRateLimit, `{"rpm":1}`, before, "EUR"); err == nil {
		t.Fatal("saved contradictory retained money denomination")
	}
}

func TestProjectRequestLegacyDigestGoldenVectors(t *testing.T) {
	zero := int64(0)
	money := "1.000000000000000001"
	input := ProjectRequestInput{Kind: entity.ProjectRequestQuota, RequestID: "req_stable", ReviewETag: strings.Repeat("a", 64), Reason: "Reviewed quota", Quota: &ProjectQuotaPatch{TokensMonth: &zero, MoneyMonth: &money, Currency: "USD"}}
	_, digest, err := projectLimitCreationIntent("usr_actor", "prj_project", input)
	if err != nil || digest != "89e1b512003cebe87c92248d3b5adcda24713c1ef2937b3dba70d8723fe80204" {
		t.Fatal("released QUOTA creation digest changed", digest, err)
	}
	decision := ProjectRequestDecision{Action: "approve", Reason: "Reviewed decision", ReviewETag: strings.Repeat("a", 64)}
	digest, err = projectLimitDecisionHash(entity.ProjectRequestQuota, "usr_actor", "prj_project", "pmr_stable", decision)
	if err != nil || digest != "3854712e5cd2a4b636a78a48dbbf077bc92e49cbb73ed8145cdc7923580d89f3" {
		t.Fatal("released QUOTA decision digest changed", digest, err)
	}
	quotaDecisionDigest := digest
	model := ProjectRequestInput{RequestID: "req_stable", ModelIDs: []string{"mdl_a", "mdl_b"}, Reason: "Reviewed models"}
	digest, err = projectModelRequestHash("usr_actor", "prj_project", model)
	if err != nil || digest != "1190fb99b99c491418291ef22507da54e69ef26ce08e499c10a1b9b12bb6dd61" {
		t.Fatal("released MODEL creation digest changed", digest, err)
	}
	rate, err := projectLimitDecisionHash(entity.ProjectRequestRateLimit, "usr_actor", "prj_project", "pmr_stable", decision)
	if err != nil || rate == quotaDecisionDigest {
		t.Fatal("RATE receipt identity omits kind", err)
	}
}

func TestProjectRateLimitRecordIsImmutableWithoutApplicationClaims(t *testing.T) {
	row := entity.ProjectModelRequest{Kind: entity.ProjectRequestRateLimit, BaselineJSON: `{"rpm":1,"tpm":null,"concurrency":2}`, RequestedJSON: `{"tpm":0}`, BaselinePolicyETag: "lim_base", Status: entity.ProjectRequestPending}
	record, err := projectRequestRecord(&row)
	if err != nil || record.BaselineRateLimit.RPM == nil || *record.RequestedRateLimit.TPM != 0 || record.CurrentRateLimit != nil || record.RuntimeApplied != nil || record.BaselineQuota != nil {
		t.Fatal(record, err)
	}
	row.Status = entity.ProjectRequestApproved
	row.ApprovedPolicyETag = "lim_approved"
	row.ApprovedPolicyJSON = `{"rpm":1,"tpm":0,"concurrency":2}`
	record, err = projectRequestRecord(&row)
	if err != nil || record.ApprovedRateLimit == nil || *record.ApprovedRateLimit.TPM != 0 || record.RuntimeApplied != nil {
		t.Fatal(record, err)
	}
	if !(projectRequestAccess{Limits: true}).allows(entity.ProjectRequestRateLimit) || (projectRequestAccess{Model: true}).allows(entity.ProjectRequestRateLimit) {
		t.Fatal("RATE inherited model review authority")
	}
}

func TestProjectRequestLegacyContextReviewGoldenVector(t *testing.T) {
	policy, err := limits.Normalize(limits.Policy{TokensMonth: limitNumber(10)})
	if err != nil {
		t.Fatal(err)
	}
	digest, err := projectQuotaReviewETag("prj_one", entity.ResourceLimit{ETag: "lim_current"}, policy, entity.PricingSetting{ETag: "pricing_current", PlatformCurrency: "USD"}, nil)
	if err != nil || digest != "65ca34d889a51d5588d31fe107bcd19ab310ccf802ed81b95d319c5f37864612" {
		t.Fatal("released context review envelope changed", digest, err)
	}
}
