package database

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// V109 freezes the prospective observer and immutable threshold episodes. There
// is no live catalog FK and no historical Attempt backfill or reordered history.
type credentialInferenceFailureStateV109 struct {
	CredentialDigest     string    `gorm:"primaryKey;size:64;check:ck_credential_failure_digest,OCTET_LENGTH(credential_digest) = 64"`
	CredentialID         string    `gorm:"size:30;not null;check:ck_credential_failure_identity,OCTET_LENGTH(credential_id) BETWEEN 1 AND 30"`
	AcceptanceSequence   int64     `gorm:"not null;check:ck_credential_failure_sequence,acceptance_sequence BETWEEN 0 AND 9223372036854775807"`
	ConsecutiveFailures  int       `gorm:"not null;check:ck_credential_failure_count,consecutive_failures BETWEEN 0 AND 5 AND consecutive_failures <= acceptance_sequence"`
	Latched              bool      `gorm:"not null;check:ck_credential_failure_latch,((latched AND OCTET_LENGTH(last_trigger_attempt_id) BETWEEN 1 AND 64) OR (NOT latched AND OCTET_LENGTH(last_trigger_attempt_id) = 0)) AND (consecutive_failures < 5 OR latched)"`
	LastAttemptID        string    `gorm:"size:64;not null;default:'';check:ck_credential_failure_last_attempt,(acceptance_sequence = 0 AND OCTET_LENGTH(last_attempt_id) = 0) OR (acceptance_sequence > 0 AND OCTET_LENGTH(last_attempt_id) BETWEEN 1 AND 64)"`
	LastSuccessAttemptID string    `gorm:"size:64;not null;default:''"`
	LastTriggerAttemptID string    `gorm:"size:64;not null;default:''"`
	UpdatedAt            time.Time `gorm:"precision:6;not null;autoUpdateTime:false;check:ck_credential_failure_updated,updated_at > '0001-01-01 00:00:00'"`
}

func (credentialInferenceFailureStateV109) TableName() string {
	return "credential_inference_failure_states"
}

type credentialInferenceFailureAttemptV109 struct {
	ID string `gorm:"primaryKey;size:64"`
}

func (credentialInferenceFailureAttemptV109) TableName() string { return "call_attempts" }

type credentialInferenceFailureEpisodeV109 struct {
	TriggerAttemptID     string                                 `gorm:"primaryKey;size:64;index:idx_credential_failure_episode_recorded,priority:2"`
	CredentialDigest     string                                 `gorm:"size:64;not null;uniqueIndex:idx_credential_failure_episode_sequence,priority:1"`
	CredentialID         string                                 `gorm:"size:30;not null;check:ck_credential_failure_episode_identity,OCTET_LENGTH(credential_id) BETWEEN 1 AND 30"`
	AcceptanceSequence   int64                                  `gorm:"not null;uniqueIndex:idx_credential_failure_episode_sequence,priority:2;check:ck_credential_failure_episode_sequence,acceptance_sequence BETWEEN 5 AND 9223372036854775807"`
	FailureCount         int                                    `gorm:"not null;check:ck_credential_failure_episode_count,failure_count = 5"`
	LastSuccessAttemptID string                                 `gorm:"size:64;not null;default:''"`
	RecordedAt           time.Time                              `gorm:"precision:6;not null;index:idx_credential_failure_episode_recorded,priority:1;check:ck_credential_failure_episode_time,recorded_at > '0001-01-01 00:00:00'"`
	Attempt              *credentialInferenceFailureAttemptV109 `gorm:"belongsTo:Attempt;foreignKey:TriggerAttemptID;references:ID;constraint:fk_credential_failure_episode_attempt,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	State                *credentialInferenceFailureStateV109   `gorm:"belongsTo:State;foreignKey:CredentialDigest;references:CredentialDigest;constraint:fk_credential_failure_episode_state,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (credentialInferenceFailureEpisodeV109) TableName() string {
	return "credential_inference_failure_episodes"
}

func credentialInferenceFailureMigration(db *gorm.DB) error {
	for _, model := range []any{&credentialInferenceFailureStateV109{}, &credentialInferenceFailureEpisodeV109{}} {
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
				if !db.Migrator().HasColumn(model, name) {
					if count != 0 {
						return fmt.Errorf("nonempty partial credential inference observer")
					}
					if err := db.Migrator().AddColumn(model, stmt.Schema.FieldsByDBName[name].Name); err != nil {
						return err
					}
				}
			}
			if err := validateCredentialInferenceColumnsV109(db, model); err != nil {
				return err
			}
		} else if err := db.Migrator().CreateTable(model); err != nil {
			return err
		}
		if err := validateCredentialInferenceColumnsV109(db, model); err != nil {
			return err
		}
		for name, check := range stmt.Schema.ParseCheckConstraints() {
			var invalid int64
			// Frozen tag predicates validate retained rows before repairing partial
			// MySQL DDL. No runtime input contributes SQL and DDL uses Migrator.
			if err := db.Model(model).Where("NOT (" + check.Constraint + ") OR (" + check.Constraint + ") IS NULL").Count(&invalid).Error; err != nil {
				return err
			}
			if invalid != 0 {
				return fmt.Errorf("invalid retained credential inference observer")
			}
			if db.Migrator().HasConstraint(model, name) {
				if err := db.Migrator().DropConstraint(model, name); err != nil {
					return err
				}
			}
			if err := db.Migrator().CreateConstraint(model, name); err != nil {
				return err
			}
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
				return fmt.Errorf("invalid credential inference observer index")
			}
			for i, column := range columns {
				if !column.ColumnName.Valid || column.ColumnName.String != index.Fields[i].DBName || !column.Position.Valid || column.Position.Int64 != int64(i+1) ||
					!column.IsUnique.Valid || column.IsUnique.Bool != (index.Class == "UNIQUE") || column.PrefixLength.Valid {
					return fmt.Errorf("invalid credential inference observer index")
				}
			}
		}
		for _, relation := range stmt.Schema.Relationships.Relations {
			constraint := relation.ParseConstraint()
			if constraint == nil {
				return fmt.Errorf("missing frozen credential inference reference")
			}
			if db.Migrator().HasConstraint(model, constraint.Name) {
				if err := db.Migrator().DropConstraint(model, constraint.Name); err != nil {
					return err
				}
			}
			if err := db.Migrator().CreateConstraint(model, constraint.Name); err != nil {
				return err
			}
		}
	}
	return migrateCredentialInferenceEnvelopeV109(db)
}

func validateCredentialInferenceColumnsV109(db *gorm.DB, model any) error {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return err
	}
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(columns))
	for _, column := range columns {
		field := stmt.Schema.FieldsByDBName[column.Name()]
		if field == nil || seen[column.Name()] {
			return fmt.Errorf("unexpected credential inference column")
		}
		seen[field.DBName] = true
		if nullable, ok := column.Nullable(); !ok || nullable {
			return fmt.Errorf("nullable credential inference column")
		}
		if primary, ok := column.PrimaryKey(); !ok || primary != field.PrimaryKey {
			return fmt.Errorf("invalid credential inference primary key")
		}
		value, defaulted := column.DefaultValue()
		if field.HasDefaultValue {
			// The only V109 source/state defaults are frozen empty identities.
			if !defaulted || (value != "" && value != "''" && value != "''::character varying" && value != "''::text") {
				return fmt.Errorf("invalid credential inference default")
			}
		} else if defaulted {
			return fmt.Errorf("unexpected credential inference default")
		}
		fullType, fullTypeKnown := column.ColumnType()
		if !fullTypeKnown || strings.Contains(strings.ToLower(fullType), "unsigned") {
			return fmt.Errorf("invalid credential inference signed column metadata")
		}
		kind := strings.ToLower(column.DatabaseTypeName())
		switch field.DBName {
		case "updated_at", "recorded_at":
			if kind != "timestamp" && kind != "timestamptz" && kind != "datetime" {
				return fmt.Errorf("invalid credential inference timestamp")
			}
			if precision, _, ok := column.DecimalSize(); !ok || precision != 6 {
				return fmt.Errorf("invalid credential inference timestamp precision")
			}
		case "acceptance_sequence":
			if kind != "bigint" && kind != "int8" {
				return fmt.Errorf("invalid credential inference sequence width")
			}
		case "consecutive_failures", "failure_count":
			if kind != "bigint" && kind != "int8" {
				return fmt.Errorf("invalid credential inference integer")
			}
		case "latched":
			if kind != "bool" && kind != "boolean" && kind != "tinyint" {
				return fmt.Errorf("invalid credential inference boolean")
			}
		default:
			length, ok := column.Length()
			if !ok || length != int64(field.Size) || kind != "varchar" && kind != "character varying" {
				return fmt.Errorf("invalid credential inference identity column")
			}
		}
	}
	if len(seen) != len(stmt.Schema.DBNames) {
		return fmt.Errorf("missing credential inference column")
	}
	return nil
}
