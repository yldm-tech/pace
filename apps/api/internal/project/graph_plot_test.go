package project

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// The corpus is sort_data's own output, which orders a priority axis by a fixed list and everything else with the literal key "none" last.
func TestSortDataMatchesPython(t *testing.T) {
	file, err := os.Open("testdata/sort_data.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatal("the fixture has no header")
	}
	rows, dropped := 0, 0
	for scanner.Scan() {
		columns := strings.Split(scanner.Text(), "\t")
		if len(columns) != 3 {
			t.Fatalf("row %d has %d columns, want 3", rows+1, len(columns))
		}
		var keys, want []string
		if err := json.Unmarshal([]byte(columns[1]), &keys); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(columns[2]), &want); err != nil {
			t.Fatal(err)
		}
		data := map[string][]gin.H{}
		for _, key := range keys {
			data[key] = []gin.H{{"dimension": key}}
		}
		got := sortGraphData(data, columns[0])
		if len(got) != len(want) {
			t.Errorf("%s over %v: got %v, want %v", columns[0], keys, got, want)
			rows++
			continue
		}
		for index := range got {
			if got[index] != want[index] {
				t.Errorf("%s over %v: got %v, want %v", columns[0], keys, got, want)
				break
			}
		}
		if len(want) < len(data) {
			dropped++
		}
		rows++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if rows < 90 {
		t.Fatalf("the fixture has %d rows, too few to cover the two orderings", rows)
	}
	if dropped == 0 {
		t.Fatal("no row covers a priority axis dropping a key the fixed order does not name")
	}
}

// A priority axis drops whatever its fixed order does not name, which is the part of sort_data that loses data rather than reordering it.
func TestAPriorityAxisDropsWhatItDoesNotName(t *testing.T) {
	data := map[string][]gin.H{"low": nil, "nonexistent": nil, "urgent": nil}
	got := sortGraphData(data, "priority")
	if len(got) != 2 || got[0] != "low" || got[1] != "urgent" {
		t.Fatalf("got %v, want the two named ones in their fixed order", got)
	}
	// Every other axis keeps everything.
	if len(sortGraphData(data, "state_id")) != 3 {
		t.Error("only a priority axis drops keys")
	}
}

// The monthly dimension is not zero padded, and a null date does not vanish: it becomes the string "-".
func TestTheMonthlyDimensionIsNotPadded(t *testing.T) {
	expression := graphDimension("created_at")
	if !strings.Contains(expression, "EXTRACT(YEAR") || !strings.Contains(expression, "EXTRACT(MONTH") {
		t.Fatalf("the expression does not extract a year and a month:\n%s", expression)
	}
	if strings.Contains(expression, "LPAD") || strings.Contains(expression, "to_char") {
		t.Error("the expression pads nothing, so March 2026 is 2026-3 rather than 2026-03")
	}
	// Each half is wrapped so a null becomes the empty string rather than a null, which is why a row with no date lands under "-".
	if strings.Count(expression, "COALESCE") < 3 {
		t.Errorf("the expression does not coalesce both halves:\n%s", expression)
	}
	// An axis that is not a date is the column itself.
	if graphDimension("priority") != "i.priority" {
		t.Errorf("a plain axis is %q", graphDimension("priority"))
	}
}

// Grouping walks consecutive runs rather than collecting values, which is what itertools.groupby does.
func TestGroupingWalksRuns(t *testing.T) {
	rows := []graphRow{
		{Dimension: "a"}, {Dimension: "a"}, {Dimension: "b"}, {Dimension: "a"},
	}
	grouped := groupGraphRows(rows, false)
	if len(grouped) != 2 {
		t.Fatalf("got %d groups, want 2", len(grouped))
	}
	// The second run of "a" overwrites the first, which is what the dict comprehension does with a repeated key.
	if len(grouped["a"]) != 1 {
		t.Errorf("the a group has %d rows, want the last run's one", len(grouped["a"]))
	}
	if len(grouped["b"]) != 1 {
		t.Errorf("the b group has %d rows, want 1", len(grouped["b"]))
	}
}

// Every allowed axis has a column, and only the five that reach through a link have a join.
func TestEveryAxisHasAColumn(t *testing.T) {
	for field := range validAnalyticsFields {
		if analyticsColumns[field] == "" {
			t.Errorf("%q maps to no column", field)
		}
	}
	if len(analyticsColumns) != len(validAnalyticsFields) {
		t.Fatalf("there are %d columns for %d fields", len(analyticsColumns), len(validAnalyticsFields))
	}
	if len(analyticsJoins) != 5 {
		t.Fatalf("there are %d joins, want 5", len(analyticsJoins))
	}
	for field := range analyticsJoins {
		if !validAnalyticsFields[field] {
			t.Errorf("%q has a join but is not an allowed axis", field)
		}
	}
	if len(analyticsDateFields) != 4 {
		t.Fatalf("there are %d date axes, want 4", len(analyticsDateFields))
	}
}
