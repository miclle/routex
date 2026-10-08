package service

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
)

const modelMonthlyBatchLimit = 500

type ModelMonthlyRequestCount struct {
	ModelID  string `json:"model_id"`
	Requests string `json:"requests"`
}

type ModelMonthlyRequests struct {
	Items      []ModelMonthlyRequestCount `json:"items"`
	PeriodFrom time.Time                  `json:"period_from"`
	PeriodTo   time.Time                  `json:"period_to"`
	AsOf       time.Time                  `json:"as_of"`
	Timezone   string                     `json:"timezone"`
	Source     string                     `json:"source"`
	MayLag     bool                       `json:"may_lag"`
}

type modelMonthlyFact struct {
	RequestID string
	ModelID   string
	StartedAt time.Time
}

func modelMonthlyTargets(ids []string) (map[string]struct{}, error) {
	if len(ids) < 1 || len(ids) > modelMonthlyBatchLimit {
		return nil, apperrors.ErrBadRequest
	}
	targets := make(map[string]struct{}, len(ids))
	for _, modelID := range ids {
		if !validAdminModelTarget(modelID) {
			return nil, apperrors.ErrBadRequest
		}
		if _, exists := targets[modelID]; exists {
			return nil, apperrors.ErrBadRequest
		}
		targets[modelID] = struct{}{}
	}
	return targets, nil
}

func modelMonthlyCounts(ids []string, facts []modelMonthlyFact, from, until time.Time) ([]ModelMonthlyRequestCount, error) {
	if len(facts) > usageRowLimit {
		return nil, &apperrors.Error{Code: 422, Message: "The complete monthly Model request query exceeds the supported limit."}
	}
	targets, err := modelMonthlyTargets(ids)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int64, len(ids))
	seen := make(map[string]struct{}, len(facts))
	for _, fact := range facts {
		// A database collation may return a superset; never borrow facts from
		// a differently cased Model ID or an alias.
		if _, exact := targets[fact.ModelID]; !exact {
			continue
		}
		if fact.RequestID == "" || fact.StartedAt.Before(from) || !fact.StartedAt.Before(until) {
			return nil, apperrors.ErrInternal
		}
		if _, duplicate := seen[fact.RequestID]; duplicate {
			return nil, apperrors.ErrInternal
		}
		seen[fact.RequestID] = struct{}{}
		counts[fact.ModelID]++
	}
	items := make([]ModelMonthlyRequestCount, 0, len(ids))
	for _, modelID := range ids {
		items = append(items, ModelMonthlyRequestCount{ModelID: modelID, Requests: strconv.FormatInt(counts[modelID], 10)})
	}
	return items, nil
}

// AdminModelMonthlyRequests counts persisted logical calls, regardless of
// attempts or outcome. This authority is independent from catalog read access.
func (s *Service) AdminModelMonthlyRequests(ctx context.Context, actorID string, ids []string) (*ModelMonthlyRequests, error) {
	targets, err := modelMonthlyTargets(ids)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := &ModelMonthlyRequests{PeriodFrom: from, PeriodTo: now, AsOf: now, Timezone: "UTC", Source: "persisted_call_records", MayLag: true}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		for _, permission := range []string{"models.read_all", "calls.read_all"} {
			if err := exactCatalogPermission(modelCreationDB(tx), actorID, permission); err != nil {
				return err
			}
		}
		var models []entity.Model
		if err := modelCreationDB(tx).Select("id").Where("id IN ?", ids).Limit(modelMonthlyBatchLimit + 1).Find(&models).Error; err != nil {
			return err
		}
		found := make(map[string]struct{}, len(models))
		for _, model := range models {
			if _, exact := targets[model.ID]; exact {
				found[model.ID] = struct{}{}
			}
		}
		if len(models) > modelMonthlyBatchLimit || len(found) != len(targets) {
			return apperrors.ErrNotFound
		}
		var facts []modelMonthlyFact
		if err := modelCreationDB(tx).Model(&entity.CallRecord{}).Select("request_id", "model_id", "started_at").
			Where("model_id IN ? AND started_at >= ? AND started_at < ?", ids, from, now).
			Order("request_id").Limit(usageRowLimit + 1).Find(&facts).Error; err != nil {
			return err
		}
		var err error
		result.Items, err = modelMonthlyCounts(ids, facts, from, now)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	return result, nil
}
