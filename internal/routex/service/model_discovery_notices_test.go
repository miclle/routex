package service

import (
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestModelDiscoveryNoticeClosedSourceAndCount(t *testing.T) {
	good := entity.ModelDiscoveryNotice{AuditID: "aud_01J00000000000000000000001", ConnectionID: "con_01J00000000000000000000001", ProviderID: "prv_01J00000000000000000000001", ProviderName: "Recorded supplier", ModelCount: 1, RecordedAt: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)}
	if !validModelDiscoveryNotice(good) || !notificationEventAllowed(notificationSourceModelDiscovery, notificationKindModelDiscovery, "low", "first_recorded") {
		t.Fatal("valid discovery rejected")
	}
	for _, change := range []func(*entity.ModelDiscoveryNotice){func(v *entity.ModelDiscoveryNotice) { v.ModelCount = 0 }, func(v *entity.ModelDiscoveryNotice) { v.ModelCount = 2001 }, func(v *entity.ModelDiscoveryNotice) { v.AuditID = "" }, func(v *entity.ModelDiscoveryNotice) { v.ConnectionID = "" }, func(v *entity.ModelDiscoveryNotice) { v.ProviderName = "invalid\nname" }, func(v *entity.ModelDiscoveryNotice) { v.RecordedAt = time.Time{} }} {
		bad := good
		change(&bad)
		if validModelDiscoveryNotice(bad) {
			t.Fatal("invalid discovery source admitted", bad)
		}
	}
	for _, row := range []struct{ source, kind, severity, detail string }{
		{notificationSourceModelDiscovery, notificationKindModelDiscovery, "medium", "first_recorded"},
		{notificationSourceModelDiscovery, notificationKindModelDiscovery, "high", "first_recorded"},
		{notificationSourceModelDiscovery, notificationKindModelDiscovery, "low", "FIRST_RECORDED"},
		{"MODEL_DISCOVERY", notificationKindModelDiscovery, "low", "first_recorded"},
		{notificationSourceCredentialVerification, notificationKindCredentialFailure, "low", "verification_failed"},
		{notificationSourceGatewayCall, notificationKindRouteUnavailable, "low", "no_candidates"},
	} {
		if notificationEventAllowed(row.source, row.kind, row.severity, row.detail) {
			t.Fatal("discovery widened another source", row)
		}
	}
	for _, count := range []int{-1, 0, 1, 2000, 2001} {
		if validDiscoveryCount(notificationKindModelDiscovery, count) != (count >= 1 && count <= 2000) {
			t.Fatal("discovery count boundary", count)
		}
		if validDiscoveryCount(notificationKindCredentialFailure, count) != (count == 0) {
			t.Fatal("borrowed discovery count", count)
		}
	}
}
