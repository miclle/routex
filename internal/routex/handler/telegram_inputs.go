package handler

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

const telegramCookie = "__Host-routex_telegram"
const telegramCookiePath = "/"
const telegramAuthPath = "/api/v1/auth/telegram"
const telegramCallbackPath = telegramAuthPath + "/callback"

type telegramInputsKey struct{}
type telegramInputs struct {
	cookie, state, code, remoteError string
	invalid                          bool
}

// TelegramInputs removes browser correlation before downstream observation. The
// callback captures only authority fields; bounded provider decorations are ignored.
func TelegramInputs(c *gin.Context) {
	captureTelegramInputs(c.Request)
	c.Next()
}

func telegramBrowserProof(v string) bool {
	if len(v) != 43 {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(v)
	return err == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == v
}

func captureTelegramInputs(r *http.Request) telegramInputs {
	if saved, ok := r.Context().Value(telegramInputsKey{}).(telegramInputs); ok {
		return saved
	}
	v := telegramInputs{}
	kept := []string{}
	count := 0
	for _, header := range r.Header.Values("Cookie") {
		for _, part := range strings.Split(header, ";") {
			part = strings.TrimSpace(part)
			key, value, hasValue := strings.Cut(part, "=")
			if strings.TrimSpace(key) == telegramCookie {
				count++
				v.cookie = value
				if !hasValue || !telegramBrowserProof(value) {
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

	if r.URL.Path == telegramAuthPath || strings.HasPrefix(r.URL.Path, telegramAuthPath+"/") {
		raw, forced := r.URL.RawQuery, r.URL.ForceQuery
		r.URL.RawQuery = ""
		r.URL.ForceQuery = false
		r.RequestURI = strings.SplitN(r.RequestURI, "?", 2)[0]
		if r.URL.RawPath != "" {
			v.invalid = true
		}
		if r.URL.Path != telegramCallbackPath {
			v.invalid = v.invalid || raw != "" || forced
		} else {
			captureTelegramCallback(raw, &v)
		}
	}
	*r = *r.WithContext(context.WithValue(r.Context(), telegramInputsKey{}, v))
	return v
}

func captureTelegramCallback(raw string, v *telegramInputs) {
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
		if len(key) > 128 || len(values) != 1 {
			v.invalid = true
		}
		limit := 2048
		switch key {
		case "code":
			limit = 4096
		case "state", "error":
		default:
			// Single-valued OAuth response extensions are ignored.
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
	// OIDC issuer response metadata is optional. When present it must be exact;
	// ID-token issuer/signature verification remains mandatory in either case.
	if len(q["iss"]) != 0 && q.Get("iss") != "https://oauth.telegram.org" {
		v.invalid = true
	}
	v.state, v.code, v.remoteError = q.Get("state"), q.Get("code"), q.Get("error")
	if !telegramBrowserProof(v.state) ||
		len(v.code) > 4096 || len(v.remoteError) > 256 ||
		(v.code == "") == (v.remoteError == "") || len(q["code"]) > 0 && len(q["error"]) > 0 {
		v.invalid = true
	}
}
