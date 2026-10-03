package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
)

type projectListQueryContext struct{}

// The root-owned isolated harness runs list scope, projections and literal ID
// adapter behavior against both supported databases, without gateway dispatch.
func testProjectListLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	var queries atomic.Int64
	const callback = "project_list_context_queries"
	countQuery := func(tx *gorm.DB) {
		if tx.Statement.Context.Value(projectListQueryContext{}) != nil {
			queries.Add(1)
		}
	}
	// Register before any service background work starts; remove only after all
	// that work stops. authDB discards loggers, so logger instrumentation cannot
	// prove the query bound of this actual authenticated read.
	if err := db.Callback().Query().Before("gorm:query").Register(callback, countQuery); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().Before("gorm:row").Register(callback, countQuery); err != nil {
		if cleanupErr := db.Callback().Query().Remove(callback); cleanupErr != nil {
			t.Error(cleanupErr)
		}
		t.Fatal(err)
	}
	defer func() {
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
		if err := db.Callback().Row().Remove(callback); err != nil {
			t.Error(err)
		}
	}()
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		svc.StopRuntime()
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
	}()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"project-list-admin@example.invalid","password":"test-only-project-list","name":"Project list administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	manager, managerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "list-manager", nil)
	peer, _, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "list-peer", nil)
	outsider, outsiderCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "list-outsider", nil)
	reader, readerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "list-reader", []string{"projects.read_all"})
	writer, writerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "list-writer", []string{"projects.read_all", "projects.write"})
	_, modelCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "list-model-only", []string{"projects.models.write"})
	_, limitCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "list-limit-only", []string{"projects.limits.write"})
	aliasReader, aliasCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "list-aliased-role", []string{"projects.read_all"})
	var aliasRoles []entity.UserRole
	if err := db.Where("user_id = ?", aliasReader.User.ID).Find(&aliasRoles).Error; err != nil {
		t.Fatal("load alias role fixture", err)
	}
	if err := db.Where("user_id = ?", aliasReader.User.ID).Delete(&entity.UserRole{}).Error; err != nil {
		t.Fatal("remove canonical alias role fixture", err)
	}
	for _, role := range aliasRoles {
		role.UserID = strings.ToUpper(aliasReader.User.ID)
		// An exact FK may reject this legacy alias before the application sees
		// it. A case-insensitive FK may retain it, but neither grants authority.
		if err := db.Create(&role).Error; err != nil && !errors.Is(err, gorm.ErrForeignKeyViolated) {
			t.Fatal("persist alias role fixture", err)
		}
	}
	// These persisted upgrade-style rows deliberately include inactive resources,
	// retained Keys and absent policies rather than invoking creation defaults.
	projects := make([]entity.Project, 20)
	for index := range projects {
		projects[index] = entity.Project{ID: fmt.Sprintf("prj_list_%02d", index), Name: fmt.Sprintf("Project row %02d", index), Status: entity.ResourceActive, CreatorID: admin.User.ID}
	}
	projects[0].ID = "prj_list_Aa_00"
	projects[0].Name = "Alpha literal% marker"
	projects[1].Name = "needle_ marker"
	projects[2].Name = "needle! marker"
	projects[3].Name = "Café exact accent"
	projects[1].Status = entity.ResourceDisabled
	projects[2].Status = entity.ResourceArchived
	if err := db.Create(&projects).Error; err != nil {
		t.Fatal(err)
	}
	relations := []entity.ProjectManager{
		{ID: "pmg_list_first", ProjectID: projects[0].ID, UserID: manager.User.ID},
		{ID: "pmg_list_second", ProjectID: projects[1].ID, UserID: manager.User.ID},
		{ID: "pmg_list_second_peer", ProjectID: projects[1].ID, UserID: peer.User.ID},
		{ID: "pmg_list_peer", ProjectID: projects[0].ID, UserID: peer.User.ID},
		{ID: "pmg_list_outsider", ProjectID: projects[2].ID, UserID: outsider.User.ID},
		{ID: "pmg_list_project_alias", ProjectID: strings.ToUpper(projects[0].ID), UserID: outsider.User.ID},
		{ID: "pmg_list_user_alias", ProjectID: projects[3].ID, UserID: strings.ToUpper(manager.User.ID)},
		{ID: "pmg_list_accent_peer", ProjectID: projects[3].ID, UserID: peer.User.ID},
	}
	canonicalRelations := append(append([]entity.ProjectManager{}, relations[:5]...), relations[7])
	if err := db.Create(&canonicalRelations).Error; err != nil {
		t.Fatal("persist canonical managers", err)
	}
	for _, relation := range relations[5:7] {
		if err := db.Create(&relation).Error; err != nil && !errors.Is(err, gorm.ErrForeignKeyViolated) {
			t.Fatal("persist alias manager fixture", err)
		}
	}
	models := []entity.Model{{ID: "mdl_list_canonical", Status: entity.ResourceActive}, {ID: "mdl_list_alias", Status: entity.ResourceActive}, {ID: "mdl_list_owner_alias", Status: entity.ResourceActive}}
	if err := db.Create(&models).Error; err != nil {
		t.Fatal(err)
	}
	grants := []entity.ProjectModelGrant{
		{ProjectID: projects[0].ID, ModelID: models[0].ID},
		{ProjectID: projects[0].ID, ModelID: strings.ToUpper(models[1].ID)},
		{ProjectID: strings.ToUpper(projects[0].ID), ModelID: models[0].ID},
	}
	if err := db.Create(&grants[0]).Error; err != nil {
		t.Fatal("persist canonical Model grant", err)
	}
	// Separate aliases let exact FKs reject them without rolling back the real
	// grant. A third Model avoids a collation-equivalent composite primary key.
	grants[2].ModelID = models[2].ID
	for _, grant := range grants[1:] {
		if err := db.Create(&grant).Error; err != nil && !errors.Is(err, gorm.ErrForeignKeyViolated) {
			t.Fatal("persist alias grant fixture", err)
		}
	}
	old := time.Now().UTC().Add(-time.Hour)
	keys := []entity.ProjectKey{}
	for index, status := range []string{entity.KeyActive, entity.KeyDisabled, entity.KeyRevoked, entity.KeyPending, entity.KeyActive} {
		row := entity.ProjectKey{ID: fmt.Sprintf("pky_list_%d", index), ProjectID: projects[0].ID, CreatorID: admin.User.ID, Name: "Retained Key", Prefix: "rx_test", TokenHash: secret.SHA256Hex(fmt.Sprintf("project-list-%d", index)), Status: status, DeliveryMode: "manual"}
		if index == 4 {
			row.ExpiresAt = &old
		}
		keys = append(keys, row)
	}
	if err := db.Create(&keys).Error; err != nil {
		t.Fatal("persist canonical retained Keys", err)
	}
	aliasKey := entity.ProjectKey{ID: "pky_list_owner_alias", ProjectID: strings.ToUpper(projects[0].ID), CreatorID: admin.User.ID, Name: "Aliased ownership", Prefix: "rx_test", TokenHash: secret.SHA256Hex("project-list-alias"), Status: entity.KeyActive, DeliveryMode: "manual"}
	if err := db.Create(&aliasKey).Error; err != nil && !errors.Is(err, gorm.ErrForeignKeyViolated) {
		t.Fatal("persist alias Key fixture", err)
	}
	zero, finite := int64(0), int64(99)
	amount, moneyZero := "0.000000000000000001", "0"
	policies := []entity.ResourceLimit{
		{ScopeKind: "project", ScopeID: projects[0].ID, ETag: "lim_list_zero", ActorID: admin.User.ID, Reason: "Recorded exact policy", TokensMonth: &zero, MoneyMonth: &amount, Currency: "USD", RPM: &zero, TPM: &finite, IPMode: "none", IPRangesJSON: "[]"},
		{ScopeKind: "project", ScopeID: projects[1].ID, ETag: "lim_list_unlimited", ActorID: admin.User.ID, Reason: "Recorded unlimited policy", IPMode: "none", IPRangesJSON: "[]"},
		{ScopeKind: "project", ScopeID: projects[2].ID, ETag: "lim_list_currency", ActorID: admin.User.ID, Reason: "Recorded historical denomination", MoneyMonth: &moneyZero, Currency: "EUR", IPMode: "none", IPRangesJSON: "[]"},
		{ScopeKind: "project", ScopeID: strings.ToUpper(projects[3].ID), ETag: "lim_list_owner_alias", ActorID: admin.User.ID, Reason: "Aliased scope is not ownership", TokensMonth: &finite, IPMode: "none", IPRangesJSON: "[]"},
	}
	if err := db.Create(&policies).Error; err != nil {
		t.Fatal(err)
	}
	defaultRow := entity.DefaultLimitRule{Kind: "user", TokensMonth: &finite, RPM: &finite, RuleETag: secret.SHA256Hex("project-list-user-default"), ActorID: admin.User.ID, Reason: "Personal creation default must not become a Project summary"}
	if err := db.Save(&defaultRow).Error; err != nil {
		t.Fatal(err)
	}
	list := func(cookie *http.Cookie, admin bool, query string) ProjectsResponse {
		t.Helper()
		path := "/api/v1/projects"
		if admin {
			path = "/api/v1/admin/projects"
		}
		res := identityRequest(router, "GET", path+"?"+query, "", cookie, "")
		if res.Code != http.StatusOK {
			t.Fatalf("Project list %s query=%q: status=%d want=200 body=%s", path, query, res.Code, res.Body.String())
		}
		return decodeCatalogResponse[ProjectsResponse](t, res, 200)
	}
	find := func(page ProjectsResponse, id string) ProjectResponse {
		t.Helper()
		for _, item := range page.Items {
			if item.ID == id {
				return item
			}
		}
		t.Fatalf("Project %s missing from list %+v", id, page)
		return ProjectResponse{}
	}
	personal := list(managerCookie, false, "")
	if len(personal.Items) != 2 {
		t.Fatalf("current manager scope included aliases or global records: %+v", personal)
	}
	first := find(personal, projects[0].ID)
	if first.KeyCount == nil || *first.KeyCount != 5 || first.Limits != nil || len(first.Managers) != 2 || !reflect.DeepEqual(first.ModelIDs, []string{models[0].ID}) {
		t.Fatalf("personal total-Key/current relationship projection changed: %+v", first)
	}
	second := find(personal, projects[1].ID)
	if second.KeyCount == nil || *second.KeyCount != 0 || second.Limits != nil {
		t.Fatal("known zero Keys or member summary scope changed", second)
	}
	if page := list(adminCookie, false, ""); len(page.Items) != 0 {
		t.Fatal("administrator personal list fetched global directory", page)
	}
	if page := list(outsiderCookie, false, ""); len(page.Items) != 1 || page.Items[0].ID != projects[2].ID {
		t.Fatal("aliased Project relationship granted access", page)
	}
	global := list(readerCookie, true, "limit=100")
	if len(global.Items) != 20 {
		t.Fatal("bounded administrator list omitted resources", global)
	}
	for _, item := range global.Items {
		if item.KeyCount != nil || item.Limits == nil {
			t.Fatal("read_all borrowed Key authority or lost independent limit read", item)
		}
	}
	first = find(global, projects[0].ID)
	if !first.Limits.Stored || first.Limits.TokensMonth == nil || *first.Limits.TokensMonth != 0 || first.Limits.MoneyMonth == nil || *first.Limits.MoneyMonth != amount || first.Limits.Currency != "USD" || first.Limits.RPM == nil || *first.Limits.RPM != 0 || first.Limits.TPM == nil || *first.Limits.TPM != 99 {
		t.Fatal("stored zero/decimal/currency/rates changed", first.Limits)
	}
	second = find(global, projects[1].ID)
	if !second.Limits.Stored || second.Limits.TokensMonth != nil || second.Limits.MoneyMonth != nil || second.Limits.Currency != "" || second.Limits.RPM != nil || second.Limits.TPM != nil {
		t.Fatal("stored unlimited policy became missing/default", second.Limits)
	}
	third := find(global, projects[2].ID)
	if third.Limits.MoneyMonth == nil || *third.Limits.MoneyMonth != "0" || third.Limits.Currency != "EUR" {
		t.Fatal("historical denomination/zero amount was inferred from platform", third.Limits)
	}
	absent := find(global, projects[3].ID)
	if !reflect.DeepEqual(absent.Limits, &service.ProjectListLimits{}) {
		t.Fatal("absent/aliased policy became default or current stored policy", absent.Limits)
	}
	if full := find(list(writerCookie, true, ""), projects[0].ID); full.KeyCount == nil || *full.KeyCount != 5 {
		t.Fatal("independent Project Key authority not reflected", full)
	}
	for _, cookie := range []*http.Cookie{managerCookie, outsiderCookie, modelCookie, limitCookie, aliasCookie} {
		expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/projects", "", cookie, ""), 403)
	}
	for _, test := range []struct {
		query string
		count int
	}{
		{"alpha", 1}, {"literal%", 1}, {"needle_", 1}, {"needle!", 1}, {"' OR 1=1", 0},
		{projects[0].ID, 1}, {"_Aa_", 1}, {"_aa_", 0}, {strings.ToUpper(projects[0].ID), 0}, {projects[0].ID + " ", 0}, {"prj_list_%", 0},
	} {
		if page := list(readerCookie, true, "q="+url.QueryEscape(test.query)); len(page.Items) != test.count {
			t.Fatalf("literal name/ID query %q returned %d, want %d", test.query, len(page.Items), test.count)
		}
	}
	// Exercise the database adapter itself with non-ASCII and wildcard data;
	// ordinary name search intentionally keeps its existing case-fold semantics.
	for _, test := range []struct {
		fragment string
		count    int64
	}{
		{"Café", 1}, {"Cafe", 0}, {"café", 0}, {"literal%", 1},
		{"literal_", 0}, {"needle_", 1}, {"needle!", 1}, {"' OR 1=1", 0},
		{projects[0].Name + " ", 0},
	} {
		var count int64
		query := db.Model(&entity.Project{}).Where(database.ExactTextContains(db, clause.Column{Name: "name"}, test.fragment))
		if err := query.Count(&count).Error; err != nil || count != test.count {
			t.Fatalf("case-sensitive literal adapter %q returned %d, want %d: %v", test.fragment, count, test.count, err)
		}
	}
	if page := list(managerCookie, false, "q="+url.QueryEscape(projects[2].ID)); len(page.Items) != 0 {
		t.Fatal("ID search escaped current manager scope", page)
	}
	if page := list(readerCookie, true, "status=disabled"); len(page.Items) != 1 || page.Items[0].ID != projects[1].ID {
		t.Fatal("status filtering changed", page)
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		page := list(readerCookie, true, "limit=2&cursor="+url.QueryEscape(cursor))
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatal("pagination repeated Project", item.ID)
			}
			seen[item.ID] = true
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if len(seen) != 20 {
		t.Fatal("pagination lost Project rows", len(seen))
	}
	// Count only this context, excluding concurrent recorder/runtime reads.
	measure := func(limit int) int64 {
		t.Helper()
		queries.Store(0)
		traceCtx := context.WithValue(ctx, projectListQueryContext{}, true)
		page, err := svc.ListResources(traceCtx, writer.User.ID, service.ProjectResource, true, service.ResourceFilter{Limit: limit})
		if err != nil || len(page.Items) != limit {
			t.Fatalf("measured list failed: %+v, %v", page, err)
		}
		return queries.Load()
	}
	small, large := measure(1), measure(20)
	if small != large || large > 12 || large < 6 {
		t.Fatalf("Project list performs per-row queries: small=%d large=%d", small, large)
	}
	if _, err := svc.ListResources(ctx, strings.ToUpper(manager.User.ID), service.ProjectResource, false, service.ResourceFilter{}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("aliased actor read Project list", err)
	}
	if _, err := svc.ListResources(ctx, manager.User.ID+" ", service.ProjectResource, false, service.ResourceFilter{}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("padded actor read Project list", err)
	}
	if _, err := svc.SetProjectManagers(ctx, admin.User.ID, projects[0].ID, []string{peer.User.ID}); err != nil {
		t.Fatal(err)
	}
	if page := list(managerCookie, false, ""); len(page.Items) != 1 || page.Items[0].ID != projects[1].ID {
		t.Fatal("removed manager retained list authority", page)
	}
	var readerAssignments []entity.UserRole
	if err := db.Where("user_id = ?", reader.User.ID).Find(&readerAssignments).Error; err != nil {
		t.Fatal(err)
	}
	readerRoleIDs := make([]string, 0, len(readerAssignments))
	for _, assignment := range readerAssignments {
		if assignment.UserID != reader.User.ID {
			t.Fatal("reader role fixture identity changed")
		}
		readerRoleIDs = append(readerRoleIDs, assignment.RoleID)
	}
	if len(readerRoleIDs) != 1 {
		t.Fatal("reader current role fixture missing")
	}
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, reader.User.ID, nil); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/projects", "", readerCookie, ""), 403)
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, writer.User.ID, readerRoleIDs); err != nil {
		t.Fatal(err)
	}
	if item := find(list(writerCookie, true, ""), projects[0].ID); item.KeyCount != nil || item.Limits == nil {
		t.Fatal("revoked Key authority retained private count", item)
	}
	disabled := true
	if _, err := svc.UpdateMember(ctx, admin.User.ID, manager.User.ID, &disabled, nil); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/projects", "", managerCookie, ""), 401)
	// A fresh response includes nullable fields without exposing private Key data.
	detail := decodeCatalogResponse[ProjectResponse](t, identityRequest(router, "GET", "/api/v1/projects/"+projects[0].ID, "", adminCookie, ""), 200)
	if detail.KeyCount != nil || detail.Limits != nil {
		t.Fatal("list-only fields leaked into detail contract", detail)
	}
	encoded, err := json.Marshal(find(list(adminCookie, true, ""), projects[0].ID))
	if err != nil || strings.Contains(string(encoded), "token_hash") || strings.Contains(string(encoded), "rx_test") || strings.Contains(string(encoded), keys[0].TokenHash) {
		t.Fatal("list projection exposed Key metadata", err)
	}
}
