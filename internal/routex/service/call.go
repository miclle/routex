package service

import (
	"context"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

type CallFact struct {
	SnapshotID      string
	RequestID       string
	ProjectID       string
	UserID          string
	KeyID           string
	ModelID         string
	ModelName       string
	ProviderModelID string
	ConnectionID    string
	Protocol        string
	Status          string
	Stream          bool
	StartedAt       time.Time
	CompletedAt     time.Time
	InputTokens     *int64
	OutputTokens    *int64
	ErrorCode       string
	Attempts        []CallAttempt
}

type CallAttempt struct {
	ID              string
	ProviderModelID string
	ConnectionID    string
	Status          string
	StartedAt       time.Time
	CompletedAt     time.Time
	HTTPStatus      int
	ErrorCode       string
}

type CallFilter struct {
	ProjectID string
	Cursor    string
	Limit     int
	Status    string
	ModelID   string
	KeyID     string
	UserID    string
	From      *time.Time
	To        *time.Time
}

type CallPage struct {
	Records    []entity.CallRecord
	NextCursor string
}
type CallDetail struct {
	Record   entity.CallRecord
	Attempts []entity.CallAttempt
}

var errCallAlreadyRecorded = errors.New("call already recorded")

var safeCallID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func validCallStatus(status string) bool {
	return status == "success" || status == "error" || status == "canceled"
}

// Error codes are machine-owned classifications, never upstream error messages.
func safeCallError(code string) string {
	switch code {
	case "", "process_interrupted", "event_buffer_unavailable", "invalid_request", "invalid_request_error", "invalid_api_key", "rate_limit_exceeded", "service_unavailable", "unauthorized", "forbidden", "model_not_found", "no_route", "upstream_error", "upstream_timeout", "upstream_unavailable", "invalid_upstream_response", "canceled", "internal_error":
		return code
	default:
		return "upstream_error"
	}
}

// RecordCall atomically persists one canonical fact and its attempts. Replaying
// the same RequestID never changes an accepted fact or doubles its usage.
func (s *Service) RecordCall(ctx context.Context, fact CallFact) error {
	if err := validateCallFact(fact); err != nil {
		return err
	}
	// Normalize to common database precision before building pagination cursors.
	record := entity.CallRecord{SnapshotID: fact.SnapshotID, RequestID: fact.RequestID, UserID: fact.UserID, ProjectID: fact.ProjectID, KeyID: fact.KeyID, ModelID: fact.ModelID, ModelName: fact.ModelName, ProviderModelID: fact.ProviderModelID, ConnectionID: fact.ConnectionID, Protocol: fact.Protocol, Status: fact.Status, Stream: fact.Stream, StartedAt: fact.StartedAt.UTC().Truncate(time.Microsecond), CompletedAt: fact.CompletedAt.UTC().Truncate(time.Microsecond), DurationMS: fact.CompletedAt.Sub(fact.StartedAt).Milliseconds(), InputTokens: fact.InputTokens, OutputTokens: fact.OutputTokens, ErrorCode: safeCallError(fact.ErrorCode)}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&record).Error; err != nil {
			// A plain unique insert remains correct with MySQL clientFoundRows;
			// affected-row counts from an upsert cannot establish deduplication.
			if errors.Is(catalogError(err), catalogConflict) {
				return errCallAlreadyRecorded
			}
			return err
		}
		for _, attempt := range fact.Attempts {
			row := entity.CallAttempt{ID: attempt.ID, RequestID: fact.RequestID, ProviderModelID: attempt.ProviderModelID, ConnectionID: attempt.ConnectionID, Status: attempt.Status, HTTPStatus: attempt.HTTPStatus, ErrorCode: safeCallError(attempt.ErrorCode), StartedAt: attempt.StartedAt.UTC().Truncate(time.Microsecond), CompletedAt: attempt.CompletedAt.UTC().Truncate(time.Microsecond)}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, errCallAlreadyRecorded) {
		return nil
	}
	return catalogError(err)
}

func (s *Service) ListCalls(ctx context.Context, ownerID string, filter CallFilter) (*CallPage, error) {
	if filter.Limit == 0 {
		filter.Limit = 40
	}
	if filter.Limit < 1 || filter.Limit > 100 || (filter.Status != "" && !validCallStatus(filter.Status)) || len(filter.Cursor) > 512 || (filter.From != nil && filter.To != nil && filter.From.After(*filter.To)) {
		return nil, apperrors.ErrBadRequest
	}
	if ownerID != "" && filter.UserID != "" && filter.UserID != ownerID {
		return nil, apperrors.ErrBadRequest
	}
	db := s.authDB(ctx).Model(&entity.CallRecord{})
	if ownerID != "" {
		db = db.Where("user_id = ?", ownerID)
	} else if filter.UserID != "" {
		db = db.Where("user_id = ?", filter.UserID)
	}
	if filter.ProjectID != "" {
		db = db.Where("project_id = ?", filter.ProjectID)
	}
	if filter.Status != "" {
		db = db.Where("status = ?", filter.Status)
	}
	if filter.ModelID != "" {
		db = db.Where("model_id = ?", filter.ModelID)
	}
	if filter.KeyID != "" {
		db = db.Where("key_id = ?", filter.KeyID)
	}
	if filter.From != nil {
		db = db.Where("started_at >= ?", filter.From.UTC())
	}
	if filter.To != nil {
		db = db.Where("started_at <= ?", filter.To.UTC())
	}
	if filter.Cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(filter.Cursor)
		if err != nil {
			return nil, apperrors.ErrBadRequest
		}
		parts := strings.SplitN(string(decoded), "|", 2)
		if len(parts) != 2 || !safeCallID.MatchString(parts[1]) {
			return nil, apperrors.ErrBadRequest
		}
		timestamp, err := time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			return nil, apperrors.ErrBadRequest
		}
		db = db.Where("started_at < ? OR (started_at = ? AND request_id < ?)", timestamp, timestamp, parts[1])
	}
	result := &CallPage{Records: []entity.CallRecord{}}
	if err := db.Order("started_at DESC, request_id DESC").Limit(filter.Limit + 1).Find(&result.Records).Error; err != nil {
		return nil, catalogError(err)
	}
	if len(result.Records) > filter.Limit {
		result.Records = result.Records[:filter.Limit]
		last := result.Records[len(result.Records)-1]
		result.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(last.StartedAt.UTC().Format(time.RFC3339Nano) + "|" + last.RequestID))
	}
	return result, nil
}

func (s *Service) GetCall(ctx context.Context, ownerID, requestID string) (*CallDetail, error) {
	if !safeCallID.MatchString(requestID) {
		return nil, apperrors.ErrNotFound
	}
	db := s.authDB(ctx)
	query := db.Where("request_id = ?", requestID)
	if ownerID != "" {
		query = query.Where("user_id = ?", ownerID)
	}
	result := &CallDetail{Attempts: []entity.CallAttempt{}}
	if err := query.First(&result.Record).Error; err != nil {
		return nil, catalogError(err)
	}
	// Only administrators need upstream attempt diagnostics.
	if ownerID == "" {
		if err := db.Where("request_id = ?", requestID).Order("started_at, id").Find(&result.Attempts).Error; err != nil {
			return nil, catalogError(err)
		}
	}
	return result, nil
}

func validateCallFact(fact CallFact) error {
	if !safeCallID.MatchString(fact.RequestID) || len(fact.SnapshotID) > 30 || (fact.UserID == "") == (fact.ProjectID == "") || len(fact.ProjectID) > 30 || len(fact.UserID) > 30 || len(fact.KeyID) > 30 || len(fact.ModelID) > 30 || len(fact.ModelName) > 128 || len(fact.ProviderModelID) > 30 || len(fact.ConnectionID) > 30 || fact.Protocol != entity.ProtocolOpenAIChat || !validCallStatus(fact.Status) || fact.StartedAt.IsZero() || fact.CompletedAt.Before(fact.StartedAt) || len(fact.Attempts) > 32 {
		return apperrors.ErrBadRequest
	}
	if (fact.InputTokens != nil && *fact.InputTokens < 0) || (fact.OutputTokens != nil && *fact.OutputTokens < 0) {
		return apperrors.ErrBadRequest
	}
	seen := map[string]bool{}
	for _, attempt := range fact.Attempts {
		if !safeCallID.MatchString(attempt.ID) || seen[attempt.ID] || len(attempt.ProviderModelID) > 30 || len(attempt.ConnectionID) > 30 || !validCallStatus(attempt.Status) || attempt.StartedAt.IsZero() || attempt.CompletedAt.Before(attempt.StartedAt) || attempt.HTTPStatus < 0 || attempt.HTTPStatus > 599 {
			return apperrors.ErrBadRequest
		}
		seen[attempt.ID] = true
	}
	return nil
}
