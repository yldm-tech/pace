package worker

import (
	"testing"
)

// Both custom tags are read out of the html with their attributes intact, including the ones a browser would not know.
func TestTheComponentsAreReadOutOfTheHTML(t *testing.T) {
	source := `<p>hello <mention-component id="11111111-1111-1111-1111-111111111111" entity_identifier="22222222-2222-2222-2222-222222222222" entity_name="user_mention"></mention-component></p>` +
		`<image-component id="33333333-3333-3333-3333-333333333333" src="https://example.test/a.png"></image-component>`
	mentions := componentAttributes(source, "mention-component")
	if len(mentions) != 1 {
		t.Fatalf("found %d mentions", len(mentions))
	}
	if mentions[0]["entity_identifier"] != "22222222-2222-2222-2222-222222222222" {
		t.Errorf("the mention is %#v", mentions[0])
	}
	if mentions[0]["entity_name"] != "user_mention" {
		t.Errorf("the mention's name is %q", mentions[0]["entity_name"])
	}
	images := componentAttributes(source, "image-component")
	if len(images) != 1 || images[0]["src"] != "https://example.test/a.png" {
		t.Errorf("the image is %#v", images)
	}
	// Nothing at all is no components rather than an error.
	if got := componentAttributes("", "mention-component"); len(got) != 0 {
		t.Errorf("empty html gave %#v", got)
	}
}

// The identifier an image would log is a url, and the column it goes into is a uuid — which is why a page with one image logs nothing at all.
func TestAnImageSourceIsNotAUUID(t *testing.T) {
	if looksLikeUUID("https://example.test/a.png") {
		t.Error("a url reads as a uuid")
	}
	for _, value := range []string{
		"11111111-1111-1111-1111-111111111111",
		"11111111111111111111111111111111",
		"11111111-1111-1111-1111-111111111111",
	} {
		if !looksLikeUUID(value) {
			t.Errorf("%q does not read as a uuid", value)
		}
	}
}

// The stripped copy is null for empty html rather than an empty string, which is what the model's own save writes.
func TestTheStrippedCopy(t *testing.T) {
	if got := strippedHTML(""); got != nil {
		t.Errorf("empty html stripped to %v", got)
	}
	if got := strippedHTML("<p>hello <b>there</b></p>"); got != "hello there" {
		t.Errorf("the stripped copy is %v", got)
	}
}

// A null json column reads as the empty object the model defaults to rather than as nothing.
func TestTheEmptyJSONDefault(t *testing.T) {
	if got := jsonOrEmpty(nil); got != "{}" {
		t.Errorf("a null column became %q", got)
	}
	if got := jsonOrEmpty([]byte(`{"a":1}`)); got != `{"a":1}` {
		t.Errorf("a set column became %q", got)
	}
}

// The creating flag is read the way Python's truthiness would read it, so the string False is false.
func TestTheCreatingFlag(t *testing.T) {
	for _, value := range []any{true, "True", "1", 1.0} {
		if !boolArgument(nil, map[string]any{"is_creating": value}, 3, "is_creating") {
			t.Errorf("%#v read as false", value)
		}
	}
	for _, value := range []any{false, "False", "false", "0", "", 0.0, nil} {
		if boolArgument(nil, map[string]any{"is_creating": value}, 3, "is_creating") {
			t.Errorf("%#v read as true", value)
		}
	}
	// An absent flag is false, which is the task's own default.
	if boolArgument(nil, nil, 3, "is_creating") {
		t.Error("an absent flag read as true")
	}
}

// The two version tasks share one window, and the page one is capped where the work item one is not.
func TestTheVersionWindowAndCap(t *testing.T) {
	if versionWindow.Seconds() != 600 {
		t.Errorf("the window is %v", versionWindow)
	}
	if pageVersionsKept != 20 {
		t.Errorf("the cap is %d", pageVersionsKept)
	}
}
