package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"slices"
	"unicode/utf8"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"strings"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/upstream"
	"github.com/miclle/routex/pkg/vault"
)

const vaultCertificatePrefix = "routex-vault-certificate-v1\n"
const vaultSavedPEMBytes = 16 << 10

type vaultCertificateTuple struct {
	AuthMount       string `json:"auth_mount"`
	CertificateRole string `json:"certificate_role"`
	CertificatePEM  string `json:"certificate_pem"`
	PrivateKeyPEM   string `json:"private_key_pem"`
	ServerCAPEM     string `json:"server_ca_pem"`
}

func (vaultCertificateTuple) String() string     { return "Vault certificate material (redacted)" }
func (v vaultCertificateTuple) GoString() string { return v.String() }

func vaultCertificateRole(s string) bool {
	return vaultAppRoleMount(s) && !strings.Contains(s, "/")
}
func vaultPEMShape(s string, optional bool) bool {
	if s == "" {
		return optional
	}
	if len(s) > vaultSavedPEMBytes {
		return false
	}
	for i := range len(s) {
		if (s[i] < 32 && s[i] != '\r' && s[i] != '\n' && s[i] != '\t') || s[i] > 126 {
			return false
		}
	}
	return true
}
func vaultCertificateValidate(v vaultCertificateTuple) error {
	if !vaultAppRoleMount(v.AuthMount) || !vaultCertificateRole(v.CertificateRole) || !vaultPEMShape(v.CertificatePEM, false) || !vaultPEMShape(v.PrivateKeyPEM, false) || !vaultPEMShape(v.ServerCAPEM, true) {
		return vaultUnavailable
	}
	client, err := upstream.NewCertificateClient(true, []byte(v.CertificatePEM), []byte(v.PrivateKeyPEM), []byte(v.ServerCAPEM))
	if err != nil {
		return vaultUnavailable
	}
	client.CloseIdleConnections()
	return nil
}
func vaultCertificateMaterial(value string) (vaultCertificateTuple, error) {
	var v vaultCertificateTuple
	if !strings.HasPrefix(value, vaultCertificatePrefix) || len(value) > 32<<10 {
		return v, vaultUnavailable
	}
	raw := []byte(strings.TrimPrefix(value, vaultCertificatePrefix))
	defer clear(raw)
	f, err := vaultStrict(raw, []string{"auth_mount", "certificate_role", "certificate_pem", "private_key_pem", "server_ca_pem"})
	if err != nil || len(f) != 5 || json.Unmarshal(raw, &v) != nil {
		return vaultCertificateTuple{}, vaultUnavailable
	}
	// Persisted material uses a closed all-string tuple, including explicit empty CA.
	for _, k := range []string{"auth_mount", "certificate_role", "certificate_pem", "private_key_pem", "server_ca_pem"} {
		var s string
		if bytes.Equal(bytes.TrimSpace(f[k]), []byte("null")) || json.Unmarshal(f[k], &s) != nil {
			return vaultCertificateTuple{}, vaultUnavailable
		}
	}
	if err := vaultCertificateValidate(v); err != nil {
		return vaultCertificateTuple{}, err
	}
	return v, nil
}
func vaultCertificateReplacement(input VaultAuthInput) (string, error) {
	if input.Token != "" || input.RoleID != "" || input.SecretID != "" {
		return "", vaultUnavailable
	}
	v := vaultCertificateTuple{input.AuthMount, input.CertificateRole, input.CertificatePEM, input.PrivateKeyPEM, input.ServerCAPEM}
	if err := vaultCertificateValidate(v); err != nil {
		return "", err
	}
	var raw bytes.Buffer
	encoder := json.NewEncoder(&raw)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return "", vaultUnavailable
	}
	value := vaultCertificatePrefix + strings.TrimSuffix(raw.String(), "\n")
	if len(value) > 32<<10 {
		return "", vaultUnavailable
	}
	return value, nil
}
func vaultIdentityClient(rev entity.VaultRevision, allowPrivate bool, method, material string) (*vault.Client, error) {
	if _, err := vaultAuthMaterial(method, material); err != nil {
		return nil, err
	}
	if method != "certificate" {
		return vaultClient(rev, allowPrivate)
	}
	v, err := vaultCertificateMaterial(material)
	if err != nil {
		return nil, err
	}
	client, err := vault.NewCertificate(vaultDescriptor(rev).client(), allowPrivate, []byte(v.CertificatePEM), []byte(v.PrivateKeyPEM), []byte(v.ServerCAPEM))
	if err != nil {
		return nil, vaultUnavailable
	}
	return client, nil
}
func vaultOperatorClient(rev entity.VaultRevision, allowPrivate bool, method, material string) (*vault.Client, error) {
	if method != "certificate" {
		return vaultClient(rev, allowPrivate)
	}
	v, err := vaultCertificateMaterial(material)
	if err != nil {
		return nil, err
	}
	client, err := vault.NewServerCA(vaultDescriptor(rev).client(), allowPrivate, []byte(v.ServerCAPEM))
	if err != nil {
		return nil, vaultUnavailable
	}
	return client, nil
}
func vaultIdentityCommand(ctx context.Context, rev entity.VaultRevision, allowPrivate bool, method, material string) (*vault.Client, string, func(), vault.Observation, error) {
	client, err := vaultIdentityClient(rev, allowPrivate, method, material)
	if err != nil {
		return nil, "", func() {}, vault.Observation{Failure: &vault.Failure{Stage: "prepare", Code: "invalid_auth"}}, err
	}
	token, release, observation, err := vaultCommandToken(ctx, client, method, material)
	return client, token, release, observation, err
}

// Public action/method JSON has different overhead from the private32KiB tuple.
// The enclosing config and each auth object remain bounded by64KiB; the tuple
// encoder independently enforces the encrypted material cap before any write.
func vaultAuthFields(raw []byte) (map[string]json.RawMessage, error) {
	if len(raw) > 64<<10 || !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return nil, apperrors.ErrBadRequest
	}
	allowed := []string{"action", "token", "method", "auth_mount", "role_id", "secret_id", "certificate_role", "certificate_pem", "private_key_pem", "server_ca_pem"}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, apperrors.ErrBadRequest
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || !slices.Contains(allowed, key) {
			return nil, apperrors.ErrBadRequest
		}
		if _, exists := fields[key]; exists {
			return nil, apperrors.ErrBadRequest
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, apperrors.ErrBadRequest
		}
		fields[key] = value
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return nil, apperrors.ErrBadRequest
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, apperrors.ErrBadRequest
	}
	return fields, nil
}
