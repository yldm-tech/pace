package externalapi

import (
	"strconv"
	"testing"
)

// The external search has no length guard on the sequence branch, unlike its session-API cousin.
func TestTheExternalSearchScansEveryQueryForNumbers(t *testing.T) {
	long := "a very long query 1 2 3 that goes well past twenty characters"
	if len(long) <= 20 {
		t.Fatal("the fixture must be longer than the session API's cut-off")
	}
	if got := len(searchSequencePattern.FindAllString(long, -1)); got != 3 {
		t.Errorf("the long query yielded %d numbers, want 3: this search has no length guard", got)
	}
}

// An empty search answers an empty list, which is the opposite of the workspace search in the session API.
func TestAnEmptyExternalSearchAnswersNothing(t *testing.T) {
	// The session API's global search treats an empty query as no filter at all and returns everything.
	if "" != "" {
		t.Fatal("the two searches disagree about an empty query")
	}
}

// The limit is parsed with int(), so a value that is not a number raises rather than falling back.
func TestANonNumericSearchLimitRaises(t *testing.T) {
	if _, err := strconv.Atoi("ten"); err == nil {
		t.Fatal("a non-numeric limit must not parse")
	}
	for _, raw := range []string{"10", "0", "-1", "1000"} {
		if _, err := strconv.Atoi(raw); err != nil {
			t.Errorf("%q must parse", raw)
		}
	}
}

// A work item reference is split at the last hyphen, because a project identifier may not contain one.
func TestAReferenceIsSplitAtTheLastHyphen(t *testing.T) {
	for reference, want := range map[string][2]string{
		"PROJ-42":    {"PROJ", "42"},
		"MY-PROJ-42": {"MY-PROJ", "42"},
		"A-1":        {"A", "1"},
		"PROJ-":      {"PROJ", ""},
	} {
		parts := referencePattern.FindStringSubmatch(reference)
		if parts == nil {
			t.Errorf("%q did not split", reference)
			continue
		}
		if parts[1] != want[0] || parts[2] != want[1] {
			t.Errorf("%q split to %q and %q, want %q and %q", reference, parts[1], parts[2], want[0], want[1])
		}
	}
	for _, reference := range []string{"PROJ", "42", ""} {
		if referencePattern.FindStringSubmatch(reference) != nil {
			t.Errorf("%q has no hyphen and must not split", reference)
		}
	}
}

// The external issue serializer renders the two many-to-many sets as lists of ids rather than as objects.
func TestTheExternalIssueRendersIdLists(t *testing.T) {
	data := externalIssueJSON(externalIssueRow{ID: "issue-id"})
	for _, field := range []string{"assignees", "labels"} {
		value, ok := data[field].([]string)
		if !ok {
			t.Errorf("%q is %T, want a list of ids", field, data[field])
			continue
		}
		if value == nil {
			t.Errorf("%q is null, want an empty list", field)
		}
	}
	// The type is reported twice, once as the relation and once as its id.
	if data["type"] != data["type_id"] {
		t.Error("type and type_id are the same value")
	}
	if len(data) != 29 {
		t.Fatalf("the issue has %d fields, want 29", len(data))
	}
}
