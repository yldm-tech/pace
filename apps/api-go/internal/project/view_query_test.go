package project

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// frozenViewQueryToday is the day the generator measures the relative terms from.
var frozenViewQueryToday = time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)

// The corpus is what issue_filters(filters, "POST") returns for a decoded JSON body, which is not the shape the GET path is given and not the shape the other fixture covers.
func TestViewQueryMatchesPython(t *testing.T) {
	file, err := os.Open("testdata/view_query.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	if !scanner.Scan() {
		t.Fatal("the fixture has no header")
	}
	rows, raises := 0, 0
	for scanner.Scan() {
		columns := strings.Split(scanner.Text(), "\t")
		if len(columns) != 2 {
			t.Fatalf("row %d has %d columns, want 2", rows+1, len(columns))
		}
		var filters map[string]any
		if err := json.Unmarshal([]byte(columns[0]), &filters); err != nil {
			t.Fatal(err)
		}

		query, err := viewQueryFromFilters(filters, frozenViewQueryToday)
		if columns[1] == "RAISES" {
			raises++
			if !errors.Is(err, errQueryHoldsADate) {
				t.Errorf("%s: got %v, want the save to be refused", columns[0], err)
			}
			rows++
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", columns[0], err)
			rows++
			continue
		}
		if got, want := normalized(t, query), columns[1]; got != normalized(t, decodeQuery(t, want)) {
			t.Errorf("%s:\n got %s\nwant %s", columns[0], got, want)
		}
		rows++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if rows < 50 {
		t.Fatalf("the fixture has %d rows, too few to cover the filters", rows)
	}
	if raises == 0 {
		t.Fatal("no row covers the filters the column cannot hold")
	}
}

// normalized renders a query with its keys sorted, so two dictionaries that differ only in spacing or in key order compare equal.
func normalized(t *testing.T, query map[string]any) string {
	t.Helper()
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		encoded, err := json.Marshal(query[key])
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, key+"="+string(encoded))
	}
	return strings.Join(parts, " ")
}

// decodeQuery reads the expected dictionary back out of the fixture.
func decodeQuery(t *testing.T, raw string) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

// The two POST shapes disagree, and the disagreement is upstream's: a string date is iterated one character at a time and a list of the same dates one clause at a time.
func TestTheTwoPostShapesDisagreeAboutADate(t *testing.T) {
	asString, err := viewQueryFromFilters(map[string]any{"created_at": "2026-01-01;after"}, frozenViewQueryToday)
	if err != nil {
		t.Fatal(err)
	}
	asList, err := viewQueryFromFilters(map[string]any{"created_at": []any{"2026-01-01;after"}}, frozenViewQueryToday)
	if err != nil {
		t.Fatal(err)
	}
	if normalized(t, asString) == normalized(t, asList) {
		t.Fatal("the two shapes must not agree, which is the whole reason there are two")
	}
	if got := asList["created_at__date__gte"]; got != "2026-01-01" {
		t.Errorf("the list shape parsed the clause as %v", got)
	}
	if _, present := asString["created_at__date__contains"]; !present {
		t.Error("the string shape must fall into the character loop")
	}
}

// An empty filters blob writes the empty object rather than nothing at all.
func TestAViewWithNoFiltersStoresAnEmptyQuery(t *testing.T) {
	for _, filters := range [][]byte{nil, []byte("{}"), []byte("null"), []byte("not json")} {
		query, err := viewQueryJSON(filters, frozenViewQueryToday)
		if err != nil {
			t.Fatal(err)
		}
		if string(query) != "{}" {
			t.Errorf("%q stored %s, want {}", filters, query)
		}
	}
}
