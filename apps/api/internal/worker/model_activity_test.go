package worker

import (
	"context"
	"testing"
)

type recordingWebhookPublisher struct{ sent []map[string]any }

func (publisher *recordingWebhookPublisher) PublishWebhookActivityChange(_ context.Context, keywords map[string]any) error {
	publisher.sent = append(publisher.sent, keywords)
	return nil
}

func run(t *testing.T, requested any, current any) []map[string]any {
	t.Helper()
	publisher := &recordingWebhookPublisher{}
	tasks := NewModelActivityTasks(publisher, nil)
	err := tasks.modelActivity(context.Background(), nil, map[string]any{
		"model_name": "issue", "model_id": "issue-id",
		"requested_data": requested, "current_instance": current,
		"actor_id": "actor-id", "slug": "acme", "origin": "https://example.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	return publisher.sent
}

// No previous state is a create, and a create names no field at all.
func TestNoPreviousStateIsACreate(t *testing.T) {
	sent := run(t, `{"name":"Hello"}`, nil)
	if len(sent) != 1 {
		t.Fatalf("a create sent %d activities", len(sent))
	}
	if sent[0]["verb"] != "created" {
		t.Errorf("the verb is %v", sent[0]["verb"])
	}
	for _, key := range []string{"field", "old_value", "new_value"} {
		if sent[0][key] != nil {
			t.Errorf("a create carries %q = %v", key, sent[0][key])
		}
	}
	if sent[0]["event_id"] != "issue-id" || sent[0]["event"] != "issue" {
		t.Errorf("the activity is %#v", sent[0])
	}
	// The empty string is not a state either, since that is what an absent keyword becomes.
	if got := run(t, `{"name":"Hello"}`, ""); len(got) != 1 || got[0]["verb"] != "created" {
		t.Errorf("an empty previous state sent %#v", got)
	}
}

// Only the fields that really moved are reported, and each one carries both of its values.
func TestOnlyChangedFieldsAreReported(t *testing.T) {
	sent := run(t, `{"name":"After","priority":"high"}`, `{"name":"Before","priority":"high"}`)
	if len(sent) != 1 {
		t.Fatalf("sent %d activities: %#v", len(sent), sent)
	}
	if sent[0]["field"] != "name" || sent[0]["old_value"] != "Before" || sent[0]["new_value"] != "After" {
		t.Errorf("the change is %#v", sent[0])
	}
	if sent[0]["verb"] != "updated" {
		t.Errorf("the verb is %v", sent[0]["verb"])
	}
}

// A field the previous state never had is not a change, which is why adding one to a serializer makes its first write invisible to webhooks.
func TestAFieldTheSnapshotLacksIsNotAChange(t *testing.T) {
	sent := run(t, `{"name":"Same","brand_new":"value"}`, `{"name":"Same"}`)
	if len(sent) != 0 {
		t.Errorf("sent %#v, want nothing", sent)
	}
}

// Values are compared as the json they decode to, so a number and its string are different and two equal objects are not.
func TestTheComparisonIsOnDecodedValues(t *testing.T) {
	if sent := run(t, `{"a":1}`, `{"a":"1"}`); len(sent) != 1 {
		t.Errorf("a number against its string sent %#v", sent)
	}
	if sent := run(t, `{"a":{"x":1,"y":2}}`, `{"a":{"y":2,"x":1}}`); len(sent) != 0 {
		t.Errorf("two equal objects sent %#v", sent)
	}
	if sent := run(t, `{"a":[1,2]}`, `{"a":[2,1]}`); len(sent) != 1 {
		t.Errorf("two lists in different orders sent %#v", sent)
	}
	if sent := run(t, `{"a":null}`, `{"a":"was"}`); len(sent) != 1 {
		t.Errorf("a value cleared sent %#v", sent)
	}
}

// Several changes go out one at a time, in a fixed order.
func TestEachChangeIsItsOwnActivity(t *testing.T) {
	sent := run(t, `{"name":"After","priority":"low","state_id":"s2"}`,
		`{"name":"Before","priority":"high","state_id":"s1"}`)
	if len(sent) != 3 {
		t.Fatalf("sent %d activities", len(sent))
	}
	fields := []string{}
	for _, activity := range sent {
		fields = append(fields, activity["field"].(string))
	}
	if fields[0] != "name" || fields[1] != "priority" || fields[2] != "state_id" {
		t.Errorf("the order is %#v", fields)
	}
}

// A state handed over as an object rather than as json is read the same way.
func TestASnapshotCanArriveAsAnObject(t *testing.T) {
	sent := run(t, map[string]any{"name": "After"}, map[string]any{"name": "Before"})
	if len(sent) != 1 || sent[0]["field"] != "name" {
		t.Errorf("sent %#v", sent)
	}
}
