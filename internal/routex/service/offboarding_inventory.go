package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
)

type OffboardingKey struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Prefix string `json:"prefix"`
	Status string `json:"status"`
}
type OffboardingPerson struct {
	UserID   string `json:"user_id"`
	Name     string `json:"name"`
	Disabled bool   `json:"disabled"`
	Role     string `json:"role"`
	Status   string `json:"status"`
}
type OffboardingResource struct {
	ID                string              `json:"id"`
	Name              string              `json:"name"`
	Status            string              `json:"status"`
	RequiresSuccessor bool                `json:"requires_successor"`
	People            []OffboardingPerson `json:"people"`
}
type OffboardingCaseRecord struct {
	ID               string                 `json:"id"`
	RequestID        string                 `json:"request_id"`
	UserID           string                 `json:"user_id"`
	ActorID          string                 `json:"actor_id"`
	Mode             string                 `json:"mode"`
	Status           string                 `json:"status"`
	Reason           string                 `json:"reason"`
	PlannedAt        *time.Time             `json:"planned_at"`
	CreatedAt        time.Time              `json:"created_at"`
	CompletedAt      *time.Time             `json:"completed_at"`
	CompletedBy      string                 `json:"completed_by"`
	InventoryVersion string                 `json:"inventory_version"`
	Assignments      OffboardingAssignments `json:"assignments"`
}
type OffboardingInventory struct {
	UserID            string                  `json:"user_id"`
	OffboardedAt      *time.Time              `json:"offboarded_at"`
	Disabled          bool                    `json:"disabled"`
	LastAdministrator bool                    `json:"last_administrator"`
	InventoryVersion  string                  `json:"inventory_version"`
	PersonalKeys      []OffboardingKey        `json:"personal_keys"`
	Projects          []OffboardingResource   `json:"projects"`
	Teams             []OffboardingResource   `json:"teams"`
	Cases             []OffboardingCaseRecord `json:"cases"`
	// These relationships participate in stale-plan detection and are removed
	// at completion, but do not broaden the public responsibility inventory.
	memberships []entity.TeamMembership
	roles       []entity.UserRole
	userRole    string
}

func offboardingCaseRecord(row entity.OffboardingCase) (*OffboardingCaseRecord, error) {
	result := &OffboardingCaseRecord{ID: row.ID, RequestID: row.RequestID, UserID: row.UserID, ActorID: row.ActorID, Mode: row.Mode, Status: row.Status, Reason: row.Reason, PlannedAt: row.PlannedAt, CreatedAt: row.CreatedAt, CompletedAt: row.CompletedAt, CompletedBy: row.CompletedBy, InventoryVersion: row.InventoryVersion}
	if err := json.Unmarshal([]byte(row.AssignmentsJSON), &result.Assignments); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) OffboardingInventory(ctx context.Context, actorID, userID string) (*OffboardingInventory, error) {
	var result *OffboardingInventory
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeGovernance(tx, actorID, "members.read"); err != nil {
			return err
		}
		var err error
		result, err = loadOffboardingInventory(tx, userID)
		if err != nil {
			return err
		}
		var cases []entity.OffboardingCase
		if err := tx.Where("user_id = ?", userID).Order("created_at DESC, id DESC").Limit(100).Find(&cases).Error; err != nil {
			return err
		}
		for _, row := range cases {
			item, err := offboardingCaseRecord(row)
			if err != nil {
				return err
			}
			result.Cases = append(result.Cases, *item)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}

func loadOffboardingInventory(tx *gorm.DB, userID string) (*OffboardingInventory, error) {
	var user entity.User
	if err := tx.Select("id", "role", "disabled", "offboarded_at").First(&user, "id = ?", userID).Error; err != nil {
		return nil, err
	}
	result := &OffboardingInventory{UserID: userID, Disabled: user.Disabled, OffboardedAt: user.OffboardedAt, PersonalKeys: []OffboardingKey{}, Projects: []OffboardingResource{}, Teams: []OffboardingResource{}, Cases: []OffboardingCaseRecord{}, userRole: user.Role}
	if user.Role == entity.RoleAdmin && !user.Disabled {
		var admins int64
		if err := tx.Model(&entity.User{}).Where("role = ? AND disabled = ?", entity.RoleAdmin, false).Count(&admins).Error; err != nil {
			return nil, err
		}
		result.LastAdministrator = admins == 1
	}
	if err := tx.Model(&entity.APIKey{}).Select("id", "name", "prefix", "status").Where("user_id = ?", userID).Order("id").Scan(&result.PersonalKeys).Error; err != nil {
		return nil, err
	}
	var projects []entity.Project
	if err := tx.Table("projects p").Select("p.*").Joins("JOIN project_managers m ON m.project_id = p.id").Where("m.user_id = ?", userID).Order("p.id").Scan(&projects).Error; err != nil {
		return nil, err
	}
	for _, project := range projects {
		item := OffboardingResource{ID: project.ID, Name: project.Name, Status: project.Status, People: []OffboardingPerson{}}
		if err := tx.Table("project_managers m").Select("m.user_id,u.name,u.disabled").Joins("JOIN users u ON u.id = m.user_id").Where("m.project_id = ?", project.ID).Order("m.user_id").Scan(&item.People).Error; err != nil {
			return nil, err
		}
		item.RequiresSuccessor = project.Status != entity.ResourceArchived && !hasOffboardingSuccessor(item.People, userID, false)
		result.Projects = append(result.Projects, item)
	}
	if err := tx.Where("user_id = ?", userID).Order("team_id").Find(&result.memberships).Error; err != nil {
		return nil, err
	}
	for _, membership := range result.memberships {
		if membership.Role != entity.TeamOwner {
			continue
		}
		var team entity.Team
		if err := tx.First(&team, "id = ?", membership.TeamID).Error; err != nil {
			return nil, err
		}
		item := OffboardingResource{ID: team.ID, Name: team.Name, Status: team.Status, People: []OffboardingPerson{}}
		if err := tx.Table("team_memberships m").Select("m.user_id,u.name,u.disabled,m.role,m.status").Joins("JOIN users u ON u.id = m.user_id").Where("m.team_id = ?", team.ID).Order("m.user_id").Scan(&item.People).Error; err != nil {
			return nil, err
		}
		item.RequiresSuccessor = team.Status != entity.ResourceArchived && !hasOffboardingSuccessor(item.People, userID, true)
		result.Teams = append(result.Teams, item)
	}
	if err := tx.Where("user_id = ?", userID).Order("role_id").Find(&result.roles).Error; err != nil {
		return nil, err
	}
	// Canonical arrays use stable IDs, and session activity does not invalidate a
	// plan: all sessions are revoked from current state inside completion.
	sort.Slice(result.Teams, func(i, j int) bool { return result.Teams[i].ID < result.Teams[j].ID })
	raw, err := json.Marshal(struct {
		Inventory   *OffboardingInventory
		Memberships []entity.TeamMembership
		Roles       []entity.UserRole
		Role        string
	}{result, result.memberships, result.roles, result.userRole})
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	result.InventoryVersion = hex.EncodeToString(digest[:])
	return result, nil
}

func hasOffboardingSuccessor(people []OffboardingPerson, departing string, team bool) bool {
	for _, person := range people {
		if person.UserID != departing && !person.Disabled && (!team || (person.Role == entity.TeamOwner && person.Status == entity.ResourceActive)) {
			return true
		}
	}
	return false
}
