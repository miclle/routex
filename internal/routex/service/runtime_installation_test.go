package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
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

// Extends the existing V87 SQL fixture, including real database/sql transaction
// staging. No real driver/server or fake clock is used by these service controls.
type runtimeInstallationSQLState struct {
	application                                        *runtimeApplicationSQLState
	rows                                               []entity.RuntimeInstallationObservation
	failInsert, failCommit, priorAlias, duplicatePrior bool
	afterPrior, afterInsert, afterCommit               func()
	deadlines                                          []time.Time
	queries, inserted                                  int
}
type runtimeInstallationConnector struct {
	base  runtimeApplicationConnector
	state *runtimeInstallationSQLState
}

func (c runtimeInstallationConnector) Connect(context.Context) (driver.Conn, error) {
	base := &runtimeApplicationConnection{roleDefinitionSQLConnection: &roleDefinitionSQLConnection{rolesSQLConnection: &rolesSQLConnection{f: c.base.f}, control: c.base.control}, state: c.base.state}
	return &runtimeInstallationConnection{runtimeApplicationConnection: base, state: c.state}, nil
}
func (c runtimeInstallationConnector) Driver() driver.Driver { return runtimeInstallationDriver(c) }

type runtimeInstallationDriver runtimeInstallationConnector

func (d runtimeInstallationDriver) Open(string) (driver.Conn, error) {
	return runtimeInstallationConnector(d).Connect(context.Background())
}

type runtimeInstallationConnection struct {
	*runtimeApplicationConnection
	state              *runtimeInstallationSQLState
	installationStaged []entity.RuntimeInstallationObservation
}

func (c *runtimeInstallationConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *runtimeInstallationConnection) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	tx, err := c.runtimeApplicationConnection.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	c.installationStaged = append([]entity.RuntimeInstallationObservation{}, c.state.rows...)
	return runtimeInstallationTransaction{Tx: tx, c: c}, nil
}

type runtimeInstallationTransaction struct {
	driver.Tx
	c *runtimeInstallationConnection
}

func (t runtimeInstallationTransaction) Commit() error {
	if t.c.state.failCommit {
		_ = t.Tx.Rollback()
		t.c.installationStaged = nil
		return errors.New("controlled installation commit failure")
	}
	if err := t.Tx.Commit(); err != nil {
		return err
	}
	t.c.state.rows = t.c.installationStaged
	t.c.installationStaged = nil
	if t.c.state.afterCommit != nil {
		t.c.state.afterCommit()
	}
	return nil
}
func (t runtimeInstallationTransaction) Rollback() error {
	t.c.installationStaged = nil
	return t.Tx.Rollback()
}
func (c *runtimeInstallationConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if end, ok := ctx.Deadline(); ok {
		c.state.deadlines = append(c.state.deadlines, end)
	}
	if !strings.Contains(q, `FROM "runtime_installation_observations"`) {
		return c.runtimeApplicationConnection.QueryContext(ctx, q, args)
	}
	c.state.queries++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := []entity.RuntimeInstallationObservation{}
	for _, row := range c.installationStaged {
		match := true
		for _, arg := range args {
			value, ok := arg.Value.(string)
			if !ok {
				continue
			}
			switch {
			case strings.HasPrefix(value, "ins_"):
				match = match && row.InstanceID == value
			case strings.HasPrefix(value, "rin_"):
				match = match && row.ID < value
			case len(value) == 64:
				match = match && row.SourceDigest == value
			}
		}
		if match {
			if c.state.priorAlias {
				row.InstanceID = strings.ToUpper(row.InstanceID)
			}
			result = append(result, row)
		}
	}
	if c.state.duplicatePrior && len(result) > 0 {
		result = append(result, result[0])
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID > result[j].ID })
	bound := regexp.MustCompile(`LIMIT (\d+)`).FindStringSubmatch(q)
	if len(bound) == 2 {
		n, _ := strconv.Atoi(bound[1])
		if len(result) > n {
			result = result[:n]
		}
	}
	if c.state.afterPrior != nil {
		c.state.afterPrior()
	}
	return effectiveSQLRows(result)
}
func (c *runtimeInstallationConnection) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if end, ok := ctx.Deadline(); ok {
		c.state.deadlines = append(c.state.deadlines, end)
	}
	if !strings.Contains(q, `INSERT INTO "runtime_installation_observations"`) {
		return c.runtimeApplicationConnection.ExecContext(ctx, q, args)
	}
	if c.state.failInsert {
		return nil, errors.New("controlled installation insert failure")
	}
	if len(args) != 8 {
		return nil, errors.New("unexpected installation fields")
	}
	version, ok := args[4].Value.(int64)
	if !ok {
		return nil, errors.New("unexpected installation version")
	}
	row := entity.RuntimeInstallationObservation{ID: args[0].Value.(string), InstanceID: args[1].Value.(string), InstanceStartedAt: args[2].Value.(time.Time), SnapshotID: args[3].Value.(string), ProjectionVersion: int(version), SourceDigest: args[5].Value.(string), RoutesPublishedAt: args[6].Value.(time.Time), FirstObservedAt: args[7].Value.(time.Time)}
	c.installationStaged = append(c.installationStaged, row)
	c.state.inserted++
	if c.state.afterInsert != nil {
		c.state.afterInsert()
	}
	return driver.RowsAffected(1), nil
}
func runtimeInstallationSQLService(t *testing.T) (*Service, *rolesSQLFixture, *runtimeInstallationSQLState) {
	t.Helper()
	_, f, control := roleDefinitionSQLService(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	lease := &systemInstanceLease{id: "ins_01m36yee4gkbns18pfcqqc75a3", token: "private-installation-token", startedAt: now.Add(-time.Minute)}
	app := &runtimeApplicationSQLState{instance: entity.SystemInstance{ID: lease.id, LeaseToken: lease.token, StartedAt: lease.startedAt, Role: systemInstanceRole, LeaseExpiresAt: now.Add(time.Minute)}}
	state := &runtimeInstallationSQLState{application: app}
	pool := sql.OpenDB(runtimeInstallationConnector{base: runtimeApplicationConnector{f, control, app}, state: state})
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	r := &gatewayRuntime{done: make(chan struct{})}
	r.auth.Store(&runtimeAuthorization{SourceDigest: strings.Repeat("a", 64), installationProjectionDigest: strings.Repeat("b", 64), ValidUntil: now.Add(time.Minute)})
	r.routes.Store(&runtimeRoutes{ID: "cfg_01m36yee4gkbns18pfcqqc75a3", Digest: strings.Repeat("a", 64), PublishedAt: now.Add(-time.Second)})
	r.status.Store(&RuntimeStatus{Enabled: true, Ready: true})
	return &Service{db: db, runtime: r, instance: lease}, f, state
}
func runtimeInstallationRecordForTest(s *Service) error {
	ctx, cancel := runtimeInstallationEvidenceContext(context.Background(), s.runtime.auth.Load())
	defer cancel()
	return s.recordRuntimeInstallation(ctx, s.runtime.routes.Load(), s.runtime.auth.Load(), s.runtime.epoch.Load())
}

func TestRuntimeInstallationExactSourceAndImmutableHistory(t *testing.T) {
	s, _, state := runtimeInstallationSQLService(t)
	ctx, cancel := runtimeInstallationEvidenceContext(context.Background(), s.runtime.auth.Load())
	defer cancel()
	end, _ := ctx.Deadline()
	s.recordRuntimeObservations(ctx, s.runtime.routes.Load(), s.runtime.auth.Load(), 0)
	if len(state.rows) != 1 || len(state.application.rows) != 1 {
		t.Fatal("legacy/new observations missing")
	}
	original, legacy := state.rows[0], state.application.rows[0]
	if !validRuntimeInstallation(original) || original.InstanceID != s.instance.id || !original.InstanceStartedAt.Equal(s.instance.startedAt) {
		t.Fatal("exact installation facts missing")
	}
	for _, deadline := range state.deadlines {
		if !deadline.Equal(end) {
			t.Fatal("projection/legacy/new evidence deadline renewed")
		}
	}
	if err := runtimeInstallationRecordForTest(s); err != nil || state.inserted != 1 || !reflect.DeepEqual(state.rows[0], original) {
		t.Fatal("first observation rewritten", err)
	}
	// Lease-only republication changes neither the logical source nor first time.
	auth := *s.runtime.auth.Load()
	auth.ValidUntil = time.Now().Add(time.Minute)
	s.runtime.auth.Store(&auth)
	if err := runtimeInstallationRecordForTest(s); err != nil || state.inserted != 1 || !reflect.DeepEqual(state.rows[0], original) {
		t.Fatal("lease renewal fabricated source", err)
	}
	// Same routing snapshot with a different installed authorization is distinct.
	next := auth
	next.installationProjectionDigest = strings.Repeat("c", 64)
	s.runtime.auth.Store(&next)
	if err := runtimeInstallationRecordForTest(s); err != nil || len(state.rows) != 2 || state.rows[1].SourceDigest == original.SourceDigest || !reflect.DeepEqual(state.rows[0], original) || !reflect.DeepEqual(state.application.rows[0], legacy) {
		t.Fatal("authorization-only change lost or rewrote history", err)
	}
	page, err := s.ListRuntimeInstallations(context.Background(), "usr_admin", RuntimeInstallationFilter{})
	if err != nil || page.Scope != "single_process_gateway_admission" || len(page.Items) != 2 {
		t.Fatal("scoped history missing", err)
	}
	for _, row := range page.Items {
		if row.CurrentServingInstallationMatches == nil || row.InstanceStatus != "online" || *row.CurrentServingInstallationMatches != (row.ID != original.ID) {
			t.Fatal("independent current match incorrect")
		}
	}
	encoded, err := json.Marshal(page)
	if err != nil || strings.Contains(string(encoded), original.SourceDigest) || strings.Contains(string(encoded), "private-installation-token") || strings.Contains(string(encoded), "source_digest") || strings.Contains(string(encoded), "ProjectionDigest") {
		t.Fatal("private source material exposed")
	}
	private, err := json.Marshal(original)
	if err != nil || strings.Contains(string(private), original.SourceDigest) {
		t.Fatal("entity accidentally exposes private digest")
	}
}

func TestRuntimeInstallationRejectedEvidencePreservesHistoryAndRuntime(t *testing.T) {
	for _, name := range []string{"unregistered", "registration_busy", "expired_auth", "expired_instance", "wrong_token", "wrong_birth", "wrong_role", "retired", "stopped", "instance_alias", "failed_routes", "stale_routes", "stale_auth", "newer_epoch", "unknown_projection", "malformed_projection", "source_mismatch", "publisher_stopped", "cancelled", "unbounded_context", "cancel_after_read", "epoch_after_prior", "cancel_after_insert", "registration_after_insert", "insert_failure", "commit_failure", "prior_corruption", "prior_alias", "duplicate_prior", "late_commit"} {
		t.Run(name, func(t *testing.T) {
			s, _, state := runtimeInstallationSQLService(t)
			if err := runtimeInstallationRecordForTest(s); err != nil || len(state.rows) != 1 {
				t.Fatal("positive prerequisite failed", err)
			}
			auth := *s.runtime.auth.Load()
			s.runtime.auth.Store(&auth)
			routes, epoch := s.runtime.routes.Load(), s.runtime.epoch.Load()
			ctx, cancel := runtimeInstallationEvidenceContext(context.Background(), &auth)
			defer cancel()
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
				state.application.instance.LeaseExpiresAt = now.Add(-time.Second)
			case "wrong_token":
				state.application.instance.LeaseToken = "wrong"
			case "wrong_birth":
				state.application.instance.StartedAt = state.application.instance.StartedAt.Add(time.Microsecond)
			case "wrong_role":
				state.application.instance.Role = "other"
			case "retired":
				state.application.instance.RetiredAt = &now
			case "stopped":
				state.application.instance.StoppedAt = &now
			case "instance_alias":
				state.application.alias = true
			case "failed_routes":
				s.runtime.status.Store(&RuntimeStatus{ErrorCode: "invalid_configuration"})
			case "stale_routes":
				copy := *routes
				s.runtime.routes.Store(&copy)
			case "stale_auth":
				copy := auth
				s.runtime.auth.Store(&copy)
			case "newer_epoch":
				s.runtime.epoch.Add(1)
			case "unknown_projection":
				auth.installationProjectionDigest = ""
			case "malformed_projection":
				auth.installationProjectionDigest = "INVALID"
			case "source_mismatch":
				auth.SourceDigest = strings.Repeat("d", 64)
			case "publisher_stopped":
				close(s.runtime.done)
			case "cancelled":
				cancel()
			case "unbounded_context":
				ctx = context.Background()
			case "cancel_after_read":
				state.application.afterInstance = cancel
			case "epoch_after_prior":
				state.afterPrior = func() { s.runtime.epoch.Add(1) }
			case "cancel_after_insert":
				auth.installationProjectionDigest = strings.Repeat("e", 64)
				state.afterInsert = cancel
			case "registration_after_insert":
				auth.installationProjectionDigest = strings.Repeat("e", 64)
				state.afterInsert = func() {
					original := s.instance
					// Replace the registration handle without copying its lifetime locks.
					s.instance = &systemInstanceLease{
						id: original.id, startedAt: original.startedAt, token: original.token,
						storagePath: original.storagePath, cancel: original.cancel,
						done: original.done, shutdown: original.shutdown,
					}
				}
			case "insert_failure":
				auth.installationProjectionDigest = strings.Repeat("e", 64)
				state.failInsert = true
			case "commit_failure":
				auth.installationProjectionDigest = strings.Repeat("e", 64)
				state.failCommit = true
			case "prior_corruption":
				state.rows[0].ProjectionVersion = 2
			case "prior_alias":
				state.priorAlias = true
			case "duplicate_prior":
				state.duplicatePrior = true
			case "late_commit":
				state.afterCommit = cancel
			}
			history := append([]entity.RuntimeInstallationObservation(nil), state.rows...)
			beforeAuth, beforeRoutes := s.runtime.auth.Load(), s.runtime.routes.Load()
			if err := s.recordRuntimeInstallation(ctx, routes, &auth, epoch); err == nil {
				t.Fatal("rejected evidence acknowledged")
			}
			if !reflect.DeepEqual(history, state.rows) || s.runtime.auth.Load() != beforeAuth || s.runtime.routes.Load() != beforeRoutes {
				t.Fatal("evidence failure mutated source or history")
			}
		})
	}
}

func TestRuntimeInstallationDeadlineAndFreshPostLockTime(t *testing.T) {
	t.Run("fresh_observation_after_lock", func(t *testing.T) {
		s, _, state := runtimeInstallationSQLService(t)
		var guarded time.Time
		state.application.afterInstance = func() { guarded = time.Now().UTC().Truncate(time.Microsecond) }
		if err := runtimeInstallationRecordForTest(s); err != nil || len(state.rows) != 1 || state.rows[0].FirstObservedAt.Before(guarded) {
			t.Fatal("observation predates guarded read", err)
		}
	})
	t.Run("lease_expires_during_instance_read", func(t *testing.T) {
		s, _, state := runtimeInstallationSQLService(t)
		state.application.instance.LeaseExpiresAt = time.Now().Add(10 * time.Millisecond)
		state.application.afterInstance = func() { time.Sleep(20 * time.Millisecond) }
		if err := runtimeInstallationRecordForTest(s); err == nil || len(state.rows) != 0 {
			t.Fatal("old pre-lock liveness accepted")
		}
	})
	t.Run("late_positive_prior_read", func(t *testing.T) {
		s, _, state := runtimeInstallationSQLService(t)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		state.afterPrior = func() { time.Sleep(20 * time.Millisecond) }
		if err := s.recordRuntimeInstallation(ctx, s.runtime.routes.Load(), s.runtime.auth.Load(), 0); err == nil || len(state.rows) != 0 {
			t.Fatal("late positive SQL response acknowledged")
		}
	})
	t.Run("exhausted_shared_budget_no_dispatch", func(t *testing.T) {
		s, _, state := runtimeInstallationSQLService(t)
		ctx, cancel := runtimeInstallationEvidenceContext(context.Background(), s.runtime.auth.Load())
		cancel()
		s.recordRuntimeObservations(ctx, s.runtime.routes.Load(), s.runtime.auth.Load(), 0)
		if state.queries != 0 || len(state.deadlines) != 0 || len(state.application.rows) != 0 || len(state.rows) != 0 {
			t.Fatal("expired shared context dispatched evidence")
		}
	})
	t.Run("original_parent_and_lease_cap", func(t *testing.T) {
		s, _, _ := runtimeInstallationSQLService(t)
		parent, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer stop()
		ctx, cancel := runtimeInstallationEvidenceContext(parent, s.runtime.auth.Load())
		defer cancel()
		parentEnd, _ := parent.Deadline()
		end, _ := ctx.Deadline()
		if !end.Equal(parentEnd) {
			t.Fatal("parent budget expanded")
		}
		auth := *s.runtime.auth.Load()
		auth.ValidUntil = time.Now().Add(time.Millisecond)
		leaseCtx, leaseCancel := runtimeInstallationEvidenceContext(context.Background(), &auth)
		defer leaseCancel()
		leaseEnd, _ := leaseCtx.Deadline()
		if !leaseEnd.Equal(auth.ValidUntil) {
			t.Fatal("authorization lease expanded")
		}
	})
}

func TestRuntimeInstallationReadAuthorityCursorAndUnknowns(t *testing.T) {
	s, f, state := runtimeInstallationSQLService(t)
	if err := runtimeInstallationRecordForTest(s); err != nil {
		t.Fatal(err)
	}
	first := state.rows[0]
	first.ID = "rin_01m36yee4gkbns18pfcqqc75a3"
	state.rows[0] = first
	second := first
	second.ID = "rin_01m36yee4gkbns18pfcqqc75a4"
	second.SourceDigest = strings.Repeat("f", 64)
	state.rows = append(state.rows, second)
	var readCompleted time.Time
	state.afterPrior = func() { readCompleted = time.Now().UTC() }
	page, err := s.ListRuntimeInstallations(context.Background(), "usr_admin", RuntimeInstallationFilter{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.NextCursor == nil || page.ObservedAt.Before(readCompleted) || page.Items[0].CurrentServingInstallationMatches == nil || *page.Items[0].CurrentServingInstallationMatches {
		t.Fatal("bounded current mismatch missing", err)
	}
	state.afterPrior = nil
	next, err := s.ListRuntimeInstallations(context.Background(), "usr_admin", RuntimeInstallationFilter{Limit: 1, Cursor: *page.NextCursor})
	if err != nil || len(next.Items) != 1 || next.Items[0].ID != first.ID || next.NextCursor != nil {
		t.Fatal("history cursor lost first row", err)
	}
	for _, test := range []struct {
		actor  string
		filter RuntimeInstallationFilter
	}{{"usr_other", RuntimeInstallationFilter{Cursor: *page.NextCursor}}, {"usr_admin", RuntimeInstallationFilter{InstanceID: state.application.instance.ID, Cursor: *page.NextCursor}}, {"usr_admin", RuntimeInstallationFilter{Cursor: runtimeApplicationCursor("usr_admin", RuntimeApplicationFilter{}, "rap_01m36yee4gkbns18pfcqqc75a3")}}} {
		if _, err := s.ListRuntimeInstallations(context.Background(), test.actor, test.filter); !errors.Is(err, apperrors.ErrBadRequest) {
			t.Fatal("borrowed cursor accepted", err)
		}
	}
	f.deny["system.write"] = true
	if _, err := s.ListRuntimeInstallations(context.Background(), "usr_admin", RuntimeInstallationFilter{}); err != nil {
		t.Fatal("reader wrongly requires write", err)
	}
	f.deny["system.write"] = false
	f.deny["system.read"] = true
	if _, err := s.ListRuntimeInstallations(context.Background(), "usr_admin", RuntimeInstallationFilter{}); !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatal("writer bypassed independent read", err)
	}
	f.deny["system.read"] = false
	f.actorAlias = true
	if _, err := s.ListRuntimeInstallations(context.Background(), "usr_admin", RuntimeInstallationFilter{}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("actor alias borrowed private rows", err)
	}
	f.actorAlias = false
	history := append([]entity.RuntimeInstallationObservation(nil), state.rows...)
	for _, name := range []string{"other_process", "unknown_projection", "newer_epoch", "failed_routes", "wrong_birth", "expired_instance", "stopped_publisher"} {
		t.Run(name, func(t *testing.T) {
			originalLease, originalAuth, originalStatus := s.instance, s.runtime.auth.Load(), s.runtime.status.Load()
			originalInstance, originalEpoch := state.application.instance, s.runtime.epoch.Load()
			switch name {
			case "other_process":
				s.instance = nil
			case "unknown_projection":
				copy := *originalAuth
				copy.installationProjectionDigest = ""
				s.runtime.auth.Store(&copy)
			case "newer_epoch":
				s.runtime.epoch.Add(1)
			case "failed_routes":
				s.runtime.status.Store(&RuntimeStatus{ErrorCode: "invalid_configuration"})
			case "wrong_birth":
				state.application.instance.StartedAt = state.application.instance.StartedAt.Add(time.Microsecond)
			case "expired_instance":
				state.application.instance.LeaseExpiresAt = time.Now().Add(-time.Second)
			case "stopped_publisher":
				close(s.runtime.done)
			}
			got, err := s.ListRuntimeInstallations(context.Background(), "usr_admin", RuntimeInstallationFilter{})
			if err != nil || len(got.Items) != 2 || got.Items[0].CurrentServingInstallationMatches != nil || !reflect.DeepEqual(history, state.rows) {
				t.Fatal("unknown current state rewrote history or became a boolean", err)
			}
			if name == "wrong_birth" && got.Items[0].InstanceStatus != "unknown" {
				t.Fatal("wrong birth borrowed liveness")
			}
			s.instance = originalLease
			s.runtime.auth.Store(originalAuth)
			s.runtime.status.Store(originalStatus)
			state.application.instance = originalInstance
			s.runtime.epoch.Store(originalEpoch)
		})
	}
	for _, tx := range f.transactions {
		if !tx.ReadOnly && tx.Isolation == driver.IsolationLevel(sql.LevelRepeatableRead) {
			t.Fatal("history read snapshot writable")
		}
	}
}

func TestRuntimeInstallationStrictRowsFiltersAndPrivateSource(t *testing.T) {
	for _, raw := range []string{"", "0", "101", "01", "+1", "1.0", " 1"} {
		if _, err := ParseRuntimeInstallationLimit(raw); err == nil {
			t.Fatal("noncanonical limit accepted", raw)
		}
	}
	for _, raw := range []string{"1", "20", "100"} {
		if _, err := ParseRuntimeInstallationLimit(raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []RuntimeInstallationFilter{{Limit: -1}, {Limit: 101}, {InstanceID: "INS_ALIAS"}, {Cursor: "invalid"}, {Cursor: strings.Repeat("x", 201)}} {
		if _, _, err := validateRuntimeInstallationFilter("usr_admin", f); err == nil {
			t.Fatal("invalid filter accepted")
		}
	}
	for _, name := range []string{"future_version", "bad_id", "bad_source", "nanosecond", "reversed_time", "duplicate_row", "instance_alias"} {
		t.Run(name, func(t *testing.T) {
			s, _, state := runtimeInstallationSQLService(t)
			if err := runtimeInstallationRecordForTest(s); err != nil {
				t.Fatal("positive prerequisite", err)
			}
			switch name {
			case "future_version":
				state.rows[0].ProjectionVersion = 2
			case "bad_id":
				state.rows[0].ID = "rin_invalid"
			case "bad_source":
				state.rows[0].SourceDigest = "invalid"
			case "nanosecond":
				state.rows[0].FirstObservedAt = state.rows[0].FirstObservedAt.Add(time.Nanosecond)
			case "reversed_time":
				state.rows[0].FirstObservedAt = state.rows[0].RoutesPublishedAt.Add(-time.Microsecond)
			case "duplicate_row":
				state.rows = append(state.rows, state.rows[0])
			case "instance_alias":
				state.application.alias = true
			}
			if page, err := s.ListRuntimeInstallations(context.Background(), "usr_admin", RuntimeInstallationFilter{}); err == nil || page != nil {
				t.Fatal("corrupt/aliased retained row accepted")
			}
		})
	}
}

func TestRuntimeInstallationRefreshCachesActualBuilderProjection(t *testing.T) {
	s, _, _, fixture := providerMetadataSQLService(t)
	if err := s.RefreshRuntime(context.Background()); err != nil {
		t.Fatal(err)
	}
	auth := s.runtime.auth.Load()
	if auth == nil || !runtimeRouteDigest.MatchString(auth.installationProjectionDigest) {
		t.Fatal("real builder projection not cached before publication")
	}
	projected, err := runtimeAuthorizationProjection(context.Background(), auth)
	if err != nil || projected != auth.installationProjectionDigest {
		t.Fatal("cached source differs from actual installed authorization", err)
	}
	before := auth.installationProjectionDigest
	fixture.base.provider.Enabled = false
	if err := s.RefreshRuntime(context.Background()); err != nil {
		t.Fatal(err)
	}
	next := s.runtime.auth.Load()
	if next == auth || next.installationProjectionDigest == "" || next.installationProjectionDigest == before || next.Providers[fixture.base.provider.ID].Enabled || auth.installationProjectionDigest != before {
		t.Fatal("fresh authorization did not replace source immutably")
	}
}

func TestRuntimeInstallationInvalidRoutesRetainOldSnapshotAndFreshDenial(t *testing.T) {
	s, _, _, fixture := providerMetadataSQLService(t)
	if err := s.RefreshRuntime(context.Background()); err != nil {
		t.Fatal("valid prerequisite", err)
	}
	beforeAuth, beforeRoutes := s.runtime.auth.Load(), s.runtime.routes.Load()
	if beforeAuth == nil || beforeRoutes == nil || beforeAuth.installationProjectionDigest == "" {
		t.Fatal("valid installed prerequisite absent")
	}
	fixture.base.provider.Enabled = false
	if err := s.db.Callback().Query().After("gorm:query").Register("test:installation_invalid_routes", func(tx *gorm.DB) {
		if tx.Statement.Table == "model_provider_bindings" {
			if rows, ok := tx.Statement.Dest.(*[]entity.ModelProviderBinding); ok {
				*rows = []entity.ModelProviderBinding{{ID: "bnd_invalid", ModelID: "mdl_invalid", ProviderModelID: "pmd_missing", Weight: 100}}
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RefreshRuntime(context.Background()); !errors.Is(err, runtimeUnavailable) {
		t.Fatal("invalid routing accepted", err)
	}
	next := s.runtime.auth.Load()
	if next == beforeAuth || next.Providers[fixture.base.provider.ID].Enabled || s.runtime.routes.Load() != beforeRoutes || s.runtime.status.Load().ErrorCode != "invalid_configuration" {
		t.Fatal("invalid route prevented current denial or replaced old routes")
	}
	if s.runtimeInstallationCaptureCurrent(context.Background(), beforeRoutes, next, s.runtime.epoch.Load(), s.runtime.status.Load()) {
		t.Fatal("failed route status acknowledged an installed pair")
	}
}

func TestRuntimeInstallationUnknownProjectionKeepsLegacyRoutingEvidence(t *testing.T) {
	s, _, state := runtimeInstallationSQLService(t)
	auth := *s.runtime.auth.Load()
	auth.installationProjectionDigest = ""
	s.runtime.auth.Store(&auth)
	ctx, cancel := runtimeInstallationEvidenceContext(context.Background(), &auth)
	defer cancel()
	s.recordRuntimeObservations(ctx, s.runtime.routes.Load(), &auth, 0)
	if len(state.application.rows) != 1 || len(state.rows) != 0 || state.queries != 0 {
		t.Fatal("unavailable projection promoted or removed V87 evidence")
	}
	page, err := s.ListRuntimeInstallations(context.Background(), "usr_admin", RuntimeInstallationFilter{})
	if err != nil || len(page.Items) != 0 || page.NextCursor != nil {
		t.Fatal("legacy evidence promoted into installation history", err)
	}
}

func TestRuntimeInstallationSourceBindsSnapshotAndAuthorization(t *testing.T) {
	s, _, _ := runtimeInstallationSQLService(t)
	routes, auth := *s.runtime.routes.Load(), *s.runtime.auth.Load()
	original, known := runtimeInstallationSource(&routes, &auth)
	if !known || !runtimeRouteDigest.MatchString(original) {
		t.Fatal("positive source prerequisite")
	}
	for _, name := range []string{"snapshot", "routing", "authorization"} {
		r, a := routes, auth
		switch name {
		case "snapshot":
			r.ID = "cfg_01m36yee4gkbns18pfcqqc75a4"
		case "routing":
			r.Digest = strings.Repeat("e", 64)
			a.SourceDigest = r.Digest
		case "authorization":
			a.installationProjectionDigest = strings.Repeat("f", 64)
		}
		next, ok := runtimeInstallationSource(&r, &a)
		if !ok || next == original {
			t.Fatal("installed source identity ignored", name)
		}
	}
	auth.installationProjectionDigest = ""
	if source, ok := runtimeInstallationSource(&routes, &auth); ok || source != "" {
		t.Fatal("missing projection synthesized from routing hash")
	}
}
