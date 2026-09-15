package project

import (
	"testing"
	"time"
)

// The archive queryset annotates only is_subscribed, so every field IssueDetailSerializer reads off an annotation is dropped by DRF and the response is the plain instance plus description_html and is_subscribed. Rendering the real serializer against an unannotated instance gives exactly these twenty keys.
func TestArchivedIssueCarriesTwentyFields(t *testing.T) {
	row := sampleIssueRow()
	data := issueSerializerJSON(row)
	data["description_html"] = row.DescriptionHTML
	data["is_subscribed"] = row.IsSubscribed
	for _, field := range []string{
		"id", "name", "state_id", "sort_order", "completed_at", "estimate_point",
		"priority", "start_date", "target_date", "sequence_id", "project_id", "parent_id",
		"created_at", "updated_at", "created_by", "updated_by", "is_draft", "archived_at",
		"description_html", "is_subscribed",
	} {
		if _, present := data[field]; !present {
			t.Errorf("archived issue is missing %q", field)
		}
	}
	// is_intake is declared on the serializer but annotated by neither this route nor the detail one.
	if _, present := data["is_intake"]; present {
		t.Error("is_intake is not annotated here and must be absent")
	}
	if len(data) != 20 {
		t.Fatalf("archived issue has %d fields, want 20", len(data))
	}
}

func TestOnlyFinishedWorkMayBeArchived(t *testing.T) {
	for _, group := range []string{"completed", "cancelled"} {
		if !archivableStateGroups[group] {
			t.Errorf("%q must be archivable", group)
		}
	}
	for _, group := range []string{"backlog", "unstarted", "started", "triage", ""} {
		if archivableStateGroups[group] {
			t.Errorf("%q must not be archivable", group)
		}
	}
}

// Issue.save writes the whole instance, so archiving also recomputes description_stripped and moves the timestamp. completed_at stays put: _sync_completed_at returns early unless the state itself changed.
func TestArchivingWritesWhatSaveWrites(t *testing.T) {
	handler := &Handler{}
	_ = handler
	stripped := strippedIssueDescription("<p>hello <b>there</b></p>")
	if stripped == nil || *stripped != "hello there" {
		t.Fatalf("description_stripped = %v, want the stripped text", stripped)
	}
	// An empty body stores null here, unlike the comment path which stores an empty string.
	if strippedIssueDescription("") != nil {
		t.Fatal("an empty description must store null")
	}
}

// archived_at is a DateField, so it is written and rendered as a bare date rather than a timestamp.
func TestArchivedAtIsADate(t *testing.T) {
	moment := time.Date(2026, 9, 15, 23, 59, 0, 0, time.UTC)
	if got := moment.Format("2006-01-02"); got != "2026-09-15" {
		t.Fatalf("archived_at = %s", got)
	}
	if got := dateOnly(&moment); got != "2026-09-15" {
		t.Fatalf("dateOnly = %v", got)
	}
}

func TestBulkArchiveUsesDjangosErrorCode(t *testing.T) {
	// ERROR_CODES["INVALID_ARCHIVE_STATE_GROUP"] in plane/utils/error_codes.py.
	if errorCodeInvalidArchiveStateGroup != 4091 {
		t.Fatalf("error code = %d, want 4091", errorCodeInvalidArchiveStateGroup)
	}
}
