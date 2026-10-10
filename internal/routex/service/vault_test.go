package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/vault"
)

func vaultTestInput() VaultConfigInput {
	return VaultConfigInput{RequestID: "11111111-1111-4111-8111-111111111111", Name: "Vault\ufeff", Descriptor: VaultDescriptor{"https://vault.example", "", "kv", "routex-probes", "value"}, WriterAuth: VaultAuthInput{Action: "replace", Token: "writer-secret-for-test"}, ReaderAuth: VaultAuthInput{Action: "replace", Token: "reader-secret-for-test"}, Reason: "Reviewed\ufeff"}
}
func TestVaultStrictConfigAndStageInputs(t *testing.T) {
	good := vaultTestInput()
	raw, _ := json.Marshal(good)
	var got VaultConfigInput
	if e := json.Unmarshal(raw, &got); e != nil || got != good {
		t.Fatal("valid immutable input rejected", e)
	}
	for name, change := range map[string]func(string) string{"null namespace": func(s string) string { return strings.Replace(s, `"namespace":""`, `"namespace": null`, 1) }, "duplicate token": func(s string) string {
		return strings.Replace(s, `"token":"writer-secret-for-test"`, `"token":"writer-secret-for-test","token":"other"`, 1)
	}, "aliased reason": func(s string) string { return strings.Replace(s, `"reason":`, `"Reason":`, 1) }, "metadata switch": func(s string) string { return s[:len(s)-1] + `,"enabled":true}` }, "null request": func(s string) string {
		return strings.Replace(s, `"request_id":"11111111-1111-4111-8111-111111111111"`, `"request_id":null`, 1)
	}, "unpaired surrogate": func(s string) string { return strings.Replace(s, `"name":"Vault`, `"name":"\ud800`, 1) }, "trailing": func(s string) string { return s + `{}` }} {
		t.Run(name, func(t *testing.T) {
			var input VaultConfigInput
			if json.Unmarshal([]byte(change(string(raw))), &input) == nil {
				t.Fatal("ambiguous input accepted")
			}
		})
	}
	for _, raw := range []string{`{"action":"remove"}`, `{"action":"keep"}`, `{"action":"replace","token":"exact-token"}`} {
		var a VaultAuthInput
		if json.Unmarshal([]byte(raw), &a) != nil {
			t.Fatal("explicit auth rejected")
		}
	}
	for _, raw := range []string{`{"action":"keep","token":""}`, `{"action":"remove","token":null}`, `{"action":"replace"}`, `{"action":"replace","token":" space"}`, `{"action":"replace","token":"x\n"}`, `{"action": ["replace"],"token":"x"}`, `{"action":"replace","token":null}`, `{"action":"remove","action":"keep"}`} {
		var a VaultAuthInput
		if json.Unmarshal([]byte(raw), &a) == nil {
			t.Fatal("ambiguous auth accepted")
		}
	}
	for _, raw := range []string{`{"request_id":"11111111-1111-4111-8111-111111111111","reason":"Reviewed"}`, `{"request_id":"11111111-1111-4111-8111-111111111111","reason":"\ufeff"}`} {
		var a VaultStageInput
		if json.Unmarshal([]byte(raw), &a) != nil {
			t.Fatal("valid stage rejected")
		}
	}
	for _, raw := range []string{`{"request_id":null,"reason":"R"}`, `{"request_id":"11111111-1111-4111-8111-111111111111","reason":"R","path":"foreign"}`, `{"request_id":"11111111-1111-4111-8111-111111111111","reason":null}`} {
		var a VaultStageInput
		if json.Unmarshal([]byte(raw), &a) == nil {
			t.Fatal("caller-selected plan accepted")
		}
	}
}
func TestVaultIntentDoesNotPersistSecretFingerprints(t *testing.T) {
	v := vaultTestInput()
	raw := vaultIntent(v)
	if strings.Contains(raw, v.WriterAuth.Token) || strings.Contains(raw, v.ReaderAuth.Token) || strings.Contains(raw, "\"token\"") {
		t.Fatal("replacement secret escaped")
	}
	changed := v
	changed.WriterAuth.Token = "changed"
	if vaultIntent(changed) != raw {
		t.Fatal("secret-derived receipt fingerprint")
	}
	changed.ReaderAuth.Action = "remove"
	if vaultIntent(changed) == raw {
		t.Fatal("auth intent not captured")
	}
	if _, e := vaultAuthValue(VaultAuthInput{Action: "keep"}, "", false); e == nil {
		t.Fatal("unconfigured keep accepted")
	}
	if _, e := vaultAuthValue(VaultAuthInput{Action: "keep"}, "old", true); e == nil {
		t.Fatal("creation keep accepted")
	}
}
func TestVaultReviewScopesAndHistoricalProbeObservations(t *testing.T) {
	birth := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	actor := entity.User{ID: "usr_01aaaaaaaaaaaaaaaaaaaaaaaa", CreatedAt: birth}
	row := entity.VaultIntegration{ID: "vlt_01aaaaaaaaaaaaaaaaaaaaaaaa", Name: "A", RevisionID: "vlr_01aaaaaaaaaaaaaaaaaaaaaaaa", CreatedAt: birth}
	rev := entity.VaultRevision{ID: row.RevisionID, Name: "A", IntegrationID: row.ID, IntegrationBirth: birth}
	original := vaultReview(actor, row, rev)
	if !vaultStrong(original) {
		t.Fatal("not strong")
	}
	rev.ID = "vlr_01bbbbbbbbbbbbbbbbbbbbbbbb"
	if vaultReview(actor, row, rev) == original {
		t.Fatal("auth/descriptor revision not reviewed")
	}
	row.CreatedAt = birth.Add(time.Millisecond)
	if vaultIdentity(actor, row) == original[:64] {
		t.Fatal("target reincarnation alias")
	}
	actor.CreatedAt = birth.Add(time.Millisecond)
	if vaultIdentity(actor, row) == original[:64] {
		t.Fatal("actor reincarnation alias")
	}
	row.CreatedAt = birth
	p := entity.VaultProbe{ID: strings.Repeat("a", 32), RequestID: vaultTestInput().RequestID, IntegrationID: row.ID, IntegrationBirth: birth, RevisionID: row.RevisionID, Generation: strings.Repeat("a", 64), Version: 1, State: "completed", CreatedAt: birth, WriteJSON: vaultJSON(vaultObservation(vault.Observation{Attempted: true, Failure: &vault.Failure{Stage: "write", Code: "transport"}})), ReadJSON: vaultJSON(vaultObservation(vault.Observation{Attempted: true, Succeeded: true})), CleanupJSON: vaultJSON(vaultCleanup(vault.Cleanup{State: "acknowledged", Observation: vault.Observation{Attempted: true, Succeeded: true}}))}
	v, e := vaultProbeView(actor, row, p)
	if e != nil {
		t.Fatal(e)
	}
	if v.Write.Succeeded || !v.Read.Succeeded || v.Version == nil || *v.Version != 1 {
		t.Fatal("current ownership manufactured original write")
	}
	raw, _ := json.Marshal(v)
	for _, private := range []string{"expected_sha256", "descriptor_sha256", "AuthCiphertext", "Claim", "writer-secret-for-test", "reader-secret-for-test"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("private probe bookkeeping escaped")
		}
	}
}
func TestVaultRootInventoryHistoryAndNullCoverage(t *testing.T) {
	job := entity.SecretRotationJob{InventoryVersion: 1, Domain: 5, Status: "completed", CountsJSON: `{"storage_revisions":{"Scanned":3}}`}
	v := rootRotationView(job, true, false)
	if v.InventoryVersion != 1 || len(v.Domains) != 5 {
		t.Fatal("legacy sentinel relabeled")
	}
	job.InventoryVersion = 2
	v = rootRotationView(job, true, false)
	if len(v.Domains) != 7 || v.Domains[5].Coverage != "not_scanned" {
		t.Fatal("missing domain treated as zero proof")
	}
	raw, _ := json.Marshal(v.Domains[5])
	if !strings.Contains(string(raw), `"scanned":null`) {
		t.Fatal("missing count fabricated")
	}
	for _, domain := range []string{"vault_writer_auth", "vault_reader_auth"} {
		spec, e := rootSpec(domain)
		if e != nil || spec.table != domain || spec.generation != "secret_generation" || rootReference(domain, "vlr_exact", "vag_exact") == "" {
			t.Fatal("missing retained auth inventory")
		}
	}
	start := time.Now().UTC().Add(-300 * time.Second)
	last := time.Now().UTC()
	s := &Service{rootNow: func() time.Time { return last }}
	job.ObservationStartedAt = &start
	job.ObservationLastConfirmedAt = &last
	job.Status = "ready"
	job.VerifiedProcessID = "ins_a"
	job.VerifiedSnapshotID = "cfg_a"
	proof := entity.SecretProcessVerification{InventoryVersion: 2, ProcessID: "ins_a", RuntimeSnapshotID: "cfg_a"}
	if s.rootObservationEligible(job, proof) {
		t.Fatal("old sentinel/missing new counts eligible")
	}
}

func TestVaultCorruptRecordedFactsFailClosed(t *testing.T) {
	for _, raw := range []string{`{"attempted":true,"succeeded":false,"duration_ms":"0","failure":null,"attempted":false}`, `{"attempted":true,"succeeded":true,"duration_ms":"00","failure":null}`, `{"attempted":false,"succeeded":true,"duration_ms":"0","failure":null}`, `{"attempted":true,"succeeded":false,"duration_ms":"0","failure":{"stage":"read","code":"private_error","http_status":0}}`, `{"attempted":true,"succeeded":false,"duration_ms":"0","failure":{"stage":"read","code":"transport","http_status":null}}`} {
		var v VaultObservationView
		if vaultDecodeObservation(raw, &v) {
			t.Fatal("corrupt stage became a fact")
		}
	}
	for _, raw := range []string{`{"vault_writer_auth":null}`, `{"vault_writer_auth":{"scanned":1}}`, `{"vault_writer_auth":{"Scanned":1,"Scanned":0}}`, `{"unknown_domain":{}}`, `{"vault_writer_auth":{"Scanned":-1}}`, `{"vault_writer_auth":{"Scanned":null}}`} {
		if _, e := rootCountsChecked(entity.SecretRotationJob{InventoryVersion: 2, CountsJSON: raw}); e == nil {
			t.Fatal("corrupt inventory proved coverage")
		}
	}
	if _, e := rootCountsChecked(entity.SecretRotationJob{InventoryVersion: 1, CountsJSON: `{"vault_writer_auth":{}}`}); e == nil {
		t.Fatal("legacy job acquired later coverage")
	}
}
func TestVaultTypedAuditProjectsOnlySafeConfigurationFacts(t *testing.T) {
	detail := vaultAuditDetail{vaultTestInput().RequestID, "Reviewed", "vlr_01aaaaaaaaaaaaaaaaaaaaaaaa", true}
	raw := vaultJSON(detail)
	raw = raw[:len(raw)-1] + `,"token":"never expose","secret_hash":"never expose"}`
	row := entity.AuditEvent{Action: "vault.integration.configure", ResourceType: "vault_integration", ResourceID: "vlt_01aaaaaaaaaaaaaaaaaaaaaaaa", DetailsJSON: &raw}
	value, ok := vaultAuditProjection(row)
	if !ok || strings.Contains(vaultJSON(value), "never expose") {
		t.Fatal("unsafe historical projection")
	}
	row.Action = "vault.integration.cleanup"
	if _, ok := vaultAuditProjection(row); ok {
		t.Fatal("cleanup fabricated configuration change")
	}
	row.Action = "vault.integration.configure"
	row.ResourceID = "vlt_01AAAAAAAAAAAAAAAAAAAAAAAA"
	if _, ok := vaultAuditProjection(row); ok {
		t.Fatal("alias audit target")
	}
}

func TestVaultRootUnknownInventoryNeverGrantsActionsOrHistory(t *testing.T) {
	for _, version := range []int{0, -1, 6} {
		job := entity.SecretRotationJob{InventoryVersion: version, CountsJSON: "{}", Status: "blocked"}
		if _, err := rootCountsChecked(job); err == nil {
			t.Fatal("unknown inventory accepted", version)
		}
		if got := rootRotationView(job, true, true); got != nil {
			t.Fatal("unknown history or action authority", version)
		}
	}
	for _, inventory := range []struct{ version, domains int }{{1, 5}, {2, 7}, {3, 8}, {4, 9}, {5, 10}} {
		for _, status := range []string{"completed", "rolled_back"} {
			view := rootRotationView(entity.SecretRotationJob{InventoryVersion: inventory.version, CountsJSON: "{}", Status: status, Domain: inventory.domains}, true, true)
			if view == nil || view.InventoryVersion != inventory.version || len(view.Domains) != inventory.domains || len(view.AllowedActions) != 0 {
				t.Fatal("explicit versioned terminal history changed", inventory.version)
			}
		}
		_, err := rootCountsChecked(entity.SecretRotationJob{InventoryVersion: inventory.version, CountsJSON: `{"oidc_providers":{}}`})
		if (err == nil) != (inventory.version == 3 || inventory.version == 4 || inventory.version == 5) {
			t.Fatal("OIDC domain coverage escaped its inventory version", inventory.version)
		}
		_, err = rootCountsChecked(entity.SecretRotationJob{InventoryVersion: inventory.version, CountsJSON: `{"oauth_providers":{}}`})
		if (err == nil) != (inventory.version == 4 || inventory.version == 5) {
			t.Fatal("OAuth domain coverage escaped its inventory version", inventory.version)
		}
		_, err = rootCountsChecked(entity.SecretRotationJob{InventoryVersion: inventory.version, CountsJSON: `{"ldap_providers":{}}`})
		if (err == nil) != (inventory.version == 5) {
			t.Fatal("LDAP domain coverage escaped its inventory version", inventory.version)
		}
	}
}
func TestVaultRootWorkerRejectsUnknownInventoryWithoutRewrap(t *testing.T) {
	for _, version := range []int{0, 6} {
		svc, f := rootPublicationService(t)
		f.job.InventoryVersion = version
		before := f.egress.AuthCiphertext
		if err := svc.RunSecretRotationOnce(context.Background()); err == nil {
			t.Fatal("unknown inventory ran")
		}
		if f.egress.AuthCiphertext != before || f.committedCAS || len(f.items) != 0 {
			t.Fatal("unknown inventory caused effects")
		}
	}
}
