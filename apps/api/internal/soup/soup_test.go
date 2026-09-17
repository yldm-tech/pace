package soup

import (
	"encoding/json"
	"os"
	"testing"
)

type soupFixture struct {
	RoundTrip []struct {
		HTML    string   `json:"html"`
		Output  string   `json:"output"`
		Sources []string `json:"sources"`
	} `json:"round_trip"`
	Replacements []struct {
		HTML   string `json:"html"`
		Tag    string `json:"tag"`
		Pairs  []struct{ Old, New string }
		Output string `json:"output"`
	} `json:"replacements"`
}

func loadFixture(t *testing.T) soupFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/round_trip.json")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	var fixture soupFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("parsing the fixture: %v", err)
	}
	if len(fixture.RoundTrip) == 0 {
		t.Fatal("the fixture has no cases")
	}
	return fixture
}

// Every description is round-tripped through this and through BeautifulSoup, and the two are compared byte for byte. That is the only way to be sure of the parts nobody would guess: that attributes come back in name order, that a paragraph is not closed before a block element, and that a class written with two spaces comes back with one.
func TestTheRoundTripMatchesBeautifulSoup(t *testing.T) {
	for _, testCase := range loadFixture(t).RoundTrip {
		if got := Parse(testCase.HTML).Render(); got != testCase.Output {
			t.Errorf("%q round-tripped to\n%q\nrather than\n%q", testCase.HTML, got, testCase.Output)
		}
	}
}

// The sources read out of a description are the ones bs4 reads, in the same order.
func TestTheSourcesMatchBeautifulSoup(t *testing.T) {
	for _, testCase := range loadFixture(t).RoundTrip {
		var sources []string
		for _, element := range Parse(testCase.HTML).FindAll("image-component") {
			if source, found := element.Get("src"); found && source != "" {
				sources = append(sources, source)
			}
		}
		if len(sources) != len(testCase.Sources) {
			t.Errorf("%q gave %v rather than %v", testCase.HTML, sources, testCase.Sources)
			continue
		}
		for index, source := range sources {
			if source != testCase.Sources[index] {
				t.Errorf("%q gave %v rather than %v", testCase.HTML, sources, testCase.Sources)
				break
			}
		}
	}
}

// Rewriting a source and writing the description back out matches what replace_asset_ids produces, including for the elements it leaves alone.
func TestTheReplacementsMatchBeautifulSoup(t *testing.T) {
	for _, testCase := range loadFixture(t).Replacements {
		document := Parse(testCase.HTML)
		for _, element := range document.FindAll(testCase.Tag) {
			for _, pair := range testCase.Pairs {
				if source, _ := element.Get("src"); source == pair.Old {
					element.Set("src", pair.New)
				}
			}
		}
		if got := document.Render(); got != testCase.Output {
			t.Errorf("%q became\n%q\nrather than\n%q", testCase.HTML, got, testCase.Output)
		}
	}
}
