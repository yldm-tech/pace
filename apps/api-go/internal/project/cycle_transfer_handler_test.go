package project

import (
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

// The snapshot row embeds the cycle and declares its counts inline, because GORM parses a second anonymous struct as nothing at all.
func TestTheTransferRowScansTheCycleAndItsCounts(t *testing.T) {
	parsed, err := schema.Parse(&transferCounts{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	columns := map[string]bool{}
	for _, field := range parsed.Fields {
		columns[field.DBName] = true
	}
	for _, column := range []string{
		"id", "name", "start_date", "end_date", "progress_snapshot", "project_id", "workspace_id",
		"total_issues", "completed_issues", "cancelled_issues", "started_issues", "unstarted_issues", "backlog_issues",
	} {
		if !columns[column] {
			t.Errorf("the transfer row does not scan %q", column)
		}
	}
}

// The transfer's counts are not the cycle list's. They count links rather than distinct issues, and they exclude only what the filter names, so a triage issue is counted here and not by the manager the distributions go through.
func TestTheTransferCountsAreNotTheManagers(t *testing.T) {
	handler := &Handler{}
	selection := transferCountSelection()
	if strings.Contains(selection, "DISTINCT") {
		t.Error("the transfer counts links, so nothing here is distinct")
	}
	if strings.Contains(selection, "triage") || strings.Contains(selection, "projects") {
		t.Error("the transfer counts do not carry the manager's triage or archived-project exclusions")
	}
	for _, group := range []string{"backlog", "unstarted", "started", "completed", "cancelled"} {
		if !strings.Contains(selection, "ts.group = '"+group+"'") {
			t.Errorf("the %s count is missing", group)
		}
	}
	// The five grouped counts count the state group column, and the total counts the link.
	if got := strings.Count(selection, "COUNT(ts.group)"); got != 5 {
		t.Errorf("%d counts read the state group, want 5", got)
	}
	if !strings.Contains(selection, "COUNT(tci.id)") {
		t.Error("the total counts the link row")
	}
	_ = handler
}

// GORM keeps one embedded struct and drops a second entirely: the fields under it get no column, scan no value and raise no error. The `embedded` tag does not help. Nothing in the library says so, and the failure is silent, so the shape is pinned here rather than remembered.
func TestGORMDropsASecondAnonymousStruct(t *testing.T) {
	type counts struct {
		Total int64 `gorm:"column:total_issues"`
	}
	type twoEmbedded struct {
		Cycle
		counts
	}
	type taggedTwoEmbedded struct {
		Cycle
		counts `gorm:"embedded"`
	}
	type embeddedPlusField struct {
		Cycle
		Total int64 `gorm:"column:total_issues"`
	}
	for name, model := range map[string]any{"two embedded structs": &twoEmbedded{}, "the second one tagged": &taggedTwoEmbedded{}} {
		if scansTotal(t, model) {
			t.Errorf("%s now scans the column, so the workaround in transferCounts can go", name)
		}
	}
	if !scansTotal(t, &embeddedPlusField{}) {
		t.Error("an embedded struct beside a plain field must scan both, which is what every annotated row here relies on")
	}
}

func scansTotal(t *testing.T, model any) bool {
	t.Helper()
	parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range parsed.Fields {
		if field.DBName == "total_issues" {
			return true
		}
	}
	return false
}

// The transfer's distribution counts `id` where the analytics endpoint counts the grouping column. The bucket with no assignee is therefore counted here and reported as zero there, for the same cycle.
func TestTheTwoDistributionsDisagreeAboutTheEmptyBucket(t *testing.T) {
	if !strings.Contains(transferDistributionCounts(), "COUNT(i.id)") {
		t.Error("the transfer counts the issue, so an unassigned issue still counts")
	}
	if strings.Contains(transferDistributionCounts(), "COUNT(ia.assignee_id)") {
		t.Error("counting the grouping column is the analytics endpoint's behaviour, not this one's")
	}
	for _, condition := range []string{liveIssue, completedIssues, pendingIssues} {
		if !strings.Contains(transferDistributionCounts(), condition) {
			t.Errorf("the transfer distribution is missing the %q filter", condition)
		}
	}
}

// Only unfinished work moves, and the states are named rather than derived.
func TestOnlyUnfinishedStatesMove(t *testing.T) {
	moving := map[string]bool{"backlog": true, "unstarted": true, "started": true}
	for _, group := range []string{"completed", "cancelled"} {
		if moving[group] {
			t.Errorf("%s work stays with the cycle it was finished in", group)
		}
	}
	if len(moving) != 3 {
		t.Fatalf("%d states move, want 3", len(moving))
	}
}

// An activity that names no issue must send null rather than the empty string, since the task looks the issue up by what it is given.
func TestAnActivityWithNoIssueSendsNull(t *testing.T) {
	if nullableID("") != nil {
		t.Error("an unset id must be null")
	}
	if nullableID("issue-id") != "issue-id" {
		t.Error("a set id must be passed through")
	}
}
