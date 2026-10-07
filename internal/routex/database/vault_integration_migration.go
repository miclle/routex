package database

import (
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

// Vault configuration is independent from the active internal Provider store.
type vaultIntegrationV72 struct {
	ID            string    `gorm:"primaryKey;size:30"`
	Name          string    `gorm:"size:100;not null"`
	RevisionID    string    `gorm:"size:30;not null"`
	ActiveProbeID *string   `gorm:"size:32"`
	CreatedAt     time.Time `gorm:"precision:6;not null"`
	UpdatedAt     time.Time `gorm:"precision:6;not null"`
}
type vaultCatalogueV72 struct {
	ID         int    `gorm:"primaryKey;autoIncrement:false;check:ck_vault_catalogue_singleton,id = 1"`
	Generation string `gorm:"size:64;not null"`
}
type vaultRevisionV72 struct {
	ID               string    `gorm:"primaryKey;size:30"`
	IntegrationID    string    `gorm:"size:30;not null;index:idx_vault_revision_integration"`
	IntegrationBirth time.Time `gorm:"precision:6;not null"`
	Name             string    `gorm:"size:100;not null"`
	Endpoint         string    `gorm:"size:2048;not null"`
	Namespace        string    `gorm:"size:256;not null"`
	Mount            string    `gorm:"size:128;not null"`
	Prefix           string    `gorm:"size:256;not null"`
	DataField        string    `gorm:"size:64;not null"`
	CreatedAt        time.Time `gorm:"precision:6;not null"`
}

// Separate tables make each auth material an exact root-rotation inventory row.
type vaultWriterAuthV72 struct {
	ID               string `gorm:"primaryKey;size:30"`
	SecretGeneration string `gorm:"size:30;not null"`
	AuthCiphertext   string `gorm:"type:text;not null" json:"-"`
}
type vaultReaderAuthV72 struct {
	ID               string `gorm:"primaryKey;size:30"`
	SecretGeneration string `gorm:"size:30;not null"`
	AuthCiphertext   string `gorm:"type:text;not null" json:"-"`
}
type vaultConfigReceiptV72 struct {
	RequestID        string    `gorm:"primaryKey;size:36"`
	ActorID          string    `gorm:"size:30;not null"`
	ActorBirth       time.Time `gorm:"precision:6;not null"`
	IntegrationID    string    `gorm:"size:30;not null"`
	IntegrationBirth time.Time `gorm:"precision:6;not null"`
	RevisionID       string    `gorm:"size:30;not null"`
	ReviewETag       string    `gorm:"size:129;not null" json:"-"`
	IntentJSON       string    `gorm:"type:text;not null" json:"-"`
	Changed          bool      `gorm:"not null"`
	CreatedAt        time.Time `gorm:"precision:6;not null"`
}
type vaultProbeV72 struct {
	ID               string     `gorm:"primaryKey;size:32"`
	RequestID        string     `gorm:"size:36;not null;uniqueIndex:idx_vault_probe_write_request"`
	IntegrationID    string     `gorm:"size:30;not null;index:idx_vault_probe_integration"`
	IntegrationBirth time.Time  `gorm:"precision:6;not null"`
	RevisionID       string     `gorm:"size:30;not null"`
	ActorID          string     `gorm:"size:30;not null"`
	ActorBirth       time.Time  `gorm:"precision:6;not null"`
	ExpectedSHA256   string     `gorm:"size:64;not null" json:"-"`
	DescriptorSHA256 string     `gorm:"size:64;not null" json:"-"`
	State            string     `gorm:"size:24;not null;check:ck_vault_probe_state,(OCTET_LENGTH(state) = 7 AND ASCII(SUBSTRING(state,1,1)) = 112 AND ASCII(SUBSTRING(state,2,1)) = 108 AND ASCII(SUBSTRING(state,3,1)) = 97 AND ASCII(SUBSTRING(state,4,1)) = 110 AND ASCII(SUBSTRING(state,5,1)) = 110 AND ASCII(SUBSTRING(state,6,1)) = 101 AND ASCII(SUBSTRING(state,7,1)) = 100) OR (OCTET_LENGTH(state) = 7 AND ASCII(SUBSTRING(state,1,1)) = 119 AND ASCII(SUBSTRING(state,2,1)) = 114 AND ASCII(SUBSTRING(state,3,1)) = 105 AND ASCII(SUBSTRING(state,4,1)) = 116 AND ASCII(SUBSTRING(state,5,1)) = 105 AND ASCII(SUBSTRING(state,6,1)) = 110 AND ASCII(SUBSTRING(state,7,1)) = 103) OR (OCTET_LENGTH(state) = 13 AND ASCII(SUBSTRING(state,1,1)) = 97 AND ASCII(SUBSTRING(state,2,1)) = 119 AND ASCII(SUBSTRING(state,3,1)) = 97 AND ASCII(SUBSTRING(state,4,1)) = 105 AND ASCII(SUBSTRING(state,5,1)) = 116 AND ASCII(SUBSTRING(state,6,1)) = 105 AND ASCII(SUBSTRING(state,7,1)) = 110 AND ASCII(SUBSTRING(state,8,1)) = 103 AND ASCII(SUBSTRING(state,9,1)) = 95 AND ASCII(SUBSTRING(state,10,1)) = 114 AND ASCII(SUBSTRING(state,11,1)) = 101 AND ASCII(SUBSTRING(state,12,1)) = 97 AND ASCII(SUBSTRING(state,13,1)) = 100) OR (OCTET_LENGTH(state) = 7 AND ASCII(SUBSTRING(state,1,1)) = 114 AND ASCII(SUBSTRING(state,2,1)) = 101 AND ASCII(SUBSTRING(state,3,1)) = 97 AND ASCII(SUBSTRING(state,4,1)) = 100 AND ASCII(SUBSTRING(state,5,1)) = 105 AND ASCII(SUBSTRING(state,6,1)) = 110 AND ASCII(SUBSTRING(state,7,1)) = 103) OR (OCTET_LENGTH(state) = 15 AND ASCII(SUBSTRING(state,1,1)) = 99 AND ASCII(SUBSTRING(state,2,1)) = 108 AND ASCII(SUBSTRING(state,3,1)) = 101 AND ASCII(SUBSTRING(state,4,1)) = 97 AND ASCII(SUBSTRING(state,5,1)) = 110 AND ASCII(SUBSTRING(state,6,1)) = 117 AND ASCII(SUBSTRING(state,7,1)) = 112 AND ASCII(SUBSTRING(state,8,1)) = 95 AND ASCII(SUBSTRING(state,9,1)) = 112 AND ASCII(SUBSTRING(state,10,1)) = 101 AND ASCII(SUBSTRING(state,11,1)) = 110 AND ASCII(SUBSTRING(state,12,1)) = 100 AND ASCII(SUBSTRING(state,13,1)) = 105 AND ASCII(SUBSTRING(state,14,1)) = 110 AND ASCII(SUBSTRING(state,15,1)) = 103) OR (OCTET_LENGTH(state) = 9 AND ASCII(SUBSTRING(state,1,1)) = 99 AND ASCII(SUBSTRING(state,2,1)) = 111 AND ASCII(SUBSTRING(state,3,1)) = 109 AND ASCII(SUBSTRING(state,4,1)) = 112 AND ASCII(SUBSTRING(state,5,1)) = 108 AND ASCII(SUBSTRING(state,6,1)) = 101 AND ASCII(SUBSTRING(state,7,1)) = 116 AND ASCII(SUBSTRING(state,8,1)) = 101 AND ASCII(SUBSTRING(state,9,1)) = 100) OR (OCTET_LENGTH(state) = 11 AND ASCII(SUBSTRING(state,1,1)) = 105 AND ASCII(SUBSTRING(state,2,1)) = 110 AND ASCII(SUBSTRING(state,3,1)) = 116 AND ASCII(SUBSTRING(state,4,1)) = 101 AND ASCII(SUBSTRING(state,5,1)) = 114 AND ASCII(SUBSTRING(state,6,1)) = 114 AND ASCII(SUBSTRING(state,7,1)) = 117 AND ASCII(SUBSTRING(state,8,1)) = 112 AND ASCII(SUBSTRING(state,9,1)) = 116 AND ASCII(SUBSTRING(state,10,1)) = 101 AND ASCII(SUBSTRING(state,11,1)) = 100)"`
	Generation       string     `gorm:"size:64;not null"`
	Version          int64      `gorm:"not null;check:ck_vault_probe_version,version = 0 OR version = 1"`
	WriteJSON        string     `gorm:"type:text;not null" json:"-"`
	ReadJSON         string     `gorm:"type:text;not null" json:"-"`
	CleanupJSON      string     `gorm:"type:text;not null" json:"-"`
	CreatedAt        time.Time  `gorm:"precision:6;not null"`
	FinishedAt       *time.Time `gorm:"precision:6"`
}

// One receipt claims the entire finite command BEFORE any remote request.
type vaultProbeCommandV72 struct {
	RequestID     string     `gorm:"primaryKey;size:36"`
	ProbeID       string     `gorm:"size:32;not null;index:idx_vault_command_probe"`
	ActorID       string     `gorm:"size:30;not null"`
	ActorBirth    time.Time  `gorm:"precision:6;not null"`
	Kind          string     `gorm:"size:16;not null;check:ck_vault_command_kind,(OCTET_LENGTH(kind) = 5 AND ASCII(SUBSTRING(kind,1,1)) = 119 AND ASCII(SUBSTRING(kind,2,1)) = 114 AND ASCII(SUBSTRING(kind,3,1)) = 105 AND ASCII(SUBSTRING(kind,4,1)) = 116 AND ASCII(SUBSTRING(kind,5,1)) = 101) OR (OCTET_LENGTH(kind) = 4 AND ASCII(SUBSTRING(kind,1,1)) = 114 AND ASCII(SUBSTRING(kind,2,1)) = 101 AND ASCII(SUBSTRING(kind,3,1)) = 97 AND ASCII(SUBSTRING(kind,4,1)) = 100) OR (OCTET_LENGTH(kind) = 7 AND ASCII(SUBSTRING(kind,1,1)) = 99 AND ASCII(SUBSTRING(kind,2,1)) = 108 AND ASCII(SUBSTRING(kind,3,1)) = 101 AND ASCII(SUBSTRING(kind,4,1)) = 97 AND ASCII(SUBSTRING(kind,5,1)) = 110 AND ASCII(SUBSTRING(kind,6,1)) = 117 AND ASCII(SUBSTRING(kind,7,1)) = 112)"`
	ReviewETag    string     `gorm:"size:129;not null" json:"-"`
	Reason        string     `gorm:"type:text;not null" json:"-"`
	Claim         string     `gorm:"size:30;not null" json:"-"`
	ExpiresAt     time.Time  `gorm:"precision:6;not null"`
	ResultJSON    string     `gorm:"type:text;not null" json:"-"`
	OwnershipJSON string     `gorm:"type:text;not null" json:"-"`
	CreatedAt     time.Time  `gorm:"precision:6;not null"`
	FinishedAt    *time.Time `gorm:"precision:6"`
}

func (vaultIntegrationV72) TableName() string   { return "vault_integrations" }
func (vaultCatalogueV72) TableName() string     { return "vault_catalogues" }
func (vaultRevisionV72) TableName() string      { return "vault_revisions" }
func (vaultWriterAuthV72) TableName() string    { return "vault_writer_auth" }
func (vaultReaderAuthV72) TableName() string    { return "vault_reader_auth" }
func (vaultConfigReceiptV72) TableName() string { return "vault_config_receipts" }
func (vaultProbeV72) TableName() string         { return "vault_probes" }
func (vaultProbeCommandV72) TableName() string  { return "vault_probe_commands" }

// Frozen upgrade metadata keeps legacy five-domain jobs distinguishable from seven-domain jobs.
type vaultRootJobV72 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_inventory_version,inventory_version = 1 OR inventory_version = 2"`
	Domain           int `gorm:"not null;check:ck_secret_rotation_domain,(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7)"`
}

func (vaultRootJobV72) TableName() string { return "secret_rotation_jobs" }

type vaultRootProcessV72 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_process_inventory_version,inventory_version = 1 OR inventory_version = 2"`
}

func (vaultRootProcessV72) TableName() string { return "secret_process_verifications" }

type vaultRootItemV72 struct {
	Domain string `gorm:"primaryKey;size:32;check:ck_secret_item_domain,(((CHAR_LENGTH(domain) = 20 AND ASCII(SUBSTRING(domain,1,1)) = 112 AND ASCII(SUBSTRING(domain,2,1)) = 114 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 118 AND ASCII(SUBSTRING(domain,5,1)) = 105 AND ASCII(SUBSTRING(domain,6,1)) = 100 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 95 AND ASCII(SUBSTRING(domain,10,1)) = 99 AND ASCII(SUBSTRING(domain,11,1)) = 114 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 100 AND ASCII(SUBSTRING(domain,14,1)) = 101 AND ASCII(SUBSTRING(domain,15,1)) = 110 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 105 AND ASCII(SUBSTRING(domain,18,1)) = 97 AND ASCII(SUBSTRING(domain,19,1)) = 108 AND ASCII(SUBSTRING(domain,20,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 101 AND ASCII(SUBSTRING(domain,2,1)) = 103 AND ASCII(SUBSTRING(domain,3,1)) = 114 AND ASCII(SUBSTRING(domain,4,1)) = 101 AND ASCII(SUBSTRING(domain,5,1)) = 115 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 115) OR (CHAR_LENGTH(domain) = 13 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 109 AND ASCII(SUBSTRING(domain,3,1)) = 116 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 116 AND ASCII(SUBSTRING(domain,9,1)) = 116 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 110 AND ASCII(SUBSTRING(domain,12,1)) = 103 AND ASCII(SUBSTRING(domain,13,1)) = 115) OR (CHAR_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 116 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 97 AND ASCII(SUBSTRING(domain,6,1)) = 103 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 95 AND ASCII(SUBSTRING(domain,9,1)) = 114 AND ASCII(SUBSTRING(domain,10,1)) = 101 AND ASCII(SUBSTRING(domain,11,1)) = 118 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 115 AND ASCII(SUBSTRING(domain,14,1)) = 105 AND ASCII(SUBSTRING(domain,15,1)) = 111 AND ASCII(SUBSTRING(domain,16,1)) = 110 AND ASCII(SUBSTRING(domain,17,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 117 AND ASCII(SUBSTRING(domain,2,1)) = 115 AND ASCII(SUBSTRING(domain,3,1)) = 101 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 109 AND ASCII(SUBSTRING(domain,7,1)) = 102 AND ASCII(SUBSTRING(domain,8,1)) = 97)) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 119 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 105 AND ASCII(SUBSTRING(domain,10,1)) = 116 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 101 AND ASCII(SUBSTRING(domain,9,1)) = 97 AND ASCII(SUBSTRING(domain,10,1)) = 100 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104))"`
}

func (vaultRootItemV72) TableName() string { return "secret_rotation_items" }

// Relationships are deliberately frozen and reconciled only after all owned tables exist.
// They establish retained row references; service checks exact births separately.
type vaultRevisionLinksV72 struct {
	IntegrationID string              `gorm:"size:30;not null"`
	Integration   vaultIntegrationV72 `gorm:"foreignKey:IntegrationID;references:ID;constraint:fk_vault_revision_integration,OnDelete:RESTRICT"`
}

func (vaultRevisionLinksV72) TableName() string { return "vault_revisions" }

type vaultWriterLinksV72 struct {
	ID       string           `gorm:"primaryKey;size:30"`
	Revision vaultRevisionV72 `gorm:"belongsTo:Revision;foreignKey:ID;references:ID;constraint:fk_vault_writer_revision,OnDelete:RESTRICT"`
}

func (vaultWriterLinksV72) TableName() string { return "vault_writer_auth" }

type vaultReaderLinksV72 struct {
	ID       string           `gorm:"primaryKey;size:30"`
	Revision vaultRevisionV72 `gorm:"belongsTo:Revision;foreignKey:ID;references:ID;constraint:fk_vault_reader_revision,OnDelete:RESTRICT"`
}

func (vaultReaderLinksV72) TableName() string { return "vault_reader_auth" }

type vaultReceiptLinksV72 struct {
	IntegrationID string              `gorm:"size:30;not null"`
	RevisionID    string              `gorm:"size:30;not null"`
	Integration   vaultIntegrationV72 `gorm:"foreignKey:IntegrationID;references:ID;constraint:fk_vault_receipt_integration,OnDelete:RESTRICT"`
	Revision      vaultRevisionV72    `gorm:"foreignKey:RevisionID;references:ID;constraint:fk_vault_receipt_revision,OnDelete:RESTRICT"`
}

func (vaultReceiptLinksV72) TableName() string { return "vault_config_receipts" }

type vaultProbeLinksV72 struct {
	IntegrationID string              `gorm:"size:30;not null"`
	RevisionID    string              `gorm:"size:30;not null"`
	Integration   vaultIntegrationV72 `gorm:"foreignKey:IntegrationID;references:ID;constraint:fk_vault_probe_integration,OnDelete:RESTRICT"`
	Revision      vaultRevisionV72    `gorm:"foreignKey:RevisionID;references:ID;constraint:fk_vault_probe_revision,OnDelete:RESTRICT"`
}

func (vaultProbeLinksV72) TableName() string { return "vault_probes" }

type vaultCommandLinksV72 struct {
	ProbeID string        `gorm:"size:32;not null"`
	Probe   vaultProbeV72 `gorm:"foreignKey:ProbeID;references:ID;constraint:fk_vault_command_probe,OnDelete:RESTRICT"`
}

func (vaultCommandLinksV72) TableName() string { return "vault_probe_commands" }

const vaultIntegrationVersion = 72

func vaultIntegrationMigration(db *gorm.DB) error {
	models := []any{&vaultCatalogueV72{}, &vaultIntegrationV72{}, &vaultRevisionV72{}, &vaultWriterAuthV72{}, &vaultReaderAuthV72{}, &vaultConfigReceiptV72{}, &vaultProbeV72{}, &vaultProbeCommandV72{}}
	if err := migrateTables(db, models...); err != nil {
		return err
	}
	// Reject incompatible partially created columns rather than publish a partial schema.
	for _, model := range models {
		if err := vaultColumnsV72(db, model); err != nil {
			return err
		}
	}
	if err := migrateTables(db, &vaultRevisionLinksV72{}, &vaultWriterLinksV72{}, &vaultReaderLinksV72{}, &vaultReceiptLinksV72{}, &vaultProbeLinksV72{}, &vaultCommandLinksV72{}); err != nil {
		return err
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&vaultCatalogueV72{ID: 1, Generation: "7d3289d7e740d7300d7477f8a487263056d6be265d7cdbce3f91a3941bd1e1fe"}).Error; err != nil {
		return err
	}
	for _, model := range []any{&vaultRootJobV72{}, &vaultRootProcessV72{}} {
		if err := vaultInventoryColumnV72(db, model); err != nil {
			return err
		}
	}
	var bad int64
	if err := db.Table("secret_rotation_jobs").Where("inventory_version NOT IN ? OR domain < 0 OR (inventory_version = ? AND domain > ?) OR domain > ?", []int{1, 2}, 1, 5, 7).Count(&bad).Error; err != nil {
		return err
	}
	if bad == 0 {
		if err := db.Table("secret_process_verifications").Where("inventory_version NOT IN ? OR inventory_version IS NULL", []int{1, 2}).Count(&bad).Error; err != nil {
			return err
		}
	}
	if bad != 0 {
		return fmt.Errorf("invalid retained root inventory version")
	}
	for _, change := range []struct {
		Model any
		Name  string
	}{{&vaultRootJobV72{}, "ck_secret_rotation_domain"}, {&vaultRootItemV72{}, "ck_secret_item_domain"}} {
		if db.Migrator().HasConstraint(change.Model, change.Name) {
			if err := db.Migrator().DropConstraint(change.Model, change.Name); err != nil {
				return err
			}
		}
		if err := db.Migrator().CreateConstraint(change.Model, change.Name); err != nil {
			return err
		}
	}
	for _, change := range []struct {
		Model any
		Name  string
	}{{&vaultRootJobV72{}, "ck_secret_inventory_version"}, {&vaultRootProcessV72{}, "ck_secret_process_inventory_version"}} {
		if !db.Migrator().HasConstraint(change.Model, change.Name) {
			if err := db.Migrator().CreateConstraint(change.Model, change.Name); err != nil {
				return err
			}
		}
	}
	for _, permission := range []string{"secrets.write", "secrets.test"} {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&vaultPermissionV72{RoleID: "rol_admin", Permission: permission}).Error; err != nil {
			return err
		}
	}
	return nil
}

type vaultPermissionV72 struct {
	RoleID     string `gorm:"primaryKey;size:30"`
	Permission string `gorm:"primaryKey;size:80"`
}

func (vaultPermissionV72) TableName() string { return "role_permissions" }

func vaultColumnsV72(db *gorm.DB, model any) error {
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(model); err != nil {
		return err
	}
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		return err
	}
	for _, field := range statement.Schema.Fields {
		if field.DBName == "" {
			continue
		}
		found := false
		for _, column := range columns {
			if column.Name() != field.DBName {
				continue
			}
			found = true
			if field.Size > 0 && field.DataType == "string" {
				length, known := column.Length()
				if !known || length != int64(field.Size) {
					return fmt.Errorf("incompatible Vault column %s.%s", statement.Table, field.DBName)
				}
			}
			nullable, known := column.Nullable()
			if field.NotNull && (!known || nullable) {
				return fmt.Errorf("nullable Vault column %s.%s", statement.Table, field.DBName)
			}
		}
		if !found {
			return fmt.Errorf("missing Vault column %s.%s", statement.Table, field.DBName)
		}
	}
	return nil
}

// Only the new inventory column may be repaired after a partial DDL attempt.
// NULL means the absent old metadata baseline, never observed new-domain counts.
func vaultInventoryColumnV72(db *gorm.DB, model any) error {
	if !db.Migrator().HasColumn(model, "InventoryVersion") {
		if err := db.Migrator().AddColumn(model, "InventoryVersion"); err != nil {
			return err
		}
	}
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		return err
	}
	for _, col := range columns {
		if col.Name() != "inventory_version" {
			continue
		}
		nullable, known := col.Nullable()
		def, defaulted := col.DefaultValue()
		if known && !nullable && defaulted && strings.Trim(def, `'()"`) == "1" {
			return nil
		}
		if err := db.Session(&gorm.Session{NewDB: true}).Model(model).Where("inventory_version IS NULL").UpdateColumn("InventoryVersion", 1).Error; err != nil {
			return err
		}
		return db.Migrator().AlterColumn(model, "InventoryVersion")
	}
	return fmt.Errorf("root inventory column unavailable")
}
