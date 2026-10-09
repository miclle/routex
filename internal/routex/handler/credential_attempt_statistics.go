package handler

import (
	"net/url"
	"strings"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func credentialAttemptStatisticsRequest(c *fox.Context) ([]string, error) {
	c.Header("Cache-Control", "private, no-store")
	target := c.Param("provider_id")
	if !memberOverviewUserID.MatchString(target) || !strings.HasPrefix(target, "prv_") ||
		len(c.Request.URL.RawQuery) > 2048 || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		return nil, apperrors.ErrBadRequest
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || len(query) != 1 {
		return nil, apperrors.ErrBadRequest
	}
	ids, ok := query["credential_id"]
	if !ok || len(ids) == 0 || len(ids) > 20 {
		return nil, apperrors.ErrBadRequest
	}
	seen := make(map[string]bool, len(ids))
	for _, value := range ids {
		if !memberOverviewUserID.MatchString(value) || !strings.HasPrefix(value, "crd_") || seen[value] {
			return nil, apperrors.ErrBadRequest
		}
		seen[value] = true
	}
	return ids, nil
}

func (ctrl *Ctrl) GetCredentialAttemptStatistics(c *fox.Context) (*service.CredentialAttemptStatisticsBatch, error) {
	ids, err := credentialAttemptStatisticsRequest(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.GetCredentialAttemptStatistics(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("provider_id"), ids)
}
