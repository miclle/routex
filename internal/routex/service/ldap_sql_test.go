package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type ldapSQLFixture struct {
	base          *rolesSQLFixture
	provider      entity.LDAPProvider
	binding       entity.LDAPBinding
	missing, fail bool
}
type ldapSQLConnector struct{ f *ldapSQLFixture }

func (c ldapSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &ldapSQLConnection{rolesSQLConnection: &rolesSQLConnection{f: c.f.base}, f: c.f}, nil
}
func (c ldapSQLConnector) Driver() driver.Driver { return ldapSQLDriver(c) }

type ldapSQLDriver ldapSQLConnector

func (d ldapSQLDriver) Open(string) (driver.Conn, error) {
	return ldapSQLConnector(d).Connect(context.Background())
}

type ldapSQLConnection struct {
	*rolesSQLConnection
	f *ldapSQLFixture
}

func (c *ldapSQLConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	switch {
	case strings.Contains(q, "SELECT DISTINCT p.permission"):
		c.f.base.queries = append(c.f.base.queries, q)
		rows := []struct{ Permission string }{}
		if !c.f.base.deny["registration.write"] {
			rows = append(rows, struct{ Permission string }{"registration.write"})
		}
		return effectiveSQLRows(rows)
	case strings.Contains(q, `FROM "ldap_providers"`):
		c.f.base.queries = append(c.f.base.queries, q)
		if c.f.fail {
			return nil, errors.New("controlled database failure")
		}
		return effectiveSQLRows([]entity.LDAPProvider{c.f.provider})
	case strings.Contains(q, `FROM "ldap_bindings"`):
		c.f.base.queries = append(c.f.base.queries, q)
		if c.f.missing {
			return effectiveSQLRows([]entity.LDAPBinding{})
		}
		return effectiveSQLRows([]entity.LDAPBinding{c.f.binding})
	case strings.Contains(q, `FROM "user_mfa"`):
		return effectiveSQLRows([]entity.UserMFA{})
	}
	return c.rolesSQLConnection.QueryContext(ctx, q, args)
}
func ldapSQLService(t *testing.T) (*Service, *ldapSQLFixture, entity.Session) {
	t.Helper()
	_, base := roleSQLService(t, 0)
	birth := base.data.users["usr_target"].CreatedAt
	admin := base.data.users["usr_admin"].CreatedAt
	f := &ldapSQLFixture{base: base}
	f.provider = entity.LDAPProvider{ID: "ldap", CreatedAt: birth, Name: "Directory", Endpoint: "ldaps://directory.example:636", BindDN: "cn=service,dc=example", BaseDN: "dc=example", UserFilter: "(uid={username})", IdentityAttribute: "entryUUID", AuthCiphertext: "encrypted", SecretGeneration: strings.Repeat("d", 64), ReviewRevision: strings.Repeat("a", 64), ConfigRevision: strings.Repeat("b", 64), PolicyRevision: strings.Repeat("c", 64), Enabled: true, VerifiedConfigRevision: strings.Repeat("b", 64), VerifiedBy: "usr_admin", VerifiedUserCreatedAt: &admin, VerifiedBindingID: "ldb_admin", VerifiedBindingCreatedAt: &admin}
	subject := base64.StdEncoding.EncodeToString([]byte("12345678-1234-4321-abcd-123456789abc"))
	f.binding = entity.LDAPBinding{ID: "ldb_target", ProviderID: "ldap", IdentityAttribute: "entryUUID", CreatedAt: birth, UserID: "usr_target", UserCreatedAt: birth, ConfigRevision: f.provider.ConfigRevision, Subject: subject, SubjectDigest: ldapSubjectDigest("entryUUID", subject)}
	pool := sql.OpenDB(ldapSQLConnector{f})
	t.Cleanup(func() { _ = pool.Close() })
	db, e := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	row := entity.Session{ID: "ses_target", UserID: "usr_target", CreatedAt: birth, PrimaryMethod: "ldap", LDAPBindingID: f.binding.ID, LDAPBindingCreatedAt: &birth, LDAPConfigRevision: f.provider.ConfigRevision, LDAPPolicyRevision: f.provider.PolicyRevision, LDAPUserCreatedAt: &birth}
	return &Service{db: db}, f, row
}
func TestLDAPSQLCurrentPrimaryAndExactBirthPolicy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ldapSQLFixture, *entity.Session)
	}{
		{"disabled", func(f *ldapSQLFixture, _ *entity.Session) { f.provider.Enabled = false }}, {"policy", func(f *ldapSQLFixture, _ *entity.Session) { f.provider.PolicyRevision = strings.Repeat("e", 64) }}, {"config", func(f *ldapSQLFixture, _ *entity.Session) { f.provider.ConfigRevision = strings.Repeat("e", 64) }}, {"missing", func(f *ldapSQLFixture, _ *entity.Session) { f.missing = true }}, {"reborn", func(f *ldapSQLFixture, _ *entity.Session) {
			f.binding.CreatedAt = f.binding.CreatedAt.Add(time.Microsecond)
		}}, {"owner_alias", func(f *ldapSQLFixture, _ *entity.Session) { f.binding.UserID = "USR_TARGET" }}, {"attribute_alias", func(f *ldapSQLFixture, _ *entity.Session) { f.binding.IdentityAttribute = "entryuuid" }}, {"subject_change", func(f *ldapSQLFixture, _ *entity.Session) {
			f.binding.Subject = base64.StdEncoding.EncodeToString([]byte("12345678-1234-4321-ABCD-123456789ABC"))
		}}, {"mixed", func(_ *ldapSQLFixture, r *entity.Session) { r.OAuthBindingID = "oab_other" }}, {"user_reborn", func(f *ldapSQLFixture, _ *entity.Session) {
			u := f.base.data.users["usr_target"]
			u.CreatedAt = u.CreatedAt.Add(time.Microsecond)
			f.base.data.users[u.ID] = u
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f, r := ldapSQLService(t)
			if e := primaryValidateSession(s.authDB(context.Background()), r); e != nil {
				t.Fatal("exact positive", e)
			}
			tc.change(f, &r)
			if primaryValidateSession(s.authDB(context.Background()), r) == nil {
				t.Fatal("obsolete primary")
			}
			if len(f.base.writes) != 0 {
				t.Fatal("authorization wrote")
			}
		})
	}
}
func TestLDAPSQLConfigIndependentPermissionAndIntrinsicAdmin(t *testing.T) {
	for _, mode := range []string{"permission", "member", "outage", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			s, f, _ := ldapSQLService(t)
			if _, e := s.GetLDAPProvider(context.Background(), "usr_admin"); e != nil {
				t.Fatal("positive", e)
			}
			f.base.queries = nil
			ctx := context.Background()
			switch mode {
			case "permission":
				f.base.deny["registration.write"] = true
			case "member":
				u := f.base.data.users["usr_admin"]
				u.Role = entity.RoleMember
				f.base.data.users[u.ID] = u
			case "outage":
				f.fail = true
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			v, e := s.GetLDAPProvider(ctx, "usr_admin")
			if e == nil || v != nil {
				t.Fatal("private config survived")
			}
			if mode == "permission" || mode == "member" {
				if !errors.Is(e, apperrors.ErrForbidden) {
					t.Fatal("authority", e)
				}
				for _, q := range f.base.queries {
					if strings.Contains(q, `FROM "ldap_providers"`) {
						t.Fatal("config read after denial")
					}
				}
			}
		})
	}
}
