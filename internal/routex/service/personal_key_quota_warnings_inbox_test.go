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

func personalKeyWarningInboxFixture() (personalKeyQuotaWarningInboxRow, quotaInboxAccess) {
	r, k, u, f := personalKeyWarningFixture()
	v := personalKeyMonthlyWarnings(r, k, u, f, "USD")[1]
	v.ID = "kwo_original"
	return personalKeyQuotaWarningInboxRow{ID: "kwi_original", RecipientID: u.ID, RecipientCreatedAt: u.CreatedAt, ObservationID: v.ID, CreatedAt: v.AsOf, CurrentRootKeyID: k.ID, CurrentRootUserID: k.UserID, CurrentRootCreatedAt: k.CreatedAt, Observation: v}, quotaInboxAccess{ActorID: u.ID, ActorCreatedAt: u.CreatedAt}
}
func TestPersonalKeyWarningRetainedHistoryExactOwnerAndBirth(t *testing.T) {
	for _, name := range []string{"original", "revoked_retained", "disabled_retained", "renamed", "admin_other", "owner_alias", "root_owner_changed", "owner_reincarnated", "root_reincarnated", "root_alias", "not_root", "missing_birth", "amount", "threshold", "generation", "invalid_inbox_prefix", "private_fields"} {
		t.Run(name, func(t *testing.T) {
			row, access := personalKeyWarningInboxFixture()
			read := row.CreatedAt.Add(time.Minute)
			row.ReadAt = &read
			switch name {
			case "renamed":
				row.Observation.RootKeyName = "Original recorded name"
			case "admin_other":
				access.Operational = true
				access.ActorID = "usr_other"
			case "owner_alias":
				access.ActorID = strings.ToUpper(access.ActorID)
			case "root_owner_changed":
				row.CurrentRootUserID = "usr_other"
			case "owner_reincarnated":
				access.ActorCreatedAt = access.ActorCreatedAt.Add(time.Millisecond)
			case "root_reincarnated":
				row.CurrentRootCreatedAt = row.CurrentRootCreatedAt.Add(time.Millisecond)
			case "root_alias":
				row.CurrentRootKeyID = strings.ToUpper(row.CurrentRootKeyID)
			case "not_root":
				id := "key_parent"
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
			want := name == "original" || name == "revoked_retained" || name == "disabled_retained" || name == "renamed" || name == "private_fields"
			if validPersonalKeyQuotaWarningInbox(row, access) != want {
				t.Fatal("retained exact scope", name)
			}
			if want {
				record := personalKeyQuotaWarningRecord(row)
				if record.SubjectType != "personal_key" || record.SubjectID != row.Observation.RootKeyID || record.QuotaWarning.ScopeID != record.SubjectID || record.QuotaWarning.ScopeKind != "personal_key" || !record.Read || !record.ReadAt.Equal(read) {
					t.Fatal("typed root/history lost")
				}
				raw, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				for _, private := range []string{"owner_id", "owner_created_at", "recipient", "coverage_start", "resource_created_at", "current_root", "token_hash", "prefix"} {
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
func TestPersonalKeyWarningIndexedBoundedPortableScopeQueries(t *testing.T) {
	for _, d := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		t.Run(d.Name(), func(t *testing.T) {
			db, err := gorm.Open(d, &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
			if err != nil {
				t.Fatal(err)
			}
			callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{})
			_, access := personalKeyWarningInboxFixture()
			var rows []personalKeyQuotaWarningInboxRow
			q := personalKeyQuotaWarningInboxQuery(db, access).Select(personalKeyQuotaWarningInboxSelect).Limit(21).Find(&rows)
			if q.Error != nil {
				t.Fatal(q.Error)
			}
			sql := q.Statement.SQL.String()
			for _, part := range []string{"JOIN api_keys", "root.created_at = warning.resource_created_at", "root.replaces_key_id IS NULL", "root.user_id", "owner_id", "owner_created_at", "recipient_created_at", "recipient_id", "threshold_generation"} {
				if !strings.Contains(sql, part) {
					t.Fatal("missing conjunctive owner/birth proof", part, sql)
				}
			}
			for _, forbidden := range []string{"root.status =", "prefix", "token_hash", "project_managers", "team_memberships"} {
				if strings.Contains(sql, forbidden) {
					t.Fatal("history revoked/borrowed", forbidden)
				}
			}
			q = personalKeyQuotaWarningMutationQuery(db, access).Where("read_at IS NULL").Update("read_at", nil)
			if q.Error != nil || !strings.Contains(q.Statement.SQL.String(), "EXISTS") {
				t.Fatal("mark-all unscoped", q.Error)
			}
			q = personalKeyQuotaWarningInboxQuery(db, quotaInboxAccess{ActorID: access.ActorID, Operational: true}).Find(&rows)
			if !strings.Contains(q.Statement.SQL.String(), "1 = 0") {
				t.Fatal("missing birth gained history")
			}
			var keys []entity.APIKey
			q = personalKeyWarningChainQuery(db, access.ActorID).Find(&keys)
			sql = q.Statement.SQL.String()
			l := q.Statement.Clauses["LIMIT"].Expression.(clause.Limit)
			if q.Error != nil || l.Limit == nil || *l.Limit != 10001 || !strings.Contains(sql, "user_id =") || !strings.Contains(sql, "ORDER BY") || strings.Contains(sql, "token_hash") || strings.Contains(sql, "prefix") {
				t.Fatal("chain lost indexed complete cap/secret exclusion", sql, q.Error)
			}
			if d.Name() == "mysql" && !strings.Contains(sql, "AS BINARY") {
				t.Fatal("collation exact guard missing")
			}
			var policies []entity.ResourceLimit
			q = personalKeyQuotaWarningScanQuery(db, quotaNotificationCursor{ID: "key_cursor"}).Find(&policies)
			if q.Error != nil || !strings.Contains(q.Statement.SQL.String(), "scope_id >") || *q.Statement.Clauses["LIMIT"].Expression.(clause.Limit).Limit != 32 {
				t.Fatal("fair scan unbounded")
			}
		})
	}
}
func TestPersonalKeyWarningCompleteChainSingleQuery(t *testing.T) {
	for _, n := range []int{500, 501, 1000, 10000} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			_, root, owner, _ := personalKeyWarningFixture()
			keys := make([]entity.APIKey, n)
			for i := range keys {
				keys[i] = root
				keys[i].ID = fmt.Sprintf("key_%026d", i)
			}
			state := &personalKeyChainSQLState{keys: keys}
			db := personalKeyChainSQLDatabase(t, state)
			var got []entity.APIKey
			start := time.Now()
			if err := personalKeyWarningChainQuery(db, owner.ID).Find(&got).Error; err != nil {
				t.Fatal(err)
			}
			elapsed := time.Since(start)
			if len(got) != n || state.queries != 1 || state.writes != 0 {
				t.Fatal("owner graph N+1/truncated", n, len(got), state.queries, state.writes)
			}
			if _, ok := personalKeyWarningRoots(got, owner.ID); !ok {
				t.Fatal("complete selected rows unproven")
			}
			t.Logf("single GORM query %d rows: %s", n, elapsed)
			if elapsed > 3*time.Second {
				t.Fatal("source hydration exceeded refresh deadline")
			}
		})
	}
}

func TestPersonalKeyWarningMissingHistoryAndFairCancellation(t *testing.T) {
	state := &personalKeyWarningSQLState{}
	db := personalKeyWarningSQLDatabase(t, state)
	_, access := personalKeyWarningInboxFixture()
	rows, unread, err := personalKeyQuotaWarningPage(db, access, NotificationFilter{}, 20, time.Time{}, "")
	if err != nil || len(rows) != 0 || unread != 0 {
		t.Fatal("missing history manufactured a row/count", err)
	}
	_, found, err := markPersonalKeyQuotaWarningRead(db, access, "kwi_missing")
	if err == nil || found || state.writes != 0 {
		t.Fatal("missing mark became success/mutation", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &Service{db: db}
	if err = s.reconcileMonthlyPersonalKeyQuotaWarnings(ctx); err == nil || state.writes != 0 {
		t.Fatal("canceled scan proceeded", err)
	}
	cursor := quotaNotificationCursor{}
	calls := 0
	batch := make([]entity.ResourceLimit, 32)
	for i := range batch {
		batch[i] = entity.ResourceLimit{ScopeKind: "key", ScopeID: fmt.Sprintf("key_%026d", i)}
	}
	done, err := reconcileQuotaNotificationRows(context.Background(), batch, &cursor, func(context.Context, string, string) error { calls++; return nil })
	if done || err != nil || calls != 32 || cursor.ID != batch[31].ScopeID {
		t.Fatal("bounded batch starved/failed to advance")
	}
	if err = s.observeMonthlyPersonalKeyQuotaWarning(context.Background(), "project", "key_00000000000000000000000001"); err != nil {
		t.Fatal("Project scope entered Personal observer")
	}
}
