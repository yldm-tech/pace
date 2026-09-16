package project

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

// The three routes that reach outside the installation: two that ask a language model for some text, and one that searches somebody else's photo library.
func (handler *Handler) registerExternalRoutes(router gin.IRouter) {
	router.GET("/api/unsplash/", handler.authenticated(handler.unsplashSearch))
	router.POST("/api/workspaces/:slug/ai-assistant/", handler.authenticated(handler.workspaceAssistant))
	router.POST("/api/workspaces/:slug/projects/:id/ai-assistant/", handler.authenticated(handler.projectAssistant))
}

// unsplashSearch passes a search on to Unsplash and hands back whatever it says.
//
// Without a key configured it answers an empty list rather than an error, which is what lets the cover picker degrade quietly. Whatever Unsplash answers is passed back with its own status, body and all — including an error body, so a rejected key reaches the browser as Unsplash worded it.
func (handler *Handler) unsplashSearch(c *gin.Context, _ *auth.User) {
	key := handler.configurationValue(c, "UNSPLASH_ACCESS_KEY", "")
	if key == "" {
		drf.Respond(c, http.StatusOK, []any{})
		return
	}
	query := c.Query("query")
	page := c.DefaultQuery("page", "1")
	perPage := c.DefaultQuery("per_page", "20")

	var endpoint string
	if query != "" {
		// The dollar sign in front of the page is upstream's, left over from a template literal, and it goes out on the wire — so a search always asks Unsplash for page "$1" whatever page was asked for.
		endpoint = "https://api.unsplash.com/search/photos/?client_id=" + url.QueryEscape(key) +
			"&query=" + query + "&page=$" + page + "&per_page=" + perPage
	} else {
		endpoint = "https://api.unsplash.com/photos/?client_id=" + url.QueryEscape(key) +
			"&page=" + page + "&per_page=" + perPage
	}

	request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := handler.externalClient().Do(request)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		// requests raises here and the base view answers 500, which is what an answer that is not json really does.
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, response.StatusCode, decoded)
}

// llmProviders are the three the installation may be pointed at, with the models each takes and the one it falls back to.
var llmProviders = map[string]struct {
	Name         string
	Models       []string
	DefaultModel string
}{
	"openai": {"OpenAI",
		[]string{"gpt-3.5-turbo", "gpt-4o-mini", "gpt-4o", "o1-mini", "o1-preview"}, "gpt-4o-mini"},
	"anthropic": {"Anthropic",
		[]string{"claude-3-5-sonnet-20240620", "claude-3-haiku-20240307", "claude-3-opus-20240229",
			"claude-3-sonnet-20240229", "claude-2.1", "claude-2", "claude-instant-1.2", "claude-instant-1"},
		"claude-3-sonnet-20240229"},
	"gemini": {"Gemini",
		[]string{"gemini-pro", "gemini-1.5-pro-latest", "gemini-pro-vision"}, "gemini-pro"},
}

// llmConfig is get_llm_config: the key, the model and the provider, or nothing at all when any of the three does not check out.
//
// A provider nobody recognises, a missing key, or a model the provider does not list all give back nothing — and the caller reports the same sentence for all three, so an operator cannot tell which it was from the response.
func (handler *Handler) llmConfig(c *gin.Context) (key, model, provider string, ok bool) {
	key = handler.configurationValue(c, "LLM_API_KEY", "")
	provider = handler.configurationValue(c, "LLM_PROVIDER", "openai")
	model = handler.configurationValue(c, "LLM_MODEL", "")

	definition, known := llmProviders[strings.ToLower(provider)]
	if !known || key == "" {
		return "", "", "", false
	}
	if model == "" {
		model = definition.DefaultModel
	}
	for _, supported := range definition.Models {
		if supported == model {
			return key, model, provider, true
		}
	}
	return "", "", "", false
}

// workspaceAssistant asks the configured model for some text, with nothing about the workspace in the answer.
func (handler *Handler) workspaceAssistant(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember) {
		return
	}
	text, refused := handler.assistantText(c)
	if refused {
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{
		"response": text, "response_html": strings.ReplaceAll(text, "\n", "<br/>"),
	})
}

// projectAssistant is the same ask with the project and the workspace named beside the answer.
func (handler *Handler) projectAssistant(c *gin.Context, user *auth.User) {
	projectID := c.Param("id")
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	text, refused := handler.assistantText(c)
	if refused {
		return
	}
	project, err := handler.projectLite(c.Request.Context(), projectID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	workspace, err := handler.workspaceLiteBySlug(c, c.Param("slug"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{
		"response": text, "response_html": strings.ReplaceAll(text, "\n", "<br/>"),
		"project_detail": project, "workspace_detail": workspace,
	})
}

// assistantText is the half both routes share: check the configuration, check the task, ask the model. It reports whether it has already answered.
func (handler *Handler) assistantText(c *gin.Context) (string, bool) {
	key, model, provider, ok := handler.llmConfig(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "LLM provider API key and model are required"})
		return "", true
	}
	payload := map[string]any{}
	_ = c.ShouldBindJSON(&payload)
	task, _ := payload["task"].(string)
	if task == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Task is required"})
		return "", true
	}
	prompt, _ := payload["prompt"].(string)

	text, err := handler.askModel(c, key, model, provider, task+"\n"+prompt)
	if err != nil {
		// Every failure answers the same sentence and a 500, so nothing about the provider reaches the caller.
		c.JSON(http.StatusInternalServerError, gin.H{"error": "An internal error has occurred."})
		return "", true
	}
	return text, false
}

// askModel is get_llm_response: one chat completion against an OpenAI-shaped endpoint.
//
// All three providers are called the same way, because upstream points the OpenAI client at whichever one is configured. Gemini's model name is prefixed with the provider, which is the one thing that differs.
func (handler *Handler) askModel(c *gin.Context, key, model, provider, content string) (string, error) {
	if strings.EqualFold(provider, "gemini") {
		model = "gemini/" + model
	}
	body, err := json.Marshal(map[string]any{
		"model":    model,
		"messages": []any{map[string]any{"role": "user", "content": content}},
	})
	if err != nil {
		return "", err
	}
	endpoint := handler.settings.LLMBaseURL
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1"
	}
	request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost,
		strings.TrimRight(endpoint, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+key)
	response, err := handler.externalClient().Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode >= 400 {
		return "", errStatus(response.StatusCode)
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(payload, &completion); err != nil {
		return "", err
	}
	if len(completion.Choices) == 0 {
		// The python client reads choices[0] without checking, so an answer with none raises.
		return "", errStatus(response.StatusCode)
	}
	return completion.Choices[0].Message.Content, nil
}

type errStatus int

func (err errStatus) Error() string { return "the language model answered " + itoa(int(err)) }

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// externalClient is the client both of these reach out with. requests has no timeout of its own, which is what would otherwise hold a worker open on a provider that never answers; thirty seconds is used here instead.
func (handler *Handler) externalClient() *http.Client {
	if handler.httpClient != nil {
		return handler.httpClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// workspaceLiteBySlug is WorkspaceLiteSerializer over the workspace a slug names.
func (handler *Handler) workspaceLiteBySlug(c *gin.Context, slug string) (gin.H, error) {
	var ids []string
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ? AND deleted_at IS NULL", slug).Limit(1).Pluck("id", &ids).Error
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return handler.workspaceLite(c.Request.Context(), ids[0])
}
