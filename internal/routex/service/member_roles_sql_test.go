package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
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

type rolesSQLData struct {
	users       map[string]entity.User
	roles       map[string]entity.Role
	permissions map[string][]string
	assignments []entity.UserRole
	audits      []entity.AuditEvent
}
type rolesSQLFixture struct {
	data                        rolesSQLData
	queries                     []string
	writes                      []string
	transactions                []driver.TxOptions
	deny                        map[string]bool
	actorAlias, subjectAlias    bool
	failAudit, failConfirmation bool
}

func (d rolesSQLData) clone() rolesSQLData {
	out := rolesSQLData{users: map[string]entity.User{}, roles: map[string]entity.Role{}, permissions: map[string][]string{}, assignments: slices.Clone(d.assignments), audits: slices.Clone(d.audits)}
	for id, u := range d.users {
		out.users[id] = u
	}
	for id, r := range d.roles {
		out.roles[id] = r
	}
	for id, p := range d.permissions {
		out.permissions[id] = slices.Clone(p)
	}
	return out
}

type rolesSQLConnector struct{ f *rolesSQLFixture }

func (c rolesSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &rolesSQLConnection{f: c.f}, nil
}
func (c rolesSQLConnector) Driver() driver.Driver { return rolesSQLDriver(c) }

type rolesSQLDriver rolesSQLConnector

func (d rolesSQLDriver) Open(string) (driver.Conn, error) { return &rolesSQLConnection{f: d.f}, nil }

type rolesSQLConnection struct {
	f    *rolesSQLFixture
	data *rolesSQLData
}

func (*rolesSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}
func (*rolesSQLConnection) Close() error { return nil }
func (c *rolesSQLConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *rolesSQLConnection) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.f.transactions = append(c.f.transactions, opts)
	data := c.f.data.clone()
	c.data = &data
	return rolesSQLTransaction{c}, nil
}

type rolesSQLTransaction struct{ c *rolesSQLConnection }

func (t rolesSQLTransaction) Commit() error   { t.c.f.data = *t.c.data; t.c.data = nil; return nil }
func (t rolesSQLTransaction) Rollback() error { t.c.data = nil; return nil }
func (c *rolesSQLConnection) current() *rolesSQLData {
	if c.data != nil {
		return c.data
	}
	return &c.f.data
}
func rolesSQLStrings(args []driver.NamedValue) []string {
	values := []string{}
	for _, arg := range args {
		if v, ok := arg.Value.(string); ok {
			values = append(values, v)
		}
	}
	return values
}
func rolesSQLLimit(args []driver.NamedValue) int {
	for i := len(args) - 1; i >= 0; i-- {
		if n, ok := args[i].Value.(int64); ok {
			return int(n)
		}
	}
	return 1000000
}
func (c *rolesSQLConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.f.queries = append(c.f.queries, q)
	d := c.current()
	values := rolesSQLStrings(args)
	switch {
	case strings.Contains(q, `FROM "governance_settings"`):
		return effectiveSQLRows([]entity.GovernanceSetting{{ID: 1}})
	case strings.Contains(q, `FROM "users"`):
		if c.f.failConfirmation && len(d.audits) > 0 {
			return nil, errors.New("controlled postcommit confirmation outage")
		}
		id := values[0]
		u, ok := d.users[id]
		if strings.Contains(q, "disabled =") && (u.Disabled || u.OffboardedAt != nil) {
			ok = false
		}
		if !ok {
			return effectiveSQLRows([]entity.User{})
		}
		if id == "usr_admin" && c.f.actorAlias {
			u.ID = "USR_ADMIN"
		}
		if id == "usr_target" && c.f.subjectAlias {
			u.ID = "USR_TARGET"
		}
		return effectiveSQLRows([]entity.User{u})
	case strings.Contains(q, "role_permissions AS permission"):
		permission := ""
		for _, v := range values {
			if slices.Contains(AvailablePermissions, v) {
				permission = v
			}
		}
		if c.f.deny[permission] {
			return effectiveSQLRows([]exactPermissionIdentity{})
		}
		return effectiveSQLRows([]exactPermissionIdentity{{RoleID: "rol_admin", PermissionRoleID: "rol_admin", Permission: permission}})
	case strings.Contains(q, "SELECT DISTINCT p.permission"):
		return effectiveSQLRows([]struct{ Permission string }{{"members.write"}})
	case strings.Contains(q, `FROM "user_roles"`):
		rows := []entity.UserRole{}
		for _, row := range d.assignments {
			if row.UserID == values[0] {
				rows = append(rows, row)
			}
		}
		return effectiveSQLRows(rows[:min(len(rows), rolesSQLLimit(args))])
	case strings.Contains(q, `FROM "roles"`):
		rows := []entity.Role{}
		for _, row := range d.roles {
			if strings.Contains(q, "WHERE builtin =") {
				if !row.Builtin {
					rows = append(rows, row)
				}
			} else if slices.Contains(values, row.ID) {
				rows = append(rows, row)
			}
		}
		slices.SortFunc(rows, func(a, b entity.Role) int { return strings.Compare(a.ID, b.ID) })
		return effectiveSQLRows(rows[:min(len(rows), rolesSQLLimit(args))])
	case strings.Contains(q, `FROM "role_permissions"`):
		if strings.Contains(q, `SELECT "permission"`) {
			rows := []struct{ Permission string }{}
			for _, id := range values {
				for _, p := range d.permissions[id] {
					rows = append(rows, struct{ Permission string }{p})
				}
			}
			slices.SortFunc(rows, func(a, b struct{ Permission string }) int { return strings.Compare(a.Permission, b.Permission) })
			return effectiveSQLRows(rows)
		}
		rows := []entity.RolePermission{}
		for _, id := range values {
			for _, p := range d.permissions[id] {
				rows = append(rows, entity.RolePermission{RoleID: id, Permission: p})
			}
		}
		return effectiveSQLRows(rows[:min(len(rows), rolesSQLLimit(args))])
	}
	return nil, errors.New("unexpected scoped role query")
}
func (c *rolesSQLConnection) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.f.writes = append(c.f.writes, q)
	d := c.current()
	values := rolesSQLStrings(args)
	switch {
	case strings.HasPrefix(q, `DELETE FROM "user_roles"`):
		rows := []entity.UserRole{}
		for _, row := range d.assignments {
			if row.UserID != values[0] || row.RoleID != values[1] {
				rows = append(rows, row)
			}
		}
		d.assignments = rows
	case strings.HasPrefix(q, `INSERT INTO "user_roles"`):
		d.assignments = append(d.assignments, entity.UserRole{UserID: values[0], RoleID: values[1]})
	case strings.HasPrefix(q, `UPDATE "users"`):
		id := values[len(values)-1]
		u := d.users[id]
		set := strings.Split(strings.Split(q, " SET ")[1], " WHERE ")[0]
		columns := strings.Split(set, ",")
		for i, col := range columns {
			field := strings.Trim(strings.Split(col, "=")[0], " \"`")
			switch field {
			case "member_role_revision":
				u.MemberRoleRevision = args[i].Value.(string)
			case "role":
				u.Role = args[i].Value.(string)
			case "disabled":
				u.Disabled = args[i].Value.(bool)
			case "updated_at":
				u.UpdatedAt = args[i].Value.(time.Time)
			}
		}
		d.users[id] = u
	case strings.HasPrefix(q, `UPDATE "roles"`):
		id := values[len(values)-1]
		role := d.roles[id]
		set := strings.Split(strings.Split(q, " SET ")[1], " WHERE ")[0]
		for i, col := range strings.Split(set, ",") {
			field := strings.Trim(strings.Split(col, "=")[0], " \"`")
			switch field {
			case "definition_revision":
				role.DefinitionRevision = args[i].Value.(string)
			case "name":
				role.Name = args[i].Value.(string)
			case "name_key":
				role.NameKey = args[i].Value.(string)
			}
		}
		d.roles[id] = role
	case strings.HasPrefix(q, `DELETE FROM "role_permissions"`):
		d.permissions[values[0]] = []string{}
	case strings.HasPrefix(q, `INSERT INTO "role_permissions"`):
		d.permissions[values[0]] = append(d.permissions[values[0]], values[1])
	case strings.HasPrefix(q, `INSERT INTO "audit_events"`):
		if c.f.failAudit {
			return nil, errors.New("controlled atomic audit failure")
		}
		var details *string
		if v, ok := args[0].Value.(string); ok {
			details = &v
		}
		d.audits = append(d.audits, entity.AuditEvent{DetailsJSON: details, ID: args[1].Value.(string), ActorID: args[2].Value.(string), Action: args[3].Value.(string), ResourceType: args[4].Value.(string), ResourceID: args[5].Value.(string)})
	default:
		return nil, errors.New("unexpected role mutation")
	}
	return driver.RowsAffected(1), nil
}
func roleSQLService(t *testing.T, count int) (*Service, *rolesSQLFixture) {
	t.Helper()
	now := time.Date(2026, 10, 5, 0, 0, 0, 123456000, time.UTC)
	f := &rolesSQLFixture{deny: map[string]bool{}, data: rolesSQLData{users: map[string]entity.User{"usr_admin": {ID: "usr_admin", Role: entity.RoleAdmin, CreatedAt: now, MemberRoleRevision: memberRoleBaseline}, "usr_target": {ID: "usr_target", Role: entity.RoleMember, CreatedAt: now, UpdatedAt: now, MemberRoleRevision: memberRoleBaseline}}, roles: map[string]entity.Role{}, permissions: map[string][]string{}, assignments: []entity.UserRole{}, audits: []entity.AuditEvent{}}}
	for _, id := range []string{"rol_member", "rol_admin"} {
		f.data.roles[id] = entity.Role{ID: id, Name: id, Builtin: true, DefinitionRevision: memberRoleBaseline}
		f.data.permissions[id] = []string{"members.read", "roles.read"}
	}
	for i := range count {
		id := fmt.Sprintf("rol_%05d", i)
		f.data.roles[id] = entity.Role{ID: id, Name: "Role literal%_", DefinitionRevision: memberRoleBaseline}
		f.data.permissions[id] = []string{"members.read"}
		f.data.assignments = append(f.data.assignments, entity.UserRole{UserID: "usr_target", RoleID: id})
	}
	pool := sql.OpenDB(rolesSQLConnector{f})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return &Service{db: db}, f
}
func TestMemberRolesMeasuredBatchSnapshotAndDenials(t *testing.T) {
	for _, n := range []int{0, 1, 100, 501, 1001, 10000} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			s, f := roleSQLService(t, n)
			page, err := s.GetMemberRoles(context.Background(), "usr_admin", "usr_target")
			if err != nil || len(page.AssignedRoles) != n {
				t.Fatal(err)
			}
			expected := 5 + 2*((n+1+memberRolesBatchSize-1)/memberRolesBatchSize)
			if n <= 1000 {
				expected++
			}
			if len(f.queries) != expected || len(f.writes) != 0 || len(f.transactions) != 1 || !f.transactions[0].ReadOnly || f.transactions[0].Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
				t.Fatal(n, len(f.queries), expected, f.transactions)
			}
			if n > 1000 && page.CanEdit {
				t.Fatal("audit bound erased")
			}
		})
	}
	for _, permission := range []string{"members.read", "roles.read"} {
		t.Run(permission, func(t *testing.T) {
			s, f := roleSQLService(t, 1)
			f.deny[permission] = true
			p, err := s.GetMemberRoles(context.Background(), "usr_admin", "usr_target")
			if p != nil || err != apperrors.ErrForbidden {
				t.Fatal(err)
			}
			for _, q := range f.queries {
				if strings.Contains(q, `FROM "user_roles"`) || strings.Contains(q, `FROM "roles"`) {
					t.Fatal("private read after denial")
				}
			}
		})
	}
	for _, alias := range []string{"actor", "subject"} {
		s, f := roleSQLService(t, 1)
		f.actorAlias = alias == "actor"
		f.subjectAlias = alias == "subject"
		page, err := s.GetMemberRoles(context.Background(), "usr_admin", "usr_target")
		if page != nil || err == nil {
			t.Fatal("collation alias")
		}
	}
}
func TestMemberRolesAtomicAuditAndPostcommitCurrentConfirmation(t *testing.T) {
	for _, mode := range []string{"success", "audit_failure", "confirmation_outage", "noop"} {
		t.Run(mode, func(t *testing.T) {
			s, f := roleSQLService(t, 2)
			original := f.data.clone()
			page, err := s.GetMemberRoles(context.Background(), "usr_admin", "usr_target")
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{"rol_00000"}
			if mode == "noop" {
				ids = append(ids, "rol_00001")
			}
			input := MemberRolesInput{RoleIDs: ids, RoleDefinitions: []MemberRoleDefinitionProof{}, BuiltinDefinitionETag: page.BuiltinRole.DefinitionETag, Reason: "Reviewed reduction"}
			for _, id := range ids {
				for _, row := range page.AssignedRoles {
					if id == row.ID {
						input.RoleDefinitions = append(input.RoleDefinitions, MemberRoleDefinitionProof{id, row.DefinitionETag})
					}
				}
			}
			f.failAudit = mode == "audit_failure"
			f.failConfirmation = mode == "confirmation_outage"
			f.writes = nil
			result, err := s.SetReviewedMemberRoles(context.Background(), "usr_admin", "usr_target", page.ETag, input)
			if mode == "audit_failure" {
				if err == nil || result != nil || !reflect.DeepEqual(f.data, original) {
					t.Fatal("audit failure did not roll back all rows", err)
				}
				return
			}
			if mode == "noop" {
				if err != nil || len(f.writes) != 0 || !reflect.DeepEqual(f.data, original) {
					t.Fatal("noop wrote", err)
				}
				return
			}
			if len(f.data.assignments) != 1 || len(f.data.audits) != 1 || f.data.users["usr_target"].MemberRoleRevision == memberRoleBaseline || !f.data.users["usr_target"].UpdatedAt.Equal(original.users["usr_target"].UpdatedAt) {
				t.Fatal("revision/immutable rows", err)
			}
			if mode == "confirmation_outage" {
				if result != nil || err != memberRolesUnavailable {
					t.Fatal("commit returned invented confirmation", err)
				}
				f.failConfirmation = false
				result, err = s.SetReviewedMemberRoles(context.Background(), "usr_admin", "usr_target", page.ETag, input)
				if err != nil || len(f.data.audits) != 1 {
					t.Fatal("retry reapplied", err)
				}
			}
			if err != nil || result.Confirmation != "current_member_roles" || result.Effect != "current_database" || !slices.Equal(result.RoleIDs, ids) {
				t.Fatal(result, err)
			}
			if last := f.transactions[len(f.transactions)-1]; !last.ReadOnly {
				t.Fatal("no fresh postcommit RR read")
			}
		})
	}
}
func TestMemberRolesExactProtectedAdminCallers(t *testing.T) {
	for _, name := range []string{"save_role", "delete_role", "assign", "registration", "create_admin"} {
		for _, bad := range []string{"alias", "disabled", "offboarded", "member"} {
			t.Run(name+"/"+bad, func(t *testing.T) {
				s, f := roleSQLService(t, 1)
				u := f.data.users["usr_admin"]
				switch bad {
				case "alias":
					f.actorAlias = true
				case "disabled":
					u.Disabled = true
				case "offboarded":
					now := time.Now()
					u.OffboardedAt = &now
				case "member":
					u.Role = entity.RoleMember
				}
				f.data.users[u.ID] = u
				var err error
				switch name {
				case "save_role":
					_, err = s.SaveRole(context.Background(), u.ID, "", "Reader", []string{"members.read"})
				case "delete_role":
					err = s.DeleteRole(context.Background(), u.ID, "rol_00000")
				case "assign":
					_, err = s.SetMemberRoles(context.Background(), u.ID, "usr_target", []string{})
				case "registration":
					err = s.SetRegistrationEnabled(context.Background(), u.ID, true)
				case "create_admin":
					_, err = s.CreateMember(context.Background(), u.ID, "created@example.invalid", "Controlled-Password-123", "Created", entity.RoleAdmin)
				}
				if err == nil || len(f.writes) != 0 {
					t.Fatal("protected helper caller borrowed invalid admin", err)
				}
			})
		}
	}
}

func TestMemberRolesRealDefinitionWriterNoopAndABA(t *testing.T) {
	s, f := roleSQLService(t, 1)
	first := f.data.roles["rol_00000"]
	if _, err := s.SaveRole(context.Background(), "usr_admin", first.ID, first.Name, []string{"members.read"}); err != nil || len(f.writes) != 0 || f.data.roles[first.ID].DefinitionRevision != first.DefinitionRevision {
		t.Fatal("matching definition changed revision/audit", err)
	}
	for _, permissions := range [][]string{{"roles.read"}, {"members.read"}} {
		previous := f.data.roles[first.ID].DefinitionRevision
		if _, err := s.SaveRole(context.Background(), "usr_admin", first.ID, first.Name, permissions); err != nil {
			t.Fatal(err)
		}
		if f.data.roles[first.ID].DefinitionRevision == previous {
			t.Fatal("definition writer ABA unfenced")
		}
	}
	if len(f.data.audits) != 2 || !reflect.DeepEqual(f.data.permissions[first.ID], []string{"members.read"}) || !f.data.roles[first.ID].CreatedAt.Equal(first.CreatedAt) {
		t.Fatal("definition facts")
	}
}
func TestMemberRolesActualBaseWriterAdvancesRevisionAndNoopDoesNot(t *testing.T) {
	s, f := roleSQLService(t, 0)
	before := f.data.users["usr_target"]
	role := entity.RoleAdmin
	if _, err := s.UpdateMember(context.Background(), "usr_admin", before.ID, nil, &role); err != nil {
		t.Fatal(err)
	}
	after := f.data.users[before.ID]
	if after.Role != entity.RoleAdmin || after.MemberRoleRevision == before.MemberRoleRevision || after.PersonalGrantRevision != before.PersonalGrantRevision || !after.CreatedAt.Equal(before.CreatedAt) {
		t.Fatal("base role generation/other identity changed")
	}
	writes := len(f.writes)
	if _, err := s.UpdateMember(context.Background(), "usr_admin", before.ID, nil, &role); err != nil || len(f.writes) != writes || f.data.users[before.ID].MemberRoleRevision != after.MemberRoleRevision {
		t.Fatal("noop base writer revised", err)
	}
}

func TestMemberRolesLiteralCandidatesBoundToReviewAndScopedDetails(t *testing.T) {
	s, f := roleSQLService(t, 3)
	page, err := s.GetMemberRoles(context.Background(), "usr_admin", "usr_target")
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.MemberRoleCandidates(context.Background(), "usr_admin", "usr_target", page.ETag, MemberRoleCandidateFilter{Query: "%_", Limit: 1})
	if err != nil || len(first.Items) != 1 || first.NextCursor == nil {
		t.Fatal(first, err)
	}
	second, err := s.MemberRoleCandidates(context.Background(), "usr_admin", "usr_target", page.ETag, MemberRoleCandidateFilter{Query: "%_", Limit: 1, Cursor: *first.NextCursor})
	if err != nil || len(second.Items) != 1 || second.Items[0].ID <= first.Items[0].ID {
		t.Fatal(second, err)
	}
	for _, query := range []string{"different", "literal"} {
		got, err := s.MemberRoleCandidates(context.Background(), "usr_admin", "usr_target", page.ETag, MemberRoleCandidateFilter{Query: query, Limit: 1, Cursor: *first.NextCursor})
		if got != nil || err != catalogConflict {
			t.Fatal("cursor borrowed another literal query", err)
		}
	}
	detail, err := s.GetMemberRoleDefinition(context.Background(), "usr_admin", "usr_target", first.Items[0].ID, page.ETag)
	if err != nil || detail.Role.DefinitionETag != first.Items[0].DefinitionETag || !slices.Equal(detail.Permissions, []string{"members.read"}) {
		t.Fatal(detail, err)
	}
	for _, id := range []string{strings.ToUpper(first.Items[0].ID), "rol_ABSENT", "rol_absent"} {
		got, err := s.GetMemberRoleDefinition(context.Background(), "usr_admin", "usr_target", id, page.ETag)
		if got != nil || err != apperrors.ErrNotFound {
			t.Fatal("unreviewed detail", err)
		}
	}
	u := f.data.users["usr_target"]
	u.MemberRoleRevision = strings.Repeat("f", 64)
	f.data.users[u.ID] = u
	got, err := s.MemberRoleCandidates(context.Background(), "usr_admin", "usr_target", page.ETag, MemberRoleCandidateFilter{Cursor: *first.NextCursor})
	if got != nil || err != catalogConflict {
		t.Fatal("stale workspace restored candidates", err)
	}
	if len(f.writes) != 0 {
		t.Fatal("read wrote")
	}
}
