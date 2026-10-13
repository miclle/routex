package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const InsightsReportsDefaultLimit = 20
const InsightsReportsMaxLimit = 100

var insightsReportRequestID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var insightsReportID = regexp.MustCompile(`^isr_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
var insightsReportConflict = &apperrors.Error{Code: 409, Message: "saved report intent conflicts with an existing request"}
var insightsReportDeleted = &apperrors.Error{Code: 410, Message: "saved report was explicitly deleted"}

type CreateInsightsReportInput struct {
	RequestID string `json:"request_id"`
	Kind      string `json:"kind"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	Timezone  string `json:"timezone,omitempty"`
	Dimension string `json:"dimension,omitempty"`
	ScopeKind string `json:"scope_kind,omitempty"`
	ScopeID   string `json:"scope_id,omitempty"`
}
type InsightsReportDefinition struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	From      string    `json:"from,omitempty"`
	To        string    `json:"to,omitempty"`
	Timezone  string    `json:"timezone,omitempty"`
	Dimension string    `json:"dimension,omitempty"`
	ScopeKind string    `json:"scope_kind,omitempty"`
	ScopeID   string    `json:"scope_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
type InsightsReportList struct {
	Items      []InsightsReportDefinition `json:"items"`
	NextCursor string                     `json:"next_cursor"`
}

func normalizeInsightsReport(input CreateInsightsReportInput) (CreateInsightsReportInput, error) {
	if !insightsReportRequestID.MatchString(input.RequestID) {
		return input, apperrors.ErrBadRequest
	}
	switch input.Kind {
	case "tokens_by_model", "provider_quality":
		if input.Dimension != "" || input.ScopeKind != "" || input.ScopeID != "" {
			return input, apperrors.ErrBadRequest
		}
		plan, err := insightsProviderQualityRange(InsightsProviderQualityInput{From: input.From, To: input.To, Timezone: input.Timezone}, time.Now().UTC())
		if err != nil {
			return input, err
		}
		input.From, input.To, input.Timezone = plan.current.from.UTC().Format(time.RFC3339Nano), plan.current.to.UTC().Format(time.RFC3339Nano), plan.location.String()
	case "team_quota", "key_quota":
		if input.From != "" || input.To != "" || input.Timezone != "" {
			return input, apperrors.ErrBadRequest
		}
		_, _, err := normalizeInsightsQuota("owner", InsightsQuotaInput{Dimension: input.Dimension, ScopeKind: input.ScopeKind, ScopeID: input.ScopeID}, input.Kind == "key_quota")
		if err != nil {
			return input, err
		}
	default:
		return input, apperrors.ErrBadRequest
	}
	return input, nil
}
func insightsReportOwner(actor entity.User) string {
	return secret.SHA256Hex(actor.ID + "\x00" + actor.CreatedAt.UTC().Format(time.RFC3339Nano))
}
func insightsReportIntent(input CreateInsightsReportInput) string {
	raw, _ := json.Marshal(input)
	return secret.SHA256Hex(string(raw))
}
func insightsReportDefinition(row entity.InsightsReport) InsightsReportDefinition {
	return InsightsReportDefinition{ID: row.ID, Kind: row.Kind, From: row.From, To: row.To, Timezone: row.Timezone, Dimension: row.Dimension, ScopeKind: row.ScopeKind, ScopeID: row.ScopeID, CreatedAt: row.CreatedAt.UTC()}
}

// Governance then exact actor locking serializes writes with permission changes,
// account disable and other owner writes. No aggregate or AI query is performed.
func lockInsightsReportOwner(tx *gorm.DB, actorID string) (entity.User, error) {
	if err := lockGovernance(tx); err != nil {
		return entity.User{}, err
	}
	return exactEnabledActor(tx.Clauses(clause.Locking{Strength: "UPDATE"}), actorID)
}
func (s *Service) CreateInsightsReport(ctx context.Context, actorID string, input CreateInsightsReportInput) (*InsightsReportDefinition, bool, error) {
	input, err := normalizeInsightsReport(input)
	if err != nil {
		return nil, false, err
	}
	var result InsightsReportDefinition
	replay := false
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := lockInsightsReportOwner(tx, actorID)
		if err != nil {
			return err
		}
		if err := authorizeInsightsReportCreation(tx.Session(&gorm.Session{}), actorID, input); err != nil {
			return err
		}
		owner, intent := insightsReportOwner(actor), insightsReportIntent(input)
		var row entity.InsightsReport
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("owner_digest = ? AND request_id = ?", owner, input.RequestID).First(&row).Error
		if err == nil {
			if row.OwnerDigest != owner || row.RequestID != input.RequestID || row.IntentDigest != intent {
				return insightsReportConflict
			}
			if row.DeletedAt != nil {
				return insightsReportDeleted
			}
			result, replay = insightsReportDefinition(row), true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		reportID, err := id.NewPrefixed("isr")
		if err != nil {
			return err
		}
		row = entity.InsightsReport{ID: reportID, OwnerDigest: owner, RequestID: input.RequestID, IntentDigest: intent, Kind: input.Kind, From: input.From, To: input.To, Timezone: input.Timezone, Dimension: input.Dimension, ScopeKind: input.ScopeKind, ScopeID: input.ScopeID, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		result = insightsReportDefinition(row)
		return nil
	})
	if err != nil {
		return nil, false, catalogError(err)
	}
	return &result, replay, nil
}
func insightsReportCursor(owner, lastID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(owner + ":" + lastID))
}
func parseInsightsReportCursor(cursor, owner string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	if len(cursor) > 160 {
		return "", apperrors.ErrBadRequest
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(cursor)
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != cursor {
		return "", apperrors.ErrBadRequest
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 2 || parts[0] != owner || !insightsReportID.MatchString(parts[1]) {
		return "", apperrors.ErrBadRequest
	}
	return parts[1], nil
}
func (s *Service) ListInsightsReports(ctx context.Context, actorID string, limit int, cursor string) (*InsightsReportList, error) {
	if limit == 0 {
		limit = InsightsReportsDefaultLimit
	}
	if limit < 1 || limit > InsightsReportsMaxLimit {
		return nil, apperrors.ErrBadRequest
	}
	result := &InsightsReportList{Items: []InsightsReportDefinition{}}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := lockInsightsReportOwner(tx, actorID)
		if err != nil {
			return err
		}
		owner := insightsReportOwner(actor)
		last, err := parseInsightsReportCursor(cursor, owner)
		if err != nil {
			return err
		}
		query := tx.Where("owner_digest = ? AND deleted_at IS NULL", owner)
		if last != "" {
			query = query.Where("id < ?", last)
		}
		var rows []entity.InsightsReport
		if err := query.Order("id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		more := len(rows) > limit
		if more {
			rows = rows[:limit]
		}
		for _, row := range rows {
			if row.OwnerDigest != owner {
				return apperrors.ErrInternal
			}
			result.Items = append(result.Items, insightsReportDefinition(row))
		}
		if more {
			result.NextCursor = insightsReportCursor(owner, rows[len(rows)-1].ID)
		}
		return nil
	})
	if err != nil {
		return nil, catalogError(err)
	}
	return result, nil
}
func (s *Service) DeleteInsightsReport(ctx context.Context, actorID, reportID string) error {
	if !insightsReportID.MatchString(reportID) {
		return apperrors.ErrNotFound
	}
	return catalogError(s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := lockInsightsReportOwner(tx, actorID)
		if err != nil {
			return err
		}
		owner := insightsReportOwner(actor)
		var row entity.InsightsReport
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND owner_digest = ?", reportID, owner).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && (row.ID != reportID || row.OwnerDigest != owner || row.DeletedAt != nil) {
			return apperrors.ErrNotFound
		}
		if err != nil {
			return err
		}
		// A minimal receipt survives; none of the saved definition parameters does.
		return tx.Model(&row).Updates(map[string]any{"kind": "", "range_from": "", "range_to": "", "timezone": "", "dimension": "", "scope_kind": "", "scope_id": "", "deleted_at": time.Now().UTC()}).Error
	}))
}

// Saving a query definition never enumerates Keys or reads quota accounting.
func authorizeInsightsReportCreation(tx *gorm.DB, actorID string, input CreateInsightsReportInput) error {
	if input.Kind == "provider_quality" {
		return authorizeInsightsProviderQuality(tx, actorID)
	}
	if err := authorizeUsage(tx, actorID, "admin", ""); err != nil {
		return err
	}
	if input.Kind != "key_quota" {
		return nil
	}
	if input.ScopeKind == "member" {
		_, _, err := memberKeyPeople(tx, actorID, input.ScopeID, "members.read")
		return err
	}
	if err := projectKeyAccess(tx, actorID, input.ScopeID); err != nil {
		return err
	}
	var project entity.Project
	if err := tx.Select("id").Where(database.ExactText(tx, clause.Column{Name: "id"}, input.ScopeID)).Take(&project).Error; err != nil {
		return err
	}
	if project.ID != input.ScopeID {
		return apperrors.ErrNotFound
	}
	return nil
}
