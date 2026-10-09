package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

func TestCredentialProcessClosureAllHolderLifetimes(t *testing.T) {
	for _, exclusive := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary_SDK", true: "exclusive_cleanup_SDK"}[exclusive], func(t *testing.T) {
			s := &Service{}
			rev, ref := finiteSourceFixture("https://vault.example.invalid")
			op, err := s.credentialFiniteOperationOwned(rev, ref, "token", "cleanup", exclusive)
			if err != nil {
				t.Fatal(err)
			}
			if (op.holder == nil) != exclusive || op.process == nil {
				t.Fatal("SDK process lifetime omitted")
			}
			s.BeginCredentialSourceShutdown()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if s.credentialSources.joinProcess(ctx) == nil || s.credentialSources.processJoined() {
				t.Fatal("live SDK inferred joined")
			}
			if _, err := s.credentialFiniteOperationOwned(rev, ref, "token", "cleanup", exclusive); err == nil {
				t.Fatal("new SDK admitted during shutdown")
			}
			op.close()
			if err := s.credentialSources.joinProcess(context.Background()); err != nil {
				t.Fatal(err)
			}
			op.close()
			if !s.credentialSources.processJoined() {
				t.Fatal("repeat close corrupted drain")
			}
		})
	}
	var registry credentialSourceHolders
	holder, err := registry.acquire(credentialSourceKey{PhysicalObject: rootHash("native")})
	if err != nil {
		t.Fatal(err)
	}
	body := holder.body(io.NopCloser(strings.NewReader("held original")))
	registry.closeProcessAdmission()
	if holder.admitUse() || registry.processJoined() {
		t.Fatal("closed admission or real body join lost")
	}
	if _, err := registry.acquire(credentialSourceKey{PhysicalObject: rootHash("never_seen")}); err == nil {
		t.Fatal("new physical object bypassed barrier")
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	if registry.joinProcess(context.Background()) != nil {
		t.Fatal("actual native body close not joined")
	}
}
func TestCredentialProcessClosureCancelAndPoisonRemainUnproven(t *testing.T) {
	for _, kind := range []string{"native_Close_error", "SDK_unknown", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			var r credentialSourceHolders
			ticket, _ := r.processAcquire()
			if kind != "cancel" {
				ticket.poison()
			}
			r.closeProcessAdmission()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if r.joinProcess(ctx) == nil {
				t.Fatal("canceled live join succeeded")
			}
			ticket.release()
			if (r.joinProcess(context.Background()) == nil) != (kind == "cancel") {
				t.Fatal("unknown close healed or late real join rejected")
			}
		})
	}
	var r credentialSourceHolders
	h, _ := r.acquire(credentialSourceKey{PhysicalObject: rootHash("idle_poison")})
	h.poison()
	h.release()
	if r.joinProcess(context.Background()) == nil {
		t.Fatal("global poison disappeared with idle physical state")
	}
}
func TestCredentialProcessClosureConcurrentAdmissionAndJoin(t *testing.T) {
	var r credentialSourceHolders
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			<-start
			ticket, err := r.processAcquire()
			if err == nil {
				ticket.release()
			}
		})
	}
	close(start)
	r.closeProcessAdmission()
	wg.Wait()
	if r.joinProcess(context.Background()) != nil {
		t.Fatal("balanced admissions did not join")
	}
	if _, err := r.processAcquire(); err == nil {
		t.Fatal("closed generation reopened")
	}
}
func TestCredentialProcessClosureSnapshotHistoryAndPreview(t *testing.T) {
	snap, op, object := drainSnapshotFixture()
	before := credentialDrainSnapshotProof(snap)
	at := op.CreatedAt.Add(time.Second)
	snap.Processes[0].ClosedAt = &at
	snap.Instances[0].StoppedAt = &at
	if !credentialDrainSnapshotValid(snap, op, object, true) || snap.Uses[0].JoinedAt != nil {
		t.Fatal("whole generation proof must not fabricate object ACK")
	}
	if before == credentialDrainSnapshotProof(snap) {
		t.Fatal("closure omitted from reviewed dependency digest")
	}
	for _, kind := range []string{"zero", "before_birth", "after_close_exposure", "missing_creator", "generation", "invalid_birth", "missing_stop", "stop_time"} {
		t.Run(kind, func(t *testing.T) {
			bad, op, obj := drainSnapshotFixture()
			closed := at
			bad.Processes[0].ClosedAt = &closed
			bad.Instances[0].StoppedAt = &closed
			switch kind {
			case "missing_stop":
				bad.Instances[0].StoppedAt = nil
			case "stop_time":
				wrong := closed.Add(time.Microsecond)
				bad.Instances[0].StoppedAt = &wrong
			case "zero":
				closed = time.Time{}
			case "before_birth":
				closed = bad.Processes[0].Birth.Add(-time.Microsecond)
			case "after_close_exposure":
				bad.Uses[0].CreatedAt = closed.Add(time.Microsecond)
			case "missing_creator":
				bad.Uses = nil
			case "generation":
				bad.Processes[0].Generation = rootHash("wrong")
			case "invalid_birth":
				bad.Processes[0].Birth = bad.Processes[0].Birth.Add(time.Microsecond)
			}
			if credentialDrainSnapshotValid(bad, op, obj, true) {
				t.Fatal("invalid closure/history accepted")
			}
		})
	}
}
func TestCredentialProcessClosurePersistAtomicAndExact(t *testing.T) {
	for _, fault := range []string{"valid", "closed_reconcile", "lease_expired", "token_changed", "local_pointer", "save_error", "instance_save_error", "poison", "canceled_held", "registered_at", "retired", "lease_expires_after_instance_read", "live_clock_advances_after_instance_read", "closed_reconcile_expired_clock"} {
		t.Run(fault, func(t *testing.T) {
			s, f := publishedCleanupSQL(t)
			snap, op, _ := drainSnapshotFixture()
			i := snap.Instances[0]
			i.Role = systemInstanceRole
			i.LeaseExpiresAt = op.CreatedAt.Add(time.Hour)
			p := sourceProcess(i)
			now := op.CreatedAt.Add(time.Second)
			expectedClosedAt := now.UTC().Truncate(time.Microsecond)
			s.instanceNow = func() time.Time { return now }
			s.instanceResources = func(string) entitySystemInstanceResources { return entitySystemInstanceResources{} }
			done := make(chan struct{})
			close(done)
			lease := &systemInstanceLease{id: i.ID, token: i.LeaseToken, startedAt: i.StartedAt, cancel: func() {}, done: done}
			s.instance = lease
			var held *credentialProcessTicket
			switch fault {
			case "canceled_held":
				held, _ = s.credentialSources.processAcquire()
			case "registered_at":
				p.RegisteredAt = p.RegisteredAt.Add(time.Microsecond)
			case "retired":
				i.RetiredAt = &now
			case "closed_reconcile":
				p.ClosedAt = &now
				i.StoppedAt = &now
			case "lease_expired":
				i.LeaseExpiresAt = now
			case "lease_expires_after_instance_read":
				i.LeaseExpiresAt = now.Add(time.Microsecond)
				f.afterInstanceRead = func() { now = i.LeaseExpiresAt }
			case "live_clock_advances_after_instance_read":
				f.afterInstanceRead = func() {
					now = now.Add(time.Second + 500*time.Nanosecond)
					expectedClosedAt = now.UTC().Truncate(time.Microsecond)
				}
			case "closed_reconcile_expired_clock":
				closed := expectedClosedAt
				p.ClosedAt, i.StoppedAt = &closed, &closed
				i.LeaseExpiresAt = closed
				f.afterInstanceRead = func() { now = now.Add(time.Hour) }
			case "token_changed":
				i.LeaseToken = "changed"
			case "local_pointer":
				f.afterInstanceRead = func() { s.instanceMu.Lock(); s.instance = nil; s.instanceMu.Unlock() }
			case "save_error":
				f.failSave = true
			case "poison":
				ticket, _ := s.credentialSources.processAcquire()
				ticket.poison()
				ticket.release()
			}
			f.data.processes[p.ProcessID] = p
			f.data.instances[i.ID] = i
			original := s.db.Callback().Update().Get("gorm:update")
			if err := s.db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
				if tx.Statement.Table != "credential_source_processes" && tx.Statement.Table != "system_instances" {
					original(tx)
					return
				}
				if f.active == nil {
					_ = tx.AddError(errors.New("unguarded closure"))
					return
				}
				if f.failSave || fault == "instance_save_error" && tx.Statement.Table == "system_instances" {
					_ = tx.AddError(errors.New("controlled closure persistence failure"))
					return
				}
				updates := tx.Statement.Dest.(map[string]any)
				switch tx.Statement.Table {
				case "credential_source_processes":
					row := f.active.processes[p.ProcessID]
					v := updates["closed_at"].(time.Time)
					row.ClosedAt = &v
					f.active.processes[p.ProcessID] = row
				case "system_instances":
					row := f.active.instances[i.ID]
					v := updates["stopped_at"].(time.Time)
					row.StoppedAt = &v
					row.LeaseExpiresAt = v
					f.active.instances[i.ID] = row
				}
				tx.RowsAffected = 1
			}); err != nil {
				t.Fatal(err)
			}
			stopContext := context.Background()
			if fault == "canceled_held" {
				canceled, cancel := context.WithCancel(stopContext)
				cancel()
				stopContext = canceled
			}
			err := s.StopSystemInstance(stopContext)
			valid := fault == "valid" || fault == "closed_reconcile" || fault == "live_clock_advances_after_instance_read" || fault == "closed_reconcile_expired_clock"
			if (err == nil) != valid {
				t.Fatal("closure outcome", fault, err)
			}
			if valid {
				if f.data.processes[p.ProcessID].ClosedAt == nil || f.data.instances[i.ID].StoppedAt == nil || s.instance != nil {
					t.Fatal("atomic closed/stopped proof missing")
				}
				if !f.data.processes[p.ProcessID].ClosedAt.Equal(expectedClosedAt) || !f.data.instances[i.ID].StoppedAt.Equal(expectedClosedAt) {
					t.Fatal("closure must use one fresh timestamp or retain its exact durable timestamp")
				}
			} else if f.data.processes[p.ProcessID].ClosedAt != nil || f.data.instances[i.ID].StoppedAt != nil {
				t.Fatal("failure persisted positive proof")
			}
			if fault == "lease_expires_after_instance_read" && (!s.credentialSources.processAdmissionClosed() || s.instance != lease) {
				t.Fatal("expiry during governance reads reopened admission or discarded the unproven generation")
			}
			if fault == "canceled_held" || fault == "save_error" {
				if !s.credentialSources.processAdmissionClosed() || s.instance != lease {
					t.Fatal("failed stop reopened admission or discarded exact generation")
				}
				held.release()
				f.failSave = false
				if err := s.StopSystemInstance(context.Background()); err != nil {
					t.Fatal("actual late join or exact failed-persistence retry", err)
				}
				if f.data.processes[p.ProcessID].ClosedAt == nil || f.data.instances[i.ID].StoppedAt == nil {
					t.Fatal("positive retry did not persist exact joined proof")
				}
			}
		})
	}
}

func TestCredentialProcessClosureHeldCloseAndExclusiveUnknown(t *testing.T) {
	var r credentialSourceHolders
	h, err := r.acquire(credentialSourceKey{PhysicalObject: rootHash("held_close")})
	if err != nil {
		t.Fatal(err)
	}
	raw := &sourceBlockingClose{entered: make(chan struct{}), finish: make(chan struct{})}
	body := h.body(raw)
	result := make(chan error, 1)
	go func() { result <- body.Close() }()
	<-raw.entered
	r.closeProcessAdmission()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r.joinProcess(ctx) == nil || r.processJoined() {
		t.Fatal("underlying Close had not joined")
	}
	close(raw.finish)
	if <-result == nil {
		t.Fatal("controlled Close error lost")
	}
	if r.joinProcess(context.Background()) == nil {
		t.Fatal("failed body Close became process proof")
	}
	s := &Service{}
	rev, ref := finiteSourceFixture("https://vault.example.invalid")
	op, err := s.credentialFiniteOperationOwned(rev, ref, "token", "cleanup", true)
	if err != nil {
		t.Fatal(err)
	}
	if op.observe(true, nil) == nil {
		t.Fatal("unobserved attempted SDK response accepted")
	}
	op.close()
	if s.credentialSources.joinProcess(context.Background()) == nil {
		t.Fatal("exclusive unknown SDK observer omitted from generation poison")
	}
}
func TestCredentialProcessClosureHeldFiniteOperationAndTicketOwnership(t *testing.T) {
	s := &Service{}
	rev, ref := finiteSourceFixture("https://vault.example.invalid")
	op, err := s.credentialFiniteOperationOwned(rev, ref, "token", "cleanup", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.credentialFiniteOperationTicket(rev, ref, "token", "cleanup", true, op.process); err == nil {
		t.Fatal("one generation ticket borrowed by another SDK owner")
	}
	// A finite Reader/SDK method owns this mutex through its synchronous response
	// close. Its real closing callback cannot release the lifetime before that join.
	op.mu.Lock()
	closed := make(chan struct{})
	go func() { op.close(); close(closed) }()
	s.BeginCredentialSourceShutdown()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s.credentialSources.joinProcess(ctx) == nil || s.credentialSources.processJoined() {
		t.Fatal("held finite operation inferred closed")
	}
	op.mu.Unlock()
	<-closed
	if s.credentialSources.joinProcess(context.Background()) != nil {
		t.Fatal("actual finite owner close not joined")
	}
}

func TestCredentialProcessClosureClosedHistoryNeverGrantsCaller(t *testing.T) {
	for _, fault := range []string{"durably_closed", "admission_closed"} {
		t.Run(fault, func(t *testing.T) {
			s, f := publishedCleanupSQL(t)
			op, ledger, object := publishedCleanupCommandFixture()
			snap, _, _ := drainSnapshotFixture()
			i := snap.Instances[0]
			i.Role = systemInstanceRole
			i.LeaseExpiresAt = ledger.StartedAt.Add(time.Hour)
			p := sourceProcess(i)
			at := op.CreatedAt.Add(time.Second)
			s.instanceNow = func() time.Time { return at }
			s.instance = &systemInstanceLease{id: i.ID, token: i.LeaseToken, startedAt: i.StartedAt}
			if fault == "durably_closed" {
				p.ClosedAt = &at
				i.StoppedAt = &at
			} else {
				s.BeginCredentialSourceShutdown()
			}
			f.data.instances[i.ID] = i
			f.data.processes[p.ProcessID] = p
			f.data.published[ledger.RequestID] = publishedCommand(ledger, object)
			f.data.denials[object] = entity.CredentialSourceDenial{PhysicalObject: object, CreationRequestID: op.RequestID, RequestID: ledger.RequestID, CreatedAt: ledger.StartedAt}
			if s.db.Transaction(func(tx *gorm.DB) error { return s.claimPublishedRemoteCurrent(tx, op, ledger, object) }) == nil || f.data.denials[object].RemoteRequestID != nil {
				t.Fatal("closed history conferred current irreversible dispatch authority")
			}
		})
	}
}
