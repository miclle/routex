package database

import (
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reflect"
	"strings"
	"time"
)

// V95 private frozen schemas; no current entities are migrated at startup.
// oauthProviderV95 is the singleton custom OAuth login configuration. Secrets and
// protocol proofs are never exposed through the configuration response.
type oauthProviderV95 struct {
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

func (oauthProviderV95) TableName() string { return "oauth_providers" }

// oauthBindingV95 preserves the provider namespace and exact subject kind.
type oauthBindingV95 struct {
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

func (oauthBindingV95) TableName() string { return "oauth_bindings" }

// oauthCeremonyV95 stores only bounded hashes and typed provenance, never remote tokens.
type oauthCeremonyV95 struct {
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

func (oauthCeremonyV95) TableName() string { return "oauth_ceremonies" }

type oauthSessionV95 struct {
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

func (oauthSessionV95) TableName() string { return "sessions" }

type oauthMFAChallengeV95 struct {
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

func (oauthMFAChallengeV95) TableName() string { return "mfa_challenges" }

type oauthSessionProofV95 struct {
	PrimaryMethod string `gorm:"check:ck_sessions_oidc_primary,(OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL)"`
}

func (oauthSessionProofV95) TableName() string { return "sessions" }

type oauthMFAProofV95 struct {
	PrimaryMethod string `gorm:"check:ck_mfa_challenges_oidc_primary,(OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL)"`
}

func (oauthMFAProofV95) TableName() string { return "mfa_challenges" }

type oauthRootJobV95 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4"`
	Domain           int `gorm:"not null;check:ck_secret_rotation_domain,(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7) OR (inventory_version = 3 AND domain >= 0 AND domain <= 8) OR (inventory_version = 4 AND domain >= 0 AND domain <= 9)"`
}

func (oauthRootJobV95) TableName() string { return "secret_rotation_jobs" }

type oauthRootItemV95 struct {
	Domain string `gorm:"primaryKey;size:32;check:ck_secret_item_domain,((((CHAR_LENGTH(domain) = 20 AND ASCII(SUBSTRING(domain,1,1)) = 112 AND ASCII(SUBSTRING(domain,2,1)) = 114 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 118 AND ASCII(SUBSTRING(domain,5,1)) = 105 AND ASCII(SUBSTRING(domain,6,1)) = 100 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 95 AND ASCII(SUBSTRING(domain,10,1)) = 99 AND ASCII(SUBSTRING(domain,11,1)) = 114 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 100 AND ASCII(SUBSTRING(domain,14,1)) = 101 AND ASCII(SUBSTRING(domain,15,1)) = 110 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 105 AND ASCII(SUBSTRING(domain,18,1)) = 97 AND ASCII(SUBSTRING(domain,19,1)) = 108 AND ASCII(SUBSTRING(domain,20,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 101 AND ASCII(SUBSTRING(domain,2,1)) = 103 AND ASCII(SUBSTRING(domain,3,1)) = 114 AND ASCII(SUBSTRING(domain,4,1)) = 101 AND ASCII(SUBSTRING(domain,5,1)) = 115 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 115) OR (CHAR_LENGTH(domain) = 13 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 109 AND ASCII(SUBSTRING(domain,3,1)) = 116 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 116 AND ASCII(SUBSTRING(domain,9,1)) = 116 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 110 AND ASCII(SUBSTRING(domain,12,1)) = 103 AND ASCII(SUBSTRING(domain,13,1)) = 115) OR (CHAR_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 116 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 97 AND ASCII(SUBSTRING(domain,6,1)) = 103 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 95 AND ASCII(SUBSTRING(domain,9,1)) = 114 AND ASCII(SUBSTRING(domain,10,1)) = 101 AND ASCII(SUBSTRING(domain,11,1)) = 118 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 115 AND ASCII(SUBSTRING(domain,14,1)) = 105 AND ASCII(SUBSTRING(domain,15,1)) = 111 AND ASCII(SUBSTRING(domain,16,1)) = 110 AND ASCII(SUBSTRING(domain,17,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 117 AND ASCII(SUBSTRING(domain,2,1)) = 115 AND ASCII(SUBSTRING(domain,3,1)) = 101 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 109 AND ASCII(SUBSTRING(domain,7,1)) = 102 AND ASCII(SUBSTRING(domain,8,1)) = 97)) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 119 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 105 AND ASCII(SUBSTRING(domain,10,1)) = 116 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 101 AND ASCII(SUBSTRING(domain,9,1)) = 97 AND ASCII(SUBSTRING(domain,10,1)) = 100 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104))) OR (OCTET_LENGTH(domain) = 14 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 105 AND ASCII(SUBSTRING(domain,3,1)) = 100 AND ASCII(SUBSTRING(domain,4,1)) = 99 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 112 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 111 AND ASCII(SUBSTRING(domain,9,1)) = 118 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 100 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 114 AND ASCII(SUBSTRING(domain,14,1)) = 115) OR (OCTET_LENGTH(domain) = 15 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 116 AND ASCII(SUBSTRING(domain,5,1)) = 104 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 112 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 111 AND ASCII(SUBSTRING(domain,10,1)) = 118 AND ASCII(SUBSTRING(domain,11,1)) = 105 AND ASCII(SUBSTRING(domain,12,1)) = 100 AND ASCII(SUBSTRING(domain,13,1)) = 101 AND ASCII(SUBSTRING(domain,14,1)) = 114 AND ASCII(SUBSTRING(domain,15,1)) = 115)"`
}

func (oauthRootItemV95) TableName() string { return "secret_rotation_items" }

type oauthRootProcessV95 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_process_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4"`
}

func (oauthRootProcessV95) TableName() string { return "secret_process_verifications" }

// oauthMigration is append-only V95. Released V94 and its local/OIDC
// constraints stay immutable; the ledger makes this the sole superseding step.
func oauthMigration(db *gorm.DB) error {
	for _, model := range []any{&oauthSessionV95{}, &oauthMFAChallengeV95{}} {
		for _, field := range []string{"OAuthBindingID", "OAuthBindingCreatedAt", "OAuthConfigRevision", "OAuthPolicyRevision", "OAuthUserCreatedAt"} {
			if !db.Migrator().HasColumn(model, field) {
				if err := db.Migrator().AddColumn(model, field); err != nil {
					return err
				}
			}
		}
	}
	models := []any{&oauthProviderV95{}, &oauthBindingV95{}, &oauthCeremonyV95{}}
	if err := migrateTables(db, models...); err != nil {
		return err
	}
	for _, model := range append(models, &oauthSessionV95{}, &oauthMFAChallengeV95{}) {
		if err := validateOAuthColumnsV95(db, model); err != nil {
			return err
		}
	}
	for _, model := range models {
		if err := validateOAuthIndexesV95(db, model); err != nil {
			return err
		}
	}
	if err := oauthPrimaryConstraintsV95(db); err != nil {
		return err
	}
	if err := oauthRootInventoryV95(db); err != nil {
		return err
	}
	initial := oauthProviderV95{ID: "oauth", ReviewRevision: strings.Repeat("0", 64), ConfigRevision: strings.Repeat("0", 64), PolicyRevision: strings.Repeat("0", 64), SecretGeneration: "0", ScopesJSON: "[]", SubjectPathJSON: "[]"}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error
}

// Validate retained facts before replacing either historical named CHECK. A
// partially applied MySQL DROP/CREATE is retried under the migration lock, and
// invalid rows never acquire a V95 ledger entry.
func oauthPrimaryConstraintsV95(db *gorm.DB) error {
	for _, item := range []struct {
		model any
		name  string
	}{
		{&oauthSessionProofV95{}, "ck_sessions_oidc_primary"},
		{&oauthMFAProofV95{}, "ck_mfa_challenges_oidc_primary"},
	} {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(item.model); err != nil {
			return err
		}
		check, ok := stmt.Schema.ParseCheckConstraints()[item.name]
		if !ok {
			return fmt.Errorf("missing frozen OAuth primary constraint")
		}
		var bad int64
		if err := db.Table(stmt.Schema.Table).Where("NOT (" + check.Constraint + ")").Count(&bad).Error; err != nil {
			return err
		}
		if bad != 0 {
			return fmt.Errorf("invalid retained OAuth primary provenance")
		}
	}
	return oauthReplaceChecksV95(db, []oauthCheckV95{
		{&oauthSessionProofV95{}, "ck_sessions_oidc_primary"},
		{&oauthMFAProofV95{}, "ck_mfa_challenges_oidc_primary"},
	})
}

type oauthCheckV95 struct {
	model any
	name  string
}

func oauthReplaceChecksV95(db *gorm.DB, checks []oauthCheckV95) error {
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

func oauthRootInventoryV95(db *gorm.DB) error {
	var bad int64
	if err := db.Table("secret_rotation_jobs").Where("inventory_version NOT IN ? OR inventory_version IS NULL OR domain IS NULL OR domain < 0 OR (inventory_version = ? AND domain > ?) OR (inventory_version = ? AND domain > ?) OR (inventory_version = ? AND domain > ?) OR domain > ?", []int{1, 2, 3, 4}, 1, 5, 2, 7, 3, 8, 9).Count(&bad).Error; err != nil {
		return err
	}
	if bad != 0 {
		return fmt.Errorf("invalid retained OAuth root job scope")
	}
	if err := db.Table("secret_process_verifications").Where("inventory_version NOT IN ? OR inventory_version IS NULL", []int{1, 2, 3, 4}).Count(&bad).Error; err != nil {
		return err
	}
	if bad != 0 {
		return fmt.Errorf("invalid retained OAuth root proof scope")
	}
	return oauthReplaceChecksV95(db, []oauthCheckV95{
		{&oauthRootJobV95{}, "ck_secret_inventory_version"}, {&oauthRootJobV95{}, "ck_secret_rotation_domain"},
		{&oauthRootItemV95{}, "ck_secret_item_domain"}, {&oauthRootProcessV95{}, "ck_secret_process_inventory_version"},
	})
}

func validateOAuthColumnsV95(db *gorm.DB, model any) error {
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
			return fmt.Errorf("missing OAuth column %s", f.DBName)
		}
		if f.PrimaryKey {
			primary, known := c.PrimaryKey()
			if !known || !primary {
				return fmt.Errorf("invalid OAuth primary key %s", f.DBName)
			}
		}
		nullable, known := c.Nullable()
		if !known || (f.NotNull || f.PrimaryKey) && nullable || f.FieldType.Kind() == reflect.Pointer && !nullable {
			return fmt.Errorf("invalid OAuth nullability %s", f.DBName)
		}
		kind := strings.ToLower(c.DatabaseTypeName())
		switch string(f.DataType) {
		case "time":
			value, hasDefault := c.DefaultValue()
			if hasDefault && value != "" && !strings.EqualFold(strings.TrimSpace(value), "NULL") {
				return fmt.Errorf("invalid OAuth timestamp default %s", f.DBName)
			}
			precision, _, sized := c.DecimalSize()
			if !sized || precision != 6 || kind != "timestamp" && kind != "timestamptz" && kind != "datetime" {
				return fmt.Errorf("invalid OAuth timestamp %s", f.DBName)
			}
		case "bool":
			if kind != "bool" && kind != "boolean" && kind != "tinyint" {
				return fmt.Errorf("invalid OAuth boolean %s", f.DBName)
			}
		case "string":
			if f.Size > 0 && kind != "varchar" && kind != "character varying" {
				return fmt.Errorf("invalid OAuth text kind %s", f.DBName)
			}
		case "text":
			if kind != "text" {
				return fmt.Errorf("invalid OAuth ciphertext kind %s", f.DBName)
			}
		}
		if string(f.DataType) == "string" && f.Size > 0 {
			n, sized := c.Length()
			if !sized || n != int64(f.Size) {
				return fmt.Errorf("invalid OAuth size %s", f.DBName)
			}
		}
		if f.HasDefaultValue && f.DefaultValue == "" {
			value, present := c.DefaultValue()
			if !present || value != "" && value != "''" && value != "''::character varying" {
				return fmt.Errorf("invalid OAuth default %s", f.DBName)
			}
		}
	}
	return nil
}

func validateOAuthIndexesV95(db *gorm.DB, model any) error {
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
			return fmt.Errorf("invalid OAuth index %s", index.Name)
		}
		unique := index.Class == "UNIQUE"
		for i, column := range columns {
			if !column.ColumnName.Valid || column.ColumnName.String != index.Fields[i].DBName || !column.Position.Valid || column.Position.Int64 != int64(i+1) || !column.IsUnique.Valid || column.IsUnique.Bool != unique || column.PrefixLength.Valid {
				return fmt.Errorf("invalid OAuth index %s", index.Name)
			}
		}
	}
	return nil
}
