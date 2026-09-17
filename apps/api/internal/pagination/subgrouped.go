package pagination

import (
	"errors"
	"sort"
)

// SubGroupedResult is one group of a doubly-nested response: its sub-groups, and its own total.
type SubGroupedResult struct {
	Results      map[string]GroupedResult `json:"results"`
	TotalResults int                      `json:"total_results"`
}

// ErrUnseededSubGroup is what the plain path does with a row whose group or sub-group was not pre-seeded: it indexes the dictionary without checking, raises KeyError, and answers 500.
//
// It is unreachable when the totals come from the same queryset as the rows, since then every pair present in the page is present in the totals. It becomes reachable the moment the two disagree, and the port refuses rather than inventing a bucket.
var ErrUnseededSubGroup = errors.New("sub-grouped page holds a group or sub-group the totals did not seed")

// SubGroupRows is SubGroupedOffsetPaginator.process_results.
//
// The two paths part company on a row that does not fit. The plain one indexes the pre-seeded dictionary and raises; the many-to-many one checks first and drops the row silently. Both are reproduced.
//
// Only sub-groups that actually have a total are pre-seeded, so a group can come back with an empty results map even though its own total is set.
func SubGroupRows(
	groupField, subGroupField string,
	rows []SubGroupedRow,
	knownGroups []string,
	groupTotals map[string]int,
	subGroupTotals map[string]map[string]int,
) (map[string]SubGroupedResult, error) {
	if len(rows) == 0 {
		return map[string]SubGroupedResult{}, nil
	}
	processed := seedSubGroups(knownGroups, groupTotals, subGroupTotals)

	_, groupIsMulti := GroupFieldMapper[groupField]
	_, subGroupIsMulti := GroupFieldMapper[subGroupField]
	multi := groupIsMulti || subGroupIsMulti

	groupsByIssue := collectGroups(rows, func(row SubGroupedRow) *string { return row.Group }, groupIsMulti)
	subGroupsByIssue := collectGroups(rows, func(row SubGroupedRow) *string { return row.SubGroup }, subGroupIsMulti)

	for _, row := range rows {
		groupKey := groupString(row.Group)
		subGroupKey := groupString(row.SubGroup)
		group, seededGroup := processed[groupKey]
		var bucket GroupedResult
		seededSubGroup := false
		if seededGroup {
			bucket, seededSubGroup = group.Results[subGroupKey]
		}
		if !seededGroup || !seededSubGroup {
			if multi {
				// The many-to-many path checks before it indexes, so the row is dropped.
				continue
			}
			return nil, ErrUnseededSubGroup
		}

		fields := copyFields(row.Fields)
		fields["id"] = row.ID
		fields[groupField] = nullableValue(row.Group)
		fields[subGroupField] = nullableValue(row.SubGroup)
		if groupIsMulti {
			fields[GroupFieldMapper[groupField]] = idArray(groupsByIssue[row.ID])
		}
		if subGroupIsMulti {
			fields[GroupFieldMapper[subGroupField]] = idArray(subGroupsByIssue[row.ID])
		}
		bucket.Results = append(bucket.Results, fields)
		group.Results[subGroupKey] = bucket
		processed[groupKey] = group
	}
	return processed, nil
}

// SubGroupedRow is one row of a doubly-nested page.
type SubGroupedRow struct {
	ID       string
	Group    *string
	SubGroup *string
	Fields   map[string]any
}

// seedSubGroups builds the empty shape the response starts from: every known group, and under each, only the sub-groups that have a total.
func seedSubGroups(knownGroups []string, groupTotals map[string]int, subGroupTotals map[string]map[string]int) map[string]SubGroupedResult {
	processed := make(map[string]SubGroupedResult, len(knownGroups))
	for _, group := range knownGroups {
		results := map[string]GroupedResult{}
		for subGroup, total := range subGroupTotals[group] {
			results[subGroup] = GroupedResult{Results: []map[string]any{}, TotalResults: total}
		}
		processed[group] = SubGroupedResult{Results: results, TotalResults: groupTotals[group]}
	}
	return processed
}

// collectGroups gathers the distinct values an issue arrived under, which is only needed for a many-to-many axis.
func collectGroups(rows []SubGroupedRow, pick func(SubGroupedRow) *string, needed bool) map[string][]string {
	collected := map[string][]string{}
	if !needed {
		return collected
	}
	seen := map[string]map[string]bool{}
	for _, row := range rows {
		key := groupString(pick(row))
		if seen[row.ID] == nil {
			seen[row.ID] = map[string]bool{}
		}
		if seen[row.ID][key] {
			continue
		}
		seen[row.ID][key] = true
		collected[row.ID] = append(collected[row.ID], key)
	}
	// Django reads these out of a set, whose order is not stable across processes.
	for id := range collected {
		sort.Strings(collected[id])
	}
	return collected
}

// idArray is the issue's own id array, which is emptied rather than filled when the issue belongs to no group.
func idArray(groups []string) []string {
	for _, group := range groups {
		if group == NoGroup {
			return []string{}
		}
	}
	if groups == nil {
		return []string{}
	}
	return groups
}

func copyFields(fields map[string]any) map[string]any {
	copied := make(map[string]any, len(fields)+3)
	for key, value := range fields {
		copied[key] = value
	}
	return copied
}

func nullableValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
