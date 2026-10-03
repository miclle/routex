package handler

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

// The partial schema intentionally omits indexes/checks to reproduce committed
// MySQL table DDL without using DropIndex's PostgreSQL adapter exception.
type projectCreationReceiptPartialV45Fixture struct {
	CreationID   string    `gorm:"primaryKey;size:36"`
	ActorID      string    `gorm:"size:30;not null"`
	ProjectID    string    `gorm:"size:30;not null"`
	RequestHash  string    `gorm:"size:64;not null"`
	ReviewETag   string    `gorm:"column:review_etag;size:64;not null"`
	SnapshotJSON string    `gorm:"type:text;not null"`
	CreatedAt    time.Time `gorm:"precision:6;not null"`
}

func (projectCreationReceiptPartialV45Fixture) TableName() string {
	return "project_creation_receipts"
}

type projectCreationReceiptV45Fixture struct {
	CreationID   string    `gorm:"primaryKey;size:36;check:ck_project_creation_intent,CHAR_LENGTH(creation_id) = 36 AND CHAR_LENGTH(request_hash) = 64 AND CHAR_LENGTH(review_etag) = 64"`
	ActorID      string    `gorm:"size:30;not null;index:idx_project_creation_actor"`
	ProjectID    string    `gorm:"size:30;not null;uniqueIndex:uq_project_creation_project"`
	RequestHash  string    `gorm:"size:64;not null"`
	ReviewETag   string    `gorm:"column:review_etag;size:64;not null"`
	SnapshotJSON string    `gorm:"type:text;not null"`
	CreatedAt    time.Time `gorm:"precision:6;not null"`
}

func (projectCreationReceiptV45Fixture) TableName() string { return "project_creation_receipts" }

func testProjectInitialResourcesMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	user := entity.User{ID: "usr_v45_upgrade", Email: "v45-upgrade@example.invalid", Name: "Historical creator", Role: entity.RoleMember, PasswordHash: "test-only-historical-hash", CreatedAt: stamp, UpdatedAt: stamp}
	project := entity.Project{ID: "prj_v45_upgrade", Name: "Historical Project", Description: "Preserved application", Status: entity.ResourceActive, CreatorID: user.ID, CreatedAt: stamp, UpdatedAt: stamp}
	manager := entity.ProjectManager{ID: "pmg_v45_upgrade", ProjectID: project.ID, UserID: user.ID}
	model := entity.Model{ID: "mdl_v45_upgrade", Status: entity.ResourceActive, CreatedAt: stamp}
	grant := entity.ProjectModelGrant{ProjectID: project.ID, ModelID: model.ID}
	tokens, rpm, money := int64(17), int64(12), "100.000000000000000001"
	policy := entity.ResourceLimit{ScopeKind: "project", ScopeID: project.ID, ETag: strings.Repeat("e", 64), PreviousETag: "0", ActorID: user.ID, Reason: "Historical policy", TokensMonth: &tokens, MoneyMonth: &money, Currency: "USD", RPM: &rpm, IPMode: "none", IPRangesJSON: "[]", UpdatedAt: stamp}
	for _, row := range []any{&user, &project, &manager, &model, &grant, &policy} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	readExisting := func() []any {
		t.Helper()
		var currentUser entity.User
		var currentProject entity.Project
		var currentManager entity.ProjectManager
		var currentModel entity.Model
		var currentGrant entity.ProjectModelGrant
		var currentPolicy entity.ResourceLimit
		for _, query := range []*gorm.DB{
			db.Take(&currentUser, "id = ?", user.ID), db.Take(&currentProject, "id = ?", project.ID),
			db.Take(&currentManager, "id = ?", manager.ID), db.Take(&currentModel, "id = ?", model.ID),
			db.Take(&currentGrant, "project_id = ? AND model_id = ?", project.ID, model.ID),
			db.Take(&currentPolicy, "scope_kind = ? AND scope_id = ?", "project", project.ID),
		} {
			if query.Error != nil {
				t.Fatal(query.Error)
			}
		}
		return []any{currentUser, currentProject, currentManager, currentModel, currentGrant, currentPolicy}
	}
	baseline := readExisting()
	defer func() {
		for _, query := range []*gorm.DB{
			db.Where("actor_id = ?", "usr_v45_missing").Delete(&entity.ProjectCreationReceipt{}),
			db.Where("scope_kind = ? AND scope_id = ?", "project", project.ID).Delete(&entity.ResourceLimit{}),
			db.Where("project_id = ?", project.ID).Delete(&entity.ProjectModelGrant{}),
			db.Delete(&manager), db.Delete(&project), db.Delete(&model), db.Delete(&user),
		} {
			if query.Error != nil {
				t.Error("clean up owned V45 fixture", query.Error)
			}
		}
	}()
	newHistory := func() entity.ProjectCreationReceipt {
		return entity.ProjectCreationReceipt{CreationID: "45000000-1111-4111-8111-111111111111", ActorID: "usr_v45_missing", ProjectID: "prj_v45_missing", RequestHash: strings.Repeat("a", 64), ReviewETag: strings.Repeat("b", 64), SnapshotJSON: `{"recorded":"historical creation"}`, CreatedAt: stamp}
	}
	schema := &projectCreationReceiptV45Fixture{}
	for prefix := 0; prefix < 4; prefix++ {
		if err := db.Migrator().DropTable(schema); err != nil {
			t.Fatal(err)
		}
		if result := db.Table("schema_migrations").Where("version = ?", 45).Delete(&struct{}{}); result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("reconstruct V44 migration ledger", result.Error)
		}
		var before *entity.ProjectCreationReceipt
		if prefix > 0 {
			var table any = &projectCreationReceiptPartialV45Fixture{}
			if prefix == 3 {
				table = schema
			}
			if err := db.Migrator().CreateTable(table); err != nil {
				t.Fatal("create partial V45 table", prefix, err)
			}
			if prefix == 2 {
				if err := db.Migrator().CreateIndex(schema, "idx_project_creation_actor"); err != nil {
					t.Fatal(err)
				}
			}
			row := newHistory()
			if err := db.Create(&row).Error; err != nil {
				t.Fatal("historical receipt must survive absent live actor/Project", err)
			}
			if err := db.Take(&row, "creation_id = ?", row.CreationID).Error; err != nil {
				t.Fatal("read persisted migration receipt baseline", err)
			}
			before = &row
		}
		var wg sync.WaitGroup
		outcomes := make(chan error, 2)
		for range 2 {
			wg.Go(func() { outcomes <- database.Migrate(ctx, db) })
		}
		wg.Wait()
		close(outcomes)
		for err := range outcomes {
			if err != nil {
				t.Fatal("concurrent V45 prefix repair", prefix, err)
			}
		}
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal("repeat V45 repair", prefix, err)
		}
		for _, index := range []string{"idx_project_creation_actor", "uq_project_creation_project"} {
			if !db.Migrator().HasIndex(schema, index) {
				t.Fatal("V45 missing repaired receipt index", prefix, index)
			}
		}
		if !db.Migrator().HasConstraint(schema, "ck_project_creation_intent") {
			t.Fatal("V45 missing repaired receipt hash/review guards", prefix)
		}
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(schema); err != nil || len(statement.Schema.Relationships.Relations) != 0 {
			t.Fatal("historical V45 schema acquired live relationships", err)
		}
		if after := readExisting(); !reflect.DeepEqual(baseline, after) {
			t.Fatal("V45 upgrade changed existing Project identity, manager, grants or policy", prefix)
		}
		if before != nil {
			var after entity.ProjectCreationReceipt
			if err := db.Take(&after, "creation_id = ?", before.CreationID).Error; err != nil || !reflect.DeepEqual(*before, after) {
				t.Fatal("V45 repair changed immutable persisted history", prefix, err)
			}
		}
	}
	first := newHistory()
	for _, duplicate := range []entity.ProjectCreationReceipt{
		{CreationID: first.CreationID, ActorID: "usr_v45_other", ProjectID: "prj_v45_other"},
		{CreationID: "45000000-2222-4222-8222-222222222222", ActorID: first.ActorID, ProjectID: first.ProjectID},
	} {
		duplicate.RequestHash, duplicate.ReviewETag, duplicate.SnapshotJSON, duplicate.CreatedAt = first.RequestHash, first.ReviewETag, first.SnapshotJSON, stamp
		if err := db.Create(&duplicate).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
			t.Fatal("V45 globally unique UUID/Project did not reject duplicate portably", err)
		}
	}
	// The same actor can create another independent Project intent.
	second := first
	second.CreationID, second.ProjectID = "45000000-3333-4333-8333-333333333333", "prj_v45_second_missing"
	if err := db.Create(&second).Error; err != nil {
		t.Fatal("actor index incorrectly became unique", err)
	}
	for i, invalid := range []entity.ProjectCreationReceipt{
		{CreationID: "short"}, {RequestHash: strings.Repeat("c", 63)}, {ReviewETag: strings.Repeat("d", 63)},
	} {
		row := first
		row.CreationID, row.ProjectID = "45000000-4444-4444-8444-44444444444"+string(rune('0'+i)), "prj_v45_invalid_"+string(rune('a'+i))
		if invalid.CreationID != "" {
			row.CreationID = invalid.CreationID
		}
		if invalid.RequestHash != "" {
			row.RequestHash = invalid.RequestHash
		}
		if invalid.ReviewETag != "" {
			row.ReviewETag = invalid.ReviewETag
		}
		if err := db.Create(&row).Error; err == nil {
			t.Fatal("V45 accepted incomplete receipt identity/hash/review", i)
		}
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var after entity.ProjectCreationReceipt
	if err := db.Take(&after, "creation_id = ?", second.CreationID).Error; err != nil || after.ActorID != "usr_v45_missing" || after.ProjectID != second.ProjectID || after.SnapshotJSON != second.SnapshotJSON {
		t.Fatal("orphan historical receipt required live actor/Project after repeat", err)
	}
}
