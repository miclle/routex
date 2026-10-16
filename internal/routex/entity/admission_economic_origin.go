package entity

// AdmissionEconomicOrigin records a future explicitly established immutable
// calendar and coverage origin. Its body/digest proves consistency, not source
// authenticity, startup exclusion, imported history or activation. No writer is
// currently provided, and migration never seeds this record.
type AdmissionEconomicOrigin struct {
	InstallationID           string                   `gorm:"primaryKey;size:30;check:ck_economic_origin_identity,OCTET_LENGTH(installation_id) = 30" json:"-"`
	FormatVersion            int64                    `gorm:"not null;check:ck_economic_origin_format,format_version = 1" json:"-"`
	SourceKind               string                   `gorm:"size:16;not null;check:ck_economic_origin_source,OCTET_LENGTH(source_kind) = 5 AND ASCII(SUBSTRING(source_kind,1,1)) = 102 AND ASCII(SUBSTRING(source_kind,2,1)) = 114 AND ASCII(SUBSTRING(source_kind,3,1)) = 101 AND ASCII(SUBSTRING(source_kind,4,1)) = 115 AND ASCII(SUBSTRING(source_kind,5,1)) = 104" json:"-"`
	TimeZone                 string                   `gorm:"size:100;not null;check:ck_economic_origin_calendar,OCTET_LENGTH(time_zone) BETWEEN 1 AND 100 AND OCTET_LENGTH(calendar_revision_at_freeze) BETWEEN 1 AND 64" json:"-"`
	CalendarRevisionAtFreeze string                   `gorm:"size:64;not null" json:"-"`
	CoverageStart            int64                    `gorm:"not null;check:ck_economic_origin_time,coverage_start > 0 AND coverage_start <= 9222767236854775807 AND clock_high_water_at_freeze = coverage_start AND boundary_at = coverage_start" json:"-"`
	ClockHighWaterAtFreeze   int64                    `gorm:"not null" json:"-"`
	BoundaryAt               int64                    `gorm:"not null" json:"-"`
	EvidenceDigest           string                   `gorm:"size:64;not null;check:ck_economic_origin_digest,OCTET_LENGTH(evidence_digest) = 64" json:"-"`
	EvidenceBody             []byte                   `gorm:"not null;check:ck_economic_origin_body,OCTET_LENGTH(evidence_body) BETWEEN 1 AND 131072" json:"-"`
	Domain                   *AdmissionEconomicDomain `gorm:"belongsTo:Domain;foreignKey:InstallationID;references:InstallationID;constraint:fk_economic_origin_domain,OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (AdmissionEconomicOrigin) TableName() string { return "admission_economic_origins" }
