package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"regexp"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

var messagesSourcePattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var messagesRunPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

// A disjoint grammar: no caller-provided request ID, predecessor, protocol or
// pair can select a row. This proves one controlled installed-Key use only.
type messagesOnceConfig struct {
	Operation        string    `json:"operation"`
	SourceCommit     string    `json:"source_commit"`
	RunID            string    `json:"run_id"`
	DatabaseDriver   string    `json:"database_driver,omitempty"`
	DatabasePassword string    `json:"database_password"`
	OwnerKind        string    `json:"owner_kind"`
	UserID           string    `json:"user_id,omitempty"`
	ProjectID        string    `json:"project_id,omitempty"`
	ProjectCreatedAt time.Time `json:"project_created_at,omitempty"`
	KeyCreatorID     string    `json:"key_creator_id,omitempty"`
	KeyID            string    `json:"key_id"`
	KeyCreatedAt     time.Time `json:"key_created_at"`
	ModelID          string    `json:"model_id"`
	CredentialID     string    `json:"credential_id"`
	ConnectionID     string    `json:"connection_id"`
	ProviderModelID  string    `json:"provider_model_id"`
	SnapshotID       string    `json:"snapshot_id"`
	RunStarted       time.Time `json:"run_started"`
	AsOf             time.Time `json:"as_of"`
}

type messagesOnceProjection struct {
	RequestID        string    `json:"request_id"`
	AttemptRequestID string    `json:"attempt_request_id"`
	UserID           string    `json:"user_id"`
	ProjectID        string    `json:"project_id"`
	KeyID            string    `json:"key_id"`
	OwnedKeyID       string    `json:"owned_key_id"`
	KeyUserID        string    `json:"key_user_id"`
	KeyProjectID     string    `json:"key_project_id"`
	KeyCreatorID     string    `json:"key_creator_id"`
	OwnerProjectID   string    `json:"owner_project_id"`
	ProjectCreatedAt time.Time `json:"project_created_at"`
	KeyCreatedAt     time.Time `json:"key_created_at"`
	KeyStatus        string    `json:"key_status"`
	StartedAt        time.Time `json:"started_at"`
	CompletedAt      time.Time `json:"completed_at"`
	AttemptStartedAt time.Time `json:"attempt_started_at"`
	AttemptEndedAt   time.Time `json:"attempt_ended_at"`
	CallStatus       string    `json:"call_status"`
	AttemptStatus    string    `json:"attempt_status"`
	Native           string    `json:"native"`
	CredentialID     string    `json:"credential_id"`
	CallSnapshotID   string    `json:"call_snapshot_id"`
	SnapshotID       string    `json:"snapshot_id"`
	ConnectionID     string    `json:"connection_id"`
	ProviderModelID  string    `json:"provider_model_id"`
	ModelID          string    `json:"model_id"`
	Protocol         string    `json:"protocol"`
	AttemptNumber    int       `json:"attempt_number"`
}

type messagesOnceResult struct {
	Result                   string                 `json:"result"`
	Operation                string                 `json:"operation"`
	SourceCommit             string                 `json:"source_commit"`
	RunID                    string                 `json:"run_id"`
	RequestIDSource          string                 `json:"request_id_source"`
	ClientRequestIDDisplayed bool                   `json:"client_request_id_displayed"`
	Proof                    messagesOnceProjection `json:"proof"`
}

func decodeMessagesOnce(raw []byte, c *messagesOnceConfig) bool {
	if c == nil || len(raw) == 0 || len(raw) > 8192 || !utf8.Valid(raw) || !validJSONObject(raw) {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	var owner string
	if json.Unmarshal(fields["owner_kind"], &owner) != nil || owner != "personal" && owner != "project" {
		return false
	}
	allowed := map[string]bool{}
	for _, key := range []string{"operation", "source_commit", "run_id", "database_password", "owner_kind", "key_id", "key_created_at", "model_id", "credential_id", "connection_id", "provider_model_id", "snapshot_id", "run_started", "as_of"} {
		allowed[key] = true
	}
	if owner == "personal" {
		allowed["user_id"] = true
	} else {
		for _, key := range []string{"project_id", "project_created_at", "key_creator_id"} {
			allowed[key] = true
		}
	}
	for key := range allowed {
		if _, present := fields[key]; !present {
			return false
		}
	}
	allowed["database_driver"] = true // Omission keeps the fixed PostgreSQL endpoint.
	for key, value := range fields {
		if !allowed[key] || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
	}
	if value, present := fields["database_driver"]; present {
		var driver string
		if json.Unmarshal(value, &driver) != nil || driver != "postgres" && driver != "mysql" {
			return false
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(c) == nil && validMessagesOnceConfig(*c)
}

func validMessagesOnceConfig(c messagesOnceConfig) bool {
	return passwordPattern.MatchString(c.DatabasePassword) && validMessagesOnceExpectations(c)
}

func validMessagesOnceExpectations(c messagesOnceConfig) bool {
	if c.Operation != "messages-once" || !messagesSourcePattern.MatchString(c.SourceCommit) || !messagesRunPattern.MatchString(c.RunID) || !validDatabaseDriver(c.DatabaseDriver) || !validID(c.ModelID, "mdl") || !validID(c.CredentialID, "crd") || !validID(c.ConnectionID, "con") || !validID(c.ProviderModelID, "pmd") || !validID(c.SnapshotID, "cfg") || c.RunStarted.IsZero() || c.AsOf.Before(c.RunStarted) || c.AsOf.Sub(c.RunStarted) > 15*time.Minute || c.KeyCreatedAt.Before(c.RunStarted) || c.KeyCreatedAt.After(c.AsOf) {
		return false
	}
	switch c.OwnerKind {
	case "personal":
		return validID(c.UserID, "usr") && validID(c.KeyID, "key") && c.ProjectID == "" && c.ProjectCreatedAt.IsZero() && c.KeyCreatorID == ""
	case "project":
		return c.UserID == "" && validID(c.ProjectID, "prj") && validID(c.KeyID, "pky") && validID(c.KeyCreatorID, "usr") && !c.ProjectCreatedAt.IsZero() && !c.ProjectCreatedAt.After(c.KeyCreatedAt)
	default:
		return false
	}
}

func prepareMessagesOnceConnection(c *messagesOnceConfig) (string, bool) {
	if c == nil || !validMessagesOnceConfig(*c) {
		return "", false
	}
	password := c.DatabasePassword
	c.DatabasePassword = ""
	return fixedEvidenceConnection(c.DatabaseDriver, password), true
}

func messagesOnceSelection(tx *gorm.DB, c messagesOnceConfig) *gorm.DB {
	fields := "calls.request_id, attempt.request_id AS attempt_request_id, calls.user_id, calls.project_id, calls.key_id, owned.id AS owned_key_id, owned.status AS key_status, owned.created_at AS key_created_at, calls.started_at, calls.completed_at, attempt.started_at AS attempt_started_at, attempt.completed_at AS attempt_ended_at, calls.status AS call_status, attempt.status AS attempt_status, attempt.native_completion_evidence AS native, attempt.credential_id, calls.snapshot_id AS call_snapshot_id, attempt.snapshot_id, attempt.connection_id, attempt.provider_model_id, calls.model_id, calls.protocol, attempt.attempt_number"
	selected := tx.Table("call_records AS calls").Joins("LEFT JOIN call_attempts AS attempt ON attempt.request_id = calls.request_id")
	if c.OwnerKind == "project" {
		fields += ", owned.project_id AS key_project_id, owned.creator_id AS key_creator_id, owner_project.id AS owner_project_id, owner_project.created_at AS project_created_at"
		selected = selected.Joins("LEFT JOIN project_api_keys AS owned ON owned.id = calls.key_id").Joins("LEFT JOIN projects AS owner_project ON owner_project.id = owned.project_id")
	} else {
		fields += ", owned.user_id AS key_user_id"
		selected = selected.Joins("LEFT JOIN api_keys AS owned ON owned.id = calls.key_id")
	}
	// Select every retained Call and every Attempt for this exact fresh Key.
	// Filtering success/protocol/time/owner or selecting only the last Attempt
	// could hide an extra call, failed retry or contradictory owner attribution.
	return selected.Select(fields).Where("calls.key_id = ?", c.KeyID).Order("calls.request_id, attempt.attempt_number, attempt.id").Limit(2)
}

func queryMessagesOnce(tx *gorm.DB, c messagesOnceConfig) ([]messagesOnceProjection, error) {
	if !validMessagesOnceExpectations(c) {
		return nil, errInvalid
	}
	var rows []messagesOnceProjection
	err := messagesOnceSelection(tx, c).Scan(&rows).Error
	return rows, err
}

func validMessagesOnceProjection(c messagesOnceConfig, rows []messagesOnceProjection) bool {
	if !validMessagesOnceExpectations(c) || len(rows) != 1 {
		return false
	}
	p := rows[0]
	if !validID(p.RequestID, "req") || p.AttemptRequestID != p.RequestID || p.KeyID != c.KeyID || p.OwnedKeyID != c.KeyID || !p.KeyCreatedAt.Equal(c.KeyCreatedAt) || p.KeyStatus != "active" || p.StartedAt.Before(c.KeyCreatedAt) || p.CompletedAt.Before(p.StartedAt) || p.CompletedAt.After(c.AsOf) || p.AttemptStartedAt.Before(p.StartedAt) || p.AttemptEndedAt.Before(p.AttemptStartedAt) || p.AttemptEndedAt.After(p.CompletedAt) || p.CallStatus != "success" || p.AttemptStatus != "success" || p.Native != "completed" || p.Protocol != "anthropic_messages" || p.AttemptNumber != 1 || p.CredentialID != c.CredentialID || p.SnapshotID != c.SnapshotID || p.CallSnapshotID != c.SnapshotID || p.ConnectionID != c.ConnectionID || p.ProviderModelID != c.ProviderModelID || p.ModelID != c.ModelID {
		return false
	}
	if c.OwnerKind == "project" {
		return p.UserID == "" && p.KeyUserID == "" && p.ProjectID == c.ProjectID && p.KeyProjectID == c.ProjectID && p.OwnerProjectID == c.ProjectID && p.ProjectCreatedAt.Equal(c.ProjectCreatedAt) && p.KeyCreatorID == c.KeyCreatorID
	}
	return p.UserID == c.UserID && p.KeyUserID == c.UserID && p.ProjectID == "" && p.KeyProjectID == "" && p.OwnerProjectID == "" && p.ProjectCreatedAt.IsZero() && p.KeyCreatorID == ""
}

func readMessagesOnce(ctx context.Context, db *gorm.DB, c messagesOnceConfig) (rows []messagesOnceProjection, err error) {
	if db == nil || db.Error != nil || ctx.Err() != nil || !validMessagesOnceExpectations(c) {
		return nil, errInvalid
	}
	tx := db.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if tx.Error != nil {
		return nil, errInvalid
	}
	defer func() {
		if recover() != nil {
			rows, err = nil, errInvalid
		}
		if tx.Rollback().Error != nil || ctx.Err() != nil {
			rows, err = nil, errInvalid
		}
	}()
	rows, err = queryMessagesOnce(tx, c)
	if err != nil || !validMessagesOnceProjection(c, rows) {
		return nil, errInvalid
	}
	return rows, nil
}

func runMessagesOnce(out io.Writer) (code int) {
	fail := func() int { _ = json.NewEncoder(out).Encode(result{Result: "evidence_rejected"}); return 1 }
	defer func() {
		if recover() != nil {
			code = fail()
		}
	}()
	raw, err := privateRead("/fixture-evidence-input/messages-once.json", 8192, true)
	if err != nil {
		return fail()
	}
	defer clear(raw)
	var c messagesOnceConfig
	if !decodeMessagesOnce(raw, &c) {
		return fail()
	}
	dsn, ok := prepareMessagesOnceConnection(&c)
	if !ok {
		return fail()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	db, err := openEvidenceDatabase(c.DatabaseDriver, dsn)
	dsn = ""
	if err != nil {
		if db != nil {
			if pool, poolErr := db.DB(); poolErr == nil {
				_ = pool.Close()
			}
		}
		return fail()
	}
	pool, err := db.DB()
	if err != nil {
		return fail()
	}
	closed := false
	defer func() {
		if !closed {
			_ = pool.Close()
		}
	}()
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(0)
	rows, readErr := readMessagesOnce(ctx, db, c)
	return finishMessagesOnce(ctx, out, c, rows, readErr, func() error {
		closed = true
		return pool.Close()
	})
}

func finishMessagesOnce(ctx context.Context, out io.Writer, c messagesOnceConfig, rows []messagesOnceProjection, readErr error, closePool func() error) (code int) {
	fail := func() int { _ = json.NewEncoder(out).Encode(result{Result: "evidence_rejected"}); return 1 }
	defer func() {
		if recover() != nil {
			code = fail()
		}
	}()
	closeErr := closePool()
	if readErr != nil || closeErr != nil || ctx.Err() != nil || !validMessagesOnceProjection(c, rows) {
		return fail()
	}
	value := messagesOnceResult{Result: "verified_exact_durable_messages_once_evidence", Operation: "messages-once", SourceCommit: c.SourceCommit, RunID: c.RunID, RequestIDSource: "exact_bounded_sql_row", ClientRequestIDDisplayed: false, Proof: rows[0]}
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > 8192 {
		return fail()
	}
	encoded = append(encoded, '\n')
	n, err := out.Write(encoded)
	if err != nil || n != len(encoded) || ctx.Err() != nil {
		return 1
	}
	return 0
}
