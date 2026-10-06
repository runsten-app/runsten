// Package secretbox encrypts secrets stored in the database (OAuth tokens) with
// AES-256-GCM. The key comes from the environment and is never stored in the database.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// KeySize is the expected key size (AES-256).
const KeySize = 32

// Box encrypts and decrypts with a fixed key.
type Box struct{ aead cipher.AEAD }

// New creates a Box from a key of KeySize bytes.
func New(key []byte) (*Box, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("key is %d bytes, want %d", len(key), KeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return &Box{aead: aead}, nil
}

// FromBase64 creates a Box from a key encoded in standard base64.
func FromBase64(s string) (*Box, error) {
	key, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("base64 key: %w", err)
	}
	return New(key)
}

// Seal encrypts plain. context binds the ciphertext to its owner (for example the
// account ID): a ciphertext copied elsewhere does not decrypt.
func (b *Box) Seal(plain, context []byte) []byte {
	nonce := make([]byte, b.aead.NonceSize())
	_, _ = rand.Read(nonce) // crypto/rand.Read never returns an error (Go ≥ 1.24)
	return b.aead.Seal(nonce, nonce, plain, context)
}

// Open decrypts a Seal result produced with the same context.
func (b *Box) Open(sealed, context []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(sealed) < n {
		return nil, errors.New("ciphertext too short")
	}
	plain, err := b.aead.Open(nil, sealed[:n], sealed[n:], context)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plain, nil
}
