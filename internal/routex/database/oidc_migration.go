package database

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// V94 freezes the first existing-member OIDC schema. It does not change local
// passwords, registration admission, MFA enrollment, roles or model grants.
// oidcProviderV94 is the singleton enterprise login configuration. Secrets and
// protocol proofs are never exposed through the configuration response.
type oidcProviderV94 struct {
	ID                       string     `gorm:"primaryKey;size:30;check:ck_oidc_singleton,CHAR_LENGTH(id) = 4 AND ASCII(SUBSTRING(id,1,1)) = 111 AND ASCII(SUBSTRING(id,2,1)) = 105 AND ASCII(SUBSTRING(id,3,1)) = 100 AND ASCII(SUBSTRING(id,4,1)) = 99"`
	ReviewRevision           string     `gorm:"size:64;not null"`
	ConfigRevision           string     `gorm:"size:64;not null"`
	PolicyRevision           string     `gorm:"size:64;not null"`
	Name                     string     `gorm:"size:100;not null"`
	Issuer                   string     `gorm:"size:2048;not null"`
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

func (oidcProviderV94) TableName() string { return "oidc_providers" }

// oidcBindingV94 binds an exact issuer subject to one existing admitted local
// identity. The unique digest preserves byte-sensitive subject identity on
// databases whose default string collation is case-insensitive.
type oidcBindingV94 struct {
	ID             string    `gorm:"primaryKey;size:30"`
	UserID         string    `gorm:"size:30;not null;uniqueIndex"`
	UserCreatedAt  time.Time `gorm:"precision:6;not null"`
	ConfigRevision string    `gorm:"size:64;not null"`
	Subject        string    `gorm:"size:255;not null"`
	SubjectDigest  string    `gorm:"size:64;not null;uniqueIndex"`
	CreatedAt      time.Time `gorm:"precision:6;not null"`
}

func (oidcBindingV94) TableName() string { return "oidc_bindings" }

// oidcCeremonyV94 retains only browser/state digests and bounded authentication
// provenance. PKCE and nonce are derived separately from the browser cookie;
// neither that cookie nor remote tokens or codes are persisted.
type oidcCeremonyV94 struct {
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
	Subject          string     `gorm:"size:255;not null"`
	BindingID        string     `gorm:"size:30;not null"`
	BindingCreatedAt *time.Time `gorm:"precision:6"`
	ExpiresAt        time.Time  `gorm:"precision:6;not null;index"`
	CreatedAt        time.Time  `gorm:"precision:6;not null"`
	VerifiedAt       *time.Time `gorm:"precision:6"`
}

func (oidcCeremonyV94) TableName() string { return "oidc_ceremonies" }

type oidcSessionV94 struct {
	PrimaryMethod        string     `gorm:"size:20;not null;default:''"`
	OIDCBindingID        string     `gorm:"column:oidc_binding_id;size:30;not null;default:''"`
	OIDCBindingCreatedAt *time.Time `gorm:"column:oidc_binding_created_at;precision:6"`
	OIDCConfigRevision   string     `gorm:"column:oidc_config_revision;size:64;not null;default:''"`
	OIDCPolicyRevision   string     `gorm:"column:oidc_policy_revision;size:64;not null;default:''"`
	OIDCUserCreatedAt    *time.Time `gorm:"column:oidc_user_created_at;precision:6"`
}

func (oidcSessionV94) TableName() string { return "sessions" }

type oidcMFAChallengeV94 struct {
	PrimaryMethod        string     `gorm:"size:20;not null;default:''"`
	OIDCBindingID        string     `gorm:"column:oidc_binding_id;size:30;not null;default:''"`
	OIDCBindingCreatedAt *time.Time `gorm:"column:oidc_binding_created_at;precision:6"`
	OIDCConfigRevision   string     `gorm:"column:oidc_config_revision;size:64;not null;default:''"`
	OIDCPolicyRevision   string     `gorm:"column:oidc_policy_revision;size:64;not null;default:''"`
	OIDCUserCreatedAt    *time.Time `gorm:"column:oidc_user_created_at;precision:6"`
}

func (oidcMFAChallengeV94) TableName() string { return "mfa_challenges" }

type oidcSessionProofV94 struct {
	PrimaryMethod string `gorm:"check:ck_sessions_oidc_primary,(primary_method = '' AND oidc_binding_id = '' AND oidc_binding_created_at IS NULL AND oidc_config_revision = '' AND oidc_policy_revision = '' AND oidc_user_created_at IS NULL) OR (CHAR_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL)"`
}

func (oidcSessionProofV94) TableName() string { return "sessions" }

type oidcMFAProofV94 struct {
	PrimaryMethod string `gorm:"check:ck_mfa_challenges_oidc_primary,(primary_method = '' AND oidc_binding_id = '' AND oidc_binding_created_at IS NULL AND oidc_config_revision = '' AND oidc_policy_revision = '' AND oidc_user_created_at IS NULL) OR (CHAR_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL)"`
}

func (oidcMFAProofV94) TableName() string { return "mfa_challenges" }

func oidcMigration(db *gorm.DB) error {
	for _, model := range []any{&oidcSessionV94{}, &oidcMFAChallengeV94{}} {
		for _, field := range []string{"PrimaryMethod", "OIDCBindingID", "OIDCBindingCreatedAt", "OIDCConfigRevision", "OIDCPolicyRevision", "OIDCUserCreatedAt"} {
			if !db.Migrator().HasColumn(model, field) {
				if err := db.Migrator().AddColumn(model, field); err != nil {
					return err
				}
			}
		}
	}
	models := []any{&oidcProviderV94{}, &oidcBindingV94{}, &oidcCeremonyV94{}}
	if err := migrateTables(db, models...); err != nil {
		return err
	}
	// Validate every frozen column after resumable DDL; an incompatible existing
	// column must not silently acquire an accepted migration ledger entry.
	for _, model := range append(models, &oidcSessionV94{}, &oidcMFAChallengeV94{}) {
		if err := validateOIDCColumnsV94(db, model); err != nil {
			return err
		}
	}
	for _, item := range []struct {
		model any
		name  string
	}{{&oidcSessionProofV94{}, "ck_sessions_oidc_primary"}, {&oidcMFAProofV94{}, "ck_mfa_challenges_oidc_primary"}} {
		if !db.Migrator().HasConstraint(item.model, item.name) {
			if err := db.Migrator().CreateConstraint(item.model, item.name); err != nil {
				return err
			}
		}
	}
	for _, model := range models {
		if err := validateOIDCIndexesV94(db, model); err != nil {
			return err
		}
	}
	if err := oidcRootInventoryV94(db); err != nil {
		return err
	}
	initial := oidcProviderV94{ID: "oidc", ConfigRevision: strings.Repeat("0", 64), PolicyRevision: strings.Repeat("0", 64), ReviewRevision: strings.Repeat("0", 64), SecretGeneration: "0"}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error
}

func validateOIDCColumnsV94(db *gorm.DB, model any) error {
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
			return fmt.Errorf("missing OIDC column %s", f.DBName)
		}
		if f.PrimaryKey {
			primary, known := c.PrimaryKey()
			if !known || !primary {
				return fmt.Errorf("invalid OIDC primary key %s", f.DBName)
			}
		}
		nullable, known := c.Nullable()
		if !known || (f.NotNull || f.PrimaryKey) && nullable || f.FieldType.Kind() == reflect.Pointer && !nullable {
			return fmt.Errorf("invalid OIDC nullability %s", f.DBName)
		}
		kind := strings.ToLower(c.DatabaseTypeName())
		switch string(f.DataType) {
		case "time":
			value, hasDefault := c.DefaultValue()
			if hasDefault && value != "" && !strings.EqualFold(strings.TrimSpace(value), "NULL") {
				return fmt.Errorf("invalid OIDC timestamp default %s", f.DBName)
			}
			precision, _, sized := c.DecimalSize()
			if !sized || precision != 6 || kind != "timestamp" && kind != "timestamptz" && kind != "datetime" {
				return fmt.Errorf("invalid OIDC timestamp %s", f.DBName)
			}
		case "bool":
			if kind != "bool" && kind != "boolean" && kind != "tinyint" {
				return fmt.Errorf("invalid OIDC boolean %s", f.DBName)
			}
		case "string":
			if f.Size > 0 && kind != "varchar" && kind != "character varying" {
				return fmt.Errorf("invalid OIDC text kind %s", f.DBName)
			}
		case "text":
			if kind != "text" {
				return fmt.Errorf("invalid OIDC ciphertext kind %s", f.DBName)
			}
		}
		if string(f.DataType) == "string" && f.Size > 0 {
			n, sized := c.Length()
			if !sized || n != int64(f.Size) {
				return fmt.Errorf("invalid OIDC size %s", f.DBName)
			}
		}
		if f.HasDefaultValue && f.DefaultValue == "" {
			value, present := c.DefaultValue()
			if !present || value != "" && value != "''" && value != "''::character varying" {
				return fmt.Errorf("invalid OIDC default %s", f.DBName)
			}
		}
	}
	return nil
}

type oidcRootJobV94 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3"`
	Domain           int `gorm:"not null;check:ck_secret_rotation_domain,(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7) OR (inventory_version = 3 AND domain >= 0 AND domain <= 8)"`
}

func (oidcRootJobV94) TableName() string { return "secret_rotation_jobs" }

type oidcRootItemV94 struct {
	Domain string `gorm:"primaryKey;size:32;check:ck_secret_item_domain,((((CHAR_LENGTH(domain) = 20 AND ASCII(SUBSTRING(domain,1,1)) = 112 AND ASCII(SUBSTRING(domain,2,1)) = 114 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 118 AND ASCII(SUBSTRING(domain,5,1)) = 105 AND ASCII(SUBSTRING(domain,6,1)) = 100 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 95 AND ASCII(SUBSTRING(domain,10,1)) = 99 AND ASCII(SUBSTRING(domain,11,1)) = 114 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 100 AND ASCII(SUBSTRING(domain,14,1)) = 101 AND ASCII(SUBSTRING(domain,15,1)) = 110 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 105 AND ASCII(SUBSTRING(domain,18,1)) = 97 AND ASCII(SUBSTRING(domain,19,1)) = 108 AND ASCII(SUBSTRING(domain,20,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 101 AND ASCII(SUBSTRING(domain,2,1)) = 103 AND ASCII(SUBSTRING(domain,3,1)) = 114 AND ASCII(SUBSTRING(domain,4,1)) = 101 AND ASCII(SUBSTRING(domain,5,1)) = 115 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 115) OR (CHAR_LENGTH(domain) = 13 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 109 AND ASCII(SUBSTRING(domain,3,1)) = 116 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 116 AND ASCII(SUBSTRING(domain,9,1)) = 116 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 110 AND ASCII(SUBSTRING(domain,12,1)) = 103 AND ASCII(SUBSTRING(domain,13,1)) = 115) OR (CHAR_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 116 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 97 AND ASCII(SUBSTRING(domain,6,1)) = 103 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 95 AND ASCII(SUBSTRING(domain,9,1)) = 114 AND ASCII(SUBSTRING(domain,10,1)) = 101 AND ASCII(SUBSTRING(domain,11,1)) = 118 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 115 AND ASCII(SUBSTRING(domain,14,1)) = 105 AND ASCII(SUBSTRING(domain,15,1)) = 111 AND ASCII(SUBSTRING(domain,16,1)) = 110 AND ASCII(SUBSTRING(domain,17,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 117 AND ASCII(SUBSTRING(domain,2,1)) = 115 AND ASCII(SUBSTRING(domain,3,1)) = 101 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 109 AND ASCII(SUBSTRING(domain,7,1)) = 102 AND ASCII(SUBSTRING(domain,8,1)) = 97)) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 119 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 105 AND ASCII(SUBSTRING(domain,10,1)) = 116 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 101 AND ASCII(SUBSTRING(domain,9,1)) = 97 AND ASCII(SUBSTRING(domain,10,1)) = 100 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104))) OR (OCTET_LENGTH(domain) = 14 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 105 AND ASCII(SUBSTRING(domain,3,1)) = 100 AND ASCII(SUBSTRING(domain,4,1)) = 99 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 112 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 111 AND ASCII(SUBSTRING(domain,9,1)) = 118 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 100 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 114 AND ASCII(SUBSTRING(domain,14,1)) = 115)"`
}

func (oidcRootItemV94) TableName() string { return "secret_rotation_items" }

type oidcRootProcessV94 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_process_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3"`
}

func (oidcRootProcessV94) TableName() string { return "secret_process_verifications" }

// Preserve historical scopes while admitting the new encrypted domain. GORM
// creates portable CHECK expressions. Retained-row validation and the migration
// lock allow interrupted MySQL constraint replacements to resume safely.
func oidcRootInventoryV94(db *gorm.DB) error {
	var bad int64
	if err := db.Table("secret_rotation_jobs").Where("inventory_version NOT IN ? OR domain < 0 OR (inventory_version = ? AND domain > ?) OR (inventory_version = ? AND domain > ?) OR domain > ?", []int{1, 2, 3}, 1, 5, 2, 7, 8).Count(&bad).Error; err != nil {
		return err
	}
	if bad != 0 {
		return fmt.Errorf("invalid retained OIDC root job scope")
	}
	if err := db.Table("secret_process_verifications").Where("inventory_version NOT IN ? OR inventory_version IS NULL", []int{1, 2, 3}).Count(&bad).Error; err != nil {
		return err
	}
	if bad != 0 {
		return fmt.Errorf("invalid retained OIDC root proof scope")
	}
	for _, item := range []struct {
		model any
		name  string
	}{
		{&oidcRootJobV94{}, "ck_secret_inventory_version"}, {&oidcRootJobV94{}, "ck_secret_rotation_domain"},
		{&oidcRootItemV94{}, "ck_secret_item_domain"}, {&oidcRootProcessV94{}, "ck_secret_process_inventory_version"},
	} {
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

func validateOIDCIndexesV94(db *gorm.DB, model any) error {
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
			return fmt.Errorf("invalid OIDC index %s", index.Name)
		}
		unique := index.Class == "UNIQUE"
		for i, column := range columns {
			if !column.ColumnName.Valid || column.ColumnName.String != index.Fields[i].DBName || !column.Position.Valid || column.Position.Int64 != int64(i+1) || !column.IsUnique.Valid || column.IsUnique.Bool != unique || column.PrefixLength.Valid {
				return fmt.Errorf("invalid OIDC index %s", index.Name)
			}
		}
	}
	return nil
}
