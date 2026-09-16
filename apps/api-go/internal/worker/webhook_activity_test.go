package worker

import (
	"encoding/json"
	"strings"
	"testing"
)

// Seven events answer to a switch on the webhook; anything else reaches every active webhook in the workspace, whether or not it asked.
func TestWhichEventsAWebhookCanTurnOff(t *testing.T) {
	if len(webhookEventColumns) != 7 {
		t.Fatalf("%d events answer to a switch, want 7", len(webhookEventColumns))
	}
	for event, column := range map[string]string{
		"project": "project", "issue": "issue",
		"module": "module", "module_issue": "module",
		"cycle": "cycle", "cycle_issue": "cycle",
		"issue_comment": "issue_comment",
	} {
		if webhookEventColumns[event] != column {
			t.Errorf("%q answers to %q, want %q", event, webhookEventColumns[event], column)
		}
	}
	// An intake work item has no switch, which is why a webhook that asked for nothing still hears about one.
	if _, narrows := webhookEventColumns["intake_issue"]; narrows {
		t.Error("an intake work item answers to a switch and should not")
	}
}

// A change with no field named carries null rather than the empty string, which is what a create sends.
func TestAChangeWithNoField(t *testing.T) {
	if got := nullableActivityField(""); got != nil {
		t.Errorf("an unnamed field is %v", got)
	}
	if got := nullableActivityField("name"); got != "name" {
		t.Errorf("a named field is %v", got)
	}
}

// The objects keep the order their serializer declared, because a webhook receiver sees the bytes.
func TestTheObjectsKeepTheirOrder(t *testing.T) {
	encoded, err := orderedJSON([]field{{"zebra", 1}, {"apple", 2}, {"mango", 3}})
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(encoded)
	if rendered != `{"zebra": 1, "apple": 2, "mango": 3}` {
		t.Errorf("the object is %s", rendered)
	}
	// It has to be valid json all the same.
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("the object is not json: %v", err)
	}
	// A raw value is spliced in as it is rather than re-encoded.
	encoded, err = orderedJSON([]field{{"data", json.RawMessage(`{"b": 1, "a": 2}`)}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `{"b": 1, "a": 2}`) {
		t.Errorf("the raw value was rebuilt: %s", encoded)
	}
	// An absent raw value is null rather than nothing.
	encoded, err = orderedJSON([]field{{"data", json.RawMessage(nil)}})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"data": null}` {
		t.Errorf("an absent raw value is %s", encoded)
	}
}

// A json column that was never written reads as the empty object the model defaults to.
func TestTheJSONColumnDefault(t *testing.T) {
	if got := string(rawJSON(nil)); got != "{}" {
		t.Errorf("a null json column is %s", got)
	}
	if got := string(rawJSON([]byte(`{"a":1}`))); got != `{"a":1}` {
		t.Errorf("a set json column is %s", got)
	}
}

// A binary description is base64, and one that was never written stays absent rather than becoming an empty string.
func TestTheBinaryDescription(t *testing.T) {
	if got := base64OrNull(nil); got != nil {
		t.Errorf("an absent binary description is %v", got)
	}
	if got := base64OrNull([]byte("hi")); got != "aGk=" {
		t.Errorf("a binary description is %v", got)
	}
}

// A text array read back as text becomes the list a serializer reports.
func TestThePostgresArrayReading(t *testing.T) {
	for raw, want := range map[string]int{"{}": 0, `{"a"}`: 1, `{"a","b"}`: 2, "{a,b,c}": 3} {
		if got := postgresArray([]byte(raw)); len(got) != want {
			t.Errorf("%s read as %#v, want %d items", raw, got, want)
		}
	}
	got := postgresArray([]byte(`{"https://a.test/x,y","b"}`))
	if len(got) != 2 || got[0] != "https://a.test/x,y" {
		t.Errorf("a quoted comma was split: %#v", got)
	}
	if len(postgresArray(nil)) != 0 {
		t.Error("nothing read as something")
	}
}
