package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
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

type archiveMetadataSQLState struct {
	rows    []entity.RuntimeConfigurationArchive
	queries []string
	fail    bool
	onRead  func()
}
type archiveMetadataSQLConnector struct {
	fixture *rolesSQLFixture
	control *roleDefinitionSQLControl
	state   *archiveMetadataSQLState
}

func (c archiveMetadataSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &archiveMetadataSQLConnection{&roleDefinitionSQLConnection{rolesSQLConnection: &rolesSQLConnection{f: c.fixture}, control: c.control}, c.state}, nil
}
func (c archiveMetadataSQLConnector) Driver() driver.Driver { return archiveMetadataSQLDriver(c) }

type archiveMetadataSQLDriver archiveMetadataSQLConnector

func (c archiveMetadataSQLDriver) Open(string) (driver.Conn, error) {
	return archiveMetadataSQLConnector(c).Connect(context.Background())
}

type archiveMetadataSQLConnection struct {
	*roleDefinitionSQLConnection
	state *archiveMetadataSQLState
}

func (c *archiveMetadataSQLConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if strings.Contains(q, `FROM "runtime_configuration_archives"`) {
		c.state.queries = append(c.state.queries, q)
		if c.state.fail {
			return nil, errors.New("private archive SQL diagnostic")
		}
		if c.state.onRead != nil {
			c.state.onRead()
		}
		rows := []entity.RuntimeConfigurationArchive{}
		last := ""
		for _, arg := range args {
			if s, ok := arg.Value.(string); ok && strings.HasPrefix(s, "rcv_") {
				last = s
			}
		}
		for _, row := range c.state.rows {
			if last == "" || row.ID < last {
				rows = append(rows, row)
			}
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].ID > rows[j].ID })
		bound := regexp.MustCompile(`LIMIT (\d+)`).FindStringSubmatch(q)
		if len(bound) == 2 {
			n, _ := strconv.Atoi(bound[1])
			if len(rows) > n {
				rows = rows[:n]
			}
		}
		return effectiveSQLRows(rows)
	}
	return c.roleDefinitionSQLConnection.QueryContext(ctx, q, args)
}
func archiveMetadataSQLService(t *testing.T) (*Service, *rolesSQLFixture, *archiveMetadataSQLState) {
	t.Helper()
	_, fixture, control := roleDefinitionSQLService(t)
	state := &archiveMetadataSQLState{}
	pool := sql.OpenDB(archiveMetadataSQLConnector{fixture, control, state})
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	return &Service{db: db}, fixture, state
}
func archiveMetadataHeaderFixture(t *testing.T, n int) entity.RuntimeConfigurationArchive {
	t.Helper()
	document := configurationFixtureV1(t)
	raw, digest, err := encodeRuntimeConfigurationV1(t.Context(), document)
	if err != nil {
		t.Fatal(err)
	}
	return runtimeArchiveHeaderV1(configurationTestIDV1("rcv", n), time.Now().UTC().Truncate(time.Microsecond), document, len(raw), digest)
}
func TestRuntimeArchiveMetadataBoundedHeadersAndAuthority(t *testing.T) {
	s, f, state := archiveMetadataSQLService(t)
	first, second := archiveMetadataHeaderFixture(t, 1), archiveMetadataHeaderFixture(t, 2)
	state.rows = []entity.RuntimeConfigurationArchive{first, second}
	page, err := s.ListRuntimeArchiveMetadata(t.Context(), "usr_admin", RuntimeArchiveMetadataFilter{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != second.ID || page.NextCursor == nil || page.Scope != "archive_metadata_only" {
		t.Fatal(page, err)
	}
	next, err := s.ListRuntimeArchiveMetadata(t.Context(), "usr_admin", RuntimeArchiveMetadataFilter{Limit: 1, Cursor: *page.NextCursor})
	if err != nil || len(next.Items) != 1 || next.Items[0].ID != first.ID || next.NextCursor != nil {
		t.Fatal(next, err)
	}
	if _, err = s.ListRuntimeArchiveMetadata(t.Context(), "usr_member", RuntimeArchiveMetadataFilter{Cursor: *page.NextCursor}); !errors.Is(err, apperrors.ErrBadRequest) {
		t.Fatal("borrowed actor cursor", err)
	}
	f.deny["system.read"] = true
	reads := len(state.queries)
	if _, err = s.ListRuntimeArchiveMetadata(t.Context(), "usr_admin", RuntimeArchiveMetadataFilter{}); !errors.Is(err, apperrors.ErrForbidden) || len(state.queries) != reads {
		t.Fatal("unauthorized metadata read", err)
	}
	f.deny["system.read"] = false
	f.actorAlias = true
	if _, err = s.ListRuntimeArchiveMetadata(t.Context(), "usr_admin", RuntimeArchiveMetadataFilter{}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("actor alias", err)
	}
	f.actorAlias = false
	for _, q := range state.queries {
		if strings.Contains(q, "chunks") || strings.Contains(q, "auth_ciphertext") {
			t.Fatal("secret query", q)
		}
	}
	raw, _ := json.Marshal(page)
	for _, private := range []string{"source_digest", "authorization_digest", "network_digest", "pricing_digest", "chunk_count", "auth_ciphertext", "snapshot_id", "applied"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("private or overclaimed metadata", private)
		}
	}
	if len(f.writes) != 0 {
		t.Fatal("read wrote state")
	}
	for _, tx := range f.transactions {
		if !tx.ReadOnly || tx.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
			t.Fatal("not repeatable-read read-only", tx)
		}
	}
}
func TestRuntimeArchiveMetadataFailuresAndEmpty(t *testing.T) {
	for _, name := range []string{"empty", "bad_header", "database_failure", "cancelled", "cancel_after_read", "disabled"} {
		t.Run(name, func(t *testing.T) {
			s, f, state := archiveMetadataSQLService(t)
			ctx := t.Context()
			switch name {
			case "bad_header":
				h := archiveMetadataHeaderFixture(t, 1)
				h.ContentDigest = "invalid"
				state.rows = []entity.RuntimeConfigurationArchive{h}
			case "database_failure":
				state.fail = true
			case "cancelled", "cancel_after_read":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				if name == "cancelled" {
					cancel()
				} else {
					state.onRead = cancel
				}
			case "disabled":
				u := f.data.users["usr_admin"]
				u.Disabled = true
				f.data.users[u.ID] = u
			}
			page, err := s.ListRuntimeArchiveMetadata(ctx, "usr_admin", RuntimeArchiveMetadataFilter{})
			if name == "empty" {
				if err != nil || page == nil || page.Items == nil || len(page.Items) != 0 || page.NextCursor != nil {
					t.Fatal(page, err)
				}
				return
			}
			if err == nil || page != nil || strings.Contains(err.Error(), "private archive SQL") {
				t.Fatal("unsafe failure", page, err)
			}
		})
	}
}
func TestRuntimeArchiveMetadataCursorStrict(t *testing.T) {
	for _, f := range []RuntimeArchiveMetadataFilter{{Limit: -1}, {Limit: 101}, {Cursor: "invalid"}, {Cursor: strings.Repeat("a", 201)}, {Cursor: runtimeArchiveMetadataCursor("usr_admin", "cfg_00000000000000000000000001")}} {
		if _, _, err := validateRuntimeArchiveMetadataFilter("usr_admin", f); err == nil {
			t.Fatal(f)
		}
	}
}
