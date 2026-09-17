// Package cycles holds what both APIs do to a cycle's dates.
//
// A cycle is stored as two instants and written as two days. The conversion between them is the project's timezone, and it is not symmetric: a start becomes the first second of the day and an end becomes its last minute, so two cycles that meet do not read as overlapping.
package cycles

import "time"

// ConvertToUTC is convert_to_utc over a pair of days, which is what both the date check and the create run a cycle's dates through.
//
// A start date becomes the first second of that day in the project's timezone; an end date becomes 23:59 of it. Both are then read as instants.
//
// A start date that falls on today in the project's timezone is the exception: it becomes the current instant rather than the start of the day, so a cycle created this afternoon does not claim to have begun this morning.
func ConvertToUTC(start, end, timezone string, now time.Time) (time.Time, time.Time, bool) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	startDay, err := time.ParseInLocation("2006-01-02", start, location)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	endDay, err := time.ParseInLocation("2006-01-02", end, location)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	startAt := startDay.Add(time.Second)
	if SameDayIn(startAt, now, location) {
		startAt = now
	}
	// The end gains 23 hours and 59 minutes, which is what keeps two adjacent cycles from reading as overlapping.
	return startAt.UTC(), endDay.Add(23*time.Hour + 59*time.Minute).UTC(), true
}

// SameDayIn compares two instants by the calendar day they fall on in the given zone.
func SameDayIn(left, right time.Time, location *time.Location) bool {
	leftDay := left.In(location)
	rightDay := right.In(location)
	return leftDay.Year() == rightDay.Year() && leftDay.YearDay() == rightDay.YearDay()
}
