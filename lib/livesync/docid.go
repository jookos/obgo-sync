package livesync

import "strings"

// nonFileIDPrefixes lists the prefixes used for non-file documents in CouchDB.
var nonFileIDPrefixes = []string{"h:", "i:", "f:", "ix:"}

// EncodeDocID converts a vault-relative file path to a CouchDB document ID.
// Paths starting with "_" get a "/" prepended (e.g. "_foo" → "/_foo")
// to avoid conflicts with CouchDB's reserved "_" namespace.
func EncodeDocID(path string) string {
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
