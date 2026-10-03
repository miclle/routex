package service

import "time"

// RuntimePersonalModelGrantApplied confirms only the exact original grant source.
// It does not assert route readiness, quota admission or an existing Key scope.
func (s *Service) RuntimePersonalModelGrantApplied(userID, modelID, sourceRequestID string) bool {
	if s.runtime == nil || sourceRequestID == "" {
		return false
	}
	s.runtime.publication.RLock()
	defer s.runtime.publication.RUnlock()
	auth := s.runtime.auth.Load()
	return auth != nil && time.Now().Before(auth.ValidUntil) &&
		!runtimeDenied(&s.runtime.deniedUsers, userID) &&
		!runtimeDenied(&s.runtime.deniedModels, modelID) &&
		auth.Models[modelID] && auth.PersonalGrantSources[userID][modelID] == sourceRequestID
}
