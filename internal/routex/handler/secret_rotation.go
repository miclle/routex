package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fox-gonic/fox"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

var (
	secretRotationID   = regexp.MustCompile(`^srt_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
	secretRotationUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	secretRootID       = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

func secretRotationQuery(c *fox.Context) error {
	c.Header("Cache-Control", "private, no-store")
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		return apperrors.ErrBadRequest
	}
	return nil
}

func (ctrl *Ctrl) GetSecretStore(c *fox.Context) (*service.SecretStoreView, error) {
	if err := secretRotationQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetSecretStore(c.Request.Context(), currentAuthentication(c).User.ID)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}

func (ctrl *Ctrl) GetSecretRotation(c *fox.Context) (*service.SecretStoreView, error) {
	if err := secretRotationQuery(c); err != nil {
		return nil, err
	}
	if !secretRotationID.MatchString(c.Param("rotation_id")) {
		return nil, apperrors.ErrBadRequest
	}
	result, err := ctrl.service.GetSecretRotation(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("rotation_id"))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}

func (ctrl *Ctrl) StartSecretRotation(c *fox.Context) error {
	if err := secretRotationQuery(c); err != nil {
		return err
	}
	fields, err := secretRotationBody(c, true)
	if err != nil {
		return err
	}
	etag, err := personalModelHeader(c)
	if err != nil {
		return err
	}
	result, err := ctrl.service.StartSecretRotation(c.Request.Context(), currentAuthentication(c).User.ID, etag, service.SecretRotationStartInput{RequestID: fields["request_id"], TargetKeyID: fields["target_key_id"], Reason: fields["reason"]})
	if err != nil {
		return err
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	// Explicit JSON preserves first-start 201 and historical-receipt 200.
	c.JSON(status, result)
	return nil
}

func (ctrl *Ctrl) ResumeSecretRotation(c *fox.Context) error {
	return ctrl.secretRotationAction(c, "resume")
}
func (ctrl *Ctrl) RetireSecretRotation(c *fox.Context) error {
	return ctrl.secretRotationAction(c, "retire")
}
func (ctrl *Ctrl) RollbackSecretRotation(c *fox.Context) error {
	return ctrl.secretRotationAction(c, "rollback")
}
func (ctrl *Ctrl) secretRotationAction(c *fox.Context, action string) error {
	if err := secretRotationQuery(c); err != nil {
		return err
	}
	if !secretRotationID.MatchString(c.Param("rotation_id")) {
		return apperrors.ErrBadRequest
	}
	fields, err := secretRotationBody(c, false)
	if err != nil {
		return err
	}
	etag, err := personalModelHeader(c)
	if err != nil {
		return err
	}
	result, err := ctrl.service.SecretRotationAction(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("rotation_id"), action, etag, service.SecretRotationActionInput{RequestID: fields["request_id"], Reason: fields["reason"]})
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

// A separate bounded decoder keeps duplicate/null/case-aliased fields and invalid
// Unicode from becoming a different durable intent before Service authorization.
func secretRotationBody(c *fox.Context, start bool) (map[string]string, error) {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, (16<<10)+1))
	if err != nil || len(raw) > 16<<10 || !utf8.Valid(raw) {
		return nil, apperrors.ErrBadRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, apperrors.ErrBadRequest
	}
	fields := make(map[string]string, 3)
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		allowed := name == "request_id" || name == "reason" || start && name == "target_key_id"
		if err != nil || !ok || !allowed {
			return nil, apperrors.ErrBadRequest
		}
		if _, duplicate := fields[name]; duplicate {
			return nil, apperrors.ErrBadRequest
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || !secretRotationUnicode(value) {
			return nil, apperrors.ErrBadRequest
		}
		var text *string
		if err := json.Unmarshal(value, &text); err != nil || text == nil || *text == "" {
			return nil, apperrors.ErrBadRequest
		}
		fields[name] = *text
	}
	if _, err := decoder.Token(); err != nil {
		return nil, apperrors.ErrBadRequest
	}
	if decoder.Decode(new(any)) != io.EOF || !secretRotationUUID.MatchString(fields["request_id"]) || start && !secretRootID.MatchString(fields["target_key_id"]) {
		return nil, apperrors.ErrBadRequest
	}
	reason := fields["reason"]
	if reason == "" || reason != strings.TrimSpace(reason) || utf8.RuneCountInString(reason) > 1000 {
		return nil, apperrors.ErrBadRequest
	}
	for _, ch := range reason {
		if unicode.IsControl(ch) {
			return nil, apperrors.ErrBadRequest
		}
	}
	return fields, nil
}

func secretRotationUnicode(raw []byte) bool {
	// encoding/json replaces unpaired UTF-16 surrogates; reject that lossy form.
	for i := 1; i < len(raw)-1; i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw)-1 || raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw)-1 {
			return false
		}
		code, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return false
		}
		if code < 0xd800 || code > 0xdbff {
			continue
		}
		if i+6 >= len(raw)-1 || raw[i+1] != '\\' || raw[i+2] != 'u' {
			return false
		}
		low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}
