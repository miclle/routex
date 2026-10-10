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

// Literal private V98 schema projections are captured from the reviewed step.
// They are neither current entities nor runtime authentication proof.
type githubFixtureProviderV98 struct {
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

func (githubFixtureProviderV98) TableName() string { return "named_identity_providers" }

// githubFixtureBindingV98 preserves the provider namespace and exact subject kind.
type githubFixtureBindingV98 struct {
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

func (githubFixtureBindingV98) TableName() string { return "named_identity_bindings" }

// githubFixtureCeremonyV98 stores only bounded hashes and typed provenance, never remote tokens.
type githubFixtureCeremonyV98 struct {
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

func (githubFixtureCeremonyV98) TableName() string { return "named_identity_ceremonies" }

type githubFixtureSessionV98 struct {
	NamedIdentityProviderID       string     `gorm:"column:named_identity_provider_id;size:30;not null;default:''" json:"-"`
	NamedIdentityProfileID        string     `gorm:"column:named_identity_profile_id;size:64;not null;default:''" json:"-"`
	NamedIdentityBindingID        string     `gorm:"column:named_identity_binding_id;size:30;not null;default:''" json:"-"`
	NamedIdentityBindingCreatedAt *time.Time `gorm:"column:named_identity_binding_created_at;precision:6" json:"-"`
	NamedIdentityConfigRevision   string     `gorm:"column:named_identity_config_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityPolicyRevision   string     `gorm:"column:named_identity_policy_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityUserCreatedAt    *time.Time `gorm:"column:named_identity_user_created_at;precision:6" json:"-"`
}

func (githubFixtureSessionV98) TableName() string { return "sessions" }

type githubFixtureSessionProofV98 struct {
	PrimaryMethod string `gorm:"size:20;not null;default:'';check:ck_sessions_oidc_primary,(((OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 108 AND ASCII(SUBSTRING(primary_method,2,1)) = 100 AND ASCII(SUBSTRING(primary_method,3,1)) = 97 AND ASCII(SUBSTRING(primary_method,4,1)) = 112 AND CHAR_LENGTH(ldap_binding_id) > 0 AND ldap_binding_created_at IS NOT NULL AND CHAR_LENGTH(ldap_config_revision) = 64 AND CHAR_LENGTH(ldap_policy_revision) = 64 AND ldap_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 115 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 109 AND ASCII(SUBSTRING(primary_method,4,1)) = 108 AND CHAR_LENGTH(saml_binding_id) > 0 AND saml_binding_created_at IS NOT NULL AND CHAR_LENGTH(saml_config_revision) = 64 AND CHAR_LENGTH(saml_policy_revision) = 64 AND saml_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL)) AND OCTET_LENGTH(named_identity_provider_id) = 0 AND OCTET_LENGTH(named_identity_profile_id) = 0 AND OCTET_LENGTH(named_identity_binding_id) = 0 AND named_identity_binding_created_at IS NULL AND OCTET_LENGTH(named_identity_config_revision) = 0 AND OCTET_LENGTH(named_identity_policy_revision) = 0 AND named_identity_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 6 AND ASCII(SUBSTRING(primary_method,1,1)) = 103 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 116 AND ASCII(SUBSTRING(primary_method,4,1)) = 104 AND ASCII(SUBSTRING(primary_method,5,1)) = 117 AND ASCII(SUBSTRING(primary_method,6,1)) = 98 AND OCTET_LENGTH(named_identity_provider_id) = 6 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 98 AND OCTET_LENGTH(named_identity_profile_id) = 23 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 98 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 109 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,15,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,16,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,17,1)) = 45 AND ASCII(SUBSTRING(named_identity_profile_id,18,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,19,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,20,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,21,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,22,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,23,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)" json:"-"`
}

func (githubFixtureSessionProofV98) TableName() string { return "sessions" }

type githubFixtureMFAChallengeV98 struct {
	NamedIdentityProviderID       string     `gorm:"column:named_identity_provider_id;size:30;not null;default:''" json:"-"`
	NamedIdentityProfileID        string     `gorm:"column:named_identity_profile_id;size:64;not null;default:''" json:"-"`
	NamedIdentityBindingID        string     `gorm:"column:named_identity_binding_id;size:30;not null;default:''" json:"-"`
	NamedIdentityBindingCreatedAt *time.Time `gorm:"column:named_identity_binding_created_at;precision:6" json:"-"`
	NamedIdentityConfigRevision   string     `gorm:"column:named_identity_config_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityPolicyRevision   string     `gorm:"column:named_identity_policy_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityUserCreatedAt    *time.Time `gorm:"column:named_identity_user_created_at;precision:6" json:"-"`
}

func (githubFixtureMFAChallengeV98) TableName() string { return "mfa_challenges" }

type githubFixtureMFAChallengeProofV98 struct {
	PrimaryMethod string `gorm:"size:20;not null;default:'';check:ck_mfa_challenges_oidc_primary,(((OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 108 AND ASCII(SUBSTRING(primary_method,2,1)) = 100 AND ASCII(SUBSTRING(primary_method,3,1)) = 97 AND ASCII(SUBSTRING(primary_method,4,1)) = 112 AND CHAR_LENGTH(ldap_binding_id) > 0 AND ldap_binding_created_at IS NOT NULL AND CHAR_LENGTH(ldap_config_revision) = 64 AND CHAR_LENGTH(ldap_policy_revision) = 64 AND ldap_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 115 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 109 AND ASCII(SUBSTRING(primary_method,4,1)) = 108 AND CHAR_LENGTH(saml_binding_id) > 0 AND saml_binding_created_at IS NOT NULL AND CHAR_LENGTH(saml_config_revision) = 64 AND CHAR_LENGTH(saml_policy_revision) = 64 AND saml_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL)) AND OCTET_LENGTH(named_identity_provider_id) = 0 AND OCTET_LENGTH(named_identity_profile_id) = 0 AND OCTET_LENGTH(named_identity_binding_id) = 0 AND named_identity_binding_created_at IS NULL AND OCTET_LENGTH(named_identity_config_revision) = 0 AND OCTET_LENGTH(named_identity_policy_revision) = 0 AND named_identity_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 6 AND ASCII(SUBSTRING(primary_method,1,1)) = 103 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 116 AND ASCII(SUBSTRING(primary_method,4,1)) = 104 AND ASCII(SUBSTRING(primary_method,5,1)) = 117 AND ASCII(SUBSTRING(primary_method,6,1)) = 98 AND OCTET_LENGTH(named_identity_provider_id) = 6 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 98 AND OCTET_LENGTH(named_identity_profile_id) = 23 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 98 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 109 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,15,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,16,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,17,1)) = 45 AND ASCII(SUBSTRING(named_identity_profile_id,18,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,19,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,20,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,21,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,22,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,23,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)" json:"-"`
}

func (githubFixtureMFAChallengeProofV98) TableName() string { return "mfa_challenges" }

type githubFixtureRootJobV98 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5 OR inventory_version = 6"`
	Domain           int `gorm:"not null;check:ck_secret_rotation_domain,(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7) OR (inventory_version = 3 AND domain >= 0 AND domain <= 8) OR (inventory_version = 4 AND domain >= 0 AND domain <= 9) OR (inventory_version = 5 AND domain >= 0 AND domain <= 10) OR (inventory_version = 6 AND domain >= 0 AND domain <= 11)"`
}

func (githubFixtureRootJobV98) TableName() string { return "secret_rotation_jobs" }

type githubFixtureRootItemV98 struct {
	Domain string `gorm:"primaryKey;size:32;check:ck_secret_item_domain,(((((CHAR_LENGTH(domain) = 20 AND ASCII(SUBSTRING(domain,1,1)) = 112 AND ASCII(SUBSTRING(domain,2,1)) = 114 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 118 AND ASCII(SUBSTRING(domain,5,1)) = 105 AND ASCII(SUBSTRING(domain,6,1)) = 100 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 95 AND ASCII(SUBSTRING(domain,10,1)) = 99 AND ASCII(SUBSTRING(domain,11,1)) = 114 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 100 AND ASCII(SUBSTRING(domain,14,1)) = 101 AND ASCII(SUBSTRING(domain,15,1)) = 110 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 105 AND ASCII(SUBSTRING(domain,18,1)) = 97 AND ASCII(SUBSTRING(domain,19,1)) = 108 AND ASCII(SUBSTRING(domain,20,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 101 AND ASCII(SUBSTRING(domain,2,1)) = 103 AND ASCII(SUBSTRING(domain,3,1)) = 114 AND ASCII(SUBSTRING(domain,4,1)) = 101 AND ASCII(SUBSTRING(domain,5,1)) = 115 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 115) OR (CHAR_LENGTH(domain) = 13 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 109 AND ASCII(SUBSTRING(domain,3,1)) = 116 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 116 AND ASCII(SUBSTRING(domain,9,1)) = 116 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 110 AND ASCII(SUBSTRING(domain,12,1)) = 103 AND ASCII(SUBSTRING(domain,13,1)) = 115) OR (CHAR_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 116 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 97 AND ASCII(SUBSTRING(domain,6,1)) = 103 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 95 AND ASCII(SUBSTRING(domain,9,1)) = 114 AND ASCII(SUBSTRING(domain,10,1)) = 101 AND ASCII(SUBSTRING(domain,11,1)) = 118 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 115 AND ASCII(SUBSTRING(domain,14,1)) = 105 AND ASCII(SUBSTRING(domain,15,1)) = 111 AND ASCII(SUBSTRING(domain,16,1)) = 110 AND ASCII(SUBSTRING(domain,17,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 117 AND ASCII(SUBSTRING(domain,2,1)) = 115 AND ASCII(SUBSTRING(domain,3,1)) = 101 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 109 AND ASCII(SUBSTRING(domain,7,1)) = 102 AND ASCII(SUBSTRING(domain,8,1)) = 97)) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 119 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 105 AND ASCII(SUBSTRING(domain,10,1)) = 116 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 101 AND ASCII(SUBSTRING(domain,9,1)) = 97 AND ASCII(SUBSTRING(domain,10,1)) = 100 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104))) OR (OCTET_LENGTH(domain) = 14 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 105 AND ASCII(SUBSTRING(domain,3,1)) = 100 AND ASCII(SUBSTRING(domain,4,1)) = 99 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 112 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 111 AND ASCII(SUBSTRING(domain,9,1)) = 118 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 100 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 114 AND ASCII(SUBSTRING(domain,14,1)) = 115) OR (OCTET_LENGTH(domain) = 15 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 116 AND ASCII(SUBSTRING(domain,5,1)) = 104 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 112 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 111 AND ASCII(SUBSTRING(domain,10,1)) = 118 AND ASCII(SUBSTRING(domain,11,1)) = 105 AND ASCII(SUBSTRING(domain,12,1)) = 100 AND ASCII(SUBSTRING(domain,13,1)) = 101 AND ASCII(SUBSTRING(domain,14,1)) = 114 AND ASCII(SUBSTRING(domain,15,1)) = 115) OR (OCTET_LENGTH(domain) = 14 AND ASCII(SUBSTRING(domain,1,1)) = 108 AND ASCII(SUBSTRING(domain,2,1)) = 100 AND ASCII(SUBSTRING(domain,3,1)) = 97 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 112 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 111 AND ASCII(SUBSTRING(domain,9,1)) = 118 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 100 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 114 AND ASCII(SUBSTRING(domain,14,1)) = 115)) OR (OCTET_LENGTH(domain) = 24 AND ASCII(SUBSTRING(domain,1,1)) = 110 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 109 AND ASCII(SUBSTRING(domain,4,1)) = 101 AND ASCII(SUBSTRING(domain,5,1)) = 100 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 105 AND ASCII(SUBSTRING(domain,8,1)) = 100 AND ASCII(SUBSTRING(domain,9,1)) = 101 AND ASCII(SUBSTRING(domain,10,1)) = 110 AND ASCII(SUBSTRING(domain,11,1)) = 116 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 116 AND ASCII(SUBSTRING(domain,14,1)) = 121 AND ASCII(SUBSTRING(domain,15,1)) = 95 AND ASCII(SUBSTRING(domain,16,1)) = 112 AND ASCII(SUBSTRING(domain,17,1)) = 114 AND ASCII(SUBSTRING(domain,18,1)) = 111 AND ASCII(SUBSTRING(domain,19,1)) = 118 AND ASCII(SUBSTRING(domain,20,1)) = 105 AND ASCII(SUBSTRING(domain,21,1)) = 100 AND ASCII(SUBSTRING(domain,22,1)) = 101 AND ASCII(SUBSTRING(domain,23,1)) = 114 AND ASCII(SUBSTRING(domain,24,1)) = 115)"`
}

func (githubFixtureRootItemV98) TableName() string { return "secret_rotation_items" }

type githubFixtureRootProcessV98 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_process_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5 OR inventory_version = 6"`
}

func (githubFixtureRootProcessV98) TableName() string { return "secret_process_verifications" }

type githubWrongSubjectWidthV98 struct {
	Subject string `gorm:"size:255;not null"`
}

func (githubWrongSubjectWidthV98) TableName() string { return "named_identity_bindings" }

type githubWrongBirthPrecisionV98 struct {
	UserCreatedAt time.Time `gorm:"precision:3;not null"`
}

func (githubWrongBirthPrecisionV98) TableName() string { return "named_identity_bindings" }

type githubWrongOptionalNullV98 struct {
	VerifiedAt time.Time `gorm:"precision:6;not null"`
}

func (githubWrongOptionalNullV98) TableName() string { return "named_identity_ceremonies" }

type githubWrongOwnerIndexV98 struct {
	ProviderID string `gorm:"index:idx_named_identity_member,priority:1"`
	UserID     string `gorm:"index:idx_named_identity_member,priority:2"`
}

func (githubWrongOwnerIndexV98) TableName() string { return "named_identity_bindings" }

type githubWrongOwnerOrderV98 struct {
	UserID     string `gorm:"uniqueIndex:idx_named_identity_member,priority:1"`
	ProviderID string `gorm:"uniqueIndex:idx_named_identity_member,priority:2"`
}

func (githubWrongOwnerOrderV98) TableName() string { return "named_identity_bindings" }

// Integration-only pristine rewind. Never deletes an identity/proof or deployed
// history: all new objects, all seven proof columns and V6 work must be empty.
func legacyMigrationBeforeV98(t *testing.T, db *gorm.DB) {
	t.Helper()
	legacyMigrationBeforeV99(t, db)
	rows := personalKeyBehaviorLedger(t, db)
	if len(rows) != 98 {
		t.Fatal("historical fixture requires exact current V98")
	}
	for i, row := range rows {
		if row.Version != i+1 {
			t.Fatal("noncontiguous V98 ledger", i)
		}
	}
	for table, want := range map[string]int64{"named_identity_providers": 1, "named_identity_bindings": 0, "named_identity_ceremonies": 0} {
		var count int64
		if db.Table(table).Count(&count).Error != nil || count != want {
			t.Fatal("historical fixture requires pristine named identity objects", table, count)
		}
	}
	var provider githubFixtureProviderV98
	if db.Session(&gorm.Session{QueryFields: true}).Take(&provider, "id = ?", "github").Error != nil || provider.CreatedAt.IsZero() || provider.UpdatedAt.IsZero() {
		t.Fatal("default GitHub singleton")
	}
	provider.CreatedAt, provider.UpdatedAt = time.Time{}, time.Time{}
	if !reflect.DeepEqual(provider, githubFixtureProviderV98{ID: "github", ProfileID: githubFixtureProfile, IdentityIssuer: "https://github.com", ReviewRevision: strings.Repeat("0", 64), ConfigRevision: strings.Repeat("0", 64), PolicyRevision: strings.Repeat("0", 64), SecretGeneration: "0"}) {
		t.Fatal("historical rewind cannot discard configured identity")
	}
	for _, table := range []string{"sessions", "mfa_challenges"} {
		var n int64
		if db.Table(table).Where("OCTET_LENGTH(named_identity_provider_id) <> 0 OR OCTET_LENGTH(named_identity_profile_id) <> 0 OR OCTET_LENGTH(named_identity_binding_id) <> 0 OR named_identity_binding_created_at IS NOT NULL OR OCTET_LENGTH(named_identity_config_revision) <> 0 OR OCTET_LENGTH(named_identity_policy_revision) <> 0 OR named_identity_user_created_at IS NOT NULL OR primary_method = ?", "github").Count(&n).Error != nil || n != 0 {
			t.Fatal("historical rewind contains named primary proof", table)
		}
	}
	for _, table := range []string{"secret_rotation_jobs", "secret_process_verifications"} {
		var n int64
		if db.Table(table).Where("inventory_version > ?", 5).Count(&n).Error != nil || n != 0 {
			t.Fatal("historical rewind contains V6 inventory", table)
		}
	}
	var n int64
	if db.Table("secret_rotation_items").Where("domain = ?", "named_identity_providers").Count(&n).Error != nil || n != 0 {
		t.Fatal("historical rewind contains named inventory item")
	}
	if database.MigrateThrough(db.Statement.Context, db, 97) == nil || !reflect.DeepEqual(rows, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("bounded97 must reject V98 ledger before mutation")
	}
	for _, v := range []struct {
		model any
		name  string
	}{{&samlFixtureSessionProofV97{}, "ck_sessions_oidc_primary"}, {&samlFixtureMFAProofV97{}, "ck_mfa_challenges_oidc_primary"}, {&ldapFixtureRootJobV96{}, "ck_secret_inventory_version"}, {&ldapFixtureRootJobV96{}, "ck_secret_rotation_domain"}, {&ldapFixtureRootItemV96{}, "ck_secret_item_domain"}, {&ldapFixtureRootProcessV96{}, "ck_secret_process_inventory_version"}} {
		if db.Migrator().DropConstraint(v.model, v.name) != nil || db.Migrator().CreateConstraint(v.model, v.name) != nil {
			t.Fatal("restore literal V97/V5 check", v.name)
		}
	}
	removed := db.Table("schema_migrations").Where("version = ?", 98).Delete(&struct{}{})
	if removed.Error != nil || removed.RowsAffected != 1 || !reflect.DeepEqual(rows[:97], personalKeyBehaviorLedger(t, db)) {
		t.Fatal("remove only owned V98 ledger")
	}
}

func testGitHubMigration(t *testing.T, db *gorm.DB) {
	legacyMigrationBeforeV99(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	db = db.WithContext(ctx)
	original := personalKeyBehaviorLedger(t, db)
	if len(original) != 98 {
		t.Fatal("exact V98 ledger")
	}
	for i, row := range original {
		if row.Version != i+1 {
			t.Fatal("released prefix", i)
		}
	}
	ledger := func(present bool) {
		t.Helper()
		rows := personalKeyBehaviorLedger(t, db)
		n := 97
		if present {
			n = 98
		}
		if len(rows) != n || !reflect.DeepEqual(rows[:97], original[:97]) || present && rows[97].Version != 98 {
			t.Fatal("released V1-V97 prefix or V98 suffix changed")
		}
	}
	remove := func() {
		t.Helper()
		x := db.Table("schema_migrations").Where("version = ?", 98).Delete(&struct{}{})
		if x.Error != nil || x.RowsAffected != 1 {
			t.Fatal("remove only98")
		}
		ledger(false)
	}
	migrate := func() {
		t.Helper()
		if e := database.MigrateThrough(ctx, db, 98); e != nil {
			t.Fatal("restore98", e)
		}
		ledger(true)
	}
	readProvider := func() githubFixtureProviderV98 {
		t.Helper()
		var p githubFixtureProviderV98
		if db.Session(&gorm.Session{QueryFields: true}).Take(&p, "id = ?", "github").Error != nil {
			t.Fatal("GitHub singleton")
		}
		return p
	}
	assertDefault := func() {
		t.Helper()
		for table, want := range map[string]int64{"named_identity_providers": 1, "named_identity_bindings": 0, "named_identity_ceremonies": 0} {
			var n int64
			if db.Table(table).Count(&n).Error != nil || n != want {
				t.Fatal("empty named identity state", table, n)
			}
		}
		p := readProvider()
		if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
			t.Fatal("singleton births")
		}
		p.CreatedAt, p.UpdatedAt = time.Time{}, time.Time{}
		if !reflect.DeepEqual(p, githubFixtureProviderV98{ID: "github", ProfileID: githubFixtureProfile, IdentityIssuer: "https://github.com", ReviewRevision: strings.Repeat("0", 64), ConfigRevision: strings.Repeat("0", 64), PolicyRevision: strings.Repeat("0", 64), SecretGeneration: "0"}) {
			t.Fatal("default invented configuration")
		}
	}
	assertDefault()
	for _, bound := range []int{0, -1, 97, 102} {
		if database.MigrateThrough(ctx, db, bound) == nil || !reflect.DeepEqual(original, personalKeyBehaviorLedger(t, db)) {
			t.Fatal("invalid/newer bound admitted", bound)
		}
	}
	birth := time.Now().UTC().Truncate(time.Microsecond)
	var sessions []entity.Session
	var challenges []entity.MFAChallenge
	for i, method := range []string{"", "oidc", "oauth", "ldap", "saml"} {
		user := entity.User{ID: []string{"usr_github_local", "usr_github_oidc", "usr_github_oauth", "usr_github_ldap", "usr_github_saml"}[i], Email: []string{"github-local@example.invalid", "github-oidc@example.invalid", "github-oauth@example.invalid", "github-ldap@example.invalid", "github-saml@example.invalid"}[i], Name: "Retained member", Role: entity.RoleMember, PasswordHash: "retained-not-proof"}
		if db.Create(&user).Error != nil {
			t.Fatal("retained user")
		}
		row := entity.Session{ID: []string{"ses_github_local", "ses_github_oidc", "ses_github_oauth", "ses_github_ldap", "ses_github_saml"}[i], UserID: user.ID, TokenHash: strings.Repeat(string(rune('a'+i)), 64), CreatedAt: birth, ExpiresAt: birth.Add(time.Hour), PrimaryMethod: method}
		c := entity.MFAChallenge{UserID: user.ID, Purpose: "login", TokenHash: strings.Repeat(string(rune('f'+i)), 64), PasswordDigest: strings.Repeat("a", 64), Generation: strings.Repeat("b", 64), ExpiresAt: birth.Add(time.Minute), PrimaryMethod: method}
		switch method {
		case "oidc":
			row.OIDCBindingID = "oib_retained"
			row.OIDCBindingCreatedAt = &birth
			row.OIDCConfigRevision = strings.Repeat("a", 64)
			row.OIDCPolicyRevision = strings.Repeat("b", 64)
			row.OIDCUserCreatedAt = &user.CreatedAt
			c.OIDCBindingID = row.OIDCBindingID
			c.OIDCBindingCreatedAt = &birth
			c.OIDCConfigRevision = row.OIDCConfigRevision
			c.OIDCPolicyRevision = row.OIDCPolicyRevision
			c.OIDCUserCreatedAt = &user.CreatedAt
		case "oauth":
			row.OAuthBindingID = "oab_retained"
			row.OAuthBindingCreatedAt = &birth
			row.OAuthConfigRevision = strings.Repeat("a", 64)
			row.OAuthPolicyRevision = strings.Repeat("b", 64)
			row.OAuthUserCreatedAt = &user.CreatedAt
			c.OAuthBindingID = row.OAuthBindingID
			c.OAuthBindingCreatedAt = &birth
			c.OAuthConfigRevision = row.OAuthConfigRevision
			c.OAuthPolicyRevision = row.OAuthPolicyRevision
			c.OAuthUserCreatedAt = &user.CreatedAt
		case "ldap":
			row.LDAPBindingID = "ldb_retained"
			row.LDAPBindingCreatedAt = &birth
			row.LDAPConfigRevision = strings.Repeat("a", 64)
			row.LDAPPolicyRevision = strings.Repeat("b", 64)
			row.LDAPUserCreatedAt = &user.CreatedAt
			c.LDAPBindingID = row.LDAPBindingID
			c.LDAPBindingCreatedAt = &birth
			c.LDAPConfigRevision = row.LDAPConfigRevision
			c.LDAPPolicyRevision = row.LDAPPolicyRevision
			c.LDAPUserCreatedAt = &user.CreatedAt
		case "saml":
			row.SAMLBindingID = "smb_retained"
			row.SAMLBindingCreatedAt = &birth
			row.SAMLConfigRevision = strings.Repeat("a", 64)
			row.SAMLPolicyRevision = strings.Repeat("b", 64)
			row.SAMLUserCreatedAt = &user.CreatedAt
			c.SAMLBindingID = row.SAMLBindingID
			c.SAMLBindingCreatedAt = &birth
			c.SAMLConfigRevision = row.SAMLConfigRevision
			c.SAMLPolicyRevision = row.SAMLPolicyRevision
			c.SAMLUserCreatedAt = &user.CreatedAt
		}
		if db.Create(&row).Error != nil || db.Create(&c).Error != nil {
			t.Fatal("retained primary positive", method)
		}
		if db.Session(&gorm.Session{QueryFields: true}).Take(&row, "id = ?", row.ID).Error != nil || db.Session(&gorm.Session{QueryFields: true}).Take(&c, "user_id = ? AND purpose = ?", user.ID, "login").Error != nil {
			t.Fatal("capture retained rows")
		}
		sessions = append(sessions, row)
		challenges = append(challenges, c)
	}
	retained := func() {
		t.Helper()
		for i, want := range sessions {
			var got entity.Session
			var c entity.MFAChallenge
			if db.Session(&gorm.Session{QueryFields: true}).Take(&got, "id = ?", want.ID).Error != nil || db.Session(&gorm.Session{QueryFields: true}).Take(&c, "user_id = ? AND purpose = ?", challenges[i].UserID, "login").Error != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(c, challenges[i]) {
				t.Fatal("retained exact primary changed", i)
			}
		}
	}
	checks := []struct {
		model, proof, old any
		name              string
	}{{&githubFixtureSessionV98{}, &githubFixtureSessionProofV98{}, &samlFixtureSessionProofV97{}, "ck_sessions_oidc_primary"}, {&githubFixtureMFAChallengeV98{}, &githubFixtureMFAChallengeProofV98{}, &samlFixtureMFAProofV97{}, "ck_mfa_challenges_oidc_primary"}}
	fields := []string{"NamedIdentityProviderID", "NamedIdentityProfileID", "NamedIdentityBindingID", "NamedIdentityBindingCreatedAt", "NamedIdentityConfigRevision", "NamedIdentityPolicyRevision", "NamedIdentityUserCreatedAt"}
	for _, v := range checks {
		if db.Migrator().DropConstraint(v.proof, v.name) != nil {
			t.Fatal("drop current primary check")
		}
		for _, field := range fields {
			if db.Migrator().DropColumn(v.model, field) != nil {
				t.Fatal("drop only additive named proof", field)
			}
		}
		if db.Migrator().CreateConstraint(v.old, v.name) != nil {
			t.Fatal("restore literal97 primary")
		}
	}
	for _, v := range []struct {
		model any
		name  string
	}{{&ldapFixtureRootJobV96{}, "ck_secret_inventory_version"}, {&ldapFixtureRootJobV96{}, "ck_secret_rotation_domain"}, {&ldapFixtureRootItemV96{}, "ck_secret_item_domain"}, {&ldapFixtureRootProcessV96{}, "ck_secret_process_inventory_version"}} {
		if db.Migrator().DropConstraint(v.model, v.name) != nil || db.Migrator().CreateConstraint(v.model, v.name) != nil {
			t.Fatal("restore V5 root check")
		}
	}
	if db.Migrator().DropTable(&githubFixtureCeremonyV98{}, &githubFixtureBindingV98{}, &githubFixtureProviderV98{}) != nil {
		t.Fatal("drop only98 tables")
	}
	remove()
	if database.MigrateThrough(ctx, db, 97) != nil {
		t.Fatal("faithful bounded97")
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- database.MigrateThrough(ctx, db, 98) })
	}
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal("concurrent V97 upgrade", e)
		}
	}
	migrate()
	assertDefault()
	retained()
	singleton := readProvider()
	current := func() {
		t.Helper()
		ledger(true)
		retained()
		if !reflect.DeepEqual(singleton, readProvider()) {
			t.Fatal("singleton changed")
		}
	}
	if db.Migrator().DropConstraint(checks[0].proof, checks[0].name) != nil || db.Migrator().DropColumn(checks[0].model, "NamedIdentityPolicyRevision") != nil {
		t.Fatal("partial additive DDL")
	}
	remove()
	migrate()
	current()
	remove()
	migrate()
	migrate()
	current()
	for _, bad := range []struct {
		model, good any
		field       string
	}{{&githubWrongSubjectWidthV98{}, &githubFixtureBindingV98{}, "Subject"}, {&githubWrongBirthPrecisionV98{}, &githubFixtureBindingV98{}, "UserCreatedAt"}, {&githubWrongOptionalNullV98{}, &githubFixtureCeremonyV98{}, "VerifiedAt"}} {
		current()
		if db.Migrator().AlterColumn(bad.model, bad.field) != nil {
			t.Fatal("install invalid shape")
		}
		remove()
		if database.MigrateThrough(ctx, db, 98) == nil {
			t.Fatal("invalid shape accepted", bad.field)
		}
		ledger(false)
		if db.Migrator().AlterColumn(bad.good, bad.field) != nil {
			t.Fatal("restore frozen shape")
		}
		migrate()
		current()
	}
	for _, bad := range []any{&githubWrongOwnerIndexV98{}, &githubWrongOwnerOrderV98{}} {
		current()
		if database.DropIndex(db, &githubFixtureBindingV98{}, "idx_named_identity_member") != nil || db.Migrator().CreateIndex(bad, "idx_named_identity_member") != nil {
			t.Fatal("install invalid owner index")
		}
		remove()
		if database.MigrateThrough(ctx, db, 98) == nil {
			t.Fatal("invalid owner index accepted")
		}
		ledger(false)
		if database.DropIndex(db, &githubFixtureBindingV98{}, "idx_named_identity_member") != nil || db.Migrator().CreateIndex(&githubFixtureBindingV98{}, "idx_named_identity_member") != nil {
			t.Fatal("restore exact ordered unique index")
		}
		migrate()
		current()
	}
	// Every preexisting primary remains a valid positive but must reject every
	// nonmatching named-identity field, including PAD SPACE and optional births.
	for i := 1; i < len(sessions); i++ {
		for _, target := range []struct {
			table, where string
			args         []any
		}{{"sessions", "id = ?", []any{sessions[i].ID}}, {"mfa_challenges", "user_id = ? AND purpose = ?", []any{challenges[i].UserID, "login"}}} {
			for _, bad := range []map[string]any{{"named_identity_provider_id": "github"}, {"named_identity_profile_id": " "}, {"named_identity_binding_id": "nib_mixed"}, {"named_identity_binding_created_at": birth}, {"named_identity_config_revision": " "}, {"named_identity_policy_revision": " "}, {"named_identity_user_created_at": birth}} {
				retained()
				if db.Transaction(func(tx *gorm.DB) error {
					return tx.Table(target.table).Where(target.where, target.args...).Updates(bad).Error
				}) == nil {
					t.Fatal("legacy primary admitted named proof", sessions[i].PrimaryMethod, target.table)
				}
				retained()
			}
		}
	}
	binding := githubFixtureBindingV98{ID: "nib_constraint", ProviderID: "github", ProfileID: githubFixtureProfile, IdentityIssuer: "https://github.com", UserID: sessions[0].UserID, UserCreatedAt: birth, ConfigRevision: strings.Repeat("a", 64), SubjectKind: "integer", Subject: "202", SubjectDigest: strings.Repeat("b", 64), CreatedAt: birth}
	if db.Create(&binding).Error != nil {
		t.Fatal("positive exact binding")
	}
	var bindingBefore githubFixtureBindingV98
	if db.Session(&gorm.Session{QueryFields: true}).Take(&bindingBefore, "id = ?", binding.ID).Error != nil {
		t.Fatal("capture stored exact binding", db.Name())
	}
	if fields := samlMigrationDifferentFields(bindingBefore, binding); len(fields) != 0 {
		t.Fatal("stored binding differs from authored facts", db.Name(), fields)
	}
	for badIndex, bad := range []map[string]any{{"provider_id": "GitHub"}, {"provider_id": "github "}, {"profile_id": "github.com.oauth-app.v1 "}, {"identity_issuer": "https://github.com/"}, {"subject_kind": "string"}, {"subject_kind": "integer "}} {
		var good githubFixtureBindingV98
		if db.Session(&gorm.Session{QueryFields: true}).Take(&good, "id = ?", binding.ID).Error != nil {
			t.Fatal("positive before binding denial read", db.Name(), badIndex)
		}
		if fields := samlMigrationDifferentFields(good, bindingBefore); len(fields) != 0 {
			t.Fatal("positive before binding denial", db.Name(), badIndex, fields)
		}
		if db.Transaction(func(tx *gorm.DB) error {
			return tx.Model(&githubFixtureBindingV98{}).Where("id = ?", binding.ID).Updates(bad).Error
		}) == nil {
			columns := make([]string, 0, len(bad))
			for column := range bad {
				columns = append(columns, column)
			}
			t.Fatal("named binding alias accepted", db.Name(), badIndex, columns)
		}
		var after githubFixtureBindingV98
		if db.Session(&gorm.Session{QueryFields: true}).Take(&after, "id = ?", binding.ID).Error != nil {
			t.Fatal("binding after denied mutation read", db.Name(), badIndex)
		}
		if fields := samlMigrationDifferentFields(after, bindingBefore); len(fields) != 0 {
			t.Fatal("denied binding mutation changed facts", db.Name(), badIndex, fields)
		}
	}
	for _, which := range []string{"member", "digest"} {
		e := db.Transaction(func(tx *gorm.DB) error {
			x := binding
			x.ID = "nib_duplicate"
			if which == "member" {
				x.SubjectDigest = strings.Repeat("c", 64)
			} else {
				x.UserID = "usr_other"
			}
			return tx.Create(&x).Error
		})
		if !errors.Is(e, gorm.ErrDuplicatedKey) {
			t.Fatal("unique exact owner/digest", which, e)
		}
	}
	for _, bad := range []string{"GitHub", "github ", "", " github"} {
		if db.Transaction(func(tx *gorm.DB) error {
			return tx.Model(&githubFixtureProviderV98{}).Where("id = ?", "github").Update("id", bad).Error
		}) == nil {
			t.Fatal("singleton alias accepted")
		}
	}
	ceremony := githubFixtureCeremonyV98{ID: "nic_constraint", ProviderID: "github", ProfileID: githubFixtureProfile, IdentityIssuer: "https://github.com", ProviderCreatedAt: singleton.CreatedAt, StateHash: strings.Repeat("a", 64), CookieHash: strings.Repeat("b", 64), Purpose: "login", Reason: "Constraint test", Status: "pending", ConfigRevision: singleton.ConfigRevision, PolicyRevision: singleton.PolicyRevision, ExpiresAt: birth.Add(time.Minute), CreatedAt: birth}
	if db.Create(&ceremony).Error != nil {
		t.Fatal("positive exact ceremony")
	}
	var ceremonyBefore githubFixtureCeremonyV98
	if db.Session(&gorm.Session{QueryFields: true}).Take(&ceremonyBefore, "id = ?", ceremony.ID).Error != nil {
		t.Fatal("capture stored exact ceremony", db.Name())
	}
	if fields := samlMigrationDifferentFields(ceremonyBefore, ceremony); len(fields) != 0 {
		t.Fatal("stored ceremony differs from authored facts", db.Name(), fields)
	}
	// These are schema-only rollback controls, never staged authentication proof.
	rollbackEnum := errors.New("rollback exact ceremony enum positive")
	for goodIndex, good := range []struct{ column, value string }{
		{"purpose", "login"}, {"purpose", "bind"}, {"purpose", "verify"},
		{"status", "pending"}, {"status", "exchanging"}, {"status", "verified"}, {"status", "consumed"}, {"status", "failed"},
	} {
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&githubFixtureCeremonyV98{}).Where("id = ?", ceremony.ID).Update(good.column, good.value).Error; err != nil {
				return err
			}
			var observed githubFixtureCeremonyV98
			if err := tx.Session(&gorm.Session{QueryFields: true}).Take(&observed, "id = ?", ceremony.ID).Error; err != nil {
				return err
			}
			want := ceremonyBefore
			if good.column == "purpose" {
				want.Purpose = good.value
			} else {
				want.Status = good.value
			}
			if len(samlMigrationDifferentFields(observed, want)) != 0 {
				return errors.New("exact ceremony enum positive changed facts")
			}
			return rollbackEnum
		})
		if !errors.Is(err, rollbackEnum) {
			t.Fatal("exact ceremony enum positive rejected", db.Name(), goodIndex, good.column)
		}
		var after githubFixtureCeremonyV98
		if db.Session(&gorm.Session{QueryFields: true}).Take(&after, "id = ?", ceremony.ID).Error != nil {
			t.Fatal("enum rollback read", db.Name(), goodIndex, good.column)
		}
		if fields := samlMigrationDifferentFields(after, ceremonyBefore); len(fields) != 0 {
			t.Fatal("enum positive did not roll back", db.Name(), goodIndex, good.column, fields)
		}
	}
	for badIndex, bad := range []map[string]any{{"purpose": "LOGIN"}, {"purpose": "login "}, {"status": "EXCHANGING"}, {"status": "exchanging "}, {"status": "unknown"}, {"subject_kind": " "}, {"profile_id": "other"}} {
		var good githubFixtureCeremonyV98
		if db.Session(&gorm.Session{QueryFields: true}).Take(&good, "id = ?", ceremony.ID).Error != nil {
			t.Fatal("positive before ceremony denial read", db.Name(), badIndex)
		}
		if fields := samlMigrationDifferentFields(good, ceremonyBefore); len(fields) != 0 {
			t.Fatal("positive before ceremony denial", db.Name(), badIndex, fields)
		}
		if db.Transaction(func(tx *gorm.DB) error {
			return tx.Model(&githubFixtureCeremonyV98{}).Where("id = ?", ceremony.ID).Updates(bad).Error
		}) == nil {
			columns := make([]string, 0, len(bad))
			for column := range bad {
				columns = append(columns, column)
			}
			t.Fatal("ceremony alias admitted", db.Name(), badIndex, columns)
		}
		var after githubFixtureCeremonyV98
		if db.Session(&gorm.Session{QueryFields: true}).Take(&after, "id = ?", ceremony.ID).Error != nil {
			t.Fatal("ceremony after denied mutation read", db.Name(), badIndex)
		}
		if fields := samlMigrationDifferentFields(after, ceremonyBefore); len(fields) != 0 {
			t.Fatal("denied ceremony mutation changed facts", db.Name(), badIndex, fields)
		}
	}
	for _, which := range []string{"state", "cookie"} {
		e := db.Transaction(func(tx *gorm.DB) error {
			x := ceremony
			x.ID = "nic_duplicate"
			x.StateHash = strings.Repeat("c", 64)
			x.CookieHash = strings.Repeat("d", 64)
			if which == "state" {
				x.StateHash = ceremony.StateHash
			} else {
				x.CookieHash = ceremony.CookieHash
			}
			return tx.Create(&x).Error
		})
		if !errors.Is(e, gorm.ErrDuplicatedKey) {
			t.Fatal("correlation unique", which, e)
		}
	}
	if db.Delete(&ceremony).Error != nil {
		t.Fatal("remove owned ceremony")
	}
	for _, target := range []struct {
		table, where string
		args         []any
	}{{"sessions", "id = ?", []any{sessions[0].ID}}, {"mfa_challenges", "user_id = ? AND purpose = ?", []any{challenges[0].UserID, "login"}}} {
		positive := map[string]any{"primary_method": "github", "named_identity_provider_id": "github", "named_identity_profile_id": githubFixtureProfile, "named_identity_binding_id": binding.ID, "named_identity_binding_created_at": birth, "named_identity_config_revision": strings.Repeat("a", 64), "named_identity_policy_revision": strings.Repeat("b", 64), "named_identity_user_created_at": birth}
		for _, bad := range []map[string]any{{"primary_method": "GITHUB"}, {"primary_method": "github "}, {"named_identity_provider_id": "github "}, {"named_identity_profile_id": ""}, {"named_identity_profile_id": "github.com.oauth-app.v1 "}, {"named_identity_binding_id": ""}, {"named_identity_binding_created_at": nil}, {"named_identity_config_revision": "short"}, {"named_identity_policy_revision": "short"}, {"named_identity_user_created_at": nil}, {"oidc_binding_id": "oib_mixed"}, {"oauth_binding_id": " "}, {"ldap_user_created_at": birth}, {"saml_binding_id": " "}} {
			if db.Table(target.table).Where(target.where, target.args...).Updates(positive).Error != nil {
				t.Fatal("complete named primary positive", target.table)
			}
			var before githubFixtureSessionV98
			if db.Table(target.table).Select("named_identity_provider_id,named_identity_profile_id,named_identity_binding_id,named_identity_binding_created_at,named_identity_config_revision,named_identity_policy_revision,named_identity_user_created_at").Where(target.where, target.args...).Take(&before).Error != nil || before.NamedIdentityBindingID != binding.ID || before.NamedIdentityProfileID != githubFixtureProfile {
				t.Fatal("positive proof capture")
			}
			if db.Transaction(func(tx *gorm.DB) error {
				return tx.Table(target.table).Where(target.where, target.args...).Updates(bad).Error
			}) == nil {
				t.Fatal("mixed/partial/alias named primary accepted", target.table)
			}
			var after githubFixtureSessionV98
			if db.Table(target.table).Select("named_identity_provider_id,named_identity_profile_id,named_identity_binding_id,named_identity_binding_created_at,named_identity_config_revision,named_identity_policy_revision,named_identity_user_created_at").Where(target.where, target.args...).Take(&after).Error != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("rejected primary rewrote proof")
			}
		}
		local := map[string]any{"primary_method": "", "named_identity_provider_id": "", "named_identity_profile_id": "", "named_identity_binding_id": "", "named_identity_binding_created_at": nil, "named_identity_config_revision": "", "named_identity_policy_revision": "", "named_identity_user_created_at": nil}
		if db.Table(target.table).Where(target.where, target.args...).Updates(local).Error != nil {
			t.Fatal("restore exact local primary")
		}
		for _, field := range []string{"named_identity_provider_id", "named_identity_profile_id", "named_identity_binding_id", "named_identity_config_revision", "named_identity_policy_revision"} {
			if db.Transaction(func(tx *gorm.DB) error {
				return tx.Table(target.table).Where(target.where, target.args...).Update(field, " ").Error
			}) == nil {
				t.Fatal("PAD SPACE blank accepted", target.table, field)
			}
		}
	}
	if db.Delete(&binding).Error != nil {
		t.Fatal("remove owned constraint binding")
	}
	current()
	if db.Migrator().DropConstraint(checks[0].proof, checks[0].name) != nil || db.Migrator().CreateConstraint(checks[0].old, checks[0].name) != nil {
		t.Fatal("install literal historical97 primary check")
	}
	if db.Table("sessions").Where("id = ?", sessions[0].ID).UpdateColumn("named_identity_profile_id", " ").Error != nil {
		t.Fatal("install retained malformed tuple")
	}
	remove()
	if database.MigrateThrough(ctx, db, 98) == nil {
		t.Fatal("V98 accepted retained malformed tuple")
	}
	ledger(false)
	var invalid struct {
		Profile string `gorm:"column:named_identity_profile_id"`
	}
	if db.Table("sessions").Select("named_identity_profile_id").Where("id = ?", sessions[0].ID).Take(&invalid).Error != nil || invalid.Profile != " " {
		t.Fatal("failed upgrade rewrote retained malformed bytes")
	}
	if db.Table("sessions").Where("id = ?", sessions[0].ID).UpdateColumn("named_identity_profile_id", "").Error != nil {
		t.Fatal("repair exact blank")
	}
	migrate()
	current()
	// Close the bounded V98 fixture with its exact observed ledger and the new suffix.
	beforeSuccessor := personalKeyBehaviorLedger(t, db)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("restore current V99 after historical V98", err)
	}
	final := personalKeyBehaviorLedger(t, db)
	if len(final) != 101 || final[100].Version != 101 || final[99].Version != 100 || final[98].Version != 99 || !reflect.DeepEqual(beforeSuccessor, final[:98]) {
		t.Fatal("historical V98 closure lost exact prefix or V99 suffix")
	}

}
