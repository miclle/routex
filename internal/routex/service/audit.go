package service

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/prices"
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
	"models":      {"team_model_request", "personal_model_request", "model", "provider_model", "model_name", "binding", "model_provider_binding", "user_model_grant", "team_model_grant", "project_model_grant"},
	"keys":        {"api_key", "project_api_key"},
	"limits":      {"key", "user", "user_default", "team", "team_member", "team_member_default", "project", "team_quota_request", "default_limit"},
	"credentials": {"credential", "provider_credential", "provider", "connection"},
	"pricing":     {"pricing"},
	"identity":    {"user", "role", "session", "mfa", "installation", "registration", "offboarding_case", "teams"},
	"site":        {"site", "announcement"},
	"system":      {"system_instance", "secret_rotation"},
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
	case "member.state.update":
		record, valid := memberStateAuditProjection(row)
		if !valid {
			return result
		}
		changes = record
	case "member.roles.update":
		record, valid := memberRolesAuditProjection(row)
		if !valid {
			return result
		}
		changes = record
	case "member.metadata.update":
		record, valid := memberMetadataAuditProjection(row)
		if !valid {
			return result
		}
		changes = record
	case "member.models.update":
		record, valid := memberModelsAuditProjection(row)
		if !valid {
			return result
		}
		changes = record
	case "member.key.disable":
		record, valid := memberKeyDisableAuditProjection(row)
		if !valid {
			return result
		}
		changes = record
	case "secret_rotation.start", "secret_rotation.resume", "secret_rotation.retire", "secret_rotation.rollback":
		record, valid := rootRotationAuditProjection(row)
		if !valid {
			return result
		}
		changes = record
	case "model.batch_create":
		var valid bool
		changes, valid = modelCreationBatchAuditProjection(row)
		if !valid {
			return result
		}
	case "team.model_request.create", "team.model_request.approve", "team.model_request.reject", "team.model_request.withdraw", "team.model_request.cancel":
		var valid bool
		changes, valid = teamModelRequestAuditProjection(row)
		if !valid {
			return result
		}
	case "personal.model_request.create", "personal.model_request.approve", "personal.model_request.reject", "personal.model_request.withdraw", "personal.model_request.cancel":
		var valid bool
		changes, valid = personalModelAuditProjection(row)
		if !valid {
			return result
		}

	case "project.creation.commit":
		var valid bool
		changes, valid = projectCreationAuditProjection(row)
		if !valid {
			return result
		}
	case "limits.defaults.update", "limits.default.apply", "limits.default.reset":
		var valid bool
		changes, valid = defaultLimitAuditProjection(row)
		if !valid {
			return result
		}
	case "team.roles.replace":
		var valid bool
		changes, valid = teamRoleAuditProjection(row)
		if !valid {
			return result
		}
	case "team.quota_request.create", "team.quota_request.approve", "team.quota_request.reject", "team.quota_request.withdraw":
		var valid bool
		changes, valid = teamQuotaAuditProjection(row)
		if !valid {
			return result
		}
	case "prices.repository.config", "prices.repository.apply":
		var valid bool
		changes, valid = repositoryPriceAuditProjection(row)
		if !valid {
			return result
		}
		source := prices.SourceID
		result.Source = &source
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
			TeamID string        `json:"team_id,omitempty"`
			Before limits.Policy `json:"before"`
			After  limits.Policy `json:"after"`
			Reason string        `json:"reason"`
			ETag   string        `json:"etag"`
		}
		if json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil {
			return result
		}
		if row.ResourceType == "team" || row.ResourceType == "team_member" {
			if !safeTeamSessionID(detail.TeamID) || !strings.HasPrefix(detail.TeamID, "tea_") || row.ResourceType == "team" && row.ResourceID != detail.TeamID || row.ResourceType == "team_member" && (!safeTeamSessionID(row.ResourceID) || !strings.HasPrefix(row.ResourceID, "usr_")) || validateTeamLimitPolicy(row.ResourceType, detail.Before) != nil || validateTeamLimitPolicy(row.ResourceType, detail.After) != nil {
				return result
			}
		} else {
			detail.TeamID = ""
		}
		changes = detail
	case "model.alias.retire":
		var valid bool
		changes, valid = modelAliasRetirementAuditProjection(row)
		if !valid {
			return result
		}
	case "credential.metadata.update":
		type values struct {
			Name     string `json:"name"`
			Priority *int   `json:"priority"`
		}
		var detail struct {
			Before values `json:"before"`
			After  values `json:"after"`
			Reason string `json:"reason"`
		}
		if json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil ||
			!validCatalogLabel(detail.Before.Name) || !validCatalogLabel(detail.After.Name) ||
			detail.Before.Priority == nil || detail.After.Priority == nil ||
			*detail.Before.Priority < 0 || *detail.Before.Priority > 10000 ||
			*detail.After.Priority < 0 || *detail.After.Priority > 10000 ||
			detail.Reason == "" || strings.TrimSpace(detail.Reason) != detail.Reason ||
			!utf8.ValidString(detail.Reason) || len(detail.Reason) > 1024 ||
			strings.ContainsFunc(detail.Reason, unicode.IsControl) {
			return result
		}
		changes = detail
	case "credential.delete":
		var detail struct {
			Before struct {
				ID           string `json:"id"`
				ConnectionID string `json:"connection_id"`
				Name         string `json:"name"`
				Priority     *int   `json:"priority"`
			} `json:"before"`
			After struct {
				Absent *bool `json:"absent"`
			} `json:"after"`
			Reason string `json:"reason"`
		}
		if json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil ||
			!credentialDeleteID.MatchString(detail.Before.ID) || detail.Before.ID != row.ResourceID ||
			detail.Before.ConnectionID == "" || !validCatalogLabel(detail.Before.Name) ||
			detail.Before.Priority == nil || *detail.Before.Priority < 0 || *detail.Before.Priority > 10000 ||
			detail.After.Absent == nil || !*detail.After.Absent ||
			strings.TrimSpace(detail.Reason) != detail.Reason || !validCredentialMetadataReason(detail.Reason) {
			return result
		}
		changes = detail
	case "credential.replacement.create":
		var detail struct {
			SourceID     string `json:"source_id"`
			ConnectionID string `json:"connection_id"`
			Name         string `json:"name"`
			Priority     *int   `json:"priority"`
			Reason       string `json:"reason"`
		}
		if json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil ||
			row.ResourceType != "credential" || !credentialDeleteID.MatchString(row.ResourceID) ||
			!credentialDeleteID.MatchString(detail.SourceID) || detail.SourceID == row.ResourceID ||
			!validCatalogLabel(detail.ConnectionID) || !validCatalogLabel(detail.Name) ||
			detail.Priority == nil || *detail.Priority < 0 || *detail.Priority > 10000 ||
			strings.TrimSpace(detail.Reason) != detail.Reason || !validCredentialMetadataReason(detail.Reason) {
			return result
		}
		type absence struct {
			Absent bool `json:"absent"`
		}
		type replacement struct {
			SourceID     string `json:"source_id"`
			ConnectionID string `json:"connection_id"`
			Name         string `json:"name"`
			Priority     int    `json:"priority"`
		}
		changes = struct {
			Before absence     `json:"before"`
			After  replacement `json:"after"`
			Reason string      `json:"reason"`
		}{absence{true}, replacement{detail.SourceID, detail.ConnectionID, detail.Name, *detail.Priority}, detail.Reason}
	case "credential.retire":
		var detail struct {
			SourceID          string `json:"source_id"`
			ReplacementID     string `json:"replacement_id"`
			ConnectionID      string `json:"connection_id"`
			RequestID         string `json:"request_id"`
			SnapshotID        string `json:"snapshot_id"`
			EvidenceAttemptID string `json:"evidence_attempt_id"`
			Reason            string `json:"reason"`
			Before            struct {
				Enabled *bool `json:"enabled"`
			} `json:"before"`
			After struct {
				Enabled *bool `json:"enabled"`
			} `json:"after"`
		}
		if json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil ||
			row.ResourceType != "credential" || row.ResourceID != detail.SourceID ||
			!validCredentialRetirementPairIDs(detail.SourceID, detail.ReplacementID) ||
			!validCatalogLabel(detail.ConnectionID) ||
			!credentialReplacementRequestID.MatchString(detail.RequestID) ||
			!credentialRetirementSnapshotID.MatchString(detail.SnapshotID) ||
			!safeCallID.MatchString(detail.EvidenceAttemptID) ||
			strings.TrimSpace(detail.Reason) != detail.Reason || !validCredentialMetadataReason(detail.Reason) ||
			detail.Before.Enabled == nil || !*detail.Before.Enabled || detail.After.Enabled == nil || *detail.After.Enabled {
			return result
		}
		changes = detail
	case "system.instance.cleanup":
		var detail struct {
			Revision uint64 `json:"revision"`
		}
		if json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil || detail.Revision == 0 {
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
				query = query.Where("action NOT IN ?", []string{"limits.update", "limits.default.apply", "limits.default.reset"})
			}
			if filter.Category == "limits" {
				query = query.Where("action IN ?", []string{"limits.update", "limits.defaults.update", "limits.default.apply", "limits.default.reset", "team.quota_request.create", "team.quota_request.approve", "team.quota_request.reject", "team.quota_request.withdraw"})
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
