package live

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// corsMethods and corsHeaders are the two lists the service it replaces is configured with.
var (
	corsMethods = []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions}
	corsHeaders = []string{"Content-Type", "Authorization", "x-api-key"}
)

// crossOriginHeaders is a port of the cors middleware as it is configured here.
//
// The allowed origins are a list, so the reply reflects the request's own origin when it is on the list and carries no allow-origin header at all when it is not — which is a refusal the browser makes rather than one the server makes. Vary: Origin goes out either way, because the answer depends on it.
//
// A preflight is answered here and goes no further: 204 with an explicit zero content length, which is what Safari needs before it will stop waiting for a body. That happens for any OPTIONS request, including one to a path nothing serves.
func crossOriginHeaders(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = true
	}
	methods := strings.Join(corsMethods, ",")
	headers := strings.Join(corsHeaders, ",")

	return func(c *gin.Context) {
		header := c.Writer.Header()
		if origin := c.Request.Header.Get("Origin"); allowed[origin] && origin != "" {
			header.Set("Access-Control-Allow-Origin", origin)
		}
		header.Add("Vary", "Origin")
		header.Set("Access-Control-Allow-Credentials", "true")

		if c.Request.Method != http.MethodOptions {
			c.Next()
			return
		}
		header.Set("Access-Control-Allow-Methods", methods)
		header.Set("Access-Control-Allow-Headers", headers)
		header.Set("Content-Length", "0")
		c.AbortWithStatus(http.StatusNoContent)
	}
}
