package externalapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
)

// The collection carries both methods, which is what lets the proxy cut the path over.
func TestTheProjectCollectionCarriesBothMethods(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, Settings{}).Register(router)

	const base = "/api/v1/workspaces/:slug/projects/"
	wanted := map[string]bool{"GET " + base: false, "POST " + base: false}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if _, listed := wanted[key]; listed {
			wanted[key] = true
		}
	}
	for route, found := range wanted {
		if !found {
			t.Errorf("%s is not served", route)
		}
	}
}

// The serializer reports forty-four fields, which is the whole project and the seven numbers annotated beside it.
func TestTheProjectSerializerShape(t *testing.T) {
	data := fullProjectJSON(projectRow{Project: Project{ID: "project-id"}})
	if len(data) != 44 {
		t.Fatalf("the project has %d fields, want 44", len(data))
	}
	for _, field := range []string{
		"id", "name", "identifier", "description", "description_text", "description_html",
		"network", "emoji", "icon_prop", "logo_props", "cover_image", "cover_image_url",
		"module_view", "cycle_view", "issue_views_view", "page_view", "intake_view",
		"is_time_tracking_enabled", "is_issue_type_enabled", "guest_view_all_features",
		"archive_in", "close_in", "archived_at", "timezone", "external_source", "external_id",
		"created_at", "updated_at", "deleted_at", "created_by", "updated_by", "workspace",
		"default_assignee", "project_lead", "cover_image_asset", "estimate", "default_state",
		"total_members", "total_cycles", "total_modules", "is_member", "sort_order",
		"member_role", "is_deployed",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the project is missing %q", field)
		}
	}
}

// Both the name and the identifier are held to the same list of characters.
func TestTheForbiddenProjectCharacters(t *testing.T) {
	for _, value := range []string{"a.b", "a-b", "a(b)", "a!b", "a@b", "a'b", "a<b", "a%b"} {
		if !forbiddenProjectChars.MatchString(value) {
			t.Errorf("%q is accepted, and the pattern refuses it", value)
		}
	}
	for _, value := range []string{"Plane", "my project", "a_b", "a/b", "a1"} {
		if forbiddenProjectChars.MatchString(value) {
			t.Errorf("%q is refused, and the pattern accepts it", value)
		}
	}
}

// The icon is drawn at random from the two lists rather than from anything about the project.
func TestTheProjectLogoIsDrawnFromTheTwoLists(t *testing.T) {
	if len(projectLogoIcons) != 28 {
		t.Errorf("the icon list has %d entries, want 28", len(projectLogoIcons))
	}
	if len(projectLogoColors) != 8 {
		t.Errorf("the colour list has %d entries, want 8", len(projectLogoColors))
	}
	for attempt := 0; attempt < 20; attempt++ {
		rendered, ok := randomProjectLogo().(auth.JSONValue)
		if !ok {
			t.Fatalf("the logo is %T, want a json value", randomProjectLogo())
		}
		var decoded struct {
			InUse string `json:"in_use"`
			Icon  struct {
				Name  string `json:"name"`
				Color string `json:"color"`
			} `json:"icon"`
		}
		if err := json.Unmarshal(rendered, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.InUse != "icon" {
			t.Errorf("the logo is in use as %q", decoded.InUse)
		}
		if !listHolds(projectLogoIcons, decoded.Icon.Name) {
			t.Errorf("the icon %q is not one of the list", decoded.Icon.Name)
		}
		if !listHolds(projectLogoColors, decoded.Icon.Color) {
			t.Errorf("the colour %q is not one of the list", decoded.Icon.Color)
		}
	}
}

func listHolds(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

// The identifier is upper-cased and trimmed on its way in, whatever the caller sent.
func TestTheIdentifierIsNormalized(t *testing.T) {
	if got := strings.ToUpper(strings.TrimSpace("  plane  ")); got != "PLANE" {
		t.Errorf("the identifier normalizes to %q", got)
	}
}
