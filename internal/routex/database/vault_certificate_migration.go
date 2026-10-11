package database

import (
	"fmt"
	"gorm.io/gorm"
	"strings"
)

// Prospective V104 widens only the frozen V78 auth method boundary. Released
// steps, ciphertext, generations, root inventory domains and AAD stay unchanged.
type vaultWriterMethodV104 struct {
	Method string `gorm:"size:16;not null;default:token;check:ck_vault_writer_method,(OCTET_LENGTH(method) = 5 AND ASCII(SUBSTRING(method,1,1)) = 116 AND ASCII(SUBSTRING(method,2,1)) = 111 AND ASCII(SUBSTRING(method,3,1)) = 107 AND ASCII(SUBSTRING(method,4,1)) = 101 AND ASCII(SUBSTRING(method,5,1)) = 110) OR (OCTET_LENGTH(method) = 7 AND ASCII(SUBSTRING(method,1,1)) = 97 AND ASCII(SUBSTRING(method,2,1)) = 112 AND ASCII(SUBSTRING(method,3,1)) = 112 AND ASCII(SUBSTRING(method,4,1)) = 114 AND ASCII(SUBSTRING(method,5,1)) = 111 AND ASCII(SUBSTRING(method,6,1)) = 108 AND ASCII(SUBSTRING(method,7,1)) = 101) OR (OCTET_LENGTH(method) = 11 AND ASCII(SUBSTRING(method,1,1)) = 99 AND ASCII(SUBSTRING(method,2,1)) = 101 AND ASCII(SUBSTRING(method,3,1)) = 114 AND ASCII(SUBSTRING(method,4,1)) = 116 AND ASCII(SUBSTRING(method,5,1)) = 105 AND ASCII(SUBSTRING(method,6,1)) = 102 AND ASCII(SUBSTRING(method,7,1)) = 105 AND ASCII(SUBSTRING(method,8,1)) = 99 AND ASCII(SUBSTRING(method,9,1)) = 97 AND ASCII(SUBSTRING(method,10,1)) = 116 AND ASCII(SUBSTRING(method,11,1)) = 101)" json:"-"`
}
type vaultReaderMethodV104 struct {
	Method string `gorm:"size:16;not null;default:token;check:ck_vault_reader_method,(OCTET_LENGTH(method) = 5 AND ASCII(SUBSTRING(method,1,1)) = 116 AND ASCII(SUBSTRING(method,2,1)) = 111 AND ASCII(SUBSTRING(method,3,1)) = 107 AND ASCII(SUBSTRING(method,4,1)) = 101 AND ASCII(SUBSTRING(method,5,1)) = 110) OR (OCTET_LENGTH(method) = 7 AND ASCII(SUBSTRING(method,1,1)) = 97 AND ASCII(SUBSTRING(method,2,1)) = 112 AND ASCII(SUBSTRING(method,3,1)) = 112 AND ASCII(SUBSTRING(method,4,1)) = 114 AND ASCII(SUBSTRING(method,5,1)) = 111 AND ASCII(SUBSTRING(method,6,1)) = 108 AND ASCII(SUBSTRING(method,7,1)) = 101) OR (OCTET_LENGTH(method) = 11 AND ASCII(SUBSTRING(method,1,1)) = 99 AND ASCII(SUBSTRING(method,2,1)) = 101 AND ASCII(SUBSTRING(method,3,1)) = 114 AND ASCII(SUBSTRING(method,4,1)) = 116 AND ASCII(SUBSTRING(method,5,1)) = 105 AND ASCII(SUBSTRING(method,6,1)) = 102 AND ASCII(SUBSTRING(method,7,1)) = 105 AND ASCII(SUBSTRING(method,8,1)) = 99 AND ASCII(SUBSTRING(method,9,1)) = 97 AND ASCII(SUBSTRING(method,10,1)) = 116 AND ASCII(SUBSTRING(method,11,1)) = 101)" json:"-"`
}

func (vaultWriterMethodV104) TableName() string { return "vault_writer_auth" }
func (vaultReaderMethodV104) TableName() string { return "vault_reader_auth" }

type vaultCertificateCheck struct {
	model any
	name  string
}

func vaultCertificateChecks() []vaultCertificateCheck {
	return []vaultCertificateCheck{{&vaultWriterMethodV104{}, "ck_vault_writer_method"}, {&vaultReaderMethodV104{}, "ck_vault_reader_method"}}
}
func vaultCertificateMigration(db *gorm.DB) error {
	checks := vaultCertificateChecks()
	// Validate BOTH shapes and ALL retained values before any implicit-commit DDL.
	for _, v := range checks {
		if err := vaultCertificateColumn(db, v.model); err != nil {
			return err
		}
		st := &gorm.Statement{DB: db}
		if err := st.Parse(v.model); err != nil {
			return err
		}
		check, ok := st.Schema.ParseCheckConstraints()[v.name]
		if !ok {
			return fmt.Errorf("missing frozen Vault certificate constraint")
		}
		var bad int64
		if err := db.Table(st.Schema.Table).Where("method IS NULL OR NOT (" + check.Constraint + ")").Count(&bad).Error; err != nil {
			return err
		}
		if bad != 0 {
			return fmt.Errorf("invalid retained Vault authentication method")
		}
	}
	return vaultCertificateReplaceChecks(db, checks)
}
func vaultCertificateColumn(db *gorm.DB, model any) error {
	m := db.Migrator()
	if !m.HasColumn(model, "Method") {
		return fmt.Errorf("missing retained Vault auth method column")
	}
	columns, err := m.ColumnTypes(model)
	if err != nil {
		return err
	}
	for _, c := range columns {
		if c.Name() != "method" {
			continue
		}
		length, hasLength := c.Length()
		nullable, hasNull := c.Nullable()
		def, hasDefault := c.DefaultValue()
		if (strings.EqualFold(c.DatabaseTypeName(), "varchar") || strings.EqualFold(c.DatabaseTypeName(), "character varying")) && hasLength && length == 16 && hasNull && !nullable && hasDefault && (def == "token" || def == "'token'::character varying" || def == "'token'") {
			return nil
		}
	}
	return fmt.Errorf("unexpected retained Vault auth method column")
}
func vaultCertificateReplaceChecks(db *gorm.DB, checks []vaultCertificateCheck) error {
	for _, v := range checks {
		m := db.Migrator()
		if m.HasConstraint(v.model, v.name) {
			if err := m.DropConstraint(v.model, v.name); err != nil {
				return err
			}
		}
		if err := m.CreateConstraint(v.model, v.name); err != nil {
			return err
		}
	}
	return nil
}
