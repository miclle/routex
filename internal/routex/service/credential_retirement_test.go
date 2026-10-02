package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func retirementIntentFixture() (string, string, string, CredentialRetirementInput) {
	return "usr_test", "crd_01k6j4hdh40000000000000000", strings.Repeat("a", 64), CredentialRetirementInput{RequestID: "81b043bb-cb96-419c-b9a2-cab04814b65e", ReplacementCredentialID: "crd_01k6j4hdh40000000000000001", EvidenceAttemptID: "req_attempt_1", SnapshotID: "cfg_01k6j4hdh40000000000000002", Reason: "Reviewed completed replacement"}
}

func TestCredentialRetirementIntentValidation(t *testing.T) {
	_, source, etag, input := retirementIntentFixture()
	if !validCredentialRetirementIntent(source, etag, input) {
		t.Fatal("valid reviewed intent rejected")
	}
	for name, modify := range map[string]func(*CredentialRetirementInput){"missing_uuid": func(v *CredentialRetirementInput) { v.RequestID = "" }, "uppercase_uuid": func(v *CredentialRetirementInput) { v.RequestID = strings.ToUpper(v.RequestID) }, "nil_uuid": func(v *CredentialRetirementInput) { v.RequestID = "00000000-0000-0000-0000-000000000000" }, "same_source": func(v *CredentialRetirementInput) { v.ReplacementCredentialID = source }, "malformed_credential": func(v *CredentialRetirementInput) { v.ReplacementCredentialID = "crd_notcanonical" }, "missing_attempt": func(v *CredentialRetirementInput) { v.EvidenceAttemptID = "" }, "unsafe_attempt": func(v *CredentialRetirementInput) { v.EvidenceAttemptID = "attempt/secret" }, "long_attempt": func(v *CredentialRetirementInput) { v.EvidenceAttemptID = strings.Repeat("a", 65) }, "missing_cfg": func(v *CredentialRetirementInput) { v.SnapshotID = "" }, "malformed_cfg": func(v *CredentialRetirementInput) { v.SnapshotID = "cfg_invented" }, "empty_reason": func(v *CredentialRetirementInput) { v.Reason = "" }, "control_reason": func(v *CredentialRetirementInput) { v.Reason = "reason\n" }, "reason_bound": func(v *CredentialRetirementInput) { v.Reason = strings.Repeat("界", 342) }} {
		t.Run(name, func(t *testing.T) {
			changed := input
			modify(&changed)
			if validCredentialRetirementIntent(source, etag, changed) {
				t.Fatal("unsafe intent accepted")
			}
		})
	}
	for _, invalid := range []string{"", strings.Repeat("a", 63), strings.Repeat("A", 64), strings.Repeat("z", 64)} {
		if validCredentialRetirementIntent(source, invalid, input) {
			t.Fatal("invalid strong aggregate validator accepted")
		}
	}
}

func TestCredentialRetirementReceiptIdentity(t *testing.T) {
	actor, source, etag, input := retirementIntentFixture()
	hash := credentialRetirementHash(actor, source, etag, input)
	row := entity.CredentialRetirementReceipt{RequestID: input.RequestID, ActorID: actor, SourceCredentialID: source, ReplacementCredentialID: input.ReplacementCredentialID, RequestHash: hash, ReadinessETag: etag, PreDisableSnapshotID: input.SnapshotID, EvidenceAttemptID: input.EvidenceAttemptID, CommittedAt: time.Now().UTC()}
	if !credentialRetirementReceiptMatches(row, actor, source, etag, hash, input) {
		t.Fatal("exact immutable replay rejected")
	}
	for name, modify := range map[string]func(*entity.CredentialRetirementReceipt){"uuid_alias": func(v *entity.CredentialRetirementReceipt) { v.RequestID = strings.ToUpper(v.RequestID) }, "actor": func(v *entity.CredentialRetirementReceipt) { v.ActorID = "usr_another" }, "source": func(v *entity.CredentialRetirementReceipt) { v.SourceCredentialID = "crd_another" }, "successor": func(v *entity.CredentialRetirementReceipt) { v.ReplacementCredentialID = "crd_another" }, "hash": func(v *entity.CredentialRetirementReceipt) { v.RequestHash = strings.Repeat("b", 64) }, "etag": func(v *entity.CredentialRetirementReceipt) { v.ReadinessETag = strings.Repeat("b", 64) }, "snapshot": func(v *entity.CredentialRetirementReceipt) { v.PreDisableSnapshotID = "cfg_another" }, "attempt": func(v *entity.CredentialRetirementReceipt) { v.EvidenceAttemptID = "attempt_another" }, "uncommitted": func(v *entity.CredentialRetirementReceipt) { v.CommittedAt = time.Time{} }} {
		t.Run(name, func(t *testing.T) {
			changed := row
			modify(&changed)
			if credentialRetirementReceiptMatches(changed, actor, source, etag, hash, input) {
				t.Fatal("receipt alias promoted into committed history")
			}
		})
	}
	for name, modify := range map[string]func(*CredentialRetirementInput){"request": func(v *CredentialRetirementInput) { v.RequestID = "91b043bb-cb96-419c-b9a2-cab04814b65e" }, "successor": func(v *CredentialRetirementInput) { v.ReplacementCredentialID = "crd_another" }, "attempt": func(v *CredentialRetirementInput) { v.EvidenceAttemptID = "attempt_another" }, "snapshot": func(v *CredentialRetirementInput) { v.SnapshotID = "cfg_another" }, "reason": func(v *CredentialRetirementInput) { v.Reason = "Changed reason" }} {
		t.Run("hash_"+name, func(t *testing.T) {
			changed := input
			modify(&changed)
			if credentialRetirementHash(actor, source, etag, changed) == hash {
				t.Fatal("immutable intent field not hashed")
			}
		})
	}
	if credentialRetirementHash("usr_another", source, etag, input) == hash || credentialRetirementHash(actor, "crd_another", etag, input) == hash || credentialRetirementHash(actor, source, strings.Repeat("b", 64), input) == hash {
		t.Fatal("reviewed identity not hashed")
	}
}

func TestCredentialRetirementHistoricalWire(t *testing.T) {
	actor, source, _, input := retirementIntentFixture()
	row := CredentialRetirementRecord{RequestID: input.RequestID, SourceCredentialID: source, ReplacementCredentialID: input.ReplacementCredentialID, Committed: true, CommittedAt: time.Now().UTC(), Blockers: []string{"runtime_unavailable"}}
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var shape map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &shape); err != nil || len(shape) != 8 || string(shape["current_snapshot_id"]) != "null" || string(shape["runtime_applied"]) != "false" {
		t.Fatal("pending publication must preserve explicit historical truth and null snapshot")
	}
	for _, forbidden := range []string{actor, "ciphertext", "secret", "request_hash", "source_etag", "usage", "token", "key_id", "evidence_attempt_id"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatal("unsafe receipt projection", forbidden)
		}
	}
}
