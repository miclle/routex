package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

func TestAPIKeyDeliveryReviewAndBodyPrivacy(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.POST("/intent", apiKeyDeliveryResponseHeaders, jsonManagementRequest, func(c *fox.Context) error {
		if e := apiKeyDeliveryRequest(c); e != nil {
			return e
		}
		if _, e := apiKeyDeliveryReview(c); e != nil {
			return e
		}
		var in service.APIKeyDeliveryIdentityInput
		if e := apiKeyDeliveryBody(c, &in); e != nil {
			return e
		}
		if in.Token != "private-token" {
			return errors.New("private-token")
		}
		c.Status(204)
		return nil
	})
	quoted := `"` + strings.Repeat("a", 64) + `"`
	good := `{"purpose":"writer","integration_id":"vin_owned","token":"private-token","reason":"Reviewed"}`
	for _, tc := range []struct {
		name, path, body string
		headers          []string
		want             int
	}{
		{"exact", "/intent", good, []string{quoted}, 204},
		{"missing_review", "/intent", good, nil, 428},
		{"weak", "/intent", good, []string{"W/" + quoted}, 400},
		{"unquoted", "/intent", good, []string{strings.Repeat("a", 64)}, 400},
		{"uppercase", "/intent", good, []string{`"` + strings.Repeat("A", 64) + `"`}, 400},
		{"duplicate_review", "/intent", good, []string{quoted, quoted}, 400},
		{"list_review", "/intent", good, []string{quoted + "," + quoted}, 400},
		{"query", "/intent?token=private-token", good, []string{quoted}, 400},
		{"force_query", "/intent?", good, []string{quoted}, 400},
		{"unknown", "/intent", strings.TrimSuffix(good, "}") + `,"accessor":"private-token"}`, []string{quoted}, 400},
		{"duplicate", "/intent", strings.Replace(good, `"token":"private-token"`, `"token":"private-token","token":"other"`, 1), []string{quoted}, 400},
		{"null_secret", "/intent", strings.Replace(good, `"token":"private-token"`, `"token":null`, 1), []string{quoted}, 400},
		{"overflow", "/intent", strings.Repeat(" ", 64<<10) + good, []string{quoted}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			for _, h := range tc.headers {
				req.Header.Add("If-Match", h)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.want || rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" || strings.Contains(rec.Body.String(), "private-token") {
				t.Fatal("unsafe wire denial", rec.Code)
			}
		})
	}
}

func TestAPIKeyDeliveryMutationUsesExistingOriginAdminAndCSRFGuards(t *testing.T) {
	entered := 0
	auth := &service.Authentication{User: entity.User{Role: entity.RoleAdmin}, Token: "transient-session"}
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.POST("/intent", apiKeyDeliveryResponseHeaders, func(c *fox.Context) error {
		actor := *auth
		if c.Request.Header.Get("X-Test-Member") == "yes" {
			actor.User.Role = entity.RoleMember
		}
		c.Set(authenticationKey, &actor)
		c.Next()
		return nil
	}, requireAdmin, sameOrigin, requireCSRF, jsonManagementRequest, func(c *fox.Context) error {
		entered++
		c.Status(204)
		return nil
	})
	for _, tc := range []struct {
		name, origin, csrf, mime, member string
		want                             int
	}{
		{"current_admin", "http://example.com", auth.CSRFToken(), "application/json", "", 204},
		{"member", "http://example.com", auth.CSRFToken(), "application/json", "yes", 403},
		{"foreign_origin", "https://foreign.example", auth.CSRFToken(), "application/json", "", 403},
		{"missing_csrf", "http://example.com", "", "application/json", "", 403},
		{"wrong_csrf", "http://example.com", "private-token", "application/json", "", 403},
		{"wrong_content_type", "http://example.com", auth.CSRFToken(), "text/plain", "", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "http://example.com/intent", strings.NewReader(`{}`))
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("X-CSRF-Token", tc.csrf)
			req.Header.Set("Content-Type", tc.mime)
			req.Header.Set("X-Test-Member", tc.member)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.want || rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("existing guard or denial privacy changed", rec.Code)
			}
		})
	}
	if entered != 1 {
		t.Fatal("denied request reached operation")
	}
}

func TestAPIKeyDeliveryManagedResponseIsOnlySafeMetadata(t *testing.T) {
	review := strings.Repeat("a", 64)
	for _, project := range []string{"", "prj_owned"} {
		for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusAccepted} {
			v := &service.ManagedAPIKeyResult{HTTPStatus: status, Delivery: service.APIKeyDeliveryReceiptView{KeyID: "key_owned", Mode: "managed_vault", DeliveryStatus: "unknown", RuntimeStatus: "unknown", ReviewETag: review}}
			if project == "" {
				v.Personal = &service.KeyRecord{Key: entity.APIKey{ID: "key_owned", UserID: "usr_owned", TokenHash: "private-token", DeliveryMode: "managed_vault", Status: entity.KeyPending}, ModelIDs: []string{"mdl_owned"}}
			} else {
				v.Project = &service.ProjectKeyRecord{Key: entity.ProjectKey{ID: "key_owned", ProjectID: project, CreatorID: "usr_owned", TokenHash: "private-token", DeliveryMode: "managed_vault", Status: entity.KeyPending}, ModelIDs: []string{"mdl_owned"}}
			}
			safe, e := apiKeyDeliveryManagedDTO(v, project)
			if e != nil {
				t.Fatal("closed status or safe projection rejected")
			}
			raw, e := json.Marshal(safe)
			if e != nil {
				t.Fatal("safe projection marshal failed")
			}
			var top map[string]json.RawMessage
			if json.Unmarshal(raw, &top) != nil || len(top) != 2 || top["key"] == nil || top["delivery"] == nil || top["secret"] != nil || strings.Contains(string(raw), "private-token") {
				t.Fatal("managed envelope exposed private record or secret")
			}
			var key map[string]json.RawMessage
			if json.Unmarshal(top["key"], &key) != nil || string(key["delivery_mode"]) != `"managed_vault"` || key["TokenHash"] != nil || key["token_hash"] != nil || key["UserID"] != nil {
				t.Fatal("unsafe Key metadata projection")
			}
			router := fox.New()
			router.RenderErrorFunc = renderAPIError
			router.POST("/managed", apiKeyDeliveryResponseHeaders, func(c *fox.Context) error { return apiKeyDeliveryWriteManaged(c, v, project) })
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest("POST", "/managed", nil))
			if rec.Code != status || rec.Header().Get("ETag") != `"`+review+`"` || strings.Contains(rec.Body.String(), "private-token") {
				t.Fatal("Service status or review not preserved", rec.Code)
			}
		}
	}
	// Stored manual metadata and the existing explicit manual secret envelope remain separate.
	k := keyResponse(service.KeyRecord{Key: entity.APIKey{DeliveryMode: "manual"}})
	raw, e := json.Marshal(CreatedKeyResponse{Key: k, Secret: "manual-once"})
	if e != nil || !strings.Contains(string(raw), `"delivery_mode":"manual"`) || !strings.Contains(string(raw), `"secret":"manual-once"`) {
		t.Fatal("manual API response behavior changed")
	}
}

func TestAPIKeyDeliveryManagedResponseRejectsMixedOrUnsupportedRecords(t *testing.T) {
	good := func() *service.ManagedAPIKeyResult {
		return &service.ManagedAPIKeyResult{HTTPStatus: 202, Personal: &service.KeyRecord{Key: entity.APIKey{ID: "key_owned", DeliveryMode: "managed_vault"}}, Delivery: service.APIKeyDeliveryReceiptView{KeyID: "key_owned", Mode: "managed_vault", ReviewETag: strings.Repeat("a", 64)}}
	}
	for _, change := range []func(*service.ManagedAPIKeyResult){
		func(v *service.ManagedAPIKeyResult) { v.HTTPStatus = 500 },
		func(v *service.ManagedAPIKeyResult) { v.HTTPStatus = 204 },
		func(v *service.ManagedAPIKeyResult) { v.Personal = nil },
		func(v *service.ManagedAPIKeyResult) { v.Project = &service.ProjectKeyRecord{} },
		func(v *service.ManagedAPIKeyResult) { v.Delivery.KeyID = "key_other" },
		func(v *service.ManagedAPIKeyResult) { v.Delivery.ReviewETag = "bad" },
		func(v *service.ManagedAPIKeyResult) { v.Delivery.Mode = "manual" },
		func(v *service.ManagedAPIKeyResult) { v.Personal.Key.DeliveryMode = "manual" },
	} {
		v := good()
		change(v)
		if _, e := apiKeyDeliveryManagedDTO(v, ""); e == nil {
			t.Fatal("invalid result projection admitted")
		}
	}
	if _, e := apiKeyDeliveryManagedDTO(nil, ""); e == nil {
		t.Fatal("nil result admitted")
	}
	if _, e := apiKeyDeliveryManagedDTO(good(), "prj_other"); e == nil {
		t.Fatal("personal result crossed Project scope")
	}
	v := good()
	v.Personal = nil
	v.Project = &service.ProjectKeyRecord{Key: entity.ProjectKey{ID: "key_owned", ProjectID: "prj_owned", DeliveryMode: "managed_vault"}}
	if _, e := apiKeyDeliveryManagedDTO(v, "prj_other"); e == nil {
		t.Fatal("Project result crossed target scope")
	}
}

func TestAPIKeyDeliveryRegisteredRoutesRejectAnonymousPrivately(t *testing.T) {
	router := fox.New()
	New(nil).RegisterRoutes(router)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/v1/admin/api-key-delivery/policy"},
		{"PUT", "/api/v1/admin/api-key-delivery/policy"},
		{"POST", "/api/v1/admin/api-key-delivery/identities"},
		{"GET", "/api/v1/admin/api-key-delivery/identities/vdi_owned"},
		{"GET", "/api/v1/admin/api-key-delivery/caller-template"},
		{"PUT", "/api/v1/admin/api-key-delivery/caller-template"},
		{"POST", "/api/v1/admin/api-key-delivery/verify"},
		{"GET", "/api/v1/admin/api-key-delivery/targets"},
		{"GET", "/api/v1/admin/api-key-delivery/targets/vap_owned"},
		{"PUT", "/api/v1/admin/api-key-delivery/targets/vap_owned/approval"},
		{"PUT", "/api/v1/admin/api-key-delivery/targets/vap_owned/isolation"},
		{"POST", "/api/v1/admin/api-key-delivery/targets/vap_owned/reconcile"},
		{"GET", "/api/v1/keys/delivery-context"},
		{"POST", "/api/v1/keys/delivery-targets/resolve"},
		{"GET", "/api/v1/keys/delivery-targets/vap_owned"},
		{"POST", "/api/v1/keys/managed"},
		{"POST", "/api/v1/keys/key_owned/managed-rotation"},
		{"GET", "/api/v1/keys/delivery-receipts/11111111-1111-4111-8111-111111111111"},
		{"POST", "/api/v1/keys/key_owned/delivery-reconcile"},
		{"GET", "/api/v1/keys/key_owned/delivery-descriptor"},
		{"GET", "/api/v1/projects/prj_owned/keys/delivery-context"},
		{"POST", "/api/v1/projects/prj_owned/keys/delivery-targets/resolve"},
		{"GET", "/api/v1/projects/prj_owned/keys/delivery-targets/vap_owned"},
		{"POST", "/api/v1/projects/prj_owned/keys/managed"},
		{"POST", "/api/v1/projects/prj_owned/keys/key_owned/managed-rotation"},
		{"GET", "/api/v1/projects/prj_owned/keys/delivery-receipts/11111111-1111-4111-8111-111111111111"},
		{"POST", "/api/v1/projects/prj_owned/keys/key_owned/delivery-reconcile"},
		{"GET", "/api/v1/projects/prj_owned/keys/key_owned/delivery-descriptor"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != 401 || rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("ETag") != "" {
				t.Fatal("registered route bypass or unsafe denial", rec.Code)
			}
		})
	}
}

func TestAPIKeyDeliveryDescriptorRevisionHasExactClosedWireShape(t *testing.T) {
	for _, revision := range []uint64{9007199254740993, 9223372036854775807, 18446744073709551615} {
		t.Run(strconv.FormatUint(revision, 10), func(t *testing.T) {
			original := &service.APIKeyDeliveryDescriptorView{SchemaVersion: 1, KeyID: "key_owned", ProfileID: "vdp_owned", PrincipalID: "vap_owned", SlotID: "vds_owned", DescriptorRevision: revision, Endpoint: "https://vault.example", Namespace: "owned", Mount: "kv", Path: "keys/owned", DataField: "api_key", Version: 1, Method: "jwt", AuthMount: "jwt", Role: "owned", Coordinator: "application", ApplicationStatus: "active"}
			descriptor := apiKeyDeliveryDescriptorDTO(original)
			expected := map[string]any{"schema_version": 1, "key_id": "key_owned", "profile_id": "vdp_owned", "principal_id": "vap_owned", "slot_id": "vds_owned", "descriptor_revision": strconv.FormatUint(revision, 10), "endpoint": "https://vault.example", "namespace": "owned", "mount": "kv", "path": "keys/owned", "data_field": "api_key", "version": 1, "method": "jwt", "auth_mount": "jwt", "role": "owned", "coordinator": "application", "application_status": "active"}
			check := func(raw []byte) {
				t.Helper()
				var fields map[string]json.RawMessage
				if json.Unmarshal(raw, &fields) != nil || len(fields) != len(expected) {
					t.Fatal("descriptor fields are not closed")
				}
				for key, want := range expected {
					encoded, e := json.Marshal(want)
					if e != nil || string(fields[key]) != string(encoded) {
						t.Fatal("descriptor field changed", key)
					}
				}
			}
			raw, e := json.Marshal(descriptor)
			if e != nil {
				t.Fatal("descriptor marshal failed")
			}
			check(raw)
			receipt := service.APIKeyDeliveryReceiptView{RequestID: "request_owned", KeyID: "key_owned", Mode: "managed_vault", DeliveryStatus: "written", KeyStatus: "active", RuntimeStatus: "applied", Descriptor: original, ReviewETag: strings.Repeat("a", 64)}
			raw, e = json.Marshal(apiKeyDeliveryReceiptDTO(receipt))
			if e != nil {
				t.Fatal("receipt marshal failed")
			}
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) != nil || len(fields) != 9 {
				t.Fatal("receipt fields are not closed")
			}
			check(fields["descriptor"])
			v := &service.ManagedAPIKeyResult{HTTPStatus: 200, Personal: &service.KeyRecord{Key: entity.APIKey{ID: "key_owned", DeliveryMode: "managed_vault"}}, Delivery: receipt}
			safe, e := apiKeyDeliveryManagedDTO(v, "")
			if e != nil {
				t.Fatal("managed receipt projection failed")
			}
			raw, e = json.Marshal(safe)
			if e != nil {
				t.Fatal("managed marshal failed")
			}
			var envelope struct {
				Delivery struct {
					Descriptor json.RawMessage `json:"descriptor"`
				} `json:"delivery"`
			}
			if json.Unmarshal(raw, &envelope) != nil {
				t.Fatal("managed envelope decode failed")
			}
			check(envelope.Delivery.Descriptor)
			if original.DescriptorRevision != revision {
				t.Fatal("projection mutated Service revision")
			}
		})
	}
	if apiKeyDeliveryDescriptorDTO(nil) != nil || apiKeyDeliveryReceiptDTO(service.APIKeyDeliveryReceiptView{}).Descriptor != nil {
		t.Fatal("unknown descriptor became known")
	}
}
