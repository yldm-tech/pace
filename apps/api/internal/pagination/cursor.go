// Package pagination ports plane.utils.global_paginator, the cursor scheme the version endpoints page with.
package pagination

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// MaxLimit is PAGINATOR_MAX_LIMIT.
const MaxLimit = 1000

// Cursor is the "size:page:offset" triple the client round-trips. Only the first two are read; the third is carried so the string keeps its shape.
type Cursor struct {
	PageSize int
	Page     int
	Offset   int
}

func (cursor Cursor) String() string {
	return strconv.Itoa(cursor.PageSize) + ":" + strconv.Itoa(cursor.Page) + ":" + strconv.Itoa(cursor.Offset)
}

// ErrInvalidCursor is what a malformed cursor produces. Django raises ValueError, which BaseAPIView turns into a 400.
var ErrInvalidCursor = errors.New("invalid cursor format")

// ParseCursor is PaginateCursor.from_string. It requires exactly three integers; anything else is refused.
func ParseCursor(value string) (Cursor, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return Cursor{}, ErrInvalidCursor
	}
	numbers := make([]int, 3)
	for index, part := range parts {
		parsed, err := strconv.Atoi(part)
		if err != nil {
			return Cursor{}, ErrInvalidCursor
		}
		numbers[index] = parsed
	}
	return Cursor{PageSize: numbers[0], Page: numbers[1], Offset: numbers[2]}, nil
}

// DefaultCursor is what an absent cursor parameter means.
func DefaultCursor() Cursor {
	return Cursor{PageSize: MaxLimit}
}

// Page describes one slice of a result set: which rows to read and what envelope to wrap them in.
type Page struct {
	// Start and End are the half-open range to read. End is clamped to the total, so an End at or below Start means there is nothing to read.
	Start int
	End   int

	PrevCursor string
	Cursor     string
	// NextCursor is empty when this is the last page, which the envelope renders as null.
	NextCursor      string
	PrevPageResults bool
	NextPageResults bool
	TotalResults    int
	TotalPages      int
}

// ErrEmptyPageSize is the division Django performs without guarding it: a cursor asking for a page size of zero raises ZeroDivisionError and answers 500.
var ErrEmptyPageSize = errors.New("pagination page size is zero")

// Plan works out the slice and the envelope for a cursor over a known total.
func Plan(cursor Cursor, total int) (Page, error) {
	pageSize := cursor.PageSize
	if pageSize > MaxLimit {
		pageSize = MaxLimit
	}
	if pageSize == 0 {
		// Django divides the total by the page size with no guard, so this is a 500 rather than an empty page.
		return Page{}, ErrEmptyPageSize
	}

	page := Page{
		TotalResults: total,
		TotalPages:   int(math.Ceil(float64(total) / float64(pageSize))),
	}
	if cursor.Page > 0 {
		page.Start = cursor.Page * pageSize
	}
	page.End = page.Start + pageSize
	if page.End > total {
		page.End = total
	}

	page.PrevCursor = Cursor{PageSize: pageSize, Page: cursor.Page - 1}.String()
	page.Cursor = Cursor{PageSize: pageSize, Page: cursor.Page}.String()
	if page.End < total {
		page.NextCursor = Cursor{PageSize: pageSize, Page: cursor.Page + 1}.String()
		page.NextPageResults = true
	}
	page.PrevPageResults = cursor.Page > 0
	return page, nil
}

// Envelope is the body the paginated endpoints return. results is supplied by the caller, since only it knows the row shape.
func (page Page) Envelope(results any, count int) map[string]any {
	var next any
	if page.NextCursor != "" {
		next = page.NextCursor
	}
	return map[string]any{
		"prev_cursor":       page.PrevCursor,
		"cursor":            page.Cursor,
		"next_cursor":       next,
		"prev_page_results": page.PrevPageResults,
		"next_page_results": page.NextPageResults,
		"page_count":        count,
		"total_results":     page.TotalResults,
		"total_pages":       page.TotalPages,
		"results":           results,
	}
}
