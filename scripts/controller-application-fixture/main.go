package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"syscall"
	"time"
)

const (
	inputRoot     = "/fixture-native-input"
	gatewayURL    = "http://127.0.0.1:19001/v1/chat/completions"
	installedRoot = "/run/secrets/routex/api-keys"
	maxBody       = 16384
	maxResponse   = 16384
)

var slotPattern = regexp.MustCompile(`^kds_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
var requestPattern = regexp.MustCompile(`^req_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
var errInvalid = errors.New("invalid fixture input")

type serveConfig struct {
	Listen          string   `json:"listen"`
	LifetimeSeconds int      `json:"lifetime_seconds"`
	MaxRequests     int      `json:"max_requests"`
	FinishReasons   []string `json:"finish_reasons"`
}
type invokeConfig struct {
	SlotID string `json:"slot_id"`
}
type result struct {
	Result    string `json:"result"`
	Status    int    `json:"status,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Native    string `json:"native"`
	Kind      string `json:"kind,omitempty"`
	Ordinal   int    `json:"ordinal,omitempty"`
}

func main() {
	code := run(os.Args[1:], os.Stdout)
	os.Exit(code)
}

func run(args []string, out io.Writer) int {
	// Neither flag parsing nor raw OS/network/JSON errors are allowed to print.
	if len(args) != 1 {
		emit(out, result{Result: "arguments_invalid", Native: "unknown"})
		return 1
	}
	switch args[0] {
	case "serve":
		var config serveConfig
		if readConfig(inputRoot+"/serve.json", &config) != nil || !validServeConfig(config) {
			emit(out, result{Result: "config_invalid", Native: "unknown"})
			return 1
		}
		secret, err := privateRead(inputRoot+"/upstream.secret", 4096, false)
		if err != nil || !validSecret(secret) {
			emit(out, result{Result: "secret_invalid", Native: "unknown"})
			return 1
		}
		defer clear(secret)
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer cancel()
		return serve(ctx, config, secret, out)
	case "probe-predecessor":
		key, err := privateRead(inputRoot+"/predecessor.key", 4096, false)
		if err != nil || !validKey(key) {
			emitProbe(out, probeResult{Result: "key_invalid"})
			return 1
		}
		defer clear(key)
		r := probePredecessor(context.Background(), gatewayURL, key, newClient())
		emitProbe(out, r)
		if r.Result != "revoked_rejected" {
			return 1
		}
		return 0
	case "invoke":
		var config invokeConfig
		if readConfig(inputRoot+"/invoke.json", &config) != nil || !slotPattern.MatchString(config.SlotID) {
			emit(out, result{Result: "config_invalid", Native: "unknown"})
			return 1
		}
		if verifyRunAlias() != nil {
			emit(out, result{Result: "key_invalid", Native: "unknown"})
			return 1
		}
		key, err := privateRead(installedRoot+"/"+config.SlotID, 4096, true)
		if err != nil || !validKey(key) {
			emit(out, result{Result: "key_invalid", Native: "unknown"})
			return 1
		}
		defer clear(key)
		r := invoke(context.Background(), gatewayURL, key, newClient())
		emit(out, r)
		if r.Result != "response" {
			return 1
		}
		return 0
	default:
		emit(out, result{Result: "arguments_invalid", Native: "unknown"})
		return 1
	}
}

func emit(out io.Writer, r result) { _ = json.NewEncoder(out).Encode(r) }
func readConfig(path string, target any) error {
	raw, err := privateRead(path, 4096, false)
	if err != nil {
		return errInvalid
	}
	defer clear(raw)
	return decodeExact(raw, target)
}
func decodeExact(raw []byte, target any) error {
	if !validJSONObject(raw) {
		return errInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil {
		return errInvalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errInvalid
	}
	return nil
}
func validSecret(s []byte) bool {
	if len(s) < 24 || len(s) > 4096 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func validKey(s []byte) bool { return len(s) == 46 && string(s[:3]) == "rx_" && validSecret(s) }
func validServeConfig(c serveConfig) bool {
	host, port, err := net.SplitHostPort(c.Listen)
	n, e := strconv.Atoi(port)
	if err != nil || e != nil || host != "127.0.0.1" || n < 1024 || n > 65535 || c.Listen != "127.0.0.1:"+strconv.Itoa(n) {
		return false
	}
	if c.LifetimeSeconds < 1 || c.LifetimeSeconds > 900 || c.MaxRequests < 1 || c.MaxRequests > 64 || len(c.FinishReasons) < 1 || len(c.FinishReasons) > 4 {
		return false
	}
	for _, f := range c.FinishReasons {
		if f != "length" && f != "stop" {
			return false
		}
	}
	return true
}

func serve(parent context.Context, c serveConfig, secret []byte, out io.Writer) int {
	ctx, cancel := context.WithTimeout(parent, time.Duration(c.LifetimeSeconds)*time.Second)
	defer cancel()
	listener, err := net.Listen("tcp4", c.Listen)
	if err != nil {
		emit(out, result{Result: "listen_failed", Native: "unknown"})
		return 1
	}
	peer := newPeer(secret, c.FinishReasons, c.MaxRequests, out)
	server := &http.Server{Handler: peer, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 4 * time.Second, WriteTimeout: 4 * time.Second, IdleTimeout: 2 * time.Second, MaxHeaderBytes: 4096, ErrorLog: log.New(io.Discard, "", 0)}
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		select {
		case <-ctx.Done():
		case <-peer.exhausted:
			cancel()
		}
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if server.Shutdown(shutdown) != nil {
			_ = server.Close()
		}
	}()
	emit(out, result{Result: "ready", Native: "unknown"})
	err = server.Serve(listener)
	cancel()
	<-closed
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		emit(out, result{Result: "serve_failed", Native: "unknown"})
		return 1
	}
	select {
	case <-peer.exhausted:
		emit(out, result{Result: "request_bound", Native: "unknown"})
		return 1
	default:
	}
	if parent.Err() != nil {
		emit(out, result{Result: "shutdown", Native: "unknown"})
		return 0
	}
	emit(out, result{Result: "deadline", Native: "unknown"})
	return 124
}
