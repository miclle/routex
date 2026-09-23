package handler

import (
	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) SMTPSettings(c *fox.Context) (*service.SMTPView, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.SMTPSettings(c.Request.Context(), currentAuthentication(c).User.ID)
}
func (ctrl *Ctrl) WriteSMTPSettings(c *fox.Context) (*service.SMTPView, error) {
	var input service.SMTPInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	return ctrl.service.WriteSMTPSettings(c.Request.Context(), currentAuthentication(c).User.ID, input)
}
func (ctrl *Ctrl) WriteSMTPSender(c *fox.Context) (*service.SMTPView, error) {
	var input service.SMTPSenderInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	return ctrl.service.WriteSMTPSender(c.Request.Context(), currentAuthentication(c).User.ID, input)
}
func (ctrl *Ctrl) TestSMTP(c *fox.Context) (*service.SMTPTestView, error) {
	var input service.SMTPTestInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	return ctrl.service.TestSMTP(c.Request.Context(), currentAuthentication(c).User.ID, input)
}
