package handler

import (
	"context"
	"errors"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// Independent literal V102 projections restore only the owned historical test
// boundary. They never migrate a live database or define current application data.
type forcedSSOFixtureSessionV102 struct {
	NamedIdentityProviderID       string     `gorm:"column:named_identity_provider_id;size:30;not null;default:''" json:"-"`
	NamedIdentityProfileID        string     `gorm:"column:named_identity_profile_id;size:64;not null;default:''" json:"-"`
	NamedIdentityBindingID        string     `gorm:"column:named_identity_binding_id;size:30;not null;default:''" json:"-"`
	NamedIdentityBindingCreatedAt *time.Time `gorm:"column:named_identity_binding_created_at;precision:6" json:"-"`
	NamedIdentityConfigRevision   string     `gorm:"column:named_identity_config_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityPolicyRevision   string     `gorm:"column:named_identity_policy_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityUserCreatedAt    *time.Time `gorm:"column:named_identity_user_created_at;precision:6" json:"-"`

	SAMLBindingID        string     `gorm:"column:saml_binding_id;size:30;not null;default:''" json:"-"`
	SAMLBindingCreatedAt *time.Time `gorm:"column:saml_binding_created_at;precision:6" json:"-"`
	SAMLConfigRevision   string     `gorm:"column:saml_config_revision;size:64;not null;default:''" json:"-"`
	SAMLPolicyRevision   string     `gorm:"column:saml_policy_revision;size:64;not null;default:''" json:"-"`
	SAMLUserCreatedAt    *time.Time `gorm:"column:saml_user_created_at;precision:6" json:"-"`

	LDAPBindingID         string     `gorm:"column:ldap_binding_id;size:30;not null;default:''" json:"-"`
	LDAPBindingCreatedAt  *time.Time `gorm:"column:ldap_binding_created_at;precision:6" json:"-"`
	LDAPConfigRevision    string     `gorm:"column:ldap_config_revision;size:64;not null;default:''" json:"-"`
	LDAPPolicyRevision    string     `gorm:"column:ldap_policy_revision;size:64;not null;default:''" json:"-"`
	LDAPUserCreatedAt     *time.Time `gorm:"column:ldap_user_created_at;precision:6" json:"-"`
	OAuthBindingID        string     `gorm:"column:oauth_binding_id;size:30;not null;default:''" json:"-"`
	OAuthBindingCreatedAt *time.Time `gorm:"column:oauth_binding_created_at;precision:6" json:"-"`
	OAuthConfigRevision   string     `gorm:"column:oauth_config_revision;size:64;not null;default:''" json:"-"`
	OAuthPolicyRevision   string     `gorm:"column:oauth_policy_revision;size:64;not null;default:''" json:"-"`
	OAuthUserCreatedAt    *time.Time `gorm:"column:oauth_user_created_at;precision:6" json:"-"`
	PrimaryMethod         string     `gorm:"size:20;not null;default:'';check:ck_sessions_oidc_primary,(((((OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 108 AND ASCII(SUBSTRING(primary_method,2,1)) = 100 AND ASCII(SUBSTRING(primary_method,3,1)) = 97 AND ASCII(SUBSTRING(primary_method,4,1)) = 112 AND CHAR_LENGTH(ldap_binding_id) > 0 AND ldap_binding_created_at IS NOT NULL AND CHAR_LENGTH(ldap_config_revision) = 64 AND CHAR_LENGTH(ldap_policy_revision) = 64 AND ldap_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 115 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 109 AND ASCII(SUBSTRING(primary_method,4,1)) = 108 AND CHAR_LENGTH(saml_binding_id) > 0 AND saml_binding_created_at IS NOT NULL AND CHAR_LENGTH(saml_config_revision) = 64 AND CHAR_LENGTH(saml_policy_revision) = 64 AND saml_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL)) AND OCTET_LENGTH(named_identity_provider_id) = 0 AND OCTET_LENGTH(named_identity_profile_id) = 0 AND OCTET_LENGTH(named_identity_binding_id) = 0 AND named_identity_binding_created_at IS NULL AND OCTET_LENGTH(named_identity_config_revision) = 0 AND OCTET_LENGTH(named_identity_policy_revision) = 0 AND named_identity_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 6 AND ASCII(SUBSTRING(primary_method,1,1)) = 103 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 116 AND ASCII(SUBSTRING(primary_method,4,1)) = 104 AND ASCII(SUBSTRING(primary_method,5,1)) = 117 AND ASCII(SUBSTRING(primary_method,6,1)) = 98 AND OCTET_LENGTH(named_identity_provider_id) = 6 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 98 AND OCTET_LENGTH(named_identity_profile_id) = 23 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 98 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 109 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,15,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,16,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,17,1)) = 45 AND ASCII(SUBSTRING(named_identity_profile_id,18,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,19,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,20,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,21,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,22,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,23,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)) OR (OCTET_LENGTH(primary_method) = 6 AND ASCII(SUBSTRING(primary_method,1,1)) = 103 AND ASCII(SUBSTRING(primary_method,2,1)) = 111 AND ASCII(SUBSTRING(primary_method,3,1)) = 111 AND ASCII(SUBSTRING(primary_method,4,1)) = 103 AND ASCII(SUBSTRING(primary_method,5,1)) = 108 AND ASCII(SUBSTRING(primary_method,6,1)) = 101 AND OCTET_LENGTH(named_identity_provider_id) = 6 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 111 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 111 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 108 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 101 AND OCTET_LENGTH(named_identity_profile_id) = 14 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 108 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 101 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 100 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)) OR (OCTET_LENGTH(primary_method) = 7 AND ASCII(SUBSTRING(primary_method,1,1)) = 100 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 115 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND ASCII(SUBSTRING(primary_method,5,1)) = 111 AND ASCII(SUBSTRING(primary_method,6,1)) = 114 AND ASCII(SUBSTRING(primary_method,7,1)) = 100 AND OCTET_LENGTH(named_identity_provider_id) = 7 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 100 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 115 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 99 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 111 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 114 AND ASCII(SUBSTRING(named_identity_provider_id,7,1)) = 100 AND OCTET_LENGTH(named_identity_profile_id) = 17 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 100 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 115 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 114 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 100 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 50 AND ASCII(SUBSTRING(named_identity_profile_id,15,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,16,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,17,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)" json:"-"`
	OIDCBindingID         string     `gorm:"column:oidc_binding_id;size:30;not null;default:''" json:"-"`
	OIDCBindingCreatedAt  *time.Time `gorm:"column:oidc_binding_created_at;precision:6" json:"-"`
	OIDCConfigRevision    string     `gorm:"column:oidc_config_revision;size:64;not null;default:''" json:"-"`
	OIDCPolicyRevision    string     `gorm:"column:oidc_policy_revision;size:64;not null;default:''" json:"-"`
	OIDCUserCreatedAt     *time.Time `gorm:"column:oidc_user_created_at;precision:6" json:"-"`

	ID        string    `gorm:"primaryKey;size:30"`
	UserID    string    `gorm:"size:30;not null;index"`
	TokenHash string    `gorm:"size:64;not null;uniqueIndex"`
	ExpiresAt time.Time `gorm:"not null;index"`
	CreatedAt time.Time
}

func (forcedSSOFixtureSessionV102) TableName() string { return "sessions" }

type forcedSSOFixtureMFAChallengeV102 struct {
	NamedIdentityProviderID       string     `gorm:"column:named_identity_provider_id;size:30;not null;default:''" json:"-"`
	NamedIdentityProfileID        string     `gorm:"column:named_identity_profile_id;size:64;not null;default:''" json:"-"`
	NamedIdentityBindingID        string     `gorm:"column:named_identity_binding_id;size:30;not null;default:''" json:"-"`
	NamedIdentityBindingCreatedAt *time.Time `gorm:"column:named_identity_binding_created_at;precision:6" json:"-"`
	NamedIdentityConfigRevision   string     `gorm:"column:named_identity_config_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityPolicyRevision   string     `gorm:"column:named_identity_policy_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityUserCreatedAt    *time.Time `gorm:"column:named_identity_user_created_at;precision:6" json:"-"`

	SAMLBindingID        string     `gorm:"column:saml_binding_id;size:30;not null;default:''" json:"-"`
	SAMLBindingCreatedAt *time.Time `gorm:"column:saml_binding_created_at;precision:6" json:"-"`
	SAMLConfigRevision   string     `gorm:"column:saml_config_revision;size:64;not null;default:''" json:"-"`
	SAMLPolicyRevision   string     `gorm:"column:saml_policy_revision;size:64;not null;default:''" json:"-"`
	SAMLUserCreatedAt    *time.Time `gorm:"column:saml_user_created_at;precision:6" json:"-"`

	LDAPBindingID         string     `gorm:"column:ldap_binding_id;size:30;not null;default:''" json:"-"`
	LDAPBindingCreatedAt  *time.Time `gorm:"column:ldap_binding_created_at;precision:6" json:"-"`
	LDAPConfigRevision    string     `gorm:"column:ldap_config_revision;size:64;not null;default:''" json:"-"`
	LDAPPolicyRevision    string     `gorm:"column:ldap_policy_revision;size:64;not null;default:''" json:"-"`
	LDAPUserCreatedAt     *time.Time `gorm:"column:ldap_user_created_at;precision:6" json:"-"`
	OAuthBindingID        string     `gorm:"column:oauth_binding_id;size:30;not null;default:''" json:"-"`
	OAuthBindingCreatedAt *time.Time `gorm:"column:oauth_binding_created_at;precision:6" json:"-"`
	OAuthConfigRevision   string     `gorm:"column:oauth_config_revision;size:64;not null;default:''" json:"-"`
	OAuthPolicyRevision   string     `gorm:"column:oauth_policy_revision;size:64;not null;default:''" json:"-"`
	OAuthUserCreatedAt    *time.Time `gorm:"column:oauth_user_created_at;precision:6" json:"-"`
	PrimaryMethod         string     `gorm:"size:20;not null;default:'';check:ck_mfa_challenges_oidc_primary,(((((OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 108 AND ASCII(SUBSTRING(primary_method,2,1)) = 100 AND ASCII(SUBSTRING(primary_method,3,1)) = 97 AND ASCII(SUBSTRING(primary_method,4,1)) = 112 AND CHAR_LENGTH(ldap_binding_id) > 0 AND ldap_binding_created_at IS NOT NULL AND CHAR_LENGTH(ldap_config_revision) = 64 AND CHAR_LENGTH(ldap_policy_revision) = 64 AND ldap_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 115 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 109 AND ASCII(SUBSTRING(primary_method,4,1)) = 108 AND CHAR_LENGTH(saml_binding_id) > 0 AND saml_binding_created_at IS NOT NULL AND CHAR_LENGTH(saml_config_revision) = 64 AND CHAR_LENGTH(saml_policy_revision) = 64 AND saml_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL)) AND OCTET_LENGTH(named_identity_provider_id) = 0 AND OCTET_LENGTH(named_identity_profile_id) = 0 AND OCTET_LENGTH(named_identity_binding_id) = 0 AND named_identity_binding_created_at IS NULL AND OCTET_LENGTH(named_identity_config_revision) = 0 AND OCTET_LENGTH(named_identity_policy_revision) = 0 AND named_identity_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 6 AND ASCII(SUBSTRING(primary_method,1,1)) = 103 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 116 AND ASCII(SUBSTRING(primary_method,4,1)) = 104 AND ASCII(SUBSTRING(primary_method,5,1)) = 117 AND ASCII(SUBSTRING(primary_method,6,1)) = 98 AND OCTET_LENGTH(named_identity_provider_id) = 6 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 98 AND OCTET_LENGTH(named_identity_profile_id) = 23 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 98 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 109 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,15,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,16,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,17,1)) = 45 AND ASCII(SUBSTRING(named_identity_profile_id,18,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,19,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,20,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,21,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,22,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,23,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)) OR (OCTET_LENGTH(primary_method) = 6 AND ASCII(SUBSTRING(primary_method,1,1)) = 103 AND ASCII(SUBSTRING(primary_method,2,1)) = 111 AND ASCII(SUBSTRING(primary_method,3,1)) = 111 AND ASCII(SUBSTRING(primary_method,4,1)) = 103 AND ASCII(SUBSTRING(primary_method,5,1)) = 108 AND ASCII(SUBSTRING(primary_method,6,1)) = 101 AND OCTET_LENGTH(named_identity_provider_id) = 6 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 111 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 111 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 108 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 101 AND OCTET_LENGTH(named_identity_profile_id) = 14 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 108 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 101 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 100 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)) OR (OCTET_LENGTH(primary_method) = 7 AND ASCII(SUBSTRING(primary_method,1,1)) = 100 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 115 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND ASCII(SUBSTRING(primary_method,5,1)) = 111 AND ASCII(SUBSTRING(primary_method,6,1)) = 114 AND ASCII(SUBSTRING(primary_method,7,1)) = 100 AND OCTET_LENGTH(named_identity_provider_id) = 7 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 100 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 115 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 99 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 111 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 114 AND ASCII(SUBSTRING(named_identity_provider_id,7,1)) = 100 AND OCTET_LENGTH(named_identity_profile_id) = 17 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 100 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 115 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 114 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 100 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 50 AND ASCII(SUBSTRING(named_identity_profile_id,15,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,16,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,17,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)" json:"-"`
	OIDCBindingID         string     `gorm:"column:oidc_binding_id;size:30;not null;default:''" json:"-"`
	OIDCBindingCreatedAt  *time.Time `gorm:"column:oidc_binding_created_at;precision:6" json:"-"`
	OIDCConfigRevision    string     `gorm:"column:oidc_config_revision;size:64;not null;default:''" json:"-"`
	OIDCPolicyRevision    string     `gorm:"column:oidc_policy_revision;size:64;not null;default:''" json:"-"`
	OIDCUserCreatedAt     *time.Time `gorm:"column:oidc_user_created_at;precision:6" json:"-"`

	UserID         string    `gorm:"primaryKey;size:30"`
	Purpose        string    `gorm:"primaryKey;size:20"`
	TokenHash      string    `gorm:"size:64;not null;uniqueIndex"`
	PasswordDigest string    `gorm:"size:64;not null"`
	Generation     string    `gorm:"size:64;not null"`
	SessionID      string    `gorm:"size:30;not null"`
	Attempts       int       `gorm:"not null"`
	ExpiresAt      time.Time `gorm:"not null"`
}

func (forcedSSOFixtureMFAChallengeV102) TableName() string { return "mfa_challenges" }

type forcedSSOFixtureGovernanceV102 struct {
	RegistrationAllowedEmailDomains string `gorm:"size:2048;not null;default:[]" json:"-"`
	RegistrationApprovalRequired    bool   `gorm:"not null;default:false" json:"-"`
	RegistrationPolicyRevision      string `gorm:"size:64;not null;default:0000000000000000000000000000000000000000000000000000000000000000;check:ck_registration_policy_revision,CHAR_LENGTH(registration_policy_revision) = 64 AND (ASCII(SUBSTRING(registration_policy_revision,1,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,1,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,2,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,2,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,3,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,3,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,4,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,4,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,5,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,5,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,6,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,6,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,7,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,7,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,8,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,8,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,9,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,9,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,10,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,10,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,11,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,11,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,12,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,12,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,13,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,13,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,14,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,14,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,15,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,15,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,16,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,16,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,17,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,17,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,18,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,18,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,19,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,19,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,20,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,20,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,21,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,21,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,22,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,22,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,23,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,23,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,24,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,24,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,25,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,25,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,26,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,26,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,27,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,27,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,28,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,28,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,29,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,29,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,30,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,30,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,31,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,31,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,32,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,32,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,33,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,33,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,34,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,34,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,35,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,35,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,36,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,36,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,37,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,37,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,38,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,38,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,39,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,39,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,40,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,40,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,41,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,41,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,42,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,42,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,43,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,43,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,44,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,44,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,45,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,45,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,46,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,46,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,47,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,47,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,48,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,48,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,49,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,49,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,50,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,50,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,51,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,51,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,52,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,52,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,53,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,53,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,54,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,54,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,55,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,55,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,56,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,56,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,57,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,57,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,58,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,58,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,59,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,59,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,60,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,60,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,61,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,61,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,62,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,62,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,63,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,63,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,64,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,64,1)) BETWEEN 97 AND 102)" json:"-"`
	ID                              int    `gorm:"primaryKey"`
	RegistrationEnabled             bool   `gorm:"not null"`
}

func (forcedSSOFixtureGovernanceV102) TableName() string { return "governance_settings" }

type forcedSSOFixtureInstallationV100 struct {
	ProjectionVersion int `gorm:"not null;check:ck_runtime_installation_projection,projection_version = 1"`
}

func (forcedSSOFixtureInstallationV100) TableName() string {
	return "runtime_installation_observations"
}

var forcedSSOFixtureFields = []string{"EmergencyPolicyRevision", "EmergencyUserCreatedAt", "EmergencyMFAGeneration", "EmergencyPasswordDigest", "EmergencyFactorCompletedAt"}
var forcedSSOFixtureGovernanceFields = []string{"ForcedSSOEnabled", "AuthenticationPolicyRevision", "EmergencyAdminID", "EmergencyAdminCreatedAt"}

func forcedSSOPristineV103(db *gorm.DB) error {
	rows := personalKeyBehaviorLedgerForForcedSSO(db)
	if len(rows) < 1 || len(rows) > 103 {
		return errors.New("invalid authentication policy historical ledger")
	}
	for i, row := range rows {
		if row.Version != i+1 {
			return errors.New("noncontiguous authentication policy historical ledger")
		}
	}
	groups := []struct {
		model  any
		fields []string
	}{
		{&entity.GovernanceSetting{}, forcedSSOFixtureGovernanceFields},
		{&entity.Session{}, forcedSSOFixtureFields},
		{&entity.MFAChallenge{}, forcedSSOFixtureFields},
	}
	if len(rows) < 103 {
		for _, group := range groups {
			for _, name := range group.fields {
				if db.Migrator().HasColumn(group.model, name) {
					return errors.New("historical ledger retained authentication policy overlay")
				}
			}
		}
		return nil
	}
	var count int64
	if err := db.Table("governance_settings").Where("forced_sso_enabled IS NULL OR forced_sso_enabled <> ? OR authentication_policy_revision IS NULL OR OCTET_LENGTH(authentication_policy_revision) <> 64 OR authentication_policy_revision <> ? OR emergency_admin_id IS NULL OR OCTET_LENGTH(emergency_admin_id) <> 0 OR emergency_admin_created_at IS NOT NULL", false, strings.Repeat("0", 64)).Count(&count).Error; err != nil || count != 0 {
		return errors.New("cannot discard configured authentication policy")
	}
	for _, table := range []string{"sessions", "mfa_challenges"} {
		if err := db.Table(table).Where("primary_method = ? OR emergency_policy_revision IS NULL OR OCTET_LENGTH(emergency_policy_revision) <> 0 OR emergency_user_created_at IS NOT NULL OR emergency_mfa_generation IS NULL OR OCTET_LENGTH(emergency_mfa_generation) <> 0 OR emergency_password_digest IS NULL OR OCTET_LENGTH(emergency_password_digest) <> 0 OR emergency_factor_completed_at IS NOT NULL", "emergency").Count(&count).Error; err != nil || count != 0 {
			return errors.New("cannot discard emergency primary authority")
		}
	}
	if err := db.Table("runtime_installation_observations").Where("projection_version IS NULL OR projection_version <> ?", 1).Count(&count).Error; err != nil || count != 0 {
		return errors.New("cannot discard projection2 installation history")
	}
	return nil
}
func personalKeyBehaviorLedgerForForcedSSO(db *gorm.DB) []personalKeyBehaviorMigrationEntry {
	var rows []personalKeyBehaviorMigrationEntry
	if err := db.Table("schema_migrations").Order("version").Find(&rows).Error; err != nil {
		return nil
	}
	return rows
}

// A finite pristine103→102 peel for migration-only fixtures. Ordinary current
// lifecycle scenarios keep103. No configured policy/proof or v2 history is erased.
func legacyForcedSSOBeforeV102(t *testing.T, db *gorm.DB) {
	t.Helper()
	// Direct migration callers also enter through the two finite newer overlays.
	switch len(personalKeyBehaviorLedger(t, db)) {
	case 104, 105, 106, 107, 108, 109, 110, 111:
		legacyRuntimePublicationBeforeV104(t, db)
		legacySavedCertificateBeforeV103(t, db)
	}
	if err := forcedSSOPristineV103(db); err != nil {
		t.Fatal(err)
	}
	before := personalKeyBehaviorLedger(t, db)
	if len(before) < 103 {
		return
	}
	if database.MigrateThrough(db.Statement.Context, db, 102) == nil || !reflect.DeepEqual(before, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("bounded102 admitted103 or changed history")
	}
	for _, v := range []struct {
		model any
		name  string
	}{
		{&forcedSSOFixtureSessionV102{}, "ck_sessions_oidc_primary"},
		{&forcedSSOFixtureMFAChallengeV102{}, "ck_mfa_challenges_oidc_primary"},
		{&forcedSSOFixtureInstallationV100{}, "ck_runtime_installation_projection"},
	} {
		if db.Migrator().DropConstraint(v.model, v.name) != nil || db.Migrator().CreateConstraint(v.model, v.name) != nil {
			t.Fatal("restore frozen pre103 check", v.name)
		}
	}
	if db.Migrator().DropConstraint(&entity.GovernanceSetting{}, "ck_governance_authentication_policy") != nil {
		t.Fatal("remove only103 policy check")
	}
	for _, group := range []struct {
		model  any
		fields []string
	}{
		{&entity.GovernanceSetting{}, forcedSSOFixtureGovernanceFields}, {&entity.Session{}, forcedSSOFixtureFields}, {&entity.MFAChallenge{}, forcedSSOFixtureFields},
	} {
		for _, name := range group.fields {
			if db.Migrator().DropColumn(group.model, name) != nil {
				t.Fatal("remove only pristine103 field", name)
			}
		}
	}
	removed := db.Table("schema_migrations").Where("version = ?", 103).Delete(&struct{}{})
	if removed.Error != nil || removed.RowsAffected != 1 || !reflect.DeepEqual(before[:102], personalKeyBehaviorLedger(t, db)) || forcedSSOPristineV103(db) != nil {
		t.Fatal("103 peel changed retained102 receipts")
	}
}

type forcedSSOFixturePolicyWidth struct {
	EmergencyPolicyRevision string `gorm:"column:emergency_policy_revision;size:63;not null;default:''"`
}

func (forcedSSOFixturePolicyWidth) TableName() string { return "sessions" }

type forcedSSOFixtureWrongInstallationIndex struct {
	FirstObservedAt time.Time `gorm:"index:idx_runtime_installation_instance,priority:1"`
	InstanceID      string    `gorm:"index:idx_runtime_installation_instance,priority:2"`
}

func (forcedSSOFixtureWrongInstallationIndex) TableName() string {
	return "runtime_installation_observations"
}

// Root registers this one scenario separately. These are migration fixtures,
// never genuine emergency login/MFA or live runtime installation acceptance.
func testForcedSSOMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db = db.WithContext(ctx)
	original := personalKeyBehaviorLedger(t, db)
	if len(original) != 111 || original[110].Version != 111 || original[109].Version != 110 || original[108].Version != 109 || original[107].Version != 108 || original[106].Version != 107 || original[105].Version != 106 || original[104].Version != 105 || original[103].Version != 104 || original[102].Version != 103 {
		t.Fatal("migration requires exact current105")
	}
	// Bound the newer pristine overlays before the strict V103-only assertion.
	legacyRuntimePublicationBeforeV104(t, db)
	legacySavedCertificateBeforeV103(t, db)
	if err := forcedSSOPristineV103(db); err != nil {
		t.Fatal("fresh off/empty default", err)
	}
	legacyForcedSSOBeforeV102(t, db)
	prefix := personalKeyBehaviorLedger(t, db)
	if !reflect.DeepEqual(prefix, original[:102]) {
		t.Fatal("102 prefix changed")
	}
	var oldGovernance forcedSSOFixtureGovernanceV102
	if db.Session(&gorm.Session{QueryFields: true}).Take(&oldGovernance, "id = ?", 1).Error != nil {
		t.Fatal("capture historical governance facts")
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	user := entity.User{ID: "usr_00000000000000000000000002", Email: "forced-sso-migration@example.test", Name: "Retained", PasswordHash: "migration-only", Role: entity.RoleMember, CreatedAt: at, UpdatedAt: at}
	if db.Create(&user).Error != nil {
		t.Fatal("retained identity fixture")
	}
	oldSession := forcedSSOFixtureSessionV102{ID: "ses_00000000000000000000000002", UserID: user.ID, TokenHash: strings.Repeat("a", 64), CreatedAt: at, ExpiresAt: at.Add(time.Hour)}
	oldChallenge := forcedSSOFixtureMFAChallengeV102{UserID: user.ID, Purpose: "login", TokenHash: strings.Repeat("b", 64), PasswordDigest: strings.Repeat("c", 64), Generation: strings.Repeat("A", 43), ExpiresAt: at.Add(time.Minute)}
	oldObservation := entity.RuntimeInstallationObservation{ID: "rin_00000000000000000000000002", InstanceID: "ins_00000000000000000000000002", SnapshotID: "cfg_00000000000000000000000002", InstanceStartedAt: at, RoutesPublishedAt: at, FirstObservedAt: at, ProjectionVersion: 1, SourceDigest: strings.Repeat("d", 64)}
	if db.Create(&oldSession).Error != nil || db.Create(&oldChallenge).Error != nil || db.Create(&oldObservation).Error != nil {
		t.Fatal("retained local/projection1 fixtures")
	}
	if db.Session(&gorm.Session{QueryFields: true}).Take(&oldSession, "id = ?", oldSession.ID).Error != nil || db.Session(&gorm.Session{QueryFields: true}).Take(&oldChallenge, "user_id = ? AND purpose = ?", user.ID, "login").Error != nil {
		t.Fatal("capture persisted old fields")
	}
	migrate := func() {
		t.Helper()
		if database.MigrateThrough(ctx, db, 103) != nil {
			t.Fatal("V103 migration failed")
		}
	}
	retained := func() {
		t.Helper()
		rows := personalKeyBehaviorLedger(t, db)
		if len(rows) != 103 || rows[102].Version != 103 || !reflect.DeepEqual(rows[:102], prefix) {
			t.Fatal("V103 changed old receipts")
		}
		var session forcedSSOFixtureSessionV102
		var challenge forcedSSOFixtureMFAChallengeV102
		var observation entity.RuntimeInstallationObservation
		var governance forcedSSOFixtureGovernanceV102
		if db.Session(&gorm.Session{QueryFields: true}).Take(&governance, "id = ?", 1).Error != nil || !reflect.DeepEqual(governance, oldGovernance) {
			t.Fatal("retained governance facts changed")
		}
		if db.Session(&gorm.Session{QueryFields: true}).Take(&session, "id = ?", oldSession.ID).Error != nil || !reflect.DeepEqual(session, oldSession) || db.Session(&gorm.Session{QueryFields: true}).Take(&challenge, "user_id = ? AND purpose = ?", user.ID, "login").Error != nil || !reflect.DeepEqual(challenge, oldChallenge) || db.Take(&observation, "id = ?", oldObservation.ID).Error != nil || observation.ID != oldObservation.ID || observation.InstanceID != oldObservation.InstanceID || !observation.InstanceStartedAt.Equal(oldObservation.InstanceStartedAt) || observation.SnapshotID != oldObservation.SnapshotID || !observation.RoutesPublishedAt.Equal(oldObservation.RoutesPublishedAt) || !observation.FirstObservedAt.Equal(oldObservation.FirstObservedAt) || observation.ProjectionVersion != 1 || observation.SourceDigest != oldObservation.SourceDigest {
			t.Fatal("retained local/projection1 facts changed")
		}
	}
	migrate()
	retained()
	complete := personalKeyBehaviorLedger(t, db)
	migrate()
	retained()
	if !reflect.DeepEqual(complete, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("repeat rewrote103 receipt")
	}
	if database.MigrateThrough(ctx, db, 112) == nil || !reflect.DeepEqual(complete, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("unsupported112 admitted or rewrote history")
	}
	remove103 := func() {
		t.Helper()
		r := db.Table("schema_migrations").Where("version = ?", 103).Delete(&struct{}{})
		if r.Error != nil || r.RowsAffected != 1 {
			t.Fatal("remove only103 receipt")
		}
	}
	remove103()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- database.MigrateThrough(ctx, db, 103) })
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal("concurrent103 startup")
		}
	}
	retained()
	remove103()
	if db.Migrator().DropConstraint(&entity.Session{}, "ck_sessions_oidc_primary") != nil || db.Migrator().DropColumn(&entity.Session{}, "EmergencyFactorCompletedAt") != nil {
		t.Fatal("partial additive DDL setup")
	}
	migrate()
	retained()
	remove103()
	if db.Migrator().DropConstraint(&entity.Session{}, "ck_sessions_oidc_primary") != nil || db.Migrator().AlterColumn(&forcedSSOFixturePolicyWidth{}, "EmergencyPolicyRevision") != nil {
		t.Fatal("wrong width setup")
	}
	if database.MigrateThrough(ctx, db, 103) == nil || len(personalKeyBehaviorLedger(t, db)) != 102 {
		t.Fatal("incompatible metadata admitted103")
	}
	if db.Migrator().AlterColumn(&entity.Session{}, "EmergencyPolicyRevision") != nil {
		t.Fatal("restore exact V103 field")
	}
	migrate()
	retained()
	remove103()
	if database.DropIndex(db, &entity.RuntimeInstallationObservation{}, "idx_runtime_installation_instance") != nil || db.Migrator().CreateIndex(&forcedSSOFixtureWrongInstallationIndex{}, "idx_runtime_installation_instance") != nil {
		t.Fatal("wrong ordered index setup")
	}
	if database.MigrateThrough(ctx, db, 103) == nil || len(personalKeyBehaviorLedger(t, db)) != 102 {
		t.Fatal("wrong ordered index admitted103")
	}
	if database.DropIndex(db, &forcedSSOFixtureWrongInstallationIndex{}, "idx_runtime_installation_instance") != nil || db.Migrator().CreateIndex(&entity.RuntimeInstallationObservation{}, "idx_runtime_installation_instance") != nil {
		t.Fatal("restore exact ordered index")
	}
	migrate()
	retained()

	// Every denial has an accepted write in the same rollback-only transaction.
	sentinel := errors.New("rollback exact primary controls")
	positive := entity.Session{ID: "ses_00000000000000000000000003", UserID: user.ID, TokenHash: strings.Repeat("e", 64), PrimaryMethod: "emergency", CreatedAt: at, ExpiresAt: at.Add(time.Hour), EmergencyPolicyRevision: strings.Repeat("f", 64), EmergencyUserCreatedAt: &at, EmergencyMFAGeneration: strings.Repeat("A", 43), EmergencyPasswordDigest: strings.Repeat("c", 64), EmergencyFactorCompletedAt: &at}
	for _, updates := range []map[string]any{
		{"created_at": nil}, {"primary_method": "emergency "}, {"emergency_mfa_generation": strings.Repeat("a", 64)}, {"emergency_policy_revision": " "}, {"emergency_factor_completed_at": nil}, {"emergency_factor_completed_at": at.Add(time.Microsecond)}, {"oidc_binding_id": "obn_contamination"}, {"primary_method": "local"},
	} {
		err := db.Transaction(func(tx *gorm.DB) error {
			if tx.Create(&positive).Error != nil {
				return errors.New("positive emergency tuple rejected")
			}
			if tx.Model(&entity.Session{}).Where("id = ?", positive.ID).Updates(updates).Error == nil {
				return errors.New("invalid emergency tuple admitted")
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatal("emergency tuple control")
		}
	}

	challenge := entity.MFAChallenge{UserID: user.ID, Purpose: "emergency", TokenHash: strings.Repeat("f", 64), PasswordDigest: strings.Repeat("c", 64), Generation: strings.Repeat("A", 43), ExpiresAt: at.Add(time.Minute), PrimaryMethod: "emergency", EmergencyPolicyRevision: strings.Repeat("e", 64), EmergencyUserCreatedAt: &at, EmergencyMFAGeneration: strings.Repeat("A", 43), EmergencyPasswordDigest: strings.Repeat("c", 64)}
	for _, updates := range []map[string]any{
		{"purpose": "login"}, {"generation": strings.Repeat("B", 43)}, {"password_digest": strings.Repeat("d", 64)}, {"emergency_factor_completed_at": at}, {"session_id": oldSession.ID}, {"primary_method": "local"}, {"emergency_user_created_at": nil},
	} {
		err := db.Transaction(func(tx *gorm.DB) error {
			if tx.Create(&challenge).Error != nil {
				return errors.New("positive emergency challenge rejected")
			}
			if tx.Model(&entity.MFAChallenge{}).Where("user_id = ? AND purpose = ?", user.ID, "emergency").Updates(updates).Error == nil {
				return errors.New("invalid emergency challenge admitted")
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatal("emergency challenge constraint control")
		}
	}
	// An accepted ordinary native login challenge cannot acquire emergency
	// purpose without full proof. Deliberate rollback preserves every retained fact.
	if err := db.Transaction(func(tx *gorm.DB) error {
		accepted := tx.Model(&entity.MFAChallenge{}).Where("user_id = ? AND purpose = ?", user.ID, "login").Update("token_hash", strings.Repeat("e", 64))
		if accepted.Error != nil || accepted.RowsAffected != 1 {
			return errors.New("ordinary challenge positive write rejected")
		}
		if tx.Model(&entity.MFAChallenge{}).Where("user_id = ? AND purpose = ?", user.ID, "login").Update("purpose", "emergency").Error == nil {
			return errors.New("ordinary challenge admitted emergency purpose without proof")
		}
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatal("ordinary/emergency challenge separation control")
	}
	for _, updates := range []map[string]any{
		{"emergency_admin_id": " "}, {"emergency_admin_created_at": at}, {"authentication_policy_revision": " "},
	} {
		err := db.Transaction(func(tx *gorm.DB) error {
			r := tx.Table("governance_settings").Where("id = ?", 1).Updates(map[string]any{"forced_sso_enabled": false, "authentication_policy_revision": strings.Repeat("e", 64), "emergency_admin_id": "", "emergency_admin_created_at": nil})
			if r.Error != nil || r.RowsAffected != 1 {
				return errors.New("off policy positive rejected")
			}
			if tx.Table("governance_settings").Where("id = ?", 1).Updates(updates).Error == nil {
				return errors.New("invalid off policy admitted")
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatal("off-policy blank/null constraint control")
		}
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		r := tx.Table("governance_settings").Where("id = ?", 1).Updates(map[string]any{"forced_sso_enabled": true, "authentication_policy_revision": strings.Repeat("e", 64), "emergency_admin_id": user.ID, "emergency_admin_created_at": at, "registration_enabled": false})
		if r.Error != nil || r.RowsAffected != 1 {
			return errors.New("enabled policy positive rejected")
		}
		if tx.Table("governance_settings").Where("id = ?", 1).Update("registration_enabled", true).Error == nil {
			return errors.New("enforced open registration admitted")
		}
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatal("enabled-policy tuple control")
	}
	// The migration-only peel rejects genuine retained configuration/proof/history;
	// accepted writes below are rollback-only schema facts, never login authority.
	for _, kind := range []string{"policy", "primary", "projection2"} {
		if err := forcedSSOPristineV103(db); err != nil {
			t.Fatal("pristine positive prerequisite", err)
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			switch kind {
			case "policy":
				if err := tx.Table("governance_settings").Where("id = ?", 1).Update("authentication_policy_revision", strings.Repeat("e", 64)).Error; err != nil {
					return err
				}
			case "primary":
				if err := tx.Create(&positive).Error; err != nil {
					return err
				}
			case "projection2":
				v := oldObservation
				v.ID = "rin_00000000000000000000000004"
				v.SourceDigest = strings.Repeat("f", 64)
				v.ProjectionVersion = 2
				if err := tx.Create(&v).Error; err != nil {
					return err
				}
			}
			if forcedSSOPristineV103(tx) == nil {
				return errors.New("dirty V103 peel admitted")
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) || forcedSSOPristineV103(db) != nil {
			t.Fatal("dirty V103 peel control", kind)
		}
	}
	// Projection2 is admitted separately without changing stored projection1.
	probe := oldObservation
	probe.ID = "rin_00000000000000000000000003"
	probe.SourceDigest = strings.Repeat("e", 64)
	probe.ProjectionVersion = 2
	if err := db.Transaction(func(tx *gorm.DB) error {
		if tx.Create(&probe).Error != nil {
			return errors.New("projection2 rejected")
		}
		if tx.Model(&entity.RuntimeInstallationObservation{}).Where("id = ?", probe.ID).Update("projection_version", 3).Error == nil {
			return errors.New("future projection admitted")
		}
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatal("projection version control")
	}

	// A nonempty partially removed emergency proof must not acquire a defaulted
	// factor on resume. Only this owned corrupt fixture is removed after denial.
	remove103()
	if db.Migrator().DropConstraint(&entity.Session{}, "ck_sessions_oidc_primary") != nil || db.Create(&positive).Error != nil || db.Migrator().DropColumn(&entity.Session{}, "EmergencyFactorCompletedAt") != nil {
		t.Fatal("dirty partial primary setup")
	}
	if database.MigrateThrough(ctx, db, 103) == nil || len(personalKeyBehaviorLedger(t, db)) != 102 {
		t.Fatal("nonpristine partial emergency proof resumed")
	}
	if db.Migrator().AddColumn(&entity.Session{}, "EmergencyFactorCompletedAt") != nil {
		t.Fatal("restore missing physical factor")
	}
	removed := db.Table("sessions").Where("id = ?", positive.ID).Delete(&struct{}{})
	if removed.Error != nil || removed.RowsAffected != 1 {
		t.Fatal("remove only owned corrupt emergency fixture")
	}
	migrate()
	retained()
	retained()

	// Leave the shared harness at the current head after bounded V103 controls.
	beforeCurrent := personalKeyBehaviorLedger(t, db)
	if database.Migrate(ctx, db) != nil {
		t.Fatal("restore current105 after bounded forced SSO scenario")
	}
	current := personalKeyBehaviorLedger(t, db)
	if len(current) != 111 || current[110].Version != 111 || current[109].Version != 110 || current[108].Version != 109 || current[107].Version != 108 || current[106].Version != 107 || current[105].Version != 106 || current[104].Version != 105 || current[103].Version != 104 || !reflect.DeepEqual(beforeCurrent, current[:103]) {
		t.Fatal("current105 restoration changed bounded V103 receipts")
	}
}
