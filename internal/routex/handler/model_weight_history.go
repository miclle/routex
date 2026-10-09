package handler

import (
	"io"
	"net/url"
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func modelWeightRequest(c *fox.Context) (url.Values, error) {
	c.Header("Cache-Control", "private, no-store")
	if !memberOverviewUserID.MatchString(c.Param("model_id")) || len(c.Param("model_id")) < 5 || c.Param("model_id")[:4] != "mdl_" {
		return nil, apperrors.ErrBadRequest
	}
	q, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || len(c.Request.URL.RawQuery) > 512 {
		return nil, apperrors.ErrBadRequest
	}
	for _, v := range q {
		if len(v) != 1 || v[0] == "" {
			return nil, apperrors.ErrBadRequest
		}
	}
	return q, nil
}
func (ctrl *Ctrl) ListModelWeightVersions(c *fox.Context) (*service.ModelWeightVersionPage, error) {
	q, err := modelWeightRequest(c)
	if err != nil {
		return nil, err
	}
	f := service.ModelWeightHistoryFilter{Limit: 20}
	for key, value := range q {
		switch key {
		case "limit":
			n, e := strconv.Atoi(value[0])
			if e != nil || n < 1 || n > 100 || strconv.Itoa(n) != value[0] {
				return nil, apperrors.ErrBadRequest
			}
			f.Limit = n
		case "cursor":
			if len(value[0]) > 200 {
				return nil, apperrors.ErrBadRequest
			}
			f.Cursor = value[0]
		default:
			return nil, apperrors.ErrBadRequest
		}
	}
	return ctrl.service.ListModelWeightVersions(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("model_id"), f)
}
func (ctrl *Ctrl) GetModelWeightVersion(c *fox.Context) (*service.ModelWeightVersionDetail, error) {
	q, err := modelWeightRequest(c)
	if err != nil {
		return nil, err
	}
	if len(q) != 0 {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.GetModelWeightVersion(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("model_id"), c.Param("version_id"))
}
func (ctrl *Ctrl) ReviewModelWeightRollback(c *fox.Context) (*service.ModelWeightRollbackReview, error) {
	q, err := modelWeightRequest(c)
	if err != nil {
		return nil, err
	}
	if len(q) != 1 || q.Get("version_id") == "" {
		return nil, apperrors.ErrBadRequest
	}
	result, err := ctrl.service.ReviewModelWeightRollback(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("model_id"), q.Get("version_id"))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) RollbackModelWeights(c *fox.Context) (*service.ModelWeightRollbackResult, error) {
	q, err := modelWeightRequest(c)
	if err != nil {
		return nil, err
	}
	if len(q) != 0 {
		return nil, apperrors.ErrBadRequest
	}
	etag, err := memberMetadataETag(c)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 32*1024+1))
	if err != nil || len(raw) > 32*1024 {
		return nil, apperrors.ErrBadRequest
	}
	var input service.ModelWeightRollbackInput
	if err := input.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	return ctrl.service.RollbackModelWeights(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("model_id"), etag, input)
}
func (ctrl *Ctrl) GetModelWeightRollbackReceipt(c *fox.Context) (*service.ModelWeightRollbackResult, error) {
	q, err := modelWeightRequest(c)
	if err != nil {
		return nil, err
	}
	if len(q) != 0 {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.GetModelWeightRollbackReceipt(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("model_id"), c.Param("request_id"))
}
