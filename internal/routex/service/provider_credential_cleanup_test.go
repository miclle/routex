package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type orphanCleanupFixture struct {
	mu                    sync.Mutex
	rows                  map[string]entity.ProviderCredentialCleanup
	active                map[string]entity.ProviderCredentialCleanup
	instances             []entity.SystemInstance
	failObservation       bool
	auditResourceBound    int
	gets, destroys, posts int
	unknownDestroy        bool
	holdReadEntered       chan struct{}
	holdReadRelease       chan struct{}
}
type orphanCleanupConnector struct {
	base credentialStorageConnector
	f    *orphanCleanupFixture
}

func (c orphanCleanupConnector) Connect(ctx context.Context) (driver.Conn, error) {
	v, e := c.base.Connect(ctx)
	if e != nil {
		return nil, e
	}
	return &orphanCleanupConnection{v.(*credentialStorageConnection), c.f}, nil
}
func (c orphanCleanupConnector) Driver() driver.Driver { return orphanCleanupDriver(c) }

type orphanCleanupDriver orphanCleanupConnector

func (c orphanCleanupDriver) Open(string) (driver.Conn, error) {
	return orphanCleanupConnector(c).Connect(context.Background())
}

type orphanCleanupConnection struct {
	*credentialStorageConnection
	cleanup *orphanCleanupFixture
}

func (c *orphanCleanupConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *orphanCleanupConnection) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, e := c.credentialStorageConnection.BeginTx(ctx, opts)
	if e != nil {
		return nil, e
	}
	c.cleanup.mu.Lock()
	c.cleanup.active = cloneStorageMap(c.cleanup.rows)
	c.cleanup.mu.Unlock()
	return orphanCleanupTx{tx, c.cleanup}, nil
}

type orphanCleanupTx struct {
	driver.Tx
	f *orphanCleanupFixture
}

func (tx orphanCleanupTx) Commit() error {
	if e := tx.Tx.Commit(); e != nil {
		return e
	}
	tx.f.mu.Lock()
	defer tx.f.mu.Unlock()
	tx.f.rows = tx.f.active
	tx.f.active = nil
	return nil
}
func (tx orphanCleanupTx) Rollback() error {
	tx.f.mu.Lock()
	tx.f.active = nil
	tx.f.mu.Unlock()
	return tx.Tx.Rollback()
}
func (c *orphanCleanupConnection) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, `INSERT INTO "audit_events"`) && len(args) >= 6 {
		action, _ := args[3].Value.(string)
		resourceID, _ := args[5].Value.(string)
		if strings.HasPrefix(action, "credential.orphan.cleanup.") && len(resourceID) > c.cleanup.auditResourceBound {
			return nil, errors.New("controlled audit resource_id schema bound")
		}
	}
	return c.credentialStorageConnection.ExecContext(ctx, q, args)
}

func (c *orphanCleanupConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	values := rolesSQLStrings(args)
	if strings.Contains(q, `FROM "provider_credential_cleanups"`) {
		c.cleanup.mu.Lock()
		defer c.cleanup.mu.Unlock()
		rows := c.cleanup.rows
		if c.cleanup.active != nil {
			rows = c.cleanup.active
		}
		out := []entity.ProviderCredentialCleanup{}
		for _, r := range rows {
			for _, v := range values {
				if v == r.CreationRequestID || v == r.RequestID {
					out = append(out, r)
					break
				}
			}
		}
		return effectiveSQLRows(out)
	}
	if strings.Contains(q, `FROM "system_instances"`) {
		c.cleanup.mu.Lock()
		defer c.cleanup.mu.Unlock()
		return effectiveSQLRows(c.cleanup.instances)
	}
	if strings.Contains(q, "count(*)") && (strings.Contains(q, `"provider_credentials"`) || strings.Contains(q, `"credential_vault_references"`)) {
		c.f.mu.Lock()
		defer c.f.mu.Unlock()
		d := c.f.current()
		n := int64(0)
		if strings.Contains(q, `"provider_credentials"`) {
			for _, r := range d.credentials {
				for _, v := range values {
					if r.ID == v {
						n++
					}
				}
			}
		} else {
			for _, r := range d.refs {
				for _, v := range values {
					if r.CredentialID == v || r.ReferenceID == v {
						n++
						break
					}
				}
			}
		}
		return &adminOverviewRows{columns: []string{"count"}, values: [][]driver.Value{{n}}}, nil
	}
	return c.credentialStorageConnection.QueryContext(ctx, q, args)
}
func cleanupSQLService(t *testing.T) (*Service, *credentialStorageFixture, *orphanCleanupFixture, *rolesSQLFixture) {
	t.Helper()
	s, f, roles := credentialStorageSQLService(t)
	_, _, control := roleDefinitionSQLService(t)
	cleanup := &orphanCleanupFixture{rows: map[string]entity.ProviderCredentialCleanup{}, instances: []entity.SystemInstance{f.data.instance}}
	auditSchema, e := schema.Parse(&entity.AuditEvent{}, &sync.Map{}, schema.NamingStrategy{})
	if e != nil || auditSchema.LookUpField("ResourceID") == nil || auditSchema.LookUpField("ResourceID").Size != 30 {
		t.Fatal("retained audit schema bound changed", e)
	}
	cleanup.auditResourceBound = auditSchema.LookUpField("ResourceID").Size
	s.instanceNow = time.Now
	s.instance = &systemInstanceLease{id: f.data.instance.ID, token: f.data.instance.LeaseToken, startedAt: f.data.instance.StartedAt}
	pool := sql.OpenDB(orphanCleanupConnector{credentialStorageConnector{vaultCommandConnector{roleDefinitionSQLConnector{roles, control}, f.vault}, f}, cleanup})
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	db, e := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	create := s.db.Callback().Create().Get("gorm:create")
	update := s.db.Callback().Update().Get("gorm:update")
	if e = db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		v, ok := tx.Statement.Dest.(*entity.ProviderCredentialCleanup)
		if !ok {
			create(tx)
			return
		}
		cleanup.mu.Lock()
		defer cleanup.mu.Unlock()
		rows := cleanup.active
		if rows == nil {
			_ = tx.AddError(errors.New("claim outside transaction"))
			return
		}
		if _, ok := rows[v.CreationRequestID]; ok {
			_ = tx.AddError(gorm.ErrDuplicatedKey)
			return
		}
		rows[v.CreationRequestID] = *v
		tx.RowsAffected = 1
	}); e != nil {
		t.Fatal(e)
	}
	if e = db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
		if tx.Statement.Table != "provider_credential_cleanups" {
			update(tx)
			return
		}
		cleanup.mu.Lock()
		defer cleanup.mu.Unlock()
		if cleanup.failObservation {
			_ = tx.AddError(errors.New("controlled observation persistence failure"))
			return
		}
		x := tx.Statement.Dest.(map[string]any)
		for key, r := range cleanup.active {
			if r.State == "pending" {
				r.State = x["state"].(string)
				r.OwnershipJSON = x["ownership_json"].(string)
				r.CleanupJSON = x["cleanup_json"].(string)
				r.FinishedAt = x["finished_at"].(*time.Time)
				cleanup.active[key] = r
				tx.RowsAffected++
			}
		}
	}); e != nil {
		t.Fatal(e)
	}
	s.db = db
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanup.mu.Lock()
		defer cleanup.mu.Unlock()
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if cleanup.active != nil || f.active != nil {
			t.Error("database transaction spans remote effect")
		}
		switch r.Method {
		case "POST":
			cleanup.posts++
			var b struct {
				Data    map[string]string `json:"data"`
				Options struct {
					CAS int `json:"cas"`
				} `json:"options"`
			}
			if json.NewDecoder(r.Body).Decode(&b) != nil || b.Options.CAS != 0 || r.Header.Get("X-Vault-Token") != "writer-token" {
				t.Error("creation request changed")
			}
			f.values[r.URL.Path] = b.Data
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}})
			if f.afterWrite != nil {
				f.afterWrite()
			}
		case "GET":
			cleanup.gets++
			if cleanup.gets == 2 && cleanup.holdReadEntered != nil {
				entered, release := cleanup.holdReadEntered, cleanup.holdReadRelease
				f.mu.Unlock()
				cleanup.mu.Unlock()
				close(entered)
				<-release
				cleanup.mu.Lock()
				f.mu.Lock()
			}
			if r.Header.Get("X-Vault-Token") != "reader-token" || r.URL.RawQuery != "version=1" {
				t.Error("ownership reader/path changed")
			}
			value, ok := f.values[r.URL.Path]
			if !ok {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": value, "metadata": map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}}})
		case "PUT":
			cleanup.destroys++
			if r.Header.Get("X-Vault-Token") != "cleanup-only-token" || !strings.Contains(r.URL.Path, "/destroy/") {
				t.Error("writer fallback or wrong exact destroy")
			}
			var b struct {
				Versions []int `json:"versions"`
			}
			if json.NewDecoder(r.Body).Decode(&b) != nil || !reflect.DeepEqual(b.Versions, []int{1}) {
				t.Error("destroy version changed")
			}
			if len(cleanup.rows) != 1 {
				t.Error("effect before durable disposition")
			}
			delete(f.values, strings.Replace(r.URL.Path, "/destroy/", "/data/", 1))
			if cleanup.unknownDestroy {
				w.WriteHeader(503)
			} else {
				w.WriteHeader(204)
			}
		default:
			t.Error("extra remote operation")
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(stub.Close)
	f.vault.data.rev.Endpoint = stub.URL
	return s, f, cleanup, roles
}
func cleanupConfirmedOrphan(t *testing.T, s *Service, f *credentialStorageFixture) (credentialCreationIntent, entity.CredentialStorageOperation) {
	t.Helper()
	i := credentialStorageFixtureIntent(t, s, f, "credential")
	f.afterWrite = func() { f.data.policy.Generation = "changed_after_write" }
	result, e := s.createVaultCredential(context.Background(), "usr_admin", "11111111-1111-4111-8111-111111111111", "private-value", i)
	if !errors.Is(e, catalogConflict) || result == nil || f.data.ops[result.RequestID].State != "orphan" {
		t.Fatal("real owned orphan setup", e)
	}
	f.afterWrite = nil
	return i, f.data.ops[result.RequestID]
}
func TestProviderCleanupDurableOwnedOrphanAndTokenFreeReplay(t *testing.T) {
	s, f, c, roles := cleanupSQLService(t)
	i, op := cleanupConfirmedOrphan(t, s, f)
	before := op
	v, e := s.GetProviderCredentialOrphan(context.Background(), "usr_admin", op.IntegrationID, op.RequestID)
	if e != nil || !v.Eligible || !v.OwnershipRecorded {
		t.Fatal("owned orphan preview", e, v)
	}
	input := ProviderCredentialCleanupInput{RequestID: "22222222-2222-4222-8222-222222222222", Reason: "Reviewed exact orphan", cleanupToken: "cleanup-only-token"}
	result, e := s.CleanupProviderCredentialOrphan(context.Background(), "usr_admin", op.IntegrationID, op.RequestID, v.ReviewETag, input)
	if e != nil || result.Receipt.State != "acknowledged" || result.Running || c.destroys != 1 || c.gets != 2 || c.posts != 1 {
		t.Fatal("one claimed ownership/destroy", e, result, c.destroys, c.gets)
	}
	audits := []entity.AuditEvent{}
	for _, event := range roles.data.audits {
		if strings.HasPrefix(event.Action, "credential.orphan.cleanup.") {
			audits = append(audits, event)
		}
	}
	if len(audits) != 2 {
		t.Fatal("missing durable claim/observation audit")
	}
	for index, event := range audits {
		action := []string{"credential.orphan.cleanup.claim", "credential.orphan.cleanup.observe"}[index]
		if event.Action != action || event.ActorID != "usr_admin" || event.ResourceType != "credential" || event.ResourceID != op.CredentialID || event.DetailsJSON == nil {
			t.Fatal("cleanup audit lost exact actor/credential correlation")
		}
		var details map[string]string
		if err := json.Unmarshal([]byte(*event.DetailsJSON), &details); err != nil || !reflect.DeepEqual(details, map[string]string{"creation_request_id": op.RequestID, "request_id": input.RequestID}) {
			t.Fatal("cleanup audit lost exact original creation/command correlation")
		}
	}
	input.cleanupToken = ""
	replay, e := s.CleanupProviderCredentialOrphan(context.Background(), "usr_admin", op.IntegrationID, op.RequestID, v.ReviewETag, input)
	if e != nil || !reflect.DeepEqual(result, replay) || c.destroys != 1 || c.gets != 2 {
		t.Fatal("receipt retry replayed HTTP", e)
	}
	if !reflect.DeepEqual(before, f.data.ops[op.RequestID]) {
		t.Fatal("original write/read or plan rewritten")
	}
	_, e = s.createVaultCredential(context.Background(), "usr_admin", op.RequestID, "private-value", i)
	if !errors.Is(e, catalogConflict) || c.gets != 2 || c.posts != 1 {
		t.Fatal("cleanup disposition resurrected business recovery", e)
	}
	for _, change := range []func(*ProviderCredentialCleanupInput){func(x *ProviderCredentialCleanupInput) { x.RequestID = "33333333-3333-4333-8333-333333333333" }, func(x *ProviderCredentialCleanupInput) { x.Reason = "different" }} {
		x := input
		change(&x)
		if _, e = s.CleanupProviderCredentialOrphan(context.Background(), "usr_admin", op.IntegrationID, op.RequestID, v.ReviewETag, x); !errors.Is(e, catalogConflict) {
			t.Fatal("changed command reconciled", e)
		}
	}
}
func TestProviderCleanupUnknownNeverReplaysAndPersistenceLossStaysUncertain(t *testing.T) {
	for _, persistFail := range []bool{false, true} {
		t.Run(fmt.Sprint(persistFail), func(t *testing.T) {
			s, f, c, _ := cleanupSQLService(t)
			_, op := cleanupConfirmedOrphan(t, s, f)
			v, e := s.GetProviderCredentialOrphan(context.Background(), "usr_admin", op.IntegrationID, op.RequestID)
			if e != nil {
				t.Fatal(e)
			}
			c.unknownDestroy = true
			c.failObservation = persistFail
			input := ProviderCredentialCleanupInput{RequestID: "22222222-2222-4222-8222-222222222222", Reason: "Confirm controlled orphan", cleanupToken: "cleanup-only-token"}
			result, e := s.CleanupProviderCredentialOrphan(context.Background(), "usr_admin", op.IntegrationID, op.RequestID, v.ReviewETag, input)
			if persistFail {
				if !errors.Is(e, vaultUnavailable) {
					t.Fatal("lost persistence became success", e)
				}
				c.failObservation = false
				s.rootNow = func() time.Time { return time.Now().Add(time.Minute) }
			} else if e != nil || result.Receipt.State != "unknown" {
				t.Fatal("ambiguous destroy changed to success", e)
			}
			input.cleanupToken = ""
			result, e = s.CleanupProviderCredentialOrphan(context.Background(), "usr_admin", op.IntegrationID, op.RequestID, v.ReviewETag, input)
			if e != nil || result.Receipt.State != "unknown" || c.gets != 2 || c.destroys != 1 {
				t.Fatal("unknown replay inferred404 or repeated destroy", e, result)
			}
		})
	}
}
func TestProviderCleanupHoldersRejectBothDirectionsAndReleaseOnce(t *testing.T) {
	s := &Service{}
	release, e := s.credentialCreationLease("one")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.credentialCleanupLease("one"); !errors.Is(e, catalogConflict) {
		t.Fatal("held recovery admitted cleanup")
	}
	release()
	release()
	exclusive, e := s.credentialCleanupLease("one")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.credentialCreationLease("one"); !errors.Is(e, catalogConflict) {
		t.Fatal("cleanup admitted recovery")
	}
	exclusive()
	exclusive()
	r, e := s.credentialCreationLease("one")
	if e != nil {
		t.Fatal(e)
	}
	r()
}
func TestProviderCleanupOriginalOwnershipAndEligibilityBlockers(t *testing.T) {
	for _, name := range []string{"unknown", "awaiting_read", "owned", "committed", "held", "fleet", "reference", "live", "current_reader_removed"} {
		t.Run(name, func(t *testing.T) {
			s, f, c, _ := cleanupSQLService(t)
			_, op := cleanupConfirmedOrphan(t, s, f)
			switch name {
			case "held":
				release, e := s.credentialCreationLease(op.RequestID)
				if e != nil {
					t.Fatal(e)
				}
				defer release()
			case "fleet":
				c.instances = append(c.instances, entity.SystemInstance{ID: "ins_other", LeaseToken: "other", Role: "combined", LeaseExpiresAt: time.Now().Add(time.Hour)})
			case "reference":
				f.data.refs[op.CredentialID] = operationReference(op)
			case "live":
				f.data.credentials[op.CredentialID] = entity.ProviderCredential{ID: op.CredentialID}
			case "current_reader_removed":
				f.vault.data.reader.AuthCiphertext = ""
			default:
				op.State = name
				f.data.ops[op.RequestID] = op
			}
			v, e := s.GetProviderCredentialOrphan(context.Background(), "usr_admin", op.IntegrationID, op.RequestID)
			if e != nil || v.Eligible || len(v.BlockerCodes) == 0 {
				t.Fatal("blocked target became eligible", e, v)
			}
			input := ProviderCredentialCleanupInput{RequestID: "22222222-2222-4222-8222-222222222222", Reason: "No unsafe cleanup", cleanupToken: "cleanup-only-token"}
			if _, e = s.CleanupProviderCredentialOrphan(context.Background(), "usr_admin", op.IntegrationID, op.RequestID, v.ReviewETag, input); !errors.Is(e, catalogConflict) {
				t.Fatal("blocked cleanup dispatched", e)
			}
			if c.destroys != 0 || c.gets != 1 {
				t.Fatal("blocked preview/confirmation performed HTTP")
			}
		})
	}
}
func TestProviderCleanupIntentDecoderAndSecretExclusion(t *testing.T) {
	good := `{"request_id":"22222222-2222-4222-8222-222222222222","reason":"Explicit confirmation","cleanup_token":"private-only-token"}`
	var input ProviderCredentialCleanupInput
	if e := json.Unmarshal([]byte(good), &input); e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(input)
	if e != nil || strings.Contains(string(raw), "private-only-token") || strings.Contains(fmt.Sprintf("%+v %#v", input, input), "private-only-token") {
		t.Fatal("input exposed cleanup authentication")
	}
	for _, raw := range []string{strings.Replace(good, "Explicit confirmation", " padded ", 1), strings.Replace(good, "private-only-token", "with space", 1), strings.Replace(good, `"reason":`, `"unexpected":1,"reason":`, 1), strings.Replace(good, `"reason":`, `"reason":"duplicate","reason":`, 1), strings.Replace(good, `"private-only-token"`, `null`, 1)} {
		if json.Unmarshal([]byte(raw), &input) == nil {
			t.Fatal("malformed intent accepted")
		}
	}
	now := time.Now().UTC()
	row := entity.ProviderCredentialCleanup{CreationRequestID: "11111111-1111-4111-8111-111111111111", RequestID: "22222222-2222-4222-8222-222222222222", IntegrationID: "vlt_01aaaaaaaaaaaaaaaaaaaaaaaa", RevisionID: "vlr_01aaaaaaaaaaaaaaaaaaaaaaaa", State: "acknowledged", StartedAt: now, Deadline: now.Add(time.Second), FinishedAt: &now, OwnershipJSON: vaultJSON(vaultEmptyObservation()), CleanupJSON: vaultJSON(VaultCleanupView{"acknowledged", vaultEmptyObservation()})}
	if _, e := cleanupReceipt(row, now); e == nil {
		t.Fatal("ack without actual ownership/destroy accepted")
	}
	private, _ := json.Marshal(entity.ProviderCredentialCleanup{Reason: "private-reason", OperationProof: "private-proof"})
	if string(private) != "{}" {
		t.Fatal("private ledger default DTO leaked")
	}
}

func TestProviderCleanupHeldRemoteRecoveryCannotBeFakedAbsentByExpiry(t *testing.T) {
	s, f, c, _ := cleanupSQLService(t)
	i, op := cleanupConfirmedOrphan(t, s, f)
	c.holdReadEntered = make(chan struct{})
	c.holdReadRelease = make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, e := s.createVaultCredential(context.Background(), "usr_admin", op.RequestID, "private-value", i)
		done <- e
	}()
	select {
	case <-c.holdReadEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("recovery did not reach held actual ownership GET")
	}
	defer func() {
		select {
		case <-c.holdReadRelease:
		default:
			close(c.holdReadRelease)
		}
	}()
	f.mu.Lock()
	live := f.data.ops[op.RequestID]
	live.ClaimedUntil = time.Now().Add(-time.Hour)
	f.data.ops[op.RequestID] = live
	f.mu.Unlock()
	v, e := s.GetProviderCredentialOrphan(context.Background(), "usr_admin", op.IntegrationID, op.RequestID)
	if e != nil || v.Eligible || !slices.Contains(v.BlockerCodes, "active_creation") {
		t.Fatal("expired durable claim hid actual held reader", e)
	}
	input := ProviderCredentialCleanupInput{RequestID: "22222222-2222-4222-8222-222222222222", Reason: "Must not destroy held recovery", cleanupToken: "cleanup-only-token"}
	if _, e = s.CleanupProviderCredentialOrphan(context.Background(), "usr_admin", op.IntegrationID, op.RequestID, v.ReviewETag, input); !errors.Is(e, catalogConflict) {
		t.Fatal("held recovery admitted destructive command", e)
	}
	close(c.holdReadRelease)
	if e = <-done; e != nil {
		t.Fatal("legitimate recovery failed", e)
	}
	if c.destroys != 0 {
		t.Fatal("held recovery was destroyed")
	}
}
func TestProviderCleanupLegacyExposureAndRetiredInstancesNeverBecomeProof(t *testing.T) {
	for _, name := range []string{"missing_use", "exposed", "different_process", "expired_self", "retired_peer", "stopped_peer"} {
		t.Run(name, func(t *testing.T) {
			s, f, c, _ := cleanupSQLService(t)
			_, op := cleanupConfirmedOrphan(t, s, f)
			now := time.Now()
			switch name {
			case "missing_use":
				delete(f.data.uses, op.RequestID)
			case "exposed":
				u := f.data.uses[op.RequestID]
				u.Exposed = true
				f.data.uses[op.RequestID] = u
			case "different_process":
				s.instance = &systemInstanceLease{id: "ins_other", token: "other"}
			case "expired_self":
				c.instances[0].LeaseExpiresAt = now.Add(-time.Hour)
			case "retired_peer":
				c.instances = append(c.instances, entity.SystemInstance{ID: "ins_old", RetiredAt: &now, LeaseExpiresAt: now.Add(-time.Hour)})
			case "stopped_peer":
				c.instances = append(c.instances, entity.SystemInstance{ID: "ins_old", StoppedAt: &now, LeaseExpiresAt: now.Add(-time.Hour)})
			}
			v, e := s.GetProviderCredentialOrphan(context.Background(), "usr_admin", op.IntegrationID, op.RequestID)
			if e != nil || v.Eligible {
				t.Fatal("history/expired lease manufactured local ownership", e, v)
			}
		})
	}
	s, f, _, _ := cleanupSQLService(t)
	_, op := cleanupConfirmedOrphan(t, s, f)
	s.instance = &systemInstanceLease{id: "ins_other", token: "other"}
	if e := s.db.Transaction(func(tx *gorm.DB) error { return s.recordCredentialCreationUse(tx, op.RequestID, false) }); e != nil {
		t.Fatal(e)
	}
	s.instance = &systemInstanceLease{id: f.data.instance.ID, token: f.data.instance.LeaseToken, startedAt: f.data.instance.StartedAt}
	owned, e := s.cleanupProcessOwned(s.db, op.RequestID)
	if e != nil || owned || !f.data.uses[op.RequestID].Exposed {
		t.Fatal("foreign recovery exposure was not permanent", e)
	}
}
func TestProviderCleanupDispositionAlsoFencesCommitAndFreshAuthority(t *testing.T) {
	s, f, c, roles := cleanupSQLService(t)
	i, op := cleanupConfirmedOrphan(t, s, f)
	v, e := s.GetProviderCredentialOrphan(context.Background(), "usr_admin", op.IntegrationID, op.RequestID)
	if e != nil {
		t.Fatal(e)
	}
	input := ProviderCredentialCleanupInput{RequestID: "22222222-2222-4222-8222-222222222222", Reason: "Exact approved orphan", cleanupToken: "cleanup-only-token"}
	if _, e = s.CleanupProviderCredentialOrphan(context.Background(), "usr_admin", op.IntegrationID, op.RequestID, v.ReviewETag, input); e != nil {
		t.Fatal(e)
	}
	if e = s.commitVaultCredential(context.Background(), "usr_admin", i, &op, op.Claim, false, s.secretEpoch()); !errors.Is(e, catalogConflict) || len(f.data.credentials) != 0 {
		t.Fatal("stale prepared commit restored destroyed business object", e)
	}
	old := roles.data.users["usr_admin"]
	disabled := old
	disabled.Disabled = true
	roles.data.users["usr_admin"] = disabled
	input.cleanupToken = ""
	if _, e = s.CleanupProviderCredentialOrphan(context.Background(), "usr_admin", op.IntegrationID, op.RequestID, v.ReviewETag, input); e == nil {
		t.Fatal("receipt retry bypassed current disabled actor")
	}
	roles.data.users["usr_admin"] = old
	if c.destroys != 1 || c.gets != 2 {
		t.Fatal("denied retry had remote effects")
	}
}

func TestProviderCleanupIndependentPermissionsAndImmutableReceiptShape(t *testing.T) {
	s, f, c, roles := cleanupSQLService(t)
	_, op := cleanupConfirmedOrphan(t, s, f)
	for _, permission := range []string{"secrets.write", "providers.write"} {
		roles.deny[permission] = true
		v, e := s.GetProviderCredentialOrphan(context.Background(), "usr_admin", op.IntegrationID, op.RequestID)
		if e != nil || !v.Eligible || v.CanCleanup {
			t.Fatal("read eligibility conflated with independent write authority", permission, e)
		}
		in := ProviderCredentialCleanupInput{RequestID: "22222222-2222-4222-8222-222222222222", Reason: "Exact review", cleanupToken: "cleanup-only-token"}
		if _, e = s.CleanupProviderCredentialOrphan(context.Background(), "usr_admin", op.IntegrationID, op.RequestID, v.ReviewETag, in); e == nil {
			t.Fatal("missing independent write authority dispatched", permission)
		}
		roles.deny[permission] = false
	}
	roles.deny["secrets.read"] = true
	if _, e := s.GetProviderCredentialOrphan(context.Background(), "usr_admin", op.IntegrationID, op.RequestID); e == nil {
		t.Fatal("preview bypassed read permission")
	}
	roles.deny["secrets.read"] = false
	if c.gets != 1 || c.destroys != 0 {
		t.Fatal("authorization check made remote effects")
	}
	now := s.secretNow()
	row := entity.ProviderCredentialCleanup{CreationRequestID: op.RequestID, RequestID: "22222222-2222-4222-8222-222222222222", IntegrationID: op.IntegrationID, RevisionID: op.RevisionID, State: "pending", StartedAt: now, Deadline: now.Add(time.Minute), OwnershipJSON: vaultJSON(vaultEmptyObservation()), CleanupJSON: vaultJSON(VaultCleanupView{"not_attempted", vaultEmptyObservation()})}
	if _, e := cleanupReceipt(row, now); e != nil {
		t.Fatal(e)
	}
	row.FinishedAt = &now
	if _, e := cleanupReceipt(row, now); e == nil {
		t.Fatal("finished running command accepted")
	}
	row.State = "failed"
	row.FinishedAt = nil
	if _, e := cleanupReceipt(row, now); e == nil {
		t.Fatal("unfinished settled command accepted")
	}
	proof := cleanupOperationProof(op)
	changed := op
	changed.ActorID = "usr_other"
	if cleanupOperationProof(changed) == proof {
		t.Fatal("actor identity not bound")
	}
	changed = op
	changed.ReaderGeneration = "other_generation"
	if cleanupOperationProof(changed) == proof {
		t.Fatal("retained reader generation not bound")
	}
}
