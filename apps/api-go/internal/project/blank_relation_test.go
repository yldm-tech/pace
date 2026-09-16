package project

import (
	"encoding/json"
	"testing"
)

// TestBlankRelationFollowsRelatedFieldRunValidation pins blankRelation to the first two lines of RelatedField.run_validation in rest_framework/relations.py:
//
//	# We force empty strings to None values for relational fields.
//	if data == '':
//	    data = None
//
// For a foreign key an empty string is the absence of a value, not a malformed identifier. The web client depends on it: the create-work-item dialog posts state_id:"" to mean the project's default state, and every attempt came back 400 "“” is not a valid UUID." with nothing created.
func TestBlankRelationFollowsRelatedFieldRunValidation(t *testing.T) {
	for _, testCase := range []struct {
		name string
		body string
		want bool
	}{
		{name: "null is absent, as it always was", body: `null`, want: true},
		{name: "the empty string is absent too", body: `""`, want: true},
		{name: "an identifier is present", body: `"4d5787f3-805f-49bb-86e2-1b248a39e542"`, want: false},
		{name: "a non-empty non-identifier is still present, and fails later as a bad UUID", body: `"nonsense"`, want: false},
		{name: "whitespace is not the empty string", body: `" "`, want: false},
		{name: "a number is not the empty string", body: `12`, want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := blankRelation(json.RawMessage(testCase.body)); got != testCase.want {
				t.Errorf("blankRelation(%s) = %v, want %v", testCase.body, got, testCase.want)
			}
		})
	}
}
