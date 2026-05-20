// Package crypto provides AES-256-GCM symmetric encryption for sensitive data at rest,
// specifically for proxy target credentials stored in the database.
//
// The encryption key is a 32-byte value (AES-256) read from the GATEWAY_ENCRYPTION_KEY
// environment variable as a 64-character hex string. A fresh 12-byte random nonce is
// generated for each encryption; the output format is:
//
//	nonce (12 bytes) || GCM ciphertext
//
// stored as a single BYTEA value. GCM's authentication tag (16 bytes, appended by
// cipher.AEAD.Seal) means any tampering of the stored ciphertext is detected on decrypt.
//
// References:
//   - ADR-0012 — encryption at rest for proxy target credentials
//   - https://pkg.go.dev/crypto/aes
//   - https://pkg.go.dev/crypto/cipher
//   - NIST SP 800-38D — GCM specification
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

const (
	gcmNonceSize = 12 // standard GCM nonce length per NIST SP 800-38D
)

// Encrypter abstracts symmetric encryption so the infra layer can be tested
// with a deterministic stub in unit tests.
type Encrypter interface {
	// Encrypt encrypts plaintext and returns nonce||ciphertext.
	Encrypt(plaintext []byte) ([]byte, error)

	// Decrypt reverses Encrypt. Returns an error if the ciphertext has been tampered with
	// (GCM authentication tag mismatch) or is too short to contain a valid nonce.
	Decrypt(ciphertext []byte) ([]byte, error)
}

// AESGCMEncrypter implements Encrypter using AES-256-GCM.
// Safe for concurrent use after construction.
type AESGCMEncrypter struct {
	aead cipher.AEAD
}

// NewAESGCMEncrypter constructs an Encrypter from a 64-char hex key string.
// keyHex must encode exactly 32 bytes (AES-256). Returns an error if the key is
// malformed or the wrong length.
//
// Reasoning: the key is passed as hex (not raw bytes) so it can be stored in an
// environment variable without binary encoding concerns. 64 hex chars = 32 bytes.
//
// References:
//   - ADR-0012
func NewAESGCMEncrypter(keyHex string) (*AESGCMEncrypter, error) {
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("decoding GATEWAY_ENCRYPTION_KEY as hex: %w", err)
	}
	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("GATEWAY_ENCRYPTION_KEY must be 64 hex chars (32 bytes); got %d bytes", len(keyBytes))
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("creating AES cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}

	return &AESGCMEncrypter{aead: aead}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM with a random nonce.
// Output format: nonce (12 bytes) || GCM ciphertext (len(plaintext) + 16 bytes tag).
//
// References:
//   - ADR-0012
func (e *AESGCMEncrypter) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, gcmNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}

	// Seal appends the authentication tag to the ciphertext automatically.
	ciphertext := e.aead.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt reverses Encrypt. The first 12 bytes of ciphertext are the nonce;
// the remainder is the GCM ciphertext + authentication tag.
//
// Returns ErrAuthFailed if the tag verification fails (tampered or corrupted data).
//
// References:
//   - ADR-0012
func (e *AESGCMEncrypter) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < gcmNonceSize {
		return nil, fmt.Errorf("ciphertext too short: %d bytes (minimum %d)", len(ciphertext), gcmNonceSize)
	}

	nonce := ciphertext[:gcmNonceSize]
	ct := ciphertext[gcmNonceSize:]

	plaintext, err := e.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		// GCM Open returns a generic error on tag mismatch; wrap with a sentinel so
		// callers can distinguish tampering from other errors.
		return nil, fmt.Errorf("decrypting credential: %w: %w", ErrAuthFailed, err)
	}

	return plaintext, nil
}

// ErrAuthFailed is returned when GCM tag verification fails, indicating that
// the ciphertext has been tampered with or was encrypted with a different key.
var ErrAuthFailed = errors.New("authentication tag mismatch")
