package handler

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

const googleCookie = "__Host-routex_google"
const googleCookiePath = "/"
const googleAuthPath = "/api/v1/auth/google"
const googleCallbackPath = googleAuthPath + "/callback"
const googleResponseIssuer = "https://accounts.google.com"

type googleInputsKey struct{}
type googleInputs struct {
	cookie, state, code, remoteError, responseIssuer string
	invalid                                          bool
}

// GoogleInputs removes browser correlation before downstream observation. The
// callback captures only authority fields; bounded provider decorations are ignored.
func GoogleInputs(c *gin.Context) {
	captureGoogleInputs(c.Request)
	c.Next()
}

func googleBrowserProof(v string) bool {
	if len(v) != 43 {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(v)
	return err == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == v
}

func captureGoogleInputs(r *http.Request) googleInputs {
	if saved, ok := r.Context().Value(googleInputsKey{}).(googleInputs); ok {
		return saved
	}
	v := googleInputs{}
	kept := []string{}
	count := 0
	for _, header := range r.Header.Values("Cookie") {
		for _, part := range strings.Split(header, ";") {
			part = strings.TrimSpace(part)
			key, value, hasValue := strings.Cut(part, "=")
			if strings.TrimSpace(key) == googleCookie {
				count++
				v.cookie = value
				if !hasValue || !googleBrowserProof(value) {
					v.invalid = true
				}
			} else if part != "" {
				kept = append(kept, part)
			}
		}
	}
	if count > 1 {
		v.invalid = true
	}
	r.Header.Del("Cookie")
	if len(kept) > 0 {
		r.Header.Set("Cookie", strings.Join(kept, "; "))
	}

	if r.URL.Path == googleAuthPath || strings.HasPrefix(r.URL.Path, googleAuthPath+"/") {
		raw, forced := r.URL.RawQuery, r.URL.ForceQuery
		r.URL.RawQuery = ""
		r.URL.ForceQuery = false
		r.RequestURI = strings.SplitN(r.RequestURI, "?", 2)[0]
		if r.URL.RawPath != "" {
			v.invalid = true
		}
		if r.URL.Path != googleCallbackPath {
			v.invalid = v.invalid || raw != "" || forced
		} else {
			captureGoogleCallback(raw, &v)
		}
	}
	*r = *r.WithContext(context.WithValue(r.Context(), googleInputsKey{}, v))
	return v
}

func captureGoogleCallback(raw string, v *googleInputs) {
	if len(raw) > 8192 {
		v.invalid = true
		return
	}
	q, err := url.ParseQuery(raw)
	if err != nil {
		v.invalid = true
		return
	}
	occurrences := 0
	for key, values := range q {
		if len(key) > 128 {
			v.invalid = true
		}
		limit := 2048
		switch key {
		case "code":
			limit = 4096
		case "state", "error", "iss":
		default:
			// OAuth response extensions are ignored, including repeated ones.
			// They still count towards every parser bound and never grant authority.
		}
		for _, value := range values {
			occurrences++
			if occurrences > 128 || len(value) > limit {
				v.invalid = true
			}
		}
	}
	for _, key := range []string{"state", "code", "error", "iss"} {
		if len(q[key]) > 1 {
			v.invalid = true
		}
	}
	v.state, v.code, v.remoteError, v.responseIssuer = q.Get("state"), q.Get("code"), q.Get("error"), q.Get("iss")
	if !googleBrowserProof(v.state) || v.responseIssuer != googleResponseIssuer ||
		len(v.code) > 4096 || len(v.remoteError) > 256 ||
		(v.code == "") == (v.remoteError == "") || len(q["code"]) > 0 && len(q["error"]) > 0 {
		v.invalid = true
	}
}
