package id

import (
	"strings"
	"testing"
)

func TestNewPrefixedShape(t *testing.T) {
	got, err := NewPrefixed("usr")
	if err != nil {
		t.Fatalf("NewPrefixed: %v", err)
	}
	if !strings.HasPrefix(got, "usr_") {
		t.Fatalf("id = %q, want usr_ prefix", got)
	}
	random := strings.TrimPrefix(got, "usr_")
	if len(random) != 26 {
		t.Fatalf("random part length = %d, want 26", len(random))
	}
	if random != strings.ToLower(random) {
		t.Fatalf("random part = %q, want lowercase", random)
	}
	if strings.Contains(random, "_") {
		t.Fatalf("random part should not contain underscore: %q", random)
	}
}

func TestNewPrefixedRandomness(t *testing.T) {
	first, err := NewPrefixed("ak")
	if err != nil {
		t.Fatalf("first NewPrefixed: %v", err)
	}
	second, err := NewPrefixed("ak")
	if err != nil {
		t.Fatalf("second NewPrefixed: %v", err)
	}
	if first == second {
		t.Fatalf("NewPrefixed returned duplicate id %q", first)
	}
}
