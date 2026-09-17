package project

import (
	"time"

	"github.com/gin-gonic/gin"
)

// burndownInput is what the chart needs about the entity it is drawn for: the window it covers and the total it counts down from.
type burndownInput struct {
	Start *time.Time
	End   *time.Time
	// Total is the issue count for the issues plot and the summed points for the points plot. The two are counted differently, which is the point of keeping it here rather than recomputing.
	Total float64
	// TotalIsFloat records whether there was anything to sum. Python's sum() over an empty list returns the integer zero, and an integer minus an integer stays an integer all the way into the body, so a points chart for a cycle holding no estimated issue is full of 0 rather than 0.0.
	TotalIsFloat bool
	// Completions are the rows the plot subtracts, one per completed issue for the issues plot and one per estimated issue for the points plot.
	Completions []burndownCompletion
	Points      bool
}

// burndownCompletion is a day and what was finished on it. The day is null for an issue that was never completed, which the chart skips rather than counts.
type burndownCompletion struct {
	Date  *time.Time
	Value float64
}

// burndownChart is analytics_plot.burndown_plot: one key per day of the window, holding what was still outstanding at the end of that day.
//
// A day in the future is null rather than a number, which is how the frontend knows where the drawn line stops. The comparison is against today in UTC, because Plane runs with TIME_ZONE set to UTC and TruncDate follows the active zone.
func burndownChart(input burndownInput, today time.Time) gin.H {
	chart := gin.H{}
	if input.Start == nil || input.End == nil {
		// No window means no days, and the empty object is what the endpoint returns.
		return chart
	}
	start := dayOf(*input.Start)
	end := dayOf(*input.End)
	// The range is inclusive of both ends, counted in whole days, which is what the timedelta loop produces.
	days := int(end.Sub(start).Hours()/24) + 1
	if days < 1 {
		return chart
	}
	deadline := dayOf(today)

	for offset := 0; offset < days; offset++ {
		day := start.AddDate(0, 0, offset)
		if day.After(deadline) {
			chart[day.Format("2006-01-02")] = nil
			continue
		}
		// Every completion up to and including this day is already gone from the outstanding total, so the sum is recomputed over the whole list rather than carried — which is what the Python does, and it matters for a list that is not sorted by day.
		var completed float64
		summed := false
		for _, completion := range input.Completions {
			if completion.Date == nil {
				continue
			}
			if !dayOf(*completion.Date).After(day) {
				completed += completion.Value
				summed = true
			}
		}
		outstanding := input.Total - completed
		if input.Points {
			if !input.TotalIsFloat && !summed {
				// Neither side had anything to add, so both are Python integers and so is the difference.
				chart[day.Format("2006-01-02")] = 0
				continue
			}
			chart[day.Format("2006-01-02")] = outstanding
			continue
		}
		// The issues plot counts whole issues, and Python keeps them integers all the way through.
		chart[day.Format("2006-01-02")] = int64(outstanding)
	}
	return chart
}

// dayOf drops the time of day. Every date in the chart is a UTC calendar date, both the window's and the completions'.
func dayOf(value time.Time) time.Time {
	utc := value.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}
