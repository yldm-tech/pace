package instances

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"github.com/yldm-tech/pace/apps/api/internal/httpsafe"
	"github.com/yldm-tech/pace/apps/api/internal/llm"
)

// llmModels answers what the configured endpoint says it serves, so the console can offer a list instead of asking an operator to type a model name from memory.
//
// It is a POST rather than a GET because the request may carry an API key. A key in a query string is a key in the access log, in the referrer and in the browser's history.
//
// The base URL and the key are taken from the request when it carries them, because the console asks for this while the form is being filled in and before anything is saved. Falling back to what is stored is what makes the button work on a page that was merely opened.
func (handler *Handler) llmModels(c *gin.Context, _ *auth.User, _ *Instance) {
	var payload struct {
		BaseURL  string `json:"base_url"`
		APIKey   string `json:"api_key"`
		Provider string `json:"provider"`
	}
	_ = c.ShouldBindJSON(&payload)

	stored := handler.configurationValues(c.Request.Context(), []struct{ Key, Fallback string }{
		{"LLM_PROVIDER", "openai"}, {"LLM_BASE_URL", ""}, {"LLM_API_KEY", ""},
	})
	provider := firstNonEmpty(payload.Provider, stored["LLM_PROVIDER"], "openai")
	baseURL := llm.ResolveBaseURL(firstNonEmpty(payload.BaseURL, stored["LLM_BASE_URL"]), "", provider)
	key := firstNonEmpty(payload.APIKey, stored["LLM_API_KEY"])
	if key == "" {
		drf.Respond(c, http.StatusBadRequest, gin.H{"error": "An API key is needed to ask the endpoint what it serves."})
		return
	}

	// No separate check before this call: ListModels dials through the pinned client, which resolves the host, validates every address, connects to the validated literal and refuses redirects. Checking here as well would suggest the guarantee lives at the call site, and it does not.
	models, err := llm.ListModels(c.Request.Context(), baseURL, key,
		handler.settings.LLMAllowedIPs, handler.settings.LLMAllowedHosts)
	if err != nil {
		var rejected httpsafe.RejectedError
		if errors.As(err, &rejected) {
			drf.Respond(c, http.StatusBadRequest, gin.H{"error": "That endpoint is not one this installation may call: " + rejected.Reason})
			return
		}
		// The endpoint's own status is worth passing on. A 401 is a key that is wrong and a 404 is usually a base URL missing its version segment, and an operator can act on either -- which is more than "something went wrong" offers.
		var status llm.StatusError
		if errors.As(err, &status) {
			drf.Respond(c, http.StatusBadGateway, gin.H{
				"error":  "The endpoint refused the request.",
				"status": int(status),
			})
			return
		}
		drf.Respond(c, http.StatusBadGateway, gin.H{"error": "The endpoint could not be reached."})
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"models": models, "base_url": baseURL})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
