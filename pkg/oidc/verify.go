package oidc

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

func readKeys(ctx context.Context, client *http.Client, rawURL string) ([]jose.JSONWebKey, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, ErrProtocol
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	var set jose.JSONWebKeySet
	decoder := json.NewDecoder(response.Body)
	if decoder.Decode(&set) != nil || len(set.Keys) == 0 || len(set.Keys) > maxKeys {
		return nil, ErrProtocol
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, ErrProtocol
	}
	keys := make([]jose.JSONWebKey, 0, len(set.Keys))
	for _, key := range set.Keys {
		if key.Use != "" && key.Use != "sig" {
			continue
		}
		if key.Algorithm != "" && len(approvedAlgorithms([]string{key.Algorithm})) == 0 {
			continue
		}
		switch public := key.Key.(type) {
		case *rsa.PublicKey:
			if public.N == nil || public.N.BitLen() < 2048 || public.N.BitLen() > 8192 || public.E < 3 || public.E%2 == 0 {
				continue
			}
			if key.Algorithm != "" && key.Algorithm[:2] != "RS" {
				continue
			}
			keys = append(keys, key)
		case *ecdsa.PublicKey:
			if _, err := public.Bytes(); err != nil {
				continue
			}
			expected := ""
			switch public.Curve.Params().Name {
			case "P-256":
				expected = "ES256"
			case "P-384":
				expected = "ES384"
			case "P-521":
				expected = "ES512"
			}
			if expected == "" || key.Algorithm != "" && key.Algorithm != expected {
				continue
			}
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 || ctx.Err() != nil {
		return nil, ErrProtocol
	}
	return keys, nil
}

func (c *Client) verify(ctx context.Context, raw string, nonce string, keys []jose.JSONWebKey, accessToken string) (Identity, error) {
	selected, err := selectKeys(raw, c.algorithms, keys)
	if err != nil {
		return Identity{}, err
	}
	verifier := coreoidc.NewVerifier(c.config.Issuer, &coreoidc.StaticKeySet{PublicKeys: selected},
		&coreoidc.Config{ClientID: c.config.ClientID, SupportedSigningAlgs: c.algorithms})
	token, err := verifier.Verify(ctx, raw)
	if err != nil || ctx.Err() != nil || token.Issuer != c.config.Issuer || !subject(token.Subject) ||
		subtle.ConstantTimeCompare([]byte(token.Nonce), []byte(nonce)) != 1 {
		return Identity{}, ErrProtocol
	}
	// coreos verifies signatures, issuer, audience and expiry. Supplement its
	// deliberately caller-owned nonce/azp checks and its permissive nbf skew.
	var claims struct {
		AuthorizedParty *string         `json:"azp"`
		IssuedAt        *int64          `json:"iat"`
		ExpiresAt       *int64          `json:"exp"`
		NotBefore       *int64          `json:"nbf"`
		AccessTokenHash json.RawMessage `json:"at_hash"`
	}
	if token.Claims(&claims) != nil || claims.IssuedAt == nil || claims.ExpiresAt == nil {
		return Identity{}, ErrProtocol
	}
	if len(claims.AccessTokenHash) != 0 {
		var hash string
		if json.Unmarshal(claims.AccessTokenHash, &hash) != nil || hash == "" || token.VerifyAccessToken(accessToken) != nil {
			return Identity{}, ErrProtocol
		}
	}
	now := time.Now()
	if *claims.IssuedAt <= 0 || time.Unix(*claims.IssuedAt, 0).After(now) ||
		!time.Unix(*claims.ExpiresAt, 0).After(now) || *claims.ExpiresAt <= *claims.IssuedAt ||
		claims.NotBefore != nil && time.Unix(*claims.NotBefore, 0).After(now) {
		return Identity{}, ErrProtocol
	}
	if len(token.Audience) > 1 && claims.AuthorizedParty == nil ||
		claims.AuthorizedParty != nil && *claims.AuthorizedParty != c.config.ClientID {
		return Identity{}, ErrProtocol
	}
	if ctx.Err() != nil {
		return Identity{}, ErrUnavailable
	}
	return Identity{Issuer: token.Issuer, Subject: token.Subject}, nil
}

func subject(value string) bool {
	if len(value) == 0 || len(value) > 255 {
		return false
	}
	for _, ch := range value {
		if ch < 0x20 || ch > 0x7e {
			return false
		}
	}
	return true
}

// Select only the protected-header key identity and declared algorithm. JOSE
// parsing does not authorize claims: coreos still verifies the selected signature
// and claims below. Compact serialization has no unsigned header source.
func selectKeys(raw string, algorithms []string, keys []jose.JSONWebKey) ([]crypto.PublicKey, error) {
	allowed := make([]jose.SignatureAlgorithm, 0, len(algorithms))
	for _, algorithm := range algorithms {
		allowed = append(allowed, jose.SignatureAlgorithm(algorithm))
	}
	signed, err := jose.ParseSignedCompact(raw, allowed)
	if err != nil || len(signed.Signatures) != 1 {
		return nil, ErrProtocol
	}
	header := signed.Signatures[0].Protected
	selected := make([]crypto.PublicKey, 0, len(keys))
	for _, key := range keys {
		if key.KeyID != header.KeyID || key.Algorithm != "" && key.Algorithm != header.Algorithm {
			continue
		}
		switch public := key.Key.(type) {
		case *rsa.PublicKey:
			selected = append(selected, public)
		case *ecdsa.PublicKey:
			selected = append(selected, public)
		}
	}
	if len(selected) == 0 {
		return nil, ErrProtocol
	}
	return selected, nil
}
