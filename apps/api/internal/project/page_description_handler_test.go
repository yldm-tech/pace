package project

import (
	"strings"
	"testing"
	"time"
)

// validate_binary_data looks at the first two hundred bytes only, lowercased, with undecodable runs dropped.
func TestTheBinaryValidatorRefusesWhatItShould(t *testing.T) {
	for name, test := range map[string]struct {
		data    []byte
		message string
	}{
		"empty":        {nil, ""},
		"too short":    {[]byte("ab"), "Binary data too short to be valid document format"},
		"exactly four": {[]byte{1, 2, 3, 4}, ""},
		"html":         {[]byte("<html><body>hello</body></html>"), "Binary data contains suspicious content patterns"},
		"doctype":      {[]byte("<!DOCTYPE html><p>x</p>"), "Binary data contains suspicious content patterns"},
		"script":       {[]byte("prefix <SCRIPT>alert(1)</script>"), "Binary data contains suspicious content patterns"},
		"javascript":   {[]byte("aaaajavascript:alert(1)"), "Binary data contains suspicious content patterns"},
		"data uri":     {[]byte("aaaadata:text/html,x"), "Binary data contains suspicious content patterns"},
		"iframe":       {[]byte("aaaa<iframe src=x>"), "Binary data contains suspicious content patterns"},
		"binary":       {[]byte{0x00, 0x01, 0x02, 0x03, 0xff, 0xfe}, ""},
	} {
		if got := validateBinaryDocument(test.data); got != test.message {
			t.Errorf("%s: got %q, want %q", name, got, test.message)
		}
	}
}

// Only the first two hundred characters are scanned, so a document that hides a pattern past them is let through — including one that merely straddles the edge.
func TestTheBinaryValidatorOnlyScansTheStart(t *testing.T) {
	for name, data := range map[string][]byte{
		"past the window": append([]byte(strings.Repeat("a", 200)), []byte("<script>")...),
		"straddling it":   append([]byte(strings.Repeat("a", 195)), []byte("<script>")...),
		"just inside it":  append([]byte(strings.Repeat("a", 192)), []byte("<script>")...),
	} {
		got := validateBinaryDocument(data)
		if name == "just inside it" {
			if got == "" {
				t.Error("a pattern that fits inside the window must be caught")
			}
			continue
		}
		if got != "" {
			t.Errorf("%s: caught as %q, and Python does not look there", name, got)
		}
	}
}

// The window is two hundred characters of decoded text, not two hundred bytes. A document full of multibyte characters is therefore scanned further in than a byte window would reach.
func TestTheBinaryWindowIsCountedInCharacters(t *testing.T) {
	// 150 two-byte characters is 300 bytes, so a byte window would stop well before the pattern.
	data := []byte(strings.Repeat("é", 150) + "<script>")
	if len(data) <= 200 {
		t.Fatalf("the fixture is %d bytes, which does not reach past a byte window", len(data))
	}
	if got := validateBinaryDocument(data); got == "" {
		t.Error("the pattern is inside two hundred characters and must be caught")
	}
}

// Ten megabytes is the cap both validators share.
func TestTheBinaryValidatorCapsAtTenMegabytes(t *testing.T) {
	if maxDocumentSize != 10*1024*1024 {
		t.Fatalf("the cap is %d, want ten megabytes", maxDocumentSize)
	}
	if got := validateBinaryDocument(make([]byte, maxDocumentSize+1)); got != "Binary data exceeds maximum size limit (10MB)" {
		t.Errorf("an oversized document was answered with %q", got)
	}
	if got := validateBinaryDocument(make([]byte, maxDocumentSize)); got != "" {
		t.Errorf("a document exactly at the cap was refused with %q", got)
	}
}

// A locked or archived page refuses with a numbered code rather than a message, which is the only place that shape appears in the migrated surface.
func TestThePageErrorCodesAreTheOnesInTheTable(t *testing.T) {
	if errorCodePageLocked != 4701 || errorCodePageArchived != 4702 {
		t.Fatalf("the codes are %d and %d, want 4701 and 4702", errorCodePageLocked, errorCodePageArchived)
	}
}

// The version list omits the documents and the detail carries them.
func TestThePageVersionDetailAddsTheDocuments(t *testing.T) {
	version := PageVersion{ID: "version-id", LastSavedAt: time.Now()}
	listed := pageVersionJSON(version, false)
	detailed := pageVersionJSON(version, true)

	if len(listed) != 9 {
		t.Fatalf("the version list row has %d fields, want 9", len(listed))
	}
	if len(detailed) != 14 {
		t.Fatalf("the version detail has %d fields, want 14", len(detailed))
	}
	for _, field := range []string{"description_binary", "description_html", "description_json", "description_stripped", "sub_pages_data"} {
		if _, present := listed[field]; present {
			t.Errorf("the list carries %q, which its serializer does not name", field)
		}
		if _, present := detailed[field]; !present {
			t.Errorf("the detail is missing %q", field)
		}
	}
}

// A duplicate carries the description but not the collaborative binary, so the editor rebuilds the document from the HTML.
func TestADuplicateDropsTheBinary(t *testing.T) {
	page := Page{ID: "page-id", Name: "Notes", DescriptionHTML: "<p>text</p>", DescriptionBinary: []byte{1, 2, 3, 4}}
	copied := page
	copied.Name = page.Name + " (Copy)"
	copied.DescriptionBinary = nil

	if copied.DescriptionHTML != page.DescriptionHTML {
		t.Error("the copy keeps the rendered description")
	}
	if copied.DescriptionBinary != nil {
		t.Error("the copy must not keep the collaborative binary")
	}
	if copied.Name != "Notes (Copy)" {
		t.Errorf("the copy is named %q", copied.Name)
	}
}
