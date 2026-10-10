package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This closed SQL fixture executes the normal ensureQuotaActive transaction and
// GORM timestamp update. It emulates only the singleton calendar row, not a real
// database or RefreshRuntime's repeatable-read loader.
type accountingFreezeSQL struct {
	setting          entity.QuotaSetting
	updates, commits int
}
type accountingFreezeConnector struct{ state *accountingFreezeSQL }

func (c accountingFreezeConnector) Connect(context.Context) (driver.Conn, error) {
	return &accountingFreezeConnection{state: c.state}, nil
}
func (c accountingFreezeConnector) Driver() driver.Driver { return accountingFreezeDriver(c) }

type accountingFreezeDriver accountingFreezeConnector

func (d accountingFreezeDriver) Open(string) (driver.Conn, error) {
	return accountingFreezeConnector(d).Connect(context.Background())
}

type accountingFreezeConnection struct {
	state   *accountingFreezeSQL
	pending *entity.QuotaSetting
}

func (c *accountingFreezeConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected accounting statement preparation")
}
func (c *accountingFreezeConnection) Close() error { return nil }
func (c *accountingFreezeConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *accountingFreezeConnection) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if c.pending != nil {
		return nil, errors.New("nested accounting transaction")
	}
	row := c.state.setting
	c.pending = &row
	return accountingFreezeTransaction{c}, nil
}

type accountingFreezeTransaction struct{ c *accountingFreezeConnection }

func (tx accountingFreezeTransaction) Commit() error {
	if tx.c.pending == nil {
		return errors.New("missing accounting transaction")
	}
	tx.c.state.setting = *tx.c.pending
	tx.c.pending = nil
	tx.c.state.commits++
	return nil
}
func (tx accountingFreezeTransaction) Rollback() error { tx.c.pending = nil; return nil }
func (c *accountingFreezeConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !strings.Contains(query, `FROM "quota_settings"`) || len(args) < 1 || args[0].Value != int64(1) {
		return nil, errors.New("unexpected accounting read")
	}
	row := c.state.setting
	if c.pending != nil {
		row = *c.pending
	}
	return effectiveSQLRows([]entity.QuotaSetting{row})
}
func (c *accountingFreezeConnection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if c.pending == nil || !strings.HasPrefix(query, `UPDATE "quota_settings" SET `) {
		return nil, errors.New("unexpected accounting write")
	}
	set, _, ok := strings.Cut(strings.TrimPrefix(query, `UPDATE "quota_settings" SET `), " WHERE ")
	columns := strings.Split(set, ",")
	if !ok || len(columns) != 2 || len(args) != 3 || args[2].Value != int64(1) {
		return nil, errors.New("unexpected accounting update shape")
	}
	seen := map[string]bool{}
	for i, column := range columns {
		name, _, found := strings.Cut(column, "=")
		name = strings.Trim(name, " \"")
		if !found || seen[name] {
			return nil, errors.New("ambiguous accounting update")
		}
		seen[name] = true
		switch name {
		case "accounting_started":
			value, valid := args[i].Value.(bool)
			if !valid || !value {
				return nil, errors.New("unexpected accounting activation")
			}
			c.pending.AccountingStarted = value
		case "updated_at":
			value, valid := args[i].Value.(time.Time)
			if !valid || value.IsZero() {
				return nil, errors.New("missing accounting timestamp")
			}
			c.pending.UpdatedAt = value
		default:
			return nil, errors.New("unexpected accounting column")
		}
	}
	if !seen["accounting_started"] || !seen["updated_at"] {
		return nil, errors.New("incomplete accounting update")
	}
	c.state.updates++
	return driver.RowsAffected(1), nil
}

func accountingRouteDigest(t *testing.T, data *runtimeData) string {
	t.Helper()
	digest, err := runtimeDigest(data)
	if err != nil || len(digest) != 64 {
		t.Fatal("route digest prerequisite", err)
	}
	return digest
}

func TestRuntimeAccountingActivationPreservesPreparedDispatch(t *testing.T) {
	for _, changePolicy := range []bool{false, true} {
		name := "accounting_only"
		if changePolicy {
			name = "calendar_revision_still_invalidates"
		}
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-upstream-secret" {
					t.Error("unexpected native dispatch")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"model":"provider-model","choices":[]}`)
			}))
			defer server.Close()
			svc, data, bearer := runtimeFixture(t, server.URL+"/v1")
			defer svc.upstream.CloseIdleConnections()
			defer func() { closeRuntimeClients(svc.runtime.routes.Load().Models) }()
			birth := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			data.Quota.Setting.ID = 1
			data.Quota.Setting.UpdatedAt = time.Now().UTC().Add(-time.Hour)
			data.Keys[0].CreatedAt = birth
			data.Keys[0].LifecycleRevision = "0"
			data.Quota.Created[limitAccount("key", data.Keys[0].ID)] = birth
			data.Quota.Created[limitAccount("user", data.Users[0].ID)] = data.Users[0].CreatedAt
			beforeSetting := data.Quota.Setting
			state := &accountingFreezeSQL{setting: beforeSetting}
			pool := sql.OpenDB(accountingFreezeConnector{state})
			defer func() { _ = pool.Close() }()
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
			if err != nil {
				t.Fatal(err)
			}
			svc.db = db
			queue, err := eventqueue.Open(filepath.Join(t.TempDir(), "accounting.db"), 8, callQueuePayloadLimit)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := queue.Close(); err != nil {
					t.Error(err)
				}
			}()
			// No delivery worker: the normal admission itself owns activation/fsync.
			svc.recorder = &callRecorder{queue: queue}
			digest := accountingRouteDigest(t, data)
			initial := *svc.runtime.routes.Load()
			initial.Digest = digest
			svc.runtime.routes.Store(&initial)
			auth := buildRuntimeAuthorization(data, time.Now().Add(time.Minute))
			auth.SourceDigest = digest
			svc.runtime.auth.Store(auth)
			commitment := projectionDigest(t, auth)
			auth.installationProjectionDigest = commitment
			hookCalls := 0
			svc.afterGatewayAdmission = func() {
				hookCalls++
				// Admission has already called actual ensureQuotaActive. Read its
				// committed row, then force the RefreshRuntime publication interleave.
				var setting entity.QuotaSetting
				if err := svc.authDB(t.Context()).First(&setting, 1).Error; err != nil {
					t.Fatal(err)
				}
				expected := beforeSetting
				expected.AccountingStarted = true
				expected.UpdatedAt = setting.UpdatedAt
				if !setting.AccountingStarted || !setting.UpdatedAt.After(beforeSetting.UpdatedAt) || !reflect.DeepEqual(setting, expected) || state.updates != 1 || state.commits != 1 {
					t.Fatal("normal accounting activation prerequisite failed")
				}
				next := *data
				quota := *data.Quota
				next.Quota = &quota
				next.Quota.Setting = setting
				if changePolicy {
					next.Quota.Setting.ETag = "changed-calendar-revision"
				}
				nextDigest := accountingRouteDigest(t, &next)
				nextAuth := buildRuntimeAuthorization(&next, auth.ValidUntil)
				nextAuth.SourceDigest = nextDigest
				nextCommitment := projectionDigest(t, nextAuth)
				if nextCommitment == commitment {
					t.Fatal("accounting freeze disappeared from authorization commitment")
				}
				nextAuth.installationProjectionDigest = nextCommitment
				if auth.Quota.Setting != beforeSetting || data.Quota.Setting != beforeSetting {
					t.Fatal("published source mutated")
				}
				svc.runtime.publication.Lock()
				defer svc.runtime.publication.Unlock()
				svc.runtime.auth.Store(nextAuth)
				// Match the production digest decision. The original source must
				// install a new actual cfg here, and pinConnectionDispatch rejects it.
				current := svc.runtime.routes.Load()
				if current.Digest != nextDigest {
					routes, err := svc.buildRuntimeRoutes(&next)
					if err != nil {
						t.Fatal(err)
					}
					cfg, err := id.NewPrefixed("cfg")
					if err != nil {
						closeRuntimeClients(routes)
						t.Fatal(err)
					}
					svc.runtime.routes.Store(&runtimeRoutes{ID: cfg, Digest: nextDigest, PublishedAt: time.Now().UTC(), Models: routes})
					closeRuntimeClients(current.Models)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			result, err := svc.GatewayChat(ctx, bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`), "req_accounting_freeze")
			if result != nil && result.Response != nil {
				defer func() { _ = result.Response.Body.Close() }()
			}
			if hookCalls != 1 {
				t.Fatal("did not cross exactly one admission boundary")
			}
			if svc.runtime.auth.Load().installationProjectionDigest == commitment {
				t.Fatal("current authorization retained old accounting commitment")
			}
			status, statusErr := queue.QuotaStatus()
			if statusErr != nil || !status.Active || status.TimeZone != "UTC" {
				t.Fatal("durable accounting activation missing", statusErr)
			}
			if changePolicy {
				if err == nil || calls.Load() != 0 || svc.runtime.routes.Load().ID == initial.ID || result == nil || result.AttemptID != "" || len(result.Attempts) != 0 {
					t.Fatal("calendar revision no longer invalidates prepared dispatch")
				}
				return
			}
			if err != nil || result == nil || result.Response == nil || result.Response.StatusCode != http.StatusOK || calls.Load() != 1 {
				t.Fatal("accounting-only activation rejected or replayed dispatch", err, calls.Load())
			}
			if result.SnapshotID != initial.ID || svc.runtime.routes.Load() != &initial || svc.runtime.routes.Load().Digest != digest || result.AttemptID == "" || len(result.Attempts) != 0 {
				t.Fatal("accounting-only activation changed route generation or attempt identity")
			}
		})
	}
}

func TestRuntimeRouteDigestKeepsQuotaSecurityInputs(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*runtimeQuotaData)
	}{
		{"id", func(q *runtimeQuotaData) { q.Setting.ID++ }},
		{"zone", func(q *runtimeQuotaData) { q.Setting.TimeZone = "Asia/Shanghai" }},
		{"revision", func(q *runtimeQuotaData) { q.Setting.ETag = "new" }},
		{"previous_revision", func(q *runtimeQuotaData) { q.Setting.PreviousETag = "old" }},
		{"actor", func(q *runtimeQuotaData) { q.Setting.ActorID = "usr_other" }},
		{"reason", func(q *runtimeQuotaData) { q.Setting.Reason = "reviewed" }},
		{"currency", func(q *runtimeQuotaData) { q.Currency = "CNY" }},
		{"birth", func(q *runtimeQuotaData) { q.Created["key_one"] = q.Created["key_one"].Add(time.Microsecond) }},
		{"policy_revision", func(q *runtimeQuotaData) { q.Revisions["key_one"] = "new" }},
		{"bound_identity", func(q *runtimeQuotaData) {
			b := q.Bounds["pmd_one"]
			b.ProviderModelID = "pmd_other"
			q.Bounds["pmd_one"] = b
		}},
		{"bound_protocol", func(q *runtimeQuotaData) {
			b := q.Bounds["pmd_one"]
			b.Protocol = entity.ProtocolOpenAIResponses
			q.Bounds["pmd_one"] = b
		}},
		{"bound_transport", func(q *runtimeQuotaData) {
			b := q.Bounds["pmd_one"]
			b.TransportGeneration = "next"
			q.Bounds["pmd_one"] = b
		}},
		{"bound_input", func(q *runtimeQuotaData) { b := q.Bounds["pmd_one"]; b.MaxInputTokens++; q.Bounds["pmd_one"] = b }},
		{"bound_output", func(q *runtimeQuotaData) { b := q.Bounds["pmd_one"]; b.MaxOutputTokens++; q.Bounds["pmd_one"] = b }},
		{"bound_revision", func(q *runtimeQuotaData) { b := q.Bounds["pmd_one"]; b.ETag = "next"; q.Bounds["pmd_one"] = b }},
		{"bound_evidence", func(q *runtimeQuotaData) { b := q.Bounds["pmd_one"]; b.Evidence = "reviewed"; q.Bounds["pmd_one"] = b }},
		{"bound_previous_revision", func(q *runtimeQuotaData) { b := q.Bounds["pmd_one"]; b.PreviousETag = "old"; q.Bounds["pmd_one"] = b }},
		{"bound_actor", func(q *runtimeQuotaData) { b := q.Bounds["pmd_one"]; b.ActorID = "usr_other"; q.Bounds["pmd_one"] = b }},
		{"bound_reason", func(q *runtimeQuotaData) { b := q.Bounds["pmd_one"]; b.Reason = "reviewed"; q.Bounds["pmd_one"] = b }},
		{"bound_timestamp", func(q *runtimeQuotaData) {
			b := q.Bounds["pmd_one"]
			b.UpdatedAt = time.Now().UTC()
			q.Bounds["pmd_one"] = b
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, data, _ := runtimeFixture(t, "http://127.0.0.1")
			defer func() { closeRuntimeClients(svc.runtime.routes.Load().Models) }()
			data.Quota.Created["key_one"] = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			data.Quota.Revisions["key_one"] = "0"
			data.Quota.Bounds["pmd_one"] = entity.ReservationBound{ProviderModelID: "pmd_one", Protocol: entity.ProtocolOpenAIChat, TransportGeneration: "0", MaxInputTokens: 100, MaxOutputTokens: 50, ETag: "0"}
			digest := accountingRouteDigest(t, data)
			tc.mutate(data.Quota)
			if accountingRouteDigest(t, data) == digest {
				t.Fatal("quota security input excluded from route digest")
			}
		})
	}
}

func TestRuntimeRouteDigestAccountingMetadataAndAuthorization(t *testing.T) {
	svc, data, _ := runtimeFixture(t, "http://127.0.0.1")
	defer func() { closeRuntimeClients(svc.runtime.routes.Load().Models) }()
	digest := accountingRouteDigest(t, data)
	auth := buildRuntimeAuthorization(data, time.Now().Add(time.Minute))
	auth.SourceDigest = digest
	commitment := projectionDigest(t, auth)
	next := *data
	quota := *data.Quota
	next.Quota = &quota
	next.Quota.Setting.UpdatedAt = time.Now().UTC()
	if accountingRouteDigest(t, &next) != digest {
		t.Fatal("accounting timestamp changed route identity")
	}
	nextAuth := buildRuntimeAuthorization(&next, auth.ValidUntil)
	nextAuth.SourceDigest = digest
	if projectionDigest(t, nextAuth) != commitment {
		t.Fatal("write timestamp became authorization source")
	}
	next.Quota.Setting.AccountingStarted = true
	if accountingRouteDigest(t, &next) != digest {
		t.Fatal("accounting freeze changed route identity")
	}
	nextAuth = buildRuntimeAuthorization(&next, auth.ValidUntil)
	nextAuth.SourceDigest = digest
	if projectionDigest(t, nextAuth) == commitment {
		t.Fatal("freeze marker omitted from authorization source")
	}
	if data.Quota.Setting.AccountingStarted || !data.Quota.Setting.UpdatedAt.IsZero() {
		t.Fatal("route projection mutated original setting")
	}
}
