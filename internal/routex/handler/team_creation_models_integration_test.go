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
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

// Registered in the normal isolated dual-driver harness, never a second DSN or
// unchecked service bypass. Read-only candidates/reviews must not persist intent.
func testTeamCreationModelsLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var fault atomic.Value
	fault.Store("")
	const hook = "team-creation-models-atomic-fault"
	if err := db.Callback().Create().Before("gorm:create").Register(hook, func(tx *gorm.DB) {
		if table := fault.Load().(string); table != "" && tx.Statement.Table == table {
			if table == "audit_events" {
				event, ok := tx.Statement.Dest.(*entity.AuditEvent)
				if !ok || event.Action != "team.creation.commit" {
					return
				}
			}
			_ = tx.AddError(errors.New("controlled initial model transaction fault"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Callback().Create().Remove(hook) }()
	store, err := secretstore.New(bytes.Repeat([]byte{166}, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		fault.Store("")
		svc.StopRuntime()
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
	}()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	admin, cookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/setup", `{"email":"initial-model-admin@example.invalid","password":"test-only-initial-model-password","name":"Initial Model administrator"}`, nil, ""))
	type actor struct {
		id, csrf string
		cookie   *http.Cookie
	}
	administrator := actor{admin.User.ID, admin.CSRFToken, cookie}
	member := func(name string, permissions []string) actor {
		t.Helper()
		user, cookie, csrf := createSystemStatusMember(t, svc, router, admin.User.ID, name, permissions)
		return actor{user.User.ID, csrf, cookie}
	}
	owner := member("tim-owner", nil)
	basic := member("tim-basic", []string{"teams.write"})
	models := member("tim-models", []string{"teams.models.write"})
	creator := member("tim-creator", []string{"teams.write", "teams.models.write"})
	sendRaw := func(who actor, method, path, body, etag string) *httptest.ResponseRecorder {
		t.Helper()
		bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		req := httptest.NewRequestWithContext(bounded, method, "http://routex.test"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://routex.test")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		if who.cookie != nil {
			req.AddCookie(who.cookie)
			req.Header.Set("X-CSRF-Token", who.csrf)
		}
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	send := func(who actor, method, path string, body any, etag string) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return sendRaw(who, method, path, string(raw), etag)
	}
	const contextPath = "/api/v1/admin/teams/creation-context"
	const candidatesPath = "/api/v1/admin/teams/creation-model-candidates"
	const reviewPath = "/api/v1/admin/teams/creation-model-review"
	const createPath = "/api/v1/admin/teams"
	contextRead := func(who actor) service.TeamCreationContext {
		t.Helper()
		out := sendRaw(who, "GET", contextPath, "", "")
		expectStatus(t, out, 200)
		if out.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("context privacy")
		}
		var result service.TeamCreationContext
		if json.Unmarshal(out.Body.Bytes(), &result) != nil || out.Header().Get("ETag") != `"`+result.ReviewETag+`"` {
			t.Fatal("context strong validator")
		}
		return result
	}
	if contextRead(basic).CanSetModels || !contextRead(creator).CanSetModels {
		t.Fatal("independent model authority context")
	}
	for _, who := range []actor{basic, models, owner} {
		expectStatus(t, sendRaw(who, "GET", candidatesPath, "", ""), 403)
	}
	expectStatus(t, send(administrator, "POST", reviewPath, map[string]any{"model_ids": []string{"mdl_none"}}, ""), 428)
	for _, suffix := range []string{"?limit=51", "?limit=0", "?q=a&q=b", "?private=1", "?cursor=not-a-model"} {
		expectStatus(t, sendRaw(administrator, "GET", candidatesPath+suffix, "", ""), 400)
	}
	unchangedReviewHeaders := []string{"?q=bad%zz", "?limit=1&limit=2"}
	for _, suffix := range unchangedReviewHeaders {
		expectStatus(t, sendRaw(administrator, "GET", candidatesPath+suffix, "", ""), 400)
	}
	var dispatches atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatches.Add(1)
		if r.Header.Get("Authorization") != "Bearer initial-model-upstream" || r.Header.Get("Cookie") != "" || r.Header.Get("X-CSRF-Token") != "" {
			t.Error("native Team authentication leaked")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, quotaWarningNativeBody(false, false))
	}))
	defer upstream.Close()
	cipher, err := store.Seal("crd_tim", "initial-model-upstream")
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Truncate(time.Millisecond)
	for _, row := range []any{&entity.Provider{ID: "prv_tim", Name: "Recorded initial provider"}, &entity.ProviderConnection{ID: "con_tim", ProviderID: "prv_tim", Name: "Native", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"}, &entity.ProviderCredential{ID: "crd_tim", ConnectionID: "con_tim", Name: "Ready", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"}, &entity.ProviderModel{ID: "pmd_tim", ConnectionID: "con_tim", UpstreamName: "initial-native"}, &entity.CredentialModelAccess{CredentialID: "crd_tim", ProviderModelID: "pmd_tim"}, &entity.ReservationBound{ProviderModelID: "pmd_tim", Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 1, MaxOutputTokens: 1, ETag: "bnd_tim", Evidence: "Controlled response", Reason: "Initial grant native proof"}, &entity.ModelPrice{ID: "mpr_tim", ProviderModelID: "pmd_tim", UpdateSource: "api"}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		if err := db.Create(&entity.PriceRate{ID: fmt.Sprintf("rat_tim_%d", i), ModelPriceID: "mpr_tim", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Amount: "0", Currency: "USD", Enabled: true}).Error; err != nil {
			t.Fatal(err)
		}
	}
	allIDs := make([]string, 1000)
	modelRows := make([]entity.Model, 1000)
	names := make([]entity.ModelName, 1000)
	bindings := make([]entity.ModelProviderBinding, 1000)
	for i := range allIDs {
		id := fmt.Sprintf("mdl_%026d", i)
		allIDs[i] = id
		modelRows[i] = entity.Model{ID: id, Status: entity.ResourceActive, CreatedAt: stamp}
		copyID := id
		names[i] = entity.ModelName{Name: fmt.Sprintf("initial-model-%04d", i), ModelID: id, CurrentModelID: &copyID}
		bindings[i] = entity.ModelProviderBinding{ID: fmt.Sprintf("bnd_%026d", i), ModelID: id, ProviderModelID: "pmd_tim", Weight: 100}
	}
	for _, rows := range []any{&modelRows, &names, &bindings} {
		if err := db.CreateInBatches(rows, 500).Error; err != nil {
			t.Fatal(err)
		}
	}
	warmup := "rx_" + strings.Repeat("m", 43)
	for _, row := range []any{&entity.UserModelGrant{UserID: admin.User.ID, ModelID: allIDs[0]}, &entity.APIKey{ID: "key_tim", UserID: admin.User.ID, Name: "Coverage", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(warmup), Status: entity.KeyActive}, &entity.APIKeyModel{KeyID: "key_tim", ModelID: allIDs[0]}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	journalPath := filepath.Join(t.TempDir(), "initial-model.db")
	if err := svc.StartCallRecorder(ctx, journalPath); err != nil {
		t.Fatal(err)
	}
	refresh := func() {
		t.Helper()
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
	}
	refresh()
	counts := func() map[string]int64 {
		t.Helper()
		result := map[string]int64{}
		for _, table := range []string{"teams", "team_memberships", "team_model_grants", "team_creation_receipts", "team_creation_receipt_models", "resource_limits", "audit_events"} {
			var n int64
			if err := db.Table(table).Count(&n).Error; err != nil {
				t.Fatal(err)
			}
			result[table] = n
		}
		return result
	}
	unchanged := func(run func()) {
		t.Helper()
		before := counts()
		run()
		if !reflect.DeepEqual(before, counts()) {
			t.Fatal("read/rejection/failed atomic creation wrote rows")
		}
	}
	candidates := func(who actor, suffix string) service.TeamCreationModelPage {
		t.Helper()
		out := sendRaw(who, "GET", candidatesPath+suffix, "", "")
		expectStatus(t, out, 200)
		if out.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("candidate privacy")
		}
		var page service.TeamCreationModelPage
		if json.Unmarshal(out.Body.Bytes(), &page) != nil {
			t.Fatal("candidate shape")
		}
		return page
	}
	// The first scanned Model can be unavailable while later Models are ready.
	// A truthful scan cursor remains; off-page selected IDs are reviewed directly.
	if err := db.Model(&entity.ModelProviderBinding{}).Where("model_id = ?", allIDs[0]).Update("Weight", 0).Error; err != nil {
		t.Fatal(err)
	}
	refresh()
	unchanged(func() {
		empty := candidates(creator, "?limit=1")
		if len(empty.Items) != 0 || empty.NextCursor == nil || *empty.NextCursor != allIDs[0] {
			t.Fatal("empty eligible page lost truthful continuation")
		}
		next := candidates(creator, "?limit=1&cursor="+*empty.NextCursor)
		if len(next.Items) != 1 || next.Items[0].ID != allIDs[1] {
			t.Fatal("continuation skipped eligible off-page Model")
		}
	})
	if err := db.Model(&entity.ModelProviderBinding{}).Where("model_id = ?", allIDs[0]).Update("Weight", 100).Error; err != nil {
		t.Fatal(err)
	}
	refresh()
	unchanged(func() {
		page := candidates(creator, "?limit=1")
		if len(page.Items) != 1 || page.NextCursor == nil || page.Items[0].Providers != nil || !slices.Equal(page.Items[0].Protocols, []string{entity.ProtocolOpenAIChat}) {
			t.Fatal("purpose-scoped bounded/redacted candidates")
		}
		next := candidates(creator, "?limit=1&cursor="+*page.NextCursor)
		if len(next.Items) != 1 || next.Items[0].ID <= page.Items[0].ID {
			t.Fatal("candidate exact continuation")
		}
		authorized := candidates(administrator, "?limit=1")
		if !slices.Equal(authorized.Items[0].Providers, []string{"Recorded initial provider"}) {
			t.Fatal("provider labels independent")
		}
	})
	review := func(who actor, ids []string, tag string) service.TeamCreationModelReview {
		t.Helper()
		out := send(who, "POST", reviewPath, map[string]any{"model_ids": ids}, tag)
		expectStatus(t, out, 200)
		var result service.TeamCreationModelReview
		if json.Unmarshal(out.Body.Bytes(), &result) != nil || len(result.ModelReviewToken) != 64 {
			t.Fatal("aggregate model proof")
		}
		expected := slices.Clone(ids)
		slices.Sort(expected)
		if !slices.Equal(result.ModelIDs, expected) {
			t.Fatal("review omitted selected IDs")
		}
		return result
	}
	sequence := 0
	input := func(ids []string, token string) map[string]any {
		sequence++
		body := map[string]any{"creation_id": fmt.Sprintf("66000000-0000-4000-8000-%012d", sequence), "name": "Reviewed initial Models", "description": "Original selected grant provenance", "owner_ids": []string{owner.id}}
		if len(ids) > 0 {
			body["model_ids"] = ids
			body["model_review_token"] = token
		}
		return body
	}
	created := func(who actor, body map[string]any, tag string, status int) teamCreationLimitsFixtureResponse {
		t.Helper()
		out := send(who, "POST", createPath, body, tag)
		expectStatus(t, out, status)
		var result teamCreationLimitsFixtureResponse
		if json.Unmarshal(out.Body.Bytes(), &result) != nil || !result.Committed || result.Team == nil || result.Receipt.CreationID != body["creation_id"] || result.Team.ID != result.Receipt.TeamID {
			t.Fatal("safe committed Team envelope")
		}
		return result
	}
	current := contextRead(creator)
	var proof service.TeamCreationModelReview
	unchanged(func() { proof = review(creator, []string{allIDs[1], allIDs[0]}, current.ReviewETag) })
	body := input(proof.ModelIDs, proof.ModelReviewToken)
	unchanged(func() {
		expectStatus(t, send(basic, "POST", createPath, body, current.ReviewETag), 403)
		changed := input([]string{allIDs[2]}, proof.ModelReviewToken)
		expectStatus(t, send(creator, "POST", createPath, changed, current.ReviewETag), 409)
	})
	// A selected definition changed after aggregate review cannot be committed
	// with the original token. Refresh outside the deliberate mutation interval.
	if err := db.Model(&entity.Model{}).Where("id = ?", allIDs[0]).Update("Status", entity.ResourceDisabled).Error; err != nil {
		t.Fatal(err)
	}
	refresh()
	unchanged(func() { expectStatus(t, send(creator, "POST", createPath, body, current.ReviewETag), 409) })
	if err := db.Model(&entity.Model{}).Where("id = ?", allIDs[0]).Update("Status", entity.ResourceActive).Error; err != nil {
		t.Fatal(err)
	}
	refresh()
	result := created(creator, body, current.ReviewETag, 201)
	if !result.RuntimeApplied || result.ApplicationStatus != "applied" || !slices.Equal(result.Team.ModelIDs, proof.ModelIDs) {
		t.Fatal("complete initial model publication not confirmed")
	}
	readReceipt := func() entity.TeamCreationReceipt {
		t.Helper()
		var receipt entity.TeamCreationReceipt
		if err := db.Take(&receipt, "creation_id = ?", result.Receipt.CreationID).Error; err != nil {
			t.Fatal(err)
		}
		return receipt
	}
	originalReceipt := readReceipt()
	if originalReceipt.ModelSnapshotVersion != 1 || originalReceipt.ModelCount != 2 || originalReceipt.ModelDigest == nil {
		t.Fatal("normalized model receipt missing")
	}
	var children []entity.TeamCreationReceiptModel
	if err := db.Where("creation_id = ?", originalReceipt.CreationID).Order("model_id").Find(&children).Error; err != nil || len(children) != 2 {
		t.Fatal("complete immutable receipt children", err)
	}
	var grants []entity.TeamModelGrant
	if err := db.Where("team_id = ?", result.Team.ID).Order("model_id").Find(&grants).Error; err != nil || len(grants) != 2 {
		t.Fatal("initial selected grants", err)
	}
	for i, g := range grants {
		if g.ModelID != proof.ModelIDs[i] || g.SourceCreationReceiptID == nil || *g.SourceCreationReceiptID != originalReceipt.CreationID || g.SourceRequestID != nil || children[i].ModelID != g.ModelID || !children[i].ModelCreatedAt.Equal(stamp) {
			t.Fatal("grant source or Model birth missing")
		}
	}
	unchanged(func() {
		retry := created(creator, body, current.ReviewETag, 200)
		if retry.Team.ID != result.Team.ID || !retry.RuntimeApplied || !teamCreationReceiptFixtureEqual(readReceipt(), originalReceipt) {
			t.Fatal("identical retry wrote/restored receipt")
		}
	})
	// A normal Model disable removes routing eligibility, not configured grants.
	if err := db.Model(&entity.Model{}).Where("id = ?", allIDs[0]).Update("Status", entity.ResourceDisabled).Error; err != nil {
		t.Fatal(err)
	}
	refresh()
	unchanged(func() {
		retry := created(creator, body, current.ReviewETag, 200)
		if !retry.RuntimeApplied || retry.ApplicationStatus != "applied" || !slices.Equal(retry.Team.ModelIDs, proof.ModelIDs) {
			t.Fatal("configured publication incorrectly promises route availability")
		}
	})
	if err := db.Model(&entity.Model{}).Where("id = ?", allIDs[0]).Update("Status", entity.ResourceActive).Error; err != nil {
		t.Fatal(err)
	}
	refresh()
	// Reusing the Model ID with a changed persisted birth cannot reconcile the
	// original child. Restore only the controlled fixture state, never by retry.
	changedBirth := stamp.Add(time.Millisecond)
	if err := db.Model(&entity.Model{}).Where("id = ?", allIDs[0]).Update("CreatedAt", changedBirth).Error; err != nil {
		t.Fatal(err)
	}
	var reincarnated entity.Model
	if err := db.Take(&reincarnated, "id = ?", allIDs[0]).Error; err != nil || !reincarnated.CreatedAt.Equal(changedBirth) {
		t.Fatal("persisted Model birth mutation", err)
	}
	refresh()
	unchanged(func() {
		replay := created(creator, body, current.ReviewETag, 200)
		if replay.RuntimeApplied || replay.ApplicationStatus != "superseded" {
			t.Fatal("receipt inherited a reused Model ID")
		}
	})
	if err := db.Model(&entity.Model{}).Where("id = ?", allIDs[0]).Update("CreatedAt", stamp).Error; err != nil {
		t.Fatal(err)
	}
	refresh()
	// An unchanged ordinary replacement preserves origin; removal/re-add must not.
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, result.Team.ID, proof.ModelIDs); err != nil {
		t.Fatal(err)
	}
	refresh()
	unchanged(func() {
		if !created(creator, body, current.ReviewETag, 200).RuntimeApplied {
			t.Fatal("unchanged replacement lost provenance")
		}
	})
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, result.Team.ID, []string{allIDs[1]}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, result.Team.ID, proof.ModelIDs); err != nil {
		t.Fatal(err)
	}
	refresh()
	unchanged(func() {
		retry := created(creator, body, current.ReviewETag, 200)
		if retry.RuntimeApplied || retry.ApplicationStatus != "superseded" || !teamCreationReceiptFixtureEqual(readReceipt(), originalReceipt) {
			t.Fatal("old receipt adopted re-added grant")
		}
	})
	var readded entity.TeamModelGrant
	if err := db.Take(&readded, "team_id = ? AND model_id = ?", result.Team.ID, allIDs[0]).Error; err != nil || readded.SourceCreationReceiptID != nil {
		t.Fatal("ordinary re-add retained original source", err)
	}
	// Every saved retry reauthorizes independently; a historical receipt is no
	// capability and never replaces current model-writing permission.
	var creatorAssignment entity.UserRole
	if err := db.Take(&creatorAssignment, "user_id = ?", creator.id).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveRole(ctx, admin.User.ID, creatorAssignment.RoleID, "System tim-creator", []string{"teams.write"}); err != nil {
		t.Fatal(err)
	}
	unchanged(func() { expectStatus(t, send(creator, "POST", createPath, body, current.ReviewETag), 403) })
	if _, err := svc.SaveRole(ctx, admin.User.ID, creatorAssignment.RoleID, "System tim-creator", []string{"teams.write", "teams.models.write"}); err != nil {
		t.Fatal(err)
	}
	refresh()
	// Failure of grant, normalized receipt child, parent, or typed audit rolls back
	// every Team/default policy/membership/grant/receipt/history row atomically.
	for _, table := range []string{"team_model_grants", "team_creation_receipt_models", "team_creation_receipts", "audit_events"} {
		refresh()
		fresh := contextRead(creator)
		selected := review(creator, []string{allIDs[2]}, fresh.ReviewETag)
		failed := input(selected.ModelIDs, selected.ModelReviewToken)
		fault.Store(table)
		unchanged(func() { expectStatus(t, send(creator, "POST", createPath, failed, fresh.ReviewETag), 500) })
		fault.Store("")
		recovered := created(creator, failed, fresh.ReviewETag, 201)
		if !recovered.RuntimeApplied {
			t.Fatal("exact rolled-back retry did not apply")
		}
	}
	// Complete 1000+1000 selection stays inside the reviewed Team-only 128 KiB
	// reader while exceeding legacy 64 KiB; no complete catalogue review is used.
	ownerRows := make([]entity.User, 1000)
	ownerIDs := make([]string, 1000)
	for i := range ownerRows {
		ownerIDs[i] = fmt.Sprintf("usr_%026d", i)
		ownerRows[i] = entity.User{ID: ownerIDs[i], Email: fmt.Sprintf("tim-max-%d@example.invalid", i), Name: "Admitted retained owner", Role: entity.RoleMember, PasswordHash: "fixture-not-a-login", CreatedAt: stamp, UpdatedAt: stamp}
	}
	if err := db.CreateInBatches(&ownerRows, 500).Error; err != nil {
		t.Fatal(err)
	}
	refresh()
	maxContext := contextRead(administrator)
	allReview := review(administrator, allIDs, maxContext.ReviewETag)
	maximal := input(allIDs, allReview.ModelReviewToken)
	maximal["owner_ids"] = ownerIDs
	maximal["description"] = strings.Repeat("<", 2000)
	raw, err := json.Marshal(maximal)
	if err != nil || len(raw) <= 64<<10 || len(raw) > 128<<10 {
		t.Fatal("canonical maximal reviewed body boundary", err, len(raw))
	}
	unchanged(func() { expectStatus(t, sendRaw(administrator, "POST", createPath, string(raw), ""), 400) })
	maximum := created(administrator, maximal, maxContext.ReviewETag, 201)
	if !maximum.RuntimeApplied || len(maximum.Team.ModelIDs) != 1000 || len(maximum.Team.Members) != 1000 {
		t.Fatal("complete maximum owner/model set")
	}
	var childCount int64
	if err := db.Model(&entity.TeamCreationReceiptModel{}).Where("creation_id = ?", maximum.Receipt.CreationID).Count(&childCount).Error; err != nil || childCount != 1000 {
		t.Fatal("maximum normalized children incomplete", err, childCount)
	}
	var nativeTeam teamCreationLimitsFixtureResponse

	native := func(name, teamID, bearer string) *httptest.ResponseRecorder {
		t.Helper()
		bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		path := "/api/v1/teams/" + teamID + "/chat/completions"
		if bearer != "" {
			path = "/v1/chat/completions"
		}
		req := httptest.NewRequestWithContext(bounded, "POST", "http://routex.test"+path, strings.NewReader(`{"model":"`+name+`","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":1}`))
		req.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		} else {
			req.Header.Set("Origin", "http://routex.test")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			req.Header.Set("X-CSRF-Token", owner.csrf)
			req.AddCookie(owner.cookie)
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	flush := func() {
		t.Helper()
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
		refresh()
	}
	expectStatus(t, native(names[0].Name, "", warmup), 200)
	flush()
	refresh()
	nativeContext := contextRead(administrator)
	selected := review(administrator, []string{allIDs[0]}, nativeContext.ReviewETag)
	nativeBody := input(selected.ModelIDs, selected.ModelReviewToken)
	nativeBody["initial_limits"] = map[string]any{"tokens_month": 2, "reason": "Explicit initial native token cap"}
	nativeTeam = created(administrator, nativeBody, nativeContext.ReviewETag, 201)
	expectStatus(t, native(names[1].Name, nativeTeam.Team.ID, ""), 404)
	flush()
	expectStatus(t, native(names[0].Name, nativeTeam.Team.ID, ""), 200)
	flush()
	expectStatus(t, native(names[0].Name, nativeTeam.Team.ID, ""), 429)
	flush()
	if dispatches.Load() != 2 {
		t.Fatal("unselected or hard-stopped request dispatched")
	}
	var calls []entity.CallRecord
	if err := db.Where("team_id = ?", nativeTeam.Team.ID).Find(&calls).Error; err != nil || len(calls) != 3 {
		t.Fatal("Team native facts incomplete", err, len(calls))
	}
	completed, denied := 0, 0
	for _, call := range calls {
		if call.UserID != owner.id || call.KeyID != "" || call.ProjectID != "" || call.TeamID != nativeTeam.Team.ID {
			t.Fatal("Team Session attribution changed")
		}
		var attempts []entity.CallAttempt
		if err := db.Where("request_id = ?", call.RequestID).Find(&attempts).Error; err != nil {
			t.Fatal(err)
		}
		if call.Status == "success" {
			completed++
			if len(attempts) != 1 || attempts[0].CredentialID != "crd_tim" || attempts[0].SnapshotID == "" || attempts[0].NativeCompletionEvidence != "completed" {
				t.Fatal("native completion evidence missing")
			}
		} else {
			denied++
			if len(attempts) != 0 {
				t.Fatal("denial created attempt")
			}
		}
	}
	if completed != 1 || denied != 2 {
		t.Fatal("initial native/hardstop oracle", completed, denied)
	}
	svc.StopRuntime()
	if err := svc.StopCallRecorder(); err != nil {
		t.Error(err)
	}
	restarted, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.StopRuntime()
	defer func() {
		if err := restarted.StopCallRecorder(); err != nil {
			t.Error(err)
		}
	}()
	if err := restarted.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if err := restarted.StartCallRecorder(ctx, journalPath); err != nil {
		t.Fatal(err)
	}
	// Decode the exact original submitted body for restart, preserving sparse limit
	// presence and reason rather than constructing a different retry by hand.
	retryRaw, _ := json.Marshal(nativeBody)
	var exact service.TeamCreationInput
	if err := exact.UnmarshalJSON(retryRaw); err != nil {
		t.Fatal(err)
	}
	exact.ReviewETag = nativeContext.ReviewETag
	replay, err := restarted.CreateTeamWithInitialLimits(ctx, administrator.id, exact)
	if err != nil || replay.Created || !replay.RuntimeApplied || replay.Team.ID != nativeTeam.Team.ID {
		t.Fatal("restart original grant receipt reconciliation", err)
	}
	sessionRouter := fox.New()
	New(restarted).RegisterRoutes(sessionRouter)
	expectStatus(t, identityRequest(sessionRouter, "GET", "/api/v1/auth/session", "", owner.cookie, ""), 200)
	if dispatches.Load() != 2 {
		t.Fatal("restart replay dispatched inference")
	}
}

func TestTeamCreationModelsFixtureMaximumReviewedBody(t *testing.T) {
	owners := make([]string, 1000)
	models := make([]string, 1000)
	for i := range owners {
		owners[i] = fmt.Sprintf("usr_%026d", i)
		models[i] = fmt.Sprintf("mdl_%026d", i)
	}
	cap := int64(9007199254740991)
	limits := map[string]any{"tokens_5h": cap, "tokens_7d": cap, "tokens_month": cap, "rpm": cap, "tpm": cap, "concurrency": cap, "money_month": "999999999999999999.999999999999999999", "currency": "USD", "reason": strings.Repeat("<", 2000)}
	body := map[string]any{"creation_id": "66000000-0000-4000-8000-000000000001", "name": strings.Repeat("<", 100), "description": strings.Repeat("<", 2000), "owner_ids": owners, "model_ids": models, "model_review_token": strings.Repeat("a", 64), "initial_limits": limits}
	raw, err := json.Marshal(body)
	if err != nil || len(raw) <= 64<<10 || len(raw) > 128<<10 {
		t.Fatal("supported canonical full creation does not fit Team cap", len(raw), err)
	}
	var decoded service.TeamCreationInput
	if err := decoded.UnmarshalJSON(raw); err != nil || !slices.Equal(decoded.OwnerIDs, owners) || !slices.Equal(decoded.ModelIDs, models) || decoded.ModelReviewToken != strings.Repeat("a", 64) {
		t.Fatal("maximum body lost selected facts", err)
	}
	for _, invalid := range []string{`{"creation_id":"66000000-0000-4000-8000-000000000001","model_ids":null}`, `{"model_ids":[]}`, `{"creation_id":"66000000-0000-4000-8000-000000000001","model_ids":[],"model_review_token":"` + strings.Repeat("a", 64) + `"}`} {
		var input service.TeamCreationInput
		if input.UnmarshalJSON([]byte(invalid)) == nil {
			t.Fatal("invalid opt-in normalized to legacy", invalid)
		}
	}
}
