package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	cleanupevidence "github.com/miclle/routex/scripts/provider-credential-cleanup-evidence"
)

func fixture(t *testing.T, phase string) (bindings, input) {
	t.Helper()
	source, run, mode, uid := BuildSource, BuildRunID, BuildPhase, BuildTrustedHostUID
	t.Cleanup(func() { BuildSource, BuildRunID, BuildPhase, BuildTrustedHostUID = source, run, mode, uid })
	BuildSource, BuildRunID, BuildPhase, BuildTrustedHostUID = strings.Repeat("a", 40), strings.Repeat("b", 32), phase, "501"
	b, err := compiledBindings(501, 501)
	if err != nil {
		t.Fatal("controlled compile binding rejected")
	}
	start := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	id := func(p string) string { return fmt.Sprintf("%s_%026d", p, 1) }
	i := input{Source: b.source, Run: b.run, Password: strings.Repeat("p", 38), CreationRequestID: "00000000-0000-4000-8000-000000000001", RequestID: "00000000-0000-4000-8000-000000000002", ActorID: id("usr"), ActorBirth: start.Add(time.Second), IntegrationID: id("vlt"), IntegrationBirth: start.Add(2 * time.Second), RevisionID: id("vlr"), CredentialID: id("crd"), CredentialBirth: start.Add(4 * time.Second), CreationBirth: start.Add(5 * time.Second), CreatorProcessID: id("ins"), CreatorGeneration: strings.Repeat("c", 64), CreatorBirth: start, PhysicalObject: strings.Repeat("d", 64), OperationProof: strings.Repeat("e", 64), ReviewedETag: strings.Repeat("f", 64) + "." + strings.Repeat("a", 64), Reason: "SYNTHETIC_PRIVATE_REASON", RunStarted: start, AsOf: start.Add(50 * time.Second), RootEpoch: 1}
	return b, i
}

func TestCompileBindingRequiresCanonicalSourceRunPhaseAndExactNonRootUID(t *testing.T) {
	_, _ = fixture(t, "normal")
	for _, phase := range []string{"normal", "f02"} {
		BuildPhase = phase
		b, err := compiledBindings(501, 501)
		want := "acknowledged"
		if phase == "f02" {
			want = "pending"
		}
		if err != nil || b.state != want {
			t.Fatal("compiled phase did not derive fixed historical state")
		}
	}
	for _, values := range [][2]int{{0, 0}, {501, 0}, {0, 501}, {502, 502}, {501, 502}, {-1, -1}} {
		if _, err := compiledBindings(values[0], values[1]); err == nil {
			t.Fatal("foreign or setuid runtime accepted")
		}
	}
	for name, change := range map[string]func(){
		"empty source":            func() { BuildSource = "" },
		"case source":             func() { BuildSource = strings.Repeat("A", 40) },
		"foreign run grammar":     func() { BuildRunID = "../foreign" },
		"leading zero UID":        func() { BuildTrustedHostUID = "0501" },
		"root UID":                func() { BuildTrustedHostUID = "0" },
		"overflow UID":            func() { BuildTrustedHostUID = "4294967296" },
		"caller historical state": func() { BuildPhase = "pending" },
		"case phase":              func() { BuildPhase = "NORMAL" },
		"dummy phase":             func() { BuildPhase = "dummy" },
	} {
		t.Run(name, func(t *testing.T) {
			source, run, phase, uid := BuildSource, BuildRunID, BuildPhase, BuildTrustedHostUID
			defer func() { BuildSource, BuildRunID, BuildPhase, BuildTrustedHostUID = source, run, phase, uid }()
			change()
			if _, err := compiledBindings(501, 501); err == nil {
				t.Fatal("invalid compile binding accepted")
			}
		})
	}
}

func TestCanonicalStdinRejectsMissingNullAliasesDuplicatesAndOverrides(t *testing.T) {
	b, i := fixture(t, "f02")
	raw, err := json.Marshal(i)
	if err != nil {
		t.Fatal("fixture encode failed")
	}
	if parsed, err := decodeInput(raw, b); err != nil || parsed.expectation(b).Phase != "pending" {
		t.Fatal("exact input rejected")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("fixture fields rejected")
	}
	for _, key := range inputKeys {
		original := fields[key]
		for _, missing := range []bool{false, true} {
			if missing {
				delete(fields, key)
			} else {
				fields[key] = json.RawMessage("null")
			}
			bad, _ := json.Marshal(fields)
			if _, err := decodeInput(bad, b); err == nil {
				t.Fatal("missing or null canonical field accepted")
			}
		}
		fields[key] = original
	}
	for _, key := range []string{"Source_Commit", "DATABASE_PASSWORD", "phase", "database_driver", "host", "port", "database", "user", "dsn", "path"} {
		fields[key] = json.RawMessage(`"foreign"`)
		bad, _ := json.Marshal(fields)
		if _, err := decodeInput(bad, b); err == nil {
			t.Fatal("alias, state or endpoint override accepted")
		}
		delete(fields, key)
	}
	duplicate := append([]byte(`{"database_password":"`+strings.Repeat("q", 38)+`",`), raw[1:]...)
	for _, bad := range [][]byte{duplicate, append(append([]byte{}, raw...), []byte(` {}`)...), {0xff}, []byte(`[]`), bytes.Repeat([]byte(" "), 16385)} {
		if _, err := decodeInput(bad, b); err == nil {
			t.Fatal("duplicate, extra or malformed input accepted")
		}
	}
	for _, key := range []string{"source_commit", "run_id", "database_password"} {
		original := fields[key]
		fields[key] = json.RawMessage(`"foreign"`)
		bad, _ := json.Marshal(fields)
		if _, err := decodeInput(bad, b); err == nil {
			t.Fatal("foreign source/run or unsafe password accepted")
		}
		fields[key] = original
	}
}

func TestConnectionConsumesSecretAndRetainsOnlyFixedNormalOrF02Namespace(t *testing.T) {
	for _, phase := range []string{"normal", "f02"} {
		t.Run(phase, func(t *testing.T) {
			b, i := fixture(t, phase)
			before := i.expectation(b)
			password := i.Password
			dsn, err := consumeConnection(&i, b)
			want := "host=postgres port=5432 user=routex_f28_" + phase + " password=" + password + " dbname=routex_f28_" + phase + "_" + b.run + " sslmode=disable connect_timeout=2 application_name=routex-f28-evidence-" + phase + "-" + b.run[:16]
			if err != nil || dsn != want || i.Password != "" || i.expectation(b) != before {
				t.Fatal("fixed connection or secret consumption altered expectations")
			}
			if dsn, err := consumeConnection(&i, b); err == nil || dsn != "" {
				t.Fatal("consumed secret reused")
			}
		})
	}
	b, i := fixture(t, "normal")
	i.Password = "password host=foreign"
	if dsn, err := consumeConnection(&i, b); err == nil || dsn != "" {
		t.Fatal("injected secret accepted")
	}
	if dsn, err := consumeConnection(nil, b); err == nil || dsn != "" {
		t.Fatal("nil connection accepted")
	}
}

type orderedWriter struct {
	closeCalls  *int
	value       bytes.Buffer
	short, fail bool
	afterWrite  func()
}

func (w *orderedWriter) Write(raw []byte) (int, error) {
	if *w.closeCalls != 1 {
		return 0, errors.New("write before close")
	}
	if w.fail {
		return 0, errors.New("SYNTHETIC_PRIVATE_OUTPUT_FAILURE")
	}
	if w.short {
		return 0, nil
	}
	n, err := w.value.Write(raw)
	if w.afterWrite != nil {
		w.afterWrite()
	}
	return n, err
}

func TestSafeOutputRequiresQueryContextAndPoolClosureBeforeFirstWrite(t *testing.T) {
	for _, mode := range []string{"success", "query refused", "close refused", "cancelled", "close panic"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			calls := 0
			out := &orderedWriter{closeCalls: &calls}
			var queryErr error
			if mode == "query refused" {
				queryErr = errors.New("SYNTHETIC_PRIVATE_QUERY")
			}
			// A terminal-ordering seam only. This empty synthetic Evidence is
			// not a successful library projection or native/database proof.
			code := complete(ctx, out, cleanupevidence.Evidence{}, queryErr, func() error {
				calls++
				if mode == "close panic" {
					panic("SYNTHETIC_PRIVATE_CLOSE")
				}
				if mode == "close refused" {
					return errors.New("SYNTHETIC_PRIVATE_CLOSE")
				}
				return nil
			})
			if calls != 1 || (code == 0) != (mode == "success") || strings.Contains(out.value.String(), "SYNTHETIC") {
				t.Fatal("unknown query/context/pool closure passed or leaked")
			}
			if mode != "success" && out.value.String() != rejected {
				t.Fatal("rejection wire changed")
			}
			if mode == "success" {
				var value map[string]json.RawMessage
				if json.Unmarshal(out.value.Bytes(), &value) != nil || len(value) != 4 || string(value["runtime_acceptance"]) != "false" || string(value["database_capability"]) != `"postgres_private_controlled"` {
					t.Fatal("safe status/proof boundary changed")
				}
			}
		})
	}
}

func TestShortOrFailedOutputNeverReportsSuccessfulTerminal(t *testing.T) {
	for _, short := range []bool{false, true} {
		calls := 0
		out := &orderedWriter{closeCalls: &calls, short: short, fail: !short}
		if complete(context.Background(), out, cleanupevidence.Evidence{}, nil, func() error { calls++; return nil }) != 1 || calls != 1 {
			t.Fatal("failed output became success or repeated pool closure")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	out := &orderedWriter{closeCalls: &calls, afterWrite: cancel}
	if complete(ctx, out, cleanupevidence.Evidence{}, nil, func() error { calls++; return nil }) != 1 || calls != 1 {
		t.Fatal("context expiry during output became a successful terminal")
	}
}

func TestArgumentsNeverEchoAndConnectionEnvironmentCannotOverride(t *testing.T) {
	var out bytes.Buffer
	if run([]string{"SYNTHETIC_PRIVATE_ARGUMENT"}, &out) != 1 || out.String() != rejected {
		t.Fatal("argument echoed or read before rejection")
	}
	if !cleanConnectionEnvironment([]string{"PATH=/usr/bin", "ROUTEX_RUN=public"}) {
		t.Fatal("clean fixed-capability environment rejected")
	}
	for _, name := range []string{"PGHOST", "PGOPTIONS", "PGSERVICEFILE", "PGPASSWORD"} {
		t.Run(name, func(t *testing.T) {
			if cleanConnectionEnvironment([]string{"PATH=/usr/bin", name + "=SYNTHETIC_PRIVATE_OVERRIDE"}) {
				t.Fatal("libpq environment override accepted")
			}
		})
	}
}
