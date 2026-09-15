package worker

import (
	"strings"
	"testing"
)

// Thirteen activity types pass through without a notification: cycles, modules, reactions, votes and anything to do with a draft.
func TestTheSilentActivityTypes(t *testing.T) {
	if len(silentActivityTypes) != 13 {
		t.Fatalf("%d types are silent, want 13", len(silentActivityTypes))
	}
	for _, activityType := range []string{
		"cycle.activity.created", "module.activity.deleted",
		"issue_reaction.activity.created", "issue_vote.activity.deleted",
		"issue_draft.activity.updated",
	} {
		if !silentActivityTypes[activityType] {
			t.Errorf("%q is not silent and should be", activityType)
		}
	}
	// The ones people do hear about.
	for _, activityType := range []string{
		"issue.activity.created", "issue.activity.updated", "comment.activity.created",
		"link.activity.created", "attachment.activity.created", "intake.activity.created",
		"issue_relation.activity.created",
	} {
		if silentActivityTypes[activityType] {
			t.Errorf("%q is silent and should not be", activityType)
		}
	}
}

// Only a mention component naming a person counts, and each person counts once however many times they are named.
func TestTheMentionsInADescription(t *testing.T) {
	const first = "11111111-1111-1111-1111-111111111111"
	const second = "22222222-2222-2222-2222-222222222222"
	source := `<p><mention-component entity_name="user_mention" entity_identifier="` + first + `"></mention-component>` +
		`<mention-component entity_name="user_mention" entity_identifier="` + first + `"></mention-component>` +
		`<mention-component entity_name="user_mention" entity_identifier="` + second + `"></mention-component>` +
		`<mention-component entity_name="issue_mention" entity_identifier="33333333-3333-3333-3333-333333333333"></mention-component></p>`
	mentions := extractMentions(source)
	if len(mentions) != 2 {
		t.Fatalf("found %#v", mentions)
	}
	if mentions[0] != first || mentions[1] != second {
		t.Errorf("found %#v", mentions)
	}
	// A work item mention is not a person and is not counted.
	for _, mention := range mentions {
		if strings.HasPrefix(mention, "3333") {
			t.Error("a work item mention was counted as a person")
		}
	}
	if got := extractMentions(""); len(got) != 0 {
		t.Errorf("empty html gave %#v", got)
	}
}

// The mentions are read out of the description inside the snapshot rather than out of the snapshot itself.
func TestTheMentionsAreInsideTheSnapshot(t *testing.T) {
	const person = "11111111-1111-1111-1111-111111111111"
	snapshot := `{"description_html":"<mention-component entity_name=\"user_mention\" entity_identifier=\"` + person + `\"></mention-component>"}`
	mentions := mentionsIn(snapshot)
	if len(mentions) != 1 || mentions[0] != person {
		t.Errorf("found %#v", mentions)
	}
	// Nothing at all, and text that is not json, both read as no mentions rather than as an error.
	if got := mentionsIn(""); len(got) != 0 {
		t.Errorf("an empty snapshot gave %#v", got)
	}
	if got := mentionsIn("not json"); len(got) != 0 {
		t.Errorf("a snapshot that is not json gave %#v", got)
	}
}

// A value that was absent reads as the word rather than as nothing, which is what Python's str() leaves in the data — except the two identifiers, which stay null.
func TestTheValuesAreRenderedAsPythonWould(t *testing.T) {
	data := activityData(map[string]any{
		"id": "act", "verb": "updated", "field": nil, "actor_id": "who",
		"new_value": nil, "old_value": "was", "old_identifier": nil, "new_identifier": "",
	}, "the comment", "", false)
	if data["field"] != "None" || data["new_value"] != "None" {
		t.Errorf("an absent value is %v and %v, want the word", data["field"], data["new_value"])
	}
	if data["old_identifier"] != nil || data["new_identifier"] != nil {
		t.Errorf("the identifiers are %v and %v, want null", data["old_identifier"], data["new_identifier"])
	}
	if data["issue_comment"] != "the comment" {
		t.Errorf("the inbox's copy carries %v", data["issue_comment"])
	}
	// The email's copy carries the time and not the comment.
	email := activityData(map[string]any{"created_at": "2026-09-16T00:00:00Z"}, "the comment", "", true)
	if _, present := email["issue_comment"]; present {
		t.Error("the email's copy carries the comment")
	}
	if email["activity_time"] != "2026-09-16T00:00:00Z" {
		t.Errorf("the email's copy carries %v as its time", email["activity_time"])
	}
	// A mention overrides the field rather than reporting the one the activity carried.
	mention := activityData(map[string]any{"field": "name"}, "", "mention", true)
	if mention["field"] != "mention" {
		t.Errorf("a mention's field is %v", mention["field"])
	}
}

// The email's copy of the work item carries two more fields than the inbox's, which is what lets the email link back into the app.
func TestTheEmailKnowsWhereToLink(t *testing.T) {
	issue := notificationIssue{ID: "issue", Name: "Hello", SequenceID: 7, StateName: "Todo", StateGroup: "unstarted"}
	project := notificationProject{ID: "project", Identifier: "ACME", WorkspaceSlug: "acme"}
	inbox := issueData(issue, project, false)
	email := issueData(issue, project, true)
	if len(inbox) != 6 {
		t.Errorf("the inbox's copy has %d fields, want 6", len(inbox))
	}
	if len(email) != 8 {
		t.Errorf("the email's copy has %d fields, want 8", len(email))
	}
	if email["workspace_slug"] != "acme" || email["project_id"] != "project" {
		t.Errorf("the email's copy is %#v", email)
	}
}

// A mention email names the wrong reader, because it reads a variable the subscriber loop left behind. When that loop never ran there is no such variable and the whole batch is lost.
func TestTheMentionEmailNamesTheLeftoverReader(t *testing.T) {
	tasks := &NotificationTasks{}
	if got, err := tasks.leftoverReceiver("somebody"); err != nil || got != "somebody" {
		t.Errorf("the leftover reader is %q and %v", got, err)
	}
	if _, err := tasks.leftoverReceiver(""); err == nil {
		t.Error("an empty leftover was accepted, and upstream would have written a boolean into a uuid column")
	}
}
