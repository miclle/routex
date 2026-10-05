package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Count database operations on this isolated handle, below authDB's deliberately
// discarded SQL logger. The publisher's handle and callback registry stay intact.
type roleListCountPool struct {
	gorm.ConnPool
	statements *atomic.Int64
}

func (p *roleListCountPool) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	p.statements.Add(1)
	return p.ConnPool.ExecContext(ctx, query, args...)
}

func (p *roleListCountPool) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	p.statements.Add(1)
	return p.ConnPool.QueryContext(ctx, query, args...)
}

func (p *roleListCountPool) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	p.statements.Add(1)
	return p.ConnPool.QueryRowContext(ctx, query, args...)
}

func (p *roleListCountPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	var tx gorm.ConnPool
	var err error
	switch pool := p.ConnPool.(type) {
	case gorm.TxBeginner:
		tx, err = pool.BeginTx(ctx, options)
	case gorm.ConnPoolBeginner:
		tx, err = pool.BeginTx(ctx, options)
	default:
		return nil, gorm.ErrInvalidTransaction
	}
	if err != nil {
		return nil, err
	}
	committer, ok := tx.(gorm.TxCommitter)
	if !ok {
		return nil, gorm.ErrInvalidTransaction
	}
	return &roleListCountTransaction{roleListCountPool: &roleListCountPool{ConnPool: tx, statements: p.statements}, TxCommitter: committer}, nil
}

type roleListCountTransaction struct {
	*roleListCountPool
	gorm.TxCommitter
}

func testRoleListMemberCounts(t *testing.T, db *gorm.DB, router *fox.Engine, actorID string, cookie *http.Cookie, csrf, readerID, writerID string) {
	t.Helper()
	ctx := context.Background()
	counter := &atomic.Int64{}
	readDB := db.Session(&gorm.Session{NewDB: true, Context: ctx})
	countPool := &roleListCountPool{ConnPool: readDB.Statement.ConnPool, statements: counter}
	readDB.ConnPool, readDB.Statement.ConnPool = countPool, countPool
	readService, err := service.New(ctx, readDB)
	if err != nil {
		t.Fatal(err)
	}
	before, err := readService.ListRoles(ctx, actorID)
	if err != nil {
		t.Fatal(err)
	}
	counts := func(rows []service.RoleRecord) map[string]int64 {
		result := map[string]int64{}
		for _, row := range rows {
			if row.MemberCount == nil {
				t.Fatal("list omitted an authoritative count", row.Role.ID)
			}
			result[row.Role.ID] = *row.MemberCount
		}
		return result
	}
	want := counts(before)
	var prototype entity.User
	if err := db.Where(database.ExactText(db, clause.Column{Name: "id"}, actorID)).First(&prototype).Error; err != nil {
		t.Fatal(err)
	}
	users, roleIDs, applicationIDs := []string{}, []string{}, []string{}
	teamID := "team_role_list_count"
	t.Cleanup(func() {
		for _, operation := range []func() error{
			func() error { return db.Where("team_id = ?", teamID).Delete(&entity.TeamRole{}).Error },
			func() error { return db.Where("team_id = ?", teamID).Delete(&entity.TeamMembership{}).Error },
			func() error { return db.Where("id = ?", teamID).Delete(&entity.Team{}).Error },
			func() error { return db.Where("user_id IN ?", users).Delete(&entity.UserRole{}).Error },
			func() error {
				return db.Where("id IN ?", applicationIDs).Delete(&entity.RegistrationApprovalApplication{}).Error
			},
			func() error { return db.Where("id IN ?", users).Delete(&entity.User{}).Error },
			func() error { return db.Where("role_id IN ?", roleIDs).Delete(&entity.RolePermission{}).Error },
			func() error { return db.Where("id IN ?", roleIDs).Delete(&entity.Role{}).Error },
		} {
			if err := operation(); err != nil {
				t.Error("owned Role-list fixture cleanup failed", err)
			}
		}
	})
	for _, state := range []string{"disabled", "offboarded", "pending", "rejected"} {
		u := entity.User{ID: "usr_role_count_" + state, Email: "role-count-" + state + "@example.invalid", Name: "Retained " + state, Role: entity.RoleMember, PasswordHash: prototype.PasswordHash, CreatedAt: prototype.CreatedAt, UpdatedAt: prototype.UpdatedAt}
		if state == "disabled" {
			u.Disabled = true
		}
		if state == "offboarded" {
			u.OffboardedAt = &u.CreatedAt
		}
		if err := db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
		users = append(users, u.ID)
		if state == "pending" || state == "rejected" {
			applicationID, err := id.NewPrefixed("raa")
			if err != nil {
				t.Fatal(err)
			}
			a := entity.RegistrationApprovalApplication{ID: applicationID, UserID: u.ID, UserCreatedAt: u.CreatedAt, CreatedAt: u.CreatedAt, State: state}
			if state == "rejected" {
				reason := "Retained Role-list fixture rejection"
				a.DecidedAt, a.DecisionActorID, a.DecisionReason = &u.CreatedAt, &actorID, &reason
			}
			if err := db.Omit("User").Create(&a).Error; err != nil {
				t.Fatal(err)
			}
			applicationIDs = append(applicationIDs, a.ID)
			if err := db.Model(&entity.User{}).Where(database.ExactText(db, clause.Column{Name: "id"}, u.ID)).UpdateColumn("approval_application_id", a.ID).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Create(&entity.UserRole{UserID: u.ID, RoleID: readerID}).Error; err != nil {
			t.Fatal(err)
		}
		want["rol_member"]++
		want[readerID]++
	}
	if err := db.Create(&entity.Team{ID: teamID, Name: "Role-list count scope", Status: entity.ResourceActive}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Omit("Team", "Role").Create(&entity.TeamRole{TeamID: teamID, RoleID: writerID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.TeamMembership{ID: "tmb_role_list_count", TeamID: teamID, UserID: users[2], Role: entity.TeamMember, Status: entity.ResourceActive}).Error; err != nil {
		t.Fatal(err)
	}
	// Some supported collations permit these foreign-key aliases; exact joins
	// must exclude them. Other drivers correctly reject them at the constraint.
	for _, alias := range []entity.UserRole{{UserID: strings.ToUpper(users[0]), RoleID: writerID}, {UserID: users[1], RoleID: strings.ToUpper(writerID)}} {
		if err := db.Create(&alias).Error; err != nil && !errors.Is(err, gorm.ErrForeignKeyViolated) {
			t.Fatal("unexpected alias fixture result", err)
		}
	}
	response := identityRequest(router, "GET", "/api/v1/admin/roles", "", cookie, csrf)
	list := decodeCatalogResponse[RolesResponse](t, response, 200)
	for _, row := range list.Items {
		if row.MemberCount == nil || *row.MemberCount != want[row.ID] {
			t.Fatal("scope or retained-count mismatch", row.ID, row.MemberCount, want[row.ID])
		}
	}
	for _, total := range []int{500, 501, 1000, 1001} {
		var existing int64
		if err := db.Model(&entity.Role{}).Count(&existing).Error; err != nil {
			t.Fatal(err)
		}
		seeds := make([]entity.Role, 0, total-int(existing))
		for i := int(existing); i < total; i++ {
			roleID := fmt.Sprintf("rol_list_count_%04d", i)
			seeds = append(seeds, entity.Role{ID: roleID, Name: fmt.Sprintf("List-count %04d", i), NameKey: fmt.Sprintf("list-count %04d", i)})
			roleIDs = append(roleIDs, roleID)
		}
		if len(seeds) > 0 {
			if err := db.CreateInBatches(&seeds, 200).Error; err != nil {
				t.Fatal(err)
			}
		}
		counter.Store(0)
		rows, err := readService.ListRoles(ctx, actorID)
		if total == 1001 {
			var app *apperrors.Error
			if rows != nil || !errors.As(err, &app) || app.Code != 422 {
				t.Fatal("catalogue overflow returned a partial list", rows, err)
			}
			expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/roles", "", cookie, csrf), 422)
			continue
		}
		budget := int64(5 + (total+499)/500)
		if err != nil || len(rows) != total || counter.Load() != budget {
			t.Fatal("real Role-list query budget or completeness", total, len(rows), counter.Load(), budget, err)
		}
		for _, row := range rows {
			if row.MemberCount == nil || *row.MemberCount != want[row.Role.ID] {
				t.Fatal("invented or missing member count", row.Role.ID)
			}
		}
		t.Logf("complete Role-list catalogue=%d statements=%d", total, budget)
	}
}
