package uploads

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestSanitizeFilenameMatchesPython diffs the port against the real function over a corpus, most of it randomly assembled from the characters that matter: dots, slashes, backslashes, whitespace and control bytes. The order of the operations is what decides the answer — whitespace is stripped before the leading dots so " .env" loses both — and a table written by hand would not have found that.
func TestSanitizeFilenameMatchesPython(t *testing.T) {
	fixture, err := os.ReadFile("testdata/sanitize_filename.tsv")
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, line := range strings.Split(string(fixture), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rawIn, rawWant, found := strings.Cut(line, "\t")
		if !found {
			t.Fatalf("malformed fixture row %q", line)
		}
		input, err := unquotePython(rawIn)
		if err != nil {
			t.Fatalf("parse %s: %v", rawIn, err)
		}
		want := ""
		if rawWant != "-" {
			if want, err = unquotePython(rawWant); err != nil {
				t.Fatalf("parse %s: %v", rawWant, err)
			}
		}
		rows++
		if got := SanitizeFilename(input); got != want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", input, got, want)
		}
	}
	if rows < 300 {
		t.Fatalf("fixture has only %d rows", rows)
	}
}

// unquotePython reads the repr() the generator wrote. Python prefers single quotes and escapes the same way Go does for the characters this corpus uses.
func unquotePython(value string) (string, error) {
	if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") && len(value) >= 2 {
		inner := strings.ReplaceAll(value[1:len(value)-1], `"`, `\"`)
		return strconv.Unquote(`"` + inner + `"`)
	}
	return strconv.Unquote(value)
}

// A name that sanitizes to nothing becomes the placeholder rather than being refused.
func TestAnEmptyResultBecomesThePlaceholder(t *testing.T) {
	for _, input := range []string{"", "   ", "...", "..", "/", "\\", "\x00"} {
		if SanitizeFilename(input) != "" {
			t.Errorf("SanitizeFilename(%q) should strip to nothing", input)
		}
	}
}

// The point of the function: nothing that survives may still reach out of its directory.
func TestNothingEscapesTheObjectKey(t *testing.T) {
	for _, input := range []string{
		"../../etc/passwd", `..\..\windows\system32`, "/absolute/path.txt",
		"dir/sub/file.txt", `dir\sub\file.txt`, "....//....//etc/passwd",
	} {
		got := SanitizeFilename(input)
		if strings.ContainsAny(got, `/\`) || strings.Contains(got, "..") {
			t.Errorf("SanitizeFilename(%q) = %q, which still carries a path", input, got)
		}
	}
}
