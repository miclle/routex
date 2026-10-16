package service

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/routeattempt"
)

const sharedTestReservationSnapshot = "cfg_01arz3ndektsv4rrffq69g5fav"

// Use the real evidence and maximum builders. Positive input/output pricing
// retains its rounding margin instead of inventing a smaller reservation.
func sharedTestReservation(t *testing.T, request string, authorization sharedCapturedAuthorization, tokens, money bool, size ...int) *GatewayReservationManifest {
	t.Helper()
	m := &GatewayReservationManifest{Version: gatewayReservationManifestVersion, RequestID: request,
		Model: GatewayReservationIdentity{authorization.Model.ID, authorization.Model.CreatedAt}, SnapshotID: sharedTestReservationSnapshot,
		Protocol: entity.ProtocolOpenAIChat, MaximumAttempts: routeattempt.MaxAttempts,
		Native: GatewayReservationNative{ClassifierVersion: 1, InputContainer: "messages", InputShape: "array", Supported: true,
			MaximumOutput: 10, CacheRead: true, CacheWrite: true, Media: []GatewayReservationMedia{}},
	}
	result := gatewayReservationSQLResult(m)
	evidence := map[string]gatewayAttemptBoundEvidence{}
	count := routeattempt.MaxAttempts
	if len(size) == 1 {
		count = size[0]
	}
	for number := 1; number <= count; number++ {
		suffix := "shared_" + strconv.Itoa(number)
		identity := func(prefix string) GatewayReservationIdentity {
			return GatewayReservationIdentity{prefix + "_" + suffix, authorization.Model.CreatedAt}
		}
		c := GatewayReservationCandidate{Binding: identity("bnd"), Weight: 25, Provider: identity("prv"), ProviderEnabled: true,
			ProviderRevision: "provider_original", Connection: identity("con"), ConnectionEnabled: true, ConnectionRevision: "connection_original",
			TransportGeneration: "0", Adapter: entity.AdapterNative, TransportIdentity: strings.Repeat("a", 64), EgressRevision: strings.Repeat("b", 64),
			ProviderModel: identity("pmd"), ProviderModelRevision: "model_original", CapabilityTransportGeneration: "0",
			Credential: GatewayReservationCredential{Identity: identity("crd"), Enabled: true, VerificationStatus: "verified", VerifiedTransportGeneration: "0"}}
		quota := &runtimeQuotaData{Currency: "USD", Bounds: map[string]entity.ReservationBound{}}
		if tokens || money {
			c.Capacity = &GatewayReservationCapacity{Protocol: m.Protocol, TransportGeneration: "0", Revision: "capacity_original", MaximumInput: 90, MaximumOutput: 10}
			quota.Bounds[c.ProviderModel.ID] = entity.ReservationBound{ProviderModelID: c.ProviderModel.ID, Protocol: m.Protocol,
				TransportGeneration: "0", ETag: c.Capacity.Revision, MaxInputTokens: 90, MaxOutputTokens: 10}
		}
		if money {
			c.PriceBasis = testPriceBasis()
			c.PriceBasis.Schedule.ProviderModelID = c.ProviderModel.ID
			c.PriceBasis.Currency = pricing.FX{PlatformCurrency: "USD", Rates: map[string]string{"USD": "1"}}
			for i := range c.PriceBasis.Schedule.Rates {
				c.PriceBasis.Schedule.Rates[i].Amount = "0.000000000000000001"
			}
		}
		candidate := gatewayAttemptCandidate{attempt: routeattempt.Attempt{TargetID: c.Binding.ID, CredentialID: c.Credential.Identity.ID,
			ConnectionID: c.Connection.ID, Protocol: m.Protocol}, route: gatewayRoute{ProviderModelID: c.ProviderModel.ID, PriceBasis: c.PriceBasis}}
		var ok bool
		c.BoundEvidence, ok = gatewayCandidateBoundEvidence(candidate, result, quota, tokens, money)
		if !ok {
			t.Fatal("structurally captured test bound unavailable")
		}
		evidence[c.Binding.ID] = c.BoundEvidence
		m.Candidates = append(m.Candidates, c)
	}
	var err error
	m.MaximumHold, err = gatewayReservationMaximum(evidence, tokens, money, "USD")
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(m.Candidates, func(a, b GatewayReservationCandidate) int {
		return compareReservationSQLID(a.Binding.ID+"\x00"+a.Credential.Identity.ID, b.Binding.ID+"\x00"+b.Credential.Identity.ID)
	})
	if _, err := gatewayReservationSQLSelection(m, nil); err != nil {
		t.Fatal("complete test reservation invalid", err)
	}
	return m
}

func alignSharedTestReservation(t *testing.T, intent *sharedAdmissionIntent) {
	t.Helper()
	intent.Reservation.RequestID = intent.RequestID
	intent.Reservation.Model = GatewayReservationIdentity{intent.Authorization.Model.ID, intent.Authorization.Model.CreatedAt}
}

func TestSharedAdmissionRequiresOriginalReservationAndOwnerBirth(t *testing.T) {
	c, _, _, original := sharedTestFixture(t)
	for _, tc := range []struct {
		name string
		edit func(*sharedAdmissionRequest)
	}{
		{"missing_manifest", func(r *sharedAdmissionRequest) { r.Reservation = nil }},
		{"missing_candidates", func(r *sharedAdmissionRequest) { r.Reservation.Candidates = nil }},
		{"reduced_candidates", func(r *sharedAdmissionRequest) { r.Reservation.Candidates = r.Reservation.Candidates[:1] }},
		{"request", func(r *sharedAdmissionRequest) { r.Reservation.RequestID = "req_other" }},
		{"model", func(r *sharedAdmissionRequest) { r.Reservation.Model.ID = "mdl_other" }},
		{"model_birth", func(r *sharedAdmissionRequest) {
			r.Reservation.Model.Birth = r.Reservation.Model.Birth.Add(time.Microsecond)
		}},
		{"protocol", func(r *sharedAdmissionRequest) { r.Reservation.Protocol = entity.ProtocolOpenAIResponses }},
		{"missing_snapshot", func(r *sharedAdmissionRequest) { r.Reservation.SnapshotID = "" }},
		{"nonblank_snapshot_mismatch", func(r *sharedAdmissionRequest) {
			var fact CallFact
			if err := json.Unmarshal(r.Fallback, &fact); err != nil {
				t.Fatal(err)
			}
			fact.SnapshotID = "cfg_01arz3ndektsv4rrffq69g5faw"
			r.Fallback = sharedEconomicHistorySQLBody(t, fact)
		}},
		{"reduced_hold", func(r *sharedAdmissionRequest) { *r.Bound.Tokens-- }},
		{"corrupt_basis", func(r *sharedAdmissionRequest) { r.Reservation.Candidates[0].PriceBasis.Schedule.Rates[0].Amount = "1" }},
		{"missing_owner_birth", func(r *sharedAdmissionRequest) { r.OwnerStartedAt = time.Time{} }},
		{"owner_birth_precision", func(r *sharedAdmissionRequest) { r.OwnerStartedAt = r.OwnerStartedAt.Add(time.Nanosecond) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := sharedTestCopy(original)
			tc.edit(&request)
			if _, err := newSharedAdmissionCoordinator(c.intent.Owner, request, c.spool, c.authority, sharedCoordinatorOptions{OperationTimeout: time.Second}); err == nil {
				t.Fatal("missing or inconsistent original evidence accepted")
			}
		})
	}
}

func TestSharedAdmissionAllowsUnknownLogicalSnapshotWithExactAttemptAttribution(t *testing.T) {
	c, _, _, request := sharedTestFixture(t)
	initial, err := sharedCallPayload(request.RequestID, request.Fallback)
	if err != nil || initial.SnapshotID != "" {
		t.Fatal("initial logical attribution was synthesized", err)
	}
	if err := validateSharedSpoolAdmission(c.intent); err != nil {
		t.Fatal("blank pre-attempt logical snapshot rejected", err)
	}
	attempt := sharedTestDispatch(t, 1)
	attempt.Key = c.key()
	attempt.Digest = durableSharedSpoolDigest(t, attempt)
	if err := validateSharedSpoolAttempt(&sharedSpoolRecord{Intent: c.intent}, attempt); err != nil {
		t.Fatal("blank logical snapshot lost exact attempt snapshot", err)
	}
	var fact CallFact
	if err := json.Unmarshal(attempt.Fallback, &fact); err != nil {
		t.Fatal(err)
	}
	fact.Attempts[0].SnapshotID = "cfg_01arz3ndektsv4rrffq69g5faw"
	attempt.Fallback = sharedEconomicHistorySQLBody(t, fact)
	attempt.Digest = durableSharedSpoolDigest(t, attempt)
	if err := validateSharedSpoolAttempt(&sharedSpoolRecord{Intent: c.intent}, attempt); err == nil {
		t.Fatal("unknown logical snapshot excused mismatched attempt attribution")
	}
}

func TestSharedAdmissionDetachesAndDigestBindsReservationAndOwnerBirth(t *testing.T) {
	c, spool, authority, request := sharedTestFixture(t)
	original := sharedTestCopy(c.intent)
	request.Reservation.Candidates[0].PriceBasis.Currency.Rates["USD"] = "2"
	request.Reservation.Candidates = request.Reservation.Candidates[:1]
	request.OwnerStartedAt = request.OwnerStartedAt.Add(time.Microsecond)
	if err := c.Admit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, spool.record.Intent) || authority.acceptCount != 1 {
		t.Fatal("caller mutation changed the original detached capture")
	}
	for _, change := range []func(*sharedAdmissionIntent){
		func(i *sharedAdmissionIntent) { i.OwnerStartedAt = i.OwnerStartedAt.Add(time.Microsecond) },
		func(i *sharedAdmissionIntent) { i.Reservation.Candidates = i.Reservation.Candidates[:1] },
	} {
		changed := sharedTestCopy(original)
		change(&changed)
		if err := validateSharedSpoolAdmission(changed); err == nil {
			t.Fatal("changed original evidence reused the old digest")
		}
	}
}

func TestSharedAdmissionWholeIntentKeepsExistingSpoolBound(t *testing.T) {
	c, _, _, request := sharedTestFixture(t)
	// The standalone manifest is valid and below its existing 4 MiB cap. The
	// complete canonical intent must still obey the smaller 128 KiB spool cap.
	request.Reservation = sharedTestReservation(t, request.RequestID, request.Authorization, true, true, 80)
	request.Bound, request.Recovery = request.Reservation.MaximumHold, zeroQuotaSettlement(request.Reservation.MaximumHold)
	body, err := json.Marshal(request.Reservation)
	if err != nil || len(body) <= eventqueue.SharedSpoolMaxDataBytes || len(body) > gatewayReservationManifestMaxBytes {
		t.Fatal(err)
	}
	if _, err := newSharedAdmissionCoordinator(c.intent.Owner, request, c.spool, c.authority, sharedCoordinatorOptions{OperationTimeout: time.Second}); err == nil {
		t.Fatal("oversized complete original intent accepted")
	}
}
