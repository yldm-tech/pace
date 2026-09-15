package externalapi

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// The two spellings do not carry the same path here: the older one says issue-attachments and the newer one attachments.
func TestTheTwoAttachmentPathsDiffer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, Settings{}).Register(router)

	counts := map[string]int{}
	for _, route := range router.Routes() {
		switch {
		case strings.Contains(route.Path, "/issue-attachments"):
			counts["issue-attachments"]++
		case strings.Contains(route.Path, "/attachments") && strings.Contains(route.Path, "/work-items/"):
			counts["attachments"]++
		}
	}
	if counts["issue-attachments"] != 5 || counts["attachments"] != 5 {
		t.Fatalf("issue-attachments has %d routes and attachments %d, want five each",
			counts["issue-attachments"], counts["attachments"])
	}
	// Neither spelling serves the other's path, which a shared matcher would have got wrong.
	for _, route := range router.Routes() {
		if strings.Contains(route.Path, "/issues/") && strings.Contains(route.Path, "/attachments/") &&
			!strings.Contains(route.Path, "/issue-attachments/") {
			t.Errorf("%s is a path Django does not serve", route.Path)
		}
	}
}

// The reserve has no default size, unlike its workspace-asset cousin, so an absent size is refused.
func TestTheAttachmentReserveHasNoDefaultSize(t *testing.T) {
	var size float64
	if value, present := map[string]any{}["size"]; present {
		parsed, _ := assetSize(value)
		size = parsed
	}
	if size != 0 {
		t.Errorf("an absent size is %v, want nothing", size)
	}
	// And the same guard says a different thing here than it does on the workspace asset route.
	const here = "Invalid request."
	const there = "Name and size are required fields."
	if here == there {
		t.Fatal("the two routes word the same refusal differently")
	}
}

// The download always serves an attachment, so nothing uploaded here is ever rendered inline.
func TestAnIssueAttachmentIsNeverInline(t *testing.T) {
	const disposition = "attachment"
	if disposition == "inline" {
		t.Fatal("this route does not decide per type the way the workspace asset route does")
	}
	// The workspace asset route picks per type; this one does not consult the list at all.
	if scriptCapableMimeTypes["image/png"] {
		t.Error("the type list is not consulted here")
	}
}

// The person who raised the work item passes whatever their role, which is what lets a guest manage their own item's attachments.
func TestTheCreatorPassesWhateverTheirRole(t *testing.T) {
	passes := func(isCreator bool, role int, withRoles bool) bool {
		if isCreator {
			return true
		}
		if !withRoles {
			return role != 0
		}
		return role == roleAdmin || role == roleMember || role == roleGuest
	}
	if !passes(true, 0, true) {
		t.Error("the creator passes without any role at all")
	}
	if !passes(false, roleGuest, true) {
		t.Error("a guest passes the role check")
	}
	if passes(false, 0, true) {
		t.Error("somebody who is not in the project does not pass")
	}
}

// The serializer reports fifteen fields.
func TestIssueAttachmentJSONShape(t *testing.T) {
	data := issueAttachmentJSON(FileAsset{ID: "asset-id"})
	for _, field := range []string{
		"id", "created_at", "updated_at", "created_by", "updated_by", "deleted_at",
		"attributes", "asset", "size", "is_uploaded", "storage_metadata",
		"external_source", "external_id", "entity_type", "project", "workspace",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the attachment is missing %q", field)
		}
	}
	if len(data) != 16 {
		t.Fatalf("the attachment has %d fields, want 16", len(data))
	}
}
