package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const applicationURL = "http://127.0.0.1:19001"
const controlURL = "http://127.0.0.1:8200"

type requestFact struct {
	Name, Method, Path, SHA256 string
	Status, Bytes              int
	CookieHeaderPresent        bool
}

type fixtureOwner struct {
	ctx             context.Context
	client          *http.Client
	dir, phase      string
	csrf, stage     string
	facts           []requestFact
	sensitiveValues [][]byte
}

func checksum(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func privateInput(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() < 1 || info.Size() > limit {
		return nil, errFixture
	}
	raw, err := os.ReadFile(path)
	if err != nil || int64(len(raw)) != info.Size() {
		return nil, errFixture
	}
	return raw, nil
}

func (o *fixtureOwner) save(name string, value any) error {
	if filepath.Base(name) != name {
		return errFixture
	}
	raw, ok := value.([]byte)
	if !ok {
		var err error
		raw, err = json.Marshal(value)
		if err != nil {
			return errFixture
		}
	}
	return writePrivate(filepath.Join(o.dir, o.phase+"-"+name), raw)
}

func newFixtureOwner(ctx context.Context, dir, phase string) *fixtureOwner {
	jar, _ := cookiejar.New(nil)
	transport := &http.Transport{
		Proxy: nil, DisableKeepAlives: true,
		DialContext:           (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		ResponseHeaderTimeout: 35 * time.Second,
	}
	return &fixtureOwner{ctx: ctx, dir: dir, phase: phase, stage: "input", client: &http.Client{
		Jar: jar, Transport: transport, Timeout: 40 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (o *fixtureOwner) request(name, base, method, path, token, etag string, input any, expected int, output any) error {
	o.stage = name
	if base != applicationURL && base != controlURL {
		return errFixture
	}
	var raw []byte
	if input != nil {
		var err error
		raw, err = json.Marshal(input)
		if err != nil || len(raw) > 64<<10 {
			return errFixture
		}
	}
	req, err := http.NewRequestWithContext(o.ctx, method, base+path, bytes.NewReader(raw))
	if err != nil {
		return errFixture
	}
	req.GetBody = nil
	req.Close = true
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := o.client
	if base == applicationURL {
		req.Header.Set("Origin", applicationURL)
		if o.csrf != "" {
			req.Header.Set("X-CSRF-Token", o.csrf)
		}
	} else {
		req.Header.Set("X-Vault-Token", token)
		isolated := *client
		isolated.Jar = nil
		client = &isolated
	}
	if etag != "" {
		req.Header.Set("If-Match", strconv.Quote(etag))
	}
	response, err := client.Do(req)
	if err != nil {
		return errFixture
	}
	data, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(data) > 4<<20 || response.Request == nil {
		return errFixture
	}
	cookies := len(response.Request.Cookies()) != 0
	o.facts = append(o.facts, requestFact{name, method, path, checksum(data), response.StatusCode, len(data), cookies})
	if o.save("private-response-"+name+".json", data) != nil || base == controlURL && cookies || response.StatusCode != expected {
		return errFixture
	}
	if output != nil && json.Unmarshal(data, output) != nil {
		return errFixture
	}
	if base == applicationURL && output != nil {
		var value map[string]json.RawMessage
		if json.Unmarshal(data, &value) != nil {
			return errFixture
		}
		if raw, exists := value["review_etag"]; exists {
			var review string
			if json.Unmarshal(raw, &review) != nil || response.Header.Get("ETag") != strconv.Quote(review) {
				return errFixture
			}
		}
	}
	return nil
}

func (o *fixtureOwner) api(name, method, path, etag string, input any, expected int, output any) error {
	return o.request(name, applicationURL, method, "/api/v1"+path, "", etag, input, expected, output)
}

func (o *fixtureOwner) control(name, method, path, token string, input any, expected int) error {
	return o.request(name, controlURL, method, "/v1"+path, token, "", input, expected, nil)
}

func freshUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", errFixture
	}
	raw[6], raw[8] = raw[6]&15|64, raw[8]&63|128
	value := hex.EncodeToString(raw[:])
	return value[:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:], nil
}

func (o *fixtureOwner) restoreCookies(cookies []*http.Cookie) error {
	if len(cookies) != 1 || cookies[0] == nil || len(cookies[0].Value) < 32 {
		return errFixture
	}
	address, _ := url.Parse(applicationURL)
	o.client.Jar.SetCookies(address, cookies)
	return nil
}
