package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This in-memory driver executes the real cross-ledger claim and receipt-save
// queries, including transaction rollback. It opens no socket or remote service.
type publishedCleanupData struct {
	processes map[string]entity.CredentialSourceProcess
	uses      map[string]entity.CredentialSourceUse
	instances map[string]entity.SystemInstance
	orphan    map[string]entity.ProviderCredentialCleanup
	published map[string]entity.CredentialPublishedCleanup
	denials   map[string]entity.CredentialSourceDenial
}
type publishedCleanupFixture struct {
	mu                sync.Mutex
	data              publishedCleanupData
	active            *publishedCleanupData
	failSave          bool
	afterInstanceRead func()
}

func (d publishedCleanupData) clone() publishedCleanupData {
	return publishedCleanupData{processes: cloneStorageMap(d.processes), uses: cloneStorageMap(d.uses), instances: cloneStorageMap(d.instances), orphan: cloneStorageMap(d.orphan), published: cloneStorageMap(d.published), denials: cloneStorageMap(d.denials)}
}

type publishedCleanupConnector struct{ f *publishedCleanupFixture }

func (c publishedCleanupConnector) Connect(context.Context) (driver.Conn, error) {
	return &publishedCleanupConnection{f: c.f}, nil
}
func (c publishedCleanupConnector) Driver() driver.Driver { return publishedCleanupDriver(c) }

type publishedCleanupDriver publishedCleanupConnector

func (d publishedCleanupDriver) Open(string) (driver.Conn, error) {
	return publishedCleanupConnector(d).Connect(context.Background())
}

type publishedCleanupConnection struct {
	f      *publishedCleanupFixture
	active bool
}

func (c *publishedCleanupConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unplanned statement")
}
func (c *publishedCleanupConnection) Close() error { return nil }
func (c *publishedCleanupConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *publishedCleanupConnection) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.f.mu.Lock()
	d := c.f.data.clone()
	c.f.active = &d
	c.active = true
	return publishedCleanupTransaction{c}, nil
}

type publishedCleanupTransaction struct{ c *publishedCleanupConnection }

func (t publishedCleanupTransaction) Commit() error {
	t.c.f.data = *t.c.f.active
	t.c.f.active = nil
	t.c.active = false
	t.c.f.mu.Unlock()
	return nil
}
func (t publishedCleanupTransaction) Rollback() error {
	t.c.f.active = nil
	t.c.active = false
	t.c.f.mu.Unlock()
	return nil
}
func (c *publishedCleanupConnection) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, errors.New("unexpected write outside guarded callback")
}
func (c *publishedCleanupConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !c.active {
		c.f.mu.Lock()
		defer c.f.mu.Unlock()
	}
	d := c.f.data
	if c.active {
		d = *c.f.active
	}
	values := rolesSQLStrings(args)
	matches := func(v string) bool {
		for _, x := range values {
			if x == v {
				return true
			}
		}
		return false
	}
	switch {
	case strings.Contains(q, `FROM "governance_settings"`):
		return effectiveSQLRows([]entity.GovernanceSetting{{ID: 1}})
	case strings.Contains(q, `FROM "credential_source_processes"`):
		out := []entity.CredentialSourceProcess{}
		for _, r := range d.processes {
			if matches(r.ProcessID) {
				out = append(out, r)
			}
		}
		return effectiveSQLRows(out)
	case strings.Contains(q, `FROM "credential_source_uses"`):
		out := []entity.CredentialSourceUse{}
		for _, r := range d.uses {
			if matches(r.PhysicalObject) && matches(r.ProcessID) {
				out = append(out, r)
			}
		}
		return effectiveSQLRows(out)
	case strings.Contains(q, `FROM "system_instances"`):
		out := []entity.SystemInstance{}
		for _, r := range d.instances {
			if matches(r.ID) {
				out = append(out, r)
			}
		}
		if c.f.afterInstanceRead != nil {
			c.f.afterInstanceRead()
		}
		return effectiveSQLRows(out)

	case strings.Contains(q, `FROM "provider_credential_cleanups"`):
		out := []entity.ProviderCredentialCleanup{}
		for _, row := range d.orphan {
			if matches(row.RequestID) {
				out = append(out, row)
			}
		}
		return effectiveSQLRows(out)
	case strings.Contains(q, `FROM "credential_published_cleanups"`):
		out := []entity.CredentialPublishedCleanup{}
		for _, row := range d.published {
			if matches(row.RequestID) || matches(row.PhysicalObject) {
				out = append(out, row)
			}
		}
		return effectiveSQLRows(out)
	case strings.Contains(q, `FROM "credential_source_denials"`):
		out := []entity.CredentialSourceDenial{}
		for _, row := range d.denials {
			if matches(row.PhysicalObject) {
				out = append(out, row)
			}
		}
		return effectiveSQLRows(out)
	}
	return nil, errors.New("unexpected query in published ledger proof")
}
func publishedCleanupSQL(t *testing.T) (*Service, *publishedCleanupFixture) {
	t.Helper()
	f := &publishedCleanupFixture{data: publishedCleanupData{processes: map[string]entity.CredentialSourceProcess{}, uses: map[string]entity.CredentialSourceUse{}, instances: map[string]entity.SystemInstance{}, orphan: map[string]entity.ProviderCredentialCleanup{}, published: map[string]entity.CredentialPublishedCleanup{}, denials: map[string]entity.CredentialSourceDenial{}}}
	pool := sql.OpenDB(publishedCleanupConnector{f})
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		if f.active == nil {
			_ = tx.AddError(errors.New("unguarded source creation"))
			return
		}
		switch row := tx.Statement.Dest.(type) {
		case *entity.CredentialSourceProcess:
			if _, ok := f.active.processes[row.ProcessID]; ok {
				_ = tx.AddError(gorm.ErrDuplicatedKey)
				return
			}
			f.active.processes[row.ProcessID] = *row
		case *entity.CredentialSourceUse:
			key := row.PhysicalObject + row.ProcessID
			if _, ok := f.active.uses[key]; ok {
				_ = tx.AddError(gorm.ErrDuplicatedKey)
				return
			}
			f.active.uses[key] = *row
		default:
			_ = tx.AddError(errors.New("unexpected source creation"))
			return
		}
		tx.RowsAffected = 1
	}); err != nil {
		t.Fatal(err)
	}
	if err = db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
		if f.active == nil {
			_ = tx.AddError(errors.New("write outside transaction"))
			return
		}
		if f.failSave {
			_ = tx.AddError(errors.New("controlled durable save failure"))
			return
		}
		tx.Statement.Build("WHERE")
		args := []string{}
		for _, v := range tx.Statement.Vars {
			if s, ok := v.(string); ok {
				args = append(args, s)
			}
		}
		match := func(value string) bool {
			for _, v := range args {
				if value == v {
					return true
				}
			}
			return false
		}
		updates := tx.Statement.Dest.(map[string]any)
		switch tx.Statement.Table {
		case "credential_source_denials":
			for k, row := range f.active.denials {
				if match(k) && row.RemoteRequestID == nil {
					v := updates["remote_request_id"].(string)
					row.RemoteRequestID = &v
					f.active.denials[k] = row
					tx.RowsAffected++
				}
			}
		case "credential_published_cleanups":
			for k, row := range f.active.published {
				if match(k) && row.State == "pending" && !row.KnownNoEffect {
					row.State = updates["state"].(string)
					row.OwnershipJSON = updates["ownership_json"].(string)
					row.CleanupJSON = updates["cleanup_json"].(string)
					row.FinishedAt = updates["finished_at"].(*time.Time)
					row.KnownNoEffect = updates["known_no_effect"].(bool)
					f.active.published[k] = row
					tx.RowsAffected++
				}
			}
		default:
			_ = tx.AddError(errors.New("unexpected ledger update"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	return &Service{db: db}, f
}
func publishedCleanupCommandFixture() (entity.CredentialStorageOperation, entity.ProviderCredentialCleanup, string) {
	at := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	birth := at.Add(-time.Hour)
	op := entity.CredentialStorageOperation{RequestID: "11111111-1111-4111-8111-111111111111", IntegrationID: "vli_00000000000000000000000000", IntegrationBirth: &birth, RevisionID: "vlr_00000000000000000000000000", CreatedAt: at}
	ledger := entity.ProviderCredentialCleanup{CreationRequestID: op.RequestID, RequestID: "22222222-2222-4222-8222-222222222222", ActorID: "usr_exact", ActorBirth: birth, IntegrationID: op.IntegrationID, IntegrationBirth: birth, RevisionID: op.RevisionID, OperationProof: cleanupOperationProof(op), ReviewedETag: rootHash("identity") + "." + rootHash("review"), Reason: "Explicit reviewed cleanup", State: "pending", OwnershipJSON: vaultJSON(vaultEmptyObservation()), CleanupJSON: vaultJSON(VaultCleanupView{"not_attempted", vaultEmptyObservation()}), StartedAt: at, Deadline: at.Add(30 * time.Second)}
	return op, ledger, rootHash("physical")
}
func TestCredentialPublishedCleanupExactCrossLedgerCommandAndReplay(t *testing.T) {
	for _, kind := range []string{"orphan", "published", "ambiguous", "missing"} {
		t.Run(kind, func(t *testing.T) {
			s, f := publishedCleanupSQL(t)
			op, ledger, object := publishedCleanupCommandFixture()
			if kind == "orphan" || kind == "ambiguous" {
				f.data.orphan[ledger.RequestID] = ledger
			}
			if kind == "published" || kind == "ambiguous" {
				f.data.published[ledger.RequestID] = publishedCommand(ledger, object)
			}
			row, published, err := cleanupCommandTx(s.db, ledger.RequestID)
			if kind == "ambiguous" {
				if !errors.Is(err, vaultUnavailable) {
					t.Fatal("ambiguous namespace preferred one receipt", err)
				}
				return
			}
			if kind == "missing" {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					t.Fatal(err)
				}
				return
			}
			if err != nil || published != (kind == "published") || !reflect.DeepEqual(row, ledger) {
				t.Fatal("wrong exact receipt", err)
			}
			actor := entity.User{ID: ledger.ActorID, CreatedAt: ledger.ActorBirth}
			input := ProviderCredentialCleanupInput{RequestID: ledger.RequestID, Reason: ledger.Reason}
			if !cleanupReplay(row, actor, op, ledger.ReviewedETag, input) {
				t.Fatal("exact immutable replay rejected")
			}
			for _, fault := range []string{"actor", "birth", "creation", "reason", "review"} {
				t.Run(fault, func(t *testing.T) {
					a, o, i, r := actor, op, input, ledger.ReviewedETag
					switch fault {
					case "actor":
						a.ID = "usr_foreign"
					case "birth":
						a.CreatedAt = a.CreatedAt.Add(time.Microsecond)
					case "creation":
						o.RequestID = "33333333-3333-4333-8333-333333333333"
					case "reason":
						i.Reason = "Changed intent"
					case "review":
						r = rootHash("new") + "." + rootHash("review")
					}
					if cleanupReplay(row, a, o, r, i) {
						t.Fatal("UUID reused with altered identity/proof")
					}
				})
			}
		})
	}
}
func TestCredentialPublishedCleanupAtomicRemoteClaimAndSaveFailure(t *testing.T) {
	s, f := publishedCleanupSQL(t)
	op, ledger, object := publishedCleanupCommandFixture()
	f.data.published[ledger.RequestID] = publishedCommand(ledger, object)
	f.data.denials[object] = entity.CredentialSourceDenial{PhysicalObject: object, CreationRequestID: op.RequestID, RequestID: ledger.RequestID, CreatedAt: ledger.StartedAt}
	run := func(fn func(*gorm.DB) error) error { return s.db.Transaction(fn) }
	if err := run(func(tx *gorm.DB) error {
		if err := s.claimPublishedRemote(tx, op, ledger, object); err != nil {
			return err
		}
		return errors.New("controlled rollback")
	}); err == nil || f.data.denials[object].RemoteRequestID != nil {
		t.Fatal("remote claim escaped rollback")
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			results <- run(func(tx *gorm.DB) error { return s.claimPublishedRemote(tx, op, ledger, object) })
		})
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 || f.data.denials[object].RemoteRequestID == nil {
		t.Fatal("physical object admitted multiple remote commands")
	}
	now := ledger.StartedAt.Add(time.Second)
	finished := ledger
	finished.State = "failed"
	finished.FinishedAt = &now
	if err := run(func(tx *gorm.DB) error { return s.finishPublishedCleanup(tx, finished, object, true) }); err == nil {
		t.Fatal("remote claim healed as known no effect")
	}
	f.failSave = true
	if err := run(func(tx *gorm.DB) error { return s.finishPublishedCleanup(tx, finished, object, false) }); err == nil {
		t.Fatal("durable failure accepted")
	}
	if row := f.data.published[ledger.RequestID]; row.State != "pending" || row.KnownNoEffect {
		t.Fatal("unknown persistence became failed/retryable")
	}
}
func TestCredentialPublishedCleanupHeldBodyTimeoutThenNewManualIntent(t *testing.T) {
	s, f := publishedCleanupSQL(t)
	op, ledger, object := publishedCleanupCommandFixture()
	f.data.published[ledger.RequestID] = publishedCommand(ledger, object)
	f.data.denials[object] = entity.CredentialSourceDenial{PhysicalObject: object, CreationRequestID: op.RequestID, RequestID: ledger.RequestID, CreatedAt: ledger.StartedAt}
	holder, err := s.credentialSources.acquire(credentialSourceKey{PhysicalObject: object})
	if err != nil {
		t.Fatal(err)
	}
	body := holder.body(io.NopCloser(strings.NewReader("in-flight original response")))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s.credentialSources.closeAndWait(ctx, credentialSourceKey{PhysicalObject: object}) == nil {
		t.Fatal("live body joined")
	}
	now := ledger.StartedAt.Add(time.Second)
	failed := ledger
	failed.State = "failed"
	failed.FinishedAt = &now
	if err := s.db.Transaction(func(tx *gorm.DB) error { return s.finishPublishedCleanup(tx, failed, object, true) }); err != nil {
		t.Fatal(err)
	}
	retained := f.data.published[ledger.RequestID]
	if !publishedKnownNoEffect(retained) || s.credentialSources.joined(object) {
		t.Fatal("no-effect receipt inferred live holder joined")
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	if !s.credentialSources.joined(object) {
		t.Fatal("late body did not join")
	}
	next := ledger
	next.RequestID = "33333333-3333-4333-8333-333333333333"
	next.ReviewedETag = rootHash("identity") + "." + rootHash("fresh review")
	f.data.published[next.RequestID] = publishedCommand(next, object)
	if err := s.db.Transaction(func(tx *gorm.DB) error { return s.claimPublishedRemote(tx, op, next, object) }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(retained, f.data.published[ledger.RequestID]) {
		t.Fatal("new manual intent mutated original failed receipt")
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error { return s.claimPublishedRemote(tx, op, ledger, object) }); err == nil {
		t.Fatal("old UUID executed remote again")
	}
}

func TestCredentialSourceDrainRegistrationPrecedesExposureAndDenialIsPhysical(t *testing.T) {
	s, f := publishedCleanupSQL(t)
	snap, op, object := drainSnapshotFixture()
	instance := snap.Instances[0]
	instance.Role = systemInstanceRole
	instance.LeaseExpiresAt = op.CreatedAt.Add(time.Hour)
	f.data.instances[instance.ID] = instance
	s.instanceNow = func() time.Time { return op.CreatedAt }
	s.rootNow = func() time.Time { return op.CreatedAt }
	s.instance = &systemInstanceLease{id: instance.ID, token: instance.LeaseToken, startedAt: instance.StartedAt}
	if err := s.exposeCredentialSource(context.Background(), object); err == nil || len(f.data.uses) != 0 {
		t.Fatal("pre-registration exposure admitted")
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error { return s.registerCredentialSourceProcess(tx, instance) }); err != nil {
		t.Fatal(err)
	}
	if err := s.exposeCredentialSource(context.Background(), object); err != nil {
		t.Fatal(err)
	}
	before := cloneStorageMap(f.data.uses)
	if err := s.exposeCredentialSource(context.Background(), object); err != nil || !reflect.DeepEqual(before, f.data.uses) {
		t.Fatal("repeat exposure rewrote birth/history", err)
	}
	f.data.denials[object] = entity.CredentialSourceDenial{PhysicalObject: object, CreationRequestID: op.RequestID, RequestID: "22222222-2222-4222-8222-222222222222", CreatedAt: op.CreatedAt}
	// The same canonical physical identity remains denied independently of any
	// subsequently selected logical Credential or auth revision.
	if err := s.exposeCredentialSource(context.Background(), object); err == nil || !reflect.DeepEqual(before, f.data.uses) {
		t.Fatal("durable physical denial admitted source")
	}
	other := rootHash("independent-object")
	if err := s.exposeCredentialSource(context.Background(), other); err != nil {
		t.Fatal("unrelated object blocked", err)
	}
	f.data.instances[instance.ID] = entity.SystemInstance{ID: instance.ID, StartedAt: instance.StartedAt, LeaseToken: "changed-token", Role: systemInstanceRole, LeaseExpiresAt: instance.LeaseExpiresAt}
	if err := s.exposeCredentialSource(context.Background(), rootHash("third-object")); err == nil {
		t.Fatal("changed process token admitted exposure")
	}
}

// Exercise the exact final dispatch seam after previously valid joined history.
// No SDK owner, socket or remote call is constructed by this in-memory proof.
func TestCredentialPublishedCleanupFinalCurrentProcessAuthority(t *testing.T) {
	for _, fault := range []string{"valid", "lease_expired", "stopped", "retired", "token_changed", "birth_changed", "unregistered", "registration_changed", "local_stopped", "local_token_changed", "stop_during_read", "token_during_read", "historical_stopped_joined"} {
		t.Run(fault, func(t *testing.T) {
			s, f := publishedCleanupSQL(t)
			op, ledger, object := publishedCleanupCommandFixture()
			snap, _, _ := drainSnapshotFixture()
			i := snap.Instances[0]
			i.Role = systemInstanceRole
			i.LeaseExpiresAt = ledger.StartedAt.Add(time.Hour)
			p := sourceProcess(i)
			s.instanceNow = func() time.Time { return ledger.StartedAt.Add(time.Second) }
			s.instance = &systemInstanceLease{id: i.ID, token: i.LeaseToken, startedAt: i.StartedAt}
			f.data.instances[i.ID] = i
			f.data.processes[p.ProcessID] = p
			f.data.published[ledger.RequestID] = publishedCommand(ledger, object)
			f.data.denials[object] = entity.CredentialSourceDenial{PhysicalObject: object, CreationRequestID: op.RequestID, RequestID: ledger.RequestID, CreatedAt: ledger.StartedAt}
			// A formerly valid joined snapshot remains historical proof even if
			// this caller loses its separate process dispatch authority later.
			snap.Instances[0] = i
			snap.Processes[0] = p
			snap.CreationUse.CreationRequestID = op.RequestID
			op.CreatedAt = snap.Uses[0].CreatedAt
			joined := op.CreatedAt.Add(time.Second)
			snap.Uses[0].JoinedAt = &joined
			if !credentialDrainSnapshotValid(snap, op, snap.Uses[0].PhysicalObject, true) {
				t.Fatal("initial join not proven")
			}
			switch fault {
			case "lease_expired":
				i.LeaseExpiresAt = ledger.StartedAt
			case "stopped":
				i.StoppedAt = &joined
			case "retired":
				i.RetiredAt = &joined
			case "token_changed":
				i.LeaseToken = "changed-token"
			case "birth_changed":
				i.StartedAt = i.StartedAt.Add(time.Microsecond)
			case "unregistered":
				delete(f.data.processes, p.ProcessID)
			case "registration_changed":
				p.Generation = rootHash("changed-registration")
				f.data.processes[p.ProcessID] = p
			case "local_stopped":
				s.instance = nil
			case "local_token_changed":
				s.instance.token = "changed-token"
			case "stop_during_read":
				f.afterInstanceRead = func() { s.instanceMu.Lock(); s.instance = nil; s.instanceMu.Unlock() }
			case "token_during_read":
				f.afterInstanceRead = func() { s.instanceMu.Lock(); s.instance.token = "changed-token"; s.instanceMu.Unlock() }
			case "historical_stopped_joined":
				old := i
				old.ID = "ins_10000000000000000000000000"
				old.LeaseExpiresAt = ledger.StartedAt
				old.StoppedAt = &joined
				oldProcess := sourceProcess(old)
				snap.Instances = append(snap.Instances, old)
				snap.Processes = append(snap.Processes, oldProcess)
				snap.Uses = append(snap.Uses, entity.CredentialSourceUse{PhysicalObject: snap.Uses[0].PhysicalObject, ProcessID: old.ID, Generation: oldProcess.Generation, Birth: old.StartedAt, CreatedAt: op.CreatedAt, JoinedAt: &joined})
				if !credentialDrainSnapshotValid(snap, op, snap.Uses[0].PhysicalObject, true) {
					t.Fatal("historical joined expired process rejected")
				}
			}
			f.data.instances[i.ID] = i
			before := f.data.published[ledger.RequestID]
			err := s.db.Transaction(func(tx *gorm.DB) error { return s.claimPublishedRemoteCurrent(tx, op, ledger, object) })
			valid := fault == "valid" || fault == "historical_stopped_joined"
			if (err == nil) != valid || (f.data.denials[object].RemoteRequestID != nil) != valid {
				t.Fatal("historical joins conferred invalid current dispatch authority", err)
			}
			if !reflect.DeepEqual(before, f.data.published[ledger.RequestID]) {
				t.Fatal("authority check rewrote immutable command")
			}
			if valid && s.db.Transaction(func(tx *gorm.DB) error { return s.claimPublishedRemoteCurrent(tx, op, ledger, object) }) == nil {
				t.Fatal("second irreversible claim accepted")
			}
		})
	}
}
