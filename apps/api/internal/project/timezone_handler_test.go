package project

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// The list the app offers is the one apps/api declares, in the same pairs.
func TestTheTimezoneList(t *testing.T) {
	locations := timezoneLocations()
	if len(locations) < 100 {
		t.Fatalf("only %d timezones are embedded", len(locations))
	}
	seen := map[string]bool{}
	for _, location := range locations {
		if location.Label == "" || location.Identifier == "" {
			t.Errorf("a location is missing its label or identifier: %#v", location)
		}
		if _, err := time.LoadLocation(location.Identifier); err != nil {
			t.Errorf("%s is not a zone this system knows: %v", location.Identifier, err)
		}
		if seen[location.Identifier] && location.Label == "" {
			t.Errorf("%s is listed twice", location.Identifier)
		}
		seen[location.Identifier] = true
	}
	if !seen["Pacific/Midway"] || !seen["Asia/Kathmandu"] {
		t.Error("the list is missing a zone the endpoint declares")
	}
}

// A zone west of Greenwich on a half hour is rendered an hour further out than it is, because the offset is worked out with floor division. Marquesas is UTC-09:30 and the endpoint calls it UTC-10:30.
func TestTheOffsetOfAHalfHourZoneWestOfGreenwich(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// A fixed January moment, so daylight saving cannot move the answer.
	moment := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	handler := &Handler{clock: func() time.Time { return moment }}
	router := gin.New()
	router.GET("/api/timezones/", handler.timezoneList)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/timezones/", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("the status is %d", recorder.Code)
	}
	var payload struct {
		Timezones []struct {
			UTCOffset string `json:"utc_offset"`
			GMTOffset string `json:"gmt_offset"`
			Value     string `json:"value"`
			Label     string `json:"label"`
		} `json:"timezones"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("parsing the response: %v", err)
	}
	if len(payload.Timezones) < 100 {
		t.Fatalf("only %d zones came back", len(payload.Timezones))
	}
	byValue := map[string]string{}
	for _, zone := range payload.Timezones {
		byValue[zone.Value] = zone.UTCOffset
		if zone.GMTOffset != "GMT"+zone.UTCOffset[3:] {
			t.Errorf("%s reports %q and %q, which do not agree", zone.Value, zone.UTCOffset, zone.GMTOffset)
		}
	}
	// Really UTC-09:30, reported an hour further out. Reproduced rather than corrected.
	if got := byValue["Pacific/Marquesas"]; got != "UTC-10:30" {
		t.Errorf("Marquesas reports %q rather than UTC-10:30", got)
	}
	// East of Greenwich the arithmetic works, so a half hour there is reported as it is.
	if got := byValue["Asia/Kolkata"]; got != "UTC+05:30" {
		t.Errorf("Kolkata reports %q rather than UTC+05:30", got)
	}
	if got := byValue["Asia/Kathmandu"]; got != "UTC+05:45" {
		t.Errorf("Kathmandu reports %q rather than UTC+05:45", got)
	}
	// Dublin in January is on UTC, which is what a whole-hour zone at zero looks like.
	if got := byValue["Europe/Dublin"]; got != "UTC+00:00" {
		t.Errorf("Dublin in January reports %q rather than UTC+00:00", got)
	}
}

// The order is by how far each zone is from UTC, with the sort key read as a four-digit number rather than as a duration — so a half-hour zone sorts thirty past its hour rather than halfway to the next.
func TestTheTimezoneOrder(t *testing.T) {
	cases := map[int]int{
		0: 0, 3600: 100, 19800: 530, 20700: 545,
		-3600: -100, -16200: -430, -34200: -930,
	}
	for seconds, want := range cases {
		if got := numericOffset(seconds); got != want {
			t.Errorf("%d seconds sorts as %d rather than %d", seconds, got, want)
		}
	}
	// Floor division and floor modulo, which is where the half-hour rendering comes from.
	if got := floorDiv(-16200, 3600); got != -5 {
		t.Errorf("the floor of -4.5 hours is %d", got)
	}
	if got := floorMod(-16200, 3600); got != 1800 {
		t.Errorf("the floor remainder is %d", got)
	}
}
