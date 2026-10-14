package deliverycontroller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type processResult struct {
	Started   bool
	Joined    bool
	GroupGone bool
	Succeeded bool
}

const (
	processGrace            = 2 * time.Second
	maxAgentExecutableBytes = 512 << 20
)

var errProcessUnclosed = errors.New("owned Agent process closure is unconfirmed; state remains locked")

func pinnedAgent(ctx context.Context, cfg Config) (os.FileInfo, error) {
	identity, err := verifyAgentExecutable(ctx, cfg, nil)
	if err != nil {
		return nil, err
	}
	version := &boundedWriter{limit: 512}
	result := runProcess(ctx, cfg.AgentExecutable, []string{"version"}, version)
	if result.Started && (!result.Joined || !result.GroupGone) {
		return nil, errProcessUnclosed
	}
	if !result.Succeeded || !result.Joined || !result.GroupGone || version.exceeded || !strings.HasPrefix(strings.TrimSpace(version.String()), "Vault v"+agentVersion+" (") || strings.ContainsAny(strings.TrimSpace(version.String()), "\r\n") {
		return nil, errors.New("unsupported Agent version or process outcome")
	}
	return verifyAgentExecutable(ctx, cfg, identity)
}

func verifyAgentExecutable(ctx context.Context, cfg Config, expected os.FileInfo) (_ os.FileInfo, runErr error) {
	if trustedParents(cfg.AgentExecutable) != nil {
		return nil, errors.New("untrusted Agent executable path")
	}
	root, err := os.OpenRoot(filepath.Dir(cfg.AgentExecutable))
	if err != nil {
		return nil, errors.New("open Agent executable directory failed")
	}
	defer func() {
		if root.Close() != nil && runErr == nil {
			runErr = errors.New("close Agent executable directory failed")
		}
	}()
	info, err := root.Lstat(filepath.Base(cfg.AgentExecutable))
	if err != nil || !info.Mode().IsRegular() || info.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 || info.Mode().Perm()&0022 != 0 || info.Mode().Perm()&0111 == 0 || !owned(info, true) || !singleLink(info) || info.Size() < 1 || info.Size() > maxAgentExecutableBytes {
		return nil, errors.New("agent executable metadata rejected")
	}
	if expected != nil && !os.SameFile(expected, info) {
		return nil, errors.New("pinned Agent executable identity changed")
	}
	file, err := root.OpenFile(filepath.Base(cfg.AgentExecutable), os.O_RDONLY|noFollowFlags(), 0)
	if err != nil {
		return nil, errors.New("open Agent executable failed")
	}
	defer func() {
		if file.Close() != nil && runErr == nil {
			runErr = errors.New("close Agent executable failed")
		}
	}()
	current, err := file.Stat()
	if err != nil || !os.SameFile(info, current) {
		return nil, errors.New("agent executable changed")
	}
	hash := sha256.New()
	buffer := make([]byte, 64<<10)
	var size int64
	for {
		if ctx.Err() != nil {
			return nil, errors.New("agent pin verification interrupted")
		}
		n, readErr := file.Read(buffer)
		size += int64(n)
		if size > maxAgentExecutableBytes {
			return nil, errors.New("agent executable exceeds bound")
		}
		if _, err = hash.Write(buffer[:n]); err != nil {
			return nil, errors.New("agent pin verification failed")
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, errors.New("read Agent executable failed")
		}
	}
	if size != info.Size() || hex.EncodeToString(hash.Sum(nil)) != cfg.AgentSHA256 {
		return nil, errors.New("agent executable does not match pin")
	}
	return info, nil
}

// Wait is directly collected for every successful Start. A terminal parent is
// insufficient: descendants in its owned group must also be absent. No process
// name scan, unrelated PID kill or inherited secret-bearing environment is used.
func runProcess(ctx context.Context, executable string, args []string, stdout io.Writer) processResult {
	result := processResult{GroupGone: true}
	if ctx.Err() != nil {
		return result
	}
	cmd := exec.Command(executable, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Stdout = stdout
	cmd.Stderr = io.Discard
	cmd.WaitDelay = processGrace
	if isolate(cmd) != nil || cmd.Start() != nil {
		return result
	}
	result.Started, result.GroupGone = true, false
	pid := cmd.Process.Pid
	joined := make(chan error, 1)
	go func() { joined <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-joined:
		result.Joined = true
		result.Succeeded = waitErr == nil
	case <-ctx.Done():
		result.Succeeded = false
	}
	// Send TERM even after parent exit, because its descendants can outlive it.
	if signalGroup(pid, false) != nil {
		result.Succeeded = false
	}
	deadline := time.NewTimer(processGrace)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for !result.Joined || groupExists(pid) {
		select {
		case waitErr = <-joined:
			result.Joined = true
			if waitErr != nil {
				result.Succeeded = false
			}
		case <-tick.C:
		case <-deadline.C:
			goto kill
		}
	}
	deadline.Stop()
	result.GroupGone = true
	return result
kill:
	deadline.Stop()
	if signalGroup(pid, true) != nil {
		result.Succeeded = false
	}
	final := time.NewTimer(processGrace)
	defer final.Stop()
	for !result.Joined || groupExists(pid) {
		select {
		case waitErr = <-joined:
			result.Joined = true
			if waitErr != nil {
				result.Succeeded = false
			}
		case <-tick.C:
		case <-final.C:
			result.Succeeded = false
			return result
		}
	}
	result.GroupGone = true
	return result
}

type boundedWriter struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (w *boundedWriter) Write(raw []byte) (int, error) {
	if len(raw) > w.limit-w.Len() {
		w.exceeded = true
		return 0, io.ErrShortWrite
	}
	return w.Buffer.Write(raw)
}
