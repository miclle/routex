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

// The normal dual-driver harness owns connection, empty startup, and ordered
// migration registration. This scenario reconstructs only the additive V66 seam.
func testTeamCreationModelsMigration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var baseline []int
	if err := db.Table("schema_migrations").Order("version").Pluck("version", &baseline).Error; err != nil {
		t.Fatal(err)
	}
	without := []int{}
	n := 0
	for i, v := range baseline {
		if i > 0 && v <= baseline[i-1] {
			t.Fatal("unordered migration baseline")
		}
		if v == 66 {
			n++
		} else {
			without = append(without, v)
		}
	}
	if n != 1 {
		t.Fatal("V66 must be registered exactly once")
	}
	ledger := func(want []int) {
		t.Helper()
		var got []int
		if err := db.Table("schema_migrations").Order("version").Pluck("version", &got).Error; err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("unrelated migration ledger changed", err, got, want)
		}
	}
	remove := func() {
		t.Helper()
		r := db.Table("schema_migrations").Where("version = ?", 66).Delete(&struct{}{})
		if r.Error != nil || r.RowsAffected != 1 {
			t.Fatal("candidate ledger removal", r.Error)
		}
		ledger(without)
	}
	migrate := func() {
		t.Helper()
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
		ledger(baseline)
	}
	concurrent := func() {
		t.Helper()
		var wg sync.WaitGroup
		failures := make(chan error, 2)
		for range 2 {
			wg.Go(func() { failures <- database.Migrate(ctx, db) })
		}
		wg.Wait()
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		ledger(baseline)
	}
	stamp := time.Date(2026, 6, 7, 8, 9, 10, 123456000, time.UTC)
	teams, models := teamCreationModelsMigrationFixtureParents(stamp)
	for i := range teams {
		if err := db.Create(&teams[i]).Error; err != nil {
			t.Fatal("grant Team parent setup", err)
		}
		if err := db.Take(&teams[i], "id = ?", teams[i].ID).Error; err != nil {
			t.Fatal("grant Team parent readback", err)
		}
	}
	for i := range models {
		if err := db.Create(&models[i]).Error; err != nil {
			t.Fatal("grant Model parent setup", err)
		}
		if err := db.Take(&models[i], "id = ?", models[i].ID).Error; err != nil {
			t.Fatal("grant Model parent readback", err)
		}
	}
	legacy := entity.TeamCreationReceipt{CreationID: "66000000-0000-4000-8000-000000000001", ActorID: "usr_deleted_66", ActorCreatedAt: stamp, TeamID: "tea_deleted_66", TeamCreatedAt: stamp, RequestHash: strings.Repeat("a", 64), ReviewETag: strings.Repeat("b", 64), SnapshotJSON: `{"historical":"unchanged"}`, CreatedAt: stamp}
	requestID := "tmr_history66"
	grant := entity.TeamModelGrant{TeamID: teams[0].ID, ModelID: models[0].ID, SourceRequestID: &requestID}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&grant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&legacy, "creation_id = ?", legacy.CreationID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&grant, "team_id = ? AND model_id = ?", grant.TeamID, grant.ModelID).Error; err != nil {
		t.Fatal(err)
	}
	// Remove only V66-owned objects. Preserve frozen V63 constraints and all
	// historical parent data; MySQL partial DDL is repaired explicitly.
	if err := db.Migrator().DropTable(&entity.TeamCreationReceiptModel{}); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		model any
		name  string
	}{{&entity.TeamCreationReceipt{}, "ck_team_creation_models"}, {&entity.TeamModelGrant{}, "ck_team_model_creation_source"}} {
		if err := db.Migrator().DropConstraint(item.model, item.name); err != nil {
			t.Fatal(err)
		}
	}
	for _, field := range []string{"ModelDigest", "ModelCount", "ModelSnapshotVersion"} {
		if err := db.Migrator().DropColumn(&entity.TeamCreationReceipt{}, field); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrator().DropColumn(&entity.TeamModelGrant{}, "SourceCreationReceiptID"); err != nil {
		t.Fatal(err)
	}
	remove()
	concurrent()
	migrate()
	preserved := func() {
		t.Helper()
		for _, original := range teams {
			var current entity.Team
			if err := db.Take(&current, "id = ?", original.ID).Error; err != nil || !teamCreationFixtureHistoryEqual(current, original) {
				t.Fatal("grant Team parent changed", err)
			}
		}
		for _, original := range models {
			var current entity.Model
			if err := db.Take(&current, "id = ?", original.ID).Error; err != nil || !teamCreationFixtureHistoryEqual(current, original) {
				t.Fatal("grant Model parent changed", err)
			}
		}
		var p entity.TeamCreationReceipt
		var g entity.TeamModelGrant
		if err := db.Take(&p, "creation_id = ?", legacy.CreationID).Error; err != nil || !teamCreationReceiptFixtureEqual(p, legacy) {
			t.Fatal("legacy receipt changed", err)
		}
		if err := db.Take(&g, "team_id = ? AND model_id = ?", grant.TeamID, grant.ModelID).Error; err != nil || !reflect.DeepEqual(g, grant) {
			t.Fatal("request grant provenance changed", err)
		}
	}
	preserved()
	for _, check := range []string{"ck_team_creation_intent", "ck_team_creation_snapshot", "ck_team_creation_models"} {
		if !db.Migrator().HasConstraint(&entity.TeamCreationReceipt{}, check) {
			t.Fatal("parent constraint missing", check)
		}
	}
	if !db.Migrator().HasConstraint(&entity.TeamModelGrant{}, "ck_team_model_creation_source") || !db.Migrator().HasConstraint(&entity.TeamCreationReceiptModel{}, "ck_team_creation_model_identity") {
		t.Fatal("additive constraint missing")
	}
	cols, err := db.Migrator().ColumnTypes(&entity.TeamCreationReceipt{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, col := range cols {
		if col.Name() == "model_digest" {
			nullable, known := col.Nullable()
			if known && !nullable {
				t.Fatal("empty legacy receipt digest must remain nullable")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("digest column missing")
	}
	digest := strings.Repeat("c", 64)
	original := legacy
	original.CreationID = "66000000-0000-4000-8000-000000000002"
	original.TeamID = "tea_child66"
	original.ModelSnapshotVersion = 1
	original.ModelCount = 1
	original.ModelDigest = &digest
	child := entity.TeamCreationReceiptModel{CreationID: original.CreationID, ModelID: "mdl_child66", ModelCreatedAt: stamp}
	if err := db.Create(&original).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&child).Error; err != nil {
		t.Fatal("receipt child cannot depend on a live Model", err)
	}
	if err := db.Take(&original, "creation_id = ?", original.CreationID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&child, "creation_id = ? AND model_id = ?", child.CreationID, child.ModelID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&child).Error; err == nil {
		t.Fatal("child composite uniqueness absent")
	}
	for i, modify := range []func(*entity.TeamCreationReceipt){func(p *entity.TeamCreationReceipt) { p.ModelDigest = nil }, func(p *entity.TeamCreationReceipt) { p.ModelCount = 0 }, func(p *entity.TeamCreationReceipt) { p.ModelCount = 1001 }, func(p *entity.TeamCreationReceipt) { p.ModelSnapshotVersion = 2 }, func(p *entity.TeamCreationReceipt) { s := "short"; p.ModelDigest = &s }, func(p *entity.TeamCreationReceipt) { p.ModelSnapshotVersion = 0 }} {
		bad := original
		bad.CreationID = "66000000-0000-4000-8000-0000000000" + teamCreationFixtureDecimal(10+i)
		bad.TeamID = "tea_bad66_" + teamCreationFixtureDecimal(i)
		modify(&bad)
		if err := db.Create(&bad).Error; err == nil {
			t.Fatal("invalid version/count/digest accepted", i)
		}
	}
	badGrant := entity.TeamModelGrant{TeamID: teams[1].ID, ModelID: models[1].ID, SourceCreationReceiptID: &requestID}
	if err := db.Create(&badGrant).Error; err == nil {
		t.Fatal("invalid creation source accepted")
	}
	// Partial schema repair preserves observation timestamps and original source.
	if err := db.Migrator().DropConstraint(&entity.TeamCreationReceiptModel{}, "ck_team_creation_model_identity"); err != nil {
		t.Fatal(err)
	}
	remove()
	concurrent()
	migrate()
	preserved()
	var got entity.TeamCreationReceiptModel
	if err := db.Take(&got, "creation_id = ? AND model_id = ?", child.CreationID, child.ModelID).Error; err != nil || !teamCreationFixtureHistoryEqual(got, child) {
		t.Fatal("child birth history changed", err)
	}
	var parent entity.TeamCreationReceipt
	if err := db.Take(&parent, "creation_id = ?", original.CreationID).Error; err != nil || !teamCreationReceiptFixtureEqual(parent, original) {
		t.Fatal("model snapshot changed", err)
	}
	var all []entity.TeamCreationReceiptModel
	if err := db.Find(&all).Error; err != nil || len(all) != 1 {
		t.Fatal("partial DDL or invalid writes changed children", err, len(all))
	}
	ledger(baseline)
}

// Existing V8 live-grant FKs differ from the historical V63/V66 receipt identity
// contract. Both the preserved request grant and malformed creation-source grant
// need live parents so V66 assertions cannot be masked by an older FK failure.
func teamCreationModelsMigrationFixtureParents(stamp time.Time) ([]entity.Team, []entity.Model) {
	return []entity.Team{
		{ID: "tea_grant66", Name: "Historical grant Team", Description: "Retained V44 grant parent", Status: entity.ResourceActive, CreatedAt: stamp, UpdatedAt: stamp},
		{ID: "tea_bad66", Name: "Creation source check Team", Description: "Valid parent for invalid provenance", Status: entity.ResourceActive, CreatedAt: stamp, UpdatedAt: stamp},
	}, []entity.Model{
		{ID: "mdl_grant66", Status: "active", CreatedAt: stamp},
		{ID: "mdl_bad66", Status: "active", CreatedAt: stamp},
	}
}

func TestTeamCreationModelsMigrationFixtureParents(t *testing.T) {
	stamp := time.Date(2026, 6, 7, 8, 9, 10, 123456000, time.UTC)
	teams, models := teamCreationModelsMigrationFixtureParents(stamp)
	if len(teams) != 2 || len(models) != 2 || teams[0].ID == teams[1].ID || models[0].ID == models[1].ID {
		t.Fatal("both grant cases require independent live parents")
	}
	for _, team := range teams {
		if team.ID == "tea_deleted_66" || team.Status != entity.ResourceActive || !team.CreatedAt.Equal(stamp) || !team.UpdatedAt.Equal(stamp) {
			t.Fatal("grant setup must not recreate deleted receipt Team history")
		}
	}
	for _, model := range models {
		if model.ID == "mdl_deleted_66" || model.ID == "mdl_child66" || !model.CreatedAt.Equal(stamp) {
			t.Fatal("grant setup must not recreate deleted receipt Model history")
		}
	}
}
