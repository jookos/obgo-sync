package watcher

import (
	"testing"
	"time"
)

func TestSuppressSet_AddAndIsSuppressed(t *testing.T) {
	s := NewSuppressSet()

	if s.IsSuppressed("notes/foo.md") {
		t.Error("path should not be suppressed before Add")
	}

	s.Add("notes/foo.md")

	if !s.IsSuppressed("notes/foo.md") {
		t.Error("path should be suppressed after Add")
	}
}

func TestSuppressSet_OtherPathNotSuppressed(t *testing.T) {
	s := NewSuppressSet()
	s.Add("notes/foo.md")

	if s.IsSuppressed("notes/bar.md") {
		t.Error("a different path should not be suppressed")
	}
}

func TestSuppressSet_TTLExpiry(t *testing.T) {
	s := NewSuppressSet()
	s.ttl = 50 * time.Millisecond

	s.Add("notes/foo.md")

	if !s.IsSuppressed("notes/foo.md") {
		t.Error("path should be suppressed immediately after Add")
	}

	time.Sleep(100 * time.Millisecond)

	if s.IsSuppressed("notes/foo.md") {
		t.Error("path should no longer be suppressed after TTL expires")
	}
}

func TestSuppressSet_ReAddAfterExpiry(t *testing.T) {
	s := NewSuppressSet()
	s.ttl = 50 * time.Millisecond

	s.Add("notes/foo.md")
	time.Sleep(100 * time.Millisecond)

	// Re-add after expiry
	s.Add("notes/foo.md")

	if !s.IsSuppressed("notes/foo.md") {
		t.Error("path should be suppressed after re-Add")
	}
}
