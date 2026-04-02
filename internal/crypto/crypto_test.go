package crypto

import (
	"crypto/sha256"
	"errors"
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

func TestEncryptContent_NotImplemented(t *testing.T) {
	svc := New("password")
	_, err := svc.EncryptContent([]byte("plaintext"))
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented, got %v", err)
	}
}

func TestDecryptContent_NotImplemented(t *testing.T) {
	svc := New("password")
	_, err := svc.DecryptContent("ciphertext")
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented, got %v", err)
	}
}
