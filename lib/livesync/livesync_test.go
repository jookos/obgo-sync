package livesync

import (
	"bytes"
	"testing"
)

// --- Split / Assemble tests ---

func TestSplitAssemble_Empty(t *testing.T) {
	data := []byte{}
	chunks := Split(data, 0)
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty data, got %d", len(chunks))
	}
	result := Assemble(chunks)
	if len(result) != 0 {
		t.Errorf("expected empty assembled result, got %d bytes", len(result))
	}
}

func TestSplitAssemble_SmallData(t *testing.T) {
	data := []byte("hello world")
	chunks := Split(data, 0)
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk for small data, got %d", len(chunks))
	}
	result := Assemble(chunks)
	if !bytes.Equal(result, data) {
		t.Errorf("Assemble result mismatch: got %q, want %q", result, data)
	}
}

func TestSplitAssemble_ExactChunkSize(t *testing.T) {
	data := bytes.Repeat([]byte("a"), 100)
	chunks := Split(data, 100)
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk for exact-size data, got %d", len(chunks))
	}
	result := Assemble(chunks)
	if !bytes.Equal(result, data) {
		t.Errorf("Assemble result mismatch")
	}
}

func TestSplitAssemble_MultipleChunks(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 250)
	chunks := Split(data, 100)
	if len(chunks) != 3 {
		t.Errorf("expected 3 chunks, got %d", len(chunks))
	}
	if len(chunks[0]) != 100 {
		t.Errorf("first chunk: expected 100 bytes, got %d", len(chunks[0]))
	}
	if len(chunks[1]) != 100 {
		t.Errorf("second chunk: expected 100 bytes, got %d", len(chunks[1]))
	}
	if len(chunks[2]) != 50 {
		t.Errorf("third chunk: expected 50 bytes, got %d", len(chunks[2]))
	}
	result := Assemble(chunks)
	if !bytes.Equal(result, data) {
		t.Errorf("Assemble result mismatch after multi-chunk split")
	}
}

func TestSplitAssemble_DefaultChunkSize(t *testing.T) {
	data := bytes.Repeat([]byte("y"), defaultChunkSize+1)
	chunks := Split(data, 0)
	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks with default chunk size, got %d", len(chunks))
	}
	result := Assemble(chunks)
	if !bytes.Equal(result, data) {
		t.Errorf("Assemble result mismatch with default chunk size")
	}
}

// --- EncodeDocID tests ---

func TestEncodeDocID_NormalPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"notes/foo.md", "notes/foo.md"},
		{"daily/2024-01-01.md", "daily/2024-01-01.md"},
		{"README.md", "readme.md"},
		{"folder/sub/file.txt", "folder/sub/file.txt"},
		{"Test.md", "test.md"},
		{"Folder/Sub/File.md", "folder/sub/file.md"},
	}
	for _, tc := range cases {
		got := EncodeDocID(tc.path)
		if got != tc.want {
			t.Errorf("EncodeDocID(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestEncodeDocID_PathStartingWithUnderscore(t *testing.T) {
	got := EncodeDocID("_design/myview")
	want := "/_design/myview"
	if got != want {
		t.Errorf("EncodeDocID(%q) = %q, want %q", "_design/myview", got, want)
	}

	got = EncodeDocID("_foo.md")
	want = "/_foo.md"
	if got != want {
		t.Errorf("EncodeDocID(%q) = %q, want %q", "_foo.md", got, want)
	}
}

func TestEncodeDocID_SpecialChars(t *testing.T) {
	// Paths with spaces and unicode are lowercased.
	got := EncodeDocID("my notes/café notes.md")
	want := "my notes/café notes.md"
	if got != want {
		t.Errorf("EncodeDocID(%q) = %q, want %q", "my notes/café notes.md", got, want)
	}
}

// --- DecodeDocID tests ---

func TestDecodeDocID_NonFileIDs(t *testing.T) {
	nonFileCases := []string{
		"h:abc123",
		"h:+deadbeef",
		"i:someindex",
		"f:someflag",
		"ix:someindex",
	}
	for _, id := range nonFileCases {
		path, isFile := DecodeDocID(id)
		if isFile {
			t.Errorf("DecodeDocID(%q): expected isFile=false, got true (path=%q)", id, path)
		}
	}
}

func TestDecodeDocID_FilePaths(t *testing.T) {
	fileCases := []struct {
		id   string
		path string
	}{
		{"notes/foo.md", "notes/foo.md"},
		{"daily/2024-01-01.md", "daily/2024-01-01.md"},
		{"/_foo.md", "_foo.md"},
	}
	for _, tc := range fileCases {
		path, isFile := DecodeDocID(tc.id)
		if !isFile {
			t.Errorf("DecodeDocID(%q): expected isFile=true, got false", tc.id)
		}
		if path != tc.path {
			t.Errorf("DecodeDocID(%q) path = %q, want %q", tc.id, path, tc.path)
		}
	}
}

func TestEncodeDecodeDocID_RoundTrip(t *testing.T) {
	paths := []string{
		"notes/foo.md",
		"_underscore.md",
		"folder/sub/file.txt",
	}
	for _, original := range paths {
		encoded := EncodeDocID(original)
		decoded, isFile := DecodeDocID(encoded)
		if !isFile {
			t.Errorf("round-trip for %q: isFile=false", original)
		}
		if decoded != original {
			t.Errorf("round-trip for %q: got %q", original, decoded)
		}
	}
}
