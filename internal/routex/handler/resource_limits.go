package handler

import (
	"strings"

	"github.com/fox-gonic/fox"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func limitTarget(c *fox.Context) service.LimitTarget {
	if key := c.Param("key_id"); key != "" {
		if project := c.Param("project_id"); project != "" {
			return service.LimitTarget{Kind: "project_key", ID: key, ProjectID: project}
		}
		return service.LimitTarget{Kind: "personal_key", ID: key}
	}
	if project := c.Param("project_id"); project != "" {
		return service.LimitTarget{Kind: "project", ID: project}
	}
	return service.LimitTarget{Kind: "user", ID: c.Param("user_id")}
}
func (ctrl *Ctrl) GetResourceLimit(c *fox.Context) (*service.LimitRecord, error) {
	result, err := ctrl.service.GetResourceLimit(c.Request.Context(), currentAuthentication(c).User.ID, limitTarget(c))
	if err == nil {
		c.Header("ETag", `"`+result.ETag+`"`)
	}
	return result, err
}
func (ctrl *Ctrl) SetResourceLimit(c *fox.Context) (*service.LimitRecord, error) {
	var input service.LimitInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	values := c.Request.Header.Values("If-Match")
	if len(values) != 1 {
		return nil, apperrors.ErrBadRequest
	}
	raw := strings.TrimSpace(values[0])
	if len(raw) < 3 || raw[0] != '"' || raw[len(raw)-1] != '"' || strings.Contains(raw[1:len(raw)-1], `"`) {
		return nil, apperrors.ErrBadRequest
	}
	etag := raw[1 : len(raw)-1]
	result, err := ctrl.service.SetResourceLimit(c.Request.Context(), currentAuthentication(c).User.ID, limitTarget(c), etag, input)
	if err == nil {
		c.Header("ETag", `"`+result.ETag+`"`)
	}
	return result, err
}
