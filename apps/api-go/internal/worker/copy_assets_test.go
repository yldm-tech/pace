package worker

import "testing"

// The live server's endpoint is the configured url with the path it lives under, and repeated slashes in the path collapse to one.
func TestTheLiveServerEndpoint(t *testing.T) {
	cases := map[string]string{
		"http://live:3000/live//convert-document/": "http://live:3000/live/convert-document/",
		"http://live:3000//convert-document/":      "http://live:3000/convert-document/",
		"https://x.test/a/b/convert-document/":     "https://x.test/a/b/convert-document/",
		"https://x.test/a//b///c/?q=1//2":          "https://x.test/a/b/c/?q=1//2",
	}
	for raw, want := range cases {
		got, err := normalizeURLPath(raw)
		if err != nil {
			t.Fatalf("normalising %q: %v", raw, err)
		}
		if got != want {
			t.Errorf("%q normalised to %q rather than %q", raw, got, want)
		}
	}
}

// A copy's key is the workspace, a fresh uuid with its dashes taken out, and the original's name — and an original whose attributes carry no name puts the literal None there, which is what python's f-string does.
func TestTheCopiedAssetName(t *testing.T) {
	if got := assetNameOf("a picture.png"); got != "a picture.png" {
		t.Errorf("the name is %q", got)
	}
	if got := assetNameOf(nil); got != "None" {
		t.Errorf("a missing name is %q", got)
	}
}

// Each entity type names the column that points back at what the description belongs to, and one the table does not name leaves every one of them null.
func TestTheOwnerColumns(t *testing.T) {
	if copyAssetOwnerColumns["PAGE_DESCRIPTION"] != "page_id" {
		t.Errorf("a page description is owned by %q", copyAssetOwnerColumns["PAGE_DESCRIPTION"])
	}
	if copyAssetOwnerColumns["ISSUE_DESCRIPTION"] != "issue_id" {
		t.Errorf("a work item description is owned by %q", copyAssetOwnerColumns["ISSUE_DESCRIPTION"])
	}
	// DRAFT_ISSUE_ATTACHMENT is an entity type the mapping does not name, so a copy of one is owned by nothing.
	if _, named := copyAssetOwnerColumns["DRAFT_ISSUE_ATTACHMENT"]; named {
		t.Error("a draft attachment is owned by something, and upstream's mapping leaves it out")
	}
}
