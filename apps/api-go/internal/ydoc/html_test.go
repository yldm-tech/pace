package ydoc

import (
	"strings"
	"testing"
)

func TestHTMLRejectsUnknownTypes(t *testing.T) {
	if _, err := HTML(Node{Type: "doc", Content: []Node{{Type: "somethingElse"}}}); err == nil || !strings.Contains(err.Error(), "somethingElse") {
		t.Fatalf("err = %v, want one naming the unknown node type", err)
	}
	unknownMark := Node{Type: "doc", Content: []Node{{Type: "text", Text: "x", Marks: []Mark{{Type: "sparkle"}}}}}
	if _, err := HTML(unknownMark); err == nil || !strings.Contains(err.Error(), "sparkle") {
		t.Fatalf("err = %v, want one naming the unknown mark type", err)
	}
}

// TestMergeAttributes covers the three rules Tiptap merges by, because the output's attribute order and its class list both come out of them.
func TestMergeAttributes(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		input []*attrList
		want  string
	}{
		{
			name:  "later wins",
			input: []*attrList{newAttrs("a", "one"), newAttrs("a", "two")},
			want:  `<x a="two"></x>`,
		},
		{
			name:  "order comes from where a name was first seen",
			input: []*attrList{newAttrs("a", "one", "b", "two"), newAttrs("b", "three", "c", "four")},
			want:  `<x a="one" b="three" c="four"></x>`,
		},
		{
			name:  "classes accumulate without repeating",
			input: []*attrList{newAttrs("class", "one two"), newAttrs("class", "two three")},
			want:  `<x class="one two three"></x>`,
		},
		{
			name:  "an empty later class leaves the earlier one alone",
			input: []*attrList{newAttrs("class", "one"), newAttrs("class", nil)},
			want:  `<x class="one"></x>`,
		},
		{
			name:  "an empty earlier value is simply replaced",
			input: []*attrList{newAttrs("a", nil), newAttrs("a", "two")},
			want:  `<x a="two"></x>`,
		},
		{
			name:  "a true value is a bare attribute and a false one is nothing",
			input: []*attrList{newAttrs("checked", true, "disabled", false)},
			want:  `<x checked></x>`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			rendered, _, err := renderParts(element("x", mergeAttributes(testCase.input...)))
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if rendered != testCase.want {
				t.Errorf("rendered = %s, want %s", rendered, testCase.want)
			}
		})
	}
}

// TestMergeStyles pins the style merge even though no style survives to the output, because the merge is what the extensions write and the drop happens later.
func TestMergeStyles(t *testing.T) {
	merged := mergeAttributes(newAttrs("style", "color: red; font-weight: bold"), newAttrs("style", "color: blue"))
	if got, want := asString(merged.get("style")), "color: blue; font-weight: bold"; got != want {
		t.Errorf("style = %q, want %q", got, want)
	}
}

func TestStyleNeverReachesTheOutput(t *testing.T) {
	rendered, _, err := renderParts(element("p", newAttrs("class", "a", "style", "text-align: center")))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if rendered != `<p class="a"></p>` {
		t.Errorf("rendered = %s, want the style dropped", rendered)
	}
}

func TestEscapeHTML(t *testing.T) {
	got := escapeHTML("a & b < c > d \"e\" 'f'   ­")
	want := "a &amp; b &lt; c &gt; d &quot;e&quot; &apos;f&apos; &nbsp; &shy;"
	if got != want {
		t.Errorf("escapeHTML = %q, want %q", got, want)
	}
}

// TestSelfClosingTagsHaveNoClosingTag covers the one place zeed-dom and an XML serialiser disagree: a break is written open and left that way.
func TestSelfClosingTagsHaveNoClosingTag(t *testing.T) {
	rendered, _, err := renderParts(element("br", newAttrs()))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if rendered != "<br>" {
		t.Errorf("rendered = %s, want <br>", rendered)
	}
}

func TestIsDangerousHref(t *testing.T) {
	for _, dangerous := range []string{
		"javascript:alert(1)",
		"JaVaScRiPt:alert(1)",
		"\tjavascript:alert(1)",
		"  \n javascript:alert(1)",
		"java\nscript:alert(1)",
		"javascript:alert(1)",
		"data:text/html,<script>",
		"vbscript:msgbox",
	} {
		if !isDangerousHref(dangerous) {
			t.Errorf("isDangerousHref(%q) = false, want true", dangerous)
		}
	}
	for _, safe := range []string{
		"https://example.test/",
		"/relative/path",
		"mailto:someone@example.test",
		"",
		"notjavascript:x",
	} {
		if isDangerousHref(safe) {
			t.Errorf("isDangerousHref(%q) = true, want false", safe)
		}
	}
}

// TestEmojiTableIsLoaded guards the embedded table against arriving empty or mangled.
func TestEmojiTableIsLoaded(t *testing.T) {
	if len(emojiByShortcode) < 1000 {
		t.Fatalf("the emoji table holds %d entries, which is too few to be the real one", len(emojiByShortcode))
	}
	if emojiByShortcode["tada"] != "🎉" {
		t.Errorf("tada = %q, want 🎉", emojiByShortcode["tada"])
	}
}
