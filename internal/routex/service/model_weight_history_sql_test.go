package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// Strict source-only SQL driver: no network, no real DB; unknown statements fail.
// Transaction clones prove journal/receipt/audit rollback instead of mirroring
// a successful return value. Driver-level lifecycle acceptance remains separate.
type weightSQLData struct {
	Actor       entity.User
	Model       entity.Model
	Bindings    []entity.ModelProviderBinding
	Models      []entity.ProviderModel
	Connections []entity.ProviderConnection
	Providers   []entity.Provider
	Names       []entity.ModelName
	Credentials []entity.ProviderCredential
	Access      []entity.CredentialModelAccess
	Versions    map[string]entity.ModelWeightVersion
	Commands    map[string]entity.ModelWeightRollbackCommand
	Audits      []entity.AuditEvent
}

func (d weightSQLData) clone() weightSQLData {
	d.Bindings = slices.Clone(d.Bindings)
	d.Versions = maps.Clone(d.Versions)
	d.Commands = maps.Clone(d.Commands)
	d.Audits = slices.Clone(d.Audits)
	return d
}

type weightSQLFixture struct {
	mu              sync.Mutex
	data            weightSQLData
	queries, writes []string
	options         []driver.TxOptions
	failAudit       bool
	deny            map[string]bool
	failTable       string
	onCommit        func()
}
type weightSQLConnector struct{ f *weightSQLFixture }

func (c weightSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &weightSQLConnection{f: c.f}, nil
}
func (c weightSQLConnector) Driver() driver.Driver { return weightSQLDriver(c) }

type weightSQLDriver weightSQLConnector

func (c weightSQLDriver) Open(string) (driver.Conn, error) { return &weightSQLConnection{f: c.f}, nil }

type weightSQLConnection struct {
	f        *weightSQLFixture
	data     *weightSQLData
	readOnly bool
}

func (c *weightSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}
func (c *weightSQLConnection) Close() error { return nil }
func (c *weightSQLConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *weightSQLConnection) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.f.mu.Lock()
	d := c.f.data.clone()
	c.data = &d
	c.readOnly = o.ReadOnly
	c.f.options = append(c.f.options, o)
	return weightSQLTransaction{c}, nil
}

type weightSQLTransaction struct{ c *weightSQLConnection }

func (t weightSQLTransaction) Commit() error {
	t.c.f.data = *t.c.data
	t.c.data = nil
	callback := t.c.f.onCommit
	t.c.f.mu.Unlock()
	if callback != nil {
		callback()
	}
	return nil
}
func (t weightSQLTransaction) Rollback() error { t.c.data = nil; t.c.f.mu.Unlock(); return nil }
func (c *weightSQLConnection) current() *weightSQLData {
	if c.data != nil {
		return c.data
	}
	return &c.f.data
}
func (c *weightSQLConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.f.queries = append(c.f.queries, q)
	if c.f.failTable != "" && strings.Contains(q, c.f.failTable) {
		return nil, errors.New("controlled selected facts unavailable")
	}
	d := c.current()
	values := rolesSQLStrings(args)
	switch {
	case strings.Contains(q, `FROM "governance_settings"`):
		return effectiveSQLRows([]entity.GovernanceSetting{{ID: 1}})
	case strings.Contains(q, `FROM "users"`):
		if slices.Contains(values, d.Actor.ID) && !d.Actor.Disabled && d.Actor.OffboardedAt == nil {
			return effectiveSQLRows([]entity.User{d.Actor})
		}
		return effectiveSQLRows([]entity.User{})
	case strings.Contains(q, "role_permissions AS permission"):
		for _, p := range []string{"models.read_all", "models.write"} {
			if slices.Contains(values, p) && !c.f.deny[p] {
				return effectiveSQLRows([]exactPermissionIdentity{{RoleID: "rol_admin", PermissionRoleID: "rol_admin", Permission: p}})
			}
		}
		return effectiveSQLRows([]exactPermissionIdentity{})
	case strings.Contains(q, `FROM "models"`):
		if slices.Contains(values, d.Model.ID) {
			return effectiveSQLRows([]entity.Model{d.Model})
		}
		return effectiveSQLRows([]entity.Model{})
	case strings.Contains(q, `FROM "model_provider_bindings"`):
		return effectiveSQLRows(d.Bindings)
	case strings.Contains(q, `FROM "provider_models"`):
		return effectiveSQLRows(d.Models)
	case strings.Contains(q, `FROM "provider_connections"`):
		return effectiveSQLRows(d.Connections)
	case strings.Contains(q, `FROM "providers"`):
		return effectiveSQLRows(d.Providers)
	case strings.Contains(q, `FROM "provider_credentials"`):
		return effectiveSQLRows(d.Credentials)
	case strings.Contains(q, `FROM "credential_model_accesses"`):
		return effectiveSQLRows(d.Access)
	case strings.Contains(q, `FROM "model_names"`):
		return effectiveSQLRows(d.Names)
	case strings.Contains(q, `FROM "egress_settings"`):
		return effectiveSQLRows([]entity.EgressSetting{{ID: 1}})
	case strings.Contains(q, `FROM "model_weight_rollback_commands"`):
		rows := []entity.ModelWeightRollbackCommand{}
		for _, v := range d.Commands {
			if slices.Contains(values, v.RequestID) {
				rows = append(rows, v)
			}
		}
		return effectiveSQLRows(rows)
	case strings.Contains(q, `FROM "model_weight_versions"`):
		rows := []entity.ModelWeightVersion{}
		specific := ""
		for _, v := range values {
			if strings.HasPrefix(v, "mwv_") {
				specific = v
			}
		}
		cutoff := uint64(0)
		if strings.Contains(q, "sequence <") {
			for _, arg := range args {
				if n, ok := arg.Value.(int64); ok && n > 0 {
					cutoff = uint64(n)
					break
				}
			}
		}
		for _, v := range d.Versions {
			if specific != "" && v.ID != specific || cutoff > 0 && v.Sequence >= cutoff {
				continue
			}
			rows = append(rows, v)
		}
		slices.SortFunc(rows, func(a, b entity.ModelWeightVersion) int {
			if a.Sequence > b.Sequence {
				return -1
			}
			if a.Sequence < b.Sequence {
				return 1
			}
			return 0
		})
		limit := rolesSQLLimit(args)
		if strings.Contains(q, "LIMIT") {
			rows = rows[:min(len(rows), limit)]
		}
		return effectiveSQLRows(rows)
	}
	return nil, fmt.Errorf("unexpected weight read: %s", q)
}
func weightSQLInsert[T any](q string, args []driver.NamedValue) (T, error) {
	var value T
	p, e := schema.Parse(&value, &sync.Map{}, schema.NamingStrategy{})
	if e != nil {
		return value, e
	}
	columns := strings.Split(strings.Split(strings.SplitN(q, "(", 2)[1], ")")[0], ",")
	if len(columns) != len(args) {
		return value, errors.New("unexpected insert cardinality")
	}
	for i, column := range columns {
		f := p.FieldsByDBName[strings.Trim(column, " \"`")]
		if f == nil {
			return value, errors.New("unknown inserted column")
		}
		if e := f.Set(context.Background(), reflect.ValueOf(&value).Elem(), args[i].Value); e != nil {
			return value, e
		}
	}
	return value, nil
}
func (c *weightSQLConnection) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.readOnly {
		return nil, errors.New("read-only projection attempted write")
	}
	c.f.writes = append(c.f.writes, q)
	d := c.current()
	switch {
	case strings.HasPrefix(q, `INSERT INTO "model_weight_versions"`):
		v, e := weightSQLInsert[entity.ModelWeightVersion](q, args)
		if e != nil {
			return nil, e
		}
		if _, ok := d.Versions[v.ID]; ok {
			return nil, gorm.ErrDuplicatedKey
		}
		d.Versions[v.ID] = v
	case strings.HasPrefix(q, `INSERT INTO "model_weight_rollback_commands"`):
		v, e := weightSQLInsert[entity.ModelWeightRollbackCommand](q, args)
		if e != nil {
			return nil, e
		}
		if _, ok := d.Commands[v.RequestID]; ok {
			return nil, gorm.ErrDuplicatedKey
		}
		d.Commands[v.RequestID] = v
	case strings.HasPrefix(q, `INSERT INTO "audit_events"`):
		if c.f.failAudit {
			return nil, errors.New("controlled audit rollback")
		}
		v, e := weightSQLInsert[entity.AuditEvent](q, args)
		if e != nil {
			return nil, e
		}
		d.Audits = append(d.Audits, v)
	case strings.HasPrefix(q, `UPDATE "model_provider_bindings"`):
		values := rolesSQLStrings(args)
		for i, b := range d.Bindings {
			if slices.Contains(values, b.ID) {
				weight := int(args[0].Value.(int64))
				if int(args[len(args)-1].Value.(int64)) != b.Weight {
					return driver.RowsAffected(0), nil
				}
				d.Bindings[i].Weight = weight
				return driver.RowsAffected(1), nil
			}
		}
		return driver.RowsAffected(0), nil
	case strings.HasPrefix(q, `UPDATE "models"`):
		d.Model.ConfigUpdatedAt = modelWeightBirth(args[0].Value.(time.Time))
	default:
		return nil, fmt.Errorf("unexpected weight write: %s", q)
	}
	return driver.RowsAffected(1), nil
}
func weightSQLService(t *testing.T) (*Service, *weightSQLFixture) {
	t.Helper()
	st := weightTestState()
	_, _, _ = weightTestRuntime(st)
	b := st.Rows[0]
	binding := entity.ModelProviderBinding{ID: b.BindingID, ModelID: st.Model.ID, ProviderModelID: b.ProviderModelID, Weight: 0, CreatedAt: *b.BindingCreatedAt}
	f := &weightSQLFixture{deny: map[string]bool{}, data: weightSQLData{Actor: entity.User{ID: "usr_weights", Role: entity.RoleAdmin, CreatedAt: st.Model.CreatedAt}, Model: st.Model, Bindings: []entity.ModelProviderBinding{binding}, Models: st.Models, Connections: st.Connections, Providers: st.Providers, Credentials: st.Credentials, Access: []entity.CredentialModelAccess{{CredentialID: st.Credentials[0].ID, ProviderModelID: b.ProviderModelID}}, Names: st.Names, Versions: map[string]entity.ModelWeightVersion{}, Commands: map[string]entity.ModelWeightRollbackCommand{}}}
	pool := sql.OpenDB(weightSQLConnector{f})
	t.Cleanup(func() { _ = pool.Close() })
	db, e := gorm.Open(postgres.New(postgres.Config{Conn: pool, PreferSimpleProtocol: true}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Discard})
	if e != nil {
		t.Fatal(e)
	}
	return &Service{db: db}, f
}
func weightSQLSeedVersion(t *testing.T, s *Service, f *weightSQLFixture) (string, string) {
	t.Helper()
	var first, second *string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		st, e := loadModelWeightState(tx, f.data.Model.ID, true, false)
		if e != nil {
			return e
		}
		rows := slices.Clone(st.Rows)
		rows[0].Weight = 100
		first, second, e = journalModelWeightChange(tx, f.data.Actor.ID, st, rows, "legacy_editor", nil, nil, time.Now().UTC().Truncate(time.Microsecond))
		if e != nil {
			return e
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f.data.Bindings[0].Weight = 100
	return *first, *second
}
func TestModelWeightJournalAtomicBaselineAndAuditFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			s, f := weightSQLService(t)
			f.failAudit = fail
			err := s.db.Transaction(func(tx *gorm.DB) error {
				st, e := loadModelWeightState(tx, f.data.Model.ID, true, false)
				if e != nil {
					return e
				}
				rows := slices.Clone(st.Rows)
				rows[0].Weight = 100
				before, after, e := journalModelWeightChange(tx, f.data.Actor.ID, st, rows, "legacy_editor", nil, nil, time.Now().UTC().Truncate(time.Microsecond))
				if e != nil {
					return e
				}
				return appendModelWeightAudit(tx, f.data.Actor.ID, st.Model.ID, "model.weights.update", modelWeightAudit{SourceVersionID: before, SavedVersionID: after, BeforeDigest: memberModelsDigest(st.Rows), AfterDigest: memberModelsDigest(rows), BindingCount: 1, Effect: "changed"})
			})
			if fail {
				if err == nil || len(f.data.Versions) != 0 || len(f.data.Audits) != 0 {
					t.Fatal("atomic journal escaped audit rollback", err)
				}
			} else {
				if err != nil || len(f.data.Versions) != 2 || len(f.data.Audits) != 1 {
					t.Fatal(err, len(f.data.Versions), len(f.data.Audits))
				}
				versions := []entity.ModelWeightVersion{}
				for _, v := range f.data.Versions {
					versions = append(versions, v)
				}
				slices.SortFunc(versions, func(a, b entity.ModelWeightVersion) int { return int(a.Sequence) - int(b.Sequence) })
				if versions[0].Source != "observed_baseline" || versions[0].ValidWeightSet || versions[1].Source != "legacy_editor" || !versions[1].ValidWeightSet || !versions[0].CapturedAt.Equal(versions[1].CapturedAt) || versions[1].ParentVersionID == nil || *versions[1].ParentVersionID != versions[0].ID {
					t.Fatal("fabricated history or lost exact baseline")
				}
			}
		})
	}
}
func TestModelWeightRollbackDurableReplayAndReadonlyRecovery(t *testing.T) {
	s, f := weightSQLService(t)
	_, target := weightSQLSeedVersion(t, s, f)
	// Current changed set is observed as all-zero; target remains complete100.
	f.data.Bindings[0].Weight = 0
	review, e := s.ReviewModelWeightRollback(context.Background(), f.data.Actor.ID, f.data.Model.ID, target)
	if e != nil || !review.Eligible {
		t.Fatal(review, e)
	}
	input := ModelWeightRollbackInput{target, "e26c3a0b-cfee-4391-bafd-7858fbf997ee", "Restore recorded weights"}
	result, e := s.RollbackModelWeights(context.Background(), f.data.Actor.ID, f.data.Model.ID, review.ReviewETag, input)
	if e != nil || result.Receipt.Effect != "changed" || result.RuntimeApplied || len(f.data.Commands) != 1 || f.data.Bindings[0].Weight != 100 {
		t.Fatal(result, e)
	}
	versionCount, auditCount, writeCount := len(f.data.Versions), len(f.data.Audits), len(f.writes)
	repeated, e := s.RollbackModelWeights(context.Background(), f.data.Actor.ID, f.data.Model.ID, review.ReviewETag, input)
	if e != nil || !reflect.DeepEqual(result.Receipt, repeated.Receipt) || len(f.data.Versions) != versionCount || len(f.data.Audits) != auditCount || len(f.writes) != writeCount {
		t.Fatal("replay repeated durable effects", e)
	}
	altered := input
	altered.Reason = "Different reason"
	if _, e := s.RollbackModelWeights(context.Background(), f.data.Actor.ID, f.data.Model.ID, review.ReviewETag, altered); e != modelWeightConflict {
		t.Fatal("altered UUID intent accepted", e)
	}
	f.deny["models.write"] = true
	recovered, e := s.GetModelWeightRollbackReceipt(context.Background(), f.data.Actor.ID, f.data.Model.ID, input.RequestID)
	if e != nil || !reflect.DeepEqual(recovered.Receipt, result.Receipt) || len(f.writes) != writeCount {
		t.Fatal("read recovery required write or mutated history", e)
	}
	if _, e := s.RollbackModelWeights(context.Background(), f.data.Actor.ID, f.data.Model.ID, review.ReviewETag, input); e == nil {
		t.Fatal("revoked write replay accepted")
	}
	f.deny["models.read_all"] = true
	if _, e := s.GetModelWeightRollbackReceipt(context.Background(), f.data.Actor.ID, f.data.Model.ID, input.RequestID); e == nil {
		t.Fatal("revoked read receipt disclosed")
	}
}
func TestModelWeightRollbackNoopSupersededAndReadOutage(t *testing.T) {
	s, f := weightSQLService(t)
	_, target := weightSQLSeedVersion(t, s, f)
	review, e := s.ReviewModelWeightRollback(context.Background(), f.data.Actor.ID, f.data.Model.ID, target)
	if e != nil {
		t.Fatal(e)
	}
	oldVersions := len(f.data.Versions)
	oldTime := f.data.Model.ConfigUpdatedAt
	input := ModelWeightRollbackInput{target, "e26c3a0b-cfee-4391-bafd-7858fbf997ee", "Confirm unchanged weights"}
	result, e := s.RollbackModelWeights(context.Background(), f.data.Actor.ID, f.data.Model.ID, review.ReviewETag, input)
	if e != nil || result.Receipt.Effect != "noop" || len(f.data.Versions) != oldVersions || !reflect.DeepEqual(oldTime, f.data.Model.ConfigUpdatedAt) {
		t.Fatal("noop manufactured version or time", result, e)
	}
	writes := len(f.writes)
	f.data.Bindings[0].Weight = 0
	recovered, e := s.GetModelWeightRollbackReceipt(context.Background(), f.data.Actor.ID, f.data.Model.ID, input.RequestID)
	if e != nil || recovered.ApplicationStatus != "superseded" || len(f.writes) != writes || f.data.Bindings[0].Weight != 0 {
		t.Fatal("superseded receipt reapplied weights", e)
	}
	f.failTable = `FROM "provider_credentials"`
	recovered, e = s.GetModelWeightRollbackReceipt(context.Background(), f.data.Actor.ID, f.data.Model.ID, input.RequestID)
	if e != nil || recovered.ApplicationStatus != "unknown" || !reflect.DeepEqual(result.Receipt, recovered.Receipt) {
		t.Fatal("eligibility outage erased historical receipt", e)
	}
}

func TestModelWeightRollbackAtomicAuditFailureAndConcurrentStaleIntents(t *testing.T) {
	t.Run("audit-failure", func(t *testing.T) {
		s, f := weightSQLService(t)
		_, target := weightSQLSeedVersion(t, s, f)
		f.data.Bindings[0].Weight = 0
		review, err := s.ReviewModelWeightRollback(context.Background(), f.data.Actor.ID, f.data.Model.ID, target)
		if err != nil {
			t.Fatal(err)
		}
		input := ModelWeightRollbackInput{target, "e26c3a0b-cfee-4391-bafd-7858fbf997ee", "Restore complete weights"}
		before := f.data.clone()
		f.failAudit = true
		if _, err := s.RollbackModelWeights(context.Background(), f.data.Actor.ID, f.data.Model.ID, review.ReviewETag, input); err == nil {
			t.Fatal("audit failure ignored")
		}
		if !reflect.DeepEqual(before, f.data) {
			t.Fatal("weights/version/receipt/timestamp escaped transaction rollback")
		}
		f.failAudit = false
		if _, err := s.RollbackModelWeights(context.Background(), f.data.Actor.ID, f.data.Model.ID, review.ReviewETag, input); err != nil {
			t.Fatal("unchanged reviewed intent could not be explicitly retried", err)
		}
	})
	t.Run("concurrent-stale-intents", func(t *testing.T) {
		s, f := weightSQLService(t)
		_, target := weightSQLSeedVersion(t, s, f)
		f.data.Bindings[0].Weight = 0
		review, err := s.ReviewModelWeightRollback(context.Background(), f.data.Actor.ID, f.data.Model.ID, target)
		if err != nil {
			t.Fatal(err)
		}
		actor, model := f.data.Actor.ID, f.data.Model.ID
		out := make(chan error, 2)
		var wg sync.WaitGroup
		for _, uuid := range []string{"e26c3a0b-cfee-4391-bafd-7858fbf997ee", "e26c3a0b-cfee-4391-bafd-7858fbf997ef"} {
			wg.Go(func() {
				_, err := s.RollbackModelWeights(context.Background(), actor, model, review.ReviewETag, ModelWeightRollbackInput{target, uuid, "Restore complete weights"})
				out <- err
			})
		}
		wg.Wait()
		close(out)
		success, conflict := 0, 0
		for err := range out {
			switch err {
			case nil:
				success++
			case modelWeightConflict:
				conflict++
			default:
				t.Fatal(err)
			}
		}
		if success != 1 || conflict != 1 || len(f.data.Commands) != 1 || len(f.data.Versions) != 4 || len(f.data.Audits) != 1 || f.data.Bindings[0].Weight != 100 {
			t.Fatal("concurrent stale review produced extra effects", success, conflict)
		}
	})
}
func TestModelWeightRollbackIndependentAuthorityAndPositiveEligibility(t *testing.T) {
	t.Run("legacy-writer-without-history-read", func(t *testing.T) {
		s, f := weightSQLService(t)
		f.deny["models.read_all"] = true
		err := s.db.Transaction(func(tx *gorm.DB) error {
			_, write, err := modelWeightAuthority(tx, f.data.Actor.ID, false, true)
			if !write {
				t.Fatal("write permission lost")
			}
			return err
		})
		if err != nil {
			t.Fatal("new history read permission changed legacy writer contract", err)
		}
	})
	for _, kind := range []string{"disabled-model", "disabled-provider-model", "disabled-connection", "source-unavailable", "coverage-unknown", "egress-unavailable"} {
		t.Run(kind, func(t *testing.T) {
			st := weightTestState()
			switch kind {
			case "disabled-model":
				st.Model.Status = "disabled"
			case "disabled-provider-model":
				st.Models[0].Disabled = true
			case "disabled-connection":
				st.Connections[0].Enabled = false
			case "source-unavailable":
				v := st.Supplies[st.Models[0].ID]
				v.SourceAvailable = false
				st.Supplies[st.Models[0].ID] = v
			case "coverage-unknown":
				v := st.Supplies[st.Models[0].ID]
				v.Covered = false
				st.Supplies[st.Models[0].ID] = v
			case "egress-unavailable":
				st.EgressReady[st.Connections[0].ID] = false
			}
			if st.executable(st.Rows) {
				t.Fatal("new rollback promoted unavailable positive route")
			}
		})
	}
}

func TestModelWeightRollbackCommitCancellationPreservesChangedOnlyFence(t *testing.T) {
	s, f := weightSQLService(t)
	_, target := weightSQLSeedVersion(t, s, f)
	f.data.Bindings[0].Weight = 0
	s.runtime = &gatewayRuntime{}
	review, err := s.ReviewModelWeightRollback(context.Background(), f.data.Actor.ID, f.data.Model.ID, target)
	if err != nil {
		t.Fatal(err)
	}
	input := ModelWeightRollbackInput{target, "e26c3a0b-cfee-4391-bafd-7858fbf997ee", "Restore complete weights"}
	dispatch := func(etag string, in ModelWeightRollbackInput) {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f.onCommit = cancel
		_, err := s.RollbackModelWeights(ctx, f.data.Actor.ID, f.data.Model.ID, etag, in)
		f.onCommit = nil
		if err == nil {
			t.Fatal("cancelled postcommit confirmation fabricated acknowledged success")
		}
	}
	dispatch(review.ReviewETag, input)
	if s.runtime.epoch.Load() != 1 || !runtimeDenied(&s.runtime.deniedModels, f.data.Model.ID) || len(f.data.Commands) != 1 || f.data.Bindings[0].Weight != 100 {
		t.Fatal("committed changed set lost its pre-publication revocation fence")
	}
	old := f.data.clone()
	writes := len(f.writes)
	dispatch(review.ReviewETag, input)
	if s.runtime.epoch.Load() != 1 || len(f.writes) != writes || !reflect.DeepEqual(old, f.data) {
		t.Fatal("receipt replay invalidated or repeated historical effects")
	}
	noopReview, err := s.ReviewModelWeightRollback(context.Background(), f.data.Actor.ID, f.data.Model.ID, target)
	if err != nil {
		t.Fatal(err)
	}
	noop := input
	noop.RequestID = "e26c3a0b-cfee-4391-bafd-7858fbf997ef"
	dispatch(noopReview.ReviewETag, noop)
	if s.runtime.epoch.Load() != 1 || len(f.data.Versions) != len(old.Versions) || len(f.data.Commands) != 2 || f.data.Commands[noop.RequestID].Effect != "noop" || !reflect.DeepEqual(old.Model.ConfigUpdatedAt, f.data.Model.ConfigUpdatedAt) {
		t.Fatal("noop manufactured invalidation/version/time")
	}
}

// A driver may enumerate identical rows differently between review and dispatch.
// Exercise the real denial hydration, not a pre-sorted fabricated state.
func TestModelWeightCredentialEnumerationReviewStability(t *testing.T) {
	s, f := weightSQLService(t)
	original := f.data.Credentials[0]
	rows := make([]entity.ProviderCredential, 0, 3)
	for _, id := range []string{"crd_a", "crd_Z", "crd_A"} {
		row := original
		row.ID = id
		rows = append(rows, row)
	}
	capture := func(input []entity.ProviderCredential) *ModelWeightRollbackReview {
		t.Helper()
		f.data.Credentials = slices.Clone(input)
		st := weightTestState()
		err := s.db.Transaction(func(tx *gorm.DB) error {
			return modelWeightSourceDenials(tx, st)
		}, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(f.data.Credentials, input) || len(f.writes) != 0 {
			t.Fatal("read-only hydration changed stored credential facts")
		}
		raw, digest, err := modelWeightSnapshot(st.Rows)
		if err != nil {
			t.Fatal(err)
		}
		target := entity.ModelWeightVersion{ID: "mwv_01m36yee4gkbns18pfcqqc75a3", ModelID: st.Model.ID, ModelBirth: modelWeightBirth(st.Model.CreatedAt), Snapshot: raw, SnapshotDigest: digest, ValidWeightSet: true}
		return modelWeightReview(f.data.Actor, true, st, target, st.Rows, nil)
	}
	baseline := capture(rows)
	t.Run("reordered-equivalent", func(t *testing.T) {
		for _, permutation := range [][]entity.ProviderCredential{
			{rows[2], rows[0], rows[1]}, {rows[1], rows[2], rows[0]},
		} {
			reviewed := capture(permutation)
			if reviewed.ReviewETag != baseline.ReviewETag || reviewed.Eligible != baseline.Eligible || !reflect.DeepEqual(reviewed.BlockerCodes, baseline.BlockerCodes) {
				t.Fatal("unchanged credential set enumeration changed captured review")
			}
		}
	})
	t.Run("source-fields-remain-bound", func(t *testing.T) {
		for _, field := range []string{"ciphertext", "birth", "enabled"} {
			t.Run(field, func(t *testing.T) {
				changed := slices.Clone(rows)
				switch field {
				case "ciphertext":
					changed[1].Ciphertext += "changed"
				case "birth":
					changed[1].CreatedAt = changed[1].CreatedAt.Add(time.Microsecond)
				case "enabled":
					changed[1].Enabled = !changed[1].Enabled
				}
				if capture(changed).ReviewETag == baseline.ReviewETag {
					t.Fatal("real credential source change did not invalidate review", field)
				}
			})
		}
	})
	for _, option := range f.options {
		if !option.ReadOnly {
			t.Fatal("enumeration read lost read-only transaction")
		}
	}
	for _, query := range f.queries {
		if !strings.Contains(query, `FROM "provider_credentials"`) {
			t.Fatal("unexpected additional lookup", query)
		}
	}
}
