package worker

import (
	"testing"
)

func TestRelationGraphLoadsAndCoversTheMigratedModels(t *testing.T) {
	graph, err := loadRelationGraph()
	if err != nil {
		t.Fatal(err)
	}
	if graph.DjangoVersion == "" {
		t.Fatal("the relation graph should record the Django version it came from")
	}
	// The three models the migrated API routes already queue this task for.
	for key, table := range map[string]string{
		"db.workspace":         "workspaces",
		"db.workspacetheme":    "workspace_themes",
		"db.workspaceuserlink": "workspace_user_links",
		"db.project":           "projects",
	} {
		model, known := graph.Models[key]
		if !known {
			t.Fatalf("relation graph is missing %s", key)
		}
		if model.Table != table {
			t.Errorf("%s table = %q, want %q", key, model.Table, table)
		}
		if !model.SoftDeletes {
			t.Errorf("%s should soft delete", key)
		}
	}
}

// ProjectIdentifier stops at AuditModel, so its save is the plain Django one and
// it keeps its audit columns. Everything built on BaseModel loses them, because
// BaseModel.save blanks them when there is no current user, which is always the
// case inside a worker.
func TestOnlyBaseModelsClearTheirAuditColumns(t *testing.T) {
	graph, err := loadRelationGraph()
	if err != nil {
		t.Fatal(err)
	}
	identifier := graph.Models["db.projectidentifier"]
	if identifier.ClearsAuditUser {
		t.Error("ProjectIdentifier stops at AuditModel and must keep created_by")
	}
	if identifier.CreatedByColumn != "created_by_id" {
		t.Errorf("ProjectIdentifier created_by column = %q", identifier.CreatedByColumn)
	}
	theme := graph.Models["db.workspacetheme"]
	if !theme.ClearsAuditUser {
		t.Error("WorkspaceTheme is a BaseModel and loses its audit columns")
	}
}

func TestRelationGraphRecordsEveryOnDeleteBehaviour(t *testing.T) {
	graph, err := loadRelationGraph()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, model := range graph.Models {
		for _, relation := range model.Relations {
			seen[relation.OnDelete]++
			if relation.RelatedTable == "" || relation.RelatedColumn == "" {
				t.Fatalf("relation %s on %s is missing its table or column", relation.Accessor, relation.RelatedModel)
			}
		}
	}
	// The worker only knows how to handle these three; a new one would be
	// treated as a cascade, so fail loudly instead.
	for behaviour := range seen {
		switch behaviour {
		case "CASCADE", "SET_NULL", "DO_NOTHING":
		default:
			t.Errorf("relation graph carries unhandled on_delete %q", behaviour)
		}
	}
	for _, required := range []string{"CASCADE", "SET_NULL", "DO_NOTHING"} {
		if seen[required] == 0 {
			t.Errorf("relation graph has no %s relation, which looks wrong", required)
		}
	}
}

func TestWorkspaceCascadeReachesItsThemes(t *testing.T) {
	graph, err := loadRelationGraph()
	if err != nil {
		t.Fatal(err)
	}
	workspace := graph.Models["db.workspace"]
	var themes *relation
	for index, candidate := range workspace.Relations {
		if candidate.RelatedModel == "db.workspacetheme" {
			themes = &workspace.Relations[index]
			break
		}
	}
	if themes == nil {
		t.Fatal("deleting a workspace should reach its themes")
	}
	if themes.OnDelete != "CASCADE" {
		t.Fatalf("workspace to theme on_delete = %q, want CASCADE", themes.OnDelete)
	}
	if themes.RelatedColumn != "workspace_id" {
		t.Fatalf("workspace to theme column = %q", themes.RelatedColumn)
	}
}

func TestDeletionTaskIsRoutedToTheGoQueue(t *testing.T) {
	found := false
	for _, name := range MigratedTaskNames() {
		if name == SoftDeleteRelatedObjectsTask {
			found = true
		}
	}
	if !found {
		t.Fatal("soft_delete_related_objects is implemented but not routed to Go")
	}
}

func TestHardDeleteWindowRejectsANegativeConfiguration(t *testing.T) {
	tasks, err := NewDeletionTasks(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tasks.hardDeleteAfterDays != HardDeleteAfterDays {
		t.Fatalf("default window = %d", tasks.hardDeleteAfterDays)
	}
	// Zero is meaningful: everything already soft-deleted.
	tasks.SetHardDeleteAfterDays(0)
	if tasks.hardDeleteAfterDays != 0 {
		t.Fatalf("zero window = %d, want it honoured", tasks.hardDeleteAfterDays)
	}
	tasks.SetHardDeleteAfterDays(30)
	tasks.SetHardDeleteAfterDays(-1)
	if tasks.hardDeleteAfterDays != 30 {
		t.Fatalf("negative window = %d, want the previous value kept", tasks.hardDeleteAfterDays)
	}
}

func TestHardDeleteLeadModelsAllExistInTheGraph(t *testing.T) {
	graph, err := loadRelationGraph()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range hardDeleteLeadModels {
		model, known := graph.Models[key]
		if !known {
			t.Errorf("hard delete names %s, which is not in the relation graph", key)
			continue
		}
		if !model.SoftDeletes {
			t.Errorf("hard delete names %s, which has no deleted_at", key)
		}
	}
	if len(hardDeleteLeadModels) != 18 {
		t.Fatalf("hard delete lead models = %d, want the 18 Django lists", len(hardDeleteLeadModels))
	}
}

func TestHardDeleteIsRoutedToTheGoQueue(t *testing.T) {
	found := false
	for _, name := range MigratedTaskNames() {
		if name == HardDeleteTask {
			found = true
		}
	}
	if !found {
		t.Fatal("hard_delete is implemented but not routed to Go")
	}
}
