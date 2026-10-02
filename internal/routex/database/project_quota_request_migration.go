package database

import "gorm.io/gorm"

// Version 36 extends the released request table without rewriting historical
// model requests. Approved quota policy and review intent remain immutable.
type projectQuotaRequestV36 struct {
	ID                  string `gorm:"primaryKey;size:30"`
	Kind                string `gorm:"size:20;not null;default:MODEL_ACCESS;check:ck_project_request_kind,kind IN ('MODEL_ACCESS','QUOTA')"`
	BaselinePolicyETag  string `gorm:"column:baseline_policy_etag;size:64;not null;default:''"`
	DecisionReviewETag  string `gorm:"column:decision_review_etag;size:64;not null;default:''"`
	ApprovedPolicyETag  string `gorm:"column:approved_policy_etag;size:64;not null;default:''"`
	DecisionRequestHash string `gorm:"size:64;not null;default:''"`
	ApprovedPolicyJSON  string `gorm:"type:text;not null;default:('');check:ck_project_quota_approval,kind <> 'QUOTA' OR status <> 'approved' OR (decision_review_etag <> '' AND approved_policy_etag <> '' AND decision_request_hash <> '' AND approved_policy_json <> '')"`
}

func (projectQuotaRequestV36) TableName() string { return "project_model_requests" }

func projectQuotaRequestMigration(db *gorm.DB) error {
	model := &projectQuotaRequestV36{}
	for _, column := range []string{"Kind", "BaselinePolicyETag", "DecisionReviewETag", "ApprovedPolicyETag", "DecisionRequestHash", "ApprovedPolicyJSON"} {
		if !db.Migrator().HasColumn(model, column) {
			if err := db.Migrator().AddColumn(model, column); err != nil {
				return err
			}
		}
	}
	// Expression defaults are supported by both drivers, including TEXT on
	// MySQL. Re-entry repairs checks after partially committed additive DDL.
	for _, constraint := range []string{"ck_project_request_kind", "ck_project_quota_approval"} {
		if !db.Migrator().HasConstraint(model, constraint) {
			if err := db.Migrator().CreateConstraint(model, constraint); err != nil {
				return err
			}
		}
	}
	return nil
}
