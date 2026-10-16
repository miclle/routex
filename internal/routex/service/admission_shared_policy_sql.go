package service

import (
	"errors"
	"net/netip"
	"reflect"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// validateSharedAdmissionPoliciesSQL must follow validateSharedAuthorizationSQL
// in the SAME bounded READ COMMITTED transaction, with governance acquired before
// installation/mode/owner locks. It retains current policy locks, not a dispatch
// permit. clientIP is a separately latched live request fact and is never stored.
func validateSharedAdmissionPoliciesSQL(tx *gorm.DB, authorization sharedCapturedAuthorization, original []eventqueue.QuotaLimit, calendarRevision, timeZone string, clientIP netip.Addr, now time.Time) error {
	if tx == nil || tx.Statement == nil || tx.Error != nil || !sharedAuthorizationValid(authorization) || now.IsZero() || now.Before(authorization.EvaluatedAt) ||
		!clientIP.IsValid() || clientIP.Zone() != "" || clientIP != clientIP.Unmap() || len(original) == 0 || len(original) > 2 ||
		calendarRevision == "" || len(calendarRevision) > 128 || timeZone == "" || timeZone == "Local" || len(timeZone) > 100 {
		return errSharedAdmissionInvalid
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return errSharedAdmissionInvalid
	}
	if err := tx.Statement.Context.Err(); err != nil {
		return err
	}
	if _, err := time.LoadLocation(timeZone); err != nil {
		return errSharedAdmissionInvalid
	}
	scopes, err := sharedAdmissionPolicyScopesSQL(tx, authorization, now)
	if err != nil {
		return err
	}
	if len(scopes) != len(original) {
		return errSharedAdmissionConflict
	}
	var calendar entity.QuotaSetting
	if err := sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&calendar, 1).Error; err != nil {
		return sharedAuthorizationSQLError(err)
	}
	if calendar.ID != 1 || calendar.ETag != calendarRevision || calendar.TimeZone != timeZone {
		return errSharedAdmissionConflict
	}
	denomination, err := lockPricing(sharedAuthorizationDB(tx))
	if err != nil {
		return sharedAuthorizationSQLError(err)
	}
	if denomination.ID != 1 || !pricing.Currency(denomination.PlatformCurrency) {
		return errSharedAdmissionConflict
	}
	return sharedAdmissionComparePoliciesSQL(tx, scopes, original, denomination.PlatformCurrency, clientIP)
}

type sharedAdmissionPolicyScope struct {
	kind, id     string
	birth        time.Time
	personalRoot *entity.APIKey
	projectRoot  *entity.ProjectKey
	project      *entity.Project
}

func sharedAdmissionPolicyScopesSQL(tx *gorm.DB, a sharedCapturedAuthorization, now time.Time) ([]sharedAdmissionPolicyScope, error) {
	switch a.Kind {
	case "personal_session", "personal_key":
		var user entity.User
		if err := sharedAuthorizationSQLRow(tx, "id", a.User.ID, &user); err != nil {
			return nil, err
		}
		if user.ID != a.User.ID || !user.CreatedAt.Equal(a.User.CreatedAt) {
			return nil, errSharedAdmissionConflict
		}
		parent := sharedAdmissionPolicyScope{kind: "user", id: user.ID, birth: user.CreatedAt}
		if a.Kind == "personal_session" {
			return []sharedAdmissionPolicyScope{parent}, nil
		}
		root, err := sharedAdmissionPersonalRootSQL(tx, a, now)
		return []sharedAdmissionPolicyScope{parent, root}, err
	case "project_key":
		var project entity.Project
		if err := sharedAuthorizationSQLRow(tx, "id", a.Project.ID, &project); err != nil {
			return nil, err
		}
		if project.ID != a.Project.ID || !project.CreatedAt.Equal(a.Project.CreatedAt) {
			return nil, errSharedAdmissionConflict
		}
		root, err := sharedAdmissionProjectRootSQL(tx, a, project, now)
		return []sharedAdmissionPolicyScope{{kind: "project", id: project.ID, birth: project.CreatedAt}, root}, err
	case "team_session":
		var team entity.Team
		if err := sharedAuthorizationSQLRow(tx, "id", a.Team.ID, &team); err != nil {
			return nil, err
		}
		if team.ID != a.Team.ID || !team.CreatedAt.Equal(a.Team.CreatedAt) {
			return nil, errSharedAdmissionConflict
		}
		// Current membership was checked by the preceding authorization loader.
		// The stable pair uses the original Team birth, not the membership birth.
		return []sharedAdmissionPolicyScope{
			{kind: "team", id: team.ID, birth: team.CreatedAt},
			{kind: "team_member", id: teamMemberLimitScopeID(team.ID, a.User.ID), birth: team.CreatedAt},
		}, nil
	default:
		return nil, errSharedAdmissionInvalid
	}
}

func sharedAdmissionPersonalRootSQL(tx *gorm.DB, a sharedCapturedAuthorization, now time.Time) (sharedAdmissionPolicyScope, error) {
	var keys []entity.APIKey
	if err := personalKeyWarningChainQuery(sharedAuthorizationDB(tx), a.User.ID).Clauses(clause.Locking{Strength: "UPDATE"}).Find(&keys).Error; err != nil {
		return sharedAdmissionPolicyScope{}, err
	}
	roots, complete := personalKeyWarningRoots(keys, a.User.ID)
	if !complete || roots[a.Key.ID] == "" {
		return sharedAdmissionPolicyScope{}, errSharedAdmissionConflict
	}
	var current, root entity.APIKey
	ids := make([]string, 0, len(keys))
	for _, key := range keys {
		if key.CreatedAt.Before(a.User.CreatedAt) || key.CreatedAt.After(now) {
			return sharedAdmissionPolicyScope{}, errSharedAdmissionConflict
		}
		ids = append(ids, key.ID)
		if key.ID == a.Key.ID {
			current = key
		}
		if key.ID == roots[a.Key.ID] {
			root = key
		}
	}
	if current.ID != a.Key.ID || !current.CreatedAt.Equal(a.Key.CreatedAt) || current.LifecycleRevision != a.Key.LifecycleRevision || !sameOptionalTime(current.ExpiresAt, a.Key.ExpiresAt) || current.Status != entity.KeyActive || current.ExpiresAt != nil && !now.Before(*current.ExpiresAt) || root.ID == "" || root.ReplacesKeyID != nil {
		return sharedAdmissionPolicyScope{}, errSharedAdmissionConflict
	}
	var collisions []entity.ProjectKey
	if err := sharedAuthorizationDB(tx).Select("id").Where("id IN ?", ids).Limit(personalKeyWarningChainLimit + 1).Find(&collisions).Error; err != nil {
		return sharedAdmissionPolicyScope{}, err
	}
	if len(collisions) != 0 {
		return sharedAdmissionPolicyScope{}, errSharedAdmissionConflict
	}
	return sharedAdmissionPolicyScope{kind: "key", id: root.ID, birth: root.CreatedAt, personalRoot: &root}, nil
}

func sharedAdmissionProjectRootSQL(tx *gorm.DB, a sharedCapturedAuthorization, project entity.Project, now time.Time) (sharedAdmissionPolicyScope, error) {
	var keys []entity.ProjectKey
	if err := projectKeyWarningChainQuery(sharedAuthorizationDB(tx), project.ID).Clauses(clause.Locking{Strength: "UPDATE"}).Find(&keys).Error; err != nil {
		return sharedAdmissionPolicyScope{}, err
	}
	roots, complete := projectKeyWarningRoots(keys, project)
	if !complete || roots[a.Key.ID] == "" {
		return sharedAdmissionPolicyScope{}, errSharedAdmissionConflict
	}
	var current, root entity.ProjectKey
	ids := make([]string, 0, len(keys))
	for _, key := range keys {
		if key.CreatedAt.After(now) {
			return sharedAdmissionPolicyScope{}, errSharedAdmissionConflict
		}
		ids = append(ids, key.ID)
		if key.ID == a.Key.ID {
			current = key
		}
		if key.ID == roots[a.Key.ID] {
			root = key
		}
	}
	if current.ID != a.Key.ID || !current.CreatedAt.Equal(a.Key.CreatedAt) || !sameOptionalTime(current.ExpiresAt, a.Key.ExpiresAt) || current.Status != entity.KeyActive || current.ExpiresAt != nil && !now.Before(*current.ExpiresAt) || root.ID == "" || root.ReplacesKeyID != nil {
		return sharedAdmissionPolicyScope{}, errSharedAdmissionConflict
	}
	var collisions []entity.APIKey
	if err := sharedAuthorizationDB(tx).Select("id").Where("id IN ?", ids).Limit(projectKeyWarningChainLimit + 1).Find(&collisions).Error; err != nil {
		return sharedAdmissionPolicyScope{}, err
	}
	if len(collisions) != 0 {
		return sharedAdmissionPolicyScope{}, errSharedAdmissionConflict
	}
	return sharedAdmissionPolicyScope{kind: "key", id: root.ID, birth: root.CreatedAt, projectRoot: &root, project: &project}, nil
}

func sharedAdmissionReadPolicySQL(tx *gorm.DB, scope sharedAdmissionPolicyScope) (entity.ResourceLimit, limits.Policy, error) {
	q := sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"})
	switch scope.kind {
	case "user":
		return readDefaultResourceLimitPolicy(q, scope.kind, scope.id)
	case "project":
		return readProjectQuotaPolicy(q, scope.id, true)
	case "team", "team_member":
		return readTeamLimitPolicy(q, scope.kind, scope.id)
	case "key":
		// The bounded owner graph above proves this immutable root. No supplied
		// GatewayResult or logical-call account is used to establish ownership.
		row := entity.ResourceLimit{ScopeKind: "key", ScopeID: scope.id, ETag: "0"}
		err := q.Where(database.ExactText(q, clause.Column{Name: "scope_kind"}, "key")).Where(database.ExactText(q, clause.Column{Name: "scope_id"}, scope.id)).Take(&row).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return row, limits.Policy{}, err
		}
		if row.ScopeKind != "key" || row.ScopeID != scope.id {
			return row, limits.Policy{}, errSharedAdmissionConflict
		}
		if scope.personalRoot != nil {
			policy, err := personalKeyPolicyFromRow(row, *scope.personalRoot, scope.personalRoot.UserID)
			return row, policy, err
		}
		if scope.projectRoot != nil && scope.project != nil {
			policy, err := projectKeyPolicyFromRow(row, *scope.projectRoot, *scope.project)
			return row, policy, err
		}
	}
	return entity.ResourceLimit{}, limits.Policy{}, errSharedAdmissionInvalid
}

func sharedAdmissionComparePoliciesSQL(tx *gorm.DB, scopes []sharedAdmissionPolicyScope, original []eventqueue.QuotaLimit, currency string, ip netip.Addr) error {
	captured := make(map[string]eventqueue.QuotaLimit, len(original))
	for _, policy := range original {
		if _, duplicate := captured[policy.Account]; duplicate {
			return errSharedAdmissionConflict
		}
		captured[policy.Account] = policy
	}
	for _, scope := range scopes {
		row, policy, err := sharedAdmissionReadPolicySQL(tx, scope)
		if err != nil {
			return sharedAuthorizationSQLError(err)
		}
		account := limitAccount(scope.kind, scope.id)
		prior, exists := captured[account]
		current := eventqueue.QuotaLimit{Limit: eventqueue.Limit{Account: account, RPM: policy.RPM, Concurrency: policy.Concurrency},
			Revision: row.ETag, CreatedAt: scope.birth.UTC(), PersonalKey: scope.personalRoot != nil, ProjectKey: scope.projectRoot != nil, TeamMember: scope.kind == "team_member",
			Tokens5H: policy.Tokens5H, Tokens7D: policy.Tokens7D, TokensMonth: policy.TokensMonth, TPM: policy.TPM,
			MoneyMonth: policy.MoneyMonth, Currency: policy.Currency, TokensMonthBehavior: policy.TokensMonthBehavior, MoneyMonthBehavior: policy.MoneyMonthBehavior}
		if !exists || !prior.CreatedAt.Equal(current.CreatedAt) || row.ETag == "" || policy.MoneyMonth != nil && policy.Currency != currency {
			return errSharedAdmissionConflict
		}
		// Compare birth instants rather than time.Location or monotonic metadata;
		// every other captured field must match the canonical SQL projection.
		prior.CreatedAt = current.CreatedAt
		if !reflect.DeepEqual(prior, current) {
			return errSharedAdmissionConflict
		}
		if !limits.Allows(policy, ip) {
			return gatewayError(403, "ip_not_allowed", "The request source is not permitted.")
		}
	}
	return tx.Statement.Context.Err()
}
