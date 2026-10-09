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
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
)

type administrativeOverviewQueryMarker struct{}

// Native facts are genuine controlled HTTP exchanges. Setup rows and the
// unrelated-cardinality rows are not represented as native completion evidence.
func testMemberOverviewLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var queryMu sync.Mutex
	var queries []string
	var failRead, failPublication atomic.Bool
	observe := func(tx *gorm.DB) {
		if failPublication.Load() && tx.Statement.Table == "models" || failRead.Load() && tx.Statement.Context.Value(administrativeOverviewQueryMarker{}) != nil {
			_ = tx.AddError(errors.New("controlled administrative Overview read outage"))
		}
		if tx.Statement.Context.Value(administrativeOverviewQueryMarker{}) != nil {
			queryMu.Lock()
			queries = append(queries, tx.Statement.SQL.String())
			queryMu.Unlock()
		}
	}
	const callback = "administrative_overview_reads"
	if err := db.Callback().Query().After("gorm:query").Register(callback, observe); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().After("gorm:row").Register(callback, observe); err != nil {
		_ = db.Callback().Query().Remove(callback)
		t.Fatal(err)
	}
	var instances []*service.Service
	var mode atomic.Int32 // 0 known, 1 held-known, 2 unknown, 3 known-zero.
	var posts atomic.Int32
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() {
		unblock()
		failRead.Store(false)
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
	store, err := secretstore.New(bytes.Repeat([]byte{199}, 32))
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
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"member-card-admin@example.invalid","password":"test-only-member-card-password","name":"Member card administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	reader, readerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "member-card-reader", []string{"members.read"})
	_, writerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "member-card-writer", []string{"limits.users.write"})
	create := func(rows ...any) {
		t.Helper()
		for _, row := range rows {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	path := func(id string) string { return "/api/v1/admin/members/" + url.PathEscape(id) + "/overview" }
	get := func(cookie *http.Cookie, id string) service.MemberOverviewRecord {
		t.Helper()
		before := time.Now().UTC()
		response := identityRequest(router, "GET", path(id), "", cookie, "")
		expectStatus(t, response, 200)
		var value service.MemberOverviewRecord
		if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if value.UserID != id || value.ObservedAt.Before(before) || value.ObservedAt.After(time.Now().UTC()) || value.ObservedAt.Location() != time.UTC || value.PlatformCurrency != "USD" || value.Personal.AccountID != "user_"+id || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("subject identity, server observation or private contract differs", value, response.Header())
		}
		for _, field := range []string{`"key_id"`, `"token_hash"`, `"prefix"`, `"secret"`, `"teams"`, `"model_ids"`, `"remaining"`} {
			if strings.Contains(response.Body.String(), field) {
				t.Fatal("statistic exposed a directory, secret or inferred allowance", field)
			}
		}
		return value
	}
	expectStatus(t, identityRequest(router, "GET", path(reader.User.ID), "", nil, ""), 401)
	expectStatus(t, identityRequest(router, "GET", path(reader.User.ID), "", writerCookie, ""), 403)
	cold := get(readerCookie, reader.User.ID)
	if cold.TotalPersonalKeys != "0" || cold.Personal.UsageStatus != "unavailable" || cold.Personal.Usage != nil || cold.Personal.ActiveReservations != nil || cold.Personal.RuntimeApplied {
		t.Fatal("cold reader Overview manufactured journal zero", cold)
	}
	for _, query := range []string{"?user_id=" + reader.User.ID, "?limit=1", "?cursor=x", "?q=x", "?currency=EUR", "?user_id=a&user_id=b", "?unused="} {
		expectStatus(t, identityRequest(router, "GET", path(reader.User.ID)+query, "", readerCookie, ""), 400)
	}
	for _, alias := range []string{strings.ToUpper(reader.User.ID), reader.User.ID + " "} {
		if _, err := svc.MemberOverview(ctx, admin.User.ID, alias); err == nil {
			t.Fatal("collation target alias acquired Overview", alias)
		}
		if _, err := svc.MemberOverview(ctx, alias, reader.User.ID); err == nil {
			t.Fatal("collation actor alias acquired Overview", alias)
		}
	}
	expectStatus(t, identityRequest(router, "GET", path("usr_card_missing"), "", readerCookie, ""), 404)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer card-upstream" || r.Header.Get("Cookie") != "" || r.Header.Get("X-CSRF-Token") != "" {
			t.Error("controlled upstream received wrong path or private authority")
			w.WriteHeader(400)
			return
		}
		current := mode.Load()
		if current == 1 {
			entered <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		base := `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Controlled"},"finish_reason":"stop"}]`
		if current == 2 {
			_, _ = io.WriteString(w, base+`}`)
			return
		}
		input, output := 4, 1
		if current == 3 {
			input, output = 0, 0
		}
		_, _ = fmt.Fprintf(w, `%s,"usage":{"prompt_tokens":%d,"completion_tokens":%d,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`, base, input, output)
	}))
	t.Cleanup(upstream.Close)
	t.Cleanup(unblock)
	cipher, err := store.Seal("crd_card", "card-upstream")
	if err != nil {
		t.Fatal(err)
	}
	modelID := "mdl_card"
	create(&entity.Provider{ID: "prv_card", Name: "Controlled card Provider"}, &entity.ProviderConnection{ID: "con_card", ProviderID: "prv_card", Name: "Chat", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"}, &entity.ProviderCredential{ID: "crd_card", ConnectionID: "con_card", Name: "Ready", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"}, &entity.ProviderModel{ID: "pmd_card", ConnectionID: "con_card", UpstreamName: "native"}, &entity.CredentialModelAccess{CredentialID: "crd_card", ProviderModelID: "pmd_card"}, &entity.Model{ID: modelID, Status: entity.ResourceActive}, &entity.ModelName{Name: "member-card-native", ModelID: modelID, CurrentModelID: &modelID}, &entity.ModelProviderBinding{ID: "bnd_card", ModelID: modelID, ProviderModelID: "pmd_card", Weight: 100}, &entity.ModelPrice{ID: "price_card", ProviderModelID: "pmd_card", UpdateSource: "api"}, &entity.ReservationBound{ProviderModelID: "pmd_card", Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 4, MaxOutputTokens: 1, ETag: "card_bound", Evidence: "Controlled native four input one output", Reason: "Member card acceptance"})
	for index, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		amount := "1000000"
		if metric == pricing.Output {
			amount = "1000000.000000000001"
		}
		create(&entity.PriceRate{ID: fmt.Sprintf("rate_card_%d", index), ModelPriceID: "price_card", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: amount, Enabled: true})
	}
	key := func(userID, id, token, status string, expires *time.Time) {
		t.Helper()
		create(&entity.APIKey{ID: id, UserID: userID, Name: "Private Key must not leak", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(token), Status: status, ExpiresAt: expires})
		if status == entity.KeyActive {
			create(&entity.APIKeyModel{KeyID: id, ModelID: modelID})
		}
	}
	warmKey := "rx_" + strings.Repeat("w", 43)
	create(&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID})
	key(admin.User.ID, "key_card_warm", warmKey, entity.KeyActive, nil)
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	spool := filepath.Join(t.TempDir(), "member-card.db")
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	if value := get(readerCookie, reader.User.ID); value.Personal.UsageStatus != "inactive" || value.Personal.Usage != nil || value.Personal.ActiveReservations != nil {
		t.Fatal("unactivated journal manufactured usage", value)
	}
	body := `{"model":"member-card-native","messages":[{"role":"user","content":"Controlled"}],"max_completion_tokens":1}`
	var requestIDs []string
	native := func(token, teamID string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		endpoint := "http://routex.test/v1/chat/completions"
		if teamID != "" {
			endpoint = "http://routex.test/api/v1/teams/" + teamID + "/chat/completions"
		}
		request := httptest.NewRequestWithContext(bounded, "POST", endpoint, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if teamID == "" {
			request.Header.Set("Authorization", "Bearer "+token)
		} else {
			request.AddCookie(cookie)
			request.Header.Set("Origin", "http://routex.test")
			request.Header.Set("Sec-Fetch-Site", "same-origin")
			request.Header.Set("X-CSRF-Token", csrf)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	complete := func(response *httptest.ResponseRecorder) {
		t.Helper()
		expectStatus(t, response, 200)
		requestIDs = append(requestIDs, response.Header().Get("X-Request-ID"))
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
	}
	complete(native(warmKey, "", nil, ""))
	cap, budget := int64(100), "100.000000000000000001"
	rule, err := svc.GetDefaultLimit(ctx, admin.User.ID, "user")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetDefaultLimit(ctx, admin.User.ID, "user", rule.ETag, service.DefaultLimitInput{Policy: service.EffectiveLimitValues{TokensMonth: &cap, MoneyMonth: &budget, Currency: "USD"}, Reason: "Exact creation default card"}); err != nil {
		t.Fatal(err)
	}
	subject, subjectCookie, subjectCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "member-card-subject", nil)
	peer, _, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "member-card-peer", nil)
	zeroSubject, _, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "member-card-zero", nil)
	subjectKey, peerKey, zeroKey := "rx_"+strings.Repeat("s", 43), "rx_"+strings.Repeat("p", 43), "rx_"+strings.Repeat("z", 43)
	for _, userID := range []string{subject.User.ID, peer.User.ID, zeroSubject.User.ID} {
		create(&entity.UserModelGrant{UserID: userID, ModelID: modelID})
	}
	key(subject.User.ID, "key_card_subject", subjectKey, entity.KeyActive, nil)
	key(peer.User.ID, "key_card_peer", peerKey, entity.KeyActive, nil)
	key(zeroSubject.User.ID, "key_card_zero", zeroKey, entity.KeyActive, nil)
	expired := time.Now().UTC().Add(-time.Hour)
	for index, status := range []string{entity.KeyPending, entity.KeyDisabled, entity.KeyRevoked, entity.KeyActive} {
		var deadline *time.Time
		if status == entity.KeyActive {
			deadline = &expired
		}
		key(subject.User.ID, fmt.Sprintf("key_card_retained_%d", index), fmt.Sprintf("retained-%d", index), status, deadline)
	}
	// Legacy persisted identities remain supported, while collation-equivalent
	// foreign-key values can never inflate an exact subject's retained count.
	legacyID := "usr_card_legacy"
	create(&entity.User{ID: legacyID, Email: "card-legacy@example.invalid", Name: "Retained legacy subject", PasswordHash: "unused", Role: entity.RoleMember})
	key(legacyID, "key_card_legacy", "legacy-retained", entity.KeyRevoked, nil)
	for index, alias := range []string{strings.ToUpper(legacyID), legacyID + " "} {
		row := entity.APIKey{ID: fmt.Sprintf("key_card_alias_%d", index), UserID: alias, Name: "Foreign collation alias", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(fmt.Sprintf("alias-%d", index)), Status: entity.KeyRevoked}
		if err := db.Create(&row).Error; err != nil {
			var count int64
			if readErr := db.Model(&entity.APIKey{}).Where("id = ?", row.ID).Count(&count).Error; readErr != nil || count != 0 {
				t.Fatal("rejected foreign-key alias left a retained row", err, readErr, count)
			}
			t.Log("governance foreign key rejected retained ownership alias")
		} else {
			var persisted entity.APIKey
			if err := db.Take(&persisted, "id = ?", row.ID).Error; err != nil || persisted.UserID != alias {
				t.Fatal("ownership alias was not retained exactly", persisted, err)
			}
		}
		if value := get(readerCookie, legacyID); value.TotalPersonalKeys != "1" || value.Personal.TokensMonth != nil || value.Personal.MoneyMonth != nil || value.Personal.PolicyETag != "0" {
			t.Fatal("legacy policy/retained ownership count borrowed an alias or current default", value)
		}
	}
	expectStatus(t, identityRequest(router, "GET", path(strings.ToUpper(legacyID)), "", readerCookie, ""), 404)
	expectStatus(t, identityRequest(router, "GET", path(legacyID+" "), "", readerCookie, ""), 400)
	projectToken := "rxp_" + strings.Repeat("j", 43)
	create(&entity.Project{ID: "prj_card", Name: "Separate Project", CreatorID: subject.User.ID, Status: entity.ResourceActive}, &entity.ProjectManager{ID: "pmg_card", ProjectID: "prj_card", UserID: subject.User.ID}, &entity.ProjectManager{ID: "pmg_card_admin", ProjectID: "prj_card", UserID: admin.User.ID}, &entity.ProjectModelGrant{ProjectID: "prj_card", ModelID: modelID}, &entity.ProjectKey{ID: "key_card_project", ProjectID: "prj_card", CreatorID: subject.User.ID, Name: "Separate Project Key", Prefix: "rxp_masked", TokenHash: secret.SHA256Hex(projectToken), Status: entity.KeyActive, DeliveryMode: "manual"}, &entity.ProjectKeyModel{KeyID: "key_card_project", ModelID: modelID}, &entity.Team{ID: "tem_card", Name: "Separate Team", Status: entity.ResourceActive}, &entity.TeamMembership{ID: "tmm_card_owner", TeamID: "tem_card", UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, &entity.TeamMembership{ID: "tmm_card_subject", TeamID: "tem_card", UserID: subject.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}, &entity.TeamModelGrant{TeamID: "tem_card", ModelID: modelID})
	refresh := func() {
		t.Helper()
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
	}
	refresh()
	initial := get(readerCookie, subject.User.ID)
	if initial.TotalPersonalKeys != "5" || initial.Personal.TokensMonth == nil || *initial.Personal.TokensMonth != "100" || initial.Personal.MoneyMonth == nil || *initial.Personal.MoneyMonth != budget || initial.Personal.Currency == nil || *initial.Personal.Currency != "USD" || initial.Personal.Usage == nil || !initial.Personal.Usage.Covered || initial.Personal.Usage.TokensUsed != "0" || !initial.Personal.RuntimeApplied {
		t.Fatal("subject count/default/known empty journal differs", initial)
	}
	rule, err = svc.GetDefaultLimit(ctx, admin.User.ID, "user")
	if err != nil {
		t.Fatal(err)
	}
	otherCap := int64(200)
	if _, err := svc.SetDefaultLimit(ctx, admin.User.ID, "user", rule.ETag, service.DefaultLimitInput{Policy: service.EffectiveLimitValues{TokensMonth: &otherCap}, Reason: "Later defaults must not change a saved subject"}); err != nil {
		t.Fatal(err)
	}
	if value := get(readerCookie, subject.User.ID); *value.Personal.TokensMonth != "100" || *value.Personal.MoneyMonth != budget {
		t.Fatal("current defaults replaced creation policy", value)
	}
	complete(native(subjectKey, "", nil, ""))
	settled := get(readerCookie, subject.User.ID)
	if settled.Personal.Usage.TokensUsed != "5" || settled.Personal.Usage.MoneyUsed["USD"] != "5.000000000000000001" || settled.Personal.Usage.TokensUnknown != "0" || settled.Personal.Usage.MoneyUnknown != "0" {
		t.Fatal("known exact native settlement differs", settled)
	}
	complete(native(peerKey, "", nil, ""))
	complete(native(projectToken, "", nil, ""))
	complete(native("", "tem_card", subjectCookie, subjectCSRF))
	if value := get(readerCookie, subject.User.ID); value.Personal.Usage.TokensUsed != settled.Personal.Usage.TokensUsed || !reflect.DeepEqual(value.Personal.Usage.MoneyUsed, settled.Personal.Usage.MoneyUsed) || value.TotalPersonalKeys != "5" {
		t.Fatal("other user/Project/Team usage contaminated subject card", value)
	}
	mode.Store(1)
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- native(subjectKey, "", nil, "") }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("held subject call did not enter")
	}
	held := get(readerCookie, subject.User.ID)
	if held.Personal.Usage.TokensUsed != "5" || held.Personal.Usage.TokensHeld != "0" || held.Personal.ActiveReservations == nil || held.Personal.ActiveReservations.TokensHeld != "5" || held.Personal.ActiveReservations.MoneyHeld["USD"] != "5.000000000000000003" {
		t.Fatal("live hold flattened into monthly facts or rounded", held)
	}
	unblock()
	select {
	case response := <-result:
		complete(response)
	case <-time.After(5 * time.Second):
		t.Fatal("held subject call did not finish")
	}
	mode.Store(3)
	complete(native(zeroKey, "", nil, ""))
	knownZero := get(readerCookie, zeroSubject.User.ID)
	if knownZero.Personal.Usage == nil || knownZero.Personal.Usage.TokensUsed != "0" || knownZero.Personal.Usage.TokensUnknown != "0" || knownZero.Personal.Usage.MoneyUnknown != "0" {
		t.Fatal("authoritative native zero became unknown", knownZero)
	}
	zeroLimit, err := svc.GetResourceLimit(ctx, admin.User.ID, service.LimitTarget{Kind: "user", ID: zeroSubject.User.ID})
	if err != nil {
		t.Fatal(err)
	}
	zeroTokens, zeroMoney := int64(0), "0"
	if _, err := svc.SetResourceLimit(ctx, admin.User.ID, service.LimitTarget{Kind: "user", ID: zeroSubject.User.ID}, zeroLimit.ETag, service.LimitInput{Policy: limits.Policy{TokensMonth: &zeroTokens, MoneyMonth: &zeroMoney, Currency: "USD"}, Reason: "Explicit finite zero card"}); err != nil {
		t.Fatal(err)
	}
	zeroPolicy := get(readerCookie, zeroSubject.User.ID)
	if zeroPolicy.Personal.TokensMonth == nil || *zeroPolicy.Personal.TokensMonth != "0" || zeroPolicy.Personal.MoneyMonth == nil || *zeroPolicy.Personal.MoneyMonth != "0" || zeroPolicy.Personal.Currency == nil || *zeroPolicy.Personal.Currency != "USD" {
		t.Fatal("finite policy zero became unlimited", zeroPolicy)
	}
	var zeroCall entity.CallRecord
	if err := db.Take(&zeroCall, "request_id = ?", requestIDs[len(requestIDs)-1]).Error; err != nil || zeroCall.ChargeAmount == nil || *zeroCall.ChargeAmount != "0" {
		t.Fatal("known native zero charge missing", zeroCall, err)
	}
	// Unknown usage must be admitted under an explicitly unlimited policy, rather
	// than replayed after contaminating finite monthly enforcement.
	limit, err := svc.GetResourceLimit(ctx, admin.User.ID, service.LimitTarget{Kind: "user", ID: subject.User.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetResourceLimit(ctx, admin.User.ID, service.LimitTarget{Kind: "user", ID: subject.User.ID}, limit.ETag, service.LimitInput{Policy: limits.Policy{}, Reason: "Observe native unknown independently"}); err != nil {
		t.Fatal(err)
	}
	mode.Store(2)
	complete(native(subjectKey, "", nil, ""))
	mode.Store(0)
	unknown := get(readerCookie, subject.User.ID)
	if unknown.Personal.TokensMonth != nil || unknown.Personal.MoneyMonth != nil || unknown.Personal.Currency != nil || unknown.Personal.Usage.TokensUsed != "10" || unknown.Personal.Usage.MoneyUsed["USD"] != "10.000000000000000002" || unknown.Personal.Usage.TokensUnknown != "1" || unknown.Personal.Usage.MoneyUnknown != "1" {
		t.Fatal("native unknown became zero or hid retained exact currency", unknown)
	}
	// Constant seven SQL reads are measured only inside the actual service
	// snapshot, excluding Session middleware and independent native accounting.
	marked := context.WithValue(ctx, administrativeOverviewQueryMarker{}, true)
	queryCount := func() int {
		t.Helper()
		queryMu.Lock()
		queries = nil
		queryMu.Unlock()
		if _, err := svc.MemberOverview(marked, reader.User.ID, subject.User.ID); err != nil {
			t.Fatal(err)
		}
		queryMu.Lock()
		defer queryMu.Unlock()
		for _, sql := range queries {
			lower := strings.ToLower(sql)
			if strings.Contains(lower, "team_memberships") || strings.Contains(lower, "provider_models") || strings.Contains(lower, "project_keys") {
				t.Fatal("Overview fetched a directory", sql)
			}
		}
		return len(queries)
	}
	if count := queryCount(); count != 7 {
		t.Fatal("unbounded or changed snapshot query count", count, queries)
	}
	for index := range 32 {
		create(&entity.User{ID: fmt.Sprintf("usr_card_noise_%02d", index), Email: fmt.Sprintf("card-noise-%02d@example.invalid", index), Name: "Unrelated cardinality", PasswordHash: "unused", Role: entity.RoleMember})
		key(fmt.Sprintf("usr_card_noise_%02d", index), fmt.Sprintf("key_card_noise_%02d", index), fmt.Sprintf("noise-%d", index), entity.KeyRevoked, nil)
	}
	if count := queryCount(); count != 7 || get(readerCookie, subject.User.ID).TotalPersonalKeys != "5" {
		t.Fatal("unrelated cardinality widened reads or target count", count)
	}
	failRead.Store(true)
	if _, err := svc.MemberOverview(marked, reader.User.ID, subject.User.ID); err == nil {
		t.Fatal("SQL outage fabricated a statistic")
	}
	failRead.Store(false)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := svc.MemberOverview(cancelled, reader.User.ID, subject.User.ID); err == nil {
		t.Fatal("cancelled snapshot returned facts")
	}
	// Raw revision/denomination/calendar changes preserve local facts but cannot
	// claim that the previous private publication applies them.
	var policy entity.ResourceLimit
	if err := db.Take(&policy, "scope_kind = ? AND scope_id = ?", "user", subject.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "user", subject.User.ID).Update("ETag", "card_unpublished").Error; err != nil {
		t.Fatal(err)
	}
	if value := get(readerCookie, subject.User.ID); value.Personal.RuntimeApplied || value.Personal.PolicyETag != "card_unpublished" || value.Personal.Usage == nil {
		t.Fatal("raw policy generation was called applied", value)
	}
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "user", subject.User.ID).Update("ETag", policy.ETag).Error; err != nil {
		t.Fatal(err)
	}
	var calendar entity.QuotaSetting
	if err := db.Take(&calendar).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.QuotaSetting{}).Where("id = ?", calendar.ID).Update("ETag", "card_calendar").Error; err != nil {
		t.Fatal(err)
	}
	if value := get(readerCookie, subject.User.ID); value.Personal.RuntimeApplied || value.Personal.Usage == nil {
		t.Fatal("unpublished calendar claimed application", value)
	}
	if err := db.Model(&entity.QuotaSetting{}).Where("id = ?", calendar.ID).Update("ETag", calendar.ETag).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).Update("platform_currency", "EUR").Error; err != nil {
		t.Fatal(err)
	}
	currencyChanged, err := svc.MemberOverview(ctx, reader.User.ID, subject.User.ID)
	if err != nil || currencyChanged.PlatformCurrency != "EUR" || currencyChanged.Personal.RuntimeApplied || currencyChanged.Personal.Usage == nil || currencyChanged.Personal.Usage.MoneyUsed["USD"] != "10.000000000000000002" || len(currencyChanged.Personal.Usage.MoneyUsed) != 1 {
		t.Fatal("currency generation converted history or claimed unpublished application", currencyChanged, err)
	}
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).Update("platform_currency", "USD").Error; err != nil {
		t.Fatal(err)
	}
	refresh()
	lease := svc.RuntimeStatus().AuthorizationValidUntil
	if lease == nil {
		t.Fatal("missing real authorization lease")
	}
	if wait := time.Until(*lease) + 20*time.Millisecond; wait > 0 {
		timer := time.NewTimer(wait)
		<-timer.C
	}
	if value := get(readerCookie, subject.User.ID); value.Personal.RuntimeApplied || value.Personal.Usage == nil {
		t.Fatal("expired lease claimed enforcement", value)
	}
	refresh()
	failPublication.Store(true)
	disabled := true
	if _, err := svc.UpdateMember(ctx, admin.User.ID, subject.User.ID, &disabled, nil); err == nil {
		t.Fatal("controlled publication failure unexpectedly succeeded")
	}
	failPublication.Store(false)
	var savedSubject entity.User
	var retainedKeys, sessions int64
	if err := db.Take(&savedSubject, "id = ?", subject.User.ID).Error; err != nil || !savedSubject.Disabled {
		t.Fatal("disable did not commit before publication failed", savedSubject, err)
	}
	if err := db.Model(&entity.APIKey{}).Where("user_id = ? AND status = ?", subject.User.ID, entity.KeyRevoked).Count(&retainedKeys).Error; err != nil || retainedKeys != 5 {
		t.Fatal("disable did not retain and revoke all five Personal Keys", retainedKeys, err)
	}
	if err := db.Model(&entity.Session{}).Where("user_id = ?", subject.User.ID).Count(&sessions).Error; err != nil || sessions != 0 {
		t.Fatal("disable retained subject Sessions", sessions, err)
	}
	inactive := get(readerCookie, subject.User.ID)
	if inactive.TotalPersonalKeys != "5" || inactive.Personal.RuntimeApplied || inactive.Personal.Usage.TokensUsed != "10" {
		t.Fatal("inactive retained target borrowed reader authority or lost facts", inactive)
	}
	refresh()
	disabled = false
	if err := db.Model(&entity.User{}).Where("id = ?", subject.User.ID).Update("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if value := get(readerCookie, subject.User.ID); value.Personal.RuntimeApplied {
		t.Fatal("raw enabled subject borrowed published disabled identity", value)
	}
	refresh()
	offboarded := time.Now().UTC()
	if err := db.Model(&entity.User{}).Where("id = ?", subject.User.ID).Update("offboarded_at", offboarded).Error; err != nil {
		t.Fatal(err)
	}
	if value := get(readerCookie, subject.User.ID); value.Personal.RuntimeApplied || value.TotalPersonalKeys != "5" {
		t.Fatal("retained offboarded target hid count or claimed authority", value)
	}
	if err := db.Model(&entity.User{}).Where("id = ?", subject.User.ID).Update("offboarded_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	refresh()
	beforeRestart := get(readerCookie, subject.User.ID)
	var facts []entity.CallRecord
	if err := db.Order("request_id").Find(&facts).Error; err != nil || len(facts) != 8 || posts.Load() != 8 {
		t.Fatal("controlled native calls were replayed/lost", len(facts), posts.Load(), err)
	}
	for index, id := range requestIDs {
		var call entity.CallRecord
		var attempts []entity.CallAttempt
		if err := db.Take(&call, "request_id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		wantUsers := []string{admin.User.ID, subject.User.ID, peer.User.ID, "", subject.User.ID, subject.User.ID, zeroSubject.User.ID, subject.User.ID}
		if call.UserID != wantUsers[index] || call.ModelID != modelID {
			t.Fatal("immutable native subject/Model differs", index, call)
		}
		switch index {
		case 3:
			if call.ProjectID != "prj_card" || call.KeyID != "key_card_project" || call.TeamID != "" || call.TeamMembershipID != "" {
				t.Fatal("Project native call borrowed Personal or Team attribution", call)
			}
		case 4:
			if call.TeamID != "tem_card" || call.TeamMembershipID != "tmm_card_subject" || call.KeyID != "" || call.ProjectID != "" {
				t.Fatal("Team native call lost immutable membership or borrowed a Key", call)
			}
		default:
			if call.KeyID == "" || call.TeamID != "" || call.TeamMembershipID != "" || call.ProjectID != "" {
				t.Fatal("Personal native call borrowed a Team/Project source", call)
			}
		}
		if err := db.Where("request_id = ?", id).Find(&attempts).Error; err != nil || len(attempts) != 1 || attempts[0].CredentialID != "crd_card" || attempts[0].SnapshotID != call.SnapshotID || attempts[0].NativeCompletionEvidence != "completed" {
			t.Fatal("native call lacked actual immutable completed attempt", call, attempts, err)
		}
	}
	svc.StopRuntime()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	if value := get(readerCookie, subject.User.ID); value.Personal.UsageStatus != "unavailable" || value.Personal.Usage != nil || value.Personal.ActiveReservations != nil || value.Personal.RuntimeApplied || value.TotalPersonalKeys != "5" {
		t.Fatal("closed journal invented usage or lost retained count", value)
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
	afterRestart := get(readerCookie, subject.User.ID)
	if afterRestart.TotalPersonalKeys != beforeRestart.TotalPersonalKeys || afterRestart.Personal.PolicyETag != beforeRestart.Personal.PolicyETag || afterRestart.Personal.Usage.TokensUsed != beforeRestart.Personal.Usage.TokensUsed || !reflect.DeepEqual(afterRestart.Personal.Usage.MoneyUsed, beforeRestart.Personal.Usage.MoneyUsed) || afterRestart.Personal.Usage.TokensUnknown != "1" || posts.Load() != 8 {
		t.Fatal("restart reset historical account/count or dispatched", beforeRestart, afterRestart)
	}
	var retained []entity.CallRecord
	if err := db.Order("request_id").Find(&retained).Error; err != nil || !reflect.DeepEqual(facts, retained) {
		t.Fatal("read/restart changed immutable native facts", err)
	}
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, reader.User.ID, nil); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "GET", path(subject.User.ID), "", readerCookie, ""), 403)
	disabled = true
	if _, err := svc.UpdateMember(ctx, admin.User.ID, reader.User.ID, &disabled, nil); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "GET", path(subject.User.ID), "", readerCookie, ""), 401)
	if get(adminCookie, subject.User.ID).TotalPersonalKeys != "5" {
		t.Fatal("authorized administrator read lost retained exact target")
	}
}

func TestMemberOverviewControlledExactPricing(t *testing.T) {
	schedule := pricing.Schedule{Protocol: entity.ProtocolOpenAIChat}
	for _, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		amount := "1000000"
		if metric == pricing.Output {
			amount = "1000000.000000000001"
		}
		schedule.Rates = append(schedule.Rates, pricing.Rate{Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: amount, Enabled: true})
	}
	fx := pricing.FX{PlatformCurrency: "USD"}
	quote, err := pricing.Calculate(schedule, fx, pricing.Usage{InputTokens: 4, OutputTokens: 1})
	if err != nil || quote.Total != "5.000000000000000001" {
		t.Fatal("fixture actual quote differs", quote, err)
	}
	bound, err := pricing.ReserveBound(schedule, fx, pricing.Capacity{Input: 4, Output: 1, CacheRead: true, CacheWrite: true})
	if err != nil || bound.Amount != "5.000000000000000003" {
		t.Fatal("fixture reservation quote differs", bound, err)
	}
}
