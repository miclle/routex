//go:build darwin || linux

package deliverycontroller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type deliveryFixture struct {
	cfg             Config
	paths           deliveryPaths
	launches        string
	pulls           atomic.Int32
	reports         atomic.Int32
	mu              sync.Mutex
	intents         [][]byte
	loseFirst       bool
	denySecondPull  bool
	changeSecondHCL bool
	denyThirdPull   bool
	changeThirdHCL  bool
	beforePull      func(int32)
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func newDeliveryFixture(t *testing.T, mode string) *deliveryFixture {
	t.Helper()
	directory := privateDirectory(t)
	f := &deliveryFixture{cfg: fixturePins()}
	config := fixtureConfiguration()
	if strings.HasPrefix(mode, "replace-") {
		f.cfg.DescriptorRevision, config.DescriptorRevision = "2", "2"
	}
	f.cfg.StateDirectory = privateDirectory(t)
	f.cfg.BearerFile = filepath.Join(directory, "controller.bearer")
	bearer := "rxdctl_" + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	writeFile(t, f.cfg.BearerFile, bearer+"\n", 0600)
	f.paths = deliveryPaths{jwt: filepath.Join(directory, "workload.jwt"), output: privateDirectory(t)}
	writeFile(t, f.paths.jwt, "independent-jwt-fixture", 0600)
	f.launches = filepath.Join(directory, "launches")
	f.cfg.AgentExecutable = filepath.Join(directory, "vault-fixture")
	version := "1.21.4"
	if mode == "wrong-version" {
		version = "1.21.3"
	}
	script := "#!/bin/sh\nif [ \"$1\" = version ]; then printf 'Vault v" + version + " (fixture)\\n'; exit 0; fi\n"
	script += "[ \"$1\" = agent ] && [ \"$3\" = '-exit-after-auth=true' ] || exit 3\nprintf '%s\\n' \"$@\" >> " + shellQuote(f.launches) + "\n"
	script += "[ -z \"${VAULT_TOKEN+x}\" ] || exit 7\nprintf 'secret-output-canary\\n'\nprintf 'secret-error-canary\\n' >&2\n"
	switch mode {
	case "failed":
		script += "exit 4\n"
	case "missing-output":
		script += "exit 0\n"
	case "replace-failed":
		script += "exit 4\n"
	case "replace-success":
		// Translate only the fixed HCL destination's basename into this
		// private fixture root; no test writes to production secret paths.
		script += `config=${2#-config=}
leaf=$(sed -n 's/^  destination = ".*\/\([^"]*\)"$/\1/p' "$config")
[ -n "$leaf" ] || exit 8
umask 077
printf 'replacement-inference-canary' > ` + shellQuote(f.paths.output) + `/"$leaf"
exit 0
`
	default:
		script += "umask 077\nprintf 'fixture-inference-key' > " + shellQuote(filepath.Join(f.paths.output, fixtureSlot)) + "\nexit 0\n"
	}
	writeFile(t, f.cfg.AgentExecutable, script, 0700)
	hash := sha256.Sum256([]byte(script))
	f.cfg.AgentSHA256 = hex.EncodeToString(hash[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+bearer || r.Header.Get("Cookie") != "" || r.Header.Get("X-CSRF-Token") != "" {
			http.Error(w, "unexpected authentication", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		switch r.Method + " " + r.URL.Path {
		case "GET " + machinePath + "/configuration":
			count := f.pulls.Add(1)
			f.mu.Lock()
			deny := f.denySecondPull && count == 2 || f.denyThirdPull && count == 3
			change := f.changeSecondHCL && count == 2 || f.changeThirdHCL && count == 3
			hook := f.beforePull
			f.mu.Unlock()
			if hook != nil {
				hook(count)
			}
			if deny {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("ETag", "\""+fixtureDigest+"\"")
			value := config
			if change {
				value.AgentConfiguration = strings.Replace(value.AgentConfiguration, "vault.example.test", "other-vault.example.test", 1)
			}
			if err := json.NewEncoder(w).Encode(value); err != nil {
				t.Errorf("fixture configuration reply: %v", err)
			}
		case "POST " + machinePath + "/acknowledgements":
			count := f.reports.Add(1)
			raw, err := io.ReadAll(io.LimitReader(r.Body, 4097))
			if err != nil || len(raw) > 4096 {
				t.Error("invalid fixture report body")
				return
			}
			f.mu.Lock()
			f.intents = append(f.intents, bytes.Clone(raw))
			lose := f.loseFirst && count == 1
			f.mu.Unlock()
			if lose {
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Errorf("fixture uncertain reply: %v", err)
					return
				}
				if err := connection.Close(); err != nil {
					t.Errorf("fixture uncertain close: %v", err)
				}
				return
			}
			var intent acknowledgement
			if err := decodeJSON(raw, &intent); err != nil {
				t.Errorf("fixture report encoding: %v", err)
				return
			}
			if err := json.NewEncoder(w).Encode(acknowledgementReceipt{acknowledgement: intent, RecordedAt: time.Now().UTC().Truncate(time.Microsecond), EvidenceKind: "controller_report"}); err != nil {
				t.Errorf("fixture report reply: %v", err)
			}
		default:
			http.Error(w, "unexpected route", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	f.cfg.RouteXURL, f.cfg.AllowLoopbackHTTP = server.URL, true
	return f
}

func TestFiniteDeliveryLocalObservation(t *testing.T) {
	t.Setenv("VAULT_TOKEN", "parent-vault-token-canary")
	f := newDeliveryFixture(t, "success")
	result, err := deliver(context.Background(), f.cfg, f.paths)
	if err != nil || !result.ReportRecorded || !result.AgentJoined || !result.GroupGone || result.AgentState != "unknown" || result.OutputState != "written" {
		t.Fatalf("finite fake-Agent lifecycle: result=%+v error=%v", result, err)
	}
	if f.pulls.Load() != 2 || f.reports.Load() != 1 {
		t.Fatal("unexpected machine request count")
	}
	launch, err := os.ReadFile(f.launches)
	if err != nil || string(launch) != "agent\n-config="+filepath.Join(f.cfg.StateDirectory, "agent.hcl")+"\n-exit-after-auth=true\n" {
		t.Fatal("Agent received unexpected arguments")
	}
	jwt, err := os.ReadFile(f.paths.jwt)
	if err != nil || string(jwt) != "independent-jwt-fixture" {
		t.Fatal("controller changed the independent JWT")
	}
	output, err := os.ReadFile(filepath.Join(f.paths.output, fixtureSlot))
	if err != nil || string(output) != "fixture-inference-key" {
		t.Fatal("fixture output did not remain available")
	}
	for _, path := range []string{"operation.json", "agent.hcl", "controller.lock"} {
		raw, err := os.ReadFile(filepath.Join(f.cfg.StateDirectory, path))
		if path != "operation.json" && !errors.Is(err, os.ErrNotExist) {
			t.Fatal("joined Agent configuration or lock retained")
		}
		for _, canary := range []string{"fixture-inference-key", "parent-vault-token-canary", "secret-output-canary", "secret-error-canary", "rxdctl_", "independent-jwt-fixture"} {
			if bytes.Contains(raw, []byte(canary)) {
				t.Fatal("secret material persisted in controller state")
			}
		}
	}
	if _, err := deliver(context.Background(), f.cfg, f.paths); err == nil {
		t.Fatal("saved operation was automatically replayed")
	}
	if f.pulls.Load() != 2 || f.reports.Load() != 1 {
		t.Fatal("duplicate delivery dispatched requests")
	}
}

func TestUncertainReportRetriesOnlyExactIntent(t *testing.T) {
	f := newDeliveryFixture(t, "success")
	f.mu.Lock()
	f.loseFirst = true
	f.mu.Unlock()
	result, err := deliver(context.Background(), f.cfg, f.paths)
	if err == nil || result.ReportRecorded || !result.AgentJoined || result.OutputState != "written" {
		t.Fatalf("lost report response was treated as success: %+v %v", result, err)
	}
	if f.reports.Load() != 1 {
		t.Fatal("uncertain report was automatically retried")
	}
	firstLaunch, err := os.ReadFile(f.launches)
	if err != nil {
		t.Fatal(err)
	}
	result, err = RetryReport(context.Background(), f.cfg)
	if err != nil || !result.ReportRecorded || result.AgentJoined || result.GroupGone || result.OutputState != "written" {
		t.Fatalf("report-only retry: %+v %v", result, err)
	}
	if f.pulls.Load() != 2 || f.reports.Load() != 2 {
		t.Fatal("retry pulled a new configuration or duplicated POST")
	}
	secondLaunch, err := os.ReadFile(f.launches)
	if err != nil || !bytes.Equal(firstLaunch, secondLaunch) {
		t.Fatal("report retry launched Agent")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.intents) != 2 || !bytes.Equal(f.intents[0], f.intents[1]) {
		t.Fatal("report retry changed UUID, pins or observed states")
	}
	if _, err := os.Stat(filepath.Join(f.paths.output, fixtureSlot)); err != nil {
		t.Fatal("uncertain report removed application output")
	}
}

func TestPreExistingOutputRefusedWithoutDispatch(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "symlink"}[symlink], func(t *testing.T) {
			f := newDeliveryFixture(t, "success")
			path := filepath.Join(f.paths.output, fixtureSlot)
			if symlink {
				if err := os.Symlink("missing-target", path); err != nil {
					t.Fatal(err)
				}
			} else {
				writeFile(t, path, "existing-application-key", 0600)
			}
			if _, err := deliver(context.Background(), f.cfg, f.paths); err == nil {
				t.Fatal("pre-existing output accepted")
			}
			if f.pulls.Load() != 0 || f.reports.Load() != 0 {
				t.Fatal("pre-existing output caused machine dispatch")
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatal("pre-existing output was removed")
			}
			if _, err := os.Stat(f.launches); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("Agent was launched")
			}
		})
	}
}

func TestFiniteDeliveryFailureDoesNotClaimWritten(t *testing.T) {
	for _, mode := range []string{"failed", "missing-output", "wrong-version"} {
		t.Run(mode, func(t *testing.T) {
			f := newDeliveryFixture(t, mode)
			result, err := deliver(context.Background(), f.cfg, f.paths)
			if err == nil || result.OutputState == "written" || result.AgentState == "running" {
				t.Fatalf("failed fixture claimed delivery: %+v %v", result, err)
			}
			if mode == "wrong-version" {
				if f.reports.Load() != 0 || result.ReportRecorded {
					t.Fatal("unsupported executable reported an Agent result")
				}
			} else if !result.ReportRecorded || !result.AgentJoined || !result.GroupGone {
				t.Fatal("failed started Agent was not joined and reported separately")
			}
			if strings.Contains(err.Error(), "secret-") {
				t.Fatal("raw Agent output leaked through diagnostic")
			}
		})
	}
}

func TestReportOnlyPreparedStateRemainsUnknown(t *testing.T) {
	f := newDeliveryFixture(t, "success")
	root, err := privateRoot(f.cfg.StateDirectory)
	if err != nil {
		t.Fatal(err)
	}
	op := newOperation(f.cfg)
	if err := saveOperation(root, op); err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := RetryReport(context.Background(), f.cfg)
	if err != nil || !result.ReportRecorded || result.AgentState != "unknown" || result.OutputState != "unknown" || result.AgentJoined || result.GroupGone {
		t.Fatalf("interrupted operation gained invented evidence: %+v %v", result, err)
	}
	if f.pulls.Load() != 0 || f.reports.Load() != 1 {
		t.Fatal("prepared report retry performed delivery work")
	}
	if _, err := os.Stat(f.launches); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("prepared report retry launched Agent")
	}
	f.cfg.DescriptorRevision = "2"
	if _, err := RetryReport(context.Background(), f.cfg); err == nil {
		t.Fatal("new assignment resolved an old report")
	}
	if f.reports.Load() != 1 {
		t.Fatal("mismatched target dispatched report")
	}
}

func TestFinalLaunchReauthorizationRefusesChangedAuthorityOrHCL(t *testing.T) {
	for _, change := range []bool{false, true} {
		t.Run(map[bool]string{false: "revoked", true: "changed HCL"}[change], func(t *testing.T) {
			f := newDeliveryFixture(t, "success")
			f.mu.Lock()
			f.denySecondPull, f.changeSecondHCL = !change, change
			f.mu.Unlock()
			if _, err := deliver(context.Background(), f.cfg, f.paths); err == nil {
				t.Fatal("launch accepted obsolete authority or altered HCL")
			}
			if f.pulls.Load() != 2 || f.reports.Load() != 0 {
				t.Fatal("rejected launch reported Agent evidence")
			}
			if _, err := os.Stat(f.launches); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("rejected reauthorization launched Agent")
			}
		})
	}
}
