package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
)

func lifecycleSubjectFixture() modelLifecycleSubject {
	alias := aliasSubjectFixture()
	stamp := alias.Model.CreatedAt
	alias.Model.ConfigUpdatedAt = &stamp
	return modelLifecycleSubject{Model: alias.Model, Current: alias.Current}
}
func TestModelLifecycleReviewedTransitionAndExactBirth(t *testing.T) {
	original := lifecycleSubjectFixture()
	etag := modelLifecycleReview(original, time.Now(), true).ETag
	for _, tc := range []struct {
		name    string
		change  func(*modelLifecycleSubject)
		status  string
		changed bool
		err     error
	}{
		{"disable", nil, "disabled", true, nil},
		{"same_state", nil, "active", false, nil},
		{"uncertain_disable", func(s *modelLifecycleSubject) {
			s.Model.Status = "disabled"
			stamp := s.Model.CreatedAt.Add(time.Minute)
			s.Model.ConfigUpdatedAt = &stamp
		}, "disabled", false, nil},
		{"renewed_configuration", func(s *modelLifecycleSubject) {
			stamp := s.Model.CreatedAt.Add(time.Minute)
			s.Model.ConfigUpdatedAt = &stamp
		}, "disabled", false, catalogConflict},
		{"renamed", func(s *modelLifecycleSubject) { s.Current.Name = "other" }, "disabled", false, catalogConflict},
		{"recreated_same_desired_state", func(s *modelLifecycleSubject) {
			s.Model.Status = "disabled"
			s.Model.CreatedAt = s.Model.CreatedAt.Add(time.Millisecond)
		}, "disabled", false, catalogConflict},
		{"case_alias_same_desired_state", func(s *modelLifecycleSubject) { s.Model.ID = "mdl_ALIAS"; s.Model.Status = "disabled" }, "disabled", false, catalogConflict},
		{"archived", func(s *modelLifecycleSubject) { s.Model.Status = "archived" }, "active", false, catalogConflict},
		{"unknown", func(s *modelLifecycleSubject) { s.Model.Status = "other" }, "active", false, catalogConflict},
		{"active_cannot_archive", nil, "archived", false, catalogConflict},
		{"archived_cannot_restore_disabled", func(s *modelLifecycleSubject) { s.Model.Status = "archived" }, "disabled", false, catalogConflict},
		{"same_archived_reconciles", func(s *modelLifecycleSubject) { s.Model.Status = "archived" }, "archived", false, nil},
		{"wrong_birth_archived", func(s *modelLifecycleSubject) {
			s.Model.Status = "archived"
			s.Model.CreatedAt = s.Model.CreatedAt.Add(time.Second)
		}, "archived", false, catalogConflict},
		{"unknown_input", nil, "unknown", false, apperrors.ErrBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			subject := original
			if tc.change != nil {
				tc.change(&subject)
			}
			changed, err := modelLifecycleTransition(subject, etag, tc.status)
			if changed != tc.changed || !errors.Is(err, tc.err) {
				t.Fatal(changed, err)
			}
		})
	}
	disabled := original
	disabled.Model.Status = "disabled"
	if changed, err := modelLifecycleTransition(disabled, modelLifecycleReview(disabled, time.Now(), true).ETag, "active"); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if changed, err := modelLifecycleTransition(disabled, modelLifecycleReview(disabled, time.Now(), true).ETag, "archived"); err != nil || !changed {
		t.Fatal("reviewed disabled archive rejected", changed, err)
	}
	if _, err := modelLifecycleTransition(disabled, etag, "archived"); !errors.Is(err, catalogConflict) {
		t.Fatal("stale archive review accepted", err)
	}
	zone := time.FixedZone("other", 9*3600)
	equivalent := original
	equivalent.Model.CreatedAt = equivalent.Model.CreatedAt.In(zone)
	v := equivalent.Model.ConfigUpdatedAt.In(zone)
	equivalent.Model.ConfigUpdatedAt = &v
	equivalent.Current.CreatedAt = equivalent.Current.CreatedAt.In(zone)
	if modelLifecycleReview(equivalent, time.Now(), false).ETag != etag {
		t.Fatal("timezone or permission changed exact review")
	}
	for _, status := range []string{"active", "disabled", "archived"} {
		subject := original
		subject.Model.Status = status
		review := modelLifecycleReview(subject, time.Now(), true)
		if review.CanChange != (status != "archived") || !validModelLifecycleETag(review.ETag) {
			t.Fatal(review)
		}
		if modelLifecycleReview(subject, time.Now(), false).CanChange {
			t.Fatal("read borrowed write")
		}
	}
}
func TestModelLifecycleInputRejectsHostileAndUnknown(t *testing.T) {
	var input ModelLifecycleInput
	if err := json.Unmarshal([]byte(`{"status":"disabled","reason":" review "}`), &input); err != nil || input.Reason != " review " {
		t.Fatal(input, err)
	}
	for _, raw := range []string{`{}`, `null`, `[]`, `{"status":"unknown","reason":"x"}`, `{"status":"DISABLED","reason":"x"}`, `{"status":"active","reason":null}`, `{"status":"active","reason":" "}`, `{"status":"active","reason":"\nstop"}`, `{"status":"active","reason":"x","status":"disabled"}`, `{"status":"active","reason":"x","extra":"x"}`, `{"status":"active","reason":"` + strings.Repeat("界", 342) + `"}`, "{\"status\":\"active\",\"reason\":\"\xff\"}", `{"status":"active","reason":"x"} {}`} {
		if json.Unmarshal([]byte(raw), &input) == nil {
			t.Fatal("invalid input accepted", raw)
		}
	}
	if err := json.Unmarshal([]byte(`{"status":"archived","reason":"retire"}`), &input); err != nil || input.Status != "archived" {
		t.Fatal("archive wire input rejected", input, err)
	}
	svc := &Service{}
	for _, etag := range []string{"", strings.Repeat("a", 64), strings.Repeat("A", 64) + "." + strings.Repeat("a", 64), strings.Repeat("a", 64) + "," + strings.Repeat("b", 64)} {
		if _, err := svc.SetModelLifecycle(context.Background(), nil, "mdl_alias", etag, ModelLifecycleInput{Status: "active", Reason: "x"}); !errors.Is(err, apperrors.ErrBadRequest) {
			t.Fatal(err)
		}
	}
	if _, err := svc.GetModelLifecycle(context.Background(), nil, "foreign"); !errors.Is(err, apperrors.ErrBadRequest) {
		t.Fatal(err)
	}
	if _, err := svc.modelLifecycleAuthority(nil, nil, "models.write", true); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal(err)
	}
}
func TestModelLifecycleRuntimeProofKeepsMissingUnknownAndExactIdentity(t *testing.T) {
	subject := lifecycleSubjectFixture()
	svc, data, _ := runtimeFixture(t, "https://example.invalid/v1")
	data.Models = []entity.Model{subject.Model}
	data.Names = []entity.ModelName{subject.Current}
	publish := func() {
		svc.runtime.deniedModels.Delete(subject.Model.ID)
		svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	}
	for _, status := range []string{"active", "disabled", "archived"} {
		subject.Model.Status = status
		data.Models[0] = subject.Model
		publish()
		if !svc.runtimeModelLifecycleApplied(subject) {
			t.Fatal("exact status publication not proven", status)
		}
	}
	for _, tc := range []struct {
		name   string
		change func()
	}{
		{"expired", func() { svc.runtime.auth.Load().ValidUntil = time.Now().Add(-time.Second) }},
		{"denied", func() { svc.InvalidateRuntimeModel(subject.Model.ID) }},
		{"missing_model", func() { delete(svc.runtime.auth.Load().Models, subject.Model.ID) }},
		{"wrong_birth", func() {
			svc.runtime.auth.Load().ModelCreated[subject.Model.ID] = subject.Model.CreatedAt.Add(time.Millisecond)
		}},
		{"wrong_status", func() { svc.runtime.auth.Load().Models[subject.Model.ID] = true }},
		{"missing_name", func() { delete(svc.runtime.auth.Load().Names, subject.Current.Name) }},
		{"wrong_name_owner", func() {
			row := subject.Current
			row.ModelID = "mdl_other"
			svc.runtime.auth.Load().Names[row.Name] = row
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			publish()
			tc.change()
			if svc.runtimeModelLifecycleApplied(subject) {
				t.Fatal("unproven runtime status accepted")
			}
		})
	}
	if (&Service{}).runtimeModelLifecycleApplied(subject) {
		t.Fatal("nil runtime proven")
	}
}
func TestModelLifecycleSubjectQueriesUseExactIndependentLocks(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		t.Run(dialect.Name(), func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			subject := lifecycleSubjectFixture()
			var sqls []string
			var vars [][]any
			if err := db.Callback().Query().Register("test:model-lifecycle", func(q *gorm.DB) {
				q.Statement.BuildClauses = []string{"SELECT", "FROM", "WHERE", "LIMIT", "FOR"}
				callbacks.BuildQuerySQL(q)
				sqls = append(sqls, q.Statement.SQL.String())
				vars = append(vars, append([]any(nil), q.Statement.Vars...))
				switch row := q.Statement.Dest.(type) {
				case *entity.Model:
					*row = subject.Model
				case *entity.ModelName:
					*row = subject.Current
				default:
					t.Fatalf("unexpected %T", row)
				}
				q.Statement.SQL.Reset()
				q.Statement.Vars = nil
			}); err != nil {
				t.Fatal(err)
			}
			got, err := loadModelLifecycleSubject(db, subject.Model.ID, true)
			if err != nil || !reflect.DeepEqual(got, subject) {
				t.Fatal(got, err)
			}
			if len(sqls) != 2 {
				t.Fatal(sqls)
			}
			for i, query := range sqls {
				if !strings.HasSuffix(query, " FOR UPDATE") || !reflect.DeepEqual(vars[i], []any{subject.Model.ID, 1}) {
					t.Fatal(query, vars[i])
				}
				if dialect.Name() == "mysql" && strings.Count(query, "AS BINARY") != 2 {
					t.Fatal(query)
				}
			}
		})
	}
}
func TestModelLifecycleAuditProjectsOnlyValidatedTypedFacts(t *testing.T) {
	subject := lifecycleSubjectFixture()
	raw, _ := json.Marshal(modelLifecycleAudit{subject.Model.ID, subject.Model.CreatedAt, "active", "disabled", "maintenance"})
	value := string(raw[:len(raw)-1]) + `,"secret":"never"}`
	row := entity.AuditEvent{Action: "model.lifecycle.update", ResourceType: "model", ResourceID: subject.Model.ID, DetailsJSON: &value}
	if changes := auditRecord(row).Changes; len(changes) == 0 || strings.Contains(string(changes), "secret") {
		t.Fatal(string(changes))
	}
	for _, transition := range [][2]string{{"active", "disabled"}, {"disabled", "active"}, {"disabled", "archived"}, {"active", "archived"}, {"archived", "active"}, {"archived", "disabled"}, {"archived", "archived"}, {"unknown", "disabled"}} {
		encoded, _ := json.Marshal(modelLifecycleAudit{subject.Model.ID, subject.Model.CreatedAt, transition[0], transition[1], "review"})
		value := string(encoded)
		candidate := row
		candidate.DetailsJSON = &value
		_, ok := modelLifecycleAuditProjection(candidate)
		want := transition == [2]string{"active", "disabled"} || transition == [2]string{"disabled", "active"} || transition == [2]string{"disabled", "archived"}
		if ok != want {
			t.Fatal("invalid lifecycle audit transition", transition, ok)
		}
	}
	row.ResourceID = "mdl_other"
	if auditRecord(row).Changes != nil {
		t.Fatal("foreign audit projected")
	}
}

func TestModelLifecyclePublicationGateRejectsBusyBeforeDatabase(t *testing.T) {
	svc, data, _ := runtimeFixture(t, "https://example.invalid/v1")
	subject := lifecycleSubjectFixture()
	data.Models = []entity.Model{subject.Model}
	data.Names = []entity.ModelName{subject.Current}
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	svc.runtime.publication.RLock()
	_, err := svc.SetModelLifecycle(context.Background(), nil, subject.Model.ID, modelLifecycleReview(subject, time.Now(), true).ETag, ModelLifecycleInput{Status: "disabled", Reason: "review"})
	svc.runtime.publication.RUnlock()
	if !errors.Is(err, runtimeUnavailable) {
		t.Fatal("busy writer borrowed database", err)
	}
	svc.runtime.publication.Lock()
	if svc.runtimeModelLifecycleApplied(subject) {
		t.Fatal("busy publication proven")
	}
	svc.runtime.publication.Unlock()
	release, err := svc.pinModelLifecycle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
	release()
	if !svc.runtime.publication.TryLock() {
		t.Fatal("gate retained after duplicate release")
	}
	svc.runtime.publication.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.pinModelLifecycle(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestModelLifecycleArchivedModelBlocksEveryNativeProtocol(t *testing.T) {
	for _, protocol := range []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		t.Run(protocol, func(t *testing.T) {
			var calls atomic.Int32
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer peer.Close()
			svc, data, bearer := runtimeFixture(t, peer.URL+"/v1")
			data.Models[0].Status = entity.ResourceArchived
			data.Connections[0].Protocol = protocol
			svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
			var result *GatewayResult
			var err error
			switch protocol {
			case entity.ProtocolOpenAIChat:
				result, err = svc.GatewayChat(context.Background(), bearer, []byte(teamNativeInput(protocol)), "req_lifecycle_disabled")
			case entity.ProtocolOpenAIResponses:
				result, err = svc.GatewayResponses(context.Background(), bearer, []byte(teamNativeInput(protocol)), "req_lifecycle_disabled")
			case entity.ProtocolAnthropicMessages:
				result, err = svc.GatewayMessages(context.Background(), bearer, []byte(teamNativeInput(protocol)), "req_lifecycle_disabled", MessagesHeaders{Version: "2023-06-01"})
			default:
				result, err = svc.GatewayGemini(context.Background(), bearer, []byte(teamNativeInput(protocol)), "req_lifecycle_disabled", "public-model", false)
			}
			if err == nil || calls.Load() != 0 || result != nil && (result.Admitted || result.AttemptID != "") {
				t.Fatal("disabled Model admitted native work", calls.Load(), err)
			}
		})
	}
}

func TestModelLifecycleDisabledModelBlocksEveryNativeProtocol(t *testing.T) {
	for _, protocol := range []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		t.Run(protocol, func(t *testing.T) {
			var calls atomic.Int32
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer peer.Close()
			svc, data, bearer := runtimeFixture(t, peer.URL+"/v1")
			data.Models[0].Status = entity.ResourceDisabled
			data.Connections[0].Protocol = protocol
			svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
			var result *GatewayResult
			var err error
			switch protocol {
			case entity.ProtocolOpenAIChat:
				result, err = svc.GatewayChat(context.Background(), bearer, []byte(teamNativeInput(protocol)), "req_lifecycle_disabled")
			case entity.ProtocolOpenAIResponses:
				result, err = svc.GatewayResponses(context.Background(), bearer, []byte(teamNativeInput(protocol)), "req_lifecycle_disabled")
			case entity.ProtocolAnthropicMessages:
				result, err = svc.GatewayMessages(context.Background(), bearer, []byte(teamNativeInput(protocol)), "req_lifecycle_disabled", MessagesHeaders{Version: "2023-06-01"})
			default:
				result, err = svc.GatewayGemini(context.Background(), bearer, []byte(teamNativeInput(protocol)), "req_lifecycle_disabled", "public-model", false)
			}
			if err == nil || calls.Load() != 0 || result != nil && (result.Admitted || result.AttemptID != "") {
				t.Fatal("disabled Model admitted native work", calls.Load(), err)
			}
		})
	}
}

func TestModelLifecycleRefreshRejectsBusyAdmission(t *testing.T) {
	for _, stage := range []string{"refresh", "publication", "effects"} {
		t.Run(stage, func(t *testing.T) {
			s, _, _, _ := providerMetadataSQLService(t)
			if err := s.RefreshRuntime(context.Background()); err != nil {
				t.Fatal(err)
			}
			auth, routes, status := s.runtime.auth.Load(), s.runtime.routes.Load(), s.runtime.status.Load()
			var queries atomic.Int32
			if err := s.db.Callback().Query().Before("gorm:query").Register("test:lifecycle_refresh_reads", func(*gorm.DB) { queries.Add(1) }); err != nil {
				t.Fatal(err)
			}
			var unlock func()
			switch stage {
			case "refresh":
				s.runtime.refreshMu.Lock()
				unlock = s.runtime.refreshMu.Unlock
			case "publication":
				s.runtime.publication.Lock()
				unlock = s.runtime.publication.Unlock
			default:
				s.runtime.mu.Lock()
				unlock = s.runtime.mu.Unlock
			}
			var released sync.Once
			finished, joined := make(chan error, 1), make(chan struct{})
			t.Cleanup(func() {
				released.Do(unlock)
				select {
				case <-joined:
				case <-time.After(2 * runtimeRefreshTimeout):
					t.Error("controlled lifecycle refresh did not join")
				}
			})
			go func() {
				defer close(joined)
				finished <- s.refreshRuntime(context.Background(), true)
			}()
			select {
			case err := <-finished:
				if !errors.Is(err, runtimeUnavailable) {
					t.Fatal("busy refresh accepted", stage, err)
				}
			case <-time.After(500 * time.Millisecond):
				t.Fatal("lifecycle refresh queued behind held admission", stage)
			}
			<-joined
			if s.runtime.auth.Load() != auth || s.runtime.routes.Load() != routes || s.runtime.status.Load() != status {
				t.Fatal("rejected refresh changed publication")
			}
			if (queries.Load() > 0) != (stage == "effects") {
				t.Fatal("refresh read occurred at wrong admission stage", stage, queries.Load())
			}
			released.Do(unlock)
			if err := s.refreshRuntime(context.Background(), true); err != nil {
				t.Fatal("busy rejection retained admission locks", stage, err)
			}
		})
	}
}

func TestModelLifecycleRefreshDoesNotQueueBehindEarlierRead(t *testing.T) {
	s, _, _, _ := providerMetadataSQLService(t)
	if err := s.RefreshRuntime(context.Background()); err != nil {
		t.Fatal(err)
	}
	auth, routes, status := s.runtime.auth.Load(), s.runtime.routes.Load(), s.runtime.status.Load()
	gate := newRuntimeRefreshSerializationGate(t, s, false)
	earlier, _ := gate.start(s, context.Background(), "first")
	waitRuntimeRefreshSerializationSignal(t, gate.firstRead, "earlier complete read")
	// The existing refresh holds refreshMu while paused at its complete read.
	// Lifecycle reconciliation must return before that worker is released.
	err := s.refreshRuntime(context.Background(), true)
	unchanged := s.runtime.auth.Load() == auth && s.runtime.routes.Load() == routes && s.runtime.status.Load() == status
	gate.firstReleased.Do(func() { close(gate.releaseFirst) })
	if earlierErr := waitRuntimeRefreshSerializationResult(t, earlier); earlierErr != nil {
		t.Fatal(earlierErr)
	}
	if !errors.Is(err, runtimeUnavailable) || !unchanged {
		t.Fatal("lifecycle refresh queued or changed earlier publication", err)
	}
	if err := s.refreshRuntime(context.Background(), true); err != nil {
		t.Fatal("drained publisher could not reconcile", err)
	}
}

func TestModelLifecycleArchivePreservesAlreadyDispatchedCall(t *testing.T) {
	entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var dispatches atomic.Int32
	var released sync.Once
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if dispatches.Add(1) == 1 {
			close(entered)
		}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"retained"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	t.Cleanup(peer.Close)
	s, data, bearer := runtimeFixture(t, peer.URL+"/v1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	finished := make(chan error, 1)
	t.Cleanup(func() {
		released.Do(func() { close(release) })
		cancel()
		select {
		case <-joined:
		case <-time.After(2 * time.Second):
			t.Error("controlled already-dispatched call did not join")
		}
	})
	go func() {
		defer close(joined)
		result, err := s.GatewayChat(ctx, bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"before archive"}]}`), "req_lifecycle_inflight")
		if err == nil {
			_, err = io.ReadAll(result.Response.Body)
			_ = result.Response.Body.Close()
			if result.Response.StatusCode != http.StatusOK || result.CredentialID != "crd_one" {
				err = errors.New("captured dispatch attribution changed")
			}
		}
		finished <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("controlled native call never dispatched")
	}
	data.Models[0].Status = entity.ResourceArchived
	s.InvalidateRuntimeModel(data.Models[0].ID)
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	if _, err := s.GatewayChat(ctx, bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"after archive"}]}`), "req_lifecycle_after_archive"); err == nil || dispatches.Load() != 1 {
		t.Fatal("archive admitted a new call", err, dispatches.Load())
	}
	released.Do(func() { close(release) })
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal("archive interrupted captured dispatch", err)
		}
	case <-ctx.Done():
		t.Fatal("captured dispatch did not finish")
	}
	<-joined
}
