// Package llm is what the assistant and the admin console agree on about language models: which providers are known, where completions are asked for, and what a provider says it serves.
//
// It exists because two packages need the same answers. internal/project asks a model for text; internal/instances lists the models so the console can offer them. Keeping the provider table in one of those and reaching into it from the other would put the knowledge in the wrong place and leave the two able to disagree about what a provider's endpoint is.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/yldm-tech/pace/apps/api/internal/httpsafe"
)

// Provider is what naming one buys: a default model and the endpoint it answers on, so neither has to be typed.
type Provider struct {
	Name           string
	DefaultModel   string
	DefaultBaseURL string
}

// Providers are the ones with a default. It is not an allowlist -- a provider absent from it is accepted and simply has nothing to infer from, so its model and base URL have to be named.
//
// Entries used to carry the full set of models each would accept, and a model outside that set was refused. Such a list could only go stale, and it had: an installation on OpenAI's own API could not select a model released after the list was written. The provider's own API is the authority on what it serves, which is what ListModels asks it.
var Providers = map[string]Provider{
	"openai":    {"OpenAI", "gpt-4o-mini", "https://api.openai.com/v1"},
	"anthropic": {"Anthropic", "claude-3-5-sonnet-20240620", "https://api.anthropic.com/v1"},
	"gemini":    {"Gemini", "gemini-1.5-pro-latest", "https://generativelanguage.googleapis.com/v1beta/openai"},
	"everyapi":  {"EveryAPI", "claude-sonnet-5", "https://api.everyapi.ai/v1"},
}

// DefaultBaseURL is where completions go when nothing else says otherwise.
const DefaultBaseURL = "https://api.openai.com/v1"

// ResolveBaseURL picks the endpoint in the order a deployment can override it: what an operator configured, then what the process was started with, then the named provider's own, then OpenAI.
//
// The provider's default sits below both explicit settings so that naming a provider stays a convenience and never overrides an address somebody wrote down.
func ResolveBaseURL(configured, fromProcess, provider string) string {
	if trimmed := strings.TrimSpace(configured); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(fromProcess); trimmed != "" {
		return trimmed
	}
	if definition, known := Providers[strings.ToLower(strings.TrimSpace(provider))]; known && definition.DefaultBaseURL != "" {
		return definition.DefaultBaseURL
	}
	return DefaultBaseURL
}

// ErrRejected is a base URL the installation will not call.
var ErrRejected = errors.New("the endpoint is not one this installation may call")

// CheckBaseURL decides whether a base URL may be called at all. An operator who can reach the admin console can already do a great deal, but the address they type is still an address this process connects to, so the same guard the webhooks use applies here: http or https only, no loop-back, and nothing resolving into a private range unless the deployment has said otherwise.
func CheckBaseURL(target string, allowedIPs []netip.Prefix, allowedHosts []string) error {
	parsed, err := url.Parse(strings.TrimSpace(target))
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("%w: it is not a url", ErrRejected)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%w: only http and https are allowed", ErrRejected)
	}
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
		return fmt.Errorf("%w: it is a loop-back address", ErrRejected)
	}
	if httpsafe.HostAllowed(hostname, allowedHosts) {
		return nil
	}
	if _, err := httpsafe.ResolveAndValidate(parsed.Hostname(), allowedIPs, true); err != nil {
		return fmt.Errorf("%w: it resolves somewhere this installation may not reach", ErrRejected)
	}
	return nil
}

// ListModels asks an endpoint what it serves, which is GET {base}/models on anything OpenAI-shaped.
//
// The ids come back in whatever order the provider gives them, because that order is usually meaningful -- providers tend to list their current models first -- and sorting would discard it.
func ListModels(ctx context.Context, client *http.Client, baseURL, key string) ([]string, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/models"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 400 {
		return nil, StatusError(response.StatusCode)
	}
	// The OpenAI shape is {"data": [{"id": "..."}]}. A few gateways answer a bare array instead, and both are cheap to accept.
	var enveloped struct {
		Data []entry `json:"data"`
	}
	if err := json.Unmarshal(payload, &enveloped); err == nil && len(enveloped.Data) > 0 {
		return identifiers(enveloped.Data), nil
	}
	var bare []entry
	if err := json.Unmarshal(payload, &bare); err == nil && len(bare) > 0 {
		return identifiers(bare), nil
	}
	return nil, errors.New("the endpoint did not answer with a list of models")
}

func identifiers(rows []entry) []string {
	models := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.ID != "" {
			models = append(models, row.ID)
		}
	}
	return models
}

// entry is one row of a models listing, in either shape the endpoint may answer with.
type entry struct {
	ID string `json:"id"`
}

// StatusError is what the endpoint answered when it refused. The status is kept because the console can say something useful with it -- 401 is a key that is wrong, 404 is usually a base URL with the version segment missing.
type StatusError int

func (err StatusError) Error() string {
	return fmt.Sprintf("the endpoint answered %d", int(err))
}
