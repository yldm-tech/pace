package project

import (
	"strings"
	"testing"
)

// A workspace's file is filed under the workspace and anybody else's under a "user-" prefix, with a fresh uuid between the two so two uploads of the same name do not collide.
func TestTheLegacyUploadPath(t *testing.T) {
	workspace := "11111111-1111-4111-8111-111111111111"
	got := legacyUploadPath(&workspace, "a picture.png")
	if !strings.HasPrefix(got, workspace+"/") {
		t.Errorf("a workspace file is filed at %q", got)
	}
	if !strings.HasSuffix(got, "-a_picture.png") && !strings.HasSuffix(got, "-a picture.png") {
		t.Errorf("the name is not on the end of %q", got)
	}
	// Two uploads of the same name do not collide.
	if second := legacyUploadPath(&workspace, "a picture.png"); second == got {
		t.Error("two uploads of the same name gave the same key")
	}

	loose := legacyUploadPath(nil, "notes.txt")
	if !strings.HasPrefix(loose, "user-") {
		t.Errorf("a file that belongs to nobody is filed at %q", loose)
	}
	if strings.Contains(loose, "/") {
		t.Errorf("a file that belongs to nobody has a folder in its key: %q", loose)
	}

	// A name that sanitises away is replaced by a bare uuid rather than left empty.
	empty := legacyUploadPath(nil, "...")
	if strings.HasSuffix(empty, "-") {
		t.Errorf("a name that sanitises away left %q", empty)
	}
}

// The columns beyond the file itself that the form may set, which is how an editor attaches an upload to a work item in the same call.
func TestTheWritableLegacyAssetFields(t *testing.T) {
	for field, column := range map[string]string{
		"entity_type": "entity_type", "issue": "issue_id",
		"project": "project_id", "page": "page_id", "comment": "comment_id",
	} {
		if legacyWritableFields[field] != column {
			t.Errorf("%s writes %q rather than %q", field, legacyWritableFields[field], column)
		}
	}
	// The three the serializer names read-only are not writable, and neither is the key itself.
	for _, readOnly := range []string{"created_by", "updated_by", "created_at", "updated_at", "asset", "size"} {
		if _, writable := legacyWritableFields[readOnly]; writable {
			t.Errorf("%s is writable and the serializer names it read-only", readOnly)
		}
	}
}
