package service

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
)

func TestMemberMetadataInputRejectsAmbiguousIntent(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `[]`, `{"name":"A","reason":null}`, `{"name":null,"reason":"R"}`, `{"Name":"A","reason":"R"}`, `{"name":"A","reason":"R","email":"private"}`, `{"name":"A","reason":"R","name":"B"}`, `{"name":"A","reason":"R","\u006eame":"B"}`, `{"name":"A","reason":"R"} {}`, `{"name":"\ud800","reason":"R"}`, `{"name":"A","reason":"\udfff"}`, `{"name":"A","reason":" R"}`, `{"name":"A\n","reason":"R"}`, `{"name":" A","reason":"R"}`, `{"name":"A","reason":""}`, "{\"name\":\"\xff\",\"reason\":\"R\"}"} {
		t.Run(raw, func(t *testing.T) {
			var input MemberMetadataInput
			if json.Unmarshal([]byte(raw), &input) == nil {
				t.Fatal("ambiguous intent accepted")
			}
		})
	}
	for _, input := range []MemberMetadataInput{{strings.Repeat("😀", 100), strings.Repeat("r", 1024)}, {"Repair", "R"}, {"O'Neil", "Reason"}} {
		raw, _ := json.Marshal(input)
		var got MemberMetadataInput
		if err := json.Unmarshal(raw, &got); err != nil || got != input {
			t.Fatal(input, got, err)
		}
	}
	for _, input := range []MemberMetadataInput{{strings.Repeat("😀", 101), "R"}, {"A", strings.Repeat("r", 1025)}} {
		raw, _ := json.Marshal(input)
		var got MemberMetadataInput
		if json.Unmarshal(raw, &got) == nil {
			t.Fatal("bound accepted")
		}
	}
	// Escaped literal backslashes do not become surrogate escapes.
	var input MemberMetadataInput
	if json.Unmarshal([]byte(`{"name":"\\ud800","reason":"R"}`), &input) != nil {
		t.Fatal("literal backslash rejected")
	}
}
func TestMemberMetadataProtectedStateAndCurrentRetry(t *testing.T) {
	now := time.Now().UTC()
	admin := entity.User{ID: "usr_admin", Role: entity.RoleAdmin}
	writer := entity.User{ID: "usr_writer", Role: entity.RoleMember}
	target := entity.User{ID: "usr_target", Role: entity.RoleMember, Name: "A", CreatedAt: now, UpdatedAt: now}
	review := memberMetadataRecord(admin, target, true)
	if !review.CanEdit || review.Status != "active" || len(review.ETag) != 64 {
		t.Fatal(review)
	}
	if memberMetadataReview(admin, target, MemberMetadataInput{Name: "B", Reason: "R"}, review.ETag) != nil {
		t.Fatal("fresh denied")
	}
	if memberMetadataReview(admin, target, MemberMetadataInput{Name: "B", Reason: "R"}, strings.Repeat("b", 64)) != catalogConflict {
		t.Fatal("stale different accepted")
	}
	if memberMetadataReview(admin, target, MemberMetadataInput{Name: "A", Reason: "R"}, strings.Repeat("b", 64)) != nil {
		t.Fatal("current match failed")
	}
	target.Disabled = true
	if !memberMetadataRecord(writer, target, true).CanEdit || memberMetadataRecord(writer, target, true).Status != "disabled" {
		t.Fatal("disabled label trapped")
	}
	target.OffboardedAt = &now
	if memberMetadataRecord(admin, target, true).CanEdit || memberMetadataReview(admin, target, MemberMetadataInput{Name: "A", Reason: "R"}, review.ETag) != catalogConflict {
		t.Fatal("terminal matching write")
	}
	target.OffboardedAt = nil
	target.ID = writer.ID
	if memberMetadataEditable(writer, target, true) {
		t.Fatal("delegated self")
	}
	target.ID = "usr_target"
	target.Role = entity.RoleAdmin
	if memberMetadataEditable(writer, target, true) {
		t.Fatal("delegated administrator")
	}
	if memberMetadataEditable(admin, target, false) {
		t.Fatal("role bypass")
	}
	admin.Disabled = true
	if memberMetadataEditable(admin, target, true) {
		t.Fatal("disabled actor")
	}
}
func TestMemberMetadataReviewIdentityAndPrivateProjection(t *testing.T) {
	now := time.Now().UTC()
	actor := entity.User{ID: "usr_admin", Role: entity.RoleAdmin}
	target := entity.User{ID: "usr_target", Name: "Legacy\nname", Role: entity.RoleMember, CreatedAt: now, UpdatedAt: now}
	base := memberMetadataRecord(actor, target, true)
	for _, change := range []func(*entity.User){func(u *entity.User) { u.ID = "USR_target" }, func(u *entity.User) { u.Name = "Later" }, func(u *entity.User) { u.Role = entity.RoleAdmin }, func(u *entity.User) { u.Disabled = true }, func(u *entity.User) { u.CreatedAt = u.CreatedAt.Add(time.Second) }, func(u *entity.User) { u.UpdatedAt = u.UpdatedAt.Add(time.Second) }, func(u *entity.User) { u.OffboardedAt = &now }} {
		next := target
		change(&next)
		if memberMetadataRecord(actor, next, true).ETag == base.ETag {
			t.Fatal("review omitted identity/lifecycle")
		}
	}
	next := target
	next.PasswordHash = "different"
	next.Email = "private@example.invalid"
	if memberMetadataRecord(actor, next, true).ETag != base.ETag {
		t.Fatal("private fields in review")
	}
	next = target
	next.CreatedAt = now.In(time.FixedZone("offset", 3600))
	next.UpdatedAt = next.CreatedAt
	if memberMetadataRecord(actor, next, true).ETag != base.ETag {
		t.Fatal("UTC normalization")
	}
	if memberMetadataRecord(entity.User{ID: "usr_other", Role: entity.RoleAdmin}, target, true).ETag == base.ETag || memberMetadataRecord(actor, target, false).ETag == base.ETag {
		t.Fatal("actor/editability not bound")
	}
	raw, _ := json.Marshal(MemberMetadataWriteResult{MemberMetadataRecord: base, Confirmation: "current_member_name"})
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	if len(fields) != 6 || fields["name"] != target.Name || fields["confirmation"] != "current_member_name" {
		t.Fatal(string(raw))
	}
}
func TestMemberMetadataExactSQLAndOnlyNameUpdate(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		t.Run(dialect.Name(), func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
			if err != nil {
				t.Fatal(err)
			}
			origin := db.Clauses(clause.Locking{Strength: "UPDATE"})
			for _, userID := range []string{"usr_actor", "usr_target", "usr_actor"} {
				query := memberMetadataUserQuery(origin, userID)
				if err := query.Statement.Parse(&entity.User{}); err != nil {
					t.Fatal(err)
				}
				query.Statement.BuildClauses = []string{"SELECT", "FROM", "WHERE", "FOR"}
				callbacks.BuildQuerySQL(query)
				if !reflect.DeepEqual(query.Statement.Vars, []any{userID}) || !strings.Contains(query.Statement.SQL.String(), "users") {
					t.Fatal(query.Statement.SQL.String(), query.Statement.Vars)
				}
				for _, private := range []string{"password_hash", "email", "personal_grant_revision"} {
					if strings.Contains(query.Statement.SQL.String(), private) {
						t.Fatal("private read", private)
					}
				}
				if dialect.Name() == "mysql" && strings.Count(query.Statement.SQL.String(), "AS BINARY") != 2 {
					t.Fatal("inexact", query.Statement.SQL.String())
				}
			}
			if origin.Statement.Model != nil || origin.Statement.Clauses["WHERE"].Expression != nil {
				t.Fatal("clone bleed")
			}
			if err := db.Callback().Update().Register("test:metadata-update-sql", func(query *gorm.DB) {
				query.Statement.BuildClauses = []string{"UPDATE", "SET", "WHERE"}
				callbacks.Update(&callbacks.Config{})(query)
			}); err != nil {
				t.Fatal(err)
			}
			update := memberMetadataUserQuery(db, "usr_target").Updates(map[string]any{"name": "Changed"})
			sql := update.Statement.SQL.String()
			if update.Error != nil || !strings.Contains(sql, "name") || !strings.Contains(sql, "updated_at") {
				t.Fatal(sql, update.Error)
			}
			for _, private := range []string{"email", "password_hash", "disabled", "role", "offboarded_at", "created_at", "personal_grant_revision"} {
				if strings.Contains(sql, private) {
					t.Fatal("updated unrelated field", sql)
				}
			}
		})
	}
}
func TestMemberMetadataSanitizedAuditAndBeforeDatabase(t *testing.T) {
	detail := `{"user_id":"usr_target","before_name":"Legacy\nname","after_name":"Repaired","reason":"Reviewed"}`
	row := entity.AuditEvent{Action: "member.metadata.update", ResourceType: "user", ResourceID: "usr_target", DetailsJSON: &detail}
	got := auditRecord(row)
	if len(got.Changes) == 0 || strings.Contains(string(got.Changes), "password") {
		t.Fatal(got)
	}
	for _, bad := range []string{`{"user_id":"usr_foreign","before_name":"A","after_name":"B","reason":"R"}`, `{"user_id":"usr_target","before_name":"A","after_name":"B","reason":"R","password":"secret"}`, `{"user_id":"usr_target","before_name":"A","after_name":"B","reason":""}`} {
		row.DetailsJSON = &bad
		if len(auditRecord(row).Changes) != 0 {
			t.Fatal("untrusted audit exposed")
		}
	}
	s := &Service{}
	if _, err := s.GetMemberMetadata(context.Background(), "usr_actor", "target/"); err != apperrors.ErrBadRequest {
		t.Fatal(err)
	}
	if _, err := s.SetMemberMetadata(context.Background(), "bad actor", "usr_target", strings.Repeat("a", 64), MemberMetadataInput{Name: "B", Reason: "R"}); err != apperrors.ErrUnauthorized {
		t.Fatal(err)
	}
	if _, err := s.SetMemberMetadata(context.Background(), "usr_actor", "usr_target", "bad", MemberMetadataInput{Name: "B", Reason: "R"}); err != apperrors.ErrBadRequest {
		t.Fatal(err)
	}
}
