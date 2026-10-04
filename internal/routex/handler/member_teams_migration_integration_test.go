package handler

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
)

// This private fixture can still read/write the complete released relationship
// while the future nullable descriptive column is absent.
type memberTeamsHistoricalFixture struct {
	ID     string `gorm:"primaryKey;size:30"`
	TeamID string `gorm:"size:30;not null;uniqueIndex:uq_team_member"`
	UserID string `gorm:"size:30;not null;uniqueIndex:uq_team_member"`
	Role   string `gorm:"size:20;not null"`
	Status string `gorm:"size:20;not null"`
}

func (memberTeamsHistoricalFixture) TableName() string { return "team_memberships" }

type memberTeamsJoinedAtFixture struct {
	ID       string     `gorm:"primaryKey;size:30"`
	JoinedAt *time.Time `json:"-"`
}

func (memberTeamsJoinedAtFixture) TableName() string { return "team_memberships" }

// Called only by the shared real-driver harness after V53 integration.
func testMemberTeamsMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	column := &memberTeamsJoinedAtFixture{}
	if !db.Migrator().HasColumn(column, "JoinedAt") {
		t.Fatal("fresh V53 omitted nullable joined time")
	}
	newID := func(prefix string) string {
		t.Helper()
		value, err := id.NewPrefixed(prefix)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	team := entity.Team{ID: newID("tea"), Name: "Historical Team", Description: "Preserved description", Status: entity.ResourceDisabled, CreatedAt: stamp, UpdatedAt: stamp}
	user := entity.User{ID: newID("usr"), Email: "member-teams-migration@example.invalid", Name: "Historical member", PasswordHash: "historical-fixture-only", Role: entity.RoleMember, Disabled: true, CreatedAt: stamp, UpdatedAt: stamp}
	for _, row := range []any{&team, &user} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	membership := memberTeamsHistoricalFixture{ID: newID("tmm"), TeamID: team.ID, UserID: user.ID, Role: entity.TeamMember, Status: entity.ResourceDisabled}
	if err := db.Create(&membership).Error; err != nil {
		t.Fatal(err)
	}
	read := func() entity.TeamMembership {
		t.Helper()
		var current entity.TeamMembership
		if err := db.Select("id", "team_id", "user_id", "role", "status", "joined_at").First(&current, "id = ?", membership.ID).Error; err != nil {
			t.Fatal(err)
		}
		return current
	}
	// Compare against complete persisted values, including driver timestamp precision.
	var baselineTeam entity.Team
	var baselineUser entity.User
	if err := db.First(&baselineTeam, "id = ?", team.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&baselineUser, "id = ?", user.ID).Error; err != nil {
		t.Fatal(err)
	}
	baseline := read()
	if baseline.JoinedAt != nil {
		t.Fatal("fresh default invented historical join")
	}
	removeLedger := func() {
		t.Helper()
		result := db.Table("schema_migrations").Where("version = ?", 53).Delete(&struct{}{})
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("cannot reconstruct V52", result.RowsAffected, result.Error)
		}
	}
	concurrent := func() {
		t.Helper()
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for range 2 {
			wg.Go(func() { results <- database.Migrate(ctx, db) })
		}
		wg.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatal("concurrent nullable migration failed", err)
			}
		}
	}
	assertHistory := func(want entity.TeamMembership) {
		t.Helper()
		current := read()
		if !reflect.DeepEqual(current, want) {
			t.Fatal("migration rewrote retained identity, owner, role, status or join", current, want)
		}
		var currentTeam entity.Team
		var currentUser entity.User
		if err := db.First(&currentTeam, "id = ?", team.ID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.First(&currentUser, "id = ?", user.ID).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(currentTeam, baselineTeam) || !reflect.DeepEqual(currentUser, baselineUser) {
			t.Fatal("descriptive column changed surrounding historical data")
		}
	}
	removeLedger()
	if err := db.Migrator().DropColumn(column, "JoinedAt"); err != nil {
		t.Fatal(err)
	}
	var historical memberTeamsHistoricalFixture
	if err := db.Select("id", "team_id", "user_id", "role", "status").First(&historical, "id = ?", membership.ID).Error; err != nil || !reflect.DeepEqual(historical, membership) {
		t.Fatal("fixture reconstruction changed historical fields", historical, err)
	}
	concurrent()
	assertHistory(baseline)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	assertHistory(baseline)
	// Already-created nullable DDL and a recorded date survive incomplete ledger.
	recorded := baselineTeam.UpdatedAt.UTC().Add(time.Minute)
	if err := db.Model(column).Where("id = ?", membership.ID).Update("joined_at", recorded).Error; err != nil {
		t.Fatal(err)
	}
	withDate := read()
	if withDate.JoinedAt == nil || !withDate.JoinedAt.Equal(recorded) {
		t.Fatal("fixture failed to retain explicit timestamp")
	}
	removeLedger()
	concurrent()
	assertHistory(withDate)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	assertHistory(withDate)
	// The existing unique pair constraint must remain intact.
	duplicate := membership
	duplicate.ID = newID("tmm")
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("joined-at migration weakened unique ownership pair")
	}
	if err := db.Model(column).Where("id = ?", membership.ID).Update("joined_at", nil).Error; err != nil {
		t.Fatal("historical unknown is no longer representable", err)
	}
	assertHistory(baseline)
	if err := db.Delete(&membership).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&team).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&user).Error; err != nil {
		t.Fatal(err)
	}
}
func TestMemberTeamsMigrationFixtureIsHarnessOwned(t *testing.T) {
	if reflect.ValueOf(testMemberTeamsMigration).IsNil() {
		t.Fatal("missing V53 fixture")
	}
}
