package project

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

// IssueDetailSerializer declares is_intake, but only
// IssueDetailIdentifierEndpoint annotates it. DRF makes a read-only field not
// required, so get_attribute raises SkipField and the key is dropped rather
// than erroring, which means these routes simply do not return it.
func TestIssueDetailOmitsIsIntake(t *testing.T) {
	data := issueDetailJSON(sampleIssueRow(), true)
	if _, present := data["is_intake"]; present {
		t.Fatal("is_intake is not annotated on these routes, so it must be absent")
	}
	if _, present := data["is_subscribed"]; !present {
		t.Fatal("is_subscribed is annotated on retrieve and must be present")
	}
}

// The update path builds current_instance from a queryset that never annotates
// is_subscribed, so that key is absent from the snapshot the activity carries.
func TestUpdateSnapshotOmitsIsSubscribed(t *testing.T) {
	data := issueDetailJSON(sampleIssueRow(), false)
	if _, present := data["is_subscribed"]; present {
		t.Fatal("the update snapshot must not carry is_subscribed")
	}
}

func TestIssueDetailSerializationShape(t *testing.T) {
	data := issueDetailJSON(sampleIssueRow(), true)
	for _, field := range []string{
		"id", "name", "state_id", "sort_order", "completed_at", "estimate_point",
		"priority", "start_date", "target_date", "sequence_id", "project_id",
		"parent_id", "cycle_id", "module_ids", "label_ids", "assignee_ids",
		"sub_issues_count", "created_at", "updated_at", "created_by", "updated_by",
		"attachment_count", "link_count", "is_draft", "archived_at",
		"description_html", "is_subscribed",
	} {
		if _, ok := data[field]; !ok {
			t.Errorf("serialized issue is missing %q", field)
		}
	}
	if len(data) != 27 {
		t.Fatalf("serialized issue has %d fields, want 27", len(data))
	}
	// Dates are DateFields in Django, so they render without a time part.
	if data["start_date"] != "2026-09-15" {
		t.Fatalf("start_date = %v, want a bare date", data["start_date"])
	}
	// The counts come back as zero rather than null when the subquery matched
	// nothing, which is what Django's IntegerField does with a null annotation.
	empty := sampleIssueRow()
	empty.LinkCount, empty.AttachmentCount, empty.SubIssuesCount = nil, nil, nil
	empty.LabelIDs, empty.AssigneeIDs, empty.ModuleIDs = nil, nil, nil
	bare := issueDetailJSON(empty, true)
	if bare["link_count"] != int64(0) || bare["sub_issues_count"] != int64(0) {
		t.Fatalf("counts = %v, %v", bare["link_count"], bare["sub_issues_count"])
	}
	encoded, err := json.Marshal(bare)
	if err != nil {
		t.Fatal(err)
	}
	// The id arrays must serialize as [] rather than null.
	if !strings.Contains(string(encoded), `"label_ids":[]`) {
		t.Fatalf("serialized issue = %s", encoded)
	}
}

func TestIssueValidationMatchesDjango(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{name: "blank name", body: `{"name":"   "}`, want: `{"name":["This field may not be blank."]}`},
		{name: "invalid priority", body: `{"priority":"critical"}`, want: `{"priority":["\"critical\" is not a valid choice."]}`},
		{name: "bad date", body: `{"start_date":"15-09-2026"}`, want: `{"start_date":["Date has wrong format. Use one of these formats instead: YYYY-MM-DD."]}`},
		{name: "start after target", body: `{"start_date":"2026-09-20","target_date":"2026-09-15"}`, want: `{"non_field_errors":["Start date cannot exceed target date"]}`},
		{name: "assignee_ids not a list", body: `{"assignee_ids":"someone"}`, want: `{"assignee_ids":["Expected a list of items but got type \"str\"."]}`},
		{name: "bad label uuid", body: `{"label_ids":["nope"]}`, want: `{"label_ids":["“nope” is not a valid UUID."]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			handler := NewHandler(nil, nil, Settings{})
			router.PATCH("/issues", func(c *gin.Context) {
				var body map[string]json.RawMessage
				if err := c.ShouldBindJSON(&body); err != nil {
					handler.invalidDetail(c)
					return
				}
				_, _ = handler.issueFields(c, body, "project-id")
			})
			request := httptest.NewRequest(http.MethodPatch, "/issues", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || response.Body.String() != test.want {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

// description_html goes through the same sanitizer the comment routes use.
func TestIssueDescriptionIsSanitized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler(nil, nil, Settings{})
	var parsed issueInput
	router.PATCH("/issues", func(c *gin.Context) {
		var body map[string]json.RawMessage
		if err := c.ShouldBindJSON(&body); err != nil {
			handler.invalidDetail(c)
			return
		}
		parsed, _ = handler.issueFields(c, body, "project-id")
	})
	request := httptest.NewRequest(http.MethodPatch, "/issues",
		strings.NewReader(`{"description_html":"<p>keep<script>alert(1)</script></p>"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if parsed.values["description_html"] != "<p>keep</p>" {
		t.Fatalf("description_html = %v", parsed.values["description_html"])
	}
}

func TestIssuePriorityChoicesMatchTheModel(t *testing.T) {
	for _, valid := range []string{"urgent", "high", "medium", "low", "none"} {
		if !validIssuePriority(valid) {
			t.Errorf("%q should be a valid priority", valid)
		}
	}
	for _, invalid := range []string{"critical", "", "NONE", "highest"} {
		if validIssuePriority(invalid) {
			t.Errorf("%q should not be a valid priority", invalid)
		}
	}
}

func sampleIssueRow() issueRow {
	now := time.Date(2026, time.September, 15, 4, 0, 0, 0, time.UTC)
	start := time.Date(2026, time.September, 15, 0, 0, 0, 0, time.UTC)
	state := "state-id"
	count := int64(2)
	return issueRow{
		Issue: Issue{
			ID: "issue-id", CreatedAt: now, UpdatedAt: now, ProjectID: "project-id",
			WorkspaceID: "workspace-id", StateID: &state, Name: "Something broke",
			Priority: "high", SequenceID: 42, SortOrder: 65535,
			DescriptionHTML: "<p>details</p>", StartDate: &start,
		},
		LinkCount: &count, AttachmentCount: &count, SubIssuesCount: &count,
		LabelIDs: pq.StringArray{"label-a"}, AssigneeIDs: pq.StringArray{"user-a"},
		ModuleIDs: pq.StringArray{}, IsSubscribed: true,
	}
}
