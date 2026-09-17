package worker

import (
	"encoding/json"
	"testing"
)

// The per-actor lists keep a fixed order, since a map has none and the email is rendered from this.
func TestTheActorDataKeepsAnOrder(t *testing.T) {
	encoded, err := orderedActorData(map[string][]json.RawMessage{
		"zebra": {json.RawMessage(`{"a":1}`)},
		"apple": {json.RawMessage(`{"b":2}`), json.RawMessage(`{"c":3}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(encoded)
	if rendered != `{"apple": [{"b":2}, {"c":3}], "zebra": [{"a":1}]}` {
		t.Errorf("the data is %s", rendered)
	}
	var decoded map[string][]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("the data is not json: %v", err)
	}
	if len(decoded["apple"]) != 2 {
		t.Errorf("the lists are %#v", decoded)
	}
}

// A log row with nothing in its data column reads as null rather than as an empty object, because this one really can be absent.
func TestAnEmptyDataColumnIsNull(t *testing.T) {
	if got := string(rawJSONOrNull(nil)); got != "null" {
		t.Errorf("an empty data column is %s", got)
	}
	if got := string(rawJSONOrNull([]byte(`{"a":1}`))); got != `{"a":1}` {
		t.Errorf("a set data column is %s", got)
	}
	// The json columns that default to an empty object are read the other way, which is the difference between the two helpers.
	if got := string(rawJSON(nil)); got != "{}" {
		t.Errorf("a defaulting json column is %s", got)
	}
}
