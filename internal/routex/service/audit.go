package service

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
)

var auditCursor = regexp.MustCompile(`^aud_[0-7][0-9a-hjkmnp-tv-z]{25}$`)

type AuditFilter struct{ Query, Category, Range, Cursor string }
type AuditRecord struct {
	ID           string          `json:"id"`
	ActorID      string          `json:"actor_id"`
	ActorName    string          `json:"actor_name"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	CreatedAt    time.Time       `json:"created_at"`
	Result       string          `json:"result"`
	Source       *string         `json:"source"`
	IP           *string         `json:"ip"`
	RequestID    *string         `json:"request_id"`
	Changes      json.RawMessage `json:"changes"`
}
type AuditPage struct {
	Items      []AuditRecord `json:"items"`
	NextCursor *string       `json:"next_cursor"`
}

var auditCategories = map[string][]string{
	"models":      {"model", "provider_model", "model_name", "binding", "model_provider_binding", "user_model_grant", "team_model_grant", "project_model_grant"},
	"keys":        {"api_key", "project_api_key"},
	"limits":      {"key", "user", "user_default", "team", "team_member", "team_member_default", "project"},
	"credentials": {"credential", "provider_credential", "provider", "connection"},
	"pricing":     {"pricing"},
	"identity":    {"user", "role", "session", "mfa", "installation", "registration", "offboarding_case"},
	"site":        {"site", "announcement"},
}

func validateAuditFilter(f AuditFilter) (AuditFilter, time.Duration, error) {
	f.Query = strings.TrimSpace(f.Query)
	if !utf8.ValidString(f.Query) || utf8.RuneCountInString(f.Query) > 100 || strings.ContainsRune(f.Query, 0) || (f.Cursor != "" && !auditCursor.MatchString(f.Cursor)) {
		return f, 0, apperrors.ErrBadRequest
	}
	if f.Category != "" && f.Category != "all" {
		if _, ok := auditCategories[f.Category]; !ok {
			return f, 0, apperrors.ErrBadRequest
		}
	}
	var duration time.Duration
	switch f.Range {
	case "", "7d":
		duration = 7 * 24 * time.Hour
	case "24h":
		duration = 24 * time.Hour
	case "30d":
		duration = 30 * 24 * time.Hour
	default:
		return f, 0, apperrors.ErrBadRequest
	}
	return f, duration, nil
}
func auditRecord(row entity.AuditEvent) AuditRecord {
	result := AuditRecord{ID: row.ID, ActorID: row.ActorID, ActorName: row.ActorID, Action: row.Action, ResourceType: row.ResourceType, ResourceID: row.ResourceID, CreatedAt: row.CreatedAt, Result: "committed"}
	if row.DetailsJSON == nil || len(*row.DetailsJSON) > 64*1024 {
		return result
	}
	// Decode only the known audit schemas. Unknown fields and future payloads must
	// never become an accidental credential/request-body read API.
	var changes any
	switch row.Action {
	case "prices.update":
		type values struct {
			ETag  string        `json:"etag"`
			Items []PriceRecord `json:"items"`
		}
		var detail struct {
			Source string `json:"source"`
			Before values `json:"before"`
			After  values `json:"after"`
		}
		if json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil {
			return result
		}
		changes = struct {
			Before values `json:"before"`
			After  values `json:"after"`
		}{detail.Before, detail.After}
		switch detail.Source {
		case "api", "csv", "xlsx", "xls":
			result.Source = &detail.Source
		}
	case "prices.currency.update":
		type values struct {
			ETag     string     `json:"etag"`
			Currency pricing.FX `json:"currency"`
		}
		var detail struct {
			Before values `json:"before"`
			After  values `json:"after"`
		}
		if json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil {
			return result
		}
		changes = detail
	case "limits.update":
		var detail struct {
			Before limits.Policy `json:"before"`
			After  limits.Policy `json:"after"`
			Reason string        `json:"reason"`
			ETag   string        `json:"etag"`
		}
		if json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil {
			return result
		}
		changes = detail
	default:
		return result
	}
	encoded, err := json.Marshal(changes)
	if err == nil {
		result.Changes = encoded
	}
	return result
}
func (s *Service) ListAudit(ctx context.Context, actor string, filter AuditFilter) (*AuditPage, error) {
	filter, duration, err := validateAuditFilter(filter)
	if err != nil {
		return nil, err
	}
	result := &AuditPage{Items: []AuditRecord{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeGovernance(tx, actor, "audit.read"); err != nil {
			return err
		}
		query := tx.Model(&entity.AuditEvent{}).Where("created_at >= ? AND created_at <= ?", time.Now().UTC().Add(-duration), time.Now().UTC()).Order("id DESC").Limit(51)
		if filter.Cursor != "" {
			query = query.Where("id < ?", filter.Cursor)
		}
		if filter.Category != "" && filter.Category != "all" {
			query = query.Where("resource_type IN ?", auditCategories[filter.Category])
			if filter.Category == "identity" {
				query = query.Where("action <> ?", "limits.update")
			}
			if filter.Category == "limits" {
				query = query.Where("action = ?", "limits.update")
			}
		}
		if filter.Query != "" {
			// Literal substring search: the escape character is portable across drivers.
			pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(filter.Query)) + "%"
			users := tx.Model(&entity.User{}).Select("id").Where("LOWER(name) LIKE ? ESCAPE '!'", pattern)
			query = query.Where("LOWER(action) LIKE ? ESCAPE '!' OR LOWER(resource_id) LIKE ? ESCAPE '!' OR LOWER(actor_id) LIKE ? ESCAPE '!' OR actor_id IN (?)", pattern, pattern, pattern, users)
		}
		var rows []entity.AuditEvent
		if err := query.Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > 50 {
			cursor := rows[49].ID
			result.NextCursor = &cursor
			rows = rows[:50]
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ActorID)
		}
		var users []entity.User
		if len(ids) > 0 {
			if err := tx.Select("id", "name").Where("id IN ?", ids).Find(&users).Error; err != nil {
				return err
			}
		}
		names := map[string]string{}
		for _, user := range users {
			names[user.ID] = user.Name
		}
		for _, row := range rows {
			record := auditRecord(row)
			if name, ok := names[row.ActorID]; ok {
				record.ActorName = name
			}
			result.Items = append(result.Items, record)
		}
		return nil
	})
	return result, catalogError(err)
}
