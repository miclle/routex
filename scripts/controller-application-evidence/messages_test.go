package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func messagesOnceFixture(owner string) (messagesOnceConfig, []messagesOnceProjection) {
	pair, old := fixture()
	c := messagesOnceConfig{Operation: "messages-once", SourceCommit: strings.Repeat("a", 40), RunID: strings.Repeat("b", 32), DatabasePassword: pair.DatabasePassword, OwnerKind: owner, UserID: pair.UserID, KeyID: pair.KeyID, KeyCreatedAt: pair.KeyCreatedAt, ModelID: pair.ModelID, CredentialID: pair.CredentialID, ConnectionID: pair.ConnectionID, ProviderModelID: pair.ProviderModelID, SnapshotID: old[0].SnapshotID, RunStarted: pair.KeyCreatedAt.Add(-time.Second), AsOf: pair.KeyCreatedAt.Add(time.Minute)}
	p := messagesOnceProjection{RequestID: old[1].RequestID, AttemptRequestID: old[1].RequestID, UserID: c.UserID, KeyID: c.KeyID, OwnedKeyID: c.KeyID, KeyUserID: c.UserID, KeyCreatedAt: c.KeyCreatedAt, KeyStatus: "active", StartedAt: c.KeyCreatedAt.Add(time.Second), CompletedAt: c.KeyCreatedAt.Add(4 * time.Second), AttemptStartedAt: c.KeyCreatedAt.Add(2 * time.Second), AttemptEndedAt: c.KeyCreatedAt.Add(3 * time.Second), CallStatus: "success", AttemptStatus: "success", Native: "completed", CredentialID: c.CredentialID, CallSnapshotID: c.SnapshotID, SnapshotID: c.SnapshotID, ConnectionID: c.ConnectionID, ProviderModelID: c.ProviderModelID, ModelID: c.ModelID, Protocol: "anthropic_messages", AttemptNumber: 1}
	if owner == "project" {
		c.UserID, c.KeyCreatorID = "", c.UserID
		c.ProjectID, c.KeyID = "prj_01arz3ndektsv4rrffq69g5fav", "pky_01arz3ndektsv4rrffq69g5fav"
		c.ProjectCreatedAt = c.KeyCreatedAt.Add(-time.Hour)
		p.UserID, p.KeyUserID = "", ""
		p.KeyID, p.OwnedKeyID = c.KeyID, c.KeyID
		p.ProjectID, p.KeyProjectID, p.OwnerProjectID = c.ProjectID, c.ProjectID, c.ProjectID
		p.KeyCreatorID, p.ProjectCreatedAt = c.KeyCreatorID, c.ProjectCreatedAt
	}
	return c, []messagesOnceProjection{p}
}

func messagesOnceFields(t *testing.T, c messagesOnceConfig) map[string]json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal("fixed Messages config encoding failed")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("fixed Messages config fields unavailable")
	}
	if c.OwnerKind == "personal" {
		delete(fields, "project_id")
		delete(fields, "project_created_at")
		delete(fields, "key_creator_id")
	} else {
		delete(fields, "user_id")
	}
	return fields
}

func TestMessagesOnceConfigIsClosedDisjointAndRequiresExactSourceOwnerAndWindow(t *testing.T) {
	for _, owner := range []string{"personal", "project"} {
		t.Run(owner, func(t *testing.T) {
			c, _ := messagesOnceFixture(owner)
			fields := messagesOnceFields(t, c)
			raw, _ := json.Marshal(fields)
			var decoded messagesOnceConfig
			if !decodeMessagesOnce(raw, &decoded) {
				t.Fatal("exact one-call Messages configuration refused")
			}
			var pair config
			if decodeConfig(raw, &pair) {
				t.Fatal("Messages configuration borrowed as a Chat pair")
			}
			for key, original := range fields {
				for _, bad := range []json.RawMessage{nil, json.RawMessage("null")} {
					if bad == nil {
						delete(fields, key)
					} else {
						fields[key] = bad
					}
					input, _ := json.Marshal(fields)
					if decodeMessagesOnce(input, new(messagesOnceConfig)) {
						t.Fatal("missing or null required Messages field accepted")
					}
				}
				fields[key] = original
			}
			for _, key := range []string{"Operation", "OWNER_KIND", "Source_Commit", "DATABASE_DRIVER", "request_id", "requests", "protocol", "predecessor_id", "host", "dsn", "path"} {
				fields[key] = json.RawMessage(`"foreign"`)
				input, _ := json.Marshal(fields)
				if decodeMessagesOnce(input, new(messagesOnceConfig)) {
					t.Fatal("alias or operation/network/request selector accepted")
				}
				delete(fields, key)
			}
			if owner == "personal" {
				fields["project_id"] = json.RawMessage(`""`)
			} else {
				fields["user_id"] = json.RawMessage(`""`)
			}
			input, _ := json.Marshal(fields)
			if decodeMessagesOnce(input, new(messagesOnceConfig)) {
				t.Fatal("other-owner field accepted even when empty")
			}
			for _, bad := range [][]byte{append([]byte(`{"operation":"messages-once",`), raw[1:]...), append(append([]byte{}, raw...), []byte(` {}`)...), {0xff}, bytes.Repeat([]byte(" "), 8193)} {
				if decodeMessagesOnce(bad, new(messagesOnceConfig)) {
					t.Fatal("duplicate/trailing/invalid UTF-8/overflow input accepted")
				}
			}
			for _, driver := range []string{"postgres", "mysql"} {
				fields = messagesOnceFields(t, c)
				fields["database_driver"] = json.RawMessage(`"` + driver + `"`)
				input, _ := json.Marshal(fields)
				if !decodeMessagesOnce(input, new(messagesOnceConfig)) {
					t.Fatal("canonical fixed driver refused")
				}
			}
			for _, driver := range []string{`null`, `""`, `"MYSQL"`, `"postgresql"`, `true`} {
				fields["database_driver"] = json.RawMessage(driver)
				input, _ := json.Marshal(fields)
				if decodeMessagesOnce(input, new(messagesOnceConfig)) {
					t.Fatal("ambiguous explicit driver accepted")
				}
			}
		})
	}
	for name, mutate := range map[string]func(*messagesOnceConfig){
		"wrong operation":    func(c *messagesOnceConfig) { c.Operation = "chat" },
		"case source":        func(c *messagesOnceConfig) { c.SourceCommit = strings.Repeat("A", 40) },
		"wrong run":          func(c *messagesOnceConfig) { c.RunID = "unknown" },
		"missing snapshot":   func(c *messagesOnceConfig) { c.SnapshotID = "" },
		"missing owner":      func(c *messagesOnceConfig) { c.OwnerKind = "" },
		"case owner":         func(c *messagesOnceConfig) { c.OwnerKind = "PERSONAL" },
		"zero run start":     func(c *messagesOnceConfig) { c.RunStarted = time.Time{} },
		"reversed interval":  func(c *messagesOnceConfig) { c.AsOf = c.RunStarted.Add(-time.Second) },
		"unbounded interval": func(c *messagesOnceConfig) { c.AsOf = c.RunStarted.Add(15*time.Minute + time.Nanosecond) },
		"old Key":            func(c *messagesOnceConfig) { c.KeyCreatedAt = c.RunStarted.Add(-time.Nanosecond) },
		"future Key":         func(c *messagesOnceConfig) { c.KeyCreatedAt = c.AsOf.Add(time.Nanosecond) },
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := messagesOnceFixture("personal")
			mutate(&c)
			if validMessagesOnceConfig(c) {
				t.Fatal("noncanonical Messages expectations accepted")
			}
		})
	}
}

func TestMessagesOnceRequiresSingleSoleCompletedAttemptAndExactImmutableAttribution(t *testing.T) {
	for _, owner := range []string{"personal", "project"} {
		t.Run(owner, func(t *testing.T) {
			c, rows := messagesOnceFixture(owner)
			if !validMessagesOnceProjection(c, rows) {
				t.Fatal("exact native Messages proof refused")
			}
			for name, mutate := range map[string]func(*messagesOnceProjection){
				"missing RequestID":        func(p *messagesOnceProjection) { p.RequestID = "" },
				"wrong request join":       func(p *messagesOnceProjection) { p.AttemptRequestID = "req_01arz3ndektsv4rrffq69g5fax" },
				"foreign Key":              func(p *messagesOnceProjection) { p.KeyID = "key_01arz3ndektsv4rrffq69g5fax" },
				"collated owned alias":     func(p *messagesOnceProjection) { p.OwnedKeyID = strings.ToUpper(p.OwnedKeyID) },
				"changed Key birth":        func(p *messagesOnceProjection) { p.KeyCreatedAt = p.KeyCreatedAt.Add(time.Nanosecond) },
				"inactive Key":             func(p *messagesOnceProjection) { p.KeyStatus = "revoked" },
				"call predates Key":        func(p *messagesOnceProjection) { p.StartedAt = c.KeyCreatedAt.Add(-time.Nanosecond) },
				"future call":              func(p *messagesOnceProjection) { p.CompletedAt = c.AsOf.Add(time.Nanosecond) },
				"unfinished call":          func(p *messagesOnceProjection) { p.CompletedAt = time.Time{} },
				"attempt predates call":    func(p *messagesOnceProjection) { p.AttemptStartedAt = p.StartedAt.Add(-time.Nanosecond) },
				"unfinished attempt":       func(p *messagesOnceProjection) { p.AttemptEndedAt = time.Time{} },
				"attempt after final call": func(p *messagesOnceProjection) { p.AttemptEndedAt = p.CompletedAt.Add(time.Nanosecond) },
				"failed call":              func(p *messagesOnceProjection) { p.CallStatus = "error" },
				"failed last Attempt":      func(p *messagesOnceProjection) { p.AttemptStatus = "error" },
				"unknown native":           func(p *messagesOnceProjection) { p.Native = "unknown" },
				"incomplete native":        func(p *messagesOnceProjection) { p.Native = "incomplete" },
				"handoff native":           func(p *messagesOnceProjection) { p.Native = "handoff" },
				"blocked native":           func(p *messagesOnceProjection) { p.Native = "blocked" },
				"wrong protocol":           func(p *messagesOnceProjection) { p.Protocol = "openai_chat" },
				"retry ordinal":            func(p *messagesOnceProjection) { p.AttemptNumber = 2 },
				"foreign Credential":       func(p *messagesOnceProjection) { p.CredentialID = "crd_01arz3ndektsv4rrffq69g5fax" },
				"foreign publication":      func(p *messagesOnceProjection) { p.SnapshotID = "cfg_01arz3ndektsv4rrffq69g5fax" },
				"Call publication differs": func(p *messagesOnceProjection) { p.CallSnapshotID = "cfg_01arz3ndektsv4rrffq69g5fax" },
				"foreign Connection":       func(p *messagesOnceProjection) { p.ConnectionID = "con_01arz3ndektsv4rrffq69g5fax" },
				"foreign ProviderModel":    func(p *messagesOnceProjection) { p.ProviderModelID = "pmd_01arz3ndektsv4rrffq69g5fax" },
				"foreign Model":            func(p *messagesOnceProjection) { p.ModelID = "mdl_01arz3ndektsv4rrffq69g5fax" },
				"borrow Personal owner":    func(p *messagesOnceProjection) { p.UserID = "usr_01arz3ndektsv4rrffq69g5fax" },
				"borrow Key owner":         func(p *messagesOnceProjection) { p.KeyUserID = "usr_01arz3ndektsv4rrffq69g5fax" },
				"borrow Project":           func(p *messagesOnceProjection) { p.ProjectID = "prj_01arz3ndektsv4rrffq69g5fax" },
				"borrow owned Project":     func(p *messagesOnceProjection) { p.KeyProjectID = "prj_01arz3ndektsv4rrffq69g5fax" },
				"foreign current Project":  func(p *messagesOnceProjection) { p.OwnerProjectID = "prj_01arz3ndektsv4rrffq69g5fax" },
				"stale Project birth":      func(p *messagesOnceProjection) { p.ProjectCreatedAt = c.KeyCreatedAt },
				"borrow creator":           func(p *messagesOnceProjection) { p.KeyCreatorID = "usr_01arz3ndektsv4rrffq69g5fax" },
			} {
				t.Run(name, func(t *testing.T) {
					bad := append([]messagesOnceProjection{}, rows...)
					mutate(&bad[0])
					if validMessagesOnceProjection(c, bad) {
						t.Fatal("weak or foreign one-call proof accepted")
					}
				})
			}
			for _, bad := range [][]messagesOnceProjection{nil, {}, append(rows, rows[0])} {
				if validMessagesOnceProjection(c, bad) {
					t.Fatal("missing/ambiguous/extra Call or Attempt accepted")
				}
			}
		})
	}
}

func TestMessagesOnceSecretConsumptionPreservesDefaultChatPairAndFixedConnections(t *testing.T) {
	for _, owner := range []string{"personal", "project"} {
		for _, driver := range []string{"", "postgres", "mysql"} {
			c, rows := messagesOnceFixture(owner)
			c.DatabaseDriver = driver
			pair, _ := fixture()
			pair.DatabaseDriver = driver
			want, pairOK := prepareConnection(&pair)
			dsn, ok := prepareMessagesOnceConnection(&c)
			if !ok || !pairOK || dsn != want || c.DatabasePassword != "" || !validMessagesOnceProjection(c, rows) {
				t.Fatal("fixed connection or post-consumption proof changed")
			}
			if _, ok := prepareMessagesOnceConnection(&c); ok {
				t.Fatal("consumed Messages password reused")
			}
		}
	}
	pair, rows := fixture()
	raw, _ := json.Marshal(pair)
	if !decodeConfig(raw, new(config)) || !validProjection(pair, rows) || decodeMessagesOnce(raw, new(messagesOnceConfig)) {
		t.Fatal("existing default Chat pair grammar changed")
	}
}

func TestMessagesOnceQueryRetainsAllCallsAndAttemptsWithLimitTwo(t *testing.T) {
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
		c, _ := messagesOnceFixture(owner)
		statement := db.ToSQL(func(tx *gorm.DB) *gorm.DB { return messagesOnceSelection(tx, c).Find(&[]messagesOnceProjection{}) })
		if !strings.Contains(statement, "LEFT JOIN call_attempts AS attempt") || !strings.Contains(statement, "LIMIT 2") || strings.Contains(statement, "NOT EXISTS") || strings.Contains(statement, " IN ") {
			t.Fatal("query can hide missing attempts or additional retries")
		}
		_, where, ok := strings.Cut(statement, " WHERE ")
		where, _, _ = strings.Cut(where, " ORDER BY ")
		if !ok || !strings.HasPrefix(where, "calls.key_id = ") || strings.Contains(where, " AND ") || strings.Contains(where, " OR ") {
			t.Fatal("query filters away unexpected ownership/protocol/outcomes")
		}
	}
}

func TestMessagesOnceOutputRequiresClosedSnapshotPoolAndNeverClaimsClientDisplay(t *testing.T) {
	c, rows := messagesOnceFixture("personal")
	for _, name := range []string{"query", "pool_close", "context", "close_panic", "invalid_rows"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			proof, readErr, closes := rows, error(nil), 0
			if name == "query" {
				readErr = errors.New("PRIVATE_DATABASE_ERROR")
			}
			if name == "context" {
				cancel()
			}
			if name == "invalid_rows" {
				proof = nil
			}
			var out bytes.Buffer
			code := finishMessagesOnce(ctx, &out, c, proof, readErr, func() error {
				closes++
				if name == "close_panic" {
					panic("PRIVATE_CLOSE_ERROR")
				}
				if name == "pool_close" {
					return errors.New("PRIVATE_CLOSE_ERROR")
				}
				return nil
			})
			if code != 1 || closes != 1 || out.String() != "{\"result\":\"evidence_rejected\"}\n" {
				t.Fatal("unknown query/close/context manufactured or leaked a proof")
			}
		})
	}
	var out bytes.Buffer
	if finishMessagesOnce(context.Background(), &out, c, rows, nil, func() error { return nil }) != 0 {
		t.Fatal("exact one-call safe output refused")
	}
	var value messagesOnceResult
	if json.Unmarshal(out.Bytes(), &value) != nil || value.Proof.RequestID != rows[0].RequestID || value.RequestIDSource != "exact_bounded_sql_row" || value.ClientRequestIDDisplayed || value.SourceCommit != c.SourceCommit || value.RunID != c.RunID {
		t.Fatal("SQL RequestID confused with client-displayed evidence")
	}
	for _, forbidden := range []string{"database_password", "token_hash", "Authorization", "PRIVATE_", c.DatabasePassword} {
		if strings.Contains(out.String(), forbidden) {
			t.Fatal("private material entered Messages evidence")
		}
	}
	if finishMessagesOnce(context.Background(), messagesShortWriter{}, c, rows, nil, func() error { return nil }) != 1 {
		t.Fatal("short safe-output write accepted")
	}
}

type messagesShortWriter struct{}

func (messagesShortWriter) Write(raw []byte) (int, error) { return len(raw) - 1, io.ErrShortWrite }

func TestMessagesOnceDispatchRejectsAliasesAndExtraArgumentsWithoutEcho(t *testing.T) {
	for _, args := range [][]string{{"Messages-once"}, {"messages_once"}, {"messages-once", "PRIVATE_ARGUMENT"}} {
		var out bytes.Buffer
		if run(args, &out) != 1 || out.String() != "{\"result\":\"evidence_rejected\"}\n" {
			t.Fatal("Messages operation alias or extra argument accepted or echoed")
		}
	}
}
