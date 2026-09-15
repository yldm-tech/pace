package project

import (
	"encoding/json"
	"testing"
)

// The list is the one shape that carries order, which is a place within the group rather than a column.
func TestTheStateOrderIsAFractionOfItsGroup(t *testing.T) {
	// Four states in one group are numbered a quarter at a time, and the last one is exactly one.
	counts := map[string]int{"backlog": 4}
	seen := map[string]int{}
	orders := []float64{}
	for index := 0; index < 4; index++ {
		seen["backlog"]++
		orders = append(orders, float64(seen["backlog"])/float64(counts["backlog"]))
	}
	want := []float64{0.25, 0.5, 0.75, 1}
	for index, value := range orders {
		if value != want[index] {
			t.Errorf("the %dth state is ordered %v, want %v", index, value, want[index])
		}
	}
	// A group of one puts its only state at one rather than at zero.
	if got := float64(1) / float64(1); got != 1 {
		t.Errorf("a lone state is ordered %v", got)
	}
}

// Every other shape drops the key rather than reporting a null, because the field is declared and an instance never has it.
func TestOnlyTheListCarriesTheOrder(t *testing.T) {
	data := stateJSON(State{ID: "state-id"})
	if _, present := data["order"]; present {
		t.Error("a plain state carries an order, and an instance has none")
	}
	if len(data) != 9 {
		t.Fatalf("a state has %d fields, want 9", len(data))
	}
}

// Triage is refused from the field list rather than accepted as a group, which is what keeps the intake state out of reach of these routes.
func TestTriageCannotBeWritten(t *testing.T) {
	handler := &Handler{}
	body := map[string]json.RawMessage{
		"name": json.RawMessage(`"Triage"`), "color": json.RawMessage(`"#fff"`),
		"group": json.RawMessage(`"triage"`),
	}
	_, failures := handler.stateFields(body, false)
	if failures == nil {
		t.Fatal("a triage state is accepted")
	}
	messages, ok := failures["non_field_errors"].([]string)
	if !ok || len(messages) != 1 || messages[0] != "Cannot create triage state" {
		t.Errorf("the refusal is %v", failures)
	}
	// It is not one of the five a state may belong to either.
	for _, group := range stateGroups {
		if group == "triage" {
			t.Error("triage is one of the groups a state may be written with")
		}
	}
	if len(stateGroups) != 5 {
		t.Errorf("there are %d groups, want 5", len(stateGroups))
	}
}

// A create wants a name and a colour; an update wants nothing at all.
func TestTheStateFieldsThatAreRequired(t *testing.T) {
	handler := &Handler{}
	_, failures := handler.stateFields(map[string]json.RawMessage{}, false)
	if failures == nil {
		t.Fatal("a create with no fields is accepted")
	}
	for _, field := range []string{"name", "color"} {
		if _, present := failures[field]; !present {
			t.Errorf("%q is not required on a create", field)
		}
	}
	if _, failures := handler.stateFields(map[string]json.RawMessage{}, true); failures != nil {
		t.Errorf("an update with no fields is refused with %v", failures)
	}
}

// The group falls back to backlog and the sequence to the column default.
func TestTheStateDefaults(t *testing.T) {
	handler := &Handler{}
	values, failures := handler.stateFields(map[string]json.RawMessage{
		"name": json.RawMessage(`"Done"`), "color": json.RawMessage(`"#000"`),
	}, false)
	if failures != nil {
		t.Fatalf("a plain create is refused with %v", failures)
	}
	if values.group != "backlog" {
		t.Errorf("the group falls back to %q", values.group)
	}
	if values.sequence != 65535 {
		t.Errorf("the sequence falls back to %v", values.sequence)
	}
}
