package handler

import (
	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/modelreferences"
	"github.com/miclle/routex/internal/routex/service"
	"net/url"
)

func (ctrl *Ctrl) ListModelCreationPublicNames(c *fox.Context) (*service.ModelCreationPublicNames, error) {
	modelCreationPrivate(c)
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return nil, apperrors.ErrBadRequest
	}
	for key, items := range values {
		if key != "q" || len(items) != 1 {
			return nil, apperrors.ErrBadRequest
		}
	}
	query := values.Get("q")
	if !modelreferences.ValidQuery(query) {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.ListModelCreationPublicNames(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("connection_id"), query)
}
