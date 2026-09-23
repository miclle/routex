package handler

import (
	"encoding/json"
	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"io"
)

// decodeStrictRequest rejects unsupported fields and trailing JSON values so
// clients cannot mistake discarded intent for an accepted management operation.
func decodeStrictRequest(c *fox.Context, target any) error {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return apperrors.ErrBadRequest
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return apperrors.ErrBadRequest
	}
	return nil
}
