package database

import "gorm.io/gorm"

// Version 37 only changes checks on released request columns. Frozen approval
// fields are shared by quota and rate requests without rewriting old history.
type projectRateRequestV37 struct {
	Kind                string `gorm:"check:ck_project_request_kind_v37,kind IN ('MODEL_ACCESS','QUOTA','RATE_LIMIT')"`
	DecisionRequestHash string `gorm:"check:ck_project_rate_approval,kind <> 'RATE_LIMIT' OR status <> 'approved' OR (decision_review_etag <> '' AND approved_policy_etag <> '' AND decision_request_hash <> '' AND approved_policy_json <> '')"`
}

func (projectRateRequestV37) TableName() string { return "project_model_requests" }

func projectRateRequestMigration(db *gorm.DB) error {
	model := &projectRateRequestV37{}
	// Install both guards before removing the released two-kind check. Every
	// interrupted MySQL DDL prefix remains conservative and can be resumed;
	// the distinct kind-check name distinguishes old and new definitions.
	for _, constraint := range []string{"ck_project_rate_approval", "ck_project_request_kind_v37"} {
		if !db.Migrator().HasConstraint(model, constraint) {
			if err := db.Migrator().CreateConstraint(model, constraint); err != nil {
				return err
			}
		}
	}
	if db.Migrator().HasConstraint(model, "ck_project_request_kind") {
		return db.Migrator().DropConstraint(model, "ck_project_request_kind")
	}
	return nil
}
