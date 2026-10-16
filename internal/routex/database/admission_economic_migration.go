// V119 is an unregistered, dormant source candidate. It seeds no coverage,
// domain, capacity, mode or owner and imports no evolving business entities.
package database

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// admissionEconomicDomainV119 is dormant until an explicit future coverage/cutover constructor initializes it. No capacity or accounting coverage is inferred by migration.
type admissionEconomicDomainV119 struct {
	InstallationID  string `gorm:"primaryKey;size:30;check:ck_economic_domain_identity,OCTET_LENGTH(installation_id) = 30" json:"-"`
	FormatVersion   int64  `gorm:"not null;check:ck_economic_domain_format,format_version = 1" json:"-"`
	Initialized     bool   `gorm:"not null;check:ck_economic_domain_coverage,(initialized = false AND coverage_start = 0 AND clock_high_water = 0 AND receipt_count = 0) OR (initialized = true AND coverage_start > 0 AND clock_high_water >= coverage_start)" json:"-"`
	CoverageStart   int64  `gorm:"not null" json:"-"`
	ClockHighWater  int64  `gorm:"not null" json:"-"`
	ReceiptCapacity int64  `gorm:"not null;check:ck_economic_domain_capacity,receipt_capacity > 0 AND receipt_count >= 0 AND receipt_count <= receipt_capacity" json:"-"`
	ReceiptCount    int64  `gorm:"not null" json:"-"`
}

func (admissionEconomicDomainV119) TableName() string { return "admission_economic_domains" }

// admissionEconomicReceiptV119 retains the exact original admission independently of mutable authorization and economic projection expiry. Terminal fields are a once-only pair; a SQL row alone cannot enforce operation immutability.
type admissionEconomicReceiptV119 struct {
	InstallationID  string                       `gorm:"primaryKey;size:30;uniqueIndex:uidx_economic_execution,priority:1;uniqueIndex:uidx_economic_receipt,priority:1;check:ck_economic_receipt_identity,OCTET_LENGTH(installation_id) = 30 AND OCTET_LENGTH(request_digest) = 64 AND OCTET_LENGTH(request_id) BETWEEN 1 AND 64 AND OCTET_LENGTH(execution_id) BETWEEN 1 AND 64 AND OCTET_LENGTH(execution_digest) = 64 AND OCTET_LENGTH(receipt_id) BETWEEN 1 AND 64 AND OCTET_LENGTH(receipt_digest) = 64 AND OCTET_LENGTH(spool_id) BETWEEN 1 AND 64 AND OCTET_LENGTH(incarnation_id) BETWEEN 1 AND 64 AND OCTET_LENGTH(admission_digest) = 64" json:"-"`
	RequestDigest   string                       `gorm:"primaryKey;size:64" json:"-"`
	RequestID       string                       `gorm:"size:64;not null" json:"-"`
	ExecutionID     string                       `gorm:"size:64;not null" json:"-"`
	ExecutionDigest string                       `gorm:"size:64;not null;uniqueIndex:uidx_economic_execution,priority:2" json:"-"`
	ReceiptID       string                       `gorm:"size:64;not null" json:"-"`
	ReceiptDigest   string                       `gorm:"size:64;not null;uniqueIndex:uidx_economic_receipt,priority:2" json:"-"`
	SpoolID         string                       `gorm:"size:64;not null" json:"-"`
	IncarnationID   string                       `gorm:"size:64;not null" json:"-"`
	ModeRevision    int64                        `gorm:"not null;check:ck_economic_receipt_owner,mode_revision > 0 AND fence > 0 AND owner_started_at > '0001-01-01 00:00:00'" json:"-"`
	Fence           int64                        `gorm:"not null" json:"-"`
	OwnerStartedAt  time.Time                    `gorm:"precision:6;not null;autoCreateTime:false" json:"-"`
	AdmissionDigest string                       `gorm:"size:64;not null" json:"-"`
	IntentBody      []byte                       `gorm:"not null;check:ck_economic_receipt_body,OCTET_LENGTH(intent_body) BETWEEN 1 AND 131072" json:"-"`
	AdmittedAt      int64                        `gorm:"not null;check:ck_economic_receipt_time,admitted_at > 0 AND admitted_at <= 9222767236854775807 AND month_start >= 0 AND month_start <= admitted_at AND month_end > admitted_at" json:"-"`
	MonthStart      int64                        `gorm:"not null" json:"-"`
	MonthEnd        int64                        `gorm:"not null" json:"-"`
	TerminalDigest  *string                      `gorm:"size:64;check:ck_economic_receipt_terminal,(terminal_digest IS NULL AND terminal_body IS NULL) OR (terminal_digest IS NOT NULL AND OCTET_LENGTH(terminal_digest) = 64 AND terminal_body IS NOT NULL AND OCTET_LENGTH(terminal_body) BETWEEN 1 AND 131072)" json:"-"`
	TerminalBody    []byte                       `gorm:"" json:"-"`
	Domain          *admissionEconomicDomainV119 `gorm:"belongsTo:Domain;foreignKey:InstallationID;references:InstallationID;constraint:fk_economic_receipt_domain,OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (admissionEconomicReceiptV119) TableName() string { return "admission_economic_receipts" }

// admissionEconomicAttemptV119 separates immutable dispatch intent from a once-only independent checkpoint. Its restrictive reference does not retain mutable Credential or process records.
type admissionEconomicAttemptV119 struct {
	InstallationID        string                        `gorm:"primaryKey;size:30;uniqueIndex:uidx_economic_attempt_identity,priority:1;check:ck_economic_attempt_identity,OCTET_LENGTH(installation_id) = 30 AND OCTET_LENGTH(request_digest) = 64 AND OCTET_LENGTH(request_id) BETWEEN 1 AND 64 AND OCTET_LENGTH(attempt_id) BETWEEN 1 AND 64 AND OCTET_LENGTH(attempt_identity_digest) = 64 AND OCTET_LENGTH(attempt_digest) = 64" json:"-"`
	RequestDigest         string                        `gorm:"primaryKey;size:64" json:"-"`
	Number                int64                         `gorm:"primaryKey;autoIncrement:false;check:ck_economic_attempt_number,number BETWEEN 1 AND 4" json:"-"`
	RequestID             string                        `gorm:"size:64;not null" json:"-"`
	AttemptID             string                        `gorm:"size:64;not null" json:"-"`
	AttemptIdentityDigest string                        `gorm:"size:64;not null;uniqueIndex:uidx_economic_attempt_identity,priority:2" json:"-"`
	AttemptDigest         string                        `gorm:"size:64;not null" json:"-"`
	DispatchBody          []byte                        `gorm:"not null;check:ck_economic_attempt_body,OCTET_LENGTH(dispatch_body) BETWEEN 1 AND 131072" json:"-"`
	CheckpointDigest      *string                       `gorm:"size:64;check:ck_economic_attempt_checkpoint,(checkpoint_digest IS NULL AND checkpoint_body IS NULL) OR (checkpoint_digest IS NOT NULL AND OCTET_LENGTH(checkpoint_digest) = 64 AND checkpoint_body IS NOT NULL AND OCTET_LENGTH(checkpoint_body) BETWEEN 1 AND 131072)" json:"-"`
	CheckpointBody        []byte                        `gorm:"" json:"-"`
	Receipt               *admissionEconomicReceiptV119 `gorm:"belongsTo:Receipt;foreignKey:InstallationID,RequestDigest;references:InstallationID,RequestDigest;constraint:fk_economic_attempt_receipt,OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (admissionEconomicAttemptV119) TableName() string { return "admission_economic_attempts" }

// admissionEconomicAccountV119 persists the exact authoritative account birth. RPM and logical-executor counts are independent of token/money contribution completeness.
type admissionEconomicAccountV119 struct {
	InstallationID string                       `gorm:"primaryKey;size:30;check:ck_economic_account_identity,OCTET_LENGTH(installation_id) = 30 AND OCTET_LENGTH(account_digest) = 64 AND OCTET_LENGTH(account) BETWEEN 1 AND 64" json:"-"`
	AccountDigest  string                       `gorm:"primaryKey;size:64" json:"-"`
	Account        string                       `gorm:"size:64;not null" json:"-"`
	Birth          time.Time                    `gorm:"precision:6;not null;autoCreateTime:false;check:ck_economic_account_birth,birth > '0001-01-01 00:00:00'" json:"-"`
	RPMCount       int64                        `gorm:"not null;check:ck_economic_account_counts,rpm_count >= 0 AND executor_count >= 0" json:"-"`
	ExecutorCount  int64                        `gorm:"not null" json:"-"`
	Domain         *admissionEconomicDomainV119 `gorm:"belongsTo:Domain;foreignKey:InstallationID;references:InstallationID;constraint:fk_economic_account_domain,OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (admissionEconomicAccountV119) TableName() string { return "admission_economic_accounts" }

// admissionEconomicCounterV119 stores canonical validated quota JSON with arbitrary-size integer-unit money totals. The storage adapter must validate every decoded counter before arithmetic.
type admissionEconomicCounterV119 struct {
	InstallationID string                        `gorm:"primaryKey;size:30;check:ck_economic_counter_identity,OCTET_LENGTH(installation_id) = 30 AND OCTET_LENGTH(account_digest) = 64 AND OCTET_LENGTH(window_name) BETWEEN 1 AND 48" json:"-"`
	AccountDigest  string                        `gorm:"primaryKey;size:64" json:"-"`
	Window         string                        `gorm:"column:window_name;primaryKey;size:48" json:"-"`
	CounterBody    []byte                        `gorm:"not null;check:ck_economic_counter_body,OCTET_LENGTH(counter_body) BETWEEN 1 AND 131072" json:"-"`
	AccountRecord  *admissionEconomicAccountV119 `gorm:"belongsTo:AccountRecord;foreignKey:InstallationID,AccountDigest;references:InstallationID,AccountDigest;constraint:fk_economic_counter_account,OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (admissionEconomicCounterV119) TableName() string { return "admission_economic_counters" }

// admissionEconomicContributionV119 expires projection data without removing its retained receipt. Executor and active quota contributions never expire by a timer.
type admissionEconomicContributionV119 struct {
	InstallationID   string                        `gorm:"primaryKey;size:30;index:idx_economic_contribution_expiry,priority:1;check:ck_economic_contribution_identity,OCTET_LENGTH(installation_id) = 30 AND OCTET_LENGTH(request_digest) = 64 AND OCTET_LENGTH(request_id) BETWEEN 1 AND 64 AND OCTET_LENGTH(account_digest) = 64 AND OCTET_LENGTH(window_name) <= 48" json:"-"`
	RequestDigest    string                        `gorm:"primaryKey;size:64" json:"-"`
	AccountDigest    string                        `gorm:"primaryKey;size:64;index:idx_economic_contribution_expiry,priority:2" json:"-"`
	Kind             string                        `gorm:"primaryKey;size:16;check:ck_economic_contribution_kind,(OCTET_LENGTH(kind) = 3 AND ASCII(SUBSTRING(kind,1,1)) = 114 AND ASCII(SUBSTRING(kind,2,1)) = 112 AND ASCII(SUBSTRING(kind,3,1)) = 109) OR (OCTET_LENGTH(kind) = 8 AND ASCII(SUBSTRING(kind,1,1)) = 101 AND ASCII(SUBSTRING(kind,2,1)) = 120 AND ASCII(SUBSTRING(kind,3,1)) = 101 AND ASCII(SUBSTRING(kind,4,1)) = 99 AND ASCII(SUBSTRING(kind,5,1)) = 117 AND ASCII(SUBSTRING(kind,6,1)) = 116 AND ASCII(SUBSTRING(kind,7,1)) = 111 AND ASCII(SUBSTRING(kind,8,1)) = 114) OR (OCTET_LENGTH(kind) = 12 AND ASCII(SUBSTRING(kind,1,1)) = 97 AND ASCII(SUBSTRING(kind,2,1)) = 99 AND ASCII(SUBSTRING(kind,3,1)) = 116 AND ASCII(SUBSTRING(kind,4,1)) = 105 AND ASCII(SUBSTRING(kind,5,1)) = 118 AND ASCII(SUBSTRING(kind,6,1)) = 101 AND ASCII(SUBSTRING(kind,7,1)) = 95 AND ASCII(SUBSTRING(kind,8,1)) = 113 AND ASCII(SUBSTRING(kind,9,1)) = 117 AND ASCII(SUBSTRING(kind,10,1)) = 111 AND ASCII(SUBSTRING(kind,11,1)) = 116 AND ASCII(SUBSTRING(kind,12,1)) = 97) OR (OCTET_LENGTH(kind) = 14 AND ASCII(SUBSTRING(kind,1,1)) = 116 AND ASCII(SUBSTRING(kind,2,1)) = 101 AND ASCII(SUBSTRING(kind,3,1)) = 114 AND ASCII(SUBSTRING(kind,4,1)) = 109 AND ASCII(SUBSTRING(kind,5,1)) = 105 AND ASCII(SUBSTRING(kind,6,1)) = 110 AND ASCII(SUBSTRING(kind,7,1)) = 97 AND ASCII(SUBSTRING(kind,8,1)) = 108 AND ASCII(SUBSTRING(kind,9,1)) = 95 AND ASCII(SUBSTRING(kind,10,1)) = 113 AND ASCII(SUBSTRING(kind,11,1)) = 117 AND ASCII(SUBSTRING(kind,12,1)) = 111 AND ASCII(SUBSTRING(kind,13,1)) = 116 AND ASCII(SUBSTRING(kind,14,1)) = 97)" json:"-"`
	Window           string                        `gorm:"column:window_name;primaryKey;size:48" json:"-"`
	RequestID        string                        `gorm:"size:64;not null" json:"-"`
	ExpiresAt        *int64                        `gorm:"index:idx_economic_contribution_expiry,priority:3;check:ck_economic_contribution_expiry,((kind IN ('rpm','terminal_quota')) AND expires_at IS NOT NULL AND expires_at > 0 AND OCTET_LENGTH(window_name) > 0) OR ((kind IN ('executor','active_quota')) AND expires_at IS NULL)" json:"-"`
	ContributionBody []byte                        `gorm:"not null;check:ck_economic_contribution_body,OCTET_LENGTH(contribution_body) BETWEEN 1 AND 131072" json:"-"`
	Receipt          *admissionEconomicReceiptV119 `gorm:"belongsTo:Receipt;foreignKey:InstallationID,RequestDigest;references:InstallationID,RequestDigest;constraint:fk_economic_contribution_receipt,OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
	AccountRecord    *admissionEconomicAccountV119 `gorm:"belongsTo:AccountRecord;foreignKey:InstallationID,AccountDigest;references:InstallationID,AccountDigest;constraint:fk_economic_contribution_account,OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (admissionEconomicContributionV119) TableName() string {
	return "admission_economic_contributions"
}

// admissionEconomicInvalidBoundV119 retains a permanently invalidated original bound revision and its exact causing receipt. Projection expiry does not restore that revision.
type admissionEconomicInvalidBoundV119 struct {
	InstallationID string                        `gorm:"primaryKey;size:30;index:idx_economic_invalid_bound_request,priority:1;check:ck_economic_invalid_bound_identity,OCTET_LENGTH(installation_id) = 30 AND OCTET_LENGTH(revision_digest) = 64 AND OCTET_LENGTH(revision) BETWEEN 1 AND 64 AND OCTET_LENGTH(request_digest) = 64 AND OCTET_LENGTH(request_id) BETWEEN 1 AND 64" json:"-"`
	RevisionDigest string                        `gorm:"primaryKey;size:64" json:"-"`
	Revision       string                        `gorm:"size:64;not null" json:"-"`
	RequestDigest  string                        `gorm:"size:64;not null;index:idx_economic_invalid_bound_request,priority:2" json:"-"`
	RequestID      string                        `gorm:"size:64;not null" json:"-"`
	RecordedAt     int64                         `gorm:"not null;check:ck_economic_invalid_bound_time,recorded_at > 0" json:"-"`
	Receipt        *admissionEconomicReceiptV119 `gorm:"belongsTo:Receipt;foreignKey:InstallationID,RequestDigest;references:InstallationID,RequestDigest;constraint:fk_economic_invalid_bound_receipt,OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (admissionEconomicInvalidBoundV119) TableName() string {
	return "admission_economic_invalid_bounds"
}

// admissionEconomicMigration owns only these seven frozen tables. The numbered
// registry and concurrent-startup lock remain the migration runner's concern.
func admissionEconomicMigration(db *gorm.DB) error {
	for _, model := range admissionEconomicModelsV119() {
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(model); err != nil {
			return err
		}
		if db.Migrator().HasTable(model) {
			var count int64
			if err := db.Model(model).Count(&count).Error; err != nil {
				return err
			}
			// Repair interrupted empty DDL only. Add unguarded fields first because
			// some drivers install a field's CHECK while adding that column.
			for _, guarded := range []bool{false, true} {
				for _, name := range statement.Schema.DBNames {
					field := statement.Schema.FieldsByDBName[name]
					if (field.TagSettings["CHECK"] != "") != guarded || db.Migrator().HasColumn(model, name) {
						continue
					}
					if count != 0 {
						return fmt.Errorf("nonempty partial economic schema: %s", statement.Schema.Table)
					}
					if err := db.Migrator().AddColumn(model, field.Name); err != nil {
						return err
					}
				}
			}
			if err := validateAdmissionEconomicColumnsV119(db, model, false); err != nil {
				return err
			}
		}
		if err := migrateTables(db, model); err != nil {
			return err
		}
		if err := validateAdmissionEconomicColumnsV119(db, model, true); err != nil {
			return err
		}
		if err := replaceRuntimeArchiveChecksV112(db, model); err != nil {
			return err
		}
	}
	return nil
}

func admissionEconomicModelsV119() []any {
	return []any{&admissionEconomicDomainV119{}, &admissionEconomicReceiptV119{},
		&admissionEconomicAttemptV119{}, &admissionEconomicAccountV119{},
		&admissionEconomicCounterV119{}, &admissionEconomicContributionV119{},
		&admissionEconomicInvalidBoundV119{}}
}

// Validate actual column/index shape before claiming repeat or partial-DDL
// success. Existing populated facts and column representations are never changed.
func validateAdmissionEconomicColumnsV119(db *gorm.DB, model any, indexesRequired bool) error {
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(model); err != nil {
		return err
	}
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		return err
	}
	if len(columns) != len(statement.Schema.DBNames) {
		return fmt.Errorf("invalid economic column count: %s", statement.Schema.Table)
	}
	seen := map[string]bool{}
	for _, column := range columns {
		field := statement.Schema.FieldsByDBName[column.Name()]
		if field == nil || seen[column.Name()] {
			return fmt.Errorf("invalid economic column")
		}
		seen[column.Name()] = true
		primary, known := column.PrimaryKey()
		if !known || primary != field.PrimaryKey {
			return fmt.Errorf("invalid economic primary key: %s", column.Name())
		}
		nullable, known := column.Nullable()
		if !known || nullable != (!field.NotNull && !field.PrimaryKey) {
			return fmt.Errorf("invalid economic nullability: %s", column.Name())
		}
		if _, present := column.DefaultValue(); present {
			return fmt.Errorf("unexpected economic default: %s", column.Name())
		}
		kind := strings.ToLower(column.DatabaseTypeName())
		switch string(field.DataType) {
		case "string":
			width, known := column.Length()
			if !known || width != int64(field.Size) || (kind != "varchar" && kind != "character varying") {
				return fmt.Errorf("invalid economic identity column: %s", column.Name())
			}
		case "int":
			if kind != "bigint" && kind != "int8" {
				return fmt.Errorf("invalid economic integer: %s", column.Name())
			}
		case "bool":
			if kind != "bool" && kind != "boolean" && kind != "tinyint" {
				return fmt.Errorf("invalid economic boolean")
			}
		case "time":
			precision, _, known := column.DecimalSize()
			if !known || precision != 6 || (kind != "timestamp" && kind != "timestamptz" && kind != "datetime") {
				return fmt.Errorf("invalid economic birth precision")
			}
		case "bytes":
			if kind != "bytea" && kind != "longblob" {
				return fmt.Errorf("invalid economic binary body: %s", column.Name())
			}
		default:
			return fmt.Errorf("unsupported economic column: %s", column.Name())
		}
	}
	for _, expected := range statement.Schema.ParseIndexes() {
		actual, err := credentialAttemptStatisticsIndexColumns(db, statement.Schema.Table, expected.Name)
		if err != nil {
			return err
		}
		if len(actual) == 0 && !indexesRequired {
			continue
		}
		if len(actual) != len(expected.Fields) {
			return fmt.Errorf("invalid economic index: %s", expected.Name)
		}
		for i, column := range actual {
			if !column.ColumnName.Valid || column.ColumnName.String != expected.Fields[i].DBName || !column.Position.Valid || column.Position.Int64 != int64(i+1) || !column.IsUnique.Valid || column.IsUnique.Bool != (expected.Class == "UNIQUE") || column.PrefixLength.Valid {
				return fmt.Errorf("invalid economic index order: %s", expected.Name)
			}
		}
	}
	return nil
}
