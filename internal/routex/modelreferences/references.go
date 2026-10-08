// Package modelreferences owns reviewed advisory Model identities, never routing metadata.
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

//go:embed public-model-references.v1.json
var embedded []byte
var ErrInvalid = errors.New("invalid public Model references")
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

type entry struct {
	Name       string `json:"name"`
	SourceURL  string `json:"source_url"`
	ReviewedAt string `json:"reviewed_at"`
}
type Snapshot struct {
	names  []string
	digest string
}

func Embedded() (*Snapshot, error)  { return Parse(embedded) }
func (s *Snapshot) Names() []string { return slices.Clone(s.names) }
func (s *Snapshot) Digest() string  { return s.digest }
func ValidQuery(q string) bool {
	return len(q) <= 128 && utf8.ValidString(q) && !strings.ContainsFunc(q, unicode.IsControl)
}
func (s *Snapshot) Search(q string) []string {
	result := []string{}
	if !ValidQuery(q) {
		return result
	}
	for _, name := range s.names {
		if strings.Contains(strings.ToLower(name), strings.ToLower(q)) {
			result = append(result, name)
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
	if json.Unmarshal(f["version"], &version) != nil || version != 1 || json.Unmarshal(f["entries"], &entries) != nil || bytes.Equal(bytes.TrimSpace(f["entries"]), []byte("null")) || len(entries) > 100 {
		return nil, ErrInvalid
	}
	names := []string{}
	seen := map[string]bool{}
	for _, rawEntry := range entries {
		if _, err = fields(rawEntry, "name", "source_url", "reviewed_at"); err != nil {
			return nil, err
		}
		var e entry
		if json.Unmarshal(rawEntry, &e) != nil || !namePattern.MatchString(e.Name) || seen[e.Name] || len(e.SourceURL) > 256 {
			return nil, ErrInvalid
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
		names = append(names, e.Name)
	}
	hash := sha256.Sum256(raw)
	return &Snapshot{names: names, digest: hex.EncodeToString(hash[:])}, nil
}
