package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/pbkdf2"
)

// saltOfPassphrase is the static PBKDF2 salt used by V1 encryption.
const saltOfPassphrase = "rHGMPtr6oWw7VSa3W3wpa8fT8U"

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

// EncryptContent encrypts plaintext chunk data using AES-256-GCM (V2 format).
// If E2EE is not enabled, the data is base64-encoded and returned as-is.
// The returned string has the prefix "%=" for V2 encrypted data.
func (s *Service) EncryptContent(plaintext []byte) (string, error) {
	if !s.Enabled() {
		return base64.StdEncoding.EncodeToString(plaintext), nil
	}
	key, err := s.hkdfKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("crypto: AES new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: AES-GCM: %w", err)
	}
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("crypto: nonce generation: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil) // nonce prepended
	return "%=" + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptContent decrypts a chunk data string.
// If E2EE is not enabled, the data is base64-decoded and returned as-is.
// If the data starts with "%=" it is V2 (HKDF-AES-256-GCM).
// If the data starts with "%" (but not "%=") it is V1 (PBKDF2-AES-256-GCM).
func (s *Service) DecryptContent(ciphertext string) ([]byte, error) {
	if !s.Enabled() {
		// No encryption: data is base64-encoded raw content.
		return base64.StdEncoding.DecodeString(ciphertext)
	}

	switch {
	case strings.HasPrefix(ciphertext, "%="):
		return s.decryptV2(ciphertext[2:])
	case strings.HasPrefix(ciphertext, "%"):
		return s.decryptV1(ciphertext[1:])
	default:
		return nil, fmt.Errorf("crypto: unrecognised ciphertext prefix in %q", ciphertext[:min(len(ciphertext), 4)])
	}
}

// decryptV2 decrypts HKDF-AES-256-GCM ciphertext (V2 format).
// Format after base64-decode: [12-byte nonce][ciphertext+tag].
func (s *Service) decryptV2(b64 string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("crypto v2: base64 decode: %w", err)
	}
	if len(data) < 12 {
		return nil, errors.New("crypto v2: ciphertext too short")
	}

	key, err := s.hkdfKey()
	if err != nil {
		return nil, err
	}

	nonce := data[:12]
	encrypted := data[12:]
	return aesGCMDecrypt(key, nonce, encrypted)
}

// decryptV1 decrypts PBKDF2-AES-256-GCM ciphertext (V1 format).
// Format after base64-decode: [12-byte nonce][ciphertext+tag].
func (s *Service) decryptV1(b64 string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("crypto v1: base64 decode: %w", err)
	}
	if len(data) < 12 {
		return nil, errors.New("crypto v1: ciphertext too short")
	}

	iterations := len(s.password) * 1000
	if iterations == 0 {
		iterations = 1000
	}
	key := pbkdf2.Key([]byte(s.password), []byte(saltOfPassphrase), iterations, 32, sha512.New)

	nonce := data[:12]
	encrypted := data[12:]
	return aesGCMDecrypt(key, nonce, encrypted)
}

// hkdfKey derives the 32-byte AES key using HKDF-SHA256.
func (s *Service) hkdfKey() ([]byte, error) {
	r := hkdf.New(sha256.New, []byte(s.password), s.salt, nil)
	key := make([]byte, 32)
	if _, err := r.Read(key); err != nil {
		return nil, fmt.Errorf("crypto: HKDF key derivation: %w", err)
	}
	return key, nil
}

// aesGCMDecrypt decrypts data with AES-256-GCM.
func aesGCMDecrypt(key, nonce, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: AES new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: AES-GCM: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: AES-GCM decrypt: %w", err)
	}
	return plaintext, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
