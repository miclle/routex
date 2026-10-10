package handler

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// These fixture projections freeze V95 and the retained pre-V95 authentication
// columns. They are not startup schemas and never AutoMigrate current entities.
type oauthFixtureProviderV95 struct {
	ID                       string     `gorm:"primaryKey;size:30;check:ck_oauth_singleton,OCTET_LENGTH(id) = 5 AND ASCII(SUBSTRING(id,1,1)) = 111 AND ASCII(SUBSTRING(id,2,1)) = 97 AND ASCII(SUBSTRING(id,3,1)) = 117 AND ASCII(SUBSTRING(id,4,1)) = 116 AND ASCII(SUBSTRING(id,5,1)) = 104"`
	ReviewRevision           string     `gorm:"size:64;not null"`
	ConfigRevision           string     `gorm:"size:64;not null"`
	PolicyRevision           string     `gorm:"size:64;not null"`
	Name                     string     `gorm:"size:100;not null"`
	AuthorizationURL         string     `gorm:"size:2048;not null"`
	TokenURL                 string     `gorm:"size:2048;not null"`
	UserInfoURL              string     `gorm:"size:2048;not null"`
	ClientAuthMethod         string     `gorm:"size:30;not null"`
	ScopesJSON               string     `gorm:"type:text;not null"`
	SubjectPathJSON          string     `gorm:"type:text;not null"`
	ClientID                 string     `gorm:"size:256;not null"`
	CallbackURL              string     `gorm:"size:2048;not null"`
	SecretGeneration         string     `gorm:"size:64;not null"`
	AuthCiphertext           string     `gorm:"type:text;not null"`
	Enabled                  bool       `gorm:"not null"`
	VerifiedConfigRevision   string     `gorm:"size:64;not null"`
	VerifiedBy               string     `gorm:"size:30;not null"`
	VerifiedUserCreatedAt    *time.Time `gorm:"precision:6"`
	VerifiedBindingID        string     `gorm:"size:30;not null"`
	VerifiedBindingCreatedAt *time.Time `gorm:"precision:6"`
	CreatedAt                time.Time  `gorm:"precision:6;not null"`
	UpdatedAt                time.Time  `gorm:"precision:6;not null"`
}

func (oauthFixtureProviderV95) TableName() string { return "oauth_providers" }

type oauthFixtureBindingV95 struct {
	ProviderID     string    `gorm:"size:30;not null;check:ck_oauth_binding_provider,OCTET_LENGTH(provider_id) = 5 AND ASCII(SUBSTRING(provider_id,1,1)) = 111 AND ASCII(SUBSTRING(provider_id,2,1)) = 97 AND ASCII(SUBSTRING(provider_id,3,1)) = 117 AND ASCII(SUBSTRING(provider_id,4,1)) = 116 AND ASCII(SUBSTRING(provider_id,5,1)) = 104"`
	SubjectKind    string    `gorm:"size:20;not null;check:ck_oauth_binding_kind,(OCTET_LENGTH(subject_kind) = 6 AND ASCII(SUBSTRING(subject_kind,1,1)) = 115 AND ASCII(SUBSTRING(subject_kind,2,1)) = 116 AND ASCII(SUBSTRING(subject_kind,3,1)) = 114 AND ASCII(SUBSTRING(subject_kind,4,1)) = 105 AND ASCII(SUBSTRING(subject_kind,5,1)) = 110 AND ASCII(SUBSTRING(subject_kind,6,1)) = 103) OR (OCTET_LENGTH(subject_kind) = 7 AND ASCII(SUBSTRING(subject_kind,1,1)) = 105 AND ASCII(SUBSTRING(subject_kind,2,1)) = 110 AND ASCII(SUBSTRING(subject_kind,3,1)) = 116 AND ASCII(SUBSTRING(subject_kind,4,1)) = 101 AND ASCII(SUBSTRING(subject_kind,5,1)) = 103 AND ASCII(SUBSTRING(subject_kind,6,1)) = 101 AND ASCII(SUBSTRING(subject_kind,7,1)) = 114)"`
	ID             string    `gorm:"primaryKey;size:30"`
	UserID         string    `gorm:"size:30;not null;uniqueIndex"`
	UserCreatedAt  time.Time `gorm:"precision:6;not null"`
	ConfigRevision string    `gorm:"size:64;not null"`
	Subject        string    `gorm:"size:256;not null"`
	SubjectDigest  string    `gorm:"size:64;not null;uniqueIndex"`
	CreatedAt      time.Time `gorm:"precision:6;not null"`
}

func (oauthFixtureBindingV95) TableName() string { return "oauth_bindings" }

type oauthFixtureCeremonyV95 struct {
	ProviderID       string     `gorm:"size:30;not null;check:ck_oauth_ceremony_provider,OCTET_LENGTH(provider_id) = 5 AND ASCII(SUBSTRING(provider_id,1,1)) = 111 AND ASCII(SUBSTRING(provider_id,2,1)) = 97 AND ASCII(SUBSTRING(provider_id,3,1)) = 117 AND ASCII(SUBSTRING(provider_id,4,1)) = 116 AND ASCII(SUBSTRING(provider_id,5,1)) = 104"`
	SubjectKind      string     `gorm:"size:20;not null;check:ck_oauth_ceremony_kind,OCTET_LENGTH(subject_kind) = 0 OR (OCTET_LENGTH(subject_kind) = 6 AND ASCII(SUBSTRING(subject_kind,1,1)) = 115 AND ASCII(SUBSTRING(subject_kind,2,1)) = 116 AND ASCII(SUBSTRING(subject_kind,3,1)) = 114 AND ASCII(SUBSTRING(subject_kind,4,1)) = 105 AND ASCII(SUBSTRING(subject_kind,5,1)) = 110 AND ASCII(SUBSTRING(subject_kind,6,1)) = 103) OR (OCTET_LENGTH(subject_kind) = 7 AND ASCII(SUBSTRING(subject_kind,1,1)) = 105 AND ASCII(SUBSTRING(subject_kind,2,1)) = 110 AND ASCII(SUBSTRING(subject_kind,3,1)) = 116 AND ASCII(SUBSTRING(subject_kind,4,1)) = 101 AND ASCII(SUBSTRING(subject_kind,5,1)) = 103 AND ASCII(SUBSTRING(subject_kind,6,1)) = 101 AND ASCII(SUBSTRING(subject_kind,7,1)) = 114)"`
	ID               string     `gorm:"primaryKey;size:30"`
	StateHash        string     `gorm:"size:64;not null;uniqueIndex"`
	CookieHash       string     `gorm:"size:64;not null;uniqueIndex"`
	Purpose          string     `gorm:"size:20;not null"`
	Reason           string     `gorm:"size:1024;not null"`
	Status           string     `gorm:"size:20;not null"`
	ConfigRevision   string     `gorm:"size:64;not null"`
	PolicyRevision   string     `gorm:"size:64;not null"`
	UserID           string     `gorm:"size:30;not null"`
	UserCreatedAt    *time.Time `gorm:"precision:6"`
	PasswordDigest   string     `gorm:"size:64;not null"`
	MFAGeneration    string     `gorm:"size:64;not null"`
	SessionID        string     `gorm:"size:30;not null"`
	SessionCreatedAt *time.Time `gorm:"precision:6"`
	Subject          string     `gorm:"size:256;not null"`
	BindingID        string     `gorm:"size:30;not null"`
	BindingCreatedAt *time.Time `gorm:"precision:6"`
	ExpiresAt        time.Time  `gorm:"precision:6;not null;index"`
	CreatedAt        time.Time  `gorm:"precision:6;not null"`
	VerifiedAt       *time.Time `gorm:"precision:6"`
}

func (oauthFixtureCeremonyV95) TableName() string { return "oauth_ceremonies" }

type oauthFixtureSessionV95 struct {
	PrimaryMethod         string     `gorm:"size:20;not null;default:''"`
	OAuthBindingID        string     `gorm:"column:oauth_binding_id;size:30;not null;default:''"`
	OAuthBindingCreatedAt *time.Time `gorm:"column:oauth_binding_created_at;precision:6"`
	OAuthConfigRevision   string     `gorm:"column:oauth_config_revision;size:64;not null;default:''"`
	OAuthPolicyRevision   string     `gorm:"column:oauth_policy_revision;size:64;not null;default:''"`
	OAuthUserCreatedAt    *time.Time `gorm:"column:oauth_user_created_at;precision:6"`
}

func (oauthFixtureSessionV95) TableName() string { return "sessions" }

type oauthFixtureMFAChallengeV95 struct {
	PrimaryMethod         string     `gorm:"size:20;not null;default:''"`
	OAuthBindingID        string     `gorm:"column:oauth_binding_id;size:30;not null;default:''"`
	OAuthBindingCreatedAt *time.Time `gorm:"column:oauth_binding_created_at;precision:6"`
	OAuthConfigRevision   string     `gorm:"column:oauth_config_revision;size:64;not null;default:''"`
	OAuthPolicyRevision   string     `gorm:"column:oauth_policy_revision;size:64;not null;default:''"`
	OAuthUserCreatedAt    *time.Time `gorm:"column:oauth_user_created_at;precision:6"`
}

func (oauthFixtureMFAChallengeV95) TableName() string { return "mfa_challenges" }

type oauthFixtureSessionProofV95 struct {
	PrimaryMethod string `gorm:"check:ck_sessions_oidc_primary,(OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL)"`
}

func (oauthFixtureSessionProofV95) TableName() string { return "sessions" }

type oauthFixtureMFAProofV95 struct {
	PrimaryMethod string `gorm:"check:ck_mfa_challenges_oidc_primary,(OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL)"`
}

func (oauthFixtureMFAProofV95) TableName() string { return "mfa_challenges" }

type oauthHistoricalSessionV94 struct {
	ID        string    `gorm:"primaryKey;size:30"`
	UserID    string    `gorm:"size:30;not null"`
	TokenHash string    `gorm:"size:64;not null"`
	ExpiresAt time.Time `gorm:"not null"`
	CreatedAt time.Time
}

func (oauthHistoricalSessionV94) TableName() string { return "sessions" }

type oauthHistoricalChallengeV94 struct {
	UserID         string    `gorm:"primaryKey;size:30"`
	Purpose        string    `gorm:"primaryKey;size:20"`
	TokenHash      string    `gorm:"size:64;not null"`
	PasswordDigest string    `gorm:"size:64;not null"`
	Generation     string    `gorm:"size:64;not null"`
	SessionID      string    `gorm:"size:30;not null"`
	Attempts       int       `gorm:"not null"`
	ExpiresAt      time.Time `gorm:"not null"`
}

func (oauthHistoricalChallengeV94) TableName() string { return "mfa_challenges" }

// Each deliberately incompatible column is isolated on the empty ceremony
// table, including optional births: the provider singleton intentionally has NULLs.
type oauthWrongWidthV95 struct {
	Reason string `gorm:"size:1023;not null"`
}

func (oauthWrongWidthV95) TableName() string { return "oauth_ceremonies" }

type oauthWrongTypeV95 struct {
	UserCreatedAt string `gorm:"size:40"`
}

func (oauthWrongTypeV95) TableName() string { return "oauth_ceremonies" }

type oauthWrongPrecisionV95 struct {
	UserCreatedAt *time.Time `gorm:"precision:3"`
}

func (oauthWrongPrecisionV95) TableName() string { return "oauth_ceremonies" }

type oauthWrongOptionalNullV95 struct {
	UserCreatedAt time.Time `gorm:"precision:6;not null"`
}

func (oauthWrongOptionalNullV95) TableName() string { return "oauth_ceremonies" }

type oauthWrongRequiredNullV95 struct {
	Reason *string `gorm:"size:1024"`
}

func (oauthWrongRequiredNullV95) TableName() string { return "oauth_ceremonies" }

type oauthWrongTimeDefaultV95 struct {
	UserCreatedAt *time.Time `gorm:"precision:6;default:CURRENT_TIMESTAMP(6)"`
}

func (oauthWrongTimeDefaultV95) TableName() string { return "oauth_ceremonies" }

type oauthNonuniqueOwnerIndexV95 struct {
	UserID string `gorm:"index:idx_oauth_bindings_user_id"`
}

func (oauthNonuniqueOwnerIndexV95) TableName() string { return "oauth_bindings" }

type oauthWrongOwnerIndexV95 struct {
	SubjectDigest string `gorm:"uniqueIndex:idx_oauth_bindings_user_id"`
}

func (oauthWrongOwnerIndexV95) TableName() string { return "oauth_bindings" }

type oauthExtraOwnerIndexV95 struct {
	UserID         string `gorm:"uniqueIndex:idx_oauth_bindings_user_id,priority:1"`
	ConfigRevision string `gorm:"uniqueIndex:idx_oauth_bindings_user_id,priority:2"`
}

func (oauthExtraOwnerIndexV95) TableName() string { return "oauth_bindings" }

// Fixed V95 result projection: no SELECT * across the additive DDL fixture.
type oauthExactPrimaryFixtureV95 struct {
	PrimaryMethod         string
	OIDCBindingID         string     `gorm:"column:oidc_binding_id"`
	OIDCBindingCreatedAt  *time.Time `gorm:"column:oidc_binding_created_at"`
	OIDCConfigRevision    string     `gorm:"column:oidc_config_revision"`
	OIDCPolicyRevision    string     `gorm:"column:oidc_policy_revision"`
	OIDCUserCreatedAt     *time.Time `gorm:"column:oidc_user_created_at"`
	OAuthBindingID        string     `gorm:"column:oauth_binding_id"`
	OAuthBindingCreatedAt *time.Time `gorm:"column:oauth_binding_created_at"`
	OAuthConfigRevision   string     `gorm:"column:oauth_config_revision"`
	OAuthPolicyRevision   string     `gorm:"column:oauth_policy_revision"`
	OAuthUserCreatedAt    *time.Time `gorm:"column:oauth_user_created_at"`
}

// legacyMigrationBeforeV96 reconstructs only the historical boundary on this
// exclusively owned integration database. LDAP columns/tables remain as a valid
// partial-DDL prefix; no configured identity or V5 inventory may be discarded.
// The caller restores current V96 with normal Migrate after its historical test.
func legacyMigrationBeforeV96(t *testing.T, db *gorm.DB) {
	t.Helper()
	legacyMigrationBeforeV97(t, db)
	ctx := context.Background()
	before := personalKeyBehaviorLedger(t, db)
	if len(before) != 96 || before[95].Version != 96 {
		t.Fatal("historical fixture requires exact current V96 ledger")
	}
	for i, row := range before {
		if row.Version != i+1 {
			t.Fatal("noncontiguous current ledger", i)
		}
	}
	for table, want := range map[string]int64{"ldap_providers": 1, "ldap_bindings": 0} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil || count != want {
			t.Fatal("historical fixture requires pristine LDAP objects", table, count, err)
		}
	}
	var provider entity.LDAPProvider
	if err := db.Session(&gorm.Session{QueryFields: true}).Take(&provider, "id = ?", "ldap").Error; err != nil {
		t.Fatal(err)
	}
	if provider.CreatedAt.IsZero() || provider.UpdatedAt.IsZero() {
		t.Fatal("missing default LDAP birth")
	}
	provider.CreatedAt, provider.UpdatedAt = time.Time{}, time.Time{}
	if !reflect.DeepEqual(provider, entity.LDAPProvider{ID: "ldap", ReviewRevision: strings.Repeat("0", 64), ConfigRevision: strings.Repeat("0", 64), PolicyRevision: strings.Repeat("0", 64), SecretGeneration: "0"}) {
		t.Fatal("historical fixture cannot remove configured LDAP provenance")
	}
	for _, table := range []string{"sessions", "mfa_challenges"} {
		var count int64
		if err := db.Table(table).Where("OCTET_LENGTH(ldap_binding_id) <> 0 OR ldap_binding_created_at IS NOT NULL OR OCTET_LENGTH(ldap_config_revision) <> 0 OR OCTET_LENGTH(ldap_policy_revision) <> 0 OR ldap_user_created_at IS NOT NULL OR primary_method = ?", "ldap").Count(&count).Error; err != nil || count != 0 {
			t.Fatal("historical fixture contains LDAP primary", table, count, err)
		}
	}
	for _, table := range []string{"secret_rotation_jobs", "secret_process_verifications"} {
		var count int64
		if err := db.Table(table).Where("inventory_version > ?", 4).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("historical fixture contains V5 inventory", table, count, err)
		}
	}
	var ldapItems int64
	if err := db.Table("secret_rotation_items").Where("domain = ?", "ldap_providers").Count(&ldapItems).Error; err != nil || ldapItems != 0 {
		t.Fatal("historical fixture contains LDAP root items", ldapItems, err)
	}
	if err := database.MigrateThrough(ctx, db, 95); err == nil {
		t.Fatal("bounded V95 migration accepted newer V96 ledger")
	}
	if !reflect.DeepEqual(before, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("rejected bound changed current ledger")
	}
	for _, check := range []struct {
		model any
		name  string
	}{
		{&oauthFixtureSessionProofV95{}, "ck_sessions_oidc_primary"}, {&oauthFixtureMFAProofV95{}, "ck_mfa_challenges_oidc_primary"},
		{&ldapHistoricalRootJobV95{}, "ck_secret_inventory_version"}, {&ldapHistoricalRootJobV95{}, "ck_secret_rotation_domain"},
		{&ldapHistoricalRootItemV95{}, "ck_secret_item_domain"}, {&ldapHistoricalRootProcessV95{}, "ck_secret_process_inventory_version"},
	} {
		if err := db.Migrator().DropConstraint(check.model, check.name); err != nil {
			t.Fatal("drop current constraint for bounded historical fixture", check.name, err)
		}
		if err := db.Migrator().CreateConstraint(check.model, check.name); err != nil {
			t.Fatal("restore exact frozen V95 constraint", check.name, err)
		}
	}
	removed := db.Table("schema_migrations").Where("version = ?", 96).Delete(&struct{}{})
	if removed.Error != nil || removed.RowsAffected != 1 {
		t.Fatal("remove only V96 ledger in owned historical fixture", removed.Error, removed.RowsAffected)
	}
	if !reflect.DeepEqual(before[:95], personalKeyBehaviorLedger(t, db)) {
		t.Fatal("historical V95 boundary changed a retained ledger row")
	}
}

func testOAuthMigration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	legacyMigrationBeforeV96(t, db)
	ledger := personalKeyBehaviorLedger(t, db)
	if len(ledger) != 95 {
		t.Fatal("OAuth requires the exact 95-version ledger")
	}
	for i, row := range ledger {
		if row.Version != i+1 {
			t.Fatal("noncontiguous released migration prefix", i)
		}
	}
	assertLedger := func(present bool) {
		t.Helper()
		rows := personalKeyBehaviorLedger(t, db)
		want := 94
		if present {
			want = 95
		}
		if len(rows) != want || !reflect.DeepEqual(ledger[:94], rows[:94]) || present && rows[94].Version != 95 {
			t.Fatal("OAuth changed a released ledger row or recorded failed DDL")
		}
	}
	remove95 := func() {
		t.Helper()
		r := db.Table("schema_migrations").Where("version = ?", 95).Delete(&struct{}{})
		if r.Error != nil || r.RowsAffected != 1 {
			t.Fatal("remove only V95", r.Error, r.RowsAffected)
		}
		assertLedger(false)
	}
	migrate := func() {
		t.Helper()
		if err := database.MigrateThrough(ctx, db, 95); err != nil {
			t.Fatal("restore V95", err)
		}
		assertLedger(true)
	}
	readProvider := func() oauthFixtureProviderV95 {
		t.Helper()
		var row oauthFixtureProviderV95
		if err := db.Session(&gorm.Session{QueryFields: true}).Take(&row, "id = ?", "oauth").Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	assertDefault := func() {
		t.Helper()
		for table, want := range map[string]int64{"oauth_providers": 1, "oauth_bindings": 0, "oauth_ceremonies": 0} {
			var count int64
			if err := db.Table(table).Count(&count).Error; err != nil || count != want {
				t.Fatal("fresh OAuth table", table, count, err)
			}
		}
		row := readProvider()
		if row.CreatedAt.IsZero() || row.UpdatedAt.IsZero() {
			t.Fatal("singleton birth absent")
		}
		row.CreatedAt, row.UpdatedAt = time.Time{}, time.Time{}
		want := oauthFixtureProviderV95{ID: "oauth", ReviewRevision: strings.Repeat("0", 64), ConfigRevision: strings.Repeat("0", 64), PolicyRevision: strings.Repeat("0", 64), SecretGeneration: "0", ScopesJSON: "[]", SubjectPathJSON: "[]"}
		if !reflect.DeepEqual(row, want) {
			t.Fatal("default singleton invents enabled configuration or verifier provenance")
		}
	}
	assertDefault()
	for _, bound := range []int{0, -1, 100, 94} {
		before := personalKeyBehaviorLedger(t, db)
		if err := database.MigrateThrough(ctx, db, bound); err == nil {
			t.Fatal("invalid/newer-ledger bound accepted", bound)
		}
		if !reflect.DeepEqual(before, personalKeyBehaviorLedger(t, db)) {
			t.Fatal("rejected bound mutated ledger", bound)
		}
		assertDefault()
	}
	if err := database.MigrateThrough(ctx, db, 95); err != nil {
		t.Fatal("historical V95 bounded equivalent", err)
	}
	if !reflect.DeepEqual(ledger, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("historical V95 bound changed ledger")
	}
	birth := time.Now().UTC().Truncate(time.Microsecond)
	user := entity.User{ID: "usr_oauth_migration", Email: "oauth-migration@example.invalid", Name: "Retained local member", Role: entity.RoleMember, PasswordHash: "retained-not-an-authentication-secret", PersonalGrantRevision: strings.Repeat("0", 64)}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	session := oauthHistoricalSessionV94{ID: "ses_oauth_migration", UserID: user.ID, TokenHash: strings.Repeat("a", 64), ExpiresAt: birth.Add(time.Hour), CreatedAt: birth}
	challenge := oauthHistoricalChallengeV94{UserID: user.ID, Purpose: "login", TokenHash: strings.Repeat("b", 64), PasswordDigest: strings.Repeat("c", 64), Generation: strings.Repeat("d", 64), SessionID: session.ID, Attempts: 2, ExpiresAt: birth.Add(time.Minute)}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&challenge).Error; err != nil {
		t.Fatal(err)
	}
	readOld := func() (oauthHistoricalSessionV94, oauthHistoricalChallengeV94) {
		t.Helper()
		var s oauthHistoricalSessionV94
		var c oauthHistoricalChallengeV94
		// Explicit original result shapes stay stable across the twelve ADD/DROPs.
		if err := db.Select("id", "user_id", "token_hash", "expires_at", "created_at").Take(&s, "id = ?", session.ID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Select("user_id", "purpose", "token_hash", "password_digest", "generation", "session_id", "attempts", "expires_at").Take(&c, "user_id = ? AND purpose = ?", challenge.UserID, challenge.Purpose).Error; err != nil {
			t.Fatal(err)
		}
		return s, c
	}
	originalSession, originalChallenge := readOld()
	oidcProof := map[string]any{"primary_method": "oidc", "oidc_binding_id": "oib_retained_v94", "oidc_binding_created_at": birth, "oidc_config_revision": strings.Repeat("6", 64), "oidc_policy_revision": strings.Repeat("7", 64), "oidc_user_created_at": birth}
	for _, target := range []struct {
		table, query string
		args         []any
	}{{"sessions", "id = ?", []any{session.ID}}, {"mfa_challenges", "user_id = ? AND purpose = ?", []any{challenge.UserID, challenge.Purpose}}} {
		if err := db.Table(target.table).Where(target.query, target.args...).Updates(oidcProof).Error; err != nil {
			t.Fatal("retain positive V94 primary", err)
		}
	}
	readOIDC := func(table, query string, args ...any) oidcFixtureSessionV94 {
		t.Helper()
		var row oidcFixtureSessionV94
		if err := db.Table(table).Select("primary_method", "oidc_binding_id", "oidc_binding_created_at", "oidc_config_revision", "oidc_policy_revision", "oidc_user_created_at").Where(query, args...).Take(&row).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	oldSessionProof := readOIDC("sessions", "id = ?", session.ID)
	oldMFAProof := readOIDC("mfa_challenges", "user_id = ? AND purpose = ?", challenge.UserID, challenge.Purpose)
	retained := func() {
		t.Helper()
		s, c := readOld()
		if !reflect.DeepEqual(s, originalSession) || !reflect.DeepEqual(c, originalChallenge) {
			t.Fatal("V95 changed retained local Session/MFA facts")
		}
		if !reflect.DeepEqual(oldSessionProof, readOIDC("sessions", "id = ?", session.ID)) || !reflect.DeepEqual(oldMFAProof, readOIDC("mfa_challenges", "user_id = ? AND purpose = ?", challenge.UserID, challenge.Purpose)) {
			t.Fatal("V95 changed retained V94 OIDC primary proof")
		}
	}
	blankProvenance := func() {
		t.Helper()
		var s oauthFixtureSessionV95
		var c oauthFixtureMFAChallengeV95
		if err := db.Session(&gorm.Session{QueryFields: true}).Take(&s, "id = ?", session.ID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Session(&gorm.Session{QueryFields: true}).Take(&c, "user_id = ? AND purpose = ?", challenge.UserID, challenge.Purpose).Error; err != nil {
			t.Fatal(err)
		}
		if s.OAuthBindingID != "" || s.OAuthBindingCreatedAt != nil || s.OAuthConfigRevision != "" || s.OAuthPolicyRevision != "" || s.OAuthUserCreatedAt != nil || c.OAuthBindingID != "" || c.OAuthBindingCreatedAt != nil || c.OAuthConfigRevision != "" || c.OAuthPolicyRevision != "" || c.OAuthUserCreatedAt != nil {
			t.Fatal("migration fabricated OAuth login provenance")
		}
	}
	checks := []struct {
		model, proof any
		name         string
	}{{&oauthFixtureSessionV95{}, &oauthFixtureSessionProofV95{}, "ck_sessions_oidc_primary"}, {&oauthFixtureMFAChallengeV95{}, &oauthFixtureMFAProofV95{}, "ck_mfa_challenges_oidc_primary"}}
	fields := []string{"OAuthBindingID", "OAuthBindingCreatedAt", "OAuthConfigRevision", "OAuthPolicyRevision", "OAuthUserCreatedAt"}
	for _, check := range checks {
		if err := db.Migrator().DropConstraint(check.proof, check.name); err != nil {
			t.Fatal(err)
		}
		for _, field := range fields {
			if err := db.Migrator().DropColumn(check.model, field); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, model := range []any{&oauthFixtureCeremonyV95{}, &oauthFixtureBindingV95{}, &oauthFixtureProviderV95{}} {
		if err := db.Migrator().DropTable(model); err != nil {
			t.Fatal(err)
		}
	}
	remove95()
	retained()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- database.MigrateThrough(ctx, db, 95) })
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal("concurrent V95 upgrade", err)
		}
	}
	migrate()
	assertDefault()
	retained()
	blankProvenance()
	providerBaseline := readProvider()
	current := func() {
		t.Helper()
		retained()
		blankProvenance()
		assertLedger(true)
		if !reflect.DeepEqual(providerBaseline, readProvider()) {
			t.Fatal("V95 replay replaced singleton birth/configuration")
		}
		for _, check := range checks {
			if !db.Migrator().HasConstraint(check.proof, check.name) {
				t.Fatal("missing primary proof check", check.name)
			}
		}
		for _, idx := range []struct {
			model any
			name  string
		}{
			{&oauthFixtureBindingV95{}, "idx_oauth_bindings_user_id"},
			{&oauthFixtureBindingV95{}, "idx_oauth_bindings_subject_digest"},
			{&oauthFixtureCeremonyV95{}, "idx_oauth_ceremonies_state_hash"},
			{&oauthFixtureCeremonyV95{}, "idx_oauth_ceremonies_cookie_hash"},
			{&oauthFixtureCeremonyV95{}, "idx_oauth_ceremonies_expires_at"},
		} {
			if !db.Migrator().HasIndex(idx.model, idx.name) {
				t.Fatal("missing exact V95 index", idx.name)
			}
		}
	}
	// Interrupted additive DDL: keep four installed OAuth columns and all new tables.
	if err := db.Migrator().DropConstraint(checks[0].proof, checks[0].name); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(checks[0].model, "OAuthPolicyRevision"); err != nil {
		t.Fatal(err)
	}
	remove95()
	migrate()
	current()
	remove95()
	migrate()
	migrate()
	current()

	for _, bad := range []struct {
		name, field string
		model       any
	}{
		{"width", "Reason", &oauthWrongWidthV95{}},
		{"type", "UserCreatedAt", &oauthWrongTypeV95{}},
		{"precision", "UserCreatedAt", &oauthWrongPrecisionV95{}},
		{"optional birth NOT NULL", "UserCreatedAt", &oauthWrongOptionalNullV95{}},
		{"required reason nullable", "Reason", &oauthWrongRequiredNullV95{}},
		{"invented birth default", "UserCreatedAt", &oauthWrongTimeDefaultV95{}},
	} {
		current() // A valid exact schema/actor history precedes every negative.
		var count int64
		if err := db.Table("oauth_ceremonies").Count(&count).Error; err != nil || count != 0 {
			t.Fatal("schema negative requires empty ceremony table", err)
		}
		if err := db.Migrator().AlterColumn(bad.model, bad.field); err != nil {
			t.Fatal("install incompatible fixture", bad.name, err)
		}
		remove95()
		if err := database.MigrateThrough(ctx, db, 95); err == nil {
			t.Fatal("incompatible OAuth column accepted", bad.name)
		}
		assertLedger(false)
		retained()
		if err := db.Migrator().AlterColumn(&oauthFixtureCeremonyV95{}, bad.field); err != nil {
			t.Fatal("restore exact frozen column", bad.name, err)
		}
		migrate()
		current()
	}
	const ownerIndex = "idx_oauth_bindings_user_id"
	for _, bad := range []struct {
		name  string
		model any
	}{
		{"not unique", &oauthNonuniqueOwnerIndexV95{}},
		{"wrong column", &oauthWrongOwnerIndexV95{}},
		{"extra column", &oauthExtraOwnerIndexV95{}},
	} {
		current()
		if err := database.DropIndex(db, &oauthFixtureBindingV95{}, ownerIndex); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().CreateIndex(bad.model, ownerIndex); err != nil {
			t.Fatal(err)
		}
		remove95()
		if err := database.MigrateThrough(ctx, db, 95); err == nil {
			t.Fatal("wrong-shape OAuth index accepted", bad.name)
		}
		assertLedger(false)
		retained()
		if err := database.DropIndex(db, &oauthFixtureBindingV95{}, ownerIndex); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().CreateIndex(&oauthFixtureBindingV95{}, ownerIndex); err != nil {
			t.Fatal(err)
		}
		migrate()
		current()
	}
	// Unique owner and byte-digest keys are independently exercised. The digest
	// is authoritative for exact subjects; no case-insensitive Subject uniqueness
	// is introduced by this fixture.
	binding := oauthFixtureBindingV95{ID: "oab_migration", UserID: user.ID, UserCreatedAt: birth, ConfigRevision: strings.Repeat("e", 64), ProviderID: "oauth", SubjectKind: "string", Subject: "ExactSubject", SubjectDigest: strings.Repeat("f", 64), CreatedAt: birth}
	if err := db.Create(&binding).Error; err != nil {
		t.Fatal("positive binding", err)
	}
	readBinding := func() oauthFixtureBindingV95 {
		t.Helper()
		var row oauthFixtureBindingV95
		if err := db.Session(&gorm.Session{QueryFields: true}).Take(&row, "id = ?", binding.ID).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	bindingBaseline := readBinding()
	for _, field := range []string{"owner", "subject digest"} {
		if !reflect.DeepEqual(bindingBaseline, readBinding()) {
			t.Fatal("positive binding prerequisite")
		}
		candidate := binding
		candidate.ID = "oab_duplicate"
		if field == "owner" {
			candidate.SubjectDigest = strings.Repeat("1", 64)
		} else {
			candidate.UserID = "usr_other_oauth"
		}
		if err := db.Create(&candidate).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
			t.Fatal("missing unique binding fence", field, err)
		}
		var count int64
		if err := db.Table("oauth_bindings").Count(&count).Error; err != nil || count != 1 || !reflect.DeepEqual(bindingBaseline, readBinding()) {
			t.Fatal("unique rejection changed bindings", field, err)
		}
	}
	for _, bad := range []map[string]any{{"provider_id": "OAUTH"}, {"subject_kind": "STRING"}, {"subject_kind": "number"}, {"subject_kind": ""}} {
		if !reflect.DeepEqual(bindingBaseline, readBinding()) {
			t.Fatal("typed binding positive prerequisite")
		}
		if err := db.Model(&oauthFixtureBindingV95{}).Where("id = ?", binding.ID).Updates(bad).Error; err == nil {
			t.Fatal("invalid typed provider/subject accepted", bad)
		}
		if !reflect.DeepEqual(bindingBaseline, readBinding()) {
			t.Fatal("typed binding negative changed retained facts")
		}
	}
	// Updating the existing singleton cannot be masked by a duplicate primary
	// key on a case-insensitive database; every alias must hit the exact CHECK.
	for _, alias := range []string{"OAUTH", "oAuth", "oauth ", "other"} {
		if !reflect.DeepEqual(providerBaseline, readProvider()) {
			t.Fatal("positive singleton prerequisite")
		}
		if err := db.Model(&oauthFixtureProviderV95{}).Where("id = ?", "oauth").UpdateColumn("ID", alias).Error; err == nil {
			t.Fatal("singleton alias accepted", alias)
		}
		if !reflect.DeepEqual(providerBaseline, readProvider()) {
			t.Fatal("singleton rejection rewrote configuration")
		}
	}
	// The same subject spelling with distinct kinds is retained as two identities;
	// their namespace digests are supplied independently of database collation.
	for i, kind := range []string{"string", "integer"} {
		member := user
		member.ID = []string{"usr_oauth_text", "usr_oauth_integer"}[i]
		member.Email = []string{"oauth-text@example.invalid", "oauth-integer@example.invalid"}[i]
		if err := db.Create(&member).Error; err != nil {
			t.Fatal(err)
		}
		candidate := binding
		candidate.ID = []string{"oab_typed_text", "oab_typed_integer"}[i]
		candidate.UserID = member.ID
		candidate.SubjectKind = kind
		candidate.Subject = "42"
		candidate.SubjectDigest = strings.Repeat([]string{"3", "4"}[i], 64)
		if err := db.Create(&candidate).Error; err != nil {
			t.Fatal("distinct typed subject identity rejected", kind, err)
		}
	}
	// End the retained-OIDC upgrade segment before independent OAuth constraint transitions.
	for _, target := range []struct {
		table, query string
		args         []any
	}{{"sessions", "id = ?", []any{session.ID}}, {"mfa_challenges", "user_id = ? AND purpose = ?", []any{challenge.UserID, challenge.Purpose}}} {
		if err := db.Table(target.table).Where(target.query, target.args...).Updates(map[string]any{"primary_method": "", "oidc_binding_id": "", "oidc_binding_created_at": nil, "oidc_config_revision": "", "oidc_policy_revision": "", "oidc_user_created_at": nil}).Error; err != nil {
			t.Fatal(err)
		}
	}
	oldSessionProof = readOIDC("sessions", "id = ?", session.ID)
	oldMFAProof = readOIDC("mfa_challenges", "user_id = ? AND purpose = ?", challenge.UserID, challenge.Purpose)
	// Exercise both new existing-table CHECKs using valid complete provenance
	// before each malformed update; no application validator substitutes for DB.
	for _, target := range []struct {
		model any
		query string
		args  []any
	}{
		{&oauthFixtureSessionV95{}, "id = ?", []any{session.ID}},
		{&oauthFixtureMFAChallengeV95{}, "user_id = ? AND purpose = ?", []any{challenge.UserID, challenge.Purpose}},
	} {
		valid := map[string]any{"primary_method": "oauth", "oauth_binding_id": binding.ID, "oauth_binding_created_at": birth, "oauth_config_revision": strings.Repeat("e", 64), "oauth_policy_revision": strings.Repeat("2", 64), "oauth_user_created_at": birth}
		readProof := func() oauthFixtureSessionV95 {
			t.Helper()
			var row oauthFixtureSessionV95
			// Use the known fixed target table and fixed six-column projection.
			table := "sessions"
			if _, ok := target.model.(*oauthFixtureMFAChallengeV95); ok {
				table = "mfa_challenges"
			}
			if err := db.Table(table).Select("primary_method", "oauth_binding_id", "oauth_binding_created_at", "oauth_config_revision", "oauth_policy_revision", "oauth_user_created_at").Where(target.query, target.args...).Take(&row).Error; err != nil {
				t.Fatal(err)
			}
			return row
		}
		for _, bad := range []map[string]any{
			{"primary_method": "OAUTH"}, {"oauth_binding_created_at": nil}, {"oauth_user_created_at": nil},
			{"oauth_binding_id": ""}, {"oauth_config_revision": "short"}, {"oauth_policy_revision": ""}, {"primary_method": ""}, {"primary_method": "oidc"}, {"oidc_binding_id": "mixed"}, {"oidc_binding_created_at": birth}, {"oidc_config_revision": strings.Repeat("9", 64)}, {"oidc_policy_revision": strings.Repeat("9", 64)}, {"oidc_user_created_at": birth},
		} {
			r := db.Model(target.model).Where(target.query, target.args...).Updates(valid)
			if r.Error != nil || r.RowsAffected != 1 {
				t.Fatal("positive complete OAuth proof", r.Error, r.RowsAffected)
			}
			baseline := readProof()
			if baseline.PrimaryMethod != "oauth" || baseline.OAuthBindingCreatedAt == nil || baseline.OAuthUserCreatedAt == nil {
				t.Fatal("positive OAuth proof not stored")
			}
			if err := db.Model(target.model).Where(target.query, target.args...).Updates(bad).Error; err == nil {
				t.Fatal("partial/uppercase OAuth proof accepted", bad)
			}
			if !reflect.DeepEqual(baseline, readProof()) {
				t.Fatal("rejected OAuth proof changed retained provenance")
			}
			// Make the next positive an actual transition on MySQL too; an
			// unchanged UPDATE may legitimately report zero affected rows.
			local := map[string]any{"primary_method": "", "oauth_binding_id": "", "oauth_binding_created_at": nil, "oauth_config_revision": "", "oauth_policy_revision": "", "oauth_user_created_at": nil}
			if err := db.Model(target.model).Where(target.query, target.args...).Updates(local).Error; err != nil {
				t.Fatal("restore local between proof controls", err)
			}
		}
		local := map[string]any{"primary_method": "", "oauth_binding_id": "", "oauth_binding_created_at": nil, "oauth_config_revision": "", "oauth_policy_revision": "", "oauth_user_created_at": nil}
		if err := db.Model(target.model).Where(target.query, target.args...).Updates(local).Error; err != nil {
			t.Fatal("restore retained local proof", err)
		}
		if got := readProof(); got != (oauthFixtureSessionV95{}) {
			t.Fatal("local proof not blank")
		}
	}
	// Exact supported primary methods remain distinct under both drivers.
	// A positive complete stored proof precedes every whitespace/padded/mixed denial.
	for _, target := range []struct {
		table, query string
		args         []any
	}{{"sessions", "id = ?", []any{session.ID}}, {"mfa_challenges", "user_id = ? AND purpose = ?", []any{challenge.UserID, challenge.Purpose}}} {
		readExact := func() oauthExactPrimaryFixtureV95 {
			t.Helper()
			var row oauthExactPrimaryFixtureV95
			if err := db.Table(target.table).Select("primary_method", "oidc_binding_id", "oidc_binding_created_at", "oidc_config_revision", "oidc_policy_revision", "oidc_user_created_at", "oauth_binding_id", "oauth_binding_created_at", "oauth_config_revision", "oauth_policy_revision", "oauth_user_created_at").Where(target.query, target.args...).Take(&row).Error; err != nil {
				t.Fatal(err)
			}
			return row
		}
		blank := map[string]any{"primary_method": "", "oidc_binding_id": "", "oidc_binding_created_at": nil, "oidc_config_revision": "", "oidc_policy_revision": "", "oidc_user_created_at": nil, "oauth_binding_id": "", "oauth_binding_created_at": nil, "oauth_config_revision": "", "oauth_policy_revision": "", "oauth_user_created_at": nil}
		for _, method := range []string{"", "oidc", "oauth"} {
			valid := make(map[string]any, len(blank))
			for key, value := range blank {
				valid[key] = value
			}
			valid["primary_method"] = method
			want := oauthExactPrimaryFixtureV95{}
			if method != "" {
				valid[method+"_binding_id"] = binding.ID
				valid[method+"_binding_created_at"] = birth
				valid[method+"_config_revision"] = strings.Repeat("e", 64)
				valid[method+"_policy_revision"] = strings.Repeat("2", 64)
				valid[method+"_user_created_at"] = birth
				if method == "oidc" {
					valid["oidc_binding_id"] = "oib_retained_v94"
					want = oauthExactPrimaryFixtureV95{PrimaryMethod: "oidc", OIDCBindingID: "oib_retained_v94", OIDCBindingCreatedAt: &birth, OIDCConfigRevision: strings.Repeat("e", 64), OIDCPolicyRevision: strings.Repeat("2", 64), OIDCUserCreatedAt: &birth}
				} else {
					want = oauthExactPrimaryFixtureV95{PrimaryMethod: "oauth", OAuthBindingID: binding.ID, OAuthBindingCreatedAt: &birth, OAuthConfigRevision: strings.Repeat("e", 64), OAuthPolicyRevision: strings.Repeat("2", 64), OAuthUserCreatedAt: &birth}
				}
			}
			bad := []map[string]any{{"primary_method": " "}, {"primary_method": "\t"}, {"primary_method": method + " "}, {"primary_method": " " + method}, {"primary_method": "local"}}
			if method != "" {
				bad = append(bad, map[string]any{"primary_method": strings.ToUpper(method)})
			}
			blanks := []string{"oidc_binding_id", "oidc_config_revision", "oidc_policy_revision", "oauth_binding_id", "oauth_config_revision", "oauth_policy_revision"}
			if method == "oidc" {
				blanks = []string{"oauth_binding_id", "oauth_config_revision", "oauth_policy_revision"}
				bad = append(bad, map[string]any{"oauth_binding_created_at": birth}, map[string]any{"oauth_user_created_at": birth})
			}
			if method == "oauth" {
				blanks = []string{"oidc_binding_id", "oidc_config_revision", "oidc_policy_revision"}
				bad = append(bad, map[string]any{"oidc_binding_created_at": birth}, map[string]any{"oidc_user_created_at": birth})
			}
			for _, column := range blanks {
				bad = append(bad, map[string]any{column: " "}, map[string]any{column: "  "}, map[string]any{column: "\t"})
			}
			for _, mutation := range bad {
				if err := db.Table(target.table).Where(target.query, target.args...).Updates(valid).Error; err != nil {
					t.Fatal("positive exact supported primary", target.table, method, err)
				}
				baseline := readExact()
				sameBirth := func(actual, expected *time.Time) bool {
					if actual == nil || expected == nil {
						return actual == nil && expected == nil
					}
					return actual.Equal(*expected)
				}
				if !sameBirth(baseline.OIDCBindingCreatedAt, want.OIDCBindingCreatedAt) || !sameBirth(baseline.OIDCUserCreatedAt, want.OIDCUserCreatedAt) || !sameBirth(baseline.OAuthBindingCreatedAt, want.OAuthBindingCreatedAt) || !sameBirth(baseline.OAuthUserCreatedAt, want.OAuthUserCreatedAt) {
					t.Fatal("positive exact primary birth instant not stored", target.table, method)
				}
				// Time-zone representation is driver-specific; birth instants above remain
				// exact, and the original full baseline below proves immutable readback.
				comparable, expected := baseline, want
				comparable.OIDCBindingCreatedAt, comparable.OIDCUserCreatedAt, comparable.OAuthBindingCreatedAt, comparable.OAuthUserCreatedAt = nil, nil, nil, nil
				expected.OIDCBindingCreatedAt, expected.OIDCUserCreatedAt, expected.OAuthBindingCreatedAt, expected.OAuthUserCreatedAt = nil, nil, nil, nil
				if !reflect.DeepEqual(comparable, expected) {
					t.Fatal("positive exact primary proof not stored", target.table, method)
				}
				if err := db.Table(target.table).Where(target.query, target.args...).Updates(mutation).Error; err == nil {
					t.Fatal("whitespace/padded/mixed primary accepted", target.table, method, mutation)
				}
				if !reflect.DeepEqual(baseline, readExact()) {
					t.Fatal("rejected exact-blank update changed provenance", target.table, method)
				}
			}
		}
		if err := db.Table(target.table).Where(target.query, target.args...).Updates(blank).Error; err != nil {
			t.Fatal("restore exact local proof", err)
		}
		if got := readExact(); got != (oauthExactPrimaryFixtureV95{}) {
			t.Fatal("exact local restoration not blank")
		}
	}
	// Pending ceremonies permit only the truly empty kind, not a padded alias.
	ceremony := oauthFixtureCeremonyV95{ID: "oac_exact_blank", ProviderID: "oauth", StateHash: strings.Repeat("8", 64), CookieHash: strings.Repeat("9", 64), Purpose: "login", Reason: "Exact empty subject-kind control", Status: "pending", ConfigRevision: strings.Repeat("0", 64), PolicyRevision: strings.Repeat("0", 64), ExpiresAt: birth.Add(time.Minute), CreatedAt: birth}
	if err := db.Create(&ceremony).Error; err != nil {
		t.Fatal("positive pending ceremony", err)
	}
	readKind := func() string {
		t.Helper()
		var row struct{ SubjectKind string }
		if err := db.Table("oauth_ceremonies").Select("subject_kind").Take(&row, "id = ?", ceremony.ID).Error; err != nil {
			t.Fatal(err)
		}
		return row.SubjectKind
	}
	for _, kind := range []string{"", "string", "integer"} {
		for _, bad := range []string{" ", "  ", "\t", kind + " ", " " + kind} {
			if err := db.Model(&oauthFixtureCeremonyV95{}).Where("id = ?", ceremony.ID).UpdateColumn("subject_kind", kind).Error; err != nil {
				t.Fatal("positive exact ceremony kind", err)
			}
			if readKind() != kind {
				t.Fatal("positive ceremony kind not retained")
			}
			if err := db.Model(&oauthFixtureCeremonyV95{}).Where("id = ?", ceremony.ID).UpdateColumn("subject_kind", bad).Error; err == nil {
				t.Fatal("whitespace ceremony kind accepted")
			}
			if readKind() != kind {
				t.Fatal("failed kind update changed ceremony")
			}
		}
	}
	if result := db.Delete(&ceremony); result.Error != nil || result.RowsAffected != 1 {
		t.Fatal("remove owned kind fixture", result.Error, result.RowsAffected)
	}
	// V94 has no OAuth provenance check. An exact V94 check plus a retained
	// whitespace OAuth field must fail V95 prevalidation, not be normalized away.
	if err := db.Migrator().DropConstraint(&oauthFixtureSessionProofV95{}, "ck_sessions_oidc_primary"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().CreateConstraint(&oidcFixtureSessionProofV94{}, "ck_sessions_oidc_primary"); err != nil {
		t.Fatal(err)
	}
	if err := db.Table("sessions").Where("id = ?", session.ID).UpdateColumn("oauth_binding_id", " ").Error; err != nil {
		t.Fatal("install retained unproven OAuth field under historical check", err)
	}
	remove95()
	if err := database.MigrateThrough(ctx, db, 95); err == nil {
		t.Fatal("V95 accepted retained whitespace blank provenance")
	}
	assertLedger(false)
	var retainedBlank struct {
		OAuthBindingID string `gorm:"column:oauth_binding_id"`
	}
	if err := db.Table("sessions").Select("oauth_binding_id").Take(&retainedBlank, "id = ?", session.ID).Error; err != nil || retainedBlank.OAuthBindingID != " " {
		t.Fatal("failed upgrade rewrote unproven provenance", err)
	}
	if err := db.Table("sessions").Where("id = ?", session.ID).UpdateColumn("oauth_binding_id", "").Error; err != nil {
		t.Fatal("restore owned exact blank", err)
	}
	migrate()
	current()
	// Successful repeated startup must preserve the actual retained binding and
	// singleton, not merely their counts or freshly synthesized timestamps.
	remove95()
	migrate()
	current()
	if !reflect.DeepEqual(bindingBaseline, readBinding()) {
		t.Fatal("V95 repeat changed exact binding subject/birth")
	}
	// Restore full current after all exact V94/V95 historical assertions.
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("restore current after bounded OAuth fixture", err)
	}
	finalLedger := personalKeyBehaviorLedger(t, db)
	if len(finalLedger) != 99 || finalLedger[98].Version != 99 || finalLedger[97].Version != 98 || finalLedger[96].Version != 97 || !reflect.DeepEqual(ledger[:94], finalLedger[:94]) || finalLedger[94].Version != 95 || finalLedger[95].Version != 96 {
		t.Fatal("historical OAuth closure lost current suffix or retained prefix")
	}
}
