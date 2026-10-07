package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
	"github.com/miclle/routex/pkg/vault"
	"gorm.io/gorm"
)

// The real-driver harness owns isolated databases; remote effects use a finite local stub.
func testVaultSavedAppRoleLifecycle(t *testing.T, db *gorm.DB) {
	var mu sync.Mutex
	logins := []string{}
	effects := []string{}
	values := map[string]string{}
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/auth/custom/approle/login" {
			var auth struct {
				RoleID   string `json:"role_id"`
				SecretID string `json:"secret_id"`
			}
			if r.Method != "POST" || r.Header.Get("X-Vault-Token") != "" || json.NewDecoder(r.Body).Decode(&auth) != nil || (auth.RoleID != "writer" && auth.RoleID != "reader") || auth.SecretID != auth.RoleID+"-reusable" {
				t.Error("wrong retained login tuple/identity")
				w.WriteHeader(403)
				return
			}
			logins = append(logins, auth.RoleID)
			_ = json.NewEncoder(w).Encode(map[string]any{"auth": map[string]any{"client_token": "fixture-" + auth.RoleID, "lease_duration": 60}})
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/kv/") {
			t.Error("unexpected KV endpoint")
			w.WriteHeader(404)
			return
		}
		effects = append(effects, r.Method)
		switch r.Method {
		case "POST":
			var body struct {
				Data    map[string]string `json:"data"`
				Options struct {
					CAS int `json:"cas"`
				} `json:"options"`
			}
			if r.Header.Get("X-Vault-Token") != "fixture-writer" || json.NewDecoder(r.Body).Decode(&body) != nil || body.Options.CAS != 0 || len(body.Data) != 1 {
				t.Error("writer/CAS0/data scope")
			}
			if _, ok := values[r.URL.Path]; ok {
				t.Error("write replayed")
			}
			values[r.URL.Path] = body.Data["value"]
			_, _ = w.Write([]byte(`{"data":{"version":1,"destroyed":false,"deletion_time":""}}`))
		case "GET":
			if r.Header.Get("X-Vault-Token") != "fixture-reader" || r.URL.RawQuery != "version=1" {
				t.Error("reader/version scope")
			}
			marker, ok := values[r.URL.Path]
			if !ok {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": map[string]string{"value": marker}, "metadata": map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}}})
		case "PUT":
			var body struct {
				Versions []int `json:"versions"`
			}
			if r.Header.Get("X-Vault-Token") != "fixture-writer" || json.NewDecoder(r.Body).Decode(&body) != nil || !reflect.DeepEqual(body.Versions, []int{1}) {
				t.Error("destroy authorization/scope")
			}
			w.WriteHeader(204)
		default:
			t.Error("unexpected operation")
			w.WriteHeader(400)
		}
	}))
	defer stub.Close()
	store, e := secretstore.New(bytes.Repeat([]byte{118}, 32))
	if e != nil {
		t.Fatal(e)
	}
	svc, e := service.New(context.Background(), db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if e != nil {
		t.Fatal(e)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"approle-admin@example.invalid","password":"test-only-approle-password","name":"Administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	request := func(method, target, raw, etag string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://routex.test"+target, strings.NewReader(raw))
		req.AddCookie(cookie)
		req.Header.Set("Content-Type", "application/json")
		if method != "GET" {
			req.Header.Set("Origin", "http://routex.test")
			req.Header.Set("X-CSRF-Token", admin.CSRFToken)
		}
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	base := "/api/v1/admin/secrets/integrations"
	list := request("GET", base, "", "")
	expectStatus(t, list, 200)
	var page service.VaultIntegrationPage
	if json.Unmarshal(list.Body.Bytes(), &page) != nil {
		t.Fatal("list")
	}
	input := service.VaultConfigInput{RequestID: "78000000-1111-4111-8111-111111111111", Name: "Saved AppRole", Descriptor: service.VaultDescriptor{Endpoint: stub.URL, Mount: "kv", Prefix: "system", DataField: "value"}, Reason: "Reviewed reusable tuples", WriterAuth: service.VaultAuthInput{Action: "replace", Method: "approle", AuthMount: "custom/approle", RoleID: "writer", SecretID: "writer-reusable"}, ReaderAuth: service.VaultAuthInput{Action: "replace", Method: "approle", AuthMount: "custom/approle", RoleID: "reader", SecretID: "reader-reusable"}}
	raw, _ := json.Marshal(input)
	created := request("POST", base, string(raw), page.ReviewETag)
	expectStatus(t, created, 200)
	var saved service.VaultConfigResult
	if json.Unmarshal(created.Body.Bytes(), &saved) != nil || !saved.Committed {
		t.Fatal("save")
	}
	expectStatus(t, request("POST", base, string(raw), page.ReviewETag), 200)
	target := base + "/" + saved.IntegrationID
	get := request("GET", target, "", "")
	expectStatus(t, get, 200)
	var current service.VaultIntegrationView
	if json.Unmarshal(get.Body.Bytes(), &current) != nil || current.WriterAuth != (service.VaultAuthView{Method: "approle", Configured: true}) || current.ReaderAuth != (service.VaultAuthView{Method: "approle", Configured: true}) {
		t.Fatal("recorded method")
	}
	for _, secret := range []string{"writer-reusable", "reader-reusable", `"role_id"`, `"auth_mount"`} {
		if strings.Contains(get.Body.String(), secret) {
			t.Fatal("saved tuple exposed")
		}
	}
	mu.Lock()
	if len(logins) != 0 || len(effects) != 0 {
		t.Error("configuration performed remote auth/effects")
	}
	mu.Unlock()
	var writer entity.VaultWriterAuth
	var reader entity.VaultReaderAuth
	if e = db.Take(&writer, "id = ?", saved.RevisionID).Error; e != nil {
		t.Fatal(e)
	}
	if e = db.Take(&reader, "id = ?", saved.RevisionID).Error; e != nil {
		t.Fatal(e)
	}
	if writer.Method != "approle" || reader.Method != "approle" || strings.Contains(writer.AuthCiphertext, "writer-reusable") || strings.Contains(reader.AuthCiphertext, "reader-reusable") {
		t.Fatal("tuple persistence boundary")
	}
	stage := service.VaultStageInput{RequestID: "78000000-2222-4222-8222-222222222222", Reason: "Explicit write"}
	stageRaw, _ := json.Marshal(stage)
	written := request("POST", target+"/probes/write", string(stageRaw), current.ReviewETag)
	expectStatus(t, written, 200)
	var probe service.VaultProbeView
	if json.Unmarshal(written.Body.Bytes(), &probe) != nil || !probe.Write.Succeeded || probe.Read.Attempted {
		t.Fatal("write stage")
	}
	expectStatus(t, request("POST", target+"/probes/write", string(stageRaw), current.ReviewETag), 200)
	stage.RequestID = "78000000-3333-4333-8333-333333333333"
	stage.Reason = "Explicit reader"
	stageRaw, _ = json.Marshal(stage)
	readPath := target + "/probes/" + probe.ID + "/read"
	read := request("POST", readPath, string(stageRaw), probe.ReviewETag)
	expectStatus(t, read, 200)
	var result service.VaultProbeView
	if json.Unmarshal(read.Body.Bytes(), &result) != nil || !result.Read.Succeeded || result.Cleanup.State != "acknowledged" || result.State != "completed" {
		t.Fatal("read/cleanup stage")
	}
	expectStatus(t, request("POST", readPath, string(stageRaw), probe.ReviewETag), 200)
	mu.Lock()
	if !reflect.DeepEqual(logins, []string{"writer", "reader", "writer"}) || !reflect.DeepEqual(effects, []string{"POST", "GET", "PUT"}) {
		t.Error("separate identity/at-most-once stages")
	}
	mu.Unlock()
	for _, model := range []any{&entity.APIKey{}, &entity.CallRecord{}, &entity.CallAttempt{}} {
		var n int64
		if e = db.Model(model).Count(&n).Error; e != nil || n != 0 {
			t.Fatal("unexpected inference side effect", e)
		}
	}
}

// Exercise the real bounded HTTP decoder, including four maximum-size auth
// fields, maximum review text and the existing complete descriptor bounds.
func TestVaultSavedAppRoleMaximumEscapedConfigurationBody(t *testing.T) {
	for _, tc := range []struct {
		name, character string
		escapeHTML      bool
		want            int
	}{
		{"quotes", `"`, true, 200}, {"backslashes", `\`, true, 200},
		{"html_literal", "<", false, 200}, {"html_expansion_exceeds_body", "<", true, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tuple := func(prefix string) service.VaultAuthInput {
				return service.VaultAuthInput{Action: "replace", Method: "approle", AuthMount: strings.Repeat("a", 128), RoleID: strings.Repeat(tc.character, 4096), SecretID: strings.Repeat(tc.character, 4095) + prefix}
			}
			input := service.VaultConfigInput{RequestID: "78000000-1111-4111-8111-111111111111", Name: strings.Repeat(tc.character, 100), Reason: strings.Repeat(tc.character, 1000), Descriptor: service.VaultDescriptor{Endpoint: "https://example.invalid/" + strings.Repeat("a", 128) + "/" + strings.Repeat("b", 127), Namespace: strings.Repeat("a", 128) + "/" + strings.Repeat("b", 127), Mount: strings.Repeat("a", 128), Prefix: strings.Repeat("a", 128) + "/" + strings.Repeat("b", 127), DataField: strings.Repeat("a", 64)}, WriterAuth: tuple("w"), ReaderAuth: tuple("r")}

			d := input.Descriptor
			client, e := vault.New(vault.Descriptor{Endpoint: d.Endpoint, Namespace: d.Namespace, Mount: d.Mount, Prefix: d.Prefix, DataField: d.DataField}, false)
			if e != nil {
				t.Fatal("maximum descriptor must remain locally valid")
			}
			client.Close()
			var raw bytes.Buffer
			encoder := json.NewEncoder(&raw)
			encoder.SetEscapeHTML(tc.escapeHTML)
			if e := encoder.Encode(input); e != nil {
				t.Fatal("encode fixture")
			}
			if (raw.Len() <= 64<<10) != (tc.want == 200) {
				t.Fatal("fixture did not exercise body bound", raw.Len())
			}
			router := fox.New()
			router.RenderErrorFunc = renderAPIError
			router.POST("/config", func(c *fox.Context) error {
				var decoded service.VaultConfigInput
				if e := vaultBody(c, &decoded); e != nil {
					return e
				}
				if !reflect.DeepEqual(decoded, input) {
					t.Fatal("maximum tuple or reviewed descriptor truncated")
				}
				c.JSON(200, map[string]bool{"accepted": true})
				return nil
			})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest("POST", "/config", bytes.NewReader(raw.Bytes())))
			if rec.Code != tc.want {
				t.Fatal("bounded decoder mismatch", rec.Code, raw.Len())
			}
			if tc.want == 400 && strings.Contains(rec.Body.String(), "role_id") {
				t.Fatal("body material exposed")
			}
			t.Logf("bounded configuration bytes=%d status=%d", raw.Len(), rec.Code)
		})
	}
}
