package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

const (
	testAttachmentImageID = "obj_01arz3ndektsv4rrffq69g5fav"
	testAttachmentPDFID   = "obj_01arz3ndektsv4rrffq69g5faw"
)

func TestGatewayAttachmentNativeRewrite(t *testing.T) {
	tests := []struct {
		name       string
		protocol   string
		body       string
		locations  []string
		assertions []string
	}{
		{
			name:       "OpenAI Chat",
			protocol:   entity.ProtocolOpenAIChat,
			body:       `{"model":"public","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"routex://attachments/` + testAttachmentImageID + `","detail":"high"}},{"type":"file","file":{"file_data":"routex://attachments/` + testAttachmentPDFID + `","filename":"caller.pdf"}}]}]}`,
			locations:  []string{"/messages/0/content/0/image_url/url", "/messages/0/content/1/file/file_data"},
			assertions: []string{`"url":"data:image/png;base64,aW1hZ2U="`, `"file_data":"data:application/pdf;base64,cGRm"`, `"filename":"report.pdf"`, `"detail":"high"`},
		},
		{
			name:       "OpenAI Responses",
			protocol:   entity.ProtocolOpenAIResponses,
			body:       `{"model":"public","input":[{"role":"user","content":[{"type":"input_image","image_url":"routex://attachments/` + testAttachmentImageID + `"},{"type":"input_file","file_data":"routex://attachments/` + testAttachmentPDFID + `"}]}]}`,
			locations:  []string{"/input/0/content/0/image_url", "/input/0/content/1/file_data"},
			assertions: []string{`"image_url":"data:image/png;base64,aW1hZ2U="`, `"file_data":"data:application/pdf;base64,cGRm"`, `"filename":"report.pdf"`},
		},
		{
			name:       "Anthropic Messages",
			protocol:   entity.ProtocolAnthropicMessages,
			body:       `{"model":"public","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/jpeg","data":"routex://attachments/` + testAttachmentImageID + `"}},{"type":"tool_result","tool_use_id":"tool_1","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"routex://attachments/` + testAttachmentPDFID + `"}}]}]}],"max_tokens":8}`,
			locations:  []string{"/messages/0/content/0/source/data", "/messages/0/content/1/content/0/source/data"},
			assertions: []string{`"data":"aW1hZ2U="`, `"media_type":"image/png"`, `"data":"cGRm"`, `"media_type":"application/pdf"`},
		},
		{
			name:       "Gemini",
			protocol:   entity.ProtocolGeminiGenerateContent,
			body:       `{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/jpeg","data":"routex://attachments/` + testAttachmentImageID + `"}},{"inline_data":{"mime_type":"application/pdf","data":"routex://attachments/` + testAttachmentPDFID + `"}}]}]}`,
			locations:  []string{"/contents/0/parts/0/inlineData/data", "/contents/0/parts/1/inline_data/data"},
			assertions: []string{`"data":"aW1hZ2U="`, `"mimeType":"image/png"`, `"data":"cGRm"`, `"mimeType":"application/pdf"`},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := attachmentTestPayload(t, test.body)
			plan, err := planGatewayAttachments(test.protocol, payload)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Occurrences) != 2 || plan.Occurrences[0].Kind != gatewayAttachmentImage || plan.Occurrences[1].Kind != gatewayAttachmentPDF {
				t.Fatalf("unexpected plan: %+v", plan.Occurrences)
			}
			for index, location := range test.locations {
				if plan.Occurrences[index].Location != location {
					t.Fatalf("location %d = %q, want %q", index, plan.Occurrences[index].Location, location)
				}
			}
			rewritten, err := plan.Rewrite(map[string]gatewayAttachmentData{
				testAttachmentImageID: {ObjectID: testAttachmentImageID, Name: "image.png", MIME: "image/png", Data: []byte("image")},
				testAttachmentPDFID:   {ObjectID: testAttachmentPDFID, Name: "../report.pdf\r\n", MIME: "application/pdf", Data: []byte("pdf")},
			})
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(rewritten)
			for _, assertion := range test.assertions {
				if !strings.Contains(string(raw), assertion) {
					t.Fatalf("rewritten request missing %s: %s", assertion, raw)
				}
			}
			if test.protocol == entity.ProtocolGeminiGenerateContent && (strings.Contains(string(raw), "inline_data") || strings.Contains(string(raw), "mime_type")) {
				t.Fatalf("Gemini aliases were not canonicalized: %s", raw)
			}
		})
	}
}

func TestGatewayAttachmentScansEveryAcceptedNativeContentContainer(t *testing.T) {
	tests := []struct {
		name      string
		protocol  string
		body      string
		locations []string
	}{
		{
			name:     "Responses tool outputs",
			protocol: entity.ProtocolOpenAIResponses,
			body: `{"model":"public","input":[` +
				`{"type":"function_call_output","output":[{"type":"input_image","image_url":"routex://attachments/` + testAttachmentImageID + `"}]},` +
				`{"type":"custom_tool_call_output","output":[{"type":"input_file","file_data":"routex://attachments/` + testAttachmentPDFID + `"}]}` +
				`]}`,
			locations: []string{"/input/0/output/0/image_url", "/input/1/output/0/file_data"},
		},
		{
			name:     "Responses computer output and image mask",
			protocol: entity.ProtocolOpenAIResponses,
			body: `{"model":"public","input":[` +
				`{"type":"computer_call_output","call_id":"call_1","output":{"type":"computer_screenshot","image_url":"routex://attachments/` + testAttachmentImageID + `"}}` +
				`],"tools":[{"type":"image_generation","input_image_mask":{"image_url":"routex://attachments/` + testAttachmentImageID + `"}}]}`,
			locations: []string{"/input/0/output/image_url", "/tools/0/input_image_mask/image_url"},
		},
		{
			name:     "Messages system and nested content source",
			protocol: entity.ProtocolAnthropicMessages,
			body: `{"model":"public","system":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"routex://attachments/` + testAttachmentImageID + `"}}],` +
				`"messages":[{"role":"user","content":[{"type":"document","source":{"type":"content","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"routex://attachments/` + testAttachmentPDFID + `"}}]}}]}],"max_tokens":8}`,
			locations: []string{"/messages/0/content/0/source/content/0/source/data", "/system/0/source/data"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := attachmentTestPayload(t, test.body)
			plan, err := planGatewayAttachments(test.protocol, payload)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Occurrences) != len(test.locations) {
				t.Fatalf("occurrences = %+v, want %d", plan.Occurrences, len(test.locations))
			}
			for index, location := range test.locations {
				if plan.Occurrences[index].Location != location {
					t.Fatalf("location %d = %q, want %q", index, plan.Occurrences[index].Location, location)
				}
			}
			rewritten, err := plan.Rewrite(map[string]gatewayAttachmentData{
				testAttachmentImageID: {MIME: "image/png", Data: []byte("image")},
				testAttachmentPDFID:   {Name: "document.pdf", MIME: "application/pdf", Data: []byte("pdf")},
			})
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(rewritten)
			if strings.Contains(string(raw), gatewayAttachmentReferencePrefix) {
				t.Fatalf("accepted native container retained RouteX reference: %s", raw)
			}
		})
	}
}

func TestGatewayAttachmentPlanDeduplicatesReadsAndPreservesOccurrences(t *testing.T) {
	payload := attachmentTestPayload(t, `{"model":"public","messages":[{"content":[{"type":"image_url","image_url":{"url":"routex://attachments/`+testAttachmentImageID+`"}},{"type":"image_url","image_url":{"url":"routex://attachments/`+testAttachmentImageID+`"}}]}]}`)
	plan, err := planGatewayAttachments(entity.ProtocolOpenAIChat, payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Occurrences) != 2 || len(plan.UniqueObjectIDs()) != 1 || plan.UniqueObjectIDs()[0] != testAttachmentImageID {
		t.Fatalf("deduplication plan = %+v, unique = %v", plan.Occurrences, plan.UniqueObjectIDs())
	}
	images, pdfs := plan.MediaInputs()
	if images != 2 || pdfs != 0 {
		t.Fatalf("media occurrence counts = image:%d pdf:%d", images, pdfs)
	}
	rewritten, err := plan.Rewrite(map[string]gatewayAttachmentData{testAttachmentImageID: {MIME: "image/jpeg", Data: []byte("same")}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(rewritten)
	if strings.Count(string(raw), "data:image/jpeg;base64,c2FtZQ==") != 2 {
		t.Fatalf("repeated occurrence was not preserved: %s", raw)
	}
}

func TestGatewayAttachmentScannerIgnoresOpaqueAndUnknownData(t *testing.T) {
	reference := "routex://attachments/" + testAttachmentImageID
	tests := []struct {
		protocol string
		body     string
	}{
		{entity.ProtocolOpenAIChat, `{"model":"public","messages":[{"content":[{"type":"text","text":"` + reference + `"},{"type":"tool_call","arguments":{"image_url":{"url":"` + reference + `"}}},{"type":"future_media","url":"` + reference + `"}]}]}`},
		{entity.ProtocolOpenAIResponses, `{"model":"public","input":[{"type":"function_call","arguments":"{\"image_url\":\"` + reference + `\"}"}],"tools":[{"type":"function","name":"opaque","parameters":{"input_image_mask":{"image_url":"` + reference + `"}}}]}`},
		{entity.ProtocolAnthropicMessages, `{"model":"public","messages":[{"role":"user","content":[{"type":"tool_use","input":{"source":{"data":"` + reference + `"}}},{"type":"text","text":"` + reference + `"}]}],"max_tokens":8}`},
		{entity.ProtocolGeminiGenerateContent, `{"contents":[{"parts":[{"text":"` + reference + `"},{"functionCall":{"args":{"inlineData":{"data":"` + reference + `"}}}}]}]}`},
	}
	for _, test := range tests {
		plan, err := planGatewayAttachments(test.protocol, attachmentTestPayload(t, test.body))
		if err != nil || len(plan.Occurrences) != 0 {
			t.Fatalf("opaque data inspected for %s: %+v %v", test.protocol, plan.Occurrences, err)
		}
		rewritten, err := plan.Rewrite(nil)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(rewritten)
		if !strings.Contains(string(raw), reference) {
			t.Fatalf("opaque reference changed for %s: %s", test.protocol, raw)
		}
	}
}

func TestGatewayAttachmentReferenceValidationAndLimits(t *testing.T) {
	tests := []struct {
		name string
		body string
		code string
	}{
		{"malformed ID", `{"messages":[{"content":[{"type":"image_url","image_url":{"url":"routex://attachments/obj_bad"}}]}]}`, "invalid_attachment_reference"},
		{"too many occurrences", `{"messages":[{"content":[` + strings.TrimSuffix(strings.Repeat(`{"type":"image_url","image_url":{"url":"routex://attachments/`+testAttachmentImageID+`"}},`, 5), ",") + `]}]}`, "attachment_limit_exceeded"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := planGatewayAttachments(entity.ProtocolOpenAIChat, attachmentTestPayload(t, test.body))
			assertGatewayAttachmentError(t, err, test.code)
		})
	}

	unsupported := []struct {
		protocol string
		body     string
	}{
		{entity.ProtocolAnthropicMessages, `{"messages":[{"content":[{"type":"image","source":{"type":"url","data":"routex://attachments/` + testAttachmentImageID + `"}}]}]}`},
		{entity.ProtocolGeminiGenerateContent, `{"contents":[{"parts":[{"inlineData":{"data":"routex://attachments/` + testAttachmentImageID + `"}}]}]}`},
	}
	for _, test := range unsupported {
		_, err := planGatewayAttachments(test.protocol, attachmentTestPayload(t, test.body))
		assertGatewayAttachmentError(t, err, "unsupported_attachment_reference")
	}
}

func TestGatewayAttachmentRewriteValidatesResolvedMediaAndBounds(t *testing.T) {
	payload := attachmentTestPayload(t, `{"messages":[{"content":[{"type":"image_url","image_url":{"url":"routex://attachments/`+testAttachmentImageID+`"}}]}]}`)
	plan, err := planGatewayAttachments(entity.ProtocolOpenAIChat, payload)
	if err != nil {
		t.Fatal(err)
	}
	_, err = plan.Rewrite(map[string]gatewayAttachmentData{testAttachmentImageID: {MIME: "application/pdf", Data: []byte("wrong")}})
	assertGatewayAttachmentError(t, err, "unsupported_attachment_reference")

	plan, _ = planGatewayAttachments(entity.ProtocolOpenAIChat, payload)
	_, err = plan.Rewrite(map[string]gatewayAttachmentData{testAttachmentImageID: {MIME: "image/png", Data: make([]byte, gatewayAttachmentMaxRawBytes+1)}})
	assertGatewayAttachmentError(t, err, "attachment_limit_exceeded")

	largeText := strings.Repeat("x", 2<<20)
	largePayload := attachmentTestPayload(t, `{"messages":[{"content":[{"type":"text","text":"`+largeText+`"},{"type":"image_url","image_url":{"url":"routex://attachments/`+testAttachmentImageID+`"}}]}]}`)
	plan, _ = planGatewayAttachments(entity.ProtocolOpenAIChat, largePayload)
	_, err = plan.Rewrite(map[string]gatewayAttachmentData{testAttachmentImageID: {MIME: "image/png", Data: make([]byte, gatewayAttachmentMaxRawBytes)}})
	assertGatewayAttachmentError(t, err, "attachment_limit_exceeded")
}

func attachmentTestPayload(t *testing.T, body string) map[string]json.RawMessage {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func assertGatewayAttachmentError(t *testing.T, err error, code string) {
	t.Helper()
	var gatewayErr *GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Code != code {
		t.Fatalf("error = %v, want GatewayError code %q", err, code)
	}
}
