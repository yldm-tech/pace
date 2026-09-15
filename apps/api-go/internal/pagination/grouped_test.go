package pagination

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestGroupedPaginatorMatchesDjango diffs both halves of the grouped paginator against the real class: the offset arithmetic, and process_results over a corpus of pages. The grouping is where the surprises are, and the fixture is what pins them rather than a reading of the Python.
func TestGroupedPaginatorMatchesDjango(t *testing.T) {
	fixture, err := os.ReadFile("testdata/grouped.tsv")
	if err != nil {
		t.Fatal(err)
	}
	offsetRows, groupRows := 0, 0
	for _, line := range strings.Split(string(fixture), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		columns := strings.Split(line, "\t")
		switch columns[0] {
		case "offset":
			if len(columns) != 6 {
				t.Fatalf("malformed offset row %q", line)
			}
			offsetRows++
			window := PlanGroupWindow(mustAtoi(t, columns[1]), mustAtoi(t, columns[2]), mustAtoi(t, columns[3]))
			if window.Offset != mustAtoi(t, columns[4]) || window.Stop != mustAtoi(t, columns[5]) {
				t.Errorf("page %s value %s limit %s: window = %d..%d, want %s..%s",
					columns[1], columns[2], columns[3], window.Offset, window.Stop, columns[4], columns[5])
			}
		case "group":
			if len(columns) != 5 {
				t.Fatalf("malformed group row %q", line)
			}
			groupRows++
			field := columns[1]
			rows := parseRows(t, field, columns[2])
			var totals map[string]int
			if err := json.Unmarshal([]byte(columns[3]), &totals); err != nil {
				t.Fatal(err)
			}
			var want map[string]GroupedResult
			if err := json.Unmarshal([]byte(columns[4]), &want); err != nil {
				t.Fatal(err)
			}
			known := sortedKeys(totals)
			got := GroupRows(field, rows, known, totals)
			compareGroups(t, line, normalize(got), normalize(want))
		default:
			t.Fatalf("unknown fixture row kind %q", columns[0])
		}
	}
	if offsetRows != 48 {
		t.Fatalf("fixture has %d offset rows, want 48", offsetRows)
	}
	if groupRows < 100 {
		t.Fatalf("fixture has only %d group rows", groupRows)
	}
}

func parseRows(t *testing.T, field, encoded string) []GroupedRow {
	t.Helper()
	var raw []map[string]any
	if err := json.Unmarshal([]byte(encoded), &raw); err != nil {
		t.Fatal(err)
	}
	rows := make([]GroupedRow, 0, len(raw))
	for _, item := range raw {
		row := GroupedRow{ID: item["id"].(string), Fields: map[string]any{}}
		if value, present := item[field]; present && value != nil {
			text := value.(string)
			row.Group = &text
		}
		rows = append(rows, row)
	}
	return rows
}

// normalize renders the buckets as comparable JSON, sorting the id arrays the way the fixture does.
func normalize(groups map[string]GroupedResult) map[string]any {
	out := map[string]any{}
	for key, bucket := range groups {
		results := make([]any, 0, len(bucket.Results))
		for _, row := range bucket.Results {
			copied := map[string]any{}
			for field, value := range row {
				if list, ok := value.([]string); ok {
					// Copied into a non-nil slice so an empty array stays an array rather than becoming null.
					ordered := make([]string, len(list))
					copy(ordered, list)
					sort.Strings(ordered)
					copied[field] = ordered
					continue
				}
				if list, ok := value.([]any); ok {
					texts := make([]string, 0, len(list))
					for _, item := range list {
						texts = append(texts, item.(string))
					}
					sort.Strings(texts)
					copied[field] = texts
					continue
				}
				copied[field] = value
			}
			results = append(results, copied)
		}
		out[key] = map[string]any{"results": results, "total_results": bucket.TotalResults}
	}
	return out
}

func compareGroups(t *testing.T, line string, got, want map[string]any) {
	t.Helper()
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if !reflect.DeepEqual(normalizeJSON(gotJSON), normalizeJSON(wantJSON)) {
		t.Errorf("grouping differs\n row  %s\n got  %s\n want %s", line, gotJSON, wantJSON)
	}
}

func normalizeJSON(encoded []byte) any {
	var value any
	_ = json.Unmarshal(encoded, &value)
	return value
}

func sortedKeys(totals map[string]int) []string {
	keys := make([]string, 0, len(totals))
	for key := range totals {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func mustAtoi(t *testing.T, value string) int {
	t.Helper()
	parsed, err := strconv.Atoi(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

// Grouping by a plain column pre-seeds every known group; grouping by a many-to-many field does not.
func TestOnlyThePlainPathPreSeedsEveryGroup(t *testing.T) {
	group := "group-1"
	rows := []GroupedRow{{ID: "issue-1", Group: &group, Fields: map[string]any{}}}
	known := []string{"group-1", "group-2"}
	totals := map[string]int{"group-1": 1, "group-2": 5}

	plain := GroupRows("state_id", rows, known, totals)
	if len(plain) != 2 {
		t.Fatalf("the plain path produced %d buckets, want every known group", len(plain))
	}
	if bucket := plain["group-2"]; len(bucket.Results) != 0 || bucket.TotalResults != 5 {
		t.Fatalf("an unseen group should still carry its total: %#v", bucket)
	}

	multi := GroupRows("labels__id", rows, known, totals)
	if len(multi) != 1 {
		t.Fatalf("the many-to-many path produced %d buckets, want only the one present", len(multi))
	}
}

// An issue that belongs to no group is filed under the literal None and carries an empty id array rather than that string.
func TestUngroupedIssuesCarryAnEmptyArray(t *testing.T) {
	rows := []GroupedRow{{ID: "issue-1", Fields: map[string]any{}}}
	grouped := GroupRows("assignees__id", rows, []string{"None"}, map[string]int{"None": 3})
	bucket, present := grouped[NoGroup]
	if !present || len(bucket.Results) != 1 {
		t.Fatalf("grouped = %#v", grouped)
	}
	if ids, ok := bucket.Results[0]["assignee_ids"].([]string); !ok || len(ids) != 0 {
		t.Fatalf("assignee_ids = %v, want an empty array", bucket.Results[0]["assignee_ids"])
	}
}

// An issue in several groups appears in each, and the row that lands in every bucket is the first one seen for that issue, so its group-by field still reads as the first row's value.
func TestAnIssueInSeveralGroupsAppearsInEachWithTheFirstRow(t *testing.T) {
	first, second := "group-1", "group-2"
	rows := []GroupedRow{
		{ID: "issue-1", Group: &first, Fields: map[string]any{}},
		{ID: "issue-1", Group: &second, Fields: map[string]any{}},
	}
	grouped := GroupRows("labels__id", rows, []string{"group-1", "group-2"}, map[string]int{"group-1": 1, "group-2": 2})
	if len(grouped) != 2 {
		t.Fatalf("grouped = %#v", grouped)
	}
	for name, bucket := range grouped {
		if len(bucket.Results) != 1 {
			t.Fatalf("%s holds %d rows, want the issue once", name, len(bucket.Results))
		}
		if bucket.Results[0]["labels__id"] != "group-1" {
			t.Errorf("%s: the group-by field reads %v, want the first row's value", name, bucket.Results[0]["labels__id"])
		}
		ids := bucket.Results[0]["label_ids"].([]string)
		if len(ids) != 2 {
			t.Errorf("%s: label_ids = %v, want both groups", name, ids)
		}
	}
}

// An empty page returns no buckets at all, even on the plain path that would otherwise pre-seed them.
func TestAnEmptyPageProducesNoBuckets(t *testing.T) {
	for _, field := range []string{"state_id", "labels__id"} {
		if grouped := GroupRows(field, nil, []string{"group-1"}, map[string]int{"group-1": 4}); len(grouped) != 0 {
			t.Errorf("%s produced %#v for an empty page", field, grouped)
		}
	}
}

// total_pages is the largest group divided by the page size, and zero when nothing came back.
func TestMaxHits(t *testing.T) {
	for _, test := range []struct {
		largest, limit int
		hasResults     bool
		want           int
	}{
		{largest: 100, limit: 50, hasResults: true, want: 2},
		{largest: 101, limit: 50, hasResults: true, want: 3},
		{largest: 1, limit: 50, hasResults: true, want: 1},
		{largest: 0, limit: 50, hasResults: true, want: 0},
		{largest: 100, limit: 50, hasResults: false, want: 0},
	} {
		if got := MaxHits(test.largest, test.limit, test.hasResults); got != test.want {
			t.Errorf("MaxHits(%d, %d, %v) = %d, want %d", test.largest, test.limit, test.hasResults, got, test.want)
		}
	}
}

// The grouped cursor keeps the flat paginator's three-part shape, with the third part marking a backwards cursor.
func TestGroupCursorRendering(t *testing.T) {
	if got := GroupCursor(50, 2, false); got != "50:2:0" {
		t.Fatalf("cursor = %s", got)
	}
	if got := GroupCursor(50, -1, true); got != "50:-1:1" {
		t.Fatalf("previous cursor = %s", got)
	}
}
