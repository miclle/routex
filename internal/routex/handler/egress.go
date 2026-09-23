package handler

import (
	"net/http"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

type EgressPath struct {
	ID string `uri:"egress_id" json:"-"`
}
type ConnectionEgressPath struct {
	ID string `uri:"connection_id" json:"-"`
}
type EgressDraftRequest struct {
	EgressID string `json:"egress_id,omitempty"`
	service.EgressInput
}

func (ctrl *Ctrl) ListEgresses(c *fox.Context) (*service.EgressPage, error) {
	if len(c.Request.URL.Query()) != 0 {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.ListEgresses(c.Request.Context(), currentAuthentication(c).User.ID, false)
}
func (ctrl *Ctrl) EgressOptions(c *fox.Context) (*service.EgressPage, error) {
	if len(c.Request.URL.Query()) != 0 {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.ListEgresses(c.Request.Context(), currentAuthentication(c).User.ID, true)
}
func (ctrl *Ctrl) CreateEgress(c *fox.Context) error {
	var input service.EgressInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return err
	}
	result, err := ctrl.service.WriteEgress(c.Request.Context(), currentAuthentication(c).User.ID, "", input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, result)
	return nil
}
func (ctrl *Ctrl) UpdateEgress(c *fox.Context, path EgressPath) (*service.EgressView, error) {
	var input service.EgressInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	return ctrl.service.WriteEgress(c.Request.Context(), currentAuthentication(c).User.ID, path.ID, input)
}
func (ctrl *Ctrl) TestEgressDraft(c *fox.Context) (*service.EgressDiagnosticView, error) {
	var input EgressDraftRequest
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	return ctrl.service.TestEgressDraft(c.Request.Context(), currentAuthentication(c).User.ID, input.EgressID, input.EgressInput)
}
func (ctrl *Ctrl) TestEgress(c *fox.Context, path EgressPath) (*service.EgressDiagnosticView, error) {
	var input service.EgressDiagnosticInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	return ctrl.service.TestEgress(c.Request.Context(), currentAuthentication(c).User.ID, path.ID, input)
}
func (ctrl *Ctrl) EgressDefault(c *fox.Context) (*service.EgressDefaultView, error) {
	if len(c.Request.URL.Query()) != 0 {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.EgressDefault(c.Request.Context(), currentAuthentication(c).User.ID)
}
func (ctrl *Ctrl) SetEgressDefault(c *fox.Context) (*service.EgressDefaultView, error) {
	var input service.EgressDefaultView
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	return ctrl.service.SetEgressDefault(c.Request.Context(), currentAuthentication(c).User.ID, input)
}
func (ctrl *Ctrl) SetConnectionEgress(c *fox.Context, path ConnectionEgressPath) (*service.ConnectionEgressView, error) {
	var input service.ConnectionEgressInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	return ctrl.service.SetConnectionEgress(c.Request.Context(), currentAuthentication(c).User.ID, path.ID, input)
}
