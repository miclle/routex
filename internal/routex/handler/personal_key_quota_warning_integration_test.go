package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
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
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	bolt "go.etcd.io/bbolt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The root owns real-driver case registration and warning worker/inbox wiring.
func testPersonalKeyMonthlyQuotaWarningLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var publicationBarrier personalKeyWarningFixturePublicationBarrier
	workerCtx := context.WithValue(ctx, personalKeyWarningFixtureWorkerContext{}, &publicationBarrier)
	const publicationCallback = "test:personal-key-warning-publication-fault"
	if err := db.Callback().Query().Before("gorm:query").Register(publicationCallback, publicationBarrier.beforeQuery); err != nil {
		t.Fatal(err)
	}
	var controlledOperations personalKeyWarningFixtureOperations
	const operationsCallback = "test:personal-key-warning-controlled-operations"
	if err := db.Callback().Create().After("gorm:create").Register(operationsCallback, func(tx *gorm.DB) {
		controlledOperations.afterCreate(tx, &publicationBarrier)
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Callback().Create().Remove(operationsCallback); err != nil {
			t.Error(err)
		}
	}()
	defer func() {
		publicationBarrier.armed.Store(false)
		if err := db.Callback().Query().Remove(publicationCallback); err != nil {
			t.Error(err)
		}
	}()
	store, err := secretstore.New(bytes.Repeat([]byte{97}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var unknown atomic.Bool
	var single atomic.Bool
	var dispatches atomic.Int64
	var hold atomic.Bool
	entered, release := make(chan struct{}, 1), make(chan struct{})
	// Register release before any held request can start, including late entry.
	var releaseOnce sync.Once
	var nativeRequests sync.WaitGroup
	defer releaseOnce.Do(func() { close(release) })
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatches.Add(1)
		if hold.Load() {
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
		_, _ = io.WriteString(w, quotaWarningNativeBody(single.Load(), unknown.Load()))
	}))
	defer func() {
		// This later defer runs first: release before waiting for server handlers.
		releaseOnce.Do(func() { close(release) })
		upstream.Close()
	}()
	makeService := func() *service.Service {
		svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
		if err != nil {
			t.Fatal(err)
		}
		return svc
	}
	svc := makeService()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"pkw-admin@example.invalid","password":"monthly-notification-password","name":"Quota admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	// Capture router by reference so restart exercises the same persisted sessions.
	request := func(cookie *http.Cookie, csrf, method, path string, body any, etag string) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, "http://routex.test"+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://routex.test")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("X-CSRF-Token", csrf)
		req.AddCookie(cookie)
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	adminRequest := func(method, path string, body any, etag string) *httptest.ResponseRecorder {
		return request(adminCookie, admin.CSRFToken, method, path, body, etag)
	}
	cipher, err := store.Seal("crd_pkw", "pkw-upstream-secret")
	if err != nil {
		t.Fatal(err)
	}
	modelID := "mdl_pkw"
	adminBearer := "rx_" + strings.Repeat("a", 43)
	for _, row := range []any{
		&entity.Provider{ID: "prv_pkw", Name: "Quota notification provider"},
		&entity.ProviderConnection{ID: "con_pkw", ProviderID: "prv_pkw", Name: "Native", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_pkw", ConnectionID: "con_pkw", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_pkw", ConnectionID: "con_pkw", UpstreamName: "quota-native"},
		&entity.CredentialModelAccess{CredentialID: "crd_pkw", ProviderModelID: "pmd_pkw"},
		&entity.Model{ID: modelID, Status: "active"}, &entity.ModelName{Name: "pkw-model", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_pkw", ModelID: modelID, ProviderModelID: "pmd_pkw", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_pkw_admin", UserID: admin.User.ID, Name: "Warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(adminBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_pkw_admin", ModelID: modelID},
		&entity.ModelPrice{ID: "price_pkw", ProviderModelID: "pmd_pkw", UpdateSource: "api"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for index, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		if err := db.Create(&entity.PriceRate{ID: fmt.Sprintf("rate_pkw_%d", index), ModelPriceID: "price_pkw", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "1000000", Enabled: true}).Error; err != nil {
			t.Fatal(err)
		}
	}
	spool := filepath.Join(t.TempDir(), "warnings.db")
	if err := svc.StartRuntime(workerCtx); err != nil {
		t.Fatal(err)
	}
	// Keep the actual publisher live: the producer independently rejects closed runtime.done.
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		releaseOnce.Do(func() { close(release) })
		nativeRequests.Wait() // Each request owns its existing bounded context.
		svc.StopRuntime()
		_ = svc.StopCallRecorder()
	}()
	call := func(bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"pkw-model","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	boundPath := "/api/v1/admin/provider-models/pmd_pkw/reservation-bound"
	capacityReview := decodeCatalogResponse[service.ReservationBoundRecord](t, adminRequest("GET", boundPath, nil, ""), 200)
	expectStatus(t, adminRequest("PUT", boundPath, map[string]any{"max_input_tokens": 1, "max_output_tokens": 1, "evidence": "Controlled native response capacity", "reason": "Personal Key warning acceptance"}, capacityReview.ETag), 200)
	expectStatus(t, call(adminBearer), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	// First use changes accounting activation; publish that real generation before
	// creating later resources or asking the observer for current-policy evidence.
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}

	member, err := svc.CreateMember(ctx, admin.User.ID, "pkw-member@example.invalid", "monthly-notification-password", "Original Key owner", "member")
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := svc.CreateMember(ctx, admin.User.ID, "pkw-outsider@example.invalid", "monthly-notification-password", "Other owner", "member")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&member.User, "id = ?", member.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.UserModelGrant{UserID: member.User.ID, ModelID: modelID}).Error; err != nil {
		t.Fatal(err)
	}
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"pkw-member@example.invalid","password":"monthly-notification-password"}`, nil, "")
	memberAuth, memberCookie := readIdentity(t, login)
	memberRequest := func(method, path string, body any, etag string) *httptest.ResponseRecorder {
		return request(memberCookie, memberAuth.CSRFToken, method, path, body, etag)
	}
	refresh := func() {
		t.Helper()
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
	}
	armPublicationFault := func() func(func() error) {
		t.Helper()
		// Arm before the manual publisher: its existing runtime.mu drains any
		// pre-arm worker publication before a SQL scalar can change.
		if err := publicationBarrier.arm(func() error { return svc.RefreshRuntime(ctx) }); err != nil {
			t.Fatal(err)
		}
		before := svc.RuntimeStatus()
		if !before.Ready || before.AuthorizationValidUntil == nil || !time.Now().Before(*before.AuthorizationValidUntil) {
			t.Fatal("publication fault needs the unchanged live lease")
		}
		// Exercise the registered real-driver callback with the same tagged context
		// the periodic worker carries; it must fail, without publishing SQL facts.
		rejected := publicationBarrier.rejected.Load()
		if err := svc.RefreshRuntime(workerCtx); err == nil || publicationBarrier.rejected.Load() <= rejected {
			t.Fatal("tagged publication was not fenced", err)
		}
		refresh()
		if current := svc.RuntimeStatus(); !current.Ready || current.SnapshotID != before.SnapshotID {
			t.Fatal("controlled worker rejection changed the published snapshot")
		}
		return func(restored func() error) {
			t.Helper()
			beforeRestore := svc.RuntimeStatus()
			if !beforeRestore.Ready || beforeRestore.SnapshotID != before.SnapshotID || beforeRestore.AuthorizationValidUntil == nil || !time.Now().Before(*beforeRestore.AuthorizationValidUntil) {
				t.Fatal("fault published another generation or expired the unchanged lease")
			}
			if err := publicationBarrier.release(restored, func() error {
				if err := svc.RefreshRuntime(ctx); err != nil {
					return err
				}
				after := svc.RuntimeStatus()
				if !after.Ready || after.SnapshotID != before.SnapshotID || after.AuthorizationValidUntil == nil || !time.Now().Before(*after.AuthorizationValidUntil) {
					return errors.New("restored configuration changed the reviewed snapshot or lease")
				}
				return nil
			}); err != nil {
				t.Fatal("publication fixture restored-before-release", err)
			}
		}
	}
	flush := func() {
		t.Helper()
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
	}
	reconcileRaw := func() error { return svc.ReconcileMonthlyQuotaNotifications(ctx) }
	reconcile := func() {
		t.Helper()
		if err := publicationBarrier.observePublished(func() error { return svc.RefreshRuntime(ctx) }, reconcileRaw); err != nil {
			t.Fatal(err)
		}
	}
	createKey := func(name string) CreatedKeyResponse {
		t.Helper()
		created := decodeCatalogResponse[CreatedKeyResponse](t, memberRequest("POST", "/api/v1/keys", map[string]any{"name": name, "model_ids": []string{modelID}}, ""), 201)
		expectStatus(t, memberRequest("POST", "/api/v1/keys/"+created.Key.ID+"/confirm", nil, ""), 200)
		return created
	}
	rotate := func(key CreatedKeyResponse) CreatedKeyResponse {
		t.Helper()
		next := decodeCatalogResponse[CreatedKeyResponse](t, memberRequest("POST", "/api/v1/keys/"+key.Key.ID+"/rotate", nil, ""), 201)
		expectStatus(t, memberRequest("POST", "/api/v1/keys/"+next.Key.ID+"/confirm", nil, ""), 200)
		return next
	}
	pathFor := func(id string) string { return "/api/v1/keys/" + id + "/limits" }
	write := func(keyID string, values map[string]any) entity.ResourceLimit {
		t.Helper()
		path := pathFor(keyID)
		current := decodeCatalogResponse[service.LimitRecord](t, memberRequest("GET", path, nil, ""), 200)
		record := decodeCatalogResponse[service.LimitRecord](t, memberRequest("PUT", path, personalKeyWarningFixturePolicy(values), current.ETag), 200)
		if !record.Enforced {
			t.Fatal("reviewed root policy was not published")
		}
		var row entity.ResourceLimit
		if err := db.Where("scope_kind = ? AND scope_id = ?", "key", strings.TrimPrefix(record.AccountID, "key_")).Take(&row).Error; err != nil || row.ScopeKind != "key" || "key_"+row.ScopeID != record.AccountID {
			t.Fatal("exact root policy identity", err)
		}
		return row
	}
	badObservationIDs := []string{}
	count := func(root string) int64 {
		t.Helper()
		var n int64
		query := db.Model(&entity.PersonalKeyQuotaWarningObservation{}).Where(database.ExactText(db, clause.Column{Name: "root_key_id"}, root))
		// Only the seven exact, independently rejected fixture receipts are excluded.
		// Every genuine observation remains in this authoritative count.
		if len(badObservationIDs) > 0 {
			query = query.Where("id NOT IN ?", badObservationIDs)
		}
		if err := query.Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	assertCount := func(root string, want int64) {
		t.Helper()
		if got := count(root); got != want {
			t.Fatalf("root warning count %d want%d", got, want)
		}
	}
	assertRecord := func(item service.NotificationRecord) {
		t.Helper()
		q := item.QuotaWarning
		if q == nil || q.ScopeKind != "personal_key" {
			return
		}
		var observation entity.PersonalKeyQuotaWarningObservation
		var inbox entity.PersonalKeyQuotaWarningInbox
		if err := db.Take(&observation, "id = ?", item.QuotaWarningObservationID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Take(&inbox, "id = ?", item.ID).Error; err != nil {
			t.Fatal(err)
		}
		if inbox.ID != item.ID || inbox.ObservationID != observation.ID || inbox.RecipientID != member.User.ID || !inbox.RecipientCreatedAt.Equal(member.User.CreatedAt) || observation.OwnerID != member.User.ID || !observation.OwnerCreatedAt.Equal(member.User.CreatedAt) {
			t.Fatal("merged warning borrowed another receipt/owner incarnation")
		}
		if q.ScopeID != observation.RootKeyID || q.Dimension != observation.Dimension || q.PolicyRevision != observation.PolicyRevision || !q.MonthStart.Equal(observation.MonthStart) || !q.MonthEnd.Equal(observation.MonthEnd) || q.TimeZone != observation.TimeZone || !q.AsOf.Equal(observation.AsOf) || q.Limit != observation.Limit || q.Settled != observation.Settled || q.Level != observation.Level || q.Threshold != observation.Threshold || q.ThresholdGeneration != observation.ThresholdGeneration || q.ThresholdGeneration != "personal-key-monthly-80-90-v1" {
			t.Fatal("merged warning changed one of the fourteen immutable fields")
		}
		if (q.Dimension == "tokens" && (q.Currency != nil || observation.Currency != "")) || (q.Dimension == "money" && (q.Currency == nil || *q.Currency != observation.Currency || observation.Currency != "USD")) {
			t.Fatal("warning currency differs from settled snapshot")
		}
		location, err := time.LoadLocation(q.TimeZone)
		if err != nil {
			t.Fatal(err)
		}
		local := q.AsOf.In(location)
		start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
		if !q.MonthStart.Equal(start) || !q.MonthEnd.Equal(start.AddDate(0, 1, 0)) || q.AsOf.Before(q.MonthStart) || !q.AsOf.Before(q.MonthEnd) || observation.CoverageStart.After(observation.ResourceCreatedAt) || observation.ResourceCreatedAt.After(q.AsOf) {
			t.Fatal("authoritative calendar/coverage differs")
		}
		limit, validLimit := new(big.Rat).SetString(q.Limit)
		settled, validSettled := new(big.Rat).SetString(q.Settled)
		if !validLimit || !validSettled || limit.Sign() <= 0 || settled.Sign() < 0 {
			t.Fatal("non-exact warning amount")
		}
		percentage := new(big.Rat).Quo(new(big.Rat).Mul(settled, big.NewRat(100, 1)), limit)
		severity := "medium"
		switch q.Level {
		case "near":
			if q.Threshold != 80 || percentage.Cmp(big.NewRat(80, 1)) < 0 || percentage.Cmp(big.NewRat(90, 1)) >= 0 {
				t.Fatal("near outside80/90")
			}
		case "critical":
			severity = "high"
			if q.Threshold != 90 || percentage.Cmp(big.NewRat(90, 1)) < 0 || percentage.Cmp(big.NewRat(100, 1)) >= 0 {
				t.Fatal("critical outside90/100")
			}
		default:
			t.Fatal("unknown warning level")
		}
		if item.Kind != "monthly_quota_warning" || item.SubjectType != "personal_key" || item.SubjectID != observation.RootKeyID || item.SubjectName != observation.RootKeyName || item.Severity != severity || item.DetailCode != q.Dimension+"_month_"+q.Level || item.OccurrenceCount != 1 || item.Quota != nil || item.QuotaObservationID != "" || item.AlertID != "" || item.DeliveryStatus != "" || item.DeliveryCode != "" || item.DeliveryAttempts != 0 || item.DeliveryUpdatedAt != nil || !item.FirstSeenAt.Equal(observation.AsOf) || !item.LastSeenAt.Equal(inbox.CreatedAt) || item.Read != (inbox.ReadAt != nil) || (item.ReadAt == nil) != (inbox.ReadAt == nil) {
			t.Fatal("warning identity/read/no-SMTP envelope differs")
		}
		if item.ReadAt != nil && !item.ReadAt.Equal(*inbox.ReadAt) {
			t.Fatal("warning read instant changed")
		}
	}
	page := func() service.NotificationPage {
		t.Helper()
		result := decodeCatalogResponse[service.NotificationPage](t, memberRequest("GET", "/api/v1/notifications?status=all", nil, ""), 200)
		for _, item := range result.Items {
			assertRecord(item)
		}
		return result
	}
	onlyRoot := func(root string) []service.NotificationRecord {
		t.Helper()
		items := []service.NotificationRecord{}
		for _, item := range page().Items {
			if item.QuotaWarning != nil && item.QuotaWarning.ScopeKind == "personal_key" && item.QuotaWarning.ScopeID == root {
				items = append(items, item)
			}
		}
		return items
	}
	originalKey := createKey("Original rotation root")
	rootID := originalKey.Key.ID
	var rootRow entity.APIKey
	if err := db.Take(&rootRow, "id = ?", rootID).Error; err != nil {
		t.Fatal(err)
	}
	if !rootRow.CreatedAt.After(member.User.CreatedAt) {
		t.Fatal("root birth must follow actual owner and warmup coverage")
	}
	policy := write(rootID, map[string]any{"tokens_month": 10})
	reconcile()
	assertCount(rootID, 0)
	for range 3 {
		expectStatus(t, call(originalKey.Secret), 200)
		flush()
		reconcile()
		assertCount(rootID, 0)
	}
	expectStatus(t, call(originalKey.Secret), 200)
	flush()
	reconcile()
	assertCount(rootID, 1)
	nearItems := onlyRoot(rootID)
	if len(nearItems) != 1 {
		t.Fatal("sole-root near absent")
	}
	near := nearItems[0]
	q := near.QuotaWarning
	if q == nil || q.ScopeKind != "personal_key" || q.ScopeID != rootID || q.PolicyRevision != policy.ETag || q.Level != "near" || q.Threshold != 80 || q.Settled != "8" || q.Limit != "10" || q.Currency != nil || q.ThresholdGeneration != "personal-key-monthly-80-90-v1" || near.SubjectType != "personal_key" || near.SubjectID != rootID || near.SubjectName != "Original rotation root" || near.Severity != "medium" || near.OccurrenceCount != 1 || near.Quota != nil || near.AlertID != "" || !strings.HasPrefix(near.ID, "kwi_") || !strings.HasPrefix(near.QuotaWarningObservationID, "kwo_") {
		t.Fatal("exact root80 snapshot mismatch")
	}
	marked := decodeCatalogResponse[service.NotificationRecord](t, memberRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil, ""), 200)
	if !marked.Read || marked.ReadAt == nil {
		t.Fatal("root read missing")
	}
	var original entity.PersonalKeyQuotaWarningObservation
	var receipt entity.PersonalKeyQuotaWarningInbox
	if err := db.Take(&original, "id = ?", near.QuotaWarningObservationID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&receipt, "id = ?", near.ID).Error; err != nil {
		t.Fatal(err)
	}
	if original.RootKeyID != rootID || original.OwnerID != member.User.ID || !original.OwnerCreatedAt.Equal(member.User.CreatedAt) || !original.ResourceCreatedAt.Equal(rootRow.CreatedAt) || receipt.RecipientID != member.User.ID || !receipt.RecipientCreatedAt.Equal(member.User.CreatedAt) {
		t.Fatal("private exact root/owner birth binding absent")
	}
	// Supported rotations retain one original quota account even after root revocation.
	replacement := rotate(originalKey)
	expectStatus(t, memberRequest("DELETE", "/api/v1/keys/"+rootID, nil, ""), 204)
	single.Store(true)
	expectStatus(t, call(replacement.Secret), 200)
	single.Store(false)
	flush()
	reconcile()
	assertCount(rootID, 2)
	grandchild := rotate(replacement)
	expectStatus(t, memberRequest("DELETE", "/api/v1/keys/"+replacement.Key.ID, nil, ""), 204)
	reconcile()
	assertCount(rootID, 2)
	current := decodeCatalogResponse[service.LimitRecord](t, memberRequest("GET", pathFor(grandchild.Key.ID), nil, ""), 200)
	if current.AccountID != "key_"+rootID || current.QuotaUsage == nil || current.QuotaUsage.Month == nil || !current.QuotaUsage.Month.Covered || current.QuotaUsage.Month.TokensUsed != 9 || current.QuotaUsage.Month.MoneyUsed["USD"] != "9" {
		t.Fatal("multi-step rotation reset original journal account")
	}
	criticalFound := false
	for _, item := range onlyRoot(rootID) {
		if item.ID == near.ID {
			if !item.Read || item.ReadAt == nil || !item.ReadAt.Equal(*receipt.ReadAt) || !reflect.DeepEqual(item.QuotaWarning, near.QuotaWarning) {
				t.Fatal("rotation changed near history/read")
			}
		} else if item.QuotaWarning.Level == "critical" && item.QuotaWarning.Settled == "9" && item.QuotaWarning.Threshold == 90 && item.Severity == "high" {
			criticalFound = true
		} else {
			t.Fatal("unexpected critical snapshot")
		}
	}
	if !criticalFound {
		t.Fatal("root90 escalation absent")
	}
	// Parent defaults and local descendant storage cannot become root warning definitions.
	parentPath := "/api/v1/admin/members/" + member.User.ID + "/limits"
	parentCurrent := decodeCatalogResponse[service.LimitRecord](t, adminRequest("GET", parentPath, nil, ""), 200)
	expectStatus(t, adminRequest("PUT", parentPath, personalKeyWarningFixturePolicy(map[string]any{"tokens_month": 100}), parentCurrent.ETag), 200)
	reconcile()
	assertCount(rootID, 2)
	descendantPolicy := entity.ResourceLimit{ScopeKind: "key", ScopeID: grandchild.Key.ID, ETag: "lim_pkw_descendant", ActorID: member.User.ID, Reason: "Negative descendant definition", TokensMonth: personalKeyWarningInt64(10), IPMode: "none", IPRangesJSON: "[]"}
	if err := db.Create(&descendantPolicy).Error; err != nil {
		t.Fatal(err)
	}
	reconcile()
	assertCount(grandchild.Key.ID, 0)
	assertCount(rootID, 2)
	if err := db.Delete(&descendantPolicy).Error; err != nil {
		t.Fatal(err)
	}
	refresh()
	moneyNear := write(grandchild.Key.ID, map[string]any{"money_month": "11.25"})
	reconcile()
	assertCount(rootID, 3)
	moneyCritical := write(grandchild.Key.ID, map[string]any{"money_month": "10"})
	reconcile()
	assertCount(rootID, 4)
	precise := write(grandchild.Key.ID, map[string]any{"money_month": "10.000000000000000001"})
	reconcile()
	assertCount(rootID, 5)
	found := map[string]bool{}
	for _, item := range onlyRoot(rootID) {
		v := item.QuotaWarning
		if v.Dimension != "money" {
			continue
		}
		if v.Currency == nil || *v.Currency != "USD" || v.Settled != "9" {
			t.Fatal("exact root money settlement lost")
		}
		switch v.PolicyRevision {
		case moneyNear.ETag:
			if v.Level != "near" || v.Limit != "11.25" {
				t.Fatal("money80")
			}
			found["near"] = true
		case moneyCritical.ETag:
			if v.Level != "critical" || v.Limit != "10" {
				t.Fatal("money90")
			}
			found["critical"] = true
		case precise.ETag:
			if v.Level != "near" || v.Limit != "10.000000000000000001" {
				t.Fatal("18-place money rounded")
			}
			found["precise"] = true
		}
	}
	if len(found) != 3 {
		t.Fatal("independent exact money observations missing")
	}
	candidate := write(grandchild.Key.ID, map[string]any{"money_month": "11"})
	// Current SQL revisions/calendar/currency must agree with the exact published generation.
	for _, fault := range []struct {
		model     any
		where     string
		args      []any
		field     string
		bad, good any
	}{
		{&entity.ResourceLimit{}, "scope_kind = ? AND scope_id = ?", []any{"key", rootID}, "ETag", "lim_pkw_unpublished", candidate.ETag},
		{&entity.QuotaSetting{}, "id = ?", []any{1}, "ETag", "calendar_pkw_unpublished", nil},
		{&entity.PricingSetting{}, "id = ?", []any{1}, "platform_currency", "EUR", "USD"},
	} {
		if fault.good == nil {
			var before entity.QuotaSetting
			if err := db.Take(&before, "id = ?", 1).Error; err != nil {
				t.Fatal(err)
			}
			fault.good = before.ETag
		}
		endPublicationFault := armPublicationFault()
		if err := db.Model(fault.model).Where(fault.where, fault.args...).UpdateColumn(fault.field, fault.bad).Error; err != nil {
			t.Fatal(err)
		}
		readScalar := func() string {
			var got string
			switch fault.model.(type) {
			case *entity.ResourceLimit:
				var stored entity.ResourceLimit
				if err := db.Where(fault.where, fault.args...).Take(&stored).Error; err != nil {
					t.Fatal(err)
				}
				got = stored.ETag
			case *entity.QuotaSetting:
				var stored entity.QuotaSetting
				if err := db.Where(fault.where, fault.args...).Take(&stored).Error; err != nil {
					t.Fatal(err)
				}
				got = stored.ETag
			case *entity.PricingSetting:
				var stored entity.PricingSetting
				if err := db.Where(fault.where, fault.args...).Take(&stored).Error; err != nil {
					t.Fatal(err)
				}
				got = stored.PlatformCurrency
			}
			return got
		}
		if got := readScalar(); got != fault.bad {
			t.Fatal("fault scalar persistence", got, fault.bad)
		}
		if err := reconcileRaw(); err != nil {
			t.Fatal(err)
		}
		assertCount(rootID, 5)
		if err := db.Model(fault.model).Where(fault.where, fault.args...).UpdateColumn(fault.field, fault.good).Error; err != nil {
			t.Fatal(err)
		}
		endPublicationFault(func() error {
			if got := readScalar(); got != fault.good {
				return errors.New("SQL scalar was not exactly restored")
			}
			return nil
		})
	}
	reconcile()
	assertCount(rootID, 6)
	// Renaming or revoking all current descendants hides no retained historical record.
	if _, err := svc.UpdatePersonalKey(ctx, member.User.ID, grandchild.Key.ID, personalKeyWarningString("Renamed current successor"), nil); err != nil {
		t.Fatal(err)
	}
	beforeHistory := onlyRoot(rootID)
	expectStatus(t, memberRequest("PATCH", "/api/v1/keys/"+grandchild.Key.ID, map[string]any{"enabled": false}, ""), 200)
	reconcile()
	assertCount(rootID, 6)
	if !reflect.DeepEqual(onlyRoot(rootID), beforeHistory) {
		t.Fatal("disabled root family erased history")
	}
	expectStatus(t, memberRequest("PATCH", "/api/v1/keys/"+grandchild.Key.ID, map[string]any{"enabled": true}, ""), 200)
	reconcile()
	assertCount(rootID, 6)
	// A reached cap remains the existing hard stop and does not create a warning.
	write(grandchild.Key.ID, map[string]any{"tokens_month": 9})
	reconcile()
	assertCount(rootID, 6)
	denial := call(grandchild.Secret)
	expectStatus(t, denial, 429)
	flush()
	var denied entity.CallRecord
	if err := db.Take(&denied, "request_id = ?", denial.Header().Get("X-Request-ID")).Error; err != nil || denied.ErrorCode != "quota_exceeded" || denied.KeyID != grandchild.Key.ID || denied.UserID != member.User.ID || denied.ProjectID != "" || denied.TeamID != "" {
		t.Fatal("root hardstop durable attribution", err)
	}
	var deniedAttempts int64
	if err := db.Model(&entity.CallAttempt{}).Where("request_id = ?", denied.RequestID).Count(&deniedAttempts).Error; err != nil || deniedAttempts != 0 || dispatches.Load() != 6 {
		t.Fatal("hardstop dispatched native", err)
	}
	write(grandchild.Key.ID, map[string]any{"tokens_month": 0})
	reconcile()
	assertCount(rootID, 6)
	write(grandchild.Key.ID, map[string]any{})
	reconcile()
	assertCount(rootID, 6)
	// Sole current original owner: administrator and another owner have no recipient override.
	for _, person := range []*service.MemberRecord{outsider} {
		raw, _ := json.Marshal(map[string]any{"email": person.User.Email, "password": "monthly-notification-password"})
		auth, cookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", string(raw), nil, ""))
		other := decodeCatalogResponse[service.NotificationPage](t, request(cookie, auth.CSRFToken, "GET", "/api/v1/notifications?status=all", nil, ""), 200)
		for _, item := range other.Items {
			if item.SubjectID == rootID {
				t.Fatal("other owner inherited original root history")
			}
		}
		expectStatus(t, request(cookie, auth.CSRFToken, "POST", "/api/v1/notifications/"+near.ID+"/read", nil, ""), 404)
		expectStatus(t, request(cookie, auth.CSRFToken, "POST", "/api/v1/notifications/read-all", nil, ""), 204)
	}
	expectStatus(t, adminRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil, ""), 404)
	adminPage := decodeCatalogResponse[service.NotificationPage](t, adminRequest("GET", "/api/v1/notifications?status=all", nil, ""), 200)
	for _, item := range adminPage.Items {
		if item.SubjectID == rootID {
			t.Fatal("administrator root history override")
		}
	}
	if len(near.ID) != 30 {
		t.Fatal("generated warning inbox ID must retain the 30-byte request boundary")
	}
	var aliasBefore entity.PersonalKeyQuotaWarningInbox
	if err := db.Take(&aliasBefore, "id = ?", near.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, alias := range personalKeyWarningFixtureAliasCases(near.ID) {
		expectStatus(t, memberRequest("POST", "/api/v1/notifications/"+url.PathEscape(alias.id)+"/read", nil, ""), alias.status)
		var aliasAfter entity.PersonalKeyQuotaWarningInbox
		if err := db.Take(&aliasAfter, "id = ?", near.ID).Error; err != nil {
			t.Fatal(err)
		}
		if (aliasBefore.ReadAt == nil) != (aliasAfter.ReadAt == nil) || aliasBefore.ReadAt != nil && !aliasBefore.ReadAt.Equal(*aliasAfter.ReadAt) {
			t.Fatal("rejected alias changed the original warning inbox read state")
		}
		aliasAfter.ReadAt = aliasBefore.ReadAt
		if !reflect.DeepEqual(aliasBefore, aliasAfter) {
			t.Fatal("rejected alias changed the original warning inbox")
		}
	}
	// Current root owner/birth is a history gate; root status is intentionally not.
	for _, fault := range []struct {
		model     any
		id, field string
		bad, good any
	}{
		{&entity.APIKey{}, rootID, "user_id", outsider.User.ID, member.User.ID},
		{&entity.APIKey{}, rootID, "created_at", rootRow.CreatedAt.Add(time.Millisecond), rootRow.CreatedAt},
		{&entity.User{}, member.User.ID, "created_at", member.User.CreatedAt.Add(time.Millisecond), member.User.CreatedAt},
	} {
		endPublicationFault := armPublicationFault()
		if err := db.Model(fault.model).Where("id = ?", fault.id).UpdateColumn(fault.field, fault.bad).Error; err != nil {
			t.Fatal(err)
		}
		if fault.field == "created_at" {
			var birth time.Time
			switch fault.model.(type) {
			case *entity.APIKey:
				var row entity.APIKey
				if err := db.Take(&row, "id = ?", fault.id).Error; err != nil {
					t.Fatal(err)
				}
				birth = row.CreatedAt
			case *entity.User:
				var row entity.User
				if err := db.Take(&row, "id = ?", fault.id).Error; err != nil {
					t.Fatal(err)
				}
				birth = row.CreatedAt
			}
			if !birth.Equal(fault.bad.(time.Time)) || birth.Equal(fault.good.(time.Time)) {
				t.Fatal("portable millisecond incarnation fault was not persisted")
			}
		}
		if len(onlyRoot(rootID)) != 0 {
			t.Fatal("changed original ownership/birth inherited history")
		}
		expectStatus(t, memberRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil, ""), 404)
		expectStatus(t, memberRequest("POST", "/api/v1/notifications/read-all", nil, ""), 204)
		var retained entity.PersonalKeyQuotaWarningInbox
		if err := db.Take(&retained, "id = ?", receipt.ID).Error; err != nil || !personalKeyWarningInboxEqual(retained, receipt) {
			t.Fatal("hidden read-all changed receipt", err)
		}
		if err := db.Model(fault.model).Where("id = ?", fault.id).UpdateColumn(fault.field, fault.good).Error; err != nil {
			t.Fatal(err)
		}
		endPublicationFault(func() error {
			switch fault.model.(type) {
			case *entity.APIKey:
				var row entity.APIKey
				if err := db.Take(&row, "id = ?", fault.id).Error; err != nil {
					return err
				}
				if (fault.field == "user_id" && row.UserID != fault.good) || (fault.field == "created_at" && !row.CreatedAt.Equal(fault.good.(time.Time))) {
					return errors.New("original Key owner/birth was not exactly restored")
				}
			case *entity.User:
				var row entity.User
				if err := db.Take(&row, "id = ?", fault.id).Error; err != nil {
					return err
				}
				if !row.CreatedAt.Equal(fault.good.(time.Time)) {
					return errors.New("original User birth was not exactly restored")
				}
			default:
				return errors.New("unknown fixture restoration target")
			}
			return nil
		})
	}
	if !reflect.DeepEqual(onlyRoot(rootID), beforeHistory) {
		t.Fatal("restored births changed immutable history")
	}
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).UpdateColumn("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, memberRequest("GET", "/api/v1/notifications?status=all", nil, ""), 401)
	if err := reconcileRaw(); err != nil {
		t.Fatal(err)
	}
	assertCount(rootID, 6)
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).UpdateColumn("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	refresh()
	// Exact aliases/private stored proofs must not pollute merged paging or read-all.
	badIDs := []string{}
	for i, kind := range []string{"root_alias", "owner_alias", "root_birth", "owner_birth", "recipient_birth", "recipient_alias", "join_alias"} {
		bad := original
		bad.ID = fmt.Sprintf("kwo_bad_%d", i)
		bad.PolicyRevision = fmt.Sprintf("lim_bad_%d", i)
		recipient, join, birth := member.User.ID, bad.ID, member.User.CreatedAt
		switch kind {
		case "root_alias":
			bad.RootKeyID = strings.ToUpper(rootID)
		case "owner_alias":
			bad.OwnerID = strings.ToUpper(member.User.ID)
		case "root_birth":
			bad.ResourceCreatedAt = bad.ResourceCreatedAt.Add(-time.Millisecond)
		case "owner_birth":
			bad.OwnerCreatedAt = bad.OwnerCreatedAt.Add(-time.Millisecond)
		case "recipient_birth":
			birth = birth.Add(-time.Millisecond)
		case "recipient_alias":
			recipient = strings.ToUpper(recipient)
		case "join_alias":
			join = strings.ToUpper(join)
		}
		if err := db.Create(&bad).Error; err != nil {
			t.Fatal(err)
		}
		badObservationIDs = append(badObservationIDs, bad.ID)
		id := fmt.Sprintf("kwi_bad_%d", i)
		badIDs = append(badIDs, id)
		if err := db.Create(&entity.PersonalKeyQuotaWarningInbox{ID: id, ObservationID: join, RecipientID: recipient, RecipientCreatedAt: birth, CreatedAt: bad.AsOf}).Error; err != nil {
			t.Fatal(err)
		}
		expectStatus(t, memberRequest("POST", "/api/v1/notifications/"+id+"/read", nil, ""), 404)
	}
	if !reflect.DeepEqual(onlyRoot(rootID), beforeHistory) {
		t.Fatal("private aliases polluted root history")
	}
	for _, item := range page().Items {
		if item.QuotaWarning == nil || item.QuotaWarning.ScopeKind != "personal_key" {
			continue
		}
		raw, _ := json.Marshal(item)
		for _, private := range []string{"token_hash", "prefix", "owner_id", "owner_created_at", "recipient_created_at", "resource_created_at", "coverage_start", "replaces_key_id"} {
			if strings.Contains(string(raw), `"`+private+`"`) {
				t.Fatal("private Key proof leaked", private)
			}
		}
	}
	// Traverse the real merged cursor without duplicates or a skipped warning row.
	seen := map[string]bool{}
	cursor := ""
	rootRecords := 0
	for pages := 0; pages < 30; pages++ {
		path := "/api/v1/notifications?status=all&limit=2"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		batch := decodeCatalogResponse[service.NotificationPage](t, memberRequest("GET", path, nil, ""), 200)
		for _, item := range batch.Items {
			assertRecord(item)
			if seen[item.ID] {
				t.Fatal("duplicate merged cursor row")
			}
			seen[item.ID] = true
			if item.QuotaWarning != nil && item.QuotaWarning.ScopeKind == "personal_key" && item.QuotaWarning.ScopeID == rootID {
				rootRecords++
			}
		}
		if batch.NextCursor == "" {
			break
		}
		if batch.NextCursor == cursor || pages == 29 {
			t.Fatal("cursor did not terminate")
		}
		cursor = batch.NextCursor
	}
	if rootRecords != len(beforeHistory) {
		t.Fatal("merged cursor skipped original root warnings")
	}
	expectStatus(t, memberRequest("POST", "/api/v1/notifications/read-all", nil, ""), 204)
	if page().UnreadCount != 0 {
		t.Fatal("read-all omitted visible merged records")
	}
	for _, id := range badIDs {
		var row entity.PersonalKeyQuotaWarningInbox
		if err := db.Take(&row, "id = ?", id).Error; err != nil || row.ReadAt != nil {
			t.Fatal("read-all changed hidden private receipt", err)
		}
	}
	// Each independent root first settles8 then has an active/unknown reservation;
	// a percentage inferred from known subtotals must never become a warning.
	heldKey := createKey("Held root")
	heldID := heldKey.Key.ID
	write(heldID, map[string]any{"tokens_month": 10})
	for range 4 {
		expectStatus(t, call(heldKey.Secret), 200)
		flush()
	}
	hold.Store(true)
	single.Store(true)
	heldResponse := make(chan *httptest.ResponseRecorder, 1)
	nativeRequests.Add(1)
	go func() {
		defer nativeRequests.Done()
		callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		req := httptest.NewRequestWithContext(callCtx, "POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"pkw-model","messages":[{"role":"user","content":"held"}],"max_completion_tokens":1}`))
		req.Header.Set("Authorization", "Bearer "+heldKey.Secret)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		heldResponse <- res
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("held native timeout")
	}
	reconcile()
	assertCount(heldID, 0)
	heldUsage := decodeCatalogResponse[service.LimitRecord](t, memberRequest("GET", pathFor(heldID), nil, ""), 200).QuotaUsage
	if heldUsage == nil || heldUsage.Month == nil || heldUsage.Month.TokensUsed != 8 || heldUsage.Active == nil || heldUsage.Active.TokensHeld != 2 {
		t.Fatal("settled8 plus active hold became a warning")
	}
	releaseOnce.Do(func() { close(release) })
	hold.Store(false)
	select {
	case response := <-heldResponse:
		expectStatus(t, response, 200)
	case <-time.After(15 * time.Second):
		t.Fatal("held request did not finish")
	}
	single.Store(false)
	flush()
	reconcile()
	assertCount(heldID, 1)
	unknownKey := createKey("Unknown root")
	unknownID := unknownKey.Key.ID
	write(unknownID, map[string]any{"tokens_month": 10})
	for range 4 {
		expectStatus(t, call(unknownKey.Secret), 200)
		flush()
	}
	unknown.Store(true)
	expectStatus(t, call(unknownKey.Secret), 200)
	unknown.Store(false)
	flush()
	write(unknownID, map[string]any{"tokens_month": 10, "money_month": "10"})
	reconcile()
	assertCount(unknownID, 0)
	unknownUsage := decodeCatalogResponse[service.LimitRecord](t, memberRequest("GET", pathFor(unknownID), nil, ""), 200).QuotaUsage
	if unknownUsage == nil || unknownUsage.Month == nil || unknownUsage.Month.TokensUsed != 8 || unknownUsage.Month.TokensHeld != 2 || unknownUsage.Month.TokensUnknown != 0 || unknownUsage.Month.MoneyUsed["USD"] != "8" || len(unknownUsage.Month.MoneyHeld) != 0 || unknownUsage.Month.MoneyUnknown != 1 {
		t.Fatal("unknown terminal usage fabricated percentage/money bound")
	}
	// Complete owner graph supports10000 retained rows and explicitly rejects10001.
	var retainedKeys []entity.APIKey
	if err := db.Where("user_id = ?", member.User.ID).Order("id").Find(&retainedKeys).Error; err != nil {
		t.Fatal(err)
	}
	filler := []entity.APIKey{}
	fillerIDs := []string{}
	for i := len(retainedKeys); i < 10000; i++ {
		id := fmt.Sprintf("key_%026d", i)
		fillerIDs = append(fillerIDs, id)
		filler = append(filler, entity.APIKey{ID: id, UserID: member.User.ID, Name: "Retained pending", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(id), Status: entity.KeyPending, LifecycleRevision: fmt.Sprintf("kvr_%026d", i)})
	}
	if err := db.CreateInBatches(filler, 500).Error; err != nil {
		t.Fatal(err)
	}
	write(grandchild.Key.ID, map[string]any{"tokens_month": 10, "rpm": 100})
	reconcile()
	// The exact10000-row graph adds one genuine observation; malformed fixture rows stay excluded only from this projection.
	assertCount(rootID, 7)
	extra := entity.APIKey{ID: "key_pkw_overflow", UserID: member.User.ID, Name: "Overflow", Prefix: "rx_masked", TokenHash: secret.SHA256Hex("pkw-overflow"), Status: entity.KeyPending, LifecycleRevision: "kvr_pkw_overflow"}
	if err := db.Create(&extra).Error; err != nil {
		t.Fatal(err)
	}
	if err := reconcileRaw(); err == nil {
		t.Fatal("10001-row graph silently truncated")
	}
	assertCount(rootID, 7)
	if err := db.Delete(&extra).Error; err != nil {
		t.Fatal(err)
	}
	for offset := 0; offset < len(fillerIDs); offset += 500 {
		end := min(offset+500, len(fillerIDs))
		if err := db.Where("id IN ?", fillerIDs[offset:end]).Delete(&entity.APIKey{}).Error; err != nil {
			t.Fatal(err)
		}
	}
	refresh()
	// The original journal registration is read together with settled/calendar facts.
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	proofQueue, err := eventqueue.Open(spool, 4096, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	proofs, err := proofQueue.AccountQuotaUsageProofBatch([]string{"key_" + rootID, "key_missing_pkw"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	proof := proofs.Accounts["key_"+rootID]
	if !proofs.Active || !proof.Registered || proof.CreatedAt.IsZero() || !proof.CreatedAt.Equal(rootRow.CreatedAt) || proof.Usage.Month.TokensUsed != 9 || !proof.Usage.AsOf.Equal(proofs.AsOf) || proofs.Accounts["key_missing_pkw"].Registered {
		t.Fatal("real SQL/journal coherent original birth proof absent")
	}
	if err := proofQueue.Close(); err != nil {
		t.Fatal(err)
	}
	mutateBirth := func(raw []byte) []byte {
		t.Helper()
		journal, err := bolt.Open(spool, 0600, &bolt.Options{Timeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		var old []byte
		err = journal.Update(func(tx *bolt.Tx) error {
			bucket := tx.Bucket([]byte("quota-accounts-v2"))
			if bucket == nil {
				return eventqueue.ErrInvalid
			}
			old = append([]byte(nil), bucket.Get([]byte("key_"+rootID))...)
			if raw == nil {
				return bucket.Delete([]byte("key_" + rootID))
			}
			return bucket.Put([]byte("key_"+rootID), raw)
		})
		closeErr := journal.Close()
		if err != nil || closeErr != nil {
			t.Fatal("private journal birth fixture", err, closeErr)
		}
		return old
	}
	// StopCallRecorder closes its recorder without making the Service reusable.
	// Recreate the process-equivalent Service/router while retaining the original Session.
	openRecorder := func() error {
		svc.StopRuntime()
		svc = makeService()
		router = fox.New()
		New(svc).RegisterRoutes(router)
		if err := svc.StartRuntime(workerCtx); err != nil {
			return err
		}
		return svc.StartCallRecorder(ctx, spool)
	}
	if err := openRecorder(); err != nil {
		t.Fatal(err)
	}
	birthMismatch, err := json.Marshal(rootRow.CreatedAt.Add(-time.Millisecond).UnixNano())
	if err != nil {
		t.Fatal(err)
	}
	for i, fault := range []struct {
		name         string
		raw          []byte
		startupFails bool
	}{{"missing", nil, true}, {"legacy-zero", []byte("0"), false}, {"different-birth", birthMismatch, false}, {"corrupt", []byte(`"not-a-birth"`), true}} {
		// Each fault gets a new, eligible unpublished observation identity: dedup
		// cannot hide a failure to reject missing or mismatched registration proof.
		write(grandchild.Key.ID, map[string]any{"tokens_month": 10, "rpm": 200 + i})
		if err := svc.StopCallRecorder(); err != nil {
			t.Fatal(err)
		}
		savedRaw := mutateBirth(fault.raw)
		err := openRecorder()
		if fault.startupFails {
			if err == nil {
				t.Fatal("corrupt/missing registered settled account opened", fault.name)
			}
		} else {
			if err != nil {
				t.Fatal("valid but unproven birth startup", fault.name, err)
			}
			if err := reconcileRaw(); err != nil {
				t.Fatal(err)
			}
			if err := svc.StopCallRecorder(); err != nil {
				t.Fatal(err)
			}
		}
		assertCount(rootID, int64(7+i))
		mutateBirth(savedRaw)
		if err := openRecorder(); err != nil {
			t.Fatal("restored exact registration", err)
		}
		reconcile()
		assertCount(rootID, int64(8+i))
	}
	assertCount(rootID, 11)
	assertCount(heldID, 1)
	assertCount(unknownID, 0)
	for _, model := range []any{&entity.PersonalKeyQuotaWarningObservation{}, &entity.PersonalKeyQuotaWarningInbox{}} {
		var rawCount int64
		if err := db.Model(model).Count(&rawCount).Error; err != nil || rawCount != 19 {
			t.Fatal("raw warning history must retain twelve genuine plus seven rejected fixture rows", err, rawCount)
		}
	}
	var originalAfter entity.PersonalKeyQuotaWarningObservation
	if err := db.Take(&originalAfter, "id = ?", original.ID).Error; err != nil || !personalKeyWarningObservationEqual(originalAfter, original) {
		t.Fatal("original warning overwritten", err)
	}
	// Exact native16 / durable denial1, with immutable attempt completion attribution.
	var calls []entity.CallRecord
	if err := db.Where("user_id IN ?", []string{admin.User.ID, member.User.ID}).Order("request_id").Find(&calls).Error; err != nil || len(calls) != 17 || dispatches.Load() != 16 {
		t.Fatal("exact native16/durable17 count", err, len(calls), dispatches.Load())
	}
	attemptsBefore := map[string]entity.CallAttempt{}
	for _, fact := range calls {
		var attempts []entity.CallAttempt
		if err := db.Where("request_id = ?", fact.RequestID).Find(&attempts).Error; err != nil {
			t.Fatal(err)
		}
		if fact.RequestID == denied.RequestID {
			if len(attempts) != 0 || !reflect.DeepEqual(fact, denied) {
				t.Fatal("durable denial changed")
			}
			continue
		}
		if fact.Status != "success" || fact.TeamID != "" || fact.ProjectID != "" || len(attempts) != 1 || attempts[0].NativeCompletionEvidence != "completed" || attempts[0].CredentialID != "crd_pkw" || attempts[0].SnapshotID == "" || fact.KeyID == "" {
			t.Fatal("native original-Key attempt identity/completion missing")
		}
		attemptsBefore[attempts[0].ID] = attempts[0]
	}
	var recipients []string
	if err := db.Model(&entity.PersonalKeyQuotaWarningInbox{}).Where("id NOT IN ?", badIDs).Distinct("recipient_id").Pluck("recipient_id", &recipients).Error; err != nil || len(recipients) != 1 || recipients[0] != member.User.ID {
		t.Fatal("warning recipient fanout expanded", err)
	}
	var deliveries int64
	if err := db.Model(&entity.NotificationDeliveryIntent{}).Count(&deliveries).Error; err != nil || deliveries != 0 {
		t.Fatal("unexpected SMTP delivery intent fanout", err, deliveries)
	}
	// SQL fault intervals are restored and disarmed. This untagged refresh drains
	// any already-running tagged publisher; future periodic reads see valid SQL.
	if publicationBarrier.armed.Load() {
		t.Fatal("controlled operations checked before restoring the publication fence")
	}
	refresh()
	for _, personID := range []string{admin.User.ID, member.User.ID, outsider.User.ID} {
		permissions, err := svc.Permissions(ctx, personID)
		if err != nil || slices.Contains(permissions, "system.read") != (personID == admin.User.ID) {
			t.Fatal("controlled operational recipient authority changed", err)
		}
	}
	var operations personalKeyWarningFixtureOperationRows
	for _, rows := range []any{&operations.Publications, &operations.Jobs, &operations.Alerts, &operations.Occurrences, &operations.Notifications} {
		query := db.Limit(personalKeyWarningFixtureOperationsBound + 1)
		switch rows.(type) {
		case *[]entity.RuntimePublication, *[]entity.SystemJob:
			query = query.Where("status = ?", "failed")
		}
		if err := query.Find(rows).Error; err != nil {
			t.Fatal("controlled operational provenance read failed", err)
		}
	}
	operationProofs, proofErr := controlledOperations.snapshot()
	if err := personalKeyWarningFixtureValidateOperations(operations, operationProofs, admin.User.ID, svc.CurrentSystemInstanceID()); proofErr != nil || err != nil {
		t.Fatal("unexpected SMTP/operational fanout", proofErr, err,
			len(operations.Publications), len(operations.Jobs), len(operations.Alerts), len(operations.Occurrences), len(operations.Notifications))
	}
	beforeRestart := page()
	var inboxesBefore []entity.PersonalKeyQuotaWarningInbox
	if err := db.Order("id").Find(&inboxesBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	svc = makeService()
	router = fox.New()
	New(svc).RegisterRoutes(router)
	if err := svc.StartRuntime(workerCtx); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	flush()
	reconcile()
	if !reflect.DeepEqual(page(), beforeRestart) || dispatches.Load() != 16 {
		t.Fatal("same Session restart changed warning/read or replayed native")
	}
	var callsAfter []entity.CallRecord
	if err := db.Where("user_id IN ?", []string{admin.User.ID, member.User.ID}).Order("request_id").Find(&callsAfter).Error; err != nil || !reflect.DeepEqual(callsAfter, calls) {
		t.Fatal("restart changed immutable call facts", err)
	}
	for id, before := range attemptsBefore {
		var after entity.CallAttempt
		if err := db.Take(&after, "id = ?", id).Error; err != nil || !reflect.DeepEqual(after, before) {
			t.Fatal("restart changed native attempt facts", err)
		}
	}
	var inboxesAfter []entity.PersonalKeyQuotaWarningInbox
	if err := db.Order("id").Find(&inboxesAfter).Error; err != nil || len(inboxesAfter) != len(inboxesBefore) {
		t.Fatal("restart changed receipt cardinality", err)
	}
	for i := range inboxesAfter {
		if !personalKeyWarningInboxEqual(inboxesAfter[i], inboxesBefore[i]) {
			t.Fatal("restart changed original recipients/readstate")
		}
	}
}

func personalKeyWarningInt64(v int64) *int64    { return &v }
func personalKeyWarningString(v string) *string { return &v }
func personalKeyWarningFixturePolicy(values map[string]any) map[string]any {
	body := map[string]any{"tokens_5h": nil, "tokens_7d": nil, "tokens_month": nil, "money_month": nil, "rpm": nil, "tpm": nil, "concurrency": nil, "ip_mode": "none", "ip_ranges": []string{}, "reason": "Personal Key shared-root warning acceptance"}
	for field, value := range values {
		body[field] = value
	}
	if body["money_month"] != nil {
		if _, present := body["currency"]; !present {
			body["currency"] = "USD"
		}
	} else {
		delete(body, "currency")
	}
	return body
}
func TestPersonalKeyWarningFixturePolicyAndRootAccount(t *testing.T) {
	for _, money := range []any{nil, "11.25", "10.000000000000000001"} {
		body := personalKeyWarningFixturePolicy(map[string]any{"tokens_month": 10, "money_month": money})
		_, currency := body["currency"]
		if currency != (money != nil) {
			t.Fatal("currency presence mismatch")
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		var decoded service.LimitInput
		if err := json.Unmarshal(raw, &decoded); err != nil || decoded.TokensMonth == nil || *decoded.TokensMonth != 10 {
			t.Fatal("fixture product wire invalid", err)
		}
		if money != nil && (decoded.MoneyMonth == nil || *decoded.MoneyMonth != money) {
			t.Fatal("decimal text changed")
		}
	}
	for _, id := range []string{"key_original", "KEY_ORIGINAL", "key_successor"} {
		if strings.TrimPrefix("key_"+id, "key_") != id {
			t.Fatal("root account identity lost")
		}
	}
}

// Only the fixture-owned live publisher carries this marker. Ordinary APIs,
// manual RefreshRuntime and the actual warning observer keep their real reads.
type personalKeyWarningFixtureWorkerContext struct{}
type personalKeyWarningFixturePublicationBarrier struct {
	armed              atomic.Bool
	baselined          atomic.Bool
	rejected           atomic.Int64
	metadataMu         sync.Mutex
	metadataProofs     map[context.Context]struct{}
	metadataProofError error
}

// observePublished freezes only the fixture-owned periodic publisher. One real
// observer retains its pointer/lease/SQL/journal checks; no failed observation is
// replayed. Unlike a fault interval this positive interval changes no SQL facts,
// so its context fence must be released even if cleanup publication fails.
func (b *personalKeyWarningFixturePublicationBarrier) observePublished(refresh, observe func() error) (err error) {
	if b.armed.Load() {
		return errors.New("nested positive publication observation")
	}
	defer func() {
		if b.baselined.Load() {
			err = errors.Join(err, b.release(func() error { return nil }, refresh))
		}
		b.baselined.Store(false)
		b.armed.Store(false)
	}()
	if err = b.arm(refresh); err != nil {
		return err
	}
	return observe()
}

func (b *personalKeyWarningFixturePublicationBarrier) arm(refresh func() error) error {
	if b.armed.Swap(true) {
		return errors.New("nested publication fixture fault")
	}
	b.baselined.Store(false)
	if err := refresh(); err != nil {
		return err
	}
	b.baselined.Store(true)
	return nil
}
func (b *personalKeyWarningFixturePublicationBarrier) release(restored, refresh func() error) error {
	if !b.armed.Load() || !b.baselined.Load() {
		return errors.New("publication fixture has no armed baseline")
	}
	if err := restored(); err != nil {
		return err
	}
	if err := refresh(); err != nil {
		return err
	}
	b.baselined.Store(false)
	b.armed.Store(false)
	return nil
}

var errPersonalKeyWarningFixturePublication = errors.New("fixture-owned periodic publication read fault")

func (b *personalKeyWarningFixturePublicationBarrier) beforeQuery(tx *gorm.DB) {
	if tx.Statement != nil && tx.Statement.Context != nil && tx.Statement.Context.Value(personalKeyWarningFixtureWorkerContext{}) == b && b.armed.Load() {
		b.rejected.Add(1)
		clean := tx.Error == nil
		_ = tx.AddError(errPersonalKeyWarningFixturePublication)
		if clean && errors.Is(tx.Error, errPersonalKeyWarningFixturePublication) {
			b.recordMetadataProof(tx.Statement.Context)
		}
	}
}

// Metadata fencing is enabled only by the two Project rolling fixtures. Their
// synthetic periodic read fault must not manufacture a real operational failure.
// A cumulative rejection count cannot identify a later refresh's failure.
const personalKeyPublicationMetadataProofLimit = 64

func (b *personalKeyWarningFixturePublicationBarrier) enableMetadataFence() {
	b.metadataMu.Lock()
	defer b.metadataMu.Unlock()
	b.metadataProofs = make(map[context.Context]struct{})
}

func (b *personalKeyWarningFixturePublicationBarrier) recordMetadataProof(ctx context.Context) {
	b.metadataMu.Lock()
	defer b.metadataMu.Unlock()
	if b.metadataProofs == nil {
		return
	}
	if !reflect.TypeOf(ctx).Comparable() {
		b.metadataProofError = errors.New("fixture publication context identity is not comparable")
		return
	}
	if _, exists := b.metadataProofs[ctx]; exists {
		return
	}
	if len(b.metadataProofs) >= personalKeyPublicationMetadataProofLimit {
		b.metadataProofError = errors.New("fixture publication context proof bound exceeded")
		return
	}
	b.metadataProofs[ctx] = struct{}{}
}

func (b *personalKeyWarningFixturePublicationBarrier) beforePublicationCreate(tx *gorm.DB) {
	if tx == nil || tx.Error != nil || tx.Statement == nil || tx.Statement.Context == nil || tx.Statement.Schema == nil || tx.Statement.Table != "runtime_publications" || tx.Statement.Schema.Table != "runtime_publications" {
		return
	}
	ctx := tx.Statement.Context
	if ctx.Value(personalKeyWarningFixtureWorkerContext{}) != b || !reflect.TypeOf(ctx).Comparable() {
		return
	}
	row, ok := tx.Statement.Dest.(*entity.RuntimePublication)
	if !ok || row == nil || row.Status != "failed" || row.ErrorCode != "database_unavailable" {
		return
	}
	b.metadataMu.Lock()
	_, proven := b.metadataProofs[ctx]
	if proven {
		delete(b.metadataProofs, ctx)
	}
	b.metadataMu.Unlock()
	if proven {
		_ = tx.AddError(errPersonalKeyWarningFixturePublication)
	}
}

// Call only after the fixture's real publisher has stopped and joined. A proof
// survives release: its failed refresh may finish recording after that release.
func (b *personalKeyWarningFixturePublicationBarrier) purgeMetadataProofsAfterJoin() error {
	b.metadataMu.Lock()
	defer b.metadataMu.Unlock()
	clear(b.metadataProofs)
	err := b.metadataProofError
	b.metadataProofError = nil
	return err
}

func TestPersonalKeyWarningFixturePublicationContextBarrier(t *testing.T) {
	var b, other personalKeyWarningFixturePublicationBarrier
	ctx := context.Background()
	marked := context.WithValue(ctx, personalKeyWarningFixtureWorkerContext{}, &b)
	canceled, cancel := context.WithCancel(marked)
	defer cancel()
	bounded, stop := context.WithTimeout(canceled, time.Second)
	defer stop()
	call := func(candidate context.Context) error {
		tx := &gorm.DB{Config: &gorm.Config{}, Statement: &gorm.Statement{Context: candidate}}
		b.beforeQuery(tx)
		return tx.Error
	}
	if err := call(bounded); err != nil {
		t.Fatal("unarmed actual worker changed", err)
	}
	b.armed.Store(true)
	if err := call(bounded); !errors.Is(err, errPersonalKeyWarningFixturePublication) {
		t.Fatal("WithCancel/WithTimeout lost the runtime worker marker", err)
	}
	if err := call(ctx); err != nil {
		t.Fatal("manual observer/API reads were fenced", err)
	}
	if err := call(context.WithValue(ctx, personalKeyWarningFixtureWorkerContext{}, &other)); err != nil {
		t.Fatal("unowned worker was fenced", err)
	}
	b.armed.Store(false)
	if err := call(bounded); err != nil || b.rejected.Load() != 1 {
		t.Fatal("worker did not resume without changing its lifetime", err, b.rejected.Load())
	}
	if bounded.Err() != nil {
		t.Fatal("barrier canceled the real runtime context")
	}
}

func TestPersonalKeyWarningFixturePublicationRestorationOrder(t *testing.T) {
	var b personalKeyWarningFixturePublicationBarrier
	stages := []string{}
	if err := b.arm(func() error {
		if !b.armed.Load() {
			t.Fatal("baseline ran before the worker fence")
		}
		stages = append(stages, "baseline")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.arm(func() error { t.Fatal("nested arm reached publication"); return nil }); err == nil {
		t.Fatal("nested interval accepted")
	}
	if err := b.release(func() error { return errors.New("SQL is still changed") }, func() error { t.Fatal("unrestored SQL reached publication"); return nil }); err == nil || !b.armed.Load() {
		t.Fatal("unrestored SQL released the worker")
	}
	if err := b.release(func() error { stages = append(stages, "restore"); return nil }, func() error {
		if !b.armed.Load() {
			t.Fatal("worker resumed before restored publication")
		}
		stages = append(stages, "confirm")
		return nil
	}); err != nil || b.armed.Load() || !reflect.DeepEqual(stages, []string{"baseline", "restore", "confirm"}) {
		t.Fatal("restore/publication/disarm ordering changed", err, stages)
	}
	var confirmationFailed personalKeyWarningFixturePublicationBarrier
	if err := confirmationFailed.arm(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := confirmationFailed.release(func() error { return nil }, func() error { return errors.New("restored publication failed") }); err == nil || !confirmationFailed.armed.Load() {
		t.Fatal("unconfirmed restoration resumed the worker")
	}
	var failed personalKeyWarningFixturePublicationBarrier
	if err := failed.arm(func() error { return errors.New("no baseline") }); err == nil {
		t.Fatal("failed publication admitted interval")
	}
	if err := failed.release(func() error { t.Fatal("missing baseline reached restoration"); return nil }, func() error { return nil }); err == nil || !failed.armed.Load() {
		t.Fatal("failed baseline resumed the worker")
	}
}

func personalKeyWarningFixtureAliasCases(value string) []struct {
	id     string
	status int
} {
	return []struct {
		id     string
		status int
	}{
		{strings.ToUpper(value), http.StatusNotFound},
		{value[:len(value)-1] + " ", http.StatusNotFound},
		{value + " ", http.StatusBadRequest},
	}
}

func TestPersonalKeyWarningFixtureAliasBoundary(t *testing.T) {
	value := "kwi_" + strings.Repeat("a", 26)
	aliases := personalKeyWarningFixtureAliasCases(value)
	if len(aliases) != 3 || aliases[0].id != strings.ToUpper(value) || aliases[0].status != 404 || len(aliases[0].id) != 30 || len(aliases[1].id) != 30 || aliases[1].id != value[:29]+" " || aliases[1].status != 404 || len(aliases[2].id) != 31 || aliases[2].status != 400 {
		t.Fatal("exact alias boundaries or statuses changed")
	}
	for _, alias := range aliases {
		decoded, err := url.PathUnescape(url.PathEscape(alias.id))
		if err != nil || decoded != alias.id || alias.id == value {
			t.Fatal("alias request changed its exact decoded identity")
		}
	}
	// Exercise the production guard without constructing a DB: oversized IDs
	// must be rejected before authorization, lookup or any possible write.
	if _, err := new(service.Service).MarkNotificationRead(context.Background(), "", aliases[2].id); !errors.Is(err, apperrors.ErrBadRequest) {
		t.Fatal("oversized alias must fail the real pre-database request bound", err)
	}
}

func TestPersonalKeyWarningFixturePositivePublicationObservation(t *testing.T) {
	for _, stage := range []string{"success", "baseline", "observer", "cleanup"} {
		t.Run(stage, func(t *testing.T) {
			var b personalKeyWarningFixturePublicationBarrier
			want := errors.New("controlled positive observation failure")
			refreshes, observations := 0, 0
			worker := context.WithValue(context.Background(), personalKeyWarningFixtureWorkerContext{}, &b)
			refresh := func() error {
				refreshes++
				if !b.armed.Load() {
					t.Fatal("publication fence released before refresh")
				}
				if stage == "baseline" && refreshes == 1 || stage == "cleanup" && refreshes == 2 {
					return want
				}
				return nil
			}
			err := b.observePublished(refresh, func() error {
				observations++
				if !b.armed.Load() || !b.baselined.Load() {
					t.Fatal("observer ran without a complete publication baseline")
				}
				periodic := &gorm.DB{Config: &gorm.Config{}, Statement: &gorm.Statement{Context: worker}}
				b.beforeQuery(periodic)
				if !errors.Is(periodic.Error, errPersonalKeyWarningFixturePublication) {
					t.Fatal("periodic publisher was not fenced")
				}
				manual := &gorm.DB{Config: &gorm.Config{}, Statement: &gorm.Statement{Context: context.Background()}}
				b.beforeQuery(manual)
				if manual.Error != nil {
					t.Fatal("untagged real observer reads were fenced")
				}
				if stage == "observer" {
					return want
				}
				return nil
			})
			wantRefreshes, wantObservations := 2, 1
			if stage == "baseline" {
				wantRefreshes, wantObservations = 1, 0
			}
			if refreshes != wantRefreshes || observations != wantObservations || b.armed.Load() || b.baselined.Load() {
				t.Fatal("positive observation replayed or leaked its publication fence", refreshes, observations)
			}
			if stage == "success" && err != nil || stage != "success" && !errors.Is(err, want) {
				t.Fatal("positive observation failure was hidden", err)
			}
			resumed := &gorm.DB{Config: &gorm.Config{}, Statement: &gorm.Statement{Context: worker}}
			b.beforeQuery(resumed)
			if resumed.Error != nil {
				t.Fatal("periodic publisher remained fenced after cleanup")
			}
		})
	}
	var occupied personalKeyWarningFixturePublicationBarrier
	occupied.armed.Store(true)
	occupied.baselined.Store(true)
	if err := occupied.observePublished(func() error { t.Fatal("nested observation refreshed"); return nil }, func() error { t.Fatal("nested observer ran"); return nil }); err == nil || !occupied.armed.Load() || !occupied.baselined.Load() {
		t.Fatal("nested positive observation cleared another interval")
	}
}

// These bounded fixture receipts observe only persisted publication/job IDs.
// Runtime's existing mutex serializes publication -> job creation. No request,
// quota warning, secret, or arbitrary operational failure is allowlisted here.
const personalKeyWarningFixtureOperationsBound = 4096

type personalKeyWarningFixtureOperationProof struct {
	PublicationID string
	JobID         string
	SnapshotID    string
}
type personalKeyWarningFixtureOperations struct {
	mu      sync.Mutex
	pending *entity.RuntimePublication
	proofs  []personalKeyWarningFixtureOperationProof
	err     error
}

func (v *personalKeyWarningFixtureOperations) afterCreate(tx *gorm.DB, barrier *personalKeyWarningFixturePublicationBarrier) {
	if tx.Error != nil || tx.Statement == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	switch row := tx.Statement.Dest.(type) {
	case *entity.RuntimePublication:
		v.pending = nil
		if row.Status != "failed" {
			return
		}
		if row.ErrorCode != "database_unavailable" || row.ID == "" || row.SnapshotID == "" || tx.Statement.Context == nil || tx.Statement.Context.Value(personalKeyWarningFixtureWorkerContext{}) != barrier || !barrier.armed.Load() {
			v.err = errors.New("unowned failed runtime publication")
			return
		}
		copy := *row
		v.pending = &copy
	case *entity.SystemJob:
		if row.Code != service.SystemJobRuntimePublication {
			return
		}
		if v.pending == nil {
			return // Successful publication jobs are outside this failure proof.
		}
		if row.ID == "" || row.Status != "running" || row.StartedAt.Before(v.pending.CreatedAt) || len(v.proofs) >= personalKeyWarningFixtureOperationsBound {
			v.err = errors.New("invalid or overflowing controlled publication job")
			return
		}
		v.proofs = append(v.proofs, personalKeyWarningFixtureOperationProof{v.pending.ID, row.ID, v.pending.SnapshotID})
		v.pending = nil
	}
}
func (v *personalKeyWarningFixtureOperations) snapshot() ([]personalKeyWarningFixtureOperationProof, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.pending != nil {
		return nil, errors.New("failed publication has no recorded job")
	}
	return slices.Clone(v.proofs), v.err
}

type personalKeyWarningFixtureOperationRows struct {
	Publications  []entity.RuntimePublication
	Jobs          []entity.SystemJob
	Alerts        []entity.OperationalAlert
	Occurrences   []entity.OperationalAlertOccurrence
	Notifications []entity.Notification
}

func personalKeyWarningFixtureValidateOperations(rows personalKeyWarningFixtureOperationRows, proofs []personalKeyWarningFixtureOperationProof, adminID, executorID string) error {
	if len(proofs) == 0 || len(proofs) > personalKeyWarningFixtureOperationsBound || len(rows.Publications) != len(proofs) || len(rows.Jobs) != len(proofs) || len(rows.Occurrences) != len(proofs) || len(rows.Alerts) != 1 || len(rows.Notifications) != 1 || adminID == "" {
		return errors.New("controlled operational cardinality differs")
	}
	publications := make(map[string]entity.RuntimePublication, len(proofs))
	for _, row := range rows.Publications {
		if row.ID == "" || row.Status != "failed" || row.ErrorCode != "database_unavailable" || row.SnapshotID == "" || row.CreatedAt.IsZero() || publications[row.ID].ID != "" {
			return errors.New("unexpected failed runtime publication")
		}
		publications[row.ID] = row
	}
	jobs := make(map[string]entity.SystemJob, len(proofs))
	for _, row := range rows.Jobs {
		if row.ID == "" || row.Code != service.SystemJobRuntimePublication || row.Status != "failed" || row.DetailCode != "database_unavailable" || row.ExecutorID != executorID || row.ItemsTotal != 1 || row.ItemsCompleted != 0 || row.Progress == nil || *row.Progress != 0 || row.StartedAt.IsZero() || row.CompletedAt == nil || row.CompletedAt.Before(row.StartedAt) || !row.UpdatedAt.Equal(*row.CompletedAt) || jobs[row.ID].ID != "" {
			return errors.New("unexpected failed system job")
		}
		jobs[row.ID] = row
	}
	seenPublications, seenJobs := map[string]bool{}, map[string]bool{}
	for _, proof := range proofs {
		publication, job := publications[proof.PublicationID], jobs[proof.JobID]
		if publication.ID == "" || job.ID == "" || seenPublications[publication.ID] || seenJobs[job.ID] || publication.SnapshotID != proof.SnapshotID || job.StartedAt.Before(publication.CreatedAt) {
			return errors.New("operational source is not the exact captured publication/job pair")
		}
		seenPublications[publication.ID], seenJobs[job.ID] = true, true
	}
	alert := rows.Alerts[0]
	if alert.ID == "" || alert.GroupKey != "system_job:runtime_publication:database_unavailable" || alert.Kind != "system_job_failure" || alert.Severity != "high" || alert.DetailCode != "database_unavailable" || alert.SubjectType != "" || alert.SubjectID != "" || alert.SubjectName != "" || alert.State != "open" || alert.ETag == "" || alert.OccurrenceCount != len(proofs) {
		return errors.New("unexpected operational alert or Key subject leakage")
	}
	seenOccurrences, seenSources := map[string]bool{}, map[string]bool{}
	var first, latest entity.OperationalAlertOccurrence
	for _, occurrence := range rows.Occurrences {
		job := jobs[occurrence.SourceID]
		if occurrence.ID == "" || seenOccurrences[occurrence.ID] || seenSources[occurrence.SourceID] || occurrence.AlertID != alert.ID || occurrence.SourceType != "system_job" || job.ID == "" || occurrence.DetailCode != "database_unavailable" || occurrence.SubjectType != "" || occurrence.SubjectID != "" || occurrence.SubjectName != "" || !occurrence.OccurredAt.Equal(*job.CompletedAt) {
			return errors.New("unexpected or orphaned operational occurrence")
		}
		seenOccurrences[occurrence.ID], seenSources[occurrence.SourceID] = true, true
		if first.ID == "" || occurrence.OccurredAt.Before(first.OccurredAt) {
			first = occurrence
		}
		if latest.ID == "" || occurrence.OccurredAt.After(latest.OccurredAt) || occurrence.OccurredAt.Equal(latest.OccurredAt) && occurrence.ID > latest.ID {
			latest = occurrence
		}
	}
	if !alert.FirstSeenAt.Equal(first.OccurredAt) || !alert.LastSeenAt.Equal(latest.OccurredAt) || alert.UpdatedAt.Before(alert.LastSeenAt) {
		return errors.New("operational alert history differs from its exact sources")
	}
	notice := rows.Notifications[0]
	if notice.ID == "" || notice.RecipientID != adminID || notice.AlertID != alert.ID || notice.LatestOccurrenceID != latest.ID || notice.Kind != alert.Kind || notice.Severity != alert.Severity || notice.DetailCode != alert.DetailCode || notice.SubjectType != "" || notice.SubjectID != "" || notice.SubjectName != "" || notice.OccurrenceCount != alert.OccurrenceCount || notice.Read || notice.ReadAt != nil || !notice.FirstSeenAt.Equal(alert.FirstSeenAt) || !notice.LastSeenAt.Equal(alert.LastSeenAt) {
		return errors.New("operational notification borrowed Key or recipient authority")
	}
	return nil
}

func TestPersonalKeyWarningFixtureControlledOperations(t *testing.T) {
	makeRows := func(count int) (personalKeyWarningFixtureOperationRows, []personalKeyWarningFixtureOperationProof) {
		now := time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
		rows := personalKeyWarningFixtureOperationRows{}
		proofs := []personalKeyWarningFixtureOperationProof{}
		zero := 0
		for i := range count {
			started, completed := now.Add(time.Duration(i)*time.Second), now.Add(time.Duration(i)*time.Second+time.Millisecond)
			publicationID, jobID, occurrenceID := fmt.Sprintf("pub_controlled_%d", i), fmt.Sprintf("job_controlled_%d", i), fmt.Sprintf("occ_controlled_%d", i)
			rows.Publications = append(rows.Publications, entity.RuntimePublication{ID: publicationID, SnapshotID: "snap_controlled", Status: "failed", ErrorCode: "database_unavailable", CreatedAt: started})
			rows.Jobs = append(rows.Jobs, entity.SystemJob{ID: jobID, Code: service.SystemJobRuntimePublication, Status: "failed", DetailCode: "database_unavailable", ExecutorID: "instance_controlled", ItemsTotal: 1, Progress: &zero, StartedAt: started, UpdatedAt: completed, CompletedAt: &completed})
			rows.Occurrences = append(rows.Occurrences, entity.OperationalAlertOccurrence{ID: occurrenceID, AlertID: "alert_controlled", SourceType: "system_job", SourceID: jobID, DetailCode: "database_unavailable", OccurredAt: completed})
			proofs = append(proofs, personalKeyWarningFixtureOperationProof{publicationID, jobID, "snap_controlled"})
		}
		first, last := rows.Occurrences[0], rows.Occurrences[count-1]
		rows.Alerts = []entity.OperationalAlert{{ID: "alert_controlled", GroupKey: "system_job:runtime_publication:database_unavailable", Kind: "system_job_failure", Severity: "high", DetailCode: "database_unavailable", State: "open", ETag: "rev_controlled", OccurrenceCount: count, FirstSeenAt: first.OccurredAt, LastSeenAt: last.OccurredAt, UpdatedAt: last.OccurredAt}}
		rows.Notifications = []entity.Notification{{ID: "notice_controlled", RecipientID: "admin_controlled", AlertID: "alert_controlled", LatestOccurrenceID: last.ID, Kind: "system_job_failure", Severity: "high", DetailCode: "database_unavailable", OccurrenceCount: count, FirstSeenAt: first.OccurredAt, LastSeenAt: last.OccurredAt}}
		return rows, proofs
	}
	for _, count := range []int{1, 2, 7} {
		rows, proofs := makeRows(count)
		if err := personalKeyWarningFixtureValidateOperations(rows, proofs, "admin_controlled", "instance_controlled"); err != nil {
			t.Fatal("legitimate periodic count must use exact source proofs", count, err)
		}
	}
	for _, tc := range []struct {
		name   string
		mutate func(*personalKeyWarningFixtureOperationRows, []personalKeyWarningFixtureOperationProof)
	}{
		{"unowned_publication", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Publications[0].ID = "pub_unowned"
		}},
		{"different_snapshot", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Publications[0].SnapshotID = "snap_other"
		}},
		{"different_job", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Jobs[0].ID = "job_unowned"
		}},
		{"unexpected_job_kind", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Jobs[0].Code = service.SystemJobCallRecordDelivery
		}},
		{"unexpected_detail", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Jobs[0].DetailCode = "publication_failed"
		}},
		{"other_executor", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Jobs[0].ExecutorID = "instance_other"
		}},
		{"completed_job", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Jobs[0].Status = "completed"
		}},
		{"missing_completion", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Jobs[0].CompletedAt = nil
		}},
		{"job_before_source", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Jobs[0].StartedAt = r.Publications[0].CreatedAt.Add(-time.Millisecond)
		}},
		{"duplicate_proof", func(_ *personalKeyWarningFixtureOperationRows, p []personalKeyWarningFixtureOperationProof) {
			p[1] = p[0]
		}},
		{"duplicate_occurrence", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Occurrences[1].ID = r.Occurrences[0].ID
		}},
		{"orphaned_occurrence", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Occurrences[0].SourceID = "job_orphan"
		}},
		{"wrong_occurrence_source", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Occurrences[0].SourceType = "gateway_call"
		}},
		{"wrong_occurrence_time", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Occurrences[0].OccurredAt = r.Occurrences[0].OccurredAt.Add(time.Millisecond)
		}},
		{"wrong_alert_link", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Occurrences[0].AlertID = "alert_other"
		}},
		{"key_occurrence_leak", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Occurrences[0].SubjectID = "key_original"
		}},
		{"warning_kind_leak", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Alerts[0].Kind = "monthly_quota_warning"
		}},
		{"other_group", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Alerts[0].GroupKey = "system_job:call_record_delivery:database_unavailable"
		}},
		{"key_alert_leak", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Alerts[0].SubjectType = "personal_key"
		}},
		{"medium_severity", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Alerts[0].Severity = "medium"
		}},
		{"wrong_count", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Alerts[0].OccurrenceCount++
		}},
		{"wrong_history", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Alerts[0].FirstSeenAt = r.Alerts[0].LastSeenAt
		}},
		{"unexpected_read_receipt", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Notifications[0].Read = true
		}},
		{"member_fanout", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Notifications[0].RecipientID = "member_original"
		}},
		{"extra_recipient", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Notifications = append(r.Notifications, r.Notifications[0])
		}},
		{"notification_orphan", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Notifications[0].AlertID = "alert_missing"
		}},
		{"wrong_latest", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Notifications[0].LatestOccurrenceID = r.Occurrences[0].ID
		}},
		{"notification_key_leak", func(r *personalKeyWarningFixtureOperationRows, _ []personalKeyWarningFixtureOperationProof) {
			r.Notifications[0].SubjectName = "original Key"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, proofs := makeRows(2)
			tc.mutate(&rows, proofs)
			if err := personalKeyWarningFixtureValidateOperations(rows, proofs, "admin_controlled", "instance_controlled"); err == nil {
				t.Fatal("unexpected operations were accepted")
			}
		})
	}
	rows, proofs := makeRows(1)
	if err := personalKeyWarningFixtureValidateOperations(rows, append(proofs, make([]personalKeyWarningFixtureOperationProof, personalKeyWarningFixtureOperationsBound)...), "admin_controlled", "instance_controlled"); err == nil {
		t.Fatal("overflow accepted")
	}
}

func TestPersonalKeyWarningFixtureOperationCapture(t *testing.T) {
	var barrier personalKeyWarningFixturePublicationBarrier
	barrier.armed.Store(true)
	worker := context.WithValue(context.Background(), personalKeyWarningFixtureWorkerContext{}, &barrier)
	created := time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	makeCreate := func(ctx context.Context, dest any) *gorm.DB {
		return &gorm.DB{Config: &gorm.Config{}, Statement: &gorm.Statement{Context: ctx, Dest: dest}}
	}
	pub := entity.RuntimePublication{ID: "pub_owned", SnapshotID: "snap_owned", Status: "failed", ErrorCode: "database_unavailable", CreatedAt: created}
	job := entity.SystemJob{ID: "job_owned", Code: service.SystemJobRuntimePublication, Status: "running", StartedAt: created}
	var capture personalKeyWarningFixtureOperations
	capture.afterCreate(makeCreate(worker, &pub), &barrier)
	if _, err := capture.snapshot(); err == nil {
		t.Fatal("missing durable job accepted")
	}
	capture.afterCreate(makeCreate(context.Background(), &job), &barrier)
	proofs, err := capture.snapshot()
	if err != nil || !reflect.DeepEqual(proofs, []personalKeyWarningFixtureOperationProof{{"pub_owned", "job_owned", "snap_owned"}}) {
		t.Fatal("exact callback source pairing changed", err, proofs)
	}
	proofs[0].JobID = "mutated"
	copy, _ := capture.snapshot()
	if copy[0].JobID != "job_owned" {
		t.Fatal("snapshot caller mutated retained source proof")
	}
	for _, tc := range []struct {
		name string
		ctx  context.Context
		row  entity.RuntimePublication
	}{
		{"untagged", context.Background(), pub},
		{"foreign_marker", context.WithValue(context.Background(), personalKeyWarningFixtureWorkerContext{}, new(personalKeyWarningFixturePublicationBarrier)), pub},
		{"other_failure", worker, entity.RuntimePublication{ID: "pub_wrong", SnapshotID: "snap_owned", Status: "failed", ErrorCode: "invalid_configuration", CreatedAt: created}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rejected personalKeyWarningFixtureOperations
			rejected.afterCreate(makeCreate(tc.ctx, &tc.row), &barrier)
			if _, err := rejected.snapshot(); err == nil {
				t.Fatal("uncontrolled failed publication accepted")
			}
		})
	}
	var overflow personalKeyWarningFixtureOperations
	overflow.proofs = make([]personalKeyWarningFixtureOperationProof, personalKeyWarningFixtureOperationsBound)
	overflow.afterCreate(makeCreate(worker, &pub), &barrier)
	overflow.afterCreate(makeCreate(context.Background(), &job), &barrier)
	if _, err := overflow.snapshot(); err == nil {
		t.Fatal("captured source bound silently dropped a real job")
	}
	var beforeSource personalKeyWarningFixtureOperations
	beforeSource.afterCreate(makeCreate(worker, &pub), &barrier)
	tooEarly := job
	tooEarly.StartedAt = created.Add(-time.Millisecond)
	beforeSource.afterCreate(makeCreate(context.Background(), &tooEarly), &barrier)
	if _, err := beforeSource.snapshot(); err == nil {
		t.Fatal("job before its publication accepted")
	}
	var snapshots sync.WaitGroup
	for range 8 {
		snapshots.Add(1)
		go func() { defer snapshots.Done(); _, _ = capture.snapshot() }()
	}
	snapshots.Wait()
}
