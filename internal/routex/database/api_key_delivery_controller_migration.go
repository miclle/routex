package database

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

// apiKeyDeliveryControllerGrantV114 is an independently enrolled machine identity.
// Its verifier and immutable scope never borrow a Session, inference Key or
// Vault credential. Configuration assignments carry separate reviewed facts.
type apiKeyDeliveryControllerGrantV114 struct {
	ID                         string     `gorm:"column:id;primaryKey;size:30"`
	EnrollmentRequestID        string     `gorm:"column:enrollment_request_id;size:36;not null;uniqueIndex:idx_delivery_controller_enrollment,priority:2;check:ck_delivery_controller_enrollment,OCTET_LENGTH(enrollment_request_id) = 36 AND OCTET_LENGTH(enrollment_intent_digest) = 64 AND OCTET_LENGTH(enrollment_review) = 64"`
	EnrollmentIntentDigest     string     `gorm:"column:enrollment_intent_digest;size:64;not null"`
	EnrollmentReview           string     `gorm:"column:enrollment_review;size:64;not null"`
	Name                       string     `gorm:"column:name;size:100;not null"`
	CredentialDigest           string     `gorm:"column:credential_digest;size:64;not null;uniqueIndex:idx_delivery_controller_verifier;check:ck_delivery_controller_identity,OCTET_LENGTH(id) = 30 AND OCTET_LENGTH(credential_digest) = 64 AND OCTET_LENGTH(review_revision) = 64 AND OCTET_LENGTH(owner_digest) = 64 AND OCTET_LENGTH(owner_id) = 30 AND OCTET_LENGTH(slot_id) = 30 AND OCTET_LENGTH(profile_id) = 30 AND OCTET_LENGTH(principal_id) = 30 AND OCTET_LENGTH(created_by) = 30 AND OCTET_LENGTH(configuration_review_actor_id) = 30 AND OCTET_LENGTH(creation_reason) BETWEEN 1 AND 1000 AND OCTET_LENGTH(name) BETWEEN 1 AND 100"`
	OwnerKind                  string     `gorm:"column:owner_kind;size:20;not null;check:ck_delivery_controller_owner,(OCTET_LENGTH(owner_kind) = 8 AND ASCII(SUBSTRING(owner_kind,1,1)) = 112 AND ASCII(SUBSTRING(owner_kind,2,1)) = 101 AND ASCII(SUBSTRING(owner_kind,3,1)) = 114 AND ASCII(SUBSTRING(owner_kind,4,1)) = 115 AND ASCII(SUBSTRING(owner_kind,5,1)) = 111 AND ASCII(SUBSTRING(owner_kind,6,1)) = 110 AND ASCII(SUBSTRING(owner_kind,7,1)) = 97 AND ASCII(SUBSTRING(owner_kind,8,1)) = 108) OR (OCTET_LENGTH(owner_kind) = 7 AND ASCII(SUBSTRING(owner_kind,1,1)) = 112 AND ASCII(SUBSTRING(owner_kind,2,1)) = 114 AND ASCII(SUBSTRING(owner_kind,3,1)) = 111 AND ASCII(SUBSTRING(owner_kind,4,1)) = 106 AND ASCII(SUBSTRING(owner_kind,5,1)) = 101 AND ASCII(SUBSTRING(owner_kind,6,1)) = 99 AND ASCII(SUBSTRING(owner_kind,7,1)) = 116)"`
	OwnerID                    string     `gorm:"column:owner_id;size:30;not null"`
	OwnerBirth                 time.Time  `gorm:"column:owner_birth;precision:6;not null"`
	OwnerDigest                string     `gorm:"column:owner_digest;size:64;not null"`
	SlotID                     string     `gorm:"column:slot_id;size:30;not null;uniqueIndex:idx_delivery_controller_enrollment,priority:1;index:idx_delivery_controller_slot,priority:1"`
	SlotBirth                  time.Time  `gorm:"column:slot_birth;precision:6;not null;index:idx_delivery_controller_slot,priority:2"`
	ProfileID                  string     `gorm:"column:profile_id;size:30;not null"`
	ProfileBirth               time.Time  `gorm:"column:profile_birth;precision:6;not null"`
	PrincipalID                string     `gorm:"column:principal_id;size:30;not null"`
	PrincipalBirth             time.Time  `gorm:"column:principal_birth;precision:6;not null"`
	ExpiresAt                  time.Time  `gorm:"column:expires_at;precision:6;not null;check:ck_delivery_controller_lifetime,created_at > '0001-01-01 00:00:00' AND owner_birth > '0001-01-01 00:00:00' AND slot_birth > '0001-01-01 00:00:00' AND profile_birth > '0001-01-01 00:00:00' AND principal_birth > '0001-01-01 00:00:00' AND created_by_birth > '0001-01-01 00:00:00' AND expires_at > created_at AND ((revoked_at IS NULL AND OCTET_LENGTH(revocation_reason) = 0 AND OCTET_LENGTH(revocation_review) = 0 AND OCTET_LENGTH(revocation_actor_id) = 0 AND revocation_actor_birth IS NULL) OR (revoked_at >= created_at AND OCTET_LENGTH(revocation_review) = 64 AND OCTET_LENGTH(revocation_actor_id) = 30 AND revocation_actor_birth IS NOT NULL AND OCTET_LENGTH(revocation_reason) BETWEEN 1 AND 1000))"`
	RevokedAt                  *time.Time `gorm:"column:revoked_at;precision:6"`
	RevocationReason           string     `gorm:"column:revocation_reason;size:1000;not null"`
	AssignmentRequestID        string     `gorm:"column:assignment_request_id;size:36;not null;check:ck_delivery_controller_assignment_intent,OCTET_LENGTH(assignment_request_id) = 36 AND OCTET_LENGTH(assignment_intent_digest) = 64 AND OCTET_LENGTH(assignment_review) = 64"`
	AssignmentIntentDigest     string     `gorm:"column:assignment_intent_digest;size:64;not null"`
	AssignmentReview           string     `gorm:"column:assignment_review;size:64;not null"`
	RevocationActorID          string     `gorm:"column:revocation_actor_id;size:30;not null"`
	RevocationActorBirth       *time.Time `gorm:"column:revocation_actor_birth;precision:6"`
	RevocationReview           string     `gorm:"column:revocation_review;size:64;not null"`
	ReviewRevision             string     `gorm:"column:review_revision;size:64;not null"`
	ReviewedDescriptorRevision uint64     `gorm:"column:reviewed_descriptor_revision;not null;check:ck_delivery_controller_assignment,reviewed_descriptor_revision BETWEEN 1 AND 9223372036854775807 AND OCTET_LENGTH(configuration_digest) = 64 AND OCTET_LENGTH(reviewed_binding_id) = 30 AND OCTET_LENGTH(reviewed_key_id) = 30 AND OCTET_LENGTH(configuration_review_reason) BETWEEN 1 AND 1000 AND reviewed_binding_birth > '0001-01-01 00:00:00' AND reviewed_key_birth > '0001-01-01 00:00:00' AND configuration_review_birth > '0001-01-01 00:00:00' AND configuration_reviewed_at >= created_at"`
	ReviewedBindingID          string     `gorm:"column:reviewed_binding_id;size:30;not null"`
	ReviewedBindingBirth       time.Time  `gorm:"column:reviewed_binding_birth;precision:6;not null"`
	ReviewedKeyID              string     `gorm:"column:reviewed_key_id;size:30;not null"`
	ReviewedKeyBirth           time.Time  `gorm:"column:reviewed_key_birth;precision:6;not null"`
	ConfigurationDigest        string     `gorm:"column:configuration_digest;size:64;not null"`
	ConfigurationReviewedAt    time.Time  `gorm:"column:configuration_reviewed_at;precision:6;not null"`
	ConfigurationReviewActorID string     `gorm:"column:configuration_review_actor_id;size:30;not null"`
	ConfigurationReviewBirth   time.Time  `gorm:"column:configuration_review_birth;precision:6;not null"`
	ConfigurationReviewReason  string     `gorm:"column:configuration_review_reason;size:1000;not null"`
	CreatedBy                  string     `gorm:"column:created_by;size:30;not null"`
	CreatedByBirth             time.Time  `gorm:"column:created_by_birth;precision:6;not null"`
	CreationReason             string     `gorm:"column:creation_reason;size:1000;not null"`
	CreatedAt                  time.Time  `gorm:"column:created_at;precision:6;not null;autoCreateTime:false"`
	UpdatedAt                  time.Time  `gorm:"column:updated_at;precision:6;not null;autoUpdateTime:false"`
}

func (apiKeyDeliveryControllerGrantV114) TableName() string {
	return "api_key_delivery_controller_grants"
}

// apiKeyDeliveryControllerAcknowledgementV114 records a controller's exact reviewed
// configuration report. It is not server proof of Agent output, application use,
// native completion, retirement or fleet convergence.
type apiKeyDeliveryControllerAcknowledgementV114 struct {
	ID                  string    `gorm:"column:id;primaryKey;size:30"`
	GrantID             string    `gorm:"column:grant_id;size:30;not null;uniqueIndex:idx_delivery_controller_ack_intent,priority:1"`
	GrantBirth          time.Time `gorm:"column:grant_birth;precision:6;not null"`
	RequestID           string    `gorm:"column:request_id;size:36;not null;uniqueIndex:idx_delivery_controller_ack_intent,priority:2;check:ck_delivery_controller_ack_identity,OCTET_LENGTH(id) = 30 AND OCTET_LENGTH(grant_id) = 30 AND OCTET_LENGTH(request_id) = 36 AND OCTET_LENGTH(intent_digest) = 64 AND OCTET_LENGTH(configuration_digest) = 64 AND descriptor_revision BETWEEN 1 AND 9223372036854775807 AND OCTET_LENGTH(binding_id) = 30 AND OCTET_LENGTH(key_id) = 30 AND grant_birth > '0001-01-01 00:00:00' AND binding_birth > '0001-01-01 00:00:00' AND key_birth > '0001-01-01 00:00:00' AND recorded_at >= grant_birth"`
	IntentDigest        string    `gorm:"column:intent_digest;size:64;not null"`
	DescriptorRevision  uint64    `gorm:"column:descriptor_revision;not null"`
	ConfigurationDigest string    `gorm:"column:configuration_digest;size:64;not null"`
	BindingID           string    `gorm:"column:binding_id;size:30;not null"`
	BindingBirth        time.Time `gorm:"column:binding_birth;precision:6;not null"`
	KeyID               string    `gorm:"column:key_id;size:30;not null"`
	KeyBirth            time.Time `gorm:"column:key_birth;precision:6;not null"`
	AgentState          string    `gorm:"column:agent_state;size:16;not null;check:ck_delivery_controller_ack_agent,(OCTET_LENGTH(agent_state) = 7 AND ASCII(SUBSTRING(agent_state,1,1)) = 117 AND ASCII(SUBSTRING(agent_state,2,1)) = 110 AND ASCII(SUBSTRING(agent_state,3,1)) = 107 AND ASCII(SUBSTRING(agent_state,4,1)) = 110 AND ASCII(SUBSTRING(agent_state,5,1)) = 111 AND ASCII(SUBSTRING(agent_state,6,1)) = 119 AND ASCII(SUBSTRING(agent_state,7,1)) = 110) OR (OCTET_LENGTH(agent_state) = 7 AND ASCII(SUBSTRING(agent_state,1,1)) = 114 AND ASCII(SUBSTRING(agent_state,2,1)) = 117 AND ASCII(SUBSTRING(agent_state,3,1)) = 110 AND ASCII(SUBSTRING(agent_state,4,1)) = 110 AND ASCII(SUBSTRING(agent_state,5,1)) = 105 AND ASCII(SUBSTRING(agent_state,6,1)) = 110 AND ASCII(SUBSTRING(agent_state,7,1)) = 103) OR (OCTET_LENGTH(agent_state) = 6 AND ASCII(SUBSTRING(agent_state,1,1)) = 102 AND ASCII(SUBSTRING(agent_state,2,1)) = 97 AND ASCII(SUBSTRING(agent_state,3,1)) = 105 AND ASCII(SUBSTRING(agent_state,4,1)) = 108 AND ASCII(SUBSTRING(agent_state,5,1)) = 101 AND ASCII(SUBSTRING(agent_state,6,1)) = 100)"`
	OutputState         string    `gorm:"column:output_state;size:16;not null;check:ck_delivery_controller_ack_output,(OCTET_LENGTH(output_state) = 7 AND ASCII(SUBSTRING(output_state,1,1)) = 117 AND ASCII(SUBSTRING(output_state,2,1)) = 110 AND ASCII(SUBSTRING(output_state,3,1)) = 107 AND ASCII(SUBSTRING(output_state,4,1)) = 110 AND ASCII(SUBSTRING(output_state,5,1)) = 111 AND ASCII(SUBSTRING(output_state,6,1)) = 119 AND ASCII(SUBSTRING(output_state,7,1)) = 110) OR (OCTET_LENGTH(output_state) = 7 AND ASCII(SUBSTRING(output_state,1,1)) = 119 AND ASCII(SUBSTRING(output_state,2,1)) = 114 AND ASCII(SUBSTRING(output_state,3,1)) = 105 AND ASCII(SUBSTRING(output_state,4,1)) = 116 AND ASCII(SUBSTRING(output_state,5,1)) = 116 AND ASCII(SUBSTRING(output_state,6,1)) = 101 AND ASCII(SUBSTRING(output_state,7,1)) = 110) OR (OCTET_LENGTH(output_state) = 6 AND ASCII(SUBSTRING(output_state,1,1)) = 102 AND ASCII(SUBSTRING(output_state,2,1)) = 97 AND ASCII(SUBSTRING(output_state,3,1)) = 105 AND ASCII(SUBSTRING(output_state,4,1)) = 108 AND ASCII(SUBSTRING(output_state,5,1)) = 101 AND ASCII(SUBSTRING(output_state,6,1)) = 100)"`
	RecordedAt          time.Time `gorm:"column:recorded_at;precision:6;not null"`
}

func (apiKeyDeliveryControllerAcknowledgementV114) TableName() string {
	return "api_key_delivery_controller_acknowledgements"
}

// Version 114 must be registered only after the separately frozen V113 step.
// Schema writes use GORM; repeat execution validates retained rows and exact
// metadata before restoring named CHECKs after interrupted MySQL DDL.
func apiKeyDeliveryControllerMigration(db *gorm.DB) error {
	for _, model := range []any{&apiKeyDeliveryControllerGrantV114{}, &apiKeyDeliveryControllerAcknowledgementV114{}} {
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(model); err != nil {
			return err
		}
		if db.Migrator().HasTable(model) {
			var count int64
			if err := db.Model(model).Count(&count).Error; err != nil {
				return err
			}
			for _, name := range statement.Schema.DBNames {
				if !db.Migrator().HasColumn(model, name) {
					if count != 0 {
						return fmt.Errorf("nonempty partial delivery controller table")
					}
					if err := db.Migrator().AddColumn(model, statement.Schema.FieldsByDBName[name].Name); err != nil {
						return err
					}
				}
			}
			if err := validateAPIKeyDeliveryControllerV114(db, model, false); err != nil {
				return err
			}
		}
		if err := migrateTables(db, model); err != nil {
			return err
		}
		names := []string{}
		for name, check := range statement.Schema.ParseCheckConstraints() {
			var invalid int64
			if err := db.Model(model).Where("NOT (" + check.Constraint + ") OR (" + check.Constraint + ") IS NULL").Count(&invalid).Error; err != nil {
				return err
			}
			if invalid != 0 {
				return fmt.Errorf("invalid retained delivery controller row")
			}
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if err := replaceV30CheckConstraint(db, model, name); err != nil {
				return err
			}
		}
		if err := validateAPIKeyDeliveryControllerV114(db, model, true); err != nil {
			return err
		}
	}
	return nil
}

func validateAPIKeyDeliveryControllerV114(db *gorm.DB, model any, indexes bool) error {
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(model); err != nil {
		return err
	}
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		return err
	}
	if len(columns) != len(statement.Schema.DBNames) {
		return fmt.Errorf("invalid delivery controller column count")
	}
	seen := map[string]bool{}
	for _, column := range columns {
		field := statement.Schema.FieldsByDBName[column.Name()]
		if field == nil || seen[column.Name()] {
			return fmt.Errorf("invalid delivery controller column")
		}
		seen[column.Name()] = true
		primary, known := column.PrimaryKey()
		if !known || primary != field.PrimaryKey {
			return fmt.Errorf("invalid delivery controller primary key")
		}
		nullable, known := column.Nullable()
		if !known || nullable != (column.Name() == "revoked_at" || column.Name() == "revocation_actor_birth") {
			return fmt.Errorf("invalid delivery controller nullability")
		}
		if _, has := column.DefaultValue(); has {
			return fmt.Errorf("invalid delivery controller default")
		}
		kind := strings.ToLower(column.DatabaseTypeName())
		switch string(field.DataType) {
		case "string":
			width, known := column.Length()
			if !known || width != int64(field.Size) || kind != "varchar" && kind != "character varying" {
				return fmt.Errorf("invalid delivery controller text metadata")
			}
		case "time":
			precision, _, known := column.DecimalSize()
			if !known || precision != 6 || kind != "timestamp" && kind != "timestamptz" && kind != "datetime" {
				return fmt.Errorf("invalid delivery controller time metadata")
			}
		case "uint":
			if kind != "bigint" && kind != "int8" {
				return fmt.Errorf("invalid delivery controller revision metadata")
			}
		default:
			return fmt.Errorf("invalid delivery controller type")
		}
	}
	// PostgreSQL GORM GetIndexes omits key ordinals; use the existing database
	// metadata adapter. All schema mutation remains in GORM Migrator.
	for _, expected := range statement.Schema.ParseIndexes() {
		columns, err := credentialAttemptStatisticsIndexColumns(db, statement.Schema.Table, expected.Name)
		if err != nil {
			return err
		}
		if len(columns) == 0 && !indexes {
			continue
		}
		if len(columns) != len(expected.Fields) {
			return fmt.Errorf("invalid delivery controller index")
		}
		for i, column := range columns {
			if !column.ColumnName.Valid || column.ColumnName.String != expected.Fields[i].DBName || !column.Position.Valid || column.Position.Int64 != int64(i+1) || !column.IsUnique.Valid || column.IsUnique.Bool != (expected.Class == "UNIQUE") || column.PrefixLength.Valid {
				return fmt.Errorf("invalid delivery controller index order")
			}
		}
	}
	return nil
}
