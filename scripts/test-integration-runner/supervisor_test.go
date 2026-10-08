//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func fakeJob(name, mode, receipt string) job {
	env := append(os.Environ(), "ROUTEX_RUNNER_HELPER="+mode, "ROUTEX_RUNNER_RECEIPT="+receipt, "GORACE=atexit_sleep_ms=0")
	return job{name: name, command: os.Args[0], args: []string{"-test.run=^TestWorkerHelper$"}, env: env}
}

func TestWorkerHelper(t *testing.T) {
	mode := os.Getenv("ROUTEX_RUNNER_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "ok":
		os.Stdout.WriteString("exact worker output\n")
		os.Exit(0)
	case "barrier":
		if err := os.WriteFile(os.Getenv("ROUTEX_RUNNER_RECEIPT"), []byte("ready"), 0600); err != nil {
			os.Exit(12)
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(os.Getenv("ROUTEX_RUNNER_PEER")); err == nil {
				os.Exit(0)
			}
			time.Sleep(5 * time.Millisecond)
		}
		os.Exit(13)
	case "fail":
		os.Exit(7)
	case "ignore":
		signal.Ignore(syscall.SIGTERM, os.Interrupt)
		if err := os.WriteFile(os.Getenv("ROUTEX_RUNNER_RECEIPT"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			os.Exit(8)
		}
		for {
			time.Sleep(time.Hour)
		}
	case "parent":
		child := exec.Command(os.Args[0], "-test.run=^TestWorkerHelper$")
		child.Env = append(os.Environ(), "ROUTEX_RUNNER_HELPER=ignore")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if child.Start() != nil {
			os.Exit(9)
		}
		signal.Ignore(syscall.SIGTERM, os.Interrupt)
		// The parent remains alive through TERM and reaps its inherited grandchild
		// when both receive KILL; macOS/Linux init then reap the parent as required.
		child.Wait()
		os.Exit(0)
	}
	os.Exit(10)
}

func TestIndependentWorkerResults(t *testing.T) {
	dir := t.TempDir()
	results, failed := supervise(context.Background(), []job{fakeJob("failed", "fail", ""), fakeJob("passed", "ok", "")}, dir, 200*time.Millisecond)
	if !failed || len(results) != 2 || results[0].ExitCode != 7 || results[1].ExitCode != 0 {
		t.Fatalf("results=%+v failed=%t", results, failed)
	}
	for _, result := range results {
		if !result.Joined || !result.GroupGone || !result.OutputOK {
			t.Fatalf("incomplete result: %+v", result)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "passed.log"))
	if err != nil || string(raw) != "exact worker output\n" {
		t.Fatalf("raw output %q error %v", raw, err)
	}
	info, err := os.Stat(filepath.Join(dir, "passed.log"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("worker logs must be private")
	}
}

func TestCancellationCleansIgnoredGrandchildAndPreservesUnrelatedProcess(t *testing.T) {
	dir := t.TempDir()
	unrelated := exec.Command(os.Args[0], "-test.run=^TestWorkerHelper$")
	unrelated.Env = fakeJob("unrelated", "ignore", filepath.Join(dir, "unrelated.pid")).env
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { unrelated.Process.Kill(); unrelated.Wait() }()
	waitReceipt(t, filepath.Join(dir, "unrelated.pid"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	receipt := filepath.Join(dir, "grandchild.pid")
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}
			if _, err := os.Stat(receipt); err == nil {
				cancel()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	start := time.Now()
	results, failed := supervise(ctx, []job{fakeJob("parent", "parent", receipt)}, dir, 2*time.Second)
	if !failed || len(results) != 1 || !results[0].Joined || !results[0].GroupGone {
		t.Fatalf("results=%+v failed=%t", results, failed)
	}
	if time.Since(start) > 6*time.Second {
		t.Fatal("cancellation exceeded cleanup bound")
	}
	if err := unrelated.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("unrelated process was stopped: %v", err)
	}
	pidRaw, err := os.ReadFile(receipt)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(pidRaw))
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		t.Fatal("grandchild survived cancellation")
	}
}

func waitReceipt(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("worker did not become ready")
}

func TestDeadlineAndLogCreationFailure(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		results, failed := supervise(ctx, []job{fakeJob("deadline", "ignore", filepath.Join(t.TempDir(), "pid"))}, t.TempDir(), 200*time.Millisecond)
		if !failed || !results[0].Joined || !results[0].GroupGone {
			t.Fatalf("results=%+v failed=%t", results, failed)
		}
	})
	t.Run("log failure does not hide another result", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "blocked.log"), 0700); err != nil {
			t.Fatal(err)
		}
		results, failed := supervise(context.Background(), []job{fakeJob("blocked", "ok", ""), fakeJob("other", "ok", "")}, dir, 200*time.Millisecond)
		if !failed || results[0].Started || results[0].OutputOK || results[1].ExitCode != 0 {
			t.Fatalf("results=%+v failed=%t", results, failed)
		}
	})
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestVerbatimBoundedReplayAndOutputErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raw.log")
	if err := os.WriteFile(path, []byte("first\nsecond\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := replay(&output, path, 6); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(output.String(), "first\n") || !strings.Contains(output.String(), "truncated") || strings.Contains(output.String(), "second") {
		t.Fatal(output.String())
	}
	if !errors.Is(replay(failedWriter{}, path, 6), io.ErrClosedPipe) {
		t.Fatal("output error was hidden")
	}
}

func TestDatabaseEnvironmentAndPortValidation(t *testing.T) {
	env := databaseEnv([]string{"ROUTEX_TEST_POSTGRES_DSN=old secret", "ROUTEX_TEST_MYSQL_DSN=old secret", "OTHER=value"}, "127.0.0.1:10001", "127.0.0.1:10002")
	if len(env) != 3 || env[0] != "OTHER=value" {
		t.Fatal("caller DSNs must be replaced without losing unrelated environment")
	}
	for _, driver := range []string{"POSTGRES", "MYSQL"} {
		count := 0
		for _, value := range env {
			if strings.HasPrefix(value, "ROUTEX_TEST_"+driver+"_DSN=") {
				count++
				if strings.Contains(value, "old secret") {
					t.Fatal("caller DSN retained")
				}
			}
		}
		if count != 1 {
			t.Fatal("both generated DSNs must be present once")
		}
	}
	for _, value := range []string{"127.0.0.1:10001\n", "0.0.0.0:10001\n", "127.0.0.1:70000", "127.0.0.1:http", "127.0.0.1:0", "malformed"} {
		path := filepath.Join(t.TempDir(), "port")
		os.WriteFile(path, []byte(value), 0600)
		_, ok := readPort(path)
		if ok != (value == "127.0.0.1:10001\n") {
			t.Fatalf("unexpected port validation for %q", value)
		}
	}
}

func TestLifecycleHelper(t *testing.T) {
	if os.Getenv("ROUTEX_RUNNER_LIFECYCLE") != "1" {
		return
	}
	budget := 10 * time.Second
	if value := os.Getenv("ROUTEX_RUNNER_FAKE_OUTPUT_BUDGET"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			os.Exit(20)
		}
		budget = parsed
	}
	os.Exit(runLifecycleWithOutputBudget("routex-test-fake", os.Getenv("ROUTEX_RUNNER_PRIVATE"), budget))
}

func fakeLifecycle(t *testing.T, fail string) (*exec.Cmd, string, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(dir, "private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "runner"), []byte("private helper fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(dir, "trace")
	docker := `#!/bin/sh
printf '%s\n' "$$" >> "$ROUTEX_RUNNER_PRIVATE/owned-pids"
case "$6" in
 up) printf 'up\n' >> "$ROUTEX_RUNNER_TRACE"
 printf 'owned' > "$ROUTEX_RUNNER_PRIVATE/resource"
 case "$ROUTEX_RUNNER_FAIL" in
  report) mkdir "$ROUTEX_RUNNER_PRIVATE/logs/status.json";;
  remove) rm "$ROUTEX_RUNNER_PRIVATE/runner"; mkdir "$ROUTEX_RUNNER_PRIVATE/runner"; printf 'keep' > "$ROUTEX_RUNNER_PRIVATE/runner/leaf";;
 esac;;
 port) if [ "$ROUTEX_RUNNER_FAIL" = invalid-port ]; then printf 'invalid\n';exit 0;fi
 if [ "$7" = postgres ]; then printf '127.0.0.1:10001\n'; else printf '127.0.0.1:10002\n'; fi;;
 logs) printf 'fake database diagnostics\n';;
 down) rm "$ROUTEX_RUNNER_PRIVATE/resource"; printf 'down\n' >> "$ROUTEX_RUNNER_TRACE"; if [ "$ROUTEX_RUNNER_FAIL" = cleanup ]; then exit 8; fi;;
 *) exit 11;;
esac
`
	goScript := `#!/bin/sh
printf '%s\n' "$$" >> "$ROUTEX_RUNNER_PRIVATE/owned-pids"
[ "$ROUTEX_TEST_POSTGRES_DSN" = 'host=127.0.0.1 port=10001 user=routex password=routex-test dbname=routex_test sslmode=disable' ] || exit 14
[ "$ROUTEX_TEST_MYSQL_DSN" = 'routex:routex-test@tcp(127.0.0.1:10002)/routex_test?charset=utf8mb4&parseTime=True&loc=UTC' ] || exit 15
printf '%s\n' "$*" >> "$ROUTEX_RUNNER_TRACE"
case "$*" in
 *-skip*) if [ "$ROUTEX_RUNNER_FAIL" = nonmatrix ]; then exit 6; fi;;
 *postgres*) if [ "$ROUTEX_RUNNER_FAIL" = postgres ]; then exit 7; fi;;
esac
if [ "$ROUTEX_RUNNER_FAIL" = cancel ]; then
 printf 'ready\n' >> "$ROUTEX_RUNNER_TRACE"
 exec sleep 1000
fi
printf 'fake test result\n'
`
	for name, content := range map[string]string{"docker": docker, "go": goScript} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(content), 0700); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestLifecycleHelper$")
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "ROUTEX_RUNNER_LIFECYCLE=1", "ROUTEX_RUNNER_PRIVATE="+private, "ROUTEX_RUNNER_TRACE="+trace, "ROUTEX_RUNNER_FAIL="+fail, "GORACE=atexit_sleep_ms=0")
	return cmd, trace, private
}

func TestCompleteCommandPlanAndFailureAggregation(t *testing.T) {
	for _, failure := range []string{"", "nonmatrix", "postgres", "cleanup"} {
		t.Run(failure, func(t *testing.T) {
			cmd, trace, private := fakeLifecycle(t, failure)
			output, err := cmd.CombinedOutput()
			if (err == nil) != (failure == "") {
				t.Fatalf("unexpected exit %v output %s", err, output)
			}
			raw, err := os.ReadFile(trace)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
			if len(lines) != 5 || lines[0] != "up" || lines[4] != "down" {
				t.Fatalf("incomplete lifecycle %q", lines)
			}
			common := "test -trimpath -race -count=1 -timeout 120m -tags development -v -json "
			if lines[1] != common+"-skip ^TestIdentityIntegration$ ./internal/routex/..." {
				t.Fatal(lines[1])
			}
			remaining := strings.Join(lines[2:4], "\n")
			for _, driver := range []string{"postgres", "mysql"} {
				if !strings.Contains(remaining, common+"-run ^TestIdentityIntegration$/^"+driver+"$ ./internal/routex/handler") {
					t.Fatalf("missing complete driver: %s", remaining)
				}
			}
			if _, err := os.Stat(filepath.Join(private, "runner")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("compiled helper not removed")
			}
			var report struct {
				ExitCode          int  `json:"exit_code"`
				Failed            bool `json:"failed"`
				RuntimeAcceptance any  `json:"runtime_acceptance"`
			}
			data, err := os.ReadFile(filepath.Join(private, "logs", "status.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(data, &report); err != nil {
				t.Fatal(err)
			}
			if report.Failed != (failure != "") || report.RuntimeAcceptance != nil {
				t.Fatalf("untruthful report: %s", data)
			}
		})
	}
}

func TestLifecycleCancellationExitAndCleanup(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			cmd, trace, private := fakeLifecycle(t, "cancel")
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			ready := false
			for time.Now().Before(deadline) {
				raw, _ := os.ReadFile(trace)
				if strings.Contains(string(raw), "ready") {
					ready = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !ready {
				cmd.Process.Kill()
				cmd.Wait()
				t.Fatal("fake lifecycle did not become ready")
			}
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			err := cmd.Wait()
			var exitError *exec.ExitError
			expected := 143
			if sig == syscall.SIGINT {
				expected = 130
			}
			if !errors.As(err, &exitError) || exitError.ExitCode() != expected {
				t.Fatalf("exit=%v output=%s", err, output.String())
			}
			raw, err := os.ReadFile(trace)
			if err != nil || !strings.HasSuffix(string(raw), "down\n") {
				t.Fatalf("cleanup omitted %q %v", raw, err)
			}
			var report struct {
				ExitCode int  `json:"exit_code"`
				Failed   bool `json:"failed"`
			}
			data, err := os.ReadFile(filepath.Join(private, "logs", "status.json"))
			if err != nil {
				t.Fatal(err)
			}
			json.Unmarshal(data, &report)
			if report.ExitCode != expected || !report.Failed {
				t.Fatalf("untruthful cancellation report %s", data)
			}
		})
	}
}

func (failedWriter) Close() error { return nil }

func TestWorkerLogWriteErrorFailsSuccessfulExit(t *testing.T) {
	results, failed := superviseWithLogs(context.Background(), []job{fakeJob("output-error", "ok", "")}, t.TempDir(), 200*time.Millisecond, func(string) (io.WriteCloser, error) { return failedWriter{}, nil })
	if !failed || !results[0].Joined || !results[0].GroupGone || results[0].OutputOK {
		t.Fatalf("results=%+v failed=%t", results, failed)
	}
}

func TestConcurrentWorkersStartTogether(t *testing.T) {
	dir := t.TempDir()
	firstReceipt := filepath.Join(dir, "first-ready")
	secondReceipt := filepath.Join(dir, "second-ready")
	first := fakeJob("first", "barrier", firstReceipt)
	first.env = append(first.env, "ROUTEX_RUNNER_PEER="+secondReceipt)
	second := fakeJob("second", "barrier", secondReceipt)
	second.env = append(second.env, "ROUTEX_RUNNER_PEER="+firstReceipt)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	results, failed := supervise(ctx, []job{first, second}, dir, 200*time.Millisecond)
	if failed || len(results) != 2 {
		t.Fatalf("workers did not overlap: %+v", results)
	}
}

// Filling a real inherited pipe exercises the OS write path. The parent never
// drains it, so successful cleanup/exit cannot rely on a responsive consumer.
func filledPipe(t *testing.T) *os.File {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close(); writer.Close() })
	fd := int(writer.Fd())
	if err = syscall.SetNonblock(fd, true); err != nil {
		t.Fatal(err)
	}
	filled := 0
	for {
		n, err := syscall.Write(fd, make([]byte, 4096))
		filled += n
		if errors.Is(err, syscall.EAGAIN) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if filled == 0 {
		t.Fatal("pipe was not filled")
	}
	if err = syscall.SetNonblock(fd, false); err != nil {
		t.Fatal(err)
	}
	return writer
}

func unrelatedWorker(t *testing.T) *exec.Cmd {
	t.Helper()
	receipt := filepath.Join(t.TempDir(), "unrelated.pid")
	cmd := exec.Command(os.Args[0], "-test.run=^TestWorkerHelper$")
	cmd.Env = fakeJob("unrelated", "ignore", receipt).env
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	waitReceipt(t, receipt)
	return cmd
}

func assertFakeCleanup(t *testing.T, trace, private string, unrelated *exec.Cmd) {
	t.Helper()
	raw, err := os.ReadFile(trace)
	if err != nil || !strings.HasSuffix(string(raw), "down\n") {
		t.Fatalf("Compose down omitted: %q %v", raw, err)
	}
	if _, err = os.Stat(filepath.Join(private, "resource")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fake owned resource remains")
	}
	raw, err = os.ReadFile(filepath.Join(private, "owned-pids"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Fields(string(raw)) {
		pid, err := strconv.Atoi(line)
		if err != nil {
			t.Fatal(err)
		}
		if !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) || !errors.Is(syscall.Kill(-pid, 0), syscall.ESRCH) {
			t.Fatalf("owned process/group %d survives", pid)
		}
	}
	if err := unrelated.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("unrelated process stopped: %v", err)
	}
}

func waitFakeExit(t *testing.T, cmd *exec.Cmd, limit time.Duration) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(limit):
		cmd.Process.Kill()
		<-done
		t.Fatal("fake runner exceeded bounded exit")
		return nil
	}
}

func TestBlockingStderrCannotPreventOwnedCleanup(t *testing.T) {
	unrelated := unrelatedWorker(t)
	for _, failure := range []string{"", "invalid-port", "cleanup", "report", "remove"} {
		name := failure
		if name == "" {
			name = "startup filled pipe"
		}
		t.Run(name, func(t *testing.T) {
			cmd, trace, private := fakeLifecycle(t, failure)
			cmd.Env = append(cmd.Env, "ROUTEX_RUNNER_FAKE_OUTPUT_BUDGET=300ms")
			cmd.Stdout = io.Discard
			cmd.Stderr = filledPipe(t)
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			err := waitFakeExit(t, cmd, 4*time.Second)
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
				t.Fatalf("unexpected exit: %v", err)
			}
			assertFakeCleanup(t, trace, private, unrelated)
			diagnostics, err := os.ReadFile(filepath.Join(private, "logs", "diagnostics.log"))
			if err != nil {
				t.Fatal(err)
			}
			if failure == "report" {
				if !strings.Contains(string(diagnostics), "could not write integration status report") {
					t.Fatal("status-write diagnostic missing")
				}
			} else {
				data, err := os.ReadFile(filepath.Join(private, "logs", "status.json"))
				if err != nil {
					t.Fatal(err)
				}
				var report lifecycleReport
				if err = json.Unmarshal(data, &report); err != nil {
					t.Fatal(err)
				}
				if !report.Completed || !report.Failed || report.ExitCode != 1 || report.TerminalOutput != "timed_out" {
					t.Fatalf("untruthful blocked output receipt: %s", data)
				}
				if failure == "remove" && !strings.Contains(string(diagnostics), "could not remove private helper") {
					t.Fatal("helper-remove diagnostic missing")
				}
				if failure == "invalid-port" && !strings.Contains(string(diagnostics), "invalid test database port response") {
					t.Fatal("invalid-port diagnostic missing")
				}
			}
		})
	}
}

func TestCancellationInterruptsBlockedReplayAfterCleanup(t *testing.T) {
	unrelated := unrelatedWorker(t)
	for _, surface := range []string{"stdout", "stderr"} {
		for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
			t.Run(surface+"/"+sig.String(), func(t *testing.T) {
				cmd, trace, private := fakeLifecycle(t, "")
				cmd.Env = append(cmd.Env, "ROUTEX_RUNNER_FAKE_OUTPUT_BUDGET=5s")
				cmd.Stdout = io.Discard
				cmd.Stderr = io.Discard
				if surface == "stdout" {
					cmd.Stdout = filledPipe(t)
				} else {
					cmd.Stderr = filledPipe(t)
				}
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(4 * time.Second)
				pending := false
				for time.Now().Before(deadline) {
					data, err := os.ReadFile(filepath.Join(private, "logs", "status.json"))
					var report lifecycleReport
					if err == nil && json.Unmarshal(data, &report) == nil && report.TerminalOutput == "pending" {
						if report.Completed || report.ExitCode != -1 {
							t.Fatal("pending receipt claims completed lifecycle")
						}
						pending = true
						break
					}
					time.Sleep(5 * time.Millisecond)
				}
				if !pending {
					cmd.Process.Kill()
					cmd.Wait()
					t.Fatal("runner did not reach private pending replay receipt")
				}
				// Both the pending receipt and down exist before the cancellation. The
				// filled sink then blocks an actual replay write while this pause elapses.
				assertFakeCleanup(t, trace, private, unrelated)
				time.Sleep(75 * time.Millisecond)
				if err := cmd.Process.Signal(sig); err != nil {
					t.Fatal(err)
				}
				started := time.Now()
				err := waitFakeExit(t, cmd, time.Second)
				if time.Since(started) > time.Second {
					t.Fatal("blocked replay delayed cancellation")
				}
				expected := 143
				if sig == syscall.SIGINT {
					expected = 130
				}
				var exitError *exec.ExitError
				if !errors.As(err, &exitError) || exitError.ExitCode() != expected {
					t.Fatalf("unexpected signal exit: %v", err)
				}
				assertFakeCleanup(t, trace, private, unrelated)
				data, err := os.ReadFile(filepath.Join(private, "logs", "status.json"))
				if err != nil {
					t.Fatal(err)
				}
				var report lifecycleReport
				if err = json.Unmarshal(data, &report); err != nil {
					t.Fatal(err)
				}
				if !report.Completed || !report.Failed || report.ExitCode != expected || report.TerminalOutput != "cancelled" {
					t.Fatalf("untruthful cancelled replay receipt: %s", data)
				}
			})
		}
	}
}

func TestImmediatePrivateIdentityLedger(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan []status, 1)
	go func() {
		results, _ := supervise(ctx, []job{fakeJob("owned", "ignore", filepath.Join(dir, "ready"))}, dir, 200*time.Millisecond)
		done <- results
	}()
	waitReceipt(t, filepath.Join(dir, "ready"))
	data, err := os.ReadFile(filepath.Join(dir, "owned-processes.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var identity struct {
		Name string `json:"name"`
		PID  int    `json:"pid"`
		PGID int    `json:"pgid"`
	}
	if err = json.Unmarshal(bytes.TrimSpace(data), &identity); err != nil {
		t.Fatal(err)
	}
	if identity.Name != "owned" || identity.PID <= 0 || identity.PGID != identity.PID {
		t.Fatalf("invalid ownership: %s", data)
	}
	if err = syscall.Kill(-identity.PGID, 0); err != nil {
		t.Fatal("ledger was not captured while the worker was alive")
	}
	info, err := os.Stat(filepath.Join(dir, "owned-processes.jsonl"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("ownership ledger is not private")
	}
	cancel()
	results := <-done
	if len(results) != 1 || !results[0].IdentityRecorded || results[0].PID != identity.PID || results[0].PGID != identity.PGID || !results[0].Joined || !results[0].GroupGone {
		t.Fatalf("ownership lost: %+v", results)
	}
	if !errors.Is(syscall.Kill(-identity.PGID, 0), syscall.ESRCH) {
		t.Fatal("recorded owned group remains")
	}
}

func TestLedgerWriteFailureStopsWorkerBeforeCleanup(t *testing.T) {
	dir := t.TempDir()
	unrelated := unrelatedWorker(t)
	failedLedger := filepath.Join(dir, "blocked-ledger")
	if err := os.Mkdir(failedLedger, 0700); err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(dir, "ready")
	record := func(s status) error {
		// Let the worker install its ignored TERM handler before inducing the real
		// filesystem error. The failed ledger must trigger bounded KILL and join.
		waitReceipt(t, receipt)
		return os.WriteFile(failedLedger, []byte("unwritable ledger"), 0600)
	}
	results, failed := superviseWithRecords(context.Background(), []job{fakeJob("owned", "ignore", receipt), fakeJob("not-started", "ok", "")}, dir, 200*time.Millisecond, func(path string) (io.WriteCloser, error) {
		return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	}, record)
	if !failed || len(results) != 2 || results[0].IdentityRecorded || !results[0].Started || !results[0].Joined || !results[0].GroupGone || results[1].Started {
		t.Fatalf("unrecorded worker continued: %+v", results)
	}
	if results[0].PID <= 0 || results[0].PID != results[0].PGID || !errors.Is(syscall.Kill(-results[0].PGID, 0), syscall.ESRCH) {
		t.Fatal("unrecorded owned group not terminated")
	}
	cleanup, failed := supervise(context.Background(), []job{fakeJob("cleanup-after-join", "ok", "")}, dir, 200*time.Millisecond)
	if failed || !cleanup[0].IdentityRecorded || !cleanup[0].Joined || !cleanup[0].GroupGone {
		t.Fatalf("cleanup failed: %+v", cleanup)
	}
	if err := unrelated.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("unrelated process stopped")
	}
}

func TestIdentityLedgerCreationFailureJoinsBeforeRecoveredCleanup(t *testing.T) {
	dir := t.TempDir()
	ledger := filepath.Join(dir, "owned-processes.jsonl")
	if err := os.Mkdir(ledger, 0700); err != nil {
		t.Fatal(err)
	}
	results, failed := supervise(context.Background(), []job{fakeJob("unrecorded", "ignore", filepath.Join(dir, "ready"))}, dir, 200*time.Millisecond)
	if !failed || !results[0].Started || results[0].IdentityRecorded || !results[0].Joined || !results[0].GroupGone || results[0].PID <= 0 || results[0].PGID != results[0].PID {
		t.Fatalf("ledger failure did not join owned worker: %+v", results)
	}
	if !errors.Is(syscall.Kill(-results[0].PGID, 0), syscall.ESRCH) {
		t.Fatal("unrecorded group remains before cleanup")
	}
	if err := os.Remove(ledger); err != nil {
		t.Fatal(err)
	}
	cleanup, failed := supervise(context.Background(), []job{fakeJob("cleanup", "ok", "")}, dir, 200*time.Millisecond)
	if failed || !cleanup[0].IdentityRecorded || !cleanup[0].Joined || !cleanup[0].GroupGone {
		t.Fatalf("recovered cleanup incomplete: %+v", cleanup)
	}
}
