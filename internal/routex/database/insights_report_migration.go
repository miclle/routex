package database

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Version 110 freezes owner-private definitions and cleared intent receipts.
type insightsReportV110 struct {
	ID           string     `gorm:"primaryKey;size:30;index:idx_insights_report_owner,priority:2;check:ck_insights_report_identity,OCTET_LENGTH(id) = 30 AND OCTET_LENGTH(owner_digest) = 64 AND OCTET_LENGTH(request_id) = 36 AND OCTET_LENGTH(intent_digest) = 64"`
	OwnerDigest  string     `gorm:"size:64;not null;uniqueIndex:idx_insights_report_intent,priority:1;index:idx_insights_report_owner,priority:1"`
	RequestID    string     `gorm:"size:36;not null;uniqueIndex:idx_insights_report_intent,priority:2"`
	IntentDigest string     `gorm:"size:64;not null"`
	Kind         string     `gorm:"size:20;not null;check:ck_insights_report_definition,(deleted_at IS NULL AND ((OCTET_LENGTH(kind) = 15 AND ASCII(SUBSTRING(kind,1,1)) = 116 AND ASCII(SUBSTRING(kind,2,1)) = 111 AND ASCII(SUBSTRING(kind,3,1)) = 107 AND ASCII(SUBSTRING(kind,4,1)) = 101 AND ASCII(SUBSTRING(kind,5,1)) = 110 AND ASCII(SUBSTRING(kind,6,1)) = 115 AND ASCII(SUBSTRING(kind,7,1)) = 95 AND ASCII(SUBSTRING(kind,8,1)) = 98 AND ASCII(SUBSTRING(kind,9,1)) = 121 AND ASCII(SUBSTRING(kind,10,1)) = 95 AND ASCII(SUBSTRING(kind,11,1)) = 109 AND ASCII(SUBSTRING(kind,12,1)) = 111 AND ASCII(SUBSTRING(kind,13,1)) = 100 AND ASCII(SUBSTRING(kind,14,1)) = 101 AND ASCII(SUBSTRING(kind,15,1)) = 108) OR (OCTET_LENGTH(kind) = 16 AND ASCII(SUBSTRING(kind,1,1)) = 112 AND ASCII(SUBSTRING(kind,2,1)) = 114 AND ASCII(SUBSTRING(kind,3,1)) = 111 AND ASCII(SUBSTRING(kind,4,1)) = 118 AND ASCII(SUBSTRING(kind,5,1)) = 105 AND ASCII(SUBSTRING(kind,6,1)) = 100 AND ASCII(SUBSTRING(kind,7,1)) = 101 AND ASCII(SUBSTRING(kind,8,1)) = 114 AND ASCII(SUBSTRING(kind,9,1)) = 95 AND ASCII(SUBSTRING(kind,10,1)) = 113 AND ASCII(SUBSTRING(kind,11,1)) = 117 AND ASCII(SUBSTRING(kind,12,1)) = 97 AND ASCII(SUBSTRING(kind,13,1)) = 108 AND ASCII(SUBSTRING(kind,14,1)) = 105 AND ASCII(SUBSTRING(kind,15,1)) = 116 AND ASCII(SUBSTRING(kind,16,1)) = 121)) AND OCTET_LENGTH(range_from) BETWEEN 20 AND 30 AND OCTET_LENGTH(range_to) BETWEEN 20 AND 30 AND OCTET_LENGTH(timezone) BETWEEN 1 AND 100) OR (deleted_at IS NOT NULL AND OCTET_LENGTH(kind) = 0 AND OCTET_LENGTH(range_from) = 0 AND OCTET_LENGTH(range_to) = 0 AND OCTET_LENGTH(timezone) = 0)"`
	From         string     `gorm:"column:range_from;size:30;not null"`
	To           string     `gorm:"column:range_to;size:30;not null"`
	Timezone     string     `gorm:"size:100;not null"`
	CreatedAt    time.Time  `gorm:"precision:6;not null;autoCreateTime:false;check:ck_insights_report_created,created_at > '0001-01-01 00:00:00'"`
	DeletedAt    *time.Time `gorm:"precision:6"`
}

func (insightsReportV110) TableName() string { return "insights_reports" }

func insightsReportMigration(db *gorm.DB) error {
	frozen := &insightsReportV110{}
	if db.Migrator().HasTable(frozen) {
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(frozen); err != nil {
			return err
		}
		var count int64
		if err := db.Model(frozen).Count(&count).Error; err != nil {
			return err
		}
		for _, name := range statement.Schema.DBNames {
			if !db.Migrator().HasColumn(frozen, name) {
				if count != 0 {
					return fmt.Errorf("nonempty partial saved report table")
				}
				if err := db.Migrator().AddColumn(frozen, statement.Schema.FieldsByDBName[name].Name); err != nil {
					return err
				}
			}
		}
		if err := validateInsightsReportColumnsV110(db, false); err != nil {
			return err
		}
	}
	if err := migrateTables(db, frozen); err != nil {
		return err
	}
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(frozen); err != nil {
		return err
	}
	names := []string{}
	for name, check := range statement.Schema.ParseCheckConstraints() {
		var invalid int64
		if err := db.Model(frozen).Where("NOT (" + check.Constraint + ") OR (" + check.Constraint + ") IS NULL").Count(&invalid).Error; err != nil {
			return err
		}
		if invalid != 0 {
			return fmt.Errorf("invalid retained saved report")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	// Replay replaces named CHECKs after validating retained rows; an interrupted
	// or weakened same-name constraint must not silently survive qualification.
	for _, name := range names {
		if err := replaceV30CheckConstraint(db, frozen, name); err != nil {
			return err
		}
	}
	return validateInsightsReportColumnsV110(db, true)
}
func validateInsightsReportColumnsV110(db *gorm.DB, requireIndexes bool) error {
	model := &insightsReportV110{}
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(model); err != nil {
		return err
	}
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		return err
	}
	if len(columns) != len(statement.Schema.DBNames) {
		return fmt.Errorf("invalid saved report column count")
	}
	seen := map[string]bool{}
	for _, column := range columns {
		field := statement.Schema.FieldsByDBName[column.Name()]
		if field == nil || seen[column.Name()] {
			return fmt.Errorf("invalid saved report column")
		}
		seen[column.Name()] = true
		primary, known := column.PrimaryKey()
		if !known || primary != field.PrimaryKey {
			return fmt.Errorf("invalid saved report primary key")
		}
		nullable, known := column.Nullable()
		if !known || nullable != (column.Name() == "deleted_at") {
			return fmt.Errorf("invalid saved report nullability")
		}
		if _, has := column.DefaultValue(); has {
			return fmt.Errorf("invalid saved report default")
		}
		kind := strings.ToLower(column.DatabaseTypeName())
		switch string(field.DataType) {
		case "string":
			width, known := column.Length()
			if !known || width != int64(field.Size) || kind != "varchar" && kind != "character varying" {
				return fmt.Errorf("invalid saved report text metadata")
			}
		case "time":
			precision, _, known := column.DecimalSize()
			if !known || precision != 6 || kind != "timestamp" && kind != "timestamptz" && kind != "datetime" {
				return fmt.Errorf("invalid saved report time metadata")
			}
		default:
			return fmt.Errorf("invalid saved report type")
		}
	}
	// GORM GetIndexes loses PostgreSQL key ordinals. Reuse the database-owned
	// ordered metadata adapter while keeping all schema writes in GORM.
	for _, expected := range statement.Schema.ParseIndexes() {
		columns, err := credentialAttemptStatisticsIndexColumns(db, statement.Schema.Table, expected.Name)
		if err != nil {
			return err
		}
		if len(columns) == 0 && !requireIndexes {
			continue
		}
		if len(columns) != len(expected.Fields) {
			return fmt.Errorf("invalid saved report index")
		}
		for i, column := range columns {
			if !column.ColumnName.Valid || column.ColumnName.String != expected.Fields[i].DBName ||
				!column.Position.Valid || column.Position.Int64 != int64(i+1) ||
				!column.IsUnique.Valid || column.IsUnique.Bool != (expected.Class == "UNIQUE") || column.PrefixLength.Valid {
				return fmt.Errorf("invalid saved report index order")
			}
		}
	}
	return nil
}
