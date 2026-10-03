package service

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

// ProjectListLimits contains stored controls only. A nil summary means no read
// authority; Stored=false means no policy row, never an inferred default policy.
type ProjectListLimits struct {
	Stored      bool    `json:"stored"`
	TokensMonth *int64  `json:"tokens_month"`
	MoneyMonth  *string `json:"money_month"`
	Currency    string  `json:"currency"`
	RPM         *int64  `json:"rpm"`
	TPM         *int64  `json:"tpm"`
}

func projectListPolicy(row entity.ResourceLimit) (*ProjectListLimits, error) {
	if _, err := policyFromRow(row); err != nil {
		return nil, err
	}
	return &ProjectListLimits{Stored: true, TokensMonth: row.TokensMonth, MoneyMonth: row.MoneyMonth, Currency: row.Currency, RPM: row.RPM, TPM: row.TPM}, nil
}

// Keep every batch join and owner selection exact on both supported collations.
func projectListScope(db *gorm.DB, column clause.Column, ids []string) *gorm.DB {
	predicates := make([]clause.Expression, 0, len(ids))
	for _, value := range ids {
		predicates = append(predicates, database.ExactText(db, column, value))
	}
	return db.Where(clause.Or(predicates...))
}

func (s *Service) listProjects(ctx context.Context, actorID string, all bool, filter ResourceFilter) (*ResourcePage, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	page := &ResourcePage{Items: []ResourceRecord{}}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		readAll, err := exactGovernancePermission(tx, actor, "projects.read_all")
		if err != nil {
			return err
		}
		if all && !readAll {
			return apperrors.ErrForbidden
		}
		query := tx.Table("projects")
		if !all {
			managers := tx.Model(&entity.ProjectManager{}).Select("1").
				Where(database.ExactText(tx, clause.Column{Name: "user_id"}, actorID)).
				Where(database.ExactTextColumns(tx, clause.Column{Table: "project_managers", Name: "project_id"}, clause.Column{Table: "projects", Name: "id"}))
			query = query.Where("EXISTS (?)", managers)
		}
		if filter.Query != "" {
			escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(filter.Query))
			query = query.Where(clause.Or(
				clause.Expr{SQL: "LOWER(name) LIKE ? ESCAPE '!'", Vars: []any{"%" + escaped + "%"}},
				database.ExactTextContains(tx, clause.Column{Name: "id"}, filter.Query),
			))
		}
		if filter.Status != "" {
			query = query.Where(database.ExactText(tx, clause.Column{Name: "status"}, filter.Status))
		}
		if filter.Cursor != "" {
			query = query.Where("id > ?", filter.Cursor)
		}
		if err := query.Order("id").Limit(filter.Limit + 1).Find(&page.Items).Error; err != nil {
			return err
		}
		if len(page.Items) > filter.Limit {
			page.Items = page.Items[:filter.Limit]
			page.NextCursor = page.Items[len(page.Items)-1].ID
		}
		if len(page.Items) == 0 {
			return nil
		}
		return projectListFacts(tx, actor, all, page.Items)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	return page, nil
}

func projectListFacts(tx *gorm.DB, actor entity.User, all bool, items []ResourceRecord) error {
	ids := make([]string, len(items))
	indices := make(map[string]int, len(items))
	for index := range items {
		ids[index] = items[index].ID
		indices[items[index].ID] = index
		items[index].Managers = []ResourcePerson{}
		items[index].ModelIDs = []string{}
	}
	var managers []struct {
		ProjectID      string
		ResourcePerson `gorm:"embedded"`
	}
	query := projectListScope(tx.Table("project_managers m"), clause.Column{Table: "m", Name: "project_id"}, ids).
		Select("m.project_id,m.id,m.user_id,u.name,u.email").Joins("JOIN users u ON u.id = m.user_id").
		Where(database.ExactTextColumns(tx, clause.Column{Table: "u", Name: "id"}, clause.Column{Table: "m", Name: "user_id"})).Order("m.id")
	if err := query.Scan(&managers).Error; err != nil {
		return err
	}
	own := make(map[string]bool, len(items))
	for _, manager := range managers {
		index, ok := indices[manager.ProjectID]
		if !ok {
			continue
		}
		items[index].Managers = append(items[index].Managers, manager.ResourcePerson)
		if manager.UserID == actor.ID {
			own[manager.ProjectID] = true
		}
	}
	var grants []entity.ProjectModelGrant
	query = projectListScope(tx.Table("project_model_grants g"), clause.Column{Table: "g", Name: "project_id"}, ids).
		Select("g.project_id,g.model_id").Joins("JOIN models m ON m.id = g.model_id").
		Where(database.ExactTextColumns(tx, clause.Column{Table: "m", Name: "id"}, clause.Column{Table: "g", Name: "model_id"})).Order("g.model_id")
	if err := query.Scan(&grants).Error; err != nil {
		return err
	}
	for _, grant := range grants {
		if index, ok := indices[grant.ProjectID]; ok {
			items[index].ModelIDs = append(items[index].ModelIDs, grant.ModelID)
		}
	}
	keyWriter, err := exactGovernancePermission(tx, actor, "projects.write")
	if err != nil {
		return err
	}
	keyIDs := make([]string, 0, len(items))
	for index := range items {
		if own[items[index].ID] || keyWriter {
			items[index].KeyCount = new(int64)
			keyIDs = append(keyIDs, items[index].ID)
		}
	}
	if len(keyIDs) > 0 {
		var counts []struct {
			ProjectID string
			Total     int64
		}
		query = projectListScope(tx.Model(&entity.ProjectKey{}), clause.Column{Name: "project_id"}, keyIDs).
			Select("project_id,COUNT(*) AS total").Group("project_id")
		if err := query.Scan(&counts).Error; err != nil {
			return err
		}
		for _, count := range counts {
			if index, ok := indices[count.ProjectID]; ok && items[index].KeyCount != nil {
				*items[index].KeyCount = count.Total
			}
		}
	}
	// The administrator endpoint requires projects.read_all, which independently
	// authorizes Project limit reads. Personal lists expose no policy summaries.
	if !all {
		return nil
	}
	for index := range items {
		items[index].ProjectListLimits = &ProjectListLimits{}
	}
	var policies []entity.ResourceLimit
	query = projectListScope(tx.Model(&entity.ResourceLimit{}), clause.Column{Name: "scope_id"}, ids).
		Where(database.ExactText(tx, clause.Column{Name: "scope_kind"}, "project"))
	if err := query.Find(&policies).Error; err != nil {
		return err
	}
	for _, row := range policies {
		if index, ok := indices[row.ScopeID]; ok {
			summary, err := projectListPolicy(row)
			if err != nil {
				return err
			}
			items[index].ProjectListLimits = summary
		}
	}
	return nil
}
