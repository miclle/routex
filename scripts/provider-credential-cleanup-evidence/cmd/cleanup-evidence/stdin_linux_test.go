//go:build linux

package main

import (
	"bytes"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestNativeStdinRequiresFiniteEOFAndRefusesOverflow(t *testing.T) {
	for _, size := range []int{0, 2, 16384, 16385} {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal("owned pipe unavailable")
		}
		data := bytes.Repeat([]byte("x"), size)
		joined := make(chan struct{})
		go func() { defer close(joined); _, _ = writer.Write(data); _ = writer.Close() }()
		raw, err := readPipe(int(reader.Fd()), time.Second)
		_ = reader.Close()
		_ = writer.Close()
		select {
		case <-joined:
		case <-time.After(time.Second):
			t.Fatal("owned writer did not join")
		}
		valid := size > 0 && size <= 16384
		if (err == nil) != valid || (valid && !bytes.Equal(raw, data)) || (!valid && raw != nil) {
			t.Fatal("stdin EOF/byte bound changed")
		}
		clear(raw)
	}
}

func TestNativeStdinDeadlineRefusesOpenPartialPipeAndRestoresFlags(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal("owned pipe unavailable")
	}
	defer reader.Close()
	defer writer.Close()
	fd := int(reader.Fd())
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		t.Fatal("pipe flags unavailable")
	}
	if _, err := writer.Write([]byte("partial")); err != nil {
		t.Fatal("owned pipe write failed")
	}
	started := time.Now()
	raw, readErr := readPipe(fd, 25*time.Millisecond)
	after, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if readErr == nil || raw != nil || err != nil || flags != after || time.Since(started) > time.Second {
		t.Fatal("open partial stdin escaped deadline or flags changed")
	}
}

func TestNativeStdinRefusesRegularFileInvalidFDAndInvalidBudget(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "stdin-")
	if err != nil {
		t.Fatal("owned file unavailable")
	}
	defer file.Close()
	for _, fd := range []int{-1, int(file.Fd())} {
		if raw, err := readPipe(fd, time.Second); err == nil || raw != nil {
			t.Fatal("non-pipe input accepted")
		}
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal("owned pipe unavailable")
	}
	defer reader.Close()
	defer writer.Close()
	for _, budget := range []time.Duration{0, -time.Second, 5*time.Second + time.Nanosecond} {
		if raw, err := readPipe(int(reader.Fd()), budget); err == nil || raw != nil {
			t.Fatal("invalid stdin budget accepted")
		}
	}
}
