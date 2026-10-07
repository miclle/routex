package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"
)

// LoginToken owns only transient login material. Close clears its owned bytes;
// callers must discard strings copied by Token and must not copy LoginToken.
// It is never cached or renewed.
type LoginToken struct {
	mu        sync.Mutex
	token     []byte
	expiresAt time.Time
}

func (t *LoginToken) String() string   { return "vault login token (redacted)" }
func (t *LoginToken) GoString() string { return t.String() }

// MarshalJSON never serializes authentication material.
func (t *LoginToken) MarshalJSON() ([]byte, error) { return []byte("null"), nil }

// Token returns a transient copy only while this lease is locally valid.
func (t *LoginToken) Token() (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.token) == 0 || !time.Now().Before(t.expiresAt) {
		clear(t.token)
		t.token = nil
		return "", &Failure{Stage: "prepare", Code: "auth_expired"}
	}
	return string(t.token), nil
}

// Close invalidates the local lease and clears its owned bytes, without HTTP.
func (t *LoginToken) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	clear(t.token)
	t.token = nil
}

// LoginAppRole makes exactly one guarded POST, at most ten seconds, without a
// Vault Token header or retries. authMount is independent from the KV mount.
// It does not perform any KV request; callers own durable claims and total
// operation deadlines. The conservative local lease starts before dispatch.
func (c *Client) LoginAppRole(ctx context.Context, authMount, roleID, secretID string) (tokenResult *LoginToken, obs Observation, resultErr error) {
	start := time.Now()
	fail := func(code string, status int) (*LoginToken, Observation, error) {
		obs.Duration = time.Since(start)
		obs.Failure = &Failure{Stage: "prepare", Code: code, HTTPStatus: status}
		return nil, obs, obs.Failure
	}
	if !validPath(authMount, 128, false) || !validToken(roleID) || !validToken(secretID) {
		return fail("invalid_auth", 0)
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	contextFail := func() (*LoginToken, Observation, error) {
		obs.Duration = time.Since(start)
		obs.Failure = contextFailure("prepare", ctx.Err())
		return nil, obs, obs.Failure
	}
	if ctx.Err() != nil {
		return contextFail()
	}
	body, _ := json.Marshal(map[string]string{"role_id": roleID, "secret_id": secretID})
	defer clear(body)
	endpoint := *c.endpoint
	endpoint.Path += "/v1/auth/" + authMount + "/login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fail("invalid_request", 0)
	}
	req.Close = true
	req.GetBody = nil
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Vault-Request", "true")
	if c.descriptor.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", c.descriptor.Namespace)
	}
	obs.Attempted = true
	response, err := c.http.Do(req)
	if err != nil {
		c.unprovenResponseClose()
		if ctx.Err() != nil {
			return contextFail()
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
	defer clear(raw)
	if err != nil {
		if ctx.Err() != nil {
			return contextFail()
		}
		return fail("transport", response.StatusCode)
	}
	if len(raw) > maxResponseBytes {
		return fail("response_too_large", response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		return fail("http_status", response.StatusCode)
	}
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || !validJSON(raw) || !noResponseErrors(raw) {
		return fail("invalid_response", response.StatusCode)
	}
	root, _ := object(raw)
	auth, ok := object(root["auth"])
	if !ok {
		return fail("invalid_response", response.StatusCode)
	}
	var token string
	var lease *int64
	if json.Unmarshal(auth["client_token"], &token) != nil || !validToken(token) || json.Unmarshal(auth["lease_duration"], &lease) != nil || lease == nil || *lease <= 0 || *lease > int64((time.Duration(1<<63-1))/time.Second) {
		return fail("invalid_response", response.StatusCode)
	}
	expiresAt := start.Add(time.Duration(*lease) * time.Second)
	if ctx.Err() != nil {
		return contextFail()
	}
	if !time.Now().Before(expiresAt) {
		return fail("invalid_response", response.StatusCode)
	}
	result := &LoginToken{token: []byte(token), expiresAt: expiresAt}
	obs.Duration = time.Since(start)
	obs.Succeeded = true
	return result, obs, nil
}
