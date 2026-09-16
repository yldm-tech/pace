package server

import (
	"os"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestEveryCutOverPathIsFullyServed is the guard that would have caught the cycle issue regression.
//
// The proxy cuts traffic over by path, not by method. So a matcher that covers a path Django serves with four methods sends all four to Go — and any method the Go router does not register becomes a 404 the moment the matcher merges. That happened, silently, and was only found by reading the route inventory afterwards.
//
// This compares the three sources directly: every route Django serves, the paths the Caddyfile cuts over, and the routes the Go router registers. A path that is cut over must have every one of Django's methods.
func TestEveryCutOverPathIsFullyServed(t *testing.T) {
	djangoRoutes := readDjangoRoutes(t)
	matchers := communityProxyMatchers(t)
	goRoutes := goRouteShapes(t)

	missing := []string{}
	for _, route := range djangoRoutes {
		if !anyMatcherCovers(matchers, route.path) {
			continue
		}
		if goRoutes[route.method+" "+route.path] {
			continue
		}
		missing = append(missing, route.method+" "+route.path)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("the proxy cuts these over but the Go router does not serve them, so they answer 404:\n  %s",
			strings.Join(missing, "\n  "))
	}
}

type djangoRoute struct {
	method string
	path   string
}

func readDjangoRoutes(t *testing.T) []djangoRoute {
	t.Helper()
	contents, err := os.ReadFile("testdata/django_routes.tsv")
	if err != nil {
		t.Fatal(err)
	}
	routes := []djangoRoute{}
	for _, line := range strings.Split(string(contents), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		method, path, found := strings.Cut(line, "\t")
		if !found {
			t.Fatalf("malformed fixture row %q", line)
		}
		routes = append(routes, djangoRoute{method: method, path: collapseWildcardSegments(path)})
	}
	if len(routes) < 500 {
		t.Fatalf("the fixture has only %d routes, which cannot be the whole app", len(routes))
	}
	return routes
}

// communityProxyMatchers compiles every path the community proxy cuts over to Go, rewritten to match the star shape the fixture uses.
//
// The Caddyfile cuts a path over in two ways, and both have to be read. A named `path_regexp` matcher is the obvious one. The other is a bare path on the `reverse_proxy` line itself — `reverse_proxy /auth/* api-go:8000` — which is Caddy's own path matcher and cuts over just as completely. Reading only the first kind is what let sixty-seven routes sit behind this guard without it ever checking that Go serves them.
func communityProxyMatchers(t *testing.T) []*regexp.Regexp {
	t.Helper()
	config := communityProxyConfig(t)
	// Only the matchers that are actually reverse proxied to the Go service count.
	proxied := map[string]bool{}
	matchers := []*regexp.Regexp{}
	for _, line := range strings.Split(config, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "reverse_proxy" || !strings.HasSuffix(fields[len(fields)-1], "api-go:8000") {
			continue
		}
		if strings.HasPrefix(fields[1], "@") {
			proxied[strings.TrimPrefix(fields[1], "@")] = true
			continue
		}
		matchers = append(matchers, caddyPathMatcher(t, fields[1]))
	}

	declaration := regexp.MustCompile(`^\s*@(\w+)\s+path_regexp\s+\w+\s+(\S+)$`)
	for _, line := range strings.Split(config, "\n") {
		groups := declaration.FindStringSubmatch(line)
		if groups == nil || !proxied[groups[1]] {
			continue
		}
		// A parameter in the proxy's regexp becomes the star the fixture uses, so the two shapes can be compared.
		pattern := groups[2]
		for _, parameter := range []string{`[^/]+`, `[0-9A-Fa-f-]+`} {
			pattern = strings.ReplaceAll(pattern, parameter, `\*`)
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			t.Fatalf("matcher %s does not compile after rewriting: %v", groups[1], err)
		}
		matchers = append(matchers, compiled)
	}
	if len(matchers) == 0 {
		t.Fatal("no proxied path matchers were found, so this guard would pass vacuously")
	}
	return matchers
}

// caddyPathMatcher compiles one of Caddy's own path matchers against the star shape the fixture uses.
//
// Caddy's `*` matches any characters, slashes included, and a path with no star at all matches only itself. Both are written out here rather than approximated, because the difference decides whether a whole subtree is cut over or a single route is.
func caddyPathMatcher(t *testing.T, path string) *regexp.Regexp {
	t.Helper()
	var pattern strings.Builder
	pattern.WriteString("^")
	for index, segment := range strings.Split(path, "*") {
		if index > 0 {
			// Caddy's star crosses slashes, and so does the fixture's.
			pattern.WriteString(`.*`)
		}
		pattern.WriteString(regexp.QuoteMeta(segment))
	}
	pattern.WriteString("$")
	compiled, err := regexp.Compile(pattern.String())
	if err != nil {
		t.Fatalf("proxy path %s does not compile: %v", path, err)
	}
	return compiled
}

func anyMatcherCovers(matchers []*regexp.Regexp, path string) bool {
	for _, matcher := range matchers {
		if matcher.MatchString(path) {
			return true
		}
	}
	return false
}

// goRouteShapes reads the routes the Go router registers, normalised to the same star shape.
func goRouteShapes(t *testing.T) map[string]bool {
	t.Helper()
	parameter := regexp.MustCompile(`:[^/]+`)
	shapes := map[string]bool{}
	for _, route := range newTestRouter(t).Routes() {
		shapes[route.Method+" "+collapseWildcardSegments(parameter.ReplaceAllString(route.Path, "*"))] = true
	}
	// DRF's format suffixes are served by a rewrite rather than by a route of their own: a suffix is part of a segment and this router's parameters are whole segments, so the path is stripped and dispatched again after routing has failed. They are named here because they really are served, and the guard compares registered shapes.
	for _, shape := range formatSuffixShapes(shapes) {
		shapes[shape] = true
	}
	if len(shapes) == 0 {
		t.Fatal("the router registered no routes")
	}
	return shapes
}

// formatSuffixShapes is every route the suffix rewrite reaches: the two router-mounted collections and their details, plus the router root. A suffix on anything else is a 404, which is what DRF does with it too.
func formatSuffixShapes(shapes map[string]bool) []string {
	suffixed := []string{}
	for _, prefix := range []string{"invitations", "stickies"} {
		collection := "/api/v1/workspaces/*/" + prefix + "/"
		detail := collection + "*/"
		for _, method := range []string{"GET", "POST", "PATCH", "PUT", "DELETE"} {
			if shapes[method+" "+collection] {
				// `stickies.json` keeps the dot, which the fixture writes as `stickies.*`.
				suffixed = append(suffixed, method+" /api/v1/workspaces/*/"+prefix+".*")
			}
			if shapes[method+" "+detail] {
				// `stickies/<pk>.json` has an optional trailing slash, so the fixture writes it without one — two wildcards and no slash on the end.
				suffixed = append(suffixed, method+" /api/v1/workspaces/*/"+prefix+"/*")
			}
		}
	}
	// The router root with a suffix, which is the one shape that looks like a bare parameter.
	if shapes["GET /api/v1/workspaces/*/"] {
		suffixed = append(suffixed, "GET /api/v1/workspaces/*/*")
	}
	return suffixed
}

// newTestRouter builds the router with its API routes registered. The database handle never connects — the guard reads which routes exist, not what they return — but it has to be non-nil, since that is what gates the registration.
func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(postgres.New(postgres.Config{DSN: "postgres://guard:guard@127.0.0.1:1/guard"}), &gorm.Config{
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Skipf("cannot build a router without a database handle: %v", err)
	}
	return NewRouter(Dependencies{
		Database:    database,
		CORSOrigins: []string{"http://localhost:3000"},
		AuthSettings: &auth.Settings{
			AppBaseURL: "http://localhost:3000", WebURL: "http://localhost:8000",
			// The session layer refuses to start without one. Any value does, since the guard reads which routes exist rather than calling them.
			SecretKey: "cutover-guard",
		},
	})
}

// collapseWildcardSegments rewrites any path segment holding a parameter down to a lone star.
//
// Django can capture twice in one segment — "<str:project_identifier>-<str:issue_identifier>" is one segment naming a work item the way a person would — and a Gin route cannot: it captures the whole segment and splits the text itself. Both shapes describe the same URL, so both are normalised to one star before they are compared, or the guard would report a route as unserved when it is served.
func collapseWildcardSegments(path string) string {
	segments := strings.Split(path, "/")
	for index, segment := range segments {
		if segment != "*" && strings.Contains(segment, "*") {
			segments[index] = "*"
		}
	}
	return strings.Join(segments, "/")
}
