package handler

import (
	"errors"
	"reflect"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// This frozen test-only predicate reconstructs V110 without altering its released
// migration. Downgrading an exclusively owned fixture never deletes report rows.
type insightsReportDefinitionCheckV110 struct {
	Kind string `gorm:"size:20;not null;check:ck_insights_report_definition,(deleted_at IS NULL AND ((OCTET_LENGTH(kind) = 15 AND ASCII(SUBSTRING(kind,1,1)) = 116 AND ASCII(SUBSTRING(kind,2,1)) = 111 AND ASCII(SUBSTRING(kind,3,1)) = 107 AND ASCII(SUBSTRING(kind,4,1)) = 101 AND ASCII(SUBSTRING(kind,5,1)) = 110 AND ASCII(SUBSTRING(kind,6,1)) = 115 AND ASCII(SUBSTRING(kind,7,1)) = 95 AND ASCII(SUBSTRING(kind,8,1)) = 98 AND ASCII(SUBSTRING(kind,9,1)) = 121 AND ASCII(SUBSTRING(kind,10,1)) = 95 AND ASCII(SUBSTRING(kind,11,1)) = 109 AND ASCII(SUBSTRING(kind,12,1)) = 111 AND ASCII(SUBSTRING(kind,13,1)) = 100 AND ASCII(SUBSTRING(kind,14,1)) = 101 AND ASCII(SUBSTRING(kind,15,1)) = 108) OR (OCTET_LENGTH(kind) = 16 AND ASCII(SUBSTRING(kind,1,1)) = 112 AND ASCII(SUBSTRING(kind,2,1)) = 114 AND ASCII(SUBSTRING(kind,3,1)) = 111 AND ASCII(SUBSTRING(kind,4,1)) = 118 AND ASCII(SUBSTRING(kind,5,1)) = 105 AND ASCII(SUBSTRING(kind,6,1)) = 100 AND ASCII(SUBSTRING(kind,7,1)) = 101 AND ASCII(SUBSTRING(kind,8,1)) = 114 AND ASCII(SUBSTRING(kind,9,1)) = 95 AND ASCII(SUBSTRING(kind,10,1)) = 113 AND ASCII(SUBSTRING(kind,11,1)) = 117 AND ASCII(SUBSTRING(kind,12,1)) = 97 AND ASCII(SUBSTRING(kind,13,1)) = 108 AND ASCII(SUBSTRING(kind,14,1)) = 105 AND ASCII(SUBSTRING(kind,15,1)) = 116 AND ASCII(SUBSTRING(kind,16,1)) = 121)) AND OCTET_LENGTH(range_from) BETWEEN 20 AND 30 AND OCTET_LENGTH(range_to) BETWEEN 20 AND 30 AND OCTET_LENGTH(timezone) BETWEEN 1 AND 100) OR (deleted_at IS NOT NULL AND OCTET_LENGTH(kind) = 0 AND OCTET_LENGTH(range_from) = 0 AND OCTET_LENGTH(range_to) = 0 AND OCTET_LENGTH(timezone) = 0)"`
}

func (insightsReportDefinitionCheckV110) TableName() string { return "insights_reports" }

func insightsReportsPeelV111(db *gorm.DB, expected []personalKeyBehaviorMigrationEntry) error {
	if len(expected) < 1 || len(expected) > 118 {
		return errors.New("unsupported quota report fixture head")
	}
	for i, row := range expected {
		if row.Version != i+1 || row.AppliedAt == "" {
			return errors.New("invalid quota report fixture receipts")
		}
	}
	var actual []personalKeyBehaviorMigrationEntry
	if err := db.Table("schema_migrations").Order("version").Find(&actual).Error; err != nil || !reflect.DeepEqual(expected, actual) {
		return errors.New("quota report fixture receipt mismatch")
	}
	if len(expected) < 111 {
		return nil
	}
	model := &entity.InsightsReport{}
	for _, field := range []string{"Dimension", "ScopeKind", "ScopeID"} {
		if !db.Migrator().HasColumn(model, field) {
			return errors.New("incomplete quota report fixture schema")
		}
	}
	if !db.Migrator().HasConstraint(model, "ck_insights_report_definition_v111") {
		return errors.New("missing quota report fixture guard")
	}
	var count int64
	if err := db.Model(model).Where("OCTET_LENGTH(dimension) <> 0 OR OCTET_LENGTH(scope_kind) <> 0 OR OCTET_LENGTH(scope_id) <> 0").Count(&count).Error; err != nil || count != 0 {
		return errors.New("retained quota report definition prevents historical peel")
	}
	if len(expected) == 112 || len(expected) == 113 || len(expected) == 114 || len(expected) == 115 || len(expected) == 116 || len(expected) == 117 || len(expected) == 118 {
		if err := runtimeArchivePeelV112(db, expected); err != nil {
			return err
		}
		expected = expected[:111]
	}
	// Install the old predicate first: a refusal must not leave an unguarded table.
	if !db.Migrator().HasConstraint(model, "ck_insights_report_definition") {
		if err := db.Migrator().CreateConstraint(&insightsReportDefinitionCheckV110{}, "ck_insights_report_definition"); err != nil {
			return err
		}
	}
	if err := db.Migrator().DropConstraint(model, "ck_insights_report_definition_v111"); err != nil {
		return err
	}
	for _, field := range []string{"ScopeID", "ScopeKind", "Dimension"} {
		if err := db.Migrator().DropColumn(model, field); err != nil {
			return err
		}
	}
	removed := db.Table("schema_migrations").Where("version = ? AND applied_at = ?", 111, expected[110].AppliedAt).Delete(&struct{}{})
	if removed.Error != nil || removed.RowsAffected != 1 {
		return errors.New("exact quota report receipt removal failed")
	}
	actual = nil
	if err := db.Table("schema_migrations").Order("version").Find(&actual).Error; err != nil || !reflect.DeepEqual(actual, expected[:110]) {
		return errors.New("quota report peel changed retained prefix")
	}
	return nil
}
