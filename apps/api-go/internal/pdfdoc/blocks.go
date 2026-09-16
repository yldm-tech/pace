package pdfdoc

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strconv"
	"strings"

	"github.com/go-pdf/fpdf"
	"github.com/yldm-tech/pace/apps/api-go/internal/ydoc"
)

// block draws one node at the given left edge, within the given width, advancing the cursor past it.
func (r *renderer) block(node ydoc.Node, left, width float64) {
	switch node.Type {
	case "paragraph":
		r.paragraph(node, left, width)
	case "heading":
		r.heading(node, left, width)
	case "blockquote":
		r.blockquote(node, left, width)
	case "codeBlock":
		r.codeBlock(node, left, width)
	case "bulletList", "orderedList":
		r.list(node, left, width)
	case "taskList":
		r.taskList(node, left, width)
	case "table":
		r.table(node, left, width)
	case "horizontalRule":
		r.horizontalRule(left, width)
	case "image", "imageComponent":
		r.image(node, left, width)
	case "calloutComponent":
		r.callout(node, left, width)
	case "issue-embed-component":
		// The exporter has no renderer for one, so it draws nothing.
	default:
		// Anything else contributes its children, which is what an unknown node does.
		for _, child := range node.Content {
			r.block(child, left, width)
		}
	}
}

func (r *renderer) paragraph(node ydoc.Node, left, width float64) {
	wrapper := style("paragraphWrapper")
	page := style("page")
	runs := r.inlineRuns(node.Content)
	if len(runs) == 0 {
		// An empty paragraph still takes a line's worth of space, which is what an empty line in a page looks like.
		r.ensure(page.FontSize * page.LineHeight)
		r.y += page.FontSize*page.LineHeight + wrapper.MarginBottom
		return
	}
	r.drawText(runs, left, width, Style{}, page)
	r.y += wrapper.MarginBottom
}

func (r *renderer) heading(node ydoc.Node, left, width float64) {
	level := 1
	switch value := node.Attrs["level"].(type) {
	case float64:
		level = int(value)
	case int:
		level = value
	}
	if level < 1 || level > 6 {
		level = 1
	}
	heading := style(fmt.Sprintf("heading%d", level))
	r.y += heading.MarginTop
	r.drawText(r.inlineRuns(node.Content), left, width, heading, style("page"))
	r.y += heading.MarginBottom
}

func (r *renderer) blockquote(node ydoc.Node, left, width float64) {
	quote := style("blockquote")
	r.y += quote.MarginVertical

	start := r.y
	startPage := r.pdf.PageNo()
	inner := left + quote.BorderLeftWidth + quote.PaddingLeft
	for _, child := range node.Content {
		r.block(child, inner, width-(inner-left))
	}

	// The rule beside a quote is drawn after its content, because only then is its height known. A quote that ran onto another page gets a rule on each.
	red, green, blue := rgb(quote.BorderLeftColor)
	r.pdf.SetDrawColor(red, green, blue)
	r.pdf.SetLineWidth(quote.BorderLeftWidth)
	x := left + quote.BorderLeftWidth/2
	if r.pdf.PageNo() == startPage {
		r.pdf.Line(x, start, x, r.y)
	} else {
		r.pdf.Line(x, r.padding, x, r.y)
	}
	r.y += quote.MarginVertical
}

func (r *renderer) codeBlock(node ydoc.Node, left, width float64) {
	block := style("codeBlock")
	r.y += block.MarginVertical

	lines := strings.Split(drawable(node.TextContent()), "\n")
	lineHeight := block.FontSize * style("page").LineHeight
	height := float64(len(lines))*lineHeight + 2*block.Padding
	r.ensure(height)

	red, green, blue := rgb(block.BackgroundColor)
	r.pdf.SetFillColor(red, green, blue)
	r.pdf.Rect(left, r.y, width, height, "F")

	red, green, blue = rgb(block.Color)
	r.pdf.SetTextColor(red, green, blue)
	r.pdf.SetFont(familyMono, "", block.FontSize)
	y := r.y + block.Padding + block.FontSize
	for _, line := range lines {
		r.pdf.Text(left+block.Padding, y, line)
		y += lineHeight
	}
	r.y += height + block.MarginVertical
}

func (r *renderer) list(node ydoc.Node, left, width float64) {
	list := style("bulletList")
	if node.Type == "orderedList" {
		list = style("orderedList")
	}
	r.y += list.MarginVertical

	start := 1
	if value, ok := node.Attrs["start"].(float64); ok {
		start = int(value)
	}

	item := style("listItem")
	page := style("page")
	for index, child := range node.Content {
		marker := "•"
		if node.Type == "orderedList" {
			marker = strconv.Itoa(start+index) + "."
		}
		r.pdf.SetFont(familyRegular, "", page.FontSize)
		markerWidth := r.pdf.GetStringWidth(marker)
		indent := markerWidth + item.Gap

		r.ensure(page.FontSize * page.LineHeight)
		red, green, blue := rgb(page.Color)
		r.pdf.SetTextColor(red, green, blue)
		r.pdf.Text(left, r.y+page.FontSize, marker)

		for _, grandchild := range child.Content {
			r.block(grandchild, left+indent, width-indent-item.PaddingRight)
		}
		r.y += item.MarginBottom
	}
	r.y += list.MarginVertical
}

func (r *renderer) taskList(node ydoc.Node, left, width float64) {
	list := style("taskList")
	r.y += list.MarginVertical

	item := style("taskItem")
	box := style("taskCheckbox")
	checked := style("taskCheckboxChecked")
	page := style("page")

	for _, child := range node.Content {
		isChecked, _ := child.Attrs["checked"].(bool)
		indent := box.Width + item.Gap

		r.ensure(page.FontSize * page.LineHeight)
		top := r.y + box.MarginTop
		if isChecked {
			red, green, blue := rgb(checked.BackgroundColor)
			r.pdf.SetFillColor(red, green, blue)
			red, green, blue = rgb(checked.BorderColor)
			r.pdf.SetDrawColor(red, green, blue)
			r.pdf.SetLineWidth(box.BorderWidth)
			r.pdf.Rect(left, top, box.Width, box.Height, "FD")
			// The tick, drawn as two strokes rather than as a glyph, because the box is twelve points across.
			r.pdf.SetDrawColor(255, 255, 255)
			r.pdf.SetLineWidth(1.2)
			r.pdf.Line(left+2.5, top+6, left+5, top+8.5)
			r.pdf.Line(left+5, top+8.5, left+9.5, top+3.5)
		} else {
			red, green, blue := rgb(box.BorderColor)
			r.pdf.SetDrawColor(red, green, blue)
			r.pdf.SetLineWidth(box.BorderWidth)
			r.pdf.Rect(left, top, box.Width, box.Height, "D")
		}

		for _, grandchild := range child.Content {
			r.block(grandchild, left+indent, width-indent-item.PaddingRight)
		}
		r.y += item.MarginBottom
	}
	r.y += list.MarginVertical
}

func (r *renderer) horizontalRule(left, width float64) {
	rule := style("horizontalRule")
	r.y += rule.MarginVertical
	r.ensure(rule.BorderBottomWidth)
	red, green, blue := rgb(rule.BorderBottomColor)
	r.pdf.SetDrawColor(red, green, blue)
	r.pdf.SetLineWidth(rule.BorderBottomWidth)
	r.pdf.Line(left, r.y, left+width, r.y)
	r.y += rule.BorderBottomWidth + rule.MarginVertical
}

func (r *renderer) callout(node ydoc.Node, left, width float64) {
	callout := style("callout")
	r.y += callout.MarginVertical

	start := r.y
	startPage := r.pdf.PageNo()
	inner := left + callout.Padding
	r.y += callout.Padding
	for _, child := range node.Content {
		r.block(child, inner, width-2*callout.Padding)
	}
	r.y += callout.Padding

	// The panel is drawn behind what is already on the page, which is the one place the order of drawing has to be worked around.
	if r.pdf.PageNo() == startPage {
		red, green, blue := rgb(callout.BackgroundColor)
		r.pdf.SetFillColor(red, green, blue)
		r.pdf.SetAlpha(0.35, "Normal")
		r.pdf.Rect(left, start, width, r.y-start, "F")
		r.pdf.SetAlpha(1, "Normal")
	}
	r.y += callout.MarginVertical
}

func (r *renderer) image(node ydoc.Node, left, width float64) {
	imageStyle := style("image")
	r.y += imageStyle.MarginVertical

	source, _ := node.Attrs["src"].(string)
	data, have := r.options.Images[source]
	if r.options.NoAssets || !have || len(data) == 0 {
		r.imagePlaceholder(left, width)
		r.y += imageStyle.MarginVertical
		return
	}

	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		r.imagePlaceholder(left, width)
		r.y += imageStyle.MarginVertical
		return
	}

	drawWidth := float64(config.Width)
	drawHeight := float64(config.Height)
	if drawWidth > width {
		drawHeight *= width / drawWidth
		drawWidth = width
	}
	r.ensure(drawHeight)

	name := "asset-" + source
	r.pdf.RegisterImageOptionsReader(name, fpdf.ImageOptions{ImageType: format, ReadDpi: false}, bytes.NewReader(data))
	if r.pdf.Ok() {
		r.pdf.ImageOptions(name, left, r.y, drawWidth, drawHeight, false, fpdf.ImageOptions{ImageType: format}, 0, "")
		r.y += drawHeight
	} else {
		// A picture the writer will not take is a placeholder rather than a failed export.
		r.pdf.ClearError()
		r.imagePlaceholder(left, width)
	}
	r.y += imageStyle.MarginVertical
}

func (r *renderer) imagePlaceholder(left, width float64) {
	placeholder := style("imagePlaceholder")
	text := style("imagePlaceholderText")
	height := 2*placeholder.Padding + text.FontSize*style("page").LineHeight
	r.ensure(height)

	red, green, blue := rgb(placeholder.BackgroundColor)
	r.pdf.SetFillColor(red, green, blue)
	red, green, blue = rgb(placeholder.BorderColor)
	r.pdf.SetDrawColor(red, green, blue)
	r.pdf.SetLineWidth(placeholder.BorderWidth)
	r.pdf.Rect(left, r.y, width, height, "FD")

	red, green, blue = rgb(text.Color)
	r.pdf.SetTextColor(red, green, blue)
	r.pdf.SetFont(familyRegular, "", text.FontSize)
	const label = "Image"
	r.pdf.Text(left+(width-r.pdf.GetStringWidth(label))/2, r.y+placeholder.Padding+text.FontSize, label)
	r.y += height
}

func (r *renderer) table(node ydoc.Node, left, width float64) {
	table := style("table")
	row := style("tableRow")
	cell := style("tableCell")
	r.y += table.MarginVertical

	columns := 0
	for _, child := range node.Content {
		if len(child.Content) > columns {
			columns = len(child.Content)
		}
	}
	if columns == 0 {
		r.y += table.MarginVertical
		return
	}
	// Every cell is flex: 1 in the original, so the columns are equal.
	columnWidth := width / float64(columns)

	for _, tableRow := range node.Content {
		rowTop := r.y
		rowPage := r.pdf.PageNo()
		tallest := r.y

		header := len(tableRow.Content) > 0 && tableRow.Content[0].Type == "tableHeader"
		if header {
			red, green, blue := rgb(style("tableHeaderRow").BackgroundColor)
			r.pdf.SetFillColor(red, green, blue)
		}

		for index, tableCell := range tableRow.Content {
			cellLeft := left + float64(index)*columnWidth
			r.y = rowTop + cell.Padding
			for _, child := range tableCell.Content {
				r.block(child, cellLeft+cell.Padding, columnWidth-2*cell.Padding)
			}
			r.y += cell.Padding
			if r.y > tallest {
				tallest = r.y
			}
		}
		r.y = tallest

		// The lines are drawn once the row's height is known, and only when the whole row stayed on one page.
		if r.pdf.PageNo() == rowPage {
			red, green, blue := rgb(row.BorderBottomColor)
			r.pdf.SetDrawColor(red, green, blue)
			r.pdf.SetLineWidth(row.BorderBottomWidth)
			r.pdf.Line(left, r.y, left+width, r.y)
			for index := 0; index <= columns; index++ {
				x := left + float64(index)*columnWidth
				r.pdf.Line(x, rowTop, x, r.y)
			}
			if rowTop == r.y {
				continue
			}
			r.pdf.Line(left, rowTop, left+width, rowTop)
		}
	}
	r.y += table.MarginVertical
}
