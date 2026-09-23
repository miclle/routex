package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/service"
)

func testAccountLifecycle(t *testing.T, db *gorm.DB) {
	router := identityRouter(t, db)
	res := identityRequest(router, "POST", "/api/v1/setup", `{"email":"account@example.com","password":"initial-password","name":"Account"}`, nil, "")
	expectStatus(t, res, 201)
	auth, cookie := readIdentity(t, res)
	otherLogin := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"account@example.com","password":"initial-password"}`, nil, "")
	expectStatus(t, otherLogin, 200)
	_, secondCookie := readIdentity(t, otherLogin)
	expectStatus(t, identityRequest(router, "PATCH", "/api/v1/account", `{"name":" "}`, cookie, auth.CSRFToken), 400)
	expectStatus(t, identityRequest(router, "PATCH", "/api/v1/account", `{"name":"Updated"}`, cookie, ""), 403)
	expectStatus(t, identityRequest(router, "PATCH", "/api/v1/account", `{"name":"Updated"}`, cookie, auth.CSRFToken), 200)
	session := identityRequest(router, "GET", "/api/v1/auth/session", "", cookie, "")
	if !strings.Contains(session.Body.String(), `"name":"Updated"`) {
		t.Fatal("profile update did not persist")
	}
	sessions := identityRequest(router, "GET", "/api/v1/account/sessions", "", cookie, "")
	expectStatus(t, sessions, 200)
	var listing AccountSessionsResponse
	if err := json.Unmarshal(sessions.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if len(listing.Items) != 2 || strings.Contains(sessions.Body.String(), "token") {
		t.Fatal("session listing invalid or secret leaked")
	}
	for _, item := range listing.Items {
		if item.Current {
			continue
		}
		svc, err := service.New(context.Background(), db)
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.RevokeAccountSession(context.Background(), "usr_other", item.ID); err == nil {
			t.Fatal("cross-owner session revoke allowed")
		}
		expectStatus(t, identityRequest(router, "DELETE", "/api/v1/account/sessions/"+item.ID, "", cookie, auth.CSRFToken), 204)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", secondCookie, ""), 401)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/account/password", `{"current_password":"incorrect-password","new_password":"updated-password"}`, cookie, auth.CSRFToken), 400)
	changed := identityRequest(router, "POST", "/api/v1/account/password", `{"current_password":"initial-password","new_password":"updated-password"}`, cookie, auth.CSRFToken)
	expectStatus(t, changed, 200)
	newAuth, newCookie := readIdentity(t, changed)
	if newCookie.Value == cookie.Value || newAuth.CSRFToken == auth.CSRFToken {
		t.Fatal("password change must rotate session and CSRF")
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", cookie, ""), 401)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", newCookie, ""), 200)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"account@example.com","password":"initial-password"}`, nil, ""), 401)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"account@example.com","password":"updated-password"}`, nil, ""), 200)
}
