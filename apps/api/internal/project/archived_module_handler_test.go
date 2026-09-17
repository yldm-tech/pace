package project

import (
	"testing"
	"time"
)

// The two module lists do not return the same fields, and the difference is easy to lose: the archived one drops the logo and both estimate sums and adds the archive time.
func TestTheArchivedModuleProjectionIsNotTheLiveOne(t *testing.T) {
	archived := time.Date(2026, 3, 4, 5, 6, 7, 123456000, time.UTC)
	row := moduleRow{Module: Module{ID: "module-id", ArchivedAt: &archived}}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)

	live := moduleListJSON(row, location)
	stale := archivedModuleListJSON(row, location)

	if len(stale) != 26 {
		t.Fatalf("the archived projection has %d fields, want 26", len(stale))
	}
	if len(live) != 28 {
		t.Fatalf("the live projection has %d fields, want 28", len(live))
	}
	for _, absent := range []string{"logo_props", "total_estimate_points", "completed_estimate_points"} {
		if _, present := stale[absent]; present {
			t.Errorf("the archived projection carries %q, which its values() call does not list", absent)
		}
	}
	if _, present := live["archived_at"]; present {
		t.Error("the live projection carries archived_at, which its values() call does not list")
	}
	for key := range stale {
		if key == "archived_at" {
			continue
		}
		if _, present := live[key]; !present {
			t.Errorf("the archived projection carries %q, which the live one does not", key)
		}
	}
}

// Only the two audit timestamps go through the timezone converter. The archive time is handed to the JSON encoder untouched, so it stays in UTC while its neighbours move.
func TestOnlyTheAuditTimestampsMoveToTheCallersTimezone(t *testing.T) {
	archived := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	data := archivedModuleListJSON(moduleRow{Module: Module{CreatedAt: created, UpdatedAt: created, ArchivedAt: &archived}}, location)

	moved, ok := data["created_at"].(time.Time)
	if !ok {
		t.Fatalf("created_at is %T, want a time", data["created_at"])
	}
	if _, offset := moved.Zone(); offset != 8*60*60 {
		t.Errorf("created_at is at offset %d, want the caller's", offset)
	}
	stayed, ok := data["archived_at"].(*time.Time)
	if !ok {
		t.Fatalf("archived_at is %T, want a time pointer", data["archived_at"])
	}
	if _, offset := stayed.Zone(); offset != 0 {
		t.Errorf("archived_at is at offset %d, want UTC", offset)
	}
	if !stayed.Equal(archived) {
		t.Errorf("archived_at is %s, want %s", stayed, archived)
	}
	if inUTC(nil) != nil {
		t.Error("a module with no archive time must render null")
	}
}
