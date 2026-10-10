package handler

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

const samlFixturePassword = "test-only-saml-application-password"
const samlAdminSubject = "Exact-Administrator-Subject"
const samlMemberSubject = "Exact-Member-Subject"

type samlApplicationFixture struct {
	router       *fox.Engine
	service      *service.Service
	signing      samlHTTPFixture
	admin        SessionResponse
	adminCookie  *http.Cookie
	member       SessionResponse
	memberCookie *http.Cookie
}
type samlBrowserCeremony struct {
	start, delivery            *http.Cookie
	requestID, relay, response string
}

// This recorder only models the supported deadline capability for in-process
// HTTP contracts. TestSAMLACSIncompleteNetworkBodyHasFiniteReadAndHandlerJoin separately verifies real transport time.
type samlApplicationRecorder struct{ *httptest.ResponseRecorder }

func (samlApplicationRecorder) SetReadDeadline(time.Time) error { return nil }
func samlApplicationRequest(router http.Handler, method, path string, body any, etag, csrf string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	var raw string
	if body != nil {
		if text, ok := body.(string); ok {
			raw = text
		} else {
			b, _ := json.Marshal(body)
			raw = string(b)
		}
	}
	req := httptest.NewRequest(method, "https://routex.test"+path, strings.NewReader(raw))
	req.Header.Set("Origin", "https://routex.test")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if etag != "" {
		req.Header.Set("If-Match", `"`+etag+`"`)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	for _, cookie := range cookies {
		if cookie != nil {
			req.AddCookie(cookie)
		}
	}
	out := httptest.NewRecorder()
	router.ServeHTTP(samlApplicationRecorder{out}, req)
	return out
}
func samlApplicationStatus(t *testing.T, res *httptest.ResponseRecorder, want int) {
	t.Helper()
	if res.Code != want {
		t.Fatalf("SAML HTTP status %d, want %d", res.Code, want)
	}
}
func samlApplicationDecode[T any](t *testing.T, res *httptest.ResponseRecorder, want int) T {
	t.Helper()
	samlApplicationStatus(t, res, want)
	var v T
	if json.Unmarshal(res.Body.Bytes(), &v) != nil {
		t.Fatal("decode SAML public response")
	}
	return v
}
func samlApplicationCookie(t *testing.T, res *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	var found *http.Cookie
	for _, c := range res.Result().Cookies() {
		if c.Name == name && c.Value != "" && c.MaxAge >= 0 {
			if found != nil {
				t.Fatal("duplicate issued cookie")
			}
			found = c
		}
	}
	if found == nil {
		t.Fatal("missing expected issued cookie")
	}
	return found
}
func samlApplicationSession(t *testing.T, res *httptest.ResponseRecorder) (SessionResponse, *http.Cookie) {
	t.Helper()
	data := samlApplicationDecode[SessionResponse](t, res, 200)
	if data.User.ID == "" || data.CSRFToken == "" {
		t.Fatal("missing admitted Session")
	}
	return data, samlApplicationCookie(t, res, sessionCookie)
}
func (f *samlApplicationFixture) start(t *testing.T, path, subject string, cookie *http.Cookie, csrf, etag string) samlBrowserCeremony {
	t.Helper()
	body := any(struct{}{})
	if path != "/api/v1/auth/saml/start" {
		body = service.SAMLIdentityInput{Password: samlFixturePassword, Reason: "Review exact existing member identity"}
	}
	res := samlApplicationRequest(f.router, "POST", path, body, etag, csrf, cookie)
	start := samlApplicationDecode[service.SAMLStart](t, res, 200)
	if start.Cookie != "" || !start.ExpiresAt.IsZero() {
		t.Fatal("private correlation material serialized")
	}
	c := samlBrowserCeremony{start: samlApplicationCookie(t, res, samlStartCookie)}
	u, e := url.Parse(start.AuthorizationURL)
	if e != nil || u.Scheme != "https" || u.Host != "idp.example" {
		t.Fatal("invalid redirect destination")
	}
	c.relay = u.Query().Get("RelayState")
	encoded, e := base64.StdEncoding.DecodeString(u.Query().Get("SAMLRequest"))
	if e != nil {
		t.Fatal("decode actual AuthnRequest")
	}
	reader := flate.NewReader(bytes.NewReader(encoded))
	raw, e := io.ReadAll(io.LimitReader(reader, 32<<10))
	closeErr := reader.Close()
	if e != nil || closeErr != nil {
		t.Fatal("decode bounded redirect request")
	}
	doc := etree.NewDocument()
	if doc.ReadFromBytes(raw) != nil || doc.Root() == nil {
		t.Fatal("parse actual AuthnRequest")
	}
	c.requestID = doc.Root().SelectAttrValue("ID", "")
	if !samlOpaqueCookie(c.relay) || len(c.requestID) != 44 || !strings.HasPrefix(c.requestID, "_") {
		t.Fatal("missing independent request and relay identities")
	}
	c.response = f.signing.response(t, c.requestID, subject, "Assertion")
	return c
}
func (f *samlApplicationFixture) begin(t *testing.T, path, subject string, cookie *http.Cookie, csrf, etag string) samlBrowserCeremony {
	t.Helper()
	c := f.start(t, path, subject, cookie, csrf, etag)
	staged := f.stage(t, c.relay, c.response)
	c.delivery = samlApplicationCookie(t, staged, samlDeliveryCookie)
	return c
}
func (f *samlApplicationFixture) stage(t *testing.T, relay, response string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"SAMLResponse": {response}, "RelayState": {relay}}
	req := httptest.NewRequest("POST", "https://routex.test"+samlACSPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Cross-site staging deliberately carries no ambient Session or Strict cookie.
	req.Header.Set("Origin", "https://idp.example")
	out := httptest.NewRecorder()
	f.router.ServeHTTP(samlApplicationRecorder{out}, req)
	samlApplicationStatus(t, out, 303)
	if out.Header().Get("Location") != "/auth/saml/complete" || out.Header().Get("Cache-Control") != "private, no-store" || out.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("ACS staging location/privacy changed")
	}
	for _, c := range out.Result().Cookies() {
		if c.Name == sessionCookie {
			t.Fatal("ACS issued a Session")
		}
	}
	return out
}
func (f *samlApplicationFixture) finish(t *testing.T, c samlBrowserCeremony, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	return samlApplicationRequest(f.router, "POST", "/api/v1/auth/saml/complete", struct{}{}, "", csrf, cookie, c.start, c.delivery)
}
func (f *samlApplicationFixture) login(t *testing.T, subject string) (SessionResponse, *http.Cookie) {
	t.Helper()
	c := f.begin(t, "/api/v1/auth/saml/start", subject, nil, "", "")
	return samlApplicationSession(t, f.finish(t, c, nil, ""))
}
func newSAMLApplicationFixture(t *testing.T, db *gorm.DB) *samlApplicationFixture {
	t.Helper()
	store, e := secretstore.New(bytes.Repeat([]byte{37}, 32))
	if e != nil {
		t.Fatal("create SAML fixture store")
	}
	svc, e := service.New(context.Background(), db, service.WithCredentialStorage(store))
	if e != nil {
		t.Fatal("create SAML service")
	}
	f := &samlApplicationFixture{service: svc, router: fox.New(), signing: newSAMLHTTPFixture(t, false)}
	New(svc).RegisterRoutes(f.router)
	public := samlApplicationDecode[service.SAMLPublic](t, samlApplicationRequest(f.router, "GET", "/api/v1/auth/saml", nil, "", ""), 200)
	if public.Available || public.Name != "" {
		t.Fatal("SAML did not default disabled")
	}
	setup := samlApplicationRequest(f.router, "POST", "/api/v1/setup", SetupRequest{Email: "saml-admin@example.invalid", Password: samlFixturePassword, Name: "SAML administrator"}, "", "")
	samlApplicationStatus(t, setup, 201)
	f.admin, f.adminCookie = readIdentity(t, setup)
	var u entity.User
	if db.Where("id = ?", f.admin.User.ID).Take(&u).Error != nil {
		t.Fatal("read existing administrator")
	}
	member := entity.User{ID: "usr_saml_existing_member", Email: "saml-member@example.invalid", Name: "SAML member", Role: entity.RoleMember, PasswordHash: u.PasswordHash}
	if db.Create(&member).Error != nil {
		t.Fatal("seed admitted member")
	}
	f.member, f.memberCookie = samlApplicationSession(t, samlApplicationRequest(f.router, "POST", "/api/v1/auth/login", LoginRequest{Email: member.Email, Password: samlFixturePassword}, "", ""))
	view := samlApplicationDecode[service.SAMLProviderView](t, samlApplicationRequest(f.router, "GET", "/api/v1/admin/auth/saml", nil, "", "", f.adminCookie), 200)
	in := service.SAMLProviderInput{Name: "Enterprise identity", IDPIssuer: f.signing.config.IDPIssuer, SSOURL: f.signing.config.SSOURL, SPEntityID: f.signing.config.SPEntityID, ACSURL: f.signing.config.ACSURL, SigningCertificatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.signing.der})), Reason: "Configure explicit trusted identity provider"}
	view = samlApplicationDecode[service.SAMLProviderView](t, samlApplicationRequest(f.router, "PUT", "/api/v1/admin/auth/saml", in, view.ReviewETag, f.admin.CSRFToken, f.adminCookie), 200)
	verify := f.begin(t, "/api/v1/admin/auth/saml/verify", samlAdminSubject, f.adminCookie, f.admin.CSRFToken, view.ReviewETag)
	result := samlApplicationDecode[struct {
		Kind string `json:"kind"`
	}](t, f.finish(t, verify, f.adminCookie, f.admin.CSRFToken), 200)
	if result.Kind != "verified" {
		t.Fatal("verification did not confirm exact administrator")
	}
	view = samlApplicationDecode[service.SAMLProviderView](t, samlApplicationRequest(f.router, "GET", "/api/v1/admin/auth/saml", nil, "", "", f.adminCookie), 200)
	view = samlApplicationDecode[service.SAMLProviderView](t, samlApplicationRequest(f.router, "PUT", "/api/v1/admin/auth/saml/status", service.SAMLStatusInput{Enabled: true, Reason: "Enable reviewed verified identity"}, view.ReviewETag, f.admin.CSRFToken, f.adminCookie), 200)
	if !view.Enabled || !view.Verified {
		t.Fatal("explicit enable failed")
	}
	account := samlApplicationDecode[service.SAMLAccountView](t, samlApplicationRequest(f.router, "GET", "/api/v1/account/identity/saml", nil, "", "", f.memberCookie), 200)
	bind := f.begin(t, "/api/v1/account/identity/saml/bind", samlMemberSubject, f.memberCookie, f.member.CSRFToken, account.ReviewETag)
	result = samlApplicationDecode[struct {
		Kind string `json:"kind"`
	}](t, f.finish(t, bind, f.memberCookie, f.member.CSRFToken), 200)
	if result.Kind != "bound" {
		t.Fatal("member binding was not explicit")
	}
	return f
}
func testSAMLLifecycle(t *testing.T, db *gorm.DB) {
	f := newSAMLApplicationFixture(t, db)
	t.Run("management_authority", func(t *testing.T) {
		samlApplicationStatus(t, samlApplicationRequest(f.router, "GET", "/api/v1/admin/auth/saml", nil, "", "", f.memberCookie), 403)
		samlApplicationStatus(t, samlApplicationRequest(f.router, "POST", "/api/v1/auth/saml/start", struct{}{}, "", f.member.CSRFToken, f.memberCookie), 403)
		samlApplicationStatus(t, samlApplicationRequest(f.router, "GET", "/api/v1/auth/saml/acs?RelayState=opaque", nil, "", ""), 404)
	})
	t.Run("dual_browser_proofs_and_single_consumption", func(t *testing.T) {
		a := f.begin(t, "/api/v1/auth/saml/start", samlMemberSubject, nil, "", "")
		b := f.begin(t, "/api/v1/auth/saml/start", samlMemberSubject, nil, "", "")
		samlApplicationStatus(t, samlApplicationRequest(f.router, "POST", "/api/v1/auth/saml/complete", struct{}{}, "", "", a.delivery), 401)
		samlApplicationStatus(t, samlApplicationRequest(f.router, "POST", "/api/v1/auth/saml/complete", struct{}{}, "", "", a.start, b.delivery), 401)
		before := int64(0)
		if db.Model(&entity.Session{}).Count(&before).Error != nil {
			t.Fatal("count Sessions")
		}
		res := f.finish(t, a, nil, "")
		session, cookie := samlApplicationSession(t, res)
		if session.User.ID != f.member.User.ID {
			t.Fatal("incorrect admitted member")
		}
		var row entity.Session
		if db.Where("token_hash = ?", secret.SHA256Hex(cookie.Value)).Take(&row).Error != nil || row.PrimaryMethod != "saml" || row.SAMLBindingID == "" || row.SAMLUserCreatedAt == nil {
			t.Fatal("missing persisted primary provenance")
		}
		samlApplicationStatus(t, f.finish(t, a, nil, ""), 401)
		after := int64(0)
		if db.Model(&entity.Session{}).Count(&after).Error != nil || after != before+1 {
			t.Fatal("replayed completion issued a Session")
		}
		replay := f.stage(t, a.relay, a.response)
		for _, c := range replay.Result().Cookies() {
			if c.Name == samlDeliveryCookie && c.Value != "" {
				t.Fatal("replayed ACS produced fresh authority")
			}
		}
		samlApplicationSession(t, f.finish(t, b, nil, ""))
	})
	t.Run("unknown_and_case_variant_subjects_never_admit", func(t *testing.T) {
		var before int64
		if db.Model(&entity.User{}).Count(&before).Error != nil {
			t.Fatal("count users")
		}
		for _, subject := range []string{"Unbound-Subject", strings.ToLower(samlMemberSubject), samlMemberSubject + " "} {
			c := f.begin(t, "/api/v1/auth/saml/start", subject, nil, "", "")
			samlApplicationStatus(t, f.finish(t, c, nil, ""), 401)
		}
		var after int64
		if db.Model(&entity.User{}).Count(&after).Error != nil || after != before {
			t.Fatal("SAML provisioned an unknown user")
		}
	})
	t.Run("signed_assertion_tamper_has_no_delivery", func(t *testing.T) {
		c := f.start(t, "/api/v1/auth/saml/start", samlMemberSubject, nil, "", "")
		doc := samlFixtureDecode(t, c.response)
		samlFixtureElement(t, doc, "./Response/Assertion/Subject/NameID").SetText("Changed-Subject")
		res := f.stage(t, c.relay, samlFixtureEncoded(t, doc))
		for _, cookie := range res.Result().Cookies() {
			if cookie.Value != "" {
				t.Fatal("invalid/replayed assertion issued authority")
			}
		}
	})
	t.Run("response_signature_and_scoped_default_namespaces", func(t *testing.T) {
		c := f.start(t, "/api/v1/auth/saml/start", samlMemberSubject, nil, "", "")
		doc := f.signing.defaultDocument(t)
		doc.Root().CreateAttr("InResponseTo", c.requestID)
		samlFixtureElement(t, doc, "./Response/Assertion/Subject/SubjectConfirmation/SubjectConfirmationData").CreateAttr("InResponseTo", c.requestID)
		samlFixtureElement(t, doc, "./Response/Assertion/Subject/NameID").SetText(samlMemberSubject)
		c.response = f.signing.sign(t, doc, "Response")
		c.delivery = samlApplicationCookie(t, f.stage(t, c.relay, c.response), samlDeliveryCookie)
		session, _ := samlApplicationSession(t, f.finish(t, c, nil, ""))
		if session.User.ID != f.member.User.ID {
			t.Fatal("default-namespace response changed identity")
		}
	})
	t.Run("explicit_abandon_and_current_session_csrf", func(t *testing.T) {
		c := f.begin(t, "/api/v1/auth/saml/start", samlMemberSubject, nil, "", "")
		samlApplicationStatus(t, f.finish(t, c, f.memberCookie, ""), 403)
		samlApplicationStatus(t, samlApplicationRequest(f.router, "POST", "/api/v1/auth/saml/abandon", struct{}{}, "", "", f.memberCookie, c.start, c.delivery), 403)
		res := samlApplicationRequest(f.router, "POST", "/api/v1/auth/saml/abandon", struct{}{}, "", f.member.CSRFToken, f.memberCookie, c.start, c.delivery)
		samlApplicationStatus(t, res, 204)
		for _, name := range []string{samlStartCookie, samlDeliveryCookie} {
			found := false
			for _, cookie := range res.Result().Cookies() {
				if cookie.Name == name && cookie.MaxAge < 0 && cookie.Value == "" {
					found = true
				}
			}
			if !found {
				t.Fatal("abandon did not clear both host proofs")
			}
		}
	})
	t.Run("name_only_review_preserves_live_proof", func(t *testing.T) {
		_, cookie := f.login(t, samlMemberSubject)
		view := samlApplicationDecode[service.SAMLProviderView](t, samlApplicationRequest(f.router, "GET", "/api/v1/admin/auth/saml", nil, "", "", f.adminCookie), 200)
		in := service.SAMLProviderInput{Name: "Renamed enterprise identity", IDPIssuer: view.IDPIssuer, SSOURL: view.SSOURL, SPEntityID: view.SPEntityID, ACSURL: view.ACSURL, SigningCertificatePEM: view.SigningCertificatePEM, Reason: "Rename without changing identity trust"}
		saved := samlApplicationDecode[service.SAMLProviderView](t, samlApplicationRequest(f.router, "PUT", "/api/v1/admin/auth/saml", in, view.ReviewETag, f.admin.CSRFToken, f.adminCookie), 200)
		if !saved.Enabled || !saved.Verified || saved.Name != in.Name {
			t.Fatal("name-only save changed verification or enablement")
		}
		samlApplicationStatus(t, samlApplicationRequest(f.router, "GET", "/api/v1/auth/session", nil, "", "", cookie), 200)
		samlApplicationStatus(t, samlApplicationRequest(f.router, "PUT", "/api/v1/admin/auth/saml", in, view.ReviewETag, f.admin.CSRFToken, f.adminCookie), 409)
	})
	t.Run("finite_ceremony_and_replay_receipt_capacity", func(t *testing.T) {
		c := f.start(t, "/api/v1/auth/saml/start", samlMemberSubject, nil, "", "")
		var template entity.SAMLCeremony
		if db.Where("cookie_hash = ?", secret.SHA256Hex(c.start.Value)).Take(&template).Error != nil {
			t.Fatal("read genuine pending capacity baseline")
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		var count int64
		if db.Model(&entity.SAMLCeremony{}).Where("expires_at > ?", now).Count(&count).Error != nil || count >= 1024 {
			t.Fatal("invalid live ceremony count")
		}
		rows := make([]entity.SAMLCeremony, 0, 1024-int(count))
		ids := make([]string, 0, cap(rows))
		for i := count; i < 1024; i++ {
			nonce, e := secret.RandomURLSafe(16)
			if e != nil {
				t.Fatal("capacity fixture nonce")
			}
			row := template
			row.ID = "smc_capacity_" + nonce[:16]
			row.RequestID = "_capacity_" + nonce
			row.RelayHash = secret.SHA256Hex("relay" + nonce)
			row.CookieHash = secret.SHA256Hex("cookie" + nonce)
			row.CreatedAt = now
			row.ExpiresAt = now.Add(4 * time.Minute)
			rows = append(rows, row)
			ids = append(ids, row.ID)
		}
		if db.CreateInBatches(&rows, 100).Error != nil {
			t.Fatal("seed finite capacity rows")
		}
		defer func() {
			if db.Where("id IN ?", ids).Delete(&entity.SAMLCeremony{}).Error != nil {
				t.Error("clean exact capacity fixture rows")
			}
		}()
		samlApplicationStatus(t, samlApplicationRequest(f.router, "POST", "/api/v1/auth/saml/start", struct{}{}, "", ""), 503)
		if db.Where("id IN ?", ids).Delete(&entity.SAMLCeremony{}).Error != nil {
			t.Fatal("release exact capacity rows")
		}
		var receipts int64
		if db.Model(&entity.SAMLAssertionReceipt{}).Where("expires_at > ?", now).Count(&receipts).Error != nil || receipts >= 1024 {
			t.Fatal("invalid receipt count")
		}
		seeded := make([]entity.SAMLAssertionReceipt, 0, 1024-int(receipts))
		digests := make([]string, 0, cap(seeded))
		for i := receipts; i < 1024; i++ {
			nonce, e := secret.RandomURLSafe(16)
			if e != nil {
				t.Fatal("receipt nonce")
			}
			digest := secret.SHA256Hex("capacity-receipt" + nonce)
			seeded = append(seeded, entity.SAMLAssertionReceipt{Digest: digest, CreatedAt: now, ExpiresAt: now.Add(4 * time.Minute)})
			digests = append(digests, digest)
		}
		if db.CreateInBatches(&seeded, 100).Error != nil {
			t.Fatal("seed finite replay receipt capacity")
		}
		defer func() {
			if db.Where("digest IN ?", digests).Delete(&entity.SAMLAssertionReceipt{}).Error != nil {
				t.Error("clean exact replay receipt fixture rows")
			}
		}()
		staged := f.stage(t, c.relay, c.response)
		for _, cookie := range staged.Result().Cookies() {
			if cookie.Name == samlDeliveryCookie && cookie.Value != "" {
				t.Fatal("saturated receipt store admitted an assertion")
			}
		}
	})
	t.Run("planned_departure_revokes_saml_and_pending_native_mfa", func(t *testing.T) {
		// This distinct existing member leaves the original lifecycle owner intact.
		var administrator entity.User
		if db.Where("id = ?", f.admin.User.ID).Take(&administrator).Error != nil {
			t.Fatal("read departure password baseline")
		}
		departing := entity.User{ID: "usr_saml_departing_member", Email: "saml-departing@example.invalid", Name: "Departing SAML member", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash}
		if db.Create(&departing).Error != nil {
			t.Fatal("create existing departure member")
		}
		if db.Where("id = ?", departing.ID).Take(&departing).Error != nil {
			t.Fatal("read exact departure member birth")
		}
		local, localCookie := samlApplicationSession(t, samlApplicationRequest(f.router, "POST", "/api/v1/auth/login", LoginRequest{Email: departing.Email, Password: samlFixturePassword}, "", ""))
		const subject = "Exact-Departing-SAML-Subject"
		account := samlApplicationDecode[service.SAMLAccountView](t, samlApplicationRequest(f.router, "GET", "/api/v1/account/identity/saml", nil, "", "", localCookie), 200)
		binding := f.begin(t, "/api/v1/account/identity/saml/bind", subject, localCookie, local.CSRFToken, account.ReviewETag)
		linked := samlApplicationDecode[struct {
			Kind string `json:"kind"`
		}](t, f.finish(t, binding, localCookie, local.CSRFToken), 200)
		if linked.Kind != "bound" {
			t.Fatal("departure member lacks explicit genuine SAML binding")
		}
		enrollment := samlApplicationDecode[service.MFAEnrollment](t, samlApplicationRequest(f.router, "POST", "/api/v1/account/mfa/enrollment", MFAPasswordRequest{CurrentPassword: samlFixturePassword}, "", local.CSRFToken, localCookie), 200)
		enabledResponse := samlApplicationRequest(f.router, "POST", "/api/v1/account/mfa/enable", MFAEnableRequest{CurrentPassword: samlFixturePassword, EnrollmentToken: enrollment.EnrollmentToken, Code: mfaFixtureCode(t, enrollment.Secret, 0)}, "", local.CSRFToken, localCookie)
		enabled := samlApplicationDecode[MFARecoveryResponse](t, enabledResponse, 200)
		if enabled.Session == nil || len(enabled.RecoveryCodes) != 10 {
			t.Fatal("departure native MFA baseline incomplete")
		}
		local = *enabled.Session
		localCookie = samlApplicationCookie(t, enabledResponse, sessionCookie)
		admitted := f.begin(t, "/api/v1/auth/saml/start", subject, nil, "", "")
		factor := samlApplicationDecode[service.MFALoginChallenge](t, f.finish(t, admitted, nil, ""), 202)
		if !factor.MFARequired || factor.ChallengeToken == "" {
			t.Fatal("departure SAML login bypassed native MFA")
		}
		login := samlApplicationRequest(f.router, "POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: factor.ChallengeToken, RecoveryCode: enabled.RecoveryCodes[0]}, "", "")
		session, samlCookie := samlApplicationSession(t, login)
		var issued entity.Session
		if db.Where("token_hash = ?", secret.SHA256Hex(samlCookie.Value)).Take(&issued).Error != nil || session.User.ID != departing.ID || issued.UserID != departing.ID || issued.PrimaryMethod != "saml" || issued.SAMLBindingID == "" || issued.SAMLBindingCreatedAt == nil || issued.SAMLUserCreatedAt == nil || !issued.SAMLUserCreatedAt.Equal(departing.CreatedAt) {
			t.Fatal("departure baseline lacks exact SAML native-MFA Session")
		}
		samlApplicationStatus(t, samlApplicationRequest(f.router, "GET", "/api/v1/auth/session", nil, "", "", samlCookie), 200)
		samlApplicationStatus(t, samlApplicationRequest(f.router, "GET", "/api/v1/auth/session", nil, "", "", localCookie), 200)
		pendingLogin := f.begin(t, "/api/v1/auth/saml/start", subject, nil, "", "")
		pending := samlApplicationDecode[service.MFALoginChallenge](t, f.finish(t, pendingLogin, nil, ""), 202)
		if !pending.MFARequired || pending.ChallengeToken == "" {
			t.Fatal("departure lacks a pending native factor")
		}
		staged := f.begin(t, "/api/v1/auth/saml/start", subject, nil, "", "")
		var stagedRow entity.SAMLCeremony
		if db.Where("cookie_hash = ?", secret.SHA256Hex(staged.start.Value)).Take(&stagedRow).Error != nil || stagedRow.Purpose != "login" || stagedRow.Status != "verified" || stagedRow.Subject != subject || stagedRow.UserID != "" || stagedRow.RequestID != staged.requestID || stagedRow.DeliveryHash != secret.SHA256Hex(staged.delivery.Value) || stagedRow.VerifiedAt == nil || stagedRow.ProofExpiresAt == nil || !stagedRow.ProofExpiresAt.After(time.Now().UTC()) {
			t.Fatal("departure lacks genuinely staged unconsumed login proof")
		}
		var providerBefore entity.SAMLProvider
		var bindingsBefore []entity.SAMLBinding
		var recoveryBefore []entity.MFARecoveryCode
		if db.Where("id = ?", "saml").Take(&providerBefore).Error != nil || !providerBefore.Enabled || db.Order("id").Find(&bindingsBefore).Error != nil || db.Where("user_id = ?", departing.ID).Order("code_hash").Find(&recoveryBefore).Error != nil {
			t.Fatal("capture retained departure identity and recovery facts")
		}
		matched := false
		for _, retained := range bindingsBefore {
			if retained.UserID == departing.ID && retained.ID == issued.SAMLBindingID && retained.CreatedAt.Equal(*issued.SAMLBindingCreatedAt) && retained.UserCreatedAt.Equal(departing.CreatedAt) && retained.ConfigRevision == issued.SAMLConfigRevision && retained.ConfigRevision == providerBefore.ConfigRevision && retained.Issuer == providerBefore.IDPIssuer && retained.Subject == subject {
				matched = true
			}
		}
		if !matched || issued.SAMLPolicyRevision != providerBefore.PolicyRevision {
			t.Fatal("departure baseline differs from current exact binding and policy")
		}
		request := func(method, path string, body any, cookie *http.Cookie, csrf, etag string, extra *http.Cookie) *httptest.ResponseRecorder {
			return samlApplicationRequest(f.router, method, path, body, etag, csrf, cookie, extra)
		}
		assertRetained := exerciseEnterpriseOffboarding(t, context.Background(), db, f.service, "saml", departing.ID, f.admin, f.adminCookie, request)
		samlApplicationStatus(t, samlApplicationRequest(f.router, "GET", "/api/v1/auth/session", nil, "", "", samlCookie), 401)
		samlApplicationStatus(t, samlApplicationRequest(f.router, "GET", "/api/v1/auth/session", nil, "", "", localCookie), 401)
		deniedFactor := samlApplicationRequest(f.router, "POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: pending.ChallengeToken, RecoveryCode: enabled.RecoveryCodes[1]}, "", "")
		samlApplicationStatus(t, deniedFactor, 401)
		deniedCompletion := f.finish(t, staged, nil, "")
		samlApplicationStatus(t, deniedCompletion, 401)
		samlApplicationStatus(t, f.finish(t, staged, nil, ""), 401)
		for _, response := range []*httptest.ResponseRecorder{deniedFactor, deniedCompletion} {
			for _, cookie := range response.Result().Cookies() {
				if cookie.Name == sessionCookie {
					t.Fatal("departure or late proof issued a Session cookie")
				}
			}
		}
		var providerAfter entity.SAMLProvider
		var bindingsAfter []entity.SAMLBinding
		var recoveryAfter []entity.MFARecoveryCode
		if db.Where("id = ?", "saml").Take(&providerAfter).Error != nil || db.Order("id").Find(&bindingsAfter).Error != nil || db.Where("user_id = ?", departing.ID).Order("code_hash").Find(&recoveryAfter).Error != nil || !reflect.DeepEqual(providerBefore, providerAfter) || !reflect.DeepEqual(bindingsBefore, bindingsAfter) || !reflect.DeepEqual(recoveryBefore, recoveryAfter) {
			t.Fatal("departure or rejected proof changed retained SAML or recovery facts")
		}
		// Recheck exact Project assets, original receipt/audit and absent authority
		// after both late authentication operations, not only at departure commit.
		assertRetained()
	})
	t.Run("native_mfa_provenance_and_policy_revocation", func(t *testing.T) {
		enrollment := samlApplicationDecode[service.MFAEnrollment](t, samlApplicationRequest(f.router, "POST", "/api/v1/account/mfa/enrollment", MFAPasswordRequest{CurrentPassword: samlFixturePassword}, "", f.member.CSRFToken, f.memberCookie), 200)
		res := samlApplicationRequest(f.router, "POST", "/api/v1/account/mfa/enable", MFAEnableRequest{CurrentPassword: samlFixturePassword, EnrollmentToken: enrollment.EnrollmentToken, Code: mfaFixtureCode(t, enrollment.Secret, 0)}, "", f.member.CSRFToken, f.memberCookie)
		enabled := samlApplicationDecode[MFARecoveryResponse](t, res, 200)
		if enabled.Session == nil || len(enabled.RecoveryCodes) != 10 {
			t.Fatal("native MFA enrollment incomplete")
		}
		f.member = *enabled.Session
		f.memberCookie = samlApplicationCookie(t, res, sessionCookie)
		c := f.begin(t, "/api/v1/auth/saml/start", samlMemberSubject, nil, "", "")
		challenge := samlApplicationDecode[service.MFALoginChallenge](t, f.finish(t, c, nil, ""), 202)
		if !challenge.MFARequired || challenge.ChallengeToken == "" {
			t.Fatal("SAML bypassed native MFA")
		}
		verified := samlApplicationRequest(f.router, "POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: challenge.ChallengeToken, RecoveryCode: enabled.RecoveryCodes[0]}, "", "")
		_, cookie := samlApplicationSession(t, verified)
		var row entity.Session
		if db.Where("token_hash = ?", secret.SHA256Hex(cookie.Value)).Take(&row).Error != nil || row.PrimaryMethod != "saml" || row.SAMLBindingID == "" {
			t.Fatal("MFA lost SAML primary proof")
		}
		c = f.begin(t, "/api/v1/auth/saml/start", samlMemberSubject, nil, "", "")
		pending := samlApplicationDecode[service.MFALoginChallenge](t, f.finish(t, c, nil, ""), 202)
		view := samlApplicationDecode[service.SAMLProviderView](t, samlApplicationRequest(f.router, "GET", "/api/v1/admin/auth/saml", nil, "", "", f.adminCookie), 200)
		samlApplicationDecode[service.SAMLProviderView](t, samlApplicationRequest(f.router, "PUT", "/api/v1/admin/auth/saml/status", service.SAMLStatusInput{Enabled: false, Reason: "Revoke reviewed SAML admission policy"}, view.ReviewETag, f.admin.CSRFToken, f.adminCookie), 200)
		samlApplicationStatus(t, samlApplicationRequest(f.router, "GET", "/api/v1/auth/session", nil, "", "", cookie), 401)
		samlApplicationStatus(t, samlApplicationRequest(f.router, "POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: pending.ChallengeToken, RecoveryCode: enabled.RecoveryCodes[1]}, "", ""), 401)
		var remaining int64
		if db.Model(&entity.MFARecoveryCode{}).Where("user_id = ? AND used_at IS NULL", f.member.User.ID).Count(&remaining).Error != nil || remaining != 9 {
			t.Fatal("revoked primary proof consumed recovery code")
		}
		samlApplicationStatus(t, samlApplicationRequest(f.router, "GET", "/api/v1/auth/session", nil, "", "", f.memberCookie), 200)
		account := samlApplicationDecode[service.SAMLAccountView](t, samlApplicationRequest(f.router, "GET", "/api/v1/account/identity/saml", nil, "", "", f.memberCookie), 200)
		if !account.Bound || account.Available || !account.MFARequired {
			t.Fatal("disabled policy invented account binding state")
		}
		unlinked := samlApplicationDecode[service.SAMLAccountView](t, samlApplicationRequest(f.router, "POST", "/api/v1/account/identity/saml/unlink", service.SAMLIdentityInput{Password: samlFixturePassword, Proof: service.SAMLProof{RecoveryCode: enabled.RecoveryCodes[2]}, Reason: "Explicitly unlink retained account identity"}, account.ReviewETag, f.member.CSRFToken, f.memberCookie), 200)
		if unlinked.Bound {
			t.Fatal("explicit unlink retained binding")
		}
		samlApplicationStatus(t, samlApplicationRequest(f.router, "GET", "/api/v1/auth/session", nil, "", "", f.memberCookie), 200)
	})
	t.Run("security_tuple_replaces_authority_and_retains_replay_history", func(t *testing.T) {
		view := samlApplicationDecode[service.SAMLProviderView](t, samlApplicationRequest(f.router, "GET", "/api/v1/admin/auth/saml", nil, "", "", f.adminCookie), 200)
		view = samlApplicationDecode[service.SAMLProviderView](t, samlApplicationRequest(f.router, "PUT", "/api/v1/admin/auth/saml/status", service.SAMLStatusInput{Enabled: true, Reason: "Reenable the separately reviewed identity"}, view.ReviewETag, f.admin.CSRFToken, f.adminCookie), 200)
		_, cookie := f.login(t, samlAdminSubject)
		c := f.begin(t, "/api/v1/auth/saml/start", samlAdminSubject, nil, "", "")
		var receiptBefore int64
		if db.Model(&entity.SAMLAssertionReceipt{}).Count(&receiptBefore).Error != nil {
			t.Fatal("count retained replay history")
		}
		in := service.SAMLProviderInput{Name: view.Name, IDPIssuer: view.IDPIssuer, SSOURL: view.SSOURL + "/replacement", SPEntityID: view.SPEntityID, ACSURL: view.ACSURL, SigningCertificatePEM: view.SigningCertificatePEM, Reason: "Replace reviewed security tuple"}
		saved := samlApplicationDecode[service.SAMLProviderView](t, samlApplicationRequest(f.router, "PUT", "/api/v1/admin/auth/saml", in, view.ReviewETag, f.admin.CSRFToken, f.adminCookie), 200)
		if saved.Enabled || saved.Verified {
			t.Fatal("security edit retained verification or admission")
		}
		samlApplicationStatus(t, f.finish(t, c, nil, ""), 401)
		samlApplicationStatus(t, samlApplicationRequest(f.router, "GET", "/api/v1/auth/session", nil, "", "", cookie), 401)
		samlApplicationStatus(t, samlApplicationRequest(f.router, "GET", "/api/v1/auth/session", nil, "", "", f.adminCookie), 200)
		var bindings, receiptAfter int64
		if db.Model(&entity.SAMLBinding{}).Count(&bindings).Error != nil || bindings != 0 || db.Model(&entity.SAMLAssertionReceipt{}).Count(&receiptAfter).Error != nil || receiptAfter != receiptBefore {
			t.Fatal("security edit retained bindings or removed replay receipts")
		}
	})
}
