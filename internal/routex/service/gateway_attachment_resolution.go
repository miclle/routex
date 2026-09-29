package service

import (
	"context"
	"errors"
	"net/http"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func validateGatewayAttachmentRoute(plan *gatewayAttachmentPlan, route *gatewayRoute) error {
	for _, ref := range plan.Occurrences {
		switch ref.Kind {
		case gatewayAttachmentImage:
			if !route.SupportsImageInput {
				return gatewayError(http.StatusBadRequest, "attachment_type_unsupported", "The selected route does not support image input.")
			}
		case gatewayAttachmentPDF:
			if !route.SupportsPDFInput {
				return gatewayError(http.StatusBadRequest, "attachment_type_unsupported", "The selected route does not support PDF input.")
			}
		default:
			return unsupportedGatewayAttachmentReference()
		}
	}
	return nil
}

func (s *Service) resolveGatewayAttachments(ctx context.Context, ownerID string, plan *gatewayAttachmentPlan) (map[string]gatewayAttachmentData, error) {
	resolved := make(map[string]gatewayAttachmentData, len(plan.Occurrences))
	for _, objectID := range plan.UniqueObjectIDs() {
		if err := ctx.Err(); err != nil {
			return nil, gatewayAttachmentContextError(err)
		}
		row, data, err := s.readOwnedAttachment(ctx, ownerID, objectID)
		if err != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				return nil, gatewayAttachmentContextError(contextErr)
			}
			return nil, gatewayAttachmentReadError(err)
		}
		resolved[objectID] = gatewayAttachmentData{
			ObjectID: row.ID,
			Name:     row.Name,
			MIME:     row.MIME,
			Data:     data,
		}
	}
	return resolved, nil
}

func gatewayAttachmentContextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return gatewayError(http.StatusGatewayTimeout, "attachment_storage_timeout", "The attachment read timed out.")
	}
	return gatewayError(http.StatusBadGateway, "canceled", "The attachment read was canceled.")
}

func gatewayAttachmentReadError(err error) error {
	var app *apperrors.Error
	if errors.As(err, &app) && (app.Code == http.StatusNotFound || app.Code == http.StatusUnauthorized) {
		return gatewayError(http.StatusNotFound, "attachment_not_found", "The attachment is unavailable or does not belong to this API key owner.")
	}
	return gatewayError(http.StatusServiceUnavailable, "attachment_storage_unavailable", "The attachment could not be read from storage.")
}
