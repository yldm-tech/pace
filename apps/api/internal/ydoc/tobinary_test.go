package ydoc

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
)

// corpusClientID is the id both sides fix the document to, so the bytes are comparable.
const corpusClientID = 4242

type updateCase struct {
	Name            string `json:"name"`
	HTML            string `json:"html"`
	Update          string `json:"update"`
	UpdateWithTitle string `json:"update_with_title"`
}

func loadUpdateCorpus(t *testing.T) []updateCase {
	t.Helper()
	raw, err := os.ReadFile("testdata/updates.json")
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	var cases []updateCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}
	return cases
}

func decodeUpdate(t *testing.T, encoded string) []byte {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return decoded
}

// TestToUpdateMatchesEditor compares the bytes rather than the documents, because the bytes are what goes into the page's description_binary and what every connected client then synchronises against.
func TestToUpdateMatchesEditor(t *testing.T) {
	parsed := map[string]parsedCase{}
	for _, testCase := range loadParsedCorpus(t) {
		parsed[testCase.Name] = testCase
	}

	for _, testCase := range loadUpdateCorpus(t) {
		t.Run(testCase.Name, func(t *testing.T) {
			document := documentFor(t, parsed, testCase)

			update, err := ToUpdateWithOptions(document, nil, corpusClientID)
			if err != nil {
				t.Fatalf("write: %v", err)
			}
			title := TitleDocument(titleFor(testCase.Name))
			withTitle, err := ToUpdateWithOptions(document, &title, corpusClientID)
			if err != nil {
				t.Fatalf("write with title: %v", err)
			}

			// The bytes match except for the two encodings the Yjs port underneath does differently, both named by EncodesLikeTheEditor. Both halves are checked: a document that should match has to, and one that should not has to actually differ, so the exemption cannot quietly grow.
			comparable := EncodesLikeTheEditor(document)
			wantUpdate := decodeUpdate(t, testCase.Update)
			wantWithTitle := decodeUpdate(t, testCase.UpdateWithTitle)

			if comparable {
				if !bytes.Equal(update, wantUpdate) {
					t.Errorf("update differs\n go: %x\nwant: %x", update, wantUpdate)
				}
				if !bytes.Equal(withTitle, wantWithTitle) {
					t.Errorf("update with a title differs\n go: %x\nwant: %x", withTitle, wantWithTitle)
				}
			} else if bytes.Equal(update, wantUpdate) {
				t.Errorf("this document was expected to encode differently and its bytes matched anyway")
			}

			// Whether the bytes match or not, both updates have to read back to the same document.
			sameDocument(t, update, wantUpdate)
			sameDocument(t, withTitle, wantWithTitle)
		})
	}
}

// sameDocument reads two updates and insists they hold the same document, which is the guarantee that survives even where the bytes do not.
func sameDocument(t *testing.T, mine, theirs []byte) {
	t.Helper()
	ours, err := Parse(mine)
	if err != nil {
		t.Fatalf("read ours: %v", err)
	}
	editors, err := Parse(theirs)
	if err != nil {
		t.Fatalf("read the editor's: %v", err)
	}
	oursJSON, err := ours.JSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	editorsJSON, err := editors.JSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !reflect.DeepEqual(decodeJSON(t, oursJSON), decodeJSON(t, editorsJSON)) {
		t.Errorf("the two updates hold different documents\n go: %s\nwant: %s", oursJSON, editorsJSON)
	}
}

func documentFor(t *testing.T, parsed map[string]parsedCase, testCase updateCase) Node {
	t.Helper()
	if testCase.Name == "empty title" {
		document, err := ParseHTML("<p></p>")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return document
	}
	source, ok := parsed[testCase.Name]
	if !ok {
		t.Fatalf("the update corpus holds %q and the parse corpus does not", testCase.Name)
	}
	document, err := ParseHTML(source.HTML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return document
}

func titleFor(name string) string {
	if name == "empty title" {
		return ""
	}
	return fmt.Sprintf("Title of %s", name)
}

// TestUpdatesReadBackToTheSameDocument is the round trip that matters most: what is written has to be what comes back, because that is the loop a page goes through every time somebody opens it.
func TestUpdatesReadBackToTheSameDocument(t *testing.T) {
	for _, testCase := range loadParsedCorpus(t) {
		t.Run(testCase.Name, func(t *testing.T) {
			document, err := ParseHTML(testCase.HTML)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			title := TitleDocument("A title")
			update, err := ToUpdate(document, &title)
			if err != nil {
				t.Fatalf("write: %v", err)
			}

			read, err := Parse(update)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			wrote, err := document.JSON()
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got, err := read.JSON()
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !reflect.DeepEqual(decodeJSON(t, got), decodeJSON(t, wrote)) {
				t.Errorf("the document changed on the way through\n out: %s\n in: %s", got, wrote)
			}

			readTitle, err := ParseTitle(update)
			if err != nil {
				t.Fatalf("read title: %v", err)
			}
			if text := readTitle.TextContent(); text != "A title" {
				t.Errorf("title = %q", text)
			}
		})
	}
}

// TestEmptyTitleHasNoContent covers the one shape the title writer branches on.
func TestEmptyTitleHasNoContent(t *testing.T) {
	title := TitleDocument("")
	if len(title.Content) != 1 || len(title.Content[0].Content) != 0 {
		t.Fatalf("an empty title is not an empty heading: %+v", title)
	}
	document, err := ParseHTML("<p></p>")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	update, err := ToUpdate(document, &title)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	read, err := ParseTitle(update)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if text := read.TextContent(); text != "" {
		t.Errorf("title = %q, want it empty", text)
	}
}

func TestUTF16Length(t *testing.T) {
	for text, want := range map[string]int{
		"":          0,
		"abc":       3,
		"é":         1,
		"☃":         1,
		"🎉":         2,
		"a🎉b":       4,
		"Ünicode ☃": 9,
	} {
		if got := utf16Length(text); got != want {
			t.Errorf("utf16Length(%q) = %d, want %d", text, got, want)
		}
	}
}
