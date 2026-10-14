package handler

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

// Registered by the integrating owner only after the frozen dependency floor
// and shared real-driver registry are adopted together.
func testAPIKeyDeliveryControllerDiscovery(t *testing.T, db *gorm.DB) {
	f := newDeliveryFixture(t, db)
	f.configure()
	f.catalog()
	base := "/api/v1/keys"
	target := f.ready(f.admin, base, f.resolve(f.admin, base, "controller-discovery"))
	key := f.managed(f.admin, base, f.context(f.admin, base).ReviewETag, f.input(target), 201)
	if key.Delivery.Descriptor == nil {
		t.Fatal("real delivered slot required")
	}
	slot := key.Delivery.Descriptor.SlotID
	adminBase := "/api/v1/admin/api-key-delivery"
	slotsPath := adminBase + "/targets/" + key.Delivery.Descriptor.ProfileID + "/slots"
	grantsPath := adminBase + "/slots/" + slot + "/controllers"
	review := deliveryFixtureDecode[service.APIKeyDeliveryControllerReviewView](t, f.request(f.admin, "GET", adminBase+"/slots/"+slot+"/controller-configuration", "", nil), 200)
	inputs := make([]service.APIKeyDeliveryControllerEnrollmentInput, 2)
	enrolled := make([]apiKeyDeliveryControllerEnrollmentResponse, 2)
	for i := range inputs {
		inputs[i] = service.APIKeyDeliveryControllerEnrollmentInput{RequestID: f.uuid(), Name: "Discoverable controller", ExpiresAt: time.Now().UTC().Add(24 * time.Hour).Truncate(time.Microsecond), OptIn: true, DescriptorRevision: review.Configuration.DescriptorRevision, ConfigurationDigest: review.Configuration.ConfigurationDigest, Reason: "Explicit discovery fixture enrollment"}
		enrolled[i] = deliveryFixtureDecode[apiKeyDeliveryControllerEnrollmentResponse](t, f.request(f.admin, "POST", grantsPath, review.ReviewETag, inputs[i]), 201)
	}
	reader := f.member("controller-discovery-reader", []string{"secrets.read", "secrets.write"})
	t.Run("intrinsic_admin_not_replaced_by_permissions", func(t *testing.T) {
		expectStatus(t, f.request(reader, "GET", slotsPath, "", nil), 403)
		expectStatus(t, f.request(reader, "GET", grantsPath, "", nil), 403)
	})
	writes, reads, metadata := f.peer.counts()
	t.Run("resource_scoped_bounded_discovery", func(t *testing.T) {
		slots := deliveryFixtureDecode[service.APIKeyDeliverySlotList](t, f.request(f.admin, "GET", slotsPath, "", nil), 200)
		if len(slots.Items) != 1 || slots.Items[0].ID != slot || slots.Items[0].ConfigurationStatus != "not_reviewed" {
			t.Fatal("exact retained slot missing")
		}
		first := deliveryFixtureDecode[service.APIKeyDeliveryControllerList](t, f.request(f.admin, "GET", grantsPath+"?limit=1", "", nil), 200)
		if len(first.Items) != 1 || first.NextCursor == "" {
			t.Fatal("bounded first page missing")
		}
		second := deliveryFixtureDecode[service.APIKeyDeliveryControllerList](t, f.request(f.admin, "GET", grantsPath+"?cursor="+url.QueryEscape(first.NextCursor)+"&limit=1", "", nil), 200)
		if len(second.Items) != 1 || second.NextCursor != "" || first.Items[0].ID == second.Items[0].ID {
			t.Fatal("cursor duplicated or omitted retained grant")
		}
		// A retained cursor cannot borrow a newly born anchor with the same ID.
		anchor := first.Items[0]
		if db.Model(&entity.APIKeyDeliveryControllerGrant{}).Where("id = ?", anchor.ID).Update("created_at", anchor.Birth.Add(-time.Microsecond)).Error != nil {
			t.Fatal("change anchor birth")
		}
		expectStatus(t, f.request(f.admin, "GET", grantsPath+"?cursor="+url.QueryEscape(first.NextCursor)+"&limit=1", "", nil), 400)
		if db.Model(&entity.APIKeyDeliveryControllerGrant{}).Where("id = ?", anchor.ID).Update("created_at", anchor.Birth).Error != nil {
			t.Fatal("restore anchor birth")
		}
		expectStatus(t, f.request(f.admin, "GET", slotsPath+"?cursor="+url.QueryEscape(first.NextCursor), "", nil), 400)
		for _, suffix := range []string{"?q=x", "?limit=51", "?limit=1&limit=2", "?limit=01"} {
			expectStatus(t, f.request(f.admin, "GET", grantsPath+suffix, "", nil), 400)
		}
	})
	t.Run("original_actor_nonsecret_receipt", func(t *testing.T) {
		path := adminBase + "/slots/" + slot + "/controller-enrollments/" + inputs[0].RequestID
		res := f.request(f.admin, "GET", path, "", nil)
		receipt := deliveryFixtureDecode[service.APIKeyDeliveryControllerEnrollmentReceipt](t, res, 200)
		if receipt.Controller.ID != enrolled[0].Controller.ID || receipt.RequestID != inputs[0].RequestID || receipt.Bearer != nil || receipt.SecretStatus != "not_recoverable" || receipt.EnrollmentReview != review.ReviewETag || len(receipt.IntentDigest) != 64 {
			t.Fatal("retained enrollment identity or nonsecret receipt missing")
		}
		for _, v := range enrolled {
			if v.Bearer == nil || strings.Contains(res.Body.String(), *v.Bearer) {
				t.Fatal("receipt recovered machine credential")
			}
		}
		var grant entity.APIKeyDeliveryControllerGrant
		if db.Take(&grant, "id = ?", receipt.Controller.ID).Error != nil {
			t.Fatal("load exact enrollment")
		}
		birth := grant.CreatedByBirth
		if db.Model(&grant).Update("created_by_birth", birth.Add(time.Microsecond)).Error != nil {
			t.Fatal("change original actor birth")
		}
		expectStatus(t, f.request(f.admin, "GET", path, "", nil), 404)
		if db.Model(&grant).Update("created_by_birth", birth).Error != nil {
			t.Fatal("restore original actor birth")
		}
	})
	t.Run("independent_read_authority", func(t *testing.T) {
		if db.Where("role_id = ? AND permission = ?", "rol_admin", "secrets.read").Delete(&entity.RolePermission{}).Error != nil {
			t.Fatal("remove read authority")
		}
		expectStatus(t, f.request(f.admin, "GET", slotsPath, "", nil), 403)
		expectStatus(t, f.request(f.admin, "GET", grantsPath, "", nil), 403)
		if db.Create(&entity.RolePermission{RoleID: "rol_admin", Permission: "secrets.read"}).Error != nil {
			t.Fatal("restore read authority")
		}
	})
	t.Run("revoked_and_unavailable_remain_discoverable", func(t *testing.T) {
		detail := deliveryFixtureDecode[service.APIKeyDeliveryControllerView](t, f.request(f.admin, "GET", adminBase+"/controllers/"+enrolled[0].Controller.ID, "", nil), 200)
		expectStatus(t, f.request(f.admin, "POST", adminBase+"/controllers/"+detail.ID+"/revoke", detail.ReviewETag, apiKeyDeliveryReasonInput{Reason: "Explicit retained grant revocation"}), 200)
		if db.Model(&entity.VaultAccessProfile{}).Where("id = ?", key.Delivery.Descriptor.ProfileID).Updates(map[string]any{"enabled": false, "status": "pending"}).Error != nil {
			t.Fatal("disable target")
		}
		result := deliveryFixtureDecode[service.APIKeyDeliveryControllerList](t, f.request(f.admin, "GET", grantsPath, "", nil), 200)
		if len(result.Items) != 2 {
			t.Fatal("unavailable or revoked grant disappeared")
		}
		for _, v := range result.Items {
			if v.ID == detail.ID && v.LifecycleStatus != "revoked" {
				t.Fatal("revoked lifecycle missing")
			}
		}
		deliveryFixtureDecode[service.APIKeyDeliverySlotList](t, f.request(f.admin, "GET", slotsPath, "", nil), 200)
	})
	if w, r, m := f.peer.counts(); w != writes || r != reads || m != metadata {
		t.Fatal("discovery performed external Vault operation")
	}
	res := f.request(f.admin, "GET", grantsPath, "", nil)
	var raw map[string]any
	if json.Unmarshal(res.Body.Bytes(), &raw) != nil || res.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("private closed response missing")
	}
}
