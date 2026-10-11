package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// The original integration owner supplies both real drivers and its disposable
// database. Registration is a separate prospective suffix, not a second harness.
func testRuntimePublicationCommandMigration(t *testing.T, db *gorm.DB) {
	// Retain the exact V105-only migration controls after the newer pristine overlay.
	legacyTelegramBeforeV106(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db = db.WithContext(ctx)
	model := &entity.RuntimePublicationCommand{}
	original := personalKeyBehaviorLedger(t, db)
	if len(original) != 105 {
		t.Fatal("exact V105 ledger required")
	}
	for i, row := range original {
		if row.Version != i+1 {
			t.Fatal("noncontiguous V105 ledger")
		}
	}
	var count int64
	if db.Model(model).Count(&count).Error != nil || count != 0 {
		t.Fatal("migration fabricated manual publication receipts")
	}
	prefix := append([]personalKeyBehaviorMigrationEntry(nil), original[:104]...)
	runtimePublicationHistoricalGuardControlsV105(t, db, original)
	dropReceipt := func() {
		t.Helper()
		r := db.Table("schema_migrations").Where("version = ?", 105).Delete(&struct{}{})
		if r.Error != nil || r.RowsAffected != 1 {
			t.Fatal("remove only owned V105 receipt")
		}
	}
	checkLedger := func() {
		t.Helper()
		rows := personalKeyBehaviorLedger(t, db)
		if len(rows) != 105 || rows[104].Version != 105 || !reflect.DeepEqual(prefix, rows[:104]) {
			t.Fatal("V105 changed complete historical receipt prefix")
		}
	}
	migrate := func() {
		t.Helper()
		if database.MigrateThrough(ctx, db, 105) != nil {
			t.Fatal("V105 migration failed")
		}
		checkLedger()
	}
	if db.Migrator().DropTable(model) != nil {
		t.Fatal("drop owned V105 table")
	}
	dropReceipt()
	if database.MigrateThrough(ctx, db, 104) != nil || db.Migrator().HasTable(model) {
		t.Fatal("bounded V104 fabricated manual commands")
	}
	migrate()
	migrate()
	dropReceipt()
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for range 2 {
		wg.Go(func() { failures <- database.MigrateThrough(ctx, db, 105) })
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal("concurrent V105 startup failed")
		}
	}
	checkLedger()
	birth := time.Now().UTC().Truncate(time.Microsecond)
	row := entity.RuntimePublicationCommand{RequestID: "1f739b12-12ce-4a20-8da7-04bc8885dfb8", ActorID: "usr_01m36yee4gkbns18pfcqqc75a3", ActorCreatedAt: birth, Reason: "Retained original manual refresh", ClaimInstanceID: "ins_01m36yee4gkbns18pfcqqc75a3", ClaimInstanceStartedAt: birth, ClaimedAt: birth, Outcome: "claimed"}
	if db.Create(&row).Error != nil {
		t.Fatal("valid claimed receipt rejected")
	}
	saved := runtimePublicationStoredV105(t, db, row.RequestID)
	final := birth.Add(time.Microsecond)
	code := "runtime_refresh_failed"
	for i, outcome := range []string{"refresh_returned_nil", "refresh_returned_error"} {
		terminal := row
		terminal.RequestID = []string{"2f739b12-12ce-4a20-8da7-04bc8885dfb8", "3f739b12-12ce-4a20-8da7-04bc8885dfb8"}[i]
		terminal.FinalizedAt = &final
		terminal.Outcome = outcome
		if i == 0 {
			terminal.FinalizedAt = &birth
		} else {
			terminal.FailureCode = &code
		}
		if db.Create(&terminal).Error != nil {
			t.Fatal("valid terminal receipt rejected")
		}
	}
	if db.Create(&row).Error == nil {
		t.Fatal("duplicate request identity admitted")
	}
	for _, which := range []string{"future", "uppercase", "trailing_space", "claimed_finalized", "claimed_failure", "nil_without_final", "nil_with_failure", "error_without_final", "error_without_code", "error_wrong_code", "error_trailing_code", "final_before_claim"} {
		bad := row
		bad.RequestID = "4f739b12-12ce-4a20-8da7-04bc8885dfb8"
		switch which {
		case "future":
			bad.Outcome = "completed"
		case "uppercase":
			bad.Outcome = "CLAIMED"
		case "trailing_space":
			bad.Outcome = "claimed "
		case "claimed_finalized":
			bad.FinalizedAt = &final
		case "claimed_failure":
			bad.FailureCode = &code
		case "nil_without_final":
			bad.Outcome = "refresh_returned_nil"
		case "nil_with_failure":
			bad.Outcome = "refresh_returned_nil"
			bad.FinalizedAt = &final
			bad.FailureCode = &code
		case "error_without_final":
			bad.Outcome = "refresh_returned_error"
			bad.FailureCode = &code
		case "error_without_code":
			bad.Outcome = "refresh_returned_error"
			bad.FinalizedAt = &final
		case "error_wrong_code":
			bad.Outcome = "refresh_returned_error"
			bad.FinalizedAt = &final
			v := "RUNTIME_REFRESH_FAILED"
			bad.FailureCode = &v
		case "error_trailing_code":
			bad.Outcome = "refresh_returned_error"
			bad.FinalizedAt = &final
			v := "runtime_refresh_failed "
			bad.FailureCode = &v
		case "final_before_claim":
			bad.Outcome = "refresh_returned_nil"
			v := birth.Add(-time.Microsecond)
			bad.FinalizedAt = &v
		}
		if db.Create(&bad).Error == nil {
			t.Fatal("invalid publication receipt admitted", which)
		}
	}
	if db.Migrator().DropConstraint(model, "ck_runtime_publication_outcome") != nil {
		t.Fatal("remove owned CHECK")
	}
	if db.Model(model).Where("request_id = ?", row.RequestID).Update("outcome", "CLAIMED").Error != nil {
		t.Fatal("construct invalid retained outcome")
	}
	dropReceipt()
	if database.MigrateThrough(ctx, db, 105) == nil {
		t.Fatal("invalid retained row admitted")
	}
	var receipts int64
	if db.Table("schema_migrations").Where("version = ?", 105).Count(&receipts).Error != nil || receipts != 0 {
		t.Fatal("invalid retained row recorded migration success")
	}
	if db.Model(model).Where("request_id = ?", row.RequestID).Update("outcome", "claimed").Error != nil {
		t.Fatal("restore exact owned receipt")
	}
	migrate()
	if !runtimePublicationRowsEqualV105(saved, runtimePublicationStoredV105(t, db, row.RequestID)) {
		t.Fatal("migration rewrote retained receipt")
	}
	if database.DropIndex(db, model, "idx_runtime_publication_actor") != nil {
		t.Fatal("remove owned index")
	}
	dropReceipt()
	migrate()
	if !runtimePublicationRowsEqualV105(saved, runtimePublicationStoredV105(t, db, row.RequestID)) {
		t.Fatal("index resume rewrote retained receipt")
	}
	// Missing fields may resume only in an empty table, never by invented backfill.
	if db.Migrator().DropColumn(model, "Reason") != nil {
		t.Fatal("construct retained partial receipt")
	}
	dropReceipt()
	if database.MigrateThrough(ctx, db, 105) == nil || db.Migrator().HasColumn(model, "Reason") {
		t.Fatal("nonempty partial receipt silently backfilled")
	}
	if db.Migrator().DropTable(model) != nil {
		t.Fatal("remove owned partial table")
	}
	migrate()
	if db.Migrator().DropColumn(model, "Reason") != nil {
		t.Fatal("construct empty partial receipt")
	}
	dropReceipt()
	migrate()
	if db.Model(model).Count(&count).Error != nil || count != 0 {
		t.Fatal("partial resume fabricated commands")
	}
	// Frozen GORM fixture mutations prove physical shape rejection on both drivers.
	typ := reflect.TypeFor[entity.RuntimePublicationCommand]()
	for _, which := range []string{"width", "precision", "nullable", "default", "extra", "index_order", "index_unique"} {
		if db.Migrator().DropTable(model) != nil {
			t.Fatal("remove owned empty receipt table")
		}
		dropReceipt()
		fields := make([]reflect.StructField, typ.NumField())
		for i := range fields {
			fields[i] = typ.Field(i)
		}
		switch which {
		case "width":
			fields[1].Tag = `gorm:"size:31;not null;index:idx_runtime_publication_actor,priority:1"`
		case "precision":
			fields[2].Tag = `gorm:"precision:3;not null;index:idx_runtime_publication_actor,priority:2"`
		case "nullable":
			fields[1].Tag = `gorm:"size:30;index:idx_runtime_publication_actor,priority:1"`
		case "default":
			fields[1].Tag = `gorm:"size:30;not null;default:invented;index:idx_runtime_publication_actor,priority:1"`
		case "extra":
			fields = append(fields, reflect.StructField{Name: "Unexpected", Type: reflect.TypeFor[string](), Tag: `gorm:"size:30"`})
		case "index_order":
			fields[1].Tag = `gorm:"size:30;not null;index:idx_runtime_publication_actor,priority:2"`
			fields[2].Tag = `gorm:"precision:6;not null;index:idx_runtime_publication_actor,priority:1"`
		case "index_unique":
			for _, i := range []int{1, 2, 6} {
				fields[i].Tag = reflect.StructTag(strings.ReplaceAll(string(fields[i].Tag), "index:idx_runtime_publication_actor", "uniqueIndex:idx_runtime_publication_actor"))
			}
		}
		altered := reflect.New(reflect.StructOf(fields)).Interface()
		if db.Table(model.TableName()).Migrator().CreateTable(altered) != nil {
			t.Fatal("construct incompatible physical receipt fixture", which)
		}
		if database.MigrateThrough(ctx, db, 105) == nil {
			t.Fatal("incompatible physical receipt admitted", which)
		}
		if db.Table("schema_migrations").Where("version = ?", 105).Count(&receipts).Error != nil || receipts != 0 {
			t.Fatal("failed physical migration recorded success")
		}
		if db.Migrator().DropTable(model) != nil {
			t.Fatal("remove incompatible fixture")
		}
		migrate()
	}
	checkLedger()
	if database.MigrateThrough(ctx, db, 104) == nil || database.MigrateThrough(ctx, db, 107) == nil {
		t.Fatal("historical/newer migration bound admitted")
	}
	if db.Model(model).Count(&count).Error != nil || count != 0 {
		t.Fatal("migration left fabricated command history")
	}
}

func runtimePublicationStoredV105(t *testing.T, db *gorm.DB, requestID string) entity.RuntimePublicationCommand {
	t.Helper()
	var row entity.RuntimePublicationCommand
	if db.Session(&gorm.Session{QueryFields: true}).Take(&row, "request_id = ?", requestID).Error != nil {
		t.Fatal("read exact retained command")
	}
	return row
}
func runtimePublicationRowsEqualV105(a, b entity.RuntimePublicationCommand) bool {
	if a.RequestID != b.RequestID || a.ActorID != b.ActorID || a.Reason != b.Reason || a.ClaimInstanceID != b.ClaimInstanceID || a.Outcome != b.Outcome || !a.ActorCreatedAt.Equal(b.ActorCreatedAt) || !a.ClaimInstanceStartedAt.Equal(b.ClaimInstanceStartedAt) || !a.ClaimedAt.Equal(b.ClaimedAt) || (a.FinalizedAt == nil) != (b.FinalizedAt == nil) || (a.FailureCode == nil) != (b.FailureCode == nil) {
		return false
	}
	return (a.FinalizedAt == nil || a.FinalizedAt.Equal(*b.FinalizedAt)) && (a.FailureCode == nil || *a.FailureCode == *b.FailureCode)
}

// This wrapper forwards each real transaction exactly once. A selected committed
// acknowledgement is lost only after the real driver reports Commit success.
// It never fabricates rows, a transaction result, or RefreshRuntime success.
type runtimePublicationAckPoolV105 struct{ gorm.ConnPool }

func (p *runtimePublicationAckPoolV105) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	var tx gorm.ConnPool
	var err error
	switch pool := p.ConnPool.(type) {
	case gorm.TxBeginner:
		tx, err = pool.BeginTx(ctx, options)
	case gorm.ConnPoolBeginner:
		tx, err = pool.BeginTx(ctx, options)
	default:
		return nil, gorm.ErrInvalidTransaction
	}
	if err != nil {
		return nil, err
	}
	committer, ok := tx.(gorm.TxCommitter)
	if !ok {
		return nil, gorm.ErrInvalidTransaction
	}
	return &runtimePublicationAckTxV105{ConnPool: tx, TxCommitter: committer}, nil
}

type runtimePublicationAckTxV105 struct {
	gorm.ConnPool
	gorm.TxCommitter
	loseAck bool
	lost    *atomic.Bool
}

func (tx *runtimePublicationAckTxV105) Commit() error {
	if err := tx.TxCommitter.Commit(); err != nil {
		return err
	}
	if tx.loseAck {
		tx.lost.Store(true)
		return errors.New("controlled committed acknowledgement loss")
	}
	return nil
}

func testRuntimePublicationCommandLifecycle(t *testing.T, db *gorm.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	isolated := db.Session(&gorm.Session{NewDB: true, Context: ctx})
	config := *isolated.Config
	isolated.Config = &config
	pool := &runtimePublicationAckPoolV105{ConnPool: isolated.Statement.ConnPool}
	isolated.ConnPool, isolated.Statement.ConnPool = pool, pool
	db = isolated
	// GORM callback registries are immutable while runtime/heartbeat workers live.
	// Each scenario publishes its exact fault closure through synchronized controls.
	var faultMu sync.RWMutex
	faultControls := map[string]func(*gorm.DB){
		"test:manual-publication-source-count-v105":       nil,
		"test:manual-publication-held-source-v105":        nil,
		"test:manual-publication-claim-ack-v105":          nil,
		"test:manual-publication-final-ack-v105":          nil,
		"test:manual-publication-final-write-v105":        nil,
		"test:manual-publication-revoke-after-claim-v105": nil,
	}
	setFault := func(name string, callback func(*gorm.DB)) error {
		faultMu.Lock()
		defer faultMu.Unlock()
		if _, ok := faultControls[name]; !ok {
			return errors.New("unknown publication fixture control")
		}
		faultControls[name] = callback
		return nil
	}
	for _, hook := range []struct {
		name     string
		register func(string, func(*gorm.DB)) error
	}{
		{"test:manual-publication-source-count-v105", db.Callback().Query().Before("gorm:query").Register},
		{"test:manual-publication-held-source-v105", db.Callback().Query().Before("gorm:query").Register},
		{"test:manual-publication-claim-ack-v105", db.Callback().Create().After("gorm:create").Register},
		{"test:manual-publication-final-ack-v105", db.Callback().Update().After("gorm:update").Register},
		{"test:manual-publication-final-write-v105", db.Callback().Update().Before("gorm:update").Register},
		{"test:manual-publication-revoke-after-claim-v105", db.Callback().Create().After("gorm:create").Register},
	} {
		if err := hook.register(hook.name, func(tx *gorm.DB) {
			faultMu.RLock()
			callback := faultControls[hook.name]
			faultMu.RUnlock()
			if callback != nil {
				callback(tx)
			}
		}); err != nil {
			t.Fatal("install publication fixture control", err)
		}
	}
	defer func() {
		faultMu.Lock()
		defer faultMu.Unlock()
		for name := range faultControls {
			faultControls[name] = nil
		}
	}()
	var svc *service.Service
	start := func() {
		t.Helper()
		var err error
		svc, err = service.New(ctx, db, service.WithRuntimeRefreshInterval(time.Hour))
		if err != nil {
			t.Fatal("construct original runtime service")
		}
		if svc.StartSystemInstance(ctx, service.SystemInstanceMetadata{Name: "Manual publication", Hostname: "manual-publication.invalid", Version: "test", GoVersion: "test", OS: "test", Arch: "test", StoragePath: t.TempDir()}) != nil {
			t.Fatal("register actual publication process")
		}
		if svc.StartRuntime(ctx) != nil {
			t.Fatal("start genuine runtime")
		}
	}
	stop := func() {
		t.Helper()
		svc.StopRuntime()
		if svc.StopSystemInstance(ctx) != nil {
			t.Fatal("join actual publication process")
		}
	}
	defer func() {
		if svc == nil {
			return
		}
		svc.StopRuntime()
		if err := svc.StopSystemInstance(ctx); err != nil {
			t.Error("close owned publication process", err)
		}
	}()
	start()
	admin, err := svc.Initialize(ctx, "manual-publication@example.invalid", "manual-publication-test-password", "Publication administrator")
	if err != nil {
		t.Fatal("initialize actual administrator")
	}
	// Use the original bearer to admit the persisted Session birth at database precision.
	admin, err = svc.Authenticate(ctx, admin.Token)
	if err != nil {
		t.Fatal("authenticate persisted original administrator Session")
	}
	if _, err := svc.CreateMember(ctx, admin.User.ID, "manual-publication-other@example.invalid", "manual-publication-other-password", "Other administrator", entity.RoleAdmin); err != nil {
		t.Fatal("create independent existing administrator")
	}
	other, err := svc.Login(ctx, "manual-publication-other@example.invalid", "manual-publication-other-password")
	if err != nil {
		t.Fatal("normal independent login")
	}
	other, err = svc.Authenticate(ctx, other.Token)
	if err != nil {
		t.Fatal("authenticate persisted independent administrator Session")
	}
	var loads atomic.Int64
	sourceHook := "test:manual-publication-source-count-v105"
	if setFault(sourceHook, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "providers" {
			loads.Add(1)
		}
	}) != nil {
		t.Fatal("register scoped source counter")
	}
	defer func() {
		if setFault(sourceHook, nil) != nil {
			t.Error("remove source counter")
		}
	}()
	var sequence atomic.Uint64
	next := func(reason string) service.RuntimePublicationCommandInput {
		t.Helper()
		return service.RuntimePublicationCommandInput{RequestID: fmt.Sprintf("6f739b12-12ce-4a20-8da7-%012x", sequence.Add(1)), Reason: reason}
	}
	status := func(err error, want int) {
		t.Helper()
		var app *apperrors.Error
		if !errors.As(err, &app) || app.Code != want {
			t.Fatalf("unexpected publication error status: want %d", want)
		}
	}
	unchanged := func(before entity.RuntimePublicationCommand) {
		t.Helper()
		if !runtimePublicationRowsEqualV105(before, runtimePublicationStoredV105(t, db, before.RequestID)) {
			t.Fatal("original immutable command changed")
		}
	}
	t.Run("exact_replay_changed_and_foreign_intent", func(t *testing.T) {
		in := next("Publish current authority once")
		before := loads.Load()
		receipt, err := svc.PublishRuntimeCommand(ctx, admin, in)
		if err != nil || receipt == nil || receipt.Status != "refresh_returned_nil" || receipt.FinalizedAt == nil || receipt.FailureCode != nil || loads.Load() != before+1 {
			t.Fatal("original successful refresh not recorded exactly once")
		}
		stored := runtimePublicationStoredV105(t, db, in.RequestID)
		runtimePublicationAuditV105(t, db, stored, 1)
		replay, err := svc.PublishRuntimeCommand(ctx, admin, in)
		if err != nil || !reflect.DeepEqual(receipt, replay) || loads.Load() != before+1 {
			t.Fatal("identical UUID replay redispatched or changed original receipt")
		}
		changed := in
		changed.Reason = "Changed original reason"
		_, err = svc.PublishRuntimeCommand(ctx, admin, changed)
		status(err, 409)
		_, err = svc.PublishRuntimeCommand(ctx, other, in)
		status(err, 409)
		_, err = svc.GetRuntimePublicationCommand(ctx, other, in.RequestID)
		status(err, 409)
		if loads.Load() != before+1 {
			t.Fatal("conflicting intent refreshed runtime")
		}
		unchanged(stored)
		if svc.RefreshRuntime(ctx) != nil {
			t.Fatal("independent later publication failed")
		}
		unchanged(stored)
	})
	t.Run("owned_pending_has_one_dispatch_and_joins", func(t *testing.T) {
		in := next("Review exact owned in-flight work")
		entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		var held atomic.Bool
		unblock := func() { once.Do(func() { close(release) }) }
		name := "test:manual-publication-held-source-v105"
		if setFault(name, func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "providers" && held.CompareAndSwap(false, true) {
				close(entered)
				select {
				case <-release:
				case <-tx.Statement.Context.Done():
					_ = tx.AddError(tx.Statement.Context.Err())
				}
			}
		}) != nil {
			t.Fatal("register bounded source barrier")
		}
		defer func() {
			unblock()
			select {
			case <-done:
			case <-ctx.Done():
				t.Error("owned publication operation failed to join")
			}
			if setFault(name, nil) != nil {
				t.Error("remove source barrier")
			}
		}()
		before := loads.Load()
		var result *service.RuntimePublicationCommandReceipt
		var operationErr error
		go func() { defer close(done); result, operationErr = svc.PublishRuntimeCommand(ctx, admin, in) }()
		select {
		case <-entered:
		case <-done:
			t.Fatal("owned source barrier not reached")
		case <-ctx.Done():
			t.Fatal("original fixture deadline exhausted")
		}
		pending, err := svc.GetRuntimePublicationCommand(ctx, admin, in.RequestID)
		if err != nil || pending.Status != "pending" || pending.FinalizedAt != nil {
			t.Fatal("claimed work lacks exact local pending ownership")
		}
		replay, err := svc.PublishRuntimeCommand(ctx, admin, in)
		if err != nil || replay.Status != "pending" {
			t.Fatal("identical in-flight replay did not preserve local ownership")
		}
		unblock()
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal("original owned operation did not join")
		}
		if operationErr != nil || result == nil || result.Status != "refresh_returned_nil" || loads.Load() != before+1 {
			t.Fatal("owned in-flight replay dispatched additional refresh")
		}
	})
	t.Run("claim_acknowledgement_loss_never_dispatches", func(t *testing.T) {
		in := next("Retain uncertain claim without dispatch")
		var lost atomic.Bool
		name := "test:manual-publication-claim-ack-v105"
		if setFault(name, func(tx *gorm.DB) {
			row, ok := tx.Statement.Dest.(*entity.RuntimePublicationCommand)
			if ok && row.RequestID == in.RequestID {
				if real, ok := tx.Statement.ConnPool.(*runtimePublicationAckTxV105); ok {
					real.loseAck = true
					real.lost = &lost
				} else {
					_ = tx.AddError(errors.New("missing real transaction wrapper"))
				}
			}
		}) != nil {
			t.Fatal("register claim acknowledgement control")
		}
		t.Cleanup(func() {
			if err := setFault(name, nil); err != nil {
				t.Error("release owned callback", err)
			}
		})
		before := loads.Load()
		_, err := svc.PublishRuntimeCommand(ctx, admin, in)
		status(err, 503)
		if setFault(name, nil) != nil {
			t.Fatal("remove claim acknowledgement control")
		}
		if !lost.Load() || loads.Load() != before {
			t.Fatal("uncertain committed claim dispatched work")
		}
		stored := runtimePublicationStoredV105(t, db, in.RequestID)
		receipt, err := svc.GetRuntimePublicationCommand(ctx, admin, in.RequestID)
		if err != nil || receipt.Status != "unknown" || stored.Outcome != "claimed" || stored.FinalizedAt != nil {
			t.Fatal("uncertain claim fabricated original outcome")
		}
		runtimePublicationAuditV105(t, db, stored, 0)
		receipt, err = svc.PublishRuntimeCommand(ctx, admin, in)
		if err != nil || receipt.Status != "unknown" || loads.Load() != before {
			t.Fatal("uncertain claim replay dispatched")
		}
		unchanged(stored)
	})
	t.Run("final_acknowledgement_loss_discovers_only_committed_receipt", func(t *testing.T) {
		in := next("Read back original committed outcome")
		var lost atomic.Bool
		name := "test:manual-publication-final-ack-v105"
		if setFault(name, func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "runtime_publication_commands" {
				if values, ok := tx.Statement.Dest.(map[string]any); ok && values["outcome"] == "refresh_returned_nil" {
					if real, ok := tx.Statement.ConnPool.(*runtimePublicationAckTxV105); ok {
						real.loseAck = true
						real.lost = &lost
					} else {
						_ = tx.AddError(errors.New("missing real transaction wrapper"))
					}
				}
			}
		}) != nil {
			t.Fatal("register final acknowledgement control")
		}
		t.Cleanup(func() {
			if err := setFault(name, nil); err != nil {
				t.Error("release owned callback", err)
			}
		})
		before := loads.Load()
		_, err := svc.PublishRuntimeCommand(ctx, admin, in)
		status(err, 503)
		if setFault(name, nil) != nil {
			t.Fatal("remove final acknowledgement control")
		}
		stored := runtimePublicationStoredV105(t, db, in.RequestID)
		receipt, err := svc.GetRuntimePublicationCommand(ctx, admin, in.RequestID)
		if !lost.Load() || err != nil || receipt.Status != "refresh_returned_nil" || stored.FinalizedAt == nil || loads.Load() != before+1 {
			t.Fatal("lost final acknowledgement did not retain genuine committed outcome")
		}
		runtimePublicationAuditV105(t, db, stored, 1)
		if _, err := svc.PublishRuntimeCommand(ctx, admin, in); err != nil || loads.Load() != before+1 {
			t.Fatal("committed receipt replay refreshed")
		}
		unchanged(stored)
	})
	t.Run("final_commit_failure_and_reconstruction_never_resume", func(t *testing.T) {
		in := next("Do not resume a refresh after uncertain final persistence")
		var failed atomic.Bool
		name := "test:manual-publication-final-write-v105"
		if setFault(name, func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "runtime_publication_commands" && failed.CompareAndSwap(false, true) {
				_ = tx.AddError(errors.New("controlled final receipt persistence failure"))
			}
		}) != nil {
			t.Fatal("register final persistence fault")
		}
		t.Cleanup(func() {
			if err := setFault(name, nil); err != nil {
				t.Error("release owned callback", err)
			}
		})
		before := loads.Load()
		_, err := svc.PublishRuntimeCommand(ctx, admin, in)
		status(err, 503)
		if setFault(name, nil) != nil {
			t.Fatal("remove final persistence fault")
		}
		stored := runtimePublicationStoredV105(t, db, in.RequestID)
		if !failed.Load() || loads.Load() != before+1 || stored.Outcome != "claimed" || stored.FinalizedAt != nil {
			t.Fatal("uncommitted final outcome fabricated")
		}
		runtimePublicationAuditV105(t, db, stored, 0)
		first := svc.CurrentSystemInstanceID()
		stop()
		start()
		if svc.CurrentSystemInstanceID() == first {
			t.Fatal("reconstruction reused registered process generation")
		}
		// StartRuntime has its own normal publication. Capture after that operation;
		// neither receipt GET nor same UUID replay may resume the claimed command.
		before = loads.Load()
		receipt, err := svc.GetRuntimePublicationCommand(ctx, admin, in.RequestID)
		if err != nil || receipt.Status != "unknown" {
			t.Fatal("reconstruction restored unsupported pending ownership")
		}
		receipt, err = svc.PublishRuntimeCommand(ctx, admin, in)
		if err != nil || receipt.Status != "unknown" || loads.Load() != before {
			t.Fatal("reconstruction replay resumed original command")
		}
		unchanged(stored)
	})
	t.Run("post_claim_actor_revocation_prevents_dispatch", func(t *testing.T) {
		in := next("Recheck current authority after durable claim")
		var revoked atomic.Bool
		name := "test:manual-publication-revoke-after-claim-v105"
		if setFault(name, func(tx *gorm.DB) {
			row, ok := tx.Statement.Dest.(*entity.RuntimePublicationCommand)
			if ok && row.RequestID == in.RequestID {
				r := tx.Session(&gorm.Session{NewDB: true}).Model(&entity.User{}).Where("id = ?", admin.User.ID).Update("disabled", true)
				if r.Error != nil || r.RowsAffected != 1 {
					_ = tx.AddError(errors.New("controlled actor revocation failed"))
				} else {
					revoked.Store(true)
				}
			}
		}) != nil {
			t.Fatal("register post-claim revocation")
		}
		t.Cleanup(func() {
			if err := setFault(name, nil); err != nil {
				t.Error("release owned callback", err)
			}
		})
		before := loads.Load()
		_, err := svc.PublishRuntimeCommand(ctx, admin, in)
		status(err, 401)
		if setFault(name, nil) != nil {
			t.Fatal("remove post-claim revocation")
		}
		stored := runtimePublicationStoredV105(t, db, in.RequestID)
		if !revoked.Load() || loads.Load() != before || stored.Outcome != "claimed" {
			t.Fatal("revoked actor dispatched claimed work")
		}
		_, err = svc.GetRuntimePublicationCommand(ctx, admin, in.RequestID)
		status(err, 401)
		if db.Model(&entity.User{}).Where("id = ?", admin.User.ID).Update("disabled", false).Error != nil {
			t.Fatal("restore exact fixture actor")
		}
		receipt, err := svc.PublishRuntimeCommand(ctx, admin, in)
		if err != nil || receipt.Status != "unknown" || loads.Load() != before {
			t.Fatal("restored authority resumed old intent")
		}
		unchanged(stored)
	})
	t.Run("failed_routes_still_publish_fresh_revocation", func(t *testing.T) {
		fresh := func(prefix string) string {
			t.Helper()
			v, e := id.NewPrefixed(prefix)
			if e != nil {
				t.Fatal(e)
			}
			return v
		}
		keyID := fresh("key")
		bearer := "rx_" + strings.Repeat("p", 43)
		birth := time.Now().UTC().Truncate(time.Microsecond)
		if db.Create(&entity.APIKey{ID: keyID, UserID: other.User.ID, Name: "Revocation proof", Prefix: "rx_fixture", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive, CreatedAt: birth}).Error != nil {
			t.Fatal("create actual native key fixture")
		}
		if svc.RefreshRuntime(ctx) != nil {
			t.Fatal("publish active native key")
		}
		router := fox.New()
		New(svc).RegisterRoutes(router)
		native := func() int {
			r := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			r.Header.Set("Authorization", "Bearer "+bearer)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			return w.Code
		}
		if native() != 200 {
			t.Fatal("native Key was not admitted before revocation")
		}
		provider, connection, pm, model, binding := fresh("prv"), fresh("con"), fresh("pmd"), fresh("mdl"), fresh("bnd")
		for _, row := range []any{&entity.Provider{ID: provider, Name: "Invalid manual route", Enabled: true, ETag: "0", CreatedAt: birth}, &entity.ProviderConnection{ID: connection, ProviderID: provider, Name: "No dispatch", BaseURL: "https://manual-publication.invalid/v1", Protocol: entity.ProtocolOpenAIChat, Adapter: "native", Enabled: true, TransportGeneration: "0", CreatedAt: birth}, &entity.ProviderModel{ID: pm, ConnectionID: connection, UpstreamName: "invalid-total", CapabilityTransportGeneration: "0", CreatedAt: birth}, &entity.Model{ID: model, Status: entity.ResourceActive, CreatedAt: birth}, &entity.ModelProviderBinding{ID: binding, ModelID: model, ProviderModelID: pm, Weight: 99, CreatedAt: birth}} {
			if db.Create(row).Error != nil {
				t.Fatal("construct invalid route total fixture")
			}
		}
		if db.Model(&entity.APIKey{}).Where("id = ?", keyID).Update("status", entity.KeyRevoked).Error != nil {
			t.Fatal("persist native key revocation")
		}
		in := next("Publish fresh denial despite invalid routes")
		before := loads.Load()
		receipt, err := svc.PublishRuntimeCommand(ctx, admin, in)
		if err != nil || receipt.Status != "refresh_returned_error" || receipt.FinalizedAt == nil || receipt.FailureCode == nil || *receipt.FailureCode != "runtime_refresh_failed" || loads.Load() != before+1 {
			t.Fatal("failed refresh outcome not retained")
		}
		if native() != 401 {
			t.Fatal("failed route refresh retained revoked native authority")
		}
		stored := runtimePublicationStoredV105(t, db, in.RequestID)
		if db.Where("id = ?", binding).Delete(&entity.ModelProviderBinding{}).Error != nil {
			t.Fatal("remove exact invalid route binding")
		}
		if svc.RefreshRuntime(ctx) != nil {
			t.Fatal("publish later valid route configuration")
		}
		unchanged(stored)
		before = loads.Load()
		replay, err := svc.PublishRuntimeCommand(ctx, admin, in)
		if err != nil || !reflect.DeepEqual(receipt, replay) || loads.Load() != before {
			t.Fatal("later publication rewrote or replayed original failure")
		}
	})
}

func runtimePublicationAuditV105(t *testing.T, db *gorm.DB, row entity.RuntimePublicationCommand, wantFinal int) {
	t.Helper()
	var events []entity.AuditEvent
	if db.Where("action IN ?", []string{"runtime.publication.claim", "runtime.publication.finalize"}).Find(&events).Error != nil {
		t.Fatal("read original publication audit")
	}
	claims, finals := 0, 0
	for _, event := range events {
		if event.DetailsJSON == nil {
			t.Fatal("publication audit missing typed details")
		}
		var detail struct {
			RequestID string `json:"request_id"`
			Reason    string `json:"reason"`
			Outcome   string `json:"outcome"`
		}
		if json.Unmarshal([]byte(*event.DetailsJSON), &detail) != nil {
			t.Fatal("publication audit malformed")
		}
		if detail.RequestID != row.RequestID {
			continue
		}
		if detail.Reason != row.Reason || event.ActorID != row.ActorID || event.ResourceType != "system_instance" || event.ResourceID != row.ClaimInstanceID {
			t.Fatal("publication audit detached from original tuple")
		}
		switch event.Action {
		case "runtime.publication.claim":
			claims++
			if detail.Outcome != "claimed" {
				t.Fatal("claim audit fabricated terminal outcome")
			}
		case "runtime.publication.finalize":
			finals++
			if detail.Outcome != row.Outcome {
				t.Fatal("terminal audit differs from retained outcome")
			}
		}
	}
	if claims != 1 || finals != wantFinal {
		t.Fatal("publication receipt and terminal audit did not commit together")
	}
}
