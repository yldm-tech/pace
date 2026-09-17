package ydoc

import (
	"fmt"
	"strings"
)

// attrList is a set of HTML attributes that remembers the order they were added in, because that is the order they are written out in and the editor's output is compared byte for byte.
//
// A value is a string, a bool or nil. True renders as a bare attribute name, false and nil render as nothing at all.
type attrList struct {
	names  []string
	values map[string]any
}

func newAttrs(pairs ...any) *attrList {
	list := &attrList{values: map[string]any{}}
	for i := 0; i+1 < len(pairs); i += 2 {
		list.set(pairs[i].(string), pairs[i+1])
	}
	return list
}

func (a *attrList) set(name string, value any) {
	if _, seen := a.values[name]; !seen {
		a.names = append(a.names, name)
	}
	a.values[name] = value
}

func (a *attrList) get(name string) any {
	if a == nil {
		return nil
	}
	return a.values[name]
}

// mergeAttributes is Tiptap's mergeAttributes. Later objects win, except for class, where the classes are concatenated without repeating one already present, and style, where the declarations are merged by property with the later value winning and the property order taken from where each was first seen.
func mergeAttributes(objects ...*attrList) *attrList {
	merged := &attrList{values: map[string]any{}}
	for _, object := range objects {
		if object == nil {
			continue
		}
		for _, name := range object.names {
			value := object.values[name]
			existing, present := merged.values[name]
			if !present || existing == nil || existing == "" || existing == false {
				merged.set(name, value)
				continue
			}
			switch name {
			case "class":
				merged.set(name, mergeClasses(asString(existing), asString(value)))
			case "style":
				merged.set(name, mergeStyles(asString(existing), asString(value)))
			default:
				merged.set(name, value)
			}
		}
	}
	return merged
}

func mergeClasses(existing, incoming string) string {
	existingClasses := strings.Split(existing, " ")
	seen := make(map[string]bool, len(existingClasses))
	for _, class := range existingClasses {
		seen[class] = true
	}
	out := existingClasses
	if incoming != "" {
		for _, class := range strings.Split(incoming, " ") {
			if !seen[class] {
				out = append(out, class)
				seen[class] = true
			}
		}
	}
	return strings.Join(out, " ")
}

func mergeStyles(existing, incoming string) string {
	var order []string
	values := map[string]string{}
	add := func(declarations string) {
		for _, declaration := range strings.Split(declarations, ";") {
			declaration = strings.TrimSpace(declaration)
			if declaration == "" {
				continue
			}
			property, value, _ := strings.Cut(declaration, ":")
			property = strings.TrimSpace(property)
			if _, seen := values[property]; !seen {
				order = append(order, property)
			}
			values[property] = strings.TrimSpace(value)
		}
	}
	add(existing)
	add(incoming)
	out := make([]string, 0, len(order))
	for _, property := range order {
		out = append(out, fmt.Sprintf("%s: %s", property, values[property]))
	}
	return strings.Join(out, "; ")
}

func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

// spec is ProseMirror's DOMOutputSpec for the shapes this schema produces: an element with attributes and children, a literal string, or the content hole.
type spec struct {
	tag      string
	attrs    *attrList
	children []spec

	text   string
	isText bool
	isHole bool
}

func element(tag string, attrs *attrList, children ...spec) spec {
	return spec{tag: tag, attrs: attrs, children: children}
}

func hole() spec            { return spec{isHole: true} }
func literal(s string) spec { return spec{text: s, isText: true} }

// selfClosing is zeed-dom's list: these tags are written with no closing tag and no children.
var selfClosing = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "keygen": true, "link": true, "meta": true, "param": true, "source": true,
	"track": true, "wbr": true, "command": true,
}

// escapeHTML is zeed-dom's, applied to both text and attribute values. It differs from Go's html package in three places that show up in real pages: an apostrophe becomes &apos; rather than &#39;, a non-breaking space becomes &nbsp;, and a soft hyphen becomes &shy;.
var htmlEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	"'", "&apos;",
	`"`, "&quot;",
	" ", "&nbsp;",
	"­", "&shy;",
)

func escapeHTML(text string) string { return htmlEscaper.Replace(text) }

// renderParts writes a spec out and reports where the content hole was, so the caller can splice the children in at that point. holeAt is -1 when the spec has no hole.
func renderParts(s spec) (rendered string, holeAt int, err error) {
	var out strings.Builder
	holeAt = -1
	if err := writeSpec(&out, s, &holeAt); err != nil {
		return "", -1, err
	}
	return out.String(), holeAt, nil
}

// renderMarkParts is renderParts for a mark. A mark spec with no hole still receives the content: ProseMirror leaves the cursor pointing at the element itself, so whatever follows lands inside it, after anything the spec already put there.
func renderMarkParts(s spec) (rendered string, holeAt int, err error) {
	rendered, holeAt, err = renderParts(s)
	if err != nil || holeAt >= 0 {
		return rendered, holeAt, err
	}
	if s.isText || s.isHole || selfClosing[s.tag] {
		return rendered, -1, nil
	}
	return rendered, len(rendered) - len("</"+s.tag+">"), nil
}

func writeSpec(out *strings.Builder, s spec, holeAt *int) error {
	switch {
	case s.isHole:
		if *holeAt >= 0 {
			return fmt.Errorf("ydoc: multiple content holes in one spec")
		}
		*holeAt = out.Len()
		return nil
	case s.isText:
		out.WriteString(escapeHTML(s.text))
		return nil
	}

	out.WriteString("<")
	out.WriteString(s.tag)
	if s.attrs != nil {
		for _, name := range s.attrs.names {
			// A style attribute never survives. ProseMirror assigns it through the element's style property rather than as an attribute, and the DOM this runs against answers that property with a throwaway object, so the declaration is written somewhere nothing reads. Text alignment, table row colours and cell backgrounds all disappear here.
			if name == "style" {
				continue
			}
			switch value := s.attrs.values[name]; value {
			case nil, false:
			case true:
				out.WriteString(" ")
				out.WriteString(name)
			default:
				out.WriteString(" ")
				out.WriteString(name)
				out.WriteString(`="`)
				out.WriteString(escapeHTML(asString(value)))
				out.WriteString(`"`)
			}
		}
	}
	out.WriteString(">")
	// Nothing is written inside a self-closing tag, and no closing tag follows it.
	if selfClosing[s.tag] {
		return nil
	}
	for _, child := range s.children {
		if child.isHole && len(s.children) != 1 {
			return fmt.Errorf("ydoc: content hole must be the only child of its parent")
		}
		if err := writeSpec(out, child, holeAt); err != nil {
			return err
		}
	}
	out.WriteString("</")
	out.WriteString(s.tag)
	out.WriteString(">")
	return nil
}
