package djangotemplate

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestMatchesDjango renders the real notification template against the same contexts Django rendered it with, and compares the html byte for byte.
//
// This is what makes keeping the template as a copy worth anything: the file in testdata is the one apps/api ships, and if the two engines disagree about it this fails.
func TestMatchesDjango(t *testing.T) {
	source, err := os.ReadFile("testdata/issue-updates.html")
	if err != nil {
		t.Fatal(err)
	}
	template, err := Parse(string(source))
	if err != nil {
		t.Fatalf("the template does not parse: %v", err)
	}

	fixture, err := os.ReadFile("testdata/issue_updates.txt")
	if err != nil {
		t.Fatal(err)
	}
	cases := splitCases(t, string(fixture))
	if len(cases) < 7 {
		t.Fatalf("the fixture carried %d cases, which is too few to be the generated one", len(cases))
	}
	for name, testCase := range cases {
		context := map[string]any{}
		if err := json.Unmarshal([]byte(testCase.context), &context); err != nil {
			t.Fatalf("%s: the context is not json: %v", name, err)
		}
		got, err := template.Render(context)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got != testCase.output {
			t.Errorf("%s: the rendering differs from Django's.\n%s", name, firstDifference(got, testCase.output))
		}
	}
}

type templateCase struct {
	context string
	output  string
}

func splitCases(t *testing.T, fixture string) map[string]templateCase {
	t.Helper()
	cases := map[string]templateCase{}
	for _, block := range strings.Split(fixture, "=== CASE ")[1:] {
		name, rest, found := strings.Cut(block, "\n--- CONTEXT\n")
		if !found {
			t.Fatalf("malformed fixture block: %.60s", block)
		}
		context, rest, found := strings.Cut(rest, "\n--- OUTPUT\n")
		if !found {
			t.Fatalf("malformed fixture block: %.60s", block)
		}
		output, _, found := strings.Cut(rest, "\n=== END\n")
		if !found {
			t.Fatalf("malformed fixture block: %.60s", block)
		}
		cases[name] = templateCase{context: context, output: output}
	}
	return cases
}

// firstDifference points at where the two renderings part company, since printing two twenty-kilobyte documents helps nobody.
func firstDifference(got, want string) string {
	limit := len(got)
	if len(want) < limit {
		limit = len(want)
	}
	for index := 0; index < limit; index++ {
		if got[index] == want[index] {
			continue
		}
		start := index - 60
		if start < 0 {
			start = 0
		}
		return "at byte " + itoa(index) + "\n  got:  " + excerpt(got, start) + "\n  want: " + excerpt(want, start)
	}
	if len(got) == len(want) {
		return "the two are the same length and differ nowhere, which should not happen"
	}
	return "one rendering is " + itoa(len(got)) + " bytes and the other " + itoa(len(want))
}

func excerpt(value string, start int) string {
	end := start + 160
	if end > len(value) {
		end = len(value)
	}
	return strings.ReplaceAll(value[start:end], "\n", "\\n")
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
