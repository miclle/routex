package database

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// V78 adds only a discriminator to the retained auth rows. Legacy encrypted
// Token material and its root reference/generation remain byte-for-byte intact.
type vaultWriterMethodV78 struct {
	Method string `gorm:"size:16;not null;default:token;check:ck_vault_writer_method,(OCTET_LENGTH(method) = 5 AND ASCII(SUBSTRING(method,1,1)) = 116 AND ASCII(SUBSTRING(method,2,1)) = 111 AND ASCII(SUBSTRING(method,3,1)) = 107 AND ASCII(SUBSTRING(method,4,1)) = 101 AND ASCII(SUBSTRING(method,5,1)) = 110) OR (OCTET_LENGTH(method) = 7 AND ASCII(SUBSTRING(method,1,1)) = 97 AND ASCII(SUBSTRING(method,2,1)) = 112 AND ASCII(SUBSTRING(method,3,1)) = 112 AND ASCII(SUBSTRING(method,4,1)) = 114 AND ASCII(SUBSTRING(method,5,1)) = 111 AND ASCII(SUBSTRING(method,6,1)) = 108 AND ASCII(SUBSTRING(method,7,1)) = 101)" json:"-"`
}
type vaultReaderMethodV78 struct {
	Method string `gorm:"size:16;not null;default:token;check:ck_vault_reader_method,(OCTET_LENGTH(method) = 5 AND ASCII(SUBSTRING(method,1,1)) = 116 AND ASCII(SUBSTRING(method,2,1)) = 111 AND ASCII(SUBSTRING(method,3,1)) = 107 AND ASCII(SUBSTRING(method,4,1)) = 101 AND ASCII(SUBSTRING(method,5,1)) = 110) OR (OCTET_LENGTH(method) = 7 AND ASCII(SUBSTRING(method,1,1)) = 97 AND ASCII(SUBSTRING(method,2,1)) = 112 AND ASCII(SUBSTRING(method,3,1)) = 112 AND ASCII(SUBSTRING(method,4,1)) = 114 AND ASCII(SUBSTRING(method,5,1)) = 111 AND ASCII(SUBSTRING(method,6,1)) = 108 AND ASCII(SUBSTRING(method,7,1)) = 101)" json:"-"`
}

func (vaultWriterMethodV78) TableName() string { return "vault_writer_auth" }
func (vaultReaderMethodV78) TableName() string { return "vault_reader_auth" }

// V78 follows the immutable V77 predecessor in the private AppRole candidate.
func vaultAppRoleMigration(db *gorm.DB) error {
	for _, pair := range []struct {
		model any
		check string
	}{{&vaultWriterMethodV78{}, "ck_vault_writer_method"}, {&vaultReaderMethodV78{}, "ck_vault_reader_method"}} {
		if err := vaultAuthMethodMigration(db, pair.model, pair.check); err != nil {
			return err
		}
	}
	return nil
}
func vaultAuthMethodMigration(db *gorm.DB, model any, check string) error {
	m := db.Migrator()
	if m.HasColumn(model, "Method") {
		columns, err := m.ColumnTypes(model)
		if err != nil {
			return err
		}
		valid := false
		for _, c := range columns {
			if c.Name() == "method" {
				length, hasLength := c.Length()
				nullable, hasNull := c.Nullable()
				def, hasDefault := c.DefaultValue()
				valid = (strings.EqualFold(c.DatabaseTypeName(), "varchar") || strings.EqualFold(c.DatabaseTypeName(), "character varying")) && hasLength && length == 16 && hasNull && !nullable && hasDefault && (def == "token" || def == "'token'::character varying" || def == "'token'")
			}
		}
		if !valid {
			return fmt.Errorf("unexpected Vault auth method column")
		}
	} else if err := m.AddColumn(model, "Method"); err != nil {
		return err
	}
	if !m.HasConstraint(model, check) {
		return m.CreateConstraint(model, check)
	}
	return nil
}
