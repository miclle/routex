package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
)

const maxCredentialReadinessRoutes = 256

type credentialRetirementAttemptEvidence struct {
	AttemptID   string
	CompletedAt time.Time
}

type credentialRetirementRuntimeProjection struct {
	SnapshotID         string
	SourceDigest       string
	ScopeDigest        string
	EligibleRouteCount int
	Evidence           *credentialRetirementAttemptEvidence
	Blockers           []string
}

type credentialReadinessRoute struct {
	ModelID               string
	BindingID             string
	ProviderID            string
	ProviderModelID       string
	ProviderModelRevision string
	ConnectionID          string
	Protocol              string
	UpstreamName          string
	Weight                int
	SupportsImageInput    bool
	SupportsPDFInput      bool
	SourceAccess          bool
	SourceCandidate       bool
	ReplacementAccess     bool
	ReplacementCandidate  bool
}

// A capture retains only immutable publication pointers and public route facts.
// Credential plaintext, clients, and full runtime routes never leave this helper.
type credentialRetirementRuntimeCapture struct {
	SnapshotID           string
	SourceDigest         string
	EligibleRouteCount   int
	Blockers             []string
	connectionID         string
	connectionBirth      time.Time
	connectionProviderID string
	providerProof        runtimeProviderProof
	sourceID             string
	replacementID        string
	auth                 *runtimeAuthorization
	routes               *runtimeRoutes
	epoch                uint64
	egressGeneration     uint64
	transportRevision    string
	scope                []credentialReadinessRoute
}

func (s *Service) captureCredentialRetirementRuntime(connectionID, sourceID, replacementID string) (*credentialRetirementRuntimeCapture, error) {
	capture := &credentialRetirementRuntimeCapture{connectionID: connectionID, sourceID: sourceID, replacementID: replacementID}
	defer func() { sort.Strings(capture.Blockers) }()
	runtime := s.runtime
	if runtime == nil || !runtime.mu.TryLock() {
		capture.Blockers = []string{"runtime_unavailable"}
		return capture, nil
	}
	defer runtime.mu.Unlock()
	capture.auth, capture.routes = runtime.auth.Load(), runtime.routes.Load()
	capture.epoch, capture.egressGeneration = runtime.epoch.Load(), s.egressGeneration.Load()
	auth, routes := capture.auth, capture.routes
	if auth == nil || routes == nil || routes.ID == "" || !s.gatewayAttemptClock().Before(auth.ValidUntil) {
		capture.Blockers = []string{"runtime_unavailable"}
		return capture, nil
	}
	if auth.SourceDigest == "" || auth.SourceDigest != routes.Digest {
		capture.Blockers = []string{"runtime_stale"}
		return capture, nil
	}
	capture.SnapshotID, capture.SourceDigest = routes.ID, auth.SourceDigest
	proof := auth.Connections[connectionID]
	capture.connectionBirth, capture.connectionProviderID = proof.Birth, proof.ProviderID
	capture.providerProof = auth.Providers[proof.ProviderID]
	related := 0
	for modelID, candidates := range routes.Models {
		for _, candidate := range candidates {
			route := candidate.Route
			if route.ConnectionID != connectionID {
				continue
			}
			related++
			if related > maxCredentialReadinessRoutes {
				capture.scope = nil
				capture.EligibleRouteCount = 0
				capture.Blockers = []string{"scope_overflow"}
				return capture, nil
			}
			if route.Weight <= 0 || !auth.Models[modelID] || !auth.ProviderModels[route.ProviderModelID] {
				continue
			}
			observed := credentialReadinessRoute{
				ModelID:               modelID,
				BindingID:             route.BindingID,
				ProviderID:            route.ProviderID,
				ProviderModelID:       route.ProviderModelID,
				ProviderModelRevision: auth.ProviderModelRevisions[route.ProviderModelID],
				ConnectionID:          route.ConnectionID,
				Protocol:              route.Protocol,
				UpstreamName:          route.UpstreamName,
				Weight:                route.Weight,
				SupportsImageInput:    route.SupportsImageInput,
				SupportsPDFInput:      route.SupportsPDFInput,
				SourceAccess:          auth.CredentialAccess[sourceID][route.ProviderModelID],
				ReplacementAccess:     auth.CredentialAccess[replacementID][route.ProviderModelID],
			}
			for _, credential := range candidate.Credentials {
				if credential.ID == sourceID {
					observed.SourceCandidate = true
				}
				if credential.ID == replacementID {
					observed.ReplacementCandidate = true
				}
			}
			capture.scope = append(capture.scope, observed)
			if !entity.SupportedNativeProtocol(route.Protocol) ||
				!s.runtimeConnectionAllowed(auth, route) ||
				route.Client == nil ||
				route.EgressRevision == "" ||
				route.EgressGeneration != capture.egressGeneration ||
				auth.ConnectionRevisions[connectionID] != route.EgressRevision ||
				runtimeDenied(&runtime.deniedModels, modelID) ||
				runtimeDenied(&runtime.deniedProviderModels, route.ProviderModelID) {
				capture.Blockers = appendCredentialReadinessBlocker(capture.Blockers, "route_unavailable")
				continue
			}
			if capture.transportRevision != "" && capture.transportRevision != route.EgressRevision {
				capture.Blockers = appendCredentialReadinessBlocker(capture.Blockers, "runtime_stale")
				continue
			}
			capture.transportRevision = route.EgressRevision
			if !observed.ReplacementCandidate || !observed.ReplacementAccess {
				capture.Blockers = appendCredentialReadinessBlocker(capture.Blockers, "coverage_missing")
				continue
			}
			if !auth.Credentials[replacementID] || runtimeDenied(&runtime.deniedCredentials, replacementID) || !s.gatewayAttemptHealthy(connectionID, replacementID) {
				capture.Blockers = appendCredentialReadinessBlocker(capture.Blockers, "route_unavailable")
				continue
			}
			capture.EligibleRouteCount++
		}
	}
	sortCredentialReadinessScope(capture.scope)
	if len(capture.scope) == 0 {
		capture.Blockers = appendCredentialReadinessBlocker(capture.Blockers, "no_eligible_routes")
	}
	return capture, nil
}

// Advisory reads validate after releasing their transaction. A pinned mutation
// may also validate before commit: TryLock never waits for a publisher which
// may itself be waiting for the database pool.
func (s *Service) validateCredentialRetirementRuntimeCapture(capture *credentialRetirementRuntimeCapture) []string {
	if capture == nil || s.runtime == nil || !s.runtime.mu.TryLock() {
		return []string{"runtime_unavailable"}
	}
	defer s.runtime.mu.Unlock()
	runtime := s.runtime
	auth, routes := runtime.auth.Load(), runtime.routes.Load()
	if auth == nil || routes == nil || !s.gatewayAttemptClock().Before(auth.ValidUntil) {
		return []string{"runtime_unavailable"}
	}
	if auth != capture.auth ||
		routes != capture.routes ||
		runtime.epoch.Load() != capture.epoch ||
		s.egressGeneration.Load() != capture.egressGeneration ||
		auth.SourceDigest == "" ||
		auth.SourceDigest != routes.Digest ||
		routes.ID != capture.SnapshotID {
		return []string{"runtime_stale"}
	}
	for _, route := range capture.scope {
		if !s.runtimeConnectionAllowed(auth, gatewayRoute{
			ConnectionID: capture.connectionID, ConnectionBirth: capture.connectionBirth,
			ProviderID: capture.connectionProviderID, ProviderBirth: capture.providerProof.Birth,
			ProviderEnabled: capture.providerProof.Enabled, ProviderRevision: capture.providerProof.Revision,
		}) ||
			auth.ConnectionRevisions[capture.connectionID] != capture.transportRevision ||
			runtimeDenied(&runtime.deniedModels, route.ModelID) ||
			runtimeDenied(&runtime.deniedProviderModels, route.ProviderModelID) ||
			runtimeDenied(&runtime.deniedCredentials, capture.replacementID) ||
			!s.gatewayAttemptHealthy(capture.connectionID, capture.replacementID) {
			return []string{"route_unavailable"}
		}
	}
	return nil
}

type credentialReadinessDBRoute struct {
	ModelRecordID         string
	BoundProviderModelID  string
	ConnectionID          string
	ModelID               string
	ModelStatus           string
	BindingID             string
	ProviderModelID       string
	ProviderModelRevision string
	Disabled              bool
	UpstreamName          string
	Weight                int
	SupportsImageInput    bool
	SupportsPDFInput      bool
}

// pinCredentialRetirementRuntime must precede every database borrow for a
// retirement operation. Publication is pinned without holding runtime.mu.
func (s *Service) pinCredentialRetirementRuntime() (func(), error) {
	if s.runtime == nil || !s.runtime.publication.TryLock() {
		return nil, runtimeUnavailable
	}
	var once sync.Once
	return func() { once.Do(s.runtime.publication.Unlock) }, nil
}

func (s *Service) credentialRetirementRuntimeReadiness(
	tx *gorm.DB,
	connection entity.ProviderConnection,
	source, replacement entity.ProviderCredential,
	capture *credentialRetirementRuntimeCapture,
) (*credentialRetirementRuntimeProjection, error) {
	return s.credentialRetirementReadinessWithAttempt(tx, connection, source, replacement, capture)
}

// An explicit proof never falls back to another attempt, including a newer one.
func (s *Service) credentialRetirementRuntimeReadinessForAttempt(
	tx *gorm.DB,
	connection entity.ProviderConnection,
	source, replacement entity.ProviderCredential,
	capture *credentialRetirementRuntimeCapture,
	attemptID string,
) (*credentialRetirementRuntimeProjection, error) {
	return s.credentialRetirementReadinessWithAttempt(tx, connection, source, replacement, capture, attemptID)
}

func (s *Service) credentialRetirementReadinessWithAttempt(
	tx *gorm.DB,
	connection entity.ProviderConnection,
	source, replacement entity.ProviderCredential,
	capture *credentialRetirementRuntimeCapture,
	reviewedAttemptID ...string,
) (*credentialRetirementRuntimeProjection, error) {
	result, err := s.credentialRetirementRuntimeScope(tx, connection, source, replacement, capture)
	if err != nil || len(result.Blockers) > 0 {
		return result, err
	}
	result.Evidence, err = completedCredentialRetirementAttempt(tx, replacement, capture, reviewedAttemptID...)
	if err != nil {
		return nil, err
	}
	if result.Evidence == nil {
		result.Blockers = appendCredentialReadinessBlocker(result.Blockers, "evidence_missing")
	}
	return result, nil
}

// A known durable receipt reconciles present application only. A fresh native
// invocation is not required, and this helper never disables the source again.
func (s *Service) credentialRetirementRuntimeApplication(
	tx *gorm.DB,
	connection entity.ProviderConnection,
	source, replacement entity.ProviderCredential,
	capture *credentialRetirementRuntimeCapture,
) (*credentialRetirementRuntimeProjection, error) {
	if blockers := credentialRetirementRuntimeApplicationBlockers(source, replacement, capture); len(blockers) > 0 {
		return &credentialRetirementRuntimeProjection{Blockers: blockers}, nil
	}
	return s.credentialRetirementRuntimeScope(tx, connection, source, replacement, capture)
}

func credentialRetirementRuntimeApplicationBlockers(
	source, replacement entity.ProviderCredential,
	capture *credentialRetirementRuntimeCapture,
) []string {
	if capture == nil {
		return []string{"runtime_unavailable"}
	}
	if source.ID == "" || source.Enabled {
		return []string{"runtime_stale"}
	}
	if replacement.ID == "" || replacement.ReplacesCredentialID == nil ||
		*replacement.ReplacesCredentialID != source.ID || replacement.ConnectionID != source.ConnectionID {
		return []string{"runtime_stale"}
	}
	if !replacement.Enabled {
		return []string{"replacement_disabled"}
	}
	if replacement.VerificationStatus != "verified" {
		return []string{"replacement_unverified"}
	}
	for _, route := range capture.scope {
		if route.SourceCandidate {
			return []string{"route_unavailable"}
		}
	}
	return nil
}

func (s *Service) credentialRetirementRuntimeScope(
	tx *gorm.DB,
	connection entity.ProviderConnection,
	source, replacement entity.ProviderCredential,
	capture *credentialRetirementRuntimeCapture,
) (*credentialRetirementRuntimeProjection, error) {
	result := &credentialRetirementRuntimeProjection{Blockers: []string{}}
	defer func() { sort.Strings(result.Blockers) }()
	if capture == nil {
		result.Blockers = []string{"runtime_unavailable"}
		return result, nil
	}
	result.SnapshotID, result.SourceDigest, result.EligibleRouteCount = capture.SnapshotID, capture.SourceDigest, capture.EligibleRouteCount
	result.Blockers = append(result.Blockers, capture.Blockers...)
	if capture.auth == nil || capture.routes == nil || capture.SnapshotID == "" {
		return result, nil
	}
	if capture.connectionID != connection.ID ||
		capture.connectionProviderID != connection.ProviderID ||
		!capture.connectionBirth.Equal(connection.CreatedAt) ||
		source.ConnectionID != connection.ID ||
		replacement.ConnectionID != connection.ID ||
		source.ID != capture.sourceID ||
		replacement.ID != capture.replacementID ||
		capture.auth.CredentialRevisions[source.ID] != credentialRuntimeRevision(source) ||
		capture.auth.CredentialRevisions[replacement.ID] != credentialRuntimeRevision(replacement) {
		result.Blockers = appendCredentialReadinessBlocker(result.Blockers, "runtime_stale")
		result.SnapshotID = ""
		result.EligibleRouteCount = 0
		return result, nil
	}
	var rows []credentialReadinessDBRoute
	columns := "b.model_id, m.id AS model_record_id, b.provider_model_id AS bound_provider_model_id, " +
		"p.connection_id, m.status AS model_status, b.id AS binding_id, p.id AS provider_model_id, " +
		"p.e_tag AS provider_model_revision, p.disabled, p.upstream_name, b.weight, " +
		"p.supports_image_input, p.supports_pdf_input"
	err := tx.Table("model_provider_bindings AS b").Select(columns).
		Joins("JOIN models m ON m.id = b.model_id").
		Joins("JOIN provider_models p ON p.id = b.provider_model_id").
		Where("p.connection_id = ?", connection.ID).
		Order("b.id").Limit(maxCredentialReadinessRoutes + 1).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) > maxCredentialReadinessRoutes {
		result.Blockers = appendCredentialReadinessBlocker(result.Blockers, "scope_overflow")
		result.EligibleRouteCount = 0
		return result, nil
	}
	providerModelIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		providerModelIDs = append(providerModelIDs, row.ProviderModelID)
	}
	var accesses []entity.CredentialModelAccess
	if err := tx.Where("credential_id IN ? AND provider_model_id IN ?", []string{source.ID, replacement.ID}, providerModelIDs).
		Order("credential_id, provider_model_id").Limit(maxCredentialReadinessRoutes*2 + 1).Find(&accesses).Error; err != nil {
		return nil, err
	}
	if len(accesses) > maxCredentialReadinessRoutes*2 {
		result.Blockers = appendCredentialReadinessBlocker(result.Blockers, "scope_overflow")
		result.EligibleRouteCount = 0
		return result, nil
	}
	if entity.ConnectionAdapter(connection) == entity.AdapterAzureOpenAIClassic {
		_, coverage, e := connectionCredentialCoverage(tx, connection.ID, providerModelIDs)
		if e != nil {
			return nil, e
		}
		accesses = nil
		for _, c := range []string{source.ID, replacement.ID} {
			for m, allowed := range coverage[c] {
				if allowed {
					accesses = append(accesses, entity.CredentialModelAccess{CredentialID: c, ProviderModelID: m})
				}
			}
		}
	}

	access := map[string]map[string]bool{source.ID: {}, replacement.ID: {}}
	for _, row := range accesses {
		if access[row.CredentialID] != nil {
			access[row.CredentialID][row.ProviderModelID] = true
		}
	}
	scope := []credentialReadinessRoute{}
	for _, row := range rows {
		if row.ModelID != row.ModelRecordID || row.BoundProviderModelID != row.ProviderModelID || row.ConnectionID != connection.ID {
			result.Blockers = appendCredentialReadinessBlocker(result.Blockers, "runtime_stale")
			result.SnapshotID = ""
			return result, nil
		}
		if row.Weight <= 0 || row.ModelStatus != "active" || row.Disabled {
			continue
		}
		scope = append(scope, credentialReadinessRoute{
			ModelID:               row.ModelID,
			BindingID:             row.BindingID,
			ProviderID:            connection.ProviderID,
			ProviderModelID:       row.ProviderModelID,
			ProviderModelRevision: row.ProviderModelRevision,
			ConnectionID:          connection.ID,
			Protocol:              connection.Protocol,
			UpstreamName:          row.UpstreamName,
			Weight:                row.Weight,
			SupportsImageInput:    row.SupportsImageInput,
			SupportsPDFInput:      row.SupportsPDFInput,
			SourceAccess:          access[source.ID][row.ProviderModelID],
			SourceCandidate:       access[source.ID][row.ProviderModelID] && source.Enabled && source.VerificationStatus == "verified",
			ReplacementAccess:     access[replacement.ID][row.ProviderModelID],
			ReplacementCandidate:  access[replacement.ID][row.ProviderModelID] && replacement.Enabled && replacement.VerificationStatus == "verified",
		})
	}
	sortCredentialReadinessScope(scope)
	if !slices.Equal(scope, capture.scope) {
		result.Blockers = appendCredentialReadinessBlocker(result.Blockers, "runtime_stale")
		result.SnapshotID = ""
		result.EligibleRouteCount = 0
		return result, nil
	}
	transport, enabled, err := credentialReadinessTransportRevision(tx, connection)
	if err != nil {
		return nil, err
	}
	if !enabled {
		result.Blockers = appendCredentialReadinessBlocker(result.Blockers, "route_unavailable")
	}
	if len(scope) > 0 && transport != capture.transportRevision {
		result.Blockers = appendCredentialReadinessBlocker(result.Blockers, "runtime_stale")
		result.SnapshotID = ""
		result.EligibleRouteCount = 0
		return result, nil
	}
	raw, _ := json.Marshal(struct {
		ConnectionID, Transport string
		Routes                  []credentialReadinessRoute
	}{connection.ID, transport, scope})
	digest := sha256.Sum256(raw)
	result.ScopeDigest = hex.EncodeToString(digest[:])
	return result, nil
}

func credentialReadinessTransportRevision(tx *gorm.DB, connection entity.ProviderConnection) (string, bool, error) {
	data := &runtimeData{}
	if connection.EgressMode == "" || connection.EgressMode == "default" {
		if err := tx.First(&data.EgressSetting, 1).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return "", false, nil
			}
			return "", false, err
		}
	}
	selected := connection.EgressID
	if connection.EgressMode == "" || connection.EgressMode == "default" {
		selected = data.EgressSetting.DefaultEgressID
	}
	if selected != nil {
		var row entity.Egress
		if err := tx.First(&row, "id = ?", *selected).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return "", false, nil
			}
			return "", false, err
		}
		data.Egresses = []entity.Egress{row}
	}
	_, revision, enabled := runtimeEgressSelection(data, connection)
	return revision, enabled, nil
}

func sortCredentialReadinessScope(scope []credentialReadinessRoute) {
	sort.Slice(scope, func(i, j int) bool {
		if scope[i].ModelID != scope[j].ModelID {
			return scope[i].ModelID < scope[j].ModelID
		}
		return scope[i].BindingID < scope[j].BindingID
	})
}
func appendCredentialReadinessBlocker(blockers []string, code string) []string {
	if !slices.Contains(blockers, code) {
		return append(blockers, code)
	}
	return blockers
}

type credentialRetirementEvidenceRow struct {
	AttemptID                string
	AttemptRequestID         string
	RequestID                string
	CredentialID             string
	SnapshotID               string
	CallSnapshotID           string
	CallStartedAt            time.Time
	AttemptStatus            string
	CallStatus               string
	NativeCompletionEvidence string
	ModelID                  string
	ProviderModelID          string
	CallProviderModelID      string
	ConnectionID             string
	CallConnectionID         string
	Protocol                 string
	StartedAt                time.Time
	CompletedAt              time.Time
	AttemptNumber            int
}

func completedCredentialRetirementAttempt(
	tx *gorm.DB,
	replacement entity.ProviderCredential,
	capture *credentialRetirementRuntimeCapture,
	reviewedAttemptID ...string,
) (*credentialRetirementAttemptEvidence, error) {
	if len(reviewedAttemptID) > 1 || len(reviewedAttemptID) == 1 && reviewedAttemptID[0] == "" {
		return nil, nil
	}
	if len(capture.scope) == 0 || len(capture.scope) > maxCredentialReadinessRoutes {
		return nil, nil
	}
	eligible := tx.Session(&gorm.Session{NewDB: true}).Where("1 = 0")
	for _, route := range capture.scope {
		eligible = eligible.Or(
			"calls.model_id = ? AND calls.protocol = ? AND attempt.provider_model_id = ? AND "+
				"attempt.connection_id = ? AND calls.provider_model_id = ? AND calls.connection_id = ?",
			route.ModelID, route.Protocol, route.ProviderModelID, route.ConnectionID, route.ProviderModelID, route.ConnectionID,
		)
	}
	later := tx.Table("call_attempts AS later").Select("1").Where("later.request_id = attempt.request_id AND later.attempt_number > attempt.attempt_number")
	var row credentialRetirementEvidenceRow
	columns := "attempt.id AS attempt_id, attempt.request_id AS attempt_request_id, calls.request_id, " +
		"attempt.credential_id, attempt.snapshot_id, calls.snapshot_id AS call_snapshot_id, " +
		"calls.started_at AS call_started_at, attempt.status AS attempt_status, calls.status AS call_status, " +
		"attempt.native_completion_evidence, calls.model_id, attempt.provider_model_id, " +
		"calls.provider_model_id AS call_provider_model_id, attempt.connection_id, " +
		"calls.connection_id AS call_connection_id, calls.protocol, attempt.started_at, " +
		"attempt.completed_at, attempt.attempt_number"
	query := tx.Table("call_attempts AS attempt").Select(columns).
		Joins("JOIN call_records AS calls ON calls.request_id = attempt.request_id").
		Where(
			"attempt.credential_id = ? AND attempt.snapshot_id = ? AND attempt.status = ? AND "+
				"attempt.native_completion_evidence = ? AND calls.status = ? AND "+
				"attempt.started_at >= ? AND calls.started_at >= ?",
			replacement.ID, capture.SnapshotID, "success", "completed", "success", replacement.CreatedAt, replacement.CreatedAt,
		).
		Where("NOT EXISTS (?)", later).Where(eligible).
		Order("attempt.completed_at DESC, attempt.id DESC")
	if len(reviewedAttemptID) == 1 {
		query = query.Where("attempt.id = ?", reviewedAttemptID[0])
	}
	found := query.Limit(1).Find(&row)
	if found.Error != nil {
		return nil, found.Error
	}
	if found.RowsAffected == 0 || !row.matches(replacement, capture, reviewedAttemptID...) {
		return nil, nil
	}
	return &credentialRetirementAttemptEvidence{AttemptID: row.AttemptID, CompletedAt: row.CompletedAt}, nil
}
func (row credentialRetirementEvidenceRow) matches(
	replacement entity.ProviderCredential,
	capture *credentialRetirementRuntimeCapture,
	reviewedAttemptID ...string,
) bool {
	if len(reviewedAttemptID) > 1 || len(reviewedAttemptID) == 1 && row.AttemptID != reviewedAttemptID[0] {
		return false
	}
	if row.AttemptID == "" ||
		row.RequestID == "" ||
		row.AttemptRequestID != row.RequestID ||
		row.CredentialID != replacement.ID ||
		row.SnapshotID != capture.SnapshotID ||
		row.CallSnapshotID != capture.SnapshotID ||
		row.CallStartedAt.Before(replacement.CreatedAt) ||
		row.AttemptStatus != "success" ||
		row.CallStatus != "success" ||
		row.NativeCompletionEvidence != "completed" ||
		row.StartedAt.Before(replacement.CreatedAt) ||
		row.CompletedAt.Before(row.StartedAt) ||
		row.AttemptNumber < 1 ||
		row.AttemptNumber > 32 {
		return false
	}
	for _, route := range capture.scope {
		if row.ModelID == route.ModelID &&
			row.Protocol == route.Protocol &&
			row.ProviderModelID == route.ProviderModelID &&
			row.CallProviderModelID == route.ProviderModelID &&
			row.ConnectionID == route.ConnectionID &&
			row.CallConnectionID == route.ConnectionID {
			return true
		}
	}
	return false
}
