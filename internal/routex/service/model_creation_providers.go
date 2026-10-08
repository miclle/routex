package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ExactID reads one retained identity without letting name search aliases hide
// the selected target. Ordinary paged filters remain separate and unchanged.
type ModelAccessPickerFilter struct {
	ModelCreationFilter
	ExactID string
}

func normalizeModelAccessPickerFilter(f ModelAccessPickerFilter, prefix string) (ModelAccessPickerFilter, string, error) {
	if f.ExactID != "" && (!modelCreationID(f.ExactID, prefix) || f.Query != "" || f.Cursor != "") {
		return f, "", apperrors.ErrBadRequest
	}
	ordinary, pattern, err := normalizeModelCreationFilter(f.ModelCreationFilter, prefix)
	f.ModelCreationFilter = ordinary
	if f.ExactID != "" {
		f.Limit = 1
	}
	return f, pattern, err
}

type ModelCreationProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type ModelCreationProviderPage struct {
	Items      []ModelCreationProvider `json:"items"`
	NextCursor *string                 `json:"next_cursor"`
}

// Read only Provider identity, including Providers without Connections. Never
// load their credentials, model catalogues or directory-wide child counts.
func (s *Service) ListModelCreationProviders(ctx context.Context, actor string, filter ModelAccessPickerFilter) (*ModelCreationProviderPage, error) {
	f, pattern, err := normalizeModelAccessPickerFilter(filter, "prv")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := &ModelCreationProviderPage{Items: []ModelCreationProvider{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := modelCreationRead(tx, actor); err != nil {
			return err
		}
		if f.Cursor != "" {
			var cursor entity.Provider
			if err := personalExact(modelCreationDB(tx), "id", f.Cursor).Take(&cursor).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return apperrors.ErrBadRequest
				}
				return err
			}
		}
		query := modelAccessProviderQuery(tx, f, pattern)
		if err := query.Scan(&result.Items).Error; err != nil {
			return err
		}
		result.Items, result.NextCursor = modelCreationPage(result.Items, f.Limit, func(row ModelCreationProvider) string { return row.ID })
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}

func modelAccessProviderQuery(tx *gorm.DB, f ModelAccessPickerFilter, namePattern string) *gorm.DB {
	if f.ExactID != "" {
		return modelCreationDB(tx).Model(&entity.Provider{}).Select("id", "name").Where(database.ExactText(tx, clause.Column{Name: "id"}, f.ExactID)).Limit(1)
	}
	return modelCreationProviderQuery(tx, f.ModelCreationFilter, namePattern)
}

func modelCreationProviderQuery(tx *gorm.DB, f ModelCreationFilter, namePattern string) *gorm.DB {
	query := modelCreationDB(tx).Model(&entity.Provider{}).Select("id", "name")
	if f.Query != "" {
		query = query.Where("LOWER(name) LIKE ? ESCAPE '!' OR ?", namePattern, database.ExactTextPrefix(tx, clause.Column{Name: "id"}, f.Query))
	}
	if f.Cursor != "" {
		query = query.Where("id > ?", f.Cursor)
	}
	return query.Order("id").Limit(f.Limit + 1)
}

type ModelCreationEgress struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}
type ModelCreationEgressPage struct {
	Items      []ModelCreationEgress `json:"items"`
	NextCursor *string               `json:"next_cursor"`
}

func (s *Service) ListModelCreationEgresses(ctx context.Context, actor string, filter ModelAccessPickerFilter) (*ModelCreationEgressPage, error) {
	f, pattern, err := normalizeModelAccessPickerFilter(filter, "egr")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := &ModelCreationEgressPage{Items: []ModelCreationEgress{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := modelCreationRead(tx, actor); err != nil {
			return err
		}
		if f.Cursor != "" {
			var cursor entity.Egress
			if err := personalExact(modelCreationDB(tx), "id", f.Cursor).Take(&cursor).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return apperrors.ErrBadRequest
				}
				return err
			}
		}
		if err := modelAccessEgressQuery(tx, f, pattern).Scan(&result.Items).Error; err != nil {
			return err
		}
		result.Items, result.NextCursor = modelCreationPage(result.Items, f.Limit, func(row ModelCreationEgress) string { return row.ID })
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}

func modelAccessEgressQuery(tx *gorm.DB, f ModelAccessPickerFilter, namePattern string) *gorm.DB {
	query := modelCreationDB(tx).Model(&entity.Egress{}).Select("id", "name", "enabled")
	if f.ExactID != "" {
		return query.Where(database.ExactText(tx, clause.Column{Name: "id"}, f.ExactID)).Limit(1)
	}
	query = query.Where("LOWER(name) LIKE ? ESCAPE '!' OR ?", namePattern, database.ExactTextPrefix(tx, clause.Column{Name: "id"}, f.Query))
	if f.Cursor != "" {
		query = query.Where("id > ?", f.Cursor)
	}
	return query.Order("id").Limit(f.Limit + 1)
}
