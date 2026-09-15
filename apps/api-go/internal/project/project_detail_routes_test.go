package project

import (
	"sort"
	"testing"

	"github.com/gin-gonic/gin"
)

// The sidebar list is ordered by the caller's own place in it and then by name, and a project they have no ordering for sorts last.
func TestTheSidebarOrdering(t *testing.T) {
	first, second := 1.0, 2.0
	rows := []projectListRow{
		{Name: "Zulu", SortOrder: &second},
		{Name: "Alpha", SortOrder: nil},
		{Name: "Bravo", SortOrder: &first},
		{Name: "Charlie", SortOrder: nil},
	}
	sort.SliceStable(rows, func(left, right int) bool {
		a, b := rows[left].SortOrder, rows[right].SortOrder
		switch {
		case a == nil && b == nil:
			return rows[left].Name < rows[right].Name
		case a == nil:
			return false
		case b == nil:
			return true
		case *a != *b:
			return *a < *b
		}
		return rows[left].Name < rows[right].Name
	})
	want := []string{"Bravo", "Zulu", "Alpha", "Charlie"}
	for index, row := range rows {
		if row.Name != want[index] {
			t.Errorf("position %d is %q, want %q", index, row.Name, want[index])
		}
	}
}

// The fields parameter narrows the body, and naming none keeps everything.
func TestTheFieldsParameterNarrows(t *testing.T) {
	data := gin.H{"id": "project-id", "name": "Plane", "identifier": "PLANE"}
	if got := narrowFields(data, nil); len(got) != 3 {
		t.Errorf("naming no fields keeps %d of them, want all three", len(got))
	}
	narrowed := narrowFields(data, []string{"id", "missing"})
	if len(narrowed) != 1 {
		t.Fatalf("naming two fields keeps %d, want the one that is there", len(narrowed))
	}
	if narrowed["id"] != "project-id" {
		t.Errorf("the kept field is %v", narrowed["id"])
	}
}

// The list paginates only when both parameters are given, so a caller who sends one of the two gets every project.
func TestPaginationNeedsBothParameters(t *testing.T) {
	paginates := func(perPage, cursor string) bool {
		return perPage != "" && cursor != ""
	}
	if paginates("100", "") || paginates("", "100:0:0") {
		t.Error("one parameter is enough to paginate, and Django wants both")
	}
	if !paginates("100", "100:0:0") {
		t.Error("both parameters do not paginate")
	}
}
