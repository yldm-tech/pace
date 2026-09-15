// Package drf renders values the way Django REST Framework does, for the parts where Go's own encoding disagrees.
package drf

import (
	"strconv"
	"time"
)

// Time is a datetime rendered the way DRF renders one.
//
// Go's encoding/json writes a time as RFC 3339 with trailing zeros trimmed from the fraction, so 03:04:05.120000 comes out as `03:04:05.12Z`. Python's isoformat pads the fraction to six digits whenever it is non-zero and omits it entirely when it is zero, so the same instant is `03:04:05.120000Z`. Around a tenth of all timestamps land on a trailing zero, so the difference is not theoretical.
//
// Both DRF paths agree on this shape: DateTimeField.to_representation with the default ISO_8601 format, and the JSONEncoder the renderer falls back to for a raw values() dict. Neither is configured away in plane/settings, so one type covers both.
type Time time.Time

// ISO8601 is the rendering itself, without the surrounding JSON quotes.
func ISO8601(value time.Time) string {
	// Python writes the offset as +HH:MM and spells a zero offset Z; Go's Z07:00 layout does the same.
	layout := "2006-01-02T15:04:05Z07:00"
	if value.Nanosecond() != 0 {
		// The fraction is always six digits when present, so a microsecond value ending in a zero keeps it.
		layout = "2006-01-02T15:04:05.000000Z07:00"
	}
	return value.Format(layout)
}

func (value Time) MarshalJSON() ([]byte, error) {
	return strconv.AppendQuote(make([]byte, 0, 40), ISO8601(time.Time(value))), nil
}

// At returns a pointer to the rendered time, or nil, which is how a nullable column reaches a response.
func At(value *time.Time) *Time {
	if value == nil {
		return nil
	}
	rendered := Time(*value)
	return &rendered
}
