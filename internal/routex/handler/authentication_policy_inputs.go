package handler

import (
	"context"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/url"
	"strings"
)

type authenticationPolicyInputsKey struct{}
type authenticationPolicyInputs struct {
	candidate string
	invalid   bool
}

// Drop rejected query material before ordinary observations, even on unknown
// emergency subpaths. Emergency proofs are accepted in strict JSON only.
func AuthenticationPolicyInputs(c *gin.Context) {
	path := c.Request.URL.Path
	if path != "/api/v1/auth/policy" && path != "/api/v1/admin/auth/policy" && !strings.HasPrefix(path, "/api/v1/auth/emergency") {
		c.Next()
		return
	}
	r := c.Request
	raw, force := r.URL.RawQuery, r.URL.ForceQuery
	r.URL.RawQuery = ""
	r.URL.ForceQuery = false
	r.RequestURI = strings.SplitN(r.RequestURI, "?", 2)[0]
	v := authenticationPolicyInputs{}
	if raw != "" || force {
		v.invalid = true
		if path == "/api/v1/admin/auth/policy" && r.Method == http.MethodGet && len(raw) <= 256 && !force {
			q, e := url.ParseQuery(raw)
			values := q["emergency_admin_id"]
			if e == nil && len(q) == 1 && len(values) == 1 && len(values[0]) == 30 && strings.HasPrefix(values[0], "usr_") {
				v.candidate = values[0]
				v.invalid = false
			}
		}
	}
	*r = *r.WithContext(context.WithValue(r.Context(), authenticationPolicyInputsKey{}, v))
	c.Next()
}
func authenticationPolicyRequest(r *http.Request) (authenticationPolicyInputs, bool) {
	v, ok := r.Context().Value(authenticationPolicyInputsKey{}).(authenticationPolicyInputs)
	return v, ok && !v.invalid
}
