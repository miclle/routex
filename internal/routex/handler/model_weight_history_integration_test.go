package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

func testModelWeightHistoryMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	before := personalKeyBehaviorLedger(t, db)
	if len(before) != 91 || before[90].Version != 91 || before[89].Version != 90 {
		t.Fatal("exact current V90 ledger required")
	}
	for i, row := range before {
		if row.Version != i+1 {
			t.Fatal("released migration prefix changed")
		}
	}
	for _, table := range []any{&entity.ModelWeightVersion{}, &entity.ModelWeightRollbackCommand{}} {
		var count int64
		if err := db.Model(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("migration fabricated history", err, count)
		}
		if err := db.Migrator().DropTable(table); err != nil {
			t.Fatal(err)
		}
	}
	remove := func() {
		t.Helper()
		r := db.Table("schema_migrations").Where("version = ?", 90).Delete(&struct{}{})
		if r.Error != nil || r.RowsAffected != 1 {
			t.Fatal("remove only V90", r.Error)
		}
	}
	repeat := func() {
		t.Helper()
		var wg sync.WaitGroup
		out := make(chan error, 2)
		for range 2 {
			wg.Go(func() { out <- database.Migrate(context.Background(), db) })
		}
		wg.Wait()
		close(out)
		for err := range out {
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := database.Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		got := personalKeyBehaviorLedger(t, db)
		if len(got) != 91 || got[90].Version != 91 || !personalKeyBehaviorLedgerPreserved(before, got, 90) {
			t.Fatal("released 1..89 ledger/times changed")
		}
	}
	remove()
	repeat()
	birth := time.Now().UTC().Truncate(time.Microsecond)
	versionID, err := id.NewPrefixed("mwv")
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(strings.Repeat(" ", 70*1024) + "[]")
	sum := sha256.Sum256([]byte("[]"))
	row := entity.ModelWeightVersion{ID: versionID, ModelID: "mdl_retained", ModelBirth: &birth, Sequence: 1, CapturedAt: birth, Source: "observed_baseline", ActorID: "usr_retained", BindingCount: 0, Snapshot: raw, SnapshotDigest: hex.EncodeToString(sum[:])}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal("portable retained snapshot over 64KiB", err)
	}
	var retained entity.ModelWeightVersion
	if err := db.Take(&retained, "id = ?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	maximum := row
	maximum.ID, err = id.NewPrefixed("mwv")
	if err != nil {
		t.Fatal(err)
	}
	maximum.Sequence = 2
	maximum.Snapshot = []byte(strings.Repeat(" ", 524286) + "[]")
	if err := db.Create(&maximum).Error; err != nil {
		t.Fatal("exact 512KiB snapshot rejected", err)
	}
	var maximumSaved entity.ModelWeightVersion
	if err := db.Take(&maximumSaved, "id = ?", maximum.ID).Error; err != nil || !reflect.DeepEqual(maximum.Snapshot, maximumSaved.Snapshot) {
		t.Fatal("exact 512KiB snapshot failed roundtrip", err)
	}
	// Reenter a partial DDL state without truncating the already retained table.
	if err := db.Migrator().DropTable(&entity.ModelWeightRollbackCommand{}); err != nil {
		t.Fatal(err)
	}
	if err := database.DropIndex(db, &entity.ModelWeightVersion{}, "idx_model_weight_sequence"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropConstraint(&entity.ModelWeightVersion{}, "ck_model_weight_snapshot"); err != nil {
		t.Fatal(err)
	}
	remove()
	repeat()
	var maximumRestored entity.ModelWeightVersion
	if err := db.Take(&maximumRestored, "id = ?", maximum.ID).Error; err != nil || !reflect.DeepEqual(maximumSaved, maximumRestored) {
		t.Fatal("reentry changed maximum retained snapshot", err)
	}
	var restored entity.ModelWeightVersion
	if err := db.Take(&restored, "id = ?", row.ID).Error; err != nil || !reflect.DeepEqual(retained, restored) {
		t.Fatal("partial DDL changed retained facts", err)
	}
	if !db.Migrator().HasIndex(&entity.ModelWeightVersion{}, "idx_model_weight_sequence") || !db.Migrator().HasConstraint(&entity.ModelWeightVersion{}, "ck_model_weight_snapshot") {
		t.Fatal("partial DDL failed to restore bounded guards")
	}
	for _, change := range []string{"sequence", "source", "count", "digest", "snapshot"} {
		invalid := row
		invalid.ID, _ = id.NewPrefixed("mwv")
		invalid.Sequence = 3
		switch change {
		case "sequence":
			invalid.Sequence = 0
		case "source":
			invalid.Source = "published"
		case "count":
			invalid.BindingCount = 1001
		case "digest":
			invalid.SnapshotDigest = "short"
		case "snapshot":
			invalid.Snapshot = []byte(strings.Repeat("x", 524289))
		}
		if err := db.Create(&invalid).Error; err == nil {
			t.Fatal("V90 constraint missing", change)
		}
	}
	duplicate := row
	duplicate.ID, _ = id.NewPrefixed("mwv")
	if err := db.Create(&duplicate).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatal("per-Model sequence uniqueness absent", err)
	}
	command := entity.ModelWeightRollbackCommand{RequestID: "e26c3a0b-cfee-4391-bafd-7858fbf997ee", ActorID: row.ActorID, ActorBirth: birth, ModelID: row.ModelID, ModelBirth: birth, VersionID: row.ID, ReviewETag: strings.Repeat("a", 64), InputDigest: strings.Repeat("b", 64), DesiredDigest: strings.Repeat("c", 64), Effect: "noop", Reason: "Retained unchanged request", CreatedAt: birth}
	if err := db.Create(&command).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&command).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatal("global request UUID uniqueness absent", err)
	}
	invalidCommand := command
	invalidCommand.RequestID = "e26c3a0b-cfee-4391-bafd-7858fbf997ef"
	invalidCommand.Effect = "applied"
	if err := db.Create(&invalidCommand).Error; err == nil {
		t.Fatal("current application incorrectly persisted as operation effect")
	}
	var saved entity.ModelWeightRollbackCommand
	if err := db.Take(&saved, "request_id = ?", command.RequestID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var after entity.ModelWeightRollbackCommand
	if err := db.Take(&after, "request_id = ?", command.RequestID).Error; err != nil || !reflect.DeepEqual(saved, after) {
		t.Fatal("repeat migration rewrote immutable command", err)
	}
}

func testModelWeightHistoryLifecycle(t *testing.T, db *gorm.DB) {
	var upstreamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		if r.Method != "GET" || r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"weight-source-a"},{"id":"weight-source-b"}]}`))
	}))
	t.Cleanup(upstream.Close)
	store, err := secretstore.New([]byte(strings.Repeat("w", 32)))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(context.Background(), db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"weight-history@example.com","password":"weight-history-password","name":"Weight owner"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	request := func(method, path string, body any, etag string) *httptest.ResponseRecorder {
		t.Helper()
		raw, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://routex.test")
		req.Header.Set("X-CSRF-Token", admin.CSRFToken)
		req.AddCookie(cookie)
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	provider := decodeCatalogResponse[ProviderResponse](t, request("POST", "/api/v1/admin/providers", map[string]any{"name": "Weight source", "connection_name": "Weight connection", "base_url": upstream.URL + "/v1", "protocol": "openai_chat", "credential_name": "Weight credential", "secret": "controlled-weight-secret"}, ""), 201)
	connection := provider.Connections[0]
	credential := connection.Credentials[0]
	credentialPath := "/api/v1/admin/credentials/" + credential.ID
	verified := decodeCatalogResponse[VerifyCredentialResponse](t, request("POST", credentialPath+"/verify", map[string]any{}, ""), 200)
	if !verified.Verified || verified.DiscoveredModels != 2 {
		t.Fatal("real controlled verification/discovery required")
	}
	enabled := decodeCatalogResponse[CredentialResponse](t, request("PATCH", credentialPath, map[string]bool{"enabled": true}, ""), 200)
	if !enabled.Enabled || enabled.VerificationStatus != "verified" {
		t.Fatal("explicit credential enable required")
	}
	var pms []entity.ProviderModel
	if err := db.Where("connection_id = ?", connection.ID).Order("id").Find(&pms).Error; err != nil || len(pms) != 2 {
		t.Fatal("complete controlled provider models", err)
	}
	model := decodeCatalogResponse[ModelResponse](t, request("POST", "/api/v1/admin/models", map[string]string{"name": "weight-history-model", "provider_model_id": pms[0].ID}, ""), 201)
	path := "/api/v1/admin/models/" + model.ID
	model = decodeCatalogResponse[ModelResponse](t, request("POST", path+"/bindings", map[string]string{"provider_model_id": pms[1].ID}, ""), 201)
	weights := func(a, b int) any {
		return map[string]any{"weights": []map[string]any{{"binding_id": model.Bindings[0].ID, "weight": a}, {"binding_id": model.Bindings[1].ID, "weight": b}}}
	}
	counts := func() (int64, int64, int64) {
		t.Helper()
		var v, c, a int64
		for _, item := range []struct {
			table  any
			value  *int64
			column string
		}{{&entity.ModelWeightVersion{}, &v, "model_id"}, {&entity.ModelWeightRollbackCommand{}, &c, "model_id"}, {&entity.AuditEvent{}, &a, "resource_id"}} {
			if err := db.Model(item.table).Where(item.column+" = ?", model.ID).Count(item.value).Error; err != nil {
				t.Fatal(err)
			}
		}
		return v, c, a
	}
	page := func() service.ModelWeightVersionPage {
		return decodeCatalogResponse[service.ModelWeightVersionPage](t, request("GET", path+"/weight-versions", nil, ""), 200)
	}
	if len(page().Items) != 0 {
		t.Fatal("legacy history invented before first actual changed write")
	}
	model = decodeCatalogResponse[ModelResponse](t, request("PUT", path+"/weights", weights(100, 0), ""), 200)
	first := page()
	if len(first.Items) != 2 || first.Items[0].Source != "legacy_editor" || first.Items[1].Source != "observed_baseline" || first.Items[1].ValidWeightSet || !first.Items[0].CapturedAt.Equal(first.Items[1].CapturedAt) {
		t.Fatal("observed baseline/actual same-time order lost", first)
	}
	target := first.Items[0].VersionID
	oldBaseline := decodeCatalogResponse[service.ModelWeightVersionDetail](t, request("GET", path+"/weight-versions/"+first.Items[1].VersionID, nil, ""), 200)
	for _, row := range oldBaseline.Weights {
		if row.Weight != 0 {
			t.Fatal("baseline reconstructed a fabricated earlier routing set")
		}
	}
	oldVersionCount, _, _ := counts()
	oldModel := model
	model = decodeCatalogResponse[ModelResponse](t, request("PUT", path+"/weights", weights(100, 0), ""), 200)
	v, _, _ := counts()
	if v != oldVersionCount || !reflect.DeepEqual(oldModel.ConfigUpdatedAt, model.ConfigUpdatedAt) {
		t.Fatal("legacy no-op invented version/time")
	}
	model = decodeCatalogResponse[ModelResponse](t, request("PUT", path+"/weights", weights(50, 50), ""), 200)
	limited := decodeCatalogResponse[service.ModelWeightVersionPage](t, request("GET", path+"/weight-versions?limit=1", nil, ""), 200)
	if len(limited.Items) != 1 || limited.NextCursor == nil {
		t.Fatal("bounded history page missing cursor")
	}
	next := decodeCatalogResponse[service.ModelWeightVersionPage](t, request("GET", path+"/weight-versions?limit=1&cursor="+*limited.NextCursor, nil, ""), 200)
	if len(next.Items) != 1 || next.Items[0].VersionID != target {
		t.Fatal("history cursor skipped actual sequence")
	}
	reviewResponse := request("GET", path+"/weights/rollback-review?version_id="+target, nil, "")
	review := decodeCatalogResponse[service.ModelWeightRollbackReview](t, reviewResponse, 200)
	if !review.Eligible || !review.CanRollback || reviewResponse.Header().Get("ETag") != `"`+review.ReviewETag+`"` || reviewResponse.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("fresh eligible strong review required", review)
	}
	raw := reviewResponse.Body.String()
	for _, secret := range []string{"controlled-weight-secret", "ciphertext", "source_reference", "price", "granted_user_ids"} {
		if strings.Contains(raw, secret) {
			t.Fatal("rollback read leaked independent facts", secret)
		}
	}
	input := service.ModelWeightRollbackInput{VersionID: target, RequestID: "e26c3a0b-cfee-4391-bafd-7858fbf997ee", Reason: "Restore the complete reviewed set"}
	expectStatus(t, request("POST", path+"/weights/rollback", input, ""), 400)
	beforeV, beforeC, beforeA := counts()
	remoteBefore := upstreamCalls.Load()
	result := decodeCatalogResponse[service.ModelWeightRollbackResult](t, request("POST", path+"/weights/rollback", input, review.ReviewETag), 200)
	if result.Receipt.Effect != "changed" || result.Receipt.VersionID != target || result.Receipt.SavedVersionID == nil || result.Receipt.SourceVersionID == nil || result.Receipt.RequestID != input.RequestID {
		t.Fatal("durable rollback receipt missing", result)
	}
	afterV, afterC, afterA := counts()
	if afterV != beforeV+1 || afterC != beforeC+1 || afterA != beforeA+1 || upstreamCalls.Load() != remoteBefore {
		t.Fatal("rollback must be atomic and remote-verification free")
	}
	restored := decodeCatalogResponse[ModelResponse](t, request("GET", path, nil, ""), 200)
	if restored.Bindings[0].Weight != 100 || restored.Bindings[1].Weight != 0 {
		t.Fatal("complete restored routing weights differ")
	}
	replay := decodeCatalogResponse[service.ModelWeightRollbackResult](t, request("POST", path+"/weights/rollback", input, review.ReviewETag), 200)
	if !reflect.DeepEqual(replay.Receipt, result.Receipt) {
		t.Fatal("same UUID receipt was rewritten")
	}
	gotV, gotC, gotA := counts()
	if gotV != afterV || gotC != afterC || gotA != afterA {
		t.Fatal("same UUID repeated side effects")
	}
	changed := input
	changed.Reason = "Different reviewed purpose"
	expectStatus(t, request("POST", path+"/weights/rollback", changed, review.ReviewETag), 409)
	// A fresh noop command retains its own immutable receipt without a new version.
	noopReview := decodeCatalogResponse[service.ModelWeightRollbackReview](t, request("GET", path+"/weights/rollback-review?version_id="+target, nil, ""), 200)
	noop := input
	noop.RequestID = "e26c3a0b-cfee-4391-bafd-7858fbf997ef"
	nooped := decodeCatalogResponse[service.ModelWeightRollbackResult](t, request("POST", path+"/weights/rollback", noop, noopReview.ReviewETag), 200)
	gotV, gotC, gotA = counts()
	if nooped.Receipt.Effect != "noop" || gotV != afterV || gotC != afterC+1 || gotA != afterA+1 {
		t.Fatal("noop manufactured a version or lost original operation")
	}
	current := decodeCatalogResponse[ModelResponse](t, request("GET", path, nil, ""), 200)
	if !reflect.DeepEqual(current.ConfigUpdatedAt, restored.ConfigUpdatedAt) {
		t.Fatal("noop changed Model configuration timestamp")
	}
	// Restore eligibility before another review; changed readiness must reject a
	// retained token without touching routing, receipts or immutable versions.
	stale := decodeCatalogResponse[service.ModelWeightRollbackReview](t, request("GET", path+"/weights/rollback-review?version_id="+target, nil, ""), 200)
	if err := db.Model(&entity.ProviderModel{}).Where("id = ?", current.Bindings[0].ProviderModelID).UpdateColumn("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	rejected := input
	rejected.RequestID = "e26c3a0b-cfee-4391-bafd-7858fbf997e0"
	expectStatus(t, request("POST", path+"/weights/rollback", rejected, stale.ReviewETag), 409)
	blocked := decodeCatalogResponse[service.ModelWeightRollbackReview](t, request("GET", path+"/weights/rollback-review?version_id="+target, nil, ""), 200)
	if blocked.Eligible || len(blocked.BlockerCodes) == 0 {
		t.Fatal("disabled positive route remained eligible")
	}
	if err := db.Model(&entity.ProviderModel{}).Where("id = ?", current.Bindings[0].ProviderModelID).UpdateColumn("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	// An actual later editor save supersedes current application, not history.
	model = decodeCatalogResponse[ModelResponse](t, request("PUT", path+"/weights", weights(50, 50), ""), 200)
	recovered := decodeCatalogResponse[service.ModelWeightRollbackResult](t, request("GET", path+"/weights/rollback-commands/"+input.RequestID, nil, ""), 200)
	if recovered.ApplicationStatus != "superseded" || recovered.RuntimeApplied || !reflect.DeepEqual(recovered.Receipt, result.Receipt) {
		t.Fatal("current equality substituted for original operation")
	}
	// Inject only this exact typed audit insert, after writes, and prove rollback.
	rollbackReview := decodeCatalogResponse[service.ModelWeightRollbackReview](t, request("GET", path+"/weights/rollback-review?version_id="+target, nil, ""), 200)
	beforeV, beforeC, beforeA = counts()
	// Compare the same authorized detail projection, including supply metadata.
	beforeFailure := decodeCatalogResponse[ModelResponse](t, request("GET", path, nil, ""), 200)
	auditFailure := errors.New("controlled model weight audit failure")
	callback := "fixture:weight_rollback_audit"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_events" {
			if event, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && event.Action == "model.weights.rollback" {
				_ = tx.AddError(auditFailure)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request("POST", path+"/weights/rollback", rejected, rollbackReview.ReviewETag), 500)
	if err := db.Callback().Create().Remove(callback); err != nil {
		t.Fatal(err)
	}
	gotV, gotC, gotA = counts()
	if gotV != beforeV || gotC != beforeC || gotA != beforeA {
		t.Fatal("atomic journal/receipt/audit rollback failed")
	}
	afterFailure := decodeCatalogResponse[ModelResponse](t, request("GET", path, nil, ""), 200)
	if !reflect.DeepEqual(afterFailure, beforeFailure) {
		t.Fatal("audit failure changed complete Model state")
	}
	// Independent read authority and exact identity survive a fresh Service.
	restart, err := service.New(context.Background(), db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	restarted := fox.New()
	New(restart).RegisterRoutes(restarted)
	saved := identityRequest(restarted, "GET", path+"/weights/rollback-commands/"+input.RequestID, "", cookie, "")
	preserved := decodeCatalogResponse[service.ModelWeightRollbackResult](t, saved, 200)
	if !reflect.DeepEqual(preserved.Receipt, result.Receipt) {
		t.Fatal("same database restart lost operation history")
	}
	unauth := identityRequest(router, "GET", path+"/weight-versions", "", nil, "")
	expectStatus(t, unauth, 401)
	if unauth.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("denied history must not be cached")
	}
	expectStatus(t, request("GET", path+"/weight-versions?limit=101", nil, ""), 400)
	expectStatus(t, request("GET", path+"/weight-versions?limit=1&limit=1", nil, ""), 400)
	expectStatus(t, request("GET", path+"/weight-versions?secret=x", nil, ""), 400)
	expectStatus(t, request("GET", "/api/v1/admin/models/mdl_MISSING/weight-versions", nil, ""), 404)
	// Read and write permissions remain independent, including legacy editor
	// compatibility for a writer without the new history read authority.
	if err := svc.SetRegistrationEnabled(context.Background(), admin.User.ID, true); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, permission string
		readStatus       int
	}{
		{"reader", "models.read_all", 200}, {"writer", "models.write", 403},
	} {
		role, err := svc.CreateRoleWithDescription(context.Background(), admin.User.ID, "Weight "+test.name, "Weight history permission isolation", []string{test.permission})
		if err != nil {
			t.Fatal(err)
		}
		auth, err := svc.Register(context.Background(), "weight-"+test.name+"@example.com", "weight-history-password", "Weight "+test.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SetMemberRoles(context.Background(), admin.User.ID, auth.User.ID, []string{role.Role.ID}); err != nil {
			t.Fatal(err)
		}
		scopedCookie := &http.Cookie{Name: sessionCookie, Value: auth.Token}
		scopedRead := identityRequest(router, "GET", path+"/weight-versions", "", scopedCookie, "")
		expectStatus(t, scopedRead, test.readStatus)
		if _, err := svc.RollbackModelWeights(context.Background(), auth.User.ID, model.ID, rollbackReview.ReviewETag, rejected); err == nil {
			t.Fatal("independent rollback read/write requirement lost")
		}
		if test.name == "writer" {
			if _, err := svc.SetModelWeights(context.Background(), auth.User.ID, model.ID, []service.ModelWeight{{BindingID: model.Bindings[0].ID, Weight: 50}, {BindingID: model.Bindings[1].ID, Weight: 50}}); err != nil {
				t.Fatal("legacy editor incorrectly acquired history-read requirement", err)
			}
		}
	}

	// Start the actual publisher only after the temporary audit callback has
	// been removed. No callback registry changes occur beside background work.
	if err := svc.StartRuntime(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	publishedReview := decodeCatalogResponse[service.ModelWeightRollbackReview](t, request("GET", path+"/weights/rollback-review?version_id="+target, nil, ""), 200)
	publishedInput := input
	publishedInput.RequestID = "e26c3a0b-cfee-4391-bafd-7858fbf997e1"
	published := decodeCatalogResponse[service.ModelWeightRollbackResult](t, request("POST", path+"/weights/rollback", publishedInput, publishedReview.ReviewETag), 200)
	if published.ApplicationStatus != "applied" || !published.RuntimeApplied {
		t.Fatal("exact current local publication not separately confirmed", published.ApplicationStatus)
	}
	originalAgain := decodeCatalogResponse[service.ModelWeightRollbackResult](t, request("GET", path+"/weights/rollback-commands/"+input.RequestID, nil, ""), 200)
	if !reflect.DeepEqual(originalAgain.Receipt, result.Receipt) {
		t.Fatal("current application rewrote historical operation receipt")
	}
	// Corrupt only controlled retained payloads: no partial/zero interpretation
	// may escape as an available detail, on either real driver.
	var retainedTarget entity.ModelWeightVersion
	if err := db.Take(&retainedTarget, "id = ?", target).Error; err != nil {
		t.Fatal(err)
	}
	for _, corrupt := range []string{
		strings.Replace(string(retainedTarget.Snapshot), `"weight":100`, `"weight":100,"unknown":"forbidden"`, 1),
		strings.Replace(string(retainedTarget.Snapshot), `"weight":100`, `"weight":100,"weight":100`, 1),
		strings.Replace(string(retainedTarget.Snapshot), `"weight":100`, `"weight":null`, 1),
	} {
		if corrupt == string(retainedTarget.Snapshot) {
			t.Fatal("corruption fixture did not change the exact controlled snapshot")
		}
		if err := db.Model(&entity.ModelWeightVersion{}).Where("id = ?", target).UpdateColumn("snapshot", []byte(corrupt)).Error; err != nil {
			t.Fatal(err)
		}
		expectStatus(t, request("GET", path+"/weight-versions/"+target, nil, ""), 503)
	}
	if err := db.Model(&entity.ModelWeightVersion{}).Where("id = ?", target).UpdateColumn("snapshot", retainedTarget.Snapshot).Error; err != nil {
		t.Fatal(err)
	}
	preservedTarget := decodeCatalogResponse[service.ModelWeightVersionDetail](t, request("GET", path+"/weight-versions/"+target, nil, ""), 200)
	if preservedTarget.Version.VersionID != target {
		t.Fatal("restored exact historical detail missing")
	}

}
