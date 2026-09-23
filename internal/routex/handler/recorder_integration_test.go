package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

// Called by the single isolated database lifecycle owner. Only the private
// connection pool is closed; the caller's pool remains available for evidence.
func testRecorderLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	privateDB, err := database.Open(ctx, db.Name(), os.Getenv("ROUTEX_TEST_"+strings.ToUpper(db.Name())+"_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	privateDB = privateDB.Session(&gorm.Session{Logger: logger.Discard})
	pool, err := privateDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()
	outage, err := service.New(ctx, privateDB)
	if err != nil {
		t.Fatal(err)
	}
	spool := filepath.Join(t.TempDir(), "call-events.db")
	if err := outage.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = outage.StopCallRecorder() }()
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}

	started := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Second)
	inputTokens, outputTokens := int64(17), int64(23)
	fact := service.CallFact{
		SnapshotID: "cfg_recorder", RequestID: "req_recorder_outage", UserID: "usr_recorder_historical",
		KeyID: "key_recorder_historical", ModelID: "mdl_recorder_historical", ModelName: "recorded-model",
		ProviderModelID: "pmd_recorder_historical", ConnectionID: "con_recorder_historical",
		Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: started, CompletedAt: started.Add(100 * time.Millisecond),
		InputTokens: &inputTokens, OutputTokens: &outputTokens,
		Attempts: []service.CallAttempt{{ID: "att_recorder_outage", ProviderModelID: "pmd_recorder_historical", ConnectionID: "con_recorder_historical", Status: "success", StartedAt: started, CompletedAt: started.Add(100 * time.Millisecond), HTTPStatus: 200}},
	}
	duplicate := fact
	duplicate.RequestID = "req_recorder_duplicate"
	duplicate.Attempts = []service.CallAttempt{fact.Attempts[0]}
	duplicate.Attempts[0].ID = "att_recorder_duplicate"
	for _, completed := range []service.CallFact{fact, duplicate} {
		admitRecorderFixture(t, outage, completed.RequestID, completed)
		// Completion is durable even though its eventual database delivery fails.
		if err := outage.PersistGatewayCall(ctx, completed); err != nil {
			t.Fatal("completion failed to remain durable during database outage")
		}
	}
	pending := fact
	pending.RequestID = "req_recorder_interrupted"
	pending.Attempts = []service.CallAttempt{fact.Attempts[0]}
	pending.Attempts[0].ID = "att_recorder_interrupted"
	admitRecorderFixture(t, outage, pending.RequestID, pending)
	if err := outage.FlushCallRecorder(ctx); err == nil {
		t.Fatal("outage delivery falsely acknowledged a completed event")
	}
	var count int64
	if err := db.Model(&entity.CallRecord{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("outage events reached the database or the primary pool was closed")
	}

	live, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	// Model a crash after relational commit and before local acknowledgment.
	// On replay, the already accepted fact must retain its original usage.
	accepted := duplicate
	acceptedInput := int64(31)
	accepted.InputTokens = &acceptedInput
	if err := live.RecordCall(ctx, accepted); err != nil {
		t.Fatal(err)
	}
	if err := outage.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(spool)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"test-only-recorder-provider-secret", "test-only-recorder-prompt-content", "test-only-recorder-credential-id"} {
		if bytes.Contains(contents, []byte(forbidden)) {
			t.Fatal("durable admission included response content or credential material")
		}
	}
	if !bytes.Contains(contents, []byte(pending.RequestID)) {
		t.Fatal("pending admission was not durably retained before restart")
	}
	info, err := os.Stat(spool)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("event spool permissions are not private")
	}

	if err := live.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := live.StopCallRecorder(); err != nil {
			t.Error(err)
		}
	}()
	if err := live.FlushCallRecorder(ctx); err != nil {
		t.Fatal("restart did not replay queued events")
	}
	if err := live.FlushCallRecorder(ctx); err != nil {
		t.Fatal("repeated flush was not idempotent")
	}
	if err := db.Model(&entity.CallRecord{}).Count(&count).Error; err != nil || count != 3 {
		t.Fatalf("recovery must retain exactly three request facts, got %d", count)
	}
	for _, expected := range []service.CallFact{fact, accepted} {
		detail, err := live.GetCall(ctx, "", expected.RequestID)
		if err != nil {
			t.Fatal(err)
		}
		if detail.Record.Status != "success" || detail.Record.SnapshotID != expected.SnapshotID || detail.Record.InputTokens == nil || *detail.Record.InputTokens != *expected.InputTokens || detail.Record.OutputTokens == nil || *detail.Record.OutputTokens != *expected.OutputTokens || len(detail.Attempts) != 1 {
			t.Fatal("recovery changed the canonical fact or doubled its attempt/usage")
		}
	}
	interrupted, err := live.GetCall(ctx, "", pending.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if interrupted.Record.Status != "error" || interrupted.Record.ErrorCode != "process_interrupted" || interrupted.Record.InputTokens != nil || interrupted.Record.OutputTokens != nil || interrupted.Record.SnapshotID != pending.SnapshotID {
		t.Fatal("interrupted admission was not recovered with explicit unknown usage")
	}
}

func admitRecorderFixture(t *testing.T, svc *service.Service, requestID string, fact service.CallFact) {
	t.Helper()
	response := &http.Response{
		Header: http.Header{"Authorization": {"Bearer test-only-recorder-provider-secret"}},
		Body:   io.NopCloser(strings.NewReader("test-only-recorder-prompt-content")),
	}
	defer func() { _ = response.Body.Close() }()
	result := &service.GatewayResult{
		SnapshotID: fact.SnapshotID, Response: response, UserID: fact.UserID, KeyID: fact.KeyID,
		ModelID: fact.ModelID, ModelName: fact.ModelName, ProviderModelID: fact.ProviderModelID,
		ConnectionID: fact.ConnectionID, CredentialID: "test-only-recorder-credential-id",
		Stream: fact.Stream, AttemptID: fact.Attempts[0].ID, AttemptStartedAt: fact.StartedAt,
	}
	if err := svc.AdmitGatewayCall(requestID, result); err != nil {
		t.Fatal("durable admission failed during database outage")
	}
}
