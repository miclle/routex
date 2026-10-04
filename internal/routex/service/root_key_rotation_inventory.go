package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var rootDomains = []string{"provider_credentials", "egresses", "smtp_settings", "storage_revisions", "user_mfa"}

type rootInventoryRow struct{ id, generation, reference, ciphertext string }
type rootDomainSpec struct{ table, id, generation, ciphertext string }

func rootSpec(domain string) (rootDomainSpec, error) {
	switch domain {
	case "provider_credentials":
		return rootDomainSpec{domain, "id", "", "ciphertext"}, nil
	case "egresses", "storage_revisions":
		return rootDomainSpec{domain, "id", "secret_generation", "auth_ciphertext"}, nil
	case "smtp_settings":
		return rootDomainSpec{domain, "id", "secret_generation", "auth_ciphertext"}, nil
	case "user_mfa":
		return rootDomainSpec{domain, "user_id", "generation", "secret_ciphertext"}, nil
	default:
		return rootDomainSpec{}, secretStoreUnavailable
	}
}
func rootReference(domain, id, generation string) string {
	switch domain {
	case "provider_credentials":
		return id
	case "egresses":
		return "egress:" + id + ":" + generation
	case "smtp_settings":
		return "smtp:1:" + generation
	case "storage_revisions":
		return "storage:" + id + ":" + generation
	case "user_mfa":
		return "mfa:" + id + ":" + generation
	}
	return ""
}
func rootSafeIdentity(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && utf8.ValidString(value) && !strings.ContainsFunc(value, func(r rune) bool { return r > 127 || unicode.IsControl(r) || r == ':' || r == ' ' })
}
func rootInventoryQuery(db *gorm.DB, spec rootDomainSpec, cursor string) *gorm.DB {
	query := db.Session(&gorm.Session{}).Table(spec.table).Where(clause.Neq{Column: clause.Column{Name: spec.ciphertext}, Value: ""})
	fields := []string{spec.id, spec.ciphertext}
	if spec.generation != "" {
		fields = append(fields, spec.generation)
	}
	query = query.Select(fields)
	if spec.table == "smtp_settings" {
		if cursor != "" {
			return query.Where("id < 0")
		}
		return query.Order("id").Limit(50)
	}
	column := clause.Column{Name: spec.id}
	if cursor != "" {
		query = query.Where(database.ByteAfter(db, column, cursor))
	}
	return query.Clauses(clause.OrderBy{Expression: database.ByteOrder(db, column)}).Limit(50)
}
func (s *Service) rootInventoryPage(ctx context.Context, domain, cursor string) ([]rootInventoryRow, error) {
	spec, err := rootSpec(domain)
	if err != nil {
		return nil, err
	}
	rows := []map[string]any{}
	if err := rootInventoryQuery(s.authDB(ctx), spec, cursor).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]rootInventoryRow, 0, len(rows))
	for _, row := range rows {
		id := rootSQLString(row[spec.id])
		generation := ""
		if spec.generation != "" {
			generation = rootSQLString(row[spec.generation])
		}
		if !rootSafeIdentity(id, 64) || (spec.generation != "" && !rootSafeIdentity(generation, 64)) || (domain == "smtp_settings" && id != "1") {
			return nil, secretStoreUnavailable
		}
		result = append(result, rootInventoryRow{id, generation, rootReference(domain, id, generation), rootSQLString(row[spec.ciphertext])})
	}
	return result, nil
}
func rootSQLString(v any) string {
	if v == nil {
		return ""
	}
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return fmt.Sprint(v)
}
func rootExactSubject(tx *gorm.DB, spec rootDomainSpec, row rootInventoryRow) *gorm.DB {
	q := personalExact(tx.Session(&gorm.Session{}).Table(spec.table), spec.id, row.id)
	if spec.generation != "" {
		q = personalExact(q, spec.generation, row.generation)
	}
	return q.Where(database.ExactText(tx, clause.Column{Name: spec.ciphertext}, row.ciphertext))
}

// Full zero validation is bounded by the caller deadline and page size. It runs
// without global locks; the unchanged writer fence is validated at final commit.
func (s *Service) rootTargetSweep(ctx context.Context, target string) error {
	for _, domain := range rootDomains {
		cursor := ""
		for {
			rows, err := s.rootInventoryPage(ctx, domain, cursor)
			if err != nil {
				return err
			}
			for _, row := range rows {
				key, err := s.secrets.KeyID(row.reference, row.ciphertext)
				if err != nil || key != target {
					return secretStoreUnavailable
				}
				cursor = row.id
			}
			if len(rows) < 50 {
				break
			}
		}
	}
	return nil
}
func (s *Service) rootCAS(tx *gorm.DB, domain string, row rootInventoryRow, result string) (string, error) {
	spec, err := rootSpec(domain)
	if err != nil {
		return "", err
	}
	update := rootExactSubject(tx, spec, row).UpdateColumn(spec.ciphertext, result)
	if update.Error != nil {
		return "", update.Error
	}
	if update.RowsAffected == 1 {
		return "rewrapped", nil
	}
	var current map[string]any
	err = personalExact(tx.Session(&gorm.Session{}).Table(spec.table), spec.id, row.id).Take(&current).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "deleted", nil
	}
	if err != nil {
		return "", err
	}
	return "changed", nil
}
func rootMFALock(tx *gorm.DB, row rootInventoryRow) error {
	var user entity.User
	// Do not require enabled status: retained disabled-user MFA remains inventory.
	if err := personalExact(tx.Session(&gorm.Session{}), "id", row.id).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		} // Historical orphan state remains inventory; no current login can authorize it.
		return err
	}
	if user.ID != row.id {
		return secretStoreUnavailable
	}
	return nil
}
