package service

import (
	"strings"
	"testing"
)

func TestSitePresentationValidation(t *testing.T) {
	base := SiteInput{Name: " RouteX ", ServiceURL: " https://example.test/ ", LogoURL: "https://cdn.example.test/logo.svg?v=2", Footer: " Footer ", DefaultLanguage: "en", ETag: "0"}
	normalized, err := normalizeSiteInput(base)
	if err != nil || normalized.Name != "RouteX" || normalized.ServiceURL != "https://example.test" || normalized.Footer != "Footer" {
		t.Fatalf("normalization failed: %+v %v", normalized, err)
	}
	for _, value := range []string{"javascript:alert(1)", "data:image/svg+xml,test", "//example.test/logo", "https://user:secret@example.test/logo", "https://example.test/#fragment", "https://example.test/\nheader", "https://", strings.Repeat("x", 2049)} {
		candidate := base
		candidate.LogoURL = value
		if _, err := normalizeSiteInput(candidate); err == nil {
			t.Fatalf("unsafe URL accepted: %q", value)
		}
	}
	for _, language := range []string{"", "fr", "EN"} {
		candidate := base
		candidate.DefaultLanguage = language
		if _, err := normalizeSiteInput(candidate); err == nil {
			t.Fatal("unsupported language accepted")
		}
	}
	for _, name := range []string{" ", strings.Repeat("中", 101), "bad\x00name", "bad\nname"} {
		candidate := base
		candidate.Name = name
		if _, err := normalizeSiteInput(candidate); err == nil {
			t.Fatal("invalid name accepted")
		}
	}
}
func TestAnnouncementPlaintextBounds(t *testing.T) {
	literal := "<script>alert('literal')</script>\nSecond line"
	if value, err := normalizeAnnouncement(" " + literal + " "); err != nil || value != literal {
		t.Fatal("plain content changed")
	}
	if _, err := normalizeAnnouncement(strings.Repeat("中", 4000)); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{" ", "a\x00b", strings.Repeat("中", 4001), string([]byte{255})} {
		if _, err := normalizeAnnouncement(value); err == nil {
			t.Fatal("invalid announcement accepted")
		}
	}
}
