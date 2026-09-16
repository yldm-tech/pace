package issuefilters

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// frozenFilterToday is the day the fixture's relative date terms were generated from. It was the constant in the generator that produced the fixture, and it has to stay this day: every relative term in issue_filters.tsv was resolved against it.
var frozenFilterToday = time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)

// TestIssueFiltersMatchDjango diffs the port against the real function over a thousand query strings. The mapping is full of asymmetries that only show up side by side — GET splits on commas while POST takes the value whole, some filters drop invalid uuids and others keep anything, a literal None becomes an isnull lookup on some and nothing on others — and two of the rows it pins are upstream bugs rather than design.
func TestIssueFiltersMatchDjango(t *testing.T) {
	fixture, err := os.ReadFile("testdata/issue_filters.tsv")
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, line := range strings.Split(string(fixture), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		columns := strings.Split(line, "\t")
		if len(columns) != 4 {
			t.Fatalf("malformed fixture row %q", line)
		}
		method, prefix := columns[0], columns[1]
		var params map[string]string
		if err := json.Unmarshal([]byte(columns[2]), &params); err != nil {
			t.Fatalf("parse params %s: %v", columns[2], err)
		}
		var want map[string]string
		if err := json.Unmarshal([]byte(columns[3]), &want); err != nil {
			t.Fatalf("parse expected %s: %v", columns[3], err)
		}
		rows++
		got := map[string]string{}
		for lookup, value := range Parse(params, method, prefix, frozenFilterToday) {
			got[lookup] = value.String()
		}
		if len(got) != len(want) {
			t.Errorf("%s %q %s: produced %d lookups, want %d\n got  %v\n want %v", method, prefix, columns[2], len(got), len(want), got, want)
			continue
		}
		for lookup, expected := range want {
			if got[lookup] != expected {
				t.Errorf("%s %q %s: %s = %q, want %q", method, prefix, columns[2], lookup, got[lookup], expected)
			}
		}
	}
	if rows < 1000 {
		t.Fatalf("fixture has only %d rows", rows)
	}
}

// The POST date branch hands date_filter a string rather than a list, and the loop iterates it one character at a time. Named here because it is the kind of thing a reader would otherwise take for a porting mistake.
func TestPostDateFiltersIterateTheStringByCharacter(t *testing.T) {
	result := Parse(map[string]string{"updated_at": "2026-01-02;before"}, "POST", "", frozenFilterToday)
	lte, present := result["updated_at__date__lte"]
	if !present || lte.String() != "" {
		t.Fatalf("updated_at__date__lte = %v, want the empty string the lone semicolon produces", result)
	}
	contains, present := result["updated_at__date__contains"]
	if !present || contains.String() != "e" {
		t.Fatalf("updated_at__date__contains = %v, want the last character", result)
	}
	// The same value on GET is parsed properly.
	onGet := Parse(map[string]string{"updated_at": "2026-01-02;before"}, "GET", "", frozenFilterToday)
	if onGet["updated_at__date__lte"].String() != "2026-01-02" {
		t.Fatalf("on GET the same value gave %v", onGet)
	}
}

// filter_intake_status guards on intake_status but stores the value of inbox_status, which is the Python None when that parameter is absent.
func TestPostIntakeStatusReadsTheOtherParameter(t *testing.T) {
	alone := Parse(map[string]string{"intake_status": "1"}, "POST", "", frozenFilterToday)
	if alone["issue_intake__status__in"].String() != "None" {
		t.Fatalf("intake_status alone gave %v, want the absent inbox_status", alone)
	}
	together := Parse(map[string]string{"intake_status": "1", "inbox_status": "2"}, "POST", "", frozenFilterToday)
	if together["issue_intake__status__in"].String() != "2" {
		t.Fatalf("together gave %v, want inbox_status to win", together)
	}
}

// The relative terms count a month as thirty flat days, and an offset that is not fromnow counts backwards.
func TestRelativeDateTerms(t *testing.T) {
	for _, test := range []struct {
		value  string
		lookup string
		want   string
	}{
		{value: "2_weeks;after;fromnow", lookup: "target_date__gte", want: "2026-06-29"},
		{value: "2_weeks;before;fromnow", lookup: "target_date__lte", want: "2026-06-29"},
		{value: "2_weeks;after;past", lookup: "target_date__gte", want: "2026-06-01"},
		{value: "3_months;after;fromnow", lookup: "target_date__gte", want: "2026-09-13"},
		{value: "1_months;before;past", lookup: "target_date__lte", want: "2026-05-16"},
	} {
		result := Parse(map[string]string{"target_date": test.value}, "GET", "", frozenFilterToday)
		if result[test.lookup].String() != test.want {
			t.Errorf("%s: %s = %q, want %q", test.value, test.lookup, result[test.lookup].String(), test.want)
		}
	}
	// A relative term missing its offset is dropped rather than defaulting.
	if result := Parse(map[string]string{"target_date": "2_weeks;after"}, "GET", "", frozenFilterToday); len(result) != 0 {
		t.Fatalf("a two-part relative term produced %v", result)
	}
}

// Three filters write a soft-delete lookup whether or not they matched anything, so asking about them at all narrows the join.
func TestJoinFiltersAreWrittenUnconditionally(t *testing.T) {
	for name, lookup := range map[string]string{
		"labels":     "label_issue__deleted_at__isnull",
		"assignees":  "issue_assignee__deleted_at__isnull",
		"cycle":      "issue_cycle__deleted_at__isnull",
		"module":     "issue_module__deleted_at__isnull",
		"subscriber": "issue_subscribers__deleted_at__isnull",
	} {
		result := Parse(map[string]string{name: ""}, "GET", "", frozenFilterToday)
		if value, present := result[lookup]; !present || value.String() != "true" {
			t.Errorf("%s with an empty value should still write %s, got %v", name, lookup, result)
		}
	}
}

// A filter runs when its parameter is present at all, even empty, and does not when it is absent.
func TestAbsentParametersRunNothing(t *testing.T) {
	if result := Parse(map[string]string{}, "GET", "", frozenFilterToday); len(result) != 0 {
		t.Fatalf("no parameters produced %v", result)
	}
	// type defaults to every group when present, which is why its absence matters.
	present := Parse(map[string]string{"type": ""}, "GET", "", frozenFilterToday)
	if present["state__group__in"].String() != "[backlog,unstarted,started,completed,cancelled]" {
		t.Fatalf("type = %v", present)
	}
}
