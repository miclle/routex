package service

import "time"

type systemProcessIdentity struct {
	ID        string
	StartedAt time.Time
}

// currentSystemProcessIdentity copies one coherent local process identity. It
// does not prove current SQL owner authority or executor/transport closure.
func (s *Service) currentSystemProcessIdentity() (systemProcessIdentity, bool) {
	s.instanceMu.RLock()
	defer s.instanceMu.RUnlock()
	if s.instance == nil {
		return systemProcessIdentity{}, false
	}
	birth := s.instance.startedAt.UTC()
	if !systemInstanceIDPattern.MatchString(s.instance.id) || !connectionMetadataBirth(birth) || birth.Nanosecond()%1000 != 0 {
		return systemProcessIdentity{}, false
	}
	return systemProcessIdentity{ID: s.instance.id, StartedAt: birth}, true
}
