package crypto

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cespare/xxhash/v2"
)

func TestChunkID_WithoutPassword(t *testing.T) {
	svc := New("")
	data := "hello world"

	id := svc.ChunkID(data)

	// Verify format: "h:" + base36(xxhash64("${data}-${charCount}"))
	length := utf8.RuneCountInString(data)
	input := data + "-" + strconv.Itoa(length)
	expected := "h:" + strconv.FormatUint(xxhash.Sum64String(input), 36)
	if id != expected {
		t.Errorf("ChunkID without password: got %q, want %q", id, expected)
	}
	if !strings.HasPrefix(id, "h:") {
		t.Errorf("ChunkID should start with 'h:', got %q", id)
	}
}

func TestChunkID_Deterministic(t *testing.T) {
	svc := New("")
	data := "some content"
	id1 := svc.ChunkID(data)
	id2 := svc.ChunkID(data)
	if id1 != id2 {
		t.Errorf("ChunkID should be deterministic: %q != %q", id1, id2)
	}
}

func TestChunkID_WithPassword_DifferentHash(t *testing.T) {
	svcPlain := New("")
	svcE2EE := New("mysecret")
	data := "hello world"

	idPlain := svcPlain.ChunkID(data)
	idE2EE := svcE2EE.ChunkID(data)

	if idPlain == idE2EE {
		t.Error("ChunkID with and without password should differ")
	}
	if len(idE2EE) < 3 || idE2EE[:3] != "h:+" {
		t.Errorf("ChunkID with password should start with 'h:+', got %q", idE2EE)
	}
}

func TestChunkID_FormatMatchesObsidian(t *testing.T) {
	// Obsidian Livesync uses xxHash-64 of "${data}-${charCount}" encoded as
	// base36 (lowercase alphanumeric). IDs for short content are 10-13 chars.
	svc := New("")
	id := svc.ChunkID("hello world")
	// Must start with "h:" and be followed by lowercase base36 digits.
	if !strings.HasPrefix(id, "h:") {
		t.Fatalf("expected h: prefix, got %q", id)
	}
	suffix := id[2:]
	for _, ch := range suffix {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'z')) {
			t.Errorf("non-base36 char %q in ID %q", ch, id)
		}
	}
	if len(suffix) < 8 || len(suffix) > 13 {
		t.Errorf("unexpected ID length %d for %q: %q", len(suffix), "hello world", id)
	}
}

func TestEncryptDecrypt_Roundtrip(t *testing.T) {
	svc := New("test-password")
	svc.SetSalt([]byte("test-salt-32-bytes-padding-here!"))

	plaintext := []byte("hello, encrypted world!")

	encrypted, err := svc.EncryptContent(plaintext)
	if err != nil {
		t.Fatalf("EncryptContent: %v", err)
	}

	if len(encrypted) < 2 || encrypted[:2] != "%=" {
		t.Errorf("expected encrypted to start with '%%=', got %q", encrypted[:min(len(encrypted), 4)])
	}

	decrypted, err := svc.DecryptContent(encrypted)
	if err != nil {
		t.Fatalf("DecryptContent: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("roundtrip mismatch: got %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptContent_NoE2EE_Base64(t *testing.T) {
	svc := New("")
	plaintext := []byte("no encryption")

	encoded, err := svc.EncryptContent(plaintext)
	if err != nil {
		t.Fatalf("EncryptContent (no E2EE): %v", err)
	}

	decoded, err := svc.DecryptContent(encoded)
	if err != nil {
		t.Fatalf("DecryptContent (no E2EE): %v", err)
	}

	if !bytes.Equal(plaintext, decoded) {
		t.Errorf("no-E2EE roundtrip mismatch: got %q, want %q", decoded, plaintext)
	}
}

func TestEncrypt_DifferentNonce(t *testing.T) {
	svc := New("password")
	svc.SetSalt([]byte("some-salt-32-bytes-padding-here!"))
	plaintext := []byte("same content")

	enc1, _ := svc.EncryptContent(plaintext)
	enc2, _ := svc.EncryptContent(plaintext)

	// Two encryptions of same plaintext should produce different ciphertexts (random nonce)
	if enc1 == enc2 {
		t.Error("two encryptions of same plaintext should differ due to random nonce")
	}
}

func TestDecryptPath_PlainPath(t *testing.T) {
	svc := New("password")
	svc.SetSalt([]byte("some-salt-32-bytes-padding-here!"))
	plain := "notes/hello.md"
	got, err := svc.DecryptPath(plain)
	if err != nil {
		t.Fatalf("DecryptPath on plain path: %v", err)
	}
	if got != plain {
		t.Errorf("DecryptPath on plain path: got %q, want %q", got, plain)
	}
}

func TestDecryptPath_Roundtrip(t *testing.T) {
	svc := New("mypassword")
	svc.SetSalt([]byte("roundtrip-salt-32-bytes-padded!!"))

	path := "notes/my secret note.md"
	encrypted, err := svc.EncryptPath(path, 1000, 2000, 512, []string{"h:abc"})
	if err != nil {
		t.Fatalf("EncryptPath: %v", err)
	}

	if !strings.HasPrefix(encrypted, "/\\:") {
		t.Errorf("EncryptPath should return /\\: prefix, got %q", encrypted[:min(len(encrypted), 10)])
	}

	decrypted, err := svc.DecryptPath(encrypted)
	if err != nil {
		t.Fatalf("DecryptPath: %v", err)
	}
	if decrypted != path {
		t.Errorf("DecryptPath roundtrip: got %q, want %q", decrypted, path)
	}
}

func TestDecryptPath_NoE2EE(t *testing.T) {
	svc := New("") // E2EE disabled
	obfuscated := "/\\:%=someciphertext"
	_, err := svc.DecryptPath(obfuscated)
	if err == nil {
		t.Error("DecryptPath with obfuscated path but no E2EE should return error")
	}
}

func TestObfuscateDocID_Deterministic(t *testing.T) {
	svc := New("mypassphrase")
	id1 := svc.ObfuscateDocID("notes/hello.md")
	id2 := svc.ObfuscateDocID("notes/hello.md")
	if id1 != id2 {
		t.Errorf("ObfuscateDocID should be deterministic: %q != %q", id1, id2)
	}
}

func TestObfuscateDocID_Prefix(t *testing.T) {
	svc := New("pass")
	id := svc.ObfuscateDocID("notes/foo.md")
	if !strings.HasPrefix(id, "f:") {
		t.Errorf("ObfuscateDocID should return f: prefix, got %q", id)
	}
}

func TestObfuscateDocID_CaseInsensitive(t *testing.T) {
	svc := New("pass")
	id1 := svc.ObfuscateDocID("Notes/Foo.md")
	id2 := svc.ObfuscateDocID("notes/foo.md")
	if id1 != id2 {
		t.Errorf("ObfuscateDocID should be case-insensitive: %q != %q", id1, id2)
	}
}

func TestObfuscateDocID_DifferentPaths(t *testing.T) {
	svc := New("pass")
	id1 := svc.ObfuscateDocID("notes/a.md")
	id2 := svc.ObfuscateDocID("notes/b.md")
	if id1 == id2 {
		t.Error("different paths should produce different obfuscated IDs")
	}
}
