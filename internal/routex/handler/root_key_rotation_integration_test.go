package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
)

type rootRotationCipherFixture struct {
	table, idColumn, id, column, reference, plaintext string
}

type rootRotationRetainedFixture struct {
	secrets []rootRotationCipherFixture
}

func rootRotationSeedLegacy(t *testing.T, db *gorm.DB, store *secretstore.Store) rootRotationRetainedFixture {
	t.Helper()
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	result := rootRotationRetainedFixture{}
	seal := func(table, idColumn, id, column, reference, plaintext string) string {
		t.Helper()
		ciphertext, err := store.Seal(reference, plaintext)
		if err != nil {
			t.Fatal("seal controlled fixture", err)
		}
		result.secrets = append(result.secrets, rootRotationCipherFixture{table, idColumn, id, column, reference, plaintext})
		return ciphertext
	}
	create := func(row any) {
		t.Helper()
		if err := db.Create(row).Error; err != nil {
			t.Fatal("seed retained root dependency", err)
		}
	}
	create(&entity.Provider{ID: "prv_root_history", Name: "Retained root provider", CreatedAt: stamp})
	create(&entity.ProviderConnection{ID: "con_root_history", ProviderID: "prv_root_history", Name: "Retained disabled connection", BaseURL: "http://127.0.0.1:1/v1", Protocol: entity.ProtocolOpenAIChat, EgressMode: "direct", ETag: "0", CreatedAt: stamp})
	for index, status := range []string{"pending", "verified"} {
		id := fmt.Sprintf("crd_root_history_%d", index)
		ciphertext := seal("provider_credentials", "id", id, "ciphertext", id, "test-only-retained-provider-secret")
		row := entity.ProviderCredential{ID: id, ConnectionID: "con_root_history", Name: "Retained " + status, Priority: index + 7, VerificationStatus: status, Ciphertext: ciphertext, CreatedAt: stamp}
		if index == 1 {
			source := "crd_root_history_0"
			row.ReplacesCredentialID, row.VerifiedAt = &source, &stamp
		}
		create(&row)
	}
	egID, egGeneration := "egr_root_history", "sec_root_egress"
	create(&entity.Egress{ID: egID, Name: "Disabled retained proxy", Kind: "https", Host: "127.0.0.1", Port: 9, ETag: "rev_root_egress", SecretGeneration: egGeneration, AuthCiphertext: seal("egresses", "id", egID, "auth_ciphertext", "egress:"+egID+":"+egGeneration, `{"Username":"test-only-user","Password":"test-only-egress-password"}`), CreatedAt: stamp, UpdatedAt: stamp})
	smtpGeneration := "sec_root_smtp"
	if err := db.Model(&entity.SMTPSetting{}).Where("id = ?", 1).Updates(map[string]any{"Enabled": false, "Host": "127.0.0.1", "Port": 9, "Security": "SSL_TLS", "ETag": "rev_root_smtp", "SecretGeneration": smtpGeneration, "AuthCiphertext": seal("smtp_settings", "id", "1", "auth_ciphertext", "smtp:1:"+smtpGeneration, `{"Username":"test-only-user","Password":"test-only-smtp-password"}`)}).Error; err != nil {
		t.Fatal(err)
	}
	for index := range 25 {
		id, generation := fmt.Sprintf("str_root_history_%02d", index), fmt.Sprintf("sec_root_storage_%02d", index)
		row := entity.StorageRevision{ID: id, Endpoint: "http://127.0.0.1:1", Region: "us-east-1", Bucket: "routex-test", Prefix: fmt.Sprintf("retained-%02d/", index), SecretGeneration: generation, AuthCiphertext: seal("storage_revisions", "id", id, "auth_ciphertext", "storage:"+id+":"+generation, `{"AccessKey":"test-only-access","SecretKey":"test-only-storage-secret"}`), CreatedBy: "usr_root_deleted", CreatedAt: stamp}
		if index%2 == 0 {
			row.VerifiedAt = &stamp
		}
		create(&row)
	}
	for index, id := range []string{"usr_root_pending", "usr_root_enabled", "usr_root_disabled"} {
		create(&entity.User{ID: id, Email: id + "@example.invalid", Name: "Retained MFA actor", PasswordHash: "test-only-retained-password-hash", Role: entity.RoleMember, Disabled: index == 2, CreatedAt: stamp, UpdatedAt: stamp})
		generation := fmt.Sprintf("root-mfa-generation-%d", index)
		locked := stamp.Add(time.Hour)
		create(&entity.UserMFA{UserID: id, Enabled: index != 0, Generation: generation, SecretCiphertext: seal("user_mfa", "user_id", id, "secret_ciphertext", "mfa:"+id+":"+generation, "JBSWY3DPEHPK3PXP"), LastTOTPStep: -1, FailedAttempts: index, LockedUntil: &locked, UpdatedAt: stamp})
		create(&entity.MFARecoveryCode{UserID: id, CodeHash: strings.Repeat(fmt.Sprint(index), 64)})
	}
	orphanID, orphanGeneration := "usr_root_missing", "root-mfa-orphan-generation"
	create(&entity.UserMFA{UserID: orphanID, Generation: orphanGeneration, SecretCiphertext: seal("user_mfa", "user_id", orphanID, "secret_ciphertext", "mfa:"+orphanID+":"+orphanGeneration, "JBSWY3DPEHPK3PXP"), LastTOTPStep: 77, FailedAttempts: 2, UpdatedAt: stamp})
	create(&entity.MFARecoveryCode{UserID: orphanID, CodeHash: strings.Repeat("c", 64), UsedAt: &stamp})
	create(&entity.MFAChallenge{UserID: "usr_root_pending", Purpose: "enrollment", TokenHash: strings.Repeat("a", 64), PasswordDigest: strings.Repeat("b", 64), Generation: "root-mfa-generation-0", SessionID: "ses_root_history", ExpiresAt: stamp.Add(time.Hour), Attempts: 2})
	// Both current and retained immutable Vault revisions participate in root
	// rotation; their auth rows never imply an active Provider-store switch.
	integrationID := "vlt_01aaaaaaaaaaaaaaaaaaaaaaaa"
	create(&entity.VaultIntegration{ID: integrationID, Name: "Retained Vault", RevisionID: "vlr_01bbbbbbbbbbbbbbbbbbbbbbbb", CreatedAt: stamp, UpdatedAt: stamp})
	for index, revisionID := range []string{"vlr_01aaaaaaaaaaaaaaaaaaaaaaaa", "vlr_01bbbbbbbbbbbbbbbbbbbbbbbb"} {
		create(&entity.VaultRevision{ID: revisionID, IntegrationID: integrationID, IntegrationBirth: stamp, Name: "Retained Vault", Endpoint: "http://127.0.0.1:1", Mount: "kv", Prefix: "routex-probes", DataField: "value", CreatedAt: stamp})
		writerGeneration, readerGeneration := fmt.Sprintf("vag_root_writer_%d", index), fmt.Sprintf("vag_root_reader_%d", index)
		create(&entity.VaultWriterAuth{ID: revisionID, SecretGeneration: writerGeneration, AuthCiphertext: seal("vault_writer_auth", "id", revisionID, "auth_ciphertext", "vault-writer:"+revisionID+":"+writerGeneration, "test-only-vault-writer")})
		create(&entity.VaultReaderAuth{ID: revisionID, SecretGeneration: readerGeneration, AuthCiphertext: seal("vault_reader_auth", "id", revisionID, "auth_ciphertext", "vault-reader:"+revisionID+":"+readerGeneration, "test-only-vault-reader")})
	}
	// Disabled OIDC configuration retains a real encrypted client secret.
	oidcID, oidcGeneration := "oidc", strings.Repeat("d", 64)
	oidc := db.Model(&entity.OIDCProvider{}).Where("id = ?", oidcID).Updates(map[string]any{
		"Name":             "Retained OIDC provider",
		"Issuer":           "https://identity.example.invalid",
		"ClientID":         "test-only-root-client",
		"CallbackURL":      "https://routex.example.invalid/api/v1/auth/oidc/callback",
		"Enabled":          false,
		"SecretGeneration": oidcGeneration,
		"AuthCiphertext":   seal("oidc_providers", "id", oidcID, "auth_ciphertext", "oidc:"+oidcID+":"+oidcGeneration, "test-only-retained-oidc-client-secret"),
	})
	if oidc.Error != nil || oidc.RowsAffected != 1 {
		t.Fatal("seed retained OIDC singleton", oidc.Error, oidc.RowsAffected)
	}
	// Disabled custom OAuth configuration is independently retained and rewrapped.
	oauthID, oauthGeneration := "oauth", strings.Repeat("e", 64)
	oauth := db.Model(&entity.OAuthProvider{}).Where("id = ?", oauthID).Updates(map[string]any{
		"Name":             "Retained OAuth provider",
		"AuthorizationURL": "https://oauth.example.invalid/authorize",
		"TokenURL":         "https://oauth.example.invalid/token",
		"UserInfoURL":      "https://oauth.example.invalid/profile",
		"ClientID":         "test-only-root-oauth-client",
		"CallbackURL":      "https://routex.example.invalid/api/v1/auth/oauth/callback",
		"ClientAuthMethod": "client_secret_basic",
		"ScopesJSON":       `["profile"]`,
		"SubjectPathJSON":  `["id"]`,
		"Enabled":          false,
		"SecretGeneration": oauthGeneration,
		"AuthCiphertext":   seal("oauth_providers", "id", oauthID, "auth_ciphertext", "oauth:"+oauthID+":"+oauthGeneration, "test-only-retained-oauth-client-secret"),
	})
	if oauth.Error != nil || oauth.RowsAffected != 1 {
		t.Fatal("seed retained OAuth singleton", oauth.Error, oauth.RowsAffected)
	}
	// Disabled LDAP configuration retains its independently encrypted service password.
	ldapID, ldapGeneration := "ldap", strings.Repeat("f", 64)
	ldap := db.Model(&entity.LDAPProvider{}).Where("id = ?", ldapID).Updates(map[string]any{
		"Name":              "Retained LDAP provider",
		"Endpoint":          "ldaps://directory.example.invalid:636",
		"BindDN":            "cn=service,dc=example,dc=invalid",
		"BaseDN":            "dc=example,dc=invalid",
		"UserFilter":        "(uid={username})",
		"IdentityAttribute": "entryUUID",
		"Enabled":           false,
		"SecretGeneration":  ldapGeneration,
		"AuthCiphertext":    seal("ldap_providers", "id", ldapID, "auth_ciphertext", "ldap:"+ldapID+":"+ldapGeneration, "test-only-retained-ldap-service-password"),
	})
	if ldap.Error != nil || ldap.RowsAffected != 1 {
		t.Fatal("seed retained LDAP singleton", ldap.Error, ldap.RowsAffected)
	}
	// The fixed named profile remains distinct from the configurable OAuth domain.
	githubID, githubGeneration := "github", strings.Repeat("9", 64)
	github := db.Model(&entity.NamedIdentityProvider{}).Where("id = ?", githubID).Updates(map[string]any{
		"Name": "Retained GitHub provider", "ClientID": "test-only-root-github-client",
		"CallbackURL": "https://routex.example.invalid/api/v1/auth/github/callback",
		"Enabled":     false, "SecretGeneration": githubGeneration,
		"AuthCiphertext": seal("named_identity_providers", "id", githubID, "auth_ciphertext", "named-identity:github.com.oauth-app.v1:github:"+githubGeneration, "test-only-retained-github-client-secret"),
	})
	if github.Error != nil || github.RowsAffected != 1 {
		t.Fatal("seed retained fixed named singleton", github.Error, github.RowsAffected)
	}
	var githubSecret struct{ AuthCiphertext string }
	if db.Table("named_identity_providers").Select("auth_ciphertext").Where("id = ?", githubID).Take(&githubSecret).Error != nil {
		t.Fatal("read controlled encrypted named secret")
	}
	validReference := "named-identity:github.com.oauth-app.v1:github:" + githubGeneration
	if plaintext, err := store.Open(validReference, githubSecret.AuthCiphertext); err != nil || plaintext != "test-only-retained-github-client-secret" {
		t.Fatal("exact named secret AAD positive")
	}
	for _, wrong := range []string{"named-identity:github.com.oauth-app.v2:github:" + githubGeneration, "named-identity:github.com.oauth-app.v1:other:" + githubGeneration, "named-identity:github.com.oauth-app.v1:github:" + strings.Repeat("8", 64), "oauth:oauth:" + githubGeneration} {
		if _, err := store.Open(wrong, githubSecret.AuthCiphertext); err == nil {
			t.Fatal("named secret accepted another profile/provider/generation/domain")
		}
	}
	// The fixed named profile remains distinct from the configurable OAuth domain.
	googleID, googleGeneration := "google", strings.Repeat("a", 64)
	google := db.Model(&entity.NamedIdentityProvider{}).Where("id = ?", googleID).Updates(map[string]any{
		"Name": "Retained Google provider", "ClientID": "test-only-root-google-client",
		"CallbackURL": "https://routex.example.invalid/api/v1/auth/google/callback",
		"Enabled":     false, "SecretGeneration": googleGeneration,
		"AuthCiphertext": seal("named_identity_providers", "id", googleID, "auth_ciphertext", "named-identity:google.oidc.v1:google:"+googleGeneration, "test-only-retained-google-client-secret"),
	})
	if google.Error != nil || google.RowsAffected != 1 {
		t.Fatal("seed retained fixed named singleton", google.Error, google.RowsAffected)
	}
	var googleSecret struct{ AuthCiphertext string }
	if db.Table("named_identity_providers").Select("auth_ciphertext").Where("id = ?", googleID).Take(&googleSecret).Error != nil {
		t.Fatal("read controlled encrypted named secret")
	}
	googleReference := "named-identity:google.oidc.v1:google:" + googleGeneration
	if plaintext, err := store.Open(googleReference, googleSecret.AuthCiphertext); err != nil || plaintext != "test-only-retained-google-client-secret" {
		t.Fatal("exact named secret AAD positive")
	}
	for _, wrong := range []string{"named-identity:google.oidc.v2:google:" + googleGeneration, "named-identity:google.oidc.v1:other:" + googleGeneration, "named-identity:google.oidc.v1:google:" + strings.Repeat("8", 64), "oauth:oauth:" + googleGeneration, "named-identity:github.com.oauth-app.v1:github:" + googleGeneration} {
		if _, err := store.Open(wrong, googleSecret.AuthCiphertext); err == nil {
			t.Fatal("named secret accepted another profile/provider/generation/domain")
		}
	}
	return result
}

func (fixture rootRotationRetainedFixture) read(t *testing.T, db *gorm.DB) []map[string]any {
	t.Helper()
	rows := make([]map[string]any, 0, len(fixture.secrets)+2)
	for _, secret := range fixture.secrets {
		var row map[string]any
		if err := db.Table(secret.table).Where(secret.idColumn+" = ?", secret.id).Take(&row).Error; err != nil {
			t.Fatal("read retained subject", secret.table, secret.id, err)
		}
		rows = append(rows, row)
	}
	for _, table := range []string{"mfa_challenges", "mfa_recovery_codes"} {
		var history []map[string]any
		if err := db.Table(table).Where("user_id LIKE ?", "usr_root_%").Order("user_id").Find(&history).Error; err != nil {
			t.Fatal(err)
		}
		rows = append(rows, map[string]any{"table": table, "rows": history})
	}
	return rows
}

func (fixture rootRotationRetainedFixture) checkRewrapped(t *testing.T, db *gorm.DB, ring *secretstore.Store, keyID string, before []map[string]any) {
	t.Helper()
	after := fixture.read(t, db)
	if len(after) != len(before) {
		t.Fatal("rotation changed retained inventory size")
	}
	for index, secret := range fixture.secrets {
		ciphertext := fmt.Sprint(after[index][secret.column])
		if raw, ok := after[index][secret.column].([]byte); ok {
			ciphertext = string(raw)
		}
		actualKey, err := ring.KeyID(secret.reference, ciphertext)
		if err != nil || actualKey != keyID {
			t.Fatal("retained dependency not authenticated under target", secret.table, secret.id, actualKey, err)
		}
		plain, err := ring.Open(secret.reference, ciphertext)
		if err != nil || plain != secret.plaintext {
			t.Fatal("rewrap changed secret value/reference", secret.table, secret.id, err)
		}
		oldEnvelope := rootRotationEnvelopePayload(t, rootRotationMapText(before[index][secret.column]))
		newEnvelope := rootRotationEnvelopePayload(t, ciphertext)
		if !reflect.DeepEqual(oldEnvelope, newEnvelope) {
			t.Fatal("rewrap replaced immutable payload or nonce", secret.table, secret.id)
		}
		copyAfter := make(map[string]any, len(after[index]))
		for column, value := range after[index] {
			copyAfter[column] = value
		}
		copyAfter[secret.column] = before[index][secret.column]
		if !reflect.DeepEqual(before[index], copyAfter) {
			t.Fatal("pure rewrap changed lifecycle, generation, business ETag or MFA state", secret.table, secret.id)
		}
	}
	if !reflect.DeepEqual(before[len(fixture.secrets):], after[len(fixture.secrets):]) {
		t.Fatal("rotation changed MFA enrollment/recovery history")
	}
}

func rootRotationMapText(value any) string {
	if raw, ok := value.([]byte); ok {
		return string(raw)
	}
	return fmt.Sprint(value)
}

func rootRotationEnvelopePayload(t *testing.T, ciphertext string) map[string]string {
	t.Helper()
	encoded, err := base64.RawURLEncoding.DecodeString(ciphertext)
	if err != nil {
		t.Fatal("invalid controlled envelope", err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatal(err)
	}
	return map[string]string{"nonce": string(envelope["nonce"]), "data": string(envelope["data"])}
}

type rootRotationViewFixture struct {
	ReviewETag string `json:"review_etag"`
	CanRotate  bool   `json:"can_rotate"`
	Policy     struct {
		WriteKeyID *string `json:"write_key_id"`
		Epoch      string  `json:"epoch"`
	} `json:"policy"`
	Process struct {
		ID       *string `json:"id"`
		Verified bool    `json:"verified"`
	} `json:"process"`
	Rotation *rootRotationJobViewFixture `json:"rotation"`
}

type rootRotationDomainViewFixture struct {
	Code          string `json:"code"`
	Scanned       string `json:"scanned"`
	Rewrapped     string `json:"rewrapped"`
	AlreadyTarget string `json:"already_target"`
	Deleted       string `json:"deleted"`
	Changed       string `json:"changed"`
	Blocked       string `json:"blocked"`
}

type rootRotationJobViewFixture struct {
	Domains               []rootRotationDomainViewFixture `json:"domains"`
	ID                    string                          `json:"id"`
	Status                string                          `json:"status"`
	Phase                 string                          `json:"phase"`
	SourceKeyID           string                          `json:"source_key_id"`
	TargetKeyID           string                          `json:"target_key_id"`
	BlockerCodes          []string                        `json:"blocker_codes"`
	AllowedActions        []string                        `json:"allowed_actions"`
	ObservationStartedAt  *time.Time                      `json:"observation_started_at"`
	ObservationEligibleAt *time.Time                      `json:"observation_eligible_at"`
}

type rootRotationResultFixture struct {
	Receipt struct {
		RequestID  string    `json:"request_id"`
		RotationID string    `json:"rotation_id"`
		Action     string    `json:"action"`
		CreatedAt  time.Time `json:"created_at"`
	} `json:"receipt"`
	Committed          bool                        `json:"committed"`
	WritePolicyApplied bool                        `json:"write_policy_applied"`
	PublicationApplied bool                        `json:"publication_applied"`
	ApplicationStatus  string                      `json:"application_status"`
	Rotation           *rootRotationJobViewFixture `json:"rotation"`
}

func testRootKeyRotationLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancelFixture := context.WithCancel(context.Background())
	var async sync.WaitGroup
	oldBytes, nextBytes := bytes.Repeat([]byte{71}, 32), bytes.Repeat([]byte{72}, 32)
	legacy, err := secretstore.New(oldBytes)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := secretstore.NewKeyring(map[string][]byte{"old": oldBytes, "next": nextBytes, "third": bytes.Repeat([]byte{73}, 32)}, "old", "old")
	if err != nil {
		t.Fatal(err)
	}
	var clock atomic.Int64
	clock.Store(time.Now().UTC().UnixNano())
	var auditFailure atomic.Bool
	var armPublicationFailure atomic.Bool
	var publicationFailure atomic.Bool
	const callback = "test_root_rotation_audit_failure"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if auditFailure.Load() && tx.Statement.Table == "audit_events" {
			_ = tx.AddError(errors.New("controlled root rotation audit outage"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	// Call-context gates pause after preparation or inventory capture, outside SQL locks.
	type gateContextKey struct{}
	// Nonzero-sized immutable identity distinguishes owning fixture closures.
	// It is compared locally only and never formatted or persisted.
	type queryGateOwner struct{ marker byte }
	owner := &queryGateOwner{marker: 1}
	type queryGate struct {
		owner                *queryGateOwner
		predispatchMatched   atomic.Bool
		callbackOwnerMatched atomic.Bool
		entered, release     chan struct{}
		armed                atomic.Bool
		stage                atomic.Uint32
		governanceSeen       atomic.Bool
		governanceSchemaSeen atomic.Bool
		pointerMatched       atomic.Bool
		armedAtMatch         atomic.Bool
		pauseEntered         atomic.Bool
	}
	staleGate := &queryGate{owner: owner, entered: make(chan struct{}), release: make(chan struct{})}
	keepGate := &queryGate{owner: owner, entered: make(chan struct{}), release: make(chan struct{})}
	casGate := &queryGate{owner: owner, entered: make(chan struct{}), release: make(chan struct{})}
	pause := func(tx *gorm.DB, gate *queryGate) {
		if !gate.armed.CompareAndSwap(true, false) {
			return
		}
		gate.pauseEntered.Store(true)
		close(gate.entered)
		select {
		case <-gate.release:
		case <-tx.Statement.Context.Done():
			_ = tx.AddError(tx.Statement.Context.Err())
		}
	}
	const beforeQuery = "test_root_rotation_prepared_writer"
	const afterQuery = "test_root_rotation_captured_inventory"
	if err := db.Callback().Query().Before("gorm:query").Register(beforeQuery, func(tx *gorm.DB) {
		if publicationFailure.Load() && tx.Statement.Table == "providers" {
			_ = tx.AddError(errors.New("controlled committed publication outage"))
			return
		}
		gate, _ := tx.Statement.Context.Value(gateContextKey{}).(*queryGate)
		if gate != nil {
			gate.callbackOwnerMatched.Store(gate.owner == owner)
			gate.stage.Store(rootRotationPreparedStage(tx.Statement.Table))
			if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "governance_settings" {
				gate.governanceSchemaSeen.Store(true)
			}
			if tx.Statement.Table == "governance_settings" {
				gate.governanceSeen.Store(true)
			}
			if gate == staleGate || gate == keepGate {
				gate.pointerMatched.Store(true)
			}
		}
		if (gate == staleGate || gate == keepGate) && tx.Statement.Table == "governance_settings" {
			if gate.armed.Load() {
				gate.armedAtMatch.Store(true)
			}
			pause(tx, gate)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().After("gorm:query").Register(afterQuery, func(tx *gorm.DB) {
		gate, _ := tx.Statement.Context.Value(gateContextKey{}).(*queryGate)
		inventory := rootRotationCapturedProviderInventory(tx)
		if gate == casGate && inventory && tx.Statement.Table == "provider_credentials" {
			pause(tx, gate)
		}
	}); err != nil {
		t.Fatal(err)
	}
	const afterCreate = "test_root_rotation_committed_publication"
	if err := db.Callback().Create().After("gorm:create").Register(afterCreate, func(tx *gorm.DB) {
		if tx.Error == nil && tx.Statement.Table == "audit_events" && armPublicationFailure.Load() {
			if event, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && event.Action == "secret_rotation.start" {
				publicationFailure.Store(true)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	var services []*service.Service
	newService := func(store *secretstore.Store) *service.Service {
		t.Helper()
		svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithSecretRotationClock(func() time.Time { return time.Unix(0, clock.Load()).UTC() }), service.WithUpstreamPolicy(true), service.WithStoragePolicy(true), service.WithSMTPPolicy(true), service.WithEgressPolicy(true))
		if err != nil {
			t.Fatal(err)
		}
		services = append(services, svc)
		return svc
	}
	stop := func(svc *service.Service) {
		t.Helper()
		svc.StopRuntime()
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
		if err := svc.StopSystemInstance(context.Background()); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() {
		cancelFixture()
		async.Wait()
		auditFailure.Store(false)
		armPublicationFailure.Store(false)
		publicationFailure.Store(false)
		for _, svc := range services {
			stop(svc)
		}
		if err := db.Callback().Query().Remove(beforeQuery); err != nil {
			t.Error(err)
		}
		if err := db.Callback().Query().Remove(afterQuery); err != nil {
			t.Error(err)
		}
		if err := db.Callback().Create().Remove(afterCreate); err != nil {
			t.Error(err)
		}
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	svc := newService(ring)
	routerFor := func(svc *service.Service) *fox.Engine {
		router := fox.New()
		New(svc).RegisterRoutes(router)
		return router
	}
	router := routerFor(svc)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"root-rotation@example.invalid","password":"test-only-root-password","name":"Root rotation admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	// Administrator role and each independent permission are both required.
	type fixtureActor struct {
		id, csrf string
		cookie   *http.Cookie
	}
	makeActor := func(name, role string, permissions []string) fixtureActor {
		t.Helper()
		member, c, csrf := createSystemStatusMember(t, svc, router, admin.User.ID, name, permissions)
		if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).Update("Role", role).Error; err != nil {
			t.Fatal(err)
		}
		return fixtureActor{member.User.ID, csrf, c}
	}
	readAdmin := makeActor("root-reader", "admin", []string{"secrets.read"})
	writeAdmin := makeActor("root-writer", "admin", []string{"secrets.rotate"})
	peerAdmin := makeActor("root-peer", "admin", []string{"secrets.read", "secrets.rotate"})
	memberActor := makeActor("root-member", "member", []string{"secrets.read", "secrets.rotate"})
	for _, row := range []any{
		&entity.Role{ID: "rol_root_exact", Name: "Exact root permissions", NameKey: "root-exact"},
		&entity.RolePermission{RoleID: "rol_root_exact", Permission: "secrets.read"},
		&entity.RolePermission{RoleID: "rol_root_exact", Permission: "secrets.rotate"},
		&entity.UserRole{UserID: admin.User.ID, RoleID: "rol_root_exact"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Where("role_id = ? AND permission IN ?", "rol_admin", []string{"secrets.read", "secrets.rotate"}).Delete(&entity.RolePermission{}).Error; err != nil {
		t.Fatal(err)
	}
	retained := rootRotationSeedLegacy(t, db, legacy)
	baseline := retained.read(t, db)
	if err := svc.InitializeSecretStore(ctx); err != nil {
		t.Fatal("initialize verified configured roots", err)
	}
	request := func(method, path, body, etag string, credential *http.Cookie, csrf string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if credential != nil {
			req.AddCookie(credential)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	send := func(method, path string, body any, etag string, status int) *httptest.ResponseRecorder {
		t.Helper()
		encoded := ""
		if body != nil {
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			encoded = string(data)
		}
		response := request(method, path, encoded, etag, cookie, admin.CSRFToken)
		if response.Code != status {
			t.Fatalf("%s %s status %d want %d: %s", method, path, response.Code, status, response.Body.String())
		}
		rootRotationCheckPrivacy(t, response, retained)
		return response
	}
	view := func() rootRotationViewFixture {
		t.Helper()
		return decodeCatalogResponse[rootRotationViewFixture](t, send("GET", "/api/v1/admin/secrets", nil, "", 200), 200)
	}
	initial := view()
	if initial.Policy.WriteKeyID == nil || *initial.Policy.WriteKeyID != "old" || initial.Policy.Epoch != "1" || initial.Process.Verified || initial.Rotation != nil {
		t.Fatalf("bootstrap persistence claimed live process publication: %+v", initial)
	}
	startBody := map[string]any{"request_id": "48000000-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "target_key_id": "next", "reason": "Rotate all retained internal secrets"}
	for _, test := range []struct {
		actor        fixtureActor
		method, path string
		body         any
		status       int
	}{
		{readAdmin, "GET", "/api/v1/admin/secrets", nil, 200},
		{writeAdmin, "GET", "/api/v1/admin/secrets", nil, 403},
		{memberActor, "GET", "/api/v1/admin/secrets", nil, 403},
		{readAdmin, "POST", "/api/v1/admin/secrets/rotations", startBody, 403},
		{memberActor, "POST", "/api/v1/admin/secrets/rotations", startBody, 403},
	} {
		data, _ := json.Marshal(test.body)
		response := request(test.method, test.path, string(data), initial.ReviewETag, test.actor.cookie, test.actor.csrf)
		if response.Code != test.status {
			t.Fatalf("independent root authority %s status%d want%d: %s", test.method, response.Code, test.status, response.Body.String())
		}
		rootRotationCheckPrivacy(t, response, retained)
		if test.status == 200 && decodeCatalogResponse[rootRotationViewFixture](t, response, 200).CanRotate {
			t.Fatal("read-only permission inferred write authority")
		}
	}
	if response := request("GET", "/api/v1/admin/secrets", "", "", nil, ""); response.Code != 401 {
		t.Fatal("anonymous root management was not denied", response.Code)
	}
	for _, alias := range []string{strings.ToUpper(admin.User.ID), admin.User.ID + " "} {
		if _, err := svc.GetSecretStore(ctx, alias); err == nil {
			t.Fatal("root read accepted actor alias", alias)
		}
	}
	for _, suffix := range []string{"?user_id=" + admin.User.ID, "?limit=10", "?"} {
		send("GET", "/api/v1/admin/secrets"+suffix, nil, "", 400)
	}
	for _, raw := range []string{
		`{"request_id":"48000000-aaaa-4aaa-8aaa-aaaaaaaaaaaa","request_id":"48000000-aaaa-4aaa-8aaa-aaaaaaaaaaaa","target_key_id":"next","reason":"duplicate"}`,
		`{"request_id":"48000000-aaaa-4aaa-8aaa-aaaaaaaaaaaa","target_key_id":"next","reason":null}`,
		`{"request_id":"48000000-aaaa-4aaa-8aaa-aaaaaaaaaaaa","target_key_id":"Next ","reason":"alias"}`,
		`{"request_id":"48000000-aaaa-4aaa-8aaa-aaaaaaaaaaaa","target_key_id":"next","reason":"bad\u0000reason"}`,
		`{"request_id":"48000000-aaaa-4aaa-8aaa-aaaaaaaaaaaa","target_key_id":"next","reason":"bad\ud800"}`,
		`{"request_id":"48000000-aaaa-4aaa-8aaa-aaaaaaaaaaaa","target_key_id":"next","reason":"valid","epoch":1}`,
	} {
		response := request("POST", "/api/v1/admin/secrets/rotations", raw, initial.ReviewETag, cookie, admin.CSRFToken)
		if response.Code != 400 {
			t.Fatalf("strict root intent accepted invalid shape: %d %s", response.Code, response.Body.String())
		}
		rootRotationCheckPrivacy(t, response, retained)
	}
	longReason := map[string]any{"request_id": "48000000-eeee-4eee-8eee-eeeeeeeeeeee", "target_key_id": "next", "reason": strings.Repeat("界", 1001)}
	send("POST", "/api/v1/admin/secrets/rotations", longReason, initial.ReviewETag, 400)
	validJSON, _ := json.Marshal(startBody)
	if response := request("POST", "/api/v1/admin/secrets/rotations", string(validJSON), initial.ReviewETag, cookie, ""); response.Code != 403 {
		t.Fatal("root mutation accepted missing CSRF", response.Code)
	}
	send("POST", "/api/v1/admin/secrets/rotations", startBody, "", 400)
	send("POST", "/api/v1/admin/secrets/rotations", startBody, initial.ReviewETag, 503)
	startProcess := func(svc *service.Service, verify bool) {
		t.Helper()
		if err := svc.StartSystemInstance(ctx, service.SystemInstanceMetadata{Name: "Controlled root rotation", Hostname: "rotation.invalid", Version: "test", GoVersion: "test", OS: "test", Arch: "test"}); err != nil {
			t.Fatal(err)
		}
		if err := svc.StartRuntime(ctx); err != nil {
			t.Fatal(err)
		}
		if verify {
			if err := svc.RunSecretRotationOnce(ctx); err != nil {
				t.Fatal("initialize current process proof", err)
			}
		}
	}
	wrongRing, err := secretstore.NewKeyring(map[string][]byte{"old": bytes.Repeat([]byte{99}, 32), "next": nextBytes}, "old", "old")
	if err != nil {
		t.Fatal(err)
	}
	wrongService := newService(wrongRing)
	if err := wrongService.InitializeSecretStore(ctx); err == nil {
		t.Fatal("configured wrong root material bypassed saved authenticated proofs")
	}
	legacyOnly := newService(legacy)
	if err := legacyOnly.InitializeSecretStore(ctx); err == nil {
		t.Fatal("legacy-only bootstrap bypassed initialized durable write policy")
	}
	startProcess(svc, true)
	current := view()
	if !current.Process.Verified || current.Process.ID == nil {
		t.Fatal("actual current combined instance/runtime was not verified")
	}
	for _, target := range []string{"NEXT", "missing-root"} {
		send("POST", "/api/v1/admin/secrets/rotations", map[string]any{"request_id": "48000000-4567-4567-8567-456745674567", "target_key_id": target, "reason": "Exact configured target identity required"}, current.ReviewETag, 404)
	}
	send("GET", "/api/v1/admin/secrets/rotations/srt_01arz3ndektsv4rrffq69g5faq", nil, "", 404)
	for _, header := range []string{`W/"` + current.ReviewETag + `"`, current.ReviewETag, `"short"`} {
		req := httptest.NewRequest("POST", "http://routex.test/api/v1/admin/secrets/rotations", bytes.NewReader(validJSON))
		req.AddCookie(cookie)
		req.Header.Set("X-CSRF-Token", admin.CSRFToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", header)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != 400 {
			t.Fatal("rotation accepted nonstrong/unreviewed precondition", response.Code)
		}
		rootRotationCheckPrivacy(t, response, retained)
	}
	// A second real registered process is not a supported fleet acknowledgement.
	second := newService(ring)
	if err := second.InitializeSecretStore(ctx); err != nil {
		t.Fatal(err)
	}
	startProcess(second, false)
	send("POST", "/api/v1/admin/secrets/rotations", startBody, view().ReviewETag, 503)
	stop(second)
	current = view()
	// Audit failure must roll back policy, job and immutable receipt together.
	var beforePolicy entity.SecretWritePolicy
	if err := db.Take(&beforePolicy, 1).Error; err != nil {
		t.Fatal(err)
	}
	auditFailure.Store(true)
	send("POST", "/api/v1/admin/secrets/rotations", startBody, current.ReviewETag, 500)
	auditFailure.Store(false)
	var afterPolicy entity.SecretWritePolicy
	var jobs, receipts int64
	if err := db.Take(&afterPolicy, 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.SecretRotationJob{}).Count(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.SecretRotationReceipt{}).Count(&receipts).Error; err != nil || jobs != 0 || receipts != 0 || !reflect.DeepEqual(beforePolicy, afterPolicy) {
		t.Fatal("audit outage partially committed rotation", err)
	}
	// Extra controlled subjects are excluded from the preservation inventory:
	// their only intended mutations are the concurrent replacement and deletion.
	for _, id := range []string{"crd_root_race_b_changed", "crd_root_race_a_deleted"} {
		ciphertext, err := legacy.Seal(id, "test-only-race-before")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&entity.ProviderCredential{ID: id, ConnectionID: "con_root_history", Name: "Controlled concurrent root subject", VerificationStatus: "pending", Ciphertext: ciphertext}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	var originalSMTP entity.SMTPSetting
	if err := db.Take(&originalSMTP, 1).Error; err != nil {
		t.Fatal(err)
	}
	waitPrepared := func(gate *queryGate, result <-chan error) {
		t.Helper()
		failure := func(err error, pending bool) {
			t.Fatalf("writer did not reach prepared epoch gate; %s", rootRotationPreparedFailure(gate.stage.Load(), err, pending, gate.governanceSeen.Load(), gate.governanceSchemaSeen.Load(), gate.pointerMatched.Load(), gate.armedAtMatch.Load(), gate.pauseEntered.Load(), gate.predispatchMatched.Load(), gate.callbackOwnerMatched.Load()))
		}
		select {
		case <-gate.entered:
		case err := <-result:
			failure(err, false)
		case <-time.After(5 * time.Second):
			select {
			case err := <-result:
				failure(err, false)
			default:
				failure(nil, true)
			}
		}
	}
	writerResult := make(chan error, 1)
	keepResult := make(chan error, 1)
	staleGate.armed.Store(true)
	keepGate.armed.Store(true)
	writerCtx, writerCancel := context.WithTimeout(context.WithValue(ctx, gateContextKey{}, staleGate), 10*time.Second)
	defer writerCancel()
	markedStaleGate, _ := writerCtx.Value(gateContextKey{}).(*queryGate)
	staleGate.predispatchMatched.Store(markedStaleGate == staleGate && markedStaleGate.owner == owner)
	keepCtx, keepCancel := context.WithTimeout(context.WithValue(ctx, gateContextKey{}, keepGate), 60*time.Second)
	defer keepCancel()
	markedKeepGate, _ := keepCtx.Value(gateContextKey{}).(*queryGate)
	keepGate.predispatchMatched.Store(markedKeepGate == keepGate && markedKeepGate.owner == owner)
	async.Go(func() {
		_, err := svc.WriteSMTPSettings(writerCtx, admin.User.ID, service.SMTPInput{Host: originalSMTP.Host, Port: originalSMTP.Port, Security: originalSMTP.Security, ETag: originalSMTP.ETag, Auth: service.SMTPAuthInput{Action: "replace", Username: "test-only-stale", Password: "test-only-stale-password"}})
		writerResult <- err
	})
	waitPrepared(staleGate, writerResult)
	async.Go(func() {
		_, err := svc.WriteSMTPSettings(keepCtx, admin.User.ID, service.SMTPInput{Host: originalSMTP.Host, Port: originalSMTP.Port, Security: originalSMTP.Security, ETag: originalSMTP.ETag, Auth: service.SMTPAuthInput{Action: "keep"}})
		keepResult <- err
	})
	waitPrepared(keepGate, keepResult)
	armPublicationFailure.Store(true)
	started := decodeCatalogResponse[rootRotationResultFixture](t, send("POST", "/api/v1/admin/secrets/rotations", startBody, view().ReviewETag, 201), 201)
	if !started.Committed || started.Receipt.Action != "start" || started.Receipt.RequestID != startBody["request_id"] || started.Receipt.RotationID == "" || !started.WritePolicyApplied {
		t.Fatalf("start receipt/cutover not concrete: %+v", started)
	}
	armPublicationFailure.Store(false)
	if started.PublicationApplied || started.ApplicationStatus != "pending" {
		t.Fatal("durable cutover claimed failed runtime publication was applied")
	}
	publicationFailure.Store(false)
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal("restore real publication after durable receipt", err)
	}
	freshCredential, err := svc.CreateCredential(ctx, admin.User.ID, "con_root_history", "Fresh post-cutover credential", "test-only-fresh-root-secret", 13)
	if err != nil || freshCredential == nil {
		t.Fatal("fresh current-epoch writer failed after durable cutover", err)
	}
	if freshCredential.Ciphertext != "" {
		t.Fatal("credential mutation exposed stored ciphertext")
	}
	var storedFresh entity.ProviderCredential
	if err := db.Take(&storedFresh, "id = ?", freshCredential.ID).Error; err != nil {
		t.Fatal(err)
	}
	if key, err := ring.KeyID(storedFresh.ID, storedFresh.Ciphertext); err != nil || key != "next" || storedFresh.Enabled || storedFresh.VerificationStatus != "pending" {
		t.Fatal("fresh writer used bootstrap root or changed lifecycle", key, err)
	}
	close(staleGate.release)
	if err := <-writerResult; err == nil {
		t.Fatal("stale prepared secret committed across write epoch cutover")
	} else {
		var status *apperrors.Error
		if !errors.As(err, &status) || status.Code != 503 {
			t.Fatal("stale epoch did not return retryable safe denial", err)
		}
	}
	var staleAfter entity.SMTPSetting
	if err := db.Take(&staleAfter, 1).Error; err != nil || !reflect.DeepEqual(originalSMTP, staleAfter) {
		t.Fatal("stale prepared write partially changed SMTP state", err)
	}
	var staleAudits int64
	if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "smtp.update", "1").Count(&staleAudits).Error; err != nil || staleAudits != 0 {
		t.Fatal("stale prepared transaction committed SMTP audit", staleAudits, err)
	}
	jobID := started.Receipt.RotationID
	jobPath := "/api/v1/admin/secrets/rotations/" + jobID
	replayed := decodeCatalogResponse[rootRotationResultFixture](t, send("POST", "/api/v1/admin/secrets/rotations", startBody, current.ReviewETag, 200), 200)
	if !reflect.DeepEqual(started.Receipt, replayed.Receipt) {
		t.Fatal("exact UUID retry changed immutable persisted receipt")
	}
	// Reconciliation through independently constructed Services is read-only even
	// when one local process view is stale or has stopped.
	type replayResult struct {
		result *service.SecretRotationResult
		err    error
	}
	concurrent := make(chan replayResult, 2)
	for _, instance := range []*service.Service{svc, second} {
		async.Go(func() {
			result, err := instance.StartSecretRotation(ctx, admin.User.ID, current.ReviewETag, service.SecretRotationStartInput{RequestID: startBody["request_id"].(string), TargetKeyID: "next", Reason: startBody["reason"].(string)})
			concurrent <- replayResult{result, err}
		})
	}
	for range 2 {
		value := <-concurrent
		if value.err != nil || value.result == nil || value.result.Created || value.result.Receipt.RequestID != started.Receipt.RequestID || value.result.Receipt.RotationID != jobID || !value.result.Receipt.CreatedAt.Equal(started.Receipt.CreatedAt) {
			t.Fatal("concurrent exact receipt retry recreated transition or changed persisted identity", value.err)
		}
	}
	var scopedStarts int64
	if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "secret_rotation.start", jobID).Count(&scopedStarts).Error; err != nil || scopedStarts != 1 {
		t.Fatal("receipt retries appended new cutover audit", scopedStarts, err)
	}
	peerData, _ := json.Marshal(startBody)
	if response := request("POST", "/api/v1/admin/secrets/rotations", string(peerData), current.ReviewETag, peerAdmin.cookie, peerAdmin.csrf); response.Code != 409 {
		t.Fatal("global UUID allowed another actor receipt", response.Code)
	}
	send("GET", jobPath, nil, "", 200)
	send("GET", "/api/v1/admin/secrets/rotations/"+strings.ToUpper(jobID), nil, "", 400)
	changed := map[string]any{"request_id": startBody["request_id"], "target_key_id": "old", "reason": startBody["reason"]}
	send("POST", "/api/v1/admin/secrets/rotations", changed, view().ReviewETag, 409)
	casGate.armed.Store(true)
	casResult := make(chan error, 1)
	casCtx, casCancel := context.WithTimeout(context.WithValue(ctx, gateContextKey{}, casGate), 10*time.Second)
	defer casCancel()
	markedCASGate, _ := casCtx.Value(gateContextKey{}).(*queryGate)
	casGate.predispatchMatched.Store(markedCASGate == casGate && markedCASGate.owner == owner)
	async.Go(func() { casResult <- svc.RunSecretRotationOnce(casCtx) })
	waitPrepared(casGate, casResult)
	nextWriter, err := ring.WithWriteKey("next")
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := nextWriter.Seal("crd_root_race_b_changed", "test-only-race-after")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.ProviderCredential{}).Where("id = ?", "crd_root_race_b_changed").Update("Ciphertext", replacement).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", "crd_root_race_a_deleted").Delete(&entity.ProviderCredential{}).Error; err != nil {
		t.Fatal(err)
	}
	close(casGate.release)
	if err := <-casResult; err != nil {
		t.Fatal("captured inventory replacement CAS", err)
	}
	var changedRow entity.ProviderCredential
	if err := db.Take(&changedRow, "id = ?", "crd_root_race_b_changed").Error; err != nil || changedRow.Ciphertext != replacement {
		t.Fatal("migration overwrote a concurrent exact ciphertext replacement", err)
	}
	// The changed-row checkpoint forces a rescan of current authoritative rows;
	// the already recorded captured deletion remains terminal for that subject.
	if err := svc.RunSecretRotationOnce(ctx); err != nil {
		t.Fatal("migration after changed checkpoint", err)
	}
	var deletedCount int64
	if err := db.Model(&entity.ProviderCredential{}).Where("id = ?", "crd_root_race_a_deleted").Count(&deletedCount).Error; err != nil || deletedCount != 0 {
		t.Fatal("migration resurrected a deleted inventory subject", err)
	}
	var deletedItem entity.SecretRotationItem
	if err := db.Take(&deletedItem, "job_id = ? AND subject_id = ?", jobID, "crd_root_race_a_deleted").Error; err != nil || deletedItem.Outcome != "deleted" {
		t.Fatal("captured deletion did not produce durable exact-CAS deletion outcome", err)
	}
	var changedItem entity.SecretRotationItem
	if err := db.Take(&changedItem, "job_id = ? AND subject_id = ?", jobID, "crd_root_race_b_changed").Error; err != nil || changedItem.Attempts < 2 {
		t.Fatal("changed CAS outcome was not retried from authoritative row", err)
	}
	if err := db.Where("role_id = ? AND permission = ?", "rol_root_exact", "secrets.rotate").Delete(&entity.RolePermission{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.RolePermission{RoleID: "rol_root_exact", Permission: "secrets.ROTATE"}).Error; err != nil {
		t.Fatal(err)
	}
	send("POST", "/api/v1/admin/secrets/rotations", startBody, current.ReviewETag, 403)
	if err := db.Where("role_id = ? AND permission = ?", "rol_root_exact", "secrets.ROTATE").Delete(&entity.RolePermission{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.RolePermission{RoleID: "rol_root_exact", Permission: "secrets.rotate"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.User{}).Where("id = ?", admin.User.ID).UpdateColumn("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartSecretRotation(ctx, admin.User.ID, current.ReviewETag, service.SecretRotationStartInput{RequestID: startBody["request_id"].(string), TargetKeyID: "next", Reason: startBody["reason"].(string)}); err == nil {
		t.Fatal("disabled receipt owner reconciled historical rotation without current authority")
	}
	if err := db.Model(&entity.User{}).Where("id = ?", admin.User.ID).UpdateColumn("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	// A corrupt historical revision blocks the global job even with no live objects.
	corrupt := retained.secrets[5]
	var original map[string]any
	if err := db.Table(corrupt.table).Where(corrupt.idColumn+" = ?", corrupt.id).Take(&original).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table(corrupt.table).Where(corrupt.idColumn+" = ?", corrupt.id).Update(corrupt.column, "invalid-controlled-envelope").Error; err != nil {
		t.Fatal(err)
	}
	for range 12 {
		err := svc.RunSecretRotationOnce(ctx)
		if state := view(); state.Rotation != nil && state.Rotation.Status == "blocked" {
			break
		}
		if err != nil {
			t.Fatal("bounded migration pass failed without durable blocker", err)
		}
	}
	blocked := view()
	if blocked.Rotation == nil || blocked.Rotation.Status != "blocked" || len(blocked.Rotation.BlockerCodes) == 0 || slices.Contains(blocked.Rotation.AllowedActions, "retire") {
		t.Fatal("corrupt historical domain was treated as globally complete")
	}
	send("POST", jobPath+"/retire", map[string]any{"request_id": "48000000-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "reason": "Cannot retire incomplete inventory"}, blocked.ReviewETag, 503)
	if err := db.Table(corrupt.table).Where(corrupt.idColumn+" = ?", corrupt.id).Update(corrupt.column, original[corrupt.column]).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal("publish actual current configuration before reviewed resume", err)
	}
	// A rotate-only administrator may act on an already reviewed opaque ETag;
	// losing read authority never grants a metadata read through the write API.
	var writerRole entity.UserRole
	if err := db.Where("user_id = ?", writeAdmin.id).Take(&writerRole).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.RolePermission{RoleID: writerRole.RoleID, Permission: "secrets.read"}).Error; err != nil {
		t.Fatal(err)
	}
	writerReviewResponse := request("GET", "/api/v1/admin/secrets", "", "", writeAdmin.cookie, writeAdmin.csrf)
	writerReview := decodeCatalogResponse[rootRotationViewFixture](t, writerReviewResponse, 200)
	if err := db.Where("role_id = ? AND permission = ?", writerRole.RoleID, "secrets.read").Delete(&entity.RolePermission{}).Error; err != nil {
		t.Fatal(err)
	}
	if response := request("GET", "/api/v1/admin/secrets", "", "", writeAdmin.cookie, writeAdmin.csrf); response.Code != 403 {
		t.Fatal("rotate permission borrowed revoked read authority", response.Code)
	}
	resumeJSON, _ := json.Marshal(map[string]any{"request_id": "48000000-cccc-4ccc-8ccc-cccccccccccc", "reason": "Resume after restoring controlled damaged row"})
	resumeResponse := request("POST", jobPath+"/resume", string(resumeJSON), writerReview.ReviewETag, writeAdmin.cookie, writeAdmin.csrf)
	if resumeResponse.Code != 200 {
		t.Fatalf("rotate-only reviewed resume status%d want200: %s", resumeResponse.Code, resumeResponse.Body.String())
	}
	rootRotationCheckPrivacy(t, resumeResponse, retained)

	for range 50 {
		if err := svc.RunSecretRotationOnce(ctx); err != nil {
			t.Fatal("resume complete-domain pass", err)
		}
		if state := view(); state.Rotation != nil && state.Rotation.Status == "observing" {
			break
		}
	}
	observing := view()
	if observing.Rotation == nil || observing.Rotation.Status != "observing" || observing.Rotation.ObservationStartedAt == nil || observing.Rotation.ObservationEligibleAt == nil || observing.Rotation.ObservationEligibleAt.Sub(*observing.Rotation.ObservationStartedAt) != 300*time.Second || slices.Contains(observing.Rotation.AllowedActions, "retire") {
		t.Fatalf("migration progress bypassed server observation: %+v", observing.Rotation)
	}
	expectedDomains := map[string]uint64{"provider_credentials": 2, "egresses": 1, "smtp_settings": 1, "storage_revisions": 25, "user_mfa": 4, "vault_writer_auth": 2, "vault_reader_auth": 2, "oidc_providers": 1, "oauth_providers": 1, "ldap_providers": 1, "named_identity_providers": 2}
	if len(observing.Rotation.Domains) != len(expectedDomains) {
		t.Fatal("global observation omitted a retained encryption domain")
	}
	seen := map[string]bool{}
	for _, domain := range observing.Rotation.Domains {
		minimum, known := expectedDomains[domain.Code]
		if !known || seen[domain.Code] {
			t.Fatal("unknown/duplicated global rotation domain", domain.Code)
		}
		seen[domain.Code] = true
		for _, text := range []string{domain.Scanned, domain.Rewrapped, domain.AlreadyTarget, domain.Deleted, domain.Changed, domain.Blocked} {
			number, err := strconv.ParseUint(text, 10, 64)
			if err != nil || strconv.FormatUint(number, 10) != text {
				t.Fatal("domain work count lost exact decimal-string encoding", domain.Code, text)
			}
		}
		rewrapped, _ := strconv.ParseUint(domain.Rewrapped, 10, 64)
		if rewrapped < minimum {
			t.Fatal("domain progress did not cover retained disabled/historical dependencies", domain.Code, rewrapped, minimum)
		}
	}
	retained.checkRewrapped(t, db, ring, "next", baseline)
	close(keepGate.release)
	if err := <-keepResult; err != nil {
		t.Fatal("Auth.keep could not reconcile current ciphertext", err)
	}
	var keptSMTP entity.SMTPSetting
	if err := db.Take(&keptSMTP, 1).Error; err != nil {
		t.Fatal(err)
	}
	if keptSMTP.SecretGeneration != originalSMTP.SecretGeneration {
		t.Fatal("Auth.keep replaced the immutable secret generation")
	}
	if key, err := ring.KeyID("smtp:1:"+keptSMTP.SecretGeneration, keptSMTP.AuthCiphertext); err != nil || key != "next" {
		t.Fatal("Auth.keep restored old prepared ciphertext", key, err)
	}
	// This explicit metadata write legitimately advances business ETag/timestamp;
	// subsequent pure rewrap preservation uses its persisted baseline.
	baseline[3] = retained.read(t, db)[3]
	// Short observation ticks retain continuity without manufacturing real leases.
	advanceObservation := func(svc *service.Service, duration time.Duration) {
		t.Helper()
		for duration > 0 {
			step := min(duration, 10*time.Second)
			clock.Add(int64(step))
			if err := svc.RefreshSystemInstance(ctx); err != nil {
				t.Fatal("refresh actual instance lease", err)
			}
			if err := svc.RefreshRuntime(ctx); err != nil {
				t.Fatal("refresh actual runtime publication", err)
			}
			if err := svc.RunSecretRotationOnce(ctx); err != nil {
				t.Fatal("confirm bounded observation tick", err)
			}
			duration -= step
		}
	}
	advanceObservation(svc, 299*time.Second)
	if state := view(); state.Rotation == nil || slices.Contains(state.Rotation.AllowedActions, "retire") {
		t.Fatal("299 seconds satisfied fixed 300-second observation")
	}
	oldProcessID := *observing.Process.ID
	stop(svc)
	// The stopped executor's bounded job lease must expire before takeover.
	clock.Add(int64(31 * time.Second))
	svc = newService(ring)
	if err := svc.InitializeSecretStore(ctx); err != nil {
		t.Fatal("restart reconciles saved policy", err)
	}
	router = routerFor(svc)
	startProcess(svc, true)
	restarted := view()
	if restarted.Policy.WriteKeyID == nil || *restarted.Policy.WriteKeyID != "next" || restarted.Policy.Epoch != "2" || restarted.Process.ID == nil || *restarted.Process.ID == oldProcessID || restarted.Rotation == nil || slices.Contains(restarted.Rotation.AllowedActions, "retire") || restarted.Rotation.ObservationStartedAt == nil || restarted.Rotation.ObservationStartedAt.Before(time.Unix(0, clock.Load()).UTC().Truncate(time.Microsecond)) {
		t.Fatalf("restart reused obsolete observation/initial write selection: %+v", restarted)
	}
	advanceObservation(svc, 300*time.Second)
	ready := view()
	if ready.Rotation == nil || ready.Rotation.Status != "ready" || !slices.Contains(ready.Rotation.AllowedActions, "retire") {
		t.Fatal("verified new process never completed its own server observation")
	}
	retired := decodeCatalogResponse[rootRotationResultFixture](t, send("POST", jobPath+"/retire", map[string]any{"request_id": "48000000-dddd-4ddd-8ddd-dddddddddddd", "reason": "Retire only after complete dependency and observation proof"}, ready.ReviewETag, 200), 200)
	if !retired.Committed || retired.Rotation == nil || retired.Rotation.Status != "completed" {
		t.Fatal("retirement did not produce a concrete terminal receipt")
	}
	retained.checkRewrapped(t, db, ring, "next", baseline)
	// Retrying start is historical receipt reconciliation, never a new cutover.
	finalReplay := decodeCatalogResponse[rootRotationResultFixture](t, send("POST", "/api/v1/admin/secrets/rotations", startBody, current.ReviewETag, 200), 200)
	if !reflect.DeepEqual(started.Receipt, finalReplay.Receipt) || view().Policy.Epoch != "3" || finalReplay.ApplicationStatus != "superseded" {
		t.Fatal("historical retry replayed cutover or changed creation receipt")
	}
	// Rollback is a durable reverse job across every domain, never restoration of
	// old ciphertext. A partially migrated third-root job makes that distinction real.
	clock.Add(int64(time.Second))
	thirdStartETag := view().ReviewETag
	thirdStarted := decodeCatalogResponse[rootRotationResultFixture](t, send("POST", "/api/v1/admin/secrets/rotations", map[string]any{"request_id": "48000000-1234-4234-8234-123412341234", "target_key_id": "third", "reason": "Controlled partial cutover before reverse rotation"}, thirdStartETag, 201), 201)
	thirdJobID := thirdStarted.Receipt.RotationID
	for range 3 {
		if err := svc.RunSecretRotationOnce(ctx); err != nil {
			t.Fatal("partial third-root migration", err)
		}
	}
	var partiallyMigrated entity.ProviderCredential
	if err := db.Take(&partiallyMigrated, "id = ?", retained.secrets[0].id).Error; err != nil {
		t.Fatal(err)
	}
	if key, err := ring.KeyID(partiallyMigrated.ID, partiallyMigrated.Ciphertext); err != nil || key != "third" {
		t.Fatal("rollback precondition did not migrate any retained real dependency", key, err)
	}
	clock.Add(int64(time.Second))
	rollbackETag := view().ReviewETag
	rollbackBody := map[string]any{"request_id": "48000000-2345-4345-8345-234523452345", "reason": "Reverse partial cutover without restoring historical ciphertext"}
	rolled := decodeCatalogResponse[rootRotationResultFixture](t, send("POST", "/api/v1/admin/secrets/rotations/"+thirdJobID+"/rollback", rollbackBody, rollbackETag, 200), 200)
	if !rolled.Committed || rolled.Receipt.Action != "rollback" || rolled.Receipt.RotationID == thirdJobID || rolled.Rotation == nil || rolled.Rotation.TargetKeyID != "next" || rolled.Rotation.SourceKeyID != "third" {
		t.Fatal("rollback did not create concrete reverse job receipt")
	}
	oldJob := decodeCatalogResponse[rootRotationViewFixture](t, send("GET", "/api/v1/admin/secrets/rotations/"+thirdJobID, nil, "", 200), 200)
	if oldJob.Rotation == nil || oldJob.Rotation.Status != "rolled_back" {
		t.Fatal("original job did not preserve immutable rolled-back history")
	}
	rollbackReplay := decodeCatalogResponse[rootRotationResultFixture](t, send("POST", "/api/v1/admin/secrets/rotations/"+thirdJobID+"/rollback", rollbackBody, rollbackETag, 200), 200)
	if !reflect.DeepEqual(rolled.Receipt, rollbackReplay.Receipt) {
		t.Fatal("reverse-job retry recreated receipt/job")
	}
	for range 50 {
		if err := svc.RunSecretRotationOnce(ctx); err != nil {
			t.Fatal("bounded reverse eight-domain pass", err)
		}
		if state := view(); state.Rotation != nil && state.Rotation.Status == "observing" {
			break
		}
	}
	if state := view(); state.Rotation == nil || state.Rotation.ID != rolled.Receipt.RotationID || state.Rotation.Status != "observing" {
		t.Fatal("reverse job skipped full migration/verification")
	}
	retained.checkRewrapped(t, db, ring, "next", baseline)
	advanceObservation(svc, 300*time.Second)
	reverseReady := view()
	if reverseReady.Rotation == nil || reverseReady.Rotation.Status != "ready" {
		t.Fatal("reverse job lacked independent observation proof")
	}
	send("POST", "/api/v1/admin/secrets/rotations/"+rolled.Receipt.RotationID+"/retire", map[string]any{"request_id": "48000000-3456-4456-8456-345634563456", "reason": "Retire reverse source after complete target-only proof"}, reverseReady.ReviewETag, 200)
	if state := view(); state.Policy.Epoch != "6" || state.Policy.WriteKeyID == nil || *state.Policy.WriteKeyID != "next" {
		t.Fatal("reverse retirement failed to preserve monotonic authoritative epoch")
	}
	retained.checkRewrapped(t, db, ring, "next", baseline)
	stop(svc)
	nextOnly, err := secretstore.NewKeyring(map[string][]byte{"next": nextBytes}, "", "next")
	if err != nil {
		t.Fatal(err)
	}
	svc = newService(nextOnly)
	if err := svc.InitializeSecretStore(ctx); err != nil {
		t.Fatal("target-only startup after complete retirement", err)
	}
	router = routerFor(svc)
	startProcess(svc, true)
	if final := view(); final.Policy.WriteKeyID == nil || *final.Policy.WriteKeyID != "next" || final.Rotation == nil || final.Rotation.Status != "completed" {
		t.Fatal("target-only restart lost saved terminal policy/history")
	}
}

func rootRotationCheckPrivacy(t *testing.T, response *httptest.ResponseRecorder, retained rootRotationRetainedFixture) {
	t.Helper()
	if response.Header().Get("Cache-Control") != "private, no-store" && response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("secret management response could be cached", response.Header())
	}
	if response.Code >= 400 && response.Header().Get("ETag") != "" {
		t.Fatal("rejected secret request advertised successful reviewed state")
	}
	if !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
		t.Fatal("secret management returned a non-JSON envelope")
	}
	var body any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal("secret management returned an invalid JSON envelope")
	}
	for _, secret := range retained.secrets {
		if strings.Contains(response.Body.String(), secret.plaintext) || rootRotationContainsPrivateSubject(body, secret.id) && secret.table != "smtp_settings" {
			t.Fatal("secret management exposed private secret/subject")
		}
	}
	for _, name := range []string{"ciphertext", "proof_ciphertext", "original_digest", "result_digest", "lease_token", "key_manifest_digest"} {
		if strings.Contains(response.Body.String(), `"`+name+`"`) {
			t.Fatal("secret management exposed private proof field", name)
		}
	}
}

// Match complete JSON subjects rather than public inventory names that contain
// them. Decoding also preserves checks for escaped strings and nested subjects.
func rootRotationContainsPrivateSubject(value any, subject string) bool {
	switch value := value.(type) {
	case string:
		return value == subject
	case []any:
		for _, item := range value {
			if rootRotationContainsPrivateSubject(item, subject) {
				return true
			}
		}
	case map[string]any:
		for key, item := range value {
			if key == subject || rootRotationContainsPrivateSubject(item, subject) {
				return true
			}
		}
	}
	return false
}

func TestRootRotationPrivacyMatchesExactPrivateSubjects(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"public inventory domain", `{"domains":[{"code":"oidc_providers","scanned":"1"}]}`, false},
		{"public name containing subject", `{"message":"oidc_providers inventory"}`, false},
		{"exact private subject", `{"id":"oidc"}`, true},
		{"escaped private subject", `{"id":"o\u0069dc"}`, true},
		{"nested private subject", `{"detail":{"id":"oidc"}}`, true},
		{"array private subject", `{"items":["other","oidc"]}`, true},
		{"private subject key", `{"oidc":"redacted"}`, true},
		{"different case", `{"id":"OIDC"}`, false},
	}
	for _, test := range cases {
		var body any
		if err := json.Unmarshal([]byte(test.body), &body); err != nil {
			t.Fatal(test.name, err)
		}
		if got := rootRotationContainsPrivateSubject(body, "oidc"); got != test.want {
			t.Errorf("%s: private subject match=%t want=%t", test.name, got, test.want)
		}
	}
	// Exercise the real privacy helper with the public domain and a retained
	// private subject whose raw substring appears in that domain.
	response := httptest.NewRecorder()
	response.Header().Set("Cache-Control", "private, no-store")
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusOK)
	response.Body.WriteString(cases[0].body)
	rootRotationCheckPrivacy(t, response, rootRotationRetainedFixture{secrets: []rootRotationCipherFixture{{
		table: "oidc_providers", id: "oidc", plaintext: "test-only-retained-oidc-client-secret",
	}}})
}

// Diagnostics contain only fixed stages/classes and a bounded public status.
// Query text, raw errors and SMTP values are never copied to a failure report.
func rootRotationPreparedStage(table string) uint32 {
	switch table {
	case "users", "role_permissions", "p", "user_roles":
		return 1
	case "smtp_settings":
		return 2
	case "governance_settings":
		return 3
	case "secret_write_policies":
		return 4
	case "provider_credentials":
		return 5
	default:
		return 6
	}
}
func rootRotationPreparedDiagnostic(stage uint32, err error, pending bool) (string, string, int) {
	stages := []string{"not_observed", "authority_query", "smtp_row_read", "governance_lock_query", "secret_policy_query", "inventory_query", "other_query"}
	name := "other_query"
	if int(stage) < len(stages) {
		name = stages[stage]
	}
	if pending {
		return name, "still_pending", 0
	}
	if err == nil {
		return name, "returned_nil", 0
	}
	if errors.Is(err, context.Canceled) {
		return name, "context_canceled", 0
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return name, "deadline_exceeded", 0
	}
	var typed *apperrors.Error
	if errors.As(err, &typed) {
		status := typed.Code
		if status < 100 || status > 599 {
			status = 0
		}
		return name, "typed_http_error", status
	}
	return name, "untyped_error", 0
}
func TestRootRotationPreparedDiagnosticNeverCopiesPrivateError(t *testing.T) {
	for _, tc := range []struct {
		err     error
		pending bool
		outcome string
		status  int
	}{
		{nil, true, "still_pending", 0}, {nil, false, "returned_nil", 0},
		{context.Canceled, false, "context_canceled", 0}, {context.DeadlineExceeded, false, "deadline_exceeded", 0},
		{&apperrors.Error{Code: 403, Message: "private token and SMTP body"}, false, "typed_http_error", 403},
		{&apperrors.Error{Code: 10000, Message: "private ciphertext"}, false, "typed_http_error", 0},
		{errors.New("private hostname and token"), false, "untyped_error", 0},
	} {
		stage, outcome, status := rootRotationPreparedDiagnostic(rootRotationPreparedStage("smtp_settings"), tc.err, tc.pending)
		if stage != "smtp_row_read" || outcome != tc.outcome || status != tc.status {
			t.Fatal("diagnostic classification changed")
		}
		if strings.Contains(stage+outcome, "private") {
			t.Fatal("private error escaped")
		}
	}
	stage, _, _ := rootRotationPreparedDiagnostic(^uint32(0), nil, true)
	if stage != "other_query" {
		t.Fatal("unknown stage leaked")
	}
}

// These cumulative facts distinguish an absent query, context mismatch and
// disarmed gate without copying SQL, table names or submitted material.
func rootRotationPreparedFailure(stage uint32, err error, pending, governanceSeen, governanceSchemaSeen, pointerMatched, armedAtMatch, pauseEntered, predispatchMatched, callbackOwnerMatched bool) string {
	name, outcome, status := rootRotationPreparedDiagnostic(stage, err, pending)
	return fmt.Sprintf("prepared epoch diagnostic: stage=%s outcome=%s status=%d governance_seen=%t governance_schema_seen=%t pointer_matched=%t armed_at_match=%t pause_entered=%t predispatch_gate_matched=%t callback_owner_matched=%t", name, outcome, status, governanceSeen, governanceSchemaSeen, pointerMatched, armedAtMatch, pauseEntered, predispatchMatched, callbackOwnerMatched)
}
func TestRootRotationPreparedCumulativeDiagnosticPrivacyAndBounds(t *testing.T) {
	private := errors.New("private SQL table host secret token ciphertext material")
	for bits := uint8(0); bits < 128; bits++ {
		seen, schemaSeen, matched, armed, entered := bits&1 != 0, bits&2 != 0, bits&4 != 0, bits&8 != 0, bits&16 != 0
		predispatchMatched, ownerMatched := bits&32 != 0, bits&64 != 0
		got := rootRotationPreparedFailure(^uint32(0), private, false, seen, schemaSeen, matched, armed, entered, predispatchMatched, ownerMatched)
		expected := fmt.Sprintf("prepared epoch diagnostic: stage=other_query outcome=untyped_error status=0 governance_seen=%t governance_schema_seen=%t pointer_matched=%t armed_at_match=%t pause_entered=%t predispatch_gate_matched=%t callback_owner_matched=%t", seen, schemaSeen, matched, armed, entered, predispatchMatched, ownerMatched)
		if got != expected || strings.Contains(got, private.Error()) {
			t.Fatal("cumulative diagnostic copied unbounded/private input")
		}
	}
	got := rootRotationPreparedFailure(6, nil, false, true, true, true, false, false, true, true)
	if got != "prepared epoch diagnostic: stage=other_query outcome=returned_nil status=0 governance_seen=true governance_schema_seen=true pointer_matched=true armed_at_match=false pause_entered=false predispatch_gate_matched=true callback_owner_matched=true" {
		t.Fatal("successful early writer was represented as a passed gate", got)
	}
}

// Typed Provider Credential reads also occur during runtime refresh. Only the
// bounded root-inventory projection represents the capture the CAS gate needs.
func rootRotationCapturedProviderInventory(tx *gorm.DB) bool {
	if _, legacy := tx.Statement.Dest.(*[]map[string]any); legacy {
		return true
	}
	if _, typed := tx.Statement.Dest.(*[]entity.ProviderCredential); !typed || !slices.Equal(tx.Statement.Selects, []string{"id", "created_at", "storage_source", "ciphertext"}) {
		return false
	}
	limit, ok := tx.Statement.Clauses["LIMIT"].Expression.(clause.Limit)
	return ok && limit.Limit != nil && *limit.Limit > 0 && *limit.Limit <= 50 && limit.Offset == 0
}

func TestRootRotationCASGateDistinguishesCapturedInventoryFromRuntimeReads(t *testing.T) {
	for _, tc := range []struct {
		name    string
		legacy  bool
		selects []string
		limit   *int
		offset  int
		want    bool
	}{
		{name: "historical map inventory", legacy: true, want: true},
		{name: "current bounded inventory", selects: []string{"id", "created_at", "storage_source", "ciphertext"}, limit: new(50), want: true},
		{name: "remaining inventory page", selects: []string{"id", "created_at", "storage_source", "ciphertext"}, limit: new(1), want: true},
		{name: "runtime catalogue", want: false},
		{name: "different projection", selects: []string{"id", "ciphertext"}, limit: new(50)},
		{name: "unbounded typed inventory", selects: []string{"id", "created_at", "storage_source", "ciphertext"}},
		{name: "zero page", selects: []string{"id", "created_at", "storage_source", "ciphertext"}, limit: new(0)},
		{name: "oversize page", selects: []string{"id", "created_at", "storage_source", "ciphertext"}, limit: new(51)},
		{name: "offset page", selects: []string{"id", "created_at", "storage_source", "ciphertext"}, limit: new(50), offset: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := &roleListCountProbePool{}
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, DryRun: true})
			if err != nil {
				t.Fatal(err)
			}
			seen, matched := false, false
			if err := db.Callback().Query().After("gorm:query").Register("test_root_inventory_purpose", func(tx *gorm.DB) {
				if tx.Statement.Table != "provider_credentials" {
					t.Fatal("query target changed")
				}
				seen, matched = true, rootRotationCapturedProviderInventory(tx)
			}); err != nil {
				t.Fatal(err)
			}
			query := db.Table("provider_credentials")
			if tc.selects != nil {
				query = query.Select(tc.selects)
			}
			if tc.limit != nil {
				query = query.Limit(*tc.limit)
			}
			if tc.offset != 0 {
				query = query.Offset(tc.offset)
			}
			if tc.legacy {
				var rows []map[string]any
				err = query.Find(&rows).Error
			} else {
				var rows []entity.ProviderCredential
				err = query.Find(&rows).Error
			}
			if err != nil || !seen || matched != tc.want || pool.queries != 0 {
				t.Fatalf("actual GORM query inventory classification: seen=%t matched=%t want=%t requests=%d err=%v", seen, matched, tc.want, pool.queries, err)
			}
		})
	}
}
