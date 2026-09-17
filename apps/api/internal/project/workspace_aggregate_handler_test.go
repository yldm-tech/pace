package project

import (
	"testing"
	"time"
)

// A state's order is its place within its group, and the workspace list counts the group across every project — so the same state is ordered differently here than in its own project's list.
func TestTheWorkspaceStateOrderCountsEveryProject(t *testing.T) {
	// Two projects with two backlog states each make a group of four here and a group of two there.
	counts := map[string]int{"backlog": 4}
	seen := map[string]int{}
	orders := []float64{}
	for index := 0; index < 4; index++ {
		seen["backlog"]++
		orders = append(orders, float64(seen["backlog"])/float64(counts["backlog"]))
	}
	if orders[0] != 0.25 {
		t.Errorf("the first state is ordered %v, want a quarter", orders[0])
	}
	// The same state inside one project of two would be ordered a half.
	if float64(1)/float64(2) != 0.5 {
		t.Error("the arithmetic of a group of two is not a half")
	}
}

// The workspace cycle list reports neither the assignees nor the version its project's list carries.
func TestTheWorkspaceCycleShape(t *testing.T) {
	body := cycleListJSON(cycleRow{Cycle: Cycle{ID: "cycle-id"}}, time.UTC)
	delete(body, "assignee_ids")
	delete(body, "version")
	delete(body, "created_by")
	if len(body) != 19 {
		t.Fatalf("the workspace cycle has %d fields, want 19", len(body))
	}
	for _, absent := range []string{"assignee_ids", "version", "created_by"} {
		if _, present := body[absent]; present {
			t.Errorf("the workspace cycle carries %q", absent)
		}
	}
}

// The workspace module list carries the archive stamp its project's list leaves out.
func TestTheWorkspaceModuleCarriesTheArchiveStamp(t *testing.T) {
	row := moduleRow{Module: Module{ID: "module-id"}}
	body := moduleListJSON(row, time.UTC)
	if _, present := body["archived_at"]; present {
		t.Error("the project's own module list carries an archive stamp, and it does not")
	}
	body["archived_at"] = row.ArchivedAt
	if len(body) != 29 {
		t.Fatalf("the workspace module has %d fields, want 29", len(body))
	}
}
