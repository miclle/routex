package database

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// V96 frozen LDAP schemas. Released V1–V95 definitions remain immutable.
type ldapProviderV96 struct {
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

func (ldapProviderV96) TableName() string { return "ldap_providers" }

type ldapBindingV96 struct {
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

func (ldapBindingV96) TableName() string { return "ldap_bindings" }

type ldapSessionV96 struct {
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

func (ldapSessionV96) TableName() string { return "sessions" }

type ldapMFAChallengeV96 struct {
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

func (ldapMFAChallengeV96) TableName() string { return "mfa_challenges" }

type ldapSessionProofV96 struct {
	PrimaryMethod string `gorm:"check:ck_sessions_oidc_primary,(OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 108 AND ASCII(SUBSTRING(primary_method,2,1)) = 100 AND ASCII(SUBSTRING(primary_method,3,1)) = 97 AND ASCII(SUBSTRING(primary_method,4,1)) = 112 AND CHAR_LENGTH(ldap_binding_id) > 0 AND ldap_binding_created_at IS NOT NULL AND CHAR_LENGTH(ldap_config_revision) = 64 AND CHAR_LENGTH(ldap_policy_revision) = 64 AND ldap_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL)"`
}

func (ldapSessionProofV96) TableName() string { return "sessions" }

type ldapMFAProofV96 struct {
	PrimaryMethod string `gorm:"check:ck_mfa_challenges_oidc_primary,(OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 108 AND ASCII(SUBSTRING(primary_method,2,1)) = 100 AND ASCII(SUBSTRING(primary_method,3,1)) = 97 AND ASCII(SUBSTRING(primary_method,4,1)) = 112 AND CHAR_LENGTH(ldap_binding_id) > 0 AND ldap_binding_created_at IS NOT NULL AND CHAR_LENGTH(ldap_config_revision) = 64 AND CHAR_LENGTH(ldap_policy_revision) = 64 AND ldap_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL)"`
}

func (ldapMFAProofV96) TableName() string { return "mfa_challenges" }

type ldapRootJobV96 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5"`
	Domain           int `gorm:"not null;check:ck_secret_rotation_domain,(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7) OR (inventory_version = 3 AND domain >= 0 AND domain <= 8) OR (inventory_version = 4 AND domain >= 0 AND domain <= 9) OR (inventory_version = 5 AND domain >= 0 AND domain <= 10)"`
}

func (ldapRootJobV96) TableName() string { return "secret_rotation_jobs" }

type ldapRootItemV96 struct {
	Domain string `gorm:"primaryKey;size:32;check:ck_secret_item_domain,((((CHAR_LENGTH(domain) = 20 AND ASCII(SUBSTRING(domain,1,1)) = 112 AND ASCII(SUBSTRING(domain,2,1)) = 114 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 118 AND ASCII(SUBSTRING(domain,5,1)) = 105 AND ASCII(SUBSTRING(domain,6,1)) = 100 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 95 AND ASCII(SUBSTRING(domain,10,1)) = 99 AND ASCII(SUBSTRING(domain,11,1)) = 114 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 100 AND ASCII(SUBSTRING(domain,14,1)) = 101 AND ASCII(SUBSTRING(domain,15,1)) = 110 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 105 AND ASCII(SUBSTRING(domain,18,1)) = 97 AND ASCII(SUBSTRING(domain,19,1)) = 108 AND ASCII(SUBSTRING(domain,20,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 101 AND ASCII(SUBSTRING(domain,2,1)) = 103 AND ASCII(SUBSTRING(domain,3,1)) = 114 AND ASCII(SUBSTRING(domain,4,1)) = 101 AND ASCII(SUBSTRING(domain,5,1)) = 115 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 115) OR (CHAR_LENGTH(domain) = 13 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 109 AND ASCII(SUBSTRING(domain,3,1)) = 116 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 116 AND ASCII(SUBSTRING(domain,9,1)) = 116 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 110 AND ASCII(SUBSTRING(domain,12,1)) = 103 AND ASCII(SUBSTRING(domain,13,1)) = 115) OR (CHAR_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 116 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 97 AND ASCII(SUBSTRING(domain,6,1)) = 103 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 95 AND ASCII(SUBSTRING(domain,9,1)) = 114 AND ASCII(SUBSTRING(domain,10,1)) = 101 AND ASCII(SUBSTRING(domain,11,1)) = 118 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 115 AND ASCII(SUBSTRING(domain,14,1)) = 105 AND ASCII(SUBSTRING(domain,15,1)) = 111 AND ASCII(SUBSTRING(domain,16,1)) = 110 AND ASCII(SUBSTRING(domain,17,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 117 AND ASCII(SUBSTRING(domain,2,1)) = 115 AND ASCII(SUBSTRING(domain,3,1)) = 101 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 109 AND ASCII(SUBSTRING(domain,7,1)) = 102 AND ASCII(SUBSTRING(domain,8,1)) = 97)) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 119 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 105 AND ASCII(SUBSTRING(domain,10,1)) = 116 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 101 AND ASCII(SUBSTRING(domain,9,1)) = 97 AND ASCII(SUBSTRING(domain,10,1)) = 100 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104))) OR (OCTET_LENGTH(domain) = 14 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 105 AND ASCII(SUBSTRING(domain,3,1)) = 100 AND ASCII(SUBSTRING(domain,4,1)) = 99 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 112 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 111 AND ASCII(SUBSTRING(domain,9,1)) = 118 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 100 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 114 AND ASCII(SUBSTRING(domain,14,1)) = 115) OR (OCTET_LENGTH(domain) = 15 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 116 AND ASCII(SUBSTRING(domain,5,1)) = 104 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 112 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 111 AND ASCII(SUBSTRING(domain,10,1)) = 118 AND ASCII(SUBSTRING(domain,11,1)) = 105 AND ASCII(SUBSTRING(domain,12,1)) = 100 AND ASCII(SUBSTRING(domain,13,1)) = 101 AND ASCII(SUBSTRING(domain,14,1)) = 114 AND ASCII(SUBSTRING(domain,15,1)) = 115) OR (OCTET_LENGTH(domain) = 14 AND ASCII(SUBSTRING(domain,1,1)) = 108 AND ASCII(SUBSTRING(domain,2,1)) = 100 AND ASCII(SUBSTRING(domain,3,1)) = 97 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 112 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 111 AND ASCII(SUBSTRING(domain,9,1)) = 118 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 100 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 114 AND ASCII(SUBSTRING(domain,14,1)) = 115)"`
}

func (ldapRootItemV96) TableName() string { return "secret_rotation_items" }

type ldapRootProcessV96 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_process_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5"`
}

func (ldapRootProcessV96) TableName() string { return "secret_process_verifications" }

// ldapMigration is the sole V96 superseding step. The migration runner skips
// every recorded released step; no historical CHECK or ledger row is rewritten.
func ldapMigration(db *gorm.DB) error {
	for _, model := range []any{&ldapSessionV96{}, &ldapMFAChallengeV96{}} {
		for _, field := range []string{"LDAPBindingID", "LDAPBindingCreatedAt", "LDAPConfigRevision", "LDAPPolicyRevision", "LDAPUserCreatedAt"} {
			if !db.Migrator().HasColumn(model, field) {
				if err := db.Migrator().AddColumn(model, field); err != nil {
					return err
				}
			}
		}
	}
	models := []any{&ldapProviderV96{}, &ldapBindingV96{}}
	if err := migrateTables(db, models...); err != nil {
		return err
	}
	for _, model := range append(models, &ldapSessionV96{}, &ldapMFAChallengeV96{}) {
		if err := validateLDAPColumnsV96(db, model); err != nil {
			return err
		}
	}
	for _, model := range models {
		if err := validateLDAPIndexesV96(db, model); err != nil {
			return err
		}
	}
	if err := ldapPrimaryConstraintsV96(db); err != nil {
		return err
	}
	if err := ldapRootInventoryV96(db); err != nil {
		return err
	}
	initial := ldapProviderV96{ID: "ldap", ReviewRevision: strings.Repeat("0", 64), ConfigRevision: strings.Repeat("0", 64), PolicyRevision: strings.Repeat("0", 64), SecretGeneration: "0"}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error
}

// Validate retained facts before replacing either historical named CHECK. A
// partially applied MySQL DROP/CREATE is retried under the migration lock, and
// invalid rows never acquire a V96 ledger entry.
func ldapPrimaryConstraintsV96(db *gorm.DB) error {
	for _, item := range []struct {
		model any
		name  string
	}{
		{&ldapSessionProofV96{}, "ck_sessions_oidc_primary"},
		{&ldapMFAProofV96{}, "ck_mfa_challenges_oidc_primary"},
	} {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(item.model); err != nil {
			return err
		}
		check, ok := stmt.Schema.ParseCheckConstraints()[item.name]
		if !ok {
			return fmt.Errorf("missing frozen LDAP primary constraint")
		}
		var bad int64
		if err := db.Table(stmt.Schema.Table).Where("NOT (" + check.Constraint + ")").Count(&bad).Error; err != nil {
			return err
		}
		if bad != 0 {
			return fmt.Errorf("invalid retained LDAP primary provenance")
		}
	}
	return ldapReplaceChecksV96(db, []ldapCheckV96{
		{&ldapSessionProofV96{}, "ck_sessions_oidc_primary"},
		{&ldapMFAProofV96{}, "ck_mfa_challenges_oidc_primary"},
	})
}

type ldapCheckV96 struct {
	model any
	name  string
}

func ldapReplaceChecksV96(db *gorm.DB, checks []ldapCheckV96) error {
	for _, item := range checks {
		if db.Migrator().HasConstraint(item.model, item.name) {
			if err := db.Migrator().DropConstraint(item.model, item.name); err != nil {
				return err
			}
		}
		if err := db.Migrator().CreateConstraint(item.model, item.name); err != nil {
			return err
		}
	}
	return nil
}

func ldapRootInventoryV96(db *gorm.DB) error {
	var bad int64
	if err := db.Table("secret_rotation_jobs").Where("inventory_version NOT IN ? OR inventory_version IS NULL OR domain IS NULL OR domain < 0 OR (inventory_version = ? AND domain > ?) OR (inventory_version = ? AND domain > ?) OR (inventory_version = ? AND domain > ?) OR (inventory_version = ? AND domain > ?) OR domain > ?", []int{1, 2, 3, 4, 5}, 1, 5, 2, 7, 3, 8, 4, 9, 10).Count(&bad).Error; err != nil {
		return err
	}
	if bad != 0 {
		return fmt.Errorf("invalid retained LDAP root job scope")
	}
	if err := db.Table("secret_process_verifications").Where("inventory_version NOT IN ? OR inventory_version IS NULL", []int{1, 2, 3, 4, 5}).Count(&bad).Error; err != nil {
		return err
	}
	if bad != 0 {
		return fmt.Errorf("invalid retained LDAP root proof scope")
	}
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&ldapRootItemV96{}); err != nil {
		return err
	}
	check, ok := stmt.Schema.ParseCheckConstraints()["ck_secret_item_domain"]
	if !ok {
		return fmt.Errorf("missing frozen LDAP item constraint")
	}
	if err := db.Table("secret_rotation_items").Where("NOT (" + check.Constraint + ")").Count(&bad).Error; err != nil {
		return err
	}
	if bad != 0 {
		return fmt.Errorf("invalid retained LDAP root item scope")
	}
	return ldapReplaceChecksV96(db, []ldapCheckV96{
		{&ldapRootJobV96{}, "ck_secret_inventory_version"}, {&ldapRootJobV96{}, "ck_secret_rotation_domain"},
		{&ldapRootItemV96{}, "ck_secret_item_domain"}, {&ldapRootProcessV96{}, "ck_secret_process_inventory_version"},
	})
}

func validateLDAPColumnsV96(db *gorm.DB, model any) error {
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
			return fmt.Errorf("missing LDAP column %s", f.DBName)
		}
		if f.PrimaryKey {
			primary, known := c.PrimaryKey()
			if !known || !primary {
				return fmt.Errorf("invalid LDAP primary key %s", f.DBName)
			}
		}
		nullable, known := c.Nullable()
		if !known || (f.NotNull || f.PrimaryKey) && nullable || f.FieldType.Kind() == reflect.Pointer && !nullable {
			return fmt.Errorf("invalid LDAP nullability %s", f.DBName)
		}
		kind := strings.ToLower(c.DatabaseTypeName())
		switch string(f.DataType) {
		case "time":
			value, hasDefault := c.DefaultValue()
			if hasDefault && value != "" && !strings.EqualFold(strings.TrimSpace(value), "NULL") {
				return fmt.Errorf("invalid LDAP timestamp default %s", f.DBName)
			}
			precision, _, sized := c.DecimalSize()
			if !sized || precision != 6 || kind != "timestamp" && kind != "timestamptz" && kind != "datetime" {
				return fmt.Errorf("invalid LDAP timestamp %s", f.DBName)
			}
		case "bool":
			if kind != "bool" && kind != "boolean" && kind != "tinyint" {
				return fmt.Errorf("invalid LDAP boolean %s", f.DBName)
			}
		case "string":
			if f.Size > 0 && kind != "varchar" && kind != "character varying" {
				return fmt.Errorf("invalid LDAP text kind %s", f.DBName)
			}
		case "text":
			if kind != "text" {
				return fmt.Errorf("invalid LDAP ciphertext kind %s", f.DBName)
			}
		}
		if string(f.DataType) == "string" && f.Size > 0 {
			n, sized := c.Length()
			if !sized || n != int64(f.Size) {
				return fmt.Errorf("invalid LDAP size %s", f.DBName)
			}
		}
		if !f.HasDefaultValue && string(f.DataType) != "time" {
			value, present := c.DefaultValue()
			if present && value != "" && !strings.EqualFold(strings.TrimSpace(value), "NULL") {
				return fmt.Errorf("invalid LDAP unexpected default %s", f.DBName)
			}
		}
		if f.HasDefaultValue && f.DefaultValue == "" {
			value, present := c.DefaultValue()
			if !present || value != "" && value != "''" && value != "''::character varying" {
				return fmt.Errorf("invalid LDAP default %s", f.DBName)
			}
		}
	}
	return nil
}

func validateLDAPIndexesV96(db *gorm.DB, model any) error {
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
			return fmt.Errorf("invalid LDAP index %s", index.Name)
		}
		unique := index.Class == "UNIQUE"
		for i, column := range columns {
			if !column.ColumnName.Valid || column.ColumnName.String != index.Fields[i].DBName || !column.Position.Valid || column.Position.Int64 != int64(i+1) || !column.IsUnique.Valid || column.IsUnique.Bool != unique || column.PrefixLength.Valid {
				return fmt.Errorf("invalid LDAP index %s", index.Name)
			}
		}
	}
	return nil
}
