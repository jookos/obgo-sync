package crypto

import (
	"crypto/sha256"
	"errors"
	"fmt"
)

// ErrNotImplemented is returned by stub methods that are not yet implemented.
var ErrNotImplemented = errors.New("not implemented")

// Service handles all E2EE operations for obgo-live.
type Service struct {
	password string
	salt     []byte
}

// New creates a new crypto Service with the given password.
// If password is empty, E2EE is disabled.
func New(password string) *Service {
	return &Service{password: password}
}

// Enabled reports whether E2EE is active (passphrase was provided).
func (s *Service) Enabled() bool { return s.password != "" }

// SetSalt configures the HKDF salt from SyncParameters.
func (s *Service) SetSalt(salt []byte) {
	s.salt = salt
}

// ChunkID computes the chunk document _id for given content.
// Without E2EE: "h:" + hex(sha256(content))
// With E2EE:    "h:+" + hex(sha256(content + passphrase))
func (s *Service) ChunkID(content []byte) string {
	if s.Enabled() {
		h := sha256.New()
		h.Write(content)
		h.Write([]byte(s.password))
		return "h:+" + fmt.Sprintf("%x", h.Sum(nil))
	}
	sum := sha256.Sum256(content)
	return "h:" + fmt.Sprintf("%x", sum)
}

// EncryptContent encrypts plaintext chunk data.
// Not yet implemented.
func (s *Service) EncryptContent(plaintext []byte) (string, error) {
	return "", ErrNotImplemented
}

// DecryptContent decrypts a chunk data string.
// Not yet implemented.
func (s *Service) DecryptContent(ciphertext string) ([]byte, error) {
	return nil, ErrNotImplemented
}
