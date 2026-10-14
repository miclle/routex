package database

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// runtimeArchiveRetentionV113 serializes archive admission and actual envelope-byte accounting.
// Caps pause new archives; existing legacy data and root rewrap are never deleted.
type runtimeArchiveRetentionV113 struct {
	ID             int   `gorm:"primaryKey;autoIncrement:false;check:ck_runtime_archive_retention_id,id = 1" json:"-"`
	Initialized    bool  `gorm:"not null" json:"-"`
	RetainedCount  int64 `gorm:"not null;check:ck_runtime_archive_retention_bounds,retained_count >= 0 AND encrypted_bytes >= 0" json:"-"`
	EncryptedBytes int64 `gorm:"not null" json:"-"`
}

func (runtimeArchiveRetentionV113) TableName() string { return "runtime_archive_retention" }

// A digest has one canonical retained target. Older duplicate versions stay immutable.
type runtimeArchiveContentV113 struct {
	ContentDigest string                           `gorm:"primaryKey;size:64;check:ck_runtime_archive_content_digest,OCTET_LENGTH(content_digest) = 64" json:"-"`
	ArchiveID     string                           `gorm:"size:30;not null;index" json:"-"`
	Archive       *runtimeConfigurationArchiveV112 `gorm:"foreignKey:ArchiveID;references:ID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (runtimeArchiveContentV113) TableName() string { return "runtime_archive_contents" }

// Only the first retained coherent local capture is recorded, not installation
// or an actor attribution. Legacy raw archives have no inferred provenance.
type runtimeArchiveCaptureV113 struct {
	ArchiveID         string                           `gorm:"primaryKey;size:30" json:"-"`
	SnapshotID        string                           `gorm:"size:30;not null;check:ck_runtime_archive_capture_identity,OCTET_LENGTH(snapshot_id) = 30 AND OCTET_LENGTH(archive_id) = 30" json:"-"`
	RoutesPublishedAt time.Time                        `gorm:"precision:6;not null;autoCreateTime:false" json:"-"`
	CapturedAt        time.Time                        `gorm:"precision:6;not null;autoCreateTime:false;check:ck_runtime_archive_capture_time,captured_at >= routes_published_at AND routes_published_at > '0001-01-01 00:00:00'" json:"-"`
	Archive           *runtimeConfigurationArchiveV112 `gorm:"foreignKey:ArchiveID;references:ID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (runtimeArchiveCaptureV113) TableName() string { return "runtime_archive_captures" }

// V113 uses bounded frozen tables. Released archive storage remains unchanged.
func runtimeArchiveRetentionMigration(db *gorm.DB) error {
	models := []any{&runtimeArchiveRetentionV113{}, &runtimeArchiveContentV113{}, &runtimeArchiveCaptureV113{}}
	for _, model := range models {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			return err
		}
		if !db.Migrator().HasTable(model) {
			continue
		}
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil {
			return err
		}
		for _, name := range stmt.Schema.DBNames {
			if db.Migrator().HasColumn(model, name) {
				continue
			}
			if count != 0 {
				return fmt.Errorf("nonempty partial archive retention table")
			}
			if err := db.Migrator().AddColumn(model, stmt.Schema.FieldsByDBName[name].Name); err != nil {
				return err
			}
		}
		if err := validateRuntimeArchiveRetentionColumnsV113(db, model, false); err != nil {
			return err
		}
	}
	if err := migrateTables(db, models...); err != nil {
		return err
	}
	for _, model := range models {
		if err := validateRuntimeArchiveRetentionColumnsV113(db, model, true); err != nil {
			return err
		}
		if err := replaceRuntimeArchiveChecksV112(db, model); err != nil {
			return err
		}
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&runtimeArchiveRetentionV113{ID: 1}).Error; err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var ledger runtimeArchiveRetentionV113
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", 1).Take(&ledger).Error; err != nil {
			return err
		}
		if ledger.ID != 1 || ledger.RetainedCount < 0 || ledger.EncryptedBytes < 0 {
			return fmt.Errorf("invalid retained archive accounting")
		}
		if ledger.Initialized {
			return nil
		}
		var count int64
		if err := tx.Model(&runtimeConfigurationArchiveV112{}).Count(&count).Error; err != nil {
			return err
		}
		bytes, err := runtimeArchiveEncryptedBytesV113(tx)
		if err != nil {
			return err
		}
		cursor := ""
		for {
			var rows []runtimeConfigurationArchiveV112
			q := tx.Order("id ASC").Limit(50)
			if cursor != "" {
				q = q.Where("id > ?", cursor)
			}
			if err := q.Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				if !runtimeArchiveIDV113.MatchString(row.ID) || !runtimeArchiveDigestV113.MatchString(row.ContentDigest) || row.ID <= cursor {
					return fmt.Errorf("invalid retained archive identity")
				}
				target := runtimeArchiveContentV113{ContentDigest: row.ContentDigest, ArchiveID: row.ID}
				if err := tx.Omit("Archive").Clauses(clause.OnConflict{DoNothing: true}).Create(&target).Error; err != nil {
					return err
				}
				cursor = row.ID
			}
			if len(rows) < 50 {
				break
			}
		}
		return tx.Model(&runtimeArchiveRetentionV113{}).Where("id = ?", 1).Updates(map[string]any{"initialized": true, "retained_count": count, "encrypted_bytes": bytes}).Error
	})
}

// GORM has no typed aggregate for a text column's stored byte length. This
// database-local expression uses OCTET_LENGTH on both supported drivers and
// counts the actual persisted envelope, including its encoding and key ID.
func runtimeArchiveEncryptedBytesV113(tx *gorm.DB) (int64, error) {
	var total int64
	err := tx.Model(&runtimeConfigurationArchiveChunkV112{}).Select("COALESCE(SUM(OCTET_LENGTH(auth_ciphertext)), 0)").Scan(&total).Error
	if err != nil {
		return 0, err
	}
	if total < 0 {
		return 0, fmt.Errorf("invalid retained archive bytes")
	}
	return total, nil
}

var runtimeArchiveIDV113 = regexp.MustCompile(`^rcv_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
var runtimeArchiveDigestV113 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func validateRuntimeArchiveRetentionColumnsV113(db *gorm.DB, model any, indexesRequired bool) error {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return err
	}
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		return err
	}
	if len(columns) != len(stmt.Schema.DBNames) {
		return fmt.Errorf("invalid archive retention column count")
	}
	seen := map[string]bool{}
	for _, column := range columns {
		field := stmt.Schema.FieldsByDBName[column.Name()]
		if field == nil || seen[column.Name()] {
			return fmt.Errorf("invalid archive retention column")
		}
		seen[column.Name()] = true
		primary, known := column.PrimaryKey()
		if !known || primary != field.PrimaryKey {
			return fmt.Errorf("invalid archive retention primary key")
		}
		nullable, known := column.Nullable()
		if !known || nullable {
			return fmt.Errorf("invalid archive retention nullability")
		}
		if _, present := column.DefaultValue(); present {
			return fmt.Errorf("invalid archive retention default")
		}
		kind := strings.ToLower(column.DatabaseTypeName())
		switch string(field.DataType) {
		case "string":
			width, known := column.Length()
			if !known || width != int64(field.Size) || (kind != "varchar" && kind != "character varying") {
				return fmt.Errorf("invalid archive retention text type")
			}
		case "int":
			if kind != "bigint" && kind != "int8" {
				return fmt.Errorf("invalid archive retention integer type")
			}
		case "bool":
			if kind != "bool" && kind != "boolean" && kind != "tinyint" {
				return fmt.Errorf("invalid archive retention boolean type")
			}
		case "time":
			precision, _, known := column.DecimalSize()
			if !known || precision != 6 || (kind != "timestamp" && kind != "timestamptz" && kind != "datetime") {
				return fmt.Errorf("invalid archive retention time type")
			}
		default:
			return fmt.Errorf("invalid archive retention type")
		}
	}
	for _, expected := range stmt.Schema.ParseIndexes() {
		actual, e := credentialAttemptStatisticsIndexColumns(db, stmt.Schema.Table, expected.Name)
		if e != nil {
			return e
		}
		if len(actual) == 0 && !indexesRequired {
			continue
		}
		if len(actual) != len(expected.Fields) {
			return fmt.Errorf("invalid archive retention index")
		}
		for i, column := range actual {
			if !column.ColumnName.Valid || column.ColumnName.String != expected.Fields[i].DBName || !column.Position.Valid || column.Position.Int64 != int64(i+1) || !column.IsUnique.Valid || column.IsUnique.Bool != (expected.Class == "UNIQUE") || column.PrefixLength.Valid {
				return fmt.Errorf("invalid archive retention index order")
			}
		}
	}
	return nil
}
