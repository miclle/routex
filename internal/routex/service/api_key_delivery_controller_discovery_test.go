package service

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestAPIKeyDeliveryDiscoveryCursor(t *testing.T) {
	birth := time.Date(2026, 10, 14, 1, 2, 3, 456000, time.UTC)
	parent := "kds_01arz3ndektsv4rrffq69g5fav"
	after := "kcg_01arz3ndektsv4rrffq69g5fav"
	cursor := apiKeyDeliveryDiscoveryEncode("controllers", parent, birth, after, birth)
	got, err := apiKeyDeliveryDiscoveryAfter(cursor, "controllers", parent, birth, "kcg")
	if err != nil || got.After != after {
		t.Fatal("exact parent birth cursor rejected")
	}
	for name, fn := range map[string]func() (apiKeyDeliveryDiscoveryCursor, error){
		"other_parent": func() (apiKeyDeliveryDiscoveryCursor, error) {
			return apiKeyDeliveryDiscoveryAfter(cursor, "controllers", parent+"x", birth, "kcg")
		},
		"recreated_parent": func() (apiKeyDeliveryDiscoveryCursor, error) {
			return apiKeyDeliveryDiscoveryAfter(cursor, "controllers", parent, birth.Add(time.Microsecond), "kcg")
		},
		"other_collection": func() (apiKeyDeliveryDiscoveryCursor, error) {
			return apiKeyDeliveryDiscoveryAfter(cursor, "slots", parent, birth, "kcg")
		},
		"wrong_child_kind": func() (apiKeyDeliveryDiscoveryCursor, error) {
			return apiKeyDeliveryDiscoveryAfter(cursor, "controllers", parent, birth, "kds")
		},
		"padding": func() (apiKeyDeliveryDiscoveryCursor, error) {
			return apiKeyDeliveryDiscoveryAfter(cursor+"=", "controllers", parent, birth, "kcg")
		},
		"overflow": func() (apiKeyDeliveryDiscoveryCursor, error) {
			return apiKeyDeliveryDiscoveryAfter(strings.Repeat("a", 513), "controllers", parent, birth, "kcg")
		},
		"unknown_field": func() (apiKeyDeliveryDiscoveryCursor, error) {
			raw, _ := base64.RawURLEncoding.DecodeString(cursor)
			raw = append(raw[:len(raw)-1], []byte(`,"extra":true}`)...)
			return apiKeyDeliveryDiscoveryAfter(base64.RawURLEncoding.EncodeToString(raw), "controllers", parent, birth, "kcg")
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := fn(); err == nil {
				t.Fatal("unbound cursor accepted")
			}
		})
	}
}

func TestAPIKeyDeliveryDiscoveryLifecycleOnly(t *testing.T) {
	now := time.Date(2026, 10, 14, 1, 2, 3, 0, time.UTC)
	g := entity.APIKeyDeliveryControllerGrant{CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}
	for _, tc := range []struct {
		name   string
		grant  entity.APIKeyDeliveryControllerGrant
		status string
	}{
		{"live", g, "active"},
		{"expired", entity.APIKeyDeliveryControllerGrant{CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour)}, "expired"},
		{"revoked", entity.APIKeyDeliveryControllerGrant{CreatedAt: g.CreatedAt, ExpiresAt: g.ExpiresAt, RevokedAt: &now}, "revoked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := apiKeyDeliveryDiscoveryController(tc.grant, now)
			if view.LifecycleStatus != tc.status || view.ConfigurationStatus != "not_reviewed" {
				t.Fatal("discovery inferred reviewed configuration or wrong lifecycle")
			}
		})
	}
}
