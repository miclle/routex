package service

import (
	"reflect"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestTelegramRootInventoryPreservesHistoricalCoverage(t *testing.T) {
	if rootInventoryVersion != 11 || len(rootDomains) != 13 || rootDomains[10] != "named_identity_providers" || rootDomains[11] != "api_key_delivery_auth" {
		t.Fatal("Telegram must reuse the twelve-domain envelope")
	}
	for _, tc := range []struct{ version, count int }{{6, 11}, {7, 11}, {8, 11}, {9, 12}, {10, 12}} {
		if got := rootInventoryDomains(tc.version); len(got) != tc.count || !reflect.DeepEqual(got, rootDomains[:tc.count]) {
			t.Fatal("historical root domain scope changed", tc.version)
		}
	}
	if rootInventoryDomains(12) != nil {
		t.Fatal("unknown future inventory admitted")
	}
	now := time.Now().UTC()
	start := now.Add(-300 * time.Second)
	svc := &Service{rootNow: func() time.Time { return now }}
	old := entity.SecretRotationJob{InventoryVersion: 9, Domain: 12, Status: "ready", CountsJSON: "{}", ObservationStartedAt: &start, ObservationLastConfirmedAt: &now, VerifiedProcessID: "ins_old", VerifiedSnapshotID: "cfg_old"}
	proof := entity.SecretProcessVerification{InventoryVersion: 9, ProcessID: "ins_old", RuntimeSnapshotID: "cfg_old"}
	if svc.rootObservationEligible(old, proof) {
		t.Fatal("historical v9 proof acquired Telegram coverage")
	}
	if _, err := rootCountsChecked(old); err != nil {
		t.Fatal("historical v9 history unreadable", err)
	}
}
