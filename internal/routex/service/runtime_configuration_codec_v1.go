package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/miclle/routex/pkg/pricing"
)

const runtimeConfigurationFormatV1 = "routex.gateway-configuration.v1"
const runtimeConfigurationMaxBytesV1 = runtimeProjectionMaxBytes
const runtimeConfigurationMaxEntriesV1 = runtimeProjectionMaxEntries

var errRuntimeConfigurationCodecV1 = errors.New("runtime configuration source unavailable")
var configurationCanonicalIDV1 = regexp.MustCompile(`^[a-z][a-z0-9]{1,15}_[0-7][0-9a-hjkmnp-tv-z]{25}$`)

// encodeRuntimeConfigurationV1 serializes private historical facts. The source
// and authorization digests retain their existing meanings; they are independent
// observations, not recomputed activation evidence. The content digest covers
// every field, including source facts absent from those older digest projections.
func encodeRuntimeConfigurationV1(ctx context.Context, document runtimeConfigurationV1) ([]byte, string, error) {
	if !configurationEncodeBudgetV1(ctx, document) {
		return nil, "", errRuntimeConfigurationCodecV1
	}
	if !validateRuntimeConfigurationV1(document) {
		return nil, "", errRuntimeConfigurationCodecV1
	}
	document.PricingObservationDigest = configurationPricingDigestV1(document.PricingObservation)
	raw, err := json.Marshal(document)
	if err != nil || !configurationJSONBoundV1(ctx, raw) {
		return nil, "", errRuntimeConfigurationCodecV1
	}
	return raw, configurationContentDigestV1(raw), nil
}

// reconstructRuntimeConfigurationV1 has no Service, database, keyring, network
// client, lease or publication input. Missing historical facts cannot be filled
// from current rows. The result cannot be assigned to gatewayRuntime.auth/routes.
func reconstructRuntimeConfigurationV1(ctx context.Context, raw []byte, digest string) (*runtimeConfigurationFactsV1, error) {
	if !runtimeRouteDigest.MatchString(digest) || !configurationJSONBoundV1(ctx, raw) || configurationContentDigestV1(raw) != digest {
		return nil, errRuntimeConfigurationCodecV1
	}
	var document runtimeConfigurationV1
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&document) != nil || !validateRuntimeConfigurationV1(document) || document.PricingObservationDigest != configurationPricingDigestV1(document.PricingObservation) {
		return nil, errRuntimeConfigurationCodecV1
	}
	// Exact canonical reserialization rejects missing/extra/duplicate fields,
	// noncanonical ordering, whitespace, number spellings and trailing documents.
	canonical, err := json.Marshal(document)
	if err != nil || !bytes.Equal(canonical, raw) || ctx.Err() != nil {
		return nil, errRuntimeConfigurationCodecV1
	}
	facts := &runtimeConfigurationFactsV1{Document: document, UnresolvedSecrets: []runtimeConfigurationSecretDependencyV1{}}
	for _, c := range document.Source.Credentials {
		facts.UnresolvedSecrets = append(facts.UnresolvedSecrets, runtimeConfigurationSecretDependencyV1{Kind: "credential", ID: c.ID, Birth: c.CreatedAt, SourceProof: c.SourceProof})
	}
	for _, e := range document.Source.Egresses {
		if e.AuthProof != "" {
			facts.UnresolvedSecrets = append(facts.UnresolvedSecrets, runtimeConfigurationSecretDependencyV1{Kind: "egress", ID: e.ID, Birth: e.CreatedAt, SourceProof: e.AuthProof})
		}
	}
	return facts, nil
}

func configurationContentDigestV1(raw []byte) string {
	sum := sha256.Sum256(append([]byte(runtimeConfigurationFormatV1+"\x00"), raw...))
	return hex.EncodeToString(sum[:])
}
func configurationPricingDigestV1(prices *runtimeConfigurationPricingV1) string {
	raw, _ := json.Marshal(prices)
	sum := sha256.Sum256(append([]byte("routex.gateway-pricing-observation.v1\x00"), raw...))
	return hex.EncodeToString(sum[:])
}
func configurationCipherProofV1(ciphertext string) string {
	if ciphertext == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(ciphertext))
	return hex.EncodeToString(sum[:])
}

// Scan before allocating typed records. Bounds apply to the whole document,
// including metadata, nested maps, scalar entries and all pricing observations.
func configurationJSONBoundV1(ctx context.Context, raw []byte) bool {
	if ctx == nil || ctx.Err() != nil || len(raw) == 0 || len(raw) > runtimeConfigurationMaxBytesV1 {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	depth, entries := 0, 0
	for {
		if ctx.Err() != nil {
			return false
		}
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return depth == 0 && entries > 0
		}
		if err != nil {
			return false
		}
		entries++
		if entries > runtimeConfigurationMaxEntriesV1 {
			return false
		}
		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
			if depth < 0 || depth > 32 {
				return false
			}
		}
	}
}

func validateRuntimeConfigurationV1(document runtimeConfigurationV1) bool {
	if document.Format != runtimeConfigurationFormatV1 || !runtimeRouteDigest.MatchString(document.SourceDigest) || !runtimeRouteDigest.MatchString(document.AuthorizationProjectionDigest) || !runtimeRouteDigest.MatchString(document.NetworkPolicyDigest) || !configurationFrozenValuesV1(reflect.ValueOf(document), "") {
		return false
	}
	s := document.Source
	if !configurationOrderedSourceV1(s, document.PricingObservation) {
		return false
	}
	return configurationReferencesV1(s, document.PricingObservation)
}

// This walker admits only the closed frozen v1 root above; it cannot serialize
// arbitrary snapshots. Domain relationships are checked separately below.
func configurationFrozenValuesV1(v reflect.Value, field string) bool {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return true
		}
		return configurationFrozenValuesV1(v.Elem(), field)
	}
	if v.Type() == reflect.TypeFor[time.Time]() {
		t := v.Interface().(time.Time)
		if !t.IsZero() && (t.Location() != time.UTC || t.Year() < 1 || t.Year() > 9999) {
			return false
		}
		return (!strings.HasSuffix(field, "CreatedAt") && !strings.HasSuffix(field, "Birth")) || !t.IsZero()
	}
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !configurationFrozenValuesV1(v.Field(i), v.Type().Field(i).Name) {
				return false
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if !configurationFrozenValuesV1(v.Index(i), field) {
				return false
			}
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			if iter.Key().String() == "" || !configurationFrozenValuesV1(iter.Value(), field) {
				return false
			}
		}
	case reflect.String:
		text := v.String()
		if !utf8.ValidString(text) {
			return false
		}
		if strings.HasSuffix(field, "PolicyRevision") || strings.HasSuffix(field, "ConfigRevision") || strings.HasSuffix(field, "BindingKey") || field == "NamedIdentityPolicyKey" {
			if text != "" && !runtimeRouteDigest.MatchString(text) {
				return false
			}
		}
		bindingPrefixes := map[string]string{"OIDCBindingID": "oib", "OAuthBindingID": "oab", "LDAPBindingID": "ldb", "SAMLBindingID": "smb", "NamedIdentityBindingID": "nib"}
		if prefix, ok := bindingPrefixes[field]; ok && text != "" && !configurationIDV1(text, prefix) {
			return false
		}
		switch field {
		case "MoneyMonth", "Amount", "Rate":
			if _, err := pricing.Decimal(text); err != nil {
				return false
			}
		case "TransportGeneration", "VerifiedTransportGeneration", "CapabilityTransportGeneration":
			if !validTransportGeneration(text) {
				return false
			}
		case "SecretGeneration":
			if text != "" && !configurationIDV1(text, "sec") {
				return false
			}
		case "EmergencyMFAGeneration":
			if text != "" && !runtimeProjectionMFAGeneration.MatchString(text) {
				return false
			}
		case "ReaderMethod":
			if text != "" && !vaultStoredMethod(text) {
				return false
			}
		case "ReaderGeneration":
			if text != "" && !rootSafeIdentity(text, 30) {
				return false
			}
		case "TokenHash", "SourceProof", "AuthProof", "PasswordDigest", "EmergencyPasswordDigest", "ExpectedMarkerSHA256", "DescriptorSHA256", "SourceContext", "PhysicalObject", "CoverageIntentSHA256":
			if text != "" && !runtimeRouteDigest.MatchString(text) {
				return false
			}
		}
	}
	return true
}

func configurationOrderedV1[T any](values []T, key func(T) string) bool {
	previous := ""
	for _, value := range values {
		current := key(value)
		if current == "" || current <= previous {
			return false
		}
		previous = current
	}
	return true
}
func configurationSortV1[T any](values []T, key func(T) string) {
	slices.SortFunc(values, func(a, b T) int { return strings.Compare(key(a), key(b)) })
}
