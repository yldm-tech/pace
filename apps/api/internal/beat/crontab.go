// Package beat runs the Go replacement for Celery beat.
//
// Django configures beat_scheduler as django_celery_beat's DatabaseScheduler,
// so the schedule lives in the django_celery_beat_* tables and can be edited at
// runtime. Reading only the static beat_schedule from celery.py would silently
// drop every periodic task an operator added or retimed through the admin, so
// this package reads the tables and syncs the static entries into them exactly
// as DatabaseScheduler.setup_schedule does.
package beat

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// crontabFields are the five fields of a CrontabSchedule row, in the order
// celery's crontab_parser bounds them.
type crontabSpec struct {
	minutes      map[int]bool
	hours        map[int]bool
	daysOfMonth  map[int]bool
	monthsOfYear map[int]bool
	daysOfWeek   map[int]bool
	location     *time.Location
}

var weekdayNames = map[string]int{
	"sunday": 0, "sun": 0, "monday": 1, "mon": 1, "tuesday": 2, "tue": 2,
	"wednesday": 3, "wed": 3, "thursday": 4, "thu": 4, "friday": 5, "fri": 5,
	"saturday": 6, "sat": 6,
}

var monthNames = map[string]int{
	"january": 1, "jan": 1, "february": 2, "feb": 2, "march": 3, "mar": 3,
	"april": 4, "apr": 4, "may": 5, "june": 6, "jun": 6, "july": 7, "jul": 7,
	"august": 8, "aug": 8, "september": 9, "sep": 9, "october": 10, "oct": 10,
	"november": 11, "nov": 11, "december": 12, "dec": 12,
}

// parseCrontab builds a spec from a CrontabSchedule row. The bounds mirror
// celery's crontab_parser calls: minute(60), hour(24), day_of_month(31, 1),
// month_of_year(12, 1) and day_of_week(7).
func parseCrontab(minute, hour, dayOfMonth, monthOfYear, dayOfWeek, timezone string) (crontabSpec, error) {
	location := time.UTC
	if timezone != "" {
		loaded, err := time.LoadLocation(timezone)
		if err != nil {
			return crontabSpec{}, fmt.Errorf("crontab timezone %q: %w", timezone, err)
		}
		location = loaded
	}
	spec := crontabSpec{location: location}
	var err error
	if spec.minutes, err = parseCrontabField(minute, 0, 60, nil); err != nil {
		return crontabSpec{}, fmt.Errorf("minute %q: %w", minute, err)
	}
	if spec.hours, err = parseCrontabField(hour, 0, 24, nil); err != nil {
		return crontabSpec{}, fmt.Errorf("hour %q: %w", hour, err)
	}
	if spec.daysOfMonth, err = parseCrontabField(dayOfMonth, 1, 31, nil); err != nil {
		return crontabSpec{}, fmt.Errorf("day_of_month %q: %w", dayOfMonth, err)
	}
	if spec.monthsOfYear, err = parseCrontabField(monthOfYear, 1, 12, monthNames); err != nil {
		return crontabSpec{}, fmt.Errorf("month_of_year %q: %w", monthOfYear, err)
	}
	// Celery's parser bounds day_of_week at 0 to 6 and raises on a literal 7,
	// even though django_celery_beat's help text offers "Sunday is 0 or 7". A
	// row storing 7 therefore fails here exactly as it fails in celery beat,
	// and the scheduler reports it rather than guessing at Sunday.
	if spec.daysOfWeek, err = parseCrontabField(dayOfWeek, 0, 7, weekdayNames); err != nil {
		return crontabSpec{}, fmt.Errorf("day_of_week %q: %w", dayOfWeek, err)
	}
	return spec, nil
}

// parseCrontabField reproduces celery's crontab_parser: comma separated parts,
// each of which is a star, a star with a step, a range, a range with a step, or
// a single value. A range whose end is below its start wraps around.
func parseCrontabField(spec string, minimum, maximum int, names map[string]int) (map[int]bool, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("empty part")
	}
	values := map[int]bool{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty part")
		}
		expanded, err := parseCrontabPart(part, minimum, maximum, names)
		if err != nil {
			return nil, err
		}
		for _, value := range expanded {
			values[value] = true
		}
	}
	return values, nil
}

func parseCrontabPart(part string, minimum, maximum int, names map[string]int) ([]int, error) {
	body, step := part, 1
	if index := strings.Index(part, "/"); index >= 0 {
		body = part[:index]
		parsed, err := strconv.Atoi(part[index+1:])
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("empty filter")
		}
		step = parsed
	}
	var expanded []int
	switch {
	case body == "*":
		// celery expands the star as range(min_, max_ + min_).
		for value := minimum; value < maximum+minimum; value++ {
			expanded = append(expanded, value)
		}
	case strings.Contains(body, "-"):
		bounds := strings.SplitN(body, "-", 2)
		from, err := expandCrontabNumber(bounds[0], minimum, maximum, names)
		if err != nil {
			return nil, err
		}
		to, err := expandCrontabNumber(bounds[1], minimum, maximum, names)
		if err != nil {
			return nil, err
		}
		if to < from {
			for value := from; value < minimum+maximum; value++ {
				expanded = append(expanded, value)
			}
			for value := minimum; value <= to; value++ {
				expanded = append(expanded, value)
			}
		} else {
			for value := from; value <= to; value++ {
				expanded = append(expanded, value)
			}
		}
	default:
		value, err := expandCrontabNumber(body, minimum, maximum, names)
		if err != nil {
			return nil, err
		}
		expanded = append(expanded, value)
	}
	if step == 1 {
		return expanded, nil
	}
	// celery applies the step by slicing the expanded list, not by testing
	// divisibility, so "5-20/7" yields 5, 12, 19.
	stepped := make([]int, 0, len(expanded)/step+1)
	for index := 0; index < len(expanded); index += step {
		stepped = append(stepped, expanded[index])
	}
	return stepped, nil
}

func expandCrontabNumber(token string, minimum, maximum int, names map[string]int) (int, error) {
	token = strings.TrimSpace(token)
	if strings.HasPrefix(token, "-") {
		return 0, fmt.Errorf("negative numbers not supported")
	}
	value, err := strconv.Atoi(token)
	if err != nil {
		if names == nil {
			return 0, fmt.Errorf("invalid literal %q", token)
		}
		named, known := names[strings.ToLower(token)]
		if !known {
			return 0, fmt.Errorf("invalid literal %q", token)
		}
		value = named
	}
	if highest := minimum + maximum - 1; value > highest {
		return 0, fmt.Errorf("invalid end range: %d > %d", value, highest)
	}
	if value < minimum {
		return 0, fmt.Errorf("invalid beginning range: %d < %d", value, minimum)
	}
	return value, nil
}

// matches reports whether the instant falls on the schedule. Celery requires
// both the day of month and the day of week to match, which is stricter than
// vixie cron's or semantics.
func (spec crontabSpec) matches(instant time.Time) bool {
	local := instant.In(spec.location)
	if !spec.minutes[local.Minute()] || !spec.hours[local.Hour()] {
		return false
	}
	if !spec.monthsOfYear[int(local.Month())] {
		return false
	}
	return spec.daysOfMonth[local.Day()] && spec.daysOfWeek[int(local.Weekday())]
}

// nextAfter returns the first minute strictly after the given instant that the
// schedule matches.
func (spec crontabSpec) nextAfter(after time.Time) (time.Time, bool) {
	local := after.In(spec.location).Truncate(time.Minute).Add(time.Minute)
	// Four years covers every February 29 placement, so a schedule that can
	// ever fire will be found well inside the bound.
	limit := local.AddDate(4, 0, 0)
	for candidate := local; candidate.Before(limit); candidate = candidate.Add(time.Minute) {
		if spec.matches(candidate) {
			return candidate, true
		}
		// Skip the rest of the day quickly when the date cannot match at all.
		if !spec.monthsOfYear[int(candidate.Month())] || !spec.daysOfMonth[candidate.Day()] || !spec.daysOfWeek[int(candidate.Weekday())] {
			candidate = time.Date(candidate.Year(), candidate.Month(), candidate.Day(), 23, 59, 0, 0, spec.location)
		}
	}
	return time.Time{}, false
}

// sortedValues is used by tests and diagnostics.
func sortedValues(values map[int]bool) []int {
	result := make([]int, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}
