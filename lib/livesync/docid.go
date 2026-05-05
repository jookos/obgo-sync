package livesync

import "strings"

// nonFileIDPrefixes lists the prefixes used for non-file documents in CouchDB.
// Note: "f:" is intentionally absent — obfuscated file meta-docs use that prefix
// and are valid file documents; their path is stored in the encrypted path field.
var nonFileIDPrefixes = []string{"h:", "i:", "ix:"}

// IsObfuscatedDocID reports whether id is an obfuscated file meta-doc ID (f: prefix).
// These documents contain vault files whose paths are encrypted in the path field.
func IsObfuscatedDocID(id string) bool {
	return strings.HasPrefix(id, "f:")
}

// EncodeDocID converts a vault-relative file path to a CouchDB document ID.
// The ID is always lowercased so obgo uses the same document as Obsidian,
// which normalises IDs to lowercase. Paths starting with "_" get a "/"
// prepended (e.g. "_foo" → "/_foo") to avoid CouchDB's reserved namespace.
func EncodeDocID(path string) string {
	path = strings.ToLower(path)
	if strings.HasPrefix(path, "_") {
		return "/" + path
	}
	return path
}

// DecodeDocID converts a CouchDB document ID back to a vault-relative path.
// Returns the path and isFile=false for non-file IDs (h:, i:, f:, ix: prefixes).
// Returns isFile=true for document IDs that represent vault files.
func DecodeDocID(id string) (path string, isFile bool) {
	for _, prefix := range nonFileIDPrefixes {
		if strings.HasPrefix(id, prefix) {
			return id, false
		}
	}

	// Undo the leading "/" added for paths starting with "_".
	if strings.HasPrefix(id, "/_") {
		return id[1:], true
	}

	return id, true
}
