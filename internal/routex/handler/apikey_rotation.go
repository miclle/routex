package handler

import (
	"net/http"

	"github.com/fox-gonic/fox"
)

type CompletePersonalKeyRotationRequest struct {
	KeyID            string `uri:"key_id" json:"-"`
	ReplacementKeyID string `json:"replacement_key_id"`
}

func (ctrl *Ctrl) CompletePersonalKeyRotation(c *fox.Context, request CompletePersonalKeyRotationRequest) error {
	if err := ctrl.service.CompletePersonalKeyRotation(c.Request.Context(), currentAuthentication(c).User.ID, request.KeyID, request.ReplacementKeyID); err != nil {
		return err
	}
	c.Status(http.StatusNoContent)
	return nil
}
