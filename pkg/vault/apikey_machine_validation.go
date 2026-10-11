package vault

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// APIKeyMachineValidator observes only its own finite machine metadata. It has no
// KV, login, cleanup, Reader, Writer, Provider or arbitrary-path capability.
type APIKeyMachineValidatorConfig struct{ Endpoint, Namespace, KVMount, KeyPrefix, AuthMount string }
type APIKeyMachinePurpose string

const (
	APIKeyMachineWriter      APIKeyMachinePurpose = "writer"
	APIKeyMachineProvisioner APIKeyMachinePurpose = "provisioner"
)

type APIKeyMachineValidationInput struct {
	Purpose                            APIKeyMachinePurpose
	ProfileID, PrincipalID, ResourceID string
}
type APIKeyMachineCapability struct {
	Path    string
	Actions []string
}
type APIKeyMachineValidationPlan struct {
	Purpose APIKeyMachinePurpose
	SHA256  string
	Grants  []APIKeyMachineCapability
}
type APIKeyMachineValidationResult struct {
	Plan                      APIKeyMachineValidationPlan
	Consumed, Validated       bool
	TokenType                 string
	TTLSeconds                uint64
	ExpiresAt                 time.Time
	MachineIdentityCommitment string
	Grants                    []APIKeyMachineCapability
	Lookup, CapabilityCheck   Observation
}

func (APIKeyMachineValidationResult) String() string { return "<vault.APIKeyMachineValidationResult>" }
func (APIKeyMachineValidationResult) GoString() string {
	return "<vault.APIKeyMachineValidationResult>"
}
func (APIKeyMachineValidationResult) MarshalJSON() ([]byte, error) { return []byte(`{}`), nil }

type APIKeyMachineValidator struct {
	client *Client
	config APIKeyMachineValidatorConfig
	life   *apiKeyMachineLifetime
}
type apiKeyMachineLifetime struct {
	mu     sync.Mutex
	closed bool
}
type PreparedAPIKeyMachineValidation struct{ state *apiKeyMachineState }
type apiKeyMachineState struct {
	mu               sync.Mutex
	config           APIKeyMachineValidatorConfig
	plan             APIKeyMachineValidationPlan
	consumed, closed bool
}

func (PreparedAPIKeyMachineValidation) String() string {
	return "<vault.PreparedAPIKeyMachineValidation>"
}
func (PreparedAPIKeyMachineValidation) GoString() string {
	return "<vault.PreparedAPIKeyMachineValidation>"
}
func (PreparedAPIKeyMachineValidation) MarshalJSON() ([]byte, error) { return []byte(`{}`), nil }
func machineCopyGrants(in []APIKeyMachineCapability) []APIKeyMachineCapability {
	out := make([]APIKeyMachineCapability, len(in))
	for i, g := range in {
		out[i] = APIKeyMachineCapability{Path: g.Path, Actions: append([]string(nil), g.Actions...)}
	}
	return out
}
func machineCopyPlan(p APIKeyMachineValidationPlan) APIKeyMachineValidationPlan {
	p.Grants = machineCopyGrants(p.Grants)
	return p
}
func (p *PreparedAPIKeyMachineValidation) Plan() APIKeyMachineValidationPlan {
	if p == nil || p.state == nil {
		return APIKeyMachineValidationPlan{}
	}
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	return machineCopyPlan(p.state.plan)
}

// Close prevents claims, not a cancellation/join of an already admitted call.
func (p *PreparedAPIKeyMachineValidation) Close() {
	if p != nil && p.state != nil {
		p.state.mu.Lock()
		p.state.closed = true
		p.state.mu.Unlock()
	}
}
func NewAPIKeyMachineValidator(cfg APIKeyMachineValidatorConfig, allowPrivate bool) (*APIKeyMachineValidator, error) {
	if !validPath(cfg.AuthMount, 128, false) {
		return nil, &Failure{Stage: "prepare", Code: "invalid_config"}
	}
	c, err := New(Descriptor{Endpoint: cfg.Endpoint, Namespace: cfg.Namespace, Mount: cfg.KVMount, Prefix: cfg.KeyPrefix, DataField: "key"}, allowPrivate)
	if err != nil {
		return nil, &Failure{Stage: "prepare", Code: "invalid_config"}
	}
	cfg.Endpoint = c.endpoint.String()
	return &APIKeyMachineValidator{client: c, config: cfg, life: &apiKeyMachineLifetime{}}, nil
}
func (v *APIKeyMachineValidator) Close() {
	if v != nil && v.client != nil && v.life != nil {
		v.life.mu.Lock()
		v.life.closed = true
		v.life.mu.Unlock()
		v.client.Close()
	}
}
func (v *APIKeyMachineValidator) ResponseCloseState() ResponseCloseState {
	if v == nil || v.client == nil {
		return ResponseCloseState{}
	}
	return v.client.ResponseCloseState()
}
func machineID(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, b := range []byte(s) {
		if (b < 'a' || b > 'z') && (b < '0' || b > '9') && b != '_' && b != '-' {
			return false
		}
	}
	return true
}
func (v *APIKeyMachineValidator) Prepare(in APIKeyMachineValidationInput) (*PreparedAPIKeyMachineValidation, error) {
	if v == nil || v.client == nil || v.life == nil || !machineID(in.ProfileID) || !machineID(in.PrincipalID) {
		return nil, &Failure{Stage: "prepare", Code: "invalid_plan"}
	}
	var grants []APIKeyMachineCapability
	switch in.Purpose {
	case APIKeyMachineWriter:
		if !machineID(in.ResourceID) {
			return nil, &Failure{Stage: "prepare", Code: "invalid_plan"}
		}
		grants = []APIKeyMachineCapability{{v.config.KVMount + "/data/" + v.config.KeyPrefix + "/" + in.ProfileID + "/routex-api-key-" + in.ResourceID, []string{"create"}}}
	case APIKeyMachineProvisioner:
		if in.ResourceID != "" {
			return nil, &Failure{Stage: "prepare", Code: "invalid_plan"}
		}
		grants = []APIKeyMachineCapability{{"auth/" + v.config.AuthMount + "/config", []string{"read"}}, {"sys/policies/acl/routex-api-key-" + in.ProfileID, []string{"read", "update"}}, {"auth/" + v.config.AuthMount + "/role/routex-api-key-" + in.PrincipalID, []string{"create", "read", "update"}}}
	default:
		return nil, &Failure{Stage: "prepare", Code: "invalid_plan"}
	}
	raw, _ := json.Marshal(struct {
		Domain string
		Config APIKeyMachineValidatorConfig
		Input  APIKeyMachineValidationInput
		Grants []APIKeyMachineCapability
	}{"routex-api-key-machine-plan-v1", v.config, in, grants})
	digest := sha256.Sum256(raw)
	plan := APIKeyMachineValidationPlan{Purpose: in.Purpose, SHA256: hex.EncodeToString(digest[:]), Grants: grants}
	v.life.mu.Lock()
	defer v.life.mu.Unlock()
	if v.life.closed {
		return nil, &Failure{Stage: "prepare", Code: "validator_closed"}
	}
	return &PreparedAPIKeyMachineValidation{state: &apiKeyMachineState{config: v.config, plan: plan}}, nil
}

// Validate consumes exactly one local plan attempt, not a durable application
// claim. Two metadata requests share the original context and joined body Close.
func (v *APIKeyMachineValidator) Validate(parent context.Context, token string, p *PreparedAPIKeyMachineValidation) (APIKeyMachineValidationResult, error) {
	result := APIKeyMachineValidationResult{}
	fail := func(stage, code string) (APIKeyMachineValidationResult, error) {
		return result, &Failure{Stage: stage, Code: code}
	}
	if parent == nil {
		return fail("prepare", "invalid_request")
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(parent, requestTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return result, contextFailure("prepare", ctx.Err())
	}
	if v == nil || v.client == nil || v.life == nil {
		return fail("prepare", "invalid_config")
	}
	if !validToken(token) {
		return fail("prepare", "invalid_tokens")
	}
	if p == nil || p.state == nil {
		return fail("prepare", "invalid_plan")
	}
	v.life.mu.Lock()
	p.state.mu.Lock()
	if v.life.closed || p.state.closed || p.state.consumed || p.state.config != v.config {
		p.state.mu.Unlock()
		v.life.mu.Unlock()
		return fail("prepare", "invalid_plan")
	}
	if ctx.Err() != nil {
		p.state.mu.Unlock()
		v.life.mu.Unlock()
		return result, contextFailure("prepare", ctx.Err())
	}
	p.state.consumed = true
	plan := machineCopyPlan(p.state.plan)
	p.state.mu.Unlock()
	v.life.mu.Unlock()
	result.Plan = machineCopyPlan(plan)
	result.Consumed = true
	raw, obs := v.machineRequest(ctx, "lookup", http.MethodGet, "auth/token/lookup-self", token, nil)
	result.Lookup = obs
	if ctx.Err() != nil {
		clear(raw)
		result.Lookup.Failure = contextFailure("lookup", ctx.Err())
		return result, result.Lookup.Failure
	}
	if obs.Failure != nil {
		clear(raw)
		return result, obs.Failure
	}
	if obs.responseCloseFailed {
		clear(raw)
		result.Lookup.Failure = &Failure{Stage: "lookup", Code: "response_close_failed"}
		return result, result.Lookup.Failure
	}
	commitment, ttl, expires, ok := machineIdentity(raw, v.config, started)
	clear(raw)
	if !ok {
		result.Lookup.Failure = &Failure{Stage: "lookup", Code: "identity_rejected"}
		return result, result.Lookup.Failure
	}
	if ctx.Err() != nil {
		return result, contextFailure("lookup", ctx.Err())
	}
	result.Lookup.Succeeded = true
	if !time.Now().Before(expires) {
		result.Lookup.Failure = &Failure{Stage: "lookup", Code: "identity_expired"}
		return result, result.Lookup.Failure
	}
	paths := make([]string, len(plan.Grants))
	for i, g := range plan.Grants {
		paths[i] = g.Path
	}
	body, _ := json.Marshal(struct {
		Paths []string `json:"paths"`
	}{paths})
	defer clear(body)
	raw, obs = v.machineRequest(ctx, "capabilities", http.MethodPost, "sys/capabilities-self", token, body)
	result.CapabilityCheck = obs
	if ctx.Err() != nil {
		clear(raw)
		result.CapabilityCheck.Failure = contextFailure("capabilities", ctx.Err())
		return result, result.CapabilityCheck.Failure
	}
	if obs.Failure != nil {
		clear(raw)
		return result, obs.Failure
	}
	if obs.responseCloseFailed {
		clear(raw)
		result.CapabilityCheck.Failure = &Failure{Stage: "capabilities", Code: "response_close_failed"}
		return result, result.CapabilityCheck.Failure
	}
	ok = machineCapabilities(raw, plan.Grants)
	clear(raw)
	if !ok {
		result.CapabilityCheck.Failure = &Failure{Stage: "capabilities", Code: "capabilities_rejected"}
		return result, result.CapabilityCheck.Failure
	}
	if ctx.Err() != nil {
		return result, contextFailure("capabilities", ctx.Err())
	}
	if !time.Now().Before(expires) {
		result.CapabilityCheck.Failure = &Failure{Stage: "capabilities", Code: "identity_expired"}
		return result, result.CapabilityCheck.Failure
	}
	result.CapabilityCheck.Succeeded = true
	result.TokenType = "service"
	result.TTLSeconds = ttl
	result.ExpiresAt = expires
	result.MachineIdentityCommitment = commitment
	result.Grants = machineCopyGrants(plan.Grants)
	result.Validated = true
	return result, nil
}

// Fixed private metadata paths only. Ordinary Client.request encodes KV paths;
// retaining a dedicated helper does not expose or change those public contracts.
func (v *APIKeyMachineValidator) machineRequest(ctx context.Context, stage, method, path, token string, body []byte) (rawResult []byte, obs Observation) {
	started := time.Now()
	fail := func(code string, status int) ([]byte, Observation) {
		obs.Duration = time.Since(started)
		obs.Failure = &Failure{Stage: stage, Code: code, HTTPStatus: status}
		return nil, obs
	}
	if ctx.Err() != nil {
		obs.Failure = contextFailure(stage, ctx.Err())
		return nil, obs
	}
	endpoint := *v.client.endpoint
	endpoint.Path += "/v1/" + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fail("invalid_request", 0)
	}
	req.GetBody = nil
	req.Close = true
	req.Header.Set("X-Vault-Token", token)
	req.Header.Set("X-Vault-Request", "true")
	if v.config.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", v.config.Namespace)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	obs.Attempted = true
	response, err := v.client.http.Do(req)
	if err != nil {
		v.client.unprovenResponseClose()
		if ctx.Err() != nil {
			obs.Failure = contextFailure(stage, ctx.Err())
			obs.Duration = time.Since(started)
			return nil, obs
		}
		var timeout interface{ Timeout() bool }
		if errors.As(err, &timeout) && timeout.Timeout() {
			return fail("timed_out", 0)
		}
		return fail("transport", 0)
	}
	closeBody := v.client.trackResponseClose(response.Body)
	defer func() { obs.responseCloseFailed = closeBody(); obs.Duration = time.Since(started) }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		clear(raw)
		if ctx.Err() != nil {
			obs.Failure = contextFailure(stage, ctx.Err())
			return nil, obs
		}
		return fail("transport", response.StatusCode)
	}
	if len(raw) > maxResponseBytes {
		clear(raw)
		return fail("response_too_large", response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		clear(raw)
		return fail("http_status", response.StatusCode)
	}
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || !validJSON(raw) || !apiKeyWriteUnicode(raw) {
		clear(raw)
		return fail("invalid_response", response.StatusCode)
	}
	return raw, obs
}
func machinePolicies(raw json.RawMessage, required bool) bool {
	if len(raw) == 0 {
		return !required
	}
	var policies []string
	if json.Unmarshal(raw, &policies) != nil || required && policies == nil || len(policies) > 128 {
		return false
	}
	for _, p := range policies {
		if len(p) == 0 || len(p) > 256 || strings.EqualFold(p, "root") {
			return false
		}
		for _, b := range []byte(p) {
			if b < 33 || b > 126 {
				return false
			}
		}
	}
	return true
}
func machineIdentity(raw []byte, cfg APIKeyMachineValidatorConfig, started time.Time) (string, uint64, time.Time, bool) {
	root, ok := object(raw)
	if !ok || !machineEnvelope(root) {
		return "", 0, time.Time{}, false
	}
	d, ok := object(root["data"])
	if !ok || len(d) > 128 {
		return "", 0, time.Time{}, false
	}
	var typ, accessor, expiry *string
	var ttl *int64
	if json.Unmarshal(d["type"], &typ) != nil || typ == nil || *typ != "service" || json.Unmarshal(d["accessor"], &accessor) != nil || accessor == nil || len(*accessor) == 0 || len(*accessor) > 256 || !validToken(*accessor) || json.Unmarshal(d["ttl"], &ttl) != nil || ttl == nil || *ttl <= 0 || *ttl > int64((time.Duration(1<<63-1))/time.Second) || json.Unmarshal(d["expire_time"], &expiry) != nil || expiry == nil {
		return "", 0, time.Time{}, false
	}
	expires, err := time.Parse(time.RFC3339Nano, *expiry)
	if err != nil || !time.Now().Before(expires) {
		return "", 0, time.Time{}, false
	}
	conservative := started.Add(time.Duration(*ttl) * time.Second)
	if conservative.Before(expires) {
		expires = conservative
	}
	if !time.Now().Before(expires) {
		return "", 0, time.Time{}, false
	}
	if !machinePolicies(d["policies"], true) || !machinePolicies(d["identity_policies"], false) {
		return "", 0, time.Time{}, false
	}
	if r, present := d["external_namespace_policies"]; present && !bytes.Equal(bytes.TrimSpace(r), []byte("null")) {
		m, valid := object(r)
		if !valid || len(m) > 128 {
			return "", 0, time.Time{}, false
		}
		for ns, list := range m {
			if len(ns) == 0 || len(ns) > 256 || !machinePolicies(list, true) {
				return "", 0, time.Time{}, false
			}
		}
	}
	typed, _ := json.Marshal(struct{ Domain, Endpoint, Namespace, Accessor string }{"routex-api-key-machine-identity-v1", cfg.Endpoint, cfg.Namespace, *accessor})
	digest := sha256.Sum256(typed)
	clear(typed)
	return hex.EncodeToString(digest[:]), uint64(*ttl), expires, true
}

// Security collections are checked separately; other bounded lookup metadata is
// discarded, never inferred as authority or returned in a safe application DTO.
func machineEnvelope(root map[string]json.RawMessage) bool {
	if len(root) > 16 {
		return false
	}
	for k, r := range root {
		switch k {
		case "data":
		case "auth", "wrap_info":
			if !bytes.Equal(bytes.TrimSpace(r), []byte("null")) {
				return false
			}
		case "warnings", "errors":
			var a []string
			if json.Unmarshal(r, &a) != nil || len(a) != 0 || k == "errors" && a == nil {
				return false
			}
		case "request_id", "mount_type":
			var s *string
			if json.Unmarshal(r, &s) != nil || s == nil || len(*s) > 128 {
				return false
			}
		case "lease_id":
			var s *string
			if json.Unmarshal(r, &s) != nil || s == nil || *s != "" {
				return false
			}
		case "lease_duration":
			var n *int64
			if json.Unmarshal(r, &n) != nil || n == nil || *n != 0 {
				return false
			}
		case "renewable":
			var b *bool
			if json.Unmarshal(r, &b) != nil || b == nil || *b {
				return false
			}
		default:
			return false
		}
	}
	return true
}
func machineActions(raw json.RawMessage, want []string) bool {
	var got []string
	if json.Unmarshal(raw, &got) != nil || len(got) != len(want) {
		return false
	}
	sort.Strings(got)
	for i, a := range got {
		if a != want[i] {
			return false
		}
	}
	return true
}

// Vault sys endpoints can inject the exact data members as top-level aliases.
// Admit one coherent projection only; legacy flat responses are explicitly
// limited to the same exact internally generated path and single-path alias set.
func machineCapabilityMap(data map[string]json.RawMessage, want []APIKeyMachineCapability) bool {
	count := len(want)
	if count == 1 {
		count++
	}
	if len(data) != count {
		return false
	}
	for _, g := range want {
		if !machineActions(data[g.Path], g.Actions) {
			return false
		}
	}
	for path, actions := range data {
		found := false
		for _, g := range want {
			if path == g.Path {
				found = true
				break
			}
		}
		if found {
			continue
		}
		if path == "capabilities" && len(want) == 1 && machineActions(actions, want[0].Actions) {
			continue
		}
		return false
	}
	return true
}
func machineCapabilities(raw []byte, want []APIKeyMachineCapability) bool {
	root, ok := object(raw)
	if !ok || len(root) > 20 {
		return false
	}
	metadata := map[string]json.RawMessage{}
	aliases := map[string]json.RawMessage{}
	for k, r := range root {
		switch k {
		case "data", "request_id", "mount_type", "lease_id", "lease_duration", "renewable", "auth", "wrap_info", "warnings", "errors":
			metadata[k] = r
		default:
			aliases[k] = r
		}
	}
	if !machineEnvelope(metadata) {
		return false
	}
	if r, present := metadata["data"]; present {
		data, valid := object(r)
		if !valid || !machineCapabilityMap(data, want) {
			return false
		}
		// A normal envelope without injector aliases is supported. Once any sys alias
		// is supplied, every member must repeat the exact checked capability set.
		if len(aliases) != 0 && !machineCapabilityMap(aliases, want) {
			return false
		}
		return true
	}
	return machineCapabilityMap(aliases, want)
}
