package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"

	"github.com/miclle/routex/internal/routex/config"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCredentialStoreBootstrap(t *testing.T) {
	store, err := credentialStore("")
	if err != nil || store != nil {
		t.Fatal("empty key should leave credential storage unavailable")
	}
	for _, invalid := range []string{"private-invalid-key", base64.StdEncoding.EncodeToString([]byte("private-short-key"))} {
		if _, err := credentialStore(invalid); err == nil || strings.Contains(err.Error(), invalid) {
			t.Fatal("invalid key must produce a sanitized configuration error")
		}
	}
	store, err = credentialStore(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{17}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := store.Seal("crd_test", "test-only-secret")
	if err != nil {
		t.Fatal(err)
	}
	if plaintext, err := store.Open("crd_test", ciphertext); err != nil || plaintext != "test-only-secret" {
		t.Fatal("clearing bootstrap input damaged the credential store")
	}
}

func TestRunRejectsConfigurationWithoutLeakingValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("addr: localhost:9000\ndriver: private-config-marker\ndsn: private-database-marker\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), path); err == nil || strings.Contains(err.Error(), "private-") {
		t.Fatal("startup must hide raw configuration errors")
	}
}

func TestHTTPShutdownDrainsActiveRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	requestCanceled := make(chan bool, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		requestCanceled <- r.Context().Err() != nil
		w.WriteHeader(http.StatusNoContent)
	})
	finished := make(chan error, 1)
	go func() { finished <- serveHTTP(ctx, listener, handler, time.Second) }()
	responseDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			_, err = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
		responseDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case <-finished:
		t.Fatal("shutdown returned before its active request finished")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if <-requestCanceled {
		t.Fatal("shutdown canceled an in-flight request before its grace period")
	}
	if err := <-responseDone; err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestHTTPShutdownDeadlineCancelsActiveRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	entered, stopped := make(chan struct{}), make(chan struct{})
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(stopped)
	})
	finished := make(chan error, 1)
	go func() { finished <- serveHTTP(ctx, listener, handler, 20*time.Millisecond) }()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			_ = response.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	if err := <-finished; err == nil {
		t.Fatal("expired shutdown deadline should report failure")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("deadline did not cancel the active request")
	}
	<-clientDone
}

func TestHTTPServeFailureClosesListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := serveHTTP(context.Background(), listener, http.NotFoundHandler(), time.Second); err == nil {
		t.Fatal("a closed listener should fail startup")
	}
}

func loadBootstrapFixture(t *testing.T, body string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("addr: localhost:9000\ndsn: test-only\n"+body), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func envelopeSelection(t *testing.T, value string) (int, string) {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Version int    `json:"v"`
		KeyID   string `json:"kid"`
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	return metadata.Version, metadata.KeyID
}

func TestLegacyCredentialStoreKeepsBase64LineHandling(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{17}, 32))
	if store, err := credentialStore(encoded[:20] + "\r\n" + encoded[20:]); err != nil || store == nil {
		t.Fatal("existing single-key base64 behavior changed")
	}
}

func TestConfiguredCredentialStorePreservesLegacyBootstrap(t *testing.T) {
	without, err := credentialStoreForConfig(loadBootstrapFixture(t, ""))
	if err != nil || without != nil {
		t.Fatal("no-key configuration must keep secret storage unavailable")
	}
	encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{17}, 32))
	cfg := loadBootstrapFixture(t, "encryption_key: '"+encoded+"'\n")
	store, err := credentialStoreForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := store.Seal("crd_legacy", "test-only-secret")
	if err != nil {
		t.Fatal(err)
	}
	version, id := envelopeSelection(t, sealed)
	if version != 1 || id != "" || cfg.EncryptionKeyring != nil {
		t.Fatal("single-key bootstrap changed its envelope format")
	}
	restarted, err := credentialStore(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if plaintext, err := restarted.Open("crd_legacy", sealed); err != nil || plaintext != "test-only-secret" {
		t.Fatal("legacy helper compatibility was lost")
	}
}

func TestConfiguredCredentialStoreUsesOnlyReviewedWriteSlot(t *testing.T) {
	oldKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{17}, 32))
	nextKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{29}, 32))
	legacy, err := credentialStore(oldKey)
	if err != nil {
		t.Fatal(err)
	}
	oldCiphertext, err := legacy.Seal("crd_legacy", "test-only-original-secret")
	if err != nil {
		t.Fatal(err)
	}
	// The separate legacy field explicitly adds its designated decrypt slot.
	cfg := loadBootstrapFixture(t, "encryption_key: '"+oldKey+"'\nencryption_keyring:\n  legacy_key_id: Old\n  write_key_id: Next\n  keys:\n    - {id: Next, key: '"+nextKey+"'}\n")
	store, err := credentialStoreForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if plaintext, err := store.Open("crd_legacy", oldCiphertext); err != nil || plaintext != "test-only-original-secret" {
		t.Fatal("explicit legacy selection did not recover retained v1 data")
	}
	sealed, err := store.Seal("crd_next", "test-only-next-secret")
	if err != nil {
		t.Fatal(err)
	}
	version, id := envelopeSelection(t, sealed)
	if version != 2 || id != "Next" {
		t.Fatal("keyring did not write v2 under its exact selected root")
	}
	if plaintext, err := store.Open("crd_next", sealed); err != nil || plaintext != "test-only-next-secret" {
		t.Fatal("temporary bootstrap key clearing damaged the keyring")
	}
	if _, err := legacy.Open("crd_next", sealed); err == nil {
		t.Fatal("legacy single-key store accepted a versioned envelope")
	}
	cfg.EncryptionKeyring.WriteKeyID = "Old"
	cfg.EncryptionKeyring.Keys[0].Key = "private-invalid-marker"
	another, err := store.Seal("crd_immutable", "test-only-independent-secret")
	if err != nil {
		t.Fatal(err)
	}
	version, id = envelopeSelection(t, another)
	if version != 2 || id != "Next" {
		t.Fatal("mutating bootstrap config changed an existing immutable store")
	}
}

func TestVersionedOnlyBootstrapNeverTrialsLegacyRoots(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{17}, 32))
	cfg := loadBootstrapFixture(t, "encryption_keyring:\n  write_key_id: root\n  keys:\n    - {id: root, key: '"+key+"'}\n")
	store, err := credentialStoreForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := credentialStore(key)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := legacy.Seal("crd_legacy", "test-only-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open("crd_legacy", sealed); err == nil {
		t.Fatal("versioned-only bootstrap silently tried a legacy root")
	}
}

func TestConfiguredCredentialStoreRejectsMalformedSlots(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{17}, 32))
	other := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{29}, 32))
	for _, tc := range []struct {
		name string
		ring config.EncryptionKeyring
	}{
		{"missing write", config.EncryptionKeyring{Keys: []config.EncryptionRootKey{{ID: "private-root-marker", Key: key}}}},
		{"missing selected slot", config.EncryptionKeyring{WriteKeyID: "private-other-marker", Keys: []config.EncryptionRootKey{{ID: "private-root-marker", Key: key}}}},
		{"missing legacy slot", config.EncryptionKeyring{LegacyKeyID: "private-other-marker", WriteKeyID: "private-root-marker", Keys: []config.EncryptionRootKey{{ID: "private-root-marker", Key: key}}}},
		{"duplicate IDs", config.EncryptionKeyring{WriteKeyID: "private-root-marker", Keys: []config.EncryptionRootKey{{ID: "private-root-marker", Key: key}, {ID: "private-root-marker", Key: other}}}},
		{"duplicate material", config.EncryptionKeyring{WriteKeyID: "private-root-marker", Keys: []config.EncryptionRootKey{{ID: "private-root-marker", Key: key}, {ID: "private-other-marker", Key: key}}}},
		{"invalid material", config.EncryptionKeyring{WriteKeyID: "private-root-marker", Keys: []config.EncryptionRootKey{{ID: "private-root-marker", Key: "private-invalid-marker"}}}},
		{"noncanonical material", config.EncryptionKeyring{WriteKeyID: "private-root-marker", Keys: []config.EncryptionRootKey{{ID: "private-root-marker", Key: key + "\n"}}}},
		{"invalid ID", config.EncryptionKeyring{WriteKeyID: "private-root-marker", Keys: []config.EncryptionRootKey{{ID: " private-root-marker", Key: key}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := credentialStoreForConfig(&config.Config{EncryptionKeyring: &tc.ring})
			if err == nil || store != nil {
				t.Fatal("malformed bootstrap keyring was accepted")
			}
			for _, marker := range []string{key, other, "private-root-marker", "private-other-marker", "private-invalid-marker"} {
				if strings.Contains(err.Error(), marker) {
					t.Fatal("bootstrap error exposed root material or slot identity")
				}
			}
		})
	}
}

func TestRunRejectsMalformedKeyringBeforeDatabaseAccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "addr: localhost:9000\ndsn: private-database-marker\nencryption_keyring:\n  write_key_id: private-root-marker\n  keys:\n    - {id: private-root-marker, key: private-secret-marker}\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), path); err == nil || err.Error() != "load configuration failed" {
		t.Fatal("startup did not reject and sanitize malformed keyring before opening the database")
	}
}
