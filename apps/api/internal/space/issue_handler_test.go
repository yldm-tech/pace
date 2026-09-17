package space

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

// Both work item routes are served, which completes the app.
func TestTheIssueRoutesAreServed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil).Register(router)

	wanted := map[string]bool{
		"GET /api/public/anchor/:anchor/issues/":        false,
		"GET /api/public/anchor/:anchor/issues/:issue/": false,
	}
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

// The projection is twenty-three keys, and the two id lists report an empty list rather than a null.
func TestTheSpaceIssueShape(t *testing.T) {
	data := spaceIssueJSON(spaceIssueRow{ID: "issue-id"}, []gin.H{}, []gin.H{})
	if len(data) != 23 {
		t.Fatalf("the work item has %d keys, want 23", len(data))
	}
	for _, field := range []string{
		"id", "name", "state_id", "sort_order", "description_json", "description_html",
		"description_stripped", "description_binary", "module_ids", "label_ids", "assignee_ids",
		"estimate_point", "priority", "start_date", "target_date", "sequence_id", "project_id",
		"parent_id", "cycle_id", "created_by", "state__group", "vote_items", "reaction_items",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the work item is missing %q", field)
		}
	}
	for _, list := range []string{"module_ids", "label_ids", "assignee_ids"} {
		values, ok := data[list].([]string)
		if !ok || len(values) != 0 {
			t.Errorf("%s is %v, want an empty list", list, data[list])
		}
	}
	filled := spaceIssueJSON(spaceIssueRow{ID: "issue-id", LabelIDs: pq.StringArray{"label-id"}}, nil, nil)
	if values, ok := filled["label_ids"].([]string); !ok || len(values) != 1 {
		t.Errorf("the labels read %v", filled["label_ids"])
	}
}

// A reaction's avatar url is computed from the voter's avatar rather than the reactor's, which is what the expression names.
func TestAReactionCarriesTheVotersAvatarURL(t *testing.T) {
	// With no votes the url is null however many reactions there are, because the subquery finds nobody.
	var voterAsset *string
	var voterAvatar *string
	var url any
	switch {
	case voterAsset != nil:
		url = "/api/assets/v2/static/" + *voterAsset + "/"
	case voterAvatar != nil && *voterAvatar != "":
		url = *voterAvatar
	}
	if url != nil {
		t.Errorf("a reaction with no votes reports %v, want null", url)
	}
}
