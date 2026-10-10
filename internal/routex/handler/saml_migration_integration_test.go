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

// These literal projections are frozen test definitions of V97, not evolving entities.
// samlFixtureProviderV97 contains public trust configuration, never a private signing key.
type samlFixtureProviderV97 struct {
	ID                       string     `gorm:"primaryKey;size:30;check:ck_saml_singleton,OCTET_LENGTH(id) = 4 AND ASCII(SUBSTRING(id,1,1)) = 115 AND ASCII(SUBSTRING(id,2,1)) = 97 AND ASCII(SUBSTRING(id,3,1)) = 109 AND ASCII(SUBSTRING(id,4,1)) = 108"`
	ReviewRevision           string     `gorm:"size:64;not null"`
	ConfigRevision           string     `gorm:"size:64;not null"`
	PolicyRevision           string     `gorm:"size:64;not null"`
	Name                     string     `gorm:"size:100;not null"`
	IDPIssuer                string     `gorm:"size:2048;not null"`
	SSOURL                   string     `gorm:"size:2048;not null"`
	SPEntityID               string     `gorm:"size:2048;not null"`
	ACSURL                   string     `gorm:"size:2048;not null"`
	SigningCertificatePEM    string     `gorm:"type:text;not null"`
	Enabled                  bool       `gorm:"not null"`
	VerifiedConfigRevision   string     `gorm:"size:64;not null"`
	VerifiedBy               string     `gorm:"size:30;not null"`
	VerifiedUserCreatedAt    *time.Time `gorm:"precision:6"`
	VerifiedBindingID        string     `gorm:"size:30;not null"`
	VerifiedBindingCreatedAt *time.Time `gorm:"precision:6"`
	CreatedAt                time.Time  `gorm:"precision:6;not null"`
	UpdatedAt                time.Time  `gorm:"precision:6;not null"`
}

func (samlFixtureProviderV97) TableName() string { return "saml_providers" }

// Subjects and ceremony proofs are never public entity projections.
type samlFixtureBindingV97 struct {
	ID             string    `gorm:"primaryKey;size:30"`
	ProviderID     string    `gorm:"size:30;not null;check:ck_saml_binding_provider,OCTET_LENGTH(provider_id) = 4 AND ASCII(SUBSTRING(provider_id,1,1)) = 115 AND ASCII(SUBSTRING(provider_id,2,1)) = 97 AND ASCII(SUBSTRING(provider_id,3,1)) = 109 AND ASCII(SUBSTRING(provider_id,4,1)) = 108"`
	UserID         string    `gorm:"size:30;not null;uniqueIndex"`
	UserCreatedAt  time.Time `gorm:"precision:6;not null"`
	ConfigRevision string    `gorm:"size:64;not null"`
	Issuer         string    `gorm:"size:2048;not null" json:"-"`
	Subject        string    `gorm:"size:256;not null" json:"-"`
	SubjectDigest  string    `gorm:"size:64;not null;uniqueIndex" json:"-"`
	CreatedAt      time.Time `gorm:"precision:6;not null"`
}

func (samlFixtureBindingV97) TableName() string { return "saml_bindings" }

type samlFixtureCeremonyV97 struct {
	ID                string     `gorm:"primaryKey;size:30"`
	RequestID         string     `gorm:"size:128;not null;uniqueIndex" json:"-"`
	RelayHash         string     `gorm:"size:64;not null;uniqueIndex" json:"-"`
	CookieHash        string     `gorm:"size:64;not null;uniqueIndex" json:"-"`
	DeliveryHash      string     `gorm:"size:64;not null" json:"-"`
	Purpose           string     `gorm:"size:20;not null;check:ck_saml_ceremony_purpose,(OCTET_LENGTH(purpose) = 5 AND ASCII(SUBSTRING(purpose,1,1)) = 108 AND ASCII(SUBSTRING(purpose,2,1)) = 111 AND ASCII(SUBSTRING(purpose,3,1)) = 103 AND ASCII(SUBSTRING(purpose,4,1)) = 105 AND ASCII(SUBSTRING(purpose,5,1)) = 110) OR (OCTET_LENGTH(purpose) = 4 AND ASCII(SUBSTRING(purpose,1,1)) = 98 AND ASCII(SUBSTRING(purpose,2,1)) = 105 AND ASCII(SUBSTRING(purpose,3,1)) = 110 AND ASCII(SUBSTRING(purpose,4,1)) = 100) OR (OCTET_LENGTH(purpose) = 6 AND ASCII(SUBSTRING(purpose,1,1)) = 118 AND ASCII(SUBSTRING(purpose,2,1)) = 101 AND ASCII(SUBSTRING(purpose,3,1)) = 114 AND ASCII(SUBSTRING(purpose,4,1)) = 105 AND ASCII(SUBSTRING(purpose,5,1)) = 102 AND ASCII(SUBSTRING(purpose,6,1)) = 121)"`
	Reason            string     `gorm:"size:1024;not null"`
	Status            string     `gorm:"size:20;not null;check:ck_saml_ceremony_status,(OCTET_LENGTH(status) = 7 AND ASCII(SUBSTRING(status,1,1)) = 112 AND ASCII(SUBSTRING(status,2,1)) = 101 AND ASCII(SUBSTRING(status,3,1)) = 110 AND ASCII(SUBSTRING(status,4,1)) = 100 AND ASCII(SUBSTRING(status,5,1)) = 105 AND ASCII(SUBSTRING(status,6,1)) = 110 AND ASCII(SUBSTRING(status,7,1)) = 103) OR (OCTET_LENGTH(status) = 10 AND ASCII(SUBSTRING(status,1,1)) = 118 AND ASCII(SUBSTRING(status,2,1)) = 97 AND ASCII(SUBSTRING(status,3,1)) = 108 AND ASCII(SUBSTRING(status,4,1)) = 105 AND ASCII(SUBSTRING(status,5,1)) = 100 AND ASCII(SUBSTRING(status,6,1)) = 97 AND ASCII(SUBSTRING(status,7,1)) = 116 AND ASCII(SUBSTRING(status,8,1)) = 105 AND ASCII(SUBSTRING(status,9,1)) = 110 AND ASCII(SUBSTRING(status,10,1)) = 103) OR (OCTET_LENGTH(status) = 8 AND ASCII(SUBSTRING(status,1,1)) = 118 AND ASCII(SUBSTRING(status,2,1)) = 101 AND ASCII(SUBSTRING(status,3,1)) = 114 AND ASCII(SUBSTRING(status,4,1)) = 105 AND ASCII(SUBSTRING(status,5,1)) = 102 AND ASCII(SUBSTRING(status,6,1)) = 105 AND ASCII(SUBSTRING(status,7,1)) = 101 AND ASCII(SUBSTRING(status,8,1)) = 100) OR (OCTET_LENGTH(status) = 8 AND ASCII(SUBSTRING(status,1,1)) = 99 AND ASCII(SUBSTRING(status,2,1)) = 111 AND ASCII(SUBSTRING(status,3,1)) = 110 AND ASCII(SUBSTRING(status,4,1)) = 115 AND ASCII(SUBSTRING(status,5,1)) = 117 AND ASCII(SUBSTRING(status,6,1)) = 109 AND ASCII(SUBSTRING(status,7,1)) = 101 AND ASCII(SUBSTRING(status,8,1)) = 100) OR (OCTET_LENGTH(status) = 6 AND ASCII(SUBSTRING(status,1,1)) = 102 AND ASCII(SUBSTRING(status,2,1)) = 97 AND ASCII(SUBSTRING(status,3,1)) = 105 AND ASCII(SUBSTRING(status,4,1)) = 108 AND ASCII(SUBSTRING(status,5,1)) = 101 AND ASCII(SUBSTRING(status,6,1)) = 100)"`
	ProviderCreatedAt time.Time  `gorm:"precision:6;not null"`
	ConfigRevision    string     `gorm:"size:64;not null"`
	PolicyRevision    string     `gorm:"size:64;not null"`
	UserID            string     `gorm:"size:30;not null" json:"-"`
	UserCreatedAt     *time.Time `gorm:"precision:6" json:"-"`
	PasswordDigest    string     `gorm:"size:64;not null" json:"-"`
	MFAGeneration     string     `gorm:"size:64;not null" json:"-"`
	SessionID         string     `gorm:"size:30;not null" json:"-"`
	SessionCreatedAt  *time.Time `gorm:"precision:6" json:"-"`
	Issuer            string     `gorm:"size:2048;not null" json:"-"`
	Subject           string     `gorm:"size:256;not null" json:"-"`
	AssertionDigest   string     `gorm:"size:64;not null" json:"-"`
	BindingID         string     `gorm:"size:30;not null" json:"-"`
	BindingCreatedAt  *time.Time `gorm:"precision:6" json:"-"`
	ProofExpiresAt    *time.Time `gorm:"precision:6" json:"-"`
	ExpiresAt         time.Time  `gorm:"precision:6;not null;index"`
	CreatedAt         time.Time  `gorm:"precision:6;not null"`
	VerifiedAt        *time.Time `gorm:"precision:6"`
	ConsumedAt        *time.Time `gorm:"precision:6"`
}

func (samlFixtureCeremonyV97) TableName() string { return "saml_ceremonies" }

// Replay receipts survive provider changes until the signed proof is expired.
type samlFixtureReceiptV97 struct {
	Digest    string    `gorm:"primaryKey;size:64" json:"-"`
	ExpiresAt time.Time `gorm:"precision:6;not null;index"`
	CreatedAt time.Time `gorm:"precision:6;not null"`
}

func (samlFixtureReceiptV97) TableName() string { return "saml_assertion_receipts" }

type samlFixtureSessionV97 struct {
	SAMLBindingID        string     `gorm:"column:saml_binding_id;size:30;not null;default:''"`
	SAMLBindingCreatedAt *time.Time `gorm:"column:saml_binding_created_at;precision:6"`
	SAMLConfigRevision   string     `gorm:"column:saml_config_revision;size:64;not null;default:''"`
	SAMLPolicyRevision   string     `gorm:"column:saml_policy_revision;size:64;not null;default:''"`
	SAMLUserCreatedAt    *time.Time `gorm:"column:saml_user_created_at;precision:6"`
}

func (samlFixtureSessionV97) TableName() string { return "sessions" }

type samlFixtureMFAV97 samlFixtureSessionV97

func (samlFixtureMFAV97) TableName() string { return "mfa_challenges" }

type samlFixtureSessionProofV97 struct {
	PrimaryMethod string `gorm:"check:ck_sessions_oidc_primary,(OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 108 AND ASCII(SUBSTRING(primary_method,2,1)) = 100 AND ASCII(SUBSTRING(primary_method,3,1)) = 97 AND ASCII(SUBSTRING(primary_method,4,1)) = 112 AND CHAR_LENGTH(ldap_binding_id) > 0 AND ldap_binding_created_at IS NOT NULL AND CHAR_LENGTH(ldap_config_revision) = 64 AND CHAR_LENGTH(ldap_policy_revision) = 64 AND ldap_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 115 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 109 AND ASCII(SUBSTRING(primary_method,4,1)) = 108 AND CHAR_LENGTH(saml_binding_id) > 0 AND saml_binding_created_at IS NOT NULL AND CHAR_LENGTH(saml_config_revision) = 64 AND CHAR_LENGTH(saml_policy_revision) = 64 AND saml_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL)"`
}

func (samlFixtureSessionProofV97) TableName() string { return "sessions" }

type samlFixtureMFAProofV97 struct {
	PrimaryMethod string `gorm:"check:ck_mfa_challenges_oidc_primary,(OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 108 AND ASCII(SUBSTRING(primary_method,2,1)) = 100 AND ASCII(SUBSTRING(primary_method,3,1)) = 97 AND ASCII(SUBSTRING(primary_method,4,1)) = 112 AND CHAR_LENGTH(ldap_binding_id) > 0 AND ldap_binding_created_at IS NOT NULL AND CHAR_LENGTH(ldap_config_revision) = 64 AND CHAR_LENGTH(ldap_policy_revision) = 64 AND ldap_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 115 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 109 AND ASCII(SUBSTRING(primary_method,4,1)) = 108 AND CHAR_LENGTH(saml_binding_id) > 0 AND saml_binding_created_at IS NOT NULL AND CHAR_LENGTH(saml_config_revision) = 64 AND CHAR_LENGTH(saml_policy_revision) = 64 AND saml_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL)"`
}

func (samlFixtureMFAProofV97) TableName() string { return "mfa_challenges" }

type samlWrongSubjectWidthV97 struct {
	Subject string `gorm:"size:255;not null"`
}

func (samlWrongSubjectWidthV97) TableName() string { return "saml_bindings" }

type samlWrongBirthPrecisionV97 struct {
	UserCreatedAt time.Time `gorm:"precision:3;not null"`
}

func (samlWrongBirthPrecisionV97) TableName() string { return "saml_bindings" }

type samlWrongOptionalNullV97 struct {
	VerifiedAt time.Time `gorm:"precision:6;not null"`
}

func (samlWrongOptionalNullV97) TableName() string { return "saml_ceremonies" }

type samlWrongOwnerIndexV97 struct {
	UserID string `gorm:"index:idx_saml_bindings_user_id"`
}

func (samlWrongOwnerIndexV97) TableName() string { return "saml_bindings" }

type samlWrongOwnerColumnV97 struct {
	SubjectDigest string `gorm:"uniqueIndex:idx_saml_bindings_user_id"`
}

func (samlWrongOwnerColumnV97) TableName() string { return "saml_bindings" }

// legacyMigrationBeforeV97 is exclusively a pristine integration-DB rewind.
// New tables/columns stay present as a replayable partial-DDL prefix; no SAML
// identity, assertion receipt or provenance may be discarded by older fixtures.
func legacyMigrationBeforeV97(t *testing.T, db *gorm.DB) {
	t.Helper()
	legacyMigrationBeforeV98(t, db)
	rows := personalKeyBehaviorLedger(t, db)
	if len(rows) != 97 {
		t.Fatal("historical fixture requires exact current V97 ledger")
	}
	for i, row := range rows {
		if row.Version != i+1 {
			t.Fatal("noncontiguous V97 ledger", i)
		}
	}
	for table, want := range map[string]int64{"saml_providers": 1, "saml_bindings": 0, "saml_ceremonies": 0, "saml_assertion_receipts": 0} {
		var count int64
		if e := db.Table(table).Count(&count).Error; e != nil || count != want {
			t.Fatal("historical fixture requires pristine SAML objects", table, count, e)
		}
	}
	var provider samlFixtureProviderV97
	if db.Session(&gorm.Session{QueryFields: true}).Take(&provider, "id = ?", "saml").Error != nil || provider.CreatedAt.IsZero() || provider.UpdatedAt.IsZero() {
		t.Fatal("read default SAML singleton")
	}
	provider.CreatedAt, provider.UpdatedAt = time.Time{}, time.Time{}
	if !reflect.DeepEqual(provider, samlFixtureProviderV97{ID: "saml", ReviewRevision: strings.Repeat("0", 64), ConfigRevision: strings.Repeat("0", 64), PolicyRevision: strings.Repeat("0", 64)}) {
		t.Fatal("historical fixture cannot discard configured SAML trust")
	}
	for _, table := range []string{"sessions", "mfa_challenges"} {
		var count int64
		if e := db.Table(table).Where("OCTET_LENGTH(saml_binding_id) <> 0 OR saml_binding_created_at IS NOT NULL OR OCTET_LENGTH(saml_config_revision) <> 0 OR OCTET_LENGTH(saml_policy_revision) <> 0 OR saml_user_created_at IS NOT NULL OR primary_method = ?", "saml").Count(&count).Error; e != nil || count != 0 {
			t.Fatal("historical fixture contains SAML proof", table, count, e)
		}
	}
	if database.MigrateThrough(db.Statement.Context, db, 96) == nil || !reflect.DeepEqual(rows, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("bounded96 must reject newer ledger without mutation")
	}
	for _, v := range []struct {
		model any
		name  string
	}{{&ldapFixtureSessionProofV96{}, "ck_sessions_oidc_primary"}, {&ldapFixtureMFAProofV96{}, "ck_mfa_challenges_oidc_primary"}} {
		if db.Migrator().DropConstraint(v.model, v.name) != nil || db.Migrator().CreateConstraint(v.model, v.name) != nil {
			t.Fatal("restore frozen V96 primary check", v.name)
		}
	}
	removed := db.Table("schema_migrations").Where("version = ?", 97).Delete(&struct{}{})
	if removed.Error != nil || removed.RowsAffected != 1 || !reflect.DeepEqual(rows[:96], personalKeyBehaviorLedger(t, db)) {
		t.Fatal("remove only owned V97 ledger for historical fixture")
	}
}

func testSAMLMigration(t *testing.T, db *gorm.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	db = db.WithContext(ctx)
	legacyMigrationBeforeV98(t, db)
	original := personalKeyBehaviorLedger(t, db)
	if len(original) != 97 {
		t.Fatal("exact V97 ledger required")
	}
	for i, row := range original {
		if row.Version != i+1 {
			t.Fatal("released prefix", i)
		}
	}
	ledger := func(present bool) {
		t.Helper()
		rows := personalKeyBehaviorLedger(t, db)
		n := 96
		if present {
			n = 97
		}
		if len(rows) != n || !reflect.DeepEqual(rows[:96], original[:96]) {
			t.Fatal("released V1-V96 ledger changed")
		}
		if present && rows[96].Version != 97 {
			t.Fatal("missing V97 suffix")
		}
	}
	remove := func() {
		t.Helper()
		r := db.Table("schema_migrations").Where("version = ?", 97).Delete(&struct{}{})
		if r.Error != nil || r.RowsAffected != 1 {
			t.Fatal("remove only V97", r.Error, r.RowsAffected)
		}
		ledger(false)
	}
	migrate := func() {
		t.Helper()
		if e := database.MigrateThrough(ctx, db, 97); e != nil {
			t.Fatal("restore V97", e)
		}
		ledger(true)
	}
	readProvider := func() samlFixtureProviderV97 {
		t.Helper()
		var p samlFixtureProviderV97
		if db.Session(&gorm.Session{QueryFields: true}).Take(&p, "id = ?", "saml").Error != nil {
			t.Fatal("SAML singleton")
		}
		return p
	}
	assertDefault := func() {
		t.Helper()
		for table, want := range map[string]int64{"saml_providers": 1, "saml_bindings": 0, "saml_ceremonies": 0, "saml_assertion_receipts": 0} {
			var n int64
			if db.Table(table).Count(&n).Error != nil || n != want {
				t.Fatal("empty SAML state", table, n)
			}
		}
		p := readProvider()
		if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
			t.Fatal("missing singleton births")
		}
		p.CreatedAt, p.UpdatedAt = time.Time{}, time.Time{}
		if !reflect.DeepEqual(p, samlFixtureProviderV97{ID: "saml", ReviewRevision: strings.Repeat("0", 64), ConfigRevision: strings.Repeat("0", 64), PolicyRevision: strings.Repeat("0", 64)}) {
			t.Fatal("default invented identity/trust")
		}
	}
	assertDefault()
	for _, bound := range []int{0, -1, 96, 101} {
		before := personalKeyBehaviorLedger(t, db)
		if database.MigrateThrough(ctx, db, bound) == nil || !reflect.DeepEqual(before, personalKeyBehaviorLedger(t, db)) {
			t.Fatal("invalid/newer bound admitted", bound)
		}
	}
	birth := time.Now().UTC().Truncate(time.Microsecond)
	var sessions []entity.Session
	var challenges []entity.MFAChallenge
	for i, method := range []string{"", "oidc", "oauth", "ldap"} {
		user := entity.User{ID: []string{"usr_saml_local", "usr_saml_oidc", "usr_saml_oauth", "usr_saml_ldap"}[i], Email: []string{"saml-local@example.invalid", "saml-oidc@example.invalid", "saml-oauth@example.invalid", "saml-ldap@example.invalid"}[i], Name: "Retained member", Role: entity.RoleMember, PasswordHash: "retained-not-proof"}
		if db.Create(&user).Error != nil {
			t.Fatal("retained member")
		}
		row := entity.Session{ID: []string{"ses_saml_local", "ses_saml_oidc", "ses_saml_oauth", "ses_saml_ldap"}[i], UserID: user.ID, TokenHash: strings.Repeat(string(rune('a'+i)), 64), CreatedAt: birth, ExpiresAt: birth.Add(time.Hour), PrimaryMethod: method}
		challenge := entity.MFAChallenge{UserID: user.ID, Purpose: "login", TokenHash: strings.Repeat(string(rune('e'+i)), 64), PasswordDigest: strings.Repeat("a", 64), Generation: strings.Repeat("b", 64), ExpiresAt: birth.Add(time.Minute), PrimaryMethod: method}
		switch method {
		case "oidc":
			row.OIDCBindingID = "oib_retained"
			row.OIDCBindingCreatedAt = &birth
			row.OIDCConfigRevision = strings.Repeat("a", 64)
			row.OIDCPolicyRevision = strings.Repeat("b", 64)
			row.OIDCUserCreatedAt = &user.CreatedAt
			challenge.OIDCBindingID = row.OIDCBindingID
			challenge.OIDCBindingCreatedAt = &birth
			challenge.OIDCConfigRevision = row.OIDCConfigRevision
			challenge.OIDCPolicyRevision = row.OIDCPolicyRevision
			challenge.OIDCUserCreatedAt = &user.CreatedAt
		case "oauth":
			row.OAuthBindingID = "oab_retained"
			row.OAuthBindingCreatedAt = &birth
			row.OAuthConfigRevision = strings.Repeat("c", 64)
			row.OAuthPolicyRevision = strings.Repeat("d", 64)
			row.OAuthUserCreatedAt = &user.CreatedAt
			challenge.OAuthBindingID = row.OAuthBindingID
			challenge.OAuthBindingCreatedAt = &birth
			challenge.OAuthConfigRevision = row.OAuthConfigRevision
			challenge.OAuthPolicyRevision = row.OAuthPolicyRevision
			challenge.OAuthUserCreatedAt = &user.CreatedAt
		case "ldap":
			row.LDAPBindingID = "ldb_retained"
			row.LDAPBindingCreatedAt = &birth
			row.LDAPConfigRevision = strings.Repeat("e", 64)
			row.LDAPPolicyRevision = strings.Repeat("f", 64)
			row.LDAPUserCreatedAt = &user.CreatedAt
			challenge.LDAPBindingID = row.LDAPBindingID
			challenge.LDAPBindingCreatedAt = &birth
			challenge.LDAPConfigRevision = row.LDAPConfigRevision
			challenge.LDAPPolicyRevision = row.LDAPPolicyRevision
			challenge.LDAPUserCreatedAt = &user.CreatedAt
		}
		if db.Create(&row).Error != nil || db.Create(&challenge).Error != nil {
			t.Fatal("retained primary positive", method)
		}
		if db.Session(&gorm.Session{QueryFields: true}).Take(&row, "id = ?", row.ID).Error != nil || db.Session(&gorm.Session{QueryFields: true}).Take(&challenge, "user_id = ? AND purpose = ?", user.ID, "login").Error != nil {
			t.Fatal("capture retained baseline")
		}
		sessions = append(sessions, row)
		challenges = append(challenges, challenge)
	}
	retained := func(stage string) {
		t.Helper()
		for i, want := range sessions {
			var got entity.Session
			var c entity.MFAChallenge
			if db.Session(&gorm.Session{QueryFields: true}).Take(&got, "id = ?", want.ID).Error != nil || db.Session(&gorm.Session{QueryFields: true}).Take(&c, "user_id = ? AND purpose = ?", challenges[i].UserID, "login").Error != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(c, challenges[i]) {
				t.Fatal("retained primary changed", stage, i)
			}
		}
	}
	checks := []struct {
		model, proof, old any
		name              string
	}{{&samlFixtureSessionV97{}, &samlFixtureSessionProofV97{}, &ldapFixtureSessionProofV96{}, "ck_sessions_oidc_primary"}, {&samlFixtureMFAV97{}, &samlFixtureMFAProofV97{}, &ldapFixtureMFAProofV96{}, "ck_mfa_challenges_oidc_primary"}}
	fields := []string{"SAMLBindingID", "SAMLBindingCreatedAt", "SAMLConfigRevision", "SAMLPolicyRevision", "SAMLUserCreatedAt"}
	for _, v := range checks {
		if db.Migrator().DropConstraint(v.proof, v.name) != nil {
			t.Fatal("drop current check")
		}
		for _, field := range fields {
			if db.Migrator().DropColumn(v.model, field) != nil {
				t.Fatal("drop only additive SAML column", field)
			}
		}
		if db.Migrator().CreateConstraint(v.old, v.name) != nil {
			t.Fatal("restore exact V96 check")
		}
	}
	if db.Migrator().DropTable(&samlFixtureReceiptV97{}, &samlFixtureCeremonyV97{}, &samlFixtureBindingV97{}, &samlFixtureProviderV97{}) != nil {
		t.Fatal("drop only SAML tables")
	}
	remove()
	if database.MigrateThrough(ctx, db, 96) != nil {
		t.Fatal("faithful bounded V96")
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- database.MigrateThrough(ctx, db, 97) })
	}
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal("concurrent V96 upgrade", e)
		}
	}
	migrate()
	assertDefault()
	retained("concurrent_upgrade")
	singleton := readProvider()
	current := func(stage string) {
		t.Helper()
		ledger(true)
		retained(stage)
		if !reflect.DeepEqual(singleton, readProvider()) {
			t.Fatal("singleton changed", stage)
		}
	}
	if db.Migrator().DropConstraint(checks[0].proof, checks[0].name) != nil || db.Migrator().DropColumn(checks[0].model, "SAMLPolicyRevision") != nil {
		t.Fatal("partial additive DDL")
	}
	remove()
	migrate()
	current("partial_column_replay")
	remove()
	migrate()
	migrate()
	current("repeat")
	for _, bad := range []struct {
		model, good any
		field       string
	}{{&samlWrongSubjectWidthV97{}, &samlFixtureBindingV97{}, "Subject"}, {&samlWrongBirthPrecisionV97{}, &samlFixtureBindingV97{}, "UserCreatedAt"}, {&samlWrongOptionalNullV97{}, &samlFixtureCeremonyV97{}, "VerifiedAt"}} {
		current("before_invalid_shape")
		if db.Migrator().AlterColumn(bad.model, bad.field) != nil {
			t.Fatal("install incompatible shape", bad.field)
		}
		remove()
		if database.MigrateThrough(ctx, db, 97) == nil {
			t.Fatal("incompatible shape accepted", bad.field)
		}
		ledger(false)
		if db.Migrator().AlterColumn(bad.good, bad.field) != nil {
			t.Fatal("restore frozen shape", bad.field)
		}
		migrate()
		current("restored_shape")
	}
	for _, bad := range []any{&samlWrongOwnerIndexV97{}, &samlWrongOwnerColumnV97{}} {
		current("before_invalid_index")
		if database.DropIndex(db, &samlFixtureBindingV97{}, "idx_saml_bindings_user_id") != nil || db.Migrator().CreateIndex(bad, "idx_saml_bindings_user_id") != nil {
			t.Fatal("install incompatible index")
		}
		remove()
		if database.MigrateThrough(ctx, db, 97) == nil {
			t.Fatal("incompatible index accepted")
		}
		ledger(false)
		if database.DropIndex(db, &samlFixtureBindingV97{}, "idx_saml_bindings_user_id") != nil || db.Migrator().CreateIndex(&samlFixtureBindingV97{}, "idx_saml_bindings_user_id") != nil {
			t.Fatal("restore frozen index")
		}
		migrate()
		current("restored_index")
	}
	binding := samlFixtureBindingV97{ID: "smb_constraint", ProviderID: "saml", UserID: sessions[0].UserID, UserCreatedAt: birth, ConfigRevision: strings.Repeat("a", 64), Issuer: "https://idp.example.invalid", Subject: "opaque-retained-subject", SubjectDigest: strings.Repeat("b", 64), CreatedAt: birth}
	if db.Create(&binding).Error != nil {
		t.Fatal("positive exact binding")
	}
	for _, provider := range []string{"SAML", "saml ", "", " saml"} {
		e := db.Transaction(func(tx *gorm.DB) error {
			x := binding
			x.ID = "smb_bad"
			x.UserID = "usr_negative"
			x.SubjectDigest = strings.Repeat("c", 64)
			x.ProviderID = provider
			return tx.Create(&x).Error
		})
		if e == nil {
			t.Fatal("provider alias accepted")
		}
	}
	for _, field := range []string{"user", "digest"} {
		e := db.Transaction(func(tx *gorm.DB) error {
			x := binding
			x.ID = "smb_duplicate"
			if field == "user" {
				x.SubjectDigest = strings.Repeat("c", 64)
			} else {
				x.UserID = "usr_other"
			}
			return tx.Create(&x).Error
		})
		if !errors.Is(e, gorm.ErrDuplicatedKey) {
			t.Fatal("unique owner/subject digest", field, e)
		}
	}
	for _, bad := range []string{"SAML", "saml ", " saml", ""} {
		if db.Transaction(func(tx *gorm.DB) error {
			return tx.Model(&samlFixtureProviderV97{}).Where("id = ?", "saml").Update("id", bad).Error
		}) == nil {
			t.Fatal("singleton alias accepted")
		}
	}

	ceremony := samlFixtureCeremonyV97{ID: "smc_constraint", RequestID: "_constraint_request", RelayHash: strings.Repeat("a", 64), CookieHash: strings.Repeat("b", 64), Purpose: "login", Status: "pending", ProviderCreatedAt: singleton.CreatedAt, ConfigRevision: singleton.ConfigRevision, PolicyRevision: singleton.PolicyRevision, ExpiresAt: birth.Add(time.Minute), CreatedAt: birth}
	if db.Create(&ceremony).Error != nil {
		t.Fatal("positive ceremony shape")
	}
	for _, bad := range []map[string]any{{"purpose": "LOGIN"}, {"purpose": "login "}, {"purpose": "unknown"}, {"status": "PENDING"}, {"status": "pending "}, {"status": "unknown"}} {
		var positive samlFixtureCeremonyV97
		if db.Session(&gorm.Session{QueryFields: true}).Take(&positive, "id = ?", ceremony.ID).Error != nil {
			t.Fatal("positive before ceremony denial read failed")
		}
		if differingFields := samlMigrationDifferentFields(positive, ceremony); len(differingFields) != 0 {
			t.Fatal("positive before ceremony denial differs in fields", strings.Join(differingFields, ","))
		}
		if db.Transaction(func(tx *gorm.DB) error {
			return tx.Model(&samlFixtureCeremonyV97{}).Where("id = ?", ceremony.ID).Updates(bad).Error
		}) == nil {
			t.Fatal("purpose/status alias admitted")
		}
	}
	for _, field := range []string{"request", "relay", "cookie"} {
		e := db.Transaction(func(tx *gorm.DB) error {
			x := ceremony
			x.ID = "smc_duplicate"
			x.RequestID = "_another_request"
			x.RelayHash = strings.Repeat("c", 64)
			x.CookieHash = strings.Repeat("d", 64)
			switch field {
			case "request":
				x.RequestID = ceremony.RequestID
			case "relay":
				x.RelayHash = ceremony.RelayHash
			case "cookie":
				x.CookieHash = ceremony.CookieHash
			}
			return tx.Create(&x).Error
		})
		if !errors.Is(e, gorm.ErrDuplicatedKey) {
			t.Fatal("ceremony correlation uniqueness", field, e)
		}
	}
	for _, purpose := range []string{"login", "bind", "verify"} {
		if db.Model(&samlFixtureCeremonyV97{}).Where("id = ?", ceremony.ID).Update("purpose", purpose).Error != nil {
			t.Fatal("supported purpose rejected")
		}
	}
	for _, status := range []string{"pending", "validating", "verified", "consumed", "failed"} {
		if db.Model(&samlFixtureCeremonyV97{}).Where("id = ?", ceremony.ID).Update("status", status).Error != nil {
			t.Fatal("supported status rejected")
		}
	}
	assertionReceipt := samlFixtureReceiptV97{Digest: strings.Repeat("a", 64), ExpiresAt: birth.Add(time.Minute), CreatedAt: birth}
	if db.Create(&assertionReceipt).Error != nil {
		t.Fatal("positive replay receipt")
	}
	if e := db.Transaction(func(tx *gorm.DB) error { x := assertionReceipt; return tx.Create(&x).Error }); !errors.Is(e, gorm.ErrDuplicatedKey) {
		t.Fatal("assertion replay receipt uniqueness", e)
	}
	if db.Delete(&assertionReceipt).Error != nil || db.Delete(&ceremony).Error != nil {
		t.Fatal("remove owned ceremony/receipt controls")
	}
	// Both primary tables require exact supported discriminants, complete matching
	// proof, and byte-empty/nonmatching nullable tuples. Every negative starts from
	// a successful complete SAML update and checks the unchanged persisted proof.
	for _, target := range []struct {
		table, where string
		args         []any
	}{{"sessions", "id = ?", []any{sessions[0].ID}}, {"mfa_challenges", "user_id = ? AND purpose = ?", []any{challenges[0].UserID, "login"}}} {
		positive := map[string]any{"primary_method": "saml", "saml_binding_id": binding.ID, "saml_binding_created_at": birth, "saml_config_revision": strings.Repeat("a", 64), "saml_policy_revision": strings.Repeat("b", 64), "saml_user_created_at": birth}
		for _, bad := range []map[string]any{{"primary_method": "SAML"}, {"primary_method": "saml "}, {"saml_binding_id": ""}, {"saml_binding_created_at": nil}, {"saml_config_revision": "short"}, {"saml_policy_revision": "short"}, {"saml_user_created_at": nil}, {"oidc_binding_id": "oib_mixed"}, {"oauth_binding_id": " "}, {"ldap_user_created_at": birth}} {
			if db.Table(target.table).Where(target.where, target.args...).Updates(positive).Error != nil {
				t.Fatal("complete SAML primary positive", target.table)
			}
			var before samlFixtureSessionV97
			if db.Table(target.table).Select("saml_binding_id,saml_binding_created_at,saml_config_revision,saml_policy_revision,saml_user_created_at").Where(target.where, target.args...).Take(&before).Error != nil {
				t.Fatal("capture complete positive primary proof read failed")
			}
			expected := samlFixtureSessionV97{SAMLBindingID: binding.ID, SAMLBindingCreatedAt: &birth, SAMLConfigRevision: strings.Repeat("a", 64), SAMLPolicyRevision: strings.Repeat("b", 64), SAMLUserCreatedAt: &birth}
			if differingFields := samlMigrationDifferentFields(before, expected); len(differingFields) != 0 {
				t.Fatal("capture complete positive primary proof differs in fields", strings.Join(differingFields, ","))
			}
			if db.Transaction(func(tx *gorm.DB) error {
				return tx.Table(target.table).Where(target.where, target.args...).Updates(bad).Error
			}) == nil {
				t.Fatal("mixed/partial/alias SAML primary accepted", target.table)
			}
			var after samlFixtureSessionV97
			if db.Table(target.table).Select("saml_binding_id,saml_binding_created_at,saml_config_revision,saml_policy_revision,saml_user_created_at").Where(target.where, target.args...).Take(&after).Error != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("rejected primary changed proof")
			}
		}
		local := map[string]any{"primary_method": "", "saml_binding_id": "", "saml_binding_created_at": nil, "saml_config_revision": "", "saml_policy_revision": "", "saml_user_created_at": nil}
		if db.Table(target.table).Where(target.where, target.args...).Updates(local).Error != nil {
			t.Fatal("restore local primary")
		}
		for _, field := range []string{"saml_binding_id", "saml_config_revision", "saml_policy_revision"} {
			if db.Transaction(func(tx *gorm.DB) error {
				return tx.Table(target.table).Where(target.where, target.args...).Update(field, " ").Error
			}) == nil {
				t.Fatal("PAD SPACE blank accepted", target.table, field)
			}
		}
	}
	if db.Delete(&binding).Error != nil {
		t.Fatal("remove constraint binding")
	}
	current("after_constraints")
	// A retained invalid additive tuple under the genuine old CHECK must reject
	// V97 before ledger publication, preserving the original malformed bytes.
	if db.Migrator().DropConstraint(checks[0].proof, checks[0].name) != nil || db.Migrator().CreateConstraint(checks[0].old, checks[0].name) != nil {
		t.Fatal("install historical V96 primary check")
	}
	if db.Table("sessions").Where("id = ?", sessions[0].ID).UpdateColumn("saml_binding_id", " ").Error != nil {
		t.Fatal("install retained unproven tuple")
	}
	remove()
	if database.MigrateThrough(ctx, db, 97) == nil {
		t.Fatal("V97 accepted retained unproven tuple")
	}
	ledger(false)
	var invalid struct {
		SAMLBindingID string `gorm:"column:saml_binding_id"`
	}
	if db.Table("sessions").Select("saml_binding_id").Take(&invalid, "id = ?", sessions[0].ID).Error != nil || invalid.SAMLBindingID != " " {
		t.Fatal("failed upgrade rewrote malformed tuple")
	}
	if db.Table("sessions").Where("id = ?", sessions[0].ID).UpdateColumn("saml_binding_id", "").Error != nil {
		t.Fatal("restore exact retained blank")
	}
	migrate()
	current("retained_invalid_repaired")
	// V97 was deliberately replayed; preserve its current durable receipt and
	// original V1-V96 prefix while appending only the current V98 successor.
	beforeSuccessor := personalKeyBehaviorLedger(t, db)
	if len(beforeSuccessor) != 97 || beforeSuccessor[96].Version != 97 || !reflect.DeepEqual(original[:96], beforeSuccessor[:96]) {
		t.Fatal("historical V97 closure lost retained prefix or current V97 receipt")
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("restore full V98 after historical V97", err)
	}
	final := personalKeyBehaviorLedger(t, db)
	if len(final) != 100 || final[99].Version != 100 || final[98].Version != 99 || final[97].Version != 98 || !reflect.DeepEqual(beforeSuccessor, final[:97]) {
		t.Fatal("historical V97 closure lost original rows or V98 suffix")
	}
}

// samlMigrationDifferentFields preserves every field. Only timestamps use exact
// instant/nil semantics instead of comparing driver-specific Location objects.
func samlMigrationDifferentFields(actual, expected any) []string {
	got, want := reflect.ValueOf(actual), reflect.ValueOf(expected)
	if !got.IsValid() || !want.IsValid() || got.Kind() != reflect.Struct || got.Type() != want.Type() {
		return []string{"type"}
	}
	var differingFields []string
	for i := 0; i < got.NumField(); i++ {
		actualField, expectedField := got.Field(i).Interface(), want.Field(i).Interface()
		equal := false
		switch value := actualField.(type) {
		case time.Time:
			equal = value.Equal(expectedField.(time.Time))
		case *time.Time:
			other := expectedField.(*time.Time)
			equal = value == nil && other == nil || value != nil && other != nil && value.Equal(*other)
		default:
			equal = reflect.DeepEqual(actualField, expectedField)
		}
		if !equal {
			differingFields = append(differingFields, got.Type().Field(i).Name)
		}
	}
	return differingFields
}

func TestSAMLMigrationExactFieldComparison(t *testing.T) {
	type facts struct {
		Instant  time.Time
		Optional *time.Time
		Scalar   string
	}
	instant := time.Date(2026, 10, 10, 1, 2, 3, 123456000, time.UTC)
	offset := instant.In(time.FixedZone("fixture-offset", 8*60*60))
	changed := instant.Add(time.Microsecond)
	want := facts{Instant: instant, Optional: &instant, Scalar: "exact"}
	for _, probe := range []struct {
		name   string
		actual facts
		fields []string
	}{
		{"equal_offset", facts{Instant: offset, Optional: &offset, Scalar: "exact"}, nil},
		{"different_instant", facts{Instant: changed, Optional: &instant, Scalar: "exact"}, []string{"Instant"}},
		{"different_optional_instant", facts{Instant: instant, Optional: &changed, Scalar: "exact"}, []string{"Optional"}},
		{"missing_optional", facts{Instant: instant, Scalar: "exact"}, []string{"Optional"}},
		{"different_scalar", facts{Instant: instant, Optional: &instant, Scalar: "changed"}, []string{"Scalar"}},
	} {
		t.Run(probe.name, func(t *testing.T) {
			if got := samlMigrationDifferentFields(probe.actual, want); !reflect.DeepEqual(got, probe.fields) {
				t.Fatal("exact field comparison lost a boundary", probe.name)
			}
		})
	}
	if got := samlMigrationDifferentFields(facts{}, facts{}); len(got) != 0 {
		t.Fatal("nil optional timestamps differ")
	}
	if got := samlMigrationDifferentFields(want, facts{Instant: instant, Scalar: "exact"}); !reflect.DeepEqual(got, []string{"Optional"}) {
		t.Fatal("unexpected optional timestamp accepted")
	}
}
