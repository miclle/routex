package database

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"gorm.io/gorm"
)

// V105 owns only original manual-publication receipts. It never dispatches work
// or promotes prior runtime observations into command outcomes. OCTET_LENGTH and
// ASCII/SUBSTRING freeze enum bytes across MySQL PAD SPACE/case collations and
// PostgreSQL; ordinary string equality alone would admit collating aliases.
// Explicit terminal nonnull guards prevent SQL CHECK UNKNOWN from admitting
// missing timestamps or failure codes. DDL remains GORM-owned on both drivers.
type runtimePublicationCommandV105 struct {
	RequestID              string     `gorm:"primaryKey;size:36"`
	ActorID                string     `gorm:"size:30;not null;index:idx_runtime_publication_actor,priority:1"`
	ActorCreatedAt         time.Time  `gorm:"precision:6;not null;index:idx_runtime_publication_actor,priority:2"`
	Reason                 string     `gorm:"type:text;not null"`
	ClaimInstanceID        string     `gorm:"size:30;not null"`
	ClaimInstanceStartedAt time.Time  `gorm:"precision:6;not null"`
	ClaimedAt              time.Time  `gorm:"precision:6;not null;index:idx_runtime_publication_actor,priority:3"`
	FinalizedAt            *time.Time `gorm:"precision:6"`
	Outcome                string     `gorm:"size:32;not null;check:ck_runtime_publication_outcome,((OCTET_LENGTH(outcome) = 7 AND ASCII(SUBSTRING(outcome,1,1)) = 99 AND ASCII(SUBSTRING(outcome,2,1)) = 108 AND ASCII(SUBSTRING(outcome,3,1)) = 97 AND ASCII(SUBSTRING(outcome,4,1)) = 105 AND ASCII(SUBSTRING(outcome,5,1)) = 109 AND ASCII(SUBSTRING(outcome,6,1)) = 101 AND ASCII(SUBSTRING(outcome,7,1)) = 100) AND finalized_at IS NULL AND failure_code IS NULL) OR ((OCTET_LENGTH(outcome) = 20 AND ASCII(SUBSTRING(outcome,1,1)) = 114 AND ASCII(SUBSTRING(outcome,2,1)) = 101 AND ASCII(SUBSTRING(outcome,3,1)) = 102 AND ASCII(SUBSTRING(outcome,4,1)) = 114 AND ASCII(SUBSTRING(outcome,5,1)) = 101 AND ASCII(SUBSTRING(outcome,6,1)) = 115 AND ASCII(SUBSTRING(outcome,7,1)) = 104 AND ASCII(SUBSTRING(outcome,8,1)) = 95 AND ASCII(SUBSTRING(outcome,9,1)) = 114 AND ASCII(SUBSTRING(outcome,10,1)) = 101 AND ASCII(SUBSTRING(outcome,11,1)) = 116 AND ASCII(SUBSTRING(outcome,12,1)) = 117 AND ASCII(SUBSTRING(outcome,13,1)) = 114 AND ASCII(SUBSTRING(outcome,14,1)) = 110 AND ASCII(SUBSTRING(outcome,15,1)) = 101 AND ASCII(SUBSTRING(outcome,16,1)) = 100 AND ASCII(SUBSTRING(outcome,17,1)) = 95 AND ASCII(SUBSTRING(outcome,18,1)) = 110 AND ASCII(SUBSTRING(outcome,19,1)) = 105 AND ASCII(SUBSTRING(outcome,20,1)) = 108) AND finalized_at IS NOT NULL AND finalized_at >= claimed_at AND failure_code IS NULL) OR ((OCTET_LENGTH(outcome) = 22 AND ASCII(SUBSTRING(outcome,1,1)) = 114 AND ASCII(SUBSTRING(outcome,2,1)) = 101 AND ASCII(SUBSTRING(outcome,3,1)) = 102 AND ASCII(SUBSTRING(outcome,4,1)) = 114 AND ASCII(SUBSTRING(outcome,5,1)) = 101 AND ASCII(SUBSTRING(outcome,6,1)) = 115 AND ASCII(SUBSTRING(outcome,7,1)) = 104 AND ASCII(SUBSTRING(outcome,8,1)) = 95 AND ASCII(SUBSTRING(outcome,9,1)) = 114 AND ASCII(SUBSTRING(outcome,10,1)) = 101 AND ASCII(SUBSTRING(outcome,11,1)) = 116 AND ASCII(SUBSTRING(outcome,12,1)) = 117 AND ASCII(SUBSTRING(outcome,13,1)) = 114 AND ASCII(SUBSTRING(outcome,14,1)) = 110 AND ASCII(SUBSTRING(outcome,15,1)) = 101 AND ASCII(SUBSTRING(outcome,16,1)) = 100 AND ASCII(SUBSTRING(outcome,17,1)) = 95 AND ASCII(SUBSTRING(outcome,18,1)) = 101 AND ASCII(SUBSTRING(outcome,19,1)) = 114 AND ASCII(SUBSTRING(outcome,20,1)) = 114 AND ASCII(SUBSTRING(outcome,21,1)) = 111 AND ASCII(SUBSTRING(outcome,22,1)) = 114) AND finalized_at IS NOT NULL AND finalized_at >= claimed_at AND failure_code IS NOT NULL AND (OCTET_LENGTH(failure_code) = 22 AND ASCII(SUBSTRING(failure_code,1,1)) = 114 AND ASCII(SUBSTRING(failure_code,2,1)) = 117 AND ASCII(SUBSTRING(failure_code,3,1)) = 110 AND ASCII(SUBSTRING(failure_code,4,1)) = 116 AND ASCII(SUBSTRING(failure_code,5,1)) = 105 AND ASCII(SUBSTRING(failure_code,6,1)) = 109 AND ASCII(SUBSTRING(failure_code,7,1)) = 101 AND ASCII(SUBSTRING(failure_code,8,1)) = 95 AND ASCII(SUBSTRING(failure_code,9,1)) = 114 AND ASCII(SUBSTRING(failure_code,10,1)) = 101 AND ASCII(SUBSTRING(failure_code,11,1)) = 102 AND ASCII(SUBSTRING(failure_code,12,1)) = 114 AND ASCII(SUBSTRING(failure_code,13,1)) = 101 AND ASCII(SUBSTRING(failure_code,14,1)) = 115 AND ASCII(SUBSTRING(failure_code,15,1)) = 104 AND ASCII(SUBSTRING(failure_code,16,1)) = 95 AND ASCII(SUBSTRING(failure_code,17,1)) = 102 AND ASCII(SUBSTRING(failure_code,18,1)) = 97 AND ASCII(SUBSTRING(failure_code,19,1)) = 105 AND ASCII(SUBSTRING(failure_code,20,1)) = 108 AND ASCII(SUBSTRING(failure_code,21,1)) = 101 AND ASCII(SUBSTRING(failure_code,22,1)) = 100))"`
	FailureCode            *string    `gorm:"size:32"`
}

func (runtimePublicationCommandV105) TableName() string { return "runtime_publication_commands" }

func runtimePublicationCommandMigration(db *gorm.DB) error {
	model := &runtimePublicationCommandV105{}
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
				var count int64
				if err := db.Model(model).Count(&count).Error; err != nil {
					return err
				}
				// MySQL must not synthesize missing claim facts into retained receipts.
				if count != 0 {
					return fmt.Errorf("nonempty partial runtime publication table")
				}
				if err := db.Migrator().AddColumn(model, field.Name); err != nil {
					return err
				}
			}
		}
	}
	if err := validateRuntimePublicationColumnsV105(db); err != nil {
		return err
	}
	check := stmt.Schema.ParseCheckConstraints()["ck_runtime_publication_outcome"]
	var invalid int64
	// IS NULL makes unknown SQL predicate results fail closed before any CHECK
	// replacement. The expression itself is a frozen schema literal, not input.
	if err := db.Model(model).Where("NOT (" + check.Constraint + ") OR (" + check.Constraint + ") IS NULL").Count(&invalid).Error; err != nil {
		return err
	}
	if invalid != 0 {
		return fmt.Errorf("invalid retained runtime publication receipt")
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
			return fmt.Errorf("invalid runtime publication index")
		}
		for i, c := range columns {
			if !c.ColumnName.Valid || c.ColumnName.String != index.Fields[i].DBName || !c.Position.Valid || c.Position.Int64 != int64(i+1) || !c.IsUnique.Valid || c.IsUnique.Bool != (index.Class == "UNIQUE") || c.PrefixLength.Valid {
				return fmt.Errorf("invalid runtime publication index")
			}
		}
	}
	// A partially applied MySQL DDL sequence is resumed on each bounded replay;
	// a same-named weak CHECK never substitutes for this frozen predicate.
	if db.Migrator().HasConstraint(model, "ck_runtime_publication_outcome") {
		if err := db.Migrator().DropConstraint(model, "ck_runtime_publication_outcome"); err != nil {
			return err
		}
	}
	return db.Migrator().CreateConstraint(model, "ck_runtime_publication_outcome")
}

func validateRuntimePublicationColumnsV105(db *gorm.DB) error {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&runtimePublicationCommandV105{}); err != nil {
		return err
	}
	columns, err := db.Migrator().ColumnTypes(&runtimePublicationCommandV105{})
	if err != nil {
		return err
	}
	if len(columns) != len(stmt.Schema.Fields) {
		return fmt.Errorf("invalid runtime publication columns")
	}
	seen := map[string]bool{}
	for _, column := range columns {
		field := stmt.Schema.FieldsByDBName[column.Name()]
		if field == nil || seen[column.Name()] {
			return fmt.Errorf("invalid runtime publication columns")
		}
		seen[column.Name()] = true
		primary, known := column.PrimaryKey()
		if !known || primary != field.PrimaryKey {
			return fmt.Errorf("invalid runtime publication primary key")
		}
		nullable, known := column.Nullable()
		expectedNullable := field.FieldType.Kind() == reflect.Pointer
		if !known || nullable != expectedNullable {
			return fmt.Errorf("invalid runtime publication nullability")
		}
		value, hasDefault := column.DefaultValue()
		if hasDefault && (!expectedNullable || !strings.EqualFold(strings.TrimSpace(value), "NULL")) {
			return fmt.Errorf("invalid runtime publication default")
		}
		kind := strings.ToLower(column.DatabaseTypeName())
		switch string(field.DataType) {
		case "string":
			if field.Size > 0 {
				width, known := column.Length()
				if !known || width != int64(field.Size) || kind != "varchar" && kind != "character varying" {
					return fmt.Errorf("invalid runtime publication string")
				}
			} else if kind != "text" {
				return fmt.Errorf("invalid runtime publication text")
			}
		case "text":
			if field.DBName != "reason" || field.Size != 0 || kind != "text" {
				return fmt.Errorf("invalid runtime publication text")
			}
		case "time":
			precision, _, known := column.DecimalSize()
			if !known || precision != 6 || kind != "timestamp" && kind != "timestamptz" && kind != "datetime" {
				return fmt.Errorf("invalid runtime publication timestamp")
			}
		default:
			return fmt.Errorf("invalid runtime publication field")
		}
	}
	return nil
}
