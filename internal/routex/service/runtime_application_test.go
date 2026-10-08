package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type runtimeApplicationSQLState struct {
	instance                      entity.SystemInstance
	rows                          []entity.RuntimeRoutingApplication
	failInsert, failCommit, alias bool
	afterInsert                   func()
	afterInstance                 func()
	inserted                      int
	deadline                      time.Duration
}
type runtimeApplicationConnector struct {
	f       *rolesSQLFixture
	control *roleDefinitionSQLControl
	state   *runtimeApplicationSQLState
}

func (c runtimeApplicationConnector) Connect(context.Context) (driver.Conn, error) {
	return &runtimeApplicationConnection{roleDefinitionSQLConnection: &roleDefinitionSQLConnection{rolesSQLConnection: &rolesSQLConnection{f: c.f}, control: c.control}, state: c.state}, nil
}
func (c runtimeApplicationConnector) Driver() driver.Driver { return runtimeApplicationDriver(c) }

type runtimeApplicationDriver runtimeApplicationConnector

func (d runtimeApplicationDriver) Open(string) (driver.Conn, error) {
	return runtimeApplicationConnector(d).Connect(context.Background())
}

type runtimeApplicationConnection struct {
	*roleDefinitionSQLConnection
	state  *runtimeApplicationSQLState
	staged []entity.RuntimeRoutingApplication
}

func (c *runtimeApplicationConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *runtimeApplicationConnection) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	tx, err := c.roleDefinitionSQLConnection.BeginTx(ctx, o)
	if err != nil {
		return nil, err
	}
	c.staged = append([]entity.RuntimeRoutingApplication{}, c.state.rows...)
	return runtimeApplicationTransaction{Tx: tx, c: c}, nil
}

type runtimeApplicationTransaction struct {
	driver.Tx
	c *runtimeApplicationConnection
}

func (t runtimeApplicationTransaction) Commit() error {
	if t.c.state.failCommit {
		_ = t.Tx.Rollback()
		t.c.staged = nil
		return errors.New("controlled evidence commit failure")
	}
	if err := t.Tx.Commit(); err != nil {
		return err
	}
	t.c.state.rows = t.c.staged
	t.c.staged = nil
	return nil
}
func (t runtimeApplicationTransaction) Rollback() error { t.c.staged = nil; return t.Tx.Rollback() }
func (c *runtimeApplicationConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch {
	case strings.Contains(q, `FROM "system_instances"`):
		if deadline, ok := ctx.Deadline(); ok {
			c.state.deadline = time.Until(deadline)
		}
		row := c.state.instance
		if c.state.alias {
			row.ID = strings.ToUpper(row.ID)
		}
		if c.state.afterInstance != nil {
			c.state.afterInstance()
		}
		return effectiveSQLRows([]entity.SystemInstance{row})
	case strings.Contains(q, `FROM "runtime_routing_applications"`):
		result := []entity.RuntimeRoutingApplication{}
		for _, row := range c.staged {
			match := true
			for _, arg := range args {
				value, ok := arg.Value.(string)
				if !ok {
					continue
				}
				switch {
				case strings.HasPrefix(value, "ins_"):
					match = match && row.InstanceID == value
				case strings.HasPrefix(value, "cfg_"):
					match = match && row.SnapshotID == value
				case strings.HasPrefix(value, "rap_"):
					match = match && row.ID < value
				}
			}
			if match {
				result = append(result, row)
			}
		}
		sort.Slice(result, func(i, j int) bool { return result[i].ID > result[j].ID })
		bound := regexp.MustCompile(`LIMIT (\d+)`).FindStringSubmatch(q)
		if len(bound) == 2 {
			n, _ := strconv.Atoi(bound[1])
			if len(result) > n {
				result = result[:n]
			}
		}
		return effectiveSQLRows(result)
	}
	return c.roleDefinitionSQLConnection.QueryContext(ctx, q, args)
}
func (c *runtimeApplicationConnection) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.Contains(q, `INSERT INTO "runtime_routing_applications"`) {
		if c.state.failInsert {
			return nil, errors.New("controlled evidence insert failure")
		}
		if len(args) != 7 {
			return nil, errors.New("unexpected evidence projection")
		}
		row := entity.RuntimeRoutingApplication{ID: args[0].Value.(string), InstanceID: args[1].Value.(string), InstanceStartedAt: args[2].Value.(time.Time), SnapshotID: args[3].Value.(string), RouteDigest: args[4].Value.(string), PublishedAt: args[5].Value.(time.Time), AppliedAt: args[6].Value.(time.Time)}
		c.staged = append(c.staged, row)
		c.state.inserted++
		if c.state.afterInsert != nil {
			c.state.afterInsert()
		}
		return driver.RowsAffected(1), nil
	}
	return c.roleDefinitionSQLConnection.ExecContext(ctx, q, args)
}
func runtimeApplicationSQLService(t *testing.T) (*Service, *rolesSQLFixture, *runtimeApplicationSQLState) {
	t.Helper()
	_, f, control := roleDefinitionSQLService(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	lease := &systemInstanceLease{id: "ins_01m36yee4gkbns18pfcqqc75a3", token: "private-token", startedAt: now.Add(-time.Minute)}
	state := &runtimeApplicationSQLState{instance: entity.SystemInstance{ID: lease.id, LeaseToken: lease.token, StartedAt: lease.startedAt, Role: systemInstanceRole, LeaseExpiresAt: now.Add(time.Minute)}}
	pool := sql.OpenDB(runtimeApplicationConnector{f, control, state})
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	r := &gatewayRuntime{}
	digest := strings.Repeat("a", 64)
	r.auth.Store(&runtimeAuthorization{SourceDigest: digest, ValidUntil: now.Add(time.Minute)})
	r.routes.Store(&runtimeRoutes{ID: "cfg_01m36yee4gkbns18pfcqqc75a3", Digest: digest, PublishedAt: now.Add(-time.Second)})
	r.status.Store(&RuntimeStatus{Enabled: true, Ready: true})
	return &Service{db: db, runtime: r, instance: lease}, f, state
}
func TestRuntimeApplicationExactCaptureAndIdempotentHistory(t *testing.T) {
	s, _, f := runtimeApplicationSQLService(t)
	r := s.runtime
	record := func() error {
		return s.recordRuntimeApplication(context.Background(), r.routes.Load(), r.auth.Load(), r.epoch.Load())
	}
	if err := record(); err != nil || len(f.rows) != 1 {
		t.Fatal("current capture failed", err)
	}
	original := f.rows[0]
	if f.deadline <= 0 || f.deadline > runtimeApplicationWriteBudget || original.InstanceID != f.instance.ID || original.RouteDigest != r.routes.Load().Digest {
		t.Fatal("evidence identity or budget changed")
	}
	if err := record(); err != nil || len(f.rows) != 1 || !reflect.DeepEqual(original, f.rows[0]) || f.inserted != 1 {
		t.Fatal("poll rewrote first application", err)
	}
	page, err := s.ListRuntimeApplications(context.Background(), "usr_admin", RuntimeApplicationFilter{})
	if err != nil || len(page.Items) != 1 || page.Items[0].CurrentServingSnapshotMatches == nil || !*page.Items[0].CurrentServingSnapshotMatches || page.Items[0].InstanceStatus != "online" {
		t.Fatal("independent live observation absent", err, page)
	}
	r.epoch.Add(1)
	page, err = s.ListRuntimeApplications(context.Background(), "usr_admin", RuntimeApplicationFilter{})
	if err != nil || page.Items[0].CurrentServingSnapshotMatches != nil || !page.Items[0].AppliedAt.Equal(original.AppliedAt) {
		t.Fatal("history became current failure proof", err)
	}
}
func TestRuntimeApplicationRejectedCapturesNeverCommitOrChangeRuntime(t *testing.T) {
	for _, name := range []string{"unregistered", "registration_busy", "expired_auth", "expired_instance", "wrong_token", "wrong_birth", "retired", "stopped", "alias", "bad_snapshot", "bad_digest", "source_mismatch", "stale_routes", "stale_auth", "revoked_during_read", "revoked_during_insert", "insert_failure", "commit_failure", "prior_corruption"} {
		t.Run(name, func(t *testing.T) {
			s, _, f := runtimeApplicationSQLService(t)
			r := s.runtime
			routes, auth, epoch := r.routes.Load(), r.auth.Load(), r.epoch.Load()
			now := time.Now()
			switch name {
			case "unregistered":
				s.instance = nil
			case "registration_busy":
				s.instanceMu.Lock()
				defer s.instanceMu.Unlock()
			case "expired_auth":
				auth.ValidUntil = now.Add(-time.Second)
			case "expired_instance":
				f.instance.LeaseExpiresAt = now.Add(-time.Second)
			case "wrong_token":
				f.instance.LeaseToken = "wrong"
			case "wrong_birth":
				f.instance.StartedAt = f.instance.StartedAt.Add(time.Microsecond)
			case "retired":
				f.instance.RetiredAt = &now
			case "stopped":
				f.instance.StoppedAt = &now
			case "alias":
				f.alias = true
			case "bad_snapshot":
				routes.ID = "cfg_fake"
			case "bad_digest":
				routes.Digest = "invalid"
			case "source_mismatch":
				auth.SourceDigest = strings.Repeat("b", 64)
			case "stale_routes":
				copy := *routes
				r.routes.Store(&copy)
			case "stale_auth":
				copy := *auth
				r.auth.Store(&copy)
			case "revoked_during_read":
				f.afterInstance = func() { r.epoch.Add(1) }
			case "revoked_during_insert":
				f.afterInsert = func() { r.epoch.Add(1) }
			case "insert_failure":
				f.failInsert = true
			case "commit_failure":
				f.failCommit = true
			case "prior_corruption":
				if err := s.recordRuntimeApplication(context.Background(), routes, auth, epoch); err != nil {
					t.Fatal(err)
				}
				f.rows[0].RouteDigest = strings.Repeat("c", 64)
			}
			history := append([]entity.RuntimeRoutingApplication(nil), f.rows...)
			beforeRoutes, beforeAuth := r.routes.Load(), r.auth.Load()
			if err := s.recordRuntimeApplication(context.Background(), routes, auth, epoch); err == nil {
				t.Fatal("rejected evidence acknowledged")
			}
			if !reflect.DeepEqual(history, f.rows) || r.routes.Load() != beforeRoutes || r.auth.Load() != beforeAuth {
				t.Fatal("evidence failure mutated history/runtime")
			}
		})
	}
}
func TestRuntimeApplicationReadAuthorityBoundsAndIndependentCurrentState(t *testing.T) {
	s, f, state := runtimeApplicationSQLService(t)
	if err := s.recordRuntimeApplication(context.Background(), s.runtime.routes.Load(), s.runtime.auth.Load(), 0); err != nil {
		t.Fatal(err)
	}
	state.rows[0].ID = "rap_01m36yee4gkbns18pfcqqc75a3"
	first := state.rows[0]
	second := first
	second.ID = "rap_01m36yee4gkbns18pfcqqc75a4"
	second.SnapshotID = "cfg_01m36yee4gkbns18pfcqqc75a4"
	state.rows = append(state.rows, second)
	page, err := s.ListRuntimeApplications(context.Background(), "usr_admin", RuntimeApplicationFilter{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.NextCursor == nil || page.Items[0].CurrentServingSnapshotMatches == nil || *page.Items[0].CurrentServingSnapshotMatches {
		t.Fatal("bounded page lost exact current mismatch", err)
	}
	next, err := s.ListRuntimeApplications(context.Background(), "usr_admin", RuntimeApplicationFilter{Limit: 1, Cursor: *page.NextCursor})
	if err != nil || len(next.Items) != 1 || next.Items[0].ID != first.ID || next.NextCursor != nil {
		t.Fatal("cursor lost retained history", err)
	}
	if _, err := s.ListRuntimeApplications(context.Background(), "other", RuntimeApplicationFilter{Cursor: *page.NextCursor}); !errors.Is(err, apperrors.ErrBadRequest) {
		t.Fatal("actor borrowed cursor", err)
	}
	if _, err := s.ListRuntimeApplications(context.Background(), "usr_admin", RuntimeApplicationFilter{InstanceID: state.instance.ID, Cursor: *page.NextCursor}); !errors.Is(err, apperrors.ErrBadRequest) {
		t.Fatal("filter borrowed cursor", err)
	}
	f.deny["system.read"] = true
	if _, err := s.ListRuntimeApplications(context.Background(), "usr_admin", RuntimeApplicationFilter{}); !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatal("read permission bypass", err)
	}
	f.deny["system.read"] = false
	f.actorAlias = true
	if _, err := s.ListRuntimeApplications(context.Background(), "usr_admin", RuntimeApplicationFilter{}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("actor alias borrowed history", err)
	}
	f.actorAlias = false
	s.instance = nil
	page, err = s.ListRuntimeApplications(context.Background(), "usr_admin", RuntimeApplicationFilter{})
	if err != nil || page.Items[0].CurrentServingSnapshotMatches != nil {
		t.Fatal("another process inferred current match", err)
	}
	state.instance.StartedAt = state.instance.StartedAt.Add(time.Microsecond)
	page, err = s.ListRuntimeApplications(context.Background(), "usr_admin", RuntimeApplicationFilter{})
	if err != nil || page.Items[0].InstanceStatus != "unknown" {
		t.Fatal("birth alias borrowed liveness", err)
	}
	for _, tx := range f.transactions {
		if !tx.ReadOnly && tx.Isolation == driver.IsolationLevel(sql.LevelRepeatableRead) {
			t.Fatal("read snapshot writable")
		}
	}
	if len(state.rows) != 2 {
		t.Fatal("reads changed history")
	}
}
func TestRuntimeApplicationCursorAndLimitStrict(t *testing.T) {
	for _, raw := range []string{"", "0", "101", "01", "+1", "1.0", " 1"} {
		if _, err := ParseRuntimeApplicationLimit(raw); err == nil {
			t.Fatal("noncanonical limit", raw)
		}
	}
	for _, raw := range []string{"1", "20", "100"} {
		if _, err := ParseRuntimeApplicationLimit(raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []RuntimeApplicationFilter{{Limit: -1}, {Limit: 101}, {InstanceID: "INS_ALIAS"}, {Cursor: "not-base64"}, {Cursor: strings.Repeat("x", 201)}} {
		if _, _, err := validateRuntimeApplicationFilter("usr_admin", f); err == nil {
			t.Fatal("invalid filter accepted")
		}
	}
}
