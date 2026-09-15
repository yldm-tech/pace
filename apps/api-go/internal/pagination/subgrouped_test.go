package pagination

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestSubGroupRowsMatchDjango diffs the doubly-nested grouping against the real class, including the cases where it raises. That is the point of the fixture: the plain path indexes the pre-seeded dictionary without checking and answers 500, while the many-to-many path checks and drops the row, and nothing in the Python says so.
func TestSubGroupRowsMatchDjango(t *testing.T) {
	fixture, err := os.ReadFile("testdata/subgrouped.tsv")
	if err != nil {
		t.Fatal(err)
	}
	rows, raised := 0, 0
	for _, line := range strings.Split(string(fixture), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		columns := strings.Split(line, "\t")
		if len(columns) != 7 {
			t.Fatalf("malformed fixture row %q", line)
		}
		groupField, subField := columns[0], columns[1]
		var knownGroups []string
		var groupTotals map[string]int
		var subTotals map[string]map[string]int
		var rawRows []map[string]any
		mustUnmarshal(t, columns[2], &knownGroups)
		mustUnmarshal(t, columns[3], &groupTotals)
		mustUnmarshal(t, columns[4], &subTotals)
		mustUnmarshal(t, columns[5], &rawRows)
		rows++

		page := make([]SubGroupedRow, 0, len(rawRows))
		for _, raw := range rawRows {
			row := SubGroupedRow{ID: raw["id"].(string), Fields: map[string]any{}}
			if value, present := raw[groupField]; present && value != nil {
				text := value.(string)
				row.Group = &text
			}
			if value, present := raw[subField]; present && value != nil {
				text := value.(string)
				row.SubGroup = &text
			}
			page = append(page, row)
		}

		got, err := SubGroupRows(groupField, subField, page, knownGroups, groupTotals, subTotals)
		if strings.HasPrefix(columns[6], "!") {
			raised++
			if err == nil {
				t.Errorf("%s/%s over %s should have raised %s, got %v", groupField, subField, columns[5], columns[6], got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s/%s over %s raised %v, want a result", groupField, subField, columns[5], err)
			continue
		}
		var want map[string]SubGroupedResult
		mustUnmarshal(t, columns[6], &want)
		if !sameSubGroups(got, want) {
			gotJSON, _ := json.Marshal(normalizeSubGroups(got))
			wantJSON, _ := json.Marshal(normalizeSubGroups(want))
			t.Errorf("%s/%s over %s\n got  %s\n want %s", groupField, subField, columns[5], gotJSON, wantJSON)
		}
	}
	if rows < 100 {
		t.Fatalf("fixture has only %d rows", rows)
	}
	if raised == 0 {
		t.Fatal("the fixture pins no raising case, which is half of what it is for")
	}
}

func mustUnmarshal(t *testing.T, encoded string, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(encoded), target); err != nil {
		t.Fatalf("parse %s: %v", encoded, err)
	}
}

func normalizeSubGroups(groups map[string]SubGroupedResult) any {
	out := map[string]any{}
	for name, group := range groups {
		subs := map[string]any{}
		for subName, sub := range group.Results {
			results := make([]any, 0, len(sub.Results))
			for _, row := range sub.Results {
				copied := map[string]any{}
				for key, value := range row {
					switch list := value.(type) {
					case []string:
						ordered := make([]string, len(list))
						copy(ordered, list)
						sort.Strings(ordered)
						copied[key] = ordered
					case []any:
						texts := make([]string, 0, len(list))
						for _, item := range list {
							texts = append(texts, item.(string))
						}
						sort.Strings(texts)
						copied[key] = texts
					default:
						copied[key] = value
					}
				}
				results = append(results, copied)
			}
			subs[subName] = map[string]any{"results": results, "total_results": sub.TotalResults}
		}
		out[name] = map[string]any{"results": subs, "total_results": group.TotalResults}
	}
	return out
}

func sameSubGroups(got, want map[string]SubGroupedResult) bool {
	gotJSON, _ := json.Marshal(normalizeSubGroups(got))
	wantJSON, _ := json.Marshal(normalizeSubGroups(want))
	var left, right any
	_ = json.Unmarshal(gotJSON, &left)
	_ = json.Unmarshal(wantJSON, &right)
	leftText, _ := json.Marshal(left)
	rightText, _ := json.Marshal(right)
	return string(leftText) == string(rightText)
}

// Only sub-groups with a total are pre-seeded, so a group can come back with an empty results map even though its own total is set.
func TestOnlySubGroupsWithATotalArePreSeeded(t *testing.T) {
	group, sub := "g1", "s1"
	rows := []SubGroupedRow{{ID: "i1", Group: &group, SubGroup: &sub, Fields: map[string]any{}}}
	processed, err := SubGroupRows("state_id", "priority", rows,
		[]string{"g1", "g2"},
		map[string]int{"g1": 1, "g2": 7},
		map[string]map[string]int{"g1": {"s1": 4}})
	if err != nil {
		t.Fatal(err)
	}
	if len(processed["g2"].Results) != 0 || processed["g2"].TotalResults != 7 {
		t.Fatalf("g2 = %#v, want an empty results map with its total", processed["g2"])
	}
	if len(processed["g1"].Results["s1"].Results) != 1 {
		t.Fatalf("g1/s1 = %#v", processed["g1"].Results["s1"])
	}
}

// The plain path raises on a row that does not fit; the many-to-many path drops it.
func TestTheTwoPathsDifferOnARowThatDoesNotFit(t *testing.T) {
	group, sub := "unseeded", "s1"
	rows := []SubGroupedRow{{ID: "i1", Group: &group, SubGroup: &sub, Fields: map[string]any{}}}
	seeded := map[string]map[string]int{"g1": {"s1": 4}}

	if _, err := SubGroupRows("state_id", "priority", rows, []string{"g1"}, map[string]int{"g1": 1}, seeded); err != ErrUnseededSubGroup {
		t.Fatalf("the plain path gave %v, want the refusal", err)
	}
	processed, err := SubGroupRows("labels__id", "priority", rows, []string{"g1"}, map[string]int{"g1": 1}, seeded)
	if err != nil {
		t.Fatalf("the many-to-many path should drop the row, not raise: %v", err)
	}
	if len(processed["g1"].Results["s1"].Results) != 0 {
		t.Fatalf("the row should have been dropped: %#v", processed)
	}
}

// An empty page returns no groups at all, even though every known group would otherwise be seeded.
func TestAnEmptySubGroupedPageReturnsNothing(t *testing.T) {
	processed, err := SubGroupRows("state_id", "priority", nil, []string{"g1"}, map[string]int{"g1": 1}, nil)
	if err != nil || len(processed) != 0 {
		t.Fatalf("processed = %#v, %v", processed, err)
	}
}
