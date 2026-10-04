package handler

import (
	"encoding/json"
	"io"
	"net/url"
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

const modelAliasRetirementBodyLimit = 16 * 1024

func decodeModelAliasRetirementInput(reader io.Reader) (service.ModelAliasRetirementInput, error) {
	var input service.ModelAliasRetirementInput
	body, err := io.ReadAll(io.LimitReader(reader, modelAliasRetirementBodyLimit+1))
	if err != nil || len(body) > modelAliasRetirementBodyLimit || json.Unmarshal(body, &input) != nil {
		return input, apperrors.ErrBadRequest
	}
	return input, nil
}
func modelAliasRetirementQuery(raw string) (string, error) {
	values, err := url.ParseQuery(raw)
	if err != nil || len(values) != 1 || len(values["name"]) != 1 || values.Get("name") == "" {
		return "", apperrors.ErrBadRequest
	}
	return values.Get("name"), nil
}
func (ctrl *Ctrl) GetModelAliasRetirement(c *fox.Context) (*service.ModelAliasRetirementReview, error) {
	c.Header("Cache-Control", "private, no-store")
	name, err := modelAliasRetirementQuery(c.Request.URL.RawQuery)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetModelAliasRetirement(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("model_id"), name)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
func (ctrl *Ctrl) RetireModelAlias(c *fox.Context) (*service.ModelAliasRetirementResult, error) {
	c.Header("Cache-Control", "private, no-store")
	input, err := decodeModelAliasRetirementInput(c.Request.Body)
	if err != nil {
		return nil, err
	}
	etag, err := quotaETag(c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.RetireModelAlias(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("model_id"), etag, input)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.Alias.ETag))
	}
	return result, err
}
