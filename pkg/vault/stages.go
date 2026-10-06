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
)

// ProbePlan is nonsecret internal bookkeeping, not caller-supplied authority.
// Persist it with immutable descriptor/auth revisions before claiming Write.
// Neither this type nor syntactic validation proves authorization or issuance.
type ProbePlan struct {
	ProbeID          string `json:"probe_id"`
	ExpectedSHA256   string `json:"expected_sha256"`
	DescriptorSHA256 string `json:"descriptor_sha256"`
}

// PreparedProbe holds the transient marker; it cannot be restored from a Plan.
// Copies share the same one-attempt state. Close clears its owned buffer, without guaranteeing process
// memory erasure or canceling an already claimed Write's request-local copy.
type PreparedProbe struct{ state *preparedState }

type preparedState struct {
	mu                sync.Mutex
	plan              ProbePlan
	marker            []byte
	attempted, closed bool
}

func (PreparedProbe) String() string               { return "<vault.PreparedProbe>" }
func (PreparedProbe) GoString() string             { return "<vault.PreparedProbe>" }
func (PreparedProbe) MarshalJSON() ([]byte, error) { return []byte(`{}`), nil }

// Plan returns a copy, including after Close, for durable ownership recovery.
func (p *PreparedProbe) Plan() ProbePlan {
	if p == nil || p.state == nil {
		return ProbePlan{}
	}
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	return p.state.plan
}
func (p *PreparedProbe) Close() {
	if p == nil || p.state == nil {
		return
	}
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	clear(p.state.marker)
	p.state.marker = nil
	p.state.closed = true
}

// WriteResult never infers success from a later ReadResult.
type WriteResult struct {
	ProbeID string
	Version int64
	Write   Observation
}

// ReadResult represents an explicit read command and separate cleanup outcome.
type ReadResult struct {
	ProbeID string
	Version int64
	Read    Observation
	Cleanup Cleanup
}

// CleanupResult's Ownership is a fresh cleanup prerequisite, not a Read-test receipt.
type CleanupResult struct {
	ProbeID   string
	Version   int64
	Ownership Observation
	Cleanup   Cleanup
}

// Prepare generates a marker and nonsecret digest without making an HTTP request.
// A service must durably record the Plan and claim the command before Write;
// this package deliberately has no database/persistence or permission hooks.
func (c *Client) Prepare() (*PreparedProbe, error) {
	entropy := make([]byte, 48)
	defer clear(entropy)
	if _, err := rand.Read(entropy); err != nil {
		return nil, &Failure{Stage: "prepare", Code: "random_unavailable"}
	}
	marker := make([]byte, 64)
	hex.Encode(marker, entropy[16:])
	digest := sha256.Sum256(marker)
	return &PreparedProbe{state: &preparedState{plan: ProbePlan{ProbeID: hex.EncodeToString(entropy[:16]), ExpectedSHA256: hex.EncodeToString(digest[:]), DescriptorSHA256: c.descriptorDigest()}, marker: marker}}, nil
}

// Write consumes one valid prepared attempt, even if its response is lost.
// It is at most one HTTP request/10 seconds. A crash discards the marker; never
// regenerate it or replay Write for a persisted Plan. Explicit Read can recover
// ownership without turning a missing historical Write observation into success.
func (c *Client) Write(ctx context.Context, writer string, p *PreparedProbe) (WriteResult, error) {
	result := WriteResult{}
	start := time.Now()
	reject := func(code string) (WriteResult, error) {
		failure := &Failure{Stage: "write", Code: code}
		result.Write = Observation{Duration: time.Since(start), Failure: failure}
		return result, failure
	}
	if !validToken(writer) {
		return reject("invalid_tokens")
	}
	if p == nil || p.state == nil {
		return reject("invalid_plan")
	}
	p.state.mu.Lock()
	plan := p.state.plan
	if !c.validPlan(plan) {
		p.state.mu.Unlock()
		return reject("invalid_plan")
	}
	result.ProbeID = plan.ProbeID
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
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	body, _ := json.Marshal(map[string]any{"options": map[string]int{"cas": 0}, "data": map[string]string{c.descriptor.DataField: string(marker)}})
	defer clear(body)
	raw, observation := c.request(ctx, "write", http.MethodPost, "data", c.probePath(plan), "", writer, body)
	result.Write = observation
	if observation.Failure != nil {
		return result, observation.Failure
	}
	version, err := writeVersion(raw)
	clear(raw)
	if err != nil {
		result.Write.Failure = &Failure{Stage: "write", Code: "invalid_response", HTTPStatus: http.StatusOK}
		return result, result.Write.Failure
	}
	result.Version = version
	result.Write.Succeeded = true
	return result, nil
}

// ReadAndCleanup requires a durable service command claim before its GET.
// It performs one exact-v1 digest read, then at most one owned-v1 destruction,
// within20 seconds. No persistence hook/hidden retry exists between these calls;
// a crash leaves uncertainty for a later explicit ownership recheck.
func (c *Client) ReadAndCleanup(ctx context.Context, reader, writer string, plan ProbePlan) (ReadResult, error) {
	version, observation, cleanup, err := c.verifyAndClean(ctx, reader, writer, plan)
	id := ""
	if c.validPlan(plan) {
		id = plan.ProbeID
	}
	return ReadResult{ProbeID: id, Version: version, Read: observation, Cleanup: cleanup}, err
}

// CleanupOwned always rechecks fresh ownership, even after recorded Write/Read
// success. Authorize its immutable old revision separately in the service; do
// not accept a public path/version/Plan. A missing/destroyed read is not a cleanup
// acknowledgement. KV-v2 cannot atomically fence a path deletion/recreation
// between GET and destroy; restrict remote system-prefix writers accordingly.
func (c *Client) CleanupOwned(ctx context.Context, reader, writer string, plan ProbePlan) (CleanupResult, error) {
	version, observation, cleanup, err := c.verifyAndClean(ctx, reader, writer, plan)
	id := ""
	if c.validPlan(plan) {
		id = plan.ProbeID
	}
	return CleanupResult{ProbeID: id, Version: version, Ownership: observation, Cleanup: cleanup}, err
}

func (c *Client) verifyAndClean(ctx context.Context, reader, writer string, plan ProbePlan) (int64, Observation, Cleanup, error) {
	cleanup := Cleanup{State: "not_attempted"}
	if !c.validPlan(plan) {
		return 0, Observation{}, cleanup, &Failure{Stage: "prepare", Code: "invalid_plan"}
	}
	if !validToken(reader) || !validToken(writer) || reader == writer {
		return 0, Observation{}, cleanup, &Failure{Stage: "prepare", Code: "invalid_tokens"}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*requestTimeout)
	defer cancel()
	raw, observation := c.request(ctx, "read", http.MethodGet, "data", c.probePath(plan), "version=1", reader, nil)
	if observation.Failure == nil && !matchesPlan(raw, c.descriptor.DataField, plan.ExpectedSHA256) {
		observation.Failure = &Failure{Stage: "read", Code: "verification_failed", HTTPStatus: http.StatusOK}
	}
	clear(raw)
	if observation.Failure != nil {
		cleanup.State = "unknown"
		return 0, observation, cleanup, observation.Failure
	}
	observation.Succeeded = true
	raw, cleanup.Observation = c.request(ctx, "cleanup", http.MethodPut, "destroy", c.probePath(plan), "", writer, []byte(`{"versions":[1]}`))
	clear(raw)
	cleanup.State = "acknowledged"
	if failure := cleanup.Observation.Failure; failure != nil {
		cleanup.State = "unknown"
		if failure.Code == "http_status" && failure.HTTPStatus >= 400 && failure.HTTPStatus < 500 {
			cleanup.State = "failed"
		}
		return 1, observation, cleanup, failure
	}
	cleanup.Observation.Succeeded = true
	return 1, observation, cleanup, nil
}
func (c *Client) probePath(plan ProbePlan) string {
	return c.descriptor.Prefix + "/routex-probe-" + plan.ProbeID
}
func (c *Client) descriptorDigest() string {
	raw, _ := json.Marshal([]string{"routex-vault-probe-plan-v1", c.endpoint.String(), c.descriptor.Namespace, c.descriptor.Mount, c.descriptor.Prefix, c.descriptor.DataField})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func (c *Client) validPlan(plan ProbePlan) bool {
	return lowerHex(plan.ProbeID, 32) && lowerHex(plan.ExpectedSHA256, 64) && lowerHex(plan.DescriptorSHA256, 64) && plan.DescriptorSHA256 == c.descriptorDigest()
}
func lowerHex(value string, size int) bool {
	if len(value) != size {
		return false
	}
	for i := range len(value) {
		if (value[i] < '0' || value[i] > '9') && (value[i] < 'a' || value[i] > 'f') {
			return false
		}
	}
	return true
}
func matchesPlan(raw []byte, field, digest string) bool {
	root, ok := object(raw)
	if !ok {
		return false
	}
	data, ok := object(root["data"])
	if !ok || !metadata(data["metadata"], 1) {
		return false
	}
	fields, ok := object(data["data"])
	if !ok || len(fields) != 1 {
		return false
	}
	var value string
	if json.Unmarshal(fields[field], &value) != nil || !lowerHex(value, 64) {
		return false
	}
	hash := sha256.Sum256([]byte(value))
	expected, err := hex.DecodeString(digest)
	return err == nil && subtle.ConstantTimeCompare(hash[:], expected) == 1
}
