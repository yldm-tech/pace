package pdfdoc

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"
	"github.com/yldm-tech/pace/apps/api/internal/ydoc"
)

// The six faces the exporter registers. The same font matters rather than a similar one: where a line breaks depends on the width of every character.
var (
	//go:embed fonts/inter-regular.ttf
	interRegular []byte
	//go:embed fonts/inter-italic.ttf
	interItalic []byte
	//go:embed fonts/inter-semibold.ttf
	interSemibold []byte
	//go:embed fonts/inter-semibold-italic.ttf
	interSemiboldItalic []byte
	//go:embed fonts/inter-bold.ttf
	interBold []byte
	//go:embed fonts/inter-bold-italic.ttf
	interBoldItalic []byte
)

// pageSizes are the six the request may name, in points, upright.
var pageSizes = map[string]fpdf.SizeType{
	"A4":      {Wd: 595.28, Ht: 841.89},
	"A3":      {Wd: 841.89, Ht: 1190.55},
	"A2":      {Wd: 1190.55, Ht: 1683.78},
	"LETTER":  {Wd: 612, Ht: 792},
	"LEGAL":   {Wd: 612, Ht: 1008},
	"TABLOID": {Wd: 792, Ht: 1224},
}

// Options is everything the render needs that is not the document itself.
type Options struct {
	Title       string
	Author      string
	Subject     string
	PageSize    string
	Orientation string

	// UserMentions is a mentioned person's display name by their id. A mention of somebody not here is drawn as "@unknown", which is what the exporter does.
	UserMentions map[string]string
	// Images is an image's bytes by the asset id a node names. An image not here is drawn as a placeholder rather than left out.
	Images map[string][]byte
	// NoAssets leaves every image out, drawing a placeholder in its place.
	NoAssets bool
}

// Render draws the document and returns the PDF.
func Render(document ydoc.Node, options Options) ([]byte, error) {
	size, ok := pageSizes[strings.ToUpper(strings.TrimSpace(options.PageSize))]
	if !ok {
		size = pageSizes["A4"]
	}
	orientation := "P"
	if strings.EqualFold(options.Orientation, "landscape") {
		orientation = "L"
	}

	pdf := fpdf.NewCustom(&fpdf.InitType{OrientationStr: orientation, UnitStr: "pt", Size: size})
	pdf.SetAutoPageBreak(false, 0)
	registerFonts(pdf)

	if options.Title != "" {
		pdf.SetTitle(options.Title, true)
	}
	if options.Author != "" {
		pdf.SetAuthor(options.Author, true)
	}
	if options.Subject != "" {
		pdf.SetSubject(options.Subject, true)
	}

	page := style("page")
	renderer := &renderer{
		pdf:     pdf,
		options: options,
		padding: page.Padding,
	}
	renderer.width, renderer.height = pdf.GetPageSize()
	renderer.newPage()

	if options.Title != "" {
		title := style("title")
		renderer.drawText([]inlineRun{{text: options.Title}}, renderer.padding, renderer.contentWidth(), title, page)
		renderer.y += title.MarginBottom
	}

	for _, node := range document.Content {
		renderer.block(node, renderer.padding, renderer.contentWidth())
	}

	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		return nil, fmt.Errorf("pdfdoc: write the document: %w", err)
	}
	if pdf.Err() {
		return nil, fmt.Errorf("pdfdoc: %w", pdf.Error())
	}
	return out.Bytes(), nil
}

// The family and style names the faces are registered under. A PDF font style is a string of flags, so the six faces are three families each with an upright and an italic.
const (
	familyRegular  = "Inter"
	familySemibold = "InterSemibold"
	familyBold     = "InterBold"
	familyMono     = "Courier"
)

func registerFonts(pdf *fpdf.Fpdf) {
	pdf.AddUTF8FontFromBytes(familyRegular, "", interRegular)
	pdf.AddUTF8FontFromBytes(familyRegular, "I", interItalic)
	pdf.AddUTF8FontFromBytes(familySemibold, "", interSemibold)
	pdf.AddUTF8FontFromBytes(familySemibold, "I", interSemiboldItalic)
	pdf.AddUTF8FontFromBytes(familyBold, "", interBold)
	pdf.AddUTF8FontFromBytes(familyBold, "I", interBoldItalic)
}

// renderer flows blocks down the page.
type renderer struct {
	pdf     *fpdf.Fpdf
	options Options

	width, height float64
	padding       float64
	y             float64
}

func (r *renderer) contentWidth() float64 { return r.width - 2*r.padding }
func (r *renderer) bottom() float64       { return r.height - r.padding }

func (r *renderer) newPage() {
	r.pdf.AddPage()
	r.y = r.padding
}

// ensure starts a new page when what is about to be drawn would not fit on this one. A block taller than a whole page is drawn anyway rather than looped over forever.
func (r *renderer) ensure(height float64) {
	if r.y+height <= r.bottom() || r.y <= r.padding {
		return
	}
	r.newPage()
}

// inlineRun is a piece of text with the marks that were on it.
type inlineRun struct {
	text  string
	marks []ydoc.Mark
	// mention and hardBreak are the two inline nodes that are not text.
	mention   string
	hardBreak bool
}

// inlineRuns flattens a block's children into the runs its text is drawn from.
func (r *renderer) inlineRuns(nodes []ydoc.Node) []inlineRun {
	var runs []inlineRun
	for _, node := range nodes {
		switch node.Type {
		case "text":
			runs = append(runs, inlineRun{text: node.Text, marks: node.Marks})
		case "hardBreak":
			runs = append(runs, inlineRun{hardBreak: true})
		case "mention":
			runs = append(runs, inlineRun{mention: r.mentionName(node), marks: node.Marks})
		case "emoji":
			runs = append(runs, inlineRun{text: emojiText(node), marks: node.Marks})
		default:
			runs = append(runs, r.inlineRuns(node.Content)...)
		}
	}
	return runs
}

func emojiText(node ydoc.Node) string {
	name, _ := node.Attrs["name"].(string)
	if name == "" {
		return ""
	}
	return ":" + name + ":"
}

// mentionName is the person a mention names, and "@unknown" when nothing says who they are.
func (r *renderer) mentionName(node ydoc.Node) string {
	id, _ := node.Attrs["entity_identifier"].(string)
	if name, ok := r.options.UserMentions[id]; ok && name != "" {
		return "@" + name
	}
	return "@unknown"
}
