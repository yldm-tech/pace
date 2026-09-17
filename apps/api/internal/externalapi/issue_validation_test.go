package externalapi

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestExternalIssueValidationMatchesDRF diffs the Go validation against the body DRF answers with for the same payload. The fixture is generated, which is what pins the wording — an integration reads these messages, and a message that differs is a message it cannot match.
func TestExternalIssueValidationMatchesDRF(t *testing.T) {
	fixture, err := os.ReadFile("testdata/issue_validation.tsv")
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, line := range strings.Split(string(fixture), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			t.Fatalf("malformed fixture row %q", line)
		}
		rows++
		var body map[string]json.RawMessage
		if err := json.Unmarshal([]byte(parts[0]), &body); err != nil {
			t.Fatalf("row %q: %v", line, err)
		}
		input, failures := validateExternalIssue(body, false)
		if parts[1] == "VALID" {
			if failures != nil {
				t.Errorf("%s is refused with %v, and DRF accepts it", parts[0], failures)
				continue
			}
			var want string
			if err := json.Unmarshal([]byte(parts[2]), &want); err != nil {
				t.Fatalf("row %q: %v", line, err)
			}
			got, _ := input.values["description_html"].(string)
			if got != want {
				t.Errorf("%s stores the description as %q, want %q", parts[0], got, want)
			}
			continue
		}
		if failures == nil {
			t.Errorf("%s is accepted, and DRF refuses it with %s", parts[0], parts[2])
			continue
		}
		encoded, err := json.Marshal(failures)
		if err != nil {
			t.Fatal(err)
		}
		if !sameJSON(t, string(encoded), parts[2]) {
			t.Errorf("%s:\n got  %s\n want %s", parts[0], encoded, parts[2])
		}
	}
	if rows < 55 {
		t.Fatalf("the fixture has %d rows, which is fewer than it was generated with", rows)
	}
}

func sameJSON(t *testing.T, left, right string) bool {
	t.Helper()
	var first, second any
	if err := json.Unmarshal([]byte(left), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(right), &second); err != nil {
		t.Fatal(err)
	}
	encodedFirst, _ := json.Marshal(first)
	encodedSecond, _ := json.Marshal(second)
	return string(encodedFirst) == string(encodedSecond)
}

// The name is required on a create and not on an update, which is the one difference partial makes.
func TestTheNameIsRequiredOnlyOnACreate(t *testing.T) {
	if _, failures := validateExternalIssue(map[string]json.RawMessage{}, false); failures == nil {
		t.Error("a create with no name is accepted")
	}
	if _, failures := validateExternalIssue(map[string]json.RawMessage{}, true); failures != nil {
		t.Errorf("an update with no name is refused with %v", failures)
	}
}
