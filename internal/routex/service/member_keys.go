package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

var memberKeyID = regexp.MustCompile(`^key_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
var memberKeyUserID = regexp.MustCompile(`^usr_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
var memberKeyRevision = regexp.MustCompile(`^kvr_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
var memberKeyStrongETag = regexp.MustCompile(`^[0-9a-f]{64}$`)

const memberKeyReadBound = 8192

// MemberKeyRecord is an administrative projection, never a personal credential
// response. Its model IDs are immutable ceilings, not effective calling grants.
type MemberKeyRecord struct {
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	Status          string                `json:"status"`
	Expired         bool                  `json:"expired"`
	ModelIDs        []string              `json:"model_ids"`
	ExpiresAt       *time.Time            `json:"expires_at"`
	CreatedAt       time.Time             `json:"created_at"`
	UpdatedAt       time.Time             `json:"updated_at"`
	ETag            string                `json:"etag"`
	DisableEligible bool                  `json:"disable_eligible"`
	LastUsedAt      *time.Time            `json:"last_used_at"`
	LastUseCoverage string                `json:"last_use_coverage"`
	Limits          *MemberKeyLimitRecord `json:"limits"`
}

type MemberKeyFilter struct {
	Cursor string
	Limit  int
}

type MemberKeyPage struct {
	Items      []MemberKeyRecord `json:"items"`
	NextCursor *string           `json:"next_cursor"`
}

func memberKeyETag(key entity.APIKey, models []string) string {
	ids := slices.Clone(models)
	slices.Sort(ids)
	var expiresAt *string
	if key.ExpiresAt != nil {
		value := key.ExpiresAt.UTC().Format(time.RFC3339Nano)
		expiresAt = &value
	}
	raw, _ := json.Marshal(struct {
		ID, UserID, Revision, Name, Status string
		ExpiresAt                          *string
		Models                             []string
	}{key.ID, key.UserID, key.LifecycleRevision, key.Name, key.Status, expiresAt, ids})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func memberKeyDisableAllowed(actor, subject entity.User, key entity.APIKey, now time.Time) bool {
	return (actor.Role == entity.RoleAdmin || actor.Role == entity.RoleMember) && (subject.Role == entity.RoleAdmin || subject.Role == entity.RoleMember) && !actor.Disabled && actor.OffboardedAt == nil && !subject.Disabled && subject.OffboardedAt == nil &&
		(actor.Role == entity.RoleAdmin || (actor.ID != subject.ID && subject.Role != entity.RoleAdmin)) &&
		key.UserID == subject.ID && key.Status == entity.KeyActive && (key.ExpiresAt == nil || now.Before(*key.ExpiresAt)) && memberKeyRevision.MatchString(key.LifecycleRevision)
}

func memberKeyPeople(db *gorm.DB, actorID, subjectID, permission string) (entity.User, entity.User, error) {
	var actor, subject entity.User
	if !memberKeyUserID.MatchString(actorID) || !memberKeyUserID.MatchString(subjectID) {
		return actor, subject, apperrors.ErrNotFound
	}
	if err := db.Select("id", "role", "disabled", "offboarded_at").First(&actor, "id = ?", actorID).Error; err != nil {
		return actor, subject, err
	}
	if actor.ID != actorID {
		return actor, subject, apperrors.ErrForbidden
	}
	permissions, err := memberKeyPermissions(db, actor)
	if err != nil {
		return actor, subject, err
	}
	if !slices.Contains(permissions, permission) {
		return actor, subject, apperrors.ErrForbidden
	}
	if err := db.Select("id", "role", "disabled", "offboarded_at").First(&subject, "id = ?", subjectID).Error; err != nil {
		return actor, subject, err
	}
	if subject.ID != subjectID {
		return actor, subject, apperrors.ErrNotFound
	}
	return actor, subject, nil
}

func readMemberOwnedKeys(db *gorm.DB, subjectID string) ([]entity.APIKey, map[string]string, error) {
	var keys []entity.APIKey
	if err := db.Omit("token_hash", "prefix").Where("user_id = ?", subjectID).Order("id DESC").Limit(memberKeyReadBound + 1).Find(&keys).Error; err != nil {
		return nil, nil, err
	}
	if len(keys) > memberKeyReadBound {
		return nil, nil, &apperrors.Error{Code: 400, Message: "member Key projection exceeds its complete-query bound"}
	}
	for _, key := range keys {
		if key.UserID != subjectID || !memberKeyID.MatchString(key.ID) {
			return nil, nil, apperrors.ErrNotFound
		}
	}
	roots, err := personalLimitRoots(keys)
	return keys, roots, err
}

func (s *Service) ListMemberKeys(ctx context.Context, actorID, subjectID string, filter MemberKeyFilter) (*MemberKeyPage, error) {
	if filter.Limit == 0 {
		filter.Limit = 40
	}
	if filter.Limit < 1 || filter.Limit > 100 || (filter.Cursor != "" && !memberKeyID.MatchString(filter.Cursor)) {
		return nil, apperrors.ErrBadRequest
	}
	var result *MemberKeyPage
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, subject, err := memberKeyPeople(tx, actorID, subjectID, "members.read")
		if err != nil {
			return err
		}
		keys, roots, err := readMemberOwnedKeys(tx, subjectID)
		if err != nil {
			return err
		}
		if filter.Cursor != "" && !slices.ContainsFunc(keys, func(key entity.APIKey) bool { return key.ID == filter.Cursor }) {
			return apperrors.ErrNotFound
		}
		selected := make([]entity.APIKey, 0, filter.Limit+1)
		for _, key := range keys {
			if filter.Cursor == "" || key.ID < filter.Cursor {
				selected = append(selected, key)
				if len(selected) == filter.Limit+1 {
					break
				}
			}
		}
		result = &MemberKeyPage{Items: []MemberKeyRecord{}}
		if len(selected) > filter.Limit {
			selected = selected[:filter.Limit]
			cursor := selected[len(selected)-1].ID
			result.NextCursor = &cursor
		}
		result.Items, err = s.memberKeyRecords(tx, actor, subject, keys, roots, selected)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}

func (s *Service) GetMemberKey(ctx context.Context, actorID, subjectID, keyID string) (*MemberKeyRecord, error) {
	if !memberKeyID.MatchString(keyID) {
		return nil, apperrors.ErrNotFound
	}
	var result *MemberKeyRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, subject, err := memberKeyPeople(tx, actorID, subjectID, "members.read")
		if err != nil {
			return err
		}
		keys, roots, err := readMemberOwnedKeys(tx, subjectID)
		if err != nil {
			return err
		}
		index := slices.IndexFunc(keys, func(key entity.APIKey) bool { return key.ID == keyID })
		if index < 0 {
			return apperrors.ErrNotFound
		}
		rows, err := s.memberKeyRecords(tx, actor, subject, keys, roots, []entity.APIKey{keys[index]})
		if err == nil {
			result = &rows[0]
		}
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}

// Resolve the actual actor's assignments without a collation-insensitive JOIN
// promoting aliases into a privileged role or member relationship.
func memberKeyPermissions(tx *gorm.DB, actor entity.User) ([]string, error) {
	if (actor.Role != entity.RoleAdmin && actor.Role != entity.RoleMember) || actor.Disabled || actor.OffboardedAt != nil {
		return nil, apperrors.ErrForbidden
	}
	builtin := "rol_member"
	if actor.Role == entity.RoleAdmin {
		builtin = "rol_admin"
	}
	roleIDs := []string{builtin}
	var assignments []entity.UserRole
	if err := tx.Where("user_id = ?", actor.ID).Find(&assignments).Error; err != nil {
		return nil, err
	}
	for _, assignment := range assignments {
		if assignment.UserID != actor.ID {
			return nil, apperrors.ErrForbidden
		}
		var role entity.Role
		if err := tx.Select("id").First(&role, "id = ?", assignment.RoleID).Error; err != nil {
			return nil, err
		}
		if role.ID != assignment.RoleID {
			return nil, apperrors.ErrForbidden
		}
		if !slices.Contains(roleIDs, role.ID) {
			roleIDs = append(roleIDs, role.ID)
		}
	}
	result := []string{}
	for _, roleID := range roleIDs {
		var permissions []entity.RolePermission
		if err := tx.Where("role_id = ?", roleID).Find(&permissions).Error; err != nil {
			return nil, err
		}
		for _, row := range permissions {
			if row.RoleID != roleID {
				return nil, apperrors.ErrForbidden
			}
			if slices.Contains(AvailablePermissions, row.Permission) && !slices.Contains(result, row.Permission) {
				result = append(result, row.Permission)
			}
		}
	}
	return result, nil
}
