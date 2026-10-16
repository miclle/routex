package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type publicationAcknowledgementSQLState struct {
	command               entity.RuntimePublicationCommand
	rows                  []entity.RuntimePublicationAcknowledgement
	failWrite, loseCommit bool
	afterInsert           func()
}
type publicationAcknowledgementConnector struct {
	base  runtimeInstallationConnector
	state *publicationAcknowledgementSQLState
}

func (c publicationAcknowledgementConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &publicationAcknowledgementConnection{runtimeInstallationConnection: conn.(*runtimeInstallationConnection), state: c.state}, nil
}
func (c publicationAcknowledgementConnector) Driver() driver.Driver {
	return publicationAcknowledgementDriver(c)
}

type publicationAcknowledgementDriver publicationAcknowledgementConnector

func (d publicationAcknowledgementDriver) Open(string) (driver.Conn, error) {
	return publicationAcknowledgementConnector(d).Connect(context.Background())
}

type publicationAcknowledgementConnection struct {
	*runtimeInstallationConnection
	state  *publicationAcknowledgementSQLState
	staged []entity.RuntimePublicationAcknowledgement
}

func (c *publicationAcknowledgementConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *publicationAcknowledgementConnection) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	tx, err := c.runtimeInstallationConnection.BeginTx(ctx, o)
	if err != nil {
		return nil, err
	}
	c.staged = append([]entity.RuntimePublicationAcknowledgement{}, c.state.rows...)
	return publicationAcknowledgementTransaction{Tx: tx, c: c}, nil
}

type publicationAcknowledgementTransaction struct {
	driver.Tx
	c *publicationAcknowledgementConnection
}

func (t publicationAcknowledgementTransaction) Commit() error {
	if err := t.Tx.Commit(); err != nil {
		return err
	}
	t.c.state.rows = t.c.staged
	t.c.staged = nil
	if t.c.state.loseCommit {
		return errors.New("controlled acknowledgement commit response loss")
	}
	return nil
}
func (t publicationAcknowledgementTransaction) Rollback() error {
	t.c.staged = nil
	return t.Tx.Rollback()
}
func (c *publicationAcknowledgementConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch {
	case strings.Contains(q, `FROM "runtime_publication_commands"`):
		return effectiveSQLRows([]entity.RuntimePublicationCommand{c.state.command})
	case strings.Contains(q, `FROM "runtime_publication_acknowledgements"`):
		return effectiveSQLRows(c.staged)
	case strings.Contains(q, `FROM "runtime_installation_observations"`):
		if len(args) > 0 {
			if id, ok := args[0].Value.(string); ok && strings.HasPrefix(id, "rin_") {
				rows := []entity.RuntimeInstallationObservation{}
				for _, row := range c.installationStaged {
					if row.ID == id {
						rows = append(rows, row)
					}
				}
				return effectiveSQLRows(rows)
			}
		}
	}
	return c.runtimeInstallationConnection.QueryContext(ctx, q, args)
}
func (c *publicationAcknowledgementConnection) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if !strings.Contains(q, `INSERT INTO "runtime_publication_acknowledgements"`) {
		return c.runtimeInstallationConnection.ExecContext(ctx, q, args)
	}
	if c.state.failWrite {
		return nil, errors.New("controlled acknowledgement write failure")
	}
	if len(args) != 3 {
		return nil, errors.New("unexpected acknowledgement fields")
	}
	c.staged = append(c.staged, entity.RuntimePublicationAcknowledgement{RequestID: args[0].Value.(string), InstallationID: args[1].Value.(string), AcknowledgedAt: args[2].Value.(time.Time)})
	if c.state.afterInsert != nil {
		c.state.afterInsert()
	}
	return driver.RowsAffected(1), nil
}
func publicationAcknowledgementFixture(t *testing.T) (*Service, *runtimeInstallationSQLState, *publicationAcknowledgementSQLState, *runtimePublicationTicket) {
	t.Helper()
	s, _, installation := runtimeInstallationSQLService(t)
	_, f, control := roleDefinitionSQLService(t)
	state := &publicationAcknowledgementSQLState{}
	pool := sql.OpenDB(publicationAcknowledgementConnector{base: runtimeInstallationConnector{base: runtimeApplicationConnector{f, control, installation.application}, state: installation}, state: state})
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	s.db = db
	if err := runtimeInstallationRecordForTest(s); err != nil {
		t.Fatal("initial actual installation", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	state.command = entity.RuntimePublicationCommand{RequestID: "1f739b12-12ce-4a20-8da7-04bc8885dfb8", ActorID: "usr_01m36yee4gkbns18pfcqqc75a3", ActorCreatedAt: now.Add(-time.Hour), Reason: "Correlate this local installation", ClaimInstanceID: s.instance.id, ClaimInstanceStartedAt: s.instance.startedAt, ClaimedAt: now, Outcome: "claimed"}
	ticket := &runtimePublicationTicket{row: state.command, lease: s.instance, ctx: t.Context()}
	s.runtimePublicationCommands.Store(state.command.RequestID, ticket)
	return s, installation, state, ticket
}
func recordPublicationAcknowledgementForTest(s *Service, ticket *runtimePublicationTicket) error {
	ctx, cancel := runtimeInstallationEvidenceContext(ticket.ctx, s.runtime.auth.Load())
	defer cancel()
	return s.recordRuntimePublicationAcknowledgement(ctx, ticket, s.runtime.routes.Load(), s.runtime.auth.Load(), s.runtime.epoch.Load())
}
func readPublicationAcknowledgementForTest(s *Service, command entity.RuntimePublicationCommand) (*RuntimePublicationCommandReceipt, error) {
	var receipt *RuntimePublicationCommandReceipt
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var err error
		receipt, err = s.runtimePublicationReceiptWithAcknowledgement(tx, command)
		return err
	})
	return receipt, err
}
func TestRuntimePublicationAcknowledgementExactInstallationAndHistoricalRead(t *testing.T) {
	s, installation, state, ticket := publicationAcknowledgementFixture(t)
	if err := recordPublicationAcknowledgementForTest(s, ticket); err != nil || len(state.rows) != 1 {
		t.Fatal("exact successful seam not retained", err)
	}
	receipt, err := readPublicationAcknowledgementForTest(s, state.command)
	if err != nil || receipt.Acknowledgement == nil || receipt.Status != "pending" || receipt.Acknowledgement.InstanceID != ticket.lease.id || receipt.Acknowledgement.InstallationID != installation.rows[0].ID || receipt.Acknowledgement.SnapshotID != s.runtime.routes.Load().ID {
		t.Fatal("original outcome and local installation were conflated", err)
	}
	original := *receipt.Acknowledgement
	// A stopped/new process and a later snapshot cannot rewrite or turn the
	// historical acknowledgement into current/fleet proof.
	close(s.runtime.done)
	s.instance = nil
	s.runtimePublicationCommands.Delete(state.command.RequestID)
	receipt, err = readPublicationAcknowledgementForTest(s, state.command)
	if err != nil || receipt.Status != "unknown" || receipt.Acknowledgement == nil || !reflect.DeepEqual(original, *receipt.Acknowledgement) {
		t.Fatal("historical local acknowledgement changed", err)
	}
	raw, err := json.Marshal(receipt)
	if err != nil || strings.Contains(string(raw), installation.rows[0].SourceDigest) || strings.Contains(string(raw), "private-installation-token") || strings.Contains(string(raw), "current_serving") {
		t.Fatal("receipt exposed private source or invented current serving proof")
	}
}
func TestRuntimePublicationAcknowledgementRejectsUnownedOrObsoleteSeams(t *testing.T) {
	for _, which := range []string{"no_ticket", "replaced_ticket", "wrong_process_birth", "expired_lease", "changed_claim", "missing_installation", "authorization_epoch", "late_cancel", "late_process_replacement", "write_failure"} {
		t.Run(which, func(t *testing.T) {
			s, installation, state, ticket := publicationAcknowledgementFixture(t)
			routes, auth := s.runtime.routes.Load(), s.runtime.auth.Load()
			switch which {
			case "no_ticket":
				s.runtimePublicationCommands.Delete(ticket.row.RequestID)
			case "replaced_ticket":
				copy := *ticket
				s.runtimePublicationCommands.Store(ticket.row.RequestID, &copy)
			case "wrong_process_birth":
				ticket.row.ClaimInstanceStartedAt = ticket.row.ClaimInstanceStartedAt.Add(time.Microsecond)
			case "expired_lease":
				installation.application.instance.LeaseExpiresAt = time.Now().Add(-time.Second)
			case "changed_claim":
				state.command.Reason = "A different retained claim"
			case "missing_installation":
				installation.rows = nil
			case "authorization_epoch":
				s.runtime.epoch.Add(1)
			case "late_cancel":
				ctx, cancel := context.WithCancel(ticket.ctx)
				ticket.ctx = ctx
				state.afterInsert = cancel
			case "late_process_replacement":
				state.afterInsert = func() { s.instance = nil }
			case "write_failure":
				state.failWrite = true
			}
			if err := recordPublicationAcknowledgementForTest(s, ticket); err == nil || len(state.rows) != 0 {
				t.Fatal("unproven installation received acknowledgement")
			}
			if s.runtime.routes.Load() != routes || s.runtime.auth.Load() != auth || !s.runtime.status.Load().Ready {
				t.Fatal("evidence failure discarded actual installation")
			}
		})
	}
}
func TestRuntimePublicationAcknowledgementCommitLossAndNoBackfill(t *testing.T) {
	s, _, state, ticket := publicationAcknowledgementFixture(t)
	state.loseCommit = true
	if err := recordPublicationAcknowledgementForTest(s, ticket); err == nil || len(state.rows) != 1 {
		t.Fatal("unknown commit response was not separate from actual storage")
	}
	state.loseCommit = false
	receipt, err := readPublicationAcknowledgementForTest(s, state.command)
	if err != nil || receipt.Acknowledgement == nil || receipt.Status != "pending" {
		t.Fatal("genuinely committed acknowledgement not recoverable", err)
	}
	state.rows = nil
	receipt, err = readPublicationAcknowledgementForTest(s, state.command)
	if err != nil || receipt.Acknowledgement != nil {
		t.Fatal("missing historical proof synthesized", err)
	}
}
