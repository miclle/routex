package database

import (
	"fmt"
	"gorm.io/gorm"
	"sort"
	"strings"
	"time"
)

// V111 extends the same private receipt table without changing released V110.
// Empty defaults backfill existing rows and permit historical two-kind inserts;
// the discriminated CHECK requires active quotas to supply their real parameters.
type insightsReportV111 struct {
	ID           string     `gorm:"primaryKey;size:30;index:idx_insights_report_owner,priority:2;check:ck_insights_report_identity,OCTET_LENGTH(id) = 30 AND OCTET_LENGTH(owner_digest) = 64 AND OCTET_LENGTH(request_id) = 36 AND OCTET_LENGTH(intent_digest) = 64"`
	OwnerDigest  string     `gorm:"size:64;not null;uniqueIndex:idx_insights_report_intent,priority:1;index:idx_insights_report_owner,priority:1"`
	RequestID    string     `gorm:"size:36;not null;uniqueIndex:idx_insights_report_intent,priority:2"`
	IntentDigest string     `gorm:"size:64;not null"`
	Kind         string     `gorm:"size:20;not null;check:ck_insights_report_definition_v111,(deleted_at IS NULL AND ((((OCTET_LENGTH(kind) = 15 AND ASCII(SUBSTRING(kind,1,1)) = 116 AND ASCII(SUBSTRING(kind,2,1)) = 111 AND ASCII(SUBSTRING(kind,3,1)) = 107 AND ASCII(SUBSTRING(kind,4,1)) = 101 AND ASCII(SUBSTRING(kind,5,1)) = 110 AND ASCII(SUBSTRING(kind,6,1)) = 115 AND ASCII(SUBSTRING(kind,7,1)) = 95 AND ASCII(SUBSTRING(kind,8,1)) = 98 AND ASCII(SUBSTRING(kind,9,1)) = 121 AND ASCII(SUBSTRING(kind,10,1)) = 95 AND ASCII(SUBSTRING(kind,11,1)) = 109 AND ASCII(SUBSTRING(kind,12,1)) = 111 AND ASCII(SUBSTRING(kind,13,1)) = 100 AND ASCII(SUBSTRING(kind,14,1)) = 101 AND ASCII(SUBSTRING(kind,15,1)) = 108) OR (OCTET_LENGTH(kind) = 16 AND ASCII(SUBSTRING(kind,1,1)) = 112 AND ASCII(SUBSTRING(kind,2,1)) = 114 AND ASCII(SUBSTRING(kind,3,1)) = 111 AND ASCII(SUBSTRING(kind,4,1)) = 118 AND ASCII(SUBSTRING(kind,5,1)) = 105 AND ASCII(SUBSTRING(kind,6,1)) = 100 AND ASCII(SUBSTRING(kind,7,1)) = 101 AND ASCII(SUBSTRING(kind,8,1)) = 114 AND ASCII(SUBSTRING(kind,9,1)) = 95 AND ASCII(SUBSTRING(kind,10,1)) = 113 AND ASCII(SUBSTRING(kind,11,1)) = 117 AND ASCII(SUBSTRING(kind,12,1)) = 97 AND ASCII(SUBSTRING(kind,13,1)) = 108 AND ASCII(SUBSTRING(kind,14,1)) = 105 AND ASCII(SUBSTRING(kind,15,1)) = 116 AND ASCII(SUBSTRING(kind,16,1)) = 121)) AND OCTET_LENGTH(range_from) BETWEEN 20 AND 30 AND OCTET_LENGTH(range_to) BETWEEN 20 AND 30 AND OCTET_LENGTH(timezone) BETWEEN 1 AND 100 AND OCTET_LENGTH(dimension) = 0 AND OCTET_LENGTH(scope_kind) = 0 AND OCTET_LENGTH(scope_id) = 0) OR ((OCTET_LENGTH(kind) = 10 AND ASCII(SUBSTRING(kind,1,1)) = 116 AND ASCII(SUBSTRING(kind,2,1)) = 101 AND ASCII(SUBSTRING(kind,3,1)) = 97 AND ASCII(SUBSTRING(kind,4,1)) = 109 AND ASCII(SUBSTRING(kind,5,1)) = 95 AND ASCII(SUBSTRING(kind,6,1)) = 113 AND ASCII(SUBSTRING(kind,7,1)) = 117 AND ASCII(SUBSTRING(kind,8,1)) = 111 AND ASCII(SUBSTRING(kind,9,1)) = 116 AND ASCII(SUBSTRING(kind,10,1)) = 97) AND ((OCTET_LENGTH(dimension) = 6 AND ASCII(SUBSTRING(dimension,1,1)) = 116 AND ASCII(SUBSTRING(dimension,2,1)) = 111 AND ASCII(SUBSTRING(dimension,3,1)) = 107 AND ASCII(SUBSTRING(dimension,4,1)) = 101 AND ASCII(SUBSTRING(dimension,5,1)) = 110 AND ASCII(SUBSTRING(dimension,6,1)) = 115) OR (OCTET_LENGTH(dimension) = 5 AND ASCII(SUBSTRING(dimension,1,1)) = 109 AND ASCII(SUBSTRING(dimension,2,1)) = 111 AND ASCII(SUBSTRING(dimension,3,1)) = 110 AND ASCII(SUBSTRING(dimension,4,1)) = 101 AND ASCII(SUBSTRING(dimension,5,1)) = 121)) AND OCTET_LENGTH(range_from) = 0 AND OCTET_LENGTH(range_to) = 0 AND OCTET_LENGTH(timezone) = 0 AND OCTET_LENGTH(scope_kind) = 0 AND OCTET_LENGTH(scope_id) = 0) OR ((OCTET_LENGTH(kind) = 9 AND ASCII(SUBSTRING(kind,1,1)) = 107 AND ASCII(SUBSTRING(kind,2,1)) = 101 AND ASCII(SUBSTRING(kind,3,1)) = 121 AND ASCII(SUBSTRING(kind,4,1)) = 95 AND ASCII(SUBSTRING(kind,5,1)) = 113 AND ASCII(SUBSTRING(kind,6,1)) = 117 AND ASCII(SUBSTRING(kind,7,1)) = 111 AND ASCII(SUBSTRING(kind,8,1)) = 116 AND ASCII(SUBSTRING(kind,9,1)) = 97) AND ((OCTET_LENGTH(dimension) = 6 AND ASCII(SUBSTRING(dimension,1,1)) = 116 AND ASCII(SUBSTRING(dimension,2,1)) = 111 AND ASCII(SUBSTRING(dimension,3,1)) = 107 AND ASCII(SUBSTRING(dimension,4,1)) = 101 AND ASCII(SUBSTRING(dimension,5,1)) = 110 AND ASCII(SUBSTRING(dimension,6,1)) = 115) OR (OCTET_LENGTH(dimension) = 5 AND ASCII(SUBSTRING(dimension,1,1)) = 109 AND ASCII(SUBSTRING(dimension,2,1)) = 111 AND ASCII(SUBSTRING(dimension,3,1)) = 110 AND ASCII(SUBSTRING(dimension,4,1)) = 101 AND ASCII(SUBSTRING(dimension,5,1)) = 121)) AND OCTET_LENGTH(range_from) = 0 AND OCTET_LENGTH(range_to) = 0 AND OCTET_LENGTH(timezone) = 0 AND ((OCTET_LENGTH(scope_kind) = 6 AND ASCII(SUBSTRING(scope_kind,1,1)) = 109 AND ASCII(SUBSTRING(scope_kind,2,1)) = 101 AND ASCII(SUBSTRING(scope_kind,3,1)) = 109 AND ASCII(SUBSTRING(scope_kind,4,1)) = 98 AND ASCII(SUBSTRING(scope_kind,5,1)) = 101 AND ASCII(SUBSTRING(scope_kind,6,1)) = 114) OR (OCTET_LENGTH(scope_kind) = 7 AND ASCII(SUBSTRING(scope_kind,1,1)) = 112 AND ASCII(SUBSTRING(scope_kind,2,1)) = 114 AND ASCII(SUBSTRING(scope_kind,3,1)) = 111 AND ASCII(SUBSTRING(scope_kind,4,1)) = 106 AND ASCII(SUBSTRING(scope_kind,5,1)) = 101 AND ASCII(SUBSTRING(scope_kind,6,1)) = 99 AND ASCII(SUBSTRING(scope_kind,7,1)) = 116)) AND OCTET_LENGTH(scope_id) BETWEEN 1 AND 30))) OR (deleted_at IS NOT NULL AND OCTET_LENGTH(kind) = 0 AND OCTET_LENGTH(range_from) = 0 AND OCTET_LENGTH(range_to) = 0 AND OCTET_LENGTH(timezone) = 0 AND OCTET_LENGTH(dimension) = 0 AND OCTET_LENGTH(scope_kind) = 0 AND OCTET_LENGTH(scope_id) = 0)"`
	From         string     `gorm:"column:range_from;size:30;not null"`
	To           string     `gorm:"column:range_to;size:30;not null"`
	Timezone     string     `gorm:"size:100;not null"`
	Dimension    string     `gorm:"size:6;not null;default:''"`
	ScopeKind    string     `gorm:"size:7;not null;default:''"`
	ScopeID      string     `gorm:"size:30;not null;default:''"`
	CreatedAt    time.Time  `gorm:"precision:6;not null;autoCreateTime:false;check:ck_insights_report_created,created_at > '0001-01-01 00:00:00'"`
	DeletedAt    *time.Time `gorm:"precision:6"`
}

func (insightsReportV111) TableName() string { return "insights_reports" }

func insightsQuotaReportMigration(db *gorm.DB) error {
	frozen := &insightsReportV111{}
	if !db.Migrator().HasTable(frozen) {
		return fmt.Errorf("saved report predecessor table is missing")
	}
	// Add one column at a time so partially applied MySQL DDL resumes safely.
	for _, field := range []string{"Dimension", "ScopeKind", "ScopeID"} {
		if !db.Migrator().HasColumn(frozen, field) {
			if err := db.Migrator().AddColumn(frozen, field); err != nil {
				return err
			}
		}
	}
	if err := validateInsightsReportColumnsV111(db, true); err != nil {
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
			return fmt.Errorf("invalid retained quota report definition")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	// A distinct new definition guard is installed before removing the old
	// two-kind guard. A failed upgrade retains the released restriction until
	// all new checks have been installed and all retained rows validated.
	for _, name := range names {
		if err := replaceV30CheckConstraint(db, frozen, name); err != nil {
			return err
		}
	}
	if db.Migrator().HasConstraint(frozen, "ck_insights_report_definition") {
		if err := db.Migrator().DropConstraint(frozen, "ck_insights_report_definition"); err != nil {
			return err
		}
	}
	return validateInsightsReportColumnsV111(db, true)
}
func validateInsightsReportColumnsV111(db *gorm.DB, requireIndexes bool) error {
	model := &insightsReportV111{}
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
		if value, has := column.DefaultValue(); has != (column.Name() == "dimension" || column.Name() == "scope_kind" || column.Name() == "scope_id") || has && value != "" && value != "''" && value != "''::character varying" && value != "''::text" {
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
