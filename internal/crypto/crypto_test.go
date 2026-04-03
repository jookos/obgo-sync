package crypto

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestChunkID_WithoutPassword(t *testing.T) {
	svc := New("")
	content := []byte("hello world")

	id := svc.ChunkID(content)

	sum := sha256.Sum256(content)
	expected := "h:" + fmt.Sprintf("%x", sum)
	if id != expected {
		t.Errorf("ChunkID without password: got %q, want %q", id, expected)
	}
}

func TestChunkID_Deterministic(t *testing.T) {
	svc := New("")
	content := []byte("some content")
	id1 := svc.ChunkID(content)
	id2 := svc.ChunkID(content)
	if id1 != id2 {
		t.Errorf("ChunkID should be deterministic: %q != %q", id1, id2)
	}
}

func TestChunkID_WithPassword_DifferentHash(t *testing.T) {
	svcPlain := New("")
	svcE2EE := New("mysecret")
	content := []byte("hello world")

	idPlain := svcPlain.ChunkID(content)
	idE2EE := svcE2EE.ChunkID(content)

	if idPlain == idE2EE {
		t.Error("ChunkID with and without password should differ")
	}
	if len(idE2EE) < 3 || idE2EE[:3] != "h:+" {
		t.Errorf("ChunkID with password should start with 'h:+', got %q", idE2EE)
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
