//go:build !linux

package main

import "testing"

func TestUnsupportedNativeStdinAlwaysRefusesWithoutReading(t *testing.T) {
	if raw, err := stdinBounded(); err == nil || raw != nil {
		t.Fatal("unsupported-platform stdin fallback accepted")
	}
}
