package worker

import (
	"testing"
	"time"
)

// A task carrying an eta waits until that moment, and one whose moment has passed runs straight away.
func TestTheETADelay(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	later := now.Add(5 * time.Minute).Format("2006-01-02T15:04:05.000000-07:00")
	if got := etaDelay(later, now); got != 5*time.Minute {
		t.Errorf("a future eta waits %v", got)
	}
	earlier := now.Add(-time.Minute).Format("2006-01-02T15:04:05.000000-07:00")
	if got := etaDelay(earlier, now); got != 0 {
		t.Errorf("a past eta waits %v", got)
	}
	// A message with no eta, or one in a shape this does not know, runs straight away rather than failing.
	for _, header := range []any{nil, "", "not a moment", 42} {
		if got := etaDelay(header, now); got != 0 {
			t.Errorf("%#v waits %v", header, got)
		}
	}
}

// The batch arguments fall back to the same numbers the task declares, and a value that arrived as text is read as a number.
func TestTheBatchArguments(t *testing.T) {
	if got := intArgument(nil, map[string]any{}, 0, "batch_size", defaultVersionBatchSize); got != 5000 {
		t.Errorf("the default batch is %d", got)
	}
	if got := intArgument(nil, map[string]any{"batch_size": float64(100)}, 0, "batch_size", 5000); got != 100 {
		t.Errorf("a given batch is %d", got)
	}
	// The management command reads its input as text and hands it straight over.
	if got := intArgument(nil, map[string]any{"batch_size": "250"}, 0, "batch_size", 5000); got != 250 {
		t.Errorf("a batch given as text is %d", got)
	}
	if got := intArgument(nil, map[string]any{"offset": float64(0)}, 1, "offset", 7); got != 0 {
		t.Errorf("an offset of zero is %d", got)
	}
}

// An edit may only name a column the version row really has, because save(update_fields=...) raises on one it does not.
func TestTheVersionColumns(t *testing.T) {
	for _, column := range []string{"name", "priority", "assignees", "cycle", "modules"} {
		if !issueVersionColumns[column] {
			t.Errorf("%s is not a version column, and the model declares it", column)
		}
	}
	// A work item has these and a version row does not, so naming one is what makes the save raise.
	for _, column := range []string{"description_html", "workspace_id", "sequence"} {
		if issueVersionColumns[column] {
			t.Errorf("%s reads as a version column, and the model does not declare it", column)
		}
	}
}
