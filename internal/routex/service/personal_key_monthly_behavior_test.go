package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

func TestPersonalKeyMonthlyBehaviorExactRootAndForeignScope(t *testing.T) {
	root := entity.APIKey{ID: "key_root", UserID: "usr_owner"}
	row := entity.ResourceLimit{ScopeKind: "key", ScopeID: root.ID, TokensMonthBehavior: "alert_only", MoneyMonthBehavior: "stop"}
	if _, err := policyFromRow(row); err == nil {
		t.Fatal("generic Project/foreign key reader admitted soft mode")
	}
	p, err := personalKeyPolicyFromRow(row, root, "usr_owner")
	if err != nil || p.TokensMonthBehavior != "alert_only" || p.MoneyMonthBehavior != "" {
		t.Fatal(p, err)
	}
	for _, test := range []struct {
		name  string
		row   entity.ResourceLimit
		root  entity.APIKey
		owner string
	}{
		{"owner", row, root, "usr_other"}, {"case", row, root, "USR_owner"}, {"scope", entity.ResourceLimit{ScopeKind: "project", ScopeID: root.ID}, root, "usr_owner"},
		{"rootAlias", row, entity.APIKey{ID: "KEY_root", UserID: root.UserID}, root.UserID}, {"rotated", row, entity.APIKey{ID: root.ID, UserID: root.UserID, ReplacesKeyID: personalKeyBehaviorString("key_before")}, root.UserID},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := personalKeyPolicyFromRow(test.row, test.root, test.owner); err == nil {
				t.Fatal("immutable parent proof bypassed")
			}
		})
	}
}
func TestPersonalKeyMonthlyBehaviorRuntimeRejectsProjectCollisionAndOrphan(t *testing.T) {
	root := entity.APIKey{ID: "key_root", UserID: "usr_owner"}
	row := entity.ResourceLimit{ScopeKind: "key", ScopeID: root.ID, TokensMonthBehavior: "alert_only"}
	for _, test := range []struct {
		name     string
		keys     []entity.APIKey
		users    []entity.User
		projects []entity.ProjectKey
		valid    bool
	}{
		{"personal", []entity.APIKey{root}, []entity.User{{ID: root.UserID}}, nil, true},
		{"project", nil, []entity.User{{ID: root.UserID}}, []entity.ProjectKey{{ID: root.ID, ProjectID: "prj_one"}}, false},
		{"collision", []entity.APIKey{root}, []entity.User{{ID: root.UserID}}, []entity.ProjectKey{{ID: root.ID, ProjectID: "prj_one"}}, false},
		{"orphan", []entity.APIKey{root}, nil, nil, false},
		{"duplicate", []entity.APIKey{root, root}, []entity.User{{ID: root.UserID}}, nil, false},
		{"userAlias", []entity.APIKey{root}, []entity.User{{ID: "USR_owner"}}, nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := &runtimeData{Keys: test.keys, Users: test.users, Limits: []entity.ResourceLimit{row}, ProjectData: &projectRuntimeData{Keys: test.projects}}
			err := compileRuntimeLimits(d)
			if (err == nil) != test.valid {
				t.Fatal(err)
			}
			if test.valid && d.LimitPolicies[limitAccount("key", root.ID)].TokensMonthBehavior != "alert_only" {
				t.Fatal("mode not published")
			}
		})
	}
}
func TestPersonalKeyMonthlyBehaviorFullReplacementOmissionAndAudit(t *testing.T) {
	var input LimitInput
	if json.Unmarshal([]byte(`{"tokens_month":0,"reason":"review"}`), &input) != nil {
		t.Fatal("legacy omission rejected")
	}
	p, err := limits.Normalize(input.Policy)
	if err != nil || p.TokensMonthBehavior != "" || p.MoneyMonthBehavior != "" {
		t.Fatal("omission did not reset stop", p, err)
	}
	row := entity.AuditEvent{ResourceType: "key", ResourceID: "key_root"}
	soft := limits.Policy{TokensMonthBehavior: "alert_only"}
	if !validLimitAuditBehavior(row, "user", soft) || validLimitAuditBehavior(row, "", soft) || validLimitAuditBehavior(row, "project", soft) {
		t.Fatal("typed Personal audit scope not exact")
	}
	for _, kind := range []string{"project", "project_key", "team_member"} {
		row.ResourceType = kind
		if validLimitAuditBehavior(row, "user", soft) {
			t.Fatal("foreign audit mode", kind)
		}
	}
	raw, _ := json.Marshal(userMonthlyBehaviorWire(p))
	if !strings.Contains(string(raw), `"tokens_month_behavior":"stop"`) {
		t.Fatal("canonical wire")
	}
}

func personalKeyBehaviorString(v string) *string { return &v }

func TestPersonalKeyMonthlyBehaviorProofUsesCurrentRootOwnerAndRotation(t *testing.T) {
	root := "key_original"
	auth := &runtimeAuthorization{LimitRoots: map[string]string{"key_current": root}, PersonalLimitOwners: map[string]string{root: "usr_owner"}, KeysByID: map[string]runtimeKey{"key_current": {Key: entity.APIKey{ID: "key_current", UserID: "usr_owner", ReplacesKeyID: &root}}}, UserProofs: map[string]runtimeUserProof{"usr_owner": {Enabled: true}}}
	result := &GatewayResult{KeyID: "key_current", UserID: "usr_owner"}
	if !personalKeyQuotaProof(auth, result, limitAccount("key", root)) {
		t.Fatal("canonical rotation proof rejected")
	}
	for _, tc := range []struct {
		name    string
		result  GatewayResult
		account string
	}{
		{"Project", GatewayResult{KeyID: result.KeyID, UserID: result.UserID, ProjectID: "prj_one"}, limitAccount("key", root)},
		{"owner", GatewayResult{KeyID: result.KeyID, UserID: "usr_other"}, limitAccount("key", root)},
		{"alias", GatewayResult{KeyID: "KEY_current", UserID: result.UserID}, limitAccount("key", root)},
		{"child", *result, limitAccount("key", result.KeyID)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if personalKeyQuotaProof(auth, &tc.result, tc.account) {
				t.Fatal("foreign proof accepted")
			}
		})
	}
	auth.UserProofs[result.UserID] = runtimeUserProof{Enabled: false}
	if personalKeyQuotaProof(auth, result, limitAccount("key", root)) {
		t.Fatal("disabled current User accepted")
	}
	auth.UserProofs[result.UserID] = runtimeUserProof{Enabled: true}
	auth.KeysByID[result.KeyID] = runtimeKey{Key: entity.APIKey{ID: result.KeyID, UserID: result.UserID}, ProjectID: "prj_one"}
	if personalKeyQuotaProof(auth, result, limitAccount("key", root)) {
		t.Fatal("Project snapshot accepted as Personal")
	}
}

func TestPersonalKeyMonthlyBehaviorKeepsSettledWarningsAndUnknownHolds(t *testing.T) {
	row, root, owner, frame := personalKeyWarningFixture()
	row.TokensMonthBehavior = "alert_only"
	row.MoneyMonthBehavior = "alert_only"
	if values := personalKeyMonthlyWarnings(row, root, owner, frame, "USD"); len(values) != 2 {
		t.Fatal("soft policy suppressed settled warnings", values)
	}
	proof := frame.Accounts[limitAccount("key", root.ID)]
	proof.Usage.Month.TokensUnknown = 1
	proof.Usage.Month.MoneyUnknown = 1
	frame.Accounts[limitAccount("key", root.ID)] = proof
	if values := personalKeyMonthlyWarnings(row, root, owner, frame, "USD"); len(values) != 0 {
		t.Fatal("unknown became settled warning", values)
	}
}

func TestPersonalKeyMonthlyBehaviorFallbackExactRotationProducer(t *testing.T) {
	root := "key_original"
	current := entity.APIKey{ID: "key_current", UserID: "usr_owner", ReplacesKeyID: &root}
	keys := []entity.APIKey{{ID: root, UserID: current.UserID}, current}
	result := &GatewayResult{KeyID: current.ID, UserID: current.UserID}
	if !personalKeyGatewayRoot(keys, result, root) {
		t.Fatal("exact rotation rejected")
	}
	for _, tc := range []struct {
		name   string
		keys   []entity.APIKey
		result GatewayResult
		root   string
	}{
		{"child_account", keys, *result, current.ID},
		{"case_alias", keys, GatewayResult{KeyID: "KEY_current", UserID: current.UserID}, root},
		{"foreign_owner", keys, GatewayResult{KeyID: current.ID, UserID: "usr_other"}, root},
		{"Project", keys, GatewayResult{KeyID: current.ID, UserID: current.UserID, ProjectID: "prj_one"}, root},
		{"duplicate", append(append([]entity.APIKey{}, keys...), current), *result, root},
		{"missing_root", keys[1:], *result, root},
		{"cross_owner_rotation", []entity.APIKey{{ID: root, UserID: "usr_other"}, current}, *result, root},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if personalKeyGatewayRoot(tc.keys, &tc.result, tc.root) {
				t.Fatal("unproven fallback accepted")
			}
		})
	}
}
