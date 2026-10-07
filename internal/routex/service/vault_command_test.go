package service

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/logger"
)

// Reuse the existing exact actor/admission SQL fixture. Only owned Vault rows
// are modeled here; no authority or client stage is bypassed. Transaction state
// rolls back, and the HTTP stub checks durable claim plus released DB locks.
type vaultCommandData struct {
	row      entity.VaultIntegration
	rev      entity.VaultRevision
	writer   entity.VaultWriterAuth
	reader   entity.VaultReaderAuth
	probes   map[string]entity.VaultProbe
	commands map[string]entity.VaultProbeCommand
	receipts map[string]entity.VaultConfigReceipt
}

func (d vaultCommandData) clone() vaultCommandData {
	out := d
	out.probes = map[string]entity.VaultProbe{}
	out.commands = map[string]entity.VaultProbeCommand{}
	out.receipts = map[string]entity.VaultConfigReceipt{}
	for k, v := range d.probes {
		out.probes[k] = v
	}
	for k, v := range d.commands {
		out.commands[k] = v
	}
	for k, v := range d.receipts {
		out.receipts[k] = v
	}
	return out
}

type vaultCommandFixture struct {
	mu          sync.Mutex
	data        vaultCommandData
	active      *vaultCommandData
	failResult  bool
	calls       int
	marker      string
	requests    []string
	logins      []string
	loginDenied bool
}

func (f *vaultCommandFixture) current() *vaultCommandData {
	if f.active != nil {
		return f.active
	}
	return &f.data
}

type vaultCommandConnector struct {
	base    roleDefinitionSQLConnector
	fixture *vaultCommandFixture
}

func (c vaultCommandConnector) Connect(ctx context.Context) (driver.Conn, error) {
	b, e := c.base.Connect(ctx)
	if e != nil {
		return nil, e
	}
	return &vaultCommandConnection{roleDefinitionSQLConnection: b.(*roleDefinitionSQLConnection), f: c.fixture}, nil
}
func (c vaultCommandConnector) Driver() driver.Driver { return vaultCommandDriver(c) }

type vaultCommandDriver vaultCommandConnector

func (c vaultCommandDriver) Open(string) (driver.Conn, error) {
	return vaultCommandConnector(c).Connect(context.Background())
}

type vaultCommandConnection struct {
	*roleDefinitionSQLConnection
	f *vaultCommandFixture
}

func (c *vaultCommandConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *vaultCommandConnection) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, e := c.roleDefinitionSQLConnection.BeginTx(ctx, opts)
	if e != nil {
		return nil, e
	}
	c.f.mu.Lock()
	d := c.f.data.clone()
	c.f.active = &d
	c.f.mu.Unlock()
	return vaultCommandTransaction{tx, c.f}, nil
}

type vaultCommandTransaction struct {
	driver.Tx
	f *vaultCommandFixture
}

func (t vaultCommandTransaction) Commit() error {
	if e := t.Tx.Commit(); e != nil {
		return e
	}
	t.f.mu.Lock()
	t.f.data = *t.f.active
	t.f.active = nil
	t.f.mu.Unlock()
	return nil
}
func (t vaultCommandTransaction) Rollback() error {
	t.f.mu.Lock()
	t.f.active = nil
	t.f.mu.Unlock()
	return t.Tx.Rollback()
}
func (c *vaultCommandConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.f.mu.Lock()
	d := c.f.current()
	values := rolesSQLStrings(args)
	switch {
	case strings.Contains(q, `FROM "vault_integrations"`):
		rows := []entity.VaultIntegration{}
		if len(values) == 0 || values[0] == d.row.ID {
			rows = append(rows, d.row)
		}
		c.f.mu.Unlock()
		return effectiveSQLRows(rows)
	case strings.Contains(q, `FROM "vault_revisions"`):
		rows := []entity.VaultRevision{}
		if len(values) > 0 && values[0] == d.rev.ID {
			rows = append(rows, d.rev)
		}
		c.f.mu.Unlock()
		return effectiveSQLRows(rows)
	case strings.Contains(q, `FROM "vault_writer_auth"`):
		v := d.writer
		c.f.mu.Unlock()
		return effectiveSQLRows([]entity.VaultWriterAuth{v})
	case strings.Contains(q, `FROM "vault_reader_auth"`):
		v := d.reader
		c.f.mu.Unlock()
		return effectiveSQLRows([]entity.VaultReaderAuth{v})
	case strings.Contains(q, `FROM "vault_probes"`):
		rows := []entity.VaultProbe{}
		for _, p := range d.probes {
			if len(values) > 0 && values[0] == p.ID {
				rows = append(rows, p)
			}
		}
		c.f.mu.Unlock()
		return effectiveSQLRows(rows)
	case strings.Contains(q, `FROM "vault_probe_commands"`):
		rows := []entity.VaultProbeCommand{}
		for _, v := range d.commands {
			if len(values) > 0 && (values[0] == v.RequestID || values[0] == v.ProbeID && v.FinishedAt == nil && vaultNow().Before(v.ExpiresAt)) {
				rows = append(rows, v)
			}
		}
		c.f.mu.Unlock()
		return effectiveSQLRows(rows)
	case strings.Contains(q, `FROM "vault_config_receipts"`):
		rows := []entity.VaultConfigReceipt{}
		for _, v := range d.receipts {
			if len(values) > 0 && values[0] == v.RequestID {
				rows = append(rows, v)
			}
		}
		c.f.mu.Unlock()
		return effectiveSQLRows(rows)
	case strings.Contains(q, `FROM "secret_write_policies"`):
		c.f.mu.Unlock()
		return effectiveSQLRows([]entity.SecretWritePolicy{{ID: 1}})
	case strings.Contains(q, `FROM "vault_catalogues"`):
		c.f.mu.Unlock()
		return effectiveSQLRows([]entity.VaultCatalogue{{ID: 1, Generation: strings.Repeat("a", 64)}})
	}
	c.f.mu.Unlock()
	return c.roleDefinitionSQLConnection.QueryContext(ctx, q, args)
}
func vaultCommandService(t *testing.T) (*Service, *vaultCommandFixture, *rolesSQLFixture) {
	t.Helper()
	_, roles, control := roleDefinitionSQLService(t)
	f := &vaultCommandFixture{data: vaultCommandData{probes: map[string]entity.VaultProbe{}, commands: map[string]entity.VaultProbeCommand{}, receipts: map[string]entity.VaultConfigReceipt{}}}
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls++
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		if f.active != nil {
			t.Error("DB lock spans Vault request")
		}
		claimed := false
		for _, c := range f.data.commands {
			if c.FinishedAt == nil {
				if _, ok := f.data.probes[c.ProbeID]; ok {
					claimed = true
				}
			}
		}
		if !claimed {
			t.Error("remote effect before durable plan/claim")
		}
		if r.URL.Path == "/v1/auth/custom/approle/login" {
			if r.Method != http.MethodPost || r.Header.Get("X-Vault-Token") != "" {
				t.Error("login inherited KV identity or method")
			}
			var auth struct {
				RoleID   string `json:"role_id"`
				SecretID string `json:"secret_id"`
			}
			if json.NewDecoder(r.Body).Decode(&auth) != nil || (auth.RoleID != "writer-role" && auth.RoleID != "reader-role") || auth.SecretID != auth.RoleID+"-reusable" {
				t.Error("wrong retained AppRole tuple")
			}
			f.logins = append(f.logins, auth.RoleID)
			if f.loginDenied {
				w.WriteHeader(403)
				return
			}
			token := "reader-token"
			if auth.RoleID == "writer-role" {
				token = "writer-token"
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"auth": map[string]any{"client_token": token, "lease_duration": 60}})
			return
		}
		switch r.Method {
		case http.MethodPost:
			var v struct {
				Data    map[string]string `json:"data"`
				Options struct {
					CAS int `json:"cas"`
				} `json:"options"`
			}
			if json.NewDecoder(r.Body).Decode(&v) != nil || v.Options.CAS != 0 {
				t.Error("not CAS0")
			}
			f.marker = v.Data["value"]
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"version":1,"destroyed":false,"deletion_time":""}}`))
		case http.MethodGet:
			if r.URL.Query().Get("version") != "1" {
				t.Error("wrong version")
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": map[string]string{"value": f.marker}, "metadata": map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}}})
		case http.MethodPut:
			var v struct {
				Versions []int `json:"versions"`
			}
			if json.NewDecoder(r.Body).Decode(&v) != nil || !reflect.DeepEqual(v.Versions, []int{1}) {
				t.Error("unsafe destroy")
			}
			w.WriteHeader(204)
		default:
			t.Error("unexpected remote method")
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(stub.Close)
	now := vaultNow()
	id := "vlt_01aaaaaaaaaaaaaaaaaaaaaaaa"
	revID := "vlr_01aaaaaaaaaaaaaaaaaaaaaaaa"
	f.data.row = entity.VaultIntegration{ID: id, Name: "Saved Vault", RevisionID: revID, CreatedAt: now, UpdatedAt: now}
	f.data.rev = entity.VaultRevision{ID: revID, IntegrationID: id, IntegrationBirth: now, Name: "Saved Vault", Endpoint: stub.URL, Mount: "kv", Prefix: "routex-probes", DataField: "value", CreatedAt: now}
	store, e := secretstore.NewKeyring(map[string][]byte{"root": bytes.Repeat([]byte{3}, 32)}, "", "root")
	if e != nil {
		t.Fatal(e)
	}
	f.data.writer = entity.VaultWriterAuth{ID: revID, SecretGeneration: "vag_writer", Method: "token"}
	f.data.reader = entity.VaultReaderAuth{ID: revID, SecretGeneration: "vag_reader", Method: "token"}
	f.data.writer.AuthCiphertext, e = store.Seal(rootReference("vault_writer_auth", revID, "vag_writer"), "writer-token")
	if e != nil {
		t.Fatal(e)
	}
	f.data.reader.AuthCiphertext, e = store.Seal(rootReference("vault_reader_auth", revID, "vag_reader"), "reader-token")
	if e != nil {
		t.Fatal(e)
	}
	pool := sql.OpenDB(vaultCommandConnector{roleDefinitionSQLConnector{roles, control}, f})
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	db, e := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	create := callbacks.Create(&callbacks.Config{})
	if e = db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		f.mu.Lock()
		d := f.current()
		owned := true
		switch v := tx.Statement.Dest.(type) {
		case *entity.VaultProbe:
			d.probes[v.ID] = *v
		case *entity.VaultProbeCommand:
			d.commands[v.RequestID] = *v
		case *entity.VaultConfigReceipt:
			d.receipts[v.RequestID] = *v
		case *entity.VaultIntegration:
			d.row = *v
		case *entity.VaultRevision:
			d.rev = *v
		case *entity.VaultWriterAuth:
			d.writer = *v
		case *entity.VaultReaderAuth:
			d.reader = *v
		default:
			owned = false
		}
		f.mu.Unlock()
		if owned {
			tx.RowsAffected = 1
		} else {
			create(tx)
		}
	}); e != nil {
		t.Fatal(e)
	}
	update := callbacks.Update(&callbacks.Config{})
	if e = db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
		f.mu.Lock()
		d := f.current()
		owned := true
		switch v := tx.Statement.Dest.(type) {
		case entity.VaultIntegration:
			d.row = v
		case *entity.VaultProbe:
			if f.failResult && v.FinishedAt != nil {
				_ = tx.AddError(errors.New("controlled result persistence failure"))
			} else {
				d.probes[v.ID] = *v
			}
		case *entity.VaultProbeCommand:
			d.commands[v.RequestID] = *v
		case map[string]any:
			switch tx.Statement.Table {
			case "vault_integrations":
				if v["active_probe_id"] == nil {
					d.row.ActiveProbeID = nil
				} else {
					x := v["active_probe_id"].(string)
					d.row.ActiveProbeID = &x
				}
			case "vault_catalogues":
				// Catalogue generation is not inspected by these update tests.
			default:
				owned = false
			}
		default:
			owned = false
		}
		f.mu.Unlock()
		if owned {
			tx.RowsAffected = 1
		} else {
			update(tx)
		}
	}); e != nil {
		t.Fatal(e)
	}
	return &Service{db: db, secrets: store, allowPrivateUpstream: true}, f, roles
}
func TestVaultCommandsClaimBeforeEffectAndNeverReplay(t *testing.T) {
	for _, mode := range []string{"complete", "audit_failure", "result_failure", "test_denied"} {
		t.Run(mode, func(t *testing.T) {
			s, f, roles := vaultCommandService(t)
			actor := roles.data.users["usr_admin"]
			etag := vaultReview(actor, f.data.row, f.data.rev)
			input := VaultStageInput{"11111111-1111-4111-8111-111111111111", "Reviewed"}
			if mode == "audit_failure" {
				roles.failAudit = true
			}
			if mode == "result_failure" {
				f.failResult = true
			}
			if mode == "test_denied" {
				roles.deny["secrets.test"] = true
			}
			v, _, e := s.RunVaultProbe(context.Background(), actor.ID, f.data.row.ID, "", "write", etag, input)
			switch mode {
			case "audit_failure", "test_denied":
				if e == nil || f.calls != 0 || len(f.data.probes) != 0 || len(f.data.commands) != 0 {
					t.Fatal("effect escaped rejected/rolled-back claim", e, f.calls)
				}
				return
			case "result_failure":
				if e == nil || f.calls != 1 || len(f.data.commands) != 1 {
					t.Fatal("result failure did not retain unknown claimed operation", e, f.calls)
				}
				f.failResult = false
				v, running, e := s.RunVaultProbe(context.Background(), actor.ID, f.data.row.ID, "", "write", etag, input)
				if e != nil || v == nil || !running || f.calls != 1 || v.Write.Attempted {
					t.Fatal("failed persistence retried or manufactured success", e, f.calls)
				}
				return
			}
			if e != nil || v == nil || !v.Write.Succeeded || v.Read.Attempted || v.State != "awaiting_read" || f.calls != 1 {
				t.Fatal("Write falsely implies Read", v, e)
			}
			again, _, e := s.RunVaultProbe(context.Background(), actor.ID, f.data.row.ID, "", "write", etag, input)
			if e != nil || again == nil || f.calls != 1 {
				t.Fatal("UUID replay dispatched", e)
			}
			readInput := VaultStageInput{"22222222-2222-4222-8222-222222222222", "Reviewed reader"}
			read, _, e := s.RunVaultProbe(context.Background(), actor.ID, f.data.row.ID, v.ID, "read", v.ReviewETag, readInput)
			if e != nil || read == nil || !read.Read.Succeeded || read.Cleanup.State != "acknowledged" || read.State != "completed" || read.RequestID != input.RequestID || f.calls != 3 || f.data.row.ActiveProbeID != nil {
				t.Fatal("finite explicit Read workflow lost", read, e, f.calls)
			}
			_, _, e = s.RunVaultProbe(context.Background(), actor.ID, f.data.row.ID, v.ID, "read", v.ReviewETag, readInput)
			if e != nil || f.calls != 3 {
				t.Fatal("Read UUID replay dispatched", e)
			}
		})
	}
}

func TestVaultConfigurationAtomicSecretsAndIndependentAuthority(t *testing.T) {
	for _, mode := range []string{"update", "audit_rollback", "read_denied_write_allowed", "write_denied", "noop"} {
		t.Run(mode, func(t *testing.T) {
			s, f, roles := vaultCommandService(t)
			actor := roles.data.users["usr_admin"]
			old := f.data.clone()
			etag := vaultReview(actor, old.row, old.rev)
			input := VaultConfigInput{RequestID: "33333333-3333-4333-8333-333333333333", Name: "Changed Vault", Descriptor: vaultDescriptor(old.rev), WriterAuth: VaultAuthInput{Action: "keep"}, ReaderAuth: VaultAuthInput{Action: "keep"}, Reason: "Reviewed metadata"}
			switch mode {
			case "audit_rollback":
				roles.failAudit = true
			case "read_denied_write_allowed":
				roles.deny["secrets.read"] = true
			case "write_denied":
				roles.deny["secrets.write"] = true
			case "noop":
				input.Name = old.row.Name
			}
			result, e := s.SaveVaultIntegration(context.Background(), actor.ID, old.row.ID, etag, input)
			if mode == "audit_rollback" || mode == "write_denied" {
				if e == nil || !reflect.DeepEqual(f.data, old) || f.calls != 0 {
					t.Fatal("atomic metadata rollback failed", e)
				}
				return
			}
			if e != nil || result == nil || !result.Committed || result.Changed != (mode != "noop") || f.calls != 0 || len(f.data.receipts) != 1 {
				t.Fatal("configuration-only save failed", result, e)
			}
			writer, reader, e := s.vaultOpen(f.data.writer, f.data.reader)
			if e != nil || writer != "writer-token" || reader != "reader-token" {
				t.Fatal("kept auth changed", e)
			}
			again, e := s.SaveVaultIntegration(context.Background(), actor.ID, old.row.ID, etag, input)
			if e != nil || !reflect.DeepEqual(again, result) || f.calls != 0 {
				t.Fatal("configuration UUID did not retain original saved revision", again, e)
			}
			for _, receipt := range f.data.receipts {
				if strings.Contains(receipt.IntentJSON, writer) || strings.Contains(receipt.IntentJSON, reader) {
					t.Fatal("secret receipt")
				}
			}
		})
	}
}
