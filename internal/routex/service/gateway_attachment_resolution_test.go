package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestGatewayAttachmentRejectsProjectKeyBeforeResolution(t *testing.T) {
	svc, _, projectBearer, _ := projectRuntimeFixture(t)
	_, err := svc.GatewayChat(context.Background(), projectBearer, attachmentChatBody(testAttachmentImageID), "req_project_attachment")
	assertGatewayResolutionError(t, err, http.StatusBadRequest, "project_attachment_unsupported")
}

func TestGatewayAttachmentRequiresSelectedRouteCapability(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamCalls.Add(1)
	}))
	defer upstream.Close()
	svc, _, bearer := runtimeFixture(t, upstream.URL+"/v1")
	_, err := svc.GatewayChat(context.Background(), bearer, attachmentChatBody(testAttachmentImageID), "req_attachment_capability")
	assertGatewayResolutionError(t, err, http.StatusBadRequest, "attachment_type_unsupported")
	if upstreamCalls.Load() != 0 {
		t.Fatal("capability rejection reached upstream")
	}
}

func TestGatewayAttachmentQuotaPreflightPrecedesStorage(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamCalls.Add(1)
	}))
	defer upstream.Close()
	svc, data, bearer := runtimeFixture(t, upstream.URL+"/v1")
	data.ProviderModels[0].SupportsImageInput = true
	data.Limits = []entity.ResourceLimit{{ScopeKind: "user", ScopeID: "usr_one", ETag: "policy_attachment", Tokens5H: limitNumber(100)}}
	if err := compileRuntimeLimits(data); err != nil {
		t.Fatal(err)
	}
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	routes, err := svc.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_attachment", Models: routes, PublishedAt: time.Now()})

	// runtimeFixture intentionally has no database or storage configuration. A
	// storage lookup would therefore fail the test instead of producing this
	// deterministic quota-shape rejection.
	_, err = svc.GatewayChat(context.Background(), bearer, attachmentChatBody(testAttachmentImageID), "req_attachment_quota")
	assertGatewayResolutionError(t, err, http.StatusBadRequest, "quota_request_unsupported")
	if upstreamCalls.Load() != 0 {
		t.Fatal("quota preflight rejection reached upstream")
	}
}

func attachmentChatBody(objectID string) []byte {
	return []byte(`{"model":"public-model","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"routex://attachments/` + objectID + `"}}]}]}`)
}

func assertGatewayResolutionError(t *testing.T, err error, status int, code string) {
	t.Helper()
	var gateway *GatewayError
	if !errors.As(err, &gateway) || gateway.Status != status || gateway.Code != code {
		t.Fatalf("gateway error = %#v, want status %d code %s", err, status, code)
	}
}
