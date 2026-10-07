package database

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// V79 adds an explicit transport discriminator and a separate retained operator
// attestation relation. No historical discovery row is inferred or copied.
type azureConnectionV79 struct {
	Adapter    string  `gorm:"size:24;not null;default:native;check:ck_connection_adapter_v79,(OCTET_LENGTH(adapter) = 6 AND ASCII(SUBSTRING(adapter,1,1)) = 110 AND ASCII(SUBSTRING(adapter,2,1)) = 97 AND ASCII(SUBSTRING(adapter,3,1)) = 116 AND ASCII(SUBSTRING(adapter,4,1)) = 105 AND ASCII(SUBSTRING(adapter,5,1)) = 118 AND ASCII(SUBSTRING(adapter,6,1)) = 101 AND api_version IS NULL) OR (OCTET_LENGTH(adapter) = 20 AND ASCII(SUBSTRING(adapter,1,1)) = 97 AND ASCII(SUBSTRING(adapter,2,1)) = 122 AND ASCII(SUBSTRING(adapter,3,1)) = 117 AND ASCII(SUBSTRING(adapter,4,1)) = 114 AND ASCII(SUBSTRING(adapter,5,1)) = 101 AND ASCII(SUBSTRING(adapter,6,1)) = 95 AND ASCII(SUBSTRING(adapter,7,1)) = 111 AND ASCII(SUBSTRING(adapter,8,1)) = 112 AND ASCII(SUBSTRING(adapter,9,1)) = 101 AND ASCII(SUBSTRING(adapter,10,1)) = 110 AND ASCII(SUBSTRING(adapter,11,1)) = 97 AND ASCII(SUBSTRING(adapter,12,1)) = 105 AND ASCII(SUBSTRING(adapter,13,1)) = 95 AND ASCII(SUBSTRING(adapter,14,1)) = 99 AND ASCII(SUBSTRING(adapter,15,1)) = 108 AND ASCII(SUBSTRING(adapter,16,1)) = 97 AND ASCII(SUBSTRING(adapter,17,1)) = 115 AND ASCII(SUBSTRING(adapter,18,1)) = 115 AND ASCII(SUBSTRING(adapter,19,1)) = 105 AND ASCII(SUBSTRING(adapter,20,1)) = 99 AND OCTET_LENGTH(protocol) = 11 AND ASCII(SUBSTRING(protocol,1,1)) = 111 AND ASCII(SUBSTRING(protocol,2,1)) = 112 AND ASCII(SUBSTRING(protocol,3,1)) = 101 AND ASCII(SUBSTRING(protocol,4,1)) = 110 AND ASCII(SUBSTRING(protocol,5,1)) = 97 AND ASCII(SUBSTRING(protocol,6,1)) = 105 AND ASCII(SUBSTRING(protocol,7,1)) = 95 AND ASCII(SUBSTRING(protocol,8,1)) = 99 AND ASCII(SUBSTRING(protocol,9,1)) = 104 AND ASCII(SUBSTRING(protocol,10,1)) = 97 AND ASCII(SUBSTRING(protocol,11,1)) = 116 AND api_version IS NOT NULL AND OCTET_LENGTH(api_version) IN (10,18))"`
	APIVersion *string `gorm:"size:18"`
}

func (azureConnectionV79) TableName() string { return "provider_connections" }

// No live FKs: historical evidence survives deletion, but its exact logical
// identity must match a current Credential/Connection/ProviderModel to authorize.
type credentialDeploymentAttestationV79 struct {
	CredentialID    string `gorm:"primaryKey;size:30"`
	ProviderModelID string `gorm:"primaryKey;size:30"`
	Identity        string `gorm:"size:64;not null"`
}

func (credentialDeploymentAttestationV79) TableName() string {
	return "credential_deployment_attestations"
}

type azureCredentialV79 struct {
	CoverageRevision     int64  `gorm:"not null;default:0;check:ck_credential_coverage_revision_v79,coverage_revision >= 0"`
	CoverageReviewETag   string `gorm:"column:coverage_review_etag;size:129;not null;default:''"`
	CoverageIntentSHA256 string `gorm:"size:64;not null;default:''"`
}

func (azureCredentialV79) TableName() string { return "provider_credentials" }
func azureDeploymentMigration(db *gorm.DB) error {
	m := db.Migrator()
	model := &azureConnectionV79{}
	// Add the nullable version before the cross-field constraint. Partial MySQL
	// DDL is validated and resumed, never treated as transactionally rolled back.
	for _, field := range []string{"APIVersion", "Adapter"} {
		if !m.HasColumn(model, field) {
			if err := m.AddColumn(model, field); err != nil {
				return err
			}
		}
	}
	columns, err := m.ColumnTypes(model)
	if err != nil {
		return err
	}
	adapter, version := false, false
	for _, c := range columns {
		width, widthOK := c.Length()
		nullable, nullOK := c.Nullable()
		varchar := strings.EqualFold(c.DatabaseTypeName(), "varchar") || strings.EqualFold(c.DatabaseTypeName(), "character varying")
		if c.Name() == "adapter" {
			def, ok := c.DefaultValue()
			adapter = varchar && widthOK && width == 24 && nullOK && !nullable && ok && (def == "native" || def == "'native'" || def == "'native'::character varying")
		}
		if c.Name() == "api_version" {
			version = varchar && widthOK && width == 18 && nullOK && nullable
		}
	}
	if !adapter || !version {
		return fmt.Errorf("unexpected Azure connection transport columns")
	}
	if !m.HasConstraint(model, "ck_connection_adapter_v79") {
		if err := m.CreateConstraint(model, "ck_connection_adapter_v79"); err != nil {
			return err
		}
	}
	credential := &azureCredentialV79{}
	for _, field := range []string{"CoverageRevision", "CoverageReviewETag", "CoverageIntentSHA256"} {
		if !m.HasColumn(credential, field) {
			if err := m.AddColumn(credential, field); err != nil {
				return err
			}
		}
	}
	credentialColumns, err := m.ColumnTypes(credential)
	if err != nil {
		return err
	}
	valid, reviewed, intent := false, false, false
	for _, c := range credentialColumns {
		if c.Name() == "coverage_review_etag" || c.Name() == "coverage_intent_sha256" {
			width, w := c.Length()
			nullable, n := c.Nullable()
			def, d := c.DefaultValue()
			kind := strings.ToLower(c.DatabaseTypeName())
			ok := (kind == "varchar" || kind == "character varying") && w && n && !nullable && d && (def == "" || def == "''" || def == "''::character varying")
			if c.Name() == "coverage_review_etag" {
				reviewed = ok && width == 129
			} else {
				intent = ok && width == 64
			}
		}
		if c.Name() == "coverage_revision" {
			nullable, n := c.Nullable()
			def, d := c.DefaultValue()
			kind := strings.ToLower(c.DatabaseTypeName())
			valid = (kind == "bigint" || kind == "int8") && n && !nullable && d && (def == "0" || def == "'0'::bigint" || def == "'0'")
		}
	}
	if !valid || !reviewed || !intent {
		return fmt.Errorf("unexpected credential coverage revision column")
	}
	if !m.HasConstraint(credential, "ck_credential_coverage_revision_v79") {
		if err := m.CreateConstraint(credential, "ck_credential_coverage_revision_v79"); err != nil {
			return err
		}
	}
	return migrateTables(db, &credentialDeploymentAttestationV79{})
}
