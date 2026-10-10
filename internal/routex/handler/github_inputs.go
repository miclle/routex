package handler

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

const githubCookie = "__Host-routex_github"
const githubCookiePath = "/"
const githubAuthPath = "/api/v1/auth/github"
const githubCallbackPath = githubAuthPath + "/callback"

type githubInputsKey struct{}
type githubInputs struct {
	cookie, state, code, remoteError string
	invalid                          bool
}

// GitHubInputs removes correlation cookies on every path before logging. Only
// this method's auth namespace owns callback query redaction; other URLs retain
// their original query and authentication inputs.
func GitHubInputs(c *gin.Context) {
	captureGitHubInputs(c.Request)
	c.Next()
}

func githubBrowserProof(v string) bool {
	if len(v) != 43 {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(v)
	return err == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == v
}

func captureGitHubInputs(r *http.Request) githubInputs {
	if saved, ok := r.Context().Value(githubInputsKey{}).(githubInputs); ok {
		return saved
	}
	v := githubInputs{}
	kept := []string{}
	count := 0
	for _, header := range r.Header.Values("Cookie") {
		for _, part := range strings.Split(header, ";") {
			part = strings.TrimSpace(part)
			key, value, hasValue := strings.Cut(part, "=")
			if strings.TrimSpace(key) == githubCookie {
				count++
				v.cookie = value
				if !hasValue || !githubBrowserProof(value) {
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

	if r.URL.Path == githubAuthPath || strings.HasPrefix(r.URL.Path, githubAuthPath+"/") {
		raw, forceQuery := r.URL.RawQuery, r.URL.ForceQuery
		r.URL.RawQuery = ""
		r.URL.ForceQuery = false
		r.RequestURI = strings.SplitN(r.RequestURI, "?", 2)[0]
		if r.URL.Path != githubCallbackPath {
			v.invalid = v.invalid || raw != "" || forceQuery
		} else {
			if len(raw) > 8192 {
				v.invalid = true
			} else {
				q, err := url.ParseQuery(raw)
				if err != nil {
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
							// Bounded provider decorations never grant authority or leave this capture.
						default:
							v.invalid = true
						}
					}
				}
			}
			if !githubBrowserProof(v.state) || len(v.code) > 4096 || len(v.remoteError) > 256 || (v.code == "") == (v.remoteError == "") {
				v.invalid = true
			}
		}
	}
	*r = *r.WithContext(context.WithValue(r.Context(), githubInputsKey{}, v))
	return v
}
