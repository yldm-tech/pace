package project

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
)

// Five of the six point sums go through Python's `or 0` and the sixth carries a Django default. The two spell zero differently in the body, which is what the fixture is for.
func TestTheProgressSumsSpellZeroTwoWays(t *testing.T) {
	file, err := os.Open("testdata/cycle_progress.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatal("the fixture has no header")
	}
	rows := 0
	for scanner.Scan() {
		columns := strings.Split(scanner.Text(), "\t")
		if len(columns) != 3 {
			t.Fatalf("row %d has %d columns, want 3", rows+1, len(columns))
		}
		var aggregate *float64
		if columns[0] != "null" {
			parsed, err := strconv.ParseFloat(columns[0], 64)
			if err != nil {
				t.Fatal(err)
			}
			aggregate = &parsed
		}
		if got := render(t, orZero(aggregate)); got != columns[1] {
			t.Errorf("orZero(%s) renders %s, Python renders %s", columns[0], got, columns[1])
		}
		if got := render(t, coalesceFloat(aggregate)); got != columns[2] {
			t.Errorf("coalesceFloat(%s) renders %s, Python renders %s", columns[0], got, columns[2])
		}
		rows++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if rows < 13 {
		t.Fatalf("the fixture has %d rows, want every case the aggregate can produce", rows)
	}
}

// render puts a value through the real response path, which is where the float rewriting happens.
func render(t *testing.T, value any) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	drf.Respond(c, http.StatusOK, gin.H{"value": value})

	var body map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return string(body["value"])
}

// A zero sum and an absent one are the same three characters, and a zero total is not.
func TestAZeroTotalIsStillAFloat(t *testing.T) {
	zero := 0.0
	if got := render(t, orZero(&zero)); got != "0" {
		t.Errorf("a zero grouped sum renders %s, want 0", got)
	}
	if got := render(t, orZero(nil)); got != "0" {
		t.Errorf("an absent grouped sum renders %s, want 0", got)
	}
	if got := render(t, coalesceFloat(nil)); got != "0.0" {
		t.Errorf("an absent total renders %s, want 0.0", got)
	}
}

// The state group each live count filters on is read off the response key, so a renamed key would quietly count the wrong group.
func TestEveryLiveCountNamesItsOwnStateGroup(t *testing.T) {
	for key, group := range map[string]string{
		"backlog_issues":   "backlog",
		"unstarted_issues": "unstarted",
		"started_issues":   "started",
		"cancelled_issues": "cancelled",
		"completed_issues": "completed",
	} {
		if derived := key[:len(key)-len("_issues")]; derived != group {
			t.Errorf("%q counts the %q group, want %q", key, derived, group)
		}
	}
}
