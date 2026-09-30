package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

const (
	providerQualityMaxAttempts = 100_000
	providerQualityGrace       = 5 * time.Minute
	providerQualityPoll        = time.Minute

	defaultQualityWindowMinutes = 60
	defaultQualityMinimum       = 20
	defaultQualitySuccessBPS    = 9500
)

var providerQualityTooLarge = &apperrors.Error{Code: http.StatusUnprocessableEntity, Message: "provider quality query exceeds the supported complete range"}

type ProviderQuality struct {
	ProviderID   string     `json:"provider_id"`
	ProviderName string     `json:"name"`
	WindowStart  time.Time  `json:"window_start"`
	WindowEnd    time.Time  `json:"window_end"`
	ObservedAt   time.Time  `json:"evaluated_at"`
	DataThrough  *time.Time `json:"data_through"`
	MayLag       bool       `json:"may_lag"`
	// Requests counts immutable upstream attempts; one logical call may fail over
	// through more than one provider and therefore contribute multiple attempts.
	Requests                   int64 `json:"requests"`
	EligibleAttempts           int64 `json:"eligible_attempts"`
	ExcludedAttempts           int64 `json:"excluded_attempts"`
	CredentialRejectedAttempts int64 `json:"credential_rejected_attempts"`
	// UnknownAttributionAttempts is platform-wide coverage for this time range;
	// it is never assigned to this provider or included in its rates.
	UnknownAttributionAttempts int64      `json:"unknown_attribution_attempts"`
	Successes                  int64      `json:"successes"`
	SuccessRate                *float64   `json:"success_rate"`
	SuccessRateBPS             *int       `json:"success_rate_bps"`
	HTTP429                    int64      `json:"rate_limited_attempts"`
	HTTP5XX                    int64      `json:"server_error_attempts"`
	KnownDurationAttempts      int64      `json:"known_duration_attempts"`
	UnknownDurationAttempts    int64      `json:"unknown_duration_attempts"`
	P95DurationMS              *int64     `json:"p95_duration_ms"`
	LatestCompletedAt          *time.Time `json:"latest_completed_at"`
	Status                     string     `json:"status"`
}

type ProviderQualityPolicy struct {
	ProviderID        string     `json:"provider_id"`
	Enabled           bool       `json:"enabled"`
	WindowMinutes     int        `json:"window_minutes"`
	MinimumAttempts   int        `json:"minimum_attempts"`
	MinSuccessRateBPS int        `json:"min_success_rate_bps"`
	MaxP95DurationMS  *int64     `json:"max_p95_duration_ms"`
	ETag              string     `json:"etag"`
	UpdatedBy         string     `json:"updated_by"`
	UpdateReason      string     `json:"update_reason"`
	UpdatedAt         *time.Time `json:"updated_at"`
}

type ProviderQualityPolicyInput struct {
	Enabled           bool   `json:"enabled"`
	WindowMinutes     int    `json:"window_minutes"`
	MinimumAttempts   int    `json:"minimum_attempts"`
	MinSuccessRateBPS int    `json:"min_success_rate_bps"`
	MaxP95DurationMS  *int64 `json:"max_p95_duration_ms"`
	ETag              string `json:"etag"`
	Reason            string `json:"reason"`
}

type providerQualityAggregate struct {
	Requests                   int64
	EligibleAttempts           int64
	ExcludedAttempts           int64
	CredentialRejectedAttempts int64
	UnknownAttributionAttempts int64
	Successes                  int64
	HTTP429                    int64 `gorm:"column:http429"`
	HTTP5XX                    int64 `gorm:"column:http5xx"`
	KnownDurationAttempts      int64
	LatestCompletedAt          *time.Time
}

func defaultProviderQualityPolicy(providerID string) ProviderQualityPolicy {
	return ProviderQualityPolicy{
		ProviderID: providerID, WindowMinutes: defaultQualityWindowMinutes,
		MinimumAttempts: defaultQualityMinimum, MinSuccessRateBPS: defaultQualitySuccessBPS,
		ETag: "0",
	}
}

func providerQualityPolicyView(row entity.ProviderQualityPolicy) ProviderQualityPolicy {
	updatedAt := row.UpdatedAt
	return ProviderQualityPolicy{
		ProviderID: row.ProviderID, Enabled: row.Enabled, WindowMinutes: row.WindowMinutes,
		MinimumAttempts: row.MinimumAttempts, MinSuccessRateBPS: row.MinSuccessRateBPS,
		MaxP95DurationMS: row.MaxP95DurationMS, ETag: row.ETag, UpdatedBy: row.UpdatedBy,
		UpdateReason: row.UpdateReason, UpdatedAt: &updatedAt,
	}
}

func validQualityPolicyInput(input ProviderQualityPolicyInput) bool {
	reason := strings.TrimSpace(input.Reason)
	return input.ETag != "" && len(input.ETag) <= 30 && input.WindowMinutes >= 5 && input.WindowMinutes <= 1440 &&
		input.MinimumAttempts >= 1 && input.MinimumAttempts <= providerQualityMaxAttempts &&
		input.MinSuccessRateBPS >= 0 && input.MinSuccessRateBPS <= 10000 &&
		(input.MaxP95DurationMS == nil || *input.MaxP95DurationMS >= 1 && *input.MaxP95DurationMS <= 3_600_000) &&
		reason != "" && reason == input.Reason && utf8.ValidString(reason) && utf8.RuneCountInString(reason) <= 500 &&
		!strings.ContainsFunc(reason, unicode.IsControl)
}

func loadQualityProvider(db *gorm.DB, providerID string) (entity.Provider, error) {
	if !safeCallID.MatchString(providerID) {
		return entity.Provider{}, apperrors.ErrNotFound
	}
	var provider entity.Provider
	if err := db.First(&provider, "id = ?", providerID).Error; err != nil {
		return provider, catalogError(err)
	}
	return provider, nil
}

// ProviderQualitySummary returns a complete bounded view for the configured
// policy window. Providers without a saved policy use the same safe default
// window returned by ProviderQualityPolicy. Legacy attempts stay outside every
// provider scope and are never joined to mutable catalog or terminal-call
// attribution.
func (s *Service) ProviderQualitySummary(ctx context.Context, actor, providerID string) (*ProviderQuality, error) {
	now := time.Now().UTC()
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actor, "providers.read"); err != nil {
		return nil, catalogError(err)
	}
	provider, err := loadQualityProvider(db, providerID)
	if err != nil {
		return nil, err
	}
	var policy entity.ProviderQualityPolicy
	if err := db.First(&policy, "provider_id = ?", providerID).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, catalogError(err)
	}
	windowMinutes, ok := qualitySummaryWindowMinutes(policy)
	if !ok {
		return nil, apperrors.ErrInternal
	}
	result, err := providerQualityRange(db, provider, now.Add(-time.Duration(windowMinutes)*time.Minute), now, now)
	if err != nil {
		return nil, err
	}
	result.Status, _ = qualityState(result, policy)
	return result, nil
}

func qualitySummaryWindowMinutes(policy entity.ProviderQualityPolicy) (int, bool) {
	if policy.ProviderID == "" {
		return defaultQualityWindowMinutes, true
	}
	if policy.WindowMinutes < 5 || policy.WindowMinutes > 1440 {
		return 0, false
	}
	return policy.WindowMinutes, true
}

func providerQualityRange(db *gorm.DB, provider entity.Provider, from, to, observedAt time.Time) (*ProviderQuality, error) {
	unknownAttribution, err := providerQualityUnknownAttribution(db, from, to)
	if err != nil {
		return nil, err
	}
	mayLag, err := providerQualityMayLag(db)
	if err != nil {
		return nil, err
	}
	return providerQualityRangeWithContext(db, provider, from, to, observedAt, unknownAttribution, mayLag)
}

func providerQualityUnknownAttribution(db *gorm.DB, from, to time.Time) (int64, error) {
	var count int64
	if err := db.Model(&entity.CallAttempt{}).Where("completed_at >= ? AND completed_at < ? AND provider_id = ?", from, to, "").Count(&count).Error; err != nil {
		return 0, catalogError(err)
	}
	return count, nil
}

func providerQualityMayLag(db *gorm.DB) (bool, error) {
	var latestDelivery entity.SystemJob
	if err := db.Where("code = ?", SystemJobCallRecordDelivery).Order("updated_at DESC, id DESC").Limit(1).Find(&latestDelivery).Error; err != nil {
		return false, catalogError(err)
	}
	return latestDelivery.ID != "" && latestDelivery.Status == systemJobFailed, nil
}

func providerQualityRangeWithContext(db *gorm.DB, provider entity.Provider, from, to, observedAt time.Time, unknownAttribution int64, mayLag bool) (*ProviderQuality, error) {
	base := func() *gorm.DB {
		return db.Session(&gorm.Session{NewDB: true}).Table("call_attempts AS a").
			Where("a.completed_at >= ? AND a.completed_at < ?", from, to).
			Where("a.provider_id = ?", provider.ID)
	}
	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, catalogError(err)
	}
	if total+unknownAttribution > providerQualityMaxAttempts {
		return nil, providerQualityTooLarge
	}
	var aggregate providerQualityAggregate
	err := base().Select(`COUNT(*) AS requests,
		0 AS unknown_attribution_attempts,
		COALESCE(SUM(CASE WHEN a.work_evidence = 'not_sent' OR a.status = 'canceled' THEN 1 ELSE 0 END), 0) AS excluded_attempts,
		COALESCE(SUM(CASE WHEN a.work_evidence <> 'not_sent' AND a.status <> 'canceled' AND a.failure_class = 'credential_rejected' THEN 1 ELSE 0 END), 0) AS credential_rejected_attempts,
		COALESCE(SUM(CASE WHEN a.work_evidence <> 'not_sent' AND a.status <> 'canceled' AND a.failure_class <> 'credential_rejected' THEN 1 ELSE 0 END), 0) AS eligible_attempts,
		COALESCE(SUM(CASE WHEN a.work_evidence <> 'not_sent' AND a.status <> 'canceled' AND a.failure_class <> 'credential_rejected' AND a.status = 'success' THEN 1 ELSE 0 END), 0) AS successes,
		COALESCE(SUM(CASE WHEN a.work_evidence <> 'not_sent' AND a.status <> 'canceled' AND a.failure_class <> 'credential_rejected' AND (a.http_status = 429 OR a.failure_class = 'rate_limited') THEN 1 ELSE 0 END), 0) AS http429,
		COALESCE(SUM(CASE WHEN a.work_evidence <> 'not_sent' AND a.status <> 'canceled' AND a.failure_class <> 'credential_rejected' AND a.http_status >= 500 AND a.http_status < 600 THEN 1 ELSE 0 END), 0) AS http5xx,
		COALESCE(SUM(CASE WHEN a.work_evidence <> 'not_sent' AND a.status <> 'canceled' AND a.failure_class <> 'credential_rejected' AND a.duration_ms IS NOT NULL THEN 1 ELSE 0 END), 0) AS known_duration_attempts,
		MAX(a.completed_at) AS latest_completed_at`).Scan(&aggregate).Error
	if err != nil {
		return nil, catalogError(err)
	}
	aggregate.UnknownAttributionAttempts = unknownAttribution
	result := &ProviderQuality{
		ProviderID: provider.ID, ProviderName: provider.Name, WindowStart: from, WindowEnd: to,
		ObservedAt: observedAt, DataThrough: aggregate.LatestCompletedAt, Requests: aggregate.Requests,
		EligibleAttempts: aggregate.EligibleAttempts, ExcludedAttempts: aggregate.ExcludedAttempts,
		CredentialRejectedAttempts: aggregate.CredentialRejectedAttempts,
		UnknownAttributionAttempts: aggregate.UnknownAttributionAttempts, Successes: aggregate.Successes,
		HTTP429: aggregate.HTTP429, HTTP5XX: aggregate.HTTP5XX,
		KnownDurationAttempts:   aggregate.KnownDurationAttempts,
		UnknownDurationAttempts: aggregate.EligibleAttempts - aggregate.KnownDurationAttempts,
		LatestCompletedAt:       aggregate.LatestCompletedAt,
	}
	result.MayLag = mayLag
	if aggregate.EligibleAttempts > 0 {
		rate := int(aggregate.Successes * 10000 / aggregate.EligibleAttempts)
		result.SuccessRateBPS = &rate
		rateFloat := float64(aggregate.Successes) / float64(aggregate.EligibleAttempts)
		result.SuccessRate = &rateFloat
	}
	if aggregate.KnownDurationAttempts > 0 {
		offset := providerQualityP95Offset(aggregate.KnownDurationAttempts)
		var duration int64
		query := base().Where("a.work_evidence <> ? AND a.status <> ? AND a.failure_class <> ? AND a.duration_ms IS NOT NULL", "not_sent", "canceled", "credential_rejected").
			Order("a.duration_ms ASC, a.id ASC").Offset(offset).Limit(1).Pluck("a.duration_ms", &duration)
		if query.Error != nil {
			return nil, catalogError(query.Error)
		}
		result.P95DurationMS = &duration
	}
	return result, nil
}

func providerQualityP95Offset(count int64) int {
	return int(math.Ceil(float64(count)*0.95)) - 1
}

func qualityState(result *ProviderQuality, policy entity.ProviderQualityPolicy) (state, detail string) {
	if policy.ProviderID == "" || !policy.Enabled {
		return "unconfigured", ""
	}
	if result.EligibleAttempts < int64(policy.MinimumAttempts) {
		return "insufficient_data", ""
	}
	rateFailed := result.SuccessRateBPS == nil || *result.SuccessRateBPS < policy.MinSuccessRateBPS
	durationFailed := policy.MaxP95DurationMS != nil && (result.P95DurationMS == nil || *result.P95DurationMS > *policy.MaxP95DurationMS)
	switch {
	case rateFailed && durationFailed:
		return "degraded", "multiple_thresholds_breached"
	case rateFailed:
		return "degraded", "success_rate_below_threshold"
	case durationFailed:
		return "degraded", "p95_duration_above_threshold"
	default:
		return "healthy", ""
	}
}

func (s *Service) ProviderQualityPolicy(ctx context.Context, actor, providerID string) (*ProviderQualityPolicy, error) {
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actor, "system.read"); err != nil {
		return nil, catalogError(err)
	}
	if _, err := loadQualityProvider(db, providerID); err != nil {
		return nil, err
	}
	var row entity.ProviderQualityPolicy
	err := db.First(&row, "provider_id = ?", providerID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		result := defaultProviderQualityPolicy(providerID)
		return &result, nil
	}
	if err != nil {
		return nil, catalogError(err)
	}
	result := providerQualityPolicyView(row)
	return &result, nil
}

func (s *Service) WriteProviderQualityPolicy(ctx context.Context, actor, providerID string, input ProviderQualityPolicyInput) (*ProviderQualityPolicy, error) {
	if !validQualityPolicyInput(input) {
		return nil, apperrors.ErrBadRequest
	}
	now := time.Now().UTC()
	var saved entity.ProviderQualityPolicy
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeGovernance(tx, actor, "system.write"); err != nil {
			return err
		}
		if _, err := loadQualityProvider(tx, providerID); err != nil {
			return err
		}
		etag, err := id.NewPrefixed("rev")
		if err != nil {
			return err
		}
		saved = entity.ProviderQualityPolicy{
			ProviderID: providerID, Enabled: input.Enabled, WindowMinutes: input.WindowMinutes,
			MinimumAttempts: input.MinimumAttempts, MinSuccessRateBPS: input.MinSuccessRateBPS,
			MaxP95DurationMS: input.MaxP95DurationMS, ETag: etag, UpdatedBy: actor,
			UpdateReason: input.Reason, UpdatedAt: now,
		}
		if input.ETag == "0" {
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&saved)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return catalogConflict
			}
		} else {
			result := tx.Model(&entity.ProviderQualityPolicy{}).Where("provider_id = ? AND e_tag = ?", providerID, input.ETag).Updates(map[string]any{
				"enabled": input.Enabled, "window_minutes": input.WindowMinutes, "minimum_attempts": input.MinimumAttempts,
				"min_success_rate_bps": input.MinSuccessRateBPS, "max_p95_duration_ms": input.MaxP95DurationMS,
				"e_tag": etag, "updated_by": actor, "update_reason": input.Reason, "updated_at": now,
			})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return catalogConflict
			}
		}
		// A policy ETag defines one incident generation. Every successful
		// revision closes the previous generation before the new thresholds can
		// produce a future breach.
		if err := resetProviderQualityIncident(tx, providerID, now); err != nil {
			return err
		}
		details, err := json.Marshal(map[string]any{
			"enabled": input.Enabled, "window_minutes": input.WindowMinutes, "minimum_attempts": input.MinimumAttempts,
			"min_success_rate_bps": input.MinSuccessRateBPS, "max_p95_duration_ms": input.MaxP95DurationMS, "reason": input.Reason,
		})
		if err != nil {
			return err
		}
		detailsJSON := string(details)
		auditID, err := id.NewPrefixed("aud")
		if err != nil {
			return err
		}
		return tx.Create(&entity.AuditEvent{ID: auditID, ActorID: actor, Action: "provider_quality.policy.update", ResourceType: "provider", ResourceID: providerID, DetailsJSON: &detailsJSON}).Error
	})
	if err != nil {
		return nil, catalogError(err)
	}
	result := providerQualityPolicyView(saved)
	return &result, nil
}

func resetProviderQualityIncident(tx *gorm.DB, providerID string, now time.Time) error {
	if err := resolveOperationalAlertGroup(tx, "provider_quality:"+providerID, now); err != nil {
		return err
	}
	return tx.Model(&entity.ProviderQualityState{}).Where("provider_id = ?", providerID).Updates(map[string]any{
		"last_state": "insufficient_data", "last_alert_id": "", "updated_at": now,
	}).Error
}
