package sync

import (
	"os"
	"path/filepath"
	"strings"
)

// resolveCase returns the real absolute path for relPath under dataDir,
// doing a case-insensitive component-by-component walk when the exact path
// does not exist on disk. This handles vaults whose path field uses original
// casing (e.g. "Projekt/foo.md") while the CouchDB document ID is lowercase
// (e.g. "projekt/foo.md") on a case-sensitive Linux filesystem.
//
// If no match is found even case-insensitively, the original (non-existent)
// path is returned so that the caller's os.Remove / os.Stat fail gracefully.
func resolveCase(dataDir, relPath string) string {
	absPath := filepath.Join(dataDir, filepath.FromSlash(relPath))
	if _, err := os.Stat(absPath); err == nil {
		return absPath // exact match — fast path
	}

	// Walk each path component individually, matching case-insensitively.
	components := strings.Split(filepath.FromSlash(relPath), string(filepath.Separator))
	current := dataDir
	for _, component := range components {
		entries, err := os.ReadDir(current)
		if err != nil {
			return absPath // directory unreadable — give up
		}
		lower := strings.ToLower(component)
		found := false
		for _, entry := range entries {
			if strings.ToLower(entry.Name()) == lower {
				current = filepath.Join(current, entry.Name())
				found = true
				break
			}
		}
		if !found {
			return absPath // no case-insensitive match — file does not exist
		}
	}
	return current
}
