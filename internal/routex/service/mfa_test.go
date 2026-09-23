package service

import (
	"bytes"
	"encoding/base32"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/pkg/secretstore"
)

func TestMFATOTPVectors(t *testing.T) {
	// SHA1 vectors from RFC6238 AppendixB; production uses six digits.
	key := []byte("12345678901234567890")
	for _, test := range []struct {
		seconds int64
		want    string
	}{{59, "94287082"}, {1111111109, "07081804"}, {1111111111, "14050471"}, {1234567890, "89005924"}, {2000000000, "69279037"}, {20000000000, "65353130"}} {
		if got := mfaTOTP(key, test.seconds/30, 8); got != test.want {
			t.Fatalf("RFC6238 mismatch at %d", test.seconds)
		}
	}
	// HOTP truncation vectors cover leading-zero formatting in the six-digit mode.
	for counter, want := range []string{"755224", "287082", "359152", "969429", "338314", "254676", "287922", "162583", "399871", "520489"} {
		if got := mfaTOTP(key, int64(counter), 6); got != want {
			t.Fatalf("RFC4226 mismatch at %d", counter)
		}
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key)
	now := time.Unix(1234567890, 0)
	step := now.Unix() / 30
	for _, offset := range []int64{-1, 0, 1} {
		code := mfaTOTP(key, step+offset, 6)
		matched, ok := mfaMatchTOTP(encoded, code, now, -1)
		if !ok || matched != step+offset {
			t.Fatal("valid clock tolerance rejected")
		}
		if _, ok := mfaMatchTOTP(encoded, code, now, matched); ok {
			t.Fatal("accepted TOTP step replayed")
		}
	}
	for _, code := range []string{"12345", "1234567", "abcdef", "１２３４５６", mfaTOTP(key, step-2, 6), mfaTOTP(key, step+2, 6)} {
		if _, ok := mfaMatchTOTP(encoded, code, now, -1); ok {
			t.Fatal("invalid/outside-window proof accepted")
		}
	}
	if _, ok := mfaMatchTOTP("not-base32", "123456", now, -1); ok {
		t.Fatal("malformed secret accepted")
	}
}
func TestMFASecretAndRecoveryStorage(t *testing.T) {
	first, err := newMFASecret()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newMFASecret()
	if err != nil || first == second || len(first) != 32 {
		t.Fatal("secrets not independent")
	}
	store, err := secretstore.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := store.Seal("mfa:usr_a:generation", first)
	if err != nil || strings.Contains(ciphertext, first) {
		t.Fatal("secret not sealed")
	}
	plain, err := store.Open("mfa:usr_a:generation", ciphertext)
	if err != nil || plain != first {
		t.Fatal("sealed secret cannot reopen")
	}
	if _, err := store.Open("mfa:usr_other:generation", ciphertext); err == nil {
		t.Fatal("MFA secret moved between accounts")
	}
	uri, err := url.Parse(mfaProvisioningURI("person+test@example.invalid", first))
	if err != nil || uri.Scheme != "otpauth" || uri.Query().Get("secret") != first || uri.Query().Get("issuer") != "RouteX" || uri.Query().Get("digits") != "6" || uri.Query().Get("period") != "30" {
		t.Fatal("provisioning URI not interoperable")
	}
	seen := map[string]bool{}
	for range 100 {
		code, err := newMFARecoveryCode()
		if err != nil || seen[code] {
			t.Fatal("recovery generation failed")
		}
		seen[code] = true
		digest, ok := mfaRecoveryDigest("usr_a", "gen_a", code)
		if !ok || len(digest) != 64 || strings.Contains(digest, code) {
			t.Fatal("recovery code not hashed")
		}
		lower, ok := mfaRecoveryDigest("usr_a", "gen_a", strings.ToLower(code))
		if !ok || lower != digest {
			t.Fatal("canonical recovery formatting changed digest")
		}
		other, _ := mfaRecoveryDigest("usr_b", "gen_a", code)
		generation, _ := mfaRecoveryDigest("usr_a", "gen_b", code)
		if other == digest || generation == digest {
			t.Fatal("recovery digest is not domain-bound")
		}
	}
	for _, code := range []string{"", "RXR-00000000-00000000-00000000", "RXR-zzzzzzzz-00000000-00000000-00000000", strings.Repeat("0", 39)} {
		if _, ok := mfaRecoveryDigest("usr_a", "gen_a", code); ok {
			t.Fatal("malformed recovery accepted")
		}
	}
}
