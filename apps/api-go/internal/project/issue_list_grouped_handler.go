package project

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/pagination"
	"gorm.io/gorm"
)

// groupedIssueRow is the windowed projection: an issue row plus the group it arrived under.
type groupedIssueRow struct {
	issueListRow
	GroupValue *string `gorm:"column:group_value"`
}

// issueListGrouped is the group_by half of the list route. The rows come out of a window partitioned by the group, so one query returns a page of every group at once rather than a page of the whole set.
func (handler *Handler) issueListGrouped(c *gin.Context, user *auth.User, request issueListRequest) {
	groupBy := c.Query("group_by")
	subGroupBy := c.Query("sub_group_by")
	if !issueGroupByAllowlist[groupBy] {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid group_by field: " + groupBy})
		return
	}
	if subGroupBy != "" {
		if !issueGroupByAllowlist[subGroupBy] {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid sub_group_by field: " + subGroupBy})
			return
		}
		if groupBy == subGroupBy {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Group by and sub group by cannot have same parameters"})
			return
		}
		// The sub-grouped paginator fans the rows a second time and is not migrated, so those requests stay on Django.
		handler.internalError(c, errSubGroupingNotMigrated)
		return
	}

	window := pagination.PlanGroupWindow(request.cursor.Offset, request.cursor.Value, request.perPage)
	rows, err := handler.groupedIssueRows(c, request, groupBy, window)
	if err != nil {
		handler.internalError(c, err)
		return
	}

	// Whether a page follows is answered by asking for one row past the window rather than by counting.
	more, err := handler.groupedIssueHasMore(c, request, groupBy, window)
	if err != nil {
		handler.internalError(c, err)
		return
	}

	totals, largest, err := handler.issueGroupTotals(c, request, groupBy)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	knownGroups, err := handler.issueGroupValueList(c.Request.Context(), request, groupBy)
	if err != nil {
		handler.internalError(c, err)
		return
	}

	grouped := make([]pagination.GroupedRow, 0, len(rows))
	for _, row := range rows {
		grouped = append(grouped, pagination.GroupedRow{
			ID: row.ID, Group: row.GroupValue, Fields: issueListRowJSON(row.issueListRow),
		})
	}
	buckets := pagination.GroupRows(groupBy, grouped, knownGroups, totals)

	page := request.cursor.Offset
	body := map[string]any{
		"grouped_by":        groupBy,
		"sub_grouped_by":    nil,
		"total_count":       request.total,
		"next_cursor":       pagination.GroupCursor(request.cursor.Value, page+1, false),
		"prev_cursor":       pagination.GroupCursor(request.cursor.Value, page-1, true),
		"next_page_results": more,
		"prev_page_results": page > 0,
		"count":             len(rows),
		"total_pages":       pagination.MaxHits(largest, request.perPage, len(rows) > 0),
		"total_results":     request.total,
		"extra_stats":       nil,
		"results":           buckets,
	}
	drf.Respond(c, http.StatusOK, body)
}

// groupedIssueRows runs the window and keeps the slice of row numbers the cursor asked for.
func (handler *Handler) groupedIssueRows(c *gin.Context, request issueListRequest, groupBy string, window pagination.GroupWindow) ([]groupedIssueRow, error) {
	inner := handler.groupedIssueWindow(c, request, groupBy)
	var rows []groupedIssueRow
	err := handler.db.WithContext(c.Request.Context()).
		Table("(?) AS windowed", inner).
		Where("windowed.row_number > ? AND windowed.row_number < ?", window.Offset, window.Stop).
		Scan(&rows).Error
	return rows, err
}

// groupedIssueHasMore asks whether any row sits at or past the end of the window, which is how the paginator decides there is a next page.
func (handler *Handler) groupedIssueHasMore(c *gin.Context, request issueListRequest, groupBy string, window pagination.GroupWindow) (bool, error) {
	inner := handler.groupedIssueWindow(c, request, groupBy)
	var count int64
	err := handler.db.WithContext(c.Request.Context()).
		Table("(?) AS windowed", inner).
		Where("windowed.row_number >= ?", window.Stop).
		Limit(1).Count(&count).Error
	return count > 0, err
}

// groupedIssueWindow builds the partitioned query. The many-to-many group-bys join with a left outer, so an issue with no rows on the far side still lands in the null partition.
func (handler *Handler) groupedIssueWindow(c *gin.Context, request issueListRequest, groupBy string) *gorm.DB {
	partition := issueGroupPartition[groupBy]
	selection := issueListAnnotations() +
		",\n\t\t" + partition + " AS group_value" +
		",\n\t\tROW_NUMBER() OVER (PARTITION BY " + partition + " ORDER BY " + issueWindowOrderClause(c.Query("order_by")) + ") AS row_number"

	query := handler.issueListScope(c.Request.Context(), request).Select(selection)
	if join := issueGroupJoin[groupBy]; join != "" {
		query = query.Joins(join)
	}
	return query
}

// issueGroupTotals counts the issues in each group, under the filter the per-group totals carry, and reports the largest of them, which is what the page count is derived from.
func (handler *Handler) issueGroupTotals(c *gin.Context, request issueListRequest, groupBy string) (map[string]int, int, error) {
	partition := issueGroupPartition[groupBy]
	// The count filter is an aggregate filter rather than a predicate, which matters: a group whose every issue is excluded still appears, with a count of zero, and Django then records it as one.
	query := handler.issueListScope(c.Request.Context(), request).
		Select(partition + " AS group_value, COUNT(DISTINCT i.id) FILTER (WHERE " + issueGroupCountFilter + ") AS total").
		Group(partition)
	if join := issueGroupJoin[groupBy]; join != "" {
		query = query.Joins(join)
	}
	var rows []struct {
		GroupValue *string `gorm:"column:group_value"`
		Total      int     `gorm:"column:total"`
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	totals := map[string]int{}
	largest := 0
	for _, row := range rows {
		key := pagination.NoGroup
		if row.GroupValue != nil {
			key = *row.GroupValue
		}
		// Django counts a group with no rows as one rather than zero, which is what its total dictionary does with a zero count.
		total := row.Total
		if total == 0 {
			total = 1
		}
		totals[key] += total
		if totals[key] > largest {
			largest = totals[key]
		}
	}
	return totals, largest, nil
}

// issueGroupValueList is issue_group_values: the groups the response pre-seeds, which for a plain group-by is every known one and not only those with rows on this page.
func (handler *Handler) issueGroupValueList(ctx context.Context, request issueListRequest, groupBy string) ([]string, error) {
	source := issueGroupValues[groupBy]
	if len(source.Fixed) > 0 {
		return source.Fixed, nil
	}

	var values []string
	if source.FromFiltered {
		// These read the distinct values out of the filtered issues themselves rather than from a reference table.
		err := handler.issueListScope(ctx, request).
			Distinct(source.Column).Pluck(source.Column, &values).Error
		if err != nil {
			return nil, err
		}
		return values, nil
	}

	query := handler.db.WithContext(ctx).Table(source.Table+" g").
		Joins("JOIN workspaces gw ON gw.id = g.workspace_id").
		Where("gw.slug = ?", request.slug)
	if source.ProjectScoped {
		query = query.Where("g.project_id = ?", request.projectID)
	}
	if source.Extra != "" {
		query = query.Where("g." + source.Extra)
	}
	if err := query.Pluck("g."+source.Column, &values).Error; err != nil {
		return nil, err
	}
	if source.WithNone {
		values = append(values, pagination.NoGroup)
	}
	return values, nil
}

// errSubGroupingNotMigrated marks the one path of this route that still belongs to Django.
var errSubGroupingNotMigrated = errSubGrouping{}

type errSubGrouping struct{}

func (errSubGrouping) Error() string {
	return "issue list: sub-grouping is not migrated"
}
