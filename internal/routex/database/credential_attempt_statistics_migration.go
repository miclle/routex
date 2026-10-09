package database

import (
	"database/sql"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// V93 indexes retained attempt chronology across runtime snapshots. It changes
// no attempt, attribution or Credential state. This private schema is frozen.
type credentialAttemptStatisticsV93 struct {
	ID           string    `gorm:"primaryKey;size:64;index:idx_attempts_credential_time,priority:3"`
	CredentialID string    `gorm:"size:30;not null;default:'';index:idx_attempts_credential_time,priority:1"`
	CompletedAt  time.Time `gorm:"not null;index:idx_attempts_credential_time,priority:2"`
}

func (credentialAttemptStatisticsV93) TableName() string { return "call_attempts" }

func credentialAttemptStatisticsMigration(db *gorm.DB) error {
	frozen := &credentialAttemptStatisticsV93{}
	const name = "idx_attempts_credential_time"
	if !db.Migrator().HasIndex(frozen, name) {
		if err := db.Migrator().CreateIndex(frozen, name); err != nil {
			return err
		}
	}
	columns, err := credentialAttemptStatisticsIndexColumns(db, frozen.TableName(), name)
	if err != nil {
		return err
	}
	if len(columns) == 0 {
		return fmt.Errorf("missing credential attempt chronology index")
	}
	want := []string{"credential_id", "completed_at", "id"}
	if len(columns) != len(want) {
		return fmt.Errorf("invalid credential attempt chronology index")
	}
	for i, column := range columns {
		if !column.ColumnName.Valid || column.ColumnName.String != want[i] || !column.Position.Valid || column.Position.Int64 != int64(i+1) || !column.IsUnique.Valid || column.IsUnique.Bool || column.PrefixLength.Valid {
			return fmt.Errorf("invalid credential attempt chronology index")
		}
	}
	return nil
}

type credentialAttemptStatisticsIndexColumn struct {
	ColumnName   sql.NullString
	Position     sql.NullInt64
	IsUnique     sql.NullBool
	PrefixLength sql.NullInt64
}

// GORM's PostgreSQL GetIndexes joins attributes with ANY(indkey), without
// preserving key ordinals. Its Columns result cannot validate this ordered key.
// Keep GORM DDL, but inspect bound table/index metadata here: PostgreSQL indkey
// ordinality and MySQL seq_in_index express the physical key order explicitly.
func credentialAttemptStatisticsIndexColumns(db *gorm.DB, table, name string) ([]credentialAttemptStatisticsIndexColumn, error) {
	var columns []credentialAttemptStatisticsIndexColumn
	var query *gorm.DB
	switch db.Name() {
	case "postgres":
		query = db.Table("pg_catalog.pg_index AS membership").
			Select("attribute.attname AS column_name, index_key.ordinality AS position, membership.indisunique AS is_unique").
			Joins("JOIN pg_catalog.pg_class AS relation ON relation.oid = membership.indrelid").
			Joins("JOIN pg_catalog.pg_class AS index_relation ON index_relation.oid = membership.indexrelid").
			Joins("JOIN LATERAL unnest(membership.indkey) WITH ORDINALITY AS index_key(attnum, ordinality) ON TRUE").
			Joins("LEFT JOIN pg_catalog.pg_attribute AS attribute ON attribute.attrelid = relation.oid AND attribute.attnum = index_key.attnum").
			Where("relation.oid = to_regclass(?)", table).
			Where("index_relation.relname = ?", name).
			Where("membership.indisvalid AND membership.indisready AND membership.indpred IS NULL AND membership.indexprs IS NULL").
			Order("index_key.ordinality ASC")
	case "mysql":
		query = db.Table("information_schema.statistics").
			Select("column_name, seq_in_index AS position, non_unique = 0 AS is_unique, sub_part AS prefix_length").
			Where("table_schema = DATABASE() AND table_name = ? AND index_name = ?", table, name).
			Order("seq_in_index ASC")
	default:
		return nil, gorm.ErrUnsupportedDriver
	}
	if err := query.Scan(&columns).Error; err != nil {
		return nil, err
	}
	return columns, nil
}
