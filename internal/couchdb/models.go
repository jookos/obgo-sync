package couchdb

// MetaDoc represents a file metadata document in CouchDB.
type MetaDoc struct {
	ID         string                 `json:"_id"`
	Rev        string                 `json:"_rev,omitempty"`
	Type       string                 `json:"type"`
	CTime      int64                  `json:"ctime"`
	MTime      int64                  `json:"mtime"`
	Size       int64                  `json:"size"`
	Path       string                 `json:"path"`
	Children   []string               `json:"children"`
	Eden       map[string]interface{} `json:"eden"`
	Deleted    bool                   `json:"_deleted,omitempty"` // CouchDB-level tombstone (read-only; written only by PouchDB/HTTP DELETE)
	DeletedApp bool                   `json:"deleted,omitempty"`  // Livesync app-level deletion; preserves all fields for path resolution
	Encrypted  bool                   `json:"e_,omitempty"`
	Conflicts  []string               `json:"_conflicts,omitempty"`
}

// IsDeleted reports whether the document is deleted by either mechanism:
// a CouchDB-level tombstone (_deleted) or a Livesync app-level marker (deleted).
func (d MetaDoc) IsDeleted() bool {
	return d.Deleted || d.DeletedApp
}

// ChunkDoc represents a data chunk document in CouchDB.
type ChunkDoc struct {
	ID        string `json:"_id"`
	Rev       string `json:"_rev,omitempty"`
	Data      string `json:"data"`
	Type      string `json:"type"` // "leaf"
	Encrypted bool   `json:"e_,omitempty"`
}

// ChangeEvent represents a single entry from the CouchDB _changes feed.
type ChangeEvent struct {
	Seq     interface{} `json:"seq"`
	ID      string      `json:"id"`
	Changes []struct {
		Rev string `json:"rev"`
	} `json:"changes"`
	Deleted bool     `json:"deleted,omitempty"`
	Doc     *MetaDoc `json:"doc,omitempty"`
}
