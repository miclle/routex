package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type domainPolicySQLFixture struct {
	*rolesSQLFixture
	setting    entity.GovernanceSetting
	failReview bool
	denyPolicy bool
}
type domainPolicySQLConnector struct{ f *domainPolicySQLFixture }

func (c domainPolicySQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &domainPolicySQLConnection{rolesSQLConnection: &rolesSQLConnection{f: c.f.rolesSQLFixture}, f: c.f}, nil
}
func (c domainPolicySQLConnector) Driver() driver.Driver { return domainPolicySQLDriver(c) }

type domainPolicySQLDriver domainPolicySQLConnector

func (d domainPolicySQLDriver) Open(string) (driver.Conn, error) {
	return domainPolicySQLConnector(d).Connect(context.Background())
}

type domainPolicySQLConnection struct {
	*rolesSQLConnection
	f       *domainPolicySQLFixture
	setting *entity.GovernanceSetting
}

func (c *domainPolicySQLConnection) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.rolesSQLConnection.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	copy := c.f.setting
	c.setting = &copy
	return domainPolicySQLTransaction{tx, c}, nil
}

type domainPolicySQLTransaction struct {
	driver.Tx
	c *domainPolicySQLConnection
}

func (t domainPolicySQLTransaction) Commit() error {
	err := t.Tx.Commit()
	if err == nil {
		t.c.f.setting = *t.c.setting
	}
	t.c.setting = nil
	return err
}
func (t domainPolicySQLTransaction) Rollback() error { t.c.setting = nil; return t.Tx.Rollback() }
func (c *domainPolicySQLConnection) currentSetting() entity.GovernanceSetting {
	if c.setting != nil {
		return *c.setting
	}
	return c.f.setting
}
func (c *domainPolicySQLConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(q, `FROM "governance_settings"`):
		c.f.queries = append(c.f.queries, q)
		if c.f.failReview && len(c.f.data.audits) > 0 && c.data != nil && len(c.f.transactions) > 0 && c.f.transactions[len(c.f.transactions)-1].ReadOnly {
			return nil, errors.New("controlled postcommit review outage")
		}
		return effectiveSQLRows([]entity.GovernanceSetting{c.currentSetting()})
	case strings.Contains(q, "SELECT DISTINCT p.permission"):
		c.f.queries = append(c.f.queries, q)
		if c.f.denyPolicy {
			return effectiveSQLRows([]struct{ Permission string }{})
		}
		return effectiveSQLRows([]struct{ Permission string }{{"registration.write"}})
	case strings.Contains(q, `FROM "installations"`):
		c.f.queries = append(c.f.queries, q)
		return effectiveSQLRows([]entity.Installation{{ID: 1, Initialized: true}})
	default:
		return c.rolesSQLConnection.QueryContext(ctx, q, args)
	}
}
func (c *domainPolicySQLConnection) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, `UPDATE "governance_settings"`) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.f.writes = append(c.f.writes, q)
		if c.setting == nil {
			return nil, errors.New("untransactional policy write")
		}
		set := strings.Split(strings.Split(q, " SET ")[1], " WHERE ")[0]
		for i, col := range strings.Split(set, ",") {
			field := strings.Trim(strings.Split(col, "=")[0], " \"`")
			switch field {
			case "registration_enabled":
				c.setting.RegistrationEnabled = args[i].Value.(bool)
			case "registration_approval_required":
				c.setting.RegistrationApprovalRequired = args[i].Value.(bool)
			case "registration_policy_revision":
				c.setting.RegistrationPolicyRevision = args[i].Value.(string)
			case "registration_allowed_email_domains":
				c.setting.RegistrationAllowedEmailDomains = args[i].Value.(string)
			default:
				return nil, errors.New("unrelated policy field changed")
			}
		}
		return driver.RowsAffected(1), nil
	}
	return c.rolesSQLConnection.ExecContext(ctx, q, args)
}
func domainPolicySQLService(t *testing.T) (*Service, *domainPolicySQLFixture) {
	t.Helper()
	_, base := roleSQLService(t, 0)
	f := &domainPolicySQLFixture{rolesSQLFixture: base, setting: entity.GovernanceSetting{ID: 1, RegistrationEnabled: true, RegistrationPolicyRevision: memberRoleBaseline, RegistrationAllowedEmailDomains: `[]`}}
	pool := sql.OpenDB(domainPolicySQLConnector{f})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	return &Service{db: db}, f
}
func TestRegistrationDomainsAtomicReviewRetryAndRollback(t *testing.T) {
	ctx := context.Background()
	svc, f := domainPolicySQLService(t)
	original := f.setting
	review, err := svc.GetRegistrationPolicy(ctx, "usr_admin")
	if err != nil {
		t.Fatal(err)
	}
	input := RegistrationPolicyInput{Enabled: true, ApprovalRequired: true, AllowedEmailDomains: []string{" A.INVALID "}, Reason: "Review domain policy"}
	result, err := svc.SetRegistrationPolicy(ctx, "usr_admin", review.ReviewETag, input)
	if err != nil || result.Confirmation != "current_registration_policy" || len(result.AllowedEmailDomains) != 1 || result.AllowedEmailDomains[0] != "a.invalid" || f.setting.RegistrationPolicyRevision == original.RegistrationPolicyRevision || len(f.data.audits) != 1 {
		t.Fatal("domain policy not atomically confirmed", err)
	}
	if len(input.AllowedEmailDomains) != 1 || input.AllowedEmailDomains[0] != " A.INVALID " {
		t.Fatal("caller immutable input mutated")
	}
	persisted := f.setting
	writes := len(f.writes)
	retry, err := svc.SetRegistrationPolicy(ctx, "usr_admin", review.ReviewETag, input)
	if err != nil || retry.ReviewETag != result.ReviewETag || len(f.writes) != writes || len(f.data.audits) != 1 || !reflect.DeepEqual(persisted, f.setting) {
		t.Fatal("current-state retry rewrote or fabricated original receipt", err)
	}
	input.AllowedEmailDomains = []string{"b.invalid"}
	if _, err := svc.SetRegistrationPolicy(ctx, "usr_admin", review.ReviewETag, input); !errors.Is(err, catalogConflict) || len(f.data.audits) != 1 {
		t.Fatal("stale differing policy changed data", err)
	}
	f.failAudit = true
	if _, err := svc.SetRegistrationPolicy(ctx, "usr_admin", result.ReviewETag, input); err == nil || !reflect.DeepEqual(persisted, f.setting) || len(f.data.audits) != 1 {
		t.Fatal("audit failure did not roll back policy")
	}
	f.failAudit = false
	f.failReview = true
	if _, err := svc.SetRegistrationPolicy(ctx, "usr_admin", result.ReviewETag, input); !errors.Is(err, registrationApprovalUnavailable) || f.setting.RegistrationAllowedEmailDomains != `["b.invalid"]` || len(f.data.audits) != 2 {
		t.Fatal("postcommit uncertainty not genuine", err)
	}
	f.failReview = false
	writes = len(f.writes)
	if _, err := svc.SetRegistrationPolicy(ctx, "usr_admin", result.ReviewETag, input); err != nil || len(f.writes) != writes || len(f.data.audits) != 2 {
		t.Fatal("uncertain identical retry wrote twice", err)
	}
}
func TestRegistrationDomainsAuthorityPublicPrivacyAndLegacyAdapter(t *testing.T) {
	ctx := context.Background()
	svc, f := domainPolicySQLService(t)
	f.setting.RegistrationAllowedEmailDomains = `["a.invalid"]`
	public, err := svc.PublicRegistrationStatus(ctx)
	if err != nil || len(public.AllowedEmailDomains) != 1 {
		t.Fatal("enabled public guidance missing", err)
	}
	if err := svc.SetRegistrationEnabled(ctx, "usr_admin", false); err != nil || f.setting.RegistrationAllowedEmailDomains != `["a.invalid"]` {
		t.Fatal("trusted switch writer reset domain policy", err)
	}
	public, err = svc.PublicRegistrationStatus(ctx)
	if err != nil || public.Enabled || public.AllowedEmailDomains == nil || len(public.AllowedEmailDomains) != 0 {
		t.Fatal("closed public policy leaked saved domains", err)
	}
	f.denyPolicy = true
	if _, err := svc.GetRegistrationPolicy(ctx, "usr_admin"); !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatal("base admin without permission read policy", err)
	}
	f.denyPolicy = false
	actor := f.data.users["usr_admin"]
	actor.Role = entity.RoleMember
	f.data.users[actor.ID] = actor
	if _, err := svc.GetRegistrationPolicy(ctx, actor.ID); !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatal("delegated permission without base admin read policy", err)
	}
	actor.Role = entity.RoleAdmin
	actor.Disabled = true
	f.data.users[actor.ID] = actor
	if _, err := svc.GetRegistrationPolicy(ctx, actor.ID); err == nil {
		t.Fatal("disabled policy actor accepted")
	}
	f.setting.RegistrationAllowedEmailDomains = ""
	if _, err := svc.PublicRegistrationStatus(ctx); !errors.Is(err, registrationApprovalUnavailable) {
		t.Fatal("unknown stored domains became unrestricted", err)
	}
}
func TestRegistrationDomainsLatestLockedPolicyBeforeCreation(t *testing.T) {
	svc, f := domainPolicySQLService(t)
	ctx := context.Background()
	if public, err := svc.PublicRegistrationStatus(ctx); err != nil || len(public.AllowedEmailDomains) != 0 {
		t.Fatal(err)
	}
	// A once-visible unrestricted policy cannot authorize the subsequent POST.
	f.setting.RegistrationAllowedEmailDomains = `["a.invalid"]`
	for _, required := range []bool{false, true} {
		f.setting.RegistrationApprovalRequired = required
		before := f.setting
		userCount := len(f.data.users)
		auditCount := len(f.data.audits)
		writes := len(f.writes)
		result, err := svc.RegisterWithApproval(ctx, "denied@b.invalid", "test-only-domain-password", "Applicant")
		if result != nil || !errors.Is(err, apperrors.ErrForbidden) || len(f.writes) != writes || len(f.data.users) != userCount || len(f.data.audits) != auditCount || !reflect.DeepEqual(before, f.setting) {
			t.Fatal("stale public policy created identity or security facts", err)
		}
	}
}
