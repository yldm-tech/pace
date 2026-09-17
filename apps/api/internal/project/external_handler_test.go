package project

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// Each known provider still carries a name and a default to fall back on.
func TestTheKnownProvidersCarryADefault(t *testing.T) {
	for name, provider := range llmProviders {
		if provider.Name == "" || provider.DefaultModel == "" {
			t.Errorf("%s is missing its name or its default model", name)
		}
	}
	if llmProviders["openai"].DefaultModel != "gpt-4o-mini" {
		t.Errorf("OpenAI defaults to %s", llmProviders["openai"].DefaultModel)
	}
}

// What llmConfig accepts, and the two things it still refuses.
//
// The case that matters here is the third: a provider absent from llmProviders, which is what a gateway or a self-hosted endpoint is. It used to be refused outright, and so was any model outside a hardcoded per-provider list -- a list that could only go stale, and had, to the point where an installation on OpenAI's own API could not select a model released after it was written.
func TestTheLanguageModelConfiguration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name          string
		environment   map[string]string
		expectedModel string
		expectedOK    bool
	}{
		{"a known provider falls back to its default",
			map[string]string{"LLM_API_KEY": "k", "LLM_PROVIDER": "openai"}, "gpt-4o-mini", true},
		{"a model the old list never held is accepted",
			map[string]string{"LLM_API_KEY": "k", "LLM_PROVIDER": "openai", "LLM_MODEL": "gpt-5-turbo-2027"}, "gpt-5-turbo-2027", true},
		{"a provider nobody has heard of is accepted when the model is named",
			map[string]string{"LLM_API_KEY": "k", "LLM_PROVIDER": "everyapi", "LLM_MODEL": "claude-opus-5"}, "claude-opus-5", true},
		{"a provider with no default and no model is refused",
			map[string]string{"LLM_API_KEY": "k", "LLM_PROVIDER": "everyapi"}, "", false},
		{"no key is refused",
			map[string]string{"LLM_PROVIDER": "openai", "LLM_MODEL": "gpt-4o"}, "", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			handler := &Handler{settings: Settings{SkipEnvironmentConfig: false, Environment: testCase.environment}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			_, model, _, ok := handler.llmConfig(c)
			if ok != testCase.expectedOK {
				t.Fatalf("configured=%v, wanted %v", ok, testCase.expectedOK)
			}
			if model != testCase.expectedModel {
				t.Errorf("model %q, wanted %q", model, testCase.expectedModel)
			}
		})
	}
}

// The base URL comes from the instance configuration first, so an operator can point the assistant somewhere else without a redeploy, then from the process environment, then OpenAI.
func TestWhereCompletionsAreAskedFor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name        string
		environment map[string]string
		fromProcess string
		expected    string
	}{
		{"configuration wins", map[string]string{"LLM_BASE_URL": "https://api.everyapi.ai/v1"}, "https://other.example/v1", "https://api.everyapi.ai/v1"},
		{"then the process environment", map[string]string{}, "https://other.example/v1", "https://other.example/v1"},
		{"then OpenAI", map[string]string{}, "", "https://api.openai.com/v1"},
		{"blank configuration does not win", map[string]string{"LLM_BASE_URL": "   "}, "https://other.example/v1", "https://other.example/v1"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			handler := &Handler{settings: Settings{
				SkipEnvironmentConfig: false, Environment: testCase.environment, LLMBaseURL: testCase.fromProcess,
			}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			if got := handler.llmBaseURL(c); got != testCase.expected {
				t.Errorf("base url %q, wanted %q", got, testCase.expected)
			}
		})
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
