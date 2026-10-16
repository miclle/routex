package service

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func sharedEconomicOriginTestRow(t *testing.T) entity.AdmissionEconomicOrigin {
	t.Helper()
	instant := sharedAuthorizationTestTime().Add(time.Nanosecond).UnixNano()
	row := entity.AdmissionEconomicOrigin{InstallationID: sharedUsageReadTestTarget().InstallationID, FormatVersion: 1, SourceKind: "fresh",
		TimeZone: "UTC", CalendarRevisionAtFreeze: "qst_original", CoverageStart: instant, ClockHighWaterAtFreeze: instant, BoundaryAt: instant}
	sharedEconomicOriginTestEvidence(t, &row)
	return row
}

func sharedEconomicOriginTestEvidence(t *testing.T, row *entity.AdmissionEconomicOrigin) {
	t.Helper()
	body := sharedEconomicOriginBody{FormatVersion: row.FormatVersion, InstallationID: row.InstallationID, SourceKind: row.SourceKind,
		TimeZone: row.TimeZone, CalendarRevisionAtFreeze: row.CalendarRevisionAtFreeze, CoverageStart: row.CoverageStart,
		ClockHighWaterAtFreeze: row.ClockHighWaterAtFreeze, BoundaryAt: row.BoundaryAt}
	var err error
	row.EvidenceBody, err = sharedEconomicCanonicalBody(body)
	if err != nil {
		t.Fatal(err)
	}
	row.EvidenceDigest = sharedEconomicIdentityDigest(string(row.EvidenceBody))
}

func TestSharedEconomicOriginPinsExactFreshCoordinates(t *testing.T) {
	row := sharedEconomicOriginTestRow(t)
	if err := validateSharedEconomicOrigin(row); err != nil {
		t.Fatal("structurally valid fresh record", err)
	}
	var body sharedEconomicOriginBody
	if !sharedEconomicDecodeBody(row.EvidenceBody, &body) || body.BoundaryAt != row.BoundaryAt || body.BoundaryAt%1000 != 1 {
		t.Fatal("origin time was truncated or not recorded exactly", body)
	}
	// Supported exact spellings remain distinct evidence, even if the zones
	// currently calculate identical offsets. No current setting is consulted.
	alias := row
	alias.TimeZone = "Etc/UTC"
	sharedEconomicOriginTestEvidence(t, &alias)
	if err := validateSharedEconomicOrigin(alias); err != nil || alias.EvidenceDigest == row.EvidenceDigest {
		t.Fatal("calendar spelling was normalized or rejected", err)
	}
	for _, revision := range []string{"0", "qst_later", strings.Repeat("x", 64)} {
		changed := row
		changed.CalendarRevisionAtFreeze = revision
		sharedEconomicOriginTestEvidence(t, &changed)
		if err := validateSharedEconomicOrigin(changed); err != nil || changed.EvidenceDigest == row.EvidenceDigest {
			t.Fatal("safe captured revision was normalized or lost", err)
		}
	}
	raw, err := json.Marshal(row)
	if err != nil || string(raw) != "{}" {
		t.Fatal("origin storage became a public DTO", string(raw), err)
	}
}

func TestSharedEconomicOriginRejectsUnsupportedOrChangedFacts(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*entity.AdmissionEconomicOrigin)
	}{
		{"unknown_version", func(r *entity.AdmissionEconomicOrigin) { r.FormatVersion = 2 }},
		{"local_import_reserved", func(r *entity.AdmissionEconomicOrigin) { r.SourceKind = "local_v2" }},
		{"source_case", func(r *entity.AdmissionEconomicOrigin) { r.SourceKind = "FRESH" }},
		{"installation_case", func(r *entity.AdmissionEconomicOrigin) { r.InstallationID = strings.ToUpper(r.InstallationID) }},
		{"host_calendar", func(r *entity.AdmissionEconomicOrigin) { r.TimeZone = "Local" }},
		{"empty_calendar", func(r *entity.AdmissionEconomicOrigin) { r.TimeZone = "" }},
		{"invalid_calendar", func(r *entity.AdmissionEconomicOrigin) { r.TimeZone = "../UTC" }},
		{"calendar_width", func(r *entity.AdmissionEconomicOrigin) { r.TimeZone = strings.Repeat("a", 101) }},
		{"empty_revision", func(r *entity.AdmissionEconomicOrigin) { r.CalendarRevisionAtFreeze = "" }},
		{"unsafe_revision", func(r *entity.AdmissionEconomicOrigin) { r.CalendarRevisionAtFreeze = "qst invalid" }},
		{"revision_width", func(r *entity.AdmissionEconomicOrigin) { r.CalendarRevisionAtFreeze = strings.Repeat("x", 65) }},
		{"zero_coverage", func(r *entity.AdmissionEconomicOrigin) { r.CoverageStart = 0 }},
		{"negative_coverage", func(r *entity.AdmissionEconomicOrigin) { r.CoverageStart = -1 }},
		{"unrepresentable_future", func(r *entity.AdmissionEconomicOrigin) { r.CoverageStart = math.MaxInt64 }},
		{"clock_regression", func(r *entity.AdmissionEconomicOrigin) { r.ClockHighWaterAtFreeze-- }},
		{"clock_not_fresh", func(r *entity.AdmissionEconomicOrigin) { r.ClockHighWaterAtFreeze++ }},
		{"earlier_alleged_coverage", func(r *entity.AdmissionEconomicOrigin) { r.CoverageStart-- }},
		{"boundary_drift", func(r *entity.AdmissionEconomicOrigin) { r.BoundaryAt++ }},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := sharedEconomicOriginTestRow(t)
			test.edit(&row)
			sharedEconomicOriginTestEvidence(t, &row)
			// Recomputed canonical bytes/digest cannot make invalid coordinates
			// or an unsupported imported source structurally valid.
			if err := validateSharedEconomicOrigin(row); !errors.Is(err, errSharedAdmissionConflict) {
				t.Fatal("invalid rehashed record was accepted", err)
			}
		})
	}
}

func TestSharedEconomicOriginRejectsTamperedOrNoncanonicalBody(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*entity.AdmissionEconomicOrigin)
	}{
		{"missing_body", func(r *entity.AdmissionEconomicOrigin) { r.EvidenceBody = nil }},
		{"oversized_body", func(r *entity.AdmissionEconomicOrigin) { r.EvidenceBody = []byte(strings.Repeat("x", 131073)) }},
		{"leading_space", func(r *entity.AdmissionEconomicOrigin) { r.EvidenceBody = append([]byte(" "), r.EvidenceBody...) }},
		{"trailing_object", func(r *entity.AdmissionEconomicOrigin) { r.EvidenceBody = append(r.EvidenceBody, []byte("{}")...) }},
		{"unknown_field", func(r *entity.AdmissionEconomicOrigin) {
			r.EvidenceBody = append([]byte(`{"authentic":true,`), r.EvidenceBody[1:]...)
		}},
		{"duplicate_field", func(r *entity.AdmissionEconomicOrigin) {
			r.EvidenceBody = append([]byte(`{"source_kind":"fresh",`), r.EvidenceBody[1:]...)
		}},
		{"zone_changed", func(r *entity.AdmissionEconomicOrigin) { r.TimeZone = "Etc/UTC" }},
		{"revision_changed", func(r *entity.AdmissionEconomicOrigin) { r.CalendarRevisionAtFreeze = "qst_new" }},
		{"installation_changed", func(r *entity.AdmissionEconomicOrigin) { r.InstallationID = "led_01arz3ndektsv4rrffq69g5faw" }},
		{"coherent_time_changed", func(r *entity.AdmissionEconomicOrigin) { r.CoverageStart++; r.ClockHighWaterAtFreeze++; r.BoundaryAt++ }},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := sharedEconomicOriginTestRow(t)
			test.edit(&row)
			row.EvidenceDigest = sharedEconomicIdentityDigest(string(row.EvidenceBody))
			if err := validateSharedEconomicOrigin(row); !errors.Is(err, errSharedAdmissionConflict) {
				t.Fatal("rehashed malformed/mismatched body was accepted", err)
			}
		})
	}
	for _, digest := range []string{"", strings.Repeat("a", 64), strings.Repeat("A", 64), strings.Repeat("z", 64)} {
		row := sharedEconomicOriginTestRow(t)
		row.EvidenceDigest = digest
		if err := validateSharedEconomicOrigin(row); !errors.Is(err, errSharedAdmissionConflict) {
			t.Fatal("wrong or noncanonical digest accepted", digest, err)
		}
	}
}

func TestSharedEconomicOriginInstantBounds(t *testing.T) {
	maximum := int64(math.MaxInt64 - int64(7*24*time.Hour))
	for _, instant := range []int64{1, sharedAuthorizationTestTime().UnixNano() + 1, maximum} {
		if !sharedEconomicOriginInstantValid(instant) {
			t.Fatal("valid exact nanosecond rejected", instant)
		}
	}
	for _, instant := range []int64{-1, 0, maximum + 1, math.MaxInt64} {
		if sharedEconomicOriginInstantValid(instant) {
			t.Fatal("invalid/out-of-range instant accepted", instant)
		}
	}
}
