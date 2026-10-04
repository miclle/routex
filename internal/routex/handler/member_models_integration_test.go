package handler

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

// Real-driver fixtures use only root-registered product routes. Registration and
// migration V54 are prerequisites, not alternate fixture-only authorization.
func testMemberModelsLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	var controlledDiscoveries atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy authentication reached the owned upstream")
			http.Error(w, "unexpected proxy authentication", http.StatusBadRequest)
			return
		}
		if r.Method != "GET" || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer member-model-test-secret" {
			http.NotFound(w, r)
			return
		}
		controlledDiscoveries.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"member-model-upstream-a"},{"id":"member-model-upstream-b"}]}`)
	}))
	defer upstream.Close()
	store, err := secretstore.New([]byte(strings.Repeat("m", 32)))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithEgressPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"member-model-admin@example.invalid","password":"member-model-password","name":"Models administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	type actor struct {
		auth   *service.MemberRecord
		csrf   string
		cookie *http.Cookie
	}
	newActor := func(name string, permissions []string) actor {
		t.Helper()
		auth, cookie, csrf := createSystemStatusMember(t, svc, router, admin.User.ID, "member-model-"+name, permissions)
		return actor{auth: auth, cookie: cookie, csrf: csrf}
	}
	subject := newActor("subject", nil)
	peer := newActor("peer", nil)
	reader := newActor("reader", []string{"members.read"})
	writer := newActor("writer", []string{"members.models.write"})
	ordinary := newActor("ordinary", []string{"members.write", "models.write"})
	full := newActor("full", []string{"members.models.write", "providers.read", "prices.read"})
	providerOnly := newActor("provider", []string{"members.models.write", "providers.read"})
	priceOnly := newActor("price", []string{"members.models.write", "prices.read"})
	path := func(userID string) string { return "/api/v1/admin/members/" + url.PathEscape(userID) + "/models" }
	endpoint := path(subject.auth.User.ID)
	send := func(who actor, method, target, body, ifMatch string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, "http://routex.test"+target, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", who.csrf)
		if who.cookie != nil {
			request.AddCookie(who.cookie)
		}
		if ifMatch != "" {
			request.Header.Set("If-Match", ifMatch)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code >= 500 {
			_, _, sourceLine, _ := runtime.Caller(1)
			t.Logf("member Models %s returned HTTP %d at fixture caller line %d", method, response.Code, sourceLine)
		}
		return response
	}
	get := func(who actor, userID string) service.MemberModelsWorkspace {
		t.Helper()
		response := send(who, "GET", path(userID), "", "")
		if response.Code != 200 {
			_, _, sourceLine, _ := runtime.Caller(1)
			t.Logf("member Models GET expected200 at fixture caller line %d", sourceLine)
		}
		expectStatus(t, response, 200)
		var value service.MemberModelsWorkspace
		if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if value.UserID != userID || value.PersonalModels == nil || value.AvailableModels == nil || value.ObservedAt.IsZero() || len(value.ETag) != 64 || response.Header().Get("ETag") != strconv.Quote(value.ETag) || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("workspace lost exact safe target, arrays, observation or validator")
		}
		for _, forbidden := range []string{`"personal_grant_revision"`, `"source_request_id"`, `"password"`, `"email"`, `"team_id"`, `"provider_model_id"`, `"receipt"`, `"egress_id"`, `"auth_ciphertext"`, `"secret_generation"`, `"proxy_username"`, `"proxy_password"`, "test-only-proxy-password"} {
			if strings.Contains(response.Body.String(), forbidden) {
				t.Fatal("workspace leaked private metadata", forbidden)
			}
		}
		return value
	}
	body := func(ids []string, reason string) string {
		t.Helper()
		raw, err := json.Marshal(service.MemberModelsWriteInput{ModelIDs: ids, Reason: reason})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	put := func(who actor, userID, etag string, ids []string, reason string, status int) *service.MemberModelsWriteResult {
		t.Helper()
		response := send(who, "PUT", path(userID), body(ids, reason), strconv.Quote(etag))
		if response.Code != status {
			_, _, sourceLine, _ := runtime.Caller(1)
			t.Logf("member Models PUT expected%d at fixture caller line %d", status, sourceLine)
		}
		expectStatus(t, response, status)
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("private write result is cacheable")
		}
		if status != 200 {
			return nil
		}
		var result service.MemberModelsWriteResult
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		want := slices.Clone(ids)
		slices.Sort(want)
		if result.UserID != userID || !slices.Equal(result.ModelIDs, want) || result.ModelIDs == nil || !result.RuntimeApplied || result.Confirmation != "current_model_grants" || response.Header().Get("ETag") != strconv.Quote(result.ETag) {
			t.Fatal("write invented receipt or failed current exact application")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil || len(fields) != 5 {
			t.Fatal("write exposed fields beyond current-state confirmation")
		}
		return &result
	}
	grants := func(userID string) []entity.UserModelGrant {
		t.Helper()
		var rows []entity.UserModelGrant
		if err := db.Where("user_id = ?", userID).Order("model_id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		return rows
	}
	revision := func(userID string) string {
		t.Helper()
		var user entity.User
		if err := db.Select("id", "personal_grant_revision").First(&user, "id = ?", userID).Error; err != nil || user.ID != userID || len(user.PersonalGrantRevision) != 64 {
			t.Fatal("missing exact persistent generation", err)
		}
		return user.PersonalGrantRevision
	}
	auditCount := func() int64 {
		t.Helper()
		var count int64
		if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "member.models.update", subject.auth.User.ID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		return count
	}
	find := func(rows []service.MemberModelRow, modelID string) service.MemberModelRow {
		t.Helper()
		for _, row := range rows {
			if row.ID == modelID {
				return row
			}
		}
		t.Fatal("expected exact row absent", modelID)
		return service.MemberModelRow{}
	}
	expectStatus(t, send(actor{}, "GET", endpoint, "", ""), 401)
	expectStatus(t, send(ordinary, "GET", endpoint, "", ""), 403)
	empty := get(writer, subject.auth.User.ID)
	if !empty.CanEdit || len(empty.PersonalModels) != 0 || empty.RuntimeApplied != nil || empty.ApplicationStatus != "unavailable" {
		t.Fatal("cold current state invented application or denied independent writer")
	}
	if readOnly := get(reader, subject.auth.User.ID); readOnly.CanEdit {
		t.Fatal("members.read authorized foreign-owner grant mutation")
	}
	put(reader, subject.auth.User.ID, empty.ETag, []string{}, "Denied reader", 403)
	put(ordinary, subject.auth.User.ID, empty.ETag, []string{}, "Denied generic writer", 403)
	for _, query := range []string{"?q=name", "?user_id=" + peer.auth.User.ID, "?team_id=tea_foreign", "?limit=20", "?q=&q="} {
		expectStatus(t, send(writer, "GET", endpoint+query, "", ""), 400)
		expectStatus(t, send(writer, "PUT", endpoint+query, body([]string{}, "Rejected selector"), strconv.Quote(empty.ETag)), 400)
	}
	for _, target := range []string{strings.ToUpper(subject.auth.User.ID)} {
		response := send(writer, "GET", path(target), "", "")
		expectStatus(t, response, 404)
	}
	expectStatus(t, send(writer, "GET", path(subject.auth.User.ID+" "), "", ""), 400)
	for _, invalid := range []string{`{}`, `{"model_ids":null,"reason":"Reason"}`, `{"model_ids":[],"reason":null}`, `{"model_ids":[],"reason":""}`, `{"model_ids":[],"reason":" Reason"}`, `{"model_ids":[],"reason":"Reason\n"}`, `{"model_ids":[],"reason":"Reason","reason":"Other"}`, `{"model_ids":[],"Reason":"Reason"}`, `{"model_ids":[],"reason":"Reason","\u0072eason":"Other"}`, `{"model_ids":[],"reason":"Reason","user_id":"foreign"}`, `{"model_ids":[],"reason":"Reason"} {}`, `{"model_ids":[],"reason":"\ud800"}`, string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'})} {
		expectStatus(t, send(writer, "PUT", endpoint, invalid, strconv.Quote(empty.ETag)), 400)
	}
	expectStatus(t, send(writer, "PUT", endpoint, body([]string{}, strings.Repeat("x", 1025)), strconv.Quote(empty.ETag)), 400)
	for _, invalid := range []string{"", empty.ETag, "W/" + strconv.Quote(empty.ETag), "*", strconv.Quote(empty.ETag) + ", " + strconv.Quote(empty.ETag)} {
		expectStatus(t, send(writer, "PUT", endpoint, body([]string{}, "Invalid validator"), invalid), 400)
	}
	// API-owned discovery and explicit positive routing; no native inference or
	// fabricated completion is used to prove control-plane grant eligibility.
	provider, err := svc.CreateProvider(ctx, admin.User.ID, "Member Models provider", service.CreateConnectionInput{Name: "Member Models connection", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, CredentialName: "Controlled Member Models credential", Secret: "member-model-test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	credentialID := provider.Connections[0].Credentials[0].ID
	if _, err := svc.VerifyCredential(ctx, admin.User.ID, credentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredentialEnabled(ctx, admin.User.ID, credentialID, true); err != nil {
		t.Fatal(err)
	}
	modelIDs := map[string]string{}
	providerModels := map[string]string{}
	for _, letter := range []string{"a", "b"} {
		var pm entity.ProviderModel
		if err := db.Where("connection_id = ? AND upstream_name = ?", provider.Connections[0].Connection.ID, "member-model-upstream-"+letter).First(&pm).Error; err != nil {
			t.Fatal(err)
		}
		model, err := svc.CreateModel(ctx, admin.User.ID, "member-model-"+letter, pm.ID)
		if err != nil {
			t.Fatal(err)
		}
		modelIDs[letter], providerModels[letter] = model.Model.ID, pm.ID
		if _, err := svc.SetModelWeights(ctx, admin.User.ID, model.Model.ID, []service.ModelWeight{{BindingID: model.Bindings[0].Binding.ID, Weight: 100}}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SetModelGrants(ctx, admin.User.ID, model.Model.ID, []string{}); err != nil {
			t.Fatal(err)
		}
	}
	if revision(admin.User.ID) == strings.Repeat("0", 64) {
		t.Fatal("legacy CreateModel/global grant writers omitted persistent generation")
	}
	team, err := svc.CreateResource(ctx, admin.User.ID, service.TeamResource, "Target Team", "", []string{subject.auth.User.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, team.ID, []string{modelIDs["b"]}); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	current := get(writer, subject.auth.User.ID)
	if current.RuntimeApplied == nil || !*current.RuntimeApplied || current.ApplicationStatus != "applied" || len(current.AvailableModels) != 1 || find(current.AvailableModels, modelIDs["a"]).Availability != "ready" {
		t.Fatal("empty exact set or target Team-only exclusion was not proven")
	}
	// Exercise BOTH projected egress ETags through real saved APIs. This only
	// verifies discovery/current publication, never native completion. Proxy
	// requests are restricted to the owned loopback discovery endpoint.
	adminActor := actor{cookie: adminCookie, csrf: admin.CSRFToken}
	connectionID := provider.Connections[0].Connection.ID
	connectionETag := provider.Connections[0].Connection.ETag
	grantRevisionBeforeEgress := revision(subject.auth.User.ID)
	readReadyEgress := func(stage string) service.MemberModelsWorkspace {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			view := get(writer, subject.auth.User.ID)
			row := find(view.AvailableModels, modelIDs["a"])
			if row.Providers != nil || row.InputPrice.State != "unauthorized" || row.InputPrice.Rate != nil || row.OutputPrice.State != "unauthorized" || row.OutputPrice.Rate != nil {
				t.Fatal("egress branch leaked independent provider/price facts", stage, row)
			}
			if view.RuntimeApplied != nil && *view.RuntimeApplied && view.ApplicationStatus == "applied" && row.Availability == "ready" && row.Selectable && slices.Equal(row.Protocols, []string{entity.ProtocolOpenAIChat}) {
				return view
			}
			if !time.Now().Before(deadline) {
				t.Fatal("saved egress did not reach current published readiness", stage, view.ApplicationStatus, row.Availability)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	setConnectionEgress := func(mode string, egressID *string) {
		t.Helper()
		raw, err := json.Marshal(service.ConnectionEgressInput{ETag: connectionETag, Mode: mode, EgressID: egressID})
		if err != nil {
			t.Fatal(err)
		}
		view := decodeCatalogResponse[service.ConnectionEgressView](t, send(adminActor, "PATCH", "/api/v1/admin/connections/"+connectionID+"/egress", string(raw), ""), 200)
		if view.ConnectionID != connectionID || view.Mode != mode || !reflect.DeepEqual(view.EgressID, egressID) || view.ETag == "" || view.ETag == connectionETag {
			t.Fatal("connection egress response lost reviewed current target", view)
		}
		connectionETag = view.ETag
	}
	defaultView := decodeCatalogResponse[service.EgressDefaultView](t, send(adminActor, "GET", "/api/v1/admin/egress-default", "", ""), 200)
	if defaultView.EgressID != nil || defaultView.ETag == "" {
		t.Fatal("fixture requires the genuine initial direct default", defaultView)
	}
	setDefaultEgress := func(egressID *string) {
		t.Helper()
		raw, err := json.Marshal(service.EgressDefaultView{ETag: defaultView.ETag, EgressID: egressID})
		if err != nil {
			t.Fatal(err)
		}
		next := decodeCatalogResponse[service.EgressDefaultView](t, send(adminActor, "PUT", "/api/v1/admin/egress-default", string(raw), ""), 200)
		if next.ETag == "" || next.ETag == defaultView.ETag || !reflect.DeepEqual(next.EgressID, egressID) {
			t.Fatal("default egress response lost current reviewed state", next)
		}
		defaultView = next
	}
	for _, authenticated := range []bool{false, true} {
		proxyAddress, forwarded := memberModelsSOCKSProxyFixture(t, strings.TrimPrefix(upstream.URL, "http://"), authenticated)
		host, portRaw, err := net.SplitHostPort(proxyAddress)
		if err != nil {
			t.Fatal(err)
		}
		port, err := strconv.Atoi(portRaw)
		if err != nil {
			t.Fatal(err)
		}
		name := "Member Models no-auth proxy"
		auth := service.EgressAuthInput{Action: "remove"}
		if authenticated {
			name = "Member Models authenticated proxy"
			auth = service.EgressAuthInput{Action: "replace", Username: "proxy-user", Password: "test-only-proxy-password"}
		}
		raw, err := json.Marshal(service.EgressInput{Name: name, Kind: "socks5", Host: host, Port: port, Auth: auth, TestTargetBaseURL: upstream.URL + "/v1"})
		if err != nil {
			t.Fatal(err)
		}
		saved := decodeCatalogResponse[service.EgressView](t, send(adminActor, "POST", "/api/v1/admin/egresses", string(raw), ""), 201)
		if saved.ETag == "" || saved.AuthConfigured != authenticated || !saved.Enabled || saved.LastDiagnostic == nil || !saved.LastDiagnostic.TransportOK || forwarded.Load() == 0 {
			t.Fatal("proxy was not genuinely saved and diagnosed", name, saved)
		}
		// Explicit named Connection routing loads the projected Egress row.
		setConnectionEgress("proxy", &saved.ID)
		beforeDiscovery := controlledDiscoveries.Load()
		if _, err := svc.VerifyCredential(ctx, admin.User.ID, credentialID); err != nil {
			t.Fatal(err)
		}
		if controlledDiscoveries.Load() <= beforeDiscovery {
			t.Fatal("saved named proxy did not carry real authenticated discovery", name)
		}
		readReadyEgress(name + " explicit Connection")
		setConnectionEgress("default", nil)
		readReadyEgress(name + " restored Connection default")
		// A named PLATFORM default while the Connection is default loads both
		// projected EgressSetting.ETag and Egress.ETag, including SecretGeneration.
		setDefaultEgress(&saved.ID)
		beforeDiscovery = controlledDiscoveries.Load()
		if _, err := svc.VerifyCredential(ctx, admin.User.ID, credentialID); err != nil {
			t.Fatal(err)
		}
		if controlledDiscoveries.Load() <= beforeDiscovery {
			t.Fatal("saved named default did not carry real authenticated discovery", name)
		}
		readReadyEgress(name + " platform default")
		setDefaultEgress(nil)
		current = readReadyEgress(name + " restored platform default")
	}
	if revision(subject.auth.User.ID) != grantRevisionBeforeEgress {
		t.Fatal("egress eligibility review changed the Personal grant generation")
	}
	// Approve a real request; the resulting provenance is retained through later
	// direct/global no-ops and additions, while the request remains immutable.
	candidateResponse := identityRequest(router, "GET", "/api/v1/model-access-candidates/"+modelIDs["a"], "", subject.cookie, "")
	candidate := decodeCatalogResponse[service.ModelAccessCandidate](t, candidateResponse, 200)
	request, created, err := svc.CreatePersonalModelRequest(ctx, subject.auth.User.ID, candidate.ReviewETag, service.PersonalModelRequestInput{RequestID: "a1000000-0000-4000-8000-000000000001", ModelID: modelIDs["a"], Reason: "Controlled Member Models request"})
	if err != nil || !created {
		t.Fatal("real request creation failed", err)
	}
	review, err := svc.GetPersonalModelRequest(ctx, writer.auth.User.ID, subject.auth.User.ID, request.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	beforeApproval := revision(subject.auth.User.ID)
	decisionInput := service.PersonalModelDecisionInput{DecisionID: "a1000000-0000-4000-8000-000000000002", Action: "approve", Reason: "Controlled approval"}
	if _, err := svc.DecidePersonalModelRequest(ctx, writer.auth.User.ID, subject.auth.User.ID, request.ID, review.ReviewETag, decisionInput, true); err != nil {
		t.Fatal(err)
	}
	if revision(subject.auth.User.ID) == beforeApproval {
		t.Fatal("request insertion did not advance direct review generation")
	}
	put(writer, subject.auth.User.ID, current.ETag, []string{}, "Stale pre-approval review", 409)
	var savedRequest entity.PersonalModelRequest
	if err := db.First(&savedRequest, "id = ?", request.ID).Error; err != nil {
		t.Fatal(err)
	}
	original := grants(subject.auth.User.ID)
	if len(original) != 1 || original[0].SourceRequestID == nil || *original[0].SourceRequestID != request.ID {
		t.Fatal("real approval omitted retained provenance")
	}
	readOnlyRetained := get(reader, subject.auth.User.ID)
	if readOnlyRetained.CanEdit || len(readOnlyRetained.AvailableModels) != 0 || len(readOnlyRetained.PersonalModels) != 1 || readOnlyRetained.PersonalModels[0].ID != modelIDs["a"] {
		t.Fatal("read-only workspace hid retained grants or disclosed candidate choices")
	}
	stableRevision := revision(subject.auth.User.ID)
	if _, err := svc.DecidePersonalModelRequest(ctx, writer.auth.User.ID, subject.auth.User.ID, request.ID, review.ReviewETag, decisionInput, true); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(grants(subject.auth.User.ID), original) || revision(subject.auth.User.ID) != stableRevision {
		t.Fatal("historical approval retry changed grant generation/provenance")
	}
	peerRevision := revision(peer.auth.User.ID)
	if _, err := svc.SetModelGrants(ctx, admin.User.ID, modelIDs["a"], []string{subject.auth.User.ID, peer.auth.User.ID}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(grants(subject.auth.User.ID), original) || revision(subject.auth.User.ID) != stableRevision || revision(peer.auth.User.ID) == peerRevision {
		t.Fatal("global grant delta recreated retained provenance or missed affected user")
	}
	peerRevision = revision(peer.auth.User.ID)
	if _, err := svc.SetModelGrants(ctx, admin.User.ID, modelIDs["a"], []string{subject.auth.User.ID}); err != nil {
		t.Fatal(err)
	}
	if revision(peer.auth.User.ID) == peerRevision || len(grants(peer.auth.User.ID)) != 0 || !reflect.DeepEqual(grants(subject.auth.User.ID), original) {
		t.Fatal("global removal did not advance removed user or rewrote retained user")
	}
	if _, err := svc.SetModelGrants(ctx, admin.User.ID, modelIDs["a"], []string{subject.auth.User.ID, peer.auth.User.ID}); err != nil {
		t.Fatal(err)
	}
	peerHistory := grants(peer.auth.User.ID)
	// Release Team-only eligibility independently: target Team grants remain a
	// separate scope and this operation is not a Personal mutation.
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, team.ID, []string{}); err != nil {
		t.Fatal(err)
	}
	current = get(writer, subject.auth.User.ID)
	baseAudit := auditCount()
	put(writer, subject.auth.User.ID, current.ETag, []string{modelIDs["a"]}, "No-op reconciliation", 200)
	if auditCount() != baseAudit || revision(subject.auth.User.ID) != stableRevision || !reflect.DeepEqual(grants(subject.auth.User.ID), original) {
		t.Fatal("matching current-state confirmation invented an original receipt/write")
	}
	added := put(writer, subject.auth.User.ID, current.ETag, []string{modelIDs["b"], modelIDs["a"]}, "Add independent Personal model", 200)
	withB := grants(subject.auth.User.ID)
	if len(withB) != 2 || revision(subject.auth.User.ID) == stableRevision || auditCount() != baseAudit+1 {
		t.Fatal("actual delta missed revision/audit or complete sorted set")
	}
	for _, grant := range withB {
		if grant.ModelID == modelIDs["a"] && !reflect.DeepEqual(grant, original[0]) || grant.ModelID == modelIDs["b"] && grant.SourceRequestID != nil {
			t.Fatal("direct addition rewrote retained provenance or invented request source")
		}
	}
	if added.ETag == current.ETag {
		t.Fatal("real addition retained obsolete reviewed generation")
	}
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, team.ID, []string{modelIDs["b"]}); err != nil {
		t.Fatal(err)
	}
	project, err := svc.CreateResource(ctx, admin.User.ID, service.ProjectResource, "Independent Project", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.ProjectResource, project.ID, []string{modelIDs["a"]}); err != nil {
		t.Fatal(err)
	}
	var teamHistory []entity.TeamModelGrant
	var projectHistory []entity.ProjectModelGrant
	if err := db.Where("team_id = ?", team.ID).Order("model_id").Find(&teamHistory).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("project_id = ?", project.ID).Order("model_id").Find(&projectHistory).Error; err != nil {
		t.Fatal(err)
	}
	put(writer, subject.auth.User.ID, get(writer, subject.auth.User.ID).ETag, []string{modelIDs["a"]}, "Remove independent Personal model", 200)
	put(writer, subject.auth.User.ID, current.ETag, []string{modelIDs["a"], modelIDs["b"]}, "Stale add after reduction", 409)
	// Force a transactional audit error and verify complete rollback, not just
	// the final membership set. This callback never alters production behavior.
	callback := "test_member_models_audit_rollback"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && row.Action == "member.models.update" {
			_ = tx.AddError(errors.New("controlled Member Models audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	beforeRollback, rollbackRevision, rollbackAudit := grants(subject.auth.User.ID), revision(subject.auth.User.ID), auditCount()
	rollbackReview := get(writer, subject.auth.User.ID)
	put(writer, subject.auth.User.ID, rollbackReview.ETag, []string{}, "Rollback all grants", 500)
	if err := db.Callback().Create().Remove(callback); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(grants(subject.auth.User.ID), beforeRollback) || revision(subject.auth.User.ID) != rollbackRevision || auditCount() != rollbackAudit {
		t.Fatal("audit failure committed grants, revision or partial receipt")
	}
	// Commit a reduction then fail only auth publication. The same request can
	// reconcile current disabled grants with no historical receipt or new audit.
	var outage atomic.Bool
	publicationCallback := "test_member_models_publication_outage"
	if err := db.Callback().Query().Before("gorm:query").Register(publicationCallback, func(tx *gorm.DB) {
		if outage.Load() && tx.Statement.Table == "api_keys" {
			_ = tx.AddError(errors.New("controlled Member Models publication outage"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().After("gorm:create").Register(publicationCallback, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && row.Action == "member.models.update" && tx.Error == nil {
			outage.Store(true)
		}
	}); err != nil {
		t.Fatal(err)
	}
	uncertainReview := get(writer, subject.auth.User.ID)
	uncertainAudit := auditCount()
	put(writer, subject.auth.User.ID, uncertainReview.ETag, []string{}, "Controlled publication reduction", 503)
	outage.Store(false)
	if err := db.Callback().Query().Remove(publicationCallback); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Remove(publicationCallback); err != nil {
		t.Fatal(err)
	}
	uncertainRevision := revision(subject.auth.User.ID)
	if len(grants(subject.auth.User.ID)) != 0 || auditCount() != uncertainAudit+1 || uncertainRevision == rollbackRevision {
		t.Fatal("uncertain publication did not retain exact committed reduction")
	}
	put(writer, subject.auth.User.ID, uncertainReview.ETag, []string{}, "Controlled publication reduction", 200)
	if auditCount() != uncertainAudit+1 || revision(subject.auth.User.ID) != uncertainRevision {
		t.Fatal("current-state retry rewrote revision or claimed an extra operation")
	}
	emptyReview := get(writer, subject.auth.User.ID)
	put(writer, subject.auth.User.ID, emptyReview.ETag, []string{modelIDs["a"]}, "Readd Personal grant", 200)
	readded := grants(subject.auth.User.ID)
	if len(readded) != 1 || readded[0].SourceRequestID != nil {
		t.Fatal("direct readd borrowed retired request provenance")
	}
	put(writer, subject.auth.User.ID, get(writer, subject.auth.User.ID).ETag, []string{}, "Empty-set ABA", 200)
	put(writer, subject.auth.User.ID, emptyReview.ETag, []string{modelIDs["b"]}, "Stale empty-set review", 409)
	put(writer, subject.auth.User.ID, uncertainReview.ETag, []string{modelIDs["a"]}, "Stale pre-reduction replay", 409)
	if !reflect.DeepEqual(grants(peer.auth.User.ID), peerHistory) {
		t.Fatal("target writes altered sibling Personal grants")
	}
	var currentTeamGrants []entity.TeamModelGrant
	var currentProjectGrants []entity.ProjectModelGrant
	if err := db.Where("team_id = ?", team.ID).Order("model_id").Find(&currentTeamGrants).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("project_id = ?", project.ID).Order("model_id").Find(&currentProjectGrants).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(currentTeamGrants, teamHistory) || !reflect.DeepEqual(currentProjectGrants, projectHistory) {
		t.Fatal("Personal deltas changed Team/Project scope history")
	}
	var retainedRequest entity.PersonalModelRequest
	if err := db.First(&retainedRequest, "id = ?", request.ID).Error; err != nil || !reflect.DeepEqual(retainedRequest, savedRequest) {
		t.Fatal("direct grant workflows rewrote immutable approved request history", err)
	}
	// Read/write permission independence includes self and an administrator
	// target; no role-based implicit rejection or borrowed actor identity.
	self := get(writer, writer.auth.User.ID)
	put(writer, writer.auth.User.ID, self.ETag, []string{}, "Self current-state confirmation", 200)
	adminReview := get(writer, admin.User.ID)
	put(writer, admin.User.ID, adminReview.ETag, []string{}, "Administrator target confirmation", 200)
	beforeInactive := get(writer, subject.auth.User.ID)
	put(writer, subject.auth.User.ID, beforeInactive.ETag, []string{modelIDs["a"]}, "Retained inactive subject fixture", 200)
	foreignReview := get(full, subject.auth.User.ID)
	put(writer, subject.auth.User.ID, foreignReview.ETag, []string{modelIDs["a"], modelIDs["b"]}, "Foreign actor review cannot authorize delta", 409)
	if err := db.Model(&entity.User{}).Where("id = ?", subject.auth.User.ID).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	inactive := get(writer, subject.auth.User.ID)
	if inactive.CanEdit || len(inactive.PersonalModels) != 1 || len(inactive.AvailableModels) != 0 || inactive.RuntimeApplied == nil || *inactive.RuntimeApplied || inactive.ApplicationStatus != "not_applied" {
		t.Fatal("disabled subject leaked candidates or claimed application")
	}
	put(writer, subject.auth.User.ID, inactive.ETag, []string{}, "Disabled target mutation", 409)
	if err := db.Model(&entity.User{}).Where("id = ?", subject.auth.User.ID).Update("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	// Offboarded target remains a retained read, never fresh grant authority.
	offboarded := time.Now().UTC().Truncate(time.Millisecond)
	if err := db.Model(&entity.User{}).Where("id = ?", subject.auth.User.ID).Update("OffboardedAt", offboarded).Error; err != nil {
		t.Fatal(err)
	}
	departed := get(writer, subject.auth.User.ID)
	if departed.CanEdit || len(departed.PersonalModels) != 1 || len(departed.AvailableModels) != 0 {
		t.Fatal("offboarding hid retained grants or authorized fresh candidates")
	}
	put(writer, subject.auth.User.ID, departed.ETag, []string{}, "Offboarded target mutation", 409)
	if err := db.Model(&entity.User{}).Where("id = ?", subject.auth.User.ID).Update("OffboardedAt", nil).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	// Retained inactive Models can still be removed; readiness is only an Add gate.
	if err := db.Model(&entity.Model{}).Where("id = ?", modelIDs["a"]).Update("Status", entity.ResourceDisabled).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	disabledModel := get(writer, subject.auth.User.ID)
	retained := find(disabledModel.PersonalModels, modelIDs["a"])
	if retained.Status != entity.ResourceDisabled || retained.Availability != "unavailable" || len(retained.Protocols) != 0 || retained.Selectable || !disabledModel.CanEdit {
		t.Fatal("retained disabled Model invented callability or lost removal authority")
	}
	put(writer, subject.auth.User.ID, disabledModel.ETag, []string{}, "Remove retained disabled Model", 200)
	if err := db.Model(&entity.Model{}).Where("id = ?", modelIDs["a"]).Update("Status", entity.ResourceActive).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	// Independent metadata permissions never disclose the other protected domain.
	for _, check := range []struct {
		who       actor
		providers bool
		prices    bool
	}{{writer, false, false}, {providerOnly, true, false}, {priceOnly, false, true}, {full, true, true}} {
		workspace := get(check.who, subject.auth.User.ID)
		row := find(workspace.AvailableModels, modelIDs["a"])
		if row.Type != nil || row.UpdatedAt != nil || row.Protocols == nil || !slices.Equal(row.Protocols, []string{entity.ProtocolOpenAIChat}) || row.Availability != "ready" || !row.Selectable {
			t.Fatal("row invented type/update time or lost current protocol authority")
		}
		if check.providers && !slices.Equal(row.Providers, []string{"Member Models provider"}) || !check.providers && row.Providers != nil {
			t.Fatal("Provider labels violated independent permission")
		}
		wantPrice := "unauthorized"
		if check.prices {
			wantPrice = "missing"
		}
		if row.InputPrice.State != wantPrice || row.OutputPrice.State != wantPrice || row.InputPrice.Rate != nil || row.OutputPrice.Rate != nil {
			t.Fatal("price visibility inferred access or invented missing prices")
		}
	}
	// Typed prices come from actual eligible schedules, preserving exact zero,
	// decimal precision and denomination without manufacturing an aggregate.
	seedPrices := func(pmID, outputAmount, outputCurrency string) (entity.PriceRate, entity.PriceRate) {
		t.Helper()
		var schedule entity.ModelPrice
		err := db.Where("provider_model_id = ?", pmID).First(&schedule).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			scheduleID, err := id.NewPrefixed("mpr")
			if err != nil {
				t.Fatal(err)
			}
			schedule = entity.ModelPrice{ID: scheduleID, ProviderModelID: pmID, UpdateSource: "manual"}
			if err := db.Create(&schedule).Error; err != nil {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		rates := []entity.PriceRate{}
		for index, metric := range []string{pricing.Input, pricing.Output} {
			rateID, err := id.NewPrefixed("rat")
			if err != nil {
				t.Fatal(err)
			}
			rate := entity.PriceRate{ID: rateID, ModelPriceID: schedule.ID, Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0", Enabled: true}
			if index == 1 {
				rate.Amount, rate.Currency = outputAmount, outputCurrency
			}
			if err := db.Create(&rate).Error; err != nil {
				t.Fatal(err)
			}
			rates = append(rates, rate)
		}
		return rates[0], rates[1]
	}
	inputA, _ := seedPrices(providerModels["a"], "0.123456789012345678", "CNY")
	priced := find(get(full, subject.auth.User.ID).AvailableModels, modelIDs["a"])
	if priced.InputPrice.State != "priced" || priced.InputPrice.Rate == nil || priced.InputPrice.Rate.Amount != "0" || priced.InputPrice.Rate.Currency != "USD" || priced.OutputPrice.State != "priced" || priced.OutputPrice.Rate == nil || priced.OutputPrice.Rate.Amount != "0.123456789012345678" || priced.OutputPrice.Rate.Currency != "CNY" {
		t.Fatal("actual rate projection rounded money, erased zero or converted currency")
	}
	if hidden := find(get(providerOnly, subject.auth.User.ID).AvailableModels, modelIDs["a"]); hidden.InputPrice.State != "unauthorized" || hidden.InputPrice.Rate != nil || hidden.OutputPrice.Rate != nil {
		t.Fatal("Provider read leaked existing prices")
	}
	var binding entity.ModelProviderBinding
	if err := db.Where("model_id = ? AND provider_model_id = ?", modelIDs["a"], providerModels["a"]).First(&binding).Error; err != nil {
		t.Fatal(err)
	}
	extraID, err := id.NewPrefixed("bnd")
	if err != nil {
		t.Fatal(err)
	}
	extra := entity.ModelProviderBinding{ID: extraID, ModelID: modelIDs["a"], ProviderModelID: providerModels["b"], Weight: 50}
	if err := db.Create(&extra).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.ModelProviderBinding{}).Where("id = ?", binding.ID).Update("Weight", 50).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	mixed := find(get(full, subject.auth.User.ID).AvailableModels, modelIDs["a"])
	if mixed.InputPrice.State != "heterogeneous" || mixed.InputPrice.Rate != nil || mixed.OutputPrice.State != "heterogeneous" || mixed.OutputPrice.Rate != nil {
		t.Fatal("missing/present schedule mix invented a numeric rate")
	}
	inputB, _ := seedPrices(providerModels["b"], "1", "USD")
	mixed = find(get(full, subject.auth.User.ID).AvailableModels, modelIDs["a"])
	if mixed.InputPrice.State != "priced" || mixed.InputPrice.Rate == nil || mixed.InputPrice.Rate.Amount != "0" || mixed.OutputPrice.State != "heterogeneous" || mixed.OutputPrice.Rate != nil {
		t.Fatal("equal/mismatching schedules failed independent metric consensus")
	}
	for _, rate := range []entity.PriceRate{inputA, inputB} {
		if err := db.Model(&entity.PriceRate{}).Where("id = ?", rate.ID).Update("Enabled", false).Error; err != nil {
			t.Fatal(err)
		}
	}
	disabledRate := find(get(priceOnly, subject.auth.User.ID).AvailableModels, modelIDs["a"])
	if disabledRate.Providers != nil || disabledRate.InputPrice.State != "disabled" || disabledRate.InputPrice.Rate == nil || disabledRate.InputPrice.Rate.Amount != "0" {
		t.Fatal("disabled rate became enabled, unknown or leaked Provider labels")
	}
	if err := db.Delete(&extra).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.ModelProviderBinding{}).Where("id = ?", binding.ID).Update("Weight", 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	// Exact grant provenance/time and revision are independent proof dimensions.
	proofReview := get(writer, peer.auth.User.ID)
	peerGrant := peerHistory[0]
	changedDate := peerGrant.CreatedAt.Add(time.Minute)
	if err := db.Model(&entity.UserModelGrant{}).Where("user_id = ? AND model_id = ?", peer.auth.User.ID, peerGrant.ModelID).UpdateColumns(map[string]any{"created_at": changedDate}).Error; err != nil {
		t.Fatal(err)
	}
	mismatch := get(writer, peer.auth.User.ID)
	if mismatch.RuntimeApplied == nil || *mismatch.RuntimeApplied || mismatch.ApplicationStatus != "not_applied" || mismatch.ETag == proofReview.ETag {
		t.Fatal("private publication proof ignored exact immutable provenance/time")
	}
	if err := db.Model(&entity.UserModelGrant{}).Where("user_id = ? AND model_id = ?", peer.auth.User.ID, peerGrant.ModelID).UpdateColumns(map[string]any{"created_at": peerGrant.CreatedAt}).Error; err != nil {
		t.Fatal(err)
	}
	if restored := get(writer, peer.auth.User.ID); restored.RuntimeApplied == nil || !*restored.RuntimeApplied || restored.ETag != proofReview.ETag {
		t.Fatal("restored exact proof failed without a historical mutation claim")
	}
	// A complete workspace has an explicit bound; it never silently truncates a
	// replacement base. Keep these fixtures separate from actual native evidence.
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, team.ID, []string{}); err != nil {
		t.Fatal(err)
	}
	boundModels := make([]entity.Model, 999)
	boundNames := make([]entity.ModelName, 999)
	for index := range boundModels {
		modelID, err := id.NewPrefixed("mdl")
		if err != nil {
			t.Fatal(err)
		}
		boundModels[index] = entity.Model{ID: modelID, Status: entity.ResourceActive, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
		boundNames[index] = entity.ModelName{Name: "member-model-bound-" + strconv.Itoa(index), ModelID: modelID, CurrentModelID: &boundModels[index].ID}
	}
	if err := db.CreateInBatches(boundModels[:998], 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.CreateInBatches(boundNames[:998], 100).Error; err != nil {
		t.Fatal(err)
	}
	if atBound := get(writer, subject.auth.User.ID); len(atBound.AvailableModels) != 1000 {
		t.Fatal("complete 1000-row workspace lost exact candidate base")
	}
	for _, row := range []any{&boundModels[998], &boundNames[998]} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	expectStatus(t, send(writer, "GET", endpoint, "", ""), 422)
}

// An owned numeric SOCKS5 proxy connects only to the exact owned discovery
// server. Accepted tunnels are transport facts, not native inference evidence.
func memberModelsSOCKSProxyFixture(t *testing.T, target string, authenticated bool) (string, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var forwarded atomic.Int32
	var workers sync.WaitGroup
	var connections sync.Map
	workers.Go(func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Store(conn, true)
			workers.Go(func() {
				defer connections.Delete(conn)
				defer func() { _ = conn.Close() }()
				_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
				var head [2]byte
				if _, err := io.ReadFull(conn, head[:]); err != nil || head[0] != 5 || head[1] == 0 {
					return
				}
				methods := make([]byte, int(head[1]))
				if _, err := io.ReadFull(conn, methods); err != nil {
					return
				}
				method := byte(0)
				if authenticated {
					method = 2
				}
				if !slices.Contains(methods, method) {
					t.Error("proxy did not receive configured authentication method")
					return
				}
				if _, err := conn.Write([]byte{5, method}); err != nil {
					return
				}
				if authenticated {
					if _, err := io.ReadFull(conn, head[:]); err != nil || head[0] != 1 {
						return
					}
					username := make([]byte, int(head[1]))
					if _, err := io.ReadFull(conn, username); err != nil {
						return
					}
					var size [1]byte
					if _, err := io.ReadFull(conn, size[:]); err != nil {
						return
					}
					password := make([]byte, int(size[0]))
					if _, err := io.ReadFull(conn, password); err != nil {
						return
					}
					if string(username) != "proxy-user" || string(password) != "test-only-proxy-password" {
						t.Error("saved proxy authentication did not match owned fixture")
						_, _ = conn.Write([]byte{1, 1})
						return
					}
					if _, err := conn.Write([]byte{1, 0}); err != nil {
						return
					}
				}
				var request [4]byte
				if _, err := io.ReadFull(conn, request[:]); err != nil || request[0] != 5 || request[1] != 1 || request[3] != 1 {
					return
				}
				var endpoint [6]byte
				if _, err := io.ReadFull(conn, endpoint[:]); err != nil {
					return
				}
				address := net.JoinHostPort(net.IP(endpoint[:4]).String(), strconv.Itoa(int(binary.BigEndian.Uint16(endpoint[4:]))))
				if address != target {
					t.Error("proxy rejected a non-owned numeric destination")
					return
				}
				remote, err := net.DialTimeout("tcp", address, time.Second)
				if err != nil {
					t.Error("owned proxy target unavailable")
					return
				}
				defer func() { _ = remote.Close() }()
				_ = remote.SetDeadline(time.Now().Add(15 * time.Second))
				if _, err := conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}); err != nil {
					return
				}
				forwarded.Add(1)
				done := make(chan struct{})
				go func() { _, _ = io.Copy(remote, conn); _ = remote.Close(); close(done) }()
				_, _ = io.Copy(conn, remote)
				_ = conn.Close()
				<-done
			})
		}
	})
	t.Cleanup(func() {
		_ = listener.Close()
		connections.Range(func(key, value any) bool { _ = key.(net.Conn).Close(); return true })
		workers.Wait()
	})
	return listener.Addr().String(), &forwarded
}
