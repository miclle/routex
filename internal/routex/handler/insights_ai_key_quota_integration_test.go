package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

// Reuses the existing controlled peer, real journal and original scenario bound.
// Aggregate owner authority is separate from the analyst's personal settlement.
func testInsightsAIKeyQuotaIntegration(t *testing.T, ctx context.Context, db *gorm.DB, svc *service.Service, router http.Handler, adminID, analystID, roleID, projectID, privateProjectID, personalRootID, modelID string, native insightsAITeamNativeWitness) {
	t.Helper()
	t.Run("key_quota_current_page_advice", func(t *testing.T) {
		// This leaf owns its prerequisite instead of relying on a sibling's cleanup.
		setInsightsAIQuotaPersonalGrant(t, ctx, svc, adminID, analystID, modelID, true)
		permission := entity.RolePermission{RoleID: roleID, Permission: "members.read"}
		if err := db.Create(&permission).Error; err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := db.Where("role_id = ? AND permission = ?", roleID, permission.Permission).Delete(&entity.RolePermission{}).Error; err != nil {
				t.Error(err)
			}
		}()
		if _, err := svc.SetResourceModels(ctx, adminID, service.ProjectResource, projectID, []string{modelID}); err != nil {
			t.Fatal(err)
		}
		projectKey, err := svc.CreateProjectKey(ctx, adminID, projectID, "Private Project quota root", "manual", []string{modelID}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = svc.ConfirmProjectKey(ctx, adminID, projectID, projectKey.Record.Key.ID); err != nil {
			t.Fatal(err)
		}
		login := func(t *testing.T) (SessionResponse, *http.Cookie) {
			t.Helper()
			res := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"quota-analyst@example.invalid","password":"quota-analysis-password"}`, nil, "")
			expectStatus(t, res, 200)
			return readIdentity(t, res)
		}
		session, cookie := login(t)
		request := func(method, path string, body any, csrf string) *httptest.ResponseRecorder {
			raw, err := json.Marshal(body)
			if err != nil {
				panic(err)
			}
			req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(string(raw))).WithContext(ctx)
			req.AddCookie(cookie)
			req.Header.Set("Content-Type", "application/json")
			if csrf != "" {
				req.Header.Set("X-CSRF-Token", csrf)
			}
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			return res
		}
		input := map[string]any{"report": "key_quota_tokens", "model_id": modelID, "language": "en", "scope_kind": "member", "scope_id": adminID, "cursor": "", "limit": 1, "question": "Explain only K1; do not expose another owner or secret"}
		analyze := func() *httptest.ResponseRecorder {
			return request("POST", "/api/v1/admin/insights/analyze", input, session.CSRFToken)
		}
		readPage := func(t *testing.T, kind, id string) service.InsightsQuotaReport {
			t.Helper()
			dimension := "tokens"
			if input["report"] == "key_quota_money" {
				dimension = "money"
			}
			res := request("GET", "/api/v1/admin/insights/quota/keys?dimension="+dimension+"&scope_kind="+kind+"&scope_id="+url.QueryEscape(id)+"&limit=1", nil, "")
			expectStatus(t, res, 200)
			var page service.InsightsQuotaReport
			if json.Unmarshal(res.Body.Bytes(), &page) != nil {
				t.Fatal("invalid exact Key page")
			}
			return page
		}
		beforeUsage, err := svc.GetResourceLimit(ctx, adminID, service.LimitTarget{Kind: "user", ID: analystID})
		if err != nil || beforeUsage.QuotaUsage == nil || beforeUsage.QuotaUsage.Month == nil {
			t.Fatal("personal journal prerequisite", err)
		}
		usedBefore := beforeUsage.QuotaUsage.Month.TokensUsed
		recorded := func(t *testing.T, res *httptest.ResponseRecorder) {
			t.Helper()
			if err := svc.FlushCallRecorder(ctx); err != nil {
				t.Fatal(err)
			}
			var call entity.CallRecord
			if err := db.Where("request_id = ?", res.Header().Get("X-Request-ID")).Take(&call).Error; err != nil {
				t.Fatal(err)
			}
			if call.UserID != analystID || call.KeyID != "" || call.ProjectID != "" || call.TeamID != "" || call.TeamMembershipID != "" || call.ModelID != modelID || call.Status != "success" || call.InputTokens == nil || *call.InputTokens != 10 || call.OutputTokens == nil || *call.OutputTokens != 4 {
				t.Fatal("Key analysis borrowed owner accounting", call)
			}
			var attempts []entity.CallAttempt
			if err := db.Where("request_id = ?", call.RequestID).Find(&attempts).Error; err != nil {
				t.Fatal(err)
			}
			if len(attempts) != 1 || attempts[0].CredentialID != native.credentialID || attempts[0].SnapshotID == "" || attempts[0].SnapshotID != call.SnapshotID || attempts[0].Status != "success" || attempts[0].NativeCompletionEvidence != "completed" || !attempts[0].FinalUsageKnown {
				t.Fatal("private answer denial erased native metering", attempts)
			}
		}
		completed := int64(0)
		assertAnswer := func(t *testing.T, kind, id, root string) {
			t.Helper()
			input["scope_kind"], input["scope_id"] = kind, id
			page := readPage(t, kind, id)
			if len(page.Items) != 1 || page.Items[0].ID != root || kind == "member" && page.Items[0].SharedKeyCount != 2 {
				t.Fatal("current page did not preserve root account", page)
			}
			res := analyze()
			expectStatus(t, res, 200)
			recorded(t, res)
			completed++
			var view service.InsightsAIResponse
			if json.Unmarshal(res.Body.Bytes(), &view) != nil || view.Capture != nil || view.QuotaCapture != nil || view.KeyQuotaCapture == nil || view.KeyQuotaCapture.ScopeKind != kind || view.KeyQuotaCapture.ScopeID != id || view.KeyQuotaCapture.Cursor != "" || view.KeyQuotaCapture.Limit != 1 || len(view.Labels) != 1 || view.Labels[0].KeyID == nil || *view.Labels[0].KeyID != root || view.Labels[0].TeamID != nil {
				t.Fatal("answer widened the reviewed Key scope")
			}
			if input["report"] == "key_quota_money" && (view.KeyQuotaCapture.Dimension != "money" || view.KeyQuotaCapture.Currency != page.Currency) {
				t.Fatal("Money Key capture lost the server-owned denomination")
			}
			payload := native.content.Load().(string)
			for _, private := range []string{adminID, analystID, projectID, privateProjectID, personalRootID, projectKey.Record.Key.ID, projectKey.Secret, page.Items[0].Name, "scope_kind", "scope_id", "key_id", "token_hash"} {
				if strings.Contains(payload, private) {
					t.Fatal("private Key identity entered native aggregate data")
				}
			}
			var projected struct {
				Aggregates struct {
					Dimension, Currency string
					Keys                []struct {
						Label            string
						LocalCap         *string `json:"local_cap"`
						LocalCapCurrency string  `json:"local_cap_currency"`
						SharedKeyCount   int     `json:"shared_key_count"`
					}
				}
			}
			if json.Unmarshal([]byte(payload), &projected) != nil || len(projected.Aggregates.Keys) != 1 || projected.Aggregates.Keys[0].Label != "K1" || projected.Aggregates.Keys[0].SharedKeyCount != page.Items[0].SharedKeyCount {
				t.Fatal("root count or anonymous projection changed")
			}
			if input["report"] == "key_quota_money" && (projected.Aggregates.Dimension != "money" || projected.Aggregates.Currency != page.Currency || page.Items[0].LocalCap == nil || projected.Aggregates.Keys[0].LocalCap == nil || *projected.Aggregates.Keys[0].LocalCap != *page.Items[0].LocalCap || projected.Aggregates.Keys[0].LocalCapCurrency != page.Currency) {
				t.Fatal("stored Money root cap changed upstream")
			}
		}
		before := native.dispatches.Load()
		expectStatus(t, request("POST", "/api/v1/admin/insights/analyze", input, "wrong-csrf"), 403)
		input["scope_kind"], input["scope_id"] = "project", privateProjectID
		expectStatus(t, analyze(), 404)
		input["scope_kind"], input["scope_id"] = "member", adminID
		modelsResponse := request("GET", "/api/v1/admin/insights/models", nil, "")
		expectStatus(t, modelsResponse, 200)
		var models service.InsightsAIModels
		if json.Unmarshal(modelsResponse.Body.Bytes(), &models) != nil || len(models.Items) != 1 || models.Items[0].ModelID != modelID {
			t.Fatal("missing exact personal Model prerequisite before member-read revocation")
		}
		if err := db.Where("role_id = ? AND permission = ?", roleID, permission.Permission).Delete(&entity.RolePermission{}).Error; err != nil {
			t.Fatal(err)
		}
		expectStatus(t, analyze(), 403)
		if native.dispatches.Load() != before {
			t.Fatal("unauthorized scope dispatched inference")
		}
		if err := db.Create(&permission).Error; err != nil {
			t.Fatal(err)
		}
		// Reading another owner's aggregate page never borrows that owner's quota.
		// Both Key scope selectors must obey the analyst's own zero Token policy.
		quotaTarget := service.LimitTarget{Kind: "user", ID: analystID}
		quotaRestored := false
		defer func() {
			if quotaRestored {
				return
			}
			current, err := svc.GetResourceLimit(ctx, adminID, quotaTarget)
			if err != nil {
				t.Error("read personal quota for fixture restoration", err)
				return
			}
			if _, err = svc.SetResourceLimit(ctx, adminID, quotaTarget, current.ETag, service.LimitInput{Policy: beforeUsage.Stored, Reason: "Restore personal Key advice fixture quota"}); err != nil {
				t.Error("restore personal Key advice fixture quota", err)
			}
		}()
		zero := int64(0)
		zeroPolicy := beforeUsage.Stored
		zeroPolicy.TokensMonth = &zero
		if _, err = svc.SetResourceLimit(ctx, adminID, quotaTarget, beforeUsage.ETag, service.LimitInput{Policy: zeroPolicy, Reason: "Deny personal Key advice before dispatch"}); err != nil {
			t.Fatal(err)
		}
		for _, scope := range []struct{ kind, id string }{{"member", adminID}, {"project", projectID}} {
			input["scope_kind"], input["scope_id"] = scope.kind, scope.id
			denied := analyze()
			expectStatus(t, denied, 429)
			if native.dispatches.Load() != before {
				t.Fatal("zero personal quota dispatched Key advice", scope.kind)
			}
			if err := svc.FlushCallRecorder(ctx); err != nil {
				t.Fatal(err)
			}
			requestID := denied.Header().Get("X-Request-ID")
			if requestID == "" {
				t.Fatal("quota denial lacks request identity")
			}
			var successes, attempts int64
			if err := db.Model(&entity.CallRecord{}).Where("request_id = ? AND status = ?", requestID, "success").Count(&successes).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&entity.CallAttempt{}).Where("request_id = ?", requestID).Count(&attempts).Error; err != nil {
				t.Fatal(err)
			}
			if successes != 0 || attempts != 0 {
				t.Fatal("quota denial recorded a successful call or dispatched attempt", scope.kind, successes, attempts)
			}
			input["report"] = "key_quota_money"
			expectStatus(t, analyze(), 429)
			if native.dispatches.Load() != before {
				t.Fatal("Money advice borrowed the selected owner's quota", scope.kind)
			}
			input["report"] = "key_quota_tokens"
		}
		zeroView, err := svc.GetResourceLimit(ctx, adminID, quotaTarget)
		if err != nil || zeroView.QuotaUsage == nil || zeroView.QuotaUsage.Month == nil || zeroView.QuotaUsage.Active == nil || zeroView.QuotaUsage.Month.TokensUsed != usedBefore || zeroView.QuotaUsage.Month.TokensUnknown != 0 || zeroView.QuotaUsage.Month.TokensHeld != 0 || zeroView.QuotaUsage.Active.TokensHeld != 0 {
			t.Fatal("zero personal Key quota denial retained usage or a hold", zeroView, err)
		}
		if _, err = svc.SetResourceLimit(ctx, adminID, quotaTarget, zeroView.ETag, service.LimitInput{Policy: beforeUsage.Stored, Reason: "Restore personal Key advice fixture quota"}); err != nil {
			t.Fatal(err)
		}
		quotaRestored = true
		assertAnswer(t, "member", adminID, personalRootID)
		assertAnswer(t, "project", projectID, projectKey.Record.Key.ID)
		held := func(t *testing.T, revoke, restore func(*testing.T), status int) {
			t.Helper()
			barrier := &insightsAIIntegrationBarrier{entered: make(chan struct{}), release: make(chan struct{})}
			native.held.Store(barrier)
			done := make(chan *httptest.ResponseRecorder, 1)
			joined := false
			revoked := false
			defer func() {
				barrier.unblock()
				native.held.Store(nil)
				if !joined {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("Key advice child failed bounded direct join")
					}
				}
				if revoked {
					restore(t)
				}
			}()
			go func() { done <- analyze() }()
			select {
			case <-barrier.entered:
			case res := <-done:
				joined = true
				t.Fatalf("Key advice ended before native dispatch: %d", res.Code)
			case <-ctx.Done():
				t.Fatal("Key advice missed native barrier")
			}
			revoke(t)
			revoked = true
			barrier.unblock()
			select {
			case res := <-done:
				joined = true
				expectStatus(t, res, status)
				if strings.Contains(res.Body.String(), "recorded tokens") || strings.Contains(res.Body.String(), projectKey.Record.Key.ID) {
					t.Fatal("revoked authority received private Key answer")
				}
				recorded(t, res)
				completed++
			case <-ctx.Done():
				t.Fatal("Key advice failed bounded response join")
			}
		}
		var manager entity.ProjectManager
		if err := db.Where("project_id = ? AND user_id = ?", projectID, analystID).Take(&manager).Error; err != nil {
			t.Fatal(err)
		}
		held(t, func(t *testing.T) {
			if err := db.Where("id = ?", manager.ID).Delete(&entity.ProjectManager{}).Error; err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T) {
			if err := db.Create(&manager).Error; err != nil {
				t.Fatal(err)
			}
		}, 404)
		updateBirth := func(t *testing.T, model any, id string, birth time.Time) {
			t.Helper()
			r := db.Model(model).Where("id = ?", id).UpdateColumn("created_at", birth)
			if r.Error != nil || r.RowsAffected != 1 {
				t.Fatal("exact birth fixture update", r.Error, r.RowsAffected)
			}
		}
		var project entity.Project
		if err := db.Where("id = ?", projectID).Take(&project).Error; err != nil {
			t.Fatal(err)
		}
		// Use a delta representable by the released resource timestamp schema.
		held(t, func(t *testing.T) {
			updateBirth(t, &entity.Project{}, projectID, project.CreatedAt.Add(time.Second))
		}, func(t *testing.T) { updateBirth(t, &entity.Project{}, projectID, project.CreatedAt) }, 404)
		var persistedKey entity.ProjectKey
		if err := db.Where("id = ?", projectKey.Record.Key.ID).Take(&persistedKey).Error; err != nil {
			t.Fatal(err)
		}
		held(t, func(t *testing.T) {
			updateBirth(t, &entity.ProjectKey{}, persistedKey.ID, persistedKey.CreatedAt.Add(time.Second))
		}, func(t *testing.T) { updateBirth(t, &entity.ProjectKey{}, persistedKey.ID, persistedKey.CreatedAt) }, 404)
		input["scope_kind"], input["scope_id"] = "member", adminID
		held(t, func(t *testing.T) {
			if err := db.Where("role_id = ? AND permission = ?", roleID, permission.Permission).Delete(&entity.RolePermission{}).Error; err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T) {
			if err := db.Create(&permission).Error; err != nil {
				t.Fatal(err)
			}
		}, 403)
		analysisPermission := entity.RolePermission{RoleID: roleID, Permission: "calls.read_all"}
		held(t, func(t *testing.T) {
			if err := db.Where("role_id = ? AND permission = ?", roleID, analysisPermission.Permission).Delete(&entity.RolePermission{}).Error; err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T) {
			if err := db.Create(&analysisPermission).Error; err != nil {
				t.Fatal(err)
			}
		}, 403)
		grant := func(t *testing.T, ids []string) {
			t.Helper()
			if len(ids) > 1 || len(ids) == 1 && ids[0] != modelID {
				t.Fatal("invalid exact Key advice Model grant fixture")
			}
			setInsightsAIQuotaPersonalGrant(t, ctx, svc, adminID, analystID, modelID, len(ids) == 1)
		}
		held(t, func(t *testing.T) { grant(t, []string{}) }, func(t *testing.T) { grant(t, []string{modelID}) }, 403)
		held(t, func(t *testing.T) {
			expectStatus(t, request("POST", "/api/v1/auth/logout", nil, session.CSRFToken), 204)
		}, func(t *testing.T) { session, cookie = login(t) }, 401)
		for _, scope := range []struct{ kind, id, root string }{{"member", adminID, personalRootID}, {"project", projectID, projectKey.Record.Key.ID}} {
			t.Run(scope.kind+"_money_current_page_and_late_birth", func(t *testing.T) {
				input["report"], input["scope_kind"], input["scope_id"] = "key_quota_money", scope.kind, scope.id
				page := readPage(t, scope.kind, scope.id)
				if len(page.Items) != 1 || page.Items[0].ID != scope.root {
					t.Fatal("Money root scope prerequisite")
				}
				targetKind := "personal_key"
				if scope.kind == "project" {
					targetKind = "project_key"
				}
				target := service.LimitTarget{Kind: targetKind, ID: scope.root}
				if scope.kind == "project" {
					target.ProjectID = scope.id
				}
				policy, err := svc.GetResourceLimit(ctx, adminID, target)
				if err != nil {
					t.Fatal(err)
				}
				cap := "9007199254740993.000000000000000001"
				stored := policy.Stored
				stored.MoneyMonth, stored.Currency = &cap, page.Currency
				if _, err = svc.SetResourceLimit(ctx, adminID, target, policy.ETag, service.LimitInput{Policy: stored, Reason: "Review exact root Money advice fixture cap"}); err != nil {
					t.Fatal(err)
				}
				assertAnswer(t, scope.kind, scope.id, scope.root)
				if scope.kind == "project" {
					held(t, func(t *testing.T) {
						updateBirth(t, &entity.ProjectKey{}, persistedKey.ID, persistedKey.CreatedAt.Add(time.Second))
					}, func(t *testing.T) { updateBirth(t, &entity.ProjectKey{}, persistedKey.ID, persistedKey.CreatedAt) }, 404)
				} else {
					var member entity.User
					if err := db.Where("id = ?", adminID).Take(&member).Error; err != nil {
						t.Fatal(err)
					}
					held(t, func(t *testing.T) { updateBirth(t, &entity.User{}, member.ID, member.CreatedAt.Add(time.Second)) }, func(t *testing.T) { updateBirth(t, &entity.User{}, member.ID, member.CreatedAt) }, 404)
				}
			})
		}
		usage, err := svc.GetResourceLimit(ctx, adminID, service.LimitTarget{Kind: "user", ID: analystID})
		if err != nil || usage.QuotaUsage == nil || usage.QuotaUsage.Month == nil || usage.QuotaUsage.Active == nil || usage.QuotaUsage.Month.TokensUsed != usedBefore+completed*14 || usage.QuotaUsage.Month.TokensUnknown != 0 || usage.QuotaUsage.Month.TokensHeld != 0 || usage.QuotaUsage.Active.TokensHeld != 0 {
			t.Fatal("Key advice did not retain exact personal-only settlements", usage, err)
		}
		if native.dispatches.Load() != before+completed {
			t.Fatal("Key advice repeated or lost an actual dispatch")
		}
	})
}
