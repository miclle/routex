package handler

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
)

const geminiFinalUsage = `{"promptTokenCount":10,"cachedContentTokenCount":4,"candidatesTokenCount":3,"thoughtsTokenCount":2,"totalTokenCount":15}`

func geminiFixture() string {
	return `{"candidates":[{"index":0,"content":{"parts":[{"text":"hello","thoughtSignature":"signed-opaque"}]},"finishReason":"STOP"}],"modelVersion":"private-model","responseId":"native-response","usageMetadata":` + geminiFinalUsage + `}`
}
func TestGeminiNativeSSEFinality(t *testing.T) {
	body := ": keepalive\n\nid: 7\ndata: " + geminiFixture() + "\n\n"
	recorder := httptest.NewRecorder()
	usage, err := proxyGeminiStream(context.Background(), recorder, strings.NewReader(body), "public", "req_gemini", 1)
	if err != nil || !usage.Complete || usage.Output == nil || *usage.Output != 5 || strings.Contains(recorder.Body.String(), "private-model") || !strings.Contains(recorder.Body.String(), "signed-opaque") || !strings.Contains(recorder.Body.String(), "id: 7") {
		t.Fatalf("native SSE lost %+v %v", usage, err)
	}
	// Usage frames are whole snapshots, including those arriving after finishReason.
	late := body + `data: {"usageMetadata":{"promptTokenCount":10,"cachedContentTokenCount":4,"candidatesTokenCount":8,"thoughtsTokenCount":2,"totalTokenCount":20}}` + "\n\n"
	usage, err = proxyGeminiStream(context.Background(), httptest.NewRecorder(), strings.NewReader(late), "public", "req", 1)
	if err != nil || !usage.Complete || usage.Output == nil || *usage.Output != 10 {
		t.Fatal("late aggregate not retained", err)
	}
	malformed := body + `data: {"usageMetadata":{"promptTokenCount":10}}` + "\n\n"
	usage, err = proxyGeminiStream(context.Background(), httptest.NewRecorder(), strings.NewReader(malformed), "public", "req", 1)
	if err != nil || !usage.Complete || usage.Output != nil || usage.CacheRead != nil {
		t.Fatal("partial aggregate merged prior counters")
	}
	for _, native := range []string{body + `data: {"usageMetadata":{"promptTokenCount":10,"cachedContentTokenCount":4,"candidatesTokenCount":1,"thoughtsTokenCount":2,"totalTokenCount":13}}` + "\n\n", body + body, strings.Replace(body, `,"finishReason":"STOP"`, "", 1), `data: {"candidates":[{"index":2,"finishReason":"STOP"}]}` + "\n\n", `data: [DONE]` + "\n\n", `data: {"error":{"status":"INTERNAL","message":"private-secret"}}` + "\n\n", "data: " + strings.Repeat("x", gatewayEventLimit) + "\n\n"} {
		recorder := httptest.NewRecorder()
		usage, err := proxyGeminiStream(context.Background(), recorder, strings.NewReader(native), "public", "req", 1)
		if err == nil || usage.Complete || strings.Contains(recorder.Body.String(), "private-secret") {
			t.Fatal("invalid stream accepted", err)
		}
	}
	// Cancellation even after candidate+usage is not authoritative EOF.
	ctx, cancel := context.WithCancel(context.Background())
	writer := &cancelResponseWriter{httptest.NewRecorder(), cancel, "STOP"}
	usage, err = proxyGeminiStream(ctx, writer, strings.NewReader(body), "public", "req", 1)
	cancel()
	if !errors.Is(err, context.Canceled) || usage.Complete {
		t.Fatal("cancellation fabricated finality")
	}
	usage, err = proxyGeminiStream(context.Background(), httptest.NewRecorder(), io.MultiReader(strings.NewReader(body), geminiErrorReader{}), "public", "req", 1)
	if err == nil || usage.Complete {
		t.Fatal("transport failure fabricated finality")
	}
	multi := `data: {"candidates":[{"index":0,"finishReason":"STOP"}]}` + "\n\n" + `data: {"candidates":[{"index":1,"finishReason":"MAX_TOKENS"}],"usageMetadata":` + geminiFinalUsage + `}` + "\n\n"
	usage, err = proxyGeminiStream(context.Background(), httptest.NewRecorder(), strings.NewReader(multi), "public", "req", 2)
	if err != nil || !usage.Complete {
		t.Fatal("multi-candidate finality", err)
	}
	blocked := `data: {"promptFeedback":{"blockReason":"SAFETY"},"usageMetadata":{"promptTokenCount":2}}` + "\n\n"
	usage, err = proxyGeminiStream(context.Background(), httptest.NewRecorder(), strings.NewReader(blocked), "public", "req", 1)
	if err != nil || !usage.Complete || usage.Output != nil {
		t.Fatal("safety response invented zero output")
	}
}
func TestGeminiCredentialsAmbiguityAndRedaction(t *testing.T) {
	for _, test := range []struct {
		query, native, auth string
		valid               bool
	}{{"key=secret", "", "", true}, {"alt=sse", "secret", "", true}, {"", "", "Bearer secret", true}, {"key=secret&key=secret", "", "", false}, {"key=secret", "secret", "", false}, {"key=secret&bad=%zz", "", "", false}, {"key=secret&unknown=yes", "", "", false}, {"alt=sse&alt=sse", "secret", "", false}} {
		request := httptest.NewRequest("POST", "/v1beta/unknown", nil)
		request.URL.RawQuery = test.query
		request.RequestURI += "?" + test.query
		if test.native != "" {
			request.Header.Set("x-goog-api-key", test.native)
		}
		if test.auth != "" {
			request.Header.Set("Authorization", test.auth)
		}
		bearer, err := geminiRequestCredentials(request, true)
		if (err == nil) != test.valid || test.valid && bearer != "secret" {
			t.Fatal("wrong auth parsing", test, err)
		}
		if request.URL.RawQuery != "" || strings.Contains(request.RequestURI, "secret") || request.Header.Get("x-goog-api-key") != "" || request.Header.Get("Authorization") != "" {
			t.Fatal("credentials remain in request")
		}
	}
	request := httptest.NewRequest("POST", "/v1beta/unknown", nil)
	request.Header.Add("x-goog-api-key", "secret")
	request.Header.Add("x-goog-api-key", "other")
	if _, err := geminiRequestCredentials(request, true); err == nil {
		t.Fatal("duplicate header accepted")
	}
}
func TestGeminiDefaultLoggerAndRecoveryRedaction(t *testing.T) {
	if os.Getenv("ROUTEX_GEMINI_LOG_CHILD") == "1" {
		engine := fox.Default()
		engine.RedirectTrailingSlash = false
		engine.RedirectFixedPath = false
		engine.Engine.Use(GeminiQueryCredentials)
		engine.POST("/v1beta/panic", func(c *fox.Context) { panic("fixed test panic") })
		for _, path := range []string{"/v1beta/missing", "/v1beta/panic/", "/v1beta/PANIC", "/v1beta/panic"} {
			request := httptest.NewRequest("POST", path+"?key=query-credential-secret&key=duplicate-credential-secret&bad=%zz", nil)
			request.Header.Set("x-goog-api-key", "header-credential-secret")
			request.Header.Set("Authorization", "Bearer bearer-credential-secret")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if response.Header().Get("Location") != "" || strings.Contains(response.Body.String(), "credential-secret") {
				t.Fatal("credential reflected")
			}
			if path != "/v1beta/panic" && response.Code != 404 {
				t.Fatal("nonexact native path did not return 404")
			}
			if path == "/v1beta/panic" && response.Code != 500 {
				t.Fatal("panic not recovered")
			}
		}
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestGeminiDefaultLoggerAndRecoveryRedaction$", "-test.v")
	command.Env = append(os.Environ(), "ROUTEX_GEMINI_LOG_CHILD=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("default engine child failed: %v %s", err, output)
	}
	for _, secret := range []string{"query-credential-secret", "duplicate-credential-secret", "header-credential-secret", "bearer-credential-secret"} {
		if strings.Contains(string(output), secret) {
			t.Fatal("default logger/recovery leaked native credentials")
		}
	}
	if !strings.Contains(string(output), "/v1beta/missing") || !strings.Contains(string(output), "panic recovered") {
		t.Fatalf("missing actual logger/recovery evidence: %s", output)
	}
}
func FuzzGeminiNativeFrames(f *testing.F) {
	f.Add(geminiFixture())
	f.Add(`{"error":{"code":500}}`)
	f.Add(`{"candidates":[{"index":0,"finishReason":"STOP"}]}`)
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > gatewayEventLimit {
			return
		}
		state := geminiStreamState{expected: 1}
		_, _ = state.object([]byte(raw), "public", "req")
	})
}

type geminiErrorReader struct{}

func (geminiErrorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
