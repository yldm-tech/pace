package pagination

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestPlanMatchesDjango diffs the arithmetic against the same computation the Python performs, over every combination of page size, page number and total the fixture covers. The edges are where it matters: a page past the end, a page size above the cap, and a total that divides exactly.
func TestPlanMatchesDjango(t *testing.T) {
	fixture, err := os.ReadFile("testdata/plan.tsv")
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, line := range strings.Split(string(fixture), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		columns := strings.Split(line, "\t")
		if len(columns) != 11 {
			t.Fatalf("malformed fixture row %q", line)
		}
		cursor, err := ParseCursor(columns[0])
		if err != nil {
			t.Fatalf("parse %q: %v", columns[0], err)
		}
		total := atoi(t, columns[1])
		page, err := Plan(cursor, total)
		if err != nil {
			t.Fatalf("plan %q over %d: %v", columns[0], total, err)
		}
		rows++
		wantNext := columns[6]
		if wantNext == "-" {
			wantNext = ""
		}
		for name, pair := range map[string][2]string{
			"start":             {strconv.Itoa(page.Start), columns[2]},
			"end":               {strconv.Itoa(page.End), columns[3]},
			"prev_cursor":       {page.PrevCursor, columns[4]},
			"cursor":            {page.Cursor, columns[5]},
			"next_cursor":       {page.NextCursor, wantNext},
			"prev_page_results": {boolDigit(page.PrevPageResults), columns[7]},
			"next_page_results": {boolDigit(page.NextPageResults), columns[8]},
			"total_results":     {strconv.Itoa(page.TotalResults), columns[9]},
			"total_pages":       {strconv.Itoa(page.TotalPages), columns[10]},
		} {
			if pair[0] != pair[1] {
				t.Errorf("cursor %s over %d: %s = %s, want %s", columns[0], total, name, pair[0], pair[1])
			}
		}
	}
	if rows != 175 {
		t.Fatalf("fixture has %d rows, want 175", rows)
	}
}

func atoi(t *testing.T, value string) int {
	t.Helper()
	parsed, err := strconv.Atoi(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func boolDigit(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func TestCursorParsing(t *testing.T) {
	for _, value := range []string{"1000:0:0", "50:3:0", "-1:-1:-1"} {
		cursor, err := ParseCursor(value)
		if err != nil {
			t.Errorf("ParseCursor(%q): %v", value, err)
			continue
		}
		if cursor.String() != value {
			t.Errorf("ParseCursor(%q).String() = %q", value, cursor.String())
		}
	}
	// Django requires exactly three integer parts.
	for _, value := range []string{"", "1000", "1000:0", "1000:0:0:0", "a:0:0", "1000:b:0", "1000.5:0:0"} {
		if _, err := ParseCursor(value); err == nil {
			t.Errorf("ParseCursor(%q) should be refused", value)
		}
	}
}

// A page size of zero reaches a division Django does not guard, so it is an error rather than an empty page.
func TestZeroPageSizeIsAnError(t *testing.T) {
	if _, err := Plan(Cursor{PageSize: 0}, 10); err != ErrEmptyPageSize {
		t.Fatalf("a zero page size gave %v, want the division error", err)
	}
	// Even with nothing to page over, since the division happens first.
	if _, err := Plan(Cursor{PageSize: 0}, 0); err != ErrEmptyPageSize {
		t.Fatalf("a zero page size over an empty set gave %v", err)
	}
}

// A page past the end has an End at or below its Start, which is the empty slice Django's queryset produces.
func TestAPagePastTheEndReadsNothing(t *testing.T) {
	page, err := Plan(Cursor{PageSize: 10, Page: 5}, 12)
	if err != nil {
		t.Fatal(err)
	}
	if page.Start != 50 || page.End != 12 {
		t.Fatalf("start = %d, end = %d; want a range that reads nothing", page.Start, page.End)
	}
	if page.NextPageResults {
		t.Fatal("there is no page after the end")
	}
}

// next_cursor is null rather than a string on the last page, and the envelope has to render it that way.
func TestEnvelopeRendersTheAbsentNextCursorAsNull(t *testing.T) {
	page, err := Plan(Cursor{PageSize: 10}, 5)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(page.Envelope([]any{}, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"next_cursor":null`) {
		t.Fatalf("envelope = %s", encoded)
	}
	// prev_cursor is a string even on the first page, where it names page -1.
	if !strings.Contains(string(encoded), `"prev_cursor":"10:-1:0"`) {
		t.Fatalf("envelope = %s", encoded)
	}
	if !strings.Contains(string(encoded), `"results":[]`) {
		t.Fatalf("envelope = %s", encoded)
	}
}

func TestEnvelopeKeys(t *testing.T) {
	page, _ := Plan(DefaultCursor(), 3)
	envelope := page.Envelope([]any{1, 2, 3}, 3)
	for _, key := range []string{
		"prev_cursor", "cursor", "next_cursor", "prev_page_results", "next_page_results",
		"page_count", "total_results", "total_pages", "results",
	} {
		if _, present := envelope[key]; !present {
			t.Errorf("envelope is missing %q", key)
		}
	}
	if len(envelope) != 9 {
		t.Fatalf("envelope has %d keys, want 9", len(envelope))
	}
}
