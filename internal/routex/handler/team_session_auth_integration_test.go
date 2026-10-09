package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
)

func testTeamSessionAuthLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{91}, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithRuntimeRefreshInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"team-auth-admin@example.invalid","password":"test-only-team-auth-password","name":"Team auth admin"}`, nil, "")
	expectStatus(t, setup, 201)
	administrator, _ := readIdentity(t, setup)
	var admin entity.User
	if err := db.First(&admin, "id = ?", administrator.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	writer, _, _ := createSystemStatusMember(t, svc, router, admin.ID, "team-auth-writer", []string{"members.write"})
	selfDisabled := true
	for _, target := range []string{writer.User.ID, strings.ToUpper(writer.User.ID)} {
		if _, err := svc.UpdateMember(ctx, writer.User.ID, target, &selfDisabled, nil); err == nil {
			t.Fatal("custom member writer bypassed self-mutation protection")
		}
	}
	var savedWriter entity.User
	if err := db.First(&savedWriter, "id = ?", writer.User.ID).Error; err != nil || savedWriter.Disabled {
		t.Fatal("forbidden self-mutation changed stored actor", err)
	}
	member := entity.User{ID: "usr_team_auth", Email: "team-auth-member@example.invalid", Name: "Team member", Role: entity.RoleMember, PasswordHash: admin.PasswordHash}
	teamID := "tem_auth_scope"
	bearer := "rx_" + strings.Repeat("q", 43)
	for _, row := range []any{
		&member,
		&entity.Team{ID: teamID, Name: "Session Team", Status: entity.ResourceActive},
		&entity.Team{ID: "tem_auth_other", Name: "Other Team", Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_auth_owner", TeamID: teamID, UserID: admin.ID, Role: entity.TeamOwner, Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_auth_member", TeamID: teamID, UserID: member.ID, Role: entity.TeamMember, Status: entity.ResourceActive},
		&entity.Model{ID: "mdl_team_auth", Status: entity.ResourceActive},
		&entity.TeamModelGrant{TeamID: teamID, ModelID: "mdl_team_auth"},
		&entity.UserModelGrant{UserID: member.ID, ModelID: "mdl_team_auth"},
		&entity.APIKey{ID: "key_team_auth", UserID: member.ID, Name: "Independent Key", Prefix: "rx_test", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_team_auth", ModelID: "mdl_team_auth"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	auth, err := svc.Login(ctx, member.Email, "test-only-team-auth-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime() // Final shutdown joins the live manual publisher.
	before := svc.RuntimeStatus().SnapshotID
	identity, err := svc.RuntimeAuthenticateTeamSession(ctx, auth.Token, teamID)
	if err != nil {
		t.Fatal(err)
	}
	if identity.TeamMembershipID != "tmm_auth_member" || len(identity.ModelIDs) != 1 {
		t.Fatalf("incorrect Team subject: %+v", identity)
	}
	cookie := &http.Cookie{Name: sessionCookie, Value: auth.Token}
	path := "/api/v1/teams/" + teamID + "/inference-models"
	expectStatus(t, identityRequest(router, "GET", path, "", cookie, ""), 200)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/teams/tem_auth_other/inference-models", "", cookie, ""), 403)
	expectStatus(t, identityRequest(router, "GET", path, "", nil, ""), 401)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/teams/"+teamID+"/chat/completions", `{"model":"unknown","messages":[]}`, cookie, ""), 403)
	crossOrigin := httptest.NewRequest("GET", "http://routex.test"+path, nil)
	crossOrigin.AddCookie(cookie)
	crossOrigin.Header.Set("Origin", "https://untrusted.invalid")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, crossOrigin)
	expectStatus(t, recorder, 403)
	// A sole borrowed connection must not block native cookie authorization.
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	previousMax := pool.Stats().MaxOpenConnections
	pool.SetMaxOpenConns(1)
	defer pool.SetMaxOpenConns(previousMax)
	if err := db.Transaction(func(tx *gorm.DB) error {
		bounded, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		if _, err := svc.RuntimeAuthenticateTeamSession(bounded, auth.Token, teamID); err != nil {
			t.Fatal("hot path borrowed the occupied pool", err)
		}
		return svc.ReauthorizeTeamSession(bounded, identity, "mdl_team_auth")
	}); err != nil {
		t.Fatal(err)
	}
	second, err := svc.Login(ctx, member.Email, "test-only-team-auth-password")
	if err != nil {
		t.Fatal(err)
	}
	if svc.RuntimeStatus().SnapshotID != before {
		t.Fatal("Session creation changed Provider configuration")
	}
	if _, err := svc.RuntimeAuthenticateTeamSession(ctx, second.Token, teamID); err != nil {
		t.Fatal("new Session was not synchronously published", err)
	}
	if err := svc.RevokeAccountSession(ctx, member.ID, second.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RuntimeAuthenticateTeamSession(ctx, second.Token, teamID); err == nil {
		t.Fatal("revoked Session still authorized")
	}
	if _, err := svc.AuthenticateAPIKey(ctx, bearer); err != nil {
		t.Fatal("Session revocation affected Personal Key", err)
	}
	rotated, err := svc.ChangePassword(ctx, member.ID, "test-only-team-auth-password", "test-only-team-auth-replacement")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ReauthorizeTeamSession(ctx, identity, "mdl_team_auth"); err == nil {
		t.Fatal("password rotation retained captured Session")
	}
	if _, err := svc.RuntimeAuthenticateTeamSession(ctx, rotated.Token, teamID); err != nil {
		t.Fatal(err)
	}
	members := []service.TeamMemberInput{{UserID: admin.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}}
	if _, err := svc.SetTeamMembers(ctx, admin.ID, teamID, members); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RuntimeAuthenticateTeamSession(ctx, rotated.Token, teamID); err == nil {
		t.Fatal("removed member retained access")
	}
	members = append(members, service.TeamMemberInput{UserID: member.ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	if _, err := svc.SetTeamMembers(ctx, admin.ID, teamID, members); err != nil {
		t.Fatal(err)
	}
	rejoined, err := svc.RuntimeAuthenticateTeamSession(ctx, rotated.Token, teamID)
	if err != nil {
		t.Fatal(err)
	}
	if rejoined.TeamMembershipID == identity.TeamMembershipID {
		t.Fatal("removed/rejoined membership reused old identity")
	}
	if _, err := svc.SetResourceModels(ctx, admin.ID, service.TeamResource, teamID, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReauthorizeTeamSession(ctx, rejoined, "mdl_team_auth"); err == nil {
		t.Fatal("removed grant retained access")
	}
	if _, err := svc.SetResourceModels(ctx, admin.ID, service.TeamResource, teamID, []string{"mdl_team_auth"}); err != nil {
		t.Fatal(err)
	}

	// Target aliases must not write one identity and invalidate another.
	alias := strings.ToUpper(teamID)
	if _, err := svc.UpdateResource(ctx, admin.ID, service.TeamResource, alias, service.ResourceUpdate{Name: &teamID}); err == nil {
		t.Fatal("folded Team update accepted")
	}
	if _, err := svc.SetTeamMembers(ctx, admin.ID, alias, members); err == nil {
		t.Fatal("folded Team member replacement accepted")
	}
	if _, err := svc.SetResourceModels(ctx, admin.ID, service.TeamResource, alias, nil); err == nil {
		t.Fatal("folded Team grant replacement accepted")
	}
	// Fail only post-commit runtime publication, keeping saved resource mutation real.
	callback := "test_team_auth_publication_failure"
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "models" {
			_ = tx.AddError(errors.New("controlled publication outage"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	disabled := entity.ResourceDisabled
	_, writeErr := svc.UpdateResource(ctx, admin.ID, service.TeamResource, teamID, service.ResourceUpdate{Status: &disabled})
	if err := db.Callback().Query().Remove(callback); err != nil {
		t.Fatal(err)
	}
	if writeErr == nil {
		t.Fatal("failed publication claimed success")
	}
	if err := svc.ReauthorizeTeamSession(ctx, rejoined, "mdl_team_auth"); err == nil {
		t.Fatal("saved Team disable was not immediately revoked")
	}
	if _, err := svc.AuthenticateAPIKey(ctx, bearer); err != nil {
		t.Fatal("Team disable affected Personal Key", err)
	}
	enabled := entity.ResourceActive
	if _, err := svc.UpdateResource(ctx, admin.ID, service.TeamResource, teamID, service.ResourceUpdate{Status: &enabled}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RuntimeAuthenticateTeamSession(ctx, rotated.Token, teamID); err != nil {
		t.Fatal(err)
	}

	enrollment, err := svc.BeginMFAEnrollment(ctx, rotated, "test-only-team-auth-replacement")
	if err != nil {
		t.Fatal(err)
	}
	enabledMFA, err := svc.EnableMFA(ctx, rotated, "test-only-team-auth-replacement", enrollment.EnrollmentToken, mfaFixtureCode(t, enrollment.Secret, 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RuntimeAuthenticateTeamSession(ctx, rotated.Token, teamID); err == nil {
		t.Fatal("MFA enable retained old Session")
	}
	rotated = enabledMFA.Authentication
	if _, err := svc.RuntimeAuthenticateTeamSession(ctx, rotated.Token, teamID); err != nil {
		t.Fatal("MFA replacement Session not published", err)
	}
	challengeAuth, challenge, err := svc.BeginLogin(ctx, member.Email, "test-only-team-auth-replacement")
	if err != nil || challengeAuth != nil || challenge == nil {
		t.Fatal("MFA challenge incorrectly authenticated", err)
	}
	if _, err := svc.RuntimeAuthenticateTeamSession(ctx, challenge.ChallengeToken, teamID); err == nil {
		t.Fatal("MFA challenge promoted to Session")
	}
	loginMFA, err := svc.CompleteMFALogin(ctx, challenge.ChallengeToken, service.MFAProof{RecoveryCode: enabledMFA.RecoveryCodes[0]})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RuntimeAuthenticateTeamSession(ctx, loginMFA.Token, teamID); err != nil {
		t.Fatal("MFA login Session not published", err)
	}
	changedMFA, err := svc.ChangeMFA(ctx, rotated, "test-only-team-auth-replacement", service.MFAProof{RecoveryCode: enabledMFA.RecoveryCodes[1]}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RuntimeAuthenticateTeamSession(ctx, loginMFA.Token, teamID); err == nil {
		t.Fatal("MFA change retained other prior Session")
	}
	rotated = changedMFA.Authentication
	if _, err := svc.RuntimeAuthenticateTeamSession(ctx, rotated.Token, teamID); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "models" {
			_ = tx.AddError(errors.New("controlled Session publication outage"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Logout(ctx, rotated); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Remove(callback); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RuntimeAuthenticateTeamSession(ctx, rotated.Token, teamID); err == nil {
		t.Fatal("logout retained Team authorization")
	}
	live, err := svc.Login(ctx, member.Email, "test-only-team-auth-replacement")
	if err != nil {
		t.Fatal(err)
	}
	captured, err := svc.RuntimeAuthenticateTeamSession(ctx, live.Token, teamID)
	if err != nil {
		t.Fatal(err)
	}
	accountDisabled := true
	if _, err := svc.UpdateMember(ctx, strings.ToUpper(admin.ID), member.ID, &accountDisabled, nil); err == nil {
		t.Fatal("folded actor acquired administrator permissions")
	}
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "models" {
			_ = tx.AddError(errors.New("controlled account publication outage"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, aliasErr := svc.UpdateMember(ctx, admin.ID, strings.ToUpper(member.ID), &accountDisabled, nil)
	var stored entity.User
	if err := db.First(&stored, "id = ?", member.ID).Error; err != nil {
		t.Fatal(err)
	}
	if aliasErr == nil || stored.Disabled {
		t.Fatal("folded account target was mutated")
	}
	// Both drivers must reject the alias before mutation, then preserve immediate
	// canonical revocation when the real saved change cannot be published.
	if _, err := svc.UpdateMember(ctx, admin.ID, member.ID, &accountDisabled, nil); err == nil {
		t.Fatal("account publication failure claimed success")
	}
	if err := db.Callback().Query().Remove(callback); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReauthorizeTeamSession(ctx, captured, "mdl_team_auth"); err == nil {
		t.Fatal("disabled canonical actor retained Team admission")
	}
	if _, err := svc.AuthenticateAPIKey(ctx, bearer); err == nil {
		t.Fatal("disabled canonical actor retained Personal Key admission")
	}

}
