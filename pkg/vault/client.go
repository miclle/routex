// Package vault probes a KV-v2 integration without changing active credential storage.
package vault

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miclle/routex/pkg/upstream"
)

const (
	maxResponseBytes = 64 << 10
	maxTokenBytes    = 4096
	requestTimeout   = 10 * time.Second
	probeTimeout     = 30 * time.Second
)

// Descriptor is non-secret configuration. Paths are canonical, relative ASCII
// segments; the field is fixed by configuration, never supplied with a probe.
type Descriptor struct{ Endpoint, Namespace, Mount, Prefix, DataField string }

// Client retains configuration and guarded transport, but no authentication tokens.
type Client struct {
	descriptor Descriptor
	endpoint   *url.URL
	http       *http.Client
	responses  *responseCloseTracker
}

// ResponseCloseState is private application-facing drain evidence, not a remote
// effect receipt. Failed is sticky and includes unobservable closure after a
// transport error; no response is not a successful Close. A
// Client must belong to one exact operation/source for callers to attribute it.
// Pending covers returned response bodies, not requests still awaiting headers;
// callers must also join their exact operation holders. Copies share lifetime
// evidence and cannot establish independent operation/source proof.
type ResponseCloseState struct {
	Observed bool
	Pending  uint64
	Failed   bool
}

type responseCloseTracker struct {
	mu    sync.Mutex
	state ResponseCloseState
}

// ResponseCloseState returns one coherent snapshot without error/secret material.
// Client.Close releases idle connections and does not reset or certify this state.
func (c *Client) ResponseCloseState() ResponseCloseState {
	c.responses.mu.Lock()
	defer c.responses.mu.Unlock()
	return c.responses.state
}

func (c *Client) trackResponseClose(body io.ReadCloser) func() bool {
	c.responses.mu.Lock()
	c.responses.state.Observed = true
	c.responses.state.Pending++
	c.responses.mu.Unlock()
	return func() bool {
		err := body.Close() // no evidence lock over the actual synchronous Close
		c.responses.mu.Lock()
		if err != nil {
			c.responses.state.Failed = true
		}
		c.responses.state.Pending-- // failure is recorded before publishing zero ownership
		c.responses.mu.Unlock()
		return err != nil
	}
}

func (c *Client) unprovenResponseClose() {
	// Do can close redirect bodies internally or discard a RoundTripper response
	// returned with an error. Neither result is observable here. Never close an
	// already closed body twice or certify drain from a nil response on this path.
	c.responses.mu.Lock()
	c.responses.state.Failed = true
	c.responses.mu.Unlock()
}

// Failure contains only bounded diagnostic labels, never the upstream error/body/URL.
// HTTPStatus is evidence of a response, not evidence that a write did not happen.
type Failure struct {
	Stage      string
	Code       string
	HTTPStatus int
}

func (f *Failure) Error() string { return "vault " + f.Stage + ": " + f.Code }

// Observation reports one stage; an HTTP success alone does not set Succeeded.
type Observation struct {
	Attempted, Succeeded bool
	Duration             time.Duration
	Failure              *Failure
	// Exact response closure, never serialized as a remote observation.
	responseCloseFailed bool
}

// Cleanup describes acknowledgement separately from successful read verification.
// Unknown includes cancellation or a possibly committed write without ownership proof.
// Acknowledged is a Vault exact-version destruction response, not physical-erasure
// evidence. Metadata and any later versions remain untouched.
type Cleanup struct {
	State       string
	Observation Observation
}

// Result contains no token, secret value, raw endpoint or user-provided path.
// ProbeID lets a future coordinator identify a possible orphan without secret data.
// Results do not prove saved configuration, runtime publication, distinct remote
// principals, or identity privileges. Distinct token strings prove only local separation.
type Result struct {
	ProbeID     string
	Version     int64
	Write, Read Observation
	Cleanup     Cleanup
}

// New preserves upstream's DNS/address/TLS/redirect policy. Private destinations
// require an explicit deployment opt-in; this never disables TLS verification.
func New(d Descriptor, allowPrivate bool) (*Client, error) {
	endpoint, err := upstream.ValidateBaseURL(d.Endpoint, allowPrivate)
	if err != nil || len(d.Endpoint) > 2048 || endpoint == nil || endpoint.RawPath != "" || !validPath(strings.Trim(endpoint.Path, "/"), 256, true) || strings.Contains(endpoint.Path, "//") || !validPath(d.Namespace, 256, true) || !validPath(d.Mount, 128, false) || !validPath(d.Prefix, 256, false) || !validSegment(d.DataField) || len(d.DataField) > 64 {
		return nil, &Failure{Stage: "prepare", Code: "invalid_descriptor"}
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/")
	client := upstream.NewNonReplayingClient(allowPrivate)
	client.Timeout = requestTimeout
	return &Client{descriptor: d, endpoint: endpoint, http: client, responses: &responseCloseTracker{}}, nil
}

// Close releases only this client's idle connections.
func (c *Client) Close() { c.http.CloseIdleConnections() }

// Probe uses separate transient Tokens, one random system-owned CAS=0 path and
// value, and the returned version for the reader. It makes at most three HTTP
// requests, with no retries. Caller cancellation also stops cleanup: an unknown
// outcome is returned for a future coordinator, never hidden detached work.
func (c *Client) Probe(ctx context.Context, writerToken, readerToken string) (Result, error) {
	result := Result{Cleanup: Cleanup{State: "not_attempted"}}
	if !validToken(writerToken) || !validToken(readerToken) || writerToken == readerToken {
		return result, &Failure{Stage: "prepare", Code: "invalid_tokens"}
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return result, contextFailure("prepare", ctx.Err())
	}
	entropy := make([]byte, 48)
	if _, err := rand.Read(entropy); err != nil {
		return result, &Failure{Stage: "prepare", Code: "random_unavailable"}
	}
	defer clear(entropy)
	result.ProbeID = hex.EncodeToString(entropy[:16])
	value := hex.EncodeToString(entropy[16:])
	path := c.descriptor.Prefix + "/routex-probe-" + result.ProbeID
	body, _ := json.Marshal(map[string]any{"options": map[string]int{"cas": 0}, "data": map[string]string{c.descriptor.DataField: value}})
	defer clear(body)
	raw, obs := c.request(ctx, "write", http.MethodPost, "data", path, "", writerToken, body)
	result.Write = obs
	if obs.Failure != nil {
		if obs.Attempted {
			result.Cleanup.State = "unknown"
		}
		return result, obs.Failure
	}
	version, err := writeVersion(raw)
	clear(raw)
	if err != nil {
		result.Write.Failure = &Failure{Stage: "write", Code: "invalid_response"}
		result.Cleanup.State = "unknown"
		return result, result.Write.Failure
	}
	result.Version = version
	result.Write.Succeeded = true
	raw, obs = c.request(ctx, "read", http.MethodGet, "data", path, "version="+strconv.FormatInt(version, 10), readerToken, nil)
	result.Read = obs
	if obs.Failure == nil {
		if !matchesRead(raw, c.descriptor.DataField, value, version) {
			result.Read.Failure = &Failure{Stage: "read", Code: "verification_failed"}
		} else {
			result.Read.Succeeded = true
		}
	}
	clear(raw)
	if !result.Read.Succeeded {
		// Initial CAS ownership cannot override a failed current ownership read.
		result.Cleanup.State = "unknown"
		return result, result.Read.Failure
	}
	// Never delete a target after ambiguous CAS ownership. Only the confirmed fresh
	// version reaches destruction, using the writer rather than reader. Metadata
	// deletion would erase unproven concurrent versions and is never requested.
	raw, obs = c.request(ctx, "cleanup", http.MethodPut, "destroy", path, "", writerToken, []byte(`{"versions":[1]}`))
	clear(raw)
	result.Cleanup.Observation = obs
	if obs.Failure == nil {
		result.Cleanup.State = "acknowledged"
		result.Cleanup.Observation.Succeeded = true
	} else {
		result.Cleanup.State = "unknown"
		if obs.Failure.Code == "http_status" && obs.Failure.HTTPStatus >= 400 && obs.Failure.HTTPStatus < 500 {
			result.Cleanup.State = "failed"
		}
	}
	return result, errors.Join(errorOrNil(result.Read.Failure), errorOrNil(obs.Failure))
}

func errorOrNil(f *Failure) error {
	if f == nil {
		return nil
	}
	return f
}
func contextFailure(stage string, err error) *Failure {
	code := "canceled"
	if errors.Is(err, context.DeadlineExceeded) {
		code = "timed_out"
	}
	return &Failure{Stage: stage, Code: code}
}
func (c *Client) request(ctx context.Context, stage, method, kind, path, query, token string, body []byte) (rawResult []byte, obs Observation) {
	start := time.Now()
	fail := func(code string, status int) ([]byte, Observation) {
		obs.Duration = time.Since(start)
		obs.Failure = &Failure{Stage: stage, Code: code, HTTPStatus: status}
		return nil, obs
	}
	if ctx.Err() != nil {
		obs.Failure = contextFailure(stage, ctx.Err())
		obs.Duration = time.Since(start)
		return nil, obs
	}
	endpoint := *c.endpoint
	endpoint.Path += "/v1/" + c.descriptor.Mount + "/" + kind + "/" + path
	endpoint.RawQuery = query
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fail("invalid_request", 0)
	}
	// The guarded HTTP/1-only transport disables reuse; GetBody is absent
	// and this request also explicitly closes its dedicated connection.
	req.Close = true
	req.GetBody = nil
	req.Header.Set("X-Vault-Token", token)
	req.Header.Set("X-Vault-Request", "true")
	if c.descriptor.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", c.descriptor.Namespace)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	obs.Attempted = true
	response, err := c.http.Do(req)
	if err != nil {
		c.unprovenResponseClose()
		if ctx.Err() != nil {
			obs.Failure = contextFailure(stage, ctx.Err())
			obs.Duration = time.Since(start)
			return nil, obs
		}
		var timeout interface{ Timeout() bool }
		if errors.As(err, &timeout) && timeout.Timeout() {
			return fail("timed_out", 0)
		}
		return fail("transport", 0)
	}
	closeResponse := c.trackResponseClose(response.Body)
	defer func() { obs.responseCloseFailed = closeResponse() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		clear(raw)
		if ctx.Err() != nil {
			obs.Failure = contextFailure(stage, ctx.Err())
			obs.Duration = time.Since(start)
			return nil, obs
		}
		return fail("transport", response.StatusCode)
	}
	if len(raw) > maxResponseBytes {
		clear(raw)
		return fail("response_too_large", response.StatusCode)
	}
	if response.StatusCode != http.StatusOK && (stage != "cleanup" || response.StatusCode != http.StatusNoContent) {
		clear(raw)
		return fail("http_status", response.StatusCode)
	}
	if response.StatusCode == http.StatusOK {
		media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
		if err != nil || media != "application/json" || !validJSON(raw) || !noResponseErrors(raw) || stage == "cleanup" && !cleanupAcknowledged(raw) {
			clear(raw)
			return fail("invalid_response", response.StatusCode)
		}
	}
	obs.Duration = time.Since(start)
	return raw, obs
}

func validToken(token string) bool {
	if len(token) == 0 || len(token) > maxTokenBytes {
		return false
	}
	for _, ch := range []byte(token) {
		if ch < 33 || ch > 126 {
			return false
		}
	}
	return true
}
func validSegment(segment string) bool {
	if segment == "" || len(segment) > 128 || segment == "." || segment == ".." || strings.HasSuffix(segment, ".") {
		return false
	}
	for _, ch := range []byte(segment) {
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '_' && ch != '-' && ch != '.' {
			return false
		}
	}
	return true
}
func validPath(path string, max int, empty bool) bool {
	if path == "" {
		return empty
	}
	if len(path) > max {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if !validSegment(segment) {
			return false
		}
	}
	return true
}
