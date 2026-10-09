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
	"regexp"
	"slices"
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
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

type teamCreationLimitsFixtureResponse struct {
	Team    *TeamResponse `json:"team"`
	Receipt struct {
		CreationID string    `json:"creation_id"`
		TeamID     string    `json:"team_id"`
		CreatedAt  time.Time `json:"created_at"`
	} `json:"receipt"`
	Committed         bool   `json:"committed"`
	RuntimeApplied    bool   `json:"runtime_applied"`
	ApplicationStatus string `json:"application_status"`
}

// Root registers this new lifecycle only after the reviewed production route and
// V63 predecessor are carried. It is not a standalone driver test or bypass.
func testTeamCreationLimitsLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var faultTable atomic.Value
	faultTable.Store("")
	var publicationFailure atomic.Bool
	var auditCommitFailure atomic.Bool
	var creationBarrier atomic.Pointer[teamCreationFixtureBarrier]
	const (
		createHook = "team-creation-atomic-fault"
		queryHook  = "team-creation-publication-fault"
	)
	if err := db.Callback().Create().Before("gorm:create").Register(createHook, func(tx *gorm.DB) {
		if tx.Statement.Table == "teams" {
			if gate := creationBarrier.Load(); gate != nil {
				gate.once.Do(func() {
					close(gate.entered)
					select {
					case <-gate.release:
					case <-tx.Statement.Context.Done():
						_ = tx.AddError(tx.Statement.Context.Err())
					}
				})
			}
		}
		if auditCommitFailure.Load() && tx.Statement.Table == "audit_events" {
			if event, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && event.Action == "team.creation.commit" {
				_ = tx.AddError(errors.New("controlled typed creation audit fault"))
			}
		}
		if table := faultTable.Load().(string); table != "" && tx.Statement.Table == table {
			_ = tx.AddError(errors.New("controlled Team creation transaction fault"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register(queryHook, func(tx *gorm.DB) {
		if publicationFailure.Load() && tx.Statement.Table == "providers" {
			_ = tx.AddError(errors.New("controlled Team creation publication fault"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	// Register immutable callbacks before any runtime worker starts; remove only
	// after all later service shutdown defers, never mutate the callback registry live.
	defer func() { _ = db.Callback().Create().Remove(createHook); _ = db.Callback().Query().Remove(queryHook) }()
	store, err := secretstore.New(bytes.Repeat([]byte{163}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var instances []*service.Service
	defer func() {
		faultTable.Store("")
		publicationFailure.Store(false)
		auditCommitFailure.Store(false)
		if gate := creationBarrier.Load(); gate != nil {
			gate.closeOnce.Do(func() { close(gate.release) })
		}
		for _, svc := range instances {
			svc.StopRuntime()
			if err := svc.StopCallRecorder(); err != nil {
				t.Error(err)
			}
		}
	}()
	makeService := func() *service.Service {
		t.Helper()
		svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, svc)
		return svc
	}
	svc := makeService()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	admin, adminCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/setup", `{"email":"team-create-admin@example.invalid","password":"test-only-create-password","name":"Team creation administrator"}`, nil, ""))
	type actor struct {
		id, csrf string
		cookie   *http.Cookie
	}
	administrator := actor{admin.User.ID, admin.CSRFToken, adminCookie}
	member := func(name string, permissions []string) actor {
		t.Helper()
		record, cookie, csrf := createSystemStatusMember(t, svc, router, admin.User.ID, name, permissions)
		return actor{record.User.ID, csrf, cookie}
	}
	ordinary := member("tc-owner", nil)
	peer := member("tc-peer", nil)
	creator := member("tc-basic", []string{"teams.write"})
	tokens := member("tc-tokens", []string{"teams.write", "teams.tokens.write"})
	money := member("tc-money", []string{"teams.write", "teams.money.write"})
	rates := member("tc-rates", []string{"teams.write", "teams.rates.write"})
	all := member("tc-all", []string{"teams.write", "teams.read_all", "teams.tokens.write", "teams.money.write", "teams.rates.write"})
	type requestFn func(actor, string, string, any, string) *httptest.ResponseRecorder
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
	var send requestFn = func(who actor, method, path string, body any, etag string) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return sendRaw(who, method, path, string(raw), etag)
	}
	contextPath := "/api/v1/admin/teams/creation-context"
	createPath := "/api/v1/admin/teams"
	readContext := func(who actor) service.TeamCreationContext {
		t.Helper()
		res := send(who, "GET", contextPath, nil, "")
		value := decodeCatalogResponse[service.TeamCreationContext](t, res, 200)
		if len(value.ReviewETag) != 64 || len(value.DefaultRuleETag) != 64 || res.Header().Get("ETag") != `"`+value.ReviewETag+`"` || res.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("creation context validator/privacy mismatch")
		}
		return value
	}
	writeDefault := func(values map[string]any) service.DefaultLimitRecord {
		t.Helper()
		path := "/api/v1/admin/default-limits/team"
		before := decodeCatalogResponse[service.DefaultLimitRecord](t, send(administrator, "GET", path, nil, ""), 200)
		policy := map[string]any{"tokens_5h": 17, "tokens_7d": 19, "tokens_month": 23, "money_month": "7.000000000000000001", "currency": "USD", "rpm": 29, "tpm": 31, "concurrency": 3}
		for k, v := range values {
			policy[k] = v
		}
		return decodeCatalogResponse[service.DefaultLimitRecord](t, send(administrator, "PUT", path, map[string]any{"policy": policy, "reason": "Reviewed initial Team defaults"}, before.ETag), 200)
	}
	writeDefault(nil)
	var sequence int
	uuid := func() string { sequence++; return fmt.Sprintf("63000000-0000-4000-8000-%012d", sequence) }
	input := func(ownerIDs []string) map[string]any {
		return map[string]any{"creation_id": uuid(), "name": "Reviewed Team", "description": "Explicit initial capacity", "owner_ids": slices.Clone(ownerIDs)}
	}
	tables := []string{"teams", "team_memberships", "resource_limits", "team_model_grants", "team_roles", "api_keys", "project_api_keys", "audit_events", "team_creation_receipts"}
	capture := func() []int64 {
		t.Helper()
		n := make([]int64, len(tables))
		for i, table := range tables {
			if err := db.Table(table).Count(&n[i]).Error; err != nil {
				t.Fatal(err)
			}
		}
		return n
	}
	unchanged := func(label string, fn func()) {
		t.Helper()
		before := capture()
		fn()
		if !slices.Equal(before, capture()) {
			t.Fatal("failed operation changed atomic state", label)
		}
	}
	created := func(who actor, body map[string]any, etag string, status int) teamCreationLimitsFixtureResponse {
		t.Helper()
		res := send(who, "POST", createPath, body, etag)
		value := decodeCatalogResponse[teamCreationLimitsFixtureResponse](t, res, status)
		if !value.Committed || value.Team == nil || value.Team.ID != value.Receipt.TeamID || value.Receipt.CreationID != body["creation_id"] || value.Receipt.CreatedAt.IsZero() || (status == 201 && len(value.Team.ModelIDs) != 0) {
			t.Fatal("saved creation lost receipt or invented grants")
		}
		return value
	}
	readPolicy := func(id string) entity.ResourceLimit {
		t.Helper()
		var row entity.ResourceLimit
		if err := db.Where("scope_kind = ? AND scope_id = ?", "team", id).Take(&row).Error; err != nil || row.ScopeKind != "team" || row.ScopeID != id {
			t.Fatal("stored Team policy mismatch", err)
		}
		return row
	}
	readReceipt := func(id string) entity.TeamCreationReceipt {
		t.Helper()
		var row entity.TeamCreationReceipt
		if err := db.Take(&row, "creation_id = ?", id).Error; err != nil || row.CreationID != id {
			t.Fatal(err)
		}
		return row
	}
	// Independent current authority governs both private previews and submitted caps.
	for _, sample := range []struct {
		who      actor
		fields   []string
		currency bool
	}{{creator, []string{}, false}, {tokens, []string{"tokens_5h", "tokens_7d", "tokens_month"}, false}, {money, []string{"money_month"}, true}, {rates, []string{"rpm", "tpm", "concurrency"}, false}, {all, []string{"tokens_5h", "tokens_7d", "tokens_month", "money_month", "rpm", "tpm", "concurrency"}, true}, {administrator, []string{"tokens_5h", "tokens_7d", "tokens_month", "money_month", "rpm", "tpm", "concurrency"}, true}} {
		got := readContext(sample.who)
		if !slices.Equal(got.EditableFields, sample.fields) || (got.PlatformCurrency != nil) != sample.currency {
			t.Fatal("context borrowed independent permission")
		}
		expected := slices.Clone(sample.fields)
		if sample.currency {
			expected = append(expected, "currency")
			if *got.PlatformCurrency != "USD" {
				t.Fatal("wrong denomination")
			}
		}
		if len(got.DefaultPolicy) != len(expected) {
			t.Fatal("redacted defaults encoded as null")
		}
		for _, field := range expected {
			if _, ok := got.DefaultPolicy[field]; !ok {
				t.Fatal("authorized preview field missing", field)
			}
		}
	}
	expectStatus(t, send(actor{}, "GET", contextPath, nil, ""), 401)
	expectStatus(t, send(ordinary, "GET", contextPath, nil, ""), 403)
	for _, query := range []string{"?user_id=" + ordinary.id, "?q=x", "?limit=1"} {
		expectStatus(t, send(creator, "GET", contextPath+query, nil, ""), 400)
	}
	deniedRead := send(actor{}, "GET", contextPath, nil, "")
	if deniedRead.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("denied private context lacks exact header")
	}
	// Preserve full-range integer previews as strings, never float values.
	var originalRule entity.DefaultLimitRule
	if err := db.Take(&originalRule, "kind = ?", "team").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.DefaultLimitRule{}).Where("kind = ?", "team").UpdateColumn("tokens_5h", int64(9007199254740991)).Error; err != nil {
		t.Fatal(err)
	}
	high := readContext(tokens)
	if high.DefaultPolicy["tokens_5h"] == nil || *high.DefaultPolicy["tokens_5h"] != "9007199254740991" {
		t.Fatal("supported integer preview rounded")
	}
	if err := db.Model(&entity.DefaultLimitRule{}).Where("kind = ?", "team").UpdateColumn("tokens_5h", originalRule.Tokens5H).Error; err != nil {
		t.Fatal(err)
	}
	// Legacy Team and Project paths preserve their old response and ownership semantics.
	legacy := decodeCatalogResponse[TeamResponse](t, send(administrator, "POST", createPath, map[string]any{"name": "Legacy Team", "description": "", "owner_ids": []string{ordinary.id}}, ""), 201)
	if len(legacy.Members) != 1 || legacy.Members[0].UserID != ordinary.id || len(legacy.ModelIDs) != 0 {
		t.Fatal("legacy Team changed")
	}
	legacyProject := decodeCatalogResponse[ProjectResponse](t, send(ordinary, "POST", "/api/v1/projects", map[string]any{"name": "Legacy Project"}, ""), 201)
	if len(legacyProject.ModelIDs) != 0 || len(legacyProject.Managers) != 1 || legacyProject.Managers[0].UserID != ordinary.id {
		t.Fatal("Project legacy path changed")
	}
	copiedInput := input([]string{ordinary.id})
	copiedReview := readContext(creator)
	beforeCopied := capture()
	copied := created(creator, copiedInput, copiedReview.ReviewETag, 201)
	afterCopied := capture()
	for _, index := range []int{3, 4, 5, 6} {
		if afterCopied[index] != beforeCopied[index] {
			t.Fatal("creation invented Model/Role/Key ownership", tables[index])
		}
	}
	copiedRow := readPolicy(copied.Receipt.TeamID)
	if copied.RuntimeApplied || copied.ApplicationStatus != "pending" || copiedRow.AppliedDefaultETag == nil || *copiedRow.AppliedDefaultETag != copiedReview.DefaultRuleETag || copiedRow.DefaultResetETag != nil || copiedRow.TokensMonth == nil || *copiedRow.TokensMonth != 23 || copiedRow.MoneyMonth == nil || *copiedRow.MoneyMonth != "7.000000000000000001" || copiedRow.Currency != "USD" {
		t.Fatal("saved copy/provenance interpreted as published")
	}
	var savedReceiptSnapshot service.TeamCreationSnapshot
	savedReceipt := readReceipt(copied.Receipt.CreationID)
	if err := json.Unmarshal([]byte(savedReceipt.SnapshotJSON), &savedReceiptSnapshot); err != nil || len(savedReceiptSnapshot.Owners) != 1 || savedReceiptSnapshot.Owners[0].UserID != ordinary.id || len(savedReceiptSnapshot.SubmittedFields) != 0 || savedReceiptSnapshot.DefaultRuleETag != copiedReview.DefaultRuleETag {
		t.Fatal("copied receipt proof incomplete", err)
	}
	var savedRelation entity.TeamMembership
	if err := db.Where("team_id = ? AND user_id = ?", copied.Team.ID, ordinary.id).Take(&savedRelation).Error; err != nil || savedRelation.Role != entity.TeamOwner || savedRelation.ID != savedReceiptSnapshot.Owners[0].MembershipID || savedRelation.JoinedAt == nil || !savedRelation.JoinedAt.Equal(savedReceiptSnapshot.OwnerJoinedAt) {
		t.Fatal("receipt relationship differs from persisted row", err)
	}
	var creationAudits []entity.AuditEvent
	if err := db.Where("resource_id = ?", copied.Team.ID).Find(&creationAudits).Error; err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"limits.default.apply", "resource.create", "team.creation.commit"} {
		n := 0
		for _, audit := range creationAudits {
			if audit.Action == action {
				n++
				if audit.ActorID != creator.id {
					t.Fatal("creation audit actor borrowed")
				}
			}
		}
		if n != 1 {
			t.Fatal("missing or duplicated atomic typed creation audit", action, n)
		}
	}
	unchanged("identical current retry", func() {
		again := created(creator, copiedInput, copiedReview.ReviewETag, 200)
		if !reflect.DeepEqual(again.Receipt, copied.Receipt) || again.Team.ID != copied.Team.ID {
			t.Fatal("retry created another Team")
		}
	})
	overrides := input([]string{ordinary.id, peer.id})
	overrides["initial_limits"] = map[string]any{"tokens_month": 0, "tokens_7d": nil, "money_month": nil, "rpm": 0, "reason": "Clear money and retain finite zero"}
	overridden := created(all, overrides, readContext(all).ReviewETag, 201)
	overrideRow := readPolicy(overridden.Receipt.TeamID)
	if overrideRow.TokensMonth == nil || *overrideRow.TokensMonth != 0 || overrideRow.Tokens7D != nil || overrideRow.MoneyMonth != nil || overrideRow.Currency != "" || overrideRow.RPM == nil || *overrideRow.RPM != 0 || overrideRow.Tokens5H == nil || *overrideRow.Tokens5H != 17 || overrideRow.AppliedDefaultETag != nil || overrideRow.DefaultResetETag != nil || len(overridden.Team.Members) != 2 {
		t.Fatal("sparse/null/zero override or copy provenance wrong")
	}
	precise := input([]string{ordinary.id})
	precise["initial_limits"] = map[string]any{"money_month": "12.500000000000000001", "currency": "USD", "reason": "Exact initial money"}
	moneyResult := created(money, precise, readContext(money).ReviewETag, 201)
	moneyRow := readPolicy(moneyResult.Receipt.TeamID)
	if moneyRow.MoneyMonth == nil || *moneyRow.MoneyMonth != "12.500000000000000001" || moneyRow.TokensMonth == nil || *moneyRow.TokensMonth != 23 {
		t.Fatal("decimal money rounded or omitted group not copied")
	}
	for _, sample := range []struct {
		who   actor
		field string
		value any
	}{{creator, "tokens_month", 23}, {creator, "tokens_month", nil}, {creator, "money_month", nil}, {tokens, "rpm", 0}, {money, "tokens_month", 0}, {rates, "money_month", nil}} {
		body := input([]string{ordinary.id})
		body["initial_limits"] = map[string]any{sample.field: sample.value, "reason": "Forbidden dimension"}
		review := readContext(sample.who)
		unchanged("independent cap authority", func() { expectStatus(t, send(sample.who, "POST", createPath, body, review.ReviewETag), 403) })
	}
	valid := input([]string{ordinary.id})
	review := readContext(all)
	unchanged("required review", func() { expectStatus(t, send(all, "POST", createPath, valid, ""), 428) })
	unchanged("CSRF", func() {
		who := all
		who.csrf = ""
		expectStatus(t, send(who, "POST", createPath, valid, review.ReviewETag), 403)
	})
	for _, initial := range []any{nil, map[string]any{}, map[string]any{"reason": "Reason alone"}, map[string]any{"currency": "USD", "reason": "Currency alone"}, map[string]any{"money_month": nil, "currency": "USD", "reason": "Null money currency"}, map[string]any{"tokens_month": -1, "reason": "negative"}, map[string]any{"tokens_month": 1.5, "reason": "fraction"}, map[string]any{"tokens_month": "5", "reason": "string cap"}, map[string]any{"money_month": 1.25, "currency": "USD", "reason": "numeric money"}, map[string]any{"model_ids": []string{}, "reason": "unsupported"}} {
		body := input([]string{ordinary.id})
		body["initial_limits"] = initial
		unchanged("malformed initial limits", func() { expectStatus(t, send(all, "POST", createPath, body, readContext(all).ReviewETag), 400) })
	}
	for _, raw := range []string{`{"name":"Bad","creation_id":"63000000-0000-4000-8000-000000000099","owner_ids":["` + ordinary.id + `"],"initial_limits":{"tokens_month":1,"tokens_month":2,"reason":"duplicate"}}`, `{"name":"Bad","creation_id":"63000000-0000-4000-8000-000000000099","owner_ids":["` + ordinary.id + `"],"initial_limits":{"tokens_month":9223372036854775808,"reason":"overflow"}}`, `{"name":"Bad","creation_id":"63000000-0000-4000-8000-000000000099","owner_ids":["` + ordinary.id + `"],"unknown":true}`} {
		unchanged("strict JSON", func() { expectStatus(t, sendRaw(all, "POST", createPath, raw, readContext(all).ReviewETag), 400) })
	}
	for _, owners := range [][]string{nil, {}, {ordinary.id, ordinary.id}, {strings.ToUpper(ordinary.id)}, {ordinary.id + " "}, {"usr_missing"}} {
		body := input(owners)
		unchanged("invalid owner identity", func() { expectStatus(t, send(all, "POST", createPath, body, readContext(all).ReviewETag), 400) })
	}
	// Explicit new-field presence cannot fall through to legacy creation.
	for _, creationID := range []any{"", nil, "not-a-uuid", "63000000-0000-1000-8000-000000000001"} {
		body := map[string]any{"name": "Reject invalid reviewed intent", "owner_ids": []string{ordinary.id}, "creation_id": creationID}
		unchanged("invalid present intent", func() { expectStatus(t, send(all, "POST", createPath, body, readContext(all).ReviewETag), 400) })
	}
	// Fresh authority is independent of captured review and remains necessary on
	// both a new dispatch and an exact saved receipt retry.
	var assigned []entity.UserRole
	if err := db.Where("user_id = ?", all.id).Find(&assigned).Error; err != nil || len(assigned) != 1 {
		t.Fatal("fixture authority role", err)
	}
	allPermissions := []string{"teams.write", "teams.read_all", "teams.tokens.write", "teams.money.write", "teams.rates.write"}
	authorityReview := readContext(all)
	authorityBody := input([]string{ordinary.id})
	authorityBody["initial_limits"] = map[string]any{"tokens_month": 8, "reason": "Independent current authority"}
	if _, err := svc.SaveRole(ctx, admin.User.ID, assigned[0].RoleID, "System tc-all", []string{"teams.write", "teams.read_all", "teams.money.write", "teams.rates.write"}); err != nil {
		t.Fatal(err)
	}
	unchanged("revoked current dimension", func() {
		expectStatus(t, send(all, "POST", createPath, authorityBody, authorityReview.ReviewETag), 403)
		expectStatus(t, send(all, "POST", createPath, overrides, readReceipt(overridden.Receipt.CreationID).ReviewETag), 403)
	})
	if _, err := svc.SaveRole(ctx, admin.User.ID, assigned[0].RoleID, "System tc-all", allPermissions); err != nil {
		t.Fatal(err)
	}
	// Exercise the actual member-role identity generation, independently of an
	// equal restored permission set, so a captured creation review stays stale.
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, all.id, []string{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, all.id, []string{assigned[0].RoleID}); err != nil {
		t.Fatal(err)
	}
	unchanged("restored authority generation", func() { expectStatus(t, send(all, "POST", createPath, authorityBody, authorityReview.ReviewETag), 409) })
	unchanged("saved retry current restored authority", func() {
		retry := created(all, overrides, readReceipt(overridden.Receipt.CreationID).ReviewETag, 200)
		if retry.Team.ID != overridden.Team.ID {
			t.Fatal("permission renewal duplicated saved target")
		}
	})
	// Recheck complete owner admission, not just the existing IN/count helper.
	var savedOwner entity.User
	if err := db.Take(&savedOwner, "id = ?", ordinary.id).Error; err != nil {
		t.Fatal(err)
	}
	offboardedAt := savedOwner.CreatedAt.Add(time.Hour)
	for _, state := range []struct {
		field        string
		bad, restore any
	}{
		{"disabled", true, savedOwner.Disabled},
		{"offboarded_at", offboardedAt, savedOwner.OffboardedAt},
	} {
		if err := db.Model(&entity.User{}).Where("id = ?", ordinary.id).UpdateColumn(state.field, state.bad).Error; err != nil {
			t.Fatal(err)
		}
		var inactiveOwner entity.User
		if err := db.Take(&inactiveOwner, "id = ?", ordinary.id).Error; err != nil {
			t.Fatal(err)
		}
		expected := savedOwner
		if state.field == "disabled" {
			expected.Disabled = true
		} else {
			expected.OffboardedAt = &offboardedAt
		}
		if !teamCreationFixtureHistoryEqual(inactiveOwner, expected) {
			t.Fatal("inactive selected owner mutation did not persist exactly", state.field)
		}
		body := input([]string{ordinary.id})
		unchanged("inactive selected owner", func() { expectStatus(t, send(all, "POST", createPath, body, readContext(all).ReviewETag), 400) })
		if err := db.Model(&entity.User{}).Where("id = ?", ordinary.id).UpdateColumn(state.field, state.restore).Error; err != nil {
			t.Fatal(err)
		}
		var restoredOwner entity.User
		if err := db.Take(&restoredOwner, "id = ?", ordinary.id).Error; err != nil || !teamCreationFixtureHistoryEqual(restoredOwner, savedOwner) {
			t.Fatal("inactive selected owner restoration changed retained facts", err)
		}
	}
	pendingID, err := id.NewPrefixed("raa")
	if err != nil {
		t.Fatal(err)
	}
	app := entity.RegistrationApprovalApplication{ID: pendingID, UserID: ordinary.id, UserCreatedAt: savedOwner.CreatedAt, CreatedAt: savedOwner.CreatedAt, State: "pending", Revision: strings.Repeat("a", 64)}
	if err := db.Create(&app).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.User{}).Where("id = ?", ordinary.id).UpdateColumn("approval_application_id", pendingID).Error; err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"pending", "rejected"} {
		changes := map[string]any{"state": state}
		if state == "rejected" {
			changes["decided_at"] = savedOwner.CreatedAt.Add(time.Hour)
			changes["decision_actor_id"] = admin.User.ID
			changes["decision_reason"] = "Rejected fixture owner"
		}
		if err := db.Model(&entity.RegistrationApprovalApplication{}).Where("id = ?", pendingID).Updates(changes).Error; err != nil {
			t.Fatal(err)
		}
		body := input([]string{ordinary.id})
		unchanged("unadmitted owner", func() { expectStatus(t, send(all, "POST", createPath, body, readContext(all).ReviewETag), 400) })
	}
	if err := db.Model(&entity.User{}).Where("id = ?", ordinary.id).UpdateColumn("approval_application_id", nil).Error; err != nil {
		t.Fatal(err)
	}
	// Current default generation and pricing generation fence stale reviewed intent.
	stale := readContext(all)
	staleInput := input([]string{ordinary.id})
	writeDefault(map[string]any{"tokens_month": 24})
	writeDefault(nil)
	unchanged("equal-value default generation", func() { expectStatus(t, send(all, "POST", createPath, staleInput, stale.ReviewETag), 409) })
	pricingBefore := entity.PricingSetting{}
	if err := db.Take(&pricingBefore, "id = ?", 1).Error; err != nil {
		t.Fatal(err)
	}
	stale = readContext(all)
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).UpdateColumns(map[string]any{"ETag": strings.Repeat("b", 64), "platform_currency": "EUR"}).Error; err != nil {
		t.Fatal(err)
	}
	var pricingChanged entity.PricingSetting
	if err := db.Take(&pricingChanged, "id = ?", 1).Error; err != nil || pricingChanged.ETag != strings.Repeat("b", 64) || pricingChanged.PlatformCurrency != "EUR" {
		t.Fatal("controlled pricing generation did not persist", err)
	}
	unchanged("pricing and currency generation", func() {
		expectStatus(t, send(all, "POST", createPath, input([]string{ordinary.id}), stale.ReviewETag), 409)
	})
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).UpdateColumns(map[string]any{"ETag": pricingBefore.ETag, "platform_currency": pricingBefore.PlatformCurrency}).Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"teams", "team_memberships", "resource_limits", "audit_events", "team_creation_receipts"} {
		body := input([]string{ordinary.id})
		body["initial_limits"] = map[string]any{"tokens_month": 5, "reason": "Atomic override"}
		review := readContext(all)
		unchanged("atomic "+table, func() {
			faultTable.Store(table)
			expectStatus(t, send(all, "POST", createPath, body, review.ReviewETag), 500)
			faultTable.Store("")
		})
	}
	typedBody := input([]string{ordinary.id})
	typedBody["initial_limits"] = map[string]any{"tokens_month": 5, "reason": "Typed commit audit rollback"}
	typedReview := readContext(all)
	unchanged("typed commit audit", func() {
		auditCommitFailure.Store(true)
		expectStatus(t, send(all, "POST", createPath, typedBody, typedReview.ReviewETag), 500)
		auditCommitFailure.Store(false)
	})
	// Hold the actual Team insert inside the governance transaction. A concurrent
	// default writer cannot mix its later values into this already reviewed create.
	raceReview := readContext(all)
	raceBody := input([]string{ordinary.id})
	raceDefaultPath := "/api/v1/admin/default-limits/team"
	raceDefaultReview := decodeCatalogResponse[service.DefaultLimitRecord](t, send(administrator, "GET", raceDefaultPath, nil, ""), 200)
	gate := &teamCreationFixtureBarrier{entered: make(chan struct{}), release: make(chan struct{})}
	creationBarrier.Store(gate)
	var raceWG sync.WaitGroup
	createReply := make(chan *httptest.ResponseRecorder, 1)
	defaultReply := make(chan *httptest.ResponseRecorder, 1)
	// Register teardown before any failure-capable wait. Callback registration itself
	// remains immutable; only this exact one-operation atomic gate is changed.
	defer func() { gate.closeOnce.Do(func() { close(gate.release) }); raceWG.Wait() }()
	raceWG.Go(func() { createReply <- send(all, "POST", createPath, raceBody, raceReview.ReviewETag) })
	select {
	case <-gate.entered:
	case <-time.After(15 * time.Second):
		t.Fatal("creation transaction barrier not entered")
	}
	writerStarted := make(chan struct{})
	raceWG.Go(func() {
		close(writerStarted)
		defaultReply <- send(administrator, "PUT", raceDefaultPath, map[string]any{"policy": map[string]any{"tokens_5h": 17, "tokens_7d": 19, "tokens_month": 43, "money_month": "7.000000000000000001", "currency": "USD", "rpm": 29, "tpm": 31, "concurrency": 3}, "reason": "Concurrent later defaults"}, raceDefaultReview.ETag)
	})
	<-writerStarted
	gate.closeOnce.Do(func() { close(gate.release) })
	creationBarrier.Store(nil)
	var raceResult teamCreationLimitsFixtureResponse
	select {
	case res := <-createReply:
		raceResult = decodeCatalogResponse[teamCreationLimitsFixtureResponse](t, res, 201)
	case <-time.After(15 * time.Second):
		t.Fatal("reviewed creation did not release transaction")
	}
	select {
	case res := <-defaultReply:
		decodeCatalogResponse[service.DefaultLimitRecord](t, res, 200)
	case <-time.After(15 * time.Second):
		t.Fatal("concurrent default writer did not finish")
	}
	racePolicy := readPolicy(raceResult.Receipt.TeamID)
	if racePolicy.TokensMonth == nil || *racePolicy.TokensMonth != 23 || racePolicy.MoneyMonth == nil || *racePolicy.MoneyMonth != "7.000000000000000001" || racePolicy.Currency != "USD" || racePolicy.AppliedDefaultETag == nil || *racePolicy.AppliedDefaultETag != raceReview.DefaultRuleETag {
		t.Fatal("concurrent creation mixed reviewed defaults")
	}
	unchanged("race saved receipt", func() {
		retry := created(all, raceBody, raceReview.ReviewETag, 200)
		if retry.Team.ID != raceResult.Team.ID || !reflect.DeepEqual(readPolicy(retry.Team.ID), racePolicy) {
			t.Fatal("default race recopied historical target")
		}
	})
	writeDefault(nil)
	// Concurrent identical reviewed intent records one resource and exact receipt.
	concurrentBody := input([]string{ordinary.id})
	concurrentReview := readContext(all)
	beforeConcurrent := capture()
	responses := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { responses <- send(all, "POST", createPath, concurrentBody, concurrentReview.ReviewETag) })
	}
	wg.Wait()
	close(responses)
	statuses := []int{}
	teamIDs := []string{}
	for res := range responses {
		statuses = append(statuses, res.Code)
		if res.Code != 200 && res.Code != 201 {
			t.Fatal("concurrent reviewed create failed", res.Code)
		}
		var result teamCreationLimitsFixtureResponse
		if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		teamIDs = append(teamIDs, result.Receipt.TeamID)
	}
	slices.Sort(statuses)
	if !slices.Equal(statuses, []int{200, 201}) || teamIDs[0] != teamIDs[1] {
		t.Fatal("concurrent creation duplicated intent")
	}
	afterConcurrent := capture()
	if afterConcurrent[0] != beforeConcurrent[0]+1 || afterConcurrent[1] != beforeConcurrent[1]+1 || afterConcurrent[8] != beforeConcurrent[8]+1 {
		t.Fatal("concurrent atomic cardinality")
	}
	// Current receipt lookup precedes changed defaults; it never reapplies originals.
	originalReceipt := readReceipt(copied.Receipt.CreationID)
	writeDefault(map[string]any{"tokens_month": 41})
	unchanged("saved receipt after default change", func() {
		retry := created(creator, copiedInput, copiedReview.ReviewETag, 200)
		if !reflect.DeepEqual(retry.Receipt, copied.Receipt) || !reflect.DeepEqual(readPolicy(copied.Team.ID), copiedRow) {
			t.Fatal("retry recopied current default")
		}
	})
	collision := input([]string{ordinary.id})
	collision["creation_id"] = copiedInput["creation_id"]
	collision["name"] = "Different request"
	unchanged("UUID payload collision", func() { expectStatus(t, send(creator, "POST", createPath, collision, copiedReview.ReviewETag), 409) })
	presenceOriginal := input([]string{ordinary.id})
	presenceReview := readContext(all)
	created(all, presenceOriginal, presenceReview.ReviewETag, 201)
	changedPresence := map[string]any{}
	for k, v := range presenceOriginal {
		changedPresence[k] = v
	}
	changedPresence["initial_limits"] = map[string]any{"tokens_month": 41, "reason": "Presence differs"}
	unchanged("receipt actor mismatch", func() { expectStatus(t, send(all, "POST", createPath, copiedInput, copiedReview.ReviewETag), 409) })
	unchanged("receipt presence mismatch", func() {
		expectStatus(t, send(all, "POST", createPath, changedPresence, presenceReview.ReviewETag), 409)
	})
	wrongCurrency := input([]string{ordinary.id})
	wrongCurrency["initial_limits"] = map[string]any{"money_month": "1", "currency": "EUR", "reason": "Wrong current denomination"}
	unchanged("conditional current currency", func() {
		expectStatus(t, send(money, "POST", createPath, wrongCurrency, readContext(money).ReviewETag), 400)
	})
	// Equal owner IDs never reconcile a different retained identity incarnation.
	var ownerBirth entity.User
	if err := db.Take(&ownerBirth, "id = ?", ordinary.id).Error; err != nil {
		t.Fatal(err)
	}
	changedBirth := ownerBirth.CreatedAt.Add(time.Millisecond)
	if err := db.Model(&entity.User{}).Where("id = ?", ordinary.id).UpdateColumn("created_at", changedBirth).Error; err != nil {
		t.Fatal(err)
	}
	var changedOwner entity.User
	if err := db.Take(&changedOwner, "id = ?", ordinary.id).Error; err != nil || !changedOwner.CreatedAt.Equal(changedBirth) {
		t.Fatal("owner birth mutation not persisted", err)
	}
	unchanged("owner birth supersession", func() {
		retry := created(creator, copiedInput, copiedReview.ReviewETag, 200)
		if retry.RuntimeApplied || retry.ApplicationStatus != "superseded" {
			t.Fatal("receipt accepted reused owner identity")
		}
	})
	if err := db.Model(&entity.User{}).Where("id = ?", ordinary.id).UpdateColumn("created_at", ownerBirth.CreatedAt).Error; err != nil {
		t.Fatal(err)
	}
	// A real concurrent role-definition writer serializes with creation under the
	// existing governance lock. Postcommit reauthorization may return403 while the
	// exact receipt remains durable; preserve original body/review for reconciliation.
	permissionBody := input([]string{ordinary.id})
	permissionBody["initial_limits"] = map[string]any{"tokens_month": 8, "reason": "Concurrent permission fence"}
	permissionReview := readContext(all)
	permissionGate := &teamCreationFixtureBarrier{entered: make(chan struct{}), release: make(chan struct{})}
	creationBarrier.Store(permissionGate)
	permissionReplies := make(chan *httptest.ResponseRecorder, 1)
	permissionWriter := make(chan error, 1)
	var permissionWG sync.WaitGroup
	defer func() { permissionGate.closeOnce.Do(func() { close(permissionGate.release) }); permissionWG.Wait() }()
	permissionWG.Go(func() {
		permissionReplies <- send(all, "POST", createPath, permissionBody, permissionReview.ReviewETag)
	})
	select {
	case <-permissionGate.entered:
	case <-time.After(15 * time.Second):
		t.Fatal("permission race did not enter actual creation transaction")
	}
	permissionCtx, permissionCancel := context.WithTimeout(ctx, 15*time.Second)
	defer permissionCancel()
	permissionStarted := make(chan struct{})
	permissionWG.Go(func() {
		close(permissionStarted)
		_, err := svc.SaveRole(permissionCtx, admin.User.ID, assigned[0].RoleID, "System tc-all", []string{"teams.write", "teams.read_all", "teams.money.write", "teams.rates.write"})
		permissionWriter <- err
	})
	<-permissionStarted
	permissionGate.closeOnce.Do(func() { close(permissionGate.release) })
	creationBarrier.Store(nil)
	var permissionFirst *httptest.ResponseRecorder
	select {
	case permissionFirst = <-permissionReplies:
	case <-time.After(15 * time.Second):
		t.Fatal("permission creation did not finish")
	}
	if permissionFirst.Code != 201 && permissionFirst.Code != 403 {
		t.Fatal("unexpected serialized permission result", permissionFirst.Code)
	}
	select {
	case err := <-permissionWriter:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("permission writer did not finish")
	}
	permissionReceipt := readReceipt(permissionBody["creation_id"].(string))
	permissionPolicy := readPolicy(permissionReceipt.TeamID)
	if permissionPolicy.TokensMonth == nil || *permissionPolicy.TokensMonth != 8 {
		t.Fatal("concurrent role writer partially committed policy")
	}
	unchanged("permission race retained retry denied", func() {
		expectStatus(t, send(all, "POST", createPath, permissionBody, permissionReview.ReviewETag), 403)
	})
	if _, err := svc.SaveRole(ctx, admin.User.ID, assigned[0].RoleID, "System tc-all", allPermissions); err != nil {
		t.Fatal(err)
	}
	unchanged("permission race original reconciliation", func() {
		retry := created(all, permissionBody, permissionReview.ReviewETag, 200)
		if retry.Team.ID != permissionReceipt.TeamID || !teamCreationReceiptFixtureEqual(readReceipt(permissionReceipt.CreationID), permissionReceipt) {
			t.Fatal("permission race replaced original captured intent")
		}
	})
	// Bound admitted owner set is complete and supports the actual 1000-row receipt.
	owners := make([]entity.User, 1000)
	ownerIDs := make([]string, 1000)
	stamp := time.Now().UTC().Truncate(time.Millisecond)
	for i := range owners {
		ownerIDs[i] = fmt.Sprintf("usr_tc_%022d", i)
		owners[i] = entity.User{ID: ownerIDs[i], Email: fmt.Sprintf("tc-max-%d@example.invalid", i), Name: "Retained owner", Role: entity.RoleMember, PasswordHash: "fixture-not-a-login-hash", CreatedAt: stamp, UpdatedAt: stamp}
	}
	if err := db.CreateInBatches(&owners, 100).Error; err != nil {
		t.Fatal(err)
	}
	maximal := input(ownerIDs)
	maximalResult := created(all, maximal, readContext(all).ReviewETag, 201)
	maxReceipt := readReceipt(maximalResult.Receipt.CreationID)
	var maxSnapshot service.TeamCreationSnapshot
	if err := json.Unmarshal([]byte(maxReceipt.SnapshotJSON), &maxSnapshot); err != nil || len(maxSnapshot.Owners) != 1000 || len(maxSnapshot.Owners) != len(maximalResult.Team.Members) || len(maxReceipt.SnapshotJSON) > 128*1024 || len(maxReceipt.SnapshotJSON) <= 65535 {
		t.Fatal("1000-owner complete portable snapshot", err, len(maxReceipt.SnapshotJSON))
	}
	seen := map[string]bool{}
	for _, owner := range maxSnapshot.Owners {
		if !slices.Contains(ownerIDs, owner.UserID) || seen[owner.UserID] || owner.UserCreatedAtMicro != stamp.UnixMicro() {
			t.Fatal("owner proof collapsed or incomplete")
		}
		seen[owner.UserID] = true
	}
	overflow := input(append(slices.Clone(ownerIDs), ordinary.id))
	unchanged("1001 owners", func() { expectStatus(t, send(all, "POST", createPath, overflow, readContext(all).ReviewETag), 400) })
	// Real native proof is enabled only by an explicit later existing model grant.
	var dispatches atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatches.Add(1)
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-CSRF-Token") != "" || r.Header.Get("Authorization") != "Bearer tc-native-upstream" {
			t.Error("Team Session leaked auth upstream")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, quotaWarningNativeBody(false, false))
	}))
	defer upstream.Close()
	cipher, err := store.Seal("crd_tc_native", "tc-native-upstream")
	if err != nil {
		t.Fatal(err)
	}
	modelID := "mdl_tc_native"
	warmupBearer := "rx_" + strings.Repeat("n", 43)
	rows := []any{&entity.Provider{ID: "prv_tc_native", Name: "Controlled creation provider"}, &entity.ProviderConnection{ID: "con_tc_native", ProviderID: "prv_tc_native", Name: "Native", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"}, &entity.ProviderCredential{ID: "crd_tc_native", ConnectionID: "con_tc_native", Name: "Ready", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"}, &entity.ProviderModel{ID: "pmd_tc_native", ConnectionID: "con_tc_native", UpstreamName: "tc-native"}, &entity.CredentialModelAccess{CredentialID: "crd_tc_native", ProviderModelID: "pmd_tc_native"}, &entity.Model{ID: modelID, Status: entity.ResourceActive}, &entity.ModelName{Name: "tc-native-model", ModelID: modelID, CurrentModelID: &modelID}, &entity.ModelProviderBinding{ID: "bnd_tc_native", ModelID: modelID, ProviderModelID: "pmd_tc_native", Weight: 100}, &entity.ReservationBound{ProviderModelID: "pmd_tc_native", Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 1, MaxOutputTokens: 1, ETag: "bnd_tc_initial", Evidence: "Controlled response", Reason: "Explicit native acceptance"}, &entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID}, &entity.APIKey{ID: "key_tc_warmup", UserID: admin.User.ID, Name: "Coverage warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(warmupBearer), Status: entity.KeyActive}, &entity.APIKeyModel{KeyID: "key_tc_warmup", ModelID: modelID}, &entity.ModelPrice{ID: "mpr_tc_native", ProviderModelID: "pmd_tc_native", UpdateSource: "api"}}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		if err := db.Create(&entity.PriceRate{ID: fmt.Sprintf("rat_tc_%d", i), ModelPriceID: "mpr_tc_native", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Amount: "1000000", Currency: "USD", Enabled: true}).Error; err != nil {
			t.Fatal(err)
		}
	}
	spool := filepath.Join(t.TempDir(), "team-creation.db")
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	native := func(who actor, teamID string, bearer string) *httptest.ResponseRecorder {
		t.Helper()
		bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		path := "/api/v1/teams/" + teamID + "/chat/completions"
		if bearer != "" {
			path = "/v1/chat/completions"
		}
		req := httptest.NewRequestWithContext(bounded, "POST", "http://routex.test"+path, strings.NewReader(`{"model":"tc-native-model","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":1}`))
		req.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		} else {
			req.Header.Set("Origin", "http://routex.test")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			req.Header.Set("X-CSRF-Token", who.csrf)
			req.AddCookie(who.cookie)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	flush := func() {
		t.Helper()
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
	}
	expectStatus(t, native(administrator, "", warmupBearer), 200)
	flush()
	nativeInput := input([]string{ordinary.id})
	nativeInput["initial_limits"] = map[string]any{"tokens_5h": nil, "tokens_7d": nil, "tokens_month": 2, "money_month": nil, "rpm": nil, "tpm": nil, "concurrency": nil, "reason": "Finite initial native cap"}
	nativeResult := created(administrator, nativeInput, readContext(administrator).ReviewETag, 201)
	if !nativeResult.RuntimeApplied || nativeResult.ApplicationStatus != "applied" {
		t.Fatal("current exact published creation not confirmed")
	}
	expectStatus(t, native(ordinary, nativeResult.Team.ID, ""), 404)
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, nativeResult.Team.ID, []string{modelID}); err != nil {
		t.Fatal(err)
	}
	flush()
	expectStatus(t, native(ordinary, nativeResult.Team.ID, ""), 200)
	flush()
	denied := native(ordinary, nativeResult.Team.ID, "")
	expectStatus(t, denied, 429)
	flush()
	if dispatches.Load() != 2 {
		t.Fatal("implicit model or hardstop dispatched")
	}
	var calls []entity.CallRecord
	if err := db.Where("team_id = ?", nativeResult.Team.ID).Order("request_id").Find(&calls).Error; err != nil || len(calls) != 3 {
		t.Fatal("native Team facts: missing model denial, completion, hardstop", err, len(calls))
	}
	nativeCount, denials := 0, 0
	for _, call := range calls {
		if call.TeamID != nativeResult.Team.ID || call.UserID != ordinary.id || call.KeyID != "" || call.ProjectID != "" {
			t.Fatal("Team Session identity changed")
		}
		var attempts []entity.CallAttempt
		if err := db.Where("request_id = ?", call.RequestID).Find(&attempts).Error; err != nil {
			t.Fatal(err)
		}
		if call.Status == "success" {
			nativeCount++
			if len(attempts) != 1 || attempts[0].CredentialID != "crd_tc_native" || attempts[0].SnapshotID == "" || attempts[0].NativeCompletionEvidence != "completed" {
				t.Fatal("native completion proof missing")
			}
		} else {
			denials++
			if len(attempts) != 0 {
				t.Fatal("denial invented attempt")
			}
		}
	}
	if nativeCount != 1 || denials != 2 {
		t.Fatal("exact native/denial oracle")
	}
	zeroInput := input([]string{ordinary.id})
	zeroInput["initial_limits"] = map[string]any{"tokens_5h": nil, "tokens_7d": nil, "tokens_month": 0, "money_month": nil, "rpm": nil, "tpm": nil, "concurrency": nil, "reason": "Finite initial zero is not unset"}
	zeroResult := created(administrator, zeroInput, readContext(administrator).ReviewETag, 201)
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, zeroResult.Team.ID, []string{modelID}); err != nil {
		t.Fatal(err)
	}
	flush()
	expectStatus(t, native(ordinary, zeroResult.Team.ID, ""), 429)
	flush()
	var zeroCalls []entity.CallRecord
	if err := db.Where("team_id = ?", zeroResult.Team.ID).Find(&zeroCalls).Error; err != nil || len(zeroCalls) != 1 || zeroCalls[0].Status == "success" {
		t.Fatal("finite zero denial facts", err, len(zeroCalls))
	}
	var zeroAttempts int64
	if err := db.Model(&entity.CallAttempt{}).Where("request_id = ?", zeroCalls[0].RequestID).Count(&zeroAttempts).Error; err != nil || zeroAttempts != 0 || dispatches.Load() != 2 {
		t.Fatal("finite zero dispatched native attempt", err)
	}
	unchanged("superseded after explicit model grant", func() {
		retry := created(administrator, nativeInput, readReceipt(nativeResult.Receipt.CreationID).ReviewETag, 200)
		if retry.RuntimeApplied || retry.ApplicationStatus != "superseded" || len(retry.Team.ModelIDs) != 1 {
			t.Fatal("receipt restored historical no-model state")
		}
	})
	uncertainBody := input([]string{ordinary.id})
	uncertainReview := readContext(administrator)
	publicationFailure.Store(true)
	uncertain := created(administrator, uncertainBody, uncertainReview.ReviewETag, 201)
	publicationFailure.Store(false)
	if uncertain.RuntimeApplied || uncertain.ApplicationStatus != "pending" {
		t.Fatal("failed publication reported applied")
	}
	flush()
	unchanged("publication exact retry", func() {
		retry := created(administrator, uncertainBody, uncertainReview.ReviewETag, 200)
		if !retry.RuntimeApplied || retry.ApplicationStatus != "applied" || !reflect.DeepEqual(retry.Receipt, uncertain.Receipt) {
			t.Fatal("current publication not reconciled")
		}
	})
	// Current metadata supersession is read back, never restored from the receipt.
	if _, err := svc.UpdateResource(ctx, admin.User.ID, service.TeamResource, copied.Team.ID, service.ResourceUpdate{Name: func() *string { v := "Current renamed Team"; return &v }()}); err != nil {
		t.Fatal(err)
	}
	unchanged("metadata supersession", func() {
		retry := created(creator, copiedInput, copiedReview.ReviewETag, 200)
		if retry.ApplicationStatus != "superseded" || retry.RuntimeApplied || retry.Team.Name != "Current renamed Team" {
			t.Fatal("historical receipt restored metadata")
		}
	})
	finalReceipt := readReceipt(copied.Receipt.CreationID)
	if !teamCreationReceiptFixtureEqual(finalReceipt, originalReceipt) {
		t.Fatal("receipt was rewritten")
	}
	beforeRestart := capture()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	svc = makeService()
	router = fox.New()
	New(svc).RegisterRoutes(router)
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	flush()
	unchanged("same Session restart retry", func() {
		retry := created(creator, copiedInput, copiedReview.ReviewETag, 200)
		if !reflect.DeepEqual(retry.Receipt, copied.Receipt) || retry.ApplicationStatus != "superseded" {
			t.Fatal("restart lost exact receipt/current status")
		}
	})
	if !slices.Equal(beforeRestart, capture()) || dispatches.Load() != 2 || !teamCreationReceiptFixtureEqual(readReceipt(copied.Receipt.CreationID), originalReceipt) {
		t.Fatal("restart replayed creation/native/audits")
	}
}

type teamCreationFixtureBarrier struct {
	entered, release chan struct{}
	once, closeOnce  sync.Once
}

func TestTeamCreationLimitsFixturePublicEnvelope(t *testing.T) {
	stamp := time.Date(2026, 4, 5, 6, 7, 8, 123000000, time.UTC)
	result := &service.TeamCreationResult{Team: &service.ResourceRecord{ID: "tea_fixture", Name: "Saved Team", Status: entity.ResourceActive, Members: []service.ResourcePerson{}, ModelIDs: []string{}}, Receipt: service.TeamCreationReceiptRecord{CreationID: "63000000-0000-4000-8000-000000000001", TeamID: "tea_fixture", CreatedAt: stamp}, Committed: true, ApplicationStatus: "pending", Created: true}
	raw, err := json.Marshal(teamCreationResponse(result))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) != 5 {
		t.Fatal("reviewed Team envelope shape", err)
	}
	for _, key := range []string{"team", "receipt", "committed", "runtime_applied", "application_status"} {
		if _, ok := fields[key]; !ok {
			t.Fatal("missing Team confirmation field", key)
		}
	}
	if _, ok := fields["project"]; ok {
		t.Fatal("Project response key reused for Team")
	}
	var decoded teamCreationLimitsFixtureResponse
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.Team == nil || decoded.Team.ID != decoded.Receipt.TeamID || !decoded.Committed || decoded.RuntimeApplied || decoded.ApplicationStatus != "pending" || !decoded.Receipt.CreatedAt.Equal(stamp) {
		t.Fatal("fixture cannot decode genuine saved-only envelope", err)
	}
	result.Team = nil
	raw, err = json.Marshal(teamCreationResponse(result))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &fields); err != nil || string(fields["team"]) != "null" {
		t.Fatal("unavailable Team must not fabricate historical private data", err)
	}
}

// Check actual GORM names used by controlled SQL fixture writes and snapshots.
// Offboarded is a derived API label, never a persisted boolean User column.
func TestTeamCreationLimitsFixtureSchemaMappings(t *testing.T) {
	cache := &sync.Map{}
	for _, sample := range []struct {
		model   any
		table   string
		columns []string
	}{
		{&entity.User{}, "users", []string{"id", "disabled", "offboarded_at", "approval_application_id", "created_at"}},
		{&entity.DefaultLimitRule{}, "default_limit_rules", []string{"kind", "tokens_5h"}},
		{&entity.PricingSetting{}, "pricing_settings", []string{"id", "ETag", "platform_currency"}},
		{&entity.RegistrationApprovalApplication{}, "registration_approval_applications", []string{"id", "state", "decided_at", "decision_actor_id", "decision_reason"}},
		{&entity.Team{}, "teams", []string{"id"}},
		{&entity.TeamMembership{}, "team_memberships", []string{"team_id", "user_id"}},
		{&entity.ResourceLimit{}, "resource_limits", []string{"scope_kind", "scope_id"}},
		{&entity.TeamModelGrant{}, "team_model_grants", []string{"team_id"}},
		{&entity.TeamRole{}, "team_roles", []string{"team_id"}},
		{&entity.APIKey{}, "api_keys", []string{"id"}},
		{&entity.ProjectKey{}, "project_api_keys", []string{"id"}},
		{&entity.AuditEvent{}, "audit_events", []string{"resource_id"}},
		{&entity.TeamCreationReceipt{}, "team_creation_receipts", []string{"creation_id"}},
		{&entity.UserRole{}, "user_roles", []string{"user_id"}},
		{&entity.CallRecord{}, "call_records", []string{"team_id", "request_id"}},
		{&entity.CallAttempt{}, "call_attempts", []string{"request_id"}},
	} {
		parsed, err := schema.Parse(sample.model, cache, schema.NamingStrategy{})
		if err != nil || parsed.Table != sample.table {
			t.Fatal("fixture table does not match entity", sample.table, err)
		}
		for _, name := range sample.columns {
			if parsed.LookUpField(name) == nil {
				t.Fatal("fixture field does not match entity", sample.table, name)
			}
		}
	}
	user, err := schema.Parse(&entity.User{}, cache, schema.NamingStrategy{})
	if err != nil || user.LookUpField("offboarded") != nil || user.LookUpField("offboarded_at").FieldType.Kind() != reflect.Pointer {
		t.Fatal("offboarding timestamp mapping changed", err)
	}
	// The released approval-ID constraint and current admission validator require
	// lower-case Crockford ULID digits, including a first digit between 0 and 7.
	canonicalApprovalID := regexp.MustCompile(`^raa_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
	for range 16 {
		generated, err := id.NewPrefixed("raa")
		if err != nil || !canonicalApprovalID.MatchString(generated) {
			t.Fatal("actual application generator violates canonical approval identity", err)
		}
	}
	for _, invalid := range []string{"raa_01ARZ3NDEKTSV4RRFFQ69G5FAV", "raa_81arz3ndektsv4rrffq69g5fav", "raa_01arz3ndektsv4rrffq69g5fai", "raa_01arz3ndektsv4rrffq69g5fav ", "RAA_01arz3ndektsv4rrffq69g5fav"} {
		if canonicalApprovalID.MatchString(invalid) {
			t.Fatal("noncanonical application identity accepted", invalid)
		}
	}
	pricingSchema, err := schema.Parse(&entity.PricingSetting{}, cache, schema.NamingStrategy{})
	if err != nil || pricingSchema.LookUpField("ETag").DBName != "e_tag" {
		t.Fatal("pricing model-field alias changed", err)
	}
}
