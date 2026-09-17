package worker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/yldm-tech/pace/apps/api/internal/httpsafe"
	"golang.org/x/net/html"
	"gorm.io/gorm"
)

// CrawlLinkTitleTask is the task that goes and looks at a link somebody attached to a work item, so the list can show its title rather than its address.
const CrawlLinkTitleTask = "plane.bgtasks.work_item_link_task.crawl_work_item_link_title"

// The timeouts the Python task uses, which are short enough that a slow site simply has no title rather than holding the worker.
const (
	linkFetchTimeout    = 1 * time.Second
	faviconProbeTimeout = 2 * time.Second
	linkMaxRedirects    = 5
	// linkBodyLimit bounds what is read back. Django reads the whole body; this stops a page that never ends from filling the worker's memory, which is a deliberate difference.
	linkBodyLimit = 5 << 20
)

// linkUserAgent is the browser the task pretends to be, because a good few sites will not answer anything else.
const linkUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"

// defaultFavicon is the link glyph a site with no icon of its own falls back to, kept byte for byte as the Python task has it so the two produce the same data uri.
const defaultFavicon = "PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHdpZHRoPSIyNCIgaGVpZ2h0PSIyNCIgdmlld0JveD0iMCAwIDI0IDI0IiBmaWxsPSJub25lIiBzdHJva2U9ImN1cnJlbnRDb2xvciIgc3Ryb2tlLXdpZHRoPSIyIiBzdHJva2UtbGluZWNhcD0icm91bmQiIHN0cm9rZS1saW5lam9pbj0icm91bmQiIGNsYXNzPSJsdWNpZGUgbHVjaWRlLWxpbmstaWNvbiBsdWNpZGUtbGluayI+PHBhdGggZD0iTTEwIDEzYTUgNSAwIDAgMCA3LjU0LjU0bDMtM2E1IDUgMCAwIDAtNy4wNy03LjA3bC0xLjcyIDEuNzEiLz48cGF0aCBkPSJNMTQgMTFhNSA1IDAgMCAwLTcuNTQtLjU0bC0zIDNhNSA1IDAgMCAwIDcuMDcgNy4wN2wxLjcxLTEuNzEiLz48L3N2Zz4="

// faviconRelations are the four rel values the task will take an icon from, in the order it tries them.
var faviconRelations = []string{"icon", "shortcut icon", "apple-touch-icon", "apple-touch-icon-precomposed"}

// LinkTasks crawls the links attached to work items.
type LinkTasks struct {
	db       *gorm.DB
	settings httpsafe.Settings
	logger   *slog.Logger
}

func NewLinkTasks(db *gorm.DB, settings httpsafe.Settings, logger *slog.Logger) *LinkTasks {
	return &LinkTasks{db: db, settings: settings, logger: logger}
}

func (tasks *LinkTasks) Register(consumer *Consumer) {
	consumer.Register(CrawlLinkTitleTask, tasks.crawlLinkTitle)
}

// crawlLinkTitle reproduces crawl_work_item_link_title: fetch the page, take its title and its icon, and keep both beside the link.
//
// Every fetch is pinned to an address that was validated first, and each hop of a redirect is validated again — a link somebody pasted is an address this server will go to, so the whole point is that it cannot be talked into going somewhere internal.
//
// Nothing here fails the delivery. A site that is down, refuses, or turns out to be internal leaves a null title and the fallback icon, which is what the row ends up holding.
func (tasks *LinkTasks) crawlLinkTitle(ctx context.Context, arguments []any, keywords map[string]any) error {
	linkID := stringArgument(arguments, keywords, 0, "id")
	target := stringArgument(arguments, keywords, 1, "url")

	metadata := tasks.crawl(ctx, target)
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}

	var link struct {
		ID string `gorm:"column:id"`
	}
	err = tasks.db.WithContext(ctx).Table("issue_links").Select("id").
		Where("id = ? AND deleted_at IS NULL", linkID).Take(&link).Error
	if err != nil {
		tasks.logger.Warn("work item link not found", "link", linkID, "url", target)
		return nil
	}
	// Django saves the whole instance, and BaseModel.save blanks the two audit columns because the worker has no current user. So crawling a link forgets who added it.
	return tasks.db.WithContext(ctx).Table("issue_links").Where("id = ?", linkID).
		Updates(map[string]any{
			"metadata": string(encoded), "updated_at": time.Now().UTC(),
			"created_by_id": nil, "updated_by_id": nil,
		}).Error
}

// crawl is the whole of what the task learns about a link: a title if the page gave one, and an icon or the fallback.
func (tasks *LinkTasks) crawl(ctx context.Context, target string) map[string]any {
	headers := map[string]string{"User-Agent": linkUserAgent}
	finalURL := target
	var document *html.Node
	var title any

	response, resolved, err := httpsafe.FetchFollowingRedirects(ctx, "GET", target,
		tasks.settings, headers, linkFetchTimeout, linkMaxRedirects, linkBodyLimit)
	if err != nil {
		tasks.logger.Warn("link title could not be fetched", "url", target, "error", err)
	} else {
		finalURL = resolved
		if parsed, err := html.Parse(strings.NewReader(response.Body)); err == nil {
			document = parsed
			if text, found := documentTitle(parsed); found {
				title = strings.TrimSpace(text)
			}
		}
	}

	icon := tasks.favicon(ctx, headers, document, finalURL)
	return map[string]any{
		"title": title, "favicon": icon["favicon_base64"],
		"url": target, "favicon_url": icon["favicon_url"],
	}
}

// favicon finds the icon and reads it, and answers with the fallback whenever any part of that does not work out.
func (tasks *LinkTasks) favicon(ctx context.Context, headers map[string]string, document *html.Node, base string) map[string]any {
	fallback := map[string]any{"favicon_url": nil, "favicon_base64": "data:image/svg+xml;base64," + defaultFavicon}

	iconURL, ok := tasks.faviconURL(ctx, document, base)
	if !ok {
		return fallback
	}
	response, _, err := httpsafe.FetchFollowingRedirects(ctx, "GET", iconURL,
		tasks.settings, headers, linkFetchTimeout, linkMaxRedirects, linkBodyLimit)
	if err != nil {
		tasks.logger.Warn("favicon could not be fetched", "url", iconURL, "error", err)
		return fallback
	}
	contentType := response.Headers.Get("Content-Type")
	if contentType == "" {
		contentType = "image/x-icon"
	}
	return map[string]any{
		"favicon_url":    iconURL,
		"favicon_base64": "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString([]byte(response.Body)),
	}
}

// faviconURL is find_favicon_url: the first icon the page declares, and failing that the one at the root — which is only used when a plain HEAD finds it there.
func (tasks *LinkTasks) faviconURL(ctx context.Context, document *html.Node, base string) (string, bool) {
	parsedBase, err := url.Parse(base)
	if err != nil {
		return "", false
	}
	if document != nil {
		for _, relation := range faviconRelations {
			href, found := linkHref(document, relation)
			if !found || href == "" {
				continue
			}
			reference, err := url.Parse(strings.TrimSpace(href))
			if err != nil {
				continue
			}
			resolved := parsedBase.ResolveReference(reference).String()
			// The declared icon is validated on its own account, since a page can point its icon somewhere the page itself was not.
			if err := validateTarget(resolved, tasks.settings); err != nil {
				tasks.logger.Warn("declared favicon refused", "url", resolved, "error", err)
				return "", false
			}
			return resolved, true
		}
	}

	root := parsedBase.Scheme + "://" + parsedBase.Host + "/favicon.ico"
	response, err := httpsafe.Fetch(ctx, "HEAD", root, tasks.settings, nil, faviconProbeTimeout, 0)
	if err != nil || response.StatusCode != 200 {
		return "", false
	}
	return root, true
}

// validateTarget resolves a url's host and checks every address without fetching anything, which is what the Python task does before it follows a declared icon.
func validateTarget(target string, settings httpsafe.Settings) error {
	parsed, err := url.Parse(target)
	if err != nil {
		return httpsafe.RejectedError{Reason: "Invalid URL"}
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return httpsafe.RejectedError{Reason: "Invalid URL scheme. Only HTTP and HTTPS are allowed"}
	}
	hostname := parsed.Hostname()
	if hostname == "" {
		return httpsafe.RejectedError{Reason: "Invalid URL: No hostname found"}
	}
	if httpsafe.HostAllowed(hostname, settings.AllowedHosts) {
		return nil
	}
	_, err = httpsafe.ResolveAndValidate(hostname, settings.AllowedIPs, true)
	return err
}

// documentTitle reads the first title element's text, which is what BeautifulSoup's find("title") returns.
func documentTitle(document *html.Node) (string, bool) {
	var found string
	var seen bool
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if seen {
			return
		}
		if node.Type == html.ElementNode && node.Data == "title" {
			var text strings.Builder
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == html.TextNode {
					text.WriteString(child.Data)
				}
			}
			found, seen = text.String(), true
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	return found, seen
}

// linkHref finds the first link element carrying one rel value and reads its href. The rel is matched whole and case-insensitively, the way a css attribute selector matches it.
func linkHref(document *html.Node, relation string) (string, bool) {
	var href string
	var seen bool
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if seen {
			return
		}
		if node.Type == html.ElementNode && node.Data == "link" {
			attributes := map[string]string{}
			for _, attribute := range node.Attr {
				attributes[strings.ToLower(attribute.Key)] = attribute.Val
			}
			if strings.EqualFold(strings.TrimSpace(attributes["rel"]), relation) {
				href, seen = attributes["href"], true
				return
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	return href, seen
}
