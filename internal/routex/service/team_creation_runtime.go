package service

import (
	"reflect"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

// Reconcile only leased current authorization. Receipt snapshots are never
// inserted into runtime state and never restore original memberships or limits.
func (s *Service) teamCreationApplication(receipt entity.TeamCreationReceipt, current *ResourceRecord, selections ...[]entity.TeamCreationReceiptModel) (bool, string) {
	if current == nil || current.ID != receipt.TeamID {
		return false, "unavailable"
	}
	snapshot, err := readTeamCreationSnapshot(receipt)
	if err != nil {
		return false, "unavailable"
	}
	if current.Status != entity.ResourceActive || !current.CreatedAt.Equal(receipt.TeamCreatedAt) || current.Name != snapshot.Name || current.Description != snapshot.Description {
		return false, "superseded"
	}
	if s.runtime == nil || s.recorder == nil {
		return false, "pending"
	}
	auth := s.runtime.auth.Load()
	account := limitAccount("team", receipt.TeamID)
	if auth == nil || !time.Now().Before(auth.ValidUntil) || auth.Quota == nil || runtimeDenied(&s.runtime.deniedTeams, receipt.TeamID) || runtimeDenied(&s.runtime.deniedLimits, account) || runtimeDenied(&s.runtime.deniedLimits, "quota_settings") {
		return false, "pending"
	}
	team, exists := auth.Teams[receipt.TeamID]
	if !exists || !team.CreatedAt.Equal(receipt.TeamCreatedAt) || len(team.Members) != len(snapshot.Owners) {
		return false, "pending"
	}
	var children []entity.TeamCreationReceiptModel
	if len(selections) > 0 {
		children = selections[0]
	}
	for id := range team.Models {
		if _, exists := auth.TeamCreationGrants[receipt.TeamID][id]; !exists {
			return false, "pending"
		}
	}
	if !teamCreationPublishedModelsMatch(auth, receipt, children) {
		return false, "pending"
	}
	for _, owner := range snapshot.Owners {
		proof := auth.UserAdmissions[owner.UserID]
		if team.Members[owner.UserID] != owner.MembershipID || !proof.Eligible || !proof.CreatedAt.Equal(time.UnixMicro(owner.UserCreatedAtMicro)) || runtimeDenied(&s.runtime.deniedUsers, owner.UserID) || runtimeDenied(&s.runtime.deniedSessionUsers, owner.UserID) || runtimeDenied(&s.runtime.deniedTeamMembers, teamMemberRuntimeKey(receipt.TeamID, owner.UserID)) {
			return false, "pending"
		}
	}
	actor := auth.UserAdmissions[receipt.ActorID]
	if !actor.Eligible || !actor.CreatedAt.Equal(receipt.ActorCreatedAt) || runtimeDenied(&s.runtime.deniedUsers, receipt.ActorID) || runtimeDenied(&s.runtime.deniedSessionUsers, receipt.ActorID) {
		return false, "pending"
	}
	expected, err := limits.Normalize(snapshot.Policy)
	if err != nil {
		return false, "pending"
	}
	published, err := limits.Normalize(auth.LimitPolicies[account])
	if err != nil || !reflect.DeepEqual(expected, published) || auth.Quota.Revisions[account] != snapshot.PolicyETag || expected.MoneyMonth != nil && auth.Quota.Currency != expected.Currency {
		return false, "pending"
	}
	// A concurrent publication must not replace the pointer used for this proof.
	if s.runtime.auth.Load() != auth {
		return false, "pending"
	}
	return true, "applied"
}

type runtimeTeamCreationGrant struct {
	ModelCreatedAt time.Time
	CreationID     string
	RequestID      string
}

func teamCreationPublishedModelsMatch(auth *runtimeAuthorization, receipt entity.TeamCreationReceipt, children []entity.TeamCreationReceiptModel) bool {
	stored, exists := auth.TeamCreationGrants[receipt.TeamID]
	if !exists || len(stored) != len(children) || len(children) != receipt.ModelCount {
		return false
	}
	if receipt.ModelSnapshotVersion == 0 {
		return len(children) == 0 && receipt.ModelDigest == nil
	}
	if receipt.ModelSnapshotVersion != 1 || receipt.ModelDigest == nil || teamCreationModelsDigest(children) != *receipt.ModelDigest {
		return false
	}
	for _, child := range children {
		grant, exists := stored[child.ModelID]
		if !exists || child.CreationID != receipt.CreationID || child.ModelCreatedAt.IsZero() || !grant.ModelCreatedAt.Equal(child.ModelCreatedAt) || grant.CreationID != receipt.CreationID || grant.RequestID != "" {
			return false
		}
	}
	return true
}
