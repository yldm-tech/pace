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

// What the installation refuses to dial. An operator with the console can do a great deal already, but the address they type is still one this process connects to.
func TestCheckBaseURLRefusesWhatItShould(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		target   string
		rejected bool
	}{
		{"a public https endpoint", "https://api.everyapi.ai/v1", false},
		{"loop-back by name", "http://localhost:8000/v1", true},
		{"loop-back by address", "http://127.0.0.1/v1", true},
		{"a private range", "http://10.0.0.5/v1", true},
		{"link-local metadata", "http://169.254.169.254/v1", true},
		{"a scheme that is not http", "file:///etc/passwd", true},
		{"not a url at all", "not a url", true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := CheckBaseURL(testCase.target, nil, nil)
			if testCase.rejected && err == nil {
				t.Errorf("%q was allowed", testCase.target)
			}
			if !testCase.rejected && err != nil {
				t.Errorf("%q was refused: %v", testCase.target, err)
			}
		})
	}
}

// An allowlisted host reaches a private address, which is what a deployment naming its own gateway needs.
func TestAnAllowedHostSkipsThePrivateCheck(t *testing.T) {
	if err := CheckBaseURL("http://gateway.internal/v1", nil, []string{"gateway.internal"}); err != nil {
		t.Errorf("an allowlisted host was refused: %v", err)
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

			models, err := ListModels(t.Context(), server.Client(), server.URL+"/v1", "k")
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

	_, err := ListModels(t.Context(), server.Client(), server.URL+"/v1", "wrong")
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
