package service

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

type ProjectOverview struct {
	ProjectID      string                       `json:"project_id"`
	ObservedAt     time.Time                    `json:"observed_at"`
	Counts         ProjectOverviewCounts        `json:"counts"`
	LastCallAt     *time.Time                   `json:"last_call_at"`
	CallsAvailable bool                         `json:"calls_available"`
	MonthlyQuota   *ProjectOverviewMonthlyQuota `json:"monthly_quota"`
	Activities     []ProjectOverviewActivity    `json:"activities"`
}

type ProjectOverviewCounts struct {
	Managers int64 `json:"managers"`
	Models   int64 `json:"models"`
	// ActiveKeys counts stored active, unexpired Keys, not gateway eligibility.
	// A disabled or archived Project may still retain those historical records.
	ActiveKeys      *int64 `json:"active_keys"`
	PendingRequests *int64 `json:"pending_requests"`
}

type ProjectOverviewMonthlyQuota struct {
	TokensMonth      *int64                       `json:"tokens_month"`
	MoneyMonth       *string                      `json:"money_month"`
	Currency         string                       `json:"currency"`
	PlatformCurrency string                       `json:"platform_currency"`
	PolicyETag       string                       `json:"policy_etag"`
	Usage            *ProjectOverviewMonthlyUsage `json:"usage"`
}

type ProjectOverviewMonthlyUsage struct {
	AsOf          time.Time         `json:"as_of"`
	TimeZone      string            `json:"time_zone"`
	MonthStart    time.Time         `json:"month_start"`
	MonthEnd      time.Time         `json:"month_end"`
	Covered       bool              `json:"covered"`
	TokensUsed    string            `json:"tokens_used"`
	TokensHeld    string            `json:"tokens_held"`
	TokensUnknown int64             `json:"tokens_unknown"`
	MoneyUsed     map[string]string `json:"money_used"`
	MoneyHeld     map[string]string `json:"money_held"`
	MoneyUnknown  int64             `json:"money_unknown"`
}

// Activities expose only committed, typed Project metadata facts. Historical
// actor names were not recorded; current directory names are not substitutes.
type ProjectOverviewActivity struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"created_at"`
	ActorName *string   `json:"actor_name"`
	Status    string    `json:"status"`
}

var projectOverviewActions = map[string]string{
	"resource.create":          "project_created",
	"resource.update":          "project_updated",
	"project.managers.replace": "managers_changed",
	"resource.models.replace":  "models_changed",
	"limits.update":            "limits_changed",
}

func projectOverviewMonthlyUsage(usage *QuotaUsageRecord) (*ProjectOverviewMonthlyUsage, error) {
	if usage == nil || !usage.Activated || usage.AsOf == nil || usage.Month == nil {
		return nil, nil
	}
	location, err := time.LoadLocation(usage.TimeZone)
	if err != nil {
		return nil, runtimeUnavailable
	}
	local := usage.AsOf.In(location)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
	month := usage.Month
	return &ProjectOverviewMonthlyUsage{
		AsOf: usage.AsOf.UTC(), TimeZone: usage.TimeZone,
		MonthStart: start.UTC(), MonthEnd: start.AddDate(0, 1, 0).UTC(),
		Covered: month.Covered, TokensUsed: strconv.FormatInt(month.TokensUsed, 10),
		TokensHeld: strconv.FormatInt(month.TokensHeld, 10), TokensUnknown: month.TokensUnknown,
		MoneyUsed: month.MoneyUsed, MoneyHeld: month.MoneyHeld, MoneyUnknown: month.MoneyUnknown,
	}, nil
}

func projectOverviewActivity(row entity.AuditEvent, projectID string) (ProjectOverviewActivity, bool) {
	kind, known := projectOverviewActions[row.Action]
	metadata := row.ResourceType == "projects" && row.Action != "limits.update"
	limit := row.ResourceType == "project" && row.Action == "limits.update"
	if !known || row.ResourceID != projectID || !metadata && !limit {
		return ProjectOverviewActivity{}, false
	}
	return ProjectOverviewActivity{ID: row.ID, Kind: kind, CreatedAt: row.CreatedAt.UTC(), Status: "committed"}, true
}

// ProjectOverview reads scoped authorization and persisted Project facts from
// one repeatable-read snapshot. Journal usage has its own authoritative AsOf;
// it is never inferred from asynchronous call statistics or browser dates.
func (s *Service) ProjectOverview(ctx context.Context, actorID, projectID string) (*ProjectOverview, error) {
	if projectID == "" || !safeCallID.MatchString(projectID) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := &ProjectOverview{ProjectID: projectID, ObservedAt: time.Now().UTC(), Activities: []ProjectOverviewActivity{}}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		if err := resourceAccess(tx, actorID, ProjectResource, projectID); err != nil {
			return err
		}
		var project entity.Project
		if err := tx.Where(database.ExactText(tx, clause.Column{Name: "id"}, projectID)).Take(&project).Error; err != nil {
			return err
		}
		if project.ID != projectID {
			return apperrors.ErrNotFound
		}
		manager, err := exactProjectRequestManager(tx, actorID, projectID)
		if err != nil {
			return err
		}
		permission := func(name string) (bool, error) { return exactGovernancePermission(tx, actor, name) }
		for _, scope := range []struct {
			table, left, right string
			count              *int64
		}{
			{"project_managers AS g", "user_id", "users", &result.Counts.Managers},
			{"project_model_grants AS g", "model_id", "models", &result.Counts.Models},
		} {
			query := tx.Table(scope.table).Joins("JOIN " + scope.right + " AS target ON target.id = g." + scope.left).
				Where(database.ExactText(tx, clause.Column{Table: "g", Name: "project_id"}, projectID)).
				Where(database.ExactTextColumns(tx, clause.Column{Table: "g", Name: scope.left}, clause.Column{Table: "target", Name: "id"}))
			if err := query.Count(scope.count).Error; err != nil {
				return err
			}
		}
		keys, err := permission("projects.write")
		if err != nil {
			return err
		}
		if manager || keys {
			var count int64
			query := tx.Model(&entity.ProjectKey{}).
				Where(database.ExactText(tx, clause.Column{Name: "project_id"}, projectID)).
				Where(database.ExactText(tx, clause.Column{Name: "status"}, entity.KeyActive)).
				Where("expires_at IS NULL OR expires_at > ?", result.ObservedAt)
			if err := query.Count(&count).Error; err != nil {
				return err
			}
			result.Counts.ActiveKeys = &count
		}
		calls, err := permission("calls.read_all")
		if err != nil {
			return err
		}
		if manager || calls {
			result.CallsAvailable = true
			var rows []entity.CallRecord
			query := tx.Model(&entity.CallRecord{}).Select("completed_at").
				Where(database.ExactText(tx, clause.Column{Name: "project_id"}, projectID)).
				Where(database.ExactText(tx, clause.Column{Name: "team_id"}, "")).
				Order("completed_at DESC, request_id DESC").Limit(1)
			if err := query.Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) == 1 {
				stamp := rows[0].CompletedAt.UTC()
				result.LastCallAt = &stamp
			}
		}
		all, err := permission("projects.read_all")
		if err != nil {
			return err
		}
		quota, err := permission("projects.limits.write")
		if err != nil {
			return err
		}
		if manager || all || quota {
			row, policy, err := readProjectQuotaPolicy(tx, projectID, false)
			if err != nil {
				return err
			}
			var pricing entity.PricingSetting
			if err := tx.Select("platform_currency").Take(&pricing, 1).Error; err != nil {
				return err
			}
			result.MonthlyQuota = &ProjectOverviewMonthlyQuota{
				TokensMonth: policy.TokensMonth, MoneyMonth: policy.MoneyMonth, Currency: policy.Currency,
				PlatformCurrency: pricing.PlatformCurrency, PolicyETag: row.ETag,
			}
			if s.recorder != nil {
				usage, err := s.resourceQuotaUsage(tx, resolvedLimitTarget{kind: "project", id: projectID})
				if err != nil && !errors.Is(err, runtimeUnavailable) {
					return err
				}
				if err == nil {
					result.MonthlyQuota.Usage, err = projectOverviewMonthlyUsage(usage)
					if err != nil && !errors.Is(err, runtimeUnavailable) {
						return err
					}
				}
			}
		}
		access, err := projectRequestReadAccess(tx, actorID, projectID)
		if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
		if err == nil && access.Model && access.Limits {
			var count int64
			query := projectRequestKindScope(tx.Model(&entity.ProjectModelRequest{}), access).
				Where(database.ExactText(tx, clause.Column{Name: "project_id"}, projectID)).
				Where(database.ExactText(tx, clause.Column{Name: "status"}, entity.ProjectRequestPending))
			if err := query.Count(&count).Error; err != nil {
				return err
			}
			result.Counts.PendingRequests = &count
		}
		var events []entity.AuditEvent
		query := tx.Model(&entity.AuditEvent{}).Select("id", "action", "resource_type", "resource_id", "created_at").
			Where(database.ExactText(tx, clause.Column{Name: "resource_id"}, projectID)).
			Where(clause.Or(
				clause.And(
					database.ExactText(tx, clause.Column{Name: "resource_type"}, "projects"),
					clause.Or(
						database.ExactText(tx, clause.Column{Name: "action"}, "resource.create"),
						database.ExactText(tx, clause.Column{Name: "action"}, "resource.update"),
						database.ExactText(tx, clause.Column{Name: "action"}, "project.managers.replace"),
						database.ExactText(tx, clause.Column{Name: "action"}, "resource.models.replace"),
					),
				),
				clause.And(
					database.ExactText(tx, clause.Column{Name: "resource_type"}, "project"),
					database.ExactText(tx, clause.Column{Name: "action"}, "limits.update"),
				),
			))
		if err := query.Order("created_at DESC, id DESC").Limit(5).Find(&events).Error; err != nil {
			return err
		}
		for _, event := range events {
			activity, valid := projectOverviewActivity(event, projectID)
			if !valid {
				return apperrors.ErrInternal
			}
			result.Activities = append(result.Activities, activity)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
