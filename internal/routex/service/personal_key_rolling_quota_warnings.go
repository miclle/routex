package service

import (
	"context"
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const personalKeyRollingWarningGeneration = "personal-key-rolling-80-90-v1"

type personalKeyRollingWarningSample struct {
	Root    entity.APIKey
	Owner   entity.User
	Rolling personalRollingWarningSample
}

func personalKeyRollingWarningSamples(row entity.ResourceLimit, root entity.APIKey, owner entity.User, frame *eventqueue.QuotaUsageProofBatch) []personalKeyRollingWarningSample {
	if frame == nil || !frame.Active || root.UserID != owner.ID || root.ReplacesKeyID != nil || !memberKeyID.MatchString(root.ID) || !validCatalogLabel(root.Name) || owner.CreatedAt.IsZero() || root.CreatedAt.Before(owner.CreatedAt) || owner.CreatedAt.After(frame.AsOf) || row.ScopeKind != "key" || row.ScopeID != root.ID {
		return nil
	}
	if _, err := personalKeyPolicyFromRow(row, root, owner.ID); err != nil {
		return nil
	}
	proof, ok := frame.Accounts[limitAccount("key", root.ID)]
	if !ok || !proof.Usage.AsOf.Equal(frame.AsOf) || !proof.Usage.CoverageStart.Equal(frame.CoverageStart) || proof.Usage.TimeZone != frame.TimeZone {
		return nil
	}
	// Sampling is identical to Personal rolling warnings; identity/proof remains Key scoped.
	own := row
	own.ScopeKind = "user"
	result := []personalKeyRollingWarningSample{}
	for _, sample := range personalRollingWarningSamples(own, root.CreatedAt, proof) {
		result = append(result, personalKeyRollingWarningSample{Root: root, Owner: owner, Rolling: sample})
	}
	return result
}

func advancePersonalKeyRollingWarning(state entity.PersonalKeyRollingQuotaWarningState, sample personalKeyRollingWarningSample) (entity.PersonalKeyRollingQuotaWarningState, *entity.PersonalKeyRollingQuotaWarningObservation, error) {
	if state.RootKeyID != "" && (state.RootKeyID != sample.Root.ID || state.OwnerID != sample.Owner.ID || !state.OwnerCreatedAt.Equal(sample.Owner.CreatedAt)) {
		return state, nil, errQuotaNotificationIdentity
	}
	// Only the existing pure episode transition is shared. Its identity is the root account.
	shared := entity.PersonalRollingQuotaWarningState{ResourceCreatedAt: state.ResourceCreatedAt, WindowKind: state.WindowKind, Cap: state.Cap, LastResetReview: state.LastResetReview, EpisodeID: state.EpisodeID, NearSent: state.NearSent, CriticalSent: state.CriticalSent, LastAsOf: state.LastAsOf}
	if state.RootKeyID != "" {
		shared.OwnerID = state.RootKeyID
	}
	next, observation, err := advancePersonalRollingWarning(shared, sample.Rolling)
	if err != nil {
		return state, nil, err
	}
	state = entity.PersonalKeyRollingQuotaWarningState{RootKeyID: sample.Root.ID, OwnerID: sample.Owner.ID, OwnerCreatedAt: sample.Owner.CreatedAt.UTC().Truncate(time.Microsecond), ResourceCreatedAt: next.ResourceCreatedAt, WindowKind: next.WindowKind, Cap: next.Cap, LastResetReview: next.LastResetReview, EpisodeID: next.EpisodeID, NearSent: next.NearSent, CriticalSent: next.CriticalSent, LastAsOf: next.LastAsOf}
	if observation == nil {
		return state, nil, nil
	}
	v := observation
	return state, &entity.PersonalKeyRollingQuotaWarningObservation{RootKeyID: sample.Root.ID, RootKeyName: sample.Root.Name, OwnerID: sample.Owner.ID, OwnerCreatedAt: state.OwnerCreatedAt, ResourceCreatedAt: v.ResourceCreatedAt, WindowKind: v.WindowKind, EpisodeID: v.EpisodeID, PolicyRevision: v.PolicyRevision, WindowStart: v.WindowStart, WindowEnd: v.WindowEnd, AsOf: v.AsOf, CoverageStart: v.CoverageStart, TimeZone: v.TimeZone, Limit: v.Limit, Settled: v.Settled, Level: v.Level, Threshold: v.Threshold, ThresholdGeneration: personalKeyRollingWarningGeneration}, nil
}

func persistPersonalKeyRollingWarning(tx *gorm.DB, sample personalKeyRollingWarningSample) error {
	query := tx.Model(&entity.PersonalKeyRollingQuotaWarningState{}).Where(database.ExactText(tx, clause.Column{Name: "root_key_id"}, sample.Root.ID)).Where(database.ExactText(tx, clause.Column{Name: "owner_id"}, sample.Owner.ID)).Where("owner_created_at = ? AND resource_created_at = ?", sample.Owner.CreatedAt, sample.Root.CreatedAt).Where(database.ExactText(tx, clause.Column{Name: "window_kind"}, sample.Rolling.Observation.WindowKind))
	var state entity.PersonalKeyRollingQuotaWarningState
	err := query.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&state).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	exists := err == nil
	state, observation, err := advancePersonalKeyRollingWarning(state, sample)
	if err != nil {
		return err
	}
	mutation := tx.Create
	if exists {
		mutation = tx.Save
	}
	if err = mutation(&state).Error; err != nil {
		return err
	}
	if observation == nil {
		return nil
	}
	observation.ID, err = id.NewPrefixed("kro")
	if err != nil {
		return err
	}
	if err = tx.Create(observation).Error; err != nil {
		return err
	}
	inboxID, err := id.NewPrefixed("kri")
	if err != nil {
		return err
	}
	return tx.Create(&entity.PersonalKeyRollingQuotaWarningInbox{ID: inboxID, ObservationID: observation.ID, RecipientID: observation.OwnerID, RecipientCreatedAt: observation.OwnerCreatedAt, CreatedAt: observation.AsOf}).Error
}

func (s *Service) observePersonalKeyRollingQuotaWarnings(ctx context.Context, kind, rootID string) error {
	return s.observePersonalKeyQuotaWarning(ctx, kind, rootID, func(tx *gorm.DB, row entity.ResourceLimit, root entity.APIKey, owner entity.User, frame *eventqueue.QuotaUsageProofBatch, _ string) error {
		for _, sample := range personalKeyRollingWarningSamples(row, root, owner, frame) {
			if err := persistPersonalKeyRollingWarning(tx, sample); err != nil {
				return err
			}
		}
		return nil
	})
}
