package service

import (
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

type runtimeProviderProof struct {
	Birth    time.Time
	Enabled  bool
	Revision string
}

func runtimeProviderProofs(data *runtimeData) map[string]runtimeProviderProof {
	result := make(map[string]runtimeProviderProof, len(data.Providers))
	for _, row := range data.Providers {
		if !modelCreationID(row.ID, "prv") || !connectionMetadataBirth(row.CreatedAt) || row.ETag == "" {
			continue
		}
		result[row.ID] = runtimeProviderProof{Birth: row.CreatedAt.UTC(), Enabled: row.Enabled, Revision: row.ETag}
	}
	return result
}

// Every queued route retains the exact Provider incarnation and reviewed
// revision. A synchronous tombstone closes dispatch before refresh can succeed.
func (s *Service) runtimeProviderMatches(auth *runtimeAuthorization, route gatewayRoute) bool {
	if s.runtime == nil || auth == nil {
		return false
	}
	proof, ok := auth.Providers[route.ProviderID]
	return ok && proof.Enabled == route.ProviderEnabled && proof.Birth.Equal(route.ProviderBirth) && proof.Revision == route.ProviderRevision && !runtimeDenied(&s.runtime.deniedProviders, route.ProviderID)
}
func (s *Service) runtimeProviderAllowed(auth *runtimeAuthorization, route gatewayRoute) bool {
	return s.runtimeProviderMatches(auth, route) && route.ProviderEnabled
}

// This observes the current local configuration, including Providers with no
// Connections. It never attests historical command identity or fleet coverage.
func (s *Service) providerStatusRuntimeApplied(digest string, egressGeneration uint64, row entity.Provider) bool {
	runtime := s.runtime
	if runtime == nil || runtime.done == nil || digest == "" || !runtime.publication.TryRLock() {
		return false
	}
	defer runtime.publication.RUnlock()
	if !runtime.mu.TryLock() {
		return false
	}
	defer runtime.mu.Unlock()
	select {
	case <-runtime.done:
		return false
	default:
	}
	generation := runtime.epoch.Load()
	auth, routes := runtime.auth.Load(), runtime.routes.Load()
	if auth == nil || routes == nil || auth.publicationEpoch != generation || !time.Now().Before(auth.ValidUntil) || auth.SourceDigest != digest || routes.Digest != digest || s.egressGeneration.Load() != egressGeneration {
		return false
	}
	proof, ok := auth.Providers[row.ID]
	return ok && proof.Birth.Equal(row.CreatedAt) && proof.Enabled == row.Enabled && proof.Revision == row.ETag && !runtimeDenied(&runtime.deniedProviders, row.ID) && runtime.auth.Load() == auth && runtime.routes.Load() == routes && runtime.epoch.Load() == generation && time.Now().Before(auth.ValidUntil)
}
