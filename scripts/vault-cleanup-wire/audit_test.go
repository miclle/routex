package vaultwire

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func auditFixture(mode string) ([]byte, []byte, AuditExpectation, []map[string]any) {
	start := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	p := AuditExpectation{ReaderMode: mode, DataPath: "kv/data/discovery/" + strings.Repeat("a", 32), VersionOneHMAC: "hmac-sha256:" + strings.Repeat("b", 64), ReaderPolicy: "discovery-reader", CleanupPolicy: "discovery-cleanup", Start: start, End: start.Add(10 * time.Second)}
	type step struct{ op, path, policy string }
	steps := []step{{"read", "sys/metrics", "root"}}
	if mode == "app_role" {
		p.LoginPath = "auth/routex-f28-" + strings.Repeat("c", 16) + "/login"
		steps = append(steps, step{"update", p.LoginPath, p.ReaderPolicy})
	}
	destroy := strings.Replace(p.DataPath, "/data/", "/destroy/", 1)
	steps = append(steps, step{"read", p.DataPath, p.ReaderPolicy}, step{"update", destroy, p.CleanupPolicy}, step{"read", "sys/metrics", "root"})
	rows := []map[string]any{}
	for i, s := range steps {
		r := map[string]any{"id": fmt.Sprintf("%08x-0000-4000-8000-000000000001", i+1), "operation": s.op, "path": s.path, "namespace": map[string]any{"id": "root"}, "remote_address": "127.0.0.1"}
		switch s.path {
		case "sys/metrics":
			r["request_uri"] = "/v1/sys/metrics?format=prometheus"
			r["data"] = map[string]any{"format": "hmac-sha256:" + strings.Repeat("d", 64)}
		case p.DataPath:
			r["request_uri"] = "/v1/" + p.DataPath + "?version=1"
			r["data"] = map[string]any{"version": p.VersionOneHMAC}
		case destroy:
			r["data"] = map[string]any{"versions": []any{p.VersionOneHMAC}}
		default:
			r["data"] = map[string]any{"role_id": "hmac-sha256:" + strings.Repeat("e", 64), "secret_id": "hmac-sha256:" + strings.Repeat("f", 64)}
		}
		response := map[string]any{}
		for k, v := range r {
			response[k] = v
		}
		if s.op == "read" {
			response["mount_accessor"] = "controlled_accessor"
		}
		rows = append(rows, map[string]any{"type": "request", "time": start.Add(time.Duration(i*2) * time.Millisecond).Format(time.RFC3339Nano), "request": r, "auth": map[string]any{"policies": []any{s.policy}}}, map[string]any{"type": "response", "time": start.Add(time.Duration(i*2+1) * time.Millisecond).Format(time.RFC3339Nano), "request": response, "auth": map[string]any{"policies": []any{s.policy}}, "response": map[string]any{}})
		if s.path == p.LoginPath {
			rows[len(rows)-2]["auth"] = map[string]any{"token_type": "default"}
		}
	}
	before := []byte("original complete prefix\n")
	return before, encodeAudit(before, rows), p, rows
}

func encodeAudit(before []byte, rows []map[string]any) []byte {
	var out bytes.Buffer
	out.Write(before)
	for _, row := range rows {
		raw, err := json.Marshal(row)
		if err != nil {
			panic("invalid test fixture")
		}
		out.Write(raw)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

func TestObservedTokenAndAppRoleAuditBrackets(t *testing.T) {
	for _, mode := range []string{"token", "app_role"} {
		t.Run(mode, func(t *testing.T) {
			before, after, plan, _ := auditFixture(mode)
			got, err := ParseAuditDelta(before, after, plan)
			want := 4
			if mode == "app_role" {
				want = 5
			}
			if err != nil || got.Pairs != want || got.Destroys != 1 || got.OwnershipReads != 1 || got.Metrics != 2 || len(got.RequestIDs) != want {
				t.Fatal("observed audit shape refused")
			}
		})
	}
}

func TestCompleteAuditDeltaRefusesMissingExtraAndChangedRequestFacts(t *testing.T) {
	for _, c := range []struct {
		name   string
		mutate func([]map[string]any) []map[string]any
	}{
		{"missing", func(r []map[string]any) []map[string]any { return r[1:] }},
		{"extra", func(r []map[string]any) []map[string]any { return append(r, r[0]) }},
		{"reordered", func(r []map[string]any) []map[string]any { r[0], r[1] = r[1], r[0]; return r }},
		{"duplicate-id", func(r []map[string]any) []map[string]any {
			r[2]["request"].(map[string]any)["id"] = r[0]["request"].(map[string]any)["id"]
			return r
		}},
		{"foreign-scope", func(r []map[string]any) []map[string]any {
			r[0]["request"].(map[string]any)["namespace"] = map[string]any{"id": "other"}
			return r
		}},
		{"forwarded", func(r []map[string]any) []map[string]any { r[0]["forwarded"] = true; return r }},
		{"null-forwarded", func(r []map[string]any) []map[string]any { r[0]["forwarded"] = nil; return r }},
		{"remote-caller", func(r []map[string]any) []map[string]any {
			r[0]["request"].(map[string]any)["remote_address"] = "192.0.2.1"
			return r
		}},
		{"unknown-request-field", func(r []map[string]any) []map[string]any {
			r[0]["request"].(map[string]any)["unqualified"] = false
			return r
		}},
		{"unknown-read-transition", func(r []map[string]any) []map[string]any {
			r[1]["request"].(map[string]any)["mount_type"] = "unreviewed"
			return r
		}},
		{"missing-accessor", func(r []map[string]any) []map[string]any {
			delete(r[1]["request"].(map[string]any), "mount_accessor")
			return r
		}},
		{"changed-metric-accessor", func(r []map[string]any) []map[string]any {
			r[9]["request"].(map[string]any)["mount_accessor"] = "changed_accessor"
			return r
		}},
		{"foreign-request-policy", func(r []map[string]any) []map[string]any {
			r[4]["auth"] = map[string]any{"policies": []any{"root"}}
			return r
		}},
		{"authenticated-login-input", func(r []map[string]any) []map[string]any {
			r[2]["auth"] = map[string]any{"policies": []any{"root"}}
			return r
		}},
		{"request-accessor", func(r []map[string]any) []map[string]any {
			r[0]["request"].(map[string]any)["mount_accessor"] = "unreviewed"
			return r
		}},
		{"broader-policy", func(r []map[string]any) []map[string]any {
			r[5]["auth"] = map[string]any{"policies": []any{"discovery-reader", "root"}}
			return r
		}},
		{"wrong-query", func(r []map[string]any) []map[string]any {
			r[4]["request"].(map[string]any)["request_uri"] = "/v1/other?version=1"
			return r
		}},
		{"null-query", func(r []map[string]any) []map[string]any {
			r[6]["request"].(map[string]any)["request_uri"] = nil
			return r
		}},
		{"invented-clear-version", func(r []map[string]any) []map[string]any {
			r[6]["request"].(map[string]any)["data"] = map[string]any{"versions": []any{1}}
			return r
		}},
		{"wrong-version-hmac", func(r []map[string]any) []map[string]any {
			r[6]["request"].(map[string]any)["data"] = map[string]any{"versions": []any{"hmac-sha256:" + strings.Repeat("f", 64)}}
			return r
		}},
		{"outside-clock", func(r []map[string]any) []map[string]any { r[0]["time"] = "2026-10-18T00:00:00Z"; return r }},
	} {
		t.Run(c.name, func(t *testing.T) {
			before, _, plan, rows := auditFixture("app_role")
			if _, err := ParseAuditDelta(before, encodeAudit(before, c.mutate(rows)), plan); err != ErrRejected {
				t.Fatal("unknown or incomplete audit admitted")
			}
		})
	}
}

func TestAuditPrefixAndStrictJSONCannotHideUnknownRows(t *testing.T) {
	before, after, plan, _ := auditFixture("app_role")
	if _, err := ParseAuditDelta([]byte("replaced prefix\n"), after, plan); err != ErrRejected {
		t.Fatal("prefix replacement accepted")
	}
	if _, err := ParseAuditDelta(before, after[:len(after)-1], plan); err != ErrRejected {
		t.Fatal("truncated tail accepted")
	}
	for _, raw := range []string{`{"id":1,"id":2}`, `{"id":1,"ID":2}`, `{"request":{"data":{"versions":1,"versions":2}}}`, `{"time":null} {}`, `[]`, `{"value":"\ud800"}`} {
		if _, err := uniqueJSON([]byte(raw)); err != ErrRejected {
			t.Fatal("ambiguous JSON accepted")
		}
	}
	bad := plan
	bad.End = bad.Start.Add(36 * time.Second)
	if _, err := ParseAuditDelta(before, after, bad); err != ErrRejected {
		t.Fatal("larger caller window accepted")
	}
	bad = plan
	bad.ReaderMode = "unknown"
	if _, err := ParseAuditDelta(before, after, bad); err != ErrRejected {
		t.Fatal("unknown profile accepted")
	}
}
