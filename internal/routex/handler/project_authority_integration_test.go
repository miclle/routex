package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
)

// The shared harness runs this against each freshly migrated supported database.
func testProjectAuthorityLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	var publicationOutage atomic.Bool
	const callback = "project_authority_publication_outage"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if publicationOutage.Load() && tx.Statement.Table == "providers" {
			_ = tx.AddError(errors.New("controlled Project publication outage"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	store, err := secretstore.New(bytes.Repeat([]byte{121}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var dispatches atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatches.Add(1)
		if r.Header.Get("Authorization") != "Bearer project-authority-upstream" || r.Header.Get("Cookie") != "" {
			t.Error("unexpected upstream authentication")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatCompletionFixture("stop", `{"role":"assistant","content":"Project result"}`, `{"prompt_tokens":3,"completion_tokens":1}`, false, 0))
	}))
	defer upstream.Close()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		publicationOutage.Store(false)
		svc.StopRuntime()
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	}()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"project-authority-admin@example.invalid","password":"test-only-project-authority","name":"Project authority administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	manager, managerCookie, managerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "authority-manager", nil)
	successor, _, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "authority-successor", nil)
	outsider, outsiderCookie, outsiderCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "authority-outsider", nil)
	platform, platformCookie, platformCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "authority-platform", []string{"projects.write", "projects.models.write"})
	inactive, _, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "authority-inactive", nil)
	offboarded, _, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "authority-offboarded", nil)
	if err := db.Model(&entity.User{}).Where("id = ?", inactive.User.ID).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Model(&entity.User{}).Where("id = ?", offboarded.User.ID).Update("offboarded_at", &now).Error; err != nil {
		t.Fatal(err)
	}
	project, err := svc.CreateResource(ctx, manager.User.ID, service.ProjectResource, "Exact Project authority", "Independent application", nil)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/projects/" + project.ID
	request := func(cookie *http.Cookie, csrf, method, target string, body any) *httptest.ResponseRecorder {
		t.Helper()
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return identityRequest(router, method, target, string(encoded), cookie, csrf)
	}
	adminRequest := func(method, target string, body any) *httptest.ResponseRecorder {
		return request(adminCookie, admin.CSRFToken, method, target, body)
	}
	managerRequest := func(method, target string, body any) *httptest.ResponseRecorder {
		return request(managerCookie, managerCSRF, method, target, body)
	}
	outsiderRequest := func(method, target string, body any) *httptest.ResponseRecorder {
		return request(outsiderCookie, outsiderCSRF, method, target, body)
	}
	platformRequest := func(method, target string, body any) *httptest.ResponseRecorder {
		return request(platformCookie, platformCSRF, method, target, body)
	}
	cipher, err := store.Seal("crd_project_authority", "project-authority-upstream")
	if err != nil {
		t.Fatal(err)
	}
	modelOne, modelTwo := "mdl_project_authority_one", "mdl_project_authority_two"
	for _, row := range []any{
		&entity.Provider{ID: "prv_project_authority", Name: "Project authority provider"},
		&entity.ProviderConnection{ID: "con_project_authority", ProviderID: "prv_project_authority", Name: "Chat", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_project_authority", ConnectionID: "con_project_authority", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_project_authority", ConnectionID: "con_project_authority", UpstreamName: "native"},
		&entity.CredentialModelAccess{CredentialID: "crd_project_authority", ProviderModelID: "pmd_project_authority"},
		&entity.Model{ID: modelOne, Status: entity.ResourceActive}, &entity.Model{ID: modelTwo, Status: entity.ResourceActive},
		&entity.ModelName{Name: "authority-one", ModelID: modelOne, CurrentModelID: &modelOne}, &entity.ModelName{Name: "authority-two", ModelID: modelTwo, CurrentModelID: &modelTwo},
		&entity.ModelProviderBinding{ID: "bnd_authority_one", ModelID: modelOne, ProviderModelID: "pmd_project_authority", Weight: 100},
		&entity.ModelProviderBinding{ID: "bnd_authority_two", ModelID: modelTwo, ProviderModelID: "pmd_project_authority", Weight: 100},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	expectStatus(t, platformRequest("PUT", path+"/models", map[string]any{"model_ids": []string{modelOne, modelTwo}}), 200)
	if err := svc.StartCallRecorder(ctx, filepath.Join(t.TempDir(), "project-authority-calls.db")); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	refresh := func() {
		t.Helper()
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
	}
	refresh()
	created := decodeCatalogResponse[CreatedProjectKeyResponse](t, managerRequest("POST", path+"/keys", map[string]any{"name": "Authority application", "model_ids": []string{modelOne, modelTwo}, "delivery_mode": "manual"}), 201)
	expectStatus(t, managerRequest("POST", path+"/keys/"+created.Key.ID+"/confirm", nil), 200)
	native := func(model string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"`+model+`","messages":[{"role":"user","content":"Project scope"}]}`))
		req.Header.Set("Authorization", "Bearer "+created.Secret)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	expectStatus(t, native("authority-one"), 200)
	expectStatus(t, native("authority-two"), 200)
	type state struct {
		Projects             []entity.Project
		Managers             []entity.ProjectManager
		Grants               []entity.ProjectModelGrant
		Audits, Publications int64
		Snapshot             string
	}
	capture := func() state {
		t.Helper()
		result := state{Snapshot: svc.RuntimeStatus().SnapshotID}
		for _, query := range []*gorm.DB{db.Order("id").Find(&result.Projects), db.Order("id").Find(&result.Managers), db.Order("project_id,model_id").Find(&result.Grants), db.Model(&entity.AuditEvent{}).Count(&result.Audits), db.Model(&entity.RuntimePublication{}).Count(&result.Publications)} {
			if query.Error != nil {
				t.Fatal(query.Error)
			}
		}
		return result
	}
	unchanged := func(label string, operation func()) {
		t.Helper()
		before := capture()
		operation()
		if after := capture(); !reflect.DeepEqual(before, after) {
			t.Fatalf("%s changed Project authority, audit, or publication: before=%+v after=%+v", label, before, after)
		}
	}
	managerBody := func(ids ...string) any { return map[string]any{"user_ids": append([]string{}, ids...)} }
	modelsBody := func(ids ...string) any { return map[string]any{"model_ids": append([]string{}, ids...)} }
	for _, selected := range []string{strings.ToUpper(successor.User.ID), "usr_authority_missing", inactive.User.ID, offboarded.User.ID} {
		unchanged("invalid selected manager "+selected, func() {
			expectStatus(t, managerRequest("PUT", path+"/managers", managerBody(manager.User.ID, selected)), 400)
		})
	}
	unchanged("empty manager continuity", func() { expectStatus(t, managerRequest("PUT", path+"/managers", managerBody()), 409) })
	unchanged("nonmanager replacement", func() { expectStatus(t, outsiderRequest("PUT", path+"/managers", managerBody(outsider.User.ID)), 403) })
	unchanged("manager model escalation", func() { expectStatus(t, managerRequest("PUT", path+"/models", modelsBody(modelOne)), 403) })
	aliasPath := "/api/v1/projects/" + strings.ToUpper(project.ID)
	unchanged("direct target alias", func() {
		expectStatus(t, adminRequest("PUT", aliasPath+"/managers", managerBody(manager.User.ID)), 404)
		expectStatus(t, adminRequest("PUT", aliasPath+"/models", modelsBody(modelOne)), 404)
	})
	unchanged("manager target alias", func() {
		expectStatus(t, managerRequest("PUT", aliasPath+"/managers", managerBody(manager.User.ID)), 403)
	})
	for _, ids := range [][]string{{strings.ToUpper(modelOne)}, {modelOne, modelOne}, {"mdl_authority_missing"}} {
		unchanged("invalid model selection", func() { expectStatus(t, platformRequest("PUT", path+"/models", modelsBody(ids...)), 400) })
	}
	for _, actorID := range []string{strings.ToUpper(admin.User.ID), inactive.User.ID, offboarded.User.ID} {
		unchanged("invalid service actor", func() {
			_, managerErr := svc.SetProjectManagers(ctx, actorID, project.ID, []string{manager.User.ID})
			_, modelErr := svc.SetResourceModels(ctx, actorID, service.ProjectResource, project.ID, []string{modelOne})
			for _, denied := range []error{managerErr, modelErr} {
				var failure *apperrors.Error
				if !errors.As(denied, &failure) || failure.Code != 401 {
					t.Fatalf("actor %s was not rejected as unauthorized: %v", actorID, denied)
				}
			}
		})
	}
	// Some databases reject malformed foreign identities at the schema boundary;
	// permissive foreign-key collations must still fail the service authority check.
	raw := func(label string, row any, denied func()) {
		t.Helper()
		if err := db.Create(row).Error; err != nil {
			if !errors.Is(err, gorm.ErrForeignKeyViolated) {
				t.Fatalf("%s unexpected raw-association error: %v", label, err)
			}
			return
		}
		unchanged(label, denied)
		if err := db.Delete(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, relation := range []entity.ProjectManager{
		{ID: "pmg_authority_alias_user", ProjectID: project.ID, UserID: strings.ToUpper(outsider.User.ID)},
		{ID: "pmg_authority_alias_project", ProjectID: strings.ToUpper(project.ID), UserID: outsider.User.ID},
	} {
		raw("raw manager identity alias", &relation, func() { expectStatus(t, outsiderRequest("PUT", path+"/managers", managerBody(outsider.User.ID)), 403) })
	}
	privileged, err := svc.SaveRole(ctx, admin.User.ID, "", "Project alias authority", []string{"projects.write", "projects.models.write"})
	if err != nil {
		t.Fatal(err)
	}
	denyBoth := func() {
		expectStatus(t, outsiderRequest("PUT", path+"/managers", managerBody(outsider.User.ID)), 403)
		expectStatus(t, outsiderRequest("PUT", path+"/models", modelsBody(modelOne)), 403)
	}
	for _, association := range []entity.UserRole{{UserID: strings.ToUpper(outsider.User.ID), RoleID: privileged.Role.ID}, {UserID: outsider.User.ID, RoleID: strings.ToUpper(privileged.Role.ID)}} {
		raw("raw user-role identity alias", &association, denyBoth)
	}
	if err := db.Create(&entity.UserRole{UserID: outsider.User.ID, RoleID: privileged.Role.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("role_id = ?", privileged.Role.ID).Delete(&entity.RolePermission{}).Error; err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"projects.write", "projects.models.write"} {
		if err := db.Create(&entity.RolePermission{RoleID: privileged.Role.ID, Permission: strings.ToUpper(action)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	unchanged("raw permission case aliases", denyBoth)
	if err := db.Where("role_id = ?", privileged.Role.ID).Delete(&entity.RolePermission{}).Error; err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"projects.write", "projects.models.write"} {
		row := entity.RolePermission{RoleID: strings.ToUpper(privileged.Role.ID), Permission: action}
		raw("raw permission role alias", &row, denyBoth)
	}
	if err := db.Delete(&entity.UserRole{UserID: outsider.User.ID, RoleID: privileged.Role.ID}).Error; err != nil {
		t.Fatal(err)
	}
	// A corrupt association cannot lend its historical identity to a canonical
	// replacement, while another exact retained manager keeps its original ID.
	for _, column := range []string{"project_id", "user_id"} {
		repairProject, err := svc.CreateResource(ctx, manager.User.ID, service.ProjectResource, "Repair "+column, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		repairPath := "/api/v1/projects/" + repairProject.ID + "/managers"
		original := decodeCatalogResponse[ProjectResponse](t, platformRequest("PUT", repairPath, managerBody(manager.User.ID, successor.User.ID)), 200)
		var corruptID, retainedID string
		for _, relation := range original.Managers {
			switch relation.UserID {
			case manager.User.ID:
				corruptID = relation.ID
			case successor.User.ID:
				retainedID = relation.ID
			}
		}
		if corruptID == "" || retainedID == "" {
			t.Fatal("repair fixture did not retain both canonical managers")
		}
		alias := strings.ToUpper(repairProject.ID)
		if column == "user_id" {
			alias = strings.ToUpper(manager.User.ID)
		}
		if err := db.Model(&entity.ProjectManager{}).Where("id = ?", corruptID).Update(column, alias).Error; err != nil {
			if !errors.Is(err, gorm.ErrForeignKeyViolated) {
				t.Fatalf("unexpected corrupt manager %s error: %v", column, err)
			}
			continue
		}
		repaired := decodeCatalogResponse[ProjectResponse](t, platformRequest("PUT", repairPath, managerBody(manager.User.ID, successor.User.ID)), 200)
		if len(repaired.Managers) != 2 {
			t.Fatal("canonical replacement did not restore exact manager set")
		}
		for _, relation := range repaired.Managers {
			switch relation.UserID {
			case manager.User.ID:
				if relation.ID == corruptID {
					t.Fatal("canonical manager replacement borrowed corrupt association identity")
				}
			case successor.User.ID:
				if relation.ID != retainedID {
					t.Fatal("canonical retained manager identity changed during repair")
				}
			default:
				t.Fatal("canonical replacement retained an aliased manager")
			}
		}
	}
	expectStatus(t, native("authority-two"), 200)
	beforeManagers := decodeCatalogResponse[ProjectResponse](t, managerRequest("GET", path, nil), 200)
	added := decodeCatalogResponse[ProjectResponse](t, managerRequest("PUT", path+"/managers", managerBody(manager.User.ID, successor.User.ID)), 200)
	retained := false
	for _, row := range added.Managers {
		if row.UserID == manager.User.ID {
			retained = row.ID == beforeManagers.Managers[0].ID
		}
	}
	if !retained || len(added.Managers) != 2 {
		t.Fatal("canonical retained manager identity changed")
	}
	stateRouter, stateRuntime := memberStateRuntimeFixtureRouter(t, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	defer stateRuntime.StopRuntime()
	// A direct platform editor can replace the full set without becoming a manager.
	expectStatus(t, platformRequest("PUT", path+"/managers", managerBody(successor.User.ID)), 200)
	unchanged("creator has no permanent management", func() { expectStatus(t, managerRequest("PUT", path+"/managers", managerBody(manager.User.ID)), 403) })
	unchanged("sole active manager cannot be disabled", func() {
		expectStatus(t, reviewedMemberStateFixtureRequest(t, stateRouter, adminCookie, admin.CSRFToken, successor.User.ID, map[string]any{"disabled": true}), 409)
	})
	var creator entity.Project
	if err := db.First(&creator, "id = ?", project.ID).Error; err != nil {
		t.Fatal(err)
	}
	if creator.CreatorID != manager.User.ID {
		t.Fatal("management replacement rewrote immutable creator")
	}
	refresh()
	// Project Keys belong to the Project, independently of the issuing manager.
	expectStatus(t, native("authority-one"), 200)
	expectStatus(t, native("authority-two"), 200)
	publicationOutage.Store(true)
	expectStatus(t, platformRequest("PUT", path+"/models", modelsBody(modelOne)), 503)
	beforeDispatch := dispatches.Load()
	// Until publication succeeds, the Project tombstone rejects the whole Key.
	expectStatus(t, native("authority-two"), 401)
	if dispatches.Load() != beforeDispatch {
		t.Fatal("grant removal dispatched through an obsolete runtime")
	}
	publicationOutage.Store(false)
	refresh()
	expectStatus(t, native("authority-one"), 200)
	expectStatus(t, native("authority-two"), 404)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	var historical int64
	if err := db.Model(&entity.CallRecord{}).Where("project_id = ? AND model_id = ?", project.ID, modelTwo).Count(&historical).Error; err != nil {
		t.Fatal(err)
	}
	if historical == 0 {
		t.Fatal("grant replacement discarded Project call history")
	}
	expectStatus(t, adminRequest("PATCH", path, map[string]any{"status": "disabled"}), 200)
	// Disabled Projects preserve canonical governance; archived Projects are terminal.
	expectStatus(t, platformRequest("PUT", path+"/managers", managerBody(successor.User.ID)), 200)
	expectStatus(t, adminRequest("PATCH", path, map[string]any{"status": "archived"}), 200)
	unchanged("archived manager replacement", func() { expectStatus(t, platformRequest("PUT", path+"/managers", managerBody(platform.User.ID)), 409) })
	unchanged("archived model replacement", func() { expectStatus(t, platformRequest("PUT", path+"/models", modelsBody()), 409) })
}
