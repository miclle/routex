package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
)

func testTeamRolesLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var publicationOutage, auditOutage atomic.Bool
	const queryCallback = "team_roles_publication_outage"
	const auditCallback = "team_roles_audit_outage"
	// Callback structure is immutable before any runtime or recorder starts.
	if err := db.Callback().Query().Before("gorm:query").Register(queryCallback, func(tx *gorm.DB) {
		if publicationOutage.Load() && tx.Statement.Table == "providers" {
			_ = tx.AddError(errors.New("controlled publication outage"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register(auditCallback, func(tx *gorm.DB) {
		if auditOutage.Load() && tx.Statement.Table == "audit_events" {
			_ = tx.AddError(errors.New("controlled assignment audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	store, err := secretstore.New(bytes.Repeat([]byte{123}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var dispatches atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatches.Add(1)
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-CSRF-Token") != "" {
			t.Error("Session proof leaked upstream")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`)
	}))
	defer upstream.Close()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		publicationOutage.Store(false)
		auditOutage.Store(false)
		svc.StopRuntime()
		_ = svc.StopCallRecorder()
		_ = db.Callback().Query().Remove(queryCallback)
		_ = db.Callback().Create().Remove(auditCallback)
	}()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"team-roles-admin@example.invalid","password":"test-only-team-roles-password","name":"Roles administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	owner, ownerCookie, ownerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "roles-owner", nil)
	member, memberCookie, memberCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "roles-member", nil)
	_, outsiderCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "roles-outsider", nil)
	extra, _, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "roles-extra", nil)
	_, platformCookie, platformCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "roles-platform", []string{"teams.write", "teams.models.write", "teams.read_all"})
	_, quotaCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "roles-quota", []string{"teams.tokens.write"})
	team, err := svc.CreateResource(ctx, admin.User.ID, service.TeamResource, "Scoped roles Team", "Target actions", []string{owner.User.ID})
	if err != nil {
		t.Fatal(err)
	}
	members := []service.TeamMemberInput{{UserID: owner.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: member.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, team.ID, members); err != nil {
		t.Fatal(err)
	}
	other, err := svc.CreateResource(ctx, admin.User.ID, service.TeamResource, "Other roles Team", "No assignment", []string{owner.User.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, other.ID, members); err != nil {
		t.Fatal(err)
	}
	mixedPermissions := []string{"teams.write", "teams.models.write", "teams.tokens.write", "teams.money.write", "teams.quota_requests.read_all", "projects.write", "providers.write"}
	mixed, err := svc.SaveRole(ctx, admin.User.ID, "", "Scoped mixed role", mixedPermissions)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := svc.SaveRole(ctx, admin.User.ID, "", "Scoped metadata role", []string{"teams.write"})
	if err != nil {
		t.Fatal(err)
	}
	spare, err := svc.SaveRole(ctx, admin.User.ID, "", "Scoped spare role", []string{"teams.models.write"})
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := store.Seal("crd_team_roles", "test-only-role-upstream")
	if err != nil {
		t.Fatal(err)
	}
	modelOne, modelTwo := "mdl_team_roles_one", "mdl_team_roles_two"
	personalBearer := "rx_" + strings.Repeat("r", 43)
	create := func(rows ...any) {
		t.Helper()
		for _, row := range rows {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	create(&entity.Provider{ID: "prv_team_roles", Name: "Role route provider"},
		&entity.ProviderConnection{ID: "con_team_roles", ProviderID: "prv_team_roles", Name: "Chat", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_team_roles", ConnectionID: "con_team_roles", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_team_roles", ConnectionID: "con_team_roles", UpstreamName: "native"},
		&entity.CredentialModelAccess{CredentialID: "crd_team_roles", ProviderModelID: "pmd_team_roles"},
		&entity.Model{ID: modelOne, Status: entity.ResourceActive}, &entity.Model{ID: modelTwo, Status: entity.ResourceActive},
		&entity.ModelName{Name: "roles-one", ModelID: modelOne, CurrentModelID: &modelOne}, &entity.ModelName{Name: "roles-two", ModelID: modelTwo, CurrentModelID: &modelTwo},
		&entity.ModelProviderBinding{ID: "bnd_team_roles_one", ModelID: modelOne, ProviderModelID: "pmd_team_roles", Weight: 100}, &entity.ModelProviderBinding{ID: "bnd_team_roles_two", ModelID: modelTwo, ProviderModelID: "pmd_team_roles", Weight: 100},
		&entity.UserModelGrant{UserID: member.User.ID, ModelID: modelOne},
		&entity.APIKey{ID: "key_team_roles", UserID: member.User.ID, Name: "Personal isolation", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(personalBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_team_roles", ModelID: modelOne})
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, team.ID, []string{modelOne}); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, filepath.Join(t.TempDir(), "roles-calls.db")); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	refresh := func() {
		t.Helper()
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
	}
	request := func(method, path, body string, cookie *http.Cookie, csrf, etag string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	rolePath := "/api/v1/teams/" + team.ID + "/roles"
	get := func(cookie *http.Cookie) service.TeamRolesRecord {
		t.Helper()
		res := request("GET", rolePath, "", cookie, "", "")
		expectStatus(t, res, 200)
		var record service.TeamRolesRecord
		if json.Unmarshal(res.Body.Bytes(), &record) != nil || record.TeamID != team.ID || res.Header().Get("ETag") != `"`+record.ETag+`"` || record.RoleIDs == nil || record.Roles == nil || record.ActorTeamActions == nil || record.EffectiveTeamActions == nil {
			t.Fatal("unsafe or incoherent role record", res.Body.String())
		}
		return record
	}
	body := func(ids []string, reason string) string {
		raw, err := json.Marshal(service.TeamRoleInput{RoleIDs: ids, Reason: reason})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	put := func(ids []string, etag string) *httptest.ResponseRecorder {
		return request("PUT", rolePath, body(ids, "Reviewed scoped role replacement"), adminCookie, admin.CSRFToken, etag)
	}
	auditCount := func() int64 {
		var n int64
		if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "team.roles.replace", team.ID).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	baselineGlobal := identityRequest(router, "GET", "/api/v1/auth/permissions", "", memberCookie, "")
	expectStatus(t, baselineGlobal, 200)
	initial := get(adminCookie)
	memberInitial := get(memberCookie)
	if !initial.CanAssignRoles || memberInitial.CanAssignRoles || len(memberInitial.ActorTeamActions) != 0 {
		t.Fatal("member/owner acquired implicit actions")
	}
	expectStatus(t, request("PATCH", "/api/v1/admin/teams/"+team.ID, `{"name":"Forbidden owner write"}`, ownerCookie, ownerCSRF, ""), 403)
	expectStatus(t, request("GET", rolePath, "", outsiderCookie, "", ""), 404)
	expectStatus(t, request("GET", rolePath, "", quotaCookie, "", ""), 404)
	expectStatus(t, request("GET", rolePath, "", nil, "", ""), 401)
	expectStatus(t, request("GET", "/api/v1/teams/"+strings.ToUpper(team.ID)+"/roles", "", adminCookie, "", ""), 404)
	if get(platformCookie).CanAssignRoles {
		t.Fatal("custom platform role became protected administrator")
	}
	expectStatus(t, request("PUT", rolePath, body([]string{mixed.Role.ID}, "Custom platform role cannot assign"), platformCookie, platformCSRF, initial.ETag), 403)
	expectStatus(t, request("GET", "/api/v1/teams/"+team.ID+"/role-candidates", "", platformCookie, "", ""), 403)
	for _, invalid := range []string{`{"role_ids":null,"reason":"Null"}`, `{"role_ids":[],"reason":""}`, `{"role_ids":[],"reason":"Duplicate","reason":"Duplicate"}`, `{"role_ids":[],"reason":"Extra","permissions":[]}`, `{"role_ids":[1],"reason":"Wrong type"}`} {
		expectStatus(t, request("PUT", rolePath, invalid, adminCookie, admin.CSRFToken, initial.ETag), 400)
	}
	expectStatus(t, request("PUT", rolePath, body([]string{mixed.Role.ID, mixed.Role.ID}, "Duplicate ids"), adminCookie, admin.CSRFToken, initial.ETag), 400)
	expectStatus(t, request("PUT", rolePath, body([]string{strings.ToUpper(mixed.Role.ID)}, "Alias id"), adminCookie, admin.CSRFToken, initial.ETag), 400)
	expectStatus(t, request("PUT", rolePath, body([]string{mixed.Role.ID}, "No CSRF"), adminCookie, "", initial.ETag), 403)
	for _, validator := range []string{"", initial.ETag, `W/"` + initial.ETag + `"`, `"` + strings.ToUpper(initial.ETag) + `"`} {
		req := httptest.NewRequest("PUT", "http://routex.test"+rolePath, strings.NewReader(body([]string{mixed.Role.ID}, "Strong review")))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", validator)
		req.Header.Set("X-CSRF-Token", admin.CSRFToken)
		req.AddCookie(adminCookie)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		expectStatus(t, res, 400)
	}
	foreign := httptest.NewRequest("PUT", "http://routex.test"+rolePath, strings.NewReader(body([]string{mixed.Role.ID}, "Origin review")))
	foreign.Header.Set("Content-Type", "application/json")
	foreign.Header.Set("Origin", "https://foreign.example.invalid")
	foreign.Header.Set("If-Match", `"`+initial.ETag+`"`)
	foreign.Header.Set("X-CSRF-Token", admin.CSRFToken)
	foreign.AddCookie(adminCookie)
	foreignResponse := httptest.NewRecorder()
	router.ServeHTTP(foreignResponse, foreign)
	expectStatus(t, foreignResponse, 403)
	candidates := request("GET", "/api/v1/teams/"+team.ID+"/role-candidates?limit=1", "", adminCookie, "", "")
	expectStatus(t, candidates, 200)
	var page service.TeamRoleCandidatePage
	if json.Unmarshal(candidates.Body.Bytes(), &page) != nil || page.ETag != initial.ETag || len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatal("incoherent bounded candidates", candidates.Body.String())
	}
	expectStatus(t, request("GET", "/api/v1/teams/"+other.ID+"/role-candidates?limit=1&cursor="+url.QueryEscape(*page.NextCursor), "", adminCookie, "", ""), 409)
	expectStatus(t, request("GET", "/api/v1/teams/"+team.ID+"/role-candidates?q=wrong", "", adminCookie, "", ""), 400)
	literal := request("GET", "/api/v1/teams/"+team.ID+"/role-candidates?query=%25", "", adminCookie, "", "")
	expectStatus(t, literal, 200)
	var literalPage service.TeamRoleCandidatePage
	if json.Unmarshal(literal.Body.Bytes(), &literalPage) != nil || len(literalPage.Items) != 0 {
		t.Fatal("role wildcard was not literal")
	}
	// Assignment commits independently of provider publication; no runtime claim.
	snapshot := svc.RuntimeStatus().SnapshotID
	publicationOutage.Store(true)
	saved := put([]string{mixed.Role.ID}, initial.ETag)
	expectStatus(t, saved, 200)
	publicationOutage.Store(false)
	assigned := get(adminCookie)
	var savedRecord service.TeamRolesRecord
	if json.Unmarshal(saved.Body.Bytes(), &savedRecord) != nil || savedRecord.ETag != assigned.ETag || saved.Header().Get("ETag") != `"`+assigned.ETag+`"` {
		t.Fatal("saved role validator differs from persisted authorized read", saved.Body.String(), assigned.ETag)
	}
	savedCandidates := request("GET", "/api/v1/teams/"+team.ID+"/role-candidates", "", adminCookie, "", "")
	expectStatus(t, savedCandidates, 200)
	var savedPage service.TeamRoleCandidatePage
	if json.Unmarshal(savedCandidates.Body.Bytes(), &savedPage) != nil || savedPage.ETag != savedRecord.ETag || savedCandidates.Header().Get("ETag") != saved.Header().Get("ETag") {
		t.Fatal("saved candidate catalogue changed without a mutation", savedCandidates.Body.String(), savedRecord.ETag)
	}
	scoped := get(memberCookie)
	actions := []string{"teams.models.write", "teams.write"}
	if !reflect.DeepEqual(scoped.ActorTeamActions, actions) || !reflect.DeepEqual(scoped.EffectiveTeamActions, actions) || scoped.CanAssignRoles || len(scoped.Roles) != 1 || !reflect.DeepEqual(scoped.Roles[0].TeamActions, actions) {
		t.Fatal("Team role union escaped allowlist", scoped)
	}
	afterGlobal := identityRequest(router, "GET", "/api/v1/auth/permissions", "", memberCookie, "")
	expectStatus(t, afterGlobal, 200)
	if afterGlobal.Body.String() != baselineGlobal.Body.String() {
		t.Fatal("Team assignment changed global authority")
	}
	refresh()
	if svc.RuntimeStatus().SnapshotID != snapshot {
		t.Fatal("authorization-only assignment perturbed route snapshot")
	}
	if auditCount() != 1 {
		t.Fatal("assignment audit missing")
	}
	expectStatus(t, put([]string{mixed.Role.ID}, initial.ETag), 409)
	expectStatus(t, put([]string{mixed.Role.ID}, assigned.ETag), 200)
	if auditCount() != 1 || get(adminCookie).ETag != assigned.ETag {
		t.Fatal("current no-op changed assignment generation or audited twice")
	}
	native := func(teamScope bool, model string) *httptest.ResponseRecorder {
		t.Helper()
		refresh()
		path := "/v1/chat/completions"
		req := httptest.NewRequest("POST", "http://routex.test"+path, strings.NewReader(`{"model":"`+model+`","messages":[{"role":"user","content":"Hello"}],"max_completion_tokens":1}`))
		if teamScope {
			path = "/api/v1/teams/" + team.ID + "/chat/completions"
			req.URL.Path = path
			req.AddCookie(memberCookie)
			req.Header.Set("X-CSRF-Token", memberCSRF)
			req.Header.Set("Origin", "http://routex.test")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
		} else {
			req.Header.Set("Authorization", "Bearer "+personalBearer)
		}
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	expectStatus(t, native(true, "roles-one"), 200)
	expectStatus(t, native(false, "roles-one"), 200)
	expectStatus(t, request("GET", "/api/v1/teams/"+other.ID+"/member-candidates", "", memberCookie, "", ""), 403)
	expectStatus(t, request("GET", "/api/v1/admin/team-member-candidates", "", memberCookie, "", ""), 403)
	expectStatus(t, request("GET", "/api/v1/admin/resource-model-candidates?kind=teams", "", memberCookie, "", ""), 403)
	expectStatus(t, request("GET", "/api/v1/admin/teams", "", memberCookie, "", ""), 403)
	expectStatus(t, request("GET", "/api/v1/admin/members", "", memberCookie, "", ""), 403)
	expectStatus(t, request("GET", "/api/v1/admin/quota-requests", "", memberCookie, "", ""), 403)
	expectStatus(t, request("GET", "/api/v1/teams/"+team.ID+"/role-candidates", "", memberCookie, "", ""), 403)
	expectStatus(t, request("PUT", rolePath, body([]string{mixed.Role.ID}, "Scoped assignment cannot be delegated"), memberCookie, memberCSRF, scoped.ETag), 403)
	expectStatus(t, request("PATCH", "/api/v1/admin/teams/"+team.ID, `{"status":"disabled"}`, memberCookie, memberCSRF, ""), 403)
	expectStatus(t, request("PATCH", "/api/v1/admin/teams/"+team.ID, `{"name":"Delegated Team name"}`, memberCookie, memberCSRF, ""), 200)
	expectStatus(t, request("GET", "/api/v1/teams/"+team.ID+"/member-candidates?query=roles-extra", "", memberCookie, "", ""), 200)
	modelCandidates := request("GET", "/api/v1/teams/"+team.ID+"/model-candidates?query=roles-", "", memberCookie, "", "")
	expectStatus(t, modelCandidates, 200)
	if strings.Contains(modelCandidates.Body.String(), "provider") || strings.Contains(modelCandidates.Body.String(), "ciphertext") {
		t.Fatal("candidate endpoint disclosed internal route metadata")
	}
	expectStatus(t, request("GET", "/api/v1/teams/"+team.ID+"/model-candidates?q=wrong", "", memberCookie, "", ""), 400)
	limits := request("GET", "/api/v1/teams/"+team.ID+"/limits", "", memberCookie, "", "")
	expectStatus(t, limits, 200)
	var limit service.LimitRecord
	if json.Unmarshal(limits.Body.Bytes(), &limit) != nil {
		t.Fatal(limits.Body.String())
	}
	expectStatus(t, request("PUT", "/api/v1/teams/"+team.ID+"/limits", `{"tokens_month":1,"reason":"No dimension inheritance"}`, memberCookie, memberCSRF, limit.ETag), 403)
	members = append(members, service.TeamMemberInput{UserID: extra.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	if _, err := svc.SetTeamMembers(ctx, member.User.ID, team.ID, members); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetResourceModels(ctx, member.User.ID, service.TeamResource, team.ID, []string{modelTwo}); err != nil {
		t.Fatal(err)
	}
	deniedDispatches := dispatches.Load()
	expectStatus(t, native(true, "roles-one"), 404)
	if dispatches.Load() != deniedDispatches {
		t.Fatal("revoked Team model dispatched")
	}
	expectStatus(t, native(true, "roles-two"), 200)
	expectStatus(t, native(false, "roles-one"), 200)
	expectStatus(t, native(false, "roles-two"), 404)
	// Definition ABA must reject the original review even after public facts return to A.
	for _, role := range []service.RoleRecord{*mixed, *spare} {
		for _, edit := range []string{"name", "team_actions", "platform_actions"} {
			before := get(adminCookie)
			beforeMember := get(memberCookie)
			beforeAudit, beforeDispatches := auditCount(), dispatches.Load()
			var beforeTeam entity.Team
			if err := db.First(&beforeTeam, "id = ?", team.ID).Error; err != nil {
				t.Fatal(err)
			}
			definitionRevision := func() string {
				t.Helper()
				var values []string
				if err := db.Model(&entity.Role{}).Where("id = ?", role.Role.ID).Pluck("definition_revision", &values).Error; err != nil || len(values) != 1 {
					t.Fatal("missing exact stored definition generation", role.Role.ID, values, err)
				}
				return values[0]
			}
			originalRevision := definitionRevision()
			cursorResponse := request("GET", "/api/v1/teams/"+team.ID+"/role-candidates?limit=1", "", adminCookie, "", "")
			expectStatus(t, cursorResponse, 200)
			var cursorPage service.TeamRoleCandidatePage
			if json.Unmarshal(cursorResponse.Body.Bytes(), &cursorPage) != nil || cursorPage.ETag != before.ETag || cursorPage.NextCursor == nil {
				t.Fatal("missing reviewed catalogue cursor", cursorResponse.Body.String())
			}
			name, permissions := role.Role.Name, slices.Clone(role.Permissions)
			switch edit {
			case "name":
				name += " temporarily changed"
			case "team_actions":
				if slices.Contains(permissions, "teams.write") {
					permissions = slices.DeleteFunc(permissions, func(p string) bool { return p == "teams.write" })
				} else {
					permissions = append(permissions, "teams.write")
				}
			case "platform_actions":
				permissions = append(permissions, "audit.read")
			}
			if _, err := svc.SaveRole(ctx, admin.User.ID, role.Role.ID, name, permissions); err != nil {
				t.Fatal(err)
			}
			changed := get(adminCookie)
			changedRevision := definitionRevision()
			if changed.ETag == before.ETag || changedRevision == originalRevision {
				t.Fatalf("%s %s did not change reviewed definition generation", role.Role.ID, edit)
			}
			if _, err := svc.SaveRole(ctx, admin.User.ID, role.Role.ID, role.Role.Name, role.Permissions); err != nil {
				t.Fatal(err)
			}
			restored := get(adminCookie)
			restoredRevision := definitionRevision()
			if restored.ETag == before.ETag || restored.ETag == changed.ETag || restoredRevision == originalRevision || restoredRevision == changedRevision {
				t.Fatalf("%s %s ABA revived an obsolete review", role.Role.ID, edit)
			}
			if !reflect.DeepEqual(restored.Roles, before.Roles) || !reflect.DeepEqual(restored.RoleIDs, before.RoleIDs) || !reflect.DeepEqual(restored.ActorTeamActions, before.ActorTeamActions) || !reflect.DeepEqual(restored.EffectiveTeamActions, before.EffectiveTeamActions) {
				t.Fatal("definition ABA did not restore the same public Team role facts")
			}
			memberRestored := get(memberCookie)
			if !reflect.DeepEqual(memberRestored.Roles, beforeMember.Roles) || !reflect.DeepEqual(memberRestored.ActorTeamActions, beforeMember.ActorTeamActions) {
				t.Fatal("definition ABA changed restored scoped member actions")
			}
			if slices.Contains(before.RoleIDs, role.Role.ID) {
				if memberRestored.ETag == beforeMember.ETag {
					t.Fatal("scoped assigned-role review ignored definition ABA")
				}
			} else if memberRestored.ETag != beforeMember.ETag {
				t.Fatal("unassigned catalogue generation leaked into scoped member review")
			}
			expectStatus(t, put(before.RoleIDs, before.ETag), 409)
			expectStatus(t, request("GET", "/api/v1/teams/"+team.ID+"/role-candidates?limit=1&cursor="+url.QueryEscape(*cursorPage.NextCursor), "", adminCookie, "", ""), 409)
			// Explicitly re-reviewed same-state retries remain zero-write confirmations.
			expectStatus(t, put(restored.RoleIDs, restored.ETag), 200)
			if _, err := svc.SaveRole(ctx, admin.User.ID, role.Role.ID, role.Role.Name, role.Permissions); err != nil {
				t.Fatal(err)
			}
			if get(adminCookie).ETag != restored.ETag || definitionRevision() != restoredRevision {
				t.Fatal("no-op role save invalidated an unchanged review")
			}
			var afterTeam entity.Team
			if err := db.First(&afterTeam, "id = ?", team.ID).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(afterTeam, beforeTeam) || auditCount() != beforeAudit || dispatches.Load() != beforeDispatches {
				t.Fatal("definition review or rejected stale retry mutated Team, assignment audit or native dispatch")
			}
			wire := request("GET", rolePath, "", adminCookie, "", "")
			expectStatus(t, wire, 200)
			var projected map[string]json.RawMessage
			if json.Unmarshal(wire.Body.Bytes(), &projected) != nil || len(projected) != 7 {
				t.Fatal("Team role public DTO changed", wire.Body.String())
			}
			var projectedRoles []map[string]json.RawMessage
			if json.Unmarshal(projected["roles"], &projectedRoles) != nil {
				t.Fatal("invalid public roles", wire.Body.String())
			}
			for _, projectedRole := range projectedRoles {
				if len(projectedRole) != 4 || projectedRole["id"] == nil || projectedRole["name"] == nil || projectedRole["builtin"] == nil || projectedRole["team_actions"] == nil {
					t.Fatal("private definition generation leaked into Team role DTO", wire.Body.String())
				}
			}
		}
	}
	currentDefinitionReview := get(adminCookie)
	currentDefinitionAudit := auditCount()
	expectStatus(t, put([]string{mixed.Role.ID, spare.Role.ID}, currentDefinitionReview.ETag), 200)
	currentDefinitionSaved := get(adminCookie)
	currentDefinitionIDs := []string{mixed.Role.ID, spare.Role.ID}
	slices.Sort(currentDefinitionIDs)
	if auditCount() != currentDefinitionAudit+1 || !reflect.DeepEqual(currentDefinitionSaved.RoleIDs, currentDefinitionIDs) {
		t.Fatal("fresh definition review did not save one complete replacement")
	}
	expectStatus(t, put(currentDefinitionSaved.RoleIDs, currentDefinitionSaved.ETag), 200)
	if auditCount() != currentDefinitionAudit+1 || get(adminCookie).ETag != currentDefinitionSaved.ETag {
		t.Fatal("unchanged exact reviewed retry wrote another assignment")
	}
	expectStatus(t, put([]string{mixed.Role.ID}, currentDefinitionSaved.ETag), 200)
	// Fresh role-definition edits revoke management immediately, not model grants.
	obsolete := get(adminCookie)
	if _, err := svc.SaveRole(ctx, admin.User.ID, mixed.Role.ID, mixed.Role.Name, []string{"teams.write", "teams.tokens.write"}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, put([]string{mixed.Role.ID}, obsolete.ETag), 409)
	expectStatus(t, request("GET", "/api/v1/teams/"+team.ID+"/model-candidates", "", memberCookie, "", ""), 403)
	if _, err := svc.SetResourceModels(ctx, member.User.ID, service.TeamResource, team.ID, []string{modelOne}); err == nil {
		t.Fatal("edited role retained model-management authority")
	}
	expectStatus(t, native(true, "roles-two"), 200)
	if _, err := svc.SaveRole(ctx, admin.User.ID, mixed.Role.ID, mixed.Role.Name, []string{"teams.tokens.write"}); err != nil {
		t.Fatal(err)
	}
	inert := get(memberCookie)
	if len(inert.Roles) != 1 || inert.Roles[0].TeamActions == nil || len(inert.ActorTeamActions) != 0 {
		t.Fatal("inert assigned definition was hidden or still effective")
	}
	expectStatus(t, request("PATCH", "/api/v1/admin/teams/"+team.ID, `{"name":"Inert denial"}`, memberCookie, memberCSRF, ""), 403)
	if err := svc.DeleteRole(ctx, admin.User.ID, mixed.Role.ID); err == nil {
		t.Fatal("assigned Team role was deletable")
	}
	if _, err := svc.SaveRole(ctx, admin.User.ID, mixed.Role.ID, mixed.Role.Name, mixedPermissions); err != nil {
		t.Fatal(err)
	}
	// Atomic assignment rollback includes relation and generation, not only audit.
	beforeRollback := get(adminCookie)
	auditBefore := auditCount()
	auditOutage.Store(true)
	expectStatus(t, put([]string{metadata.Role.ID}, beforeRollback.ETag), 500)
	auditOutage.Store(false)
	if get(adminCookie).ETag != beforeRollback.ETag || auditCount() != auditBefore {
		t.Fatal("failed audit left partial role assignment")
	}
	// Concurrent stale proposals serialize: exactly one saves this generation.
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for _, ids := range [][]string{{metadata.Role.ID}, {mixed.Role.ID, metadata.Role.ID}} {
		wg.Add(1)
		go func(ids []string) { defer wg.Done(); results <- put(ids, beforeRollback.ETag).Code }(ids)
	}
	wg.Wait()
	close(results)
	codes := []int{}
	for code := range results {
		codes = append(codes, code)
	}
	slices.Sort(codes)
	if !reflect.DeepEqual(codes, []int{200, 409}) || auditCount() != auditBefore+1 {
		t.Fatal("concurrent assignment did not preserve one reviewed generation", codes)
	}
	current := get(adminCookie)
	expectStatus(t, put([]string{mixed.Role.ID}, current.ETag), 200)
	firstABA := get(adminCookie)
	expectStatus(t, put([]string{metadata.Role.ID}, firstABA.ETag), 200)
	expectStatus(t, put([]string{mixed.Role.ID}, get(adminCookie).ETag), 200)
	if get(adminCookie).ETag == firstABA.ETag {
		t.Fatal("assignment ABA revived obsolete review")
	}
	// Metadata must not rewind a future saved generation during assignment ABA.
	future := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	if err := db.Model(&entity.Team{}).Where("id = ?", team.ID).UpdateColumn("UpdatedAt", future).Error; err != nil {
		t.Fatal(err)
	}
	futureA := get(adminCookie)
	expectStatus(t, put([]string{metadata.Role.ID}, futureA.ETag), 200)
	var beforeMetadata entity.Team
	if err := db.First(&beforeMetadata, "id = ?", team.ID).Error; err != nil {
		t.Fatal(err)
	}
	sameName := beforeMetadata.Name
	if _, err := svc.UpdateResource(ctx, admin.User.ID, service.TeamResource, team.ID, service.ResourceUpdate{Name: &sameName}); err != nil {
		t.Fatal(err)
	}
	var afterMetadata entity.Team
	if err := db.First(&afterMetadata, "id = ?", team.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !afterMetadata.UpdatedAt.After(beforeMetadata.UpdatedAt) {
		t.Fatal("Team metadata rewound or reused persisted assignment generation", beforeMetadata.UpdatedAt, afterMetadata.UpdatedAt)
	}
	expectStatus(t, put([]string{mixed.Role.ID}, get(adminCookie).ETag), 200)
	if get(adminCookie).ETag == futureA.ETag {
		t.Fatal("metadata and assignment ABA revived an old validator")
	}
	expectStatus(t, put([]string{metadata.Role.ID}, futureA.ETag), 409)

	// Candidate deletion changes complete reviewed generation; same intent cannot reconcile.
	reviewedSpare := get(adminCookie)
	if err := svc.DeleteRole(ctx, admin.User.ID, spare.Role.ID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, put([]string{spare.Role.ID}, reviewedSpare.ETag), 409)
	expectStatus(t, request("GET", "/api/v1/teams/"+team.ID+"/role-candidates?limit=1&cursor="+url.QueryEscape(*page.NextCursor), "", adminCookie, "", ""), 409)
	// Case-folded raw role associations never confer authority on a byte-distinct identity.
	alias := strings.ToUpper(mixed.Role.ID)
	aliasResult := db.Model(&entity.TeamRole{}).Where("team_id = ? AND role_id = ?", team.ID, mixed.Role.ID).Update("RoleID", alias)
	if aliasResult.Error == nil {
		expectStatus(t, request("PATCH", "/api/v1/admin/teams/"+team.ID, `{"name":"Alias denied"}`, memberCookie, memberCSRF, ""), 400)
		if err := db.Model(&entity.TeamRole{}).Where("team_id = ? AND role_id = ?", team.ID, alias).Update("RoleID", mixed.Role.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	// A role-derived membership removal commits and locally revokes before failed publication.
	refresh()
	publicationOutage.Store(true)
	withoutMember := []service.TeamMemberInput{members[0], members[2]}
	if _, err := svc.SetTeamMembers(ctx, member.User.ID, team.ID, withoutMember); err == nil {
		t.Fatal("controlled publication failure was not surfaced")
	}
	expectStatus(t, request("GET", rolePath, "", memberCookie, "", ""), 404)
	req := httptest.NewRequest("POST", "http://routex.test/api/v1/teams/"+team.ID+"/chat/completions", strings.NewReader(`{"model":"roles-two","messages":[{"role":"user","content":"Denied"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", memberCSRF)
	req.AddCookie(memberCookie)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	expectStatus(t, res, 403)
	publicationOutage.Store(false)
	refresh()
	expectStatus(t, native(false, "roles-one"), 200)
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, team.ID, members); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, native(true, "roles-two"), 200)
	lifecycle := get(adminCookie)
	disabled, active := entity.ResourceDisabled, entity.ResourceActive
	if _, err := svc.UpdateResource(ctx, admin.User.ID, service.TeamResource, team.ID, service.ResourceUpdate{Status: &disabled}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request("GET", rolePath, "", memberCookie, "", ""), 404)
	if _, err := svc.UpdateResource(ctx, admin.User.ID, service.TeamResource, team.ID, service.ResourceUpdate{Status: &active}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, put([]string{mixed.Role.ID}, lifecycle.ETag), 409)
	expectStatus(t, put([]string{}, get(adminCookie).ETag), 200)
	cleared := get(memberCookie)
	if len(cleared.ActorTeamActions) != 0 || len(cleared.Roles) != 0 || cleared.RoleIDs == nil {
		t.Fatal("clear did not revoke scoped roles")
	}
	if err := svc.DeleteRole(ctx, admin.User.ID, mixed.Role.ID); err != nil {
		t.Fatal("unassigned role remained undeletable", err)
	}
	var audit entity.AuditEvent
	if err := db.Where("action = ? AND resource_id = ?", "team.roles.replace", team.ID).Order("created_at DESC,id DESC").First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if audit.DetailsJSON == nil || !strings.Contains(*audit.DetailsJSON, `"after":{"role_ids":[],"team_actions":[]}`) {
		t.Fatal("removal audit lacked exact safe before/after")
	}
}
