package project

import (
	"testing"
)

// The profile reports eight things about a person and none of them is anything an account needs.
func TestTheProfileStopsShortOfTheAccount(t *testing.T) {
	profile := map[string]any{
		"email": "", "first_name": "", "last_name": "", "avatar_url": nil,
		"cover_image_url": nil, "date_joined": nil, "user_timezone": "", "display_name": "",
	}
	if len(profile) != 8 {
		t.Fatalf("the profile has %d fields, want 8", len(profile))
	}
	for _, key := range []string{"password", "is_superuser", "token", "last_login_medium", "is_bot"} {
		if _, present := profile[key]; present {
			t.Errorf("the profile carries %q", key)
		}
	}
}

// The uploaded asset wins over a stored url, and neither one leaves the field null rather than empty.
func TestTheAssetURLPrefersTheUpload(t *testing.T) {
	asset := "11111111-2222-3333-4444-555555555555"
	if got := staticAssetURL(&asset, "https://example.test/avatar.png"); got != "/api/assets/v2/static/"+asset+"/" {
		t.Errorf("with an upload the url is %v", got)
	}
	if got := staticAssetURL(nil, "https://example.test/avatar.png"); got != "https://example.test/avatar.png" {
		t.Errorf("without an upload the url is %v", got)
	}
	if got := staticAssetURL(nil, ""); got != nil {
		t.Errorf("with neither the url is %v, want null", got)
	}
}

// A value a spreadsheet would read as a formula is written as text instead.
func TestTheExportDefusesFormulas(t *testing.T) {
	for _, value := range []string{"=1+1", "+1", "-1", "@SUM(A1)"} {
		got := sanitizeCSVCell(value)
		if got != "'"+value {
			t.Errorf("%q is written as %q", value, got)
		}
	}
	for _, value := range []string{"", "plain", "1+1"} {
		if got := sanitizeCSVCell(value); got != value {
			t.Errorf("%q is rewritten as %q and should not be", value, got)
		}
	}
}

// Only two fields can be ordered on, and anything else falls back to newest first rather than reaching the database.
func TestTheActivityOrderingIsAllowlisted(t *testing.T) {
	if len(activityOrderByAllowlist) != 2 {
		t.Fatalf("there are %d orderable fields, want 2", len(activityOrderByAllowlist))
	}
	for _, value := range []string{"created_at", "-created_at", "updated_at", "-updated_at"} {
		if got := sanitizeOrderBy(value, activityOrderByAllowlist, "-created_at"); got != value {
			t.Errorf("%q is rewritten as %q", value, got)
		}
	}
	for _, value := range []string{"", "verb", "--created_at", "actor__email", "created_at; DROP TABLE users"} {
		if got := sanitizeOrderBy(value, activityOrderByAllowlist, "-created_at"); got != "-created_at" {
			t.Errorf("%q is accepted as %q", value, got)
		}
	}
}

// The five priorities sort into their own order rather than alphabetically, and anything else lands after them.
func TestThePriorityOrderIsTheProductOrder(t *testing.T) {
	for index, priority := range []string{"urgent", "high", "medium", "low", "none"} {
		fragment := "WHEN i.priority = '" + priority + "' THEN " + string(rune('0'+index))
		if !containsFragment(priorityOrderCase, fragment) {
			t.Errorf("%q is not sorted into position %d", priority, index)
		}
	}
	if !containsFragment(priorityOrderCase, "ELSE 5") {
		t.Error("a priority the product does not have is not sorted last")
	}
}

func containsFragment(haystack, needle string) bool {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}
