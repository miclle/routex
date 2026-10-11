package database

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// V102 is frozen. It adds private API-Key delivery persistence only; no bearer,
// remote dispatch, Provider credential, current authority or fleet claim is seeded.
type apiKeyDeliveryPolicyV102 struct {
	ID                             int        `gorm:"column:id;primaryKey;autoIncrement:false;check:ck_api_key_delivery_policy_singleton,id = 1" json:"-"`
	ReviewRevision                 string     `gorm:"column:review_revision;size:64;not null" json:"-"`
	SecurityRevision               string     `gorm:"column:security_revision;size:64;not null" json:"-"`
	AllowManual                    bool       `gorm:"column:allow_manual;not null" json:"-"`
	AllowManagedVault              bool       `gorm:"column:allow_managed_vault;not null" json:"-"`
	DefaultMode                    string     `gorm:"column:default_mode;size:20;not null;check:ck_api_key_delivery_policy_modes,((OCTET_LENGTH(default_mode) = 6 AND ASCII(SUBSTRING(default_mode,1,1)) = 109 AND ASCII(SUBSTRING(default_mode,2,1)) = 97 AND ASCII(SUBSTRING(default_mode,3,1)) = 110 AND ASCII(SUBSTRING(default_mode,4,1)) = 117 AND ASCII(SUBSTRING(default_mode,5,1)) = 97 AND ASCII(SUBSTRING(default_mode,6,1)) = 108) OR (OCTET_LENGTH(default_mode) = 13 AND ASCII(SUBSTRING(default_mode,1,1)) = 109 AND ASCII(SUBSTRING(default_mode,2,1)) = 97 AND ASCII(SUBSTRING(default_mode,3,1)) = 110 AND ASCII(SUBSTRING(default_mode,4,1)) = 97 AND ASCII(SUBSTRING(default_mode,5,1)) = 103 AND ASCII(SUBSTRING(default_mode,6,1)) = 101 AND ASCII(SUBSTRING(default_mode,7,1)) = 100 AND ASCII(SUBSTRING(default_mode,8,1)) = 95 AND ASCII(SUBSTRING(default_mode,9,1)) = 118 AND ASCII(SUBSTRING(default_mode,10,1)) = 97 AND ASCII(SUBSTRING(default_mode,11,1)) = 117 AND ASCII(SUBSTRING(default_mode,12,1)) = 108 AND ASCII(SUBSTRING(default_mode,13,1)) = 116)) AND (allow_manual OR allow_managed_vault) AND ((OCTET_LENGTH(default_mode) = 6 AND ASCII(SUBSTRING(default_mode,1,1)) = 109 AND ASCII(SUBSTRING(default_mode,2,1)) = 97 AND ASCII(SUBSTRING(default_mode,3,1)) = 110 AND ASCII(SUBSTRING(default_mode,4,1)) = 117 AND ASCII(SUBSTRING(default_mode,5,1)) = 97 AND ASCII(SUBSTRING(default_mode,6,1)) = 108 AND allow_manual) OR (OCTET_LENGTH(default_mode) = 13 AND ASCII(SUBSTRING(default_mode,1,1)) = 109 AND ASCII(SUBSTRING(default_mode,2,1)) = 97 AND ASCII(SUBSTRING(default_mode,3,1)) = 110 AND ASCII(SUBSTRING(default_mode,4,1)) = 97 AND ASCII(SUBSTRING(default_mode,5,1)) = 103 AND ASCII(SUBSTRING(default_mode,6,1)) = 101 AND ASCII(SUBSTRING(default_mode,7,1)) = 100 AND ASCII(SUBSTRING(default_mode,8,1)) = 95 AND ASCII(SUBSTRING(default_mode,9,1)) = 118 AND ASCII(SUBSTRING(default_mode,10,1)) = 97 AND ASCII(SUBSTRING(default_mode,11,1)) = 117 AND ASCII(SUBSTRING(default_mode,12,1)) = 108 AND ASCII(SUBSTRING(default_mode,13,1)) = 116 AND allow_managed_vault)) AND (NOT allow_managed_vault OR (OCTET_LENGTH(integration_id) > 0 AND integration_birth IS NOT NULL AND OCTET_LENGTH(integration_revision) = 64 AND OCTET_LENGTH(vault_revision_id) > 0 AND vault_revision_birth IS NOT NULL AND OCTET_LENGTH(infrastructure_security_revision) = 64 AND OCTET_LENGTH(writer_identity_id) > 0 AND OCTET_LENGTH(provisioner_identity_id) > 0 AND writer_identity_id <> provisioner_identity_id AND OCTET_LENGTH(template_id) > 0 AND OCTET_LENGTH(template_security_revision) = 64 AND OCTET_LENGTH(verified_revision) = 64 AND verified_at IS NOT NULL))" json:"-"`
	IntegrationID                  string     `gorm:"column:integration_id;size:30;not null" json:"-"`
	IntegrationBirth               *time.Time `gorm:"column:integration_birth;precision:6" json:"-"`
	IntegrationRevision            string     `gorm:"column:integration_revision;size:64;not null" json:"-"`
	VaultRevisionID                string     `gorm:"column:vault_revision_id;size:30;not null" json:"-"`
	VaultRevisionBirth             *time.Time `gorm:"column:vault_revision_birth;precision:6" json:"-"`
	InfrastructureSecurityRevision string     `gorm:"column:infrastructure_security_revision;size:64;not null" json:"-"`
	WriterIdentityID               string     `gorm:"column:writer_identity_id;size:30;not null" json:"-"`
	ProvisionerIdentityID          string     `gorm:"column:provisioner_identity_id;size:30;not null" json:"-"`
	TemplateID                     string     `gorm:"column:template_id;size:30;not null" json:"-"`
	TemplateSecurityRevision       string     `gorm:"column:template_security_revision;size:64;not null" json:"-"`
	VerifiedRevision               string     `gorm:"column:verified_revision;size:64;not null" json:"-"`
	VerifiedAt                     *time.Time `gorm:"column:verified_at;precision:6" json:"-"`
	CreatedAt                      time.Time  `gorm:"column:created_at;precision:6;not null" json:"-"`
	UpdatedAt                      time.Time  `gorm:"column:updated_at;precision:6;not null" json:"-"`
}

func (apiKeyDeliveryPolicyV102) TableName() string { return "api_key_delivery_policies" }

type apiKeyDeliveryAuthV102 struct {
	ID                  string    `gorm:"column:id;primaryKey;size:30" json:"-"`
	Purpose             string    `gorm:"column:purpose;size:20;not null;check:ck_api_key_delivery_auth_purpose,(OCTET_LENGTH(purpose) = 6 AND ASCII(SUBSTRING(purpose,1,1)) = 119 AND ASCII(SUBSTRING(purpose,2,1)) = 114 AND ASCII(SUBSTRING(purpose,3,1)) = 105 AND ASCII(SUBSTRING(purpose,4,1)) = 116 AND ASCII(SUBSTRING(purpose,5,1)) = 101 AND ASCII(SUBSTRING(purpose,6,1)) = 114) OR (OCTET_LENGTH(purpose) = 11 AND ASCII(SUBSTRING(purpose,1,1)) = 112 AND ASCII(SUBSTRING(purpose,2,1)) = 114 AND ASCII(SUBSTRING(purpose,3,1)) = 111 AND ASCII(SUBSTRING(purpose,4,1)) = 118 AND ASCII(SUBSTRING(purpose,5,1)) = 105 AND ASCII(SUBSTRING(purpose,6,1)) = 115 AND ASCII(SUBSTRING(purpose,7,1)) = 105 AND ASCII(SUBSTRING(purpose,8,1)) = 111 AND ASCII(SUBSTRING(purpose,9,1)) = 110 AND ASCII(SUBSTRING(purpose,10,1)) = 101 AND ASCII(SUBSTRING(purpose,11,1)) = 114)" json:"-"`
	IntegrationID       string    `gorm:"column:integration_id;size:30;not null" json:"-"`
	IntegrationBirth    time.Time `gorm:"column:integration_birth;precision:6;not null" json:"-"`
	IntegrationRevision string    `gorm:"column:integration_revision;size:64;not null" json:"-"`
	VaultRevisionID     string    `gorm:"column:vault_revision_id;size:30;not null" json:"-"`
	VaultRevisionBirth  time.Time `gorm:"column:vault_revision_birth;precision:6;not null" json:"-"`
	Method              string    `gorm:"column:method;size:16;not null;check:ck_api_key_delivery_auth_method,(OCTET_LENGTH(method) = 5 AND ASCII(SUBSTRING(method,1,1)) = 116 AND ASCII(SUBSTRING(method,2,1)) = 111 AND ASCII(SUBSTRING(method,3,1)) = 107 AND ASCII(SUBSTRING(method,4,1)) = 101 AND ASCII(SUBSTRING(method,5,1)) = 110)" json:"-"`
	SecretGeneration    string    `gorm:"column:secret_generation;size:30;not null" json:"-"`
	AuthCiphertext      string    `gorm:"column:auth_ciphertext;type:text;not null;check:ck_api_key_delivery_auth_material,OCTET_LENGTH(auth_ciphertext) > 0 AND OCTET_LENGTH(secret_generation) > 0 AND OCTET_LENGTH(id) > 0 AND OCTET_LENGTH(integration_id) > 0 AND OCTET_LENGTH(integration_revision) = 64 AND OCTET_LENGTH(vault_revision_id) > 0 AND vault_revision_birth IS NOT NULL" json:"-"`
	CreatedAt           time.Time `gorm:"column:created_at;precision:6;not null" json:"-"`
	UpdatedAt           time.Time `gorm:"column:updated_at;precision:6;not null" json:"-"`
}

func (apiKeyDeliveryAuthV102) TableName() string { return "api_key_delivery_auth" }

type apiKeyCallerTemplateV102 struct {
	ID                    string     `gorm:"column:id;primaryKey;size:30;check:ck_api_key_caller_template_singleton,OCTET_LENGTH(id) = 3 AND ASCII(SUBSTRING(id,1,1)) = 106 AND ASCII(SUBSTRING(id,2,1)) = 119 AND ASCII(SUBSTRING(id,3,1)) = 116" json:"-"`
	ReviewRevision        string     `gorm:"column:review_revision;size:64;not null" json:"-"`
	SecurityRevision      string     `gorm:"column:security_revision;size:64;not null" json:"-"`
	AuthMount             string     `gorm:"column:auth_mount;size:128;not null;check:ck_api_key_caller_template_trust,OCTET_LENGTH(auth_mount) > 0 AND OCTET_LENGTH(issuer) > 0 AND OCTET_LENGTH(jwks_url) > 0 AND OCTET_LENGTH(audience) > 0 AND OCTET_LENGTH(allowed_subjects_json) > 0 AND OCTET_LENGTH(trust_security_revision) = 64" json:"-"`
	Issuer                string     `gorm:"column:issuer;size:2048;not null" json:"-"`
	JWKSURL               string     `gorm:"column:jwks_url;size:2048;not null" json:"-"`
	Audience              string     `gorm:"column:audience;size:256;not null" json:"-"`
	AllowedSubjectsJSON   string     `gorm:"column:allowed_subjects_json;type:text;not null" json:"-"`
	TrustSecurityRevision string     `gorm:"column:trust_security_revision;size:64;not null" json:"-"`
	TokenTTLSeconds       uint64     `gorm:"column:token_ttl_seconds;not null;check:ck_api_key_caller_template_ttl,token_ttl_seconds > 0 AND token_ttl_seconds <= token_max_ttl_seconds AND token_max_ttl_seconds <= 3600" json:"-"`
	TokenMaxTTLSeconds    uint64     `gorm:"column:token_max_ttl_seconds;not null" json:"-"`
	VerifiedRevision      string     `gorm:"column:verified_revision;size:64;not null" json:"-"`
	VerifiedAt            *time.Time `gorm:"column:verified_at;precision:6" json:"-"`
	CreatedAt             time.Time  `gorm:"column:created_at;precision:6;not null" json:"-"`
	UpdatedAt             time.Time  `gorm:"column:updated_at;precision:6;not null" json:"-"`
}

func (apiKeyCallerTemplateV102) TableName() string { return "api_key_caller_templates" }

type apiKeyDeliveryConsumerV102 struct {
	ID              string    `gorm:"column:id;primaryKey;size:30;check:ck_api_key_delivery_consumer_identity,OCTET_LENGTH(id) > 0 AND OCTET_LENGTH(consumer_id) > 0 AND OCTET_LENGTH(application_name) > 0" json:"-"`
	OwnerKind       string    `gorm:"column:owner_kind;size:20;not null;check:ck_api_key_delivery_consumers_owner,(OCTET_LENGTH(owner_kind) = 8 AND ASCII(SUBSTRING(owner_kind,1,1)) = 112 AND ASCII(SUBSTRING(owner_kind,2,1)) = 101 AND ASCII(SUBSTRING(owner_kind,3,1)) = 114 AND ASCII(SUBSTRING(owner_kind,4,1)) = 115 AND ASCII(SUBSTRING(owner_kind,5,1)) = 111 AND ASCII(SUBSTRING(owner_kind,6,1)) = 110 AND ASCII(SUBSTRING(owner_kind,7,1)) = 97 AND ASCII(SUBSTRING(owner_kind,8,1)) = 108) OR (OCTET_LENGTH(owner_kind) = 7 AND ASCII(SUBSTRING(owner_kind,1,1)) = 112 AND ASCII(SUBSTRING(owner_kind,2,1)) = 114 AND ASCII(SUBSTRING(owner_kind,3,1)) = 111 AND ASCII(SUBSTRING(owner_kind,4,1)) = 106 AND ASCII(SUBSTRING(owner_kind,5,1)) = 101 AND ASCII(SUBSTRING(owner_kind,6,1)) = 99 AND ASCII(SUBSTRING(owner_kind,7,1)) = 116)" json:"-"`
	OwnerID         string    `gorm:"column:owner_id;size:30;not null" json:"-"`
	OwnerBirth      time.Time `gorm:"column:owner_birth;precision:6;not null" json:"-"`
	OwnerDigest     string    `gorm:"column:owner_digest;size:64;not null;uniqueIndex:idx_api_key_delivery_consumer_owner,priority:1;check:ck_api_key_delivery_consumers_owner_shape,OCTET_LENGTH(owner_id) > 0 AND OCTET_LENGTH(owner_digest) = 64" json:"-"`
	ConsumerID      string    `gorm:"column:consumer_id;size:64;not null;uniqueIndex:idx_api_key_delivery_consumer_owner,priority:2" json:"-"`
	ApplicationName string    `gorm:"column:application_name;size:100;not null" json:"-"`
	CreatedAt       time.Time `gorm:"column:created_at;precision:6;not null" json:"-"`
	UpdatedAt       time.Time `gorm:"column:updated_at;precision:6;not null" json:"-"`
}

func (apiKeyDeliveryConsumerV102) TableName() string { return "api_key_delivery_consumers" }

type vaultAccessProfileV102 struct {
	ID                             string     `gorm:"column:id;primaryKey;size:30;check:ck_vault_access_profile_ready,NOT (OCTET_LENGTH(status) = 5 AND ASCII(SUBSTRING(status,1,1)) = 114 AND ASCII(SUBSTRING(status,2,1)) = 101 AND ASCII(SUBSTRING(status,3,1)) = 97 AND ASCII(SUBSTRING(status,4,1)) = 100 AND ASCII(SUBSTRING(status,5,1)) = 121) OR (enabled AND OCTET_LENGTH(readiness_digest) = 64 AND readiness_at IS NOT NULL AND OCTET_LENGTH(current_isolation_revision) = 64 AND OCTET_LENGTH(isolation_state) = 17 AND ASCII(SUBSTRING(isolation_state,1,1)) = 111 AND ASCII(SUBSTRING(isolation_state,2,1)) = 112 AND ASCII(SUBSTRING(isolation_state,3,1)) = 101 AND ASCII(SUBSTRING(isolation_state,4,1)) = 114 AND ASCII(SUBSTRING(isolation_state,5,1)) = 97 AND ASCII(SUBSTRING(isolation_state,6,1)) = 116 AND ASCII(SUBSTRING(isolation_state,7,1)) = 111 AND ASCII(SUBSTRING(isolation_state,8,1)) = 114 AND ASCII(SUBSTRING(isolation_state,9,1)) = 95 AND ASCII(SUBSTRING(isolation_state,10,1)) = 97 AND ASCII(SUBSTRING(isolation_state,11,1)) = 116 AND ASCII(SUBSTRING(isolation_state,12,1)) = 116 AND ASCII(SUBSTRING(isolation_state,13,1)) = 101 AND ASCII(SUBSTRING(isolation_state,14,1)) = 115 AND ASCII(SUBSTRING(isolation_state,15,1)) = 116 AND ASCII(SUBSTRING(isolation_state,16,1)) = 101 AND ASCII(SUBSTRING(isolation_state,17,1)) = 100)" json:"-"`
	OwnerKind                      string     `gorm:"column:owner_kind;size:20;not null;check:ck_vault_access_profiles_owner,(OCTET_LENGTH(owner_kind) = 8 AND ASCII(SUBSTRING(owner_kind,1,1)) = 112 AND ASCII(SUBSTRING(owner_kind,2,1)) = 101 AND ASCII(SUBSTRING(owner_kind,3,1)) = 114 AND ASCII(SUBSTRING(owner_kind,4,1)) = 115 AND ASCII(SUBSTRING(owner_kind,5,1)) = 111 AND ASCII(SUBSTRING(owner_kind,6,1)) = 110 AND ASCII(SUBSTRING(owner_kind,7,1)) = 97 AND ASCII(SUBSTRING(owner_kind,8,1)) = 108) OR (OCTET_LENGTH(owner_kind) = 7 AND ASCII(SUBSTRING(owner_kind,1,1)) = 112 AND ASCII(SUBSTRING(owner_kind,2,1)) = 114 AND ASCII(SUBSTRING(owner_kind,3,1)) = 111 AND ASCII(SUBSTRING(owner_kind,4,1)) = 106 AND ASCII(SUBSTRING(owner_kind,5,1)) = 101 AND ASCII(SUBSTRING(owner_kind,6,1)) = 99 AND ASCII(SUBSTRING(owner_kind,7,1)) = 116)" json:"-"`
	OwnerID                        string     `gorm:"column:owner_id;size:30;not null;check:ck_vault_access_profile_target,OCTET_LENGTH(target_digest) = 64 AND OCTET_LENGTH(consumer_id) > 0 AND OCTET_LENGTH(environment) > 0 AND OCTET_LENGTH(config_revision) = 64 AND OCTET_LENGTH(security_revision) = 64" json:"-"`
	OwnerBirth                     time.Time  `gorm:"column:owner_birth;precision:6;not null;check:ck_vault_access_profiles_security,OCTET_LENGTH(integration_id) > 0 AND OCTET_LENGTH(integration_revision) = 64 AND OCTET_LENGTH(vault_revision_id) > 0 AND vault_revision_birth IS NOT NULL AND OCTET_LENGTH(infrastructure_security_revision) = 64 AND OCTET_LENGTH(writer_identity_id) > 0 AND OCTET_LENGTH(writer_generation) > 0 AND OCTET_LENGTH(provisioner_identity_id) > 0 AND OCTET_LENGTH(provisioner_generation) > 0 AND writer_identity_id <> provisioner_identity_id AND OCTET_LENGTH(template_id) > 0 AND OCTET_LENGTH(template_security_revision) = 64 AND OCTET_LENGTH(auth_mount) > 0 AND OCTET_LENGTH(issuer) > 0 AND OCTET_LENGTH(jwks_url) > 0 AND OCTET_LENGTH(audience) > 0 AND OCTET_LENGTH(trust_security_revision) = 64" json:"-"`
	OwnerDigest                    string     `gorm:"column:owner_digest;size:64;not null;check:ck_vault_access_profiles_owner_shape,OCTET_LENGTH(owner_id) > 0 AND OCTET_LENGTH(owner_digest) = 64" json:"-"`
	ConsumerID                     string     `gorm:"column:consumer_id;size:64;not null" json:"-"`
	Environment                    string     `gorm:"column:environment;size:64;not null" json:"-"`
	TargetDigest                   string     `gorm:"column:target_digest;size:64;not null;uniqueIndex:idx_vault_access_profile_target" json:"-"`
	ApplicationName                string     `gorm:"column:application_name;size:100;not null" json:"-"`
	ReviewRevision                 string     `gorm:"column:review_revision;size:64;not null" json:"-"`
	Status                         string     `gorm:"column:status;size:20;not null;check:ck_vault_access_profile_status,(OCTET_LENGTH(status) = 7 AND ASCII(SUBSTRING(status,1,1)) = 112 AND ASCII(SUBSTRING(status,2,1)) = 101 AND ASCII(SUBSTRING(status,3,1)) = 110 AND ASCII(SUBSTRING(status,4,1)) = 100 AND ASCII(SUBSTRING(status,5,1)) = 105 AND ASCII(SUBSTRING(status,6,1)) = 110 AND ASCII(SUBSTRING(status,7,1)) = 103) OR (OCTET_LENGTH(status) = 5 AND ASCII(SUBSTRING(status,1,1)) = 114 AND ASCII(SUBSTRING(status,2,1)) = 101 AND ASCII(SUBSTRING(status,3,1)) = 97 AND ASCII(SUBSTRING(status,4,1)) = 100 AND ASCII(SUBSTRING(status,5,1)) = 121) OR (OCTET_LENGTH(status) = 5 AND ASCII(SUBSTRING(status,1,1)) = 101 AND ASCII(SUBSTRING(status,2,1)) = 114 AND ASCII(SUBSTRING(status,3,1)) = 114 AND ASCII(SUBSTRING(status,4,1)) = 111 AND ASCII(SUBSTRING(status,5,1)) = 114)" json:"-"`
	Enabled                        bool       `gorm:"column:enabled;not null" json:"-"`
	ConfigRevision                 string     `gorm:"column:config_revision;size:64;not null" json:"-"`
	SecurityRevision               string     `gorm:"column:security_revision;size:64;not null" json:"-"`
	GeneratedPolicy                string     `gorm:"column:generated_policy;size:128;not null" json:"-"`
	CurrentIsolationRevision       string     `gorm:"column:current_isolation_revision;size:64;not null" json:"-"`
	IsolationState                 string     `gorm:"column:isolation_state;size:20;not null;check:ck_vault_access_profile_isolation,(OCTET_LENGTH(isolation_state) = 7 AND ASCII(SUBSTRING(isolation_state,1,1)) = 109 AND ASCII(SUBSTRING(isolation_state,2,1)) = 105 AND ASCII(SUBSTRING(isolation_state,3,1)) = 115 AND ASCII(SUBSTRING(isolation_state,4,1)) = 115 AND ASCII(SUBSTRING(isolation_state,5,1)) = 105 AND ASCII(SUBSTRING(isolation_state,6,1)) = 110 AND ASCII(SUBSTRING(isolation_state,7,1)) = 103) OR (OCTET_LENGTH(isolation_state) = 17 AND ASCII(SUBSTRING(isolation_state,1,1)) = 111 AND ASCII(SUBSTRING(isolation_state,2,1)) = 112 AND ASCII(SUBSTRING(isolation_state,3,1)) = 101 AND ASCII(SUBSTRING(isolation_state,4,1)) = 114 AND ASCII(SUBSTRING(isolation_state,5,1)) = 97 AND ASCII(SUBSTRING(isolation_state,6,1)) = 116 AND ASCII(SUBSTRING(isolation_state,7,1)) = 111 AND ASCII(SUBSTRING(isolation_state,8,1)) = 114 AND ASCII(SUBSTRING(isolation_state,9,1)) = 95 AND ASCII(SUBSTRING(isolation_state,10,1)) = 97 AND ASCII(SUBSTRING(isolation_state,11,1)) = 116 AND ASCII(SUBSTRING(isolation_state,12,1)) = 116 AND ASCII(SUBSTRING(isolation_state,13,1)) = 101 AND ASCII(SUBSTRING(isolation_state,14,1)) = 115 AND ASCII(SUBSTRING(isolation_state,15,1)) = 116 AND ASCII(SUBSTRING(isolation_state,16,1)) = 101 AND ASCII(SUBSTRING(isolation_state,17,1)) = 100) OR (OCTET_LENGTH(isolation_state) = 7 AND ASCII(SUBSTRING(isolation_state,1,1)) = 114 AND ASCII(SUBSTRING(isolation_state,2,1)) = 101 AND ASCII(SUBSTRING(isolation_state,3,1)) = 118 AND ASCII(SUBSTRING(isolation_state,4,1)) = 111 AND ASCII(SUBSTRING(isolation_state,5,1)) = 107 AND ASCII(SUBSTRING(isolation_state,6,1)) = 101 AND ASCII(SUBSTRING(isolation_state,7,1)) = 100) OR (OCTET_LENGTH(isolation_state) = 5 AND ASCII(SUBSTRING(isolation_state,1,1)) = 115 AND ASCII(SUBSTRING(isolation_state,2,1)) = 116 AND ASCII(SUBSTRING(isolation_state,3,1)) = 97 AND ASCII(SUBSTRING(isolation_state,4,1)) = 108 AND ASCII(SUBSTRING(isolation_state,5,1)) = 101)" json:"-"`
	ReadinessDigest                string     `gorm:"column:readiness_digest;size:64;not null" json:"-"`
	ReadinessAt                    *time.Time `gorm:"column:readiness_at;precision:6" json:"-"`
	IntegrationID                  string     `gorm:"column:integration_id;size:30;not null" json:"-"`
	IntegrationBirth               time.Time  `gorm:"column:integration_birth;precision:6;not null" json:"-"`
	IntegrationRevision            string     `gorm:"column:integration_revision;size:64;not null" json:"-"`
	VaultRevisionID                string     `gorm:"column:vault_revision_id;size:30;not null" json:"-"`
	VaultRevisionBirth             time.Time  `gorm:"column:vault_revision_birth;precision:6;not null" json:"-"`
	InfrastructureSecurityRevision string     `gorm:"column:infrastructure_security_revision;size:64;not null" json:"-"`
	WriterIdentityID               string     `gorm:"column:writer_identity_id;size:30;not null" json:"-"`
	WriterIdentityBirth            time.Time  `gorm:"column:writer_identity_birth;precision:6;not null" json:"-"`
	WriterGeneration               string     `gorm:"column:writer_generation;size:30;not null" json:"-"`
	ProvisionerIdentityID          string     `gorm:"column:provisioner_identity_id;size:30;not null" json:"-"`
	ProvisionerIdentityBirth       time.Time  `gorm:"column:provisioner_identity_birth;precision:6;not null" json:"-"`
	ProvisionerGeneration          string     `gorm:"column:provisioner_generation;size:30;not null" json:"-"`
	TemplateID                     string     `gorm:"column:template_id;size:30;not null" json:"-"`
	TemplateBirth                  time.Time  `gorm:"column:template_birth;precision:6;not null" json:"-"`
	TemplateSecurityRevision       string     `gorm:"column:template_security_revision;size:64;not null" json:"-"`
	AuthMount                      string     `gorm:"column:auth_mount;size:128;not null" json:"-"`
	Issuer                         string     `gorm:"column:issuer;size:2048;not null" json:"-"`
	JWKSURL                        string     `gorm:"column:jwks_url;size:2048;not null" json:"-"`
	Audience                       string     `gorm:"column:audience;size:256;not null" json:"-"`
	TrustSecurityRevision          string     `gorm:"column:trust_security_revision;size:64;not null" json:"-"`
	CreatedAt                      time.Time  `gorm:"column:created_at;precision:6;not null" json:"-"`
	UpdatedAt                      time.Time  `gorm:"column:updated_at;precision:6;not null" json:"-"`
}

func (vaultAccessProfileV102) TableName() string { return "vault_access_profiles" }

type vaultPrincipalBindingV102 struct {
	ID                 string     `gorm:"column:id;primaryKey;size:30;check:ck_vault_principal_security,OCTET_LENGTH(profile_id) > 0 AND OCTET_LENGTH(security_revision) = 64" json:"-"`
	ProfileID          string     `gorm:"column:profile_id;size:30;not null;uniqueIndex:idx_vault_principal_profile" json:"-"`
	ProfileBirth       time.Time  `gorm:"column:profile_birth;precision:6;not null" json:"-"`
	Method             string     `gorm:"column:method;size:16;not null;check:ck_vault_principal_method,(OCTET_LENGTH(method) = 3 AND ASCII(SUBSTRING(method,1,1)) = 106 AND ASCII(SUBSTRING(method,2,1)) = 119 AND ASCII(SUBSTRING(method,3,1)) = 116)" json:"-"`
	SecurityRevision   string     `gorm:"column:security_revision;size:64;not null" json:"-"`
	Issuer             string     `gorm:"column:issuer;size:2048;not null" json:"-"`
	Audience           string     `gorm:"column:audience;size:256;not null" json:"-"`
	Subject            string     `gorm:"column:subject;size:256;not null" json:"-"`
	SubjectDigest      string     `gorm:"column:subject_digest;size:64;not null" json:"-"`
	Enabled            bool       `gorm:"column:enabled;not null;check:ck_vault_principal_approval,(NOT enabled OR (OCTET_LENGTH(approval_revision) = 64 AND OCTET_LENGTH(approval_actor_id) > 0 AND approval_actor_birth IS NOT NULL AND OCTET_LENGTH(approval_reason) > 0 AND approved_at IS NOT NULL)) AND OCTET_LENGTH(subject) > 0 AND OCTET_LENGTH(subject_digest) = 64 AND OCTET_LENGTH(issuer) > 0 AND OCTET_LENGTH(audience) > 0" json:"-"`
	ApprovalRevision   string     `gorm:"column:approval_revision;size:64;not null" json:"-"`
	ApprovalActorID    string     `gorm:"column:approval_actor_id;size:30;not null" json:"-"`
	ApprovalActorBirth *time.Time `gorm:"column:approval_actor_birth;precision:6" json:"-"`
	ApprovalReason     string     `gorm:"column:approval_reason;size:1024;not null" json:"-"`
	ApprovedAt         *time.Time `gorm:"column:approved_at;precision:6" json:"-"`
	GeneratedRole      string     `gorm:"column:generated_role;size:128;not null" json:"-"`
	ReconciledRevision string     `gorm:"column:reconciled_revision;size:64;not null" json:"-"`
	CreatedAt          time.Time  `gorm:"column:created_at;precision:6;not null" json:"-"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;precision:6;not null" json:"-"`
}

func (vaultPrincipalBindingV102) TableName() string { return "vault_principal_bindings" }

type vaultPrincipalIsolationAttestationV102 struct {
	Revision                       string    `gorm:"column:revision;primaryKey;size:64;check:ck_vault_isolation_evidence,OCTET_LENGTH(revision) = 64 AND OCTET_LENGTH(tuple_digest) = 64 AND OCTET_LENGTH(reason) > 0 AND OCTET_LENGTH(operator_id) > 0 AND OCTET_LENGTH(subject) > 0 AND (OCTET_LENGTH(superseded_by_revision) = 0 OR OCTET_LENGTH(superseded_by_revision) = 64)" json:"-"`
	ProfileID                      string    `gorm:"column:profile_id;size:30;not null;check:ck_vault_principal_isolation_attestations_security,OCTET_LENGTH(integration_id) > 0 AND OCTET_LENGTH(integration_revision) = 64 AND OCTET_LENGTH(vault_revision_id) > 0 AND vault_revision_birth IS NOT NULL AND OCTET_LENGTH(infrastructure_security_revision) = 64 AND OCTET_LENGTH(writer_identity_id) > 0 AND OCTET_LENGTH(writer_generation) > 0 AND OCTET_LENGTH(provisioner_identity_id) > 0 AND OCTET_LENGTH(provisioner_generation) > 0 AND writer_identity_id <> provisioner_identity_id AND OCTET_LENGTH(template_id) > 0 AND OCTET_LENGTH(template_security_revision) = 64 AND OCTET_LENGTH(auth_mount) > 0 AND OCTET_LENGTH(issuer) > 0 AND OCTET_LENGTH(jwks_url) > 0 AND OCTET_LENGTH(audience) > 0 AND OCTET_LENGTH(trust_security_revision) = 64 AND OCTET_LENGTH(profile_id) > 0 AND OCTET_LENGTH(principal_id) > 0 AND OCTET_LENGTH(profile_config_revision) = 64 AND OCTET_LENGTH(profile_security_revision) = 64 AND OCTET_LENGTH(principal_security_revision) = 64 AND OCTET_LENGTH(consumer_id) > 0 AND OCTET_LENGTH(environment) > 0" json:"-"`
	ProfileBirth                   time.Time `gorm:"column:profile_birth;precision:6;not null" json:"-"`
	PrincipalID                    string    `gorm:"column:principal_id;size:30;not null" json:"-"`
	PrincipalBirth                 time.Time `gorm:"column:principal_birth;precision:6;not null" json:"-"`
	ProfileConfigRevision          string    `gorm:"column:profile_config_revision;size:64;not null" json:"-"`
	ProfileSecurityRevision        string    `gorm:"column:profile_security_revision;size:64;not null" json:"-"`
	PrincipalSecurityRevision      string    `gorm:"column:principal_security_revision;size:64;not null" json:"-"`
	OwnerKind                      string    `gorm:"column:owner_kind;size:20;not null;check:ck_vault_principal_isolation_attestations_owner,(OCTET_LENGTH(owner_kind) = 8 AND ASCII(SUBSTRING(owner_kind,1,1)) = 112 AND ASCII(SUBSTRING(owner_kind,2,1)) = 101 AND ASCII(SUBSTRING(owner_kind,3,1)) = 114 AND ASCII(SUBSTRING(owner_kind,4,1)) = 115 AND ASCII(SUBSTRING(owner_kind,5,1)) = 111 AND ASCII(SUBSTRING(owner_kind,6,1)) = 110 AND ASCII(SUBSTRING(owner_kind,7,1)) = 97 AND ASCII(SUBSTRING(owner_kind,8,1)) = 108) OR (OCTET_LENGTH(owner_kind) = 7 AND ASCII(SUBSTRING(owner_kind,1,1)) = 112 AND ASCII(SUBSTRING(owner_kind,2,1)) = 114 AND ASCII(SUBSTRING(owner_kind,3,1)) = 111 AND ASCII(SUBSTRING(owner_kind,4,1)) = 106 AND ASCII(SUBSTRING(owner_kind,5,1)) = 101 AND ASCII(SUBSTRING(owner_kind,6,1)) = 99 AND ASCII(SUBSTRING(owner_kind,7,1)) = 116)" json:"-"`
	OwnerID                        string    `gorm:"column:owner_id;size:30;not null" json:"-"`
	OwnerBirth                     time.Time `gorm:"column:owner_birth;precision:6;not null" json:"-"`
	OwnerDigest                    string    `gorm:"column:owner_digest;size:64;not null;check:ck_vault_principal_isolation_attestations_owner_shape,OCTET_LENGTH(owner_id) > 0 AND OCTET_LENGTH(owner_digest) = 64" json:"-"`
	ConsumerID                     string    `gorm:"column:consumer_id;size:64;not null" json:"-"`
	Environment                    string    `gorm:"column:environment;size:64;not null" json:"-"`
	Method                         string    `gorm:"column:method;size:16;not null;check:ck_vault_isolation_method,(OCTET_LENGTH(method) = 3 AND ASCII(SUBSTRING(method,1,1)) = 106 AND ASCII(SUBSTRING(method,2,1)) = 119 AND ASCII(SUBSTRING(method,3,1)) = 116)" json:"-"`
	Subject                        string    `gorm:"column:subject;size:256;not null" json:"-"`
	IntegrationID                  string    `gorm:"column:integration_id;size:30;not null" json:"-"`
	IntegrationBirth               time.Time `gorm:"column:integration_birth;precision:6;not null" json:"-"`
	IntegrationRevision            string    `gorm:"column:integration_revision;size:64;not null" json:"-"`
	VaultRevisionID                string    `gorm:"column:vault_revision_id;size:30;not null" json:"-"`
	VaultRevisionBirth             time.Time `gorm:"column:vault_revision_birth;precision:6;not null" json:"-"`
	InfrastructureSecurityRevision string    `gorm:"column:infrastructure_security_revision;size:64;not null" json:"-"`
	WriterIdentityID               string    `gorm:"column:writer_identity_id;size:30;not null" json:"-"`
	WriterIdentityBirth            time.Time `gorm:"column:writer_identity_birth;precision:6;not null" json:"-"`
	WriterGeneration               string    `gorm:"column:writer_generation;size:30;not null" json:"-"`
	ProvisionerIdentityID          string    `gorm:"column:provisioner_identity_id;size:30;not null" json:"-"`
	ProvisionerIdentityBirth       time.Time `gorm:"column:provisioner_identity_birth;precision:6;not null" json:"-"`
	ProvisionerGeneration          string    `gorm:"column:provisioner_generation;size:30;not null" json:"-"`
	TemplateID                     string    `gorm:"column:template_id;size:30;not null" json:"-"`
	TemplateBirth                  time.Time `gorm:"column:template_birth;precision:6;not null" json:"-"`
	TemplateSecurityRevision       string    `gorm:"column:template_security_revision;size:64;not null" json:"-"`
	AuthMount                      string    `gorm:"column:auth_mount;size:128;not null" json:"-"`
	Issuer                         string    `gorm:"column:issuer;size:2048;not null" json:"-"`
	JWKSURL                        string    `gorm:"column:jwks_url;size:2048;not null" json:"-"`
	Audience                       string    `gorm:"column:audience;size:256;not null" json:"-"`
	TrustSecurityRevision          string    `gorm:"column:trust_security_revision;size:64;not null" json:"-"`
	TupleDigest                    string    `gorm:"column:tuple_digest;size:64;not null" json:"-"`
	State                          string    `gorm:"column:state;size:20;not null;check:ck_vault_isolation_state,(OCTET_LENGTH(state) = 17 AND ASCII(SUBSTRING(state,1,1)) = 111 AND ASCII(SUBSTRING(state,2,1)) = 112 AND ASCII(SUBSTRING(state,3,1)) = 101 AND ASCII(SUBSTRING(state,4,1)) = 114 AND ASCII(SUBSTRING(state,5,1)) = 97 AND ASCII(SUBSTRING(state,6,1)) = 116 AND ASCII(SUBSTRING(state,7,1)) = 111 AND ASCII(SUBSTRING(state,8,1)) = 114 AND ASCII(SUBSTRING(state,9,1)) = 95 AND ASCII(SUBSTRING(state,10,1)) = 97 AND ASCII(SUBSTRING(state,11,1)) = 116 AND ASCII(SUBSTRING(state,12,1)) = 116 AND ASCII(SUBSTRING(state,13,1)) = 101 AND ASCII(SUBSTRING(state,14,1)) = 115 AND ASCII(SUBSTRING(state,15,1)) = 116 AND ASCII(SUBSTRING(state,16,1)) = 101 AND ASCII(SUBSTRING(state,17,1)) = 100) OR (OCTET_LENGTH(state) = 7 AND ASCII(SUBSTRING(state,1,1)) = 114 AND ASCII(SUBSTRING(state,2,1)) = 101 AND ASCII(SUBSTRING(state,3,1)) = 118 AND ASCII(SUBSTRING(state,4,1)) = 111 AND ASCII(SUBSTRING(state,5,1)) = 107 AND ASCII(SUBSTRING(state,6,1)) = 101 AND ASCII(SUBSTRING(state,7,1)) = 100)" json:"-"`
	OperatorID                     string    `gorm:"column:operator_id;size:30;not null" json:"-"`
	OperatorBirth                  time.Time `gorm:"column:operator_birth;precision:6;not null" json:"-"`
	Reason                         string    `gorm:"column:reason;size:1024;not null" json:"-"`
	RecordedAt                     time.Time `gorm:"column:recorded_at;precision:6;not null" json:"-"`
	SupersededByRevision           string    `gorm:"column:superseded_by_revision;size:64;not null" json:"-"`
}

func (vaultPrincipalIsolationAttestationV102) TableName() string {
	return "vault_principal_isolation_attestations"
}

type apiKeyDeliverySlotV102 struct {
	ID                 string    `gorm:"column:id;primaryKey;size:30;check:ck_api_key_delivery_slot_identity,OCTET_LENGTH(slot_digest) = 64 AND OCTET_LENGTH(chain_id) > 0 AND OCTET_LENGTH(profile_id) > 0 AND descriptor_revision >= 0" json:"-"`
	OwnerKind          string    `gorm:"column:owner_kind;size:20;not null;check:ck_api_key_delivery_slots_owner,(OCTET_LENGTH(owner_kind) = 8 AND ASCII(SUBSTRING(owner_kind,1,1)) = 112 AND ASCII(SUBSTRING(owner_kind,2,1)) = 101 AND ASCII(SUBSTRING(owner_kind,3,1)) = 114 AND ASCII(SUBSTRING(owner_kind,4,1)) = 115 AND ASCII(SUBSTRING(owner_kind,5,1)) = 111 AND ASCII(SUBSTRING(owner_kind,6,1)) = 110 AND ASCII(SUBSTRING(owner_kind,7,1)) = 97 AND ASCII(SUBSTRING(owner_kind,8,1)) = 108) OR (OCTET_LENGTH(owner_kind) = 7 AND ASCII(SUBSTRING(owner_kind,1,1)) = 112 AND ASCII(SUBSTRING(owner_kind,2,1)) = 114 AND ASCII(SUBSTRING(owner_kind,3,1)) = 111 AND ASCII(SUBSTRING(owner_kind,4,1)) = 106 AND ASCII(SUBSTRING(owner_kind,5,1)) = 101 AND ASCII(SUBSTRING(owner_kind,6,1)) = 99 AND ASCII(SUBSTRING(owner_kind,7,1)) = 116)" json:"-"`
	OwnerID            string    `gorm:"column:owner_id;size:30;not null" json:"-"`
	OwnerBirth         time.Time `gorm:"column:owner_birth;precision:6;not null" json:"-"`
	OwnerDigest        string    `gorm:"column:owner_digest;size:64;not null;check:ck_api_key_delivery_slots_owner_shape,OCTET_LENGTH(owner_id) > 0 AND OCTET_LENGTH(owner_digest) = 64" json:"-"`
	ProfileID          string    `gorm:"column:profile_id;size:30;not null" json:"-"`
	ProfileBirth       time.Time `gorm:"column:profile_birth;precision:6;not null" json:"-"`
	ChainID            string    `gorm:"column:chain_id;size:30;not null" json:"-"`
	SlotDigest         string    `gorm:"column:slot_digest;size:64;not null;uniqueIndex:idx_api_key_delivery_slot_digest" json:"-"`
	DescriptorRevision uint64    `gorm:"column:descriptor_revision;not null" json:"-"`
	CreatedAt          time.Time `gorm:"column:created_at;precision:6;not null" json:"-"`
	UpdatedAt          time.Time `gorm:"column:updated_at;precision:6;not null" json:"-"`
}

func (apiKeyDeliverySlotV102) TableName() string { return "api_key_delivery_slots" }

type apiKeyVaultDeliveryBindingV102 struct {
	ID                             string     `gorm:"column:id;primaryKey;size:30;check:ck_api_key_vault_delivery_bindings_security,OCTET_LENGTH(integration_id) > 0 AND OCTET_LENGTH(integration_revision) = 64 AND OCTET_LENGTH(vault_revision_id) > 0 AND vault_revision_birth IS NOT NULL AND OCTET_LENGTH(infrastructure_security_revision) = 64 AND OCTET_LENGTH(writer_identity_id) > 0 AND OCTET_LENGTH(writer_generation) > 0 AND OCTET_LENGTH(provisioner_identity_id) > 0 AND OCTET_LENGTH(provisioner_generation) > 0 AND writer_identity_id <> provisioner_identity_id AND OCTET_LENGTH(template_id) > 0 AND OCTET_LENGTH(template_security_revision) = 64 AND OCTET_LENGTH(auth_mount) > 0 AND OCTET_LENGTH(issuer) > 0 AND OCTET_LENGTH(jwks_url) > 0 AND OCTET_LENGTH(audience) > 0 AND OCTET_LENGTH(trust_security_revision) = 64 AND OCTET_LENGTH(profile_id) > 0 AND OCTET_LENGTH(principal_id) > 0 AND OCTET_LENGTH(profile_config_revision) = 64 AND OCTET_LENGTH(profile_security_revision) = 64 AND OCTET_LENGTH(principal_security_revision) = 64 AND OCTET_LENGTH(consumer_id) > 0 AND OCTET_LENGTH(environment) > 0 AND key_kind = owner_kind AND OCTET_LENGTH(key_id) > 0 AND OCTET_LENGTH(slot_id) > 0 AND OCTET_LENGTH(mount) > 0 AND OCTET_LENGTH(path) > 0 AND OCTET_LENGTH(data_field) > 0 AND OCTET_LENGTH(principal_subject) > 0" json:"-"`
	KeyKind                        string     `gorm:"column:key_kind;size:20;not null;check:ck_api_key_vault_binding_kind,(OCTET_LENGTH(key_kind) = 8 AND ASCII(SUBSTRING(key_kind,1,1)) = 112 AND ASCII(SUBSTRING(key_kind,2,1)) = 101 AND ASCII(SUBSTRING(key_kind,3,1)) = 114 AND ASCII(SUBSTRING(key_kind,4,1)) = 115 AND ASCII(SUBSTRING(key_kind,5,1)) = 111 AND ASCII(SUBSTRING(key_kind,6,1)) = 110 AND ASCII(SUBSTRING(key_kind,7,1)) = 97 AND ASCII(SUBSTRING(key_kind,8,1)) = 108) OR (OCTET_LENGTH(key_kind) = 7 AND ASCII(SUBSTRING(key_kind,1,1)) = 112 AND ASCII(SUBSTRING(key_kind,2,1)) = 114 AND ASCII(SUBSTRING(key_kind,3,1)) = 111 AND ASCII(SUBSTRING(key_kind,4,1)) = 106 AND ASCII(SUBSTRING(key_kind,5,1)) = 101 AND ASCII(SUBSTRING(key_kind,6,1)) = 99 AND ASCII(SUBSTRING(key_kind,7,1)) = 116)" json:"-"`
	KeyID                          string     `gorm:"column:key_id;size:30;not null" json:"-"`
	KeyBirth                       time.Time  `gorm:"column:key_birth;precision:6;not null" json:"-"`
	KeyDigest                      string     `gorm:"column:key_digest;size:64;not null;uniqueIndex:idx_api_key_vault_binding_key;check:ck_api_key_vault_binding_ack,descriptor_revision > 0 AND ((version = 0 AND acknowledged_at IS NULL) OR (version = 1 AND acknowledged_at IS NOT NULL)) AND OCTET_LENGTH(key_digest) = 64 AND OCTET_LENGTH(plan_digest) = 64 AND OCTET_LENGTH(isolation_revision) = 64 AND OCTET_LENGTH(isolation_tuple_digest) = 64" json:"-"`
	SlotID                         string     `gorm:"column:slot_id;size:30;not null;uniqueIndex:idx_api_key_vault_binding_slot,priority:1" json:"-"`
	DescriptorRevision             uint64     `gorm:"column:descriptor_revision;not null;uniqueIndex:idx_api_key_vault_binding_slot,priority:2" json:"-"`
	ProfileID                      string     `gorm:"column:profile_id;size:30;not null" json:"-"`
	ProfileBirth                   time.Time  `gorm:"column:profile_birth;precision:6;not null" json:"-"`
	PrincipalID                    string     `gorm:"column:principal_id;size:30;not null" json:"-"`
	PrincipalBirth                 time.Time  `gorm:"column:principal_birth;precision:6;not null" json:"-"`
	ProfileConfigRevision          string     `gorm:"column:profile_config_revision;size:64;not null" json:"-"`
	ProfileSecurityRevision        string     `gorm:"column:profile_security_revision;size:64;not null" json:"-"`
	PrincipalSecurityRevision      string     `gorm:"column:principal_security_revision;size:64;not null" json:"-"`
	OwnerKind                      string     `gorm:"column:owner_kind;size:20;not null;check:ck_api_key_vault_delivery_bindings_owner,(OCTET_LENGTH(owner_kind) = 8 AND ASCII(SUBSTRING(owner_kind,1,1)) = 112 AND ASCII(SUBSTRING(owner_kind,2,1)) = 101 AND ASCII(SUBSTRING(owner_kind,3,1)) = 114 AND ASCII(SUBSTRING(owner_kind,4,1)) = 115 AND ASCII(SUBSTRING(owner_kind,5,1)) = 111 AND ASCII(SUBSTRING(owner_kind,6,1)) = 110 AND ASCII(SUBSTRING(owner_kind,7,1)) = 97 AND ASCII(SUBSTRING(owner_kind,8,1)) = 108) OR (OCTET_LENGTH(owner_kind) = 7 AND ASCII(SUBSTRING(owner_kind,1,1)) = 112 AND ASCII(SUBSTRING(owner_kind,2,1)) = 114 AND ASCII(SUBSTRING(owner_kind,3,1)) = 111 AND ASCII(SUBSTRING(owner_kind,4,1)) = 106 AND ASCII(SUBSTRING(owner_kind,5,1)) = 101 AND ASCII(SUBSTRING(owner_kind,6,1)) = 99 AND ASCII(SUBSTRING(owner_kind,7,1)) = 116)" json:"-"`
	OwnerID                        string     `gorm:"column:owner_id;size:30;not null" json:"-"`
	OwnerBirth                     time.Time  `gorm:"column:owner_birth;precision:6;not null" json:"-"`
	OwnerDigest                    string     `gorm:"column:owner_digest;size:64;not null;check:ck_api_key_vault_delivery_bindings_owner_shape,OCTET_LENGTH(owner_id) > 0 AND OCTET_LENGTH(owner_digest) = 64" json:"-"`
	ConsumerID                     string     `gorm:"column:consumer_id;size:64;not null" json:"-"`
	Environment                    string     `gorm:"column:environment;size:64;not null" json:"-"`
	IntegrationID                  string     `gorm:"column:integration_id;size:30;not null" json:"-"`
	IntegrationBirth               time.Time  `gorm:"column:integration_birth;precision:6;not null" json:"-"`
	IntegrationRevision            string     `gorm:"column:integration_revision;size:64;not null" json:"-"`
	VaultRevisionID                string     `gorm:"column:vault_revision_id;size:30;not null" json:"-"`
	VaultRevisionBirth             time.Time  `gorm:"column:vault_revision_birth;precision:6;not null" json:"-"`
	InfrastructureSecurityRevision string     `gorm:"column:infrastructure_security_revision;size:64;not null" json:"-"`
	WriterIdentityID               string     `gorm:"column:writer_identity_id;size:30;not null" json:"-"`
	WriterIdentityBirth            time.Time  `gorm:"column:writer_identity_birth;precision:6;not null" json:"-"`
	WriterGeneration               string     `gorm:"column:writer_generation;size:30;not null" json:"-"`
	ProvisionerIdentityID          string     `gorm:"column:provisioner_identity_id;size:30;not null" json:"-"`
	ProvisionerIdentityBirth       time.Time  `gorm:"column:provisioner_identity_birth;precision:6;not null" json:"-"`
	ProvisionerGeneration          string     `gorm:"column:provisioner_generation;size:30;not null" json:"-"`
	TemplateID                     string     `gorm:"column:template_id;size:30;not null" json:"-"`
	TemplateBirth                  time.Time  `gorm:"column:template_birth;precision:6;not null" json:"-"`
	TemplateSecurityRevision       string     `gorm:"column:template_security_revision;size:64;not null" json:"-"`
	AuthMount                      string     `gorm:"column:auth_mount;size:128;not null" json:"-"`
	Issuer                         string     `gorm:"column:issuer;size:2048;not null" json:"-"`
	JWKSURL                        string     `gorm:"column:jwks_url;size:2048;not null" json:"-"`
	Audience                       string     `gorm:"column:audience;size:256;not null" json:"-"`
	TrustSecurityRevision          string     `gorm:"column:trust_security_revision;size:64;not null" json:"-"`
	PrincipalSubject               string     `gorm:"column:principal_subject;size:256;not null" json:"-"`
	IsolationRevision              string     `gorm:"column:isolation_revision;size:64;not null" json:"-"`
	IsolationTupleDigest           string     `gorm:"column:isolation_tuple_digest;size:64;not null" json:"-"`
	Mount                          string     `gorm:"column:mount;size:128;not null" json:"-"`
	Path                           string     `gorm:"column:path;size:512;not null" json:"-"`
	DataField                      string     `gorm:"column:data_field;size:64;not null" json:"-"`
	PlanDigest                     string     `gorm:"column:plan_digest;size:64;not null" json:"-"`
	Version                        uint64     `gorm:"column:version;not null" json:"-"`
	AcknowledgedAt                 *time.Time `gorm:"column:acknowledged_at;precision:6" json:"-"`
	CreatedAt                      time.Time  `gorm:"column:created_at;precision:6;not null" json:"-"`
}

func (apiKeyVaultDeliveryBindingV102) TableName() string { return "api_key_vault_delivery_bindings" }

type apiKeyDeliveryCommandV102 struct {
	ID                     string     `gorm:"column:id;primaryKey;size:30;check:ck_api_key_delivery_command_shape,((OCTET_LENGTH(purpose) = 7 AND ASCII(SUBSTRING(purpose,1,1)) = 114 AND ASCII(SUBSTRING(purpose,2,1)) = 101 AND ASCII(SUBSTRING(purpose,3,1)) = 115 AND ASCII(SUBSTRING(purpose,4,1)) = 111 AND ASCII(SUBSTRING(purpose,5,1)) = 108 AND ASCII(SUBSTRING(purpose,6,1)) = 118 AND ASCII(SUBSTRING(purpose,7,1)) = 101) AND OCTET_LENGTH(key_id) = 0 AND key_birth IS NULL AND OCTET_LENGTH(binding_id) = 0 AND OCTET_LENGTH(predecessor_id) = 0 AND predecessor_birth IS NULL AND OCTET_LENGTH(claim_process_id) = 0 AND claim_process_birth IS NULL AND claimed_at IS NULL AND acknowledged_at IS NULL AND published_at IS NULL AND revoked_at IS NULL AND OCTET_LENGTH(delivery_status) = 13 AND ASCII(SUBSTRING(delivery_status,1,1)) = 110 AND ASCII(SUBSTRING(delivery_status,2,1)) = 111 AND ASCII(SUBSTRING(delivery_status,3,1)) = 116 AND ASCII(SUBSTRING(delivery_status,4,1)) = 95 AND ASCII(SUBSTRING(delivery_status,5,1)) = 97 AND ASCII(SUBSTRING(delivery_status,6,1)) = 116 AND ASCII(SUBSTRING(delivery_status,7,1)) = 116 AND ASCII(SUBSTRING(delivery_status,8,1)) = 101 AND ASCII(SUBSTRING(delivery_status,9,1)) = 109 AND ASCII(SUBSTRING(delivery_status,10,1)) = 112 AND ASCII(SUBSTRING(delivery_status,11,1)) = 116 AND ASCII(SUBSTRING(delivery_status,12,1)) = 101 AND ASCII(SUBSTRING(delivery_status,13,1)) = 100 AND OCTET_LENGTH(runtime_status) = 7 AND ASCII(SUBSTRING(runtime_status,1,1)) = 112 AND ASCII(SUBSTRING(runtime_status,2,1)) = 101 AND ASCII(SUBSTRING(runtime_status,3,1)) = 110 AND ASCII(SUBSTRING(runtime_status,4,1)) = 100 AND ASCII(SUBSTRING(runtime_status,5,1)) = 105 AND ASCII(SUBSTRING(runtime_status,6,1)) = 110 AND ASCII(SUBSTRING(runtime_status,7,1)) = 103) OR (((OCTET_LENGTH(purpose) = 6 AND ASCII(SUBSTRING(purpose,1,1)) = 99 AND ASCII(SUBSTRING(purpose,2,1)) = 114 AND ASCII(SUBSTRING(purpose,3,1)) = 101 AND ASCII(SUBSTRING(purpose,4,1)) = 97 AND ASCII(SUBSTRING(purpose,5,1)) = 116 AND ASCII(SUBSTRING(purpose,6,1)) = 101) OR (OCTET_LENGTH(purpose) = 6 AND ASCII(SUBSTRING(purpose,1,1)) = 114 AND ASCII(SUBSTRING(purpose,2,1)) = 111 AND ASCII(SUBSTRING(purpose,3,1)) = 116 AND ASCII(SUBSTRING(purpose,4,1)) = 97 AND ASCII(SUBSTRING(purpose,5,1)) = 116 AND ASCII(SUBSTRING(purpose,6,1)) = 101)) AND OCTET_LENGTH(key_id) > 0 AND key_birth IS NOT NULL AND OCTET_LENGTH(binding_id) > 0 AND OCTET_LENGTH(target_review) = 64 AND OCTET_LENGTH(policy_security_revision) = 64 AND OCTET_LENGTH(isolation_revision) = 64 AND ((OCTET_LENGTH(claim_process_id) = 0 AND claim_process_birth IS NULL AND claimed_at IS NULL) OR (OCTET_LENGTH(claim_process_id) > 0 AND claim_process_birth IS NOT NULL AND claimed_at IS NOT NULL)) AND ((OCTET_LENGTH(purpose) = 6 AND ASCII(SUBSTRING(purpose,1,1)) = 99 AND ASCII(SUBSTRING(purpose,2,1)) = 114 AND ASCII(SUBSTRING(purpose,3,1)) = 101 AND ASCII(SUBSTRING(purpose,4,1)) = 97 AND ASCII(SUBSTRING(purpose,5,1)) = 116 AND ASCII(SUBSTRING(purpose,6,1)) = 101 AND OCTET_LENGTH(predecessor_id) = 0 AND predecessor_birth IS NULL) OR (OCTET_LENGTH(purpose) = 6 AND ASCII(SUBSTRING(purpose,1,1)) = 114 AND ASCII(SUBSTRING(purpose,2,1)) = 111 AND ASCII(SUBSTRING(purpose,3,1)) = 116 AND ASCII(SUBSTRING(purpose,4,1)) = 97 AND ASCII(SUBSTRING(purpose,5,1)) = 116 AND ASCII(SUBSTRING(purpose,6,1)) = 101 AND OCTET_LENGTH(predecessor_id) > 0 AND predecessor_birth IS NOT NULL)) AND (NOT (OCTET_LENGTH(delivery_status) = 12 AND ASCII(SUBSTRING(delivery_status,1,1)) = 97 AND ASCII(SUBSTRING(delivery_status,2,1)) = 99 AND ASCII(SUBSTRING(delivery_status,3,1)) = 107 AND ASCII(SUBSTRING(delivery_status,4,1)) = 110 AND ASCII(SUBSTRING(delivery_status,5,1)) = 111 AND ASCII(SUBSTRING(delivery_status,6,1)) = 119 AND ASCII(SUBSTRING(delivery_status,7,1)) = 108 AND ASCII(SUBSTRING(delivery_status,8,1)) = 101 AND ASCII(SUBSTRING(delivery_status,9,1)) = 100 AND ASCII(SUBSTRING(delivery_status,10,1)) = 103 AND ASCII(SUBSTRING(delivery_status,11,1)) = 101 AND ASCII(SUBSTRING(delivery_status,12,1)) = 100) OR (claimed_at IS NOT NULL AND acknowledged_at IS NOT NULL)) AND (acknowledged_at IS NULL OR (OCTET_LENGTH(delivery_status) = 12 AND ASCII(SUBSTRING(delivery_status,1,1)) = 97 AND ASCII(SUBSTRING(delivery_status,2,1)) = 99 AND ASCII(SUBSTRING(delivery_status,3,1)) = 107 AND ASCII(SUBSTRING(delivery_status,4,1)) = 110 AND ASCII(SUBSTRING(delivery_status,5,1)) = 111 AND ASCII(SUBSTRING(delivery_status,6,1)) = 119 AND ASCII(SUBSTRING(delivery_status,7,1)) = 108 AND ASCII(SUBSTRING(delivery_status,8,1)) = 101 AND ASCII(SUBSTRING(delivery_status,9,1)) = 100 AND ASCII(SUBSTRING(delivery_status,10,1)) = 103 AND ASCII(SUBSTRING(delivery_status,11,1)) = 101 AND ASCII(SUBSTRING(delivery_status,12,1)) = 100 AND claimed_at IS NOT NULL)) AND (NOT (OCTET_LENGTH(delivery_status) = 7 AND ASCII(SUBSTRING(delivery_status,1,1)) = 117 AND ASCII(SUBSTRING(delivery_status,2,1)) = 110 AND ASCII(SUBSTRING(delivery_status,3,1)) = 107 AND ASCII(SUBSTRING(delivery_status,4,1)) = 110 AND ASCII(SUBSTRING(delivery_status,5,1)) = 111 AND ASCII(SUBSTRING(delivery_status,6,1)) = 119 AND ASCII(SUBSTRING(delivery_status,7,1)) = 110) OR (claimed_at IS NOT NULL AND terminal_at IS NOT NULL AND revoked_at IS NOT NULL AND acknowledged_at IS NULL AND published_at IS NULL)) AND (published_at IS NULL OR acknowledged_at IS NOT NULL) AND (NOT (OCTET_LENGTH(runtime_status) = 9 AND ASCII(SUBSTRING(runtime_status,1,1)) = 99 AND ASCII(SUBSTRING(runtime_status,2,1)) = 111 AND ASCII(SUBSTRING(runtime_status,3,1)) = 110 AND ASCII(SUBSTRING(runtime_status,4,1)) = 102 AND ASCII(SUBSTRING(runtime_status,5,1)) = 105 AND ASCII(SUBSTRING(runtime_status,6,1)) = 114 AND ASCII(SUBSTRING(runtime_status,7,1)) = 109 AND ASCII(SUBSTRING(runtime_status,8,1)) = 101 AND ASCII(SUBSTRING(runtime_status,9,1)) = 100) OR (acknowledged_at IS NOT NULL AND published_at IS NOT NULL AND revoked_at IS NULL)) AND (revoked_at IS NULL OR terminal_at IS NOT NULL) AND (NOT (OCTET_LENGTH(delivery_status) = 13 AND ASCII(SUBSTRING(delivery_status,1,1)) = 110 AND ASCII(SUBSTRING(delivery_status,2,1)) = 111 AND ASCII(SUBSTRING(delivery_status,3,1)) = 116 AND ASCII(SUBSTRING(delivery_status,4,1)) = 95 AND ASCII(SUBSTRING(delivery_status,5,1)) = 97 AND ASCII(SUBSTRING(delivery_status,6,1)) = 116 AND ASCII(SUBSTRING(delivery_status,7,1)) = 116 AND ASCII(SUBSTRING(delivery_status,8,1)) = 101 AND ASCII(SUBSTRING(delivery_status,9,1)) = 109 AND ASCII(SUBSTRING(delivery_status,10,1)) = 112 AND ASCII(SUBSTRING(delivery_status,11,1)) = 116 AND ASCII(SUBSTRING(delivery_status,12,1)) = 101 AND ASCII(SUBSTRING(delivery_status,13,1)) = 100) OR (claimed_at IS NULL AND acknowledged_at IS NULL AND terminal_at IS NOT NULL AND revoked_at IS NOT NULL)))" json:"-"`
	OwnerKind              string     `gorm:"column:owner_kind;size:20;not null;check:ck_api_key_delivery_commands_owner,(OCTET_LENGTH(owner_kind) = 8 AND ASCII(SUBSTRING(owner_kind,1,1)) = 112 AND ASCII(SUBSTRING(owner_kind,2,1)) = 101 AND ASCII(SUBSTRING(owner_kind,3,1)) = 114 AND ASCII(SUBSTRING(owner_kind,4,1)) = 115 AND ASCII(SUBSTRING(owner_kind,5,1)) = 111 AND ASCII(SUBSTRING(owner_kind,6,1)) = 110 AND ASCII(SUBSTRING(owner_kind,7,1)) = 97 AND ASCII(SUBSTRING(owner_kind,8,1)) = 108) OR (OCTET_LENGTH(owner_kind) = 7 AND ASCII(SUBSTRING(owner_kind,1,1)) = 112 AND ASCII(SUBSTRING(owner_kind,2,1)) = 114 AND ASCII(SUBSTRING(owner_kind,3,1)) = 111 AND ASCII(SUBSTRING(owner_kind,4,1)) = 106 AND ASCII(SUBSTRING(owner_kind,5,1)) = 101 AND ASCII(SUBSTRING(owner_kind,6,1)) = 99 AND ASCII(SUBSTRING(owner_kind,7,1)) = 116)" json:"-"`
	OwnerID                string     `gorm:"column:owner_id;size:30;not null" json:"-"`
	OwnerBirth             time.Time  `gorm:"column:owner_birth;precision:6;not null" json:"-"`
	OwnerDigest            string     `gorm:"column:owner_digest;size:64;not null;uniqueIndex:idx_api_key_delivery_command_intent,priority:1;check:ck_api_key_delivery_commands_owner_shape,OCTET_LENGTH(owner_id) > 0 AND OCTET_LENGTH(owner_digest) = 64" json:"-"`
	RequestID              string     `gorm:"column:request_id;size:36;not null;uniqueIndex:idx_api_key_delivery_command_intent,priority:2" json:"-"`
	ActorID                string     `gorm:"column:actor_id;size:30;not null" json:"-"`
	ActorBirth             time.Time  `gorm:"column:actor_birth;precision:6;not null" json:"-"`
	SessionID              string     `gorm:"column:session_id;size:30;not null" json:"-"`
	SessionBirth           time.Time  `gorm:"column:session_birth;precision:6;not null" json:"-"`
	Purpose                string     `gorm:"column:purpose;size:20;not null;check:ck_api_key_delivery_command_purpose,(OCTET_LENGTH(purpose) = 7 AND ASCII(SUBSTRING(purpose,1,1)) = 114 AND ASCII(SUBSTRING(purpose,2,1)) = 101 AND ASCII(SUBSTRING(purpose,3,1)) = 115 AND ASCII(SUBSTRING(purpose,4,1)) = 111 AND ASCII(SUBSTRING(purpose,5,1)) = 108 AND ASCII(SUBSTRING(purpose,6,1)) = 118 AND ASCII(SUBSTRING(purpose,7,1)) = 101) OR (OCTET_LENGTH(purpose) = 6 AND ASCII(SUBSTRING(purpose,1,1)) = 99 AND ASCII(SUBSTRING(purpose,2,1)) = 114 AND ASCII(SUBSTRING(purpose,3,1)) = 101 AND ASCII(SUBSTRING(purpose,4,1)) = 97 AND ASCII(SUBSTRING(purpose,5,1)) = 116 AND ASCII(SUBSTRING(purpose,6,1)) = 101) OR (OCTET_LENGTH(purpose) = 6 AND ASCII(SUBSTRING(purpose,1,1)) = 114 AND ASCII(SUBSTRING(purpose,2,1)) = 111 AND ASCII(SUBSTRING(purpose,3,1)) = 116 AND ASCII(SUBSTRING(purpose,4,1)) = 97 AND ASCII(SUBSTRING(purpose,5,1)) = 116 AND ASCII(SUBSTRING(purpose,6,1)) = 101)" json:"-"`
	IntentDigest           string     `gorm:"column:intent_digest;size:64;not null;check:ck_api_key_delivery_command_intent,OCTET_LENGTH(intent_digest) = 64 AND OCTET_LENGTH(request_id) = 36 AND OCTET_LENGTH(owner_digest) = 64 AND OCTET_LENGTH(actor_id) > 0 AND OCTET_LENGTH(session_id) > 0 AND OCTET_LENGTH(reason) > 0" json:"-"`
	ReviewRevision         string     `gorm:"column:review_revision;size:64;not null" json:"-"`
	TargetReview           string     `gorm:"column:target_review;size:64;not null" json:"-"`
	ProfileID              string     `gorm:"column:profile_id;size:30;not null" json:"-"`
	ProfileBirth           time.Time  `gorm:"column:profile_birth;precision:6;not null" json:"-"`
	PrincipalID            string     `gorm:"column:principal_id;size:30;not null" json:"-"`
	PrincipalBirth         time.Time  `gorm:"column:principal_birth;precision:6;not null" json:"-"`
	KeyID                  string     `gorm:"column:key_id;size:30;not null" json:"-"`
	KeyBirth               *time.Time `gorm:"column:key_birth;precision:6" json:"-"`
	BindingID              string     `gorm:"column:binding_id;size:30;not null" json:"-"`
	PredecessorID          string     `gorm:"column:predecessor_id;size:30;not null" json:"-"`
	PredecessorBirth       *time.Time `gorm:"column:predecessor_birth;precision:6" json:"-"`
	PolicySecurityRevision string     `gorm:"column:policy_security_revision;size:64;not null" json:"-"`
	IsolationRevision      string     `gorm:"column:isolation_revision;size:64;not null" json:"-"`
	ClaimProcessID         string     `gorm:"column:claim_process_id;size:30;not null" json:"-"`
	ClaimProcessBirth      *time.Time `gorm:"column:claim_process_birth;precision:6" json:"-"`
	ClaimedAt              *time.Time `gorm:"column:claimed_at;precision:6" json:"-"`
	DeliveryStatus         string     `gorm:"column:delivery_status;size:20;not null;check:ck_api_key_delivery_command_delivery,(OCTET_LENGTH(delivery_status) = 7 AND ASCII(SUBSTRING(delivery_status,1,1)) = 112 AND ASCII(SUBSTRING(delivery_status,2,1)) = 101 AND ASCII(SUBSTRING(delivery_status,3,1)) = 110 AND ASCII(SUBSTRING(delivery_status,4,1)) = 100 AND ASCII(SUBSTRING(delivery_status,5,1)) = 105 AND ASCII(SUBSTRING(delivery_status,6,1)) = 110 AND ASCII(SUBSTRING(delivery_status,7,1)) = 103) OR (OCTET_LENGTH(delivery_status) = 12 AND ASCII(SUBSTRING(delivery_status,1,1)) = 97 AND ASCII(SUBSTRING(delivery_status,2,1)) = 99 AND ASCII(SUBSTRING(delivery_status,3,1)) = 107 AND ASCII(SUBSTRING(delivery_status,4,1)) = 110 AND ASCII(SUBSTRING(delivery_status,5,1)) = 111 AND ASCII(SUBSTRING(delivery_status,6,1)) = 119 AND ASCII(SUBSTRING(delivery_status,7,1)) = 108 AND ASCII(SUBSTRING(delivery_status,8,1)) = 101 AND ASCII(SUBSTRING(delivery_status,9,1)) = 100 AND ASCII(SUBSTRING(delivery_status,10,1)) = 103 AND ASCII(SUBSTRING(delivery_status,11,1)) = 101 AND ASCII(SUBSTRING(delivery_status,12,1)) = 100) OR (OCTET_LENGTH(delivery_status) = 7 AND ASCII(SUBSTRING(delivery_status,1,1)) = 117 AND ASCII(SUBSTRING(delivery_status,2,1)) = 110 AND ASCII(SUBSTRING(delivery_status,3,1)) = 107 AND ASCII(SUBSTRING(delivery_status,4,1)) = 110 AND ASCII(SUBSTRING(delivery_status,5,1)) = 111 AND ASCII(SUBSTRING(delivery_status,6,1)) = 119 AND ASCII(SUBSTRING(delivery_status,7,1)) = 110) OR (OCTET_LENGTH(delivery_status) = 13 AND ASCII(SUBSTRING(delivery_status,1,1)) = 110 AND ASCII(SUBSTRING(delivery_status,2,1)) = 111 AND ASCII(SUBSTRING(delivery_status,3,1)) = 116 AND ASCII(SUBSTRING(delivery_status,4,1)) = 95 AND ASCII(SUBSTRING(delivery_status,5,1)) = 97 AND ASCII(SUBSTRING(delivery_status,6,1)) = 116 AND ASCII(SUBSTRING(delivery_status,7,1)) = 116 AND ASCII(SUBSTRING(delivery_status,8,1)) = 101 AND ASCII(SUBSTRING(delivery_status,9,1)) = 109 AND ASCII(SUBSTRING(delivery_status,10,1)) = 112 AND ASCII(SUBSTRING(delivery_status,11,1)) = 116 AND ASCII(SUBSTRING(delivery_status,12,1)) = 101 AND ASCII(SUBSTRING(delivery_status,13,1)) = 100)" json:"-"`
	RuntimeStatus          string     `gorm:"column:runtime_status;size:20;not null;check:ck_api_key_delivery_command_runtime,(OCTET_LENGTH(runtime_status) = 7 AND ASCII(SUBSTRING(runtime_status,1,1)) = 112 AND ASCII(SUBSTRING(runtime_status,2,1)) = 101 AND ASCII(SUBSTRING(runtime_status,3,1)) = 110 AND ASCII(SUBSTRING(runtime_status,4,1)) = 100 AND ASCII(SUBSTRING(runtime_status,5,1)) = 105 AND ASCII(SUBSTRING(runtime_status,6,1)) = 110 AND ASCII(SUBSTRING(runtime_status,7,1)) = 103) OR (OCTET_LENGTH(runtime_status) = 9 AND ASCII(SUBSTRING(runtime_status,1,1)) = 99 AND ASCII(SUBSTRING(runtime_status,2,1)) = 111 AND ASCII(SUBSTRING(runtime_status,3,1)) = 110 AND ASCII(SUBSTRING(runtime_status,4,1)) = 102 AND ASCII(SUBSTRING(runtime_status,5,1)) = 105 AND ASCII(SUBSTRING(runtime_status,6,1)) = 114 AND ASCII(SUBSTRING(runtime_status,7,1)) = 109 AND ASCII(SUBSTRING(runtime_status,8,1)) = 101 AND ASCII(SUBSTRING(runtime_status,9,1)) = 100) OR (OCTET_LENGTH(runtime_status) = 7 AND ASCII(SUBSTRING(runtime_status,1,1)) = 117 AND ASCII(SUBSTRING(runtime_status,2,1)) = 110 AND ASCII(SUBSTRING(runtime_status,3,1)) = 107 AND ASCII(SUBSTRING(runtime_status,4,1)) = 110 AND ASCII(SUBSTRING(runtime_status,5,1)) = 111 AND ASCII(SUBSTRING(runtime_status,6,1)) = 119 AND ASCII(SUBSTRING(runtime_status,7,1)) = 110)" json:"-"`
	AcknowledgedAt         *time.Time `gorm:"column:acknowledged_at;precision:6" json:"-"`
	PublishedAt            *time.Time `gorm:"column:published_at;precision:6" json:"-"`
	RevokedAt              *time.Time `gorm:"column:revoked_at;precision:6" json:"-"`
	TerminalAt             *time.Time `gorm:"column:terminal_at;precision:6" json:"-"`
	FailureCode            string     `gorm:"column:failure_code;size:64;not null" json:"-"`
	Reason                 string     `gorm:"column:reason;size:1024;not null" json:"-"`
	CreatedAt              time.Time  `gorm:"column:created_at;precision:6;not null" json:"-"`
	UpdatedAt              time.Time  `gorm:"column:updated_at;precision:6;not null" json:"-"`
}

func (apiKeyDeliveryCommandV102) TableName() string { return "api_key_delivery_commands" }

type apiKeyDeliveryPersonalV102 struct {
	ID           string `gorm:"primaryKey;size:30"`
	DeliveryMode string `gorm:"column:delivery_mode;size:20;not null;default:manual;check:ck_api_keys_delivery_mode,(OCTET_LENGTH(delivery_mode) = 6 AND ASCII(SUBSTRING(delivery_mode,1,1)) = 109 AND ASCII(SUBSTRING(delivery_mode,2,1)) = 97 AND ASCII(SUBSTRING(delivery_mode,3,1)) = 110 AND ASCII(SUBSTRING(delivery_mode,4,1)) = 117 AND ASCII(SUBSTRING(delivery_mode,5,1)) = 97 AND ASCII(SUBSTRING(delivery_mode,6,1)) = 108) OR (OCTET_LENGTH(delivery_mode) = 13 AND ASCII(SUBSTRING(delivery_mode,1,1)) = 109 AND ASCII(SUBSTRING(delivery_mode,2,1)) = 97 AND ASCII(SUBSTRING(delivery_mode,3,1)) = 110 AND ASCII(SUBSTRING(delivery_mode,4,1)) = 97 AND ASCII(SUBSTRING(delivery_mode,5,1)) = 103 AND ASCII(SUBSTRING(delivery_mode,6,1)) = 101 AND ASCII(SUBSTRING(delivery_mode,7,1)) = 100 AND ASCII(SUBSTRING(delivery_mode,8,1)) = 95 AND ASCII(SUBSTRING(delivery_mode,9,1)) = 118 AND ASCII(SUBSTRING(delivery_mode,10,1)) = 97 AND ASCII(SUBSTRING(delivery_mode,11,1)) = 117 AND ASCII(SUBSTRING(delivery_mode,12,1)) = 108 AND ASCII(SUBSTRING(delivery_mode,13,1)) = 116)"`
}

func (apiKeyDeliveryPersonalV102) TableName() string { return "api_keys" }

type apiKeyDeliveryProjectV102 struct {
	ID           string `gorm:"primaryKey;size:30"`
	DeliveryMode string `gorm:"column:delivery_mode;size:20;not null;check:ck_project_keys_delivery,(OCTET_LENGTH(delivery_mode) = 6 AND ASCII(SUBSTRING(delivery_mode,1,1)) = 109 AND ASCII(SUBSTRING(delivery_mode,2,1)) = 97 AND ASCII(SUBSTRING(delivery_mode,3,1)) = 110 AND ASCII(SUBSTRING(delivery_mode,4,1)) = 117 AND ASCII(SUBSTRING(delivery_mode,5,1)) = 97 AND ASCII(SUBSTRING(delivery_mode,6,1)) = 108) OR (OCTET_LENGTH(delivery_mode) = 13 AND ASCII(SUBSTRING(delivery_mode,1,1)) = 109 AND ASCII(SUBSTRING(delivery_mode,2,1)) = 97 AND ASCII(SUBSTRING(delivery_mode,3,1)) = 110 AND ASCII(SUBSTRING(delivery_mode,4,1)) = 97 AND ASCII(SUBSTRING(delivery_mode,5,1)) = 103 AND ASCII(SUBSTRING(delivery_mode,6,1)) = 101 AND ASCII(SUBSTRING(delivery_mode,7,1)) = 100 AND ASCII(SUBSTRING(delivery_mode,8,1)) = 95 AND ASCII(SUBSTRING(delivery_mode,9,1)) = 118 AND ASCII(SUBSTRING(delivery_mode,10,1)) = 97 AND ASCII(SUBSTRING(delivery_mode,11,1)) = 117 AND ASCII(SUBSTRING(delivery_mode,12,1)) = 108 AND ASCII(SUBSTRING(delivery_mode,13,1)) = 116)"`
}

func (apiKeyDeliveryProjectV102) TableName() string { return "project_api_keys" }

type apiKeyDeliveryRootJobV102 struct {
	ID                         string     `gorm:"primaryKey;size:30"`
	SourceKeyID                string     `gorm:"size:64;not null"`
	TargetKeyID                string     `gorm:"size:64;not null;check:ck_secret_rotation_keys,CHAR_LENGTH(source_key_id) > 0 AND CHAR_LENGTH(target_key_id) > 0 AND source_key_id <> target_key_id"`
	CutoverEpoch               uint64     `gorm:"not null;check:ck_secret_rotation_epoch,cutover_epoch > 0 AND scan_generation > 0"`
	ETag                       string     `gorm:"column:etag;size:64;not null;check:ck_secret_rotation_etag,CHAR_LENGTH(etag) = 64"`
	Status                     string     `gorm:"size:16;not null;check:ck_secret_rotation_status,((CHAR_LENGTH(status) = 9 AND ASCII(SUBSTRING(status,1,1)) = 109 AND ASCII(SUBSTRING(status,2,1)) = 105 AND ASCII(SUBSTRING(status,3,1)) = 103 AND ASCII(SUBSTRING(status,4,1)) = 114 AND ASCII(SUBSTRING(status,5,1)) = 97 AND ASCII(SUBSTRING(status,6,1)) = 116 AND ASCII(SUBSTRING(status,7,1)) = 105 AND ASCII(SUBSTRING(status,8,1)) = 110 AND ASCII(SUBSTRING(status,9,1)) = 103) OR (CHAR_LENGTH(status) = 7 AND ASCII(SUBSTRING(status,1,1)) = 98 AND ASCII(SUBSTRING(status,2,1)) = 108 AND ASCII(SUBSTRING(status,3,1)) = 111 AND ASCII(SUBSTRING(status,4,1)) = 99 AND ASCII(SUBSTRING(status,5,1)) = 107 AND ASCII(SUBSTRING(status,6,1)) = 101 AND ASCII(SUBSTRING(status,7,1)) = 100) OR (CHAR_LENGTH(status) = 9 AND ASCII(SUBSTRING(status,1,1)) = 111 AND ASCII(SUBSTRING(status,2,1)) = 98 AND ASCII(SUBSTRING(status,3,1)) = 115 AND ASCII(SUBSTRING(status,4,1)) = 101 AND ASCII(SUBSTRING(status,5,1)) = 114 AND ASCII(SUBSTRING(status,6,1)) = 118 AND ASCII(SUBSTRING(status,7,1)) = 105 AND ASCII(SUBSTRING(status,8,1)) = 110 AND ASCII(SUBSTRING(status,9,1)) = 103) OR (CHAR_LENGTH(status) = 5 AND ASCII(SUBSTRING(status,1,1)) = 114 AND ASCII(SUBSTRING(status,2,1)) = 101 AND ASCII(SUBSTRING(status,3,1)) = 97 AND ASCII(SUBSTRING(status,4,1)) = 100 AND ASCII(SUBSTRING(status,5,1)) = 121) OR (CHAR_LENGTH(status) = 9 AND ASCII(SUBSTRING(status,1,1)) = 99 AND ASCII(SUBSTRING(status,2,1)) = 111 AND ASCII(SUBSTRING(status,3,1)) = 109 AND ASCII(SUBSTRING(status,4,1)) = 112 AND ASCII(SUBSTRING(status,5,1)) = 108 AND ASCII(SUBSTRING(status,6,1)) = 101 AND ASCII(SUBSTRING(status,7,1)) = 116 AND ASCII(SUBSTRING(status,8,1)) = 101 AND ASCII(SUBSTRING(status,9,1)) = 100) OR (CHAR_LENGTH(status) = 11 AND ASCII(SUBSTRING(status,1,1)) = 114 AND ASCII(SUBSTRING(status,2,1)) = 111 AND ASCII(SUBSTRING(status,3,1)) = 108 AND ASCII(SUBSTRING(status,4,1)) = 108 AND ASCII(SUBSTRING(status,5,1)) = 101 AND ASCII(SUBSTRING(status,6,1)) = 100 AND ASCII(SUBSTRING(status,7,1)) = 95 AND ASCII(SUBSTRING(status,8,1)) = 98 AND ASCII(SUBSTRING(status,9,1)) = 97 AND ASCII(SUBSTRING(status,10,1)) = 99 AND ASCII(SUBSTRING(status,11,1)) = 107))"`
	Phase                      string     `gorm:"size:16;not null;check:ck_secret_rotation_phase,((CHAR_LENGTH(phase) = 9 AND ASCII(SUBSTRING(phase,1,1)) = 109 AND ASCII(SUBSTRING(phase,2,1)) = 105 AND ASCII(SUBSTRING(phase,3,1)) = 103 AND ASCII(SUBSTRING(phase,4,1)) = 114 AND ASCII(SUBSTRING(phase,5,1)) = 97 AND ASCII(SUBSTRING(phase,6,1)) = 116 AND ASCII(SUBSTRING(phase,7,1)) = 105 AND ASCII(SUBSTRING(phase,8,1)) = 111 AND ASCII(SUBSTRING(phase,9,1)) = 110) OR (CHAR_LENGTH(phase) = 12 AND ASCII(SUBSTRING(phase,1,1)) = 118 AND ASCII(SUBSTRING(phase,2,1)) = 101 AND ASCII(SUBSTRING(phase,3,1)) = 114 AND ASCII(SUBSTRING(phase,4,1)) = 105 AND ASCII(SUBSTRING(phase,5,1)) = 102 AND ASCII(SUBSTRING(phase,6,1)) = 105 AND ASCII(SUBSTRING(phase,7,1)) = 99 AND ASCII(SUBSTRING(phase,8,1)) = 97 AND ASCII(SUBSTRING(phase,9,1)) = 116 AND ASCII(SUBSTRING(phase,10,1)) = 105 AND ASCII(SUBSTRING(phase,11,1)) = 111 AND ASCII(SUBSTRING(phase,12,1)) = 110) OR (CHAR_LENGTH(phase) = 11 AND ASCII(SUBSTRING(phase,1,1)) = 111 AND ASCII(SUBSTRING(phase,2,1)) = 98 AND ASCII(SUBSTRING(phase,3,1)) = 115 AND ASCII(SUBSTRING(phase,4,1)) = 101 AND ASCII(SUBSTRING(phase,5,1)) = 114 AND ASCII(SUBSTRING(phase,6,1)) = 118 AND ASCII(SUBSTRING(phase,7,1)) = 97 AND ASCII(SUBSTRING(phase,8,1)) = 116 AND ASCII(SUBSTRING(phase,9,1)) = 105 AND ASCII(SUBSTRING(phase,10,1)) = 111 AND ASCII(SUBSTRING(phase,11,1)) = 110) OR (CHAR_LENGTH(phase) = 9 AND ASCII(SUBSTRING(phase,1,1)) = 99 AND ASCII(SUBSTRING(phase,2,1)) = 111 AND ASCII(SUBSTRING(phase,3,1)) = 109 AND ASCII(SUBSTRING(phase,4,1)) = 112 AND ASCII(SUBSTRING(phase,5,1)) = 108 AND ASCII(SUBSTRING(phase,6,1)) = 101 AND ASCII(SUBSTRING(phase,7,1)) = 116 AND ASCII(SUBSTRING(phase,8,1)) = 101 AND ASCII(SUBSTRING(phase,9,1)) = 100))"`
	InventoryVersion           int        `gorm:"not null;default:1;check:ck_secret_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5 OR inventory_version = 6 OR inventory_version = 7 OR inventory_version = 8 OR inventory_version = 9"`
	Domain                     int        `gorm:"not null;check:ck_secret_rotation_domain,(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7) OR (inventory_version = 3 AND domain >= 0 AND domain <= 8) OR (inventory_version = 4 AND domain >= 0 AND domain <= 9) OR (inventory_version = 5 AND domain >= 0 AND domain <= 10) OR (inventory_version = 6 AND domain >= 0 AND domain <= 11) OR (inventory_version = 7 AND domain >= 0 AND domain <= 11) OR (inventory_version = 8 AND domain >= 0 AND domain <= 11) OR (inventory_version = 9 AND domain >= 0 AND domain <= 12)"`
	Cursor                     string     `gorm:"size:64;not null"`
	ScanGeneration             uint64     `gorm:"not null"`
	LeaseExecutorID            string     `gorm:"size:30;not null"`
	LeaseToken                 string     `gorm:"size:30;not null" json:"-"`
	LeaseUntil                 *time.Time `gorm:"precision:6"`
	CountsJSON                 string     `gorm:"type:text;not null" json:"-"`
	BlockerCode                string     `gorm:"size:64;not null"`
	ObservationStartedAt       *time.Time `gorm:"precision:6"`
	ObservationLastConfirmedAt *time.Time `gorm:"precision:6"`
	VerifiedProcessID          string     `gorm:"size:30;not null"`
	VerifiedSnapshotID         string     `gorm:"size:30;not null"`
	CreatedAt                  time.Time  `gorm:"precision:6;not null"`
	UpdatedAt                  time.Time  `gorm:"precision:6;not null"`
	CompletedAt                *time.Time `gorm:"precision:6;check:ck_secret_rotation_terminal,(status IN ('completed','rolled_back') AND completed_at IS NOT NULL) OR (status NOT IN ('completed','rolled_back') AND completed_at IS NULL)"`
}

func (apiKeyDeliveryRootJobV102) TableName() string { return "secret_rotation_jobs" }

type apiKeyDeliveryRootProcessV102 struct {
	ProcessID           string    `gorm:"primaryKey;size:30"`
	InventoryVersion    int       `gorm:"not null;default:1;check:ck_secret_process_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5 OR inventory_version = 6 OR inventory_version = 7 OR inventory_version = 8 OR inventory_version = 9"`
	PolicyEpoch         uint64    `gorm:"not null;check:ck_secret_process_epoch,policy_epoch > 0 AND crypto_version = 2"`
	KeyManifestDigest   string    `gorm:"size:64;not null" json:"-"`
	CryptoVersion       int       `gorm:"not null"`
	LeaseToken          string    `gorm:"size:30;not null" json:"-"`
	RuntimeSnapshotID   string    `gorm:"size:30;not null"`
	RuntimeSourceDigest string    `gorm:"size:64;not null" json:"-"`
	VerifiedAt          time.Time `gorm:"precision:6;not null"`
}

func (apiKeyDeliveryRootProcessV102) TableName() string { return "secret_process_verifications" }

type apiKeyDeliveryRootItemV102 struct {
	JobID             string    `gorm:"primaryKey;size:30"`
	Domain            string    `gorm:"primaryKey;size:32;check:ck_secret_item_domain,(((((CHAR_LENGTH(domain) = 20 AND ASCII(SUBSTRING(domain,1,1)) = 112 AND ASCII(SUBSTRING(domain,2,1)) = 114 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 118 AND ASCII(SUBSTRING(domain,5,1)) = 105 AND ASCII(SUBSTRING(domain,6,1)) = 100 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 95 AND ASCII(SUBSTRING(domain,10,1)) = 99 AND ASCII(SUBSTRING(domain,11,1)) = 114 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 100 AND ASCII(SUBSTRING(domain,14,1)) = 101 AND ASCII(SUBSTRING(domain,15,1)) = 110 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 105 AND ASCII(SUBSTRING(domain,18,1)) = 97 AND ASCII(SUBSTRING(domain,19,1)) = 108 AND ASCII(SUBSTRING(domain,20,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 101 AND ASCII(SUBSTRING(domain,2,1)) = 103 AND ASCII(SUBSTRING(domain,3,1)) = 114 AND ASCII(SUBSTRING(domain,4,1)) = 101 AND ASCII(SUBSTRING(domain,5,1)) = 115 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 115) OR (CHAR_LENGTH(domain) = 13 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 109 AND ASCII(SUBSTRING(domain,3,1)) = 116 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 116 AND ASCII(SUBSTRING(domain,9,1)) = 116 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 110 AND ASCII(SUBSTRING(domain,12,1)) = 103 AND ASCII(SUBSTRING(domain,13,1)) = 115) OR (CHAR_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 116 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 97 AND ASCII(SUBSTRING(domain,6,1)) = 103 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 95 AND ASCII(SUBSTRING(domain,9,1)) = 114 AND ASCII(SUBSTRING(domain,10,1)) = 101 AND ASCII(SUBSTRING(domain,11,1)) = 118 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 115 AND ASCII(SUBSTRING(domain,14,1)) = 105 AND ASCII(SUBSTRING(domain,15,1)) = 111 AND ASCII(SUBSTRING(domain,16,1)) = 110 AND ASCII(SUBSTRING(domain,17,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 117 AND ASCII(SUBSTRING(domain,2,1)) = 115 AND ASCII(SUBSTRING(domain,3,1)) = 101 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 109 AND ASCII(SUBSTRING(domain,7,1)) = 102 AND ASCII(SUBSTRING(domain,8,1)) = 97)) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 119 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 105 AND ASCII(SUBSTRING(domain,10,1)) = 116 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 101 AND ASCII(SUBSTRING(domain,9,1)) = 97 AND ASCII(SUBSTRING(domain,10,1)) = 100 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104))) OR (OCTET_LENGTH(domain) = 14 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 105 AND ASCII(SUBSTRING(domain,3,1)) = 100 AND ASCII(SUBSTRING(domain,4,1)) = 99 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 112 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 111 AND ASCII(SUBSTRING(domain,9,1)) = 118 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 100 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 114 AND ASCII(SUBSTRING(domain,14,1)) = 115) OR (OCTET_LENGTH(domain) = 15 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 116 AND ASCII(SUBSTRING(domain,5,1)) = 104 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 112 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 111 AND ASCII(SUBSTRING(domain,10,1)) = 118 AND ASCII(SUBSTRING(domain,11,1)) = 105 AND ASCII(SUBSTRING(domain,12,1)) = 100 AND ASCII(SUBSTRING(domain,13,1)) = 101 AND ASCII(SUBSTRING(domain,14,1)) = 114 AND ASCII(SUBSTRING(domain,15,1)) = 115) OR (OCTET_LENGTH(domain) = 14 AND ASCII(SUBSTRING(domain,1,1)) = 108 AND ASCII(SUBSTRING(domain,2,1)) = 100 AND ASCII(SUBSTRING(domain,3,1)) = 97 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 112 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 111 AND ASCII(SUBSTRING(domain,9,1)) = 118 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 100 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 114 AND ASCII(SUBSTRING(domain,14,1)) = 115)) OR (OCTET_LENGTH(domain) = 24 AND ASCII(SUBSTRING(domain,1,1)) = 110 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 109 AND ASCII(SUBSTRING(domain,4,1)) = 101 AND ASCII(SUBSTRING(domain,5,1)) = 100 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 105 AND ASCII(SUBSTRING(domain,8,1)) = 100 AND ASCII(SUBSTRING(domain,9,1)) = 101 AND ASCII(SUBSTRING(domain,10,1)) = 110 AND ASCII(SUBSTRING(domain,11,1)) = 116 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 116 AND ASCII(SUBSTRING(domain,14,1)) = 121 AND ASCII(SUBSTRING(domain,15,1)) = 95 AND ASCII(SUBSTRING(domain,16,1)) = 112 AND ASCII(SUBSTRING(domain,17,1)) = 114 AND ASCII(SUBSTRING(domain,18,1)) = 111 AND ASCII(SUBSTRING(domain,19,1)) = 118 AND ASCII(SUBSTRING(domain,20,1)) = 105 AND ASCII(SUBSTRING(domain,21,1)) = 100 AND ASCII(SUBSTRING(domain,22,1)) = 101 AND ASCII(SUBSTRING(domain,23,1)) = 114 AND ASCII(SUBSTRING(domain,24,1)) = 115) OR (OCTET_LENGTH(domain) = 21 AND ASCII(SUBSTRING(domain,1,1)) = 97 AND ASCII(SUBSTRING(domain,2,1)) = 112 AND ASCII(SUBSTRING(domain,3,1)) = 105 AND ASCII(SUBSTRING(domain,4,1)) = 95 AND ASCII(SUBSTRING(domain,5,1)) = 107 AND ASCII(SUBSTRING(domain,6,1)) = 101 AND ASCII(SUBSTRING(domain,7,1)) = 121 AND ASCII(SUBSTRING(domain,8,1)) = 95 AND ASCII(SUBSTRING(domain,9,1)) = 100 AND ASCII(SUBSTRING(domain,10,1)) = 101 AND ASCII(SUBSTRING(domain,11,1)) = 108 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 118 AND ASCII(SUBSTRING(domain,14,1)) = 101 AND ASCII(SUBSTRING(domain,15,1)) = 114 AND ASCII(SUBSTRING(domain,16,1)) = 121 AND ASCII(SUBSTRING(domain,17,1)) = 95 AND ASCII(SUBSTRING(domain,18,1)) = 97 AND ASCII(SUBSTRING(domain,19,1)) = 117 AND ASCII(SUBSTRING(domain,20,1)) = 116 AND ASCII(SUBSTRING(domain,21,1)) = 104)"`
	SubjectID         string    `gorm:"primaryKey;size:64"`
	SubjectGeneration string    `gorm:"size:64;not null"`
	Reference         string    `gorm:"size:160;not null" json:"-"`
	OriginalDigest    string    `gorm:"size:64;not null" json:"-"`
	ResultDigest      string    `gorm:"size:64;not null" json:"-"`
	Outcome           string    `gorm:"size:20;not null;check:ck_secret_item_outcome,((CHAR_LENGTH(outcome) = 9 AND ASCII(SUBSTRING(outcome,1,1)) = 114 AND ASCII(SUBSTRING(outcome,2,1)) = 101 AND ASCII(SUBSTRING(outcome,3,1)) = 119 AND ASCII(SUBSTRING(outcome,4,1)) = 114 AND ASCII(SUBSTRING(outcome,5,1)) = 97 AND ASCII(SUBSTRING(outcome,6,1)) = 112 AND ASCII(SUBSTRING(outcome,7,1)) = 112 AND ASCII(SUBSTRING(outcome,8,1)) = 101 AND ASCII(SUBSTRING(outcome,9,1)) = 100) OR (CHAR_LENGTH(outcome) = 14 AND ASCII(SUBSTRING(outcome,1,1)) = 97 AND ASCII(SUBSTRING(outcome,2,1)) = 108 AND ASCII(SUBSTRING(outcome,3,1)) = 114 AND ASCII(SUBSTRING(outcome,4,1)) = 101 AND ASCII(SUBSTRING(outcome,5,1)) = 97 AND ASCII(SUBSTRING(outcome,6,1)) = 100 AND ASCII(SUBSTRING(outcome,7,1)) = 121 AND ASCII(SUBSTRING(outcome,8,1)) = 95 AND ASCII(SUBSTRING(outcome,9,1)) = 116 AND ASCII(SUBSTRING(outcome,10,1)) = 97 AND ASCII(SUBSTRING(outcome,11,1)) = 114 AND ASCII(SUBSTRING(outcome,12,1)) = 103 AND ASCII(SUBSTRING(outcome,13,1)) = 101 AND ASCII(SUBSTRING(outcome,14,1)) = 116) OR (CHAR_LENGTH(outcome) = 7 AND ASCII(SUBSTRING(outcome,1,1)) = 100 AND ASCII(SUBSTRING(outcome,2,1)) = 101 AND ASCII(SUBSTRING(outcome,3,1)) = 108 AND ASCII(SUBSTRING(outcome,4,1)) = 101 AND ASCII(SUBSTRING(outcome,5,1)) = 116 AND ASCII(SUBSTRING(outcome,6,1)) = 101 AND ASCII(SUBSTRING(outcome,7,1)) = 100) OR (CHAR_LENGTH(outcome) = 7 AND ASCII(SUBSTRING(outcome,1,1)) = 99 AND ASCII(SUBSTRING(outcome,2,1)) = 104 AND ASCII(SUBSTRING(outcome,3,1)) = 97 AND ASCII(SUBSTRING(outcome,4,1)) = 110 AND ASCII(SUBSTRING(outcome,5,1)) = 103 AND ASCII(SUBSTRING(outcome,6,1)) = 101 AND ASCII(SUBSTRING(outcome,7,1)) = 100) OR (CHAR_LENGTH(outcome) = 7 AND ASCII(SUBSTRING(outcome,1,1)) = 98 AND ASCII(SUBSTRING(outcome,2,1)) = 108 AND ASCII(SUBSTRING(outcome,3,1)) = 111 AND ASCII(SUBSTRING(outcome,4,1)) = 99 AND ASCII(SUBSTRING(outcome,5,1)) = 107 AND ASCII(SUBSTRING(outcome,6,1)) = 101 AND ASCII(SUBSTRING(outcome,7,1)) = 100))"`
	Attempts          uint64    `gorm:"not null;check:ck_secret_item_attempts,attempts > 0"`
	UpdatedAt         time.Time `gorm:"precision:6;not null"`
}

func (apiKeyDeliveryRootItemV102) TableName() string { return "secret_rotation_items" }

func apiKeyDeliveryModelsV102() []any {
	return []any{&apiKeyDeliveryPolicyV102{}, &apiKeyDeliveryAuthV102{}, &apiKeyCallerTemplateV102{}, &apiKeyDeliveryConsumerV102{}, &vaultAccessProfileV102{}, &vaultPrincipalBindingV102{}, &vaultPrincipalIsolationAttestationV102{}, &apiKeyDeliverySlotV102{}, &apiKeyVaultDeliveryBindingV102{}, &apiKeyDeliveryCommandV102{}}
}

func apiKeyDeliveryMigration(db *gorm.DB) error {
	models := apiKeyDeliveryModelsV102()
	for _, model := range models {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			return err
		}
		if !db.Migrator().HasTable(model) {
			if err := db.Migrator().CreateTable(model); err != nil {
				return err
			}
		} else {
			for _, field := range stmt.Schema.Fields {
				if !db.Migrator().HasColumn(model, field.DBName) {
					// Never let MySQL synthesize zero values into a retained partial row.
					var count int64
					if err := db.Table(stmt.Schema.Table).Count(&count).Error; err != nil {
						return err
					}
					if count != 0 {
						return fmt.Errorf("nonempty partial API-Key delivery table")
					}
					if err := db.Migrator().AddColumn(model, field.Name); err != nil {
						return err
					}
				}
			}
		}
		if err := validateAPIKeyDeliveryColumnsV102(db, model, false); err != nil {
			return err
		}
	}
	// This is the only historical data backfill: a new personal Key column has a
	// frozen manual default. Existing Project modes are validated without rewriting them.
	personal := &apiKeyDeliveryPersonalV102{}
	if !db.Migrator().HasColumn(personal, "DeliveryMode") {
		if err := db.Migrator().AddColumn(personal, "DeliveryMode"); err != nil {
			return err
		}
	}
	for _, model := range []any{personal, &apiKeyDeliveryProjectV102{}} {
		if err := validateAPIKeyDeliveryColumnsV102(db, model, true); err != nil {
			return err
		}
	}
	checks, err := apiKeyDeliveryRetainedChecksV102(db)
	if err != nil {
		return err
	}
	// All retained CHECK predicates pass before replacing any CHECK. DDL is not
	// assumed transactional: repeated startup retries every missing/replaced check.
	if err := apiKeyDeliveryReplaceChecksV102(db, checks); err != nil {
		return err
	}
	for _, model := range models {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			return err
		}
		for _, index := range stmt.Schema.ParseIndexes() {
			if !db.Migrator().HasIndex(model, index.Name) {
				if err := db.Migrator().CreateIndex(model, index.Name); err != nil {
					return err
				}
			}
			columns, err := credentialAttemptStatisticsIndexColumns(db, stmt.Schema.Table, index.Name)
			if err != nil {
				return err
			}
			if len(columns) != len(index.Fields) {
				return fmt.Errorf("invalid API-Key delivery index")
			}
			for i, column := range columns {
				if !column.ColumnName.Valid || column.ColumnName.String != index.Fields[i].DBName || !column.Position.Valid || column.Position.Int64 != int64(i+1) || !column.IsUnique.Valid || column.IsUnique.Bool != (index.Class == "UNIQUE") || column.PrefixLength.Valid {
					return fmt.Errorf("invalid API-Key delivery index")
				}
			}
		}
	}
	initial := apiKeyDeliveryPolicyV102{ID: 1, ReviewRevision: strings.Repeat("0", 64), SecurityRevision: strings.Repeat("0", 64), AllowManual: true, DefaultMode: "manual"}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error
}

func apiKeyDeliveryRetainedChecksV102(db *gorm.DB) ([]apiKeyDeliveryCheckV102, error) {
	checks := []apiKeyDeliveryCheckV102{}
	for _, model := range append(apiKeyDeliveryModelsV102(), &apiKeyDeliveryPersonalV102{}, &apiKeyDeliveryProjectV102{}, &apiKeyDeliveryRootJobV102{}, &apiKeyDeliveryRootProcessV102{}, &apiKeyDeliveryRootItemV102{}) {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			return nil, err
		}
		parsed := stmt.Schema.ParseCheckConstraints()
		names := []string{}
		for name := range parsed {
			if strings.HasPrefix(name, "ck_secret_") && name != "ck_secret_inventory_version" && name != "ck_secret_rotation_domain" && name != "ck_secret_process_inventory_version" && name != "ck_secret_item_domain" {
				continue
			}
			names = append(names, name)
		}
		// Deterministic order keeps retry behavior independent of Go map iteration.
		slices.Sort(names)
		for _, name := range names {
			check := parsed[name]
			var bad int64
			if err := db.Table(stmt.Schema.Table).Where("NOT (" + check.Constraint + ")").Count(&bad).Error; err != nil {
				return nil, err
			}
			if bad != 0 {
				return nil, fmt.Errorf("invalid retained API-Key delivery facts")
			}
			checks = append(checks, apiKeyDeliveryCheckV102{model, name})
		}
	}
	return checks, nil
}

type apiKeyDeliveryCheckV102 struct {
	model any
	name  string
}

func apiKeyDeliveryReplaceChecksV102(db *gorm.DB, checks []apiKeyDeliveryCheckV102) error {
	for _, check := range checks {
		if db.Migrator().HasConstraint(check.model, check.name) {
			if err := db.Migrator().DropConstraint(check.model, check.name); err != nil {
				return err
			}
		}
		if err := db.Migrator().CreateConstraint(check.model, check.name); err != nil {
			return err
		}
	}
	return nil
}
func validateAPIKeyDeliveryColumnsV102(db *gorm.DB, model any, subset bool) error {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return err
	}
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		return err
	}
	byName := map[string]gorm.ColumnType{}
	for _, column := range columns {
		if _, duplicate := byName[column.Name()]; duplicate {
			return fmt.Errorf("invalid API-Key delivery column set")
		}
		byName[column.Name()] = column
	}
	if !subset && len(byName) != len(stmt.Schema.Fields) {
		return fmt.Errorf("invalid API-Key delivery column set")
	}
	for _, field := range stmt.Schema.Fields {
		if subset && field.DBName != "delivery_mode" {
			continue
		}
		column, exists := byName[field.DBName]
		if !exists {
			return fmt.Errorf("missing API-Key delivery column")
		}
		pk, known := column.PrimaryKey()
		if !known || pk != field.PrimaryKey {
			return fmt.Errorf("invalid API-Key delivery primary key")
		}
		nullable, known := column.Nullable()
		expectedNullable := field.FieldType.Kind() == reflect.Pointer
		if !known || nullable != expectedNullable {
			return fmt.Errorf("invalid API-Key delivery nullability")
		}
		value, hasDefault := column.DefaultValue()
		if field.HasDefaultValue {
			if !hasDefault || value != field.DefaultValue {
				return fmt.Errorf("invalid API-Key delivery default")
			}
		} else if hasDefault && (!expectedNullable || !strings.EqualFold(strings.TrimSpace(value), "NULL")) {
			return fmt.Errorf("invalid API-Key delivery default")
		}
		kind := strings.ToLower(column.DatabaseTypeName())
		switch string(field.DataType) {
		case "string":
			if field.Size > 0 {
				length, known := column.Length()
				if !known || length != int64(field.Size) || kind != "varchar" && kind != "character varying" {
					return fmt.Errorf("invalid API-Key delivery string")
				}
			} else if kind != "text" {
				return fmt.Errorf("invalid API-Key delivery text")
			}
		case "text":
			if kind != "text" {
				return fmt.Errorf("invalid API-Key delivery text")
			}
		case "time":
			precision, _, known := column.DecimalSize()
			if !known || precision != 6 || kind != "timestamp" && kind != "timestamptz" && kind != "datetime" {
				return fmt.Errorf("invalid API-Key delivery timestamp")
			}
		case "bool":
			if kind != "bool" && kind != "boolean" && kind != "tinyint" {
				return fmt.Errorf("invalid API-Key delivery boolean")
			}
		case "int", "uint":
			if kind != "int8" && kind != "bigint" {
				return fmt.Errorf("invalid API-Key delivery integer")
			}
		default:
			return fmt.Errorf("invalid API-Key delivery field")
		}
	}
	return nil
}
