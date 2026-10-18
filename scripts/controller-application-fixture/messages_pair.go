package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"
)

const messagesPairOperation = "serve-messages-pair"
const messagesPairInput = inputRoot + "/messages-pair-serve.json"

type messagesPairServeConfig struct {
	messagesServeConfig
	Operation string `json:"operation"`
}

// Pair mode has a disjoint input and a hard-coded two-call budget. It keeps the
// existing compiled Messages phase: both actual clients still use the same
// closed client-messages operation, source/run/UID and official-artifact binding.
func decodeMessagesPairServe(raw []byte, c *messagesPairServeConfig) bool {
	if c == nil || len(raw) == 0 || len(raw) > 4096 || !validJSONObject(raw) {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 7 {
		return false
	}
	for _, key := range []string{"operation", "source_commit", "run_id", "phase", "listen", "lifetime_seconds", "max_requests"} {
		value, present := fields[key]
		if !present || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
	}
	if decodeExact(raw, c) != nil {
		return false
	}
	return c.Operation == messagesPairOperation && sourcePattern.MatchString(c.SourceCommit) && c.SourceCommit != "0000000000000000000000000000000000000000" && runPattern.MatchString(c.RunID) && c.RunID != "00000000000000000000000000000000" && c.Phase == claudePhaseName && validServeConfig(serveConfig{Listen: c.Listen, LifetimeSeconds: c.LifetimeSeconds, MaxRequests: c.MaxRequests, FinishReasons: []string{"stop"}})
}
func newMessagesPairPeer(secret []byte, maximum int, out io.Writer) *messagesPeer {
	peer := newMessagesPeer(secret, maximum, out)
	peer.pair = true
	return peer
}
func messagesPairComplete(peer *messagesPeer) bool {
	peer.mu.Lock()
	defer peer.mu.Unlock()
	return peer.pair && peer.calls == 2 && peer.completed == 2
}
func runMessagesPairServe(out io.Writer) int {
	// Invalid compile bindings refuse before either private input is read.
	if !validClaudeBinding() {
		emit(out, result{Result: "messages_pair_config_invalid", Native: "unknown"})
		return 1
	}
	raw, err := privateRead(messagesPairInput, 4096, false)
	if err != nil {
		emit(out, result{Result: "messages_pair_config_invalid", Native: "unknown"})
		return 1
	}
	defer clear(raw)
	var config messagesPairServeConfig
	if !decodeMessagesPairServe(raw, &config) || !matchesMessagesIdentity(config.messagesIdentity) {
		emit(out, result{Result: "messages_pair_config_invalid", Native: "unknown"})
		return 1
	}
	secret, err := privateRead(inputRoot+"/upstream.secret", 4096, false)
	if err != nil || !validSecret(secret) {
		emit(out, result{Result: "secret_invalid", Native: "unknown"})
		return 1
	}
	defer clear(secret)
	parent, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	return serveMessagesPair(parent, config, newMessagesPairPeer(secret, config.MaxRequests, out), out)
}
func serveMessagesPair(parent context.Context, config messagesPairServeConfig, peer *messagesPeer, out io.Writer) int {
	ctx, cancel := context.WithTimeout(parent, time.Duration(config.LifetimeSeconds)*time.Second)
	defer cancel()
	listener, err := net.Listen("tcp4", config.Listen)
	if err != nil {
		emit(out, result{Result: "listen_failed", Native: "unknown"})
		return 1
	}
	server := &http.Server{Handler: peer, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 4 * time.Second, WriteTimeout: 4 * time.Second, IdleTimeout: 2 * time.Second, MaxHeaderBytes: 4096, ErrorLog: log.New(io.Discard, "", 0)}
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		select {
		case <-ctx.Done():
		case <-peer.exhausted:
		}
		cancel()
		shutdown, done := context.WithTimeout(context.Background(), 2*time.Second)
		defer done()
		if server.Shutdown(shutdown) != nil {
			_ = server.Close()
		}
	}()
	emit(out, result{Result: "ready", Native: "unknown", Kind: "messages_pair"})
	err = server.Serve(listener)
	cancel()
	<-closed
	if err != nil && err != http.ErrServerClosed {
		emit(out, result{Result: "serve_failed", Native: "unknown"})
		return 1
	}
	select {
	case <-peer.exhausted:
		emit(out, result{Result: "request_bound", Native: "unknown"})
		return 1
	default:
	}
	// A normal owned shutdown is not pair success unless both terminal frames
	// were written/flushed. SQL/native/client proof remains independently required.
	if parent.Err() != nil {
		if !messagesPairComplete(peer) {
			emit(out, result{Result: "messages_pair_incomplete", Native: "unknown"})
			return 1
		}
		emit(out, result{Result: "shutdown", Native: "unknown", Kind: "messages_pair"})
		return 0
	}
	emit(out, result{Result: "deadline", Native: "unknown"})
	return 124
}
