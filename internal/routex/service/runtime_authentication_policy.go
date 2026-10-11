package service

import (
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"time"
)

type runtimeAuthenticationPolicy struct {
	Enforced                bool
	Revision                string
	EmergencyAdminID        string
	EmergencyAdminCreatedAt *time.Time
}

func runtimePolicy(p entity.GovernanceSetting) runtimeAuthenticationPolicy {
	return runtimeAuthenticationPolicy{Enforced: p.ForcedSSOEnabled, Revision: p.AuthenticationPolicyRevision, EmergencyAdminID: p.EmergencyAdminID, EmergencyAdminCreatedAt: p.EmergencyAdminCreatedAt}
}
func loadAuthenticationRuntimeData(tx *gorm.DB, data *teamSessionRuntimeData) error {
	p, e := authenticationPolicy(tx)
	if e != nil {
		return e
	}
	data.AuthenticationPolicy = p
	data.EmergencyUsers = map[string]entity.User{}
	data.EmergencyMFA = map[string]entity.UserMFA{}
	if !p.ForcedSSOEnabled {
		return nil
	}
	var users []entity.User
	if e = tx.Session(&gorm.Session{NewDB: true}).Select("id", "role", "disabled", "offboarded_at", "created_at", "password_hash").Where("id = ?", p.EmergencyAdminID).Find(&users).Error; e != nil {
		return e
	}
	for _, u := range users {
		if u.ID == p.EmergencyAdminID {
			data.EmergencyUsers[u.ID] = u
		}
	}
	var states []entity.UserMFA
	if e = tx.Session(&gorm.Session{NewDB: true}).Select("user_id", "enabled", "generation").Where("user_id = ?", p.EmergencyAdminID).Find(&states).Error; e != nil {
		return e
	}
	for _, m := range states {
		if m.UserID == p.EmergencyAdminID {
			data.EmergencyMFA[m.UserID] = m
		}
	}
	return nil
}
func authenticationRuntimeSession(data *teamSessionRuntimeData, row entity.Session, now time.Time) bool {
	p := data.AuthenticationPolicy
	if !authenticationMethodAllowed(p, row.PrimaryMethod) {
		return false
	}
	if row.PrimaryMethod != "emergency" {
		return emergencyProofEmpty(row)
	}
	u, ok := data.EmergencyUsers[row.UserID]
	m, exists := data.EmergencyMFA[row.UserID]
	return ok && exists && emergencyCurrent(p, row, u, m, now, false)
}

// The latest committed policy fences held snapshots even when publication fails.
// An old off snapshot cannot revive local authority after an on/off cycle.
func (s *Service) runtimeAuthenticationPolicyDenied(auth *runtimeAuthorization, row runtimeTeamSession) bool {
	if row.PrimaryMethod == "oidc" || row.PrimaryMethod == "ldap" {
		return false
	}
	if s.runtime == nil {
		return true
	}
	p := auth.AuthenticationPolicy
	if latest := s.runtime.authenticationPolicyFence.Load(); latest != nil {
		if latest.Revision != p.Revision {
			return true
		}
		p = *latest
	}
	if p.Enforced {
		return row.PrimaryMethod != "emergency" || row.EmergencyPolicyRevision != p.Revision || row.UserID != p.EmergencyAdminID || !authenticationInstantEqual(row.EmergencyUserCreatedAt, p.EmergencyAdminCreatedAt)
	}
	return row.PrimaryMethod == "emergency"
}
