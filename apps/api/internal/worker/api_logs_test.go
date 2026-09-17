package worker

import (
	"encoding/json"
	"testing"
	"time"
)

// A record becomes one row, with the columns that were never given left null rather than written as empty strings.
func TestTheRowOneRecordBecomes(t *testing.T) {
	now := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	row := apiLogRow(map[string]any{
		"token_identifier": "abc",
		"path":             "/api/v1/workspaces/acme/projects/",
		"method":           "POST",
		"query_params":     "",
		"headers":          "{'User-Agent': 'curl/8'}",
		"body":             nil,
		"response_code":    float64(201),
		"response_body":    `{"id":"made"}`,
		"ip_address":       "203.0.113.9",
		"user_agent":       nil,
	}, "the-id", now)

	if row["token_identifier"] != "abc" || row["method"] != "POST" || row["response_code"] != 201 {
		t.Errorf("the row is %#v", row)
	}
	// A request with no body and no agent leaves those columns null; a request with an empty query string leaves an empty string, since that is what was given.
	if row["body"] != nil || row["user_agent"] != nil {
		t.Errorf("a column that was never given is %#v / %#v", row["body"], row["user_agent"])
	}
	if row["query_params"] != "" {
		t.Errorf("the query is %#v", row["query_params"])
	}
	if row["created_at"] != now || row["updated_at"] != now || row["id"] != "the-id" {
		t.Errorf("the row's own columns are %#v", row)
	}
	// Nobody owns the record, so neither author column is written.
	if row["created_by_id"] != nil || row["updated_by_id"] != nil {
		t.Errorf("the record has an author: %#v", row)
	}
}

// A record that arrives with nothing at all still becomes a row, since the three not-null columns fall back to the empty string rather than refusing.
func TestARecordWithNothingInIt(t *testing.T) {
	row := apiLogRow(map[string]any{}, "the-id", time.Now())
	for _, column := range []string{"token_identifier", "path", "method"} {
		if row[column] != "" {
			t.Errorf("%s is %#v", column, row[column])
		}
	}
	if row["response_code"] != 0 {
		t.Errorf("the status is %#v", row["response_code"])
	}
	if row["headers"] != nil {
		t.Errorf("the headers are %#v", row["headers"])
	}
}

// The status survives whichever shape the json decoder left it in, since a record queued by the python side and one queued by the Go side do not agree on the type.
func TestTheStatusIsReadWhateverShapeItArrivesIn(t *testing.T) {
	for _, value := range []any{float64(404), 404, json.Number("404"), "404"} {
		if got := statusCodeOf(value); got != 404 {
			t.Errorf("%#v read as %d", value, got)
		}
	}
	for _, value := range []any{nil, "not a number", true} {
		if got := statusCodeOf(value); got != 0 {
			t.Errorf("%#v read as %d", value, got)
		}
	}
}
