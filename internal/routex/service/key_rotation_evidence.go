package service

import (
	"time"

	"gorm.io/gorm"
)

type replacementKeyCallEvidence struct {
	RequestID                string
	AttemptRequestID         string
	UserID                   string
	ProjectID                string
	KeyID                    string
	CallStatus               string
	AttemptStatus            string
	NativeCompletionEvidence string
	StartedAt                time.Time
	AttemptNumber            int
}

// hasCompletedReplacementKeyCall requires an immutable terminal attempt with
// explicit native completion. Generic HTTP/call success and known usage are not
// proof of inference completion. It reads one latest eligible projection only;
// a case-folded or corrupt candidate conservatively rejects until fresh proof.
func hasCompletedReplacementKeyCall(tx *gorm.DB, userID, projectID, keyID string, createdAt time.Time) (bool, error) {
	if (userID == "") == (projectID == "") || keyID == "" || createdAt.IsZero() {
		return false, nil
	}
	later := tx.Table("call_attempts AS later").Select("1").Where("later.request_id = attempt.request_id AND later.attempt_number > attempt.attempt_number")
	var evidence replacementKeyCallEvidence
	result := tx.Table("call_records AS calls").
		Select("calls.request_id, attempt.request_id AS attempt_request_id, calls.user_id, calls.project_id, calls.key_id, calls.status AS call_status, calls.started_at, attempt.status AS attempt_status, attempt.native_completion_evidence, attempt.attempt_number").
		Joins("JOIN call_attempts AS attempt ON attempt.request_id = calls.request_id").
		Where("calls.user_id = ? AND calls.project_id = ? AND calls.key_id = ?", userID, projectID, keyID).
		Where("calls.status = ? AND calls.started_at >= ? AND attempt.status = ? AND attempt.native_completion_evidence = ?", "success", createdAt, "success", "completed").
		Where("NOT EXISTS (?)", later).
		Order("calls.completed_at DESC, calls.request_id DESC, attempt.id DESC").Limit(1).Find(&evidence)
	if result.Error != nil || result.RowsAffected == 0 {
		return false, result.Error
	}
	return evidence.matches(userID, projectID, keyID, createdAt), nil
}

func (evidence replacementKeyCallEvidence) matches(userID, projectID, keyID string, createdAt time.Time) bool {
	// Database collation may fold candidate comparisons; require exact
	// immutable IDs and enums after the bounded lookup.
	return evidence.RequestID != "" && evidence.RequestID == evidence.AttemptRequestID && evidence.UserID == userID && evidence.ProjectID == projectID && evidence.KeyID == keyID &&
		evidence.CallStatus == "success" && evidence.AttemptStatus == "success" && evidence.NativeCompletionEvidence == "completed" &&
		!evidence.StartedAt.Before(createdAt) && evidence.AttemptNumber >= 1 && evidence.AttemptNumber <= 32
}
