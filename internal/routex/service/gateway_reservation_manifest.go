package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"slices"
	"sort"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/routeattempt"
)

const (
	gatewayReservationManifestVersion  = 1
	gatewayReservationManifestMaxBytes = 4 << 20
	gatewayReservationMaxTargets       = 128
	gatewayReservationMaxCredentials   = 32
)

var errGatewayReservationManifest = errors.New("invalid captured gateway reservation")

// GatewayReservationManifest is detached, secret-free captured evidence, not
// current SQL authorization, a dispatch permit, or a runtime health assertion.
// MaximumHold belongs to the logical request and must never shrink at retry.
// This first source slice deliberately does not wire capture into admission.
type GatewayReservationManifest struct {
	Version         int                           `json:"version"`
	RequestID       string                        `json:"request_id"`
	Model           GatewayReservationIdentity    `json:"model"`
	SnapshotID      string                        `json:"snapshot_id"`
	Protocol        string                        `json:"protocol"`
	MaximumAttempts int                           `json:"maximum_attempts"`
	Native          GatewayReservationNative      `json:"native"`
	Candidates      []GatewayReservationCandidate `json:"candidates"`
	MaximumHold     eventqueue.QuotaBound         `json:"maximum_hold"`
}

type GatewayReservationIdentity struct {
	ID    string    `json:"id"`
	Birth time.Time `json:"birth"`
}

type GatewayReservationCredential struct {
	Identity                    GatewayReservationIdentity `json:"identity"`
	Priority                    int                        `json:"priority"`
	Enabled                     bool                       `json:"enabled"`
	VerificationStatus          string                     `json:"verification_status"`
	VerifiedTransportGeneration string                     `json:"verified_transport_generation"`
	CoverageRevision            int64                      `json:"coverage_revision"`
	// DeploymentIdentity is the retained operator-attestation identity, never
	// a Vault descriptor, ciphertext, or credential/source-holder secret proof.
	// Empty is the native discovery meaning, not an Azure attestation.
	DeploymentIdentity string `json:"deployment_identity"`
}

type GatewayReservationCandidate struct {
	Binding             GatewayReservationIdentity `json:"binding"`
	Weight              int                        `json:"weight"`
	Provider            GatewayReservationIdentity `json:"provider"`
	ProviderEnabled     bool                       `json:"provider_enabled"`
	ProviderRevision    string                     `json:"provider_revision"`
	Connection          GatewayReservationIdentity `json:"connection"`
	ConnectionEnabled   bool                       `json:"connection_enabled"`
	ConnectionRevision  string                     `json:"connection_revision"`
	TransportGeneration string                     `json:"transport_generation"`
	Adapter             string                     `json:"adapter"`
	APIVersion          *string                    `json:"api_version"`
	// TransportIdentity binds the exact normalized transport/deployment tuple
	// without retaining its URL or upstream name. EgressRevision is the existing
	// server-recomputable revision; its secret-bearing inputs are never copied.
	TransportIdentity             string                       `json:"transport_identity"`
	EgressRevision                string                       `json:"egress_revision"`
	ProviderModel                 GatewayReservationIdentity   `json:"provider_model"`
	ProviderModelRevision         string                       `json:"provider_model_revision"`
	Disabled                      bool                         `json:"disabled"`
	CapabilityTransportGeneration string                       `json:"capability_transport_generation"`
	ImageInput                    bool                         `json:"image_input"`
	PDFInput                      bool                         `json:"pdf_input"`
	Credential                    GatewayReservationCredential `json:"credential"`
	Capacity                      *GatewayReservationCapacity  `json:"capacity"`
	PriceBasis                    *CallPriceBasis              `json:"price_basis"`
	BoundEvidence                 gatewayAttemptBoundEvidence  `json:"bound_evidence"`
}

type GatewayReservationCapacity struct {
	Protocol            string `json:"protocol"`
	TransportGeneration string `json:"transport_generation"`
	Revision            string `json:"revision"`
	MaximumInput        int64  `json:"maximum_input"`
	MaximumOutput       int64  `json:"maximum_output"`
}

// Native retains classifier-owned structure only. No input text, tool schemas,
// arbitrary field names, attachment identifiers, locations, bytes or URLs survive.
// Input tokens come from capacity attestations, never file/token estimates.
type GatewayReservationNative struct {
	ClassifierVersion        int                       `json:"classifier_version"`
	InputContainer           string                    `json:"input_container"`
	InputShape               string                    `json:"input_shape"`
	Supported                bool                      `json:"supported"`
	MaximumOutput            int64                     `json:"maximum_output"`
	CacheRead                bool                      `json:"cache_read"`
	CacheWrite               bool                      `json:"cache_write"`
	PricingDimensions        []string                  `json:"pricing_dimensions"`
	MessagesHeadersSupported *bool                     `json:"messages_headers_supported"`
	Media                    []GatewayReservationMedia `json:"media"`
}

type GatewayReservationMedia struct {
	Kind  string `json:"kind"`
	Shape string `json:"shape"`
}

// captureGatewayReservationManifest accepts the original parsed payload before
// attachment rewriting and the final admission-prepared detached plan/result. It never
// reads current catalog/runtime/SQL state, resolves a secret/object, or filters
// an original candidate out. Unknown births or inconsistent captures fail closed.
func captureGatewayReservationManifest(requestID string, plan *gatewayAttemptPlan, result *GatewayResult, data *runtimeQuotaData, payload map[string]json.RawMessage, options ...gatewayNativeOptions) (*GatewayReservationManifest, error) {
	if plan == nil || result == nil || data == nil || requestID == "" || len(requestID) > 128 || !safeCallID.MatchString(requestID) || plan.modelID != result.ModelID || plan.protocol != result.NativeProtocol() || !entity.SupportedNativeProtocol(plan.protocol) || !runtimeSnapshotID.MatchString(plan.snapshotID) || !modelCreationID(plan.modelID, "mdl") || len(plan.candidates) == 0 || len(plan.candidates) > gatewayReservationMaxTargets*gatewayReservationMaxCredentials || len(plan.allowed) != len(plan.candidates) || len(result.admissionAllowed) != len(plan.candidates) || len(result.attemptEvidence) != len(plan.candidates) {
		return nil, errGatewayReservationManifest
	}
	native, classification, err := gatewayReservationNativeShape(plan.protocol, payload, options...)
	if err != nil || classification != result.quotaRequest || (len(native.PricingDimensions) != 0) != result.PricingUnsupported || !slices.Equal(native.PricingDimensions, result.PricingDimensions) {
		return nil, errGatewayReservationManifest
	}
	var images, pdfs int64
	for _, media := range native.Media {
		if media.Kind == gatewayAttachmentImage {
			images++
		} else {
			pdfs++
		}
	}
	if images != result.ImageInputs || pdfs != result.PDFInputs {
		return nil, errGatewayReservationManifest
	}
	tokens, money := gatewayQuotaDimensions(result.admissionQuota)
	if (tokens || money) && !classification.Supported || money && result.PricingUnsupported {
		return nil, errGatewayReservationManifest
	}
	manifest := &GatewayReservationManifest{Version: gatewayReservationManifestVersion, RequestID: requestID, SnapshotID: plan.snapshotID, Protocol: plan.protocol, MaximumAttempts: routeattempt.MaxAttempts, Native: native, MaximumHold: cloneGatewayReservationBound(result.quotaBound)}
	targets := map[string]GatewayReservationCandidate{}
	pairs := map[string]bool{}
	credentialOwners := map[string]string{}
	counts := map[string]int{}
	evidence := map[string]gatewayAttemptBoundEvidence{}
	for _, candidate := range plan.candidates {
		key := gatewayAttemptKey(candidate.attempt)
		if pairs[key] || !plan.allowed[key] || !result.admissionAllowed[key] {
			return nil, errGatewayReservationManifest
		}
		pairs[key] = true
		captured, err := gatewayReservationCandidate(candidate, plan, result, data, tokens, money)
		if err != nil {
			return nil, err
		}
		birth := candidate.route.ModelBirth.UTC()
		if manifest.Model.ID == "" {
			manifest.Model = GatewayReservationIdentity{plan.modelID, birth}
		}
		if !manifest.Model.Birth.Equal(birth) {
			return nil, errGatewayReservationManifest
		}
		if previous, exists := targets[captured.Binding.ID]; exists {
			// All credentials of one target share its exact route and bound.
			previous.Credential = captured.Credential
			if !reflect.DeepEqual(previous, captured) {
				return nil, errGatewayReservationManifest
			}
		} else {
			targets[captured.Binding.ID] = captured
		}
		counts[captured.Binding.ID]++
		if len(targets) > gatewayReservationMaxTargets || counts[captured.Binding.ID] > gatewayReservationMaxCredentials {
			return nil, errGatewayReservationManifest
		}
		if owner, exists := credentialOwners[captured.Credential.Identity.ID]; exists && owner != captured.Connection.ID {
			return nil, errGatewayReservationManifest
		}
		credentialOwners[captured.Credential.Identity.ID] = captured.Connection.ID
		evidence[captured.Binding.ID] = captured.BoundEvidence
		manifest.Candidates = append(manifest.Candidates, captured)
	}
	bound, err := gatewayReservationMaximum(evidence, tokens, money, data.Currency)
	if err != nil || !reflect.DeepEqual(bound, manifest.MaximumHold) {
		return nil, errGatewayReservationManifest
	}
	sort.Slice(manifest.Candidates, func(i, j int) bool {
		a, b := manifest.Candidates[i], manifest.Candidates[j]
		if a.Binding.ID != b.Binding.ID {
			return a.Binding.ID < b.Binding.ID
		}
		return a.Credential.Identity.ID < b.Credential.Identity.ID
	})
	raw, err := json.Marshal(manifest)
	if err != nil || len(raw) > gatewayReservationManifestMaxBytes {
		return nil, errGatewayReservationManifest
	}
	return manifest, nil
}

func gatewayReservationCandidate(candidate gatewayAttemptCandidate, plan *gatewayAttemptPlan, result *GatewayResult, data *runtimeQuotaData, tokens, money bool) (GatewayReservationCandidate, error) {
	route, credential := candidate.route, candidate.reservationCredential
	out := GatewayReservationCandidate{Binding: GatewayReservationIdentity{route.BindingID, route.BindingBirth.UTC()}, Weight: route.Weight, Provider: GatewayReservationIdentity{route.ProviderID, route.ProviderBirth.UTC()}, ProviderEnabled: route.ProviderEnabled, ProviderRevision: route.ProviderRevision, Connection: GatewayReservationIdentity{route.ConnectionID, route.ConnectionBirth.UTC()}, ConnectionEnabled: route.ConnectionEnabled, ConnectionRevision: route.ConnectionRevision, TransportGeneration: route.ConnectionTransportGeneration, Adapter: route.Adapter, TransportIdentity: gatewayReservationTransportIdentity(route), EgressRevision: route.EgressRevision, ProviderModel: GatewayReservationIdentity{route.ProviderModelID, route.ProviderModelBirth.UTC()}, ProviderModelRevision: route.ProviderModelRevision, Disabled: route.Disabled, CapabilityTransportGeneration: route.CapabilityTransportGeneration, ImageInput: route.SupportsImageInput, PDFInput: route.SupportsPDFInput, Credential: credential}
	for prefix, identity := range map[string]GatewayReservationIdentity{"bnd": out.Binding, "prv": out.Provider, "con": out.Connection, "pmd": out.ProviderModel, "crd": credential.Identity} {
		if !modelCreationID(identity.ID, prefix) || !connectionMetadataBirth(identity.Birth) {
			return out, errGatewayReservationManifest
		}
	}
	if route.ModelID != plan.modelID || !connectionMetadataBirth(route.ModelBirth) || route.SnapshotID != plan.snapshotID || route.Protocol != plan.protocol || candidate.attempt != (routeattempt.Attempt{TargetID: route.BindingID, ConnectionID: route.ConnectionID, CredentialID: credential.Identity.ID, Protocol: plan.protocol}) || route.CredentialID != credential.Identity.ID || !route.CredentialBirth.Equal(credential.Identity.Birth) || candidate.priority != credential.Priority || credential.Priority < 0 || !credential.Enabled || credential.VerificationStatus != "verified" || credential.CoverageRevision < 0 || credential.VerifiedTransportGeneration != route.ConnectionTransportGeneration || !validTransportGeneration(route.ConnectionTransportGeneration) || route.CapabilityTransportGeneration != route.ConnectionTransportGeneration || !route.CapabilitiesTransportCurrent || !route.ProviderEnabled || !route.ConnectionEnabled || route.Disabled || route.Weight <= 0 || route.Weight > 100 || route.ProviderRevision == "" || route.ConnectionRevision == "" || route.ProviderModelRevision == "" || !runtimeRouteDigest.MatchString(route.EgressRevision) {
		return out, errGatewayReservationManifest
	}
	if validateConnectionAdapter(entity.ProviderConnection{BaseURL: route.BaseURL, Protocol: route.Protocol, Adapter: route.Adapter, APIVersion: route.APIVersion}, true) != nil {
		return out, errGatewayReservationManifest
	}
	if route.Adapter == entity.AdapterAzureOpenAIClassic {
		if !runtimeRouteDigest.MatchString(credential.DeploymentIdentity) {
			return out, errGatewayReservationManifest
		}
	} else if route.Adapter != entity.AdapterNative {
		return out, errGatewayReservationManifest
	} else {
		out.Credential.DeploymentIdentity = ""
	}
	if route.APIVersion != nil {
		v := *route.APIVersion
		out.APIVersion = &v
	}
	bound, usable := gatewayCandidateBoundEvidence(candidate, result, data, tokens, money)
	if !usable || !reflect.DeepEqual(bound, result.attemptEvidence[gatewayAttemptKey(candidate.attempt)]) {
		return out, errGatewayReservationManifest
	}
	out.BoundEvidence = bound
	if tokens || money {
		capacity, exists := data.Bounds[route.ProviderModelID]
		if !exists || !capacityTransportCurrent(capacity, entity.ProviderModel{ID: route.ProviderModelID, ConnectionID: route.ConnectionID, CreatedAt: route.ProviderModelBirth}, entity.ProviderConnection{ID: route.ConnectionID, CreatedAt: route.ConnectionBirth, Protocol: route.Protocol, TransportGeneration: route.ConnectionTransportGeneration}) {
			return out, errGatewayReservationManifest
		}
		out.Capacity = &GatewayReservationCapacity{capacity.Protocol, capacity.TransportGeneration, capacity.ETag, capacity.MaxInputTokens, capacity.MaxOutputTokens}
	}
	if money {
		if size := pricingBasisSize(route.PriceBasis); size < 0 || size > callPricingSnapshotLimit {
			return out, errGatewayReservationManifest
		}
		out.PriceBasis = clonePriceBasis(route.PriceBasis)
	}
	return out, nil
}

func gatewayReservationTransportIdentity(route gatewayRoute) string {
	return connectionMetadataHash(struct {
		Version      string
		Transport    ConnectionTransportInput
		UpstreamName string
	}{"gateway.reservation.transport.v1", ConnectionTransportInput{route.BaseURL, route.Protocol, route.Adapter, route.APIVersion}, route.UpstreamName})
}

func cloneGatewayReservationBound(bound eventqueue.QuotaBound) eventqueue.QuotaBound {
	if bound.Tokens != nil {
		v := *bound.Tokens
		bound.Tokens = &v
	}
	if bound.Money != nil {
		v := *bound.Money
		bound.Money = &v
	}
	return bound
}

// Reuse the existing candidate bound builder and canonical evidence digests;
// only the maximum across original targets is aggregated here, never repriced.
func gatewayReservationMaximum(byTarget map[string]gatewayAttemptBoundEvidence, tokens, money bool, currency string) (eventqueue.QuotaBound, error) {
	rows := make([]gatewayAttemptBoundEvidence, 0, len(byTarget))
	for _, row := range byTarget {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].TargetID < rows[j].TargetID })
	bound := eventqueue.QuotaBound{}
	maximumTokens := int64(0)
	maximumMoney := new(big.Rat)
	moneyText := ""
	for _, row := range rows {
		if tokens {
			maximumTokens = max(maximumTokens, row.InputTokens+row.OutputTokens)
		}
		if money {
			amount, ok := new(big.Rat).SetString(row.ReservedAmount)
			if !ok {
				return bound, errGatewayReservationManifest
			}
			if currency == "" {
				currency = row.PriceCurrency
			}
			if row.PriceCurrency != currency {
				return bound, errGatewayReservationManifest
			}
			if moneyText == "" || amount.Cmp(maximumMoney) > 0 {
				maximumMoney.Set(amount)
				moneyText = row.ReservedAmount
			}
		}
	}
	if tokens || money {
		bound.Revision = gatewayAttemptEvidenceDigest(rows, "bound")
	}
	if tokens {
		bound.Tokens = &maximumTokens
	}
	if money {
		bound.Money, bound.Currency = &moneyText, currency
		bound.PriceRevision = gatewayAttemptEvidenceDigest(rows, "price")
		bound.BasisDigest = gatewayAttemptEvidenceDigest(rows, "basis")
	}
	return bound, nil
}

func gatewayReservationNativeShape(protocol string, payload map[string]json.RawMessage, options ...gatewayNativeOptions) (GatewayReservationNative, quotaRequest, error) {
	shape := GatewayReservationNative{ClassifierVersion: 1, Media: []GatewayReservationMedia{}}
	plan, err := planGatewayAttachments(protocol, payload)
	if err != nil {
		return shape, quotaRequest{}, errGatewayReservationManifest
	}
	classification := inspectQuotaRequest(protocol, payload, plan)
	pricingPayload := payload
	if len(plan.Occurrences) != 0 {
		if detached, ok := quotaAttachmentPayload(protocol, plan); ok {
			pricingPayload = detached
		}
	}
	shape.PricingDimensions = gatewayPricingDimensions(protocol, pricingPayload)
	switch protocol {
	case entity.ProtocolOpenAIChat, entity.ProtocolAnthropicMessages:
		shape.InputContainer = "messages"
	case entity.ProtocolOpenAIResponses:
		shape.InputContainer = "input"
	case entity.ProtocolGeminiGenerateContent:
		shape.InputContainer = "contents"
	default:
		return shape, quotaRequest{}, errGatewayReservationManifest
	}
	raw := bytes.TrimSpace(payload[shape.InputContainer])
	shape.InputShape = "other"
	if len(raw) > 0 {
		switch raw[0] {
		case '[':
			shape.InputShape = "array"
		case '"':
			shape.InputShape = "string"
		case '{':
			shape.InputShape = "object"
		case 'n':
			shape.InputShape = "null"
		}
	}
	if protocol == entity.ProtocolAnthropicMessages {
		supported := len(options) == 1 && options[0].Messages.Version == "2023-06-01" && options[0].Messages.Beta == ""
		shape.MessagesHeadersSupported = &supported
		if !supported {
			classification.Supported = false
		}
	} else if len(options) > 1 {
		return shape, quotaRequest{}, errGatewayReservationManifest
	}
	for _, occurrence := range plan.Occurrences {
		shape.Media = append(shape.Media, GatewayReservationMedia{occurrence.Kind, occurrence.shape})
	}
	shape.Supported, shape.MaximumOutput, shape.CacheRead, shape.CacheWrite = classification.Supported, classification.MaxOutput, classification.CacheRead, classification.CacheWrite
	return shape, classification, nil
}
