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
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
)

// The corpus is the Python loop's own output over windows behind, across and ahead of today, with completions inside and outside the window and a never-completed row the loop skips.
func TestBurndownChartMatchesPython(t *testing.T) {
	today := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)

	file, err := os.Open("testdata/burndown.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	if !scanner.Scan() {
		t.Fatal("the fixture has no header")
	}
	rows := 0
	for scanner.Scan() {
		columns := strings.Split(scanner.Text(), "\t")
		if len(columns) != 7 {
			t.Fatalf("row %d has %d columns, want 7", rows+1, len(columns))
		}
		start := parseDay(t, columns[0])
		end := parseDay(t, columns[1])
		total, err := strconv.ParseFloat(columns[2], 64)
		if err != nil {
			t.Fatal(err)
		}

		var completions [][]any
		if err := json.Unmarshal([]byte(columns[5]), &completions); err != nil {
			t.Fatal(err)
		}
		input := burndownInput{
			Start:        &start,
			End:          &end,
			Total:        total,
			TotalIsFloat: columns[3] == "true",
			Points:       columns[4] == "points",
		}
		for _, completion := range completions {
			entry := burndownCompletion{}
			if day, ok := completion[0].(string); ok {
				parsed := parseDay(t, day)
				entry.Date = &parsed
			}
			value, ok := completion[1].(float64)
			if !ok {
				t.Fatalf("row %d has a completion with no number", rows+1)
			}
			entry.Value = value
			input.Completions = append(input.Completions, entry)
		}

		if got := renderBody(t, burndownChart(input, today)); got != columns[6] {
			t.Errorf("row %d (%s %s..%s):\n got %s\nwant %s", rows+1, columns[4], columns[0], columns[1], got, columns[6])
		}
		rows++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if rows < 20 {
		t.Fatalf("the fixture has %d rows, too few to cover the windows", rows)
	}
}

func parseDay(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", value, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

// renderBody writes a payload the way a response does, then reads it back with sorted keys so it can be compared against json.dumps(..., sort_keys=True).
func renderBody(t *testing.T, payload any) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	drf.Respond(c, http.StatusOK, payload)

	var sorted map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &sorted); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(sorted))
	for key := range sorted {
		keys = append(keys, key)
	}
	sortStrings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, strconv.Quote(key)+": "+string(sorted[key]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func sortStrings(values []string) {
	for outer := 1; outer < len(values); outer++ {
		for inner := outer; inner > 0 && values[inner] < values[inner-1]; inner-- {
			values[inner], values[inner-1] = values[inner-1], values[inner]
		}
	}
}

// A cycle with no window at all draws no chart, which is the empty object rather than a null.
func TestABurndownWithNoWindowIsEmpty(t *testing.T) {
	day := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	for _, input := range []burndownInput{
		{Start: nil, End: &day},
		{Start: &day, End: nil},
		{Start: nil, End: nil},
	} {
		if chart := burndownChart(input, day); len(chart) != 0 {
			t.Errorf("a chart with no window has %d days, want none", len(chart))
		}
	}
}
