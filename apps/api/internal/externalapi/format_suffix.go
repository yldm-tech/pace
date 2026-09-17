package externalapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
)

// The two prefixes DRF's routers are mounted at, and therefore the only paths that carry a format suffix.
//
// Everything else under /api/v1/ is a plain path() and takes no suffix at all, so `.../states.json` is a 404 while `.../stickies.json` is not. That asymmetry is DRF's: only a route registered through a router gets the suffix treatment.
var formatSuffixPrefixes = []string{"invitations", "stickies"}

// RegisterFormatSuffixes teaches the router DRF's format suffixes, which the two viewsets mounted through a DefaultRouter accept and nothing else does.
//
// A suffix cannot be a path parameter in this router — a parameter is a whole segment and a suffix is part of one — so the rewrite happens where a request has already failed to match. Stripping a suffix and dispatching again lands on the route that was always there, which is exactly what DRF does with a different mechanism.
func RegisterFormatSuffixes(engine *gin.Engine, notFound gin.HandlerFunc) {
	engine.NoRoute(func(c *gin.Context) {
		stripped, format, carried := stripFormatSuffix(c.Request.URL.Path)
		if !carried {
			notFound(c)
			return
		}
		if !formatSuffixCandidate(stripped) {
			notFound(c)
			return
		}
		if format != "json" {
			// Content negotiation refuses a format no renderer answers to, and DRF raises Http404 for it rather than a 406.
			c.JSON(http.StatusNotFound, gin.H{"detail": "Not found."})
			return
		}
		c.Request.URL.Path = stripped
		// A second pass cannot rewrite again, because the suffix is gone.
		engine.HandleContext(c)
	})
}

// stripFormatSuffix takes the ".json" off the last segment, whether or not the path ends in a slash.
//
// DRF's pattern is `\.(?P<format>[a-z0-9]+)/?$`, so the suffix is lowercase letters and digits and the trailing slash is optional.
func stripFormatSuffix(path string) (stripped, format string, carried bool) {
	trailing := strings.HasSuffix(path, "/")
	trimmed := strings.TrimSuffix(path, "/")
	dot := strings.LastIndex(trimmed, ".")
	if dot < 0 {
		return "", "", false
	}
	// The suffix has to be inside the last segment.
	if strings.Contains(trimmed[dot:], "/") {
		return "", "", false
	}
	format = trimmed[dot+1:]
	if format == "" {
		return "", "", false
	}
	for _, character := range format {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return "", "", false
		}
	}
	stripped = trimmed[:dot]
	// The route the suffix stands in for always ends in a slash here, whether or not the request did.
	_ = trailing
	return stripped + "/", format, true
}

// formatSuffixCandidate says whether the path is one of the two a router owns. Without this any path at all could have a suffix taken off it and be dispatched again, which is not what DRF does.
func formatSuffixCandidate(path string) bool {
	if !strings.HasPrefix(path, "/api/v1/workspaces/") {
		return false
	}
	segments := strings.Split(strings.Trim(path, "/"), "/")
	// /api/v1/workspaces/<slug>/<prefix>/ or /api/v1/workspaces/<slug>/<prefix>/<pk>/
	if len(segments) < 5 || len(segments) > 6 {
		return false
	}
	for _, prefix := range formatSuffixPrefixes {
		if segments[4] == prefix {
			return true
		}
	}
	return false
}

// registerAPIRoot is the DRF router's own root view.
//
// Two routers are mounted at the same prefix, so only the first one's root ever resolves — and the first is the invitations one, which is why the stickies are not listed beside it. The reply is one key and one absolute url.
//
// It is the one route under /api/v1/ that an api key cannot reach. DRF's own APIRootView takes the project's default authentication, which is the session, rather than the key authentication every view in this package declares for itself.
func (handler *Handler) registerAPIRoot(router gin.IRouter) {
	router.GET("/api/v1/workspaces/:slug/", func(c *gin.Context) {
		if handler.sessions == nil {
			c.JSON(http.StatusForbidden, gin.H{"detail": "Authentication credentials were not provided."})
			return
		}
		user, _, err := handler.sessions.Authenticate(c.Request.Context(), c.Request, c.Writer)
		if err != nil || user == nil {
			c.JSON(http.StatusForbidden, gin.H{"detail": "Authentication credentials were not provided."})
			return
		}
		drf.Respond(c, http.StatusOK, gin.H{
			"invitations": absoluteURL(c, "/api/v1/workspaces/"+c.Param("slug")+"/invitations/"),
		})
	})
}

// SetSessions hands the package the session manager the one session-authenticated route here needs.
func (handler *Handler) SetSessions(sessions *auth.SessionManager) { handler.sessions = sessions }

// absoluteURL is request.build_absolute_uri: the scheme and host the request arrived on, with the path on the end.
func absoluteURL(c *gin.Context, path string) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if forwarded := c.GetHeader("X-Forwarded-Proto"); forwarded != "" {
		scheme = forwarded
	}
	return scheme + "://" + c.Request.Host + path
}
