package service

import (
	"fmt"
	"testing"
	"time"
)

func TestGatewayAttemptHealthCooldownAndRecovery(t *testing.T) {
	now := time.Date(2026, 9, 29, 1, 2, 3, 0, time.UTC)
	svc := &Service{attemptNow: func() time.Time { return now }}
	if !svc.gatewayAttemptHealthy("con_one", "crd_one") {
		t.Fatal("new route started unhealthy")
	}
	svc.markGatewayConnectionFailure("con_one")
	if svc.gatewayAttemptHealthy("con_one", "crd_two") {
		t.Fatal("connection cooldown did not cover its credentials")
	}
	if !svc.gatewayAttemptHealthy("con_two", "crd_one") {
		t.Fatal("connection cooldown escaped its connection")
	}
	now = now.Add(gatewayConnectionCooldown)
	if !svc.gatewayAttemptHealthy("con_one", "crd_one") {
		t.Fatal("expired connection cooldown was not pruned")
	}
	svc.markGatewayCredentialRejected("crd_one")
	if svc.gatewayAttemptHealthy("con_one", "crd_one") || !svc.gatewayAttemptHealthy("con_one", "crd_two") {
		t.Fatal("credential cooldown did not remain credential-scoped")
	}
	svc.markGatewayAttemptSuccess("con_one", "crd_one")
	if !svc.gatewayAttemptHealthy("con_one", "crd_one") {
		t.Fatal("success did not clear health cooldowns")
	}
}

func TestGatewayAttemptHealthStorageIsBounded(t *testing.T) {
	now := time.Date(2026, 9, 29, 1, 2, 3, 0, time.UTC)
	svc := &Service{attemptNow: func() time.Time { return now }}
	for index := 0; index < maxGatewayHealthEntries+10; index++ {
		svc.markGatewayCredentialRejected(fmt.Sprintf("crd_%04d", index))
	}
	if len(svc.attemptHealth.credentials) != maxGatewayHealthEntries {
		t.Fatalf("credential health entries = %d, want %d", len(svc.attemptHealth.credentials), maxGatewayHealthEntries)
	}
}
