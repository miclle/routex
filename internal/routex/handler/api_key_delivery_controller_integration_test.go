package handler

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

func deliveryControllerMachineRequest(t *testing.T, f *deliveryFixture, bearer, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal("encode controlled controller request")
		}
	}
	defer clear(raw)
	req := httptest.NewRequestWithContext(t.Context(), method, "http://routex.test"+path, bytes.NewReader(raw))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res := httptest.NewRecorder()
	f.router.ServeHTTP(res, req)
	if res.Header().Get("Cache-Control") != "private, no-store" || res.Header().Get("X-Content-Type-Options") != "nosniff" || bearer != "" && strings.Contains(res.Body.String(), bearer) {
		t.Fatal("machine response leaks bearer or cacheability")
	}
	f.peer.mu.Lock()
	defer f.peer.mu.Unlock()
	for _, key := range f.peer.values {
		if strings.Contains(res.Body.String(), key) {
			t.Fatal("machine response transported plaintext inference Key")
		}
	}
	return res
}

func testAPIKeyDeliveryController(t *testing.T, db *gorm.DB) {
	f := newDeliveryFixture(t, db)
	f.configure()
	f.catalog()
	base := "/api/v1/keys"
	target := f.ready(f.admin, base, f.resolve(f.admin, base, "controller"))
	first := f.managed(f.admin, base, f.context(f.admin, base).ReviewETag, f.input(target), 201)
	if first.Delivery.Descriptor == nil {
		t.Fatal("controller requires real acknowledged descriptor")
	}
	slot := first.Delivery.Descriptor.SlotID
	adminBase := "/api/v1/admin/api-key-delivery"
	machineBase := "/api/v1/api-key-delivery/controller"
	review := deliveryFixtureDecode[service.APIKeyDeliveryControllerReviewView](t, f.request(f.admin, "GET", adminBase+"/slots/"+slot+"/controller-configuration", "", nil), 200)
	in := service.APIKeyDeliveryControllerEnrollmentInput{RequestID: f.uuid(), Name: "Controlled independent controller", ExpiresAt: time.Now().UTC().Add(24 * time.Hour).Truncate(time.Microsecond), OptIn: true, DescriptorRevision: review.Configuration.DescriptorRevision, ConfigurationDigest: review.Configuration.ConfigurationDigest, Reason: "Explicit exact-slot opt-in"}
	var enrolled apiKeyDeliveryControllerEnrollmentResponse
	t.Run("enrollment_opt_in_review_independent_permissions", func(t *testing.T) {
		bad := in
		bad.OptIn = false
		expectStatus(t, f.request(f.admin, "POST", adminBase+"/slots/"+slot+"/controllers", review.ReviewETag, bad), 400)
		expectStatus(t, f.request(f.admin, "POST", adminBase+"/slots/"+slot+"/controllers", strings.Repeat("b", 64), in), 409)
		if db.Where("role_id = ? AND permission = ?", "rol_admin", "secrets.write").Delete(&entity.RolePermission{}).Error != nil {
			t.Fatal("remove independent write authority")
		}
		expectStatus(t, f.request(f.admin, "POST", adminBase+"/slots/"+slot+"/controllers", review.ReviewETag, in), 403)
		expectStatus(t, f.request(f.admin, "GET", adminBase+"/slots/"+slot+"/controller-configuration", "", nil), 200)
		if db.Create(&entity.RolePermission{RoleID: "rol_admin", Permission: "secrets.write"}).Error != nil {
			t.Fatal("restore fixture permission")
		}
		enrolled = deliveryFixtureDecode[apiKeyDeliveryControllerEnrollmentResponse](t, f.request(f.admin, "POST", adminBase+"/slots/"+slot+"/controllers", review.ReviewETag, in), 201)
		if enrolled.Bearer == nil || !strings.HasPrefix(*enrolled.Bearer, "rxdctl_") || enrolled.SecretStatus != "display_once" || enrolled.Controller.ConfigurationStatus != "assigned" {
			t.Fatal("one-time independent enrollment missing")
		}
		var record entity.APIKeyDeliveryControllerGrant
		if db.Take(&record, "id = ?", enrolled.Controller.ID).Error != nil || strings.Contains(record.CredentialDigest, *enrolled.Bearer) || len(record.CredentialDigest) != 64 || record.SlotID != slot || record.OwnerID != f.admin.Session.User.ID {
			t.Fatal("controller not stored as exact digest-only scope")
		}
		replay := deliveryFixtureDecode[apiKeyDeliveryControllerEnrollmentResponse](t, f.request(f.admin, "POST", adminBase+"/slots/"+slot+"/controllers", review.ReviewETag, in), 200)
		if replay.Bearer != nil || replay.SecretStatus != "not_recoverable" || replay.Controller.ID != enrolled.Controller.ID {
			t.Fatal("uncertain enrollment retry re-displayed credential")
		}
		changed := in
		changed.Name = "Changed intent"
		expectStatus(t, f.request(f.admin, "POST", adminBase+"/slots/"+slot+"/controllers", review.ReviewETag, changed), 409)
	})
	if enrolled.Bearer == nil {
		return
	}
	bearer := *enrolled.Bearer
	grantPath := adminBase + "/controllers/" + enrolled.Controller.ID
	writes, reads, metadata := f.peer.counts()
	t.Run("machine_pull_and_ack_are_not_inference_or_retirement", func(t *testing.T) {
		expectStatus(t, f.request(f.admin, "GET", machineBase+"/configuration", "", nil), 401)
		expectStatus(t, deliveryControllerMachineRequest(t, f, f.peer.bearer(t, first.Key.ID), "GET", machineBase+"/configuration", nil), 401)
		configuration := deliveryFixtureDecode[service.APIKeyDeliveryControllerConfiguration](t, deliveryControllerMachineRequest(t, f, bearer, "GET", machineBase+"/configuration", nil), 200)
		if !reflect.DeepEqual(configuration, review.Configuration) || strings.Contains(configuration.AgentConfiguration, "sink {") {
			t.Fatal("machine received different or overprivileged configuration")
		}
		ack := service.APIKeyDeliveryControllerAcknowledgementInput{RequestID: f.uuid(), DescriptorRevision: configuration.DescriptorRevision, ConfigurationDigest: configuration.ConfigurationDigest, AgentState: "running", OutputState: "written"}
		result := deliveryFixtureDecode[service.APIKeyDeliveryControllerAcknowledgementView](t, deliveryControllerMachineRequest(t, f, bearer, "POST", machineBase+"/acknowledgements", ack), 200)
		if result.EvidenceKind != "controller_report" {
			t.Fatal("controller report promoted to server proof")
		}
		replay := deliveryFixtureDecode[service.APIKeyDeliveryControllerAcknowledgementView](t, deliveryControllerMachineRequest(t, f, bearer, "POST", machineBase+"/acknowledgements", ack), 200)
		if !reflect.DeepEqual(result, replay) {
			t.Fatal("UUID retry changed original report")
		}
		changed := ack
		changed.OutputState = "failed"
		expectStatus(t, deliveryControllerMachineRequest(t, f, bearer, "POST", machineBase+"/acknowledgements", changed), 409)
		view := deliveryFixtureDecode[service.APIKeyDeliveryControllerView](t, f.request(f.admin, "GET", grantPath, "", nil), 200)
		if view.LastAcknowledgement == nil || !reflect.DeepEqual(*view.LastAcknowledgement, result) {
			t.Fatal("exact retained report not projected")
		}
		var calls int64
		if db.Model(&entity.CallRecord{}).Count(&calls).Error != nil || calls != 0 {
			t.Fatal("controller pull or output report caused inference")
		}
		var key entity.APIKey
		if db.Take(&key, "id = ?", first.Key.ID).Error != nil || key.Status != entity.KeyActive {
			t.Fatal("controller report changed Key lifecycle")
		}
	})
	t.Run("exact_scope_birth_fences_and_bounded_expiry", func(t *testing.T) {
		var g entity.APIKeyDeliveryControllerGrant
		if db.Take(&g, "id = ?", enrolled.Controller.ID).Error != nil {
			t.Fatal("read controller fixture")
		}
		for _, field := range []string{"owner_birth", "slot_birth", "profile_birth", "principal_birth", "reviewed_binding_birth", "reviewed_key_birth"} {
			if db.Model(&entity.APIKeyDeliveryControllerGrant{}).Where("id = ?", g.ID).Update(field, g.CreatedAt.Add(-time.Hour)).Error != nil {
				t.Fatal("construct stale exact birth")
			}
			want := 409
			if field == "owner_birth" {
				want = 503
			}
			expectStatus(t, deliveryControllerMachineRequest(t, f, bearer, "GET", machineBase+"/configuration", nil), want)
			if db.Save(&g).Error != nil {
				t.Fatal("restore controller fixture")
			}
		}
		if db.Model(&entity.APIKeyDeliveryControllerGrant{}).Where("id = ?", g.ID).Update("expires_at", g.CreatedAt.Add(time.Microsecond)).Error != nil {
			t.Fatal("construct expired controller")
		}
		expectStatus(t, deliveryControllerMachineRequest(t, f, bearer, "GET", machineBase+"/configuration", nil), 401)
		if db.Save(&g).Error != nil {
			t.Fatal("restore expiry")
		}
	})
	t.Run("new_slot_revision_requires_explicit_reviewed_assignment", func(t *testing.T) {
		target = deliveryFixtureDecode[service.APIKeyDeliveryTargetView](t, f.request(f.admin, "GET", base+"/delivery-targets/"+target.ID, "", nil), 200)
		rotation := f.input(target)
		rotation.Name = ""
		rotation.ModelIDs = []string{}
		replacement := deliveryFixtureDecode[deliveryFixtureManaged](t, f.request(f.admin, "POST", base+"/"+first.Key.ID+"/managed-rotation", f.context(f.admin, base).ReviewETag, rotation), 201)
		if replacement.Delivery.Descriptor == nil || replacement.Delivery.Descriptor.SlotID != slot {
			t.Fatal("replacement lost immutable delivery slot")
		}
		expectStatus(t, deliveryControllerMachineRequest(t, f, bearer, "GET", machineBase+"/configuration", nil), 409)
		view := deliveryFixtureDecode[service.APIKeyDeliveryControllerView](t, f.request(f.admin, "GET", grantPath, "", nil), 200)
		if view.ConfigurationStatus != "review_required" || view.CurrentConfiguration == nil || view.CurrentConfiguration.KeyID != replacement.Key.ID {
			t.Fatal("current replacement revision not explicitly reviewable")
		}
		assignment := service.APIKeyDeliveryControllerAssignmentInput{RequestID: f.uuid(), OptIn: true, DescriptorRevision: view.CurrentConfiguration.DescriptorRevision, ConfigurationDigest: view.CurrentConfiguration.ConfigurationDigest, Reason: "Review concrete replacement configuration"}
		expectStatus(t, f.request(f.admin, "PUT", grantPath+"/configuration", enrolled.Controller.ReviewETag, assignment), 409)
		assigned := deliveryFixtureDecode[service.APIKeyDeliveryControllerView](t, f.request(f.admin, "PUT", grantPath+"/configuration", view.ReviewETag, assignment), 200)
		if assigned.ConfigurationStatus != "assigned" {
			t.Fatal("reviewed assignment unavailable")
		}
		expectStatus(t, f.request(f.admin, "PUT", grantPath+"/configuration", view.ReviewETag, assignment), 200)
		configuration := deliveryFixtureDecode[service.APIKeyDeliveryControllerConfiguration](t, deliveryControllerMachineRequest(t, f, bearer, "GET", machineBase+"/configuration", nil), 200)
		if configuration.KeyID != replacement.Key.ID || configuration.KeyFile != review.Configuration.KeyFile || configuration.ConfigurationDigest == review.Configuration.ConfigurationDigest {
			t.Fatal("explicit assignment drifted slot or omitted revision")
		}
		stale := service.APIKeyDeliveryControllerAcknowledgementInput{RequestID: f.uuid(), DescriptorRevision: review.Configuration.DescriptorRevision, ConfigurationDigest: review.Configuration.ConfigurationDigest, AgentState: "running", OutputState: "written"}
		expectStatus(t, deliveryControllerMachineRequest(t, f, bearer, "POST", machineBase+"/acknowledgements", stale), 409)
		receipt := f.receipt(f.admin, base, rotation.RequestID)
		if receipt.ReplacementCallStatus != "not_recorded" {
			t.Fatal("configuration assignment fabricated native completion")
		}
	})
	// Rotation performs one explicit Writer operation. Controller management,
	// pull and reports never contact Vault or perform additional dispatch.
	afterWrites, afterReads, afterMetadata := f.peer.counts()
	if afterWrites != writes+1 || afterReads != reads || afterMetadata != metadata {
		t.Fatal("controller operations performed Vault I/O")
	}
	t.Run("revocation_stops_pull_and_identical_intent_replays", func(t *testing.T) {
		view := deliveryFixtureDecode[service.APIKeyDeliveryControllerView](t, f.request(f.admin, "GET", grantPath, "", nil), 200)
		body := map[string]string{"reason": "Explicit controller retirement"}
		revoked := deliveryFixtureDecode[service.APIKeyDeliveryControllerView](t, f.request(f.admin, "POST", grantPath+"/revoke", view.ReviewETag, body), 200)
		if revoked.RevokedAt == nil || revoked.ConfigurationStatus != "revoked" || revoked.CurrentConfiguration != nil {
			t.Fatal("revoked grant still projects callable configuration")
		}
		expectStatus(t, f.request(f.admin, "POST", grantPath+"/revoke", view.ReviewETag, body), 200)
		expectStatus(t, deliveryControllerMachineRequest(t, f, bearer, "GET", machineBase+"/configuration", nil), 401)
		ack := service.APIKeyDeliveryControllerAcknowledgementInput{RequestID: f.uuid(), DescriptorRevision: review.Configuration.DescriptorRevision, ConfigurationDigest: review.Configuration.ConfigurationDigest, AgentState: "running", OutputState: "written"}
		expectStatus(t, deliveryControllerMachineRequest(t, f, bearer, "POST", machineBase+"/acknowledgements", ack), 401)
	})
	t.Run("Project_scope_is_exact_and_revocable_after_owner_disable", func(t *testing.T) {
		project := deliveryFixtureDecode[ProjectResponse](t, f.request(f.admin, "POST", "/api/v1/projects", "", map[string]any{"name": "Controller Project"}), 201)
		projectPath := "/api/v1/projects/" + project.ID
		expectStatus(t, f.request(f.admin, "PUT", projectPath+"/managers", "", map[string]any{"user_ids": []string{f.admin.Session.User.ID}}), 200)
		expectStatus(t, f.request(f.admin, "PUT", projectPath+"/models", "", map[string]any{"model_ids": []string{deliveryFixtureModel}}), 200)
		projectBase := projectPath + "/keys"
		target := f.ready(f.admin, projectBase, f.resolve(f.admin, projectBase, "controller-project"))
		managed := f.managed(f.admin, projectBase, f.context(f.admin, projectBase).ReviewETag, f.input(target), 201)
		if managed.Delivery.Descriptor == nil {
			t.Fatal("Project descriptor missing")
		}
		slot := managed.Delivery.Descriptor.SlotID
		review := deliveryFixtureDecode[service.APIKeyDeliveryControllerReviewView](t, f.request(f.admin, "GET", adminBase+"/slots/"+slot+"/controller-configuration", "", nil), 200)
		in := service.APIKeyDeliveryControllerEnrollmentInput{RequestID: f.uuid(), Name: "Project controller", ExpiresAt: time.Now().UTC().Add(time.Hour * 24).Truncate(time.Microsecond), OptIn: true, DescriptorRevision: review.Configuration.DescriptorRevision, ConfigurationDigest: review.Configuration.ConfigurationDigest, Reason: "Project exact-slot opt-in"}
		enrolled := deliveryFixtureDecode[apiKeyDeliveryControllerEnrollmentResponse](t, f.request(f.admin, "POST", adminBase+"/slots/"+slot+"/controllers", review.ReviewETag, in), 201)
		if enrolled.Bearer == nil || enrolled.Controller.OwnerKind != "project" || enrolled.Controller.OwnerID != project.ID {
			t.Fatal("Project controller borrowed personal scope")
		}
		bearer := *enrolled.Bearer
		configuration := deliveryFixtureDecode[service.APIKeyDeliveryControllerConfiguration](t, deliveryControllerMachineRequest(t, f, bearer, "GET", machineBase+"/configuration", nil), 200)
		if configuration.KeyID != managed.Key.ID || configuration.KeyID == first.Key.ID || configuration.KeyFile == review.Configuration.JWTFile {
			t.Fatal("Project controller borrowed personal output")
		}
		if db.Model(&entity.Project{}).Where("id = ?", project.ID).Update("status", entity.ResourceDisabled).Error != nil {
			t.Fatal("construct disabled Project owner")
		}
		expectStatus(t, deliveryControllerMachineRequest(t, f, bearer, "GET", machineBase+"/configuration", nil), 409)
		grantPath := adminBase + "/controllers/" + enrolled.Controller.ID
		view := deliveryFixtureDecode[service.APIKeyDeliveryControllerView](t, f.request(f.admin, "GET", grantPath, "", nil), 200)
		if view.CurrentConfiguration != nil || view.ConfigurationStatus != "unavailable" {
			t.Fatal("disabled owner exposed current configuration")
		}
		revoked := deliveryFixtureDecode[service.APIKeyDeliveryControllerView](t, f.request(f.admin, "POST", grantPath+"/revoke", view.ReviewETag, map[string]string{"reason": "Revoke disabled-owner controller"}), 200)
		if revoked.RevokedAt == nil {
			t.Fatal("owner lifecycle prevented grant revocation")
		}
		expectStatus(t, deliveryControllerMachineRequest(t, f, bearer, "GET", machineBase+"/configuration", nil), 401)
	})

}
