package project

import (
	"strings"
	"testing"
)

// The list's projection is twenty-six fields, and its three counts stay as the raw annotations rather than being coalesced to zero.
func TestTheWorkspaceIssueProjection(t *testing.T) {
	data := viewIssueListJSON(issueListRow{})
	if len(data) != 26 {
		t.Fatalf("the projection has %d fields, want 26", len(data))
	}
	for _, key := range []string{"sub_issues_count", "attachment_count", "link_count"} {
		// The pointer is carried through as it is, so a count the query left null renders as null rather than as zero.
		count, ok := data[key].(*int64)
		if !ok || count != nil {
			t.Errorf("%q is %#v, want the raw annotation left as null", key, data[key])
		}
	}
	for _, key := range []string{"assignee_ids", "label_ids", "module_ids"} {
		list, ok := data[key].([]string)
		if !ok || list == nil {
			t.Errorf("%q is %#v, want an empty array", key, data[key])
		}
	}
}

// Unlike the project list, this one does not drop a module that has since been archived — its ids come from the prefetched links rather than from an aggregate that joins the module.
func TestTheModuleIdsKeepArchivedModules(t *testing.T) {
	annotations := workspaceIssueAnnotations()
	if strings.Contains(annotations, "m.archived_at IS NULL") {
		t.Error("the module ids exclude archived modules, and this list's do not")
	}
	if !strings.Contains(annotations, "module_issues mi WHERE mi.issue_id = i.id AND mi.deleted_at IS NULL") {
		t.Errorf("the module ids are read as %q", annotations)
	}
	// The project list's own annotations do exclude them, which is the difference worth keeping visible.
	if !strings.Contains(issueListAnnotations(), "m.archived_at IS NULL") {
		t.Error("the project list stopped excluding archived modules")
	}
}
