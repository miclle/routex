package entity

import "time"

// OffboardingNotificationRecipient binds a completed Case's explicit recipient
// to the original account birth. Later responsibility changes preserve history;
// another account reusing the ID cannot inherit its inbox or email delivery.
type OffboardingNotificationRecipient struct {
	CaseID             string    `gorm:"primaryKey;size:30"`
	RecipientID        string    `gorm:"primaryKey;size:30;uniqueIndex:idx_offboarding_notice_occurrence_recipient,priority:2"`
	OccurrenceID       string    `gorm:"size:30;not null;uniqueIndex:idx_offboarding_notice_occurrence_recipient,priority:1"`
	NotificationID     string    `gorm:"size:30;not null;uniqueIndex"`
	RecipientCreatedAt time.Time `gorm:"precision:6;not null"`
}

func (OffboardingNotificationRecipient) TableName() string {
	return "offboarding_notification_recipients"
}
