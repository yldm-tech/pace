package worker

import (
	"strings"
	"testing"

	"github.com/yldm-tech/pace/apps/api-go/internal/httpsafe"
	"golang.org/x/net/html"
)

func parse(t *testing.T, source string) *html.Node {
	t.Helper()
	document, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return document
}

// The title is the first one in the document, read as text.
func TestTheTitleIsTheFirstOne(t *testing.T) {
	document := parse(t, `<html><head><title>  Hello  </title><title>Second</title></head><body></body></html>`)
	title, found := documentTitle(document)
	if !found {
		t.Fatal("no title was found")
	}
	if strings.TrimSpace(title) != "Hello" {
		t.Errorf("the title is %q", title)
	}
	// A page with no title leaves the field null rather than empty.
	if _, found := documentTitle(parse(t, `<html><body>nothing</body></html>`)); found {
		t.Error("a page with no title reported one")
	}
}

// The icon is taken from the first of the four rel values the task recognises, and the match is whole and case-insensitive.
func TestTheFaviconRelations(t *testing.T) {
	if len(faviconRelations) != 4 {
		t.Fatalf("there are %d relations, want 4", len(faviconRelations))
	}
	document := parse(t, `<html><head><link rel="ICON" href="/a.png"><link rel="apple-touch-icon" href="/b.png"></head></html>`)
	href, found := linkHref(document, "icon")
	if !found || href != "/a.png" {
		t.Errorf("the icon href is %q (found %v)", href, found)
	}
	href, found = linkHref(document, "apple-touch-icon")
	if !found || href != "/b.png" {
		t.Errorf("the apple icon href is %q (found %v)", href, found)
	}
	// A rel that only contains the word is not a match, the way a css attribute selector would not match it.
	document = parse(t, `<html><head><link rel="preload icon" href="/c.png"></head></html>`)
	if _, found := linkHref(document, "icon"); found {
		t.Error("a compound rel matched")
	}
	if _, found := linkHref(document, "shortcut icon"); found {
		t.Error("a compound rel matched the two-word relation")
	}
}

// The fallback icon is the same bytes the Python task carries, so a link with no icon looks the same whichever worker crawled it.
func TestTheFallbackIcon(t *testing.T) {
	if !strings.HasPrefix(defaultFavicon, "PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmci") {
		t.Errorf("the fallback icon starts %q", defaultFavicon[:40])
	}
	if strings.Contains(defaultFavicon, "\n") || strings.Contains(defaultFavicon, " ") {
		t.Error("the fallback icon carries whitespace, and base64 in a data uri must not")
	}
}

// A url that is not http is refused before anything is resolved, which is what keeps file:// and gopher:// out.
func TestOnlyHTTPTargetsAreAccepted(t *testing.T) {
	for _, target := range []string{"file:///etc/passwd", "gopher://example.test/", "ftp://example.test/"} {
		if err := validateTarget(target, linkTestSettings()); err == nil {
			t.Errorf("%q was accepted", target)
		}
	}
	if err := validateTarget("https://", linkTestSettings()); err == nil {
		t.Error("a url with no host was accepted")
	}
}

// An address inside the blocked ranges is refused however it is written.
func TestInternalTargetsAreRefused(t *testing.T) {
	for _, target := range []string{
		"http://127.0.0.1/",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/",
		"http://[::1]/",
	} {
		if err := validateTarget(target, linkTestSettings()); err == nil {
			t.Errorf("%q was accepted", target)
		}
	}
}

func linkTestSettings() httpsafe.Settings { return httpsafe.Settings{} }
