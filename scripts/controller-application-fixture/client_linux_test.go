//go:build linux

package main

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestClaudeOwnedUnreapedGroupTermination(t *testing.T) {
	if os.Getenv("ROUTEX_CLAUDE_GROUP_CONTROL") == "1" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestClaudeOwnedUnreapedGroupTermination$")
	command.Env = []string{"ROUTEX_CLAUDE_GROUP_CONTROL=1"}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal("control start failed")
	}
	joined := false
	defer func() {
		if !joined {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	pid := command.Process.Pid
	if exited, known := claudeExited(pid); !known || exited {
		t.Fatal("original child not observed")
	}
	if !signalClaudeGroup(pid, syscall.SIGTERM) || !waitClaudeExit(pid, 2*time.Second) {
		t.Fatal("owned group did not terminate")
	}
	_ = command.Wait()
	joined = true
	if command.ProcessState.ExitCode() != -1 || !waitClaudeGroupGone(pid, time.Second) {
		t.Fatal("original terminal/closure missing")
	}
	if signalClaudeGroup(pid, syscall.SIGTERM) {
		t.Fatal("reaped identity authorized a signal")
	}
}
func TestClaudeHeldMetadataRejectsExecutableMutation(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "artifact-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if file.Chmod(0555) != nil {
		t.Fatal("mode setup")
	}
	var before, after syscall.Stat_t
	if syscall.Fstat(int(file.Fd()), &before) != nil {
		t.Fatal("initial stat")
	}
	if file.Chmod(0755) != nil || syscall.Fstat(int(file.Fd()), &after) != nil {
		t.Fatal("changed stat")
	}
	if sameClaudeArtifact(before, after) {
		t.Fatal("changed executable metadata accepted")
	}
	if !sameClaudeArtifact(after, after) {
		t.Fatal("stable metadata rejected")
	}
}
