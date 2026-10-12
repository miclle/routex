package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	credentialInferenceFailureThreshold   = 5
	notificationSourceCredentialInference = "credential_inference"
	notificationKindCredentialInference   = "credential_inference_failure"
	notificationDetailCredentialInference = "five_consecutive_recorded_failures"
)

func credentialInferenceDigest(credentialID string) string {
	digest := sha256.Sum256([]byte("routex.credential-inference-failure:" + credentialID))
	return hex.EncodeToString(digest[:])
}

func validCredentialInferenceIdentity(value string) bool {
	return value != "" && len(value) <= 30 && safeCallID.MatchString(value)
}

func validCredentialInferenceEnvelope(kind, credentialID string) bool {
	if kind == notificationKindCredentialInference {
		return validCredentialInferenceIdentity(credentialID)
	}
	return credentialID == ""
}

func validCredentialInferenceState(state entity.CredentialInferenceFailureState) bool {
	optionalAttempt := func(value string) bool { return value == "" || safeCallID.MatchString(value) }
	return validCredentialInferenceIdentity(state.CredentialID) && state.CredentialDigest == credentialInferenceDigest(state.CredentialID) &&
		state.AcceptanceSequence >= 0 && state.ConsecutiveFailures >= 0 && state.ConsecutiveFailures <= credentialInferenceFailureThreshold && int64(state.ConsecutiveFailures) <= state.AcceptanceSequence &&
		(state.AcceptanceSequence == 0) == (state.LastAttemptID == "") && optionalAttempt(state.LastAttemptID) &&
		optionalAttempt(state.LastSuccessAttemptID) && optionalAttempt(state.LastTriggerAttemptID) &&
		state.Latched == (state.LastTriggerAttemptID != "") && (state.ConsecutiveFailures != credentialInferenceFailureThreshold || state.Latched) &&
		(state.AcceptanceSequence != 0 || state.LastSuccessAttemptID == "") &&
		!state.UpdatedAt.IsZero()
}

// Unknown/canceled observations break the proven contiguous count. They cannot
// rearm an already fired episode; only an explicitly recorded success does that.
func advanceCredentialInferenceFailure(state *entity.CredentialInferenceFailureState, attempt entity.CallAttempt, acceptedAt time.Time) (*entity.CredentialInferenceFailureEpisode, error) {
	if state == nil || !validCredentialInferenceState(*state) || attempt.CredentialID != state.CredentialID ||
		!safeCallID.MatchString(attempt.ID) || acceptedAt.IsZero() || state.AcceptanceSequence == math.MaxInt64 {
		return nil, fmt.Errorf("invalid credential inference observation")
	}
	state.AcceptanceSequence++
	state.LastAttemptID, state.UpdatedAt = attempt.ID, acceptedAt.UTC().Truncate(time.Microsecond)
	known := credentialStatisticsKnown(credentialStatisticsAttempt{ID: attempt.ID, CredentialID: attempt.CredentialID,
		Status: attempt.Status, StartedAt: attempt.StartedAt, CompletedAt: attempt.CompletedAt}, state.CredentialID, acceptedAt)
	if !known || attempt.Status == "canceled" {
		state.ConsecutiveFailures = 0
		return nil, nil
	}
	if attempt.Status == "success" {
		state.ConsecutiveFailures, state.Latched = 0, false
		state.LastSuccessAttemptID, state.LastTriggerAttemptID = attempt.ID, ""
		return nil, nil
	}
	if state.ConsecutiveFailures < credentialInferenceFailureThreshold {
		state.ConsecutiveFailures++
	}
	if state.Latched || state.ConsecutiveFailures != credentialInferenceFailureThreshold {
		return nil, nil
	}
	state.Latched, state.LastTriggerAttemptID = true, attempt.ID
	return &entity.CredentialInferenceFailureEpisode{TriggerAttemptID: attempt.ID, CredentialDigest: state.CredentialDigest,
		CredentialID: state.CredentialID, AcceptanceSequence: state.AcceptanceSequence, FailureCount: credentialInferenceFailureThreshold,
		LastSuccessAttemptID: state.LastSuccessAttemptID, RecordedAt: state.UpdatedAt}, nil
}

// The caller has inserted this fact and all its immutable Attempts in the same
// transaction. Duplicate RequestIDs never enter this helper. State locks are
// acquired in byte-exact Credential order before processing ordered Attempts.
func observeCredentialInferenceFailures(tx *gorm.DB, attempts []entity.CallAttempt, acceptedAt time.Time) error {
	if len(attempts) == 0 {
		return nil
	}
	identities := []string{}
	seen := make(map[string]bool, len(attempts))
	for _, attempt := range attempts {
		if attempt.CredentialID == "" {
			continue
		}
		if !validCredentialInferenceIdentity(attempt.CredentialID) {
			return fmt.Errorf("invalid credential inference identity")
		}
		if !seen[attempt.CredentialID] {
			identities = append(identities, attempt.CredentialID)
			seen[attempt.CredentialID] = true
		}
	}
	slices.Sort(identities)
	states := make(map[string]*entity.CredentialInferenceFailureState, len(identities))
	for _, credentialID := range identities {
		state := entity.CredentialInferenceFailureState{CredentialDigest: credentialInferenceDigest(credentialID), CredentialID: credentialID, UpdatedAt: acceptedAt.UTC().Truncate(time.Microsecond)}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "credential_digest"}}, DoNothing: true}).Create(&state).Error; err != nil {
			return err
		}
		var retained entity.CredentialInferenceFailureState
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("credential_digest = ?", state.CredentialDigest).Take(&retained).Error; err != nil {
			return err
		}
		if retained.CredentialID != credentialID || !validCredentialInferenceState(retained) {
			return fmt.Errorf("invalid retained credential inference state")
		}
		states[credentialID] = &retained
	}
	ordered := append([]entity.CallAttempt(nil), attempts...)
	slices.SortFunc(ordered, func(left, right entity.CallAttempt) int {
		return left.AttemptNumber - right.AttemptNumber
	})
	for _, attempt := range ordered {
		if attempt.CredentialID == "" {
			continue
		}
		episode, err := advanceCredentialInferenceFailure(states[attempt.CredentialID], attempt, acceptedAt)
		if err != nil {
			return err
		}
		if episode != nil {
			if err := tx.Create(episode).Error; err != nil {
				return err
			}
		}
	}
	for _, credentialID := range identities {
		state := states[credentialID]
		if err := tx.Model(&entity.CredentialInferenceFailureState{}).Where("credential_digest = ?", state.CredentialDigest).
			Updates(map[string]any{"acceptance_sequence": state.AcceptanceSequence, "consecutive_failures": state.ConsecutiveFailures,
				"latched": state.Latched, "last_attempt_id": state.LastAttemptID, "last_success_attempt_id": state.LastSuccessAttemptID,
				"last_trigger_attempt_id": state.LastTriggerAttemptID, "updated_at": state.UpdatedAt}).Error; err != nil {
			return err
		}
		if err := resolveCredentialInferenceAlert(tx, *state); err != nil {
			return err
		}
	}
	return nil
}

func validCredentialInferenceEpisode(episode entity.CredentialInferenceFailureEpisode) bool {
	return validCredentialInferenceIdentity(episode.CredentialID) && episode.CredentialDigest == credentialInferenceDigest(episode.CredentialID) &&
		safeCallID.MatchString(episode.TriggerAttemptID) && episode.AcceptanceSequence >= credentialInferenceFailureThreshold &&
		episode.FailureCount == credentialInferenceFailureThreshold && !episode.RecordedAt.IsZero() &&
		(episode.LastSuccessAttemptID == "" || safeCallID.MatchString(episode.LastSuccessAttemptID))
}

func credentialInferenceEpisodeActive(state entity.CredentialInferenceFailureState, episode entity.CredentialInferenceFailureEpisode) bool {
	return state.Latched && state.CredentialID == episode.CredentialID && state.LastTriggerAttemptID == episode.TriggerAttemptID &&
		state.LastSuccessAttemptID == episode.LastSuccessAttemptID
}

// The caller holds the exact state lock before acquiring this alert lock. A
// success resolves the old episode even when the next episode is not projected
// yet. It never rewrites immutable occurrences, recipient reads or SMTP facts.
func resolveCredentialInferenceAlert(tx *gorm.DB, state entity.CredentialInferenceFailureState) error {
	var alert entity.OperationalAlert
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("group_key = ? AND kind = ?", "credential_inference:"+state.CredentialDigest, notificationKindCredentialInference).Take(&alert).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if alert.CredentialID != state.CredentialID {
		return fmt.Errorf("invalid credential inference alert identity")
	}
	if alert.State == "resolved" {
		return nil
	}
	var episode entity.CredentialInferenceFailureEpisode
	if err := tx.Table("credential_inference_failure_episodes AS e").Select("e.*").
		Joins("JOIN operational_alert_occurrences AS o ON o.source_id = e.trigger_attempt_id AND o.source_type = ?", notificationSourceCredentialInference).
		Where("o.alert_id = ? AND e.credential_digest = ?", alert.ID, state.CredentialDigest).Order("e.acceptance_sequence DESC").Clauses(clause.Locking{Strength: "UPDATE"}).Take(&episode).Error; err != nil {
		return err
	}
	if !validCredentialInferenceEpisode(episode) || episode.AcceptanceSequence > state.AcceptanceSequence {
		return fmt.Errorf("invalid credential inference recovery provenance")
	}
	if credentialInferenceEpisodeActive(state, episode) {
		return nil
	}
	etag, err := id.NewPrefixed("rev")
	if err != nil {
		return err
	}
	return tx.Model(&alert).Updates(map[string]any{"state": "resolved", "e_tag": etag, "updated_at": maxTime(alert.UpdatedAt, state.UpdatedAt)}).Error
}

// Immutable episodes recover missed notices. Candidate reads acquire no locks:
// state locks always precede alert locks, in the same exact-ID order as calls.
func (s *Service) reconcileCredentialInferenceFailures(tx *gorm.DB) error {
	var episodes []entity.CredentialInferenceFailureEpisode
	if err := tx.Model(&entity.CredentialInferenceFailureEpisode{}).
		Where("NOT EXISTS (SELECT 1 FROM operational_alert_occurrences o WHERE o.source_type = ? AND o.source_id = credential_inference_failure_episodes.trigger_attempt_id)", notificationSourceCredentialInference).
		Order("recorded_at, trigger_attempt_id").Limit(100).Find(&episodes).Error; err != nil {
		return err
	}
	// Include already-projected, now recovered alerts after a worker restart.
	// The exact column comparison is a database-owned portability adapter.
	alerts := tx.Table("operational_alerts AS a").Select("1").Where("a.kind = ? AND a.state <> ?", notificationKindCredentialInference, "resolved").
		Where(database.ExactTextColumns(tx, clause.Column{Table: "a", Name: "credential_id"}, clause.Column{Table: "credential_inference_failure_states", Name: "credential_id"}))
	var recovered []entity.CredentialInferenceFailureState
	if err := tx.Where("latched = ?", false).Where("EXISTS (?)", alerts).Order("credential_digest").Limit(100).Find(&recovered).Error; err != nil {
		return err
	}
	identities := make(map[string]string, len(episodes)+len(recovered))
	for _, episode := range episodes {
		if !validCredentialInferenceEpisode(episode) {
			return fmt.Errorf("invalid retained credential inference episode")
		}
		identities[episode.CredentialID] = episode.CredentialDigest
	}
	for _, state := range recovered {
		if !validCredentialInferenceState(state) {
			return fmt.Errorf("invalid retained credential inference recovery state")
		}
		identities[state.CredentialID] = state.CredentialDigest
	}
	orderedIDs := make([]string, 0, len(identities))
	for credentialID := range identities {
		orderedIDs = append(orderedIDs, credentialID)
	}
	slices.Sort(orderedIDs)
	states := make(map[string]entity.CredentialInferenceFailureState, len(identities))
	for _, credentialID := range orderedIDs {
		var state entity.CredentialInferenceFailureState
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("credential_digest = ?", identities[credentialID]).Take(&state).Error; err != nil {
			return err
		}
		if !validCredentialInferenceState(state) || state.CredentialID != credentialID {
			return fmt.Errorf("invalid retained credential inference projection state")
		}
		states[credentialID] = state
	}
	for _, episode := range episodes {
		state := states[episode.CredentialID]
		if episode.AcceptanceSequence > state.AcceptanceSequence {
			return fmt.Errorf("invalid credential inference acceptance sequence")
		}
		var attempt entity.CallAttempt
		if err := tx.Where(database.ExactText(tx, clause.Column{Name: "id"}, episode.TriggerAttemptID)).Take(&attempt).Error; err != nil {
			return err
		}
		known := credentialStatisticsKnown(credentialStatisticsAttempt{ID: attempt.ID, CredentialID: attempt.CredentialID, Status: attempt.Status,
			StartedAt: attempt.StartedAt, CompletedAt: attempt.CompletedAt}, episode.CredentialID, episode.RecordedAt)
		if !known || attempt.ID != episode.TriggerAttemptID || attempt.Status != "error" {
			return fmt.Errorf("invalid credential inference episode provenance")
		}
		subjectType, subjectID, subjectName := "", "", ""
		if validNotificationSubject("provider", attempt.ProviderID, attempt.ProviderName) && len(attempt.ProviderID) <= 30 && len(attempt.ProviderName) <= 400 {
			subjectType, subjectID, subjectName = "provider", attempt.ProviderID, attempt.ProviderName
		}
		if err := s.recordOperationalAlertOccurrenceProjection(tx, notificationSourceCredentialInference, episode.TriggerAttemptID,
			"credential_inference:"+episode.CredentialDigest, notificationKindCredentialInference, "medium", notificationDetailCredentialInference,
			subjectType, subjectID, subjectName, episode.RecordedAt, 0, episode.CredentialID, &state); err != nil {
			return err
		}
	}
	for _, credentialID := range orderedIDs {
		if err := resolveCredentialInferenceAlert(tx, states[credentialID]); err != nil {
			return err
		}
	}
	return nil
}
