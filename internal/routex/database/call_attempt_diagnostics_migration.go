package database

import "gorm.io/gorm"

// Version 25 adds normalized route-stop and attempt evidence. Conservative
// defaults keep historical rows from claiming that an upstream replay was safe.
type callDiagnosticsRecordV25 struct {
	RequestID       string `gorm:"primaryKey;size:64"`
	RouteStopReason string `gorm:"size:40;not null;default:''"`
}

func (callDiagnosticsRecordV25) TableName() string { return "call_records" }

type callDiagnosticsAttemptV25 struct {
	ID              string `gorm:"primaryKey;size:64"`
	AttemptNumber   int    `gorm:"not null;default:0"`
	FailureClass    string `gorm:"size:40;not null;default:permanent_failure"`
	WorkEvidence    string `gorm:"size:40;not null;default:unknown"`
	OutputStarted   bool   `gorm:"not null;default:false"`
	FinalUsageKnown bool   `gorm:"not null;default:false"`
	EvidenceCode    string `gorm:"size:40;not null;default:''"`
}

func (callDiagnosticsAttemptV25) TableName() string { return "call_attempts" }

func callAttemptDiagnosticsMigration(db *gorm.DB) error {
	for _, field := range []string{"RouteStopReason"} {
		if !db.Migrator().HasColumn(&callDiagnosticsRecordV25{}, field) {
			if err := db.Migrator().AddColumn(&callDiagnosticsRecordV25{}, field); err != nil {
				return err
			}
		}
	}
	for _, field := range []string{"AttemptNumber", "FailureClass", "WorkEvidence", "OutputStarted", "FinalUsageKnown", "EvidenceCode"} {
		if !db.Migrator().HasColumn(&callDiagnosticsAttemptV25{}, field) {
			if err := db.Migrator().AddColumn(&callDiagnosticsAttemptV25{}, field); err != nil {
				return err
			}
		}
	}
	return nil
}
