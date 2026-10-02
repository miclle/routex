package database

import "gorm.io/gorm"

// Version 33 stores explicit native terminal evidence independently of generic
// attempt status and usage. Old rows stay unknown, even if status was success.
type callNativeCompletionV33 struct {
	ID                       string `gorm:"primaryKey;size:64"`
	NativeCompletionEvidence string `gorm:"size:20;not null;default:unknown;check:ck_attempts_native_completion,native_completion_evidence IN ('unknown','completed','handoff','blocked','incomplete')"`
}

func (callNativeCompletionV33) TableName() string { return "call_attempts" }

func callNativeCompletionMigration(db *gorm.DB) error {
	model := &callNativeCompletionV33{}
	if !db.Migrator().HasColumn(model, "NativeCompletionEvidence") {
		if err := db.Migrator().AddColumn(model, "NativeCompletionEvidence"); err != nil {
			return err
		}
	}
	// The standard GORM check bounds the stored domain. Services enforce the
	// exact case-sensitive enum independently of database collation behavior.
	if !db.Migrator().HasConstraint(model, "ck_attempts_native_completion") {
		return db.Migrator().CreateConstraint(model, "ck_attempts_native_completion")
	}
	return nil
}
