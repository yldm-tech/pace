package pagination

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// DefaultPerPage is the per_page BasePaginator falls back to, which is also its ceiling.
const DefaultPerPage = 1000

// OffsetCursor is the three-part cursor this paginator round-trips: the page size, the page number, and whether the client is walking backwards.
//
// It is not the cursor the version routes use. That one is "size:page:offset" with the third part inert; this one's third part is a flag, and its second part is the page rather than a raw offset.
type OffsetCursor struct {
	Value  int
	Offset int
	IsPrev bool
}

func (cursor OffsetCursor) String() string {
	previous := "0"
	if cursor.IsPrev {
		previous = "1"
	}
	return strconv.Itoa(cursor.Value) + ":" + strconv.Itoa(cursor.Offset) + ":" + previous
}

// ErrInvalidOffsetCursor is what a malformed cursor produces. Django raises ParseError, which answers 400.
var ErrInvalidOffsetCursor = errors.New("invalid cursor parameter")

// ParseOffsetCursor is Cursor.from_string. The first part is read as a float when it carries a decimal point and as an integer otherwise, and the third is read as an integer before being taken as a flag.
func ParseOffsetCursor(value string) (OffsetCursor, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return OffsetCursor{}, ErrInvalidOffsetCursor
	}
	head, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return OffsetCursor{}, ErrInvalidOffsetCursor
	}
	offset, err := strconv.Atoi(parts[1])
	if err != nil {
		return OffsetCursor{}, ErrInvalidOffsetCursor
	}
	previous, err := strconv.Atoi(parts[2])
	if err != nil {
		return OffsetCursor{}, ErrInvalidOffsetCursor
	}
	return OffsetCursor{Value: int(head), Offset: offset, IsPrev: previous != 0}, nil
}

// ErrPerPageTooLarge is what get_per_page raises when the caller asks for more than the ceiling. It is a sentinel to match on; the message a client sees carries the ceiling, as Django's does.
var ErrPerPageTooLarge = errors.New("per_page exceeds the maximum")

// perPageTooLarge renders get_per_page's ParseError verbatim. These strings go straight into the detail field of a 400, so they are the API's own wording rather than Go's, capital letter and full stop included.
type perPageTooLarge struct{ max int }

func (err perPageTooLarge) Error() string {
	return fmt.Sprintf("Invalid per_page value. Cannot exceed %d.", err.max)
}

func (err perPageTooLarge) Is(target error) bool { return target == ErrPerPageTooLarge }

// ErrInvalidPerPage is what it raises when per_page is not a number at all.
var ErrInvalidPerPage = errors.New("Invalid per_page parameter.")

// PerPage is BasePaginator.get_per_page: an absent value takes the default, a non-numeric one is refused, and one above the ceiling is refused rather than clamped.
func PerPage(raw string, defaultPerPage, maxPerPage int) (int, error) {
	if raw == "" {
		return defaultPerPage, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, ErrInvalidPerPage
	}
	if maxPerPage < defaultPerPage {
		maxPerPage = defaultPerPage
	}
	if value > maxPerPage {
		return 0, perPageTooLarge{max: maxPerPage}
	}
	return value, nil
}

// OffsetPage is the window to read and the envelope to wrap it in.
type OffsetPage struct {
	// Offset and Stop are the half-open range the query reads. Stop is one past the page on purpose: the extra row is how the paginator decides whether a next page exists.
	Offset int
	Stop   int
	// Limit is how many of the fetched rows actually go in the response.
	Limit int

	NextCursor      string
	PrevCursor      string
	NextPageResults bool
	PrevPageResults bool
	TotalCount      int
	TotalPages      int
}

// PlanOffsetPage is OffsetPaginator.get_result. The caller supplies the total and how many rows the window actually returned, since only it can run the query.
func PlanOffsetPage(perPage int, cursor OffsetCursor, total, windowRows, maxLimit int) OffsetPage {
	if maxLimit <= 0 {
		maxLimit = DefaultPerPage
	}
	limit := perPage
	if limit > maxLimit {
		limit = maxLimit
	}
	page := cursor.Offset
	// The offset is the page number times the limit, not the cursor's own value; the two agree in practice but the class uses the limit.
	offset := page * limit

	result := OffsetPage{
		Offset: offset,
		Stop:   offset + limit + 1,
		Limit:  limit,
		// The cursors carry the limit rather than whatever the caller sent.
		NextCursor: OffsetCursor{Value: limit, Offset: page + 1}.String(),
		PrevCursor: OffsetCursor{Value: limit, Offset: page - 1, IsPrev: true}.String(),
		// One more row than the page means there is a page after this one.
		NextPageResults: windowRows > limit,
		PrevPageResults: page > 0,
		TotalCount:      total,
	}
	if limit > 0 {
		result.TotalPages = total / limit
		if total%limit != 0 {
			result.TotalPages++
		}
	}
	return result
}

// Envelope is the twelve-key body BasePaginator.paginate returns, which is a different shape from the cursor paginator's nine.
//
// count is the number of rows in this page; total_count and total_results are both the whole set, which the class reports twice under two names.
func (page OffsetPage) Envelope(results any, count int, groupedBy, subGroupedBy, extraStats any) map[string]any {
	return map[string]any{
		"grouped_by":        groupedBy,
		"sub_grouped_by":    subGroupedBy,
		"total_count":       page.TotalCount,
		"next_cursor":       page.NextCursor,
		"prev_cursor":       page.PrevCursor,
		"next_page_results": page.NextPageResults,
		"prev_page_results": page.PrevPageResults,
		"count":             count,
		"total_pages":       page.TotalPages,
		"total_results":     page.TotalCount,
		"extra_stats":       extraStats,
		"results":           results,
	}
}
