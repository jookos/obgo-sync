package couchdb

// MetaDoc represents a file metadata document in CouchDB.
type MetaDoc struct {
	ID        string   `json:"_id"`
	Rev       string   `json:"_rev,omitempty"`
	Type      string   `json:"type"`
	CTime     int64    `json:"ctime"`
	MTime     int64    `json:"mtime"`
	Size      int64    `json:"size"`
	Path      string   `json:"path"`
	Children  []string `json:"children"`
	Deleted   bool     `json:"_deleted,omitempty"`
	Encrypted bool     `json:"e_,omitempty"`
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
