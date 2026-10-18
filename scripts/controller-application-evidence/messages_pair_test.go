package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func messagesPairFixture(owner string) (messagesPairConfig, []messagesPairProjection) {
	once, first := messagesOnceFixture(owner)
	predecessor := "key_01arz3ndektsv4rrffq69g5faw"
	if owner == "project" {
		predecessor = "pky_01arz3ndektsv4rrffq69g5faw"
	}
	c := messagesPairConfig{messagesOnceConfig: once, PredecessorID: predecessor, PredecessorCreatedAt: once.RunStarted.Add(time.Millisecond)}
	c.Operation = "messages-pair"
	c.FirstAccepted = messagesOnceResult{Result: "verified_exact_durable_messages_once_evidence", Operation: "messages-once", SourceCommit: once.SourceCommit, RunID: once.RunID, RequestIDSource: "exact_bounded_sql_row", Proof: first[0]}
	path := "/api/v1/keys/" + predecessor + "/complete-rotation"
	if owner == "project" {
		path = "/api/v1/projects/" + c.ProjectID + "/keys/" + predecessor + "/complete-rotation"
	}
	c.RetirementBoundary = messagesRetirementBoundary{Source: "coordinator_original_http_interval", Method: "POST", Path: path, HTTPStatus: 204, RequestStartedAt: once.KeyCreatedAt.Add(5 * time.Second), ResponseReceivedAt: once.KeyCreatedAt.Add(6 * time.Second)}
	rows := []messagesPairProjection{{MessagesPairCallProjection: first[0], ReplacesKeyID: predecessor, PredecessorID: predecessor, PredecessorCreatedAt: c.PredecessorCreatedAt, PredecessorStatus: "revoked"}}
	if owner == "project" {
		rows[0].PredecessorProjectID = c.ProjectID
	} else {
		rows[0].PredecessorUserID = c.UserID
	}
	second := rows[0]
	// A lexically smaller second RequestID intentionally disproves ULID ordering.
	second.RequestID, second.AttemptRequestID = "req_01arz3ndektsv4rrffq69g5faa", "req_01arz3ndektsv4rrffq69g5faa"
	second.StartedAt, second.CompletedAt = once.KeyCreatedAt.Add(7*time.Second), once.KeyCreatedAt.Add(10*time.Second)
	second.AttemptStartedAt, second.AttemptEndedAt = once.KeyCreatedAt.Add(8*time.Second), once.KeyCreatedAt.Add(9*time.Second)
	c.SnapshotID = "cfg_01arz3ndektsv4rrffq69g5fax"
	second.SnapshotID, second.CallSnapshotID = c.SnapshotID, c.SnapshotID
	return c, append(rows, second)
}

func messagesPairFields(t *testing.T, c messagesPairConfig) map[string]json.RawMessage {
	t.Helper()
	fields := messagesOnceFields(t, c.messagesOnceConfig)
	for name, value := range map[string]any{"predecessor_id": c.PredecessorID, "predecessor_created_at": c.PredecessorCreatedAt, "first_accepted": c.FirstAccepted, "retirement_boundary": c.RetirementBoundary} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal("fixed pair input encoding failed")
		}
		fields[name] = raw
	}
	return fields
}

func TestMessagesPairClosedConfigRequiresCompleteExactPriorReceiptAndBoundary(t *testing.T) {
	for _, owner := range []string{"personal", "project"} {
		t.Run(owner, func(t *testing.T) {
			c, _ := messagesPairFixture(owner)
			fields := messagesPairFields(t, c)
			raw, _ := json.Marshal(fields)
			if !decodeMessagesPair(raw, new(messagesPairConfig)) || decodeMessagesOnce(raw, new(messagesOnceConfig)) || decodeConfig(raw, new(config)) {
				t.Fatal("pair grammar is not valid and disjoint")
			}
			for key, original := range fields {
				delete(fields, key)
				bad, _ := json.Marshal(fields)
				if decodeMessagesPair(bad, new(messagesPairConfig)) {
					t.Fatal("missing required pair field accepted")
				}
				fields[key] = json.RawMessage("null")
				bad, _ = json.Marshal(fields)
				if decodeMessagesPair(bad, new(messagesPairConfig)) {
					t.Fatal("null required pair field accepted")
				}
				fields[key] = original
			}
			for _, name := range []string{"request_id", "requests", "protocol", "dsn", "OWNER_KIND", "Operation", "updated_at"} {
				fields[name] = json.RawMessage(`"foreign"`)
				bad, _ := json.Marshal(fields)
				if decodeMessagesPair(bad, new(messagesPairConfig)) {
					t.Fatal("selector, alias or invented retirement time accepted")
				}
				delete(fields, name)
			}
			for _, component := range []string{"first_accepted", "proof", "retirement_boundary"} {
				for _, mode := range []string{"missing", "alias", "unknown", "null", "duplicate"} {
					firstRaw, _ := json.Marshal(c.FirstAccepted)
					boundaryRaw, _ := json.Marshal(c.RetirementBoundary)
					var first, proof, boundary map[string]json.RawMessage
					_ = json.Unmarshal(firstRaw, &first)
					_ = json.Unmarshal(first["proof"], &proof)
					_ = json.Unmarshal(boundaryRaw, &boundary)
					target, key := first, "source_commit"
					if component == "proof" {
						target, key = proof, "project_created_at"
					}
					if component == "retirement_boundary" {
						target, key = boundary, "http_status"
					}
					original := target[key]
					switch mode {
					case "missing":
						delete(target, key)
					case "alias":
						delete(target, key)
						target[strings.ToUpper(key)] = original
					case "unknown":
						target["private_secret"] = json.RawMessage(`"PRIVATE_CANARY"`)
					case "null":
						target[key] = json.RawMessage("null")
					}
					changed, _ := json.Marshal(target)
					if mode == "duplicate" {
						changed = append([]byte(`{"`+key+`":`+string(original)+`,`), changed[1:]...)
					}
					if component == "proof" {
						first["proof"] = changed
						changed, _ = json.Marshal(first)
					}
					badFields := messagesPairFields(t, c)
					if component == "retirement_boundary" {
						badFields["retirement_boundary"] = changed
					} else {
						badFields["first_accepted"] = changed
					}
					bad, _ := json.Marshal(badFields)
					if decodeMessagesPair(bad, new(messagesPairConfig)) {
						t.Fatal("weak nested receipt or boundary schema accepted")
					}
				}
			}
			for _, bad := range [][]byte{append(raw, []byte(` {}`)...), {0xff}, bytes.Repeat([]byte(" "), 16385)} {
				if decodeMessagesPair(bad, new(messagesPairConfig)) {
					t.Fatal("trailing/invalid/oversize input accepted")
				}
			}
		})
	}
}

func TestMessagesPairPreservesEveryFirstFactAndDoesNotInferRequestOrder(t *testing.T) {
	for _, owner := range []string{"personal", "project"} {
		c, rows := messagesPairFixture(owner)
		if !validMessagesPairProjection(c, rows) || !validMessagesPairProjection(c, []messagesPairProjection{rows[1], rows[0]}) {
			t.Fatal("exact pair or unordered SQL rows refused")
		}
		for i := 0; i < reflect.TypeOf(messagesOnceProjection{}).NumField(); i++ {
			bad := append([]messagesPairProjection(nil), rows...)
			field := reflect.ValueOf(&bad[0].MessagesPairCallProjection).Elem().Field(i)
			switch field.Kind() {
			case reflect.String:
				field.SetString(field.String() + "changed")
			case reflect.Int:
				field.SetInt(field.Int() + 1)
			case reflect.Struct:
				field.Set(reflect.ValueOf(field.Interface().(time.Time).Add(time.Nanosecond)))
			default:
				t.Fatal("new proof field needs a mutation control")
			}
			if validMessagesPairProjection(c, bad) {
				t.Fatal("changed first immutable fact accepted")
			}
		}
		for _, bad := range [][]messagesPairProjection{nil, rows[:1], {rows[0], rows[0]}, append(append([]messagesPairProjection(nil), rows...), rows[1])} {
			if validMessagesPairProjection(c, bad) {
				t.Fatal("missing/duplicate/extra Call or Attempt accepted")
			}
		}
	}
}

func TestMessagesPairRequiresExactRetiredPredecessorAndIndependentlyExpectedFreshFinalCall(t *testing.T) {
	for _, owner := range []string{"personal", "project"} {
		c, rows := messagesPairFixture(owner)
		for name, mutate := range map[string]func(*messagesPairProjection){
			"broken replacement link":     func(p *messagesPairProjection) { p.ReplacesKeyID = "" },
			"foreign predecessor":         func(p *messagesPairProjection) { p.PredecessorID = strings.ToUpper(p.PredecessorID) },
			"stale predecessor birth":     func(p *messagesPairProjection) { p.PredecessorCreatedAt = p.PredecessorCreatedAt.Add(time.Nanosecond) },
			"active predecessor":          func(p *messagesPairProjection) { p.PredecessorStatus = "active" },
			"foreign personal owner":      func(p *messagesPairProjection) { p.PredecessorUserID = "usr_01arz3ndektsv4rrffq69g5fax" },
			"foreign project owner":       func(p *messagesPairProjection) { p.PredecessorProjectID = "prj_01arz3ndektsv4rrffq69g5fax" },
			"revoked replacement":         func(p *messagesPairProjection) { p.KeyStatus = "revoked" },
			"retry":                       func(p *messagesPairProjection) { p.AttemptNumber = 2 },
			"foreign final snapshot":      func(p *messagesPairProjection) { p.SnapshotID = c.FirstAccepted.Proof.SnapshotID },
			"foreign final Call snapshot": func(p *messagesPairProjection) { p.CallSnapshotID = c.FirstAccepted.Proof.SnapshotID },
			"native incomplete":           func(p *messagesPairProjection) { p.Native = "incomplete" },
			"failed attempt":              func(p *messagesPairProjection) { p.AttemptStatus = "error" },
			"before retirement reply": func(p *messagesPairProjection) {
				p.StartedAt = c.RetirementBoundary.ResponseReceivedAt.Add(-time.Nanosecond)
			},
		} {
			t.Run(owner+"/"+name, func(t *testing.T) {
				bad := append([]messagesPairProjection(nil), rows...)
				mutate(&bad[1])
				if validMessagesPairProjection(c, bad) {
					t.Fatal("weak final pair accepted")
				}
			})
		}
		for name, mutate := range map[string]func(*messagesPairConfig){
			"foreign source":               func(c *messagesPairConfig) { c.FirstAccepted.SourceCommit = strings.Repeat("c", 40) },
			"foreign run":                  func(c *messagesPairConfig) { c.FirstAccepted.RunID = strings.Repeat("c", 32) },
			"foreign Key":                  func(c *messagesPairConfig) { c.FirstAccepted.Proof.KeyID = c.PredecessorID },
			"client display claim":         func(c *messagesPairConfig) { c.FirstAccepted.ClientRequestIDDisplayed = true },
			"old predecessor":              func(c *messagesPairConfig) { c.PredecessorCreatedAt = c.RunStarted.Add(-time.Nanosecond) },
			"post replacement predecessor": func(c *messagesPairConfig) { c.PredecessorCreatedAt = c.KeyCreatedAt.Add(time.Nanosecond) },
			"SQL timestamp claim":          func(c *messagesPairConfig) { c.RetirementBoundary.Source = "sql_retired_at" },
			"wrong method":                 func(c *messagesPairConfig) { c.RetirementBoundary.Method = "GET" },
			"arbitrary URL":                func(c *messagesPairConfig) { c.RetirementBoundary.Path = "https://example.invalid" },
			"wrong predecessor path": func(c *messagesPairConfig) {
				c.RetirementBoundary.Path = strings.ReplaceAll(c.RetirementBoundary.Path, c.PredecessorID, c.KeyID)
			},
			"wrong HTTP status": func(c *messagesPairConfig) { c.RetirementBoundary.HTTPStatus = 200 },
			"before first completion": func(c *messagesPairConfig) {
				c.RetirementBoundary.RequestStartedAt = c.FirstAccepted.Proof.CompletedAt.Add(-time.Nanosecond)
			},
			"reversed interval": func(c *messagesPairConfig) {
				c.RetirementBoundary.ResponseReceivedAt = c.RetirementBoundary.RequestStartedAt.Add(-time.Nanosecond)
			},
		} {
			t.Run(owner+"/"+name, func(t *testing.T) {
				bad := c
				mutate(&bad)
				if validMessagesPairProjection(bad, rows) {
					t.Fatal("foreign prior receipt or unauthenticated boundary accepted")
				}
			})
		}
	}
}

func TestMessagesPairQueryRetainsAllCallsAttemptsAndPredecessorWithLimitThree(t *testing.T) {
	db, err := openEvidenceDatabase("postgres", "host=postgres user=routex dbname=routex sslmode=disable")
	if err != nil {
		t.Fatal("offline query builder unavailable")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("offline query pool unavailable")
	}
	defer func() { _ = pool.Close() }()
	for _, owner := range []string{"personal", "project"} {
		c, _ := messagesPairFixture(owner)
		statement := db.ToSQL(func(tx *gorm.DB) *gorm.DB { return messagesPairSelection(tx, c).Find(&[]messagesPairProjection{}) })
		if !strings.Contains(statement, "LEFT JOIN call_attempts AS attempt") || !strings.Contains(statement, " AS predecessor ON predecessor.id = owned.replaces_key_id") || !strings.Contains(statement, "LIMIT 3") || strings.Contains(statement, "LIMIT 2") || strings.Contains(statement, "NOT EXISTS") || strings.Contains(statement, " IN ") {
			t.Fatal("pair sentinel or complete left joins changed")
		}
		_, where, ok := strings.Cut(statement, " WHERE ")
		where, _, _ = strings.Cut(where, " ORDER BY ")
		if !ok || !strings.HasPrefix(where, "calls.key_id = ") || strings.Contains(where, " AND ") || strings.Contains(where, " OR ") {
			t.Fatal("unexpected outcomes hidden by pair filters")
		}
	}
}

func TestMessagesPairConsumesSecretAndRequiresClosedSuccessfulReadBeforeSafeOutput(t *testing.T) {
	for _, owner := range []string{"personal", "project"} {
		c, rows := messagesPairFixture(owner)
		for _, driver := range []string{"", "postgres", "mysql"} {
			copy := c
			copy.DatabaseDriver = driver
			dsn, ok := prepareMessagesPairConnection(&copy)
			if !ok || dsn != fixedEvidenceConnection(driver, c.DatabasePassword) || copy.DatabasePassword != "" || !validMessagesPairProjection(copy, rows) {
				t.Fatal("pair password consumption changed fixed connections or proof")
			}
			if _, ok := prepareMessagesPairConnection(&copy); ok {
				t.Fatal("pair password reused")
			}
		}
		for _, failure := range []string{"query", "context", "close", "panic", "invalid"} {
			ctx, cancel := context.WithCancel(context.Background())
			var out bytes.Buffer
			proof, readErr, closes := rows, error(nil), 0
			if failure == "query" {
				readErr = errors.New("PRIVATE_DATABASE_ERROR")
			}
			if failure == "context" {
				cancel()
			}
			if failure == "invalid" {
				proof = nil
			}
			code := finishMessagesPair(ctx, &out, c, proof, readErr, func() error {
				closes++
				if failure == "panic" {
					panic("PRIVATE_CLOSE_ERROR")
				}
				if failure == "close" {
					return errors.New("PRIVATE_CLOSE_ERROR")
				}
				return nil
			})
			cancel()
			if code != 1 || closes != 1 || out.String() != "{\"result\":\"evidence_rejected\"}\n" {
				t.Fatal("failed closure/read leaked or produced success")
			}
		}
		var out bytes.Buffer
		if finishMessagesPair(context.Background(), &out, c, []messagesPairProjection{rows[1], rows[0]}, nil, func() error { return nil }) != 0 {
			t.Fatal("valid closed pair refused")
		}
		var got messagesPairResult
		if json.Unmarshal(out.Bytes(), &got) != nil || got.Result != "verified_exact_durable_messages_pair_evidence" || !equalMessagesPairFirst(got.FirstAccepted.Proof, c.FirstAccepted.Proof) || got.Proof[0].RequestID != c.FirstAccepted.Proof.RequestID || got.ClientRequestIDDisplayed || got.RetirementBoundaryAuthenticatedBySQL || got.RetirementBoundary.Source != "coordinator_original_http_interval" {
			t.Fatal("pair confused retained first, current SQL and supplied HTTP facts")
		}
		for _, forbidden := range []string{c.DatabasePassword, "database_password", "PRIVATE_", "token_hash"} {
			if strings.Contains(out.String(), forbidden) {
				t.Fatal("private material leaked")
			}
		}
		if finishMessagesPair(context.Background(), messagesShortWriter{}, c, rows, nil, func() error { return nil }) != 1 {
			t.Fatal("short pair output accepted")
		}
	}
}

func TestMessagesPairDispatchRefusesAliasesAndExtraArguments(t *testing.T) {
	for _, args := range [][]string{{"Messages-pair"}, {"messages_pair"}, {"messages-pair", "PRIVATE_ARGUMENT"}} {
		var out bytes.Buffer
		if run(args, &out) != 1 || out.String() != "{\"result\":\"evidence_rejected\"}\n" {
			t.Fatal("pair alias/extra argument accepted or echoed")
		}
	}
}

func TestMessagesPairGORMScanRetainsAllImmutableAndPredecessorFields(t *testing.T) {
	base, err := schema.Parse(&messagesOnceProjection{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal("original scan metadata unavailable")
	}
	pair, err := schema.Parse(&messagesPairProjection{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal("pair scan metadata unavailable")
	}
	for name := range base.FieldsByDBName {
		if pair.FieldsByDBName[name] == nil {
			t.Fatalf("immutable scan destination missing: %s", name)
		}
	}
	for _, name := range []string{"replaces_key_id", "predecessor_id", "predecessor_user_id", "predecessor_project_id", "predecessor_created_at", "predecessor_status"} {
		if pair.FieldsByDBName[name] == nil {
			t.Fatalf("predecessor scan destination missing: %s", name)
		}
	}
}
