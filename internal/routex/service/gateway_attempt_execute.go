package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/routeattempt"
	"github.com/miclle/routex/pkg/upstream"
)

type preparedGatewayAttempt struct {
	route      *gatewayRoute
	credential string
	request    *http.Request
	client     *http.Client
}

func (s *Service) gatewayNativeAttempts(ctx context.Context, requestID string, result *GatewayResult, payload map[string]json.RawMessage, attachmentPlan *gatewayAttachmentPlan, options ...gatewayNativeOptions) (*GatewayResult, error) {
	result.quotaRequest = inspectQuotaRequest(result.NativeProtocol(), payload, attachmentPlan)
	if result.NativeProtocol() == entity.ProtocolAnthropicMessages && (len(options) != 1 || options[0].Messages.Version != "2023-06-01" || options[0].Messages.Beta != "") {
		result.quotaRequest.Supported = false
	}
	pricingPayload := payload
	if len(attachmentPlan.Occurrences) != 0 {
		if detached, ok := quotaAttachmentPayload(result.NativeProtocol(), attachmentPlan); ok {
			pricingPayload = detached
		}
	}
	result.PricingDimensions = gatewayPricingDimensions(result.NativeProtocol(), pricingPayload)
	result.PricingUnsupported = len(result.PricingDimensions) != 0

	plan, err := s.gatewayAttemptPlan(result.ModelID, result.NativeProtocol())
	if err != nil {
		return result, gatewayPublicAttemptError(err)
	}
	plan.userID, plan.projectID, plan.keyID = result.UserID, result.ProjectID, result.KeyID
	plan.team = result.identity.team
	plan, err = gatewayAttachmentAttemptPlan(plan, attachmentPlan)
	if err != nil {
		return result, err
	}
	plan, err = s.prepareGatewayAttemptQuotaState(ctx, plan, result)
	if err != nil {
		return result, gatewayPublicAttemptError(err)
	}
	if len(attachmentPlan.Occurrences) != 0 {
		if err := s.preflightPreparedGatewayAttempt(requestID, result); err != nil {
			return result, err
		}
		owner := attachmentOwner{Kind: entity.StorageOwnerUser, ID: result.UserID}
		if result.ProjectID != "" {
			owner = attachmentOwner{Kind: entity.StorageOwnerProject, ID: result.ProjectID}
		}
		resolved, resolveErr := s.resolveGatewayAttachments(ctx, owner, attachmentPlan)
		if resolveErr != nil {
			return result, resolveErr
		}
		payload, err = attachmentPlan.Rewrite(resolved)
		if err != nil {
			return result, err
		}
	}

	var current *preparedGatewayAttempt
	var hookErr error
	var lastPublic error
	run, runErr := plan.Plan.Run(ctx, routeattempt.Hooks{
		Eligible: func(ctx context.Context, attempt routeattempt.Attempt) (bool, error) {
			return s.gatewayAttemptEligible(ctx, plan, attempt)
		},
		Prepare: func(ctx context.Context, attempt routeattempt.Attempt) error {
			eligible, eligibleErr := s.gatewayAttemptEligible(ctx, plan, attempt)
			if eligibleErr != nil || !eligible {
				if eligibleErr == nil {
					eligibleErr = runtimeUnavailable
				}
				hookErr = eligibleErr
				return eligibleErr
			}
			route, credential, ok := plan.Candidate(attempt)
			if !ok {
				hookErr = runtimeUnavailable
				return hookErr
			}
			if attempt.Number > 1 {
				if prepareErr := s.validateGatewayRetryEvidence(ctx, plan, result, attempt); prepareErr != nil {
					hookErr = prepareErr
					return prepareErr
				}
			}
			prepared, prepareErr := prepareGatewayHTTPRequest(ctx, requestID, result, payload, route, credential, s.allowPrivateUpstream, options...)
			if prepareErr != nil {
				hookErr = prepareErr
				return prepareErr
			}
			current = prepared
			return nil
		},
		Admit: func(ctx context.Context, attempt routeattempt.Attempt) error {
			admitErr := s.admitPreparedGatewayAttempt(ctx, requestID, plan, result, attempt)
			if admitErr != nil {
				hookErr = admitErr
			} else {
				result.Admitted = true
				if s.afterGatewayAdmission != nil {
					s.afterGatewayAdmission()
				}
			}
			return admitErr
		},
		Execute: func(ctx context.Context, attempt routeattempt.Attempt) (routeattempt.Outcome, error) {
			if current == nil {
				return routeattempt.Outcome{Failure: routeattempt.PermanentFailure, Work: routeattempt.Unknown}, routeattempt.ErrExecution
			}
			outcome, public, executeErr := s.executeGatewayAttempt(ctx, requestID, result, attempt, current)
			lastPublic = public
			current = nil
			return outcome, executeErr
		},
	})
	result.Admitted = result.Admitted || run.Admitted
	result.RouteStopReason = string(run.Stop)
	if runErr != nil {
		if hookErr != nil {
			return result, gatewayPublicAttemptError(hookErr)
		}
		if ctx.Err() != nil {
			return result, gatewayContextError(ctx.Err())
		}
		if lastPublic != nil {
			return result, lastPublic
		}
		return result, gatewayError(http.StatusBadGateway, "upstream_error", "The upstream request failed.")
	}
	if run.Stop == routeattempt.Succeeded && result.Response != nil {
		return result, nil
	}
	if lastPublic != nil {
		return result, lastPublic
	}
	return result, gatewayError(http.StatusServiceUnavailable, "upstream_unavailable", "No usable upstream is available.")
}

func gatewayPricingDimensions(protocol string, payload map[string]json.RawMessage) []string {
	switch protocol {
	case entity.ProtocolOpenAIResponses:
		return responsesPricingDimensions(payload)
	case entity.ProtocolAnthropicMessages:
		return messagesPricingDimensions(payload)
	case entity.ProtocolGeminiGenerateContent:
		return geminiPricingDimensions(payload)
	default:
		return pricingRequestDimensions(payload)
	}
}

func gatewayAttachmentAttemptPlan(plan *gatewayAttemptPlan, attachments *gatewayAttachmentPlan) (*gatewayAttemptPlan, error) {
	if plan == nil || attachments == nil || len(attachments.Occurrences) == 0 {
		return plan, nil
	}
	retained := make([]gatewayAttemptCandidate, 0, len(plan.candidates))
	for _, candidate := range plan.Candidates() {
		if validateGatewayAttachmentRoute(attachments, &candidate.route) == nil {
			retained = append(retained, candidate)
		}
	}
	if len(retained) == 0 {
		return nil, gatewayError(http.StatusBadRequest, "attachment_type_unsupported", "No available route supports the requested attachment type.")
	}
	filtered, err := plan.filtered(retained)
	if err != nil {
		return nil, runtimeUnavailable
	}
	return filtered, nil
}

func (s *Service) prepareGatewayAttemptQuotaState(ctx context.Context, plan *gatewayAttemptPlan, result *GatewayResult) (*gatewayAttemptPlan, error) {
	s.limitMu.RLock()
	defer s.limitMu.RUnlock()
	limits, err := s.gatewayLimits(ctx, result)
	if err != nil {
		return nil, err
	}
	policies, data, err := s.gatewayQuotaPolicies(ctx, result, limits)
	if err != nil {
		return nil, err
	}
	filtered, bound, err := s.prepareGatewayAttemptQuotaBound(ctx, plan, result, policies, data)
	if err != nil {
		return nil, err
	}
	result.admissionLimits = limits
	result.admissionQuota = policies
	result.quotaBound = bound
	result.quotaTimeZone = data.Setting.TimeZone
	result.attemptEvidence = gatewayAttemptEvidence(filtered, result, policies, data)
	result.admissionAllowed = cloneGatewayAttemptAllowed(filtered.allowed)
	return filtered, nil
}

func (s *Service) preflightPreparedGatewayAttempt(requestID string, result *GatewayResult) error {
	if s.recorder == nil {
		for _, policy := range result.admissionQuota {
			if policy.Tokens5H != nil || policy.Tokens7D != nil || policy.TokensMonth != nil || policy.TPM != nil || policy.MoneyMonth != nil || policy.RPM != nil || policy.Concurrency != nil {
				return callQueueUnavailable
			}
		}
		return nil
	}
	if err := s.recorder.queue.PreflightWithQuota(requestID, result.admissionQuota, result.quotaBound, result.quotaTimeZone, time.Now().UTC()); err != nil {
		return quotaGatewayError(err)
	}
	return nil
}

func (s *Service) admitPreparedGatewayAttempt(ctx context.Context, requestID string, plan *gatewayAttemptPlan, result *GatewayResult, attempt routeattempt.Attempt) error {
	// Keep the established egress-before-limit lock order used by the legacy
	// admission path so direct and published runtime calls cannot deadlock.
	s.egressMu.RLock()
	defer s.egressMu.RUnlock()
	s.limitMu.RLock()
	defer s.limitMu.RUnlock()

	eligible, err := s.gatewayAttemptEligible(ctx, plan, attempt)
	if err != nil || !eligible {
		if err != nil {
			return err
		}
		return runtimeUnavailable
	}
	route, _, ok := plan.Candidate(attempt)
	if !ok || route.EgressGeneration != s.egressGeneration.Load() {
		return runtimeUnavailable
	}
	auth := s.runtime.auth.Load()
	if auth == nil || !s.gatewayAttemptClock().Before(auth.ValidUntil) || auth.ConnectionRevisions[route.ConnectionID] != route.EgressRevision {
		return runtimeUnavailable
	}
	limits, err := s.gatewayLimits(ctx, result)
	if err != nil {
		return err
	}
	policies, data, err := s.gatewayQuotaPolicies(ctx, result, limits)
	if err != nil {
		return err
	}
	current, bound, err := s.prepareGatewayAttemptQuotaBound(ctx, plan, result, policies, data)
	if err != nil {
		return err
	}
	if allowed := current.allowed[gatewayAttemptKey(attempt)]; !allowed {
		return runtimeUnavailable
	}
	result.admissionLimits = limits
	result.admissionQuota = policies
	result.quotaBound = bound
	result.quotaTimeZone = data.Setting.TimeZone
	result.attemptEvidence = gatewayAttemptEvidence(current, result, policies, data)
	result.admissionAllowed = cloneGatewayAttemptAllowed(current.allowed)
	return s.AdmitGatewayCall(requestID, result)
}

func (s *Service) validateGatewayRetryEvidence(ctx context.Context, plan *gatewayAttemptPlan, result *GatewayResult, attempt routeattempt.Attempt) error {
	s.egressMu.RLock()
	defer s.egressMu.RUnlock()
	s.limitMu.RLock()
	defer s.limitMu.RUnlock()
	if !result.admissionAllowed[gatewayAttemptKey(attempt)] {
		return runtimeUnavailable
	}
	eligible, err := s.gatewayAttemptEligible(ctx, plan, attempt)
	if err != nil || !eligible {
		if err != nil {
			return err
		}
		return runtimeUnavailable
	}
	route, _, ok := plan.Candidate(attempt)
	if !ok || route.EgressGeneration != s.egressGeneration.Load() {
		return runtimeUnavailable
	}
	auth := s.runtime.auth.Load()
	if auth == nil || !s.gatewayAttemptClock().Before(auth.ValidUntil) || auth.ConnectionRevisions[route.ConnectionID] != route.EgressRevision {
		return runtimeUnavailable
	}
	limits, err := s.gatewayLimits(ctx, result)
	if err != nil {
		return err
	}
	policies, data, err := s.gatewayQuotaPolicies(ctx, result, limits)
	if err != nil {
		return err
	}
	if data.Setting.TimeZone != result.quotaTimeZone || !reflect.DeepEqual(limits, result.admissionLimits) || !reflect.DeepEqual(policies, result.admissionQuota) {
		return runtimeUnavailable
	}
	candidate, ok := gatewayAttemptCandidateFor(plan, attempt)
	if !ok {
		return runtimeUnavailable
	}
	tokens, money := gatewayQuotaDimensions(policies)
	evidence, usable := gatewayCandidateBoundEvidence(candidate, result, data, tokens, money)
	if !usable || !reflect.DeepEqual(evidence, result.attemptEvidence[gatewayAttemptKey(attempt)]) {
		return runtimeUnavailable
	}
	return nil
}

func gatewayAttemptEvidence(plan *gatewayAttemptPlan, result *GatewayResult, policies []eventqueue.QuotaLimit, data *runtimeQuotaData) map[string]gatewayAttemptBoundEvidence {
	evidence := map[string]gatewayAttemptBoundEvidence{}
	if plan == nil || result == nil || data == nil {
		return evidence
	}
	tokens, money := gatewayQuotaDimensions(policies)
	for _, candidate := range plan.Candidates() {
		item, usable := gatewayCandidateBoundEvidence(candidate, result, data, tokens, money)
		if usable {
			evidence[gatewayAttemptKey(candidate.attempt)] = item
		}
	}
	return evidence
}

func gatewayAttemptCandidateFor(plan *gatewayAttemptPlan, attempt routeattempt.Attempt) (gatewayAttemptCandidate, bool) {
	if plan != nil {
		for _, candidate := range plan.candidates {
			if gatewayAttemptKey(candidate.attempt) == gatewayAttemptKey(attempt) && candidate.attempt.ConnectionID == attempt.ConnectionID {
				return candidate, true
			}
		}
	}
	return gatewayAttemptCandidate{}, false
}

func cloneGatewayAttemptAllowed(source map[string]bool) map[string]bool {
	result := make(map[string]bool, len(source))
	for key, allowed := range source {
		result[key] = allowed
	}
	return result
}

func prepareGatewayHTTPRequest(ctx context.Context, requestID string, result *GatewayResult, payload map[string]json.RawMessage, route *gatewayRoute, credential string, allowPrivate bool, options ...gatewayNativeOptions) (*preparedGatewayAttempt, error) {
	if route == nil || route.Client == nil {
		return nil, gatewayError(http.StatusServiceUnavailable, "upstream_unavailable", "No usable upstream is available.")
	}
	base, err := upstream.ValidateBaseURL(route.BaseURL, allowPrivate)
	if err != nil {
		return nil, gatewayError(http.StatusServiceUnavailable, "upstream_unavailable", "No usable upstream is available.")
	}
	suffix := "/chat/completions"
	switch result.NativeProtocol() {
	case entity.ProtocolOpenAIResponses:
		suffix = "/responses"
	case entity.ProtocolAnthropicMessages:
		suffix = "/messages"
	case entity.ProtocolGeminiGenerateContent:
		if !geminiModelSegment.MatchString(route.UpstreamName) {
			return nil, gatewayError(http.StatusServiceUnavailable, "upstream_unavailable", "The upstream model path is unavailable.")
		}
		suffix = "/models/" + route.UpstreamName + ":generateContent"
		if result.Stream {
			suffix = "/models/" + route.UpstreamName + ":streamGenerateContent?alt=sse"
		}
	}
	requestPayload := make(map[string]json.RawMessage, len(payload)+1)
	for key, value := range payload {
		requestPayload[key] = value
	}
	if result.NativeProtocol() != entity.ProtocolGeminiGenerateContent {
		requestPayload["model"], err = json.Marshal(route.UpstreamName)
		if err != nil {
			return nil, gatewayError(http.StatusInternalServerError, "internal_error", "The request could not be prepared.")
		}
	}
	encoded, err := json.Marshal(requestPayload)
	if err != nil {
		return nil, gatewayError(http.StatusBadRequest, "invalid_request_error", "The request must be a valid JSON object.")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base.String(), "/")+suffix, bytes.NewReader(encoded))
	if err != nil {
		return nil, gatewayError(http.StatusServiceUnavailable, "upstream_unavailable", "No usable upstream is available.")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+credential)
	if result.NativeProtocol() == entity.ProtocolAnthropicMessages {
		if len(options) != 1 {
			return nil, gatewayError(http.StatusBadRequest, "invalid_request_error", "Native headers are required.")
		}
		req.Header.Del("Authorization")
		req.Header.Set("x-api-key", credential)
		req.Header.Set("anthropic-version", options[0].Messages.Version)
		if options[0].Messages.Beta != "" {
			req.Header.Set("anthropic-beta", options[0].Messages.Beta)
		}
	}
	if result.NativeProtocol() == entity.ProtocolGeminiGenerateContent {
		req.Header.Del("Authorization")
		req.Header.Set("x-goog-api-key", credential)
	}
	req.Header.Set("X-Request-ID", requestID)
	if result.Stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	client := *route.Client
	if result.Stream {
		client.Timeout = 0
	}
	return &preparedGatewayAttempt{route: route, credential: credential, request: req, client: &client}, nil
}

func applyGatewayAttemptRoute(result *GatewayResult, route *gatewayRoute) {
	result.SnapshotID = route.SnapshotID
	result.ProviderID = route.ProviderID
	result.ProviderName = route.ProviderName
	result.ProviderModelID = route.ProviderModelID
	result.ConnectionID = route.ConnectionID
	result.ConnectionName = route.ConnectionName
	result.UpstreamModelName = route.UpstreamName
	result.CredentialID = route.CredentialID
	result.PriceBasis = clonePriceBasis(route.PriceBasis)
}

func (s *Service) executeGatewayAttempt(ctx context.Context, requestID string, result *GatewayResult, attempt routeattempt.Attempt, prepared *preparedGatewayAttempt) (routeattempt.Outcome, error, error) {
	if result.identity.team != nil {
		if err := s.ReauthorizeTeamSession(ctx, result.identity.team, result.ModelID); err != nil {
			return routeattempt.Outcome{Failure: routeattempt.PermanentFailure, Work: routeattempt.NotSent}, err, routeattempt.ErrExecution
		}
	}
	result.AttemptID, _ = id.NewPrefixed("att")
	if result.AttemptID == "" {
		return routeattempt.Outcome{Failure: routeattempt.PermanentFailure, Work: routeattempt.NotSent}, gatewayError(http.StatusInternalServerError, "internal_error", "The request could not be initialized."), routeattempt.ErrExecution
	}
	applyGatewayAttemptRoute(result, prepared.route)
	result.AttemptStartedAt = s.gatewayAttemptClock()
	if err := s.CheckpointGatewayCall(requestID, result); err != nil {
		return routeattempt.Outcome{Failure: routeattempt.PermanentFailure, Work: routeattempt.NotSent}, err, routeattempt.ErrExecution
	}
	if result.identity.team != nil {
		if err := s.ReauthorizeTeamSession(ctx, result.identity.team, result.ModelID); err != nil {
			result.AttemptID = ""
			result.AttemptStartedAt = time.Time{}
			if checkpointErr := s.CheckpointGatewayCall(requestID, result); checkpointErr != nil {
				return routeattempt.Outcome{Failure: routeattempt.PermanentFailure, Work: routeattempt.NotSent}, checkpointErr, routeattempt.ErrExecution
			}
			return routeattempt.Outcome{Failure: routeattempt.PermanentFailure, Work: routeattempt.NotSent}, err, routeattempt.ErrExecution
		}
	}
	response, requestErr := prepared.client.Do(prepared.request)
	result.Response = response
	if requestErr != nil {
		if ctx.Err() != nil {
			public := gatewayContextError(ctx.Err())
			outcome := routeattempt.Outcome{Failure: routeattempt.PermanentFailure, Work: routeattempt.Unknown}
			return outcome, public, s.finishGatewayFailedAttempt(requestID, result, attempt, outcome, "transport_ambiguous", 0, gatewayErrorCode(public))
		}
		if upstream.IsPreRequestFailure(requestErr) {
			outcome := routeattempt.Outcome{Failure: routeattempt.ConnectionFailure, Work: routeattempt.NotSent}
			s.markGatewayConnectionFailure(attempt.ConnectionID)
			public := gatewayError(http.StatusBadGateway, "upstream_error", "The upstream request failed.")
			return outcome, public, s.finishGatewayFailedAttempt(requestID, result, attempt, outcome, "pre_request_connection", 0, "upstream_error")
		}
		var timedOut interface{ Timeout() bool }
		public := error(gatewayError(http.StatusBadGateway, "upstream_error", "The upstream request failed."))
		if errors.As(requestErr, &timedOut) && timedOut.Timeout() {
			public = gatewayError(http.StatusGatewayTimeout, "upstream_timeout", "The upstream request timed out.")
		}
		outcome := routeattempt.Outcome{Failure: routeattempt.PermanentFailure, Work: routeattempt.Unknown}
		return outcome, public, s.finishGatewayFailedAttempt(requestID, result, attempt, outcome, "transport_ambiguous", 0, gatewayErrorCode(public))
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		classification, public := readGatewayHTTPFailure(response, result.NativeProtocol())
		_ = response.Body.Close()
		result.Response = nil
		if classification.Outcome.Failure == routeattempt.CredentialRejected {
			s.markGatewayCredentialRejected(attempt.CredentialID)
		}
		err := s.finishGatewayFailedAttempt(requestID, result, attempt, classification.Outcome, classification.EvidenceCode, response.StatusCode, gatewayErrorCode(public))
		return classification.Outcome, public, err
	}
	contentType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaErr != nil || result.Stream && contentType != "text/event-stream" || !result.Stream && contentType != "application/json" {
		_ = response.Body.Close()
		result.Response = nil
		outcome := routeattempt.Outcome{Failure: routeattempt.PermanentFailure, Work: routeattempt.Unknown}
		public := gatewayError(http.StatusBadGateway, "invalid_upstream_response", "The upstream returned an invalid response.")
		return outcome, public, s.finishGatewayFailedAttempt(requestID, result, attempt, outcome, "upstream_response", response.StatusCode, public.Code)
	}
	s.markGatewayAttemptSuccess(attempt.ConnectionID, attempt.CredentialID)
	return routeattempt.Outcome{Failure: routeattempt.Success, Work: routeattempt.Completed}, nil, nil
}

func (s *Service) finishGatewayFailedAttempt(requestID string, result *GatewayResult, attempt routeattempt.Attempt, outcome routeattempt.Outcome, evidence string, status int, code string) error {
	result.Attempts = append(result.Attempts, CallAttempt{
		ID:                result.AttemptID,
		ProviderID:        result.ProviderID,
		ProviderName:      result.ProviderName,
		ProviderModelID:   result.ProviderModelID,
		ConnectionID:      result.ConnectionID,
		CredentialID:      result.CredentialID,
		SnapshotID:        result.SnapshotID,
		ConnectionName:    result.ConnectionName,
		UpstreamModelName: result.UpstreamModelName,
		AttemptNumber:     attempt.Number,
		Status:            "error",
		FailureClass:      string(outcome.Failure),
		WorkEvidence:      string(outcome.Work),
		OutputStarted:     outcome.OutputStarted,
		FinalUsageKnown:   outcome.FinalUsageKnown,
		EvidenceCode:      evidence,
		StartedAt:         result.AttemptStartedAt,
		CompletedAt:       s.gatewayAttemptClock(),
		HTTPStatus:        status,
		ErrorCode:         safeCallError(code),
	})
	result.AttemptID = ""
	result.AttemptStartedAt = time.Time{}
	result.Response = nil
	return s.CheckpointGatewayCall(requestID, result)
}

func gatewayContextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return gatewayError(http.StatusGatewayTimeout, "upstream_timeout", "The upstream request timed out.")
	}
	return gatewayError(http.StatusBadGateway, "canceled", "The upstream request was canceled.")
}

func gatewayErrorCode(err error) string {
	var gateway *GatewayError
	if errors.As(err, &gateway) {
		return gateway.Code
	}
	return "upstream_error"
}

func gatewayPublicAttemptError(err error) error {
	var gateway *GatewayError
	if errors.As(err, &gateway) {
		return gateway
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return gatewayContextError(err)
	}
	return gatewayError(http.StatusServiceUnavailable, "upstream_unavailable", "No usable upstream is available.")
}
