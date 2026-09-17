package pagination

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestOffsetPaginatorMatchesDjango diffs the arithmetic against the same expressions OffsetPaginator uses, over every combination of page size, page number and total the fixture covers. It is the second of the three paginators in the codebase and shares nothing with the cursor one but the word.
func TestOffsetPaginatorMatchesDjango(t *testing.T) {
	fixture, err := os.ReadFile("testdata/offset.tsv")
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, line := range strings.Split(string(fixture), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		columns := strings.Split(line, "\t")
		if len(columns) != 13 {
			t.Fatalf("malformed fixture row %q", line)
		}
		rows++
		perPage := mustAtoi(t, columns[0])
		cursor := OffsetCursor{Value: mustAtoi(t, columns[1]), Offset: mustAtoi(t, columns[2])}
		total := mustAtoi(t, columns[3])
		wantOffset, wantStop := mustAtoi(t, columns[4]), mustAtoi(t, columns[5])

		// The window is what the database would return for that slice, which is what the caller measures.
		windowRows := wantStop
		if windowRows > total {
			windowRows = total
		}
		windowRows -= wantOffset
		if windowRows < 0 {
			windowRows = 0
		}

		page := PlanOffsetPage(perPage, cursor, total, windowRows, DefaultPerPage)
		returned := windowRows
		if returned > page.Limit {
			returned = page.Limit
		}
		for name, pair := range map[string][2]string{
			"offset":            {strconv.Itoa(page.Offset), columns[4]},
			"stop":              {strconv.Itoa(page.Stop), columns[5]},
			"returned":          {strconv.Itoa(returned), columns[6]},
			"next_cursor":       {page.NextCursor, columns[7]},
			"prev_cursor":       {page.PrevCursor, columns[8]},
			"next_page_results": {boolDigit(page.NextPageResults), columns[9]},
			"prev_page_results": {boolDigit(page.PrevPageResults), columns[10]},
			"total_count":       {strconv.Itoa(page.TotalCount), columns[11]},
			"total_pages":       {strconv.Itoa(page.TotalPages), columns[12]},
		} {
			if pair[0] != pair[1] {
				t.Errorf("per_page %s page %s total %s: %s = %s, want %s",
					columns[0], columns[2], columns[3], name, pair[0], pair[1])
			}
		}
	}
	if rows != 200 {
		t.Fatalf("fixture has %d rows, want 200", rows)
	}
}

// The window reaches one row past the page on purpose: whether that row arrives is how the paginator answers "is there a next page".
func TestTheExtraRowIsWhatDecidesTheNextPage(t *testing.T) {
	page := PlanOffsetPage(10, OffsetCursor{Value: 10}, 100, 11, DefaultPerPage)
	if page.Stop != 11 || !page.NextPageResults {
		t.Fatalf("a full window should report a next page: %#v", page)
	}
	exact := PlanOffsetPage(10, OffsetCursor{Value: 10}, 10, 10, DefaultPerPage)
	if exact.NextPageResults {
		t.Fatal("exactly one page of rows means there is nothing after it")
	}
}

// per_page above the ceiling is refused rather than clamped, which is the opposite of what the cursor paginator does with its page size.
func TestPerPageIsRefusedRatherThanClamped(t *testing.T) {
	if value, err := PerPage("", 1000, 1000); err != nil || value != 1000 {
		t.Fatalf("an absent per_page gave (%d, %v)", value, err)
	}
	if value, err := PerPage("50", 1000, 1000); err != nil || value != 50 {
		t.Fatalf("a valid per_page gave (%d, %v)", value, err)
	}
	if _, err := PerPage("5000", 1000, 1000); !errors.Is(err, ErrPerPageTooLarge) {
		t.Fatalf("an oversized per_page gave %v, want a refusal", err)
	}
	if _, err := PerPage("many", 1000, 1000); !errors.Is(err, ErrInvalidPerPage) {
		t.Fatalf("a non-numeric per_page gave %v", err)
	}
	// The ceiling is raised to the default when it would otherwise sit below it.
	if value, err := PerPage("800", 1000, 10); err != nil || value != 800 {
		t.Fatalf("a ceiling below the default gave (%d, %v)", value, err)
	}
}

// The cursor's first part is read as a number, so a decimal one is accepted and truncated; the third is a flag rather than an offset.
func TestOffsetCursorParsing(t *testing.T) {
	cursor, err := ParseOffsetCursor("50:2:1")
	if err != nil || cursor.Value != 50 || cursor.Offset != 2 || !cursor.IsPrev {
		t.Fatalf("parsed = %#v, %v", cursor, err)
	}
	if cursor.String() != "50:2:1" {
		t.Fatalf("round trip = %s", cursor.String())
	}
	if decimal, err := ParseOffsetCursor("50.5:0:0"); err != nil || decimal.Value != 50 {
		t.Fatalf("a decimal page size gave %#v, %v", decimal, err)
	}
	for _, value := range []string{"", "50", "50:2", "50:2:0:0", "a:2:0", "50:b:0", "50:2:c"} {
		if _, err := ParseOffsetCursor(value); err == nil {
			t.Errorf("ParseOffsetCursor(%q) should be refused", value)
		}
	}
}

// The envelope has twelve keys, not the cursor paginator's nine, and reports the whole set twice under two names.
func TestOffsetEnvelopeShape(t *testing.T) {
	page := PlanOffsetPage(10, OffsetCursor{Value: 10}, 42, 11, DefaultPerPage)
	envelope := page.Envelope([]any{}, 10, nil, nil, nil)
	for _, key := range []string{
		"grouped_by", "sub_grouped_by", "total_count", "next_cursor", "prev_cursor",
		"next_page_results", "prev_page_results", "count", "total_pages",
		"total_results", "extra_stats", "results",
	} {
		if _, present := envelope[key]; !present {
			t.Errorf("envelope is missing %q", key)
		}
	}
	if len(envelope) != 12 {
		t.Fatalf("envelope has %d keys, want 12", len(envelope))
	}
	if envelope["total_count"] != envelope["total_results"] {
		t.Fatal("the whole set is reported twice under two names")
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	// Unlike the cursor paginator, both cursors are always strings.
	if !strings.Contains(string(encoded), `"next_cursor":"10:1:0"`) || !strings.Contains(string(encoded), `"prev_cursor":"10:-1:1"`) {
		t.Fatalf("envelope = %s", encoded)
	}
}
