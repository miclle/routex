package service

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/vault"
)

type VaultDescriptor struct {
	Endpoint  string `json:"endpoint"`
	Namespace string `json:"namespace"`
	Mount     string `json:"mount"`
	Prefix    string `json:"prefix"`
	DataField string `json:"data_field"`
}

func (d VaultDescriptor) client() vault.Descriptor {
	return vault.Descriptor{Endpoint: d.Endpoint, Namespace: d.Namespace, Mount: d.Mount, Prefix: d.Prefix, DataField: d.DataField}
}

type VaultAuthInput struct {
	Action string `json:"action"`
	Token  string `json:"token,omitempty"`
}
type VaultConfigInput struct {
	RequestID  string          `json:"request_id"`
	Name       string          `json:"name"`
	Descriptor VaultDescriptor `json:"descriptor"`
	WriterAuth VaultAuthInput  `json:"writer_auth"`
	ReaderAuth VaultAuthInput  `json:"reader_auth"`
	Reason     string          `json:"reason"`
}
type VaultStageInput struct {
	RequestID string `json:"request_id"`
	Reason    string `json:"reason"`
}
type VaultAuthView struct {
	Method     string `json:"method"`
	Configured bool   `json:"configured"`
}
type VaultIntegrationView struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	RevisionID string          `json:"revision_id"`
	Descriptor VaultDescriptor `json:"descriptor"`
	WriterAuth VaultAuthView   `json:"writer_auth"`
	ReaderAuth VaultAuthView   `json:"reader_auth"`
	ReviewETag string          `json:"review_etag"`
	CanWrite   bool            `json:"can_write"`
	CanTest    bool            `json:"can_test"`
	LastProbe  *VaultProbeView `json:"last_probe"`
}
type VaultIntegrationPage struct {
	Items      []VaultIntegrationView `json:"items"`
	NextCursor *string                `json:"next_cursor"`
	ReviewETag string                 `json:"review_etag"`
	CanWrite   bool                   `json:"can_write"`
	CanTest    bool                   `json:"can_test"`
}
type VaultConfigResult struct {
	RequestID     string `json:"request_id"`
	IntegrationID string `json:"integration_id"`
	RevisionID    string `json:"revision_id"`
	Committed     bool   `json:"committed"`
	Changed       bool   `json:"changed"`
}
type VaultFailureView struct {
	Stage      string `json:"stage"`
	Code       string `json:"code"`
	HTTPStatus int    `json:"http_status"`
}
type VaultObservationView struct {
	Attempted  bool              `json:"attempted"`
	Succeeded  bool              `json:"succeeded"`
	DurationMS string            `json:"duration_ms"`
	Failure    *VaultFailureView `json:"failure"`
}
type VaultCleanupView struct {
	State       string               `json:"state"`
	Observation VaultObservationView `json:"observation"`
}
type VaultProbeView struct {
	ID            string               `json:"id"`
	RequestID     string               `json:"request_id"`
	IntegrationID string               `json:"integration_id"`
	RevisionID    string               `json:"revision_id"`
	ReviewETag    string               `json:"review_etag"`
	State         string               `json:"state"`
	Version       *int64               `json:"version"`
	Write         VaultObservationView `json:"write"`
	Read          VaultObservationView `json:"read"`
	Cleanup       VaultCleanupView     `json:"cleanup"`
	CreatedAt     time.Time            `json:"created_at"`
	FinishedAt    *time.Time           `json:"finished_at"`
}

func vaultObservation(o vault.Observation) VaultObservationView {
	v := VaultObservationView{Attempted: o.Attempted, Succeeded: o.Succeeded, DurationMS: strconv.FormatInt(max(0, o.Duration.Milliseconds()), 10)}
	if o.Failure != nil {
		v.Failure = &VaultFailureView{o.Failure.Stage, o.Failure.Code, o.Failure.HTTPStatus}
	}
	return v
}
func vaultCleanup(c vault.Cleanup) VaultCleanupView {
	return VaultCleanupView{c.State, vaultObservation(c.Observation)}
}
func vaultToken(s string) bool {
	if len(s) < 1 || len(s) > 4096 {
		return false
	}
	for i := range len(s) {
		if s[i] < 33 || s[i] > 126 {
			return false
		}
	}
	return true
}
func vaultStrict(raw []byte, fields []string) (map[string]json.RawMessage, error) {
	if len(raw) > 64<<10 || !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return nil, apperrors.ErrBadRequest
	}
	return decodeDefaultLimitObject(raw, fields)
}
func (d *VaultDescriptor) UnmarshalJSON(raw []byte) error {
	f, e := vaultStrict(raw, []string{"endpoint", "namespace", "mount", "prefix", "data_field"})
	if e != nil {
		return e
	}
	var v VaultDescriptor
	for k, p := range map[string]*string{"endpoint": &v.Endpoint, "namespace": &v.Namespace, "mount": &v.Mount, "prefix": &v.Prefix, "data_field": &v.DataField} {
		if bytes.Equal(bytes.TrimSpace(f[k]), []byte("null")) || json.Unmarshal(f[k], p) != nil {
			return apperrors.ErrBadRequest
		}
	}
	*d = v
	return nil
}
func (a *VaultAuthInput) UnmarshalJSON(raw []byte) error {
	f, e := vaultStrict(raw, []string{"action"})
	if e != nil {
		f, e = vaultStrict(raw, []string{"action", "token"})
	}
	if e != nil {
		return e
	}
	var v VaultAuthInput
	if bytes.Equal(bytes.TrimSpace(f["action"]), []byte("null")) || json.Unmarshal(f["action"], &v.Action) != nil {
		return apperrors.ErrBadRequest
	}
	token, present := f["token"]
	if present && (bytes.Equal(bytes.TrimSpace(token), []byte("null")) || json.Unmarshal(token, &v.Token) != nil) {
		return apperrors.ErrBadRequest
	}
	if v.Action == "replace" {
		if !present || !vaultToken(v.Token) {
			return apperrors.ErrBadRequest
		}
	} else if (v.Action != "keep" && v.Action != "remove") || present {
		return apperrors.ErrBadRequest
	}
	*a = v
	return nil
}
func (v *VaultConfigInput) UnmarshalJSON(raw []byte) error {
	f, e := vaultStrict(raw, []string{"request_id", "name", "descriptor", "writer_auth", "reader_auth", "reason"})
	if e != nil {
		return e
	}
	var n VaultConfigInput
	for k, p := range map[string]any{"request_id": &n.RequestID, "name": &n.Name, "descriptor": &n.Descriptor, "writer_auth": &n.WriterAuth, "reader_auth": &n.ReaderAuth, "reason": &n.Reason} {
		if bytes.Equal(bytes.TrimSpace(f[k]), []byte("null")) || json.Unmarshal(f[k], p) != nil {
			return apperrors.ErrBadRequest
		}
	}
	if !credentialReplacementRequestID.MatchString(n.RequestID) || !validCatalogLabel(n.Name) || !rootReason(n.Reason) || strings.TrimSpace(n.Reason) != n.Reason {
		return apperrors.ErrBadRequest
	}
	*v = n
	return nil
}
func (v *VaultStageInput) UnmarshalJSON(raw []byte) error {
	f, e := vaultStrict(raw, []string{"request_id", "reason"})
	if e != nil {
		return e
	}
	var n VaultStageInput
	if json.Unmarshal(f["request_id"], &n.RequestID) != nil || json.Unmarshal(f["reason"], &n.Reason) != nil || !credentialReplacementRequestID.MatchString(n.RequestID) || !rootReason(n.Reason) || strings.TrimSpace(n.Reason) != n.Reason {
		return apperrors.ErrBadRequest
	}
	*v = n
	return nil
}
