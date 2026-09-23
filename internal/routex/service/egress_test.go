package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func TestEgressPreparationEncryptsAndPreservesSecrets(t *testing.T) {
	svc, _, _ := runtimeFixture(t, "http://127.0.0.1/v1")
	input := EgressInput{Name: "Proxy", Kind: "socks5", Host: "proxy.example.com", Port: 1080, Auth: EgressAuthInput{Action: "replace", Username: "fixture-user", Password: "fixture-proxy-secret"}}
	row, config, changed, err := svc.prepareEgress(entity.Egress{ID: "egr_one", Enabled: true}, input)
	if err != nil || !changed || config.Password != "fixture-proxy-secret" || row.AuthCiphertext == "" || strings.Contains(row.AuthCiphertext, "fixture-proxy-secret") {
		t.Fatal("proxy credentials were not encrypted")
	}
	copy := row
	copy.ID = "egr_other"
	if _, err := svc.egressConfig(copy); err == nil {
		t.Fatal("copied proxy ciphertext accepted a different identity")
	}
	view := egressView(row)
	if !view.AuthConfigured {
		t.Fatal("missing redacted authentication marker")
	}
	svc.secrets = nil
	input.Name = "Renamed"
	input.Auth = EgressAuthInput{Action: "keep"}
	renamed, _, changed, err := svc.prepareEgress(row, input)
	if err != nil || changed || renamed.AuthCiphertext != row.AuthCiphertext {
		t.Fatal("name-only change unnecessarily decrypted or erased credentials")
	}
	input.Auth = EgressAuthInput{Action: "remove"}
	cleared, _, changed, err := svc.prepareEgress(row, input)
	if err != nil || !changed || cleared.AuthCiphertext != "" || cleared.SecretGeneration != "" {
		t.Fatal("explicit authentication removal failed")
	}
}

func TestEgressPreparationRejectsCredentialRedirect(t *testing.T) {
	svc, _, _ := runtimeFixture(t, "http://127.0.0.1/v1")
	row := entity.Egress{
		ID:               "egr_one",
		Name:             "Proxy",
		Kind:             "socks5",
		Host:             "proxy.example.com",
		Port:             1080,
		Enabled:          true,
		SecretGeneration: "sec_one",
		AuthCiphertext:   "encrypted",
	}
	input := EgressInput{
		Name: "Proxy", Kind: "socks5", Host: "attacker.example.com", Port: 1080,
		Auth: EgressAuthInput{Action: "keep"},
	}
	if _, _, _, err := svc.prepareEgress(row, input); err != apperrors.ErrBadRequest {
		t.Fatalf("credential redirect error = %v, want bad request", err)
	}

	input.Auth.Action = "remove"
	updated, _, changed, err := svc.prepareEgress(row, input)
	if err != nil || !changed || updated.AuthCiphertext != "" {
		t.Fatalf("explicit credential removal failed: changed=%v err=%v", changed, err)
	}
}

func TestEgressRuntimeRejectsStaleTransportOnFailedPublication(t *testing.T) {
	svc, data, _ := runtimeFixture(t, "http://127.0.0.1/v1")
	route, _, err := svc.runtimeRoute("mdl_one")
	if err != nil {
		t.Fatal(err)
	}
	egressID := "egr_proxy"
	data.EgressSetting = entity.EgressSetting{ID: 1, DefaultEgressID: &egressID, ETag: "rev_changed"}
	data.Egresses = []entity.Egress{{ID: egressID, Kind: "socks5", Host: "proxy.example.com", Port: 1080, Enabled: true, ETag: "rev_bad", SecretGeneration: "sec_bad", AuthCiphertext: "corrupt"}}
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	if _, err := svc.buildRuntimeRoutes(data); err == nil {
		t.Fatal("invalid proxy secret published")
	}
	if _, _, err := svc.runtimeRoute("mdl_one"); err == nil {
		t.Fatal("last-valid route bypassed new transport policy")
	}
	if err := svc.admitEgressGatewayCall(context.Background(), "req_stale", &GatewayResult{}, route); err == nil {
		t.Fatal("already-selected stale transport admitted")
	}
}

func TestEgressAdmissionSerializesAcknowledgedChanges(t *testing.T) {
	svc, _, _ := runtimeFixture(t, "http://127.0.0.1/v1")
	route, _, err := svc.runtimeRoute("mdl_one")
	if err != nil {
		t.Fatal(err)
	}
	svc.egressMu.Lock()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		done <- svc.admitEgressGatewayCall(context.Background(), "req_after_change", &GatewayResult{}, route)
	}()
	<-started
	svc.invalidateEgressRuntime()
	svc.egressMu.Unlock()
	if err := <-done; err == nil {
		t.Fatal("request selected before update admitted after acknowledgement")
	}
}

func TestEgressResolutionKeepsDirectAndDefaultDistinct(t *testing.T) {
	egressID := "egr_proxy"
	data := &runtimeData{EgressSetting: entity.EgressSetting{ID: 1, DefaultEgressID: &egressID, ETag: "rev_default"}, Egresses: []entity.Egress{{ID: egressID, Enabled: false, ETag: "rev_disabled"}}}
	inherited := entity.ProviderConnection{ID: "con_default", EgressMode: "default"}
	direct := entity.ProviderConnection{ID: "con_direct", EgressMode: "direct"}
	if _, _, enabled := runtimeEgressSelection(data, inherited); enabled {
		t.Fatal("disabled default silently fell back to direct")
	}
	if proxy, revision, enabled := runtimeEgressSelection(data, direct); !enabled || proxy != nil || revision == "" {
		t.Fatal("explicit direct was affected by default proxy")
	}
	data.Egresses[0].Enabled = true
	if proxy, _, enabled := runtimeEgressSelection(data, inherited); !enabled || proxy == nil || proxy.ID != egressID {
		t.Fatal("default proxy not resolved")
	}
	before, _ := runtimeDigest(data)
	data.Egresses[0].LastDiagnostic = "diagnostic changed"
	now := time.Now()
	data.Egresses[0].LastCheckedAt = &now
	after, _ := runtimeDigest(data)
	if before != after {
		t.Fatal("diagnostic metadata unnecessarily replaced runtime clients")
	}
}

func TestEgressRevisionIncludesActualTransportState(t *testing.T) {
	proxyID := "egr_proxy"
	connection := entity.ProviderConnection{ID: "con_one", BaseURL: "https://target.example.com", EgressMode: "default"}
	setting := entity.EgressSetting{DefaultEgressID: &proxyID, ETag: "same"}
	row := entity.Egress{ID: proxyID, Kind: "socks5", Host: "proxy.example.com", Port: 1080, Enabled: true, ETag: "same"}
	before := egressRevision(connection, setting, &row)
	row.AuthCiphertext = "corrupt external change"
	if before == egressRevision(connection, setting, &row) {
		t.Fatal("transport revision trusted mutable ETag alone")
	}
	setting.DefaultEgressID = nil
	if egressRevision(connection, setting, nil) == egressRevision(connection, entity.EgressSetting{DefaultEgressID: &proxyID, ETag: "same"}, nil) {
		t.Fatal("default selection missing from revision")
	}
}
