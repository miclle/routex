package service

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/secretstore"
)

// This wrapper retains existing actor/permission/Connection fixture behavior.
// Its extra selected Credential row and read-only commits never model a real DB.
type connectionDiagnosticSQLState struct {
	base          *connectionMetadataSQLState
	credential    entity.ProviderCredential
	egress        *entity.Egress
	egressAlias   bool
	alias         bool
	missing       bool
	commits       int
	readOptions   []driver.TxOptions
	afterSnapshot func(int)
}

type connectionDiagnosticSQLConnector struct {
	base  connectionMetadataSQLConnector
	state *connectionDiagnosticSQLState
}

func (c connectionDiagnosticSQLConnector) Connect(ctx context.Context) (driver.Conn, error) {
	base, err := c.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &connectionDiagnosticSQLConnection{connectionMetadataSQLConnection: base.(*connectionMetadataSQLConnection), state: c.state}, nil
}
func (c connectionDiagnosticSQLConnector) Driver() driver.Driver {
	return connectionDiagnosticSQLDriver(c)
}

type connectionDiagnosticSQLDriver connectionDiagnosticSQLConnector

func (d connectionDiagnosticSQLDriver) Open(string) (driver.Conn, error) {
	return connectionDiagnosticSQLConnector(d).Connect(context.Background())
}

type connectionDiagnosticSQLConnection struct {
	*connectionMetadataSQLConnection
	state *connectionDiagnosticSQLState
}

func (c *connectionDiagnosticSQLConnection) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	c.state.readOptions = append(c.state.readOptions, options)
	tx, err := c.connectionMetadataSQLConnection.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return connectionDiagnosticSQLTransaction{Tx: tx, state: c.state}, nil
}

func (c *connectionDiagnosticSQLConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, `FROM "egresses"`) && c.state.egress != nil {
		c.f.queries = append(c.f.queries, query)
		row := *c.state.egress
		if c.state.egressAlias {
			row.ID = "EGR_TARGET"
		}
		return effectiveSQLRows([]entity.Egress{row})
	}
	if strings.Contains(query, `FROM "provider_credentials"`) {
		c.f.queries = append(c.f.queries, query)
		rows := []entity.ProviderCredential{}
		if !c.state.missing {
			row := c.state.credential
			if c.state.alias {
				row.ID = "CRD_TARGET"
			}
			rows = append(rows, row)
		}
		return effectiveSQLRows(rows)
	}
	return c.connectionMetadataSQLConnection.QueryContext(ctx, query, args)
}

type connectionDiagnosticSQLTransaction struct {
	driver.Tx
	state *connectionDiagnosticSQLState
}

func (tx connectionDiagnosticSQLTransaction) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	tx.state.commits++
	if tx.state.afterSnapshot != nil {
		tx.state.afterSnapshot(tx.state.commits)
	}
	return nil
}

type connectionDiagnosticTransport func(*http.Request) (*http.Response, error)

func (f connectionDiagnosticTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func connectionDiagnosticSQLService(t *testing.T) (*Service, *rolesSQLFixture, *roleDefinitionSQLControl, *connectionDiagnosticSQLState, string) {
	t.Helper()
	_, roles, control := roleDefinitionSQLService(t)
	actor, provider, connection := connectionMetadataTestRows()
	roles.data.users[actor.ID] = actor
	connection.EgressMode = "direct"
	store, err := secretstore.New(bytes.Repeat([]byte{93}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := store.Seal("crd_target", "test-only-explicit-diagnostic-secret")
	if err != nil {
		t.Fatal(err)
	}
	state := &connectionDiagnosticSQLState{base: &connectionMetadataSQLState{provider: provider, row: connection}, credential: entity.ProviderCredential{ID: "crd_target", ConnectionID: connection.ID, Name: "Explicit pending", Ciphertext: ciphertext, StorageSource: "inline", Enabled: false, VerificationStatus: "pending", CreatedAt: connection.CreatedAt}}
	pool := sql.OpenDB(connectionDiagnosticSQLConnector{base: connectionMetadataSQLConnector{roles, control, state.base}, state: state})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{db: db, secrets: store}
	service.upstream = &http.Client{Transport: connectionDiagnosticTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method != "GET" || request.URL.Path != "/v1/models" || request.Header.Get("Authorization") != "Bearer test-only-explicit-diagnostic-secret" {
			t.Fatal("incorrect explicit native request", request.Method, request.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[]}`)), Header: http.Header{}}, nil
	})}
	review, err := connectionMetadataRecord(actor, provider, connection, true)
	if err != nil {
		t.Fatal(err)
	}
	return service, roles, control, state, review.ETag
}

func TestConnectionDiagnosticSQLReadOnlySelectedPendingCredential(t *testing.T) {
	s, roles, _, state, etag := connectionDiagnosticSQLService(t)
	beforeConnection, beforeCredential := state.base.row, state.credential
	if beforeConnection.Enabled || beforeCredential.Enabled || beforeCredential.VerificationStatus != "pending" {
		t.Fatal("fixture must explicitly retain disabled Connection and pending Credential")
	}
	result, err := s.TestConnection(context.Background(), "usr_admin", state.base.row.ID, etag, ConnectionDiagnosticInput{CredentialID: state.credential.ID})
	if err != nil || result == nil || result.Outcome != "passed" || result.Scope != "model_discovery" || result.DiscoveredModelCount == nil || *result.DiscoveredModelCount != 0 || result.CheckedAt.Location() != time.UTC {
		t.Fatal(result, err)
	}
	if state.commits != 3 || len(roles.writes) != 0 || len(roles.data.audits) != 0 || !reflect.DeepEqual(state.base.row, beforeConnection) || !reflect.DeepEqual(state.credential, beforeCredential) {
		t.Fatal("diagnostic mutated domain state", state.commits, roles.writes)
	}
	for _, options := range state.readOptions {
		if !options.ReadOnly || options.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
			t.Fatal("unbounded mutable snapshot", options)
		}
	}
}

func TestConnectionDiagnosticSQLRejectsUnsafeAuthorityAndIdentityBeforeNetwork(t *testing.T) {
	for _, name := range []string{"read", "write", "pending_actor", "disabled_actor", "actor_alias", "connection_alias", "provider_alias", "credential_alias", "foreign_credential", "missing_credential", "reborn_credential", "stale_review", "empty_source"} {
		t.Run(name, func(t *testing.T) {
			s, roles, control, state, etag := connectionDiagnosticSQLService(t)
			want := apperrors.ErrNotFound
			switch name {
			case "read", "write":
				roles.deny["providers."+name] = true
				want = apperrors.ErrForbidden
			case "pending_actor":
				roleDefinitionSQLPending(roles, control)
				want = apperrors.ErrUnauthorized
			case "disabled_actor":
				actor := roles.data.users["usr_admin"]
				actor.Disabled = true
				roles.data.users[actor.ID] = actor
				want = apperrors.ErrUnauthorized
			case "actor_alias":
				roles.actorAlias = true
				want = apperrors.ErrUnauthorized
			case "connection_alias":
				state.base.aliasConnection = true
			case "provider_alias":
				state.base.aliasProvider = true
			case "credential_alias":
				state.alias = true
			case "foreign_credential":
				state.credential.ConnectionID = "con_other"
			case "missing_credential":
				state.missing = true
			case "reborn_credential":
				state.credential.CreatedAt = time.Time{}
			case "stale_review":
				state.base.row.ETag = "revised"
				want = catalogConflict
			case "empty_source":
				state.credential.Ciphertext = ""
				want = connectionDiagnosticUnavailable
			}
			calls := 0
			s.upstream.Transport = connectionDiagnosticTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("unexpected outbound use")
			})
			result, err := s.TestConnection(context.Background(), "usr_admin", "con_target", etag, ConnectionDiagnosticInput{CredentialID: "crd_target"})
			if result != nil || err != want || calls != 0 || len(roles.writes) != 0 {
				t.Fatal(name, result, err, calls, roles.writes)
			}
		})
	}
}

func TestConnectionDiagnosticSQLRechecksBeforeDispatchAndResult(t *testing.T) {
	for _, stage := range []string{"before_dispatch", "after_native"} {
		for _, change := range []string{"source", "credential_birth", "actor_birth", "provider_birth", "connection_birth", "transport", "read_authority", "write_authority"} {
			t.Run(stage+"/"+change, func(t *testing.T) {
				s, roles, _, state, etag := connectionDiagnosticSQLService(t)
				mutate := func() {
					switch change {
					case "source":
						state.credential.Ciphertext = "replaced-encrypted-source"
					case "credential_birth":
						state.credential.CreatedAt = state.credential.CreatedAt.Add(time.Microsecond)
					case "actor_birth":
						actor := roles.data.users["usr_admin"]
						actor.CreatedAt = actor.CreatedAt.Add(time.Microsecond)
						roles.data.users[actor.ID] = actor
					case "provider_birth":
						state.base.provider.CreatedAt = state.base.provider.CreatedAt.Add(time.Microsecond)
					case "connection_birth":
						state.base.row.CreatedAt = state.base.row.CreatedAt.Add(time.Microsecond)
					case "transport":
						state.base.row.ETag = "revised-transport"
					case "read_authority":
						roles.deny["providers.read"] = true
					case "write_authority":
						roles.deny["providers.write"] = true
					}
				}
				calls := 0
				if stage == "before_dispatch" {
					state.afterSnapshot = func(n int) {
						if n == 1 {
							mutate()
						}
					}
				}
				s.upstream.Transport = connectionDiagnosticTransport(func(*http.Request) (*http.Response, error) {
					calls++
					if stage == "after_native" {
						mutate()
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[]}`)), Header: http.Header{}}, nil
				})
				result, err := s.TestConnection(context.Background(), "usr_admin", "con_target", etag, ConnectionDiagnosticInput{CredentialID: "crd_target"})
				want := catalogConflict
				if strings.HasSuffix(change, "authority") {
					want = apperrors.ErrForbidden
				}
				wantCalls := 0
				if stage == "after_native" {
					wantCalls = 1
				}
				if result != nil || err != want || calls != wantCalls || len(roles.writes) != 0 {
					t.Fatal(result, err, calls, roles.writes)
				}
			})
		}
	}
}

func TestConnectionDiagnosticSQLFailedDiscoveryAndCancellationRemainPrivate(t *testing.T) {
	for _, name := range []string{"malformed", "upstream_denied", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			s, roles, _, state, etag := connectionDiagnosticSQLService(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s.upstream.Transport = connectionDiagnosticTransport(func(request *http.Request) (*http.Response, error) {
				deadline, ok := request.Context().Deadline()
				if !ok || time.Until(deadline) > 10*time.Second {
					t.Fatal("missing whole-operation bound")
				}
				if name == "cancelled" {
					cancel()
					return nil, context.Canceled
				}
				status := 200
				if name == "upstream_denied" {
					status = 401
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"error":"test-only-sensitive-upstream-body"}`)), Header: http.Header{}}, nil
			})
			result, err := s.TestConnection(ctx, "usr_admin", "con_target", etag, ConnectionDiagnosticInput{CredentialID: "crd_target"})
			if name == "cancelled" {
				if result != nil || err != connectionDiagnosticUnavailable {
					t.Fatal(result, err)
				}
			} else if err != nil || result == nil || result.Outcome != "failed" || result.DiscoveredModelCount != nil {
				t.Fatal(result, err)
			}
			if len(roles.writes) != 0 || state.credential.Enabled || state.credential.VerificationStatus != "pending" {
				t.Fatal("failed test changed verification")
			}
		})
	}
}

func TestConnectionDiagnosticSQLRejectsProxyAliasAndRebirthBeforeDispatch(t *testing.T) {
	for _, scenario := range []string{"alias", "rebirth"} {
		t.Run(scenario, func(t *testing.T) {
			s, roles, _, state, _ := connectionDiagnosticSQLService(t)
			egressID := "egr_target"
			state.base.row.EgressMode, state.base.row.EgressID = "proxy", &egressID
			state.egress = &entity.Egress{ID: egressID, Kind: "https", Host: "127.0.0.1", Port: 9, Enabled: true, ETag: "initial", CreatedAt: state.base.row.CreatedAt}
			s.allowPrivateUpstream, s.allowPrivateEgress = true, true
			review, err := connectionMetadataRecord(roles.data.users["usr_admin"], state.base.provider, state.base.row, true)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "alias" {
				state.egressAlias = true
			} else {
				state.afterSnapshot = func(n int) {
					if n == 1 {
						state.egress.CreatedAt = state.egress.CreatedAt.Add(time.Microsecond)
					}
				}
			}
			// No listener exists at the selected proxy. A correct fresh proof
			// rejects before client dispatch rather than accepting network failure.
			result, err := s.TestConnection(context.Background(), "usr_admin", "con_target", review.ETag, ConnectionDiagnosticInput{CredentialID: "crd_target"})
			want := connectionDiagnosticUnavailable
			if scenario == "rebirth" {
				want = catalogConflict
			}
			if result != nil || err != want || len(roles.writes) != 0 {
				t.Fatal(result, err, roles.writes)
			}
		})
	}
}
