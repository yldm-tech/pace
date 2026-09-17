package pdfdoc

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yldm-tech/pace/apps/api/internal/ydoc"
)

// documentCase is one entry of the editor corpus, which is reused here so that every shape the editor can produce is also drawn.
type documentCase struct {
	Name   string `json:"name"`
	Binary string `json:"binary"`
}

func loadDocuments(t *testing.T) []documentCase {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "ydoc", "testdata", "documents.json"))
	if err != nil {
		t.Fatalf("read the corpus: %v", err)
	}
	var cases []documentCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse the corpus: %v", err)
	}
	return cases
}

// TestEveryDocumentDraws is the broadest check there is: every document the editor can produce is drawn, and none of them may fail or come back empty. The output cannot be compared with the original's — a different layout engine writes different bytes — so what is checked is that every shape has a renderer and every renderer survives being given one.
func TestEveryDocumentDraws(t *testing.T) {
	for _, testCase := range loadDocuments(t) {
		t.Run(testCase.Name, func(t *testing.T) {
			update, err := base64.StdEncoding.DecodeString(testCase.Binary)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			document, err := ydoc.Parse(update)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			out, err := Render(document, Options{Title: "A page"})
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			assertIsPDF(t, out)
		})
	}
}

func assertIsPDF(t *testing.T, out []byte) {
	t.Helper()
	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatalf("the output is not a PDF: %q", out[:min(16, len(out))])
	}
	if !bytes.Contains(out, []byte("%%EOF")) {
		t.Error("the output has no end marker")
	}
	if len(out) < 1000 {
		t.Errorf("the output is %d bytes, which is too small to hold an embedded font", len(out))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func parse(t *testing.T, html string) ydoc.Node {
	t.Helper()
	document, err := ydoc.ParseHTML(html)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return document
}

// TestEveryPageSizeAndOrientation covers the six sizes and the two orientations a request may name, and the fallback for one it may not.
func TestEveryPageSizeAndOrientation(t *testing.T) {
	document := parse(t, "<p>a page</p>")
	for _, size := range []string{"A4", "A3", "A2", "LETTER", "LEGAL", "TABLOID", "", "something else"} {
		for _, orientation := range []string{"portrait", "landscape", ""} {
			out, err := Render(document, Options{PageSize: size, Orientation: orientation})
			if err != nil {
				t.Fatalf("%s %s: %v", size, orientation, err)
			}
			assertIsPDF(t, out)
		}
	}
}

// TestALongDocumentRunsOntoMorePages covers the page break, which is the one thing the layout does that a short document never exercises.
func TestALongDocumentRunsOntoMorePages(t *testing.T) {
	var html strings.Builder
	for i := 0; i < 200; i++ {
		html.WriteString("<p>A paragraph with enough words in it to take a whole line of the page, repeated until the page is full and then some.</p>")
	}
	out, err := Render(parse(t, html.String()), Options{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if pages := bytes.Count(out, []byte("/Type /Page\n")); pages < 2 {
		t.Errorf("a two hundred paragraph document came out as %d pages", pages)
	}
}

// TestAMentionNamesThePersonWhenItCan covers the metadata: a mention is drawn as the person's name, and as "@unknown" when nothing says who they are.
func TestAMentionNamesThePersonWhenItCan(t *testing.T) {
	document := parse(t, `<p><mention-component entity_identifier="u1" entity_name="user_mention"></mention-component></p>`)

	named, err := Render(document, Options{UserMentions: map[string]string{"u1": "Someone"}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	assertIsPDF(t, named)

	unnamed, err := Render(document, Options{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	assertIsPDF(t, unnamed)

	// The two differ because the text drawn differs, which is the only way to see from outside that the name was used.
	if bytes.Equal(named, unnamed) {
		t.Error("naming the person changed nothing")
	}
}

// TestAssetIDsFindsTheStoredImages covers what the service has to fetch before a page can be drawn: the images it holds itself, and not the ones it merely links to.
func TestAssetIDsFindsTheStoredImages(t *testing.T) {
	document := parse(t, `<image-component src="11111111-1111-4111-8111-111111111111"></image-component><img src="https://example.test/a.png"><img src="data:image/png;base64,AA"><image-component src="11111111-1111-4111-8111-111111111111"></image-component>`)
	found := AssetIDs(document)
	if len(found) != 1 || found[0] != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("assets = %v, want the one stored image, once", found)
	}
}

// TestNoAssetsLeavesThePicturesOut covers the option the request may ask for.
func TestNoAssetsLeavesThePicturesOut(t *testing.T) {
	document := parse(t, `<image-component src="an-asset"></image-component>`)
	images := map[string][]byte{"an-asset": onePixelPNG()}

	with, err := Render(document, Options{Images: images})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	without, err := Render(document, Options{Images: images, NoAssets: true})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if bytes.Equal(with, without) {
		t.Error("leaving the assets out changed nothing")
	}
	assertIsPDF(t, with)
	assertIsPDF(t, without)
}

// TestAnImageThatWillNotDecodeIsAPlaceholder covers the failure that must not fail the export.
func TestAnImageThatWillNotDecodeIsAPlaceholder(t *testing.T) {
	document := parse(t, `<image-component src="an-asset"></image-component>`)
	out, err := Render(document, Options{Images: map[string][]byte{"an-asset": []byte("not a picture")}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	assertIsPDF(t, out)
}

// TestTheStylesheetIsTheOnesTheExporterUses guards the generated file against arriving empty or missing the entries the renderer reads by name.
func TestTheStylesheetIsTheOnesTheExporterUses(t *testing.T) {
	for _, name := range []string{
		"page", "title", "heading1", "heading6", "paragraphWrapper", "blockquote", "codeBlock", "codeInline",
		"bulletList", "orderedList", "listItem", "taskList", "taskItem", "taskCheckbox", "taskCheckboxChecked",
		"table", "tableRow", "tableHeaderRow", "tableCell", "horizontalRule", "image", "imagePlaceholder",
		"imagePlaceholderText", "callout", "mention", "link",
	} {
		if _, ok := styles.Styles[name]; !ok {
			t.Errorf("the stylesheet has no %s", name)
		}
	}
	if got := style("page"); got.Padding != 40 || got.FontSize != 11 || got.LineHeight != 1.6 {
		t.Errorf("the page style is not the exporter's: %+v", got)
	}
}

func TestRGB(t *testing.T) {
	for colour, want := range map[string][3]int{
		"#1f1f1f":    {31, 31, 31},
		"1f1f1f":     {31, 31, 31},
		"#fff":       {255, 255, 255},
		"#3f76ff33":  {63, 118, 255},
		"":           {0, 0, 0},
		"rgb(1,2,3)": {0, 0, 0},
	} {
		red, green, blue := rgb(colour)
		if [3]int{red, green, blue} != want {
			t.Errorf("rgb(%q) = %d %d %d, want %v", colour, red, green, blue, want)
		}
	}
}

// TestLineBreaking covers the layout decision the whole of the text rendering rests on.
func TestLineBreaking(t *testing.T) {
	pieces := []piece{
		{text: "one", width: 30}, {text: " ", width: 5, space: true},
		{text: "two", width: 30}, {text: " ", width: 5, space: true},
		{text: "three", width: 50},
	}
	lines := breakLines(pieces, 70)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want two", len(lines))
	}
	if got := lineText(lines[0]); got != "one two" {
		t.Errorf("first line = %q", got)
	}
	if got := lineText(lines[1]); got != "three" {
		t.Errorf("second line = %q, want the space at the break dropped", got)
	}

	// A word wider than the line gets a line of its own rather than being cut.
	long := breakLines([]piece{{text: "unbreakable", width: 500}}, 70)
	if len(long) != 1 || lineText(long[0]) != "unbreakable" {
		t.Errorf("a word wider than the line was not kept whole: %v", long)
	}

	// A hard break ends the line wherever it falls.
	broken := breakLines([]piece{{text: "one", width: 10}, {text: "\n"}, {text: "two", width: 10}}, 500)
	if len(broken) != 2 {
		t.Errorf("a hard break gave %d lines, want two", len(broken))
	}
}

func lineText(line []piece) string {
	var out strings.Builder
	for _, p := range line {
		out.WriteString(p.text)
	}
	return out.String()
}

func TestSplitKeepingSpaces(t *testing.T) {
	got := splitKeepingSpaces("one  two three")
	want := []string{"one", "  ", "two", " ", "three"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("split = %v, want %v", got, want)
	}
}

// onePixelPNG is the smallest picture there is, for the tests that need one.
func onePixelPNG() []byte {
	encoded := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		panic(err)
	}
	return decoded
}
