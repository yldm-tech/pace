package pagination

import (
	"sort"
	"strconv"
)

// GroupFieldMapper names the three group-by fields that reach through a many-to-many join, and the id array on the issue that each of them corresponds to.
//
// Grouping by one of these makes the queryset fan a single issue into one row per value, which is what the multi-group path below has to undo.
var GroupFieldMapper = map[string]string{
	"labels__id":              "label_ids",
	"assignees__id":           "assignee_ids",
	"issue_module__module_id": "module_ids",
}

// NoGroup is the key an issue that belongs to no group is filed under. It is the literal string Python's str(None) produces, not a null.
const NoGroup = "None"

// GroupWindow is the range of row numbers a cursor asks for, as the grouped paginator computes it.
//
// It is deliberately not the same arithmetic as the flat paginator's: this one multiplies by the page size baked into the cursor rather than by the request's limit, and falls back to the limit only when that size is zero.
type GroupWindow struct {
	// Offset and Stop bound the window exclusively: the query keeps rows with Offset < row_number < Stop.
	Offset int
	Stop   int
}

// PlanGroupWindow is the offset computation from GroupedOffsetPaginator.get_result.
func PlanGroupWindow(page, cursorValue, limit int) GroupWindow {
	offset := page * cursorValue
	size := cursorValue
	if size == 0 {
		size = limit
	}
	return GroupWindow{Offset: offset, Stop: offset + size + 1}
}

// GroupedRow is one row of a page, reduced to what the grouping needs: its issue id, the group it arrived under, and the rest of its fields.
type GroupedRow struct {
	ID string
	// Group is the value of the group-by field, absent when the row has none.
	Group *string
	// Fields carries everything else the row holds, and is what ends up in the response.
	Fields map[string]any
}

// GroupedResult is one bucket of the response.
type GroupedResult struct {
	Results      []map[string]any `json:"results"`
	TotalResults int              `json:"total_results"`
}

// GroupRows is process_results: it turns a flat page into the grouped body the issue list returns.
//
// The two paths differ in more than mechanics. Grouping by a plain column pre-seeds every known group, so a group with no rows on this page still appears with an empty list; grouping by a many-to-many field does not, so only the groups actually present come back. An empty page returns no buckets at all either way.
func GroupRows(field string, rows []GroupedRow, knownGroups []string, totals map[string]int) map[string]GroupedResult {
	if len(rows) == 0 {
		return map[string]GroupedResult{}
	}
	if _, multi := GroupFieldMapper[field]; multi {
		return groupMultiValued(field, rows, totals)
	}
	return groupSingleValued(field, rows, knownGroups, totals)
}

// groupSingleValued files each row under its own group, ignoring any row whose group is not one of the known ones.
func groupSingleValued(field string, rows []GroupedRow, knownGroups []string, totals map[string]int) map[string]GroupedResult {
	grouped := make(map[string]GroupedResult, len(knownGroups))
	for _, group := range knownGroups {
		grouped[group] = GroupedResult{Results: []map[string]any{}, TotalResults: totals[group]}
	}
	for _, row := range rows {
		key := groupString(row.Group)
		bucket, known := grouped[key]
		if !known {
			// A row whose group is not in the known list is dropped rather than added, which is what the dictionary lookup does.
			continue
		}
		bucket.Results = append(bucket.Results, rowFields(row, field))
		grouped[key] = bucket
	}
	return grouped
}

// groupMultiValued undoes the fan-out: it collects every group an issue arrived under, rewrites the issue's own id array from that set, and files the issue under each of them.
//
// The row that lands in every bucket is the first one seen for that issue, not the one whose group matches the bucket — the duplicate check is by id alone, so the group-by field of a row in a later bucket still reads as the first row's value.
func groupMultiValued(field string, rows []GroupedRow, totals map[string]int) map[string]GroupedResult {
	groupsByIssue := map[string][]string{}
	seen := map[string]map[string]bool{}
	for _, row := range rows {
		key := groupString(row.Group)
		if seen[row.ID] == nil {
			seen[row.ID] = map[string]bool{}
		}
		if !seen[row.ID][key] {
			seen[row.ID][key] = true
			groupsByIssue[row.ID] = append(groupsByIssue[row.ID], key)
		}
	}
	// Django reads these out of a set, whose order is not stable across processes; sorting makes the response deterministic without changing what it means.
	for id := range groupsByIssue {
		sort.Strings(groupsByIssue[id])
	}

	arrayField := GroupFieldMapper[field]
	grouped := map[string]GroupedResult{}
	added := map[string]map[string]bool{}
	for _, row := range rows {
		groups := groupsByIssue[row.ID]
		fields := rowFields(row, field)
		// An issue that belongs to no group carries an empty array rather than the literal None.
		if containsGroup(groups, NoGroup) {
			fields[arrayField] = []string{}
		} else {
			fields[arrayField] = groups
		}
		for _, group := range groups {
			if added[group] == nil {
				added[group] = map[string]bool{}
			}
			if added[group][row.ID] {
				continue
			}
			added[group][row.ID] = true
			bucket := grouped[group]
			bucket.Results = append(bucket.Results, fields)
			bucket.TotalResults = totals[group]
			grouped[group] = bucket
		}
	}
	return grouped
}

// rowFields rebuilds the row's dictionary, including the group-by field itself, which Django leaves in the values() projection.
func rowFields(row GroupedRow, field string) map[string]any {
	fields := make(map[string]any, len(row.Fields)+2)
	for key, value := range row.Fields {
		fields[key] = value
	}
	fields["id"] = row.ID
	if row.Group != nil {
		fields[field] = *row.Group
	} else {
		fields[field] = nil
	}
	return fields
}

// groupString is str() over the group value, which turns a missing group into the literal None.
func groupString(value *string) string {
	if value == nil {
		return NoGroup
	}
	return *value
}

func containsGroup(groups []string, needle string) bool {
	for _, group := range groups {
		if group == needle {
			return true
		}
	}
	return false
}

// MaxHits is the total_pages the grouped response reports: the largest group's size divided by the page size, and zero when the page is empty.
func MaxHits(largestGroup, limit int, hasResults bool) int {
	if !hasResults || limit == 0 {
		return 0
	}
	pages := largestGroup / limit
	if largestGroup%limit != 0 {
		pages++
	}
	return pages
}

// GroupCursor renders the cursor the grouped response hands back, which keeps the flat paginator's three-part shape.
func GroupCursor(value, page int, isPrev bool) string {
	previous := "0"
	if isPrev {
		previous = "1"
	}
	return strconv.Itoa(value) + ":" + strconv.Itoa(page) + ":" + previous
}
