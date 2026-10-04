package secretstore

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func testKeyring(t *testing.T) *Store {
	t.Helper()
	store, err := NewKeyring(map[string][]byte{"rky_old": bytes.Repeat([]byte{42}, 32), "rky_new": bytes.Repeat([]byte{43}, 32), "rky_third": bytes.Repeat([]byte{44}, 32)}, "rky_old", "rky_old")
	if err != nil {
		t.Fatal(err)
	}
	return store
}
func keyringTestEnvelope(t *testing.T, value string) keyringEnvelope {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	var sealed keyringEnvelope
	if err = json.Unmarshal(raw, &sealed); err != nil {
		t.Fatal(err)
	}
	return sealed
}
func keyringTestEncode(t *testing.T, sealed keyringEnvelope) string {
	t.Helper()
	value, err := encodeKeyringEnvelope(sealed)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func keyringTestJSON(raw string) string { return base64.RawURLEncoding.EncodeToString([]byte(raw)) }

// Fixed DEK/nonces and literal AAD freeze the legacy wire contract independently
// of the store's Seal/aad helpers. These are public test-only cryptographic values.
func keyringFrozenV1(t *testing.T) string {
	t.Helper()
	makeAEAD := func(key []byte) cipher.AEAD {
		block, err := aes.NewCipher(key)
		if err != nil {
			t.Fatal(err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			t.Fatal(err)
		}
		return aead
	}
	root := makeAEAD(bytes.Repeat([]byte{42}, 32))
	key := bytes.Repeat([]byte{7}, 32)
	data := makeAEAD(key)
	wrappingNonce := bytes.Repeat([]byte{1}, 12)
	nonce := bytes.Repeat([]byte{2}, 12)
	raw, err := json.Marshal(envelope{Version: 1, Key: root.Seal(wrappingNonce, wrappingNonce, key, []byte("routex:credential:v1:key:fixture")), Nonce: nonce, Data: data.Seal(nil, nonce, []byte("test-secret-秘密\x00"), []byte("routex:credential:v1:data:fixture"))})
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
func TestKeyringLegacyCompatibilityAndFrozenAAD(t *testing.T) {
	ring := testKeyring(t)
	legacy := testStore(t)
	old := keyringFrozenV1(t)
	for _, store := range []*Store{legacy, ring} {
		value, err := store.Open("fixture", old)
		if err != nil || value != "test-secret-秘密\x00" {
			t.Fatal("legacy wire contract changed", err)
		}
	}
	if id, err := ring.KeyID("fixture", old); err != nil || id != "rky_old" {
		t.Fatal("legacy identity not authenticated", id, err)
	}
	fresh, err := legacy.Seal("fixture", "legacy")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(fresh)
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 4 || string(fields["v"]) != "1" || fields["kid"] != nil {
		t.Fatal("New no longer writes v1")
	}
	fresh, err = ring.Seal("fixture", "keyring")
	if err != nil {
		t.Fatal(err)
	}
	sealed := keyringTestEnvelope(t, fresh)
	if sealed.Version != 2 || sealed.KeyID != "rky_old" {
		t.Fatal("keyring did not write exact v2 identity")
	}
	root, err := newAEAD(bytes.Repeat([]byte{42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	key, err := root.Open(nil, sealed.Key[:12], sealed.Key[12:], []byte("routex:credential:v2:key:rky_old:fixture"))
	if err != nil {
		t.Fatal("v2 wrapping AAD contract changed", err)
	}
	defer clear(key)
	data, err := newAEAD(key)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := data.Open(nil, sealed.Nonce, sealed.Data, []byte("routex:credential:v1:data:fixture"))
	if err != nil || string(plain) != "keyring" {
		t.Fatal("v2 payload AAD contract changed", err)
	}
	clear(plain)
	if _, err = legacy.Open("fixture", fresh); !errors.Is(err, errOpen) {
		t.Fatal("legacy New accepted v2")
	}
}
func TestKeyringRewrapPreservesPayloadAndRemovesOldRootDependency(t *testing.T) {
	ring := testKeyring(t)
	v1 := keyringFrozenV1(t)
	v2, err := ring.Seal("fixture", "test-secret-秘密\x00")
	if err != nil {
		t.Fatal(err)
	}
	for _, original := range []string{v1, v2} {
		before := keyringTestEnvelope(t, original)
		rewrapped, err := ring.Rewrap("fixture", original, "rky_new")
		if err != nil {
			t.Fatal(err)
		}
		after := keyringTestEnvelope(t, rewrapped)
		if after.Version != 2 || after.KeyID != "rky_new" || !bytes.Equal(before.Nonce, after.Nonce) || !bytes.Equal(before.Data, after.Data) || bytes.Equal(before.Key, after.Key) {
			t.Fatal("rewrap changed payload/nonce or failed key switch")
		}
		targetOnly, err := NewKeyring(map[string][]byte{"rky_new": bytes.Repeat([]byte{43}, 32)}, "", "rky_new")
		if err != nil {
			t.Fatal(err)
		}
		if plain, err := targetOnly.Open("fixture", rewrapped); err != nil || plain != "test-secret-秘密\x00" {
			t.Fatal("target still requires old root", err)
		}
		if id, err := targetOnly.KeyID("fixture", rewrapped); err != nil || id != "rky_new" {
			t.Fatal(id, err)
		}
		if value, err := targetOnly.Open("fixture", original); value != "" || !errors.Is(err, errOpen) {
			t.Fatal("target-only ring tried old envelope with arbitrary key")
		}
		next, err := ring.Rewrap("fixture", rewrapped, "rky_third")
		if err != nil {
			t.Fatal(err)
		}
		third := keyringTestEnvelope(t, next)
		if !bytes.Equal(before.Nonce, third.Nonce) || !bytes.Equal(before.Data, third.Data) || third.KeyID != "rky_third" {
			t.Fatal("v2-to-v2 changed payload")
		}
		repeated, err := ring.Rewrap("fixture", next, "rky_third")
		if err != nil || repeated == next {
			t.Fatal("same-target wrapping was not fresh", err)
		}
		if plain, err := ring.Open("fixture", repeated); err != nil || plain != "test-secret-秘密\x00" {
			t.Fatal(err)
		}
	}
}
func TestKeyringConstructionRejectsAmbiguousIdentityAndMaterial(t *testing.T) {
	for _, test := range []struct {
		keys          map[string][]byte
		legacy, write string
	}{
		{nil, "", ""}, {map[string][]byte{}, "", "old"},
		{map[string][]byte{"old": bytes.Repeat([]byte{1}, 32)}, "missing", "old"},
		{map[string][]byte{"old": bytes.Repeat([]byte{1}, 32)}, "old", "missing"},
		{map[string][]byte{"old": bytes.Repeat([]byte{1}, 32)}, "old", ""},
		{map[string][]byte{"old": bytes.Repeat([]byte{1}, 32), "alias": bytes.Repeat([]byte{1}, 32)}, "old", "alias"},
		{map[string][]byte{"old": bytes.Repeat([]byte{1}, 31)}, "old", "old"},
		{map[string][]byte{"old": nil}, "old", "old"},
	} {
		if store, err := NewKeyring(test.keys, test.legacy, test.write); store != nil || !errors.Is(err, errInvalidKeyring) {
			t.Fatal("invalid keyring accepted", err)
		}
	}
	for _, id := range []string{"", " old", "old ", "old.key", "old:key", "old/key", "old\x00", "old\n", "密钥", "é", strings.Repeat("a", 65)} {
		if store, err := NewKeyring(map[string][]byte{id: bytes.Repeat([]byte{1}, 32)}, id, id); store != nil || !errors.Is(err, errInvalidKeyring) {
			t.Fatal("noncanonical ID accepted", err)
		}
	}
	keys := map[string][]byte{}
	for i := range maxKeyringKeys + 1 {
		keys[fmt.Sprintf("key_%d", i)] = bytes.Repeat([]byte{byte(i)}, 32)
	}
	if store, err := NewKeyring(keys, "key_0", "key_1"); store != nil || !errors.Is(err, errInvalidKeyring) {
		t.Fatal("unbounded keyring accepted")
	}
	boundary := strings.Repeat("a", 64)
	if _, err := NewKeyring(map[string][]byte{boundary: bytes.Repeat([]byte{1}, 32)}, "", boundary); err != nil {
		t.Fatal("bounded safe ID rejected", err)
	}
}
func TestKeyringCopiesKeysMapAndWriteViewsAreImmutable(t *testing.T) {
	old, newKey := bytes.Repeat([]byte{42}, 32), bytes.Repeat([]byte{43}, 32)
	keys := map[string][]byte{"rky_old": old, "rky_new": newKey}
	ring, err := NewKeyring(keys, "rky_old", "rky_old")
	if err != nil {
		t.Fatal(err)
	}
	target, err := ring.WithWriteKey("rky_new")
	if err != nil {
		t.Fatal(err)
	}
	clear(old)
	clear(newKey)
	delete(keys, "rky_old")
	keys["rky_new"] = make([]byte, 32)
	ids := ring.KeyIDs()
	if !reflect.DeepEqual(ids, []string{"rky_new", "rky_old"}) {
		t.Fatal("key IDs not sorted", ids)
	}
	ids[0] = "caller-mutated"
	if !reflect.DeepEqual(ring.KeyIDs(), []string{"rky_new", "rky_old"}) {
		t.Fatal("key IDs borrowed internal slice")
	}
	fresh, err := NewKeyring(map[string][]byte{"rky_old": bytes.Repeat([]byte{42}, 32), "rky_new": bytes.Repeat([]byte{43}, 32)}, "rky_old", "rky_new")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		store *Store
		id    string
	}{{ring, "rky_old"}, {target, "rky_new"}} {
		value, err := test.store.Seal("id", "plaintext")
		if err != nil {
			t.Fatal(err)
		}
		if id, err := fresh.KeyID("id", value); err != nil || id != test.id {
			t.Fatal("caller mutation or view selection changed root", id, err)
		}
	}
	if _, err := ring.WithWriteKey("RKY_NEW"); !errors.Is(err, errInvalidKeyring) {
		t.Fatal("write ID case folded")
	}
	if _, err := ring.WithWriteKey(""); !errors.Is(err, errInvalidKeyring) {
		t.Fatal("empty write ID allowed")
	}
}
func TestKeyringExactSelectionNoTrialFallback(t *testing.T) {
	ring := testKeyring(t)
	encrypted, err := ring.Seal("id", "private")
	if err != nil {
		t.Fatal(err)
	}
	cases := []*Store{}
	// The correct material exists, but it is assigned to the wrong exact ID.
	wrongLabel, err := NewKeyring(map[string][]byte{"different": bytes.Repeat([]byte{42}, 32)}, "different", "different")
	if err != nil {
		t.Fatal(err)
	}
	cases = append(cases, wrongLabel)
	wrongLegacy, err := NewKeyring(map[string][]byte{"rky_old": bytes.Repeat([]byte{42}, 32), "rky_new": bytes.Repeat([]byte{43}, 32)}, "rky_new", "rky_old")
	if err != nil {
		t.Fatal(err)
	}
	v1 := keyringFrozenV1(t)
	if value, err := wrongLegacy.Open("fixture", v1); value != "" || !errors.Is(err, errOpen) {
		t.Fatal("v1 tried another available key")
	}
	v2Only, err := NewKeyring(map[string][]byte{"rky_old": bytes.Repeat([]byte{42}, 32)}, "", "rky_old")
	if err != nil {
		t.Fatal(err)
	}
	if value, err := v2Only.Open("fixture", v1); value != "" || !errors.Is(err, errOpen) {
		t.Fatal("v1 inferred implicit legacy slot")
	}
	if id, err := v2Only.KeyID("fixture", v1); id != "" || !errors.Is(err, errOpen) {
		t.Fatal("legacy identity inferred")
	}
	for _, store := range cases {
		if value, err := store.Open("id", encrypted); value != "" || !errors.Is(err, errOpen) {
			t.Fatal("v2 tried another ID's material")
		}
	}
	sealed := keyringTestEnvelope(t, encrypted)
	sealed.KeyID = "rky_new"
	if value, err := ring.Open("id", keyringTestEncode(t, sealed)); value != "" || !errors.Is(err, errOpen) {
		t.Fatal("key ID not authenticated")
	}
	sealed.KeyID = "RKY_OLD"
	if value, err := ring.Open("id", keyringTestEncode(t, sealed)); value != "" || !errors.Is(err, errOpen) {
		t.Fatal("header ID case folded")
	}
}
func TestKeyringAllReadOperationsAuthenticatePayloadAndReference(t *testing.T) {
	ring := testKeyring(t)
	for _, kind := range []string{"v1", "v2"} {
		encrypted := keyringFrozenV1(t)
		if kind == "v2" {
			var err error
			encrypted, err = ring.Seal("fixture", "private")
			if err != nil {
				t.Fatal(err)
			}
		}
		for name, change := range map[string]func(*keyringEnvelope){
			"wrapped key":       func(v *keyringEnvelope) { v.Key[len(v.Key)-1] ^= 1 },
			"wrapping nonce":    func(v *keyringEnvelope) { v.Key[0] ^= 1 },
			"payload nonce":     func(v *keyringEnvelope) { v.Nonce[0] ^= 1 },
			"payload":           func(v *keyringEnvelope) { v.Data[len(v.Data)-1] ^= 1 },
			"short wrapped key": func(v *keyringEnvelope) { v.Key = v.Key[:3] },
			"short nonce":       func(v *keyringEnvelope) { v.Nonce = v.Nonce[:3] },
			"empty payload":     func(v *keyringEnvelope) { v.Data = nil },
		} {
			t.Run(kind+"/"+name, func(t *testing.T) {
				sealed := keyringTestEnvelope(t, encrypted)
				change(&sealed)
				var tampered string
				if kind == "v1" {
					raw, err := json.Marshal(envelope{Version: 1, Key: sealed.Key, Nonce: sealed.Nonce, Data: sealed.Data})
					if err != nil {
						t.Fatal(err)
					}
					tampered = base64.RawURLEncoding.EncodeToString(raw)
				} else {
					tampered = keyringTestEncode(t, sealed)
				}
				keyringRejectEveryOperation(t, ring, "fixture", tampered)
			})
		}
		keyringRejectEveryOperation(t, ring, "foreign", encrypted)
		keyringRejectEveryOperation(t, ring, "", encrypted)
	}
}
func keyringRejectEveryOperation(t *testing.T, ring *Store, reference, value string) {
	t.Helper()
	if result, err := ring.Open(reference, value); result != "" || !errors.Is(err, errOpen) {
		t.Fatal("Open accepted invalid envelope", err)
	}
	if id, err := ring.KeyID(reference, value); id != "" || !errors.Is(err, errOpen) {
		t.Fatal("KeyID reported unauthenticated identity", err)
	}
	if result, err := ring.Rewrap(reference, value, "rky_new"); result != "" || !errors.Is(err, errRewrap) {
		t.Fatal("Rewrap accepted unauthenticated payload", err)
	}
}
func TestKeyringStrictEnvelopeFieldsTypesAndTrailingData(t *testing.T) {
	ring := testKeyring(t)
	encrypted, err := ring.Seal("fixture", "test-private")
	if err != nil {
		t.Fatal(err)
	}
	rawBytes, _ := base64.RawURLEncoding.DecodeString(encrypted)
	raw := string(rawBytes)
	malformed := []string{"null", "[]", "{}", raw + " {}", raw + " null",
		strings.Replace(raw, `"v":2`, `"v":2,"v":2`, 1), strings.Replace(raw, `"v":2`, `"v":1,"v":2`, 1),
		strings.Replace(raw, `"kid":"rky_old"`, `"kid":"rky_new","kid":"rky_old"`, 1),
		strings.Replace(raw, `"kid"`, `"KID"`, 1), strings.Replace(raw, `"kid"`, `"key_id"`, 1),
		strings.Replace(raw, `"v"`, `"V"`, 1), strings.Replace(raw, `"key"`, `"Key"`, 1),
		strings.Replace(raw, `"nonce"`, `"Nonce"`, 1), strings.Replace(raw, `"data"`, `"Data"`, 1),
		strings.Replace(raw, `"v":2`, `"v":null`, 1), strings.Replace(raw, `"v":2`, `"v":2.0`, 1), strings.Replace(raw, `"v":2`, `"v":3`, 1),
		strings.Replace(raw, `"kid":"rky_old"`, `"kid":null`, 1), strings.Replace(raw, `"kid":"rky_old"`, `"kid":""`, 1),
		strings.Replace(raw, `"kid":"rky_old"`, `"kid":"rky_old "`, 1), strings.Replace(raw, `"kid":"rky_old",`, ``, 1),
		strings.Replace(raw, `"v":2`, `"v":2,"unexpected":1`, 1),
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(rawBytes, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"key", "nonce", "data"} {
		for _, replacement := range []string{"null", "[]", "[1,2,3]", "1", "true", `"!"`} {
			malformed = append(malformed, strings.Replace(raw, `"`+name+`":`+string(fields[name]), `"`+name+`":`+replacement, 1))
		}
		malformed = append(malformed, strings.Replace(raw, `"`+name+`":`+string(fields[name]), `"`+name+`":`+string(fields[name])+`,"`+name+`":`+string(fields[name]), 1))
	}
	for _, value := range malformed {
		keyringRejectEveryOperation(t, ring, "fixture", keyringTestJSON(value))
	}
	oldRaw, _ := base64.RawURLEncoding.DecodeString(keyringFrozenV1(t))
	for _, value := range []string{strings.Replace(string(oldRaw), `"v":1`, `"v":1,"v":1`, 1), strings.Replace(string(oldRaw), `"v":1`, `"v":1,"kid":"rky_old"`, 1)} {
		keyringRejectEveryOperation(t, ring, "fixture", keyringTestJSON(value))
	}
	keyringRejectEveryOperation(t, ring, "fixture", encrypted+"\n")
	keyringRejectEveryOperation(t, ring, "fixture", "!")
}
func TestKeyringUniformSanitizedErrorsAndInvalidViews(t *testing.T) {
	ring := testKeyring(t)
	const reference = "private-reference"
	value, err := ring.Seal(reference, "private-plaintext")
	if err != nil {
		t.Fatal(err)
	}
	errorsToCheck := []error{}
	_, err = ring.WithWriteKey("private-missing-id")
	errorsToCheck = append(errorsToCheck, err)
	_, err = ring.Rewrap(reference, value, "private-missing-id")
	errorsToCheck = append(errorsToCheck, err)
	_, err = ring.Open("other-private-reference", value)
	errorsToCheck = append(errorsToCheck, err)
	_, err = ring.KeyID(reference, "private-ciphertext")
	errorsToCheck = append(errorsToCheck, err)
	_, err = ring.Seal("", "private-plaintext")
	errorsToCheck = append(errorsToCheck, err)
	for _, err := range errorsToCheck {
		if err == nil {
			t.Fatal("invalid operation succeeded")
		}
		for _, secret := range []string{reference, "other-private-reference", "private-missing-id", "private-plaintext", "private-ciphertext", value, "rky_old", "rky_new"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatal("error exposed private context")
			}
		}
	}
	for _, store := range []*Store{nil, new(Store), testStore(t)} {
		if len(store.KeyIDs()) != 0 {
			t.Fatal("legacy/nil IDs fabricated")
		}
		if _, err := store.WithWriteKey("rky_old"); !errors.Is(err, errInvalidKeyring) {
			t.Fatal("invalid view allowed")
		}
		if _, err := store.KeyID(reference, value); !errors.Is(err, errOpen) {
			t.Fatal("invalid identity read allowed")
		}
		if _, err := store.Rewrap(reference, value, "rky_old"); !errors.Is(err, errRewrap) {
			t.Fatal("invalid rewrap allowed")
		}
	}
}
func TestKeyringEnvelopeAndPlaintextBounds(t *testing.T) {
	ring := testKeyring(t)
	payload := strings.Repeat("x", maxKeyringPlaintextBytes)
	value, err := ring.Seal("id", payload)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := ring.Open("id", value); err != nil || result != payload {
		t.Fatal("valid boundary rejected", err)
	}
	if value, err := ring.Seal("id", payload+"x"); value != "" || !errors.Is(err, errSeal) {
		t.Fatal("oversize keyring plaintext accepted")
	}
	keyringRejectEveryOperation(t, ring, "id", strings.Repeat("x", maxKeyringEnvelopeBytes+1))
	// Legacy New retains its original size behavior, independently of ring limits.
	legacy := testStore(t)
	value, err = legacy.Seal("id", payload+"x")
	if err != nil {
		t.Fatal("legacy size behavior changed", err)
	}
	if result, err := legacy.Open("id", value); err != nil || result != payload+"x" {
		t.Fatal(err)
	}
}
func TestKeyringConcurrentViewsAndRewrap(t *testing.T) {
	ring := testKeyring(t)
	views := map[string]*Store{}
	for _, id := range ring.KeyIDs() {
		var err error
		views[id], err = ring.WithWriteKey(id)
		if err != nil {
			t.Fatal(err)
		}
	}
	var group sync.WaitGroup
	for index := range 32 {
		group.Go(func() {
			reference := fmt.Sprintf("reference-%d", index)
			for _, id := range ring.KeyIDs() {
				value, err := views[id].Seal(reference, "test-private")
				if err != nil {
					t.Error(err)
					return
				}
				if actual, err := ring.KeyID(reference, value); err != nil || actual != id {
					t.Error("view changed", err)
					return
				}
				next, err := ring.Rewrap(reference, value, "rky_new")
				if err != nil {
					t.Error(err)
					return
				}
				if plain, err := ring.Open(reference, next); err != nil || plain != "test-private" {
					t.Error(err)
					return
				}
			}
		})
	}
	group.Wait()
	value, err := ring.Seal("original", "value")
	if err != nil {
		t.Fatal(err)
	}
	if id, err := ring.KeyID("original", value); err != nil || id != "rky_old" {
		t.Fatal("parallel views mutated original", err)
	}
}

func TestKeyringRestrictedViewCannotDecryptRetiredRoot(t *testing.T) {
	ring := testKeyring(t)
	old := keyringFrozenV1(t)
	fresh, err := ring.Seal("fixture", "private")
	if err != nil {
		t.Fatal(err)
	}
	writer, err := ring.WithWriteKey("rky_new")
	if err != nil {
		t.Fatal(err)
	}
	restricted, err := writer.WithAllowedKeys([]string{"rky_new"})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{old, fresh} {
		if plain, err := restricted.Open("fixture", value); plain != "" || !errors.Is(err, errOpen) {
			t.Fatal("retired root remains readable")
		}
		if _, err := restricted.KeyID("fixture", value); !errors.Is(err, errOpen) {
			t.Fatal("retired identity trusted")
		}
	}
	rewrapped, err := ring.Rewrap("fixture", fresh, "rky_new")
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := restricted.Open("fixture", rewrapped); err != nil || plain != "private" {
		t.Fatal("target no longer readable", err)
	}
	if _, err := restricted.WithWriteKey("rky_old"); !errors.Is(err, errInvalidKeyring) {
		t.Fatal("retired key can be selected again")
	}
	for _, ids := range [][]string{nil, {}, {"rky_old"}, {"rky_new", "rky_new"}, {"rky_new", "missing"}} {
		if _, err := writer.WithAllowedKeys(ids); !errors.Is(err, errInvalidKeyring) {
			t.Fatal("invalid restriction accepted")
		}
	}
	if !reflect.DeepEqual(ring.KeyIDs(), []string{"rky_new", "rky_old", "rky_third"}) {
		t.Fatal("restriction mutated masterring")
	}
}
