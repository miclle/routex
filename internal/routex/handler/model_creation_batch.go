package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func modelCreationPrivate(c *fox.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
}
func modelCreationFilter(c *fox.Context) (service.ModelCreationFilter, error) {
	values, err := parseTeamQuotaQuery(c, map[string]bool{"q": true, "cursor": true, "limit": true})
	if err != nil {
		return service.ModelCreationFilter{}, err
	}
	if len(values["q"]) > 200 || !utf8.ValidString(values["q"]) {
		return service.ModelCreationFilter{}, apperrors.ErrBadRequest
	}
	limit := 0
	if values["limit"] != "" {
		limit, err = strconv.Atoi(values["limit"])
		if err != nil || limit < 1 || limit > 50 || strconv.Itoa(limit) != values["limit"] {
			return service.ModelCreationFilter{}, apperrors.ErrBadRequest
		}
	}
	return service.ModelCreationFilter{Query: values["q"], Cursor: values["cursor"], Limit: limit}, nil
}

// Only the inline identity pickers accept exact_id. Other Model creation
// endpoints retain the existing paged filter contract unchanged.
func modelAccessPickerFilter(c *fox.Context) (service.ModelAccessPickerFilter, error) {
	values, err := parseTeamQuotaQuery(c, map[string]bool{"q": true, "cursor": true, "limit": true, "exact_id": true})
	if err != nil || len(values["q"]) > 200 || !utf8.ValidString(values["q"]) || values["exact_id"] != "" && (values["q"] != "" || values["cursor"] != "") {
		return service.ModelAccessPickerFilter{}, apperrors.ErrBadRequest
	}
	limit := 0
	if values["limit"] != "" {
		limit, err = strconv.Atoi(values["limit"])
		if err != nil || limit < 1 || limit > 50 || strconv.Itoa(limit) != values["limit"] {
			return service.ModelAccessPickerFilter{}, apperrors.ErrBadRequest
		}
	}
	return service.ModelAccessPickerFilter{ModelCreationFilter: service.ModelCreationFilter{Query: values["q"], Cursor: values["cursor"], Limit: limit}, ExactID: values["exact_id"]}, nil
}

func decodeModelCreation(c *fox.Context, target any) error {
	if err := noTeamQuotaQuery(c); err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 32*1024+1))
	if err != nil || len(raw) > 32*1024 || json.Unmarshal(raw, target) != nil {
		return apperrors.ErrBadRequest
	}
	return nil
}
func (ctrl *Ctrl) ListModelCreationConnections(c *fox.Context) (*service.ModelCreationConnectionPage, error) {
	modelCreationPrivate(c)
	filter, err := modelCreationFilter(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListModelCreationConnections(c.Request.Context(), currentAuthentication(c).User.ID, filter)
}
func (ctrl *Ctrl) ListModelCreationProviders(c *fox.Context) (*service.ModelCreationProviderPage, error) {
	modelCreationPrivate(c)
	filter, err := modelAccessPickerFilter(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListModelCreationProviders(c.Request.Context(), currentAuthentication(c).User.ID, filter)
}
func (ctrl *Ctrl) ListModelCreationEgresses(c *fox.Context) (*service.ModelCreationEgressPage, error) {
	modelCreationPrivate(c)
	filter, err := modelAccessPickerFilter(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListModelCreationEgresses(c.Request.Context(), currentAuthentication(c).User.ID, filter)
}
func (ctrl *Ctrl) GetModelCreationContext(c *fox.Context) (*service.ModelCreationContext, error) {
	modelCreationPrivate(c)
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	return ctrl.service.GetModelCreationContext(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("connection_id"))
}
func (ctrl *Ctrl) ListModelCreationProviderModels(c *fox.Context) (*service.ModelCreationProviderModelPage, error) {
	modelCreationPrivate(c)
	filter, err := modelCreationFilter(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListModelCreationProviderModels(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("connection_id"), filter)
}
func (ctrl *Ctrl) ListModelCreationTargets(c *fox.Context) (*service.ModelCreationTargetPage, error) {
	modelCreationPrivate(c)
	filter, err := modelCreationFilter(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListModelCreationTargets(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("connection_id"), filter)
}
func (ctrl *Ctrl) PreviewModelCreationBatch(c *fox.Context) (*service.ModelCreationPreview, error) {
	modelCreationPrivate(c)
	var input service.ModelCreationPreviewInput
	if err := decodeModelCreation(c, &input); err != nil {
		return nil, err
	}
	result, err := ctrl.service.PreviewModelCreationBatch(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("connection_id"), input)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) CreateModelBatch(c *fox.Context) error {
	modelCreationPrivate(c)
	var input service.ModelCreationBatchInput
	if err := decodeModelCreation(c, &input); err != nil {
		return err
	}
	etag, err := quotaETag(c)
	if err != nil || len(etag) != 64 {
		return apperrors.ErrBadRequest
	}
	for _, value := range etag {
		if (value < '0' || value > '9') && (value < 'a' || value > 'f') {
			return apperrors.ErrBadRequest
		}
	}
	result, err := ctrl.service.CreateModelBatch(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("connection_id"), etag, input)
	if err != nil {
		return err
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	c.JSON(status, result)
	return nil
}
func (ctrl *Ctrl) GetModelCreationReceipt(c *fox.Context) (*service.ModelCreationBatchResult, error) {
	modelCreationPrivate(c)
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	return ctrl.service.GetModelCreationReceipt(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("request_id"))
}
