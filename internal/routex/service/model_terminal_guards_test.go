package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var errTerminalGuardDownstream = errors.New("controlled downstream statement")

// Reuse the transactional SQL fixture, but refuse every mutation. Reaching that
// refusal proves disabled editing still proceeds; archived editing must stop at
// its locked Model without reaching any mutation, even for an equal-state intent.
type terminalGuardConnector struct{ fixture *weightSQLFixture }

func (c terminalGuardConnector) Connect(context.Context) (driver.Conn, error) {
	return &terminalGuardConnection{weightSQLConnection: &weightSQLConnection{f: c.fixture}}, nil
}
func (c terminalGuardConnector) Driver() driver.Driver { return terminalGuardDriver(c) }

type terminalGuardDriver terminalGuardConnector

func (c terminalGuardDriver) Open(string) (driver.Conn, error) {
	return terminalGuardConnector(c).Connect(context.Background())
}

type terminalGuardConnection struct{ *weightSQLConnection }

func (c *terminalGuardConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(query, "INSERT ") || strings.HasPrefix(query, "UPDATE ") || strings.HasPrefix(query, "DELETE ") {
		c.f.writes = append(c.f.writes, query)
		return nil, errTerminalGuardDownstream
	}
	if strings.Contains(query, `FROM "user_model_grants"`) {
		c.f.queries = append(c.f.queries, query)
		return effectiveSQLRows([]entity.UserModelGrant{})
	}
	if strings.Contains(query, `FROM "model_names"`) {
		c.f.queries = append(c.f.queries, query)
		rows := []entity.ModelName{}
		values := rolesSQLStrings(args)
		for _, row := range c.current().Names {
			if strings.Contains(query, "current_model_id") {
				if row.CurrentModelID != nil && slices.Contains(values, *row.CurrentModelID) {
					rows = append(rows, row)
				}
			} else if slices.Contains(values, row.Name) {
				rows = append(rows, row)
			}
		}
		return effectiveSQLRows(rows)
	}
	if strings.HasPrefix(query, "SELECT DISTINCT") {
		c.f.queries = append(c.f.queries, query)
		return nil, errTerminalGuardDownstream
	}
	return c.weightSQLConnection.QueryContext(ctx, query, args)
}
func (c *terminalGuardConnection) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.f.writes = append(c.f.writes, query)
	return nil, errTerminalGuardDownstream
}

func terminalGuardService(t *testing.T, status string) (*Service, *weightSQLFixture) {
	t.Helper()
	s, fixture := weightSQLService(t)
	fixture.data.Model.Status = status
	modelID := fixture.data.Model.ID
	future := time.Now().UTC().Add(time.Hour)
	fixture.data.Names = []entity.ModelName{
		{Name: "current", ModelID: modelID, CurrentModelID: &modelID, CreatedAt: fixture.data.Model.CreatedAt},
		{Name: "old/name", ModelID: modelID, ExpiresAt: &future, CreatedAt: fixture.data.Model.CreatedAt},
	}
	pool := sql.OpenDB(terminalGuardConnector{fixture})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool, PreferSimpleProtocol: true}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	s.db = db
	return s, fixture
}

func TestTerminalModelGuardsUseLockedStateBeforeWritesAndNoops(t *testing.T) {
	cases := []struct {
		name string
		run  func(*Service, *weightSQLFixture) error
	}{
		{"binding", func(s *Service, f *weightSQLFixture) error {
			_, err := s.AddModelBinding(context.Background(), f.data.Actor.ID, f.data.Model.ID, f.data.Models[0].ID)
			return err
		}},
		{"weights", func(s *Service, f *weightSQLFixture) error {
			_, err := s.SetModelWeights(context.Background(), f.data.Actor.ID, f.data.Model.ID, []ModelWeight{{BindingID: f.data.Bindings[0].ID, Weight: f.data.Bindings[0].Weight}})
			return err
		}},
		{"rename", func(s *Service, f *weightSQLFixture) error {
			_, err := s.RenameModel(context.Background(), f.data.Actor.ID, f.data.Model.ID, "renamed", nil)
			return err
		}},
		{"rename_equal_state", func(s *Service, f *weightSQLFixture) error {
			_, err := s.RenameModel(context.Background(), f.data.Actor.ID, f.data.Model.ID, "current", nil)
			return err
		}},
		{"grants_equal_empty", func(s *Service, f *weightSQLFixture) error {
			return s.db.Transaction(func(tx *gorm.DB) error {
				_, err := setCatalogModelGrants(tx, f.data.Actor.ID, f.data.Model.ID, nil)
				return err
			})
		}},
		{"alias", func(s *Service, f *weightSQLFixture) error {
			_, err := s.RetireModelAlias(context.Background(), f.data.Actor.ID, f.data.Model.ID, strings.Repeat("a", 64), ModelAliasRetirementInput{Name: "old/name", Reason: "Stop compatibility"})
			return err
		}},
		{"alias_already_retired", func(s *Service, f *weightSQLFixture) error {
			f.data.Names[1].ExpiresAt = nil
			_, err := s.RetireModelAlias(context.Background(), f.data.Actor.ID, f.data.Model.ID, strings.Repeat("a", 64), ModelAliasRetirementInput{Name: "old/name", Reason: "Stop compatibility"})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, fixture := terminalGuardService(t, entity.ResourceArchived)
			if tc.name == "alias_already_retired" {
				fixture.data.Names[1].ExpiresAt = nil
			}
			before := fixture.data.clone()
			if err := tc.run(s, fixture); err != catalogConflict {
				t.Fatal("archived Model did not reject with catalog conflict", err)
			}
			if len(fixture.writes) != 0 || !reflect.DeepEqual(before, fixture.data) {
				t.Fatal("archived mutation reached a write or changed retained facts", fixture.writes)
			}
			locked := false
			for _, query := range fixture.queries {
				locked = locked || strings.Contains(query, `FROM "models"`) && strings.HasSuffix(query, " FOR UPDATE")
			}
			if !locked {
				t.Fatal("status guard did not read locked Model", fixture.queries)
			}
			if tc.name == "weights" && slices.ContainsFunc(fixture.queries, func(query string) bool { return strings.Contains(query, `FROM "model_provider_bindings"`) }) {
				t.Fatal("archived weights continued into topology reads", fixture.queries)
			}
		})
	}
}

func TestTerminalModelGuardsPreserveDisabledEditing(t *testing.T) {
	for _, operation := range []string{"binding", "weights", "rename", "grants", "alias"} {
		t.Run(operation, func(t *testing.T) {
			s, fixture := terminalGuardService(t, entity.ResourceDisabled)
			var err error
			switch operation {
			case "binding":
				_, err = s.AddModelBinding(context.Background(), fixture.data.Actor.ID, fixture.data.Model.ID, fixture.data.Models[0].ID)
			case "weights":
				_, err = s.SetModelWeights(context.Background(), fixture.data.Actor.ID, fixture.data.Model.ID, []ModelWeight{{BindingID: fixture.data.Bindings[0].ID, Weight: 100}})
			case "rename":
				_, err = s.RenameModel(context.Background(), fixture.data.Actor.ID, fixture.data.Model.ID, "renamed", nil)
			case "grants":
				err = s.db.Transaction(func(tx *gorm.DB) error {
					_, err := setCatalogModelGrants(tx, fixture.data.Actor.ID, fixture.data.Model.ID, nil)
					return err
				})
			case "alias":
				subject := modelAliasSubject{Model: fixture.data.Model, Current: fixture.data.Names[0], Selected: fixture.data.Names[1]}
				review := modelAliasReview(subject, time.Now(), true)
				if !review.CanRetire {
					t.Fatal("disabled compatibility alias became read-only")
				}
				_, err = s.RetireModelAlias(context.Background(), fixture.data.Actor.ID, fixture.data.Model.ID, review.ETag, ModelAliasRetirementInput{Name: "old/name", Reason: "Stop compatibility"})
			}
			if operation == "grants" {
				if err != nil {
					t.Fatal("disabled equal-state grants rejected", err)
				}
			} else if operation == "weights" {
				if !slices.ContainsFunc(fixture.queries, func(query string) bool { return strings.HasPrefix(query, "SELECT DISTINCT") }) || err == nil || err == catalogConflict {
					t.Fatal("disabled weights did not reach downstream connection review", err)
				}
			} else if len(fixture.writes) != 1 || err == nil || err == catalogConflict {
				t.Fatal("disabled editing did not reach controlled write refusal", err, fixture.writes)
			}
		})
	}
}

func TestArchivedModelAliasReviewIsReadOnly(t *testing.T) {
	subject := aliasSubjectFixture()
	subject.Model.Status = entity.ResourceArchived
	review := modelAliasReview(subject, subject.Model.CreatedAt, true)
	if review.State != "compatibility" || review.CanRetire || review.CurrentName != subject.Current.Name || review.Alias.Name != subject.Selected.Name {
		t.Fatal("archive hid alias history or offered retirement", review)
	}
}
