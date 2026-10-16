package service

import (
	"math"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

// This frozen body records calendar and coverage coordinates only. It contains
// no fabricated claim that old local workers/startups are excluded. A future
// constructor must independently establish those facts before writing it.
type sharedEconomicOriginBody struct {
	FormatVersion            int64  `json:"format_version"`
	InstallationID           string `json:"installation_id"`
	SourceKind               string `json:"source_kind"`
	TimeZone                 string `json:"time_zone"`
	CalendarRevisionAtFreeze string `json:"calendar_revision_at_freeze"`
	CoverageStart            int64  `json:"coverage_start"`
	ClockHighWaterAtFreeze   int64  `json:"clock_high_water_at_freeze"`
	BoundaryAt               int64  `json:"boundary_at"`
}

// validateSharedEconomicOrigin is structural validation, not an initialization,
// import or authority check. A matching digest cannot authenticate a source.
func validateSharedEconomicOrigin(row entity.AdmissionEconomicOrigin) error {
	if !admissionOwnerJournal.MatchString(row.InstallationID) || row.FormatVersion != 1 || row.SourceKind != "fresh" ||
		row.TimeZone == "" || len(row.TimeZone) > 100 || row.TimeZone == "Local" || !safeCallID.MatchString(row.CalendarRevisionAtFreeze) ||
		!sharedEconomicOriginInstantValid(row.CoverageStart) || row.ClockHighWaterAtFreeze != row.CoverageStart || row.BoundaryAt != row.CoverageStart ||
		!runtimeRouteDigest.MatchString(row.EvidenceDigest) {
		return errSharedAdmissionConflict
	}
	if _, err := time.LoadLocation(row.TimeZone); err != nil {
		return errSharedAdmissionConflict
	}
	var body sharedEconomicOriginBody
	if !sharedEconomicDecodeBody(row.EvidenceBody, &body) || sharedEconomicIdentityDigest(string(row.EvidenceBody)) != row.EvidenceDigest ||
		body.FormatVersion != row.FormatVersion || body.InstallationID != row.InstallationID || body.SourceKind != row.SourceKind ||
		body.TimeZone != row.TimeZone || body.CalendarRevisionAtFreeze != row.CalendarRevisionAtFreeze ||
		body.CoverageStart != row.CoverageStart || body.ClockHighWaterAtFreeze != row.ClockHighWaterAtFreeze || body.BoundaryAt != row.BoundaryAt {
		return errSharedAdmissionConflict
	}
	return nil
}

func sharedEconomicOriginInstantValid(instant int64) bool {
	return instant > 0 && instant <= math.MaxInt64-int64(7*24*time.Hour) && time.Unix(0, instant).UnixNano() == instant
}
