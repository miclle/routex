package handler

import (
	"encoding/json"
	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestRegistrationApprovalWirePrivacy(t *testing.T) {
	cases := []struct {
		name   string
		value  any
		fields []string
	}{
		{"pending", struct {
			Kind string `json:"kind"`
		}{"approval_pending"}, []string{"kind"}},
		{"public policy", service.RegistrationStatus{}, []string{"enabled", "approval_required"}},
		{"policy review", service.RegistrationPolicy{}, []string{"enabled", "approval_required", "review_etag"}},
		{"policy confirmation", service.RegistrationPolicyResult{}, []string{"confirmation", "enabled", "approval_required", "review_etag"}},
		{"decision review", service.MemberApprovalRecord{}, []string{"user_id", "name", "identity_role", "disabled", "offboarded_at", "approval_status", "application", "can_approve", "can_reject", "admission_eligible", "runtime_applied", "review_etag"}},
		{"decision confirmation", service.MemberApprovalResult{}, []string{"confirmation", "user_id", "application_id", "decision", "admission_eligible", "runtime_applied"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := json.Marshal(c.value)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(b, &fields); err != nil {
				t.Fatal(err)
			}
			if len(fields) != len(c.fields) {
				t.Fatalf("wire field count %d", len(fields))
			}
			for _, n := range c.fields {
				if _, ok := fields[n]; !ok {
					t.Fatalf("missing %s", n)
				}
			}
		})
	}
	typ := reflect.TypeFor[MemberResponse]()
	if _, exists := typ.FieldByName("RegistrationApproval"); exists {
		t.Fatal("mutation/create DTO acquired review metadata")
	}
}
func TestRegistrationPolicyStrictReviewInput(t *testing.T) {
	good := []byte(`{"enabled":false,"approval_required":true,"reason":"Reviewed policy"}`)
	var input service.RegistrationPolicyInput
	if err := json.Unmarshal(good, &input); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"enabled":true,"approval_required":null,"reason":"r"}`, `{"enabled":true,"approval_required":true,"reason":" r"}`, `{"enabled":true,"enabled":false,"approval_required":true,"reason":"r"}`, `{"Enabled":true,"approval_required":true,"reason":"r"}`, `{"enabled":true,"approval_required":true,"reason":"r","user_id":"usr_other"}`} {
		if json.Unmarshal([]byte(raw), &input) == nil {
			t.Fatalf("accepted invalid policy %q", raw)
		}
	}
}

func TestRegistrationApprovalRegisteredDenialsRemainPrivate(t *testing.T) {
	router := fox.New()
	New(nil).RegisterRoutes(router)
	for _, request := range []struct{ method, path string }{
		{"GET", "/api/v1/admin/registration"}, {"PATCH", "/api/v1/admin/registration"},
		{"GET", "/api/v1/admin/members/usr_subject/approval"}, {"PATCH", "/api/v1/admin/members/usr_subject/approval"},
	} {
		t.Run(request.method+request.path, func(t *testing.T) {
			req := httptest.NewRequest(request.method, request.path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			assertAPIErrorResponse(t, res, request.method, ErrorResponse{Code: 401, Message: "unauthorized"})
			if res.Header().Get("Cache-Control") != "private, no-store" || res.Header().Get("ETag") != "" {
				t.Fatal("entered denial leaked review metadata or cacheability")
			}
		})
	}
}
