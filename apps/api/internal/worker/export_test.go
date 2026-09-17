package worker

import (
	"strings"
	"testing"
	"time"
)

// The exported row carries the twenty-five fields the export serializer really renders, in the order it declares them — and not the three it declares but never renders, since the export queryset adds no annotations for them.
func TestTheExportedRowIsTheSerializersOwnFields(t *testing.T) {
	issue := exportIssue{
		ID: "one", SequenceID: 42, Name: "Land the thing", Priority: "urgent",
		CreatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 1, 2, 3, 4, 5, 120000000, time.UTC),
		ProjectName: "Apollo", ProjectIdentifier: "APO",
		StateName: stringPointer("In Progress"), EstimateValue: stringPointer("8"),
		CreatedByFirst: stringPointer("Ada"), CreatedByLast: stringPointer("Lovelace"),
	}
	row := exportRow(issue, exportRelated{})

	want := []string{
		"project_name", "project_identifier", "parent", "identifier", "sequence_id", "name",
		"state_name", "priority", "assignees", "subscribers", "created_by_name", "start_date",
		"target_date", "completed_at", "created_at", "updated_at", "archived_at", "estimate",
		"labels", "cycles", "modules", "links", "relations", "comments", "is_draft",
	}
	if strings.Join(row.Keys(), ",") != strings.Join(want, ",") {
		t.Errorf("the columns are\n%v\nrather than\n%v", row.Keys(), want)
	}
	for _, absent := range []string{"sub_issues_count", "link_count", "attachment_count"} {
		if _, found := row.Get(absent); found {
			t.Errorf("%s is in the row, and the queryset never annotates it", absent)
		}
	}

	if value, _ := row.Get("identifier"); value != "APO-42" {
		t.Errorf("the identifier is %v", value)
	}
	if value, _ := row.Get("created_by_name"); value != "Ada Lovelace" {
		t.Errorf("the author is %v", value)
	}
	// The fraction is padded to six digits, which is what DRF renders and what Go's own encoder would have trimmed.
	if value, _ := row.Get("updated_at"); value != "2026-01-02T03:04:05.120000Z" {
		t.Errorf("the timestamp is %v", value)
	}
	// A work item with no parent is an empty string rather than null, since the serializer method returns one.
	if value, _ := row.Get("parent"); value != "" {
		t.Errorf("the parent is %#v", value)
	}
	// A date nobody set is null, and an empty list is a list rather than null.
	if value, _ := row.Get("start_date"); value != nil {
		t.Errorf("an unset date is %#v", value)
	}
	if value, _ := row.Get("labels"); len(value.([]string)) != 0 {
		t.Errorf("an empty list is %#v", value)
	}
}

// A work item with no state, no author and no estimate renders those as empty strings rather than as null, because the serializer declares a default for each.
func TestTheColumnsThatFallBackToAnEmptyString(t *testing.T) {
	row := exportRow(exportIssue{ProjectIdentifier: "APO", SequenceID: 1}, exportRelated{})
	for _, column := range []string{"state_name", "created_by_name", "estimate"} {
		value, _ := row.Get(column)
		if value != "" {
			t.Errorf("%s is %#v rather than an empty string", column, value)
		}
	}
}

// A parent in another project is named by that project's identifier, not by the one the work item itself lives in.
func TestAParentInAnotherProject(t *testing.T) {
	sequence := 7
	row := exportRow(exportIssue{
		ProjectIdentifier: "APO", SequenceID: 42,
		ParentIdentifier: stringPointer("ZEU"), ParentSequenceID: &sequence,
	}, exportRelated{})
	if value, _ := row.Get("parent"); value != "ZEU-7" {
		t.Errorf("the parent is %v", value)
	}
}

// The prettified headers are what `str.title()` makes of the column names, which is not the same as capitalising each word.
func TestThePrettifiedHeaders(t *testing.T) {
	cases := map[string]string{
		"project_name":    "Project Name",
		"sequence_id":     "Sequence Id",
		"created_by_name": "Created By Name",
		"is_draft":        "Is Draft",
		"url":             "Url",
	}
	for column, want := range cases {
		if got := prettifyHeader(column); got != want {
			t.Errorf("%s reads as %q rather than %q", column, got, want)
		}
	}
}

// A value that a spreadsheet would run as a formula is prefixed with an apostrophe, which is what keeps an exported work item name from executing when somebody opens the file.
func TestTheFormulaTriggers(t *testing.T) {
	for _, dangerous := range []string{"=cmd", "+1", "-1", "@here", "\tTabbed", "\rReturn", "\nNewline"} {
		got := sanitizeCSVValue(dangerous)
		if text, ok := got.(string); !ok || !strings.HasPrefix(text, "'") {
			t.Errorf("%q was left as %#v", dangerous, got)
		}
	}
	for _, safe := range []string{"", "Apollo", "APO-1", "1+1"} {
		if got := sanitizeCSVValue(safe); got != safe {
			t.Errorf("%q became %#v", safe, got)
		}
	}
	// A non-string is left alone, so a sequence number is still a number in the spreadsheet.
	if got := sanitizeCSVValue(42); got != 42 {
		t.Errorf("a number became %#v", got)
	}
}

// The same list reaches a csv as json and a spreadsheet as joined text, which is a difference between the two formatters rather than a mistake in either.
func TestAListReachesTheTwoFormatsDifferently(t *testing.T) {
	link := newOrderedMap()
	link.Set("url", "https://example.test/a")
	link.Set("title", "it's here")
	links := []*orderedMap{link}

	if got := flattenCSVValue(links); got != `[{"url": "https://example.test/a", "title": "it's here"}]` {
		t.Errorf("the csv value is %v", got)
	}
	if got := xlsxValue(links); got != `{'url': 'https://example.test/a', 'title': "it's here"}` {
		t.Errorf("the spreadsheet value is %v", got)
	}
	if got := flattenCSVValue([]string{"a", "b"}); got != `["a", "b"]` {
		t.Errorf("the csv list is %v", got)
	}
	if got := xlsxValue([]string{"a", "b"}); got != "a, b" {
		t.Errorf("the spreadsheet list is %v", got)
	}
}

// The zip carries one entry per file, named after the workspace when the export is one file and after each project when it is several.
func TestTheZipCarriesOneEntryPerFile(t *testing.T) {
	archive, err := zipExport([][2]any{{"acme-one.csv", "a,b\r\n"}, {"acme-two.csv", "c,d\r\n"}})
	if err != nil {
		t.Fatalf("zipping: %v", err)
	}
	if len(archive) == 0 {
		t.Fatal("the archive is empty")
	}
	// The entry names are stored uncompressed in the local headers, so they are readable in the bytes themselves.
	for _, name := range []string{"acme-one.csv", "acme-two.csv"} {
		if !strings.Contains(string(archive), name) {
			t.Errorf("the archive does not name %s", name)
		}
	}
}
