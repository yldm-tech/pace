package project

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

// GORM flattens one level of anonymous embedding and silently drops the next: a row struct embedding issueRow parses to only its own fields, so every column of the issue scans as a zero value with no error anywhere. Annotations therefore go on issueRow itself rather than into a second wrapper, and this pins both halves of that rule.
func TestIssueRowFlattensExactlyOneLevelOfEmbedding(t *testing.T) {
	parsed, err := schema.Parse(&issueRow{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	columns := map[string]bool{}
	for _, field := range parsed.Fields {
		columns[field.DBName] = true
	}
	for _, column := range []string{
		"id", "name", "priority", "sequence_id", "project_id", "workspace_id",
		"cycle_id", "link_count", "attachment_count", "sub_issues_count",
		"label_ids", "assignee_ids", "module_ids", "is_subscribed", "state_group",
	} {
		if !columns[column] {
			t.Errorf("issueRow does not scan %q", column)
		}
	}

	type wrapped struct {
		issueRow
		Extra *string `gorm:"column:extra"`
	}
	nested, err := schema.Parse(&wrapped{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if len(nested.Fields) != 1 {
		t.Fatalf("GORM now parses %d fields through two levels of embedding; a wrapper row struct may be safe again", len(nested.Fields))
	}
}
