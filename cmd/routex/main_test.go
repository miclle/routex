package main

import (
	"bytes"
	"context"
	"encoding/base64"
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
