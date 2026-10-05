package handler

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// A complete historical projection remains usable while V55's column is absent.
type recentLoginHistoricalUser struct {
	ID, Email, Name, PasswordHash, Role, PersonalGrantRevision string
	Disabled                                                   bool
	OffboardedAt                                               *time.Time
	CreatedAt, UpdatedAt                                       time.Time
}

func (recentLoginHistoricalUser) TableName() string { return "users" }

type recentLoginColumnFixture struct {
	ID          string     `gorm:"primaryKey;size:30"`
	LastLoginAt *time.Time `json:"-" gorm:"precision:6"`
}

func (recentLoginColumnFixture) TableName() string { return "users" }
func testMemberRecentLoginMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	column := &recentLoginColumnFixture{}
	if !db.Migrator().HasColumn(column, "LastLoginAt") {
		t.Fatal("fresh V55 missing nullable login column")
	}
	off := time.Now().UTC().Truncate(time.Microsecond)
	for i, id := range []string{"usr_login_history", "usr_login_disabled", "usr_login_offboarded"} {
		user := entity.User{ID: id, Email: id + "@example.invalid", Name: "Retained", Role: entity.RoleMember, PasswordHash: "historical-only-not-used", PersonalGrantRevision: strings.Repeat("0", 64), Disabled: i > 0}
		if i == 2 {
			user.OffboardedAt = &off
		}
		if err := db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
	}
	read := func() []recentLoginHistoricalUser {
		t.Helper()
		var rows []recentLoginHistoricalUser
		// Select the frozen historical columns explicitly: SELECT * would change
		// its prepared result shape across the deliberate column reconstruction.
		if err := db.Select("ID", "Email", "Name", "PasswordHash", "Role", "PersonalGrantRevision", "Disabled", "OffboardedAt", "CreatedAt", "UpdatedAt").Where("id IN ?", []string{"usr_login_history", "usr_login_disabled", "usr_login_offboarded"}).Order("id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		return rows
	}
	baseline := read()
	retainedSession := entity.Session{ID: "ses_login_history", UserID: "usr_login_history", TokenHash: strings.Repeat("a", 64), ExpiresAt: off.Add(time.Hour)}
	if err := db.Create(&retainedSession).Error; err != nil {
		t.Fatal(err)
	}
	readSession := func() entity.Session {
		t.Helper()
		var row entity.Session
		if err := db.First(&row, "id = ?", retainedSession.ID).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	sessionBaseline := readSession()
	removeLedger := func() {
		t.Helper()
		q := db.Table("schema_migrations").Where("version = ?", 55).Delete(&struct{}{})
		if q.Error != nil || q.RowsAffected != 1 {
			t.Fatal("V55 ledger reconstruction", q.Error, q.RowsAffected)
		}
	}
	migrate := func() {
		t.Helper()
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	removeLedger()
	if err := db.Migrator().DropColumn(column, "LastLoginAt"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(read(), baseline) {
		t.Fatal("pre-V55 history changed")
	}
	migrate()
	if !reflect.DeepEqual(read(), baseline) {
		t.Fatal("upgrade changed retained identity/history")
	}
	var rows []recentLoginColumnFixture
	if err := db.Where("id IN ?", []string{"usr_login_history", "usr_login_disabled", "usr_login_offboarded"}).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.LastLoginAt != nil {
			t.Fatal("historical login invented", row.ID)
		}
	}
	recorded := off.Add(time.Minute)
	if err := db.Model(column).Where("id = ?", "usr_login_history").UpdateColumn("LastLoginAt", recorded).Error; err != nil {
		t.Fatal(err)
	}
	removeLedger()
	migrate()
	migrate()
	removeLedger() // Existing exact column, interrupted ledger claim; both startups must converge.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- database.Migrate(ctx, db) })
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(readSession(), sessionBaseline) {
		t.Fatal("migration changed retained Session")
	}
	if !reflect.DeepEqual(read(), baseline) {
		t.Fatal("partial/repeated migration changed historical User")
	}
	rows = nil
	if err := db.Where("id IN ?", []string{"usr_login_history", "usr_login_disabled", "usr_login_offboarded"}).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == "usr_login_history" {
			if row.LastLoginAt == nil || !row.LastLoginAt.Equal(recorded) {
				t.Fatal("partial DDL retry lost recorded time")
			}
		} else if row.LastLoginAt != nil {
			t.Fatal("old NULL became synthetic login")
		}
	}
	cols, err := db.Migrator().ColumnTypes(column)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range cols {
		if c.Name() == "last_login_at" {
			found = true
			n, ok := c.Nullable()
			p, _, sized := c.DecimalSize()
			value, hasDefault := c.DefaultValue()
			if !ok || !n || !sized || p != 6 || hasDefault && value != "" && !strings.EqualFold(value, "NULL") {
				t.Fatal("incorrect nullable precision/default shape")
			}
		}
	}
	if !found {
		t.Fatal("missing column")
	}
}
