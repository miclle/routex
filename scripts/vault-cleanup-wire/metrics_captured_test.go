package vaultwire

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// Expected digests bind selected bytes, not their origin or execution. The
// original producer/source/ownership audit remains an independent prerequisite.
func privateCapture(t *testing.T, path, wanted string, limit int64) []byte {
	t.Helper()
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		t.Fatal("Captured path is not canonical")
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil || real != path {
		t.Fatal("Captured path is symbolic or unreadable")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() <= 0 || info.Size() > limit {
		t.Fatal("Captured file is not private and bounded")
	}
	decoded, err := hex.DecodeString(wanted)
	if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != wanted {
		t.Fatal("Captured digest is invalid")
	}
	raw, err := os.ReadFile(path)
	if err != nil || int64(len(raw)) != info.Size() {
		t.Fatal("Captured read incomplete")
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != wanted {
		t.Fatal("Original captured bytes changed")
	}
	return raw
}

func TestCapturedDummyStatusProfile(t *testing.T) {
	names := []string{"ROUTEX_TEST_VAULT_METRICS_BEFORE", "ROUTEX_TEST_VAULT_METRICS_BEFORE_SHA256", "ROUTEX_TEST_VAULT_METRICS_AFTER", "ROUTEX_TEST_VAULT_METRICS_AFTER_SHA256", "ROUTEX_TEST_VAULT_METRICS_READ_RESPONSES"}
	values := make([]string, len(names))
	present := 0
	for i, name := range names {
		values[i] = os.Getenv(name)
		if values[i] != "" {
			present++
		}
	}
	if present == 0 {
		t.Skip("Actual captured-metrics inputs absent; no actual-data or runtime qualification is inferred")
	}
	if present != len(names) {
		t.Fatal("Actual captured-metrics inputs incomplete")
	}
	reads, err := strconv.ParseUint(values[4], 10, 64)
	if err != nil || reads < 1 || reads > 3 || strconv.FormatUint(reads, 10) != values[4] {
		t.Fatal("Independent expected read-response count invalid")
	}
	before, err := ParseStatusCounters(privateCapture(t, values[0], values[1], 4<<20))
	if err != nil {
		t.Fatal("Original before-metrics profile refused")
	}
	after, err := ParseStatusCounters(privateCapture(t, values[2], values[3], 4<<20))
	if err != nil {
		t.Fatal("Original after-metrics profile refused")
	}
	if RequireStatusDelta(before, after, map[uint16]uint64{200: reads, 204: 1}) != nil {
		t.Fatal("Complete captured status-family delta refused")
	}
	t.Log("Actual selected captured metrics parsed; no product cleanup or original process qualification is inferred")
}
