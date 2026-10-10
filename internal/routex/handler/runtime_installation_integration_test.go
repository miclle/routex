package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testRuntimeInstallationLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	root := bytes.Repeat([]byte{131}, 32)
	defer clear(root)
	store, err := secretstore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	var dispatches atomic.Int64
	var hold atomic.Bool
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer installation-controlled" || r.Header.Get("Cookie") != "" {
			t.Error("unexpected controlled native dispatch")
			w.WriteHeader(400)
			return
		}
		dispatches.Add(1)
		if hold.Load() {
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"installation-native","object":"chat.completion","model":"installation-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer upstream.Close()
	defer unblock()
	privateDB, err := database.Open(ctx, db.Name(), os.Getenv("ROUTEX_TEST_"+strings.ToUpper(db.Name())+"_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	privateDB = privateDB.Session(&gorm.Session{Logger: logger.Discard})
	pool, err := privateDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()
	svc, err := service.New(ctx, privateDB, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"installation@example.invalid","password":"installation-test-password","name":"Installation administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	member, memberCookie, memberCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "installation-member", nil)
	_, readerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "installation-reader", []string{"system.read"})
	_, writerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "installation-writer", []string{"system.write"})
	fresh := func(prefix string) string {
		t.Helper()
		v, e := id.NewPrefixed(prefix)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	provider, connection, credential, pm, model, binding, keyID, team := fresh("prv"), fresh("con"), fresh("crd"), fresh("pmd"), fresh("mdl"), fresh("bnd"), fresh("key"), fresh("tem")
	birth := time.Now().UTC().Truncate(time.Microsecond)
	cipher, err := store.Seal(credential, "installation-controlled")
	if err != nil {
		t.Fatal(err)
	}
	bearer := "rx_" + strings.Repeat("i", 43)
	for _, row := range []any{
		&entity.Provider{ID: provider, Name: "Installation provider", Enabled: true, ETag: "0", CreatedAt: birth},
		&entity.ProviderConnection{ID: connection, ProviderID: provider, Name: "Installation", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, Adapter: "native", Enabled: true, TransportGeneration: "0", CreatedAt: birth},
		&entity.ProviderCredential{ID: credential, ConnectionID: connection, Name: "Controlled", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified", VerifiedTransportGeneration: "0", CreatedAt: birth},
		&entity.ProviderModel{ID: pm, ConnectionID: connection, UpstreamName: "installation-upstream", CapabilityTransportGeneration: "0", CreatedAt: birth},
		&entity.CredentialModelAccess{CredentialID: credential, ProviderModelID: pm},
		&entity.Model{ID: model, Status: entity.ResourceActive, CreatedAt: birth},
		&entity.ModelName{Name: "installation-model", ModelID: model, CurrentModelID: &model, CreatedAt: birth},
		&entity.ModelProviderBinding{ID: binding, ModelID: model, ProviderModelID: pm, Weight: 100, CreatedAt: birth},
		&entity.UserModelGrant{UserID: member.User.ID, ModelID: model},
		&entity.APIKey{ID: keyID, UserID: member.User.ID, Name: "Installation Key", Prefix: "rx_fixture", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive, CreatedAt: birth},
		&entity.APIKeyModel{KeyID: keyID, ModelID: model},
		&entity.Team{ID: team, Name: "Installation Team", Status: entity.ResourceActive},
		&entity.TeamMembership{ID: fresh("tmm"), TeamID: team, UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive},
		&entity.TeamMembership{ID: fresh("tmm"), TeamID: team, UserID: member.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive},
		&entity.TeamModelGrant{TeamID: team, ModelID: model},
	} {
		if db.Create(row).Error != nil {
			t.Fatal("build exact controlled fixture")
		}
	}
	// Begin with pre-existing accounting before the first runtime snapshot.
	// Otherwise the first genuine native call freezes AccountingStarted and
	// legitimately changes the route digest independently of authority reduction.
	accounting := db.WithContext(ctx).Model(&entity.QuotaSetting{}).Where("id = ?", 1).Update("AccountingStarted", true)
	if accounting.Error != nil || accounting.RowsAffected != 1 {
		t.Fatal("establish pre-existing accounting baseline")
	}
	var calendar entity.QuotaSetting
	if db.WithContext(ctx).Take(&calendar, "id = ?", 1).Error != nil || !calendar.AccountingStarted {
		t.Fatal("pre-existing accounting baseline absent")
	}
	if err := svc.StartSystemInstance(ctx, service.SystemInstanceMetadata{Name: "Installation", Hostname: "installation.invalid", Version: "test", GoVersion: "test", OS: "test", Arch: "test", StoragePath: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		svc.StopRuntime()
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
		stop, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		if err := svc.StopSystemInstance(stop); err != nil {
			t.Error(err)
		}
	}()
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, filepath.Join(t.TempDir(), "installation-calls.db")); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/admin/runtime/installations"
	req := systemStatusRequest(t, router, cookie, admin.CSRFToken)
	page := func() service.RuntimeInstallationPage {
		t.Helper()
		out := req("GET", path, nil)
		if out.Header().Get("Cache-Control") != "private, no-store" || out.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("installation privacy headers")
		}
		return decodeCatalogResponse[service.RuntimeInstallationPage](t, out, 200)
	}
	first := page()
	if first.Scope != "single_process_gateway_admission" || len(first.Items) != 1 || first.Items[0].ProjectionVersion != 1 || first.Items[0].CurrentServingInstallationMatches == nil || !*first.Items[0].CurrentServingInstallationMatches {
		t.Fatal("actual builder installation absent")
	}
	var public map[string]any
	out := req("GET", path, nil)
	if json.Unmarshal(out.Body.Bytes(), &public) != nil || len(public) != 4 || len(public["items"].([]any)[0].(map[string]any)) != 9 {
		t.Fatal("private installation fields exposed")
	}
	expectStatus(t, identityRequest(router, "GET", path, "", nil, ""), 401)
	expectStatus(t, identityRequest(router, "GET", path, "", readerCookie, ""), 200)
	expectStatus(t, identityRequest(router, "GET", path, "", writerCookie, ""), 403)
	expectStatus(t, req("POST", path, nil), 404)
	expectStatus(t, identityRequest(router, "GET", path, "{}", cookie, ""), 400)
	for _, q := range []string{"?unknown=1", "?limit=01", "?limit=1&limit=2", "?cursor=invalid", "?instance_id=" + strings.ToUpper(first.Items[0].InstanceID)} {
		expectStatus(t, req("GET", path+q, nil), 400)
	}
	var original []entity.RuntimeInstallationObservation
	if db.Session(&gorm.Session{QueryFields: true}).Order("id").Find(&original).Error != nil || len(original) != 1 {
		t.Fatal("first observation not durable")
	}
	for range 2 {
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var retained []entity.RuntimeInstallationObservation
	if db.Session(&gorm.Session{QueryFields: true}).Order("id").Find(&retained).Error != nil || !reflect.DeepEqual(original, retained) {
		t.Fatal("lease renewal rewrote first observation")
	}
	capturedTeam, err := svc.RuntimeAuthenticateTeamSession(ctx, memberCookie.Value, team)
	if err != nil {
		t.Fatal("genuine Team Session prerequisite")
	}
	if _, err := svc.AuthenticateAPIKey(ctx, bearer); err != nil {
		t.Fatal("Key prerequisite")
	}
	body := `{"model":"installation-model","messages":[{"role":"user","content":"controlled"}]}`
	teamRequest := httptest.NewRequestWithContext(ctx, "POST", "http://routex.test/api/v1/teams/"+team+"/chat/completions", strings.NewReader(body))
	teamRequest.AddCookie(memberCookie)
	teamRequest.Header.Set("Content-Type", "application/json")
	teamRequest.Header.Set("Origin", "http://routex.test")
	teamRequest.Header.Set("Sec-Fetch-Site", "same-origin")
	teamRequest.Header.Set("X-CSRF-Token", memberCSRF)
	teamResponse := httptest.NewRecorder()
	router.ServeHTTP(teamResponse, teamRequest)
	expectStatus(t, teamResponse, 200)
	if dispatches.Load() != 1 {
		t.Fatal("genuine Team native prerequisite missing")
	}
	hold.Store(true)
	pendingCtx, pendingCancel := context.WithTimeout(ctx, 10*time.Second)
	done := make(chan *httptest.ResponseRecorder, 1)
	joined := false
	go func() {
		r := httptest.NewRequestWithContext(pendingCtx, "POST", "http://routex.test/v1/chat/completions", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+bearer)
		r.Header.Set("X-Request-ID", "installation-held")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		done <- w
	}()
	// Always cancel/release and join the owned request before recorder/runtime teardown.
	defer func() {
		pendingCancel()
		unblock()
		if !joined {
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Error("held native request closure unknown")
			}
		}
	}()
	select {
	case <-entered:
	case <-pendingCtx.Done():
		t.Fatal("held native dispatch absent")
	}
	if dispatches.Load() != 2 {
		t.Fatal("native dispatch replayed")
	}
	// Invalid route configuration may retain the old route while publishing reduced
	// authorization. That cannot mint a successful combined installation record.
	if db.Model(&entity.ModelProviderBinding{}).Where("id = ?", binding).Update("weight", 50).Error != nil {
		t.Fatal("invalid route fixture")
	}
	if err := svc.RevokePersonalKey(ctx, member.User.ID, keyID); err == nil {
		t.Fatal("invalid route publication falsely acknowledged")
	}
	if _, err := svc.AuthenticateAPIKey(ctx, bearer); err == nil {
		t.Fatal("reduced Key authority retained")
	}
	if err := svc.RevokeAccountSession(ctx, member.User.ID, capturedTeam.SessionID); err != nil {
		t.Fatal("Session revocation failed")
	}
	if err := svc.ReauthorizeTeamSession(ctx, capturedTeam, model); err == nil {
		t.Fatal("reduced Team Session retained")
	}
	partial := page()
	for _, r := range partial.Items {
		if r.CurrentServingInstallationMatches != nil {
			t.Fatal("partial invalid-route publication claimed combined match")
		}
	}
	if db.Session(&gorm.Session{QueryFields: true}).Order("id").Find(&retained).Error != nil || !reflect.DeepEqual(original, retained) {
		t.Fatal("partial publication fabricated successful history")
	}
	unblock()
	select {
	case <-done:
		joined = true
	case <-pendingCtx.Done():
		t.Fatal("held native request not joined")
	}
	pendingCancel()
	if dispatches.Load() != 2 {
		t.Fatal("revocation replayed held native request")
	}
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	var attempts []entity.CallAttempt
	if db.Where("credential_id = ?", credential).Find(&attempts).Error != nil || len(attempts) != 2 {
		t.Fatal("native attempts missing")
	}
	for _, attempt := range attempts {
		if attempt.SnapshotID != first.Items[0].SnapshotID || attempt.ConnectionID != connection || attempt.ProviderID != provider || attempt.ProviderModelID != pm {
			t.Fatal("native attempt lost immutable attribution")
		}
	}
	var calls []entity.CallRecord
	if db.Where("connection_id = ?", connection).Find(&calls).Error != nil || len(calls) != 2 {
		t.Fatal("native call facts missing")
	}
	heldFact, teamFact := false, false
	for _, call := range calls {
		if call.KeyID == keyID && call.UserID == member.User.ID && call.TeamID == "" {
			heldFact = true
		}
		if call.KeyID == "" && call.UserID == member.User.ID && call.TeamID == team && call.TeamMembershipID == capturedTeam.TeamMembershipID {
			teamFact = true
		}
	}
	if !heldFact || !teamFact {
		t.Fatal("Key and Team native attribution conflated")
	}
	if db.Model(&entity.ModelProviderBinding{}).Where("id = ?", binding).Update("weight", 100).Error != nil {
		t.Fatal("restore valid routing")
	}
	// Ready operational metadata must not be admitted ahead of the committed
	// combined evidence for this exact owned process and retained route snapshot.
	callbackName := "test:installation-evidence-before-ready-metadata"
	var readyMetadataSeen atomic.Int64
	var readyMetadataEvidence atomic.Bool
	if err := privateDB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "runtime_publications" {
			return
		}
		publication, ok := tx.Statement.Dest.(*entity.RuntimePublication)
		if !ok || publication.Status != "ready" || publication.ErrorCode != "" || publication.SnapshotID != first.Items[0].SnapshotID {
			return
		}
		readyMetadataSeen.Add(1)
		status := svc.RuntimeStatus()
		if !status.Ready || status.ErrorCode != "" || status.SnapshotID != publication.SnapshotID || status.LastRefreshAt == nil || !status.LastRefreshAt.Equal(publication.CreatedAt) {
			return
		}
		// The independent harness connection can see only committed evidence.
		// Its existing parent context is retained; no timing sleep or retry is used.
		var observed []entity.RuntimeInstallationObservation
		if db.WithContext(tx.Statement.Context).Session(&gorm.Session{QueryFields: true}).Where("instance_id = ? AND snapshot_id = ?", original[0].InstanceID, publication.SnapshotID).Order("id").Limit(3).Find(&observed).Error != nil || len(observed) != 2 {
			return
		}
		oldUnchanged, newCommitted := false, false
		for _, row := range observed {
			if row.ID == original[0].ID {
				oldUnchanged = reflect.DeepEqual(row, original[0])
				continue
			}
			newCommitted = row.ID != "" && row.InstanceID == original[0].InstanceID && row.InstanceStartedAt.Equal(original[0].InstanceStartedAt) && row.SnapshotID == original[0].SnapshotID && row.RoutesPublishedAt.Equal(original[0].RoutesPublishedAt) && row.ProjectionVersion == original[0].ProjectionVersion && row.SourceDigest != "" && row.SourceDigest != original[0].SourceDigest && !row.FirstObservedAt.Before(publication.CreatedAt.UTC().Truncate(time.Microsecond))
		}
		readyMetadataEvidence.Store(oldUnchanged && newCommitted)
	}); err != nil {
		t.Fatal(err)
	}
	var removeOrderingOnce sync.Once
	removeOrderingCallback := func() {
		removeOrderingOnce.Do(func() {
			if err := privateDB.Callback().Create().Remove(callbackName); err != nil {
				t.Error(err)
			}
		})
	}
	defer removeOrderingCallback()
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	removeOrderingCallback()
	if readyMetadataSeen.Load() != 1 || !readyMetadataEvidence.Load() {
		t.Fatal("ready operational metadata preceded committed exact installation evidence")
	}
	changed := page()
	if len(changed.Items) != 2 {
		status := svc.RuntimeStatus()
		t.Fatalf("authority change observation count=%d ready=%t code=%q current_snapshot=%q original_snapshot=%q", len(changed.Items), status.Ready, status.ErrorCode, status.SnapshotID, first.Items[0].SnapshotID)
	}
	if changed.Items[0].SnapshotID != first.Items[0].SnapshotID {
		t.Fatalf("authority change did not retain route generation: current=%q original=%q", changed.Items[0].SnapshotID, first.Items[0].SnapshotID)
	}
	old, newest := false, false
	for _, r := range changed.Items {
		if r.ID == first.Items[0].ID {
			old = r.CurrentServingInstallationMatches != nil && !*r.CurrentServingInstallationMatches
		} else {
			newest = r.CurrentServingInstallationMatches != nil && *r.CurrentServingInstallationMatches
		}
	}
	if !old || !newest {
		t.Fatal("same-birth installed pair comparison missing")
	}
	firstPage := decodeCatalogResponse[service.RuntimeInstallationPage](t, req("GET", path+"?limit=1", nil), 200)
	if len(firstPage.Items) != 1 || firstPage.NextCursor == nil {
		t.Fatal("bounded installation cursor absent")
	}
	tail := decodeCatalogResponse[service.RuntimeInstallationPage](t, req("GET", path+"?limit=1&cursor="+url.QueryEscape(*firstPage.NextCursor), nil), 200)
	if len(tail.Items) != 1 || tail.Items[0].ID == firstPage.Items[0].ID || tail.NextCursor != nil {
		t.Fatal("bounded installation cursor reused row")
	}
	expectStatus(t, req("GET", path+"?instance_id="+first.Items[0].InstanceID+"&cursor="+url.QueryEscape(*firstPage.NextCursor), nil), 400)
	expectStatus(t, identityRequest(router, "GET", path+"?cursor="+url.QueryEscape(*firstPage.NextCursor), "", readerCookie, ""), 400)
	var oldRow entity.RuntimeInstallationObservation
	if db.Session(&gorm.Session{QueryFields: true}).Take(&oldRow, "id = ?", original[0].ID).Error != nil || !reflect.DeepEqual(oldRow, original[0]) {
		t.Fatal("new authority rewrote old observation")
	}
	// A failed primary database refresh cannot extend the original five-second
	// admission lease, mint history or cause a native replay.
	live, err := svc.CreatePersonalKey(ctx, member.User.ID, "Outage Key", []string{model}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmKeyDelivery(ctx, member.User.ID, live.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateAPIKey(ctx, live.Secret); err != nil {
		t.Fatal("outage Key prerequisite")
	}
	leaseObservation := time.Now()
	if db.Session(&gorm.Session{QueryFields: true}).Order("id").Find(&retained).Error != nil {
		t.Fatal("capture pre-outage observations")
	}
	previousMax := pool.Stats().MaxOpenConnections
	pool.SetMaxOpenConns(1)
	occupied, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal("hold owned sole database connection")
	}
	released := false
	defer func() {
		if !released {
			if err := occupied.Close(); err != nil {
				t.Error(err)
			}
		}
		pool.SetMaxOpenConns(previousMax)
	}()
	unavailable, boundedCancel := context.WithTimeout(ctx, 100*time.Millisecond)
	if err := svc.RefreshRuntime(unavailable); err == nil {
		boundedCancel()
		t.Fatal("unavailable database refresh accepted")
	}
	boundedCancel()
	denied, cancelRead := context.WithTimeout(ctx, 100*time.Millisecond)
	outageRequest := httptest.NewRequestWithContext(denied, "GET", "http://routex.test"+path, nil)
	outageRequest.AddCookie(cookie)
	outageResponse := httptest.NewRecorder()
	router.ServeHTTP(outageResponse, outageRequest)
	cancelRead()
	// Session authentication needs the same exhausted database connection and
	// returns the existing sanitized internal error before the endpoint runs.
	expectStatus(t, outageResponse, 500)
	var outagePayload map[string]any
	if json.Unmarshal(outageResponse.Body.Bytes(), &outagePayload) != nil || len(outagePayload) != 2 || outagePayload["code"] != float64(500) || outagePayload["message"] != "internal server error" {
		t.Fatal("unavailable authentication exposed private evidence")
	}
	timer := time.NewTimer(time.Until(leaseObservation.Add(5*time.Second + 100*time.Millisecond)))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal("original lease observation exceeded parent")
	}
	if _, err := svc.AuthenticateAPIKey(ctx, live.Secret); err == nil {
		t.Fatal("database outage extended original authorization lease")
	}
	if err := occupied.Close(); err != nil {
		t.Fatal("release owned database connection")
	}
	released = true
	pool.SetMaxOpenConns(previousMax)
	var postOutage []entity.RuntimeInstallationObservation
	if db.Session(&gorm.Session{QueryFields: true}).Order("id").Find(&postOutage).Error != nil || !reflect.DeepEqual(retained, postOutage) {
		t.Fatal("database outage fabricated evidence")
	}
	if dispatches.Load() != 2 {
		t.Fatal("outage replayed native work")
	}
}
