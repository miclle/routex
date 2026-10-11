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

const machineTestToken = "machine-private-token"
const machineTestAccessor = "machine-private-accessor"
const machineTestWriterPath = "kv/data/applications/profile_1/routex-api-key-key_1"

var machineTestProvisionerPaths = []string{"auth/workload/config", "sys/policies/acl/routex-api-key-profile_1", "auth/workload/role/routex-api-key-principal_1"}

func machineTestConfig(endpoint string) APIKeyMachineValidatorConfig {
	return APIKeyMachineValidatorConfig{Endpoint: endpoint, Namespace: "tenant", KVMount: "kv", KeyPrefix: "applications", AuthMount: "workload"}
}
func machineTestInput(purpose APIKeyMachinePurpose) APIKeyMachineValidationInput {
	v := APIKeyMachineValidationInput{Purpose: purpose, ProfileID: "profile_1", PrincipalID: "principal_1"}
	if purpose == APIKeyMachineWriter {
		v.ResourceID = "key_1"
	}
	return v
}
func machineTestIdentity() map[string]any {
	return map[string]any{
		"id": machineTestToken, "accessor": machineTestAccessor, "policies": []string{"writer-metadata"}, "path": "auth/token/create", "meta": map[string]string{"private": "private-metadata"}, "display_name": "private-display", "num_uses": 0, "orphan": true, "creation_time": 1700000000, "creation_ttl": 600, "expire_time": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339Nano), "ttl": 600, "explicit_max_ttl": 0, "entity_id": "private-entity", "type": "service", "identity_policies": []string{}, "external_namespace_policies": map[string][]string{},
	}
}
func machineTestCaps(purpose APIKeyMachinePurpose) map[string]any {
	if purpose == APIKeyMachineWriter {
		return map[string]any{machineTestWriterPath: []string{"create"}, "capabilities": []string{"create"}}
	}
	return map[string]any{machineTestProvisionerPaths[0]: []string{"read"}, machineTestProvisionerPaths[1]: []string{"update", "read"}, machineTestProvisionerPaths[2]: []string{"update", "create", "read"}}
}
func machineTestEnvelope(data map[string]any) map[string]any {
	return map[string]any{"request_id": "safe-id", "lease_id": "", "renewable": false, "lease_duration": 0, "data": data, "auth": nil, "wrap_info": nil, "warnings": nil, "mount_type": "system"}
}
func machineTestEncoded(v any) string { raw, _ := json.Marshal(v); return string(raw) }
func machineTestReply(i int, purpose APIKeyMachinePurpose, mode string, rw http.ResponseWriter) {
	rw.Header().Set("Content-Type", "application/json")
	if i == 0 {
		_, _ = io.WriteString(rw, machineTestEncoded(machineTestEnvelope(machineTestIdentity())))
		return
	}
	data := machineTestCaps(purpose)
	var v map[string]any
	switch mode {
	case "flat":
		v = data
	case "aliases":
		v = machineTestEnvelope(data)
		for k, r := range data {
			v[k] = r
		}
	default:
		v = machineTestEnvelope(data)
	}
	_, _ = io.WriteString(rw, machineTestEncoded(v))
}

type machineTestPeer struct {
	mu     sync.Mutex
	paths  []string
	bodies [][]byte
	valid  bool
}

func (f *machineTestPeer) snapshot() ([]string, [][]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...), append([][]byte(nil), f.bodies...), f.valid
}
func machineTestClient(t *testing.T, purpose APIKeyMachinePurpose, mode string, override func(int, http.ResponseWriter, *http.Request)) (*APIKeyMachineValidator, *machineTestPeer) {
	t.Helper()
	f := &machineTestPeer{valid: true}
	peer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 4097))
		_ = r.Body.Close()
		f.mu.Lock()
		i := len(f.paths)
		f.paths = append(f.paths, r.Method+" "+r.URL.Path)
		f.bodies = append(f.bodies, raw)
		if err != nil || len(raw) > 4096 || r.URL.RawQuery != "" || r.Header.Get("X-Vault-Token") != machineTestToken || r.Header.Get("X-Vault-Request") != "true" || r.Header.Get("X-Vault-Namespace") != "tenant" {
			f.valid = false
		}
		f.mu.Unlock()
		if override != nil {
			override(i, rw, r)
		} else {
			machineTestReply(i, purpose, mode, rw)
		}
	}))
	t.Cleanup(peer.Close)
	v, err := NewAPIKeyMachineValidator(machineTestConfig(peer.URL), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	return v, f
}
func machineTestPrepared(t *testing.T, v *APIKeyMachineValidator, purpose APIKeyMachinePurpose) *PreparedAPIKeyMachineValidation {
	t.Helper()
	p, err := v.Prepare(machineTestInput(purpose))
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func machineTestRejected(t *testing.T, r APIKeyMachineValidationResult, err error, calls int, f *machineTestPeer) {
	t.Helper()
	var failure *Failure
	if err == nil || r.Validated || r.MachineIdentityCommitment != "" || r.TTLSeconds != 0 || !errors.As(err, &failure) {
		t.Fatal("rejected proof became machine identity")
	}
	paths, _, valid := f.snapshot()
	want := []string{"GET /v1/auth/token/lookup-self", "POST /v1/sys/capabilities-self"}
	if !valid || len(paths) != calls || !reflect.DeepEqual(paths, want[:calls]) {
		t.Fatal("unexpected/repeated metadata operation")
	}
	for _, private := range []string{machineTestToken, machineTestAccessor, "private-metadata", "private-display", "private-entity"} {
		if strings.Contains(fmt.Sprintf("%v %#v %v", r, r, err), private) {
			t.Fatal("private metadata escaped")
		}
	}
}

func TestAPIKeyMachineValidatorNativeMetadataAndPurpose(t *testing.T) {
	for _, purpose := range []APIKeyMachinePurpose{APIKeyMachineWriter, APIKeyMachineProvisioner} {
		for _, mode := range []string{"normal", "aliases", "flat"} {
			t.Run(string(purpose)+"_"+mode, func(t *testing.T) {
				v, f := machineTestClient(t, purpose, mode, nil)
				p := machineTestPrepared(t, v, purpose)
				plan := p.Plan()
				// Returned slices must not mutate the internally captured plan or body.
				plan.Grants[0].Path = "unrelated"
				plan.Grants[0].Actions[0] = "root"
				started := time.Now()
				r, err := v.Validate(t.Context(), machineTestToken, p)
				if err != nil || !r.Validated || !r.Consumed || r.TokenType != "service" || r.TTLSeconds != 600 || len(r.MachineIdentityCommitment) != 64 || r.Plan.Purpose != purpose || r.Plan.SHA256 != p.Plan().SHA256 || !r.ExpiresAt.After(started) || r.ExpiresAt.After(time.Now().Add(600*time.Second)) {
					t.Fatal("native metadata rejected", err)
				}
				paths, bodies, valid := f.snapshot()
				if !valid || !reflect.DeepEqual(paths, []string{"GET /v1/auth/token/lookup-self", "POST /v1/sys/capabilities-self"}) || len(bodies[0]) != 0 {
					t.Fatal("fixed operation scope changed")
				}
				var request struct {
					Paths []string `json:"paths"`
				}
				if json.Unmarshal(bodies[1], &request) != nil {
					t.Fatal("capability body")
				}
				want := machineTestProvisionerPaths
				if purpose == APIKeyMachineWriter {
					want = []string{machineTestWriterPath}
				}
				if !reflect.DeepEqual(request.Paths, want) {
					t.Fatal("generated capability paths changed")
				}
				if !r.Lookup.Succeeded || !r.CapabilityCheck.Succeeded || v.ResponseCloseState().Pending != 0 || v.ResponseCloseState().Failed {
					t.Fatal("joined proof missing")
				}
				raw, _ := json.Marshal(r)
				if string(raw) != "{}" {
					t.Fatal("private result serialized")
				}
				raw, _ = json.Marshal(p)
				if string(raw) != "{}" {
					t.Fatal("prepared material serialized")
				}
				r.Grants[0].Actions[0] = "root"
				if p.Plan().Grants[0].Actions[0] == "root" {
					t.Fatal("result slice aliases private plan")
				}
			})
		}
	}
	// Observe both purposes against the same real peer/namespace/accessor. A purpose
	// salt would incorrectly make one actual machine look like two identities.
	var commonCalls atomic.Int32
	peer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		commonCalls.Add(1)
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 4097))
		_ = r.Body.Close()
		if r.Method == http.MethodGet && r.URL.Path == "/v1/auth/token/lookup-self" {
			machineTestReply(0, APIKeyMachineWriter, "normal", rw)
			return
		}
		var body struct {
			Paths []string `json:"paths"`
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sys/capabilities-self" || json.Unmarshal(raw, &body) != nil {
			t.Error("unexpected common-machine request")
			rw.WriteHeader(400)
			return
		}
		purpose := APIKeyMachineProvisioner
		if reflect.DeepEqual(body.Paths, []string{machineTestWriterPath}) {
			purpose = APIKeyMachineWriter
		} else if !reflect.DeepEqual(body.Paths, machineTestProvisionerPaths) {
			t.Error("unexpected common-machine scope")
			rw.WriteHeader(400)
			return
		}
		machineTestReply(1, purpose, "aliases", rw)
	}))
	t.Cleanup(peer.Close)
	common, err := NewAPIKeyMachineValidator(machineTestConfig(peer.URL), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(common.Close)
	writer, err := common.Validate(t.Context(), machineTestToken, machineTestPrepared(t, common, APIKeyMachineWriter))
	if err != nil {
		t.Fatal(err)
	}
	provisioner, err := common.Validate(t.Context(), machineTestToken, machineTestPrepared(t, common, APIKeyMachineProvisioner))
	if err != nil || writer.MachineIdentityCommitment != provisioner.MachineIdentityCommitment || writer.Plan.Purpose == provisioner.Plan.Purpose || writer.Plan.SHA256 == provisioner.Plan.SHA256 || commonCalls.Load() != 4 {
		t.Fatal("same identity or independent purpose/plan binding lost", err)
	}
	cfg := machineTestConfig("https://vault.example")
	started := time.Now()
	raw := []byte(machineTestEncoded(machineTestEnvelope(machineTestIdentity())))
	a, _, _, ok := machineIdentity(raw, cfg, started)
	if !ok {
		t.Fatal("identity fixture invalid")
	}
	b, _, _, ok := machineIdentity(raw, cfg, started)
	if !ok || a != b {
		t.Fatal("same accessor lost common identity")
	}
	cfg.Namespace = "other"
	c, _, _, ok := machineIdentity(raw, cfg, started)
	if !ok || a == c {
		t.Fatal("namespace not bound")
	}
	cfg.Namespace = "tenant"
	cfg.Endpoint = "https://other.example"
	c, _, _, ok = machineIdentity(raw, cfg, started)
	if !ok || a == c {
		t.Fatal("endpoint not bound")
	}
}
func TestAPIKeyMachineValidatorAdmissionAndCopiedClaim(t *testing.T) {
	v, f := machineTestClient(t, APIKeyMachineWriter, "normal", nil)
	for _, input := range []APIKeyMachineValidationInput{{Purpose: "reader", ProfileID: "profile_1", PrincipalID: "principal_1"}, {Purpose: APIKeyMachineWriter, ProfileID: "p/*", PrincipalID: "principal_1", ResourceID: "key_1"}, {Purpose: APIKeyMachineWriter, ProfileID: "profile_1", PrincipalID: "principal_1"}, {Purpose: APIKeyMachineProvisioner, ProfileID: "profile_1", PrincipalID: "principal_1", ResourceID: "key_1"}} {
		if p, err := v.Prepare(input); err == nil || p != nil {
			t.Fatal("arbitrary capability plan admitted")
		}
	}
	p := machineTestPrepared(t, v, APIKeyMachineWriter)
	//lint:ignore SA1012 The public nil-context guard is deliberately exercised.
	//nolint:staticcheck // Deliberate invalid API input.
	r, err := v.Validate(nil, machineTestToken, p)
	if err == nil || r.Consumed {
		t.Fatal("nil context claimed")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	r, err = v.Validate(canceled, machineTestToken, p)
	if err == nil || r.Consumed {
		t.Fatal("pre-canceled plan claimed")
	}
	r, err = v.Validate(t.Context(), "", p)
	if err == nil || r.Consumed {
		t.Fatal("empty Token claimed")
	}
	cfg := v.config
	cfg.Namespace = "other"
	other, err := NewAPIKeyMachineValidator(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	r, err = other.Validate(t.Context(), machineTestToken, p)
	if err == nil || r.Consumed {
		t.Fatal("plan crossed namespace")
	}
	copied := *p
	done := make(chan APIKeyMachineValidationResult, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	ctx, cancelClaims := context.WithCancel(t.Context())
	defer cancelClaims()
	for _, plan := range []*PreparedAPIKeyMachineValidation{p, &copied} {
		wg.Go(func() { <-start; r, _ := v.Validate(ctx, machineTestToken, plan); done <- r })
	}
	close(start)
	joined := make(chan struct{})
	go func() { wg.Wait(); close(joined) }()
	defer func() {
		cancelClaims()
		select {
		case <-joined:
		case <-time.After(2 * time.Second):
			t.Error("owned claim cleanup not joined")
		}
	}()
	select {
	case <-joined:
	case <-time.After(2 * time.Second):
		t.Fatal("owned claims not joined")
	}
	a, b := <-done, <-done
	if a.Validated == b.Validated || a.Consumed == b.Consumed {
		t.Fatal("copied plans claimed twice")
	}
	paths, _, _ := f.snapshot()
	if len(paths) != 2 {
		t.Fatal("copied plan replayed")
	}
	closed := machineTestPrepared(t, v, APIKeyMachineWriter)
	closed.Close()
	r, err = v.Validate(t.Context(), machineTestToken, closed)
	if err == nil || r.Consumed {
		t.Fatal("closed plan claimed")
	}
	fresh := machineTestPrepared(t, v, APIKeyMachineWriter)
	v.Close()
	r, err = v.Validate(t.Context(), machineTestToken, fresh)
	if err == nil || r.Consumed {
		t.Fatal("closed validator claimed")
	}
}
func TestAPIKeyMachineValidatorRejectsIdentityAmbiguity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"batch", func(d map[string]any) { d["type"] = "batch" }}, {"missing_type", func(d map[string]any) { delete(d, "type") }}, {"missing_accessor", func(d map[string]any) { delete(d, "accessor") }}, {"empty_accessor", func(d map[string]any) { d["accessor"] = "" }}, {"accessor_whitespace", func(d map[string]any) { d["accessor"] = " accessor" }}, {"accessor_oversize", func(d map[string]any) { d["accessor"] = strings.Repeat("x", 257) }},
		{"ttl_zero", func(d map[string]any) { d["ttl"] = 0 }}, {"ttl_null", func(d map[string]any) { d["ttl"] = nil }}, {"ttl_string", func(d map[string]any) { d["ttl"] = "600" }}, {"expiry_nil", func(d map[string]any) { d["expire_time"] = nil }}, {"expiry_missing", func(d map[string]any) { delete(d, "expire_time") }}, {"expiry_past", func(d map[string]any) { d["expire_time"] = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano) }}, {"expiry_invalid", func(d map[string]any) { d["expire_time"] = "tomorrow" }},
		{"root", func(d map[string]any) { d["policies"] = []string{"root"} }}, {"identity_root", func(d map[string]any) { d["identity_policies"] = []string{"root"} }}, {"namespace_root", func(d map[string]any) { d["external_namespace_policies"] = map[string][]string{"ns": []string{"root"}} }}, {"malformed_policies", func(d map[string]any) { d["policies"] = map[string]string{"grant": "root"} }}, {"malformed_identity", func(d map[string]any) { d["identity_policies"] = true }}, {"malformed_external", func(d map[string]any) { d["external_namespace_policies"] = []string{"root"} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, f := machineTestClient(t, APIKeyMachineWriter, "normal", func(_ int, rw http.ResponseWriter, _ *http.Request) {
				d := machineTestIdentity()
				tc.mutate(d)
				rw.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(rw, machineTestEncoded(machineTestEnvelope(d)))
			})
			r, err := v.Validate(t.Context(), machineTestToken, machineTestPrepared(t, v, APIKeyMachineWriter))
			machineTestRejected(t, r, err, 1, f)
		})
	}
	for _, tc := range []struct{ name, raw string }{{"duplicate", `{"data":{"type":"service","type":"batch"}}`}, {"escaped_duplicate", `{"data":{"type":"service","\u0074ype":"batch"}}`}, {"surrogate", `{"data":{"accessor":"\ud800"}}`}, {"utf8", "{\"data\":\"\xff\"}"}, {"depth", `{"data":` + strings.Repeat("[", 17) + `0` + strings.Repeat("]", 17) + `}`}} {
		t.Run(tc.name, func(t *testing.T) {
			v, f := machineTestClient(t, APIKeyMachineWriter, "normal", func(_ int, rw http.ResponseWriter, _ *http.Request) {
				rw.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(rw, tc.raw)
			})
			r, err := v.Validate(t.Context(), machineTestToken, machineTestPrepared(t, v, APIKeyMachineWriter))
			machineTestRejected(t, r, err, 1, f)
		})
	}
}
func TestAPIKeyMachineValidatorRejectsCapabilitiesAndAliases(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"writer_read", func(d map[string]any) { d[machineTestWriterPath] = []string{"create", "read"} }}, {"writer_update", func(d map[string]any) { d[machineTestWriterPath] = []string{"create", "update"} }}, {"sudo", func(d map[string]any) { d[machineTestWriterPath] = []string{"create", "sudo"} }}, {"root", func(d map[string]any) { d[machineTestWriterPath] = []string{"root"} }}, {"deny", func(d map[string]any) { d[machineTestWriterPath] = []string{"deny"} }}, {"missing", func(d map[string]any) { delete(d, machineTestWriterPath) }}, {"extra_path", func(d map[string]any) { d["kv/data/neighbor"] = []string{"create"} }}, {"duplicate_action", func(d map[string]any) { d[machineTestWriterPath] = []string{"create", "create"} }}, {"wrong_alias", func(d map[string]any) { d["capabilities"] = []string{"read"} }}, {"missing_alias", func(d map[string]any) { delete(d, "capabilities") }}, {"wrong_type", func(d map[string]any) { d[machineTestWriterPath] = "create" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, f := machineTestClient(t, APIKeyMachineWriter, "normal", func(i int, rw http.ResponseWriter, _ *http.Request) {
				if i == 0 {
					machineTestReply(i, APIKeyMachineWriter, "normal", rw)
					return
				}
				d := machineTestCaps(APIKeyMachineWriter)
				tc.mutate(d)
				rw.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(rw, machineTestEncoded(machineTestEnvelope(d)))
			})
			r, err := v.Validate(t.Context(), machineTestToken, machineTestPrepared(t, v, APIKeyMachineWriter))
			machineTestRejected(t, r, err, 2, f)
		})
	}
	for _, mode := range []string{"partial_alias", "conflicting_alias", "extra_alias", "nonempty_auth", "warning", "lease"} {
		t.Run(mode, func(t *testing.T) {
			v, f := machineTestClient(t, APIKeyMachineWriter, "normal", func(i int, rw http.ResponseWriter, _ *http.Request) {
				if i == 0 {
					machineTestReply(i, APIKeyMachineWriter, "normal", rw)
					return
				}
				data := machineTestCaps(APIKeyMachineWriter)
				root := machineTestEnvelope(data)
				switch mode {
				case "partial_alias":
					root[machineTestWriterPath] = []string{"create"}
				case "conflicting_alias":
					root[machineTestWriterPath] = []string{"read"}
					root["capabilities"] = []string{"read"}
				case "extra_alias":
					root["foreign"] = []string{"create"}
				case "nonempty_auth":
					root["auth"] = map[string]string{"client_token": "private"}
				case "warning":
					root["warnings"] = []string{"private"}
				case "lease":
					root["lease_duration"] = 1
				}
				rw.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(rw, machineTestEncoded(root))
			})
			r, err := v.Validate(t.Context(), machineTestToken, machineTestPrepared(t, v, APIKeyMachineWriter))
			machineTestRejected(t, r, err, 2, f)
		})
	}
	for _, index := range []int{0, 1, 2} {
		t.Run(fmt.Sprint("provisioner_", index), func(t *testing.T) {
			v, f := machineTestClient(t, APIKeyMachineProvisioner, "normal", func(i int, rw http.ResponseWriter, _ *http.Request) {
				if i == 0 {
					machineTestReply(i, APIKeyMachineProvisioner, "normal", rw)
					return
				}
				d := machineTestCaps(APIKeyMachineProvisioner)
				d[machineTestProvisionerPaths[index]] = []string{"read", "create", "update", "delete"}
				rw.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(rw, machineTestEncoded(machineTestEnvelope(d)))
			})
			r, err := v.Validate(t.Context(), machineTestToken, machineTestPrepared(t, v, APIKeyMachineProvisioner))
			machineTestRejected(t, r, err, 2, f)
		})
	}
}
func TestAPIKeyMachineValidatorLossRedirectAndTLS(t *testing.T) {
	for _, at := range []int{0, 1} {
		t.Run(fmt.Sprint("loss_", at), func(t *testing.T) {
			var dropped atomic.Bool
			v, f := machineTestClient(t, APIKeyMachineWriter, "normal", func(i int, rw http.ResponseWriter, _ *http.Request) {
				if i != at {
					machineTestReply(i, APIKeyMachineWriter, "normal", rw)
					return
				}
				conn, _, err := rw.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				dropped.Store(true)
				_ = conn.Close()
			})
			p := machineTestPrepared(t, v, APIKeyMachineWriter)
			r, err := v.Validate(t.Context(), machineTestToken, p)
			machineTestRejected(t, r, err, at+1, f)
			if !dropped.Load() || !v.ResponseCloseState().Failed {
				t.Fatal("lost response closure certified")
			}
			r, err = v.Validate(t.Context(), machineTestToken, p)
			if err == nil || r.Consumed || r.Lookup.Attempted {
				t.Fatal("lost validation replayed")
			}
		})
	}
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var targetCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls.Add(1) }))
			t.Cleanup(target.Close)
			v, f := machineTestClient(t, APIKeyMachineWriter, "normal", func(_ int, rw http.ResponseWriter, r *http.Request) { http.Redirect(rw, r, target.URL, status) })
			r, err := v.Validate(t.Context(), machineTestToken, machineTestPrepared(t, v, APIKeyMachineWriter))
			machineTestRejected(t, r, err, 1, f)
			if targetCalls.Load() != 0 {
				t.Fatal("metadata redirect followed")
			}
		})
	}
	var calls atomic.Int32
	peer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(peer.Close)
	v, err := NewAPIKeyMachineValidator(machineTestConfig(peer.URL), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	r, err := v.Validate(t.Context(), machineTestToken, machineTestPrepared(t, v, APIKeyMachineWriter))
	if err == nil || r.Validated || calls.Load() != 0 {
		t.Fatal("private mode bypassed TLS trust")
	}
}

type machineTestTransport func(*http.Request) (*http.Response, error)

func (f machineTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type machineTestBody struct {
	io.Reader
	read, closed int
	close        func() error
}

func (b *machineTestBody) Read(p []byte) (int, error) {
	n, e := b.Reader.Read(p)
	b.read += n
	return n, e
}
func (b *machineTestBody) Close() error {
	b.closed++
	if b.close != nil {
		return b.close()
	}
	return nil
}
func machineTestResponse(i int) *http.Response {
	rw := httptest.NewRecorder()
	machineTestReply(i, APIKeyMachineWriter, "normal", rw)
	return rw.Result()
}
func TestAPIKeyMachineValidatorOriginalDeadlineAndClose(t *testing.T) {
	for _, mode := range []string{"parent", "sdk", "cancel_close", "close_error", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			v, err := NewAPIKeyMachineValidator(machineTestConfig("https://vault.example"), false)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(v.Close)
			budget := 3 * time.Second
			if mode == "sdk" {
				budget = 30 * time.Second
			}
			ctx, cancel := context.WithTimeout(t.Context(), budget)
			defer cancel()
			var deadline time.Time
			calls := 0
			var bodies []*machineTestBody
			v.client.http.Transport = machineTestTransport(func(req *http.Request) (*http.Response, error) {
				i := calls
				calls++
				d, ok := req.Context().Deadline()
				if !ok {
					t.Error("missing original deadline")
				}
				if i == 0 {
					deadline = d
				} else if !deadline.Equal(d) {
					t.Error("deadline renewed")
				}
				if req.GetBody != nil || !req.Close {
					t.Error("replayable request")
				}
				if mode != "sdk" {
					parent, _ := ctx.Deadline()
					if !d.Equal(parent) {
						t.Error("parent cap ignored")
					}
				}
				r := machineTestResponse(i)
				inner := r.Body
				body := &machineTestBody{Reader: inner}
				if mode == "oversize" {
					body.Reader = strings.NewReader(strings.Repeat("x", maxResponseBytes*2))
				}
				body.close = func() error {
					if i == 1 && mode == "cancel_close" {
						cancel()
					}
					if i == 1 && mode == "close_error" {
						return errors.New("private-close-error")
					}
					return inner.Close()
				}
				bodies = append(bodies, body)
				r.Body = body
				return r, nil
			})
			started := time.Now()
			r, err := v.Validate(ctx, machineTestToken, machineTestPrepared(t, v, APIKeyMachineWriter))
			finished := time.Now()
			if mode == "parent" || mode == "sdk" {
				if err != nil || !r.Validated || calls != 2 {
					t.Fatal("normal closed validation failed", err)
				}
			} else if err == nil || r.Validated {
				t.Fatal("bounded/error close accepted")
			}
			if mode == "sdk" && (deadline.Before(started.Add(requestTimeout)) || deadline.After(finished.Add(requestTimeout))) {
				t.Fatal("SDK cap renewed")
			}
			for _, b := range bodies {
				if b.closed != 1 {
					t.Fatal("body Close not joined exactly once")
				}
			}
			if mode == "oversize" && (calls != 1 || bodies[0].read != maxResponseBytes+1) {
				t.Fatal("response read unbounded")
			}
			if v.ResponseCloseState().Pending != 0 {
				t.Fatal("pending body leaked")
			}
		})
	}
}
func TestAPIKeyMachineValidatorCancellationBothPhases(t *testing.T) {
	for _, at := range []int{0, 1} {
		t.Run(fmt.Sprint(at), func(t *testing.T) {
			entered, peerDone := make(chan struct{}), make(chan struct{})
			v, f := machineTestClient(t, APIKeyMachineWriter, "normal", func(i int, rw http.ResponseWriter, r *http.Request) {
				if i != at {
					machineTestReply(i, APIKeyMachineWriter, "normal", rw)
					return
				}
				close(entered)
				<-r.Context().Done()
				close(peerDone)
			})
			ctx, cancel := context.WithCancel(t.Context())
			type answer struct {
				r   APIKeyMachineValidationResult
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
						t.Error("owned canceled operation not joined")
					}
				}
			}()
			prepared := machineTestPrepared(t, v, APIKeyMachineWriter)
			go func() { r, err := v.Validate(ctx, machineTestToken, prepared); done <- answer{r, err} }()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("metadata request not entered")
			}
			cancel()
			select {
			case a := <-done:
				joined = true
				machineTestRejected(t, a.r, a.err, at+1, f)
			case <-time.After(2 * time.Second):
				t.Fatal("metadata cancel not joined")
			}
			select {
			case <-peerDone:
			case <-time.After(2 * time.Second):
				t.Fatal("controlled peer not joined")
			}
		})
	}
}
func TestAPIKeyMachineValidatorHeldFinalClose(t *testing.T) {
	v, err := NewAPIKeyMachineValidator(machineTestConfig("https://vault.example"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	calls := 0
	v.client.http.Transport = machineTestTransport(func(*http.Request) (*http.Response, error) {
		i := calls
		calls++
		r := machineTestResponse(i)
		if i == 1 {
			inner := r.Body
			r.Body = &machineTestBody{Reader: inner, close: func() error { close(entered); <-release; return inner.Close() }}
		}
		return r, nil
	})
	type answer struct {
		r   APIKeyMachineValidationResult
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
				t.Error("held Close operation not joined")
			}
		}
	}()
	p := machineTestPrepared(t, v, APIKeyMachineWriter)
	go func() { r, err := v.Validate(t.Context(), machineTestToken, p); done <- answer{r, err} }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("final Close not entered")
	}
	if v.ResponseCloseState().Pending != 1 {
		t.Fatal("held Close ownership missing")
	}
	select {
	case <-done:
		t.Fatal("machine proof before body joined")
	default:
	}
	once.Do(func() { close(release) })
	select {
	case a := <-done:
		joined = true
		if a.err != nil || !a.r.Validated {
			t.Fatal("joined Close rejected", a.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("held operation not joined")
	}
}
