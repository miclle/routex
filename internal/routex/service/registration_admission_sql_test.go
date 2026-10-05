package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"strings"
	"testing"
	"time"
)

type registrationSQLFixture struct {
	*rolesSQLFixture
	application        entity.RegistrationApprovalApplication
	applicationQueries int
}
type registrationSQLConnector struct{ f *registrationSQLFixture }

func (c registrationSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &registrationSQLConnection{rolesSQLConnection: &rolesSQLConnection{f: c.f.rolesSQLFixture}, f: c.f}, nil
}
func (c registrationSQLConnector) Driver() driver.Driver { return registrationSQLDriver(c) }

type registrationSQLDriver registrationSQLConnector

func (d registrationSQLDriver) Open(string) (driver.Conn, error) {
	return &registrationSQLConnection{rolesSQLConnection: &rolesSQLConnection{f: d.f.rolesSQLFixture}, f: d.f}, nil
}

type registrationSQLConnection struct {
	*rolesSQLConnection
	f *registrationSQLFixture
}

func (c *registrationSQLConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, `FROM "registration_approval_applications"`) {
		c.f.applicationQueries++
		return effectiveSQLRows([]entity.RegistrationApprovalApplication{c.f.application})
	}
	return c.rolesSQLConnection.QueryContext(ctx, q, args)
}
func TestRegistrationAdmissionBeforeSessionMFAAndPermissionsSQL(t *testing.T) {
	_, base := roleSQLService(t, 0)
	now := time.Now().UTC().Truncate(time.Microsecond)
	applicationID := "raa_01j00000000000000000000000"
	u := base.data.users["usr_admin"]
	u.CreatedAt = now
	u.ApprovalApplicationID = &applicationID
	base.data.users[u.ID] = u
	f := &registrationSQLFixture{rolesSQLFixture: base, application: entity.RegistrationApprovalApplication{ID: applicationID, UserID: u.ID, UserCreatedAt: now, CreatedAt: now, State: "pending", Revision: memberRoleBaseline}}
	raw := sql.OpenDB(registrationSQLConnector{f})
	t.Cleanup(func() { _ = raw.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: raw}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	before := len(f.writes)
	if _, err := createSession(db, entity.User{ID: u.ID}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("caller-supplied empty link bypassed Session issuance: %v", err)
	}
	if _, err := mfaActiveUser(db, u.ID); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("pending identity reached MFA proof boundary: %v", err)
	}
	if _, err := permissionsFor(db, u.ID); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("pending base admin obtained capabilities: %v", err)
	}
	if len(f.writes) != before || f.applicationQueries != 3 {
		t.Fatal("before-use guard wrote state or failed to load each complete link")
	}
	f.application.UserCreatedAt = now.Add(time.Microsecond)
	f.application.State = "approved"
	actor := "usr_admin"
	reason := "Approved exact subject"
	f.application.DecidedAt = &now
	f.application.DecisionActorID = &actor
	f.application.DecisionReason = &reason
	if _, err := registrationAdmittedUser(db, u.ID, false); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("changed creation borrowed decision: %v", err)
	}
}
