package handler

import (
	"strconv"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) PreviewRuntimeArchiveRollback(c *fox.Context) (*service.RuntimeArchiveRollbackPreview, error) {
	if err := runtimePublicationRequest(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.PreviewRuntimeArchiveRollback(c.Request.Context(), currentAuthentication(c), c.Param("archive_id"))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
func (ctrl *Ctrl) RollbackRuntimeArchive(c *fox.Context) (*service.RuntimeArchiveRollbackReceipt, error) {
	if err := runtimePublicationRequest(c); err != nil {
		return nil, err
	}
	// Reuse the strict bounded UUID/reason object grammar; duplicate, missing,
	// unknown, null and non-string values are rejected before service dispatch.
	input, err := runtimePublicationBody(c)
	if err != nil {
		return nil, err
	}
	etag, err := memberMetadataETag(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.RollbackRuntimeArchive(c.Request.Context(), currentAuthentication(c), c.Param("archive_id"), etag, service.RuntimeArchiveRollbackInput(input))
}
func (ctrl *Ctrl) GetRuntimeArchiveRollback(c *fox.Context) (*service.RuntimeArchiveRollbackReceipt, error) {
	if err := runtimePublicationRequest(c); err != nil {
		return nil, err
	}
	return ctrl.service.GetRuntimeArchiveRollback(c.Request.Context(), currentAuthentication(c), c.Param("request_id"))
}
