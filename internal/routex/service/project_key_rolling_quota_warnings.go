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

const projectKeyRollingWarningGeneration = "project-key-rolling-80-90-v1"

type projectKeyRollingWarningSample struct {
	Root    entity.ProjectKey
	Project entity.Project
	Rolling personalRollingWarningSample
}

func projectKeyRollingWarningSamples(row entity.ResourceLimit, root entity.ProjectKey, project entity.Project, frame *eventqueue.QuotaUsageProofBatch) []projectKeyRollingWarningSample {
	if frame == nil || !frame.Active || root.ProjectID != project.ID || root.ReplacesKeyID != nil || !projectWarningKeyID(root.ID) || !validCatalogLabel(root.Name) || project.Status != entity.ResourceActive || project.CreatedAt.IsZero() || row.DefaultResetETag != nil || row.AppliedDefaultETag != nil || root.CreatedAt.Before(project.CreatedAt) || project.CreatedAt.After(frame.AsOf) || row.ScopeKind != "key" || row.ScopeID != root.ID {
		return nil
	}
	if _, err := projectKeyPolicyFromRow(row, root, project); err != nil {
		return nil
	}
	proof, ok := frame.Accounts[limitAccount("key", root.ID)]
	if !ok || !proof.Usage.AsOf.Equal(frame.AsOf) || !proof.Usage.CoverageStart.Equal(frame.CoverageStart) || proof.Usage.TimeZone != frame.TimeZone {
		return nil
	}
	// Sampling is identical to Personal rolling warnings; identity/proof remains Key scoped.
	own := row
	own.ScopeKind = "user"
	result := []projectKeyRollingWarningSample{}
	for _, sample := range personalRollingWarningSamples(own, root.CreatedAt, proof) {
		result = append(result, projectKeyRollingWarningSample{Root: root, Project: project, Rolling: sample})
	}
	return result
}

func advanceProjectKeyRollingWarning(state entity.ProjectKeyRollingQuotaWarningState, sample projectKeyRollingWarningSample) (entity.ProjectKeyRollingQuotaWarningState, *entity.ProjectKeyRollingQuotaWarningObservation, error) {
	if state.RootKeyID != "" && (state.RootKeyID != sample.Root.ID || state.ProjectID != sample.Project.ID || !state.ProjectCreatedAt.Equal(sample.Project.CreatedAt)) {
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
	state = entity.ProjectKeyRollingQuotaWarningState{RootKeyID: sample.Root.ID, ProjectID: sample.Project.ID, ProjectCreatedAt: sample.Project.CreatedAt.UTC().Truncate(time.Microsecond), ResourceCreatedAt: next.ResourceCreatedAt, WindowKind: next.WindowKind, Cap: next.Cap, LastResetReview: next.LastResetReview, EpisodeID: next.EpisodeID, NearSent: next.NearSent, CriticalSent: next.CriticalSent, LastAsOf: next.LastAsOf}
	if observation == nil {
		return state, nil, nil
	}
	v := observation
	return state, &entity.ProjectKeyRollingQuotaWarningObservation{RootKeyID: sample.Root.ID, RootKeyName: sample.Root.Name, ProjectID: sample.Project.ID, ProjectCreatedAt: state.ProjectCreatedAt, ResourceCreatedAt: v.ResourceCreatedAt, WindowKind: v.WindowKind, EpisodeID: v.EpisodeID, PolicyRevision: v.PolicyRevision, WindowStart: v.WindowStart, WindowEnd: v.WindowEnd, AsOf: v.AsOf, CoverageStart: v.CoverageStart, TimeZone: v.TimeZone, Limit: v.Limit, Settled: v.Settled, Level: v.Level, Threshold: v.Threshold, ThresholdGeneration: projectKeyRollingWarningGeneration}, nil
}

func persistProjectKeyRollingWarning(tx *gorm.DB, sample projectKeyRollingWarningSample, recipients []projectQuotaWarningRecipient) error {
	if len(recipients) == 0 {
		return nil
	}
	if len(recipients) > quotaInboxManagerLimit {
		return errQuotaNotificationIdentity
	}
	seen := map[string]bool{}
	for _, recipient := range recipients {
		if !safeTeamSessionID(recipient.ID) || recipient.CreatedAt.IsZero() || recipient.CreatedAt.After(sample.Rolling.Observation.AsOf) || !safeTeamSessionID(recipient.ManagerID) || seen[recipient.ID] {
			return errQuotaNotificationIdentity
		}
		seen[recipient.ID] = true
	}
	query := tx.Model(&entity.ProjectKeyRollingQuotaWarningState{}).Where(database.ExactText(tx, clause.Column{Name: "root_key_id"}, sample.Root.ID)).Where(database.ExactText(tx, clause.Column{Name: "project_id"}, sample.Project.ID)).Where("project_created_at = ? AND resource_created_at = ?", sample.Project.CreatedAt, sample.Root.CreatedAt).Where(database.ExactText(tx, clause.Column{Name: "window_kind"}, sample.Rolling.Observation.WindowKind))
	var state entity.ProjectKeyRollingQuotaWarningState
	err := query.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&state).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	exists := err == nil
	state, observation, err := advanceProjectKeyRollingWarning(state, sample)
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
	observation.ID, err = id.NewPrefixed("qro")
	if err != nil {
		return err
	}
	if err = tx.Create(observation).Error; err != nil {
		return err
	}
	for _, recipient := range recipients {
		inboxID, err := id.NewPrefixed("qri")
		if err != nil {
			return err
		}
		if err = tx.Create(&entity.ProjectKeyRollingQuotaWarningInbox{ID: inboxID, ObservationID: observation.ID, RecipientID: recipient.ID, RecipientCreatedAt: recipient.CreatedAt.UTC().Truncate(time.Microsecond), CreatedAt: observation.AsOf}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) observeProjectKeyRollingQuotaWarnings(ctx context.Context, kind, rootID string) error {
	return s.observeProjectKeyQuotaWarning(ctx, kind, rootID, func(tx *gorm.DB, row entity.ResourceLimit, root entity.ProjectKey, project entity.Project, frame *eventqueue.QuotaUsageProofBatch, _ string, auth *runtimeAuthorization) ([]projectQuotaWarningRecipient, error) {
		samples := projectKeyRollingWarningSamples(row, root, project, frame)
		if len(samples) == 0 {
			return nil, nil
		}
		recipients, err := s.projectQuotaWarningRecipients(tx, auth, project.ID)
		if err != nil {
			return nil, err
		}
		for _, sample := range samples {
			if err := persistProjectKeyRollingWarning(tx, sample, recipients); err != nil {
				return nil, err
			}
		}
		return recipients, nil
	})
}
