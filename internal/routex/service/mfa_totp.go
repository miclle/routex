package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // RFC 4226/6238 authenticator interoperability uses HMAC-SHA1.
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/miclle/routex/pkg/secret"
)

func mfaTOTP(key []byte, step int64, digits uint32) string {
	counter := make([]byte, 8)
	binary.BigEndian.PutUint64(counter, uint64(step))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(counter)
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 15
	value := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	modulus := uint32(1)
	for range digits {
		modulus *= 10
	}
	return fmt.Sprintf("%0*d", digits, value%modulus)
}
func mfaMatchTOTP(encoded, code string, now time.Time, last int64) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(encoded)
	if err != nil || len(key) != 20 {
		return 0, false
	}
	defer clear(key)
	current := now.Unix() / 30
	matched := int64(-1)
	for _, step := range []int64{current - 1, current, current + 1} {
		if step >= 0 && step > last && subtle.ConstantTimeCompare([]byte(mfaTOTP(key, step, 6)), []byte(code)) == 1 {
			matched = step
		}
	}
	return matched, matched >= 0
}
func newMFASecret() (string, error) {
	data := make([]byte, 20)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	defer clear(data)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(data), nil
}
func mfaProvisioningURI(email, key string) string {
	uri := url.URL{Scheme: "otpauth", Host: "totp", Path: "/RouteX:" + email}
	query := url.Values{"secret": {key}, "issuer": {"RouteX"}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	uri.RawQuery = query.Encode()
	return uri.String()
}
func newMFARecoveryCode() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	defer clear(data)
	value := strings.ToUpper(hex.EncodeToString(data))
	return "RXR-" + value[:8] + "-" + value[8:16] + "-" + value[16:24] + "-" + value[24:], nil
}
func mfaRecoveryDigest(userID, generation, code string) (string, bool) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 39 || !strings.HasPrefix(code, "RXR-") {
		return "", false
	}
	parts := strings.Split(code[4:], "-")
	if len(parts) != 4 {
		return "", false
	}
	for _, part := range parts {
		if len(part) != 8 {
			return "", false
		}
		if _, err := hex.DecodeString(part); err != nil {
			return "", false
		}
	}
	return secret.SHA256Hex("routex:mfa:recovery:" + userID + ":" + generation + ":" + code), true
}
