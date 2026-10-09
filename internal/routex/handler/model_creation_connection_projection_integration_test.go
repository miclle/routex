package handler

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

// Called by the existing isolated dual-driver lifecycle after its original
// write/native/history assertions. No additional registry or inference request.
func assertModelCreationConnectionTransportProjection(t *testing.T, db *gorm.DB, read func(string) *httptest.ResponseRecorder) {
	t.Helper()
	version := "2024-10-21"
	rows := []entity.ProviderConnection{
		{ID: "con_projection_chat", Name: "transport-projection-chat", Protocol: entity.ProtocolOpenAIChat, Adapter: entity.AdapterNative, BaseURL: "https://projection.example.invalid/v1"},
		{ID: "con_projection_responses", Name: "transport-projection-responses", Protocol: entity.ProtocolOpenAIResponses, Adapter: entity.AdapterNative, BaseURL: "https://projection.example.invalid/v1"},
		{ID: "con_projection_messages", Name: "transport-projection-messages", Protocol: entity.ProtocolAnthropicMessages, Adapter: entity.AdapterNative, BaseURL: "https://projection.example.invalid/v1"},
		{ID: "con_projection_gemini", Name: "transport-projection-gemini", Protocol: entity.ProtocolGeminiGenerateContent, Adapter: entity.AdapterNative, BaseURL: "https://projection.example.invalid/v1beta"},
		{ID: "con_projection_azure", Name: "transport-projection-azure", Protocol: entity.ProtocolOpenAIChat, Adapter: entity.AdapterAzureOpenAIClassic, APIVersion: &version, BaseURL: "https://projection.openai.azure.com"},
	}
	want := map[string]service.ModelCreationConnection{}
	for i := range rows {
		row := &rows[i]
		row.ProviderID = "prv_batch_main"
		row.EgressMode = "direct"
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
		want[row.ID] = service.ModelCreationConnection{ID: row.ID, ProviderID: row.ProviderID, ProviderName: "Batch supplier", Name: row.Name, Protocol: row.Protocol, Adapter: row.Adapter, APIVersion: row.APIVersion, BaseURL: row.BaseURL}
	}
	seen := map[string]bool{}
	path := "/api/v1/admin/model-creation/connections?q=transport-projection&limit=2"
	for pageNumber := 0; pageNumber < 3; pageNumber++ {
		response := read(path)
		page := decodeCatalogResponse[service.ModelCreationConnectionPage](t, response, 200)
		if response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" || len(page.Items) == 0 || len(page.Items) > 2 {
			t.Fatal("bounded private connection projection", response.Header(), len(page.Items))
		}
		var wire struct {
			Items []map[string]json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &wire); err != nil || len(wire.Items) != len(page.Items) {
			t.Fatal("connection projection JSON", err)
		}
		for i, item := range page.Items {
			expected, exists := want[item.ID]
			if !exists || seen[item.ID] || !reflect.DeepEqual(item, expected) || len(wire.Items[i]) != 8 {
				t.Fatal("exact stored adapter/version projection missing or changed", item, expected)
			}
			seen[item.ID] = true
			contextResponse := read("/api/v1/admin/connections/" + item.ID + "/model-creation")
			current := decodeCatalogResponse[service.ModelCreationContext](t, contextResponse, 200)
			if !reflect.DeepEqual(current.Connection, expected) || current.CanCreate || contextResponse.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("read-only context differs from picker transport", current)
			}
		}
		if page.NextCursor == nil {
			if len(seen) != len(want) {
				t.Fatal("transport projection omitted retained connection", len(seen))
			}
			return
		}
		path = "/api/v1/admin/model-creation/connections?q=transport-projection&limit=2&cursor=" + *page.NextCursor
	}
	t.Fatal("transport projection exceeded exact three-page bound")
}
