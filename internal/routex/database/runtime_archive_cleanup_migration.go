package database

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// runtimeArchiveCleanupV115 retains the exact committed administrator intent. Its
// receipt describes storage deletion, never configuration or fleet application.
type runtimeArchiveCleanupV115 struct {
	RequestID      string    `gorm:"primaryKey;size:36;check:ck_runtime_archive_cleanup_identity,OCTET_LENGTH(request_id) = 36 AND OCTET_LENGTH(actor_id) = 30 AND OCTET_LENGTH(review_etag) = 64" json:"-"`
	ActorID        string    `gorm:"size:30;not null;index" json:"-"`
	ActorCreatedAt time.Time `gorm:"precision:6;not null;autoCreateTime:false" json:"-"`
	ReviewETag     string    `gorm:"column:review_etag;size:64;not null" json:"-"`
	Reason         string    `gorm:"type:text;not null;check:ck_runtime_archive_cleanup_payload,OCTET_LENGTH(reason) BETWEEN 1 AND 4000 AND OCTET_LENGTH(archive_ids_json) BETWEEN 34 AND 661" json:"-"`
	ArchiveIDsJSON string    `gorm:"type:text;not null" json:"-"`
	DeletedCount   int64     `gorm:"not null;check:ck_runtime_archive_cleanup_bounds,deleted_count BETWEEN 1 AND 20 AND deleted_bytes > 0" json:"-"`
	DeletedBytes   int64     `gorm:"not null" json:"-"`
	CompletedAt    time.Time `gorm:"precision:6;not null;autoCreateTime:false;check:ck_runtime_archive_cleanup_time,actor_created_at > '0001-01-01 00:00:00' AND completed_at >= actor_created_at" json:"-"`
}

func (runtimeArchiveCleanupV115) TableName() string { return "runtime_archive_cleanups" }

// Frozen V115 owns only completed storage-cleanup receipts. Missing columns in
// nonempty partial DDL cannot be synthesized into historical intent.
func runtimeArchiveCleanupMigration(db *gorm.DB) error {
	model := &runtimeArchiveCleanupV115{}
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return err
	}
	if db.Migrator().HasTable(model) {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil {
			return err
		}
		for _, name := range stmt.Schema.DBNames {
			if db.Migrator().HasColumn(model, name) {
				continue
			}
			if count != 0 {
				return fmt.Errorf("nonempty partial archive cleanup table")
			}
			if err := db.Migrator().AddColumn(model, stmt.Schema.FieldsByDBName[name].Name); err != nil {
				return err
			}
		}
		if err := validateRuntimeArchiveCleanupColumnsV115(db, model, false); err != nil {
			return err
		}
	}
	if err := migrateTables(db, model); err != nil {
		return err
	}
	if err := validateRuntimeArchiveCleanupColumnsV115(db, model, true); err != nil {
		return err
	}
	return replaceRuntimeArchiveChecksV112(db, model)
}

func validateRuntimeArchiveCleanupColumnsV115(db *gorm.DB, model any, indexesRequired bool) error {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return err
	}
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		return err
	}
	if len(columns) != len(stmt.Schema.DBNames) {
		return fmt.Errorf("invalid archive cleanup column count")
	}
	seen := map[string]bool{}
	for _, column := range columns {
		field := stmt.Schema.FieldsByDBName[column.Name()]
		if field == nil || seen[column.Name()] {
			return fmt.Errorf("invalid archive cleanup column")
		}
		seen[column.Name()] = true
		primary, known := column.PrimaryKey()
		if !known || primary != field.PrimaryKey {
			return fmt.Errorf("invalid archive cleanup primary key")
		}
		nullable, known := column.Nullable()
		if !known || nullable {
			return fmt.Errorf("invalid archive cleanup nullability")
		}
		if _, present := column.DefaultValue(); present {
			return fmt.Errorf("invalid archive cleanup default")
		}
		kind := strings.ToLower(column.DatabaseTypeName())
		switch string(field.DataType) {
		case "text":
			if kind != "text" {
				return fmt.Errorf("invalid archive cleanup text type")
			}
		case "string":
			width, known := column.Length()
			if !known || width != int64(field.Size) || (kind != "varchar" && kind != "character varying") {
				return fmt.Errorf("invalid archive cleanup text type")
			}
		case "int":
			if kind != "bigint" && kind != "int8" {
				return fmt.Errorf("invalid archive cleanup integer type")
			}
		case "bool":
			if kind != "bool" && kind != "boolean" && kind != "tinyint" {
				return fmt.Errorf("invalid archive cleanup boolean type")
			}
		case "time":
			precision, _, known := column.DecimalSize()
			if !known || precision != 6 || (kind != "timestamp" && kind != "timestamptz" && kind != "datetime") {
				return fmt.Errorf("invalid archive cleanup time type")
			}
		default:
			return fmt.Errorf("invalid archive cleanup type")
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
			return fmt.Errorf("invalid archive cleanup index")
		}
		for i, column := range actual {
			if !column.ColumnName.Valid || column.ColumnName.String != expected.Fields[i].DBName || !column.Position.Valid || column.Position.Int64 != int64(i+1) || !column.IsUnique.Valid || column.IsUnique.Bool != (expected.Class == "UNIQUE") || column.PrefixLength.Valid {
				return fmt.Errorf("invalid archive cleanup index order")
			}
		}
	}
	return nil
}
