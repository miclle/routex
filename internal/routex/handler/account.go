package handler

import (
	"net/http"
	"time"

	"github.com/fox-gonic/fox"
)

type UpdateProfileRequest struct {
	Name string `json:"name"`
}
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}
type AccountSessionResponse struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Current   bool      `json:"current"`
}
type AccountSessionsResponse struct {
	Items []AccountSessionResponse `json:"items"`
}
type AccountSessionPath struct {
	SessionID string `uri:"session_id" json:"-" binding:"required"`
}

func (ctrl *Ctrl) UpdateProfile(c *fox.Context, request UpdateProfileRequest) (*UserResponse, error) {
	user, err := ctrl.service.UpdateProfile(c.Request.Context(), currentAuthentication(c).User.ID, request.Name)
	if err != nil {
		return nil, err
	}
	return &UserResponse{ID: user.ID, Name: user.Name, Email: user.Email, Role: user.Role}, nil
}
func (ctrl *Ctrl) ChangePassword(c *fox.Context, request ChangePasswordRequest) (*SessionResponse, error) {
	auth, err := ctrl.service.ChangePassword(c.Request.Context(), currentAuthentication(c).User.ID, request.CurrentPassword, request.NewPassword)
	if err != nil {
		return nil, err
	}
	setSessionCookie(c, auth)
	return sessionResponse(auth), nil
}
func (ctrl *Ctrl) ListAccountSessions(c *fox.Context) (*AccountSessionsResponse, error) {
	auth := currentAuthentication(c)
	sessions, err := ctrl.service.ListAccountSessions(c.Request.Context(), auth.User.ID)
	if err != nil {
		return nil, err
	}
	items := make([]AccountSessionResponse, 0, len(sessions))
	for _, s := range sessions {
		items = append(items, AccountSessionResponse{ID: s.ID, CreatedAt: s.CreatedAt, ExpiresAt: s.ExpiresAt, Current: s.ID == auth.Session.ID})
	}
	return &AccountSessionsResponse{Items: items}, nil
}
func (ctrl *Ctrl) RevokeAccountSession(c *fox.Context, request AccountSessionPath) error {
	auth := currentAuthentication(c)
	if err := ctrl.service.RevokeAccountSession(c.Request.Context(), auth.User.ID, request.SessionID); err != nil {
		return err
	}
	if request.SessionID == auth.Session.ID {
		http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: c.Request.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	}
	c.Status(http.StatusNoContent)
	return nil
}
