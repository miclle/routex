package handler

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDiscordInputCaptureCorrelationBoundsAndPrivacy(t *testing.T) {
	proof := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	authority := "state=" + proof
	valid := discordCallbackPath + "?" + authority + "&code=private-code"
	cookie := discordCookie + "=" + proof
	atOccurrences := valid
	for i := 0; i < 126; i++ { atOccurrences += "&decoration" + strconv.Itoa(i) + "=x" }
	for _, test := range []struct {
		name, target string
		cookies []string
		invalid bool
		expectedCookie, retainedQuery string
	}{
		{"one_exact_cookie", valid, []string{cookie}, false, proof, ""},
		{"duplicate_cookie_headers", valid, []string{cookie,cookie}, true, proof, ""},
		{"duplicate_cookie_parts", valid, []string{cookie+"; "+cookie}, true, proof, ""},
		{"missing_cookie_value", valid, []string{discordCookie}, true, "", ""},
		{"quoted_cookie", valid, []string{discordCookie+"=\""+proof+"\""}, true, "\""+proof+"\"", ""},
		{"cookie_alias_no_authority", valid, []string{"routex_discord="+proof}, false, "", ""},
		{"cookie_case_alias_no_authority", valid, []string{"__host-routex_discord="+proof}, false, "", ""},
		{"foreign_profile_no_authority", valid, []string{"__Host-routex_google="+proof}, false, "", ""},
		{"noncanonical_cookie", valid, []string{discordCookie+"="+strings.Repeat("A",42)+"B"}, true, strings.Repeat("A",42)+"B", ""},
		{"missing_state",discordCallbackPath+"?code=private-code",[]string{cookie},true,proof,""},
		{"state_case_alias",discordCallbackPath+"?State="+proof+"&code=private-code",[]string{cookie},true,proof,""},
		{"duplicate_state",valid+"&state="+proof,[]string{cookie},true,proof,""},
		{"escaped_duplicate_state",valid+"&%73tate="+proof,[]string{cookie},true,proof,""},
		{"duplicate_code",valid+"&code=private-code",[]string{cookie},true,proof,""},
		{"native_denial",discordCallbackPath+"?"+authority+"&error=access_denied&error_description=private-decoration",[]string{cookie},false,proof,""},
		{"duplicate_error",discordCallbackPath+"?"+authority+"&error=access_denied&error=access_denied",[]string{cookie},true,proof,""},
		{"code_and_error",valid+"&error=access_denied",[]string{cookie},true,proof,""},
		{"code_and_empty_error",valid+"&error=",[]string{cookie},true,proof,""},
		{"empty_code",discordCallbackPath+"?"+authority+"&code=",[]string{cookie},true,proof,""},
		{"ignored_scope",valid+"&scope=identify+email",[]string{cookie},false,proof,""},
		{"ignored_redirect",valid+"&redirect=https%3A%2F%2Fexample.invalid",[]string{cookie},false,proof,""},
		{"ignored_issuer_not_required",valid+"&iss=https%3A%2F%2Fexample.invalid",[]string{cookie},false,proof,""},
		{"ignored_nonce_not_authority",valid+"&nonce=private-decoration",[]string{cookie},false,proof,""},
		{"repeated_decoration",valid+"&future=one&future=two",[]string{cookie},true,proof,""},
		{"ignored_authority_case",valid+"&State=other&Code=other",[]string{cookie},false,proof,""},
		{"malformed_escape",valid+"&future=%zz",[]string{cookie},true,proof,""},
		{"malformed_semicolon",valid+"&future=a;b",[]string{cookie},true,proof,""},
		{"query_on_start",discordAuthPath+"/start?code=private-code",[]string{cookie},true,proof,""},
		{"forced_empty_query",discordAuthPath+"/start?",[]string{cookie},true,proof,""},
		{"query_on_unknown",discordAuthPath+"/missing?code=private-code",[]string{cookie},true,proof,""},
		{"escaped_auth_path",strings.Replace(valid,"/discord/","/%64iscord/",1),[]string{cookie},true,proof,""},
		{"unrelated_query_preserved","/api/v1/calls?cursor=opaque",[]string{cookie},false,proof,"cursor=opaque"},
		{"raw_query_bound",discordCallbackPath+"?"+strings.Repeat("a",8193),[]string{cookie},true,proof,""},
		{"code_at_bound",discordCallbackPath+"?"+authority+"&code="+strings.Repeat("x",4096),[]string{cookie},false,proof,""},
		{"code_over_bound",discordCallbackPath+"?"+authority+"&code="+strings.Repeat("x",4097),[]string{cookie},true,proof,""},
		{"error_at_bound",discordCallbackPath+"?"+authority+"&error="+strings.Repeat("x",256),[]string{cookie},false,proof,""},
		{"error_over_bound",discordCallbackPath+"?"+authority+"&error="+strings.Repeat("x",257),[]string{cookie},true,proof,""},
		{"decoration_at_bound",valid+"&future="+strings.Repeat("x",2048),[]string{cookie},false,proof,""},
		{"decoration_over_bound",valid+"&future="+strings.Repeat("x",2049),[]string{cookie},true,proof,""},
		{"key_at_bound",valid+"&"+strings.Repeat("x",128)+"=one",[]string{cookie},false,proof,""},
		{"key_over_bound",valid+"&"+strings.Repeat("x",129)+"=one",[]string{cookie},true,proof,""},
		{"occurrences_at_bound",atOccurrences,[]string{cookie},false,proof,""},
		{"occurrences_over_bound",atOccurrences+"&extra=x",[]string{cookie},true,proof,""},
	} {
		t.Run(test.name,func(t *testing.T){
			r:=httptest.NewRequest(http.MethodGet,test.target,nil)
			for _,value:=range test.cookies{r.Header.Add("Cookie",value)}
			r.Header.Add("Cookie","routex_session=retained-session; __Host-routex_github=retained-github")
			got:=captureDiscordInputs(r)
			if got.invalid!=test.invalid || got.cookie!=test.expectedCookie {t.Fatal("correlation or rejection changed")}
			if r.URL.RawQuery!=test.retainedQuery || r.URL.ForceQuery || strings.Contains(r.RequestURI,"private-code") || strings.Contains(r.RequestURI,"private-decoration"){t.Fatal("callback privacy or unrelated query changed")}
			if strings.Contains(r.Header.Get("Cookie"),discordCookie)||!strings.Contains(r.Header.Get("Cookie"),"routex_session=retained-session")||!strings.Contains(r.Header.Get("Cookie"),"__Host-routex_github=retained-github"){t.Fatal("cookie isolation or redaction")}
			if again:=captureDiscordInputs(r);!reflect.DeepEqual(again,got){t.Fatal("redacted capture lost original authority")}
			if !got.invalid&&r.URL.Path==discordCallbackPath{if got.state!=proof||(got.code=="")== (got.remoteError==""){t.Fatal("decoration replaced callback authority")}}
		})
	}
}

func TestDiscordInputMiddlewareRedactsBeforeDownstreamObservation(t *testing.T){
	proof:=base64.RawURLEncoding.EncodeToString(make([]byte,32))
	for _,target:=range []string{discordCallbackPath+"?state="+proof+"&code=private-code",discordAuthPath+"/unknown?code=private-code","/unrelated?cursor=opaque"}{
		t.Run(target[:strings.Index(target,"?")],func(t *testing.T){
			engine:=gin.New();observed:=false
			engine.Use(DiscordInputs,func(c *gin.Context){observed=true;if strings.Contains(c.Request.Header.Get("Cookie"),discordCookie)||strings.Contains(c.Request.RequestURI,"private-code")||strings.Contains(c.Request.URL.RawQuery,"private-code"){t.Error("sensitive input reached downstream observation")};c.Next()})
			engine.NoRoute(func(c *gin.Context){c.Status(http.StatusNotFound)})
			r:=httptest.NewRequest(http.MethodGet,target,nil);r.Header.Set("Cookie",discordCookie+"="+proof);w:=httptest.NewRecorder();engine.ServeHTTP(w,r)
			if !observed||w.Code!=http.StatusNotFound{t.Fatal("unknown route bypassed real middleware")}
		})
	}
}

func TestDiscordBrowserProofRequiresCanonicalIndependentBytes(t *testing.T){
	for _,value:=range []string{"",strings.Repeat("A",42),strings.Repeat("A",44),strings.Repeat("A",42)+"B",strings.Repeat("+",43)}{if discordBrowserProof(value){t.Fatal("invalid proof admitted")}}
	if !discordBrowserProof(base64.RawURLEncoding.EncodeToString(make([]byte,32))){t.Fatal("canonical proof rejected")}
}
