package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompiledBindingsRequireExactLinuxNonrootGeneration(t *testing.T) {
	run, source := strings.Repeat("a", 32), strings.Repeat("b", 64)
	if !bindings(run, source, "501", "linux", 501, 501) {
		t.Fatal("valid native generation refused")
	}
	for _, c := range []struct {
		name, run, source, uid, goos string
		actual, effective            int
	}{
		{"root", run, source, "0", "linux", 0, 0},
		{"wrong-effective", run, source, "501", "linux", 501, 502},
		{"wrong-host", run, source, "502", "linux", 501, 501},
		{"noncanonical-uid", run, source, "0501", "linux", 501, 501},
		{"missing-uid", run, source, "", "linux", 501, 501},
		{"other-platform", run, source, "501", "darwin", 501, 501},
		{"missing-run", "", source, "501", "linux", 501, 501},
		{"uppercase-run", strings.ToUpper(run), source, "501", "linux", 501, 501},
		{"unknown-source", run, "unknown", "501", "linux", 501, 501},
	} {
		t.Run(c.name, func(t *testing.T) {
			if bindings(c.run, c.source, c.uid, c.goos, c.actual, c.effective) {
				t.Fatal("foreign native binding accepted")
			}
		})
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOriginalMutationHasNoReplayBodyAndRecordsLiteralStatus(t *testing.T) {
	for _, status := range []int{204, 200, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			w := witness{ctx: context.Background(), client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "PUT" || r.URL.String() != "http://127.0.0.1:8200/v1/kv/destroy/discovery/object" || r.GetBody != nil || !r.Close || r.Header.Get("X-Vault-Token") != "test-only-cleanup-token" {
					t.Fatal("native mutation boundary changed")
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
			})}}
			_, err := w.request("dummy-destroy", "PUT", "/v1/kv/destroy/discovery/object", "test-only-cleanup-token", map[string][]int{"versions": {1}}, 204)
			if calls != 1 || len(w.rows) != 1 || w.rows[0].Status != status || (err == nil) != (status == 204) {
				t.Fatal("literal status or original dispatch changed")
			}
		})
	}
}

func TestUncertainTransportEndsMutationWithoutReplayOrReceipt(t *testing.T) {
	calls := 0
	w := witness{ctx: context.Background(), client: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("private transport detail")
	})}}
	_, err := w.request("dummy-destroy", "PUT", "/v1/kv/destroy/discovery/object", "test-only-cleanup-token", nil, 204)
	if err != errRefused || calls != 1 || len(w.rows) != 0 || strings.Contains(err.Error(), "private") {
		t.Fatal("uncertain mutation was retried, exposed or recorded as success")
	}
}

type refusedClose struct{ io.Reader }

func (refusedClose) Close() error { return errors.New("private close detail") }
func TestBodyCloseAndOverflowRefuseAResponse(t *testing.T) {
	for _, c := range []struct {
		name string
		body io.ReadCloser
	}{
		{"close", refusedClose{strings.NewReader("metrics")}},
		{"overflow", io.NopCloser(strings.NewReader(strings.Repeat("x", (4<<20)+1)))},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := witness{ctx: context.Background(), client: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: c.body, Header: make(http.Header)}, nil
			})}}
			if _, err := w.request("metrics-before", "GET", "/v1/sys/metrics?format=prometheus", "", nil, 200); err != errRefused {
				t.Fatal("unread or unclosed response accepted")
			}
		})
	}
}

func TestSecretScannerRejectsKnownRepresentationsAndMissingMaterial(t *testing.T) {
	secret := strings.Repeat("a", 64)
	decoded, _ := hex.DecodeString(secret)
	for _, raw := range [][]byte{[]byte(secret), []byte(hex.EncodeToString([]byte(secret))), []byte(strings.ToUpper(hex.EncodeToString([]byte(secret)))), []byte(base64.StdEncoding.EncodeToString([]byte(secret))), decoded, []byte(base64.StdEncoding.EncodeToString(decoded))} {
		if secretFree(raw, []string{secret}) {
			t.Fatal("private material accepted in evidence")
		}
	}
	if !secretFree([]byte("# nonsecret observed metric"), []string{secret}) || secretFree(nil, nil) || secretFree(nil, []string{"short"}) {
		t.Fatal("secret scanner premise changed")
	}
}

func TestPrivateWritesNeverOverwriteOriginalOrFollowExistingLink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "original.json")
	if save(dir, "original.json", map[string]string{"result": "original"}) != nil {
		t.Fatal("private original write failed")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if save(dir, "original.json", []byte("replacement")) != errRefused {
		t.Fatal("original evidence overwritten")
	}
	after, _ := os.ReadFile(path)
	info, _ := os.Lstat(path)
	if !bytes.Equal(before, after) || info.Mode().Perm() != 0600 {
		t.Fatal("private original changed")
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	if save(dir, "link.json", []byte("replacement")) != errRefused {
		t.Fatal("existing link followed")
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("symlink target changed")
	}
}

func TestArgumentsRefuseBeforePrivateFilesystemOrNetwork(t *testing.T) {
	var out bytes.Buffer
	if run([]string{"--endpoint=http://untrusted"}, &out) != 1 || out.Len() != 0 {
		t.Fatal("caller-selected endpoint accepted")
	}
}

func TestReaderModesDoNotAcceptEndpointSecretOrAmbiguousOverrides(t *testing.T) {
	if mode, ok := readerMode(nil); !ok || mode != "token" {
		t.Fatal("default mode changed")
	}
	if mode, ok := readerMode([]string{"--app-role-reader"}); !ok || mode != "app_role" {
		t.Fatal("exact AppRole mode refused")
	}
	for _, args := range [][]string{{"--endpoint=http://foreign"}, {"--app-role-reader", "--app-role-reader"}, {"--app-role-reader", "secret"}, {"app_role"}, {"--token"}} {
		if _, ok := readerMode(args); ok {
			t.Fatal("ambiguous native mode accepted")
		}
	}
}

func TestNativeManifestPreservesPartialFilesAndRejectsUnknownOrUnprotectedInputs(t *testing.T) {
	dir := t.TempDir()
	if save(dir, "http-census.json", []byte("[]")) != nil {
		t.Fatal("private census write failed")
	}
	files, err := privateManifest(dir)
	if err != nil || len(files) != 1 || files[0].Name != "http-census.json" || files[0].Bytes != 2 || files[0].SHA256 != digest([]byte("[]")) {
		t.Fatal("partial original artifact changed")
	}
	unknown := filepath.Join(dir, "unknown.json")
	if os.WriteFile(unknown, []byte("unknown"), 0600) != nil {
		t.Fatal("test input failed")
	}
	if _, err := privateManifest(dir); err != errRefused {
		t.Fatal("unknown artifact accepted")
	}
	if os.Remove(unknown) != nil {
		t.Fatal("test cleanup failed")
	}
	path := filepath.Join(dir, "http-census.json")
	if os.Chmod(path, 0644) != nil {
		t.Fatal("test mode failed")
	}
	if _, err := privateManifest(dir); err != errRefused {
		t.Fatal("public artifact accepted")
	}
}
