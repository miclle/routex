package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"gorm.io/gorm"
)

// Invoked by the existing exclusively owned Accept SQL worker. These fixtures
// create no deployment authority and never qualify real fleet startup exclusion.
func sharedGatewaySQLIntegration(t *testing.T, ctx context.Context, db *gorm.DB, fixture func(*testing.T, string, map[string]any) sharedEconomicAcceptSQLFixture, owner admissionOwnerRegistration) {
	t.Helper()
	for _, kind := range []string{"personal_key", "project_key", "personal_session", "team_session"} {
		t.Run("gateway_preflight_rollback_"+kind, func(t *testing.T) {
			f := fixture(t, kind, nil)
			before := sharedEconomicAcceptSQLSnapshot(t, db)
			if err := f.store.preflightSharedGatewaySQL(ctx, f.intent, time.Second); err != nil {
				t.Fatal("complete conjunctive preflight", err)
			}
			sharedEconomicAcceptSQLRequireSnapshot(t, db, before)
			// The same original operation still performs exactly one final atomic
			// reservation; preflight consumed neither RPM nor executor capacity.
			f.accept(t, ctx)
			f.requireAdmission(t, db)
		})
	}
	t.Run("gateway_live_capture_dispatch_body_close_and_final_fact", func(t *testing.T) {
		f := fixture(t, "personal_key", nil)
		s := f.store.live.service
		s.instance = &systemInstanceLease{id: owner.Owner.IncarnationID, startedAt: owner.InstanceStartedAt}
		// The dormant constructor requires original/calendar metadata. This
		// explicitly seeded test table/body is not a production Fresh command.
		if db.Migrator().HasTable(&entity.AdmissionEconomicOrigin{}) {
			t.Fatal("worker unexpectedly has registered origin schema")
		}
		if err := db.Migrator().CreateTable(&entity.AdmissionEconomicOrigin{}); err != nil {
			t.Fatal(err)
		}
		origin, err := sharedFreshDomainOrigin(sharedFreshDomainIntent{InstallationID: owner.Owner.InstallationID, TimeZone: f.quota.Setting.TimeZone, CalendarETag: f.quota.Setting.ETag, BoundaryAt: 1, ReceiptCapacity: 1000})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&origin).Error; err != nil {
			t.Fatal(err)
		}
		registered, err := (&admissionOwnerStore{db: db}).RegisterOwner(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		journal, err := eventqueue.OpenSharedSpool(ctx, filepath.Join(t.TempDir(), "calls.db"), eventqueue.SharedSpoolIdentity{InstallationID: owner.Owner.InstallationID, SpoolID: owner.Owner.SpoolID, ModeRevision: owner.Owner.ModeRevision}, eventqueue.SharedSpoolOptions{Capacity: 8, MaxDataBytes: eventqueue.SharedSpoolMaxDataBytes, DeliveredCapacity: 8})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := journal.Close(); err != nil {
				t.Error(err)
			}
		})
		r, err := newSharedGatewayRuntime(ctx, s, journal, *registered, time.Second)
		if err != nil {
			t.Fatal("verified owner, origin and exact spool runtime", err)
		}
		s.sharedGateway = r
		transport := &sharedGatewaySQLTransport{}
		routes := s.runtime.routes.Load()
		for model, candidates := range routes.Models {
			for i := range candidates {
				candidates[i].Route.Client = &http.Client{Transport: transport}
			}
			routes.Models[model] = candidates
		}
		payload, err := json.Marshal(f.payload)
		if err != nil {
			t.Fatal(err)
		}
		requestID := f.intent.RequestID + "_gateway"
		requestCtx := WithGatewayClientIP(ctx, f.store.live.clientIP)
		result, err := s.gatewayNativeIdentity(requestCtx, f.store.live.result.identity, payload, requestID, entity.ProtocolOpenAIChat)
		if err != nil || result == nil || !result.Admitted || result.sharedExecution == nil || result.Response == nil {
			t.Fatal("actual native Gateway did not select shared lifecycle", err)
		}
		if transport.calls != 1 || transport.body == nil || transport.body.closed != 0 {
			t.Fatal("dispatch repeated or closed at response headers")
		}
		execution := result.sharedExecution
		var before entity.AdmissionEconomicAccount
		policy := execution.coordinator.intent.Policies[0]
		if err := db.Where("installation_id = ? AND account_digest = ?", owner.Owner.InstallationID, sharedEconomicIdentityDigest(policy.Account)).Take(&before).Error; err != nil || before.ExecutorCount < 1 {
			t.Fatal("headers released executor", err)
		}
		fallback, err := gatewayFallbackPayload(requestID, result, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		fact, err := sharedCallPayload(requestID, fallback)
		if err != nil {
			t.Fatal(err)
		}
		// No native parser result is fabricated: unknown usage stays unknown.
		if err := s.FinalizeGatewayCall(requestCtx, result, fact); err != nil {
			t.Fatal("exact Gateway finalization", err)
		}
		if transport.body.closed != 1 {
			t.Fatal("terminal did not close body")
		}
		var after entity.AdmissionEconomicAccount
		if err := db.Where("installation_id = ? AND account_digest = ?", owner.Owner.InstallationID, sharedEconomicIdentityDigest(policy.Account)).Take(&after).Error; err != nil || after.ExecutorCount != before.ExecutorCount-1 {
			t.Fatal("exact executor did not release once", err)
		}
		if err := s.FinalizeGatewayCall(requestCtx, result, fact); err != nil {
			t.Fatal("identical terminal retry before delivery", err)
		}
		if err := s.FlushCallRecorder(ctx); err != nil {
			t.Fatal("ready shared fact delivery", err)
		}
		var call entity.CallRecord
		if err := db.Where("request_id = ?", requestID).Take(&call).Error; err != nil {
			t.Fatal("real Call projection missing", err)
		}
		if call.ModelID != result.ModelID || call.KeyID != result.KeyID {
			t.Fatal("delivered fact changed immutable scope")
		}
		if err := s.FlushCallRecorder(ctx); err != nil {
			t.Fatal("idempotent shared delivery retry", err)
		}
		if transport.calls != 1 || transport.body.closed != 1 {
			t.Fatal("delivery retry repeated inference or transport closure")
		}
		if entries, err := r.spool.ReadDelivery(ctx, 8); err != nil || len(entries) != 0 {
			t.Fatal("acknowledged fact was delivered twice", err)
		}
	})
}

type sharedGatewaySQLTransport struct {
	calls int
	body  *sharedGatewayTestBody
}

func (t *sharedGatewaySQLTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.calls++
	t.body = &sharedGatewayTestBody{Reader: strings.NewReader(`{"id":"chatcmpl_shared","object":"chat.completion","model":"native","choices":[{"index":0,"message":{"role":"assistant","content":"controlled"},"finish_reason":"stop"}]}`)}
	if request.Header.Get("Authorization") == "" {
		return nil, fmt.Errorf("prepared upstream credential missing")
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: t.body, Request: request}, nil
}

var _ io.ReadCloser = (*sharedGatewayTestBody)(nil)
