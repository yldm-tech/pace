package project

import (
	"net/http"
	"testing"
	"time"
)

// The method table is the unusual part of the page permission: it keys on the HTTP verb rather than on the action, so the two halves of one button sit behind different rules.
func TestLockingAndUnlockingWantDifferentRoles(t *testing.T) {
	if !pageMethodAllows(http.MethodPost, roleMember) {
		t.Error("a member may lock a page, since locking is a POST")
	}
	if pageMethodAllows(http.MethodDelete, roleMember) {
		t.Error("a member may not unlock a page, since unlocking is a DELETE and only an admin may DELETE")
	}
	if !pageMethodAllows(http.MethodDelete, roleAdmin) {
		t.Error("an admin may unlock a page")
	}
}

// A guest may read a page and nothing else.
func TestAGuestMayOnlyReadAPage(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		if !pageMethodAllows(method, roleGuest) {
			t.Errorf("a guest may %s a page", method)
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete} {
		if pageMethodAllows(method, roleGuest) {
			t.Errorf("a guest may not %s a page", method)
		}
	}
	// An unknown verb is refused for everyone rather than falling through.
	if pageMethodAllows("TRACE", roleAdmin) {
		t.Error("an unnamed method is denied by default")
	}
}

// Access reads the opposite way round from every other flag in the codebase: zero is public.
func TestZeroIsThePublicPage(t *testing.T) {
	if pagePublicAccess != 0 || pagePrivateAccess != 1 {
		t.Fatalf("public is %d and private is %d, which is the way round the model has it", pagePublicAccess, pagePrivateAccess)
	}
}

// The serializer has two shapes: the list omits the description and the detail carries it.
func TestThePageDetailAddsTheDescription(t *testing.T) {
	row := pageRow{Page: Page{ID: "page-id", DescriptionHTML: "<p>text</p>"}}
	listed := pageJSON(row, false)
	detailed := pageJSON(row, true)

	if len(detailed) != len(listed)+1 {
		t.Fatalf("the detail has %d fields and the list %d, want one more", len(detailed), len(listed))
	}
	if _, present := listed["description_html"]; present {
		t.Error("the list must not carry the description")
	}
	if detailed["description_html"] != "<p>text</p>" {
		t.Errorf("the detail carries %v", detailed["description_html"])
	}
	for _, field := range []string{
		"id", "name", "owned_by", "access", "color", "parent", "is_favorite", "is_locked",
		"archived_at", "workspace", "created_at", "updated_at", "created_by", "updated_by",
		"view_props", "logo_props", "label_ids", "project_ids",
	} {
		if _, present := listed[field]; !present {
			t.Errorf("the page is missing %q", field)
		}
	}
	if len(listed) != 18 {
		t.Fatalf("the page has %d fields, want 18", len(listed))
	}
}

// The archive body is str(datetime.now()), which is a naive local wall clock with a space in it rather than an ISO instant.
func TestTheArchiveBodyIsANaiveLocalTimestamp(t *testing.T) {
	instant := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	rendered := naiveTimestamp(instant)
	if len(rendered) < 19 || rendered[10] != ' ' {
		t.Fatalf("the timestamp is %q, want a space between the date and the time", rendered)
	}
	if rendered[len(rendered)-1] == 'Z' {
		t.Error("the timestamp carries no zone, since datetime.now() has none")
	}
	withFraction := naiveTimestamp(time.Date(2026, 3, 4, 5, 6, 7, 123456000, time.UTC))
	if len(withFraction) != len(rendered)+7 {
		t.Errorf("a fractional second renders as %q", withFraction)
	}
}

// The archived column is a date rather than a datetime, which is why the page list reports a day.
func TestAPageIsArchivedOnADay(t *testing.T) {
	instant := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	data := pageJSON(pageRow{Page: Page{ArchivedAt: &instant}}, false)
	if data["archived_at"] != "2026-03-04" {
		t.Errorf("archived_at is %v, want the day alone", data["archived_at"])
	}
	if pageJSON(pageRow{}, false)["archived_at"] != nil {
		t.Error("a live page reports no archive date")
	}
}

// The two read-only fields are not writable, and the access column is.
func TestApplyPagePayloadWritesWhatItShould(t *testing.T) {
	page := Page{WorkspaceID: "workspace-id", OwnedByID: "owner-id", Access: pagePublicAccess}
	applyPagePayload(&page, map[string]any{
		"name": "Notes", "color": "#fff", "access": float64(pagePrivateAccess),
		"workspace": "elsewhere", "owned_by": "someone-else", "parent": "parent-id",
	})
	if page.WorkspaceID != "workspace-id" || page.OwnedByID != "owner-id" {
		t.Errorf("a read-only field was written: %+v", page)
	}
	if page.Name != "Notes" || page.Color != "#fff" || page.Access != pagePrivateAccess {
		t.Errorf("a writable field was not written: %+v", page)
	}
	if page.ParentID == nil || *page.ParentID != "parent-id" {
		t.Errorf("the parent is %v", page.ParentID)
	}
	// A null parent cuts the page loose rather than being ignored.
	applyPagePayload(&page, map[string]any{"parent": nil})
	if page.ParentID != nil {
		t.Errorf("the parent survived as %v", page.ParentID)
	}
}
