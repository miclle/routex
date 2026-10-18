//go:build integration && routex_cleanup_evidence

package cleanupevidence

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

// Run one driver at a time in a freshly observed, owned Compose namespace.
// ROUTEX_TEST_CLEANUP_EVIDENCE_OWNED=1 is caller attestation, not Docker proof.
// Select postgres/mysql with ROUTEX_TEST_CLEANUP_EVIDENCE_DRIVER and provide
// only its existing ROUTEX_TEST_POSTGRES_DSN/ROUTEX_TEST_MYSQL_DSN input. The
// selected DSN must name loopback, a nondefault mapped port and routex_test,
// with finite <=5s connection/read/write timeouts and UTC timestamp decoding.
// This seeds SQL fixtures only; it proves no API claim, remote204 or adoption.
func TestCleanupEvidenceProjectionSQLIntegration(t *testing.T) {
	db, driver := cleanupProjectionSQLDB(t)
	e, seeded := fixture("pending")
	e.AsOf = e.RunStarted.Add(20 * time.Second)
	seedCleanupProjectionSQL(t, db, e, seeded)

	t.Run("pending_exact_SQL_mapping_and_borrowed_pool", func(t *testing.T) {
		before := cleanupSQLSnapshot(t, db, e)
		value := projectCleanupSQL(t, db, e, false)
		if Validate(e, value) != nil || value.PresentedState != "pending" || value.SourceProcesses != 1 || value.SourceUses != 1 || before.published[0].Command.ReviewedETag != e.ReviewedETag {
			t.Fatal("exact pending SQL projection rejected")
		}
		claim, err := PendingClaim(e, value)
		if err != nil || claim.State != "pending" || claim.RequestID != e.RequestID || claim.CreationRequestID != e.CreationRequestID || claim.RemoteRequestID != e.RequestID || claim.FinishedAt != nil || claim.ClaimAuditCount != 1 || claim.ObserveAuditCount != 0 {
			t.Fatal("pending consumer tuple changed")
		}
		if !reflect.DeepEqual(before, cleanupSQLSnapshot(t, db, e)) {
			t.Fatal("read-only projection changed durable rows")
		}
		// A returned read transaction must neither close the caller's pool nor
		// leave the borrowed connection in a read-only transaction/session.
		cleanupSQLUpdate(t, db, &entity.CredentialPublishedCleanup{}, "request_id", e.RequestID, map[string]any{"root_epoch": uint64(2)})
		cleanupSQLUpdate(t, db, &entity.CredentialPublishedCleanup{}, "request_id", e.RequestID, map[string]any{"root_epoch": e.RootEpoch})
		projectCleanupSQL(t, db, e, false)
	})

	t.Run("expired_pending_preserves_original_claim", func(t *testing.T) {
		expired := e
		expired.AsOf = e.RunStarted.Add(50 * time.Second)
		before := cleanupSQLSnapshot(t, db, expired)
		value := projectCleanupSQL(t, db, expired, false)
		claim, err := PendingClaim(expired, value)
		if err != nil || value.PresentedState != "unknown" || claim.State != "pending" || claim.FinishedAt != nil || !claim.StartedAt.Equal(seeded.published[0].Command.StartedAt) || !claim.Deadline.Equal(seeded.published[0].Command.Deadline) {
			t.Fatal("expiry rewrote pending SQL evidence")
		}
		if !reflect.DeepEqual(before, cleanupSQLSnapshot(t, db, expired)) {
			t.Fatal("expiry persisted a fabricated terminal state")
		}
	})

	t.Run("acknowledged_SQL_mapping_is_not_pending_or_HTTP_proof", func(t *testing.T) {
		acknowledged, ack := fixture("acknowledged")
		command := ack.published[0].Command
		t.Cleanup(func() {
			cleanupSQLRetained(t, db, func(conn *gorm.DB) *gorm.DB {
				return conn.Model(&entity.CredentialPublishedCleanup{}).Where("request_id = ?", e.RequestID).Updates(cleanupSQLCommandState(seeded.published[0].Command))
			})
			cleanupSQLRetained(t, db, func(conn *gorm.DB) *gorm.DB {
				return conn.Where("id = ?", ack.audits[1].ID).Delete(&entity.AuditEvent{})
			})
		})
		cleanupSQLUpdate(t, db, &entity.CredentialPublishedCleanup{}, "request_id", e.RequestID, cleanupSQLCommandState(command))
		cleanupSQLCreate(t, db, cleanupSQLAudit(ack.audits[1]))
		before := cleanupSQLSnapshot(t, db, acknowledged)
		value := projectCleanupSQL(t, db, acknowledged, false)
		if Validate(acknowledged, value) != nil || value.PresentedState != "acknowledged" || value.Claim.ObserveAuditCount != 1 || value.Claim.FinishedAt == nil || !value.Claim.FinishedAt.Equal(*command.FinishedAt) {
			t.Fatal("acknowledged SQL projection rejected")
		}
		if _, err := PendingClaim(acknowledged, value); !errors.Is(err, ErrRejected) {
			t.Fatal("acknowledged ledger borrowed as pending claim")
		}
		if !reflect.DeepEqual(before, cleanupSQLSnapshot(t, db, acknowledged)) {
			t.Fatal("acknowledged projection changed durable rows")
		}
	})

	t.Run("exact_identity_and_microsecond_birth_mismatch", func(t *testing.T) {
		cases := []Expectation{e, e, e, e}
		cases[0].ActorID = testIdentity("usr", 2)
		cases[1].ActorBirth = e.ActorBirth.Add(time.Microsecond)
		cases[2].IntegrationBirth = e.IntegrationBirth.Add(time.Microsecond)
		cases[3].RevisionID = testIdentity("vlr", 2)
		for _, foreign := range cases {
			projectCleanupSQL(t, db, foreign, true)
		}
	})

	t.Run("missing_exact_claim_audit", func(t *testing.T) {
		t.Cleanup(func() {
			cleanupSQLRetained(t, db, func(conn *gorm.DB) *gorm.DB { return conn.Create(cleanupSQLAudit(seeded.audits[0])) })
		})
		cleanupSQLDelete(t, db, &entity.AuditEvent{}, "id", seeded.audits[0].ID)
		projectCleanupSQL(t, db, e, true)
	})

	t.Run("ambiguous_published_request_same_physical_object", func(t *testing.T) {
		other := cleanupSQLPublished(seeded.published[0])
		other.RequestID = "00000000-0000-4000-8000-000000000003"
		t.Cleanup(func() {
			cleanupSQLRetained(t, db, func(conn *gorm.DB) *gorm.DB {
				return conn.Where("request_id = ?", other.RequestID).Delete(&entity.CredentialPublishedCleanup{})
			})
		})
		cleanupSQLCreate(t, db, &other)
		projectCleanupSQL(t, db, e, true)
	})

	t.Run("orphan_namespace_cannot_replace_published_history", func(t *testing.T) {
		c := seeded.published[0].Command
		orphan := entity.ProviderCredentialCleanup{CreationRequestID: c.CreationRequestID, RequestID: c.RequestID, ActorID: c.ActorID, ActorBirth: c.ActorBirth, IntegrationID: c.IntegrationID, IntegrationBirth: c.IntegrationBirth, RevisionID: c.RevisionID, OperationProof: c.OperationProof, ReviewedETag: c.ReviewedETag, Reason: c.Reason, State: c.State, OwnershipJSON: c.OwnershipJSON, CleanupJSON: c.CleanupJSON, RootEpoch: c.RootEpoch, StartedAt: c.StartedAt, Deadline: c.Deadline, FinishedAt: c.FinishedAt}
		t.Cleanup(func() {
			cleanupSQLRetained(t, db, func(conn *gorm.DB) *gorm.DB {
				return conn.Where("creation_request_id = ?", e.CreationRequestID).Delete(&entity.ProviderCredentialCleanup{})
			})
		})
		cleanupSQLCreate(t, db, &orphan)
		projectCleanupSQL(t, db, e, true)
	})

	t.Run("real_readonly_refusal_and_checked_rollback", func(t *testing.T) {
		before := cleanupSQLSnapshot(t, db, e)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		tx := db.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
		if tx.Error != nil {
			t.Fatal("real read-only transaction unavailable")
		}
		// The known existing row and changed value avoid treating a zero-match
		// or no-op UPDATE as evidence of the driver's read-only enforcement.
		writeErr := tx.Model(&entity.CredentialPublishedCleanup{}).Where("request_id = ?", e.RequestID).Update("reason", "Synthetic forbidden replacement").Error
		rollbackErr := tx.Rollback().Error
		if !cleanupSQLReadOnlyRefusal(driver, writeErr) || rollbackErr != nil || ctx.Err() != nil {
			t.Fatal("typed read-only refusal or original rollback unconfirmed")
		}
		if !reflect.DeepEqual(before, cleanupSQLSnapshot(t, db, e)) {
			t.Fatal("rejected write changed exact durable rows")
		}
		projectCleanupSQL(t, db, e, false)
	})
}

func cleanupProjectionSQLDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	driver := os.Getenv("ROUTEX_TEST_CLEANUP_EVIDENCE_DRIVER")
	if os.Getenv("ROUTEX_TEST_CLEANUP_EVIDENCE_OWNED") != "1" || (driver != "postgres" && driver != "mysql") {
		t.Fatal("one explicitly owned cleanup evidence SQL worker is required")
	}
	dsn := os.Getenv("ROUTEX_TEST_" + strings.ToUpper(driver) + "_DSN")
	other := "MYSQL"
	if driver == "mysql" {
		other = "POSTGRES"
	}
	if dsn == "" || os.Getenv("ROUTEX_TEST_"+other+"_DSN") != "" || !cleanupSQLDSNSafe(driver, dsn) {
		t.Fatal("one bounded loopback routex_test connection is required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	db, err := database.Open(ctx, driver, dsn)
	cancel()
	if err != nil {
		t.Fatal("owned cleanup evidence database connection failed")
	}
	db = db.Session(&gorm.Session{Logger: logger.Discard})
	pool, err := db.DB()
	if err != nil {
		t.Fatal("owned cleanup evidence pool unavailable")
	}
	t.Cleanup(func() {
		if pool.Close() != nil {
			t.Error("caller-owned cleanup evidence pool closure failed")
		}
	})
	ctx, cancel = context.WithTimeout(t.Context(), 5*time.Second)
	query := "SELECT current_database()"
	if driver == "mysql" {
		query = "SELECT DATABASE()"
	}
	var name string
	err = db.WithContext(ctx).Raw(query).Scan(&name).Error
	tables, tableErr := db.WithContext(ctx).Migrator().GetTables()
	cancel()
	if err != nil || name != "routex_test" || tableErr != nil || len(tables) != 0 {
		t.Fatal("an initially empty owned routex_test database is required")
	}
	if database.Migrate(t.Context(), db) != nil {
		t.Fatal("released schema migration failed")
	}
	// Restrict the projection lane only after the startup migration has released
	// its dedicated advisory-lock connection.
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	return db, driver
}

func cleanupSQLDSNSafe(driver, dsn string) bool {
	if driver == "postgres" {
		c, err := pgconn.ParseConfig(dsn)
		return err == nil && c.Host == "127.0.0.1" && c.Database == "routex_test" && c.Port != 0 && c.Port != 5432 && c.ConnectTimeout > 0 && c.ConnectTimeout <= 5*time.Second && c.TLSConfig == nil && len(c.Fallbacks) == 0
	}
	c, err := mysqldriver.ParseDSN(dsn)
	if err != nil || c.Net != "tcp" || c.DBName != "routex_test" || !c.ParseTime || c.Loc != time.UTC || c.Timeout <= 0 || c.Timeout > 5*time.Second || c.ReadTimeout <= 0 || c.ReadTimeout > 5*time.Second || c.WriteTimeout <= 0 || c.WriteTimeout > 5*time.Second {
		return false
	}
	host, port, err := net.SplitHostPort(c.Addr)
	n, numberErr := strconv.ParseUint(port, 10, 16)
	return err == nil && numberErr == nil && host == "127.0.0.1" && n > 0 && n != 3306
}

func seedCleanupProjectionSQL(t *testing.T, db *gorm.DB, e Expectation, s snapshot) {
	t.Helper()
	op := s.operations[0]
	c := s.creationUses[0]
	p := s.processes[0]
	u := s.uses[0]
	d := s.denials[0]
	published := cleanupSQLPublished(s.published[0])
	models := []any{
		&entity.User{ID: e.ActorID, Email: "cleanup-evidence@example.invalid", Name: "Synthetic SQL actor", PasswordHash: strings.Repeat("x", 60), Role: entity.RoleMember, CreatedAt: e.ActorBirth, UpdatedAt: e.ActorBirth},
		&entity.VaultIntegration{ID: e.IntegrationID, Name: "Synthetic SQL integration", RevisionID: e.RevisionID, CreatedAt: e.IntegrationBirth, UpdatedAt: e.IntegrationBirth},
		&entity.VaultRevision{ID: e.RevisionID, IntegrationID: e.IntegrationID, IntegrationBirth: e.IntegrationBirth, Name: "Synthetic SQL revision", Endpoint: "https://vault.invalid", Mount: "kv", Prefix: "synthetic", DataField: "token", CreatedAt: e.IntegrationBirth},
		&entity.CredentialStorageOperation{StorageSource: op.StorageSource, RequestID: op.RequestID, ActorID: e.ActorID, ActorBirth: e.ActorBirth, Kind: "provider", TargetID: testIdentity("prv", 1), TargetBirth: e.IntegrationBirth, PolicyGeneration: "0", IntentJSON: "{}", CredentialID: op.CredentialID, CredentialBirth: op.CredentialBirth, ProviderID: testIdentity("prv", 1), ConnectionID: testIdentity("con", 1), ProviderBirth: e.IntegrationBirth, ConnectionBirth: e.IntegrationBirth, ReferenceID: strings.Repeat("1", 32), ExpectedMarkerSHA256: strings.Repeat("d", 64), DescriptorSHA256: strings.Repeat("e", 64), IntegrationID: op.IntegrationID, IntegrationBirth: op.IntegrationBirth, RevisionID: op.RevisionID, WriterGeneration: testIdentity("sec", 1), ReaderGeneration: testIdentity("sec", 2), RootEpoch: 1, State: op.State, Claim: testIdentity("ins", 1), ClaimedUntil: e.AsOf, WriteJSON: op.WriteJSON, ReadJSON: op.ReadJSON, CreatedAt: op.CreatedAt},
		&entity.CredentialSourceProcess{ProcessID: p.ProcessID, Generation: p.Generation, Birth: p.Birth, RegisteredAt: p.RegisteredAt, ClosedAt: p.ClosedAt},
		&entity.ProviderCredentialCreationUse{CreationRequestID: c.CreationRequestID, ProcessID: c.ProcessID, ProcessGeneration: c.ProcessGeneration, Exposed: c.Exposed},
		&entity.CredentialSourceUse{PhysicalObject: u.PhysicalObject, ProcessID: u.ProcessID, Generation: u.Generation, Birth: u.Birth, Exposed: u.Exposed, CreatedAt: u.CreatedAt, JoinedAt: u.JoinedAt},
		&entity.CredentialSourceDenial{PhysicalObject: d.PhysicalObject, CreationRequestID: d.CreationRequestID, RequestID: d.RequestID, RemoteRequestID: d.RemoteRequestID, CreatedAt: d.CreatedAt},
		&published,
		cleanupSQLAudit(s.audits[0]),
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, model := range models {
			if tx.Create(model).Error != nil {
				return ErrRejected
			}
		}
		return nil
	}) != nil {
		t.Fatal("finite released-schema SQL fixture refused")
	}
}

func cleanupSQLPublished(p publishedRow) entity.CredentialPublishedCleanup {
	c := p.Command
	return entity.CredentialPublishedCleanup{PhysicalObject: p.PhysicalObject, KnownNoEffect: p.KnownNoEffect, CreationRequestID: c.CreationRequestID, RequestID: c.RequestID, ActorID: c.ActorID, ActorBirth: c.ActorBirth, IntegrationID: c.IntegrationID, IntegrationBirth: c.IntegrationBirth, RevisionID: c.RevisionID, OperationProof: c.OperationProof, ReviewedETag: c.ReviewedETag, Reason: c.Reason, State: c.State, OwnershipJSON: c.OwnershipJSON, CleanupJSON: c.CleanupJSON, RootEpoch: c.RootEpoch, StartedAt: c.StartedAt, Deadline: c.Deadline, FinishedAt: c.FinishedAt}
}

func cleanupSQLAudit(a auditRow) *entity.AuditEvent {
	return &entity.AuditEvent{ID: a.ID, ActorID: a.ActorID, Action: a.Action, ResourceType: a.ResourceType, ResourceID: a.ResourceID, DetailsJSON: a.DetailsJSON, CreatedAt: a.CreatedAt}
}

func cleanupSQLCommandState(c commandRow) map[string]any {
	return map[string]any{"state": c.State, "ownership_json": c.OwnershipJSON, "cleanup_json": c.CleanupJSON, "finished_at": c.FinishedAt}
}

// Go cancels t.Context before Cleanup runs. Detach only these bounded exact-row
// restorations in the already attested owned fixture, never normal test work.
func cleanupSQLRetained(t *testing.T, db *gorm.DB, mutate func(*gorm.DB) *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
	defer cancel()
	if result := mutate(db.WithContext(ctx)); result.Error != nil || result.RowsAffected != 1 {
		t.Error("controlled exact SQL fixture cleanup failed")
	}
}

func cleanupSQLCreate(t *testing.T, db *gorm.DB, model any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if result := db.WithContext(ctx).Create(model); result.Error != nil || result.RowsAffected != 1 {
		t.Fatal("controlled SQL fixture insert failed")
	}
}

func cleanupSQLDelete(t *testing.T, db *gorm.DB, model any, column, value string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if result := db.WithContext(ctx).Where(column+" = ?", value).Delete(model); result.Error != nil || result.RowsAffected != 1 {
		t.Fatal("controlled exact SQL fixture removal failed")
	}
}

func cleanupSQLUpdate(t *testing.T, db *gorm.DB, model any, column, value string, changes map[string]any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if result := db.WithContext(ctx).Model(model).Where(column+" = ?", value).Updates(changes); result.Error != nil || result.RowsAffected != 1 {
		t.Fatal("controlled exact SQL fixture update failed")
	}
}

func cleanupSQLSnapshot(t *testing.T, db *gorm.DB, e Expectation) snapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	s, err := readSnapshot(db.WithContext(ctx), e)
	if err != nil {
		t.Fatal("independent durable SQL snapshot unavailable")
	}
	return s
}

// The wrapper observes real database/sql transactions without replacing the
// driver or any SQL result. Only the test owns and ultimately closes the pool.
type cleanupSQLObservedPool struct {
	*sql.DB
	begins, rollbacks, commits int
	options                    sql.TxOptions
	rollbackError              error
}

func (p *cleanupSQLObservedPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	p.begins++
	if options != nil {
		p.options = *options
	}
	tx, err := p.DB.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &cleanupSQLObservedTx{Tx: tx, owner: p}, nil
}

func (p *cleanupSQLObservedPool) GetDBConn() (*sql.DB, error) { return p.DB, nil }

type cleanupSQLObservedTx struct {
	*sql.Tx
	owner *cleanupSQLObservedPool
}

func (tx *cleanupSQLObservedTx) Rollback() error {
	tx.owner.rollbacks++
	err := tx.Tx.Rollback()
	tx.owner.rollbackError = err
	return err
}

func (tx *cleanupSQLObservedTx) Commit() error {
	tx.owner.commits++
	return tx.Tx.Commit()
}

func projectCleanupSQL(t *testing.T, db *gorm.DB, e Expectation, rejected bool) Evidence {
	t.Helper()
	pool, err := db.DB()
	if err != nil {
		t.Fatal("caller SQL pool unavailable")
	}
	observed := &cleanupSQLObservedPool{DB: pool}
	wrapped := db.Session(&gorm.Session{NewDB: true})
	wrapped.ConnPool = observed
	wrapped.Statement = &gorm.Statement{DB: wrapped, ConnPool: observed, Context: t.Context()}
	value, err := Project(t.Context(), wrapped, e)
	if rejected {
		if !errors.Is(err, ErrRejected) || !reflect.DeepEqual(value, Evidence{}) {
			t.Fatal("foreign or ambiguous SQL evidence accepted")
		}
	} else if err != nil {
		t.Fatal("exact real SQL projection failed")
	}
	if observed.begins != 1 || observed.rollbacks != 1 || observed.commits != 0 || observed.rollbackError != nil || !observed.options.ReadOnly || observed.options.Isolation != sql.LevelRepeatableRead || pool.Stats().InUse != 0 {
		t.Fatal("real read-only transaction or checked rollback changed")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if pool.PingContext(ctx) != nil {
		t.Fatal("projection closed its caller-owned pool")
	}
	return value
}

func cleanupSQLReadOnlyRefusal(driver string, err error) bool {
	if err == nil {
		return false
	}
	if driver == "mysql" {
		var refused *mysqldriver.MySQLError
		return errors.As(err, &refused) && refused.Number == 1792 && refused.SQLState == [5]byte{'2', '5', '0', '0', '6'}
	}
	var refused interface{ SQLState() string }
	return errors.As(err, &refused) && refused.SQLState() == "25006"
}
