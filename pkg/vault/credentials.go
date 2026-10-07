package vault

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"
	"unicode/utf8"
)

const credentialMarkerField = "routex_ownership_marker"
const maxCredentialBytes = 2048

// CredentialPlan is internal nonsecret bookkeeping, never public caller authority.
// Persist it with immutable descriptor/auth revisions and an effect claim first.
// Its independent high-entropy marker digest is not a credential-value digest.
type CredentialPlan struct {
	ReferenceID          string `json:"reference_id"`
	ExpectedMarkerSHA256 string `json:"expected_marker_sha256"`
	DescriptorSHA256     string `json:"descriptor_sha256"`
}

type credentialPreparedState struct {
	mu                sync.Mutex
	plan              CredentialPlan
	marker            []byte
	attempted, closed bool
}

// PreparedCredential holds only a transient marker. Copies share one local
// Write claim; no persisted plan can regenerate or replay the original write.
type PreparedCredential struct{ state *credentialPreparedState }

func (PreparedCredential) String() string               { return "<vault.PreparedCredential>" }
func (PreparedCredential) GoString() string             { return "<vault.PreparedCredential>" }
func (PreparedCredential) MarshalJSON() ([]byte, error) { return []byte(`{}`), nil }
func (p *PreparedCredential) Plan() CredentialPlan {
	if p == nil || p.state == nil {
		return CredentialPlan{}
	}
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	return p.state.plan
}
func (p *PreparedCredential) Close() {
	if p == nil || p.state == nil {
		return
	}
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	clear(p.state.marker)
	p.state.marker = nil
	p.state.closed = true
}

// CredentialValue is transient resolved material, deliberately separate from
// observations. Copies share the clearing owner; explicit Bytes returns a copy
// whose lifetime/clearing is the caller's responsibility. Close cannot promise
// physical memory erasure or clear copies already handed to runtime preparation.
type CredentialValue struct{ state *credentialValueState }
type credentialValueState struct {
	mu    sync.Mutex
	value []byte
}

func (CredentialValue) String() string               { return "<vault.CredentialValue>" }
func (CredentialValue) GoString() string             { return "<vault.CredentialValue>" }
func (CredentialValue) MarshalJSON() ([]byte, error) { return []byte(`{}`), nil }
func (v *CredentialValue) Bytes() []byte {
	if v == nil || v.state == nil {
		return nil
	}
	v.state.mu.Lock()
	defer v.state.mu.Unlock()
	return append([]byte(nil), v.state.value...)
}
func (v *CredentialValue) Close() {
	if v == nil || v.state == nil {
		return
	}
	v.state.mu.Lock()
	defer v.state.mu.Unlock()
	clear(v.state.value)
	v.state.value = nil
}

type CredentialWriteResult struct {
	ReferenceID string
	Version     int64
	Write       Observation
}
type CredentialReadResult struct {
	ReferenceID string
	Version     int64
	Read        Observation
}
type CredentialCleanupResult struct {
	ReferenceID string
	Version     int64
	Ownership   Observation
	Cleanup     Cleanup
}

// PrepareCredential performs no HTTP. Paths are derived exclusively from the
// generated reference, never supplied by the caller. The configured data field
// cannot collide with the independent private ownership marker.
func (c *Client) PrepareCredential() (*PreparedCredential, error) {
	if c.descriptor.DataField == credentialMarkerField {
		return nil, &Failure{Stage: "prepare", Code: "reserved_field"}
	}
	entropy := make([]byte, 48)
	defer clear(entropy)
	if _, err := rand.Read(entropy); err != nil {
		return nil, &Failure{Stage: "prepare", Code: "random_unavailable"}
	}
	marker := make([]byte, 64)
	hex.Encode(marker, entropy[16:])
	digest := sha256.Sum256(marker)
	plan := CredentialPlan{ReferenceID: hex.EncodeToString(entropy[:16]), ExpectedMarkerSHA256: hex.EncodeToString(digest[:]), DescriptorSHA256: c.credentialDescriptorDigest()}
	return &PreparedCredential{state: &credentialPreparedState{plan: plan, marker: marker}}, nil
}

// WriteCredential makes at most one CAS0 POST within10s, including after response
// loss. Caller-owned value is not retained or normalized. Service persistence,
// authority/root epoch and outcome reconciliation remain caller responsibilities.
func (c *Client) WriteCredential(ctx context.Context, writer string, p *PreparedCredential, value []byte) (CredentialWriteResult, error) {
	result := CredentialWriteResult{}
	start := time.Now()
	reject := func(code string) (CredentialWriteResult, error) {
		f := &Failure{Stage: "write", Code: code}
		result.Write = Observation{Duration: time.Since(start), Failure: f}
		return result, f
	}
	if !validToken(writer) {
		return reject("invalid_tokens")
	}
	if !validCredentialValue(value) {
		return reject("invalid_value")
	}
	if p == nil || p.state == nil {
		return reject("invalid_plan")
	}
	p.state.mu.Lock()
	plan := p.state.plan
	if !c.validCredentialPlan(plan) {
		p.state.mu.Unlock()
		return reject("invalid_plan")
	}
	result.ReferenceID = plan.ReferenceID
	if p.state.attempted {
		p.state.mu.Unlock()
		return reject("already_attempted")
	}
	if p.state.closed || len(p.state.marker) != 64 {
		p.state.mu.Unlock()
		return reject("prepared_closed")
	}
	p.state.attempted = true
	marker := append([]byte(nil), p.state.marker...)
	clear(p.state.marker)
	p.state.marker = nil
	p.state.mu.Unlock()
	defer clear(marker)
	body, _ := json.Marshal(map[string]any{"options": map[string]int{"cas": 0}, "data": map[string]string{c.descriptor.DataField: string(value), credentialMarkerField: string(marker)}})
	defer clear(body)
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	raw, obs := c.request(ctx, "write", http.MethodPost, "data", c.credentialPath(plan), "", writer, body)
	defer clear(raw)
	result.Write = obs
	if obs.Failure != nil {
		return result, obs.Failure
	}
	version, err := writeVersion(raw)
	if err != nil {
		result.Write.Failure = &Failure{Stage: "write", Code: "invalid_response", HTTPStatus: http.StatusOK}
		return result, result.Write.Failure
	}
	result.Version = version
	result.Write.Succeeded = true
	return result, nil
}

// ReadCredential resolves only exact live version1 with a matching marker. It
// never falls back to another source/current version or changes historical Write
// observations. A canceled or malformed response returns no credential value.
func (c *Client) ReadCredential(ctx context.Context, reader string, plan CredentialPlan) (*CredentialValue, CredentialReadResult, error) {
	result := CredentialReadResult{}
	if !c.validCredentialPlan(plan) {
		return nil, result, &Failure{Stage: "prepare", Code: "invalid_plan"}
	}
	result.ReferenceID = plan.ReferenceID
	if !validToken(reader) {
		return nil, result, &Failure{Stage: "prepare", Code: "invalid_tokens"}
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	raw, obs := c.request(ctx, "read", http.MethodGet, "data", c.credentialPath(plan), "version=1", reader, nil)
	defer clear(raw)
	result.Read = obs
	if obs.Failure != nil {
		return nil, result, obs.Failure
	}
	value, ok := credentialResponse(raw, c.descriptor.DataField, plan.ExpectedMarkerSHA256)
	if !ok {
		result.Read.Failure = &Failure{Stage: "read", Code: "verification_failed", HTTPStatus: http.StatusOK}
		return nil, result, result.Read.Failure
	}
	result.Version = 1
	result.Read.Succeeded = true
	return &CredentialValue{state: &credentialValueState{value: value}}, result, nil
}

// CleanupCredentialOwned uses an explicit cleanup Token, never an implicit
// writer privilege. Distinct local reader/cleanup strings do not attest remote
// principals or historical writer separation; the service must bind/authorize
// that policy. Claim the entire command durably before one ownership GET and
// conditional destroy1, release locks before HTTP, at most20s/no detached work.
// GET→destroy has no remote path-incarnation CAS: exclusive prefix writers are a
// deployment requirement. A404 or already destroyed version is not success.
func (c *Client) CleanupCredentialOwned(ctx context.Context, reader, cleanupToken string, plan CredentialPlan) (CredentialCleanupResult, error) {
	result := CredentialCleanupResult{Cleanup: Cleanup{State: "not_attempted"}}
	if !c.validCredentialPlan(plan) {
		return result, &Failure{Stage: "prepare", Code: "invalid_plan"}
	}
	result.ReferenceID = plan.ReferenceID
	if !validToken(reader) || !validToken(cleanupToken) || reader == cleanupToken {
		return result, &Failure{Stage: "prepare", Code: "invalid_tokens"}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*requestTimeout)
	defer cancel()
	value, read, err := c.ReadCredential(ctx, reader, plan)
	result.Ownership = read.Read
	if err != nil {
		result.Cleanup.State = "unknown"
		return result, err
	}
	value.Close()
	result.Version = 1
	// The synchronous ownership read returned only after its response Close.
	// Preserve that read evidence, but never destroy after failed/unjoined Close.
	if state := c.ResponseCloseState(); read.Read.responseCloseFailed || state.Pending != 0 {
		result.Cleanup.State = "unknown"
		return result, &Failure{Stage: "cleanup", Code: "transport"}
	}
	raw, obs := c.request(ctx, "cleanup", http.MethodPut, "destroy", c.credentialPath(plan), "", cleanupToken, []byte(`{"versions":[1]}`))
	clear(raw)
	result.Cleanup.Observation = obs
	if obs.Failure != nil {
		result.Cleanup.State = "unknown"
		if obs.Failure.Code == "http_status" && obs.Failure.HTTPStatus >= 400 && obs.Failure.HTTPStatus < 500 {
			result.Cleanup.State = "failed"
		}
		return result, obs.Failure
	}
	result.Cleanup.State = "acknowledged"
	result.Cleanup.Observation.Succeeded = true
	return result, nil
}
func validCredentialValue(value []byte) bool {
	if len(value) == 0 || len(value) > maxCredentialBytes || !utf8.Valid(value) {
		return false
	}
	for _, b := range value {
		if b == '\r' || b == '\n' {
			return false
		}
	}
	return true
}
func (c *Client) credentialPath(plan CredentialPlan) string {
	return c.descriptor.Prefix + "/routex-credential-" + plan.ReferenceID
}
func (c *Client) credentialDescriptorDigest() string {
	raw, _ := json.Marshal([]string{"routex-vault-credential-plan-v1", c.endpoint.String(), c.descriptor.Namespace, c.descriptor.Mount, c.descriptor.Prefix, c.descriptor.DataField, credentialMarkerField})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func (c *Client) validCredentialPlan(plan CredentialPlan) bool {
	return c.descriptor.DataField != credentialMarkerField && lowerHex(plan.ReferenceID, 32) && lowerHex(plan.ExpectedMarkerSHA256, 64) && lowerHex(plan.DescriptorSHA256, 64) && plan.DescriptorSHA256 == c.credentialDescriptorDigest()
}
func credentialResponse(raw []byte, field, digest string) ([]byte, bool) {
	root, ok := object(raw)
	if !ok {
		return nil, false
	}
	data, ok := object(root["data"])
	if !ok || !metadata(data["metadata"], 1) {
		return nil, false
	}
	fields, ok := object(data["data"])
	if !ok || len(fields) != 2 {
		return nil, false
	}
	var value, marker string
	if json.Unmarshal(fields[field], &value) != nil || json.Unmarshal(fields[credentialMarkerField], &marker) != nil || !validCredentialValue([]byte(value)) || !lowerHex(marker, 64) {
		return nil, false
	}
	hash := sha256.Sum256([]byte(marker))
	expected, err := hex.DecodeString(digest)
	if err != nil || subtle.ConstantTimeCompare(hash[:], expected) != 1 {
		return nil, false
	}
	return []byte(value), true
}
