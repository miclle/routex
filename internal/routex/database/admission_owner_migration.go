package database

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// V118 adds dormant owner metadata only. No mode activation or runtime wiring.
type admissionModeV118 struct {
	ID             int                   `gorm:"primaryKey;autoIncrement:false;check:ck_admission_mode_singleton,id = 1" json:"-"`
	InstallationID string                `gorm:"size:64;not null;check:ck_admission_mode_identity,OCTET_LENGTH(installation_id) = 30" json:"-"`
	Mode           string                `gorm:"size:16;not null;default:local;check:ck_admission_mode_value,(OCTET_LENGTH(mode) = 5 AND ASCII(SUBSTRING(mode,1,1)) = 108 AND ASCII(SUBSTRING(mode,2,1)) = 111 AND ASCII(SUBSTRING(mode,3,1)) = 99 AND ASCII(SUBSTRING(mode,4,1)) = 97 AND ASCII(SUBSTRING(mode,5,1)) = 108) OR (OCTET_LENGTH(mode) = 6 AND ASCII(SUBSTRING(mode,1,1)) = 115 AND ASCII(SUBSTRING(mode,2,1)) = 104 AND ASCII(SUBSTRING(mode,3,1)) = 97 AND ASCII(SUBSTRING(mode,4,1)) = 114 AND ASCII(SUBSTRING(mode,5,1)) = 101 AND ASCII(SUBSTRING(mode,6,1)) = 100)" json:"-"`
	Revision       int64                 `gorm:"not null;default:1;check:ck_admission_mode_revision,revision > 0" json:"-"`
	Installation   *limitInstallationV14 `gorm:"belongsTo:Installation;foreignKey:ID;references:ID;constraint:fk_admission_mode_installation,OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (admissionModeV118) TableName() string { return "admission_modes" }

// admissionSpoolOwnerV118 retains one registration and at most one terminal fence.
// Neither a fence nor process lease metadata proves executor/transport closure.
type admissionSpoolOwnerV118 struct {
	InstallationID    string             `gorm:"primaryKey;size:64;check:ck_admission_owner_identity,OCTET_LENGTH(installation_id) = 30 AND OCTET_LENGTH(spool_id) BETWEEN 1 AND 64 AND OCTET_LENGTH(incarnation_id) = 30 AND OCTET_LENGTH(registration_id) = 36" json:"-"`
	SpoolID           string             `gorm:"primaryKey;size:64" json:"-"`
	ModeID            int                `gorm:"not null;check:ck_admission_owner_mode,mode_id = 1 AND mode_revision > 0" json:"-"`
	ModeRevision      int64              `gorm:"not null" json:"-"`
	RegistrationID    string             `gorm:"size:36;not null;uniqueIndex:uidx_admission_owner_registration" json:"-"`
	IncarnationID     string             `gorm:"size:30;not null" json:"-"`
	InstanceStartedAt time.Time          `gorm:"precision:6;not null;autoCreateTime:false;check:ck_admission_owner_time,instance_started_at > '0001-01-01 00:00:00' AND registered_at >= instance_started_at" json:"-"`
	RegisteredAt      time.Time          `gorm:"precision:6;not null;autoCreateTime:false" json:"-"`
	RegisteredFence   int64              `gorm:"not null;check:ck_admission_owner_fence,registered_fence = 1 AND ((current_fence = 1 AND fencing_id IS NULL AND fenced_at IS NULL) OR (current_fence = 2 AND fencing_id IS NOT NULL AND OCTET_LENGTH(fencing_id) = 36 AND fenced_at IS NOT NULL AND fenced_at >= registered_at))" json:"-"`
	CurrentFence      int64              `gorm:"not null" json:"-"`
	FencingID         *string            `gorm:"size:36;uniqueIndex:uidx_admission_owner_fencing" json:"-"`
	FencedAt          *time.Time         `gorm:"precision:6" json:"-"`
	Mode              *admissionModeV118 `gorm:"belongsTo:Mode;foreignKey:ModeID;references:ID;constraint:fk_admission_owner_mode,OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (admissionSpoolOwnerV118) TableName() string { return "admission_spool_owners" }

var admissionJournalV118 = regexp.MustCompile(`^led_[0-7][0-9a-hjkmnp-tv-z]{25}$`)

func admissionOwnerMigration(db *gorm.DB) error {
	for _, model := range []any{&admissionModeV118{}, &admissionSpoolOwnerV118{}} {
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
					return fmt.Errorf("nonempty partial admission owner schema")
				}
				if err := db.Migrator().AddColumn(model, stmt.Schema.FieldsByDBName[name].Name); err != nil {
					return err
				}
			}
			if err := validateAdmissionOwnerColumnsV118(db, model, false); err != nil {
				return err
			}
		}
		if err := migrateTables(db, model); err != nil {
			return err
		}
		if err := validateAdmissionOwnerColumnsV118(db, model, true); err != nil {
			return err
		}
		if err := replaceRuntimeArchiveChecksV112(db, model); err != nil {
			return err
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var binding limitInstallationV14
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&binding, 1).Error; err != nil {
			return err
		}
		var modes []admissionModeV118
		if err := tx.Find(&modes).Error; err != nil {
			return err
		}
		// A fresh SQL schema has no established local journal. Never seed an owner
		// domain with an absent identity or manufacture one during migration.
		if !binding.Initialized {
			if len(modes) != 0 {
				return fmt.Errorf("admission mode without initialized journal")
			}
			return nil
		}
		if !admissionJournalV118.MatchString(binding.JournalID) {
			return fmt.Errorf("invalid initialized admission journal identity")
		}
		if len(modes) > 1 || (len(modes) == 1 && (modes[0].ID != 1 || modes[0].InstallationID != binding.JournalID || modes[0].Revision <= 0 || (modes[0].Mode != "local" && modes[0].Mode != "shared"))) {
			return fmt.Errorf("foreign admission mode installation")
		}
		if len(modes) == 0 {
			return tx.Omit("Installation").Create(&admissionModeV118{ID: 1, InstallationID: binding.JournalID, Mode: "local", Revision: 1}).Error
		}
		return nil
	})
}

// Missing columns may be repaired only in empty interrupted tables. Existing
// column shape is never silently changed, including nullable receipt tails.
func validateAdmissionOwnerColumnsV118(db *gorm.DB, model any, indexesRequired bool) error {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return err
	}
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		return err
	}
	if len(columns) != len(stmt.Schema.DBNames) {
		return fmt.Errorf("invalid admission owner column count")
	}
	seen := map[string]bool{}
	for _, column := range columns {
		field := stmt.Schema.FieldsByDBName[column.Name()]
		if field == nil || seen[column.Name()] {
			return fmt.Errorf("invalid admission owner column")
		}
		seen[column.Name()] = true
		pk, known := column.PrimaryKey()
		if !known || pk != field.PrimaryKey {
			return fmt.Errorf("invalid admission owner primary key")
		}
		nullable, known := column.Nullable()
		expectedNullable := !field.NotNull && !field.PrimaryKey
		if !known || nullable != expectedNullable {
			return fmt.Errorf("invalid admission owner nullability")
		}
		def, present := column.DefaultValue()
		if present != field.HasDefaultValue || (present && strings.Trim(strings.SplitN(def, "::", 2)[0], "'() ") != field.DefaultValue) {
			return fmt.Errorf("invalid admission owner default")
		}
		kind := strings.ToLower(column.DatabaseTypeName())
		switch field.IndirectFieldType.Kind() {
		case reflect.String:
			width, known := column.Length()
			if !known || width != int64(field.Size) || (kind != "varchar" && kind != "character varying") {
				return fmt.Errorf("invalid admission owner string")
			}
		case reflect.Int64:
			if kind != "bigint" && kind != "int8" {
				return fmt.Errorf("invalid admission owner integer")
			}
		case reflect.Int:
			if kind != "bigint" && kind != "int8" && kind != "int" && kind != "int4" && kind != "integer" {
				return fmt.Errorf("invalid admission owner singleton")
			}
		case reflect.Struct:
			if field.IndirectFieldType != reflect.TypeFor[time.Time]() || (kind != "timestamp" && kind != "timestamptz" && kind != "datetime") {
				return fmt.Errorf("invalid admission owner time")
			}
			precision, _, known := column.DecimalSize()
			if !known || precision != 6 {
				return fmt.Errorf("invalid admission owner time precision")
			}
		default:
			return fmt.Errorf("unsupported admission owner column")
		}
	}
	for _, expected := range stmt.Schema.ParseIndexes() {
		actual, err := credentialAttemptStatisticsIndexColumns(db, stmt.Schema.Table, expected.Name)
		if err != nil {
			return err
		}
		if len(actual) == 0 && !indexesRequired {
			continue
		}
		if len(actual) != len(expected.Fields) {
			return fmt.Errorf("invalid admission owner index")
		}
		for i, column := range actual {
			if !column.ColumnName.Valid || column.ColumnName.String != expected.Fields[i].DBName || !column.Position.Valid || column.Position.Int64 != int64(i+1) || !column.IsUnique.Valid || column.IsUnique.Bool != (expected.Class == "UNIQUE") || column.PrefixLength.Valid {
				return fmt.Errorf("invalid admission owner index order")
			}
		}
	}
	return nil
}
