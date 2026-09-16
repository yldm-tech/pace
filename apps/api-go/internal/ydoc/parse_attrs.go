package ydoc

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/yldm-tech/pace/apps/api-go/internal/vdom"
)

// fromString is how Tiptap reads a plain attribute: a string that is entirely a number becomes one, "true" and "false" become booleans, and everything else is left alone. It is why a table cell's colspan comes back as a number rather than as "1".
var numericString = regexp.MustCompile(`^[+-]?(?:\d*\.)?\d+$`)

func fromString(value string) any {
	if numericString.MatchString(value) {
		if number, err := strconv.ParseFloat(value, 64); err == nil {
			return number
		}
	}
	switch value {
	case "true":
		return true
	case "false":
		return false
	}
	return value
}

// attributeParsers are the attributes whose reader is code rather than the element's attribute of the same name. Each is keyed by the type that declares it, and the JavaScript each one is a port of is recorded in parse_rules.json beside the entry.
//
// A reader returning absent leaves the attribute out, and the schema's default then applies.
var attributeParsers = map[string]map[string]func(*vdom.Node) (any, bool){
	"taskItem": {
		// Checked either way round: the attribute written bare and the attribute written "true" both mean ticked.
		"checked": func(element *vdom.Node) (any, bool) {
			value, present := element.Attr("data-checked")
			if !present {
				return false, true
			}
			return value == "" || value == "true", true
		},
	},
	"customColor": {
		"color":           dataAttribute("data-text-color"),
		"backgroundColor": dataAttribute("data-background-color"),
	},
	"paragraph": {"textAlign": textAlignAttribute},
	"heading":   {"textAlign": textAlignAttribute},
	"orderedList": {
		// A list with no start attribute starts at one, which is the value the renderer then leaves out again.
		"start": func(element *vdom.Node) (any, bool) {
			value, present := element.Attr("start")
			if !present {
				return float64(1), true
			}
			number, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				// parseInt of something that is not a number is NaN, which Tiptap keeps rather than discards.
				return nil, false
			}
			return float64(number), true
		},
		"type": dataAttribute("type"),
	},
	"emoji": {
		"name": func(element *vdom.Node) (any, bool) {
			value, present := element.Attr("data-name")
			if !present {
				return nil, false
			}
			return value, true
		},
	},
	"codeBlock":   {"language": codeBlockLanguage},
	"tableHeader": {"colwidth": colwidthAttribute},
	"tableCell":   {"colwidth": colwidthAttribute},
}

// dataAttribute reads one attribute and treats an absent one as nothing at all, which is what returning null from a reader does.
func dataAttribute(name string) func(*vdom.Node) (any, bool) {
	return func(element *vdom.Node) (any, bool) {
		value, present := element.Attr(name)
		if !present {
			return nil, false
		}
		return value, true
	}
}

// textAlignAttribute takes the alignment off the element's own style, and only when it is one of the three the editor offers. Anything else falls back to the default, which is nothing.
func textAlignAttribute(element *vdom.Node) (any, bool) {
	alignment := element.StyleValue("textAlign")
	switch alignment {
	case "left", "center", "right":
		return alignment, true
	}
	return nil, false
}

// colwidthAttribute reads a cell's width as the one-element list the schema stores it as.
func colwidthAttribute(element *vdom.Node) (any, bool) {
	value, present := element.Attr("colwidth")
	if !present || value == "" {
		return nil, false
	}
	number, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(value, ",", 2)[0]))
	if err != nil {
		return nil, false
	}
	return []any{float64(number)}, true
}

// codeBlockLanguage never finds one.
//
// The reader looks for a class on the element's first child element, and reaches it through a property the DOM the editor runs on does not have. Reading an absent property gives nothing, the list of classes is empty, and no language is ever found — so a code block whose HTML says language-go comes back with no language at all, and the highlighting is lost the first time a page is read back from its HTML.
//
// Reproduced rather than corrected, and written out rather than left as a stub, so that the day the DOM grows the property this starts working the same way the editor does.
func codeBlockLanguage(element *vdom.Node) (any, bool) {
	first := firstElementChild(element)
	if first == nil {
		return nil, false
	}
	for _, class := range first.Classes() {
		if strings.HasPrefix(class, "language-") {
			return strings.TrimPrefix(class, "language-"), true
		}
	}
	return nil, false
}

// firstElementChild is the property the editor's DOM is missing, and answering nothing is what makes the language reader find nothing.
func firstElementChild(*vdom.Node) *vdom.Node { return nil }

// ruleAttrs works out the attributes a rule produces for an element, the way Tiptap's wrapper does: the rule's own answer first, then every attribute the type declares read off the element, with the second winning.
//
// The rule's own answer may also be a refusal, and a refused rule is passed over for the next one that matches.
func ruleAttrs(rule *ParseRule, element *vdom.Node) (map[string]any, bool) {
	attrs := map[string]any{}
	if base, ok := baseGetAttrs(rule, element); ok {
		for name, value := range base {
			attrs[name] = value
		}
	} else {
		return nil, false
	}
	for _, reader := range rule.Reads {
		typeName := rule.Node
		if typeName == "" {
			typeName = rule.Mark
		}
		if parser, ok := attributeParsers[typeName][reader.Name]; ok {
			if value, present := parser(element); present {
				attrs[reader.Name] = value
			}
			continue
		}
		if value, present := element.Attr(reader.Name); present {
			attrs[reader.Name] = fromString(value)
		}
	}
	return attrs, true
}

// boldWeights are the font weights the bold mark recognises in a style declaration.
var boldWeights = regexp.MustCompile(`^(bold(er)?|[5-9]\d{2,})$`)

// tagRuleAttrs is every rule that carries its own answer, keyed by the type that declares it and the rule's position among that type's rules — which is exactly how parse_rules.json records the JavaScript each one is a port of.
//
// A rule answering false is refused, and the element is offered to the next rule that matches it.
var tagRuleAttrs = map[string]map[int]func(*vdom.Node) (map[string]any, bool){
	"bold": {
		// A <b> is bold unless it says otherwise.
		1: func(element *vdom.Node) (map[string]any, bool) {
			return nil, element.StyleValue("fontWeight") != "normal"
		},
	},
	"italic": {
		1: func(element *vdom.Node) (map[string]any, bool) {
			return nil, element.StyleValue("fontStyle") != "normal"
		},
	},
	"textStyle": {
		// A span with no style attribute is not a text style at all.
		0: func(element *vdom.Node) (map[string]any, bool) { return nil, element.HasAttr("style") },
	},
	"customColor": {
		// Both of these look like they only match a span carrying a colour, and neither does: they are written to answer null when the attribute is there, and the DOM the editor runs on answers undefined rather than null when it is not — so both answers are read as "matched with no attributes" and every span becomes a colour mark.
		//
		// Reproduced rather than corrected. A plain span in a page's HTML comes back carrying a customColor mark with both colours null, and correcting it here would mean reading pages differently from the editor that wrote them.
		0: func(*vdom.Node) (map[string]any, bool) { return nil, true },
		1: func(*vdom.Node) (map[string]any, bool) { return nil, true },
	},
	"link": {
		// An href that would run rather than navigate makes the rule refuse, so the text survives and the link does not.
		0: func(element *vdom.Node) (map[string]any, bool) {
			return nil, !isDangerousHref(element.AttrOr("href", ""))
		},
	},
}

// baseGetAttrs is the rule's own answer, before the attributes the type declares are read.
func baseGetAttrs(rule *ParseRule, element *vdom.Node) (map[string]any, bool) {
	if answer, ok := tagRuleAttrs[rule.Owner][rule.OwnerIndex]; ok {
		return answer(element)
	}
	// A rule carrying fixed attributes hands them over unchanged, which is how each heading tag names its own level.
	return rule.Attrs, true
}

// styleRuleAttrs is the same, for the rules that match a style declaration rather than an element. Their answer is worked out from the declaration's value.
var styleRuleAttrs = map[string]map[int]func(string) (map[string]any, bool){
	"bold": {
		3: func(value string) (map[string]any, bool) { return nil, boldWeights.MatchString(value) },
	},
	"strike": {
		3: func(value string) (map[string]any, bool) { return nil, strings.Contains(value, "line-through") },
	},
	"underline": {
		1: func(value string) (map[string]any, bool) { return nil, strings.Contains(value, "underline") },
	},
}

func styleAttrs(rule *ParseRule, value string) (map[string]any, bool) {
	if answer, ok := styleRuleAttrs[rule.Owner][rule.OwnerIndex]; ok {
		return answer(value)
	}
	return rule.Attrs, true
}
