package database

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"gorm.io/gorm"
)

const registrationEmailDomainsVersion = 58

// Frozen V58 stores only the explicit self-registration domain policy. NULL is
// the historical unrestricted baseline; invalid retained values are not reset.
type registrationEmailDomainsV58 struct {
	ID                              int    `gorm:"primaryKey"`
	RegistrationAllowedEmailDomains string `gorm:"size:2048;not null;default:[]"`
}

func (registrationEmailDomainsV58) TableName() string { return "governance_settings" }

type registrationEmailDomainsNullableV58 struct {
	ID                              int     `gorm:"primaryKey"`
	RegistrationAllowedEmailDomains *string `gorm:"size:2048"`
}

func (registrationEmailDomainsNullableV58) TableName() string { return "governance_settings" }
func registrationEmailDomainsMigration(db *gorm.DB) error {
	nullable := &registrationEmailDomainsNullableV58{}
	if !db.Migrator().HasColumn(nullable, "RegistrationAllowedEmailDomains") {
		if err := db.Migrator().AddColumn(nullable, "RegistrationAllowedEmailDomains"); err != nil {
			return err
		}
	}
	if err := db.Session(&gorm.Session{NewDB: true}).Model(nullable).Where("registration_allowed_email_domains IS NULL").UpdateColumn("RegistrationAllowedEmailDomains", "[]").Error; err != nil {
		return err
	}
	var rows []registrationEmailDomainsV58
	if err := db.Session(&gorm.Session{NewDB: true}).Select("id", "registration_allowed_email_domains").Limit(2).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) > 1 {
		return fmt.Errorf("invalid registration domain singleton")
	}
	for _, row := range rows {
		if row.ID != 1 || !registrationEmailDomainsValueV58(row.RegistrationAllowedEmailDomains) {
			return fmt.Errorf("invalid retained registration domain policy")
		}
	}
	return registrationEmailDomainsShapeV58(db)
}

// This private grammar is frozen with V58, independently of future business
// parser changes. Canonical representation prevents a narrowing repair truncation.
func registrationEmailDomainsValueV58(raw string) bool {
	if len(raw) > 2048 {
		return false
	}
	var domains []string
	if json.Unmarshal([]byte(raw), &domains) != nil || domains == nil || len(domains) > 32 {
		return false
	}
	for i, domain := range domains {
		if len(domain) > 253 || !strings.Contains(domain, ".") || strings.Trim(domain, "0123456789.") == "" || i > 0 && domains[i-1] >= domain {
			return false
		}
		for _, label := range strings.Split(domain, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return false
			}
			for _, c := range []byte(label) {
				if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
					return false
				}
			}
		}
	}
	canonical, err := json.Marshal(domains)
	return err == nil && string(canonical) == raw
}
func registrationEmailDomainsShapeV58(db *gorm.DB) error {
	model := &registrationEmailDomainsV58{}
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		return err
	}
	for _, column := range columns {
		if column.Name() != "registration_allowed_email_domains" {
			continue
		}
		length, sized := column.Length()
		nullable, known := column.Nullable()
		value, defaulted := column.DefaultValue()
		validDefault := slices.Contains([]string{"[]", "'[]'::character varying", "'[]'::varchar", "('[]')"}, value)
		kind := strings.ToLower(column.DatabaseTypeName())
		if !sized || length != 2048 || !known || nullable || !defaulted || !validDefault || kind != "varchar" && kind != "character varying" {
			return db.Migrator().AlterColumn(model, "RegistrationAllowedEmailDomains")
		}
		return nil
	}
	return fmt.Errorf("registration domain column unavailable")
}
