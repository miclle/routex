package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Extend the existing finite recipient fake, without a second storage harness.
// No driver, server, network, runtime or integration fixture is invoked.
type teamCreationSQLFixture struct {
	children     []entity.TeamCreationReceiptModel
	users        []entity.User
	applications []entity.RegistrationApprovalApplication
	members      []entity.TeamMembership
	policy       entity.ResourceLimit
	pricing      entity.PricingSetting
	queries      []string
	userIDs      [][]string
	fault        string
}
type teamCreationSQLConnector struct{ fixture *teamCreationSQLFixture }

func (c teamCreationSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return c.connection(), nil
}
func (c teamCreationSQLConnector) Driver() driver.Driver { return teamCreationSQLDriver(c) }
func (c teamCreationSQLConnector) connection() *teamCreationSQLConnection {
	return &teamCreationSQLConnection{teamWarningRecipientSQLConnection: teamWarningRecipientSQLConnection{state: &teamWarningRecipientSQLState{}}, fixture: c.fixture}
}

type teamCreationSQLDriver teamCreationSQLConnector

func (d teamCreationSQLDriver) Open(string) (driver.Conn, error) {
	return teamCreationSQLConnector(d).connection(), nil
}

type teamCreationSQLConnection struct {
	teamWarningRecipientSQLConnection
	fixture *teamCreationSQLFixture
}

func (c *teamCreationSQLConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f := c.fixture
	f.queries = append(f.queries, q)
	if f.fault == "query_error" {
		return nil, errors.New("controlled owner read failure")
	}
	selected := []string{}
	for _, arg := range args {
		if text, ok := arg.Value.(string); ok && !slices.Contains(selected, text) {
			selected = append(selected, text)
		}
	}
	switch {
	case strings.Contains(q, `FROM "team_creation_receipt_models"`):
		if !strings.Contains(q, "LIMIT") || !strings.Contains(q, `"creation_id" =`) {
			return nil, errors.New("unbounded receipt child read")
		}
		rows := []entity.TeamCreationReceiptModel{}
		for _, row := range f.children {
			if slices.Contains(selected, row.CreationID) {
				rows = append(rows, row)
			}
		}
		return effectiveSQLRows(rows)
	case strings.Contains(q, `FROM "users"`):
		if !strings.Contains(q, " IN ") || !strings.Contains(q, `"id" =`) || !strings.Contains(q, "LIMIT") || len(selected) > 500 {
			return nil, errors.New("owner read lost finite exact batch")
		}
		f.userIDs = append(f.userIDs, slices.Clone(selected))
		rows := []entity.User{}
		for _, u := range f.users {
			if slices.Contains(selected, u.ID) {
				rows = append(rows, u)
			}
		}
		if len(rows) > 0 {
			switch f.fault {
			case "alias":
				rows[0].ID = strings.ToUpper(rows[0].ID)
			case "space":
				rows[0].ID += " "
			case "duplicate":
				rows = append(rows, rows[0])
			case "missing":
				rows = rows[1:]
			case "overflow":
				rows = append(rows, entity.User{ID: "usr_unrequested"})
			case "disabled":
				rows[0].Disabled = true
			case "offboarded":
				now := time.Now()
				rows[0].OffboardedAt = &now
			case "zero_birth":
				rows[0].CreatedAt = time.Time{}
			case "lost_precision":
				rows[0].CreatedAt = rows[0].CreatedAt.Add(time.Nanosecond)
			}
		}
		return effectiveSQLRows(rows)
	case strings.Contains(q, `FROM "registration_approval_applications"`):
		rows := []entity.RegistrationApprovalApplication{}
		for _, a := range f.applications {
			if slices.Contains(selected, a.ID) {
				rows = append(rows, a)
			}
		}
		return effectiveSQLRows(rows)
	case strings.Contains(q, `FROM "team_memberships"`):
		return effectiveSQLRows(f.members)
	case strings.Contains(q, `FROM "resource_limits"`):
		return effectiveSQLRows([]entity.ResourceLimit{f.policy})
	case strings.Contains(q, `FROM "pricing_settings"`):
		return effectiveSQLRows([]entity.PricingSetting{f.pricing})
	default:
		return nil, errors.New("unexpected Team creation source read")
	}
}
func teamCreationSQLDatabase(t *testing.T, f *teamCreationSQLFixture) *gorm.DB {
	t.Helper()
	pool := sql.OpenDB(teamCreationSQLConnector{f})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
func TestTeamCreationOwnersCompleteBatchesAndAdmission(t *testing.T) {
	for _, count := range []int{1, 500, 501, 1000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := &teamCreationSQLFixture{}
			ids := make([]string, count)
			born := time.Unix(1700000000, 123000000).UTC()
			reason := "Explicit approval"
			actor := "usr_admin"
			for i := range ids {
				ids[i] = fmt.Sprintf("usr_%026d", i)
				app := fmt.Sprintf("raa_%026d", i)
				f.users = append(f.users, entity.User{ID: ids[i], CreatedAt: born, ApprovalApplicationID: &app})
				f.applications = append(f.applications, entity.RegistrationApprovalApplication{ID: app, UserID: ids[i], UserCreatedAt: born, CreatedAt: born, Revision: memberRoleBaseline, State: "approved", DecidedAt: &born, DecisionActorID: &actor, DecisionReason: &reason})
			}
			owners, err := teamCreationOwners(teamCreationSQLDatabase(t, f), ids, true)
			expected := 2 * ((count + 499) / 500)
			if err != nil || len(owners) != count || len(f.queries) != expected || len(f.userIDs) != (count+499)/500 {
				t.Fatal("complete set/query budget changed", count, len(owners), len(f.queries), err)
			}
			for _, q := range f.queries {
				if strings.Contains(q, `FROM "users"`) && !strings.Contains(q, "FOR UPDATE") {
					t.Fatal("selected owner lock lost")
				}
			}
			if owners[0].ID != ids[0] || owners[count-1].ID != ids[count-1] {
				t.Fatal("exact identities dropped")
			}
		})
	}
	for _, fault := range []string{"alias", "space", "duplicate", "missing", "overflow", "disabled", "offboarded", "zero_birth", "lost_precision", "query_error"} {
		t.Run(fault, func(t *testing.T) {
			f := &teamCreationSQLFixture{users: []entity.User{{ID: "usr_owner", CreatedAt: time.Unix(1700000000, 0)}}, fault: fault}
			owners, err := teamCreationOwners(teamCreationSQLDatabase(t, f), []string{"usr_owner"}, true)
			if err == nil || len(owners) != 0 {
				t.Fatal("incomplete or aliased identity accepted", fault, owners, err)
			}
		})
	}
	for _, state := range []string{"pending", "rejected", "missing", "wrong_birth"} {
		t.Run(state, func(t *testing.T) {
			born := time.Unix(1700000000, 0)
			appID := "raa_00000000000000000000000000"
			reason := "Decision"
			actor := "usr_admin"
			a := entity.RegistrationApprovalApplication{ID: appID, UserID: "usr_owner", UserCreatedAt: born, CreatedAt: born, Revision: memberRoleBaseline, State: "approved", DecidedAt: &born, DecisionActorID: &actor, DecisionReason: &reason}
			f := &teamCreationSQLFixture{users: []entity.User{{ID: "usr_owner", CreatedAt: born, ApprovalApplicationID: &appID}}}
			switch state {
			case "pending":
				a.State = state
				a.DecidedAt = nil
				a.DecisionActorID = nil
				a.DecisionReason = nil
			case "rejected":
				a.State = state
			case "wrong_birth":
				a.UserCreatedAt = born.Add(time.Millisecond)
			}
			if state != "missing" {
				f.applications = []entity.RegistrationApprovalApplication{a}
			}
			if _, err := teamCreationOwners(teamCreationSQLDatabase(t, f), []string{"usr_owner"}, false); !errors.Is(err, apperrors.ErrBadRequest) {
				t.Fatal("unadmitted owner accepted", state, err)
			}
		})
	}
}
func TestTeamCreationReconciliationChecksSeparateBirthsAndRelationship(t *testing.T) {
	for _, name := range []string{"original", "user_birth", "relationship_birth", "relationship_id", "role", "status", "extra_membership", "currency", "revision", "provenance"} {
		t.Run(name, func(t *testing.T) {
			_, receipt, current, _ := teamCreationPublicationFixture(t)
			snapshot, err := readTeamCreationSnapshot(receipt)
			if err != nil {
				t.Fatal(err)
			}
			joined := snapshot.OwnerJoinedAt
			defaultTag := snapshot.DefaultRuleETag
			current.Members = []ResourcePerson{{ID: "tmm_owner", UserID: "usr_owner", Role: entity.TeamOwner, Status: entity.ResourceActive}}
			f := &teamCreationSQLFixture{users: []entity.User{{ID: "usr_owner", CreatedAt: time.UnixMicro(snapshot.Owners[0].UserCreatedAtMicro)}}, members: []entity.TeamMembership{{ID: "tmm_owner", TeamID: receipt.TeamID, UserID: "usr_owner", Role: entity.TeamOwner, Status: entity.ResourceActive, JoinedAt: &joined}}, policy: entity.ResourceLimit{ScopeKind: "team", ScopeID: receipt.TeamID, ETag: snapshot.PolicyETag, AppliedDefaultETag: &defaultTag, IPMode: "none", IPRangesJSON: "[]"}, pricing: entity.PricingSetting{PlatformCurrency: "USD"}}
			switch name {
			case "user_birth":
				f.users[0].CreatedAt = f.users[0].CreatedAt.Add(time.Millisecond)
			case "relationship_birth":
				joined = joined.Add(time.Millisecond)
			case "relationship_id":
				f.members[0].ID = "tmm_rejoined"
			case "role":
				f.members[0].Role = entity.TeamMember
			case "status":
				f.members[0].Status = entity.ResourceDisabled
			case "extra_membership":
				f.members = append(f.members, f.members[0])
			case "revision":
				f.policy.ETag = "lim_new"
			case "provenance":
				f.policy.AppliedDefaultETag = nil
			case "currency":
				money := "1"
				snapshot.Policy.MoneyMonth = &money
				snapshot.Policy.Currency = "USD"
				f.policy.MoneyMonth = &money
				f.policy.Currency = "USD"
				f.pricing.PlatformCurrency = "EUR"
				raw, _ := jsonMarshalTeamCreation(snapshot)
				receipt.SnapshotJSON = raw
			}
			match, err := teamCreationConfigurationMatches(teamCreationSQLDatabase(t, f), receipt, current)
			if err != nil || match != (name == "original") {
				t.Fatal("historical configuration borrowed current identity", name, match, err)
			}
		})
	}
}
func jsonMarshalTeamCreation(snapshot TeamCreationSnapshot) (string, error) {
	raw, err := json.Marshal(snapshot)
	return string(raw), err
}

func TestTeamCreationOwnerQueriesPortableExactIndexNarrowing(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			pool := sql.OpenDB(teamCreationSQLConnector{&teamCreationSQLFixture{}})
			t.Cleanup(func() { _ = pool.Close() })
			var dialector = postgres.New(postgres.Config{Conn: pool})
			if dialect == "mysql" {
				dialector = mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true})
			}
			db, err := gorm.Open(dialector, &gorm.Config{DisableAutomaticPing: true, DryRun: true})
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{"usr_Case", "usr_case", "usr_case "}
			statement := teamCreationOwnerQuery(db, ids, true).Find(&[]entity.User{}).Statement
			query := statement.SQL.String()
			if !strings.Contains(query, " IN ") || !strings.Contains(query, " AND ") || !strings.Contains(query, " OR ") || !strings.Contains(query, "FOR UPDATE") {
				t.Fatal("index superset replaced exact conjunction", query)
			}
			if dialect == "mysql" && strings.Count(query, "AS BINARY") != 2*len(ids) {
				t.Fatal("exact case/trailing-space predicates lost", query)
			}
			count := map[string]int{}
			for _, value := range statement.Vars {
				if text, ok := value.(string); ok {
					count[text]++
				}
			}
			for _, value := range ids {
				if count[value] != 2 || strings.Contains(query, value) {
					t.Fatal("identity bind lost or interpolated", count, query)
				}
			}
			empty := teamCreationOwnerQuery(db, nil, false).Find(&[]entity.User{}).Statement
			if !strings.Contains(empty.SQL.String(), "1 = 0") || len(empty.Vars) != 0 {
				t.Fatal("empty selection read directory", empty.SQL.String())
			}
		})
	}
}

func TestTeamCreationEmptyReceiptRejectsUnexpectedChildren(t *testing.T) {
	_, receipt, _, _ := teamCreationPublicationFixture(t)
	for _, corrupt := range []bool{false, true} {
		f := &teamCreationSQLFixture{}
		if corrupt {
			f.children = []entity.TeamCreationReceiptModel{{CreationID: receipt.CreationID, ModelID: "mdl_injected", ModelCreatedAt: time.Now()}}
		}
		rows, err := readTeamCreationReceiptModels(teamCreationSQLDatabase(t, f), receipt)
		if corrupt && err == nil || !corrupt && (err != nil || len(rows) != 0) || len(f.queries) != 1 {
			t.Fatal("legacy corruption normalized to empty", corrupt, rows, err, len(f.queries))
		}
	}
}
