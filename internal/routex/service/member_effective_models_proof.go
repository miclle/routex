package service

import (
	"maps"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func (s *Service) memberEffectivePublication(auth *runtimeAuthorization, routes *runtimeRoutes) bool {
	return s.runtime != nil && auth != nil && routes != nil && s.runtime.auth.Load() == auth && s.runtime.routes.Load() == routes && time.Now().Before(auth.ValidUntil) && auth.SourceDigest != "" && auth.SourceDigest == routes.Digest
}
func (s *Service) memberEffectivePersonalProof(data *memberModelsData, auth *runtimeAuthorization, routes *runtimeRoutes) bool {
	if !s.memberEffectivePublication(auth, routes) {
		return false
	}
	state, applied := s.memberModelsApplication(data.Subject, data.Grants, auth, data.Applications)
	return state == "applied" && applied != nil && *applied
}
func (s *Service) memberEffectiveTeamProof(data *memberEffectiveModelsData, current memberEffectiveTeam, auth *runtimeAuthorization, routes *runtimeRoutes) bool {
	subject := data.Metadata.Subject
	if !data.TeamsRead || !s.memberEffectivePublication(auth, routes) || subject.Disabled || subject.OffboardedAt != nil || current.Status != entity.ResourceActive || current.MembershipStatus != entity.ResourceActive || current.MembershipUserID != subject.ID || current.MembershipTeamID != current.ID || current.MembershipRole != entity.TeamOwner && current.MembershipRole != entity.TeamMember {
		return false
	}
	user, exists := auth.UserProofs[subject.ID]
	if !exists || !s.registrationAdvisoryPublished(auth, subject, data.Metadata.Applications) || !user.Enabled || subject.CreatedAt.IsZero() || !user.CreatedAt.Equal(subject.CreatedAt) || runtimeDenied(&s.runtime.deniedUsers, subject.ID) || runtimeDenied(&s.runtime.deniedTeams, current.ID) || runtimeDenied(&s.runtime.deniedTeamMembers, teamMemberRuntimeKey(current.ID, subject.ID)) {
		return false
	}
	team, exists := auth.Teams[current.ID]
	if !exists || current.CreatedAt.IsZero() || !team.CreatedAt.Equal(current.CreatedAt) || current.MembershipID == "" || team.Members[subject.ID] != current.MembershipID {
		return false
	}
	active := map[string]bool{}
	for _, m := range data.Metadata.Models {
		if m.Status == entity.ResourceActive {
			active[m.ID] = true
		}
	}
	models := map[string]bool{}
	sources := map[string]string{}
	for _, g := range data.TeamGrants {
		if g.TeamID == current.ID && active[g.ModelID] {
			models[g.ModelID] = true
			if g.SourceRequestID != nil {
				sources[g.ModelID] = *g.SourceRequestID
			}
		}
	}
	// The complete filtered set and nullable provenance must match, not just the
	// displayed row. Published Team maps intentionally omit inactive Models.
	return maps.Equal(models, team.Models) && maps.Equal(sources, auth.TeamGrantSources[current.ID])
}
