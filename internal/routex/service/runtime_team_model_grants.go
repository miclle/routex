package service

// RuntimeTeamModelGrantApplied confirms shared Team grant provenance under the
// current authorization lease. It proves no applicant membership or native call.
func (s *Service) RuntimeTeamModelGrantApplied(teamID, modelID, sourceRequestID string) bool {
	if s.runtime == nil || sourceRequestID == "" {
		return false
	}
	s.runtime.publication.RLock()
	defer s.runtime.publication.RUnlock()
	auth := s.runtime.auth.Load()
	if auth == nil || !s.gatewayAttemptClock().Before(auth.ValidUntil) || runtimeDenied(&s.runtime.deniedTeams, teamID) || runtimeDenied(&s.runtime.deniedModels, modelID) {
		return false
	}
	team, exists := auth.Teams[teamID]
	return exists && team.Models[modelID] && auth.Models[modelID] && auth.TeamGrantSources[teamID][modelID] == sourceRequestID
}
