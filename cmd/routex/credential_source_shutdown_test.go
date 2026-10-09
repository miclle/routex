package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Pure Listener: no socket or request is created by this ordering regression.
type credentialShutdownListener struct {
	entered    chan struct{}
	closed     chan struct{}
	once       sync.Once
	acceptOnce sync.Once
	admission  *atomic.Bool
	violation  atomic.Bool
}

func (l *credentialShutdownListener) Accept() (net.Conn, error) {
	l.acceptOnce.Do(func() { close(l.entered) })
	<-l.closed
	return nil, net.ErrClosed
}
func (l *credentialShutdownListener) Close() error {
	if !l.admission.Load() {
		l.violation.Store(true)
	}
	l.once.Do(func() { close(l.closed) })
	return nil
}
func (*credentialShutdownListener) Addr() net.Addr { return credentialShutdownAddress{} }

type credentialShutdownAddress struct{}

func (credentialShutdownAddress) Network() string { return "test" }
func (credentialShutdownAddress) String() string  { return "pure-listener" }
func TestCredentialSourceShutdownBarrierBeforeListenerClose(t *testing.T) {
	var admission atomic.Bool
	listener := &credentialShutdownListener{entered: make(chan struct{}), closed: make(chan struct{}), admission: &admission}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- serveHTTP(ctx, listener, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("pure listener served a request") }), time.Second, func() { admission.Store(true) })
	}()
	<-listener.entered
	cancel()
	if err := <-done; err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatal(err)
	}
	if !admission.Load() || listener.violation.Load() {
		t.Fatal("listener shutdown preceded secret-source admission barrier")
	}
}
