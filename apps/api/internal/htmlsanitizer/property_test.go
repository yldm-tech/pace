package htmlsanitizer

import (
	"math/rand"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// TestCleanNeverEscapesThePolicy is the security property the port has to hold
// regardless of how closely its output tracks nh3: whatever comes back must
// reparse into allowlisted tags, allowlisted attributes, and safe URL schemes.
// It is checked over generated markup so malformed and hostile input is covered
// without pinning byte-for-byte expectations.
func TestCleanNeverEscapesThePolicy(t *testing.T) {
	policy := PlanePolicy()
	random := rand.New(rand.NewSource(20260915))
	for iteration := 0; iteration < 4000; iteration++ {
		input := randomMarkup(random, 0)
		cleaned := Clean(input)
		context := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
		nodes, err := html.ParseFragment(strings.NewReader(cleaned), context)
		if err != nil {
			t.Fatalf("reparse %q: %v", cleaned, err)
		}
		for _, node := range nodes {
			assertWithinPolicy(t, policy, node, input, cleaned)
		}
	}
}

func assertWithinPolicy(t *testing.T, policy *Policy, node *html.Node, input, cleaned string) {
	t.Helper()
	if node.Type == html.ElementNode {
		name := strings.ToLower(node.Data)
		if node.Namespace != "" || !policy.allowsTag(name) {
			t.Fatalf("tag %q survived\n in   %q\n out  %q", name, input, cleaned)
		}
		for _, attribute := range node.Attr {
			attributeName := attribute.Key
			if attribute.Namespace != "" {
				attributeName = attribute.Namespace + ":" + attributeName
			}
			if name == "a" && attributeName == "rel" {
				continue
			}
			if !policy.allowsAttribute(name, attributeName) {
				t.Fatalf("attribute %q on %q survived\n in   %q\n out  %q", attributeName, name, input, cleaned)
			}
			if policy.checksURL(attributeName) && !policy.allowsURL(attribute.Val) {
				t.Fatalf("unsafe url %q survived\n in   %q\n out  %q", attribute.Val, input, cleaned)
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		assertWithinPolicy(t, policy, child, input, cleaned)
	}
}

var fuzzTags = []string{
	"p", "div", "span", "a", "img", "script", "style", "template", "iframe",
	"svg", "math", "object", "embed", "form", "select", "option", "button",
	"body", "html", "meta", "link", "base", "frame", "frameset", "marquee",
	"font", "plaintext", "xmp", "noscript", "textarea", "custom-el", "H3",
	"table", "tr", "td", "th", "li", "ul", "pre", "code", "input", "label",
	"mention-component", "image-component",
}

var fuzzAttributes = []string{
	"class", "id", "onclick", "onerror", "style", "href", "src", "target",
	"rel", "title", "data-type", "data-unknown", "checked", "type", "colspan",
	"language", "spellcheck", "srcset", "entity_name", "aspectRatio", "xmlns",
}

var fuzzValues = []string{
	"x", "http://a.com", "https://a.com/p?q=1&r=2", "javascript:alert(1)",
	"JaVaScRiPt:x", "data:text/html;base64,PHA+", "mailto:a@b.com", "tel:+1",
	"/rel/path", "//host/p", "#frag", "  http://spaced.com  ", "vbscript:x",
	"", `"q"`, "'s'", "a&b", "<x>", " ", "中文",
}

var fuzzText = []string{
	"", "t", "a & b", "5 < 6 > 4", `"q"`, "'s'", "&amp;", "&#65;",
	"&unknown;", " ", "中文 \U0001F600", "</p>", "<b>x", "\n\t",
}

func randomMarkup(random *rand.Rand, depth int) string {
	tag := fuzzTags[random.Intn(len(fuzzTags))]
	var attributes strings.Builder
	for count := random.Intn(4); count > 0; count-- {
		name := fuzzAttributes[random.Intn(len(fuzzAttributes))]
		if random.Intn(8) == 0 {
			attributes.WriteString(" " + name)
			continue
		}
		quote := []string{`"`, "'", ""}[random.Intn(3)]
		value := fuzzValues[random.Intn(len(fuzzValues))]
		if quote == "" && (value == "" || strings.ContainsAny(value, " \t")) {
			quote = `"`
		}
		attributes.WriteString(" " + name + "=" + quote + value + quote)
	}
	var inner string
	if depth < 2 && random.Intn(100) < 45 {
		for count := random.Intn(3) + 1; count > 0; count-- {
			inner += randomMarkup(random, depth+1)
		}
	} else {
		inner = fuzzText[random.Intn(len(fuzzText))]
	}
	switch {
	case random.Intn(10) == 0:
		return "<" + tag + attributes.String() + ">" + inner
	case random.Intn(16) == 0:
		return "<" + tag + attributes.String() + "/>"
	default:
		return "<" + tag + attributes.String() + ">" + inner + "</" + tag + ">"
	}
}

func TestValidateHTMLContentMirrorsPythonHelper(t *testing.T) {
	valid, message, cleaned := ValidateHTMLContent("")
	if !valid || message != "" || cleaned != nil {
		t.Fatalf("empty content = %v, %q, %v", valid, message, cleaned)
	}

	valid, message, cleaned = ValidateHTMLContent("<p>ok<script>x</script></p>")
	if !valid || message != "" || cleaned == nil || *cleaned != "<p>ok</p>" {
		t.Fatalf("content = %v, %q, %v", valid, message, cleaned)
	}

	valid, message, cleaned = ValidateHTMLContent(strings.Repeat("a", MaxSize+1))
	if valid || message != "HTML content exceeds maximum size limit (10MB)" || cleaned != nil {
		t.Fatalf("oversized content = %v, %q, %v", valid, message, cleaned)
	}
}

func TestPolicyCoversContentValidatorConfiguration(t *testing.T) {
	policy := PlanePolicy()
	if got := len(sortedTags(policy.tags)); got != 79 {
		t.Fatalf("policy has %d tags, want the 75 nh3 defaults plus the 4 editor tags", got)
	}
	for _, tag := range []string{"mention-component", "image-component", "label", "input"} {
		if !policy.allowsTag(tag) {
			t.Errorf("editor tag %q is not allowed", tag)
		}
	}
	for _, tag := range []string{"script", "style", "iframe", "object", "form"} {
		if policy.allowsTag(tag) {
			t.Errorf("tag %q must not be allowed", tag)
		}
	}
	for _, scheme := range []string{"http", "https", "mailto", "tel"} {
		if !policy.allowsScheme(scheme) {
			t.Errorf("scheme %q is not allowed", scheme)
		}
	}
	for _, scheme := range []string{"javascript", "data", "vbscript", "ftp"} {
		if policy.allowsScheme(scheme) {
			t.Errorf("scheme %q must not be allowed", scheme)
		}
	}
}
