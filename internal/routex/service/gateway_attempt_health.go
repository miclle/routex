package service

import (
	"sync"
	"time"
)

const (
	gatewayConnectionCooldown = 15 * time.Second
	gatewayCredentialCooldown = 30 * time.Second
	maxGatewayHealthEntries   = 1024
)

type gatewayAttemptHealth struct {
	mu          sync.Mutex
	connections map[string]time.Time
	credentials map[string]time.Time
}

func (s *Service) gatewayAttemptClock() time.Time {
	if s.attemptNow != nil {
		return s.attemptNow().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) gatewayAttemptHealthy(connectionID, credentialID string) bool {
	now := s.gatewayAttemptClock()
	health := &s.attemptHealth
	health.mu.Lock()
	defer health.mu.Unlock()
	pruneGatewayAttemptHealth(health, now)
	_, connectionCooling := health.connections[connectionID]
	_, credentialCooling := health.credentials[credentialID]
	return !connectionCooling && !credentialCooling
}

func (s *Service) markGatewayConnectionFailure(connectionID string) {
	if connectionID == "" {
		return
	}
	now := s.gatewayAttemptClock()
	until := now.Add(gatewayConnectionCooldown)
	health := &s.attemptHealth
	health.mu.Lock()
	defer health.mu.Unlock()
	if health.connections == nil {
		health.connections = map[string]time.Time{}
	}
	pruneGatewayAttemptHealth(health, now)
	boundGatewayAttemptHealth(health.connections)
	health.connections[connectionID] = until
}

func (s *Service) markGatewayCredentialRejected(credentialID string) {
	if credentialID == "" {
		return
	}
	now := s.gatewayAttemptClock()
	until := now.Add(gatewayCredentialCooldown)
	health := &s.attemptHealth
	health.mu.Lock()
	defer health.mu.Unlock()
	if health.credentials == nil {
		health.credentials = map[string]time.Time{}
	}
	pruneGatewayAttemptHealth(health, now)
	boundGatewayAttemptHealth(health.credentials)
	health.credentials[credentialID] = until
}

func boundGatewayAttemptHealth(entries map[string]time.Time) {
	if len(entries) < maxGatewayHealthEntries {
		return
	}
	oldestID := ""
	var oldest time.Time
	for id, until := range entries {
		if oldestID == "" || until.Before(oldest) || until.Equal(oldest) && id < oldestID {
			oldestID, oldest = id, until
		}
	}
	delete(entries, oldestID)
}

func (s *Service) markGatewayAttemptSuccess(connectionID, credentialID string) {
	now := s.gatewayAttemptClock()
	health := &s.attemptHealth
	health.mu.Lock()
	defer health.mu.Unlock()
	delete(health.connections, connectionID)
	delete(health.credentials, credentialID)
	pruneGatewayAttemptHealth(health, now)
}

func pruneGatewayAttemptHealth(health *gatewayAttemptHealth, now time.Time) {
	for id, until := range health.connections {
		if !now.Before(until) {
			delete(health.connections, id)
		}
	}
	for id, until := range health.credentials {
		if !now.Before(until) {
			delete(health.credentials, id)
		}
	}
}
