package handler

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
)

// One package owns the complete disposable database lifecycle. Refuse to reset
// any database except the explicit routex_test integration database.
func TestIdentityIntegration(t *testing.T) {
	for _, driver := range []string{"postgres", "mysql"} {
		t.Run(driver, func(t *testing.T) {
			dsn := os.Getenv("ROUTEX_TEST_" + strings.ToUpper(driver) + "_DSN")
			if dsn == "" {
				t.Skip("set ROUTEX_TEST_" + strings.ToUpper(driver) + "_DSN for real database tests")
			}
			db, err := database.Open(context.Background(), driver, dsn)
			if err != nil {
				t.Fatal(err)
			}
			db = db.Session(&gorm.Session{Logger: logger.Discard})
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := sqlDB.Close(); err != nil {
					t.Error(err)
				}
			})
			query := "SELECT current_database()"
			if driver == "mysql" {
				query = "SELECT DATABASE()"
			}
			var name string
			if err := db.Raw(query).Scan(&name).Error; err != nil {
				t.Fatal(err)
			}
			if name != "routex_test" {
				t.Fatal("integration tests require a dedicated database named routex_test")
			}
			reset := func() {
				for _, table := range []string{"project_model_requests", "price_rates", "model_prices", "pricing_exchange_rates", "pricing_settings", "offboarding_cases", "project_api_key_models", "project_api_keys", "project_model_grants", "project_managers", "projects", "team_model_grants", "team_memberships", "teams", "runtime_publications", "user_roles", "role_permissions", "roles", "governance_settings", "call_attempts", "call_records", "api_key_models", "api_keys", "audit_events", "user_model_grants", "model_provider_bindings", "model_names", "credential_model_accesses", "provider_models", "provider_credentials", "provider_connections", "providers", "models", "sessions", "users", "installations", "schema_migrations", "examples"} {
					if err := db.Migrator().DropTable(table); err != nil {
						t.Fatal(err)
					}
				}
			}
			reset()
			t.Cleanup(reset)
			// Simulate an existing bootstrap database and preserve its data.
			if err := db.AutoMigrate(&entity.Example{}); err != nil {
				t.Fatal(err)
			}
			example := entity.Example{Title: "existing bootstrap data"}
			if err := db.Create(&example).Error; err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			migrationErrors := make(chan error, 2)
			for range 2 {
				wg.Go(func() { migrationErrors <- database.Migrate(context.Background(), db) })
			}
			wg.Wait()
			close(migrationErrors)
			for err := range migrationErrors {
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := database.Migrate(context.Background(), db); err != nil {
				t.Fatal(err)
			}
			var versions int64
			if err := db.Table("schema_migrations").Count(&versions).Error; err != nil || versions != 12 {
				t.Fatalf("migration ledger: %d, %v", versions, err)
			}
			var preserved entity.Example
			if err := db.First(&preserved, example.ID).Error; err != nil || preserved.Title != example.Title {
				t.Fatalf("legacy data not preserved: %v", err)
			}
			// Unknown schema versions must stop an older binary before use.
			if err := db.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (999, 'test')").Error; err != nil {
				t.Fatal(err)
			}
			if err := database.Migrate(context.Background(), db); err == nil {
				t.Fatal("future schema version must be rejected")
			}
			if err := db.Exec("DELETE FROM schema_migrations WHERE version = 999").Error; err != nil {
				t.Fatal(err)
			}
			for _, index := range []string{"idx_sessions_user_id", "idx_sessions_expires_at"} {
				if !db.Migrator().HasIndex(&entity.Session{}, index) {
					t.Fatalf("missing session index %s", index)
				}
			}
			orphan := entity.Session{ID: "ses_orphan", UserID: "usr_missing", TokenHash: strings.Repeat("a", 64), ExpiresAt: time.Now().Add(time.Hour)}
			if err := db.Create(&orphan).Error; err == nil {
				t.Fatal("orphan session must be rejected by database FK")
			}
			// New GORM versions must rebuild a partially applied version's tables
			// without changing unrelated released tables or losing existing rows.
			if err := db.Migrator().DropTable(&entity.CallAttempt{}); err != nil {
				t.Fatal(err)
			}
			if err := db.Table("schema_migrations").Where("version = ?", 5).Delete(&struct{}{}).Error; err != nil {
				t.Fatal(err)
			}
			if err := database.Migrate(context.Background(), db); err != nil {
				t.Fatal(err)
			}
			for _, index := range []string{"idx_calls_owner_time", "idx_calls_time"} {
				if !db.Migrator().HasIndex(&entity.CallRecord{}, index) {
					t.Fatalf("missing GORM-created index %s", index)
				}
			}
			orphanAttempt := entity.CallAttempt{ID: "att_orphan", RequestID: "req_missing", StartedAt: time.Now(), CompletedAt: time.Now()}
			if err := db.Create(&orphanAttempt).Error; err == nil {
				t.Fatal("GORM-created request foreign key must reject orphan attempts")
			}
			testIdentityLifecycle(t, db)
			for _, test := range []struct {
				name string
				run  func(*testing.T, *gorm.DB)
			}{{"catalog", testCatalogLifecycle}, {"keys", testKeyLifecycle}, {"gateway", testGatewayLifecycle}, {"calls", testCallLifecycle}, {"account", testAccountLifecycle}, {"governance", testGovernanceLifecycle}, {"runtime", testRuntimeLifecycle}, {"resources", testResourceLifecycle}, {"recorder", testRecorderLifecycle}, {"project_keys", testProjectKeyLifecycle}, {"personal_key_rotation", testPersonalKeyRotationLifecycle}, {"offboarding", testOffboardingLifecycle}, {"pricing", testPricingLifecycle}, {"project_requests", testProjectRequestLifecycle}} {
				reset()
				if err := database.Migrate(context.Background(), db); err != nil {
					t.Fatal(err)
				}
				t.Run(test.name, func(t *testing.T) { test.run(t, db) })
			}
		})
	}
}

func identityRouter(t *testing.T, db *gorm.DB) *fox.Engine {
	t.Helper()
	svc, err := service.New(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	return router
}

func identityRequest(router http.Handler, method, path, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func expectStatus(t *testing.T, res *httptest.ResponseRecorder, status int) {
	t.Helper()
	if res.Code != status {
		t.Fatalf("status %d, want %d; response: %s", res.Code, status, res.Body.String())
	}
}

func readIdentity(t *testing.T, res *httptest.ResponseRecorder) (SessionResponse, *http.Cookie) {
	t.Helper()
	var data SessionResponse
	if err := json.Unmarshal(res.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	cookies := res.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie, got %d", len(cookies))
	}
	return data, cookies[0]
}

func testIdentityLifecycle(t *testing.T, db *gorm.DB) {
	router := identityRouter(t, db)
	res := identityRequest(router, "GET", "/api/v1/setup", "", nil, "")
	expectStatus(t, res, 200)
	if !strings.Contains(res.Body.String(), `"initialized":false`) {
		t.Fatal("new database must be uninitialized")
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", nil, ""), 401)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/status", "", nil, ""), 401)

	for _, body := range []string{`{"email":"bad","password":"valid-password","name":"Admin"}`, `{"email":"admin@example.com","password":"short","name":"Admin"}`, `{"email":"admin@example.com","password":"valid-password","name":" "}`, `{"password": "secret-parser-value", "email": 123}`} {
		res := identityRequest(router, "POST", "/api/v1/setup", body, nil, "")
		expectStatus(t, res, 400)
		if strings.Contains(res.Body.String(), "secret-parser-value") {
			t.Fatal("binding error exposed secret")
		}
	}
	expectStatus(t, identityRequest(router, "POST", "/api/v1/setup", strings.Repeat("x", 5000), nil, ""), 400)

	const setupBody = `{"email":" Admin@Example.com ","password":"valid-password","name":"First admin"}`
	responses := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { responses <- identityRequest(router, "POST", "/api/v1/setup", setupBody, nil, "") })
	}
	wg.Wait()
	close(responses)
	var created *httptest.ResponseRecorder
	statuses := map[int]int{}
	for response := range responses {
		statuses[response.Code]++
		if response.Code == 201 {
			created = response
		}
	}
	if statuses[201] != 1 || statuses[409] != 1 {
		t.Fatalf("concurrent setup results = %v", statuses)
	}
	data, cookie := readIdentity(t, created)
	if data.User.Email != "admin@example.com" || data.User.Role != "admin" || !strings.HasPrefix(data.User.ID, "usr_") || len(data.CSRFToken) != 64 {
		t.Fatalf("unexpected safe identity DTO: %+v", data.User)
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Secure || cookie.Path != "/" || cookie.MaxAge != 604800 {
		t.Fatal("incorrect session cookie policy")
	}
	if created.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("identity response must not be cached")
	}
	if strings.Contains(created.Body.String(), cookie.Value) || strings.Contains(created.Body.String(), "password") {
		t.Fatal("identity response exposed credentials")
	}
	var users []entity.User
	if err := db.Find(&users).Error; err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || bcrypt.CompareHashAndPassword([]byte(users[0].PasswordHash), []byte("valid-password")) != nil {
		t.Fatal("one administrator with bcrypt hash expected")
	}
	var sessions []entity.Session
	if err := db.Find(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].TokenHash != secret.SHA256Hex(cookie.Value) || sessions[0].TokenHash == cookie.Value {
		t.Fatal("session must persist digest only")
	}
	originalExpiry := sessions[0].ExpiresAt
	expectStatus(t, identityRequest(router, "POST", "/api/v1/setup", setupBody, nil, ""), 409)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/status", "", cookie, ""), 200)
	// A fresh service/router proves authentication is stored in the database.
	restarted := identityRouter(t, db)
	expectStatus(t, identityRequest(restarted, "GET", "/api/v1/auth/session", "", cookie, ""), 200)
	var persisted entity.Session
	if err := db.First(&persisted, "id = ?", sessions[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if !persisted.ExpiresAt.Equal(originalExpiry) {
		t.Fatal("session reads must not slide expiration")
	}

	for _, body := range []string{`{"email":"admin@example.com","password":"wrong-password"}`, `{"email":"unknown@example.com","password":"wrong-password"}`, `{"email":"ádmin@example.com","password":"valid-password"}`} {
		res := identityRequest(router, "POST", "/api/v1/auth/login", body, nil, "")
		expectStatus(t, res, 401)
		if res.Body.String() != "{\"code\":401,\"message\":\"unauthorized\"}" {
			t.Fatalf("unexpected login error: %s", res.Body.String())
		}
	}
	// Both databases must distinguish accent-bearing email addresses. MySQL's
	// default accent-insensitive collation would otherwise authenticate admin.
	accentUser := entity.User{ID: "usr_accent_test", Email: "ádmin@example.com", Name: "Another user", PasswordHash: users[0].PasswordHash, Role: entity.RoleMember}
	if err := db.Create(&accentUser).Error; err != nil {
		t.Fatalf("accent-distinct email should be unique: %v", err)
	}
	accentResponse := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"ádmin@example.com","password":"valid-password"}`, nil, "")
	expectStatus(t, accentResponse, 200)
	accentIdentity, _ := readIdentity(t, accentResponse)
	if accentIdentity.User.ID != accentUser.ID {
		t.Fatal("accent-distinct email selected another identity")
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", &http.Cookie{Name: sessionCookie, Value: strings.Repeat("z", 43)}, ""), 401)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/auth/logout", "", cookie, ""), 403)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/auth/logout", "", cookie, "wrong"), 403)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", cookie, ""), 200)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/auth/logout", "", cookie, data.CSRFToken), 204)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", cookie, ""), 401)

	login := func() (SessionResponse, *http.Cookie) {
		res := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"ADMIN@example.com","password":"valid-password"}`, nil, "")
		expectStatus(t, res, 200)
		return readIdentity(t, res)
	}
	_, cookie = login()
	if err := db.Model(&entity.Session{}).Where("token_hash = ?", secret.SHA256Hex(cookie.Value)).Update("expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", cookie, ""), 401)
	_, cookie = login()
	if err := db.Model(&entity.User{}).Where("id = ?", data.User.ID).Update("role", entity.RoleMember).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", cookie, ""), 200)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/status", "", cookie, ""), 403)
	if err := db.Model(&entity.User{}).Where("id = ?", data.User.ID).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", cookie, ""), 401)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"admin@example.com","password":"valid-password"}`, nil, ""), 401)
}

func TestIdentityOriginAndCookies(t *testing.T) {
	for _, tc := range []struct {
		name, origin, fetchSite string
		tls                     bool
		want                    int
	}{
		{"same origin", "http://routex.test", "same-origin", false, 204},
		{"CLI", "", "", false, 204},
		{"foreign origin", "https://evil.test", "", false, 403},
		{"null origin", "null", "", false, 403},
		{"same site sibling", "", "same-site", false, 403},
		{"cross site", "", "cross-site", false, 403},
		{"HTTPS origin over HTTP", "https://routex.test", "", false, 403},
		{"HTTPS", "https://routex.test", "same-origin", true, 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := fox.New()
			router.RenderErrorFunc = renderAPIError
			router.POST("/test", sameOrigin, func(c *fox.Context) { c.Status(204) })
			req := httptest.NewRequest("POST", "http://routex.test/test", nil)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			req.Header.Set("X-Forwarded-Proto", "https")
			if tc.tls {
				req.TLS = &tls.ConnectionState{}
			}
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			expectStatus(t, res, tc.want)
		})
	}
	for _, secure := range []bool{false, true} {
		t.Run(fmt.Sprintf("secure_cookie_%v", secure), func(t *testing.T) {
			router := fox.New()
			router.GET("/test", func(c *fox.Context) { setSessionCookie(c, &service.Authentication{Token: "test"}); c.Status(204) })
			req := httptest.NewRequest("GET", "http://routex.test/test", nil)
			req.Header.Set("X-Forwarded-Proto", "https")
			if secure {
				req.TLS = &tls.ConnectionState{}
			}
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Result().Cookies()[0].Secure != secure {
				t.Fatal("cookie security must depend only on actual TLS")
			}
		})
	}
}
