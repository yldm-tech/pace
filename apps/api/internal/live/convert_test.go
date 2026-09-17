package live

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/yldm-tech/pace/apps/api/internal/ydoc"
)

type convertCase struct {
	Name              string          `json:"name"`
	Variant           string          `json:"variant"`
	HTML              string          `json:"html"`
	DescriptionBinary string          `json:"description_binary"`
	DescriptionJSON   json.RawMessage `json:"description_json"`
	DescriptionHTML   string          `json:"description_html"`
}

func loadConvertCorpus(t *testing.T) []convertCase {
	t.Helper()
	raw, err := os.ReadFile("testdata/converted.json")
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	var cases []convertCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}
	return cases
}

func convertServer(t *testing.T) *Server {
	t.Helper()
	return NewServer(Config{APIBaseURL: "http://api.test", BasePath: "/live"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func convertRequestFor(t *testing.T, server *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/live/convert-document/", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(recorder, request)
	return recorder
}

// TestConvertMatchesTheEditor is what the route exists for: the API hands it HTML whose assets have been swapped and gets back the two representations it stores beside them.
func TestConvertMatchesTheEditor(t *testing.T) {
	server := convertServer(t)
	for _, testCase := range loadConvertCorpus(t) {
		t.Run(testCase.Name, func(t *testing.T) {
			body, err := json.Marshal(map[string]string{"description_html": testCase.HTML, "variant": testCase.Variant})
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			response := convertRequestFor(t, server, string(body))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", response.Code, response.Body.String())
			}

			var answer struct {
				DescriptionJSON   json.RawMessage `json:"description_json"`
				DescriptionBinary string          `json:"description_binary"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
				t.Fatalf("decode: %v", err)
			}

			if !reflect.DeepEqual(decodeAny(t, answer.DescriptionJSON), decodeAny(t, testCase.DescriptionJSON)) {
				t.Errorf("json differs\n go: %s\nwant: %s", answer.DescriptionJSON, testCase.DescriptionJSON)
			}

			// The binary is compared as the document it holds rather than as bytes. A page's update carries the client id of whoever wrote it, and the route draws a fresh one every call — deliberately, so that two servers converting the same content do not claim the same identity. What has to match is what the update says.
			variant, err := ydoc.VariantFor(testCase.Variant)
			if err != nil {
				t.Fatalf("variant: %v", err)
			}
			sameDocumentAs(t, answer.DescriptionBinary, testCase.DescriptionBinary, variant)
		})
	}
}

// TestConvertProducesTheSameHTML covers the third representation, which the route builds and does not return.
func TestConvertProducesTheSameHTML(t *testing.T) {
	for _, testCase := range loadConvertCorpus(t) {
		t.Run(testCase.Name, func(t *testing.T) {
			variant, err := ydoc.VariantFor(testCase.Variant)
			if err != nil {
				t.Fatalf("variant: %v", err)
			}
			formats, err := ydoc.ConvertHTML(testCase.HTML, variant)
			if err != nil {
				t.Fatalf("convert: %v", err)
			}
			if formats.DescriptionHTML != testCase.DescriptionHTML {
				t.Errorf("html differs\n go: %s\nwant: %s", formats.DescriptionHTML, testCase.DescriptionHTML)
			}
		})
	}
}

// TestTheTwoVariantsDisagreeAboutTheEmbed pins the one thing they disagree about, because it is the whole reason there are two.
func TestTheTwoVariantsDisagreeAboutTheEmbed(t *testing.T) {
	const embed = `<issue-embed-component entity_identifier="y" entity_name="issue_mention"></issue-embed-component>`

	asDocument, err := ydoc.ConvertHTML(embed, ydoc.Document)
	if err != nil {
		t.Fatalf("document: %v", err)
	}
	if !strings.Contains(asDocument.DescriptionHTML, "issue-embed-component") {
		t.Errorf("the document schema lost the embed: %s", asDocument.DescriptionHTML)
	}

	asRichText, err := ydoc.ConvertHTML(embed, ydoc.RichText)
	if err != nil {
		t.Fatalf("rich: %v", err)
	}
	if strings.Contains(asRichText.DescriptionHTML, "issue-embed-component") {
		t.Errorf("the rich text schema kept an embed it has no node for: %s", asRichText.DescriptionHTML)
	}
}

// TestConvertRefusesBadRequests pins the complaints, because the caller reads them.
func TestConvertRefusesBadRequests(t *testing.T) {
	server := convertServer(t)
	for _, testCase := range []struct {
		name    string
		body    string
		wantIn  []string
		wantOut []string
	}{
		{name: "no html", body: `{"variant":"rich"}`, wantIn: []string{"HTML content cannot be empty"}},
		{name: "whitespace only", body: `{"description_html":"   ","variant":"rich"}`, wantIn: []string{"HTML content cannot be just whitespace"}},
		{name: "not html", body: `{"description_html":"just words","variant":"rich"}`, wantIn: []string{"Content must be valid HTML"}},
		{name: "no variant", body: `{"description_html":"<p>x</p>"}`, wantIn: []string{"Invalid enum value"}},
		{name: "unknown variant", body: `{"description_html":"<p>x</p>","variant":"markdown"}`, wantIn: []string{"Invalid enum value", "markdown"}},
		{name: "both wrong", body: `{"description_html":"","variant":"markdown"}`, wantIn: []string{"HTML content cannot be empty", "Invalid enum value"}},
		{name: "not json at all", body: `not json`, wantIn: []string{"Validation error"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := convertRequestFor(t, server, testCase.body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", response.Code, response.Body.String())
			}
			body := response.Body.String()
			if !strings.Contains(body, "Validation error") {
				t.Errorf("body = %s", body)
			}
			for _, wanted := range testCase.wantIn {
				if !strings.Contains(body, wanted) {
					t.Errorf("body does not say %q: %s", wanted, body)
				}
			}
		})
	}
}

// TestConvertAnswersUnderBothSpellings covers the trailing slash, which the API's own caller writes with one.
func TestConvertAnswersUnderBothSpellings(t *testing.T) {
	server := convertServer(t)
	for _, path := range []string{"/live/convert-document", "/live/convert-document/"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"description_html":"<p>x</p>","variant":"rich"}`))
		request.Header.Set("Content-Type", "application/json")
		server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Errorf("%s: status = %d", path, recorder.Code)
		}
	}
}

// sameDocumentAs reads two updates and insists they hold the same document.
func sameDocumentAs(t *testing.T, mine, theirs string, variant ydoc.Variant) {
	t.Helper()
	ours := decodeBase64(t, mine)
	editors := decodeBase64(t, theirs)

	oursDocument, err := ydoc.ParseWithSchema(ours, variant.Schema)
	if err != nil {
		t.Fatalf("read ours: %v", err)
	}
	editorsDocument, err := ydoc.ParseWithSchema(editors, variant.Schema)
	if err != nil {
		t.Fatalf("read the editor's: %v", err)
	}
	oursJSON, err := oursDocument.JSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	editorsJSON, err := editorsDocument.JSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !reflect.DeepEqual(decodeAny(t, oursJSON), decodeAny(t, editorsJSON)) {
		t.Errorf("the two updates hold different documents\n go: %s\nwant: %s", oursJSON, editorsJSON)
	}
}

func decodeBase64(t *testing.T, encoded string) []byte {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return decoded
}

func decodeAny(t *testing.T, raw []byte) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return value
}
