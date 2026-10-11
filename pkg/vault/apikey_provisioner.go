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
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/miclle/routex/pkg/upstream"
)

// APIKeyProvisionerConfig is reviewed public configuration, not caller authority.
// This finite adapter supports direct system-trusted HTTPS JWKS only.
type APIKeyProvisionerConfig struct {
	Endpoint, Namespace, KVMount, KeyPrefix, AuthMount, Issuer, JWKSURL string
}

// APIKeyProvisionerInput contains exact admitted stable identities. No arbitrary
// policy, role name, path, endpoint, claim expression or caller Token is accepted.
type APIKeyProvisionerInput struct {
	ProfileID, PrincipalID, Subject, Audience string
	TokenTTLSeconds, TokenMaxTTLSeconds       int64
}

// APIKeyProvisioningPlan is safe local bookkeeping, never an authority receipt.
type APIKeyProvisioningPlan struct {
	ProfileID, PrincipalID, PolicyName, RoleName, SHA256 string
}

type APIKeyProvisionerOutcome string

const (
	APIKeyProvisionerNotAttempted APIKeyProvisionerOutcome = "not_attempted"
	APIKeyProvisionerUnknown      APIKeyProvisionerOutcome = "unknown"
	APIKeyProvisionerReconciled   APIKeyProvisionerOutcome = "reconciled"
)

// Reconciled means only exact desired metadata readback. It is not READY, caller
// login/read, effective-privilege, namespace isolation or permanent drift proof.
type APIKeyProvisionerResult struct {
	Plan                                                APIKeyProvisioningPlan
	Consumed                                            bool
	Outcome                                             APIKeyProvisionerOutcome
	DesiredGrantsReconciled                             bool
	Trust, PolicyWrite, PolicyRead, RoleWrite, RoleRead Observation
}

type apiKeyProvisionerLifetime struct {
	mu     sync.Mutex
	closed bool
}

// APIKeyProvisioner privately composes guarded transport/closure evidence. It has
// no KV value, cleanup, login, Reader or provider-credential method.
type APIKeyProvisioner struct {
	client *Client
	config APIKeyProvisionerConfig
	life   *apiKeyProvisionerLifetime
}

type apiKeyProvisioningState struct {
	mu               sync.Mutex
	config           APIKeyProvisionerConfig
	input            APIKeyProvisionerInput
	plan             APIKeyProvisioningPlan
	consumed, closed bool
}

// Copies share a single local attempt; this is not a durable command claim.
type PreparedAPIKeyProvisioning struct{ state *apiKeyProvisioningState }

func (PreparedAPIKeyProvisioning) String() string               { return "<vault.PreparedAPIKeyProvisioning>" }
func (PreparedAPIKeyProvisioning) GoString() string             { return "<vault.PreparedAPIKeyProvisioning>" }
func (PreparedAPIKeyProvisioning) MarshalJSON() ([]byte, error) { return []byte(`{}`), nil }
func (p *PreparedAPIKeyProvisioning) Plan() APIKeyProvisioningPlan {
	if p == nil || p.state == nil {
		return APIKeyProvisioningPlan{}
	}
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	return p.state.plan
}

// Close prevents another claim; it does not cancel or join an in-flight call.
func (p *PreparedAPIKeyProvisioning) Close() {
	if p != nil && p.state != nil {
		p.state.mu.Lock()
		p.state.closed = true
		p.state.mu.Unlock()
	}
}

func NewAPIKeyProvisioner(cfg APIKeyProvisionerConfig, allowPrivate bool) (*APIKeyProvisioner, error) {
	// Validate issuer/JWKS syntax without fetching either or adopting custom CA.
	if !provisionerHTTPS(cfg.Issuer, allowPrivate) || !provisionerHTTPS(cfg.JWKSURL, allowPrivate) || !validPath(cfg.AuthMount, 128, false) {
		return nil, &Failure{Stage: "prepare", Code: "invalid_config"}
	}
	c, err := New(Descriptor{Endpoint: cfg.Endpoint, Namespace: cfg.Namespace, Mount: cfg.KVMount, Prefix: cfg.KeyPrefix, DataField: "key"}, allowPrivate)
	if err != nil {
		return nil, &Failure{Stage: "prepare", Code: "invalid_config"}
	}
	cfg.Endpoint = c.endpoint.String()
	return &APIKeyProvisioner{client: c, config: cfg, life: &apiKeyProvisionerLifetime{}}, nil
}
func provisionerHTTPS(raw string, allowPrivate bool) bool {
	u, err := upstream.ValidateBaseURL(raw, allowPrivate)
	return err == nil && len(raw) <= 2048 && utf8.ValidString(raw) && u != nil && u.String() == raw && u.Scheme == "https" && u.RawPath == "" && !strings.Contains(u.Path, "//")
}
func (p *APIKeyProvisioner) Close() {
	if p == nil || p.client == nil || p.life == nil {
		return
	}
	p.life.mu.Lock()
	p.life.closed = true
	p.life.mu.Unlock()
	p.client.Close() // idle disposal does not prove in-flight closure
}
func (p *APIKeyProvisioner) ResponseCloseState() ResponseCloseState {
	if p == nil || p.client == nil {
		return ResponseCloseState{}
	}
	return p.client.ResponseCloseState()
}
func provisionerID(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, c := range []byte(s) {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			return false
		}
	}
	return true
}
func provisionerText(s string) bool {
	if len(s) == 0 || len(s) > 256 || !utf8.ValidString(s) {
		return false
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}
func (p *APIKeyProvisioner) Prepare(in APIKeyProvisionerInput) (*PreparedAPIKeyProvisioning, error) {
	if p == nil || p.client == nil || p.life == nil || !provisionerID(in.ProfileID) || !provisionerID(in.PrincipalID) || !provisionerText(in.Subject) || !provisionerText(in.Audience) || in.TokenTTLSeconds < 1 || in.TokenMaxTTLSeconds < in.TokenTTLSeconds || in.TokenMaxTTLSeconds > 3600 {
		return nil, &Failure{Stage: "prepare", Code: "invalid_plan"}
	}
	p.life.mu.Lock()
	defer p.life.mu.Unlock()
	if p.life.closed {
		return nil, &Failure{Stage: "prepare", Code: "provisioner_closed"}
	}
	plan := APIKeyProvisioningPlan{ProfileID: in.ProfileID, PrincipalID: in.PrincipalID, PolicyName: "routex-api-key-" + in.ProfileID, RoleName: "routex-api-key-" + in.PrincipalID}
	raw, _ := json.Marshal(struct {
		Purpose string
		Config  APIKeyProvisionerConfig
		Input   APIKeyProvisionerInput
	}{"routex-api-key-provisioning-plan-v1", p.config, in})
	digest := sha256.Sum256(raw)
	plan.SHA256 = hex.EncodeToString(digest[:])
	return &PreparedAPIKeyProvisioning{state: &apiKeyProvisioningState{config: p.config, input: in, plan: plan}}, nil
}
func provisionerPolicy(cfg APIKeyProvisionerConfig, in APIKeyProvisionerInput) string {
	// All interpolated segments passed canonical ASCII path admission, so no HCL
	// quoting, glob injection, metadata read, list, create, update or delete grant.
	return "path \"" + cfg.KVMount + "/data/" + cfg.KeyPrefix + "/" + in.ProfileID + "/*\" {\n  capabilities = [\"read\"]\n}\n"
}

// These native JWT-plugin defaults are an explicit initial adapter choice, not
// an application-contract requirement or an arbitrary readback tolerance.
const (
	provisionerClockSkewLeewaySeconds  int64 = 60
	provisionerExpirationLeewaySeconds int64 = 150
	provisionerNotBeforeLeewaySeconds  int64 = 150
)

func provisionerRole(in APIKeyProvisionerInput, name string) []byte {
	raw, _ := json.Marshal(struct {
		RoleType             string   `json:"role_type"`
		UserClaim            string   `json:"user_claim"`
		BoundSubject         string   `json:"bound_subject"`
		BoundAudiences       []string `json:"bound_audiences"`
		TokenPolicies        []string `json:"token_policies"`
		TokenNoDefaultPolicy bool     `json:"token_no_default_policy"`
		TokenTTL             int64    `json:"token_ttl"`
		TokenMaxTTL          int64    `json:"token_max_ttl"`
		ClockSkewLeeway      int64    `json:"clock_skew_leeway"`
		ExpirationLeeway     int64    `json:"expiration_leeway"`
		NotBeforeLeeway      int64    `json:"not_before_leeway"`
	}{"jwt", "sub", in.Subject, []string{in.Audience}, []string{name}, true, in.TokenTTLSeconds, in.TokenMaxTTLSeconds, provisionerClockSkewLeewaySeconds, provisionerExpirationLeewaySeconds, provisionerNotBeforeLeewaySeconds})
	return raw
}

// Reconcile uses one caller-shortened 10s deadline for all five fixed steps,
// including synchronous body Close and every final proof check. No automatic
// retry, compensation or detached cleanup is available on an unknown outcome.
func (p *APIKeyProvisioner) Reconcile(parent context.Context, token string, prepared *PreparedAPIKeyProvisioning) (APIKeyProvisionerResult, error) {
	result := APIKeyProvisionerResult{Outcome: APIKeyProvisionerNotAttempted}
	reject := func(stage, code string) (APIKeyProvisionerResult, error) {
		return result, &Failure{Stage: stage, Code: code}
	}
	if parent == nil {
		return reject("prepare", "invalid_request")
	}
	ctx, cancel := context.WithTimeout(parent, requestTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return result, contextFailure("prepare", ctx.Err())
	}
	if p == nil || p.client == nil || p.life == nil {
		return reject("prepare", "invalid_config")
	}
	if !validToken(token) {
		return reject("prepare", "invalid_tokens")
	}
	if prepared == nil || prepared.state == nil {
		return reject("prepare", "invalid_plan")
	}
	p.life.mu.Lock()
	prepared.state.mu.Lock()
	state := prepared.state
	if p.life.closed || state.closed || state.consumed || state.config != p.config {
		code := "invalid_plan"
		if p.life.closed {
			code = "provisioner_closed"
		} else if state.closed {
			code = "prepared_closed"
		} else if state.consumed {
			code = "already_attempted"
		}
		state.mu.Unlock()
		p.life.mu.Unlock()
		return reject("prepare", code)
	}
	if ctx.Err() != nil {
		state.mu.Unlock()
		p.life.mu.Unlock()
		return result, contextFailure("prepare", ctx.Err())
	}
	state.consumed = true
	in, plan := state.input, state.plan
	state.mu.Unlock()
	p.life.mu.Unlock()
	result.Consumed = true
	result.Plan = plan
	policy := provisionerPolicy(p.config, in)
	policyBody, _ := json.Marshal(struct {
		Policy string `json:"policy"`
	}{policy})
	defer clear(policyBody)
	roleBody := provisionerRole(in, plan.PolicyName)
	defer clear(roleBody)
	steps := []struct {
		stage, method, path string
		body                []byte
		obs                 *Observation
		verify              func([]byte) bool
	}{
		{"trust", http.MethodGet, "auth/" + p.config.AuthMount + "/config", nil, &result.Trust, func(raw []byte) bool { return provisionerTrust(raw, p.config) }},
		{"policy_write", http.MethodPut, "sys/policies/acl/" + plan.PolicyName, policyBody, &result.PolicyWrite, provisionerWriteAck},
		{"policy_read", http.MethodGet, "sys/policies/acl/" + plan.PolicyName, nil, &result.PolicyRead, func(raw []byte) bool { return provisionerPolicyMatches(raw, plan.PolicyName, policy) }},
		{"role_write", http.MethodPost, "auth/" + p.config.AuthMount + "/role/" + plan.RoleName, roleBody, &result.RoleWrite, provisionerWriteAck},
		{"role_read", http.MethodGet, "auth/" + p.config.AuthMount + "/role/" + plan.RoleName, nil, &result.RoleRead, func(raw []byte) bool { return provisionerRoleMatches(raw, in, plan.PolicyName) }},
	}
	for i, step := range steps {
		raw, obs := p.metadataRequest(ctx, step.stage, step.method, step.path, token, step.body)
		*step.obs = obs
		if (i == 1 || i == 3) && obs.Attempted {
			result.Outcome = APIKeyProvisionerUnknown
		}
		if ctx.Err() != nil {
			clear(raw)
			step.obs.Failure = contextFailure(step.stage, ctx.Err())
			return result, step.obs.Failure
		}
		if obs.Failure != nil {
			clear(raw)
			return result, obs.Failure
		}
		if obs.responseCloseFailed {
			clear(raw)
			step.obs.Failure = &Failure{Stage: step.stage, Code: "response_close_failed"}
			return result, step.obs.Failure
		}
		valid := step.verify(raw)
		clear(raw)
		if !valid {
			step.obs.Failure = &Failure{Stage: step.stage, Code: "metadata_mismatch"}
			return result, step.obs.Failure
		}
		if ctx.Err() != nil {
			step.obs.Failure = contextFailure(step.stage, ctx.Err())
			return result, step.obs.Failure
		}
		step.obs.Succeeded = true
	}
	if ctx.Err() != nil {
		return result, contextFailure("role_read", ctx.Err())
	}
	result.Outcome = APIKeyProvisionerReconciled
	result.DesiredGrantsReconciled = true
	return result, nil
}

// Only the fixed internal paths above reach this helper. Client.request cannot
// express these non-KV paths or write 204s without changing its existing contract.
func (p *APIKeyProvisioner) metadataRequest(ctx context.Context, stage, method, path, token string, body []byte) (rawResult []byte, obs Observation) {
	start := time.Now()
	fail := func(code string, status int) ([]byte, Observation) {
		obs.Duration = time.Since(start)
		obs.Failure = &Failure{Stage: stage, Code: code, HTTPStatus: status}
		return nil, obs
	}
	if ctx.Err() != nil {
		obs.Failure = contextFailure(stage, ctx.Err())
		return nil, obs
	}
	endpoint := *p.client.endpoint
	endpoint.Path += "/v1/" + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fail("invalid_request", 0)
	}
	req.GetBody = nil
	req.Close = true
	req.Header.Set("X-Vault-Request", "true")
	req.Header.Set("X-Vault-Token", token)
	if p.config.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", p.config.Namespace)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	obs.Attempted = true
	response, err := p.client.http.Do(req)
	if err != nil {
		p.client.unprovenResponseClose()
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
	closeResponse := p.client.trackResponseClose(response.Body)
	defer func() { obs.responseCloseFailed = closeResponse(); obs.Duration = time.Since(start) }()
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
	write := method != http.MethodGet
	if response.StatusCode == http.StatusNoContent && write {
		if len(raw) != 0 {
			clear(raw)
			return fail("invalid_response", response.StatusCode)
		}
		obs.Duration = time.Since(start)
		return raw, obs
	}
	if response.StatusCode != http.StatusOK {
		clear(raw)
		return fail("http_status", response.StatusCode)
	}
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || !validJSON(raw) || !apiKeyWriteUnicode(raw) || !provisionerEnvelope(raw, stage) {
		clear(raw)
		return fail("invalid_response", response.StatusCode)
	}
	obs.Duration = time.Since(start)
	return raw, obs
}
func provisionerEnvelope(raw []byte, stage string) bool {
	root, ok := object(raw)
	if !ok || len(root) > 16 {
		return false
	}
	_, hasName := root["name"]
	_, hasPolicy := root["policy"]
	if hasName != hasPolicy {
		return false
	}
	for key, value := range root {
		switch key {
		case "name", "policy":
			// Vault system responses retain data and repeat its fields as aliases.
			// Accept this fixed policy shape only when both representations agree.
			data, valid := object(root["data"])
			var alias *string
			if stage != "policy_read" || !valid || json.Unmarshal(value, &alias) != nil || alias == nil || !provisionerString(data[key], *alias) {
				return false
			}
		case "data":
		case "auth", "wrap_info":
			if !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return false
			}
		case "errors", "warnings":
			var v []string
			if json.Unmarshal(value, &v) != nil || len(v) != 0 || key == "errors" && v == nil {
				return false
			}
		case "request_id", "mount_type", "lease_id":
			var v string
			if json.Unmarshal(value, &v) != nil || len(v) > 128 || key == "lease_id" && v != "" {
				return false
			}
		case "renewable":
			var v *bool
			if json.Unmarshal(value, &v) != nil || v == nil || *v {
				return false
			}
		case "lease_duration":
			var v *int64
			if json.Unmarshal(value, &v) != nil || v == nil || *v != 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
func provisionerWriteAck(raw []byte) bool {
	if len(raw) == 0 {
		return true
	}
	root, ok := object(raw)
	if !ok {
		return false
	}
	v, present := root["data"]
	return !present || bytes.Equal(bytes.TrimSpace(v), []byte("null"))
}
func provisionerData(raw []byte) (map[string]json.RawMessage, bool) {
	root, ok := object(raw)
	if !ok {
		return nil, false
	}
	data, ok := object(root["data"])
	return data, ok && len(data) <= 128
}
func provisionerString(v json.RawMessage, want string) bool {
	var got *string
	return json.Unmarshal(v, &got) == nil && got != nil && *got == want
}
func provisionerList(v json.RawMessage, want string) bool {
	var got []string
	return json.Unmarshal(v, &got) == nil && len(got) == 1 && got[0] == want
}
func provisionerTrust(raw []byte, cfg APIKeyProvisionerConfig) bool {
	d, ok := provisionerData(raw)
	if !ok || !provisionerString(d["bound_issuer"], cfg.Issuer) || !provisionerString(d["jwks_url"], cfg.JWKSURL) {
		return false
	}
	for _, key := range []string{"oidc_discovery_url", "jwks_ca_pem", "oidc_discovery_ca_pem", "oidc_client_id", "oidc_client_secret"} {
		if v, present := d[key]; present && !provisionerString(v, "") {
			return false
		}
	}
	for _, key := range []string{"jwt_validation_pubkeys", "jwks_pairs"} {
		if v, present := d[key]; present {
			var a []json.RawMessage
			if json.Unmarshal(v, &a) != nil || len(a) != 0 {
				return false
			}
		}
	}
	if v, present := d["provider_config"]; present && !bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
		m, valid := object(v)
		if !valid || len(m) != 0 {
			return false
		}
	}
	return true
}
func provisionerPolicyMatches(raw []byte, name, policy string) bool {
	d, ok := provisionerData(raw)
	return ok && len(d) == 2 && provisionerString(d["name"], name) && provisionerString(d["policy"], policy)
}
func provisionerRoleMatches(raw []byte, in APIKeyProvisionerInput, policy string) bool {
	d, ok := provisionerData(raw)
	if !ok {
		return false
	}
	for key, want := range map[string]string{"role_type": "jwt", "user_claim": "sub", "bound_subject": in.Subject} {
		if !provisionerString(d[key], want) {
			return false
		}
	}
	if !provisionerList(d["bound_audiences"], in.Audience) || !provisionerList(d["token_policies"], policy) {
		return false
	}
	var noDefault *bool
	if json.Unmarshal(d["token_no_default_policy"], &noDefault) != nil || noDefault == nil || !*noDefault {
		return false
	}
	for key, want := range map[string]int64{"token_ttl": in.TokenTTLSeconds, "token_max_ttl": in.TokenMaxTTLSeconds, "clock_skew_leeway": provisionerClockSkewLeewaySeconds, "expiration_leeway": provisionerExpirationLeewaySeconds, "not_before_leeway": provisionerNotBeforeLeewaySeconds} {
		var v *int64
		if json.Unmarshal(d[key], &v) != nil || v == nil || *v != want {
			return false
		}
	}
	for key, raw := range d {
		switch key {
		case "role_type", "user_claim", "bound_subject", "bound_audiences", "token_policies", "token_no_default_policy", "token_ttl", "token_max_ttl", "clock_skew_leeway", "expiration_leeway", "not_before_leeway":
		case "policies":
			if !provisionerList(raw, policy) {
				return false
			}
		case "bound_claims_type":
			if !provisionerString(raw, "string") {
				return false
			}
		case "token_type":
			if !provisionerString(raw, "default") && !provisionerString(raw, "service") {
				return false
			}
		case "groups_claim":
			if !provisionerString(raw, "") {
				return false
			}
		case "alias_metadata":
			// Native Vault returns an empty map even when no alias metadata is configured.
			v, valid := object(raw)
			if !valid || len(v) != 0 {
				return false
			}
		case "bound_claims", "claim_mappings":
			if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				v, valid := object(raw)
				if !valid || len(v) != 0 {
					return false
				}
			}
		case "token_bound_cidrs", "allowed_redirect_uris", "oidc_scopes":
			var v []string
			if json.Unmarshal(raw, &v) != nil || len(v) != 0 {
				return false
			}
		case "token_period", "token_explicit_max_ttl", "token_num_uses", "max_age":
			var v *int64
			if json.Unmarshal(raw, &v) != nil || v == nil || *v != 0 {
				return false
			}
		case "user_claim_json_pointer", "verbose_oidc_logging":
			var v *bool
			if json.Unmarshal(raw, &v) != nil || v == nil || *v {
				return false
			}
		default:
			return false
		}
	}
	return true
}
