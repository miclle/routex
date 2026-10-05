package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type effectiveModelsQueryMarker struct{}

// Database catalog fixtures prove read/projection/publication, not native
// completion. Every Overview read must make zero upstream requests or writes.
func testMemberEffectiveModelsLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	var mu sync.Mutex
	queries := []string{}
	const callback = "test_member_effective_models_queries"
	observeQuery := func(tx *gorm.DB) {
		if tx.Statement.Context.Value(effectiveModelsQueryMarker{}) != nil {
			mu.Lock()
			queries = append(queries, tx.Statement.SQL.String())
			mu.Unlock()
		}
	}
	if err := db.Callback().Query().After("gorm:query").Register(callback, observeQuery); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().After("gorm:row").Register(callback, observeQuery); err != nil {
		_ = db.Callback().Query().Remove(callback)
		t.Fatal(err)
	}

	var instances []*service.Service
	t.Cleanup(func() {
		for _, s := range instances {
			s.StopRuntime()
			if err := s.StopCallRecorder(); err != nil {
				t.Error(err)
			}
		}
		_ = db.Callback().Row().Remove(callback)
		_ = db.Callback().Query().Remove(callback)
	})
	var dispatches atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { dispatches.Add(1); w.WriteHeader(500) }))
	defer upstream.Close()
	store, err := secretstore.New(bytes.Repeat([]byte{153}, 32))
	if err != nil {
		t.Fatal(err)
	}
	newService := func() *service.Service {
		t.Helper()
		s, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, s)
		return s
	}
	svc := newService()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"effective-admin@example.invalid","password":"effective-test-password","name":"Effective administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	subject, _, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "effective-subject", nil)
	readerRecord, readerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "effective-reader", []string{"members.read"})
	fullRecord, fullCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "effective-full", []string{"members.read", "teams.read_all", "providers.read", "prices.read"})
	_, teamOnlyCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "effective-team-only", []string{"teams.read_all"})
	_, writerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "effective-write-only", []string{"members.models.write"})
	create := func(rows ...any) {
		t.Helper()
		for _, row := range rows {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	create(&entity.Provider{ID: "prv_effective", Name: "Recorded Provider"})
	ids := []string{"mdl_effective_personal", "mdl_effective_team", "mdl_effective_disabled", "mdl_effective_project"}
	for i, id := range ids {
		status := entity.ResourceActive
		if i == 2 {
			status = entity.ResourceDisabled
		}
		create(&entity.Model{ID: id, Status: status}, &entity.ModelName{Name: "effective-model-" + string(rune('a'+i)), ModelID: id, CurrentModelID: &id})
	}
	for i, protocol := range []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		suffix := string(rune('a' + i))
		con, crd, pm := "con_effective_"+suffix, "crd_effective_"+suffix, "pmd_effective_"+suffix
		cipher, err := store.Seal(crd, "controlled-effective-secret")
		if err != nil {
			t.Fatal(err)
		}
		create(&entity.ProviderConnection{ID: con, ProviderID: "prv_effective", Name: suffix, Protocol: protocol, BaseURL: upstream.URL + "/v1", EgressMode: "direct"}, &entity.ProviderCredential{ID: crd, ConnectionID: con, Name: "Controlled verified credential", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"}, &entity.ProviderModel{ID: pm, ConnectionID: con, UpstreamName: "effective-upstream-" + suffix}, &entity.CredentialModelAccess{CredentialID: crd, ProviderModelID: pm}, &entity.ModelPrice{ID: "prc_effective_" + suffix, ProviderModelID: pm, UpdateSource: "api"})
		create(&entity.PriceRate{ID: "rate_effective_in_" + suffix, ModelPriceID: "prc_effective_" + suffix, Metric: pricing.Input, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0", Enabled: true}, &entity.PriceRate{ID: "rate_effective_out_" + suffix, ModelPriceID: "prc_effective_" + suffix, Metric: pricing.Output, Tier: pricing.Base, Unit: pricing.Unit, Currency: "EUR", Amount: "0.000000000000000001", Enabled: true})
		for j, model := range ids {
			create(&entity.ModelProviderBinding{ID: "bnd_effective_" + suffix + string(rune('a'+j)), ModelID: model, ProviderModelID: pm, Weight: 100})
		}
	}
	// Explicit grants and current relationships only; Project authority cannot join.
	if _, err := svc.SetModelGrants(ctx, admin.User.ID, ids[0], []string{subject.User.ID}); err != nil {
		t.Fatal(err)
	}
	create(&entity.UserModelGrant{UserID: subject.User.ID, ModelID: ids[2]})
	for _, teamID := range []string{"tea_effective_a", "tea_effective_b", "tea_effective_inactive"} {
		status := entity.ResourceActive
		if strings.HasSuffix(teamID, "inactive") {
			status = entity.ResourceDisabled
		}
		create(&entity.Team{ID: teamID, Name: "Same recorded Team", Status: status}, &entity.TeamMembership{ID: "tmm_" + strings.TrimPrefix(teamID, "tea_"), TeamID: teamID, UserID: subject.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive})
		for _, model := range ids[:3] {
			grant := entity.TeamModelGrant{TeamID: teamID, ModelID: model}
			if teamID == "tea_effective_a" && model == ids[1] {
				source := "tmr_effective_retained"
				grant.SourceRequestID = &source
			}
			create(&grant)
		}
	}
	create(&entity.Project{ID: "prj_effective", Name: "Unrelated Project", Status: entity.ResourceActive, CreatorID: subject.User.ID}, &entity.ProjectManager{ID: "pmg_effective", ProjectID: "prj_effective", UserID: subject.User.ID}, &entity.ProjectModelGrant{ProjectID: "prj_effective", ModelID: ids[3]})
	createdKey, err := svc.CreatePersonalKey(ctx, subject.User.ID, "Retained ceiling", []string{ids[0]}, nil)
	if err != nil {
		t.Fatal(err)
	}
	createdKey.Secret = ""
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	path := func(user string) string { return "/api/v1/admin/members/" + url.PathEscape(user) + "/effective-models" }
	get := func(cookie *http.Cookie) service.MemberEffectiveModelsPage {
		t.Helper()
		response := identityRequest(router, "GET", path(subject.User.ID), "", cookie, "")
		if response.Code != 200 {
			t.Fatalf("effective read got%d: %s", response.Code, response.Body.String())
		}
		var page service.MemberEffectiveModelsPage
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if page.UserID != subject.User.ID || page.ObservedAt.IsZero() || page.Items == nil || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(page, response.Header())
		}
		for _, field := range []string{"source_request_id", "membership_id", "personal_grant_revision", "ciphertext", "selectable", "receipt", "token_hash"} {
			if strings.Contains(response.Body.String(), `"`+field+`"`) {
				t.Fatal("private fact exposed", field)
			}
		}
		return page
	}
	snapshot := func() map[string][32]byte {
		t.Helper()
		result := map[string][32]byte{}
		for _, table := range []string{"models", "model_names", "model_provider_bindings", "provider_connections", "provider_models", "provider_credentials", "credential_model_accesses", "model_prices", "price_rates", "user_model_grants", "team_model_grants", "project_model_grants", "api_keys", "api_key_models", "resource_limits", "call_records", "audit_events"} {
			var rows []map[string]any
			if err := db.Table(table).Find(&rows).Error; err != nil {
				t.Fatal("immutable table snapshot", table, err)
			}
			encoded := make([]string, 0, len(rows))
			for _, row := range rows {
				raw, err := json.Marshal(row)
				if err != nil {
					t.Fatal(err)
				}
				encoded = append(encoded, string(raw))
			}
			slices.Sort(encoded)
			raw, err := json.Marshal(encoded)
			if err != nil {
				t.Fatal(err)
			}
			result[table] = sha256.Sum256(raw)
		}
		return result
	}
	immutable := snapshot()
	var fullRole entity.UserRole
	if err := db.Where("user_id = ?", fullRecord.User.ID).First(&fullRole).Error; err != nil {
		t.Fatal(err)
	}

	unauthenticated := identityRequest(router, "GET", path(subject.User.ID), "", nil, "")
	expectStatus(t, unauthenticated, 401)
	if unauthenticated.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("authentication denial cache contract")
	}
	for _, cookie := range []*http.Cookie{teamOnlyCookie, writerCookie} {
		denied := identityRequest(router, "GET", path(subject.User.ID), "", cookie, "")
		expectStatus(t, denied, 403)
		if denied.Header().Get("Cache-Control") != "no-store" || strings.Contains(denied.Body.String(), `"items"`) {
			t.Fatal("permission denial leaked or cached models")
		}
	}
	for _, target := range []string{strings.ToUpper(subject.User.ID), "usr_missing"} {
		expectStatus(t, identityRequest(router, "GET", path(target), "", adminCookie, ""), 404)
	}
	expectStatus(t, identityRequest(router, "GET", path(subject.User.ID)+"?team_id=", "", adminCookie, ""), 400)
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	personal := get(readerCookie)
	if personal.TeamEnrichment != "not_authorized" || personal.UnionCompleteness != "unknown" || len(personal.Items) != 2 {
		t.Fatal(personal)
	}
	for _, row := range personal.Items {
		if len(row.Sources) != 1 || row.Sources[0].Kind != "personal" || row.Sources[0].TeamID != nil || row.Providers != nil || row.InputPrice.State != "unauthorized" {
			t.Fatal(row)
		}
	}
	complete := get(fullCookie)
	if complete.TeamEnrichment != "included" || complete.UnionCompleteness != "complete" || len(complete.Items) != 3 {
		t.Fatal(complete)
	}
	for _, row := range complete.Items {
		wantSources := 2
		if row.ID != ids[1] {
			wantSources = 3
		}
		if len(row.Sources) != wantSources || row.Type != nil || row.UpdatedAt != nil || !slices.Equal(row.Providers, []string{"Recorded Provider"}) {
			t.Fatal(row)
		}
		if row.ID == ids[2] {
			if row.Availability != "unavailable" || len(row.Protocols) != 0 {
				t.Fatal(row)
			}
		} else if row.Availability != "ready" || len(row.Protocols) != 4 || row.InputPrice.Rate == nil || row.InputPrice.Rate.Amount != "0" || row.OutputPrice.Rate == nil || row.OutputPrice.Rate.Amount != "0.000000000000000001" {
			t.Fatal(row)
		}
	}
	// Tag only this API transaction: Session middleware/refresh is not its budget.
	observe := func() []string {
		t.Helper()
		mu.Lock()
		queries = nil
		mu.Unlock()
		_, err := svc.MemberEffectiveModels(context.WithValue(ctx, effectiveModelsQueryMarker{}, true), admin.User.ID, subject.User.ID)
		if err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(queries)
	}
	firstQueries := observe()
	if len(firstQueries) != 20 {
		t.Fatalf("direct-egress complete query count got%d want20", len(firstQueries))
	}
	mu.Lock()
	queries = nil
	mu.Unlock()
	if _, err := svc.MemberEffectiveModels(context.WithValue(ctx, effectiveModelsQueryMarker{}, true), readerRecord.User.ID, subject.User.ID); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	personalQueries := slices.Clone(queries)
	mu.Unlock()
	for _, q := range personalQueries {
		if strings.Contains(q, "team_") || strings.Contains(q, "FROM teams") {
			t.Fatal("unauthorized Team SQL", q)
		}
	}
	find := func(page service.MemberEffectiveModelsPage, id string) service.MemberEffectiveModelRow {
		t.Helper()
		for _, row := range page.Items {
			if row.ID == id {
				return row
			}
		}
		t.Fatal("missing exact Model", id)
		return service.MemberEffectiveModelRow{}
	}
	if !reflect.DeepEqual(snapshot(), immutable) {
		t.Fatal("read altered immutable catalog/grants/Key ceiling/accounting/audit")
	}
	// A same-plaintext envelope replacement is still a new private source.
	var credential entity.ProviderCredential
	if err := db.Select("id", "ciphertext").Where("id = ?", "crd_effective_a").First(&credential).Error; err != nil {
		t.Fatal(err)
	}
	newCipher, err := store.Seal(credential.ID, "controlled-effective-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.ProviderCredential{}).Where("id = ?", credential.ID).Update("Ciphertext", newCipher).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range get(fullCookie).Items {
		if row.Status == entity.ResourceActive && (row.Availability != "unknown" || len(row.Protocols) != 0 || row.InputPrice.State != "unavailable") {
			t.Fatal("unpublished source borrowed ready route", row.ID)
		}
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	for _, row := range get(fullCookie).Items {
		if row.Status == entity.ResourceActive && row.Availability != "ready" {
			t.Fatal("authenticated refreshed source remained stale", row.ID)
		}
	}
	if err := db.Model(&entity.ProviderCredential{}).Where("id = ?", credential.ID).Update("Ciphertext", credential.Ciphertext).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot(), immutable) {
		t.Fatal("read/controlled source restoration changed immutable facts")
	}
	// Losing only Team read changes completeness without borrowing cached enrichment.
	if err := db.Where("role_id = ? AND permission = ?", fullRole.RoleID, "teams.read_all").Delete(&entity.RolePermission{}).Error; err != nil {
		t.Fatal(err)
	}
	downgraded := get(fullCookie)
	if downgraded.TeamEnrichment != "not_authorized" || downgraded.UnionCompleteness != "unknown" || len(downgraded.Items) != 2 {
		t.Fatal(downgraded)
	}
	for _, row := range downgraded.Items {
		for _, source := range row.Sources {
			if source.Kind != "personal" || source.TeamID != nil {
				t.Fatal("revoked Team permission exposed source")
			}
		}
	}
	create(&entity.RolePermission{RoleID: fullRole.RoleID, Permission: "teams.read_all"})
	// Preserve the real foreign key while testing exact target identity. A
	// case-sensitive database needs a distinct disabled alias parent; a database
	// that folds the primary key reports the portable duplicate error instead.
	aliasID := strings.ToUpper(subject.User.ID)
	aliasParent := entity.User{ID: aliasID, Email: "effective-alias-parent@example.invalid", Name: "Disabled alias parent", PasswordHash: strings.Repeat("0", 60), Role: entity.RoleMember, Disabled: true}
	aliasErr := db.Create(&aliasParent).Error
	if aliasErr != nil && !errors.Is(aliasErr, gorm.ErrDuplicatedKey) {
		t.Fatal(aliasErr)
	}
	aliasCreated := aliasErr == nil
	// SQL target aliases cannot borrow canonical source relationships.
	if err := db.Model(&entity.TeamMembership{}).Where("id = ?", "tmm_effective_b").Update("UserID", aliasID).Error; err != nil {
		t.Fatal(err)
	}
	aliased := get(fullCookie)
	for _, row := range aliased.Items {
		for _, source := range row.Sources {
			if source.TeamID != nil && *source.TeamID == "tea_effective_b" {
				t.Fatal("aliased membership borrowed target union")
			}
		}
	}
	if err := db.Model(&entity.TeamMembership{}).Where("id = ?", "tmm_effective_b").Update("UserID", subject.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	if aliasCreated {
		removed := db.Where(database.ExactText(db, clause.Column{Name: "id"}, aliasID)).Delete(&entity.User{})
		if removed.Error != nil || removed.RowsAffected != 1 {
			t.Fatal("exact disabled alias parent cleanup", removed.Error, removed.RowsAffected)
		}
	}
	// A larger authorized source set has the identical constant query budget.
	for i := range 20 {
		model := fmt.Sprintf("mdl_effective_extra_%02d", i)
		create(&entity.Model{ID: model, Status: entity.ResourceActive}, &entity.ModelName{Name: fmt.Sprintf("effective-extra-%d", i), ModelID: model, CurrentModelID: &model}, &entity.TeamModelGrant{TeamID: "tea_effective_a", ModelID: model})
	}
	if currentQueries := observe(); len(currentQueries) != len(firstQueries) {
		t.Fatal("Model count introduced per-row reads", len(currentQueries), len(firstQueries))
	}
	// These additional no-route Models remain known unavailable only after exact publication.
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	// Provenance differs on the complete Team set even when the displayed grant persists.
	if err := db.Model(&entity.TeamModelGrant{}).Where("team_id = ? AND model_id = ?", "tea_effective_a", ids[1]).Update("SourceRequestID", "tmr_effective_unpublished").Error; err != nil {
		t.Fatal(err)
	}
	unpublished := find(get(fullCookie), ids[1])
	for _, source := range unpublished.Sources {
		if source.TeamID != nil && *source.TeamID == "tea_effective_a" && (source.Availability != "unknown" || len(source.Protocols) != 0) {
			t.Fatal("unpublished provenance claimed ready")
		}
	}
	if err := db.Model(&entity.TeamModelGrant{}).Where("team_id = ? AND model_id = ?", "tea_effective_a", ids[1]).Update("SourceRequestID", "tmr_effective_retained").Error; err != nil {
		t.Fatal(err)
	}
	// Source reduction is observed without causing a publication from this read.
	if err := db.Where("team_id = ? AND model_id = ?", "tea_effective_a", ids[0]).Delete(&entity.TeamModelGrant{}).Error; err != nil {
		t.Fatal(err)
	}
	after := get(fullCookie)
	if len(find(after, ids[0]).Sources) != 2 {
		t.Fatal("removed source retained", after)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	current := get(fullCookie)
	if !reflect.DeepEqual(find(after, ids[0]).Sources[0], find(current, ids[0]).Sources[0]) {
		t.Fatal("unchanged Personal source drift")
	}
	if err := db.Model(&entity.TeamMembership{}).Where("id = ?", "tmm_effective_b").Update("Status", entity.ResourceDisabled).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	current = get(fullCookie)
	for _, row := range current.Items {
		for _, source := range row.Sources {
			if source.TeamID != nil && *source.TeamID == "tea_effective_b" {
				t.Fatal("disabled membership contributes source")
			}
		}
	}
	if err := db.Model(&entity.User{}).Where("id = ?", subject.User.ID).Update("Disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	current = get(fullCookie)
	if current.SubjectStatus != "disabled" {
		t.Fatal(current)
	}
	for _, row := range current.Items {
		if row.Availability != "unavailable" || len(row.Protocols) != 0 {
			t.Fatal("inactive target callable", row)
		}
	}
	beforeRestart := current
	svc.StopRuntime()
	svc = newService()
	router = fox.New()
	New(svc).RegisterRoutes(router)
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	current = get(fullCookie)
	current.ObservedAt = beforeRestart.ObservedAt
	if !reflect.DeepEqual(current, beforeRestart) {
		t.Fatal("read-only restart changed retained configuration", current, beforeRestart)
	}
	if dispatches.Load() != 0 {
		t.Fatal("Overview dispatched upstream", dispatches.Load())
	}
}
