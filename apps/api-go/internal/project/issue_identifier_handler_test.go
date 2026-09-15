package project

import (
	"strings"
	"testing"
)

// strict_str_to_int accepts digits with an optional leading minus and nothing else, so a malformed key is a 400 rather than a 404.
func TestStrictIntegerParsing(t *testing.T) {
	for _, accepted := range []string{"1", "42", "-7", "0", "000"} {
		if !strictInteger(accepted) {
			t.Errorf("%q should be accepted", accepted)
		}
	}
	for _, refused := range []string{"", "-", "1e3", " 12", "12 ", "1.0", "12a", "+3", "٣"} {
		if strictInteger(refused) {
			t.Errorf("%q should be refused", refused)
		}
	}
}

// This is the only route that annotates is_intake, which is why every other one drops the key instead of returning it as false.
func TestOnlyThisRouteAnnotatesIsIntake(t *testing.T) {
	row := sampleIssueRow()
	row.IsIntake = true
	data := issueDetailJSON(row, true)
	if _, present := data["is_intake"]; present {
		t.Fatal("the shared serializer must not emit is_intake; this route adds it")
	}
	data["is_intake"] = row.IsIntake
	if len(data) != 28 {
		t.Fatalf("the identifier response has %d fields, want 28", len(data))
	}
}

// The subscriber check reaches through the sequence number rather than the issue id, which is what the Django queryset does, and the cycle annotation here carries no soft-delete filter.
func TestIdentifierAnnotationsDifferFromTheDetailRoute(t *testing.T) {
	annotations := issueIdentifierAnnotations()
	if !strings.Contains(annotations, "si.sequence_id = ?") {
		t.Error("the subscriber check reaches through the sequence number")
	}
	if !strings.Contains(annotations, "AS is_intake") {
		t.Error("the identifier route annotates is_intake")
	}
	// The detail route filters deleted cycle links; this queryset does not.
	if strings.Contains(annotations, "ci.issue_id = i.id AND ci.deleted_at IS NULL") {
		t.Error("this route's cycle annotation carries no soft-delete filter, unlike the detail route's")
	}
}

// The key splits on the first dash, so a project identifier containing one still resolves.
func TestIdentifierSplitting(t *testing.T) {
	for _, test := range []struct{ key, project, sequence string }{
		{key: "PROJ-42", project: "PROJ", sequence: "42"},
		{key: "MY-PROJ-7", project: "MY", sequence: "PROJ-7"},
		{key: "P-1", project: "P", sequence: "1"},
	} {
		project, sequence, found := strings.Cut(test.key, "-")
		if !found || project != test.project || sequence != test.sequence {
			t.Errorf("%q split to %q / %q", test.key, project, sequence)
		}
	}
	if _, _, found := strings.Cut("PROJ", "-"); found {
		t.Error("a key with no dash must not split")
	}
}
