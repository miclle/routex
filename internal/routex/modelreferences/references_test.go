package modelreferences

import (
	"slices"
	"strings"
	"testing"
)

func TestReviewedReferencesAreExactImmutableAndBounded(t *testing.T) {
	s, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"gpt-5.2", "gpt-5.2-2025-12-11", "claude-sonnet-4-6", "gemini-2.5-flash"}
	if !slices.Equal(s.Names(), want) || len(s.Digest()) != 64 {
		t.Fatal("reviewed source changed")
	}
	copy := s.Names()
	copy[0] = "changed"
	if !slices.Equal(s.Names(), want) {
		t.Fatal("caller mutated snapshot")
	}
	if !slices.Equal(s.Search("GPT-5.2"), want[:2]) || len(s.Search("%")) != 0 || len(s.Search("arbitrary-private-name")) != 0 {
		t.Fatal("literal search broadened namespace")
	}
}
func TestReferenceMetadataFailsClosed(t *testing.T) {
	raw := string(embedded)
	for _, bad := range []string{
		strings.Replace(raw, `"version": 2`, `"version": 2, "version": 2`, 1),
		strings.Replace(raw, `"version": 2`, `"version": 3`, 1),
		strings.Replace(raw, `"entries": [`, `"extra": true,"entries": [`, 1),
		strings.Replace(raw, `"name": "gpt-5.2"`, `"name": "gpt-5.2", "name": "gpt-5.2"`, 1),
		strings.Replace(raw, `"name": "gpt-5.2-2025-12-11"`, `"name": "gpt-5.2"`, 1),
		strings.Replace(raw, "2026-10-12", "2026-02-30", 1),
		strings.Replace(raw, "https://developers.openai.com", "https://user@developers.openai.com", 1),
		strings.Replace(raw, "https://developers.openai.com", "https://untrusted.example", 1),
		strings.Replace(raw, "api/docs/models/gpt-5.2", "api/docs/models/gpt-5.2?x=y", 1),
		`{"version":2,"entries":null}`, raw + raw,
		strings.Replace(raw, `"aliases": [`, `"aliases": null, "ignored": [`, 1),
		strings.Replace(raw, `"GPT 5.2",`, `"GPT 5.2", "gpt 5.2",`, 1),
		strings.Replace(raw, `"GPT 5.2"`, `" leading"`, 1),
		strings.Replace(raw, `"image_input"`, `"made_up"`, 1),
		strings.Replace(raw, `"openai_chat"`, `"arbitrary_protocol"`, 1),
		strings.Replace(raw, `"image_input"`, `"image_input", "image_input"`, 1),
	} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Fatal("invalid reference accepted")
		}
	}
	for _, q := range []string{strings.Repeat("x", 129), "x\n", string([]byte{255})} {
		if ValidQuery(q) {
			t.Fatal("unsafe query accepted")
		}
	}
	entries := []string{}
	for i := 0; i < 12; i++ {
		entries = append(entries, `{"aliases":[],"protocols":[],"capabilities":[],"name":"model-`+string(rune('a'+i))+`","source_url":"https://ai.google.dev/api/models","reviewed_at":"2026-10-12"}`)
	}
	many, err := Parse([]byte(`{"version":2,"entries":[` + strings.Join(entries, ",") + `]}`))
	if err != nil || len(many.Search("")) != 8 {
		t.Fatal("suggestion bound", err)
	}
}

func TestAliasesReturnCanonicalNamesAndMetadataCannotMutateSnapshot(t *testing.T) {
	s, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.Search("GPT 5.2"), []string{"gpt-5.2", "gpt-5.2-2025-12-11"}) || !slices.Equal(s.Search("SONNET 4.6"), []string{"claude-sonnet-4-6"}) {
		t.Fatal("alias search lost canonical identities")
	}
	m, ok := s.Metadata("gpt-5.2")
	if !ok || !slices.Equal(m.Protocols, []string{"openai_chat", "openai_responses"}) || !slices.Equal(m.Capabilities, []string{"image_input"}) {
		t.Fatal("reviewed positive hints missing")
	}
	m.Aliases[0] = "changed"
	m.Protocols[0] = "changed"
	m.Capabilities[0] = "changed"
	m, _ = s.Metadata("gpt-5.2")
	if m.Aliases[0] == "changed" || m.Protocols[0] == "changed" || m.Capabilities[0] == "changed" {
		t.Fatal("caller mutated reference")
	}
	if _, ok := s.Metadata("GPT 5.2"); ok {
		t.Fatal("search alias became canonical identity")
	}
}
