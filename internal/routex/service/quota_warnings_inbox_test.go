package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
)

func TestQuotaWarningInboxExactOwnerAndProjection(t *testing.T) {
	row, created, usage := warningFixture()
	v := monthlyQuotaWarnings(row, created, usage, "USD")[1]
	v.ID = "qwo_original"
	inbox := quotaWarningInboxRow{ID: "qwi_original", RecipientID: v.OwnerID, ObservationID: v.ID, CreatedAt: v.AsOf, Observation: v}
	if !validQuotaWarningInbox(inbox, quotaInboxAccess{ActorID: v.OwnerID, ActorCreatedAt: v.ResourceCreatedAt}) {
		t.Fatal("known original notice rejected")
	}
	record := quotaWarningRecord(inbox)
	if record.Kind != "monthly_quota_warning" || record.Severity != "high" || record.Quota != nil || record.QuotaWarning.Level != "critical" || record.QuotaWarning.Threshold != 90 || record.QuotaWarning.Currency == nil || *record.QuotaWarning.Currency != "USD" || record.QuotaWarning.Settled != v.Settled || record.QuotaWarning.Limit != v.Limit {
		t.Fatal("typed exact snapshot changed", record)
	}
	for _, kind := range []string{"recipient", "owner", "observation", "level", "threshold", "generation", "amount", "currency", "birth"} {
		t.Run(kind, func(t *testing.T) {
			copy := inbox
			switch kind {
			case "recipient":
				copy.RecipientID = strings.ToUpper(copy.RecipientID)
			case "owner":
				copy.Observation.OwnerID += " "
			case "observation":
				copy.ObservationID = strings.ToUpper(copy.ObservationID)
			case "level":
				copy.Observation.Level = "near"
			case "threshold":
				copy.Observation.Threshold = 80
			case "generation":
				copy.Observation.ThresholdGeneration += "x"
			case "amount":
				copy.Observation.Settled = "1e3"
			case "currency":
				copy.Observation.Currency = ""
			case "birth":
				copy.Observation.ResourceCreatedAt = copy.Observation.ResourceCreatedAt.Add(time.Microsecond)
			}
			if validQuotaWarningInbox(copy, quotaInboxAccess{ActorID: v.OwnerID, ActorCreatedAt: v.ResourceCreatedAt}) {
				t.Fatal("alias/corrupt source exposed")
			}
		})
	}
}
func TestQuotaWarningPortableScopedQueryShapes(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		t.Run(dialect.Name(), func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
			if err != nil {
				t.Fatal(err)
			}
			callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{})
			var rows []quotaWarningInboxRow
			q := quotaWarningInboxQuery(db, quotaInboxAccess{ActorID: "usr_exact", ActorCreatedAt: createdForWarningTest()}).Select(quotaWarningInboxSelect).Limit(21).Find(&rows)
			if q.Error != nil {
				t.Fatal(q.Error)
			}
			sql := q.Statement.SQL.String()
			if !strings.Contains(sql, "recipient_id") || !strings.Contains(sql, "owner_id") || !strings.Contains(sql, "threshold_generation") || !strings.Contains(sql, "resource_created_at") || strings.Contains(sql, "project_managers") || strings.Contains(sql, "team_memberships") || !strings.Contains(sql, "JOIN") {
				t.Fatal("private owner query shape", sql)
			}
			if dialect.Name() == "mysql" && strings.Count(sql, "AS BINARY") < 16 {
				t.Fatal("warning identity lost exact collation", sql)
			}
			var policies []entity.ResourceLimit
			q = quotaWarningScanQuery(db, quotaNotificationCursor{Kind: "user", ID: "usr_031"}).Find(&policies)
			if q.Error != nil || !strings.Contains(q.Statement.SQL.String(), "scope_id >") || !strings.Contains(q.Statement.SQL.String(), "ORDER BY") || q.Statement.Clauses["LIMIT"].Expression == nil {
				t.Fatal("bounded personal scan missing")
			}
			q = quotaWarningMutationQuery(db, quotaInboxAccess{ActorID: "usr_exact", ActorCreatedAt: createdForWarningTest()}).Where("read_at IS NULL").Update("read_at", nil)
			if q.Error != nil || !strings.Contains(q.Statement.SQL.String(), "EXISTS") || !strings.Contains(q.Statement.SQL.String(), "owner_id") {
				t.Fatal("mark-all bypassed owner source", q.Error)
			}
		})
	}
}
func TestQuotaWarningScannerFairWrapAndCancellation(t *testing.T) {
	rows := make([]entity.ResourceLimit, 32)
	for i := range rows {
		rows[i] = entity.ResourceLimit{ScopeKind: "user", ScopeID: fmt.Sprintf("usr_%03d", i)}
	}
	cursor := quotaNotificationCursor{}
	calls := 0
	failure := errors.New("outage")
	observe := func(context.Context, string, string) error {
		calls++
		if calls == 1 {
			return failure
		}
		return nil
	}
	done, err := reconcileQuotaNotificationRows(context.Background(), rows, &cursor, observe)
	if done || !errors.Is(err, failure) || calls != 32 || cursor.ID != "usr_031" {
		t.Fatal("failure starved owners")
	}
	_, _ = reconcileQuotaNotificationRows(context.Background(), []entity.ResourceLimit{{ScopeKind: "user", ScopeID: "usr_032"}}, &cursor, observe)
	done, err = reconcileQuotaNotificationRows(context.Background(), nil, &cursor, observe)
	if !done || err != nil || calls != 33 || cursor.ID != "" {
		t.Fatal("full wrap not fair")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = reconcileQuotaNotificationRows(ctx, rows, &cursor, observe)
	if !errors.Is(err, context.Canceled) || calls != 33 {
		t.Fatal("cancel observed owner")
	}
}

func createdForWarningTest() time.Time { return time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC) }
