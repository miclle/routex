package entity

import "time"

// CredentialInferenceFailureState observes newly accepted immutable Attempts.
// Its sequence is database acceptance order, not upstream completion order.
type CredentialInferenceFailureState struct {
	CredentialDigest     string    `gorm:"primaryKey;size:64"`
	CredentialID         string    `gorm:"size:30;not null"`
	AcceptanceSequence   int64     `gorm:"not null"`
	ConsecutiveFailures  int       `gorm:"not null"`
	Latched              bool      `gorm:"not null"`
	LastAttemptID        string    `gorm:"size:64;not null;default:''"`
	LastSuccessAttemptID string    `gorm:"size:64;not null;default:''"`
	LastTriggerAttemptID string    `gorm:"size:64;not null;default:''"`
	UpdatedAt            time.Time `gorm:"precision:6;not null;autoUpdateTime:false"`
}

func (CredentialInferenceFailureState) TableName() string {
	return "credential_inference_failure_states"
}

// CredentialInferenceFailureEpisode is an immutable threshold source. Success
// rearms the observer without rewriting or removing a previously recorded episode.
type CredentialInferenceFailureEpisode struct {
	TriggerAttemptID     string    `gorm:"primaryKey;size:64;index:idx_credential_failure_episode_recorded,priority:2"`
	CredentialDigest     string    `gorm:"size:64;not null;uniqueIndex:idx_credential_failure_episode_sequence,priority:1"`
	CredentialID         string    `gorm:"size:30;not null"`
	AcceptanceSequence   int64     `gorm:"not null;uniqueIndex:idx_credential_failure_episode_sequence,priority:2"`
	FailureCount         int       `gorm:"not null"`
	LastSuccessAttemptID string    `gorm:"size:64;not null;default:''"`
	RecordedAt           time.Time `gorm:"precision:6;not null;index:idx_credential_failure_episode_recorded,priority:1"`
}

func (CredentialInferenceFailureEpisode) TableName() string {
	return "credential_inference_failure_episodes"
}
