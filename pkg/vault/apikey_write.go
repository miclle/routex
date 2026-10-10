package vault

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

const maxAPIKeyWriteBytes = 4096
const maxAPIKeyWriteBodyBytes = 32 << 10

// APIKeyWriter exposes only one-shot KV-v2 creation. The supplied Token's remote
// ACL, resource admission and durable command claim remain caller responsibilities.
// Private composition deliberately does not expose Client's Reader or cleanup.
type APIKeyWriter struct{ client *Client }

// APIKeyWritePlan is nonsecret bookkeeping, not authority or a restorable handle.
// ResourceID must be the exact pre-generated Key ID admitted by the service.
type APIKeyWritePlan struct {
	ResourceID       string
	DescriptorSHA256 string
}

type APIKeyWriteOutcome string

const (
	APIKeyWriteNotAttempted APIKeyWriteOutcome = "not_attempted"
	APIKeyWriteAcknowledged APIKeyWriteOutcome = "acknowledged"
	APIKeyWriteUnknown      APIKeyWriteOutcome = "unknown"
)

// Version is 1 only for an acknowledged response, not a retained ownership read.
// Consumed records local claim; Write.Attempted records entry into HTTP Do, not
// proven network dispatch or commit. Neither field establishes durable recovery.
type APIKeyWriteResult struct {
	Plan     APIKeyWritePlan
	Consumed bool
	Outcome  APIKeyWriteOutcome
	Version  int64
	Write    Observation
}

// Copies share one claim. No Token, bearer, Provider marker or response is held.
type PreparedAPIKeyWrite struct{ state *apiKeyWriteState }

type apiKeyWriteState struct {
	mu               sync.Mutex
	plan             APIKeyWritePlan
	consumed, closed bool
}

func (PreparedAPIKeyWrite) String() string               { return "<vault.PreparedAPIKeyWrite>" }
func (PreparedAPIKeyWrite) GoString() string             { return "<vault.PreparedAPIKeyWrite>" }
func (PreparedAPIKeyWrite) MarshalJSON() ([]byte, error) { return []byte(`{}`), nil }

func (p *PreparedAPIKeyWrite) Plan() APIKeyWritePlan {
	if p == nil || p.state == nil {
		return APIKeyWritePlan{}
	}
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	return p.state.plan
}

// Close prevents future claim; it does not cancel or join an already claimed call.
func (p *PreparedAPIKeyWrite) Close() {
	if p == nil || p.state == nil {
		return
	}
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	p.state.closed = true
}

func NewAPIKeyWriter(d Descriptor, allowPrivate bool) (*APIKeyWriter, error) {
	if d.DataField == credentialMarkerField {
		return nil, &Failure{Stage: "prepare", Code: "invalid_descriptor"}
	}
	c, err := New(d, allowPrivate)
	if err != nil {
		return nil, err
	}
	return &APIKeyWriter{client: c}, nil
}

// Close releases only idle connections. Callers must join their in-flight calls.
func (w *APIKeyWriter) Close() {
	if w != nil && w.client != nil {
		w.client.Close()
	}
}

func (w *APIKeyWriter) ResponseCloseState() ResponseCloseState {
	if w == nil || w.client == nil {
		return ResponseCloseState{}
	}
	return w.client.ResponseCloseState()
}

func (w *APIKeyWriter) Prepare(resourceID string) (*PreparedAPIKeyWrite, error) {
	if w == nil || w.client == nil || !validSegment(resourceID) {
		return nil, &Failure{Stage: "prepare", Code: "invalid_plan"}
	}
	plan := APIKeyWritePlan{ResourceID: resourceID, DescriptorSHA256: w.planDigest(resourceID)}
	return &PreparedAPIKeyWrite{state: &apiKeyWriteState{plan: plan}}, nil
}

func (w *APIKeyWriter) planDigest(resourceID string) string {
	c := w.client
	raw, _ := json.Marshal([]string{"routex-vault-api-key-write-plan-v1", c.endpoint.String(), c.descriptor.Namespace, c.descriptor.Mount, c.descriptor.Prefix, c.descriptor.DataField, resourceID})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// Write consumes one prepared CAS0 attempt. Unknown outcomes never trigger reads,
// retries, alternate paths, cleanup or a plaintext fallback. The same absolute
// deadline governs preparation, I/O and final acknowledgement after body Close.
func (w *APIKeyWriter) Write(parent context.Context, writerToken string, p *PreparedAPIKeyWrite, value []byte) (APIKeyWriteResult, error) {
	started := time.Now()
	result := APIKeyWriteResult{Outcome: APIKeyWriteNotAttempted}
	reject := func(code string) (APIKeyWriteResult, error) {
		status := 0
		if result.Write.Failure != nil {
			status = result.Write.Failure.HTTPStatus
		} else if result.Write.Attempted {
			status = http.StatusOK
		}
		f := &Failure{Stage: "write", Code: code, HTTPStatus: status}
		result.Write.Duration = time.Since(started)
		result.Write.Failure = f
		return result, f
	}
	if parent == nil {
		return reject("invalid_request")
	}
	ctx, cancel := context.WithTimeout(parent, requestTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return reject(contextFailure("write", err).Code)
	}
	if w == nil || w.client == nil {
		return reject("invalid_descriptor")
	}
	if !validToken(writerToken) {
		return reject("invalid_tokens")
	}
	if len(value) == 0 || len(value) > maxAPIKeyWriteBytes {
		return reject("invalid_value")
	}
	for _, ch := range value {
		if ch < 33 || ch > 126 {
			return reject("invalid_value")
		}
	}
	if p == nil || p.state == nil {
		return reject("invalid_plan")
	}
	plan := p.Plan()
	if !validSegment(plan.ResourceID) || !lowerHex(plan.DescriptorSHA256, 64) || plan.DescriptorSHA256 != w.planDigest(plan.ResourceID) {
		return reject("invalid_plan")
	}
	result.Plan = plan
	owned := append([]byte(nil), value...)
	defer clear(owned)
	body, err := json.Marshal(struct {
		Options struct {
			CAS int `json:"cas"`
		} `json:"options"`
		Data map[string]string `json:"data"`
	}{Data: map[string]string{w.client.descriptor.DataField: string(owned)}})
	if err != nil {
		return reject("invalid_request")
	}
	defer clear(body)
	if len(body) > maxAPIKeyWriteBodyBytes {
		return reject("invalid_value")
	}
	p.state.mu.Lock()
	if p.state.consumed {
		p.state.mu.Unlock()
		return reject("already_attempted")
	}
	if p.state.closed {
		p.state.mu.Unlock()
		return reject("prepared_closed")
	}
	if err := ctx.Err(); err != nil {
		p.state.mu.Unlock()
		return reject(contextFailure("write", err).Code)
	}
	p.state.consumed = true
	p.state.mu.Unlock()
	result.Consumed = true
	path := w.client.descriptor.Prefix + "/routex-api-key-" + plan.ResourceID
	raw, obs := w.client.request(ctx, "write", http.MethodPost, "data", path, "", writerToken, body)
	defer clear(raw)
	result.Write = obs
	result.Write.Duration = time.Since(started)
	if obs.Attempted {
		result.Outcome = APIKeyWriteUnknown
	}
	// request joins Close before returning. Cancellation or unproven closure must
	// not become an acknowledgement even when the metadata looked like version 1.
	if err := ctx.Err(); err != nil {
		return reject(contextFailure("write", err).Code)
	}
	if obs.Failure != nil {
		return result, obs.Failure
	}
	if obs.responseCloseFailed {
		return reject("response_close_failed")
	}
	if !apiKeyWriteResponse(raw) {
		return reject("invalid_response")
	}
	if err := ctx.Err(); err != nil {
		return reject(contextFailure("write", err).Code)
	}
	result.Version = 1
	result.Outcome = APIKeyWriteAcknowledged
	result.Write.Succeeded = true
	result.Write.Duration = time.Since(started)
	return result, nil
}

func apiKeyWriteResponse(raw []byte) bool {
	// request already enforces UTF-8, duplicate keys, depth, MIME and body bounds.
	// encoding/json replaces malformed UTF-16 escapes; reject those before proof.
	if !apiKeyWriteUnicode(raw) {
		return false
	}
	root, ok := object(raw)
	if !ok {
		return false
	}
	if field, present := root["errors"]; present {
		var values []string
		if json.Unmarshal(field, &values) != nil || values == nil || len(values) != 0 {
			return false
		}
	}
	for _, name := range []string{"auth", "wrap_info"} {
		if field, present := root[name]; present && !bytes.Equal(bytes.TrimSpace(field), []byte("null")) {
			return false
		}
	}
	version, err := writeVersion(raw)
	return err == nil && version == 1
}

// This string-escape admission supplements, rather than replaces, encoding/json.
// Literal U+FFFD and properly paired UTF-16 remain valid. Structure is validated
// by the existing parser; this scan only prevents silent Unicode replacement.
func apiKeyWriteUnicode(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '"' {
			continue
		}
		i++
		for i < len(raw) && raw[i] != '"' {
			if raw[i] != '\\' {
				i++
				continue
			}
			i++
			if i >= len(raw) {
				return false
			}
			if raw[i] != 'u' {
				i++
				continue
			}
			v, ok := apiKeyWriteHex4(raw, i+1)
			if !ok {
				return false
			}
			i += 5
			if v >= 0xD800 && v <= 0xDBFF {
				if i+6 > len(raw) || raw[i] != '\\' || raw[i+1] != 'u' {
					return false
				}
				low, valid := apiKeyWriteHex4(raw, i+2)
				if !valid || low < 0xDC00 || low > 0xDFFF {
					return false
				}
				i += 6
			} else if v >= 0xDC00 && v <= 0xDFFF {
				return false
			}
		}
		if i >= len(raw) {
			return false
		}
	}
	return true
}

func apiKeyWriteHex4(raw []byte, at int) (uint16, bool) {
	if at+4 > len(raw) {
		return 0, false
	}
	var v uint16
	for _, ch := range raw[at : at+4] {
		v <<= 4
		switch {
		case ch >= '0' && ch <= '9':
			v |= uint16(ch - '0')
		case ch >= 'a' && ch <= 'f':
			v |= uint16(ch - 'a' + 10)
		case ch >= 'A' && ch <= 'F':
			v |= uint16(ch - 'A' + 10)
		default:
			return 0, false
		}
	}
	return v, true
}
