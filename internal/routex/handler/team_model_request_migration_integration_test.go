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
	"gorm.io/gorm/schema"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

type teamModelGrantProvenanceV44Fixture struct {
	SourceRequestID *string `gorm:"column:source_request_id;size:30;check:ck_team_model_grant_source,source_request_id IS NULL OR (CHAR_LENGTH(source_request_id) >= 1 AND CHAR_LENGTH(source_request_id) <= 30)"`
}

func (teamModelGrantProvenanceV44Fixture) TableName() string { return "team_model_grants" }

func testTeamModelRequestMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	team := entity.Team{ID: "tem_model_request_upgrade", Name: "Historical granted Team", Status: entity.ResourceActive, CreatedAt: stamp, UpdatedAt: stamp}
	model := entity.Model{ID: "mdl_model_request_upgrade", Status: entity.ResourceActive, CreatedAt: stamp}
	grant := entity.TeamModelGrant{TeamID: team.ID, ModelID: model.ID}
	for _, row := range []any{&team, &model, &grant} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Where("team_id = ? AND model_id = ?", grant.TeamID, grant.ModelID).Take(&grant).Error; err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, query := range []*gorm.DB{
			db.Where("team_id = ?", "tem_v44_missing").Delete(&entity.TeamModelRequestPendingSlot{}),
			db.Where("team_id = ?", "tem_v44_missing").Delete(&entity.TeamModelRequest{}),
			db.Where("team_id = ?", team.ID).Delete(&entity.TeamModelGrant{}),
			db.Delete(&model), db.Delete(&team),
		} {
			if query.Error != nil {
				t.Error("clean up owned V44 fixture", query.Error)
			}
		}
	}()
	provenance := &teamModelGrantProvenanceV44Fixture{}
	tables := []any{&entity.TeamModelRequest{}, &entity.TeamModelRequestPendingSlot{}}
	newHistory := func() entity.TeamModelRequest {
		return entity.TeamModelRequest{
			ID: "tmr_v44_historical", RequestID: "11111111-1111-4111-8111-111111111143",
			RequestHash: strings.Repeat("a", 64), ApplicantUserID: "usr_v44_missing", ApplicantName: "Recorded applicant", TeamID: "tem_v44_missing", TeamName: "Recorded Team", ApplicantMembershipID: "tmm_v44_missing",
			ModelID: "mdl_v44_missing", ModelName: "Recorded model", ReviewETag: strings.Repeat("b", 64),
			Reason: "Preserve submitted history", Status: entity.TeamModelRequestPending, CreatedAt: stamp, UpdatedAt: stamp,
		}
	}
	repairSchema := func() {
		t.Helper()
		for _, table := range tables {
			statement := &gorm.Statement{DB: db}
			if err := statement.Parse(table); err != nil {
				t.Fatal(err)
			}
			for name := range statement.Schema.ParseCheckConstraints() {
				if !db.Migrator().HasConstraint(table, name) {
					t.Fatal("missing repaired request guard", name)
				}
			}
			for _, index := range statement.Schema.ParseIndexes() {
				if !db.Migrator().HasIndex(table, index.Name) {
					t.Fatal("missing repaired request index", index.Name)
				}
			}
			if len(statement.Schema.Relationships.Relations) != 0 {
				t.Fatal("historical requests must not acquire live foreign keys")
			}
		}
		if !db.Migrator().HasConstraint(provenance, "ck_team_model_grant_source") || !db.Migrator().HasColumn(provenance, "SourceRequestID") {
			t.Fatal("missing nullable grant provenance repair")
		}

	}
	for prefix := 0; prefix <= 4; prefix++ {
		if db.Migrator().HasConstraint(provenance, "ck_team_model_grant_source") {
			if err := db.Migrator().DropConstraint(provenance, "ck_team_model_grant_source"); err != nil {
				t.Fatal(err)
			}
		}
		if db.Migrator().HasColumn(provenance, "SourceRequestID") {
			if err := db.Migrator().DropColumn(provenance, "SourceRequestID"); err != nil {
				t.Fatal(err)
			}
		}
		for _, table := range tables {
			if err := db.Migrator().DropTable(table); err != nil {
				t.Fatal(err)
			}
		}
		if result := db.Table("schema_migrations").Where("version = ?", 44).Delete(&struct{}{}); result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("reconstruct V43 ledger", result.Error)
		}
		var historical *entity.TeamModelRequest
		if prefix > 0 {
			if err := db.Migrator().CreateTable(tables[0]); err != nil {
				t.Fatal(err)
			}
			value := newHistory()
			if err := db.Create(&value).Error; err != nil {
				t.Fatal("request history must survive absent current applicant/model", err)
			}
			if err := db.Take(&value, "id = ?", value.ID).Error; err != nil {
				t.Fatal(err)
			}
			historical = &value
		}
		if prefix > 1 {
			if err := db.Migrator().CreateTable(tables[1]); err != nil {
				t.Fatal(err)
			}
			// Reconstruct committed table DDL with missing checks and indexes,
			// independently of transactional DDL support on either driver.
			for _, table := range tables {
				statement := &gorm.Statement{DB: db}
				if err := statement.Parse(table); err != nil {
					t.Fatal(err)
				}
				for name := range statement.Schema.ParseCheckConstraints() {
					if err := db.Migrator().DropConstraint(table, name); err != nil {
						t.Fatal(err)
					}
				}
				for _, index := range statement.Schema.ParseIndexes() {
					dropTeamModelRequestFixtureIndex(t, db, table, index.Name)
				}
			}
		}
		if prefix > 2 {
			if err := db.Migrator().AddColumn(provenance, "SourceRequestID"); err != nil {
				t.Fatal(err)
			}
			if db.Migrator().HasConstraint(provenance, "ck_team_model_grant_source") {
				if err := db.Migrator().DropConstraint(provenance, "ck_team_model_grant_source"); err != nil {
					t.Fatal(err)
				}
			}
		}
		if prefix > 3 {
			if err := db.Migrator().CreateConstraint(provenance, "ck_team_model_grant_source"); err != nil {
				t.Fatal(err)
			}
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
				t.Fatal("concurrent V44 prefix repair", prefix, err)
			}
		}
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal("repeat V44", err)
		}
		repairSchema()
		var after entity.TeamModelGrant
		if err := db.Where("team_id = ? AND model_id = ?", grant.TeamID, grant.ModelID).Take(&after).Error; err != nil || !reflect.DeepEqual(grant, after) {
			t.Fatal("upgrade changed legacy Team authorization or unknown provenance", after, err)
		}
		if historical != nil {
			var after entity.TeamModelRequest
			if err := db.Take(&after, "id = ?", historical.ID).Error; err != nil || !reflect.DeepEqual(*historical, after) {
				t.Fatal("prefix repair rewrote immutable submitted request evidence", err)
			}
		}
	}
	var original entity.TeamModelRequest
	if err := db.Take(&original, "id = ?", "tmr_v44_historical").Error; err != nil {
		t.Fatal(err)
	}
	second := newHistory()
	second.ApplicantUserID = "usr_v44_other_applicant"
	second.ApplicantMembershipID = "tmm_v44_other_applicant"
	second.ID, second.RequestID = "tmr_v44_second_pending", "22222222-2222-4222-8222-222222222243"
	if err := db.Create(&second).Error; err != nil {
		t.Fatal("null terminal UUIDs must not collide for pending requests", err)
	}
	slot := entity.TeamModelRequestPendingSlot{TeamID: original.TeamID, ModelID: original.ModelID, RequestID: original.ID, CreatedAt: stamp}
	if err := db.Create(&slot).Error; err != nil {
		t.Fatal(err)
	}
	duplicateSlot := slot
	duplicateSlot.RequestID = second.ID
	if err := db.Create(&duplicateSlot).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatal("one pending shared Team/Model slot must reject duplicates portably", err)
	}
	if err := db.Delete(&slot).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&duplicateSlot).Error; err != nil {
		t.Fatal("terminal history must not prevent a later pending request", err)
	}
	decisionID := "33333333-3333-4333-8333-333333333343"
	actorID, actorName := "usr_v44_missing_reviewer", "Recorded reviewer"
	approved := newHistory()
	approved.ID, approved.RequestID = "tmr_v44_approved_history", "44444444-4444-4444-8444-444444444443"
	approved.Status, approved.DecisionAction = entity.TeamModelRequestApproved, "approve"
	approved.DecisionID, approved.DecisionActorID, approved.DecisionActorName = &decisionID, &actorID, &actorName
	approved.DecisionReviewETag, approved.DecisionRequestHash = strings.Repeat("c", 64), strings.Repeat("d", 64)
	approved.DecidedAt, approved.ResolvedAt = &stamp, &stamp
	if err := db.Create(&approved).Error; err != nil {
		t.Fatal("first-terminal receipt must survive missing live identities", err)
	}
	// Establish the persisted baseline before any repeat migration. PostgreSQL
	// may scan timestamptz with a different Location from the inserted UTC value;
	// equal instants and all non-time fields must still survive initial storage.
	var approvedBeforeMigration entity.TeamModelRequest
	if err := db.Take(&approvedBeforeMigration, "id = ?", approved.ID).Error; err != nil {
		t.Fatal("read persisted terminal baseline", err)
	}
	if !reflect.DeepEqual(teamModelMigrationUTCFacts(approved), teamModelMigrationUTCFacts(approvedBeforeMigration)) {
		t.Fatalf("initial storage changed terminal facts: differences=%v", teamModelMigrationDifferenceFields(teamModelMigrationUTCFacts(approved), teamModelMigrationUTCFacts(approvedBeforeMigration)))
	}
	duplicateDecision := approved
	duplicateDecision.ID, duplicateDecision.RequestID = "tmr_v44_duplicate_decision", "55555555-5555-4555-8555-555555555543"
	if err := db.Create(&duplicateDecision).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatal("first-terminal decision UUID must be globally unique", err)
	}
	for i, invalid := range []entity.TeamModelRequest{
		{Status: "completed"},
		{Status: entity.TeamModelRequestPending, ResolvedAt: &stamp},
		{Status: entity.TeamModelRequestApproved},
		{Status: entity.TeamModelRequestCancelled, ResolvedAt: &stamp},
	} {
		row := newHistory()
		row.ID, row.RequestID = "tmr_v44_bad_"+string(rune('a'+i)), "66666666-6666-4666-8666-6666666666"+string(rune('a'+i))+"3"
		row.Status, row.ResolvedAt = invalid.Status, invalid.ResolvedAt
		if err := db.Create(&row).Error; err == nil {
			t.Fatal("invalid pending/terminal evidence was accepted", row.Status)
		}
	}
	wrongAction := approved
	wrongAction.ID, wrongAction.RequestID = "tmr_v44_wrong_action", "77777777-7777-4777-8777-777777777743"
	wrongDecision := "88888888-8888-4888-8888-888888888843"
	wrongAction.DecisionID, wrongAction.DecisionAction = &wrongDecision, "reject"
	if err := db.Create(&wrongAction).Error; err == nil {
		t.Fatal("approved receipt cannot record rejection as the winning action")
	}
	// First-terminal guards reject coherent-looking receipts with borrowed actors,
	// missing names/reasons, or terminal material smuggled into pending history.
	for i, mutate := range []func(*entity.TeamModelRequest){
		func(row *entity.TeamModelRequest) { row.DecisionActorID = &row.ApplicantUserID },
		func(row *entity.TeamModelRequest) { row.DecisionActorName = nil },
		func(row *entity.TeamModelRequest) {
			row.Status, row.DecisionAction, row.DecisionReason = entity.TeamModelRequestRejected, "reject", ""
		},
		func(row *entity.TeamModelRequest) {
			row.Status, row.DecisionAction = entity.TeamModelRequestWithdrawn, "withdraw"
		},
	} {
		row := approved
		row.ID, row.RequestID = "tmr_v44_bad_actor_"+string(rune('a'+i)), "99999999-9999-4999-8999-9999999999"+string(rune('a'+i))+"4"
		terminalID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa" + string(rune('a'+i)) + "4"
		row.DecisionID = &terminalID
		mutate(&row)
		if err := db.Create(&row).Error; err == nil {
			t.Fatal("invalid first-terminal receipt was persisted", i)
		}
	}
	pendingActor := newHistory()
	pendingActor.ID, pendingActor.RequestID, pendingActor.DecisionActorID = "tmr_v44_pending_actor", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbb4", &actorID
	if err := db.Create(&pendingActor).Error; err == nil {
		t.Fatal("pending history invented a terminal actor")
	}
	withdrawn := approved
	withdrawn.ID, withdrawn.RequestID = "tmr_v44_withdrawn", "cccccccc-cccc-4ccc-8ccc-ccccccccccc4"
	withdrawID := "dddddddd-dddd-4ddd-8ddd-ddddddddddd4"
	withdrawn.DecisionID, withdrawn.DecisionActorID = &withdrawID, &withdrawn.ApplicantUserID
	withdrawn.Status, withdrawn.DecisionAction, withdrawn.DecisionReason = entity.TeamModelRequestWithdrawn, "withdraw", ""
	if err := db.Create(&withdrawn).Error; err != nil {
		t.Fatal("own immutable withdrawal receipt was rejected", err)
	}
	cancelled := newHistory()
	cancelled.ID, cancelled.RequestID = "tmr_v44_cancelled", "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeee4"
	cancelled.Status, cancelled.CancelledReason, cancelled.ResolvedAt = entity.TeamModelRequestCancelled, "membership_unavailable", &stamp
	if err := db.Create(&cancelled).Error; err != nil {
		t.Fatal("lifecycle cancellation required invented decision evidence", err)
	}
	for _, source := range []string{"", strings.Repeat("x", 31)} {
		if err := db.Model(&entity.TeamModelGrant{}).Where("team_id = ? AND model_id = ?", grant.TeamID, grant.ModelID).Update("source_request_id", source).Error; err == nil {
			t.Fatal("empty or unbounded grant provenance was accepted")
		}
	}
	if err := db.Model(&entity.TeamModelGrant{}).Where("team_id = ? AND model_id = ?", grant.TeamID, grant.ModelID).Update("source_request_id", approved.ID).Error; err != nil {
		t.Fatal("Team grant provenance must not require a live request foreign key", err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var retained entity.TeamModelRequest
	if err := db.Take(&retained, "id = ?", approved.ID).Error; err != nil || !reflect.DeepEqual(approvedBeforeMigration, retained) {
		t.Fatalf("repeat migration changed immutable terminal receipt: differences=%v error=%v", teamModelMigrationDifferenceFields(approvedBeforeMigration, retained), err)
	}
	// Parse the actual current model to guard against accidentally introducing
	// a relationship through an evolving entity after the frozen V44 release.
	parsed, err := schema.Parse(&entity.TeamModelRequest{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil || len(parsed.Relationships.Relations) != 0 {
		t.Fatal("historical request unexpectedly acquired a live relationship", err)
	}
}

func dropTeamModelRequestFixtureIndex(t *testing.T, db *gorm.DB, model any, name string) {
	t.Helper()
	if db.Name() == "postgres" {
		// Pinned GORM DropIndex renders an invalid CURRENT_SCHEMA().index target.
		// These fixed test-only statements reconstruct partially committed DDL;
		// production creation and repair continue to use GORM Migrator APIs.
		statements := map[string]string{
			"idx_team_model_request_user_cursor": `DROP INDEX "idx_team_model_request_user_cursor"`,
			"idx_team_model_request_team_cursor": `DROP INDEX "idx_team_model_request_team_cursor"`,
			"uq_team_model_request_intent":       `DROP INDEX "uq_team_model_request_intent"`,
			"uq_team_model_request_decision":     `DROP INDEX "uq_team_model_request_decision"`,
			"uq_team_model_pending_request":      `DROP INDEX "uq_team_model_pending_request"`,
		}
		statement, ok := statements[name]
		if !ok {
			t.Fatal("unsupported Team request fault-injection index", name)
		}
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	} else if err := db.Migrator().DropIndex(model, name); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasIndex(model, name) {
		t.Fatal("Team request fault injection retained index", name)
	}
}

func teamModelMigrationUTCFacts(row entity.TeamModelRequest) entity.TeamModelRequest {
	row.CreatedAt, row.UpdatedAt = row.CreatedAt.UTC(), row.UpdatedAt.UTC()
	if row.ResolvedAt != nil {
		stamp := row.ResolvedAt.UTC()
		row.ResolvedAt = &stamp
	}
	if row.DecidedAt != nil {
		stamp := row.DecidedAt.UTC()
		row.DecidedAt = &stamp
	}
	return row
}

func teamModelMigrationDifferenceFields(before, after entity.TeamModelRequest) []string {
	left, right := reflect.ValueOf(before), reflect.ValueOf(after)
	var fields []string
	for i := 0; i < left.NumField(); i++ {
		if !reflect.DeepEqual(left.Field(i).Interface(), right.Field(i).Interface()) {
			fields = append(fields, left.Type().Field(i).Name)
		}
	}
	return fields
}
