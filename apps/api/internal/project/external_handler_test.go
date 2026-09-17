package project

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// The three providers with their models, and the rules that decide whether the assistant may be called at all.
func TestTheLanguageModelConfiguration(t *testing.T) {
	if len(llmProviders) != 3 {
		t.Fatalf("there are %d providers", len(llmProviders))
	}
	for name, provider := range llmProviders {
		if provider.Name == "" || provider.DefaultModel == "" || len(provider.Models) == 0 {
			t.Errorf("%s is missing its name, default or models", name)
		}
		listed := false
		for _, model := range provider.Models {
			if model == provider.DefaultModel {
				listed = true
			}
		}
		if !listed {
			t.Errorf("%s defaults to %s, which it does not list", name, provider.DefaultModel)
		}
	}
	if llmProviders["openai"].DefaultModel != "gpt-4o-mini" {
		t.Errorf("OpenAI defaults to %s", llmProviders["openai"].DefaultModel)
	}
}

// An unsplash search with no key configured answers an empty list rather than an error, which is what lets the cover picker degrade quietly.
func TestAnUnsplashSearchWithoutAKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &Handler{settings: Settings{SkipEnvironmentConfig: false, Environment: map[string]string{}}}
	router := gin.New()
	router.GET("/api/unsplash/", func(c *gin.Context) { handler.unsplashSearch(c, nil) })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/unsplash/", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("the status is %d", recorder.Code)
	}
	if strings.TrimSpace(recorder.Body.String()) != "[]" {
		t.Errorf("the body is %q rather than an empty list", recorder.Body.String())
	}
}

// Whatever Unsplash answers is passed back with its own status, and the search url carries the stray dollar sign the template literal left behind.
func TestAnUnsplashSearchPassesTheAnswerBack(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var asked string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.String()
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(`{"errors":["no"]}`))
	}))
	defer upstream.Close()

	handler := &Handler{
		settings:   Settings{Environment: map[string]string{"UNSPLASH_ACCESS_KEY": "a-key"}},
		httpClient: upstream.Client(),
	}
	// The endpoint is fixed upstream, so the test reaches it through a transport that redirects to the stub.
	handler.httpClient = &http.Client{Transport: redirectTo(upstream.URL)}

	router := gin.New()
	router.GET("/api/unsplash/", func(c *gin.Context) { handler.unsplashSearch(c, nil) })
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/unsplash/?query=mountains&page=3", nil))

	if recorder.Code != http.StatusTeapot {
		t.Errorf("the status is %d rather than the one Unsplash gave", recorder.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("parsing the response: %v", err)
	}
	if _, present := payload["errors"]; !present {
		t.Errorf("the body is %q rather than the one Unsplash gave", recorder.Body.String())
	}
	// The dollar sign is upstream's and goes out on the wire, so page 3 is asked for as "$3".
	if !strings.Contains(asked, "page=$3") {
		t.Errorf("the search asked for %q, and the stray dollar sign is part of it", asked)
	}
	if !strings.Contains(asked, "query=mountains") {
		t.Errorf("the search asked for %q", asked)
	}
}

// redirectTo sends every request to one host, so a test can answer for an endpoint that is a literal in the code.
type redirectTo string

func (target redirectTo) RoundTrip(request *http.Request) (*http.Response, error) {
	replacement := request.Clone(request.Context())
	parsed, err := replacement.URL.Parse(string(target))
	if err != nil {
		return nil, err
	}
	replacement.URL.Scheme = parsed.Scheme
	replacement.URL.Host = parsed.Host
	return http.DefaultTransport.RoundTrip(replacement)
}
