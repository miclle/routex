package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secretstore"
	"reflect"
	"strings"
	"testing"
)

func TestSavedVaultAppRoleExactReplacementUnion(t *testing.T) {
	for _, raw := range []string{`{"action":"replace","token":"legacy"}`, `{"action":"replace","method":"token","token":"explicit"}`, `{"action":"replace","method":"approle","auth_mount":"custom/approle","role_id":"reader-role","secret_id":"reusable-reader"}`, `{"action":"keep"}`, `{"action":"remove"}`} {
		var input VaultAuthInput
		if err := json.Unmarshal([]byte(raw), &input); err != nil {
			t.Fatalf("supported authentication union rejected: %v", err)
		}
	}
	for _, raw := range []string{`{"action":"replace","method":"approle","auth_mount":"approle","role_id":"reader-role"}`, `{"action":"replace","method":"approle","auth_mount":"approle","role_id":"reader-role","secret_id":"reusable-reader","token":"mixed"}`, `{"action":"replace","method":"approle","auth_mount":"../approle","role_id":"reader-role","secret_id":"reusable-reader"}`, `{"action":"replace","method":"approle","auth_mount":"approle","role_id":"reader-role","secret_id":null}`, `{"action":"replace","method":"APPROLE","auth_mount":"approle","role_id":"reader-role","secret_id":"reusable-reader"}`, `{"action":"keep","method":"token"}`} {
		var input VaultAuthInput
		if json.Unmarshal([]byte(raw), &input) == nil {
			t.Fatal("ambiguous authentication union accepted")
		}
	}
}

func savedAppRoleAuth(t *testing.T, s *Service, f *vaultCommandFixture) {
	t.Helper()
	writer, e := vaultAuthReplacement(VaultAuthInput{Action: "replace", Method: "approle", AuthMount: "custom/approle", RoleID: "writer-role", SecretID: "writer-role-reusable"})
	if e != nil {
		t.Fatal(e)
	}
	reader, e := vaultAuthReplacement(VaultAuthInput{Action: "replace", Method: "approle", AuthMount: "custom/approle", RoleID: "reader-role", SecretID: "reader-role-reusable"})
	if e != nil {
		t.Fatal(e)
	}
	f.data.writer.Method, f.data.reader.Method = "approle", "approle"
	f.data.writer.AuthCiphertext, e = s.secrets.Seal(rootReference("vault_writer_auth", f.data.writer.ID, f.data.writer.SecretGeneration), writer)
	if e != nil {
		t.Fatal(e)
	}
	f.data.reader.AuthCiphertext, e = s.secrets.Seal(rootReference("vault_reader_auth", f.data.reader.ID, f.data.reader.SecretGeneration), reader)
	if e != nil {
		t.Fatal(e)
	}
}

func TestSavedVaultAppRoleProbeClaimIdentityAndReplay(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(fmt.Sprint(denied), func(t *testing.T) {
			s, f, roles := vaultCommandService(t)
			savedAppRoleAuth(t, s, f)
			f.loginDenied = denied
			actor := roles.data.users["usr_admin"]
			etag := vaultReview(actor, f.data.row, f.data.rev)
			input := VaultStageInput{"11111111-1111-4111-8111-111111111111", "Reviewed"}
			result, _, e := s.RunVaultProbe(context.Background(), actor.ID, f.data.row.ID, "", "write", etag, input)
			if e != nil || result == nil {
				t.Fatal("claimed result missing", e)
			}
			if denied {
				if result.Write.Attempted || result.Write.Succeeded || result.Write.Failure == nil || result.Write.Failure.Stage != "prepare" || f.calls != 1 {
					t.Fatal("login failure manufactured KV effect")
				}
				return
			}
			if !result.Write.Succeeded || !reflect.DeepEqual(f.logins, []string{"writer-role"}) {
				t.Fatal("write used wrong identity")
			}
			before := f.calls
			again, _, e := s.RunVaultProbe(context.Background(), actor.ID, f.data.row.ID, "", "write", etag, input)
			if e != nil || again == nil || f.calls != before {
				t.Fatal("receipt replay performed login/effect", e)
			}
			read, _, e := s.RunVaultProbe(context.Background(), actor.ID, f.data.row.ID, result.ID, "read", result.ReviewETag, VaultStageInput{"22222222-2222-4222-8222-222222222222", "Read"})
			if e != nil || read == nil || !read.Read.Succeeded || read.Cleanup.State != "acknowledged" || !reflect.DeepEqual(f.logins, []string{"writer-role", "reader-role", "writer-role"}) {
				t.Fatal("read/cleanup identity lifecycle", e)
			}
		})
	}
}

func TestSavedVaultAppRoleCredentialRecoveryDoesNotRepeatWriteLogin(t *testing.T) {
	s, f, _ := credentialStorageSQLService(t)
	savedAppRoleAuth(t, s, f.vault)
	input := credentialStorageFixtureIntent(t, s, f, "credential")
	request := "11111111-1111-4111-8111-111111111111"
	f.lostWrite = true
	_, e := s.createVaultCredential(context.Background(), "usr_admin", request, "private-value", input)
	if e == nil || f.posts != 1 || !reflect.DeepEqual(f.logins, []string{"writer-role"}) {
		t.Fatal("lost response boundary not exercised")
	}
	f.lostWrite = false
	result, e := s.createVaultCredential(context.Background(), "usr_admin", request, "private-value", input)
	if e != nil || result == nil || f.posts != 1 || f.gets != 1 || !reflect.DeepEqual(f.logins, []string{"writer-role", "reader-role"}) {
		t.Fatal("recovery repeated original write/login", e)
	}
	rows := []entity.ProviderCredential{f.data.credentials[result.CredentialID]}
	if e = attachCredentialSources(s.db, rows); e != nil {
		t.Fatal(e)
	}
	if e = s.cacheCredentialValue(context.Background(), rows[0], "private-value"); e != nil {
		t.Fatal(e)
	}
	before := len(f.logins)
	value, e := s.preparedCredentialValue(rows[0])
	if e != nil || value != "private-value" || len(f.logins) != before {
		t.Fatal("prepared inference required login", e)
	}
	f.vault.data.reader.Method = "token"
	rows = []entity.ProviderCredential{f.data.credentials[result.CredentialID]}
	if e = attachCredentialSources(s.db, rows); e == nil {
		if _, e = s.preparedCredentialValue(rows[0]); e == nil {
			t.Fatal("method substitution reused tuple/cache")
		}
	}
	if len(f.logins) != before {
		t.Fatal("local auth check dispatched login")
	}
}

func TestSavedVaultAppRoleMaximumEscapingFitsRootEnvelope(t *testing.T) {
	for _, chars := range [][2]string{{"\"", "\\"}, {"<", "&"}} {
		t.Run(chars[0], func(t *testing.T) {
			input := VaultAuthInput{Action: "replace", Method: "approle", AuthMount: "approle", RoleID: strings.Repeat(chars[0], 4096), SecretID: strings.Repeat(chars[1], 4096)}
			material, e := vaultAuthReplacement(input)
			if e != nil {
				t.Fatal(e)
			}
			if len(material) > 32<<10 {
				t.Fatal("valid tuple exceeds existing root plaintext limit")
			}
			s, _, _ := vaultCommandService(t)
			sealed, e := s.secrets.Seal("owned-tuple", material)
			if e != nil {
				t.Fatal("existing root envelope cannot retain maximum tuple", e)
			}
			opened, e := s.secrets.Open("owned-tuple", sealed)
			if e != nil || opened != material {
				t.Fatal("maximum tuple did not roundtrip", e)
			}
			if _, e = vaultAuthMaterial("approle", opened); e != nil {
				t.Fatal("maximum tuple invalid", e)
			}
			if _, e = vaultAuthMaterial("token", opened); e == nil {
				t.Fatal("AppRole tuple interpreted as Token")
			}
		})
	}
}

func TestSavedVaultAppRoleDrainedRootProofNeedsNoLogin(t *testing.T) {
	s, root, f, _ := rootVaultDrainFixture(t)
	savedAppRoleAuth(t, s, f.vault)
	rows := []entity.ProviderCredential{}
	for _, row := range f.data.credentials {
		if row.StorageSource == "vault" {
			rows = append(rows, row)
		}
	}
	if e := attachCredentialSources(s.db, rows); e != nil || len(rows) != 1 {
		t.Fatal("source", e)
	}
	if e := s.cacheCredentialValue(context.Background(), rows[0], "private-value"); e != nil {
		t.Fatal(e)
	}
	if e := s.RefreshRuntime(context.Background()); e != nil {
		t.Fatal(e)
	}
	beforeGET, beforePOST, beforeLogin := f.gets, f.posts, len(f.logins)
	view := s.rootPolicy.Load()
	if e := s.drainSecretView(context.Background(), view); e != nil {
		t.Fatal(e)
	}
	if _, e := s.rootCurrentProofAdmission(s.db, root.policy, true); e != nil {
		t.Fatal("AppRole retained tuple lost drained proof", e)
	}
	if f.gets != beforeGET || f.posts != beforePOST || len(f.logins) != beforeLogin {
		t.Fatal("root proof performed Vault auth/read")
	}
}

func TestSavedVaultAppRoleConfigurationReplayRetainsTupleWithoutLogin(t *testing.T) {
	s, f, roles := vaultCommandService(t)
	actor := roles.data.users["usr_admin"]
	old := f.data.clone()
	input := VaultConfigInput{RequestID: "33333333-3333-4333-8333-333333333333", Name: old.row.Name, Descriptor: vaultDescriptor(old.rev), Reason: "Reviewed auth", WriterAuth: VaultAuthInput{Action: "replace", Method: "approle", AuthMount: "custom/approle", RoleID: "writer-role", SecretID: "writer-role-reusable"}, ReaderAuth: VaultAuthInput{Action: "replace", Method: "approle", AuthMount: "custom/approle", RoleID: "reader-role", SecretID: "reader-role-reusable"}}
	etag := vaultReview(actor, old.row, old.rev)
	result, e := s.SaveVaultIntegration(context.Background(), actor.ID, old.row.ID, etag, input)
	if e != nil || result == nil || !result.Committed || f.calls != 0 || f.data.writer.Method != "approle" || f.data.reader.Method != "approle" {
		t.Fatal("save logged in or failed tuple persistence", e)
	}
	ciphertext := f.data.reader.AuthCiphertext
	again, e := s.SaveVaultIntegration(context.Background(), actor.ID, old.row.ID, etag, input)
	if e != nil || !reflect.DeepEqual(again, result) || f.calls != 0 || f.data.reader.AuthCiphertext != ciphertext {
		t.Fatal("replay changed original auth/login", e)
	}
	changed := input
	changed.ReaderAuth.SecretID = "different"
	if _, e = s.SaveVaultIntegration(context.Background(), actor.ID, old.row.ID, etag, changed); e == nil || f.calls != 0 {
		t.Fatal("UUID replay accepted new tuple")
	}
	for _, receipt := range f.data.receipts {
		for _, secret := range []string{input.WriterAuth.RoleID, input.WriterAuth.SecretID, input.ReaderAuth.RoleID, input.ReaderAuth.SecretID} {
			if strings.Contains(receipt.IntentJSON, secret) {
				t.Fatal("auth material in durable intent")
			}
		}
	}
	view, e := s.GetVaultIntegration(context.Background(), actor.ID, f.data.row.ID)
	if e != nil || view.WriterAuth != (VaultAuthView{"approle", true}) || view.ReaderAuth != (VaultAuthView{"approle", true}) {
		t.Fatal("recorded auth method missing", e)
	}
	raw, e := json.Marshal(view)
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{input.WriterAuth.RoleID, input.WriterAuth.SecretID, input.ReaderAuth.RoleID, input.ReaderAuth.SecretID} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("GET exposed tuple")
		}
	}
}

func TestSavedVaultAppRoleAllCreateKindsAndFailedLogin(t *testing.T) {
	for _, kind := range []string{"provider", "connection", "credential", "replacement"} {
		t.Run(kind, func(t *testing.T) {
			s, f, _ := credentialStorageSQLService(t)
			savedAppRoleAuth(t, s, f.vault)
			intent := credentialStorageFixtureIntent(t, s, f, kind)
			result, e := s.createVaultCredential(context.Background(), "usr_admin", "11111111-1111-4111-8111-111111111111", "private-value", intent)
			if e != nil || result == nil || f.posts != 1 || f.gets != 1 || !reflect.DeepEqual(f.logins, []string{"writer-role", "reader-role"}) {
				t.Fatal("create identities", e)
			}
		})
	}
	s, f, _ := credentialStorageSQLService(t)
	savedAppRoleAuth(t, s, f.vault)
	f.loginDenied = true
	intent := credentialStorageFixtureIntent(t, s, f, "credential")
	result, e := s.createVaultCredential(context.Background(), "usr_admin", "11111111-1111-4111-8111-111111111111", "private-value", intent)
	if e == nil || result == nil || f.posts != 0 || f.gets != 0 || len(f.data.ops) != 1 || !reflect.DeepEqual(f.logins, []string{"writer-role"}) {
		t.Fatal("failed login escaped claim")
	}
	var observation VaultObservationView
	if !vaultDecodeObservation(result.WriteJSON, &observation) || observation.Attempted || observation.Succeeded || observation.Failure == nil || observation.Failure.Stage != "prepare" {
		t.Fatal("login failure recorded as KV")
	}
}

func TestSavedVaultAppRoleReadLoginFailurePreservesWriteFact(t *testing.T) {
	s, f, roles := vaultCommandService(t)
	savedAppRoleAuth(t, s, f)
	actor := roles.data.users["usr_admin"]
	written, _, e := s.RunVaultProbe(context.Background(), actor.ID, f.data.row.ID, "", "write", vaultReview(actor, f.data.row, f.data.rev), VaultStageInput{"11111111-1111-4111-8111-111111111111", "Write"})
	if e != nil || !written.Write.Succeeded {
		t.Fatal(e)
	}
	f.loginDenied = true
	before := f.calls
	read, _, e := s.RunVaultProbe(context.Background(), actor.ID, f.data.row.ID, written.ID, "read", written.ReviewETag, VaultStageInput{"22222222-2222-4222-8222-222222222222", "Read"})
	if e != nil || read == nil || read.Write != written.Write || read.Read.Attempted || read.Read.Succeeded || read.Read.Failure == nil || read.Read.Failure.Stage != "prepare" || read.Cleanup.State != "unknown" || f.calls != before+1 {
		t.Fatal("failed login changed original write or manufactured read/cleanup", e)
	}
}

func TestSavedVaultAppRoleRewrapPreservesLogicalCacheButChangedTupleDoesNot(t *testing.T) {
	s, f, _ := credentialStorageSQLService(t)
	savedAppRoleAuth(t, s, f.vault)
	input := credentialStorageFixtureIntent(t, s, f, "credential")
	result, e := s.createVaultCredential(context.Background(), "usr_admin", "11111111-1111-4111-8111-111111111111", "private-value", input)
	if e != nil {
		t.Fatal(e)
	}
	rows := []entity.ProviderCredential{f.data.credentials[result.CredentialID]}
	if e = attachCredentialSources(s.db, rows); e != nil {
		t.Fatal(e)
	}
	if e = s.cacheCredentialValue(context.Background(), rows[0], "private-value"); e != nil {
		t.Fatal(e)
	}
	proof := credentialSourceProof(rows[0])
	beforeLogin, beforeGET := len(f.logins), f.gets
	material, e := s.secrets.Open(rootReference("vault_reader_auth", f.vault.data.reader.ID, f.vault.data.reader.SecretGeneration), f.vault.data.reader.AuthCiphertext)
	if e != nil {
		t.Fatal(e)
	}
	ring, e := secretstore.NewKeyring(map[string][]byte{"root": bytes.Repeat([]byte{3}, 32), "new": bytes.Repeat([]byte{9}, 32)}, "root", "new")
	if e != nil {
		t.Fatal(e)
	}
	f.vault.data.reader.AuthCiphertext, e = ring.Seal(rootReference("vault_reader_auth", f.vault.data.reader.ID, f.vault.data.reader.SecretGeneration), material)
	if e != nil {
		t.Fatal(e)
	}
	s.secrets = ring
	rows = []entity.ProviderCredential{f.data.credentials[result.CredentialID]}
	if e = attachCredentialSources(s.db, rows); e != nil {
		t.Fatal(e)
	}
	if credentialSourceProof(rows[0]) != proof {
		t.Fatal("rewrap changed logical identity")
	}
	if value, e := s.preparedCredentialValue(rows[0]); e != nil || value != "private-value" {
		t.Fatal("rewrap lost prepared source", e)
	}
	changed, e := vaultAuthReplacement(VaultAuthInput{Action: "replace", Method: "approle", AuthMount: "custom/approle", RoleID: "reader-role", SecretID: "changed"})
	if e != nil {
		t.Fatal(e)
	}
	f.vault.data.reader.AuthCiphertext, e = ring.Seal(rootReference("vault_reader_auth", f.vault.data.reader.ID, f.vault.data.reader.SecretGeneration), changed)
	if e != nil {
		t.Fatal(e)
	}
	rows = []entity.ProviderCredential{f.data.credentials[result.CredentialID]}
	if e = attachCredentialSources(s.db, rows); e != nil {
		t.Fatal(e)
	}
	if _, e = s.preparedCredentialValue(rows[0]); e == nil {
		t.Fatal("valid same-generation different tuple reused cache")
	}
	if len(f.logins) != beforeLogin || f.gets != beforeGET {
		t.Fatal("local cache validation logged in/read remotely")
	}
}

func TestSavedVaultAppRoleLiteralDuplicateRejectsWithoutPrincipalInference(t *testing.T) {
	for _, same := range []bool{true, false} {
		t.Run(fmt.Sprint(same), func(t *testing.T) {
			s, f, roles := vaultCommandService(t)
			actor := roles.data.users["usr_admin"]
			old := f.data.clone()
			auth := VaultAuthInput{Action: "replace", Method: "approle", AuthMount: "custom/approle", RoleID: "writer-role", SecretID: "writer-role-reusable"}
			reader := auth
			if !same {
				reader = VaultAuthInput{Action: "replace", Method: "token", Token: auth.SecretID}
			}
			input := VaultConfigInput{RequestID: "33333333-3333-4333-8333-333333333333", Name: old.row.Name, Descriptor: vaultDescriptor(old.rev), Reason: "Reviewed", WriterAuth: auth, ReaderAuth: reader}
			result, e := s.SaveVaultIntegration(context.Background(), actor.ID, old.row.ID, vaultReview(actor, old.row, old.rev), input)
			if same {
				if e == nil || result != nil || !reflect.DeepEqual(old, f.data) {
					t.Fatal("literal duplicate accepted")
				}
			} else if e != nil || result == nil || f.data.writer.Method != "approle" || f.data.reader.Method != "token" {
				t.Fatal("mixed method equality inferred", e)
			}
			if f.calls != 0 {
				t.Fatal("configuration attempted principal attestation")
			}
		})
	}
}

func TestSavedVaultAppRoleOriginalAuthRecoveryAfterConfigurationAdvance(t *testing.T) {
	s, f, _ := credentialStorageSQLService(t)
	savedAppRoleAuth(t, s, f.vault)
	input := credentialStorageFixtureIntent(t, s, f, "credential")
	request := "11111111-1111-4111-8111-111111111111"
	f.lostWrite = true
	if _, e := s.createVaultCredential(context.Background(), "usr_admin", request, "private-value", input); e == nil {
		t.Fatal("ambiguous write not exercised")
	}
	original := f.data.ops[request]
	old := f.vault.data
	f.retained = &old
	f.vault.data.row.RevisionID = "vlr_02bbbbbbbbbbbbbbbbbbbbbbbb"
	f.vault.data.rev.ID = f.vault.data.row.RevisionID
	f.vault.data.rev.Prefix = "future-prefix"
	f.vault.data.writer.ID = f.vault.data.row.RevisionID
	f.vault.data.reader.ID = f.vault.data.row.RevisionID
	f.vault.data.writer.Method = "token"
	f.vault.data.reader.Method = "token"
	f.data.policy = entity.CredentialStoragePolicy{ID: 1, Mode: "inline", Generation: "future_inline"}
	f.lostWrite = false
	result, e := s.createVaultCredential(context.Background(), "usr_admin", request, "private-value", input)
	if e != nil || result == nil || result.RevisionID != old.rev.ID || result.ReferenceID != original.ReferenceID || result.WriteJSON != original.WriteJSON || f.posts != 1 || f.gets != 1 || !reflect.DeepEqual(f.logins, []string{"writer-role", "reader-role"}) {
		t.Fatal("recovery followed current method/descriptor or repeated writer login", e)
	}
}

func TestSavedVaultAppRoleStartupAndVerifyPrepareOnlyOutsideLocks(t *testing.T) {
	s, f, _ := credentialStorageSQLService(t)
	savedAppRoleAuth(t, s, f.vault)
	input := credentialStorageFixtureIntent(t, s, f, "credential")
	result, e := s.createVaultCredential(context.Background(), "usr_admin", "11111111-1111-4111-8111-111111111111", "private-value", input)
	if e != nil {
		t.Fatal(e)
	}
	row := f.data.credentials[result.CredentialID]
	row.Enabled = true
	row.VerificationStatus = "verified"
	f.data.credentials[row.ID] = row
	before := len(f.logins)
	f.allowUnclaimedReaderLogin = true // Startup/explicit read has no external write claim.
	if e = s.prepareStartupCredentialValues(context.Background()); e != nil || len(f.logins) != before+1 || f.logins[len(f.logins)-1] != "reader-role" {
		t.Fatal("startup reader preparation", e)
	}
	rows := []entity.ProviderCredential{row}
	if e = attachCredentialSources(s.db, rows); e != nil {
		t.Fatal(e)
	}
	before = len(f.logins)
	if value, e := s.preparedCredentialValue(rows[0]); e != nil || value != "private-value" || len(f.logins) != before {
		t.Fatal("prepared startup value needed auth", e)
	}
	f.loginDenied = true
	if e = s.prepareStartupCredentialValues(context.Background()); e != nil {
		t.Fatal("Vault auth outage blocked startup/control plane", e)
	}
	if _, e = s.preparedCredentialValue(rows[0]); e == nil {
		t.Fatal("failed preparation retained affected route")
	}
}
