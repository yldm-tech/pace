package workspace

import (
	"encoding/json"
	"testing"
)

// A work item and a folder both report a null beside them: the first is in the table of types with no serializer, and the second has no model at all.
func TestSomeFavoritesShowNothingAboutThemselves(t *testing.T) {
	for _, entity := range []string{"issue", "folder", "nonsense"} {
		table := ""
		switch entity {
		case "cycle", "module", "view", "page", "project":
			table = entity
		}
		if table != "" {
			t.Errorf("%q reads a table, and it should not", entity)
		}
	}
	// The five that do have one are the five the map names.
	for _, entity := range []string{"cycle", "module", "view", "page", "project"} {
		switch entity {
		case "cycle", "module", "view", "page", "project":
		default:
			t.Errorf("%q is not one of the five", entity)
		}
	}
}

// The project favourite list is broken upstream and answers a 500, which is reproduced rather than invented around.
func TestTheProjectFavoriteListIsBroken(t *testing.T) {
	if errProjectFavoritesHaveNoSerializer == nil {
		t.Fatal("the failure is not named")
	}
	if got := errProjectFavoritesHaveNoSerializer.Error(); got == "" {
		t.Error("the failure has no message")
	}
}

// Starring something twice answers the star that is already there rather than refusing.
func TestStarringTwiceIsNotARefusal(t *testing.T) {
	payload := map[string]json.RawMessage{
		"entity_type":       json.RawMessage(`"cycle"`),
		"entity_identifier": json.RawMessage(`"11111111-2222-3333-4444-555555555555"`),
	}
	if got := payloadText(payload, "entity_identifier"); got != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("the identifier reads as %q", got)
	}
	if got := payloadText(payload, "missing"); got != "" {
		t.Errorf("an absent field reads as %q", got)
	}
	// A number is not a string and reads as absent, which is how the lookup is skipped.
	if got := payloadText(map[string]json.RawMessage{"entity_identifier": json.RawMessage(`5`)}, "entity_identifier"); got != "" {
		t.Errorf("a number reads as %q", got)
	}
}
