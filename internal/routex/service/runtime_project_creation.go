package service

import (
	"reflect"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

type runtimeProjectCreationState struct {
	Project         entity.Project
	Managers        map[string]string
	EnabledManagers map[string]bool
	Models          []string
	Eligible        bool
}

// Publish only the current configuration. Historical creation receipts are never
// projected into desired state and cannot restore a manager, grant or policy.
func addProjectCreationAuthorization(auth *runtimeAuthorization, data *projectRuntimeData, users map[string]bool) {
	auth.ProjectCreationStates = map[string]runtimeProjectCreationState{}
	if data == nil {
		return
	}
	for _, project := range data.Projects {
		auth.ProjectCreationStates[project.ID] = runtimeProjectCreationState{Project: project, Managers: map[string]string{}, EnabledManagers: map[string]bool{}, Models: []string{}}
	}
	for _, manager := range data.Managers {
		state, exists := auth.ProjectCreationStates[manager.ProjectID]
		if exists {
			state.Managers[manager.UserID] = manager.ID
			state.EnabledManagers[manager.UserID] = users[manager.UserID]
			state.Eligible = state.Eligible || users[manager.UserID]
			auth.ProjectCreationStates[manager.ProjectID] = state
		}
	}
	for _, grant := range data.Grants {
		state, exists := auth.ProjectCreationStates[grant.ProjectID]
		if exists {
			state.Models = append(state.Models, grant.ModelID)
			auth.ProjectCreationStates[grant.ProjectID] = state
		}
	}
	for key, state := range auth.ProjectCreationStates {
		slices.Sort(state.Models)
		auth.ProjectCreationStates[key] = state
	}
}

func (s *Service) projectCreationApplication(receipt entity.ProjectCreationReceipt, current *ResourceRecord) (bool, string) {
	if current == nil || current.ID != receipt.ProjectID {
		return false, "unavailable"
	}
	snapshot, err := readProjectCreationSnapshot(receipt)
	if err != nil {
		return false, "unavailable"
	}
	if current.Status != entity.ResourceActive {
		return false, "superseded"
	}
	if s.runtime == nil {
		return false, "pending"
	}
	auth := s.runtime.auth.Load()
	account := limitAccount("project", receipt.ProjectID)
	if auth == nil || !time.Now().Before(auth.ValidUntil) || runtimeDenied(&s.runtime.deniedProjects, receipt.ProjectID) || runtimeDenied(&s.runtime.deniedLimits, account) || runtimeDenied(&s.runtime.deniedLimits, "quota_settings") {
		return false, "pending"
	}
	state, exists := auth.ProjectCreationStates[receipt.ProjectID]
	if !exists || !state.Eligible || state.Project.Status != entity.ResourceActive || state.Project.CreatorID != snapshot.CreatorID || len(state.Managers) != len(snapshot.Managers) || !slices.Equal(state.Models, snapshot.ModelIDs) {
		return false, "pending"
	}
	for _, manager := range snapshot.Managers {
		if state.Managers[manager.UserID] != manager.ID || !state.EnabledManagers[manager.UserID] || runtimeDenied(&s.runtime.deniedUsers, manager.UserID) || runtimeDenied(&s.runtime.deniedSessionUsers, manager.UserID) {
			return false, "pending"
		}
	}
	for _, modelID := range snapshot.ModelIDs {
		if !auth.Models[modelID] {
			return false, "pending"
		}
	}
	expected, err := limits.Normalize(snapshot.Policy)
	if err != nil {
		return false, "pending"
	}
	published, err := limits.Normalize(auth.LimitPolicies[account])
	if err != nil || !reflect.DeepEqual(expected, published) {
		return false, "pending"
	}
	revision := "0"
	if auth.Quota != nil && auth.Quota.Revisions[account] != "" {
		revision = auth.Quota.Revisions[account]
	}
	if revision != snapshot.PolicyETag {
		return false, "pending"
	}
	if expected.MoneyMonth != nil && (auth.Quota == nil || auth.Quota.Currency != expected.Currency) {
		return false, "pending"
	}
	return true, "applied"
}
