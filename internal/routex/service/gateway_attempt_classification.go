package service

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/routeattempt"
)

const gatewayRetryBodyLimit = 1 << 20

type gatewayAttemptClassification struct {
	Outcome      routeattempt.Outcome
	EvidenceCode string
}

func classifyGatewayHTTPFailure(protocol string, status int, body []byte) gatewayAttemptClassification {
	result := gatewayAttemptClassification{
		Outcome:      routeattempt.Outcome{Failure: routeattempt.PermanentFailure, Work: routeattempt.Unknown},
		EvidenceCode: "upstream_response",
	}
	if len(body) == 0 || len(body) > gatewayRetryBodyLimit {
		return result
	}

	switch protocol {
	case entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses:
		var envelope struct {
			Error struct {
				Code string `json:"code"`
				Type string `json:"type"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &envelope) != nil {
			return result
		}
		if status == http.StatusUnauthorized &&
			(envelope.Error.Code == "invalid_api_key" || envelope.Error.Type == "authentication_error") {
			return retryableGatewayRejection(routeattempt.CredentialRejected, "native_auth_rejection")
		}
		if status == http.StatusTooManyRequests &&
			(envelope.Error.Code == "rate_limit_exceeded" || envelope.Error.Type == "rate_limit_error") {
			return retryableGatewayRejection(routeattempt.RateLimited, "native_rate_rejection")
		}
	case entity.ProtocolAnthropicMessages:
		var envelope struct {
			Error struct {
				Type string `json:"type"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &envelope) != nil {
			return result
		}
		if status == http.StatusUnauthorized && envelope.Error.Type == "authentication_error" {
			return retryableGatewayRejection(routeattempt.CredentialRejected, "native_auth_rejection")
		}
		if status == http.StatusTooManyRequests && envelope.Error.Type == "rate_limit_error" {
			return retryableGatewayRejection(routeattempt.RateLimited, "native_rate_rejection")
		}
	case entity.ProtocolGeminiGenerateContent:
		var envelope struct {
			Error struct {
				Status string `json:"status"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &envelope) != nil {
			return result
		}
		if status == http.StatusUnauthorized && envelope.Error.Status == "UNAUTHENTICATED" {
			return retryableGatewayRejection(routeattempt.CredentialRejected, "native_auth_rejection")
		}
		if status == http.StatusTooManyRequests && envelope.Error.Status == "RESOURCE_EXHAUSTED" {
			return retryableGatewayRejection(routeattempt.RateLimited, "native_rate_rejection")
		}
	}
	return result
}

func retryableGatewayRejection(failure routeattempt.Failure, evidence string) gatewayAttemptClassification {
	return gatewayAttemptClassification{
		Outcome:      routeattempt.Outcome{Failure: failure, Work: routeattempt.RejectedWithoutWork},
		EvidenceCode: evidence,
	}
}

func readGatewayHTTPFailure(response *http.Response, protocol string) (gatewayAttemptClassification, error) {
	if response == nil || response.Body == nil {
		classification := gatewayAttemptClassification{
			Outcome:      routeattempt.Outcome{Failure: routeattempt.PermanentFailure, Work: routeattempt.Unknown},
			EvidenceCode: "transport_ambiguous",
		}
		return classification, gatewayError(http.StatusBadGateway, "upstream_error", "The upstream request failed.")
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, gatewayRetryBodyLimit+1))
	if readErr != nil || len(body) > gatewayRetryBodyLimit {
		body = nil
	}
	classification := classifyGatewayHTTPFailure(protocol, response.StatusCode, body)
	copy := *response
	copy.Body = io.NopCloser(bytes.NewReader(body))
	var public error
	switch protocol {
	case entity.ProtocolGeminiGenerateContent:
		public = nativeGeminiHTTPError(&copy)
	case entity.ProtocolAnthropicMessages:
		public = nativeMessagesHTTPError(&copy)
	case entity.ProtocolOpenAIResponses:
		public = nativeResponsesHTTPError(&copy)
	default:
		status, code := http.StatusBadGateway, "upstream_error"
		switch response.StatusCode {
		case http.StatusTooManyRequests:
			status, code = http.StatusTooManyRequests, "rate_limit_exceeded"
		case http.StatusRequestTimeout, http.StatusGatewayTimeout:
			status, code = http.StatusGatewayTimeout, "upstream_timeout"
		}
		public = gatewayError(status, code, "The upstream could not complete the request.")
	}
	return classification, public
}
