package handler

import (
	"errors"
	"net/http"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

// Identity review tokens contain one exact digest; Connection reviews use a different contract.
func identityReviewETag(c *fox.Context) (string, error) {
	headers := c.Request.Header.Values("If-Match")
	if len(headers) != 1 {
		return "", apperrors.ErrBadRequest
	}
	value := headers[0]
	if len(value) != 66 || value[0] != '"' || value[65] != '"' {
		return "", apperrors.ErrBadRequest
	}
	value = value[1:65]
	for _, ch := range value {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return "", apperrors.ErrBadRequest
		}
	}
	return value, nil
}
func identityNoQuery(c *fox.Context) error {
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery || c.Request.ContentLength > 64<<10 {
		return apperrors.ErrBadRequest
	}
	return nil
}
func (ctrl *Ctrl) optionalIdentitySession(c *fox.Context) (*service.Authentication, error) {
	cookie, err := c.Request.Cookie(sessionCookie)
	if errors.Is(err, http.ErrNoCookie) {
		return nil, nil
	}
	if err != nil {
		return nil, apperrors.ErrUnauthorized
	}
	auth, err := ctrl.service.Authenticate(c.Request.Context(), cookie.Value)
	if errors.Is(err, apperrors.ErrUnauthorized) {
		return nil, nil
	}
	return auth, err
}

func identityCompletionResponse(c *fox.Context, kind string, authentication *service.Authentication, challenge *service.MFALoginChallenge) error {
	switch kind {
	case "session":
		if authentication == nil {
			return apperrors.ErrInternal
		}
		setSessionCookie(c, authentication)
		c.JSON(http.StatusOK, sessionResponse(authentication))
	case "challenge":
		if challenge == nil {
			return apperrors.ErrInternal
		}
		c.JSON(http.StatusAccepted, challenge)
	case "bound", "verified":
		c.JSON(http.StatusOK, struct {
			Kind string `json:"kind"`
		}{kind})
	default:
		return apperrors.ErrInternal
	}
	return nil
}

func identityPrivate(c *fox.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "no-referrer")
	c.Next()
}
