package pdfdoc

import (
	"strings"

	"github.com/yldm-tech/pace/apps/api/internal/ydoc"
)

// piece is one word or space of one run, with the settings it is drawn with. A line is a list of these.
type piece struct {
	text    string
	width   float64
	space   bool
	mention bool

	family     string
	fontStyle  string
	size       float64
	colour     string
	background string
	underline  bool
	strike     bool
	link       string
}

// drawText lays out a run of inline content and draws it, advancing the cursor past the last line.
//
// The block's own style decides the size and weight; the page's decides the line height and the colour when the block says nothing. Each run's marks are applied on top, which is where bold, a link's colour and inline code's typeface come from.
func (r *renderer) drawText(runs []inlineRun, left, width float64, block, page Style) {
	pieces := r.piecesFor(runs, block, page)
	if len(pieces) == 0 {
		return
	}

	size := block.FontSize
	if size == 0 {
		size = page.FontSize
	}
	lineHeight := size * page.LineHeight

	for _, line := range breakLines(pieces, width) {
		r.ensure(lineHeight)
		x := left
		baseline := r.y + size
		for _, p := range line {
			if p.background != "" && !p.space {
				red, green, blue := rgb(p.background)
				r.pdf.SetFillColor(red, green, blue)
				r.pdf.Rect(x, r.y, p.width, lineHeight*0.85, "F")
			}
			if !p.space {
				red, green, blue := rgb(p.colour)
				r.pdf.SetTextColor(red, green, blue)
				r.pdf.SetFont(p.family, p.fontStyle, p.size)
				r.pdf.Text(x, baseline, p.text)
				if p.underline {
					r.pdf.SetDrawColor(red, green, blue)
					r.pdf.SetLineWidth(0.5)
					r.pdf.Line(x, baseline+1.5, x+p.width, baseline+1.5)
				}
				if p.strike {
					r.pdf.SetDrawColor(red, green, blue)
					r.pdf.SetLineWidth(0.5)
					r.pdf.Line(x, baseline-p.size*0.3, x+p.width, baseline-p.size*0.3)
				}
				if p.link != "" {
					r.pdf.LinkString(x, r.y, p.width, lineHeight, p.link)
				}
			}
			x += p.width
		}
		r.y += lineHeight
	}
}

// piecesFor turns the runs into measured words and spaces.
func (r *renderer) piecesFor(runs []inlineRun, block, page Style) []piece {
	var pieces []piece
	for _, run := range runs {
		if run.hardBreak {
			pieces = append(pieces, piece{text: "\n"})
			continue
		}
		text := drawable(run.text)
		settings := r.settingsFor(run, block, page)
		if run.mention != "" {
			text = drawable(run.mention)
			settings.mention = true
		}
		if text == "" {
			continue
		}
		r.pdf.SetFont(settings.family, settings.fontStyle, settings.size)
		for _, word := range splitKeepingSpaces(text) {
			measured := settings
			measured.text = word
			measured.space = strings.TrimSpace(word) == ""
			measured.width = r.pdf.GetStringWidth(word)
			pieces = append(pieces, measured)
		}
	}
	return pieces
}

// settingsFor works out how one run is drawn: the block's own settings, then each of its marks on top.
func (r *renderer) settingsFor(run inlineRun, block, page Style) piece {
	settings := piece{
		family:    familyRegular,
		fontStyle: "",
		size:      block.FontSize,
		colour:    block.Color,
	}
	if settings.size == 0 {
		settings.size = page.FontSize
	}
	if settings.colour == "" {
		settings.colour = page.Color
	}
	// A block whose own weight is heavy is drawn in the face that weight names, which is what a heading is.
	switch weight := block.Weight(); {
	case weight >= 700:
		settings.family = familyBold
	case weight >= 600:
		settings.family = familySemibold
	}

	for _, mark := range run.marks {
		switch mark.Type {
		case "bold":
			settings.family = familyBold
		case "italic":
			settings.fontStyle = "I"
		case "underline":
			settings.underline = true
		case "strike":
			settings.strike = true
		case "code":
			code := style("codeInline")
			settings.family = familyMono
			settings.size = code.FontSize
			settings.colour = code.Color
			settings.background = code.BackgroundColor
		case "link":
			link := style("link")
			settings.colour = link.Color
			settings.underline = true
			if href, ok := mark.Attrs["href"].(string); ok {
				settings.link = href
			}
		case "customColor":
			if colour, ok := mark.Attrs["color"].(string); ok && colour != "" {
				if resolved := editorColour(colour, "EDITOR_TEXT_COLORS"); resolved != "" {
					settings.colour = resolved
				}
			}
			if background, ok := mark.Attrs["backgroundColor"].(string); ok && background != "" {
				if resolved := editorColour(background, "EDITOR_BACKGROUND_COLORS"); resolved != "" {
					settings.background = resolved
				}
			}
		}
	}

	if run.mention != "" {
		mention := style("mention")
		settings.colour = mention.Color
		settings.background = mention.BackgroundColor
	}
	return settings
}

// editorColour looks a named colour up in the editor's palette, and answers nothing for a name the palette does not carry — which is what leaves a colour the editor does not offer undrawn.
func editorColour(name, group string) string {
	if palette, ok := styles.Colors[group]; ok {
		if colour, ok := palette[name]; ok {
			return colour
		}
	}
	return ""
}

// drawable is the text as the PDF writer will take it.
//
// The writer keeps one entry per character code in a table of sixty-five thousand, so a character outside that range — an emoji, most of them — walks off the end of it and brings the process down. The font has no glyph for one either, so nothing is lost by replacing it; what is gained is that a page with an emoji in it exports rather than failing.
func drawable(text string) string {
	if !strings.ContainsFunc(text, func(r rune) bool { return r > 0xffff }) {
		return text
	}
	var out strings.Builder
	for _, r := range text {
		if r > 0xffff {
			out.WriteRune('\uFFFD')
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

// splitKeepingSpaces cuts text into words and the runs of space between them, so that the spacing survives a line break decision.
func splitKeepingSpaces(text string) []string {
	var out []string
	var current strings.Builder
	inSpace := false
	for i, r := range text {
		space := r == ' ' || r == '\t'
		if i > 0 && space != inSpace {
			out = append(out, current.String())
			current.Reset()
		}
		inSpace = space
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		out = append(out, current.String())
	}
	return out
}

// breakLines fills lines up to the width, breaking at a space. A word wider than the whole line is given a line of its own rather than cut.
func breakLines(pieces []piece, width float64) [][]piece {
	var lines [][]piece
	var line []piece
	used := 0.0

	flush := func() {
		// The spaces at the end of a line are not drawn and do not count.
		for len(line) > 0 && line[len(line)-1].space {
			line = line[:len(line)-1]
		}
		lines = append(lines, line)
		line = nil
		used = 0
	}

	for _, p := range pieces {
		if p.text == "\n" {
			flush()
			continue
		}
		if used+p.width > width && len(line) > 0 {
			flush()
			// A space at the start of a new line is dropped.
			if p.space {
				continue
			}
		}
		line = append(line, p)
		used += p.width
	}
	if len(line) > 0 {
		flush()
	}
	if len(lines) == 0 {
		lines = append(lines, nil)
	}
	return lines
}

// AssetIDs is every image a document names that is stored rather than linked, which is what the service has to fetch before a page can be drawn.
func AssetIDs(node ydoc.Node) []string {
	var found []string
	seen := map[string]bool{}

	var walk func(ydoc.Node)
	walk = func(current ydoc.Node) {
		if current.Type == "image" || current.Type == "imageComponent" {
			if source, ok := current.Attrs["src"].(string); ok && source != "" {
				if !strings.HasPrefix(source, "http") && !strings.HasPrefix(source, "data:") && !seen[source] {
					seen[source] = true
					found = append(found, source)
				}
			}
		}
		for _, child := range current.Content {
			walk(child)
		}
	}
	walk(node)
	return found
}
