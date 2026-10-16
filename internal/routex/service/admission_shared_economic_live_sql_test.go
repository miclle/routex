package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/miclle/routex/pkg/routeattempt"
)

func TestSharedEconomicDispatchRequiresOriginalRoute(t *testing.T) {
	_, plan, result, data, payload := reservationManifestFixture(t)
	manifest, err := captureGatewayReservationManifest("req_capture", plan, result, data, payload)
	if err != nil {
		t.Fatal(err)
	}
	intent := sharedAdmissionIntent{sharedAdmissionRequest: sharedAdmissionRequest{Reservation: manifest}}
	candidate := manifest.Candidates[0]
	attempt := sharedAttemptIntent{Number: 1, CredentialID: candidate.Credential.Identity.ID, ConnectionID: candidate.Connection.ID,
		ProviderModelID: candidate.ProviderModel.ID, SnapshotID: manifest.SnapshotID}
	original := sharedTestCopy(*manifest)
	selected, err := sharedEconomicSelectedAttempt(intent, attempt)
	if err != nil || selected != (routeattempt.Attempt{Number: 1, TargetID: candidate.Binding.ID, CredentialID: attempt.CredentialID,
		ConnectionID: attempt.ConnectionID, Protocol: manifest.Protocol}) {
		t.Fatal("original route selection", selected, err)
	}
	for _, test := range []struct {
		name   string
		change func(*sharedAttemptIntent)
	}{
		{"another_credential", func(a *sharedAttemptIntent) { a.CredentialID = "credential_other" }},
		{"another_connection", func(a *sharedAttemptIntent) { a.ConnectionID = "connection_other" }},
		{"another_provider_model", func(a *sharedAttemptIntent) { a.ProviderModelID = "provider_model_other" }},
		{"another_snapshot", func(a *sharedAttemptIntent) { a.SnapshotID = "snapshot_other" }},
		{"zero_attempt", func(a *sharedAttemptIntent) { a.Number = 0 }},
		{"fifth_attempt", func(a *sharedAttemptIntent) { a.Number = 5 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := attempt
			test.change(&changed)
			if _, err := sharedEconomicSelectedAttempt(intent, changed); err == nil {
				t.Fatal("dispatch accepted a route outside the original reservation")
			}
		})
	}
	if !reflect.DeepEqual(*manifest, original) {
		t.Fatal("dispatch selection rewrote the original maximum or route set")
	}
	reduced := sharedTestCopy(*manifest)
	tokens := *reduced.MaximumHold.Tokens - 1
	reduced.MaximumHold.Tokens = &tokens
	intent.Reservation = &reduced
	if _, err := sharedEconomicSelectedAttempt(intent, attempt); err == nil {
		t.Fatal("dispatch accepted a reduced original maximum")
	}
}

func TestSharedEconomicLiveCaptureBindsActualNativeRequest(t *testing.T) {
	svc, plan, result, data, payload := reservationManifestFixture(t)
	manifest, err := captureGatewayReservationManifest("req_capture", plan, result, data, payload)
	if err != nil {
		t.Fatal(err)
	}
	facts := sharedAuthorizationTestFacts("personal_key")
	facts.Model.ID, facts.Model.CreatedAt = manifest.Model.ID, manifest.Model.Birth
	facts.PersonalRows[0].ModelID = manifest.Model.ID
	facts.PersonalGrants.Hash = personalGrantHash(facts.PersonalRows)
	facts.KeyModels = []string{manifest.Model.ID}
	proof, err := newSharedCapturedAuthorization(facts, sharedAuthorizationTestTime())
	if err != nil {
		t.Fatal("actual selected model proof", err)
	}
	initial, err := sharedCallPayload("req_shared", sharedTestPayload(t, nil, false))
	if err != nil {
		t.Fatal(err)
	}
	initial.RequestID, initial.ModelID, initial.SnapshotID = "req_capture", manifest.Model.ID, ""
	fallback, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	result.UserID, result.KeyID, result.ModelName = initial.UserID, initial.KeyID, initial.ModelName
	plan.userID, plan.keyID = result.UserID, result.KeyID
	request := sharedAdmissionRequest{RequestID: initial.RequestID, Reservation: manifest, OwnerStartedAt: sharedAuthorizationTestTime().Add(-time.Minute),
		Authorization: proof, CalendarRevision: "calendar_live", TimeZone: "UTC", Policies: result.admissionQuota,
		Bound: manifest.MaximumHold, Recovery: zeroQuotaSettlement(manifest.MaximumHold), Fallback: fallback}
	owner := sharedAdmissionOwner{InstallationID: "led_01arz3ndektsv4rrffq69g5fav", SpoolID: "spool_live", IncarnationID: "ins_01arz3ndektsv4rrffq69g5fav", ModeRevision: 2, Fence: 1}
	ip := netip.MustParseAddr("192.0.2.7")
	live, err := newSharedEconomicLiveAuthority(svc, plan, result, ip, owner, request, data, payload)
	if err != nil || live == nil {
		t.Fatal("coherent native capture", err)
	}
	for _, invalidIP := range []netip.Addr{{}, netip.MustParseAddr("::ffff:192.0.2.7"), netip.MustParseAddr("fe80::1%en0")} {
		if authority, err := newSharedEconomicLiveAuthority(svc, plan, result, invalidIP, owner, request, data, payload); authority != nil || err == nil {
			t.Fatal("noncanonical live address accepted", invalidIP)
		}
	}
	changedPayload := make(map[string]json.RawMessage, len(payload))
	for key, value := range payload {
		changedPayload[key] = value
	}
	changedPayload["max_completion_tokens"] = json.RawMessage(`1`)
	if authority, err := newSharedEconomicLiveAuthority(svc, plan, result, ip, owner, request, data, changedPayload); authority != nil || err == nil {
		t.Fatal("manifest accepted a different actual native output bound")
	}
	original := sharedTestCopy(*manifest)
	request.Reservation.Native.MaximumOutput++
	if !reflect.DeepEqual(*live.original.Reservation, original) {
		t.Fatal("caller mutation replaced the captured live manifest")
	}
}

func TestSharedEconomicLiveAuthorityCannotComeFromHistory(t *testing.T) {
	var live *sharedEconomicLiveAuthority
	if err := live.check(context.Background(), sharedAdmissionIntent{}, nil); !errors.Is(err, errSharedDispatchForbidden) {
		t.Fatal("missing live request authorized work", err)
	}
	var store *sharedEconomicSQLStore
	if release, err := store.lockLive(); release != nil || !errors.Is(err, errSharedDispatchForbidden) {
		t.Fatal("missing live authority acquired execution locks", err)
	}
}

func TestSharedEconomicLiveAcceptanceBindsOneExecution(t *testing.T) {
	live := &sharedEconomicLiveAuthority{}
	key := sharedAdmissionKey{RequestID: "req_original", ExecutionID: "executor_original", AdmissionDigest: "original"}
	if live.acceptedHere(key) {
		t.Fatal("persisted identity alone was treated as a live accepted executor")
	}
	if err := live.confirmAdmission(key); err != nil || !live.acceptedHere(key) {
		t.Fatal("live admission acknowledgement", err)
	}
	if err := live.confirmAdmission(key); err != nil {
		t.Fatal("identical live admission acknowledgement", err)
	}
	changed := key
	changed.ExecutionID = "executor_recovered"
	if live.acceptedHere(changed) || !errors.Is(live.confirmAdmission(changed), errSharedAdmissionConflict) || !live.acceptedHere(key) {
		t.Fatal("a different execution adopted the first executor's live acknowledgement")
	}
}
