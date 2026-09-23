package secret

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func TestRandomURLSafeShape(t *testing.T) {
	got, err := RandomURLSafe(32)
	if err != nil {
		t.Fatalf("RandomURLSafe: %v", err)
	}
	if len(got) != 43 {
		t.Fatalf("encoded length = %d, want 43", len(got))
	}
	if strings.ContainsAny(got, "+/=") {
		t.Fatalf("URL-safe token contains non URL-safe characters: %q", got)
	}
	raw, err := base64.RawURLEncoding.DecodeString(got)
	if err != nil {
		t.Fatalf("decode URL-safe token: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("decoded length = %d, want 32", len(raw))
	}
}

func TestRandomBase64Shape(t *testing.T) {
	got, err := RandomBase64(32)
	if err != nil {
		t.Fatalf("RandomBase64: %v", err)
	}
	if len(got) != 43 {
		t.Fatalf("encoded length = %d, want 43", len(got))
	}
	if strings.ContainsAny(got, "_=") {
		t.Fatalf("standard base64 token should not contain '_' or padding: %q", got)
	}
	raw, err := base64.RawStdEncoding.DecodeString(got)
	if err != nil {
		t.Fatalf("decode base64 token: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("decoded length = %d, want 32", len(raw))
	}
}

func TestRandomValuesDiffer(t *testing.T) {
	first, err := RandomURLSafe(32)
	if err != nil {
		t.Fatalf("first RandomURLSafe: %v", err)
	}
	second, err := RandomURLSafe(32)
	if err != nil {
		t.Fatalf("second RandomURLSafe: %v", err)
	}
	if first == second {
		t.Fatalf("RandomURLSafe returned duplicate value %q", first)
	}
}

func TestSHA256Hex(t *testing.T) {
	got := SHA256Hex("hello")
	const want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Fatalf("SHA256Hex = %q, want %q", got, want)
	}
	if _, err := hex.DecodeString(got); err != nil {
		t.Fatalf("SHA256Hex returned non-hex value: %v", err)
	}
}
