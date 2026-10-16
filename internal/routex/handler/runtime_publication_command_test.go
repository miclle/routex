package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

const runtimePublicationTestID = "11111111-1111-4111-8111-111111111111"

func TestRuntimePublicationCommandBody(t *testing.T) {
	good := `{"request_id":"` + runtimePublicationTestID + `","reason":"private-intent"}`
	wantReason := "private-intent"
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.POST("/intent", runtimePublicationResponseHeaders, jsonManagementRequest, func(c *fox.Context) error {
		in, err := runtimePublicationBody(c)
		if err != nil {
			return err
		}
		if in.RequestID != runtimePublicationTestID || in.Reason != wantReason {
			t.Fatal("command intent changed during decoding")
		}
		c.Status(204)
		return nil
	})
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"exact", good, 204},
		{"unicode_reason", strings.Replace(good, "private-intent", "Reviewed 审核", 1), 204},
		{"reason_boundary", strings.Replace(good, "private-intent", strings.Repeat("审", 1000), 1), 204},
		{"body_boundary", good + strings.Repeat(" ", (16<<10)-len(good)), 204},
		{"empty", "", 400},
		{"array", "[]", 400},
		{"missing_request_id", `{"reason":"private-intent"}`, 400},
		{"missing_reason", `{"request_id":"` + runtimePublicationTestID + `"}`, 400},
		{"duplicate_request_id", strings.TrimSuffix(good, "}") + `,"request_id":"` + runtimePublicationTestID + `"}`, 400},
		{"escaped_duplicate_reason", strings.TrimSuffix(good, "}") + `,"rea\u0073on":"other"}`, 400},
		{"case_alias", strings.Replace(good, "request_id", "Request_ID", 1), 400},
		{"unknown", strings.TrimSuffix(good, "}") + `,"snapshot_id":"private-intent"}`, 400},
		{"null_request_id", strings.Replace(good, `"`+runtimePublicationTestID+`"`, "null", 1), 400},
		{"null_reason", strings.Replace(good, `"private-intent"`, "null", 1), 400},
		{"number_reason", strings.Replace(good, `"private-intent"`, "42", 1), 400},
		{"noncanonical_uuid", strings.Replace(good, runtimePublicationTestID, "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA", 1), 400},
		{"wrong_uuid_version", strings.Replace(good, "-4111-", "-1111-", 1), 400},
		{"wrong_uuid_variant", strings.Replace(good, "-8111-", "-7111-", 1), 400},
		{"uuid_whitespace", strings.Replace(good, runtimePublicationTestID, " "+runtimePublicationTestID, 1), 400},
		{"blank_reason", strings.Replace(good, "private-intent", "", 1), 400},
		{"leading_reason_space", strings.Replace(good, "private-intent", " private-intent", 1), 400},
		{"trailing_reason_space", strings.Replace(good, "private-intent", "private-intent ", 1), 400},
		{"control_reason", strings.Replace(good, "private-intent", `private-intent\n`, 1), 400},
		{"reason_overflow", strings.Replace(good, "private-intent", strings.Repeat("审", 1001), 1), 400},
		{"surrogate_reason", strings.Replace(good, "private-intent", `\ud800`, 1), 400},
		{"invalid_utf8", strings.Replace(good, "private-intent", string([]byte{0xff}), 1), 400},
		{"trailing_object", good + "{}", 400},
		{"malformed", strings.TrimSuffix(good, "}"), 400},
		{"body_overflow", good + strings.Repeat(" ", (16<<10)+1-len(good)), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantReason = "private-intent"
			switch tc.name {
			case "unicode_reason":
				wantReason = "Reviewed 审核"
			case "reason_boundary":
				wantReason = strings.Repeat("审", 1000)
			}
			req := httptest.NewRequest("POST", "/intent", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.want || rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" || strings.Contains(rec.Body.String(), "private-intent") {
				t.Fatal("unsafe command body response", rec.Code)
			}
		})
	}
}

func TestRuntimePublicationCommandMutationGuards(t *testing.T) {
	entered := 0
	auth := &service.Authentication{User: entity.User{Role: entity.RoleAdmin}, Token: "transient-session"}
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.POST("/intent", runtimePublicationResponseHeaders, func(c *fox.Context) error {
		actor := *auth
		if c.Request.Header.Get("X-Test-Member") == "yes" {
			actor.User.Role = entity.RoleMember
		}
		c.Set(authenticationKey, &actor)
		c.Next()
		return nil
	}, requireAdmin, sameOrigin, requireCSRF, jsonManagementRequest, func(c *fox.Context) error {
		if _, err := runtimePublicationBody(c); err != nil {
			return err
		}
		entered++
		c.Status(204)
		return nil
	})
	for _, tc := range []struct {
		name, origin, csrf, mime, member string
		want                             int
	}{
		{"current_admin", "http://example.com", auth.CSRFToken(), "application/json", "", 204},
		{"native_admin", "", auth.CSRFToken(), "application/json", "", 204},
		{"member", "http://example.com", auth.CSRFToken(), "application/json", "yes", 403},
		{"foreign_origin", "https://foreign.example", auth.CSRFToken(), "application/json", "", 403},
		{"missing_csrf", "http://example.com", "", "application/json", "", 403},
		{"wrong_csrf", "http://example.com", "private-intent", "application/json", "", 403},
		{"wrong_content_type", "http://example.com", auth.CSRFToken(), "text/plain", "", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"request_id":"` + runtimePublicationTestID + `","reason":"private-intent"}`
			req := httptest.NewRequest("POST", "http://example.com/intent", strings.NewReader(body))
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("X-CSRF-Token", tc.csrf)
			req.Header.Set("Content-Type", tc.mime)
			req.Header.Set("X-Test-Member", tc.member)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.want || rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" || strings.Contains(rec.Body.String(), "private-intent") {
				t.Fatal("existing mutation guard or privacy changed", rec.Code)
			}
		})
	}
	if entered != 2 {
		t.Fatal("denied request reached command dispatch")
	}
}

func TestRuntimePublicationCommandRejectsInvalidTargetBeforeService(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	ctrl := New(nil)
	router.POST("/publish", runtimePublicationResponseHeaders, jsonManagementRequest, ctrl.PublishRuntime)
	router.GET("/commands/:requestID", runtimePublicationResponseHeaders, ctrl.GetRuntimePublicationCommand)
	for _, tc := range []struct{ method, target, body string }{
		{"POST", "/publish", "{}"},
		{"POST", "/publish?reason=private-intent", "{}"},
		{"POST", "/publish?", "{}"},
		{"GET", "/commands/not-a-uuid", ""},
		{"GET", "/commands/AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA", ""},
		{"GET", "/commands/" + runtimePublicationTestID + "?reason=private-intent", ""},
		{"GET", "/commands/" + runtimePublicationTestID + "?", ""},
	} {
		t.Run(tc.method+tc.target, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != 400 || rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" || strings.Contains(rec.Body.String(), "private-intent") {
				t.Fatal("invalid command crossed service boundary", rec.Code)
			}
		})
	}
}

func TestRuntimePublicationCommandRegisteredRoutesRejectAnonymousPrivately(t *testing.T) {
	router := fox.New()
	New(nil).RegisterRoutes(router)
	for _, tc := range []struct{ method, target string }{
		{"POST", "/api/v1/admin/runtime/publish"},
		{"GET", "/api/v1/admin/runtime/publication-commands/" + runtimePublicationTestID},
	} {
		t.Run(tc.method+tc.target, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != 401 || rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("ETag") != "" {
				t.Fatal("registered command route bypass or unsafe denial", rec.Code)
			}
		})
	}
}

func TestRuntimePublicationCommandReceiptHTTPContract(t *testing.T) {
	claimed := time.Date(2026, 10, 11, 1, 2, 3, 0, time.UTC)
	finalized := claimed.Add(time.Second)
	failure := "runtime_refresh_failed"
	for _, tc := range []struct {
		status    string
		finalized *time.Time
		failure   *string
		want      int
	}{
		{"pending", nil, nil, 202},
		{"unknown", nil, nil, 202},
		{"refresh_returned_nil", &finalized, nil, 200},
		{"refresh_returned_error", &finalized, &failure, 200},
	} {
		t.Run(tc.status, func(t *testing.T) {
			receipt := &service.RuntimePublicationCommandReceipt{
				RequestID: runtimePublicationTestID, ActorID: "usr_owned", Reason: "Reviewed",
				ClaimInstanceID: "ins_owned", ClaimInstanceStartedAt: claimed.Add(-time.Minute),
				ClaimedAt: claimed, FinalizedAt: tc.finalized, Status: tc.status, FailureCode: tc.failure,
			}
			router := fox.New()
			router.RenderErrorFunc = renderAPIError
			router.GET("/receipt", runtimePublicationResponseHeaders, func(c *fox.Context) error {
				return runtimePublicationReply(c, receipt)
			})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest("GET", "/receipt", nil))
			if rec.Code != tc.want || rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("ETag") != "" {
				t.Fatal("receipt response contract changed", rec.Code)
			}
			var fields map[string]json.RawMessage
			expected := map[string]any{
				"request_id": runtimePublicationTestID, "actor_id": "usr_owned", "reason": "Reviewed",
				"claim_instance_id": "ins_owned", "claim_instance_started_at": claimed.Add(-time.Minute),
				"claimed_at": claimed, "finalized_at": tc.finalized, "status": tc.status, "failure_code": tc.failure, "acknowledgement": nil,
			}
			if json.Unmarshal(rec.Body.Bytes(), &fields) != nil || len(fields) != len(expected) {
				t.Fatal("receipt wire fields are not closed")
			}
			for key, value := range expected {
				encoded, err := json.Marshal(value)
				if err != nil || string(fields[key]) != string(encoded) {
					t.Fatal("receipt field or explicit null changed", key)
				}
			}
			// A durable local installation does not promote the original
			// pending/unknown/error outcome into successful publication.
			receipt.Acknowledgement = &service.RuntimePublicationAcknowledgementReceipt{Scope: "single_process_gateway_admission", InstallationID: "rin_01m36yee4gkbns18pfcqqc75a3", InstanceID: receipt.ClaimInstanceID, InstanceStartedAt: receipt.ClaimInstanceStartedAt, SnapshotID: "cfg_01m36yee4gkbns18pfcqqc75a3", RoutesPublishedAt: claimed.Add(-time.Second), AcknowledgedAt: finalized}
			acknowledged := httptest.NewRecorder()
			router.ServeHTTP(acknowledged, httptest.NewRequest("GET", "/receipt", nil))
			var withProof map[string]json.RawMessage
			if acknowledged.Code != tc.want || json.Unmarshal(acknowledged.Body.Bytes(), &withProof) != nil || len(withProof) != 10 || string(withProof["status"]) != string(fields["status"]) || string(withProof["finalized_at"]) != string(fields["finalized_at"]) || string(withProof["failure_code"]) != string(fields["failure_code"]) {
				t.Fatal("local acknowledgement reinterpreted original outcome")
			}
			var proof map[string]json.RawMessage
			if json.Unmarshal(withProof["acknowledgement"], &proof) != nil || len(proof) != 7 {
				t.Fatal("local acknowledgement is not a closed nonsecret object")
			}
			for _, key := range []string{"scope", "installation_id", "instance_id", "instance_started_at", "snapshot_id", "routes_published_at", "acknowledged_at"} {
				if _, exists := proof[key]; !exists {
					t.Fatal("missing acknowledgement field", key)
				}
			}

		})
	}
	for _, receipt := range []*service.RuntimePublicationCommandReceipt{nil, {Status: "applied"}} {
		router := fox.New()
		router.RenderErrorFunc = renderAPIError
		router.GET("/receipt", runtimePublicationResponseHeaders, func(c *fox.Context) error {
			return runtimePublicationReply(c, receipt)
		})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", "/receipt", nil))
		if rec.Code != 500 || strings.Contains(rec.Body.String(), "applied") || rec.Header().Get("ETag") != "" {
			t.Fatal("unsupported Service result became publication evidence", rec.Code)
		}
	}
}
