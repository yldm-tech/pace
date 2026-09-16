package worker

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/xuri/excelize/v2"
)

// orderedMap is one exported row. The order of the keys is the order of the columns, so it cannot be a Go map.
type orderedMap struct {
	keys   []string
	values map[string]any
}

func newOrderedMap() *orderedMap {
	return &orderedMap{values: map[string]any{}}
}

func (row *orderedMap) Set(key string, value any) {
	if _, seen := row.values[key]; !seen {
		row.keys = append(row.keys, key)
	}
	row.values[key] = value
}

func (row *orderedMap) Get(key string) (any, bool) {
	value, found := row.values[key]
	return value, found
}

func (row *orderedMap) Keys() []string { return row.keys }

// exportFieldnames collects every key across the rows in the order they were first seen, which is what decides the columns.
func exportFieldnames(rows []*orderedMap) []string {
	names := []string{}
	seen := map[string]bool{}
	for _, row := range rows {
		for _, key := range row.Keys() {
			if !seen[key] {
				seen[key] = true
				names = append(names, key)
			}
		}
	}
	return names
}

// prettifyHeader is `header.replace("_", " ").title()`, so created_by_name becomes Created By Name.
func prettifyHeader(header string) string {
	spaced := strings.ReplaceAll(header, "_", " ")
	return pythonTitle(spaced)
}

// pythonTitle is str.title: the first letter of every run of letters is uppercased and the rest are lowered, where a run is broken by anything that is not a letter.
func pythonTitle(value string) string {
	var builder strings.Builder
	previousWasLetter := false
	for _, character := range value {
		isLetter := isASCIILetter(character) || (character > 127 && strings.ToUpper(string(character)) != strings.ToLower(string(character)))
		switch {
		case !isLetter:
			builder.WriteRune(character)
		case previousWasLetter:
			builder.WriteString(strings.ToLower(string(character)))
		default:
			builder.WriteString(strings.ToUpper(string(character)))
		}
		previousWasLetter = isLetter
	}
	return builder.String()
}

func isASCIILetter(character rune) bool {
	return (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')
}

// csvFormulaTriggers are the characters a spreadsheet would read as the start of a formula.
const csvFormulaTriggers = "=+-@\t\r\n"

// sanitizeCSVValue is sanitize_csv_value: a string that starts with one of those characters is prefixed with an apostrophe so it is read as text.
func sanitizeCSVValue(value any) any {
	text, ok := value.(string)
	if !ok || text == "" {
		return value
	}
	if strings.ContainsRune(csvFormulaTriggers, rune(text[0])) {
		return "'" + text
	}
	return value
}

// encodeExportJSON is JSONFormatter.encode: json.dumps with an indent of two.
func encodeExportJSON(rows []*orderedMap) string {
	items := make([]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, row)
	}
	return pythonJSON(items, 2, 0)
}

// encodeExportCSV is CSVFormatter.encode with its defaults: flattened rows, prettified headers, and every list written as json.
func encodeExportCSV(rows []*orderedMap) string {
	if len(rows) == 0 {
		return ""
	}
	fieldnames := exportFieldnames(rows)
	var buffer bytes.Buffer

	headers := make([]string, 0, len(fieldnames))
	for _, name := range fieldnames {
		headers = append(headers, csvText(sanitizeCSVValue(prettifyHeader(name))))
	}
	writeCSVRecord(&buffer, headers)
	for _, row := range rows {
		record := make([]string, 0, len(fieldnames))
		for _, name := range fieldnames {
			value, found := row.Get(name)
			if !found {
				value = ""
			}
			record = append(record, csvText(sanitizeCSVValue(flattenCSVValue(value))))
		}
		writeCSVRecord(&buffer, record)
	}
	return buffer.String()
}

// writeCSVRecord writes one record the way python's csv.writer does.
//
// Go's own writer is not usable here. Its UseCRLF rewrites a newline *inside* a field as well as the one between records, and a work item whose name has a line break in it is not unusual — python leaves the one in the field alone and only ends the record with CRLF. Go also quotes a field that merely starts with a space, which python does not.
func writeCSVRecord(buffer *bytes.Buffer, record []string) {
	for index, field := range record {
		if index > 0 {
			buffer.WriteByte(',')
		}
		if strings.ContainsAny(field, ",\"\r\n") {
			buffer.WriteByte('"')
			buffer.WriteString(strings.ReplaceAll(field, `"`, `""`))
			buffer.WriteByte('"')
			continue
		}
		buffer.WriteString(field)
	}
	buffer.WriteString("\r\n")
}

// flattenCSVValue is _flatten's leaf rule: a list becomes json and everything else is left as it is. No key is ever nested here, since none of the exported fields is a bare object.
func flattenCSVValue(value any) any {
	switch value.(type) {
	case []string, []*orderedMap:
		return pythonJSON(value, 0, 0)
	}
	return value
}

// csvText is what python's csv writer makes of a value on its way into a field: None is written as nothing and anything else goes through str().
func csvText(value any) string {
	if value == nil {
		return ""
	}
	return pythonString(value)
}

// pythonString is str(): a bool reads as True or False, and a float keeps repr's shortest round-tripping form.
func pythonString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		if typed {
			return "True"
		}
		return "False"
	case int:
		return strconv.Itoa(typed)
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64)
	case nil:
		return "None"
	default:
		return fmt.Sprint(typed)
	}
}

// encodeExportXLSX is XLSXFormatter.encode.
//
// It does not flatten and it does not write json. A list becomes its items joined with ", ", and an item that is an object is joined in by str(), so the links column reads as a python dict in a spreadsheet and as json in the csv of the same export.
func encodeExportXLSX(rows []*orderedMap) ([]byte, error) {
	book := excelize.NewFile()
	defer book.Close()
	// openpyxl names the first sheet Sheet; this library names it Sheet1.
	const sheet = "Sheet"
	if err := book.SetSheetName("Sheet1", sheet); err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		fieldnames := exportFieldnames(rows)
		headers := make([]any, 0, len(fieldnames))
		for _, name := range fieldnames {
			headers = append(headers, sanitizeCSVValue(prettifyHeader(name)))
		}
		if err := book.SetSheetRow(sheet, "A1", &headers); err != nil {
			return nil, err
		}
		for index, row := range rows {
			record := make([]any, 0, len(fieldnames))
			for _, name := range fieldnames {
				value, found := row.Get(name)
				if !found {
					value = ""
				}
				record = append(record, sanitizeCSVValue(xlsxValue(value)))
			}
			cell := "A" + strconv.Itoa(index+2)
			if err := book.SetSheetRow(sheet, cell, &record); err != nil {
				return nil, err
			}
		}
	}
	buffer, err := book.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// xlsxValue is _format_value: nothing becomes an empty string, a list is joined with ", " through str(), and anything else goes into the cell as it is.
func xlsxValue(value any) any {
	switch typed := value.(type) {
	case nil:
		return ""
	case []string:
		return strings.Join(typed, ", ")
	case []*orderedMap:
		rendered := make([]string, 0, len(typed))
		for _, item := range typed {
			rendered = append(rendered, pythonDict(item))
		}
		return strings.Join(rendered, ", ")
	}
	return value
}

// pythonDict is str() of a dict, which is what a list of objects is joined out of. The keys keep the order the serializer built them in, which is why these objects are ordered maps rather than plain ones.
func pythonDict(value *orderedMap) string {
	parts := make([]string, 0, len(value.Keys()))
	for _, key := range value.Keys() {
		item, _ := value.Get(key)
		parts = append(parts, pythonReprValue(key)+": "+pythonReprValue(item))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// pythonReprValue is repr() for the handful of types an exported object holds.
func pythonReprValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return pythonString(value)
	}
	quote := "'"
	if strings.Contains(text, "'") && !strings.Contains(text, `"`) {
		quote = `"`
	}
	escaped := strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\r", `\r`, "\t", `\t`, quote, `\`+quote).Replace(text)
	return quote + escaped + quote
}

// pythonJSON writes json the way json.dumps does, which Go's own encoder does not.
//
// Two differences matter. Python escapes every non-ASCII character as \uXXXX and leaves <, > and & alone; Go does the opposite on both counts. And with an indent, python separates items with ", " when there is none and with a newline when there is, which is what the exported json is compared against.
func pythonJSON(value any, indent, depth int) string {
	var builder strings.Builder
	writePythonJSON(&builder, value, indent, depth)
	return builder.String()
}

func writePythonJSON(builder *strings.Builder, value any, indent, depth int) {
	switch typed := value.(type) {
	case nil:
		builder.WriteString("null")
	case bool:
		builder.WriteString(strconv.FormatBool(typed))
	case int:
		builder.WriteString(strconv.Itoa(typed))
	case float64:
		builder.WriteString(strconv.FormatFloat(typed, 'g', -1, 64))
	case string:
		builder.WriteString(pythonJSONString(typed))
	case *orderedMap:
		writePythonJSONObject(builder, typed.Keys(), func(key string) any { value, _ := typed.Get(key); return value }, indent, depth)
	case []string:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
		writePythonJSONArray(builder, items, indent, depth)
	case []*orderedMap:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
		writePythonJSONArray(builder, items, indent, depth)
	case []any:
		writePythonJSONArray(builder, typed, indent, depth)
	default:
		builder.WriteString(pythonJSONString(pythonString(typed)))
	}
}

func writePythonJSONObject(builder *strings.Builder, keys []string, valueOf func(string) any, indent, depth int) {
	if len(keys) == 0 {
		builder.WriteString("{}")
		return
	}
	builder.WriteByte('{')
	for index, key := range keys {
		if index > 0 {
			builder.WriteByte(',')
			writeJSONSeparator(builder, indent, depth+1)
		} else {
			writeJSONOpening(builder, indent, depth+1)
		}
		builder.WriteString(pythonJSONString(key))
		builder.WriteString(": ")
		writePythonJSON(builder, valueOf(key), indent, depth+1)
	}
	writeJSONClosing(builder, indent, depth)
	builder.WriteByte('}')
}

func writePythonJSONArray(builder *strings.Builder, items []any, indent, depth int) {
	if len(items) == 0 {
		builder.WriteString("[]")
		return
	}
	builder.WriteByte('[')
	for index, item := range items {
		if index > 0 {
			builder.WriteByte(',')
			writeJSONSeparator(builder, indent, depth+1)
		} else {
			writeJSONOpening(builder, indent, depth+1)
		}
		writePythonJSON(builder, item, indent, depth+1)
	}
	writeJSONClosing(builder, indent, depth)
	builder.WriteByte(']')
}

// writeJSONOpening writes what follows the opening bracket: nothing at all without an indent, and a newline with the padding of the level inside when there is one.
func writeJSONOpening(builder *strings.Builder, indent, depth int) {
	if indent > 0 {
		builder.WriteByte('\n')
		builder.WriteString(strings.Repeat(" ", indent*depth))
	}
}

// writeJSONSeparator writes what follows the comma between two items, which is a single space without an indent — json.dumps separates with ", " then.
func writeJSONSeparator(builder *strings.Builder, indent, depth int) {
	if indent > 0 {
		builder.WriteByte('\n')
		builder.WriteString(strings.Repeat(" ", indent*depth))
		return
	}
	builder.WriteByte(' ')
}

// writeJSONClosing writes what comes before the closing bracket, which is nothing without an indent.
func writeJSONClosing(builder *strings.Builder, indent, depth int) {
	if indent > 0 {
		builder.WriteByte('\n')
		builder.WriteString(strings.Repeat(" ", indent*depth))
	}
}

// pythonJSONString escapes a string the way json.dumps does with ensure_ascii left on.
func pythonJSONString(value string) string {
	var builder strings.Builder
	builder.WriteByte('"')
	for _, character := range value {
		switch character {
		case '"':
			builder.WriteString(`\"`)
		case '\\':
			builder.WriteString(`\\`)
		case '\n':
			builder.WriteString(`\n`)
		case '\r':
			builder.WriteString(`\r`)
		case '\t':
			builder.WriteString(`\t`)
		case '\b':
			builder.WriteString(`\b`)
		case '\f':
			builder.WriteString(`\f`)
		default:
			switch {
			case character < 0x20:
				builder.WriteString(fmt.Sprintf(`\u%04x`, character))
			case character < 0x7f:
				builder.WriteRune(character)
			case character > 0xffff:
				// Everything outside the basic plane is written as the surrogate pair python writes.
				high, low := utf16.EncodeRune(character)
				builder.WriteString(fmt.Sprintf(`\u%04x\u%04x`, high, low))
			default:
				builder.WriteString(fmt.Sprintf(`\u%04x`, character))
			}
		}
	}
	builder.WriteByte('"')
	return builder.String()
}

// zipExport is create_zip_file: one deflated entry per file, in the order they were made.
func zipExport(files [][2]any) ([]byte, error) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, entry := range files {
		name, _ := entry[0].(string)
		writer, err := archive.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			return nil, err
		}
		switch content := entry[1].(type) {
		case string:
			_, err = writer.Write([]byte(content))
		case []byte:
			_, err = writer.Write(content)
		}
		if err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
