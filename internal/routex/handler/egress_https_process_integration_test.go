package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The existing matrix registry owns migration/reset and the backend artifact.
// Prove local HTTPS CONNECT and target TLS across two genuine application processes.
// Retained Vault composition remains outside this scenario.
func testEgressHTTPSProcessLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	db = db.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
	driver := db.Name()
	envName := map[string]string{"postgres": "ROUTEX_TEST_POSTGRES_DSN", "mysql": "ROUTEX_TEST_MYSQL_DSN"}[driver]
	if envName == "" || os.Getenv(envName) == "" {
		t.Fatal("HTTPS process test requires selected matrix database")
	}
	guardDir := os.Getenv("ROUTEX_TEST_OIDC_GUARD_DIR")
	guardInfo, guardErr := os.Lstat(guardDir)
	if !filepath.IsAbs(guardDir) || guardErr != nil || !guardInfo.IsDir() || guardInfo.Mode()&os.ModeSymlink != 0 || guardInfo.Mode().Perm() != 0700 {
		t.Fatal("HTTPS process test requires owned private guard directory")
	}
	guardPath := filepath.Join(guardDir, driver+".json")
	var databaseName string
	// Dedicated-database guard matches the existing real-driver test boundary.
	query := "SELECT current_database()"
	if driver == "mysql" {
		query = "SELECT DATABASE()"
	}
	if db.Raw(query).Scan(&databaseName).Error != nil || databaseName != "routex_test" {
		t.Fatal("HTTPS process test requires dedicated routex_test")
	}
	binary := os.Getenv("ROUTEX_TEST_OIDC_BINARY")
	expectedHash := os.Getenv("ROUTEX_TEST_OIDC_BINARY_SHA256")
	digestBytes, digestErr := hex.DecodeString(expectedHash)
	if !filepath.IsAbs(binary) || digestErr != nil || len(digestBytes) != sha256.Size || hex.EncodeToString(digestBytes) != expectedHash {
		t.Fatal("root-built backend artifact path and SHA256 are required")
	}
	verifyBinary := func() {
		t.Helper()
		st, err := os.Lstat(binary)
		if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0111 == 0 || st.Size() < 1 || st.Size() > 256*1024*1024 {
			t.Fatal("backend artifact must be regular executable")
		}
		file, err := os.Open(binary)
		if err != nil {
			t.Fatal("read backend artifact")
		}
		digest := sha256.New()
		n, readErr := io.Copy(digest, io.LimitReader(file, 256*1024*1024+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || n != st.Size() || hex.EncodeToString(digest.Sum(nil)) != expectedHash {
			t.Fatal("backend artifact differs from root binding")
		}
	}
	verifyBinary()

	root := bytes.Repeat([]byte{141}, 32)
	defer clear(root)

	// Never remove descriptors/journal while an owned process or group may live.
	dir, err := os.MkdirTemp("", "routex-egress-https-process-")
	if err != nil {
		t.Fatal("create private restart directory")
	}
	if os.Chmod(dir, 0700) != nil {
		t.Fatal("restrict restart directory")
	}
	var current *oidcRestartProcess
	cleanupKnown := true
	defer func() {
		if current != nil {
			if err := current.stop(); err != nil {
				cleanupKnown = false
				t.Error("owned HTTPS closure failed")
			}
		}
		if cleanupKnown {
			if err := os.RemoveAll(dir); err != nil {
				t.Error("remove closed fixture")
			}
		} else {
			t.Logf("retained private HTTPS process fixture for root cleanup: %s", dir)
		}
	}()
	// Each role has an independent issuer. Only proxy and target issuers are trusted.
	for _, name := range []string{"proxy", "target", "untrusted"} {
		part := filepath.Join(dir, name)
		if os.Mkdir(part, 0700) != nil {
			t.Fatal("create private TLS fixture")
		}
		oidcWriteTLSMaterial(t, part)
	}
	var roots []byte
	for _, name := range []string{"proxy", "target"} {
		ca, err := os.ReadFile(filepath.Join(dir, name, "ca.pem"))
		if err != nil {
			t.Fatal("read fixture public CA")
		}
		roots = append(roots, ca...)
	}
	rootsPath := filepath.Join(dir, "trusted.pem")
	if os.WriteFile(rootsPath, roots, 0600) != nil || os.Mkdir(filepath.Join(dir, "roots"), 0700) != nil {
		t.Fatal("write isolated process trust roots")
	}
	const providerSecret = "test-only-https-provider-secret"
	const proxyUser = "https-proxy-user"
	const proxyPassword = "test-only-https-proxy-password"
	var targetHTTP, nativeHTTP, badTargetHTTP, badProxyHTTP, connectHTTP atomic.Int32
	var leaks atomic.Bool
	serveTLS := func(name string, handler http.Handler) *httptest.Server {
		t.Helper()
		certificate, err := tls.LoadX509KeyPair(filepath.Join(dir, name, "server.pem"), filepath.Join(dir, name, "server-key.pem"))
		if err != nil {
			t.Fatal("load private fixture TLS identity")
		}
		server := httptest.NewUnstartedServer(handler)
		server.Config.ErrorLog = log.New(io.Discard, "", 0)
		server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
		server.StartTLS()
		return server
	}
	target := serveTLS("target", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHTTP.Add(1)
		if r.Header.Get("Proxy-Authorization") != "" {
			leaks.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer "+providerSecret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/models":
			_, _ = io.WriteString(w, "{\"data\":[{\"id\":\"https-upstream-model\"}]}")
		case r.Method == "POST" && r.URL.Path == "/v1/chat/completions":
			var body map[string]json.RawMessage
			var model string
			if json.NewDecoder(io.LimitReader(r.Body, 32*1024)).Decode(&body) != nil || json.Unmarshal(body["model"], &model) != nil || model != "https-upstream-model" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			nativeHTTP.Add(1)
			_, _ = io.WriteString(w, "{\"id\":\"https-native\",\"object\":\"chat.completion\",\"model\":\"https-upstream-model\",\"choices\":[{\"index\":0,\"message\":{\"role\":\"assistant\",\"content\":\"OK\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer target.Close()
	badTarget := serveTLS("untrusted", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		badTargetHTTP.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer badTarget.Close()
	targetAddress := strings.TrimPrefix(target.URL, "https://")
	badTargetAddress := strings.TrimPrefix(badTarget.URL, "https://")
	var tunnels sync.Map
	var tunnelWorkers sync.WaitGroup
	proxy := serveTLS("proxy", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tunnelWorkers.Add(1)
		defer tunnelWorkers.Done()
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Vault-Token") != "" {
			leaks.Store(true)
		}
		if r.Method != http.MethodConnect || r.Host != targetAddress && r.Host != badTargetAddress {
			t.Error("proxy received non-CONNECT or unpinned target")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte(proxyUser+":"+proxyPassword)) {
			t.Error("proxy authentication missing")
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		remote, err := net.DialTimeout("tcp4", r.Host, time.Second)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		client, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			_ = remote.Close()
			t.Error("fixture CONNECT hijack failed")
			return
		}
		tunnels.Store(client, remote)
		defer tunnels.Delete(client)
		defer func() { _ = client.Close(); _ = remote.Close() }()
		_ = client.SetDeadline(time.Now().Add(20 * time.Second))
		_ = remote.SetDeadline(time.Now().Add(20 * time.Second))
		if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
		connectHTTP.Add(1)
		done := make(chan struct{})
		go func() { _, _ = io.Copy(remote, buffered); _ = remote.Close(); close(done) }()
		_, _ = io.Copy(client, remote)
		_ = client.Close()
		<-done
	}))
	defer func() {
		proxy.Close()
		tunnels.Range(func(client, remote any) bool {
			_ = client.(net.Conn).Close()
			_ = remote.(net.Conn).Close()
			return true
		})
		tunnelWorkers.Wait()
	}()
	badProxy := serveTLS("untrusted", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		badProxyHTTP.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer badProxy.Close()
	// Stop/join the owned app before closing any fixture peer, including failures.
	defer func() {
		if current != nil {
			if current.stop() != nil {
				cleanupKnown = false
				t.Error("owned HTTPS process closure unknown")
			} else {
				current = nil
			}
		}
	}()

	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal("reserve private port")
	}
	address := reservation.Addr().String()
	if reservation.Close() != nil {
		t.Fatal("release private reservation")
	}
	origin := "http://" + address
	configPath := filepath.Join(dir, "config.yaml")
	config := fmt.Sprintf("addr: %q\ndriver: %s\ndsn: %q\nencryption_key: %q\nallow_private_upstreams: true\nallow_private_egresses: true\nevent_queue_path: \"calls.db\"\n", address, driver, "$"+"{ROUTEX_HTTPS_PROCESS_DSN}", "$"+"{ROUTEX_HTTPS_PROCESS_ROOT}")
	if os.WriteFile(configPath, []byte(config), 0600) != nil {
		t.Fatal("write private configuration")
	}
	var environment []string
	blocked := map[string]bool{"SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "GODEBUG": true, "ROUTEX_HTTPS_PROCESS_DSN": true, "ROUTEX_HTTPS_PROCESS_ROOT": true}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if !blocked[key] {
			environment = append(environment, value)
		}
	}
	var debug []string
	for _, value := range strings.Split(os.Getenv("GODEBUG"), ",") {
		if value != "" && !strings.HasPrefix(value, "x509sslcertoverrideplatform=") {
			debug = append(debug, value)
		}
	}
	debug = append(debug, "x509sslcertoverrideplatform=1")
	environment = append(environment, "SSL_CERT_FILE="+rootsPath, "SSL_CERT_DIR="+filepath.Join(dir, "roots"), "GODEBUG="+strings.Join(debug, ","), "ROUTEX_HTTPS_PROCESS_DSN="+os.Getenv(envName), "ROUTEX_HTTPS_PROCESS_ROOT="+base64.StdEncoding.EncodeToString(root))

	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	launch := func() {
		t.Helper()
		verifyBinary()
		probe, err := net.Listen("tcp4", address)
		if err != nil {
			t.Fatal("restart port unavailable; refusing to terminate another listener")
		}
		if probe.Close() != nil {
			t.Fatal("close prelaunch probe")
		}
		process, startErr := oidcStartRestartProcess(binary, configPath, dir, guardPath, environment)
		current = process
		if process != nil {
			t.Logf("owned HTTPS backend process pid=%d expected_pgid=%d actual_pgid=%d group_confirmed=%t", process.pid, process.pgid, process.actualPGID, process.groupConfirmed)
		}
		if startErr != nil {
			t.Fatal("start owned backend process")
		}
		readyCtx, readyCancel := context.WithTimeout(ctx, 30*time.Second)
		defer readyCancel()
		for {
			if process.hasExited() {
				t.Fatal("backend process exited before readiness")
			}
			req, err := http.NewRequestWithContext(readyCtx, "GET", origin+"/health", nil)
			if err != nil {
				t.Fatal("create readiness request")
			}
			response, err := client.Do(req)
			if err == nil {
				body, readErr := io.ReadAll(io.LimitReader(response.Body, 16))
				closeErr := response.Body.Close()
				if response.StatusCode == 200 && readErr == nil && closeErr == nil && string(body) == "ok" && !process.hasExited() {
					return
				}
			}
			select {
			case <-readyCtx.Done():
				t.Fatal("readiness exceeded bound")
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	var cookie *http.Cookie
	var admin SessionResponse
	request := func(method, path string, body any, csrf, bearer string, status int) *httptest.ResponseRecorder {
		t.Helper()
		var encoded []byte
		if body != nil {
			var err error
			encoded, err = json.Marshal(body)
			if err != nil {
				t.Fatal("encode transient API request")
			}
			defer clear(encoded)
		}
		req, err := http.NewRequestWithContext(ctx, method, origin+path, bytes.NewReader(encoded))
		if err != nil {
			t.Fatal("create real API request")
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Origin", origin)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		} else if cookie != nil {
			req.AddCookie(cookie)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal("real API request failed")
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 128*1024+1))
		closeErr := response.Body.Close()
		defer clear(raw)
		if readErr != nil || closeErr != nil || len(raw) > 128*1024 || response.StatusCode != status {
			t.Fatalf("real API response rejected: method=%s path=%s status=%d expected=%d", method, path, response.StatusCode, status)
		}
		if bytes.Contains(raw, []byte(providerSecret)) || bytes.Contains(raw, []byte(proxyPassword)) || bytes.Contains(raw, []byte(proxyUser)) {
			t.Fatal("API response disclosed upstream or proxy credential")
		}
		out := httptest.NewRecorder()
		for key, values := range response.Header {
			out.Header()[key] = append([]string(nil), values...)
		}
		out.WriteHeader(response.StatusCode)
		_, _ = out.Write(raw)
		return out
	}
	api := func(method, path string, body any, status int) *httptest.ResponseRecorder {
		return request(method, path, body, admin.CSRFToken, "", status)
	}
	launch()
	firstPID := current.pid
	setup := api("POST", "/api/v1/setup", map[string]string{"email": "https-process@example.invalid", "password": "test-only-https-process-password", "name": "HTTPS process"}, 201)
	admin, cookie = readIdentity(t, setup)
	setup.Body.Reset()
	if admin.User.ID == "" || admin.CSRFToken == "" || cookie.Value == "" {
		t.Fatal("real setup did not establish administrator session")
	}
	var originalSession entity.Session
	if db.Take(&originalSession, "token_hash = ?", secret.SHA256Hex(cookie.Value)).Error != nil {
		t.Fatal("capture real persisted session")
	}
	storage := decodeCatalogResponse[service.CredentialStorageContext](t, api("GET", "/api/v1/admin/provider-credential-storage-context", nil, 200), 200)
	if storage.StorageSource != "inline" || storage.ETag == "" {
		t.Fatal("isolated initial credential policy is not inline")
	}
	provider := decodeCatalogResponse[ProviderResponse](t, api("POST", "/api/v1/admin/providers", map[string]any{"request_id": "14000000-1111-4111-8111-000000000001", "name": "HTTPS provider", "connection_name": "TLS target", "base_url": target.URL + "/v1", "protocol": entity.ProtocolOpenAIChat, "egress_mode": "direct", "credential_name": "TLS credential", "secret": providerSecret, "storage_policy_etag": storage.ETag}, 201), 201)
	if len(provider.Connections) != 1 || len(provider.Connections[0].Credentials) != 1 {
		t.Fatal("real API catalog creation returned unexpected identities")
	}
	connection := provider.Connections[0]
	credential := connection.Credentials[0]
	credentialPath := "/api/v1/admin/credentials/" + credential.ID
	verified := decodeCatalogResponse[VerifyCredentialResponse](t, api("POST", credentialPath+"/verify", map[string]any{}, 200), 200)
	if !verified.Verified || verified.DiscoveredModels != 1 {
		t.Fatal("real TLS discovery failed")
	}
	var storedCredential entity.ProviderCredential
	if db.Take(&storedCredential, "id = ?", credential.ID).Error != nil || storedCredential.Enabled || storedCredential.Ciphertext == "" || strings.Contains(storedCredential.Ciphertext, providerSecret) {
		t.Fatal("verification enabled credential or plaintext source persisted")
	}
	enabled := decodeCatalogResponse[CredentialResponse](t, api("PATCH", credentialPath, map[string]bool{"enabled": true}, 200), 200)
	if !enabled.Enabled || enabled.VerificationStatus != "verified" {
		t.Fatal("explicit credential enablement failed")
	}
	providers := decodeCatalogResponse[ProvidersResponse](t, api("GET", "/api/v1/admin/providers", nil, 200), 200)
	var pmID string
	for _, p := range providers.Items {
		for _, c := range p.Connections {
			if c.ID == connection.ID && len(c.ProviderModels) == 1 && c.ProviderModels[0].UpstreamName == "https-upstream-model" {
				pmID = c.ProviderModels[0].ID
			}
		}
	}
	if pmID == "" {
		t.Fatal("discovery did not retain exact provider model")
	}
	model := decodeCatalogResponse[ModelResponse](t, api("POST", "/api/v1/admin/models", CreateModelRequest{Name: "https-public-model", ProviderModelID: pmID}, 201), 201)
	if len(model.Bindings) != 1 {
		t.Fatal("model creation binding missing")
	}
	modelPath := "/api/v1/admin/models/" + model.ID
	api("PUT", modelPath+"/weights", UpdateModelWeightsRequest{Weights: []ModelWeightRequest{{BindingID: model.Bindings[0].ID, Weight: 100}}}, 200)
	api("PUT", modelPath+"/grants", map[string]any{"user_ids": []string{admin.User.ID}}, 200)
	createdKey := api("POST", "/api/v1/keys", CreateKeyRequest{Name: "HTTPS native key", ModelIDs: []string{model.ID}}, 201)
	key := decodeCatalogResponse[CreatedKeyResponse](t, createdKey, 201)
	createdKey.Body.Reset()
	if key.Secret == "" {
		t.Fatal("manual transient key secret missing")
	}
	defer func() { key.Secret = ""; admin.CSRFToken = ""; cookie.Value = "" }()
	api("POST", "/api/v1/keys/"+key.Key.ID+"/confirm", nil, 200)
	makeEgress := func(name, endpoint string) service.EgressInput {
		host, portText, err := net.SplitHostPort(strings.TrimPrefix(endpoint, "https://"))
		port, portErr := strconv.Atoi(portText)
		if err != nil || portErr != nil || host != "127.0.0.1" {
			t.Fatal("invalid owned proxy endpoint")
		}
		disabled := false
		return service.EgressInput{Name: name, Kind: "https", Host: host, Port: port, Enabled: &disabled, Auth: service.EgressAuthInput{Action: "replace", Username: proxyUser, Password: proxyPassword}, TestTargetBaseURL: target.URL + "/v1"}
	}
	input := makeEgress("Trusted HTTPS proxy", proxy.URL)
	beforeTarget, before := targetHTTP.Load(), connectHTTP.Load()
	request("POST", "/api/v1/admin/egresses", input, "", "", 403)
	if targetHTTP.Load() != beforeTarget || connectHTTP.Load() != before || nativeHTTP.Load() != 0 {
		t.Fatal("CSRF denial performed target, proxy or native I/O")
	}
	// Disabled creation still requires a valid explicit transport-test target.
	unreviewed := input
	unreviewed.TestTargetBaseURL = ""
	api("POST", "/api/v1/admin/egresses", unreviewed, 400)
	if targetHTTP.Load() != beforeTarget || connectHTTP.Load() != before || nativeHTTP.Load() != 0 {
		t.Fatal("missing-target denial performed target, proxy or native I/O")
	}
	egress := decodeCatalogResponse[service.EgressView](t, api("POST", "/api/v1/admin/egresses", input, 201), 201)
	var storedEgress entity.Egress
	if db.Take(&storedEgress, "id = ?", egress.ID).Error != nil || storedEgress.Enabled || storedEgress.AuthCiphertext == "" || strings.Contains(storedEgress.AuthCiphertext, proxyPassword) || targetHTTP.Load() != beforeTarget+1 || connectHTTP.Load() != before+1 || nativeHTTP.Load() != 0 || egress.LastDiagnostic == nil || !egress.LastDiagnostic.TransportOK || egress.LastDiagnostic.APIOK || egress.LastDiagnostic.HTTPStatus != http.StatusUnauthorized {
		t.Fatal("disabled creation omitted transport validation, enabled, dispatched inference or exposed proxy auth")
	}
	egressHTTPSRequireStages(t, service.EgressDiagnosticView{Diagnostic: *egress.LastDiagnostic}, "passed", "passed")
	beforeTarget, before = targetHTTP.Load(), connectHTTP.Load()
	testPath := "/api/v1/admin/egresses/" + egress.ID + "/test"
	diagnostic := decodeCatalogResponse[service.EgressDiagnosticView](t, api("POST", testPath, service.EgressDiagnosticInput{ETag: egress.ETag, ConnectionID: connection.ID}, 200), 200)
	if !diagnostic.TransportOK || !diagnostic.APIOK || diagnostic.Stale || targetHTTP.Load() != beforeTarget+1 || connectHTTP.Load() != before+1 || nativeHTTP.Load() != 0 {
		t.Fatal("disabled candidate diagnostic bypassed HTTPS or dispatched inference")
	}
	egressHTTPSRequireStages(t, diagnostic, "passed", "passed")
	var directAfter entity.ProviderConnection
	var credentialAfter entity.ProviderCredential
	if db.Take(&directAfter, "id = ?", connection.ID).Error != nil || directAfter.EgressMode != "direct" || directAfter.EgressID != nil || directAfter.ETag != connection.ETag || db.Take(&credentialAfter, "id = ?", credential.ID).Error != nil || !credentialAfter.Enabled || credentialAfter.Ciphertext != storedCredential.Ciphertext || credentialAfter.VerificationStatus != "verified" || db.Take(&storedEgress, "id = ?", egress.ID).Error != nil || storedEgress.Enabled {
		t.Fatal("diagnostic changed persisted routing, source or enablement")
	}
	badInput := makeEgress("Untrusted HTTPS proxy", badProxy.URL)
	beforeTarget, beforeConnect := targetHTTP.Load(), connectHTTP.Load()
	// An untrusted disabled candidate cannot become a persisted egress.
	api("POST", "/api/v1/admin/egresses", badInput, 422)
	if badProxyHTTP.Load() != 0 || targetHTTP.Load() != beforeTarget || connectHTTP.Load() != beforeConnect || nativeHTTP.Load() != 0 {
		t.Fatal("untrusted disabled creation allowed target, proxy or native dispatch")
	}
	badDiagnostic := decodeCatalogResponse[service.EgressDiagnosticView](t, api("POST", "/api/v1/admin/egresses/test", badInput, 200), 200)
	if badDiagnostic.TransportOK || badDiagnostic.APIOK || badDiagnostic.Stale || badProxyHTTP.Load() != 0 || targetHTTP.Load() != beforeTarget || connectHTTP.Load() != beforeConnect || nativeHTTP.Load() != 0 {
		t.Fatal("untrusted proxy TLS allowed HTTP or credential dispatch")
	}
	egressHTTPSRequireStages(t, badDiagnostic, "failed", "skipped")
	var rejectedEgresses int64
	if db.Model(&entity.Egress{}).Where("name = ?", badInput.Name).Count(&rejectedEgresses).Error != nil || rejectedEgresses != 0 {
		t.Fatal("untrusted creation or draft diagnostic persisted an egress")
	}
	badDiagnostic = decodeCatalogResponse[service.EgressDiagnosticView](t, api("POST", testPath, service.EgressDiagnosticInput{ETag: egress.ETag, TargetBaseURL: badTarget.URL + "/v1"}, 200), 200)
	if badDiagnostic.TransportOK || badDiagnostic.APIOK || badDiagnostic.Stale || badTargetHTTP.Load() != 0 || targetHTTP.Load() != beforeTarget || connectHTTP.Load() != beforeConnect+1 || nativeHTTP.Load() != 0 {
		t.Fatal("trusted proxy bypassed independent target TLS")
	}
	egressHTTPSRequireStages(t, badDiagnostic, "passed", "failed")
	beforeTarget, beforeConnect = targetHTTP.Load(), connectHTTP.Load()
	active := true
	input.ETag, input.Enabled, input.Auth, input.TestTargetBaseURL = egress.ETag, &active, service.EgressAuthInput{Action: "keep"}, target.URL+"/v1"
	egress = decodeCatalogResponse[service.EgressView](t, api("PATCH", "/api/v1/admin/egresses/"+egress.ID, input, 200), 200)
	if !egress.Enabled || egress.ETag == input.ETag || targetHTTP.Load() != beforeTarget+1 || connectHTTP.Load() != beforeConnect+1 || nativeHTTP.Load() != 0 || egress.LastDiagnostic == nil || !egress.LastDiagnostic.TransportOK || egress.LastDiagnostic.APIOK || egress.LastDiagnostic.HTTPStatus != http.StatusUnauthorized {
		t.Fatal("explicit tested proxy enablement failed")
	}
	selection := service.ConnectionEgressInput{ETag: connection.ETag, Mode: "proxy", EgressID: &egress.ID}
	selected := decodeCatalogResponse[service.ConnectionEgressView](t, api("PATCH", "/api/v1/admin/connections/"+connection.ID+"/egress", selection, 200), 200)
	if selected.ETag == connection.ETag || selected.Mode != "proxy" || selected.EgressID == nil || *selected.EgressID != egress.ID {
		t.Fatal("reviewed Connection proxy selection failed")
	}
	beforeConnect = connectHTTP.Load()
	api("PATCH", "/api/v1/admin/connections/"+connection.ID+"/egress", selection, 409)
	if connectHTTP.Load() != beforeConnect {
		t.Fatal("stale selection performed proxy I/O")
	}
	var persistedConnection entity.ProviderConnection
	if db.Take(&persistedConnection, "id = ?", connection.ID).Error != nil || db.Take(&storedEgress, "id = ?", egress.ID).Error != nil || db.Take(&storedCredential, "id = ?", credential.ID).Error != nil {
		t.Fatal("capture persisted proxy configuration")
	}
	infer := func() (entity.CallRecord, entity.CallAttempt, service.RuntimeApplicationRecord) {
		t.Helper()
		beforeConnect, beforeNative := connectHTTP.Load(), nativeHTTP.Load()
		response := request("POST", "/v1/chat/completions", map[string]any{"model": model.Name, "messages": []map[string]string{{"role": "user", "content": "Hello"}}}, "", key.Secret, 200)
		requestID := response.Header().Get("X-Request-ID")
		var wire map[string]json.RawMessage
		var choices []map[string]json.RawMessage
		var finishReason string
		if requestID == "" || json.Unmarshal(response.Body.Bytes(), &wire) != nil || json.Unmarshal(wire["choices"], &choices) != nil || len(choices) != 1 || json.Unmarshal(choices[0]["finish_reason"], &finishReason) != nil || finishReason != "stop" || connectHTTP.Load() <= beforeConnect || nativeHTTP.Load() != beforeNative+1 || leaks.Load() {
			t.Fatal("native HTTPS composition did not complete exactly once")
		}
		var call entity.CallRecord
		var attempts []entity.CallAttempt
		deadline := time.Now().Add(10 * time.Second)
		for db.Take(&call, "request_id = ?", requestID).Error != nil || db.Where("request_id = ?", requestID).Find(&attempts).Error != nil || len(attempts) != 1 {
			if time.Now().After(deadline) || ctx.Err() != nil {
				t.Fatal("native immutable call evidence exceeded bound")
			}
			time.Sleep(50 * time.Millisecond)
		}
		attempt := attempts[0]
		if call.Status != "success" || call.ModelID != model.ID || call.KeyID != key.Key.ID || call.UserID != admin.User.ID || call.ProviderID != provider.ID || call.ConnectionID != connection.ID || call.ProviderModelID != pmID || call.Protocol != entity.ProtocolOpenAIChat || call.SnapshotID == "" || attempt.SnapshotID != call.SnapshotID || attempt.CredentialID != credential.ID || attempt.ConnectionID != connection.ID || attempt.ProviderID != provider.ID || attempt.ProviderModelID != pmID || attempt.Status != "success" || attempt.NativeCompletionEvidence != "completed" || !attempt.FinalUsageKnown {
			t.Fatal("native attempt lost exact route, credential, publication or terminal evidence")
		}
		page := decodeCatalogResponse[service.RuntimeApplicationPage](t, api("GET", "/api/v1/admin/runtime/applications?limit=100", nil, 200), 200)
		var application service.RuntimeApplicationRecord
		for _, row := range page.Items {
			if row.SnapshotID == attempt.SnapshotID && row.InstanceStatus == "online" && row.CurrentServingSnapshotMatches != nil && *row.CurrentServingSnapshotMatches {
				if application.ID != "" {
					t.Fatal("multiple current serving process routing receipts")
				}
				application = row
			}
		}
		if page.Scope != "routing_only" || application.ID == "" || page.NextCursor != nil {
			t.Fatal("exact current process publication receipt missing")
		}
		return call, attempt, application
	}
	firstCall, firstAttempt, firstApplication := infer()
	if current.stop() != nil {
		cleanupKnown = false
		t.Fatal("first owned HTTPS process closure unknown")
	}
	current = nil
	transport.CloseIdleConnections()
	launch()
	if current.pid == firstPID {
		t.Fatal("genuine restart reused application PID")
	}
	resumed := decodeCatalogResponse[SessionResponse](t, api("GET", "/api/v1/auth/session", nil, 200), 200)
	var retainedSession entity.Session
	if resumed.User.ID != admin.User.ID || resumed.CSRFToken != admin.CSRFToken || db.Take(&retainedSession, "id = ?", originalSession.ID).Error != nil || !oidcRestartSameSession(originalSession, retainedSession) {
		t.Fatal("restart did not retain exact original administrator session")
	}
	page := decodeCatalogResponse[service.EgressPage](t, api("GET", "/api/v1/admin/egresses", nil, 200), 200)
	found := false
	for _, row := range page.Items {
		if row.ID == egress.ID {
			found = row.Enabled && row.AuthConfigured && row.ETag == egress.ETag && row.Kind == "https" && row.Host == input.Host && row.Port == input.Port
		}
	}
	providers = decodeCatalogResponse[ProvidersResponse](t, api("GET", "/api/v1/admin/providers", nil, 200), 200)
	selectedAfterRestart := false
	for _, p := range providers.Items {
		for _, c := range p.Connections {
			if p.ID == provider.ID && c.ID == connection.ID {
				selectedAfterRestart = c.EgressMode == "proxy" && c.EgressID != nil && *c.EgressID == egress.ID && c.ETag == selected.ETag && len(c.Credentials) == 1 && c.Credentials[0].ID == credential.ID && c.Credentials[0].Enabled
			}
		}
	}
	var retainedConnection entity.ProviderConnection
	var retainedEgress entity.Egress
	var retainedCredential entity.ProviderCredential
	if !found || !selectedAfterRestart || db.Take(&retainedConnection, "id = ?", connection.ID).Error != nil || !reflect.DeepEqual(persistedConnection, retainedConnection) || db.Take(&retainedEgress, "id = ?", egress.ID).Error != nil || !reflect.DeepEqual(storedEgress, retainedEgress) || db.Take(&retainedCredential, "id = ?", credential.ID).Error != nil || !reflect.DeepEqual(storedCredential, retainedCredential) {
		t.Fatal("restart rewrote or failed to restore persisted proxy/source configuration")
	}
	secondCall, _, secondApplication := infer()
	if secondCall.RequestID == firstCall.RequestID || secondApplication.InstanceID == firstApplication.InstanceID || !secondApplication.InstanceStartedAt.After(firstApplication.InstanceStartedAt) {
		t.Fatal("restart reused call or serving process application identity")
	}
	var oldCall entity.CallRecord
	var oldAttempt entity.CallAttempt
	if db.Take(&oldCall, "request_id = ?", firstCall.RequestID).Error != nil || !reflect.DeepEqual(firstCall, oldCall) || db.Take(&oldAttempt, "id = ?", firstAttempt.ID).Error != nil || !reflect.DeepEqual(firstAttempt, oldAttempt) {
		t.Fatal("restart rewrote immutable native history")
	}
	if current.stop() != nil {
		cleanupKnown = false
		t.Fatal("second owned HTTPS process closure unknown")
	}
	current = nil
	probe, probeErr := net.DialTimeout("tcp4", address, 100*time.Millisecond)
	if probe != nil {
		_ = probe.Close()
	}
	if !errors.Is(probeErr, syscall.ECONNREFUSED) {
		cleanupKnown = false
		t.Fatal("owned application listener not refused after closure")
	}
	free, bindErr := net.Listen("tcp4", address)
	if bindErr != nil {
		cleanupKnown = false
		t.Fatal("owned application port not released")
	}
	if free.Close() != nil {
		cleanupKnown = false
		t.Fatal("close final application port proof")
	}
}

func egressHTTPSRequireStages(t *testing.T, diagnostic service.EgressDiagnosticView, proxyTLS, targetTLS string) {
	t.Helper()
	stages := map[string]string{}
	for _, stage := range diagnostic.Stages {
		stages[stage.Stage] = stage.Status
	}
	if stages["proxy_tls"] != proxyTLS || stages["target_tls"] != targetTLS {
		t.Fatal("independent proxy and target TLS stage evidence missing")
	}
}
