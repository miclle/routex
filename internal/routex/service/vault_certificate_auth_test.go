package service

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/vault"
	"reflect"
	"strings"
	"testing"
	"time"
)

func savedVaultCertificateInput(t *testing.T, role string) VaultAuthInput {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("fixture key generation")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "controlled certificate issuer"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal("fixture CA")
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("fixture leaf key")
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: role}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, key)
	if err != nil {
		t.Fatal("fixture leaf")
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		t.Fatal("fixture key encoding")
	}
	return VaultAuthInput{Action: "replace", Method: "certificate", AuthMount: "explicit/cert", CertificateRole: role, CertificatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})), ServerCAPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}))}
}
func TestSavedVaultCertificateClosedUnionAndExactEnvelope(t *testing.T) {
	input := savedVaultCertificateInput(t, "reader-role")
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var parsed VaultAuthInput
	if json.Unmarshal(raw, &parsed) != nil || parsed != input {
		t.Fatal("valid certificate union rejected")
	}
	value, err := vaultAuthReplacement(parsed)
	if err != nil || vaultAuthMethod(value) != "certificate" {
		t.Fatal("certificate envelope rejected")
	}
	tuple, err := vaultCertificateMaterial(value)
	if err != nil || tuple.CertificatePEM != input.CertificatePEM || tuple.PrivateKeyPEM != input.PrivateKeyPEM || tuple.ServerCAPEM != input.ServerCAPEM || tuple.AuthMount != input.AuthMount || tuple.CertificateRole != input.CertificateRole {
		t.Fatal("certificate tuple changed")
	}
	if _, err = vaultAuthMaterial("token", value); err == nil {
		t.Fatal("certificate envelope laundered as Token")
	}
	if _, err = vaultAuthMaterial("approle", value); err == nil {
		t.Fatal("certificate envelope laundered as AppRole")
	}
	// Optional public CA canonicalizes to the same explicit empty private field.
	input.ServerCAPEM = ""
	raw, _ = json.Marshal(input)
	if json.Unmarshal(raw, &parsed) != nil {
		t.Fatal("system trust replacement rejected")
	}
	value, err = vaultAuthReplacement(parsed)
	if err != nil || !strings.HasSuffix(value, `"server_ca_pem":""}`) {
		t.Fatal("optional CA was inferred or omitted from private tuple")
	}
}
func TestSavedVaultCertificateRejectsAmbiguousMalformedAndOversizedInput(t *testing.T) {
	good := savedVaultCertificateInput(t, "reader-role")
	mutations := []func(*VaultAuthInput){
		func(v *VaultAuthInput) { v.Token = "mixed" }, func(v *VaultAuthInput) { v.RoleID = "mixed" }, func(v *VaultAuthInput) { v.SecretID = "mixed" },
		func(v *VaultAuthInput) { v.CertificateRole = "" }, func(v *VaultAuthInput) { v.CertificateRole = "unnamed/role" }, func(v *VaultAuthInput) { v.AuthMount = "../cert" },
		func(v *VaultAuthInput) { v.PrivateKeyPEM = "not a key" }, func(v *VaultAuthInput) { v.CertificatePEM = "junk" + v.CertificatePEM },
		func(v *VaultAuthInput) { v.ServerCAPEM = v.PrivateKeyPEM }, func(v *VaultAuthInput) { v.CertificatePEM = strings.Repeat("x", vaultSavedPEMBytes+1) },
		func(v *VaultAuthInput) { v.PrivateKeyPEM += "\u0000" }, func(v *VaultAuthInput) { v.Method = "token" }, func(v *VaultAuthInput) { v.Action = "keep" },
	}
	for i, mutate := range mutations {
		v := good
		mutate(&v)
		raw, _ := json.Marshal(v)
		var parsed VaultAuthInput
		if json.Unmarshal(raw, &parsed) == nil {
			t.Fatalf("invalid certificate union accepted at control %d", i)
		}
	}
	raw, _ := json.Marshal(good)
	for _, suffix := range []string{`,"unknown":"x"}`, `,"auth_mount":"again"}`, `,"server_ca_pem":null}`} {
		malformed := append(append([]byte(nil), raw[:len(raw)-1]...), []byte(suffix)...)
		var parsed VaultAuthInput
		if json.Unmarshal(malformed, &parsed) == nil {
			t.Fatal("duplicate, unknown or null certificate field accepted")
		}
	}
	// Many syntactically valid repeated cert blocks fit each individual bound but
	// must still be rejected when the complete private tuple exceeds32KiB.
	v := good
	v.CertificatePEM = ""
	for len(v.CertificatePEM)+len(good.CertificatePEM) <= vaultSavedPEMBytes {
		v.CertificatePEM += good.CertificatePEM
	}
	v.ServerCAPEM = ""
	for len(v.ServerCAPEM)+len(good.ServerCAPEM) <= vaultSavedPEMBytes {
		v.ServerCAPEM += good.ServerCAPEM
	}
	if len(v.CertificatePEM) > vaultSavedPEMBytes || len(v.ServerCAPEM) > vaultSavedPEMBytes {
		t.Fatal("fixture no longer isolates aggregate bound")
	}
	v.PrivateKeyPEM = good.PrivateKeyPEM + strings.Repeat(" ", vaultSavedPEMBytes-len(good.PrivateKeyPEM))
	if _, err := vaultAuthReplacement(v); err == nil {
		t.Fatal("oversized aggregate envelope accepted")
	}
}
func TestSavedVaultCertificateSaveReplayPrivacyAndPurposeBoundEncryption(t *testing.T) {
	s, f, roles := vaultCommandService(t)
	actor := roles.data.users["usr_admin"]
	f.data.rev.Endpoint = "https://vault.example.test"
	old := f.data.clone()
	input := VaultConfigInput{RequestID: "34333333-3333-4333-8333-333333333333", Name: old.row.Name, Descriptor: vaultDescriptor(old.rev), WriterAuth: savedVaultCertificateInput(t, "writer-role"), ReaderAuth: savedVaultCertificateInput(t, "reader-role"), Reason: "Reviewed certificate identities"}
	etag := vaultReview(actor, old.row, old.rev)
	result, err := s.SaveVaultIntegration(t.Context(), actor.ID, old.row.ID, etag, input)
	if err != nil || result == nil || !result.Committed || f.calls != 0 || f.data.writer.Method != "certificate" || f.data.reader.Method != "certificate" {
		t.Fatal("certificate save failed or performed network I/O")
	}
	ciphertext := f.data.reader.AuthCiphertext
	again, err := s.SaveVaultIntegration(t.Context(), actor.ID, old.row.ID, etag, input)
	if err != nil || !reflect.DeepEqual(result, again) || f.calls != 0 || f.data.reader.AuthCiphertext != ciphertext {
		t.Fatal("exact retry changed certificate identity")
	}
	changed := input
	changed.ReaderAuth.CertificateRole = "different-role"
	if _, err = s.SaveVaultIntegration(t.Context(), actor.ID, old.row.ID, etag, changed); err == nil || f.calls != 0 {
		t.Fatal("UUID accepted changed certificate tuple")
	}
	for _, receipt := range f.data.receipts {
		for _, material := range []string{input.WriterAuth.CertificatePEM, input.WriterAuth.PrivateKeyPEM, input.WriterAuth.ServerCAPEM, input.WriterAuth.CertificateRole, input.ReaderAuth.PrivateKeyPEM} {
			if strings.Contains(receipt.IntentJSON, material) {
				t.Fatal("certificate material leaked into durable intent")
			}
		}
	}
	view, err := s.GetVaultIntegration(t.Context(), actor.ID, f.data.row.ID)
	if err != nil || view.WriterAuth != (VaultAuthView{"certificate", true}) || view.ReaderAuth != (VaultAuthView{"certificate", true}) {
		t.Fatal("safe certificate metadata missing")
	}
	raw, _ := json.Marshal(view)
	if bytes.Contains(raw, []byte("-----BEGIN")) || bytes.Contains(raw, []byte("certificate_role")) {
		t.Fatal("certificate material leaked into view")
	}
	row := f.data.reader
	original := rootReference("vault_reader_auth", row.ID, row.SecretGeneration)
	opened, err := s.openSecret(original, row.AuthCiphertext)
	if err != nil {
		t.Fatal("original reader tuple did not decrypt")
	}
	if _, err = vaultCertificateMaterial(opened); err != nil {
		t.Fatal("decrypted tuple invalid")
	}
	for _, ref := range []string{rootReference("vault_writer_auth", row.ID, row.SecretGeneration), rootReference("vault_reader_auth", row.ID, "substituted-generation")} {
		if _, err = s.openSecret(ref, row.AuthCiphertext); err == nil {
			t.Fatal("certificate ciphertext crossed purpose/generation")
		}
	}
}
func TestSavedVaultCertificateHTTPSAndKeptIdentityRemainConfigurationOnly(t *testing.T) {
	s, f, roles := vaultCommandService(t)
	actor := roles.data.users["usr_admin"]
	original := f.data.clone()
	input := VaultConfigInput{RequestID: "35333333-3333-4333-8333-333333333333", Name: original.row.Name, Descriptor: vaultDescriptor(original.rev), WriterAuth: savedVaultCertificateInput(t, "writer-role"), ReaderAuth: savedVaultCertificateInput(t, "reader-role"), Reason: "Reviewed HTTPS requirement"}
	if _, err := s.SaveVaultIntegration(t.Context(), actor.ID, original.row.ID, vaultReview(actor, original.row, original.rev), input); err == nil || f.calls != 0 {
		t.Fatal("certificate saved on plain HTTP")
	}
	if f.data.row.RevisionID != original.row.RevisionID {
		t.Fatal("rejected certificate replaced current revision")
	}
	input.Descriptor.Endpoint = "https://vault.example.test"
	result, err := s.SaveVaultIntegration(t.Context(), actor.ID, original.row.ID, vaultReview(actor, original.row, original.rev), input)
	if err != nil || result == nil || !result.Committed || f.calls != 0 {
		t.Fatal("HTTPS certificate config failed")
	}
	saved := f.data.clone()
	kept := VaultConfigInput{RequestID: "36333333-3333-4333-8333-333333333333", Name: saved.row.Name, Descriptor: vaultDescriptor(saved.rev), WriterAuth: VaultAuthInput{Action: "keep"}, ReaderAuth: VaultAuthInput{Action: "keep"}, Reason: "Reviewed kept identity transport"}
	kept.Descriptor.Endpoint = original.rev.Endpoint
	if _, err = s.SaveVaultIntegration(t.Context(), actor.ID, saved.row.ID, vaultReview(actor, saved.row, saved.rev), kept); err == nil || !reflect.DeepEqual(saved, f.data) || f.calls != 0 {
		t.Fatal("kept certificate escaped HTTPS or changed config")
	}
	for _, action := range []string{"keep", "remove"} {
		if _, err := vaultAuthValue(VaultAuthInput{Action: action, CertificatePEM: input.WriterAuth.CertificatePEM}, "retained", false); err == nil {
			t.Fatal("direct auth action admitted unexpected material")
		}
	}
}

// The application fixture retains the existing SQL transaction/claim authority;
// this peer adds verified TLS identities instead of substituting a transport.
type savedCertificatePeer struct {
	mu             sync.Mutex
	calls          []string
	data           map[string]map[string]string
	writer, reader VaultAuthInput
	server         *httptest.Server
	before         func()
}

func savedCertificatePeerFixture(t *testing.T) *savedCertificatePeer {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("peer CA key")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "application peer issuer"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal("peer CA certificate")
	}
	caPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}))
	leaf := func(name string, server bool) (string, string) {
		t.Helper()
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal("peer leaf key")
		}
		cert := &x509.Certificate{SerialNumber: big.NewInt(int64(len(name) + 2)), Subject: pkix.Name{CommonName: name}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
		if server {
			cert.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
			cert.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
		}
		der, err := x509.CreateCertificate(rand.Reader, cert, ca, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal("peer leaf certificate")
		}
		raw, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal("peer private key")
		}
		return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}))
	}
	makeInput := func(name string) VaultAuthInput {
		cert, key := leaf(name, false)
		return VaultAuthInput{Action: "replace", Method: "certificate", AuthMount: "explicit/cert", CertificateRole: name + "-role", CertificatePEM: cert, PrivateKeyPEM: key, ServerCAPEM: caPEM}
	}
	peer := &savedCertificatePeer{data: map[string]map[string]string{}, writer: makeInput("writer"), reader: makeInput("reader")}
	serverCert, serverKey := leaf("server", true)
	pair, err := tls.X509KeyPair([]byte(serverCert), []byte(serverKey))
	if err != nil {
		t.Fatal("peer server pair")
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM([]byte(caPEM))
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer.mu.Lock()
		defer peer.mu.Unlock()
		if peer.before != nil {
			peer.before()
		}
		if r.TLS == nil || len(r.TLS.PeerCertificates) != 1 || r.ProtoMajor != 1 || !r.Close {
			w.WriteHeader(403)
			return
		}
		identity := r.TLS.PeerCertificates[0].Subject.CommonName
		if identity != "writer" && identity != "reader" {
			w.WriteHeader(403)
			return
		}
		peer.calls = append(peer.calls, identity+":"+r.Method+":"+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/auth/explicit/cert/login" {
			var body map[string]string
			if r.Method != "POST" || r.Header.Get("X-Vault-Token") != "" || json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body) != nil || len(body) != 1 || body["name"] != identity+"-role" {
				w.WriteHeader(403)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"auth": map[string]any{"client_token": identity + "-token", "lease_duration": 60}})
			return
		}
		if r.Header.Get("X-Vault-Token") != identity+"-token" {
			w.WriteHeader(403)
			return
		}
		path := strings.Replace(r.URL.Path, "/destroy/", "/data/", 1)
		switch r.Method {
		case "POST":
			var body struct {
				Options struct {
					CAS int `json:"cas"`
				} `json:"options"`
				Data map[string]string `json:"data"`
			}
			if identity != "writer" || json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&body) != nil || body.Options.CAS != 0 || peer.data[path] != nil {
				w.WriteHeader(409)
				return
			}
			peer.data[path] = body.Data
			_, _ = io.WriteString(w, `{"data":{"version":1,"destroyed":false,"deletion_time":""}}`)
		case "GET":
			if identity != "reader" || r.URL.RawQuery != "version=1" || peer.data[path] == nil {
				w.WriteHeader(403)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": peer.data[path], "metadata": map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}}})
		case "PUT":
			var body struct {
				Versions []int `json:"versions"`
			}
			if identity != "writer" || json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body) != nil || !reflect.DeepEqual(body.Versions, []int{1}) || peer.data[path] == nil {
				w.WriteHeader(403)
				return
			}
			delete(peer.data, path)
			w.WriteHeader(204)
		default:
			w.WriteHeader(405)
		}
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.Config.ReadHeaderTimeout = time.Second
	server.Config.ReadTimeout = 2 * time.Second
	server.Config.WriteTimeout = 2 * time.Second
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}
	server.StartTLS()
	peer.server = server
	t.Cleanup(server.Close)
	return peer
}
func TestSavedVaultCertificateProbeClaimAndKVIdentityRemainExact(t *testing.T) {
	s, f, roles := vaultCommandService(t)
	actor := roles.data.users["usr_admin"]
	peer := savedCertificatePeerFixture(t)
	peer.before = func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.active != nil {
			t.Error("certificate I/O crossed DB lock")
		}
		claimed := false
		for _, command := range f.data.commands {
			if command.FinishedAt == nil {
				_, claimed = f.data.probes[command.ProbeID]
				if claimed {
					break
				}
			}
		}
		if !claimed {
			t.Error("certificate I/O preceded durable command")
		}
	}
	old := f.data.clone()
	descriptor := vaultDescriptor(old.rev)
	descriptor.Endpoint = peer.server.URL
	input := VaultConfigInput{RequestID: "37333333-3333-4333-8333-333333333333", Name: old.row.Name, Descriptor: descriptor, WriterAuth: peer.writer, ReaderAuth: peer.reader, Reason: "Reviewed verified certificate peer"}
	if result, err := s.SaveVaultIntegration(t.Context(), actor.ID, old.row.ID, vaultReview(actor, old.row, old.rev), input); err != nil || result == nil || !result.Committed {
		t.Fatal("certificate config failed")
	}
	if len(peer.calls) != 0 {
		t.Fatal("save performed remote operation")
	}
	writeInput := VaultStageInput{"38333333-3333-4333-8333-333333333333", "Explicit Write"}
	writeReview := vaultReview(actor, f.data.row, f.data.rev)
	write, _, err := s.RunVaultProbe(t.Context(), actor.ID, f.data.row.ID, "", "write", writeReview, writeInput)
	if err != nil || write == nil || !write.Write.Succeeded || write.Read.Attempted || write.State != "awaiting_read" {
		t.Fatal("certificate Write lost explicit boundary")
	}
	if _, _, err = s.RunVaultProbe(t.Context(), actor.ID, f.data.row.ID, "", "write", writeReview, writeInput); err != nil {
		t.Fatal("exact write replay failed")
	}
	readInput := VaultStageInput{"39333333-3333-4333-8333-333333333333", "Explicit Read"}
	read, _, err := s.RunVaultProbe(t.Context(), actor.ID, f.data.row.ID, write.ID, "read", write.ReviewETag, readInput)
	if err != nil || read == nil || !read.Read.Succeeded || read.Cleanup.State != "acknowledged" || read.State != "completed" {
		t.Fatal("certificate Read and writer cleanup failed")
	}
	if _, _, err = s.RunVaultProbe(t.Context(), actor.ID, f.data.row.ID, write.ID, "read", write.ReviewETag, readInput); err != nil {
		t.Fatal("exact read replay failed")
	}
	peer.mu.Lock()
	calls := append([]string(nil), peer.calls...)
	remaining := len(peer.data)
	peer.mu.Unlock()
	if len(calls) != 6 || remaining != 0 || !strings.HasPrefix(calls[1], "writer:POST:") || !strings.HasPrefix(calls[4], "reader:GET:") || !strings.HasPrefix(calls[5], "writer:PUT:") {
		t.Fatal("identity crossed KV or replay dispatched")
	}
}
func TestSavedVaultCertificateFiniteOperationRejectsUnissuedTokenAndClearsBindings(t *testing.T) {
	peer := savedCertificatePeerFixture(t)
	rev := entity.VaultRevision{Endpoint: peer.server.URL, Mount: "kv", Prefix: "providers", DataField: "value"}
	base, err := vaultClient(rev, true)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := base.PrepareCredential()
	if err != nil {
		base.Close()
		t.Fatal(err)
	}
	defer prepared.Close()
	op := &credentialFiniteOperation{client: base, clients: []*vault.Client{base}, identities: map[string]*vault.Client{}, revision: rev, allowPrivate: true, purpose: "create", plan: prepared.Plan()}
	defer op.close()
	writerMaterial, err := vaultAuthReplacement(peer.writer)
	if err != nil {
		t.Fatal("writer envelope")
	}
	readerMaterial, err := vaultAuthReplacement(peer.reader)
	if err != nil {
		t.Fatal("reader envelope")
	}
	writer, releaseW, _, err := op.token(t.Context(), "certificate", writerMaterial)
	if err != nil {
		t.Fatal("writer login")
	}
	defer releaseW()
	if _, err = op.write(t.Context(), "unissued-token", prepared, []byte("controlled-provider-value")); err == nil {
		t.Fatal("unissued Token accepted")
	}
	if result, err := op.write(t.Context(), writer, prepared, []byte("controlled-provider-value")); err != nil || !result.Write.Succeeded {
		t.Fatal("writer identity not retained")
	}
	reader, releaseR, _, err := op.token(t.Context(), "certificate", readerMaterial)
	if err != nil {
		t.Fatal("reader login")
	}
	defer releaseR()
	value, result, err := op.read(t.Context(), reader, prepared.Plan())
	if err != nil || value == nil || !result.Read.Succeeded {
		t.Fatal("reader identity not retained")
	}
	defer value.Close()
	copied := value.Bytes()
	defer clear(copied)
	if string(copied) != "controlled-provider-value" {
		t.Fatal("resolved value changed")
	}
	op.close()
	if len(op.identities) != 0 || op.admissible() {
		t.Fatal("operation retained usable issued-token bindings")
	}
	peer.mu.Lock()
	calls := len(peer.calls)
	peer.mu.Unlock()
	if calls != 4 {
		t.Fatal("unissued Token performed HTTP or operation replayed")
	}
}

func TestSavedVaultCertificateExactTupleDuplicateRuleDoesNotInferPrincipal(t *testing.T) {
	s, f, roles := vaultCommandService(t)
	actor := roles.data.users["usr_admin"]
	old := f.data.clone()
	writer := savedVaultCertificateInput(t, "writer-role")
	descriptor := vaultDescriptor(old.rev)
	descriptor.Endpoint = "https://vault.example.test"
	input := VaultConfigInput{RequestID: "40333333-3333-4333-8333-333333333333", Name: old.row.Name, Descriptor: descriptor, WriterAuth: writer, ReaderAuth: writer, Reason: "Reviewed exact tuple separation"}
	if _, err := s.SaveVaultIntegration(t.Context(), actor.ID, old.row.ID, vaultReview(actor, old.row, old.rev), input); err == nil || !reflect.DeepEqual(old, f.data) || f.calls != 0 {
		t.Fatal("duplicate exact identity accepted")
	}
	input.ReaderAuth.CertificateRole = "independently-named-reader"
	result, err := s.SaveVaultIntegration(t.Context(), actor.ID, old.row.ID, vaultReview(actor, old.row, old.rev), input)
	if err != nil || result == nil || !result.Committed || f.calls != 0 {
		t.Fatal("different explicit role inferred as same principal")
	}
}

func TestSavedVaultCertificatePublicOverheadDoesNotShrinkPrivateEnvelope(t *testing.T) {
	input := savedVaultCertificateInput(t, "reader-role")
	cert, ca := input.CertificatePEM, input.ServerCAPEM
	for len(input.CertificatePEM)+len(cert) < 12<<10 {
		input.CertificatePEM += cert
	}
	for len(input.ServerCAPEM)+len(ca) < 12<<10 {
		input.ServerCAPEM += ca
	}
	value, err := vaultAuthReplacement(input)
	if err != nil {
		t.Fatal("bounded base tuple")
	}
	padding := (32 << 10) - len(value)
	if padding <= 0 || len(input.PrivateKeyPEM)+padding > vaultSavedPEMBytes {
		t.Fatal("fixture no longer isolates public overhead")
	}
	input.PrivateKeyPEM += strings.Repeat(" ", padding)
	value, err = vaultAuthReplacement(input)
	if err != nil || len(value) != 32<<10 {
		t.Fatal("exact encrypted envelope cap rejected")
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) <= 32<<10 || len(raw) > 64<<10 {
		t.Fatal("fixture public overhead boundary changed")
	}
	var parsed VaultAuthInput
	if json.Unmarshal(raw, &parsed) != nil || parsed != input {
		t.Fatal("public JSON overhead incorrectly consumed private envelope budget")
	}
	input.PrivateKeyPEM += " "
	if _, err = vaultAuthReplacement(input); err == nil {
		t.Fatal("private envelope cap widened")
	}
}
