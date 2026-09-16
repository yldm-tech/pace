package project

import (
	"bufio"
	_ "embed"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
)

// The timezone list the app offers, copied from the endpoint that declares it and diffed against it by CI.
//
//go:embed timezones.tsv
var timezoneListTSV string

func (handler *Handler) registerTimezoneRoutes(router gin.IRouter) {
	// Anybody may read this, signed in or not, because the sign-up screen offers it.
	router.GET("/api/timezones/", handler.timezoneList)
}

// timezoneLocation is one row of the list: what a person sees, and what it means.
type timezoneLocation struct{ Label, Identifier string }

func timezoneLocations() []timezoneLocation {
	locations := make([]timezoneLocation, 0, 120)
	scanner := bufio.NewScanner(strings.NewReader(timezoneListTSV))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		label, identifier, found := strings.Cut(line, "\t")
		if !found {
			continue
		}
		locations = append(locations, timezoneLocation{Label: label, Identifier: identifier})
	}
	return locations
}

// timezoneList is every timezone the app offers, ordered by how far each is from UTC right now.
//
// "Right now" is the point: a zone's offset moves with daylight saving, so the order is worked out per request rather than baked in, and the same request answers differently in January and in July. A zone the system does not know is left out rather than failing the request.
func (handler *Handler) timezoneList(c *gin.Context) {
	now := handler.clock()
	type entry struct {
		offset  int
		label   string
		payload gin.H
	}
	entries := make([]entry, 0, 120)
	for _, location := range timezoneLocations() {
		zone, err := time.LoadLocation(location.Identifier)
		if err != nil {
			continue
		}
		_, seconds := now.In(zone).Zone()
		// Floor division and floor modulo, which is what python's // and % do — and which is why a zone west of Greenwich on a half hour is rendered an hour further out than it is. Marquesas is UTC-09:30 and this calls it UTC-10:30. Reproduced rather than corrected.
		hours := floorDiv(seconds, 3600)
		minutes := floorMod(seconds, 3600) / 60
		sign := "+"
		if hours < 0 {
			sign = "-"
		}
		rendered := sign + twoDigits(abs(hours)) + ":" + twoDigits(minutes)
		entries = append(entries, entry{
			// The sort key is strftime's %z read as a number, so -0430 sorts as minus four hundred and thirty rather than as minus four and a half hours. It is dropped before the response is built.
			offset: numericOffset(seconds),
			label:  location.Label,
			payload: gin.H{
				"utc_offset": "UTC" + rendered,
				"gmt_offset": "GMT" + rendered,
				"value":      location.Identifier,
				"label":      location.Label,
			},
		})
	}
	sort.SliceStable(entries, func(first, second int) bool {
		if entries[first].offset != entries[second].offset {
			return entries[first].offset < entries[second].offset
		}
		return entries[first].label < entries[second].label
	})
	results := make([]gin.H, 0, len(entries))
	for _, item := range entries {
		results = append(results, item.payload)
	}
	drf.Respond(c, http.StatusOK, gin.H{"timezones": results})
}

// numericOffset is int(strftime("%z")): the four digits read as a whole number, sign included. A half-hour zone therefore sorts thirty past the hour above it rather than halfway to the next one, which is what orders Kathmandu the way the list orders it.
func numericOffset(seconds int) int {
	hours := seconds / 3600
	minutes := abs(seconds%3600) / 60
	value := abs(hours)*100 + minutes
	if seconds < 0 {
		return -value
	}
	return value
}

// floorDiv and floorMod are python's // and %, which round toward negative infinity where Go rounds toward zero.
func floorDiv(value, divisor int) int {
	quotient := value / divisor
	if value%divisor != 0 && (value < 0) != (divisor < 0) {
		quotient--
	}
	return quotient
}

func floorMod(value, divisor int) int {
	remainder := value % divisor
	if remainder != 0 && (remainder < 0) != (divisor < 0) {
		remainder += divisor
	}
	return remainder
}

func twoDigits(value int) string {
	if value < 10 {
		return "0" + string(rune('0'+value))
	}
	return string(rune('0'+value/10)) + string(rune('0'+value%10))
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
