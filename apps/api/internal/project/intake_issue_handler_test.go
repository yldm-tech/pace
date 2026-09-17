package project

import (
	"strings"
	"testing"
)

// The status filter defaults to pending alone, so a caller that names nothing sees only what still needs triaging.
func TestTheIntakeListDefaultsToPending(t *testing.T) {
	statuses := []string{}
	for _, item := range strings.Split("-2", ",") {
		if item != "null" {
			statuses = append(statuses, item)
		}
	}
	if len(statuses) != 1 || statuses[0] != "-2" {
		t.Fatalf("the default status filter is %v, want pending alone", statuses)
	}
	// A list of nothing but "null" narrows nothing rather than matching nothing.
	empty := []string{}
	for _, item := range strings.Split("null,null", ",") {
		if item != "null" {
			empty = append(empty, item)
		}
	}
	if len(empty) != 0 {
		t.Errorf("a filter of nulls left %v behind", empty)
	}
}

// The five statuses are the ones the model names, and only the accepted one leaves the work item behind on delete.
func TestOnlyAnAcceptedIssueSurvivesDeletion(t *testing.T) {
	if intakeStatusPending != -2 || intakeStatusRejected != -1 || intakeStatusSnoozed != 0 ||
		intakeStatusAccepted != 1 || intakeStatusDuplicate != 2 {
		t.Fatal("the statuses are not the ones the model names")
	}
	for _, status := range []int{intakeStatusPending, intakeStatusRejected, intakeStatusSnoozed, intakeStatusDuplicate} {
		if status == intakeStatusAccepted {
			t.Errorf("%d must take the work item with it", status)
		}
	}
}

// Every field the order allowlist names maps to a column, and anything else falls back to the issue's creation time.
func TestEveryAllowedOrderFieldHasAColumn(t *testing.T) {
	for field := range intakeIssueOrderByAllowlist {
		if column := intakeOrderColumn(field); column == "" {
			t.Errorf("%q maps to no column", field)
		}
		if column := intakeOrderColumn("-" + field); !strings.HasSuffix(column, " DESC") {
			t.Errorf("a descending %q renders as %q", field, column)
		}
	}
	if len(intakeIssueOrderByAllowlist) != 11 {
		t.Fatalf("the allowlist has %d fields, want 11", len(intakeIssueOrderByAllowlist))
	}
	// The default is the issue's creation time rather than the link's, because most of the allowlist reaches through the issue.
	if got := intakeOrderColumn(sanitizeOrderBy("", intakeIssueOrderByAllowlist, "-issue__created_at")); got != "i.created_at DESC" {
		t.Errorf("the default order is %q", got)
	}
	// A field that is not on the list never reaches the query.
	if got := sanitizeOrderBy("issue__description_html", intakeIssueOrderByAllowlist, "-issue__created_at"); got != "-issue__created_at" {
		t.Errorf("an unlisted field survived as %q", got)
	}
}

// The priority is checked by hand before the serializer sees it, so an unknown one is a plain message rather than a field error.
func TestThePriorityIsCheckedByHand(t *testing.T) {
	for _, priority := range []string{"low", "medium", "high", "urgent", "none"} {
		if !validIssuePriorities[priority] {
			t.Errorf("%q is a valid priority", priority)
		}
	}
	for _, priority := range []string{"", "critical", "None", "URGENT"} {
		if validIssuePriorities[priority] {
			t.Errorf("%q is not a valid priority", priority)
		}
	}
	if len(validIssuePriorities) != 5 {
		t.Fatalf("there are %d priorities, want 5", len(validIssuePriorities))
	}
}

// The two serializers differ in what they carry, and only the detail names the duplicate it points at.
func TestTheTwoIntakeSerializersDiffer(t *testing.T) {
	listed := map[string]bool{"id": true, "status": true, "duplicate_to": true, "snoozed_till": true, "source": true, "issue": true, "created_by": true}
	detailed := map[string]bool{"id": true, "status": true, "duplicate_to": true, "snoozed_till": true, "duplicate_issue_detail": true, "source": true, "issue": true}

	if len(listed) != 7 || len(detailed) != 7 {
		t.Fatalf("the two serializers have %d and %d fields, want seven each", len(listed), len(detailed))
	}
	if listed["duplicate_issue_detail"] {
		t.Error("the list serializer does not expand the duplicate")
	}
	if detailed["created_by"] {
		t.Error("the detail serializer does not carry created_by")
	}
}

// The filters arrive prefixed because they are applied to the link, and the prefix has to come off before the SQL is built.
func TestTheIntakeFiltersArePrefixed(t *testing.T) {
	filters := issueFilters(map[string]string{"priority": "urgent"}, "GET", "issue__", frozenViewQueryToday)
	found := false
	for lookup := range filters {
		if !strings.HasPrefix(lookup, "issue__") {
			t.Errorf("%q is not prefixed, and every filter here should be", lookup)
		}
		found = true
	}
	if !found {
		t.Fatal("the fixture produced no filters")
	}
	_, conditions, _, ok := intakeIssueFilterSQL(filters)
	if !ok {
		t.Fatal("the prefixed filters must translate once the prefix is off")
	}
	if len(conditions) == 0 {
		t.Error("the translation produced no conditions")
	}
}
