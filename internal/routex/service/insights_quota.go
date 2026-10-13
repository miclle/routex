package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type insightsQuotaTarget struct {
	row     InsightsQuotaRow
	kind    string
	created time.Time
	policy  limits.Policy
}

func insightsQuotaCursor(actor string, input InsightsQuotaInput, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join([]string{actor, input.ScopeKind, input.ScopeID, input.Dimension, id}, "|")))
}

func normalizeInsightsQuota(actor string, input InsightsQuotaInput, keys bool) (InsightsQuotaInput, string, error) {
	if !safeTeamSessionID(actor) {
		return input, "", apperrors.ErrUnauthorized
	}
	if input.Limit == 0 {
		input.Limit = 20
	}
	if input.Limit < 1 || input.Limit > 50 || len(input.Cursor) > 160 || input.Dimension != "tokens" && input.Dimension != "money" {
		return input, "", apperrors.ErrBadRequest
	}
	if keys {
		if input.ScopeKind != "member" && input.ScopeKind != "project" || !safeTeamSessionID(input.ScopeID) {
			return input, "", apperrors.ErrBadRequest
		}
	} else if input.ScopeKind != "" || input.ScopeID != "" {
		return input, "", apperrors.ErrBadRequest
	}
	if input.Cursor == "" {
		return input, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(input.Cursor)
	parts := strings.Split(string(raw), "|")
	if err != nil || len(parts) != 5 || parts[0] != actor || parts[1] != input.ScopeKind || parts[2] != input.ScopeID || parts[3] != input.Dimension || !safeTeamSessionID(parts[4]) || base64.RawURLEncoding.EncodeToString(raw) != input.Cursor {
		return input, "", apperrors.ErrBadRequest
	}
	return input, parts[4], nil
}

func insightsQuotaTeams(tx *gorm.DB, actorID, after string, limit int) ([]insightsQuotaTarget, bool, error) {
	actor, err := exactEnabledActor(tx, actorID)
	if err != nil {
		return nil, false, err
	}
	all, err := exactGovernancePermission(tx, actor, "teams.read_all")
	if err != nil {
		return nil, false, err
	}
	editable, err := teamLimitEditableFields(tx, actor, "team")
	if err != nil {
		return nil, false, err
	}
	query := tx.Model(&entity.Team{})
	if !all && len(editable) == 0 {
		members := tx.Model(&entity.TeamMembership{}).Select("team_id").Where(database.ExactText(tx, clause.Column{Name: "user_id"}, actorID)).Where(database.ExactText(tx, clause.Column{Name: "status"}, entity.ResourceActive)).Where(clause.Or(database.ExactText(tx, clause.Column{Name: "role"}, entity.TeamOwner), database.ExactText(tx, clause.Column{Name: "role"}, entity.TeamMember)))
		query = query.Where("id IN (?)", members).Where(database.ExactText(tx, clause.Column{Name: "status"}, entity.ResourceActive))
	}
	if after != "" {
		query = query.Where("id > ?", after)
	}
	var teams []entity.Team
	if err := query.Order("id").Limit(limit + 1).Find(&teams).Error; err != nil {
		return nil, false, err
	}
	more := len(teams) > limit
	if more {
		teams = teams[:limit]
	}
	targets := make([]insightsQuotaTarget, 0, len(teams))
	for _, team := range teams {
		// The final exact resource read is independent of directory discovery
		// and rejects collation aliases, invalid memberships and inactive scope.
		current, err := loadTeamLimitContext(tx, actorID, team.ID, "", false)
		if err != nil {
			return nil, false, err
		}
		if !validCatalogLabel(team.Name) || team.CreatedAt.IsZero() || team.Status != entity.ResourceActive && team.Status != entity.ResourceDisabled && team.Status != entity.ResourceArchived {
			return nil, false, apperrors.ErrInternal
		}
		targets = append(targets, insightsQuotaTarget{row: InsightsQuotaRow{ID: team.ID, Name: team.Name, Status: team.Status}, kind: "team", created: team.CreatedAt, policy: current.Stored})
	}
	return targets, more, nil
}

func insightsQuotaKeyPolicy(tx *gorm.DB, id string) (limits.Policy, error) {
	row := entity.ResourceLimit{ScopeKind: "key", ScopeID: id, ETag: "0"}
	err := tx.Where(database.ExactText(tx, clause.Column{Name: "scope_kind"}, "key")).Where(database.ExactText(tx, clause.Column{Name: "scope_id"}, id)).Take(&row).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return limits.Policy{}, err
	}
	if row.ScopeKind != "key" || row.ScopeID != id {
		return limits.Policy{}, apperrors.ErrNotFound
	}
	// The complete exact-owner graph is established before this call. A Key's
	// local monthly behavior is independent of its owner's conjunctive limits.
	return decodeLimitPolicy(row, true)
}

func insightsQuotaKeys(tx *gorm.DB, actor string, input InsightsQuotaInput, after string, now time.Time) ([]insightsQuotaTarget, bool, error) {
	targets := []insightsQuotaTarget{}
	counts := map[string]int{}
	live := map[string]bool{}
	if input.ScopeKind == "member" {
		_, subject, err := memberKeyPeople(tx, actor, input.ScopeID, "members.read")
		if err != nil {
			return nil, false, err
		}
		if err := tx.Model(&entity.User{}).Select("created_at").Where(database.ExactText(tx, clause.Column{Name: "id"}, subject.ID)).Take(&subject).Error; err != nil {
			return nil, false, err
		}
		if subject.CreatedAt.IsZero() {
			return nil, false, apperrors.ErrInternal
		}
		keys, roots, err := readMemberOwnedKeys(tx, subject.ID)
		if err != nil {
			return nil, false, err
		}
		for _, key := range keys {
			if !safeTeamSessionID(key.ID) || key.CreatedAt.IsZero() || key.CreatedAt.Before(subject.CreatedAt) || key.Status != entity.KeyActive && key.Status != entity.KeyPending && key.Status != entity.KeyDisabled && key.Status != entity.KeyRevoked {
				return nil, false, apperrors.ErrInternal
			}
			root := roots[key.ID]
			counts[root]++
			live[root] = live[root] || !subject.Disabled && subject.OffboardedAt == nil && key.Status == entity.KeyActive && (key.ExpiresAt == nil || now.Before(*key.ExpiresAt))
			if key.ID == root {
				targets = append(targets, insightsQuotaTarget{row: InsightsQuotaRow{ID: key.ID, Name: key.Name, Status: key.Status}, kind: "key", created: key.CreatedAt})
			}
		}
	} else {
		if err := projectKeyAccess(tx, actor, input.ScopeID); err != nil {
			return nil, false, err
		}
		var project entity.Project
		if err := tx.Where(database.ExactText(tx, clause.Column{Name: "id"}, input.ScopeID)).Take(&project).Error; err != nil {
			return nil, false, err
		}
		if project.ID != input.ScopeID {
			return nil, false, apperrors.ErrNotFound
		}
		var keys []entity.ProjectKey
		if err := projectKeyWarningChainQuery(tx, project.ID).Find(&keys).Error; err != nil {
			return nil, false, err
		}
		if len(keys) == 0 {
			return targets, false, nil
		}
		if len(keys) > projectKeyWarningChainLimit {
			return nil, false, &apperrors.Error{Code: 400, Message: "Project Key analysis exceeds its complete-query bound"}
		}
		roots, valid := projectKeyWarningRoots(keys, project)
		if !valid {
			return nil, false, apperrors.ErrInternal
		}
		for _, key := range keys {
			root := roots[key.ID]
			counts[root]++
			live[root] = live[root] || project.Status == entity.ResourceActive && key.Status == entity.KeyActive && (key.ExpiresAt == nil || now.Before(*key.ExpiresAt))
			if key.ID == root {
				targets = append(targets, insightsQuotaTarget{row: InsightsQuotaRow{ID: key.ID, Name: key.Name, Status: key.Status}, kind: "key", created: key.CreatedAt})
			}
		}
	}
	slices.SortFunc(targets, func(a, b insightsQuotaTarget) int { return strings.Compare(a.row.ID, b.row.ID) })
	selected := make([]insightsQuotaTarget, 0, input.Limit+1)
	for _, target := range targets {
		if after != "" && target.row.ID <= after {
			continue
		}
		if !validCatalogLabel(target.row.Name) {
			return nil, false, apperrors.ErrInternal
		}
		target.row.SharedKeyCount = counts[target.row.ID]
		if live[target.row.ID] {
			target.row.Status = entity.KeyActive
		} else if target.row.Status == entity.KeyActive {
			target.row.Status = entity.KeyDisabled
		}
		policy, err := insightsQuotaKeyPolicy(tx, target.row.ID)
		if err != nil {
			return nil, false, err
		}
		target.policy = policy
		selected = append(selected, target)
		if len(selected) > input.Limit {
			break
		}
	}
	more := len(selected) > input.Limit
	if more {
		selected = selected[:input.Limit]
	}
	// Both key tables feed the same journal namespace. Even a malformed row in
	// the other table cannot lend its account proof to this authorized owner.
	if len(selected) != 0 {
		ids := make([]string, len(selected))
		for i, target := range selected {
			ids[i] = target.row.ID
		}
		var collisionIDs []string
		model := any(&entity.ProjectKey{})
		if input.ScopeKind == "project" {
			model = &entity.APIKey{}
		}
		if err := tx.Model(model).Where("id IN ?", ids).Limit(len(ids)).Pluck("id", &collisionIDs).Error; err != nil {
			return nil, false, err
		}
		if len(collisionIDs) != 0 {
			return nil, false, apperrors.ErrInternal
		}
	}
	return selected, more, nil
}

// InsightsQuota reads one bounded SQL resource/policy snapshot and one coherent
// journal proof batch. It never reconstructs quota history from delayed Calls,
// aggregates parent caps, invokes inference, or claims runtime enforcement.
func (s *Service) InsightsQuota(ctx context.Context, actor string, input InsightsQuotaInput, keys bool) (*InsightsQuotaReport, error) {
	input, after, err := normalizeInsightsQuota(actor, input, keys)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := &InsightsQuotaReport{Kind: "team_quota", Dimension: input.Dimension, Scope: InsightsQuotaScope{Kind: "readable_teams"}, BaselineBasis: "settled_admissions_168h", Items: []InsightsQuotaRow{}}
	if keys {
		result.Kind = "key_quota"
		result.Scope = InsightsQuotaScope{Kind: input.ScopeKind, ID: input.ScopeID}
	}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeUsage(tx, actor, "admin", ""); err != nil {
			return err
		}
		now := time.Now().UTC()
		var targets []insightsQuotaTarget
		var more bool
		var err error
		if keys {
			targets, more, err = insightsQuotaKeys(tx, actor, input, after, now)
		} else {
			targets, more, err = insightsQuotaTeams(tx, actor, after, input.Limit)
		}
		if err != nil {
			return err
		}
		var calendar entity.QuotaSetting
		if err := memberOverviewCalendarQuery(tx).Take(&calendar, 1).Error; err != nil {
			return err
		}
		result.TimeZone = calendar.TimeZone
		if input.Dimension == "money" {
			var setting entity.PricingSetting
			if err := tx.Select("platform_currency").Take(&setting, 1).Error; err != nil {
				return err
			}
			if !pricing.Currency(setting.PlatformCurrency) {
				return apperrors.ErrInternal
			}
			result.Currency = setting.PlatformCurrency
		}
		accounts := make([]string, len(targets))
		for i, target := range targets {
			accounts[i] = limitAccount(target.kind, target.row.ID)
		}
		var batch *eventqueue.QuotaUsageProofBatch
		if len(accounts) != 0 && s.recorder != nil {
			batch, _ = s.recorder.queue.AccountQuotaUsageProofBatch(accounts, now)
		}
		if batch != nil && batch.Active && batch.TimeZone == calendar.TimeZone {
			start, reset, valid := insightsQuotaCalendar(batch.AsOf, batch.TimeZone)
			if valid {
				result.AsOf, result.CoverageStart = insightsQuotaTime(batch.AsOf), insightsQuotaTime(batch.CoverageStart)
				result.BaselineFrom, result.BaselineTo = insightsQuotaTime(batch.AsOf.Add(-7*24*time.Hour)), insightsQuotaTime(batch.AsOf)
				result.MonthFrom, result.MonthReset = insightsQuotaTime(start), insightsQuotaTime(reset)
			}
		}
		for _, target := range targets {
			var proof *eventqueue.QuotaUsageAccountProof
			if result.AsOf != nil {
				value, exists := batch.Accounts[limitAccount(target.kind, target.row.ID)]
				if exists && value.Usage.AsOf.Equal(batch.AsOf) && value.Usage.CoverageStart.Equal(batch.CoverageStart) && value.Usage.TimeZone == batch.TimeZone {
					proof = &value
				}
			}
			result.Items = append(result.Items, insightsQuotaProject(target.row, target.policy, input.Dimension, result.Currency, target.created, proof))
		}
		if more {
			cursor := insightsQuotaCursor(actor, input, targets[len(targets)-1].row.ID)
			result.NextCursor = &cursor
		}
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
