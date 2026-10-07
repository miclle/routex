package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secret"
)

func sourceHolderRuntime(t *testing.T, base, protocol string) (*Service, string, credentialSourceKey, *runtimeData) {
	t.Helper()
	s, data, bearer := runtimeFixture(t, base)
	c := &data.Credentials[0]
	c.CreatedAt = time.Date(2026, 10, 1, 0, 0, 0, 123000, time.UTC)
	c.StorageSource, c.Ciphertext = "vault", ""
	c.VaultReference = &entity.CredentialVaultReference{
		CredentialID: c.ID, CredentialBirth: c.CreatedAt, ReferenceID: strings.Repeat("a", 32),
		ExpectedMarkerSHA256: strings.Repeat("b", 64), DescriptorSHA256: strings.Repeat("c", 64),
		IntegrationID: "vlt_01k00000000000000000000000", IntegrationBirth: c.CreatedAt,
		RevisionID: "vlr_01k00000000000000000000000", ReaderGeneration: "reader_generation",
		PhysicalObject: rootHash("holder-fixture-object"), ReaderMethod: "token", SourceContext: strings.Repeat("d", 64),
	}
	var err error
	c.VaultReference.ReaderCiphertext, err = s.secrets.Seal(rootReference("vault_reader_auth", c.VaultReference.RevisionID, c.VaultReference.ReaderGeneration), "reader-token")
	if err != nil {
		t.Fatal(err)
	}
	proof := credentialSourceProof(*c)
	auth, err := preparedCredentialAuthProof(*c, s.openSecret)
	if err != nil || proof == "" {
		t.Fatal("invalid retained fixture source", err)
	}
	s.credentialValues = map[string]string{proof: "test-upstream-secret"}
	s.credentialAuthProofs = map[string]string{proof: auth}
	data.Connections[0].Protocol = protocol
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	routes, err := s.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	s.runtime.routes.Store(&runtimeRoutes{ID: "cfg_source_holders", Models: routes, PublishedAt: time.Now()})
	key, err := credentialHolderKey(*c)
	if err != nil {
		t.Fatal(err)
	}
	return s, bearer, key, data
}

func sourceHolderCount(t *testing.T, s *Service, key credentialSourceKey) int {
	t.Helper()
	s.credentialSources.mu.Lock()
	defer s.credentialSources.mu.Unlock()
	state := s.credentialSources.states[physicalHolderKey(key)]
	if state == nil {
		return 0
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.holders
}

func sourceHolderCall(ctx context.Context, s *Service, bearer, protocol string, stream bool) (*GatewayResult, error) {
	streamField := ""
	if stream {
		streamField = `,"stream":true`
	}
	switch protocol {
	case entity.ProtocolOpenAIResponses:
		return s.GatewayResponses(ctx, bearer, []byte(`{"model":"public-model","input":"hello"`+streamField+`}`), "req_holder")
	case entity.ProtocolAnthropicMessages:
		return s.GatewayMessages(ctx, bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"max_tokens":8`+streamField+`}`), "req_holder", MessagesHeaders{Version: "2023-06-01"})
	case entity.ProtocolGeminiGenerateContent:
		return s.GatewayGemini(ctx, bearer, []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`), "req_holder", "public-model", stream)
	default:
		return s.GatewayChat(ctx, bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]`+streamField+`}`), "req_holder")
	}
}

func TestCredentialSourceHolderNativeBodiesRemainHeldUntilClose(t *testing.T) {
	for _, protocol := range []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		for _, stream := range []bool{false, true} {
			t.Run(protocol+map[bool]string{false: "/buffered", true: "/SSE"}[stream], func(t *testing.T) {
				var calls atomic.Int32
				finish := make(chan struct{})
				var once sync.Once
				defer once.Do(func() { close(finish) })
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
					} else {
						w.Header().Set("Content-Type", "application/json")
					}
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
					select {
					case <-finish:
					case <-r.Context().Done():
						return
					}
					if stream {
						_, _ = io.WriteString(w, "data: {}\n\ndata: [DONE]\n\n")
					} else {
						_, _ = io.WriteString(w, `{}`)
					}
				}))
				defer func() { once.Do(func() { close(finish) }); server.Close() }()
				s, bearer, key, _ := sourceHolderRuntime(t, server.URL, protocol)
				s.secrets = nil // Published inference must not resolve or log in to Vault.
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				result, err := sourceHolderCall(ctx, s, bearer, protocol, stream)
				if err != nil || result == nil || result.Response == nil {
					t.Fatal("native dispatch failed", err)
				}
				if result.CredentialID != key.CredentialID || result.SnapshotID != "cfg_source_holders" || result.AttemptID == "" || sourceHolderCount(t, s, key) != 1 {
					t.Fatal("active native body lost exact source/Attempt ownership")
				}
				cancelDrain, done := context.WithCancel(context.Background())
				done()
				if !errors.Is(s.credentialSources.closeAndWait(cancelDrain, key), context.Canceled) {
					t.Fatal("held body falsely drained")
				}
				blocked, blockedErr := sourceHolderCall(context.Background(), s, bearer, protocol, stream)
				if blockedErr == nil || blocked.AttemptID != "" || len(blocked.Attempts) != 0 || calls.Load() != 1 {
					t.Fatal("closed source dispatched a new Attempt")
				}
				once.Do(func() { close(finish) })
				if _, err := io.ReadAll(result.Response.Body); err != nil {
					t.Fatal(err)
				}
				cancel()
				if sourceHolderCount(t, s, key) != 1 {
					t.Fatal("EOF or cancellation released a readable native body")
				}
				if err := result.Response.Body.Close(); err != nil {
					t.Fatal(err)
				}
				_ = result.Response.Body.Close()
				if sourceHolderCount(t, s, key) != 0 || s.credentialSources.closeAndWait(context.Background(), key) != nil {
					t.Fatal("Body.Close did not join source exactly once")
				}
			})
		}
	}
}

func TestCredentialSourceHolderClosureBeforeAttemptStart(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	s, bearer, key, _ := sourceHolderRuntime(t, server.URL, entity.ProtocolOpenAIChat)
	s.afterGatewayAdmission = func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if !errors.Is(s.credentialSources.closeAndWait(ctx, key), context.Canceled) {
			t.Fatal("prepared holder not retained")
		}
	}
	result, err := sourceHolderCall(context.Background(), s, bearer, entity.ProtocolOpenAIChat, false)
	if err == nil || result.AttemptID != "" || len(result.Attempts) != 0 || calls.Load() != 0 || sourceHolderCount(t, s, key) != 0 {
		t.Fatal("closed reservation crossed final Attempt start")
	}
}

func TestCredentialSourceHolderPreparationAndBadResponsesRelease(t *testing.T) {
	for _, status := range []int{401, 500, 200} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "untrusted")
			}))
			defer server.Close()
			s, bearer, key, _ := sourceHolderRuntime(t, server.URL, entity.ProtocolOpenAIChat)
			result, err := sourceHolderCall(context.Background(), s, bearer, entity.ProtocolOpenAIChat, false)
			if err == nil || result.Response != nil || sourceHolderCount(t, s, key) != 0 {
				t.Fatal("failed native response leaked holder")
			}
		})
	}
	s, bearer, key, _ := sourceHolderRuntime(t, "https://provider-one.example/v1", entity.ProtocolOpenAIChat)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = sourceHolderCall(ctx, s, bearer, entity.ProtocolOpenAIChat, false)
	if sourceHolderCount(t, s, key) != 0 {
		t.Fatal("cancelled preparation leaked holder")
	}
}

func TestCredentialSourceHolderAvailabilityAndFilteredPlanOwnAllCandidates(t *testing.T) {
	s, _, key, _ := sourceHolderRuntime(t, "https://provider-one.example/v1", entity.ProtocolOpenAIChat)
	plan, err := s.gatewayAttemptPlan("mdl_one", entity.ProtocolOpenAIChat)
	if err != nil {
		t.Fatal(err)
	}
	filtered, err := plan.filtered(plan.Candidates())
	if err != nil {
		t.Fatal(err)
	}
	if sourceHolderCount(t, s, key) != 1 {
		t.Fatal("filtered plan cloned source ownership")
	}
	filtered.releaseSources()
	plan.releaseSources()
	if sourceHolderCount(t, s, key) != 0 {
		t.Fatal("availability or filtered plan leaked source")
	}
}

func TestCredentialSourceHolderLogicalIdentityAndRootRewrap(t *testing.T) {
	s, f, _ := credentialStorageSQLService(t)
	op, err := s.createVaultCredential(context.Background(), "usr_admin", "11111111-1111-4111-8111-111111111111", "private-value", credentialStorageFixtureIntent(t, s, f, "credential"))
	if err != nil {
		t.Fatal(err)
	}
	rows := []entity.ProviderCredential{f.data.credentials[op.CredentialID]}
	if err = attachCredentialSources(s.db, rows); err != nil {
		t.Fatal(err)
	}
	c := rows[0]
	key, err := credentialHolderKey(c)
	if err != nil {
		t.Fatal(err)
	}
	holder, err := s.acquireCredentialSource(c)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.release()
	c.VaultReference.ReaderCiphertext = "a different root envelope"
	other, err := credentialHolderKey(c)
	if err != nil || key != other {
		t.Fatal("root envelope changed logical source")
	}
	for _, change := range []func(*entity.ProviderCredential){
		func(c *entity.ProviderCredential) { c.ID += "x" }, func(c *entity.ProviderCredential) { c.CreatedAt = c.CreatedAt.Add(time.Microsecond) },
		func(c *entity.ProviderCredential) { c.VaultReference.ReferenceID = strings.Repeat("e", 32) }, func(c *entity.ProviderCredential) { c.VaultReference.ReaderGeneration = "reader_new" },
		func(c *entity.ProviderCredential) { c.VaultReference.ReaderMethod = "approle" },
		func(c *entity.ProviderCredential) { c.VaultReference.ExpectedMarkerSHA256 = strings.Repeat("e", 64) }, func(c *entity.ProviderCredential) { c.VaultReference.DescriptorSHA256 = strings.Repeat("e", 64) },
	} {
		copy := c
		ref := *c.VaultReference
		copy.VaultReference = &ref
		change(&copy)
		other, err := credentialHolderKey(copy)
		if err == nil && other == key {
			t.Fatal("different source aliased holder key")
		}
	}
}

func TestCredentialSourceHolderPausedVaultReadCannotRepopulateClosedSource(t *testing.T) {
	s, f, _ := credentialStorageSQLService(t)
	original, _ := url.Parse(f.vault.data.rev.Endpoint)
	entered, release := make(chan struct{}), make(chan struct{})
	var hold atomic.Bool
	var once sync.Once
	defer once.Do(func() { close(release) })
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hold.Load() && r.Method == http.MethodGet {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		request := r.Clone(r.Context())
		request.URL.Scheme, request.URL.Host = original.Scheme, original.Host
		request.RequestURI = ""
		response, err := http.DefaultTransport.RoundTrip(request)
		if err != nil {
			w.WriteHeader(502)
			return
		}
		defer func() { _ = response.Body.Close() }()
		for k, v := range response.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	defer func() { once.Do(func() { close(release) }); proxy.Close() }()
	f.vault.data.rev.Endpoint = proxy.URL
	op, err := s.createVaultCredential(context.Background(), "usr_admin", "11111111-1111-4111-8111-111111111111", "private-value", credentialStorageFixtureIntent(t, s, f, "credential"))
	if err != nil {
		t.Fatal(err)
	}
	rows := []entity.ProviderCredential{f.data.credentials[op.CredentialID]}
	if err = attachCredentialSources(s.db, rows); err != nil {
		t.Fatal(err)
	}
	c := rows[0]
	key, _ := credentialHolderKey(c)
	hold.Store(true)
	finished := make(chan error, 1)
	go func() {
		value, err := s.resolveCredential(context.Background(), c)
		if err == nil {
			err = s.cacheCredentialValue(context.Background(), c, value)
		}
		finished <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("actual Vault read not entered")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sourceHolderCount(t, s, key) != 1 || !errors.Is(s.credentialSources.closeAndWait(ctx, key), context.Canceled) {
		t.Fatal("finite Vault read not retained across close")
	}
	once.Do(func() { close(release) })
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("late Vault read repopulated closed source")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("finite read did not finish")
	}
	if sourceHolderCount(t, s, key) != 0 || len(s.credentialValues) != 0 {
		t.Fatal("finite preparer/cache holder leaked")
	}
	if _, err := s.resolveCredential(context.Background(), c); err == nil {
		t.Fatal("closed source admitted new Vault read")
	}
}

func TestCredentialSourceHolderFailoverReleasesRejectedAndUnusedCandidates(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":{"code":"invalid_api_key"}}`)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	s, bearer, first, data := sourceHolderRuntime(t, server.URL, entity.ProtocolOpenAIChat)
	c := data.Credentials[0]
	c.ID, c.Priority = "crd_secondary", 10
	ref := *c.VaultReference
	ref.CredentialID, ref.ReferenceID = c.ID, strings.Repeat("e", 32)
	ref.PhysicalObject = rootHash("secondary-fixture-object")
	c.VaultReference = &ref
	second, err := credentialHolderKey(c)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := preparedCredentialAuthProof(c, s.openSecret)
	if err != nil {
		t.Fatal(err)
	}
	s.credentialValues[second.Proof] = "test-secondary-secret"
	s.credentialAuthProofs[second.Proof] = auth
	data.Credentials = append(data.Credentials, c)
	data.Access = append(data.Access, entity.CredentialModelAccess{CredentialID: c.ID, ProviderModelID: "pmd_one"})
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	routes, err := s.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	s.runtime.routes.Store(&runtimeRoutes{ID: "cfg_source_holders", Models: routes, PublishedAt: time.Now()})
	result, err := sourceHolderCall(context.Background(), s, bearer, entity.ProtocolOpenAIChat, false)
	if err != nil || result.Response == nil || len(result.Attempts) != 1 || result.Attempts[0].CredentialID != first.CredentialID || result.CredentialID != second.CredentialID || calls.Load() != 2 {
		t.Fatal("failover lost original/final attribution", err)
	}
	if sourceHolderCount(t, s, first) != 0 || sourceHolderCount(t, s, second) != 1 {
		t.Fatal("failover did not transfer only selected source")
	}
	_ = result.Response.Body.Close()
	if sourceHolderCount(t, s, second) != 0 {
		t.Fatal("failover body leaked source")
	}
}

type sourceBlockingClose struct {
	entered, finish chan struct{}
	calls           atomic.Int32
}

func (b *sourceBlockingClose) Read([]byte) (int, error) { return 0, io.EOF }
func (b *sourceBlockingClose) Close() error {
	b.calls.Add(1)
	close(b.entered)
	<-b.finish
	return errors.New("controlled close failure")
}

func TestCredentialSourceHolderJoinsUnderlyingCloseBeforeRelease(t *testing.T) {
	var registry credentialSourceHolders
	key := credentialSourceKey{CredentialID: "credential", Proof: "logical", PhysicalObject: rootHash("credential")}
	holder, err := registry.acquire(key)
	if err != nil {
		t.Fatal(err)
	}
	raw := &sourceBlockingClose{entered: make(chan struct{}), finish: make(chan struct{})}
	body := holder.body(raw)
	finished := make(chan error, 2)
	go func() { finished <- body.Close() }()
	<-raw.entered
	go func() { finished <- body.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(registry.closeAndWait(ctx, key), context.Canceled) {
		t.Fatal("underlying Close did not hold ownership")
	}
	other, err := registry.acquire(credentialSourceKey{CredentialID: "independent", Proof: "other", PhysicalObject: rootHash("independent")})
	if err != nil {
		t.Fatal("one held source blocked another", err)
	}
	other.release()
	close(raw.finish)
	for range 2 {
		if err := <-finished; err == nil {
			t.Fatal("underlying Close error discarded")
		}
	}
	if raw.calls.Load() != 1 || !errors.Is(registry.closeAndWait(context.Background(), key), vaultUnavailable) {
		t.Fatal("double Close released before underlying close or leaked source")
	}
}

func TestCredentialSourceHolderTeamModelDiscoveryReleasesPlansWithoutVault(t *testing.T) {
	s, _, key, _ := sourceHolderRuntime(t, "https://provider-one.example/v1", entity.ProtocolOpenAIChat)
	auth := s.runtime.auth.Load()
	cookie := strings.Repeat("s", 43)
	expires := time.Now().Add(time.Hour)
	auth.TeamSessions = map[string]runtimeTeamSession{secret.SHA256Hex(cookie): {ID: "ses_team", UserID: "usr_one", TokenHash: secret.SHA256Hex(cookie), ExpiresAt: expires}}
	auth.Teams = map[string]runtimeTeam{"tem_one": {CreatedAt: time.Now().Add(-time.Hour), Members: map[string]string{"usr_one": "tmb_one"}, Models: map[string]bool{"mdl_one": true}}}
	identity, err := s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one")
	if err != nil {
		t.Fatal(err)
	}
	s.secrets = nil
	models, err := s.TeamGatewayModels(context.Background(), identity)
	if err != nil || len(models) != 1 || sourceHolderCount(t, s, key) != 0 {
		t.Fatal("Team availability leaked a detached plan", err)
	}
	if err = s.credentialSources.closeAndWait(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	models, err = s.TeamGatewayModels(context.Background(), identity)
	if err != nil || len(models) != 0 || sourceHolderCount(t, s, key) != 0 {
		t.Fatal("closed source remained available or read Vault", err)
	}
}

// More than the registry capacity of sequential live generations must not
// exhaust new supply once each open reservation has been released.
func TestCredentialSourceHolderOpenChurnReclaimsIdleStates(t *testing.T) {
	var registry credentialSourceHolders
	for i := range 5001 {
		key := credentialSourceKey{CredentialID: fmt.Sprintf("crd_churn_%d", i), Proof: "logical", PhysicalObject: rootHash(fmt.Sprintf("crd_churn_%d", i))}
		holder, err := registry.acquire(key)
		if err != nil {
			t.Fatalf("idle open sources exhausted new-source capacity at %d", i)
		}
		holder.release()
	}
	registry.mu.Lock()
	count := len(registry.states)
	registry.mu.Unlock()
	if count != 0 {
		t.Fatal("released open generations were retained", count)
	}
}

func TestCredentialSourceHolderRetainsClosedTombstonesAndCapacity(t *testing.T) {
	var registry credentialSourceHolders
	for i := range 5000 {
		key := credentialSourceKey{CredentialID: fmt.Sprintf("crd_closed_%d", i), Proof: "logical", PhysicalObject: rootHash(fmt.Sprintf("crd_closed_%d", i))}
		if err := registry.closeAndWait(context.Background(), key); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []credentialSourceKey{{CredentialID: "crd_closed_0", Proof: "logical", PhysicalObject: rootHash("crd_closed_0")}, {CredentialID: "crd_new", Proof: "logical", PhysicalObject: rootHash("crd_new")}} {
		if holder, err := registry.acquire(key); err == nil || holder != nil {
			t.Fatal("closed source or full tombstone bound reopened")
		}
	}
	registry.mu.Lock()
	count := len(registry.states)
	registry.mu.Unlock()
	if count != 5000 {
		t.Fatal("closed tombstones were pruned", count)
	}
}

func TestCredentialSourceHolderActiveGenerationSurvivesChurn(t *testing.T) {
	var registry credentialSourceHolders
	key := credentialSourceKey{CredentialID: "crd_active", Proof: "logical", PhysicalObject: rootHash("crd_active")}
	holder, err := registry.acquire(key)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 5001 {
		other, err := registry.acquire(credentialSourceKey{CredentialID: fmt.Sprintf("crd_other_%d", i), Proof: "logical", PhysicalObject: rootHash(fmt.Sprintf("crd_other_%d", i))})
		if err != nil {
			t.Fatal("idle churn exhausted active registry", err)
		}
		other.release()
	}
	registry.mu.Lock()
	state := registry.states[physicalHolderKey(key)]
	count := len(registry.states)
	registry.mu.Unlock()
	if count != 1 || state != holder.state || !holder.admitUse() {
		t.Fatal("live generation lost exact state")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(registry.closeAndWait(ctx, key), context.Canceled) {
		t.Fatal("active generation falsely drained")
	}
	if holder.admitUse() {
		t.Fatal("closed active generation re-admitted")
	}
	holder.release()
	if registry.closeAndWait(context.Background(), key) != nil {
		t.Fatal("closed active generation not joined")
	}
	if other, err := registry.acquire(key); err == nil || other != nil {
		t.Fatal("released closed generation reopened")
	}
}

func TestCredentialSourceHolderConcurrentAcquireReleaseClose(t *testing.T) {
	for range 100 {
		var registry credentialSourceHolders
		key := credentialSourceKey{CredentialID: "crd_race", Proof: "logical", PhysicalObject: rootHash("crd_race")}
		original, err := registry.acquire(key)
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		result := make(chan error, 3)
		go func() { <-start; original.release(); original.release(); result <- nil }()
		go func() {
			<-start
			h, e := registry.acquire(key)
			if e == nil {
				h.release()
			}
			result <- nil
		}()
		go func() {
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			result <- registry.closeAndWait(ctx, key)
		}()
		close(start)
		for range 3 {
			if err := <-result; err != nil {
				t.Fatal("concurrent close failed to join", err)
			}
		}
		registry.mu.Lock()
		state := registry.states[physicalHolderKey(key)]
		count := len(registry.states)
		registry.mu.Unlock()
		if state == nil || count != 1 {
			t.Fatal("closed state disappeared")
		}
		state.mu.Lock()
		closed, holders := state.closed, state.holders
		state.mu.Unlock()
		if !closed || holders != 0 {
			t.Fatal("closed state did not retain exact drain", closed, holders)
		}
		if h, e := registry.acquire(key); e == nil || h != nil {
			t.Fatal("concurrent final release reopened closure")
		}
	}
}
