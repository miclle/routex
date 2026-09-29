package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/oklog/ulid/v2"
)

const (
	gatewayAttachmentMaxOccurrences   = 4
	gatewayAttachmentMaxUnique        = 4
	gatewayAttachmentMaxRawBytes      = 8 << 20
	gatewayAttachmentMaxRewrittenJSON = 12 << 20

	gatewayAttachmentImage = "image"
	gatewayAttachmentPDF   = "pdf"
)

const gatewayAttachmentReferencePrefix = "routex://attachments/"

// gatewayAttachmentReference identifies one occurrence in native request order.
// Location is a JSON Pointer and ObjectID is safe to use as the request-local
// deduplication key.
type gatewayAttachmentReference struct {
	ObjectID string
	Kind     string
	Location string
	path     []any
	shape    string
}

// gatewayAttachmentData is supplied by the storage boundary after it has
// authenticated ownership and verified the stored object.
type gatewayAttachmentData struct {
	ObjectID string
	Name     string
	MIME     string
	Data     []byte
}

// gatewayAttachmentPlan owns a detached request tree so scanning never mutates
// the parser result before every reference has been authorized and resolved.
type gatewayAttachmentPlan struct {
	root        map[string]any
	Occurrences []gatewayAttachmentReference
	scanError   error
}

// MediaInputs counts forwarded occurrences rather than unique stored objects.
// One stored object referenced twice is read once but is sent, and therefore
// billed, twice by the native request.
func (plan *gatewayAttachmentPlan) MediaInputs() (images, pdfs int64) {
	if plan == nil {
		return 0, 0
	}
	for _, occurrence := range plan.Occurrences {
		switch occurrence.Kind {
		case gatewayAttachmentImage:
			images++
		case gatewayAttachmentPDF:
			pdfs++
		}
	}
	return images, pdfs
}

func planGatewayAttachments(protocol string, payload map[string]json.RawMessage) (*gatewayAttachmentPlan, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, invalidGatewayAttachmentReference()
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var root map[string]any
	if decoder.Decode(&root) != nil || root == nil {
		return nil, invalidGatewayAttachmentReference()
	}
	plan := &gatewayAttachmentPlan{root: root}
	switch protocol {
	case entity.ProtocolOpenAIChat:
		plan.scanOpenAIChat()
	case entity.ProtocolOpenAIResponses:
		plan.scanOpenAIResponses()
	case entity.ProtocolAnthropicMessages:
		plan.scanAnthropicMessages()
	case entity.ProtocolGeminiGenerateContent:
		plan.scanGemini()
	default:
		return nil, unsupportedGatewayAttachmentReference()
	}
	if plan.scanError != nil {
		return nil, plan.scanError
	}
	if len(plan.Occurrences) > gatewayAttachmentMaxOccurrences {
		return nil, gatewayAttachmentLimitError()
	}
	unique := make(map[string]struct{}, len(plan.Occurrences))
	for _, ref := range plan.Occurrences {
		unique[ref.ObjectID] = struct{}{}
	}
	if len(unique) > gatewayAttachmentMaxUnique {
		return nil, gatewayAttachmentLimitError()
	}
	return plan, nil
}

// UniqueObjectIDs returns first-occurrence order for request-local storage
// reads. Rewrite still applies every occurrence in its original position.
func (plan *gatewayAttachmentPlan) UniqueObjectIDs() []string {
	ids := make([]string, 0, len(plan.Occurrences))
	seen := make(map[string]struct{}, len(plan.Occurrences))
	for _, ref := range plan.Occurrences {
		if _, exists := seen[ref.ObjectID]; exists {
			continue
		}
		seen[ref.ObjectID] = struct{}{}
		ids = append(ids, ref.ObjectID)
	}
	return ids
}

// Rewrite replaces authorized RouteX references with provider-native inline
// data and returns a fresh RawMessage map for the normal gateway pipeline.
func (plan *gatewayAttachmentPlan) Rewrite(resolved map[string]gatewayAttachmentData) (map[string]json.RawMessage, error) {
	total := 0
	counted := make(map[string]struct{}, len(plan.Occurrences))
	for _, ref := range plan.Occurrences {
		item, ok := resolved[ref.ObjectID]
		if !ok || (item.ObjectID != "" && item.ObjectID != ref.ObjectID) || !gatewayAttachmentMIMEMatches(ref.Kind, item.MIME) {
			return nil, unsupportedGatewayAttachmentReference()
		}
		if _, exists := counted[ref.ObjectID]; !exists {
			total += len(item.Data)
			counted[ref.ObjectID] = struct{}{}
			if total > gatewayAttachmentMaxRawBytes {
				return nil, gatewayAttachmentLimitError()
			}
		}
		if err := plan.rewriteOccurrence(ref, item); err != nil {
			return nil, err
		}
	}
	raw, err := json.Marshal(plan.root)
	if err != nil {
		return nil, invalidGatewayAttachmentReference()
	}
	if len(raw) > gatewayAttachmentMaxRewrittenJSON {
		return nil, gatewayAttachmentLimitError()
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(raw, &payload) != nil {
		return nil, invalidGatewayAttachmentReference()
	}
	return payload, nil
}

func gatewayAttachmentMIMEMatches(kind, mime string) bool {
	switch kind {
	case gatewayAttachmentImage:
		return mime == "image/png" || mime == "image/jpeg"
	case gatewayAttachmentPDF:
		return mime == "application/pdf"
	default:
		return false
	}
}

func (plan *gatewayAttachmentPlan) rewriteOccurrence(ref gatewayAttachmentReference, item gatewayAttachmentData) error {
	parent, key, ok := attachmentLocation(plan.root, ref.path)
	if !ok {
		return invalidGatewayAttachmentReference()
	}
	encoded := base64.StdEncoding.EncodeToString(item.Data)
	dataURL := "data:" + item.MIME + ";base64," + encoded
	switch ref.shape {
	case "data_url":
		parent[key] = dataURL
	case "anthropic_source":
		parent[key] = encoded
		source := parent
		source["type"] = "base64"
		source["media_type"] = item.MIME
	case "gemini_inline_camel":
		parent[key] = encoded
		parent["mimeType"] = item.MIME
		delete(parent, "mime_type")
	case "gemini_inline_snake":
		parent[key] = encoded
		parent["mimeType"] = item.MIME
		delete(parent, "mime_type")
		part, ok := attachmentObject(plan.root, ref.path[:len(ref.path)-2])
		if !ok {
			return invalidGatewayAttachmentReference()
		}
		delete(part, "inline_data")
		part["inlineData"] = parent
	case "openai_file":
		parent[key] = dataURL
		parent["filename"] = safeGatewayAttachmentFilename(item.Name)
	default:
		return unsupportedGatewayAttachmentReference()
	}
	return nil
}

func safeGatewayAttachmentFilename(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	name = path.Base(name)
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "attachment.pdf"
	}
	for len(name) > 200 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}

func attachmentLocation(root map[string]any, segments []any) (map[string]any, string, bool) {
	if len(segments) == 0 {
		return nil, "", false
	}
	var current any = root
	for _, segment := range segments[:len(segments)-1] {
		switch value := segment.(type) {
		case string:
			object, ok := current.(map[string]any)
			if !ok {
				return nil, "", false
			}
			current, ok = object[value]
			if !ok {
				return nil, "", false
			}
		case int:
			array, ok := current.([]any)
			if !ok || value < 0 || value >= len(array) {
				return nil, "", false
			}
			current = array[value]
		default:
			return nil, "", false
		}
	}
	key, ok := segments[len(segments)-1].(string)
	parent, parentOK := current.(map[string]any)
	return parent, key, ok && parentOK
}

func attachmentObject(root map[string]any, segments []any) (map[string]any, bool) {
	var current any = root
	for _, segment := range segments {
		switch value := segment.(type) {
		case string:
			object, ok := current.(map[string]any)
			if !ok {
				return nil, false
			}
			current, ok = object[value]
			if !ok {
				return nil, false
			}
		case int:
			array, ok := current.([]any)
			if !ok || value < 0 || value >= len(array) {
				return nil, false
			}
			current = array[value]
		default:
			return nil, false
		}
	}
	object, ok := current.(map[string]any)
	return object, ok
}

// scanError lives on the plan to keep the traversal helpers small and to stop
// deterministically at the first malformed reserved reference.
func (plan *gatewayAttachmentPlan) setScanError(err error) {
	if plan.scanError == nil {
		plan.scanError = err
	}
}

func (plan *gatewayAttachmentPlan) scanOpenAIChat() {
	messages, _ := plan.root["messages"].([]any)
	for messageIndex, rawMessage := range messages {
		message, _ := rawMessage.(map[string]any)
		parts, ok := message["content"].([]any)
		if !ok {
			continue
		}
		for partIndex, rawPart := range parts {
			part, _ := rawPart.(map[string]any)
			base := []any{"messages", messageIndex, "content", partIndex}
			switch part["type"] {
			case "image_url":
				container, _ := part["image_url"].(map[string]any)
				plan.recordReference(container, "url", gatewayAttachmentImage, appendPath(base, "image_url", "url"), "data_url")
			case "file":
				container, _ := part["file"].(map[string]any)
				plan.recordReference(container, "file_data", gatewayAttachmentPDF, appendPath(base, "file", "file_data"), "openai_file")
			}
		}
	}
}

func (plan *gatewayAttachmentPlan) scanOpenAIResponses() {
	items, _ := plan.root["input"].([]any)
	for itemIndex, rawItem := range items {
		item, _ := rawItem.(map[string]any)
		kind, _ := item["type"].(string)
		field := ""
		switch kind {
		case "", "message":
			field = "content"
		case "function_call_output", "custom_tool_call_output":
			field = "output"
		}
		if field != "" {
			plan.scanOpenAIResponsesContent(item[field], []any{"input", itemIndex, field})
		}
		if kind == "computer_call_output" {
			output, _ := item["output"].(map[string]any)
			plan.recordReference(output, "image_url", gatewayAttachmentImage, []any{"input", itemIndex, "output", "image_url"}, "data_url")
		}
	}

	tools, _ := plan.root["tools"].([]any)
	for toolIndex, rawTool := range tools {
		tool, _ := rawTool.(map[string]any)
		kind, _ := tool["type"].(string)
		switch kind {
		case "web_search", "web_search_preview", "computer", "computer_use_preview", "image_generation":
			mask, _ := tool["input_image_mask"].(map[string]any)
			plan.recordReference(mask, "image_url", gatewayAttachmentImage, []any{"tools", toolIndex, "input_image_mask", "image_url"}, "data_url")
		}
	}
}

func (plan *gatewayAttachmentPlan) scanOpenAIResponsesContent(value any, base []any) {
	parts, ok := value.([]any)
	if !ok {
		return
	}
	for partIndex, rawPart := range parts {
		part, _ := rawPart.(map[string]any)
		partPath := appendPath(base, partIndex)
		switch part["type"] {
		case "input_image":
			plan.recordReference(part, "image_url", gatewayAttachmentImage, appendPath(partPath, "image_url"), "data_url")
		case "input_file":
			plan.recordReference(part, "file_data", gatewayAttachmentPDF, appendPath(partPath, "file_data"), "openai_file")
		}
	}
}

func (plan *gatewayAttachmentPlan) scanAnthropicMessages() {
	messages, _ := plan.root["messages"].([]any)
	for messageIndex, rawMessage := range messages {
		message, _ := rawMessage.(map[string]any)
		plan.scanAnthropicBlocks(message["content"], []any{"messages", messageIndex, "content"}, 0)
	}
	plan.scanAnthropicBlocks(plan.root["system"], []any{"system"}, 0)
}

func (plan *gatewayAttachmentPlan) scanAnthropicBlocks(value any, base []any, depth int) {
	if plan.scanError != nil || depth >= 8 {
		return
	}
	blocks, ok := value.([]any)
	if !ok {
		return
	}
	for blockIndex, rawBlock := range blocks {
		block, _ := rawBlock.(map[string]any)
		blockPath := appendPath(base, blockIndex)
		switch block["type"] {
		case "image", "document":
			source, _ := block["source"].(map[string]any)
			if source == nil {
				continue
			}
			if source["type"] == "content" {
				plan.scanAnthropicBlocks(source["content"], appendPath(blockPath, "source", "content"), depth+1)
				continue
			}
			if _, matched, err := parseGatewayAttachmentReference(source["data"]); err != nil {
				plan.setScanError(err)
			} else if matched && source["type"] != "base64" {
				plan.setScanError(unsupportedGatewayAttachmentReference())
			} else {
				kind := gatewayAttachmentImage
				if block["type"] == "document" {
					kind = gatewayAttachmentPDF
				}
				plan.recordReference(source, "data", kind, appendPath(blockPath, "source", "data"), "anthropic_source")
			}
		case "tool_result", "search_result":
			plan.scanAnthropicBlocks(block["content"], appendPath(blockPath, "content"), depth+1)
		}
	}
}

func (plan *gatewayAttachmentPlan) scanGemini() {
	plan.scanGeminiContents(plan.root["contents"], []any{"contents"}, 0)
	for _, field := range []string{"systemInstruction", "system_instruction"} {
		if content, ok := plan.root[field].(map[string]any); ok {
			plan.scanGeminiParts(content["parts"], []any{field, "parts"}, 0)
		}
	}
}

func (plan *gatewayAttachmentPlan) scanGeminiContents(value any, base []any, depth int) {
	contents, ok := value.([]any)
	if !ok {
		return
	}
	for contentIndex, rawContent := range contents {
		content, _ := rawContent.(map[string]any)
		plan.scanGeminiParts(content["parts"], appendPath(base, contentIndex, "parts"), depth)
	}
}

func (plan *gatewayAttachmentPlan) scanGeminiParts(value any, base []any, depth int) {
	if plan.scanError != nil || depth >= 8 {
		return
	}
	parts, ok := value.([]any)
	if !ok {
		return
	}
	for partIndex, rawPart := range parts {
		part, _ := rawPart.(map[string]any)
		partPath := appendPath(base, partIndex)
		for _, field := range []string{"inlineData", "inline_data"} {
			inline, _ := part[field].(map[string]any)
			if inline == nil {
				continue
			}
			objectID, matched, err := parseGatewayAttachmentReference(inline["data"])
			if err != nil {
				plan.setScanError(err)
				continue
			}
			if !matched {
				continue
			}
			mimeField := "mimeType"
			if field == "inline_data" {
				mimeField = "mime_type"
			}
			mime, _ := inline[mimeField].(string)
			kind := gatewayAttachmentImage
			if mime == "application/pdf" {
				kind = gatewayAttachmentPDF
			} else if mime != "image/png" && mime != "image/jpeg" {
				plan.setScanError(unsupportedGatewayAttachmentReference())
				continue
			}
			shape := "gemini_inline_camel"
			if field == "inline_data" {
				shape = "gemini_inline_snake"
			}
			plan.addReference(objectID, kind, appendPath(partPath, field, "data"), shape)
		}
		responseField := "functionResponse"
		if part[responseField] == nil {
			responseField = "function_response"
		}
		response, _ := part[responseField].(map[string]any)
		if response != nil {
			plan.scanGeminiParts(response["parts"], appendPath(partPath, responseField, "parts"), depth+1)
		}
	}
}

func (plan *gatewayAttachmentPlan) recordReference(parent map[string]any, key, kind string, location []any, shape string) {
	if plan.scanError != nil || parent == nil {
		return
	}
	objectID, matched, err := parseGatewayAttachmentReference(parent[key])
	if err != nil {
		plan.setScanError(err)
		return
	}
	if matched {
		plan.addReference(objectID, kind, location, shape)
	}
}

func (plan *gatewayAttachmentPlan) addReference(objectID, kind string, location []any, shape string) {
	plan.Occurrences = append(plan.Occurrences, gatewayAttachmentReference{
		ObjectID: objectID,
		Kind:     kind,
		Location: gatewayAttachmentJSONPointer(location),
		path:     location,
		shape:    shape,
	})
}

func parseGatewayAttachmentReference(value any) (string, bool, error) {
	reference, ok := value.(string)
	if !ok || !strings.HasPrefix(reference, gatewayAttachmentReferencePrefix) {
		return "", false, nil
	}
	objectID := strings.TrimPrefix(reference, gatewayAttachmentReferencePrefix)
	suffix, hasPrefix := strings.CutPrefix(objectID, "obj_")
	_, err := ulid.ParseStrict(strings.ToUpper(suffix))
	if !hasPrefix || len(suffix) != 26 || suffix != strings.ToLower(suffix) || err != nil {
		return "", true, invalidGatewayAttachmentReference()
	}
	return objectID, true, nil
}

func appendPath(base []any, segments ...any) []any {
	result := make([]any, 0, len(base)+len(segments))
	result = append(result, base...)
	return append(result, segments...)
}

func gatewayAttachmentJSONPointer(segments []any) string {
	var result strings.Builder
	for _, segment := range segments {
		result.WriteByte('/')
		value := fmt.Sprint(segment)
		value = strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
		result.WriteString(value)
	}
	return result.String()
}

func invalidGatewayAttachmentReference() *GatewayError {
	return gatewayError(400, "invalid_attachment_reference", "The attachment reference is invalid.")
}

func unsupportedGatewayAttachmentReference() *GatewayError {
	return gatewayError(400, "unsupported_attachment_reference", "The attachment reference is not supported in this native media position.")
}

func gatewayAttachmentLimitError() *GatewayError {
	return gatewayError(413, "attachment_limit_exceeded", "The attachment request exceeds the supported limits.")
}
