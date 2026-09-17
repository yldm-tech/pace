package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func newTestEmailTasks(t *testing.T) *EmailSendTasks {
	t.Helper()
	tasks, err := NewEmailSendTasks(nil, nil, EmailSettings{}, nil, &recordingMailer{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return tasks
}

// The template apps/api ships parses here, which is what makes keeping it a copy worth anything.
func TestTheShippedTemplateParses(t *testing.T) {
	newTestEmailTasks(t)
	if !strings.Contains(issueUpdatesTemplate, "{% if actors_involved == 1 %}") {
		t.Error("the embedded template is not the one apps/api ships")
	}
}

// Each value is kept once per field, and the two sides are kept apart.
func TestThePayloadKeepsEachValueOnce(t *testing.T) {
	tasks := newTestEmailTasks(t)
	data := json.RawMessage(`{
		"actor-1": [
			{"issue_activity": {"field": "labels", "old_value": "bug", "new_value": "urgent", "activity_time": "2026-09-16T10:30:00Z"}},
			{"issue_activity": {"field": "labels", "old_value": "bug", "new_value": "p1", "activity_time": "2026-09-16T11:00:00Z"}},
			{"issue_activity": {"field": "name", "old_value": "", "new_value": "After", "activity_time": "2026-09-16T11:30:00Z"}}
		]
	}`)
	grouped, err := tasks.createPayload(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	changes := grouped["actor-1"]
	labels, _ := changes["labels"].(map[string]any)
	if labels == nil {
		t.Fatalf("the changes are %#v", changes)
	}
	oldValues, _ := labels["old_value"].([]any)
	if len(oldValues) != 1 || oldValues[0] != "bug" {
		t.Errorf("the old labels are %#v, want one entry", oldValues)
	}
	newValues, _ := labels["new_value"].([]any)
	if len(newValues) != 2 {
		t.Errorf("the new labels are %#v, want two", newValues)
	}
	// An empty value is skipped rather than kept as an empty string.
	name, _ := changes["name"].(map[string]any)
	if _, present := name["old_value"]; present {
		t.Errorf("an empty old value was kept: %#v", name)
	}
	// The time is rewritten by every change rather than kept from the first, which is what upstream's misspelled check comes to.
	if changes["activity_time"] != "2026-09-16 11:30:00" {
		t.Errorf("the time is %v, want the last change's", changes["activity_time"])
	}
}

// An absent value reads as the word rather than being skipped, which is what Python's str() of None leaves behind.
func TestAnAbsentValueReadsAsTheWord(t *testing.T) {
	tasks := newTestEmailTasks(t)
	grouped, err := tasks.createPayload(context.Background(), json.RawMessage(
		`{"actor-1": [{"issue_activity": {"field": "state", "old_value": null, "new_value": "Done"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	state, _ := grouped["actor-1"]["state"].(map[string]any)
	oldValues, _ := state["old_value"].([]any)
	if len(oldValues) != 1 || oldValues[0] != "None" {
		t.Errorf("an absent old value is %#v, want the word", oldValues)
	}
}

// The time passes through two formats: a plain timestamp on the way in, and a time of day on the way out.
func TestTheTwoTimeFormats(t *testing.T) {
	if got := normalizeActivityTime("2026-09-16T14:05:09.123456Z"); got != "2026-09-16 14:05:09" {
		t.Errorf("the timestamp normalised to %q", got)
	}
	if got := normalizeActivityTime("2026-09-16T14:05:09"); got != "2026-09-16 14:05:09" {
		t.Errorf("a timestamp with no zone normalised to %q", got)
	}
	// The clock is the twenty-four hour one with an am or pm after it, which for the afternoon reads oddly and is what upstream sends.
	if got := formatEmailTime("2026-09-16 14:05:09"); got != "14:05 PM" {
		t.Errorf("the afternoon reads as %q", got)
	}
	if got := formatEmailTime("2026-09-16 09:05:09"); got != "09:05 AM" {
		t.Errorf("the morning reads as %q", got)
	}
}

// A control character in a work item's name is taken out of the subject line rather than breaking it.
func TestTheSubjectIsCleaned(t *testing.T) {
	if got := removeControlCharacters("Hello\r\nBcc: someone@example.test"); got != "HelloBcc: someone@example.test" {
		t.Errorf("the subject is %q", got)
	}
	if got := removeControlCharacters("plain"); got != "plain" {
		t.Errorf("a clean subject became %q", got)
	}
}

// The person a mention names is read out of the tag.
func TestTheMentionIdentifier(t *testing.T) {
	const person = "11111111-1111-1111-1111-111111111111"
	tag := `<mention-component entity_name="user_mention" entity_identifier="` + person + `"></mention-component>`
	if got := mentionIdentifier(tag); got != person {
		t.Errorf("the mention names %q", got)
	}
	if got := mentionIdentifier("<p>no mention</p>"); got != "" {
		t.Errorf("a tag with no mention gave %q", got)
	}
}

// The lock is named after every notification in the batch, sorted, which is why two emails to the same person in one sweep collide.
func TestTheLockNamesTheWholeBatch(t *testing.T) {
	first := lockNameFor("issue", "reader", []string{"b", "a", "c"})
	second := lockNameFor("issue", "reader", []string{"c", "b", "a"})
	if first != second {
		t.Errorf("the same batch in a different order took two locks:\n  %s\n  %s", first, second)
	}
	if !strings.HasSuffix(first, "a_b_c") {
		t.Errorf("the lock is %q", first)
	}
}
