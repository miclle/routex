// V120 is an unregistered dormant candidate. It owns only the origin table,
// preserves V119 and seeds no provenance, calendar, coverage or initialization.
package database

import (
	"fmt"

	"gorm.io/gorm"
)

// This frozen predecessor stub is only an FK target, never a migration owner.
type admissionEconomicOriginDomainV120 struct {
	InstallationID string `gorm:"primaryKey;size:30"`
}

func (admissionEconomicOriginDomainV120) TableName() string { return "admission_economic_domains" }

type admissionEconomicOriginV120 struct {
	InstallationID           string                             `gorm:"primaryKey;size:30;check:ck_economic_origin_identity,OCTET_LENGTH(installation_id) = 30" json:"-"`
	FormatVersion            int64                              `gorm:"not null;check:ck_economic_origin_format,format_version = 1" json:"-"`
	SourceKind               string                             `gorm:"size:16;not null;check:ck_economic_origin_source,OCTET_LENGTH(source_kind) = 5 AND ASCII(SUBSTRING(source_kind,1,1)) = 102 AND ASCII(SUBSTRING(source_kind,2,1)) = 114 AND ASCII(SUBSTRING(source_kind,3,1)) = 101 AND ASCII(SUBSTRING(source_kind,4,1)) = 115 AND ASCII(SUBSTRING(source_kind,5,1)) = 104" json:"-"`
	TimeZone                 string                             `gorm:"size:100;not null;check:ck_economic_origin_calendar,OCTET_LENGTH(time_zone) BETWEEN 1 AND 100 AND OCTET_LENGTH(calendar_revision_at_freeze) BETWEEN 1 AND 64" json:"-"`
	CalendarRevisionAtFreeze string                             `gorm:"size:64;not null" json:"-"`
	CoverageStart            int64                              `gorm:"not null;check:ck_economic_origin_time,coverage_start > 0 AND coverage_start <= 9222767236854775807 AND clock_high_water_at_freeze = coverage_start AND boundary_at = coverage_start" json:"-"`
	ClockHighWaterAtFreeze   int64                              `gorm:"not null" json:"-"`
	BoundaryAt               int64                              `gorm:"not null" json:"-"`
	EvidenceDigest           string                             `gorm:"size:64;not null;check:ck_economic_origin_digest,OCTET_LENGTH(evidence_digest) = 64" json:"-"`
	EvidenceBody             []byte                             `gorm:"not null;check:ck_economic_origin_body,OCTET_LENGTH(evidence_body) BETWEEN 1 AND 131072" json:"-"`
	Domain                   *admissionEconomicOriginDomainV120 `gorm:"belongsTo:Domain;foreignKey:InstallationID;references:InstallationID;constraint:fk_economic_origin_domain,OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (admissionEconomicOriginV120) TableName() string { return "admission_economic_origins" }

// The existing runner must own its same-connection migration lock before this
// candidate is called. There is no registry entry or production caller yet.
func admissionEconomicOriginMigration(db *gorm.DB) error {
	if !db.Migrator().HasTable(&admissionEconomicOriginDomainV120{}) {
		return fmt.Errorf("economic origin predecessor missing")
	}
	// Inspect the frozen V119 shape; never migrate or modify its domain.
	if err := validateAdmissionEconomicColumnsV119(db, &admissionEconomicDomainV119{}, true); err != nil {
		return err
	}
	model := &admissionEconomicOriginV120{}
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(model); err != nil {
		return err
	}
	if db.Migrator().HasTable(model) {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil {
			return err
		}
		// Repair only empty interrupted DDL. Unguarded columns must precede
		// guarded ones because GORM may install CHECK when it adds the field.
		for _, guarded := range []bool{false, true} {
			for _, name := range statement.Schema.DBNames {
				field := statement.Schema.FieldsByDBName[name]
				if (field.TagSettings["CHECK"] != "") != guarded || db.Migrator().HasColumn(model, name) {
					continue
				}
				if count != 0 {
					return fmt.Errorf("nonempty partial economic origin schema")
				}
				if err := db.Migrator().AddColumn(model, field.Name); err != nil {
					return err
				}
			}
		}
		if err := validateAdmissionEconomicColumnsV119(db, model, false); err != nil {
			return err
		}
	}
	if err := migrateTables(db, model); err != nil {
		return err
	}
	if err := validateAdmissionEconomicColumnsV119(db, model, true); err != nil {
		return err
	}
	// The helper validates all existing rows before replacing frozen CHECKs;
	// MySQL DDL is not assumed to roll back transactionally.
	return replaceRuntimeArchiveChecksV112(db, model)
}
