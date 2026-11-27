package sync

import (
	"path/filepath"
	"strings"
)

func shouldSkipLocalName(name string, syncHidden bool) bool {
	if name == "." {
		return false
	}
	if isProtectedLocalName(name) {
		return true
	}
	return !syncHidden && isHiddenLocalName(name)
}

// shouldSkipLocalPath reports whether path should be excluded from a sync
// operation scoped to filterRoot (the vault-relative folder/file the caller
// explicitly requested, or "" for a whole-vault operation).
//
// Protected names (.git, .obgo*) are always excluded, anywhere in the path.
// Hidden names (dot-prefixed) are excluded only when syncHidden is off AND
// the component falls outside filterRoot — a component the caller named
// explicitly (e.g. pushing ".obsidian/" directly) is not treated as "hidden"
// for that request, matching the directory walk's own walkRoot exemption.
func shouldSkipLocalPath(path, filterRoot string, syncHidden bool) bool {
	filterParts := splitPathParts(filterRoot)
	parts := splitPathParts(path)
	for i, part := range parts {
		if isProtectedLocalName(part) {
			return true
		}
		if i < len(filterParts) && filterParts[i] == part {
			continue // part of the explicitly-requested filter, not "hidden" here
		}
		if !syncHidden && isHiddenLocalName(part) {
			return true
		}
	}
	return false
}

func splitPathParts(path string) []string {
	var parts []string
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func isHiddenLocalName(name string) bool {
	return len(name) > 0 && name[0] == '.'
}

func isProtectedLocalName(name string) bool {
	return name == ".git" || strings.HasPrefix(name, ".obgo")
}
