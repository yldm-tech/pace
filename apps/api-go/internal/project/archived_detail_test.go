package project

import (
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// The archived cycle detail is the archived list's projection plus five fields, and it is not the live detail's.
func TestTheArchivedCycleDetailProjection(t *testing.T) {
	row := cycleRow{}
	list := archivedCycleListJSON(row)
	detail := archivedCycleDetailJSON(row)

	for _, added := range []string{"sub_issues", "logo_props", "completed_estimate_points", "total_estimate_points", "created_by"} {
		if _, present := list[added]; present {
			t.Errorf("%s is in the archived list, and only the detail carries it", added)
		}
		if _, present := detail[added]; !present {
			t.Errorf("%s is missing from the archived detail", added)
		}
	}
	if len(detail) != len(list)+5 {
		t.Errorf("the detail has %d fields and the list has %d", len(detail), len(list))
	}
	// The three state counts the archived projection asks for are on both.
	for _, shared := range []string{"started_issues", "unstarted_issues", "backlog_issues", "archived_at"} {
		if _, present := detail[shared]; !present {
			t.Errorf("%s is missing from the archived detail", shared)
		}
	}
	// The live list carries these two and the archived projections carry neither.
	for _, absent := range []string{"version"} {
		if _, present := detail[absent]; present {
			t.Errorf("%s is in the archived detail, and the archived projection drops it", absent)
		}
	}
}

// The archived module detail is the module serializer's fields plus the six the detail serializer declares.
func TestTheArchivedModuleDetailProjection(t *testing.T) {
	row := moduleRow{}
	detail := archivedModuleDetailJSON(row, nil, time.UTC)
	for _, added := range []string{
		"link_module", "sub_issues", "backlog_estimate_points",
		"unstarted_estimate_points", "started_estimate_points", "cancelled_estimate_points",
		"archived_at",
	} {
		if _, present := detail[added]; !present {
			t.Errorf("%s is missing from the archived module detail", added)
		}
	}
	// An empty link list is a list rather than a null, which is what a nested many serializer gives.
	links, ok := detail["link_module"].([]gin.H)
	if !ok || len(links) != 0 {
		t.Errorf("the links are %#v rather than an empty list", detail["link_module"])
	}
}
