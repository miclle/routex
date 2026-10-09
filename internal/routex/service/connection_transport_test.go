package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

const transportTestA = "rev_01j0000000000000000000000a"
const transportTestB = "rev_01j0000000000000000000000b"

func transportProofFixture() (entity.User, entity.Provider, entity.ProviderConnection, entity.ProviderCredential, entity.ProviderModel, entity.ReservationBound) {
	a, p, c := connectionMetadataTestRows()
	c.TransportGeneration = "0"
	c.Enabled = false
	cred := entity.ProviderCredential{ID: "crd_transport", ConnectionID: c.ID, CreatedAt: c.CreatedAt, Enabled: true, VerificationStatus: "verified", VerifiedTransportGeneration: "0"}
	pm := entity.ProviderModel{ID: "pmd_transport", ConnectionID: c.ID, CreatedAt: c.CreatedAt, ETag: "0", CapabilityTransportGeneration: "0", SupportsImageInput: true}
	b := entity.ReservationBound{ProviderModelID: pm.ID, Protocol: c.Protocol, ETag: "bnd_retained", TransportGeneration: "0", MaxInputTokens: 100, MaxOutputTokens: 20, Evidence: "Recorded native capacity"}
	return a, p, c, cred, pm, b
}
func TestTransportGenerationCanonicalAndExact(t *testing.T) {
	for _, v := range []string{"0", transportTestA, transportTestB} {
		if !validTransportGeneration(v) {
			t.Fatalf("valid generation rejected %q", v)
		}
	}
	for _, v := range []string{"", "00", "rev_one", strings.ToUpper(transportTestA), "rev_81j00000000000000000000000a", transportTestA + " ", " 0"} {
		if validTransportGeneration(v) {
			t.Fatalf("unknown generation admitted %q", v)
		}
	}
}
func TestTransportABAExpiresRecordedProofWithoutMutatingFacts(t *testing.T) {
	_, _, c, cred, pm, b := transportProofFixture()
	originalCred, originalPM, originalBound := cred, pm, b
	if !verifiedTransportCurrent(cred, c) || !capabilityTransportCurrent(pm, c) || !capacityTransportCurrent(b, pm, c) {
		t.Fatal("legacy immutable context rejected")
	}
	originalURL := c.BaseURL
	c.BaseURL = "https://other.invalid/v1"
	c.TransportGeneration = transportTestA
	if verifiedTransportCurrent(cred, c) || capabilityTransportCurrent(pm, c) || capacityTransportCurrent(b, pm, c) {
		t.Fatal("old proof admitted changed endpoint")
	}
	c.BaseURL = originalURL
	c.TransportGeneration = transportTestB
	if verifiedTransportCurrent(cred, c) || capabilityTransportCurrent(pm, c) || capacityTransportCurrent(b, pm, c) {
		t.Fatal("URL ABA revived old proof")
	}
	if cred != originalCred || pm != originalPM || b != originalBound {
		t.Fatal("reading freshness rewrote configured facts")
	}
	cred.VerifiedTransportGeneration = transportTestB
	pm.CapabilityTransportGeneration = transportTestB
	b.TransportGeneration = transportTestB
	if !verifiedTransportCurrent(cred, c) || !capabilityTransportCurrent(pm, c) || !capacityTransportCurrent(b, pm, c) {
		t.Fatal("explicit current stamps rejected")
	}
	cred.ConnectionID = "con_other"
	pm.ConnectionID = "con_other"
	if verifiedTransportCurrent(cred, c) || capabilityTransportCurrent(pm, c) || capacityTransportCurrent(b, pm, c) {
		t.Fatal("borrowed parent admitted")
	}
}
func TestTransportMetadataStrictCompleteTupleDecoder(t *testing.T) {
	valid := `{"name":"A","reason":"Reviewed destination","transport":{"base_url":"https://example.invalid/v1","protocol":"openai_chat","adapter":"native","api_version":null}}`
	var in ConnectionMetadataInput
	if json.Unmarshal([]byte(valid), &in) != nil || in.Transport == nil {
		t.Fatal("complete tuple rejected")
	}
	for _, raw := range []string{strings.Replace(valid, `,"api_version":null`, "", 1), strings.Replace(valid, `"adapter":"native"`, `"adapter":"native","adapter":"native"`, 1), strings.Replace(valid, `"api_version":null`, `"api_version":null,"secret":"hidden"`, 1), strings.Replace(valid, `"transport":{`, `"transport":null,"unused":{`, 1), strings.Replace(valid, `"base_url":"https://example.invalid/v1"`, `"base_url":null`, 1), valid + " {}"} {
		if json.Unmarshal([]byte(raw), &in) == nil {
			t.Fatal("ambiguous or incomplete tuple accepted")
		}
	}
}
func TestTransportCommittedTargetSeparatesOriginalAdmissionAndConfirmation(t *testing.T) {
	a, p, c, _, _, _ := transportProofFixture()
	record, e := connectionMetadataRecord(a, p, c, true, false)
	if e != nil {
		t.Fatal(e)
	}
	original := record.ETag
	c.BaseURL = "https://changed.invalid/v1"
	c.TransportGeneration = transportTestA
	c.ETag = transportTestA
	target := connectionTransportTarget{a, p, c}
	current, e := connectionMetadataRecord(a, p, c, true, false)
	if e != nil {
		t.Fatal(e)
	}
	if current.ETag == original || !target.matches(a, p, c) {
		t.Fatal("committed proof failed own exact new target")
	}
	// A later same-tuple revision is a distinct operation; URL equality is insufficient.
	c.ETag = transportTestB
	if target.matches(a, p, c) {
		t.Fatal("later same tuple revision accepted")
	}
	c.ETag = transportTestA
	c.TransportGeneration = transportTestB
	if target.matches(a, p, c) {
		t.Fatal("generation ABA accepted")
	}
	c.TransportGeneration = transportTestA
	a.CreatedAt = a.CreatedAt.Add(time.Microsecond)
	if target.matches(a, p, c) {
		t.Fatal("reborn actor accepted")
	}
}
func TestCapabilityAndCapacityReviewBindCurrentTransportAndActor(t *testing.T) {
	a, p, c, _, pm, b := transportProofFixture()
	review := capabilityReviewETag(a, p, c, pm)
	bound := reservationBoundReview(a, p, c, pm, b)
	if !validMemberRoleDigest(review) || !validMemberRoleDigest(bound.ETag) || bound.Revision != b.ETag || !bound.TransportCurrent {
		t.Fatal("invalid reviewed projection")
	}
	c.TransportGeneration = transportTestA
	if capabilityReviewETag(a, p, c, pm) == review || reservationBoundReview(a, p, c, pm, b).ETag == bound.ETag || reservationBoundReview(a, p, c, pm, b).TransportCurrent {
		t.Fatal("old review carried to new generation")
	}
	c.TransportGeneration = "0"
	a.CreatedAt = a.CreatedAt.Add(time.Microsecond)
	if capabilityReviewETag(a, p, c, pm) == review || reservationBoundReview(a, p, c, pm, b).ETag == bound.ETag {
		t.Fatal("reborn actor review reused")
	}
	for _, input := range []ProviderModelUpdate{{SupportsImageInput: new(bool)}, {SupportsPDFInput: new(bool)}, {CapabilityReviewETag: &review}, {Enabled: new(bool), CapabilityReviewETag: &review}} {
		if validProviderModelUpdate(input) {
			t.Fatal("implicit or partial capability review accepted")
		}
	}
	if !validProviderModelUpdate(ProviderModelUpdate{Enabled: new(bool)}) || !validProviderModelUpdate(ProviderModelUpdate{SupportsImageInput: new(bool), SupportsPDFInput: new(bool), CapabilityReviewETag: &review}) {
		t.Fatal("valid operation shape rejected")
	}
}
func TestTransportEvidenceExcludedFromHistoricalEntityJSON(t *testing.T) {
	_, _, c, cred, pm, b := transportProofFixture()
	for _, v := range []any{c, cred, pm, b} {
		raw, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(raw), "TransportGeneration") || strings.Contains(string(raw), "transport_generation") || strings.Contains(string(raw), "TransportCurrent") || strings.Contains(string(raw), "CapabilityReview") {
			t.Fatal("new proof rewrote historical entity serialization")
		}
	}
	data := &runtimeData{Connections: []entity.ProviderConnection{c}, Credentials: []entity.ProviderCredential{cred}, ProviderModels: []entity.ProviderModel{pm}, Quota: &runtimeQuotaData{Bounds: map[string]entity.ReservationBound{pm.ID: b}}}
	if len(runtimeTransportDigestProof(data)) != 0 {
		t.Fatal("legacy0 adds a new serialized proof branch")
	}
	c.TransportGeneration = transportTestA
	cred.VerifiedTransportGeneration = transportTestA
	pm.CapabilityTransportGeneration = transportTestA
	b.TransportGeneration = transportTestA
	data.Connections[0] = c
	data.Credentials[0] = cred
	data.ProviderModels[0] = pm
	data.Quota.Bounds[pm.ID] = b
	if len(runtimeTransportDigestProof(data)) != 4 {
		t.Fatal("live digest omits a generation owner")
	}
}
func TestRuntimeTransportOldSnapshotsAndPublicationFailureRemainClosed(t *testing.T) {
	s, data, _ := runtimeFixture(t, "https://example.invalid/v1")
	candidate := s.runtime.routes.Load().Models["mdl_one"][0]
	route := candidate.Route
	route.SnapshotID = "cfg_test"
	if !s.runtimeRouteReadyForDiscovery(s.runtime.auth.Load(), candidate) {
		t.Fatal("current route unavailable")
	}
	data.Connections[0].TransportGeneration = transportTestA
	data.Connections[0].BaseURL = "https://changed.invalid/v1"
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	if s.runtimeRouteReadyForDiscovery(s.runtime.auth.Load(), candidate) || s.runtimeConnectionAllowed(s.runtime.auth.Load(), route) {
		t.Fatal("old route advertised or admitted after current authority changed")
	}
	if release, ok := s.pinConnectionDispatch(&route); ok {
		release()
		t.Fatal("queued old dispatch survived transport publication failure")
	}
	// Restore current transport proof; changing a capability raw revision still
	// fences captured old true flags when route rebuilding fails.
	data.Connections[0].TransportGeneration = "0"
	data.Connections[0].BaseURL = route.BaseURL
	data.ProviderModels[0].ETag = "pms_changed"
	data.ProviderModels[0].SupportsImageInput = false
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	if s.runtimeRouteReadyForDiscovery(s.runtime.auth.Load(), candidate) {
		t.Fatal("old capability revision advertised")
	}
	if release, ok := s.pinConnectionDispatch(&route); ok {
		release()
		t.Fatal("old capability revision admitted")
	}
}

func TestAzureTransportIdentityLegacyAndABA(t *testing.T) {
	c, connection, pm := azureIdentityFixture()
	original := deploymentAttestationIdentity(c, connection, pm)
	legacy := connectionMetadataHash(struct {
		Version, CredentialID, ConnectionID, ProviderID, ModelID, Source, Adapter, Endpoint, APIVersion, Deployment string
		CredentialBirth, ConnectionBirth, ModelBirth                                                                time.Time
		Reference                                                                                                   *entity.CredentialVaultReference
	}{"azure.deployment.identity.v1", c.ID, connection.ID, connection.ProviderID, pm.ID, "inline", entity.ConnectionAdapter(connection), connection.BaseURL, *connection.APIVersion, pm.UpstreamName, c.CreatedAt.UTC(), connection.CreatedAt.UTC(), pm.CreatedAt.UTC(), nil})
	if original != legacy || original == "" {
		t.Fatal("legacy0 changed Azure v1 identity")
	}
	attestation := []entity.CredentialDeploymentAttestation{{CredentialID: c.ID, ProviderModelID: pm.ID, Identity: original}}
	if len(deploymentCoverageProjection([]entity.ProviderCredential{c}, []entity.ProviderConnection{connection}, []entity.ProviderModel{pm}, nil, attestation)) != 1 {
		t.Fatal("original legacy declaration rejected")
	}
	connection.TransportGeneration = transportTestA
	c.VerifiedTransportGeneration = transportTestA
	first := deploymentAttestationIdentity(c, connection, pm)
	if first == original || first == "" {
		t.Fatal("edited Azure identity reused legacy proof")
	}
	if len(deploymentCoverageProjection([]entity.ProviderCredential{c}, []entity.ProviderConnection{connection}, []entity.ProviderModel{pm}, nil, attestation)) != 0 {
		t.Fatal("legacy Azure coverage survived edit")
	}
	attestation[0].Identity = first
	connection.TransportGeneration = transportTestB
	c.VerifiedTransportGeneration = transportTestB
	if deploymentAttestationIdentity(c, connection, pm) == first || len(deploymentCoverageProjection([]entity.ProviderCredential{c}, []entity.ProviderConnection{connection}, []entity.ProviderModel{pm}, nil, attestation)) != 0 {
		t.Fatal("same endpoint Azure ABA revived declaration")
	}
	attestation[0].Identity = deploymentAttestationIdentity(c, connection, pm)
	if len(deploymentCoverageProjection([]entity.ProviderCredential{c}, []entity.ProviderConnection{connection}, []entity.ProviderModel{pm}, nil, attestation)) != 1 {
		t.Fatal("explicit current deployment declaration rejected")
	}
}
func TestNameStatusAndEgressDoNotAdvanceTransportProof(t *testing.T) {
	_, _, connection, c, pm, b := transportProofFixture()
	connection.TransportGeneration = transportTestA
	c.VerifiedTransportGeneration = transportTestA
	pm.CapabilityTransportGeneration = transportTestA
	b.TransportGeneration = transportTestA
	for _, change := range []func(*entity.ProviderConnection){func(row *entity.ProviderConnection) { row.Name = "Renamed"; row.ETag = transportTestB }, func(row *entity.ProviderConnection) { row.Enabled = !row.Enabled; row.ETag = transportTestB }, func(row *entity.ProviderConnection) {
		row.EgressMode = "direct"
		row.EgressID = nil
		row.ETag = transportTestB
	}} {
		row := connection
		change(&row)
		if !verifiedTransportCurrent(c, row) || !capabilityTransportCurrent(pm, row) || !capacityTransportCurrent(b, pm, row) {
			t.Fatal("nontransport metadata invalidated immutable transport proof")
		}
	}
}

func TestModelWeightCurrentTransportEligibilityAndRollbackProof(t *testing.T) {
	s, st, _ := modelWeightActualBuilderFixture(t, true, false)
	before := st.currentEligibilityHash()
	if !st.executable(st.Rows) || !s.modelWeightRuntimeApplied(st) {
		t.Fatal("current legacy topology rejected")
	}
	st.Connections[0].TransportGeneration = transportTestA
	if st.executable(st.Rows) || s.modelWeightRuntimeApplied(st) || st.currentEligibilityHash() == before {
		t.Fatal("current or rollback plan reused old transport capabilities")
	}
	st.Models[0].CapabilityTransportGeneration = transportTestA
	supply := st.Supplies[st.Models[0].ID]
	supply.Credentials[0].TransportCurrent = false
	supply.Credentials[0].TransportGeneration = "0"
	supply.Covered = modelCreationReady(supply.Credentials, st.Models[0].ID)
	st.Supplies[st.Models[0].ID] = supply
	if st.executable(st.Rows) {
		t.Fatal("positive plan accepted stale verification")
	}
	supply.Credentials[0].TransportCurrent = true
	supply.Credentials[0].TransportGeneration = transportTestA
	supply.Covered = modelCreationReady(supply.Credentials, st.Models[0].ID)
	st.Supplies[st.Models[0].ID] = supply
	if !st.executable(st.Rows) {
		t.Fatal("independent current capability/verification proof not executable")
	}
	if s.modelWeightRuntimeApplied(st) {
		t.Fatal("configured current plan claimed older publication")
	}
}

func TestTransportTypedAuditsRejectUnboundedOrSecretProjection(t *testing.T) {
	_, _, c, _, pm, _ := transportProofFixture()
	details := connectionTransportAudit{"connection.transport.v1", connectionTransportAuditValues{c.Name, connectionTransportTuple(c), "0"}, connectionTransportAuditValues{c.Name, ConnectionTransportInput{BaseURL: "https://changed.invalid/v1", Protocol: c.Protocol, Adapter: entity.ConnectionAdapter(c)}, transportTestA}, "Reviewed destination"}
	raw, err := json.Marshal(details)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	row := entity.AuditEvent{ActorID: "usr_review", ResourceType: "connection", ResourceID: c.ID, DetailsJSON: &text}
	if _, ok := connectionTransportAuditProjection(row); !ok {
		t.Fatal("bounded typed transport audit rejected")
	}
	for _, bad := range []string{strings.Replace(text, `"reason":"Reviewed destination"`, `"reason":"Reviewed destination","secret":"hidden"`, 1), strings.Replace(text, `"generation":"0"`, `"generation":""`, 1), strings.Replace(text, `"api_version":null`, `"api_version":null,"secret":"hidden"`, 1)} {
		row.DetailsJSON = &bad
		if _, ok := connectionTransportAuditProjection(row); ok {
			t.Fatal("unsafe transport audit projected")
		}
	}
	before := capabilityTransportAuditValue(pm)
	after := before
	after.Generation = transportTestA
	raw, err = json.Marshal(capabilityTransportAudit{"provider_model.capability.v1", before, after})
	if err != nil {
		t.Fatal(err)
	}
	text = string(raw)
	row = entity.AuditEvent{ActorID: "usr_review", ResourceType: "provider_model", ResourceID: pm.ID, DetailsJSON: &text}
	if _, ok := capabilityTransportAuditProjection(row); !ok {
		t.Fatal("same-pair fresh stamp audit rejected")
	}
	for _, bad := range []string{strings.Replace(text, `"image":true`, `"image":null`, 1), strings.Replace(text, `"pdf":false`, `"pdf":false,"secret":"hidden"`, 1), strings.Replace(text, transportTestA, "unknown", 1)} {
		row.DetailsJSON = &bad
		if _, ok := capabilityTransportAuditProjection(row); ok {
			t.Fatal("unsafe capability audit projected")
		}
	}
}
