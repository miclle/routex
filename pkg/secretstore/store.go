// Package secretstore encrypts recoverable upstream credentials with a local root key.
package secretstore

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
)

var (
	errInvalidKey = errors.New("secret store requires a 32-byte root key")
	errSeal       = errors.New("cannot encrypt credential")
	errOpen       = errors.New("cannot decrypt credential")
)

// Store holds the root encryption key independently of the caller's key buffer.
// A Store is safe for concurrent use. Its zero value is not usable.
type Store struct {
	root cipher.AEAD
}

type envelope struct {
	Version int    `json:"v"`
	Key     []byte `json:"key"`
	Nonce   []byte `json:"nonce"`
	Data    []byte `json:"data"`
}

// New creates a store with a 32-byte AES-256 root key. The caller must retain the
// same key outside the database to recover credentials after a process restart.
func New(key []byte) (*Store, error) {
	if len(key) != 32 {
		return nil, errInvalidKey
	}
	root, err := newAEAD(key)
	if err != nil {
		return nil, errInvalidKey
	}
	return &Store{root: root}, nil
}

// Seal encrypts plaintext under a fresh data key and binds both ciphertexts to
// reference, an immutable credential identifier. Empty values are rejected.
func (s *Store) Seal(reference, plaintext string) (string, error) {
	if s == nil || s.root == nil || reference == "" || plaintext == "" {
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
	if _, err := rand.Read(nonce); err != nil {
		return "", errSeal
	}
	keyNonce := make([]byte, s.root.NonceSize())
	if _, err := rand.Read(keyNonce); err != nil {
		return "", errSeal
	}
	sealed := envelope{
		Version: 1,
		Key:     s.root.Seal(keyNonce, keyNonce, key, aad("key", reference)),
		Nonce:   nonce,
		Data:    dataAEAD.Seal(nil, nonce, []byte(plaintext), aad("data", reference)),
	}
	encoded, err := json.Marshal(sealed)
	if err != nil {
		return "", errSeal
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

// Open decrypts a supported envelope only for its original reference and root
// key. Errors never contain plaintext, identifiers, keys, or ciphertext.
func (s *Store) Open(reference, ciphertext string) (string, error) {
	if s == nil || s.root == nil || reference == "" || ciphertext == "" {
		return "", errOpen
	}
	encoded, err := base64.RawURLEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", errOpen
	}
	var sealed envelope
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&sealed); err != nil || sealed.Version != 1 {
		return "", errOpen
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return "", errOpen
	}
	nonceSize := s.root.NonceSize()
	if len(sealed.Key) != nonceSize+32+s.root.Overhead() {
		return "", errOpen
	}
	key, err := s.root.Open(nil, sealed.Key[:nonceSize], sealed.Key[nonceSize:], aad("key", reference))
	if err != nil {
		return "", errOpen
	}
	defer clear(key)
	dataAEAD, err := newAEAD(key)
	if err != nil || len(sealed.Nonce) != dataAEAD.NonceSize() || len(sealed.Data) <= dataAEAD.Overhead() {
		return "", errOpen
	}
	plaintext, err := dataAEAD.Open(nil, sealed.Nonce, sealed.Data, aad("data", reference))
	if err != nil {
		return "", errOpen
	}
	defer clear(plaintext)
	return string(plaintext), nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func aad(purpose, reference string) []byte {
	// Fixed domains prevent swapping the wrapped key and credential payload.
	return []byte("routex:credential:v1:" + purpose + ":" + reference)
}
