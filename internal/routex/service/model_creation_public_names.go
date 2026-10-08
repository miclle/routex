package service

import (
	"context"
	"database/sql"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/modelreferences"
	"gorm.io/gorm"
)

type ModelCreationPublicName struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
}
type ModelCreationPublicNames struct {
	ConnectionID string                    `json:"connection_id"`
	Query        string                    `json:"query"`
	Items        []ModelCreationPublicName `json:"items"`
}

// Only maintained advisory identities are candidates. A query cannot enumerate
// arbitrary reservations; the confirmed creation preview remains authoritative.
func (s *Service) ListModelCreationPublicNames(ctx context.Context, actor, connectionID, query string) (*ModelCreationPublicNames, error) {
	if !modelCreationID(connectionID, "con") || !modelreferences.ValidQuery(query) {
		return nil, apperrors.ErrBadRequest
	}
	references, err := modelreferences.Embedded()
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	candidates := references.Search(query)
	result := &ModelCreationPublicNames{ConnectionID: connectionID, Query: query, Items: []ModelCreationPublicName{}}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := modelCreationRead(tx, actor); err != nil {
			return err
		}
		if _, _, err := modelCreationConnection(tx, connectionID); err != nil {
			return err
		}
		if len(candidates) == 0 {
			return nil
		}
		// One exact bounded batch includes expired aliases: reservation never expires.
		var rows []entity.ModelName
		if err := modelCreationDB(tx).Model(&entity.ModelName{}).Select("name").Where(memberModelsExactIDs(tx, "name", candidates)).Limit(9).Find(&rows).Error; err != nil {
			return err
		}
		var err error
		result.Items, err = projectModelCreationPublicNames(candidates, rows)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
func projectModelCreationPublicNames(candidates []string, rows []entity.ModelName) ([]ModelCreationPublicName, error) {
	if len(candidates) > 8 || len(rows) > len(candidates) {
		return nil, apperrors.ErrInternal
	}
	selected := map[string]bool{}
	reserved := map[string]bool{}
	for _, name := range candidates {
		if selected[name] {
			return nil, apperrors.ErrInternal
		}
		selected[name] = true
	}
	for _, row := range rows {
		if !selected[row.Name] || reserved[row.Name] {
			return nil, apperrors.ErrInternal
		}
		reserved[row.Name] = true
	}
	result := make([]ModelCreationPublicName, 0, len(candidates))
	for _, name := range candidates {
		result = append(result, ModelCreationPublicName{Name: name, Available: !reserved[name]})
	}
	return result, nil
}
