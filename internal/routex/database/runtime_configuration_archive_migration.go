package database

import (
	"fmt"
	"gorm.io/gorm"
	"sort"
	"strings"
	"time"
)

// V112 freezes archive storage; it does not create an automatic capture pipeline.
// runtimeConfigurationArchiveV112 stores private source facts, never runtime authority.
type runtimeConfigurationArchiveV112 struct {
	ID                  string    `gorm:"primaryKey;size:30;check:ck_runtime_archive_identity,OCTET_LENGTH(id) = 30 AND OCTET_LENGTH(content_digest) = 64 AND OCTET_LENGTH(source_digest) = 64 AND OCTET_LENGTH(authorization_digest) = 64 AND OCTET_LENGTH(network_digest) = 64 AND OCTET_LENGTH(pricing_digest) = 64" json:"-"`
	CodecVersion        int       `gorm:"not null;check:ck_runtime_archive_bounds,codec_version = 1 AND plaintext_bytes > 0 AND plaintext_bytes <= 16777216 AND chunk_count > 0 AND chunk_count <= 512 AND plaintext_bytes > (chunk_count - 1) * 32768 AND plaintext_bytes <= chunk_count * 32768" json:"-"`
	ContentDigest       string    `gorm:"size:64;not null" json:"-"`
	SourceDigest        string    `gorm:"size:64;not null" json:"-"`
	AuthorizationDigest string    `gorm:"size:64;not null" json:"-"`
	NetworkDigest       string    `gorm:"size:64;not null" json:"-"`
	PricingDigest       string    `gorm:"size:64;not null" json:"-"`
	PlaintextBytes      int       `gorm:"not null" json:"-"`
	ChunkCount          int       `gorm:"not null" json:"-"`
	CreatedAt           time.Time `gorm:"precision:6;not null;autoCreateTime:false;check:ck_runtime_archive_birth,created_at > '0001-01-01 00:00:00'" json:"-"`
}

func (runtimeConfigurationArchiveV112) TableName() string { return "runtime_configuration_archives" }

// Only AuthCiphertext may change during root-envelope rewrap.
type runtimeConfigurationArchiveChunkV112 struct {
	ID               string                           `gorm:"primaryKey;size:34;check:ck_runtime_archive_chunk_identity,OCTET_LENGTH(id) = 34 AND OCTET_LENGTH(archive_id) = 30 AND OCTET_LENGTH(secret_generation) = 64 AND OCTET_LENGTH(plaintext_digest) = 64" json:"-"`
	ArchiveID        string                           `gorm:"size:30;not null;uniqueIndex:idx_runtime_archive_chunk_order,priority:1" json:"-"`
	Ordinal          int                              `gorm:"not null;uniqueIndex:idx_runtime_archive_chunk_order,priority:2;check:ck_runtime_archive_chunk_bounds,ordinal >= 0 AND ordinal < 512 AND plaintext_bytes > 0 AND plaintext_bytes <= 32768 AND OCTET_LENGTH(auth_ciphertext) BETWEEN 1 AND 131072" json:"-"`
	PlaintextBytes   int                              `gorm:"not null" json:"-"`
	PlaintextDigest  string                           `gorm:"size:64;not null" json:"-"`
	SecretGeneration string                           `gorm:"size:64;not null" json:"-"`
	AuthCiphertext   string                           `gorm:"type:text;not null" json:"-"`
	Archive          *runtimeConfigurationArchiveV112 `gorm:"foreignKey:ArchiveID;references:ID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (runtimeConfigurationArchiveChunkV112) TableName() string {
	return "runtime_configuration_archive_chunks"
}

type runtimeArchiveRootJobV112 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5 OR inventory_version = 6 OR inventory_version = 7 OR inventory_version = 8 OR inventory_version = 9 OR inventory_version = 10 OR inventory_version = 11"`
	Domain           int `gorm:"not null;check:ck_secret_rotation_domain,(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7) OR (inventory_version = 3 AND domain >= 0 AND domain <= 8) OR (inventory_version = 4 AND domain >= 0 AND domain <= 9) OR (inventory_version = 5 AND domain >= 0 AND domain <= 10) OR (inventory_version = 6 AND domain >= 0 AND domain <= 11) OR (inventory_version = 7 AND domain >= 0 AND domain <= 11) OR (inventory_version = 8 AND domain >= 0 AND domain <= 11) OR (inventory_version = 9 AND domain >= 0 AND domain <= 12) OR (inventory_version = 10 AND domain >= 0 AND domain <= 12) OR (inventory_version = 11 AND domain >= 0 AND domain <= 13)"`
}

func (runtimeArchiveRootJobV112) TableName() string { return "secret_rotation_jobs" }

type runtimeArchiveRootProcessV112 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_process_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5 OR inventory_version = 6 OR inventory_version = 7 OR inventory_version = 8 OR inventory_version = 9 OR inventory_version = 10 OR inventory_version = 11"`
}

func (runtimeArchiveRootProcessV112) TableName() string { return "secret_process_verifications" }

type runtimeArchiveRootItemV112 struct {
	Domain string `gorm:"primaryKey;size:32;check:ck_secret_item_domain,((((((CHAR_LENGTH(domain) = 20 AND ASCII(SUBSTRING(domain,1,1)) = 112 AND ASCII(SUBSTRING(domain,2,1)) = 114 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 118 AND ASCII(SUBSTRING(domain,5,1)) = 105 AND ASCII(SUBSTRING(domain,6,1)) = 100 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 95 AND ASCII(SUBSTRING(domain,10,1)) = 99 AND ASCII(SUBSTRING(domain,11,1)) = 114 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 100 AND ASCII(SUBSTRING(domain,14,1)) = 101 AND ASCII(SUBSTRING(domain,15,1)) = 110 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 105 AND ASCII(SUBSTRING(domain,18,1)) = 97 AND ASCII(SUBSTRING(domain,19,1)) = 108 AND ASCII(SUBSTRING(domain,20,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 101 AND ASCII(SUBSTRING(domain,2,1)) = 103 AND ASCII(SUBSTRING(domain,3,1)) = 114 AND ASCII(SUBSTRING(domain,4,1)) = 101 AND ASCII(SUBSTRING(domain,5,1)) = 115 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 115) OR (CHAR_LENGTH(domain) = 13 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 109 AND ASCII(SUBSTRING(domain,3,1)) = 116 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 116 AND ASCII(SUBSTRING(domain,9,1)) = 116 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 110 AND ASCII(SUBSTRING(domain,12,1)) = 103 AND ASCII(SUBSTRING(domain,13,1)) = 115) OR (CHAR_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 116 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 97 AND ASCII(SUBSTRING(domain,6,1)) = 103 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 95 AND ASCII(SUBSTRING(domain,9,1)) = 114 AND ASCII(SUBSTRING(domain,10,1)) = 101 AND ASCII(SUBSTRING(domain,11,1)) = 118 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 115 AND ASCII(SUBSTRING(domain,14,1)) = 105 AND ASCII(SUBSTRING(domain,15,1)) = 111 AND ASCII(SUBSTRING(domain,16,1)) = 110 AND ASCII(SUBSTRING(domain,17,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 117 AND ASCII(SUBSTRING(domain,2,1)) = 115 AND ASCII(SUBSTRING(domain,3,1)) = 101 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 109 AND ASCII(SUBSTRING(domain,7,1)) = 102 AND ASCII(SUBSTRING(domain,8,1)) = 97)) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 119 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 105 AND ASCII(SUBSTRING(domain,10,1)) = 116 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 101 AND ASCII(SUBSTRING(domain,9,1)) = 97 AND ASCII(SUBSTRING(domain,10,1)) = 100 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104))) OR (OCTET_LENGTH(domain) = 14 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 105 AND ASCII(SUBSTRING(domain,3,1)) = 100 AND ASCII(SUBSTRING(domain,4,1)) = 99 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 112 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 111 AND ASCII(SUBSTRING(domain,9,1)) = 118 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 100 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 114 AND ASCII(SUBSTRING(domain,14,1)) = 115) OR (OCTET_LENGTH(domain) = 15 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 116 AND ASCII(SUBSTRING(domain,5,1)) = 104 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 112 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 111 AND ASCII(SUBSTRING(domain,10,1)) = 118 AND ASCII(SUBSTRING(domain,11,1)) = 105 AND ASCII(SUBSTRING(domain,12,1)) = 100 AND ASCII(SUBSTRING(domain,13,1)) = 101 AND ASCII(SUBSTRING(domain,14,1)) = 114 AND ASCII(SUBSTRING(domain,15,1)) = 115) OR (OCTET_LENGTH(domain) = 14 AND ASCII(SUBSTRING(domain,1,1)) = 108 AND ASCII(SUBSTRING(domain,2,1)) = 100 AND ASCII(SUBSTRING(domain,3,1)) = 97 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 112 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 111 AND ASCII(SUBSTRING(domain,9,1)) = 118 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 100 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 114 AND ASCII(SUBSTRING(domain,14,1)) = 115)) OR (OCTET_LENGTH(domain) = 24 AND ASCII(SUBSTRING(domain,1,1)) = 110 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 109 AND ASCII(SUBSTRING(domain,4,1)) = 101 AND ASCII(SUBSTRING(domain,5,1)) = 100 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 105 AND ASCII(SUBSTRING(domain,8,1)) = 100 AND ASCII(SUBSTRING(domain,9,1)) = 101 AND ASCII(SUBSTRING(domain,10,1)) = 110 AND ASCII(SUBSTRING(domain,11,1)) = 116 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 116 AND ASCII(SUBSTRING(domain,14,1)) = 121 AND ASCII(SUBSTRING(domain,15,1)) = 95 AND ASCII(SUBSTRING(domain,16,1)) = 112 AND ASCII(SUBSTRING(domain,17,1)) = 114 AND ASCII(SUBSTRING(domain,18,1)) = 111 AND ASCII(SUBSTRING(domain,19,1)) = 118 AND ASCII(SUBSTRING(domain,20,1)) = 105 AND ASCII(SUBSTRING(domain,21,1)) = 100 AND ASCII(SUBSTRING(domain,22,1)) = 101 AND ASCII(SUBSTRING(domain,23,1)) = 114 AND ASCII(SUBSTRING(domain,24,1)) = 115) OR (OCTET_LENGTH(domain) = 21 AND ASCII(SUBSTRING(domain,1,1)) = 97 AND ASCII(SUBSTRING(domain,2,1)) = 112 AND ASCII(SUBSTRING(domain,3,1)) = 105 AND ASCII(SUBSTRING(domain,4,1)) = 95 AND ASCII(SUBSTRING(domain,5,1)) = 107 AND ASCII(SUBSTRING(domain,6,1)) = 101 AND ASCII(SUBSTRING(domain,7,1)) = 121 AND ASCII(SUBSTRING(domain,8,1)) = 95 AND ASCII(SUBSTRING(domain,9,1)) = 100 AND ASCII(SUBSTRING(domain,10,1)) = 101 AND ASCII(SUBSTRING(domain,11,1)) = 108 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 118 AND ASCII(SUBSTRING(domain,14,1)) = 101 AND ASCII(SUBSTRING(domain,15,1)) = 114 AND ASCII(SUBSTRING(domain,16,1)) = 121 AND ASCII(SUBSTRING(domain,17,1)) = 95 AND ASCII(SUBSTRING(domain,18,1)) = 97 AND ASCII(SUBSTRING(domain,19,1)) = 117 AND ASCII(SUBSTRING(domain,20,1)) = 116 AND ASCII(SUBSTRING(domain,21,1)) = 104)) OR (OCTET_LENGTH(domain) = 21 AND ASCII(SUBSTRING(domain,1,1)) = 114 AND ASCII(SUBSTRING(domain,2,1)) = 117 AND ASCII(SUBSTRING(domain,3,1)) = 110 AND ASCII(SUBSTRING(domain,4,1)) = 116 AND ASCII(SUBSTRING(domain,5,1)) = 105 AND ASCII(SUBSTRING(domain,6,1)) = 109 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 95 AND ASCII(SUBSTRING(domain,9,1)) = 99 AND ASCII(SUBSTRING(domain,10,1)) = 111 AND ASCII(SUBSTRING(domain,11,1)) = 110 AND ASCII(SUBSTRING(domain,12,1)) = 102 AND ASCII(SUBSTRING(domain,13,1)) = 105 AND ASCII(SUBSTRING(domain,14,1)) = 103 AND ASCII(SUBSTRING(domain,15,1)) = 95 AND ASCII(SUBSTRING(domain,16,1)) = 99 AND ASCII(SUBSTRING(domain,17,1)) = 104 AND ASCII(SUBSTRING(domain,18,1)) = 117 AND ASCII(SUBSTRING(domain,19,1)) = 110 AND ASCII(SUBSTRING(domain,20,1)) = 107 AND ASCII(SUBSTRING(domain,21,1)) = 115)"`
}

func (runtimeArchiveRootItemV112) TableName() string { return "secret_rotation_items" }

func runtimeConfigurationArchiveMigration(db *gorm.DB) error {
	models := []any{&runtimeConfigurationArchiveV112{}, &runtimeConfigurationArchiveChunkV112{}}
	for _, model := range models {
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(model); err != nil {
			return err
		}
		if db.Migrator().HasTable(model) {
			var count int64
			if err := db.Model(model).Count(&count).Error; err != nil {
				return err
			}
			for _, name := range statement.Schema.DBNames {
				if !db.Migrator().HasColumn(model, name) {
					if count != 0 {
						return fmt.Errorf("nonempty partial runtime archive table")
					}
					if err := db.Migrator().AddColumn(model, statement.Schema.FieldsByDBName[name].Name); err != nil {
						return err
					}
				}
			}
			if err := validateRuntimeArchiveColumnsV112(db, model, false); err != nil {
				return err
			}
		}
	}
	if err := migrateTables(db, models...); err != nil {
		return err
	}
	for _, model := range models {
		if err := replaceRuntimeArchiveChecksV112(db, model); err != nil {
			return err
		}
		if err := validateRuntimeArchiveColumnsV112(db, model, true); err != nil {
			return err
		}
	}
	// Frozen narrow adapters extend root inventory guards, leaving all released
	// domain prefixes and other root-policy schemas unchanged.
	for _, model := range []any{&runtimeArchiveRootJobV112{}, &runtimeArchiveRootProcessV112{}, &runtimeArchiveRootItemV112{}} {
		if !db.Migrator().HasTable(model) {
			return fmt.Errorf("runtime archive root predecessor missing")
		}
		if err := replaceRuntimeArchiveChecksV112(db, model); err != nil {
			return err
		}
	}
	return nil
}

func replaceRuntimeArchiveChecksV112(db *gorm.DB, model any) error {
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(model); err != nil {
		return err
	}
	names := []string{}
	for name, check := range statement.Schema.ParseCheckConstraints() {
		var invalid int64
		if err := db.Model(model).Where("NOT (" + check.Constraint + ") OR (" + check.Constraint + ") IS NULL").Count(&invalid).Error; err != nil {
			return err
		}
		if invalid != 0 {
			return fmt.Errorf("invalid retained runtime archive schema facts")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := replaceV30CheckConstraint(db, model, name); err != nil {
			return err
		}
	}
	return nil
}

func validateRuntimeArchiveColumnsV112(db *gorm.DB, model any, requireIndexes bool) error {
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(model); err != nil {
		return err
	}
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		return err
	}
	if len(columns) != len(statement.Schema.DBNames) {
		return fmt.Errorf("invalid runtime archive column count")
	}
	seen := map[string]bool{}
	for _, column := range columns {
		field := statement.Schema.FieldsByDBName[column.Name()]
		if field == nil || seen[column.Name()] {
			return fmt.Errorf("invalid runtime archive column")
		}
		seen[column.Name()] = true
		primary, known := column.PrimaryKey()
		if !known || primary != field.PrimaryKey {
			return fmt.Errorf("invalid runtime archive primary key")
		}
		nullable, known := column.Nullable()
		if !known || nullable {
			return fmt.Errorf("invalid runtime archive nullability")
		}
		if _, has := column.DefaultValue(); has {
			return fmt.Errorf("invalid runtime archive default")
		}
		kind := strings.ToLower(column.DatabaseTypeName())
		switch string(field.DataType) {
		case "string", "text":
			if field.DBName == "auth_ciphertext" {
				if kind != "text" {
					return fmt.Errorf("invalid runtime archive envelope type")
				}
				continue
			}
			if string(field.DataType) == "text" {
				return fmt.Errorf("invalid runtime archive text metadata")
			}
			width, known := column.Length()
			if !known || width != int64(field.Size) || kind != "varchar" && kind != "character varying" {
				return fmt.Errorf("invalid runtime archive text metadata")
			}
		case "time":
			precision, _, known := column.DecimalSize()
			if !known || precision != 6 || kind != "timestamp" && kind != "timestamptz" && kind != "datetime" {
				return fmt.Errorf("invalid runtime archive time metadata")
			}
		case "int":
			if kind != "bigint" && kind != "int8" {
				return fmt.Errorf("invalid runtime archive integer metadata")
			}
		default:
			return fmt.Errorf("invalid runtime archive type")
		}
	}
	// Reuse the database-owned ordered-index metadata adapter because GORM's
	// PostgreSQL GetIndexes omits key ordinals. Schema writes remain GORM-owned.
	for _, expected := range statement.Schema.ParseIndexes() {
		columns, e := credentialAttemptStatisticsIndexColumns(db, statement.Schema.Table, expected.Name)
		if e != nil {
			return e
		}
		if len(columns) == 0 && !requireIndexes {
			continue
		}
		if len(columns) != len(expected.Fields) {
			return fmt.Errorf("invalid runtime archive index")
		}
		for i, column := range columns {
			if !column.ColumnName.Valid || column.ColumnName.String != expected.Fields[i].DBName || !column.Position.Valid || column.Position.Int64 != int64(i+1) || !column.IsUnique.Valid || column.IsUnique.Bool != (expected.Class == "UNIQUE") || column.PrefixLength.Valid {
				return fmt.Errorf("invalid runtime archive index order")
			}
		}
	}
	return nil
}
