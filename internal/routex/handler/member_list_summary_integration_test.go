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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

type memberListQueryMarker struct{}

// Metadata cardinality is fixture setup. Only the three controlled native
// subject requests plus one warmup establish usage/finality; list reads create no inference work.
func testMemberListSummaryLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var mu sync.Mutex
	var queries []string
	var fail atomic.Bool
	observe := func(tx *gorm.DB) {
		if tx.Statement.Context.Value(memberListQueryMarker{}) != nil {
			mu.Lock()
			queries = append(queries, tx.Statement.SQL.String())
			mu.Unlock()
			if fail.Load() {
				_ = tx.AddError(errors.New("controlled member list read outage"))
			}
		}
	}
	const callback = "member_list_summary_reads"
	if err := db.Callback().Query().After("gorm:query").Register(callback, observe); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().After("gorm:row").Register(callback, observe); err != nil {
		t.Fatal(err)
	}
	store, err := secretstore.New(bytes.Repeat([]byte{147}, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		fail.Store(false)
		svc.StopRuntime()
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
		_ = db.Callback().Query().Remove(callback)
		_ = db.Callback().Row().Remove(callback)
	})
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"member-list-admin@example.invalid","password":"test-only-member-list-password","name":"Member list administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	reader, readerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "member-list-reader", []string{"members.read"})
	both, bothCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "member-list-team-reader", []string{"members.read", "teams.read_all"})
	_, writerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "member-list-writer", []string{"members.write"})
	create := func(rows ...any) {
		t.Helper()
		for _, row := range rows {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	users := make([]entity.User, 105)
	for i := range users {
		users[i] = entity.User{ID: fmt.Sprintf("ml_user_%03d", i), Email: fmt.Sprintf("ml-%03d@example.invalid", i), Name: fmt.Sprintf("Member list subject %%_! %03d", i), Role: entity.RoleMember, PasswordHash: "retained fixture hash", Disabled: i == 1}
	}
	create(&users)
	subject := users[0].ID
	query := context.WithValue(ctx, memberListQueryMarker{}, true)
	filter := service.MemberFilter{Query: "Member list subject %_!", Limit: 100}
	direct := func(actorID string, limit int) (*service.MemberListPage, []string) {
		t.Helper()
		mu.Lock()
		queries = nil
		mu.Unlock()
		f := filter
		f.Limit = limit
		page, err := svc.ListMemberSummaries(query, actorID, f)
		if err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		sql := append([]string{}, queries...)
		mu.Unlock()
		return page, sql
	}
	one, q1 := direct(reader.User.ID, 1)
	page, q100 := direct(reader.User.ID, 100)
	if len(one.Members) != 1 || len(page.Members) != 100 || len(q1) != 9 || len(q100) != 9 || one.NextCursor != subject || page.NextCursor != users[99].ID {
		t.Fatal("page or fixed SQL budget", len(q1), len(q100), one.NextCursor, page.NextCursor)
	}
	for _, sql := range q100 {
		if strings.Contains(sql, "team_memberships") || strings.Contains(sql, "FROM \"teams\"") || strings.Contains(sql, "FROM `teams`") {
			t.Fatal("reader queried Team metadata", sql)
		}
	}
	for _, row := range page.Members {
		if row.TotalPersonalKeys != "0" || row.Teams.Status != "not_authorized" || row.Teams.Items != nil || row.PersonalPolicyStored || row.Personal.PolicyETag != "0" || row.Personal.UsageStatus != "unavailable" || row.Personal.RuntimeApplied {
			t.Fatal(row)
		}
	}
	tail, err := svc.ListMemberSummaries(ctx, reader.User.ID, service.MemberFilter{Query: filter.Query, Limit: 100, Cursor: page.NextCursor})
	if err != nil || len(tail.Members) != 5 || tail.NextCursor != "" {
		t.Fatal(tail, err)
	}
	empty, err := svc.ListMemberSummaries(ctx, reader.User.ID, service.MemberFilter{Query: "absent exact list phrase"})
	if err != nil || len(empty.Members) != 0 || empty.Members == nil {
		t.Fatal(empty, err)
	}
	for _, actor := range []string{strings.ToUpper(reader.User.ID), reader.User.ID + " "} {
		if _, err := svc.ListMemberSummaries(ctx, actor, filter); err == nil {
			t.Fatal("actor alias borrowed authority", actor)
		}
	}
	get := func(cookie *http.Cookie) MemberListResponse {
		t.Helper()
		response := identityRequest(router, "GET", "/api/v1/admin/members?q=Member+list+subject+%25_%21&limit=100", "", cookie, "")
		expectStatus(t, response, 200)
		var result MemberListResponse
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(response.Header())
		}
		for _, private := range []string{"password_hash", "token_hash", "prefix", "key_id", "membership_id", "personal_grant_revision"} {
			if strings.Contains(response.Body.String(), private) {
				t.Fatal("list leaked private field", private)
			}
		}
		return result
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/members", "", writerCookie, ""), 403)
	response := get(readerCookie)
	if response.ActorUserID != reader.User.ID || len(response.Items) != 100 || response.Items[0].LastLoginAt != nil || response.Items[0].LastLoginStatus != "historical_unavailable" {
		t.Fatal(response)
	}
	for _, q := range []string{"limit=", "limit=101", "limit=1&limit=2", "user_id=" + subject, "cursor=bad%2Fid", "status=Active"} {
		expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/members?"+q, "", readerCookie, ""), 400)
	}
	// The role compatibility budget permits all100x100 supported assignments.
	roles := make([]entity.Role, 101)
	assignments := make([]entity.UserRole, 0, 10000)
	for i := range roles {
		roles[i] = entity.Role{ID: fmt.Sprintf("ml_role_%03d", i), Name: fmt.Sprintf("List role %03d", i), NameKey: secret.SHA256Hex(fmt.Sprintf("list-role-%d", i))}
	}
	create(&roles)
	for i := 0; i < 100; i++ {
		for j := 0; j < 100; j++ {
			assignments = append(assignments, entity.UserRole{UserID: users[i].ID, RoleID: roles[j].ID})
		}
	}
	if err := db.CreateInBatches(&assignments, 500).Error; err != nil {
		t.Fatal(err)
	}
	page, _ = direct(reader.User.ID, 100)
	for _, row := range page.Members {
		if len(row.RoleIDs) != 100 {
			t.Fatal("role_ids truncated", row.User.ID)
		}
	}
	extra := entity.UserRole{UserID: subject, RoleID: roles[100].ID}
	create(&extra)
	if _, err := svc.ListMemberSummaries(ctx, reader.User.ID, filter); err == nil {
		t.Fatal("10001 roles silently truncated")
	}
	historical, _ := direct(reader.User.ID, 1)
	if len(historical.Members[0].RoleIDs) != 101 {
		t.Fatal("historical >100 roles truncated")
	}
	if err := db.Delete(&extra).Error; err != nil {
		t.Fatal(err)
	}
	// Authoritative count includes all retained statuses, expiry and ancestors.
	expired := time.Now().UTC().Add(-time.Hour)
	token := "rx_" + strings.Repeat("m", 43)
	keys := make([]entity.APIKey, 6)
	for i, status := range []string{entity.KeyActive, entity.KeyPending, entity.KeyDisabled, entity.KeyRevoked, entity.KeyActive, entity.KeyRevoked} {
		keys[i] = entity.APIKey{ID: fmt.Sprintf("ml_key_%d", i), UserID: subject, Name: "Retained private", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(fmt.Sprintf("ml-retained-%d", i)), Status: status}
		if i == 4 {
			keys[i].ExpiresAt = &expired
		}
		if i == 5 {
			keys[i].ReplacesKeyID = &keys[3].ID
		}
	}
	keys[0].TokenHash = secret.SHA256Hex(token)
	create(&keys)
	project, err := svc.CreateResource(ctx, admin.User.ID, service.ProjectResource, "List count exclusion", "", []string{subject})
	if err != nil {
		t.Fatal(err)
	}
	create(&entity.ProjectKey{ID: "ml_project_key", ProjectID: project.ID, CreatorID: subject, Name: "Excluded", Prefix: "rx_project", TokenHash: secret.SHA256Hex("ml-project"), Status: entity.KeyRevoked, DeliveryMode: "manual"})
	zero := int64(0)
	large := limits.MaxInteger
	money := "100.000000000000000001"
	create(&entity.ResourceLimit{ScopeKind: "user", ScopeID: subject, ETag: "ml_subject_policy", TokensMonth: &large, MoneyMonth: &money, Currency: "USD"}, &entity.ResourceLimit{ScopeKind: "user", ScopeID: users[2].ID, ETag: "0", TokensMonth: &zero})
	page, _ = direct(reader.User.ID, 100)
	if page.Members[0].TotalPersonalKeys != "6" || page.Members[0].Personal.TokensMonth == nil || *page.Members[0].Personal.TokensMonth != "9007199254740991" || !page.Members[2].PersonalPolicyStored || page.Members[2].Personal.PolicyETag != "0" || *page.Members[2].Personal.TokensMonth != "0" {
		t.Fatal("count, exact policy or stored row existence lost", page.Members[:3])
	}
	// Policy ceilings are safe integers; int64 journal counters have their own
	// exact string contract. A corrupt stored ceiling must fail the complete
	// list rather than be rounded or treated as a supported policy.
	policyQuery := func() *gorm.DB {
		return db.Session(&gorm.Session{NewDB: true}).Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "user", subject)
	}
	var reviewedPolicy entity.ResourceLimit
	if err := policyQuery().First(&reviewedPolicy).Error; err != nil {
		t.Fatal(err)
	}
	if err := policyQuery().UpdateColumns(map[string]any{"TokensMonth": limits.MaxInteger + 1}).Error; err != nil {
		t.Fatal(err)
	}
	if invalid, err := svc.ListMemberSummaries(ctx, reader.User.ID, filter); err == nil || invalid != nil {
		t.Fatal("unsupported policy ceiling produced a complete list", invalid, err)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/members?q=Member+list+subject+%25_%21&limit=100", "", readerCookie, ""), 500)
	if err := policyQuery().UpdateColumns(map[string]any{"TokensMonth": large}).Error; err != nil {
		t.Fatal(err)
	}
	var restoredPolicy entity.ResourceLimit
	if err := policyQuery().First(&restoredPolicy).Error; err != nil || !reflect.DeepEqual(reviewedPolicy, restoredPolicy) {
		t.Fatal("ceiling rejection/read mutated the complete retained policy", err)
	}
	page, _ = direct(reader.User.ID, 100)
	if page.Members[0].Personal.TokensMonth == nil || *page.Members[0].Personal.TokensMonth != "9007199254740991" || page.Members[0].Personal.MoneyMonth == nil || *page.Members[0].Personal.MoneyMonth != money {
		t.Fatal("restored maximum policy lost exact token or money values")
	}
	teams := make([]entity.Team, 11)
	memberships := make([]entity.TeamMembership, 0, 1001)
	for i := range teams {
		status := entity.ResourceActive
		if i == 1 {
			status = entity.ResourceArchived
		}
		teams[i] = entity.Team{ID: fmt.Sprintf("ml_team_%02d", i), Name: fmt.Sprintf("Retained list Team %02d", i), Status: status}
	}
	create(&teams)
	for i := 0; i < 100; i++ {
		for j := 0; j < 10; j++ {
			status := entity.ResourceActive
			if j == 1 {
				status = entity.ResourceDisabled
			}
			memberships = append(memberships, entity.TeamMembership{ID: fmt.Sprintf("ml_tm_%03d_%02d", i, j), UserID: users[i].ID, TeamID: teams[j].ID, Role: entity.TeamMember, Status: status})
		}
	}
	if err := db.CreateInBatches(&memberships, 500).Error; err != nil {
		t.Fatal(err)
	}
	page, qTeams := direct(both.User.ID, 100)
	if len(qTeams) != 11 || page.Members[0].Teams.Status != "available" || len(page.Members[0].Teams.Items) != 10 || page.Members[0].Teams.Items[1].Status != entity.ResourceArchived {
		t.Fatal("bounded retained Team hydration", len(qTeams), page.Members[0].Teams)
	}
	overflow := entity.TeamMembership{ID: "ml_tm_overflow", UserID: subject, TeamID: teams[10].ID, Role: entity.TeamMember, Status: entity.ResourceActive}
	create(&overflow)
	page, qTeams = direct(both.User.ID, 100)
	if len(qTeams) != 10 {
		t.Fatal("overflow hydrated a partial Team directory", len(qTeams))
	}
	for _, row := range page.Members {
		if row.Teams.Status != "overflow" || row.Teams.Items != nil {
			t.Fatal(row.Teams)
		}
	}
	_ = get(bothCookie)
	if err := db.Delete(&overflow).Error; err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	if _, err := svc.ListMemberSummaries(query, reader.User.ID, filter); err == nil {
		t.Fatal("SQL outage became known empty")
	}
	fail.Store(false)
	// Genuine known/held/unknown native facts prove one journal batch remains
	// separate from SQL caps, retained counts and optional Team metadata.
	var mode, posts atomic.Int32
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer member-list-upstream" {
			t.Error("wrong controlled native route")
			w.WriteHeader(400)
			return
		}
		if mode.Load() == 1 {
			entered <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		body := `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"List usage"},"finish_reason":"stop"}]`
		if mode.Load() == 2 {
			_, _ = io.WriteString(w, body+`}`)
			return
		}
		_, _ = io.WriteString(w, body+`,"usage":{"prompt_tokens":4,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`)
	}))
	t.Cleanup(upstream.Close)
	t.Cleanup(unblock)
	cipher, err := store.Seal("ml_credential", "member-list-upstream")
	if err != nil {
		t.Fatal(err)
	}
	modelID := "ml_model"
	create(&entity.Provider{ID: "ml_provider", Name: "Controlled list Provider"}, &entity.ProviderConnection{ID: "ml_connection", ProviderID: "ml_provider", Name: "Chat", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"}, &entity.ProviderCredential{ID: "ml_credential", ConnectionID: "ml_connection", Name: "Ready", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"}, &entity.ProviderModel{ID: "ml_provider_model", ConnectionID: "ml_connection", UpstreamName: "native"}, &entity.CredentialModelAccess{CredentialID: "ml_credential", ProviderModelID: "ml_provider_model"}, &entity.Model{ID: modelID, Status: entity.ResourceActive}, &entity.ModelName{Name: "member-list-native", ModelID: modelID, CurrentModelID: &modelID}, &entity.ModelProviderBinding{ID: "ml_binding", ModelID: modelID, ProviderModelID: "ml_provider_model", Weight: 100}, &entity.ModelPrice{ID: "ml_price", ProviderModelID: "ml_provider_model", UpdateSource: "api"}, &entity.ReservationBound{ProviderModelID: "ml_provider_model", Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 4, MaxOutputTokens: 1, ETag: "ml_bound", Evidence: "Controlled four input one output", Reason: "List journal acceptance"})
	for i, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		amount := "1000000"
		if metric == pricing.Output {
			amount = "1000000.000000000001"
		}
		create(&entity.PriceRate{ID: fmt.Sprintf("ml_rate_%d", i), ModelPriceID: "ml_price", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: amount, Enabled: true})
	}
	warmToken := "rx_" + strings.Repeat("w", 43)
	create(&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID}, &entity.APIKey{ID: "ml_key_warm", UserID: admin.User.ID, Name: "Coverage warmup", Prefix: "rx_warm", TokenHash: secret.SHA256Hex(warmToken), Status: entity.KeyActive}, &entity.APIKeyModel{KeyID: "ml_key_warm", ModelID: modelID})
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	spool := filepath.Join(t.TempDir(), "member-list.db")
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	native := func(bearer string) *httptest.ResponseRecorder {
		bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		request := httptest.NewRequestWithContext(bounded, "POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"member-list-native","messages":[{"role":"user","content":"Controlled"}],"max_completion_tokens":1}`))
		request.Header.Set("Authorization", "Bearer "+bearer)
		request.Header.Set("Content-Type", "application/json")
		result := httptest.NewRecorder()
		router.ServeHTTP(result, request)
		return result
	}
	requestIDs := []string{}
	complete := func(response *httptest.ResponseRecorder) {
		t.Helper()
		expectStatus(t, response, 200)
		requestID := response.Header().Get("X-Request-ID")
		if requestID == "" {
			t.Fatal("native response lost its immutable request identity")
		}
		for _, original := range requestIDs {
			if original == requestID {
				t.Fatal("native requests reused an immutable identity")
			}
		}
		requestIDs = append(requestIDs, requestID)
	}
	// Activate coverage with an unrestricted existing account before creating
	// the finite native subject; retained metadata subjects remain independent.
	complete(native(warmToken))
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	nativeMember, err := svc.CreateMember(ctx, admin.User.ID, "member-list-native@example.invalid", "test-only-list-native-password", "Member list native subject", entity.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	nativeSubject := nativeMember.User.ID
	nativeToken := "rx_" + strings.Repeat("n", 43)
	create(&entity.UserModelGrant{UserID: nativeSubject, ModelID: modelID}, &entity.APIKey{ID: "ml_key_native", UserID: nativeSubject, Name: "Native subject", Prefix: "rx_native", TokenHash: secret.SHA256Hex(nativeToken), Status: entity.KeyActive}, &entity.APIKeyModel{KeyID: "ml_key_native", ModelID: modelID})
	policy := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "user", nativeSubject).UpdateColumns(map[string]any{"ETag": "ml_native_policy", "TokensMonth": large, "MoneyMonth": money, "Currency": "USD"})
	if policy.Error != nil || policy.RowsAffected != 1 {
		t.Fatal("native policy setup", policy.RowsAffected, policy.Error)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	complete(native(nativeToken))
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	row := func() service.MemberListSummary {
		t.Helper()
		p, err := svc.ListMemberSummaries(ctx, reader.User.ID, service.MemberFilter{Query: "Member list native subject", Limit: 1})
		if err != nil || len(p.Members) != 1 || p.Members[0].User.ID != nativeSubject {
			t.Fatal("native subject list", p, err)
		}
		return p.Members[0]
	}
	known := row()
	for deadline := time.Now().Add(3 * time.Second); !known.Personal.RuntimeApplied && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
		known = row()
	}
	if known.Personal.Usage == nil || known.Personal.Usage.TokensUsed != "5" || known.Personal.Usage.MoneyUsed["USD"] != "5.000000000000000001" || !known.Personal.RuntimeApplied {
		t.Fatal("known usage missing", known.Personal)
	}
	mode.Store(1)
	completed := make(chan *httptest.ResponseRecorder, 1)
	go func() { completed <- native(nativeToken) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("controlled hold not dispatched")
	}
	held := row()
	if held.Personal.Usage == nil || held.Personal.Usage.TokensHeld != "0" || held.Personal.ActiveReservations == nil || held.Personal.ActiveReservations.TokensHeld != "5" || held.Personal.ActiveReservations.MoneyHeld["USD"] != "5.000000000000000003" {
		t.Fatal("monthly/live reservation merged", held.Personal)
	}
	unblock()
	complete(<-completed)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	mode.Store(2)
	complete(native(nativeToken))
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	// Completed missing usage retains its finite admission bounds in monthly
	// holds. Unknown counters count missing bounds, not missing actual usage;
	// immutable native facts below independently prove unknown final usage.
	unknown := row()
	if unknown.Personal.Usage == nil || unknown.Personal.ActiveReservations == nil || unknown.Personal.Usage.TokensUsed != "10" || unknown.Personal.Usage.TokensHeld != "5" || unknown.Personal.Usage.TokensUnknown != "0" || unknown.Personal.Usage.MoneyUsed["USD"] != "10.000000000000000002" || unknown.Personal.Usage.MoneyHeld["USD"] != "5.000000000000000003" || unknown.Personal.Usage.MoneyUnknown != "0" || unknown.Personal.ActiveReservations.TokensHeld != "0" || posts.Load() != 4 {
		t.Fatal("unknown usage became zero or list dispatched", unknown.Personal, posts.Load())
	}
	var calls []entity.CallRecord
	if err := db.Where("user_id = ?", nativeSubject).Order("request_id").Find(&calls).Error; err != nil || len(calls) != 3 {
		t.Fatal(len(calls), err)
	}
	var allNative int64
	if err := db.Model(&entity.CallRecord{}).Count(&allNative).Error; err != nil || allNative != 4 {
		t.Fatal("warmup plus three subject facts", allNative, err)
	}
	for _, call := range calls {
		if call.UserID != nativeSubject || call.KeyID != "ml_key_native" || call.ProjectID != "" || call.TeamID != "" {
			t.Fatal("list native attribution changed", call.RequestID)
		}
	}
	// HTTP success and authoritative usage are independent from parser-owned
	// completion. Capture all four exact immutable calls and attempts, including
	// the unlimited coverage warmup and the final completed unknown-usage call.
	if len(requestIDs) != 4 {
		t.Fatal("unexpected controlled native request count", len(requestIDs))
	}
	var nativeCalls []entity.CallRecord
	if err := db.Where("request_id IN ?", requestIDs).Order("request_id").Limit(5).Find(&nativeCalls).Error; err != nil || len(nativeCalls) != 4 {
		t.Fatal("four immutable native calls missing", len(nativeCalls), err)
	}
	byRequest := map[string]entity.CallRecord{}
	for _, call := range nativeCalls {
		byRequest[call.RequestID] = call
	}
	for index, requestID := range requestIDs {
		call, found := byRequest[requestID]
		wantUser, wantKey := nativeSubject, "ml_key_native"
		if index == 0 {
			wantUser, wantKey = admin.User.ID, "ml_key_warm"
		}
		if !found || call.Status != "success" || call.UserID != wantUser || call.KeyID != wantKey || call.ProjectID != "" || call.TeamID != "" || call.TeamMembershipID != "" || call.ModelID != modelID || call.ProviderID != "ml_provider" || call.ProviderModelID != "ml_provider_model" || call.ConnectionID != "ml_connection" || call.Protocol != entity.ProtocolOpenAIChat || call.SnapshotID == "" {
			t.Fatal("native call lost its original successful scope or route", index, requestID)
		}
		if index == 3 {
			if call.InputTokens != nil || call.OutputTokens != nil {
				t.Fatal("completed unknown native usage became known", requestID)
			}
		} else if call.InputTokens == nil || *call.InputTokens != 4 || call.OutputTokens == nil || *call.OutputTokens != 1 {
			t.Fatal("known native usage changed", requestID)
		}
	}
	var nativeAttempts []entity.CallAttempt
	if err := db.Where("request_id IN ?", requestIDs).Order("id").Limit(5).Find(&nativeAttempts).Error; err != nil || len(nativeAttempts) != 4 {
		t.Fatal("four immutable native attempts missing", len(nativeAttempts), err)
	}
	seenAttempts := map[string]bool{}
	for _, attempt := range nativeAttempts {
		call, found := byRequest[attempt.RequestID]
		if !found || seenAttempts[attempt.RequestID] || attempt.ID == "" || attempt.Status != "success" || attempt.AttemptNumber != 1 || attempt.CredentialID != "ml_credential" || attempt.SnapshotID != call.SnapshotID || attempt.ProviderID != call.ProviderID || attempt.ProviderModelID != call.ProviderModelID || attempt.ConnectionID != call.ConnectionID || attempt.NativeCompletionEvidence != "completed" || attempt.FinalUsageKnown != (attempt.RequestID != requestIDs[3]) {
			t.Fatal("native completion, usage or original attempt attribution changed", attempt.RequestID)
		}
		seenAttempts[attempt.RequestID] = true
	}
	assertNativeRetained := func() {
		t.Helper()
		var currentCalls []entity.CallRecord
		var currentAttempts []entity.CallAttempt
		if err := db.Where("request_id IN ?", requestIDs).Order("request_id").Limit(5).Find(&currentCalls).Error; err != nil || !reflect.DeepEqual(nativeCalls, currentCalls) {
			t.Fatal("list or lifecycle changed immutable native call facts", err)
		}
		if err := db.Where("request_id IN ?", requestIDs).Order("id").Limit(5).Find(&currentAttempts).Error; err != nil || !reflect.DeepEqual(nativeAttempts, currentAttempts) || posts.Load() != 4 {
			t.Fatal("list or lifecycle changed native attempts or replayed inference", err, posts.Load())
		}
	}
	// A case-folded retained Key owner either fails the released FK or remains
	// excluded by the exact count join. Neither driver may borrow that identity.
	aliasKey := entity.APIKey{ID: "ml_key_owner_alias", UserID: strings.ToUpper(subject), Name: "Alias fixture", Prefix: "rx_masked", TokenHash: secret.SHA256Hex("ml-alias-owner"), Status: entity.KeyRevoked}
	if err := db.Create(&aliasKey).Error; err == nil {
		aliasCount, _ := direct(reader.User.ID, 1)
		if aliasCount.Members[0].TotalPersonalKeys != "6" {
			t.Fatal("collated owner entered Personal count")
		}
		if err := db.Delete(&aliasKey).Error; err != nil {
			t.Fatal(err)
		}
	} else if !errors.Is(err, gorm.ErrForeignKeyViolated) {
		t.Fatal("unexpected alias fixture rejection", err)
	}
	aliasMembership := entity.TeamMembership{ID: "ml_tm_owner_alias", TeamID: teams[10].ID, UserID: strings.ToUpper(subject), Role: entity.TeamMember, Status: entity.ResourceActive}
	if err := db.Create(&aliasMembership).Error; err == nil {
		if _, err := svc.ListMemberSummaries(ctx, both.User.ID, service.MemberFilter{Query: filter.Query, Limit: 1}); err == nil {
			t.Fatal("collated relationship returned partial Team names")
		}
		if err := db.Delete(&aliasMembership).Error; err != nil {
			t.Fatal(err)
		}
	} else if !errors.Is(err, gorm.ErrForeignKeyViolated) {
		t.Fatal("unexpected alias relationship rejection", err)
	}

	// A read and journal outage do not change immutable calls or stored policies.
	_ = get(readerCookie)
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	after := row()
	if after.TotalPersonalKeys != "1" || after.Personal.Usage != nil || after.Personal.ActiveReservations != nil || after.Personal.RuntimeApplied {
		t.Fatal("closed journal manufactured zero", after)
	}
	var retained []entity.CallRecord
	if err := db.Where("user_id = ?", nativeSubject).Order("request_id").Find(&retained).Error; err != nil || !reflect.DeepEqual(calls, retained) || posts.Load() != 4 {
		t.Fatal("list rewrote or replayed immutable facts", err)
	}
	assertNativeRetained()
	permission := entity.RolePermission{RoleID: "rol_admin", Permission: "members.read"}
	if err := db.Delete(&permission).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/members", "", adminCookie, ""), 403)
	if err := db.Create(&permission).Error; err != nil {
		t.Fatal(err)
	}
	assertNativeRetained()
	// Same journal and persisted Session reopen without any inference replay.
	svc.StopRuntime()
	restarted, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		restarted.StopRuntime()
		if err := restarted.StopCallRecorder(); err != nil {
			t.Error(err)
		}
	})
	if err := restarted.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if err := restarted.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	restartRouter := fox.New()
	New(restarted).RegisterRoutes(restartRouter)
	readBack := identityRequest(restartRouter, "GET", "/api/v1/admin/members?q=Member+list+native+subject&limit=1", "", readerCookie, "")
	expectStatus(t, readBack, 200)
	var restored MemberListResponse
	if err := json.Unmarshal(readBack.Body.Bytes(), &restored); err != nil {
		t.Fatal(err)
	}
	if len(restored.Items) != 1 || restored.Items[0].ID != nativeSubject || restored.Items[0].Personal.Usage == nil || restored.Items[0].Personal.Usage.TokensUsed != "10" || restored.Items[0].Personal.Usage.TokensHeld != "5" || restored.Items[0].Personal.Usage.TokensUnknown != "0" || restored.Items[0].Personal.Usage.MoneyUsed["USD"] != "10.000000000000000002" || restored.Items[0].Personal.Usage.MoneyHeld["USD"] != "5.000000000000000003" || restored.Items[0].Personal.Usage.MoneyUnknown != "0" || restored.Items[0].Personal.ActiveReservations == nil || restored.Items[0].Personal.ActiveReservations.TokensHeld != "0" || restored.Items[0].TotalPersonalKeys != "1" || posts.Load() != 4 {
		t.Fatal("restart changed exact retained facts", restored, posts.Load())
	}
	if err := restarted.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	var afterRestart []entity.CallRecord
	if err := db.Where("user_id = ?", nativeSubject).Order("request_id").Find(&afterRestart).Error; err != nil || !reflect.DeepEqual(calls, afterRestart) || posts.Load() != 4 {
		t.Fatal("journal restart rewrote/replayed native facts", err)
	}
	assertNativeRetained()

}

func TestMemberListNativeFinalityFixtures(t *testing.T) {
	body := `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"List usage"},"finish_reason":"stop"}]`
	for _, known := range []bool{true, false} {
		raw := body + `}`
		if known {
			raw = body + `,"usage":{"prompt_tokens":4,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`
		}
		observation := parseGatewayUsage([]byte(raw))
		if observation.NativeCompletionEvidence != "completed" || observation.Complete != known {
			t.Fatal("controlled native completion and usage were conflated", known, observation)
		}
		if known {
			if observation.Input == nil || *observation.Input != 4 || observation.Output == nil || *observation.Output != 1 {
				t.Fatal("controlled native usage changed", observation)
			}
		} else if observation.Input != nil || observation.Output != nil {
			t.Fatal("completed unknown native usage became known", observation)
		}
	}
}
