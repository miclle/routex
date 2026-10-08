package service

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

type ModelCreationFilter struct {
	Query, Cursor string
	Limit         int
}
type ModelCreationConnection struct {
	Adapter      string  `json:"adapter"`
	APIVersion   *string `json:"api_version"`
	ID           string  `json:"id"`
	ProviderID   string  `json:"provider_id"`
	ProviderName string  `json:"provider_name"`
	Name         string  `json:"name"`
	Protocol     string  `json:"protocol"`
	BaseURL      string  `json:"base_url"`
}
type ModelCreationContext struct {
	Connection ModelCreationConnection `json:"connection"`
	CanCreate  bool                    `json:"can_create"`
	ObservedAt time.Time               `json:"observed_at"`
}
type ModelCreationConnectionPage struct {
	Items      []ModelCreationConnection `json:"items"`
	NextCursor *string                   `json:"next_cursor"`
}
type ModelCreationInitialTarget struct {
	Target        string `json:"target"`
	Name          string `json:"name"`
	ModelID       string `json:"model_id,omitempty"`
	InitialWeight *int   `json:"initial_weight,omitempty"`
}

type ModelCreationProviderModel struct {
	InitialTarget     *ModelCreationInitialTarget `json:"initial_target"`
	ID                string                      `json:"id"`
	UpstreamName      string                      `json:"upstream_name"`
	Disabled          bool                        `json:"disabled"`
	InputCapabilities []string                    `json:"input_capabilities"`
	CredentialReady   bool                        `json:"credential_ready"`
	Selectable        bool                        `json:"selectable"`
	BlockerCodes      []string                    `json:"blocker_codes"`
}
type ModelCreationProviderModelPage struct {
	Items      []ModelCreationProviderModel `json:"items"`
	NextCursor *string                      `json:"next_cursor"`
}
type ModelCreationTarget struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	InitialWeight int      `json:"initial_weight"`
	Selectable    bool     `json:"selectable"`
	BlockerCodes  []string `json:"blocker_codes"`
}
type ModelCreationTargetPage struct {
	Items      []ModelCreationTarget `json:"items"`
	NextCursor *string               `json:"next_cursor"`
}
type ModelCreationItem struct {
	ProviderModelID string `json:"provider_model_id,omitempty"`
	UpstreamName    string `json:"upstream_name,omitempty"`
	Target          string `json:"target"`
	Name            string `json:"name,omitempty"`
	ModelID         string `json:"model_id,omitempty"`
}
type ModelCreationPreviewInput struct {
	Items []ModelCreationItem `json:"items"`
}
type ModelCreationBatchInput struct {
	RequestID string              `json:"request_id"`
	Reason    string              `json:"reason"`
	Items     []ModelCreationItem `json:"items"`
}
type ModelCreationReviewedItem struct {
	WarningCodes    []string `json:"warning_codes,omitempty"`
	ProviderModelID string   `json:"provider_model_id"`
	UpstreamName    string   `json:"upstream_name"`
	Target          string   `json:"target"`
	ModelID         *string  `json:"model_id"`
	Name            string   `json:"name"`
	Protocol        string   `json:"protocol"`
	InitialWeight   int      `json:"initial_weight"`
	BlockerCodes    []string `json:"blocker_codes"`
}
type ModelCreationPreview struct {
	Connection ModelCreationConnection     `json:"connection"`
	Items      []ModelCreationReviewedItem `json:"items"`
	ReviewETag string                      `json:"review_etag"`
	ObservedAt time.Time                   `json:"observed_at"`
	CanCommit  bool                        `json:"can_commit"`
}
type ModelCreationReceiptItem struct {
	ManualUpstreamName string `json:"manual_upstream_name,omitempty"`
	ProviderModelID    string `json:"provider_model_id"`
	ModelID            string `json:"model_id"`
	BindingID          string `json:"binding_id"`
	CreatedModel       bool   `json:"created_model"`
	Name               string `json:"name"`
	Protocol           string `json:"protocol"`
	Weight             int    `json:"weight"`
}
type ModelCreationReceipt struct {
	RequestID    string                     `json:"request_id"`
	ConnectionID string                     `json:"connection_id"`
	CreatedAt    time.Time                  `json:"created_at"`
	Items        []ModelCreationReceiptItem `json:"items"`
}
type ModelCreationBatchResult struct {
	Receipt           ModelCreationReceipt       `json:"receipt"`
	Committed         bool                       `json:"committed"`
	Changed           bool                       `json:"changed"`
	CurrentItems      []ModelCreationReceiptItem `json:"current_items"`
	RuntimeApplied    bool                       `json:"runtime_applied"`
	ApplicationStatus string                     `json:"application_status"`
	Created           bool                       `json:"-"`
}

func modelCreationID(value, prefix string) bool {
	return strings.HasPrefix(value, prefix+"_") && safeTeamSessionID(value)
}
func modelCreationObject(raw []byte, allowed ...string) (map[string]json.RawMessage, error) {
	if len(raw) > 32*1024 || !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return nil, apperrors.ErrBadRequest
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return nil, apperrors.ErrBadRequest
	}
	fields := map[string]json.RawMessage{}
	for dec.More() {
		token, err = dec.Token()
		key, ok := token.(string)
		if err != nil || !ok || !slices.Contains(allowed, key) {
			return nil, apperrors.ErrBadRequest
		}
		if _, exists := fields[key]; exists {
			return nil, apperrors.ErrBadRequest
		}
		var value json.RawMessage
		if dec.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, apperrors.ErrBadRequest
		}
		fields[key] = value
	}
	if _, err = dec.Token(); err != nil {
		return nil, apperrors.ErrBadRequest
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, apperrors.ErrBadRequest
	}
	return fields, nil
}
func (item *ModelCreationItem) UnmarshalJSON(raw []byte) error {
	fields, err := modelCreationObject(raw, "provider_model_id", "upstream_name", "target", "name", "model_id")
	if err != nil {
		return err
	}
	var next ModelCreationItem
	for key, value := range fields {
		var dst *string
		switch key {
		case "provider_model_id":
			dst = &next.ProviderModelID
		case "upstream_name":
			dst = &next.UpstreamName
		case "target":
			dst = &next.Target
		case "name":
			dst = &next.Name
		case "model_id":
			dst = &next.ModelID
		}
		if json.Unmarshal(value, dst) != nil {
			return apperrors.ErrBadRequest
		}
	}
	_, stored := fields["provider_model_id"]
	_, manual := fields["upstream_name"]
	if stored == manual || stored && !modelCreationID(next.ProviderModelID, "pmd") || manual && !validUpstreamName(next.UpstreamName) {
		return apperrors.ErrBadRequest
	}
	switch next.Target {
	case "new":
		if _, present := fields["model_id"]; present || !publicModelName.MatchString(next.Name) {
			return apperrors.ErrBadRequest
		}
	case "existing":
		if _, present := fields["name"]; present || !validAdminModelTarget(next.ModelID) {
			return apperrors.ErrBadRequest
		}
	default:
		return apperrors.ErrBadRequest
	}
	*item = next
	return nil
}
func (input *ModelCreationPreviewInput) UnmarshalJSON(raw []byte) error {
	fields, err := modelCreationObject(raw, "items")
	if err != nil {
		return err
	}
	var next ModelCreationPreviewInput
	if json.Unmarshal(fields["items"], &next.Items) != nil {
		return apperrors.ErrBadRequest
	}
	next.Items, err = normalizeModelCreationItems(next.Items)
	if err != nil {
		return err
	}
	*input = next
	return nil
}
func (input *ModelCreationBatchInput) UnmarshalJSON(raw []byte) error {
	fields, err := modelCreationObject(raw, "request_id", "reason", "items")
	if err != nil {
		return err
	}
	var next ModelCreationBatchInput
	if json.Unmarshal(fields["request_id"], &next.RequestID) != nil || json.Unmarshal(fields["reason"], &next.Reason) != nil || json.Unmarshal(fields["items"], &next.Items) != nil {
		return apperrors.ErrBadRequest
	}
	next, err = normalizeModelCreationBatch(next)
	if err != nil {
		return err
	}
	*input = next
	return nil
}
func normalizeModelCreationItems(items []ModelCreationItem) ([]ModelCreationItem, error) {
	if len(items) < 1 || len(items) > 50 {
		return nil, apperrors.ErrBadRequest
	}
	result := slices.Clone(items)
	pms, names, models := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, item := range result {
		key := modelCreationItemKey(item)
		if !validModelCreationSelector(item) || pms[key] {
			return nil, apperrors.ErrBadRequest
		}
		pms[key] = true
		switch item.Target {
		case "new":
			if item.ModelID != "" || !publicModelName.MatchString(item.Name) || names[item.Name] {
				return nil, apperrors.ErrBadRequest
			}
			names[item.Name] = true
		case "existing":
			if item.Name != "" || !validAdminModelTarget(item.ModelID) || models[item.ModelID] {
				return nil, apperrors.ErrBadRequest
			}
			models[item.ModelID] = true
		default:
			return nil, apperrors.ErrBadRequest
		}
	}
	slices.SortFunc(result, func(a, b ModelCreationItem) int {
		return strings.Compare(modelCreationItemKey(a), modelCreationItemKey(b))
	})
	return result, nil
}
func normalizeModelCreationBatch(input ModelCreationBatchInput) (ModelCreationBatchInput, error) {
	if !credentialReplacementRequestID.MatchString(input.RequestID) || !validCredentialMetadataReason(input.Reason) || strings.TrimSpace(input.Reason) == "" {
		return input, apperrors.ErrBadRequest
	}
	input.Reason = strings.TrimSpace(input.Reason)
	var err error
	input.Items, err = normalizeModelCreationItems(input.Items)
	return input, err
}

// encoding/json replaces unpaired surrogate escapes; reject them before intent decoding.
func modelCreationUnicode(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		value, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if value >= 0xdc00 && value <= 0xdfff {
			return false
		}
		if value < 0xd800 || value > 0xdbff {
			continue
		}
		if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
			return false
		}
		low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}

func validModelCreationSelector(item ModelCreationItem) bool {
	return item.ProviderModelID != "" && item.UpstreamName == "" && modelCreationID(item.ProviderModelID, "pmd") || item.ProviderModelID == "" && validUpstreamName(item.UpstreamName)
}
func modelCreationItemKey(item ModelCreationItem) string {
	if item.UpstreamName != "" {
		return "manual:" + item.UpstreamName
	}
	return item.ProviderModelID
}
func modelCreationHasManual(items []ModelCreationItem) bool {
	for _, item := range items {
		if item.UpstreamName != "" {
			return true
		}
	}
	return false
}
