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
	"net/url"
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

type memberOverviewQueryMarker struct{}

// This lifecycle runs independently against each supported governance driver.
// All usage comes from native calls and the durable journal, not fabricated
// observation rows or a sum of aggregate and member accounts.
func testMemberOverviewAccountsLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var queryMu sync.Mutex
	var queries []string
	var failPublication atomic.Bool
	callback := "test_member_overview_accounts_reads"
	observe := func(tx *gorm.DB) {
		if failPublication.Load() && tx.Statement.Table == "models" {
			_ = tx.AddError(errors.New("controlled Overview publication outage"))
		}
		if tx.Statement.Context.Value(memberOverviewQueryMarker{}) != nil {
			queryMu.Lock()
			queries = append(queries, tx.Statement.SQL.String())
			queryMu.Unlock()
		}
	}
	if err := db.Callback().Query().After("gorm:query").Register(callback, observe); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().After("gorm:row").Register(callback, observe); err != nil {
		_ = db.Callback().Query().Remove(callback)
		t.Fatal(err)
	}
	var instances []*service.Service
	var blocked, unknown atomic.Bool
	var dispatches atomic.Int32
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	// Callback mutation happens only after every runtime/recorder is stopped.
	t.Cleanup(func() {
		unblock()
		failPublication.Store(false)
		for _, instance := range instances {
			instance.StopRuntime()
			if err := instance.StopCallRecorder(); err != nil {
				t.Error(err)
			}
		}
		_ = db.Callback().Row().Remove(callback)
		_ = db.Callback().Query().Remove(callback)
	})
	store, err := secretstore.New(bytes.Repeat([]byte{129}, 32))
	if err != nil {
		t.Fatal(err)
	}
	newService := func() *service.Service {
		t.Helper()
		value, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, value)
		return value
	}
	svc := newService()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"overview-admin@example.invalid","password":"test-only-overview-password","name":"Overview administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	actor, actorCookie, actorCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "overview-actor", nil)
	peer, peerCookie, peerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "overview-peer", nil)
	operator, operatorCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "overview-operator", []string{"teams.read_all", "teams.tokens.write", "teams.money.write", "teams.rates.write"})
	get := func(cookie *http.Cookie, query string) service.MemberOverviewAccountsPage {
		t.Helper()
		response := identityRequest(router, "GET", "/api/v1/overview/accounts"+query, "", cookie, "")
		expectStatus(t, response, 200)
		var page service.MemberOverviewAccountsPage
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if page.ObservedAt.IsZero() || page.Personal.AccountID == "" || page.Teams == nil {
			t.Fatal("missing explicit self account or observation", response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("self account response is cacheable", response.Header())
		}
		for _, privateField := range []string{`"model_ids"`, `"members"`, `"key_count"`, `"remaining"`, `"effective"`} {
			if strings.Contains(response.Body.String(), privateField) {
				t.Fatal("Overview invented or exposed unrelated facts", privateField, response.Body.String())
			}
		}
		return page
	}
	find := func(page service.MemberOverviewAccountsPage, id string) service.MemberOverviewTeamAccount {
		t.Helper()
		for _, item := range page.Teams {
			if item.ID == id {
				return item
			}
		}
		t.Fatal("missing current Team account", id, page)
		return service.MemberOverviewTeamAccount{}
	}
	absent := func(page service.MemberOverviewAccountsPage, id string) {
		t.Helper()
		for _, item := range page.Teams {
			if item.ID == id {
				t.Fatal("non-current Team leaked into self Overview", item)
			}
		}
	}
	for _, item := range []struct {
		cookie *http.Cookie
		id     string
	}{{adminCookie, admin.User.ID}, {actorCookie, actor.User.ID}, {operatorCookie, operator.User.ID}} {
		page := get(item.cookie, "")
		if page.ActorUserID != item.id || len(page.Teams) != 0 || page.Personal.UsageStatus != "unavailable" || page.Personal.Usage != nil || page.Personal.ActiveReservations != nil || page.Personal.RuntimeApplied {
			t.Fatal("cold Overview manufactured usage or borrowed authority", page)
		}
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/overview/accounts", "", nil, ""), 401)
	for _, query := range []string{"?user_id=" + peer.User.ID, "?actor_user_id=" + peer.User.ID, "?team_id=tem_other", "?q=Team", "?limit=0", "?limit=-1", "?limit=51", "?limit=1&limit=2", "?cursor=bad", "?cursor=" + strings.Repeat("x", 1000), "?cursor=a&cursor=b"} {
		expectStatus(t, identityRequest(router, "GET", "/api/v1/overview/accounts"+query, "", actorCookie, ""), 400)
	}
	for _, alias := range []string{strings.ToUpper(actor.User.ID), actor.User.ID + " "} {
		if _, err := svc.MemberOverviewAccounts(ctx, alias, service.MemberOverviewAccountsFilter{}); err == nil {
			t.Fatal("collation actor alias obtained self accounts", alias)
		}
	}
	create := func(rows ...any) {
		t.Helper()
		for _, row := range rows {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatches.Add(1)
		if blocked.Load() {
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if unknown.Load() {
			_, _ = io.WriteString(w, `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Done"},"finish_reason":"stop"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`)
	}))
	t.Cleanup(upstream.Close)
	// Unblock precedes server cleanup even when a later assertion fails.
	t.Cleanup(unblock)
	modelID := "mdl_overview"
	bearer := "rx_" + strings.Repeat("o", 43)
	cipher, err := store.Seal("crd_overview", "test-only-overview-upstream")
	if err != nil {
		t.Fatal(err)
	}
	create(
		&entity.Provider{ID: "prv_overview", Name: "Overview controlled provider"},
		&entity.ProviderConnection{ID: "con_overview", ProviderID: "prv_overview", Name: "Chat", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_overview", ConnectionID: "con_overview", Name: "Ready", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_overview", ConnectionID: "con_overview", UpstreamName: "native"},
		&entity.CredentialModelAccess{CredentialID: "crd_overview", ProviderModelID: "pmd_overview"},
		&entity.Model{ID: modelID, Status: entity.ResourceActive},
		&entity.ModelName{Name: "overview-native", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_overview", ModelID: modelID, ProviderModelID: "pmd_overview", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_overview", UserID: admin.User.ID, Name: "Journal warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_overview", ModelID: modelID},
		&entity.ModelPrice{ID: "price_overview", ProviderModelID: "pmd_overview", UpdateSource: "api"},
		&entity.ReservationBound{ProviderModelID: "pmd_overview", Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 4, MaxOutputTokens: 1, ETag: "overview_bound", Evidence: "Controlled native usage", Reason: "Overview acceptance"},
	)
	for index, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		create(&entity.PriceRate{ID: fmt.Sprintf("rate_overview_%d", index), ModelPriceID: "price_overview", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "1000000", Enabled: true})
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	spool := filepath.Join(t.TempDir(), "member-overview.db")
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	if page := get(actorCookie, ""); page.Personal.UsageStatus != "inactive" || page.Personal.Usage != nil || page.Personal.ActiveReservations != nil {
		t.Fatal("inactive journal fabricated a zero monthly window", page.Personal)
	}
	body := `{"model":"overview-native","messages":[{"role":"user","content":"Hello"}],"max_completion_tokens":1}`
	warmup := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	warmup.Header.Set("Content-Type", "application/json")
	warmup.Header.Set("Authorization", "Bearer "+bearer)
	warm := httptest.NewRecorder()
	router.ServeHTTP(warm, warmup)
	expectStatus(t, warm, 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	keyOnly := httptest.NewRequest("GET", "/api/v1/overview/accounts", nil)
	keyOnly.Header.Set("Authorization", "Bearer "+bearer)
	keyResponse := httptest.NewRecorder()
	router.ServeHTTP(keyResponse, keyOnly)
	expectStatus(t, keyResponse, 401)
	teamID := "tem_overview"
	members := []service.TeamMemberInput{{UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: actor.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}, {UserID: peer.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}}
	create(&entity.Team{ID: teamID, Name: "Current Team", Status: entity.ResourceActive}, &entity.TeamModelGrant{TeamID: teamID, ModelID: modelID})
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, members); err != nil {
		t.Fatal(err)
	}
	refresh := func() {
		t.Helper()
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
	}
	write := func(userID, patch string) {
		t.Helper()
		before, err := svc.GetTeamResourceLimit(ctx, admin.User.ID, teamID, userID)
		if err != nil {
			t.Fatal(err)
		}
		var input service.TeamLimitInput
		if err := json.Unmarshal([]byte(patch), &input); err != nil {
			t.Fatal(err)
		}
		if after, err := svc.SetTeamResourceLimit(ctx, admin.User.ID, teamID, userID, before.ETag, input); err != nil || !after.Enforced {
			t.Fatal("Team policy not applied", after, err)
		}
	}
	zero := int64(0)
	personalTarget := service.LimitTarget{Kind: "user", ID: actor.User.ID}
	personal, err := svc.GetResourceLimit(ctx, admin.User.ID, personalTarget)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetResourceLimit(ctx, admin.User.ID, personalTarget, personal.ETag, service.LimitInput{Policy: limits.Policy{TokensMonth: &zero, RPM: &zero}, Reason: "Personal isolation"}); err != nil {
		t.Fatal(err)
	}
	write("", `{"tokens_month":100,"money_month":"100","currency":"USD","reason":"Aggregate ceiling"}`)
	write(actor.User.ID, `{"tokens_month":60,"money_month":"20","currency":"USD","reason":"Separate stable member ceiling"}`)
	native := func(cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		request := httptest.NewRequestWithContext(requestCtx, "POST", "http://routex.test/api/v1/teams/"+teamID+"/chat/completions", strings.NewReader(body))
		request.AddCookie(cookie)
		request.Header.Set("X-CSRF-Token", csrf)
		request.Header.Set("Origin", "http://routex.test")
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	assertUsage := func(account service.MemberOverviewMonthlyAccount, used, held, money, moneyHeld string) {
		t.Helper()
		usage := account.Usage
		if account.UsageStatus != "active" || usage == nil || !usage.Covered || usage.TokensUsed != used || usage.TokensHeld != held || usage.TokensUnknown != "0" || usage.MoneyUnknown != "0" || usage.MoneyUsed["USD"] != money || usage.MoneyHeld["USD"] != moneyHeld {
			t.Fatalf("unexpected authoritative usage for %s: %+v", account.AccountID, usage)
		}
	}
	assertReservations := func(account service.MemberOverviewMonthlyAccount, tokens, money string) {
		t.Helper()
		active := account.ActiveReservations
		if active == nil || active.TokensHeld != tokens || active.MoneyHeld["USD"] != money || money == "" && len(active.MoneyHeld) != 0 || money != "" && len(active.MoneyHeld) != 1 {
			t.Fatalf("unexpected separate live reservation for %s: %+v", account.AccountID, active)
		}
	}
	// Empty monetary maps express no recorded charge; do not require invented
	// currency-zero buckets until actual native activity exists.
	initial := find(get(actorCookie, ""), teamID)
	if initial.Aggregate.AccountID == initial.Member.AccountID || initial.Member.AccountID == get(actorCookie, "").Personal.AccountID || initial.Aggregate.TokensMonth == nil || *initial.Aggregate.TokensMonth != "100" || initial.Member.TokensMonth == nil || *initial.Member.TokensMonth != "60" || initial.Aggregate.Currency == nil || *initial.Aggregate.Currency != "USD" || initial.Member.MoneyMonth == nil || *initial.Member.MoneyMonth != "20" || !initial.Aggregate.RuntimeApplied || !initial.Member.RuntimeApplied {
		t.Fatal("aggregate/member policy identities were flattened", initial)
	}
	peerMembershipID := find(get(peerCookie, ""), teamID).MembershipID
	absent(get(operatorCookie, ""), teamID)
	write(actor.User.ID, `{"tokens_month":0,"money_month":"0","currency":"USD","reason":"Explicit stored zeros"}`)
	zeroPolicy := find(get(actorCookie, ""), teamID)
	if zeroPolicy.Member.TokensMonth == nil || *zeroPolicy.Member.TokensMonth != "0" || zeroPolicy.Member.MoneyMonth == nil || *zeroPolicy.Member.MoneyMonth != "0" || zeroPolicy.Member.Currency == nil || *zeroPolicy.Member.Currency != "USD" {
		t.Fatal("finite zero was rendered as unlimited or unknown", zeroPolicy.Member)
	}
	beforeZeroDispatch := dispatches.Load()
	expectStatus(t, native(actorCookie, actorCSRF), 429)
	if dispatches.Load() != beforeZeroDispatch {
		t.Fatal("Overview zero-policy proof changed finite enforcement")
	}
	write(actor.User.ID, `{"tokens_month":60,"money_month":"20","currency":"USD","reason":"Restore separate member ceiling"}`)
	expectStatus(t, native(actorCookie, actorCSRF), 200)
	expectStatus(t, native(peerCookie, peerCSRF), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	refresh()
	settledPage := get(actorCookie, "")
	settled := find(settledPage, teamID)
	assertUsage(settled.Aggregate, "10", "0", "10", "")
	assertUsage(settled.Member, "5", "0", "5", "")
	adminSelf := find(get(adminCookie, ""), teamID)
	if adminSelf.Member.AccountID == settled.Member.AccountID || adminSelf.Member.Usage == nil || adminSelf.Member.Usage.TokensUsed != "0" || get(adminCookie, "").ActorUserID != admin.User.ID {
		t.Fatal("administrator self Overview borrowed a member account", adminSelf)
	}
	if settledPage.Personal.Usage == nil || settledPage.Personal.Usage.TokensUsed != "0" || settledPage.Personal.Usage.MoneyUsed["USD"] != "" || settledPage.Personal.TokensMonth == nil || *settledPage.Personal.TokensMonth != "0" || !settled.Aggregate.Usage.AsOf.Equal(settled.Member.Usage.AsOf) || !settledPage.Personal.Usage.AsOf.Equal(settled.Member.Usage.AsOf) {
		t.Fatal("Team charged Personal or batch observations were incoherent", settledPage)
	}
	var calls []entity.CallRecord
	if err := db.Where("team_id = ?", teamID).Find(&calls).Error; err != nil || len(calls) != 3 {
		t.Fatal("missing immutable native Team facts including the quota denial", calls, err)
	}
	memberships := map[string]string{actor.User.ID: initial.MembershipID, peer.User.ID: peerMembershipID}
	succeeded := map[string]bool{}
	denied := 0
	for _, call := range calls {
		if call.TeamID != teamID || memberships[call.UserID] == "" || call.TeamMembershipID != memberships[call.UserID] || call.ProjectID != "" || call.KeyID != "" || call.ModelID != modelID || call.Protocol != entity.ProtocolOpenAIChat || call.Stream {
			t.Fatal("native Team attribution borrowed another scope", call)
		}
		var attempts []entity.CallAttempt
		if err := db.Where("request_id = ?", call.RequestID).Find(&attempts).Error; err != nil {
			t.Fatal("read exact immutable Team attempts", err)
		}
		if call.Status == "error" && call.ErrorCode == "quota_exceeded" {
			if call.UserID != actor.User.ID || call.SnapshotID != "" || len(attempts) != 0 || call.ProviderID != "" || call.ProviderModelID != "" || call.ConnectionID != "" {
				t.Fatal("quota denial fabricated dispatch or route attribution", call, attempts)
			}
			denied++
			continue
		}
		if call.Status != "success" || call.ErrorCode != "" || call.SnapshotID == "" || succeeded[call.UserID] || len(attempts) != 1 {
			t.Fatal("missing one successful native Team attempt per member", call, attempts)
		}
		attempt := attempts[0]
		if attempt.RequestID != call.RequestID || attempt.SnapshotID != call.SnapshotID || attempt.CredentialID != "crd_overview" || attempt.ProviderModelID != "pmd_overview" || attempt.Status != "success" || attempt.NativeCompletionEvidence != "completed" || !attempt.FinalUsageKnown {
			t.Fatal("native completion or immutable route evidence differs", call, attempt)
		}
		succeeded[call.UserID] = true
	}
	if denied != 1 || len(succeeded) != 2 || !succeeded[actor.User.ID] || !succeeded[peer.User.ID] || dispatches.Load() != 3 {
		t.Fatal("quota denial or successful native calls were omitted or replayed", denied, succeeded, dispatches.Load())
	}
	blocked.Store(true)
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- native(actorCookie, actorCSRF) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("controlled Team reservation did not enter upstream")
	}
	heldPage := get(actorCookie, "")
	held := find(heldPage, teamID)
	assertUsage(held.Aggregate, "10", "0", "10", "")
	assertUsage(held.Member, "5", "0", "5", "")
	assertReservations(held.Aggregate, "5", "5.000000000000000002")
	assertReservations(held.Member, "5", "5.000000000000000002")
	assertReservations(heldPage.Personal, "0", "")
	if !held.Aggregate.Usage.AsOf.Equal(held.Member.Usage.AsOf) || !heldPage.Personal.Usage.AsOf.Equal(held.Member.Usage.AsOf) {
		t.Fatal("live reservations were sampled outside the coherent account batch", heldPage)
	}
	unblock()
	select {
	case response := <-result:
		expectStatus(t, response, 200)
	case <-time.After(5 * time.Second):
		t.Fatal("controlled Team reservation did not settle")
	}
	blocked.Store(false)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	refresh()
	settled = find(get(actorCookie, ""), teamID)
	assertUsage(settled.Aggregate, "15", "0", "15", "")
	assertUsage(settled.Member, "10", "0", "10", "")
	assertReservations(settled.Aggregate, "0", "")
	assertReservations(settled.Member, "0", "")
	pairID, membershipID := settled.Member.AccountID, settled.MembershipID
	memberDisabled := append([]service.TeamMemberInput{}, members...)
	memberDisabled[1].Status = entity.ResourceDisabled
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, memberDisabled); err != nil {
		t.Fatal(err)
	}
	absent(get(actorCookie, ""), teamID)
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, members); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, []service.TeamMemberInput{members[0], members[2]}); err != nil {
		t.Fatal(err)
	}
	absent(get(actorCookie, ""), teamID)
	beforeDispatch := dispatches.Load()
	expectStatus(t, native(actorCookie, actorCSRF), 403)
	if dispatches.Load() != beforeDispatch {
		t.Fatal("removed member dispatched native call")
	}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, members); err != nil {
		t.Fatal(err)
	}
	rejoined := find(get(actorCookie, ""), teamID)
	if rejoined.MembershipID == membershipID || rejoined.Member.AccountID != pairID || !reflect.DeepEqual(rejoined.Member.TokensMonth, settled.Member.TokensMonth) {
		t.Fatal("rejoin reused authority or reset stable policy", rejoined, settled)
	}
	assertUsage(rejoined.Member, "10", "0", "10", "")
	// Stored child null remains distinct from an aggregate allowance. Decimal
	// policy strings must survive the database and JSON without floating point.
	write(actor.User.ID, `{"tokens_month":null,"money_month":"1.000000000000000001","currency":"USD","reason":"Exact local stored summary"}`)
	decimal := find(get(actorCookie, ""), teamID)
	if decimal.Member.TokensMonth != nil || decimal.Member.MoneyMonth == nil || *decimal.Member.MoneyMonth != "1.000000000000000001" || *decimal.Aggregate.TokensMonth != "100" {
		t.Fatal("summary inferred effective quota or rounded money", decimal)
	}
	write(actor.User.ID, `{"money_month":null,"reason":"Unlimited local member money"}`)
	write("", `{"tokens_month":null,"money_month":null,"reason":"Observe native unknown usage independently"}`)
	unknown.Store(true)
	expectStatus(t, native(actorCookie, actorCSRF), 200)
	unknown.Store(false)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	refresh()
	unknownPage := find(get(actorCookie, ""), teamID)
	for _, account := range []service.MemberOverviewMonthlyAccount{unknownPage.Aggregate, unknownPage.Member} {
		if account.Usage == nil || account.Usage.TokensUnknown != "1" || account.Usage.MoneyUnknown != "1" || account.Usage.TokensUsed == "0" || account.TokensMonth != nil || account.MoneyMonth != nil || account.Currency != nil {
			t.Fatal("unknown native use became zero or inherited a policy", account)
		}
	}
	if own := get(actorCookie, "").Personal; own.Usage == nil || own.Usage.TokensUsed != "0" || own.Usage.TokensUnknown != "0" || own.Usage.MoneyUnknown != "0" {
		t.Fatal("unknown Team use contaminated Personal history", own)
	}
	// Each malformed persisted relationship is a separate exactness boundary.
	// MySQL's default case/trailing-space collation must not confer authority.
	for _, mutation := range []struct{ field, value string }{{"user_id", strings.ToUpper(actor.User.ID)}, {"user_id", actor.User.ID + " "}, {"team_id", strings.ToUpper(teamID)}, {"team_id", teamID + " "}, {"status", "ACTIVE"}, {"role", "MEMBER"}} {
		var row entity.TeamMembership
		if err := db.Where("id = ?", rejoined.MembershipID).First(&row).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&entity.TeamMembership{}).Where("id = ?", row.ID).Update(mutation.field, mutation.value).Error; err != nil {
			// Released foreign keys/checks reject some aliases on PostgreSQL;
			// case-insensitive MySQL constraints may retain the same values. In
			// the former case prove the canonical relationship was unchanged;
			// in the latter case the endpoint must exclude the persisted alias.
			var unchanged entity.TeamMembership
			if readErr := db.Where("id = ?", row.ID).First(&unchanged).Error; readErr != nil || !reflect.DeepEqual(unchanged, row) {
				t.Fatal("rejected alias changed current membership", mutation, err, unchanged, readErr)
			}
			t.Log("governance constraint rejected membership alias", mutation.field)
			continue
		}
		var persisted entity.TeamMembership
		if err := db.Where("id = ?", row.ID).First(&persisted).Error; err != nil {
			t.Fatal("read retained membership after alias attempt", mutation, err)
		}
		if reflect.DeepEqual(persisted, row) {
			// A full-width varchar may discard an excess trailing space instead
			// of retaining an alias. Canonical retained bytes remain authority;
			// do not claim this normalization exercised alias rejection.
			if mutation.field != "user_id" || mutation.value != row.UserID+" " {
				t.Fatal("membership mutation unexpectedly retained canonical bytes", mutation, persisted)
			}
			canonical := find(get(actorCookie, ""), teamID)
			if canonical.MembershipID != row.ID || canonical.Member.AccountID != pairID {
				t.Fatal("normalized membership changed current authority", canonical)
			}
			t.Log("database retained canonical full-width user ID after trailing-space normalization")
			continue
		}
		absent(get(actorCookie, ""), teamID)
		if err := db.Model(&entity.TeamMembership{}).Where("id = ?", row.ID).Updates(map[string]any{"user_id": row.UserID, "team_id": row.TeamID, "status": row.Status, "role": row.Role}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// A historical Team account predates journal activation; known subtotals are
	// useful but never prove full monthly coverage, including after a new join.
	historicalID := "tem_overview_historical"
	create(&entity.Team{ID: historicalID, Name: "Historical Team", Status: entity.ResourceActive, CreatedAt: time.Now().Add(-48 * time.Hour)}, &entity.TeamMembership{ID: "tmm_overview_historical", TeamID: historicalID, UserID: actor.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	refresh()
	historical := find(get(actorCookie, ""), historicalID)
	if historical.Aggregate.Usage == nil || historical.Member.Usage == nil || historical.Aggregate.Usage.Covered || historical.Member.Usage.Covered || historical.Aggregate.Usage.TokensUsed != "0" || historical.Member.Usage.TokensUsed != "0" {
		t.Fatal("membership birth fabricated historical Team coverage", historical)
	}
	var historicalTeam entity.Team
	if err := db.Where("id = ?", historicalID).First(&historicalTeam).Error; err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"ACTIVE", entity.ResourceDisabled} {
		if err := db.Model(&entity.Team{}).Where("id = ?", historicalID).Update("status", status).Error; err != nil {
			if status != "ACTIVE" {
				t.Fatal(err)
			}
			var unchanged entity.Team
			if readErr := db.Where("id = ?", historicalID).First(&unchanged).Error; readErr != nil || unchanged.Status != historicalTeam.Status {
				t.Fatal("rejected Team status alias changed authority", err, unchanged, readErr)
			}
			continue
		}
		absent(get(actorCookie, ""), historicalID)
	}
	if err := db.Model(&entity.Team{}).Where("id = ?", historicalID).Update("status", historicalTeam.Status).Error; err != nil {
		t.Fatal(err)
	}
	// A later creation template never becomes a policy for retained resources.
	templateTokens := int64(999)
	if err := db.Save(&entity.DefaultLimitRule{Kind: "team", TokensMonth: &templateTokens, RuleETag: secret.SHA256Hex("overview_creation_template"), ActorID: admin.User.ID, Reason: "Future creation only"}).Error; err != nil {
		t.Fatal(err)
	}
	templatePage := find(get(actorCookie, ""), historicalID)
	if templatePage.Aggregate.PolicyETag != "0" || templatePage.Member.PolicyETag != "0" || templatePage.Aggregate.TokensMonth != nil || templatePage.Member.TokensMonth != nil {
		t.Fatal("Overview inherited a creation template", templatePage)
	}
	// Legacy safe IDs and fifty independent memberships use the same bounded
	// snapshot query count as one row. No catalogue or member directory is read.
	for i := 0; i < 51; i++ {
		id := fmt.Sprintf("tem_overview_page_%02d", i)
		create(&entity.Team{ID: id, Name: fmt.Sprintf("Page Team %02d", i), Status: entity.ResourceActive}, &entity.TeamMembership{ID: fmt.Sprintf("tmm_overview_page_%02d", i), TeamID: id, UserID: actor.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	}
	refresh()
	seen := map[string]bool{}
	cursor := ""
	var previous string
	for {
		query := "?limit=10"
		if cursor != "" {
			query += "&cursor=" + url.QueryEscape(cursor)
		}
		page := get(actorCookie, query)
		if len(page.Teams) > 10 || page.ActorUserID != actor.User.ID {
			t.Fatal("pagination escaped self bounds", page)
		}
		for _, item := range page.Teams {
			if seen[item.ID] || previous != "" && item.ID <= previous || item.Member.AccountID == item.Aggregate.AccountID {
				t.Fatal("unstable or aliased Team pagination", item)
			}
			seen[item.ID], previous = true, item.ID
		}
		if page.NextCursor == nil {
			break
		}
		if *page.NextCursor == cursor || len(page.Teams) == 0 {
			t.Fatal("nonadvancing bounded cursor", page)
		}
		cursor = *page.NextCursor
	}
	if len(seen) != 53 {
		t.Fatal("bounded pages lost current memberships", len(seen))
	}
	firstPage := get(actorCookie, "?limit=1")
	if firstPage.NextCursor == nil {
		t.Fatal("missing bounded continuation")
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/overview/accounts?cursor="+url.QueryEscape(*firstPage.NextCursor), "", peerCookie, ""), 400)
	measure := func(limit int) {
		t.Helper()
		queryMu.Lock()
		queries = nil
		queryMu.Unlock()
		page, err := svc.MemberOverviewAccounts(context.WithValue(ctx, memberOverviewQueryMarker{}, true), actor.User.ID, service.MemberOverviewAccountsFilter{Limit: limit})
		queryMu.Lock()
		reads := append([]string{}, queries...)
		queryMu.Unlock()
		if err != nil || page == nil || len(page.Teams) != limit || len(reads) != 5 {
			t.Fatal("Overview did not use five bounded governance reads", limit, page, err, len(reads), reads)
		}
		for _, sql := range reads {
			for _, forbidden := range []string{"provider", "models", "model_grant", "api_keys", "call_records", "audit_events", "role_permissions", "user_roles"} {
				if strings.Contains(strings.ToLower(sql), forbidden) {
					t.Fatal("Overview borrowed a directory or another workspace", sql)
				}
			}
		}
	}
	measure(1)
	measure(50)
	// Current publication, monetary denomination and calendar are independent
	// from saved rows and authoritative usage. An old snapshot cannot prove them.
	var currency entity.PricingSetting
	var calendar entity.QuotaSetting
	if err := db.First(&currency, 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&calendar, 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).Updates(map[string]any{"platform_currency": "EUR", "ETag": "overview_unpublished_currency"}).Error; err != nil {
		t.Fatal(err)
	}
	currencyPage := get(actorCookie, "?limit=50")
	if currencyPage.PlatformCurrency != "EUR" || currencyPage.Personal.RuntimeApplied || find(currencyPage, teamID).Aggregate.RuntimeApplied {
		t.Fatal("unpublished denomination claimed current application", currencyPage)
	}
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).Updates(map[string]any{"platform_currency": currency.PlatformCurrency, "ETag": currency.ETag}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.QuotaSetting{}).Where("id = ?", 1).Updates(map[string]any{"time_zone": "Asia/Tokyo", "ETag": "overview_unpublished_calendar"}).Error; err != nil {
		t.Fatal(err)
	}
	calendarPage := get(actorCookie, "?limit=50")
	if calendarPage.Personal.RuntimeApplied || find(calendarPage, teamID).Member.RuntimeApplied || calendarPage.Personal.Usage == nil || calendarPage.Personal.Usage.TimeZone != calendar.TimeZone {
		t.Fatal("stored calendar fabricated journal application", calendarPage)
	}
	if err := db.Model(&entity.QuotaSetting{}).Where("id = ?", 1).Updates(map[string]any{"time_zone": calendar.TimeZone, "ETag": calendar.ETag}).Error; err != nil {
		t.Fatal(err)
	}
	refresh()
	var personalRow entity.ResourceLimit
	if err := db.Where("scope_kind = ? AND scope_id = ?", "user", actor.User.ID).First(&personalRow).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "user", actor.User.ID).Update("ETag", "overview_unpublished_policy").Error; err != nil {
		t.Fatal(err)
	}
	unpublished := get(actorCookie, "")
	if unpublished.Personal.PolicyETag != "overview_unpublished_policy" || unpublished.Personal.RuntimeApplied || unpublished.Personal.Usage == nil {
		t.Fatal("saved policy was mistaken for applied policy", unpublished.Personal)
	}
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "user", actor.User.ID).Update("ETag", personalRow.ETag).Error; err != nil {
		t.Fatal(err)
	}
	refresh()
	lease := svc.RuntimeStatus().AuthorizationValidUntil
	if lease == nil {
		t.Fatal("missing authorization lease")
	}
	if wait := time.Until(*lease) + 20*time.Millisecond; wait > 0 {
		timer := time.NewTimer(wait)
		<-timer.C
	}
	stale := get(actorCookie, "?limit=50")
	if stale.Personal.RuntimeApplied || find(stale, teamID).Aggregate.RuntimeApplied || stale.Personal.Usage == nil {
		t.Fatal("expired authorization lease claimed current application", stale)
	}
	refresh()
	// A failed publication after lifecycle change still removes the Team from
	// the current SQL snapshot and prevents dispatch through its tombstone.
	failPublication.Store(true)
	disabled := entity.ResourceDisabled
	if _, err := svc.UpdateResource(ctx, admin.User.ID, service.TeamResource, teamID, service.ResourceUpdate{Status: &disabled}); err == nil {
		t.Fatal("controlled publication outage unexpectedly succeeded")
	}
	failPublication.Store(false)
	absent(get(actorCookie, "?limit=50"), teamID)
	beforeDispatch = dispatches.Load()
	expectStatus(t, native(actorCookie, actorCSRF), 403)
	if dispatches.Load() != beforeDispatch {
		t.Fatal("disabled Team dispatched through stale publication")
	}
	active := entity.ResourceActive
	if _, err := svc.UpdateResource(ctx, admin.User.ID, service.TeamResource, teamID, service.ResourceUpdate{Status: &active}); err != nil {
		t.Fatal(err)
	}
	refresh()
	beforeRestart := find(get(actorCookie, "?limit=50"), teamID)
	svc.StopRuntime()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	journalUnavailable := find(get(actorCookie, "?limit=50"), teamID)
	if journalUnavailable.Member.UsageStatus != "unavailable" || journalUnavailable.Member.Usage != nil || journalUnavailable.Member.ActiveReservations != nil || journalUnavailable.Member.RuntimeApplied || journalUnavailable.Member.PolicyETag != beforeRestart.Member.PolicyETag {
		t.Fatal("closed journal fabricated counters or hid saved policy", journalUnavailable.Member)
	}
	svc = newService()
	router = fox.New()
	New(svc).RegisterRoutes(router)
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	afterRestart := find(get(actorCookie, "?limit=50"), teamID)
	if afterRestart.MembershipID != beforeRestart.MembershipID || afterRestart.Member.AccountID != pairID || afterRestart.Member.Usage == nil || afterRestart.Member.Usage.TokensUsed != beforeRestart.Member.Usage.TokensUsed || afterRestart.Member.Usage.TokensUnknown != "1" || afterRestart.Aggregate.Usage.TokensUsed != "15" {
		t.Fatal("restart reset current membership or historical stable account", beforeRestart, afterRestart)
	}
	assertReservations(afterRestart.Aggregate, "0", "")
	assertReservations(afterRestart.Member, "0", "")
	archived := entity.ResourceArchived
	if _, err := svc.UpdateResource(ctx, admin.User.ID, service.TeamResource, teamID, service.ResourceUpdate{Status: &disabled}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateResource(ctx, admin.User.ID, service.TeamResource, teamID, service.ResourceUpdate{Status: &archived}); err != nil {
		t.Fatal(err)
	}
	absent(get(actorCookie, "?limit=50"), teamID)
	if _, err := svc.UpdateMember(ctx, admin.User.ID, actor.User.ID, func() *bool { v := true; return &v }(), nil); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/overview/accounts", "", actorCookie, ""), 401)
	// A nonmember administrator/operator still has only its own Personal account.
	if page := get(operatorCookie, "?limit=50"); page.ActorUserID != operator.User.ID || len(page.Teams) != 0 {
		t.Fatal("platform authority became another actor's Team directory", page)
	}
	if _, err := svc.EmergencyOffboarding(ctx, admin.User.ID, operator.User.ID, service.OffboardingEmergencyInput{RequestID: "a7451331-7a61-4909-a8ee-ab9932f57b94", CurrentPassword: "test-only-overview-password", Reason: "Overview current actor authority"}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/overview/accounts", "", operatorCookie, ""), 401)
	if _, err := svc.MemberOverviewAccounts(ctx, operator.User.ID, service.MemberOverviewAccountsFilter{}); err == nil {
		t.Fatal("offboarded actor retained self account authority")
	}
}
