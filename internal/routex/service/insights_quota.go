package service

import (
	"context"
	"database/sql"
	"encoding/base64"
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
	platformRead := false
	for _, permission := range []string{"teams.read_all", "teams.tokens.write", "teams.money.write", "teams.rates.write"} {
		allowed, err := exactGovernancePermissionForAdmittedActor(tx, actor, permission)
		if err != nil {
			return nil, false, err
		}
		platformRead = platformRead || allowed
	}
	query := insightsQuotaTeamQuery(tx, actorID, platformRead)
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
		if !safeTeamSessionID(team.ID) || !validCatalogLabel(team.Name) || team.CreatedAt.IsZero() || team.Status != entity.ResourceActive && team.Status != entity.ResourceDisabled && team.Status != entity.ResourceArchived {
			return nil, false, apperrors.ErrInternal
		}
		targets = append(targets, insightsQuotaTarget{row: InsightsQuotaRow{ID: team.ID, Name: team.Name, Status: team.Status}, kind: "team", created: team.CreatedAt})
	}
	if err := hydrateInsightsQuotaPolicies(tx, targets); err != nil {
		return nil, false, err
	}
	return targets, more, nil
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
		selected = append(selected, target)
		if len(selected) > input.Limit {
			break
		}
	}
	// Include the lookahead root, as the previous per-root reads did, so an
	// invalid stored policy cannot be hidden just beyond the returned page.
	if err := hydrateInsightsQuotaPolicies(tx, selected); err != nil {
		return nil, false, err
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
			result.keyScopeProof, err = insightsAIKeyScope(tx, actor, input.ScopeKind, input.ScopeID)
			if err != nil {
				return err
			}
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
		// Team reads historically require the pricing row even for Tokens.
		// Capture it once for the selected page, rather than once per Team.
		if input.Dimension == "money" || !keys && len(targets) != 0 {
			var setting entity.PricingSetting
			if err := tx.Select("platform_currency").Take(&setting, 1).Error; err != nil {
				return err
			}
			if input.Dimension == "money" {
				if !pricing.Currency(setting.PlatformCurrency) {
					return apperrors.ErrInternal
				}
				result.Currency = setting.PlatformCurrency
			}
		}
		accounts := make([]string, len(targets))
		for i, target := range targets {
			accounts[i] = limitAccount(target.kind, target.row.ID)
		}
		var batch *eventqueue.QuotaUsageProofBatch
		if len(accounts) != 0 && s.recorder != nil && s.admission != nil {
			batch, _ = s.admission.AccountQuotaUsageProofBatch(ctx, accounts, now)
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
			if !keys {
				result.teamProofs = append(result.teamProofs, insightsAITeamProof{ID: target.row.ID, CreatedAt: target.created})
			} else {
				result.keyProofs = append(result.keyProofs, insightsAIKeyProof{ID: target.row.ID, CreatedAt: target.created})
			}
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
