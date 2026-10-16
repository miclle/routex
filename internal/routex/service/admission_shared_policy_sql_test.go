package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type sharedPolicySQLFixture struct {
	auth        *sharedAuthorizationSQLFixture
	keys        []entity.APIKey
	projectKeys []entity.ProjectKey
	collisions  bool
	rows        map[string]entity.ResourceLimit
	calendar    entity.QuotaSetting
	pricing     entity.PricingSetting
	failure     error
	queries     []string
}

type sharedPolicySQLConnector struct{ f *sharedPolicySQLFixture }

func (c sharedPolicySQLConnector) Connect(context.Context) (driver.Conn, error) {
	base := &sharedAuthorizationSQLConnection{rolesSQLConnection: &rolesSQLConnection{f: &rolesSQLFixture{}}, f: c.f.auth}
	return &sharedPolicySQLConnection{sharedAuthorizationSQLConnection: base, f: c.f}, nil
}
func (c sharedPolicySQLConnector) Driver() driver.Driver { return sharedPolicySQLDriver(c) }

type sharedPolicySQLDriver sharedPolicySQLConnector

func (d sharedPolicySQLDriver) Open(string) (driver.Conn, error) {
	return sharedPolicySQLConnector(d).Connect(context.Background())
}

type sharedPolicySQLConnection struct {
	*sharedAuthorizationSQLConnection
	f *sharedPolicySQLFixture
}

func (c *sharedPolicySQLConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f := c.f
	f.queries = append(f.queries, q)
	if f.failure != nil {
		return nil, f.failure
	}
	switch {
	case strings.Contains(q, `FROM "quota_settings"`):
		return sharedAuthorizationSQLTestRows(&f.calendar)
	case strings.Contains(q, `FROM "pricing_settings"`):
		return sharedAuthorizationSQLTestRows(&f.pricing)
	case strings.Contains(q, `FROM "resource_limits"`):
		kind, _ := args[0].Value.(string)
		id, _ := args[1].Value.(string)
		row, exists := f.rows[limitAccount(kind, id)]
		if !exists {
			return effectiveSQLRows([]entity.ResourceLimit{})
		}
		return sharedAuthorizationSQLTestRows(&row)
	case strings.Contains(q, `FROM "api_keys"`) && strings.Contains(q, "ORDER BY id"):
		return effectiveSQLRows(f.keys)
	case strings.Contains(q, `FROM "project_api_keys"`) && strings.Contains(q, "ORDER BY id"):
		return effectiveSQLRows(f.projectKeys)
	case strings.Contains(q, `FROM "api_keys"`) && strings.Contains(q, " IN "):
		if f.collisions {
			return effectiveSQLRows([]entity.APIKey{{ID: strings.ToUpper(f.projectKeys[0].ID)}})
		}
		return effectiveSQLRows([]entity.APIKey{})
	case strings.Contains(q, `FROM "project_api_keys"`) && strings.Contains(q, " IN "):
		if f.collisions {
			return effectiveSQLRows([]entity.ProjectKey{{ID: strings.ToUpper(f.keys[0].ID)}})
		}
		return effectiveSQLRows([]entity.ProjectKey{})
	default:
		return c.sharedAuthorizationSQLConnection.QueryContext(ctx, q, args)
	}
}

func sharedPolicySQLTestFixture(t *testing.T, kind string) (*gorm.DB, *sharedPolicySQLFixture, sharedCapturedAuthorization, []eventqueue.QuotaLimit) {
	t.Helper()
	f := &sharedPolicySQLFixture{auth: &sharedAuthorizationSQLFixture{facts: sharedAuthorizationTestFacts(kind)}, rows: map[string]entity.ResourceLimit{},
		calendar: entity.QuotaSetting{ID: 1, ETag: "qst_current", TimeZone: "UTC"}, pricing: entity.PricingSetting{ID: 1, ETag: "prc_current", PlatformCurrency: "USD"}}
	facts := &f.auth.facts
	if facts.User != nil {
		facts.User.ID = configurationTestIDV1("usr", 1700)
		if facts.PersonalKey != nil {
			facts.PersonalKey.UserID = facts.User.ID
		}
		if facts.Session != nil {
			facts.Session.UserID = facts.User.ID
		}
		if facts.Membership != nil {
			facts.Membership.UserID = facts.User.ID
		}
		for i := range facts.PersonalRows {
			facts.PersonalRows[i].UserID = facts.User.ID
		}
		if facts.PersonalGrants != nil {
			facts.PersonalGrants.Hash = personalGrantHash(facts.PersonalRows)
		}
	}
	rootBirth := sharedAuthorizationTestTime().Add(-45 * time.Minute)
	if facts.PersonalKey != nil {
		rootID := configurationTestIDV1("key", 1701)
		facts.PersonalKey.ID, facts.PersonalKey.CreatedAt, facts.PersonalKey.ReplacesKeyID, facts.PersonalKey.DeliveryMode = configurationTestIDV1("key", 1700), rootBirth.Add(15*time.Minute), &rootID, "manual"
		f.keys = []entity.APIKey{{ID: rootID, UserID: facts.User.ID, Status: entity.KeyRevoked, CreatedAt: rootBirth, LifecycleRevision: configurationTestIDV1("kvr", 1701)}, *facts.PersonalKey}
	}
	if facts.ProjectKey != nil {
		rootID := configurationTestIDV1("pky", 1701)
		facts.ProjectKey.ID, facts.ProjectKey.CreatedAt, facts.ProjectKey.ReplacesKeyID, facts.ProjectKey.DeliveryMode = configurationTestIDV1("pky", 1700), rootBirth.Add(15*time.Minute), &rootID, "manual"
		f.projectKeys = []entity.ProjectKey{{ID: rootID, ProjectID: facts.Project.ID, Status: entity.KeyDisabled, CreatedAt: rootBirth}, *facts.ProjectKey}
		f.auth.managerUser = entity.User{ID: "usr_manager", CreatedAt: facts.Project.CreatedAt}
		f.auth.managers = []entity.ProjectManager{{ID: "pmg_current", ProjectID: facts.Project.ID, UserID: f.auth.managerUser.ID}}
	}
	original, err := newSharedCapturedAuthorization(*facts, sharedAuthorizationTestTime())
	if err != nil {
		t.Fatal(err)
	}
	policies := sharedPolicySQLFixturePolicies(f, original)
	pool := sql.OpenDB(sharedPolicySQLConnector{f})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	return db, f, original, policies
}

func sharedPolicySQLFixturePolicies(f *sharedPolicySQLFixture, a sharedCapturedAuthorization) []eventqueue.QuotaLimit {
	var scopes []sharedAdmissionPolicyScope
	switch a.Kind {
	case "personal_session", "personal_key":
		scopes = []sharedAdmissionPolicyScope{{kind: "user", id: a.User.ID, birth: a.User.CreatedAt}}
		if a.Kind == "personal_key" {
			scopes = append(scopes, sharedAdmissionPolicyScope{kind: "key", id: f.keys[0].ID, birth: f.keys[0].CreatedAt, personalRoot: &f.keys[0]})
		}
	case "project_key":
		scopes = []sharedAdmissionPolicyScope{{kind: "project", id: a.Project.ID, birth: a.Project.CreatedAt}, {kind: "key", id: f.projectKeys[0].ID, birth: f.projectKeys[0].CreatedAt, projectRoot: &f.projectKeys[0]}}
	case "team_session":
		scopes = []sharedAdmissionPolicyScope{{kind: "team", id: a.Team.ID, birth: a.Team.CreatedAt}, {kind: "team_member", id: teamMemberLimitScopeID(a.Team.ID, a.User.ID), birth: a.Team.CreatedAt}}
	}
	output := make([]eventqueue.QuotaLimit, 0, len(scopes))
	for _, scope := range scopes {
		account := limitAccount(scope.kind, scope.id)
		cap, rpm, tpm, concurrent, money := int64(100), int64(12), int64(1000), int64(2), "20.000000000000000001"
		row := entity.ResourceLimit{ScopeKind: scope.kind, ScopeID: scope.id, ETag: "lim_" + scope.kind, TokensMonth: &cap, TokensMonthBehavior: "alert_only", MoneyMonthBehavior: "stop",
			MoneyMonth: &money, Currency: "USD", TPM: &tpm, RPM: &rpm, Concurrency: &concurrent, IPMode: "none", IPRangesJSON: "[]"}
		f.rows[account] = row
		output = append(output, eventqueue.QuotaLimit{Limit: eventqueue.Limit{Account: account, RPM: &rpm, Concurrency: &concurrent}, Revision: row.ETag, CreatedAt: scope.birth,
			PersonalKey: scope.personalRoot != nil, ProjectKey: scope.projectRoot != nil, TeamMember: scope.kind == "team_member",
			TokensMonth: &cap, TokensMonthBehavior: "alert_only", MoneyMonth: &money, Currency: "USD", TPM: &tpm})
	}
	return output
}

func sharedPolicySQLValidate(t *testing.T, db *gorm.DB, a sharedCapturedAuthorization, original []eventqueue.QuotaLimit, ip netip.Addr) error {
	t.Helper()
	return db.Transaction(func(tx *gorm.DB) error {
		if err := validateSharedAuthorizationSQL(tx, a, sharedAuthorizationTestTime()); err != nil {
			return err
		}
		return validateSharedAdmissionPoliciesSQL(tx, a, original, "qst_current", "UTC", ip, sharedAuthorizationTestTime())
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}

func TestSharedAdmissionPoliciesSQLFourPrincipalsAndStoredOnlyDefaults(t *testing.T) {
	for _, kind := range []string{"personal_key", "project_key", "personal_session", "team_session"} {
		t.Run(kind, func(t *testing.T) {
			db, f, a, original := sharedPolicySQLTestFixture(t, kind)
			slices.Reverse(original)
			if err := sharedPolicySQLValidate(t, db, a, original, netip.MustParseAddr("198.51.100.9")); err != nil {
				t.Fatal("current policies with inactive continuity predecessor", err)
			}
			for _, q := range f.queries {
				if strings.Contains(q, "default_limit_rules") {
					t.Fatal("admission dynamically consulted creation defaults")
				}
				for _, table := range []string{"resource_limits", "quota_settings", "pricing_settings"} {
					if strings.Contains(q, `FROM "`+table+`"`) && !strings.Contains(q, "FOR UPDATE") {
						t.Fatal("current policy fact lacked retained SQL lock", q)
					}
				}
			}
		})
	}
}

func TestSharedAdmissionPoliciesSQLRejectsChangedCompleteCapture(t *testing.T) {
	changes := []struct {
		name string
		edit func(*eventqueue.QuotaLimit)
	}{
		{"revision", func(p *eventqueue.QuotaLimit) { p.Revision = "lim_obsolete" }},
		{"account", func(p *eventqueue.QuotaLimit) { p.Account = "user_other" }},
		{"unknown_birth", func(p *eventqueue.QuotaLimit) { p.CreatedAt = time.Time{} }},
		{"birth", func(p *eventqueue.QuotaLimit) { p.CreatedAt = p.CreatedAt.Add(time.Second) }},
		{"rpm", func(p *eventqueue.QuotaLimit) { v := int64(11); p.RPM = &v }},
		{"concurrency", func(p *eventqueue.QuotaLimit) { p.Concurrency = nil }},
		{"rolling_5h", func(p *eventqueue.QuotaLimit) { v := int64(0); p.Tokens5H = &v }},
		{"rolling_7d", func(p *eventqueue.QuotaLimit) { v := int64(1); p.Tokens7D = &v }},
		{"monthly", func(p *eventqueue.QuotaLimit) { p.TokensMonth = nil }},
		{"tpm", func(p *eventqueue.QuotaLimit) { p.TPM = nil }},
		{"money", func(p *eventqueue.QuotaLimit) { v := "20"; p.MoneyMonth = &v }},
		{"currency", func(p *eventqueue.QuotaLimit) { p.Currency = "EUR" }},
		{"token_behavior", func(p *eventqueue.QuotaLimit) { p.TokensMonthBehavior = "" }},
		{"money_behavior", func(p *eventqueue.QuotaLimit) { p.MoneyMonthBehavior = "alert_only" }},
		{"personal_proof", func(p *eventqueue.QuotaLimit) { p.PersonalKey = !p.PersonalKey }},
		{"project_proof", func(p *eventqueue.QuotaLimit) { p.ProjectKey = !p.ProjectKey }},
		{"team_proof", func(p *eventqueue.QuotaLimit) { p.TeamMember = !p.TeamMember }},
	}
	for _, kind := range []string{"personal_key", "project_key", "personal_session", "team_session"} {
		for _, change := range changes {
			t.Run(kind+"/"+change.name, func(t *testing.T) {
				db, _, a, original := sharedPolicySQLTestFixture(t, kind)
				change.edit(&original[len(original)-1])
				if err := sharedPolicySQLValidate(t, db, a, original, netip.MustParseAddr("198.51.100.9")); err == nil {
					t.Fatal("changed complete policy capture accepted")
				}
			})
		}
	}
}

func TestSharedAdmissionPoliciesSQLRejectsMissingDuplicateAndExtraAccounts(t *testing.T) {
	for _, change := range []string{"missing", "duplicate", "extra", "root_substitution"} {
		t.Run(change, func(t *testing.T) {
			db, _, a, original := sharedPolicySQLTestFixture(t, "personal_key")
			switch change {
			case "missing":
				original = original[:1]
			case "duplicate":
				original[1] = original[0]
			case "extra":
				original = append(original, original[0])
			case "root_substitution":
				original[1].Account, original[1].CreatedAt = limitAccount("key", a.Key.ID), a.Key.CreatedAt
			}
			if err := sharedPolicySQLValidate(t, db, a, original, netip.MustParseAddr("198.51.100.9")); err == nil {
				t.Fatal("incorrect independent account set accepted")
			}
		})
	}
}

func TestSharedAdmissionPoliciesSQLCurrentSQLChangesFailClosed(t *testing.T) {
	for _, change := range []string{"calendar_etag", "calendar_zone", "currency", "policy_etag", "policy_alias", "key_owner", "key_birth", "root_birth", "missing_ancestor", "cycle", "collision", "team_ip", "team_member_rolling"} {
		kind := "personal_key"
		if strings.HasPrefix(change, "team_") {
			kind = "team_session"
		}
		t.Run(change, func(t *testing.T) {
			db, f, a, original := sharedPolicySQLTestFixture(t, kind)
			account := original[len(original)-1].Account
			row := f.rows[account]
			switch change {
			case "calendar_etag":
				f.calendar.ETag = "qst_new"
			case "calendar_zone":
				f.calendar.TimeZone = "Etc/UTC"
			case "currency":
				f.pricing.PlatformCurrency = "EUR"
			case "policy_etag":
				row.ETag = "lim_new"
			case "policy_alias":
				row.ScopeID = strings.ToUpper(row.ScopeID)
			case "key_owner":
				f.keys[1].UserID = configurationTestIDV1("usr", 1800)
			case "key_birth":
				f.keys[1].CreatedAt = f.keys[1].CreatedAt.Add(time.Second)
			case "root_birth":
				f.keys[0].CreatedAt = f.keys[0].CreatedAt.Add(time.Second)
			case "missing_ancestor":
				f.keys = f.keys[1:]
			case "cycle":
				f.keys[0].ReplacesKeyID = &f.keys[1].ID
			case "collision":
				f.collisions = true
			case "team_ip":
				row.IPMode, row.IPRangesJSON = "allowlist", `["198.51.100.0/24"]`
			case "team_member_rolling":
				v := int64(1)
				row.Tokens5H = &v
			}
			f.rows[account] = row
			if err := sharedPolicySQLValidate(t, db, a, original, netip.MustParseAddr("198.51.100.9")); err == nil {
				t.Fatal("changed SQL facts accepted original policy")
			}
		})
	}
}

func TestSharedAdmissionPoliciesSQLParentAndRootIPAreConjunctive(t *testing.T) {
	for _, kind := range []string{"personal_key", "project_key", "personal_session"} {
		for _, rejected := range []bool{false, true} {
			t.Run(kind+"/"+map[bool]string{false: "allowed", true: "denied"}[rejected], func(t *testing.T) {
				db, f, a, original := sharedPolicySQLTestFixture(t, kind)
				parent := f.rows[original[0].Account]
				parent.IPMode, parent.IPRangesJSON = "allowlist", `["198.51.100.0/24"]`
				f.rows[original[0].Account] = parent
				ip := netip.MustParseAddr("198.51.100.9")
				if len(original) == 2 {
					root := f.rows[original[1].Account]
					root.IPMode, root.IPRangesJSON = "denylist", `["198.51.100.10/32"]`
					if rejected {
						root.IPRangesJSON = `["198.51.100.9/32"]`
					}
					f.rows[original[1].Account] = root
				} else if rejected {
					ip = netip.MustParseAddr("203.0.113.9")
				}
				if err := sharedPolicySQLValidate(t, db, a, original, ip); (err != nil) != rejected {
					t.Fatal("current independent IP policies", err)
				}
			})
		}
	}
}

func TestSharedAdmissionPoliciesSQLKeyParentIPCannotBeBypassedByRoot(t *testing.T) {
	for _, kind := range []string{"personal_key", "project_key"} {
		t.Run(kind, func(t *testing.T) {
			db, f, a, original := sharedPolicySQLTestFixture(t, kind)
			parent := f.rows[original[0].Account]
			parent.IPMode, parent.IPRangesJSON = "allowlist", `["198.51.100.0/24"]`
			f.rows[original[0].Account] = parent
			// The root is unrestricted. Parent refusal must remain conjunctive.
			if err := sharedPolicySQLValidate(t, db, a, original, netip.MustParseAddr("203.0.113.9")); err == nil {
				t.Fatal("unrestricted Key root bypassed parent IP policy")
			}
		})
	}
}

func TestSharedAdmissionPoliciesSQLAbsentAndExplicitZeroStayDistinct(t *testing.T) {
	for _, zero := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "explicit_zero"}[zero], func(t *testing.T) {
			db, f, a, original := sharedPolicySQLTestFixture(t, "personal_session")
			account := original[0].Account
			original[0] = eventqueue.QuotaLimit{Limit: eventqueue.Limit{Account: account}, Revision: "0", CreatedAt: a.User.CreatedAt}
			delete(f.rows, account)
			if zero {
				v := int64(0)
				f.rows[account] = entity.ResourceLimit{ScopeKind: "user", ScopeID: a.User.ID, ETag: "lim_zero", TokensMonth: &v, IPMode: "none", IPRangesJSON: "[]"}
				original[0].Revision, original[0].TokensMonth = "lim_zero", &v
			}
			if err := sharedPolicySQLValidate(t, db, a, original, netip.MustParseAddr("198.51.100.9")); err != nil {
				t.Fatal("stored absent/zero fact", err)
			}
			if zero {
				original[0].TokensMonth = nil
				if err := sharedPolicySQLValidate(t, db, a, original, netip.MustParseAddr("198.51.100.9")); err == nil {
					t.Fatal("explicit zero replaced by unknown/unset")
				}
			}
		})
	}
}

func TestSharedAdmissionPoliciesSQLTeamMembershipReplacementKeepsPairAccount(t *testing.T) {
	db, f, a, original := sharedPolicySQLTestFixture(t, "team_session")
	f.auth.facts.Membership.ID = "tmb_replacement"
	joined := sharedAuthorizationTestTime().Add(-10 * time.Minute)
	f.auth.facts.Membership.JoinedAt = &joined
	replacement, err := newSharedCapturedAuthorization(f.auth.facts, sharedAuthorizationTestTime())
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Team.MembershipID == a.Team.MembershipID || original[1].Account != teamMemberLimitAccount(a.Team.ID, a.User.ID) {
		t.Fatal("replacement fixture or complete stable pair account")
	}
	if err := sharedPolicySQLValidate(t, db, replacement, original, netip.MustParseAddr("198.51.100.9")); err != nil {
		t.Fatal("replacement membership reset stable accounting birth", err)
	}
	for _, q := range f.queries {
		if strings.Contains(q, "user_model_grants") || strings.Contains(q, `FROM "api_keys"`) {
			t.Fatal("Team policy review borrowed a Personal grant or Key")
		}
	}
}

func TestSharedAdmissionPoliciesSQLRetainedGraphsAreBoundedAndExact(t *testing.T) {
	for _, kind := range []string{"personal_key", "project_key"} {
		for _, change := range []string{"overflow", "owner_alias", "current_alias", "current_birth", "root_birth", "collision"} {
			t.Run(kind+"/"+change, func(t *testing.T) {
				db, f, a, original := sharedPolicySQLTestFixture(t, kind)
				if kind == "personal_key" {
					switch change {
					case "overflow":
						f.keys = make([]entity.APIKey, personalKeyWarningChainLimit+1)
					case "owner_alias":
						f.keys[1].UserID = strings.ToUpper(f.keys[1].UserID)
					case "current_alias":
						f.keys[1].ID = strings.ToUpper(f.keys[1].ID)
					case "current_birth":
						f.keys[1].CreatedAt = f.keys[1].CreatedAt.Add(time.Second)
					case "root_birth":
						f.keys[0].CreatedAt = f.keys[0].CreatedAt.Add(time.Second)
					case "collision":
						f.collisions = true
					}
				} else {
					switch change {
					case "overflow":
						f.projectKeys = make([]entity.ProjectKey, projectKeyWarningChainLimit+1)
					case "owner_alias":
						f.projectKeys[1].ProjectID = strings.ToUpper(f.projectKeys[1].ProjectID)
					case "current_alias":
						f.projectKeys[1].ID = strings.ToUpper(f.projectKeys[1].ID)
					case "current_birth":
						f.projectKeys[1].CreatedAt = f.projectKeys[1].CreatedAt.Add(time.Second)
					case "root_birth":
						f.projectKeys[0].CreatedAt = f.projectKeys[0].CreatedAt.Add(time.Second)
					case "collision":
						f.collisions = true
					}
				}
				if err := sharedPolicySQLValidate(t, db, a, original, netip.MustParseAddr("198.51.100.9")); err == nil {
					t.Fatal("unbounded or nonexact retained graph accepted")
				}
			})
		}
	}
}

func TestSharedAdmissionPoliciesSQLRequiresTransactionCanonicalIPAndPropagatesErrors(t *testing.T) {
	db, f, a, original := sharedPolicySQLTestFixture(t, "personal_session")
	ip := netip.MustParseAddr("198.51.100.9")
	if err := validateSharedAdmissionPoliciesSQL(db, a, original, "qst_current", "UTC", ip, sharedAuthorizationTestTime()); err == nil || len(f.queries) != 0 {
		t.Fatal("base DB reached current policy reads")
	}
	for _, invalid := range []netip.Addr{{}, netip.MustParseAddr("::ffff:198.51.100.9"), netip.MustParseAddr("fe80::1%en0")} {
		if err := sharedPolicySQLValidate(t, db, a, original, invalid); err == nil {
			t.Fatal("noncanonical live request IP accepted")
		}
	}
	fault := errors.New("controlled policy SQL failure")
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := validateSharedAuthorizationSQL(tx, a, sharedAuthorizationTestTime()); err != nil {
			return err
		}
		f.failure = fault
		return validateSharedAdmissionPoliciesSQL(tx, a, original, "qst_current", "UTC", ip, sharedAuthorizationTestTime())
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if !errors.Is(err, fault) {
		t.Fatal("SQL outage became policy absence", err)
	}
}
