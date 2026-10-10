package oauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type countReader struct{ count int }

func (r *countReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	r.count += len(p)
	return len(p), nil
}

type failedReader struct{}

func (failedReader) Read([]byte) (int, error) { return 0, errors.New("private read failure") }

func TestBoundedBodiesCloseBeforeParseAndNeverDrainOverflow(t *testing.T) {
	for _, role := range []string{"token", "profile"} {
		t.Run(role, func(t *testing.T) {
			calls := 0
			reader := &countReader{}
			huge := &observedBody{reader: reader}
			config := testConfig(testTransport(func(*http.Request) (*http.Response, error) {
				calls++
				if role == "profile" && calls == 1 {
					return jsonReply(io.NopCloser(strings.NewReader(`{"access_token":"safe","token_type":"Bearer"}`)), 200), nil
				}
				return jsonReply(huge, 200), nil
			}))
			client, err := New(context.Background(), config)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Exchange(context.Background(), testCallback()); err != ErrUnavailable || !huge.closed {
				t.Fatalf("overflow/closure: %v", err)
			}
			bound := maxTokenBytes
			expectedCalls := 1
			if role == "profile" {
				bound = maxProfileBytes
				expectedCalls = 2
			}
			if reader.count != bound+1 || calls != expectedCalls {
				t.Fatalf("unbounded drain or additional request: bytes=%d calls=%d", reader.count, calls)
			}
		})
	}
	for _, failure := range []string{"read", "close", "media", "profile_redirect"} {
		t.Run(failure, func(t *testing.T) {
			calls := 0
			observed := &observedBody{reader: strings.NewReader(`{"account":{"id":"safe"}}`)}
			if failure == "read" {
				observed.reader = failedReader{}
			}
			if failure == "close" {
				observed.closeErr = errors.New("private close error")
			}
			config := testConfig(testTransport(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return jsonReply(io.NopCloser(strings.NewReader(`{"access_token":"safe","token_type":"Bearer"}`)), 200), nil
				}
				reply := jsonReply(observed, 200)
				if failure == "media" {
					reply.Header.Set("Content-Type", "text/html")
				}
				if failure == "profile_redirect" {
					reply.StatusCode = 307
					reply.Header.Set("Location", "https://other.example/secret")
				}
				return reply, nil
			}))
			client, err := New(context.Background(), config)
			if err != nil {
				t.Fatal(err)
			}
			expected := ErrUnavailable
			if failure == "media" || failure == "profile_redirect" {
				expected = ErrProtocol
			}
			if got, err := client.Exchange(context.Background(), testCallback()); err != expected || got != (Identity{}) || calls != 2 || !observed.closed {
				t.Fatalf("failure not closed/sanitized: %v calls=%d", err, calls)
			}
		})
	}
}
