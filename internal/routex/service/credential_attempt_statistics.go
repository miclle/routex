package service

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

const credentialAttemptLimit = 100
const credentialAttemptBatchLimit = 20

var credentialAttemptStatisticsUnavailable = &apperrors.Error{Code: http.StatusServiceUnavailable, Message: "credential attempt statistics unavailable"}

type CredentialFailureStreak struct {
	State      string `json:"state"`
	Count      *int   `json:"count"`
	LowerBound int    `json:"lower_bound"`
}

type CredentialRecentError struct {
	State       string     `json:"state"`
	Code        *string    `json:"code"`
	CompletedAt *time.Time `json:"completed_at"`
}

type CredentialAttemptStatistics struct {
	CredentialID      string                  `json:"credential_id"`
	ConnectionID      string                  `json:"connection_id"`
	InspectedAttempts int                     `json:"inspected_attempts"`
	HasMore           bool                    `json:"has_more"`
	FailureStreak     CredentialFailureStreak `json:"failure_streak"`
	RecentError       CredentialRecentError   `json:"recent_error"`
}

type CredentialAttemptStatisticsBatch struct {
	ProviderID   string                        `json:"provider_id"`
	ObservedAt   time.Time                     `json:"observed_at"`
	AttemptLimit int                           `json:"attempt_limit"`
	RecordedOnly bool                          `json:"recorded_only"`
	Items        []CredentialAttemptStatistics `json:"items"`
}

// These are immutable inference-attempt facts, not verification results or
// current route health. Snapshot identity is deliberately not a filter.
type credentialStatisticsAttempt struct {
	ID           string
	CredentialID string
	Status       string
	ErrorCode    string
	StartedAt    time.Time
	CompletedAt  time.Time
}

func validCredentialAttemptStatisticsIDs(ids []string) bool {
	if len(ids) == 0 || len(ids) > credentialAttemptBatchLimit {
		return false
	}
	seen := make(map[string]bool, len(ids))
	for _, value := range ids {
		if !safeTeamSessionID(value) || !strings.HasPrefix(value, "crd_") || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func credentialStatisticsKnown(row credentialStatisticsAttempt, credentialID string, observedAt time.Time) bool {
	return row.CredentialID == credentialID && safeCallID.MatchString(row.ID) &&
		connectionMetadataBirth(row.StartedAt) && connectionMetadataBirth(row.CompletedAt) &&
		!row.CompletedAt.Before(row.StartedAt) && !row.CompletedAt.After(observedAt) && validCallStatus(row.Status)
}

// rows must be in descending completion/byte-ID order, with at most one
// overflow sentinel. Unknown and canceled streak boundaries are never bridged.
func projectCredentialAttemptStatistics(credentialID, connectionID string, rows []credentialStatisticsAttempt, observedAt time.Time) CredentialAttemptStatistics {
	out := CredentialAttemptStatistics{CredentialID: credentialID, ConnectionID: connectionID,
		FailureStreak: CredentialFailureStreak{State: "no_records"}, RecentError: CredentialRecentError{State: "no_records"}}
	out.HasMore = len(rows) > credentialAttemptLimit
	if out.HasMore {
		rows = rows[:credentialAttemptLimit]
	}
	out.InspectedAttempts = len(rows)
	if len(rows) == 0 {
		return out
	}
	out.FailureStreak.State = "unknown"
	out.RecentError.State = "none"
	prefixOpen, uncertain := true, false
	for _, row := range rows {
		known := credentialStatisticsKnown(row, credentialID, observedAt)
		if !known {
			uncertain = true
			// An unclassifiable newer event may itself be the latest error. Do not
			// silently search through it and present an older error as most recent.
			if out.RecentError.State == "none" {
				out.RecentError.State = "unknown"
			}
		}
		if out.RecentError.State == "none" && known && row.Status == "error" {
			completedAt := row.CompletedAt.UTC()
			out.RecentError = CredentialRecentError{State: "recorded", CompletedAt: &completedAt}
			if row.ErrorCode != "" && safeCallError(row.ErrorCode) == row.ErrorCode {
				code := row.ErrorCode
				out.RecentError.Code = &code
			}
		}
		if !prefixOpen {
			continue
		}
		if !known || row.Status == "canceled" {
			prefixOpen = false
			continue
		}
		if row.Status == "error" {
			out.FailureStreak.LowerBound++
			continue
		}
		count := out.FailureStreak.LowerBound
		out.FailureStreak.State, out.FailureStreak.Count = "exact", &count
		prefixOpen = false
	}
	if prefixOpen {
		if out.HasMore {
			out.FailureStreak.State = "lower_bound"
		} else {
			count := out.FailureStreak.LowerBound
			out.FailureStreak.State, out.FailureStreak.Count = "exact", &count
		}
	}
	if out.RecentError.State != "recorded" && (out.HasMore || uncertain) {
		out.RecentError.State = "unknown"
	}
	return out
}

func credentialStatisticsAttemptsQuery(tx *gorm.DB, credentialID string) *gorm.DB {
	// Ordinary equality enables the leading index; ExactText additionally rejects
	// collation aliases. Byte order makes equal completion timestamps deterministic.
	return memberRolesDB(tx).Table("call_attempts").
		Select("id", "credential_id", "status", "error_code", "started_at", "completed_at").
		Where("credential_id = ?", credentialID).
		Where(database.ExactText(tx, clause.Column{Name: "credential_id"}, credentialID)).
		Clauses(clause.OrderBy{Expression: clause.Expr{SQL: "? DESC, ? DESC", Vars: []any{
			clause.Column{Name: "completed_at"}, database.ByteOrder(tx, clause.Column{Name: "id"}),
		}}}).Limit(credentialAttemptLimit + 1)
}

func (s *Service) GetCredentialAttemptStatistics(ctx context.Context, actorID, providerID string, ids []string) (*CredentialAttemptStatisticsBatch, error) {
	if err := providerMetadataIDs(actorID, providerID); err != nil {
		return nil, err
	}
	if !validCredentialAttemptStatisticsIDs(ids) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out := CredentialAttemptStatisticsBatch{ProviderID: providerID,
		AttemptLimit: credentialAttemptLimit, RecordedOnly: true, Items: make([]CredentialAttemptStatistics, 0, len(ids))}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		out.ObservedAt = time.Now().UTC()
		actor, err := registrationAdmittedUser(memberRolesDB(tx), actorID, false)
		if err != nil {
			return err
		}
		if actor.Role != entity.RoleAdmin && actor.Role != entity.RoleMember {
			return apperrors.ErrUnauthorized
		}
		if actor.ID != actorID || !connectionMetadataBirth(actor.CreatedAt) {
			return credentialAttemptStatisticsUnavailable
		}
		allowed, err := exactGovernancePermissionForAdmittedActor(memberRolesDB(tx), actor, "providers.read")
		if err != nil {
			return err
		}
		if !allowed {
			return apperrors.ErrForbidden
		}
		var provider entity.Provider
		if err := providerMetadataQuery(tx, providerID).Select("id", "created_at").Take(&provider).Error; err != nil {
			return err
		}
		if provider.ID != providerID {
			return apperrors.ErrNotFound
		}
		if !connectionMetadataBirth(provider.CreatedAt) {
			return credentialAttemptStatisticsUnavailable
		}
		for _, credentialID := range ids {
			var credential entity.ProviderCredential
			if err := memberRolesExact(memberRolesDB(tx).Model(&entity.ProviderCredential{}), "id", credentialID).
				Select("id", "connection_id", "created_at").Take(&credential).Error; err != nil {
				return err
			}
			if credential.ID != credentialID {
				return apperrors.ErrNotFound
			}
			if !connectionMetadataBirth(credential.CreatedAt) {
				return credentialAttemptStatisticsUnavailable
			}
			var connection entity.ProviderConnection
			if err := memberRolesExact(memberRolesDB(tx).Model(&entity.ProviderConnection{}), "id", credential.ConnectionID).
				Select("id", "provider_id", "created_at").Take(&connection).Error; err != nil {
				return err
			}
			if connection.ID != credential.ConnectionID || connection.ProviderID != providerID {
				return apperrors.ErrNotFound
			}
			if !safeTeamSessionID(connection.ID) || !strings.HasPrefix(connection.ID, "con_") || !connectionMetadataBirth(connection.CreatedAt) {
				return credentialAttemptStatisticsUnavailable
			}
			var rows []credentialStatisticsAttempt
			if err := credentialStatisticsAttemptsQuery(tx, credentialID).Find(&rows).Error; err != nil {
				return err
			}
			out.Items = append(out.Items, projectCredentialAttemptStatistics(credentialID, connection.ID, rows, out.ObservedAt))
		}
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, credentialAttemptStatisticsReadError(err)
	}
	if ctx.Err() != nil {
		return nil, credentialAttemptStatisticsUnavailable
	}
	return &out, nil
}

func credentialAttemptStatisticsReadError(err error) error {
	mapped := providerMetadataReadError(err)
	if mapped == providerMetadataUnavailable {
		return credentialAttemptStatisticsUnavailable
	}
	return mapped
}
