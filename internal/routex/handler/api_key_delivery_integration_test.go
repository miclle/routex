package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

// The peer is controlled protocol evidence. It is not real Vault, a caller JWT
// login/ACL test, browser evidence, or a separate executable restart. Infrastructure
// probes use different Tokens/paths/counters from the restricted delivery SDKs.
const deliveryFixtureAdminEmail = "delivery-admin@example.invalid"
const deliveryFixturePassword = "test-only-delivery-password"
const deliveryFixtureWriter = "fixture-delivery-writer"
const deliveryFixtureProvisioner = "fixture-delivery-provisioner"
const deliveryFixtureIssuer = "https://workload.example.invalid"
const deliveryFixtureJWKS = "https://workload.example.invalid/keys"
const deliveryFixtureSubject = "workload:delivery"
const deliveryFixtureModel = "mdl_delivery_fixture"
const deliveryFixtureModelName = "delivery-fixture"

type deliveryFixtureActor struct {
	Session SessionResponse
	Cookie  *http.Cookie
}
type deliveryFixtureHold struct {
	Entered chan string
	Release chan struct{}
	Joined  chan struct{}
	once    sync.Once
}

func (h *deliveryFixtureHold) release() { h.once.Do(func() { close(h.Release) }) }

type deliveryFixturePeer struct {
	t                                                       *testing.T
	db                                                      *gorm.DB
	Server                                                  *httptest.Server
	mu                                                      sync.Mutex
	probes                                                  map[string]map[string]string
	values                                                  map[string]string // test-local simulated caller receipt; never a Gateway GET
	policies                                                map[string]string
	roles                                                   map[string]map[string]any
	requests                                                []string // methods and fixed metadata paths only, no values/Tokens
	writes                                                  int
	reads, destroys, probeWrites, probeReads, probeDestroys int
	lostBody, lostHeader                                    bool
	hold                                                    *deliveryFixtureHold
	acknowledge                                             func()
	active                                                  sync.WaitGroup
}

func deliveryFixtureEnvelope(data any) map[string]any {
	return map[string]any{"request_id": "fixture-safe-id", "lease_id": "", "renewable": false, "lease_duration": 0, "data": data, "auth": nil, "wrap_info": nil, "warnings": nil}
}
func (p *deliveryFixturePeer) reply(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(deliveryFixtureEnvelope(data)); err != nil {
		p.t.Error("controlled peer response failed")
	}
}
func newDeliveryFixturePeer(t *testing.T, db *gorm.DB) *deliveryFixturePeer {
	t.Helper()
	p := &deliveryFixturePeer{t: t, db: db, probes: map[string]map[string]string{}, values: map[string]string{}, policies: map[string]string{}, roles: map[string]map[string]any{}}
	p.Server = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(func() {
		p.mu.Lock()
		h := p.hold
		p.mu.Unlock()
		if h != nil {
			h.release()
		}
		p.Server.Close()
		p.active.Wait()
		p.mu.Lock()
		clear(p.values)
		clear(p.probes)
		p.mu.Unlock()
	})
	return p
}
func (p *deliveryFixturePeer) reject(w http.ResponseWriter, code int) {
	p.t.Error("controlled peer rejected unexpected operation")
	w.WriteHeader(code)
}
func (p *deliveryFixturePeer) serve(w http.ResponseWriter, r *http.Request) {
	p.active.Add(1)
	defer p.active.Done()
	raw, err := io.ReadAll(io.LimitReader(r.Body, (32<<10)+1))
	_ = r.Body.Close()
	defer clear(raw)
	if err != nil || len(raw) > 32<<10 || r.Header.Get("X-Vault-Namespace") != "" || r.Header.Get("X-Vault-Request") != "true" {
		p.reject(w, 400)
		return
	}
	token, path := r.Header.Get("X-Vault-Token"), r.URL.Path
	// Infrastructure probes are deliberately not delivery identities or objects.
	if token == "fixture-infrastructure-writer" || token == "fixture-infrastructure-reader" {
		p.mu.Lock()
		defer p.mu.Unlock()
		if !strings.HasPrefix(path, "/v1/kv/") {
			p.reject(w, 400)
			return
		}
		switch r.Method {
		case "POST":
			var body struct {
				Data    map[string]string `json:"data"`
				Options struct {
					CAS int `json:"cas"`
				} `json:"options"`
			}
			if token != "fixture-infrastructure-writer" || r.URL.RawQuery != "" || json.Unmarshal(raw, &body) != nil || body.Options.CAS != 0 || len(body.Data) != 1 || body.Data["key"] == "" {
				p.reject(w, 400)
				return
			}
			if _, exists := p.probes[path]; exists {
				p.reject(w, 409)
				return
			}
			p.probes[path] = body.Data
			p.probeWrites++
			p.reply(w, map[string]any{"version": 1, "destroyed": false, "deletion_time": ""})
		case "GET":
			value, ok := p.probes[path]
			if token != "fixture-infrastructure-reader" || r.URL.RawQuery != "version=1" || !ok || len(raw) != 0 {
				p.reject(w, 400)
				return
			}
			p.probeReads++
			p.reply(w, map[string]any{"data": value, "metadata": map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}})
		case "PUT":
			var body struct {
				Versions []int `json:"versions"`
			}
			exact := strings.Replace(path, "/destroy/", "/data/", 1)
			if token != "fixture-infrastructure-writer" || path == exact || r.URL.RawQuery != "" || json.Unmarshal(raw, &body) != nil || !reflect.DeepEqual(body.Versions, []int{1}) || p.probes[exact] == nil {
				p.reject(w, 400)
				return
			}
			delete(p.probes, exact)
			p.probeDestroys++
			w.WriteHeader(204)
		default:
			p.reject(w, 400)
		}
		return
	}
	if token != deliveryFixtureWriter && token != deliveryFixtureProvisioner {
		p.reject(w, 403)
		return
	}
	if r.URL.RawQuery != "" {
		p.reject(w, 400)
		return
	}
	p.mu.Lock()
	p.requests = append(p.requests, r.Method+" "+path)
	p.mu.Unlock()
	if path == "/v1/auth/token/lookup-self" && r.Method == "GET" && len(raw) == 0 {
		p.reply(w, map[string]any{"id": token, "accessor": "private-accessor-" + token, "policies": []string{"fixture-machine"}, "path": "auth/token/create", "meta": map[string]string{}, "display_name": "fixture", "num_uses": 0, "orphan": true, "creation_time": 1700000000, "creation_ttl": 600, "expire_time": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339Nano), "ttl": 600, "explicit_max_ttl": 0, "entity_id": "", "type": "service", "identity_policies": []string{}, "external_namespace_policies": map[string][]string{}})
		return
	}
	if path == "/v1/sys/capabilities-self" && r.Method == "POST" {
		var body struct {
			Paths []string `json:"paths"`
		}
		if json.Unmarshal(raw, &body) != nil {
			p.reject(w, 400)
			return
		}
		caps := map[string]any{}
		if token == deliveryFixtureWriter {
			if len(body.Paths) != 1 || !strings.HasPrefix(body.Paths[0], "kv/data/applications/vap_") || (!strings.Contains(body.Paths[0], "/routex-api-key-key_") && !strings.Contains(body.Paths[0], "/routex-api-key-pky_")) {
				p.reject(w, 400)
				return
			}
			caps[body.Paths[0]] = []string{"create"}
			caps["capabilities"] = []string{"create"}
		} else {
			if len(body.Paths) != 3 || body.Paths[0] != "auth/workload/config" || !strings.HasPrefix(body.Paths[1], "sys/policies/acl/routex-api-key-vap_") || !strings.HasPrefix(body.Paths[2], "auth/workload/role/routex-api-key-vpb_") {
				p.reject(w, 400)
				return
			}
			caps[body.Paths[0]] = []string{"read"}
			caps[body.Paths[1]] = []string{"read", "update"}
			caps[body.Paths[2]] = []string{"create", "read", "update"}
		}
		p.reply(w, caps)
		return
	}
	if strings.HasPrefix(path, "/v1/kv/") {
		if r.Method == "GET" {
			p.mu.Lock()
			p.reads++
			p.mu.Unlock()
			p.reject(w, 403)
			return
		}
		if r.Method != "POST" || token != deliveryFixtureWriter || !strings.HasPrefix(path, "/v1/kv/data/applications/vap_") {
			p.mu.Lock()
			p.destroys++
			p.mu.Unlock()
			p.reject(w, 403)
			return
		}
		var body struct {
			Data    map[string]string `json:"data"`
			Options struct {
				CAS int `json:"cas"`
			} `json:"options"`
		}
		if json.Unmarshal(raw, &body) != nil || len(body.Data) != 1 || body.Options.CAS != 0 || body.Data["key"] == "" {
			p.reject(w, 400)
			return
		}
		keyID := path[strings.LastIndex(path, "/routex-api-key-")+len("/routex-api-key-"):]
		var command entity.APIKeyDeliveryCommand
		var binding entity.APIKeyVaultDeliveryBinding
		if e := p.db.Where("key_id = ?", keyID).Take(&command).Error; e != nil || command.DeliveryStatus != "pending" || command.ClaimedAt == nil || command.ClaimProcessID == "" || command.ClaimProcessBirth == nil || command.AcknowledgedAt != nil {
			p.reject(w, 500)
			return
		}
		if e := p.db.Take(&binding, "id = ?", command.BindingID).Error; e != nil || binding.Version != 0 || binding.AcknowledgedAt != nil || "/v1/"+binding.Mount+"/data/"+binding.Path != path {
			p.reject(w, 500)
			return
		}
		var hash string
		if command.OwnerKind == "personal" {
			var key entity.APIKey
			if e := p.db.Take(&key, "id = ?", keyID).Error; e != nil || key.Status != entity.KeyPending || key.DeliveryMode != "managed_vault" {
				p.reject(w, 500)
				return
			}
			hash = key.TokenHash
		} else {
			var key entity.ProjectKey
			if e := p.db.Take(&key, "id = ?", keyID).Error; e != nil || key.Status != entity.KeyPending || key.DeliveryMode != "managed_vault" {
				p.reject(w, 500)
				return
			}
			hash = key.TokenHash
		}
		if hash != secret.SHA256Hex(body.Data["key"]) {
			p.reject(w, 500)
			return
		}
		p.mu.Lock()
		if _, exists := p.values[keyID]; exists {
			p.mu.Unlock()
			p.reject(w, 409)
			return
		}
		p.values[keyID] = body.Data["key"]
		p.writes++
		h, bodyLoss, headerLoss, ack := p.hold, p.lostBody, p.lostHeader, p.acknowledge
		p.hold = nil
		p.lostBody = false
		p.lostHeader = false
		p.acknowledge = nil
		p.mu.Unlock()
		if h != nil {
			defer close(h.Joined)
			h.Entered <- keyID
			select {
			case <-h.Release:
			case <-r.Context().Done():
				return
			}
		}
		if headerLoss {
			conn, _, e := w.(http.Hijacker).Hijack()
			if e != nil {
				p.t.Error("controlled pre-header close failed")
				return
			}
			_ = conn.Close()
			return
		}
		if bodyLoss {
			conn, buf, e := w.(http.Hijacker).Hijack()
			if e != nil {
				p.t.Error("controlled body close failed")
				return
			}
			_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 64\r\nConnection: close\r\n\r\n{\"data\":")
			_ = buf.Flush()
			_ = conn.Close()
			return
		}
		// Encode the genuine exact version1 envelope before arming the local
		// publication-only outage. The SDK still verifies the actual reply; this
		// callback never supplies acknowledgement or READY facts to the Service.
		reply, e := json.Marshal(deliveryFixtureEnvelope(map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}))
		if e != nil {
			p.reject(w, 500)
			return
		}
		defer clear(reply)
		w.Header().Set("Content-Type", "application/json")
		if ack != nil {
			ack()
		}
		if _, e = w.Write(reply); e != nil {
			p.t.Error("controlled acknowledgement response failed")
		}
		return
	}
	if token != deliveryFixtureProvisioner {
		p.reject(w, 403)
		return
	}
	if path == "/v1/auth/workload/config" && r.Method == "GET" && len(raw) == 0 {
		p.reply(w, map[string]any{"bound_issuer": deliveryFixtureIssuer, "jwks_url": deliveryFixtureJWKS, "oidc_discovery_url": "", "jwks_ca_pem": "", "jwt_validation_pubkeys": []string{}, "jwks_pairs": nil})
		return
	}
	if strings.HasPrefix(path, "/v1/sys/policies/acl/routex-api-key-vap_") {
		name := strings.TrimPrefix(path, "/v1/sys/policies/acl/")
		profile := strings.TrimPrefix(name, "routex-api-key-")
		expected := "path \"kv/data/applications/" + profile + "/*\" {\n  capabilities = [\"read\"]\n}\n"
		p.mu.Lock()
		defer p.mu.Unlock()
		if r.Method == "PUT" {
			var body map[string]string
			if json.Unmarshal(raw, &body) != nil || len(body) != 1 || body["policy"] != expected {
				p.reject(w, 400)
				return
			}
			p.policies[name] = body["policy"]
			w.WriteHeader(204)
			return
		}
		if r.Method == "GET" && len(raw) == 0 && p.policies[name] == expected {
			p.reply(w, map[string]string{"name": name, "policy": expected})
			return
		}
		p.reject(w, 400)
		return
	}
	if strings.HasPrefix(path, "/v1/auth/workload/role/routex-api-key-vpb_") {
		principalID := strings.TrimPrefix(path, "/v1/auth/workload/role/routex-api-key-")
		var principal entity.VaultPrincipalBinding
		if e := p.db.Take(&principal, "id = ?", principalID).Error; e != nil {
			p.reject(w, 400)
			return
		}
		expected := map[string]any{"role_type": "jwt", "user_claim": "sub", "bound_subject": principal.Subject, "bound_audiences": []string{"routex-app"}, "token_policies": []string{"routex-api-key-" + principal.ProfileID}, "token_no_default_policy": true, "token_ttl": 120, "token_max_ttl": 300, "clock_skew_leeway": 60, "expiration_leeway": 150, "not_before_leeway": 150}
		encoded, _ := json.Marshal(expected)
		var independent map[string]any
		_ = json.Unmarshal(encoded, &independent)
		p.mu.Lock()
		defer p.mu.Unlock()
		if r.Method == "POST" {
			var body map[string]any
			if json.Unmarshal(raw, &body) != nil || !reflect.DeepEqual(body, independent) {
				p.reject(w, 400)
				return
			}
			p.roles[principalID] = independent
			w.WriteHeader(204)
			return
		}
		if r.Method == "GET" && len(raw) == 0 && reflect.DeepEqual(p.roles[principalID], independent) {
			p.reply(w, independent)
			return
		}
		p.reject(w, 400)
		return
	}
	p.reject(w, 400)
}
func (p *deliveryFixturePeer) counts() (int, int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.writes, p.reads, p.destroys
}
func (p *deliveryFixturePeer) bearer(t *testing.T, key string) string {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	v := p.values[key]
	if v == "" {
		t.Fatal("missing test-local caller receipt")
	}
	return v
}
func (p *deliveryFixturePeer) holdNext(t *testing.T) *deliveryFixtureHold {
	t.Helper()
	h := &deliveryFixtureHold{Entered: make(chan string, 1), Release: make(chan struct{}), Joined: make(chan struct{})}
	p.mu.Lock()
	if p.hold != nil {
		p.mu.Unlock()
		t.Fatal("overlapping controlled holds")
	}
	p.hold = h
	p.mu.Unlock()
	t.Cleanup(h.release)
	return h
}

type deliveryFixture struct {
	t        *testing.T
	db       *gorm.DB
	svc      *service.Service
	router   *fox.Engine
	store    *secretstore.Store
	peer     *deliveryFixturePeer
	admin    deliveryFixtureActor
	seq      atomic.Uint64
	stopped  map[*service.Service]bool
	unproven map[*service.Service]bool
}

func (f *deliveryFixture) uuid() string {
	return fmt.Sprintf("f2900000-0000-4000-8000-%012x", f.seq.Add(1))
}
func (f *deliveryFixture) request(a deliveryFixtureActor, method, path, etag string, body any) *httptest.ResponseRecorder {
	f.t.Helper()
	return f.requestContext(f.t.Context(), a, method, path, etag, body)
}
func (f *deliveryFixture) requestContext(ctx context.Context, a deliveryFixtureActor, method, path, etag string, body any) *httptest.ResponseRecorder {
	f.t.Helper()
	raw := []byte{}
	var e error
	if body != nil {
		raw, e = json.Marshal(body)
		if e != nil {
			f.t.Error("controlled request encoding failed")
			res := httptest.NewRecorder()
			res.WriteHeader(500)
			return res
		}
	}
	defer clear(raw)
	req := httptest.NewRequestWithContext(ctx, method, "http://routex.test"+path, bytes.NewReader(raw))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if a.Cookie != nil {
		req.AddCookie(a.Cookie)
	}
	if method != "GET" {
		req.Header.Set("Origin", "http://routex.test")
		req.Header.Set("X-CSRF-Token", a.Session.CSRFToken)
	}
	if etag != "" {
		req.Header.Set("If-Match", `"`+etag+`"`)
	}
	res := httptest.NewRecorder()
	f.router.ServeHTTP(res, req)
	f.peer.mu.Lock()
	leaked := false
	for _, bearer := range f.peer.values {
		if strings.Contains(res.Body.String(), bearer) {
			leaked = true
		}
	}
	f.peer.mu.Unlock()
	if leaked {
		f.t.Error("delivered bearer escaped HTTP projection")
	}
	for _, private := range []string{deliveryFixtureWriter, deliveryFixtureProvisioner, "fixture-infrastructure-writer", "fixture-infrastructure-reader", "private-accessor-", "token_hash", "auth_ciphertext"} {
		if strings.Contains(res.Body.String(), private) {
			f.t.Error("private delivery metadata escaped HTTP")
		}
	}
	if strings.Contains(path, "api-key-delivery") || strings.Contains(path, "delivery-") || strings.HasSuffix(path, "/managed") {
		if !strings.Contains(res.Header().Get("Cache-Control"), "no-store") || res.Header().Get("X-Content-Type-Options") != "nosniff" {
			f.t.Error("delivery response privacy headers absent")
		}
	}
	return res
}
func deliveryFixtureDecode[T any](t *testing.T, res *httptest.ResponseRecorder, status int) T {
	t.Helper()
	expectStatus(t, res, status)
	var v T
	if json.Unmarshal(res.Body.Bytes(), &v) != nil {
		t.Fatal("invalid safe delivery DTO")
	}
	var receipt struct {
		ReviewETag string `json:"review_etag"`
	}
	if json.Unmarshal(res.Body.Bytes(), &receipt) == nil && receipt.ReviewETag != "" && res.Header().Get("ETag") != `"`+receipt.ReviewETag+`"` {
		t.Fatal("review ETag mismatch")
	}
	return v
}
func (f *deliveryFixture) policy() service.APIKeyDeliveryPolicyView {
	return deliveryFixtureDecode[service.APIKeyDeliveryPolicyView](f.t, f.request(f.admin, "GET", "/api/v1/admin/api-key-delivery/policy", "", nil), 200)
}
func (f *deliveryFixture) context(a deliveryFixtureActor, base string) service.APIKeyDeliveryContextView {
	return deliveryFixtureDecode[service.APIKeyDeliveryContextView](f.t, f.request(a, "GET", base+"/delivery-context", "", nil), 200)
}
func (f *deliveryFixture) start(s *service.Service, name string) {
	f.t.Helper()
	if e := s.StartSystemInstance(f.t.Context(), service.SystemInstanceMetadata{Name: name, Hostname: "delivery.invalid", Version: "test", GoVersion: "test", OS: "test", Arch: "test"}); e != nil {
		f.t.Fatal(e)
	}
	f.t.Cleanup(func() {
		s.StopRuntime()
		if !f.stopped[s] {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if e := s.StopSystemInstance(ctx); f.unproven[s] {
				if e == nil {
					f.t.Error("expected unproven shutdown became positive closure")
				}
			} else if e != nil {
				f.t.Error("owned delivery generation closure unproven", e)
			}
		}
	})
}
func (f *deliveryFixture) stop(s *service.Service) error {
	s.StopRuntime()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	e := s.StopSystemInstance(ctx)
	if e == nil {
		f.stopped[s] = true
	}
	return e
}
func (f *deliveryFixture) newService() *service.Service {
	f.t.Helper()
	s, e := service.New(f.t.Context(), f.db, service.WithCredentialStorage(f.store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour), service.WithNativeAPIKeyDeliveryValidator(true))
	if e != nil {
		f.t.Fatal(e)
	}
	if e = s.InitializeSecretStore(f.t.Context()); e != nil {
		f.t.Fatal(e)
	}
	return s
}
func (f *deliveryFixture) install(s *service.Service) {
	f.svc = s
	f.router = fox.New()
	New(s).RegisterRoutes(f.router)
}
func newDeliveryFixture(t *testing.T, db *gorm.DB) *deliveryFixture {
	t.Helper()
	store, e := secretstore.New(bytes.Repeat([]byte{137}, 32))
	if e != nil {
		t.Fatal(e)
	}
	f := &deliveryFixture{t: t, db: db, store: store, stopped: map[*service.Service]bool{}, unproven: map[*service.Service]bool{}}
	f.peer = newDeliveryFixturePeer(t, db)
	s := f.newService()
	f.install(s)
	f.start(s, "API-Key delivery fixture")
	setup := identityRequest(f.router, "POST", "/api/v1/setup", `{"email":"`+deliveryFixtureAdminEmail+`","password":"`+deliveryFixturePassword+`","name":"Delivery administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	session, cookie := readIdentity(t, setup)
	f.admin = deliveryFixtureActor{session, cookie}
	return f
}
func (f *deliveryFixture) login() deliveryFixtureActor {
	f.t.Helper()
	r := identityRequest(f.router, "POST", "/api/v1/auth/login", `{"email":"`+deliveryFixtureAdminEmail+`","password":"`+deliveryFixturePassword+`"}`, nil, "")
	expectStatus(f.t, r, 200)
	s, c := readIdentity(f.t, r)
	return deliveryFixtureActor{s, c}
}
func (f *deliveryFixture) configure() {
	f.t.Helper()
	a := f.admin
	base := "/api/v1/admin/api-key-delivery"
	empty := deliveryFixtureDecode[service.APIKeyCallerTemplateView](f.t, f.request(a, "GET", base+"/caller-template", "", nil), 200)
	if empty.AuthMount != "" || empty.Issuer != "" || empty.JWKSURL != "" || empty.Audience != "" || len(empty.AllowedSubjects) != 0 || empty.TokenTTLSeconds != 0 || empty.TokenMaxTTLSeconds != 0 || empty.Verified || empty.ReviewETag != f.policy().ReviewETag {
		f.t.Fatal("unconfigured template projection drift")
	}
	infraBase := "/api/v1/admin/secrets/integrations"
	page := deliveryFixtureDecode[service.VaultIntegrationPage](f.t, f.request(a, "GET", infraBase, "", nil), 200)
	input := service.VaultConfigInput{RequestID: f.uuid(), Name: "Delivery infrastructure", Descriptor: service.VaultDescriptor{Endpoint: f.peer.Server.URL, Mount: "kv", Prefix: "applications", DataField: "key"}, WriterAuth: service.VaultAuthInput{Action: "replace", Token: "fixture-infrastructure-writer"}, ReaderAuth: service.VaultAuthInput{Action: "replace", Token: "fixture-infrastructure-reader"}, Reason: "Review infrastructure probe identities"}
	saved := deliveryFixtureDecode[service.VaultConfigResult](f.t, f.request(a, "POST", infraBase, page.ReviewETag, input), 200)
	target := infraBase + "/" + saved.IntegrationID
	current := deliveryFixtureDecode[service.VaultIntegrationView](f.t, f.request(a, "GET", target, "", nil), 200)
	probe := deliveryFixtureDecode[service.VaultProbeView](f.t, f.request(a, "POST", target+"/probes/write", current.ReviewETag, service.VaultStageInput{RequestID: f.uuid(), Reason: "Explicit infrastructure write"}), 200)
	finished := deliveryFixtureDecode[service.VaultProbeView](f.t, f.request(a, "POST", target+"/probes/"+probe.ID+"/read", probe.ReviewETag, service.VaultStageInput{RequestID: f.uuid(), Reason: "Explicit infrastructure read and cleanup"}), 200)
	if !finished.Write.Succeeded || !finished.Read.Succeeded || finished.Cleanup.State != "acknowledged" {
		f.t.Fatal("infrastructure probe not established")
	}
	var identities [2]string
	for i, purpose := range []string{"writer", "provisioner"} {
		token := deliveryFixtureWriter
		if i == 1 {
			token = deliveryFixtureProvisioner
		}
		v := deliveryFixtureDecode[service.APIKeyDeliveryIdentityView](f.t, f.request(a, "POST", base+"/identities", f.policy().ReviewETag, map[string]any{"purpose": purpose, "integration_id": saved.IntegrationID, "token": token, "reason": "Configure independent machine identity"}), 200)
		identities[i] = v.ID
	}
	tpl := service.APIKeyCallerTemplateInput{AuthMount: "workload", Issuer: deliveryFixtureIssuer, JWKSURL: deliveryFixtureJWKS, Audience: "routex-app", AllowedSubjects: []string{deliveryFixtureSubject}, TokenTTLSeconds: 120, TokenMaxTTLSeconds: 300, Reason: "Review operator workload trust"}
	deliveryFixtureDecode[service.APIKeyCallerTemplateView](f.t, f.request(a, "PUT", base+"/caller-template", f.policy().ReviewETag, tpl), 200)
	policy := service.APIKeyDeliveryPolicyInput{AllowedModes: []string{"manual"}, DefaultMode: "manual", IntegrationID: saved.IntegrationID, WriterIdentityID: identities[0], ProvisionerIdentityID: identities[1], Reason: "Select reviewed dependencies before managed admission"}
	deliveryFixtureDecode[service.APIKeyDeliveryPolicyView](f.t, f.request(a, "PUT", base+"/policy", f.policy().ReviewETag, policy), 200)
	verified := deliveryFixtureDecode[service.APIKeyDeliveryPolicyView](f.t, f.request(a, "POST", base+"/verify", f.policy().ReviewETag, map[string]string{"reason": "Validate actual dedicated machine metadata"}), 200)
	if !verified.ManagedAvailable {
		f.t.Fatal("native dependency validation unavailable")
	}
	policy.AllowedModes = []string{"manual", "managed_vault"}
	policy.Reason = "Explicitly admit managed delivery"
	enabled := deliveryFixtureDecode[service.APIKeyDeliveryPolicyView](f.t, f.request(a, "PUT", base+"/policy", verified.ReviewETag, policy), 200)
	if !enabled.ManagedAvailable {
		f.t.Fatal("managed dependency eligibility lost")
	}
	f.peer.mu.Lock()
	defer f.peer.mu.Unlock()
	if !reflect.DeepEqual(f.peer.requests, []string{"GET /v1/auth/token/lookup-self", "POST /v1/sys/capabilities-self", "GET /v1/auth/token/lookup-self", "POST /v1/sys/capabilities-self"}) {
		f.t.Fatal("native machine validation did not use exactly four metadata requests")
	}
	if f.peer.probeWrites != 1 || f.peer.probeReads != 1 || f.peer.probeDestroys != 1 || f.peer.writes != 0 || f.peer.reads != 0 || f.peer.destroys != 0 {
		f.t.Fatal("infrastructure and delivery roles conflated")
	}
}
func (f *deliveryFixture) catalog() {
	f.t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-delivery-provider" {
			f.t.Error("wrong native upstream credential")
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatCompletionFixture("stop", `{"role":"assistant","content":"Delivered"}`, `{"prompt_tokens":3,"completion_tokens":5}`, false, 0))
	}))
	f.t.Cleanup(upstream.Close)
	ciphertext, e := f.store.Seal("crd_delivery_fixture", "fixture-delivery-provider")
	if e != nil {
		f.t.Fatal(e)
	}
	model := deliveryFixtureModel
	// Catalog-only seed. No managed Key, readiness, claim, acknowledgement, control
	// approval, isolation or publication receipt is fabricated by this fixture.
	for _, row := range []any{&entity.Provider{ID: "prv_delivery_fixture", Name: "Delivery provider"}, &entity.ProviderConnection{ID: "con_delivery_fixture", ProviderID: "prv_delivery_fixture", Name: "Native connection", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat}, &entity.ProviderCredential{ID: "crd_delivery_fixture", ConnectionID: "con_delivery_fixture", Name: "Native access", Ciphertext: ciphertext, Enabled: true, VerificationStatus: "verified"}, &entity.ProviderModel{ID: "pmd_delivery_fixture", ConnectionID: "con_delivery_fixture", UpstreamName: "delivery-upstream"}, &entity.CredentialModelAccess{CredentialID: "crd_delivery_fixture", ProviderModelID: "pmd_delivery_fixture"}, &entity.Model{ID: model, Status: "active"}, &entity.UserModelGrant{UserID: f.admin.Session.User.ID, ModelID: model}, &entity.ModelName{Name: deliveryFixtureModelName, ModelID: model, CurrentModelID: &model}, &entity.ModelProviderBinding{ID: "bnd_delivery_fixture", ModelID: model, ProviderModelID: "pmd_delivery_fixture", Weight: 100}} {
		if e := f.db.Create(row).Error; e != nil {
			f.t.Fatal(e)
		}
	}
	if e := f.svc.StartRuntime(f.t.Context()); e != nil {
		f.t.Fatal(e)
	}
	if e := f.svc.StartCallRecorder(f.t.Context(), filepath.Join(f.t.TempDir(), "delivery-calls.db")); e != nil {
		f.t.Fatal(e)
	}
	s := f.svc
	f.t.Cleanup(func() {
		if e := s.StopCallRecorder(); e != nil {
			f.t.Error(e)
		}
	})
}
func (f *deliveryFixture) native(bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(f.t.Context(), "POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"`+deliveryFixtureModelName+`","messages":[{"role":"user","content":"Delivery fixture"}]}`))
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")
	r := httptest.NewRecorder()
	f.router.ServeHTTP(r, req)
	return r
}
func (f *deliveryFixture) resolve(a deliveryFixtureActor, base, environment string) service.APIKeyDeliveryTargetView {
	f.t.Helper()
	ctx := f.context(a, base)
	in := service.APIKeyDeliveryResolveInput{RequestID: f.uuid(), ConsumerID: "", ApplicationName: "Application delivery fixture", Environment: environment, Principal: service.APIKeyDeliveryPrincipalInput{Method: "jwt", Subject: deliveryFixtureSubject}, Reason: "Resolve explicit application environment"}
	target := deliveryFixtureDecode[service.APIKeyDeliveryTargetView](f.t, f.request(a, "POST", base+"/delivery-targets/resolve", ctx.ReviewETag, in), 200)
	replay := deliveryFixtureDecode[service.APIKeyDeliveryTargetView](f.t, f.request(a, "POST", base+"/delivery-targets/resolve", ctx.ReviewETag, in), 200)
	if target.ID != replay.ID || target.ConsumerID == "" || target.ConsumerID != replay.ConsumerID || target.Status == "ready" {
		f.t.Fatal("empty Consumer allocation or identical intent receipt drift")
	}
	in.ApplicationName = "Changed intent"
	expectStatus(f.t, f.request(a, "POST", base+"/delivery-targets/resolve", ctx.ReviewETag, in), 409)
	return target
}
func (f *deliveryFixture) adminTarget(id string) service.APIKeyDeliveryTargetReviewView {
	return deliveryFixtureDecode[service.APIKeyDeliveryTargetReviewView](f.t, f.request(f.admin, "GET", "/api/v1/admin/api-key-delivery/targets/"+id, "", nil), 200)
}
func (f *deliveryFixture) ready(a deliveryFixtureActor, base string, target service.APIKeyDeliveryTargetView) service.APIKeyDeliveryTargetView {
	f.t.Helper()
	adminBase := "/api/v1/admin/api-key-delivery/targets/" + target.ID
	control := f.adminTarget(target.ID)
	deliveryFixtureDecode[service.APIKeyDeliveryTargetView](f.t, f.request(f.admin, "PUT", adminBase+"/approval", control.ReviewETag, service.APIKeyDeliveryApprovalInput{Approved: true, ControlVerified: true, Reason: "Operator confirms exact Principal control"}), 200)
	current := f.adminTarget(target.ID)
	// Control and isolation are distinct, and neither alone can reconcile READY.
	expectStatus(f.t, f.request(f.admin, "POST", adminBase+"/reconcile", current.ReviewETag, service.APIKeyDeliveryReconcileInput{RequestID: f.uuid(), Reason: "Control alone is insufficient"}), 503)
	deliveryFixtureDecode[service.APIKeyDeliveryTargetView](f.t, f.request(f.admin, "PUT", adminBase+"/isolation", current.ReviewETag, service.APIKeyDeliveryIsolationInput{Isolated: true, Reason: "Operator records resource-scoped isolation fact"}), 200)
	current = f.adminTarget(target.ID)
	result := deliveryFixtureDecode[service.APIKeyDeliveryTargetView](f.t, f.request(f.admin, "POST", adminBase+"/reconcile", current.ReviewETag, service.APIKeyDeliveryReconcileInput{RequestID: f.uuid(), Reason: "Reconcile exact desired caller grants"}), 200)
	if result.Status != "ready" || !result.ControlVerified || result.Isolation.Status != "operator_attested" {
		f.t.Fatal("genuine metadata reconciliation did not establish READY dependency proof")
	}
	return deliveryFixtureDecode[service.APIKeyDeliveryTargetView](f.t, f.request(a, "GET", base+"/delivery-targets/"+target.ID, "", nil), 200)
}
func (f *deliveryFixture) input(target service.APIKeyDeliveryTargetView) service.ManagedAPIKeyInput {
	return service.ManagedAPIKeyInput{Name: "Managed application Key", ModelIDs: []string{deliveryFixtureModel}, RequestID: f.uuid(), Reason: "Explicit managed issuance", Delivery: service.APIKeyDeliverySelection{ConsumerID: target.ConsumerID, Environment: target.Environment, Principal: service.APIKeyDeliveryPrincipalInput{Method: "jwt", Subject: deliveryFixtureSubject}, TargetReview: target.ReviewETag}}
}

// HTTP projects descriptor_revision as exact decimal text, independently of the
// private Service uint64. Do not round it through a float or decode Service DTOs.
type deliveryFixtureDescriptor struct {
	SchemaVersion      int    `json:"schema_version"`
	KeyID              string `json:"key_id"`
	ProfileID          string `json:"profile_id"`
	PrincipalID        string `json:"principal_id"`
	SlotID             string `json:"slot_id"`
	DescriptorRevision string `json:"descriptor_revision"`
	Endpoint           string `json:"endpoint"`
	Namespace          string `json:"namespace"`
	Mount              string `json:"mount"`
	Path               string `json:"path"`
	DataField          string `json:"data_field"`
	Version            uint64 `json:"version"`
	Method             string `json:"method"`
	AuthMount          string `json:"auth_mount"`
	Role               string `json:"role"`
	Coordinator        string `json:"coordinator"`
	ApplicationStatus  string `json:"application_status"`
}
type deliveryFixtureReceipt struct {
	RequestID      string                     `json:"request_id"`
	KeyID          string                     `json:"key_id"`
	Mode           string                     `json:"mode"`
	DeliveryStatus string                     `json:"delivery_status"`
	KeyStatus      string                     `json:"key_status"`
	RuntimeStatus  string                     `json:"runtime_status"`
	FailureCode    *string                    `json:"failure_code"`
	Descriptor     *deliveryFixtureDescriptor `json:"descriptor"`
	ReviewETag     string                     `json:"review_etag"`
}
type deliveryFixtureManaged struct {
	Key struct {
		ID, Status    string
		DeliveryMode  string  `json:"delivery_mode"`
		ReplacesKeyID *string `json:"replaces_key_id"`
	} `json:"key"`
	Delivery deliveryFixtureReceipt `json:"delivery"`
}

func (f *deliveryFixture) managed(a deliveryFixtureActor, base, etag string, in service.ManagedAPIKeyInput, status int) deliveryFixtureManaged {
	f.t.Helper()
	res := f.request(a, "POST", base+"/managed", etag, in)
	v := deliveryFixtureDecode[deliveryFixtureManaged](f.t, res, status)
	var shape map[string]json.RawMessage
	if json.Unmarshal(res.Body.Bytes(), &shape) != nil || len(shape) != 2 || shape["key"] == nil || shape["delivery"] == nil {
		f.t.Fatal("managed envelope exposed extra member")
	}
	if v.Key.ID == "" || v.Key.ID != v.Delivery.KeyID || v.Key.DeliveryMode != "managed_vault" || res.Header().Get("ETag") != `"`+v.Delivery.ReviewETag+`"` {
		f.t.Fatal("managed metadata/review mismatch")
	}
	return v
}
func (f *deliveryFixture) command(request string) entity.APIKeyDeliveryCommand {
	f.t.Helper()
	var c entity.APIKeyDeliveryCommand
	if e := f.db.Where("request_id = ? AND purpose <> ?", request, "resolve").Take(&c).Error; e != nil {
		f.t.Fatal(e)
	}
	return c
}
func (f *deliveryFixture) receipt(a deliveryFixtureActor, base, request string) deliveryFixtureReceipt {
	return deliveryFixtureDecode[deliveryFixtureReceipt](f.t, f.request(a, "GET", base+"/delivery-receipts/"+request, "", nil), 200)
}
func (f *deliveryFixture) assertNoDeliveryRead() {
	f.t.Helper()
	_, reads, destroys := f.peer.counts()
	if reads != 0 || destroys != 0 {
		f.t.Fatal("restricted delivery borrowed Reader or cleanup capability")
	}
}

func (f *deliveryFixture) assertTerminal(request, status string, ack bool) {
	f.t.Helper()
	c := f.command(request)
	if c.DeliveryStatus != status || c.RuntimeStatus == "confirmed" || c.ClaimedAt == nil || c.ClaimProcessBirth == nil || c.ClaimProcessID == "" || c.TerminalAt == nil || c.RevokedAt == nil || c.PublishedAt != nil || (c.AcknowledgedAt != nil) != ack {
		f.t.Fatal("durable terminal command shape lost")
	}
	var binding entity.APIKeyVaultDeliveryBinding
	if e := f.db.Take(&binding, "id = ?", c.BindingID).Error; e != nil {
		f.t.Fatal(e)
	}
	if (binding.AcknowledgedAt != nil) != ack || (ack && binding.Version != 1) || (!ack && binding.Version != 0) {
		f.t.Fatal("historical acknowledgement or unknown binding proof changed")
	}
	if c.OwnerKind == "personal" {
		var key entity.APIKey
		if e := f.db.Take(&key, "id = ?", c.KeyID).Error; e != nil || key.Status != entity.KeyRevoked || key.DeliveryMode != "managed_vault" {
			f.t.Fatal("terminal personal verifier remained usable")
		}
	} else {
		var key entity.ProjectKey
		if e := f.db.Take(&key, "id = ?", c.KeyID).Error; e != nil || key.Status != entity.KeyRevoked || key.DeliveryMode != "managed_vault" {
			f.t.Fatal("terminal Project verifier remained usable")
		}
	}
	if _, e := f.svc.AuthenticateAPIKey(f.t.Context(), f.peer.bearer(f.t, c.KeyID)); e == nil {
		f.t.Fatal("terminal managed verifier authenticated")
	}
}
func (f *deliveryFixture) assertImmutableCommand(before entity.APIKeyDeliveryCommand) {
	f.t.Helper()
	after := f.command(before.RequestID)
	if fields := samlMigrationDifferentFields(before, after); len(fields) != 0 {
		f.t.Fatal("same-intent receipt mutated durable command fields", fields)
	}
}
func (f *deliveryFixture) member(name string, permissions []string) deliveryFixtureActor {
	f.t.Helper()
	_, cookie, csrf := createSystemStatusMember(f.t, f.svc, f.router, f.admin.Session.User.ID, name, permissions)
	r := identityRequest(f.router, "GET", "/api/v1/auth/session", "", cookie, "")
	expectStatus(f.t, r, 200)
	var session SessionResponse
	if json.Unmarshal(r.Body.Bytes(), &session) != nil || session.CSRFToken != csrf {
		f.t.Fatal("member Session readback")
	}
	return deliveryFixtureActor{session, cookie}
}
func (f *deliveryFixture) project(a, b deliveryFixtureActor) string {
	f.t.Helper()
	p := deliveryFixtureDecode[ProjectResponse](f.t, f.request(f.admin, "POST", "/api/v1/projects", "", map[string]any{"name": "Managed application Project"}), 201)
	path := "/api/v1/projects/" + p.ID
	expectStatus(f.t, f.request(f.admin, "PUT", path+"/managers", "", map[string]any{"user_ids": []string{a.Session.User.ID, b.Session.User.ID}}), 200)
	expectStatus(f.t, f.request(f.admin, "PUT", path+"/models", "", map[string]any{"model_ids": []string{deliveryFixtureModel}}), 200)
	return path + "/keys"
}
func deliveryFixtureAwait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-t.Context().Done():
		t.Fatal("owned controlled operation exceeded original test context")
		var zero T
		return zero
	case <-time.After(10 * time.Second):
		t.Fatal("owned controlled operation did not join within SDK bound")
		var zero T
		return zero
	}
}
func (f *deliveryFixture) held(t *testing.T, a deliveryFixtureActor, base string, target service.APIKeyDeliveryTargetView) (service.ManagedAPIKeyInput, string, *deliveryFixtureHold, <-chan *httptest.ResponseRecorder) {
	t.Helper()
	in := f.input(target)
	etag := f.context(a, base).ReviewETag
	h := f.peer.holdNext(t)
	done := make(chan *httptest.ResponseRecorder, 1)
	// Cancellation changes no original request deadline. Cleanup belongs to this
	// subcase and is installed before either the request or its first await.
	ctx, cancel := context.WithCancel(f.t.Context())
	requestJoined := make(chan struct{})
	t.Cleanup(func() {
		h.release()
		cancel()
		// One cleanup budget joins both original operations; the result channel is
		// kept intact for the positive-path assertions below each held call.
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		requestWait := requestJoined
		peerWait := h.Joined
		for requestWait != nil || peerWait != nil {
			select {
			case <-requestWait:
				requestWait = nil
			case <-peerWait:
				peerWait = nil
			case <-timer.C:
				t.Errorf("owned held operation closure unproven: application_joined=%t peer_joined=%t", requestWait == nil, peerWait == nil)
				return
			}
		}
	})
	go func() {
		defer close(requestJoined)
		done <- f.requestContext(ctx, a, "POST", base+"/managed", etag, in)
	}()
	key := deliveryFixtureAwait(t, h.Entered)
	var pending entity.APIKey
	c := f.command(in.RequestID)
	if c.KeyID != key || c.DeliveryStatus != "pending" || c.ClaimedAt == nil || c.AcknowledgedAt != nil {
		t.Fatal("held remote effect not backed by original durable claim")
	}
	if c.OwnerKind == "personal" {
		if e := f.db.Take(&pending, "id = ?", key).Error; e != nil || pending.Status != entity.KeyPending {
			t.Fatal("held Key activated early")
		}
	}
	if _, e := f.svc.AuthenticateAPIKey(t.Context(), f.peer.bearer(t, key)); e == nil {
		t.Fatal("pending delivered verifier authenticated before acknowledgement")
	}
	return in, etag, h, done
}

func testAPIKeyDeliveryLifecycle(t *testing.T, db *gorm.DB) {
	f := newDeliveryFixture(t, db)
	base := "/api/v1/keys"
	t.Run("independent_management_authority", func(t *testing.T) {
		member := f.member("delivery-operator", []string{"secrets.read", "secrets.write", "secrets.test"})
		before, _, _ := f.peer.counts()
		expectStatus(t, f.request(member, "GET", "/api/v1/admin/api-key-delivery/policy", "", nil), 403)
		expectStatus(t, f.request(member, "GET", "/api/v1/admin/api-key-delivery/caller-template", "", nil), 403)
		if e := db.Where("role_id = ? AND permission = ?", "rol_admin", "secrets.read").Delete(&entity.RolePermission{}).Error; e != nil {
			t.Fatal(e)
		}
		expectStatus(t, f.request(f.admin, "GET", "/api/v1/admin/api-key-delivery/policy", "", nil), 403)
		if e := db.Create(&entity.RolePermission{RoleID: "rol_admin", Permission: "secrets.read"}).Error; e != nil {
			t.Fatal(e)
		}
		after, _, _ := f.peer.counts()
		if before != after {
			t.Fatal("denied management performed remote delivery")
		}
	})
	f.configure()
	f.catalog()
	var target service.APIKeyDeliveryTargetView
	var first deliveryFixtureManaged
	t.Run("isolation_without_principal_control_cannot_be_ready", func(t *testing.T) {
		target := f.resolve(f.admin, base, "isolation-only")
		review := f.adminTarget(target.ID)
		adminBase := "/api/v1/admin/api-key-delivery/targets/" + target.ID
		isolated := deliveryFixtureDecode[service.APIKeyDeliveryTargetView](t, f.request(f.admin, "PUT", adminBase+"/isolation", review.ReviewETag, service.APIKeyDeliveryIsolationInput{Isolated: true, Reason: "Separate isolation from Principal control"}), 200)
		expectStatus(t, f.request(f.admin, "POST", adminBase+"/reconcile", isolated.ReviewETag, service.APIKeyDeliveryReconcileInput{RequestID: f.uuid(), Reason: "Isolation is not Principal control"}), 409)
		if isolated.ControlVerified || isolated.Status == "ready" {
			t.Fatal("isolation attestation fabricated Principal control")
		}
	})
	if !t.Run("explicit_target_and_acknowledged_native_use", func(t *testing.T) {
		target = f.resolve(f.admin, base, "production")
		in := f.input(target)
		expectStatus(t, f.request(f.admin, "POST", base+"/managed", f.context(f.admin, base).ReviewETag, in), 503)
		target = f.ready(f.admin, base, target)
		in = f.input(target)
		etag := f.context(f.admin, base).ReviewETag
		before, _, _ := f.peer.counts()
		assertBirth := deliveryFixtureManagedBirth(t, db, "personal", f.admin.Session.User.ID, f.admin.Session.User.ID)
		first = f.managed(f.admin, base, etag, in, 201)
		assertBirth(first.Key.ID)
		if first.Key.Status != entity.KeyActive || first.Delivery.DeliveryStatus != "acknowledged" || first.Delivery.RuntimeStatus != "confirmed" || first.Delivery.Descriptor == nil {
			t.Fatal("delivery receipt did not separate acknowledged/runtime proof")
		}
		descriptor := deliveryFixtureDecode[deliveryFixtureDescriptor](t, f.request(f.admin, "GET", base+"/"+first.Key.ID+"/delivery-descriptor", "", nil), 200)
		if !reflect.DeepEqual(descriptor, *first.Delivery.Descriptor) || descriptor.SchemaVersion != 1 || descriptor.DescriptorRevision != "1" || descriptor.Version != 1 || descriptor.Coordinator != "not_configured" || descriptor.ApplicationStatus != "unknown" || descriptor.Method != "jwt" || descriptor.AuthMount != "workload" || descriptor.Path != "applications/"+target.ID+"/routex-api-key-"+first.Key.ID {
			t.Fatal("safe versioned descriptor drift")
		}
		var key entity.APIKey
		if e := db.Take(&key, "id = ?", first.Key.ID).Error; e != nil || key.TokenHash != secret.SHA256Hex(f.peer.bearer(t, key.ID)) {
			t.Fatal("managed Key not hash-only")
		}
		c := f.command(in.RequestID)
		if c.ClaimedAt == nil || c.AcknowledgedAt == nil || c.PublishedAt == nil || c.RevokedAt != nil {
			t.Fatal("claim/ack/publication ordering missing")
		}
		expectStatus(t, f.request(f.admin, "POST", base+"/"+key.ID+"/confirm", "", nil), 409)
		replay := f.managed(f.admin, base, etag, in, 200)
		if replay.Key.ID != first.Key.ID {
			t.Fatal("identical UUID allocated another verifier")
		}
		in.Name = "Changed original request"
		expectStatus(t, f.request(f.admin, "POST", base+"/managed", etag, in), 409)
		after, _, _ := f.peer.counts()
		if after != before+1 {
			t.Fatal("original managed intent replayed Writer")
		}
		f.assertImmutableCommand(c)
		expectStatus(t, f.native(f.peer.bearer(t, key.ID)), 200)
		if e := f.svc.FlushCallRecorder(t.Context()); e != nil {
			t.Fatal(e)
		}
		var call entity.CallRecord
		if e := db.Where("key_id = ?", key.ID).Take(&call).Error; e != nil || call.Status != "success" {
			t.Fatal("real managed call fact missing")
		}
		var attempts []entity.CallAttempt
		if e := db.Where("request_id = ?", call.RequestID).Find(&attempts).Error; e != nil || len(attempts) != 1 || attempts[0].NativeCompletionEvidence != "completed" {
			t.Fatal("real native completed use missing")
		}
		f.assertNoDeliveryRead()
	}) {
		return
	}
	t.Run("managed_rotation_keeps_native_retirement_gate", func(t *testing.T) {
		current := deliveryFixtureDecode[service.APIKeyDeliveryTargetView](t, f.request(f.admin, "GET", base+"/delivery-targets/"+target.ID, "", nil), 200)
		in := f.input(current)
		in.Name = ""
		in.ModelIDs = []string{}
		res := f.request(f.admin, "POST", base+"/"+first.Key.ID+"/managed-rotation", f.context(f.admin, base).ReviewETag, in)
		replacement := deliveryFixtureDecode[deliveryFixtureManaged](t, res, 201)
		if replacement.Key.ReplacesKeyID == nil || *replacement.Key.ReplacesKeyID != first.Key.ID || replacement.Delivery.Descriptor == nil || replacement.Delivery.Descriptor.DescriptorRevision != "2" || replacement.Delivery.Descriptor.SlotID != first.Delivery.Descriptor.SlotID || replacement.Delivery.Descriptor.Path == first.Delivery.Descriptor.Path {
			t.Fatal("rotation did not append a distinct CAS0 descriptor revision")
		}
		complete := map[string]string{"replacement_key_id": replacement.Key.ID}
		expectStatus(t, f.request(f.admin, "POST", base+"/"+first.Key.ID+"/complete-rotation", "", complete), 409)
		if _, e := f.svc.AuthenticateAPIKey(t.Context(), f.peer.bearer(t, first.Key.ID)); e != nil {
			t.Fatal("acknowledgement prematurely retired predecessor")
		}
		expectStatus(t, f.native(f.peer.bearer(t, replacement.Key.ID)), 200)
		if e := f.svc.FlushCallRecorder(t.Context()); e != nil {
			t.Fatal(e)
		}
		expectStatus(t, f.request(f.admin, "POST", base+"/"+first.Key.ID+"/complete-rotation", "", complete), 204)
		if _, e := f.svc.AuthenticateAPIKey(t.Context(), f.peer.bearer(t, first.Key.ID)); e == nil {
			t.Fatal("native completed retirement left predecessor usable")
		}
	})
	t.Run("project_owner_scope_and_urgent_revocation", func(t *testing.T) {
		manager := f.member("delivery-manager", nil)
		successor := f.member("delivery-successor", nil)
		outsider := f.member("delivery-outsider", nil)
		projectBase := f.project(manager, successor)
		owned := f.ready(manager, projectBase, f.resolve(manager, projectBase, "project-production"))
		before, _, _ := f.peer.counts()
		expectStatus(t, f.request(outsider, "GET", projectBase+"/delivery-targets/"+owned.ID, "", nil), 404)
		assertBirth := deliveryFixtureManagedBirth(t, db, "project", strings.TrimSuffix(strings.TrimPrefix(projectBase, "/api/v1/projects/"), "/keys"), manager.Session.User.ID)
		managed := f.managed(manager, projectBase, f.context(manager, projectBase).ReviewETag, f.input(owned), 201)
		assertBirth(managed.Key.ID)
		descriptor := deliveryFixtureDecode[deliveryFixtureDescriptor](t, f.request(successor, "GET", projectBase+"/"+managed.Key.ID+"/delivery-descriptor", "", nil), 200)
		expectStatus(t, f.request(outsider, "GET", projectBase+"/"+managed.Key.ID+"/delivery-descriptor", "", nil), 404)
		expectStatus(t, f.request(manager, "POST", projectBase+"/"+managed.Key.ID+"/confirm", "", nil), 409)
		auth, e := f.svc.AuthenticateAPIKey(t.Context(), f.peer.bearer(t, managed.Key.ID))
		if e != nil || auth.ProjectID == "" || auth.Key.UserID != "" || descriptor.ProfileID != owned.ID {
			t.Fatal("Project delivery borrowed manager personal authority")
		}
		expectStatus(t, f.request(successor, "DELETE", projectBase+"/"+managed.Key.ID, "", nil), 204)
		if _, e = f.svc.AuthenticateAPIKey(t.Context(), f.peer.bearer(t, managed.Key.ID)); e == nil {
			t.Fatal("urgent Project revocation not applied")
		}
		c := f.command(managed.Delivery.RequestID)
		if c.RevokedAt == nil || c.TerminalAt == nil || c.AcknowledgedAt == nil || c.PublishedAt == nil || c.RuntimeStatus == "confirmed" {
			t.Fatal("revocation discarded historical receipt or kept current publication")
		}
		after, _, _ := f.peer.counts()
		if after != before+1 {
			t.Fatal("Project ownership/revocation performed extra Writer")
		}
		f.assertNoDeliveryRead()
	})
	t.Run("disabled_rotation_preserves_disabled_unknown_until_enable", func(t *testing.T) {
		current := deliveryFixtureDecode[service.APIKeyDeliveryTargetView](t, f.request(f.admin, "GET", base+"/delivery-targets/"+target.ID, "", nil), 200)
		original := f.managed(f.admin, base, f.context(f.admin, base).ReviewETag, f.input(current), 201)
		expectStatus(t, f.request(f.admin, "PATCH", base+"/"+original.Key.ID, "", map[string]any{"enabled": false}), 200)
		in := f.input(current)
		in.Name = ""
		in.ModelIDs = []string{}
		replacement := deliveryFixtureDecode[deliveryFixtureManaged](t, f.request(f.admin, "POST", base+"/"+original.Key.ID+"/managed-rotation", f.context(f.admin, base).ReviewETag, in), 202)
		if replacement.Key.Status != entity.KeyDisabled || replacement.Delivery.DeliveryStatus != "acknowledged" || replacement.Delivery.RuntimeStatus != "unknown" || replacement.Delivery.Descriptor != nil {
			t.Fatal("disabled predecessor silently enabled or acquired active runtime proof")
		}
		if _, e := f.svc.AuthenticateAPIKey(t.Context(), f.peer.bearer(t, replacement.Key.ID)); e == nil {
			t.Fatal("disabled replacement authenticated")
		}
		expectStatus(t, f.request(f.admin, "POST", base+"/"+replacement.Key.ID+"/confirm", "", nil), 409)
		expectStatus(t, f.request(f.admin, "PATCH", base+"/"+replacement.Key.ID, "", map[string]any{"enabled": true}), 200)
		receipt := f.receipt(f.admin, base, in.RequestID)
		recovered := deliveryFixtureDecode[deliveryFixtureManaged](t, f.request(f.admin, "POST", base+"/"+replacement.Key.ID+"/delivery-reconcile", receipt.ReviewETag, service.APIKeyDeliveryReconcileInput{RequestID: in.RequestID, Reason: "Explicit enable then reconcile active runtime"}), 200)
		if recovered.Key.Status != entity.KeyActive || recovered.Delivery.RuntimeStatus != "confirmed" || recovered.Delivery.Descriptor == nil {
			t.Fatal("explicit enabled replacement did not obtain fresh proof")
		}
	})
	t.Run("manual_policy_and_mode_are_independent", func(t *testing.T) {
		p := f.policy()
		body := service.APIKeyDeliveryPolicyInput{AllowedModes: []string{"managed_vault"}, DefaultMode: "managed_vault", IntegrationID: p.IntegrationID, WriterIdentityID: p.WriterIdentityID, ProvisionerIdentityID: p.ProvisionerIdentityID, Reason: "Restrict new manual issuance"}
		updated := deliveryFixtureDecode[service.APIKeyDeliveryPolicyView](t, f.request(f.admin, "PUT", "/api/v1/admin/api-key-delivery/policy", p.ReviewETag, body), 200)
		expectStatus(t, f.request(f.admin, "POST", base, "", map[string]any{"name": "Denied manual", "model_ids": []string{deliveryFixtureModel}}), 409)
		body.AllowedModes = []string{"manual", "managed_vault"}
		body.DefaultMode = "manual"
		body.Reason = "Restore manual choice"
		deliveryFixtureDecode[service.APIKeyDeliveryPolicyView](t, f.request(f.admin, "PUT", "/api/v1/admin/api-key-delivery/policy", updated.ReviewETag, body), 200)
		manual := deliveryFixtureDecode[CreatedKeyResponse](t, f.request(f.admin, "POST", base, "", map[string]any{"name": "Manual remains transient", "model_ids": []string{deliveryFixtureModel}}), 201)
		if manual.Secret == "" {
			t.Fatal("manual one-time delivery changed")
		}
		expectStatus(t, f.request(f.admin, "POST", base+"/"+manual.Key.ID+"/confirm", "", nil), 200)
		expectStatus(t, f.request(f.admin, "PATCH", base+"/"+manual.Key.ID, "", map[string]any{"delivery_mode": "managed_vault"}), 400)
		var stored entity.APIKey
		if e := db.Take(&stored, "id = ?", manual.Key.ID).Error; e != nil || stored.DeliveryMode != "manual" || stored.TokenHash != secret.SHA256Hex(manual.Secret) {
			t.Fatal("immutable manual mode changed")
		}
		f.assertNoDeliveryRead()
	})
}

func testAPIKeyDeliveryRecovery(t *testing.T, db *gorm.DB) {
	f := newDeliveryFixture(t, db)
	f.configure()
	f.catalog()
	base := "/api/v1/keys"
	makeReady := func(environment string) service.APIKeyDeliveryTargetView {
		return f.ready(f.admin, base, f.resolve(f.admin, base, environment))
	}
	t.Run("lost_body_unknown_same_intent_no_replay", func(t *testing.T) {
		target := makeReady("body-loss")
		in := f.input(target)
		etag := f.context(f.admin, base).ReviewETag
		f.peer.mu.Lock()
		f.peer.lostBody = true
		f.peer.mu.Unlock()
		before, _, _ := f.peer.counts()
		result := f.managed(f.admin, base, etag, in, 202)
		if result.Delivery.DeliveryStatus != "unknown" || result.Delivery.KeyStatus != entity.KeyRevoked || result.Delivery.Descriptor != nil {
			t.Fatal("lost write response inferred acknowledged delivery")
		}
		f.assertTerminal(in.RequestID, "unknown", false)
		snapshot := f.command(in.RequestID)
		f.managed(f.admin, base, etag, in, 200)
		f.assertImmutableCommand(snapshot)
		changed := in
		changed.Reason = "Different same-UUID intent"
		expectStatus(t, f.request(f.admin, "POST", base+"/managed", etag, changed), 409)
		receipt := f.receipt(f.admin, base, in.RequestID)
		expectStatus(t, f.request(f.admin, "POST", base+"/"+result.Key.ID+"/delivery-reconcile", receipt.ReviewETag, service.APIKeyDeliveryReconcileInput{RequestID: in.RequestID, Reason: "Unknown cannot publish"}), 409)
		after, _, _ := f.peer.counts()
		if after != before+1 {
			t.Fatal("unknown intent reissued CAS0")
		}
		f.assertNoDeliveryRead()
	})
	t.Run("held_ack_loses_exact_issuing_session", func(t *testing.T) {
		actor := f.login()
		target := f.ready(actor, base, f.resolve(actor, base, "session-loss"))
		in, _, h, done := f.held(t, actor, base, target)
		expectStatus(t, f.request(actor, "POST", "/api/v1/auth/logout", "", nil), 204)
		h.release()
		res := deliveryFixtureAwait(t, done)
		deliveryFixtureAwait(t, h.Joined)
		expectStatus(t, res, 401)
		f.assertTerminal(in.RequestID, "acknowledged", true)
		fresh := f.login()
		receipt := f.receipt(fresh, base, in.RequestID)
		if receipt.FailureCode == nil || *receipt.FailureCode != "issuing_authority_changed" || receipt.Descriptor != nil {
			t.Fatal("late known ack became activation authority")
		}
		before, _, _ := f.peer.counts()
		expectStatus(t, f.request(fresh, "POST", base+"/"+receipt.KeyID+"/delivery-reconcile", receipt.ReviewETag, service.APIKeyDeliveryReconcileInput{RequestID: in.RequestID, Reason: "Fresh Session cannot resurrect original"}), 409)
		after, _, _ := f.peer.counts()
		if after != before {
			t.Fatal("new Session replayed terminal Writer")
		}
	})
	t.Run("held_ack_loses_isolation_revision", func(t *testing.T) {
		target := makeReady("isolation-loss")
		in, _, h, done := f.held(t, f.admin, base, target)
		review := f.adminTarget(target.ID)
		deliveryFixtureDecode[service.APIKeyDeliveryTargetView](t, f.request(f.admin, "PUT", "/api/v1/admin/api-key-delivery/targets/"+target.ID+"/isolation", review.ReviewETag, service.APIKeyDeliveryIsolationInput{Isolated: false, Reason: "Revoke exact resource isolation"}), 200)
		h.release()
		res := deliveryFixtureAwait(t, done)
		deliveryFixtureAwait(t, h.Joined)
		expectStatus(t, res, 202)
		f.assertTerminal(in.RequestID, "acknowledged", true)
		receipt := f.receipt(f.admin, base, in.RequestID)
		if receipt.Descriptor != nil {
			t.Fatal("revoked isolation published descriptor")
		}
	})
	t.Run("held_project_ack_loses_current_manager", func(t *testing.T) {
		manager := f.member("held-manager", nil)
		successor := f.member("held-successor", nil)
		projectBase := f.project(manager, successor)
		target := f.ready(manager, projectBase, f.resolve(manager, projectBase, "manager-loss"))
		in, _, h, done := f.held(t, manager, projectBase, target)
		projectPath := strings.TrimSuffix(projectBase, "/keys")
		expectStatus(t, f.request(successor, "PUT", projectPath+"/managers", "", map[string]any{"user_ids": []string{successor.Session.User.ID}}), 200)
		h.release()
		res := deliveryFixtureAwait(t, done)
		deliveryFixtureAwait(t, h.Joined)
		expectStatus(t, res, 404)
		f.assertTerminal(in.RequestID, "acknowledged", true)
		receipt := f.receipt(successor, projectBase, in.RequestID)
		expectStatus(t, f.request(successor, "POST", projectBase+"/"+receipt.KeyID+"/delivery-reconcile", receipt.ReviewETag, service.APIKeyDeliveryReconcileInput{RequestID: in.RequestID, Reason: "Successor cannot resurrect late original proof"}), 409)
	})
	t.Run("known_ack_publication_outage_reconciles_without_write", func(t *testing.T) {
		target := makeReady("publication-outage")
		in := f.input(target)
		etag := f.context(f.admin, base).ReviewETag
		var armed, observed atomic.Bool
		name := "test:delivery-runtime-publication-once"
		registered := true
		if e := db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "providers" && armed.CompareAndSwap(true, false) {
				observed.Store(true)
				_ = tx.AddError(errors.New("controlled runtime publication read outage"))
			}
		}); e != nil {
			t.Fatal(e)
		}
		defer func() {
			if registered {
				if e := db.Callback().Query().Remove(name); e != nil {
					t.Error(e)
				}
			}
		}()
		f.peer.mu.Lock()
		f.peer.acknowledge = func() { armed.Store(true) }
		f.peer.mu.Unlock()
		before, _, _ := f.peer.counts()
		result := f.managed(f.admin, base, etag, in, 202)
		if !observed.Load() || armed.Load() || result.Delivery.DeliveryStatus != "acknowledged" || result.Delivery.RuntimeStatus != "unknown" || result.Delivery.Descriptor != nil {
			t.Fatal("controlled publication error not distinguished from write uncertainty")
		}
		c := f.command(in.RequestID)
		if c.AcknowledgedAt == nil || c.RevokedAt != nil || c.PublishedAt != nil {
			t.Fatal("eligible acknowledgement was terminally revoked by publication outage")
		}
		if e := db.Callback().Query().Remove(name); e != nil {
			t.Fatal(e)
		}
		registered = false
		fresh := f.login()
		receipt := f.receipt(fresh, base, in.RequestID)
		recovered := deliveryFixtureDecode[deliveryFixtureManaged](t, f.request(fresh, "POST", base+"/"+result.Key.ID+"/delivery-reconcile", receipt.ReviewETag, service.APIKeyDeliveryReconcileInput{RequestID: in.RequestID, Reason: "Publish original durable acknowledgement"}), 200)
		if recovered.Delivery.RuntimeStatus != "confirmed" || recovered.Delivery.Descriptor == nil {
			t.Fatal("fresh authorized local reconciliation lost original ack")
		}
		after, _, _ := f.peer.counts()
		if after != before+1 {
			t.Fatal("publication recovery reissued Writer")
		}
		expectStatus(t, f.native(f.peer.bearer(t, result.Key.ID)), 200)
		f.assertNoDeliveryRead()
	})
	t.Run("fresh_service_generation_retains_claimed_unknown_tombstone", func(t *testing.T) {
		target := makeReady("new-generation")
		in := f.input(target)
		etag := f.context(f.admin, base).ReviewETag
		var failed atomic.Bool
		name := "test:delivery-ack-persistence-once"
		registered := true
		if e := db.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
			binding, ok := tx.Statement.Dest.(*entity.APIKeyVaultDeliveryBinding)
			if ok && binding.Version == 1 && binding.AcknowledgedAt != nil && failed.CompareAndSwap(false, true) {
				_ = tx.AddError(errors.New("controlled acknowledgement commit outage"))
			}
		}); e != nil {
			t.Fatal(e)
		}
		defer func() {
			if registered {
				if e := db.Callback().Update().Remove(name); e != nil {
					t.Error(e)
				}
			}
		}()
		before, _, _ := f.peer.counts()
		expectStatus(t, f.request(f.admin, "POST", base+"/managed", etag, in), 503)
		c := f.command(in.RequestID)
		if !failed.Load() || c.DeliveryStatus != "pending" || c.ClaimedAt == nil || c.AcknowledgedAt != nil {
			t.Fatal("missing genuine pre-Do claim after unavailable ack commit")
		}
		if e := db.Callback().Update().Remove(name); e != nil {
			t.Fatal(e)
		}
		registered = false
		original := f.svc
		if e := f.stop(original); e != nil {
			t.Fatal("original owned generation did not close", e)
		}
		fresh := f.newService()
		f.start(fresh, "API-Key delivery reconstructed generation")
		if e := fresh.StartRuntime(f.t.Context()); e != nil {
			t.Fatal("reconstructed owned runtime did not start", e)
		}
		f.install(fresh)
		// This is a fresh Service/SystemInstance in the same test process, explicitly
		// not executable persistence acceptance. The original Session still authenticates.
		expectStatus(t, f.request(f.admin, "GET", "/api/v1/auth/session", "", nil), 200)
		replay := f.managed(f.admin, base, etag, in, 200)
		if replay.Delivery.DeliveryStatus != "unknown" || replay.Delivery.KeyStatus != entity.KeyRevoked {
			t.Fatal("new generation dispatched claimed intent again")
		}
		f.assertTerminal(in.RequestID, "unknown", false)
		after, _, _ := f.peer.counts()
		if after != before+1 {
			t.Fatal("fresh Service recovered a transient Writer handle")
		}
		freshTarget := deliveryFixtureDecode[service.APIKeyDeliveryTargetView](t, f.request(f.admin, "GET", base+"/delivery-targets/"+target.ID, "", nil), 200)
		newIntent := f.input(freshTarget)
		next := f.managed(f.admin, base, f.context(f.admin, base).ReviewETag, newIntent, 201)
		if next.Key.ID == c.KeyID || next.Delivery.Descriptor == nil || next.Delivery.Descriptor.Path == "applications/"+target.ID+"/routex-api-key-"+c.KeyID {
			t.Fatal("explicit new intent reused uncertain object")
		}
		f.assertNoDeliveryRead()
	})
	t.Run("headerless_loss_never_fabricates_process_closure", func(t *testing.T) {
		target := makeReady("headerless-loss")
		in := f.input(target)
		etag := f.context(f.admin, base).ReviewETag
		before, _, _ := f.peer.counts()
		f.peer.mu.Lock()
		f.peer.lostHeader = true
		f.peer.mu.Unlock()
		result := f.managed(f.admin, base, etag, in, 202)
		f.assertTerminal(in.RequestID, "unknown", false)
		// The HTTP peer and original SDK call have actually returned. This does not
		// upgrade a missing SDK response-body closure witness into a process receipt.
		f.peer.active.Wait()
		snapshot := f.command(in.RequestID)
		receipt := f.receipt(f.admin, base, in.RequestID)
		if receipt.Descriptor != nil {
			t.Fatal("pre-header loss exposed descriptor")
		}
		// A stored terminal receipt is safe local replay, not new SDK admission.
		retained := f.managed(f.admin, base, etag, in, 200)
		if retained.Key.ID != result.Key.ID || retained.Key.Status != entity.KeyRevoked || retained.Delivery.DeliveryStatus != "unknown" || retained.Delivery.RuntimeStatus != "unknown" || retained.Delivery.Descriptor != nil {
			t.Fatal("same-intent receipt fabricated activation or delivery proof")
		}
		changed := in
		changed.Reason = "Changed original uncertain body"
		expectStatus(t, f.request(f.admin, "POST", base+"/managed", etag, changed), 409)
		f.assertImmutableCommand(snapshot)
		original := f.svc
		var app *apperrors.Error
		if e := f.stop(original); !errors.As(e, &app) || app.Code != 503 {
			t.Fatal("headerless response loss did not retain unavailable shutdown proof")
		}
		// Shutdown closes admission even when its join proof remains unavailable.
		newIntent := in
		newIntent.RequestID = f.uuid()
		expectStatus(t, f.request(f.admin, "POST", base+"/managed", etag, newIntent), 503)
		var process entity.CredentialSourceProcess
		if e := db.Take(&process, "process_id = ?", snapshot.ClaimProcessID).Error; e != nil || process.ClosedAt != nil {
			t.Fatal("unproven source process was marked closed")
		}
		var instance entity.SystemInstance
		if e := db.Take(&instance, "id = ?", process.ProcessID).Error; e != nil || instance.StoppedAt != nil {
			t.Fatal("unproven system instance was marked stopped")
		}
		// Mark cleanup as intentionally observed-unproven, not successful. Peer Close
		// above owns actual sockets; no cleanup receipt or external process is invented.
		f.unproven[original] = true
		fresh := f.newService()
		f.start(fresh, "API-Key delivery retained-unknown observer")
		f.install(fresh)
		replay := f.managed(f.admin, base, etag, in, 200)
		if replay.Key.ID != result.Key.ID || replay.Delivery.DeliveryStatus != "unknown" || replay.Delivery.Descriptor != nil {
			t.Fatal("token-free retained receipt became executable cleanup")
		}
		changed = in
		changed.Reason = "Different UUID body after reconstruction"
		expectStatus(t, f.request(f.admin, "POST", base+"/managed", etag, changed), 409)
		f.assertImmutableCommand(snapshot)
		after, _, _ := f.peer.counts()
		if after != before+1 {
			t.Fatal("headerless retained intent replayed remote Write")
		}
		f.assertNoDeliveryRead()
	})
}
