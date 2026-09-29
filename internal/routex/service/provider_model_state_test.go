package service

import (
	"context"
	"testing"
	"time"
)

func TestProviderModelUpdateValidation(t *testing.T) {
	var svc Service
	for _, tc := range []struct {
		name  string
		etag  string
		input ProviderModelUpdate
	}{
		{name: "missing etag", input: ProviderModelUpdate{Enabled: new(bool)}},
		{name: "oversized etag", etag: string(make([]byte, 65)), input: ProviderModelUpdate{Enabled: new(bool)}},
		{name: "missing fields", etag: "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.UpdateProviderModel(context.Background(), "usr_one", "pmd_one", tc.etag, tc.input); err == nil {
				t.Fatal("invalid provider model update accepted")
			}
		})
	}
}

func TestDisabledProviderSupply(t *testing.T) {
	for range 100 {
		choice, err := chooseAvailableGatewayRoute([]int{80, 20, 0}, []bool{false, true, true})
		if err != nil || choice != 1 {
			t.Fatal("disabled or zero-weight supply selected", choice, err)
		}
	}
	for _, available := range [][]bool{{false, false}, {true}} {
		if _, err := chooseAvailableGatewayRoute([]int{70, 30}, available); err == nil {
			t.Fatal("unavailable or malformed supply accepted")
		}
	}
	svc, data, _ := runtimeFixture(t, "https://provider.example/v1")
	data.ProviderModels[0].Disabled = true
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	if _, _, err := svc.runtimeRoute("mdl_one"); err == nil {
		t.Fatal("old route snapshot bypassed new disabled authorization")
	}
	data.ProviderModels[0].Disabled = false
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	svc.runtime.deniedProviderModels.Store("pmd_one", uint64(1))
	if _, _, err := svc.runtimeRoute("mdl_one"); err == nil {
		t.Fatal("old authorization bypassed emergency supply denial")
	}
}
