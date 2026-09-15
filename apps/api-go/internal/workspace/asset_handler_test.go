package workspace

import (
	"testing"
)

// The entity decides which column the identifier is written to, and one nobody recognises writes none.
func TestTheEntityColumns(t *testing.T) {
	for entity, column := range map[string]string{
		"WORKSPACE_LOGO": "workspace_id", "PROJECT_COVER": "project_id",
		"USER_AVATAR": "user_id", "USER_COVER": "user_id",
		"ISSUE_ATTACHMENT": "issue_id", "ISSUE_DESCRIPTION": "issue_id",
		"PAGE_DESCRIPTION": "page_id", "COMMENT_DESCRIPTION": "comment_id",
	} {
		if got := entityColumn(entity); got != column {
			t.Errorf("%s writes %q, want %q", entity, got, column)
		}
	}
	// The two draft entities are allowed to be reserved and have no column of their own, so the identifier is dropped.
	for _, entity := range []string{"DRAFT_ISSUE_ATTACHMENT", "DRAFT_ISSUE_DESCRIPTION"} {
		if got := entityColumn(entity); got != "" {
			t.Errorf("%s writes %q, and Django writes nothing", entity, got)
		}
		if !assetEntityTypes[entity] {
			t.Errorf("%s is refused, and it is one of the entity types", entity)
		}
	}
	if len(assetEntityTypes) != 10 {
		t.Fatalf("there are %d entity types, want 10", len(assetEntityTypes))
	}
}

// This endpoint holds every upload to an image however it names itself, so an attachment reserved here cannot be a pdf.
func TestTheWorkspaceAssetTakesImagesOnly(t *testing.T) {
	if len(assetImageTypes) != 5 {
		t.Fatalf("the list has %d types, want 5", len(assetImageTypes))
	}
	if assetImageTypes["application/pdf"] {
		t.Error("a pdf passes, and this endpoint takes images alone")
	}
	for _, accepted := range []string{"image/jpeg", "image/png", "image/webp", "image/jpg", "image/gif"} {
		if !assetImageTypes[accepted] {
			t.Errorf("%q is refused", accepted)
		}
	}
}

// The static route serves a type a browser would execute as an attachment rather than inline.
func TestAScriptCapableAssetIsServedAsAnAttachment(t *testing.T) {
	if len(scriptCapableMimeTypes) != 7 {
		t.Fatalf("the list has %d types, want 7", len(scriptCapableMimeTypes))
	}
	for _, refused := range []string{"image/svg+xml", "text/html", "application/xml"} {
		if !scriptCapableMimeTypes[refused] {
			t.Errorf("%q is served inline, and a browser would run it", refused)
		}
	}
	if scriptCapableMimeTypes["image/png"] {
		t.Error("a png is served as an attachment, and nothing runs it")
	}
}

// The type is read off the attributes, cut down to the media type alone and lowercased.
func TestTheAssetMimeTypeIsNarrowed(t *testing.T) {
	asset := FileAsset{Attributes: []byte(`{"type": "Image/SVG+XML; charset=utf-8"}`)}
	if got := assetMimeType(asset); got != "image/svg+xml" {
		t.Errorf("the type reads as %q", got)
	}
	if got := assetMimeType(FileAsset{}); got != "" {
		t.Errorf("an asset with no attributes reads as %q", got)
	}
}
