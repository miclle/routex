package handler

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

const oauthCookie = "routex_oauth"
const oauthCookiePath = "/api/v1/auth/oauth"
const oauthCallbackPath = oauthCookiePath + "/callback"

type oauthInputsKey struct{}
type oauthInputs struct {
	cookie, state, code, remoteError string
	invalid                          bool
}

// OAuthInputs captures bounded callback secrets before logger/recovery observes
// the completed request. Query values and the correlation cookie never return
// to the URL or ordinary Cookie header, including on malformed/unknown paths.
func OAuthInputs(c *gin.Context) {
	if c.Request.URL.Path == oauthCookiePath || strings.HasPrefix(c.Request.URL.Path, oauthCookiePath+"/") {
		captureOAuthInputs(c.Request)
	}
	c.Next()
}
func captureOAuthInputs(r *http.Request) oauthInputs {
	if saved, ok := r.Context().Value(oauthInputsKey{}).(oauthInputs); ok {
		return saved
	}
	v := oauthInputs{}
	raw := r.URL.RawQuery
	forceQuery := r.URL.ForceQuery
	r.URL.RawQuery = ""
	r.URL.ForceQuery = false
	r.RequestURI = strings.SplitN(r.RequestURI, "?", 2)[0]
	cookies := r.Cookies()
	count := 0
	for _, cookie := range cookies {
		if cookie.Name == oauthCookie {
			count++
			v.cookie = cookie.Value
		}
	}
	if count > 1 || count == 1 && len(v.cookie) != 43 {
		v.invalid = true
	}
	kept := []string{}
	for _, header := range r.Header.Values("Cookie") {
		for _, part := range strings.Split(header, ";") {
			part = strings.TrimSpace(part)
			key, _, _ := strings.Cut(part, "=")
			if key != oauthCookie && part != "" {
				kept = append(kept, part)
			}
		}
	}
	r.Header.Del("Cookie")
	if len(kept) > 0 {
		r.Header.Set("Cookie", strings.Join(kept, "; "))
	}
	if r.URL.Path != oauthCallbackPath {
		v.invalid = v.invalid || raw != "" || forceQuery
	} else {
		var q url.Values
		var err error
		if len(raw) > 8192 {
			v.invalid = true
		} else {
			q, err = url.ParseQuery(raw)
		}
		if err != nil || v.invalid {
			v.invalid = true
		} else {
			for key, values := range q {
				if len(values) != 1 {
					v.invalid = true
					continue
				}
				switch key {
				case "state":
					v.state = values[0]
				case "code":
					v.code = values[0]
				case "error":
					v.remoteError = values[0]
				case "error_description", "error_uri", "session_state":
					// Protocol decorations are ignored, never reflected or logged.
				default:
					v.invalid = true
				}
			}
		}
		if len(v.state) != 43 || len(v.code) > 4096 || len(v.remoteError) > 256 || (v.code == "") == (v.remoteError == "") {
			v.invalid = true
		}
	}
	*r = *r.WithContext(context.WithValue(r.Context(), oauthInputsKey{}, v))
	return v
}
