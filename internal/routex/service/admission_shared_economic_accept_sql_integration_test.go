package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/routeattempt"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"github.com/miclle/routex/pkg/upstream"
	"gorm.io/gorm"
)

// This worker owns one empty disposable database. The seven economic tables
// are explicit test-only DDL, not migration or production cutover acceptance.
func TestSharedEconomicAcceptSQLIntegration(t *testing.T) {
	if os.Getenv("ROUTEX_TEST_SHARED_ECONOMIC_ACCEPT_SQL") != "1" {
		t.Skip("dedicated shared economic Accept/MarkDispatch SQL worker is not enabled")
	}
	driver := os.Getenv("ROUTEX_TEST_SHARED_ECONOMIC_ACCEPT_SQL_DRIVER")
	if driver != "postgres" && driver != "mysql" {
		t.Fatal("choose one dedicated postgres or mysql worker")
	}
	db := runtimeArchiveIntegrationDB(t, driver)
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	db = db.WithContext(ctx)
	if db.Migrator().HasTable("schema_migrations") {
		t.Fatal("Accept worker requires a fresh, exclusively owned schema")
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("current predecessor schema", err)
	}
	for _, table := range []any{&entity.AdmissionEconomicDomain{}, &entity.AdmissionEconomicReceipt{}, &entity.AdmissionEconomicAttempt{},
		&entity.AdmissionEconomicAccount{}, &entity.AdmissionEconomicCounter{}, &entity.AdmissionEconomicContribution{}, &entity.AdmissionEconomicInvalidBound{}} {
		if db.Migrator().HasTable(table) {
			t.Fatal("predecessor unexpectedly created an economic table")
		}
		if err := db.Migrator().CreateTable(table); err != nil {
			t.Fatal("seven bounded test-only economic tables", err)
		}
	}
	owner := sharedEconomicHistorySQLOwner(t, db)
	domain := entity.AdmissionEconomicDomain{InstallationID: owner.Owner.InstallationID, FormatVersion: 1,
		Initialized: true, CoverageStart: 1, ClockHighWater: 1, ReceiptCapacity: 1000}
	if err := db.Create(&domain).Error; err != nil {
		t.Fatal("explicitly empty test-only economic domain", err)
	}
	_, catalog := gatewayReservationSQLFixture(t)
	gatewayReservationSQLSeed(t, db, catalog)
	secrets, err := secretstore.New(bytes.Repeat([]byte{43}, 32))
	if err != nil {
		t.Fatal(err)
	}
	// The isolated route SQL fixture deliberately has unresolved ciphertext.
	// Replace it with actual local secret storage before building any runtime.
	for credentialID := range catalog.credentials {
		ciphertext, err := secrets.Seal(credentialID, "isolated-test-upstream-secret")
		if err != nil {
			t.Fatal(err)
		}
		sharedEconomicHistorySQLChanged(t, db.Model(&entity.ProviderCredential{}).Where("id = ?", credentialID).Update("ciphertext", ciphertext))
	}
	ordinal := 3100
	fixture := func(t *testing.T, kind string, policy map[string]any) sharedEconomicAcceptSQLFixture {
		t.Helper()
		ordinal++
		return newSharedEconomicAcceptSQLFixture(t, db, secrets, owner, kind, ordinal, policy)
	}
	t.Run("real_sql_real_file_restart_recovery", func(t *testing.T) {
		sharedRecoverySQLIntegration(t, ctx, db, fixture)
	})

	for _, kind := range []string{"personal_key", "project_key", "personal_session", "team_session"} {
		t.Run("real_live_"+kind, func(t *testing.T) {
			f := fixture(t, kind, nil)
			if got, err := f.store.MarkDispatch(ctx, f.attempt(t, 1, nil)); got != nil || err == nil {
				t.Fatal("live runtime without returned Accept permitted dispatch", err)
			}
			f.accept(t, ctx)
			f.requireAdmission(t, db)
			first := f.attempt(t, 1, nil)
			if got, err := f.store.MarkDispatch(ctx, first); err != nil || got == nil || got.AttemptDigest != first.Digest {
				t.Fatal("real current selected dispatch", err)
			}
			before := sharedEconomicAcceptSQLSnapshot(t, db)
			if _, err := f.store.MarkDispatch(ctx, first); err != nil {
				t.Fatal("exact dispatch acknowledgement replay", err)
			}
			sharedEconomicAcceptSQLRequireSnapshot(t, db, before)
		})
	}

	t.Run("concurrent_exact_accept_is_one_receipt_and_hold", func(t *testing.T) {
		f := fixture(t, "personal_key", nil)
		before := sharedEconomicAcceptSQLSnapshot(t, db)
		type outcome struct {
			receipt *sharedAuthorityReceipt
			err     error
		}
		start := make(chan struct{})
		results := make(chan outcome, 2)
		for range 2 {
			go func() {
				<-start
				got, err := f.store.Accept(ctx, sharedTestCopy(f.intent))
				results <- outcome{got, err}
			}()
		}
		close(start)
		var receiptID string
		for range 2 {
			select {
			case got := <-results:
				if got.err != nil || got.receipt == nil || got.receipt.Key != sharedIntentKey(f.intent) {
					t.Fatal("concurrent exact Accept", got.err)
				}
				if receiptID != "" && receiptID != got.receipt.ReceiptID {
					t.Fatal("identical requests produced different receipts")
				}
				receiptID = got.receipt.ReceiptID
			case <-ctx.Done():
				t.Fatal("concurrent Accept exceeded worker bound", ctx.Err())
			}
		}
		after := sharedEconomicAcceptSQLSnapshot(t, db)
		if len(after.receipts) != len(before.receipts)+1 || after.domains[0].ReceiptCount != before.domains[0].ReceiptCount+1 {
			t.Fatal("exact concurrent retry duplicated domain or receipt capacity")
		}
		f.requireAdmission(t, db)
		if _, err := f.store.Accept(ctx, f.intent); err != nil {
			t.Fatal("original Accept replay", err)
		}
		sharedEconomicAcceptSQLRequireSnapshot(t, db, after)
	})

	t.Run("changed_intent_and_execution_conflict", func(t *testing.T) {
		f := fixture(t, "personal_key", nil)
		f.accept(t, ctx)
		before := sharedEconomicAcceptSQLSnapshot(t, db)
		for _, change := range []func(*sharedAdmissionIntent){
			func(in *sharedAdmissionIntent) { in.ExecutionID += "_other" },
			func(in *sharedAdmissionIntent) { in.CalendarRevision += "_other" },
			func(in *sharedAdmissionIntent) { in.Reservation.Candidates[0].Weight++ },
		} {
			changed := sharedTestCopy(f.intent)
			change(&changed)
			changed.Digest = durableSharedSpoolDigest(t, changed)
			if got, err := f.store.Accept(ctx, changed); got != nil || err == nil {
				t.Fatal("changed immutable intent was acknowledged", err)
			}
			sharedEconomicAcceptSQLRequireSnapshot(t, db, before)
		}
	})

	for _, test := range []struct {
		name   string
		policy map[string]any
		want   error
	}{
		{"tokens", map[string]any{"tokens_month": int64(0), "tokens_month_behavior": "stop"}, eventqueue.ErrQuotaTokens},
		{"money", map[string]any{"money_month": "0", "money_month_behavior": "stop"}, eventqueue.ErrQuotaMoney},
		{"rpm", map[string]any{"rpm": int64(0)}, eventqueue.ErrRateLimit},
		{"concurrency", map[string]any{"concurrency": int64(0)}, eventqueue.ErrConcurrency},
	} {
		t.Run("independent_"+test.name+"_refusal_is_atomic", func(t *testing.T) {
			f := fixture(t, "personal_key", test.policy)
			before := sharedEconomicAcceptSQLSnapshot(t, db)
			if got, err := f.store.Accept(ctx, f.intent); got != nil || !errors.Is(err, test.want) {
				t.Fatal("independent ceiling did not refuse admission", err, test.want)
			}
			sharedEconomicAcceptSQLRequireSnapshot(t, db, before)
		})
	}

	for _, test := range []struct {
		name   string
		change func(*gorm.DB, sharedEconomicAcceptSQLFixture) *gorm.DB
	}{
		{"key_revocation", func(tx *gorm.DB, f sharedEconomicAcceptSQLFixture) *gorm.DB {
			return tx.Model(&entity.APIKey{}).Where("id = ?", f.intent.Authorization.Key.ID).Update("status", entity.KeyRevoked)
		}},
		{"owner_fence", nil},
		{"mode_revision", func(tx *gorm.DB, f sharedEconomicAcceptSQLFixture) *gorm.DB {
			return tx.Model(&entity.AdmissionMode{}).Where("id = ?", 1).Update("revision", int64(f.intent.Owner.ModeRevision+1))
		}},
		{"calendar_unfrozen", func(tx *gorm.DB, _ sharedEconomicAcceptSQLFixture) *gorm.DB {
			return tx.Model(&entity.QuotaSetting{}).Where("id = ?", 1).Update("accounting_started", false)
		}},
		{"selected_route", func(tx *gorm.DB, f sharedEconomicAcceptSQLFixture) *gorm.DB {
			return tx.Model(&entity.ProviderConnection{}).Where("id = ?", f.intent.Reservation.Candidates[0].Connection.ID).Update("enabled", false)
		}},
		{"current_ip_without_revision_change", func(tx *gorm.DB, f sharedEconomicAcceptSQLFixture) *gorm.DB {
			return tx.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "user", f.intent.Authorization.User.ID).
				Updates(map[string]any{"ip_mode": "denylist", "ip_ranges_json": `["198.51.100.9/32"]`})
		}},
	} {
		t.Run("current_"+test.name+"_blocks_accept_and_dispatch", func(t *testing.T) {
			f := fixture(t, "personal_key", nil)
			f.accept(t, ctx)
			before := sharedEconomicAcceptSQLSnapshot(t, db)
			tx := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer func() { _ = tx.Rollback().Error }()
			if test.name == "owner_fence" {
				fence := admissionOwnerFence{FencingID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Owner: f.intent.Owner, InstanceStartedAt: f.intent.OwnerStartedAt}
				receipt, err := (&admissionOwnerStore{db: tx}).FenceOwner(ctx, fence)
				if err != nil || receipt == nil || receipt.ResultFence != 2 || receipt.FencingID != fence.FencingID ||
					receipt.Owner != fence.Owner || !receipt.InstanceStartedAt.Equal(fence.InstanceStartedAt) {
					t.Fatal("real exact owner fencing transition", receipt, err)
				}
			} else {
				sharedEconomicHistorySQLChanged(t, test.change(tx, f))
			}
			store := &sharedEconomicSQLStore{db: tx, live: f.store.live}
			if got, err := store.Accept(ctx, f.intent); got != nil || err == nil {
				t.Fatal("current revocation allowed an Accept replay", err)
			}
			if got, err := store.MarkDispatch(ctx, f.attempt(t, 1, nil)); got != nil || err == nil {
				t.Fatal("current revocation permitted a new dispatch", err)
			}
			sharedEconomicAcceptSQLRequireSnapshot(t, tx, before)
			if err := tx.Rollback().Error; err != nil {
				t.Fatal(err)
			}
			sharedEconomicAcceptSQLRequireSnapshot(t, db, before)
		})
	}

	t.Run("selected_dispatch_requires_exact_no_work_checkpoint_prefix", func(t *testing.T) {
		f := fixture(t, "personal_key", nil)
		f.accept(t, ctx)
		first := f.attempt(t, 1, nil)
		if _, err := f.store.MarkDispatch(ctx, first); err != nil {
			t.Fatal(err)
		}
		fact, err := sharedCallPayload(f.intent.RequestID, first.Fallback)
		if err != nil {
			t.Fatal(err)
		}
		fact.Attempts[0].WorkEvidence, fact.Attempts[0].FailureClass = "not_sent", "connection_failure"
		checkpoint := sharedAttemptCheckpoint{Key: first.Key, Number: 1, AttemptDigest: first.Digest,
			Fallback: sharedEconomicHistorySQLBody(t, fact), Recovery: sharedCheckpointRecovery(fact, f.intent.Bound)}
		checkpoint.Digest = durableSharedSpoolDigest(t, checkpoint)
		second := f.attempt(t, 2, fact.Attempts)
		before := sharedEconomicAcceptSQLSnapshot(t, db)
		if got, err := f.store.MarkDispatch(ctx, second); got != nil || err == nil {
			t.Fatal("uncheckpointed predecessor permitted another dispatch", err)
		}
		sharedEconomicAcceptSQLRequireSnapshot(t, db, before)
		if err := f.store.Checkpoint(ctx, checkpoint); err != nil {
			t.Fatal("original no-work checkpoint", err)
		}
		changed := sharedTestCopy(second)
		changed.ProviderModelID = "pmd_outside_original"
		changed.Digest = durableSharedSpoolDigest(t, changed)
		before = sharedEconomicAcceptSQLSnapshot(t, db)
		if got, err := f.store.MarkDispatch(ctx, changed); got != nil || err == nil {
			t.Fatal("new current candidate escaped the original manifest", err)
		}
		sharedEconomicAcceptSQLRequireSnapshot(t, db, before)
		if _, err := f.store.MarkDispatch(ctx, second); err != nil {
			t.Fatal("selected second original route after exact checkpoint", err)
		}
		f.requireAdmission(t, db)
	})

	t.Run("invalid_bound_blocks_new_accept_and_dispatch", func(t *testing.T) {
		f := fixture(t, "personal_key", nil)
		f.accept(t, ctx)
		if f.intent.Bound.Revision == "" {
			t.Fatal("finite capture must supply an original bound revision")
		}
		tx := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer func() { _ = tx.Rollback().Error }()
		row := entity.AdmissionEconomicInvalidBound{InstallationID: f.intent.Owner.InstallationID,
			Revision: f.intent.Bound.Revision, RevisionDigest: sharedEconomicIdentityDigest(f.intent.Bound.Revision),
			RequestID: f.intent.RequestID, RequestDigest: sharedEconomicIdentityDigest(f.intent.RequestID), RecordedAt: f.intent.Authorization.EvaluatedAt.UnixNano()}
		if err := tx.Create(&row).Error; err != nil {
			t.Fatal("test-only retained invalid-bound cause", err)
		}
		before := sharedEconomicAcceptSQLSnapshot(t, tx)
		store := &sharedEconomicSQLStore{db: tx, live: f.store.live}
		if got, err := store.MarkDispatch(ctx, f.attempt(t, 1, nil)); got != nil || !errors.Is(err, eventqueue.ErrQuotaBound) {
			t.Fatal("invalid original bound permitted dispatch", err)
		}
		other := f.anotherRequest(t, "_invalid_bound")
		other.store.db = tx
		if got, err := other.store.Accept(ctx, other.intent); got != nil || !errors.Is(err, eventqueue.ErrQuotaBound) {
			t.Fatal("invalid original bound permitted new admission", err)
		}
		sharedEconomicAcceptSQLRequireSnapshot(t, tx, before)
	})

	t.Run("cancellation_and_outer_rollback_leave_no_partial_admission", func(t *testing.T) {
		f := fixture(t, "personal_key", nil)
		before := sharedEconomicAcceptSQLSnapshot(t, db)
		cancelled, stop := context.WithCancel(ctx)
		stop()
		if got, err := f.store.Accept(cancelled, f.intent); got != nil || !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled Accept returned authority", err)
		}
		sharedEconomicAcceptSQLRequireSnapshot(t, db, before)
		held := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if held.Error != nil {
			t.Fatal(held.Error)
		}
		defer func() { _ = held.Rollback().Error }()
		if err := lockGovernance(held); err != nil {
			t.Fatal(err)
		}
		blocked, cancelBlocked := context.WithTimeout(ctx, 250*time.Millisecond)
		got, err := f.store.Accept(blocked, f.intent)
		deadline := blocked.Err()
		cancelBlocked()
		if got != nil || err == nil || deadline != context.DeadlineExceeded {
			t.Fatal("Accept bypassed a held governance barrier", err, deadline)
		}
		if err := held.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		sharedEconomicAcceptSQLRequireSnapshot(t, db, before)
		tx := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer func() { _ = tx.Rollback().Error }()
		store := &sharedEconomicSQLStore{db: tx, live: f.store.live}
		if got, err := store.Accept(ctx, f.intent); err != nil || got == nil {
			t.Fatal("positive nested Accept before whole rollback", err)
		}
		f.requireAdmission(t, tx)
		if err := tx.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		sharedEconomicAcceptSQLRequireSnapshot(t, db, before)
		f.accept(t, ctx)
		f.requireAdmission(t, db)
		accepted := sharedEconomicAcceptSQLSnapshot(t, db)
		attempt := f.attempt(t, 1, nil)
		if got, err := f.store.MarkDispatch(cancelled, attempt); got != nil || !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled MarkDispatch returned authority", err)
		}
		sharedEconomicAcceptSQLRequireSnapshot(t, db, accepted)
		held = db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if held.Error != nil {
			t.Fatal(held.Error)
		}
		defer func() { _ = held.Rollback().Error }()
		if err := lockGovernance(held); err != nil {
			t.Fatal(err)
		}
		blocked, cancelBlocked = context.WithTimeout(ctx, 250*time.Millisecond)
		dispatch, err := f.store.MarkDispatch(blocked, attempt)
		deadline = blocked.Err()
		cancelBlocked()
		if dispatch != nil || err == nil || deadline != context.DeadlineExceeded {
			t.Fatal("MarkDispatch bypassed a held governance barrier", err, deadline)
		}
		if err := held.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		sharedEconomicAcceptSQLRequireSnapshot(t, db, accepted)
		tx = db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer func() { _ = tx.Rollback().Error }()
		store = &sharedEconomicSQLStore{db: tx, live: f.store.live}
		if got, err := store.MarkDispatch(ctx, attempt); err != nil || got == nil {
			t.Fatal("positive nested MarkDispatch before whole rollback", err)
		}
		staged := sharedEconomicAcceptSQLSnapshot(t, tx)
		if len(staged.attempts) != len(accepted.attempts)+1 {
			t.Fatal("nested dispatch did not retain exactly one attempt")
		}
		if err := tx.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		sharedEconomicAcceptSQLRequireSnapshot(t, db, accepted)
		if got, err := f.store.MarkDispatch(ctx, attempt); err != nil || got == nil {
			t.Fatal("original dispatch after whole rollback", err)
		}
	})

	t.Run("historical_settlement_survives_committed_revocation", func(t *testing.T) {
		f := fixture(t, "personal_key", nil)
		f.accept(t, ctx)
		attempt := f.attempt(t, 1, nil)
		if _, err := f.store.MarkDispatch(ctx, attempt); err != nil {
			t.Fatal(err)
		}
		sharedEconomicHistorySQLChanged(t, db.Model(&entity.APIKey{}).Where("id = ?", f.intent.Authorization.Key.ID).Update("status", entity.KeyRevoked))
		if got, err := f.store.MarkDispatch(ctx, attempt); got != nil || err == nil {
			t.Fatal("revoked current Key retained dispatch authority", err)
		}
		fact, err := sharedCallPayload(f.intent.RequestID, attempt.Fallback)
		if err != nil {
			t.Fatal(err)
		}
		terminal := sharedTerminalIntent{Key: attempt.Key, LastAttempt: 1, Fact: attempt.Fallback,
			Actual: quotaCallSettlement(fact), ExecutorClosed: true, Upstream: sharedUpstreamUnknown}
		terminal.Digest = durableSharedSpoolDigest(t, terminal)
		history := &sharedEconomicSQLStore{db: db}
		if got, err := history.Settle(ctx, terminal); err != nil || got == nil || got.Terminal == nil {
			t.Fatal("current revocation suppressed historical settlement", err)
		}
		for _, policy := range f.intent.Policies {
			var account entity.AdmissionEconomicAccount
			if err := db.Where("installation_id = ? AND account_digest = ?", f.intent.Owner.InstallationID, sharedEconomicIdentityDigest(policy.Account)).Take(&account).Error; err != nil || account.ExecutorCount != 0 || account.RPMCount != 1 {
				t.Fatal("historical settlement did not release the exact executor once", err)
			}
		}
		before := sharedEconomicAcceptSQLSnapshot(t, db)
		if _, err := history.Settle(ctx, terminal); err != nil {
			t.Fatal("historical exact terminal replay", err)
		}
		sharedEconomicAcceptSQLRequireSnapshot(t, db, before)
	})
	sharedGatewaySQLIntegration(t, ctx, db, fixture, owner)
}

type sharedEconomicAcceptSQLFixture struct {
	store   *sharedEconomicSQLStore
	intent  sharedAdmissionIntent
	quota   *runtimeQuotaData
	payload map[string]json.RawMessage
}

func newSharedEconomicAcceptSQLFixture(t *testing.T, db *gorm.DB, secrets *secretstore.Store, owner admissionOwnerRegistration, kind string, ordinal int, policy map[string]any) sharedEconomicAcceptSQLFixture {
	t.Helper()
	state := sharedPolicySQLIntegrationFixture(t, db, kind, ordinal)
	// This exclusively owned shared-domain fixture explicitly freezes the
	// actual persisted calendar before preparing live economic authority.
	var calendar entity.QuotaSetting
	if err := db.Take(&calendar, 1).Error; err != nil {
		t.Fatal("read actual shared fixture calendar", err)
	}
	if !calendar.AccountingStarted {
		sharedEconomicHistorySQLChanged(t, db.Model(&entity.QuotaSetting{}).Where("id = ? AND accounting_started = ?", 1, false).Update("accounting_started", true))
	}
	a := state.authorization
	if policy != nil {
		parentKind, parentID := "user", ""
		if a.User != nil {
			parentID = a.User.ID
		}
		if a.Project != nil {
			parentKind, parentID = "project", a.Project.ID
		}
		if a.Team != nil {
			parentKind, parentID = "team", a.Team.ID
		}
		sharedEconomicHistorySQLChanged(t, db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", parentKind, parentID).Updates(policy))
	}
	name := fmt.Sprintf("shared-accept-%d", ordinal)
	currentModel := a.Model.ID
	if err := db.Create(&entity.ModelName{Name: name, ModelID: currentModel, CurrentModelID: &currentModel}).Error; err != nil {
		t.Fatal("real current public Model name", err)
	}
	for i, providerModel := range []string{"pmd_one", "pmd_two"} {
		weight := 40
		if i == 1 {
			weight = 60
		}
		row := entity.ModelProviderBinding{ID: configurationTestIDV1("bnd", ordinal*2+i), CreatedAt: a.Model.CreatedAt,
			ModelID: a.Model.ID, ProviderModelID: providerModel, Weight: weight}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal("actual principal Model routing", err)
		}
	}
	raw := fmt.Sprintf("%043d", ordinal)
	if a.Key != nil {
		prefix := "rx_"
		model := any(&entity.APIKey{})
		if a.Project != nil {
			prefix, model = "rxp_", &entity.ProjectKey{}
		}
		raw = prefix + raw
		sharedEconomicHistorySQLChanged(t, db.Model(model).Where("id = ?", a.Key.ID).Update("token_hash", secret.SHA256Hex(raw)))
	} else {
		sharedEconomicHistorySQLChanged(t, db.Model(&entity.Session{}).Where("id = ?", a.Session.ID).Update("token_hash", secret.SHA256Hex(raw)))
	}
	svc := &Service{db: db, secrets: secrets, upstream: upstream.NewClient(false), runtime: &gatewayRuntime{}}
	data, err := svc.loadRuntimeData(t.Context())
	if err != nil {
		t.Fatal("production repeatable-read runtime loader", err)
	}
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(runtimeAuthorizationLease)))
	routes, err := svc.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal("production credential and route preparation", err)
	}
	t.Cleanup(func() { closeRuntimeClients(routes) })
	snapshot := configurationTestIDV1("cfg", ordinal)
	svc.runtime.routes.Store(&runtimeRoutes{ID: snapshot, Models: routes, PublishedAt: time.Now().UTC()})
	identity := gatewayIdentity{selectedModelID: a.Model.ID}
	switch kind {
	case "personal_key", "project_key":
		identity.key, err = svc.authenticateRuntimeKey(raw)
		if a.PersonalGrants != nil {
			identity.personalGrantRevision = a.PersonalGrants.Revision
		}
	case "personal_session":
		identity.personal, err = svc.RuntimeAuthenticatePersonalSession(t.Context(), raw)
	case "team_session":
		identity.team, err = svc.RuntimeAuthenticateTeamSession(t.Context(), raw, a.Team.ID)
	}
	if err != nil {
		t.Fatal("actual runtime principal authentication", err)
	}
	result := identity.result(entity.ProtocolOpenAIChat, name, false)
	result.ModelID, result.SnapshotID = a.Model.ID, snapshot
	payload := attachmentTestPayload(t, fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"test-only transient prompt"}],"max_completion_tokens":10}`, name))
	result.quotaRequest = inspectQuotaRequest(result.NativeProtocol(), payload)
	plan, err := svc.gatewayAttemptPlan(result.ModelID, result.NativeProtocol())
	if err != nil {
		t.Fatal("actual retained runtime plan", err)
	}
	t.Cleanup(plan.releaseSources)
	plan.userID, plan.projectID, plan.keyID = result.UserID, result.ProjectID, result.KeyID
	plan.team, plan.personal, plan.personalGrantRevision = identity.team, identity.personal, identity.personalGrantRevision
	ip := netip.MustParseAddr("198.51.100.9")
	requestCtx := context.WithValue(t.Context(), gatewayIPKey{}, ip)
	plan, err = svc.prepareGatewayAttemptQuotaState(requestCtx, plan, result)
	if err != nil {
		t.Fatal("real current scopes and conservative maximum", err)
	}
	requestID := fmt.Sprintf("req_accept_%d", ordinal)
	manifest, err := captureGatewayReservationManifest(requestID, plan, result, data.Quota, payload)
	if err != nil {
		t.Fatal("real immutable reservation capture", err)
	}
	fallback, err := gatewayFallbackPayload(requestID, result, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	request := sharedAdmissionRequest{RequestID: requestID, Reservation: manifest, OwnerStartedAt: owner.InstanceStartedAt,
		Authorization: a, CalendarRevision: data.Quota.Setting.ETag, TimeZone: data.Quota.Setting.TimeZone,
		Policies: result.admissionQuota, Bound: result.quotaBound, Recovery: zeroQuotaSettlement(result.quotaBound), Fallback: fallback}
	live, err := newSharedEconomicLiveAuthority(svc, plan, result, ip, owner.Owner, request, data.Quota, payload)
	if err != nil {
		t.Fatal("live authority from coherent current runtime and SQL facts", err)
	}
	intent := sharedTestCopy(sharedAdmissionIntent{sharedAdmissionRequest: request, Owner: owner.Owner, ExecutionID: fmt.Sprintf("execution_accept_%d", ordinal)})
	intent.Digest = durableSharedSpoolDigest(t, intent)
	if err := validateSharedSpoolAdmission(intent); err != nil {
		t.Fatal("complete original intent", err)
	}
	return sharedEconomicAcceptSQLFixture{store: &sharedEconomicSQLStore{db: db, live: live}, intent: intent, quota: data.Quota, payload: payload}
}

func (f sharedEconomicAcceptSQLFixture) anotherRequest(t *testing.T, suffix string) sharedEconomicAcceptSQLFixture {
	t.Helper()
	request := sharedTestCopy(f.intent.sharedAdmissionRequest)
	request.RequestID += suffix
	request.Reservation.RequestID = request.RequestID
	fact, err := sharedCallPayload(f.intent.RequestID, request.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	fact.RequestID = request.RequestID
	request.Fallback = sharedEconomicHistorySQLBody(t, fact)
	prior := f.store.live
	live, err := newSharedEconomicLiveAuthority(prior.service, prior.plan, prior.result, prior.clientIP, prior.owner, request, f.quota, f.payload)
	if err != nil {
		t.Fatal("another independent live request", err)
	}
	intent := sharedTestCopy(sharedAdmissionIntent{sharedAdmissionRequest: request, Owner: f.intent.Owner, ExecutionID: f.intent.ExecutionID + suffix})
	intent.Digest = durableSharedSpoolDigest(t, intent)
	f.store, f.intent = &sharedEconomicSQLStore{db: f.store.db, live: live}, intent
	return f
}

func (f sharedEconomicAcceptSQLFixture) accept(t *testing.T, ctx context.Context) {
	t.Helper()
	got, err := f.store.Accept(ctx, f.intent)
	if err != nil || got == nil || got.Key != sharedIntentKey(f.intent) {
		t.Fatal("positive real Accept", err)
	}
}

func (f sharedEconomicAcceptSQLFixture) attempt(t *testing.T, number int, previous []CallAttempt) sharedAttemptIntent {
	t.Helper()
	candidate := f.intent.Reservation.Candidates[(number-1)%len(f.intent.Reservation.Candidates)]
	route, _, found := f.store.live.plan.Candidate(routeattempt.Attempt{Number: number, TargetID: candidate.Binding.ID,
		ConnectionID: candidate.Connection.ID, CredentialID: candidate.Credential.Identity.ID, Protocol: f.intent.Reservation.Protocol})
	if !found {
		t.Fatal("selected original runtime candidate is absent")
	}
	now := time.Now().UTC()
	result := *f.store.live.result
	applyGatewayAttemptRoute(&result, route)
	result.Attempts = append([]CallAttempt(nil), previous...)
	result.AttemptID, result.AttemptStartedAt = fmt.Sprintf("att_%s_%d", f.intent.RequestID, number), now
	fallback, err := gatewayFallbackPayload(f.intent.RequestID, &result, now)
	if err != nil {
		t.Fatal("actual selected-route interrupted fallback", err)
	}
	fact, err := sharedCallPayload(f.intent.RequestID, fallback)
	if err != nil {
		t.Fatal(err)
	}
	fact.Attempts[len(fact.Attempts)-1].NativeCompletionEvidence = "unknown"
	current := fact.Attempts[len(fact.Attempts)-1]
	attempt := sharedAttemptIntent{Key: sharedIntentKey(f.intent), Number: number, AttemptID: current.ID,
		CredentialID: current.CredentialID, SnapshotID: current.SnapshotID, ConnectionID: current.ConnectionID,
		ProviderModelID: current.ProviderModelID, Fallback: sharedEconomicHistorySQLBody(t, fact)}
	attempt.Digest = durableSharedSpoolDigest(t, attempt)
	return attempt
}

func (f sharedEconomicAcceptSQLFixture) requireAdmission(t *testing.T, db *gorm.DB) {
	t.Helper()
	var receipt entity.AdmissionEconomicReceipt
	if err := db.Where("installation_id = ? AND request_digest = ?", f.intent.Owner.InstallationID, sharedEconomicIdentityDigest(f.intent.RequestID)).Take(&receipt).Error; err != nil {
		t.Fatal("persisted exact accepted receipt", err)
	}
	if !bytes.Equal(receipt.IntentBody, sharedEconomicHistorySQLBody(t, f.intent)) {
		t.Fatal("accepted receipt rewrote original bytes")
	}
	want, err := eventqueue.QuotaCounterDelta(eventqueue.QuotaCounter{MoneyHeld: map[string]string{}, MoneyUsed: map[string]string{}},
		eventqueue.QuotaCounterContribution{Bound: f.intent.Bound}, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range f.intent.Policies {
		var account entity.AdmissionEconomicAccount
		if err := db.Where("installation_id = ? AND account_digest = ?", f.intent.Owner.InstallationID, sharedEconomicIdentityDigest(policy.Account)).Take(&account).Error; err != nil || account.RPMCount != 1 || account.ExecutorCount != 1 {
			t.Fatal("one logical request must retain one RPM and one executor per account", err)
		}
		var counter entity.AdmissionEconomicCounter
		if err := db.Where("installation_id = ? AND account_digest = ? AND window_name = ?", account.InstallationID, account.AccountDigest, "active").Take(&counter).Error; err != nil {
			t.Fatal(err)
		}
		var actual eventqueue.QuotaCounter
		if !sharedEconomicDecodeBody(counter.CounterBody, &actual) || !reflect.DeepEqual(actual, want) {
			t.Fatal("accepted active projection duplicated or reduced original maximum")
		}
	}
	var count int64
	if err := db.Model(&entity.AdmissionEconomicContribution{}).Where("installation_id = ? AND request_digest = ?", receipt.InstallationID, receipt.RequestDigest).Count(&count).Error; err != nil || count != int64(3*len(f.intent.Policies)) {
		t.Fatal("one exact RPM/executor/active contribution per account", err, count)
	}
}

type sharedEconomicAcceptSQLState struct {
	domains       []entity.AdmissionEconomicDomain
	receipts      []entity.AdmissionEconomicReceipt
	attempts      []entity.AdmissionEconomicAttempt
	accounts      []entity.AdmissionEconomicAccount
	counters      []entity.AdmissionEconomicCounter
	contributions []entity.AdmissionEconomicContribution
	invalid       []entity.AdmissionEconomicInvalidBound
}

func sharedEconomicAcceptSQLSnapshot(t *testing.T, db *gorm.DB) sharedEconomicAcceptSQLState {
	t.Helper()
	state := sharedEconomicAcceptSQLState{}
	for _, query := range []struct {
		destination any
		order       string
	}{
		{&state.domains, "installation_id"},
		{&state.receipts, "installation_id, request_digest"},
		{&state.attempts, "installation_id, request_digest, number"},
		{&state.accounts, "installation_id, account_digest"},
		{&state.counters, "installation_id, account_digest, window_name"},
		{&state.contributions, "installation_id, account_digest, request_digest, kind, window_name"},
		{&state.invalid, "installation_id, revision_digest"},
	} {
		if err := db.Order(query.order).Find(query.destination).Error; err != nil {
			t.Fatal("complete seven-table economic snapshot", err)
		}
	}
	return state
}

func sharedEconomicAcceptSQLRequireSnapshot(t *testing.T, db *gorm.DB, before sharedEconomicAcceptSQLState) {
	t.Helper()
	if after := sharedEconomicAcceptSQLSnapshot(t, db); !reflect.DeepEqual(before, after) {
		t.Fatal("refused or replayed operation changed economic state")
	}
}
