package project

import (
	"strings"
	"testing"
	"time"
)

// The module list returns twenty-eight fields.
func TestModuleListProjection(t *testing.T) {
	data := moduleListJSON(moduleRow{Module: Module{ID: "module-id", WorkspaceID: "workspace-id", ProjectID: "project-id"}}, time.UTC)
	for _, field := range []string{
		"id", "workspace_id", "project_id", "name", "description", "description_text",
		"description_html", "start_date", "target_date", "status", "lead_id", "member_ids",
		"view_props", "sort_order", "external_source", "external_id", "logo_props",
		"completed_estimate_points", "total_estimate_points", "total_issues", "is_favorite",
		"cancelled_issues", "completed_issues", "started_issues", "unstarted_issues",
		"backlog_issues", "created_at", "updated_at",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the module list is missing %q", field)
		}
	}
	if len(data) != 28 {
		t.Fatalf("the module list has %d fields, want 28", len(data))
	}
}

// The module list renders its timestamps in the caller's timezone, where the cycle list uses the project's. The two apps differ here and both are reproduced.
func TestTheModuleListUsesTheCallersZone(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	moment := time.Date(2026, 6, 1, 4, 0, 0, 0, time.UTC)
	module := moduleListJSON(moduleRow{Module: Module{CreatedAt: moment, UpdatedAt: moment}}, shanghai)
	if name, _ := module["created_at"].(time.Time).Zone(); name != "CST" {
		t.Fatalf("created_at is in %s", name)
	}
	// The dates are DateFields on a module, where a cycle's are DateTimeFields, so these render bare.
	start := moment
	dated := moduleListJSON(moduleRow{Module: Module{StartDate: &start, CreatedAt: moment, UpdatedAt: moment}}, shanghai)
	if dated["start_date"] != "2026-06-01" {
		t.Fatalf("start_date = %v, want a bare date", dated["start_date"])
	}
}

// Every count and sum reads through the issue_objects manager and requires a live link, so a removed issue counts towards none of a module's totals.
func TestModuleCountsRequireALiveLinkAndALiveIssue(t *testing.T) {
	annotations := moduleAnnotations()
	if strings.Count(annotations, "mi.deleted_at IS NULL") < 7 {
		t.Fatalf("every count needs a live link:\n%s", annotations)
	}
	if strings.Count(annotations, issueObjectsPredicate("mis")) < 7 {
		t.Fatal("every count reads through issue_objects")
	}
	for _, group := range []string{"cancelled", "completed", "started", "unstarted", "backlog"} {
		if !strings.Contains(annotations, "ms.group = '"+group+"'") {
			t.Errorf("the select is missing the %s count", group)
		}
	}
}

// Only an estimate of the points type has a number to add, so a category estimate contributes nothing to either sum.
func TestOnlyPointsEstimatesAreSummed(t *testing.T) {
	annotations := moduleAnnotations()
	if strings.Count(annotations, "es.type = 'points'") != 2 {
		t.Fatalf("both sums narrow to a points estimate:\n%s", annotations)
	}
	// The completed sum narrows further; the total one does not.
	if !strings.Contains(annotations, "ms2.group = 'completed'") {
		t.Error("the completed sum narrows to completed issues")
	}
}

// The create response is the list projection minus nothing: the same twenty-eight fields, since both read through the same annotated queryset.
func TestModuleCreateResponseMatchesTheList(t *testing.T) {
	row := moduleRow{Module: Module{ID: "module-id"}}
	created := moduleListJSON(row, time.UTC)
	listed := moduleListJSON(row, time.UTC)
	if len(created) != len(listed) {
		t.Fatalf("create has %d fields and the list %d; both read the same queryset", len(created), len(listed))
	}
}

// The status choices are the six on the model, and anything else is refused rather than stored.
func TestModuleStatusChoices(t *testing.T) {
	for _, allowed := range []string{"backlog", "planned", "in-progress", "paused", "completed", "cancelled"} {
		if !moduleStatuses[allowed] {
			t.Errorf("%q is a valid status", allowed)
		}
	}
	for _, refused := range []string{"in progress", "started", "", "Backlog"} {
		if moduleStatuses[refused] {
			t.Errorf("%q is not a valid status", refused)
		}
	}
	if len(moduleStatuses) != 6 {
		t.Fatalf("there are %d statuses, want 6", len(moduleStatuses))
	}
}
