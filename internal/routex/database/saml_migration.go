package database

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// samlProviderV97 contains public trust configuration, never a private signing key.
type samlProviderV97 struct {
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

func (samlProviderV97) TableName() string { return "saml_providers" }

// Subjects and ceremony proofs are never public entity projections.
type samlBindingV97 struct {
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

func (samlBindingV97) TableName() string { return "saml_bindings" }

type samlCeremonyV97 struct {
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

func (samlCeremonyV97) TableName() string { return "saml_ceremonies" }

// Replay receipts survive provider changes until the signed proof is expired.
type samlAssertionReceiptV97 struct {
	Digest    string    `gorm:"primaryKey;size:64" json:"-"`
	ExpiresAt time.Time `gorm:"precision:6;not null;index"`
	CreatedAt time.Time `gorm:"precision:6;not null"`
}

func (samlAssertionReceiptV97) TableName() string { return "saml_assertion_receipts" }

type samlSessionV97 struct {
	SAMLBindingID        string     `gorm:"column:saml_binding_id;size:30;not null;default:''"`
	SAMLBindingCreatedAt *time.Time `gorm:"column:saml_binding_created_at;precision:6"`
	SAMLConfigRevision   string     `gorm:"column:saml_config_revision;size:64;not null;default:''"`
	SAMLPolicyRevision   string     `gorm:"column:saml_policy_revision;size:64;not null;default:''"`
	SAMLUserCreatedAt    *time.Time `gorm:"column:saml_user_created_at;precision:6"`
}

func (samlSessionV97) TableName() string { return "sessions" }

type samlMFAChallengeV97 samlSessionV97

func (samlMFAChallengeV97) TableName() string { return "mfa_challenges" }

type samlSessionProofV97 struct {
	PrimaryMethod string `gorm:"check:ck_sessions_oidc_primary,(OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 108 AND ASCII(SUBSTRING(primary_method,2,1)) = 100 AND ASCII(SUBSTRING(primary_method,3,1)) = 97 AND ASCII(SUBSTRING(primary_method,4,1)) = 112 AND CHAR_LENGTH(ldap_binding_id) > 0 AND ldap_binding_created_at IS NOT NULL AND CHAR_LENGTH(ldap_config_revision) = 64 AND CHAR_LENGTH(ldap_policy_revision) = 64 AND ldap_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 115 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 109 AND ASCII(SUBSTRING(primary_method,4,1)) = 108 AND CHAR_LENGTH(saml_binding_id) > 0 AND saml_binding_created_at IS NOT NULL AND CHAR_LENGTH(saml_config_revision) = 64 AND CHAR_LENGTH(saml_policy_revision) = 64 AND saml_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL)"`
}

func (samlSessionProofV97) TableName() string { return "sessions" }

type samlMFAProofV97 struct {
	PrimaryMethod string `gorm:"check:ck_mfa_challenges_oidc_primary,(OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 108 AND ASCII(SUBSTRING(primary_method,2,1)) = 100 AND ASCII(SUBSTRING(primary_method,3,1)) = 97 AND ASCII(SUBSTRING(primary_method,4,1)) = 112 AND CHAR_LENGTH(ldap_binding_id) > 0 AND ldap_binding_created_at IS NOT NULL AND CHAR_LENGTH(ldap_config_revision) = 64 AND CHAR_LENGTH(ldap_policy_revision) = 64 AND ldap_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 115 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 109 AND ASCII(SUBSTRING(primary_method,4,1)) = 108 AND CHAR_LENGTH(saml_binding_id) > 0 AND saml_binding_created_at IS NOT NULL AND CHAR_LENGTH(saml_config_revision) = 64 AND CHAR_LENGTH(saml_policy_revision) = 64 AND saml_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL)"`
}

func (samlMFAProofV97) TableName() string { return "mfa_challenges" }

// V97 uses frozen GORM schemas; public SAML trust introduces no root-key domain.
func samlMigration(db *gorm.DB) error {
	for _, m := range []any{&samlSessionV97{}, &samlMFAChallengeV97{}} {
		for _, f := range []string{"SAMLBindingID", "SAMLBindingCreatedAt", "SAMLConfigRevision", "SAMLPolicyRevision", "SAMLUserCreatedAt"} {
			if !db.Migrator().HasColumn(m, f) {
				if e := db.Migrator().AddColumn(m, f); e != nil {
					return e
				}
			}
		}
	}
	models := []any{&samlProviderV97{}, &samlBindingV97{}, &samlCeremonyV97{}, &samlAssertionReceiptV97{}}
	if e := migrateTables(db, models...); e != nil {
		return e
	}
	for _, m := range append(models, &samlSessionV97{}, &samlMFAChallengeV97{}) {
		if e := validateSAMLColumnsV97(db, m); e != nil {
			return e
		}
	}
	for _, m := range models {
		if e := validateSAMLIndexesV97(db, m); e != nil {
			return e
		}
	}
	checks := []struct {
		model any
		name  string
	}{{&samlSessionProofV97{}, "ck_sessions_oidc_primary"}, {&samlMFAProofV97{}, "ck_mfa_challenges_oidc_primary"}}
	// Validate all retained facts before replacing released CHECK names. MySQL
	// partial DDL is replayable without relying on transactional DDL rollback.
	for _, v := range checks {
		st := &gorm.Statement{DB: db}
		if e := st.Parse(v.model); e != nil {
			return e
		}
		c, ok := st.Schema.ParseCheckConstraints()[v.name]
		if !ok {
			return fmt.Errorf("missing frozen SAML primary constraint")
		}
		var bad int64
		if e := db.Table(st.Schema.Table).Where("NOT (" + c.Constraint + ")").Count(&bad).Error; e != nil {
			return e
		}
		if bad != 0 {
			return fmt.Errorf("invalid retained SAML primary provenance")
		}
	}
	for _, v := range checks {
		if db.Migrator().HasConstraint(v.model, v.name) {
			if e := db.Migrator().DropConstraint(v.model, v.name); e != nil {
				return e
			}
		}
		if e := db.Migrator().CreateConstraint(v.model, v.name); e != nil {
			return e
		}
	}
	initial := samlProviderV97{ID: "saml", ReviewRevision: strings.Repeat("0", 64), ConfigRevision: strings.Repeat("0", 64), PolicyRevision: strings.Repeat("0", 64)}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error
}
func validateSAMLColumnsV97(db *gorm.DB, model any) error {
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
			return fmt.Errorf("missing SAML column %s", f.DBName)
		}
		if f.PrimaryKey {
			primary, known := c.PrimaryKey()
			if !known || !primary {
				return fmt.Errorf("invalid SAML primary key %s", f.DBName)
			}
		}
		nullable, known := c.Nullable()
		if !known || (f.NotNull || f.PrimaryKey) && nullable || f.FieldType.Kind() == reflect.Pointer && !nullable {
			return fmt.Errorf("invalid SAML nullability %s", f.DBName)
		}
		kind := strings.ToLower(c.DatabaseTypeName())
		switch string(f.DataType) {
		case "time":
			value, hasDefault := c.DefaultValue()
			if hasDefault && value != "" && !strings.EqualFold(strings.TrimSpace(value), "NULL") {
				return fmt.Errorf("invalid SAML timestamp default %s", f.DBName)
			}
			precision, _, sized := c.DecimalSize()
			if !sized || precision != 6 || kind != "timestamp" && kind != "timestamptz" && kind != "datetime" {
				return fmt.Errorf("invalid SAML timestamp %s", f.DBName)
			}
		case "bool":
			if kind != "bool" && kind != "boolean" && kind != "tinyint" {
				return fmt.Errorf("invalid SAML boolean %s", f.DBName)
			}
		case "string":
			if f.Size > 0 && kind != "varchar" && kind != "character varying" {
				return fmt.Errorf("invalid SAML text kind %s", f.DBName)
			}
		case "text":
			if kind != "text" {
				return fmt.Errorf("invalid SAML text kind %s", f.DBName)
			}
		}
		if string(f.DataType) == "string" && f.Size > 0 {
			n, sized := c.Length()
			if !sized || n != int64(f.Size) {
				return fmt.Errorf("invalid SAML size %s", f.DBName)
			}
		}
		if !f.HasDefaultValue && string(f.DataType) != "time" {
			value, present := c.DefaultValue()
			if present && value != "" && !strings.EqualFold(strings.TrimSpace(value), "NULL") {
				return fmt.Errorf("invalid SAML unexpected default %s", f.DBName)
			}
		}
		if f.HasDefaultValue && f.DefaultValue == "" {
			value, present := c.DefaultValue()
			if !present || value != "" && value != "''" && value != "''::character varying" {
				return fmt.Errorf("invalid SAML default %s", f.DBName)
			}
		}
	}
	return nil
}

func validateSAMLIndexesV97(db *gorm.DB, model any) error {
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
			return fmt.Errorf("invalid SAML index %s", index.Name)
		}
		unique := index.Class == "UNIQUE"
		for i, column := range columns {
			if !column.ColumnName.Valid || column.ColumnName.String != index.Fields[i].DBName || !column.Position.Valid || column.Position.Int64 != int64(i+1) || !column.IsUnique.Valid || column.IsUnique.Bool != unique || column.PrefixLength.Valid {
				return fmt.Errorf("invalid SAML index %s", index.Name)
			}
		}
	}
	return nil
}
