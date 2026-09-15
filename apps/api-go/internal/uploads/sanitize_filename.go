// Package uploads holds the pieces both APIs need to accept a file: the name a caller may not choose freely, and the types they may send. Both are settings rather than code, and both are checked against Python over a corpus, so they live in one place rather than in each app that uploads.
package uploads

import "strings"

// SanitizeFilename is plane.utils.path_validator.sanitize_filename: it strips the directory components, traversal sequences, control characters and leading dots from a caller-supplied name before that name becomes part of an object key.
func SanitizeFilename(filename string) string {
	if filename == "" {
		return ""
	}
	var stripped strings.Builder
	for _, character := range filename {
		if character < 32 || character == 127 {
			continue
		}
		stripped.WriteRune(character)
	}
	// Backslashes are normalised first so a Windows-style path loses its directories too.
	result := strings.ReplaceAll(stripped.String(), "\\", "/")
	// Everything after the last separator, which is what Python's basename returns. Go's path.Base is not that function: it trims the trailing separators first, so it turns "trailing/" into "trailing" where Python gives nothing at all.
	result = result[strings.LastIndexByte(result, '/')+1:]
	result = strings.ReplaceAll(result, "..", "")
	// The whitespace goes before the dots so a name like " .env" loses both.
	result = strings.TrimSpace(result)
	result = strings.TrimLeft(result, ".")
	return strings.TrimSpace(result)
}
