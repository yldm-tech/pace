package vdom

import (
	"html"
	"regexp"
	"strings"
)

// The three shapes the scanner recognises, and the one that tells a start tag it closes itself. They are zeed-dom's own, transcribed with Go's syntax for a character class and without the anchors Go applies by matching at the start explicitly.
var (
	endTagPattern    = regexp.MustCompile(`^</([^>\s]+)[^>]*>`)
	startTagPattern  = regexp.MustCompile(`^<([^>\s/]+)((\s+[^=>\s]+(\s*=\s*(("[^"]*")|('[^']*')|[^>\s]+))?)*)\s*(?:/\s*)?>`)
	selfClosePattern = regexp.MustCompile(`\s*/\s*>\s*$`)
	attrPattern      = regexp.MustCompile(`([^=\s]+)(\s*=\s*(("([^"]*)")|('([^']*)')|[^>\s]+))?`)
)

// voidTags are written without a closing tag and never hold anything. The list is zeed-dom's, which is not quite HTML's — it carries `command`, which no longer exists.
var voidTags = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "keygen": true, "link": true, "meta": true, "param": true, "source": true,
	"track": true, "wbr": true, "command": true,
}

// Parse reads HTML into a tree.
//
// It is a scanner rather than a tree builder, and the difference is worth stating because it is what the editor does. A closing tag pops whatever is open rather than the tag it names, so markup that overlaps — `<b><i></b></i>` — nests by where the tags are rather than by what they say. Nothing is implied: a `<tr>` written straight inside a `<table>` stays there, with no `<tbody>` invented around it. A tag the scanner cannot make sense of is text.
func Parse(source string) *Node {
	fragment := NewFragment()
	stack := []*Node{fragment}

	// current is whatever is open, and nil once the stack has been emptied by an unbalanced closing tag. Everything after that point is dropped, which is what zeed-dom does: the pop takes the fragment itself off the stack and there is nothing left to append to.
	current := func() *Node {
		if len(stack) == 0 {
			return nil
		}
		return stack[len(stack)-1]
	}

	appendText := func(text string) {
		if text == "" {
			return
		}
		parent := current()
		if parent == nil {
			return
		}
		decoded := html.UnescapeString(text)
		if last := parent.LastChild(); last != nil && last.Type == TextNode {
			last.Text += decoded
			return
		}
		parent.Append(&Node{Type: TextNode, Text: decoded})
	}

	for len(source) > 0 {
		// Each case either consumes what it recognised and carries on, or falls out of the switch to be read as text. Falling out is how an unrecognised tag, an unterminated comment and the body of a script all end up as text.
		switch {
		case strings.HasPrefix(source, "<!--"):
			// A comment is read and dropped. One that never ends is text.
			if index := strings.Index(source, "-->"); index != -1 {
				source = source[index+3:]
				continue
			}

		case strings.HasPrefix(source, "</"):
			match := endTagPattern.FindString(source)
			if match != "" {
				source = source[len(match):]
				// Whatever is open is closed, named or not — including the fragment, when nothing is open.
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
				continue
			}

		case strings.HasPrefix(source, "<"):
			match := startTagPattern.FindStringSubmatch(source)
			if match != nil {
				whole := match[0]
				source = source[len(whole):]
				tagName := match[1]
				selfClosing := selfClosePattern.MatchString(whole)
				attributeSource := match[2]
				if selfClosing {
					attributeSource = trailingSlash.ReplaceAllString(attributeSource, "")
				}

				if strings.EqualFold(tagName, "!doctype") {
					continue
				}
				parent := current()
				if parent == nil {
					continue
				}
				element := &Node{Type: ElementNode, TagName: tagName, Attrs: parseAttributes(attributeSource)}
				parent.Append(element)
				if !voidTags[strings.ToLower(tagName)] && !selfClosing {
					stack = append(stack, element)
				}

				// Everything inside a script or a style is text, however much of it looks like markup.
				if lower := strings.ToLower(tagName); lower == "script" || lower == "style" {
					if index := closingTagIndex(source, lower); index != -1 {
						appendText(source[:index])
						source = source[index:]
						continue
					}
				} else {
					continue
				}
			}
		}

		// Text runs to the next "<". A "<" right at the start is part of the text, because it has already failed to be a tag, and the search resumes after it.
		index := strings.Index(source, "<")
		offset := index
		if index == 0 {
			index = strings.Index(source[1:], "<")
			offset++
		}
		if index == -1 {
			appendText(source)
			source = ""
			continue
		}
		appendText(source[:offset])
		source = source[offset:]
	}

	return fragment
}

var trailingSlash = regexp.MustCompile(`\s*/\s*$`)

// closingTagIndex finds where a raw text element's closing tag starts, matching the name without regard to case.
func closingTagIndex(source, tagName string) int {
	needle := "</" + tagName
	lowered := strings.ToLower(source)
	return strings.Index(lowered, needle)
}

// parseAttributes reads a start tag's attributes. A name written with no value at all is present rather than empty, and a name written twice keeps the last value.
func parseAttributes(source string) []Attribute {
	var attrs []Attribute
	byName := map[string]int{}
	for _, match := range attrPattern.FindAllStringSubmatch(source, -1) {
		name := match[1]
		if name == "" {
			continue
		}
		attr := Attribute{Name: name}
		switch {
		case match[7] != "" || strings.HasPrefix(match[3], "'"):
			attr.Value = html.UnescapeString(match[7])
		case match[5] != "" || strings.HasPrefix(match[3], `"`):
			attr.Value = html.UnescapeString(match[5])
		case match[2] != "":
			attr.Value = html.UnescapeString(match[3])
		default:
			attr.Bare = true
		}
		if index, seen := byName[name]; seen {
			attrs[index] = attr
			continue
		}
		byName[name] = len(attrs)
		attrs = append(attrs, attr)
	}
	return attrs
}
