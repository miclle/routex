package entity

import "time"

// AdmissionMode is an unwired installation-scoped authority selection. A local
// row is not a shared-mode activation; transitions require a future cutover.
type AdmissionMode struct {
	ID             int                `gorm:"primaryKey;autoIncrement:false;check:ck_admission_mode_singleton,id = 1" json:"-"`
	InstallationID string             `gorm:"size:64;not null;check:ck_admission_mode_identity,OCTET_LENGTH(installation_id) = 30" json:"-"`
	Mode           string             `gorm:"size:16;not null;default:local;check:ck_admission_mode_value,(OCTET_LENGTH(mode) = 5 AND ASCII(SUBSTRING(mode,1,1)) = 108 AND ASCII(SUBSTRING(mode,2,1)) = 111 AND ASCII(SUBSTRING(mode,3,1)) = 99 AND ASCII(SUBSTRING(mode,4,1)) = 97 AND ASCII(SUBSTRING(mode,5,1)) = 108) OR (OCTET_LENGTH(mode) = 6 AND ASCII(SUBSTRING(mode,1,1)) = 115 AND ASCII(SUBSTRING(mode,2,1)) = 104 AND ASCII(SUBSTRING(mode,3,1)) = 97 AND ASCII(SUBSTRING(mode,4,1)) = 114 AND ASCII(SUBSTRING(mode,5,1)) = 101 AND ASCII(SUBSTRING(mode,6,1)) = 100)" json:"-"`
	Revision       int64              `gorm:"not null;default:1;check:ck_admission_mode_revision,revision > 0" json:"-"`
	Installation   *LimitInstallation `gorm:"belongsTo:Installation;foreignKey:ID;references:ID;constraint:fk_admission_mode_installation,OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (AdmissionMode) TableName() string { return "admission_modes" }

// AdmissionSpoolOwner retains one registration and at most one terminal fence.
// Neither a fence nor process lease metadata proves executor/transport closure.
type AdmissionSpoolOwner struct {
	InstallationID    string         `gorm:"primaryKey;size:64;check:ck_admission_owner_identity,OCTET_LENGTH(installation_id) = 30 AND OCTET_LENGTH(spool_id) BETWEEN 1 AND 64 AND OCTET_LENGTH(incarnation_id) = 30 AND OCTET_LENGTH(registration_id) = 36" json:"-"`
	SpoolID           string         `gorm:"primaryKey;size:64" json:"-"`
	ModeID            int            `gorm:"not null;check:ck_admission_owner_mode,mode_id = 1 AND mode_revision > 0" json:"-"`
	ModeRevision      int64          `gorm:"not null" json:"-"`
	RegistrationID    string         `gorm:"size:36;not null;uniqueIndex:uidx_admission_owner_registration" json:"-"`
	IncarnationID     string         `gorm:"size:30;not null" json:"-"`
	InstanceStartedAt time.Time      `gorm:"precision:6;not null;autoCreateTime:false;check:ck_admission_owner_time,instance_started_at > '0001-01-01 00:00:00' AND registered_at >= instance_started_at" json:"-"`
	RegisteredAt      time.Time      `gorm:"precision:6;not null;autoCreateTime:false" json:"-"`
	RegisteredFence   int64          `gorm:"not null;check:ck_admission_owner_fence,registered_fence = 1 AND ((current_fence = 1 AND fencing_id IS NULL AND fenced_at IS NULL) OR (current_fence = 2 AND fencing_id IS NOT NULL AND OCTET_LENGTH(fencing_id) = 36 AND fenced_at IS NOT NULL AND fenced_at >= registered_at))" json:"-"`
	CurrentFence      int64          `gorm:"not null" json:"-"`
	FencingID         *string        `gorm:"size:36;uniqueIndex:uidx_admission_owner_fencing" json:"-"`
	FencedAt          *time.Time     `gorm:"precision:6" json:"-"`
	Mode              *AdmissionMode `gorm:"belongsTo:Mode;foreignKey:ModeID;references:ID;constraint:fk_admission_owner_mode,OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (AdmissionSpoolOwner) TableName() string { return "admission_spool_owners" }
