// This private acceptance command performs only a bounded read-only projection.
package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
)

var errInvalid = errors.New("invalid private evidence input")
var idPattern = regexp.MustCompile(`^(usr|prj|key|pky|mdl|crd|con|pmd|cfg|req)_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
var passwordPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{38}$`)

type expectation struct {
	RequestID string `json:"request_id"`
	Native    string `json:"native"`
}
type config struct {
	DatabaseDriver       string        `json:"database_driver,omitempty"`
	OwnerKind            string        `json:"owner_kind,omitempty"`
	ProjectID            string        `json:"project_id,omitempty"`
	ProjectCreatedAt     time.Time     `json:"project_created_at,omitempty"`
	KeyCreatorID         string        `json:"key_creator_id,omitempty"`
	PredecessorCreatedAt time.Time     `json:"predecessor_created_at,omitempty"`
	DatabasePassword     string        `json:"database_password"`
	UserID               string        `json:"user_id"`
	KeyID                string        `json:"key_id"`
	PredecessorID        string        `json:"predecessor_id"`
	ModelID              string        `json:"model_id"`
	CredentialID         string        `json:"credential_id"`
	ConnectionID         string        `json:"connection_id"`
	ProviderModelID      string        `json:"provider_model_id"`
	KeyCreatedAt         time.Time     `json:"key_created_at"`
	Requests             []expectation `json:"requests"`
}
type projection struct {
	KeyProjectID         string    `json:"key_project_id,omitempty"`
	KeyCreatorID         string    `json:"key_creator_id,omitempty"`
	ProjectCreatedAt     time.Time `json:"project_created_at,omitempty"`
	PredecessorProjectID string    `json:"predecessor_project_id,omitempty"`
	PredecessorCreatedAt time.Time `json:"predecessor_created_at,omitempty"`
	RequestID            string    `json:"request_id"`
	AttemptRequestID     string    `json:"attempt_request_id"`
	UserID               string    `json:"user_id"`
	ProjectID            string    `json:"project_id"`
	KeyID                string    `json:"key_id"`
	KeyUserID            string    `json:"key_user_id"`
	ReplacesKeyID        string    `json:"replaces_key_id"`
	KeyStatus            string    `json:"key_status"`
	KeyCreatedAt         time.Time `json:"key_created_at"`
	StartedAt            time.Time `json:"started_at"`
	CallStatus           string    `json:"call_status"`
	AttemptStatus        string    `json:"attempt_status"`
	Native               string    `json:"native"`
	CredentialID         string    `json:"credential_id"`
	SnapshotID           string    `json:"snapshot_id"`
	ConnectionID         string    `json:"connection_id"`
	ProviderModelID      string    `json:"provider_model_id"`
	ModelID              string    `json:"model_id"`
	Protocol             string    `json:"protocol"`
	AttemptNumber        int       `json:"attempt_number"`
}
type result struct {
	Result string       `json:"result"`
	Proof  []projection `json:"proof,omitempty"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }
func run(args []string, out io.Writer) int {
	fail := func() int { _ = json.NewEncoder(out).Encode(result{Result: "evidence_rejected"}); return 1 }
	if len(args) != 0 {
		return fail()
	}
	raw, err := privateRead("/fixture-evidence-input/config.json", 8192, false)
	if err != nil {
		return fail()
	}
	defer clear(raw)
	var c config
	if !decodeConfig(raw, &c) {
		return fail()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	dsn, ok := prepareConnection(&c)
	if !ok {
		return fail()
	}
	db, err := openEvidenceDatabase(c.DatabaseDriver, dsn)
	dsn = ""
	if err != nil {
		return fail()
	}
	pool, err := db.DB()
	if err != nil {
		return fail()
	}
	defer func() { _ = pool.Close() }()
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(0)
	tx := db.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if tx.Error != nil {
		return fail()
	}
	defer tx.Rollback()
	rows, err := query(tx, c)
	if err != nil || !validProjection(c, rows) {
		return fail()
	}
	// Commit only closes a server-enforced read-only snapshot; no mutation API,
	// migration, raw Exec, schema write or business service is called.
	if tx.Commit().Error != nil {
		return fail()
	}
	_ = json.NewEncoder(out).Encode(result{Result: "verified_exact_durable_native_evidence", Proof: rows})
	return 0
}
func validID(value, prefix string) bool {
	return idPattern.MatchString(value) && len(value) == len(prefix)+27 && value[:len(prefix)+1] == prefix+"_"
}

// An omitted owner_kind preserves the original Personal config contract. Project
// evidence is selected explicitly and never borrows the issuing manager's UserID.
func decodeConfig(raw []byte, c *config) bool {
	if c == nil || !validJSONObject(raw) {
		return false
	}
	// Struct decoding folds field names. Check the canonical discriminator before
	// decoding so aliases cannot default null or override a reviewed owner kind.
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	for field := range fields {
		if (strings.EqualFold(field, "owner_kind") && field != "owner_kind") || (strings.EqualFold(field, "database_driver") && field != "database_driver") {
			return false
		}
	}
	if value, present := fields["owner_kind"]; present {
		var owner string
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &owner) != nil || owner != "personal" && owner != "project" {
			return false
		}
	}
	if value, present := fields["database_driver"]; present {
		var driver string
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &driver) != nil || driver != "postgres" && driver != "mysql" {
			return false
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(c) == nil && validConfig(*c)
}

func validOwnerConfig(c config) bool {
	switch c.OwnerKind {
	case "", "personal":
		return validID(c.UserID, "usr") && validID(c.KeyID, "key") && validID(c.PredecessorID, "key") && c.ProjectID == "" && c.ProjectCreatedAt.IsZero() && c.KeyCreatorID == "" && c.PredecessorCreatedAt.IsZero()
	case "project":
		return c.UserID == "" && validID(c.ProjectID, "prj") && validID(c.KeyID, "pky") && validID(c.PredecessorID, "pky") && validID(c.KeyCreatorID, "usr") && !c.ProjectCreatedAt.IsZero() && !c.PredecessorCreatedAt.IsZero() && !c.PredecessorCreatedAt.Before(c.ProjectCreatedAt) && !c.KeyCreatedAt.Before(c.PredecessorCreatedAt)
	default:
		return false
	}
}
func validConfig(c config) bool {
	return passwordPattern.MatchString(c.DatabasePassword) && validExpectations(c)
}
func validExpectations(c config) bool {
	if !validDatabaseDriver(c.DatabaseDriver) || !validOwnerConfig(c) || c.KeyID == c.PredecessorID || !validID(c.ModelID, "mdl") || !validID(c.CredentialID, "crd") || !validID(c.ConnectionID, "con") || !validID(c.ProviderModelID, "pmd") || c.KeyCreatedAt.IsZero() || len(c.Requests) != 2 {
		return false
	}
	return validID(c.Requests[0].RequestID, "req") && validID(c.Requests[1].RequestID, "req") && c.Requests[0].RequestID != c.Requests[1].RequestID && c.Requests[0].Native == "incomplete" && c.Requests[1].Native == "completed"
}
func query(tx *gorm.DB, c config) ([]projection, error) {
	if !validExpectations(c) {
		return nil, errInvalid
	}
	later := tx.Session(&gorm.Session{NewDB: true}).Table("call_attempts AS later").Select("1").Where("later.request_id = attempt.request_id AND later.attempt_number > attempt.attempt_number")
	// These fixed bounded diagnostics deliberately project immutable Attempt
	// attribution. No table/column/SQL comes from config or the current catalogue.
	fields := "calls.request_id, attempt.request_id AS attempt_request_id, calls.user_id, calls.project_id, calls.key_id, owned.replaces_key_id, owned.status AS key_status, owned.created_at AS key_created_at, calls.started_at, calls.status AS call_status, attempt.status AS attempt_status, attempt.native_completion_evidence AS native, attempt.credential_id, attempt.snapshot_id, attempt.connection_id, attempt.provider_model_id, calls.model_id, calls.protocol, attempt.attempt_number"
	selected := tx.Table("call_records AS calls").Joins("JOIN call_attempts AS attempt ON attempt.request_id = calls.request_id")
	if c.OwnerKind == "project" {
		fields += ", owned.project_id AS key_project_id, owned.creator_id AS key_creator_id, owner_project.created_at AS project_created_at, predecessor.project_id AS predecessor_project_id, predecessor.created_at AS predecessor_created_at"
		selected = selected.Joins("JOIN project_api_keys AS owned ON owned.id = calls.key_id").Joins("JOIN projects AS owner_project ON owner_project.id = owned.project_id").Joins("JOIN project_api_keys AS predecessor ON predecessor.id = owned.replaces_key_id")
	} else {
		fields += ", owned.user_id AS key_user_id"
		selected = selected.Joins("JOIN api_keys AS owned ON owned.id = calls.key_id")
	}
	var rows []projection
	err := selected.Select(fields).Where("calls.request_id IN ?", []string{c.Requests[0].RequestID, c.Requests[1].RequestID}).Where("NOT EXISTS (?)", later).Order("calls.request_id, attempt.attempt_number").Limit(3).Scan(&rows).Error
	return rows, err
}
func validOwnerProjection(c config, p projection) bool {
	if c.OwnerKind == "project" {
		return p.UserID == "" && p.KeyUserID == "" && p.ProjectID == c.ProjectID && p.KeyProjectID == c.ProjectID && p.KeyCreatorID == c.KeyCreatorID && p.ProjectCreatedAt.Equal(c.ProjectCreatedAt) && p.PredecessorProjectID == c.ProjectID && p.PredecessorCreatedAt.Equal(c.PredecessorCreatedAt)
	}
	return p.UserID == c.UserID && p.KeyUserID == c.UserID && p.ProjectID == "" && p.KeyProjectID == "" && p.KeyCreatorID == "" && p.ProjectCreatedAt.IsZero() && p.PredecessorProjectID == "" && p.PredecessorCreatedAt.IsZero()
}
func validProjection(c config, rows []projection) bool {
	if !validExpectations(c) || len(rows) != 2 {
		return false
	}
	wants := map[string]string{c.Requests[0].RequestID: c.Requests[0].Native, c.Requests[1].RequestID: c.Requests[1].Native}
	seen := map[string]bool{}
	for _, p := range rows {
		expected, ok := wants[p.RequestID]
		if !ok || seen[p.RequestID] || p.AttemptRequestID != p.RequestID || !validOwnerProjection(c, p) || p.KeyID != c.KeyID || p.ReplacesKeyID != c.PredecessorID || p.KeyStatus != "active" || !p.KeyCreatedAt.Equal(c.KeyCreatedAt) || p.StartedAt.Before(p.KeyCreatedAt) || p.CallStatus != "success" || p.AttemptStatus != "success" || p.Native != expected || p.CredentialID != c.CredentialID || !validID(p.SnapshotID, "cfg") || p.ConnectionID != c.ConnectionID || p.ProviderModelID != c.ProviderModelID || p.ModelID != c.ModelID || p.Protocol != "openai_chat" || p.AttemptNumber != 1 {
			return false
		}
		seen[p.RequestID] = true
	}
	return len(seen) == 2
}
