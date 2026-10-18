package vaultwire

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var auditUUID = regexp.MustCompile(`^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$`)
var auditHash = regexp.MustCompile(`^hmac-sha256:[a-f0-9]{64}$`)
var auditDataPath = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}/data/(?:[A-Za-z0-9_-]+/)*[A-Za-z0-9_-]{1,96}$`)
var auditLoginPath = regexp.MustCompile(`^auth/routex-f28-[a-f0-9]{16}/login$`)
var auditPolicy = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// AuditExpectation comes from original run inputs and the real audit-hash
// version-one response, never from whichever audit row happens to be present.
// It cannot establish the provenance of its caller's values or server isolation.
type AuditExpectation struct {
	ReaderMode, DataPath, LoginPath, VersionOneHMAC string
	ReaderPolicy, CleanupPolicy                     string
	Start, End                                      time.Time
}

// AuditBracket is safe paired request identity, not a numeric HTTP receipt or
// acknowledgement. Vault's logical update operation does not identify PUT/POST.
type AuditBracket struct {
	RequestIDs                                       []string
	Pairs, OwnershipReads, Logins, Destroys, Metrics int
}

// uniqueJSON refuses duplicate and case-alias keys at every depth. Only the
// observed bounded JSON profile is supported; no raw row is returned publicly.
func uniqueJSON(raw []byte) (map[string]any, error) {
	if len(raw) == 0 || len(raw) > 262144 || !utf8.Valid(raw) {
		return nil, ErrRejected
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	budget := 8192
	var value func(int) (any, error)
	value = func(depth int) (any, error) {
		budget--
		if depth > 32 || budget < 0 {
			return nil, ErrRejected
		}
		token, err := d.Token()
		if err != nil {
			return nil, ErrRejected
		}
		switch v := token.(type) {
		case json.Delim:
			switch v {
			case '{':
				out := map[string]any{}
				folded := map[string]bool{}
				for d.More() {
					key, err := d.Token()
					name, ok := key.(string)
					lower := strings.ToLower(name)
					if err != nil || !ok || len(name) > 256 || strings.ContainsRune(name, '\ufffd') || folded[lower] || len(out) >= 256 {
						return nil, ErrRejected
					}
					folded[lower] = true
					item, err := value(depth + 1)
					if err != nil {
						return nil, err
					}
					out[name] = item
				}
				end, err := d.Token()
				if err != nil || end != json.Delim('}') {
					return nil, ErrRejected
				}
				return out, nil
			case '[':
				out := []any{}
				for d.More() {
					if len(out) >= 256 {
						return nil, ErrRejected
					}
					item, err := value(depth + 1)
					if err != nil {
						return nil, err
					}
					out = append(out, item)
				}
				end, err := d.Token()
				if err != nil || end != json.Delim(']') {
					return nil, ErrRejected
				}
				return out, nil
			default:
				return nil, ErrRejected
			}
		case string:
			if strings.ContainsRune(v, '\ufffd') {
				return nil, ErrRejected
			}
			return v, nil
		case json.Number, bool, nil:
			return v, nil
		default:
			return nil, ErrRejected
		}
	}
	v, err := value(0)
	if err != nil {
		return nil, err
	}
	row, ok := v.(map[string]any)
	if !ok {
		return nil, ErrRejected
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, ErrRejected
	}
	return row, nil
}

func object(value any) (map[string]any, bool) { v, ok := value.(map[string]any); return v, ok }
func text(value any) (string, bool)           { v, ok := value.(string); return v, ok }
func allowedKeys(value map[string]any, names ...string) bool {
	allowed := map[string]bool{}
	for _, name := range names {
		allowed[name] = true
	}
	for name := range value {
		if !allowed[name] {
			return false
		}
	}
	return true
}
func absentOr(value map[string]any, key string, want any) bool {
	got, present := value[key]
	return !present || reflect.DeepEqual(got, want)
}

func auditRow(row map[string]any, wantType string, plan AuditExpectation) (map[string]any, time.Time, error) {
	if !allowedKeys(row, "type", "time", "auth", "request", "response", "error", "forwarded", "forwarded_from") || row["type"] != wantType || !absentOr(row, "error", "") || !absentOr(row, "forwarded", false) || !absentOr(row, "forwarded_from", "") {
		return nil, time.Time{}, ErrRejected
	}
	stamp, ok := text(row["time"])
	if !ok {
		return nil, time.Time{}, ErrRejected
	}
	observed, err := time.Parse(time.RFC3339Nano, stamp)
	_, offset := observed.Zone()
	if err != nil || offset != 0 || observed.Before(plan.Start) || observed.After(plan.End) {
		return nil, time.Time{}, ErrRejected
	}
	request, ok := object(row["request"])
	if !ok || !allowedKeys(request, "id", "operation", "path", "namespace", "data", "headers", "client_id", "client_token", "client_token_accessor", "mount_accessor", "mount_class", "mount_point", "mount_running_version", "mount_type", "remote_address", "remote_port", "request_uri", "wrap_ttl", "policy_override") {
		return nil, time.Time{}, ErrRejected
	}
	id, ok := text(request["id"])
	if !ok || !auditUUID.MatchString(id) {
		return nil, time.Time{}, ErrRejected
	}
	namespace, ok := object(request["namespace"])
	if !ok || !allowedKeys(namespace, "id", "path") || namespace["id"] != "root" || !absentOr(namespace, "path", "") {
		return nil, time.Time{}, ErrRejected
	}
	if request["remote_address"] != "127.0.0.1" || !absentOr(request, "wrap_ttl", json.Number("0")) || !absentOr(request, "policy_override", false) {
		return nil, time.Time{}, ErrRejected
	}
	return request, observed, nil
}

func exactPolicy(row map[string]any, want string) bool {
	auth, ok := object(row["auth"])
	if !ok {
		return false
	}
	return reflect.DeepEqual(auth["policies"], []any{want}) && absentOr(auth, "identity_policies", []any{})
}

// ParseAuditDelta consumes the complete appended byte range between two
// original audit snapshots. It never filters away unwanted callers or rows.
// The profile explicitly accounts for Vault 2.2.0 adding mount_accessor only
// to read responses; every other request field must remain identical.
func ParseAuditDelta(before, after []byte, plan AuditExpectation) (AuditBracket, error) {
	bad := func() (AuditBracket, error) { return AuditBracket{}, ErrRejected }
	if plan.Start.IsZero() || plan.End.Before(plan.Start) || plan.End.Sub(plan.Start) > 35*time.Second || !auditDataPath.MatchString(plan.DataPath) || len(plan.DataPath) > 512 || !auditHash.MatchString(plan.VersionOneHMAC) || !auditPolicy.MatchString(plan.ReaderPolicy) || !auditPolicy.MatchString(plan.CleanupPolicy) {
		return bad()
	}
	if plan.ReaderMode != "token" && plan.ReaderMode != "app_role" {
		return bad()
	}
	if (plan.ReaderMode == "token" && plan.LoginPath != "") || (plan.ReaderMode == "app_role" && !auditLoginPath.MatchString(plan.LoginPath)) {
		return bad()
	}
	if len(before) == 0 || len(after) > 1<<20 || !bytes.HasSuffix(before, []byte("\n")) || !bytes.HasSuffix(after, []byte("\n")) || !bytes.HasPrefix(after, before) {
		return bad()
	}
	type operation struct{ verb, path, policy string }
	destroyPath := strings.Replace(plan.DataPath, "/data/", "/destroy/", 1)
	wanted := []operation{{"read", "sys/metrics", "root"}}
	if plan.ReaderMode == "app_role" {
		wanted = append(wanted, operation{"update", plan.LoginPath, plan.ReaderPolicy})
	}
	wanted = append(wanted, operation{"read", plan.DataPath, plan.ReaderPolicy}, operation{"update", destroyPath, plan.CleanupPolicy}, operation{"read", "sys/metrics", "root"})
	lines := bytes.Split(after[len(before):], []byte("\n"))
	if len(lines) != len(wanted)*2+1 || len(lines[len(lines)-1]) != 0 {
		return bad()
	}
	out := AuditBracket{RequestIDs: make([]string, 0, len(wanted)), Pairs: len(wanted), OwnershipReads: 1, Destroys: 1, Metrics: 2}
	if plan.ReaderMode == "app_role" {
		out.Logins = 1
	}
	ids := map[string]bool{}
	accessors := map[string]string{}
	last := plan.Start
	for index, want := range wanted {
		left, err := uniqueJSON(lines[index*2])
		if err != nil {
			return bad()
		}
		right, err := uniqueJSON(lines[index*2+1])
		if err != nil {
			return bad()
		}
		request, start, err := auditRow(left, "request", plan)
		if err != nil || start.Before(last) {
			return bad()
		}
		response, end, err := auditRow(right, "response", plan)
		if err != nil || end.Before(start) {
			return bad()
		}
		last = end
		id := request["id"].(string)
		if ids[id] || response["id"] != id || request["operation"] != want.verb || request["path"] != want.path || response["operation"] != want.verb || response["path"] != want.path || !exactPolicy(right, want.policy) {
			return bad()
		}
		ids[id] = true
		out.RequestIDs = append(out.RequestIDs, id)
		if want.path == plan.LoginPath {
			auth, ok := object(left["auth"])
			if !ok || !absentOr(auth, "policies", []any{}) || !absentOr(auth, "client_token", "") {
				return bad()
			}
		} else if !exactPolicy(left, want.policy) {
			return bad()
		}
		if want.verb == "read" {
			if _, present := request["mount_accessor"]; present {
				return bad()
			}
			accessor, ok := text(response["mount_accessor"])
			if !ok || accessor == "" || len(accessor) > 256 {
				return bad()
			}
			if previous, present := accessors[want.path]; present && previous != accessor {
				return bad()
			}
			accessors[want.path] = accessor
			copy := make(map[string]any, len(response)-1)
			for k, v := range response {
				if k != "mount_accessor" {
					copy[k] = v
				}
			}
			if !reflect.DeepEqual(request, copy) {
				return bad()
			}
		} else if !reflect.DeepEqual(request, response) {
			return bad()
		}
		data, ok := object(request["data"])
		if !ok {
			return bad()
		}
		switch want.path {
		case "sys/metrics":
			if request["request_uri"] != "/v1/sys/metrics?format=prometheus" || !allowedKeys(data, "format") {
				return bad()
			}
			format, ok := text(data["format"])
			if !ok || !auditHash.MatchString(format) {
				return bad()
			}
		case plan.DataPath:
			if request["request_uri"] != "/v1/"+plan.DataPath+"?version=1" || len(data) != 1 || data["version"] != plan.VersionOneHMAC {
				return bad()
			}
		case destroyPath:
			if _, present := request["request_uri"]; present {
				return bad()
			}
			if len(data) != 1 || !reflect.DeepEqual(data["versions"], []any{plan.VersionOneHMAC}) {
				return bad()
			}
		case plan.LoginPath:
			if _, present := request["request_uri"]; present {
				return bad()
			}
			if len(data) != 2 || !allowedKeys(data, "role_id", "secret_id") {
				return bad()
			}
			for _, key := range []string{"role_id", "secret_id"} {
				v, ok := text(data[key])
				if !ok || !auditHash.MatchString(v) {
					return bad()
				}
			}
		default:
			return bad()
		}
	}
	return out, nil
}
