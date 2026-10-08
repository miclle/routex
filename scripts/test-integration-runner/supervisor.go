package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type job struct {
	name    string
	command string
	args    []string
	env     []string
}

type status struct {
	Name             string `json:"name"`
	Started          bool   `json:"started"`
	ExitCode         int    `json:"exit_code"`
	OutputOK         bool   `json:"output_ok"`
	Joined           bool   `json:"joined"`
	GroupGone        bool   `json:"group_gone"`
	PID              int    `json:"pid"`
	PGID             int    `json:"pgid"`
	IdentityRecorded bool   `json:"identity_recorded"`
}

type worker struct {
	command *exec.Cmd
	file    io.WriteCloser
	result  <-chan error
	status  status
}

// The context bounds work; the two separate grace periods reserve finite time
// to join parents and terminate inherited descendants before database cleanup.
func supervise(ctx context.Context, jobs []job, logDir string, grace time.Duration) ([]status, bool) {
	return superviseWithLogs(ctx, jobs, logDir, grace, func(path string) (io.WriteCloser, error) {
		return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	})
}

// The local opener permits deterministic I/O failure tests without filling disk.
func superviseWithLogs(ctx context.Context, jobs []job, logDir string, grace time.Duration, openLog func(string) (io.WriteCloser, error)) ([]status, bool) {
	return superviseWithRecords(ctx, jobs, logDir, grace, openLog, func(s status) error { return recordIdentity(logDir, s) })
}

func superviseWithRecords(ctx context.Context, jobs []job, logDir string, grace time.Duration, openLog func(string) (io.WriteCloser, error), recordStart func(status) error) ([]status, bool) {
	aborted := false
	workers := make([]*worker, 0, len(jobs))
	failed := false
	for _, j := range jobs {
		w := &worker{status: status{Name: j.name, ExitCode: -1, OutputOK: true, GroupGone: true}}
		workers = append(workers, w)
		if aborted || ctx.Err() != nil {
			failed = true
			continue
		}
		file, err := openLog(filepath.Join(logDir, j.name+".log"))
		if err != nil {
			failed = true
			w.status.OutputOK = false
			continue
		}
		w.file = file
		cmd := exec.Command(j.command, j.args...)
		cmd.Env = j.env
		// Non-*os.File writers make exec observe log write failures, rather than
		// allowing a child's failed write to be hidden by a successful exit status.
		cmd.Stdout = struct{ io.Writer }{file}
		cmd.Stderr = cmd.Stdout
		cmd.WaitDelay = grace
		w.command = cmd
		if isolate(cmd) != nil || cmd.Start() != nil {
			failed = true
			continue
		}
		w.status.Started = true
		w.status.PID = cmd.Process.Pid
		w.status.PGID = cmd.Process.Pid
		w.status.GroupGone = false
		// Capture ownership immediately, before waiting or starting another child.
		// An unrecorded child must be stopped and joined rather than kept running.
		if err := recordStart(w.status); err != nil {
			failed = true
			aborted = true
		} else {
			w.status.IdentityRecorded = true
		}
		result := make(chan error, 1)
		w.result = result
		go func() { result <- cmd.Wait() }()
	}
	// Both driver results are collected even if one fails. Cancellation stops all
	// owned groups, while an ordinary failure does not omit the other driver's run.
	type completion struct {
		index int
		err   error
	}
	done := make(chan completion, len(workers))
	for i, w := range workers {
		if w.result == nil {
			continue
		}
		go func() {
			err := <-w.result
			done <- completion{i, err}
		}()
	}
	record := func(c completion) {
		w := workers[c.index]
		w.status.Joined = true
		w.status.ExitCode = w.command.ProcessState.ExitCode()
		var exitError *exec.ExitError
		if c.err != nil && !errors.As(c.err, &exitError) {
			w.status.OutputOK = false
		}
	}
	remaining := 0
	for _, w := range workers {
		if w.result != nil {
			remaining++
		}
	}
	if aborted {
		goto shutdown
	}
	for remaining > 0 {
		select {
		case c := <-done:
			record(c)
			remaining--
		case <-ctx.Done():
			failed = true
			goto shutdown
		}
	}
shutdown:
	// Signal every owned group, including one whose parent has already exited.
	// Descendants retaining log pipes cannot hold Wait indefinitely (WaitDelay).
	for _, w := range workers {
		if w.status.Started && signalGroup(w.command.Process.Pid, false) != nil {
			failed = true
		}
	}
	term := time.NewTimer(grace)
	for remaining > 0 {
		select {
		case c := <-done:
			record(c)
			remaining--
		case <-term.C:
			goto kill
		}
	}
	// Give descendants whose parent exited a bounded TERM grace period too.
	for anyGroup(workers) {
		select {
		case <-term.C:
			goto kill
		case <-time.After(10 * time.Millisecond):
		}
	}
kill:
	term.Stop()
	for _, w := range workers {
		if w.status.Started && signalGroup(w.command.Process.Pid, true) != nil {
			failed = true
		}
	}
	final := time.NewTimer(grace)
	defer final.Stop()
	for remaining > 0 {
		select {
		case c := <-done:
			record(c)
			remaining--
		case <-final.C:
			failed = true
			goto finish
		}
	}
	for anyGroup(workers) {
		select {
		case <-final.C:
			failed = true
			goto finish
		case <-time.After(10 * time.Millisecond):
		}
	}
finish:
	statuses := make([]status, 0, len(workers))
	for _, w := range workers {
		s := w.status
		if w.status.Started {
			s.GroupGone = !groupExists(w.command.Process.Pid)
		} else {
			s.GroupGone = true
		}
		if w.file != nil && w.file.Close() != nil {
			s.OutputOK = false
		}
		if !s.Started || !s.IdentityRecorded || !s.Joined || s.ExitCode != 0 || !s.OutputOK || !s.GroupGone {
			failed = true
		}
		statuses = append(statuses, s)
	}
	return statuses, failed
}

func anyGroup(workers []*worker) bool {
	for _, w := range workers {
		if w.status.Started && groupExists(w.command.Process.Pid) {
			return true
		}
	}
	return false
}

// Replay a bounded verbatim prefix. Retained private files carry complete raw
// output; the truncation notice explicitly prevents treating a prefix as proof.
func replay(out io.Writer, path string, limit int64) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, io.LimitReader(file, limit)); err != nil {
		return err
	}
	if info.Size() > limit {
		_, err = fmt.Fprintln(out, "\n[raw log replay truncated; complete private log retained]")
	}
	return err
}

func recordIdentity(logDir string, s status) (err error) {
	record := struct {
		Name string `json:"name"`
		PID  int    `json:"pid"`
		PGID int    `json:"pgid"`
	}{s.Name, s.PID, s.PGID}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := os.OpenFile(filepath.Join(logDir, "owned-processes.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}()
	n, err := file.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}
