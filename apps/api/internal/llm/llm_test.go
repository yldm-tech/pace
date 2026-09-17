package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Every known provider carries what naming one is supposed to buy.
func TestTheKnownProvidersAreComplete(t *testing.T) {
	for name, provider := range Providers {
		if provider.Name == "" || provider.DefaultModel == "" || provider.DefaultBaseURL == "" {
			t.Errorf("%s is missing its name, default model or endpoint", name)
		}
	}
	if Providers["everyapi"].DefaultBaseURL != "https://api.everyapi.ai/v1" {
		t.Errorf("EveryAPI answers on %s", Providers["everyapi"].DefaultBaseURL)
	}
}

// The order the endpoint is resolved in. What matters is the third case: a named provider is a convenience and must never win over an address somebody wrote down.
func TestResolveBaseURL(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		configured string
		process    string
		provider   string
		expected   string
	}{
		{"configuration wins", "https://mine.example/v1", "https://other.example/v1", "everyapi", "https://mine.example/v1"},
		{"then the process environment", "", "https://other.example/v1", "everyapi", "https://other.example/v1"},
		{"then the provider's own", "", "", "everyapi", "https://api.everyapi.ai/v1"},
		{"then OpenAI", "", "", "nobody-has-heard-of-this", DefaultBaseURL},
		{"blank is not a value", "   ", "  ", "everyapi", "https://api.everyapi.ai/v1"},
		{"the provider name is not case sensitive", "", "", "EveryAPI", "https://api.everyapi.ai/v1"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := ResolveBaseURL(testCase.configured, testCase.process, testCase.provider); got != testCase.expected {
				t.Errorf("resolved %q, wanted %q", got, testCase.expected)
			}
		})
	}
}

// What the installation refuses to dial. These go through ListModels rather than a separate checker, because that is the only way in: the guarantee has to hold for the function callers actually reach for, not for one beside it that a caller might forget.
func TestListModelsRefusesWhatItShould(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		target string
	}{
		{"loop-back by name", "http://localhost:8000/v1"},
		{"loop-back by address", "http://127.0.0.1/v1"},
		{"a private range", "http://10.0.0.5/v1"},
		{"link-local metadata", "http://169.254.169.254/v1"},
		{"a scheme that is not http", "file:///etc/passwd"},
		{"not a url at all", "://nonsense"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := ListModels(t.Context(), testCase.target, "k", nil, nil); err == nil {
				t.Errorf("%q was dialled", testCase.target)
			}
		})
	}
}

// Both shapes an endpoint answers with, and the order it answered in is kept.
func TestListModelsReadsBothShapes(t *testing.T) {
	for _, testCase := range []struct {
		name string
		body any
	}{
		{"the OpenAI envelope", map[string]any{"data": []map[string]string{{"id": "b"}, {"id": "a"}}}},
		{"a bare array", []map[string]string{{"id": "b"}, {"id": "a"}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/models" {
					t.Errorf("asked for %s", r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer k" {
					t.Errorf("sent %q", r.Header.Get("Authorization"))
				}
				_ = json.NewEncoder(w).Encode(testCase.body)
			}))
			defer server.Close()

			// The test server is on loop-back, which the pinned client refuses by default. Naming it here is what an operator does for their own gateway, so this exercises the allowlist as well.
			models, err := ListModels(t.Context(), server.URL+"/v1", "k", nil, []string{"127.0.0.1"})
			if err != nil {
				t.Fatalf("listing failed: %v", err)
			}
			// Not sorted: providers tend to list their current models first, and that order is worth keeping.
			if len(models) != 2 || models[0] != "b" || models[1] != "a" {
				t.Errorf("got %v", models)
			}
		})
	}
}

// A refusal keeps its status, because 401 and 404 mean different things to whoever is filling in the form.
func TestListModelsKeepsTheStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := ListModels(t.Context(), server.URL+"/v1", "wrong", nil, []string{"127.0.0.1"})
	var status StatusError
	if !errorsAs(err, &status) || int(status) != http.StatusUnauthorized {
		t.Fatalf("error was %v", err)
	}
}

func errorsAs(err error, target *StatusError) bool {
	if status, ok := err.(StatusError); ok {
		*target = status
		return true
	}
	return false
}

// A redirect is not followed. This is the failure the first version of this had: it used a plain http.Client, which follows redirects, so an endpoint answering 302 to a metadata address would have been fetched with every check already passed.
func TestListModelsDoesNotFollowARedirect(t *testing.T) {
	var reached bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
			return
		}
		reached = true
	}))
	defer server.Close()

	_, err := ListModels(t.Context(), server.URL+"/v1", "k", nil, []string{"127.0.0.1"})
	if err == nil {
		t.Error("a redirecting endpoint was accepted")
	}
	if reached {
		t.Error("the redirect was followed")
	}
}
