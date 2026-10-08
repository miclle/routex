package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/limits"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const projectRollingWarningGeneration = "project-rolling-80-90-v1"

type projectRollingWarningSample struct {
	Project entity.Project
	Rolling personalRollingWarningSample
}

func projectRollingWarningSamples(row entity.ResourceLimit, project entity.Project, frame *eventqueue.QuotaUsageProofBatch) []projectRollingWarningSample {
	if frame == nil || !frame.Active || row.ScopeKind != "project" || row.ScopeID != project.ID || !projectMonthlyID(project.ID) || project.Status != entity.ResourceActive || project.CreatedAt.IsZero() || !validCatalogLabel(project.Name) || row.DefaultResetETag != nil || row.AppliedDefaultETag != nil {
		return nil
	}
	if _, err := policyFromRow(row); err != nil {
		return nil
	}
	proof, ok := frame.Accounts[limitAccount("project", project.ID)]
	if !ok || !proof.Usage.AsOf.Equal(frame.AsOf) || !proof.Usage.CoverageStart.Equal(frame.CoverageStart) || proof.Usage.TimeZone != frame.TimeZone {
		return nil
	}
	own := row
	own.ScopeKind = "user"
	result := []projectRollingWarningSample{}
	for _, sample := range personalRollingWarningSamples(own, project.CreatedAt, proof) {
		result = append(result, projectRollingWarningSample{Project: project, Rolling: sample})
	}
	return result
}

func advanceProjectRollingWarning(state entity.ProjectRollingQuotaWarningState, sample projectRollingWarningSample) (entity.ProjectRollingQuotaWarningState, *entity.ProjectRollingQuotaWarningObservation, error) {
	if state.ProjectID != "" && state.ProjectID != sample.Project.ID {
		return state, nil, errQuotaNotificationIdentity
	}
	shared := entity.PersonalRollingQuotaWarningState{OwnerID: state.ProjectID, ResourceCreatedAt: state.ResourceCreatedAt, WindowKind: state.WindowKind, Cap: state.Cap, LastResetReview: state.LastResetReview, EpisodeID: state.EpisodeID, NearSent: state.NearSent, CriticalSent: state.CriticalSent, LastAsOf: state.LastAsOf}
	// Only the pure sampled transition is shared; Project proof and recipient scope remain separate.
	next, observation, err := advancePersonalRollingWarning(shared, sample.Rolling)
	if err != nil {
		return state, nil, err
	}
	state = entity.ProjectRollingQuotaWarningState{ProjectID: sample.Project.ID, ResourceCreatedAt: next.ResourceCreatedAt, WindowKind: next.WindowKind, Cap: next.Cap, LastResetReview: next.LastResetReview, EpisodeID: next.EpisodeID, NearSent: next.NearSent, CriticalSent: next.CriticalSent, LastAsOf: next.LastAsOf}
	if observation == nil {
		return state, nil, nil
	}
	v := observation
	return state, &entity.ProjectRollingQuotaWarningObservation{ProjectID: sample.Project.ID, ProjectName: sample.Project.Name, ResourceCreatedAt: v.ResourceCreatedAt, WindowKind: v.WindowKind, EpisodeID: v.EpisodeID, PolicyRevision: v.PolicyRevision, WindowStart: v.WindowStart, WindowEnd: v.WindowEnd, AsOf: v.AsOf, CoverageStart: v.CoverageStart, TimeZone: v.TimeZone, Limit: v.Limit, Settled: v.Settled, Level: v.Level, Threshold: v.Threshold, ThresholdGeneration: projectRollingWarningGeneration}, nil
}

func persistProjectRollingWarning(tx *gorm.DB, sample projectRollingWarningSample, recipients []projectQuotaWarningRecipient) error {
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
	query := tx.Model(&entity.ProjectRollingQuotaWarningState{}).Where(database.ExactText(tx, clause.Column{Name: "project_id"}, sample.Project.ID)).Where("resource_created_at = ?", sample.Project.CreatedAt).Where(database.ExactText(tx, clause.Column{Name: "window_kind"}, sample.Rolling.Observation.WindowKind))
	var state entity.ProjectRollingQuotaWarningState
	err := query.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&state).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	exists := err == nil
	state, observation, err := advanceProjectRollingWarning(state, sample)
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
	observation.ID, err = id.NewPrefixed("jro")
	if err != nil {
		return err
	}
	if err = tx.Create(observation).Error; err != nil {
		return err
	}
	for _, recipient := range recipients {
		inboxID, err := id.NewPrefixed("jri")
		if err != nil {
			return err
		}
		if err = tx.Create(&entity.ProjectRollingQuotaWarningInbox{ID: inboxID, ObservationID: observation.ID, RecipientID: recipient.ID, RecipientCreatedAt: recipient.CreatedAt.UTC().Truncate(time.Microsecond), CreatedAt: observation.AsOf}).Error; err != nil {
			return err
		}
	}
	return nil
}

// A cached lease from a stopped publisher is not proof for a new rolling observation.
func (s *Service) projectRollingWarningApplied(ctx context.Context, auth *runtimeAuthorization, row entity.ResourceLimit, project entity.Project, policy limits.Policy, calendar entity.QuotaSetting, currency string) bool {
	rt := s.runtime
	if rt == nil || auth == nil || rt.auth.Load() != auth || auth.Quota == nil || !auth.Quota.Created[limitAccount("project", project.ID)].Equal(project.CreatedAt) || !s.projectQuotaWarningApplied(ctx, row, project, policy, calendar, currency) {
		return false
	}
	select {
	case <-rt.done:
		return false
	default:
		return s.runtime == rt && rt.auth.Load() == auth && ctx.Err() == nil
	}
}

func (s *Service) observeProjectRollingQuotaWarnings(ctx context.Context, kind, projectID string) error {
	if kind != "project" || !projectMonthlyID(projectID) || s.recorder == nil || s.runtime == nil {
		return nil
	}
	bounded, cancel := context.WithTimeout(ctx, runtimeRefreshTimeout)
	defer cancel()
	s.limitMu.RLock()
	defer s.limitMu.RUnlock()
	// The governance lock serializes episodes; read committed makes a waiter
	// see the preceding commit without relying on the database default.
	return s.authDB(bounded).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		var project entity.Project
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "id"}, projectID)).Take(&project).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if project.ID != projectID || project.Status != entity.ResourceActive || project.CreatedAt.IsZero() {
			return nil
		}
		var row entity.ResourceLimit
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "scope_kind"}, "project")).Where(database.ExactText(tx, clause.Column{Name: "scope_id"}, projectID)).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		policy, err := policyFromRow(row)
		if err != nil {
			return nil
		}
		var calendar entity.QuotaSetting
		if err = tx.Take(&calendar, 1).Error; err != nil {
			return err
		}
		var pricing entity.PricingSetting
		if err = tx.Take(&pricing, 1).Error; err != nil {
			return err
		}
		auth := s.runtime.auth.Load()
		if !s.projectRollingWarningApplied(bounded, auth, row, project, policy, calendar, pricing.PlatformCurrency) {
			return nil
		}
		frame, err := s.recorder.queue.AccountQuotaUsageProofBatch([]string{limitAccount("project", projectID)}, time.Now())
		if err != nil {
			return runtimeUnavailable
		}
		if frame == nil || frame.TimeZone != calendar.TimeZone {
			return nil
		}
		samples := projectRollingWarningSamples(row, project, frame)
		if len(samples) == 0 {
			return nil
		}
		recipients, err := s.projectQuotaWarningRecipients(tx, auth, projectID)
		if err != nil {
			return err
		}
		for _, sample := range samples {
			if err = persistProjectRollingWarning(tx, sample, recipients); err != nil {
				return err
			}
		}
		if s.runtime.auth.Load() != auth || !s.projectRollingWarningApplied(bounded, auth, row, project, policy, calendar, pricing.PlatformCurrency) {
			return runtimeUnavailable
		}
		for _, recipient := range recipients {
			if runtimeDenied(&s.runtime.deniedUsers, recipient.ID) || runtimeDenied(&s.runtime.deniedSessionUsers, recipient.ID) || runtimeDenied(&s.runtime.deniedProjects, projectID) {
				return runtimeUnavailable
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}
