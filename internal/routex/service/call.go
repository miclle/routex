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
	CacheReadTokens, CacheWriteTokens *int64
	ImageInputs, PDFInputs            *int64
	UsageComplete                     bool
	NoWork                            bool
	PricingUnsupported                bool
	PricingDimensions                 []string
	PriceBasis                        *CallPriceBasis
	Pricing                           *CallPricing
	SnapshotID                        string
	RequestID                         string
	ProjectID                         string
	UserID                            string
	KeyID                             string
	ModelID                           string
	ModelName                         string
	ProviderID                        string
	ProviderName                      string
	ProviderModelID                   string
	ConnectionID                      string
	ConnectionName                    string
	UpstreamModelName                 string
	RouteStopReason                   string
	Protocol                          string
	Status                            string
	Stream                            bool
	StartedAt                         time.Time
	CompletedAt                       time.Time
	InputTokens                       *int64
	OutputTokens                      *int64
	ErrorCode                         string
	Attempts                          []CallAttempt
}

type CallAttempt struct {
	ID              string
	ProviderModelID string
	ConnectionID    string
	AttemptNumber   int
	Status          string
	FailureClass    string
	WorkEvidence    string
	OutputStarted   bool
	FinalUsageKnown bool
	EvidenceCode    string
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

var callRouteStopReasons = map[string]bool{
	"": true, "succeeded": true, "unsafe_to_replay": true, "permanent_failure": true,
	"attempt_budget_exhausted": true, "no_candidates": true, "canceled": true, "blocked": true,
}

var callAttemptFailures = map[string]bool{
	"success": true, "credential_rejected": true, "connection_failure": true,
	"rate_limited": true, "permanent_failure": true,
}

var callAttemptWorkEvidence = map[string]bool{
	"not_sent": true, "rejected_without_work": true, "unknown": true, "completed": true,
}

var callAttemptEvidenceCodes = map[string]bool{
	"": true, "pre_request_connection": true, "native_auth_rejection": true,
	"native_rate_rejection": true, "upstream_response": true, "transport_ambiguous": true,
}

func validCallStatus(status string) bool {
	return status == "success" || status == "error" || status == "canceled"
}

func validateCallFilter(filter CallFilter, admin bool) error {
	if !admin && filter.UserID != "" {
		return apperrors.ErrBadRequest
	}
	if filter.Status != "" && !validCallStatus(filter.Status) {
		return apperrors.ErrBadRequest
	}
	for _, value := range []string{filter.ModelID, filter.KeyID, filter.UserID} {
		if value != "" && !safeCallID.MatchString(value) {
			return apperrors.ErrBadRequest
		}
	}
	if filter.From != nil && filter.To != nil && filter.From.After(*filter.To) {
		return apperrors.ErrBadRequest
	}
	return nil
}

// Error codes are machine-owned classifications, never upstream error messages.
func safeCallError(code string) string {
	switch code {
	case "quota_exceeded", "quota_history_incomplete", "quota_usage_unknown", "quota_currency_mismatch", "quota_bound_unavailable", "quota_price_unavailable", "quota_request_unsupported", "", "process_interrupted", "event_buffer_unavailable", "invalid_request", "invalid_request_error", "invalid_api_key", "rate_limit_exceeded", "concurrency_limit_exceeded", "ip_not_allowed", "service_unavailable", "unauthorized", "forbidden", "model_not_found", "no_route", "upstream_error", "upstream_timeout", "upstream_unavailable", "invalid_upstream_response", "canceled", "internal_error", "invalid_attachment_reference", "unsupported_attachment_reference", "attachment_limit_exceeded", "project_attachment_unsupported", "attachment_type_unsupported", "attachment_not_found", "attachment_storage_unavailable", "attachment_storage_timeout":
		return code
	default:
		return "upstream_error"
	}
}

// RecordCall atomically persists one canonical fact and its attempts. Replaying
// the same RequestID never changes an accepted fact or doubles its usage.
func (s *Service) RecordCall(ctx context.Context, fact CallFact) error {
	fact.Attempts = append([]CallAttempt(nil), fact.Attempts...)
	normalizeCallAttemptEvidence(&fact)
	finalizeCallPricing(&fact)
	if err := validateCallFact(fact); err != nil {
		return err
	}
	// Normalize to common database precision before building pagination cursors.
	record := entity.CallRecord{CallPricingFields: callPricingFields(fact), SnapshotID: fact.SnapshotID, RequestID: fact.RequestID, UserID: fact.UserID, ProjectID: fact.ProjectID, KeyID: fact.KeyID, ModelID: fact.ModelID, ModelName: fact.ModelName, ProviderID: fact.ProviderID, ProviderName: fact.ProviderName, ProviderModelID: fact.ProviderModelID, ConnectionID: fact.ConnectionID, ConnectionName: fact.ConnectionName, UpstreamModelName: fact.UpstreamModelName, RouteStopReason: fact.RouteStopReason, Protocol: fact.Protocol, Status: fact.Status, Stream: fact.Stream, StartedAt: fact.StartedAt.UTC().Truncate(time.Microsecond), CompletedAt: fact.CompletedAt.UTC().Truncate(time.Microsecond), DurationMS: fact.CompletedAt.Sub(fact.StartedAt).Milliseconds(), InputTokens: fact.InputTokens, OutputTokens: fact.OutputTokens, ImageInputs: fact.ImageInputs, PDFInputs: fact.PDFInputs, ErrorCode: safeCallError(fact.ErrorCode)}
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
			row := entity.CallAttempt{ID: attempt.ID, RequestID: fact.RequestID, ProviderModelID: attempt.ProviderModelID, ConnectionID: attempt.ConnectionID, AttemptNumber: attempt.AttemptNumber, Status: attempt.Status, FailureClass: attempt.FailureClass, WorkEvidence: attempt.WorkEvidence, OutputStarted: attempt.OutputStarted, FinalUsageKnown: attempt.FinalUsageKnown, EvidenceCode: attempt.EvidenceCode, HTTPStatus: attempt.HTTPStatus, ErrorCode: safeCallError(attempt.ErrorCode), StartedAt: attempt.StartedAt.UTC().Truncate(time.Microsecond), CompletedAt: attempt.CompletedAt.UTC().Truncate(time.Microsecond)}
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
	admin := ownerID == "" && filter.ProjectID == ""
	if err := validateCallFilter(filter, admin); err != nil {
		return nil, err
	}
	if filter.Limit < 1 || filter.Limit > 100 || len(filter.Cursor) > 512 {
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
		if err := db.Where("request_id = ?", requestID).Order("attempt_number, started_at, id").Find(&result.Attempts).Error; err != nil {
			return nil, catalogError(err)
		}
	}
	return result, nil
}

func validateCallFact(fact CallFact) error {
	fact.Attempts = append([]CallAttempt(nil), fact.Attempts...)
	normalizeCallAttemptEvidence(&fact)
	if err := validateCallPricing(fact); err != nil {
		return err
	}
	if !safeCallID.MatchString(fact.RequestID) || len(fact.SnapshotID) > 30 || (fact.UserID == "") == (fact.ProjectID == "") || len(fact.ProjectID) > 30 || len(fact.UserID) > 30 || len(fact.KeyID) > 30 || len(fact.ModelID) > 30 || len(fact.ModelName) > 128 || len(fact.ProviderID) > 30 || len(fact.ProviderName) > 100 || len(fact.ProviderModelID) > 30 || len(fact.ConnectionID) > 30 || len(fact.ConnectionName) > 100 || len(fact.UpstreamModelName) > 255 || !callRouteStopReasons[fact.RouteStopReason] || !entity.SupportedNativeProtocol(fact.Protocol) || !validCallStatus(fact.Status) || fact.StartedAt.IsZero() || fact.CompletedAt.Before(fact.StartedAt) || len(fact.Attempts) > 32 {
		return apperrors.ErrBadRequest
	}
	if (fact.InputTokens != nil && *fact.InputTokens < 0) || (fact.OutputTokens != nil && *fact.OutputTokens < 0) || (fact.ImageInputs != nil && *fact.ImageInputs < 0) || (fact.PDFInputs != nil && *fact.PDFInputs < 0) {
		return apperrors.ErrBadRequest
	}
	if fact.NoWork && (!fact.UsageComplete || !zeroCounter(fact.InputTokens) || !zeroCounter(fact.OutputTokens) || !zeroCounter(fact.CacheReadTokens) || !zeroCounter(fact.CacheWriteTokens) || !callAttemptsProveNoWork(fact.Attempts)) {
		return apperrors.ErrBadRequest
	}
	seen := map[string]bool{}
	numbers := map[int]bool{}
	for _, attempt := range fact.Attempts {
		if !safeCallID.MatchString(attempt.ID) || seen[attempt.ID] || attempt.AttemptNumber < 1 || attempt.AttemptNumber > 32 || numbers[attempt.AttemptNumber] || len(attempt.ProviderModelID) > 30 || len(attempt.ConnectionID) > 30 || !validCallStatus(attempt.Status) || !callAttemptFailures[attempt.FailureClass] || !callAttemptWorkEvidence[attempt.WorkEvidence] || !callAttemptEvidenceCodes[attempt.EvidenceCode] || attempt.StartedAt.IsZero() || attempt.CompletedAt.Before(attempt.StartedAt) || attempt.HTTPStatus < 0 || attempt.HTTPStatus > 599 {
			return apperrors.ErrBadRequest
		}
		if (attempt.FailureClass == "success") != (attempt.Status == "success") || attempt.Status == "success" && attempt.WorkEvidence != "completed" || (attempt.OutputStarted || attempt.FinalUsageKnown) && (attempt.WorkEvidence == "not_sent" || attempt.WorkEvidence == "rejected_without_work") {
			return apperrors.ErrBadRequest
		}
		seen[attempt.ID] = true
		numbers[attempt.AttemptNumber] = true
	}
	return nil
}

func zeroCounter(value *int64) bool { return value != nil && *value == 0 }

func normalizeCallAttemptEvidence(fact *CallFact) {
	for index := range fact.Attempts {
		attempt := &fact.Attempts[index]
		if attempt.AttemptNumber == 0 {
			attempt.AttemptNumber = index + 1
		}
		if attempt.FailureClass == "" {
			attempt.FailureClass = "permanent_failure"
			if attempt.Status == "success" {
				attempt.FailureClass = "success"
			}
		}
		if attempt.WorkEvidence == "" {
			attempt.WorkEvidence = "unknown"
			if attempt.Status == "success" {
				attempt.WorkEvidence = "completed"
			}
		}
	}
}
