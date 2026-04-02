package watcher

import (
	"sync"
	"time"
)

// SuppressSet tracks vault-relative file paths that were written by the app.
// It is used by LocalWatcher to drop fsnotify events triggered by the app's
// own writes, preventing push→pull→push feedback loops.
type SuppressSet struct {
	mu      sync.Mutex
	entries map[string]time.Time
	ttl     time.Duration
}

// NewSuppressSet creates a new SuppressSet with a 2 second TTL.
func NewSuppressSet() *SuppressSet {
	return &SuppressSet{
		entries: make(map[string]time.Time),
		ttl:     2 * time.Second,
	}
}

// Add marks path as written by the app. Expires after ttl.
func (s *SuppressSet) Add(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[path] = time.Now()
}

// IsSuppressed returns true if path was added recently (within ttl).
// Lazily evicts expired entries.
func (s *SuppressSet) IsSuppressed(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.entries[path]
	if !ok {
		return false
	}
	if time.Since(t) > s.ttl {
		delete(s.entries, path)
		return false
	}
	return true
}
