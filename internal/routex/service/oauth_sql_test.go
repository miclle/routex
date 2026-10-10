package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This adapter adds only read-side OAuth rows to the existing exact identity
// fixture. Real database/migration/protocol acceptance remains independently required.
type oauthSQLFixture struct {
	base           *rolesSQLFixture
	provider       entity.OAuthProvider
	binding        entity.OAuthBinding
	missingBinding bool
	failProvider   bool
	ceremonies     []entity.OAuthCeremony
	failCeremony   bool
	ceremonyLimits []int
}
type oauthSQLConnector struct{ f *oauthSQLFixture }

func (c oauthSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &oauthSQLConnection{rolesSQLConnection: &rolesSQLConnection{f: c.f.base}, f: c.f}, nil
}
func (c oauthSQLConnector) Driver() driver.Driver { return oauthSQLDriver(c) }

type oauthSQLDriver oauthSQLConnector

func (d oauthSQLDriver) Open(string) (driver.Conn, error) {
	return oauthSQLConnector(d).Connect(context.Background())
}

type oauthSQLConnection struct {
	*rolesSQLConnection
	f *oauthSQLFixture
}

func (c *oauthSQLConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
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
	case strings.Contains(q, `FROM "oauth_ceremonies"`):
		c.f.base.queries = append(c.f.base.queries, q)
		c.f.ceremonyLimits = append(c.f.ceremonyLimits, rolesSQLLimit(args))
		if c.f.failCeremony {
			return nil, errors.New("controlled ceremony outage")
		}
		var now time.Time
		for _, a := range args {
			if v, ok := a.Value.(time.Time); ok {
				now = v
				break
			}
		}
		rows := []entity.OAuthCeremony{}
		for _, row := range c.f.ceremonies {
			if strings.Contains(q, "expires_at <=") && !row.ExpiresAt.After(now) || strings.Contains(q, "expires_at >") && row.ExpiresAt.After(now) {
				rows = append(rows, row)
			}
		}
		slices.SortFunc(rows, func(a, b entity.OAuthCeremony) int { return a.ExpiresAt.Compare(b.ExpiresAt) })
		return effectiveSQLRows(rows[:min(len(rows), rolesSQLLimit(args))])
	case strings.Contains(q, `FROM "oauth_providers"`):
		c.f.base.queries = append(c.f.base.queries, q)
		if c.f.failProvider {
			return nil, errors.New("controlled provider outage")
		}
		return effectiveSQLRows([]entity.OAuthProvider{c.f.provider})
	case strings.Contains(q, `FROM "oauth_bindings"`):
		c.f.base.queries = append(c.f.base.queries, q)
		if c.f.missingBinding {
			return effectiveSQLRows([]entity.OAuthBinding{})
		}
		return effectiveSQLRows([]entity.OAuthBinding{c.f.binding})
	case strings.Contains(q, `FROM "user_mfa"`):
		c.f.base.queries = append(c.f.base.queries, q)
		return effectiveSQLRows([]entity.UserMFA{})
	}
	return c.rolesSQLConnection.QueryContext(ctx, q, args)
}
func oauthSQLService(t *testing.T) (*Service, *oauthSQLFixture, entity.Session) {
	t.Helper()
	_, base := roleSQLService(t, 0)
	birth := base.data.users["usr_target"].CreatedAt
	adminBirth := base.data.users["usr_admin"].CreatedAt
	f := &oauthSQLFixture{base: base}
	f.provider = entity.OAuthProvider{ID: "oauth", CreatedAt: birth, Name: "Corporate", AuthorizationURL: "https://identity.example/authorize", TokenURL: "https://identity.example/token", UserInfoURL: "https://identity.example/profile", ClientAuthMethod: "client_secret_basic", ScopesJSON: "[]", SubjectPathJSON: `["id"]`, AuthCiphertext: "fixture-ciphertext", ClientID: "routex", CallbackURL: "https://routex.example/api/v1/auth/oauth/callback", ReviewRevision: strings.Repeat("a", 64), ConfigRevision: strings.Repeat("b", 64), PolicyRevision: strings.Repeat("c", 64), Enabled: true, VerifiedConfigRevision: strings.Repeat("b", 64), VerifiedBy: "usr_admin", VerifiedUserCreatedAt: &adminBirth, VerifiedBindingID: "oab_admin", VerifiedBindingCreatedAt: &adminBirth}
	f.binding = entity.OAuthBinding{ID: "oab_target", ProviderID: "oauth", SubjectKind: "string", CreatedAt: birth, UserID: "usr_target", UserCreatedAt: birth, ConfigRevision: f.provider.ConfigRevision, Subject: "CaseSensitiveSubject", SubjectDigest: oauthSubjectDigest("oauth", "string", "CaseSensitiveSubject")}
	pool := sql.OpenDB(oauthSQLConnector{f})
	t.Cleanup(func() { _ = pool.Close() })
	db, e := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	row := entity.Session{ID: "ses_target", UserID: "usr_target", CreatedAt: birth, PrimaryMethod: "oauth", OAuthBindingID: f.binding.ID, OAuthBindingCreatedAt: &birth, OAuthConfigRevision: f.provider.ConfigRevision, OAuthPolicyRevision: f.provider.PolicyRevision, OAuthUserCreatedAt: &birth}
	return &Service{db: db}, f, row
}
func TestOAuthSQLPrimaryRequiresCurrentExactBindingPolicyAndAdmission(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*oauthSQLFixture, *entity.Session)
	}{
		{"disabled", func(f *oauthSQLFixture, _ *entity.Session) { f.provider.Enabled = false }},
		{"policy_revised", func(f *oauthSQLFixture, _ *entity.Session) { f.provider.PolicyRevision = strings.Repeat("d", 64) }},
		{"config_revised", func(f *oauthSQLFixture, _ *entity.Session) { f.provider.ConfigRevision = strings.Repeat("e", 64) }},
		{"binding_deleted", func(f *oauthSQLFixture, _ *entity.Session) { f.missingBinding = true }},
		{"binding_reborn", func(f *oauthSQLFixture, _ *entity.Session) {
			f.binding.CreatedAt = f.binding.CreatedAt.Add(time.Microsecond)
		}},
		{"binding_id_alias", func(f *oauthSQLFixture, _ *entity.Session) { f.binding.ID = "OAB_TARGET" }},
		{"binding_owner_alias", func(f *oauthSQLFixture, _ *entity.Session) { f.binding.UserID = "USR_TARGET" }},
		{"typed_subject_changed", func(f *oauthSQLFixture, _ *entity.Session) { f.binding.SubjectKind = "integer" }},
		{"provider_namespace_alias", func(f *oauthSQLFixture, _ *entity.Session) { f.binding.ProviderID = "OAuth" }},
		{"mixed_oidc_marker", func(_ *oauthSQLFixture, r *entity.Session) { r.OIDCBindingID = "oib_other" }},
		{"subject_alias", func(f *oauthSQLFixture, _ *entity.Session) { f.binding.Subject = "casesensitivesubject" }},
		{"user_reborn", func(f *oauthSQLFixture, _ *entity.Session) {
			u := f.base.data.users["usr_target"]
			u.CreatedAt = u.CreatedAt.Add(time.Microsecond)
			f.base.data.users[u.ID] = u
		}},
		{"user_disabled", func(f *oauthSQLFixture, _ *entity.Session) {
			u := f.base.data.users["usr_target"]
			u.Disabled = true
			f.base.data.users[u.ID] = u
		}},
		{"provider_id_alias", func(f *oauthSQLFixture, _ *entity.Session) { f.provider.ID = "OAuth" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, f, row := oauthSQLService(t)
			if e := s.oauthValidatePrimary(s.authDB(context.Background()), row); e != nil {
				t.Fatal("successful exact baseline required", e)
			}
			tc.mutate(f, &row)
			if e := s.oauthValidatePrimary(s.authDB(context.Background()), row); e == nil {
				t.Fatal("obsolete or aliased primary accepted")
			}
			if len(f.base.writes) != 0 {
				t.Fatal("read-side validation wrote domain facts")
			}
		})
	}
}
func TestOAuthSQLConfigReadRequiresIntrinsicAdminAndPermission(t *testing.T) {
	for _, mode := range []string{"allowed", "permission_denied", "non_admin", "outage", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			s, f, _ := oauthSQLService(t)
			view, e := s.GetOAuthProvider(context.Background(), "usr_admin")
			if e != nil || view == nil || !view.Enabled || !validMemberRoleDigest(view.ReviewETag) {
				t.Fatal("authorized baseline", e)
			}
			f.base.queries = nil
			ctx := context.Background()
			switch mode {
			case "permission_denied":
				f.base.deny["registration.write"] = true
			case "non_admin":
				u := f.base.data.users["usr_admin"]
				u.Role = entity.RoleMember
				f.base.data.users[u.ID] = u
			case "outage":
				f.failProvider = true
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			view, e = s.GetOAuthProvider(ctx, "usr_admin")
			if mode == "allowed" {
				if e != nil || view == nil {
					t.Fatal(e)
				}
			} else if e == nil || view != nil {
				t.Fatal("private config survived denial/outage")
			}
			if mode == "permission_denied" || mode == "non_admin" {
				if !errors.Is(e, apperrors.ErrForbidden) {
					t.Fatal("authority", e)
				}
				for _, q := range f.base.queries {
					if strings.Contains(q, `FROM "oauth_providers"`) {
						t.Fatal("private configuration read after denial")
					}
				}
			}
			if len(f.base.writes) != 0 {
				t.Fatal("config GET wrote")
			}
		})
	}
}

func (c *oauthSQLConnection) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, `DELETE FROM "oauth_ceremonies"`) {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		c.f.base.writes = append(c.f.base.writes, q)
		values := rolesSQLStrings(args)
		if len(values) == 0 {
			return nil, errors.New("unscoped ceremony delete")
		}
		var now time.Time
		for _, a := range args {
			if v, ok := a.Value.(time.Time); ok {
				now = v
			}
		}
		kept := []entity.OAuthCeremony{}
		var deleted int64
		for _, row := range c.f.ceremonies {
			if row.ID == values[0] && !row.ExpiresAt.After(now) {
				deleted++
			} else {
				kept = append(kept, row)
			}
		}
		c.f.ceremonies = kept
		return driver.RowsAffected(deleted), nil
	}
	return c.rolesSQLConnection.ExecContext(ctx, q, args)
}
func TestOAuthSQLCeremonyAdmissionBoundsExpiryAndTerminalRows(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	for _, n := range []int{0, oauthCeremonyLiveLimit - 1, oauthCeremonyLiveLimit, oauthCeremonyLiveLimit + 1} {
		t.Run(fmt.Sprintf("retained_%d", n), func(t *testing.T) {
			s, f, _ := oauthSQLService(t)
			for i := range n {
				f.ceremonies = append(f.ceremonies, entity.OAuthCeremony{ID: fmt.Sprintf("oac_%04d", i), Status: "failed", ExpiresAt: now.Add(time.Minute)})
			}
			e := oauthAdmitCeremony(s.authDB(context.Background()), now)
			if (e == nil) != (n < oauthCeremonyLiveLimit) {
				t.Fatal("unexpired terminal rows bypassed finite cap", e)
			}
			if len(f.base.writes) != 0 {
				t.Fatal("live ceremony deleted")
			}
			if len(f.base.queries) != 2 {
				t.Fatal("unexpected admission reads")
			}
			if len(f.ceremonyLimits) != 2 || f.ceremonyLimits[0] != oauthCeremonyPruneLimit || f.ceremonyLimits[1] != oauthCeremonyLiveLimit {
				t.Fatal("parameterized query limits changed", f.ceremonyLimits)
			}
			for _, q := range f.base.queries {
				if !strings.Contains(q, "LIMIT") || !strings.Contains(q, "expires_at") {
					t.Fatal("missing bounded indexed expiry predicate", q)
				}
			}
		})
	}
	s, f, _ := oauthSQLService(t)
	for i := range oauthCeremonyPruneLimit + 1 {
		f.ceremonies = append(f.ceremonies, entity.OAuthCeremony{ID: fmt.Sprintf("oac_expired_%04d", i), Status: "pending", ExpiresAt: now})
	}
	live := entity.OAuthCeremony{ID: "oac_live", Status: "verified", ExpiresAt: now.Add(time.Minute)}
	f.ceremonies = append(f.ceremonies, live)
	if e := oauthAdmitCeremony(s.authDB(context.Background()), now); e != nil {
		t.Fatal(e)
	}
	if len(f.base.writes) != oauthCeremonyPruneLimit || len(f.ceremonies) != 2 {
		t.Fatal("cleanup exceeded one bounded batch or removed current proof")
	}
	for _, q := range f.base.writes {
		if !strings.Contains(q, "expires_at <=") || !strings.Contains(q, "id") {
			t.Fatal("delete lost exact identity/expiry fence")
		}
	}
	if f.ceremonies[1].ID != live.ID || f.ceremonies[0].Status != "pending" {
		t.Fatal("cleanup changed retained ceremony state")
	}
	for _, mode := range []string{"outage", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			s, f, _ := oauthSQLService(t)
			ctx := context.Background()
			if mode == "outage" {
				f.failCeremony = true
			} else {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if e := oauthAdmitCeremony(s.authDB(ctx), now); e == nil {
				t.Fatal("unknown capacity admitted ceremony")
			}
			if len(f.base.writes) != 0 {
				t.Fatal("failed capacity read mutated ceremonies")
			}
		})
	}
}

func TestOAuthSQLTypedAuditIsAtomicAndReasonBound(t *testing.T) {
	for _, mode := range []string{"committed", "rollback", "audit_outage", "invalid_reason"} {
		t.Run(mode, func(t *testing.T) {
			s, f, _ := oauthSQLService(t)
			u := f.base.data.users["usr_target"]
			reason := "Link approved corporate identity"
			if mode == "invalid_reason" {
				reason = ""
			}
			if mode == "audit_outage" {
				f.base.failAudit = true
			}
			sentinel := errors.New("controlled caller rollback")
			e := s.authDB(context.Background()).Transaction(func(tx *gorm.DB) error {
				if e := oauthAudit(tx, u, "account.oauth.bind", f.provider, &f.binding, reason); e != nil {
					return e
				}
				if mode == "rollback" {
					return sentinel
				}
				return nil
			})
			if mode == "committed" {
				if e != nil || len(f.base.data.audits) != 1 {
					t.Fatal("typed audit missing", e)
				}
				a := f.base.data.audits[0]
				if a.Action != "account.oauth.bind" || a.ResourceID != f.binding.ID || a.ActorID != u.ID || a.DetailsJSON == nil || !strings.Contains(*a.DetailsJSON, reason) {
					t.Fatal("audit identity/reason lost")
				}
			} else {
				if e == nil || len(f.base.data.audits) != 0 {
					t.Fatal("audit failure escaped caller transaction", e)
				}
			}
			if mode == "invalid_reason" && len(f.base.writes) != 0 {
				t.Fatal("invalid reason wrote an event")
			}
		})
	}
}
