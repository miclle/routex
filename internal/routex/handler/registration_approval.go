package handler

import (
	"encoding/json"
	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"io"
	"net/http"
)

// Root registration installs this before Session/CSRF/permission middleware.
func registrationApprovalResponseHeaders(c *fox.Context) error {
	c.Header("Cache-Control", "private, no-store")
	c.Next()
	return nil
}
func registrationApprovalBody(c *fox.Context, input any) error {
	if c.Request.URL.RawQuery != "" {
		return apperrors.ErrBadRequest
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 64*1024+1))
	if err != nil || len(raw) > 64*1024 || json.Unmarshal(raw, input) != nil {
		return apperrors.ErrBadRequest
	}
	return nil
}
func (ctrl *Ctrl) GetMemberApproval(c *fox.Context) (*service.MemberApprovalRecord, error) {
	c.Header("Cache-Control", "private, no-store")
	userID, err := memberOverviewSubject(c)
	if err != nil {
		return nil, err
	}
	r, err := ctrl.service.GetMemberApproval(c.Request.Context(), currentAuthentication(c).User.ID, userID)
	if err != nil {
		return nil, err
	}
	c.Header("ETag", `"`+r.ReviewETag+`"`)
	return r, nil
}
func (ctrl *Ctrl) SetMemberApproval(c *fox.Context) (*service.MemberApprovalResult, error) {
	c.Header("Cache-Control", "private, no-store")
	userID, err := memberOverviewSubject(c)
	if err != nil {
		return nil, err
	}
	etag, err := memberMetadataETag(c)
	if err != nil {
		return nil, err
	}
	var input service.MemberApprovalInput
	if err := registrationApprovalBody(c, &input); err != nil {
		return nil, err
	}
	return ctrl.service.SetMemberApproval(c.Request.Context(), currentAuthentication(c).User.ID, userID, etag, input)
}
func (ctrl *Ctrl) GetRegistrationPolicy(c *fox.Context) (*service.RegistrationPolicy, error) {
	c.Header("Cache-Control", "private, no-store")
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	v, err := ctrl.service.GetRegistrationPolicy(c.Request.Context(), currentAuthentication(c).User.ID)
	if err != nil {
		return nil, err
	}
	c.Header("ETag", `"`+v.ReviewETag+`"`)
	return v, nil
}
func (ctrl *Ctrl) SetRegistrationPolicy(c *fox.Context) (*service.RegistrationPolicyResult, error) {
	c.Header("Cache-Control", "private, no-store")
	etag, err := memberMetadataETag(c)
	if err != nil {
		return nil, err
	}
	var input service.RegistrationPolicyInput
	if err := registrationApprovalBody(c, &input); err != nil {
		return nil, err
	}
	v, err := ctrl.service.SetRegistrationPolicy(c.Request.Context(), currentAuthentication(c).User.ID, etag, input)
	if err != nil {
		return nil, err
	}
	c.Header("ETag", `"`+v.ReviewETag+`"`)
	return v, nil
}
func (ctrl *Ctrl) registrationPending(c *fox.Context) {
	c.JSON(http.StatusAccepted, struct {
		Kind string `json:"kind"`
	}{"approval_pending"})
}
