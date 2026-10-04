package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

var repositoryResourceID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,30}$`)
var repositorySourceID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,127}$`)
var repositoryRequestID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func repositoryPrivate(c *fox.Context) { c.Header("Cache-Control", "private, no-store") }
func repositoryNoQuery(c *fox.Context) error {
	repositoryPrivate(c)
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		return apperrors.ErrBadRequest
	}
	return nil
}
func (ctrl *Ctrl) GetRepositoryPriceSource(c *fox.Context) (*service.RepositoryPriceConfigView, error) {
	if err := repositoryNoQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetRepositoryPriceSource(c.Request.Context(), currentAuthentication(c).User.ID)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) WriteRepositoryPriceSource(c *fox.Context) error {
	if err := repositoryNoQuery(c); err != nil {
		return err
	}
	raw, err := repositoryBody(c)
	if err != nil {
		return err
	}
	fields, err := repositoryObject(raw, "request_id", "enabled", "mappings", "reason")
	if err != nil {
		return err
	}
	var input service.RepositoryPriceConfigInput
	if json.Unmarshal(raw, &input) != nil || !repositoryRequestID.MatchString(input.RequestID) || !repositoryValidReason(input.Reason) {
		return apperrors.ErrBadRequest
	}
	var enabled bool
	if json.Unmarshal(fields["enabled"], &enabled) != nil {
		return apperrors.ErrBadRequest
	}
	var items []json.RawMessage
	if repositoryArray(fields["mappings"], &items) != nil || len(items) > 100 {
		return apperrors.ErrBadRequest
	}
	seen := map[string]bool{}
	for _, item := range items {
		if _, err := repositoryObject(item, "provider_model_id", "source_model_key"); err != nil {
			return err
		}
		var mapping service.RepositoryPriceMappingInput
		if json.Unmarshal(item, &mapping) != nil || !repositoryResourceID.MatchString(mapping.ProviderModelID) || !repositorySourceID.MatchString(mapping.SourceModelKey) || seen[mapping.ProviderModelID] {
			return apperrors.ErrBadRequest
		}
		seen[mapping.ProviderModelID] = true
	}
	etag, err := personalModelHeader(c)
	if err != nil {
		return err
	}
	result, err := ctrl.service.WriteRepositoryPriceSource(c.Request.Context(), currentAuthentication(c).User.ID, etag, input)
	if err != nil {
		return err
	}
	return repositoryResult(c, result)
}
func (ctrl *Ctrl) ListRepositoryPriceCandidates(c *fox.Context) (*service.RepositoryPriceCandidatePage, error) {
	repositoryPrivate(c)
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || c.Request.URL.ForceQuery && c.Request.URL.RawQuery == "" {
		return nil, apperrors.ErrBadRequest
	}
	for key, values := range query {
		if len(values) != 1 || values[0] == "" || (key != "q" && key != "cursor" && key != "limit") {
			return nil, apperrors.ErrBadRequest
		}
	}
	limit := 20
	if value := query.Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 50 || strconv.Itoa(limit) != value {
			return nil, apperrors.ErrBadRequest
		}
	}
	q := query.Get("q")
	if len(q) > 200 || !utf8.ValidString(q) || strings.ContainsFunc(q, unicode.IsControl) || (query.Get("cursor") != "" && !repositoryResourceID.MatchString(query.Get("cursor"))) {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.ListRepositoryPriceCandidates(c.Request.Context(), currentAuthentication(c).User.ID, service.RepositoryPriceCandidateFilter{Query: q, Cursor: query.Get("cursor"), Limit: limit})
}
func (ctrl *Ctrl) PreviewRepositoryPrices(c *fox.Context) (*service.RepositoryPricePreview, error) {
	if err := repositoryNoQuery(c); err != nil {
		return nil, err
	}
	raw, err := repositoryBody(c)
	if err != nil {
		return nil, err
	}
	input, err := repositorySelection(raw)
	if err != nil {
		return nil, err
	}
	return ctrl.service.PreviewRepositoryPrices(c.Request.Context(), currentAuthentication(c).User.ID, input)
}
func (ctrl *Ctrl) ApplyRepositoryPrices(c *fox.Context) error {
	if err := repositoryNoQuery(c); err != nil {
		return err
	}
	raw, err := repositoryBody(c)
	if err != nil {
		return err
	}
	fields, err := repositoryObject(raw, "request_id", "preview_digest", "selection", "reason")
	if err != nil {
		return err
	}
	input := service.RepositoryPriceApplyInput{}
	if json.Unmarshal(raw, &input) != nil || !repositoryRequestID.MatchString(input.RequestID) || !repositoryValidReason(input.Reason) || !repositoryHashString(input.PreviewDigest) {
		return apperrors.ErrBadRequest
	}
	selection, err := repositorySelection(fields["selection"])
	if err != nil {
		return err
	}
	input.Selection = selection
	etag, err := personalModelHeader(c)
	if err != nil {
		return err
	}
	result, err := ctrl.service.ApplyRepositoryPrices(c.Request.Context(), currentAuthentication(c).User.ID, etag, input)
	if err != nil {
		return err
	}
	return repositoryResult(c, result)
}
func (ctrl *Ctrl) GetRepositoryPriceReceipt(c *fox.Context) (*service.RepositoryPriceResult, error) {
	if err := repositoryNoQuery(c); err != nil {
		return nil, err
	}
	id := c.Param("request_id")
	if !repositoryRequestID.MatchString(id) {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.GetRepositoryPriceReceipt(c.Request.Context(), currentAuthentication(c).User.ID, id)
}
func repositoryResult(c *fox.Context, result *service.RepositoryPriceResult) error {
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	c.JSON(status, result)
	return nil
}
func repositorySelection(raw []byte) (service.RepositoryPriceSelection, error) {
	result := service.RepositoryPriceSelection{}
	fields, err := repositoryObject(raw, "mode", "provider_model_ids", "rate_ids")
	if err != nil {
		return result, err
	}
	if json.Unmarshal(raw, &result) != nil || result.Mode != "sync" && result.Mode != "restore" {
		return result, apperrors.ErrBadRequest
	}
	if repositoryStringIDs(fields["provider_model_ids"], 1, 20) != nil || repositoryStringIDs(fields["rate_ids"], 0, 200) != nil || result.Mode == "sync" && len(result.RateIDs) != 0 || result.Mode == "restore" && len(result.RateIDs) == 0 {
		return result, apperrors.ErrBadRequest
	}
	return result, nil
}
func repositoryStringIDs(raw []byte, min, max int) error {
	var items []json.RawMessage
	if repositoryArray(raw, &items) != nil || len(items) < min || len(items) > max {
		return apperrors.ErrBadRequest
	}
	seen := map[string]bool{}
	for _, item := range items {
		var id string
		if json.Unmarshal(item, &id) != nil || !repositoryResourceID.MatchString(id) || seen[id] {
			return apperrors.ErrBadRequest
		}
		seen[id] = true
	}
	return nil
}
func repositoryHashString(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			if ch < 'a' || ch > 'f' {
				return false
			}
		}
	}
	return true
}
func repositoryValidReason(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && utf8.RuneCountInString(value) <= 1000 && !strings.ContainsFunc(value, unicode.IsControl)
}
func repositoryArray(raw []byte, target *[]json.RawMessage) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, target) != nil {
		return apperrors.ErrBadRequest
	}
	return nil
}
func repositoryBody(c *fox.Context) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 || !utf8.Valid(raw) || !repositoryUnicode(raw) {
		return nil, apperrors.ErrBadRequest
	}
	return raw, nil
}
func repositoryObject(raw []byte, required ...string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, apperrors.ErrBadRequest
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		allowed := false
		for _, field := range required {
			if key == field {
				allowed = true
			}
		}
		if err != nil || !ok || !allowed || fields[key] != nil {
			return nil, apperrors.ErrBadRequest
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, apperrors.ErrBadRequest
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil || decoder.Decode(new(any)) != io.EOF || len(fields) != len(required) {
		return nil, apperrors.ErrBadRequest
	}
	return fields, nil
}
func repositoryUnicode(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) || raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		code, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return false
		}
		if code < 0xd800 || code > 0xdbff {
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
