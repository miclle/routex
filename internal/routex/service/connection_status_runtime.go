package service

import (
	"sync"
	"time"
)

func runtimeConnectionProofs(data *runtimeData) map[string]runtimeConnectionProof {
	result := make(map[string]runtimeConnectionProof, len(data.Connections))
	for _, row := range data.Connections {
		if connectionMetadataIDs("usr_runtime", row.ID) != nil || !connectionMetadataBirth(row.CreatedAt) {
			continue
		}
		result[row.ID] = runtimeConnectionProof{ProviderID: row.ProviderID, Birth: row.CreatedAt.UTC(), Enabled: row.Enabled}
	}
	return result
}
func (s *Service) runtimeConnectionAllowed(auth *runtimeAuthorization, route gatewayRoute) bool {
	if s.runtime == nil || auth == nil {
		return false
	}
	proof, ok := auth.Connections[route.ConnectionID]
	return ok && proof.Enabled && proof.ProviderID == route.ProviderID && proof.Birth.Equal(route.ConnectionBirth) && !runtimeDenied(&s.runtime.deniedConnections, route.ConnectionID)
}

// A dispatch admitted before the status commit may finish normally. The gate
// covers only local checkpointing; it is released before any remote HTTP.
func (s *Service) pinConnectionDispatch(route *gatewayRoute) (func(), bool) {
	if s.runtime == nil || route == nil {
		return func() {}, false
	}
	runtime := s.runtime
	runtime.publication.RLock()
	var once sync.Once
	release := func() { once.Do(runtime.publication.RUnlock) }
	auth, routes := runtime.auth.Load(), runtime.routes.Load()
	if auth == nil || !time.Now().Before(auth.ValidUntil) || routes == nil || routes.ID != route.SnapshotID || !s.runtimeConnectionAllowed(auth, *route) {
		release()
		return func() {}, false
	}
	return release, true
}
