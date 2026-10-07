package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/limits"
)

var teamLimitFields = []string{"tokens_5h", "tokens_7d", "tokens_month", "money_month", "rpm", "tpm", "concurrency"}

// Creation/default policies retain the original seven caps. Only existing Team
// resource writes expose the two independent monthly behavior fields.
var teamResourceLimitFields = append(append([]string(nil), teamLimitFields...), "tokens_month_behavior", "money_month_behavior")

var teamMemberLimitFields = []string{"tokens_month", "money_month", "rpm", "tpm", "concurrency", "tokens_month_behavior", "money_month_behavior"}

// TeamLimitInput preserves omitted fields independently of explicit null and zero.
// A submitted field always requires its dimension's current write authority.
type TeamLimitInput struct {
	Reason string
	Fields map[string]json.RawMessage
}

func (input *TeamLimitInput) UnmarshalJSON(raw []byte) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return apperrors.ErrBadRequest
	}
	input.Reason = ""
	input.Fields = map[string]json.RawMessage{}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return apperrors.ErrBadRequest
		}
		name, ok := token.(string)
		if !ok || seen[name] || name != "reason" && name != "currency" && !slices.Contains(teamResourceLimitFields, name) {
			return apperrors.ErrBadRequest
		}
		seen[name] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return apperrors.ErrBadRequest
		}
		if name == "reason" {
			if err := json.Unmarshal(value, &input.Reason); err != nil {
				return apperrors.ErrBadRequest
			}
		} else {
			input.Fields[name] = value
		}
	}
	if _, err := decoder.Token(); err != nil {
		return apperrors.ErrBadRequest
	}
	if _, err := decoder.Token(); err != io.EOF {
		return apperrors.ErrBadRequest
	}
	return nil
}

func teamMemberLimitScopeID(teamID, userID string) string {
	return strings.TrimPrefix(teamMemberLimitAccount(teamID, userID), "team_member_")
}

func validateTeamLimitPolicy(kind string, policy limits.Policy) error {
	if kind != "team" && kind != "team_member" || policy.IPMode != "" && policy.IPMode != "none" || len(policy.IPRanges) != 0 {
		return limits.ErrInvalid
	}
	if kind == "team_member" && (policy.Tokens5H != nil || policy.Tokens7D != nil) {
		return limits.ErrInvalid
	}
	return nil
}

func teamLimitPermission(field string) string {
	switch field {
	case "tokens_5h", "tokens_7d", "tokens_month", "tokens_month_behavior":
		return "teams.tokens.write"
	case "money_month", "currency", "money_month_behavior":
		return "teams.money.write"
	default:
		return "teams.rates.write"
	}
}

func teamLimitEditableFields(tx *gorm.DB, actor entity.User, kind string) ([]string, error) {
	fields := teamResourceLimitFields
	if kind == "team_member" {
		fields = teamMemberLimitFields
	}
	allowed := map[string]bool{}
	for _, permission := range []string{"teams.tokens.write", "teams.money.write", "teams.rates.write"} {
		value, err := exactGovernancePermission(tx, actor, permission)
		if err != nil {
			return nil, err
		}
		allowed[permission] = value
	}
	result := []string{}
	for _, field := range fields {
		if allowed[teamLimitPermission(field)] {
			result = append(result, field)
		}
	}
	return result, nil
}

type teamLimitContext struct {
	Actor      entity.User
	Team       entity.Team
	Member     *entity.TeamMembership
	Resolved   resolvedLimitTarget
	Editable   []string
	Row        entity.ResourceLimit
	Stored     limits.Policy
	ParentRow  entity.ResourceLimit
	Parent     limits.Policy
	Pricing    entity.PricingSetting
	ReviewETag string
	ParentETag string
}

func loadTeamLimitContext(tx *gorm.DB, actorID, teamID, userID string, write bool) (*teamLimitContext, error) {
	if !safeTeamSessionID(teamID) || userID != "" && !safeTeamSessionID(userID) {
		return nil, apperrors.ErrBadRequest
	}
	actor, err := exactEnabledActor(tx, actorID)
	if err != nil {
		return nil, err
	}
	kind := "team"
	if userID != "" {
		kind = "team_member"
	}
	editable, err := teamLimitEditableFields(tx, actor, kind)
	if err != nil {
		return nil, err
	}
	platformRead, err := exactGovernancePermission(tx, actor, "teams.read_all")
	if err != nil {
		return nil, err
	}
	var own entity.TeamMembership
	membershipErr := tx.Where(database.ExactText(tx, clause.Column{Name: "team_id"}, teamID)).Where(database.ExactText(tx, clause.Column{Name: "user_id"}, actorID)).First(&own).Error
	if membershipErr != nil && !errors.Is(membershipErr, gorm.ErrRecordNotFound) {
		return nil, membershipErr
	}
	ownActive := membershipErr == nil && own.TeamID == teamID && own.UserID == actorID && own.Status == entity.ResourceActive && (own.Role == entity.TeamOwner || own.Role == entity.TeamMember)
	if write && len(editable) == 0 {
		return nil, apperrors.ErrForbidden
	}
	if !write && !platformRead && len(editable) == 0 && (!ownActive || userID != "" && userID != actorID && own.Role != entity.TeamOwner) {
		return nil, apperrors.ErrNotFound
	}
	query := tx.Where(database.ExactText(tx, clause.Column{Name: "id"}, teamID))
	if write {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	result := &teamLimitContext{Actor: actor, Editable: editable}
	if err := query.First(&result.Team).Error; err != nil {
		return nil, err
	}
	if result.Team.ID != teamID {
		return nil, apperrors.ErrNotFound
	}
	if result.Team.Status != entity.ResourceActive {
		if write {
			return nil, errLimitConflict
		}
		if !platformRead && len(editable) == 0 {
			return nil, apperrors.ErrNotFound
		}
	}
	result.Resolved = resolvedLimitTarget{kind: kind, id: teamID, teamID: teamID}
	if userID != "" {
		var user entity.User
		if err := tx.Where(database.ExactText(tx, clause.Column{Name: "id"}, userID)).Where("disabled = ? AND offboarded_at IS NULL", false).First(&user).Error; err != nil {
			return nil, err
		}
		var member entity.TeamMembership
		if err := tx.Where(database.ExactText(tx, clause.Column{Name: "team_id"}, teamID)).Where(database.ExactText(tx, clause.Column{Name: "user_id"}, userID)).First(&member).Error; err != nil {
			return nil, err
		}
		if user.ID != userID || member.TeamID != teamID || member.UserID != userID || member.Status != entity.ResourceActive || member.Role != entity.TeamOwner && member.Role != entity.TeamMember {
			return nil, apperrors.ErrNotFound
		}
		result.Member = &member
		result.Resolved = resolvedLimitTarget{kind: kind, id: teamMemberLimitScopeID(teamID, userID), parentKind: "team", parentID: teamID, teamID: teamID, userID: userID}
	}
	result.Row, result.Stored, err = readTeamLimitPolicy(tx, kind, result.Resolved.id)
	if err != nil {
		return nil, err
	}
	if result.Member != nil {
		result.ParentRow, result.Parent, err = readTeamLimitPolicy(tx, "team", teamID)
		if err != nil {
			return nil, err
		}
	}
	if err := tx.First(&result.Pricing, 1).Error; err != nil {
		return nil, err
	}
	result.ParentETag, err = teamLimitReviewETag(result.Team, nil, result.ParentRow, result.Parent, entity.ResourceLimit{}, limits.Policy{}, result.Pricing)
	if err != nil {
		return nil, err
	}
	result.ReviewETag, err = teamLimitReviewETag(result.Team, result.Member, result.Row, result.Stored, result.ParentRow, result.Parent, result.Pricing)
	return result, err
}

func readTeamLimitPolicy(tx *gorm.DB, kind, scopeID string) (entity.ResourceLimit, limits.Policy, error) {
	row := entity.ResourceLimit{ScopeKind: kind, ScopeID: scopeID, ETag: "0"}
	err := tx.Where(database.ExactText(tx, clause.Column{Name: "scope_kind"}, kind)).Where(database.ExactText(tx, clause.Column{Name: "scope_id"}, scopeID)).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	if err != nil {
		return row, limits.Policy{}, err
	}
	if row.ScopeKind != kind || row.ScopeID != scopeID {
		return row, limits.Policy{}, limits.ErrInvalid
	}
	policy, err := policyFromRow(row)
	if err == nil {
		err = validateTeamLimitPolicy(kind, policy)
	}
	return row, policy, err
}

func teamLimitReviewETag(team entity.Team, member *entity.TeamMembership, row entity.ResourceLimit, policy limits.Policy, parentRow entity.ResourceLimit, parent limits.Policy, pricing entity.PricingSetting) (string, error) {
	raw, err := json.Marshal(struct {
		TeamID, TeamStatus           string
		TeamCreatedAt, TeamUpdatedAt time.Time
		Member                       *entity.TeamMembership
		Revision                     string
		Policy                       limits.Policy
		ParentRevision               string
		Parent                       limits.Policy
		PricingRevision, Currency    string
	}{team.ID, team.Status, team.CreatedAt.UTC(), team.UpdatedAt.UTC(), member, row.ETag, policy, parentRow.ETag, parent, pricing.ETag, pricing.PlatformCurrency})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func applyTeamLimitInput(before limits.Policy, input TeamLimitInput, kind string, editable []string, platformCurrency string) (limits.Policy, error) {
	if len(input.Fields) == 0 {
		return before, apperrors.ErrBadRequest
	}
	supported := teamResourceLimitFields
	if kind == "team_member" {
		supported = teamMemberLimitFields
	}
	result := before
	for field, raw := range input.Fields {
		if field == "currency" {
			continue
		}
		if !slices.Contains(supported, field) {
			return before, apperrors.ErrBadRequest
		}
		if !slices.Contains(editable, field) {
			return before, apperrors.ErrForbidden
		}
		if field == "tokens_month_behavior" || field == "money_month_behavior" {
			var mode string
			if json.Unmarshal(raw, &mode) != nil || mode != limits.MonthlyBehaviorStop && mode != limits.MonthlyBehaviorAlertOnly {
				return before, apperrors.ErrBadRequest
			}
			if field == "tokens_month_behavior" {
				result.TokensMonthBehavior = mode
			} else {
				result.MoneyMonthBehavior = mode
			}
			continue
		}
		if field == "money_month" {
			var value *string
			if err := json.Unmarshal(raw, &value); err != nil {
				return before, apperrors.ErrBadRequest
			}
			result.MoneyMonth = value
			continue
		}
		var value *int64
		if err := json.Unmarshal(raw, &value); err != nil {
			return before, apperrors.ErrBadRequest
		}
		switch field {
		case "tokens_5h":
			result.Tokens5H = value
		case "tokens_7d":
			result.Tokens7D = value
		case "tokens_month":
			result.TokensMonth = value
		case "rpm":
			result.RPM = value
		case "tpm":
			result.TPM = value
		case "concurrency":
			result.Concurrency = value
		}
	}
	_, hasMoney := input.Fields["money_month"]
	currency, hasCurrency := input.Fields["currency"]
	if !hasMoney && hasCurrency || hasMoney && result.MoneyMonth == nil && hasCurrency {
		return before, apperrors.ErrBadRequest
	}
	if hasMoney {
		if result.MoneyMonth == nil {
			result.Currency = ""
		} else if !hasCurrency || json.Unmarshal(currency, &result.Currency) != nil || result.Currency != platformCurrency {
			return before, apperrors.ErrBadRequest
		}
	}
	normalized, err := limits.Normalize(result)
	if err != nil || validateTeamLimitPolicy(kind, normalized) != nil {
		return before, apperrors.ErrBadRequest
	}
	if normalized.MoneyMonth != nil && normalized.Currency != platformCurrency {
		return before, errLimitConflict
	}
	return normalized, nil
}

func (s *Service) GetTeamResourceLimit(ctx context.Context, actorID, teamID, userID string) (*LimitRecord, error) {
	var result *LimitRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := loadTeamLimitContext(tx, actorID, teamID, userID, false)
		if err != nil {
			return err
		}
		result, err = s.teamResourceLimitRecord(tx, current)
		return err
	})
	return result, catalogError(err)
}

func (s *Service) teamResourceLimitRecord(tx *gorm.DB, current *teamLimitContext) (*LimitRecord, error) {
	target := LimitTarget{Kind: current.Resolved.kind, ID: current.Team.ID, TeamID: current.Team.ID}
	if current.Member != nil {
		target.ID = current.Member.UserID
	}
	result, err := s.resourceLimitRecord(tx, target, current.Resolved)
	if err != nil {
		return nil, err
	}
	result.TeamID = current.Team.ID
	result.ETag = current.ReviewETag
	result.EditableFields = current.Editable
	if current.Member != nil {
		result.ParentETag = current.ParentETag
	}
	result.Enforced = s.teamLimitApplied(current)
	return result, nil
}

func (s *Service) teamLimitApplied(current *teamLimitContext) bool {
	if s.runtime == nil || s.recorder == nil {
		return false
	}
	auth := s.runtime.auth.Load()
	if auth == nil || !time.Now().Before(auth.ValidUntil) || auth.Quota == nil || runtimeDenied(&s.runtime.deniedTeams, current.Team.ID) || current.Team.Status != entity.ResourceActive {
		return false
	}
	team, exists := auth.Teams[current.Team.ID]
	if !exists {
		return false
	}
	if current.Member != nil && (team.Members[current.Member.UserID] != current.Member.ID || runtimeDenied(&s.runtime.deniedTeamMembers, teamMemberRuntimeKey(current.Team.ID, current.Member.UserID)) || runtimeDenied(&s.runtime.deniedUsers, current.Member.UserID)) {
		return false
	}
	accounts := []string{limitAccount(current.Resolved.kind, current.Resolved.id)}
	rows := []entity.ResourceLimit{current.Row}
	policies := []limits.Policy{current.Stored}
	if current.Member != nil {
		accounts = append(accounts, limitAccount("team", current.Team.ID))
		rows = append(rows, current.ParentRow)
		policies = append(policies, current.Parent)
	}
	for i, account := range accounts {
		revision := auth.Quota.Revisions[account]
		if revision == "" {
			revision = "0"
		}
		published, err := limits.Normalize(auth.LimitPolicies[account])
		if err != nil || revision != rows[i].ETag || !reflect.DeepEqual(published, policies[i]) || runtimeDenied(&s.runtime.deniedLimits, account) || policies[i].MoneyMonth != nil && (policies[i].Currency != auth.Quota.Currency || auth.Quota.Currency != current.Pricing.PlatformCurrency) {
			return false
		}
	}
	return !runtimeDenied(&s.runtime.deniedLimits, "quota_settings")
}

func (s *Service) SetTeamResourceLimit(ctx context.Context, actorID, teamID, userID, etag string, input TeamLimitInput) (*LimitRecord, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if !teamSessionDigest.MatchString(etag) || input.Reason == "" || len(input.Reason) > 2000 || !utf8.ValidString(input.Reason) || strings.ContainsFunc(input.Reason, unicode.IsControl) {
		return nil, apperrors.ErrBadRequest
	}
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	var resolved resolvedLimitTarget
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		current, err := loadTeamLimitContext(tx, actorID, teamID, userID, true)
		if err != nil {
			return err
		}
		resolved = current.Resolved
		policy, err := applyTeamLimitInput(current.Stored, input, resolved.kind, current.Editable, current.Pricing.PlatformCurrency)
		if err != nil {
			return err
		}
		if etag != current.ReviewETag {
			if current.Row.AppliedDefaultETag == nil && current.Row.DefaultResetETag == nil && current.Row.PreviousETag == etag && current.Row.ActorID == actorID && current.Row.Reason == input.Reason && reflect.DeepEqual(current.Stored, policy) {
				return nil
			}
			return errLimitConflict
		}
		if current.Member != nil && !limits.Narrower(current.Parent, policy) {
			return apperrors.ErrBadRequest
		}
		if reflect.DeepEqual(current.Stored, policy) {
			return nil
		}
		row, err := persistResourceLimitPolicy(tx, actorID, resolved, current.Row, current.Stored, policy, input.Reason)
		if err != nil {
			return err
		}
		return tx.Model(&entity.ResourceLimit{}).Where(database.ExactText(tx, clause.Column{Name: "scope_kind"}, row.ScopeKind)).Where(database.ExactText(tx, clause.Column{Name: "scope_id"}, row.ScopeID)).Update("PreviousETag", etag).Error
	})
	if err != nil {
		return nil, catalogError(err)
	}
	s.denyLimitScope(resolved.kind, resolved.id)
	if err := s.RefreshRuntime(ctx); err != nil {
		return nil, err
	}
	result, err := s.GetTeamResourceLimit(ctx, actorID, teamID, userID)
	if err != nil {
		return nil, err
	}
	if !result.Enforced {
		return nil, runtimeUnavailable
	}
	return result, nil
}

// Legacy records retain their established wire shape. Team records always expose
// an explicit empty editable_fields list when the reader has no write authority.
func (record LimitRecord) MarshalJSON() ([]byte, error) {
	type wire LimitRecord
	if record.TeamID == "" {
		return json.Marshal(wire(record))
	}
	fields := record.EditableFields
	if fields == nil {
		fields = []string{}
	}
	return json.Marshal(struct {
		*wire
		EditableFields []string `json:"editable_fields"`
	}{(*wire)(&record), fields})
}
