package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"reflect"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

// A supplied HTTP interval is not a SQL retirement timestamp. Neither Key table
// has a dedicated retired/revoked timestamp; UpdatedAt is not evidence of one.
// The coordinator must independently retain the original exact API exchange.
type messagesRetirementBoundary struct {
	Source             string    `json:"source"`
	Method             string    `json:"method"`
	Path               string    `json:"path"`
	HTTPStatus         int       `json:"http_status"`
	RequestStartedAt   time.Time `json:"request_started_at"`
	ResponseReceivedAt time.Time `json:"response_received_at"`
}

type messagesPairConfig struct {
	messagesOnceConfig
	PredecessorID        string                     `json:"predecessor_id"`
	PredecessorCreatedAt time.Time                  `json:"predecessor_created_at"`
	FirstAccepted        messagesOnceResult         `json:"first_accepted"`
	RetirementBoundary   messagesRetirementBoundary `json:"retirement_boundary"`
}

// Exported embedding keeps GORM scan metadata for every immutable Call field.
type MessagesPairCallProjection = messagesOnceProjection

type messagesPairProjection struct {
	MessagesPairCallProjection
	ReplacesKeyID        string    `json:"replaces_key_id"`
	PredecessorID        string    `json:"predecessor_id"`
	PredecessorUserID    string    `json:"predecessor_user_id"`
	PredecessorProjectID string    `json:"predecessor_project_id"`
	PredecessorCreatedAt time.Time `json:"predecessor_created_at"`
	PredecessorStatus    string    `json:"predecessor_status"`
}

type messagesPairResult struct {
	Result                               string                     `json:"result"`
	Operation                            string                     `json:"operation"`
	SourceCommit                         string                     `json:"source_commit"`
	RunID                                string                     `json:"run_id"`
	RequestIDSource                      string                     `json:"request_id_source"`
	ClientRequestIDDisplayed             bool                       `json:"client_request_id_displayed"`
	FirstAccepted                        messagesOnceResult         `json:"first_accepted"`
	Proof                                []messagesPairProjection   `json:"proof"`
	RetirementBoundary                   messagesRetirementBoundary `json:"caller_supplied_retirement_boundary"`
	RetirementBoundaryAuthenticatedBySQL bool                       `json:"retirement_boundary_authenticated_by_sql"`
}

// Every field, including zero-valued other-owner facts, is required and exact.
// Struct decoding alone would accept case aliases and silently omit fields.
func messagesPairExactFields(raw []byte, value any) bool {
	if !validJSONObject(raw) {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	t := reflect.TypeOf(value)
	if t.Kind() != reflect.Struct || len(fields) != t.NumField() {
		return false
	}
	for i := 0; i < t.NumField(); i++ {
		name := t.Field(i).Tag.Get("json")
		v, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return false
		}
	}
	return true
}

func decodeMessagesPair(raw []byte, c *messagesPairConfig) bool {
	if c == nil || len(raw) == 0 || len(raw) > 16384 || !utf8.Valid(raw) || !validJSONObject(raw) {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	for _, key := range []string{"predecessor_id", "predecessor_created_at", "first_accepted", "retirement_boundary"} {
		if v, ok := fields[key]; !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return false
		}
	}
	if !messagesPairExactFields(fields["first_accepted"], messagesOnceResult{}) || !messagesPairExactFields(fields["retirement_boundary"], messagesRetirementBoundary{}) {
		return false
	}
	var firstFields map[string]json.RawMessage
	if json.Unmarshal(fields["first_accepted"], &firstFields) != nil || !messagesPairExactFields(firstFields["proof"], messagesOnceProjection{}) {
		return false
	}
	var operation string
	if json.Unmarshal(fields["operation"], &operation) != nil || operation != "messages-pair" {
		return false
	}
	// Reuse the exact fixed endpoint/owner/secret grammar without changing once.
	base := make(map[string]json.RawMessage, len(fields))
	for k, v := range fields {
		base[k] = v
	}
	for _, k := range []string{"predecessor_id", "predecessor_created_at", "first_accepted", "retirement_boundary"} {
		delete(base, k)
	}
	base["operation"] = json.RawMessage(`"messages-once"`)
	baseRaw, err := json.Marshal(base)
	var once messagesOnceConfig
	if err != nil || !decodeMessagesOnce(baseRaw, &once) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(c) == nil && passwordPattern.MatchString(c.DatabasePassword) && validMessagesPairExpectations(*c)
}

func messagesPairOnce(c messagesPairConfig) messagesOnceConfig {
	v := c.messagesOnceConfig
	v.Operation = "messages-once"
	return v
}

func validMessagesPairExpectations(c messagesPairConfig) bool {
	once := messagesPairOnce(c)
	if c.Operation != "messages-pair" || !validMessagesOnceExpectations(once) || c.PredecessorID == c.KeyID || c.PredecessorCreatedAt.Before(c.RunStarted) || c.PredecessorCreatedAt.After(c.KeyCreatedAt) {
		return false
	}
	prefix := "key"
	path := "/api/v1/keys/" + c.PredecessorID + "/complete-rotation"
	if c.OwnerKind == "project" {
		prefix = "pky"
		path = "/api/v1/projects/" + c.ProjectID + "/keys/" + c.PredecessorID + "/complete-rotation"
	}
	if !validID(c.PredecessorID, prefix) {
		return false
	}
	first := c.FirstAccepted
	if first.Result != "verified_exact_durable_messages_once_evidence" || first.Operation != "messages-once" || first.SourceCommit != c.SourceCommit || first.RunID != c.RunID || first.RequestIDSource != "exact_bounded_sql_row" || first.ClientRequestIDDisplayed {
		return false
	}
	once.SnapshotID = first.Proof.SnapshotID
	if !validMessagesOnceProjection(once, []messagesOnceProjection{first.Proof}) {
		return false
	}
	b := c.RetirementBoundary
	return b.Source == "coordinator_original_http_interval" && b.Method == "POST" && b.Path == path && b.HTTPStatus == 204 && !b.RequestStartedAt.Before(first.Proof.CompletedAt) && !b.ResponseReceivedAt.Before(b.RequestStartedAt) && !b.ResponseReceivedAt.After(c.AsOf)
}

func prepareMessagesPairConnection(c *messagesPairConfig) (string, bool) {
	if c == nil || !passwordPattern.MatchString(c.DatabasePassword) || !validMessagesPairExpectations(*c) {
		return "", false
	}
	password := c.DatabasePassword
	c.DatabasePassword = ""
	return fixedEvidenceConnection(c.DatabaseDriver, password), true
}

func messagesPairSelection(tx *gorm.DB, c messagesPairConfig) *gorm.DB {
	selected := messagesOnceSelection(tx, messagesPairOnce(c))
	if len(selected.Statement.Selects) != 1 {
		selected.AddError(errInvalid)
		return selected
	}
	fields := selected.Statement.Selects[0] + ", owned.replaces_key_id, predecessor.id AS predecessor_id, predecessor.created_at AS predecessor_created_at, predecessor.status AS predecessor_status"
	if c.OwnerKind == "project" {
		fields += ", predecessor.project_id AS predecessor_project_id"
		selected = selected.Joins("LEFT JOIN project_api_keys AS predecessor ON predecessor.id = owned.replaces_key_id")
	} else {
		fields += ", predecessor.user_id AS predecessor_user_id"
		selected = selected.Joins("LEFT JOIN api_keys AS predecessor ON predecessor.id = owned.replaces_key_id")
	}
	// Override only the sentinel: ALL Calls and ALL Attempts remain visible.
	return selected.Select(fields).Limit(3)
}

func equalMessagesPairFirst(a, b messagesOnceProjection) bool {
	av, bv := reflect.ValueOf(a), reflect.ValueOf(b)
	for i := 0; i < av.NumField(); i++ {
		if x, ok := av.Field(i).Interface().(time.Time); ok {
			if !x.Equal(bv.Field(i).Interface().(time.Time)) {
				return false
			}
		} else if av.Field(i).Interface() != bv.Field(i).Interface() {
			return false
		}
	}
	return true
}

func validMessagesPairProjection(c messagesPairConfig, rows []messagesPairProjection) bool {
	if !validMessagesPairExpectations(c) || len(rows) != 2 || rows[0].RequestID == rows[1].RequestID {
		return false
	}
	first, second := -1, -1
	for i, row := range rows {
		if row.ReplacesKeyID != c.PredecessorID || row.PredecessorID != c.PredecessorID || !row.PredecessorCreatedAt.Equal(c.PredecessorCreatedAt) || row.PredecessorStatus != "revoked" {
			return false
		}
		if c.OwnerKind == "personal" {
			if row.PredecessorUserID != c.UserID || row.PredecessorProjectID != "" {
				return false
			}
		} else if row.PredecessorProjectID != c.ProjectID || row.PredecessorUserID != "" {
			return false
		}
		if row.RequestID == c.FirstAccepted.Proof.RequestID {
			if !equalMessagesPairFirst(row.MessagesPairCallProjection, c.FirstAccepted.Proof) {
				return false
			}
			first = i
		} else {
			if !validMessagesOnceProjection(messagesPairOnce(c), []messagesOnceProjection{row.MessagesPairCallProjection}) || row.StartedAt.Before(c.RetirementBoundary.ResponseReceivedAt) {
				return false
			}
			second = i
		}
	}
	return first >= 0 && second >= 0
}

func readMessagesPair(ctx context.Context, db *gorm.DB, c messagesPairConfig) (rows []messagesPairProjection, err error) {
	if db == nil || db.Error != nil || ctx.Err() != nil || !validMessagesPairExpectations(c) {
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
	err = messagesPairSelection(tx, c).Scan(&rows).Error
	if err != nil || !validMessagesPairProjection(c, rows) {
		return nil, errInvalid
	}
	return rows, nil
}

func runMessagesPair(out io.Writer) (code int) {
	fail := func() int { _ = json.NewEncoder(out).Encode(result{Result: "evidence_rejected"}); return 1 }
	defer func() {
		if recover() != nil {
			code = fail()
		}
	}()
	raw, err := privateRead("/fixture-evidence-input/messages-pair.json", 16384, true)
	if err != nil {
		return fail()
	}
	defer clear(raw)
	var c messagesPairConfig
	if !decodeMessagesPair(raw, &c) {
		return fail()
	}
	dsn, ok := prepareMessagesPairConnection(&c)
	if !ok {
		return fail()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	db, err := openEvidenceDatabase(c.DatabaseDriver, dsn)
	dsn = ""
	if err != nil {
		if db != nil {
			if pool, e := db.DB(); e == nil {
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
	rows, readErr := readMessagesPair(ctx, db, c)
	return finishMessagesPair(ctx, out, c, rows, readErr, func() error { closed = true; return pool.Close() })
}

func finishMessagesPair(ctx context.Context, out io.Writer, c messagesPairConfig, rows []messagesPairProjection, readErr error, closePool func() error) (code int) {
	fail := func() int { _ = json.NewEncoder(out).Encode(result{Result: "evidence_rejected"}); return 1 }
	defer func() {
		if recover() != nil {
			code = fail()
		}
	}()
	closeErr := closePool()
	if readErr != nil || closeErr != nil || ctx.Err() != nil || !validMessagesPairProjection(c, rows) {
		return fail()
	}
	// Semantic output order follows the retained first proof, never ULID order.
	ordered := append([]messagesPairProjection(nil), rows...)
	if ordered[0].RequestID != c.FirstAccepted.Proof.RequestID {
		ordered[0], ordered[1] = ordered[1], ordered[0]
	}
	value := messagesPairResult{Result: "verified_exact_durable_messages_pair_evidence", Operation: "messages-pair", SourceCommit: c.SourceCommit, RunID: c.RunID, RequestIDSource: "exact_bounded_sql_rows", FirstAccepted: c.FirstAccepted, Proof: ordered, RetirementBoundary: c.RetirementBoundary}
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > 16384 {
		return fail()
	}
	encoded = append(encoded, '\n')
	n, err := out.Write(encoded)
	if err != nil || n != len(encoded) || ctx.Err() != nil {
		return 1
	}
	return 0
}
