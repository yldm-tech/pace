package project

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm/schema"
)

// GORM does not clear a nullable time field when the column comes back NULL: its setter is `case **time.Time: if data != nil && *data != nil`, so a NULL is a no-op and whatever the destination already held survives the scan. Re-reading into a struct that was populated by an earlier query therefore reports a stale timestamp with no error anywhere, which is how a passing archive write looked like a failing unarchive one.
//
// Every read here allocates a fresh destination, and this pins the reason.
func TestScanningNullLeavesAReusedTimeFieldAlone(t *testing.T) {
	parsed, err := schema.Parse(&Issue{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	field := parsed.LookUpField("archived_at")
	if field == nil {
		t.Fatal("issues.archived_at is not mapped")
	}
	moment := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	issue := Issue{ID: "issue-id", ArchivedAt: &moment}

	// prepareValues hands the setter reflect.New(reflect.PtrTo(field.FieldType)), whose pointee is nil for a NULL column.
	holder := reflect.New(reflect.PointerTo(field.FieldType)).Interface()
	if err := field.Set(nil, reflect.ValueOf(&issue).Elem(), holder); err != nil {
		t.Fatalf("set from a NULL holder: %v", err)
	}
	if issue.ArchivedAt == nil {
		t.Fatal("GORM now clears a nullable time field on NULL; reads may reuse a destination again")
	}

	// A non-null column does overwrite, so the hazard is only in the direction of clearing.
	later := moment.Add(24 * time.Hour)
	pointer := &later
	if err := field.Set(nil, reflect.ValueOf(&issue).Elem(), &pointer); err != nil {
		t.Fatalf("set from a value holder: %v", err)
	}
	if issue.ArchivedAt == nil || !issue.ArchivedAt.Equal(later) {
		t.Fatalf("archived_at = %v, want the scanned value", issue.ArchivedAt)
	}
}
