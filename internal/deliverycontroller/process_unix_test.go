//go:build darwin || linux

package deliverycontroller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOwnedProcessCancellationJoinsParentAndDescendant(t *testing.T) {
	directory := privateDirectory(t)
	executable := filepath.Join(directory, "agent-fixture")
	writeFile(t, executable, "#!/bin/sh\nsleep 60 &\nchild=$!\ntrap 'wait \"$child\"; exit 0' TERM\nwait \"$child\"\n", 0700)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	result := runProcess(ctx, executable, nil, io.Discard)
	if !result.Started || !result.Joined || !result.GroupGone || result.Succeeded {
		t.Fatalf("cancelled parent/group did not join truthfully: %+v", result)
	}
}

func TestNoProcessStartsAfterCancellation(t *testing.T) {
	directory := privateDirectory(t)
	marker := filepath.Join(directory, "launched")
	executable := filepath.Join(directory, "agent-fixture")
	writeFile(t, executable, "#!/bin/sh\ntouch "+shellQuote(marker)+"\n", 0700)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := runProcess(ctx, executable, nil, io.Discard)
	if result.Started || result.Joined || result.Succeeded {
		t.Fatal("cancelled command started")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("cancelled command changed the filesystem")
	}
}

func TestBinaryPinFailureDoesNotExecuteProgram(t *testing.T) {
	f := newDeliveryFixture(t, "success")
	f.cfg.AgentSHA256 = fixtureDigest
	if _, err := pinnedAgent(context.Background(), f.cfg); err == nil {
		t.Fatal("wrong executable checksum accepted")
	}
	if _, err := os.Stat(f.launches); !os.IsNotExist(err) {
		t.Fatal("wrong executable pin launched Agent")
	}
}

func TestAgentReplacementDuringVersionRefused(t *testing.T) {
	directory := privateDirectory(t)
	executable := filepath.Join(directory, "agent-fixture")
	replacement := filepath.Join(directory, "replacement")
	marker := filepath.Join(directory, "replacement-launched")
	writeFile(t, replacement, "#!/bin/sh\ntouch "+shellQuote(marker)+"\n", 0700)
	script := "#!/bin/sh\n[ \"$1\" = version ] || exit 5\nmv " + shellQuote(replacement) + " " + shellQuote(executable) + "\nprintf 'Vault v1.21.4 (fixture)\\n'\n"
	writeFile(t, executable, script, 0700)
	cfg := fixturePins()
	cfg.AgentExecutable = executable
	hash := sha256.Sum256([]byte(script))
	cfg.AgentSHA256 = hex.EncodeToString(hash[:])
	if _, err := pinnedAgent(context.Background(), cfg); err == nil {
		t.Fatal("version helper replaced the pinned Agent executable")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("replacement executable was launched")
	}
}

func TestAgentExecutableSizeBoundBeforeContentRead(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int64
		want string
	}{
		{"official_linux_arm64_size", 490358435, "agent pin verification interrupted"},
		{"exact_bound", maxAgentExecutableBytes, "agent pin verification interrupted"},
		{"one_byte_over_bound", maxAgentExecutableBytes + 1, "agent executable metadata rejected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := privateDirectory(t)
			executable := filepath.Join(directory, "agent-fixture")
			writeFile(t, executable, "controlled sparse executable fixture", 0700)
			if err := os.Truncate(executable, tc.size); err != nil {
				t.Fatal(err)
			}
			cfg := fixturePins()
			cfg.AgentExecutable = executable
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			// A cancelled context prevents reading or hashing the large sparse
			// fixture. The distinct errors prove rejection happens at metadata
			// for oversized files and reaches the content gate at allowed sizes.
			if _, err := verifyAgentExecutable(ctx, cfg, nil); err == nil || err.Error() != tc.want {
				t.Fatalf("size %d crossed the wrong gate: %v", tc.size, err)
			}
		})
	}
}
