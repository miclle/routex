// Package modelreferences owns reviewed advisory Model identities, never routing authority.
package modelreferences

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

//go:embed public-model-references.v2.json
var embedded []byte
var ErrInvalid = errors.New("invalid public Model references")
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

// Metadata records positive reviewed hints only. Omission is not a denial of support.
type Metadata struct {
	Aliases      []string `json:"aliases"`
	Protocols    []string `json:"protocols"`
	Capabilities []string `json:"capabilities"`
	SourceURL    string   `json:"source_url"`
	ReviewedAt   string   `json:"reviewed_at"`
}
type Entry struct {
	Name string `json:"name"`
	Metadata
}
type Snapshot struct {
	entries []Entry
	digest  string
}

func Embedded() (*Snapshot, error) { return Parse(embedded) }
func (s *Snapshot) Names() []string {
	names := make([]string, 0, len(s.entries))
	for _, entry := range s.entries {
		names = append(names, entry.Name)
	}
	return names
}
func (s *Snapshot) Metadata(name string) (Metadata, bool) {
	for _, entry := range s.entries {
		if entry.Name == name {
			m := entry.Metadata
			m.Aliases = slices.Clone(m.Aliases)
			m.Protocols = slices.Clone(m.Protocols)
			m.Capabilities = slices.Clone(m.Capabilities)
			return m, true
		}
	}
	return Metadata{}, false
}
func (s *Snapshot) Digest() string { return s.digest }
func ValidQuery(q string) bool {
	return len(q) <= 128 && utf8.ValidString(q) && !strings.ContainsFunc(q, unicode.IsControl)
}
func (s *Snapshot) Search(q string) []string {
	result := []string{}
	if !ValidQuery(q) {
		return result
	}
	for _, entry := range s.entries {
		matches := strings.Contains(strings.ToLower(entry.Name), strings.ToLower(q))
		for _, alias := range entry.Aliases {
			matches = matches || strings.Contains(strings.ToLower(alias), strings.ToLower(q))
		}
		if matches {
			result = append(result, entry.Name)
			if len(result) == 8 {
				break
			}
		}
	}
	return result
}
func fields(raw []byte, expected ...string) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrInvalid
	}
	result := map[string]json.RawMessage{}
	for d.More() {
		token, err = d.Token()
		key, ok := token.(string)
		if err != nil || !ok || !slices.Contains(expected, key) {
			return nil, ErrInvalid
		}
		if _, exists := result[key]; exists {
			return nil, ErrInvalid
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, ErrInvalid
		}
		result[key] = value
	}
	if _, err = d.Token(); err != nil || len(result) != len(expected) {
		return nil, ErrInvalid
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, ErrInvalid
	}
	return result, nil
}
func Parse(raw []byte) (*Snapshot, error) {
	if len(raw) == 0 || len(raw) > 65536 || !utf8.Valid(raw) {
		return nil, ErrInvalid
	}
	f, err := fields(raw, "version", "entries")
	if err != nil {
		return nil, err
	}
	var version int
	var entries []json.RawMessage
	if json.Unmarshal(f["version"], &version) != nil || version != 2 || json.Unmarshal(f["entries"], &entries) != nil || bytes.Equal(bytes.TrimSpace(f["entries"]), []byte("null")) || len(entries) > 100 {
		return nil, ErrInvalid
	}
	parsed := []Entry{}
	seen := map[string]bool{}
	for _, rawEntry := range entries {
		if _, err = fields(rawEntry, "name", "aliases", "protocols", "capabilities", "source_url", "reviewed_at"); err != nil {
			return nil, err
		}
		var e Entry
		if json.Unmarshal(rawEntry, &e) != nil || !namePattern.MatchString(e.Name) || seen[e.Name] || len(e.SourceURL) > 256 {
			return nil, ErrInvalid
		}
		if e.Aliases == nil || len(e.Aliases) > 8 || e.Protocols == nil || len(e.Protocols) > 4 || e.Capabilities == nil || len(e.Capabilities) > 2 {
			return nil, ErrInvalid
		}
		aliases := map[string]bool{}
		for _, alias := range e.Aliases {
			normalized := strings.ToLower(alias)
			if alias == "" || strings.TrimSpace(alias) != alias || !ValidQuery(alias) || aliases[normalized] || normalized == strings.ToLower(e.Name) {
				return nil, ErrInvalid
			}
			aliases[normalized] = true
		}
		for _, group := range []struct{ values, allowed []string }{
			{e.Protocols, []string{"openai_chat", "openai_responses", "anthropic_messages", "gemini_generate_content"}},
			{e.Capabilities, []string{"image_input", "pdf_input"}},
		} {
			seen := map[string]bool{}
			for _, value := range group.values {
				if !slices.Contains(group.allowed, value) || seen[value] {
					return nil, ErrInvalid
				}
				seen[value] = true
			}
		}
		u, err := url.Parse(e.SourceURL)
		if err != nil || u.String() != e.SourceURL || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" || !slices.Contains([]string{"developers.openai.com", "platform.claude.com", "ai.google.dev"}, u.Hostname()) || u.Host != u.Hostname() {
			return nil, ErrInvalid
		}
		date, err := time.Parse("2006-01-02", e.ReviewedAt)
		if err != nil || date.Format("2006-01-02") != e.ReviewedAt {
			return nil, ErrInvalid
		}
		seen[e.Name] = true
		parsed = append(parsed, e)
	}
	hash := sha256.Sum256(raw)
	return &Snapshot{entries: parsed, digest: hex.EncodeToString(hash[:])}, nil
}
