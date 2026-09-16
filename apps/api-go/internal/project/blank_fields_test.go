package project

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestProjectFieldsFollowsBlankTrue pins projectFields to the Project model's own declarations in plane/db/models/project.py. DRF maps blank=True to CharField(allow_blank=True) and leaves everything else refusing "".
//
// The web client posts description:"" when creating a project with no description, which is every project created from the dialog, and the whole request came back 400 "This field may not be blank."
func TestProjectFieldsFollowsBlankTrue(t *testing.T) {
	gin.SetMode(gin.TestMode)

	blankAllowed := []string{"description", "emoji", "cover_image", "external_source", "external_id"}
	blankRefused := []string{"name", "identifier", "timezone"}

	// A body that is otherwise complete, so the field under test is the only thing that can fail it.
	base := map[string]any{"name": "A project", "identifier": "PROJ"}

	for _, field := range blankAllowed {
		t.Run(field+" is blank=True on the model", func(t *testing.T) {
			if status := postProjectFields(t, base, field, ""); status != 0 {
				t.Errorf("a blank %s was refused with %d, want it accepted", field, status)
			}
		})
	}
	for _, field := range blankRefused {
		t.Run(field+" is not blank=True", func(t *testing.T) {
			if status := postProjectFields(t, base, field, ""); status != http.StatusBadRequest {
				t.Errorf("a blank %s gave %d, want 400", field, status)
			}
		})
	}
}

// postProjectFields runs projectFields over a body and returns the status it wrote, or 0 when it accepted the body.
func postProjectFields(t *testing.T, base map[string]any, field, value string) int {
	t.Helper()
	body := map[string]any{}
	for key, existing := range base {
		body[key] = existing
	}
	body[field] = value

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/workspaces/a/projects/", bytes.NewReader(encoded))

	handler := &Handler{}
	if _, ok := handler.projectFields(c, raw, false); ok {
		return 0
	}
	return recorder.Code
}
