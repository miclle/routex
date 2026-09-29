package service

import (
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestQuotaNativeCapacityAdapters(t *testing.T) {
	tests := []struct {
		protocol, body string
		valid, write   bool
	}{
		{entity.ProtocolOpenAIResponses, `{"input":"text","max_output_tokens":100,"store":false,"reasoning":{"effort":"high"}}`, true, true},
		{entity.ProtocolOpenAIResponses, `{"input":"text","max_output_tokens":100,"tools":[{"type":"function","name":"run","parameters":{"properties":{"file_id":{"type":"string"}}}}]}`, true, true},
		{entity.ProtocolOpenAIResponses, `{"input":"text","max_output_tokens":100,"tools":[{"type":"web_search"}]}`, false, false},
		{entity.ProtocolOpenAIResponses, `{"input":"text","max_completion_tokens":100}`, false, false},
		{entity.ProtocolAnthropicMessages, `{"messages":[{"role":"user","content":"hello"}],"max_tokens":100,"thinking":{"type":"enabled","budget_tokens":20}}`, true, true},
		{entity.ProtocolAnthropicMessages, `{"messages":[{"role":"user","content":"hello"}],"max_tokens":100,"cache_control":{"type":"ephemeral","ttl":"5m"}}`, true, true},
		{entity.ProtocolAnthropicMessages, `{"messages":[{"role":"user","content":"hello"}],"max_tokens":100,"cache_control":{"type":"ephemeral","ttl":"1h"}}`, false, false},
		{entity.ProtocolAnthropicMessages, `{"messages":[{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral","ttl":"1h"}}]}],"max_tokens":100}`, false, false},
		{entity.ProtocolAnthropicMessages, `{"messages":[{"role":"user","content":"hello"}],"max_tokens":100,"tools":[{"type":"web_search_20250305"}]}`, false, false},
		{entity.ProtocolGeminiGenerateContent, `{"contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":100,"candidateCount":1,"thinkingConfig":{"thinkingBudget":-1}}}`, true, false},
		{entity.ProtocolGeminiGenerateContent, `{"contents":[{"parts":[{"text":"hello"}]}],"generation_config":{"max_output_tokens":100}}`, true, false},
		{entity.ProtocolGeminiGenerateContent, `{"contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":100,"candidateCount":2}}`, false, false},
		{entity.ProtocolGeminiGenerateContent, `{"contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":100,"max_output_tokens":100}}`, false, false},
		{entity.ProtocolGeminiGenerateContent, `{"contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":100},"tools":[{"googleSearch":{}}]}`, false, false},
	}
	for _, test := range tests {
		result := inspectQuotaRequest(test.protocol, quotaPayload(t, test.body))
		if result.Supported != test.valid || test.valid && (result.MaxOutput != 100 || result.CacheWrite != test.write) {
			t.Fatalf("%s %s => %+v", test.protocol, test.body, result)
		}
	}
}

func TestQuotaNativeAuthenticatedAttachmentsUseCapacityBounds(t *testing.T) {
	image := gatewayAttachmentReferencePrefix + testAttachmentImageID
	pdf := gatewayAttachmentReferencePrefix + testAttachmentPDFID
	tests := []struct {
		name       string
		protocol   string
		body       string
		cacheWrite bool
	}{
		{
			name:       "chat",
			protocol:   entity.ProtocolOpenAIChat,
			body:       `{"messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"` + image + `","detail":"high"}},{"type":"file","file":{"file_data":"` + pdf + `","filename":"document.pdf"}}]}],"max_completion_tokens":100}`,
			cacheWrite: true,
		},
		{
			name:       "responses",
			protocol:   entity.ProtocolOpenAIResponses,
			body:       `{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"},{"type":"input_image","image_url":"` + image + `","detail":"low"},{"type":"input_file","file_data":"` + pdf + `","filename":"document.pdf"}]}],"max_output_tokens":100}`,
			cacheWrite: true,
		},
		{
			name:       "messages",
			protocol:   entity.ProtocolAnthropicMessages,
			body:       `{"messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + image + `"}},{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"` + pdf + `"}}]}],"max_tokens":100}`,
			cacheWrite: true,
		},
		{
			name:       "gemini",
			protocol:   entity.ProtocolGeminiGenerateContent,
			body:       `{"contents":[{"role":"user","parts":[{"text":"hello"},{"inlineData":{"mimeType":"image/png","data":"` + image + `"}},{"inline_data":{"mime_type":"application/pdf","data":"` + pdf + `"}}]}],"generationConfig":{"maxOutputTokens":100}}`,
			cacheWrite: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := quotaPayload(t, test.body)
			if inspectQuotaRequest(test.protocol, payload).Supported {
				t.Fatal("unattested media request accepted")
			}
			plan, err := planGatewayAttachments(test.protocol, payload)
			if err != nil {
				t.Fatal(err)
			}
			result := inspectQuotaRequest(test.protocol, payload, plan)
			if !result.Supported || result.MaxOutput != 100 || result.CacheWrite != test.cacheWrite {
				t.Fatalf("authenticated attachment request rejected: %+v", result)
			}
		})
	}
}

func TestQuotaNativeAttachmentPlanDoesNotHideOtherMedia(t *testing.T) {
	image := gatewayAttachmentReferencePrefix + testAttachmentImageID
	tests := []struct {
		name, protocol, body string
	}{
		{
			"chat",
			entity.ProtocolOpenAIChat,
			`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"` + image + `"}},{"type":"image_url","image_url":{"url":"https://example.com/external.png"}}]}],"max_completion_tokens":100}`,
		},
		{
			"responses",
			entity.ProtocolOpenAIResponses,
			`{"input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"` + image + `"},{"type":"input_image","image_url":"https://example.com/external.png"}]}],"max_output_tokens":100}`,
		},
		{
			"messages",
			entity.ProtocolAnthropicMessages,
			`{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + image + `"}},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}}]}],"max_tokens":100}`,
		},
		{
			"gemini",
			entity.ProtocolGeminiGenerateContent,
			`{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"` + image + `"}},{"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}]}],"generationConfig":{"maxOutputTokens":100}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := quotaPayload(t, test.body)
			plan, err := planGatewayAttachments(test.protocol, payload)
			if err != nil {
				t.Fatal(err)
			}
			if inspectQuotaRequest(test.protocol, payload, plan).Supported {
				t.Fatal("unplanned external media was hidden by the attachment plan")
			}
		})
	}
}

func TestQuotaNativeToolAttachmentPositionsRemainUnsupported(t *testing.T) {
	image := gatewayAttachmentReferencePrefix + testAttachmentImageID
	tests := []struct {
		name, protocol, body string
	}{
		{
			"responses hosted tool",
			entity.ProtocolOpenAIResponses,
			`{"input":"hello","max_output_tokens":100,"tools":[{"type":"web_search","input_image_mask":{"image_url":"` + image + `"}}]}`,
		},
		{
			"messages tool result",
			entity.ProtocolAnthropicMessages,
			`{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_1","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + image + `"}}]}]}],"max_tokens":100}`,
		},
		{
			"gemini function response",
			entity.ProtocolGeminiGenerateContent,
			`{"contents":[{"parts":[{"functionResponse":{"name":"lookup","response":{},"parts":[{"inlineData":{"mimeType":"image/png","data":"` + image + `"}}]}}]}],"generationConfig":{"maxOutputTokens":100}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := quotaPayload(t, test.body)
			plan, err := planGatewayAttachments(test.protocol, payload)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Occurrences) == 0 {
				t.Fatal("test does not exercise a planned attachment")
			}
			if inspectQuotaRequest(test.protocol, payload, plan).Supported {
				t.Fatal("tool attachment position became token-boundable")
			}
		})
	}
}
