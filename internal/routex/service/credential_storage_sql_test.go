package service

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/logger"
)

// Reuse real actor/admission SQL and transaction rollback. Only this slice's
// persistence tables are modeled. The controlled remote checks durable state
// and that no database transaction spans its HTTP request.
type credentialStorageData struct {
	policy      entity.CredentialStoragePolicy
	ops         map[string]entity.CredentialStorageOperation
	refs        map[string]entity.CredentialVaultReference
	providers   map[string]entity.Provider
	connections map[string]entity.ProviderConnection
	credentials map[string]entity.ProviderCredential
	receipts    map[string]entity.CredentialReplacementReceipt
}

func (d credentialStorageData) clone() credentialStorageData {
	out := d
	out.ops = cloneStorageMap(d.ops)
	out.refs = cloneStorageMap(d.refs)
	out.providers = cloneStorageMap(d.providers)
	out.connections = cloneStorageMap(d.connections)
	out.credentials = cloneStorageMap(d.credentials)
	out.receipts = cloneStorageMap(d.receipts)
	return out
}
func cloneStorageMap[T any](in map[string]T) map[string]T {
	out := map[string]T{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

type credentialStorageFixture struct {
	mu                    sync.Mutex
	data                  credentialStorageData
	active                *credentialStorageData
	vault                 *vaultCommandFixture
	posts, gets           int
	values                map[string]map[string]string
	lostWrite, readDenied bool
	retained              *vaultCommandData
	afterWrite            func()
	failStage             bool
}

func (f *credentialStorageFixture) current() *credentialStorageData {
	if f.active != nil {
		return f.active
	}
	return &f.data
}

type credentialStorageConnector struct {
	base vaultCommandConnector
	f    *credentialStorageFixture
}

func (c credentialStorageConnector) Connect(ctx context.Context) (driver.Conn, error) {
	v, e := c.base.Connect(ctx)
	if e != nil {
		return nil, e
	}
	return &credentialStorageConnection{v.(*vaultCommandConnection), c.f}, nil
}
func (c credentialStorageConnector) Driver() driver.Driver { return credentialStorageDriver(c) }

type credentialStorageDriver credentialStorageConnector

func (c credentialStorageDriver) Open(string) (driver.Conn, error) {
	return credentialStorageConnector(c).Connect(context.Background())
}

type credentialStorageConnection struct {
	*vaultCommandConnection
	f *credentialStorageFixture
}

func (c *credentialStorageConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *credentialStorageConnection) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, e := c.vaultCommandConnection.BeginTx(ctx, opts)
	if e != nil {
		return nil, e
	}
	c.f.mu.Lock()
	d := c.f.data.clone()
	c.f.active = &d
	c.f.mu.Unlock()
	return credentialStorageTx{tx, c.f}, nil
}

type credentialStorageTx struct {
	driver.Tx
	f *credentialStorageFixture
}

func (t credentialStorageTx) Commit() error {
	if e := t.Tx.Commit(); e != nil {
		return e
	}
	t.f.mu.Lock()
	t.f.data = *t.f.active
	t.f.active = nil
	t.f.mu.Unlock()
	return nil
}
func (t credentialStorageTx) Rollback() error {
	t.f.mu.Lock()
	t.f.active = nil
	t.f.mu.Unlock()
	return t.Tx.Rollback()
}
func (c *credentialStorageConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	c.f.mu.Lock()
	d := c.f.current()
	values := rolesSQLStrings(args)
	matches := func(id string) bool { return slices.Contains(values, id) }
	if old := c.f.retained; old != nil && matches(old.rev.ID) {
		switch {
		case strings.Contains(q, `FROM "vault_revisions"`):
			c.f.mu.Unlock()
			return effectiveSQLRows([]entity.VaultRevision{old.rev})
		case strings.Contains(q, `FROM "vault_writer_auth"`):
			c.f.mu.Unlock()
			return effectiveSQLRows([]entity.VaultWriterAuth{old.writer})
		case strings.Contains(q, `FROM "vault_reader_auth"`):
			c.f.mu.Unlock()
			return effectiveSQLRows([]entity.VaultReaderAuth{old.reader})
		}
	}
	switch {
	case strings.Contains(q, `FROM "credential_storage_policies"`):
		v := d.policy
		c.f.mu.Unlock()
		return effectiveSQLRows([]entity.CredentialStoragePolicy{v})
	case strings.Contains(q, `FROM "credential_storage_operations"`):
		rows := []entity.CredentialStorageOperation{}
		for _, v := range d.ops {
			if matches(v.RequestID) {
				rows = append(rows, v)
			}
		}
		c.f.mu.Unlock()
		return effectiveSQLRows(rows)
	case strings.Contains(q, `FROM "credential_vault_references"`):
		rows := []entity.CredentialVaultReference{}
		for _, v := range d.refs {
			if matches(v.CredentialID) {
				rows = append(rows, v)
			}
		}
		c.f.mu.Unlock()
		return effectiveSQLRows(rows)
	case strings.Contains(q, `vault_revisions AS rev`):
		v := c.f.vault.data
		type row struct {
			RevisionID, IntegrationID                                                                                                                                                                          string
			IntegrationBirth, CurrentBirth                                                                                                                                                                     time.Time
			Endpoint, Namespace, Mount, Prefix, DataField, ReaderID, ReaderGeneration, ReaderCiphertext, CurrentReaderID, CurrentReaderCiphertext, CurrentWriterID, CurrentWriterCiphertext, CurrentRevisionID string
		}
		rows := []row{}
		if matches(v.rev.ID) {
			rows = append(rows, row{v.rev.ID, v.row.ID, v.rev.IntegrationBirth, v.row.CreatedAt, v.rev.Endpoint, v.rev.Namespace, v.rev.Mount, v.rev.Prefix, v.rev.DataField, v.reader.ID, v.reader.SecretGeneration, v.reader.AuthCiphertext, v.reader.ID, v.reader.AuthCiphertext, v.writer.ID, v.writer.AuthCiphertext, v.row.RevisionID})
		}
		c.f.mu.Unlock()
		return effectiveSQLRows(rows)
	case strings.Contains(q, `FROM "providers"`):
		rows := []entity.Provider{}
		for _, v := range d.providers {
			if matches(v.ID) {
				rows = append(rows, v)
			}
		}
		c.f.mu.Unlock()
		return effectiveSQLRows(rows)
	case strings.Contains(q, `FROM "provider_connections"`):
		rows := []entity.ProviderConnection{}
		for _, v := range d.connections {
			if matches(v.ID) || matches(v.ProviderID) {
				rows = append(rows, v)
			}
		}
		c.f.mu.Unlock()
		return effectiveSQLRows(rows)
	case strings.Contains(q, `FROM "provider_credentials"`):
		rows := []entity.ProviderCredential{}
		for _, v := range d.credentials {
			if matches(v.ID) || matches(v.ConnectionID) || matches(v.StorageSource) || len(values) == 0 {
				rows = append(rows, v)
			}
		}
		c.f.mu.Unlock()
		return effectiveSQLRows(rows)
	case strings.Contains(q, `FROM "credential_replacement_receipts"`):
		rows := []entity.CredentialReplacementReceipt{}
		for _, v := range d.receipts {
			if matches(v.RequestID) {
				rows = append(rows, v)
			}
		}
		c.f.mu.Unlock()
		return effectiveSQLRows(rows)
	case strings.Contains(q, `FROM "egress_settings"`):
		c.f.mu.Unlock()
		return effectiveSQLRows([]entity.EgressSetting{{ID: 1, ETag: "0"}})
	}
	c.f.mu.Unlock()
	return c.vaultCommandConnection.QueryContext(ctx, q, args)
}
func credentialStorageSQLService(t *testing.T) (*Service, *credentialStorageFixture, *rolesSQLFixture) {
	t.Helper()
	s, v, roles := vaultCommandService(t)
	_, _, control := roleDefinitionSQLService(t)
	f := &credentialStorageFixture{vault: v, values: map[string]map[string]string{}, data: credentialStorageData{ops: map[string]entity.CredentialStorageOperation{}, refs: map[string]entity.CredentialVaultReference{}, providers: map[string]entity.Provider{}, connections: map[string]entity.ProviderConnection{}, credentials: map[string]entity.ProviderCredential{}, receipts: map[string]entity.CredentialReplacementReceipt{}}}
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.active != nil {
			t.Error("database transaction spans external effect")
		}
		claimed := false
		for _, op := range f.data.ops {
			if op.Claim != "" && op.ClaimedUntil.After(time.Now()) {
				claimed = true
			}
		}
		if !claimed && r.Method == http.MethodPost {
			t.Error("external write precedes durable plan")
		}
		w.Header().Set("Content-Type", "application/json")
		key := r.URL.Path
		switch r.Method {
		case http.MethodPost:
			f.posts++
			if r.Header.Get("X-Vault-Token") != "writer-token" {
				t.Error("wrong write identity")
			}
			var body struct {
				Data    map[string]string `json:"data"`
				Options struct {
					CAS int `json:"cas"`
				} `json:"options"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Options.CAS != 0 || len(body.Data) != 2 {
				t.Error("not an exact credential CAS0 write")
			}
			if _, exists := f.values[key]; exists {
				t.Error("write replayed")
			}
			f.values[key] = body.Data
			if f.afterWrite != nil {
				f.afterWrite()
			}
			if f.lostWrite {
				w.WriteHeader(503)
				return
			}
			_, _ = w.Write([]byte(`{"data":{"version":1,"destroyed":false,"deletion_time":""}}`))
		case http.MethodGet:
			f.gets++
			if r.Header.Get("X-Vault-Token") != "reader-token" || r.URL.Query().Get("version") != "1" {
				t.Error("wrong pinned read")
			}
			if f.readDenied {
				w.WriteHeader(403)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": f.values[key], "metadata": map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}}})
		default:
			t.Error("unexpected destroy/remote operation")
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(stub.Close)
	v.data.rev.Endpoint = stub.URL
	integrationID, integrationBirth, revisionID := v.data.row.ID, v.data.row.CreatedAt, v.data.rev.ID
	f.data.policy = entity.CredentialStoragePolicy{ID: 1, Mode: "vault", IntegrationID: &integrationID, IntegrationBirth: &integrationBirth, RevisionID: &revisionID, Generation: "csp_reviewed"}
	pool := sql.OpenDB(credentialStorageConnector{vaultCommandConnector{roleDefinitionSQLConnector{roles, control}, v}, f})
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
		switch x := tx.Statement.Dest.(type) {
		case *entity.Provider:
			d.providers[x.ID] = *x
		case *entity.ProviderConnection:
			d.connections[x.ID] = *x
		case *entity.ProviderCredential:
			d.credentials[x.ID] = *x
		case *entity.CredentialStorageOperation:
			d.ops[x.RequestID] = *x
		case *entity.CredentialVaultReference:
			d.refs[x.CredentialID] = *x
		case *entity.CredentialReplacementReceipt:
			d.receipts[x.RequestID] = *x
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
		if tx.Statement.Table != "credential_storage_operations" {
			update(tx)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		d := f.current()
		if f.failStage {
			_ = tx.AddError(errors.New("controlled stage persistence failure"))
			return
		}
		for key, op := range d.ops {
			switch x := tx.Statement.Dest.(type) {
			case *entity.CredentialStorageOperation:
				op.Claim = x.Claim
				op.ClaimedUntil = x.ClaimedUntil
			case map[string]any:
				for name, value := range x {
					switch name {
					case "state":
						op.State = value.(string)
					case "write_json":
						op.WriteJSON = value.(string)
					case "read_json":
						op.ReadJSON = value.(string)
					case "claimed_until":
						op.ClaimedUntil = value.(time.Time)
					}
				}
			}
			d.ops[key] = op
		}
		tx.RowsAffected = 1
	}); e != nil {
		t.Fatal(e)
	}
	s.db = db
	return s, f, roles
}
func credentialStorageFixtureIntent(t *testing.T, s *Service, f *credentialStorageFixture, kind string) credentialCreationIntent {
	t.Helper()
	birth := time.Now().UTC().Truncate(time.Microsecond)
	p := entity.Provider{ID: "prv_existing", Name: "Existing", CreatedAt: birth}
	c := entity.ProviderConnection{ID: "con_existing", ProviderID: p.ID, Name: "Existing", Protocol: "openai_chat", BaseURL: "https://example.com/v1", EgressMode: "direct", CreatedAt: birth}
	f.data.providers[p.ID] = p
	f.data.connections[c.ID] = c
	i := credentialCreationIntent{Kind: kind, ProviderName: "New Provider", CredentialName: "New credential", Priority: 3, Reason: "Reviewed creation", Connection: CreateConnectionInput{Name: "New connection", BaseURL: c.BaseURL, Protocol: c.Protocol, EgressMode: "direct", CredentialName: "New credential"}}
	switch kind {
	case "connection":
		i.Target = p.ID
	case "credential":
		i.Target = c.ID
	case "replacement":
		old := entity.ProviderCredential{ID: "crd_01aaaaaaaaaaaaaaaaaaaaaaaa", Name: "Old", ConnectionID: c.ID, Priority: 7, StorageSource: "inline", CreatedAt: birth}
		var e error
		old.Ciphertext, e = s.sealSecret(old.ID, "old-value")
		if e != nil {
			t.Fatal(e)
		}
		f.data.credentials[old.ID] = old
		i.Target = old.ID
		i.SourceETag = credentialMetadataRecord(old).ETag
	}
	i.PolicyETag = credentialStorageContext(f.vaultRoleActor(s, t), f.data.policy).ETag
	return i
}
func (f *credentialStorageFixture) vaultRoleActor(s *Service, t *testing.T) entity.User {
	t.Helper()
	actor, e := exactEnabledActor(s.db, "usr_admin")
	if e != nil {
		t.Fatal(e)
	}
	return actor
}

func TestCredentialStorageDurablePlansAllCreatesAndReadOnlyRecovery(t *testing.T) {
	for _, kind := range []string{"provider", "connection", "credential", "replacement"} {
		t.Run(kind, func(t *testing.T) {
			s, f, _ := credentialStorageSQLService(t)
			i := credentialStorageFixtureIntent(t, s, f, kind)
			ctx := context.Background()
			uuid := "11111111-1111-4111-8111-111111111111"
			op, e := s.createVaultCredential(ctx, "usr_admin", uuid, "private-value", i)
			if e != nil {
				t.Fatal(e)
			}
			if op.State != "committed" || !op.created || f.posts != 1 || f.gets != 1 {
				t.Fatal("missing durable creation", op.State, f.posts, f.gets)
			}
			saved := f.data.credentials[op.CredentialID]
			if saved.Ciphertext != "" || saved.StorageSource != "vault" || saved.Enabled || saved.VerificationStatus != "pending" {
				t.Fatal("Vault value persisted or implicitly enabled")
			}
			before := f.data.clone()
			again, e := s.createVaultCredential(ctx, "usr_admin", uuid, "private-value", i)
			if e != nil || again.CredentialID != op.CredentialID || again.created || f.posts != 1 || f.gets != 2 || len(f.data.credentials) != len(before.credentials) {
				t.Fatal("recovery repeated business/external write", e)
			}
			if _, e = s.createVaultCredential(ctx, "usr_admin", uuid, "different-value", i); !errors.Is(e, catalogConflict) || f.posts != 1 {
				t.Fatal("different value accepted", e)
			}
			raw, _ := json.Marshal(f.data.ops[uuid])
			if strings.Contains(string(raw), "private-value") || strings.Contains(string(raw), "reader-token") || strings.Contains(string(raw), "routex_ownership_marker") {
				t.Fatal("durable observation leaked material")
			}
		})
	}
}
func TestCredentialStorageAmbiguousWriteRecoversWithoutReplay(t *testing.T) {
	s, f, _ := credentialStorageSQLService(t)
	i := credentialStorageFixtureIntent(t, s, f, "credential")
	uuid := "11111111-1111-4111-8111-111111111111"
	f.lostWrite = true
	op, e := s.createVaultCredential(context.Background(), "usr_admin", uuid, "private-value", i)
	if e == nil || op.State != "unknown" || f.posts != 1 || f.gets != 0 || len(f.data.refs) != 0 {
		t.Fatal("ambiguous external write became success", e)
	}
	write := f.data.ops[uuid].WriteJSON
	f.lostWrite = false
	op, e = s.createVaultCredential(context.Background(), "usr_admin", uuid, "private-value", i)
	if e != nil || op.State != "committed" || f.posts != 1 || f.gets != 1 || f.data.ops[uuid].WriteJSON != write {
		t.Fatal("original unknown write rewritten/replayed", e)
	}
}
func TestCredentialStorageInlineUUIDRetainsCiphertextAfterPolicySwitch(t *testing.T) {
	s, f, _ := credentialStorageSQLService(t)
	f.data.policy = entity.CredentialStoragePolicy{ID: 1, Mode: "inline", Generation: "inline_review"}
	i := credentialStorageFixtureIntent(t, s, f, "credential")
	uuid := "11111111-1111-4111-8111-111111111111"
	op, e := s.createInlineCredential(context.Background(), "usr_admin", uuid, "private-value", i)
	if e != nil {
		t.Fatal(e)
	}
	cipher := f.data.credentials[op.CredentialID].Ciphertext
	f.data.policy.Generation = "changed"
	f.data.policy.Mode = "vault"
	again, e := s.createInlineCredential(context.Background(), "usr_admin", uuid, "private-value", i)
	if e != nil || !op.created || again.created || again.CredentialID != op.CredentialID || f.data.credentials[op.CredentialID].Ciphertext != cipher || f.posts != 0 {
		t.Fatal("inline recovery resealed/repointed", e)
	}
	before := f.data.clone()
	if _, e = s.createInlineCredential(context.Background(), "usr_admin", uuid, "different", i); !errors.Is(e, catalogConflict) || !reflect.DeepEqual(before, f.data) {
		t.Fatal("different replay mutated original", e)
	}
}

func TestCredentialStorageReplayChecksFreshAuthorityAndAuditRollback(t *testing.T) {
	for _, fault := range []string{"denied_write", "audit_failure", "changed_review", "malformed_secret"} {
		t.Run(fault, func(t *testing.T) {
			s, f, roles := credentialStorageSQLService(t)
			i := credentialStorageFixtureIntent(t, s, f, "credential")
			uuid := "11111111-1111-4111-8111-111111111111"
			value := "private-value"
			switch fault {
			case "denied_write":
				roles.deny["providers.write"] = true
			case "audit_failure":
				roles.failAudit = true
			case "changed_review":
				i.PolicyETag = strings.Repeat("0", 64)
			case "malformed_secret":
				value = string([]byte{0xff})
			}
			_, e := s.createVaultCredential(context.Background(), "usr_admin", uuid, value, i)
			if e == nil {
				t.Fatal("invalid creation accepted")
			}
			if fault == "audit_failure" {
				if f.posts != 1 || f.gets != 1 || len(f.data.credentials) != 0 || len(f.data.refs) != 0 || f.data.ops[uuid].State != "orphan" {
					t.Fatal("audit rollback lost external orphan fact")
				}
			} else if f.posts != 0 || len(f.data.ops) != 0 {
				t.Fatal("remote write before validation/authority")
			}
		})
	}
	s, f, roles := credentialStorageSQLService(t)
	i := credentialStorageFixtureIntent(t, s, f, "credential")
	uuid := "11111111-1111-4111-8111-111111111111"
	if _, e := s.createVaultCredential(context.Background(), "usr_admin", uuid, "private-value", i); e != nil {
		t.Fatal(e)
	}
	roles.deny["providers.write"] = true
	before := f.data.clone()
	if _, e := s.createVaultCredential(context.Background(), "usr_admin", uuid, "private-value", i); e == nil || f.gets != 1 || !reflect.DeepEqual(before, f.data) {
		t.Fatal("revoked replay read/mutated", e)
	}
}
func TestCredentialStorageCommittedReadFailureRetainsOriginalBusinessFact(t *testing.T) {
	s, f, _ := credentialStorageSQLService(t)
	i := credentialStorageFixtureIntent(t, s, f, "credential")
	uuid := "11111111-1111-4111-8111-111111111111"
	op, e := s.createVaultCredential(context.Background(), "usr_admin", uuid, "private-value", i)
	if e != nil {
		t.Fatal(e)
	}
	write := op.WriteJSON
	f.readDenied = true
	if _, e = s.createVaultCredential(context.Background(), "usr_admin", uuid, "private-value", i); e == nil || f.data.ops[uuid].State != "committed" || f.data.ops[uuid].WriteJSON != write || f.data.credentials[op.CredentialID].ID != op.CredentialID {
		t.Fatal("current read denial rewrote historical save", e)
	}
}
func TestCredentialStorageHistoricalInlineReplacementSurvivesFuturePolicyChange(t *testing.T) {
	s, f, _ := credentialStorageSQLService(t)
	i := credentialStorageFixtureIntent(t, s, f, "replacement")
	uuid := "11111111-1111-4111-8111-111111111111"
	input := CredentialReplacementInput{RequestID: uuid, Name: i.CredentialName, Secret: "retained-value", Reason: i.Reason}
	replacement := entity.ProviderCredential{ID: "crd_retained", ConnectionID: "con_existing", Name: i.CredentialName, ReplacesCredentialID: &i.Target}
	var e error
	replacement.Ciphertext, e = s.sealSecret(replacement.ID, input.Secret)
	if e != nil {
		t.Fatal(e)
	}
	f.data.credentials[replacement.ID] = replacement
	f.data.receipts[uuid] = entity.CredentialReplacementReceipt{RequestID: uuid, ActorID: "usr_admin", SourceCredentialID: i.Target, ConnectionID: replacement.ConnectionID, ResultCredentialID: replacement.ID, RequestHash: credentialReplacementHash("usr_admin", i.Target, i.SourceETag, input)}
	// Future policy is already Vault, but original legacy receipt has no context token.
	before := f.data.clone()
	result, created, e := s.CreateCredentialReplacement(context.Background(), "usr_admin", i.Target, i.SourceETag, input)
	if e != nil || created || result.ID != replacement.ID || result.StorageSource != "inline" || f.posts != 0 || !reflect.DeepEqual(before, f.data) {
		t.Fatal("historical receipt followed new policy", e)
	}
	input.Secret = "different"
	if _, _, e = s.CreateCredentialReplacement(context.Background(), "usr_admin", i.Target, i.SourceETag, input); !errors.Is(e, catalogConflict) {
		t.Fatal("historical secret mismatch accepted", e)
	}
}

func TestCredentialStoragePreparedValueSurvivesAuthenticatedRootRewrap(t *testing.T) {
	s, f, _ := credentialStorageSQLService(t)
	i := credentialStorageFixtureIntent(t, s, f, "credential")
	op, e := s.createVaultCredential(context.Background(), "usr_admin", "11111111-1111-4111-8111-111111111111", "private-value", i)
	if e != nil {
		t.Fatal(e)
	}
	rows := []entity.ProviderCredential{f.data.credentials[op.CredentialID]}
	if e = attachCredentialSources(s.db, rows); e != nil {
		t.Fatal(e)
	}
	c := rows[0]
	proof := credentialSourceProof(c)
	if e = s.cacheCredentialValue(context.Background(), c, "private-value"); e != nil {
		t.Fatal(e)
	}
	// Rewrap under a second real authenticated keyring. Source identity stays
	// immutable; ciphertext changes and must still validate locally at consumption.
	store, e := secretstore.NewKeyring(map[string][]byte{"root": bytes.Repeat([]byte{3}, 32), "new": bytes.Repeat([]byte{9}, 32)}, "root", "new")
	if e != nil {
		t.Fatal(e)
	}
	f.vault.data.reader.AuthCiphertext, e = store.Seal(rootReference("vault_reader_auth", f.vault.data.reader.ID, f.vault.data.reader.SecretGeneration), "reader-token")
	if e != nil {
		t.Fatal(e)
	}
	s.secrets = store
	rows = []entity.ProviderCredential{f.data.credentials[op.CredentialID]}
	if e = attachCredentialSources(s.db, rows); e != nil {
		t.Fatal(e)
	}
	if credentialSourceProof(rows[0]) != proof {
		t.Fatal("root rewrap changed retained source identity")
	}
	before := f.gets
	value, e := s.preparedCredentialValue(rows[0])
	if e != nil || value != "private-value" || f.gets != before {
		t.Fatal("prepared value required Verify/remote read after rewrap", e)
	}
	for _, fault := range []string{"reader_removed", "reader_generation", "reader_tampered", "integration_recreated", "missing_reference", "current_revision_alias", "current_writer_alias"} {
		t.Run(fault, func(t *testing.T) {
			reader := f.vault.data.reader
			writer := f.vault.data.writer
			row := f.vault.data.row
			ref := f.data.refs[op.CredentialID]
			defer func() {
				f.vault.data.reader = reader
				f.vault.data.writer = writer
				f.vault.data.row = row
				f.data.refs[op.CredentialID] = ref
			}()
			switch fault {
			case "reader_removed":
				f.vault.data.reader.AuthCiphertext = ""
			case "reader_generation":
				f.vault.data.reader.SecretGeneration = "new-generation"
			case "reader_tampered":
				f.vault.data.reader.AuthCiphertext = "invalid-envelope"
			case "integration_recreated":
				f.vault.data.row.CreatedAt = row.CreatedAt.Add(time.Second)
			case "missing_reference":
				delete(f.data.refs, op.CredentialID)
			case "current_revision_alias":
				f.vault.data.row.RevisionID = strings.ToUpper(row.RevisionID)
			case "current_writer_alias":
				f.vault.data.writer.ID = strings.ToUpper(writer.ID)
			}
			rows := []entity.ProviderCredential{f.data.credentials[op.CredentialID]}
			if e := attachCredentialSources(s.db, rows); e == nil {
				if _, e = s.preparedCredentialValue(rows[0]); e == nil {
					t.Fatal("revoked or malformed source reused prepared value")
				}
			}
			if f.gets != before {
				t.Fatal("runtime source check performed HTTP")
			}
		})
	}
}
func TestCredentialStorageUUIDResponseContainsOnlyOwnedBootstrap(t *testing.T) {
	s, f, _ := credentialStorageSQLService(t)
	i := credentialStorageFixtureIntent(t, s, f, "provider")
	op, e := s.createVaultCredential(context.Background(), "usr_admin", "11111111-1111-4111-8111-111111111111", "private-value", i)
	if e != nil {
		t.Fatal(e)
	}
	unrelated := entity.ProviderCredential{ID: "crd_later", ConnectionID: op.ConnectionID, StorageSource: "inline"}
	f.data.credentials[unrelated.ID] = unrelated
	result, e := s.createdProviderCatalog(context.Background(), op.CredentialStorageOperation)
	if e != nil || len(result.Connections) != 1 || len(result.Connections[0].Credentials) != 1 || result.Connections[0].Credentials[0].ID != op.CredentialID {
		t.Fatal("UUID response identified a later row", e)
	}
}

func TestCredentialStorageInventoryKeepsSevenDomainsAndRejectsEmptyInline(t *testing.T) {
	s, f, _ := credentialStorageSQLService(t)
	i := credentialStorageFixtureIntent(t, s, f, "credential")
	op, e := s.createVaultCredential(context.Background(), "usr_admin", "11111111-1111-4111-8111-111111111111", "private-value", i)
	if e != nil {
		t.Fatal(e)
	}
	inline := entity.ProviderCredential{ID: "crd_later", StorageSource: "inline", Ciphertext: "authenticated-envelope"}
	f.data.credentials[inline.ID] = inline
	rows, e := s.rootInventoryPage(context.Background(), "provider_credentials", "")
	if e != nil || len(rows) != 1 || rows[0].id != inline.ID || len(rootDomains) != 7 {
		t.Fatal("valid external reference became value domain or hid inline inventory", e, rows)
	}
	for _, fault := range []string{"empty_inline", "unknown_source", "vault_cipher", "missing_ref"} {
		t.Run(fault, func(t *testing.T) {
			before := f.data.clone()
			defer func() { f.data = before }()
			switch fault {
			case "empty_inline":
				v := f.data.credentials[inline.ID]
				v.Ciphertext = ""
				f.data.credentials[inline.ID] = v
			case "unknown_source":
				v := f.data.credentials[inline.ID]
				v.StorageSource = "other"
				f.data.credentials[inline.ID] = v
			case "vault_cipher":
				v := f.data.credentials[op.CredentialID]
				v.Ciphertext = "wrong"
				f.data.credentials[op.CredentialID] = v
			case "missing_ref":
				delete(f.data.refs, op.CredentialID)
			}
			if _, e := s.rootInventoryPage(context.Background(), "provider_credentials", ""); e == nil {
				t.Fatal("malformed source silently skipped")
			}
		})
	}
}
func TestCredentialStorageCachePrunesDeletedSourcesBeforeCapacity(t *testing.T) {
	s, f, _ := credentialStorageSQLService(t)
	i := credentialStorageFixtureIntent(t, s, f, "credential")
	op, e := s.createVaultCredential(context.Background(), "usr_admin", "11111111-1111-4111-8111-111111111111", "private-value", i)
	if e != nil {
		t.Fatal(e)
	}
	rows := []entity.ProviderCredential{f.data.credentials[op.CredentialID]}
	if e = attachCredentialSources(s.db, rows); e != nil {
		t.Fatal(e)
	}
	s.credentialValues = map[string]string{}
	for n := 0; n < 5000; n++ {
		s.credentialValues[fmt.Sprintf("deleted-%d", n)] = "stale-value"
	}
	if e = s.cacheCredentialValue(context.Background(), rows[0], "private-value"); e != nil || len(s.credentialValues) != 1 {
		t.Fatal("cumulative deleted history consumed current preparation capacity", e)
	}
	// A compiled attempt retains its own immutable value; cache pruning changes
	// neither that value nor recorded route identity.
	compiled := runtimeCredential{ID: op.CredentialID, Plaintext: "private-value", CipherHash: credentialSourceProof(rows[0])}
	delete(f.data.credentials, op.CredentialID)
	if e = s.cacheCredentialValue(context.Background(), rows[0], "private-value"); !errors.Is(e, catalogConflict) {
		t.Fatal("deleted source was prepared", e)
	}
	if len(s.credentialValues) != 0 || compiled.Plaintext != "private-value" || compiled.ID != op.CredentialID {
		t.Fatal("pruning changed in-flight attribution/value")
	}
}
func TestCredentialStorageStartupOutageDoesNotBlockInlineRecovery(t *testing.T) {
	s, f, _ := credentialStorageSQLService(t)
	i := credentialStorageFixtureIntent(t, s, f, "credential")
	op, e := s.createVaultCredential(context.Background(), "usr_admin", "11111111-1111-4111-8111-111111111111", "private-value", i)
	if e != nil {
		t.Fatal(e)
	}
	v := f.data.credentials[op.CredentialID]
	v.Enabled = true
	v.VerificationStatus = "verified"
	f.data.credentials[v.ID] = v
	f.readDenied = true
	before := f.data.clone()
	if e = s.prepareStartupCredentialValues(context.Background()); e != nil || len(s.credentialValues) != 0 || !reflect.DeepEqual(before, f.data) {
		t.Fatal("one external outage blocked startup or rewrote saved state", e)
	}
	inline := entity.ProviderCredential{ID: "crd_inline", StorageSource: "inline"}
	inline.Ciphertext, e = s.sealSecret(inline.ID, "inline-value")
	if e != nil {
		t.Fatal(e)
	}
	if value, e := s.preparedCredentialValue(inline); e != nil || value != "inline-value" {
		t.Fatal("valid inline supply unavailable", e)
	}
	rows := []entity.ProviderCredential{v}
	if e = attachCredentialSources(s.db, rows); e != nil {
		t.Fatal(e)
	}
	if _, e = s.preparedCredentialValue(rows[0]); e == nil {
		t.Fatal("outage admitted unprepared external source")
	}
}

func TestCredentialStorageSelectedRevisionAdvanceRejectsBeforePlan(t *testing.T) {
	s, f, _ := credentialStorageSQLService(t)
	i := credentialStorageFixtureIntent(t, s, f, "credential")
	original := f.vault.data.rev
	// The saved policy still pins R1; current configuration advances independently.
	// A creator's unchanged source-context review cannot authorize R2 implicitly.
	f.vault.data.row.RevisionID = "vlr_02bbbbbbbbbbbbbbbbbbbbbbbb"
	f.vault.data.rev.ID = f.vault.data.row.RevisionID
	f.vault.data.writer.ID = f.vault.data.row.RevisionID
	f.vault.data.reader.ID = f.vault.data.row.RevisionID
	if _, e := s.createVaultCredential(context.Background(), "usr_admin", "11111111-1111-4111-8111-111111111111", "private-value", i); e == nil || len(f.data.ops) != 0 || f.posts != 0 {
		t.Fatal("new creation silently followed current revision", e)
	}
	if *f.data.policy.RevisionID != original.ID {
		t.Fatal("creator changed administrative policy")
	}
}

func TestCredentialStorageUnknownWriteRecoveryRetainsOriginalSourceAfterPolicyChange(t *testing.T) {
	s, f, _ := credentialStorageSQLService(t)
	i := credentialStorageFixtureIntent(t, s, f, "credential")
	uuid := "11111111-1111-4111-8111-111111111111"
	f.lostWrite = true
	if _, err := s.createVaultCredential(context.Background(), "usr_admin", uuid, "private-value", i); err == nil {
		t.Fatal("ambiguous write became successful")
	}
	original := f.data.ops[uuid]
	f.data.policy = entity.CredentialStoragePolicy{ID: 1, Mode: "inline", Generation: "later_inline"}
	vaultMode, err := s.useVaultCreation(context.Background(), "usr_admin", uuid, i.PolicyETag)
	if err != nil || !vaultMode {
		t.Fatal("recovery selected new future source", err)
	}
	f.lostWrite = false
	result, err := s.createVaultCredential(context.Background(), "usr_admin", uuid, "private-value", i)
	if err != nil || result.StorageSource != "vault" || result.ReferenceID != original.ReferenceID || result.RevisionID != original.RevisionID || result.WriteJSON != original.WriteJSON || result.created || f.posts != 1 || f.gets != 1 {
		t.Fatal("original unknown observation/source was changed or replayed", err)
	}
}
func TestCredentialStorageCrossActorUUIDReadAndReplayDenyWithoutFacts(t *testing.T) {
	s, f, roles := credentialStorageSQLService(t)
	i := credentialStorageFixtureIntent(t, s, f, "credential")
	uuid := "11111111-1111-4111-8111-111111111111"
	if _, err := s.createVaultCredential(context.Background(), "usr_admin", uuid, "private-value", i); err != nil {
		t.Fatal(err)
	}
	other := roles.data.users["usr_admin"]
	other.ID = "usr_other"
	other.CreatedAt = other.CreatedAt.Add(time.Second)
	roles.data.users[other.ID] = other
	before := f.data.clone()
	if view, err := s.GetCredentialStorageOperation(context.Background(), other.ID, uuid); err == nil || view != nil {
		t.Fatal("another actor received operation facts", err)
	}
	if result, err := s.createVaultCredential(context.Background(), other.ID, uuid, "private-value", i); err == nil || result != nil {
		t.Fatal("another actor recovered original command", err)
	}
	if f.posts != 1 || f.gets != 1 || !reflect.DeepEqual(before, f.data) {
		t.Fatal("denied recovery mutated/read remote source")
	}
}

func TestCredentialStorageUnknownWriteRecoveryRetainsOriginalRevisionAfterAdvance(t *testing.T) {
	s, f, _ := credentialStorageSQLService(t)
	i := credentialStorageFixtureIntent(t, s, f, "credential")
	uuid := "11111111-1111-4111-8111-111111111111"
	f.lostWrite = true
	if _, err := s.createVaultCredential(context.Background(), "usr_admin", uuid, "private-value", i); err == nil {
		t.Fatal("ambiguous Write became successful")
	}
	original := f.data.ops[uuid]
	old := f.vault.data
	f.retained = &old
	f.vault.data.row.RevisionID = "vlr_02bbbbbbbbbbbbbbbbbbbbbbbb"
	f.vault.data.rev.ID = f.vault.data.row.RevisionID
	f.vault.data.rev.Prefix = "future-prefix"
	f.vault.data.writer.ID = f.vault.data.row.RevisionID
	f.vault.data.reader.ID = f.vault.data.row.RevisionID
	f.data.policy = entity.CredentialStoragePolicy{ID: 1, Mode: "inline", Generation: "future_inline"}
	f.lostWrite = false
	result, err := s.createVaultCredential(context.Background(), "usr_admin", uuid, "private-value", i)
	if err != nil || result.RevisionID != old.rev.ID || result.ReferenceID != original.ReferenceID || result.WriteJSON != original.WriteJSON || result.created || f.posts != 1 || f.gets != 1 {
		t.Fatal("recovery followed newer descriptor or replayed Write", err)
	}
	if f.data.refs[result.CredentialID].RevisionID != old.rev.ID {
		t.Fatal("saved reference repointed")
	}
}

func TestCredentialStorageUnknownWriteRecoveryAfterCompletedRootRewrap(t *testing.T) {
	s, f, _ := credentialStorageSQLService(t)
	i := credentialStorageFixtureIntent(t, s, f, "credential")
	uuid := "11111111-1111-4111-8111-111111111111"
	f.lostWrite = true
	if _, err := s.createVaultCredential(context.Background(), "usr_admin", uuid, "private-value", i); err == nil {
		t.Fatal("ambiguous Write became successful")
	}
	original := f.data.ops[uuid]
	ring, err := secretstore.NewKeyring(map[string][]byte{"root": bytes.Repeat([]byte{3}, 32), "new": bytes.Repeat([]byte{9}, 32)}, "root", "new")
	if err != nil {
		t.Fatal(err)
	}
	f.vault.data.writer.AuthCiphertext, err = ring.Seal(rootReference("vault_writer_auth", f.vault.data.writer.ID, f.vault.data.writer.SecretGeneration), "writer-token")
	if err != nil {
		t.Fatal(err)
	}
	f.vault.data.reader.AuthCiphertext, err = ring.Seal(rootReference("vault_reader_auth", f.vault.data.reader.ID, f.vault.data.reader.SecretGeneration), "reader-token")
	if err != nil {
		t.Fatal(err)
	}
	s.rootPolicy.Store(&secretPolicyView{epoch: 1, writeID: "new", store: ring})
	f.lostWrite = false
	result, err := s.createVaultCredential(context.Background(), "usr_admin", uuid, "private-value", i)
	if err != nil || result.RootEpoch != original.RootEpoch || result.IntentJSON != original.IntentJSON || result.WriteJSON != original.WriteJSON || result.ReferenceID != original.ReferenceID || result.created || f.posts != 1 || f.gets != 1 {
		t.Fatal("root rewrap blocked original ownership recovery or rewrote its history", err)
	}
	if s.rootPolicy.Load().refs.Load() != 0 || s.rootReaders.Load() != 0 {
		t.Fatal("finite recovery leaked reader lease")
	}
}

func TestCredentialStorageInitialWriteStillRequiresOriginalPolicyAtCommit(t *testing.T) {
	s, f, _ := credentialStorageSQLService(t)
	i := credentialStorageFixtureIntent(t, s, f, "credential")
	uuid := "11111111-1111-4111-8111-111111111111"
	f.afterWrite = func() { f.data.policy.Generation = "changed_during_initial_write" }
	result, err := s.createVaultCredential(context.Background(), "usr_admin", uuid, "private-value", i)
	if !errors.Is(err, catalogConflict) || result == nil || f.data.ops[uuid].State != "orphan" || len(f.data.credentials) != 0 || len(f.data.refs) != 0 || f.posts != 1 || f.gets != 1 {
		t.Fatal("initial changed-policy write was committed or lost external orphan", err)
	}
	original := f.data.ops[uuid]
	f.afterWrite = nil
	result, err = s.createVaultCredential(context.Background(), "usr_admin", uuid, "private-value", i)
	if err != nil || result.created || result.PolicyGeneration != original.PolicyGeneration || result.IntentJSON != original.IntentJSON || result.WriteJSON != original.WriteJSON || result.ReferenceID != original.ReferenceID || f.posts != 1 || f.gets != 2 {
		t.Fatal("explicit original-source recovery replayed Write or rewrote intent", err)
	}
}

func TestCredentialStorageDeletedInlineReplacementReceiptRemainsConflict(t *testing.T) {
	s, f, roles := credentialStorageSQLService(t)
	intent := credentialStorageFixtureIntent(t, s, f, "replacement")
	requestID := "11111111-1111-4111-8111-111111111111"
	input := CredentialReplacementInput{RequestID: requestID, Name: intent.CredentialName, Secret: "retained-value", Reason: intent.Reason}
	// The authorized durable receipt remains after deletion of its exact result.
	f.data.receipts[requestID] = entity.CredentialReplacementReceipt{RequestID: requestID, ActorID: "usr_admin", SourceCredentialID: intent.Target, ConnectionID: "con_existing", ResultCredentialID: "crd_deleted", RequestHash: credentialReplacementHash("usr_admin", intent.Target, intent.SourceETag, input)}
	before := f.data.clone()
	result, created, err := s.CreateCredentialReplacement(context.Background(), "usr_admin", intent.Target, intent.SourceETag, input)
	if !errors.Is(err, catalogConflict) || result != nil || created || f.posts != 0 || !reflect.DeepEqual(before, f.data) {
		t.Fatal("deleted result changed retained retry semantics", err)
	}
	roles.deny["providers.write"] = true
	if _, _, err = s.CreateCredentialReplacement(context.Background(), "usr_admin", intent.Target, intent.SourceETag, input); err == nil || errors.Is(err, catalogConflict) {
		t.Fatal("current write authority bypassed", err)
	}
	if !reflect.DeepEqual(before, f.data) || f.posts != 0 {
		t.Fatal("retry recreated result or receipt")
	}
}
