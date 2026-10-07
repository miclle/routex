package service

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const personalRollingWarningGeneration = "personal-rolling-80-90-v1"

type personalRollingWarningSample struct {
	Observation entity.PersonalRollingQuotaWarningObservation
	ResetReview string
}

// Rolling sampling can skip directly to exhaustion. This still emits the90
// critical warning; it does not introduce a100 event or change monthly rules.
func personalRollingWarningLevel(settled, cap int64) (string, int) {
	if cap <= 0 || settled < 0 {
		return "", 0
	}
	if settled >= cap {
		return "critical", 90
	}
	return quotaWarningLevel(strconv.FormatInt(settled, 10), strconv.FormatInt(cap, 10))
}

func personalRollingDuration(kind string) time.Duration {
	switch kind {
	case "5h":
		return 5 * time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	default:
		return 0
	}
}

// Only fully covered known settled windows may change episode state. Holds are
// retained by accounting, but never added to this settled percentage.
func personalRollingWarningSamples(row entity.ResourceLimit, created time.Time, proof eventqueue.QuotaUsageAccountProof) []personalRollingWarningSample {
	usage := proof.Usage
	if row.ScopeKind != "user" || !proof.Registered || !proof.CreatedAt.Equal(created) || !validMonthlyQuotaFacts(row, created, &usage, 30) {
		return nil
	}
	if _, err := time.LoadLocation(usage.TimeZone); err != nil {
		return nil
	}
	result := []personalRollingWarningSample{}
	for _, window := range []struct {
		kind  string
		cap   *int64
		usage eventqueue.QuotaUsage
	}{{"5h", row.Tokens5H, usage.FiveHours}, {"7d", row.Tokens7D, usage.SevenDays}} {
		start := usage.AsOf.Add(-personalRollingDuration(window.kind))
		coveredFrom := start
		if created.After(coveredFrom) {
			coveredFrom = created
		}
		if coveredFrom.Before(usage.CoverageStart) || window.usage.TokensUnknown != 0 || window.usage.TokensUsed < 0 {
			continue
		}
		cap := int64(0)
		if window.cap != nil {
			cap = *window.cap
		}
		if cap < 0 {
			continue
		}
		reset := ""
		if row.DefaultResetETag != nil {
			reset = *row.DefaultResetETag
			if !safeCallID.MatchString(reset) || len(reset) > 64 {
				continue
			}
		}
		result = append(result, personalRollingWarningSample{Observation: entity.PersonalRollingQuotaWarningObservation{OwnerID: row.ScopeID, ResourceCreatedAt: created.UTC().Truncate(time.Microsecond), WindowKind: window.kind, PolicyRevision: row.ETag, WindowStart: start.UTC().Truncate(time.Microsecond), WindowEnd: usage.AsOf.UTC().Truncate(time.Microsecond), AsOf: usage.AsOf.UTC().Truncate(time.Microsecond), CoverageStart: usage.CoverageStart.UTC().Truncate(time.Microsecond), TimeZone: usage.TimeZone, ThresholdGeneration: personalRollingWarningGeneration, Limit: cap, Settled: window.usage.TokensUsed}, ResetReview: reset})
	}
	return result
}

// A reset review is consumed once and retained across ordinary full policy saves
// that clear default provenance. AsOf orders samples; it is never a dedup key.
func advancePersonalRollingWarning(state entity.PersonalRollingQuotaWarningState, sample personalRollingWarningSample) (entity.PersonalRollingQuotaWarningState, *entity.PersonalRollingQuotaWarningObservation, error) {
	v := sample.Observation
	if state.OwnerID != "" && (state.OwnerID != v.OwnerID || !state.ResourceCreatedAt.Equal(v.ResourceCreatedAt) || state.WindowKind != v.WindowKind) {
		return state, nil, errQuotaNotificationIdentity
	}
	if state.OwnerID != "" && (state.Cap < 0 || state.LastAsOf.IsZero() || state.CriticalSent && !state.NearSent || (state.NearSent || state.CriticalSent) && (!safeTeamSessionID(state.EpisodeID) || len(state.EpisodeID) < 4 || state.EpisodeID[:4] != "rwe_")) {
		return state, nil, errQuotaNotificationIdentity
	}
	if !state.LastAsOf.IsZero() && !v.AsOf.After(state.LastAsOf) {
		return state, nil, nil
	}
	changed := state.OwnerID == "" || state.Cap != v.Limit || sample.ResetReview != "" && sample.ResetReview != state.LastResetReview
	state.OwnerID, state.ResourceCreatedAt, state.WindowKind = v.OwnerID, v.ResourceCreatedAt, v.WindowKind
	state.LastAsOf = v.AsOf
	if changed {
		state.Cap = v.Limit
		state.EpisodeID = ""
		state.NearSent = false
		state.CriticalSent = false
	}
	if sample.ResetReview != "" {
		state.LastResetReview = sample.ResetReview
	}
	if v.Limit <= 0 {
		return state, nil, nil
	} // Zero remains enforced; there is no percentage denominator.
	// Compare without overflowing an int64 percentage product.
	level, threshold := personalRollingWarningLevel(v.Settled, v.Limit)
	if v.Settled < v.Limit && level == "" {
		state.EpisodeID = ""
		state.NearSent = false
		state.CriticalSent = false
		return state, nil, nil
	}
	if level == "" || level == "near" && state.NearSent || level == "critical" && state.CriticalSent {
		return state, nil, nil
	}
	if state.EpisodeID == "" {
		var err error
		state.EpisodeID, err = id.NewPrefixed("rwe")
		if err != nil {
			return state, nil, err
		}
	}
	v.EpisodeID, v.Level, v.Threshold = state.EpisodeID, level, threshold
	if level == "near" {
		state.NearSent = true
	} else {
		state.NearSent = true
		state.CriticalSent = true
	}
	return state, &v, nil
}

func personalRollingStateQuery(tx *gorm.DB, v entity.PersonalRollingQuotaWarningObservation) *gorm.DB {
	return tx.Model(&entity.PersonalRollingQuotaWarningState{}).Where(database.ExactText(tx, clause.Column{Name: "owner_id"}, v.OwnerID)).Where("resource_created_at = ?", v.ResourceCreatedAt).Where(database.ExactText(tx, clause.Column{Name: "window_kind"}, v.WindowKind))
}
func persistPersonalRollingWarning(tx *gorm.DB, sample personalRollingWarningSample) error {
	var state entity.PersonalRollingQuotaWarningState
	err := personalRollingStateQuery(tx, sample.Observation).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&state).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	exists := !errors.Is(err, gorm.ErrRecordNotFound)
	state, observation, err := advancePersonalRollingWarning(state, sample)
	if err != nil {
		return err
	}
	mutation := tx.Save
	if !exists {
		mutation = tx.Create
	}
	if err = mutation(&state).Error; err != nil {
		return err
	}
	if observation == nil {
		return nil
	}
	observation.ID, err = id.NewPrefixed("rwo")
	if err != nil {
		return err
	}
	if err = tx.Create(observation).Error; err != nil {
		return err
	}
	inboxID, err := id.NewPrefixed("rwi")
	if err != nil {
		return err
	}
	return tx.Create(&entity.PersonalRollingQuotaWarningInbox{ID: inboxID, ObservationID: observation.ID, RecipientID: observation.OwnerID, CreatedAt: observation.AsOf}).Error
}

func (s *Service) observePersonalRollingQuotaWarnings(ctx context.Context, kind, ownerID string) error {
	if kind != "user" || !safeTeamSessionID(ownerID) || s.recorder == nil || s.runtime == nil {
		return nil
	}
	s.limitMu.RLock()
	defer s.limitMu.RUnlock()
	return s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		var user entity.User
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "id"}, ownerID)).Take(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if user.ID != ownerID || user.Disabled || user.OffboardedAt != nil {
			return nil
		}
		applications, err := loadRegistrationApplications(tx, []entity.User{user})
		if err != nil {
			return err
		}
		admitted, _ := registrationAdmission(user, applications)
		if !admitted.AdmissionEligible {
			return nil
		}
		var row entity.ResourceLimit
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "scope_kind"}, "user")).Where(database.ExactText(tx, clause.Column{Name: "scope_id"}, ownerID)).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if row.ScopeKind != "user" || row.ScopeID != ownerID {
			return nil
		}
		policy, err := policyFromRow(row)
		if err != nil {
			return nil
		}
		var calendar entity.QuotaSetting
		if err = tx.Take(&calendar, 1).Error; err != nil {
			return err
		}
		var pricingSetting entity.PricingSetting
		if err = tx.Take(&pricingSetting, 1).Error; err != nil {
			return err
		}
		if !s.quotaWarningApplied(ctx, row, user, applications, policy, calendar, pricingSetting.PlatformCurrency) {
			return nil
		}
		status, err := s.recorder.queue.QuotaStatus()
		if err != nil {
			return runtimeUnavailable
		}
		if !status.Active {
			return nil
		}
		proofs, err := s.recorder.queue.AccountQuotaUsageProofBatch([]string{limitAccount("user", ownerID)}, time.Now())
		if err != nil {
			return runtimeUnavailable
		}
		if proofs == nil || !proofs.Active {
			return nil
		}
		proof := proofs.Accounts[limitAccount("user", ownerID)]
		usage := proof.Usage
		if usage.TimeZone != calendar.TimeZone || status.TimeZone != usage.TimeZone || status.CoverageStart == nil || !status.CoverageStart.Equal(usage.CoverageStart) {
			return nil
		}
		for _, sample := range personalRollingWarningSamples(row, user.CreatedAt, proof) {
			if err = persistPersonalRollingWarning(tx, sample); err != nil {
				return err
			}
		}
		if !s.quotaWarningApplied(ctx, row, user, applications, policy, calendar, pricingSetting.PlatformCurrency) {
			return runtimeUnavailable
		}
		return nil
	})
}
