package workspace

import "testing"

// The project endpoint's entity table differs from the workspace one by a single line: a draft issue description writes a column here and writes none there.
func TestTheProjectEntityTableDiffersByOneLine(t *testing.T) {
	if got := projectEntityColumn("DRAFT_ISSUE_DESCRIPTION"); got != "draft_issue_id" {
		t.Errorf("a draft description writes %q here, want draft_issue_id", got)
	}
	if got := entityColumn("DRAFT_ISSUE_DESCRIPTION"); got != "" {
		t.Errorf("a draft description writes %q on the workspace route, and Django writes nothing", got)
	}
	// Everything else agrees between the two.
	for _, entity := range []string{
		"WORKSPACE_LOGO", "PROJECT_COVER", "USER_AVATAR", "USER_COVER",
		"ISSUE_ATTACHMENT", "ISSUE_DESCRIPTION", "PAGE_DESCRIPTION", "COMMENT_DESCRIPTION",
		"DRAFT_ISSUE_ATTACHMENT",
	} {
		if projectEntityColumn(entity) != entityColumn(entity) {
			t.Errorf("%s writes %q here and %q there", entity, projectEntityColumn(entity), entityColumn(entity))
		}
	}
}

// A claim against an entity that has been deleted since the upload is a foreign key failure, which is swallowed rather than reported.
func TestADeletedEntityIsSwallowed(t *testing.T) {
	if !isForeignKeyViolation(errorOf(`pq: insert or update on table "file_assets" violates foreign key constraint "file_assets_issue_id_fkey"`)) {
		t.Error("a foreign key failure is not recognised")
	}
	if isForeignKeyViolation(errorOf("connection refused")) {
		t.Error("an unrelated failure is read as a foreign key one")
	}
	// It is not the same failure as a clash, which is reported.
	if isForeignKeyViolation(errorOf(`pq: duplicate key value violates unique constraint "x"`)) {
		t.Error("a clash is read as a foreign key failure")
	}
}

type stringError string

func (err stringError) Error() string { return string(err) }

func errorOf(message string) error { return stringError(message) }
