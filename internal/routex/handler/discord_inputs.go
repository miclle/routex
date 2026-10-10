package handler

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

const discordCookie = "__Host-routex_discord"
const discordCookiePath = "/"
const discordAuthPath = "/api/v1/auth/discord"
const discordCallbackPath = discordAuthPath + "/callback"

type discordInputsKey struct{}
type discordInputs struct {
	cookie, state, code, remoteError string
	invalid                                          bool
}

// DiscordInputs removes browser correlation before downstream observation. The
// callback captures only authority fields; bounded provider decorations are ignored.
func DiscordInputs(c *gin.Context) {
	captureDiscordInputs(c.Request)
	c.Next()
}

func discordBrowserProof(v string) bool {
	if len(v) != 43 {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(v)
	return err == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == v
}

func captureDiscordInputs(r *http.Request) discordInputs {
	if saved, ok := r.Context().Value(discordInputsKey{}).(discordInputs); ok {
		return saved
	}
	v := discordInputs{}
	kept := []string{}
	count := 0
	for _, header := range r.Header.Values("Cookie") {
		for _, part := range strings.Split(header, ";") {
			part = strings.TrimSpace(part)
			key, value, hasValue := strings.Cut(part, "=")
			if strings.TrimSpace(key) == discordCookie {
				count++
				v.cookie = value
				if !hasValue || !discordBrowserProof(value) {
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

	if r.URL.Path == discordAuthPath || strings.HasPrefix(r.URL.Path, discordAuthPath+"/") {
		raw, forced := r.URL.RawQuery, r.URL.ForceQuery
		r.URL.RawQuery = ""
		r.URL.ForceQuery = false
		r.RequestURI = strings.SplitN(r.RequestURI, "?", 2)[0]
		if r.URL.RawPath != "" {
			v.invalid = true
		}
		if r.URL.Path != discordCallbackPath {
			v.invalid = v.invalid || raw != "" || forced
		} else {
			captureDiscordCallback(raw, &v)
		}
	}
	*r = *r.WithContext(context.WithValue(r.Context(), discordInputsKey{}, v))
	return v
}

func captureDiscordCallback(raw string, v *discordInputs) {
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
	for _, key := range []string{"state", "code", "error"} {
		if len(q[key]) > 1 {
			v.invalid = true
		}
	}
	v.state, v.code, v.remoteError = q.Get("state"), q.Get("code"), q.Get("error")
	if !discordBrowserProof(v.state) ||
		len(v.code) > 4096 || len(v.remoteError) > 256 ||
		(v.code == "") == (v.remoteError == "") || len(q["code"]) > 0 && len(q["error"]) > 0 {
		v.invalid = true
	}
}
