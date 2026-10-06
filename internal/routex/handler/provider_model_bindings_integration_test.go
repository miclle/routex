package handler

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

const providerBindingsFixtureIndex = "idx_bindings_provider_model"

type providerBindingsFixtureQuery struct {
	SQL  string
	Vars []any
}
type providerBindingsFixtureContext struct{}

// Observe below authDB's discarded logger, including permission Scan statements.
type providerBindingsFixturePool struct {
	*roleListCountPool
	options []sql.TxOptions
	bounded bool
}

func (p *providerBindingsFixturePool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	if options != nil {
		p.options = append(p.options, *options)
	}
	deadline, ok := ctx.Deadline()
	p.bounded = ok && time.Until(deadline) > 0 && time.Until(deadline) <= 5*time.Second
	return p.roleListCountPool.BeginTx(ctx, options)
}

func seedProviderBindingsFixture(t *testing.T, db *gorm.DB, suffix string) (entity.Provider, entity.ProviderConnection, entity.ProviderModel) {
	t.Helper()
	birth := time.Date(2026, 8, 9, 10, 11, 12, 123000000, time.UTC)
	p := entity.Provider{ID: "prv_" + suffix, Name: "Stored " + suffix, CreatedAt: birth}
	c := entity.ProviderConnection{ID: "con_" + suffix, ProviderID: p.ID, Name: "Connection " + suffix, BaseURL: "https://example.invalid", Protocol: entity.ProtocolOpenAIChat, EgressMode: "default", ETag: "0", CreatedAt: birth}
	pm := entity.ProviderModel{ID: "pmd_" + suffix, ConnectionID: c.ID, UpstreamName: "upstream-" + suffix, Disabled: true, ETag: "0", CreatedAt: birth}
	for _, row := range []any{&p, &c, &pm} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	return p, c, pm
}
func providerBindingsFixtureError(t *testing.T, err error, code int) {
	t.Helper()
	var app *apperrors.Error
	if !errors.As(err, &app) || app.Code != code {
		t.Fatalf("binding projection error got %v; expected %d", err, code)
	}
}
func readProviderBindingsFixture(t *testing.T, router *fox.Engine, cookie *http.Cookie, target string, status int) *service.ProviderModelBindings {
	t.Helper()
	response := identityRequest(router, "GET", "/api/v1/admin/providers/"+target+"/model-bindings", "", cookie, "")
	expectStatus(t, response, status)
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("binding facts cacheable")
	}
	if status != 200 {
		return nil
	}
	var raw map[string]json.RawMessage
	var page service.ProviderModelBindings
	if json.Unmarshal(response.Body.Bytes(), &raw) != nil || len(raw) != 2 || raw["provider_id"] == nil || raw["items"] == nil || json.Unmarshal(response.Body.Bytes(), &page) != nil || page.ProviderID != target || page.Items == nil {
		t.Fatal("binding wire identity/shape")
	}
	var items []map[string]json.RawMessage
	if json.Unmarshal(raw["items"], &items) != nil {
		t.Fatal("binding rows malformed")
	}
	for i, item := range items {
		for _, key := range []string{"provider_model_id", "connection_id", "binding_count", "models"} {
			if item[key] == nil {
				t.Fatal("missing binding fact", key)
			}
		}
		if len(item) != 4 || page.Items[i].BindingCount != len(page.Items[i].Models) || page.Items[i].Models == nil {
			t.Fatal("incomplete or private binding row")
		}
		var models []map[string]json.RawMessage
		if json.Unmarshal(item["models"], &models) != nil {
			t.Fatal("bound names malformed")
		}
		for _, model := range models {
			if len(model) != 2 || model["id"] == nil || model["name"] == nil {
				t.Fatal("private Model metadata leaked")
			}
		}
	}
	return &page
}
func testProviderModelBindingsLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"bindings-admin@example.invalid","password":"binding-test-password","name":"Bindings administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	both, cookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "bindings-both", []string{"providers.read", "models.read_all"})
	_, providerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "bindings-provider", []string{"providers.read"})
	_, modelCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "bindings-model", []string{"models.read_all"})
	_, writerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "bindings-writer", []string{"providers.write"})
	p, c, pm := seedProviderBindingsFixture(t, db, "scope")
	empty := entity.Provider{ID: "prv_empty", Name: "Empty Provider", CreatedAt: p.CreatedAt}
	if err := db.Create(&empty).Error; err != nil {
		t.Fatal(err)
	}
	unbound := entity.ProviderModel{ID: "pmd_empty", ConnectionID: c.ID, UpstreamName: "unbound", ETag: "0", CreatedAt: p.CreatedAt}
	if err := db.Create(&unbound).Error; err != nil {
		t.Fatal(err)
	}
	models := []entity.Model{{ID: "mdl_first", Status: "disabled", CreatedAt: p.CreatedAt}, {ID: "mdl_second", Status: "active", CreatedAt: p.CreatedAt}, {ID: "mdl_alias_target", Status: "active", CreatedAt: p.CreatedAt}}
	if err := db.Create(&models).Error; err != nil {
		t.Fatal(err)
	}
	name := entity.ModelName{Name: "original:model", ModelID: models[0].ID, CurrentModelID: &models[0].ID, CreatedAt: p.CreatedAt}
	old := p.CreatedAt.Add(-time.Hour)
	historical := entity.ModelName{Name: "old:model", ModelID: models[0].ID, ExpiresAt: &old, CreatedAt: p.CreatedAt}
	for _, row := range []any{&name, &historical} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	bindings := []entity.ModelProviderBinding{{ID: "bnd_first", ModelID: models[0].ID, ProviderModelID: pm.ID, Weight: 0, CreatedAt: p.CreatedAt}, {ID: "bnd_second", ModelID: models[1].ID, ProviderModelID: pm.ID, Weight: 100, CreatedAt: p.CreatedAt}}
	if err := db.Create(&bindings).Error; err != nil {
		t.Fatal(err)
	}
	seedProviderBindingsFixture(t, db, "outside")
	var auditBefore int64
	if err := db.Model(&entity.AuditEvent{}).Count(&auditBefore).Error; err != nil {
		t.Fatal(err)
	}
	for _, denied := range []*http.Cookie{providerCookie, modelCookie, writerCookie} {
		readProviderBindingsFixture(t, router, denied, p.ID, 403)
	}
	readProviderBindingsFixture(t, router, nil, p.ID, 401)
	for _, target := range []string{"prv_missing", "prv_SCOPE"} {
		readProviderBindingsFixture(t, router, cookie, target, 404)
	}
	readProviderBindingsFixture(t, router, cookie, "invalid", 400)
	for _, query := range []string{"?limit=1", "?cursor=x", "?provider_id=" + p.ID} {
		expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/providers/"+p.ID+"/model-bindings"+query, "", cookie, ""), 400)
	}
	if len(readProviderBindingsFixture(t, router, cookie, empty.ID, 200).Items) != 0 {
		t.Fatal("empty catalogue invented rows")
	}
	page := readProviderBindingsFixture(t, router, cookie, p.ID, 200)
	if len(page.Items) != 2 || page.Items[0].ProviderModelID != unbound.ID || page.Items[0].BindingCount != 0 || page.Items[1].ProviderModelID != pm.ID || page.Items[1].ConnectionID != c.ID || page.Items[1].BindingCount != 2 || page.Items[1].Models[0].Name == nil || *page.Items[1].Models[0].Name != name.Name || page.Items[1].Models[1].Name != nil {
		t.Fatal("complete stored zero/disabled/current/unknown projection", page)
	}
	readProviderBindingsFixture(t, router, adminCookie, p.ID, 200)
	for _, state := range []string{"Disabled", "OffboardedAt"} {
		var value any = true
		if state == "OffboardedAt" {
			value = time.Now().UTC().Truncate(time.Millisecond)
		}
		if err := db.Model(&entity.User{}).Where("id = ?", both.User.ID).UpdateColumn(state, value).Error; err != nil {
			t.Fatal(err)
		}
		_, err = svc.GetProviderModelBindings(ctx, both.User.ID, p.ID)
		providerBindingsFixtureError(t, err, 401)
		var restored any = false
		if state == "OffboardedAt" {
			restored = nil
		}
		if err := db.Model(&entity.User{}).Where("id = ?", both.User.ID).UpdateColumn(state, restored).Error; err != nil {
			t.Fatal(err)
		}
	}
	_, err = svc.GetProviderModelBindings(ctx, "usr_"+strings.ToUpper(strings.TrimPrefix(both.User.ID, "usr_")), p.ID)
	providerBindingsFixtureError(t, err, 401)
	// Real FK and uniqueness constraints prohibit invented or duplicate bindings.
	for _, bad := range []entity.ModelProviderBinding{{ID: "bnd_missing_model", ModelID: "mdl_missing", ProviderModelID: pm.ID, Weight: 1, CreatedAt: p.CreatedAt}, {ID: "bnd_missing_supply", ModelID: models[0].ID, ProviderModelID: "pmd_missing", Weight: 1, CreatedAt: p.CreatedAt}} {
		if err := db.Create(&bad).Error; !errors.Is(err, gorm.ErrForeignKeyViolated) {
			t.Fatal("binding orphan not rejected", err)
		}
	}
	duplicate := bindings[0]
	duplicate.ID = "bnd_duplicate"
	if err := db.Create(&duplicate).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatal("duplicate pair not rejected", err)
	}
	// Case-insensitive FK equality may accept aliases, but service identity never does.
	alias := entity.ModelProviderBinding{ID: "bnd_alias", ModelID: models[2].ID, ProviderModelID: "pmd_SCOPE", Weight: 1, CreatedAt: p.CreatedAt}
	if err := db.Create(&alias).Error; err == nil {
		readProviderBindingsFixture(t, router, cookie, p.ID, 503)
		if err := db.Where("id = ?", alias.ID).Delete(&entity.ModelProviderBinding{}).Error; err != nil {
			t.Fatal(err)
		}
	} else if !errors.Is(err, gorm.ErrForeignKeyViolated) {
		t.Fatal("unexpected alias persistence failure", err)
	}
	alias.ModelID = "mdl_ALIAS_TARGET"
	alias.ProviderModelID = pm.ID
	if err := db.Create(&alias).Error; err == nil {
		readProviderBindingsFixture(t, router, cookie, p.ID, 503)
		if err := db.Where("id = ?", alias.ID).Delete(&entity.ModelProviderBinding{}).Error; err != nil {
			t.Fatal(err)
		}
	} else if !errors.Is(err, gorm.ErrForeignKeyViolated) {
		t.Fatal("unexpected Model alias persistence failure", err)
	}
	for _, change := range []struct {
		row                         any
		id, column, alias, original string
	}{{&entity.ProviderConnection{}, c.ID, "ProviderID", "prv_SCOPE", p.ID}, {&entity.ProviderModel{}, pm.ID, "ConnectionID", "con_SCOPE", c.ID}} {
		if err := db.Model(change.row).Where("id = ?", change.id).UpdateColumn(change.column, change.alias).Error; err == nil {
			readProviderBindingsFixture(t, router, cookie, p.ID, 503)
			if err := db.Model(change.row).Where("id = ?", change.id).UpdateColumn(change.column, change.original).Error; err != nil {
				t.Fatal(err)
			}
		} else if !errors.Is(err, gorm.ErrForeignKeyViolated) {
			t.Fatal("unexpected parent alias persistence failure", err)
		}
	}
	// An unusable retained current name is null; historical aliases never fill it.
	if err := db.Model(&entity.ModelName{}).Where("name = ?", name.Name).UpdateColumn("Name", "bad name").Error; err != nil {
		t.Fatal(err)
	}
	if readProviderBindingsFixture(t, router, cookie, p.ID, 200).Items[1].Models[0].Name != nil {
		t.Fatal("legacy malformed name substituted")
	}
	if err := db.Model(&entity.ModelName{}).Where("name = ?", "bad name").UpdateColumn("Name", name.Name).Error; err != nil {
		t.Fatal(err)
	}
	// Commit an independent catalogue revision after the binding read. RR keeps
	// names and counts from the original transaction, and the next read advances.
	var swapped atomic.Bool
	callback := "fixture:provider_bindings_snapshot"
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Context.Value(providerBindingsFixtureContext{}) != true || tx.Statement.Table != "model_provider_bindings" || !swapped.CompareAndSwap(false, true) {
			return
		}
		err := db.Transaction(func(write *gorm.DB) error {
			if err := write.Model(&entity.ModelName{}).Where("name = ?", name.Name).UpdateColumn("CurrentModelID", nil).Error; err != nil {
				return err
			}
			next := entity.ModelName{Name: "next:model", ModelID: models[0].ID, CurrentModelID: &models[0].ID, CreatedAt: p.CreatedAt}
			if err := write.Create(&next).Error; err != nil {
				return err
			}
			return write.Create(&entity.ModelProviderBinding{ID: "bnd_next", ModelID: models[1].ID, ProviderModelID: unbound.ID, Weight: 0, CreatedAt: p.CreatedAt}).Error
		})
		if err != nil {
			_ = tx.AddError(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := svc.GetProviderModelBindings(context.WithValue(ctx, providerBindingsFixtureContext{}, true), both.User.ID, p.ID)
	if removeErr := db.Callback().Query().Remove(callback); removeErr != nil {
		t.Fatal(removeErr)
	}
	if err != nil || !swapped.Load() || snapshot.Items[0].BindingCount != 0 || snapshot.Items[1].Models[0].Name == nil || *snapshot.Items[1].Models[0].Name != name.Name {
		t.Fatal("incoherent read snapshot", err, snapshot)
	}
	current := readProviderBindingsFixture(t, router, cookie, p.ID, 200)
	if current.Items[0].BindingCount != 1 || *current.Items[1].Models[0].Name != "next:model" {
		t.Fatal("next transaction did not advance")
	}
	testProviderBindingsFixtureMaximum(t, db, both.User.ID)
	var auditAfter int64
	if err := db.Model(&entity.AuditEvent{}).Count(&auditAfter).Error; err != nil || auditAfter != auditBefore {
		t.Fatal("read-only projection changed audit state", err, auditBefore, auditAfter)
	}
	for _, table := range []string{"call_records", "call_attempts", "api_keys", "project_api_keys"} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("read projection introduced native/Key state", table, count, err)
		}
	}
}

func testProviderBindingsFixtureMaximum(t *testing.T, db *gorm.DB, actorID string) {
	t.Helper()
	p, _, pm := seedProviderBindingsFixture(t, db, "maximum")
	models := make([]entity.Model, 10000)
	bindings := make([]entity.ModelProviderBinding, 10000)
	names := make([]entity.ModelName, 10000)
	for i := range models {
		models[i] = entity.Model{ID: fmt.Sprintf("mdl_max_%05d", i), Status: "disabled", CreatedAt: p.CreatedAt}
		bindings[i] = entity.ModelProviderBinding{ID: fmt.Sprintf("bnd_max_%05d", i), ModelID: models[i].ID, ProviderModelID: pm.ID, Weight: 0, CreatedAt: p.CreatedAt}
		names[i] = entity.ModelName{Name: fmt.Sprintf("maximum:%05d", i), ModelID: models[i].ID, CurrentModelID: &models[i].ID, CreatedAt: p.CreatedAt}
	}
	for _, rows := range []any{&models, &bindings, &names} {
		if err := db.CreateInBatches(rows, 500).Error; err != nil {
			t.Fatal(err)
		}
	}
	var statements atomic.Int64
	readDB := db.Session(&gorm.Session{NewDB: true, Context: context.Background()})
	pool := &providerBindingsFixturePool{roleListCountPool: &roleListCountPool{ConnPool: readDB.Statement.ConnPool, statements: &statements}}
	readDB.ConnPool, readDB.Statement.ConnPool = pool, pool
	readSvc, err := service.New(context.Background(), readDB)
	if err != nil {
		t.Fatal(err)
	}
	var captured []providerBindingsFixtureQuery
	captureName := "fixture:provider_bindings_queries"
	if err := readDB.Callback().Query().After("gorm:query").Register(captureName, func(tx *gorm.DB) {
		if tx.Statement.Context.Value(providerBindingsFixtureContext{}) == true {
			captured = append(captured, providerBindingsFixtureQuery{SQL: tx.Statement.SQL.String(), Vars: append([]any(nil), tx.Statement.Vars...)})
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := readDB.Callback().Query().Remove(captureName); err != nil {
			t.Error(err)
		}
	}()
	statements.Store(0)
	started := time.Now()
	page, err := readSvc.GetProviderModelBindings(context.WithValue(context.Background(), providerBindingsFixtureContext{}, true), actorID, p.ID)
	elapsed := time.Since(started)
	if err != nil || page == nil || len(page.Items) != 1 || page.Items[0].BindingCount != 10000 || len(page.Items[0].Models) != 10000 || statements.Load() != 9 || len(captured) != 7 || !pool.bounded || len(pool.options) != 1 || !pool.options[0].ReadOnly || pool.options[0].Isolation != sql.LevelRepeatableRead || elapsed >= 5*time.Second {
		t.Fatal("maximum completeness/snapshot/query budget", err, statements.Load(), len(captured), elapsed, pool.options)
	}
	for i, m := range page.Items[0].Models {
		if m.ID != models[i].ID || m.Name == nil || *m.Name != names[i].Name {
			t.Fatal("maximum page lost exact ID/name", i)
		}
	}
	t.Logf("10000 complete stored bindings: %d statements (3 authority + 6 catalogue), elapsed=%s, read-only RR timeout=5s", statements.Load(), elapsed)
	// Measure actual plan outputs with the real driver, without logging bound IDs.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range captured {
		if strings.Contains(query.SQL, "role_permissions") || strings.Contains(query.SQL, "users") {
			continue
		}
		measureProviderBindingsFixturePlan(t, db.Name(), sqlDB, query)
	}
	extra := entity.Model{ID: "mdl_overflow", Status: "active", CreatedAt: p.CreatedAt}
	if err := db.Create(&extra).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.ModelProviderBinding{ID: "bnd_overflow", ModelID: extra.ID, ProviderModelID: pm.ID, Weight: 0, CreatedAt: p.CreatedAt}).Error; err != nil {
		t.Fatal(err)
	}
	statements.Store(0)
	page, err = readSvc.GetProviderModelBindings(context.Background(), actorID, p.ID)
	providerBindingsFixtureError(t, err, 422)
	if page != nil || statements.Load() != 7 {
		t.Fatal("overflow returned a partial page or exceeded bounded early read", statements.Load())
	}
}

// Keep only measured plan facts. Conditions and literals are deliberately omitted.
func providerBindingsFixturePlanFacts(value any) any {
	switch v := value.(type) {
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, providerBindingsFixturePlanFacts(item))
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for key, item := range v {
			switch key {
			case "Node Type", "Relation Name", "Index Name", "Plan Rows", "Actual Rows", "Actual Total Time", "Total Cost", "Execution Time", "Planning Time", "access_type", "key", "possible_keys", "rows_examined_per_scan", "rows_produced_per_join", "query_cost":
				out[key] = item
			default:
				if _, ok := item.(map[string]any); ok {
					out[key] = providerBindingsFixturePlanFacts(item)
				}
				if _, ok := item.([]any); ok {
					out[key] = providerBindingsFixturePlanFacts(item)
				}
			}
		}
		return out
	default:
		return nil
	}
}
func measureProviderBindingsFixturePlan(t *testing.T, driver string, pool *sql.DB, query providerBindingsFixtureQuery) {
	t.Helper()
	prefix := "EXPLAIN (ANALYZE, FORMAT JSON) "
	if driver == "mysql" {
		prefix = "EXPLAIN FORMAT=JSON "
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	var body []byte
	if err := pool.QueryRowContext(ctx, prefix+query.SQL, query.Vars...).Scan(&body); err != nil {
		t.Fatal("measured binding plan", err)
	}
	if len(body) == 0 || len(body) > 1024*1024 {
		t.Fatal("unbounded or absent plan")
	}
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	safe, err := json.Marshal(providerBindingsFixturePlanFacts(parsed))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("binding plan driver=%s sql_sha256=%x elapsed=%s facts=%s", driver, sha256.Sum256([]byte(query.SQL)), time.Since(started), safe)
}
func TestProviderModelBindingDriverPlanRedaction(t *testing.T) {
	value := map[string]any{"Plan": map[string]any{"Node Type": "Index Scan", "Index Name": providerBindingsFixtureIndex, "Filter": "secret literal", "Plans": []any{map[string]any{"Actual Rows": 10000, "attached_condition": "private"}}}, "query_block": map[string]any{"key": providerBindingsFixtureIndex, "rows_examined_per_scan": 10000, "possible_keys": []any{providerBindingsFixtureIndex}, "attached_condition": "private"}}
	body, err := json.Marshal(providerBindingsFixturePlanFacts(value))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "secret") || strings.Contains(string(body), "private") || !strings.Contains(string(body), providerBindingsFixtureIndex) || !strings.Contains(string(body), "10000") {
		t.Fatal("plan diagnostic exposes conditions or omits measured facts", string(body))
	}
	if !reflect.DeepEqual(providerBindingsFixturePlanFacts(nil), nil) {
		t.Fatal("unknown plan invented facts")
	}
}
