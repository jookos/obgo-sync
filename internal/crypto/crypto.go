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
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cespare/xxhash/v2"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/pbkdf2"
)

// saltOfPassphrase is the static PBKDF2 salt used by V1 encryption.
const saltOfPassphrase = "rHGMPtr6oWw7VSa3W3wpa8fT8U"

// pbkdf2Iterations matches octagonal-wheels PBKDF2_ITERATIONS (OWASP recommendation).
const pbkdf2Iterations = 310_000

// hkdfSaltLen is the per-chunk HKDF salt length embedded in each V2 ciphertext.
const hkdfSaltLen = 32

// ivLen is the AES-GCM IV (nonce) length.
const ivLen = 12

// Service handles all E2EE operations for obgo-live.
type Service struct {
	password  string
	salt      []byte // pbkdf2Salt from _local/obsidian_livesync_sync_parameters
	masterKey []byte // cached: PBKDF2-SHA256(password, salt, 310000, 32)
}

// New creates a new crypto Service with the given password.
// If password is empty, E2EE is disabled.
func New(password string) *Service {
	return &Service{password: password}
}

// Enabled reports whether E2EE is active (passphrase was provided).
func (s *Service) Enabled() bool { return s.password != "" }

// SetSalt configures the PBKDF2 salt from SyncParameters and pre-derives the
// master key so per-chunk operations only pay the cheap HKDF cost.
func (s *Service) SetSalt(salt []byte) {
	s.salt = salt
	s.masterKey = pbkdf2.Key([]byte(s.password), salt, pbkdf2Iterations, 32, sha256.New)
}

// ChunkID computes the chunk document _id for the given data string.
// The data string is the value that will be stored in the CouchDB chunk doc's
// "data" field: raw UTF-8 text for plain files, base64 for binary files.
//
// Without E2EE: "h:"  + base36(xxhash64("${data}-${charCount}"))
// With E2EE:    "h:+" + base36(xxhash64("${data}-${charCount}"))
//
// charCount is the Unicode code-point count (matching JavaScript's
// String.length for BMP-only strings), computed with utf8.RuneCountInString.
func (s *Service) ChunkID(data string) string {
	length := utf8.RuneCountInString(data)
	input := data + "-" + strconv.Itoa(length)
	h := xxhash.Sum64String(input)
	id := strconv.FormatUint(h, 36)
	if s.Enabled() {
		return "h:+" + id
	}
	return "h:" + id
}

// EncryptContent encrypts plaintext chunk data using the V2 format (prefix "%=").
// If E2EE is not enabled, the data is base64-encoded and returned as-is.
//
// V2 binary layout: [IV(12)] [HKDF_salt(32)] [AES-256-GCM ciphertext+tag]
// Key derivation:   chunkKey = HKDF-SHA256(IKM=masterKey, salt=HKDF_salt, info="")
// where             masterKey = PBKDF2-SHA256(passphrase, pbkdf2Salt, 310000, 32)
func (s *Service) EncryptContent(plaintext []byte) (string, error) {
	if !s.Enabled() {
		return base64.StdEncoding.EncodeToString(plaintext), nil
	}
	if s.masterKey == nil {
		return "", errors.New("crypto: E2EE salt not set; call SetSalt before encrypting")
	}

	chunkHkdfSalt := make([]byte, hkdfSaltLen)
	if _, err := io.ReadFull(rand.Reader, chunkHkdfSalt); err != nil {
		return "", fmt.Errorf("crypto: HKDF salt generation: %w", err)
	}
	iv := make([]byte, ivLen)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", fmt.Errorf("crypto: IV generation: %w", err)
	}

	key, err := s.deriveChunkKey(chunkHkdfSalt)
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
	ciphertext := gcm.Seal(nil, iv, plaintext, nil)

	// Pack: [IV(12)][HKDF_salt(32)][ciphertext+tag]
	blob := make([]byte, 0, ivLen+hkdfSaltLen+len(ciphertext))
	blob = append(blob, iv...)
	blob = append(blob, chunkHkdfSalt...)
	blob = append(blob, ciphertext...)
	return "%=" + base64.StdEncoding.EncodeToString(blob), nil
}

// DecryptContent decrypts a chunk data string.
// If E2EE is not enabled, the data is base64-decoded and returned as-is.
// If the data starts with "%=" it is V2 (PBKDF2→HKDF-AES-256-GCM, per-chunk salt).
// If the data starts with "%" (but not "%=") it is V1 (PBKDF2-SHA512-AES-256-GCM, static salt).
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

// decryptV2 decrypts V2-format ciphertext.
// Binary layout after base64-decode: [IV(12)][HKDF_salt(32)][AES-GCM ciphertext+tag]
func (s *Service) decryptV2(b64 string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("crypto v2: base64 decode: %w", err)
	}
	if len(data) < ivLen+hkdfSaltLen {
		return nil, errors.New("crypto v2: ciphertext too short")
	}
	if s.masterKey == nil {
		return nil, errors.New("crypto v2: E2EE salt not set; call SetSalt before decrypting")
	}

	iv := data[:ivLen]
	chunkHkdfSalt := data[ivLen : ivLen+hkdfSaltLen]
	encrypted := data[ivLen+hkdfSaltLen:]

	key, err := s.deriveChunkKey(chunkHkdfSalt)
	if err != nil {
		return nil, err
	}
	return aesGCMDecrypt(key, iv, encrypted)
}

// deriveChunkKey derives the per-chunk AES-256-GCM key using HKDF-SHA256.
// HKDF-SHA256(IKM=masterKey, salt=hkdfSalt, info="") → 32-byte key
func (s *Service) deriveChunkKey(hkdfSalt []byte) ([]byte, error) {
	r := hkdf.New(sha256.New, s.masterKey, hkdfSalt, nil)
	key := make([]byte, 32)
	if _, err := r.Read(key); err != nil {
		return nil, fmt.Errorf("crypto: HKDF chunk key derivation: %w", err)
	}
	return key, nil
}

// decryptV1 decrypts PBKDF2-SHA512-AES-256-GCM ciphertext (V1 legacy format).
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
