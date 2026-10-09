package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestProviderStatusDisabledDiscoveryAndDispatch(t *testing.T) {
	for _, protocol := range []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		t.Run(protocol, func(t *testing.T) {
			s, data, _ := runtimeFixture(t, "https://api.example.invalid/v1")
			data.Connections[0].Protocol = protocol
			routes, err := s.buildRuntimeRoutes(data)
			if err != nil {
				t.Fatal(err)
			}
			s.runtime.routes.Store(&runtimeRoutes{ID: "cfg_status", Models: routes})
			route := routes["mdl_one"][0].Route
			route.SnapshotID = "cfg_status"
			if !s.runtimeRouteReadyForDiscovery(s.runtime.auth.Load(), routes["mdl_one"][0]) {
				t.Fatal("enabled supply not eligible")
			}
			release, ok := s.pinConnectionDispatch(&route)
			if !ok {
				t.Fatal("enabled dispatch refused")
			}
			release()
			release()
			// Synchronous denial stops old published supply even before refresh.
			s.runtime.deniedProviders.Store("prv_one", s.runtime.epoch.Add(1))
			if s.runtimeRouteReadyForDiscovery(s.runtime.auth.Load(), routes["mdl_one"][0]) {
				t.Fatal("stale discovery exposed")
			}
			if _, _, err := s.runtimeProtocolRoute("mdl_one", protocol); err == nil {
				t.Fatal("stale selection eligible")
			}
			if release, ok := s.pinConnectionDispatch(&route); ok {
				release()
				t.Fatal("prepared dispatch admitted after disable")
			}
			data.Providers[0].Enabled = false
			s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
			clearRuntimeTombstones(&s.runtime.deniedProviders, s.runtime.epoch.Load())
			if s.runtimeConnectionAllowed(s.runtime.auth.Load(), route) {
				t.Fatal("publication restored disabled Provider")
			}
			data.Providers[0].Enabled = true
			data.Providers[0].CreatedAt = data.Providers[0].CreatedAt.Add(time.Second)
			s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
			if s.runtimeConnectionAllowed(s.runtime.auth.Load(), route) {
				t.Fatal("old incarnation restored")
			}
			if !data.Connections[0].Enabled || data.Credentials[0].Enabled != true || data.ProviderModels[0].Disabled || data.Bindings[0].Weight != 100 {
				t.Fatal("child state changed")
			}
		})
	}
}
func TestProviderStatusPublicationGateHasNoRemoteLifetime(t *testing.T) {
	s, data, _ := runtimeFixture(t, "https://api.example.invalid/v1")
	route := s.runtime.routes.Load().Models["mdl_one"][0].Route
	route.SnapshotID = "cfg_test"
	release, ok := s.pinConnectionDispatch(&route)
	if !ok {
		t.Fatal("dispatch not admitted")
	}
	attempted := make(chan struct{})
	committed := make(chan struct{})
	go func() {
		close(attempted)
		s.runtime.publication.Lock()
		data.Providers[0].Enabled = false
		s.runtime.deniedProviders.Store(route.ProviderID, s.runtime.epoch.Add(1))
		s.runtime.publication.Unlock()
		close(committed)
	}()
	<-attempted
	if s.runtime.publication.TryLock() {
		s.runtime.publication.Unlock()
		t.Fatal("status crossed held local admission")
	}
	// Remote request/stream may continue after this local release; status commit
	// is no longer held until response completion.
	release()
	<-committed
	if s.runtimeConnectionAllowed(s.runtime.auth.Load(), route) {
		t.Fatal("future dispatch still allowed")
	}
}

func TestProviderStatusDisableBeforeFinalDispatchCreatesNoAttempt(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = io.WriteString(w, `{"choices":[]}`) }))
	defer server.Close()
	svc, _, bearer := runtimeFixture(t, server.URL+"/v1")
	svc.afterGatewayAdmission = func() {
		svc.runtime.publication.Lock()
		svc.runtime.deniedProviders.Store("prv_one", svc.runtime.epoch.Add(1))
		svc.runtime.publication.Unlock()
	}
	result, err := svc.GatewayChat(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"held prepared request"}]}`), "req_provider_before_dispatch")
	if err == nil || calls.Load() != 0 || result == nil || result.AttemptID != "" || len(result.Attempts) != 0 {
		t.Fatalf("prepared reservation bypassed disable: calls=%d result=%+v err=%v", calls.Load(), result, err)
	}
}
func TestProviderStatusAlreadyReceivedAttemptKeepsIdentity(t *testing.T) {
	received := make(chan struct{})
	finish := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(received)
		<-finish
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"provider-model","choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
	}))
	defer server.Close()
	svc, _, bearer := runtimeFixture(t, server.URL+"/v1")
	type observation struct {
		result *GatewayResult
		err    error
	}
	done := make(chan observation, 1)
	go func() {
		result, err := svc.GatewayChat(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"already received"}]}`), "req_provider_inflight")
		done <- observation{result, err}
	}()
	<-received
	if !svc.runtime.publication.TryLock() {
		close(finish)
		<-done
		t.Fatal("remote response held global publication gate")
	}
	svc.runtime.deniedProviders.Store("prv_one", svc.runtime.epoch.Add(1))
	svc.runtime.publication.Unlock()
	close(finish)
	got := <-done
	if got.err != nil || got.result == nil || got.result.Response == nil {
		t.Fatal("in-flight work aborted", got.err)
	}
	defer func() { _ = got.result.Response.Body.Close() }()
	body, readErr := io.ReadAll(got.result.Response.Body)
	if readErr != nil || !strings.Contains(string(body), `"finish_reason":"stop"`) {
		t.Fatal("received attempt response did not finish", string(body), readErr)
	}
	if got.result.ConnectionID != "con_one" || got.result.CredentialID != "crd_one" || got.result.SnapshotID != "cfg_test" || got.result.AttemptID == "" {
		t.Fatalf("captured attempt attribution changed: %+v", got.result)
	}
	if _, _, err := svc.runtimeProtocolRoute("mdl_one", entity.ProtocolOpenAIChat); err == nil {
		t.Fatal("future selection allowed")
	}
}

func TestProviderStatusMissingBirthRevisionAndProofFailClosed(t *testing.T) {
	for _, kind := range []string{"missing", "unknown_birth", "unknown_revision", "disabled", "borrowed_provider", "revised", "reborn", "tombstone"} {
		t.Run(kind, func(t *testing.T) {
			s, data, _ := runtimeFixture(t, "https://example.invalid/v1")
			route := s.runtime.routes.Load().Models["mdl_one"][0].Route
			switch kind {
			case "missing":
				data.Providers = nil
			case "unknown_birth":
				data.Providers[0].CreatedAt = time.Time{}
			case "unknown_revision":
				data.Providers[0].ETag = ""
			case "disabled":
				data.Providers[0].Enabled = false
			case "borrowed_provider":
				data.Providers[0].ID = "prv_foreign"
			case "revised":
				data.Providers[0].ETag = "rev_new"
			case "reborn":
				data.Providers[0].CreatedAt = data.Providers[0].CreatedAt.Add(time.Microsecond)
			case "tombstone":
				s.runtime.deniedProviders.Store("prv_one", s.runtime.epoch.Add(1))
			}
			s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
			if s.runtimeConnectionAllowed(s.runtime.auth.Load(), route) {
				t.Fatal("queued route borrowed or inferred Provider eligibility", kind)
			}
		})
	}
}

func TestProviderStatusReenableRequiresCurrentRouteAndKeepsChildren(t *testing.T) {
	s, data, _ := runtimeFixture(t, "https://example.invalid/v1")
	old := s.runtime.routes.Load().Models["mdl_one"][0].Route
	data.Providers[0].Enabled = false
	data.Providers[0].ETag = "rev_disabled"
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	if s.runtimeConnectionAllowed(s.runtime.auth.Load(), old) {
		t.Fatal("disabled Provider remained ready")
	}
	data.Providers[0].Enabled = true
	data.Providers[0].ETag = "rev_enabled"
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	if s.runtimeConnectionAllowed(s.runtime.auth.Load(), old) {
		t.Fatal("enable revived queued old Provider revision")
	}
	routes, err := s.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	if !s.runtimeConnectionAllowed(s.runtime.auth.Load(), routes["mdl_one"][0].Route) {
		t.Fatal("current independently eligible route not restored")
	}
	data.Connections[0].Enabled = false
	routes, err = s.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	if s.runtimeConnectionAllowed(s.runtime.auth.Load(), routes["mdl_one"][0].Route) || data.Connections[0].Enabled {
		t.Fatal("Provider enable changed child enablement")
	}
	if !data.Credentials[0].Enabled || data.ProviderModels[0].Disabled || data.Bindings[0].Weight != 100 {
		t.Fatal("enable mutated independent supply facts")
	}
}

func TestProviderStatusTombstoneCoversAllOwnConnectionsOnly(t *testing.T) {
	s, data, _ := runtimeFixture(t, "https://example.invalid/v1")
	p := data.Providers[0]
	p.ID = "prv_other"
	data.Providers = append(data.Providers, p)
	c := data.Connections[0]
	c.ID = "con_second"
	data.Connections = append(data.Connections, c)
	c.ID, c.ProviderID = "con_other", p.ID
	data.Connections = append(data.Connections, c)
	auth := buildRuntimeAuthorization(data, time.Now().Add(time.Minute))
	base := s.runtime.routes.Load().Models["mdl_one"][0].Route
	s.runtime.deniedProviders.Store("prv_one", s.runtime.epoch.Add(1))
	for _, c := range data.Connections {
		route := base
		route.ConnectionID, route.ProviderID = c.ID, c.ProviderID
		if got := s.runtimeConnectionAllowed(auth, route); got != (c.ProviderID == "prv_other") {
			t.Fatal("Provider disable leaked across scope or missed child", c.ID, got)
		}
	}
}

func TestProviderStatusCurrentApplicationIncludesEmptyProvider(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		for _, kind := range []string{"current", "wrong_birth", "wrong_revision", "wrong_status", "wrong_epoch", "wrong_digest", "expired", "tombstone", "stopped", "runtime_busy", "publication_busy"} {
			t.Run(kind+"/"+map[bool]string{true: "enabled", false: "disabled"}[enabled], func(t *testing.T) {
				s, data, _ := runtimeFixture(t, "https://example.invalid/v1")
				data.Connections, data.ProviderModels, data.Credentials, data.Access, data.Bindings = nil, nil, nil, nil, nil
				data.Providers[0].Enabled = enabled
				s.runtime.done = make(chan struct{})
				digest, err := runtimeDigest(data)
				if err != nil {
					t.Fatal(err)
				}
				auth := buildRuntimeAuthorization(data, time.Now().Add(time.Minute))
				auth.SourceDigest = digest
				s.runtime.auth.Store(auth)
				s.runtime.routes.Store(&runtimeRoutes{ID: "cfg_empty_provider", Digest: digest, Models: map[string][]runtimeRoute{}})
				row := data.Providers[0]
				switch kind {
				case "wrong_birth":
					row.CreatedAt = row.CreatedAt.Add(time.Microsecond)
				case "wrong_revision":
					row.ETag = "rev_other"
				case "wrong_status":
					row.Enabled = !row.Enabled
				case "wrong_epoch":
					s.runtime.epoch.Add(1)
				case "wrong_digest":
					s.runtime.routes.Load().Digest = "other"
				case "expired":
					auth.ValidUntil = time.Now().Add(-time.Second)
				case "tombstone":
					s.runtime.deniedProviders.Store(row.ID, uint64(1))
				case "stopped":
					close(s.runtime.done)
				case "runtime_busy":
					s.runtime.mu.Lock()
					defer s.runtime.mu.Unlock()
				case "publication_busy":
					s.runtime.publication.Lock()
					defer s.runtime.publication.Unlock()
				}
				if got := s.providerStatusRuntimeApplied(digest, 0, row); got != (kind == "current") {
					t.Fatal("empty Provider application inferred", kind, got)
				}
			})
		}
	}
}

func TestProviderStatusMemberMetadataBindsInternalEligibilityWithoutNames(t *testing.T) {
	s, data, source := memberModelsProjectionFixture(t)
	data.ProvidersRead = false
	before := s.projectMemberModels("usr_reader", data)
	if before.PersonalModels[0].Availability != "ready" || before.PersonalModels[0].Providers != nil {
		t.Fatal("directory permission changed route eligibility", before)
	}
	data.Providers = slices.Clone(data.Providers)
	source.Providers[0].Enabled = false
	source.Providers[0].ETag = "rev_disabled"
	data.Providers[0] = source.Providers[0]
	digest, err := runtimeDigest(source)
	if err != nil {
		t.Fatal(err)
	}
	auth := buildRuntimeAuthorization(source, time.Now().Add(time.Minute))
	auth.SourceDigest = digest
	routes, err := s.buildRuntimeRoutes(source)
	if err != nil {
		t.Fatal(err)
	}
	s.runtime.auth.Store(auth)
	s.runtime.routes.Store(&runtimeRoutes{ID: "cfg_provider_disabled", Digest: digest, Models: routes})
	after := s.projectMemberModels("usr_reader", data)
	if after.ETag == before.ETag || after.PersonalModels[0].Availability != "unavailable" || len(after.PersonalModels[0].Protocols) != 0 || after.PersonalModels[0].Providers != nil {
		t.Fatal("disabled unnamed Provider still eligible", after)
	}
	raw, _ := json.Marshal(after)
	for _, private := range []string{"Provider One", "prv_one", "rev_disabled", "provider_birth", "provider_revision"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("internal Provider proof exposed", private)
		}
	}
}

func TestProviderStatusWeightRollbackAndPublicationUseCurrentProvider(t *testing.T) {
	st := weightTestState()
	actor := entity.User{ID: "usr_reader", CreatedAt: st.Model.CreatedAt}
	raw, digest, err := modelWeightSnapshot(st.Rows)
	if err != nil {
		t.Fatal(err)
	}
	target := entity.ModelWeightVersion{ID: "mwv_01m36yee4gkbns18pfcqqc75a3", ModelID: st.Model.ID, ModelBirth: modelWeightBirth(st.Model.CreatedAt), Snapshot: raw, SnapshotDigest: digest, ValidWeightSet: true}
	initial := modelWeightReview(actor, true, st, target, st.Rows, nil)
	if !initial.Eligible {
		t.Fatal(initial)
	}
	st.Providers[0].Enabled = false
	st.Providers[0].ETag = "rev_disabled"
	denied := modelWeightReview(actor, true, st, target, st.Rows, nil)
	if denied.Eligible || !slices.Contains(denied.BlockerCodes, "positive_route_unavailable") || denied.ReviewETag == initial.ReviewETag {
		t.Fatal("disabled Provider rollback remained reviewed executable", denied)
	}
	st.Providers[0].Enabled = true
	st.Providers[0].ETag = "rev_enabled"
	svc, auth, _ := weightTestRuntime(st)
	if !svc.modelWeightRuntimeApplied(st) {
		t.Fatal("current enabled Provider publication rejected")
	}
	proof := auth.Providers[st.Providers[0].ID]
	proof.Revision = "rev_stale"
	auth.Providers[st.Providers[0].ID] = proof
	if svc.modelWeightRuntimeApplied(st) {
		t.Fatal("stale Provider publication reported current weights")
	}
}

// Exercise the real selector and GORM scan without a live database. The adapter
// rejects the nonexistent logical spelling and returns the physical alias facts.
type providerStatusSelectorSQLFixture struct {
	route      gatewayRoute
	queries    []string
	credential entity.ProviderCredential
}

type providerStatusSelectorSQLConnector struct {
	fixture *providerStatusSelectorSQLFixture
}

func (c providerStatusSelectorSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return providerStatusSelectorSQLConnection(c), nil
}
func (c providerStatusSelectorSQLConnector) Driver() driver.Driver {
	return providerStatusSelectorSQLDriver(c)
}

type providerStatusSelectorSQLDriver providerStatusSelectorSQLConnector

func (d providerStatusSelectorSQLDriver) Open(string) (driver.Conn, error) {
	return providerStatusSelectorSQLConnection(d), nil
}

type providerStatusSelectorSQLConnection struct {
	fixture *providerStatusSelectorSQLFixture
}

func (providerStatusSelectorSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected selector prepared statement")
}
func (providerStatusSelectorSQLConnection) Close() error { return nil }
func (providerStatusSelectorSQLConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("read-only selector unexpectedly began a transaction")
}
func (c providerStatusSelectorSQLConnection) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.fixture.queries = append(c.fixture.queries, query)
	switch {
	case strings.Contains(query, "model_provider_bindings AS b"):
		if strings.Contains(query, "pr.etag") || !strings.Contains(query, "pr.e_tag AS provider_revision") ||
			!strings.Contains(query, "pr.created_at AS provider_birth") || !strings.Contains(query, "pr.enabled AS provider_enabled") {
			return nil, errors.New("selector lost the physical Provider eligibility columns or scan aliases")
		}
		return effectiveSQLRows([]gatewayRoute{c.fixture.route})
	case strings.Contains(query, "provider_credentials AS c"):
		return effectiveSQLRows([]entity.ProviderCredential{c.fixture.credential})
	default:
		return nil, errors.New("selector escaped its bounded route and credential reads")
	}
}

func TestProviderStatusDatabaseSelectorScansExactProviderProof(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		name := "enabled"
		if !enabled {
			name = "disabled"
		}
		t.Run(name, func(t *testing.T) {
			birth := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			fixture := &providerStatusSelectorSQLFixture{
				route: gatewayRoute{
					BindingID: "bnd_selector", Weight: 100, ProviderModelID: "pmd_selector",
					ConnectionID: "con_selector", ConnectionEnabled: true, ConnectionBirth: birth,
					ProviderID: "prv_selector", ProviderBirth: birth, ProviderEnabled: enabled,
					ProviderRevision: "rev_selector", Protocol: entity.ProtocolOpenAIChat,
				},
				credential: entity.ProviderCredential{ID: "crd_selector", ConnectionID: "con_selector", Enabled: true, VerificationStatus: "verified"},
			}
			pool := sql.OpenDB(providerStatusSelectorSQLConnector{fixture: fixture})
			t.Cleanup(func() { _ = pool.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			route, err := selectGatewayProtocolRoute(db, "mdl_selector", entity.ProtocolOpenAIChat)
			if enabled {
				if err != nil || route == nil || len(fixture.queries) != 2 {
					t.Fatal("enabled database route failed", route, err, fixture.queries)
				}
				if route.ProviderID != fixture.route.ProviderID || !route.ProviderBirth.Equal(birth) ||
					!route.ProviderEnabled || route.ProviderRevision != fixture.route.ProviderRevision || route.CredentialID != fixture.credential.ID {
					t.Fatal("Provider physical columns did not scan into exact route authority", route)
				}
			} else if err == nil || route != nil || len(fixture.queries) != 1 {
				t.Fatal("disabled Provider admitted route or read a credential", route, err, fixture.queries)
			}
		})
	}
}

func providerStatusStoppedRuntimeFacts(s *Service) (*runtimeAuthorization, *runtimeRoutes, *RuntimeStatus) {
	auth := &runtimeAuthorization{SourceDigest: "prior_source", ValidUntil: time.Now().Add(time.Minute)}
	routes := &runtimeRoutes{ID: "cfg_prior", Digest: auth.SourceDigest}
	status := &RuntimeStatus{Enabled: true, Ready: true, SnapshotID: routes.ID}
	s.runtime.auth.Store(auth)
	s.runtime.routes.Store(routes)
	s.runtime.status.Store(status)
	s.runtime.epoch.Store(13)
	s.runtime.deniedProviders.Store("prv_target", uint64(13))
	s.runtime.deniedCredentials.Store("crd_target", uint64(13))
	s.runtime.deniedModels.Store("mdl_target", uint64(13))
	return auth, routes, status
}

func assertProviderStatusStoppedRuntimeFacts(t *testing.T, s *Service, auth *runtimeAuthorization, routes *runtimeRoutes, status *RuntimeStatus) {
	t.Helper()
	if s.runtime.auth.Load() != auth || s.runtime.routes.Load() != routes || s.runtime.status.Load() != status || s.runtime.epoch.Load() != 13 {
		t.Fatal("stopped refresh changed publication or failure metadata")
	}
	if !runtimeDenied(&s.runtime.deniedProviders, "prv_target") ||
		!runtimeDenied(&s.runtime.deniedCredentials, "crd_target") ||
		!runtimeDenied(&s.runtime.deniedModels, "mdl_target") {
		t.Fatal("stopped refresh cleared a committed revocation barrier")
	}
}

func TestProviderStatusStoppedRefreshNeverBorrowsDatabase(t *testing.T) {
	// There is deliberately no database: crossing the stopped pre-read gate
	// would borrow it and panic instead of preserving these publication facts.
	s := &Service{runtime: &gatewayRuntime{done: make(chan struct{})}}
	auth, routes, status := providerStatusStoppedRuntimeFacts(s)
	close(s.runtime.done)
	if err := s.RefreshRuntime(context.Background()); err != runtimeUnavailable {
		t.Fatal("stopped publisher was refreshed", err)
	}
	assertProviderStatusStoppedRuntimeFacts(t, s, auth, routes, status)
}

func TestProviderStatusStopDuringDatabaseLoadCannotPublish(t *testing.T) {
	for _, failRead := range []bool{false, true} {
		name := "complete_read"
		if failRead {
			name = "failed_read"
		}
		t.Run(name, func(t *testing.T) {
			s, fixture, _, _ := providerMetadataSQLService(t)
			auth, routes, status := providerStatusStoppedRuntimeFacts(s)
			closed := false
			if err := s.db.Callback().Query().After("gorm:query").Register("test:provider_runtime_stop_during_load", func(tx *gorm.DB) {
				table := "reservation_bounds"
				if failRead {
					table = "providers"
				}
				if tx.Statement.Table == table && !closed {
					closed = true
					close(s.runtime.done)
					if failRead {
						_ = tx.AddError(errors.New("controlled snapshot read failure after publisher stopped"))
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			if err := s.RefreshRuntime(context.Background()); err != runtimeUnavailable || !closed || len(fixture.queries) == 0 {
				t.Fatal("controlled in-read stop was not rejected", err, closed, fixture.queries)
			}
			assertProviderStatusStoppedRuntimeFacts(t, s, auth, routes, status)
			if len(fixture.writes) != 0 || len(fixture.data.audits) != 0 {
				t.Fatal("stopped refresh persisted publication or failure effects", fixture.writes, fixture.data.audits)
			}
		})
	}
}

func TestProviderStatusLegacyNilDoneRefreshIsNotStopped(t *testing.T) {
	s, fixture, _, _ := providerMetadataSQLService(t)
	s.runtime.done = nil
	if err := s.RefreshRuntime(context.Background()); err != nil || len(fixture.queries) == 0 || s.runtime.auth.Load() == nil || s.runtime.routes.Load() == nil {
		t.Fatal("legacy nil channel was treated as stopped", err)
	}
}

func TestProviderStatusStoppedRuntimeReplacementRetryConfirmsCurrentOnly(t *testing.T) {
	s, fixture, _, state := providerMetadataSQLService(t)
	originalConnection := state.base.row
	reviewed, err := s.GetProviderStatus(context.Background(), "usr_admin", state.base.provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	close(s.runtime.done)
	input := ProviderStatusInput{Enabled: false, Reason: "Reviewed disable"}
	result, err := s.WriteProviderStatus(context.Background(), "usr_admin", reviewed.ID, reviewed.ETag, input)
	if result != nil || err != providerMetadataUnavailable || state.base.provider.Enabled || state.base.provider.ETag == "0" ||
		len(fixture.data.audits) != 1 || !runtimeDenied(&s.runtime.deniedProviders, reviewed.ID) {
		t.Fatal("closed publication lost durable disabled intent or its denial", result, err)
	}
	committed := state.base.provider
	audit, err := json.Marshal(fixture.data.audits[0])
	if err != nil {
		t.Fatal(err)
	}
	result, err = s.WriteProviderStatus(context.Background(), "usr_admin", reviewed.ID, reviewed.ETag, input)
	if result != nil || err != providerMetadataUnavailable || state.base.provider != committed || len(fixture.data.audits) != 1 || !runtimeDenied(&s.runtime.deniedProviders, reviewed.ID) {
		t.Fatal("stopped identical retry changed committed intent", result, err)
	}
	s.runtime = &gatewayRuntime{done: make(chan struct{})}
	result, err = s.WriteProviderStatus(context.Background(), "usr_admin", reviewed.ID, reviewed.ETag, input)
	if err != nil || result == nil || !result.RuntimeApplied || result.Changed || result.Provider.Enabled ||
		state.base.provider != committed || state.base.row != originalConnection || len(fixture.data.audits) != 1 || runtimeDenied(&s.runtime.deniedProviders, reviewed.ID) {
		t.Fatal("fresh runtime failed current-only identical retry", result, err)
	}
	currentAudit, err := json.Marshal(fixture.data.audits[0])
	if err != nil || string(currentAudit) != string(audit) {
		t.Fatal("current-only retry rewrote original audit", err)
	}
	proof, ok := s.runtime.auth.Load().Providers[reviewed.ID]
	if !ok || proof.Enabled || proof.Revision != committed.ETag || !proof.Birth.Equal(committed.CreatedAt) {
		t.Fatal("fresh runtime did not publish exact current disabled Provider proof", proof)
	}
}
