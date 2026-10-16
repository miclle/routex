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
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var errInvalid = errors.New("invalid private evidence input")
var idPattern = regexp.MustCompile(`^(usr|key|mdl|crd|con|pmd|cfg|req)_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
var passwordPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{38}$`)

type expectation struct {
	RequestID string `json:"request_id"`
	Native    string `json:"native"`
}
type config struct {
	DatabasePassword string        `json:"database_password"`
	UserID           string        `json:"user_id"`
	KeyID            string        `json:"key_id"`
	PredecessorID    string        `json:"predecessor_id"`
	ModelID          string        `json:"model_id"`
	CredentialID     string        `json:"credential_id"`
	ConnectionID     string        `json:"connection_id"`
	ProviderModelID  string        `json:"provider_model_id"`
	KeyCreatedAt     time.Time     `json:"key_created_at"`
	Requests         []expectation `json:"requests"`
}
type projection struct {
	RequestID        string    `json:"request_id"`
	AttemptRequestID string    `json:"attempt_request_id"`
	UserID           string    `json:"user_id"`
	ProjectID        string    `json:"project_id"`
	KeyID            string    `json:"key_id"`
	KeyUserID        string    `json:"key_user_id"`
	ReplacesKeyID    string    `json:"replaces_key_id"`
	KeyStatus        string    `json:"key_status"`
	KeyCreatedAt     time.Time `json:"key_created_at"`
	StartedAt        time.Time `json:"started_at"`
	CallStatus       string    `json:"call_status"`
	AttemptStatus    string    `json:"attempt_status"`
	Native           string    `json:"native"`
	CredentialID     string    `json:"credential_id"`
	SnapshotID       string    `json:"snapshot_id"`
	ConnectionID     string    `json:"connection_id"`
	ProviderModelID  string    `json:"provider_model_id"`
	ModelID          string    `json:"model_id"`
	Protocol         string    `json:"protocol"`
	AttemptNumber    int       `json:"attempt_number"`
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
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if !validJSONObject(raw) || decoder.Decode(&c) != nil || !validConfig(c) {
		return fail()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	dsn := "host=postgres port=5432 user=routex password=" + c.DatabasePassword + " dbname=routex sslmode=disable connect_timeout=2"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true})
	c.DatabasePassword = ""
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
func validConfig(c config) bool {
	if !passwordPattern.MatchString(c.DatabasePassword) || !validID(c.UserID, "usr") || !validID(c.KeyID, "key") || !validID(c.PredecessorID, "key") || c.KeyID == c.PredecessorID || !validID(c.ModelID, "mdl") || !validID(c.CredentialID, "crd") || !validID(c.ConnectionID, "con") || !validID(c.ProviderModelID, "pmd") || c.KeyCreatedAt.IsZero() || len(c.Requests) != 2 {
		return false
	}
	return validID(c.Requests[0].RequestID, "req") && validID(c.Requests[1].RequestID, "req") && c.Requests[0].RequestID != c.Requests[1].RequestID && c.Requests[0].Native == "incomplete" && c.Requests[1].Native == "completed"
}
func query(tx *gorm.DB, c config) ([]projection, error) {
	later := tx.Session(&gorm.Session{NewDB: true}).Table("call_attempts AS later").Select("1").Where("later.request_id = attempt.request_id AND later.attempt_number > attempt.attempt_number")
	var rows []projection
	err := tx.Table("call_records AS calls").Select("calls.request_id, attempt.request_id AS attempt_request_id, calls.user_id, calls.project_id, calls.key_id, owned.user_id AS key_user_id, owned.replaces_key_id, owned.status AS key_status, owned.created_at AS key_created_at, calls.started_at, calls.status AS call_status, attempt.status AS attempt_status, attempt.native_completion_evidence AS native, attempt.credential_id, attempt.snapshot_id, attempt.connection_id, attempt.provider_model_id, calls.model_id, calls.protocol, attempt.attempt_number").
		Joins("JOIN call_attempts AS attempt ON attempt.request_id = calls.request_id").
		Joins("JOIN api_keys AS owned ON owned.id = calls.key_id").
		Where("calls.request_id IN ?", []string{c.Requests[0].RequestID, c.Requests[1].RequestID}).
		Where("NOT EXISTS (?)", later).Order("calls.request_id, attempt.attempt_number").Limit(3).Scan(&rows).Error
	return rows, err
}
func validProjection(c config, rows []projection) bool {
	if len(rows) != 2 {
		return false
	}
	wants := map[string]string{c.Requests[0].RequestID: c.Requests[0].Native, c.Requests[1].RequestID: c.Requests[1].Native}
	seen := map[string]bool{}
	for _, p := range rows {
		expected, ok := wants[p.RequestID]
		if !ok || seen[p.RequestID] || p.AttemptRequestID != p.RequestID || p.UserID != c.UserID || p.ProjectID != "" || p.KeyID != c.KeyID || p.KeyUserID != c.UserID || p.ReplacesKeyID != c.PredecessorID || p.KeyStatus != "active" || !p.KeyCreatedAt.Equal(c.KeyCreatedAt) || p.StartedAt.Before(p.KeyCreatedAt) || p.CallStatus != "success" || p.AttemptStatus != "success" || p.Native != expected || p.CredentialID != c.CredentialID || !validID(p.SnapshotID, "cfg") || p.ConnectionID != c.ConnectionID || p.ProviderModelID != c.ProviderModelID || p.ModelID != c.ModelID || p.Protocol != "openai_chat" || p.AttemptNumber != 1 {
			return false
		}
		seen[p.RequestID] = true
	}
	return len(seen) == 2
}
