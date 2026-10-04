package secretstore

import (
	"bytes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
)

const (
	maxKeyringKeys           = 64
	maxKeyringPlaintextBytes = 32 << 10
	maxKeyringEnvelopeBytes  = 128 << 10
)

var (
	errInvalidKeyring = errors.New("invalid secret store keyring")
	errRewrap         = errors.New("cannot rewrap credential")
)

type keyringEnvelope struct {
	Version int    `json:"v"`
	KeyID   string `json:"kid"`
	Key     []byte `json:"key"`
	Nonce   []byte `json:"nonce"`
	Data    []byte `json:"data"`
}

// NewKeyring copies pre-provisioned roots into immutable cryptographic state.
// IDs are exact ASCII identifiers, 1..64 letters/digits/underscores/hyphens.
// Empty legacyKeyID disables v1 reads; a provided legacy and the write ID must
// exist. Identical material under different IDs is rejected to prevent aliases.
func NewKeyring(keys map[string][]byte, legacyKeyID, writeKeyID string) (*Store, error) {
	if len(keys) == 0 || len(keys) > maxKeyringKeys || !validKeyID(writeKeyID) || (legacyKeyID != "" && !validKeyID(legacyKeyID)) {
		return nil, errInvalidKeyring
	}
	if _, ok := keys[writeKeyID]; !ok {
		return nil, errInvalidKeyring
	}
	if legacyKeyID != "" {
		if _, ok := keys[legacyKeyID]; !ok {
			return nil, errInvalidKeyring
		}
	}
	roots := make(map[string]cipher.AEAD, len(keys))
	material := make([][]byte, 0, len(keys))
	for id, key := range keys {
		if !validKeyID(id) || len(key) != 32 {
			return nil, errInvalidKeyring
		}
		for _, previous := range material {
			if subtle.ConstantTimeCompare(previous, key) == 1 {
				return nil, errInvalidKeyring
			}
		}
		root, err := newAEAD(key)
		if err != nil {
			return nil, errInvalidKeyring
		}
		roots[id] = root
		material = append(material, key)
	}
	return &Store{keys: roots, legacyKeyID: legacyKeyID, writeKeyID: writeKeyID}, nil
}

func validKeyID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, ch := range []byte(id) {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '_', ch == '-':
		default:
			return false
		}
	}
	return true
}

// WithWriteKey creates an independent write-selection view over the same
// immutable keyring. It does not mutate another view or implement policy cutover.
func (s *Store) WithWriteKey(id string) (*Store, error) {
	if s == nil || s.keys == nil || !validKeyID(id) || s.keys[id] == nil {
		return nil, errInvalidKeyring
	}
	return &Store{keys: s.keys, legacyKeyID: s.legacyKeyID, writeKeyID: id}, nil
}

// KeyIDs returns a sorted copy of nonsecret provisioned IDs. Legacy New stores
// have no key identity and return an empty list.
func (s *Store) KeyIDs() []string {
	ids := []string{}
	if s != nil {
		for id := range s.keys {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// v2 authenticates its exact wrapping-key selection. Payload AAD stays v1 so a
// root rewrap can preserve the original payload and nonce without resealing it.
func keyringAAD(id, reference string) []byte {
	return []byte("routex:credential:v2:key:" + id + ":" + reference)
}
func (s *Store) sealKeyring(reference, plaintext string) (string, error) {
	root := s.keys[s.writeKeyID]
	if root == nil || reference == "" || plaintext == "" || len(plaintext) > maxKeyringPlaintextBytes {
		return "", errSeal
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", errSeal
	}
	defer clear(key)
	dataAEAD, err := newAEAD(key)
	if err != nil {
		return "", errSeal
	}
	nonce := make([]byte, dataAEAD.NonceSize())
	keyNonce := make([]byte, root.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", errSeal
	}
	if _, err = rand.Read(keyNonce); err != nil {
		return "", errSeal
	}
	payload := []byte(plaintext)
	defer clear(payload)
	sealed := keyringEnvelope{Version: 2, KeyID: s.writeKeyID, Key: root.Seal(keyNonce, keyNonce, key, keyringAAD(s.writeKeyID, reference)), Nonce: nonce, Data: dataAEAD.Seal(nil, nonce, payload, aad("data", reference))}
	encoded, err := encodeKeyringEnvelope(sealed)
	if err != nil {
		return "", errSeal
	}
	return encoded, nil
}
func encodeKeyringEnvelope(sealed keyringEnvelope) (string, error) {
	raw, err := json.Marshal(sealed)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Decode fields explicitly: encoding/json struct matching alone accepts case
// aliases and duplicates, which must not influence cryptographic key selection.
func decodeKeyringEnvelope(ciphertext string) (keyringEnvelope, error) {
	var sealed keyringEnvelope
	if len(ciphertext) > maxKeyringEnvelopeBytes || strings.ContainsAny(ciphertext, "\r\n") {
		return sealed, errOpen
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(ciphertext)
	if err != nil {
		return sealed, errOpen
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return sealed, errOpen
	}
	fields := map[string]bool{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return sealed, errOpen
		}
		name, ok := token.(string)
		if !ok || fields[name] {
			return sealed, errOpen
		}
		fields[name] = true
		var target any
		switch name {
		case "v":
			target = &sealed.Version
		case "kid":
			target = &sealed.KeyID
		case "key":
			target = &sealed.Key
		case "nonce":
			target = &sealed.Nonce
		case "data":
			target = &sealed.Data
		default:
			return sealed, errOpen
		}
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return sealed, errOpen
		}
		if name == "key" || name == "nonce" || name == "data" {
			var encoded string
			if len(value) == 0 || value[0] != '"' || json.Unmarshal(value, &encoded) != nil || strings.ContainsAny(encoded, "\r\n") {
				return sealed, errOpen
			}
			decoded, decodeErr := base64.StdEncoding.Strict().DecodeString(encoded)
			if decodeErr != nil {
				return sealed, errOpen
			}
			switch name {
			case "key":
				sealed.Key = decoded
			case "nonce":
				sealed.Nonce = decoded
			case "data":
				sealed.Data = decoded
			}
		} else if err = json.Unmarshal(value, target); err != nil {
			return sealed, errOpen
		}
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return sealed, errOpen
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return sealed, errOpen
	}
	if !fields["v"] || !fields["key"] || !fields["nonce"] || !fields["data"] {
		return sealed, errOpen
	}
	switch sealed.Version {
	case 1:
		if fields["kid"] {
			return sealed, errOpen
		}
	case 2:
		if !fields["kid"] || !validKeyID(sealed.KeyID) {
			return sealed, errOpen
		}
	default:
		return sealed, errOpen
	}
	return sealed, nil
}

type authenticatedEnvelope struct {
	sealed         keyringEnvelope
	keyID          string
	key, plaintext []byte
}

func (value *authenticatedEnvelope) clear() { clear(value.key); clear(value.plaintext) }
func (s *Store) authenticateEnvelope(reference, ciphertext string) (*authenticatedEnvelope, error) {
	if s == nil || s.keys == nil || reference == "" || ciphertext == "" {
		return nil, errOpen
	}
	sealed, err := decodeKeyringEnvelope(ciphertext)
	if err != nil {
		return nil, errOpen
	}
	id := sealed.KeyID
	keyAAD := keyringAAD(id, reference)
	if sealed.Version == 1 {
		id = s.legacyKeyID
		keyAAD = aad("key", reference)
	}
	root := s.keys[id]
	if root == nil {
		return nil, errOpen
	}
	size := root.NonceSize()
	if len(sealed.Key) != size+32+root.Overhead() {
		return nil, errOpen
	}
	key, err := root.Open(nil, sealed.Key[:size], sealed.Key[size:], keyAAD)
	if err != nil {
		return nil, errOpen
	}
	dataAEAD, err := newAEAD(key)
	if err != nil || len(sealed.Nonce) != dataAEAD.NonceSize() || len(sealed.Data) <= dataAEAD.Overhead() {
		clear(key)
		return nil, errOpen
	}
	plaintext, err := dataAEAD.Open(nil, sealed.Nonce, sealed.Data, aad("data", reference))
	if err != nil {
		clear(key)
		return nil, errOpen
	}
	return &authenticatedEnvelope{sealed: sealed, keyID: id, key: key, plaintext: plaintext}, nil
}
func (s *Store) openKeyring(reference, ciphertext string) (string, error) {
	authenticated, err := s.authenticateEnvelope(reference, ciphertext)
	if err != nil {
		return "", errOpen
	}
	defer authenticated.clear()
	return string(authenticated.plaintext), nil
}

// KeyID returns an identity only after authenticating both the DEK and payload.
// It never guesses legacy key ownership or exposes an unauthenticated header.
func (s *Store) KeyID(reference, ciphertext string) (string, error) {
	authenticated, err := s.authenticateEnvelope(reference, ciphertext)
	if err != nil {
		return "", errOpen
	}
	defer authenticated.clear()
	return authenticated.keyID, nil
}

// Rewrap authenticates the complete original envelope and replaces only its DEK
// wrapping. Target selection is exact and independent of this view's write ID.
// Business references, payload nonce and ciphertext remain byte-identical.
func (s *Store) Rewrap(reference, ciphertext, targetID string) (string, error) {
	if s == nil || s.keys == nil || !validKeyID(targetID) || s.keys[targetID] == nil {
		return "", errRewrap
	}
	authenticated, err := s.authenticateEnvelope(reference, ciphertext)
	if err != nil {
		return "", errRewrap
	}
	defer authenticated.clear()
	root := s.keys[targetID]
	nonce := make([]byte, root.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", errRewrap
	}
	sealed := authenticated.sealed
	sealed.Version, sealed.KeyID = 2, targetID
	sealed.Key = root.Seal(nonce, nonce, authenticated.key, keyringAAD(targetID, reference))
	encoded, err := encodeKeyringEnvelope(sealed)
	if err != nil {
		return "", errRewrap
	}
	return encoded, nil
}

// WriteKeyID returns only a configured immutable selection, not application proof.
func (s *Store) WriteKeyID() string {
	if s == nil {
		return ""
	}
	return s.writeKeyID
}

// WithAllowedKeys removes forbidden roots from an immutable view. Removed legacy
// material cannot be tried implicitly; the current write root must remain allowed.
func (s *Store) WithAllowedKeys(ids []string) (*Store, error) {
	if s == nil || s.keys == nil || len(ids) == 0 {
		return nil, errInvalidKeyring
	}
	roots := make(map[string]cipher.AEAD, len(ids))
	for _, id := range ids {
		if !validKeyID(id) || roots[id] != nil || s.keys[id] == nil {
			return nil, errInvalidKeyring
		}
		roots[id] = s.keys[id]
	}
	if roots[s.writeKeyID] == nil {
		return nil, errInvalidKeyring
	}
	legacy := s.legacyKeyID
	if roots[legacy] == nil {
		legacy = ""
	}
	return &Store{keys: roots, writeKeyID: s.writeKeyID, legacyKeyID: legacy}, nil
}
