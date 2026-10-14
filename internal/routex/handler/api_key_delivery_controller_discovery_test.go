package handler

import "testing"

func TestAPIKeyDeliveryDiscoveryQuery(t *testing.T) {
	for _, raw := range []string{"", "limit=1", "limit=20", "limit=50", "cursor=opaque&limit=2"} {
		t.Run("valid_"+raw, func(t *testing.T) {
			_, limit, err := apiKeyDeliveryDiscoveryQuery(raw, false)
			if err != nil || limit < 1 || limit > 50 {
				t.Fatal("valid bounded pagination rejected")
			}
		})
	}
	for _, raw := range []string{"limit=0", "limit=51", "limit=-1", "limit=01", "limit=+1", "limit=1&limit=2", "cursor=", "cursor", "q=x", "limit=1;cursor=x", "limit=1&", "%6cimit=1", "limit=1%00"} {
		t.Run("invalid_"+raw, func(t *testing.T) {
			if _, _, err := apiKeyDeliveryDiscoveryQuery(raw, false); err == nil {
				t.Fatal("noncanonical pagination accepted")
			}
		})
	}
	if _, _, err := apiKeyDeliveryDiscoveryQuery("", true); err == nil {
		t.Fatal("bare query marker accepted")
	}
}
