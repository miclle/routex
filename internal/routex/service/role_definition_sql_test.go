package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"reflect"
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

// Reuse the existing Member Roles driver model without changing its original
// assertions. This wrapper adds only admitted actor and independent receipt faults.
type roleDefinitionSQLControl struct {
	applications               map[string]entity.RegistrationApprovalApplication
	roleAlias, permissionAlias bool
	afterCommit                func(*rolesSQLFixture)
	commitFaultApplied         bool
}
type roleDefinitionSQLConnector struct {
	fixture *rolesSQLFixture
	control *roleDefinitionSQLControl
}

func (c roleDefinitionSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &roleDefinitionSQLConnection{rolesSQLConnection: &rolesSQLConnection{f: c.fixture}, control: c.control}, nil
}
func (c roleDefinitionSQLConnector) Driver() driver.Driver { return roleDefinitionSQLDriver(c) }

type roleDefinitionSQLDriver roleDefinitionSQLConnector

func (d roleDefinitionSQLDriver) Open(string) (driver.Conn, error) {
	return roleDefinitionSQLConnector(d).Connect(context.Background())
}

type roleDefinitionSQLConnection struct {
	*rolesSQLConnection
	control *roleDefinitionSQLControl
}

func (c *roleDefinitionSQLConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *roleDefinitionSQLConnection) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	tx, err := c.rolesSQLConnection.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return roleDefinitionSQLTransaction{Tx: tx, connection: c}, nil
}

type roleDefinitionSQLTransaction struct {
	driver.Tx
	connection *roleDefinitionSQLConnection
}

func (t roleDefinitionSQLTransaction) Commit() error {
	if err := t.Tx.Commit(); err != nil {
		return err
	}
	c := t.connection
	if c.control.afterCommit != nil && !c.control.commitFaultApplied {
		for _, row := range c.f.data.audits {
			if row.Action == "role.definition.update" {
				c.control.commitFaultApplied = true
				c.control.afterCommit(c.f)
				break
			}
		}
	}
	return nil
}
func (c *roleDefinitionSQLConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	values := rolesSQLStrings(args)
	switch {
	case strings.Contains(query, "role_permissions AS permission"):
		c.f.queries = append(c.f.queries, query)
		permission, actorID := "", ""
		for _, value := range values {
			if _, ok := c.current().users[value]; ok {
				actorID = value
			}
			if slices.Contains(AvailablePermissions, value) {
				permission = value
			}
		}
		if c.f.deny[permission] {
			return effectiveSQLRows([]exactPermissionIdentity{})
		}
		builtin := "rol_member"
		if c.current().users[actorID].Role == entity.RoleAdmin {
			builtin = "rol_admin"
		}
		return effectiveSQLRows([]exactPermissionIdentity{{RoleID: builtin, PermissionRoleID: builtin, Permission: permission}})
	case strings.Contains(query, `FROM "registration_approval_applications"`):
		c.f.queries = append(c.f.queries, query)
		rows := []entity.RegistrationApprovalApplication{}
		for _, value := range values {
			if row, ok := c.control.applications[value]; ok {
				rows = append(rows, row)
			}
		}
		return effectiveSQLRows(rows)
	case strings.Contains(query, `FROM "roles"`) && c.control.roleAlias:
		c.f.queries = append(c.f.queries, query)
		role := c.current().roles["rol_00000"]
		role.ID = "rol_0000A"
		return effectiveSQLRows([]entity.Role{role})
	case strings.Contains(query, `FROM "role_permissions"`) && c.control.permissionAlias:
		c.f.queries = append(c.f.queries, query)
		return effectiveSQLRows([]entity.RolePermission{{RoleID: "rol_0000A", Permission: "members.read"}})
	}
	return c.rolesSQLConnection.QueryContext(ctx, query, args)
}

func roleDefinitionSQLService(t *testing.T) (*Service, *rolesSQLFixture, *roleDefinitionSQLControl) {
	t.Helper()
	_, fixture := roleSQLService(t, 1)
	birth := fixture.data.users["usr_admin"].CreatedAt
	for id, role := range fixture.data.roles {
		role.CreatedAt = birth
		if !role.Builtin {
			role.Description = "Recorded role purpose"
		}
		fixture.data.roles[id] = role
	}
	control := &roleDefinitionSQLControl{applications: map[string]entity.RegistrationApprovalApplication{}}
	pool := sql.OpenDB(roleDefinitionSQLConnector{fixture: fixture, control: control})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return &Service{db: db}, fixture, control
}
func roleDefinitionSQLInput(t *testing.T, s *Service) (*RoleDefinitionRecord, RoleDefinitionInput) {
	t.Helper()
	record, err := s.GetRoleDefinition(context.Background(), "usr_admin", "rol_00000")
	if err != nil || record.IdentityETag == nil {
		t.Fatal(record, err)
	}
	return record, RoleDefinitionInput{Name: "Reviewed changed role", Description: "Reviewed changed purpose", Permissions: []string{"prices.read"}, IdentityETag: *record.IdentityETag, Reason: "Controlled reviewed definition"}
}
func roleDefinitionSQLPending(fixture *rolesSQLFixture, control *roleDefinitionSQLControl) {
	actor := fixture.data.users["usr_admin"]
	applicationID := "raa_00000000000000000000000000"
	actor.ApprovalApplicationID = &applicationID
	fixture.data.users[actor.ID] = actor
	control.applications[applicationID] = entity.RegistrationApprovalApplication{ID: applicationID, UserID: actor.ID, UserCreatedAt: actor.CreatedAt, CreatedAt: actor.CreatedAt, State: "pending", Revision: memberRoleBaseline}
}

func TestRoleDefinitionSQLIndependentAuthorityAndPrivateReadBounds(t *testing.T) {
	ctx := context.Background()
	t.Run("read_permission_is_not_writer_authority", func(t *testing.T) {
		s, fixture, _ := roleDefinitionSQLService(t)
		actor := fixture.data.users["usr_admin"]
		actor.Role = entity.RoleMember
		fixture.data.users[actor.ID] = actor
		record, err := s.GetRoleDefinition(ctx, actor.ID, "rol_00000")
		if err != nil || record.CanEdit || len(fixture.writes) != 0 {
			t.Fatal(record, err)
		}
		input := RoleDefinitionInput{Name: record.Name, Description: record.Description, Permissions: record.Permissions, IdentityETag: *record.IdentityETag, Reason: "Delegated writer cannot edit"}
		if result, err := s.SetReviewedRoleDefinition(ctx, actor.ID, record.ID, record.ReviewETag, input); result != nil || err != apperrors.ErrForbidden || len(fixture.writes) != 0 {
			t.Fatal(result, err)
		}
	})
	t.Run("writer_and_receipt_do_not_require_roles_read", func(t *testing.T) {
		s, fixture, _ := roleDefinitionSQLService(t)
		record, input := roleDefinitionSQLInput(t, s)
		fixture.deny["roles.read"] = true
		fixture.queries = nil
		result, err := s.SetReviewedRoleDefinition(ctx, "usr_admin", record.ID, record.ReviewETag, input)
		if err != nil || result.Confirmation != "current_role_definition" || result.Effect != "current_database" || len(fixture.data.audits) != 1 {
			t.Fatal(result, err)
		}
		for _, query := range fixture.queries {
			if strings.Contains(query, "role_permissions AS permission") {
				t.Fatal("writer acquired a read-permission dependency")
			}
		}
	})
	for _, fault := range []string{"denied_read", "actor_alias", "disabled", "offboarded", "pending", "role_alias", "permission_alias", "stored_101", "stored_duplicate", "stored_unsafe", "stored_historical"} {
		t.Run(fault, func(t *testing.T) {
			s, fixture, control := roleDefinitionSQLService(t)
			actor := fixture.data.users["usr_admin"]
			switch fault {
			case "denied_read":
				fixture.deny["roles.read"] = true
			case "actor_alias":
				fixture.actorAlias = true
			case "disabled":
				actor.Disabled = true
				fixture.data.users[actor.ID] = actor
			case "offboarded":
				actor.OffboardedAt = &actor.CreatedAt
				fixture.data.users[actor.ID] = actor
			case "pending":
				roleDefinitionSQLPending(fixture, control)
			case "role_alias":
				control.roleAlias = true
			case "permission_alias":
				control.permissionAlias = true
			case "stored_101":
				fixture.data.permissions["rol_00000"] = make([]string, 101)
			case "stored_duplicate":
				fixture.data.permissions["rol_00000"] = []string{"members.read", "members.read"}
			case "stored_unsafe":
				fixture.data.permissions["rol_00000"] = []string{"members/read"}
			case "stored_historical":
				fixture.data.permissions["rol_00000"] = []string{"Recorded.Mixed_CASE"}
			}
			record, err := s.GetRoleDefinition(ctx, actor.ID, "rol_00000")
			if fault == "stored_historical" {
				if err != nil || !slices.Equal(record.Permissions, []string{"Recorded.Mixed_CASE"}) {
					t.Fatal(record, err)
				}
			} else if record != nil || err == nil {
				t.Fatal("unsafe private read accepted", record, err)
			}
			if fault == "stored_101" && err != roleDefinitionOverflow {
				t.Fatal("incomplete read hid overflow", err)
			}
			if fault == "denied_read" || fault == "actor_alias" || fault == "disabled" || fault == "offboarded" || fault == "pending" {
				for _, query := range fixture.queries {
					if strings.Contains(query, `FROM "roles"`) || strings.Contains(query, `FROM "role_permissions"`) {
						t.Fatal("target definition read before current authorization")
					}
				}
			}
			if len(fixture.writes) != 0 {
				t.Fatal("read wrote")
			}
		})
	}
}

func TestRoleDefinitionSQLAtomicAuditNoopAndUncertainRetry(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"success", "audit_failure", "confirmation_outage", "noop"} {
		t.Run(mode, func(t *testing.T) {
			s, fixture, _ := roleDefinitionSQLService(t)
			record, input := roleDefinitionSQLInput(t, s)
			original := fixture.data.clone()
			if mode == "noop" {
				input.Description = record.Description
				input.Name = record.Name
				input.Permissions = slices.Clone(record.Permissions)
			}
			fixture.failAudit = mode == "audit_failure"
			fixture.failConfirmation = mode == "confirmation_outage"
			fixture.writes = nil
			result, err := s.SetReviewedRoleDefinition(ctx, "usr_admin", record.ID, record.ReviewETag, input)
			if mode == "audit_failure" {
				if result != nil || err == nil || !reflect.DeepEqual(fixture.data, original) {
					t.Fatal("audit failure did not roll back complete mutation", err)
				}
				return
			}
			if mode == "noop" {
				if err != nil || result == nil || len(fixture.writes) != 0 || !reflect.DeepEqual(fixture.data, original) {
					t.Fatal("no-op changed durable facts", err)
				}
				return
			}
			role := fixture.data.roles[record.ID]
			if role.Name != input.Name || !slices.Equal(fixture.data.permissions[record.ID], input.Permissions) || role.DefinitionRevision == original.roles[record.ID].DefinitionRevision || !role.CreatedAt.Equal(original.roles[record.ID].CreatedAt) || len(fixture.data.audits) != 1 || !reflect.DeepEqual(fixture.data.users, original.users) || !reflect.DeepEqual(fixture.data.assignments, original.assignments) {
				t.Fatal("atomic definition or unrelated identity facts", err)
			}
			if fixture.data.audits[0].Action != "role.definition.update" {
				t.Fatal("wrong audit action")
			}
			if mode == "confirmation_outage" {
				if result != nil || err != roleDefinitionUnavailable {
					t.Fatal("uncertain commit reported success", result, err)
				}
				fixture.failConfirmation = false
				beforeRetry := fixture.data.clone()
				fixture.writes = nil
				result, err = s.SetReviewedRoleDefinition(ctx, "usr_admin", record.ID, record.ReviewETag, input)
				if err != nil || len(fixture.writes) != 0 || !reflect.DeepEqual(fixture.data, beforeRetry) {
					t.Fatal("exact old retry replayed writes", err)
				}
			}
			if result != nil {
				raw, marshalErr := json.Marshal(result)
				var fields map[string]json.RawMessage
				if marshalErr != nil || json.Unmarshal(raw, &fields) != nil || len(fields) != 8 {
					t.Fatal("noncontract receipt", string(raw), marshalErr)
				}
				for _, key := range []string{"id", "name", "description", "permissions", "identity_etag", "etag", "confirmation", "effect"} {
					if fields[key] == nil {
						t.Fatal("missing receipt field", key)
					}
				}
			}
			if err != nil || result == nil || result.IdentityETag != input.IdentityETag || !slices.Equal(result.Permissions, input.Permissions) {
				t.Fatal(result, err)
			}
			last := fixture.transactions[len(fixture.transactions)-1]
			if !last.ReadOnly || last.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
				t.Fatal("receipt lacked fresh RR snapshot", last)
			}
			for _, query := range fixture.queries {
				if strings.Contains(query, "user_roles") && !strings.Contains(query, "role_permissions AS permission") {
					t.Fatal("definition fetched assignment directory")
				}
			}
		})
	}
}

func TestRoleDefinitionSQLABAIdentityAndTrustedZeroBirthCompatibility(t *testing.T) {
	ctx := context.Background()
	t.Run("private_generation_blocks_changing_ABA_only", func(t *testing.T) {
		s, fixture, _ := roleDefinitionSQLService(t)
		record, input := roleDefinitionSQLInput(t, s)
		for _, name := range []string{"Transient definition", record.Name} {
			if _, err := s.SaveRole(ctx, "usr_admin", record.ID, name, record.Permissions); err != nil {
				t.Fatal(err)
			}
		}
		before := fixture.data.clone()
		fixture.writes = nil
		if result, err := s.SetReviewedRoleDefinition(ctx, "usr_admin", record.ID, record.ReviewETag, input); result != nil || err != catalogConflict || len(fixture.writes) != 0 {
			t.Fatal("changing ABA review accepted", result, err)
		}
		input.Name, input.Description, input.Permissions = record.Name, record.Description, slices.Clone(record.Permissions)
		if result, err := s.SetReviewedRoleDefinition(ctx, "usr_admin", record.ID, record.ReviewETag, input); result == nil || err != nil || len(fixture.writes) != 0 || !reflect.DeepEqual(fixture.data, before) {
			t.Fatal("same current incarnation equality wrote or failed", result, err)
		}
	})
	t.Run("reused_exact_ID_cannot_reconcile_equal_content", func(t *testing.T) {
		s, fixture, _ := roleDefinitionSQLService(t)
		record, input := roleDefinitionSQLInput(t, s)
		input.Name, input.Description, input.Permissions = record.Name, record.Description, slices.Clone(record.Permissions)
		role := fixture.data.roles[record.ID]
		role.CreatedAt = role.CreatedAt.Add(time.Microsecond)
		fixture.data.roles[record.ID] = role
		before := fixture.data.clone()
		if result, err := s.SetReviewedRoleDefinition(ctx, "usr_admin", record.ID, record.ReviewETag, input); result != nil || err != catalogConflict || len(fixture.writes) != 0 || !reflect.DeepEqual(before, fixture.data) {
			t.Fatal("captured birth identity retargeted", result, err)
		}
	})
	t.Run("unknown_birth_trusted_adapter_only", func(t *testing.T) {
		s, fixture, _ := roleDefinitionSQLService(t)
		role := fixture.data.roles["rol_00000"]
		role.CreatedAt = time.Time{}
		fixture.data.roles[role.ID] = role
		record, err := s.GetRoleDefinition(ctx, "usr_admin", role.ID)
		if err != nil || record.IdentityETag != nil || record.CanEdit {
			t.Fatal("unknown provenance not read-only", record, err)
		}
		input := RoleDefinitionInput{Name: record.Name, Description: record.Description, Permissions: record.Permissions, IdentityETag: strings.Repeat("a", 64), Reason: "Unknown birth cannot confirm"}
		before := fixture.data.clone()
		if result, err := s.SetReviewedRoleDefinition(ctx, "usr_admin", role.ID, record.ReviewETag, input); result != nil || err != catalogConflict || len(fixture.writes) != 0 || !reflect.DeepEqual(before, fixture.data) {
			t.Fatal("public equal intent gained unknown birth", result, err)
		}
		if _, err := s.SaveRole(ctx, "usr_admin", role.ID, "Trusted fixture revision", []string{"prices.read"}); err != nil {
			t.Fatal("trusted exact adapter compatibility regressed", err)
		}
		if !fixture.data.roles[role.ID].CreatedAt.IsZero() || fixture.data.roles[role.ID].DefinitionRevision == role.DefinitionRevision || len(fixture.data.audits) != 1 || fixture.data.audits[0].Action != "role.save" {
			t.Fatal("trusted adapter forged birth or skipped revision/audit")
		}
	})
	t.Run("recorded_nonassignable_equality_denied", func(t *testing.T) {
		s, fixture, _ := roleDefinitionSQLService(t)
		fixture.data.permissions["rol_00000"] = []string{"Recorded.Mixed_CASE"}
		record, err := s.GetRoleDefinition(ctx, "usr_admin", "rol_00000")
		if err != nil {
			t.Fatal(err)
		}
		input := RoleDefinitionInput{Name: record.Name, Description: record.Description, Permissions: record.Permissions, IdentityETag: *record.IdentityETag, Reason: "No historical grant"}
		if result, err := s.SetReviewedRoleDefinition(ctx, "usr_admin", record.ID, record.ReviewETag, input); result != nil || err != apperrors.ErrBadRequest || len(fixture.writes) != 0 {
			t.Fatal("recorded code became assignable through equality", result, err)
		}
	})
}

func TestRoleDefinitionSQLPostcommitCurrentDenialsNeverReplay(t *testing.T) {
	ctx := context.Background()
	for _, fault := range []string{"demoted", "disabled", "offboarded", "pending", "deleted", "different_birth", "different_contents", "read_failure"} {
		t.Run(fault, func(t *testing.T) {
			s, fixture, control := roleDefinitionSQLService(t)
			record, input := roleDefinitionSQLInput(t, s)
			control.afterCommit = func(f *rolesSQLFixture) {
				actor := f.data.users["usr_admin"]
				switch fault {
				case "demoted":
					actor.Role = entity.RoleMember
					f.data.users[actor.ID] = actor
				case "disabled":
					actor.Disabled = true
					f.data.users[actor.ID] = actor
				case "offboarded":
					actor.OffboardedAt = &actor.CreatedAt
					f.data.users[actor.ID] = actor
				case "pending":
					roleDefinitionSQLPending(f, control)
				case "deleted":
					delete(f.data.roles, record.ID)
				case "different_birth":
					role := f.data.roles[record.ID]
					role.CreatedAt = role.CreatedAt.Add(time.Microsecond)
					f.data.roles[record.ID] = role
				case "different_contents":
					role := f.data.roles[record.ID]
					role.Name = "Later independently edited role"
					f.data.roles[record.ID] = role
				case "read_failure":
					f.failConfirmation = true
				}
			}
			result, err := s.SetReviewedRoleDefinition(ctx, "usr_admin", record.ID, record.ReviewETag, input)
			if result != nil || err == nil || len(fixture.data.audits) != 1 || fixture.data.audits[0].Action != "role.definition.update" {
				t.Fatal("postcommit denial invented receipt or rolled back already committed facts", result, err)
			}
			before := fixture.data.clone()
			fixture.writes = nil
			result, err = s.SetReviewedRoleDefinition(ctx, "usr_admin", record.ID, record.ReviewETag, input)
			if result != nil || err == nil || len(fixture.writes) != 0 || !reflect.DeepEqual(before, fixture.data) {
				t.Fatal("failed retry replayed or discarded durable facts", result, err)
			}
		})
	}
}

var _ driver.QueryerContext = (*roleDefinitionSQLConnection)(nil)
var _ driver.ConnBeginTx = (*roleDefinitionSQLConnection)(nil)
