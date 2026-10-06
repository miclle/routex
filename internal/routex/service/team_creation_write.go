package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/limits"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const teamCreationSnapshotBytes = 128 * 1024

var errTeamCreationConfigurationOverflow = &apperrors.Error{Code: http.StatusUnprocessableEntity, Message: "Team creation reconciliation exceeds supported bounds"}

type TeamCreationReceiptRecord struct {
	CreationID string    `json:"creation_id"`
	TeamID     string    `json:"team_id"`
	CreatedAt  time.Time `json:"created_at"`
}
type TeamCreationResult struct {
	Team              *ResourceRecord           `json:"team"`
	Receipt           TeamCreationReceiptRecord `json:"receipt"`
	Committed         bool                      `json:"committed"`
	RuntimeApplied    bool                      `json:"runtime_applied"`
	ApplicationStatus string                    `json:"application_status"`
	Created           bool                      `json:"-"`
}

// Compact triples preserve the exact persisted User birth independently of
// membership identity. Every original membership shares the separately recorded
// JoinedAt, set once during creation and checked independently on reconciliation.
type TeamCreationOwnerSnapshot struct {
	UserID             string
	MembershipID       string
	UserCreatedAtMicro int64
}

func (owner TeamCreationOwnerSnapshot) MarshalJSON() ([]byte, error) {
	return json.Marshal([3]any{owner.UserID, owner.MembershipID, owner.UserCreatedAtMicro})
}
func (owner *TeamCreationOwnerSnapshot) UnmarshalJSON(raw []byte) error {
	var fields []json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 3 || json.Unmarshal(fields[0], &owner.UserID) != nil || json.Unmarshal(fields[1], &owner.MembershipID) != nil || json.Unmarshal(fields[2], &owner.UserCreatedAtMicro) != nil {
		return apperrors.ErrInternal
	}
	return nil
}

type TeamCreationSnapshot struct {
	Name            string                      `json:"name"`
	Description     string                      `json:"description"`
	Owners          []TeamCreationOwnerSnapshot `json:"owners"`
	OwnerJoinedAt   time.Time                   `json:"owner_joined_at"`
	DefaultRuleETag string                      `json:"default_rule_etag"`
	SubmittedFields []string                    `json:"submitted_fields"`
	Reason          string                      `json:"reason"`
	PolicyETag      string                      `json:"policy_etag"`
	Policy          limits.Policy               `json:"policy"`
}

func readTeamCreationSnapshot(receipt entity.TeamCreationReceipt) (TeamCreationSnapshot, error) {
	var snapshot TeamCreationSnapshot
	if len(receipt.SnapshotJSON) > teamCreationSnapshotBytes || !utf8Snapshot(receipt.SnapshotJSON) || !credentialReplacementRequestID.MatchString(receipt.CreationID) || !safeTeamSessionID(receipt.ActorID) || !strings.HasPrefix(receipt.ActorID, "usr_") || !safeTeamSessionID(receipt.TeamID) || !strings.HasPrefix(receipt.TeamID, "tea_") || !teamSessionDigest.MatchString(receipt.RequestHash) || !teamSessionDigest.MatchString(receipt.ReviewETag) || receipt.ActorCreatedAt.IsZero() || receipt.TeamCreatedAt.IsZero() || receipt.CreatedAt.IsZero() {
		return snapshot, apperrors.ErrInternal
	}
	decoder := json.NewDecoder(strings.NewReader(receipt.SnapshotJSON))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&snapshot) != nil {
		return snapshot, apperrors.ErrInternal
	}
	if _, err := decoder.Token(); err != io.EOF {
		return snapshot, apperrors.ErrInternal
	}
	if !validCatalogLabel(snapshot.Name) || !validResourceDescription(snapshot.Description) || len(snapshot.Owners) < 1 || len(snapshot.Owners) > 1000 || snapshot.OwnerJoinedAt.IsZero() || !teamSessionDigest.MatchString(snapshot.DefaultRuleETag) || !safeTeamSessionID(snapshot.PolicyETag) || !strings.HasPrefix(snapshot.PolicyETag, "lim_") || !slices.IsSorted(snapshot.SubmittedFields) {
		return snapshot, apperrors.ErrInternal
	}
	if len(snapshot.SubmittedFields) == 0 {
		if snapshot.Reason != "" {
			return snapshot, apperrors.ErrInternal
		}
	} else if len(snapshot.Reason) > 2000 || strings.TrimSpace(snapshot.Reason) == "" || !validResourceDescription(snapshot.Reason) || strings.ContainsFunc(snapshot.Reason, unicode.IsControl) {
		return snapshot, apperrors.ErrInternal
	}
	for i, field := range snapshot.SubmittedFields {
		if !slices.Contains(teamLimitFields, field) || i > 0 && field == snapshot.SubmittedFields[i-1] {
			return snapshot, apperrors.ErrInternal
		}
	}
	seen := map[string]bool{}
	relationships := map[string]bool{}
	for i, owner := range snapshot.Owners {
		if !strings.HasPrefix(owner.UserID, "usr_") || !safeTeamSessionID(owner.UserID) || !strings.HasPrefix(owner.MembershipID, "tmm_") || !safeTeamSessionID(owner.MembershipID) || owner.UserCreatedAtMicro == 0 || seen[owner.UserID] || relationships[owner.MembershipID] || i > 0 && snapshot.Owners[i-1].UserID >= owner.UserID {
			return snapshot, apperrors.ErrInternal
		}
		seen[owner.UserID] = true
		relationships[owner.MembershipID] = true
	}
	policy, err := limits.Normalize(snapshot.Policy)
	if err != nil || validateTeamLimitPolicy("team", policy) != nil || !reflect.DeepEqual(policy, snapshot.Policy) {
		return snapshot, apperrors.ErrInternal
	}
	return snapshot, nil
}

func utf8Snapshot(value string) bool { return utf8.ValidString(value) && json.Valid([]byte(value)) }

// An indexed collation superset is narrowed by parameterized exact predicates;
// complete result checks remain mandatory even under faulty/aliasing drivers.
func teamCreationOwnerQuery(tx *gorm.DB, ids []string, lock bool) *gorm.DB {
	expressions := make([]clause.Expression, 0, len(ids))
	for _, value := range ids {
		expressions = append(expressions, database.ExactText(tx, clause.Column{Name: "id"}, value))
	}
	q := tx.Session(&gorm.Session{NewDB: true}).Model(&entity.User{})
	if len(ids) == 0 {
		return q.Where("1 = 0")
	}
	q = q.Where("id IN ?", ids).Where(clause.Or(expressions...)).Order("id").Limit(len(ids) + 1)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return q
}

func teamCreationOwners(tx *gorm.DB, ids []string, lock bool) ([]entity.User, error) {
	if len(ids) < 1 || len(ids) > 1000 {
		return nil, apperrors.ErrBadRequest
	}
	result := make([]entity.User, 0, len(ids))
	for start := 0; start < len(ids); start += 500 {
		selected := ids[start:min(start+500, len(ids))]
		var rows []entity.User
		if err := teamCreationOwnerQuery(tx, selected, lock).Find(&rows).Error; err != nil {
			return nil, err
		}
		if len(rows) != len(selected) {
			return nil, apperrors.ErrBadRequest
		}
		seen := map[string]bool{}
		for _, user := range rows {
			if !slices.Contains(selected, user.ID) || seen[user.ID] || user.CreatedAt.IsZero() || !time.UnixMicro(user.CreatedAt.UnixMicro()).Equal(user.CreatedAt) {
				return nil, apperrors.ErrBadRequest
			}
			seen[user.ID] = true
		}
		result = append(result, rows...)
	}
	apps, err := loadRegistrationApplications(tx, result)
	if err != nil {
		return nil, err
	}
	for _, user := range result {
		summary, _ := registrationAdmission(user, apps)
		if !summary.AdmissionEligible {
			return nil, apperrors.ErrBadRequest
		}
	}
	slices.SortFunc(result, func(a, b entity.User) int { return strings.Compare(a.ID, b.ID) })
	return result, nil
}

func teamCreationPermissions(tx *gorm.DB, actor entity.User, fields []string) error {
	permissions := []string{"teams.write"}
	for _, field := range fields {
		permission := teamLimitPermission(field)
		if !slices.Contains(permissions, permission) {
			permissions = append(permissions, permission)
		}
	}
	for _, permission := range permissions {
		allowed, err := exactGovernancePermissionForAdmittedActor(tx, actor, permission)
		if err != nil {
			return err
		}
		if !allowed {
			return apperrors.ErrForbidden
		}
	}
	return nil
}

func teamCreationCurrent(tx *gorm.DB, actorID, teamID string) (*ResourceRecord, error) {
	if _, err := teamRoleTargetAccess(tx, actorID, teamID, false); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, apperrors.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	result := &ResourceRecord{Members: []ResourcePerson{}, ModelIDs: []string{}}
	if err := personalExact(tx.Table("teams"), "id", teamID).Take(result).Error; err != nil {
		return nil, err
	}
	if result.ID != teamID {
		return nil, apperrors.ErrInternal
	}
	var memberships []entity.TeamMembership
	if err := personalExact(tx, "team_id", teamID).Order("id").Limit(1001).Find(&memberships).Error; err != nil {
		return nil, err
	}
	if len(memberships) > 1000 {
		return nil, errTeamCreationConfigurationOverflow
	}
	// Read only this authorized Team's retained profiles, preserving exact join
	// identity. A missing profile remains unknown instead of borrowing an alias.
	if err := tx.Table("team_memberships AS m").Select("m.id,m.user_id,u.name,u.email,m.role,m.status").
		Joins("LEFT JOIN users AS u ON ?", database.ExactTextColumns(tx, clause.Column{Table: "u", Name: "id"}, clause.Column{Table: "m", Name: "user_id"})).
		Where(database.ExactText(tx, clause.Column{Table: "m", Name: "team_id"}, teamID)).Order("m.id").Limit(1001).Scan(&result.Members).Error; err != nil {
		return nil, err
	}
	if len(result.Members) != len(memberships) {
		return nil, apperrors.ErrInternal
	}
	people := map[string]ResourcePerson{}
	for _, person := range result.Members {
		if people[person.ID].ID != "" {
			return nil, apperrors.ErrInternal
		}
		people[person.ID] = person
	}
	for _, member := range memberships {
		person := people[member.ID]
		if member.TeamID != teamID || person.ID != member.ID || person.UserID != member.UserID || person.Role != member.Role || person.Status != member.Status {
			return nil, apperrors.ErrInternal
		}
	}
	slices.SortFunc(result.Members, func(a, b ResourcePerson) int { return strings.Compare(a.ID, b.ID) })
	if err := personalExact(tx.Model(&entity.TeamModelGrant{}), "team_id", teamID).Order("model_id").Limit(1001).Pluck("model_id", &result.ModelIDs).Error; err != nil {
		return nil, err
	}
	if len(result.ModelIDs) > 1000 {
		return nil, errTeamCreationConfigurationOverflow
	}
	slices.Sort(result.ModelIDs)
	return result, nil
}

func teamCreationConfigurationMatches(tx *gorm.DB, receipt entity.TeamCreationReceipt, current *ResourceRecord) (bool, error) {
	snapshot, err := readTeamCreationSnapshot(receipt)
	if err != nil {
		return false, err
	}
	if current == nil || current.ID != receipt.TeamID || !current.CreatedAt.Equal(receipt.TeamCreatedAt) || current.Status != entity.ResourceActive || current.Name != snapshot.Name || current.Description != snapshot.Description || len(current.Members) != len(snapshot.Owners) {
		return false, nil
	}
	modelsMatch, err := teamCreationCurrentModelsMatch(tx, receipt, current.ModelIDs)
	if err != nil || !modelsMatch {
		return false, err
	}
	var members []entity.TeamMembership
	if err := personalExact(tx, "team_id", receipt.TeamID).Limit(1001).Find(&members).Error; err != nil {
		return false, err
	}
	if len(members) != len(snapshot.Owners) {
		return false, nil
	}
	byUser := map[string]entity.TeamMembership{}
	for _, member := range members {
		if member.TeamID != receipt.TeamID || byUser[member.UserID].ID != "" {
			return false, nil
		}
		byUser[member.UserID] = member
	}
	users := make([]string, len(snapshot.Owners))
	for i, owner := range snapshot.Owners {
		member := byUser[owner.UserID]
		if member.ID != owner.MembershipID || member.Role != entity.TeamOwner || member.Status != entity.ResourceActive || member.JoinedAt == nil || !member.JoinedAt.Equal(snapshot.OwnerJoinedAt) {
			return false, nil
		}
		users[i] = owner.UserID
	}
	owners, err := teamCreationOwners(tx, users, false)
	if errors.Is(err, apperrors.ErrBadRequest) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for i, owner := range owners {
		if owner.ID != snapshot.Owners[i].UserID || owner.CreatedAt.UnixMicro() != snapshot.Owners[i].UserCreatedAtMicro {
			return false, nil
		}
	}
	row, policy, err := readTeamLimitPolicy(tx, "team", receipt.TeamID)
	if err != nil {
		return false, err
	}
	if row.ETag != snapshot.PolicyETag || !reflect.DeepEqual(policy, snapshot.Policy) {
		return false, nil
	}
	if len(snapshot.SubmittedFields) == 0 {
		if row.AppliedDefaultETag == nil || *row.AppliedDefaultETag != snapshot.DefaultRuleETag || row.DefaultResetETag != nil {
			return false, nil
		}
	} else if row.AppliedDefaultETag != nil || row.DefaultResetETag != nil {
		return false, nil
	}
	if policy.MoneyMonth != nil {
		var pricing entity.PricingSetting
		if err := tx.First(&pricing, 1).Error; err != nil {
			return false, err
		}
		if pricing.PlatformCurrency != policy.Currency {
			return false, nil
		}
	}
	return true, nil
}

func teamCreationReceiptMatches(receipt entity.TeamCreationReceipt, actor entity.User, input TeamCreationInput) bool {
	return receipt.CreationID == input.CreationID && receipt.ActorID == actor.ID && receipt.ActorCreatedAt.Equal(actor.CreatedAt) && receipt.RequestHash == teamCreationHash(actor.ID, input) && receipt.ReviewETag == input.ReviewETag
}

func (s *Service) CreateTeamWithInitialLimits(ctx context.Context, actorID string, input TeamCreationInput) (*TeamCreationResult, error) {
	input, err := normalizeTeamCreationInput(input)
	if err != nil {
		return nil, err
	}
	var receipt entity.TeamCreationReceipt
	created := false
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		fields := []string{}
		if input.InitialLimits != nil {
			for field := range input.InitialLimits.Fields {
				if field != "currency" {
					fields = append(fields, field)
				}
			}
		}
		slices.Sort(fields)
		if err := teamCreationPermissions(tx, actor, fields); err != nil {
			return err
		}
		if len(input.ModelIDs) > 0 {
			if err := teamCreationModelPermissions(tx, actor); err != nil {
				return err
			}
		}
		err = personalExact(tx, "creation_id", input.CreationID).Take(&receipt).Error
		if err == nil {
			if !teamCreationReceiptMatches(receipt, actor, input) {
				return catalogConflict
			}
			_, err = readTeamCreationSnapshot(receipt)
			if err != nil {
				return err
			}
			_, err = readTeamCreationReceiptModels(tx, receipt)
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		review, err := loadTeamCreationReview(tx, actor, true)
		if err != nil {
			return err
		}
		if review.Public.ReviewETag != input.ReviewETag {
			return catalogConflict
		}
		if !teamCreationSubmittedAllowed(input.InitialLimits, review.Public.EditableFields) {
			return apperrors.ErrForbidden
		}
		owners, err := teamCreationOwners(tx, input.OwnerIDs, true)
		if err != nil {
			return err
		}
		selectedModels, err := s.validateTeamCreationSelectedModels(tx, review, input)
		if err != nil {
			return err
		}
		policy := review.Policy
		if input.InitialLimits != nil {
			policy, err = applyTeamLimitInput(policy, *input.InitialLimits, "team", review.Public.EditableFields, review.Pricing.PlatformCurrency)
			if err != nil {
				return err
			}
		}
		if policy.MoneyMonth != nil && policy.Currency != review.Pricing.PlatformCurrency {
			return catalogConflict
		}
		teamID, err := id.NewPrefixed("tea")
		if err != nil {
			return err
		}
		team := entity.Team{ID: teamID, Name: input.Name, Description: input.Description, Status: entity.ResourceActive}
		if err := tx.Create(&team).Error; err != nil {
			return err
		}
		// Reload persisted driver precision before freezing the Team incarnation.
		if err := personalExact(tx, "id", teamID).Take(&team).Error; err != nil {
			return err
		}
		if team.ID != teamID || team.CreatedAt.IsZero() {
			return apperrors.ErrInternal
		}
		joined := newMemberTeamJoinedAt(time.Now().UTC().Truncate(time.Millisecond))
		memberships := make([]entity.TeamMembership, 0, len(owners))
		snapOwners := make([]TeamCreationOwnerSnapshot, 0, len(owners))
		for _, owner := range owners {
			membershipID, err := id.NewPrefixed("tmm")
			if err != nil {
				return err
			}
			memberships = append(memberships, entity.TeamMembership{ID: membershipID, TeamID: teamID, UserID: owner.ID, Role: entity.TeamOwner, Status: entity.ResourceActive, JoinedAt: joined})
			snapOwners = append(snapOwners, TeamCreationOwnerSnapshot{owner.ID, membershipID, owner.CreatedAt.UnixMicro()})
		}
		if err := tx.CreateInBatches(&memberships, 500).Error; err != nil {
			return err
		}
		// All rows receive one timestamp; require their actual persisted value and
		// relation identity before recording that shared relationship incarnation.
		var stored []entity.TeamMembership
		if err := personalExact(tx, "team_id", teamID).Order("user_id").Limit(1001).Find(&stored).Error; err != nil {
			return err
		}
		slices.SortFunc(stored, func(a, b entity.TeamMembership) int { return strings.Compare(a.UserID, b.UserID) })
		if len(stored) != len(memberships) {
			return apperrors.ErrInternal
		}
		for i, member := range stored {
			if member.ID != memberships[i].ID || member.TeamID != teamID || member.UserID != owners[i].ID || member.JoinedAt == nil || member.Role != entity.TeamOwner || member.Status != entity.ResourceActive || !member.JoinedAt.Equal(*joined) {
				return apperrors.ErrInternal
			}
		}
		if err := applyCreationDefaultLimit(tx, "team", teamID, actor.ID); err != nil {
			return err
		}
		row, before, err := readTeamLimitPolicy(tx, "team", teamID)
		if err != nil {
			return err
		}
		reason := ""
		if input.InitialLimits != nil {
			reason = input.InitialLimits.Reason
			row, err = persistResourceLimitPolicy(tx, actor.ID, resolvedLimitTarget{kind: "team", id: teamID, teamID: teamID}, row, before, policy, reason)
			if err != nil {
				return err
			}
		} else {
			policy = before
		}
		if err := appendAudit(tx, actor.ID, "resource.create", string(TeamResource), teamID); err != nil {
			return err
		}
		snapshot := TeamCreationSnapshot{Name: team.Name, Description: team.Description, Owners: snapOwners, OwnerJoinedAt: *joined, DefaultRuleETag: review.Rule.RuleETag, SubmittedFields: fields, Reason: reason, PolicyETag: row.ETag, Policy: policy}
		raw, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		if len(raw) > teamCreationSnapshotBytes {
			return apperrors.ErrBadRequest
		}
		receipt = entity.TeamCreationReceipt{CreationID: input.CreationID, ActorID: actor.ID, ActorCreatedAt: actor.CreatedAt, TeamID: team.ID, TeamCreatedAt: team.CreatedAt, RequestHash: teamCreationHash(actor.ID, input), ReviewETag: input.ReviewETag, SnapshotJSON: string(raw)}
		if err := persistTeamCreationModels(tx, &receipt, selectedModels); err != nil {
			return err
		}
		if err := tx.Create(&receipt).Error; err != nil {
			return err
		}
		if err := personalExact(tx, "creation_id", input.CreationID).Take(&receipt).Error; err != nil {
			return err
		}
		if _, err := readTeamCreationSnapshot(receipt); err != nil {
			return err
		}
		if err := appendDefaultLimitAudit(tx, actor.ID, "team.creation.commit", "team", teamID, teamCreationCommitAudit{CreationID: receipt.CreationID, OwnerCount: len(owners), Fields: fields, Reason: reason, ModelSnapshotVersion: receipt.ModelSnapshotVersion, ModelCount: receipt.ModelCount, ModelDigest: receipt.ModelDigest}); err != nil {
			return err
		}
		created = true
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, catalogError(err)
	}
	// A failed publication never hides the already committed durable receipt.
	publicationOK := s.RefreshRuntime(ctx) == nil
	result := &TeamCreationResult{Receipt: TeamCreationReceiptRecord{receipt.CreationID, receipt.TeamID, receipt.CreatedAt.UTC()}, Committed: true, Created: created, ApplicationStatus: "pending"}
	// Recheck the current SQL boundary after publication. Runtime maps intentionally
	// contain no historical receipt state and cannot restore owners or policies.
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		snapshot, err := readTeamCreationSnapshot(receipt)
		if err != nil {
			return err
		}
		if !teamCreationReceiptMatches(receipt, actor, input) {
			return catalogConflict
		}
		if err := teamCreationPermissions(tx, actor, snapshot.SubmittedFields); err != nil {
			return err
		}
		if len(input.ModelIDs) > 0 {
			if err := teamCreationModelPermissions(tx, actor); err != nil {
				return err
			}
		}
		current, err := teamCreationCurrent(tx, actorID, receipt.TeamID)
		if err != nil {
			return err
		}
		result.Team = current
		if current == nil {
			result.ApplicationStatus = "unavailable"
			return nil
		}
		matches, err := teamCreationConfigurationMatches(tx, receipt, current)
		if err != nil {
			return err
		}
		if !matches {
			result.ApplicationStatus = "superseded"
			return nil
		}
		if publicationOK {
			children, err := readTeamCreationReceiptModels(tx, receipt)
			if err != nil {
				return err
			}
			result.RuntimeApplied, result.ApplicationStatus = s.teamCreationApplication(receipt, current, children)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, catalogError(err)
	}
	return result, nil
}
