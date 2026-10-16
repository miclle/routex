package service

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sort"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/routeattempt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	gatewayReservationSQLChunk      = 256
	gatewayReservationSQLMaxQueries = 128
	gatewayReservationSQLMaxRows    = 32768
)

var errGatewayReservationSQLConflict = errors.New("captured gateway reservation changed")

// These read-only validators retain row locks in the caller's READ COMMITTED
// transaction. Successful owner, current authorization and policy checks in
// that SAME transaction are prerequisites. Governance must precede owner/auth
// locks; reacquiring it here cannot repair an incorrect caller lock order.
// The transaction must permit SELECT FOR UPDATE (not SQL ReadOnly=true).
// No schema, receipt, runtime wiring or authority token is introduced here.
func validateGatewayReservationAcceptSQL(tx *gorm.DB, original *GatewayReservationManifest) error {
	return validateGatewayReservationSQL(tx, original, nil)
}

func validateGatewayReservationDispatchSQL(tx *gorm.DB, original *GatewayReservationManifest, selected routeattempt.Attempt) error {
	if selected.Number < 1 || selected.Number > routeattempt.MaxAttempts {
		return errGatewayReservationManifest
	}
	return validateGatewayReservationSQL(tx, original, &selected)
}

func validateGatewayReservationSQL(tx *gorm.DB, original *GatewayReservationManifest, selected *routeattempt.Attempt) error {
	if tx == nil || tx.Statement == nil || tx.Error != nil {
		return errGatewayReservationManifest
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return errGatewayReservationManifest
	}
	if err := tx.Statement.Context.Err(); err != nil {
		return err
	}
	candidates, err := gatewayReservationSQLSelection(original, selected)
	if err != nil {
		return err
	}
	r := &gatewayReservationSQLReader{tx: tx}
	if err := r.query(); err != nil {
		return err
	}
	if err := lockGovernance(modelCreationDB(tx)); err != nil {
		return gatewayReservationSQLError(err)
	}
	facts, err := r.load(original, candidates)
	if err != nil {
		return err
	}
	for _, captured := range candidates {
		current, err := gatewayReservationSQLCurrent(original, captured, facts)
		if err != nil || !reflect.DeepEqual(captured, current) {
			return errGatewayReservationSQLConflict
		}
	}
	// Never recompute MaximumHold from only the selected dispatch candidate.
	// Local snapshot, source-holder, egress generation and health checks remain
	// separately mandatory immediately before actual upstream work.
	return tx.Statement.Context.Err()
}

// Validate the COMPLETE original capture even for selected-only current reads.
// This is consistency validation, not proof that any supplied capture is trusted.
func gatewayReservationSQLSelection(m *GatewayReservationManifest, selected *routeattempt.Attempt) ([]GatewayReservationCandidate, error) {
	if m == nil || m.Version != gatewayReservationManifestVersion || !safeCallID.MatchString(m.RequestID) || !modelCreationID(m.Model.ID, "mdl") || !connectionMetadataBirth(m.Model.Birth) || !runtimeSnapshotID.MatchString(m.SnapshotID) || !entity.SupportedNativeProtocol(m.Protocol) || m.MaximumAttempts != routeattempt.MaxAttempts || len(m.Candidates) == 0 || len(m.Candidates) > gatewayReservationMaxTargets*gatewayReservationMaxCredentials || !gatewayReservationSQLNativeValid(m.Protocol, m.Native) {
		return nil, errGatewayReservationManifest
	}
	raw, err := json.Marshal(m)
	if err != nil || len(raw) > gatewayReservationManifestMaxBytes {
		return nil, errGatewayReservationManifest
	}
	tokens, money := m.MaximumHold.Tokens != nil, m.MaximumHold.Money != nil
	if (tokens || money) && !m.Native.Supported || money && len(m.Native.PricingDimensions) != 0 {
		return nil, errGatewayReservationManifest
	}
	result := gatewayReservationSQLResult(m)
	targets := map[string]GatewayReservationCandidate{}
	counts := map[string]int{}
	owners := map[string]string{}
	byTarget := map[string]gatewayAttemptBoundEvidence{}
	var chosen []GatewayReservationCandidate
	previous := ""
	for _, c := range m.Candidates {
		key := c.Binding.ID + "\x00" + c.Credential.Identity.ID
		if key <= previous || !gatewayReservationSQLCandidateValid(c) {
			return nil, errGatewayReservationManifest
		}
		previous = key
		for _, media := range m.Native.Media {
			if media.Kind == gatewayAttachmentImage && !c.ImageInput || media.Kind == gatewayAttachmentPDF && !c.PDFInput {
				return nil, errGatewayReservationManifest
			}
		}
		if prior, exists := targets[c.Binding.ID]; exists {
			prior.Credential = c.Credential
			if !reflect.DeepEqual(prior, c) {
				return nil, errGatewayReservationManifest
			}
		} else {
			targets[c.Binding.ID] = c
		}
		counts[c.Binding.ID]++
		if len(targets) > gatewayReservationMaxTargets || counts[c.Binding.ID] > gatewayReservationMaxCredentials {
			return nil, errGatewayReservationManifest
		}
		if owner, exists := owners[c.Credential.Identity.ID]; exists && owner != c.Connection.ID {
			return nil, errGatewayReservationManifest
		}
		owners[c.Credential.Identity.ID] = c.Connection.ID
		if (tokens || money) != (c.Capacity != nil) || money != (c.PriceBasis != nil) {
			return nil, errGatewayReservationManifest
		}
		quota := &runtimeQuotaData{Bounds: map[string]entity.ReservationBound{}, Currency: m.MaximumHold.Currency}
		if c.Capacity != nil {
			b := c.Capacity
			if b.Protocol != m.Protocol || b.TransportGeneration != c.TransportGeneration || b.Revision == "" {
				return nil, errGatewayReservationManifest
			}
			quota.Bounds[c.ProviderModel.ID] = entity.ReservationBound{ProviderModelID: c.ProviderModel.ID, Protocol: b.Protocol, TransportGeneration: b.TransportGeneration, ETag: b.Revision, MaxInputTokens: b.MaximumInput, MaxOutputTokens: b.MaximumOutput}
		}
		if c.PriceBasis != nil && pricingBasisSize(c.PriceBasis) > callPricingSnapshotLimit {
			return nil, errGatewayReservationManifest
		}
		attempt := routeattempt.Attempt{TargetID: c.Binding.ID, ConnectionID: c.Connection.ID, CredentialID: c.Credential.Identity.ID, Protocol: m.Protocol}
		candidate := gatewayAttemptCandidate{attempt: attempt, route: gatewayRoute{ProviderModelID: c.ProviderModel.ID, PriceBasis: c.PriceBasis}}
		evidence, usable := gatewayCandidateBoundEvidence(candidate, result, quota, tokens, money)
		if !usable || !reflect.DeepEqual(evidence, c.BoundEvidence) {
			return nil, errGatewayReservationManifest
		}
		byTarget[c.Binding.ID] = evidence
		if selected == nil || selected.TargetID == attempt.TargetID && selected.ConnectionID == attempt.ConnectionID && selected.CredentialID == attempt.CredentialID && selected.Protocol == attempt.Protocol {
			chosen = append(chosen, c)
		}
	}
	maximum, err := gatewayReservationMaximum(byTarget, tokens, money, m.MaximumHold.Currency)
	if err != nil || !reflect.DeepEqual(maximum, m.MaximumHold) || len(chosen) == 0 || selected != nil && len(chosen) != 1 {
		return nil, errGatewayReservationManifest
	}
	return chosen, nil
}

func gatewayReservationSQLNativeValid(protocol string, n GatewayReservationNative) bool {
	container := map[string]string{entity.ProtocolOpenAIChat: "messages", entity.ProtocolOpenAIResponses: "input", entity.ProtocolAnthropicMessages: "messages", entity.ProtocolGeminiGenerateContent: "contents"}[protocol]
	if n.ClassifierVersion != 1 || n.InputContainer != container || !slices.Contains([]string{"array", "string", "object", "null", "other"}, n.InputShape) || n.MaximumOutput < 0 || (protocol == entity.ProtocolAnthropicMessages) != (n.MessagesHeadersSupported != nil) || n.Supported && (n.MaximumOutput <= 0 || n.MessagesHeadersSupported != nil && !*n.MessagesHeadersSupported) {
		return false
	}
	// Dimensions are parser-owned closed values. Do not admit arbitrary fields.
	allowed := []string{"external_tool", "request_service_tier", "request_non_text", "cache_retention", "request_condition"}
	seen := map[string]bool{}
	for _, value := range n.PricingDimensions {
		if !slices.Contains(allowed, value) || seen[value] {
			return false
		}
		seen[value] = true
	}
	for _, media := range n.Media {
		if media.Kind != gatewayAttachmentImage && media.Kind != gatewayAttachmentPDF {
			return false
		}
		switch protocol {
		case entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses:
			if media.Kind == gatewayAttachmentImage && media.Shape != "data_url" || media.Kind == gatewayAttachmentPDF && media.Shape != "openai_file" {
				return false
			}
		case entity.ProtocolAnthropicMessages:
			if media.Shape != "anthropic_source" {
				return false
			}
		case entity.ProtocolGeminiGenerateContent:
			if media.Shape != "gemini_inline_camel" && media.Shape != "gemini_inline_snake" {
				return false
			}
		}
	}
	return true
}

func gatewayReservationSQLCandidateValid(c GatewayReservationCandidate) bool {
	for prefix, identity := range map[string]GatewayReservationIdentity{"bnd": c.Binding, "prv": c.Provider, "con": c.Connection, "pmd": c.ProviderModel, "crd": c.Credential.Identity} {
		if !modelCreationID(identity.ID, prefix) || !connectionMetadataBirth(identity.Birth) {
			return false
		}
	}
	return c.Weight > 0 && c.Weight <= 100 && c.ProviderEnabled && c.ConnectionEnabled && !c.Disabled && c.ProviderRevision != "" && c.ConnectionRevision != "" && c.ProviderModelRevision != "" && validTransportGeneration(c.TransportGeneration) && c.CapabilityTransportGeneration == c.TransportGeneration && runtimeRouteDigest.MatchString(c.TransportIdentity) && runtimeRouteDigest.MatchString(c.EgressRevision) && c.Credential.Enabled && c.Credential.Priority >= 0 && c.Credential.VerificationStatus == "verified" && c.Credential.VerifiedTransportGeneration == c.TransportGeneration && c.Credential.CoverageRevision >= 0 && (c.Adapter == entity.AdapterNative && c.APIVersion == nil && c.Credential.DeploymentIdentity == "" || c.Adapter == entity.AdapterAzureOpenAIClassic && c.APIVersion != nil && runtimeRouteDigest.MatchString(c.Credential.DeploymentIdentity))
}

func gatewayReservationSQLResult(m *GatewayReservationManifest) *GatewayResult {
	r := &GatewayResult{ModelID: m.Model.ID, Protocol: m.Protocol, quotaRequest: quotaRequest{Supported: m.Native.Supported, MaxOutput: m.Native.MaximumOutput, CacheRead: m.Native.CacheRead, CacheWrite: m.Native.CacheWrite}, attemptEvidence: map[string]gatewayAttemptBoundEvidence{}}
	for _, occurrence := range m.Native.Media {
		if occurrence.Kind == gatewayAttachmentImage {
			r.ImageInputs++
		} else {
			r.PDFInputs++
		}
	}
	for _, c := range m.Candidates {
		r.attemptEvidence[gatewayAttemptKey(routeattempt.Attempt{TargetID: c.Binding.ID, ConnectionID: c.Connection.ID, CredentialID: c.Credential.Identity.ID, Protocol: m.Protocol})] = c.BoundEvidence
	}
	return r
}

type gatewayReservationSQLReader struct {
	tx            *gorm.DB
	queries, rows int
}

func (r *gatewayReservationSQLReader) query() error {
	r.queries++
	if r.queries > gatewayReservationSQLMaxQueries {
		return errGatewayReservationSQLConflict
	}
	return r.tx.Statement.Context.Err()
}

func (r *gatewayReservationSQLReader) count(n int) error {
	r.rows += n
	if r.rows > gatewayReservationSQLMaxRows {
		return errGatewayReservationSQLConflict
	}
	return nil
}

// Every query is finite and uses a new GORM statement on the SAME transaction.
// No administrative service helper or dialect branch is used.
func gatewayReservationSQLRows[T any](r *gatewayReservationSQLReader, column string, ids []string, selectColumns ...string) ([]T, error) {
	ids = slices.Clone(ids)
	sort.Strings(ids)
	ids = slices.Compact(ids)
	result := []T{}
	for start := 0; start < len(ids); start += gatewayReservationSQLChunk {
		chunk := ids[start:min(start+gatewayReservationSQLChunk, len(ids))]
		if err := r.query(); err != nil {
			return nil, err
		}
		var rows []T
		q := modelCreationDB(r.tx).Clauses(clause.Locking{Strength: "UPDATE"}).Where(memberModelsExactIDs(r.tx, column, chunk)).Order(column).Limit(len(chunk) + 1)
		if len(selectColumns) != 0 {
			q = q.Select(selectColumns)
		}
		if err := q.Find(&rows).Error; err != nil {
			return nil, gatewayReservationSQLError(err)
		}
		if len(rows) > len(chunk) {
			return nil, errGatewayReservationSQLConflict
		}
		if err := r.count(len(rows)); err != nil {
			return nil, err
		}
		result = append(result, rows...)
	}
	return result, nil
}

type gatewayReservationSQLFacts struct {
	model       entity.Model
	providers   map[string]entity.Provider
	connections map[string]entity.ProviderConnection
	credentials map[string]entity.ProviderCredential
	models      map[string]entity.ProviderModel
	bindings    map[string]entity.ModelProviderBinding
	coverage    map[[2]string]string
	access      map[[2]string]bool
	quota       *runtimeQuotaData
	pricing     *runtimePricingData
	egress      *runtimeData
}

func (r *gatewayReservationSQLReader) load(m *GatewayReservationManifest, selected []GatewayReservationCandidate) (*gatewayReservationSQLFacts, error) {
	f := &gatewayReservationSQLFacts{providers: map[string]entity.Provider{}, connections: map[string]entity.ProviderConnection{}, credentials: map[string]entity.ProviderCredential{}, models: map[string]entity.ProviderModel{}, bindings: map[string]entity.ModelProviderBinding{}, coverage: map[[2]string]string{}, access: map[[2]string]bool{}, quota: &runtimeQuotaData{Bounds: map[string]entity.ReservationBound{}, Currency: m.MaximumHold.Currency}, egress: &runtimeData{}}
	models, err := gatewayReservationSQLRows[entity.Model](r, "id", []string{m.Model.ID})
	if err != nil || len(models) != 1 || models[0].ID != m.Model.ID || !models[0].CreatedAt.Equal(m.Model.Birth) || models[0].Status != entity.ResourceActive {
		return nil, gatewayReservationSQLErrorOrConflict(err)
	}
	f.model = models[0]
	var providers, connections, credentials, pms, bindings []string
	for _, c := range selected {
		providers = append(providers, c.Provider.ID)
		connections = append(connections, c.Connection.ID)
		credentials = append(credentials, c.Credential.Identity.ID)
		pms = append(pms, c.ProviderModel.ID)
		bindings = append(bindings, c.Binding.ID)
	}
	ps, err := gatewayReservationSQLRows[entity.Provider](r, "id", providers)
	if err != nil {
		return nil, err
	}
	for _, row := range ps {
		f.providers[row.ID] = row
	}
	cs, err := gatewayReservationSQLRows[entity.ProviderConnection](r, "id", connections)
	if err != nil {
		return nil, err
	}
	for _, row := range cs {
		f.connections[row.ID] = row
	}
	// Connection locks always precede Credential locks, intersecting enable /
	// disable even though that existing path does not take governance.
	crs, err := gatewayReservationSQLRows[entity.ProviderCredential](r, "id", credentials, "id", "connection_id", "created_at", "priority", "enabled", "verification_status", "verified_transport_generation", "coverage_revision", "storage_source")
	if err != nil {
		return nil, err
	}
	for _, row := range crs {
		f.credentials[row.ID] = row
	}
	ms, err := gatewayReservationSQLRows[entity.ProviderModel](r, "id", pms)
	if err != nil {
		return nil, err
	}
	for _, row := range ms {
		f.models[row.ID] = row
	}
	bs, err := gatewayReservationSQLRows[entity.ModelProviderBinding](r, "id", bindings)
	if err != nil {
		return nil, err
	}
	for _, row := range bs {
		f.bindings[row.ID] = row
	}
	if err := r.loadCoverage(f, selected); err != nil {
		return nil, err
	}
	if m.MaximumHold.Tokens != nil || m.MaximumHold.Money != nil {
		bounds, err := gatewayReservationSQLRows[entity.ReservationBound](r, "provider_model_id", pms)
		if err != nil {
			return nil, err
		}
		for _, row := range bounds {
			f.quota.Bounds[row.ProviderModelID] = row
		}
	}
	if m.MaximumHold.Money != nil {
		if err := r.loadPricing(f, pms); err != nil {
			return nil, err
		}
	}
	if err := r.loadEgress(f, cs); err != nil {
		return nil, err
	}
	return f, nil
}

func (r *gatewayReservationSQLReader) loadCoverage(f *gatewayReservationSQLFacts, candidates []GatewayReservationCandidate) error {
	var vaultIDs []string
	for _, c := range f.credentials {
		if c.StorageSource == "vault" {
			vaultIDs = append(vaultIDs, c.ID)
		} else if c.StorageSource != "" && c.StorageSource != "inline" {
			return errGatewayReservationSQLConflict
		}
	}
	// Only immutable reference metadata is needed by deployment identity. Do not
	// call attachCredentialSources: it also derives private source-context proof.
	refs, err := gatewayReservationSQLRows[entity.CredentialVaultReference](r, "credential_id", vaultIDs)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		c := f.credentials[ref.CredentialID]
		copy := ref
		c.VaultReference = &copy
		f.credentials[c.ID] = c
	}
	for start := 0; start < len(candidates); start += gatewayReservationSQLChunk {
		chunk := candidates[start:min(start+gatewayReservationSQLChunk, len(candidates))]
		pairs := make([]clause.Expression, 0, len(chunk))
		for _, c := range chunk {
			pairs = append(pairs, clause.And(memberModelsExactIDs(r.tx, "credential_id", []string{c.Credential.Identity.ID}), memberModelsExactIDs(r.tx, "provider_model_id", []string{c.ProviderModel.ID})))
		}
		var discovered []entity.CredentialModelAccess
		var attestations []entity.CredentialDeploymentAttestation
		for _, target := range []any{&discovered, &attestations} {
			if err := r.query(); err != nil {
				return err
			}
			if err := modelCreationDB(r.tx).Clauses(clause.Locking{Strength: "UPDATE"}).Where(clause.Or(pairs...)).Order("credential_id,provider_model_id").Limit(len(chunk) + 1).Find(target).Error; err != nil {
				return gatewayReservationSQLError(err)
			}
		}
		if len(discovered) > len(chunk) || len(attestations) > len(chunk) {
			return errGatewayReservationSQLConflict
		}
		if err := r.count(len(discovered) + len(attestations)); err != nil {
			return err
		}
		for _, row := range attestations {
			f.coverage[[2]string{row.CredentialID, row.ProviderModelID}] = row.Identity
		}
		cs := make([]entity.ProviderCredential, 0, len(f.credentials))
		for _, c := range f.credentials {
			cs = append(cs, c)
		}
		connections := make([]entity.ProviderConnection, 0, len(f.connections))
		for _, c := range f.connections {
			connections = append(connections, c)
		}
		models := make([]entity.ProviderModel, 0, len(f.models))
		for _, pm := range f.models {
			models = append(models, pm)
		}
		for _, row := range deploymentCoverageProjection(cs, connections, models, discovered, attestations) {
			f.access[[2]string{row.CredentialID, row.ProviderModelID}] = true
		}
	}
	return nil
}

func (r *gatewayReservationSQLReader) loadPricing(f *gatewayReservationSQLFacts, pms []string) error {
	if err := r.query(); err != nil {
		return err
	}
	f.pricing = &runtimePricingData{}
	if err := modelCreationDB(r.tx).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&f.pricing.Setting, 1).Error; err != nil {
		return gatewayReservationSQLError(err)
	}
	if err := r.count(1); err != nil {
		return err
	}
	if err := r.query(); err != nil {
		return err
	}
	// pricing.Currency has seven supported values. Eight is an overflow probe,
	// not permission to omit an unused exchange-rate constituent.
	if err := modelCreationDB(r.tx).Clauses(clause.Locking{Strength: "UPDATE"}).Order("currency").Limit(8).Find(&f.pricing.FX).Error; err != nil {
		return gatewayReservationSQLError(err)
	}
	if len(f.pricing.FX) > 7 {
		return errGatewayReservationSQLConflict
	}
	if err := r.count(len(f.pricing.FX)); err != nil {
		return err
	}
	prices, err := gatewayReservationSQLRows[entity.ModelPrice](r, "provider_model_id", pms)
	if err != nil {
		return err
	}
	f.pricing.Prices = prices
	// Price rates are one-to-many, so use explicit per-parent * MaxRates + 1
	// bounds rather than the one-row-per-ID loader.
	var priceIDs []string
	for _, row := range prices {
		priceIDs = append(priceIDs, row.ID)
	}
	sort.Strings(priceIDs)
	for start := 0; start < len(priceIDs); start += gatewayReservationSQLChunk {
		chunk := priceIDs[start:min(start+gatewayReservationSQLChunk, len(priceIDs))]
		if err := r.query(); err != nil {
			return err
		}
		var rows []entity.PriceRate
		if err := modelCreationDB(r.tx).Clauses(clause.Locking{Strength: "UPDATE"}).Where(memberModelsExactIDs(r.tx, "model_price_id", chunk)).Order("id").Limit(len(chunk)*pricing.MaxRates + 1).Find(&rows).Error; err != nil {
			return gatewayReservationSQLError(err)
		}
		if len(rows) > len(chunk)*pricing.MaxRates {
			return errGatewayReservationSQLConflict
		}
		if err := r.count(len(rows)); err != nil {
			return err
		}
		f.pricing.Rates = append(f.pricing.Rates, rows...)
	}
	slices.SortFunc(f.pricing.Prices, func(a, b entity.ModelPrice) int { return compareReservationSQLID(a.ID, b.ID) })
	slices.SortFunc(f.pricing.Rates, func(a, b entity.PriceRate) int { return compareReservationSQLID(a.ID, b.ID) })
	return nil
}

func compareReservationSQLID(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func (r *gatewayReservationSQLReader) loadEgress(f *gatewayReservationSQLFacts, connections []entity.ProviderConnection) error {
	needsDefault := false
	for _, c := range connections {
		if c.EgressMode == "" || c.EgressMode == "default" {
			needsDefault = true
		}
	}
	if needsDefault {
		if err := r.query(); err != nil {
			return err
		}
		if err := modelCreationDB(r.tx).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&f.egress.EgressSetting, 1).Error; err != nil {
			return gatewayReservationSQLError(err)
		}
		if err := r.count(1); err != nil {
			return err
		}
	}
	var ids []string
	for _, c := range connections {
		selected := c.EgressID
		if c.EgressMode == "" || c.EgressMode == "default" {
			selected = f.egress.EgressSetting.DefaultEgressID
		}
		if selected != nil {
			ids = append(ids, *selected)
		}
	}
	rows, err := gatewayReservationSQLRows[entity.Egress](r, "id", ids)
	if err != nil {
		return err
	}
	f.egress.Egresses = rows
	return nil
}

func gatewayReservationSQLCurrent(m *GatewayReservationManifest, captured GatewayReservationCandidate, f *gatewayReservationSQLFacts) (GatewayReservationCandidate, error) {
	p, c, cr, pm, b := f.providers[captured.Provider.ID], f.connections[captured.Connection.ID], f.credentials[captured.Credential.Identity.ID], f.models[captured.ProviderModel.ID], f.bindings[captured.Binding.ID]
	if p.ID != captured.Provider.ID || c.ID != captured.Connection.ID || cr.ID != captured.Credential.Identity.ID || pm.ID != captured.ProviderModel.ID || b.ID != captured.Binding.ID || b.ModelID != m.Model.ID || b.ProviderModelID != pm.ID || pm.ConnectionID != c.ID || cr.ConnectionID != c.ID || c.ProviderID != p.ID || !f.access[[2]string{cr.ID, pm.ID}] {
		return GatewayReservationCandidate{}, errGatewayReservationSQLConflict
	}
	_, egress, usable := runtimeEgressSelection(f.egress, c)
	if !usable {
		return GatewayReservationCandidate{}, errGatewayReservationSQLConflict
	}
	route := gatewayRoute{ModelID: f.model.ID, ModelBirth: f.model.CreatedAt, BindingID: b.ID, BindingBirth: b.CreatedAt, Weight: b.Weight, ProviderID: p.ID, ProviderBirth: p.CreatedAt, ProviderEnabled: p.Enabled, ProviderRevision: p.ETag, ConnectionID: c.ID, ConnectionBirth: c.CreatedAt, ConnectionEnabled: c.Enabled, ConnectionRevision: c.ETag, ConnectionTransportGeneration: c.TransportGeneration, Protocol: c.Protocol, Adapter: entity.ConnectionAdapter(c), APIVersion: c.APIVersion, BaseURL: c.BaseURL, EgressRevision: egress, ProviderModelID: pm.ID, ProviderModelBirth: pm.CreatedAt, ProviderModelRevision: pm.ETag, UpstreamName: pm.UpstreamName, Disabled: pm.Disabled, SupportsImageInput: pm.SupportsImageInput, SupportsPDFInput: pm.SupportsPDFInput, CapabilityTransportGeneration: pm.CapabilityTransportGeneration, CapabilitiesTransportCurrent: capabilityTransportCurrent(pm, c), CredentialID: cr.ID, CredentialBirth: cr.CreatedAt, SnapshotID: m.SnapshotID}
	if m.MaximumHold.Money != nil {
		route.PriceBasis = runtimePriceBasis(f.pricing, pm.ID, c.Protocol)
	}
	candidate := gatewayAttemptCandidate{attempt: routeattempt.Attempt{TargetID: b.ID, ConnectionID: c.ID, CredentialID: cr.ID, Protocol: m.Protocol}, route: route, priority: cr.Priority, reservationCredential: GatewayReservationCredential{Identity: GatewayReservationIdentity{cr.ID, cr.CreatedAt.UTC()}, Priority: cr.Priority, Enabled: cr.Enabled, VerificationStatus: cr.VerificationStatus, VerifiedTransportGeneration: cr.VerifiedTransportGeneration, CoverageRevision: cr.CoverageRevision, DeploymentIdentity: f.coverage[[2]string{cr.ID, pm.ID}]}}
	return gatewayReservationCandidate(candidate, &gatewayAttemptPlan{modelID: m.Model.ID, protocol: m.Protocol, snapshotID: m.SnapshotID}, gatewayReservationSQLResult(m), f.quota, m.MaximumHold.Tokens != nil, m.MaximumHold.Money != nil)
}

func gatewayReservationSQLError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errGatewayReservationSQLConflict
	}
	return err
}
func gatewayReservationSQLErrorOrConflict(err error) error {
	if err == nil {
		return errGatewayReservationSQLConflict
	}
	return gatewayReservationSQLError(err)
}
