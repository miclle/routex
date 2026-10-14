// Package deliverycontroller performs one explicitly pinned API-Key file delivery.
// It neither owns server credentials nor invokes an application or inference API.
package deliverycontroller

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	jwtFile         = "/var/run/secrets/routex/workload.jwt"
	keyDirectory    = "/var/run/secrets/routex/api-keys"
	agentVersion    = "1.21.4"
	maxJSONBytes    = 32 << 10
	maxHCLBytes     = 16 << 10
	maxKeyFileBytes = 4096
)

var (
	slotPattern    = regexp.MustCompile(`^kds_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
	keyPattern     = regexp.MustCompile(`^(?:key|pky)_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
	digestPattern  = regexp.MustCompile(`^[a-f0-9]{64}$`)
	segmentPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)
	uuidPattern    = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)
)

// Config contains non-secret bootstrap choices. The bearer is read separately.
type Config struct {
	RouteXURL           string `json:"routex_url"`
	SlotID              string `json:"slot_id"`
	DescriptorRevision  string `json:"descriptor_revision"`
	ConfigurationDigest string `json:"configuration_digest"`
	BearerFile          string `json:"bearer_file"`
	StateDirectory      string `json:"state_directory"`
	AgentExecutable     string `json:"agent_executable"`
	AgentSHA256         string `json:"agent_sha256"`
	TimeoutSeconds      int    `json:"timeout_seconds"`
	AllowLoopbackHTTP   bool   `json:"allow_loopback_http"`
}

// LoadConfig reads a protected, bounded JSON configuration with no interpolation.
func LoadConfig(path string) (Config, error) {
	var cfg Config
	raw, err := readProtected(path, maxJSONBytes)
	if err != nil {
		return cfg, errors.New("read controller configuration failed")
	}
	if decodeJSON(raw, &cfg) != nil || cfg.validate() != nil {
		return Config{}, errors.New("invalid controller configuration")
	}
	return cfg, nil
}

func (cfg Config) validate() error {
	if !slotPattern.MatchString(cfg.SlotID) || !revisionValid(cfg.DescriptorRevision) || !digestPattern.MatchString(cfg.ConfigurationDigest) || !digestPattern.MatchString(cfg.AgentSHA256) || cfg.TimeoutSeconds < 10 || cfg.TimeoutSeconds > 300 {
		return errors.New("invalid controller pins or budget")
	}
	u, err := endpoint(cfg.RouteXURL, cfg.AllowLoopbackHTTP)
	if err != nil || u.Path != "" {
		return errors.New("invalid RouteX origin")
	}
	for _, path := range []string{cfg.BearerFile, cfg.StateDirectory, cfg.AgentExecutable} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || len(path) > 2048 || strings.ContainsAny(path, "\x00\r\n") {
			return errors.New("invalid controller path")
		}
	}
	return nil
}

func revisionValid(revision string) bool {
	n, err := strconv.ParseUint(revision, 10, 63)
	return err == nil && n > 0 && strconv.FormatUint(n, 10) == revision
}

func endpoint(value string, allowLoopbackHTTP bool) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || len(value) == 0 || len(value) > 2048 || strings.ContainsAny(value, "\\%?# \t\r\n\"") || strings.Contains(value, "${") || strings.Contains(value, "%{") || strings.Contains(value, "{{") || strings.Contains(value, "}}") || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" || u.Hostname() == "" || !agentPath(strings.TrimPrefix(u.Path, "/"), 256, true) {
		return nil, errors.New("invalid endpoint")
	}
	if u.Scheme == "https" {
		return u, nil
	}
	// A hostname resolving to loopback is insufficient; explicit numeric loopback
	// prevents DNS changes from turning fixture HTTP into bearer disclosure.
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "http" || !allowLoopbackHTTP || ip == nil || !ip.IsLoopback() {
		return nil, errors.New("endpoint requires verified TLS")
	}
	return u, nil
}

func agentPath(value string, max int, empty bool) bool {
	if value == "" {
		return empty
	}
	if len(value) > max {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if len(part) > 128 || !segmentPattern.MatchString(part) || part == "." || part == ".." || strings.HasSuffix(part, ".") {
			return false
		}
	}
	return true
}

// Decode a closed JSON object, rejecting duplicate members and malformed Unicode
// before encoding/json can silently replace a reviewed scalar or collapse keys.
func decodeJSON(raw []byte, target any) error {
	if !utf8.Valid(raw) || !json.Valid(raw) || !jsonUnicodeValid(raw) {
		return errors.New("invalid JSON encoding")
	}
	scan := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueJSONValue(scan); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid JSON object")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("unexpected JSON suffix")
	}
	return nil
}

func uniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for decoder.More() {
			key, e := decoder.Token()
			name, ok := key.(string)
			if e != nil || !ok || seen[name] {
				return errors.New("duplicate or invalid JSON field")
			}
			seen[name] = true
			if e = uniqueJSONValue(decoder); e != nil {
				return e
			}
		}
	case '[':
		for decoder.More() {
			if err = uniqueJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}

func jsonUnicodeValid(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) || raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		code, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return false
		}
		if code < 0xd800 || code > 0xdbff {
			continue
		}
		if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
			return false
		}
		low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}

func (cfg Config) budget() time.Duration { return time.Duration(cfg.TimeoutSeconds) * time.Second }
