package database

import (
	"fmt"
	"gorm.io/gorm"
	"time"
)

// V117 owns only additive command-to-installation acknowledgements. Parent
// schemas remain frozen and historical commands are never backfilled.
type runtimePublicationAcknowledgementV117 struct {
	RequestID      string                         `gorm:"primaryKey;size:36;check:ck_runtime_publication_ack_identity,OCTET_LENGTH(request_id) = 36 AND OCTET_LENGTH(installation_id) = 30" json:"-"`
	InstallationID string                         `gorm:"size:30;not null;index" json:"-"`
	AcknowledgedAt time.Time                      `gorm:"precision:6;not null;autoCreateTime:false;check:ck_runtime_publication_ack_time,acknowledged_at > '0001-01-01 00:00:00'" json:"-"`
	Command        *runtimePublicationCommandV105 `gorm:"belongsTo:Command;foreignKey:RequestID;references:RequestID;constraint:fk_runtime_publication_ack_command,OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
	Installation   *runtimeInstallationV100       `gorm:"belongsTo:Installation;foreignKey:InstallationID;references:ID;constraint:fk_runtime_publication_ack_installation,OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (runtimePublicationAcknowledgementV117) TableName() string {
	return "runtime_publication_acknowledgements"
}

func runtimePublicationAcknowledgementMigration(db *gorm.DB) error {
	model := &runtimePublicationAcknowledgementV117{}
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
				return fmt.Errorf("nonempty partial runtime acknowledgement table")
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
