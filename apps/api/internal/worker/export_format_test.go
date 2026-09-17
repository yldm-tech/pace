package worker

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// exportFormatCase is one set of rows with what the three real formatters made of it.
type exportFormatCase struct {
	Name string            `json:"name"`
	Rows []json.RawMessage `json:"rows"`
	CSV  string            `json:"csv"`
	JSON string            `json:"json"`
	XLSX struct {
		Title string  `json:"title"`
		Cells [][]any `json:"cells"`
	} `json:"xlsx"`
}

// The three encoders are diffed against what the Python formatters really produce, which is the only way to be sure about the parts nobody would think to check: how a list reaches a csv cell, how python escapes a non-ASCII character in json, and which of those two a spreadsheet gets instead.
func TestTheEncodersMatchThePythonFormatters(t *testing.T) {
	raw, err := os.ReadFile("testdata/export_format.json")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	var cases []exportFormatCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parsing the fixture: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("the fixture has no cases")
	}

	for _, testCase := range cases {
		t.Run(testCase.Name, func(t *testing.T) {
			rows := make([]*orderedMap, 0, len(testCase.Rows))
			for _, row := range testCase.Rows {
				parsed, err := parseOrderedRow(row)
				if err != nil {
					t.Fatalf("parsing a row: %v", err)
				}
				rows = append(rows, parsed)
			}

			if got := encodeExportCSV(rows); got != testCase.CSV {
				t.Errorf("the csv is\n%q\nrather than\n%q", got, testCase.CSV)
			}
			if got := encodeExportJSON(rows); got != testCase.JSON {
				t.Errorf("the json is\n%s\nrather than\n%s", got, testCase.JSON)
			}

			content, err := encodeExportXLSX(rows)
			if err != nil {
				t.Fatalf("encoding the spreadsheet: %v", err)
			}
			book, err := excelize.OpenReader(bytes.NewReader(content))
			if err != nil {
				t.Fatalf("reading the spreadsheet back: %v", err)
			}
			defer book.Close()
			sheets := book.GetSheetList()
			if len(sheets) != 1 || sheets[0] != testCase.XLSX.Title {
				t.Errorf("the sheets are %v rather than [%s]", sheets, testCase.XLSX.Title)
			}
			cells, err := book.GetRows(testCase.XLSX.Title)
			if err != nil {
				t.Fatalf("reading the cells: %v", err)
			}
			compareXLSXCells(t, cells, testCase.XLSX.Cells)
		})
	}
}

// compareXLSXCells compares the grid this wrote against the one openpyxl read back. Both are compared as text, since the two libraries do not agree on which Go or Python type a cell comes back as.
func compareXLSXCells(t *testing.T, got [][]string, want [][]any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("the spreadsheet has %d rows rather than %d", len(got), len(want))
	}
	for index, wantRow := range want {
		gotRow := got[index]
		if len(gotRow) != len(wantRow) {
			t.Errorf("row %d has %d cells rather than %d", index, len(gotRow), len(wantRow))
			continue
		}
		for column, wantCell := range wantRow {
			if gotRow[column] != xlsxCellText(wantCell) {
				t.Errorf("row %d column %d is %q rather than %q", index, column, gotRow[column], xlsxCellText(wantCell))
			}
		}
	}
}

// xlsxCellText renders what the fixture recorded for one cell. A number comes back through json as a float and a boolean as a boolean, and both are compared as the text a reader would see.
func xlsxCellText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case bool:
		if typed {
			return "TRUE"
		}
		return "FALSE"
	case float64:
		return strings.TrimSuffix(strings.TrimRight(jsonNumber(typed), "0"), ".")
	}
	return value.(string)
}

func jsonNumber(value float64) string {
	encoded, _ := json.Marshal(value)
	text := string(encoded)
	if !strings.Contains(text, ".") {
		return text + "."
	}
	return text
}

// parseOrderedRow reads one fixture row while keeping the order its keys were written in, which is what decides the columns.
func parseOrderedRow(raw json.RawMessage) (*orderedMap, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	row := newOrderedMap()
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		row.Set(key.(string), fixtureValue(value))
	}
	_, err := decoder.Token()
	return row, err
}

// fixtureValue turns what the json decoder produced into the shapes the encoders take: a list of strings, a list of ordered objects, or a plain scalar.
func fixtureValue(value any) any {
	switch typed := value.(type) {
	case json.Number:
		number, err := typed.Int64()
		if err != nil {
			float, _ := typed.Float64()
			return float
		}
		return int(number)
	case []any:
		if len(typed) == 0 {
			return []string{}
		}
		if _, isObject := typed[0].(map[string]any); isObject {
			objects := make([]*orderedMap, 0, len(typed))
			for _, item := range typed {
				objects = append(objects, orderedFromMap(item.(map[string]any)))
			}
			return objects
		}
		texts := make([]string, 0, len(typed))
		for _, item := range typed {
			texts = append(texts, item.(string))
		}
		return texts
	}
	return value
}

// orderedFromMap rebuilds one of the three nested objects in the order the export serializer writes it, which a decoded map has lost.
func orderedFromMap(value map[string]any) *orderedMap {
	orders := [][]string{
		{"url", "title"},
		{"type", "issue", "direction"},
		{"comment", "created_by", "created_at"},
	}
	row := newOrderedMap()
	for _, order := range orders {
		if _, found := value[order[0]]; !found {
			continue
		}
		for _, key := range order {
			row.Set(key, value[key])
		}
		return row
	}
	for key, item := range value {
		row.Set(key, item)
	}
	return row
}
