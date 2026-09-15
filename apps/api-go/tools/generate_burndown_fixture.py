"""Dumps what analytics_plot.burndown_plot writes for a chart, over made-up windows and completion lists.

The loop is lifted out of the function rather than called through it, because the real one needs a database for everything except the arithmetic this covers: which days are null, what is subtracted on each day, and whether the number is an int or a float.

Run from apps/api: python ../api-go/tools/generate_burndown_fixture.py > ../api-go/internal/project/testdata/burndown.tsv
"""

import json
import random
from datetime import date, timedelta


def chart(start, end, total, completions, plot_type, today):
    """The body of burndown_plot after the queries, copied line for line."""
    date_range = [start + timedelta(days=x) for x in range((end - start).days + 1)]
    chart_data = {str(day): 0 for day in date_range}
    for day in date_range:
        cumulative_pending_issues = total
        total_completed = 0
        if plot_type == "points":
            total_completed = sum(
                float(item[1]) for item in completions if item[0] is not None and item[0] <= day
            )
        else:
            total_completed = sum(item[1] for item in completions if item[0] is not None and item[0] <= day)
        cumulative_pending_issues -= total_completed
        if day > today:
            chart_data[str(day)] = None
        else:
            chart_data[str(day)] = cumulative_pending_issues
    return chart_data


TODAY = date(2026, 3, 15)
CASES = []

# A window entirely behind today, one straddling it, one entirely ahead, and a single day.
WINDOWS = [
    (date(2026, 3, 1), date(2026, 3, 7)),
    (date(2026, 3, 10), date(2026, 3, 20)),
    (date(2026, 4, 1), date(2026, 4, 5)),
    (date(2026, 3, 15), date(2026, 3, 15)),
    (date(2026, 2, 26), date(2026, 3, 2)),
]

random.seed(20260916)
for start, end in WINDOWS:
    for plot_type in ("issues", "points"):
        # Nothing completed at all, which is where Python's integer zero shows up in the points chart.
        CASES.append((start, end, 0 if plot_type == "points" else 0, [], plot_type))
        # Completions on days inside and outside the window, plus the never-completed row the loop skips.
        completions = []
        for _ in range(6):
            offset = random.randint(-5, 12)
            value = round(random.uniform(0.5, 8.0), 1) if plot_type == "points" else random.randint(1, 4)
            completions.append((start + timedelta(days=offset), value))
        completions.append((None, 3 if plot_type == "issues" else 2.5))
        if plot_type == "points":
            total = sum(float(value) for _, value in completions)
        else:
            total = random.randint(0, 30)
        CASES.append((start, end, total, completions, plot_type))

print("start\tend\ttotal\ttotal_is_float\tplot_type\tcompletions\tchart")
for start, end, total, completions, plot_type in CASES:
    encoded = json.dumps([[None if day is None else str(day), value] for day, value in completions])
    print(
        "%s\t%s\t%s\t%s\t%s\t%s\t%s"
        % (
            start,
            end,
            json.dumps(total),
            json.dumps(isinstance(total, float)),
            plot_type,
            encoded,
            json.dumps(chart(start, end, total, completions, plot_type, TODAY), sort_keys=True),
        )
    )
