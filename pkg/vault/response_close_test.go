package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type closeTransport struct {
	base http.RoundTripper
	wrap func(*http.Request, io.ReadCloser) io.ReadCloser
}

func (r closeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := r.base.RoundTrip(req)
	if res != nil && res.Body != nil {
		res.Body = r.wrap(req, res.Body)
	}
	return res, err
}

type countedCloseBody struct {
	io.ReadCloser
	closes  *atomic.Int32
	err     error
	entered chan struct{}
	release chan struct{}
}

func (b *countedCloseBody) Close() error {
	b.closes.Add(1)
	if b.entered != nil {
		close(b.entered)
		<-b.release
	}
	err := b.ReadCloser.Close()
	if b.err != nil {
		return b.err
	}
	return err
}

func TestCredentialOwnershipCloseErrorStopsDestroy(t *testing.T) {
	f := &credentialFixture{}
	c := credentialClient(t, f)
	p := preparedCredential(t, c)
	writeCredential(t, c, p)
	var closes atomic.Int32
	c.http.Transport = closeTransport{base: c.http.Transport, wrap: func(req *http.Request, b io.ReadCloser) io.ReadCloser {
		if req.Method == http.MethodGet {
			return &countedCloseBody{ReadCloser: b, closes: &closes, err: errors.New("private close error")}
		}
		return b
	}}
	result, err := c.CleanupCredentialOwned(t.Context(), "reader-test", "cleanup-test", p.Plan())
	if err == nil || !result.Ownership.Attempted || !result.Ownership.Succeeded || result.Version != 1 || result.Cleanup.State != "unknown" || result.Cleanup.Observation.Attempted || result.Cleanup.Observation.Succeeded || f.count() != 2 || closes.Load() != 1 {
		t.Fatal("failed ownership response Close must retain read evidence without issuing destroy")
	}
	if strings.Contains(err.Error(), "private") {
		t.Fatal("Close error escaped sanitized failure")
	}
}

func TestFiniteResponseCloseBlocksReturnAndIdleCloseCannotJoin(t *testing.T) {
	for _, login := range []bool{false, true} {
		name := "kv"
		if login {
			name = "login"
		}
		t.Run(name, func(t *testing.T) {
			c := loginFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if login {
					_, _ = io.WriteString(w, loginResponse)
				} else {
					_, _ = io.WriteString(w, `{}`)
				}
			})
			var count atomic.Int32
			entered, release := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			c.http.Transport = closeTransport{base: c.http.Transport, wrap: func(_ *http.Request, b io.ReadCloser) io.ReadCloser {
				return &countedCloseBody{ReadCloser: b, closes: &count, entered: entered, release: release}
			}}
			done := make(chan bool, 1)
			go func() {
				if login {
					tok, obs, err := c.LoginAppRole(t.Context(), "approle", "role", "secret")
					if tok != nil {
						tok.Close()
					}
					done <- err == nil && obs.Succeeded
				} else {
					_, obs := c.request(t.Context(), "read", http.MethodGet, "data", "item", "", "reader", nil)
					done <- obs.Failure == nil && obs.Attempted
				}
			}()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("Close never entered")
			}
			state := c.ResponseCloseState()
			if !state.Observed || state.Pending != 1 || state.Failed {
				t.Fatal("pending body not owned")
			}
			c.Close()
			if c.ResponseCloseState() != state {
				t.Fatal("idle cleanup changed response evidence")
			}
			select {
			case <-done:
				t.Fatal("finite method returned before Close")
			default:
			}
			close(release)
			select {
			case good := <-done:
				if !good {
					t.Fatal("remote observation changed")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Close not joined")
			}
			if count.Load() != 1 || c.ResponseCloseState() != (ResponseCloseState{Observed: true}) {
				t.Fatal("body not closed exactly once")
			}
		})
	}
}

func TestFiniteResponseCloseErrorsPreserveRemoteObservationsAndRemainSticky(t *testing.T) {
	for _, login := range []bool{false, true} {
		name := "kv"
		if login {
			name = "login"
		}
		t.Run(name, func(t *testing.T) {
			var calls, closes atomic.Int32
			c := loginFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if login {
					_, _ = io.WriteString(w, loginResponse)
				} else {
					_, _ = io.WriteString(w, `{}`)
				}
			})
			c.http.Transport = closeTransport{base: c.http.Transport, wrap: func(_ *http.Request, b io.ReadCloser) io.ReadCloser {
				return &countedCloseBody{ReadCloser: b, closes: &closes, err: errors.New("private close detail")}
			}}
			if login {
				tok, obs, err := c.LoginAppRole(t.Context(), "approle", "role", "secret")
				if err != nil || tok == nil || !obs.Succeeded || obs.Failure != nil {
					t.Fatal("known Login result overwritten")
				}
				tok.Close()
			} else {
				_, obs := c.request(t.Context(), "read", http.MethodGet, "data", "item", "", "reader", nil)
				if obs.Failure != nil || !obs.Attempted {
					t.Fatal("known HTTP result overwritten")
				}
			}
			want := ResponseCloseState{Observed: true, Failed: true}
			if c.ResponseCloseState() != want || closes.Load() != 1 {
				t.Fatal("missing fixed Close evidence")
			}
			c.Close()
			if c.ResponseCloseState() != want {
				t.Fatal("idle Close reset failure")
			}
			if calls.Load() != 1 {
				t.Fatal("close observation replayed HTTP")
			}
		})
	}
}

type closeReadError struct{ io.ReadCloser }

func (b closeReadError) Read([]byte) (int, error) { return 0, errors.New("private read error") }
func TestResponseCloseRunsOnEveryFiniteFailureExit(t *testing.T) {
	for _, login := range []bool{false, true} {
		for _, kind := range []string{"denied", "invalid", "oversize", "read-error"} {
			t.Run(fmt.Sprintf("login=%t/%s", login, kind), func(t *testing.T) {
				c := loginFixture(t, func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					switch kind {
					case "denied":
						w.WriteHeader(403)
					case "invalid":
						_, _ = io.WriteString(w, `{broken`)
					case "oversize":
						_, _ = io.WriteString(w, strings.Repeat("x", maxResponseBytes+1))
					default:
						_, _ = io.WriteString(w, `{}`)
					}
				})
				var closes atomic.Int32
				c.http.Transport = closeTransport{base: c.http.Transport, wrap: func(_ *http.Request, b io.ReadCloser) io.ReadCloser {
					if kind == "read-error" {
						b = closeReadError{b}
					}
					return &countedCloseBody{ReadCloser: b, closes: &closes, err: errors.New("private close error")}
				}}
				var obs Observation
				if login {
					tok, o, err := c.LoginAppRole(t.Context(), "approle", "role", "secret")
					obs = o
					if tok != nil || err == nil {
						t.Fatal("bad Login accepted")
					}
				} else {
					_, obs = c.request(t.Context(), "read", http.MethodGet, "data", "item", "", "reader", nil)
				}
				if !obs.Attempted || obs.Failure == nil || closes.Load() != 1 || c.ResponseCloseState() != (ResponseCloseState{Observed: true, Failed: true}) {
					t.Fatal("failure bypassed synchronous close evidence")
				}
			})
		}
	}
}

func TestConcurrentResponseCloseSnapshotDoesNotBorrowAnotherJoin(t *testing.T) {
	c := loginFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	})
	var closes atomic.Int32
	entered := []chan struct{}{make(chan struct{}), make(chan struct{})}
	release := []chan struct{}{make(chan struct{}), make(chan struct{})}
	defer func() {
		for _, ch := range release {
			select {
			case <-ch:
			default:
				close(ch)
			}
		}
	}()
	c.http.Transport = closeTransport{base: c.http.Transport, wrap: func(req *http.Request, b io.ReadCloser) io.ReadCloser {
		i := 0
		if strings.HasSuffix(req.URL.Path, "/second") {
			i = 1
		}
		var err error
		if i == 0 {
			err = errors.New("private")
		}
		return &countedCloseBody{ReadCloser: b, closes: &closes, err: err, entered: entered[i], release: release[i]}
	}}
	done := []chan Observation{make(chan Observation, 1), make(chan Observation, 1)}
	for i, path := range []string{"first", "second"} {
		go func(i int, path string) {
			_, obs := c.request(t.Context(), "read", http.MethodGet, "data", path, "", "reader", nil)
			done[i] <- obs
		}(i, path)
	}
	for _, ch := range entered {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatal("Close not entered")
		}
	}
	if c.ResponseCloseState() != (ResponseCloseState{Observed: true, Pending: 2}) {
		t.Fatal("concurrent bodies missing")
	}
	close(release[0])
	if obs := <-done[0]; obs.Failure != nil || !obs.responseCloseFailed {
		t.Fatal("remote response overwritten")
	}
	if c.ResponseCloseState() != (ResponseCloseState{Observed: true, Pending: 1, Failed: true}) {
		t.Fatal("failed Close borrowed pending body's join")
	}
	close(release[1])
	if obs := <-done[1]; obs.responseCloseFailed || obs.Failure != nil {
		t.Fatal("other request borrowed failed Close attribution")
	}
	if closes.Load() != 2 || c.ResponseCloseState() != (ResponseCloseState{Observed: true, Failed: true}) {
		t.Fatal("success erased sticky failure")
	}
	other := loginFixture(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{}`) })
	if other.ResponseCloseState() != (ResponseCloseState{}) {
		t.Fatal("evidence leaked between Clients")
	}
}

func TestCredentialDestroyAcknowledgmentCloseErrorDoesNotReplay(t *testing.T) {
	f := &credentialFixture{}
	c := credentialClient(t, f)
	p := preparedCredential(t, c)
	writeCredential(t, c, p)
	var closes atomic.Int32
	c.http.Transport = closeTransport{base: c.http.Transport, wrap: func(req *http.Request, b io.ReadCloser) io.ReadCloser {
		if req.Method == http.MethodPut {
			return &countedCloseBody{ReadCloser: b, closes: &closes, err: errors.New("private close")}
		}
		return b
	}}
	result, err := c.CleanupCredentialOwned(t.Context(), "reader-test", "cleanup-test", p.Plan())
	if err != nil || result.Cleanup.State != "acknowledged" || !result.Cleanup.Observation.Succeeded || !result.Ownership.Succeeded || closes.Load() != 1 || !c.ResponseCloseState().Failed || f.count() != 3 {
		t.Fatal("remote destroy acknowledgment overwritten")
	}
}

func TestCredentialWriteCloseErrorRetainsExactVersion(t *testing.T) {
	f := &credentialFixture{}
	c := credentialClient(t, f)
	p := preparedCredential(t, c)
	var closes atomic.Int32
	c.http.Transport = closeTransport{base: c.http.Transport, wrap: func(_ *http.Request, b io.ReadCloser) io.ReadCloser {
		return &countedCloseBody{ReadCloser: b, closes: &closes, err: errors.New("private")}
	}}
	result, err := c.WriteCredential(t.Context(), "writer-test", p, []byte("test-only-secret"))
	if err != nil || result.Version != 1 || !result.Write.Succeeded || !c.ResponseCloseState().Failed || closes.Load() != 1 || f.count() != 1 {
		t.Fatal("known write became unknown due only to Close")
	}
}

type closeRoundTripFunc func(*http.Request) (*http.Response, error)

func (f closeRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestFiniteNoResponseIsNotSuccessfulCloseEvidence(t *testing.T) {
	c := loginFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("no network expected") })
	var calls atomic.Int32
	c.http.Transport = closeRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("private transport error")
	})
	_, obs := c.request(t.Context(), "read", http.MethodGet, "data", "item", "", "reader", nil)
	if !obs.Attempted || obs.Failure == nil || obs.Failure.Code != "transport" || c.ResponseCloseState() != (ResponseCloseState{Failed: true}) || calls.Load() != 1 {
		t.Fatal("absent response fabricated a Close")
	}
}
func TestFiniteRedirectFailureDoesNotCloseResponseTwiceOrInventJoin(t *testing.T) {
	c := loginFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/elsewhere")
		w.WriteHeader(http.StatusFound)
	})
	var closes atomic.Int32
	c.http.Transport = closeTransport{base: c.http.Transport, wrap: func(_ *http.Request, b io.ReadCloser) io.ReadCloser {
		return &countedCloseBody{ReadCloser: b, closes: &closes, err: errors.New("private Close error")}
	}}
	_, obs := c.request(t.Context(), "read", http.MethodGet, "data", "item", "", "reader", nil)
	if obs.Failure == nil || obs.Failure.Code != "transport" || closes.Load() != 1 || c.ResponseCloseState() != (ResponseCloseState{Failed: true}) {
		t.Fatal("already closed redirect body became successful drain or closed twice")
	}
}

func TestClientCopyCannotResetResponseCloseFailure(t *testing.T) {
	c := loginFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	})
	var closes atomic.Int32
	c.http.Transport = closeTransport{base: c.http.Transport, wrap: func(_ *http.Request, b io.ReadCloser) io.ReadCloser {
		return &countedCloseBody{ReadCloser: b, closes: &closes, err: errors.New("private")}
	}}
	_, obs := c.request(t.Context(), "read", http.MethodGet, "data", "item", "", "reader", nil)
	if obs.Failure != nil || !c.ResponseCloseState().Failed {
		t.Fatal("missing original Close failure")
	}
	copyClient := *c
	if copyClient.ResponseCloseState() != c.ResponseCloseState() {
		t.Fatal("copy manufactured independent clean lifetime")
	}
}

func TestDoDiscardedTransportResponseNeverCertifiesBodyJoin(t *testing.T) {
	c := loginFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("no network expected") })
	var calls, closes atomic.Int32
	body := &countedCloseBody{ReadCloser: io.NopCloser(strings.NewReader(`{}`)), closes: &closes, err: errors.New("private Close failure")}
	// Deliberately exercise the net/http defensive path for an invalid transport
	// response+error pair. Do discards the response without closing it; the SDK
	// cannot infer its body joined. The test owns and disposes that fake body.
	c.http.Transport = closeRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, Body: body, Header: make(http.Header)}, errors.New("private transport error")
	})
	_, obs := c.request(t.Context(), "read", http.MethodGet, "data", "item", "", "reader", nil)
	if !obs.Attempted || obs.Failure == nil || obs.Failure.Code != "transport" || calls.Load() != 1 || closes.Load() != 0 || c.ResponseCloseState() != (ResponseCloseState{Failed: true}) {
		t.Fatal("discarded body incorrectly reported joined")
	}
	if err := body.Close(); err == nil {
		t.Fatal("fake discarded body lost original Close failure")
	}
	if closes.Load() != 1 {
		t.Fatal("discarded body closed twice")
	}
}

func TestResponseCloseEvidenceNeverIncludesPrivateCloseText(t *testing.T) {
	c := loginFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	})
	var closes atomic.Int32
	private := "private-role-secret-token-path-close-detail"
	c.http.Transport = closeTransport{base: c.http.Transport, wrap: func(_ *http.Request, b io.ReadCloser) io.ReadCloser {
		return &countedCloseBody{ReadCloser: b, closes: &closes, err: errors.New(private)}
	}}
	_, obs := c.request(t.Context(), "read", http.MethodGet, "data", "item", "", "reader", nil)
	encoded, err := json.Marshal(struct {
		State       ResponseCloseState
		Observation Observation
	}{c.ResponseCloseState(), obs})
	if err != nil || strings.Contains(string(encoded), private) || strings.Contains(string(encoded), "responseCloseFailed") || strings.Contains(fmt.Sprintf("%+v", c.ResponseCloseState()), private) {
		t.Fatal("private Close material escaped fixed evidence")
	}
	if !obs.responseCloseFailed || obs.Failure != nil || !c.ResponseCloseState().Failed {
		t.Fatal("local Close evidence changed remote observation")
	}
}
