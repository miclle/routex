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

// Private frozen projections are test-only; no evolving entity defines V96 DDL.
type ldapFixtureProviderV96 struct {
	ID                       string     `gorm:"primaryKey;size:30;check:ck_ldap_singleton,OCTET_LENGTH(id) = 4 AND ASCII(SUBSTRING(id,1,1)) = 108 AND ASCII(SUBSTRING(id,2,1)) = 100 AND ASCII(SUBSTRING(id,3,1)) = 97 AND ASCII(SUBSTRING(id,4,1)) = 112"`
	ReviewRevision           string     `gorm:"size:64;not null"`
	ConfigRevision           string     `gorm:"size:64;not null"`
	PolicyRevision           string     `gorm:"size:64;not null"`
	Name                     string     `gorm:"size:100;not null"`
	Endpoint                 string     `gorm:"size:2048;not null"`
	BindDN                   string     `gorm:"size:2048;not null"`
	BaseDN                   string     `gorm:"size:2048;not null"`
	UserFilter               string     `gorm:"size:4096;not null"`
	IdentityAttribute        string     `gorm:"size:20;not null;check:ck_ldap_provider_attribute,(OCTET_LENGTH(identity_attribute) = 0 AND enabled = FALSE AND OCTET_LENGTH(auth_ciphertext) = 0 AND config_revision = '0000000000000000000000000000000000000000000000000000000000000000') OR (OCTET_LENGTH(identity_attribute) = 9 AND ASCII(SUBSTRING(identity_attribute,1,1)) = 101 AND ASCII(SUBSTRING(identity_attribute,2,1)) = 110 AND ASCII(SUBSTRING(identity_attribute,3,1)) = 116 AND ASCII(SUBSTRING(identity_attribute,4,1)) = 114 AND ASCII(SUBSTRING(identity_attribute,5,1)) = 121 AND ASCII(SUBSTRING(identity_attribute,6,1)) = 85 AND ASCII(SUBSTRING(identity_attribute,7,1)) = 85 AND ASCII(SUBSTRING(identity_attribute,8,1)) = 73 AND ASCII(SUBSTRING(identity_attribute,9,1)) = 68) OR (OCTET_LENGTH(identity_attribute) = 10 AND ASCII(SUBSTRING(identity_attribute,1,1)) = 111 AND ASCII(SUBSTRING(identity_attribute,2,1)) = 98 AND ASCII(SUBSTRING(identity_attribute,3,1)) = 106 AND ASCII(SUBSTRING(identity_attribute,4,1)) = 101 AND ASCII(SUBSTRING(identity_attribute,5,1)) = 99 AND ASCII(SUBSTRING(identity_attribute,6,1)) = 116 AND ASCII(SUBSTRING(identity_attribute,7,1)) = 71 AND ASCII(SUBSTRING(identity_attribute,8,1)) = 85 AND ASCII(SUBSTRING(identity_attribute,9,1)) = 73 AND ASCII(SUBSTRING(identity_attribute,10,1)) = 68)"`
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

func (ldapFixtureProviderV96) TableName() string { return "ldap_providers" }

type ldapFixtureBindingV96 struct {
	ProviderID        string    `gorm:"size:30;not null;check:ck_ldap_binding_provider,OCTET_LENGTH(provider_id) = 4 AND ASCII(SUBSTRING(provider_id,1,1)) = 108 AND ASCII(SUBSTRING(provider_id,2,1)) = 100 AND ASCII(SUBSTRING(provider_id,3,1)) = 97 AND ASCII(SUBSTRING(provider_id,4,1)) = 112"`
	IdentityAttribute string    `gorm:"size:20;not null;check:ck_ldap_binding_attribute,(OCTET_LENGTH(identity_attribute) = 9 AND ASCII(SUBSTRING(identity_attribute,1,1)) = 101 AND ASCII(SUBSTRING(identity_attribute,2,1)) = 110 AND ASCII(SUBSTRING(identity_attribute,3,1)) = 116 AND ASCII(SUBSTRING(identity_attribute,4,1)) = 114 AND ASCII(SUBSTRING(identity_attribute,5,1)) = 121 AND ASCII(SUBSTRING(identity_attribute,6,1)) = 85 AND ASCII(SUBSTRING(identity_attribute,7,1)) = 85 AND ASCII(SUBSTRING(identity_attribute,8,1)) = 73 AND ASCII(SUBSTRING(identity_attribute,9,1)) = 68) OR (OCTET_LENGTH(identity_attribute) = 10 AND ASCII(SUBSTRING(identity_attribute,1,1)) = 111 AND ASCII(SUBSTRING(identity_attribute,2,1)) = 98 AND ASCII(SUBSTRING(identity_attribute,3,1)) = 106 AND ASCII(SUBSTRING(identity_attribute,4,1)) = 101 AND ASCII(SUBSTRING(identity_attribute,5,1)) = 99 AND ASCII(SUBSTRING(identity_attribute,6,1)) = 116 AND ASCII(SUBSTRING(identity_attribute,7,1)) = 71 AND ASCII(SUBSTRING(identity_attribute,8,1)) = 85 AND ASCII(SUBSTRING(identity_attribute,9,1)) = 73 AND ASCII(SUBSTRING(identity_attribute,10,1)) = 68)"`
	ID                string    `gorm:"primaryKey;size:30"`
	UserID            string    `gorm:"size:30;not null;uniqueIndex"`
	UserCreatedAt     time.Time `gorm:"precision:6;not null"`
	ConfigRevision    string    `gorm:"size:64;not null"`
	Subject           string    `gorm:"size:64;not null"`
	SubjectDigest     string    `gorm:"size:64;not null;uniqueIndex"`
	CreatedAt         time.Time `gorm:"precision:6;not null"`
}

func (ldapFixtureBindingV96) TableName() string { return "ldap_bindings" }

type ldapFixtureSessionV96 struct {
	LDAPBindingID         string     `gorm:"column:ldap_binding_id;size:30;not null;default:''"`
	LDAPBindingCreatedAt  *time.Time `gorm:"column:ldap_binding_created_at;precision:6"`
	LDAPConfigRevision    string     `gorm:"column:ldap_config_revision;size:64;not null;default:''"`
	LDAPPolicyRevision    string     `gorm:"column:ldap_policy_revision;size:64;not null;default:''"`
	LDAPUserCreatedAt     *time.Time `gorm:"column:ldap_user_created_at;precision:6"`
	OAuthBindingID        string     `gorm:"column:oauth_binding_id;size:30;not null;default:''"`
	OAuthBindingCreatedAt *time.Time `gorm:"column:oauth_binding_created_at;precision:6"`
	OAuthConfigRevision   string     `gorm:"column:oauth_config_revision;size:64;not null;default:''"`
	OAuthPolicyRevision   string     `gorm:"column:oauth_policy_revision;size:64;not null;default:''"`
	OAuthUserCreatedAt    *time.Time `gorm:"column:oauth_user_created_at;precision:6"`
	PrimaryMethod         string     `gorm:"size:20;not null;default:''"`
	OIDCBindingID         string     `gorm:"column:oidc_binding_id;size:30;not null;default:''"`
	OIDCBindingCreatedAt  *time.Time `gorm:"column:oidc_binding_created_at;precision:6"`
	OIDCConfigRevision    string     `gorm:"column:oidc_config_revision;size:64;not null;default:''"`
	OIDCPolicyRevision    string     `gorm:"column:oidc_policy_revision;size:64;not null;default:''"`
	OIDCUserCreatedAt     *time.Time `gorm:"column:oidc_user_created_at;precision:6"`
}

func (ldapFixtureSessionV96) TableName() string { return "sessions" }

type ldapFixtureMFAChallengeV96 struct {
	LDAPBindingID         string     `gorm:"column:ldap_binding_id;size:30;not null;default:''"`
	LDAPBindingCreatedAt  *time.Time `gorm:"column:ldap_binding_created_at;precision:6"`
	LDAPConfigRevision    string     `gorm:"column:ldap_config_revision;size:64;not null;default:''"`
	LDAPPolicyRevision    string     `gorm:"column:ldap_policy_revision;size:64;not null;default:''"`
	LDAPUserCreatedAt     *time.Time `gorm:"column:ldap_user_created_at;precision:6"`
	OAuthBindingID        string     `gorm:"column:oauth_binding_id;size:30;not null;default:''"`
	OAuthBindingCreatedAt *time.Time `gorm:"column:oauth_binding_created_at;precision:6"`
	OAuthConfigRevision   string     `gorm:"column:oauth_config_revision;size:64;not null;default:''"`
	OAuthPolicyRevision   string     `gorm:"column:oauth_policy_revision;size:64;not null;default:''"`
	OAuthUserCreatedAt    *time.Time `gorm:"column:oauth_user_created_at;precision:6"`
	PrimaryMethod         string     `gorm:"size:20;not null;default:''"`
	OIDCBindingID         string     `gorm:"column:oidc_binding_id;size:30;not null;default:''"`
	OIDCBindingCreatedAt  *time.Time `gorm:"column:oidc_binding_created_at;precision:6"`
	OIDCConfigRevision    string     `gorm:"column:oidc_config_revision;size:64;not null;default:''"`
	OIDCPolicyRevision    string     `gorm:"column:oidc_policy_revision;size:64;not null;default:''"`
	OIDCUserCreatedAt     *time.Time `gorm:"column:oidc_user_created_at;precision:6"`
}

func (ldapFixtureMFAChallengeV96) TableName() string { return "mfa_challenges" }

type ldapFixtureSessionProofV96 struct {
	PrimaryMethod string `gorm:"check:ck_sessions_oidc_primary,(OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 108 AND ASCII(SUBSTRING(primary_method,2,1)) = 100 AND ASCII(SUBSTRING(primary_method,3,1)) = 97 AND ASCII(SUBSTRING(primary_method,4,1)) = 112 AND CHAR_LENGTH(ldap_binding_id) > 0 AND ldap_binding_created_at IS NOT NULL AND CHAR_LENGTH(ldap_config_revision) = 64 AND CHAR_LENGTH(ldap_policy_revision) = 64 AND ldap_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL)"`
}

func (ldapFixtureSessionProofV96) TableName() string { return "sessions" }

type ldapFixtureMFAProofV96 struct {
	PrimaryMethod string `gorm:"check:ck_mfa_challenges_oidc_primary,(OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 108 AND ASCII(SUBSTRING(primary_method,2,1)) = 100 AND ASCII(SUBSTRING(primary_method,3,1)) = 97 AND ASCII(SUBSTRING(primary_method,4,1)) = 112 AND CHAR_LENGTH(ldap_binding_id) > 0 AND ldap_binding_created_at IS NOT NULL AND CHAR_LENGTH(ldap_config_revision) = 64 AND CHAR_LENGTH(ldap_policy_revision) = 64 AND ldap_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL)"`
}

func (ldapFixtureMFAProofV96) TableName() string { return "mfa_challenges" }

type ldapFixtureRootJobV96 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5"`
	Domain           int `gorm:"not null;check:ck_secret_rotation_domain,(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7) OR (inventory_version = 3 AND domain >= 0 AND domain <= 8) OR (inventory_version = 4 AND domain >= 0 AND domain <= 9) OR (inventory_version = 5 AND domain >= 0 AND domain <= 10)"`
}

func (ldapFixtureRootJobV96) TableName() string { return "secret_rotation_jobs" }

type ldapFixtureRootItemV96 struct {
	Domain string `gorm:"primaryKey;size:32;check:ck_secret_item_domain,((((CHAR_LENGTH(domain) = 20 AND ASCII(SUBSTRING(domain,1,1)) = 112 AND ASCII(SUBSTRING(domain,2,1)) = 114 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 118 AND ASCII(SUBSTRING(domain,5,1)) = 105 AND ASCII(SUBSTRING(domain,6,1)) = 100 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 95 AND ASCII(SUBSTRING(domain,10,1)) = 99 AND ASCII(SUBSTRING(domain,11,1)) = 114 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 100 AND ASCII(SUBSTRING(domain,14,1)) = 101 AND ASCII(SUBSTRING(domain,15,1)) = 110 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 105 AND ASCII(SUBSTRING(domain,18,1)) = 97 AND ASCII(SUBSTRING(domain,19,1)) = 108 AND ASCII(SUBSTRING(domain,20,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 101 AND ASCII(SUBSTRING(domain,2,1)) = 103 AND ASCII(SUBSTRING(domain,3,1)) = 114 AND ASCII(SUBSTRING(domain,4,1)) = 101 AND ASCII(SUBSTRING(domain,5,1)) = 115 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 115) OR (CHAR_LENGTH(domain) = 13 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 109 AND ASCII(SUBSTRING(domain,3,1)) = 116 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 116 AND ASCII(SUBSTRING(domain,9,1)) = 116 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 110 AND ASCII(SUBSTRING(domain,12,1)) = 103 AND ASCII(SUBSTRING(domain,13,1)) = 115) OR (CHAR_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 116 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 97 AND ASCII(SUBSTRING(domain,6,1)) = 103 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 95 AND ASCII(SUBSTRING(domain,9,1)) = 114 AND ASCII(SUBSTRING(domain,10,1)) = 101 AND ASCII(SUBSTRING(domain,11,1)) = 118 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 115 AND ASCII(SUBSTRING(domain,14,1)) = 105 AND ASCII(SUBSTRING(domain,15,1)) = 111 AND ASCII(SUBSTRING(domain,16,1)) = 110 AND ASCII(SUBSTRING(domain,17,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 117 AND ASCII(SUBSTRING(domain,2,1)) = 115 AND ASCII(SUBSTRING(domain,3,1)) = 101 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 109 AND ASCII(SUBSTRING(domain,7,1)) = 102 AND ASCII(SUBSTRING(domain,8,1)) = 97)) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 119 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 105 AND ASCII(SUBSTRING(domain,10,1)) = 116 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 101 AND ASCII(SUBSTRING(domain,9,1)) = 97 AND ASCII(SUBSTRING(domain,10,1)) = 100 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104))) OR (OCTET_LENGTH(domain) = 14 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 105 AND ASCII(SUBSTRING(domain,3,1)) = 100 AND ASCII(SUBSTRING(domain,4,1)) = 99 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 112 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 111 AND ASCII(SUBSTRING(domain,9,1)) = 118 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 100 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 114 AND ASCII(SUBSTRING(domain,14,1)) = 115) OR (OCTET_LENGTH(domain) = 15 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 116 AND ASCII(SUBSTRING(domain,5,1)) = 104 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 112 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 111 AND ASCII(SUBSTRING(domain,10,1)) = 118 AND ASCII(SUBSTRING(domain,11,1)) = 105 AND ASCII(SUBSTRING(domain,12,1)) = 100 AND ASCII(SUBSTRING(domain,13,1)) = 101 AND ASCII(SUBSTRING(domain,14,1)) = 114 AND ASCII(SUBSTRING(domain,15,1)) = 115) OR (OCTET_LENGTH(domain) = 14 AND ASCII(SUBSTRING(domain,1,1)) = 108 AND ASCII(SUBSTRING(domain,2,1)) = 100 AND ASCII(SUBSTRING(domain,3,1)) = 97 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 112 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 111 AND ASCII(SUBSTRING(domain,9,1)) = 118 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 100 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 114 AND ASCII(SUBSTRING(domain,14,1)) = 115)"`
}

func (ldapFixtureRootItemV96) TableName() string { return "secret_rotation_items" }

type ldapFixtureRootProcessV96 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_process_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5"`
}

func (ldapFixtureRootProcessV96) TableName() string { return "secret_process_verifications" }

// ldapMigration is the sole V96 superseding step. The migration runner skips
// every recorded released step; no historical CHECK or ledger row is rewritten.
type ldapHistoricalRootJobV95 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4"`
	Domain           int `gorm:"not null;check:ck_secret_rotation_domain,(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7) OR (inventory_version = 3 AND domain >= 0 AND domain <= 8) OR (inventory_version = 4 AND domain >= 0 AND domain <= 9)"`
}

func (ldapHistoricalRootJobV95) TableName() string { return "secret_rotation_jobs" }

type ldapHistoricalRootItemV95 struct {
	Domain string `gorm:"primaryKey;size:32;check:ck_secret_item_domain,((((CHAR_LENGTH(domain) = 20 AND ASCII(SUBSTRING(domain,1,1)) = 112 AND ASCII(SUBSTRING(domain,2,1)) = 114 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 118 AND ASCII(SUBSTRING(domain,5,1)) = 105 AND ASCII(SUBSTRING(domain,6,1)) = 100 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 95 AND ASCII(SUBSTRING(domain,10,1)) = 99 AND ASCII(SUBSTRING(domain,11,1)) = 114 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 100 AND ASCII(SUBSTRING(domain,14,1)) = 101 AND ASCII(SUBSTRING(domain,15,1)) = 110 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 105 AND ASCII(SUBSTRING(domain,18,1)) = 97 AND ASCII(SUBSTRING(domain,19,1)) = 108 AND ASCII(SUBSTRING(domain,20,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 101 AND ASCII(SUBSTRING(domain,2,1)) = 103 AND ASCII(SUBSTRING(domain,3,1)) = 114 AND ASCII(SUBSTRING(domain,4,1)) = 101 AND ASCII(SUBSTRING(domain,5,1)) = 115 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 115) OR (CHAR_LENGTH(domain) = 13 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 109 AND ASCII(SUBSTRING(domain,3,1)) = 116 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 116 AND ASCII(SUBSTRING(domain,9,1)) = 116 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 110 AND ASCII(SUBSTRING(domain,12,1)) = 103 AND ASCII(SUBSTRING(domain,13,1)) = 115) OR (CHAR_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 116 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 97 AND ASCII(SUBSTRING(domain,6,1)) = 103 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 95 AND ASCII(SUBSTRING(domain,9,1)) = 114 AND ASCII(SUBSTRING(domain,10,1)) = 101 AND ASCII(SUBSTRING(domain,11,1)) = 118 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 115 AND ASCII(SUBSTRING(domain,14,1)) = 105 AND ASCII(SUBSTRING(domain,15,1)) = 111 AND ASCII(SUBSTRING(domain,16,1)) = 110 AND ASCII(SUBSTRING(domain,17,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 117 AND ASCII(SUBSTRING(domain,2,1)) = 115 AND ASCII(SUBSTRING(domain,3,1)) = 101 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 109 AND ASCII(SUBSTRING(domain,7,1)) = 102 AND ASCII(SUBSTRING(domain,8,1)) = 97)) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 119 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 105 AND ASCII(SUBSTRING(domain,10,1)) = 116 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 101 AND ASCII(SUBSTRING(domain,9,1)) = 97 AND ASCII(SUBSTRING(domain,10,1)) = 100 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104))) OR (OCTET_LENGTH(domain) = 14 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 105 AND ASCII(SUBSTRING(domain,3,1)) = 100 AND ASCII(SUBSTRING(domain,4,1)) = 99 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 112 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 111 AND ASCII(SUBSTRING(domain,9,1)) = 118 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 100 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 114 AND ASCII(SUBSTRING(domain,14,1)) = 115) OR (OCTET_LENGTH(domain) = 15 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 116 AND ASCII(SUBSTRING(domain,5,1)) = 104 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 112 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 111 AND ASCII(SUBSTRING(domain,10,1)) = 118 AND ASCII(SUBSTRING(domain,11,1)) = 105 AND ASCII(SUBSTRING(domain,12,1)) = 100 AND ASCII(SUBSTRING(domain,13,1)) = 101 AND ASCII(SUBSTRING(domain,14,1)) = 114 AND ASCII(SUBSTRING(domain,15,1)) = 115)"`
}

func (ldapHistoricalRootItemV95) TableName() string { return "secret_rotation_items" }

type ldapHistoricalRootProcessV95 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_process_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4"`
}

func (ldapHistoricalRootProcessV95) TableName() string { return "secret_process_verifications" }

// oauthMigration is append-only V95. Released V94 and its local/OIDC
// constraints stay immutable; the ledger makes this the sole superseding step.

type ldapWrongSubjectWidth struct {
	Subject string `gorm:"size:63;not null"`
}

func (ldapWrongSubjectWidth) TableName() string { return "ldap_bindings" }

type ldapWrongBirthType struct {
	UserCreatedAt string `gorm:"size:40;not null"`
}

func (ldapWrongBirthType) TableName() string { return "ldap_bindings" }

type ldapWrongBirthPrecision struct {
	UserCreatedAt time.Time `gorm:"precision:3;not null"`
}

func (ldapWrongBirthPrecision) TableName() string { return "ldap_bindings" }

type ldapWrongRequiredNull struct {
	Subject *string `gorm:"size:64"`
}

func (ldapWrongRequiredNull) TableName() string { return "ldap_bindings" }

type ldapWrongBirthDefault struct {
	UserCreatedAt time.Time `gorm:"precision:6;not null;default:CURRENT_TIMESTAMP(6)"`
}

func (ldapWrongBirthDefault) TableName() string { return "ldap_bindings" }

type ldapWrongOwnerNonunique struct {
	UserID string `gorm:"index:idx_ldap_bindings_user_id"`
}

func (ldapWrongOwnerNonunique) TableName() string { return "ldap_bindings" }

type ldapWrongOwnerColumn struct {
	SubjectDigest string `gorm:"uniqueIndex:idx_ldap_bindings_user_id"`
}

func (ldapWrongOwnerColumn) TableName() string { return "ldap_bindings" }

type ldapWrongOwnerExtra struct {
	UserID         string `gorm:"uniqueIndex:idx_ldap_bindings_user_id,priority:1"`
	ConfigRevision string `gorm:"uniqueIndex:idx_ldap_bindings_user_id,priority:2"`
}

func (ldapWrongOwnerExtra) TableName() string { return "ldap_bindings" }
func testLDAPMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	db = db.WithContext(ctx)
	legacyMigrationBeforeV97(t, db)
	original := personalKeyBehaviorLedger(t, db)
	if len(original) != 96 {
		t.Fatal("exact V96 ledger required")
	}
	for i, row := range original {
		if row.Version != i+1 {
			t.Fatal("released prefix")
		}
	}
	ledger := func(present bool) {
		t.Helper()
		rows := personalKeyBehaviorLedger(t, db)
		n := 95
		if present {
			n = 96
		}
		if len(rows) != n || !reflect.DeepEqual(rows[:95], original[:95]) {
			t.Fatal("released V1–V95 changed")
		}
	}
	remove := func() {
		t.Helper()
		r := db.Table("schema_migrations").Where("version = ?", 96).Delete(&struct{}{})
		if r.Error != nil || r.RowsAffected != 1 {
			t.Fatal("remove exact V96")
		}
		ledger(false)
	}
	migrate := func() {
		t.Helper()
		if e := database.MigrateThrough(ctx, db, 96); e != nil {
			t.Fatal("restore V96", e)
		}
		ledger(true)
	}
	readProvider := func() ldapFixtureProviderV96 {
		t.Helper()
		var p ldapFixtureProviderV96
		if db.Session(&gorm.Session{QueryFields: true}).Take(&p, "id = ?", "ldap").Error != nil {
			t.Fatal("singleton")
		}
		return p
	}
	assertDefault := func() {
		t.Helper()
		for table, want := range map[string]int64{"ldap_providers": 1, "ldap_bindings": 0} {
			var n int64
			if db.Table(table).Count(&n).Error != nil || n != want {
				t.Fatal("empty LDAP state", table)
			}
		}
		p := readProvider()
		if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
			t.Fatal("singleton birth")
		}
		p.CreatedAt, p.UpdatedAt = time.Time{}, time.Time{}
		want := ldapFixtureProviderV96{ID: "ldap", ReviewRevision: strings.Repeat("0", 64), ConfigRevision: strings.Repeat("0", 64), PolicyRevision: strings.Repeat("0", 64), SecretGeneration: "0"}
		if !reflect.DeepEqual(p, want) {
			t.Fatal("default invented identity/config")
		}
	}
	assertDefault()
	for _, bound := range []int{0, -1, 95, 101} {
		before := personalKeyBehaviorLedger(t, db)
		if database.MigrateThrough(ctx, db, bound) == nil || !reflect.DeepEqual(before, personalKeyBehaviorLedger(t, db)) {
			t.Fatal("bounded ledger rejection", bound)
		}
	}
	if database.MigrateThrough(ctx, db, 96) != nil {
		t.Fatal("current bound")
	}
	// Positive retained local, OIDC and OAuth proof rows precede additive DDL.
	birth := time.Now().UTC().Truncate(time.Microsecond)
	var sessions []entity.Session
	var challenges []entity.MFAChallenge
	for i, method := range []string{"", "oidc", "oauth"} {
		u := entity.User{ID: []string{"usr_ldap_mig_local", "usr_ldap_mig_oidc", "usr_ldap_mig_oauth"}[i], Email: []string{"ldap-local@example.invalid", "ldap-oidc@example.invalid", "ldap-oauth@example.invalid"}[i], Name: "Retained member", Role: entity.RoleMember, PasswordHash: "retained-not-proof"}
		if db.Create(&u).Error != nil {
			t.Fatal("retained user")
		}
		row := entity.Session{ID: []string{"ses_ldap_mig_local", "ses_ldap_mig_oidc", "ses_ldap_mig_oauth"}[i], UserID: u.ID, TokenHash: strings.Repeat(string(rune('a'+i)), 64), CreatedAt: birth, ExpiresAt: birth.Add(time.Hour), PrimaryMethod: method}
		c := entity.MFAChallenge{UserID: u.ID, Purpose: "login", TokenHash: strings.Repeat(string(rune('d'+i)), 64), PasswordDigest: strings.Repeat("f", 64), Generation: strings.Repeat("a", 64), ExpiresAt: birth.Add(time.Minute)}
		if method == "oidc" {
			row.OIDCBindingID = "oib_retained"
			row.OIDCBindingCreatedAt = &birth
			row.OIDCConfigRevision = strings.Repeat("a", 64)
			row.OIDCPolicyRevision = strings.Repeat("b", 64)
			row.OIDCUserCreatedAt = &u.CreatedAt
			c.PrimaryMethod = method
			c.OIDCBindingID = row.OIDCBindingID
			c.OIDCBindingCreatedAt = &birth
			c.OIDCConfigRevision = row.OIDCConfigRevision
			c.OIDCPolicyRevision = row.OIDCPolicyRevision
			c.OIDCUserCreatedAt = &u.CreatedAt
		}
		if method == "oauth" {
			row.OAuthBindingID = "oab_retained"
			row.OAuthBindingCreatedAt = &birth
			row.OAuthConfigRevision = strings.Repeat("c", 64)
			row.OAuthPolicyRevision = strings.Repeat("d", 64)
			row.OAuthUserCreatedAt = &u.CreatedAt
			c.PrimaryMethod = method
			c.OAuthBindingID = row.OAuthBindingID
			c.OAuthBindingCreatedAt = &birth
			c.OAuthConfigRevision = row.OAuthConfigRevision
			c.OAuthPolicyRevision = row.OAuthPolicyRevision
			c.OAuthUserCreatedAt = &u.CreatedAt
		}
		if db.Create(&row).Error != nil || db.Create(&c).Error != nil {
			t.Fatal("retained primary rows")
		}
		if db.Take(&row, "id = ?", row.ID).Error != nil || db.Take(&c, "user_id = ? AND purpose = ?", u.ID, "login").Error != nil {
			t.Fatal("read retained baseline")
		}
		sessions = append(sessions, row)
		challenges = append(challenges, c)
	}
	retained := func(stage string) {
		t.Helper()
		for i, want := range sessions {
			var got entity.Session
			var c entity.MFAChallenge
			sessionReadOK := db.Session(&gorm.Session{QueryFields: true}).Take(&got, "id = ?", want.ID).Error == nil
			challengeReadOK := false
			if sessionReadOK {
				challengeReadOK = db.Session(&gorm.Session{QueryFields: true}).Take(&c, "user_id = ? AND purpose = ?", challenges[i].UserID, "login").Error == nil
			}
			if !sessionReadOK || !challengeReadOK || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(c, challenges[i]) {
				// Failure diagnostics contain field names only, never retained values.
				differentFields := func(got, want any) []string {
					left, right := reflect.ValueOf(got), reflect.ValueOf(want)
					var fields []string
					for field := 0; field < left.NumField(); field++ {
						if !reflect.DeepEqual(left.Field(field).Interface(), right.Field(field).Interface()) {
							fields = append(fields, left.Type().Field(field).Name)
						}
					}
					return fields
				}
				t.Fatal("V96 rewrote historical primary", "stage", stage, "row", i, "session_read_ok", sessionReadOK, "challenge_read_ok", challengeReadOK, "session_fields", differentFields(got, want), "challenge_fields", differentFields(c, challenges[i]))
			}
		}
	}
	checks := []struct {
		model, proof, old any
		name              string
	}{{&ldapFixtureSessionV96{}, &ldapFixtureSessionProofV96{}, &oauthFixtureSessionProofV95{}, "ck_sessions_oidc_primary"}, {&ldapFixtureMFAChallengeV96{}, &ldapFixtureMFAProofV96{}, &oauthFixtureMFAProofV95{}, "ck_mfa_challenges_oidc_primary"}}
	fields := []string{"LDAPBindingID", "LDAPBindingCreatedAt", "LDAPConfigRevision", "LDAPPolicyRevision", "LDAPUserCreatedAt"}
	for _, v := range checks {
		if db.Migrator().DropConstraint(v.proof, v.name) != nil {
			t.Fatal("drop current primary check")
		}
		for _, field := range fields {
			if db.Migrator().DropColumn(v.model, field) != nil {
				t.Fatal("drop only additive LDAP column")
			}
		}
		if db.Migrator().CreateConstraint(v.old, v.name) != nil {
			t.Fatal("restore immutable V95 primary check")
		}
	}
	rootChecks := []struct {
		model any
		name  string
	}{{&ldapHistoricalRootJobV95{}, "ck_secret_inventory_version"}, {&ldapHistoricalRootJobV95{}, "ck_secret_rotation_domain"}, {&ldapHistoricalRootItemV95{}, "ck_secret_item_domain"}, {&ldapHistoricalRootProcessV95{}, "ck_secret_process_inventory_version"}}
	for _, v := range rootChecks {
		if db.Migrator().DropConstraint(v.model, v.name) != nil || db.Migrator().CreateConstraint(v.model, v.name) != nil {
			t.Fatal("restore exact V4 root checks")
		}
	}
	if db.Migrator().DropTable(&ldapFixtureBindingV96{}, &ldapFixtureProviderV96{}) != nil {
		t.Fatal("drop only new LDAP tables")
	}
	remove()
	if database.MigrateThrough(ctx, db, 95) != nil {
		t.Fatal("faithful bounded V95")
	}
	// Current entities have additive columns, so compare retained full rows only
	// after V96 restores them; V95 proof constraints above were installed first.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- database.MigrateThrough(ctx, db, 96) })
	}
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal("concurrent upgrade", e)
		}
	}
	migrate()
	assertDefault()
	retained("concurrent_upgrade")
	baseline := readProvider()
	current := func(stage string) {
		t.Helper()
		ledger(true)
		retained(stage)
		if !reflect.DeepEqual(readProvider(), baseline) {
			t.Fatal("replay changed singleton")
		}
	}
	ldapAssertRootInventoryV96(t, db)
	current("root_constraints")
	if db.Migrator().DropConstraint(checks[0].proof, checks[0].name) != nil || db.Migrator().DropColumn(checks[0].model, "LDAPPolicyRevision") != nil {
		t.Fatal("partial additive fixture")
	}
	remove()
	migrate()
	current("partial_column_replay")
	remove()
	migrate()
	migrate()
	current("repeat_replay")
	for _, bad := range []struct {
		field string
		model any
	}{{"Subject", &ldapWrongSubjectWidth{}}, {"UserCreatedAt", &ldapWrongBirthType{}}, {"UserCreatedAt", &ldapWrongBirthPrecision{}}, {"Subject", &ldapWrongRequiredNull{}}, {"UserCreatedAt", &ldapWrongBirthDefault{}}} {
		current("before_wrong_column")
		var n int64
		if db.Model(&ldapFixtureBindingV96{}).Count(&n).Error != nil || n != 0 {
			t.Fatal("shape negative requires empty table")
		}
		if db.Migrator().AlterColumn(bad.model, bad.field) != nil {
			t.Fatal("install wrong shape")
		}
		remove()
		if database.MigrateThrough(ctx, db, 96) == nil {
			t.Fatal("wrong column accepted", bad.field)
		}
		ledger(false)
		if db.Migrator().AlterColumn(&ldapFixtureBindingV96{}, bad.field) != nil {
			t.Fatal("restore frozen shape")
		}
		migrate()
		current("restored_wrong_column")
	}
	for _, bad := range []any{&ldapWrongOwnerNonunique{}, &ldapWrongOwnerColumn{}, &ldapWrongOwnerExtra{}} {
		current("before_wrong_index")
		if database.DropIndex(db, &ldapFixtureBindingV96{}, "idx_ldap_bindings_user_id") != nil || db.Migrator().CreateIndex(bad, "idx_ldap_bindings_user_id") != nil {
			t.Fatal("install wrong index")
		}
		remove()
		if database.MigrateThrough(ctx, db, 96) == nil {
			t.Fatal("wrong index accepted")
		}
		ledger(false)
		if database.DropIndex(db, &ldapFixtureBindingV96{}, "idx_ldap_bindings_user_id") != nil || db.Migrator().CreateIndex(&ldapFixtureBindingV96{}, "idx_ldap_bindings_user_id") != nil {
			t.Fatal("restore exact index")
		}
		migrate()
		current("restored_wrong_index")
	}
	// Constraint negatives run in isolated transactions; PostgreSQL's aborted
	// transaction cannot poison the next positive or MySQL DDL/replay checks.
	binding := ldapFixtureBindingV96{ID: "ldb_constraint", ProviderID: "ldap", IdentityAttribute: "entryUUID", UserID: "usr_constraint", UserCreatedAt: birth, ConfigRevision: strings.Repeat("a", 64), Subject: "MTIzNDU2NzgtMTIzNC00MzIxLWFiY2QtMTIzNDU2Nzg5YWJj", SubjectDigest: strings.Repeat("b", 64), CreatedAt: birth}
	if db.Create(&binding).Error != nil {
		t.Fatal("positive binding")
	}
	for _, v := range []struct{ provider, attribute string }{{"LDAP", "entryUUID"}, {"ldap ", "entryUUID"}, {"ldap", "entryuuid"}, {"ldap", "entryUUID "}, {"ldap", ""}, {"ldap", "uid"}} {
		e := db.Transaction(func(tx *gorm.DB) error {
			x := binding
			x.ID = "ldb_negative"
			x.UserID = "usr_negative"
			x.SubjectDigest = strings.Repeat("c", 64)
			x.ProviderID = v.provider
			x.IdentityAttribute = v.attribute
			return tx.Create(&x).Error
		})
		if e == nil {
			t.Fatal("alias/check admitted")
		}
	}
	for _, field := range []string{"user", "digest"} {
		e := db.Transaction(func(tx *gorm.DB) error {
			x := binding
			x.ID = "ldb_duplicate"
			if field == "user" {
				x.SubjectDigest = strings.Repeat("c", 64)
			} else {
				x.UserID = "usr_other"
			}
			return tx.Create(&x).Error
		})
		if !errors.Is(e, gorm.ErrDuplicatedKey) {
			t.Fatal("unique stable binding", field, e)
		}
	}
	if db.Delete(&binding).Error != nil {
		t.Fatal("remove temporary binding")
	}
	for _, v := range []map[string]any{{"id": "LDAP"}, {"id": "ldap "}, {"identity_attribute": "entryuuid"}, {"identity_attribute": "entryUUID "}, {"enabled": true}, {"auth_ciphertext": "not-empty"}} {
		e := db.Transaction(func(tx *gorm.DB) error {
			return tx.Model(&ldapFixtureProviderV96{}).Where("id = ?", "ldap").Updates(v).Error
		})
		if e == nil {
			t.Fatal("singleton/default alias accepted")
		}
	}
	for _, v := range []map[string]any{{"primary_method": "LDAP"}, {"ldap_binding_id": "ldb_mixed"}, {"primary_method": "ldap"}, {"primary_method": "ldap", "ldap_binding_id": "ldb_exact", "ldap_binding_created_at": birth, "ldap_config_revision": strings.Repeat("a", 64), "ldap_policy_revision": strings.Repeat("b", 64), "ldap_user_created_at": birth, "oauth_binding_id": "oab_mixed"}} {
		e := db.Transaction(func(tx *gorm.DB) error {
			return tx.Model(&entity.Session{}).Where("id = ?", sessions[0].ID).Updates(v).Error
		})
		if e == nil {
			t.Fatal("mixed/partial primary accepted")
		}
	}
	current("after_primary_negatives")
	// V96 was deliberately replayed above; preserve its current durable receipt
	// and original V1-V95 prefix while appending the V97/V98 suffix.
	beforeSuffix := personalKeyBehaviorLedger(t, db)
	if len(beforeSuffix) != 96 || beforeSuffix[95].Version != 96 || !reflect.DeepEqual(original[:95], beforeSuffix[:95]) {
		t.Fatal("LDAP historical closure lost retained V1-V95 prefix or current V96 receipt")
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("restore current after historical LDAP fixture", err)
	}
	finalLedger := personalKeyBehaviorLedger(t, db)
	if len(finalLedger) != 100 || finalLedger[99].Version != 100 || finalLedger[98].Version != 99 || finalLedger[97].Version != 98 || finalLedger[96].Version != 97 || !reflect.DeepEqual(beforeSuffix, finalLedger[:96]) {
		t.Fatal("LDAP historical closure lost retained V96 prefix or V97/V98 suffix")
	}

}

// Use the frozen V96 projections against actual rows, without retaining test
// jobs/items/process proofs or changing any historical inventory envelope.
func ldapAssertRootInventoryV96(t *testing.T, db *gorm.DB) {
	t.Helper()
	rollback := errors.New("rollback LDAP root constraint fixture")
	err := db.Transaction(func(tx *gorm.DB) error {
		job := entity.SecretRotationJob{ID: "srj_ldap_constraints", SourceKeyID: "source", TargetKeyID: "target", CutoverEpoch: 1, ETag: strings.Repeat("a", 64), Status: "migrating", Phase: "migration", InventoryVersion: 1, ScanGeneration: 1, CountsJSON: "{}"}
		process := entity.SecretProcessVerification{ProcessID: "spv_ldap_constraints", InventoryVersion: 1, PolicyEpoch: 1, KeyManifestDigest: strings.Repeat("b", 64), CryptoVersion: 2, RuntimeSourceDigest: strings.Repeat("c", 64), VerifiedAt: time.Now().UTC().Truncate(time.Microsecond)}
		item := entity.SecretRotationItem{JobID: job.ID, Domain: "provider_credentials", SubjectID: "fixture", SubjectGeneration: "fixture", Reference: "fixture", OriginalDigest: strings.Repeat("d", 64), ResultDigest: strings.Repeat("e", 64), Outcome: "rewrapped", Attempts: 1}
		for _, row := range []any{&job, &process, &item} {
			if err := tx.Create(row).Error; err != nil {
				t.Fatal("positive root constraint fixture", err)
			}
		}
		// Nested transactions use savepoints: a rejected PostgreSQL statement
		// must not poison the following unchanged-row proof or other cases.
		check := func(model any, column, value string, change map[string]any, accepted bool, expected any) {
			t.Helper()
			err := tx.Transaction(func(probe *gorm.DB) error {
				return probe.Model(model).Where(column+" = ?", value).Updates(change).Error
			})
			if (err == nil) != accepted || tx.Statement.Context.Err() != nil {
				t.Fatal("root constraint acceptance mismatch", column, accepted, err)
			}
			// Updates mutates model fields even when its savepoint rolls back.
			// Read into a fresh projection so a rejected primary key cannot filter it.
			observed := reflect.New(reflect.TypeOf(model).Elem()).Interface()
			if err := tx.Where(column+" = ?", value).Take(observed).Error; err != nil || !reflect.DeepEqual(observed, expected) {
				t.Fatal("root constraint result or rejection changed facts", column, err)
			}
		}
		for _, envelope := range []struct{ version, bound int }{{1, 5}, {2, 7}, {3, 8}, {4, 9}, {5, 10}} {
			for _, domain := range []int{0, envelope.bound} {
				check(&ldapFixtureRootJobV96{}, "id", job.ID, map[string]any{"inventory_version": envelope.version, "domain": domain}, true, &ldapFixtureRootJobV96{InventoryVersion: envelope.version, Domain: domain})
			}
			for _, domain := range []int{-1, envelope.bound + 1} {
				check(&ldapFixtureRootJobV96{}, "id", job.ID, map[string]any{"inventory_version": envelope.version, "domain": domain}, false, &ldapFixtureRootJobV96{InventoryVersion: envelope.version, Domain: envelope.bound})
			}
		}
		for _, version := range []int{0, 6} {
			check(&ldapFixtureRootJobV96{}, "id", job.ID, map[string]any{"inventory_version": version}, false, &ldapFixtureRootJobV96{InventoryVersion: 5, Domain: 10})
		}
		for _, version := range []int{1, 2, 3, 4, 5} {
			check(&ldapFixtureRootProcessV96{}, "process_id", process.ProcessID, map[string]any{"inventory_version": version}, true, &ldapFixtureRootProcessV96{InventoryVersion: version})
		}
		for _, version := range []int{0, 6} {
			check(&ldapFixtureRootProcessV96{}, "process_id", process.ProcessID, map[string]any{"inventory_version": version}, false, &ldapFixtureRootProcessV96{InventoryVersion: 5})
		}
		for _, domain := range []string{"provider_credentials", "egresses", "smtp_settings", "storage_revisions", "user_mfa", "vault_writer_auth", "vault_reader_auth", "oidc_providers", "oauth_providers", "ldap_providers"} {
			check(&ldapFixtureRootItemV96{}, "job_id", job.ID, map[string]any{"domain": domain}, true, &ldapFixtureRootItemV96{Domain: domain})
		}
		for _, domain := range []string{"LDAP_PROVIDERS", "ldap_providers ", "ldap_provider", "unknown"} {
			check(&ldapFixtureRootItemV96{}, "job_id", job.ID, map[string]any{"domain": domain}, false, &ldapFixtureRootItemV96{Domain: "ldap_providers"})
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal("root fixture rollback", err)
	}
	for _, row := range []struct {
		model         any
		column, value string
	}{{&ldapFixtureRootJobV96{}, "id", "srj_ldap_constraints"}, {&ldapFixtureRootProcessV96{}, "process_id", "spv_ldap_constraints"}, {&ldapFixtureRootItemV96{}, "job_id", "srj_ldap_constraints"}} {
		var count int64
		if err := db.Model(row.model).Where(row.column+" = ?", row.value).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("root constraint fixture leaked rows", err)
		}
	}
}
