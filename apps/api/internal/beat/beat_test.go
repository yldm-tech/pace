package beat

import (
	"reflect"
	"testing"
	"time"
)

// The expectations below come from celery's own crontab_parser.
func TestCrontabParserMatchesCelery(t *testing.T) {
	for _, test := range []struct {
		field   string
		spec    string
		minimum int
		maximum int
		names   map[string]int
		want    []int
	}{
		{field: "minute", spec: "*/15", minimum: 0, maximum: 60, want: []int{0, 15, 30, 45}},
		{field: "minute", spec: "0,30", minimum: 0, maximum: 60, want: []int{0, 30}},
		// celery slices the expanded range, so the step is positional.
		{field: "minute", spec: "5-20/7", minimum: 0, maximum: 60, want: []int{5, 12, 19}},
		{field: "hour", spec: "*/6", minimum: 0, maximum: 24, want: []int{0, 6, 12, 18}},
		{field: "day_of_month", spec: "1,15", minimum: 1, maximum: 31, want: []int{1, 15}},
		// A range whose end is below its start wraps around.
		{field: "day_of_month", spec: "30-2", minimum: 1, maximum: 31, want: []int{1, 2, 30, 31}},
		{field: "month_of_year", spec: "jan-mar", minimum: 1, maximum: 12, names: monthNames, want: []int{1, 2, 3}},
		{field: "day_of_week", spec: "mon-fri", minimum: 0, maximum: 7, names: weekdayNames, want: []int{1, 2, 3, 4, 5}},
		{field: "day_of_week", spec: "sat,sun", minimum: 0, maximum: 7, names: weekdayNames, want: []int{0, 6}},
	} {
		got, err := parseCrontabField(test.spec, test.minimum, test.maximum, test.names)
		if err != nil {
			t.Errorf("%s %q: %v", test.field, test.spec, err)
			continue
		}
		if !reflect.DeepEqual(sortedValues(got), test.want) {
			t.Errorf("%s %q = %v, want %v", test.field, test.spec, sortedValues(got), test.want)
		}
	}
}

// django_celery_beat's help text says "Sunday is 0 or 7", but celery's parser
// bounds day_of_week at 0 to 6 and raises on a literal 7. Go has to raise too,
// or a row celery beat refuses to schedule would quietly start running here.
func TestDayOfWeekSevenIsRejectedLikeCelery(t *testing.T) {
	if _, err := parseCrontab("0", "0", "*", "*", "7", "UTC"); err == nil {
		t.Fatal("day_of_week 7 should be rejected, as celery rejects it")
	}
	if _, err := parseCrontab("0", "0", "*", "*", "1-7", "UTC"); err == nil {
		t.Fatal("day_of_week 1-7 should be rejected, as celery rejects it")
	}
	spec, err := parseCrontab("0", "0", "*", "*", "sun", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if !spec.daysOfWeek[0] {
		t.Fatalf("day_of_week sun = %v, want 0", sortedValues(spec.daysOfWeek))
	}
}

// The expectations come from celery's crontab.remaining_estimate with its clock
// frozen at the reference instant.
func TestNextRunMatchesCelery(t *testing.T) {
	for _, test := range []struct {
		fields [5]string
		from   string
		want   string
	}{
		{fields: [5]string{"*", "*", "*", "*", "*"}, from: "2026-09-15T12:00:00", want: "2026-09-15T12:01:00"},
		{fields: [5]string{"0", "0", "*", "*", "*"}, from: "2026-09-15T12:00:00", want: "2026-09-16T00:00:00"},
		{fields: [5]string{"*/5", "*", "*", "*", "*"}, from: "2026-09-15T12:00:00", want: "2026-09-15T12:05:00"},
		{fields: [5]string{"45", "3", "*", "*", "*"}, from: "2026-09-15T03:45:00", want: "2026-09-16T03:45:00"},
		// Leap day: the next February 29 is in 2028.
		{fields: [5]string{"0", "0", "29", "2", "*"}, from: "2026-02-27T00:00:00", want: "2028-02-29T00:00:00"},
		{fields: [5]string{"0", "0", "*", "*", "1"}, from: "2026-09-15T12:00:00", want: "2026-09-21T00:00:00"},
		{fields: [5]string{"0", "12", "1,15", "*", "*"}, from: "2026-09-30T23:59:00", want: "2026-10-01T12:00:00"},
		{fields: [5]string{"*", "*", "*", "*", "*"}, from: "2026-12-31T23:59:00", want: "2027-01-01T00:00:00"},
	} {
		spec, err := parseCrontab(test.fields[0], test.fields[1], test.fields[2], test.fields[3], test.fields[4], "UTC")
		if err != nil {
			t.Fatalf("%v: %v", test.fields, err)
		}
		from, err := time.Parse("2006-01-02T15:04:05", test.from)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := spec.nextAfter(from.UTC())
		if !ok {
			t.Errorf("%v from %s: no next run", test.fields, test.from)
			continue
		}
		if formatted := got.UTC().Format("2006-01-02T15:04:05"); formatted != test.want {
			t.Errorf("%v from %s = %s, want %s", test.fields, test.from, formatted, test.want)
		}
	}
}

func TestCrontabHonoursItsTimezone(t *testing.T) {
	// A row scheduled for 03:00 Asia/Shanghai is 19:00 UTC the day before.
	spec, err := parseCrontab("0", "3", "*", "*", "*", "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, time.September, 15, 12, 0, 0, 0, time.UTC)
	next, ok := spec.nextAfter(from)
	if !ok {
		t.Fatal("no next run")
	}
	if got := next.UTC().Format("2006-01-02T15:04:05"); got != "2026-09-15T19:00:00" {
		t.Fatalf("next run = %s, want 2026-09-15T19:00:00", got)
	}
}

func TestStaticScheduleMatchesCeleryPy(t *testing.T) {
	entries := StaticSchedule(360)
	if len(entries) != 12 {
		t.Fatalf("static schedule has %d entries, want the 12 in celery.py", len(entries))
	}
	byName := map[string]StaticEntry{}
	for _, entry := range entries {
		byName[entry.Name] = entry
	}
	for name, want := range map[string]struct{ task, minute, hour string }{
		"check-every-five-minutes-to-send-email-notifications": {"plane.bgtasks.email_notification_task.stack_email_notification", "*/5", "*"},
		"check-every-day-to-delete-hard-delete":                {"plane.bgtasks.deletion_task.hard_delete", "0", "0"},
		"check-every-day-to-delete-api-logs":                   {"plane.bgtasks.cleanup_task.delete_api_logs", "30", "2"},
		"check-every-day-to-delete-webhook-logs":               {"plane.bgtasks.cleanup_task.delete_webhook_logs", "30", "3"},
		"check-every-day-to-delete-exporter-history":           {"plane.bgtasks.exporter_expired_task.delete_old_s3_link", "45", "3"},
	} {
		entry, known := byName[name]
		if !known {
			t.Errorf("static schedule is missing %q", name)
			continue
		}
		if entry.Task != want.task || entry.Minute != want.minute || entry.Hour != want.hour {
			t.Errorf("%q = %+v, want task %s at %s %s", name, entry, want.task, want.minute, want.hour)
		}
	}
	// The metrics push is the one interval entry.
	metrics := byName["push-instance-metrics"]
	if metrics.IntervalMinutes != 360 || metrics.Minute != "" {
		t.Fatalf("metrics entry = %+v, want a 360 minute interval", metrics)
	}
	if StaticSchedule(0)[1].IntervalMinutes != 360 || StaticSchedule(20_000_000)[1].IntervalMinutes != 360 {
		t.Fatal("an out of range interval should fall back to 360, as celery.py does")
	}
	if StaticSchedule(5)[1].IntervalMinutes != 5 {
		t.Fatal("a valid interval should be honoured")
	}
}

func TestEntryDueRespectsTheGates(t *testing.T) {
	spec, err := parseCrontab("0", "0", "*", "*", "*", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	midnight := time.Date(2026, time.September, 15, 0, 0, 0, 0, time.UTC)
	yesterday := midnight.AddDate(0, 0, -1)

	entry := Entry{Enabled: true, Crontab: &spec, LastRunAt: &yesterday}
	if !entry.due(midnight) {
		t.Fatal("a crontab entry should be due at its next matching minute")
	}
	if entry.due(midnight.Add(-time.Minute)) {
		t.Fatal("a crontab entry should not be due before its time")
	}

	disabled := entry
	disabled.Enabled = false
	if disabled.due(midnight) {
		t.Fatal("a disabled entry is never due")
	}

	future := midnight.AddDate(0, 0, 1)
	notStarted := entry
	notStarted.StartTime = &future
	if notStarted.due(midnight) {
		t.Fatal("an entry before its start time is not due")
	}

	past := midnight.AddDate(0, 0, -1)
	expired := entry
	expired.Expires = &past
	if expired.due(midnight) {
		t.Fatal("an expired entry is not due")
	}
}

func TestIntervalEntriesUseTheirPeriod(t *testing.T) {
	last := time.Date(2026, time.September, 15, 12, 0, 0, 0, time.UTC)
	every := int64(6)
	entry := Entry{Enabled: true, IntervalEvery: &every, IntervalPeriod: "hours", LastRunAt: &last}
	if entry.due(last.Add(5 * time.Hour)) {
		t.Fatal("an interval entry should not be due early")
	}
	if !entry.due(last.Add(6 * time.Hour)) {
		t.Fatal("an interval entry should be due after its period")
	}
	for period, want := range map[string]time.Duration{
		"microseconds": time.Microsecond, "seconds": time.Second,
		"minutes": time.Minute, "hours": time.Hour, "days": 24 * time.Hour,
	} {
		got, ok := intervalDuration(period, 1)
		if !ok || got != want {
			t.Errorf("intervalDuration(%q) = %v, %v", period, got, ok)
		}
	}
	if _, ok := intervalDuration("fortnights", 1); ok {
		t.Fatal("an unknown period should be rejected rather than guessed at")
	}
}

// TestNeverRunEntryFollowsDjango pins what a null last_run_at means, which is not "due now".
//
// The three rows below were put through django_celery_beat's own ModelEntry.is_due against a Django-migrated database, with last_run_at forced back to null, and these are the answers it gave. An earlier reading of the code had a never-run task due immediately, which fired all twelve of the schedule's tasks the first time beat started.
func TestNeverRunEntryFollowsDjango(t *testing.T) {
	spec, err := parseCrontab("0", "0", "*", "*", "*", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.September, 15, 12, 34, 0, 0, time.UTC)
	written := now.Add(-time.Minute)
	anHourAgo := now.Add(-time.Hour)
	inAnHour := now.Add(time.Hour)

	for _, test := range []struct {
		name      string
		startTime *time.Time
		due       bool
	}{
		// date_changed is a minute ago, so midnight has not come round again.
		{name: "no start time", due: false},
		// Backdated thirty years, so the crontab is due and the start time gate lets it through.
		{name: "start time an hour ago", startTime: &anHourAgo, due: true},
		// Due on the schedule for the same reason, but the start time gate holds it.
		{name: "start time in an hour", startTime: &inAnHour, due: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			entry := Entry{Enabled: true, Crontab: &spec, DateChanged: &written, StartTime: test.startTime}
			if got := entry.due(now); got != test.due {
				t.Fatalf("due = %v, want %v", got, test.due)
			}
		})
	}
}
