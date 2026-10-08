package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/limits"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func teamRollingID(value string) bool {
	return len(value) == 30 && strings.HasPrefix(value, "tea_") && safeTeamSessionID(value)
}

const teamRollingWarningGeneration = "team-rolling-80-90-v1"

type teamRollingWarningSample struct {
	Team    entity.Team
	Rolling personalRollingWarningSample
}

func teamRollingWarningSamples(row entity.ResourceLimit, team entity.Team, frame *eventqueue.QuotaUsageProofBatch) []teamRollingWarningSample {
	if frame == nil || !frame.Active || row.ScopeKind != "team" || row.ScopeID != team.ID || !teamRollingID(team.ID) || team.Status != entity.ResourceActive || team.CreatedAt.IsZero() || !validCatalogLabel(team.Name) {
		return nil
	}
	if _, err := policyFromRow(row); err != nil {
		return nil
	}
	proof, ok := frame.Accounts[limitAccount("team", team.ID)]
	if !ok || !proof.Usage.AsOf.Equal(frame.AsOf) || !proof.Usage.CoverageStart.Equal(frame.CoverageStart) || proof.Usage.TimeZone != frame.TimeZone {
		return nil
	}
	own := row
	own.ScopeKind = "user"
	result := []teamRollingWarningSample{}
	for _, sample := range personalRollingWarningSamples(own, team.CreatedAt, proof) {
		result = append(result, teamRollingWarningSample{Team: team, Rolling: sample})
	}
	return result
}

func advanceTeamRollingWarning(state entity.TeamRollingQuotaWarningState, sample teamRollingWarningSample) (entity.TeamRollingQuotaWarningState, *entity.TeamRollingQuotaWarningObservation, error) {
	if state.TeamID != "" && state.TeamID != sample.Team.ID {
		return state, nil, errQuotaNotificationIdentity
	}
	shared := entity.PersonalRollingQuotaWarningState{OwnerID: state.TeamID, ResourceCreatedAt: state.ResourceCreatedAt, WindowKind: state.WindowKind, Cap: state.Cap, LastResetReview: state.LastResetReview, EpisodeID: state.EpisodeID, NearSent: state.NearSent, CriticalSent: state.CriticalSent, LastAsOf: state.LastAsOf}
	// Only the pure sampled transition is shared; Team proof and recipient scope remain separate.
	next, observation, err := advancePersonalRollingWarning(shared, sample.Rolling)
	if err != nil {
		return state, nil, err
	}
	state = entity.TeamRollingQuotaWarningState{TeamID: sample.Team.ID, ResourceCreatedAt: next.ResourceCreatedAt, WindowKind: next.WindowKind, Cap: next.Cap, LastResetReview: next.LastResetReview, EpisodeID: next.EpisodeID, NearSent: next.NearSent, CriticalSent: next.CriticalSent, LastAsOf: next.LastAsOf}
	if observation == nil {
		return state, nil, nil
	}
	v := observation
	return state, &entity.TeamRollingQuotaWarningObservation{TeamID: sample.Team.ID, TeamName: sample.Team.Name, ResourceCreatedAt: v.ResourceCreatedAt, WindowKind: v.WindowKind, EpisodeID: v.EpisodeID, PolicyRevision: v.PolicyRevision, WindowStart: v.WindowStart, WindowEnd: v.WindowEnd, AsOf: v.AsOf, CoverageStart: v.CoverageStart, TimeZone: v.TimeZone, Limit: v.Limit, Settled: v.Settled, Level: v.Level, Threshold: v.Threshold, ThresholdGeneration: teamRollingWarningGeneration}, nil
}

func persistTeamRollingWarning(tx *gorm.DB, sample teamRollingWarningSample, recipients []teamQuotaWarningRecipient) error {
	if len(recipients) == 0 {
		return nil
	}
	if len(recipients) > quotaInboxManagerLimit {
		return errQuotaNotificationIdentity
	}
	seen := map[string]bool{}
	for _, recipient := range recipients {
		if !safeTeamSessionID(recipient.ID) || recipient.CreatedAt.IsZero() || recipient.CreatedAt.After(sample.Rolling.Observation.AsOf) || !safeTeamSessionID(recipient.MembershipID) || seen[recipient.ID] {
			return errQuotaNotificationIdentity
		}
		seen[recipient.ID] = true
	}
	query := tx.Model(&entity.TeamRollingQuotaWarningState{}).Where(database.ExactText(tx, clause.Column{Name: "team_id"}, sample.Team.ID)).Where("resource_created_at = ?", sample.Team.CreatedAt).Where(database.ExactText(tx, clause.Column{Name: "window_kind"}, sample.Rolling.Observation.WindowKind))
	var state entity.TeamRollingQuotaWarningState
	err := query.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&state).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	exists := err == nil
	state, observation, err := advanceTeamRollingWarning(state, sample)
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
	observation.ID, err = id.NewPrefixed("tro")
	if err != nil {
		return err
	}
	if err = tx.Create(observation).Error; err != nil {
		return err
	}
	for _, recipient := range recipients {
		inboxID, err := id.NewPrefixed("tri")
		if err != nil {
			return err
		}
		if err = tx.Create(&entity.TeamRollingQuotaWarningInbox{ID: inboxID, ObservationID: observation.ID, RecipientID: recipient.ID, RecipientCreatedAt: recipient.CreatedAt.UTC().Truncate(time.Microsecond), CreatedAt: observation.AsOf}).Error; err != nil {
			return err
		}
	}
	return nil
}

// A cached lease from a stopped publisher is not proof for a new rolling observation.
func (s *Service) teamRollingWarningApplied(ctx context.Context, auth *runtimeAuthorization, row entity.ResourceLimit, team entity.Team, policy limits.Policy, calendar entity.QuotaSetting, currency string) bool {
	rt := s.runtime
	if rt == nil || auth == nil || rt.auth.Load() != auth || auth.Quota == nil || !auth.Quota.Created[limitAccount("team", team.ID)].Equal(team.CreatedAt) || !s.teamQuotaWarningApplied(ctx, row, team, policy, calendar, currency) || !auth.Teams[team.ID].CreatedAt.Equal(team.CreatedAt) {
		return false
	}
	select {
	case <-rt.done:
		return false
	default:
		return s.runtime == rt && rt.auth.Load() == auth && ctx.Err() == nil
	}
}

func (s *Service) observeTeamRollingQuotaWarnings(ctx context.Context, kind, teamID string) error {
	if kind != "team" || !teamRollingID(teamID) || s.recorder == nil || s.runtime == nil {
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
		var team entity.Team
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "id"}, teamID)).Take(&team).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if team.ID != teamID || team.Status != entity.ResourceActive || team.CreatedAt.IsZero() {
			return nil
		}
		var row entity.ResourceLimit
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "scope_kind"}, "team")).Where(database.ExactText(tx, clause.Column{Name: "scope_id"}, teamID)).Take(&row).Error
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
		if !s.teamRollingWarningApplied(bounded, auth, row, team, policy, calendar, pricing.PlatformCurrency) {
			return nil
		}
		frame, err := s.recorder.queue.AccountQuotaUsageProofBatch([]string{limitAccount("team", teamID)}, time.Now())
		if err != nil {
			return runtimeUnavailable
		}
		if frame == nil || frame.TimeZone != calendar.TimeZone {
			return nil
		}
		samples := teamRollingWarningSamples(row, team, frame)
		if len(samples) == 0 {
			return nil
		}
		recipients, err := s.teamQuotaWarningRecipients(tx, auth, teamID)
		if err != nil {
			return err
		}
		for _, sample := range samples {
			if err = persistTeamRollingWarning(tx, sample, recipients); err != nil {
				return err
			}
		}
		if s.runtime.auth.Load() != auth || !s.teamRollingWarningApplied(bounded, auth, row, team, policy, calendar, pricing.PlatformCurrency) {
			return runtimeUnavailable
		}
		for _, recipient := range recipients {
			if runtimeDenied(&s.runtime.deniedUsers, recipient.ID) || runtimeDenied(&s.runtime.deniedSessionUsers, recipient.ID) || runtimeDenied(&s.runtime.deniedTeams, teamID) || runtimeDenied(&s.runtime.deniedTeamMembers, teamMemberRuntimeKey(teamID, recipient.ID)) {
				return runtimeUnavailable
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}
