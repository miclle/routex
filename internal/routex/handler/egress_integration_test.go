package handler

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

// A real numeric-address SOCKS5 forwarder, restricted to the test server target.
func egressSOCKSFixture(t *testing.T, target string) (string, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	var workers sync.WaitGroup
	var connections sync.Map
	workers.Go(func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Store(conn, true)
			workers.Go(func() {
				defer connections.Delete(conn)
				defer func() { _ = conn.Close() }()
				_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
				var head [2]byte
				if _, err := io.ReadFull(conn, head[:]); err != nil || head[0] != 5 {
					return
				}
				methods := make([]byte, int(head[1]))
				if _, err := io.ReadFull(conn, methods); err != nil {
					return
				}
				if _, err := conn.Write([]byte{5, 2}); err != nil {
					return
				}
				if _, err := io.ReadFull(conn, head[:]); err != nil || head[0] != 1 {
					return
				}
				username := make([]byte, int(head[1]))
				if _, err := io.ReadFull(conn, username); err != nil {
					return
				}
				var n [1]byte
				if _, err := io.ReadFull(conn, n[:]); err != nil {
					return
				}
				password := make([]byte, int(n[0]))
				if _, err := io.ReadFull(conn, password); err != nil {
					return
				}
				if string(username) != "proxy-user" || string(password) != "test-only-proxy-password" {
					_, _ = conn.Write([]byte{1, 1})
					return
				}
				if _, err := conn.Write([]byte{1, 0}); err != nil {
					return
				}
				var request [4]byte
				if _, err := io.ReadFull(conn, request[:]); err != nil || request[0] != 5 || request[1] != 1 || request[3] != 1 {
					return
				}
				var endpoint [6]byte
				if _, err := io.ReadFull(conn, endpoint[:]); err != nil {
					return
				}
				address := net.JoinHostPort(net.IP(endpoint[:4]).String(), strconv.Itoa(int(binary.BigEndian.Uint16(endpoint[4:]))))
				if address != target {
					t.Error("proxy received unexpected or unpinned target")
					return
				}
				remote, err := net.DialTimeout("tcp", address, time.Second)
				if err != nil {
					return
				}
				defer func() { _ = remote.Close() }()
				if _, err := conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}); err != nil {
					return
				}
				calls.Add(1)
				done := make(chan struct{})
				go func() { _, _ = io.Copy(remote, conn); _ = remote.Close(); close(done) }()
				_, _ = io.Copy(conn, remote)
				_ = conn.Close()
				<-done
			})
		}
	})
	t.Cleanup(func() {
		_ = listener.Close()
		connections.Range(func(key, value any) bool { _ = key.(net.Conn).Close(); return true })
		workers.Wait()
	})
	return listener.Addr().String(), &calls
}

func testEgressLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var upstreamCalls atomic.Int32
	var pauseDiagnostic atomic.Bool
	var diagnosticMutationMu sync.Mutex
	var diagnosticMutation func() error
	diagnosticEntered := make(chan struct{}, 1)
	diagnosticContinue := make(chan struct{})
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy authentication leaked to target")
		}
		if strings.HasSuffix(r.URL.Path, "/models") {
			diagnosticMutationMu.Lock()
			mutate := diagnosticMutation
			diagnosticMutation = nil
			diagnosticMutationMu.Unlock()
			if mutate != nil {
				if err := mutate(); err != nil {
					t.Error("controlled diagnostic mutation failed", err)
					w.WriteHeader(500)
					return
				}
			}
		}
		if r.URL.Path != "/v1/models" && strings.HasSuffix(r.URL.Path, "/models") {
			wantHeader := map[string]string{
				"/chat/models": "Authorization", "/responses/models": "Authorization",
				"/messages/models": "x-api-key", "/gemini/models": "x-goog-api-key", "/openai/models": "api-key",
			}[r.URL.Path]
			if wantHeader == "" || r.Method != "GET" || r.Header.Get("Accept") != "application/json" {
				t.Error("incorrect candidate diagnostic endpoint")
				w.WriteHeader(400)
				return
			}
			for _, header := range []string{"Authorization", "x-api-key", "x-goog-api-key", "api-key"} {
				want := ""
				if header == wantHeader {
					want = "test-only-candidate-secret"
					if header == "Authorization" {
						want = "Bearer " + want
					}
				}
				if r.Header.Get(header) != want {
					t.Error("incorrect candidate diagnostic authentication", header)
				}
			}
			if (r.URL.Path == "/messages/models") != (r.Header.Get("anthropic-version") == "2023-06-01") {
				t.Error("incorrect candidate Messages version")
			}
			wantQuery := ""
			if r.URL.Path == "/openai/models" {
				wantQuery = "api-version=2024-10-21"
			}
			if r.URL.RawQuery != wantQuery {
				t.Error("incorrect candidate adapter version query")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":[]}`)
			return
		}
		if r.URL.Path == "/v1/models" {
			if pauseDiagnostic.CompareAndSwap(true, false) {
				diagnosticEntered <- struct{}{}
				select {
				case <-diagnosticContinue:
				case <-r.Context().Done():
					return
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":[{"id":"egress-model"}]}`)
			return
		}
		upstreamCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-only-provider-secret" {
			t.Error("missing provider authorization")
		}
		var payload struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"model\":\"egress-model\",\"choices\":[]}\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"egress-model","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	defer target.Close()
	proxyAddress, proxyCalls := egressSOCKSFixture(t, strings.TrimPrefix(target.URL, "http://"))
	proxyHost, proxyPort, _ := net.SplitHostPort(proxyAddress)
	port, _ := strconv.Atoi(proxyPort)
	store, err := secretstore.New(bytes.Repeat([]byte{27}, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithEgressPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"egress@example.invalid","password":"test-only-egress-password","name":"Egress"}`, nil, "")
	expectStatus(t, setup, 201)
	auth, cookie := readIdentity(t, setup)
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		encoded, _ := json.Marshal(body)
		return identityRequest(router, method, path, string(encoded), cookie, auth.CSRFToken)
	}
	input := service.EgressInput{Name: "Test proxy", Kind: "socks5", Host: proxyHost, Port: port, Auth: service.EgressAuthInput{Action: "replace", Username: "proxy-user", Password: "test-only-proxy-password"}, TestTargetBaseURL: target.URL + "/v1"}
	expectStatus(t, identityRequest(router, "POST", "/api/v1/admin/egresses", `{}`, cookie, ""), 403)
	expectStatus(t, request("POST", "/api/v1/admin/egresses", map[string]any{"unknown": true}), 400)
	row := decodeCatalogResponse[service.EgressView](t, request("POST", "/api/v1/admin/egresses", input), 201)
	if !row.AuthConfigured || row.LastDiagnostic == nil || !row.LastDiagnostic.TransportOK {
		t.Fatal("missing tested encrypted egress")
	}
	var stored entity.Egress
	if err := db.First(&stored, "id = ?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored.AuthCiphertext, "test-only") || stored.AuthCiphertext == "" {
		t.Fatal("proxy credentials not encrypted")
	}
	redirect := input
	redirect.ETag = row.ETag
	redirect.Host = "attacker.example.invalid"
	redirect.Auth = service.EgressAuthInput{Action: "keep"}
	expectStatus(t, request("PATCH", "/api/v1/admin/egresses/"+row.ID, redirect), 400)
	draft := map[string]any{
		"egress_id":            row.ID,
		"name":                 redirect.Name,
		"kind":                 redirect.Kind,
		"host":                 redirect.Host,
		"port":                 redirect.Port,
		"enabled":              redirect.Enabled,
		"etag":                 redirect.ETag,
		"auth":                 redirect.Auth,
		"test_target_base_url": redirect.TestTargetBaseURL,
	}
	expectStatus(t, request("POST", "/api/v1/admin/egresses/test", draft), 400)
	var unchanged entity.Egress
	if err := db.First(&unchanged, "id = ?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.ETag != row.ETag || unchanged.Host != stored.Host || unchanged.AuthCiphertext != stored.AuthCiphertext {
		t.Fatal("rejected credential redirect mutated the saved egress")
	}
	listed := request("GET", "/api/v1/admin/egresses", nil)
	expectStatus(t, listed, 200)
	if strings.Contains(listed.Body.String(), "proxy-user") || strings.Contains(listed.Body.String(), "test-only-proxy-password") {
		t.Fatal("secret leaked in list")
	}
	provider, err := svc.CreateProvider(ctx, auth.User.ID, "Egress provider", service.CreateConnectionInput{Name: "Primary", BaseURL: target.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, CredentialName: "Primary", Secret: "test-only-provider-secret"})
	if err != nil {
		t.Fatal(err)
	}
	connection := provider.Connections[0].Connection
	credential := provider.Connections[0].Credentials[0]
	if _, err := svc.VerifyCredential(ctx, auth.User.ID, credential.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredentialEnabled(ctx, auth.User.ID, credential.ID, true); err != nil {
		t.Fatal(err)
	}
	var pm entity.ProviderModel
	if err := db.First(&pm, "connection_id = ?", connection.ID).Error; err != nil {
		t.Fatal(err)
	}
	model, err := svc.CreateModel(ctx, auth.User.ID, "public-egress", pm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetModelWeights(ctx, auth.User.ID, model.Model.ID, []service.ModelWeight{{BindingID: model.Bindings[0].Binding.ID, Weight: 100}}); err != nil {
		t.Fatal(err)
	}
	key, err := svc.CreatePersonalKey(ctx, auth.User.ID, "Egress key", []string{model.Model.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmKeyDelivery(ctx, auth.User.ID, key.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	chat := func(stream bool) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"model": "public-egress", "messages": []any{map[string]string{"role": "user", "content": "Hello"}}, "stream": stream})
		r := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+key.Secret)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	before := proxyCalls.Load()
	expectStatus(t, chat(false), 200)
	if proxyCalls.Load() != before {
		t.Fatal("initial default was not direct")
	}
	defaults := decodeCatalogResponse[service.EgressDefaultView](t, request("GET", "/api/v1/admin/egress-default", nil), 200)
	savedDefault := decodeCatalogResponse[service.EgressDefaultView](t, request("PUT", "/api/v1/admin/egress-default", service.EgressDefaultView{EgressID: &row.ID, ETag: defaults.ETag}), 200)
	expectStatus(t, request("PUT", "/api/v1/admin/egress-default", defaults), 409)
	expectStatus(t, chat(false), 200)
	expectStatus(t, chat(true), 200)
	if proxyCalls.Load() <= before {
		t.Fatal("selected proxy was bypassed")
	}
	input.ETag = row.ETag
	input.Auth = service.EgressAuthInput{Action: "keep"}
	disabled := false
	input.Enabled = &disabled
	row = decodeCatalogResponse[service.EgressView](t, request("PATCH", "/api/v1/admin/egresses/"+row.ID, input), 200)
	count := upstreamCalls.Load()
	blocked := chat(false)
	if blocked.Code < 400 || upstreamCalls.Load() != count {
		t.Fatal("disabled proxy silently fell back")
	}
	direct := decodeCatalogResponse[service.ConnectionEgressView](t, request("PATCH", "/api/v1/admin/connections/"+connection.ID+"/egress", service.ConnectionEgressInput{ETag: connection.ETag, Mode: "direct"}), 200)
	before = proxyCalls.Load()
	expectStatus(t, chat(false), 200)
	if proxyCalls.Load() != before {
		t.Fatal("explicit direct inherited default")
	}
	expectStatus(t, request("PATCH", "/api/v1/admin/connections/"+connection.ID+"/egress", service.ConnectionEgressInput{ETag: connection.ETag, Mode: "default"}), 409)
	if direct.ETag == connection.ETag || savedDefault.EgressID == nil {
		t.Fatal("revision missing")
	}
	// A saved check cannot enable the proxy and never discloses authentication.
	diagnostic := decodeCatalogResponse[service.EgressDiagnosticView](t, request("POST", "/api/v1/admin/egresses/"+row.ID+"/test", service.EgressDiagnosticInput{ETag: row.ETag, TargetBaseURL: target.URL + "/v1"}), 200)
	if !diagnostic.TransportOK || diagnostic.Stale {
		t.Fatal("saved diagnostic failed")
	}
	if err := db.First(&stored, "id = ?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Enabled {
		t.Fatal("diagnostic re-enabled proxy")
	}
	// Connection diagnostics use the selected disabled candidate even though
	// this Connection is explicitly direct. They perform no inference operation.
	before = proxyCalls.Load()
	nativeBefore := upstreamCalls.Load()
	candidateResult := decodeCatalogResponse[service.EgressDiagnosticView](t, request("POST", "/api/v1/admin/egresses/"+row.ID+"/test", service.EgressDiagnosticInput{ETag: row.ETag, ConnectionID: connection.ID}), 200)
	if !candidateResult.TransportOK || !candidateResult.APIOK || candidateResult.Stale || proxyCalls.Load() <= before || upstreamCalls.Load() != nativeBefore {
		t.Fatal("candidate transport was bypassed or diagnostic invoked inference")
	}
	var directAfter entity.ProviderConnection
	if err := db.First(&directAfter, "id = ?", connection.ID).Error; err != nil || directAfter.EgressMode != "direct" || directAfter.EgressID != nil || directAfter.ETag != direct.ETag {
		t.Fatal("candidate diagnostic changed Connection transport", err)
	}
	version := "2024-10-21"
	for _, tc := range []struct {
		name, protocol, adapter, path string
		version                       *string
	}{
		{"Chat", entity.ProtocolOpenAIChat, entity.AdapterNative, "/chat", nil},
		{"Responses", entity.ProtocolOpenAIResponses, entity.AdapterNative, "/responses", nil},
		{"Messages", entity.ProtocolAnthropicMessages, entity.AdapterNative, "/messages", nil},
		{"Gemini", entity.ProtocolGeminiGenerateContent, entity.AdapterNative, "/gemini", nil},
		{"Azure", entity.ProtocolOpenAIChat, entity.AdapterAzureOpenAIClassic, "", &version},
	} {
		candidateProvider, err := svc.CreateProvider(ctx, auth.User.ID, "Diagnostic "+tc.name, service.CreateConnectionInput{Name: tc.name, BaseURL: target.URL + tc.path, Protocol: tc.protocol, Adapter: tc.adapter, APIVersion: tc.version, EgressMode: "direct", CredentialName: "Candidate", Secret: "test-only-candidate-secret"})
		if err != nil {
			t.Fatal("candidate fixture creation", tc.name, err)
		}
		candidateConnection := candidateProvider.Connections[0].Connection
		candidateCredential := candidateProvider.Connections[0].Credentials[0]
		// Fixture eligibility only: the diagnostic must not Verify or Enable it.
		if err := db.Model(&entity.ProviderCredential{}).Where("id = ?", candidateCredential.ID).Updates(map[string]any{"enabled": true, "verification_status": "verified"}).Error; err != nil {
			t.Fatal(err)
		}
		var credentialBefore entity.ProviderCredential
		if err := db.First(&credentialBefore, "id = ?", candidateCredential.ID).Error; err != nil {
			t.Fatal(err)
		}
		before = proxyCalls.Load()
		result := decodeCatalogResponse[service.EgressDiagnosticView](t, request("POST", "/api/v1/admin/egresses/"+row.ID+"/test", service.EgressDiagnosticInput{ETag: row.ETag, ConnectionID: candidateConnection.ID}), 200)
		if result.Stale || !result.TransportOK || !result.APIOK || proxyCalls.Load() <= before || upstreamCalls.Load() != nativeBefore {
			t.Fatal("native adapter candidate diagnostic failed", tc.name)
		}
		var credentialAfter entity.ProviderCredential
		if err := db.First(&credentialAfter, "id = ?", candidateCredential.ID).Error; err != nil || credentialAfter.Ciphertext != credentialBefore.Ciphertext || credentialAfter.Enabled != credentialBefore.Enabled || credentialAfter.VerificationStatus != credentialBefore.VerificationStatus || credentialAfter.VerifiedAt != nil {
			t.Fatal("diagnostic changed verification or secret source", tc.name, err)
		}
		if tc.adapter == entity.AdapterAzureOpenAIClassic {
			if err := db.First(&stored, "id = ?", row.ID).Error; err != nil {
				t.Fatal(err)
			}
			beforeDiagnostic, beforeChecked := stored.LastDiagnostic, stored.LastCheckedAt
			diagnosticMutationMu.Lock()
			diagnosticMutation = func() error {
				return db.Model(&entity.ProviderConnection{}).Where("id = ?", candidateConnection.ID).Update("api_version", "2025-01-01-preview").Error
			}
			diagnosticMutationMu.Unlock()
			staleAdapter := decodeCatalogResponse[service.EgressDiagnosticView](t, request("POST", "/api/v1/admin/egresses/"+row.ID+"/test", service.EgressDiagnosticInput{ETag: row.ETag, ConnectionID: candidateConnection.ID}), 200)
			if !staleAdapter.Stale {
				t.Fatal("changed adapter accepted after native diagnostic")
			}
			if err := db.First(&stored, "id = ?", row.ID).Error; err != nil || stored.LastDiagnostic != beforeDiagnostic || stored.LastCheckedAt == nil || beforeChecked == nil || !stored.LastCheckedAt.Equal(*beforeChecked) {
				t.Fatal("stale adapter diagnostic persisted", err)
			}
		}
	}
	if err := db.First(&stored, "id = ?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	beforeDiagnostic, beforeChecked := stored.LastDiagnostic, stored.LastCheckedAt
	var originalCredential entity.ProviderCredential
	if err := db.First(&originalCredential, "id = ?", credential.ID).Error; err != nil {
		t.Fatal(err)
	}
	replacementCipher, err := store.Seal(credential.ID, "test-only-replaced-provider-secret")
	if err != nil {
		t.Fatal(err)
	}
	diagnosticMutationMu.Lock()
	diagnosticMutation = func() error {
		return db.Model(&entity.ProviderCredential{}).Where("id = ?", credential.ID).Update("ciphertext", replacementCipher).Error
	}
	diagnosticMutationMu.Unlock()
	staleSource := decodeCatalogResponse[service.EgressDiagnosticView](t, request("POST", "/api/v1/admin/egresses/"+row.ID+"/test", service.EgressDiagnosticInput{ETag: row.ETag, ConnectionID: connection.ID}), 200)
	if !staleSource.Stale {
		t.Fatal("changed credential source accepted after native diagnostic")
	}
	if err := db.First(&stored, "id = ?", row.ID).Error; err != nil || stored.LastDiagnostic != beforeDiagnostic || stored.LastCheckedAt == nil || beforeChecked == nil || !stored.LastCheckedAt.Equal(*beforeChecked) {
		t.Fatal("stale source diagnostic persisted", err)
	}
	if err := db.Model(&entity.ProviderCredential{}).Where("id = ?", credential.ID).Update("ciphertext", originalCredential.Ciphertext).Error; err != nil {
		t.Fatal(err)
	}
	if upstreamCalls.Load() != nativeBefore {
		t.Fatal("diagnostic generated an inference request")
	}
	// The slow check belongs to the old revision and must not overwrite a newer edit.
	pauseDiagnostic.Store(true)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- request("POST", "/api/v1/admin/egresses/"+row.ID+"/test", service.EgressDiagnosticInput{ETag: row.ETag, TargetBaseURL: target.URL + "/v1"})
	}()
	select {
	case <-diagnosticEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("diagnostic did not reach controlled target")
	}
	input.ETag, input.Name = row.ETag, "Renamed while testing"
	renamed := decodeCatalogResponse[service.EgressView](t, request("PATCH", "/api/v1/admin/egresses/"+row.ID, input), 200)
	close(diagnosticContinue)
	stale := decodeCatalogResponse[service.EgressDiagnosticView](t, <-done, 200)
	if !stale.Stale {
		t.Fatal("old diagnostic accepted after config revision changed")
	}
	if err := db.First(&stored, "id = ?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ETag != renamed.ETag || stored.LastCheckedAt == nil || !stored.LastCheckedAt.Equal(*renamed.LastCheckedAt) {
		t.Fatal("stale diagnostic overwrote current metadata")
	}
	svc.StopRuntime()
	restarted, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithEgressPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer restarted.StopRuntime()
	router = fox.New()
	New(restarted).RegisterRoutes(router)
	expectStatus(t, chat(false), 200)
}
