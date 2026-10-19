package vaultwire

import (
	"strings"
	"testing"
)

const prefix = "# HELP vault_core_response_status_code vault_core_response_status_code\n# TYPE vault_core_response_status_code counter\n"

func samples(read, destroy, unavailable string) []byte {
	return []byte(prefix + "vault_core_response_status_code{code=\"200\",type=\"2xx\"} " + read + "\nvault_core_response_status_code{code=\"204\",type=\"2xx\"} " + destroy + "\nvault_core_response_status_code{code=\"501\",type=\"5xx\"} " + unavailable + "\n")
}

func TestObservedStatusFamilyRetainsWholeSetWithoutAliasing(t *testing.T) {
	before, err := ParseStatusCounters(samples("8", "5", "1"))
	if err != nil {
		t.Fatal("observed shape refused")
	}
	after, err := ParseStatusCounters(samples("9", "6", "1"))
	if err != nil {
		t.Fatal("observed final shape refused")
	}
	if RequireStatusDelta(before, after, map[uint16]uint64{200: 1, 204: 1}) != nil {
		t.Fatal("exact complete-family delta refused")
	}
	view := before.Values()
	view[204] = 0
	if before.Values()[204] != 5 || len(before.SHA256()) != 64 {
		t.Fatal("parsed facts were aliased")
	}
}

func TestStatusWireRefusesUnknownGrammarRatherThanSynthesizingCounters(t *testing.T) {
	valid := string(samples("8", "5", "1"))
	for _, c := range []struct{ name, raw string }{
		{"absent", "# unrelated metric\n"},
		{"no-type", strings.ReplaceAll(valid, "# TYPE vault_core_response_status_code counter\n", "")},
		{"gauge", strings.ReplaceAll(valid, " counter\n", " gauge\n")},
		{"duplicate-header", prefix + valid},
		{"duplicate-series", valid + "vault_core_response_status_code{code=\"204\",type=\"2xx\"} 5\n"},
		{"zero-destroy", string(samples("8", "0", "1"))},
		{"missing-destroy", prefix + "vault_core_response_status_code{code=\"200\",type=\"2xx\"} 8\n"},
		{"wrong-class", strings.Replace(valid, "code=\"501\",type=\"5xx\"", "code=\"501\",type=\"2xx\"", 1)},
		{"extra-label", strings.Replace(valid, "code=\"204\",type=\"2xx\"", "code=\"204\",type=\"2xx\",caller=\"unknown\"", 1)},
		{"fraction", string(samples("8", "5.0", "1"))},
		{"nan", string(samples("8", "NaN", "1"))},
		{"infinity", string(samples("8", "+Inf", "1"))},
		{"negative", string(samples("8", "-1", "1"))},
		{"leading-zero", string(samples("8", "05", "1"))},
		{"unsafe-integer", string(samples("8", "9007199254740992", "1"))},
		{"suffix-series", valid + "vault_core_response_status_code_created 1\n"},
		{"truncated", strings.TrimSuffix(valid, "\n")},
		{"invalid-utf8", valid + string([]byte{255}) + "\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ParseStatusCounters([]byte(c.raw)); err != ErrRejected {
				t.Fatal("unknown metric grammar accepted")
			}
		})
	}
}

func TestWholeFamilyRefusesResetNewSeriesAndUnaccountedHTTP(t *testing.T) {
	before, err := ParseStatusCounters(samples("8", "5", "1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		after []byte
		want  map[uint16]uint64
	}{
		{"reset", samples("9", "4", "1"), map[uint16]uint64{200: 1, 204: 1}},
		{"double-destroy", samples("9", "7", "1"), map[uint16]uint64{200: 1, 204: 1}},
		{"unaccounted-read", samples("10", "6", "1"), map[uint16]uint64{200: 1, 204: 1}},
		{"unaccounted-failure", samples("9", "6", "2"), map[uint16]uint64{200: 1, 204: 1}},
		{"new-series", append(samples("9", "6", "1"), []byte("vault_core_response_status_code{code=\"403\",type=\"4xx\"} 1\n")...), map[uint16]uint64{200: 1, 204: 1}},
		{"invented-zero", samples("9", "6", "1"), map[uint16]uint64{200: 1, 204: 1, 403: 0}},
		{"missing-expectation", samples("9", "6", "1"), map[uint16]uint64{204: 1}},
	} {
		t.Run(c.name, func(t *testing.T) {
			after, err := ParseStatusCounters(c.after)
			if err != nil {
				t.Fatal(err)
			}
			if RequireStatusDelta(before, after, c.want) != ErrRejected {
				t.Fatal("incomplete whole-family delta accepted")
			}
		})
	}
	if RequireStatusDelta(StatusCounters{}, before, map[uint16]uint64{204: 1}) != ErrRejected {
		t.Fatal("unparsed counters accepted")
	}
}
