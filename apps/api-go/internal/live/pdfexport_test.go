package live

import (
	"bytes"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yldm-tech/pace/apps/api-go/internal/ydoc"
)

// pdfAPI stands in for the API a page and its pictures are read through.
type pdfAPI struct {
	binary   []byte
	mentions string
	assetHit atomic.Int64
	// storage is where the asset route redirects to, which is where the picture actually is.
	storage *httptest.Server
}

func newPDFAPI(t *testing.T, html string) *pdfAPI {
	t.Helper()
	document, err := ydoc.ParseHTML(html)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	title := ydoc.TitleDocument("The page's own title")
	update, err := ydoc.ToUpdate(document, &title)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	return &pdfAPI{binary: update, mentions: `[{"id":"u1","display_name":"Someone"}]`}
}

func (a *pdfAPI) serve(t *testing.T) *Server {
	t.Helper()
	a.storage = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(onePixelPNG(t))
	}))
	t.Cleanup(a.storage.Close)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/assets/"):
			a.assetHit.Add(1)
			w.Header().Set("Location", a.storage.URL+"/picture.png")
			w.WriteHeader(http.StatusFound)
		case strings.HasSuffix(r.URL.Path, "/mentions/"):
			_, _ = w.Write([]byte(a.mentions))
		case strings.HasSuffix(r.URL.Path, "/description/"):
			_, _ = w.Write(a.binary)
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(api.Close)

	return NewServer(Config{APIBaseURL: api.URL, BasePath: "/live"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func exportRequest(t *testing.T, server *Server, body string, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/live/pdf-export/", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		request.Header.Set("Cookie", cookie)
	}
	server.Handler().ServeHTTP(recorder, request)
	return recorder
}

// TestExportDrawsThePage is the route end to end: the page is read, the people it mentions are looked up, the pictures are fetched, and a PDF comes back.
func TestExportDrawsThePage(t *testing.T) {
	api := newPDFAPI(t, `<h1>A heading</h1><p>Some <strong>bold</strong> text.</p><p><mention-component entity_identifier="u1" entity_name="user_mention"></mention-component></p><image-component src="an-asset"></image-component>`)
	server := api.serve(t)

	response := exportRequest(t, server, `{"pageId":"a-page","workspaceSlug":"a-workspace","projectId":"a-project","title":"A title","fileName":"export"}`, "session=abc")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/pdf" {
		t.Errorf("content type = %q", got)
	}
	if got := response.Header().Get("Content-Disposition"); !strings.Contains(got, `filename="export.pdf"`) {
		t.Errorf("disposition = %q", got)
	}
	if !bytes.HasPrefix(response.Body.Bytes(), []byte("%PDF-")) {
		t.Fatal("the answer is not a PDF")
	}
	if got := response.Header().Get("Content-Length"); got == "" || got == "0" {
		t.Errorf("content length = %q", got)
	}
	if api.assetHit.Load() == 0 {
		t.Error("the picture was never fetched")
	}
}

// TestExportWithoutAssetsFetchesNothing covers the option: no picture is drawn and none is asked for.
func TestExportWithoutAssetsFetchesNothing(t *testing.T) {
	api := newPDFAPI(t, `<image-component src="an-asset"></image-component>`)
	server := api.serve(t)

	response := exportRequest(t, server, `{"pageId":"a-page","workspaceSlug":"a-workspace","projectId":"a-project","noAssets":true}`, "session=abc")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if api.assetHit.Load() != 0 {
		t.Error("a picture was fetched for an export that asked for none")
	}
}

// TestExportFallsBackToThePagesOwnTitle covers the name a download gets when the request gave none.
func TestExportFallsBackToThePagesOwnTitle(t *testing.T) {
	api := newPDFAPI(t, "<p>a page</p>")
	server := api.serve(t)

	response := exportRequest(t, server, `{"pageId":"a-page","workspaceSlug":"a-workspace","projectId":"a-project"}`, "session=abc")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Disposition"); !strings.Contains(got, "page.pdf") {
		t.Errorf("disposition = %q, want the fallback name", got)
	}
}

// TestExportRefusesWithoutACookie covers the one thing the route checks for itself.
func TestExportRefusesWithoutACookie(t *testing.T) {
	api := newPDFAPI(t, "<p>a page</p>")
	server := api.serve(t)

	response := exportRequest(t, server, `{"pageId":"a-page","workspaceSlug":"a-workspace"}`, "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	if !strings.Contains(response.Body.String(), "Authentication required") {
		t.Errorf("body = %s", response.Body.String())
	}
}

// TestExportRefusesBadRequests pins the complaints and the statuses, because a caller reads both.
func TestExportRefusesBadRequests(t *testing.T) {
	api := newPDFAPI(t, "<p>a page</p>")
	server := api.serve(t)

	for _, testCase := range []struct {
		name string
		body string
		want int
	}{
		{name: "not json", body: "not json", want: http.StatusBadRequest},
		{name: "no page", body: `{"workspaceSlug":"a-workspace"}`, want: http.StatusBadRequest},
		{name: "no workspace", body: `{"pageId":"a-page"}`, want: http.StatusBadRequest},
		{name: "blank page", body: `{"pageId":"  ","workspaceSlug":"a-workspace"}`, want: http.StatusBadRequest},
		{name: "unknown page size", body: `{"pageId":"a-page","workspaceSlug":"a-workspace","pageSize":"A0"}`, want: http.StatusBadRequest},
		{name: "unknown orientation", body: `{"pageId":"a-page","workspaceSlug":"a-workspace","pageOrientation":"sideways"}`, want: http.StatusBadRequest},
		{name: "unknown page size in the other case", body: `{"pageId":"a-page","workspaceSlug":"a-workspace","pageSize":"a4"}`, want: http.StatusBadRequest},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := exportRequest(t, server, testCase.body, "session=abc")
			if response.Code != testCase.want {
				t.Errorf("status = %d, want %d: %s", response.Code, testCase.want, response.Body.String())
			}
		})
	}
}

// TestExportOfAPageWithNothingInItIsNotFound covers the page that has no document to draw.
func TestExportOfAPageWithNothingInItIsNotFound(t *testing.T) {
	api := newPDFAPI(t, "<p>a page</p>")
	api.binary = nil
	server := api.serve(t)

	response := exportRequest(t, server, `{"pageId":"a-page","workspaceSlug":"a-workspace","projectId":"a-project"}`, "session=abc")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", response.Code, response.Body.String())
	}
}

// TestExportSurvivesMentionsItCannotRead covers the lookup that is allowed to fail: the export goes ahead with the mentions drawn as unknown.
func TestExportSurvivesMentionsItCannotRead(t *testing.T) {
	api := newPDFAPI(t, `<p><mention-component entity_identifier="u1" entity_name="user_mention"></mention-component></p>`)
	api.mentions = "not json at all"
	server := api.serve(t)

	response := exportRequest(t, server, `{"pageId":"a-page","workspaceSlug":"a-workspace","projectId":"a-project"}`, "session=abc")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want the export to go ahead: %s", response.Code, response.Body.String())
	}
}

// TestExportWithoutAProjectFailsRatherThanRefusing pins a shape that looks wrong and is upstream's: the request schema says the project is optional, and the page service it reaches throws without one. The throw is a defect rather than a failure, so the answer is the five hundred any unexpected failure gets rather than the four hundred a missing field would.
func TestExportWithoutAProjectFailsRatherThanRefusing(t *testing.T) {
	api := newPDFAPI(t, "<p>a page</p>")
	server := api.serve(t)

	response := exportRequest(t, server, `{"pageId":"a-page","workspaceSlug":"a-workspace"}`, "session=abc")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "Failed to generate PDF") {
		t.Errorf("body = %s", response.Body.String())
	}
}

func TestOutputFileName(t *testing.T) {
	for _, testCase := range []struct {
		request pdfExportRequest
		want    string
	}{
		{request: pdfExportRequest{FileName: "report"}, want: "report.pdf"},
		{request: pdfExportRequest{FileName: "report.pdf"}, want: "report.pdf"},
		{request: pdfExportRequest{FileName: "report.PDF"}, want: "report.PDF"},
		{request: pdfExportRequest{Title: "A title"}, want: "A title.pdf"},
		{request: pdfExportRequest{FileName: "  ", Title: "A title"}, want: "A title.pdf"},
		{request: pdfExportRequest{}, want: "page.pdf"},
	} {
		if got := outputFileName(testCase.request); got != testCase.want {
			t.Errorf("outputFileName(%+v) = %q, want %q", testCase.request, got, testCase.want)
		}
	}
}

// TestSanitiseFileName covers the header injection the name would otherwise allow.
func TestSanitiseFileName(t *testing.T) {
	for name, want := range map[string]string{
		`plain.pdf`:             "plain.pdf",
		"with\"quote.pdf":       "withquote.pdf",
		"with\\slash.pdf":       "withslash.pdf",
		"with\r\nHeader: x.pdf": "withHeader: x.pdf",
		"Ünicode ☃.pdf":         "_nicode _.pdf",
	} {
		if got := sanitiseFileName(name); got != want {
			t.Errorf("sanitiseFileName(%q) = %q, want %q", name, got, want)
		}
	}
}

func onePixelPNG(t *testing.T) []byte {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return decoded
}
