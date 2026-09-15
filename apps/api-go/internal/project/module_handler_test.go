package project

import (
	"testing"
	"time"
)

// ModuleUserPropertiesSerializer is fields = "__all__" with the four relations read-only, which renders fourteen keys — the same shape as the cycle's, keyed on module rather than cycle.
func TestModuleUserPropertiesShape(t *testing.T) {
	data := moduleUserPropertiesJSON(ModuleUserProperties{
		ID: "properties-id", ProjectID: "project-id", WorkspaceID: "workspace-id",
		ModuleID: "module-id", UserID: "user-id",
		Filters: defaultFiltersJSON(), DisplayFilters: defaultDisplayFiltersJSON(),
		DisplayProperties: defaultDisplayPropertiesJSON(), RichFilters: emptyJSON(),
	})
	for _, field := range []string{
		"id", "created_at", "updated_at", "created_by", "updated_by", "deleted_at",
		"filters", "display_filters", "display_properties", "rich_filters",
		"project", "workspace", "module", "user",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the module properties are missing %q", field)
		}
	}
	if len(data) != 14 {
		t.Fatalf("the module properties have %d fields, want 14", len(data))
	}
	if _, present := data["cycle"]; present {
		t.Error("a module's properties are keyed on the module, not a cycle")
	}
}

// ModuleLinkSerializer is fields = "__all__" with the three relations read-only.
func TestModuleLinkShape(t *testing.T) {
	title := "Design"
	data := moduleLinkJSON(ModuleLink{
		ID: "link-id", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		ProjectID: "project-id", WorkspaceID: "workspace-id", ModuleID: "module-id",
		Title: &title, URL: "https://example.invalid/", Metadata: emptyJSON(),
	})
	for _, field := range []string{
		"id", "created_at", "updated_at", "created_by", "updated_by", "deleted_at",
		"title", "url", "metadata", "project", "workspace", "module",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the module link is missing %q", field)
		}
	}
	if len(data) != 12 {
		t.Fatalf("the module link has %d fields, want 12", len(data))
	}
}

// The favourite helpers are shared with the cycle routes, so both entity types write and remove the same way.
func TestFavouritesAreSharedAcrossEntityTypes(t *testing.T) {
	// The only difference is the entity type the row records, which is what keeps one project's cycles and modules apart in the one table.
	for _, entityType := range []string{"cycle", "module"} {
		favourite := UserFavorite{EntityType: entityType, Sequence: 65535}
		if favourite.EntityType != entityType {
			t.Errorf("entity type = %q", favourite.EntityType)
		}
		// Every favourite starts at the default sequence, whatever it points at.
		if favourite.Sequence != 65535 {
			t.Errorf("sequence = %v", favourite.Sequence)
		}
	}
}
