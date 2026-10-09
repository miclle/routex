package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secretstore"
)

func testMemberModelCatalogLifecycle(t *testing.T, db *gorm.DB) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer directory-private-secret" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"catalog-upstream"},{"id":"catalog-secondary"}]}`))
	}))
	defer upstream.Close()
	store, err := secretstore.New([]byte(strings.Repeat("d", 32)))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(context.Background(), db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"directory-admin@example.com","password":"directory-password","name":"Directory admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	request := func(cookie *http.Cookie, csrf, method, path string, body any) *httptest.ResponseRecorder {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return identityRequest(router, method, path, string(encoded), cookie, csrf)
	}
	adminRequest := func(method, path string, body any) *httptest.ResponseRecorder {
		return request(adminCookie, admin.CSRFToken, method, path, body)
	}
	expectStatus(t, adminRequest("POST", "/api/v1/admin/members", map[string]string{"email": "directory-member@example.com", "password": "directory-password", "name": "Directory member"}), 201)
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"directory-member@example.com","password":"directory-password"}`, nil, "")
	expectStatus(t, login, 200)
	member, memberCookie := readIdentity(t, login)
	get := func(path string) *httptest.ResponseRecorder {
		return request(memberCookie, member.CSRFToken, "GET", path, nil)
	}
	const path = "/api/v1/model-catalog"
	expectStatus(t, identityRequest(router, "GET", path, "", nil, ""), 401)
	expectStatus(t, get(path+"?q=ignored"), 400)
	expectStatus(t, get(path+"/mdl.invalid"), 400)
	expectStatus(t, get(path+"/mdl_missing"), 404)
	empty := decodeCatalogResponse[MemberModelCatalogResponse](t, get(path), 200)
	if len(empty.Items) != 0 || empty.Items == nil {
		t.Fatal("empty list must be a JSON array")
	}
	provider := decodeCatalogResponse[ProviderResponse](t, adminRequest("POST", "/api/v1/admin/providers", map[string]string{"name": "Directory provider", "connection_name": "Directory connection", "base_url": upstream.URL + "/v1", "protocol": "openai_chat", "credential_name": "Directory credential", "secret": "directory-private-secret"}), 201)
	conn := provider.Connections[0]
	credential := conn.Credentials[0]
	verification := decodeCatalogResponse[VerifyCredentialResponse](t, adminRequest("POST", "/api/v1/admin/credentials/"+credential.ID+"/verify", map[string]any{}), 200)
	if !verification.Verified {
		t.Fatal("actual discovery failed")
	}
	expectStatus(t, adminRequest("PATCH", "/api/v1/admin/credentials/"+credential.ID, map[string]bool{"enabled": true}), 200)
	providers := decodeCatalogResponse[ProvidersResponse](t, adminRequest("GET", "/api/v1/admin/providers", nil), 200)
	var pm ProviderModelResponse
	for _, item := range providers.Items {
		for _, connection := range item.Connections {
			for _, candidate := range connection.ProviderModels {
				if candidate.UpstreamName == "catalog-upstream" && connection.ID == conn.ID {
					pm = candidate
				}
			}
		}
	}
	if pm.ID == "" {
		t.Fatal("actual discovery missing from provider aggregate")
	}

	// Keep the primary Service's original database-before-runtime comparison.
	// This one capability write independently confirms a real publication.
	func() {
		writer, err := service.New(context.Background(), db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.StartRuntime(context.Background()); err != nil {
			t.Fatal(err)
		}
		defer writer.StopRuntime()
		writerRouter := fox.New()
		New(writer).RegisterRoutes(writerRouter)
		body, err := json.Marshal(map[string]any{"etag": pm.ETag, "capability_review_etag": pm.CapabilityReviewETag, "supports_image_input": true, "supports_pdf_input": true})
		if err != nil {
			t.Fatal(err)
		}
		pm = decodeCatalogResponse[ProviderModelResponse](t, identityRequest(writerRouter, "PATCH", "/api/v1/admin/provider-models/"+pm.ID, string(body), adminCookie, admin.CSRFToken), 200)
	}()
	model := decodeCatalogResponse[ModelResponse](t, adminRequest("POST", "/api/v1/admin/models", map[string]string{"name": "directory-model", "provider_model_id": pm.ID}), 201)
	expectStatus(t, adminRequest("PUT", "/api/v1/admin/models/"+model.ID+"/weights", map[string]any{"weights": []map[string]any{{"binding_id": model.Bindings[0].ID, "weight": 100}}}), 200)
	expectStatus(t, adminRequest("PUT", "/api/v1/admin/models/"+model.ID+"/grants", map[string]any{"user_ids": []string{member.User.ID}}), 200)
	create := func(value any) {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	createModel := func(id, name string) {
		create(&entity.Model{ID: id, Status: "active", CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)})
		create(&entity.ModelName{Name: name, ModelID: id, CurrentModelID: &id})
	}
	createModel("mdl_team_only", "team-only-ready")
	createModel("mdl_hidden", "hidden-model")
	createModel("mdl_unavailable", "unavailable-model")
	create(&entity.ModelProviderBinding{ID: "mpb_directory_team_ready", ModelID: "mdl_team_only", ProviderModelID: pm.ID, Weight: 100})
	teams := []entity.Team{{ID: "tem_directory_a", Name: "Directory A", Status: "active"}, {ID: "tem_directory_b", Name: "Directory B", Status: "active"}, {ID: "tem_directory_hidden", Name: "Hidden Team", Status: "active"}, {ID: "tem_directory_disabled", Name: "Disabled Team", Status: "disabled"}, {ID: "tem_directory_archived", Name: "Archived Team", Status: "archived"}, {ID: "tem_directory_inactive", Name: "Inactive member Team", Status: "active"}}
	create(&teams)
	memberships := []entity.TeamMembership{}
	for i, team := range teams {
		if i == 2 {
			continue
		}
		status := "active"
		if i == 5 {
			status = "disabled"
		}
		memberships = append(memberships, entity.TeamMembership{ID: fmt.Sprintf("tmb_directory_%d", i), TeamID: team.ID, UserID: member.User.ID, Role: "member", Status: status})
	}
	create(&memberships)
	grants := []entity.TeamModelGrant{{TeamID: teams[0].ID, ModelID: model.ID}, {TeamID: teams[1].ID, ModelID: model.ID}, {TeamID: teams[0].ID, ModelID: "mdl_team_only"}, {TeamID: teams[0].ID, ModelID: "mdl_unavailable"}}
	for _, team := range teams[2:] {
		grants = append(grants, entity.TeamModelGrant{TeamID: team.ID, ModelID: "mdl_hidden"})
	}
	create(&grants)
	var auditBefore int64
	if err := db.Model(&entity.AuditEvent{}).Count(&auditBefore).Error; err != nil {
		t.Fatal(err)
	}
	response := get(path)
	items := decodeCatalogResponse[MemberModelCatalogResponse](t, response, 200).Items
	if len(items) != 3 {
		t.Fatalf("wrong scope: %#v", items)
	}
	ready := decodeCatalogResponse[service.MemberModelCatalogRecord](t, get(path+"/"+model.ID), 200)
	if len(ready.Sources) != 3 || ready.Sources[0].Type != "personal" || ready.Sources[0].TeamID != nil || ready.Sources[0].TeamName != nil || !ready.Sources[0].InvocationSupported || ready.Sources[1].InvocationSupported || !ready.PersonalAvailable || !slices.Equal(ready.Protocols, []string{"openai_chat"}) || !slices.Equal(ready.InputCapabilities["openai_chat"], []string{"image", "pdf"}) {
		t.Fatalf("invalid actual directory metadata: %#v", ready)
	}
	var stored entity.Model
	if err := db.First(&stored, "id = ?", model.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.CreatedAt.Equal(ready.CreatedAt) || ready.CreatedAt.Location() != time.UTC {
		t.Fatal("creation timestamp was invented")
	}
	unavailable := decodeCatalogResponse[service.MemberModelCatalogRecord](t, get(path+"/mdl_unavailable"), 200)
	if unavailable.PersonalAvailable || unavailable.Protocols == nil || len(unavailable.Protocols) != 0 || unavailable.InputCapabilities == nil || len(unavailable.Sources) != 1 || unavailable.Sources[0].InvocationSupported {
		t.Fatal("Team availability or protocol was invented")
	}
	teamReady := decodeCatalogResponse[service.MemberModelCatalogRecord](t, get(path+"/mdl_team_only"), 200)
	if teamReady.PersonalAvailable || len(teamReady.Sources) != 1 || teamReady.Sources[0].InvocationSupported || !slices.Equal(teamReady.Protocols, []string{"openai_chat"}) || !slices.Equal(teamReady.InputCapabilities["openai_chat"], []string{"image", "pdf"}) {
		t.Fatalf("ready Team-only model promoted personal invocation: %#v", teamReady)
	}
	expectStatus(t, get(path+"/mdl_hidden"), 404)
	if adminItems := decodeCatalogResponse[MemberModelCatalogResponse](t, adminRequest("GET", path, nil), 200).Items; len(adminItems) != 0 {
		t.Fatal("administrator received implicit directory grants")
	}
	legacy := decodeCatalogResponse[VisibleModelsResponse](t, get("/api/v1/models"), 200)
	if len(legacy.Items) != 1 || legacy.Items[0].ID != model.ID {
		t.Fatal("Team grant expanded legacy personal model scope")
	}
	expectStatus(t, request(memberCookie, member.CSRFToken, "POST", "/api/v1/keys", map[string]any{"name": "Denied Team key", "model_ids": []string{"mdl_team_only"}}), 403)
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(get(path+"/"+model.ID).Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	expectedFields := []string{"id", "name", "status", "created_at", "protocols", "input_capabilities", "personal_available", "sources", "input_price", "output_price"}
	if len(wire) != len(expectedFields) {
		t.Fatal("public directory shape expanded")
	}
	for _, field := range expectedFields {
		if _, ok := wire[field]; !ok {
			t.Fatal("missing public field", field)
		}
	}
	for _, forbidden := range []string{"directory-private-secret", "ciphertext", "provider_id", "provider_model_id", "connection_id", "user_id", "request_id", "key_id", provider.Name} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatal("directory exposed private topology", forbidden)
		}
	}
	// Source reads and metadata fallback must release their transaction before borrowing again.
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	oldMax := pool.Stats().MaxOpenConnections
	pool.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	var wg sync.WaitGroup
	failures := make(chan error, 4)
	for range 4 {
		wg.Go(func() { _, err := svc.ListMemberModelCatalog(ctx, member.User.ID); failures <- err })
	}
	wg.Wait()
	cancel()
	close(failures)
	pool.SetMaxOpenConns(oldMax)
	for err := range failures {
		if err != nil {
			t.Fatal("single-connection directory read failed", err)
		}
	}
	// Runtime publication reports the same actual protocols/capabilities without promoting Team grants.
	if err := svc.StartRuntime(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	runtimePersonal := decodeCatalogResponse[service.MemberModelCatalogRecord](t, get(path+"/"+model.ID), 200)
	runtimeTeam := decodeCatalogResponse[service.MemberModelCatalogRecord](t, get(path+"/mdl_team_only"), 200)
	if !runtimePersonal.PersonalAvailable || !slices.Equal(runtimePersonal.Protocols, ready.Protocols) || !slices.Equal(runtimePersonal.InputCapabilities["openai_chat"], []string{"image", "pdf"}) || runtimeTeam.PersonalAvailable || !slices.Equal(runtimeTeam.Protocols, teamReady.Protocols) || !slices.Equal(runtimeTeam.InputCapabilities["openai_chat"], []string{"image", "pdf"}) {
		t.Fatal("runtime directory metadata diverged from scoped source semantics")
	}
	// Removing the direct grant preserves explicit Team visibility, not personal invocation.
	expectStatus(t, adminRequest("PUT", "/api/v1/admin/models/"+model.ID+"/grants", map[string]any{"user_ids": []string{}}), 200)
	ready = decodeCatalogResponse[service.MemberModelCatalogRecord](t, get(path+"/"+model.ID), 200)
	if ready.PersonalAvailable || len(ready.Sources) != 2 {
		t.Fatal("revoked personal grant remained")
	}
	if err := db.Model(&entity.TeamMembership{}).Where("user_id = ?", member.User.ID).Update("status", "disabled").Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, get(path+"/"+model.ID), 404)
	if err := db.Model(&entity.TeamMembership{}).Where("user_id = ?", member.User.ID).Update("status", "active").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.Model{}).Where("id = ?", model.ID).Update("status", "disabled").Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, get(path+"/"+model.ID), 404)
	if err := db.Model(&entity.Model{}).Where("id = ?", model.ID).Update("status", "active").Error; err != nil {
		t.Fatal(err)
	}
	// Folded raw database values must never promote active authority.
	if err := db.Model(&entity.TeamMembership{}).Where("user_id = ?", member.User.ID).Update("status", "ACTIVE").Error; err == nil {
		expectStatus(t, get(path+"/"+model.ID), 404)
	}
	if err := db.Model(&entity.TeamMembership{}).Where("user_id = ?", member.User.ID).Update("status", "active").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.TeamMembership{}).Where("user_id = ?", member.User.ID).Update("role", "MEMBER").Error; err == nil {
		expectStatus(t, get(path+"/"+model.ID), 404)
	}
	if err := db.Model(&entity.TeamMembership{}).Where("user_id = ?", member.User.ID).Update("role", "member").Error; err != nil {
		t.Fatal(err)
	}
	var auditAfter int64
	if err := db.Model(&entity.AuditEvent{}).Count(&auditAfter).Error; err != nil {
		t.Fatal(err)
	}
	// The explicit grant replacement above audits once; directory GETs write nothing.
	if auditAfter != auditBefore+1 {
		t.Fatalf("directory reads wrote audit events: before=%d after=%d", auditBefore, auditAfter)
	}
	// Isolated price checks follow the original audit baseline assertions.
	testMemberCatalogPriceFacts(t, db, svc, admin.User.ID, member.User.ID, model.ID, pm.ID, get)
	testMemberCatalogOverflow(t, db, get, member.User.ID)
	// Expired publication yields503 rather than fabricated protocols or stale404.
	svc.StopRuntime()
	validUntil := svc.RuntimeStatus().AuthorizationValidUntil
	if validUntil == nil {
		t.Fatal("missing runtime lease")
	}
	if remaining := time.Until(*validUntil) + 10*time.Millisecond; remaining > 0 {
		time.Sleep(remaining)
	}
	expectStatus(t, get(path+"/mdl_team_only"), 503)
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, get(path), 401)
}

func testMemberCatalogOverflow(t *testing.T, db *gorm.DB, get func(string) *httptest.ResponseRecorder, userID string) {
	t.Helper()
	create := func(rows any) {
		t.Helper()
		if err := db.CreateInBatches(rows, 100).Error; err != nil {
			t.Fatal(err)
		}
	}
	assertOverflow := func() {
		t.Helper()
		res := get("/api/v1/model-catalog")
		expectStatus(t, res, 422)
		if !strings.Contains(res.Body.String(), "model catalogue exceeds supported bounds") || strings.Contains(res.Body.String(), `"items"`) {
			t.Fatal("overflow did not fail complete query", res.Body.String())
		}
	}
	models := make([]entity.Model, 1001)
	names := make([]entity.ModelName, 1001)
	personal := make([]entity.UserModelGrant, 1001)
	for i := range models {
		id := fmt.Sprintf("mdl_directory_cap_%04d", i)
		models[i] = entity.Model{ID: id, Status: "active"}
		names[i] = entity.ModelName{Name: id, ModelID: id, CurrentModelID: &models[i].ID}
		personal[i] = entity.UserModelGrant{UserID: userID, ModelID: id}
	}
	create(&models)
	create(&names)
	create(&personal)
	assertOverflow()
	expectStatus(t, get("/api/v1/model-catalog/mdl_directory_cap_0000"), 200)
	if err := db.Where("user_id = ?", userID).Delete(&entity.UserModelGrant{}).Error; err != nil {
		t.Fatal(err)
	}
	teams := make([]entity.Team, 101)
	members := make([]entity.TeamMembership, 101)
	grants := make([]entity.TeamModelGrant, 101)
	for i := range teams {
		id := fmt.Sprintf("tem_directory_cap_%03d", i)
		teams[i] = entity.Team{ID: id, Name: id, Status: "active"}
		members[i] = entity.TeamMembership{ID: fmt.Sprintf("tmb_directory_cap_%03d", i), TeamID: id, UserID: userID, Role: "member", Status: "active"}
		grants[i] = entity.TeamModelGrant{TeamID: id, ModelID: models[i].ID}
	}
	create(&teams)
	create(&members)
	create(&grants)
	assertOverflow()
	// Detail restricts sources to the requested model, regardless of other Team/model overflow.
	expectStatus(t, get("/api/v1/model-catalog/"+models[0].ID), 200)
	if err := db.Where("team_id IN ?", func() []string {
		ids := []string{}
		for _, team := range teams {
			ids = append(ids, team.ID)
		}
		return ids
	}()).Delete(&entity.TeamModelGrant{}).Error; err != nil {
		t.Fatal(err)
	}
	grants = make([]entity.TeamModelGrant, 0, 5050)
	for _, team := range teams[:50] {
		for _, model := range models[:101] {
			grants = append(grants, entity.TeamModelGrant{TeamID: team.ID, ModelID: model.ID})
		}
	}
	create(&grants)
	assertOverflow()
	expectStatus(t, get("/api/v1/model-catalog/"+models[0].ID), 200)
	if err := db.Where("team_id LIKE ?", "tem_directory_cap_%").Delete(&entity.TeamModelGrant{}).Error; err != nil {
		t.Fatal(err)
	}
}

func testMemberCatalogPriceFacts(t *testing.T, db *gorm.DB, svc *service.Service, adminID, memberID, modelID, pmID string, get func(string) *httptest.ResponseRecorder) {
	t.Helper()
	ctx := context.Background()
	// The existing late lifecycle has removed the direct grant and retained Team sources.
	// Restore the ordinary member role spelling used by its original corruption probe.
	if err := db.Model(&entity.TeamMembership{}).Where("user_id = ?", memberID).Update("role", "member").Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	target := "/api/v1/model-catalog/" + modelID
	before := decodeCatalogResponse[service.MemberModelCatalogRecord](t, get(target), 200)
	if before.InputPrice.State != "unauthorized" || before.InputPrice.Rate != nil || before.OutputPrice.State != "unauthorized" {
		t.Fatal("member catalogue granted price authority", before)
	}
	role, err := svc.SaveRole(ctx, adminID, "", "Catalogue price reader", []string{"prices.read"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetMemberRoles(ctx, adminID, memberID, []string{role.Role.ID}); err != nil {
		t.Fatal(err)
	}
	page, err := svc.ListPrices(ctx, adminID, service.PriceFilter{ProviderModelID: pmID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.WritePrices(ctx, adminID, page.ETag, []service.PriceInput{{ProviderModelID: pmID, Rates: []pricing.Rate{
		{Metric: pricing.Input, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0", Enabled: true},
		{Metric: pricing.Output, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0.123456789012345678", Enabled: false},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	current := decodeCatalogResponse[service.MemberModelCatalogRecord](t, get(target), 200)
	if current.InputPrice.State != "priced" || current.InputPrice.Rate == nil || current.InputPrice.Rate.Amount != "0" || current.OutputPrice.State != "disabled" || current.OutputPrice.Rate == nil || current.OutputPrice.Rate.Amount != "0.123456789012345678" {
		t.Fatal("exact catalogue base prices lost", current)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(get(target).Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"input_price", "output_price"} {
		var cell map[string]json.RawMessage
		if err := json.Unmarshal(wire[field], &cell); err != nil || len(cell) != 2 || cell["state"] == nil || cell["rate"] == nil {
			t.Fatal("unsafe price cell shape", field, err)
		}
	}
	if _, err := svc.SetMemberRoles(ctx, adminID, memberID, []string{}); err != nil {
		t.Fatal(err)
	}
	current = decodeCatalogResponse[service.MemberModelCatalogRecord](t, get(target), 200)
	if current.InputPrice.State != "unauthorized" || current.InputPrice.Rate != nil || current.OutputPrice.Rate != nil {
		t.Fatal("revoked price permission leaked stored values", current)
	}
}
