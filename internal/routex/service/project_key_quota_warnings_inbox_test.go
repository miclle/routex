package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
)

func projectKeyWarningInboxFixture() (projectKeyQuotaWarningInboxRow, quotaInboxAccess) {
	r, k, u, f := projectKeyWarningFixture()
	v := projectKeyMonthlyWarnings(r, k, u, f, "USD")[1]
	v.ID = "jwo_original"
	return projectKeyQuotaWarningInboxRow{ID: "jwi_original", RecipientID: "usr_original", RecipientCreatedAt: u.CreatedAt, ObservationID: v.ID, CreatedAt: v.AsOf, CurrentRootKeyID: k.ID, CurrentRootProjectID: k.ProjectID, CurrentProjectID: u.ID, CurrentProjectCreatedAt: u.CreatedAt, CurrentProjectStatus: entity.ResourceActive, CurrentRootCreatedAt: k.CreatedAt, Observation: v}, quotaInboxAccess{ActorID: "usr_original", ActorCreatedAt: u.CreatedAt, ProjectIDs: []string{u.ID}}
}
func TestProjectKeyWarningRetainedHistoryExactOwnerAndBirth(t *testing.T) {
	for _, name := range []string{"original", "revoked_retained", "disabled_retained", "renamed", "admin_other", "owner_alias", "root_owner_changed", "removed_manager", "project_reincarnated", "project_disabled", "project_archived", "rejoined_original", "late_manager", "owner_reincarnated", "root_reincarnated", "root_alias", "not_root", "missing_birth", "amount", "threshold", "generation", "invalid_inbox_prefix", "private_fields"} {
		t.Run(name, func(t *testing.T) {
			row, access := projectKeyWarningInboxFixture()
			read := row.CreatedAt.Add(time.Minute)
			row.ReadAt = &read
			switch name {
			case "renamed":
				row.Observation.RootKeyName = "Original recorded name"
			case "admin_other":
				access.Operational = true
				access.ActorID = "prj_other"
			case "owner_alias":
				access.ActorID = strings.ToUpper(access.ActorID)
			case "root_owner_changed":
				row.CurrentRootProjectID = "prj_other"
			case "removed_manager", "late_manager":
				access.ProjectIDs = nil
			case "project_reincarnated":
				row.CurrentProjectCreatedAt = row.CurrentProjectCreatedAt.Add(time.Millisecond)
			case "project_disabled":
				row.CurrentProjectStatus = entity.ResourceDisabled
			case "project_archived":
				row.CurrentProjectStatus = entity.ResourceArchived
			case "owner_reincarnated":
				access.ActorCreatedAt = access.ActorCreatedAt.Add(time.Millisecond)
			case "root_reincarnated":
				row.CurrentRootCreatedAt = row.CurrentRootCreatedAt.Add(time.Millisecond)
			case "root_alias":
				row.CurrentRootKeyID = strings.ToUpper(row.CurrentRootKeyID)
			case "not_root":
				id := "pky_parent"
				row.CurrentRootParent = &id
			case "missing_birth":
				row.RecipientCreatedAt = time.Time{}
			case "amount":
				row.Observation.Settled = "1e3"
			case "threshold":
				row.Observation.Threshold = 80
			case "generation":
				row.Observation.ThresholdGeneration = quotaWarningGeneration
			case "invalid_inbox_prefix":
				row.ID = "qwi_original"
			}
			want := name == "rejoined_original" || name == "original" || name == "revoked_retained" || name == "disabled_retained" || name == "renamed" || name == "private_fields"
			if validProjectKeyQuotaWarningInbox(row, access) != want {
				t.Fatal("retained exact scope", name)
			}
			if want {
				record := projectKeyQuotaWarningRecord(row)
				if record.SubjectType != "project_key" || record.SubjectID != row.Observation.RootKeyID || record.QuotaWarning.ScopeID != record.SubjectID || record.QuotaWarning.ScopeKind != "project_key" || !record.Read || !record.ReadAt.Equal(read) {
					t.Fatal("typed root/history lost")
				}
				raw, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				for _, private := range []string{"project_id", "project_created_at", "recipient", "coverage_start", "resource_created_at", "current_root", "token_hash", "prefix"} {
					if strings.Contains(string(raw), private) {
						t.Fatal("private field exposed", private)
					}
				}
				snapshot, err := json.Marshal(record.QuotaWarning)
				if err != nil {
					t.Fatal(err)
				}
				var fields map[string]json.RawMessage
				if json.Unmarshal(snapshot, &fields) != nil || len(fields) != 14 {
					t.Fatal("warning DTO expanded")
				}
			}
		})
	}
}
func TestProjectKeyWarningIndexedBoundedPortableScopeQueries(t *testing.T) {
	for _, d := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		t.Run(d.Name(), func(t *testing.T) {
			db, err := gorm.Open(d, &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
			if err != nil {
				t.Fatal(err)
			}
			callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{})
			_, access := projectKeyWarningInboxFixture()
			var rows []projectKeyQuotaWarningInboxRow
			q := projectKeyQuotaWarningInboxQuery(db, access).Select(projectKeyQuotaWarningInboxSelect).Limit(21).Find(&rows)
			if q.Error != nil {
				t.Fatal(q.Error)
			}
			sql := q.Statement.SQL.String()
			for _, part := range []string{"JOIN project_api_keys", "root.created_at = warning.resource_created_at", "root.replaces_key_id IS NULL", "root.project_id", "project_id", "project_created_at", "recipient_created_at", "recipient_id", "threshold_generation"} {
				if !strings.Contains(sql, part) {
					t.Fatal("missing conjunctive owner/birth proof", part, sql)
				}
			}
			for _, forbidden := range []string{"root.status =", "prefix", "token_hash", "creator_id", "team_memberships"} {
				if strings.Contains(sql, forbidden) {
					t.Fatal("history revoked/borrowed", forbidden)
				}
			}
			q = projectKeyQuotaWarningMutationQuery(db, access).Where("read_at IS NULL").Update("read_at", nil)
			if q.Error != nil || !strings.Contains(q.Statement.SQL.String(), "EXISTS") {
				t.Fatal("mark-all unscoped", q.Error)
			}
			q = projectKeyQuotaWarningInboxQuery(db, quotaInboxAccess{ActorID: access.ActorID, Operational: true}).Find(&rows)
			if !strings.Contains(q.Statement.SQL.String(), "1 = 0") {
				t.Fatal("missing birth gained history")
			}
			var keys []entity.ProjectKey
			q = projectKeyWarningChainQuery(db, access.ProjectIDs[0]).Find(&keys)
			sql = q.Statement.SQL.String()
			l := q.Statement.Clauses["LIMIT"].Expression.(clause.Limit)
			if q.Error != nil || l.Limit == nil || *l.Limit != 10001 || !strings.Contains(sql, "project_id =") || !strings.Contains(sql, "ORDER BY") || strings.Contains(sql, "token_hash") || strings.Contains(sql, "prefix") {
				t.Fatal("chain lost indexed complete cap/secret exclusion", sql, q.Error)
			}
			if d.Name() == "mysql" && !strings.Contains(sql, "AS BINARY") {
				t.Fatal("collation exact guard missing")
			}
			var policies []entity.ResourceLimit
			q = projectKeyQuotaWarningScanQuery(db, quotaNotificationCursor{ID: "pky_cursor"}).Find(&policies)
			if q.Error != nil || !strings.Contains(q.Statement.SQL.String(), "scope_id >") || *q.Statement.Clauses["LIMIT"].Expression.(clause.Limit).Limit != 32 {
				t.Fatal("fair scan unbounded")
			}
		})
	}
}
func TestProjectKeyWarningCompleteChainSingleQuery(t *testing.T) {
	for _, n := range []int{500, 501, 1000, 10000} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			_, root, owner, _ := projectKeyWarningFixture()
			keys := make([]entity.ProjectKey, n)
			for i := range keys {
				keys[i] = root
				keys[i].ID = fmt.Sprintf("pky_%026d", i)
			}
			state := &projectKeyChainSQLState{keys: keys}
			db := projectKeyChainSQLDatabase(t, state)
			var got []entity.ProjectKey
			start := time.Now()
			if err := projectKeyWarningChainQuery(db, owner.ID).Find(&got).Error; err != nil {
				t.Fatal(err)
			}
			elapsed := time.Since(start)
			if len(got) != n || state.queries != 1 || state.writes != 0 {
				t.Fatal("owner graph N+1/truncated", n, len(got), state.queries, state.writes)
			}
			if _, ok := projectKeyWarningRoots(got, owner); !ok {
				t.Fatal("complete selected rows unproven")
			}
			t.Logf("single GORM query %d rows: %s", n, elapsed)
			if elapsed > 3*time.Second {
				t.Fatal("source hydration exceeded refresh deadline")
			}
		})
	}
}

func TestProjectKeyWarningMissingHistoryAndFairCancellation(t *testing.T) {
	state := &projectKeyWarningSQLState{}
	db := projectKeyWarningSQLDatabase(t, state)
	_, access := projectKeyWarningInboxFixture()
	rows, unread, err := projectKeyQuotaWarningPage(db, access, NotificationFilter{}, 20, time.Time{}, "")
	if err != nil || len(rows) != 0 || unread != 0 {
		t.Fatal("missing history manufactured a row/count", err)
	}
	_, found, err := markProjectKeyQuotaWarningRead(db, access, "jwi_missing")
	if err == nil || found || state.writes != 0 {
		t.Fatal("missing mark became success/mutation", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &Service{db: db}
	if err = s.reconcileMonthlyProjectKeyQuotaWarnings(ctx); err == nil || state.writes != 0 {
		t.Fatal("canceled scan proceeded", err)
	}
	cursor := quotaNotificationCursor{}
	calls := 0
	batch := make([]entity.ResourceLimit, 32)
	for i := range batch {
		batch[i] = entity.ResourceLimit{ScopeKind: "key", ScopeID: fmt.Sprintf("pky_%026d", i)}
	}
	done, err := reconcileQuotaNotificationRows(context.Background(), batch, &cursor, func(context.Context, string, string) error { calls++; return nil })
	if done || err != nil || calls != 32 || cursor.ID != batch[31].ScopeID {
		t.Fatal("bounded batch starved/failed to advance")
	}
	if err = s.observeMonthlyProjectKeyQuotaWarning(context.Background(), "project", "pky_00000000000000000000000001"); err != nil {
		t.Fatal("Project scope entered Personal observer")
	}
}
