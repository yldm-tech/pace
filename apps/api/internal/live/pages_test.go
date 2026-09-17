package live

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func projectPageContext() ConnectionContext {
	return ConnectionContext{
		UserID:        "a-user",
		Cookie:        "session=abc",
		DocumentType:  ProjectPage,
		WorkspaceSlug: "a-workspace",
		ProjectID:     "a-project",
	}
}

func TestNewPageServiceRefusesAnIncompleteContext(t *testing.T) {
	client := NewAPIClient("http://api.test")
	for _, testCase := range []struct {
		name    string
		context ConnectionContext
		wantIn  string
	}{
		{name: "an unknown document type", context: ConnectionContext{DocumentType: "user_page", WorkspaceSlug: "w", ProjectID: "p", Cookie: "c"}, wantIn: "invalid document type"},
		{name: "no workspace", context: ConnectionContext{DocumentType: ProjectPage, ProjectID: "p", Cookie: "c"}, wantIn: "missing required fields"},
		{name: "no project", context: ConnectionContext{DocumentType: ProjectPage, WorkspaceSlug: "w", Cookie: "c"}, wantIn: "missing required fields"},
		{name: "no cookie", context: ConnectionContext{DocumentType: ProjectPage, WorkspaceSlug: "w", ProjectID: "p"}, wantIn: "cookie is required"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := NewPageService(client, testCase.context)
			if err == nil || !strings.Contains(err.Error(), testCase.wantIn) {
				t.Errorf("err = %v, want one saying %q", err, testCase.wantIn)
			}
		})
	}
}

func TestPageServiceReadsAndWrites(t *testing.T) {
	var lastMethod, lastPath, lastCookie, lastQuery string
	var lastBody []byte
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastMethod, lastPath, lastCookie, lastQuery = r.Method, r.URL.Path, r.Header.Get("Cookie"), r.URL.RawQuery
		lastBody, _ = io.ReadAll(r.Body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/description/") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte{1, 2, 3})
		case strings.HasSuffix(r.URL.Path, "/mentions/"):
			_, _ = w.Write([]byte(`[{"id":"u1","display_name":"Someone"}]`))
		case strings.HasSuffix(r.URL.Path, "/pages/a-page/") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"a-page","name":"A page","description_html":"<p>hi</p>"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer api.Close()

	service, err := NewPageService(NewAPIClient(api.URL), projectPageContext())
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	ctx := context.Background()

	page, err := service.FetchDetails(ctx, "a-page")
	if err != nil {
		t.Fatalf("details: %v", err)
	}
	if page.Name != "A page" || page.DescriptionHTML != "<p>hi</p>" {
		t.Errorf("page = %+v", page)
	}
	if lastPath != "/api/workspaces/a-workspace/projects/a-project/pages/a-page/" {
		t.Errorf("path = %s", lastPath)
	}
	if lastCookie != "session=abc" {
		t.Errorf("cookie = %q", lastCookie)
	}

	binary, err := service.FetchDescriptionBinary(ctx, "a-page")
	if err != nil {
		t.Fatalf("binary: %v", err)
	}
	if len(binary) != 3 {
		t.Errorf("binary = %v", binary)
	}

	payload := DocumentPayload{DescriptionBinary: "AQID", DescriptionHTML: "<p>hi</p>", DescriptionJSON: json.RawMessage(`{"type":"doc"}`)}
	if err := service.UpdateDescriptionBinary(ctx, "a-page", payload); err != nil {
		t.Fatalf("update: %v", err)
	}
	if lastMethod != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", lastMethod)
	}
	var written map[string]any
	if err := json.Unmarshal(lastBody, &written); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if written["description_binary"] != "AQID" || written["description_html"] != "<p>hi</p>" {
		t.Errorf("body = %v", written)
	}

	if err := service.UpdatePageProperties(ctx, "a-page", map[string]any{"name": "Renamed"}); err != nil {
		t.Fatalf("properties: %v", err)
	}
	if !strings.Contains(string(lastBody), "Renamed") {
		t.Errorf("body = %s", lastBody)
	}

	mentions, err := service.FetchUserMentions(ctx, "a-page")
	if err != nil {
		t.Fatalf("mentions: %v", err)
	}
	if len(mentions) != 1 || mentions[0].DisplayName != "Someone" {
		t.Errorf("mentions = %+v", mentions)
	}
	if lastQuery != "mention_type=user_mention" {
		t.Errorf("query = %q", lastQuery)
	}
}

// TestFetchDescriptionBinaryAllowsAnEmptyBody is the case a page that has never been opened in the collaborative editor is in: the column is empty, and the document is built from the HTML instead.
func TestFetchDescriptionBinaryAllowsAnEmptyBody(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer api.Close()

	service, err := NewPageService(NewAPIClient(api.URL), projectPageContext())
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	binary, err := service.FetchDescriptionBinary(context.Background(), "a-page")
	if err != nil {
		t.Fatalf("binary: %v", err)
	}
	if len(binary) != 0 {
		t.Errorf("binary = %v, want it empty", binary)
	}
}

// TestUpdateCarriesTheStatus matters because the status is what tells a page that is too large from any other failure, and the two are handled differently.
func TestUpdateCarriesTheStatus(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = w.Write([]byte(`{"detail":"too big"}`))
	}))
	defer api.Close()

	service, err := NewPageService(NewAPIClient(api.URL), projectPageContext())
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	err = service.UpdateDescriptionBinary(context.Background(), "a-page", DocumentPayload{})
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("err = %v, want a 413", err)
	}
	if apiError.Message != "too big" {
		t.Errorf("message = %q", apiError.Message)
	}
}

// TestRedirectsAreNotFollowed keeps the session cookie out of object storage: a route that answers with a redirect hands the location back rather than being followed with the cookie attached.
func TestRedirectsAreNotFollowed(t *testing.T) {
	var followed bool
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			followed = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Location", "/elsewhere")
		w.WriteHeader(http.StatusFound)
	}))
	defer api.Close()

	client := NewAPIClient(api.URL)
	response, _, err := client.request(context.Background(), http.MethodGet, "/api/assets/v2/something/", "session=abc", nil, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if followed {
		t.Error("the redirect was followed")
	}
	if response.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want 302", response.StatusCode)
	}
	if got := response.Header.Get("Location"); got != "/elsewhere" {
		t.Errorf("location = %q", got)
	}
}
