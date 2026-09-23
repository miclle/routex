package objectstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testConfig(endpoint string) Config {
	return Config{Endpoint: endpoint, Region: "us-east-1", Bucket: "test-bucket", Prefix: "private", AccessKey: "test-only-access", SecretKey: "test-only-secret"}
}

func TestVersionedObjectLifecycle(t *testing.T) {
	var mu sync.Mutex
	var data []byte
	var owner string
	var deleted bool
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		mu.Lock()
		defer mu.Unlock()
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=test-only-access/") || r.URL.Path != "/test-bucket/private/routex/obj_test" {
			t.Error("request not scoped and signed")
			w.WriteHeader(403)
			return
		}
		if r.Method == "PUT" {
			if r.Header.Get("If-None-Match") != "*" {
				t.Error("missing conditional write")
			}
			if data != nil {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(412)
				_, _ = io.WriteString(w, "<Error><Code>PreconditionFailed</Code></Error>")
				return
			}
			data, _ = io.ReadAll(r.Body)
			owner = r.Header.Get("X-Amz-Meta-Routex-Id")
			w.Header().Set("X-Amz-Version-Id", "version-1")
			return
		}
		if data == nil {
			w.WriteHeader(404)
			return
		}
		if q := r.URL.Query().Get("versionId"); q != "" && q != "version-1" {
			t.Error("wrong version")
		}
		w.Header().Set("X-Amz-Version-Id", "version-1")
		w.Header().Set("X-Amz-Meta-Routex-Id", owner)
		switch r.Method {
		case "GET":
			_, _ = w.Write(data)
		case "HEAD":
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		case "DELETE":
			if r.URL.Query().Get("versionId") != "version-1" {
				t.Error("delete created a marker instead of removing the version")
			}
			data = nil
			deleted = true
			w.WriteHeader(204)
		default:
			t.Error("unexpected operation")
		}
	}))
	defer server.Close()
	client, err := New(testConfig(server.URL), true)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	version, err := client.Put(ctx, "obj_test", "application/pdf", []byte("test body"))
	if err != nil || version != "version-1" {
		t.Fatalf("put: %v", err)
	}
	if _, err := client.Put(ctx, "obj_test", "application/pdf", []byte("new body")); !errors.Is(err, ErrConflict) {
		t.Fatalf("conditional collision: %v", err)
	}
	got, err := client.Get(ctx, "obj_test", version)
	if err != nil || string(got.Data) != "test body" || got.OwnerID != "obj_test" {
		t.Fatalf("get: %v", err)
	}
	mu.Lock()
	owner = "another_object"
	mu.Unlock()
	if err := client.Delete(ctx, "obj_test", version); !errors.Is(err, ErrConflict) {
		t.Fatal("foreign ownership deleted")
	}
	mu.Lock()
	owner = "obj_test"
	mu.Unlock()
	if err := client.Delete(ctx, "obj_test", ""); err != nil {
		t.Fatal(err)
	}
	if err := client.Delete(ctx, "obj_test", version); err != nil {
		t.Fatalf("repeat delete: %v", err)
	}
	if !deleted || requests.Load() != 7 {
		t.Fatalf("unexpected operation count: %d", requests.Load())
	}
}

func TestObjectNetworkPolicyBoundsAndCancellation(t *testing.T) {
	var received atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		if r.URL.Path == "/test-bucket/private/routex/obj_large" {
			_, _ = w.Write(bytes.Repeat([]byte{'x'}, MaxBytes+1))
			return
		}
		http.Redirect(w, r, "http://127.0.0.1:1/leak", http.StatusFound)
	}))
	defer server.Close()
	if _, err := New(testConfig(server.URL), false); err == nil {
		t.Fatal("private storage accepted without separate opt-in")
	}
	client, err := New(testConfig(server.URL), true)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Get(context.Background(), "obj_redirect", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("redirect: %v", err)
	}
	if _, err := client.Get(context.Background(), "obj_large", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatal("oversized object accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Get(ctx, "obj_cancel", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatal("cancellation not sanitized")
	}
	if received.Load() != 2 {
		t.Fatal("canceled request reached storage")
	}
	for _, id := range []string{"../escape", "obj/x", "OBJ_SECRET", "x"} {
		if _, err := client.Get(context.Background(), id, ""); !errors.Is(err, ErrConfig) {
			t.Fatal("unsafe key accepted")
		}
	}
	if _, err := client.Put(context.Background(), "obj_big", "application/pdf", make([]byte, MaxBytes+1)); !errors.Is(err, ErrConfig) {
		t.Fatal("oversized upload accepted")
	}
	if _, err := client.Head(context.Background(), "obj_ok", "bad\r\nversion"); !errors.Is(err, ErrConfig) {
		t.Fatal("invalid version accepted")
	}
}

func TestObjectDeadlineAndSafeErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	client, err := New(testConfig(server.URL), true)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = client.Get(ctx, "obj_wait", "")
	if !errors.Is(err, ErrUnavailable) || time.Since(start) > time.Second {
		t.Fatal("deadline not honored")
	}
	if strings.Contains(fmt.Sprint(testConfig(server.URL)), "test-only-secret") {
		t.Fatal("configuration printed credentials")
	}
	for _, endpoint := range []string{"https://user:pass@example.com", "https://example.com/path", "https://example.com?secret=x", "http://example.com", "https://example.com/#fragment"} {
		if Validate(testConfig(endpoint), true) == nil {
			t.Fatalf("unsafe endpoint accepted: %s", endpoint)
		}
	}
}

func TestAttachmentValidation(t *testing.T) {
	var pngData, jpegData bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if err := png.Encode(&pngData, img); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&jpegData, img, nil); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		data []byte
		want string
	}{{"picture.png", pngData.Bytes(), "image/png"}, {"photo.jpg", jpegData.Bytes(), "image/jpeg"}, {"document.pdf", []byte("%PDF-1.7\ncontrolled\n%%EOF"), "application/pdf"}} {
		got, err := ValidateAttachment(tc.name, tc.data)
		if err != nil || got != tc.want {
			t.Fatalf("content rejected: %v", err)
		}
	}
	var wide bytes.Buffer
	if err := png.Encode(&wide, image.NewRGBA(image.Rect(0, 0, 8193, 1))); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateAttachment("wide.png", wide.Bytes()); err == nil {
		t.Fatal("oversized dimensions accepted")
	}
	if _, err := ValidateAttachment("control\x01.png", pngData.Bytes()); err == nil {
		t.Fatal("control filename accepted")
	}
	for _, tc := range []struct {
		name string
		data []byte
	}{{"../file", pngData.Bytes()}, {"bad\nfile", pngData.Bytes()}, {"script.png", []byte("<script>alert(1)</script>")}, {"empty.pdf", nil}, {"bad.pdf", []byte("%PDF-1.7 truncated")}, {"big.pdf", make([]byte, MaxBytes+1)}, {strings.Repeat("a", 201), pngData.Bytes()}, {"gif.gif", []byte("GIF89a")}} {
		if _, err := ValidateAttachment(tc.name, tc.data); err == nil {
			t.Fatal("unsafe content accepted")
		}
	}
}
