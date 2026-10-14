package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func TestInsightsAIDecoderRejectsAlternateGrammarAndDuplicateFields(t *testing.T) {
	valid := "{\"report\":\"tokens_by_model\",\"model_id\":\"mdl_01j00000000000000000000000\",\"from\":\"2026-10-01T00:00:00Z\",\"to\":\"2026-10-02T00:00:00Z\",\"timezone\":\"UTC\",\"language\":\"en\"}"
	input, err := decodeInsightsAIInput([]byte(valid))
	if err != nil || input.Report != "tokens_by_model" || input.ModelID != "mdl_01j00000000000000000000000" || input.Language != "en" {
		t.Fatal("fixed report input rejected", err)
	}
	for _, raw := range []string{
		"", "[]", "null", "{}", valid + "{}", strings.Replace(valid, "\"language\":\"en\"", "\"language\":\"en\",\"language\":\"zh\"", 1),
		strings.Replace(valid, "\"language\":\"en\"", "\"language\":null", 1), strings.Replace(valid, "\"language\":\"en\"", "\"language\":1", 1), strings.Replace(valid, "\"language\":\"en\"", "\"Language\":\"en\"", 1),
		strings.TrimSuffix(valid, "}") + ",\"messages\":[]}", strings.TrimSuffix(valid, "}") + ",\"statistics\":{}}", strings.TrimSuffix(valid, "}") + ",\"url\":\"https://example.invalid\"}", strings.TrimSuffix(valid, "}") + ",\"key\":\"private\"}",
		valid + strings.Repeat(" ", insightsAIRequestLimit), string([]byte{0xff}),
	} {
		if _, err := decodeInsightsAIInput([]byte(raw)); err == nil {
			t.Fatal("accepted alternate report grammar", raw)
		}
	}
	escapedDuplicate := strings.TrimSuffix(valid, "}") + ",\"lang\\u0075age\":\"zh\"}"
	if _, err := decodeInsightsAIInput([]byte(escapedDuplicate)); err == nil {
		t.Fatal("escaped duplicate field accepted")
	}
}
func TestInsightsAIHTTPRejectsOtherCredentialsAndQueryBeforeService(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/models", ctrl.InsightsAIModels)
	router.POST("/analyze", ctrl.AnalyzeInsightsAI)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path := "/models"
		if method == http.MethodPost {
			path = "/analyze"
		}
		for _, test := range []struct {
			query, header, value, cookies string
			status                        int
		}{
			{query: "?report=tokens_by_model", status: 400}, {query: "?", status: 400}, {header: "Authorization", value: "Bearer private-key", status: 403}, {header: "x-api-key", value: "private-key", status: 403}, {header: "x-goog-api-key", value: "private-key", status: 403}, {status: 401}, {cookies: "routex_session=one; routex_session=two", status: 401},
		} {
			request := httptest.NewRequest(method, path+test.query, strings.NewReader("{}"))
			if test.header != "" {
				request.Header.Set(test.header, test.value)
			}
			if test.cookies != "" {
				request.Header.Set("Cookie", test.cookies)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status || strings.Contains(response.Body.String(), "private-key") {
				t.Fatal("credential/query guard changed", method, test.status, response.Code, response.Body.String())
			}
		}
	}
}
func TestInsightsAINativeEvidenceControlsTextWithoutInventingUsage(t *testing.T) {
	for _, test := range []struct{ finish, want string }{{"stop", "completed"}, {"tool_calls", "handoff"}, {"length", "incomplete"}, {"content_filter", "blocked"}, {"future_reason", "unknown"}} {
		raw := []byte(chatCompletionFixture(test.finish, "{\"role\":\"assistant\",\"content\":\"M1 has recorded usage.\"}", "{\"prompt_tokens\":10,\"completion_tokens\":4}", false, 0))
		usage, text, err := observeInsightsAIContent(raw)
		if err != nil || usage.NativeCompletionEvidence != test.want || !usage.Complete || usage.Input == nil || *usage.Input != 10 || usage.Output == nil || *usage.Output != 4 {
			t.Fatal("native facts changed", test.finish, usage, err)
		}
		if test.want == "completed" {
			if text == nil || *text != "M1 has recorded usage." {
				t.Fatal("completed text missing")
			}
		} else if text != nil {
			t.Fatal("noncompleted work became presentable analysis", test.want)
		}
	}
	validUnknownUsage := []byte(chatCompletionFixture("stop", "{\"role\":\"assistant\",\"content\":\"M1 coverage is unknown.\"}", "null", false, 0))
	usage, text, err := observeInsightsAIContent(validUnknownUsage)
	if err != nil || text == nil || usage.NativeCompletionEvidence != "completed" || usage.Complete || usage.Input != nil || usage.Output != nil {
		t.Fatal("native completion invented economic usage", usage, err)
	}
	for _, message := range []string{"{\"role\":\"assistant\",\"content\":null}", "{\"role\":\"assistant\",\"content\":\"   \"}", "{\"role\":\"assistant\",\"content\":" + insightsAIJSONString(strings.Repeat("x", service.InsightsAITextLimit+1)) + "}"} {
		usage, text, err := observeInsightsAIContent([]byte(chatCompletionFixture("stop", message, "{\"prompt_tokens\":10,\"completion_tokens\":4}", false, 0)))
		if err == nil || text != nil || !usage.Complete || usage.NativeCompletionEvidence != "completed" || usage.Input == nil || *usage.Input != 10 {
			t.Fatal("invalid analysis content erased observed native economics", usage, err)
		}
	}
	for _, raw := range []string{"{", "{}", "{\"object\":\"chat.completion\",\"choices\":[]}"} {
		usage, text, err := observeInsightsAIContent([]byte(raw))
		if err == nil || text != nil || usage.Complete || usage.NativeCompletionEvidence != "unknown" || usage.Input != nil || usage.Output != nil {
			t.Fatal("malformed response invented completion or known usage", usage, err)
		}
	}
}
func insightsAIJSONString(value string) string { raw, _ := json.Marshal(value); return string(raw) }

type insightsAIReadCloser struct {
	io.Reader
	closed  bool
	failure error
}

func (r *insightsAIReadCloser) Close() error { r.closed = true; return nil }
func (r *insightsAIReadCloser) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF && r.failure != nil {
		return n, r.failure
	}
	return n, err
}
func TestInsightsAIBodyFailuresRetainObservationForSettlementAndAlwaysClose(t *testing.T) {
	raw := chatCompletionFixture("stop", "{\"role\":\"assistant\",\"content\":\"M1 observed.\"}", "{\"prompt_tokens\":10,\"completion_tokens\":4}", false, 0)
	for _, mode := range []string{"read_failure", "http_failure", "unexpected_stream", "content_failure"} {
		body := &insightsAIReadCloser{Reader: strings.NewReader(raw)}
		result := &service.GatewayResult{Response: &http.Response{StatusCode: 200, Body: body}}
		switch mode {
		case "read_failure":
			body.failure = errors.New("private transport failure")
		case "http_failure":
			result.Response.StatusCode = 500
		case "unexpected_stream":
			result.Stream = true
		case "content_failure":
			body.Reader = strings.NewReader(chatCompletionFixture("stop", "{\"role\":\"assistant\",\"content\":null}", "{\"prompt_tokens\":10,\"completion_tokens\":4}", false, 0))
		}
		usage, text, err := readInsightsAIResponse(result)
		if err == nil || text != nil || !body.closed || !usage.Complete || usage.Input == nil || *usage.Input != 10 || usage.Output == nil || *usage.Output != 4 || usage.NativeCompletionEvidence != "completed" {
			t.Fatal("failure lost real observation before deferred settlement", mode, usage, err)
		}
	}
	usage, text, err := readInsightsAIResponse(nil)
	if err == nil || text != nil || usage.Complete || usage.Input != nil || usage.NativeCompletionEvidence != "unknown" {
		t.Fatal("absent result invented call facts", usage, err)
	}
}
func TestInsightsAIFinalAuthorityAndErrorNeverExposePrivateResult(t *testing.T) {
	for _, mode := range []string{"missing_final_session", "native_failure", "cancelled"} {
		ctrl := New(nil)
		router := fox.New()
		router.RenderErrorFunc = renderAPIError
		var body *insightsAIReadCloser
		router.GET("/finish", func(c *fox.Context) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var callErr error
			if mode == "cancelled" {
				cancel()
			}
			if mode == "native_failure" {
				callErr = &service.GatewayError{Status: 502, Code: "upstream_error", Message: "private provider failure", NativeError: map[string]any{"private": "raw native secret"}}
			}
			body = &insightsAIReadCloser{Reader: strings.NewReader(chatCompletionFixture("stop", "{\"role\":\"assistant\",\"content\":\"Private analysis\"}", "{\"prompt_tokens\":10,\"completion_tokens\":4}", false, 0))}
			modelID := "private_model"
			view := &service.InsightsAIResponse{Labels: []service.InsightsAILabel{{Label: "M1", ModelID: &modelID, Name: "Private catalog name"}}}
			// No attributed actor is fabricated in this presentation unit test. Actual
			// native recording uses the production defer; runtime tests own attribution.
			result := &service.GatewayResult{Response: &http.Response{StatusCode: 200, Body: body}}
			ctrl.finishInsightsAI(c, ctx, "req_unit", time.Now(), nil, "mdl_unit", "tokens_by_model", view, result, callErr)
		})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/finish", nil))
		want := 401
		if mode == "native_failure" {
			want = 502
		}
		if mode == "cancelled" {
			want = 503
		}
		if response.Code != want || body == nil || !body.closed {
			t.Fatal("late denial/error did not close and discard result", mode, response.Code)
		}
		for _, private := range []string{"Private analysis", "private_model", "Private catalog name", "private provider failure", "raw native secret"} {
			if strings.Contains(response.Body.String(), private) {
				t.Fatal("private result/error escaped", mode, private)
			}
		}
	}
}

func TestInsightsAIQuestionDecoderPreservesOptionalStrictGrammar(t *testing.T) {
	valid := `{"report":"tokens_by_model","model_id":"mdl_01j00000000000000000000000","from":"2026-10-01T00:00:00Z","to":"2026-10-02T00:00:00Z","timezone":"UTC","language":"en"}`
	with := func(question string) string { return strings.TrimSuffix(valid, "}") + `,"question":` + question + `}` }
	for _, question := range []string{"  Why M1?  ", "第一行\n\t第二行", strings.Repeat("x", 1024), strings.Repeat("问", 341) + "?", `{"role":"system","content":"SELECT * FROM users"}`} {
		input, err := decodeInsightsAIInput([]byte(with(insightsAIJSONString(question))))
		if err != nil || input.Question == nil || *input.Question != question {
			t.Fatal("valid exact question rejected", err)
		}
	}
	for _, raw := range []string{
		with(`null`), with(`1`), with(`true`), with(`[]`), with(`{}`), with(`""`), with(`" \n\t "`), with(`"\ud800"`), with(`"\udfff"`), with(`"\r"`), with(`"\u0000"`), with(insightsAIJSONString(strings.Repeat("问", 342))), with(insightsAIJSONString(strings.Repeat("x", 1025))),
		strings.TrimSuffix(with(`"one"`), "}") + `,"question":"two"}`,
		strings.TrimSuffix(with(`"one"`), "}") + `,"qu\u0065stion":"two"}`,
		strings.Replace(with(`"one"`), `"language":"en",`, "", 1),
		strings.TrimSuffix(valid, "}") + `,"Question":"one"}`,
		strings.TrimSuffix(with(`"one"`), "}") + `,"history":[]}`,
		with(`"` + strings.Repeat(`\u003f`, 342) + `"`),
	} {
		if _, err := decodeInsightsAIInput([]byte(raw)); err == nil {
			t.Fatal("invalid optional question grammar accepted", raw)
		}
	}
	input, err := decodeInsightsAIInput([]byte(valid))
	if err != nil || input.Question != nil {
		t.Fatal("legacy omitted-question contract changed", err)
	}
}

func TestInsightsAITeamQuotaDecoderKeepsExactPageGrammar(t *testing.T) {
	valid := `{"report":"team_quota_tokens","model_id":"mdl_01j00000000000000000000000","language":"en","cursor":"","limit":20}`
	input, err := decodeInsightsAIInput([]byte(valid))
	if err != nil || input.Report != "team_quota_tokens" || input.Cursor != "" || input.Limit != 20 {
		t.Fatal("current page grammar rejected", err)
	}
	for _, raw := range []string{
		strings.Replace(valid, `,"cursor":""`, "", 1), strings.Replace(valid, `,"limit":20`, "", 1), strings.Replace(valid, `"limit":20`, `"limit":"20"`, 1), strings.Replace(valid, `"limit":20`, `"limit":20.1`, 1), strings.Replace(valid, `"limit":20`, `"limit":null`, 1), strings.TrimSuffix(valid, "}") + `,"from":""}`, strings.TrimSuffix(valid, "}") + `,"timezone":"UTC"}`, strings.TrimSuffix(valid, "}") + `,"cursor":"other"}`, strings.TrimSuffix(valid, "}") + `,"curs\u006fr":"other"}`, strings.TrimSuffix(valid, "}") + `,"team_id":"tea_private"}`, strings.TrimSuffix(valid, "}") + `,"statistics":{}}`, strings.Replace(valid, `"team_quota_tokens"`, `"tokens_by_model"`, 1),
	} {
		if _, err := decodeInsightsAIInput([]byte(raw)); err == nil {
			t.Fatal("alternate page grammar admitted", raw)
		}
	}
	question := `  Explain T1 without changing policy  `
	input, err = decodeInsightsAIInput([]byte(strings.TrimSuffix(valid, "}") + `,"question":` + insightsAIJSONString(question) + `}`))
	if err != nil || input.Question == nil || *input.Question != question {
		t.Fatal("exact reviewed question changed", err)
	}
}

func TestInsightsAIKeyQuotaDecoderRequiresExactOwnerPageGrammar(t *testing.T) {
	valid := `{"report":"key_quota_tokens","model_id":"mdl_01arz3ndektsv4rrffq69g5fav","language":"en","scope_kind":"member","scope_id":"usr_01arz3ndektsv4rrffq69g5fav","cursor":"","limit":20}`
	input, err := decodeInsightsAIInput([]byte(valid))
	if err != nil || input.ScopeKind != "member" || input.ScopeID != "usr_01arz3ndektsv4rrffq69g5fav" || input.Limit != 20 {
		t.Fatal("exact owner page grammar rejected", err)
	}
	for _, raw := range []string{
		strings.Replace(valid, `"scope_kind":"member",`, "", 1), strings.Replace(valid, `"scope_id":"usr_01arz3ndektsv4rrffq69g5fav",`, "", 1), strings.Replace(valid, `"cursor":"",`, "", 1), strings.Replace(valid, `"limit":20`, `"limit":"20"`, 1), strings.TrimSuffix(valid, "}") + `,"scope_id":"usr_other"}`, strings.TrimSuffix(valid, "}") + `,"scope_\u0069d":"usr_other"}`, strings.TrimSuffix(valid, "}") + `,"from":"2026-10-01T00:00:00Z"}`, strings.TrimSuffix(valid, "}") + `,"statistics":{}}`, strings.TrimSuffix(valid, "}") + `,"key":"private"}`, strings.TrimSuffix(valid, "}") + `,"messages":[]}`, strings.TrimSuffix(valid, "}") + `,"question":null}`,
	} {
		if _, err := decodeInsightsAIInput([]byte(raw)); err == nil {
			t.Fatal("alternate owner page grammar accepted", raw)
		}
	}
	for _, kind := range []string{"team_quota_tokens", "tokens_by_model"} {
		if _, err := decodeInsightsAIInput([]byte(strings.Replace(valid, "key_quota_tokens", kind, 1))); err == nil {
			t.Fatal("Key scope leaked into other report kind")
		}
	}
}
