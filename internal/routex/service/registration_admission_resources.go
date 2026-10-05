package service

import (
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// These control-plane reads retain exact relationship identity. Native dispatch
// consumes the immutable admitted-user map and performs no approval SQL.
func admittedProjectManagers(tx *gorm.DB, projectID, excludeID string) (int64, error) {
	var users []entity.User
	q := tx.Session(&gorm.Session{NewDB: true}).Table("users AS subject").Select("subject.*").Joins("JOIN project_managers AS manager ON manager.user_id = subject.id").Where(database.ExactTextColumns(tx, clause.Column{Table: "manager", Name: "user_id"}, clause.Column{Table: "subject", Name: "id"})).Where(database.ExactText(tx, clause.Column{Table: "manager", Name: "project_id"}, projectID))
	if excludeID != "" {
		q = q.Where(clause.Not(database.ExactText(tx, clause.Column{Table: "subject", Name: "id"}, excludeID)))
	}
	if err := q.Find(&users).Error; err != nil {
		return 0, err
	}
	apps, err := loadRegistrationApplications(tx, users)
	if err != nil {
		return 0, err
	}
	var n int64
	for _, u := range users {
		s, _ := registrationAdmission(u, apps)
		if s.AdmissionEligible {
			n++
		}
	}
	return n, nil
}
func admittedTeamOwners(tx *gorm.DB, teamID, excludeID string) (int64, error) {
	var users []entity.User
	q := tx.Session(&gorm.Session{NewDB: true}).Table("users AS subject").Select("subject.*").Joins("JOIN team_memberships AS member ON member.user_id = subject.id").Where(database.ExactTextColumns(tx, clause.Column{Table: "member", Name: "user_id"}, clause.Column{Table: "subject", Name: "id"})).Where(database.ExactText(tx, clause.Column{Table: "member", Name: "team_id"}, teamID)).Where(database.ExactText(tx, clause.Column{Table: "member", Name: "role"}, entity.TeamOwner)).Where(database.ExactText(tx, clause.Column{Table: "member", Name: "status"}, entity.ResourceActive))
	if excludeID != "" {
		q = q.Where(clause.Not(database.ExactText(tx, clause.Column{Table: "subject", Name: "id"}, excludeID)))
	}
	if err := q.Find(&users).Error; err != nil {
		return 0, err
	}
	apps, err := loadRegistrationApplications(tx, users)
	if err != nil {
		return 0, err
	}
	var n int64
	for _, u := range users {
		s, _ := registrationAdmission(u, apps)
		if s.AdmissionEligible {
			n++
		}
	}
	return n, nil
}

func admittedResourceUsers(tx *gorm.DB, ids []string) error {
	if err := activeResourceUsers(tx, ids); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := registrationAdmittedUser(tx, id, true); err != nil {
			return err
		}
	}
	return nil
}
