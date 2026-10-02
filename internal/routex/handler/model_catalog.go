package handler

import (
	"github.com/fox-gonic/fox"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

type MemberModelCatalogResponse struct {
	Items []service.MemberModelCatalogRecord `json:"items"`
}
type MemberModelCatalogRequest struct {
	ModelID string `uri:"model_id" json:"-"`
}

func (ctrl *Ctrl) ListMemberModelCatalog(c *fox.Context) (*MemberModelCatalogResponse, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	items, err := ctrl.service.ListMemberModelCatalog(c.Request.Context(), currentAuthentication(c).User.ID)
	if err != nil {
		return nil, err
	}
	return &MemberModelCatalogResponse{Items: items}, nil
}

func (ctrl *Ctrl) GetMemberModelCatalog(c *fox.Context, request MemberModelCatalogRequest) (*service.MemberModelCatalogRecord, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.GetMemberModelCatalog(c.Request.Context(), currentAuthentication(c).User.ID, request.ModelID)
}
