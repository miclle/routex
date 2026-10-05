package service

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const memberRoleBaseline = "0000000000000000000000000000000000000000000000000000000000000000"

func newMemberRoleRevision() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
func memberRolesDB(tx *gorm.DB) *gorm.DB { return tx.Session(&gorm.Session{NewDB: true}) }
func memberRolesExact(tx *gorm.DB, column, id string) *gorm.DB {
	return tx.Where(database.ExactText(tx, clause.Column{Name: column}, id))
}
func advanceMemberRoleRevision(tx *gorm.DB, userID string) error {
	revision, err := newMemberRoleRevision()
	if err != nil {
		return err
	}
	result := memberRolesExact(memberRolesDB(tx).Model(&entity.User{}), "id", userID).UpdateColumn("MemberRoleRevision", revision)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return apperrors.ErrNotFound
	}
	return nil
}
func validMemberRoleDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			if r < 'a' || r > 'f' {
				return false
			}
		}
	}
	return true
}
func memberRolesUserQuery(tx *gorm.DB, userID string, lock bool) *gorm.DB {
	q := memberRolesExact(memberRolesDB(tx).Model(&entity.User{}), "id", userID).
		Select("ID", "Role", "Disabled", "OffboardedAt", "CreatedAt", "MemberRoleRevision")
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return q
}
