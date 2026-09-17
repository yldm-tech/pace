package live

import "github.com/gin-gonic/gin"

// helmetCSP is the content security policy helmet writes when it is given no options, with its directives in the order helmet declares them and joined the way helmet joins them — by a bare semicolon, with a directive carrying no values written as its name alone.
const helmetCSP = "default-src 'self';base-uri 'self';font-src 'self' https: data:;form-action 'self';frame-ancestors 'self';img-src 'self' data:;object-src 'none';script-src 'self';script-src-attr 'none';style-src 'self' https: 'unsafe-inline';upgrade-insecure-requests"

// helmetHeaders is every header helmet sets by default, at its default value.
//
// Cross-Origin-Embedder-Policy is deliberately absent: helmet has not set it by default since version five, and setting it here would break the editor's cross-origin assets. The max age is helmet's own — a hundred and eighty days, in seconds.
var helmetHeaders = [][2]string{
	{"Content-Security-Policy", helmetCSP},
	{"Cross-Origin-Opener-Policy", "same-origin"},
	{"Cross-Origin-Resource-Policy", "same-origin"},
	{"Origin-Agent-Cluster", "?1"},
	{"Referrer-Policy", "no-referrer"},
	{"Strict-Transport-Security", "max-age=15552000; includeSubDomains"},
	{"X-Content-Type-Options", "nosniff"},
	{"X-DNS-Prefetch-Control", "off"},
	{"X-Download-Options", "noopen"},
	{"X-Frame-Options", "SAMEORIGIN"},
	{"X-Permitted-Cross-Domain-Policies", "none"},
	{"X-XSS-Protection", "0"},
}

// securityHeaders writes what helmet writes.
func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.Writer.Header()
		for _, pair := range helmetHeaders {
			header.Set(pair[0], pair[1])
		}
		c.Next()
	}
}
