package livesync

const defaultChunkSize = 102400 // 100 KB

// DefaultChunkSize is the max byte/character count per binary chunk,
// matching Obsidian Livesync's MAX_DOC_SIZE_BIN constant (102400 chars of base64).
const DefaultChunkSize = defaultChunkSize

// DefaultTextChunkSize is the max character count per plain-text chunk,
// matching Obsidian Livesync's MAX_DOC_SIZE constant.
const DefaultTextChunkSize = 1000

// Split splits data into chunks of up to chunkSize bytes.
// If chunkSize <= 0, uses the default of 102400 (100 KB).
func Split(data []byte, chunkSize int) [][]byte {
	if chunkSize <= 0 {
		chunkSize = defaultChunkSize
	}

	if len(data) == 0 {
		return [][]byte{}
	}

	var chunks [][]byte
	for len(data) > 0 {
		size := chunkSize
		if size > len(data) {
			size = len(data)
		}
		// Copy the chunk so callers own their slices.
		chunk := make([]byte, size)
		copy(chunk, data[:size])
		chunks = append(chunks, chunk)
		data = data[size:]
	}
	return chunks
}

// SplitText splits a UTF-8 string into chunks of at most chunkSize Unicode
// code points. If chunkSize <= 0, DefaultTextChunkSize (1000) is used.
// This matches Obsidian Livesync's plain-text chunking (MAX_DOC_SIZE = 1000).
func SplitText(text string, chunkSize int) []string {
	if chunkSize <= 0 {
		chunkSize = DefaultTextChunkSize
	}
	if len(text) == 0 {
		return []string{}
	}
	runes := []rune(text)
	var chunks []string
	for len(runes) > 0 {
		size := chunkSize
		if size > len(runes) {
			size = len(runes)
		}
		chunks = append(chunks, string(runes[:size]))
		runes = runes[size:]
	}
	return chunks
}

// Assemble concatenates chunks into the original data.
func Assemble(chunks [][]byte) []byte {
	var total int
	for _, c := range chunks {
		total += len(c)
	}
	result := make([]byte, 0, total)
	for _, c := range chunks {
		result = append(result, c...)
	}
	return result
}
