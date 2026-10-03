package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
)

// The isolated harness exercises this transaction and its native effects on both drivers.
func testProjectInitialResourcesLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	var failingTable atomic.Value
	failingTable.Store("")
	var failPublication atomic.Bool
	const createCallback, queryCallback = "project-initial-create-outage", "project-initial-publication-outage"
	if err := db.Callback().Create().Before("gorm:create").Register(createCallback, func(tx *gorm.DB) {
		if target := failingTable.Load().(string); target != "" && tx.Statement.Table == target {
			_ = tx.AddError(errors.New("controlled initial Project transaction failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register(queryCallback, func(tx *gorm.DB) {
		if failPublication.Load() && tx.Statement.Table == "providers" {
			_ = tx.AddError(errors.New("controlled initial Project publication failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	// These earliest defers run after all later runtime and recorder shutdowns.
	defer func() {
		_ = db.Callback().Create().Remove(createCallback)
		_ = db.Callback().Query().Remove(queryCallback)
	}()
	store, err := secretstore.New(bytes.Repeat([]byte{149}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var dispatches atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatches.Add(1)
		if r.Header.Get("Authorization") != "Bearer initial-project-upstream" || r.Header.Get("Cookie") != "" {
			t.Error("initial Project inference changed upstream authentication")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatCompletionFixture("stop", `{"role":"assistant","content":"Initial resources"}`, `{"prompt_tokens":4,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}`, false, 0))
	}))
	defer upstream.Close()
	var instances []*service.Service
	defer func() {
		failingTable.Store("")
		failPublication.Store(false)
		for _, instance := range instances {
			instance.StopRuntime()
			if err := instance.StopCallRecorder(); err != nil {
				t.Error(err)
			}
		}
	}()
	makeService := func() *service.Service {
		t.Helper()
		instance, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, instance)
		return instance
	}
	svc := makeService()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	admin, adminCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/setup", `{"email":"project-initial-admin@example.invalid","password":"test-only-initial-password","name":"Initial Project administrator"}`, nil, ""))
	type actor struct {
		id, csrf string
		cookie   *http.Cookie
	}
	administrator := actor{admin.User.ID, admin.CSRFToken, adminCookie}
	member := func(name string, permissions []string) actor {
		t.Helper()
		value, cookie, csrf := createSystemStatusMember(t, svc, router, admin.User.ID, name, permissions)
		return actor{value.User.ID, csrf, cookie}
	}
	applicant := member("initial-applicant", nil)
	peer := member("initial-peer", nil)
	writer := member("initial-writer", []string{"projects.write"})
	modelsWriter := member("initial-models", []string{"projects.models.write"})
	limitsWriter := member("initial-limits", []string{"projects.limits.write"})
	reader := member("initial-reader", []string{"projects.read_all"})
	aliasPermissions := member("initial-alias-permissions", nil)
	for _, row := range []any{
		&entity.Role{ID: "rol_initial_alias", Name: "Alias permissions", NameKey: "initial-alias-permissions"},
		&entity.RolePermission{RoleID: "rol_initial_alias", Permission: "projects.models.WRITE"},
		&entity.RolePermission{RoleID: "rol_initial_alias", Permission: "projects.limits.WRITE"},
		&entity.UserRole{UserID: aliasPermissions.id, RoleID: "rol_initial_alias"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	var sequence int
	uuid := func() string { sequence++; return fmt.Sprintf("45000000-0000-4000-8000-%012d", sequence) }
	sendRaw := func(who actor, method, path, raw, etag string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(raw))
		req.Header.Set("Origin", "http://routex.test")
		req.Header.Set("Content-Type", "application/json")
		if who.cookie != nil {
			req.AddCookie(who.cookie)
		}
		if who.csrf != "" {
			req.Header.Set("X-CSRF-Token", who.csrf)
		}
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	send := func(who actor, method, path string, input any, etag string) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		return sendRaw(who, method, path, string(raw), etag)
	}
	creationContext := func(who actor) service.ProjectCreationContext {
		t.Helper()
		res := send(who, "GET", "/api/v1/project-creation-resources", nil, "")
		value := decodeCatalogResponse[service.ProjectCreationContext](t, res, 200)
		if len(value.ReviewETag) != 64 || res.Header().Get("ETag") != `"`+value.ReviewETag+`"` || value.PlatformCurrency != "USD" {
			t.Fatal("initial Project review lost its coherent denomination/validator", res.Body.String())
		}
		return value
	}
	tables := []string{"projects", "project_managers", "project_model_grants", "project_api_keys", "project_api_key_models", "resource_limits", "project_model_requests", "project_creation_receipts", "audit_events"}
	capture := func() []int64 {
		t.Helper()
		counts := make([]int64, len(tables))
		for i, table := range tables {
			if err := db.Table(table).Count(&counts[i]).Error; err != nil {
				t.Fatal(err)
			}
		}
		return counts
	}
	unchanged := func(label string, operation func()) {
		t.Helper()
		before := capture()
		operation()
		if after := capture(); !slices.Equal(before, after) {
			t.Fatalf("%s changed atomic state: before=%v after=%v", label, before, after)
		}
	}
	create := func(who actor, input map[string]any, etag string) ProjectCreationResponse {
		t.Helper()
		value := decodeCatalogResponse[ProjectCreationResponse](t, send(who, "POST", "/api/v1/projects", input, etag), 201)
		if !value.Committed || value.Receipt.CreationID != input["creation_id"] || value.Receipt.ProjectID == "" || value.Receipt.CreatedAt.IsZero() || value.Receipt.InitialRequestIDs == nil {
			t.Fatalf("enhanced creation lost its durable receipt: %+v", value)
		}
		return value
	}
	for _, who := range []actor{applicant, writer, modelsWriter, limitsWriter, reader, aliasPermissions, administrator} {
		value := creationContext(who)
		if value.CanSetModels != (who.id == modelsWriter.id || who.id == administrator.id) || value.CanSetLimits != (who.id == limitsWriter.id || who.id == administrator.id) || !value.CanRequestResources {
			t.Fatalf("creation context borrowed independent permissions for %s: %+v", who.id, value)
		}
	}
	expectStatus(t, send(actor{}, "GET", "/api/v1/project-creation-resources", nil, ""), 401)
	for _, query := range []string{"?user_id=" + applicant.id, "?q=x", "?limit=1"} {
		expectStatus(t, send(applicant, "GET", "/api/v1/project-creation-resources"+query, nil, ""), 400)
	}
	for _, query := range []string{"?limit=0", "?limit=51", "?limit=01", "?limit=", "?q=a&q=b", "?user_id=" + peer.id, "?cursor=bad%0A"} {
		expectStatus(t, send(applicant, "GET", "/api/v1/project-creation-models"+query, nil, ""), 400)
	}
	expectStatus(t, send(applicant, "GET", "/api/v1/admin/models", nil, ""), 403)
	legacy := decodeCatalogResponse[ProjectResponse](t, send(applicant, "POST", "/api/v1/projects", map[string]any{"name": "Legacy creation"}, ""), 201)
	if legacy.CreatorID != applicant.id || len(legacy.Managers) != 1 || len(legacy.ModelIDs) != 0 {
		t.Fatal("legacy flat creation changed", legacy)
	}
	unchanged("legacy conditional header", func() {
		expectStatus(t, send(applicant, "POST", "/api/v1/projects", map[string]any{"name": "Rejected legacy"}, creationContext(applicant).ReviewETag), 400)
	})
	modelIDs := []string{"mdl_initial_project_a", "mdl_initial_project_b"}
	for i, modelID := range modelIDs {
		if err := db.Create(&entity.Model{ID: modelID, Status: entity.ResourceActive}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&entity.ModelName{Name: fmt.Sprintf("initial-project-%c", 'a'+i), ModelID: modelID, CurrentModelID: &modelID}).Error; err != nil {
			t.Fatal(err)
		}
	}
	createRows := func(rows ...any) {
		t.Helper()
		for _, row := range rows {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	ciphertext, err := store.Seal("crd_initial_project", "initial-project-upstream")
	if err != nil {
		t.Fatal(err)
	}
	warmupBearer := "rx_" + strings.Repeat("i", 43)
	createRows(
		&entity.Provider{ID: "prv_initial_project", Name: "Initial Project provider"},
		&entity.ProviderConnection{ID: "con_initial_project", ProviderID: "prv_initial_project", Name: "Chat", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_initial_project", ConnectionID: "con_initial_project", Name: "Ready", Ciphertext: ciphertext, Enabled: true, VerificationStatus: "verified"},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelIDs[0]},
		&entity.APIKey{ID: "key_initial_warmup", UserID: admin.User.ID, Name: "Coverage warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(warmupBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_initial_warmup", ModelID: modelIDs[0]},
	)
	for i, modelID := range modelIDs {
		providerModelID := fmt.Sprintf("pmd_initial_project_%d", i)
		priceID := fmt.Sprintf("mpr_initial_project_%d", i)
		createRows(
			&entity.ProviderModel{ID: providerModelID, ConnectionID: "con_initial_project", UpstreamName: fmt.Sprintf("initial-native-%d", i)},
			&entity.CredentialModelAccess{CredentialID: "crd_initial_project", ProviderModelID: providerModelID},
			&entity.ModelProviderBinding{ID: fmt.Sprintf("bnd_initial_project_%d", i), ModelID: modelID, ProviderModelID: providerModelID, Weight: 100},
			&entity.ReservationBound{ProviderModelID: providerModelID, Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 4, MaxOutputTokens: 1, ETag: fmt.Sprintf("bound-initial-%d", i), Evidence: "Controlled native maximum", Reason: "Initial creation acceptance"},
			&entity.ModelPrice{ID: priceID, ProviderModelID: providerModelID, UpdateSource: "manual"},
		)
		for rateIndex, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
			createRows(&entity.PriceRate{ID: fmt.Sprintf("rat_initial_%d_%d", i, rateIndex), ModelPriceID: priceID, Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Amount: "1000000", Currency: "USD", Enabled: true})
		}
	}
	spool := filepath.Join(t.TempDir(), "initial-project-calls.db")
	start := func() {
		t.Helper()
		if err := svc.StartCallRecorder(ctx, spool); err != nil {
			t.Fatal(err)
		}
		if err := svc.StartRuntime(ctx); err != nil {
			t.Fatal(err)
		}
		svc.StopRuntime() // Synchronous publications keep controlled failure boundaries deterministic.
	}
	start()
	native := func(bearer, name string) *httptest.ResponseRecorder {
		t.Helper()
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"`+name+`","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	expectStatus(t, native(warmupBearer, "initial-project-a"), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	page := decodeCatalogResponse[service.ProjectCreationModelPage](t, send(applicant, "GET", "/api/v1/project-creation-models?limit=1", nil, ""), 200)
	if len(page.Items) != 1 || page.NextCursor == nil || page.Items[0].ID != modelIDs[0] || page.Items[0].Name != "initial-project-a" {
		t.Fatal("minimal creation model picker lost pagination or canonical identity", page)
	}
	next := decodeCatalogResponse[service.ProjectCreationModelPage](t, send(applicant, "GET", "/api/v1/project-creation-models?cursor="+*page.NextCursor, nil, ""), 200)
	if len(next.Items) != 1 || next.Items[0].ID != modelIDs[1] {
		t.Fatal("creation model picker repeated the first page", next)
	}
	literal := decodeCatalogResponse[service.ProjectCreationModelPage](t, send(applicant, "GET", "/api/v1/project-creation-models?q=%25", nil, ""), 200)
	if len(literal.Items) != 0 {
		t.Fatal("creation model search treated percent as wildcard")
	}
	// Every subset of the three independent request kinds is one creation transaction.
	for mask := 0; mask < 8; mask++ {
		input := map[string]any{"name": fmt.Sprintf("Initial requests %d", mask), "creation_id": uuid(), "manager_ids": []string{applicant.id, peer.id}}
		requested := map[string]any{"reason": "Reviewed initial workload"}
		wantKinds := []string{}
		if mask&1 != 0 {
			requested["model_ids"] = modelIDs
			wantKinds = append(wantKinds, entity.ProjectRequestModelAccess)
		}
		if mask&2 != 0 {
			requested["tokens_month"], requested["money_month"], requested["currency"] = 0, "0.000000000000000001", "USD"
			wantKinds = append(wantKinds, entity.ProjectRequestQuota)
		}
		if mask&4 != 0 {
			requested["rpm"], requested["tpm"], requested["concurrency"] = 0, 100, 1
			wantKinds = append(wantKinds, entity.ProjectRequestRateLimit)
		}
		if mask != 0 {
			input["initial_request"] = requested
		}
		created := create(applicant, input, creationContext(applicant).ReviewETag)
		if created.Project == nil || created.Project.CreatorID != applicant.id || len(created.Project.Managers) != 2 || len(created.Project.ModelIDs) != 0 || !created.RuntimeApplied || created.ApplicationStatus != "applied" {
			t.Fatalf("pending initial resources became effective or unavailable, mask=%d: %+v", mask, created)
		}
		var rows []entity.ProjectModelRequest
		if err := db.Where("project_id = ?", created.Receipt.ProjectID).Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		ids, kinds := []string{}, []string{}
		for _, row := range rows {
			if row.ApplicantUserID != applicant.id || row.Status != entity.ProjectRequestPending || row.Reason != requested["reason"] {
				t.Fatal("initial request lost pending actor/reason facts", row)
			}
			ids, kinds = append(ids, row.ID), append(kinds, row.Kind)
		}
		slices.Sort(ids)
		slices.Sort(kinds)
		slices.Sort(wantKinds)
		receiptIDs := slices.Clone(created.Receipt.InitialRequestIDs)
		slices.Sort(receiptIDs)
		if !slices.Equal(ids, receiptIDs) || !slices.Equal(kinds, wantKinds) {
			t.Fatalf("initial request split/receipt mismatch mask=%d kinds=%v receipt=%v", mask, kinds, created.Receipt.InitialRequestIDs)
		}
		policy, err := svc.GetResourceLimit(ctx, applicant.id, service.LimitTarget{Kind: "project", ID: created.Receipt.ProjectID})
		if err != nil || policy.Stored.TokensMonth != nil || policy.Stored.MoneyMonth != nil || policy.Stored.RPM != nil || policy.Stored.TPM != nil || policy.Stored.Concurrency != nil {
			t.Fatal("initial request changed effective policy before approval", policy, err)
		}
		unchanged("saved initial creation retry", func() {
			retried := create(applicant, input, creationContext(applicant).ReviewETag)
			if !reflect.DeepEqual(created.Receipt, retried.Receipt) {
				t.Fatal("saved initial request receipt changed", created.Receipt, retried.Receipt)
			}
		})
	}
	// Independent direct authority applies only to the submitted dimensions.
	for _, sample := range []struct {
		who    actor
		fields map[string]any
	}{
		{applicant, map[string]any{"model_ids": modelIDs}},
		{writer, map[string]any{"model_ids": modelIDs}},
		{reader, map[string]any{"tokens_month": 5}},
		{modelsWriter, map[string]any{"model_ids": modelIDs, "rpm": 1}},
		{limitsWriter, map[string]any{"model_ids": modelIDs, "tokens_month": 5}},
		{aliasPermissions, map[string]any{"model_ids": modelIDs, "tokens_month": 5}},
	} {
		sample.fields["reason"] = "Direct initial resources"
		input := map[string]any{"name": "Forbidden direct initial resources", "creation_id": uuid(), "initial_resources": sample.fields}
		unchanged("independent direct resource permission", func() {
			expectStatus(t, send(sample.who, "POST", "/api/v1/projects", input, creationContext(sample.who).ReviewETag), 403)
		})
	}
	for _, sample := range []struct {
		who    actor
		fields map[string]any
	}{
		{modelsWriter, map[string]any{"model_ids": []string{modelIDs[0]}, "reason": "Direct models only"}},
		{limitsWriter, map[string]any{"tokens_month": 0, "rpm": 0, "reason": "Direct zero limits"}},
	} {
		created := create(sample.who, map[string]any{"name": "Independent direct initial resources", "creation_id": uuid(), "initial_resources": sample.fields}, creationContext(sample.who).ReviewETag)
		if created.Project == nil || !created.RuntimeApplied || created.ApplicationStatus != "applied" || len(created.Receipt.InitialRequestIDs) != 0 {
			t.Fatal("direct resources borrowed request semantics", created)
		}
		if sample.who.id == limitsWriter.id {
			var zero entity.ResourceLimit
			if err := db.Where("scope_kind = ? AND scope_id = ?", "project", created.Receipt.ProjectID).Take(&zero).Error; err != nil || zero.TokensMonth == nil || *zero.TokensMonth != 0 || zero.RPM == nil || *zero.RPM != 0 || zero.MoneyMonth != nil || zero.TPM != nil {
				t.Fatal("initial zero limits became unlimited or invented omitted fields", zero, err)
			}
		}
	}
	permissionInput := map[string]any{"name": "Saved direct authority", "creation_id": uuid(), "initial_resources": map[string]any{"model_ids": []string{modelIDs[0]}, "reason": "Reviewed direct Model"}}
	permissionETag := creationContext(modelsWriter).ReviewETag
	permissionCreation := create(modelsWriter, permissionInput, permissionETag)
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, modelsWriter.id, []string{}); err != nil {
		t.Fatal(err)
	}
	unchanged("saved creation reconciles before renewed direct permission", func() {
		retry := create(modelsWriter, permissionInput, permissionETag)
		if retry.Project == nil || !retry.RuntimeApplied || retry.ApplicationStatus != "applied" || !reflect.DeepEqual(permissionCreation.Receipt, retry.Receipt) {
			t.Fatal("loss of direct permission erased a saved own receipt", retry)
		}
	})
	unchanged("revoked direct permission cannot create a fresh intent", func() {
		fresh := map[string]any{"name": permissionInput["name"], "creation_id": uuid(), "initial_resources": permissionInput["initial_resources"]}
		expectStatus(t, send(modelsWriter, "POST", "/api/v1/projects", fresh, creationContext(modelsWriter).ReviewETag), 403)
	})
	// Trust-boundary failures cannot leave Projects, relationships, requests or UUID slots.
	valid := map[string]any{"name": "Reviewed initial resources", "creation_id": uuid(), "manager_ids": []string{applicant.id}, "initial_request": map[string]any{"model_ids": modelIDs, "reason": "Initial workload"}}
	for _, raw := range []string{
		`{"name":"Empty supplied UUID","creation_id":""}`,
		`{"name":"No UUID","initial_request":{"rpm":1,"reason":"Work"}}`,
		`{"name":"Invalid UUID","creation_id":"not-a-uuid"}`,
		`{"name":"Null","creation_id":null}`,
		`{"name":"Null limits","creation_id":"45000000-0000-4000-8000-000000999001","initial_resources":{"tokens_month":null,"reason":"Work"}}`,
		`{"name":"Null rates","creation_id":"45000000-0000-4000-8000-000000999010","initial_request":{"rpm":null,"reason":"Work"}}`,
		`{"name":"Null amount","creation_id":"45000000-0000-4000-8000-000000999011","initial_request":{"money_month":null,"currency":"USD","reason":"Work"}}`,
		`{"name":"Duplicate","name":"Other"}`,
		`{"name":"Duplicate nested","creation_id":"45000000-0000-4000-8000-000000999002","initial_request":{"rpm":1,"rpm":2,"reason":"Work"}}`,
		`{"name":"Mixed","creation_id":"45000000-0000-4000-8000-000000999003","initial_resources":{"rpm":1,"reason":"Work"},"initial_request":{"rpm":1,"reason":"Work"}}`,
		`{"name":"Unsupported","creation_id":"45000000-0000-4000-8000-000000999004","initial_request":{"tokens_5h":1,"reason":"Work"}}`,
		`{"name":"Currency only","creation_id":"45000000-0000-4000-8000-000000999005","initial_request":{"currency":"USD","reason":"Work"}}`,
		`{"name":"Empty models","creation_id":"45000000-0000-4000-8000-000000999006","initial_resources":{"model_ids":[],"reason":"Work"}}`,
		"{\"name\":\"Invalid UTF-8 \xff\"}",
	} {
		unchanged("malformed initial creation", func() {
			expectStatus(t, sendRaw(administrator, "POST", "/api/v1/projects", raw, creationContext(administrator).ReviewETag), 400)
		})
	}
	for _, fields := range []map[string]any{
		{"model_ids": []string{strings.ToUpper(modelIDs[0])}, "reason": "Alias"},
		{"model_ids": []string{modelIDs[0], modelIDs[0]}, "reason": "Duplicate"},
		{"tokens_month": -1, "reason": "Negative"}, {"tpm": 9007199254740992, "reason": "Overflow"},
		{"money_month": 1, "currency": "USD", "reason": "Numeric amount"},
		{"money_month": "1e2", "currency": "USD", "reason": "Exponent"},
		{"rpm": 1, "reason": ""},
		{"rpm": 1, "reason": "Invalid\u0000control"},
		{"rpm": 1, "reason": strings.Repeat("r", 2001)},
	} {
		input := map[string]any{"name": "Rejected initial fields", "creation_id": uuid(), "initial_resources": fields}
		unchanged("invalid initial fields", func() {
			expectStatus(t, send(administrator, "POST", "/api/v1/projects", input, creationContext(administrator).ReviewETag), 400)
		})
	}
	unchanged("current denomination mismatch", func() {
		input := map[string]any{"name": "Wrong denomination", "creation_id": uuid(), "initial_resources": map[string]any{"money_month": "1", "currency": "EUR", "reason": "Work"}}
		expectStatus(t, send(administrator, "POST", "/api/v1/projects", input, creationContext(administrator).ReviewETag), 409)
	})
	unchanged("exact persisted Model identity", func() {
		input := map[string]any{"name": "Collated Model alias", "creation_id": uuid(), "initial_resources": map[string]any{"model_ids": []string{"mdl_initial_project_A"}, "reason": "Canonical selection required"}}
		res := send(administrator, "POST", "/api/v1/projects", input, creationContext(administrator).ReviewETag)
		if res.Code != 400 && res.Code != 404 && res.Code != 409 {
			t.Fatal("noncanonical Model obtained a grant", res.Code, res.Body.String())
		}
	})
	if _, err := svc.GetProjectCreationContext(ctx, "usr_"+strings.ToUpper(applicant.id[4:])); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("collated actor alias obtained a current creation context", err)
	}
	unchanged("applicant must explicitly select own manager identity", func() {
		input := map[string]any{"name": "Missing explicit applicant", "creation_id": uuid(), "manager_ids": []string{peer.id}, "initial_request": map[string]any{"rpm": 1, "reason": "Work"}}
		expectStatus(t, send(applicant, "POST", "/api/v1/projects", input, creationContext(applicant).ReviewETag), 400)
	})
	for _, who := range []actor{actor{}, {id: applicant.id, cookie: applicant.cookie}} {
		status := 401
		if who.cookie != nil {
			status = 403
		}
		unchanged("creation authentication", func() {
			expectStatus(t, send(who, "POST", "/api/v1/projects", valid, creationContext(applicant).ReviewETag), status)
		})
	}
	for _, etag := range []string{"", "weak", strings.Repeat("0", 64)} {
		status := 400
		if len(etag) == 64 {
			status = 409
		}
		unchanged("creation review required", func() { expectStatus(t, send(applicant, "POST", "/api/v1/projects", valid, etag), status) })
	}
	for _, headers := range [][]string{{"W/\"" + strings.Repeat("a", 64) + "\""}, {"*"}, {`"` + strings.Repeat("a", 64) + `"`, `"` + strings.Repeat("b", 64) + `"`}} {
		unchanged("strong singleton creation review header", func() {
			raw, _ := json.Marshal(valid)
			req := httptest.NewRequest("POST", "http://routex.test/api/v1/projects", bytes.NewReader(raw))
			req.Header.Set("Origin", "http://routex.test")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-CSRF-Token", applicant.csrf)
			req.AddCookie(applicant.cookie)
			for _, header := range headers {
				req.Header.Add("If-Match", header)
			}
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			expectStatus(t, res, 400)
		})
	}
	// Faults at each write boundary must roll back the complete transaction.
	for _, table := range []string{"projects", "project_managers", "project_model_grants", "resource_limits", "project_creation_receipts", "audit_events", "project_model_requests"} {
		input := map[string]any{"name": "Atomic initial resources", "creation_id": uuid(), "manager_ids": []string{applicant.id, peer.id}, "initial_resources": map[string]any{"model_ids": modelIDs, "tokens_month": 5, "rpm": 10, "reason": "Atomic workload"}}
		if table == "project_model_requests" {
			delete(input, "initial_resources")
			input["manager_ids"] = []string{administrator.id, applicant.id, peer.id}
			input["initial_request"] = map[string]any{"model_ids": modelIDs, "tokens_month": 5, "rpm": 10, "reason": "Atomic requests"}
		}
		reviewed := creationContext(administrator).ReviewETag
		unchanged("controlled "+table+" rollback", func() {
			failingTable.Store(table)
			expectStatus(t, send(administrator, "POST", "/api/v1/projects", input, reviewed), 500)
			failingTable.Store("")
		})
		created := create(administrator, input, reviewed)
		if created.Project == nil {
			t.Fatal("rolled-back UUID could not create a complete Project")
		}
	}
	// Immutable creation receipts never authorize a second write under a collided intent.
	reviewed := creationContext(applicant).ReviewETag
	created := create(applicant, valid, reviewed)
	for _, change := range []map[string]any{
		{"name": "Changed intent", "creation_id": valid["creation_id"], "manager_ids": []string{applicant.id}, "initial_request": valid["initial_request"]},
		{"name": valid["name"], "creation_id": valid["creation_id"], "manager_ids": []string{applicant.id}, "initial_request": map[string]any{"model_ids": modelIDs, "reason": "Different reason"}},
	} {
		unchanged("UUID intent collision", func() { expectStatus(t, send(applicant, "POST", "/api/v1/projects", change, reviewed), 409) })
	}
	unchanged("foreign actor cannot reconcile creation receipt", func() {
		res := send(peer, "POST", "/api/v1/projects", permissionInput, permissionETag)
		expectStatus(t, res, 409)
		if strings.Contains(res.Body.String(), permissionCreation.Receipt.ProjectID) {
			t.Fatal("foreign creation collision leaked the original Project")
		}
	})
	var receiptBefore entity.ProjectCreationReceipt
	if err := db.Where("creation_id = ?", created.Receipt.CreationID).Take(&receiptBefore).Error; err != nil {
		t.Fatal(err)
	}
	updatedName := "Renamed after creation"
	if _, err := svc.UpdateResource(ctx, applicant.id, service.ProjectResource, created.Receipt.ProjectID, service.ResourceUpdate{Name: &updatedName}); err != nil {
		t.Fatal(err)
	}
	unchanged("incidental metadata does not undo initial resource application", func() {
		retry := create(applicant, valid, reviewed)
		if retry.Project == nil || retry.Project.Name != updatedName || !retry.RuntimeApplied || retry.ApplicationStatus != "applied" || !reflect.DeepEqual(created.Receipt, retry.Receipt) {
			t.Fatal("metadata edit confused current application with historical receipt", retry)
		}
	})
	var savedInput service.ProjectCreationInput
	raw, _ := json.Marshal(valid)
	if err := json.Unmarshal(raw, &savedInput); err != nil {
		t.Fatal(err)
	}
	savedInput.ReviewETag = reviewed
	if _, err := svc.CreateProjectWithInitialResources(ctx, "usr_"+strings.ToUpper(applicant.id[4:]), savedInput); !errors.Is(err, apperrors.ErrUnauthorized) && !errors.Is(err, apperrors.ErrForbidden) && !errors.Is(err, apperrors.ErrBadRequest) {
		t.Fatal("actor alias gained creation authority", err)
	}
	// Initial requests reuse real reviewer routes, without allowing self-approval.
	requestID := created.Receipt.InitialRequestIDs[0]
	requestPath := "/api/v1/projects/" + created.Receipt.ProjectID + "/requests/" + requestID
	expectStatus(t, send(applicant, "POST", requestPath+"/decision", map[string]any{"action": "approve", "reason": "Self review"}, ""), 403)
	approved := decodeCatalogResponse[service.ProjectRequestRecord](t, send(administrator, "POST", requestPath+"/decision", map[string]any{"action": "approve", "reason": "Independent review"}, ""), 200)
	if approved.Status != entity.ProjectRequestApproved || !slices.Equal(approved.RequestedModelIDs, modelIDs) {
		t.Fatal("initial request could not enter existing independent approval", approved)
	}
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.ProjectResource, created.Receipt.ProjectID, []string{}); err != nil {
		t.Fatal(err)
	}
	unchanged("historical initial request retry cannot regrant or recreate", func() {
		retry := create(applicant, valid, reviewed)
		if !reflect.DeepEqual(created.Receipt, retry.Receipt) || retry.Project == nil || len(retry.Project.ModelIDs) != 0 {
			t.Fatal("creation receipt restored an approved then revoked request", retry)
		}
	})
	var receiptAfter entity.ProjectCreationReceipt
	if err := db.Where("creation_id = ?", created.Receipt.CreationID).Take(&receiptAfter).Error; err != nil || !reflect.DeepEqual(receiptBefore, receiptAfter) {
		t.Fatal("current application changed immutable persisted creation receipt", err)
	}
	// Same-intent contention returns one complete durable creation on independent services.
	racing := map[string]any{"name": "Concurrent initial creation", "creation_id": uuid(), "initial_resources": map[string]any{"model_ids": modelIDs, "rpm": 100, "reason": "Concurrent application"}}
	racingETag := creationContext(administrator).ReviewETag
	racingRaw, _ := json.Marshal(racing)
	var racingInput service.ProjectCreationInput
	if err := json.Unmarshal(racingRaw, &racingInput); err != nil {
		t.Fatal(err)
	}
	racingInput.ReviewETag = racingETag
	secondService := makeService()
	beforeConcurrent := capture()
	type result struct {
		value *service.ProjectCreationResult
		err   error
	}
	outcomes := make(chan result, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	gate := make(chan struct{})
	for _, instance := range []*service.Service{svc, secondService} {
		go func() {
			ready.Done()
			<-gate
			value, err := instance.CreateProjectWithInitialResources(ctx, admin.User.ID, racingInput)
			outcomes <- result{value, err}
		}()
	}
	ready.Wait()
	close(gate)
	var racingProject string
	var racingReceipt service.ProjectCreationReceiptRecord
	for range 2 {
		outcome := <-outcomes
		if outcome.err != nil || outcome.value == nil || !outcome.value.Committed {
			t.Fatal("same intent could not reconcile concurrent creation", outcome.err)
		}
		if racingProject != "" && racingProject != outcome.value.Receipt.ProjectID {
			t.Fatal("same creation UUID produced two Projects")
		}
		if outcome.value.Receipt.CreationID != racingInput.CreationID || racingProject != "" && !reflect.DeepEqual(racingReceipt, outcome.value.Receipt) {
			t.Fatal("same creation UUID returned different immutable receipts")
		}
		racingProject = outcome.value.Receipt.ProjectID
		racingReceipt = outcome.value.Receipt
	}
	afterConcurrent := capture()
	resourceChanges := make([]int64, len(tables)-1)
	for i := range resourceChanges {
		resourceChanges[i] = afterConcurrent[i] - beforeConcurrent[i]
	}
	if !slices.Equal(resourceChanges, []int64{1, 1, 2, 0, 0, 1, 0, 1}) {
		t.Fatal("concurrent intent duplicated creation resources", beforeConcurrent, afterConcurrent)
	}
	// Direct initial limits produce their own audit in the same transaction.
	// Inspect this exact Project rather than counting unrelated global audits.
	var racingAudits []entity.AuditEvent
	if err := db.Where("resource_id = ?", racingProject).Find(&racingAudits).Error; err != nil {
		t.Fatal(err)
	}
	if len(racingAudits) != 3 {
		t.Fatalf("concurrent Project requires exactly three atomic audits, got %d", len(racingAudits))
	}
	seenActions := map[string]bool{}
	for _, event := range racingAudits {
		expectedType := "project"
		if event.Action == "resource.create" {
			expectedType = string(service.ProjectResource)
		}
		if event.ResourceID != racingProject || event.ResourceType != expectedType || event.ActorID != admin.User.ID || seenActions[event.Action] {
			t.Fatalf("concurrent Project audit attribution: action=%q type=%q expected_type=%q target=%q expected_target=%q actor=%q expected_actor=%q duplicate=%t", event.Action, event.ResourceType, expectedType, event.ResourceID, racingProject, event.ActorID, admin.User.ID, seenActions[event.Action])
		}
		seenActions[event.Action] = true
		switch event.Action {
		case "resource.create":
		case "limits.update":
			var detail struct {
				After        limits.Policy
				Reason, ETag string
			}
			var storedPolicy entity.ResourceLimit
			if event.DetailsJSON == nil || json.Unmarshal([]byte(*event.DetailsJSON), &detail) != nil || db.Where("scope_kind = ? AND scope_id = ?", "project", racingProject).Take(&storedPolicy).Error != nil || detail.Reason != "Concurrent application" || detail.ETag != storedPolicy.ETag || detail.After.RPM == nil || *detail.After.RPM != 100 {
				t.Fatal("concurrent Project limit audit lost reviewed policy/revision")
			}
		case "project.creation.commit":
			var detail struct {
				CreationID        string   `json:"creation_id"`
				ProjectID         string   `json:"project_id"`
				Reason            string   `json:"reason"`
				ManagerCount      int      `json:"manager_count"`
				ModelCount        int      `json:"model_count"`
				InitialRequestIDs []string `json:"initial_request_ids"`
			}
			if event.DetailsJSON == nil || json.Unmarshal([]byte(*event.DetailsJSON), &detail) != nil || detail.CreationID != racingInput.CreationID || detail.ProjectID != racingProject || detail.Reason != "Concurrent application" || detail.ManagerCount != 1 || detail.ModelCount != len(modelIDs) || len(detail.InitialRequestIDs) != 0 {
				t.Fatal("concurrent Project commit audit lost exact creation intent")
			}
		default:
			t.Fatal("concurrent Project recorded an unexpected audit action", event.Action)
		}
	}
	// Currency generation changes require renewed review for fresh intents only.
	currency, err := svc.GetPricingCurrency(ctx, admin.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	stale := creationContext(administrator).ReviewETag
	if _, err := svc.WritePricingCurrency(ctx, admin.User.ID, currency.ETag, pricing.FX{PlatformCurrency: "USD", Rates: map[string]string{"USD": "1", "EUR": "2"}}); err != nil {
		t.Fatal(err)
	}
	unchanged("stale initial denomination generation", func() {
		expectStatus(t, send(administrator, "POST", "/api/v1/projects", map[string]any{"name": "Stale review", "creation_id": uuid()}, stale), 409)
	})
	// A saved receipt is returned even when publication cannot confirm enforcement.
	uncertainInput := map[string]any{"name": "Durable publication outage", "creation_id": uuid(), "initial_resources": map[string]any{"model_ids": modelIDs, "tokens_month": 10, "reason": "Saved atomic resources"}}
	uncertainETag := creationContext(administrator).ReviewETag
	failPublication.Store(true)
	uncertain := create(administrator, uncertainInput, uncertainETag)
	failPublication.Store(false)
	if !uncertain.Committed || uncertain.RuntimeApplied || uncertain.ApplicationStatus != "pending" || uncertain.Project == nil {
		t.Fatal("publication outage confused durable commit with enforcement", uncertain)
	}
	unchanged("identical publication retry", func() {
		retry := create(administrator, uncertainInput, uncertainETag)
		if !reflect.DeepEqual(uncertain.Receipt, retry.Receipt) || !retry.RuntimeApplied || retry.ApplicationStatus != "applied" {
			t.Fatal("saved creation did not reconcile publication separately", retry)
		}
	})
	// Actual native completion consumes only the new Project account's finite initial cap.
	directInput := map[string]any{"name": "Native initial resources", "creation_id": uuid(), "manager_ids": []string{admin.User.ID, applicant.id}, "initial_resources": map[string]any{"model_ids": []string{modelIDs[0]}, "tokens_month": 5, "money_month": "100.000000000000000001", "currency": "USD", "rpm": 100, "tpm": 100, "concurrency": 1, "reason": "Exact initial application"}}
	directETag := creationContext(administrator).ReviewETag
	direct := create(administrator, directInput, directETag)
	projectID := direct.Receipt.ProjectID
	var stored entity.ResourceLimit
	if err := db.Where("scope_kind = ? AND scope_id = ?", "project", projectID).Take(&stored).Error; err != nil || stored.MoneyMonth == nil || *stored.MoneyMonth != "100.000000000000000001" || stored.TokensMonth == nil || *stored.TokensMonth != 5 || stored.RPM == nil || *stored.RPM != 100 || stored.TPM == nil || *stored.TPM != 100 || stored.Concurrency == nil || *stored.Concurrency != 1 {
		t.Fatal("direct creation lost exact initial policy", stored, err)
	}
	createKey := func(models []string) *service.CreatedProjectKey {
		t.Helper()
		key, err := svc.CreateProjectKey(ctx, admin.User.ID, projectID, "Initial native Key", "manual", models, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.ConfirmProjectKey(ctx, admin.User.ID, projectID, key.Record.Key.ID); err != nil {
			t.Fatal(err)
		}
		return key
	}
	oldKey := createKey([]string{modelIDs[0]})
	expectStatus(t, native(oldKey.Secret, "initial-project-a"), 200)
	beforeDenied := dispatches.Load()
	expectStatus(t, native(oldKey.Secret, "initial-project-a"), 429)
	if dispatches.Load() != beforeDenied {
		t.Fatal("finite initial Project token exhaustion dispatched")
	}
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	var call entity.CallRecord
	// Project Key calls belong to the Project independently of the issuing manager.
	if err := db.Where("key_id = ? AND status = ?", oldKey.Record.Key.ID, "success").Take(&call).Error; err != nil || call.ProjectID != projectID || call.UserID != "" || call.TeamID != "" || call.TeamMembershipID != "" || call.KeyID != oldKey.Record.Key.ID || call.ModelID != modelIDs[0] || call.InputTokens == nil || *call.InputTokens != 4 || call.OutputTokens == nil || *call.OutputTokens != 1 {
		t.Fatal("initial native call lost immutable Project attribution/usage", call, err)
	}
	var attempts []entity.CallAttempt
	if err := db.Where("request_id = ?", call.RequestID).Find(&attempts).Error; err != nil || len(attempts) != 1 || attempts[0].Status != "success" || attempts[0].NativeCompletionEvidence != "completed" || !attempts[0].FinalUsageKnown || attempts[0].CredentialID != "crd_initial_project" || attempts[0].SnapshotID == "" {
		t.Fatal("HTTP success did not retain exact native completion evidence", attempts, err)
	}
	policy, err := svc.GetResourceLimit(ctx, admin.User.ID, service.LimitTarget{Kind: "project", ID: projectID})
	if err != nil || policy.QuotaUsage == nil || policy.QuotaUsage.Month == nil || !policy.QuotaUsage.Month.Covered || policy.QuotaUsage.Month.TokensUsed != 5 || policy.QuotaUsage.Month.TokensHeld != 0 || policy.QuotaUsage.Month.MoneyUsed["USD"] != "5" {
		t.Fatal("initial native call did not settle its exact Project account", policy, err)
	}
	cap := int64(1000)
	if _, err := svc.SetResourceLimit(ctx, admin.User.ID, service.LimitTarget{Kind: "project", ID: projectID}, policy.ETag, service.LimitInput{Policy: limits.Policy{TokensMonth: &cap}, Reason: "Explicit later policy"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.ProjectResource, projectID, modelIDs); err != nil {
		t.Fatal(err)
	}
	beforeDenied = dispatches.Load()
	expectStatus(t, native(oldKey.Secret, "initial-project-b"), 404)
	if dispatches.Load() != beforeDenied {
		t.Fatal("later grants widened the original Project Key")
	}
	newKey := createKey([]string{modelIDs[1]})
	expectStatus(t, native(newKey.Secret, "initial-project-b"), 200)
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.ProjectResource, projectID, []string{modelIDs[1]}); err != nil {
		t.Fatal(err)
	}
	unchanged("superseded creation cannot restore grants or policy", func() {
		retry := create(administrator, directInput, directETag)
		if retry.Project == nil || !slices.Equal(retry.Project.ModelIDs, []string{modelIDs[1]}) || retry.RuntimeApplied || retry.ApplicationStatus != "superseded" || !reflect.DeepEqual(direct.Receipt, retry.Receipt) {
			t.Fatal("historical creation restored superseded resources", retry)
		}
	})
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.ProjectResource, projectID, []string{}); err != nil {
		t.Fatal(err)
	}
	beforeDenied = dispatches.Load()
	expectStatus(t, native(newKey.Secret, "initial-project-b"), 404)
	if dispatches.Load() != beforeDenied {
		t.Fatal("revoked initial Project grant remained invocable")
	}
	var ceiling []entity.ProjectKeyModel
	if err := db.Where("key_id = ?", oldKey.Record.Key.ID).Find(&ceiling).Error; err != nil || len(ceiling) != 1 || ceiling[0].ModelID != modelIDs[0] {
		t.Fatal("initial creation reconciliation changed immutable Key ceiling", err)
	}
	// Current authority may disappear; original enabled actors retain only their own receipt.
	if _, err := svc.SetProjectManagers(ctx, admin.User.ID, created.Receipt.ProjectID, []string{peer.id}); err != nil {
		t.Fatal(err)
	}
	unchanged("departed creator historical receipt", func() {
		retry := create(applicant, valid, reviewed)
		if retry.Project != nil || retry.RuntimeApplied || retry.ApplicationStatus != "unavailable" || !reflect.DeepEqual(created.Receipt, retry.Receipt) {
			t.Fatal("historical receipt disclosed current Project after departure", retry)
		}
	})
	if _, err := svc.SetProjectManagers(ctx, admin.User.ID, created.Receipt.ProjectID, []string{applicant.id, peer.id}); err != nil {
		t.Fatal(err)
	}
	unchanged("rejoined managers do not restore original relationship proof", func() {
		retry := create(applicant, valid, reviewed)
		if retry.Project == nil || retry.RuntimeApplied || retry.ApplicationStatus != "superseded" {
			t.Fatal("rejoining recreated original creation authority", retry)
		}
	})
	lifecycleInput := map[string]any{"name": "Creation lifecycle boundary", "creation_id": uuid()}
	lifecycleETag := creationContext(applicant).ReviewETag
	lifecycle := create(applicant, lifecycleInput, lifecycleETag)
	for _, status := range []string{entity.ResourceDisabled, entity.ResourceArchived} {
		if _, err := svc.UpdateResource(ctx, admin.User.ID, service.ProjectResource, lifecycle.Receipt.ProjectID, service.ResourceUpdate{Status: &status}); err != nil {
			t.Fatal(err)
		}
		unchanged("inactive Project remains historical creation", func() {
			retry := create(applicant, lifecycleInput, lifecycleETag)
			if retry.Project == nil || retry.Project.Status != status || retry.RuntimeApplied || retry.ApplicationStatus != "superseded" || !reflect.DeepEqual(lifecycle.Receipt, retry.Receipt) {
				t.Fatal("creation retry recreated or enabled an inactive Project", retry)
			}
		})
	}
	// Restart retains the Session and immutable receipt while current application remains independent.
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	svc = makeService()
	router = fox.New()
	New(svc).RegisterRoutes(router)
	start()
	unchanged("creation receipt survives Service restart", func() {
		retry := create(applicant, valid, reviewed)
		if !reflect.DeepEqual(created.Receipt, retry.Receipt) || retry.ApplicationStatus != "superseded" {
			t.Fatal("restart changed historical initial creation", retry)
		}
	})
	if err := db.Where("creation_id = ?", created.Receipt.CreationID).Take(&receiptAfter).Error; err != nil || !reflect.DeepEqual(receiptBefore, receiptAfter) {
		t.Fatal("restart changed persisted creation receipt", err)
	}
}
