package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func nativeDeliveryValidationPlan(endpoint, purpose string) APIKeyDeliveryValidationPlan {
	birth := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	p := APIKeyDeliveryValidationPlan{Purpose: purpose, AuthID: "kda_fixture", Generation: "kdg_fixture", IntegrationID: "vlt_fixture", IntegrationRevision: strings.Repeat("a", 64), VaultRevisionID: "vlr_fixture", AuthBirth: birth, IntegrationBirth: birth, VaultRevisionBirth: birth, Endpoint: endpoint, Namespace: "tenant", KVMount: "kv", KeyPrefix: "applications", AuthMount: "workload", ProfileID: "profile_1", PrincipalID: "principal_1", ResourceID: "key_1"}
	if purpose == "writer" {
		p.Capabilities = []APIKeyDeliveryCapability{{Path: "kv/data/applications/profile_1/routex-api-key-key_1", Actions: []string{"create"}}}
	} else {
		p.Capabilities = []APIKeyDeliveryCapability{{Path: "auth/workload/config", Actions: []string{"read"}}, {Path: "sys/policies/acl/routex-api-key-profile_1", Actions: []string{"read", "update"}}, {Path: "auth/workload/role/routex-api-key-principal_1", Actions: []string{"create", "read", "update"}}}
	}
	p.SHA256 = apiKeyDeliveryDigest("api-key-delivery:validation-plan:v1", p)
	return p
}
func nativeDeliveryValidationReply(t *testing.T, rw http.ResponseWriter, r *http.Request, purpose string) {
	t.Helper()
	rw.Header().Set("Content-Type", "application/json")
	if r.Header.Get("X-Vault-Token") != "private-machine-token" || r.Header.Get("X-Vault-Namespace") != "tenant" {
		t.Error("machine scope lost")
	}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/auth/token/lookup-self" {
		_ = json.NewEncoder(rw).Encode(map[string]any{"data": map[string]any{"type": "service", "accessor": "private-accessor", "ttl": 300, "expire_time": time.Now().Add(300 * time.Second).UTC().Format(time.RFC3339Nano), "policies": []string{"machine-policy"}}})
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != "/v1/sys/capabilities-self" {
		t.Error("unexpected metadata operation")
		rw.WriteHeader(400)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4097))
	if err != nil || len(raw) > 4096 {
		t.Error("unbounded request")
		rw.WriteHeader(400)
		return
	}
	var body struct {
		Paths []string `json:"paths"`
	}
	if json.Unmarshal(raw, &body) != nil {
		t.Error("invalid body")
		rw.WriteHeader(400)
		return
	}
	want := []string{"kv/data/applications/profile_1/routex-api-key-key_1"}
	data := map[string][]string{want[0]: {"create"}, "capabilities": {"create"}}
	if purpose == "provisioner" {
		want = []string{"auth/workload/config", "sys/policies/acl/routex-api-key-profile_1", "auth/workload/role/routex-api-key-principal_1"}
		data = map[string][]string{want[0]: {"read"}, want[1]: {"read", "update"}, want[2]: {"create", "read", "update"}}
	}
	if !reflect.DeepEqual(want, body.Paths) {
		t.Error("generated scope changed")
		rw.WriteHeader(400)
		return
	}
	_ = json.NewEncoder(rw).Encode(map[string]any{"data": data})
}
func TestNativeAPIKeyDeliveryValidatorMapsOnlyExactSDKFacts(t *testing.T) {
	for _, purpose := range []string{"writer", "provisioner"} {
		t.Run(purpose, func(t *testing.T) {
			var calls atomic.Int32
			peer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				nativeDeliveryValidationReply(t, rw, r, purpose)
			}))
			t.Cleanup(peer.Close)
			p := nativeDeliveryValidationPlan(peer.URL, purpose)
			v := nativeAPIKeyDeliveryValidator{allowPrivate: true}
			facts, err := v.Validate(t.Context(), p, "private-machine-token")
			if err != nil || !apiKeyDeliveryValidationMatches(p, facts) || calls.Load() != 2 {
				t.Fatal("exact native proof rejected", err)
			}
			if facts.PlanSHA256 != p.SHA256 || facts.TokenType != "service" || facts.TTLSeconds != 300 || len(facts.MachineIdentityCommitment) != 64 {
				t.Fatal("safe projection changed")
			}
			facts.Capabilities[0].Actions[0] = "root"
			if p.Capabilities[0].Actions[0] == "root" {
				t.Fatal("facts alias admission plan")
			}
		})
	}
}
func TestNativeAPIKeyDeliveryValidatorRejectsTupleAndPathSubstitutionBeforeDo(t *testing.T) {
	var calls atomic.Int32
	peer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(peer.Close)
	for _, which := range []string{"digest", "auth_birth", "revision", "path", "grant", "mount", "purpose", "profile", "principal"} {
		t.Run(which, func(t *testing.T) {
			p := nativeDeliveryValidationPlan(peer.URL, "writer")
			switch which {
			case "digest":
				p.SHA256 = strings.Repeat("0", 64)
			case "auth_birth":
				p.AuthBirth = p.AuthBirth.Add(time.Microsecond)
			case "revision":
				p.VaultRevisionID += "_other"
			case "path":
				p.Capabilities[0].Path = "kv/data/neighbor"
			case "grant":
				p.Capabilities[0].Actions = []string{"create", "read"}
			case "mount":
				p.KVMount = "../kv"
			case "purpose":
				p.Purpose = "reader"
			case "profile":
				p.ProfileID = "neighbor"
			case "principal":
				p.PrincipalID = "../principal"
			}
			if which != "digest" && which != "auth_birth" && which != "revision" {
				p.SHA256 = ""
				p.SHA256 = apiKeyDeliveryDigest("api-key-delivery:validation-plan:v1", p)
			}
			facts, err := (nativeAPIKeyDeliveryValidator{allowPrivate: true}).Validate(t.Context(), p, "private-machine-token")
			if err == nil || facts.OwnIdentityObserved || facts.PlanSHA256 != "" || calls.Load() != 0 {
				t.Fatal("substituted plan reached SDK")
			}
		})
	}
}
func TestNativeAPIKeyDeliveryValidatorLossRetainsUnprovenClose(t *testing.T) {
	var calls atomic.Int32
	peer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		conn, _, err := rw.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	t.Cleanup(peer.Close)
	facts, err := (nativeAPIKeyDeliveryValidator{allowPrivate: true}).Validate(t.Context(), nativeDeliveryValidationPlan(peer.URL, "writer"), "private-machine-token")
	if err == nil || facts.OwnIdentityObserved || facts.PlanSHA256 != "" || !facts.CloseState.Failed || calls.Load() != 1 {
		t.Fatal("lost metadata response certified")
	}
}
func TestNativeAPIKeyDeliveryValidatorCanceledAndMissingAuthority(t *testing.T) {
	var calls atomic.Int32
	peer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(peer.Close)
	p := nativeDeliveryValidationPlan(peer.URL, "writer")
	v := nativeAPIKeyDeliveryValidator{allowPrivate: true}
	//lint:ignore SA1012 The public nil-context guard is deliberately exercised.
	//nolint:staticcheck // Deliberate invalid API input.
	if _, err := v.Validate(nil, p, "private-machine-token"); err == nil {
		t.Fatal("nil context admitted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := v.Validate(ctx, p, "private-machine-token"); err == nil {
		t.Fatal("canceled operation admitted")
	}
	if _, err := v.Validate(t.Context(), p, ""); err == nil {
		t.Fatal("empty Token admitted")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid authority reached network")
	}
	s := &Service{}
	WithNativeAPIKeyDeliveryValidator(true)(s)
	if s.apiKeyDeliveryValidator == nil {
		t.Fatal("explicit native option missing")
	}
}
