package ydoc

import "strings"

// markRenderers is each mark type's renderHTML, keyed the way the schema names it. Every one of them is a port of the extension that declares it; the corpus is what says the port is right.
var markRenderers = map[string]func(Mark) spec{
	"bold":        func(Mark) spec { return element("strong", nil, hole()) },
	"italic":      func(Mark) spec { return element("em", nil, hole()) },
	"strike":      func(Mark) spec { return element("s", nil, hole()) },
	"underline":   func(Mark) spec { return element("u", nil, hole()) },
	"textStyle":   func(Mark) spec { return element("span", nil, hole()) },
	"code":        renderCodeMark,
	"link":        renderLink,
	"customColor": renderCustomColor,
}

// renderCodeMark is the inline code mark. Its classes and its spellcheck are the extension's own options rather than anything stored on the document.
func renderCodeMark(Mark) spec {
	options := newAttrs(
		"class", "rounded-sm bg-layer-3 px-[6px] py-[1.5px] font-code font-medium text-(--extended-color-orange-600) border-[0.5px] border-subtle",
		"spellcheck", "false",
	)
	return element("code", mergeAttributes(options), hole())
}

// blockedLinkProtocols are the schemes a browser would run in the page's own security context.
var blockedLinkProtocols = []string{"javascript:", "data:", "vbscript:"}

// isDangerousHref reports whether an href would execute rather than navigate.
//
// The check is made against a normalised copy rather than the raw value, because a browser normalises before it decides what the scheme is: the WHATWG URL parser strips tab, newline and carriage return from anywhere in a url, and strips leading C0 controls and whitespace before the scheme. Without that, a leading tab is enough to slip "javascript:" past a plain prefix test.
func isDangerousHref(raw string) bool {
	var normalized strings.Builder
	for _, r := range raw {
		if r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		normalized.WriteRune(r)
	}
	trimmed := strings.TrimLeftFunc(normalized.String(), func(r rune) bool {
		if r <= 0x08 || (r >= 0x0e && r <= 0x1f) {
			return true
		}
		return isJSWhitespace(r)
	})
	lowered := strings.ToLower(trimmed)
	for _, protocol := range blockedLinkProtocols {
		if strings.HasPrefix(lowered, protocol) {
			return true
		}
	}
	return false
}

func renderLink(mark Mark) spec {
	options := newAttrs(
		"target", "_blank",
		"rel", "noopener noreferrer nofollow",
		"class", "text-accent-secondary underline underline-offset-[3px] hover:text-accent-primary transition-colors cursor-pointer",
	)
	attrs := attrsInOrder(mark.Attrs, "href", "target", "rel", "class")
	// A dangerous href is emptied rather than dropped, so the text stays a link that goes nowhere.
	if isDangerousHref(asString(mark.Attrs["href"])) {
		attrs.set("href", "")
	}
	return element("a", mergeAttributes(options, attrs), hole())
}

// paletteColours are the colour keys the editor offers by name. A colour outside the list is written out as a style declaration as well — which this pipeline then drops, so the distinction leaves no trace in the stored HTML.
var paletteColours = map[string]bool{
	"gray": true, "peach": true, "pink": true, "orange": true, "green": true, "light-blue": true,
	"dark-blue": true, "purple": true, "pink-blue-gradient": true, "sans-serif": true, "serif": true, "monospace": true,
}

func renderCustomColor(mark Mark) spec {
	colour := newAttrs()
	if value := asString(mark.Attrs["color"]); value != "" {
		colour.set("data-text-color", value)
		if !paletteColours[value] {
			colour.set("style", "color: "+value)
		}
	}
	background := newAttrs()
	if value := asString(mark.Attrs["backgroundColor"]); value != "" {
		background.set("data-background-color", value)
		if !paletteColours[value] {
			background.set("style", "background-color: "+value)
		}
	}
	return element("span", mergeAttributes(colour, background), hole())
}

// isJSWhitespace matches what JavaScript's \s matches, which is what the upstream check trims with.
func isJSWhitespace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\v', '\f', '\r', 0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}
