package pagination

import (
	"errors"
	"testing"
)

// TestPerPageMatchesGetPerPage pins PerPage to BasePaginator.get_per_page in plane/utils/paginator.py:
//
//	def get_per_page(self, request, default_per_page=1000, max_per_page=1000):
//	    try: per_page = int(request.GET.get("per_page", default_per_page))
//	    except ValueError: raise ParseError(detail="Invalid per_page parameter.")
//	    max_per_page = max(max_per_page, default_per_page)
//	    if per_page > max_per_page:
//	        raise ParseError(detail=f"Invalid per_page value. Cannot exceed {max_per_page}.")
//
// A view overrides default_per_page alone -- the sticky list asks for twenty -- and the ceiling stays at the paginator's own thousand. Reading the override as the ceiling as well is what made the workspace sidebar's per_page=30 answer 400.
func TestPerPageMatchesGetPerPage(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		raw       string
		dflt, max int
		want      int
		wantErr   error
		wantText  string
	}{
		{name: "absent takes the default", raw: "", dflt: 20, max: DefaultPerPage, want: 20},
		{name: "the sidebar asks for thirty against a default of twenty", raw: "30", dflt: 20, max: DefaultPerPage, want: 30},
		{name: "the ceiling itself is allowed", raw: "1000", dflt: 20, max: DefaultPerPage, want: 1000},
		{
			name: "above the ceiling is refused, and says which ceiling",
			raw:  "1001", dflt: 20, max: DefaultPerPage,
			wantErr: ErrPerPageTooLarge, wantText: "Invalid per_page value. Cannot exceed 1000.",
		},
		{
			name: "a route that really does cap at ten still caps at ten",
			raw:  "11", dflt: 10, max: 10,
			wantErr: ErrPerPageTooLarge, wantText: "Invalid per_page value. Cannot exceed 10.",
		},
		{
			name: "a ceiling below the default is raised to it, as max() does",
			raw:  "20", dflt: 20, max: 5,
			want: 20,
		},
		{
			name: "not a number at all",
			raw:  "many", dflt: 20, max: DefaultPerPage,
			wantErr: ErrInvalidPerPage, wantText: "Invalid per_page parameter.",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := PerPage(testCase.raw, testCase.dflt, testCase.max)
			if testCase.wantErr != nil {
				if !errors.Is(err, testCase.wantErr) {
					t.Fatalf("PerPage(%q) error = %v, want %v", testCase.raw, err, testCase.wantErr)
				}
				if err.Error() != testCase.wantText {
					t.Errorf("detail = %q, want %q", err.Error(), testCase.wantText)
				}
				return
			}
			if err != nil {
				t.Fatalf("PerPage(%q) unexpected error: %v", testCase.raw, err)
			}
			if got != testCase.want {
				t.Errorf("PerPage(%q) = %d, want %d", testCase.raw, got, testCase.want)
			}
		})
	}
}
