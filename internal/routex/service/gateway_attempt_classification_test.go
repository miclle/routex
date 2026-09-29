package service

import (
	"net/http"
	"testing"

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
