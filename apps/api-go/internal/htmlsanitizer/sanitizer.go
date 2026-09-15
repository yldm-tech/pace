package htmlsanitizer

import (
	"sort"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ValidateHTMLContent mirrors content_validator.validate_html_content. It
// returns whether the content is usable, the failure message, and the cleaned
// HTML. Empty content is reported as valid with no cleaned value, exactly as
// the Python helper does.
func ValidateHTMLContent(content string) (bool, string, *string) {
	if content == "" {
		return true, "", nil
	}
	if len(content) > MaxSize {
		return false, "HTML content exceeds maximum size limit (10MB)", nil
	}
	cleaned := Clean(content)
	return true, "", &cleaned
}

// Clean applies the policy validate_html_content uses.
func Clean(content string) string {
	return PlanePolicy().Clean(content)
}

// Clean parses the fragment in body context the way ammonia does, filters the
// tree, and serializes the result.
func (policy *Policy) Clean(content string) string {
	context := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(content), context)
	if err != nil {
		// x/net/html only fails here on a read error, which a strings.Reader
		// cannot produce; treat anything unexpected as fully unsafe.
		return ""
	}
	var builder strings.Builder
	for _, node := range nodes {
		policy.renderNode(&builder, node, false)
	}
	return builder.String()
}

// renderNode walks the parsed tree. rawText says whether the nearest ancestor
// that survives into the output is a raw text element, which is the only case
// where the serializer leaves text unescaped.
func (policy *Policy) renderNode(builder *strings.Builder, node *html.Node, rawText bool) {
	switch node.Type {
	case html.TextNode:
		if rawText {
			builder.WriteString(node.Data)
			return
		}
		builder.WriteString(escapeText(node.Data))
	case html.ElementNode:
		policy.renderElement(builder, node)
	case html.DocumentNode:
		policy.renderChildren(builder, node, rawText)
	}
	// Comments and doctypes are dropped, matching ammonia.
}

func (policy *Policy) renderChildren(builder *strings.Builder, node *html.Node, rawText bool) {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		policy.renderNode(builder, child, rawText)
	}
}

func (policy *Policy) renderElement(builder *strings.Builder, node *html.Node) {
	name := strings.ToLower(node.Data)
	if policy.dropsContent(name) || policy.isolatesContent(name) {
		return
	}
	// SVG and MathML elements live in a foreign namespace, so they never match
	// the HTML allowlist ammonia checks and are unwrapped like any other
	// disallowed tag.
	if node.Namespace != "" || !policy.allowsTag(name) {
		// ammonia unwraps a disallowed element and keeps its children. The
		// wrapper is gone, so its content is escaped even when the source
		// element was a raw text one such as plaintext or textarea.
		policy.renderChildren(builder, node, false)
		return
	}
	builder.WriteString("<")
	builder.WriteString(name)
	for _, attribute := range policy.filterAttributes(name, node.Attr) {
		builder.WriteString(" ")
		builder.WriteString(attribute.Key)
		builder.WriteString(`="`)
		builder.WriteString(escapeAttribute(attribute.Val))
		builder.WriteString(`"`)
	}
	builder.WriteString(">")
	if voidElements[name] {
		return
	}
	policy.renderChildren(builder, node, rawTextElements[name])
	builder.WriteString("</")
	builder.WriteString(name)
	builder.WriteString(">")
}

func (policy *Policy) filterAttributes(tag string, attributes []html.Attribute) []html.Attribute {
	kept := make([]html.Attribute, 0, len(attributes))
	seen := make(map[string]struct{}, len(attributes))
	for _, attribute := range attributes {
		name := attribute.Key
		if attribute.Namespace != "" {
			name = attribute.Namespace + ":" + name
		}
		if tag == "a" && name == "rel" {
			// ammonia rewrites rel on every anchor, so any incoming value goes.
			continue
		}
		if !policy.allowsAttribute(tag, name) {
			continue
		}
		if policy.checksURL(name) && !policy.allowsURL(attribute.Val) {
			continue
		}
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		kept = append(kept, html.Attribute{Key: name, Val: attribute.Val})
	}
	if tag == "a" && policy.linkRel != "" {
		kept = append(kept, html.Attribute{Key: "rel", Val: policy.linkRel})
	}
	return kept
}

// allowsURL accepts relative references and absolute URLs whose scheme is in
// the policy, which is how ammonia treats url_schemes with relative URLs
// passing through.
func (policy *Policy) allowsURL(value string) bool {
	scheme, ok := urlScheme(value)
	if !ok {
		return true
	}
	return policy.allowsScheme(scheme)
}

func urlScheme(value string) (string, bool) {
	trimmed := strings.TrimLeft(value, " \t\n\r\f\v")
	for index, character := range trimmed {
		switch {
		case character == ':':
			if index == 0 {
				return "", false
			}
			return strings.ToLower(trimmed[:index]), true
		case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z':
			continue
		case index > 0 && (character >= '0' && character <= '9' || character == '+' || character == '-' || character == '.'):
			continue
		default:
			return "", false
		}
	}
	return "", false
}

// rawTextElements keep their text unescaped during serialization. None of them
// is in the policy allowlist today, so this only matters if one is ever added.
var rawTextElements = map[string]bool{
	"style": true, "script": true, "xmp": true, "iframe": true,
	"noembed": true, "noframes": true, "plaintext": true, "noscript": true,
}

var voidElements = map[string]bool{
	"area": true, "base": true, "basefont": true, "bgsound": true, "br": true,
	"col": true, "embed": true, "frame": true, "hr": true, "img": true,
	"input": true, "keygen": true, "link": true, "meta": true, "param": true,
	"source": true, "track": true, "wbr": true,
}

// escapeText follows the html5ever serializer: only &, <, > and the no-break
// space are replaced, so quotes in text survive untouched.
func escapeText(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\u00a0", "&nbsp;")
	return replacer.Replace(value)
}

// escapeAttribute follows the html5ever serializer for a double-quoted value.
func escapeAttribute(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", `"`, "&quot;", "\u00a0", "&nbsp;")
	return replacer.Replace(value)
}

// sortedTags is only used by tests and diagnostics.
func sortedTags(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
