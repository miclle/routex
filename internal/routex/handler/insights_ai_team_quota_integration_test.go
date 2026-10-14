package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

type insightsAITeamNativeWitness struct {
	held         *atomic.Pointer[insightsAIIntegrationBarrier]
	dispatches   *atomic.Int64
	content      *atomic.Value
	credentialID string
}

// The selected Model is also inherited through the analyst's Team. Member
// additions intentionally exclude inherited Models, so use the administrator's
// real Model grant replacement while preserving every other current grantee.
func setInsightsAIQuotaPersonalGrant(t *testing.T, ctx context.Context, svc *service.Service, adminID, analystID, modelID string, enabled bool) {
	t.Helper()
	review, err := svc.GetAdminModel(ctx, adminID, modelID)
	if err != nil || review == nil || review.Model.ID != modelID {
		t.Fatal("read exact selected Model grant fixture", err)
	}
	grantees := slices.DeleteFunc(slices.Clone(review.GrantedUserIDs), func(id string) bool { return id == analystID })
	if enabled {
		grantees = append(grantees, analystID)
	}
	slices.Sort(grantees)
	saved, err := svc.SetModelGrants(ctx, adminID, modelID, grantees)
	if err != nil || saved == nil || saved.Model.ID != modelID || !slices.Equal(saved.GrantedUserIDs, grantees) {
		t.Fatal("replace only exact analyst Model grant", err)
	}
	personal, err := svc.MemberModelsWorkspace(ctx, adminID, analystID)
	if err != nil || personal == nil {
		t.Fatal("read analyst personal Model grant prerequisite", err)
	}
	present := false
	for _, model := range personal.PersonalModels {
		if model.ID == modelID {
			present = true
		}
	}
	if present != enabled {
		t.Fatal("exact personal Model grant prerequisite differs from reviewed fixture")
	}
}

// Reuses the real-driver scenario's one controlled native peer and recorder.
// Additional advice settles only to the analyst's personal account, not Teams.
func testInsightsAITeamQuotaIntegration(t *testing.T, ctx context.Context, db *gorm.DB, svc *service.Service, router http.Handler, adminID, analystID, roleID, teamID, privateTeamID, modelID string, native insightsAITeamNativeWitness) {
	t.Helper()
	t.Run("team_quota_current_page_advice", func(t *testing.T) {
		login := func(t *testing.T) (SessionResponse, *http.Cookie) {
			t.Helper()
			res := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"quota-analyst@example.invalid","password":"quota-analysis-password"}`, nil, "")
			expectStatus(t, res, 200)
			return readIdentity(t, res)
		}
		// Establish the independent personal grant through the real complete-set API.
		grant := func(t *testing.T, ids []string) {
			t.Helper()
			if len(ids) > 1 || len(ids) == 1 && ids[0] != modelID {
				t.Fatal("invalid exact Team advice Model grant fixture")
			}
			setInsightsAIQuotaPersonalGrant(t, ctx, svc, adminID, analystID, modelID, len(ids) == 1)
		}
		grant(t, []string{modelID})
		second, err := svc.CreateResource(ctx, adminID, service.TeamResource, "Team advice second page", "", []string{adminID, analystID})
		if err != nil {
			t.Fatal(err)
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
		readPage := func(t *testing.T, cursor string) service.InsightsQuotaReport {
			t.Helper()
			path := "/api/v1/admin/insights/quota/teams?dimension=tokens&limit=1"
			if cursor != "" {
				path += "&cursor=" + url.QueryEscape(cursor)
			}
			res := request("GET", path, nil, "")
			expectStatus(t, res, 200)
			var page service.InsightsQuotaReport
			if json.Unmarshal(res.Body.Bytes(), &page) != nil {
				t.Fatal("invalid real page")
			}
			return page
		}
		first := readPage(t, "")
		if len(first.Items) != 1 || first.NextCursor == nil {
			t.Fatal("two readable Teams require bounded cursor", first)
		}
		page := readPage(t, *first.NextCursor)
		if len(page.Items) != 1 || page.Items[0].ID == first.Items[0].ID || page.NextCursor != nil || page.Items[0].ID == privateTeamID || page.Items[0].ID != teamID && page.Items[0].ID != second.ID {
			t.Fatal("reviewed following page escaped exact readable scope", page)
		}
		input := map[string]any{"report": "team_quota_tokens", "model_id": modelID, "language": "en", "cursor": *first.NextCursor, "limit": 1, "question": "Explain only T1; no new policy or forecast"}
		analyze := func() *httptest.ResponseRecorder {
			return request("POST", "/api/v1/admin/insights/analyze", input, session.CSRFToken)
		}
		denied := func(t *testing.T, res *httptest.ResponseRecorder, status int, before int64) {
			t.Helper()
			expectStatus(t, res, status)
			if native.dispatches.Load() != before {
				t.Fatal("denial dispatched inference")
			}
		}
		recorded := func(t *testing.T, res *httptest.ResponseRecorder) {
			t.Helper()
			if err := svc.FlushCallRecorder(ctx); err != nil {
				t.Fatal(err)
			}
			var call entity.CallRecord
			if err := db.Where("request_id = ?", res.Header().Get("X-Request-ID")).Take(&call).Error; err != nil {
				t.Fatal(err)
			}
			if call.UserID != analystID || call.KeyID != "" || call.TeamID != "" || call.TeamMembershipID != "" || call.ProjectID != "" || call.ModelID != modelID || call.Status != "success" || call.InputTokens == nil || *call.InputTokens != 10 || call.OutputTokens == nil || *call.OutputTokens != 4 {
				t.Fatal("Team aggregate advice borrowed resource accounting", call)
			}
			var attempts []entity.CallAttempt
			if err := db.Where("request_id = ?", call.RequestID).Find(&attempts).Error; err != nil {
				t.Fatal(err)
			}
			if len(attempts) != 1 || attempts[0].CredentialID != native.credentialID || attempts[0].SnapshotID == "" || attempts[0].SnapshotID != call.SnapshotID || attempts[0].Status != "success" || attempts[0].NativeCompletionEvidence != "completed" || !attempts[0].FinalUsageKnown {
				t.Fatal("late private denial erased real native evidence", attempts)
			}
		}
		before := native.dispatches.Load()
		denied(t, request("POST", "/api/v1/admin/insights/analyze", input, "wrong-csrf"), 403, before)
		// The admin cursor is valid syntax but belongs to a different actor.
		adminPage, err := svc.InsightsQuota(ctx, adminID, service.InsightsQuotaInput{Dimension: "tokens", Limit: 1}, false)
		if err != nil || adminPage.NextCursor == nil {
			t.Fatal("admin cursor prerequisite", err)
		}
		input["cursor"] = *adminPage.NextCursor
		denied(t, analyze(), 400, before)
		input["cursor"] = *first.NextCursor
		grant(t, []string{})
		// A new request resolves the current grant set; an absent selected Model is
		// hidden with 404. The held-response stale-generation control below uses 403.
		denied(t, analyze(), 404, before)
		grant(t, []string{modelID})
		target := service.LimitTarget{Kind: "user", ID: analystID}
		limitsBefore, err := svc.GetResourceLimit(ctx, adminID, target)
		if err != nil {
			t.Fatal(err)
		}
		zero := int64(0)
		zeroPolicy := limitsBefore.Stored
		zeroPolicy.TokensMonth = &zero
		if _, err := svc.SetResourceLimit(ctx, adminID, target, limitsBefore.ETag, service.LimitInput{Policy: zeroPolicy, Reason: "Deny personal Team advice before dispatch"}); err != nil {
			t.Fatal(err)
		}
		denied(t, analyze(), 429, before)
		zeroView, err := svc.GetResourceLimit(ctx, adminID, target)
		if err != nil || zeroView.QuotaUsage == nil || zeroView.QuotaUsage.Active == nil || zeroView.QuotaUsage.Active.TokensHeld != 0 {
			t.Fatal("quota denial retained an active hold", zeroView, err)
		}
		if _, err := svc.SetResourceLimit(ctx, adminID, target, zeroView.ETag, service.LimitInput{Policy: limitsBefore.Stored, Reason: "Restore personal Team advice fixture quota"}); err != nil {
			t.Fatal(err)
		}
		response := analyze()
		expectStatus(t, response, 200)
		recorded(t, response)
		var view service.InsightsAIResponse
		if json.Unmarshal(response.Body.Bytes(), &view) != nil || view.Capture != nil || view.QuotaCapture == nil || view.QuotaCapture.Cursor != input["cursor"] || view.QuotaCapture.Limit != 1 || len(view.Labels) != 1 || view.Labels[0].TeamID == nil || *view.Labels[0].TeamID != page.Items[0].ID {
			t.Fatal("answer did not use the reviewed current page")
		}
		payload := native.content.Load().(string)
		for _, private := range []string{teamID, second.ID, privateTeamID, analystID, *first.NextCursor, page.Items[0].Name, "shared_key_count", "team_id"} {
			if strings.Contains(payload, private) {
				t.Fatal("identity or pagination entered native aggregate data", private)
			}
		}
		var projected struct {
			Aggregates struct{ Teams []struct{ Label string } }
		}
		if json.Unmarshal([]byte(payload), &projected) != nil || len(projected.Aggregates.Teams) != 1 || projected.Aggregates.Teams[0].Label != "T1" {
			t.Fatal("native report added pages or lost anonymous label")
		}
		completed := int64(1)
		held := func(t *testing.T, revoke, restore func(*testing.T), status int) {
			t.Helper()
			barrier := &insightsAIIntegrationBarrier{entered: make(chan struct{}), release: make(chan struct{})}
			native.held.Store(barrier)
			done := make(chan *httptest.ResponseRecorder, 1)
			joined := false
			defer func() {
				barrier.unblock()
				native.held.Store(nil)
				if !joined {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("Team advice child failed bounded direct join")
					}
				}
				restore(t)
			}()
			go func() { done <- analyze() }()
			select {
			case <-barrier.entered:
			case res := <-done:
				joined = true
				t.Fatalf("advice ended before actual native dispatch: %d", res.Code)
			case <-ctx.Done():
				t.Fatal("advice never reached native barrier")
			}
			revoke(t)
			barrier.unblock()
			select {
			case res := <-done:
				joined = true
				expectStatus(t, res, status)
				if strings.Contains(res.Body.String(), "recorded tokens") || strings.Contains(res.Body.String(), page.Items[0].ID) {
					t.Fatal("revoked caller received private analysis")
				}
				recorded(t, res)
				completed++
			case <-ctx.Done():
				t.Fatal("Team advice failed bounded response join")
			}
		}
		updateMembership := func(t *testing.T, status string) {
			t.Helper()
			r := db.Model(&entity.TeamMembership{}).Where("team_id = ? AND user_id = ?", page.Items[0].ID, analystID).UpdateColumn("status", status)
			if r.Error != nil || r.RowsAffected != 1 {
				t.Fatal("exact Team membership fixture update failed", r.Error, r.RowsAffected)
			}
		}
		held(t, func(t *testing.T) { updateMembership(t, entity.ResourceDisabled) }, func(t *testing.T) { updateMembership(t, entity.ResourceActive) }, 404)
		var capturedTeam entity.Team
		if err := db.Where("id = ?", page.Items[0].ID).Take(&capturedTeam).Error; err != nil {
			t.Fatal(err)
		}
		updateBirth := func(t *testing.T, birth time.Time) {
			t.Helper()
			result := db.Model(&entity.Team{}).Where("id = ?", capturedTeam.ID).UpdateColumn("created_at", birth)
			if result.Error != nil || result.RowsAffected != 1 {
				t.Fatal("exact Team birth fixture update", result.Error, result.RowsAffected)
			}
		}
		// Use a delta representable by the released resource timestamp schema.
		held(t, func(t *testing.T) { updateBirth(t, capturedTeam.CreatedAt.Add(time.Second)) }, func(t *testing.T) { updateBirth(t, capturedTeam.CreatedAt) }, 404)
		permission := entity.RolePermission{RoleID: roleID, Permission: "calls.read_all"}
		held(t, func(t *testing.T) {
			r := db.Where("role_id = ? AND permission = ?", roleID, permission.Permission).Delete(&entity.RolePermission{})
			if r.Error != nil || r.RowsAffected != 1 {
				t.Fatal(r.Error, r.RowsAffected)
			}
		}, func(t *testing.T) {
			if err := db.Create(&permission).Error; err != nil {
				t.Fatal(err)
			}
		}, 403)
		held(t, func(t *testing.T) { grant(t, []string{}) }, func(t *testing.T) { grant(t, []string{modelID}) }, 403)
		held(t, func(t *testing.T) {
			expectStatus(t, request("POST", "/api/v1/auth/logout", nil, session.CSRFToken), 204)
		}, func(t *testing.T) { session, cookie = login(t) }, 401)
		usage, err := svc.GetResourceLimit(ctx, adminID, service.LimitTarget{Kind: "user", ID: analystID})
		if err != nil || usage.QuotaUsage == nil || usage.QuotaUsage.Month == nil || usage.QuotaUsage.Active == nil || usage.QuotaUsage.Month.TokensUsed != completed*14 || usage.QuotaUsage.Month.TokensUnknown != 0 || usage.QuotaUsage.Month.TokensHeld != 0 || usage.QuotaUsage.Active.TokensHeld != 0 {
			t.Fatal("all real advice must settle personal-only usage despite late denials", usage, err)
		}
	})
}
