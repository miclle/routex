package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func fixture() (config, []projection) {
	id := func(p string) string { return p + "_01arz3ndektsv4rrffq69g5fav" }
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	c := config{DatabasePassword: strings.Repeat("p", 38), UserID: id("usr"), KeyID: id("key"), PredecessorID: "key_01arz3ndektsv4rrffq69g5faw", ModelID: id("mdl"), CredentialID: id("crd"), ConnectionID: id("con"), ProviderModelID: id("pmd"), KeyCreatedAt: now, Requests: []expectation{{id("req"), "incomplete"}, {"req_01arz3ndektsv4rrffq69g5faw", "completed"}}}
	rows := make([]projection, 2)
	for i, expected := range c.Requests {
		rows[i] = projection{RequestID: expected.RequestID, AttemptRequestID: expected.RequestID, UserID: c.UserID, KeyID: c.KeyID, KeyUserID: c.UserID, ReplacesKeyID: c.PredecessorID, KeyStatus: "active", KeyCreatedAt: now, StartedAt: now.Add(time.Second), CallStatus: "success", AttemptStatus: "success", Native: expected.Native, CredentialID: c.CredentialID, SnapshotID: id("cfg"), ConnectionID: c.ConnectionID, ProviderModelID: c.ProviderModelID, ModelID: c.ModelID, Protocol: "openai_chat", AttemptNumber: 1}
	}
	return c, rows
}
func TestExactProjectionRequiresNativeIdentityBirthAndFinalSuccess(t *testing.T) {
	c, rows := fixture()
	if !validConfig(c) || !validProjection(c, rows) {
		t.Fatal("valid exact proof rejected")
	}
	for name, mutate := range map[string]func(*projection){
		"wrong owner": func(p *projection) { p.UserID = "usr_wrong" }, "wrong key owner": func(p *projection) { p.KeyUserID = "usr_wrong" }, "wrong key": func(p *projection) { p.KeyID = c.PredecessorID }, "wrong predecessor": func(p *projection) { p.ReplacesKeyID = c.KeyID }, "project attribution": func(p *projection) { p.ProjectID = "prj_other" }, "pre-birth": func(p *projection) { p.StartedAt = p.KeyCreatedAt.Add(-time.Nanosecond) }, "changed birth": func(p *projection) { p.KeyCreatedAt = p.KeyCreatedAt.Add(time.Nanosecond) }, "wrong marker": func(p *projection) { p.Native = "completed" }, "unknown marker": func(p *projection) { p.Native = "unknown" }, "casefold marker": func(p *projection) { p.Native = "INCOMPLETE" }, "failed attempt": func(p *projection) { p.AttemptStatus = "error" }, "failed call": func(p *projection) { p.CallStatus = "error" }, "wrong ordinal": func(p *projection) { p.AttemptNumber = 2 }, "missing credential": func(p *projection) { p.CredentialID = "" }, "missing snapshot": func(p *projection) { p.SnapshotID = "" }, "wrong request join": func(p *projection) { p.AttemptRequestID = c.Requests[1].RequestID }, "wrong connection": func(p *projection) { p.ConnectionID = "con_other" }, "wrong model": func(p *projection) { p.ModelID = "mdl_other" }, "wrong protocol": func(p *projection) { p.Protocol = "openai_responses" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := append([]projection{}, rows...)
			mutate(&bad[0])
			if validProjection(c, bad) {
				t.Fatal("invalid durable evidence accepted")
			}
		})
	}
	for _, bad := range [][]projection{nil, rows[:1], {rows[0], rows[0]}, append(rows, rows[1])} {
		if validProjection(c, bad) {
			t.Fatal("ambiguous row count accepted")
		}
	}
}
func TestConfigRejectsArbitraryNetworkAndUnboundedOrDuplicateRequests(t *testing.T) {
	c, _ := fixture()
	for name, mutate := range map[string]func(*config){"password injection": func(c *config) { c.DatabasePassword = "password host=elsewhere" }, "casefold ID": func(c *config) { c.UserID = strings.ToUpper(c.UserID) }, "same predecessor": func(c *config) { c.PredecessorID = c.KeyID }, "zero birth": func(c *config) { c.KeyCreatedAt = time.Time{} }, "duplicate requests": func(c *config) { c.Requests = []expectation{c.Requests[0], c.Requests[0]} }, "wrong order": func(c *config) { c.Requests = []expectation{c.Requests[1], c.Requests[0]} }} {
		t.Run(name, func(t *testing.T) {
			bad := c
			mutate(&bad)
			if validConfig(bad) {
				t.Fatal("invalid config accepted")
			}
		})
	}
}
func TestErrorsNeverPrintArgumentsOrDatabaseCredentials(t *testing.T) {
	const secret = "NeverPrintPassword"
	var out bytes.Buffer
	if run([]string{secret}, &out) != 1 || strings.Contains(out.String(), secret) {
		t.Fatal("argument secret leaked")
	}
	c, rows := fixture()
	_ = c
	encoded, err := json.Marshal(result{Result: "verified_exact_durable_native_evidence", Proof: rows})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "database_password") || strings.Contains(string(encoded), "token_hash") || strings.Contains(string(encoded), "Authorization") {
		t.Fatal("secret projection fields")
	}
}
