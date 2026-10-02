package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestQuotaInboxManagerIdentityIsExact(t *testing.T) {
	valid := quotaManagerIdentity{
		ManagerUserID: "usr_member", UserID: "usr_member",
		ManagerProjectID: "prj_scope", ProjectID: "prj_scope", ProjectStatus: entity.ResourceActive,
	}
	if !validQuotaManager(valid, "usr_member", "prj_scope") {
		t.Fatal("current enabled manager was rejected")
	}
	for _, mutate := range []func(*quotaManagerIdentity){
		func(row *quotaManagerIdentity) { row.ManagerUserID = "USR_MEMBER" },
		func(row *quotaManagerIdentity) { row.UserID = "usr_other" },
		func(row *quotaManagerIdentity) { row.ManagerProjectID = "PRJ_SCOPE" },
		func(row *quotaManagerIdentity) { row.ProjectID = "prj_other" },
		func(row *quotaManagerIdentity) { row.ProjectStatus = "ACTIVE" },
		func(row *quotaManagerIdentity) { row.ProjectStatus = entity.ResourceArchived },
		func(row *quotaManagerIdentity) { row.Disabled = true },
		func(row *quotaManagerIdentity) { row.Offboarded = true },
	} {
		row := valid
		mutate(&row)
		if validQuotaManager(row, "usr_member", "prj_scope") {
			t.Fatalf("ineligible or aliased identity accepted: %+v", row)
		}
	}
}

func TestQuotaInboxFrozenSnapshotAndAuthority(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 123000000, time.UTC)
	row := quotaInboxRow{
		ID: "qni_test", RecipientID: "usr_member", InboxObservationID: "qob_test", CreatedAt: now,
		Observation: entity.QuotaNotificationObservation{
			ID: "qob_test", ScopeKind: "project", ScopeID: "prj_scope", ScopeName: "Recorded Project",
			Dimension: "money", PolicyRevision: "rev_original", AsOf: now,
			MonthStart: now.AddDate(0, 0, -1), MonthEnd: now.AddDate(0, 1, -1), TimeZone: "UTC",
			Limit: "0.000000000000000001", Settled: "9007199254740993.123456789012345678", Currency: "USD",
		},
	}
	access := quotaInboxAccess{ActorID: "usr_member", ProjectIDs: []string{"prj_scope"}}
	if !validQuotaInboxRow(row, access) {
		t.Fatal("exact current manager cannot read recorded snapshot")
	}
	if validQuotaInboxRow(row, quotaInboxAccess{ActorID: "usr_member", Operational: true}) {
		t.Fatal("system permission substituted for Project management")
	}
	for _, mutate := range []func(*quotaInboxRow){
		func(row *quotaInboxRow) { row.RecipientID = "USR_MEMBER" },
		func(row *quotaInboxRow) { row.InboxObservationID = "QOB_TEST" },
		func(row *quotaInboxRow) { row.Observation.ScopeID = "PRJ_SCOPE" },
		func(row *quotaInboxRow) { row.Observation.ScopeKind = "PROJECT" },
		func(row *quotaInboxRow) { row.Observation.Dimension = "MONEY" },
	} {
		other := row
		mutate(&other)
		if validQuotaInboxRow(other, access) {
			t.Fatalf("aliased frozen row accepted: %+v", other)
		}
	}
	record := quotaNotificationRecord(row)
	if record.AlertID != "" || record.Quota == nil || record.Quota.Currency == nil || *record.Quota.Currency != "USD" || record.Quota.Settled != row.Observation.Settled || record.Quota.Limit != row.Observation.Limit || record.DeliveryStatus != "" {
		t.Fatalf("snapshot changed or acquired operational delivery: %+v", record)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "alert_id") || strings.Contains(string(raw), "delivery_status") {
		t.Fatalf("quota record exposed operational fields: %s", raw)
	}
	row.Observation.Dimension, row.Observation.Currency = "tokens", ""
	if record := quotaNotificationRecord(row); record.Quota.Currency != nil || record.DetailCode != "tokens_month_exhausted" {
		t.Fatal("token observation fabricated currency")
	}
}

func TestNotificationMergedCursorPreservesBothSources(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	rows := []NotificationRecord{
		{ID: "ntf_a", LastSeenAt: now}, {ID: "qni_z", LastSeenAt: now},
		{ID: "ntf_new", LastSeenAt: now.Add(time.Second)}, {ID: "qni_old", LastSeenAt: now.Add(-time.Second)},
	}
	page, cursor := mergeNotificationRecords(rows, 2)
	if len(page) != 2 || page[0].ID != "ntf_new" || page[1].ID != "qni_z" {
		t.Fatalf("sources were paginated separately: %+v", page)
	}
	cursorTime, cursorID, err := decodeNotificationCursor(cursor)
	if err != nil || !cursorTime.Equal(now) || cursorID != "qni_z" {
		t.Fatalf("cursor did not identify the global last emitted record: %s", cursor)
	}
	var remaining []NotificationRecord
	for _, row := range rows {
		if row.LastSeenAt.Before(cursorTime) || row.LastSeenAt.Equal(cursorTime) && row.ID < cursorID {
			remaining = append(remaining, row)
		}
	}
	page, cursor = mergeNotificationRecords(remaining, 2)
	if len(page) != 2 || page[0].ID != "ntf_a" || page[1].ID != "qni_old" || cursor != "" {
		t.Fatalf("equal timestamp merged pagination omitted or repeated a row: %+v %s", page, cursor)
	}
}
