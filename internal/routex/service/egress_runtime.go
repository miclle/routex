package service

import (
	"context"
	"net/http"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/upstream"
)

// Local mutations advance the admission generation before acknowledging them.
func (s *Service) invalidateEgressRuntime() { s.egressGeneration.Add(1) }

func runtimeEgressSelection(data *runtimeData, connection entity.ProviderConnection) (*entity.Egress, string, bool) {
	selected := connection.EgressID
	switch connection.EgressMode {
	case "", "default":
		selected = data.EgressSetting.DefaultEgressID
	case "direct":
		if selected != nil {
			return nil, "", false
		}
	case "proxy":
		if selected == nil {
			return nil, "", false
		}
	default:
		return nil, "", false
	}
	if selected == nil {
		return nil, egressRevision(connection, data.EgressSetting, nil), true
	}
	for i := range data.Egresses {
		row := &data.Egresses[i]
		if row.ID == *selected {
			return row, egressRevision(connection, data.EgressSetting, row), row.Enabled
		}
	}
	return nil, "", false
}
func runtimeConnectionRevisions(data *runtimeData) map[string]string {
	result := map[string]string{}
	for _, connection := range data.Connections {
		_, revision, enabled := runtimeEgressSelection(data, connection)
		if enabled {
			result[connection.ID] = revision
		}
	}
	return result
}
func (s *Service) runtimeConnectionClients(data *runtimeData) (map[string]*http.Client, error) {
	clients := map[string]*http.Client{}
	for _, connection := range data.Connections {
		row, _, enabled := runtimeEgressSelection(data, connection)
		if !enabled {
			continue
		}
		if row == nil {
			clients[connection.ID] = s.upstream
			continue
		}
		config, err := s.egressConfig(*row)
		if err != nil {
			closeEgressClients(clients)
			return nil, runtimeUnavailable
		}
		client, err := upstream.NewEgressClient(s.allowPrivateUpstream, s.allowPrivateEgress, *config)
		if err != nil {
			closeEgressClients(clients)
			return nil, runtimeUnavailable
		}
		clients[connection.ID] = client
	}
	return clients, nil
}
func closeEgressClients(clients map[string]*http.Client) {
	for _, client := range clients {
		if client != nil {
			client.CloseIdleConnections()
		}
	}
}
func closeRuntimeClients(routes map[string][]runtimeRoute) {
	seen := map[*http.Client]bool{}
	for _, candidates := range routes {
		for _, candidate := range candidates {
			client := candidate.Route.Client
			if client != nil && !seen[client] {
				client.CloseIdleConnections()
				seen[client] = true
			}
		}
	}
}
func (s *Service) prepareGatewayEgress(ctx context.Context, route *gatewayRoute) error {
	route.EgressGeneration = s.egressGeneration.Load()
	var connection entity.ProviderConnection
	if err := s.authDB(ctx).First(&connection, "id = ?", route.ConnectionID).Error; err != nil {
		return runtimeUnavailable
	}
	client, revision, err := s.clientForConnection(ctx, connection)
	if err != nil {
		return err
	}
	route.Client, route.EgressRevision = client, revision
	return nil
}
func (s *Service) admitEgressGatewayCall(ctx context.Context, requestID string, result *GatewayResult, route *gatewayRoute) error {
	s.egressMu.RLock()
	defer s.egressMu.RUnlock()
	if route.EgressGeneration != s.egressGeneration.Load() {
		return runtimeUnavailable
	}
	if s.runtime != nil {
		auth := s.runtime.auth.Load()
		if auth == nil || !time.Now().Before(auth.ValidUntil) || auth.ConnectionRevisions[route.ConnectionID] != route.EgressRevision {
			return runtimeUnavailable
		}
	}
	return s.admitLimitedGatewayCall(ctx, requestID, result)
}
