package entity

import "time"

const (
	ResourceActive   = "active"
	ResourceDisabled = "disabled"
	ResourceArchived = "archived"
	TeamOwner        = "owner"
	TeamMember       = "member"
)

type Team struct {
	ID          string `gorm:"primaryKey;size:30"`
	Name        string `gorm:"size:100;not null"`
	Description string `gorm:"size:2000;not null"`
	Status      string `gorm:"size:20;not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
type TeamMembership struct {
	JoinedAt *time.Time `json:"-"`
	ID       string     `gorm:"primaryKey;size:30"`
	TeamID   string     `gorm:"size:30;not null;uniqueIndex:uq_team_member"`
	UserID   string     `gorm:"size:30;not null;uniqueIndex:uq_team_member"`
	Role     string     `gorm:"size:20;not null"`
	Status   string     `gorm:"size:20;not null"`
}
type TeamModelGrant struct {
	SourceRequestID *string `gorm:"column:source_request_id;size:30;check:ck_team_model_grant_source,source_request_id IS NULL OR (CHAR_LENGTH(source_request_id) >= 1 AND CHAR_LENGTH(source_request_id) <= 30)"`
	TeamID          string  `gorm:"primaryKey;size:30"`
	ModelID         string  `gorm:"primaryKey;size:30"`
}
type Project struct {
	ID          string `gorm:"primaryKey;size:30"`
	Name        string `gorm:"size:100;not null"`
	Description string `gorm:"size:2000;not null"`
	Status      string `gorm:"size:20;not null"`
	CreatorID   string `gorm:"size:30;not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
type ProjectManager struct {
	ID        string `gorm:"primaryKey;size:30"`
	ProjectID string `gorm:"size:30;not null;uniqueIndex:uq_project_manager"`
	UserID    string `gorm:"size:30;not null;uniqueIndex:uq_project_manager"`
}
type ProjectModelGrant struct {
	ProjectID string `gorm:"primaryKey;size:30"`
	ModelID   string `gorm:"primaryKey;size:30"`
}
