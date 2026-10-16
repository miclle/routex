package entity

import "time"

// RuntimePublicationAcknowledgement binds one command to actual installed
// routing and authorization in its claiming process. It is not a fleet receipt.
type RuntimePublicationAcknowledgement struct {
	RequestID      string                          `gorm:"primaryKey;size:36;check:ck_runtime_publication_ack_identity,OCTET_LENGTH(request_id) = 36 AND OCTET_LENGTH(installation_id) = 30" json:"-"`
	InstallationID string                          `gorm:"size:30;not null;index" json:"-"`
	AcknowledgedAt time.Time                       `gorm:"precision:6;not null;autoCreateTime:false;check:ck_runtime_publication_ack_time,acknowledged_at > '0001-01-01 00:00:00'" json:"-"`
	Command        *RuntimePublicationCommand      `gorm:"belongsTo:Command;foreignKey:RequestID;references:RequestID;constraint:fk_runtime_publication_ack_command,OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
	Installation   *RuntimeInstallationObservation `gorm:"belongsTo:Installation;foreignKey:InstallationID;references:ID;constraint:fk_runtime_publication_ack_installation,OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (RuntimePublicationAcknowledgement) TableName() string {
	return "runtime_publication_acknowledgements"
}
