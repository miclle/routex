package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/routeattempt"
)

func TestClassifyGatewayHTTPFailureRequiresNativeEvidence(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		status   int
		body     string
		failure  routeattempt.Failure
		work     routeattempt.WorkEvidence
	}{
		{"chat authentication", entity.ProtocolOpenAIChat, 401, `{"error":{"code":"invalid_api_key"}}`, routeattempt.CredentialRejected, routeattempt.RejectedWithoutWork},
		{"responses authentication", entity.ProtocolOpenAIResponses, 401, `{"error":{"type":"authentication_error"}}`, routeattempt.CredentialRejected, routeattempt.RejectedWithoutWork},
		{"messages authentication", entity.ProtocolAnthropicMessages, 401, `{"error":{"type":"authentication_error"}}`, routeattempt.CredentialRejected, routeattempt.RejectedWithoutWork},
		{"gemini authentication", entity.ProtocolGeminiGenerateContent, 401, `{"error":{"status":"UNAUTHENTICATED"}}`, routeattempt.CredentialRejected, routeattempt.RejectedWithoutWork},
		{"chat rate", entity.ProtocolOpenAIChat, 429, `{"error":{"type":"rate_limit_error"}}`, routeattempt.RateLimited, routeattempt.RejectedWithoutWork},
		{"responses rate", entity.ProtocolOpenAIResponses, 429, `{"error":{"code":"rate_limit_exceeded"}}`, routeattempt.RateLimited, routeattempt.RejectedWithoutWork},
		{"messages rate", entity.ProtocolAnthropicMessages, 429, `{"error":{"type":"rate_limit_error"}}`, routeattempt.RateLimited, routeattempt.RejectedWithoutWork},
		{"gemini rate", entity.ProtocolGeminiGenerateContent, 429, `{"error":{"status":"RESOURCE_EXHAUSTED"}}`, routeattempt.RateLimited, routeattempt.RejectedWithoutWork},
		{"bare unauthorized", entity.ProtocolOpenAIChat, 401, `{}`, routeattempt.PermanentFailure, routeattempt.Unknown},
		{"permission is not credential rejection", entity.ProtocolAnthropicMessages, 403, `{"error":{"type":"authentication_error"}}`, routeattempt.PermanentFailure, routeattempt.Unknown},
		{"bare rate limit", entity.ProtocolGeminiGenerateContent, 429, `{}`, routeattempt.PermanentFailure, routeattempt.Unknown},
		{"server failure", entity.ProtocolOpenAIResponses, 503, `{"error":{"type":"server_error"}}`, routeattempt.PermanentFailure, routeattempt.Unknown},
		{"malformed", entity.ProtocolOpenAIChat, 401, `{`, routeattempt.PermanentFailure, routeattempt.Unknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := classifyGatewayHTTPFailure(test.protocol, test.status, []byte(test.body))
			if got.Outcome.Failure != test.failure || got.Outcome.Work != test.work || got.Outcome.OutputStarted || got.Outcome.FinalUsageKnown {
				t.Fatalf("classification = %+v, want failure=%s work=%s", got, test.failure, test.work)
			}
		})
	}
}

func TestClassifyGatewayHTTPFailureRejectsOversizedEvidence(t *testing.T) {
	body := make([]byte, gatewayRetryBodyLimit+1)
	got := classifyGatewayHTTPFailure(entity.ProtocolOpenAIChat, http.StatusUnauthorized, body)
	if got.Outcome.Failure != routeattempt.PermanentFailure || got.Outcome.Work != routeattempt.Unknown {
		t.Fatalf("oversized response became retryable: %+v", got)
	}
}

func TestGatewayNativeRejectionContradictionsStayUnknown(t *testing.T) {
	for _, protocol := range []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		t.Run(protocol, func(t *testing.T) {
			var marker, discriminator string
			switch protocol {
			case entity.ProtocolOpenAIChat:
				marker, discriminator = "usage", `"code":"invalid_api_key"`
			case entity.ProtocolOpenAIResponses:
				marker, discriminator = "output", `"type":"authentication_error"`
			case entity.ProtocolAnthropicMessages:
				marker, discriminator = "content", `"type":"authentication_error"`
			default:
				marker, discriminator = "usageMetadata", `"status":"UNAUTHENTICATED"`
			}
			bodies := []string{
				fmt.Sprintf(`{"error":{%s},"%s":{"tokens":1}}`, discriminator, marker),
				fmt.Sprintf(`{"error":{%s},"%s":null}`, discriminator, marker),
				fmt.Sprintf(`{"error":{%s},"%s":[]}`, discriminator, marker),
				fmt.Sprintf(`{"error":{%s},"%s":{}}`, discriminator, marker),
				fmt.Sprintf(`{"error":{%s},"%s":0}`, discriminator, marker),
				fmt.Sprintf(`{"error":{%s},"%s":false}`, discriminator, marker),
				fmt.Sprintf(`{"error":{},"error":{%s}}`, discriminator),
				fmt.Sprintf(`{"error":{%s},"error":{%s}}`, discriminator, discriminator),
				fmt.Sprintf(`{"error":{},"\u0065rror":{%s}}`, discriminator),
				fmt.Sprintf(`{"error":{%s,%s}}`, discriminator, discriminator),
				fmt.Sprintf(`{"ERROR":{%s}}`, discriminator),
				fmt.Sprintf(`{"error":{%s},"message":"`+string([]byte{0xff})+`"}`, discriminator),
			}
			for index, body := range bodies {
				t.Run(fmt.Sprint(index), func(t *testing.T) {
					got := classifyGatewayHTTPFailure(protocol, 401, []byte(body))
					if got.Outcome.Failure != routeattempt.PermanentFailure || got.Outcome.Work != routeattempt.Unknown || got.Outcome.OutputStarted || got.Outcome.FinalUsageKnown {
						t.Fatalf("ambiguous evidence became replayable: %+v", got)
					}
				})
			}
		})
	}
}

func TestGatewayNativeRejectionBenignDiagnosticsRemainOpaque(t *testing.T) {
	for _, protocol := range []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		t.Run(protocol, func(t *testing.T) {
			field, value := "type", "authentication_error"
			if protocol == entity.ProtocolGeminiGenerateContent {
				field, value = "status", "UNAUTHENTICATED"
			}
			body := fmt.Sprintf(`{"error":{"%s":"%s","message":"usage output content candidates tokenCount null","details":[{"usage":{"total_tokens":5},"tool":{"output":"opaque"}}]},"request_id":"test-only","debug":{"content":"opaque","usage":1}}`, field, value)
			got := classifyGatewayHTTPFailure(protocol, 401, []byte(body))
			if got.Outcome.Failure != routeattempt.CredentialRejected || got.Outcome.Work != routeattempt.RejectedWithoutWork {
				t.Fatalf("opaque diagnostic changed native rejection: %+v", got)
			}
		})
	}
}

func TestGatewayNativeRejectionUnknownStopsPlannerWithoutAnotherAdmission(t *testing.T) {
	plan, err := routeattempt.New(entity.ProtocolOpenAIChat, []routeattempt.Target{{ID: "bnd_one", ConnectionID: "con_one", Protocol: entity.ProtocolOpenAIChat, Weight: 100, Credentials: []routeattempt.Credential{{ID: "crd_one"}, {ID: "crd_two", Priority: 1}}}}, routeattempt.Options{MaxAttempts: 4, Draw: func(int) (int, error) { return 0, nil }})
	if err != nil {
		t.Fatal(err)
	}
	admitted, executed := 0, 0
	result, err := plan.Run(context.Background(), routeattempt.Hooks{
		Eligible: func(context.Context, routeattempt.Attempt) (bool, error) { return true, nil },
		Prepare:  func(context.Context, routeattempt.Attempt) error { return nil },
		Admit:    func(context.Context, routeattempt.Attempt) error { admitted++; return nil },
		Execute: func(context.Context, routeattempt.Attempt) (routeattempt.Outcome, error) {
			executed++
			return classifyGatewayHTTPFailure(entity.ProtocolOpenAIChat, 401, []byte(`{"error":{"code":"invalid_api_key"},"usage":{"total_tokens":2}}`)).Outcome, nil
		},
	})
	if err != nil || result.Stop != routeattempt.UnsafeToReplay || admitted != 1 || executed != 1 || len(result.Attempts) != 1 {
		t.Fatalf("result=%+v err=%v admissions=%d executions=%d", result, err, admitted, executed)
	}
}

func TestGatewayNativeRejectionExactlyBoundedBenignBody(t *testing.T) {
	prefix, suffix := `{"error":{"code":"invalid_api_key","message":"`, `"}}`
	body := prefix + strings.Repeat("x", gatewayRetryBodyLimit-len(prefix)-len(suffix)) + suffix
	got := classifyGatewayHTTPFailure(entity.ProtocolOpenAIChat, 401, []byte(body))
	if got.Outcome.Work != routeattempt.RejectedWithoutWork {
		t.Fatalf("exact limit rejected: %+v", got)
	}
	if got := classifyGatewayHTTPFailure(entity.ProtocolOpenAIChat, 401, []byte(body+" ")); got.Outcome.Work != routeattempt.Unknown {
		t.Fatalf("over limit replayable: %+v", got)
	}
}

func TestGatewayNativeRejectionWorkFieldPresenceAcrossProtocols(t *testing.T) {
	tests := []struct {
		protocol   string
		fields     []string
		auth, rate string
	}{
		{entity.ProtocolOpenAIChat, []string{"choices", "usage", "id", "object", "created", "model"}, `"code":"invalid_api_key"`, `"code":"rate_limit_exceeded"`},
		{entity.ProtocolOpenAIResponses, []string{"output", "output_text", "usage", "status", "incomplete_details", "id", "object", "model", "created_at", "completed_at"}, `"type":"authentication_error"`, `"type":"rate_limit_error"`},
		{entity.ProtocolAnthropicMessages, []string{"content", "usage", "stop_reason", "stop_sequence", "id", "model", "role"}, `"type":"authentication_error"`, `"type":"rate_limit_error"`},
		{entity.ProtocolGeminiGenerateContent, []string{"candidates", "usageMetadata", "promptFeedback", "modelVersion", "responseId"}, `"status":"UNAUTHENTICATED"`, `"status":"RESOURCE_EXHAUSTED"`},
	}
	for _, test := range tests {
		t.Run(test.protocol, func(t *testing.T) {
			for _, field := range test.fields {
				t.Run(field, func(t *testing.T) {
					for _, status := range []int{401, 429} {
						evidence := test.auth
						if status == 429 {
							evidence = test.rate
						}
						for _, value := range []string{`null`, `[]`, `{}`, `0`, `false`, `""`, `{"native_work":true}`} {
							body := fmt.Sprintf(`{"error":{%s},%q:%s}`, evidence, field, value)
							got := classifyGatewayHTTPFailure(test.protocol, status, []byte(body))
							if got.Outcome.Work != routeattempt.Unknown || got.Outcome.Failure != routeattempt.PermanentFailure || got.Outcome.OutputStarted || got.Outcome.FinalUsageKnown {
								t.Fatalf("status=%d value=%s classification=%+v", status, value, got)
							}
						}
					}
				})
			}
		})
	}
}

func TestGatewayNativeRejectionEnvelopeShapeAndDiscriminatorAmbiguity(t *testing.T) {
	tests := []struct{ protocol, body string }{
		{entity.ProtocolOpenAIChat, `[]`},
		{entity.ProtocolOpenAIChat, `null`},
		{entity.ProtocolOpenAIChat, `{"error":null}`},
		{entity.ProtocolOpenAIChat, `{"error":[]}`},
		{entity.ProtocolOpenAIChat, `{"error":"invalid_api_key"}`},
		{entity.ProtocolOpenAIChat, `{"error":{"code":123,"type":"authentication_error"}}`},
		{entity.ProtocolOpenAIChat, `{"error":{"code":"invalid_api_key","type":"rate_limit_error"}}`},
		{entity.ProtocolOpenAIResponses, `{"error":{"code":"rate_limit_exceeded","type":"authentication_error"}}`},
		{entity.ProtocolOpenAIChat, `{"error":{"Code":"invalid_api_key"}}`},
		{entity.ProtocolOpenAIChat, `{"error":{"code":"invalid_api_key","Code":"server_error"}}`},
		{entity.ProtocolOpenAIChat, `{"error":{"code":"invalid_api_key","\u0063ode":"invalid_api_key"}}`},
		{entity.ProtocolOpenAIChat, `{"error":{"code":"invalid_api_key"},"Usage":null}`},
		{entity.ProtocolAnthropicMessages, `{"type":"message","error":{"type":"authentication_error"}}`},
		{entity.ProtocolAnthropicMessages, `{"type":null,"error":{"type":"authentication_error"}}`},
		{entity.ProtocolAnthropicMessages, `{"Type":"error","error":{"type":"authentication_error"}}`},
		{entity.ProtocolGeminiGenerateContent, `{"error":{"status":"UNAUTHENTICATED","status":"UNAUTHENTICATED"}}`},
		{entity.ProtocolGeminiGenerateContent, `{"error":{"Status":"UNAUTHENTICATED"}}`},
		{entity.ProtocolGeminiGenerateContent, `{"error":{"status":"UNAUTHENTICATED"}} {}`},
	}
	for index, test := range tests {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			got := classifyGatewayHTTPFailure(test.protocol, 401, []byte(test.body))
			if got.Outcome.Work != routeattempt.Unknown || got.Outcome.Failure != routeattempt.PermanentFailure {
				t.Fatalf("invalid evidence replayable: %+v", got)
			}
		})
	}
}

func TestGatewayNativeRejectionNullableOptionalDiagnosticsRemainSupported(t *testing.T) {
	tests := []struct{ protocol, body string }{
		{entity.ProtocolOpenAIChat, `{"error":{"type":"authentication_error","code":null,"param":null,"message":"test-only"}}`},
		{entity.ProtocolOpenAIResponses, `{"error":{"code":"invalid_api_key","type":"invalid_request_error","message":"test-only"}}`},
		{entity.ProtocolAnthropicMessages, `{"type":"error","error":{"type":"authentication_error","message":"test-only"},"request_id":"test-only"}`},
		{entity.ProtocolGeminiGenerateContent, `{"error":{"code":401,"status":"UNAUTHENTICATED","message":"test-only","details":[{"reason":"test-only"}]}}`},
	}
	for _, test := range tests {
		t.Run(test.protocol, func(t *testing.T) {
			got := classifyGatewayHTTPFailure(test.protocol, 401, []byte(test.body))
			if got.Outcome.Failure != routeattempt.CredentialRejected || got.Outcome.Work != routeattempt.RejectedWithoutWork {
				t.Fatalf("benign native envelope rejected: %+v", got)
			}
		})
	}
}

func TestGatewayNativeRejectionFailedBodyReadCannotAuthorizeReplay(t *testing.T) {
	for _, protocol := range []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		t.Run(protocol, func(t *testing.T) {
			// A complete-looking prefix is not complete evidence if the body read failed.
			body := `{"error":{"code":"invalid_api_key","type":"authentication_error","status":"UNAUTHENTICATED","message":"test-only-private"}}`
			response := &http.Response{StatusCode: 401, Body: io.NopCloser(io.MultiReader(strings.NewReader(body), iotest.ErrReader(io.ErrUnexpectedEOF)))}
			got, public := readGatewayHTTPFailure(response, protocol)
			if got.Outcome.Failure != routeattempt.PermanentFailure || got.Outcome.Work != routeattempt.Unknown || public == nil {
				t.Fatalf("partial evidence classification=%+v error=%v", got, public)
			}
			if strings.Contains(public.Error(), "test-only-private") {
				t.Fatal("private native error text escaped sanitization")
			}
		})
	}
}

func TestGatewayNativeRejectionGeminiCodeMustAgreeWithHTTPStatus(t *testing.T) {
	for _, status := range []int{401, 429} {
		kind := "UNAUTHENTICATED"
		if status == 429 {
			kind = "RESOURCE_EXHAUSTED"
		}
		for _, code := range []string{"200", "403", "500", "null", `"401"`, "{}", "[]", "true", "401.0", "4.01e2"} {
			t.Run(fmt.Sprintf("%d/%s", status, code), func(t *testing.T) {
				body := fmt.Sprintf(`{"error":{"status":%q,"code":%s}}`, kind, code)
				got := classifyGatewayHTTPFailure(entity.ProtocolGeminiGenerateContent, status, []byte(body))
				if got.Outcome.Work != routeattempt.Unknown || got.Outcome.Failure != routeattempt.PermanentFailure {
					t.Fatalf("contradictory code became replayable: %+v", got)
				}
			})
		}
		other := 429
		if status == 429 {
			other = 401
		}
		body := fmt.Sprintf(`{"error":{"status":%q,"code":%d}}`, kind, other)
		if got := classifyGatewayHTTPFailure(entity.ProtocolGeminiGenerateContent, status, []byte(body)); got.Outcome.Work != routeattempt.Unknown {
			t.Fatalf("opposing rejection replayable: %+v", got)
		}
		for _, suffix := range []string{"", fmt.Sprintf(`,"code":%d`, status)} {
			body := fmt.Sprintf(`{"error":{"status":%q%s}}`, kind, suffix)
			if got := classifyGatewayHTTPFailure(entity.ProtocolGeminiGenerateContent, status, []byte(body)); got.Outcome.Work != routeattempt.RejectedWithoutWork {
				t.Fatalf("canonical rejection lost: %+v", got)
			}
		}
	}
}

func TestGatewayNativeRejectionReservedDiscriminatorsCannotConflict(t *testing.T) {
	tests := []struct {
		protocol string
		status   int
		body     string
	}{
		{entity.ProtocolOpenAIChat, 401, `{"error":{"code":"invalid_api_key","status":"RESOURCE_EXHAUSTED"}}`},
		{entity.ProtocolOpenAIResponses, 429, `{"error":{"type":"rate_limit_error","status":"UNAUTHENTICATED"}}`},
		{entity.ProtocolOpenAIChat, 401, `{"error":{"type":"authentication_error","status":"OK"}}`},
		{entity.ProtocolOpenAIResponses, 401, `{"error":{"type":"authentication_error","status":200}}`},
		{entity.ProtocolOpenAIChat, 401, `{"error":{"type":"authentication_error","code":"server_error"}}`},
		{entity.ProtocolOpenAIChat, 401, `{"error":{"code":"invalid_api_key","type":"api_error"}}`},
		{entity.ProtocolAnthropicMessages, 401, `{"error":{"type":"authentication_error","code":"rate_limit_exceeded"}}`},
		{entity.ProtocolAnthropicMessages, 429, `{"error":{"type":"rate_limit_error","status":"UNAUTHENTICATED"}}`},
		{entity.ProtocolGeminiGenerateContent, 401, `{"error":{"status":"UNAUTHENTICATED","type":"rate_limit_error"}}`},
		{entity.ProtocolGeminiGenerateContent, 429, `{"error":{"status":"RESOURCE_EXHAUSTED","type":"authentication_error"}}`},
		{entity.ProtocolGeminiGenerateContent, 401, `{"error":{"status":"UNAUTHENTICATED","type":{}}}`},
	}
	for index, test := range tests {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			got := classifyGatewayHTTPFailure(test.protocol, test.status, []byte(test.body))
			if got.Outcome.Work != routeattempt.Unknown || got.Outcome.Failure != routeattempt.PermanentFailure {
				t.Fatalf("reserved discriminator contradiction replayable: %+v", got)
			}
		})
	}
}

func TestGatewayNativeRejectionResponsesEventEnvelopesStayUnknown(t *testing.T) {
	for _, status := range []int{401, 429} {
		code := "invalid_api_key"
		if status == 429 {
			code = "rate_limit_exceeded"
		}
		for _, kind := range []string{"response.completed", "response.in_progress", "response.error", "response.output_text.delta", "response.created", "Response.Completed"} {
			t.Run(fmt.Sprintf("%d/%s", status, kind), func(t *testing.T) {
				body := fmt.Sprintf(`{"error":{"code":%q},"type":%q,"delta":"Already emitted"}`, code, kind)
				got := classifyGatewayHTTPFailure(entity.ProtocolOpenAIResponses, status, []byte(body))
				if got.Outcome.Work != routeattempt.Unknown || got.Outcome.Failure != routeattempt.PermanentFailure || got.Outcome.OutputStarted || got.Outcome.FinalUsageKnown {
					t.Fatalf("native event envelope became replayable: %+v", got)
				}
			})
		}
		for _, typeField := range []string{"", `,"type":"provider_diagnostic"`, `,"type":null`, `,"type":"error"`} {
			for _, value := range []string{`null`, `{}`, `[]`, `""`, `0`, `false`, `{"id":"resp_recorded","status":"completed","output":[{"type":"message"}]}`} {
				t.Run(fmt.Sprintf("%d/response%s/%s", status, typeField, value), func(t *testing.T) {
					body := fmt.Sprintf(`{"error":{"code":%q}%s,"response":%s}`, code, typeField, value)
					got := classifyGatewayHTTPFailure(entity.ProtocolOpenAIResponses, status, []byte(body))
					if got.Outcome.Work != routeattempt.Unknown || got.Outcome.Failure != routeattempt.PermanentFailure || got.Outcome.OutputStarted || got.Outcome.FinalUsageKnown {
						t.Fatalf("native response payload became replayable: %+v", got)
					}
				})
			}
		}
	}
}

func TestGatewayNativeRejectionResponsesEventAliasesAndOpaqueDiagnostics(t *testing.T) {
	for _, fields := range []string{
		`"\u0072esponse":null`,
		`"Response":null`,
		`"Type":"response.completed"`,
		`"type":"error","Type":"response.completed"`,
		`"type":"error","\u0074ype":"error"`,
		`"response":null,"\u0072esponse":null`,
		`"type":"\u0072esponse.completed"`,
		`"type":{}`,
	} {
		t.Run(fields, func(t *testing.T) {
			body := `{"error":{"code":"invalid_api_key"},` + fields + `}`
			got := classifyGatewayHTTPFailure(entity.ProtocolOpenAIResponses, 401, []byte(body))
			if got.Outcome.Work != routeattempt.Unknown || got.Outcome.Failure != routeattempt.PermanentFailure {
				t.Fatalf("ambiguous native event envelope became replayable: %+v", got)
			}
		})
	}
	for _, typeField := range []string{"", `,"type":"error"`, `,"type":"provider_diagnostic"`, `,"type":null`, `,"type":""`} {
		t.Run("benign"+typeField, func(t *testing.T) {
			body := `{"error":{"code":"invalid_api_key","message":"response.completed output response", "details":{"type":"response.completed","response":{"output":["opaque"]}}}` + typeField + `,"debug":{"response":null,"type":"response.output_text.delta"}}`
			got := classifyGatewayHTTPFailure(entity.ProtocolOpenAIResponses, 401, []byte(body))
			if got.Outcome.Work != routeattempt.RejectedWithoutWork || got.Outcome.Failure != routeattempt.CredentialRejected {
				t.Fatalf("opaque diagnostic or generic error typing lost native rejection: %+v", got)
			}
		})
	}
}
