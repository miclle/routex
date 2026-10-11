package handler

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

const modelLifecycleBodyLimit = 16 * 1024

func decodeModelLifecycleInput(reader io.Reader) (service.ModelLifecycleInput, error) {
	var input service.ModelLifecycleInput
	body, err := io.ReadAll(io.LimitReader(reader, modelLifecycleBodyLimit+1))
	if err != nil || len(body) > modelLifecycleBodyLimit || json.Unmarshal(body, &input) != nil {
		return input, apperrors.ErrBadRequest
	}
	return input, nil
}
func modelLifecycleHeader(c *fox.Context) (string, error) {
	headers := c.Request.Header.Values("If-Match")
	if len(headers) != 1 {
		return "", apperrors.ErrBadRequest
	}
	raw := strings.TrimSpace(headers[0])
	if len(raw) != 131 || raw[0] != '"' || raw[130] != '"' || raw[65] != '.' {
		return "", apperrors.ErrBadRequest
	}
	for i, b := range []byte(raw[1:130]) {
		if i == 64 {
			continue
		}
		if (b < '0' || b > '9') && (b < 'a' || b > 'f') {
			return "", apperrors.ErrBadRequest
		}
	}
	return raw[1:130], nil
}
func (ctrl *Ctrl) GetModelLifecycle(c *fox.Context) (*service.ModelLifecycleReview, error) {
	c.Header("Cache-Control", "private, no-store")
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	result, err := ctrl.service.GetModelLifecycle(c.Request.Context(), currentAuthentication(c), c.Param("model_id"))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
func (ctrl *Ctrl) SetModelLifecycle(c *fox.Context) (*service.ModelLifecycleResult, error) {
	c.Header("Cache-Control", "private, no-store")
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	input, err := decodeModelLifecycleInput(c.Request.Body)
	if err != nil {
		return nil, err
	}
	etag, err := modelLifecycleHeader(c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.SetModelLifecycle(c.Request.Context(), currentAuthentication(c), c.Param("model_id"), etag, input)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.Model.ETag))
	}
	return result, err
}
