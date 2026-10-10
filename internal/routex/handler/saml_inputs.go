package handler

import (
	"context"
	"encoding/base64"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/miclle/routex/pkg/saml"
)

const samlStartCookie = "__Host-routex_saml_start"
const samlDeliveryCookie = "__Host-routex_saml_delivery"
const samlAuthPath = "/api/v1/auth/saml"
const samlACSPath = samlAuthPath + "/acs"
const samlFormLimit = 3*((saml.MaxResponseBytes+2)/3*4) + 256

type samlInputsKey struct{}
type samlInputs struct {
	start, delivery, relay, response string
	invalid                          bool
}

// SAMLInputs removes correlation cookies on every path, since the host-only
// proofs deliberately use Path=/. ACS form material is captured before ordinary
// request logging/recovery and never restored to the request body or URL.
func SAMLInputs(c *gin.Context) {
	if c.Request.URL.Path != samlACSPath {
		captureSAMLInputs(c.Request)
		c.Next()
		return
	}
	// Body reception and subsequent staging share this original operation end.
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	*c.Request = *c.Request.WithContext(ctx)
	deadline := time.Now().Add(5 * time.Second)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	controller := http.NewResponseController(c.Writer)
	if ctx.Err() != nil || controller.SetReadDeadline(deadline) != nil {
		rejectSAMLBody(c.Request)
		c.Next()
		return
	}
	captureSAMLInputs(c.Request)
	// The body has been read and closed under the same finite transport deadline.
	// A reset failure must not authorize further staging on this connection.
	if controller.SetReadDeadline(time.Time{}) != nil || ctx.Err() != nil {
		rejectSAMLBody(c.Request)
	}
	c.Next()
}

func rejectSAMLBody(r *http.Request) {
	// Unsupported transports retain responsibility for their original body and
	// connection. Do not read or Close that body here: either may block. This
	// discard is fail-closed input handling, not proof that its owner has joined.
	r.Close = true
	r.Body = http.NoBody
	r.ContentLength = 0
	r.Header.Del("Content-Length")
	r.Form = nil
	r.PostForm = nil
	captureSAMLInputs(r)
	*r = *r.WithContext(context.WithValue(r.Context(), samlInputsKey{}, samlInputs{invalid: true}))
}
func samlOpaqueCookie(value string) bool {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	return err == nil && len(raw) == 32 && base64.RawURLEncoding.EncodeToString(raw) == value
}
func captureSAMLInputs(r *http.Request) samlInputs {
	if v, ok := r.Context().Value(samlInputsKey{}).(samlInputs); ok {
		return v
	}
	v := samlInputs{}
	counts := map[string]int{}
	kept := []string{}
	for _, header := range r.Header.Values("Cookie") {
		for _, part := range strings.Split(header, ";") {
			part = strings.TrimSpace(part)
			key, value, found := strings.Cut(part, "=")
			if key != samlStartCookie && key != samlDeliveryCookie {
				if part != "" {
					kept = append(kept, part)
				}
				continue
			}
			counts[key]++
			if !found || !samlOpaqueCookie(value) {
				v.invalid = true
			}
			if key == samlStartCookie {
				v.start = value
			} else {
				v.delivery = value
			}
		}
	}
	if counts[samlStartCookie] > 1 || counts[samlDeliveryCookie] > 1 {
		v.invalid = true
	}
	r.Header.Del("Cookie")
	if len(kept) > 0 {
		r.Header.Set("Cookie", strings.Join(kept, "; "))
	}
	relevant := r.URL.Path == samlAuthPath || strings.HasPrefix(r.URL.Path, samlAuthPath+"/") || strings.HasPrefix(r.URL.Path, "/api/v1/admin/auth/saml") || strings.HasPrefix(r.URL.Path, "/api/v1/account/identity/saml")
	if relevant {
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			v.invalid = true
		}
		r.URL.RawQuery = ""
		r.URL.ForceQuery = false
		r.RequestURI = strings.SplitN(r.RequestURI, "?", 2)[0]
	}
	if r.URL.Path == samlACSPath {
		media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/x-www-form-urlencoded" || r.Method != http.MethodPost || r.Header.Get("Content-Encoding") != "" || r.ContentLength > samlFormLimit {
			v.invalid = true
		}
		for k, value := range params {
			if k != "charset" || !strings.EqualFold(value, "utf-8") {
				v.invalid = true
			}
		}
		var raw []byte
		if r.Body != nil {
			raw, err = io.ReadAll(io.LimitReader(r.Body, samlFormLimit+1))
			closeErr := r.Body.Close()
			if err != nil || closeErr != nil || len(raw) > samlFormLimit {
				v.invalid = true
			}
		}
		r.Body = http.NoBody
		r.ContentLength = 0
		r.Header.Del("Content-Length")
		r.Form = nil
		r.PostForm = nil
		if !v.invalid {
			form, err := url.ParseQuery(string(raw))
			if err != nil || len(form) != 2 || len(form["SAMLResponse"]) != 1 || len(form["RelayState"]) != 1 {
				v.invalid = true
			} else {
				v.response = form.Get("SAMLResponse")
				v.relay = form.Get("RelayState")
				if len(v.response) == 0 || len(v.response) > base64.StdEncoding.EncodedLen(saml.MaxResponseBytes) || !samlOpaqueCookie(v.relay) {
					v.invalid = true
				}
			}
		}
	}
	if v.invalid {
		v.start = ""
		v.delivery = ""
		v.response = ""
		v.relay = ""
	}
	*r = *r.WithContext(context.WithValue(r.Context(), samlInputsKey{}, v))
	return v
}
