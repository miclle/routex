package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func TestCredentialAttemptStatisticsProjectionBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	attempt := func(status, code string) credentialStatisticsAttempt {
		return credentialStatisticsAttempt{ID: "att_recorded", CredentialID: "crd_target", Status: status, ErrorCode: code,
			StartedAt: now.Add(-time.Second), CompletedAt: now}
	}
	failure, success, canceled := attempt("error", "process_interrupted"), attempt("success", ""), attempt("canceled", "canceled")
	unknown := attempt("legacy_unknown", "private upstream body")
	invalidTime := failure
	invalidTime.CompletedAt = time.Time{}
	future := failure
	future.CompletedAt = now.Add(time.Second)
	alias := failure
	alias.CredentialID = "CRD_TARGET"
	type projectionCase struct {
		name   string
		rows   []credentialStatisticsAttempt
		state  string
		count  *int
		lower  int
		recent string
		code   *string
	}
	tests := []projectionCase{}
	integer := func(n int) *int { return &n }
	text := func(s string) *string { return &s }
	tests = append(tests,
		projectionCase{"no records", nil, "no_records", nil, 0, "no_records", nil},
		projectionCase{"success zero retains older error", []credentialStatisticsAttempt{success, failure}, "exact", integer(0), 0, "recorded", text("process_interrupted")},
		projectionCase{"error prefix resets", []credentialStatisticsAttempt{failure, failure, success, failure}, "exact", integer(2), 2, "recorded", text("process_interrupted")},
		projectionCase{"complete retained errors", []credentialStatisticsAttempt{failure, failure}, "exact", integer(2), 2, "recorded", text("process_interrupted")},
		projectionCase{"canceled boundary never bridged", []credentialStatisticsAttempt{failure, canceled, failure, success}, "unknown", nil, 1, "recorded", text("process_interrupted")},
		projectionCase{"cancellation does not hide older error", []credentialStatisticsAttempt{canceled, failure}, "unknown", nil, 0, "recorded", text("process_interrupted")},
		projectionCase{"known cancellation is no error", []credentialStatisticsAttempt{canceled}, "unknown", nil, 0, "none", nil},
		projectionCase{"unknown status", []credentialStatisticsAttempt{unknown}, "unknown", nil, 0, "unknown", nil},
		projectionCase{"unknown blocks older recent error", []credentialStatisticsAttempt{unknown, failure}, "unknown", nil, 0, "unknown", nil},
		projectionCase{"invalid timestamp", []credentialStatisticsAttempt{invalidTime}, "unknown", nil, 0, "unknown", nil},
		projectionCase{"future timestamp remains unknown boundary", []credentialStatisticsAttempt{future, success}, "unknown", nil, 0, "unknown", nil},
		projectionCase{"alias never attributed", []credentialStatisticsAttempt{alias}, "unknown", nil, 0, "unknown", nil},
	)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := projectCredentialAttemptStatistics("crd_target", "con_target", tc.rows, now)
			if got.FailureStreak.State != tc.state || got.FailureStreak.LowerBound != tc.lower ||
				(got.FailureStreak.Count == nil) != (tc.count == nil) || tc.count != nil && *got.FailureStreak.Count != *tc.count ||
				got.RecentError.State != tc.recent || (got.RecentError.Code == nil) != (tc.code == nil) || tc.code != nil && *got.RecentError.Code != *tc.code {
				t.Fatal(got)
			}
			if got.RecentError.State == "recorded" && (got.RecentError.CompletedAt == nil || !got.RecentError.CompletedAt.Equal(now)) {
				t.Fatal(got)
			}
		})
	}
}

func TestCredentialAttemptStatisticsSentinelAndSanitization(t *testing.T) {
	now := time.Now().UTC()
	rows := make([]credentialStatisticsAttempt, 101)
	for i := range rows {
		rows[i] = credentialStatisticsAttempt{ID: "att_safe", CredentialID: "crd_target", Status: "error", ErrorCode: "private-secret-response", StartedAt: now, CompletedAt: now}
	}
	got := projectCredentialAttemptStatistics("crd_target", "con_target", rows, now)
	if got.InspectedAttempts != 100 || !got.HasMore || got.FailureStreak.State != "lower_bound" || got.FailureStreak.Count != nil || got.FailureStreak.LowerBound != 100 || got.RecentError.Code != nil {
		t.Fatal(got)
	}
	if got.LastAttempt.State != "recorded" || got.LastAttempt.CompletedAt == nil || !got.LastAttempt.CompletedAt.Equal(now) {
		t.Fatal("sentinel must not hide the newest recorded completion", got)
	}
	rows[0].Status = "success"
	got = projectCredentialAttemptStatistics("crd_target", "con_target", rows, now)
	if got.FailureStreak.State != "exact" || got.FailureStreak.Count == nil || *got.FailureStreak.Count != 0 || got.RecentError.State != "recorded" {
		t.Fatal(got)
	}
	for i := range rows {
		rows[i].Status = "success"
	}
	got = projectCredentialAttemptStatistics("crd_target", "con_target", rows, now)
	if got.RecentError.State != "unknown" {
		t.Fatal(got)
	}
	raw, err := json.Marshal(got)
	if err != nil || strings.Contains(string(raw), "private-secret-response") || strings.Contains(string(raw), "snapshot") {
		t.Fatal(string(raw), err)
	}
}

func TestCredentialAttemptStatisticsRejectsInvalidBatchBeforeDatabase(t *testing.T) {
	s := &Service{}
	for _, ids := range [][]string{nil, {}, {"crd_target", "crd_target"}, {"CRD_TARGET"}, {"crd_target "}, {"crd_" + strings.Repeat("a", 27)}, make([]string, 21)} {
		if got, err := s.GetCredentialAttemptStatistics(context.Background(), "usr_actor", "prv_target", ids); got != nil || err != apperrors.ErrBadRequest {
			t.Fatal(got, err, ids)
		}
	}
}

func TestCredentialAttemptStatisticsLastRecordedCompletion(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 123456000, time.UTC)
	newest := credentialStatisticsAttempt{ID: "att_newest", CredentialID: "crd_target", Status: "success", StartedAt: now.Add(-time.Second), CompletedAt: now}
	older := newest
	older.ID, older.Status, older.ErrorCode = "att_older", "error", "upstream_timeout"
	older.StartedAt, older.CompletedAt = now.Add(-3*time.Second), now.Add(-2*time.Second)
	for _, status := range []string{"success", "error", "canceled"} {
		newest.Status = status
		got := projectCredentialAttemptStatistics("crd_target", "con_target", []credentialStatisticsAttempt{newest, older}, now)
		if got.LastAttempt.State != "recorded" || got.LastAttempt.CompletedAt == nil || !got.LastAttempt.CompletedAt.Equal(now) || got.LastAttempt.CompletedAt.Location() != time.UTC {
			t.Fatalf("latest %s completion not retained: %+v", status, got.LastAttempt)
		}
		if status != "error" && (got.RecentError.CompletedAt == nil || !got.RecentError.CompletedAt.Equal(older.CompletedAt)) {
			t.Fatal("latest attempt must remain separate from the older recorded error", got)
		}
	}
	for _, corrupt := range []string{"status", "credential", "id", "start", "completion", "chronology", "future"} {
		row := newest
		switch corrupt {
		case "status":
			row.Status = "legacy_unknown"
		case "credential":
			row.CredentialID = "CRD_TARGET"
		case "id":
			row.ID = "att_invalid "
		case "start":
			row.StartedAt = time.Time{}
		case "completion":
			row.CompletedAt = time.Time{}
		case "chronology":
			row.StartedAt = now.Add(time.Second)
		case "future":
			row.CompletedAt = now.Add(time.Nanosecond)
		}
		got := projectCredentialAttemptStatistics("crd_target", "con_target", []credentialStatisticsAttempt{row, older}, now)
		if got.LastAttempt.State != "unknown" || got.LastAttempt.CompletedAt != nil {
			t.Fatalf("%s newest row must not fall back to an older completion: %+v", corrupt, got.LastAttempt)
		}
	}
	empty := projectCredentialAttemptStatistics("crd_target", "con_target", nil, now)
	if empty.LastAttempt.State != "no_records" || empty.LastAttempt.CompletedAt != nil {
		t.Fatal("no recorded attempts is distinct from unknown completion", empty)
	}
}
