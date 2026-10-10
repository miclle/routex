package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"github.com/fox-gonic/fox"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestSAMLInputCaptureAndRedaction(t *testing.T) {
	proof := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	response := base64.StdEncoding.EncodeToString([]byte("bounded-private-XML"))
	good := url.Values{"SAMLResponse": {response}, "RelayState": {proof}}.Encode()
	for _, tc := range []struct {
		name, body, content, query, encoding string
		wantInvalid                          bool
	}{
		{name: "valid", body: good, content: "application/x-www-form-urlencoded"},
		{name: "utf8", body: good, content: "application/x-www-form-urlencoded; charset=UTF-8"},
		{name: "query", body: good, content: "application/x-www-form-urlencoded", query: "?SAMLResponse=private", wantInvalid: true},
		{name: "empty_query", body: good, content: "application/x-www-form-urlencoded", query: "?", wantInvalid: true},
		{name: "unknown", body: good + "&extra=private", content: "application/x-www-form-urlencoded", wantInvalid: true},
		{name: "duplicate", body: good + "&RelayState=" + proof, content: "application/x-www-form-urlencoded", wantInvalid: true},
		{name: "missing", body: "RelayState=" + proof, content: "application/x-www-form-urlencoded", wantInvalid: true},
		{name: "empty_response", body: "SAMLResponse=&RelayState=" + proof, content: "application/x-www-form-urlencoded", wantInvalid: true},
		{name: "invalid_escape", body: good + "%zz", content: "application/x-www-form-urlencoded", wantInvalid: true},
		{name: "bad_relay", body: "SAMLResponse=" + response + "&RelayState=not-a-proof", content: "application/x-www-form-urlencoded", wantInvalid: true},
		{name: "json", body: good, content: "application/json", wantInvalid: true},
		{name: "multipart", body: good, content: "multipart/form-data; boundary=x", wantInvalid: true},
		{name: "latin1", body: good, content: "application/x-www-form-urlencoded; charset=latin1", wantInvalid: true},
		{name: "compressed", body: good, content: "application/x-www-form-urlencoded", encoding: "gzip", wantInvalid: true},
		{name: "oversized", body: strings.Repeat("x", samlFormLimit+1), content: "application/x-www-form-urlencoded", wantInvalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "https://routex.test"+samlACSPath+tc.query, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.content)
			req.Header.Set("Content-Encoding", tc.encoding)
			req.Header.Add("Cookie", "routex_session=retained; "+samlStartCookie+"="+proof+"; "+samlDeliveryCookie+"="+proof)
			got := captureSAMLInputs(req)
			if got.invalid != tc.wantInvalid {
				t.Fatal("unexpected form admission")
			}
			if !got.invalid && (got.response != response || got.relay != proof || got.start != proof || got.delivery != proof) {
				t.Fatal("valid bounded form/cookies lost")
			}
			if got.invalid && (got.response != "" || got.relay != "" || got.start != "" || got.delivery != "") {
				t.Fatal("invalid capture retained usable proof")
			}
			if req.URL.RawQuery != "" || req.URL.ForceQuery || strings.Contains(req.RequestURI, "?") || strings.Contains(req.Header.Get("Cookie"), proof) || req.Header.Get("Cookie") != "routex_session=retained" {
				t.Fatal("ordinary request retained private query/cookie")
			}
			raw, err := io.ReadAll(req.Body)
			if err != nil || len(raw) != 0 || req.ContentLength != 0 || req.PostForm != nil || req.Form != nil {
				t.Fatal("ordinary request retained form body")
			}
			if repeat := captureSAMLInputs(req); repeat != got {
				t.Fatal("capture was not idempotent")
			}
		})
	}
}

func TestSAMLCookieAmbiguityAndGlobalPrivacy(t *testing.T) {
	proof := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32))
	other := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
	for _, tc := range []struct {
		name            string
		headers         []string
		bad             bool
		start, delivery string
	}{
		{name: "pair", headers: []string{samlStartCookie + "=" + proof + "; " + samlDeliveryCookie + "=" + other}, start: proof, delivery: other},
		{name: "matching_duplicate", headers: []string{samlStartCookie + "=" + proof + "; " + samlStartCookie + "=" + proof}, bad: true},
		{name: "multiple_headers", headers: []string{samlStartCookie + "=" + proof, samlStartCookie + "=" + other}, bad: true},
		{name: "delivery_duplicate", headers: []string{samlDeliveryCookie + "=" + proof, samlDeliveryCookie + "=" + proof}, bad: true},
		{name: "quoted", headers: []string{samlStartCookie + "=\"" + proof + "\""}, bad: true},
		{name: "empty", headers: []string{samlStartCookie + "="}, bad: true},
		{name: "missing_equals", headers: []string{samlStartCookie}, bad: true},
		{name: "noncanonical", headers: []string{samlStartCookie + "=" + proof + "="}, bad: true},
		{name: "aliases", headers: []string{"__Secure-routex_saml_start=" + proof + "; routex_saml_delivery=" + other}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "https://routex.test/ordinary-path", nil)
			for _, header := range tc.headers {
				req.Header.Add("Cookie", header)
			}
			got := captureSAMLInputs(req)
			if got.invalid != tc.bad || got.start != tc.start || got.delivery != tc.delivery {
				t.Fatal("ambiguous or alias cookie conferred proof")
			}
			if strings.Contains(req.Header.Get("Cookie"), samlStartCookie+"=") || strings.Contains(req.Header.Get("Cookie"), samlDeliveryCookie+"=") {
				t.Fatal("host proofs retained on non-auth request")
			}
		})
	}
}

func TestSAMLFormAndCookieRecoveryPrivacy(t *testing.T) {
	proof := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{11}, 32))
	response := base64.StdEncoding.EncodeToString([]byte("private-SAML-assertion"))
	var logs bytes.Buffer
	router := gin.New()
	router.Use(gin.LoggerWithWriter(&logs), gin.RecoveryWithWriter(&logs), SAMLInputs)
	router.POST(samlACSPath, func(c *gin.Context) {
		if captureSAMLInputs(c.Request).invalid {
			t.Error("valid private form rejected")
		}
		panic("controlled recovery")
	})
	req := httptest.NewRequest("POST", "https://routex.test"+samlACSPath, strings.NewReader(url.Values{"SAMLResponse": {response}, "RelayState": {proof}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", samlStartCookie+"="+proof)
	out := httptest.NewRecorder()
	writer := &samlDeadlineWriter{ResponseWriter: out}
	router.ServeHTTP(writer, req)
	if len(writer.deadlines) != 2 || writer.deadlines[0].IsZero() || !writer.deadlines[1].IsZero() {
		t.Fatal("ACS capture lost finite transport deadline/reset")
	}
	if out.Code != 500 {
		t.Fatal("recovery did not execute")
	}
	for _, private := range []string{proof, response, "private-SAML-assertion"} {
		if strings.Contains(logs.String(), private) || strings.Contains(out.Body.String(), private) {
			t.Fatal("recovery reflected protocol proof")
		}
	}
}

func TestSAMLHostCookieAttributes(t *testing.T) {
	value := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{12}, 32))
	expires := time.Now().Add(5 * time.Minute)
	router := fox.New()
	router.GET("/set", func(c *fox.Context) {
		if !samlSetCookie(c, samlStartCookie, value, expires) || !samlSetCookie(c, samlDeliveryCookie, value, expires) {
			t.Error("valid host proof cookie rejected")
		}
		c.Status(204)
	})
	router.GET("/clear", func(c *fox.Context) { samlClearCookies(c); c.Status(204) })
	router.GET("/expired", func(c *fox.Context) {
		if samlSetCookie(c, samlStartCookie, value, time.Now().Add(-time.Second)) {
			t.Error("expired proof cookie set")
		}
		c.Status(204)
	})
	for _, path := range []string{"/set", "/clear", "/expired"} {
		t.Run(path, func(t *testing.T) {
			out := httptest.NewRecorder()
			router.ServeHTTP(out, httptest.NewRequest("GET", "https://routex.test"+path, nil))
			cookies := out.Result().Cookies()
			if path == "/expired" {
				if len(cookies) != 0 {
					t.Fatal("expired proof was emitted")
				}
				return
			}
			if len(cookies) != 2 || cookies[0].Name != samlStartCookie || cookies[1].Name != samlDeliveryCookie {
				t.Fatal("exact host cookie pair lost")
			}
			for _, cookie := range cookies {
				if cookie.Domain != "" || cookie.Path != "/" || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
					t.Fatal("host cookie attributes weakened")
				}
				if path == "/set" && (cookie.Value != value || cookie.MaxAge <= 0 || cookie.MaxAge > 300 || cookie.Expires.After(expires)) {
					t.Fatal("proof expiry or value changed")
				}
				if path == "/clear" && (cookie.Value != "" || cookie.MaxAge != -1) {
					t.Fatal("host proof was not cleared")
				}
			}
		})
	}
}

// A direct recorder is deliberately unsupported. Tests that execute ACS
// middleware must supply the same explicit deadline capability as net/http.
type samlDeadlineWriter struct {
	http.ResponseWriter
	deadlines            []time.Time
	observed             []time.Time
	setError, resetError error
}

func (w *samlDeadlineWriter) SetReadDeadline(value time.Time) error {
	w.observed = append(w.observed, time.Now())
	w.deadlines = append(w.deadlines, value)
	if value.IsZero() {
		return w.resetError
	}
	return w.setError
}

type samlUnreadBody struct{ reads, closes int }

func (b *samlUnreadBody) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (b *samlUnreadBody) Close() error             { b.closes++; return nil }

func TestSAMLACSUnsupportedDeadlineDiscardsWithoutReadingOrClosing(t *testing.T) {
	proof := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{21}, 32))
	for _, mode := range []string{"unsupported", "set_error"} {
		t.Run(mode, func(t *testing.T) {
			router := gin.New()
			router.Use(SAMLInputs)
			called := false
			router.POST(samlACSPath, func(c *gin.Context) {
				called = true
				got := captureSAMLInputs(c.Request)
				if !got.invalid || got.start != "" || got.delivery != "" || got.response != "" || got.relay != "" || !c.Request.Close || c.Request.Body != http.NoBody || c.Request.ContentLength != 0 || c.Request.Form != nil || c.Request.PostForm != nil || c.Request.URL.RawQuery != "" || strings.Contains(c.Request.RequestURI, "?") || c.Request.Header.Get("Cookie") != "routex_session=retained" {
					t.Error("unsupported transport retained actionable or loggable protocol input")
				}
				c.Redirect(http.StatusSeeOther, "/auth/saml/complete")
			})
			body := &samlUnreadBody{}
			req := httptest.NewRequest("POST", "https://routex.test"+samlACSPath+"?private=discard", nil)
			req.Body, req.ContentLength = body, 100
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Cookie", "routex_session=retained; "+samlStartCookie+"="+proof+"; "+samlDeliveryCookie+"="+proof)
			req.Form = url.Values{"private": {"discard"}}
			req.PostForm = url.Values{"private": {"discard"}}
			out := httptest.NewRecorder()
			var writer http.ResponseWriter = out
			if mode == "set_error" {
				writer = &samlDeadlineWriter{ResponseWriter: out, setError: errors.New("controlled unavailable deadline")}
			}
			router.ServeHTTP(writer, req)
			if !called || out.Code != 303 || out.Header().Get("Location") != "/auth/saml/complete" || body.reads != 0 || body.closes != 0 {
				t.Fatal("unsupported deadline performed unbounded body work or changed fixed outcome")
			}
		})
	}
}

func TestSAMLACSDeadlineResetFailureRejectsCapturedProof(t *testing.T) {
	proof := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{22}, 32))
	payload := url.Values{"SAMLResponse": {base64.StdEncoding.EncodeToString([]byte("bounded-proof"))}, "RelayState": {proof}}.Encode()
	router := gin.New()
	router.Use(SAMLInputs)
	called := false
	router.POST(samlACSPath, func(c *gin.Context) {
		called = true
		in := captureSAMLInputs(c.Request)
		if !in.invalid || in.response != "" || in.relay != "" || !c.Request.Close {
			t.Error("failed deadline reset retained staging authority")
		}
		c.Redirect(http.StatusSeeOther, "/auth/saml/complete")
	})
	req := httptest.NewRequest("POST", "https://routex.test"+samlACSPath, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	out := httptest.NewRecorder()
	writer := &samlDeadlineWriter{ResponseWriter: out, resetError: errors.New("controlled reset failure")}
	router.ServeHTTP(writer, req)
	if !called || out.Code != 303 || len(writer.deadlines) != 2 || writer.deadlines[0].IsZero() || !writer.deadlines[1].IsZero() {
		t.Fatal("reset failure skipped bounded capture or fixed failure outcome")
	}
}

func TestSAMLACSUsesOriginalOperationDeadline(t *testing.T) {
	proof := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{23}, 32))
	payload := url.Values{"SAMLResponse": {base64.StdEncoding.EncodeToString([]byte("bounded-proof"))}, "RelayState": {proof}}.Encode()
	for _, limit := range []time.Duration{time.Hour, 2 * time.Second} {
		t.Run(limit.String(), func(t *testing.T) {
			parent, cancel := context.WithTimeout(context.Background(), limit)
			defer cancel()
			start := time.Now()
			var operation context.Context
			router := gin.New()
			router.Use(SAMLInputs)
			router.POST(samlACSPath, func(c *gin.Context) {
				operation = c.Request.Context()
				deadline, ok := operation.Deadline()
				if !ok || !deadline.After(start) || operation.Err() != nil || captureSAMLInputs(c.Request).invalid {
					t.Error("valid capture renewed, lost or cancelled original operation authority")
				}
				c.Status(204)
			})
			req := httptest.NewRequest("POST", "https://routex.test"+samlACSPath, strings.NewReader(payload)).WithContext(parent)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			out := httptest.NewRecorder()
			writer := &samlDeadlineWriter{ResponseWriter: out}
			router.ServeHTTP(writer, req)
			if out.Code != 204 || operation == nil || !errors.Is(operation.Err(), context.Canceled) || len(writer.deadlines) != 2 || !writer.deadlines[1].IsZero() {
				t.Fatal("original context was not held through handler and cancelled afterwards")
			}
			operationEnd, _ := operation.Deadline()
			parentEnd, _ := parent.Deadline()
			if operationEnd.After(writer.observed[0].Add(10*time.Second)) || operationEnd.After(parentEnd) {
				t.Fatal("operation deadline exceeded original ten-second or parent budget")
			}
			if writer.deadlines[0].After(operationEnd) || writer.deadlines[0].After(writer.observed[0].Add(5*time.Second)) || !writer.deadlines[0].After(start) {
				t.Fatal("body deadline exceeded original five-second or parent budget")
			}
		})
	}
}

func TestSAMLACSIncompleteNetworkBodyHasFiniteReadAndHandlerJoin(t *testing.T) {
	// A single eight-second observation/cleanup end contains the actual five-
	// second product body deadline; no renewal waits for a late positive result.
	end := time.Now().Add(8 * time.Second)
	joined := make(chan struct{})
	router := gin.New()
	router.Use(SAMLInputs)
	router.POST(samlACSPath, func(c *gin.Context) {
		defer close(joined)
		if !captureSAMLInputs(c.Request).invalid {
			t.Error("incomplete body admitted protocol staging")
		}
		c.Redirect(http.StatusSeeOther, "/auth/saml/complete")
	})
	server := httptest.NewServer(router)
	defer server.Close()
	connection, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Until(end))
	if err != nil {
		t.Fatal(err)
	}
	// Close the exact client before server.Close on every failure path.
	defer func() {
		if err := connection.Close(); err != nil {
			t.Error("close exact withheld-body client", err)
		}
	}()
	if err := connection.SetDeadline(end); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(connection, "POST "+samlACSPath+" HTTP/1.1\r\nHost: routex.test\r\nContent-Type: application/x-www-form-urlencoded\r\nContent-Length: 2\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	if err != nil {
		t.Fatal("withheld body did not receive bounded failure response", err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Error("close withheld-body response", err)
		}
	}()
	if !time.Now().Before(end) || response.StatusCode != 303 || response.Header.Get("Location") != "/auth/saml/complete" {
		t.Fatal("withheld body exceeded fixed observation bound or changed failure route")
	}
	remaining := time.Until(end)
	if remaining <= 0 {
		t.Fatal("handler did not join before original observation end")
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-joined:
	case <-timer.C:
		t.Fatal("withheld body handler did not join within original observation end")
	}
	if !time.Now().Before(end) {
		t.Fatal("late handler join exceeded original observation end")
	}
}
