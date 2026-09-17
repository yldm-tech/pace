package auth

import (
	"reflect"
	"testing"
)

func TestJSONValueUsesTextDriverValue(t *testing.T) {
	value := JSONValue(`{"enabled":true}`)
	driverValue, err := value.Value()
	if err != nil {
		t.Fatal(err)
	}
	if driverValue != `{"enabled":true}` {
		t.Fatalf("driver value = %#v", driverValue)
	}
}

func TestJSONValueRejectsInvalidJSON(t *testing.T) {
	if _, err := (JSONValue(`{"broken"`)).Value(); err == nil {
		t.Fatal("expected invalid JSON error")
	}
	var value JSONValue
	if err := value.Scan([]byte(`{"items":[1,2]}`)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual([]byte(value), []byte(`{"items":[1,2]}`)) {
		t.Fatalf("scanned value = %s", value)
	}
}
