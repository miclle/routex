package service

import (
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/upstream"
)

func validateConnectionAdapter(c entity.ProviderConnection, allowPrivate bool) error {
	switch entity.ConnectionAdapter(c) {
	case entity.AdapterNative:
		if c.APIVersion != nil {
			return apperrors.ErrBadRequest
		}
	case entity.AdapterAzureOpenAIClassic:
		if c.Protocol != entity.ProtocolOpenAIChat || c.APIVersion == nil || !upstream.ValidAzureAPIVersion(*c.APIVersion) {
			return apperrors.ErrBadRequest
		}
		if _, err := upstream.AzureClassicOrigin(c.BaseURL, allowPrivate); err != nil {
			return apperrors.ErrBadRequest
		}
	default:
		return apperrors.ErrBadRequest
	}
	return nil
}
