package main

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMutationIsNotReplayedOnRedirect(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Location", "/other")
		w.WriteHeader(307)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	o := owner{ctx: ctx, dir: t.TempDir(), client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if o.request("original", server.URL, "PUT", "/destroy", "new-token", "", map[string]any{"versions": []int{1}}, 204, nil) == nil || calls.Load() != 1 {
		t.Fatal("uncertain mutation was retried or accepted")
	}
	if len(o.http) != 1 || o.http[0].Status != 307 {
		t.Fatal("original numeric refusal missing")
	}
}
func TestResponseOverflowNeverProducesAcknowledgement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(make([]byte, (4<<20)+1))
	}))
	defer server.Close()
	o := owner{ctx: context.Background(), dir: t.TempDir(), client: server.Client()}
	if o.request("overflow", server.URL, "GET", "/", "", "", nil, 200, nil) == nil || len(o.http) != 0 {
		t.Fatal("overflow accepted as a complete response")
	}
}
func TestPrivateInputRefusesSymlinkAndPublicMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "value")
	if os.WriteFile(p, []byte("fresh-private"), 0600) != nil {
		t.Fatal("write")
	}
	if _, err := private(p, 128); err != nil {
		t.Fatal("original private file refused")
	}
	link := filepath.Join(dir, "link")
	if os.Symlink(p, link) != nil {
		t.Fatal("link")
	}
	if _, err := private(link, 128); err == nil {
		t.Fatal("symlink admitted")
	}
	if os.Chmod(p, 0644) != nil {
		t.Fatal("chmod")
	}
	if _, err := private(p, 128); err == nil {
		t.Fatal("public material admitted")
	}
}
func TestUnboundRunNeverCreatesArtifacts(t *testing.T) {
	if run(nil) != 1 || run([]string{"--endpoint"}) != 1 {
		t.Fatal("unbound or overridden execution admitted")
	}
}

func TestVaultObservationDoesNotCarryApplicationSession(t *testing.T) {
	var leaked atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(len(r.Cookies()) != 0)
		http.SetCookie(w, &http.Cookie{Name: "routex_session", Value: "foreign-cookie-fixture", Path: "/"})
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse(server.URL)
	jar.SetCookies(u, []*http.Cookie{{Name: "routex_session", Value: "session-isolation-fixture-value", Path: "/"}})
	o := owner{ctx: context.Background(), dir: t.TempDir(), client: &http.Client{Jar: jar}}
	if o.request("isolated-vault", server.URL, "GET", "/metrics", "fresh-vault-token", "", nil, 200, nil) != nil {
		t.Fatal("observation failed")
	}
	if leaked.Load() {
		t.Fatal("application Session crossed into Vault observer transport")
	}
	if len(jar.Cookies(u)) != 1 || jar.Cookies(u)[0].Value != "session-isolation-fixture-value" {
		t.Fatal("application Session was destroyed instead of scoped")
	}
}

func TestRestartLifecycleNeedsPositiveClosureAndFreshExactLease(t *testing.T) {
	birth := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	closed := birth.Add(time.Second)
	restart := birth.Add(2 * time.Second)
	fresh := restart.Add(time.Millisecond)
	asOf := restart.Add(time.Second)
	baseline := func() lifecycleFacts {
		return lifecycleFacts{Instances: []instanceFact{{ID: "original", Role: "combined", StartedAt: birth, StoppedAt: &closed}, {ID: "replacement", Role: "combined", StartedAt: fresh, LeaseExpiresAt: asOf.Add(time.Second)}}, Processes: []processFact{{ProcessID: "original", Generation: strings.Repeat("a", 64), Birth: birth, RegisteredAt: birth, ClosedAt: &closed}, {ProcessID: "replacement", Generation: strings.Repeat("b", 64), Birth: fresh, RegisteredAt: fresh}}}
	}
	if !validateLifecycle(baseline(), "original", birth, restart, asOf) {
		t.Fatal("positive old join/current lease refused")
	}
	cases := map[string]func(*lifecycleFacts){
		"heartbeat_expiry_is_not_join": func(f *lifecycleFacts) {
			f.Instances[0].StoppedAt = nil
			f.Processes[0].ClosedAt = nil
			f.Instances[0].LeaseExpiresAt = birth
		},
		"missing_source_close": func(f *lifecycleFacts) { f.Processes[0].ClosedAt = nil },
		"changed_old_birth":    func(f *lifecycleFacts) { f.Instances[0].StartedAt = birth.Add(time.Microsecond) },
		"unmatched_close":      func(f *lifecycleFacts) { v := closed.Add(time.Microsecond); f.Processes[0].ClosedAt = &v },
		"new_generation_before_restart": func(f *lifecycleFacts) {
			f.Instances[1].StartedAt = birth
			f.Processes[1].Birth = birth
			f.Processes[1].RegisteredAt = birth
		},
		"expired_current_lease":  func(f *lifecycleFacts) { f.Instances[1].LeaseExpiresAt = asOf },
		"duplicate_owner":        func(f *lifecycleFacts) { f.Instances[1] = f.Instances[0] },
		"extra_unknown_process":  func(f *lifecycleFacts) { f.Processes = append(f.Processes, f.Processes[1]) },
		"foreign_source_process": func(f *lifecycleFacts) { f.Processes[1].ProcessID = "foreign" },
		"unknown_role":           func(f *lifecycleFacts) { f.Instances[1].Role = "unknown" },
		"retired_current":        func(f *lifecycleFacts) { f.Instances[1].RetiredAt = &asOf },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			facts := baseline()
			change(&facts)
			if validateLifecycle(facts, "original", birth, restart, asOf) {
				t.Fatal("uncertain or foreign lifecycle admitted")
			}
		})
	}
}
