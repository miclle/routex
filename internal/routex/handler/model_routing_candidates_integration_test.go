package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

// Extends the registered catalog lifecycle; it is not a new or implicitly executed scenario.
func testModelRoutingCandidates(t *testing.T, db *gorm.DB, svc *service.Service, router *fox.Engine, actor string, baseURL, modelID string, request func(string, string, any) *httptest.ResponseRecorder) {
	t.Helper()
	path := "/api/v1/admin/models/" + modelID
	body := map[string]any{"name": "Routing candidate supplier", "connection_name": "Recorded candidate Connection", "base_url": baseURL + "/v1", "protocol": "openai_chat", "credential_name": "Candidate credential", "secret": "catalog-test-secret"}
	provider := decodeCatalogResponse[ProviderResponse](t, request("POST", "/api/v1/admin/providers", body), 201)
	c := provider.Connections[0]
	credential := c.Credentials[0]
	verification := decodeCatalogResponse[VerifyCredentialResponse](t, request("POST", "/api/v1/admin/credentials/"+credential.ID+"/verify", map[string]any{}), 200)
	if !verification.Verified {
		t.Fatal("controlled candidate verification failed")
	}
	decodeCatalogResponse[CredentialResponse](t, request("PATCH", "/api/v1/admin/credentials/"+credential.ID, map[string]bool{"enabled": true}), 200)
	providers := decodeCatalogResponse[service.ModelRoutingProviderPage](t, request("GET", path+"/routing-providers?protocol=openai_chat&q=Routing%20candidate", nil), 200)
	if len(providers.Items) != 1 || providers.Items[0].ID != provider.ID || providers.Items[0].Name != provider.Name {
		t.Fatal("resource-scoped compatible Provider page lost exact recorded identity")
	}
	candidatesPath := path + "/routing-candidates?protocol=openai_chat&provider_id=" + provider.ID
	page := decodeCatalogResponse[service.ModelRoutingCandidatePage](t, request("GET", candidatesPath, nil), 200)
	if len(page.Items) != 2 || page.NextCursor != nil {
		t.Fatal("bounded exact case-distinct supply missing")
	}
	for _, row := range page.Items {
		if row.ProviderID != provider.ID || row.ConnectionID != c.ID || row.ConnectionName != c.Name || !row.ConfiguredAvailable || !row.VerificationCovered || !row.Selectable {
			t.Fatal("candidate inferred/omitted recorded eligibility")
		}
	}
	// Case and protocol aliases must not borrow the saved target under either database collation.
	expectStatus(t, request("GET", strings.Replace(candidatesPath, provider.ID, strings.ToUpper(provider.ID), 1), nil), 400)
	expectStatus(t, request("GET", path+"/routing-providers?protocol=OPENAI_CHAT", nil), 400)
	before := decodeCatalogResponse[ModelResponse](t, request("GET", path, nil), 200)
	var beforeModel entity.Model
	if err := db.Take(&beforeModel, "id = ?", modelID).Error; err != nil {
		t.Fatal(err)
	}
	first := page.Items[0]
	var pm entity.ProviderModel
	if err := db.First(&pm, "id = ?", first.ID).Error; err != nil {
		t.Fatal(err)
	}
	decodeCatalogResponse[ProviderModelResponse](t, request("PATCH", "/api/v1/admin/provider-models/"+pm.ID, map[string]any{"etag": pm.ETag, "enabled": false}), 200)
	intent := map[string]string{"provider_model_id": first.ID, "protocol": "openai_chat", "review_etag": first.ReviewETag}
	expectStatus(t, request("POST", path+"/bindings", intent), 409)
	if err := db.First(&pm, "id = ?", first.ID).Error; err != nil {
		t.Fatal(err)
	}
	decodeCatalogResponse[ProviderModelResponse](t, request("PATCH", "/api/v1/admin/provider-models/"+pm.ID, map[string]any{"etag": pm.ETag, "enabled": true}), 200)
	page = decodeCatalogResponse[service.ModelRoutingCandidatePage](t, request("GET", candidatesPath, nil), 200)
	first = page.Items[0]
	intent["review_etag"] = first.ReviewETag
	// Read independently of write; write without either read authority cannot use the new branch.
	_, readCookie, _ := createSystemStatusMember(t, svc, router, actor, "routing-read", []string{"models.read_all", "providers.read"})
	read := decodeCatalogResponse[service.ModelRoutingCandidatePage](t, identityRequest(router, "GET", candidatesPath, "", readCookie, ""), 200)
	if read.Items[0].Prices != nil {
		t.Fatal("prices borrowed from administrative actor")
	}
	_, writeCookie, csrf := createSystemStatusMember(t, svc, router, actor, "routing-write", []string{"models.write"})
	encoded, _ := json.Marshal(intent)
	expectStatus(t, identityRequest(router, "POST", path+"/bindings", string(encoded), writeCookie, csrf), 403)
	_, modelCookie, _ := createSystemStatusMember(t, svc, router, actor, "routing-model-read", []string{"models.read_all"})
	detail := decodeCatalogResponse[ModelResponse](t, identityRequest(router, "GET", path, "", modelCookie, ""), 200)
	for _, b := range detail.Bindings {
		if b.Supply != nil {
			t.Fatal("Provider facts exposed without independent read")
		}
	}
	expectStatus(t, identityRequest(router, "GET", candidatesPath, "", modelCookie, ""), 403)
	// A captured Chat supply cannot be inserted into another protocol.
	intent["protocol"] = "openai_responses"
	expectStatus(t, request("POST", path+"/bindings", intent), 409)
	intent["protocol"] = "openai_chat"
	var rejectedModel entity.Model
	if err := db.Take(&rejectedModel, "id = ?", modelID).Error; err != nil || !reflect.DeepEqual(rejectedModel, beforeModel) {
		t.Fatal("candidate reads/rejected writes changed Model configuration", err)
	}
	inserted := decodeCatalogResponse[ModelResponse](t, request("POST", path+"/bindings", intent), 201)
	var recordedModel entity.Model
	if err := db.Take(&recordedModel, "id = ?", modelID).Error; err != nil {
		t.Fatal(err)
	}
	if recordedModel.ConfigUpdatedAt == nil || recordedModel.ConfigUpdatedAt.IsZero() || (beforeModel.ConfigUpdatedAt != nil && !recordedModel.ConfigUpdatedAt.After(*beforeModel.ConfigUpdatedAt)) {
		t.Fatal("reviewed binding did not advance recorded Model configuration time")
	}
	expected := beforeModel
	expected.ConfigUpdatedAt = recordedModel.ConfigUpdatedAt
	if !reflect.DeepEqual(recordedModel, expected) {
		t.Fatal("reviewed binding changed unrelated Model facts")
	}
	if len(inserted.Bindings) != len(before.Bindings)+1 {
		t.Fatal("reviewed insertion did not add exactly one relation")
	}
	for _, old := range before.Bindings {
		found := false
		for _, now := range inserted.Bindings {
			if old.ID == now.ID {
				found = true
				if old.Weight != now.Weight {
					t.Fatal("insertion silently rewrote grouped weights")
				}
			}
		}
		if !found {
			t.Fatal("insertion discarded retained relation")
		}
	}
	found := false
	for _, b := range inserted.Bindings {
		if b.ProviderModelID == first.ID {
			found = true
			if b.Weight != 0 || b.Protocol != "openai_chat" || b.Supply == nil || b.Supply.ConnectionName != c.Name {
				t.Fatal("insertion did not retain zero weight and recorded source")
			}
		}
	}
	if !found {
		t.Fatal("new exact binding missing")
	}
	expectStatus(t, request("POST", path+"/bindings", intent), 409)
	other := page.Items[1]
	intent["provider_model_id"] = other.ID
	intent["review_etag"] = other.ReviewETag
	expectStatus(t, request("POST", path+"/bindings", intent), 409)
	var afterRejected entity.Model
	if err := db.Take(&afterRejected, "id = ?", modelID).Error; err != nil || !reflect.DeepEqual(afterRejected, recordedModel) {
		t.Fatal("duplicate/no-op changed recorded Model configuration", err)
	}
	duplicatePage := decodeCatalogResponse[service.ModelRoutingCandidatePage](t, request("GET", candidatesPath, nil), 200)
	if len(duplicatePage.Items) != 0 {
		t.Fatal("retained zero-weight Provider did not exclude same-protocol duplicates")
	}
	// Rejected operations do not add hidden relations; no inference or price write is performed here.
	var count int64
	if err := db.Model(&entity.ModelProviderBinding{}).Where("model_id = ?", modelID).Count(&count).Error; err != nil || count != int64(len(inserted.Bindings)) {
		t.Fatal("rejected candidate dispatch changed topology")
	}
	if _, err := svc.GetAdminModel(context.Background(), actor, modelID); err != nil {
		t.Fatal("post-insertion exact resource read failed", err)
	}
}
