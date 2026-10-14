package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"unicode/utf8"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func decodeRuntimeArchiveCleanupInput(raw []byte, apply bool) (service.RuntimeArchiveCleanupInput, error) {
	var input service.RuntimeArchiveCleanupInput
	if len(raw) == 0 || len(raw) > 8192 || !utf8.Valid(raw) {
		return input, apperrors.ErrBadRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return input, apperrors.ErrBadRequest
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, e := decoder.Token()
		name, ok := token.(string)
		if e != nil || !ok || seen[name] || name != "archive_ids" && (!apply || name != "request_id" && name != "reason") {
			return input, apperrors.ErrBadRequest
		}
		seen[name] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return input, apperrors.ErrBadRequest
		}
		if name == "archive_ids" {
			var entries []json.RawMessage
			if json.Unmarshal(value, &entries) != nil || len(entries) < 1 || len(entries) > 20 {
				return input, apperrors.ErrBadRequest
			}
			for _, entry := range entries {
				var identity string
				if !secretRotationUnicode(bytes.TrimSpace(entry)) || bytes.Equal(bytes.TrimSpace(entry), []byte("null")) || json.Unmarshal(entry, &identity) != nil {
					return input, apperrors.ErrBadRequest
				}
				input.ArchiveIDs = append(input.ArchiveIDs, identity)
			}
		} else {
			var text string
			if !secretRotationUnicode(bytes.TrimSpace(value)) || json.Unmarshal(value, &text) != nil {
				return input, apperrors.ErrBadRequest
			}
			if name == "request_id" {
				input.RequestID = text
			} else {
				input.Reason = text
			}
		}
	}
	end, err := decoder.Token()
	required := 1
	if apply {
		required = 3
	}
	if err != nil || end != json.Delim('}') || decoder.Decode(new(any)) != io.EOF || len(seen) != required || !seen["archive_ids"] {
		return input, apperrors.ErrBadRequest
	}
	return input, nil
}

func runtimeArchiveCleanupBody(c *fox.Context, apply bool) (service.RuntimeArchiveCleanupInput, error) {
	if err := runtimePublicationRequest(c); err != nil {
		return service.RuntimeArchiveCleanupInput{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 8193))
	if err != nil {
		return service.RuntimeArchiveCleanupInput{}, apperrors.ErrBadRequest
	}
	return decodeRuntimeArchiveCleanupInput(raw, apply)
}

func (ctrl *Ctrl) PreviewRuntimeArchiveCleanup(c *fox.Context) (*service.RuntimeArchiveCleanupPreview, error) {
	input, err := runtimeArchiveCleanupBody(c, false)
	if err != nil {
		return nil, err
	}
	preview, err := ctrl.service.PreviewRuntimeArchiveCleanup(c.Request.Context(), currentAuthentication(c), service.RuntimeArchiveCleanupSelection{ArchiveIDs: input.ArchiveIDs})
	if err == nil {
		c.Header("ETag", strconv.Quote(preview.ETag))
	}
	return preview, err
}

func (ctrl *Ctrl) CleanupRuntimeArchives(c *fox.Context) (*service.RuntimeArchiveCleanupReceipt, error) {
	input, err := runtimeArchiveCleanupBody(c, true)
	if err != nil {
		return nil, err
	}
	etag, err := memberMetadataETag(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.CleanupRuntimeArchives(c.Request.Context(), currentAuthentication(c), etag, input)
}
