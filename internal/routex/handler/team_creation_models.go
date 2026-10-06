package handler

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

// A review header selects only a bounded reader, never creation authority.
// Legacy inputs still reject every If-Match before invoking the legacy service.
func jsonTeamCreationRequest(c *fox.Context) error {
	if c.ContentType() != "application/json" {
		return apperrors.ErrBadRequest
	}
	limit := int64(64 << 10)
	if len(c.Request.Header.Values("If-Match")) > 0 {
		limit = 128 << 10
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	c.Next()
	return nil
}
func (ctrl *Ctrl) ListTeamCreationModelCandidates(c *fox.Context) (*service.TeamCreationModelPage, error) {
	q, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return nil, apperrors.ErrBadRequest
	}
	for k, v := range q {
		if (k != "q" && k != "cursor" && k != "limit") || len(v) != 1 {
			return nil, apperrors.ErrBadRequest
		}
	}
	filter := service.TeamCreationModelFilter{Query: q.Get("q"), Cursor: q.Get("cursor")}
	if raw, ok := q["limit"]; ok {
		n, err := strconv.Atoi(raw[0])
		if err != nil || n < 1 || n > 50 {
			return nil, apperrors.ErrBadRequest
		}
		filter.Limit = n
	}
	return ctrl.service.ListTeamCreationModelCandidates(c.Request.Context(), currentAuthentication(c).User.ID, filter)
}
func (ctrl *Ctrl) ReviewTeamCreationModels(c *fox.Context) (*service.TeamCreationModelReview, error) {
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	if len(c.Request.Header.Values("If-Match")) == 0 {
		return nil, &apperrors.Error{Code: http.StatusPreconditionRequired, Message: "If-Match is required"}
	}
	tag, err := personalModelHeader(c)
	if err != nil {
		return nil, err
	}
	var input service.TeamCreationModelReviewInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	return ctrl.service.ReviewTeamCreationModels(c.Request.Context(), currentAuthentication(c).User.ID, tag, input)
}
