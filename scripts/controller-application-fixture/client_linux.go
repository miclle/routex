//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const claudePath = "/fixture-client/claude"

type messagesClientConfig struct {
	messagesIdentity
	SlotID string `json:"slot_id"`
}

func launchClaude(out io.Writer) (code int) {
	r := claudeResult{Result: "client_binding_invalid", Native: "unknown"}
	defer func() {
		if json.NewEncoder(out).Encode(r) != nil {
			code = 1
		}
	}()
	if !validClaudeBinding() {
		return 1
	}
	r.SourceCommit, r.RunID, r.Phase, r.ClientVersion = claudeSourceCommit, claudeRunID, claudePhase, claudeVersion
	var config messagesClientConfig
	if readMessagesConfig(inputRoot+"/client-messages.json", &config, "source_commit", "run_id", "phase", "slot_id") != nil || !matchesMessagesIdentity(config.messagesIdentity) || !slotPattern.MatchString(config.SlotID) {
		r.Result = "client_config_invalid"
		return 1
	}
	// Hash the held executable before reading any installed credential. A pathname
	// replacement cannot change the bytes executed through inherited descriptor 3.
	binary, err := openClaudeArtifact(claudePath, claudeSHA256, claudeSize)
	if err != nil {
		r.Result = "client_artifact_invalid"
		return 1
	}
	defer func() { _ = binary.Close() }()
	if verifyRunAlias() != nil {
		r.Result = "key_invalid"
		return 1
	}
	key, err := privateRead(installedRoot+"/"+config.SlotID, 4096, true)
	if err != nil || !validKey(key) {
		r.Result = "key_invalid"
		return 1
	}
	defer clear(key)
	work, err := os.MkdirTemp("/tmp", "routex-claude-")
	if err != nil {
		r.Result = "client_workspace_invalid"
		return 1
	}
	// The private workspace is preserved on unknown child/group closure. The
	// surrounding owned receiver cleanup must read back its physical disposition.
	removable := true
	defer func() {
		if removable {
			_ = os.RemoveAll(work)
		}
	}()
	for _, sub := range []string{"home", "tmp", "workspace"} {
		if os.Mkdir(filepath.Join(work, sub), 0700) != nil {
			r.Result = "client_workspace_invalid"
			return 1
		}
	}
	stdout, stderr := &claudeOutput{}, &claudeOutput{}
	defer stdout.erase()
	defer stderr.erase()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	cmd := exec.Command("/proc/self/fd/3", claudeArgs()...)
	cmd.Args[0] = claudePath
	cmd.ExtraFiles = []*os.File{binary}
	cmd.Dir = filepath.Join(work, "workspace")
	cmd.Env = claudeEnv(filepath.Join(work, "home"), filepath.Join(work, "tmp"), key)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = 2 * time.Second
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if cmd.Start() != nil {
		r.Result = "client_start_failed"
		return 1
	}
	removable = false
	r.OriginalPID = cmd.Process.Pid
	r.OriginalPGID = cmd.Process.Pid
	// WNOWAIT deliberately retains the original child's PID until all signals
	// finish. A concurrent Cmd.Wait would reap it and permit PID/PGID reuse.
	deadline := time.Now().Add(45 * time.Second)
	observed := false
	failed := false
	for time.Now().Before(deadline) {
		exited, known := claudeExited(cmd.Process.Pid)
		if !known {
			r.Result = "client_ownership_unknown"
			return 1
		}
		if exited {
			observed = true
			break
		}
		if ctx.Err() != nil || stdout.exceeded() || stderr.exceeded() {
			failed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !observed {
		failed = true
	}
	// Even normal leader exit may leave descendants holding output descriptors.
	// While the leader is unreaped its group remains positively owned; terminate
	// only that group, then collect the original command once.
	if !signalClaudeGroup(cmd.Process.Pid, syscall.SIGTERM) {
		r.Result = "client_ownership_unknown"
		return 1
	}
	if !observed {
		observed = waitClaudeExit(cmd.Process.Pid, 2*time.Second)
	}
	if !observed {
		if !signalClaudeGroup(cmd.Process.Pid, syscall.SIGKILL) {
			r.Result = "client_ownership_unknown"
			return 1
		}
		observed = waitClaudeExit(cmd.Process.Pid, 2*time.Second)
	}
	if !observed {
		r.Result = "client_join_unknown"
		return 1
	}
	waitErr := cmd.Wait()
	r.OriginalJoined = true
	if cmd.ProcessState == nil {
		r.Result = "client_join_unknown"
		return 1
	}
	exit := cmd.ProcessState.ExitCode()
	r.OriginalExit = &exit
	r.GroupClosed = waitClaudeGroupGone(cmd.Process.Pid, 2*time.Second)
	removable = r.GroupClosed
	r.OutputMatched = stdout.matchesAnswer() && stderr.empty()
	r.Result = "client_failed"
	if !failed && waitErr == nil && ctx.Err() == nil && exit == 0 && r.GroupClosed && r.OutputMatched {
		r.Result = "client_completed"
		return 0
	}
	return 1
}
func claudeExited(pid int) (bool, bool) {
	var info unix.Siginfo
	if pid <= 0 {
		return false, false
	}
	for {
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOHANG|unix.WNOWAIT, nil)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return false, false
		}
		return info.Signo != 0, true
	}
}
func waitClaudeExit(pid int, bound time.Duration) bool {
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		exited, known := claudeExited(pid)
		if !known {
			return false
		}
		if exited {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
func signalClaudeGroup(pid int, sig syscall.Signal) bool {
	pgid, err := syscall.Getpgid(pid)
	// Only called while the original child is held unreaped by waitid(WNOWAIT).
	return pid > 0 && err == nil && pgid == pid && syscall.Kill(-pid, sig) == nil
}
func waitClaudeGroupGone(pid int, bound time.Duration) bool {
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		err := syscall.Kill(-pid, 0)
		if err == syscall.ESRCH {
			return true
		}
		if err != nil {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func openClaudeArtifact(path, digest, sizeText string) (*os.File, error) {
	size, err := strconv.ParseInt(sizeText, 10, 64)
	if err != nil || size <= 0 || size > 512<<20 || !hashPattern.MatchString(digest) || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errInvalid
	}
	root, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, errInvalid
	}
	defer func() { _ = syscall.Close(root) }()
	fd, err := syscall.Dup(root)
	if err != nil {
		return nil, errInvalid
	}
	defer func() { _ = syscall.Close(fd) }()
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for _, part := range parts[:len(parts)-1] {
		if part == "" || part == "." || part == ".." {
			return nil, errInvalid
		}
		next, e := syscall.Openat(fd, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
		if e != nil {
			return nil, errInvalid
		}
		var st syscall.Stat_t
		if syscall.Fstat(next, &st) != nil || st.Mode&syscall.S_IFMT != syscall.S_IFDIR || st.Mode&0022 != 0 || (st.Uid != 0 && st.Uid != uint32(os.Geteuid())) {
			_ = syscall.Close(next)
			return nil, errInvalid
		}
		_ = syscall.Close(fd)
		fd = next
	}
	leaf, err := syscall.Openat(fd, parts[len(parts)-1], syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errInvalid
	}
	f := os.NewFile(uintptr(leaf), "verified-client")
	accepted := false
	defer func() {
		if !accepted {
			_ = f.Close()
		}
	}()
	var before, after syscall.Stat_t
	if syscall.Fstat(leaf, &before) != nil || before.Mode&syscall.S_IFMT != syscall.S_IFREG || (before.Mode&07777 != 0555 && before.Mode&07777 != 0755) || before.Nlink != 1 || before.Size != size || (before.Uid != 0 && before.Uid != uint32(os.Geteuid())) {
		return nil, errInvalid
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(f, size+1))
	if err != nil || n != size || hex.EncodeToString(hash.Sum(nil)) != digest || syscall.Fstat(leaf, &after) != nil || !sameClaudeArtifact(before, after) {
		return nil, errInvalid
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return nil, errInvalid
	}
	accepted = true
	return f, nil
}
func sameClaudeArtifact(a, b syscall.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Uid == b.Uid && a.Gid == b.Gid && a.Mode == b.Mode && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}
