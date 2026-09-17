package beat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// The django_celery_beat tables, named by Django's default convention since the
// package sets no db_table.
const (
	periodicTaskTable     = "django_celery_beat_periodictask"
	periodicTasksTable    = "django_celery_beat_periodictasks"
	crontabScheduleTable  = "django_celery_beat_crontabschedule"
	intervalScheduleTable = "django_celery_beat_intervalschedule"
)

// Entry is one row of django_celery_beat_periodictask with its schedule
// resolved.
type Entry struct {
	ID             int64
	Name           string
	Task           string
	Args           string
	Kwargs         string
	Queue          *string
	Enabled        bool
	OneOff         bool
	StartTime      *time.Time
	Expires        *time.Time
	ExpireSeconds  *int64
	LastRunAt      *time.Time
	DateChanged    *time.Time
	TotalRunCount  int64
	Crontab        *crontabSpec
	IntervalEvery  *int64
	IntervalPeriod string
}

// due reports whether the entry should run at now, given how Django's
// ModelEntry.is_due gates on enabled, start_time and expiry.
func (entry Entry) due(now time.Time) bool {
	if !entry.Enabled {
		return false
	}
	if entry.StartTime != nil && now.Before(*entry.StartTime) {
		return false
	}
	if entry.Expires != nil && !now.Before(*entry.Expires) {
		return false
	}
	next, ok := entry.nextRun(now)
	if !ok {
		return false
	}
	return !now.Before(next)
}

// neverRunBackdate is how far Django pushes the reference into the past for a task that has never run and has a start time. It is thirty years, written out the way ModelEntry does it, and the size is the point: the schedule is then due whatever it is, so the start time gate above is what actually decides when the task first fires.
const neverRunBackdate = 365 * 30 * 24 * time.Hour

// nextRun is the instant this entry is next allowed to fire, measured from the last run.
//
// A task that has never run has no last run to measure from, and Django does not treat that as "due now". ModelEntry fills the null in from date_changed — which auto_now holds at the moment the row was last written — so a task created by the scheduler's own sync waits a full period before its first run rather than firing the moment beat starts. Getting this wrong fires every task in the schedule at boot, which is what a fresh install did before this was written down.
//
// The exception is a task with a start time, which Django backdates by thirty years instead. That makes the schedule due immediately and hands the decision to the start time gate in due, so such a task fires at its start time rather than at the first slot after it.
func (entry Entry) nextRun(now time.Time) (time.Time, bool) {
	reference := entry.LastRunAt
	if reference == nil {
		fallback := now
		if entry.DateChanged != nil {
			fallback = *entry.DateChanged
		}
		if entry.StartTime != nil {
			fallback = fallback.Add(-neverRunBackdate)
		}
		reference = &fallback
	}
	switch {
	case entry.Crontab != nil:
		return entry.Crontab.nextAfter(*reference)
	case entry.IntervalEvery != nil:
		step, ok := intervalDuration(entry.IntervalPeriod, *entry.IntervalEvery)
		if !ok {
			return time.Time{}, false
		}
		return reference.Add(step), true
	default:
		return time.Time{}, false
	}
}

// intervalDuration maps IntervalSchedule.period onto a duration. The names are
// celery's own period constants.
func intervalDuration(period string, every int64) (time.Duration, bool) {
	unit, known := map[string]time.Duration{
		"microseconds": time.Microsecond,
		"seconds":      time.Second,
		"minutes":      time.Minute,
		"hours":        time.Hour,
		"days":         24 * time.Hour,
	}[period]
	if !known {
		return 0, false
	}
	return time.Duration(every) * unit, true
}

type Store struct{ db *gorm.DB }

func NewStore(db *gorm.DB) *Store { return &Store{db: db} }

// Entries reads every enabled periodic task with its schedule resolved. A row
// on a solar or clocked schedule is reported, because silently skipping one
// would mean a task an operator configured simply never runs.
func (store *Store) Entries(ctx context.Context) ([]Entry, []string, error) {
	var rows []struct {
		ID             int64      `gorm:"column:id"`
		Name           string     `gorm:"column:name"`
		Task           string     `gorm:"column:task"`
		Args           string     `gorm:"column:args"`
		Kwargs         string     `gorm:"column:kwargs"`
		Queue          *string    `gorm:"column:queue"`
		Enabled        bool       `gorm:"column:enabled"`
		OneOff         bool       `gorm:"column:one_off"`
		StartTime      *time.Time `gorm:"column:start_time"`
		Expires        *time.Time `gorm:"column:expires"`
		ExpireSeconds  *int64     `gorm:"column:expire_seconds"`
		LastRunAt      *time.Time `gorm:"column:last_run_at"`
		DateChanged    *time.Time `gorm:"column:date_changed"`
		TotalRunCount  int64      `gorm:"column:total_run_count"`
		SolarID        *int64     `gorm:"column:solar_id"`
		ClockedID      *int64     `gorm:"column:clocked_id"`
		Minute         *string    `gorm:"column:minute"`
		Hour           *string    `gorm:"column:hour"`
		DayOfMonth     *string    `gorm:"column:day_of_month"`
		MonthOfYear    *string    `gorm:"column:month_of_year"`
		DayOfWeek      *string    `gorm:"column:day_of_week"`
		CrontabTZ      *string    `gorm:"column:crontab_timezone"`
		IntervalEvery  *int64     `gorm:"column:every"`
		IntervalPeriod *string    `gorm:"column:period"`
	}
	err := store.db.WithContext(ctx).Table(periodicTaskTable + " pt").
		Select(`pt.id, pt.name, pt.task, pt.args, pt.kwargs, pt.queue, pt.enabled, pt.one_off,
			pt.start_time, pt.expires, pt.expire_seconds, pt.last_run_at, pt.date_changed, pt.total_run_count,
			pt.solar_id, pt.clocked_id,
			c.minute, c.hour, c.day_of_month, c.month_of_year, c.day_of_week, c.timezone AS crontab_timezone,
			i.every, i.period`).
		Joins("LEFT JOIN " + crontabScheduleTable + " c ON c.id = pt.crontab_id").
		Joins("LEFT JOIN " + intervalScheduleTable + " i ON i.id = pt.interval_id").
		Where("pt.enabled = TRUE").Scan(&rows).Error
	if err != nil {
		return nil, nil, fmt.Errorf("read periodic tasks: %w", err)
	}
	entries := make([]Entry, 0, len(rows))
	var unsupported []string
	for _, row := range rows {
		entry := Entry{
			ID: row.ID, Name: row.Name, Task: row.Task, Args: row.Args, Kwargs: row.Kwargs,
			Queue: row.Queue, Enabled: row.Enabled, OneOff: row.OneOff,
			StartTime: row.StartTime, Expires: row.Expires, ExpireSeconds: row.ExpireSeconds,
			LastRunAt: row.LastRunAt, DateChanged: row.DateChanged, TotalRunCount: row.TotalRunCount,
		}
		switch {
		case row.Minute != nil:
			timezone := "UTC"
			if row.CrontabTZ != nil && *row.CrontabTZ != "" {
				timezone = *row.CrontabTZ
			}
			spec, err := parseCrontab(*row.Minute, orEmpty(row.Hour), orEmpty(row.DayOfMonth),
				orEmpty(row.MonthOfYear), orEmpty(row.DayOfWeek), timezone)
			if err != nil {
				unsupported = append(unsupported, fmt.Sprintf("%s: %v", row.Name, err))
				continue
			}
			entry.Crontab = &spec
		case row.IntervalEvery != nil:
			entry.IntervalEvery = row.IntervalEvery
			entry.IntervalPeriod = orEmpty(row.IntervalPeriod)
			if _, ok := intervalDuration(entry.IntervalPeriod, *entry.IntervalEvery); !ok {
				unsupported = append(unsupported, fmt.Sprintf("%s: unknown interval period %q", row.Name, entry.IntervalPeriod))
				continue
			}
		case row.SolarID != nil:
			unsupported = append(unsupported, row.Name+": solar schedules are not supported")
			continue
		case row.ClockedID != nil:
			unsupported = append(unsupported, row.Name+": clocked schedules are not supported")
			continue
		default:
			unsupported = append(unsupported, row.Name+": has no schedule")
			continue
		}
		entries = append(entries, entry)
	}
	return entries, unsupported, nil
}

// MarkRun writes back what Django's sync does: the run timestamp and the count.
func (store *Store) MarkRun(ctx context.Context, entry Entry, now time.Time) error {
	updates := map[string]any{
		"last_run_at":     now,
		"total_run_count": entry.TotalRunCount + 1,
	}
	if entry.OneOff {
		// Django disables a one-off entry once it has run.
		updates["enabled"] = false
	}
	return store.db.WithContext(ctx).Table(periodicTaskTable).
		Where("id = ?", entry.ID).Updates(updates).Error
}

// LastChange reads django_celery_beat_periodictasks, the single row whose
// timestamp django_celery_beat bumps whenever a schedule is edited.
func (store *Store) LastChange(ctx context.Context) (time.Time, error) {
	var row struct {
		LastUpdate time.Time `gorm:"column:last_update"`
	}
	err := store.db.WithContext(ctx).Table(periodicTasksTable).
		Order("last_update DESC").Limit(1).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return time.Time{}, nil
	}
	return row.LastUpdate, err
}

// decodedArgs reads the JSON celery stores in the args and kwargs columns.
func decodedArgs(entry Entry) ([]any, map[string]any) {
	arguments := []any{}
	if entry.Args != "" {
		_ = json.Unmarshal([]byte(entry.Args), &arguments)
	}
	keywords := map[string]any{}
	if entry.Kwargs != "" {
		_ = json.Unmarshal([]byte(entry.Kwargs), &keywords)
	}
	return arguments, keywords
}

func orEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
