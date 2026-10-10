// The integration runner owns disposable database and test process lifetimes.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const grace = 5 * time.Second
const replayLimit = 1024 * 1024
const totalBudget = 7300 * time.Second
const cleanupReserve = 120 * time.Second
const oidcBinaryLimit = 256 * 1024 * 1024

func main() { os.Exit(run()) }

func run() int {
	flags := flag.NewFlagSet("integration-runner", flag.ContinueOnError)
	// Argument errors have no owned resources and must not block on a terminal.
	flags.SetOutput(io.Discard)
	project := flags.String("project", "", "private Compose project")
	private := flags.String("private-dir", "", "private helper and log directory")
	if flags.Parse(os.Args[1:]) != nil || !strings.HasPrefix(*project, "routex-test-") || *private == "" {
		return 1
	}
	return runLifecycle(*project, *private)
}

func runLifecycle(project, private string) int {
	return runLifecycleWithOutputBudget(project, private, 10*time.Second)
}

type lifecycleReport struct {
	Statuses          []status `json:"statuses"`
	Failed            bool     `json:"failed"`
	ExitCode          int      `json:"exit_code"`
	Completed         bool     `json:"completed"`
	TerminalOutput    string   `json:"terminal_output"`
	Diagnostics       []string `json:"diagnostics"`
	RuntimeAcceptance any      `json:"runtime_acceptance"`
}

func runLifecycleWithOutputBudget(project, private string, outputBudget time.Duration) int {
	started := time.Now()
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	// No terminal writes occur before the work deadline and signal handling exist.
	ctx, cancel := context.WithTimeout(context.Background(), totalBudget-cleanupReserve)
	defer cancel()
	cancelled := make(chan int, 1)
	monitorDone := make(chan struct{})
	defer close(monitorDone)
	go func() {
		select {
		case sig := <-signals:
			code := 143
			if sig == os.Interrupt {
				code = 130
			}
			cancelled <- code
			cancel()
		case <-monitorDone:
		}
	}()
	logDir := filepath.Join(private, "logs")
	diagnostics := []string{}
	statuses := []status{}
	failure := false
	allGone := true
	ready := true
	if err := os.Mkdir(logDir, 0700); err != nil {
		diagnostics = append(diagnostics, "could not create private integration logs")
		failure = true
		ready = false
	}
	env := withoutOIDCBinary(os.Environ())
	binaryPath := filepath.Join(private, "oidc-routex")
	binaryOwned := false
	guardDir := ""
	compose := func(name string, args ...string) job {
		return job{name: name, command: "docker", args: append([]string{"compose", "-p", project, "-f", "compose.test.yaml"}, args...), env: env}
	}
	batch := func(jobs ...job) bool {
		results, failed := supervise(ctx, jobs, logDir, grace)
		statuses = append(statuses, results...)
		for _, s := range results {
			if s.Started && (!s.Joined || !s.GroupGone) {
				allGone = false
			}
		}
		failure = failure || failed
		return !failed
	}
	// Cleanup is necessary even after a failed or interrupted Compose up.
	if ready {
		if batch(compose("compose-up", "up", "--detach", "--wait", "--wait-timeout", "180")) &&
			batch(compose("postgres-port", "port", "postgres", "5432"), compose("mysql-port", "port", "mysql", "3306")) {
			postgres, pgOK := readPort(filepath.Join(logDir, "postgres-port.log"))
			mysql, myOK := readPort(filepath.Join(logDir, "mysql-port.log"))
			if !pgOK || !myOK {
				failure = true
				diagnostics = append(diagnostics, "invalid test database port response")
			} else {
				env = databaseEnv(env, postgres, mysql)
				test := func(name string, extra ...string) job {
					args := []string{"test", "-trimpath", "-race", "-count=1", "-timeout", "120m", "-tags", "development", "-v", "-json"}
					return job{name: name, command: "go", args: append(args, extra...), env: env}
				}
				batch(test("nonmatrix", "-skip", "^TestIdentityIntegration$", "./internal/routex/..."))
				if ctx.Err() == nil && allGone {
					// Reserve only this fixed private output; inherited artifact paths
					// never authorize reading or replacing a caller-supplied binary.
					absolute, err := filepath.Abs(binaryPath)
					if err == nil {
						binaryPath = absolute
						var file *os.File
						file, err = os.OpenFile(binaryPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
						if err == nil {
							binaryOwned = true
							err = file.Close()
						}
					}
					if err != nil {
						failure = true
						diagnostics = append(diagnostics, "could not reserve private OIDC binary")
					} else if batch(job{name: "oidc-build", command: "go", args: []string{"build", "-trimpath", "-tags", "development", "-o", binaryPath, "./cmd/routex"}, env: env}) {
						digest, ok := oidcBinaryDigest(binaryPath)
						if !ok {
							failure = true
							diagnostics = append(diagnostics, "invalid private OIDC binary")
						} else if ctx.Err() == nil && allGone {
							guardDir = filepath.Join(filepath.Dir(binaryPath), "oidc-owners")
							if err := os.Mkdir(guardDir, 0700); err != nil {
								failure = true
								allGone = false
								diagnostics = append(diagnostics, "could not reserve private OIDC ownership guards")
							} else {
								postgresJob := test("postgres", "-run", "^TestIdentityIntegration$/^postgres$", "./internal/routex/handler")
								mysqlJob := test("mysql", "-run", "^TestIdentityIntegration$/^mysql$", "./internal/routex/handler")
								driverEnv := append(append([]string(nil), env...), "ROUTEX_TEST_OIDC_BINARY="+binaryPath, "ROUTEX_TEST_OIDC_BINARY_SHA256="+digest, "ROUTEX_TEST_OIDC_GUARD_DIR="+guardDir)
								postgresJob.env, mysqlJob.env = driverEnv, driverEnv
								batch(postgresJob, mysqlJob)
							}
						}
					}
				}
			}
		}
		if guardDir != "" && !oidcGuardsClear(guardDir) {
			allGone = false
			failure = true
			diagnostics = append(diagnostics, "OIDC child ownership remains unknown; dependencies and executables retained")
		}
		// Diagnostics remain private until every owned group and database cleanup
		// has finished. A blocked terminal cannot prevent these cleanup operations.
		if allGone && (failure || ctx.Err() != nil) {
			diagnosticCtx, diagnosticCancel := context.WithTimeout(context.Background(), 20*time.Second)
			results, failed := supervise(diagnosticCtx, []job{compose("compose-logs", "logs", "--no-color")}, logDir, grace)
			diagnosticCancel()
			statuses = append(statuses, results...)
			failure = failure || failed
			for _, s := range results {
				if s.Started && (!s.Joined || !s.GroupGone) {
					allGone = false
				}
			}
		}
		if allGone {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 45*time.Second)
			results, failed := supervise(cleanupCtx, []job{compose("compose-down", "down", "--timeout", "10")}, logDir, grace)
			cleanupCancel()
			statuses = append(statuses, results...)
			failure = failure || failed
			for _, s := range results {
				if s.Started && (!s.Joined || !s.GroupGone) {
					allGone = false
				}
			}
			if failed {
				diagnostics = append(diagnostics, "could not clean up private integration project")
			}
		} else {
			failure = true
			diagnostics = append(diagnostics, "database cleanup blocked by an unjoined owned process group")
		}
	}
	if allGone && guardDir != "" {
		if err := os.Remove(guardDir); err != nil {
			allGone = false
			failure = true
			diagnostics = append(diagnostics, "could not remove empty OIDC ownership directory")
		}
	}
	if allGone {
		if binaryOwned {
			if err := os.Remove(binaryPath); err != nil && !os.IsNotExist(err) {
				failure = true
				diagnostics = append(diagnostics, "could not remove private OIDC binary")
			}
		}
		if err := os.Remove(filepath.Join(private, "runner")); err != nil {
			failure = true
			diagnostics = append(diagnostics, "could not remove private helper")
		}
	} else {
		failure = true
		diagnostics = append(diagnostics, "private executables retained for an unjoined owned process group")
	}
	report := lifecycleReport{Statuses: statuses, Failed: failure, ExitCode: -1, TerminalOutput: "pending", Diagnostics: diagnostics}
	// A pending receipt is explicitly incomplete; it is stored before any terminal
	// I/O so readers cannot mistake an interrupted replay for a complete lifecycle.
	if !persistReport(logDir, &report) {
		failure = true
	}
	diagnostics = report.Diagnostics
	outputDone := make(chan bool, 1)
	outputDiagnostics := append([]string(nil), diagnostics...)
	go func(failed bool) { outputDone <- emitLogs(logDir, statuses, outputDiagnostics, failed) }(failure)
	deadline := started.Add(totalBudget)
	if limit := time.Now().Add(outputBudget); limit.Before(deadline) {
		deadline = limit
	}
	outputTimer := time.NewTimer(time.Until(deadline))
	select {
	case outputFailed := <-outputDone:
		failure = failure || outputFailed
		report.TerminalOutput = "completed"
		if outputFailed {
			report.TerminalOutput = "failed"
			diagnostics = append(diagnostics, "could not replay integration output")
		}
	case <-ctx.Done():
		failure = true
		report.TerminalOutput = "cancelled"
	case <-outputTimer.C:
		failure = true
		report.TerminalOutput = "timed_out"
		diagnostics = append(diagnostics, "integration terminal output deadline exceeded")
	}
	outputTimer.Stop()
	code := 0
	if failure || ctx.Err() != nil {
		code = 1
		failure = true
	}
	select {
	case cancelledCode := <-cancelled:
		code = cancelledCode
		failure = true
		report.TerminalOutput = "cancelled"
	default:
	}
	report.Failed = failure
	report.ExitCode = code
	report.Completed = true
	report.Diagnostics = diagnostics
	persistReport(logDir, &report)
	return report.ExitCode
}

// All diagnostics stay in owned regular private files. Neither failure path
// falls back to a terminal write, including failure to persist the status file.
func persistReport(logDir string, report *lifecycleReport) bool {
	good := true
	fail := func(message string) {
		good = false
		report.Failed = true
		if report.Completed && report.ExitCode == 0 {
			report.ExitCode = 1
		}
		report.Diagnostics = append(report.Diagnostics, message)
	}
	diagnosticsPath := filepath.Join(logDir, "diagnostics.log")
	bytes := func() []byte { return []byte(strings.Join(report.Diagnostics, "\n") + "\n") }
	if err := os.WriteFile(diagnosticsPath, bytes(), 0600); err != nil {
		fail("could not write private diagnostics")
	}
	if err := writeReport(filepath.Join(logDir, "status.json"), *report); err != nil {
		fail("could not write integration status report")
		// Best-effort separate diagnostic receipt: a failed status write must not
		// delay cleanup or cause an unbounded fallback to stdout/stderr.
		if err := os.WriteFile(diagnosticsPath, bytes(), 0600); err != nil {
			good = false
		}
	}
	return good
}

func writeReport(path string, report lifecycleReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

func readPort(path string) (string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 128 {
		return "", false
	}
	address := strings.TrimSpace(string(raw))
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" || port == "" {
		return "", false
	}
	numeric, err := strconv.Atoi(port)
	if err != nil || numeric < 1 || numeric > 65535 {
		return "", false
	}
	return address, true
}

// Only the runner's fresh private build may supply these values to matrix jobs.
func withoutOIDCBinary(env []string) []string {
	result := make([]string, 0, len(env))
	for _, value := range env {
		if !strings.HasPrefix(value, "ROUTEX_TEST_OIDC_BINARY=") && !strings.HasPrefix(value, "ROUTEX_TEST_OIDC_BINARY_SHA256=") && !strings.HasPrefix(value, "ROUTEX_TEST_OIDC_GUARD_DIR=") {
			result = append(result, value)
		}
	}
	return result
}

// Any remaining descriptor or unreadable/missing directory denies dependency
// cleanup, even after the owning Go worker itself has joined and disappeared.
func oidcGuardsClear(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
		return false
	}
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) == 0
}

// Refuse symlinks, non-executables, empty or oversized outputs. Both stat and
// streaming bounds apply, including a file that grows after its initial stat.
func oidcBinaryDigest(path string) (string, bool) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0111 == 0 || before.Size() <= 0 || before.Size() > oidcBinaryLimit {
		return "", false
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	opened, statErr := file.Stat()
	if statErr != nil || !os.SameFile(before, opened) {
		_ = file.Close()
		return "", false
	}
	digest := sha256.New()
	size, readErr := io.Copy(digest, io.LimitReader(file, oidcBinaryLimit+1))
	after, afterErr := file.Stat()
	closeErr := file.Close()
	current, currentErr := os.Lstat(path)
	if readErr != nil || afterErr != nil || closeErr != nil || currentErr != nil || size != before.Size() || size > oidcBinaryLimit || !os.SameFile(before, current) || after.Size() != before.Size() || current.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || !current.ModTime().Equal(before.ModTime()) {
		return "", false
	}
	return hex.EncodeToString(digest.Sum(nil)), true
}

func databaseEnv(env []string, postgres, mysql string) []string {
	result := make([]string, 0, len(env)+2)
	for _, value := range env {
		if !strings.HasPrefix(value, "ROUTEX_TEST_POSTGRES_DSN=") && !strings.HasPrefix(value, "ROUTEX_TEST_MYSQL_DSN=") {
			result = append(result, value)
		}
	}
	_, port, _ := net.SplitHostPort(postgres)
	return append(result, "ROUTEX_TEST_POSTGRES_DSN=host=127.0.0.1 port="+port+" user=routex password=routex-test dbname=routex_test sslmode=disable", "ROUTEX_TEST_MYSQL_DSN=routex:routex-test@tcp("+mysql+")/routex_test?charset=utf8mb4&parseTime=True&loc=UTC")
}

func emitLogs(logDir string, statuses []status, diagnostics []string, failed bool) bool {
	outputFailed := false
	if _, err := fmt.Fprintf(os.Stderr, "Integration raw logs: %s\n", logDir); err != nil {
		outputFailed = true
	}
	for _, message := range diagnostics {
		if _, err := fmt.Fprintln(os.Stderr, message); err != nil {
			outputFailed = true
		}
	}
	for _, s := range statuses {
		if _, err := fmt.Fprintf(os.Stderr, "%s: started=%t exit=%d joined=%t group_gone=%t output_ok=%t\n", s.Name, s.Started, s.ExitCode, s.Joined, s.GroupGone, s.OutputOK); err != nil {
			outputFailed = true
		}
		if strings.HasPrefix(s.Name, "compose-") && !failed {
			continue
		}
		if err := replay(os.Stdout, filepath.Join(logDir, s.Name+".log"), replayLimit); err != nil {
			outputFailed = true
		}
	}
	return outputFailed
}
