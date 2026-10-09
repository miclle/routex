package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	jose "github.com/go-jose/go-jose/v4"
)

func TestExchangeMatchesProtectedHeaderToExactJWKMetadata(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name                         string
		tokenID, keyID               string
		tokenAlgorithm, keyAlgorithm jose.SignatureAlgorithm
		accepted                     bool
	}{
		{"matching", "current", "current", jose.RS256, jose.RS256, true},
		{"unknown_kid", "obsolete", "current", jose.RS256, jose.RS256, false},
		{"missing_kid", "", "current", jose.RS256, jose.RS256, false},
		{"declared_algorithm_mismatch", "current", "current", jose.RS384, jose.RS256, false},
		{"matching_second_algorithm", "current", "current", jose.RS384, jose.RS384, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newProtocolFixture(t)
			fixture.metadata["id_token_signing_alg_values_supported"] = []string{"RS256", "RS384"}
			options := (&jose.SignerOptions{}).WithType("JWT")
			if test.tokenID != "" {
				options = options.WithHeader("kid", test.tokenID)
			}
			signer, err := jose.NewSigner(jose.SigningKey{Algorithm: test.tokenAlgorithm, Key: key}, options)
			if err != nil {
				t.Fatal(err)
			}
			claims, err := json.Marshal(fixture.claims)
			if err != nil {
				t.Fatal(err)
			}
			signed, err := signer.Sign(claims)
			if err != nil {
				t.Fatal(err)
			}
			fixture.rawToken, err = signed.CompactSerialize()
			if err != nil {
				t.Fatal(err)
			}
			fixture.override = func(request *http.Request) *http.Response {
				if request.URL.String() == testIssuer+"/keys" {
					return responseJSON(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
						{Key: &key.PublicKey, KeyID: test.keyID, Use: "sig", Algorithm: string(test.keyAlgorithm)},
					}})
				}
				return nil
			}
			client := discoverFixture(t, fixture)
			identity, err := client.Exchange(context.Background(), testCallback())
			if test.accepted {
				if err != nil || identity.Subject != "exact-subject" {
					t.Fatal("matching retained JWK rejected", err)
				}
			} else if !errors.Is(err, ErrProtocol) || identity != (Identity{}) {
				t.Fatal("same public key bypassed protected kid/algorithm selection")
			}
			if len(fixture.requests) != 3 {
				t.Fatal("key mismatch triggered retry/fallback")
			}
		})
	}
}

func TestExchangeValidatesPresentAccessTokenHash(t *testing.T) {
	sum := sha256.Sum256([]byte("private-access-token"))
	correct := base64.RawURLEncoding.EncodeToString(sum[:len(sum)/2])
	cases := []struct {
		name              string
		value             any
		present, accepted bool
	}{
		{"absent", nil, false, true},
		{"matching", correct, true, true},
		{"wrong", "different", true, false},
		{"empty", "", true, false},
		{"null", nil, true, false},
		{"wrong_type", []string{correct}, true, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newProtocolFixture(t)
			if test.present {
				fixture.claims["at_hash"] = test.value
			}
			client := discoverFixture(t, fixture)
			identity, err := client.Exchange(context.Background(), testCallback())
			if test.accepted {
				if err != nil || identity != (Identity{Issuer: testIssuer, Subject: "exact-subject"}) {
					t.Fatal("valid optional access token hash rejected", err)
				}
			} else if !errors.Is(err, ErrProtocol) || identity != (Identity{}) {
				t.Fatal("invalid access token hash accepted")
			}
			if len(fixture.requests) != 3 {
				t.Fatal("access token hash failure triggered replay")
			}
		})
	}
}
