package project

import (
	"strings"
	"time"
)

// analyticsWindow is one half of an analytics date range: the moment it opens and the moment it closes, both inclusive.
type analyticsWindow struct {
	from time.Time
	to   time.Time
}

// analyticsFilters is get_analytics_filters: who is asking, which projects they narrowed to, and whichever of the two date shapes the route wanted.
//
// The two shapes are not interchangeable. The analytics one is a pair of timestamps compared against created_at; the chart one is a pair of dates compared against created_at's date. A route asks for one of them and gets nothing for the other.
type analyticsFilters struct {
	slug       string
	userID     string
	projectIDs []string
	// window is the analytics date range, which is absent unless the route asked for the analytics shape and the caller named a filter it recognises.
	window *analyticsWindow
	// period is the chart date range, in whole days.
	period *chartPeriod
}

type chartPeriod struct {
	start time.Time
	end   time.Time
}

// newAnalyticsFilters reads the query string the way get_analytics_filters does. An unrecognised date_filter leaves both ranges empty rather than being an error.
func newAnalyticsFilters(slug, userID, projectIDs, dateFilter, shape string, now time.Time) analyticsFilters {
	filters := analyticsFilters{slug: slug, userID: userID}
	if projectIDs != "" {
		filters.projectIDs = strings.Split(projectIDs, ",")
	}
	switch shape {
	case "analytics":
		filters.window = analyticsDateRange(dateFilter, now)
	case "chart":
		filters.period = chartPeriodRange(dateFilter, now)
	}
	return filters
}

// analyticsDateRange is get_analytics_date_range, minus the previous period — the comparison count it was built for is commented out upstream, so only the current half is ever read.
//
// Each window opens at the first instant of its first day and closes at the last microsecond of its last, which is how datetime.min.time() and datetime.max.time() land in UTC.
func analyticsDateRange(dateFilter string, now time.Time) *analyticsWindow {
	if dateFilter == "" {
		return nil
	}
	today := now.UTC().Truncate(24 * time.Hour)
	dayStart := func(offset int) time.Time { return today.AddDate(0, 0, offset) }
	dayEnd := func(offset int) time.Time {
		return today.AddDate(0, 0, offset).Add(24*time.Hour - time.Microsecond)
	}
	switch dateFilter {
	case "yesterday":
		return &analyticsWindow{from: dayStart(-1), to: dayEnd(-1)}
	case "last_7_days":
		return &analyticsWindow{from: dayStart(-7), to: dayEnd(0)}
	case "last_30_days":
		return &analyticsWindow{from: dayStart(-30), to: dayEnd(0)}
	case "last_3_months":
		return &analyticsWindow{from: dayStart(-90), to: dayEnd(0)}
	}
	// The custom range needs a start and an end the endpoint never passes, so it is unreachable from here and falls through to nothing.
	return nil
}

// chartPeriodRange is get_chart_period_range: whole days rather than timestamps, and nothing at all for a filter it does not know.
func chartPeriodRange(dateFilter string, now time.Time) *chartPeriod {
	if dateFilter == "" {
		return nil
	}
	today := now.UTC().Truncate(24 * time.Hour)
	switch dateFilter {
	case "yesterday":
		return &chartPeriod{start: today.AddDate(0, 0, -1), end: today.AddDate(0, 0, -1)}
	case "last_7_days":
		return &chartPeriod{start: today.AddDate(0, 0, -7), end: today}
	case "last_30_days":
		return &chartPeriod{start: today.AddDate(0, 0, -30), end: today}
	case "last_3_months":
		return &chartPeriod{start: today.AddDate(0, 0, -90), end: today}
	}
	return nil
}

// baseScope is the base_filters predicate for any table that carries a workspace and a project.
//
// The project side is a single EXISTS rather than a join, which is what Django's one-row-per-member join comes to. It carries no soft-delete check on the membership: a related lookup does not go through the model's manager, so a deleted membership still counts the project in.
func (filters analyticsFilters) baseScope(alias string) (string, []any) {
	condition := `EXISTS (SELECT 1 FROM workspaces aw WHERE aw.id = ` + alias + `.workspace_id AND aw.slug = ?)
		AND EXISTS (SELECT 1 FROM projects ap JOIN project_members apm ON apm.project_id = ap.id
			WHERE ap.id = ` + alias + `.project_id AND apm.member_id = ? AND apm.is_active = TRUE
			AND ap.deleted_at IS NULL AND ap.archived_at IS NULL)`
	arguments := []any{filters.slug, filters.userID}
	if len(filters.projectIDs) > 0 {
		condition += " AND " + alias + ".project_id IN ?"
		arguments = append(arguments, filters.projectIDs)
	}
	return condition, arguments
}

// projectScope is project_filters, which is the same rule written against the project table itself.
func (filters analyticsFilters) projectScope(alias string) (string, []any) {
	condition := `EXISTS (SELECT 1 FROM workspaces aw WHERE aw.id = ` + alias + `.workspace_id AND aw.slug = ?)
		AND EXISTS (SELECT 1 FROM project_members apm WHERE apm.project_id = ` + alias + `.id
			AND apm.member_id = ? AND apm.is_active = TRUE)
		AND ` + alias + `.deleted_at IS NULL AND ` + alias + `.archived_at IS NULL`
	arguments := []any{filters.slug, filters.userID}
	if len(filters.projectIDs) > 0 {
		condition += " AND " + alias + ".id IN ?"
		arguments = append(arguments, filters.projectIDs)
	}
	return condition, arguments
}

// windowScope is what get_filtered_counts adds: a created_at between the two ends of the current period, and nothing when there is no period.
func (filters analyticsFilters) windowScope(alias string) (string, []any) {
	if filters.window == nil {
		return "", nil
	}
	return alias + ".created_at >= ? AND " + alias + ".created_at <= ?",
		[]any{filters.window.from, filters.window.to}
}

// periodScope is the chart's date range, compared against the date rather than the timestamp.
func (filters analyticsFilters) periodScope(alias string) (string, []any) {
	if filters.period == nil {
		return "", nil
	}
	return "(" + alias + ".created_at AT TIME ZONE 'UTC')::date >= ? AND (" + alias + ".created_at AT TIME ZONE 'UTC')::date <= ?",
		[]any{filters.period.start.Format("2006-01-02"), filters.period.end.Format("2006-01-02")}
}

// chartKeyName is the name a projects-chart entry carries, which is its key with the underscores taken out and each word capitalised.
func chartKeyName(key string) string {
	words := strings.Split(key, "_")
	for index, word := range words {
		if word == "" {
			continue
		}
		words[index] = strings.ToUpper(word[:1]) + strings.ToLower(word[1:])
	}
	return strings.Join(words, " ")
}
