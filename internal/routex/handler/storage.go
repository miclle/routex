package handler

import (
	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) StorageSettings(c *fox.Context) (*service.StorageView, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.StorageSettings(c.Request.Context(), currentAuthentication(c).User.ID)
}
func (ctrl *Ctrl) WriteStorageSettings(c *fox.Context) (*service.StorageView, error) {
	var input service.StorageInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	return ctrl.service.WriteStorageSettings(c.Request.Context(), currentAuthentication(c).User.ID, input)
}
func (ctrl *Ctrl) TestStorage(c *fox.Context) (*service.StorageProbe, error) {
	var input struct {
		ETag string `json:"etag"`
	}
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	return ctrl.service.TestStorage(c.Request.Context(), currentAuthentication(c).User.ID, input.ETag)
}
func (ctrl *Ctrl) RollbackStorage(c *fox.Context) (*service.StorageView, error) {
	var input service.StorageRollbackInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	return ctrl.service.RollbackStorage(c.Request.Context(), currentAuthentication(c).User.ID, input)
}
