package cleanupevidence

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func testIdentity(prefix string, n int) string { return fmt.Sprintf("%s_%026d", prefix, n) }
func testJSON(v any) string                    { b, _ := json.Marshal(v); return string(b) }
func fixture(phase string) (Expectation, snapshot) {
	start := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	at := func(n int) time.Time { return start.Add(time.Duration(n) * time.Second) }
	e := Expectation{SourceCommit: strings.Repeat("a", 40), RunID: strings.Repeat("b", 32), Phase: phase, CreationRequestID: "00000000-0000-4000-8000-000000000001", RequestID: "00000000-0000-4000-8000-000000000002", ActorID: testIdentity("usr", 1), ActorBirth: at(1), IntegrationID: testIdentity("vlt", 1), IntegrationBirth: at(2), RevisionID: testIdentity("vlr", 1), CredentialID: testIdentity("crd", 1), CredentialBirth: at(4), CreationBirth: at(5), CreatorProcessID: testIdentity("ins", 1), CreatorGeneration: strings.Repeat("c", 64), CreatorBirth: at(0), PhysicalObject: strings.Repeat("d", 64), OperationProof: strings.Repeat("e", 64), ReviewedETag: strings.Repeat("f", 64) + "." + strings.Repeat("a", 64), Reason: "Controlled exact cleanup", RunStarted: start, AsOf: at(50), RootEpoch: 1}
	empty := observation{DurationMS: "0"}
	success := observation{Attempted: true, Succeeded: true, DurationMS: "1.25"}
	command := commandRow{CreationRequestID: e.CreationRequestID, RequestID: e.RequestID, ActorID: e.ActorID, ActorBirth: e.ActorBirth, IntegrationID: e.IntegrationID, IntegrationBirth: e.IntegrationBirth, RevisionID: e.RevisionID, OperationProof: e.OperationProof, ReviewedETag: e.ReviewedETag, Reason: e.Reason, State: phase, OwnershipJSON: testJSON(empty), CleanupJSON: testJSON(cleanupObservation{State: "not_attempted", Observation: empty}), RootEpoch: 1, StartedAt: at(10), Deadline: at(40)}
	detail := testJSON(map[string]string{"creation_request_id": e.CreationRequestID, "request_id": e.RequestID})
	joined := at(11)
	s := snapshot{operations: []operationRow{{RequestID: e.CreationRequestID, StorageSource: "vault", CredentialID: e.CredentialID, CredentialBirth: e.CredentialBirth, IntegrationID: e.IntegrationID, IntegrationBirth: &e.IntegrationBirth, RevisionID: e.RevisionID, State: "committed", WriteJSON: testJSON(success), ReadJSON: testJSON(success), CreatedAt: e.CreationBirth}}, published: []publishedRow{{Command: command, PhysicalObject: e.PhysicalObject}}, actors: []identityRow{{ID: e.ActorID, CreatedAt: e.ActorBirth}}, integrations: []identityRow{{ID: e.IntegrationID, CreatedAt: e.IntegrationBirth}}, revisions: []revisionRow{{ID: e.RevisionID, IntegrationID: e.IntegrationID, IntegrationBirth: e.IntegrationBirth}}, denials: []denialRow{{PhysicalObject: e.PhysicalObject, CreationRequestID: e.CreationRequestID, RequestID: e.RequestID, RemoteRequestID: &e.RequestID, CreatedAt: at(10)}}, creationUses: []creationUseRow{{CreationRequestID: e.CreationRequestID, ProcessID: e.CreatorProcessID, ProcessGeneration: e.CreatorGeneration}}, processes: []processRow{{ProcessID: e.CreatorProcessID, Generation: e.CreatorGeneration, Birth: e.CreatorBirth, RegisteredAt: e.CreatorBirth}}, uses: []useRow{{PhysicalObject: e.PhysicalObject, ProcessID: e.CreatorProcessID, Generation: e.CreatorGeneration, Birth: e.CreatorBirth, Exposed: true, CreatedAt: at(6), JoinedAt: &joined}}, audits: []auditRow{{ID: testIdentity("aud", 1), ActorID: e.ActorID, Action: "credential.orphan.cleanup.claim", ResourceType: "credential", ResourceID: e.CredentialID, DetailsJSON: &detail, CreatedAt: at(10)}}}
	if phase == "acknowledged" {
		finished := at(12)
		s.published[0].Command.FinishedAt = &finished
		s.published[0].Command.OwnershipJSON = testJSON(success)
		s.published[0].Command.CleanupJSON = testJSON(cleanupObservation{State: "acknowledged", Observation: success})
		s.audits = append(s.audits, auditRow{ID: testIdentity("aud", 2), ActorID: e.ActorID, Action: "credential.orphan.cleanup.observe", ResourceType: "credential", ResourceID: e.CredentialID, DetailsJSON: &detail, CreatedAt: finished})
	}
	return e, s
}

func TestPendingClaimHasExactConsumerFieldsAndNeverRewritesExpiredRow(t *testing.T) {
	e, s := fixture("pending")
	value := derive(e, s)
	if err := Validate(e, value); err != nil {
		t.Fatal("valid pending fixture refused")
	}
	claim, err := PendingClaim(e, value)
	if err != nil {
		t.Fatal("pending consumer projection refused")
	}
	raw, err := json.Marshal(claim)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("claim JSON rejected")
	}
	if len(fields) != 11 || fields["state"] != "pending" || value.PresentedState != "unknown" || claim.FinishedAt != nil || claim.RemoteRequestID != e.RequestID {
		t.Fatal("expiry changed durable tuple or consumer shape")
	}
	for _, key := range []string{"request_id", "creation_request_id", "state", "finished_at", "started_at", "deadline", "observation_state", "observe_audit_count", "claim_audit_count", "remote_request_id", "known_no_effect"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing fixed field %s", key)
		}
	}
	e.AsOf = e.RunStarted.Add(20 * time.Second)
	current := derive(e, s)
	if Validate(e, current) != nil || current.PresentedState != "pending" || current.Claim.State != claim.State || !current.Claim.Deadline.Equal(claim.Deadline) {
		t.Fatal("pending projection changed its original claim")
	}
}

func TestAcknowledgedLedgerIsNotPendingClaimOrRemoteHTTPProof(t *testing.T) {
	e, s := fixture("acknowledged")
	value := derive(e, s)
	if Validate(e, value) != nil || value.PresentedState != "acknowledged" {
		t.Fatal("valid normal ledger refused")
	}
	if _, err := PendingClaim(e, value); !errors.Is(err, ErrRejected) {
		t.Fatal("acknowledged ledger borrowed as pending claim")
	}
	raw, _ := json.Marshal(value)
	for _, forbidden := range []string{"204", "http_status", "normal_control_passed", "runtime_ready", "operation_proof", "reviewed_etag", "ownership_json", "cleanup_json"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("projection claims remote effect or emits private fields")
		}
	}
}

func TestCleanupProofUsesFactsAvailableBeforeOriginalFinish(t *testing.T) {
	t.Run("creation must precede either claim phase", func(t *testing.T) {
		for _, phase := range []string{"pending", "acknowledged"} {
			e, s := fixture(phase)
			e.CreationBirth = s.published[0].Command.StartedAt.Add(time.Microsecond)
			s.operations[0].CreatedAt = e.CreationBirth
			if Validate(e, derive(e, s)) == nil {
				t.Fatal("claim before original creation accepted")
			}
		}
	})
	t.Run("fresh denial belongs to exact original claim", func(t *testing.T) {
		for _, phase := range []string{"pending", "acknowledged"} {
			e, s := fixture(phase)
			s.denials[0].CreatedAt = e.CreationBirth.Add(-time.Second)
			if Validate(e, derive(e, s)) == nil {
				t.Fatal("earlier denial borrowed by fresh original claim")
			}
		}
	})
	cases := []struct {
		name   string
		mutate func(*snapshot)
		valid  bool
	}{
		{"late join", func(s *snapshot) {
			at := s.published[0].Command.FinishedAt.Add(time.Microsecond)
			s.uses[0].JoinedAt = &at
		}, false},
		{"late closure without join", func(s *snapshot) {
			at := s.published[0].Command.FinishedAt.Add(time.Microsecond)
			s.uses[0].JoinedAt = nil
			s.processes[0].ClosedAt = &at
		}, false},
		{"late closure and late join", func(s *snapshot) {
			at := s.published[0].Command.FinishedAt.Add(time.Microsecond)
			s.uses[0].JoinedAt = &at
			s.processes[0].ClosedAt = &at
		}, false},
		{"late claim audit", func(s *snapshot) {
			s.audits[0].CreatedAt = s.published[0].Command.FinishedAt.Add(time.Microsecond)
		}, false},
		{"later shutdown after timely join", func(s *snapshot) {
			at := s.published[0].Command.FinishedAt.Add(time.Microsecond)
			s.processes[0].ClosedAt = &at
		}, true},
		{"timely closure without join", func(s *snapshot) {
			at := *s.published[0].Command.FinishedAt
			s.uses[0].JoinedAt = nil
			s.processes[0].ClosedAt = &at
		}, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			e, s := fixture("acknowledged")
			test.mutate(&s)
			if (Validate(e, derive(e, s)) == nil) != test.valid {
				t.Fatal("original cleanup time ordering was not preserved")
			}
		})
	}
}

func TestExactIdentityBirthAndNamespaceFailuresAreRejected(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Expectation, *snapshot)
	}{
		{"UUID reused", func(e *Expectation, _ *snapshot) { e.RequestID = e.CreationRequestID }},
		{"foreign actor", func(_ *Expectation, s *snapshot) { s.published[0].Command.ActorID = testIdentity("usr", 2) }},
		{"case alias", func(_ *Expectation, s *snapshot) { s.integrations[0].ID = strings.ToUpper(s.integrations[0].ID) }},
		{"actor birth", func(_ *Expectation, s *snapshot) { s.actors[0].CreatedAt = s.actors[0].CreatedAt.Add(time.Microsecond) }},
		{"credential birth", func(_ *Expectation, s *snapshot) {
			s.operations[0].CredentialBirth = s.operations[0].CredentialBirth.Add(time.Microsecond)
		}},
		{"creation birth", func(_ *Expectation, s *snapshot) {
			s.operations[0].CreatedAt = s.operations[0].CreatedAt.Add(time.Microsecond)
		}},
		{"foreign revision", func(_ *Expectation, s *snapshot) { s.revisions[0].IntegrationID = testIdentity("vlt", 2) }},
		{"proof mismatch", func(_ *Expectation, s *snapshot) { s.published[0].Command.OperationProof = strings.Repeat("f", 64) }},
		{"review mismatch", func(_ *Expectation, s *snapshot) {
			s.published[0].Command.ReviewedETag = strings.Repeat("e", 64) + "." + strings.Repeat("f", 64)
		}},
		{"reason mismatch", func(_ *Expectation, s *snapshot) { s.published[0].Command.Reason = "Other intent" }},
		{"orphan namespace", func(_ *Expectation, s *snapshot) { s.orphans = []commandRow{s.published[0].Command} }},
		{"duplicate command", func(_ *Expectation, s *snapshot) { s.published = append(s.published, s.published[0]) }},
		{"live credential", func(e *Expectation, s *snapshot) {
			s.credentials = []identityRow{{ID: e.CredentialID, CreatedAt: e.CredentialBirth}}
		}},
		{"uncommitted", func(_ *Expectation, s *snapshot) { s.operations[0].State = "orphan" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			e, s := fixture("pending")
			test.mutate(&e, &s)
			if !errors.Is(Validate(e, derive(e, s)), ErrRejected) {
				t.Fatal("foreign or ambiguous identity accepted")
			}
		})
	}
}

func TestPendingClaimRefusesUnknownEffectsTimesAndAudits(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*snapshot)
	}{
		{"finished pending", func(s *snapshot) { v := s.published[0].Command.StartedAt; s.published[0].Command.FinishedAt = &v }},
		{"deadline changed", func(s *snapshot) {
			s.published[0].Command.Deadline = s.published[0].Command.Deadline.Add(time.Microsecond)
		}},
		{"known no effect", func(s *snapshot) { s.published[0].KnownNoEffect = true }},
		{"remote absent", func(s *snapshot) { s.denials[0].RemoteRequestID = nil }},
		{"remote different", func(s *snapshot) { v := "00000000-0000-4000-8000-000000000003"; s.denials[0].RemoteRequestID = &v }},
		{"cleanup attempted", func(s *snapshot) {
			s.published[0].Command.CleanupJSON = testJSON(cleanupObservation{State: "unknown", Observation: observation{DurationMS: "0"}})
		}},
		{"observe present", func(s *snapshot) {
			row := s.audits[0]
			row.ID = testIdentity("aud", 2)
			row.Action = "credential.orphan.cleanup.observe"
			s.audits = append(s.audits, row)
		}},
		{"duplicate claim", func(s *snapshot) { s.audits = append(s.audits, s.audits[0]) }},
		{"foreign audit resource", func(s *snapshot) { s.audits[0].ResourceID = testIdentity("crd", 2) }},
		{"wrong audit creation", func(s *snapshot) {
			v := `{"creation_request_id":"00000000-0000-4000-8000-000000000003","request_id":"00000000-0000-4000-8000-000000000002"}`
			s.audits[0].DetailsJSON = &v
		}},
		{"missing source join", func(s *snapshot) { s.uses[0].JoinedAt = nil }},
		{"foreign generation", func(s *snapshot) { s.uses[0].Generation = strings.Repeat("f", 64) }},
		{"missing exposure", func(s *snapshot) { s.uses[0].Exposed = false }},
		{"source overflow", func(s *snapshot) {
			for len(s.processes) <= censusLimit {
				s.processes = append(s.processes, s.processes[0])
			}
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			e, s := fixture("pending")
			test.mutate(&s)
			if Validate(e, derive(e, s)) == nil {
				t.Fatal("unknown or extra effect accepted")
			}
		})
	}
}

func TestDurableJSONRejectsMissingNullDuplicateCaseAndPayloadFields(t *testing.T) {
	for _, raw := range []string{`{"duration_ms":"0"}`, `{"attempted":null,"succeeded":false,"duration_ms":"0","failure":null}`, `{"attempted":false,"attempted":false,"succeeded":false,"duration_ms":"0","failure":null}`, `{"Attempted":false,"succeeded":false,"duration_ms":"0","failure":null}`, `{"attempted":false,"succeeded":false,"duration_ms":"0","failure":null,"secret":"SYNTHETIC_SECRET"}`, `{"attempted":false,"succeeded":false,"duration_ms":"0","failure":{"code":"SYNTHETIC_SECRET"}}`, `{"attempted":false,"succeeded":false,"duration_ms":"0","failure":null} {}`} {
		e, s := fixture("pending")
		s.published[0].Command.OwnershipJSON = raw
		if err := Validate(e, derive(e, s)); !errors.Is(err, ErrRejected) || strings.Contains(fmt.Sprintf("%v", err), "SYNTHETIC") {
			t.Fatal("noncanonical durable JSON accepted or leaked")
		}
	}
}

func TestOutputTamperingAndDiagnosticFormattingAreRefused(t *testing.T) {
	e, s := fixture("pending")
	e.Reason = "SYNTHETIC_PRIVATE_REASON"
	s.published[0].Command.Reason = e.Reason
	value := derive(e, s)
	if Validate(e, value) != nil {
		t.Fatal("controlled reason refused")
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(format, e), "SYNTHETIC") || strings.Contains(fmt.Sprintf(format, value), "SYNTHETIC") {
			t.Fatal("diagnostic formatting leaked private fields")
		}
	}
	raw, _ := json.Marshal(value)
	if strings.Contains(string(raw), "SYNTHETIC") {
		t.Fatal("JSON leaked private reason")
	}
	value.Claim.ClaimAuditCount = 2
	if Validate(e, value) == nil {
		t.Fatal("modified projection accepted")
	}
}

func TestFrozenEmbeddedGORMColumnsMatchSelectedSchema(t *testing.T) {
	parsed, err := schema.Parse(&publishedRow{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal("frozen query schema refused")
	}
	for _, name := range []string{"creation_request_id", "request_id", "actor_birth", "integration_birth", "reviewed_etag", "ownership_json", "cleanup_json", "known_no_effect", "physical_object"} {
		if parsed.LookUpField(name) == nil {
			t.Fatalf("missing exact selected column %s", name)
		}
	}
	if parsed.LookUpField("reviewed_e_tag") != nil {
		t.Fatal("accidental initialism mapping accepted")
	}
}

type refusalDriver struct{ state *transactionState }
type transactionState struct {
	begin, rollback, close atomic.Int32
	options                driver.TxOptions
	rollbackFails          bool
	rows                   *snapshot
}
type refusalConn struct{ state *transactionState }
type refusalTx struct{ state *transactionState }

func (d refusalDriver) Open(string) (driver.Conn, error) { return &refusalConn{state: d.state}, nil }
func (c *refusalConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("SYNTHETIC_PRIVATE_SQL")
}
func (c *refusalConn) Close() error              { c.state.close.Add(1); return nil }
func (c *refusalConn) Begin() (driver.Tx, error) { return nil, ErrRejected }
func (c *refusalConn) BeginTx(_ context.Context, o driver.TxOptions) (driver.Tx, error) {
	c.state.begin.Add(1)
	c.state.options = o
	return &refusalTx{state: c.state}, nil
}
func (c *refusalConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if c.state.rows == nil {
		return nil, errors.New("SYNTHETIC_PRIVATE_DRIVER_ERROR")
	}
	s := c.state.rows
	var records []any
	switch {
	case strings.Contains(query, `FROM "credential_storage_operations"`):
		for _, row := range s.operations {
			records = append(records, row)
		}
	case strings.Contains(query, `FROM "credential_published_cleanups"`):
		for _, row := range s.published {
			records = append(records, row)
		}
	case strings.Contains(query, `FROM "provider_credential_cleanups"`):
		for _, row := range s.orphans {
			records = append(records, row)
		}
	case strings.Contains(query, `FROM "users"`):
		for _, row := range s.actors {
			records = append(records, row)
		}
	case strings.Contains(query, `FROM "vault_integrations"`):
		for _, row := range s.integrations {
			records = append(records, row)
		}
	case strings.Contains(query, `FROM "vault_revisions"`):
		for _, row := range s.revisions {
			records = append(records, row)
		}
	case strings.Contains(query, `FROM "provider_credentials"`):
		for _, row := range s.credentials {
			records = append(records, row)
		}
	case strings.Contains(query, `FROM "credential_source_denials"`):
		for _, row := range s.denials {
			records = append(records, row)
		}
	case strings.Contains(query, `FROM "provider_credential_creation_uses"`):
		for _, row := range s.creationUses {
			records = append(records, row)
		}
	case strings.Contains(query, `FROM "credential_source_processes"`):
		for _, row := range s.processes {
			records = append(records, row)
		}
	case strings.Contains(query, `FROM "credential_source_uses"`):
		for _, row := range s.uses {
			records = append(records, row)
		}
	case strings.Contains(query, `FROM "audit_events"`):
		for _, row := range s.audits {
			records = append(records, row)
		}
	default:
		return nil, ErrRejected
	}
	selectPart, _, ok := strings.Cut(query, " FROM ")
	if !ok || !strings.HasPrefix(selectPart, "SELECT ") {
		return nil, ErrRejected
	}
	columns := strings.Split(strings.TrimPrefix(selectPart, "SELECT "), ",")
	result := &fixtureRows{columns: columns}
	for _, row := range records {
		parsed, err := schema.Parse(row, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			return nil, ErrRejected
		}
		values := make([]driver.Value, len(columns))
		for index, name := range columns {
			field := parsed.LookUpField(strings.TrimSpace(name))
			if field == nil {
				return nil, ErrRejected
			}
			value, _ := field.ValueOf(context.Background(), reflect.ValueOf(row))
			converted, err := driver.DefaultParameterConverter.ConvertValue(value)
			if err != nil {
				return nil, ErrRejected
			}
			values[index] = converted
		}
		result.rows = append(result.rows, values)
	}
	return result, nil
}

type fixtureRows struct {
	columns []string
	rows    [][]driver.Value
	next    int
}

func (r *fixtureRows) Columns() []string { return r.columns }
func (r *fixtureRows) Close() error      { return nil }
func (r *fixtureRows) Next(values []driver.Value) error {
	if r.next >= len(r.rows) {
		return io.EOF
	}
	copy(values, r.rows[r.next])
	r.next++
	return nil
}

func (tx *refusalTx) Commit() error { return errors.New("unexpected commit") }
func (tx *refusalTx) Rollback() error {
	tx.state.rollback.Add(1)
	if tx.state.rollbackFails {
		return errors.New("SYNTHETIC_PRIVATE_ROLLBACK")
	}
	return nil
}

var driverNumber atomic.Int32

func TestActualReadOnlyTransactionAlwaysRollsBackAndKeepsBorrowedPoolOwned(t *testing.T) {
	for _, fails := range []bool{false, true} {
		state := &transactionState{rollbackFails: fails}
		name := fmt.Sprintf("routex_cleanup_evidence_refusal_%d", driverNumber.Add(1))
		sql.Register(name, refusalDriver{state: state})
		pool, err := sql.Open(name, "")
		if err != nil {
			t.Fatal(err)
		}
		db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
		if err != nil {
			_ = pool.Close()
			t.Fatal("owned fake connection initialization failed")
		}
		e, _ := fixture("pending")
		_, err = Project(context.Background(), db, e)
		if !errors.Is(err, ErrRejected) || strings.Contains(fmt.Sprint(err), "SYNTHETIC") || state.begin.Load() != 1 || state.rollback.Load() != 1 || !state.options.ReadOnly || state.options.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) || state.close.Load() != 0 {
			_ = pool.Close()
			t.Fatal("original readonly transaction or caller pool ownership changed")
		}
		if pool.Close() != nil || state.close.Load() != 1 {
			t.Fatal("test-owned pool closure failed")
		}
	}
}

func TestPriorCallerErrorRefusesBeforeBorrowingTransaction(t *testing.T) {
	state := &transactionState{}
	name := fmt.Sprintf("routex_cleanup_evidence_prior_error_%d", driverNumber.Add(1))
	sql.Register(name, refusalDriver{state: state})
	pool, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := pool.Close(); err != nil {
			t.Error("test-owned pool closure failed")
		}
	}()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal("test-owned connection rejected")
	}
	prior := errors.New("SYNTHETIC_PRIVATE_CALLER_ERROR")
	_ = db.AddError(prior)
	e, _ := fixture("pending")
	result, err := Project(context.Background(), db, e)
	if !errors.Is(err, ErrRejected) || strings.Contains(fmt.Sprint(err), "SYNTHETIC") || result.SourceCommit != "" || db.Error != prior || state.begin.Load() != 0 || state.rollback.Load() != 0 || state.close.Load() != 0 {
		t.Fatal("prior caller error borrowed a transaction or changed pool ownership", state.begin.Load(), state.rollback.Load(), state.close.Load())
	}
}

func TestInvalidInputAndCancelledContextRefuseBeforeTransaction(t *testing.T) {
	e, _ := fixture("pending")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, c := range []context.Context{nil, ctx, context.Background()} {
		if _, err := Project(c, nil, e); !errors.Is(err, ErrRejected) {
			t.Fatal("invalid preflight accepted")
		}
	}
	e.Phase = "orphan"
	if validExpectation(e) {
		t.Fatal("unimplemented orphan scope accepted")
	}
}

func TestActualGORMProjectionScansExactRowsAndRefusesUnknownRollback(t *testing.T) {
	for _, phase := range []string{"pending", "acknowledged"} {
		for _, fails := range []bool{false, true} {
			e, rows := fixture(phase)
			state := &transactionState{rollbackFails: fails, rows: &rows}
			name := fmt.Sprintf("routex_cleanup_evidence_projection_%d", driverNumber.Add(1))
			sql.Register(name, refusalDriver{state: state})
			pool, err := sql.Open(name, "")
			if err != nil {
				t.Fatal(err)
			}
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				_ = pool.Close()
				t.Fatal("test-owned connection rejected")
			}
			result, err := Project(context.Background(), db, e)
			if fails {
				if !errors.Is(err, ErrRejected) || result.SourceCommit != "" {
					_ = pool.Close()
					t.Fatal("unknown rollback accepted or partial evidence published")
				}
			} else if err != nil || Validate(e, result) != nil || result.Claim.CreationRequestID != e.CreationRequestID {
				_ = pool.Close()
				t.Fatal("exact GORM projection did not survive frozen row mapping")
			}
			if state.begin.Load() != 1 || state.rollback.Load() != 1 || !state.options.ReadOnly || state.options.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
				_ = pool.Close()
				t.Fatal("readonly transaction/rollback altered")
			}
			if pool.Close() != nil || state.close.Load() != 1 {
				t.Fatal("test-owned pool did not close")
			}
		}
	}
}

func TestProjectedFinishDoesNotAliasRetainedValidationRow(t *testing.T) {
	e, s := fixture("acknowledged")
	value := derive(e, s)
	if Validate(e, value) != nil {
		t.Fatal("normal evidence refused")
	}
	original := *value.rows.published[0].Command.FinishedAt
	*value.Claim.FinishedAt = value.Claim.FinishedAt.Add(time.Microsecond)
	if !value.rows.published[0].Command.FinishedAt.Equal(original) || Validate(e, value) == nil {
		t.Fatal("mutable output changed retained durable validation fact")
	}
}
