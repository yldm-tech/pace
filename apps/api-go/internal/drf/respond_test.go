package drf

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func recordResponse(t *testing.T, payload any) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	Respond(c, http.StatusOK, payload)
	return recorder.Body.String()
}

func TestRespondRewritesDatetimesWhereverTheySit(t *testing.T) {
	instant := time.Date(2026, 1, 2, 3, 4, 5, 120000000, time.UTC)
	body := recordResponse(t, gin.H{
		"at":     instant,
		"maybe":  &instant,
		"absent": (*time.Time)(nil),
		"nested": gin.H{"at": instant},
		"list":   []gin.H{{"at": instant}},
		"mixed":  []any{instant, gin.H{"at": instant}},
		"rows":   []map[string]any{{"at": instant}},
		"name":   "unchanged",
	})
	if strings.Contains(body, "05.12Z") {
		t.Fatalf("a datetime was left in Go's rendering:\n%s", body)
	}
	if count := strings.Count(body, "2026-01-02T03:04:05.120000Z"); count != 7 {
		t.Fatalf("rewrote %d of 7 datetimes:\n%s", count, body)
	}
	if !strings.Contains(body, `"absent":null`) || !strings.Contains(body, `"name":"unchanged"`) {
		t.Fatalf("Respond changed something it should not have:\n%s", body)
	}
}

// The grouped responses serialize one map into several buckets, so the rewrite must not write back into the caller's map.
func TestRespondDoesNotMutateTheCallersPayload(t *testing.T) {
	instant := time.Date(2026, 1, 2, 3, 4, 5, 120000000, time.UTC)
	shared := gin.H{"at": instant}
	recordResponse(t, gin.H{"a": shared, "b": shared})
	if _, unchanged := shared["at"].(time.Time); !unchanged {
		t.Fatalf("the caller's map now holds %T", shared["at"])
	}
}

func TestRespondLeavesNonContainersAlone(t *testing.T) {
	for _, payload := range []any{
		gin.H{"error": "nope"},
		gin.H{"count": 3, "ok": true, "ratio": 1.5, "nothing": nil},
		gin.H{"raw": json.RawMessage(`{"a":1}`)},
	} {
		body := recordResponse(t, payload)
		expected, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if body != string(expected) {
			t.Errorf("Respond changed a payload with no datetimes:\n got  %s\n want %s", body, expected)
		}
	}
}

// A struct is the serializer's own business; reaching into one would rewrite a field the caller deliberately typed.
func TestRespondDoesNotReachIntoStructs(t *testing.T) {
	type payload struct {
		At time.Time `json:"at"`
	}
	body := recordResponse(t, gin.H{"row": payload{At: time.Date(2026, 1, 2, 3, 4, 5, 120000000, time.UTC)}})
	if !strings.Contains(body, "05.12Z") {
		t.Fatalf("a struct field was rewritten, which is outside this function's contract:\n%s", body)
	}
}

// Respond exists so that no serializer has to remember. That only holds while every success response goes through it, so the tree is scanned for one written directly.
func TestEverySuccessResponseGoesThroughRespond(t *testing.T) {
	direct := regexp.MustCompile(`c\.JSON\(http\.Status(OK|Created|Accepted)\b`)
	root := filepath.Join("..", "..", "internal")
	offenders := []string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		// The live service is not the API. It answers as the Express service it replaces, whose bodies are JavaScript's own rendering rather than Django's, so putting them through a function that rewrites datetimes the Django way would be the bug rather than the fix.
		if strings.Contains(filepath.ToSlash(path), "/internal/live/") {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for number, line := range strings.Split(string(contents), "\n") {
			if direct.MatchString(line) {
				offenders = append(offenders, filepath.ToSlash(path)+":"+strconv.Itoa(number+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Fatalf("these write a success body directly, so their datetimes keep Go's rendering instead of Django's; use drf.Respond:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

// A float anywhere in a response body is a Django FloatField, and Python renders one with repr(). The walk has to reach into maps, slices and pointers alike, because sort_order sits in a map while an estimate sum arrives through a pointer.
func TestRespondRendersEveryFloatTheWayPythonDoes(t *testing.T) {
	sortOrder := 65535.0
	payload := gin.H{
		"sort_order": 65535.0,
		"nested":     gin.H{"total_estimate_points": 0.0},
		"list":       []any{1.0, 2.5},
		"pointer":    &sortOrder,
		"typed":      []float64{3.0},
		"integer":    7,
	}
	body := recordResponse(t, payload)
	for _, expected := range []string{
		`"sort_order":65535.0`,
		`"total_estimate_points":0.0`,
		`"list":[1.0,2.5]`,
		`"pointer":65535.0`,
		`"typed":[3.0]`,
		`"integer":7`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("the body is missing %s:\n%s", expected, body)
		}
	}
}

// A number that came out of a jsonb column keeps whatever it was stored as. Django gets the blob's own parse from psycopg, so an integer in view_props stays an integer, and rewriting it as a float would be a new kind of wrong.
func TestADecodedBlobKeepsItsOwnNumbers(t *testing.T) {
	body := recordResponse(t, gin.H{"view_props": DecodeJSON([]byte(`{"count": 3, "ratio": 0.5, "big": 12345678901234567890}`))})
	for _, expected := range []string{`"count":3`, `"ratio":0.5`, `"big":12345678901234567890`} {
		if !strings.Contains(body, expected) {
			t.Errorf("the body is missing %s:\n%s", expected, body)
		}
	}
	if DecodeJSON(nil) != nil || DecodeJSON([]byte("not json")) != nil {
		t.Error("an unreadable blob must decode to null, the way the previous decoder did")
	}
}
