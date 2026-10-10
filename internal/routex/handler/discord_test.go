package handler

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func TestDiscordRoutesKeepExistingIdentityAndInstallationReads(t *testing.T) {
	router:=fox.New();New(nil).RegisterRoutes(router)
	routes:=map[string]bool{}
	for _,route:=range router.Routes(){routes[route.Method+" "+route.Path]=true}
	for _,route:=range []string{
		"GET /api/v1/auth/discord","POST /api/v1/auth/discord/start","GET /api/v1/auth/discord/callback","POST /api/v1/auth/discord/complete","POST /api/v1/auth/discord/abandon",
		"GET /api/v1/admin/auth/discord","PUT /api/v1/admin/auth/discord","POST /api/v1/admin/auth/discord/verify","PUT /api/v1/admin/auth/discord/status",
		"GET /api/v1/account/identity/discord","POST /api/v1/account/identity/discord/bind","POST /api/v1/account/identity/discord/unlink",
		"GET /api/v1/auth/google/callback","POST /api/v1/auth/google/complete","GET /api/v1/auth/github/callback","POST /api/v1/auth/github/complete","POST /api/v1/auth/saml/acs","GET /api/v1/admin/runtime/installations",
	}{if !routes[route]{t.Fatalf("required route missing: %s",route)}}
	count:=0;for route:=range routes{if strings.Contains(route,"/discord"){count++}}
	if count!=12{t.Fatal("Discord exposed an unexpected route")}
}

func TestDiscordHTTPAdmissionPrivacyBeforeServiceAccess(t *testing.T){
	// A nil service makes any unintended transition into DB/remote work visible.
	router:=fox.New();New(nil).RegisterRoutes(router)
	proof:=base64.RawURLEncoding.EncodeToString(make([]byte,32))
	for _,tc:=range []struct{name,method,target,body,cookie,origin,contentType string;status int}{
		{"public_query", "GET",discordAuthPath+"?code=private-code","","","","",400},
		{"start_extra_body","POST",discordAuthPath+"/start",`{"unexpected":"private-proof"}`,"","","application/json",400},
		{"start_duplicate_document","POST",discordAuthPath+"/start",`{} {}`,"","","application/json",400},
		{"start_null","POST",discordAuthPath+"/start",`null`,"","","application/json",400},
		{"start_wrong_mime","POST",discordAuthPath+"/start",`{}`,"","","text/plain",400},
		{"start_cross_origin","POST",discordAuthPath+"/start",`{}`,"","https://foreign.invalid","application/json",403},
		{"complete_cross_origin","POST",discordAuthPath+"/complete",`{}`,discordCookie+"="+proof,"https://foreign.invalid","application/json",403},
		{"complete_missing_cookie","POST",discordAuthPath+"/complete",`{}`,"","","application/json",401},
		{"complete_alias_cookie","POST",discordAuthPath+"/complete",`{}`,"routex_discord="+proof,"","application/json",401},
		{"complete_duplicate_cookie","POST",discordAuthPath+"/complete",`{}`,discordCookie+"="+proof+"; "+discordCookie+"="+proof,"","application/json",401},
		{"complete_bad_body","POST",discordAuthPath+"/complete",`{"code":"private-proof"}`,discordCookie+"="+proof,"","application/json",400},
		{"abandon_bad_body","POST",discordAuthPath+"/abandon",`{"code":"private-proof"}`,"","","application/json",400},
		{"abandon_duplicate_cookie","POST",discordAuthPath+"/abandon",`{}`,discordCookie+"="+proof+"; "+discordCookie+"="+proof,"","application/json",400},
		{"admin_get_requires_session","GET","/api/v1/admin/auth/discord","","","","",401},
		{"admin_save_requires_session","PUT","/api/v1/admin/auth/discord",`{}`,"","","application/json",401},
		{"admin_verify_requires_session","POST","/api/v1/admin/auth/discord/verify",`{}`,"","","application/json",401},
		{"admin_status_requires_session","PUT","/api/v1/admin/auth/discord/status",`{}`,"","","application/json",401},
		{"account_get_requires_session","GET","/api/v1/account/identity/discord","","","","",401},
		{"account_bind_requires_session","POST","/api/v1/account/identity/discord/bind",`{}`,"","","application/json",401},
		{"account_unlink_requires_session","POST","/api/v1/account/identity/discord/unlink",`{}`,"","","application/json",401},
	}{t.Run(tc.name,func(t *testing.T){
		r:=httptest.NewRequest(tc.method,tc.target,strings.NewReader(tc.body));if tc.cookie!=""{r.Header.Set("Cookie",tc.cookie)};if tc.origin!=""{r.Header.Set("Origin",tc.origin)};if tc.contentType!=""{r.Header.Set("Content-Type",tc.contentType)}
		w:=httptest.NewRecorder();router.ServeHTTP(w,r)
		if w.Code!=tc.status{t.Fatalf("admission status changed: %d",w.Code)}
		if w.Header().Get("Cache-Control")!="private, no-store"||w.Header().Get("X-Content-Type-Options")!="nosniff"||w.Header().Get("Referrer-Policy")!="no-referrer"{t.Fatal("private response headers changed")}
		if strings.Contains(w.Body.String(),"private-code")||strings.Contains(w.Body.String(),"private-proof"){t.Fatal("rejection reflected sensitive input")}
		for _,cookie:=range w.Result().Cookies(){if cookie.Name==sessionCookie{t.Fatal("rejection issued a Session")}}
	})}
}

func TestDiscordHTTPReviewRequiresOneExactStrongETag(t *testing.T){
	ctrl:=New(nil);router:=fox.New();router.RenderErrorFunc=renderAPIError
	router.PUT("/save",identityPrivate,jsonManagementRequest,ctrl.SaveDiscordProvider)
	router.PUT("/status",identityPrivate,jsonManagementRequest,ctrl.SetDiscordEnabled)
	router.POST("/bind",identityPrivate,jsonManagementRequest,ctrl.StartDiscordBinding)
	router.POST("/verify",identityPrivate,jsonManagementRequest,ctrl.StartDiscordVerification)
	router.POST("/unlink",identityPrivate,jsonManagementRequest,ctrl.UnlinkDiscord)
	for _,route:=range []struct{method,path string}{{"PUT","/save"},{"PUT","/status"},{"POST","/bind"},{"POST","/verify"},{"POST","/unlink"}}{
		for _,headers:=range [][]string{nil,{"*"},{"W/\""+strings.Repeat("a",64)+"\""},{"\""+strings.Repeat("A",64)+"\""},{"\""+strings.Repeat("a",63)+"\""},{"\""+strings.Repeat("a",64)+"\"","\""+strings.Repeat("a",64)+"\""}}{
			r:=httptest.NewRequest(route.method,route.path,strings.NewReader(`{}`));r.Header.Set("Content-Type","application/json");for _,header:=range headers{r.Header.Add("If-Match",header)}
			w:=httptest.NewRecorder();router.ServeHTTP(w,r);if w.Code!=http.StatusBadRequest{t.Fatal("ambiguous review entered service")}
		}
	}
}

func TestDiscordHTTPCookieAndManualCompletionBoundaries(t *testing.T){
	router:=fox.New();New(nil).RegisterRoutes(router)
	proof:=base64.RawURLEncoding.EncodeToString(make([]byte,32))
	for _,target:=range []string{discordCallbackPath+"?state="+proof+"&code=private-code",discordCallbackPath+"?state="+proof+"&code=private-code&state="+proof}{
		r:=httptest.NewRequest("GET",target,nil);if strings.Contains(target,"&state="){r.Header.Set("Cookie",discordCookie+"="+proof)}
		w:=httptest.NewRecorder();router.ServeHTTP(w,r)
		if w.Code!=http.StatusSeeOther||w.Header().Get("Location")!="/auth/discord/complete"||strings.Contains(w.Body.String(),"private-code"){t.Fatal("callback did not use the fixed clean completion route")}
		cookies:=w.Result().Cookies();if len(cookies)!=1||cookies[0].Name!=discordCookie||cookies[0].Value!=""||cookies[0].MaxAge!=-1{t.Fatal("rejected callback did not clear correlation only")}
	}
	r:=httptest.NewRequest("POST",discordAuthPath+"/abandon",strings.NewReader(`{}`));r.Header.Set("Content-Type","application/json");w:=httptest.NewRecorder();router.ServeHTTP(w,r)
	if w.Code!=http.StatusNoContent||w.Body.Len()!=0{t.Fatal("explicit abandon was not confirmed empty 204")}
	cookies:=w.Result().Cookies();if len(cookies)!=1||cookies[0].Name!=discordCookie||cookies[0].Domain!=""||cookies[0].Path!="/"||!cookies[0].Secure||!cookies[0].HttpOnly||cookies[0].SameSite!=http.SameSiteLaxMode||cookies[0].MaxAge!=-1{t.Fatal("abandon changed exact host cookie policy")}
	// Direct response-boundary fixtures are not authentication acceptance.
	render:=fox.New();render.RenderErrorFunc=renderAPIError
	expires:=time.Unix(1800000000,0).UTC()
	render.POST("/set",identityPrivate,func(c *fox.Context)error{discordSetCookie(c,&service.DiscordStart{Cookie:proof,ExpiresAt:expires});c.Status(http.StatusNoContent);return nil})
	render.POST("/challenge",identityPrivate,func(c *fox.Context)error{return identityCompletionResponse(c,"challenge",nil,&service.MFALoginChallenge{MFARequired:true,ChallengeToken:"transient-challenge",ExpiresAt:expires,Methods:[]string{"totp","recovery_code"}})})
	render.POST("/bound",identityPrivate,func(c *fox.Context)error{return identityCompletionResponse(c,"bound",nil,nil)})
	w=httptest.NewRecorder();render.ServeHTTP(w,httptest.NewRequest("POST","/set",nil));cookies=w.Result().Cookies()
	if len(cookies)!=1||cookies[0].Name!=discordCookie||cookies[0].Value!=proof||cookies[0].Domain!=""||cookies[0].Path!="/"||!cookies[0].Secure||!cookies[0].HttpOnly||cookies[0].SameSite!=http.SameSiteLaxMode||cookies[0].MaxAge!=300||!cookies[0].Expires.Equal(expires){t.Fatal("start cookie lost host-only/transient policy")}
	w=httptest.NewRecorder();render.ServeHTTP(w,httptest.NewRequest("POST","/challenge",nil));var challenge service.MFALoginChallenge
	if w.Code!=http.StatusAccepted||len(w.Result().Cookies())!=0||json.Unmarshal(w.Body.Bytes(),&challenge)!=nil||!challenge.MFARequired||challenge.ChallengeToken!="transient-challenge"{t.Fatal("native MFA was promoted to a Session")}
	w=httptest.NewRecorder();render.ServeHTTP(w,httptest.NewRequest("POST","/bound",nil));if w.Code!=http.StatusOK||len(w.Result().Cookies())!=0||strings.TrimSpace(w.Body.String())!=`{"kind":"bound"}`{t.Fatal("binding completion altered authentication")}
}

func TestDiscordHTTPMutationCSRFBeforeDispatch(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	auth := &service.Authentication{Token: "transient-local-session"}
	install := func(c *fox.Context) { c.Set(authenticationKey, auth); c.Next() }
	router.PUT("/save", identityPrivate, install, requireCSRF, ctrl.SaveDiscordProvider)
	router.PUT("/status", identityPrivate, install, requireCSRF, ctrl.SetDiscordEnabled)
	router.POST("/verify", identityPrivate, install, requireCSRF, ctrl.StartDiscordVerification)
	router.POST("/bind", identityPrivate, install, requireCSRF, ctrl.StartDiscordBinding)
	router.POST("/unlink", identityPrivate, install, requireCSRF, ctrl.UnlinkDiscord)
	for _, route := range []struct{method, path string}{{"PUT","/save"},{"PUT","/status"},{"POST","/verify"},{"POST","/bind"},{"POST","/unlink"}} {
		for _, proof := range []string{"", "obsolete-csrf"} {
			r := httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
			r.Header.Set("X-CSRF-Token", proof)
			w := httptest.NewRecorder(); router.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "transient-local-session") { t.Fatal("CSRF rejection entered mutation or reflected private state") }
		}
	}
	// This is a shared middleware control, not a Session/authentication fixture.
	reached := false
	router.POST("/guard", install, requireCSRF, func(c *fox.Context) { reached = true; c.Status(http.StatusNoContent) })
	r := httptest.NewRequest("POST", "/guard", nil); r.Header.Set("X-CSRF-Token", auth.CSRFToken())
	w := httptest.NewRecorder(); router.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent || !reached { t.Fatal("exact current CSRF control rejected") }
}
