package service

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/miclle/routex/pkg/vault"
)

const vaultAppRolePrefix = "routex-vault-approle-v1\n"

type vaultAppRoleMaterial struct {
	AuthMount string `json:"auth_mount"`
	RoleID    string `json:"role_id"`
	SecretID  string `json:"secret_id"`
}

func (vaultAppRoleMaterial) String() string     { return "Vault AppRole material (redacted)" }
func (m vaultAppRoleMaterial) GoString() string { return m.String() }

func vaultAppRoleMount(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, part := range strings.Split(s, "/") {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".") {
			return false
		}
		for i := range len(part) {
			c := part[i]
			if c != '_' && c != '-' && c != '.' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
				return false
			}
		}
	}
	return true
}
func vaultAuthMethod(value string) string {
	if strings.HasPrefix(value, vaultAppRolePrefix) {
		return "approle"
	}
	return "token"
}
func vaultAuthMaterial(method, value string) (vaultAppRoleMaterial, error) {
	var tuple vaultAppRoleMaterial
	if method == "token" {
		if !vaultToken(value) {
			return tuple, vaultUnavailable
		}
		return tuple, nil
	}
	if method != "approle" || !strings.HasPrefix(value, vaultAppRolePrefix) || len(value) > 32<<10 {
		return tuple, vaultUnavailable
	}
	raw := []byte(strings.TrimPrefix(value, vaultAppRolePrefix))
	f, e := vaultStrict(raw, []string{"auth_mount", "role_id", "secret_id"})
	if e != nil || len(f) != 3 || json.Unmarshal(raw, &tuple) != nil || !vaultAppRoleMount(tuple.AuthMount) || !vaultToken(tuple.RoleID) || !vaultToken(tuple.SecretID) {
		return tuple, vaultUnavailable
	}
	return tuple, nil
}
func vaultAuthReplacement(input VaultAuthInput) (string, error) {
	if input.Method == "approle" {
		if input.Token != "" || !vaultAppRoleMount(input.AuthMount) || !vaultToken(input.RoleID) || !vaultToken(input.SecretID) {
			return "", vaultUnavailable
		}
		var raw bytes.Buffer
		encoder := json.NewEncoder(&raw)
		encoder.SetEscapeHTML(false) // Complete printable-ASCII tuples fit the existing 32 KiB root envelope.
		if err := encoder.Encode(vaultAppRoleMaterial{input.AuthMount, input.RoleID, input.SecretID}); err != nil {
			return "", vaultUnavailable
		}
		return vaultAppRolePrefix + strings.TrimSuffix(raw.String(), "\n"), nil
	}
	if (input.Method != "" && input.Method != "token") || !vaultToken(input.Token) || input.AuthMount != "" || input.RoleID != "" || input.SecretID != "" {
		return "", vaultUnavailable
	}
	return input.Token, nil
}

// Login Tokens belong to one finite command, never a revision/cache or retry.
func vaultCommandToken(ctx context.Context, client *vault.Client, method, value string) (string, func(), vault.Observation, error) {
	tuple, e := vaultAuthMaterial(method, value)
	empty := func() {}
	if e != nil {
		o := vault.Observation{Failure: &vault.Failure{Stage: "prepare", Code: "invalid_auth"}}
		return "", empty, o, o.Failure
	}
	if method == "token" {
		return value, empty, vault.Observation{}, nil
	}
	lease, o, e := client.LoginAppRole(ctx, tuple.AuthMount, tuple.RoleID, tuple.SecretID)
	if e != nil {
		return "", empty, o, e
	}
	token, e := lease.Token()
	if e != nil {
		lease.Close()
		o.Succeeded = false
		o.Failure = &vault.Failure{Stage: "prepare", Code: "auth_expired"}
		return "", empty, o, e
	}
	return token, lease.Close, o, nil
}
func vaultLoginFailure(o vault.Observation) VaultObservationView {
	// A login attempt is not a KV attempt. Retain fixed failure/duration only.
	o.Attempted = false
	o.Succeeded = false
	return vaultObservation(o)
}

func vaultStoredMethod(method string) bool { return method == "token" || method == "approle" }
