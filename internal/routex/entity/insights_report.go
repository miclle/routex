package entity

import "time"

// InsightsReport retains a private definition or a cleared creation receipt.
// Deletion erases all parameters, while the intent digest prevents resurrection.
type InsightsReport struct {
	ID           string     `gorm:"primaryKey;size:30;index:idx_insights_report_owner,priority:2"`
	OwnerDigest  string     `gorm:"size:64;not null;uniqueIndex:idx_insights_report_intent,priority:1;index:idx_insights_report_owner,priority:1"`
	RequestID    string     `gorm:"size:36;not null;uniqueIndex:idx_insights_report_intent,priority:2"`
	IntentDigest string     `gorm:"size:64;not null"`
	Kind         string     `gorm:"size:20;not null"`
	From         string     `gorm:"column:range_from;size:30;not null"`
	To           string     `gorm:"column:range_to;size:30;not null"`
	Timezone     string     `gorm:"size:100;not null"`
	Dimension    string     `gorm:"size:6;not null;default:''"`
	ScopeKind    string     `gorm:"size:7;not null;default:''"`
	ScopeID      string     `gorm:"size:30;not null;default:''"`
	CreatedAt    time.Time  `gorm:"precision:6;not null;autoCreateTime:false"`
	DeletedAt    *time.Time `gorm:"precision:6"`
}

func (InsightsReport) TableName() string { return "insights_reports" }
