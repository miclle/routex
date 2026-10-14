package database

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// V116 freezes only rollback intent and restrictive references. It never copies
// historical catalog, credential, authorization or verification state.
type runtimeArchiveRollbackV116 struct {
	RequestID        string                           `gorm:"primaryKey;size:36;check:ck_archive_rollback_identity,OCTET_LENGTH(request_id) = 36 AND OCTET_LENGTH(actor_id) = 30 AND OCTET_LENGTH(archive_id) = 30 AND OCTET_LENGTH(review_etag) = 64 AND OCTET_LENGTH(input_digest) = 64 AND OCTET_LENGTH(desired_digest) = 64" json:"-"`
	ActorID          string                           `gorm:"size:30;not null;index" json:"-"`
	ActorCreatedAt   time.Time                        `gorm:"precision:6;not null;autoCreateTime:false" json:"-"`
	ArchiveID        string                           `gorm:"size:30;not null;index" json:"-"`
	ArchiveCreatedAt time.Time                        `gorm:"precision:6;not null;autoCreateTime:false" json:"-"`
	ReviewETag       string                           `gorm:"column:review_etag;size:64;not null" json:"-"`
	InputDigest      string                           `gorm:"size:64;not null" json:"-"`
	DesiredDigest    string                           `gorm:"size:64;not null" json:"-"`
	Reason           string                           `gorm:"type:text;not null;check:ck_archive_rollback_reason,OCTET_LENGTH(reason) BETWEEN 1 AND 4000" json:"-"`
	Changed          bool                             `gorm:"not null" json:"-"`
	AppliedAt        time.Time                        `gorm:"precision:6;not null;autoCreateTime:false;check:ck_archive_rollback_time,actor_created_at > '0001-01-01 00:00:00' AND archive_created_at > '0001-01-01 00:00:00' AND applied_at >= actor_created_at AND applied_at >= archive_created_at" json:"-"`
	Archive          *runtimeConfigurationArchiveV112 `gorm:"belongsTo:Archive;foreignKey:ArchiveID;references:ID;constraint:fk_archive_rollback_archive,OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
	Publication      *runtimePublicationCommandV105   `gorm:"belongsTo:Publication;foreignKey:RequestID;references:RequestID;constraint:fk_archive_rollback_publication,OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (runtimeArchiveRollbackV116) TableName() string { return "runtime_archive_rollbacks" }

func runtimeArchiveRollbackMigration(db *gorm.DB) error {
	model := &runtimeArchiveRollbackV116{}
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
				return fmt.Errorf("nonempty partial routing rollback table")
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
