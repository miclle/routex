package handler

// Acceptance-only source overlay. Root provisions and owns the real Vault,
// dedicated test databases and exact process groups. This file is never shipped.
import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/pkg/upstream"
)

type f14VaultAcceptanceInput struct {
	RootGO          string `json:"root_go"`
	Endpoint        string `json:"endpoint"`
	WriterToken     string `json:"writer_token"`
	ReaderToken     string `json:"reader_token"`
	ObservationPath string `json:"observation_path"`
}

type f14VaultAcceptance struct {
	input  f14VaultAcceptanceInput
	client *http.Client
	file   *os.File
	mu     sync.Mutex
	count  int
}

func newF14VaultAcceptance(t *testing.T) *f14VaultAcceptance {
	t.Helper()
	path := os.Getenv("ROUTEX_F14_REAL_VAULT_INPUT")
	if path == "" {
		return nil // The original controlled fixture is byte-semantically retained.
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 16384 {
		t.Fatal("F14 requires a private bounded regular input")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("F14 cannot read private input")
	}
	var input f14VaultAcceptanceInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.RootGO != "F14_REAL_VAULT_CANDIDATE_DIAGNOSTIC_ONLY" {
		t.Fatal("F14 root execution binding is missing")
	}
	u, err := url.Parse(input.Endpoint)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.ForceQuery {
		t.Fatal("F14 requires the captured owned loopback Vault endpoint")
	}
	if _, _, err := net.SplitHostPort(u.Host); err != nil || input.WriterToken == "" || input.ReaderToken == "" || input.WriterToken == input.ReaderToken || len(input.WriterToken) > 4096 || len(input.ReaderToken) > 4096 {
		t.Fatal("F14 requires distinct bounded real Vault identities")
	}
	if input.ObservationPath == "" {
		t.Fatal("F14 requires a fresh private observation file")
	}
	parts := strings.Split(t.Name(), "/")
	if len(parts) != 3 || parts[0] != "TestIdentityIntegration" || (parts[1] != "postgres" && parts[1] != "mysql") || parts[2] != "provider_credential_storage" {
		t.Fatal("F14 adapter is scoped to the original credential-storage scenario")
	}
	file, err := os.OpenFile(input.ObservationPath+"."+parts[1]+".jsonl", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("F14 observation file must be new")
	}
	client := upstream.NewNonReplayingClient(true)
	client.Timeout = 5 * time.Second
	v := &f14VaultAcceptance{input: input, client: client, file: file}
	t.Cleanup(func() {
		client.CloseIdleConnections()
		if err := file.Close(); err != nil {
			t.Error("F14 observation close failed")
		}
	})
	return v
}

// Forward the original SDK operation once. Do not replace it with fabricated
// version/ownership data. The candidate SOCKS proxy is not Vault's transport.
func (v *f14VaultAcceptance) forward(t *testing.T, incoming *http.Request, value any, expected map[string]string) ([]byte, bool) {
	t.Helper()
	if v == nil {
		return nil, false
	}
	identity := "reader"
	if incoming.Method == http.MethodPost {
		identity = "writer"
	}
	pathOK := regexp.MustCompile(`^/v1/kv/data/(provider-secrets|future-secrets)/routex-credential-[0-9a-f]{32}$`).MatchString(incoming.URL.Path)
	if !pathOK || (incoming.Method != http.MethodGet && incoming.Method != http.MethodPost) || incoming.URL.RawQuery != map[bool]string{true: "version=1", false: ""}[incoming.Method == http.MethodGet] {
		t.Error("F14 rejected an unowned or destructive Vault operation")
		return nil, false
	}
	token := v.input.ReaderToken
	if identity == "writer" {
		token = v.input.WriterToken
	}
	if incoming.Header.Get("X-Vault-Token") != token {
		t.Error("F14 actual retained Vault identity mismatch")
		return nil, false
	}
	var body []byte
	if value != nil {
		var err error
		body, err = json.Marshal(value)
		if err != nil || len(body) > 8192 {
			t.Error("F14 Vault request bound")
			return nil, false
		}
	}
	req, err := http.NewRequestWithContext(incoming.Context(), incoming.Method, v.input.Endpoint+incoming.URL.RequestURI(), bytes.NewReader(body))
	if err != nil {
		t.Error("F14 real Vault request invalid")
		return nil, false
	}
	req.Header.Set("X-Vault-Token", token)
	req.Header.Set("Content-Type", "application/json")
	response, err := v.client.Do(req)
	if err != nil {
		t.Error("F14 real Vault exchange failed")
		return nil, false
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, 65537))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 65536 || response.StatusCode != http.StatusOK {
		t.Error("F14 real Vault response/close/status failed")
		return nil, false
	}
	var receipt struct {
		RequestID string `json:"request_id"`
		Data      struct {
			Version  int64             `json:"version"`
			Data     map[string]string `json:"data"`
			Metadata struct {
				Version      int64  `json:"version"`
				Destroyed    bool   `json:"destroyed"`
				DeletionTime string `json:"deletion_time"`
			} `json:"metadata"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &receipt) != nil || !regexp.MustCompile(`^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$`).MatchString(receipt.RequestID) {
		t.Error("F14 real Vault receipt missing")
		return nil, false
	}
	version := receipt.Data.Version
	if identity == "reader" {
		version = receipt.Data.Metadata.Version
		if receipt.Data.Metadata.Destroyed || receipt.Data.Metadata.DeletionTime != "" || len(receipt.Data.Data) != len(expected) {
			t.Error("F14 retained object metadata changed")
			return nil, false
		}
		for key, value := range expected {
			if receipt.Data.Data[key] != value {
				t.Error("F14 retained object ownership/value changed")
				return nil, false
			}
		}
	}
	if version != 1 {
		t.Error("F14 selected another Vault version")
		return nil, false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.count >= 64 {
		t.Error("F14 real Vault observation cap")
		return nil, false
	}
	v.count++
	object := sha256.Sum256([]byte(incoming.URL.Path))
	observation := struct {
		Ordinal      int    `json:"ordinal"`
		Method       string `json:"method"`
		Identity     string `json:"identity"`
		ObjectSHA256 string `json:"object_sha256"`
		Version      int64  `json:"version"`
		Status       int    `json:"status"`
		RequestID    string `json:"request_id"`
	}{v.count, incoming.Method, identity, hex.EncodeToString(object[:]), version, response.StatusCode, receipt.RequestID}
	encoded, err := json.Marshal(observation)
	if err != nil {
		t.Error("F14 observation encoding failed")
		return nil, false
	}
	if _, err = fmt.Fprintf(v.file, "%s\n", encoded); err != nil {
		t.Error("F14 observation write failed")
		return nil, false
	}
	if err = v.file.Sync(); err != nil {
		t.Error("F14 observation sync failed")
		return nil, false
	}
	return raw, true
}
