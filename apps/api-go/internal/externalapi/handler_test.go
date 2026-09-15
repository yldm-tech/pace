package externalapi

import (
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// DRF reads only the first letter of the period, so "minute", "min" and "m" are the same thing and so are "month" and "millisecond".
func TestTheRateNotationReadsOneLetter(t *testing.T) {
	for rate, want := range map[string]time.Duration{
		"60/minute":  time.Minute,
		"60/min":     time.Minute,
		"60/m":       time.Minute,
		"60/month":   time.Minute,
		"100/second": time.Second,
		"100/s":      time.Second,
		"10/hour":    time.Hour,
		"10/h":       time.Hour,
		"1000/day":   24 * time.Hour,
		"1000/d":     24 * time.Hour,
	} {
		count, window, ok := parseRate(rate)
		if !ok {
			t.Errorf("%q did not parse", rate)
			continue
		}
		if window != want {
			t.Errorf("%q is a window of %s, want %s", rate, window, want)
		}
		if count <= 0 {
			t.Errorf("%q parsed to a count of %d", rate, count)
		}
	}
}

// A rate nobody can parse is no limit at all, which is what an unset throttle does.
func TestAnUnparseableRateIsNoLimit(t *testing.T) {
	for _, rate := range []string{"", "60", "/minute", "sixty/minute", "0/minute", "-1/minute", "60/year", "60/"} {
		if _, _, ok := parseRate(rate); ok {
			t.Errorf("%q must not parse", rate)
		}
	}
}

// The fields parameter narrows what a serializer renders, and an empty one narrows nothing.
func TestTheFieldsParameterNarrows(t *testing.T) {
	data := gin.H{"id": "state-id", "name": "Backlog", "color": "#fff"}
	if got := narrow(data, nil); len(got) != 3 {
		t.Errorf("no fields kept %d, want all three", len(got))
	}
	got := narrow(data, []string{"id", "name", "nonexistent"})
	if len(got) != 2 || got["id"] != "state-id" || got["name"] != "Backlog" {
		t.Errorf("narrowing produced %v", got)
	}
}

// The state serializer asks for every field, and nine of them are read-only.
func TestStateJSONShape(t *testing.T) {
	data := stateJSON(State{ID: "state-id"})
	for _, field := range []string{
		"id", "created_at", "updated_at", "created_by", "updated_by", "deleted_at",
		"name", "description", "color", "slug", "sequence", "group", "is_triage",
		"default", "external_source", "external_id", "project", "workspace",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the state is missing %q", field)
		}
	}
	if len(data) != 18 {
		t.Fatalf("the state has %d fields, want 18", len(data))
	}
}

// Nine fields are read-only, the slug among them, so a caller naming one changes nothing.
func TestTheReadOnlyStateFieldsAreNotWritable(t *testing.T) {
	state := State{Slug: "backlog", ProjectID: "project-id", WorkspaceID: "workspace-id", IsTriage: true}
	applyStatePayload(&state, map[string]any{
		"name": "Renamed", "slug": "elsewhere", "project": "elsewhere",
		"workspace": "elsewhere", "is_triage": false,
	})
	if state.Slug != "backlog" || state.ProjectID != "project-id" || state.WorkspaceID != "workspace-id" {
		t.Errorf("a read-only field was written: %+v", state)
	}
	if !state.IsTriage {
		t.Error("is_triage is not writable through the serializer")
	}
	if state.Name != "Renamed" {
		t.Error("a writable field was not written")
	}
}

// Turning the default on clears it everywhere else; turning it off does not.
func TestOnlyTurningTheDefaultOnClearsTheOthers(t *testing.T) {
	if !wantsDefault(map[string]any{"default": true}) {
		t.Error("turning it on must clear the others")
	}
	for _, payload := range []map[string]any{
		{"default": false}, {}, {"default": "true"}, {"default": nil},
	} {
		if wantsDefault(payload) {
			t.Errorf("%v must not clear the others", payload)
		}
	}
}
