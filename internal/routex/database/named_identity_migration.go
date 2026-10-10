package database

import (
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reflect"
	"strings"
	"time"
)

// Private V98 models are frozen. Released V1–97 definitions stay unchanged.
// namedIdentityProviderV98 is the fixed named-identity configuration; only GitHub is currently admitted. Secrets and
// protocol proofs are never exposed through the configuration response.
type namedIdentityProviderV98 struct {
	ProfileID                string     `gorm:"size:64;not null;check:ck_namedidentityprovider_profile,OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49" json:"-"`
	IdentityIssuer           string     `gorm:"size:2048;not null;check:ck_namedidentityprovider_issuer,OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109" json:"-"`
	ID                       string     `gorm:"primaryKey;size:30;check:ck_named_identity_singleton,OCTET_LENGTH(id) = 6 AND ASCII(SUBSTRING(id,1,1)) = 103 AND ASCII(SUBSTRING(id,2,1)) = 105 AND ASCII(SUBSTRING(id,3,1)) = 116 AND ASCII(SUBSTRING(id,4,1)) = 104 AND ASCII(SUBSTRING(id,5,1)) = 117 AND ASCII(SUBSTRING(id,6,1)) = 98"`
	ReviewRevision           string     `gorm:"size:64;not null"`
	ConfigRevision           string     `gorm:"size:64;not null"`
	PolicyRevision           string     `gorm:"size:64;not null"`
	Name                     string     `gorm:"size:100;not null"`
	ClientID                 string     `gorm:"size:256;not null"`
	CallbackURL              string     `gorm:"size:2048;not null"`
	SecretGeneration         string     `gorm:"size:64;not null"`
	AuthCiphertext           string     `gorm:"type:text;not null" json:"-"`
	Enabled                  bool       `gorm:"not null"`
	VerifiedConfigRevision   string     `gorm:"size:64;not null"`
	VerifiedBy               string     `gorm:"size:30;not null"`
	VerifiedUserCreatedAt    *time.Time `gorm:"precision:6"`
	VerifiedBindingID        string     `gorm:"size:30;not null"`
	VerifiedBindingCreatedAt *time.Time `gorm:"precision:6"`
	CreatedAt                time.Time  `gorm:"precision:6;not null"`
	UpdatedAt                time.Time  `gorm:"precision:6;not null"`
}

func (namedIdentityProviderV98) TableName() string { return "named_identity_providers" }

// namedIdentityBindingV98 preserves the provider namespace and exact subject kind.
type namedIdentityBindingV98 struct {
	ProfileID      string    `gorm:"size:64;not null;check:ck_namedidentitybinding_profile,OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49" json:"-"`
	IdentityIssuer string    `gorm:"size:2048;not null;check:ck_namedidentitybinding_issuer,OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109" json:"-"`
	ProviderID     string    `gorm:"size:30;not null;uniqueIndex:idx_named_identity_member,priority:1;check:ck_named_identity_binding_provider,OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98"`
	SubjectKind    string    `gorm:"size:20;not null;check:ck_named_identity_binding_kind,OCTET_LENGTH(subject_kind) = 7 AND ASCII(SUBSTRING(subject_kind,1,1)) = 105 AND ASCII(SUBSTRING(subject_kind,2,1)) = 110 AND ASCII(SUBSTRING(subject_kind,3,1)) = 116 AND ASCII(SUBSTRING(subject_kind,4,1)) = 101 AND ASCII(SUBSTRING(subject_kind,5,1)) = 103 AND ASCII(SUBSTRING(subject_kind,6,1)) = 101 AND ASCII(SUBSTRING(subject_kind,7,1)) = 114" json:"-"`
	ID             string    `gorm:"primaryKey;size:30"`
	UserID         string    `gorm:"size:30;not null;uniqueIndex:idx_named_identity_member,priority:2"`
	UserCreatedAt  time.Time `gorm:"precision:6;not null"`
	ConfigRevision string    `gorm:"size:64;not null"`
	Subject        string    `gorm:"size:256;not null" json:"-"`
	SubjectDigest  string    `gorm:"size:64;not null;uniqueIndex" json:"-"`
	CreatedAt      time.Time `gorm:"precision:6;not null"`
}

func (namedIdentityBindingV98) TableName() string { return "named_identity_bindings" }

// namedIdentityCeremonyV98 stores only bounded hashes and typed provenance, never remote tokens.
type namedIdentityCeremonyV98 struct {
	ProviderCreatedAt time.Time  `gorm:"precision:6;not null"`
	ConsumedAt        *time.Time `gorm:"precision:6" json:"-"`
	ProfileID         string     `gorm:"size:64;not null;check:ck_namedidentityceremony_profile,OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49" json:"-"`
	IdentityIssuer    string     `gorm:"size:2048;not null;check:ck_namedidentityceremony_issuer,OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109" json:"-"`
	ProviderID        string     `gorm:"size:30;not null;check:ck_named_identity_ceremony_provider,OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98"`
	SubjectKind       string     `gorm:"size:20;not null;check:ck_named_identity_ceremony_kind,OCTET_LENGTH(subject_kind) = 0 OR (OCTET_LENGTH(subject_kind) = 7 AND ASCII(SUBSTRING(subject_kind,1,1)) = 105 AND ASCII(SUBSTRING(subject_kind,2,1)) = 110 AND ASCII(SUBSTRING(subject_kind,3,1)) = 116 AND ASCII(SUBSTRING(subject_kind,4,1)) = 101 AND ASCII(SUBSTRING(subject_kind,5,1)) = 103 AND ASCII(SUBSTRING(subject_kind,6,1)) = 101 AND ASCII(SUBSTRING(subject_kind,7,1)) = 114)" json:"-"`
	ID                string     `gorm:"primaryKey;size:30"`
	StateHash         string     `gorm:"size:64;not null;uniqueIndex" json:"-"`
	CookieHash        string     `gorm:"size:64;not null;uniqueIndex" json:"-"`
	Purpose           string     `gorm:"size:20;not null;check:ck_named_identity_ceremonies_purpose,(OCTET_LENGTH(purpose) = 5 AND ASCII(SUBSTRING(purpose,1,1)) = 108 AND ASCII(SUBSTRING(purpose,2,1)) = 111 AND ASCII(SUBSTRING(purpose,3,1)) = 103 AND ASCII(SUBSTRING(purpose,4,1)) = 105 AND ASCII(SUBSTRING(purpose,5,1)) = 110) OR (OCTET_LENGTH(purpose) = 4 AND ASCII(SUBSTRING(purpose,1,1)) = 98 AND ASCII(SUBSTRING(purpose,2,1)) = 105 AND ASCII(SUBSTRING(purpose,3,1)) = 110 AND ASCII(SUBSTRING(purpose,4,1)) = 100) OR (OCTET_LENGTH(purpose) = 6 AND ASCII(SUBSTRING(purpose,1,1)) = 118 AND ASCII(SUBSTRING(purpose,2,1)) = 101 AND ASCII(SUBSTRING(purpose,3,1)) = 114 AND ASCII(SUBSTRING(purpose,4,1)) = 105 AND ASCII(SUBSTRING(purpose,5,1)) = 102 AND ASCII(SUBSTRING(purpose,6,1)) = 121)"`
	Reason            string     `gorm:"size:1024;not null"`
	Status            string     `gorm:"size:20;not null;check:ck_named_identity_ceremonies_status,(OCTET_LENGTH(status) = 7 AND ASCII(SUBSTRING(status,1,1)) = 112 AND ASCII(SUBSTRING(status,2,1)) = 101 AND ASCII(SUBSTRING(status,3,1)) = 110 AND ASCII(SUBSTRING(status,4,1)) = 100 AND ASCII(SUBSTRING(status,5,1)) = 105 AND ASCII(SUBSTRING(status,6,1)) = 110 AND ASCII(SUBSTRING(status,7,1)) = 103) OR (OCTET_LENGTH(status) = 10 AND ASCII(SUBSTRING(status,1,1)) = 101 AND ASCII(SUBSTRING(status,2,1)) = 120 AND ASCII(SUBSTRING(status,3,1)) = 99 AND ASCII(SUBSTRING(status,4,1)) = 104 AND ASCII(SUBSTRING(status,5,1)) = 97 AND ASCII(SUBSTRING(status,6,1)) = 110 AND ASCII(SUBSTRING(status,7,1)) = 103 AND ASCII(SUBSTRING(status,8,1)) = 105 AND ASCII(SUBSTRING(status,9,1)) = 110 AND ASCII(SUBSTRING(status,10,1)) = 103) OR (OCTET_LENGTH(status) = 8 AND ASCII(SUBSTRING(status,1,1)) = 118 AND ASCII(SUBSTRING(status,2,1)) = 101 AND ASCII(SUBSTRING(status,3,1)) = 114 AND ASCII(SUBSTRING(status,4,1)) = 105 AND ASCII(SUBSTRING(status,5,1)) = 102 AND ASCII(SUBSTRING(status,6,1)) = 105 AND ASCII(SUBSTRING(status,7,1)) = 101 AND ASCII(SUBSTRING(status,8,1)) = 100) OR (OCTET_LENGTH(status) = 8 AND ASCII(SUBSTRING(status,1,1)) = 99 AND ASCII(SUBSTRING(status,2,1)) = 111 AND ASCII(SUBSTRING(status,3,1)) = 110 AND ASCII(SUBSTRING(status,4,1)) = 115 AND ASCII(SUBSTRING(status,5,1)) = 117 AND ASCII(SUBSTRING(status,6,1)) = 109 AND ASCII(SUBSTRING(status,7,1)) = 101 AND ASCII(SUBSTRING(status,8,1)) = 100) OR (OCTET_LENGTH(status) = 6 AND ASCII(SUBSTRING(status,1,1)) = 102 AND ASCII(SUBSTRING(status,2,1)) = 97 AND ASCII(SUBSTRING(status,3,1)) = 105 AND ASCII(SUBSTRING(status,4,1)) = 108 AND ASCII(SUBSTRING(status,5,1)) = 101 AND ASCII(SUBSTRING(status,6,1)) = 100)"`
	ConfigRevision    string     `gorm:"size:64;not null"`
	PolicyRevision    string     `gorm:"size:64;not null"`
	UserID            string     `gorm:"size:30;not null"`
	UserCreatedAt     *time.Time `gorm:"precision:6"`
	PasswordDigest    string     `gorm:"size:64;not null" json:"-"`
	MFAGeneration     string     `gorm:"size:64;not null" json:"-"`
	SessionID         string     `gorm:"size:30;not null"`
	SessionCreatedAt  *time.Time `gorm:"precision:6"`
	Subject           string     `gorm:"size:256;not null" json:"-"`
	BindingID         string     `gorm:"size:30;not null"`
	BindingCreatedAt  *time.Time `gorm:"precision:6"`
	ExpiresAt         time.Time  `gorm:"precision:6;not null;index"`
	CreatedAt         time.Time  `gorm:"precision:6;not null"`
	VerifiedAt        *time.Time `gorm:"precision:6"`
}

func (namedIdentityCeremonyV98) TableName() string { return "named_identity_ceremonies" }

type namedIdentitySessionV98 struct {
	NamedIdentityProviderID       string     `gorm:"column:named_identity_provider_id;size:30;not null;default:''" json:"-"`
	NamedIdentityProfileID        string     `gorm:"column:named_identity_profile_id;size:64;not null;default:''" json:"-"`
	NamedIdentityBindingID        string     `gorm:"column:named_identity_binding_id;size:30;not null;default:''" json:"-"`
	NamedIdentityBindingCreatedAt *time.Time `gorm:"column:named_identity_binding_created_at;precision:6" json:"-"`
	NamedIdentityConfigRevision   string     `gorm:"column:named_identity_config_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityPolicyRevision   string     `gorm:"column:named_identity_policy_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityUserCreatedAt    *time.Time `gorm:"column:named_identity_user_created_at;precision:6" json:"-"`
}

func (namedIdentitySessionV98) TableName() string { return "sessions" }

type namedIdentitySessionProofV98 struct {
	PrimaryMethod string `gorm:"size:20;not null;default:'';check:ck_sessions_oidc_primary,(((OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 108 AND ASCII(SUBSTRING(primary_method,2,1)) = 100 AND ASCII(SUBSTRING(primary_method,3,1)) = 97 AND ASCII(SUBSTRING(primary_method,4,1)) = 112 AND CHAR_LENGTH(ldap_binding_id) > 0 AND ldap_binding_created_at IS NOT NULL AND CHAR_LENGTH(ldap_config_revision) = 64 AND CHAR_LENGTH(ldap_policy_revision) = 64 AND ldap_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 115 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 109 AND ASCII(SUBSTRING(primary_method,4,1)) = 108 AND CHAR_LENGTH(saml_binding_id) > 0 AND saml_binding_created_at IS NOT NULL AND CHAR_LENGTH(saml_config_revision) = 64 AND CHAR_LENGTH(saml_policy_revision) = 64 AND saml_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL)) AND OCTET_LENGTH(named_identity_provider_id) = 0 AND OCTET_LENGTH(named_identity_profile_id) = 0 AND OCTET_LENGTH(named_identity_binding_id) = 0 AND named_identity_binding_created_at IS NULL AND OCTET_LENGTH(named_identity_config_revision) = 0 AND OCTET_LENGTH(named_identity_policy_revision) = 0 AND named_identity_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 6 AND ASCII(SUBSTRING(primary_method,1,1)) = 103 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 116 AND ASCII(SUBSTRING(primary_method,4,1)) = 104 AND ASCII(SUBSTRING(primary_method,5,1)) = 117 AND ASCII(SUBSTRING(primary_method,6,1)) = 98 AND OCTET_LENGTH(named_identity_provider_id) = 6 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 98 AND OCTET_LENGTH(named_identity_profile_id) = 23 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 98 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 109 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,15,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,16,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,17,1)) = 45 AND ASCII(SUBSTRING(named_identity_profile_id,18,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,19,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,20,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,21,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,22,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,23,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)" json:"-"`
}

func (namedIdentitySessionProofV98) TableName() string { return "sessions" }

type namedIdentityMFAChallengeV98 struct {
	NamedIdentityProviderID       string     `gorm:"column:named_identity_provider_id;size:30;not null;default:''" json:"-"`
	NamedIdentityProfileID        string     `gorm:"column:named_identity_profile_id;size:64;not null;default:''" json:"-"`
	NamedIdentityBindingID        string     `gorm:"column:named_identity_binding_id;size:30;not null;default:''" json:"-"`
	NamedIdentityBindingCreatedAt *time.Time `gorm:"column:named_identity_binding_created_at;precision:6" json:"-"`
	NamedIdentityConfigRevision   string     `gorm:"column:named_identity_config_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityPolicyRevision   string     `gorm:"column:named_identity_policy_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityUserCreatedAt    *time.Time `gorm:"column:named_identity_user_created_at;precision:6" json:"-"`
}

func (namedIdentityMFAChallengeV98) TableName() string { return "mfa_challenges" }

type namedIdentityMFAChallengeProofV98 struct {
	PrimaryMethod string `gorm:"size:20;not null;default:'';check:ck_mfa_challenges_oidc_primary,(((OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 108 AND ASCII(SUBSTRING(primary_method,2,1)) = 100 AND ASCII(SUBSTRING(primary_method,3,1)) = 97 AND ASCII(SUBSTRING(primary_method,4,1)) = 112 AND CHAR_LENGTH(ldap_binding_id) > 0 AND ldap_binding_created_at IS NOT NULL AND CHAR_LENGTH(ldap_config_revision) = 64 AND CHAR_LENGTH(ldap_policy_revision) = 64 AND ldap_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 115 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 109 AND ASCII(SUBSTRING(primary_method,4,1)) = 108 AND CHAR_LENGTH(saml_binding_id) > 0 AND saml_binding_created_at IS NOT NULL AND CHAR_LENGTH(saml_config_revision) = 64 AND CHAR_LENGTH(saml_policy_revision) = 64 AND saml_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL)) AND OCTET_LENGTH(named_identity_provider_id) = 0 AND OCTET_LENGTH(named_identity_profile_id) = 0 AND OCTET_LENGTH(named_identity_binding_id) = 0 AND named_identity_binding_created_at IS NULL AND OCTET_LENGTH(named_identity_config_revision) = 0 AND OCTET_LENGTH(named_identity_policy_revision) = 0 AND named_identity_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 6 AND ASCII(SUBSTRING(primary_method,1,1)) = 103 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 116 AND ASCII(SUBSTRING(primary_method,4,1)) = 104 AND ASCII(SUBSTRING(primary_method,5,1)) = 117 AND ASCII(SUBSTRING(primary_method,6,1)) = 98 AND OCTET_LENGTH(named_identity_provider_id) = 6 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 98 AND OCTET_LENGTH(named_identity_profile_id) = 23 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 98 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 109 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,15,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,16,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,17,1)) = 45 AND ASCII(SUBSTRING(named_identity_profile_id,18,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,19,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,20,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,21,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,22,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,23,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)" json:"-"`
}

func (namedIdentityMFAChallengeProofV98) TableName() string { return "mfa_challenges" }

type namedIdentityRootJobV98 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5 OR inventory_version = 6"`
	Domain           int `gorm:"not null;check:ck_secret_rotation_domain,(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7) OR (inventory_version = 3 AND domain >= 0 AND domain <= 8) OR (inventory_version = 4 AND domain >= 0 AND domain <= 9) OR (inventory_version = 5 AND domain >= 0 AND domain <= 10) OR (inventory_version = 6 AND domain >= 0 AND domain <= 11)"`
}

func (namedIdentityRootJobV98) TableName() string { return "secret_rotation_jobs" }

type namedIdentityRootItemV98 struct {
	Domain string `gorm:"primaryKey;size:32;check:ck_secret_item_domain,(((((CHAR_LENGTH(domain) = 20 AND ASCII(SUBSTRING(domain,1,1)) = 112 AND ASCII(SUBSTRING(domain,2,1)) = 114 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 118 AND ASCII(SUBSTRING(domain,5,1)) = 105 AND ASCII(SUBSTRING(domain,6,1)) = 100 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 95 AND ASCII(SUBSTRING(domain,10,1)) = 99 AND ASCII(SUBSTRING(domain,11,1)) = 114 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 100 AND ASCII(SUBSTRING(domain,14,1)) = 101 AND ASCII(SUBSTRING(domain,15,1)) = 110 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 105 AND ASCII(SUBSTRING(domain,18,1)) = 97 AND ASCII(SUBSTRING(domain,19,1)) = 108 AND ASCII(SUBSTRING(domain,20,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 101 AND ASCII(SUBSTRING(domain,2,1)) = 103 AND ASCII(SUBSTRING(domain,3,1)) = 114 AND ASCII(SUBSTRING(domain,4,1)) = 101 AND ASCII(SUBSTRING(domain,5,1)) = 115 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 115) OR (CHAR_LENGTH(domain) = 13 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 109 AND ASCII(SUBSTRING(domain,3,1)) = 116 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 116 AND ASCII(SUBSTRING(domain,9,1)) = 116 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 110 AND ASCII(SUBSTRING(domain,12,1)) = 103 AND ASCII(SUBSTRING(domain,13,1)) = 115) OR (CHAR_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 116 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 97 AND ASCII(SUBSTRING(domain,6,1)) = 103 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 95 AND ASCII(SUBSTRING(domain,9,1)) = 114 AND ASCII(SUBSTRING(domain,10,1)) = 101 AND ASCII(SUBSTRING(domain,11,1)) = 118 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 115 AND ASCII(SUBSTRING(domain,14,1)) = 105 AND ASCII(SUBSTRING(domain,15,1)) = 111 AND ASCII(SUBSTRING(domain,16,1)) = 110 AND ASCII(SUBSTRING(domain,17,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 117 AND ASCII(SUBSTRING(domain,2,1)) = 115 AND ASCII(SUBSTRING(domain,3,1)) = 101 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 109 AND ASCII(SUBSTRING(domain,7,1)) = 102 AND ASCII(SUBSTRING(domain,8,1)) = 97)) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 119 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 105 AND ASCII(SUBSTRING(domain,10,1)) = 116 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 101 AND ASCII(SUBSTRING(domain,9,1)) = 97 AND ASCII(SUBSTRING(domain,10,1)) = 100 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104))) OR (OCTET_LENGTH(domain) = 14 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 105 AND ASCII(SUBSTRING(domain,3,1)) = 100 AND ASCII(SUBSTRING(domain,4,1)) = 99 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 112 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 111 AND ASCII(SUBSTRING(domain,9,1)) = 118 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 100 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 114 AND ASCII(SUBSTRING(domain,14,1)) = 115) OR (OCTET_LENGTH(domain) = 15 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 116 AND ASCII(SUBSTRING(domain,5,1)) = 104 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 112 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 111 AND ASCII(SUBSTRING(domain,10,1)) = 118 AND ASCII(SUBSTRING(domain,11,1)) = 105 AND ASCII(SUBSTRING(domain,12,1)) = 100 AND ASCII(SUBSTRING(domain,13,1)) = 101 AND ASCII(SUBSTRING(domain,14,1)) = 114 AND ASCII(SUBSTRING(domain,15,1)) = 115) OR (OCTET_LENGTH(domain) = 14 AND ASCII(SUBSTRING(domain,1,1)) = 108 AND ASCII(SUBSTRING(domain,2,1)) = 100 AND ASCII(SUBSTRING(domain,3,1)) = 97 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 112 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 111 AND ASCII(SUBSTRING(domain,9,1)) = 118 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 100 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 114 AND ASCII(SUBSTRING(domain,14,1)) = 115)) OR (OCTET_LENGTH(domain) = 24 AND ASCII(SUBSTRING(domain,1,1)) = 110 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 109 AND ASCII(SUBSTRING(domain,4,1)) = 101 AND ASCII(SUBSTRING(domain,5,1)) = 100 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 105 AND ASCII(SUBSTRING(domain,8,1)) = 100 AND ASCII(SUBSTRING(domain,9,1)) = 101 AND ASCII(SUBSTRING(domain,10,1)) = 110 AND ASCII(SUBSTRING(domain,11,1)) = 116 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 116 AND ASCII(SUBSTRING(domain,14,1)) = 121 AND ASCII(SUBSTRING(domain,15,1)) = 95 AND ASCII(SUBSTRING(domain,16,1)) = 112 AND ASCII(SUBSTRING(domain,17,1)) = 114 AND ASCII(SUBSTRING(domain,18,1)) = 111 AND ASCII(SUBSTRING(domain,19,1)) = 118 AND ASCII(SUBSTRING(domain,20,1)) = 105 AND ASCII(SUBSTRING(domain,21,1)) = 100 AND ASCII(SUBSTRING(domain,22,1)) = 101 AND ASCII(SUBSTRING(domain,23,1)) = 114 AND ASCII(SUBSTRING(domain,24,1)) = 115)"`
}

func (namedIdentityRootItemV98) TableName() string { return "secret_rotation_items" }

type namedIdentityRootProcessV98 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_process_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5 OR inventory_version = 6"`
}

func (namedIdentityRootProcessV98) TableName() string { return "secret_process_verifications" }

func namedIdentityMigration(db *gorm.DB) error {
	for _, m := range []any{&namedIdentitySessionV98{}, &namedIdentityMFAChallengeV98{}} {
		for _, f := range []string{"NamedIdentityProviderID", "NamedIdentityProfileID", "NamedIdentityBindingID", "NamedIdentityBindingCreatedAt", "NamedIdentityConfigRevision", "NamedIdentityPolicyRevision", "NamedIdentityUserCreatedAt"} {
			if !db.Migrator().HasColumn(m, f) {
				if err := db.Migrator().AddColumn(m, f); err != nil {
					return err
				}
			}
		}
	}
	models := []any{&namedIdentityProviderV98{}, &namedIdentityBindingV98{}, &namedIdentityCeremonyV98{}}
	if err := migrateTables(db, models...); err != nil {
		return err
	}
	for _, m := range append(models, &namedIdentitySessionV98{}, &namedIdentityMFAChallengeV98{}) {
		if err := validateNamedIdentityColumnsV98(db, m); err != nil {
			return err
		}
	}
	for _, m := range models {
		if err := validateNamedIdentityIndexesV98(db, m); err != nil {
			return err
		}
	}
	if err := namedIdentityConstraintsV98(db); err != nil {
		return err
	}
	initial := namedIdentityProviderV98{ID: "github", ProfileID: "github.com.oauth-app.v1", IdentityIssuer: "https://github.com", ReviewRevision: strings.Repeat("0", 64), ConfigRevision: strings.Repeat("0", 64), PolicyRevision: strings.Repeat("0", 64), SecretGeneration: "0"}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error
}
func namedIdentityConstraintsV98(db *gorm.DB) error {
	checks := []struct {
		model any
		name  string
	}{
		{&namedIdentityCeremonyV98{}, "ck_named_identity_ceremonies_purpose"},
		{&namedIdentityCeremonyV98{}, "ck_named_identity_ceremonies_status"},
		{&namedIdentitySessionProofV98{}, "ck_sessions_oidc_primary"},
		{&namedIdentityMFAChallengeProofV98{}, "ck_mfa_challenges_oidc_primary"},
		{&namedIdentityRootJobV98{}, "ck_secret_inventory_version"},
		{&namedIdentityRootJobV98{}, "ck_secret_rotation_domain"},
		{&namedIdentityRootItemV98{}, "ck_secret_item_domain"},
		{&namedIdentityRootProcessV98{}, "ck_secret_process_inventory_version"},
	}
	// Validate every retained fact before replacing current constraint names. MySQL
	// partial DDL resumes under the existing ledger lock; no rollback is assumed.
	for _, v := range checks {
		st := &gorm.Statement{DB: db}
		if err := st.Parse(v.model); err != nil {
			return err
		}
		c, ok := st.Schema.ParseCheckConstraints()[v.name]
		if !ok {
			return fmt.Errorf("missing frozen named identity constraint")
		}
		var bad int64
		if err := db.Table(st.Schema.Table).Where("NOT (" + c.Constraint + ")").Count(&bad).Error; err != nil {
			return err
		}
		if bad != 0 {
			return fmt.Errorf("invalid retained named identity provenance")
		}
	}
	for _, v := range checks {
		if db.Migrator().HasConstraint(v.model, v.name) {
			if err := db.Migrator().DropConstraint(v.model, v.name); err != nil {
				return err
			}
		}
		if err := db.Migrator().CreateConstraint(v.model, v.name); err != nil {
			return err
		}
	}
	return nil
}
func validateNamedIdentityColumnsV98(db *gorm.DB, model any) error {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return err
	}
	cols, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		return err
	}
	byName := map[string]gorm.ColumnType{}
	for _, c := range cols {
		byName[c.Name()] = c
	}
	for _, f := range stmt.Schema.Fields {
		if f.DBName == "" {
			continue
		}
		c, ok := byName[f.DBName]
		if !ok {
			return fmt.Errorf("missing named identity column %s", f.DBName)
		}
		if f.PrimaryKey {
			primary, known := c.PrimaryKey()
			if !known || !primary {
				return fmt.Errorf("invalid named identity primary key %s", f.DBName)
			}
		}
		nullable, known := c.Nullable()
		if !known || (f.NotNull || f.PrimaryKey) && nullable || f.FieldType.Kind() == reflect.Pointer && !nullable {
			return fmt.Errorf("invalid named identity nullability %s", f.DBName)
		}
		kind := strings.ToLower(c.DatabaseTypeName())
		switch string(f.DataType) {
		case "time":
			value, hasDefault := c.DefaultValue()
			if hasDefault && value != "" && !strings.EqualFold(strings.TrimSpace(value), "NULL") {
				return fmt.Errorf("invalid named identity timestamp default %s", f.DBName)
			}
			precision, _, sized := c.DecimalSize()
			if !sized || precision != 6 || kind != "timestamp" && kind != "timestamptz" && kind != "datetime" {
				return fmt.Errorf("invalid named identity timestamp %s", f.DBName)
			}
		case "bool":
			if kind != "bool" && kind != "boolean" && kind != "tinyint" {
				return fmt.Errorf("invalid named identity boolean %s", f.DBName)
			}
		case "string":
			if f.Size > 0 && kind != "varchar" && kind != "character varying" {
				return fmt.Errorf("invalid named identity text kind %s", f.DBName)
			}
		case "text":
			if kind != "text" {
				return fmt.Errorf("invalid named identity text kind %s", f.DBName)
			}
		}
		if string(f.DataType) == "string" && f.Size > 0 {
			n, sized := c.Length()
			if !sized || n != int64(f.Size) {
				return fmt.Errorf("invalid named identity size %s", f.DBName)
			}
		}
		if !f.HasDefaultValue && string(f.DataType) != "time" {
			value, present := c.DefaultValue()
			if present && value != "" && !strings.EqualFold(strings.TrimSpace(value), "NULL") {
				return fmt.Errorf("invalid named identity unexpected default %s", f.DBName)
			}
		}
		if f.HasDefaultValue && f.DefaultValue == "" {
			value, present := c.DefaultValue()
			if !present || value != "" && value != "''" && value != "''::character varying" {
				return fmt.Errorf("invalid named identity default %s", f.DBName)
			}
		}
	}
	return nil
}

func validateNamedIdentityIndexesV98(db *gorm.DB, model any) error {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return err
	}
	// Reuse the V93 database-layer metadata adapter: GORM GetIndexes cannot
	// prove unconditional ordered keys on PostgreSQL or full keys on MySQL.
	// DDL remains GORM-owned; all table/index metadata identities are bound.
	for _, index := range stmt.Schema.ParseIndexes() {
		columns, err := credentialAttemptStatisticsIndexColumns(db, stmt.Schema.Table, index.Name)
		if err != nil {
			return err
		}
		if len(columns) != len(index.Fields) {
			return fmt.Errorf("invalid named identity index %s", index.Name)
		}
		unique := index.Class == "UNIQUE"
		for i, column := range columns {
			if !column.ColumnName.Valid || column.ColumnName.String != index.Fields[i].DBName || !column.Position.Valid || column.Position.Int64 != int64(i+1) || !column.IsUnique.Valid || column.IsUnique.Bool != unique || column.PrefixLength.Valid {
				return fmt.Errorf("invalid named identity index %s", index.Name)
			}
		}
	}
	return nil
}
