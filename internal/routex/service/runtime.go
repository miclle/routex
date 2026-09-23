package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/upstream"
)

const runtimeRefreshInterval = time.Second
const runtimeAuthorizationLease = 5 * time.Second
const runtimeRefreshTimeout = 3 * time.Second

var runtimeUnavailable = &apperrors.Error{Code: 503, Message: "runtime configuration is temporarily unavailable"}

type gatewayRuntime struct {
	cancel               context.CancelFunc
	done                 chan struct{}
	mu                   sync.Mutex
	auth                 atomic.Pointer[runtimeAuthorization]
	routes               atomic.Pointer[runtimeRoutes]
	status               atomic.Pointer[RuntimeStatus]
	epoch                atomic.Uint64
	deniedKeys           sync.Map
	deniedLimits         sync.Map
	deniedUsers          sync.Map
	deniedProjects       sync.Map
	deniedModels         sync.Map
	deniedProviderModels sync.Map
	deniedCredentials    sync.Map
	lastRecordedState    string
}

type runtimeAuthorization struct {
	Quota               *runtimeQuotaData
	ConnectionRevisions map[string]string
	ValidUntil          time.Time
	LimitPolicies       map[string]limits.Policy
	LimitRoots          map[string]string
	Keys                map[string]runtimeKey
	Names               map[string]entity.ModelName
	Models              map[string]bool
	Credentials         map[string]bool
	ProviderModels      map[string]bool
	CredentialAccess    map[string]map[string]bool
	ModelCreated        map[string]time.Time
}

type runtimeKey struct {
	Key       entity.APIKey
	ProjectID string
	Models    []string
}

type runtimeRoutes struct {
	ID          string
	Digest      string
	PublishedAt time.Time
	Models      map[string][]runtimeRoute
}

type runtimeRoute struct {
	Route       gatewayRoute
	Credentials []runtimeCredential
}

type runtimeCredential struct {
	ID        string
	Plaintext string
	Priority  int
	CreatedAt time.Time
}

// RuntimeStatus exposes publication metadata without keys or routing secrets.
type RuntimeStatus struct {
	Enabled                 bool       `json:"enabled"`
	Ready                   bool       `json:"ready"`
	SnapshotID              string     `json:"snapshot_id"`
	PublishedAt             *time.Time `json:"published_at,omitempty"`
	AuthorizationValidUntil *time.Time `json:"authorization_valid_until,omitempty"`
	LastRefreshAt           *time.Time `json:"last_refresh_at,omitempty"`
	ErrorCode               string     `json:"error_code,omitempty"`
}

// StartRuntime loads the initial configuration before serving requests and then
// refreshes it until ctx is canceled. Call once during application startup.
func (s *Service) StartRuntime(ctx context.Context) error {
	if s.runtime != nil {
		return errors.New("runtime is already started")
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.runtime = &gatewayRuntime{cancel: cancel, done: make(chan struct{})}
	if err := s.RefreshRuntime(runCtx); err != nil {
		cancel()
		close(s.runtime.done)
		return err
	}
	go func() {
		defer close(s.runtime.done)
		ticker := time.NewTicker(runtimeRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				_ = s.RefreshRuntime(runCtx)
			}
		}
	}()
	return nil
}

// StopRuntime stops and joins the background publisher during shutdown.
func (s *Service) StopRuntime() {
	if s.runtime != nil && s.runtime.cancel != nil {
		s.runtime.cancel()
		<-s.runtime.done
		if routes := s.runtime.routes.Load(); routes != nil {
			closeRuntimeClients(routes.Models)
		}
	}
}

// RefreshRuntime independently publishes current authorization and validated
// routes. Invalid routes never replace the last valid routing snapshot.
func (s *Service) RefreshRuntime(ctx context.Context) error {
	runtime := s.runtime
	if runtime == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, runtimeRefreshTimeout)
	defer cancel()
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if ctx.Err() != nil {
		return runtimeUnavailable
	}
	generation := runtime.epoch.Load()
	started := time.Now().UTC()
	data, err := s.loadRuntimeData(ctx)
	if err != nil {
		s.setRuntimeStatus(ctx, started, "database_unavailable")
		return runtimeUnavailable
	}
	auth := buildRuntimeAuthorization(data, started.Add(runtimeAuthorizationLease))
	runtime.auth.Store(auth)
	clearRuntimeTombstones(&runtime.deniedKeys, generation)
	clearRuntimeTombstones(&runtime.deniedLimits, generation)
	clearRuntimeTombstones(&runtime.deniedUsers, generation)
	clearRuntimeTombstones(&runtime.deniedProjects, generation)
	clearRuntimeTombstones(&runtime.deniedModels, generation)
	// Credential revocations are also checked against the freshly published
	// eligibility map, so clearing older tombstones cannot restore disabled keys.
	clearRuntimeTombstones(&runtime.deniedCredentials, generation)
	clearRuntimeTombstones(&runtime.deniedProviderModels, generation)
	digest, err := runtimeDigest(data)
	if err != nil {
		s.setRuntimeStatus(ctx, started, "invalid_configuration")
		return runtimeUnavailable
	}
	current := runtime.routes.Load()
	if current == nil || current.Digest != digest {
		routes, err := s.buildRuntimeRoutes(data)
		if err != nil {
			s.setRuntimeStatus(ctx, started, "invalid_configuration")
			return runtimeUnavailable
		}
		snapshotID, err := id.NewPrefixed("cfg")
		if err != nil {
			closeRuntimeClients(routes)
			return runtimeUnavailable
		}
		runtime.routes.Store(&runtimeRoutes{ID: snapshotID, Digest: digest, PublishedAt: started, Models: routes})
		if current != nil {
			closeRuntimeClients(current.Models)
		}
	}
	s.setRuntimeStatus(ctx, started, "")
	return nil
}

// refreshAfterMutation must run after a successful control-plane transaction and
// before its successful response. Reduction paths also install a tombstone first.
func (s *Service) refreshAfterMutation(ctx context.Context, err error) error {
	if err != nil {
		return err
	}
	return s.RefreshRuntime(ctx)
}

// RefreshAfterMutation is the handler-facing variant for composed operations.
func (s *Service) RefreshAfterMutation(ctx context.Context, err error) error {
	return s.refreshAfterMutation(ctx, err)
}

func (s *Service) RuntimeStatus() RuntimeStatus {
	if s.runtime == nil {
		return RuntimeStatus{}
	}
	status := s.runtime.status.Load()
	if status == nil {
		return RuntimeStatus{Enabled: true, ErrorCode: "not_published"}
	}
	result := *status
	if result.AuthorizationValidUntil == nil || !time.Now().Before(*result.AuthorizationValidUntil) {
		result.Ready = false
		result.ErrorCode = "authorization_expired"
	}
	return result
}

func (s *Service) setRuntimeStatus(ctx context.Context, now time.Time, code string) {
	runtime := s.runtime
	status := &RuntimeStatus{Enabled: true, LastRefreshAt: &now, ErrorCode: code}
	if auth := runtime.auth.Load(); auth != nil {
		until := auth.ValidUntil
		status.AuthorizationValidUntil = &until
	}
	if routes := runtime.routes.Load(); routes != nil {
		published := routes.PublishedAt
		status.SnapshotID, status.PublishedAt = routes.ID, &published
	}
	status.Ready = status.SnapshotID != "" && status.AuthorizationValidUntil != nil && now.Before(*status.AuthorizationValidUntil)
	runtime.status.Store(status)
	state := status.SnapshotID + ":" + code
	if runtime.lastRecordedState == state {
		return
	}
	publicationID, err := id.NewPrefixed("pub")
	if err != nil {
		return
	}
	row := entity.RuntimePublication{ID: publicationID, SnapshotID: status.SnapshotID, Status: "ready", ErrorCode: code, CreatedAt: now}
	if code != "" {
		row.Status = "failed"
	}
	// Publication metadata is best-effort. Audit events remain in each original
	// mutation transaction and are not replaced by this operational history.
	if s.authDB(ctx).Create(&row).Error == nil {
		runtime.lastRecordedState = state
	}
}

func (s *Service) InvalidateRuntimeKey(keyID string) {
	if s.runtime != nil {
		s.runtime.deniedKeys.Store(keyID, s.runtime.epoch.Add(1))
	}
}
func (s *Service) InvalidateRuntimeUser(userID string) {
	if s.runtime != nil {
		s.runtime.deniedUsers.Store(userID, s.runtime.epoch.Add(1))
	}
}
func (s *Service) InvalidateRuntimeModel(modelID string) {
	if s.runtime != nil {
		s.runtime.deniedModels.Store(modelID, s.runtime.epoch.Add(1))
	}
}
func (s *Service) InvalidateRuntimeCredential(credentialID string) {
	if s.runtime != nil {
		s.runtime.deniedCredentials.Store(credentialID, s.runtime.epoch.Add(1))
	}
}

func clearRuntimeTombstones(tombstones *sync.Map, generation uint64) {
	tombstones.Range(func(key, value any) bool {
		if value.(uint64) <= generation {
			tombstones.CompareAndDelete(key, value)
		}
		return true
	})
}

func runtimeDenied(tombstones *sync.Map, key string) bool {
	_, denied := tombstones.Load(key)
	return denied
}

func (s *Service) authenticateRuntimeKey(bearer string) (*KeyRecord, error) {
	runtime := s.runtime
	auth := runtime.auth.Load()
	if auth == nil || !time.Now().Before(auth.ValidUntil) {
		return nil, runtimeUnavailable
	}
	personal := len(bearer) == 46 && bearer[:3] == "rx_"
	project := len(bearer) == 47 && bearer[:4] == "rxp_"
	if !personal && !project {
		return nil, apperrors.ErrUnauthorized
	}
	key, exists := auth.Keys[secret.SHA256Hex(bearer)]
	if !exists || project != (key.ProjectID != "") || runtimeDenied(&runtime.deniedKeys, key.Key.ID) || (key.Key.ExpiresAt != nil && !time.Now().Before(*key.Key.ExpiresAt)) {
		return nil, apperrors.ErrUnauthorized
	}
	if (project && runtimeDenied(&runtime.deniedProjects, key.ProjectID)) || (personal && runtimeDenied(&runtime.deniedUsers, key.Key.UserID)) {
		return nil, apperrors.ErrUnauthorized
	}
	models := make([]string, 0, len(key.Models))
	for _, modelID := range key.Models {
		if !runtimeDenied(&runtime.deniedModels, modelID) {
			models = append(models, modelID)
		}
	}
	return &KeyRecord{Key: key.Key, ProjectID: key.ProjectID, ModelIDs: models}, nil
}

func (s *Service) runtimeModelName(name string) (entity.ModelName, bool) {
	auth := s.runtime.auth.Load()
	if auth == nil || !time.Now().Before(auth.ValidUntil) {
		return entity.ModelName{}, false
	}
	model, exists := auth.Names[name]
	if !exists || !auth.Models[model.ModelID] || runtimeDenied(&s.runtime.deniedModels, model.ModelID) {
		return entity.ModelName{}, false
	}
	if model.CurrentModelID == nil && (model.ExpiresAt == nil || !time.Now().Before(*model.ExpiresAt)) {
		return entity.ModelName{}, false
	}
	return model, true
}

func (s *Service) runtimeRoute(modelID string) (*gatewayRoute, string, error) {
	return s.runtimeProtocolRoute(modelID, entity.ProtocolOpenAIChat)
}
func (s *Service) runtimeProtocolRoute(modelID, protocol string) (*gatewayRoute, string, error) {
	runtime := s.runtime
	auth, routes := runtime.auth.Load(), runtime.routes.Load()
	if auth == nil || !time.Now().Before(auth.ValidUntil) || routes == nil {
		return nil, "", runtimeUnavailable
	}
	candidates := []runtimeRoute{}
	for _, candidate := range routes.Models[modelID] {
		if candidate.Route.Protocol == protocol {
			candidates = append(candidates, candidate)
		}
	}
	weights := make([]int, len(candidates))
	available := make([]bool, len(candidates))
	for i := range candidates {
		weights[i] = candidates[i].Route.Weight
		id := candidates[i].Route.ProviderModelID
		available[i] = auth.ProviderModels[id] && !runtimeDenied(&runtime.deniedProviderModels, id) && candidates[i].Route.EgressRevision != "" && auth.ConnectionRevisions[candidates[i].Route.ConnectionID] == candidates[i].Route.EgressRevision && candidates[i].Route.EgressGeneration == s.egressGeneration.Load()
	}
	chosen, err := chooseAvailableGatewayRoute(weights, available)
	if err != nil {
		return nil, "", err
	}
	candidate := candidates[chosen]
	for _, credential := range candidate.Credentials {
		if auth.Credentials[credential.ID] && auth.CredentialAccess[credential.ID][candidate.Route.ProviderModelID] && !runtimeDenied(&runtime.deniedCredentials, credential.ID) {
			route := candidate.Route
			route.CredentialID = credential.ID
			route.SnapshotID = routes.ID
			return &route, credential.Plaintext, nil
		}
	}
	return nil, "", gatewayError(503, "upstream_unavailable", "No usable upstream is available.")
}

type runtimeData struct {
	Quota            *runtimeQuotaData
	Egresses         []entity.Egress
	EgressSetting    entity.EgressSetting
	EgressGeneration uint64
	Limits           []entity.ResourceLimit
	LimitPolicies    map[string]limits.Policy
	LimitRoots       map[string]string
	Pricing          *runtimePricingData
	ProjectData      *projectRuntimeData
	Users            []entity.User
	Keys             []entity.APIKey
	Scopes           []entity.APIKeyModel
	Grants           []entity.UserModelGrant
	Models           []entity.Model
	Names            []entity.ModelName
	Connections      []entity.ProviderConnection
	Credentials      []entity.ProviderCredential
	ProviderModels   []entity.ProviderModel
	Access           []entity.CredentialModelAccess
	Bindings         []entity.ModelProviderBinding
}

func (s *Service) loadRuntimeData(ctx context.Context) (*runtimeData, error) {
	data := &runtimeData{EgressGeneration: s.egressGeneration.Load()}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		// A repeatable-read transaction prevents mixed entity generations.
		if err := tx.Select("id", "disabled", "created_at").Find(&data.Users).Error; err != nil {
			return err
		}
		for _, target := range []any{&data.Limits, &data.Keys, &data.Scopes, &data.Grants, &data.Models, &data.Names, &data.Connections, &data.Credentials, &data.ProviderModels, &data.Access, &data.Bindings, &data.Egresses} {
			if err := tx.Find(target).Error; err != nil {
				return err
			}
		}
		if err := tx.First(&data.EgressSetting, 1).Error; err != nil {
			return err
		}
		var err error
		data.ProjectData, err = loadProjectRuntimeData(tx)
		if err != nil {
			return err
		}
		data.Pricing, err = loadRuntimePricing(tx)
		if err != nil {
			return err
		}
		data.Quota, err = loadRuntimeQuota(tx, data)
		if err != nil {
			return err
		}
		return compileRuntimeLimits(data)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return data, err
}

func buildRuntimeAuthorization(data *runtimeData, until time.Time) *runtimeAuthorization {
	auth := &runtimeAuthorization{Quota: data.Quota, ConnectionRevisions: runtimeConnectionRevisions(data), ValidUntil: until, LimitPolicies: data.LimitPolicies, LimitRoots: data.LimitRoots, Keys: map[string]runtimeKey{}, Names: map[string]entity.ModelName{}, Models: map[string]bool{}, Credentials: map[string]bool{}, ProviderModels: map[string]bool{}, CredentialAccess: map[string]map[string]bool{}, ModelCreated: map[string]time.Time{}}
	users := map[string]bool{}
	for _, user := range data.Users {
		users[user.ID] = !user.Disabled
	}
	for _, model := range data.Models {
		auth.Models[model.ID] = model.Status == "active"
		auth.ModelCreated[model.ID] = model.CreatedAt
	}
	for _, name := range data.Names {
		auth.Names[name.Name] = name
	}
	grants := map[string]map[string]bool{}
	for _, grant := range data.Grants {
		if grants[grant.UserID] == nil {
			grants[grant.UserID] = map[string]bool{}
		}
		grants[grant.UserID][grant.ModelID] = true
	}
	scopes := map[string][]string{}
	for _, scope := range data.Scopes {
		scopes[scope.KeyID] = append(scopes[scope.KeyID], scope.ModelID)
	}
	for _, key := range data.Keys {
		if key.Status != entity.KeyActive || !users[key.UserID] {
			continue
		}
		allowed := []string{}
		for _, modelID := range scopes[key.ID] {
			if auth.Models[modelID] && grants[key.UserID][modelID] {
				allowed = append(allowed, modelID)
			}
		}
		sort.Strings(allowed)
		auth.Keys[key.TokenHash] = runtimeKey{Key: key, Models: allowed}
	}
	addProjectRuntimeAuthorization(auth, data.ProjectData, users)
	for _, model := range data.ProviderModels {
		auth.ProviderModels[model.ID] = !model.Disabled
	}
	for _, credential := range data.Credentials {
		auth.Credentials[credential.ID] = credential.Enabled && credential.VerificationStatus == "verified"
	}
	for _, access := range data.Access {
		if auth.CredentialAccess[access.CredentialID] == nil {
			auth.CredentialAccess[access.CredentialID] = map[string]bool{}
		}
		auth.CredentialAccess[access.CredentialID][access.ProviderModelID] = true
	}
	return auth
}

func runtimeDigest(data *runtimeData) (string, error) {
	// Serialize sorted rows so query order cannot trigger a spurious publication.
	// Ciphertexts are hashed only; plaintext is never part of this source object.
	sort.Slice(data.Connections, func(i, j int) bool { return data.Connections[i].ID < data.Connections[j].ID })
	sort.Slice(data.Credentials, func(i, j int) bool { return data.Credentials[i].ID < data.Credentials[j].ID })
	sort.Slice(data.ProviderModels, func(i, j int) bool { return data.ProviderModels[i].ID < data.ProviderModels[j].ID })
	sort.Slice(data.Bindings, func(i, j int) bool { return data.Bindings[i].ID < data.Bindings[j].ID })
	sort.Slice(data.Access, func(i, j int) bool {
		a, b := data.Access[i], data.Access[j]
		if a.CredentialID == b.CredentialID {
			return a.ProviderModelID < b.ProviderModelID
		}
		return a.CredentialID < b.CredentialID
	})
	sort.Slice(data.Limits, func(i, j int) bool {
		a, b := data.Limits[i], data.Limits[j]
		if a.ScopeKind == b.ScopeKind {
			return a.ScopeID < b.ScopeID
		}
		return a.ScopeKind < b.ScopeKind
	})
	egresses := append([]entity.Egress(nil), data.Egresses...)
	sort.Slice(egresses, func(i, j int) bool { return egresses[i].ID < egresses[j].ID })
	ciphertexts := map[string]string{}
	for i := range egresses {
		ciphertexts[egresses[i].ID] = egresses[i].AuthCiphertext
		egresses[i].LastDiagnostic = ""
		egresses[i].LastCheckedAt = nil
		egresses[i].UpdatedAt = time.Time{}
	}
	raw, err := json.Marshal(struct {
		Quota            *runtimeQuotaData
		Egresses         []entity.Egress
		EgressSecrets    map[string]string
		EgressSetting    entity.EgressSetting
		EgressGeneration uint64
		Limits           []entity.ResourceLimit
		Pricing          *runtimePricingData
		Connections      []entity.ProviderConnection
		Credentials      []entity.ProviderCredential
		ProviderModels   []entity.ProviderModel
		Bindings         []entity.ModelProviderBinding
		Access           []entity.CredentialModelAccess
	}{data.Quota, egresses, ciphertexts, data.EgressSetting, data.EgressGeneration, data.Limits, data.Pricing, data.Connections, data.Credentials, data.ProviderModels, data.Bindings, data.Access})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func (s *Service) buildRuntimeRoutes(data *runtimeData) (map[string][]runtimeRoute, error) {
	clients, err := s.runtimeConnectionClients(data)
	if err != nil {
		return nil, err
	}
	completed := false
	defer func() {
		if !completed {
			closeEgressClients(clients)
		}
	}()
	connections := map[string]entity.ProviderConnection{}
	for _, connection := range data.Connections {
		if !entity.SupportedNativeProtocol(connection.Protocol) {
			continue
		}
		if _, err := upstream.ValidateBaseURL(connection.BaseURL, s.allowPrivateUpstream); err != nil {
			return nil, runtimeUnavailable
		}
		connections[connection.ID] = connection
	}
	providerModels := map[string]entity.ProviderModel{}
	for _, model := range data.ProviderModels {
		providerModels[model.ID] = model
	}
	access := map[string]map[string]bool{}
	for _, grant := range data.Access {
		if access[grant.CredentialID] == nil {
			access[grant.CredentialID] = map[string]bool{}
		}
		access[grant.CredentialID][grant.ProviderModelID] = true
	}
	credentials := map[string][]runtimeCredential{}
	for _, credential := range data.Credentials {
		if !credential.Enabled || credential.VerificationStatus != "verified" {
			continue
		}
		if s.secrets == nil {
			return nil, runtimeUnavailable
		}
		plaintext, err := s.secrets.Open(credential.ID, credential.Ciphertext)
		if err != nil {
			return nil, runtimeUnavailable
		}
		credentials[credential.ConnectionID] = append(credentials[credential.ConnectionID], runtimeCredential{ID: credential.ID, Plaintext: plaintext, Priority: credential.Priority, CreatedAt: credential.CreatedAt})
	}
	for connectionID := range credentials {
		sort.Slice(credentials[connectionID], func(i, j int) bool {
			a, b := credentials[connectionID][i], credentials[connectionID][j]
			if a.Priority != b.Priority {
				return a.Priority < b.Priority
			}
			if !a.CreatedAt.Equal(b.CreatedAt) {
				return a.CreatedAt.Before(b.CreatedAt)
			}
			return a.ID < b.ID
		})
	}
	result := map[string][]runtimeRoute{}
	for _, binding := range data.Bindings {
		pm, exists := providerModels[binding.ProviderModelID]
		if !exists {
			return nil, runtimeUnavailable
		}
		connection, exists := connections[pm.ConnectionID]
		if !exists {
			continue
		}
		if binding.Weight < 0 || binding.Weight > 100 {
			return nil, runtimeUnavailable
		}
		_, egressRevision, _ := runtimeEgressSelection(data, connection)
		candidate := runtimeRoute{Route: gatewayRoute{Client: clients[connection.ID], EgressGeneration: data.EgressGeneration, EgressRevision: egressRevision, Protocol: connection.Protocol, PriceBasis: runtimePriceBasis(data.Pricing, pm.ID, connection.Protocol), BindingID: binding.ID, Weight: binding.Weight, ProviderID: connection.ProviderID, ProviderModelID: pm.ID, ConnectionID: connection.ID, UpstreamName: pm.UpstreamName, BaseURL: connection.BaseURL}}
		for _, credential := range credentials[connection.ID] {
			if access[credential.ID][pm.ID] {
				candidate.Credentials = append(candidate.Credentials, credential)
			}
		}
		result[binding.ModelID] = append(result[binding.ModelID], candidate)
	}
	for modelID, candidates := range result {
		totals := map[string]int{}
		for _, candidate := range candidates {
			totals[candidate.Route.Protocol] += candidate.Route.Weight
		}
		for _, total := range totals {
			if total != 0 && total != 100 {
				return nil, runtimeUnavailable
			}
		}
		active := make([]runtimeRoute, 0, len(candidates))
		for _, candidate := range candidates {
			if totals[candidate.Route.Protocol] > 0 {
				active = append(active, candidate)
			}
		}
		if len(active) == 0 {
			delete(result, modelID)
		} else {
			result[modelID] = active
		}
	}

	used := map[*http.Client]bool{}
	for _, candidates := range result {
		for _, candidate := range candidates {
			used[candidate.Route.Client] = true
		}
	}
	for _, client := range clients {
		if client != nil && !used[client] {
			client.CloseIdleConnections()
		}
	}
	completed = true
	return result, nil
}
