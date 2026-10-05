package service

import (
	"errors"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"regexp"
	"slices"
	"time"
)

var registrationApplicationPattern = regexp.MustCompile(`^raa_[0-7][0-9a-hjkmnp-tv-z]{25}$`)

func registrationApplicationID(s string) bool { return registrationApplicationPattern.MatchString(s) }

type RegistrationApprovalSummary struct {
	Status            string `json:"status"`
	AdmissionEligible bool   `json:"admission_eligible"`
}
type runtimeAdmissionProof struct {
	CreatedAt            time.Time
	ApplicationID        string
	ApplicationCreatedAt time.Time
	Revision, State      string
	Eligible             bool
}

func registrationApplicationValid(u entity.User, a entity.RegistrationApprovalApplication) bool {
	if u.ApprovalApplicationID == nil || !registrationApplicationID(*u.ApprovalApplicationID) || a.ID != *u.ApprovalApplicationID || a.UserID != u.ID || u.CreatedAt.IsZero() || !a.UserCreatedAt.Equal(u.CreatedAt) || a.CreatedAt.IsZero() || !validMemberRoleDigest(a.Revision) {
		return false
	}
	switch a.State {
	case "pending":
		return a.DecidedAt == nil && a.DecisionActorID == nil && a.DecisionReason == nil
	case "approved", "rejected":
		return a.DecidedAt != nil && !a.DecidedAt.IsZero() && a.DecisionActorID != nil && safeTeamSessionID(*a.DecisionActorID) && a.DecisionReason != nil && validRegistrationReason(*a.DecisionReason) && *a.DecisionReason != ""
	default:
		return false
	}
}

// The caller must load the private link explicitly; incomplete User projections
// cannot call this pure projection as proof of an unmanaged account.
func registrationAdmission(u entity.User, applications map[string]entity.RegistrationApprovalApplication) (RegistrationApprovalSummary, runtimeAdmissionProof) {
	p := runtimeAdmissionProof{CreatedAt: u.CreatedAt, Eligible: false}
	r := RegistrationApprovalSummary{Status: "unknown"}
	if !safeTeamSessionID(u.ID) || u.CreatedAt.IsZero() {
		return r, p
	}
	active := !u.Disabled && u.OffboardedAt == nil
	if u.ApprovalApplicationID == nil {
		r.Status = "not_required"
		r.AdmissionEligible = active
		p.State = "not_required"
		p.Eligible = active
		return r, p
	}
	p.ApplicationID = *u.ApprovalApplicationID
	a, ok := applications[p.ApplicationID]
	if !ok || !registrationApplicationValid(u, a) {
		return r, p
	}
	p.ApplicationCreatedAt = a.CreatedAt
	p.Revision = a.Revision
	p.State = a.State
	r.Status = a.State
	r.AdmissionEligible = active && a.State == "approved"
	p.Eligible = r.AdmissionEligible
	return r, p
}
func loadRegistrationApplications(tx *gorm.DB, users []entity.User) (map[string]entity.RegistrationApprovalApplication, error) {
	ids := []string{}
	for _, u := range users {
		if u.ApprovalApplicationID != nil && registrationApplicationID(*u.ApprovalApplicationID) {
			ids = append(ids, *u.ApprovalApplicationID)
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	result := map[string]entity.RegistrationApprovalApplication{}
	for start := 0; start < len(ids); start += 500 {
		end := min(start+500, len(ids))
		var rows []entity.RegistrationApprovalApplication
		q := tx.Session(&gorm.Session{NewDB: true}).Where("id IN ?", ids[start:end]).Limit(end - start + 1)
		if err := q.Find(&rows).Error; err != nil {
			return nil, err
		}
		if len(rows) > end-start {
			return nil, apperrors.ErrInternal
		}
		for _, a := range rows {
			if slices.Contains(ids[start:end], a.ID) {
				result[a.ID] = a
			}
		}
	}
	return result, nil
}
func requireRegistrationAdmission(tx *gorm.DB, u entity.User) error {
	apps, err := loadRegistrationApplications(tx, []entity.User{u})
	if err != nil {
		return err
	}
	r, _ := registrationAdmission(u, apps)
	if !r.AdmissionEligible {
		return apperrors.ErrUnauthorized
	}
	return nil
}

// Fresh complete SQL identity is required even when a caller supplied a User.
func registrationAdmittedUser(tx *gorm.DB, id string, lock bool) (entity.User, error) {
	var u entity.User
	q := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, id))
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && u.ID != id {
		return u, apperrors.ErrUnauthorized
	}
	if err != nil {
		return u, err
	}
	return u, requireRegistrationAdmission(tx, u)
}
func admittedAdministratorCount(tx *gorm.DB) (int64, error) {
	var users []entity.User
	q := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "role"}, entity.RoleAdmin)).Where("disabled = ? AND offboarded_at IS NULL", false)
	if err := q.Find(&users).Error; err != nil {
		return 0, err
	}
	apps, err := loadRegistrationApplications(tx, users)
	if err != nil {
		return 0, err
	}
	var n int64
	for _, u := range users {
		r, _ := registrationAdmission(u, apps)
		if r.AdmissionEligible {
			n++
		}
	}
	return n, nil
}

func createRegistrationApplication(tx *gorm.DB, u *entity.User) error {
	var persisted entity.User
	if err := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, u.ID)).Take(&persisted).Error; err != nil {
		return err
	}
	if persisted.ID != u.ID || persisted.CreatedAt.IsZero() || persisted.ApprovalApplicationID != nil {
		return apperrors.ErrInternal
	}
	applicationID, err := id.NewPrefixed("raa")
	if err != nil {
		return err
	}
	app := entity.RegistrationApprovalApplication{ID: applicationID, UserID: persisted.ID, UserCreatedAt: persisted.CreatedAt, State: "pending", Revision: memberRoleBaseline}
	if err := tx.Session(&gorm.Session{NewDB: true}).Omit("User").Create(&app).Error; err != nil {
		return err
	}
	result := tx.Session(&gorm.Session{NewDB: true}).Model(&entity.User{}).Where(database.ExactText(tx, clause.Column{Name: "id"}, persisted.ID)).Where("approval_application_id IS NULL").UpdateColumn("ApprovalApplicationID", app.ID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return apperrors.ErrInternal
	}
	persisted.ApprovalApplicationID = &app.ID
	*u = persisted
	return nil
}
