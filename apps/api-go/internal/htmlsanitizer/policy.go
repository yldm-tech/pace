// Package htmlsanitizer reproduces plane.utils.content_validator, which cleans
// stored editor HTML with nh3 (the Python binding for the Rust ammonia crate).
//
// ammonia parses with html5ever, filters the resulting tree, and serializes it
// again, so the output carries HTML5 tree construction: unclosed elements are
// closed, a table gains its tbody, and misnested inline elements are repaired.
// A token filter cannot do that, so this package parses with golang.org/x/net/html,
// which implements the same HTML5 algorithm, and serializes with the html5ever
// rules ammonia relies on.
package htmlsanitizer

// MaxSize is content_validator.MAX_SIZE: the 10MB cap shared with binary data.
const MaxSize = 10 * 1024 * 1024

// defaultAllowedTags is nh3.ALLOWED_TAGS, ammonia's default tag allowlist.
var defaultAllowedTags = []string{
	"a", "abbr", "acronym", "area", "article", "aside", "b", "bdi", "bdo",
	"blockquote", "br", "caption", "center", "cite", "code", "col", "colgroup",
	"data", "dd", "del", "details", "dfn", "div", "dl", "dt", "em", "figcaption",
	"figure", "footer", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hgroup",
	"hr", "i", "img", "ins", "kbd", "li", "map", "mark", "nav", "ol", "p", "pre",
	"q", "rp", "rt", "rtc", "ruby", "s", "samp", "small", "span", "strike",
	"strong", "sub", "summary", "sup", "table", "tbody", "td", "th", "thead",
	"time", "tr", "tt", "u", "ul", "var", "wbr",
}

// customTags is content_validator.CUSTOM_TAGS, the editor's own nodes.
var customTags = []string{"mention-component", "label", "input", "image-component"}

// genericAttributes is content_validator.ATTRIBUTES["*"].
var genericAttributes = []string{
	"class", "id", "title", "role", "aria-label", "aria-hidden", "style",
	"start", "type", "xmlns",
	"data-tight", "data-node-type", "data-type", "data-checked",
	"data-background-color", "data-text-color", "data-name", "data-id",
	"data-icon-name", "data-icon-color", "data-background",
	"data-emoji-unicode", "data-emoji-url", "data-logo-in-use", "data-block-type",
}

// tagAttributes is the rest of content_validator.ATTRIBUTES.
var tagAttributes = map[string][]string{
	"a":                 {"href", "target"},
	"image-component":   {"id", "width", "height", "aspectRatio", "aspectratio", "src", "alignment", "status"},
	"img":               {"width", "height", "aspectRatio", "aspectratio", "alignment", "src", "alt", "title"},
	"mention-component": {"id", "entity_identifier", "entity_name"},
	"th":                {"colspan", "rowspan", "colwidth", "background", "style"},
	"td":                {"colspan", "rowspan", "colwidth", "background", "textColor", "textcolor", "style"},
	"tr":                {"background", "textColor", "textcolor", "style"},
	"pre":               {"language"},
	"code":              {"language", "spellcheck"},
	"input":             {"type", "checked"},
}

// safeProtocols is content_validator.SAFE_PROTOCOLS.
var safeProtocols = []string{"http", "https", "mailto", "tel"}

// contentDroppingTags is ammonia's clean_content_tags default: these elements
// are removed together with their text instead of being unwrapped.
var contentDroppingTags = []string{"script", "style"}

// isolatedContentTags parse their children into a separate fragment under the
// HTML5 algorithm, so ammonia never sees that content and it does not survive
// the unwrapping a disallowed tag would otherwise get.
var isolatedContentTags = []string{"template"}

// urlAttributes are the attributes ammonia runs the scheme check over. Probing
// nh3 shows it checks only these two; every other attribute keeps its value.
var urlAttributes = []string{"href", "src"}

// linkRel is ammonia's link_rel default, written onto every anchor.
const linkRel = "noopener noreferrer"

// Policy is the resolved allowlist a Cleaner applies.
type Policy struct {
	tags            map[string]struct{}
	genericAttrs    map[string]struct{}
	tagAttrs        map[string]map[string]struct{}
	schemes         map[string]struct{}
	dropContentTags map[string]struct{}
	isolatedTags    map[string]struct{}
	urlAttrs        map[string]struct{}
	linkRel         string
}

// PlanePolicy builds the policy validate_html_content passes to nh3.clean.
func PlanePolicy() *Policy {
	policy := &Policy{
		tags:            set(defaultAllowedTags, customTags),
		genericAttrs:    set(genericAttributes),
		tagAttrs:        make(map[string]map[string]struct{}, len(tagAttributes)),
		schemes:         set(safeProtocols),
		dropContentTags: set(contentDroppingTags),
		isolatedTags:    set(isolatedContentTags),
		urlAttrs:        set(urlAttributes),
		linkRel:         linkRel,
	}
	for tag, attributes := range tagAttributes {
		policy.tagAttrs[tag] = set(attributes)
	}
	return policy
}

func (policy *Policy) allowsTag(name string) bool {
	_, ok := policy.tags[name]
	return ok
}

func (policy *Policy) dropsContent(name string) bool {
	_, ok := policy.dropContentTags[name]
	return ok
}

func (policy *Policy) isolatesContent(name string) bool {
	_, ok := policy.isolatedTags[name]
	return ok
}

func (policy *Policy) allowsAttribute(tag, attribute string) bool {
	if _, ok := policy.genericAttrs[attribute]; ok {
		return true
	}
	_, ok := policy.tagAttrs[tag][attribute]
	return ok
}

func (policy *Policy) checksURL(attribute string) bool {
	_, ok := policy.urlAttrs[attribute]
	return ok
}

func (policy *Policy) allowsScheme(scheme string) bool {
	_, ok := policy.schemes[scheme]
	return ok
}

func set(groups ...[]string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, group := range groups {
		for _, value := range group {
			result[value] = struct{}{}
		}
	}
	return result
}
