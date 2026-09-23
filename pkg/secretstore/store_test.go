package secretstore

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(bytes.Repeat([]byte{42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRoundTripAndRandomEnvelopes(t *testing.T) {
	s := testStore(t)
	plaintext := "test-credential-秘密\x00with-binary"
	first, err := s.Seal("credential-1", plaintext)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Seal("credential-1", plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || strings.Contains(first, plaintext) {
		t.Fatal("encryption must be randomized and must not expose plaintext")
	}
	for _, ciphertext := range []string{first, second} {
		got, err := s.Open("credential-1", ciphertext)
		if err != nil || got != plaintext {
			t.Fatal("credential did not round trip")
		}
	}
	var a, b envelope
	for ciphertext, target := range map[string]*envelope{first: &a, second: &b} {
		raw, err := base64.RawURLEncoding.DecodeString(ciphertext)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatal(err)
		}
	}
	if bytes.Equal(a.Key, b.Key) || bytes.Equal(a.Nonce, b.Nonce) || bytes.Equal(a.Data, b.Data) {
		t.Fatal("envelopes must use independent keys and nonces")
	}
}

func TestEnvelopeAuthentication(t *testing.T) {
	s := testStore(t)
	ciphertext, err := s.Seal("private-reference", "private-plaintext")
	if err != nil {
		t.Fatal(err)
	}
	wrongKey, err := New(bytes.Repeat([]byte{43}, 32))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		store     *Store
		reference string
		value     string
	}{
		"wrong root":         {wrongKey, "private-reference", ciphertext},
		"wrong reference":    {s, "other-reference", ciphertext},
		"missing reference":  {s, "", ciphertext},
		"missing ciphertext": {s, "private-reference", ""},
		"invalid encoding":   {s, "private-reference", "not:base64"},
		"nil store":          {nil, "private-reference", ciphertext},
		"zero store":         {new(Store), "private-reference", ciphertext},
	}
	for name, change := range map[string]func(*envelope){
		"version":     func(v *envelope) { v.Version = 2 },
		"wrapped key": func(v *envelope) { v.Key[len(v.Key)-1] ^= 1 },
		"key nonce":   func(v *envelope) { v.Key[0] ^= 1 },
		"data nonce":  func(v *envelope) { v.Nonce[0] ^= 1 },
		"data":        func(v *envelope) { v.Data[0] ^= 1 },
		"short key":   func(v *envelope) { v.Key = v.Key[:2] },
		"short nonce": func(v *envelope) { v.Nonce = v.Nonce[:2] },
		"short data":  func(v *envelope) { v.Data = v.Data[:2] },
	} {
		raw, err := base64.RawURLEncoding.DecodeString(ciphertext)
		if err != nil {
			t.Fatal(err)
		}
		var v envelope
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		change(&v)
		encoded, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		cases[name] = struct {
			store            *Store
			reference, value string
		}{s, "private-reference", base64.RawURLEncoding.EncodeToString(encoded)}
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := tc.store.Open(tc.reference, tc.value)
			if err == nil || got != "" {
				t.Fatal("invalid envelope decrypted")
			}
			for _, secret := range []string{"private-reference", "other-reference", "private-plaintext", ciphertext} {
				if strings.Contains(err.Error(), secret) {
					t.Fatal("error leaked secret data")
				}
			}
		})
	}
}

func TestEnvelopeStructureRejected(t *testing.T) {
	s := testStore(t)
	for _, raw := range []string{"null", "{}", `{"v":1,"unexpected":true}`, `{} {}`, `{"v":1,"key":"!"}`} {
		if _, err := s.Open("reference", base64.RawURLEncoding.EncodeToString([]byte(raw))); err == nil {
			t.Fatal("invalid envelope accepted")
		}
	}
}

func TestKeyValidationAndOwnership(t *testing.T) {
	for _, size := range []int{0, 16, 24, 31, 33, 64} {
		if _, err := New(make([]byte, size)); err == nil {
			t.Errorf("accepted key of length %d", size)
		}
	}
	key := bytes.Repeat([]byte{42}, 32)
	s, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	clear(key)
	ciphertext, err := s.Seal("id", "test-only")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testStore(t).Open("id", ciphertext); err != nil {
		t.Fatal("caller mutation changed store key")
	}
	for _, tc := range []struct {
		store                *Store
		reference, plaintext string
	}{
		{s, "", "secret"}, {s, "id", ""}, {nil, "id", "secret"}, {new(Store), "id", "secret"},
	} {
		if _, err := tc.store.Seal(tc.reference, tc.plaintext); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
}

func TestConcurrentUse(t *testing.T) {
	s := testStore(t)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			value, err := s.Seal("id", "test-only")
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := s.Open("id", value); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}

func ExampleStore() {
	// Load a securely provisioned 32-byte key in production.
	store, err := New(bytes.Repeat([]byte{42}, 32))
	if err != nil {
		panic(err)
	}
	sealed, err := store.Seal("credential-id", "example-only")
	if err != nil {
		panic(err)
	}
	value, err := store.Open("credential-id", sealed)
	fmt.Println(err == nil && value == "example-only")
	// Output: true
}
