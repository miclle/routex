//go:build linux

package main

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func stdinBounded() ([]byte, error) { return readPipe(int(os.Stdin.Fd()), 5*time.Second) }

// No file path or reader duplication: a finite nonblocking stdin transport only.
// The coordinator must independently validate protected input before streaming.
func readPipe(fd int, budget time.Duration) (raw []byte, err error) {
	if fd < 0 || budget <= 0 || budget > 5*time.Second {
		return nil, errRejected
	}
	var info unix.Stat_t
	if unix.Fstat(fd, &info) != nil || (info.Mode&unix.S_IFMT != unix.S_IFIFO && info.Mode&unix.S_IFMT != unix.S_IFSOCK) {
		return nil, errRejected
	}
	flags, e := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if e != nil || unix.SetNonblock(fd, true) != nil {
		return nil, errRejected
	}
	defer func() {
		if _, restoreErr := unix.FcntlInt(uintptr(fd), unix.F_SETFL, flags); restoreErr != nil {
			err = errRejected
		}
		if err != nil {
			clear(raw)
			raw = nil
		}
	}()
	deadline := time.Now().Add(budget)
	raw = make([]byte, 0, 16384)
	buffer := make([]byte, 4096)
	defer clear(buffer)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return raw, errRejected
		}
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP}}
		ready, pollErr := unix.Poll(fds, int((remaining+time.Millisecond-1)/time.Millisecond))
		if pollErr == unix.EINTR {
			continue
		}
		if pollErr != nil || ready == 0 || fds[0].Revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
			return raw, errRejected
		}
		if time.Until(deadline) <= 0 {
			return raw, errRejected
		}
		n, readErr := unix.Read(fd, buffer)
		if readErr == unix.EAGAIN || readErr == unix.EINTR {
			continue
		}
		if readErr != nil {
			return raw, errRejected
		}
		if time.Until(deadline) <= 0 {
			return raw, errRejected
		}
		if n == 0 {
			if len(raw) == 0 {
				return raw, errRejected
			}
			return raw, nil
		}
		if len(raw)+n > 16384 {
			return raw, errRejected
		}
		raw = append(raw, buffer[:n]...)
		clear(buffer)
	}
}
