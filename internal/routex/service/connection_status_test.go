package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestConnectionStatusStrictBody(t *testing.T) {
	for _, raw := range []string{`{}`, `{"enabled":null,"reason":"R"}`, `{"enabled":1,"reason":"R"}`, `{"enabled":"false","reason":"R"}`, `{"enabled":false,"reason":null}`, `{"enabled":false,"reason":" R"}`, `{"enabled":false,"reason":"R","name":"N"}`, `{"enabled":false,"enabled":true,"reason":"R"}`, `{"enabled":false,"reason":"\ud800"}`, `{"enabled":false,"reason":"R"}{}`} {
		var input ConnectionStatusInput
		if json.Unmarshal([]byte(raw), &input) == nil {
			t.Fatalf("ambiguous input accepted: %s", raw)
		}
	}
	var input ConnectionStatusInput
	if err := json.Unmarshal([]byte(`{"enabled":false,"reason":"Reviewed"}`), &input); err != nil || input.Enabled || input.Reason != "Reviewed" {
		t.Fatal(input, err)
	}
	if json.Unmarshal([]byte(`{"enabled":true,"reason":"`+strings.Repeat("r", 1025)+`"}`), &input) == nil {
		t.Fatal("oversize reason")
	}
}
func TestConnectionStatusIdentityAndSharedReviews(t *testing.T) {
	actor, provider, row := connectionMetadataTestRows()
	row.Enabled = true
	metadata, _ := connectionMetadataRecord(actor, provider, row, true)
	initial := connectionStatusRecord(metadata, row)
	if connectionStatusReview(initial, initial.ETag, false) != nil || initial.ETag == metadata.ETag {
		t.Fatal("purpose binding")
	}
	egress := egressRevision(row, entity.EgressSetting{}, nil)
	row.Enabled = false
	row.ETag = "rev_status"
	nextMeta, _ := connectionMetadataRecord(actor, provider, row, true)
	next := connectionStatusRecord(nextMeta, row)
	if nextMeta.ETag == metadata.ETag || egress == egressRevision(row, entity.EgressSetting{}, nil) {
		t.Fatal("status left sibling reviews current")
	}
	if connectionStatusReview(next, initial.ETag, false) != nil || connectionStatusReview(next, initial.ETag, true) != catalogConflict {
		t.Fatal("current-only status reconciliation")
	}
	row.CreatedAt = row.CreatedAt.Add(time.Second)
	replacedMeta, _ := connectionMetadataRecord(actor, provider, row, true)
	if connectionStatusReview(connectionStatusRecord(replacedMeta, row), initial.ETag, false) != catalogConflict {
		t.Fatal("recreated row reconciled")
	}
	row.CreatedAt = row.CreatedAt.Add(-time.Second)
	row.Name = "Renamed"
	row.ETag = "rev_name"
	renamed, _ := connectionMetadataRecord(actor, provider, row, true)
	if connectionStatusReview(connectionStatusRecord(renamed, row), next.ETag, true) != catalogConflict {
		t.Fatal("name change left status review current")
	}
}
func TestConnectionStatusFalseUpdatePreservesChildren(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		t.Run(dialect.Name(), func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Callback().Update().Register("test:connection-status-update", func(q *gorm.DB) {
				q.Statement.BuildClauses = []string{"UPDATE", "SET", "WHERE"}
				callbacks.Update(&callbacks.Config{})(q)
			}); err != nil {
				t.Fatal(err)
			}
			stmt := connectionMetadataQuery(db, "con_CASE").Updates(map[string]any{"enabled": false, "ETag": "rev_status"}).Statement
			if !strings.Contains(stmt.SQL.String(), "enabled") || !reflect.DeepEqual(stmt.Vars, []any{"rev_status", false, "con_CASE"}) {
				t.Fatal(stmt.SQL.String(), stmt.Vars)
			}
			for _, forbidden := range []string{"provider_credentials", "provider_models", "model_provider_bindings", "verification_status", "weight"} {
				if strings.Contains(stmt.SQL.String(), forbidden) {
					t.Fatal("child altered", stmt.SQL.String())
				}
			}
		})
	}
}
func TestConnectionStatusDisabledDiscoveryAndDispatch(t *testing.T) {
	for _, protocol := range []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		t.Run(protocol, func(t *testing.T) {
			s, data, _ := runtimeFixture(t, "https://api.example.invalid/v1")
			data.Connections[0].Protocol = protocol
			routes, err := s.buildRuntimeRoutes(data)
			if err != nil {
				t.Fatal(err)
			}
			s.runtime.routes.Store(&runtimeRoutes{ID: "cfg_status", Models: routes})
			route := routes["mdl_one"][0].Route
			route.SnapshotID = "cfg_status"
			if !s.runtimeRouteReadyForDiscovery(s.runtime.auth.Load(), routes["mdl_one"][0]) {
				t.Fatal("enabled supply not eligible")
			}
			release, ok := s.pinConnectionDispatch(&route)
			if !ok {
				t.Fatal("enabled dispatch refused")
			}
			release()
			release()
			// Synchronous denial stops old published supply even before refresh.
			s.runtime.deniedConnections.Store("con_one", s.runtime.epoch.Add(1))
			if s.runtimeRouteReadyForDiscovery(s.runtime.auth.Load(), routes["mdl_one"][0]) {
				t.Fatal("stale discovery exposed")
			}
			if _, _, err := s.runtimeProtocolRoute("mdl_one", protocol); err == nil {
				t.Fatal("stale selection eligible")
			}
			if release, ok := s.pinConnectionDispatch(&route); ok {
				release()
				t.Fatal("prepared dispatch admitted after disable")
			}
			data.Connections[0].Enabled = false
			s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
			clearRuntimeTombstones(&s.runtime.deniedConnections, s.runtime.epoch.Load())
			if s.runtimeConnectionAllowed(s.runtime.auth.Load(), route) {
				t.Fatal("publication restored disabled connection")
			}
			data.Connections[0].Enabled = true
			data.Connections[0].CreatedAt = data.Connections[0].CreatedAt.Add(time.Second)
			s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
			if s.runtimeConnectionAllowed(s.runtime.auth.Load(), route) {
				t.Fatal("old incarnation restored")
			}
			if data.Credentials[0].Enabled != true || data.ProviderModels[0].Disabled || data.Bindings[0].Weight != 100 {
				t.Fatal("child state changed")
			}
		})
	}
}
func TestConnectionStatusPublicationGateHasNoRemoteLifetime(t *testing.T) {
	s, data, _ := runtimeFixture(t, "https://api.example.invalid/v1")
	route := s.runtime.routes.Load().Models["mdl_one"][0].Route
	route.SnapshotID = "cfg_test"
	release, ok := s.pinConnectionDispatch(&route)
	if !ok {
		t.Fatal("dispatch not admitted")
	}
	attempted := make(chan struct{})
	committed := make(chan struct{})
	go func() {
		close(attempted)
		s.runtime.publication.Lock()
		data.Connections[0].Enabled = false
		s.runtime.deniedConnections.Store(route.ConnectionID, s.runtime.epoch.Add(1))
		s.runtime.publication.Unlock()
		close(committed)
	}()
	<-attempted
	if s.runtime.publication.TryLock() {
		s.runtime.publication.Unlock()
		t.Fatal("status crossed held local admission")
	}
	// Remote request/stream may continue after this local release; status commit
	// is no longer held until response completion.
	release()
	<-committed
	if s.runtimeConnectionAllowed(s.runtime.auth.Load(), route) {
		t.Fatal("future dispatch still allowed")
	}
}

func TestConnectionStatusDisableBeforeFinalDispatchCreatesNoAttempt(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = io.WriteString(w, `{"choices":[]}`) }))
	defer server.Close()
	svc, _, bearer := runtimeFixture(t, server.URL+"/v1")
	svc.afterGatewayAdmission = func() {
		svc.runtime.publication.Lock()
		svc.runtime.deniedConnections.Store("con_one", svc.runtime.epoch.Add(1))
		svc.runtime.publication.Unlock()
	}
	result, err := svc.GatewayChat(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"held prepared request"}]}`), "req_connection_before_dispatch")
	if err == nil || calls.Load() != 0 || result == nil || result.AttemptID != "" || len(result.Attempts) != 0 {
		t.Fatalf("prepared reservation bypassed disable: calls=%d result=%+v err=%v", calls.Load(), result, err)
	}
}
func TestConnectionStatusAlreadyReceivedAttemptKeepsIdentity(t *testing.T) {
	received := make(chan struct{})
	finish := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(received)
		<-finish
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"provider-model","choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
	}))
	defer server.Close()
	svc, _, bearer := runtimeFixture(t, server.URL+"/v1")
	type observation struct {
		result *GatewayResult
		err    error
	}
	done := make(chan observation, 1)
	go func() {
		result, err := svc.GatewayChat(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"already received"}]}`), "req_connection_inflight")
		done <- observation{result, err}
	}()
	<-received
	if !svc.runtime.publication.TryLock() {
		close(finish)
		<-done
		t.Fatal("remote response held global publication gate")
	}
	svc.runtime.deniedConnections.Store("con_one", svc.runtime.epoch.Add(1))
	svc.runtime.publication.Unlock()
	close(finish)
	got := <-done
	if got.err != nil || got.result == nil || got.result.Response == nil {
		t.Fatal("in-flight work aborted", got.err)
	}
	defer func() { _ = got.result.Response.Body.Close() }()
	body, readErr := io.ReadAll(got.result.Response.Body)
	if readErr != nil || !strings.Contains(string(body), `"finish_reason":"stop"`) {
		t.Fatal("received attempt response did not finish", string(body), readErr)
	}
	if got.result.ConnectionID != "con_one" || got.result.CredentialID != "crd_one" || got.result.SnapshotID != "cfg_test" || got.result.AttemptID == "" {
		t.Fatalf("captured attempt attribution changed: %+v", got.result)
	}
	if _, _, err := svc.runtimeProtocolRoute("mdl_one", entity.ProtocolOpenAIChat); err == nil {
		t.Fatal("future selection allowed")
	}
}
