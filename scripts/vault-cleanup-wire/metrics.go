// Package vaultwire parses the bounded wire profile observed from the isolated
// Vault fixture. Parsed data does not authenticate its producer or prove cleanup.
package vaultwire

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var ErrRejected = errors.New("vault wire evidence rejected")

const statusFamily = "vault_core_response_status_code"
const maxExactCounter = uint64(1<<53 - 1)

var statusSample = regexp.MustCompile(`^vault_core_response_status_code\{code="([1-5][0-9]{2})",type="([1-5]xx)"\} (0|[1-9][0-9]*)$`)

// StatusCounters retains the complete status family, including explicit zeros.
// Callers receive copies so a parsed counter set cannot be silently rewritten.
type StatusCounters struct {
	values map[uint16]uint64
	digest string
}

func (s StatusCounters) Values() map[uint16]uint64 {
	if s.values == nil {
		return nil
	}
	out := make(map[uint16]uint64, len(s.values))
	for k, v := range s.values {
		out[k] = v
	}
	return out
}

func (s StatusCounters) SHA256() string { return s.digest }

// ParseStatusCounters accepts the actual bounded Prometheus profile observed
// from Vault 2.2.0. Unsupported syntax refuses instead of guessing a value or
// silently substituting zero. Other metric families carry no evidence here.
func ParseStatusCounters(raw []byte) (StatusCounters, error) {
	if len(raw) == 0 || len(raw) > 4<<20 || raw[len(raw)-1] != '\n' || !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		return StatusCounters{}, ErrRejected
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) > 8192 {
		return StatusCounters{}, ErrRejected
	}
	values := map[uint16]uint64{}
	help, kind := false, false
	for _, line := range lines {
		if !strings.Contains(line, statusFamily) {
			continue
		}
		switch line {
		case "# HELP " + statusFamily + " " + statusFamily:
			if help || kind || len(values) != 0 {
				return StatusCounters{}, ErrRejected
			}
			help = true
		case "# TYPE " + statusFamily + " counter":
			if !help || kind || len(values) != 0 {
				return StatusCounters{}, ErrRejected
			}
			kind = true
		default:
			if !help || !kind {
				return StatusCounters{}, ErrRejected
			}
			match := statusSample.FindStringSubmatch(line)
			if len(match) != 4 || match[1][:1]+"xx" != match[2] {
				return StatusCounters{}, ErrRejected
			}
			code, err := strconv.ParseUint(match[1], 10, 16)
			if err != nil {
				return StatusCounters{}, ErrRejected
			}
			count, err := strconv.ParseUint(match[3], 10, 64)
			if err != nil || count > maxExactCounter {
				return StatusCounters{}, ErrRejected
			}
			if _, duplicate := values[uint16(code)]; duplicate {
				return StatusCounters{}, ErrRejected
			}
			values[uint16(code)] = count
		}
	}
	if !help || !kind || len(values) == 0 {
		return StatusCounters{}, ErrRejected
	}
	if count, present := values[204]; !present || count == 0 {
		return StatusCounters{}, ErrRejected
	}
	sum := sha256.Sum256(raw)
	return StatusCounters{values: values, digest: hex.EncodeToString(sum[:])}, nil
}

// RequireStatusDelta compares every member of both complete status families.
// Missing series, resets, unknown increments and caller-invented zeros refuse.
// Expected values must come from an independently observed exact HTTP/audit
// bracket. This function does not establish namespace isolation or attribution.
func RequireStatusDelta(before, after StatusCounters, expected map[uint16]uint64) error {
	if len(before.values) == 0 || len(before.values) != len(after.values) || len(before.digest) != 64 || len(after.digest) != 64 || len(expected) == 0 {
		return ErrRejected
	}
	if expected[204] != 1 || before.values[204] == 0 {
		return ErrRejected
	}
	for code, delta := range expected {
		if _, present := before.values[code]; !present || delta > maxExactCounter {
			return ErrRejected
		}
	}
	for code, prior := range before.values {
		current, present := after.values[code]
		if !present || current < prior || current-prior != expected[code] {
			return ErrRejected
		}
	}
	return nil
}
