package service

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func publishRuntimeFixture(t *testing.T, svc *Service, data *runtimeData) {
	t.Helper()
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	routes, err := svc.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_capabilities", Models: routes, PublishedAt: time.Now()})
}

func TestGatewayModelInputCapabilitiesIntersectReadyRoutes(t *testing.T) {
	svc, data, bearer := runtimeFixture(t, "https://example.com/v1")
	data.ProviderModels[0].SupportsImageInput = true
	data.ProviderModels[0].SupportsPDFInput = true
	publishRuntimeFixture(t, svc, data)

	models, err := svc.GatewayModels(context.Background(), bearer)
	if err != nil || len(models) != 1 {
		t.Fatalf("list models: %+v %v", models, err)
	}
	if !models[0].PersonalAttachments {
		t.Fatal("personal Key model did not advertise personal attachment references")
	}
	if models[0].AttachmentScope != entity.StorageOwnerUser || models[0].AttachmentProjectID != "" {
		t.Fatalf("personal attachment scope metadata = %q/%q", models[0].AttachmentScope, models[0].AttachmentProjectID)
	}
	protocol := entity.ProtocolOpenAIChat
	if !slices.Equal(models[0].InputCapabilities[protocol], []string{inputCapabilityImage, inputCapabilityPDF}) {
		t.Fatalf("single-route capabilities = %v, want image and pdf", models[0].InputCapabilities)
	}

	data.Bindings[0].Weight = 50
	data.ProviderModels = append(data.ProviderModels, entity.ProviderModel{
		CapabilityTransportGeneration: "0",
		ETag:                          "0",
		CreatedAt:                     time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		ID:                            "pmd_two",
		ConnectionID:                  "con_one",
		UpstreamName:                  "provider-model-two",
		SupportsImageInput:            true,
	})
	data.Access = append(data.Access, entity.CredentialModelAccess{CredentialID: "crd_one", ProviderModelID: "pmd_two"})
	data.Bindings = append(data.Bindings, entity.ModelProviderBinding{ID: "bnd_two", ModelID: "mdl_one", ProviderModelID: "pmd_two", Weight: 50})
	publishRuntimeFixture(t, svc, data)

	models, err = svc.GatewayModels(context.Background(), bearer)
	if err != nil || len(models) != 1 || !slices.Equal(models[0].Protocols, []string{protocol}) {
		t.Fatalf("weighted protocols changed: %+v %v", models, err)
	}
	if !slices.Equal(models[0].InputCapabilities[protocol], []string{inputCapabilityImage}) {
		t.Fatalf("weighted capability intersection = %v, want image", models[0].InputCapabilities)
	}

	data.Access = data.Access[:1]
	publishRuntimeFixture(t, svc, data)
	models, err = svc.GatewayModels(context.Background(), bearer)
	if err != nil || len(models) != 1 || !slices.Equal(models[0].InputCapabilities[protocol], []string{inputCapabilityImage, inputCapabilityPDF}) {
		t.Fatalf("unready route reduced capabilities: %+v %v", models, err)
	}

	data.ProviderModels = append(data.ProviderModels, entity.ProviderModel{
		CapabilityTransportGeneration: "0",
		ETag:                          "0",
		CreatedAt:                     time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		ID:                            "pmd_zero",
		ConnectionID:                  "con_one",
		UpstreamName:                  "provider-model-zero",
	})
	data.Access = append(data.Access, entity.CredentialModelAccess{CredentialID: "crd_one", ProviderModelID: "pmd_zero"})
	data.Bindings = append(data.Bindings, entity.ModelProviderBinding{ID: "bnd_zero", ModelID: "mdl_one", ProviderModelID: "pmd_zero"})
	publishRuntimeFixture(t, svc, data)
	models, err = svc.GatewayModels(context.Background(), bearer)
	if err != nil || len(models) != 1 || !slices.Equal(models[0].InputCapabilities[protocol], []string{inputCapabilityImage, inputCapabilityPDF}) {
		t.Fatalf("zero-weight route reduced capabilities: %+v %v", models, err)
	}

	data.ProviderModels[1].Disabled = true
	data.Access = append(data.Access, entity.CredentialModelAccess{CredentialID: "crd_one", ProviderModelID: "pmd_two"})
	publishRuntimeFixture(t, svc, data)
	models, err = svc.GatewayModels(context.Background(), bearer)
	if err != nil || len(models) != 1 || !slices.Equal(models[0].InputCapabilities[protocol], []string{inputCapabilityImage, inputCapabilityPDF}) {
		t.Fatalf("disabled route reduced capabilities: %+v %v", models, err)
	}
}

func TestRuntimeDigestIncludesProviderModelInputCapabilities(t *testing.T) {
	_, data, _ := runtimeFixture(t, "https://example.com/v1")
	before, err := runtimeDigest(data)
	if err != nil {
		t.Fatal(err)
	}
	data.ProviderModels[0].SupportsImageInput = true
	afterImage, err := runtimeDigest(data)
	if err != nil {
		t.Fatal(err)
	}
	data.ProviderModels[0].SupportsPDFInput = true
	afterPDF, err := runtimeDigest(data)
	if err != nil {
		t.Fatal(err)
	}
	if before == afterImage || afterImage == afterPDF {
		t.Fatal("provider model capability mutation was excluded from runtime digest")
	}
}
