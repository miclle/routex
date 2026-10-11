package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const provisionerTestToken = "provisioner-private-token"

func provisionerTestConfig(endpoint string) APIKeyProvisionerConfig {
	return APIKeyProvisionerConfig{Endpoint: endpoint, Namespace: "tenant", KVMount: "kv", KeyPrefix: "applications", AuthMount: "workload", Issuer: "https://issuer.example", JWKSURL: "https://issuer.example/keys"}
}
func provisionerTestInput() APIKeyProvisionerInput {
	return APIKeyProvisionerInput{ProfileID: "profile_1", PrincipalID: "principal_1", Subject: "workload:one", Audience: "routex-app", TokenTTLSeconds: 120, TokenMaxTTLSeconds: 300}
}

// Fixtures state independently authored normal Vault shapes, not production
// matcher output. The complete native role projection includes explicit native
// leeway seconds and ordinary empty defaults, but makes no caller identity proof.
func provisionerTestTrust() string {
	return `{"request_id":"safe-id","data":{"bound_issuer":"https://issuer.example","jwks_url":"https://issuer.example/keys","oidc_discovery_url":"","jwks_ca_pem":"","jwt_validation_pubkeys":[],"jwks_pairs":null},"auth":null,"wrap_info":null,"warnings":null,"lease_id":"","renewable":false,"lease_duration":0}`
}

// This native role readback has all 26 observed fields; TTLs follow the test plan.
func provisionerTestRole() string {
	return `{"data":{"alias_metadata":{},"role_type":"jwt","user_claim":"sub","bound_subject":"workload:one","bound_audiences":["routex-app"],"token_policies":["routex-api-key-profile_1"],"token_no_default_policy":true,"token_ttl":120,"token_max_ttl":300,"bound_claims_type":"string","bound_claims":null,"claim_mappings":null,"groups_claim":"","token_type":"default","token_bound_cidrs":[],"allowed_redirect_uris":null,"oidc_scopes":null,"token_period":0,"token_explicit_max_ttl":0,"token_num_uses":0,"max_age":0,"clock_skew_leeway":60,"expiration_leeway":150,"not_before_leeway":150,"user_claim_json_pointer":false,"verbose_oidc_logging":false}}`
}

const provisionerTestPolicy = "path \"kv/data/applications/profile_1/*\" {\n  capabilities = [\"read\"]\n}\n"

func provisionerTestPolicyResponse() string {
	// The system HTTP wrapper repeats native data fields at the top level.
	raw, _ := json.Marshal(map[string]any{"request_id": "safe-id", "lease_id": "", "renewable": false, "lease_duration": 0, "auth": nil, "wrap_info": nil, "warnings": nil, "data": map[string]string{"name": "routex-api-key-profile_1", "policy": provisionerTestPolicy}, "name": "routex-api-key-profile_1", "policy": provisionerTestPolicy})
	return string(raw)
}
func provisionerTestBroaderPolicyResponse() string {
	raw, _ := json.Marshal(map[string]any{"data": map[string]string{"name": "routex-api-key-profile_1", "policy": strings.Replace(provisionerTestPolicy, `["read"]`, `["read","update"]`, 1)}})
	return string(raw)
}

var provisionerTestRoutes = []string{"GET /v1/auth/workload/config", "PUT /v1/sys/policies/acl/routex-api-key-profile_1", "GET /v1/sys/policies/acl/routex-api-key-profile_1", "POST /v1/auth/workload/role/routex-api-key-principal_1", "GET /v1/auth/workload/role/routex-api-key-principal_1"}

func provisionerTestReply(index int, rw http.ResponseWriter) {
	if index == 1 || index == 3 {
		rw.WriteHeader(http.StatusNoContent)
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	body := provisionerTestTrust()
	if index == 2 {
		body = provisionerTestPolicyResponse()
	}
	if index == 4 {
		body = provisionerTestRole()
	}
	_, _ = io.WriteString(rw, body)
}

type provisionerTestPeer struct {
	mu       sync.Mutex
	requests []string
	bodies   [][]byte
	valid    bool
}

func (f *provisionerTestPeer) snapshot() ([]string, [][]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...), append([][]byte(nil), f.bodies...), f.valid
}
func provisionerTestClient(t *testing.T, override func(int, http.ResponseWriter, *http.Request)) (*APIKeyProvisioner, *provisionerTestPeer) {
	t.Helper()
	f := &provisionerTestPeer{valid: true}
	peer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 32769))
		_ = r.Body.Close()
		f.mu.Lock()
		i := len(f.requests)
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		f.bodies = append(f.bodies, body)
		if err != nil || len(body) > 32768 || r.URL.RawQuery != "" || r.Header.Get("X-Vault-Token") != provisionerTestToken || r.Header.Get("X-Vault-Namespace") != "tenant" || r.Header.Get("X-Vault-Request") != "true" {
			f.valid = false
		}
		f.mu.Unlock()
		if override != nil {
			override(i, rw, r)
		} else {
			provisionerTestReply(i, rw)
		}
	}))
	t.Cleanup(peer.Close)
	p, err := NewAPIKeyProvisioner(provisionerTestConfig(peer.URL), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p, f
}
func provisionerTestPrepared(t *testing.T, p *APIKeyProvisioner) *PreparedAPIKeyProvisioning {
	t.Helper()
	v, err := p.Prepare(provisionerTestInput())
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func provisionerTestReject(t *testing.T, r APIKeyProvisionerResult, err error, stage string, requests int, f *provisionerTestPeer) {
	t.Helper()
	if err == nil || r.DesiredGrantsReconciled || r.Outcome == APIKeyProvisionerReconciled {
		t.Fatal("denied metadata became reconciliation")
	}
	var failure *Failure
	if !errors.As(err, &failure) || failure.Stage != stage {
		t.Fatal("safe stage classification lost", err)
	}
	paths, _, valid := f.snapshot()
	if !valid || len(paths) != requests || !reflect.DeepEqual(paths, provisionerTestRoutes[:requests]) {
		t.Fatal("unexpected/repeated metadata request")
	}
	if strings.Contains(fmt.Sprintf("%v %#v", err, r), provisionerTestToken) || strings.Contains(fmt.Sprintf("%v", err), "remote-private-detail") {
		t.Fatal("private material escaped")
	}
}

func TestAPIKeyProvisionerExactGeneratedMetadataFlow(t *testing.T) {
	p, f := provisionerTestClient(t, nil)
	prepared := provisionerTestPrepared(t, p)
	r, err := p.Reconcile(t.Context(), provisionerTestToken, prepared)
	if err != nil || !r.Consumed || r.Outcome != APIKeyProvisionerReconciled || !r.DesiredGrantsReconciled {
		t.Fatal("normal fixed reconciliation failed", err)
	}
	for _, obs := range []Observation{r.Trust, r.PolicyWrite, r.PolicyRead, r.RoleWrite, r.RoleRead} {
		if !obs.Attempted || !obs.Succeeded || obs.Failure != nil || obs.responseCloseFailed {
			t.Fatal("incomplete stage proof")
		}
	}
	paths, bodies, valid := f.snapshot()
	if !valid || !reflect.DeepEqual(paths, provisionerTestRoutes) {
		t.Fatal("unexpected scope/sequence")
	}
	if len(bodies[0])+len(bodies[2])+len(bodies[4]) != 0 {
		t.Fatal("metadata GET body")
	}
	var policy map[string]string
	if json.Unmarshal(bodies[1], &policy) != nil || len(policy) != 1 || policy["policy"] != provisionerTestPolicy {
		t.Fatal("policy broadened")
	}
	var role map[string]json.RawMessage
	if json.Unmarshal(bodies[3], &role) != nil || len(role) != 11 {
		t.Fatal("role fields changed")
	}
	var decoded map[string]any
	if json.Unmarshal(bodies[3], &decoded) != nil {
		t.Fatal("role JSON")
	}
	expected := map[string]any{"role_type": "jwt", "user_claim": "sub", "bound_subject": "workload:one", "bound_audiences": []any{"routex-app"}, "token_policies": []any{"routex-api-key-profile_1"}, "token_no_default_policy": true, "token_ttl": float64(120), "token_max_ttl": float64(300), "clock_skew_leeway": float64(60), "expiration_leeway": float64(150), "not_before_leeway": float64(150)}
	if !reflect.DeepEqual(decoded, expected) {
		t.Fatal("unexpected role authority fields")
	}
	if state := p.ResponseCloseState(); !state.Observed || state.Pending != 0 || state.Failed {
		t.Fatal("response closure not proven")
	}
	raw, _ := json.Marshal(prepared)
	if string(raw) != "{}" || strings.Contains(fmt.Sprintf("%v %#v", prepared, prepared), "workload:one") {
		t.Fatal("prepared plan leaked tuple")
	}
	raw, _ = json.Marshal(r)
	for _, private := range []string{provisionerTestToken, "workload:one", "https://issuer.example", provisionerTestPolicy} {
		if strings.Contains(string(raw), private) {
			t.Fatal("result returned private tuple")
		}
	}
	// Result deliberately has no READY, caller login, effective-policy or application fields.
	for _, name := range []string{"Ready", "CallerAuthenticated", "EffectivePrivilegesVerified", "IsolationVerified", "ApplicationApplied"} {
		if _, present := reflect.TypeFor[APIKeyProvisionerResult]().FieldByName(name); present {
			t.Fatal("metadata result exposes unsupported authority proof")
		}
	}
	if r.Plan.PolicyName != "routex-api-key-profile_1" || r.Plan.RoleName != "routex-api-key-principal_1" || len(r.Plan.SHA256) != 64 {
		t.Fatal("safe generated plan absent")
	}

	t.Run("role_without_alias_metadata_remains_compatible", func(t *testing.T) {
		p, f := provisionerTestClient(t, func(i int, rw http.ResponseWriter, _ *http.Request) {
			if i != 4 {
				provisionerTestReply(i, rw)
				return
			}
			rw.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(rw, strings.Replace(provisionerTestRole(), `"alias_metadata":{},`, "", 1))
		})
		r, err := p.Reconcile(t.Context(), provisionerTestToken, provisionerTestPrepared(t, p))
		if err != nil || !r.Consumed || !r.DesiredGrantsReconciled || r.Outcome != APIKeyProvisionerReconciled {
			t.Fatal("role without alias metadata rejected", err)
		}
		paths, _, valid := f.snapshot()
		if !valid || !reflect.DeepEqual(paths, provisionerTestRoutes) {
			t.Fatal("role compatibility changed scope or sequence")
		}
		if state := p.ResponseCloseState(); !state.Observed || state.Pending != 0 || state.Failed {
			t.Fatal("role compatibility response closure not proven")
		}
	})
}

func TestAPIKeyProvisionerRejectsInvalidAndCopiedPlans(t *testing.T) {
	for _, mutate := range []func(*APIKeyProvisionerConfig){
		func(c *APIKeyProvisionerConfig) { c.AuthMount = "../jwt" }, func(c *APIKeyProvisionerConfig) { c.KeyPrefix = "keys/*" }, func(c *APIKeyProvisionerConfig) { c.JWKSURL = "http://issuer.example/keys" }, func(c *APIKeyProvisionerConfig) { c.Issuer = "https://user:secret@issuer.example" }, func(c *APIKeyProvisionerConfig) { c.Endpoint = "https://vault.example?query=1" },
	} {
		c := provisionerTestConfig("https://vault.example")
		mutate(&c)
		if p, err := NewAPIKeyProvisioner(c, false); err == nil || p != nil {
			t.Fatal("unsafe config admitted")
		}
	}
	p, f := provisionerTestClient(t, nil)
	for _, mutate := range []func(*APIKeyProvisionerInput){func(i *APIKeyProvisionerInput) { i.ProfileID = "x/*" }, func(i *APIKeyProvisionerInput) { i.PrincipalID = ".." }, func(i *APIKeyProvisionerInput) { i.ProfileID = "Profile_1" }, func(i *APIKeyProvisionerInput) { i.Subject = "\u0085" }, func(i *APIKeyProvisionerInput) { i.Audience = "" }, func(i *APIKeyProvisionerInput) { i.TokenTTLSeconds = 0 }, func(i *APIKeyProvisionerInput) { i.TokenMaxTTLSeconds = 3601 }, func(i *APIKeyProvisionerInput) { i.TokenMaxTTLSeconds = 1 }} {
		in := provisionerTestInput()
		mutate(&in)
		if v, err := p.Prepare(in); err == nil || v != nil {
			t.Fatal("unsafe plan admitted")
		}
	}
	prepared := provisionerTestPrepared(t, p)
	var absent context.Context
	r, err := p.Reconcile(absent, provisionerTestToken, prepared)
	if err == nil || r.Consumed || r.Trust.Attempted {
		t.Fatal("nil parent context admitted")
	}
	different := p.config
	different.Namespace = "other"
	foreign, err := NewAPIKeyProvisioner(different, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(foreign.Close)
	r, err = foreign.Reconcile(t.Context(), provisionerTestToken, prepared)
	if err == nil || r.Consumed || r.Trust.Attempted {
		t.Fatal("plan crossed reviewed config")
	}
	for _, token := range []string{"", " token", "token\n", strings.Repeat("a", 4097)} {
		r, err := p.Reconcile(t.Context(), token, prepared)
		if err == nil || r.Consumed || r.Trust.Attempted {
			t.Fatal("invalid token dispatched")
		}
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	r, err = p.Reconcile(cancelled, provisionerTestToken, prepared)
	if err == nil || r.Consumed {
		t.Fatal("canceled preflight consumed plan")
	}
	copyPlan := *prepared
	r, err = p.Reconcile(t.Context(), provisionerTestToken, &copyPlan)
	if err != nil || !r.DesiredGrantsReconciled {
		t.Fatal("valid copied plan rejected", err)
	}
	r, err = p.Reconcile(t.Context(), provisionerTestToken, prepared)
	if err == nil || r.Consumed || r.Trust.Attempted {
		t.Fatal("copied plan replayed")
	}
	paths, _, _ := f.snapshot()
	if len(paths) != 5 {
		t.Fatal("invalid/copy request escaped")
	}
	closed := provisionerTestPrepared(t, p)
	closed.Close()
	r, err = p.Reconcile(t.Context(), provisionerTestToken, closed)
	if err == nil || r.Consumed {
		t.Fatal("closed plan claimed")
	}
	fresh := provisionerTestPrepared(t, p)
	p.Close()
	r, err = p.Reconcile(t.Context(), provisionerTestToken, fresh)
	if err == nil || r.Consumed {
		t.Fatal("closed wrapper admitted new call")
	}
}

func provisionerTestEnvelopeMutation(raw string, mutate func(map[string]any)) string {
	var root map[string]any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		panic(err)
	}
	mutate(root)
	encoded, err := json.Marshal(root)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func TestAPIKeyProvisionerAcceptsNormalPolicyEnvelope(t *testing.T) {
	p, f := provisionerTestClient(t, func(i int, rw http.ResponseWriter, _ *http.Request) {
		if i != 2 {
			provisionerTestReply(i, rw)
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(rw, provisionerTestEnvelopeMutation(provisionerTestPolicyResponse(), func(root map[string]any) { delete(root, "name"); delete(root, "policy") }))
	})
	r, err := p.Reconcile(t.Context(), provisionerTestToken, provisionerTestPrepared(t, p))
	if err != nil || !r.DesiredGrantsReconciled || r.Outcome != APIKeyProvisionerReconciled {
		t.Fatal("normal policy envelope rejected", err)
	}
	paths, _, valid := f.snapshot()
	if !valid || !reflect.DeepEqual(paths, provisionerTestRoutes) {
		t.Fatal("normal policy scope changed")
	}
}

func TestAPIKeyProvisionerRejectsDriftAndAmbiguousJSON(t *testing.T) {
	tests := []struct {
		name string
		at   int
		body string
	}{
		{"issuer", 0, strings.Replace(provisionerTestTrust(), "https://issuer.example\"", "https://other.example\"", 1)},
		{"jwks", 0, strings.Replace(provisionerTestTrust(), "https://issuer.example/keys", "https://other.example/keys", 1)},
		{"discovery", 0, strings.Replace(provisionerTestTrust(), `"oidc_discovery_url":""`, `"oidc_discovery_url":"https://issuer.example"`, 1)},
		{"custom_ca", 0, strings.Replace(provisionerTestTrust(), `"jwks_ca_pem":""`, `"jwks_ca_pem":"remote-private-detail"`, 1)},
		{"oidc_client", 0, strings.Replace(provisionerTestTrust(), `"oidc_discovery_url":""`, `"oidc_client_id":"another","oidc_discovery_url":""`, 1)},
		{"provider_extension", 0, strings.Replace(provisionerTestTrust(), `"oidc_discovery_url":""`, `"provider_config":{"extension":"remote-private-detail"},"oidc_discovery_url":""`, 1)},
		{"static_keys", 0, strings.Replace(provisionerTestTrust(), `"jwt_validation_pubkeys":[]`, `"jwt_validation_pubkeys":["remote-private-detail"]`, 1)},
		{"policy_extra", 2, strings.Replace(provisionerTestPolicyResponse(), `"name":`, `"extra":true,"name":`, 1)},
		{"policy_broader", 2, provisionerTestBroaderPolicyResponse()},
		{"policy_name_alias_mismatch", 2, provisionerTestEnvelopeMutation(provisionerTestPolicyResponse(), func(root map[string]any) { root["name"] = "wrong" })},
		{"policy_text_alias_mismatch", 2, provisionerTestEnvelopeMutation(provisionerTestPolicyResponse(), func(root map[string]any) { root["policy"] = "remote-private-detail" })},
		{"policy_alias_null", 2, provisionerTestEnvelopeMutation(provisionerTestPolicyResponse(), func(root map[string]any) { root["policy"] = nil })},
		{"policy_alias_partial", 2, provisionerTestEnvelopeMutation(provisionerTestPolicyResponse(), func(root map[string]any) { delete(root, "policy") })},
		{"policy_alias_without_data", 2, provisionerTestEnvelopeMutation(provisionerTestPolicyResponse(), func(root map[string]any) { delete(root, "data") })},
		{"policy_alias_duplicate", 2, strings.Replace(provisionerTestPolicyResponse(), `{"`, `{"name":"wrong","`, 1)},
		{"trust_rejects_policy_aliases", 0, provisionerTestEnvelopeMutation(provisionerTestTrust(), func(root map[string]any) {
			root["name"] = "routex-api-key-profile_1"
			root["policy"] = provisionerTestPolicy
		})},
		{"role_rejects_policy_aliases", 4, provisionerTestEnvelopeMutation(provisionerTestRole(), func(root map[string]any) {
			root["name"] = "routex-api-key-profile_1"
			root["policy"] = provisionerTestPolicy
		})},
		{"alias_metadata_null", 4, strings.Replace(provisionerTestRole(), `"alias_metadata":{}`, `"alias_metadata":null`, 1)},
		{"alias_metadata_array", 4, strings.Replace(provisionerTestRole(), `"alias_metadata":{}`, `"alias_metadata":[]`, 1)},
		{"alias_metadata_string", 4, strings.Replace(provisionerTestRole(), `"alias_metadata":{}`, `"alias_metadata":""`, 1)},
		{"alias_metadata_number", 4, strings.Replace(provisionerTestRole(), `"alias_metadata":{}`, `"alias_metadata":0`, 1)},
		{"alias_metadata_boolean", 4, strings.Replace(provisionerTestRole(), `"alias_metadata":{}`, `"alias_metadata":false`, 1)},
		{"alias_metadata_nonempty", 4, strings.Replace(provisionerTestRole(), `"alias_metadata":{}`, `"alias_metadata":{"remote-private-detail":"value"}`, 1)},
		{"alias_metadata_nonempty_nested", 4, strings.Replace(provisionerTestRole(), `"alias_metadata":{}`, `"alias_metadata":{"entry":{}}`, 1)},
		{"alias_metadata_duplicate", 4, strings.Replace(provisionerTestRole(), `{"data":{`, `{"data":{"alias_metadata":{},`, 1)},
		{"alias_metadata_escaped_duplicate", 4, strings.Replace(provisionerTestRole(), `{"data":{`, `{"data":{"\u0061lias_metadata":{},`, 1)},
		{"role_oidc", 4, strings.Replace(provisionerTestRole(), `"role_type":"jwt"`, `"role_type":"oidc"`, 1)},
		{"wildcard_subject", 4, strings.Replace(provisionerTestRole(), `workload:one`, `*`, 1)},
		{"extra_audience", 4, strings.Replace(provisionerTestRole(), `["routex-app"]`, `["routex-app","another"]`, 1)},
		{"extra_policy", 4, strings.Replace(provisionerTestRole(), `["routex-api-key-profile_1"]`, `["routex-api-key-profile_1","root"]`, 1)},
		{"default_policy", 4, strings.Replace(provisionerTestRole(), `"token_no_default_policy":true`, `"token_no_default_policy":false`, 1)},
		{"groups_claim", 4, strings.Replace(provisionerTestRole(), `"groups_claim":""`, `"groups_claim":"groups"`, 1)},
		{"periodic", 4, strings.Replace(provisionerTestRole(), `"token_period":0`, `"token_period":100`, 1)},
		{"clock_skew_leeway_drift", 4, strings.Replace(provisionerTestRole(), `"clock_skew_leeway":60`, `"clock_skew_leeway":61`, 1)},
		{"expiration_leeway_drift", 4, strings.Replace(provisionerTestRole(), `"expiration_leeway":150`, `"expiration_leeway":151`, 1)},
		{"not_before_leeway_drift", 4, strings.Replace(provisionerTestRole(), `"not_before_leeway":150`, `"not_before_leeway":151`, 1)},
		{"ttl_coercion", 4, strings.Replace(provisionerTestRole(), `"token_ttl":120`, `"token_ttl":120.0`, 1)},
		{"null_required", 4, strings.Replace(provisionerTestRole(), `"user_claim":"sub"`, `"user_claim":null`, 1)},
		{"unknown_grant", 4, strings.Replace(provisionerTestRole(), `{"data":{`, `{"data":{"future_grant":true,`, 1)},
		{"duplicate", 4, strings.Replace(provisionerTestRole(), `{"data":{`, `{"data":{"user_claim":"wrong",`, 1)},
		{"escaped_duplicate", 4, strings.Replace(provisionerTestRole(), `{"data":{`, `{"data":{"\u0075ser_claim":"wrong",`, 1)},
		{"unpaired_value", 0, strings.Replace(provisionerTestTrust(), `{"request_id":`, `{"ignored":"\ud800","request_id":`, 1)},
		{"unpaired_key", 0, strings.Replace(provisionerTestTrust(), `{"request_id":`, `{"\udc00":null,"request_id":`, 1)},
		{"invalid_utf8", 0, "{\"data\":\"\xff\"}"},
		{"depth", 0, `{"data":` + strings.Repeat(`[`, 17) + `0` + strings.Repeat(`]`, 17) + `}`},
		{"oversize", 0, strings.Repeat(" ", maxResponseBytes+1)},
		{"upstream_errors", 0, `{"errors":["remote-private-detail"]}`},
		{"auth_material", 0, strings.Replace(provisionerTestTrust(), `"auth":null`, `"auth":{"client_token":"remote-private-detail"}`, 1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, f := provisionerTestClient(t, func(i int, rw http.ResponseWriter, _ *http.Request) {
				if i == tc.at {
					rw.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(rw, tc.body)
				} else {
					provisionerTestReply(i, rw)
				}
			})
			r, err := p.Reconcile(t.Context(), provisionerTestToken, provisionerTestPrepared(t, p))
			stages := []string{"trust", "policy_write", "policy_read", "role_write", "role_read"}
			provisionerTestReject(t, r, err, stages[tc.at], tc.at+1, f)
			if tc.at > 0 && r.Outcome != APIKeyProvisionerUnknown {
				t.Fatal("possible policy write lost unknown state")
			}
		})
	}
}

func TestAPIKeyProvisionerLostWriteAndRedirectNeverReplay(t *testing.T) {
	var committed atomic.Bool
	p, f := provisionerTestClient(t, func(i int, rw http.ResponseWriter, _ *http.Request) {
		if i != 1 {
			provisionerTestReply(i, rw)
			return
		}
		conn, _, err := rw.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		committed.Store(true)
		_ = conn.Close()
	})
	prepared := provisionerTestPrepared(t, p)
	r, err := p.Reconcile(t.Context(), provisionerTestToken, prepared)
	provisionerTestReject(t, r, err, "policy_write", 2, f)
	if !committed.Load() || r.Outcome != APIKeyProvisionerUnknown || !p.ResponseCloseState().Failed {
		t.Fatal("lost committed response became known")
	}
	r, err = p.Reconcile(t.Context(), provisionerTestToken, prepared)
	if err == nil || r.Consumed || r.Trust.Attempted {
		t.Fatal("lost operation replayed")
	}
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var targetCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls.Add(1) }))
			t.Cleanup(target.Close)
			p, f := provisionerTestClient(t, func(i int, rw http.ResponseWriter, req *http.Request) {
				if i == 1 {
					http.Redirect(rw, req, target.URL, status)
				} else {
					provisionerTestReply(i, rw)
				}
			})
			r, err := p.Reconcile(t.Context(), provisionerTestToken, provisionerTestPrepared(t, p))
			provisionerTestReject(t, r, err, "policy_write", 2, f)
			if r.Outcome != APIKeyProvisionerUnknown || targetCalls.Load() != 0 {
				t.Fatal("redirect replayed or claimed known")
			}
			state := p.ResponseCloseState()
			if !state.Observed || state.Pending != 0 || state.Failed != (status != 307 && status != 308) {
				t.Fatal("redirect response ownership misclassified")
			}
		})
	}
}

type provisionerTestTransport func(*http.Request) (*http.Response, error)

func (f provisionerTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type provisionerTestBody struct {
	io.Reader
	read, closes int
	close        func() error
}

func (b *provisionerTestBody) Read(v []byte) (int, error) {
	n, e := b.Reader.Read(v)
	b.read += n
	return n, e
}
func (b *provisionerTestBody) Close() error {
	b.closes++
	if b.close != nil {
		return b.close()
	}
	return nil
}
func provisionerTestResponse(i int) *http.Response {
	rw := httptest.NewRecorder()
	provisionerTestReply(i, rw)
	return rw.Result()
}
func TestAPIKeyProvisionerOriginalDeadlineAndCloseAdmission(t *testing.T) {
	for _, mode := range []string{"parent_cap", "sdk_cap", "cancel_on_close", "close_error", "bounded_read"} {
		t.Run(mode, func(t *testing.T) {
			p, err := NewAPIKeyProvisioner(provisionerTestConfig("https://vault.example"), false)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(p.Close)
			budget := 3 * time.Second
			if mode == "sdk_cap" {
				budget = 30 * time.Second
			}
			ctx, cancel := context.WithTimeout(t.Context(), budget)
			defer cancel()
			var deadline time.Time
			calls := 0
			var owned []*provisionerTestBody
			p.client.http.Transport = provisionerTestTransport(func(req *http.Request) (*http.Response, error) {
				i := calls
				calls++
				d, ok := req.Context().Deadline()
				if !ok {
					t.Error("missing deadline")
				}
				if i == 0 {
					deadline = d
				} else if !deadline.Equal(d) {
					t.Error("renewed step deadline")
				}
				parentDeadline, _ := ctx.Deadline()
				if mode != "sdk_cap" && !d.Equal(parentDeadline) {
					t.Error("parent shortened budget changed")
				}
				if req.GetBody != nil || !req.Close || req.URL.RawQuery != "" {
					t.Error("replayable metadata request")
				}
				response := provisionerTestResponse(i)
				body := &provisionerTestBody{Reader: response.Body}
				_ = response.Body.Close()
				// Read the recorder's bytes independently: the closed bytes.Reader remains
				// readable, and no real network response ownership is hidden by this fake.
				if mode == "bounded_read" && i == 0 {
					body.Reader = strings.NewReader(strings.Repeat("x", maxResponseBytes*2))
				}
				body.close = func() error {
					if i == 4 && mode == "cancel_on_close" {
						cancel()
					}
					if i == 4 && mode == "close_error" {
						return errors.New("remote-private-detail")
					}
					return nil
				}
				owned = append(owned, body)
				response.Body = body
				return response, nil
			})
			started := time.Now()
			r, err := p.Reconcile(ctx, provisionerTestToken, provisionerTestPrepared(t, p))
			finished := time.Now()
			if mode == "parent_cap" || mode == "sdk_cap" {
				if err != nil || !r.DesiredGrantsReconciled || calls != 5 {
					t.Fatal("normal closed operation rejected", err)
				}
			} else if err == nil || r.DesiredGrantsReconciled {
				t.Fatal("late/error close admitted")
			}
			if mode == "sdk_cap" && (deadline.Before(started.Add(requestTimeout)) || deadline.After(finished.Add(requestTimeout))) {
				t.Fatal("SDK budget not captured at admission")
			}
			for _, b := range owned {
				if b.closes != 1 {
					t.Fatal("owned Close not joined exactly once")
				}
			}
			if mode == "bounded_read" && (calls != 1 || owned[0].read != maxResponseBytes+1) {
				t.Fatal("unbounded response drain")
			}
			if mode == "cancel_on_close" && (r.Outcome != APIKeyProvisionerUnknown || r.RoleRead.Failure.Code != "canceled") {
				t.Fatal("post-Close cancellation lost")
			}
			if mode == "close_error" && (r.Outcome != APIKeyProvisionerUnknown || r.RoleRead.Failure.Code != "response_close_failed") {
				t.Fatal("close failure lost")
			}
			if p.ResponseCloseState().Pending != 0 || p.ResponseCloseState().Failed != (mode == "close_error") {
				t.Fatal("closure evidence mismatch")
			}
		})
	}
}

func TestAPIKeyProvisionerPendingCloseCannotReturnSuccess(t *testing.T) {
	p, err := NewAPIKeyProvisioner(provisionerTestConfig("https://vault.example"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	calls := 0
	p.client.http.Transport = provisionerTestTransport(func(*http.Request) (*http.Response, error) {
		i := calls
		calls++
		response := provisionerTestResponse(i)
		if i == 4 {
			inner := response.Body
			response.Body = &provisionerTestBody{Reader: inner, close: func() error { close(entered); <-release; return inner.Close() }}
		}
		return response, nil
	})
	type answer struct {
		r   APIKeyProvisionerResult
		err error
	}
	done := make(chan answer, 1)
	joined := false
	defer func() {
		once.Do(func() { close(release) })
		if !joined {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("owned request did not join")
			}
		}
	}()
	prepared := provisionerTestPrepared(t, p)
	go func() { r, err := p.Reconcile(t.Context(), provisionerTestToken, prepared); done <- answer{r, err} }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("final Close not reached")
	}
	if state := p.ResponseCloseState(); !state.Observed || state.Pending != 1 || state.Failed {
		t.Fatal("held closure missing")
	}
	select {
	case <-done:
		t.Fatal("success before final owned Close")
	default:
	}
	once.Do(func() { close(release) })
	select {
	case a := <-done:
		joined = true
		if a.err != nil || !a.r.DesiredGrantsReconciled {
			t.Fatal("joined response rejected", a.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("final owned call not joined")
	}
	if p.ResponseCloseState().Pending != 0 {
		t.Fatal("pending response retained")
	}
}

func TestAPIKeyProvisionerCancellationAndSystemTLS(t *testing.T) {
	entered, returned := make(chan struct{}), make(chan struct{})
	p, f := provisionerTestClient(t, func(_ int, rw http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(returned)
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type answer struct {
		r   APIKeyProvisionerResult
		err error
	}
	done := make(chan answer, 1)
	joined := false
	defer func() {
		cancel()
		if !joined {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("owned request did not join")
			}
		}
	}()
	prepared := provisionerTestPrepared(t, p)
	go func() { r, err := p.Reconcile(ctx, provisionerTestToken, prepared); done <- answer{r, err} }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request not entered")
	}
	cancel()
	select {
	case a := <-done:
		joined = true
		provisionerTestReject(t, a.r, a.err, "trust", 1, f)
		if a.r.Outcome != APIKeyProvisionerNotAttempted {
			t.Fatal("metadata-read cancellation claims write")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request not canceled")
	}
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("owned peer not joined")
	}
	var calls atomic.Int32
	peer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(peer.Close)
	strict, err := NewAPIKeyProvisioner(provisionerTestConfig(peer.URL), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(strict.Close)
	r, err := strict.Reconcile(t.Context(), provisionerTestToken, provisionerTestPrepared(t, strict))
	if err == nil || r.DesiredGrantsReconciled || calls.Load() != 0 {
		t.Fatal("untrusted certificate bypassed")
	}
}
