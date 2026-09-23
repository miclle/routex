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
