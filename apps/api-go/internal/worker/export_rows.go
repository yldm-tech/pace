package worker

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

// exportIssue is one work item's own columns plus everything select_related pulls alongside it.
type exportIssue struct {
	ID                string     `gorm:"column:id"`
	SequenceID        int        `gorm:"column:sequence_id"`
	Name              string     `gorm:"column:name"`
	Priority          string     `gorm:"column:priority"`
	StartDate         *time.Time `gorm:"column:start_date"`
	TargetDate        *time.Time `gorm:"column:target_date"`
	CompletedAt       *time.Time `gorm:"column:completed_at"`
	CreatedAt         time.Time  `gorm:"column:created_at"`
	UpdatedAt         time.Time  `gorm:"column:updated_at"`
	ArchivedAt        *time.Time `gorm:"column:archived_at"`
	IsDraft           bool       `gorm:"column:is_draft"`
	ProjectName       string     `gorm:"column:project_name"`
	ProjectIdentifier string     `gorm:"column:project_identifier"`
	StateName         *string    `gorm:"column:state_name"`
	CreatedByFirst    *string    `gorm:"column:created_by_first"`
	CreatedByLast     *string    `gorm:"column:created_by_last"`
	EstimateValue     *string    `gorm:"column:estimate_value"`
	ParentIdentifier  *string    `gorm:"column:parent_identifier"`
	ParentSequenceID  *int       `gorm:"column:parent_sequence_id"`
}

// exportIssueColumns is the select_related half of the queryset: the project, state, author, estimate point and parent every row carries.
//
// The parent's own project is joined too, because the export names a parent the way a person would read it — the parent's project identifier and its sequence number — and that project need not be the one the work item itself lives in.
const exportIssueColumns = `i.id, i.sequence_id, i.name, i.priority, i.start_date, i.target_date,
	i.completed_at, i.created_at, i.updated_at, i.archived_at, i.is_draft,
	p.name AS project_name, p.identifier AS project_identifier,
	s.name AS state_name, cu.first_name AS created_by_first, cu.last_name AS created_by_last,
	ep.value AS estimate_value,
	pp.identifier AS parent_identifier, parent.sequence_id AS parent_sequence_id`

// loadExportIssues reads the work items one export covers.
//
// Two things a reader would expect are missing on purpose, because they are missing upstream. The queryset uses Issue.objects rather than Issue.issue_objects, so drafts, archived work items and everything sitting in a project's intake are all exported alongside the rest. And the membership join filters is_active but not deleted_at, so somebody who left a project and rejoined it has two rows there — and every one of that project's work items comes back twice.
func loadExportIssues(ctx context.Context, db *gorm.DB, workspaceID string, projectIDs []string, memberID string) ([]exportIssue, error) {
	var rows []exportIssue
	if len(projectIDs) == 0 {
		return rows, nil
	}
	err := db.WithContext(ctx).Table("issues i").
		Joins("JOIN projects p ON p.id = i.project_id").
		Joins("JOIN project_members pm ON pm.project_id = i.project_id AND pm.member_id = ? AND pm.is_active = TRUE", memberID).
		Joins("LEFT JOIN states s ON s.id = i.state_id").
		Joins("LEFT JOIN users cu ON cu.id = i.created_by_id").
		Joins("LEFT JOIN estimate_points ep ON ep.id = i.estimate_point_id").
		Joins("LEFT JOIN issues parent ON parent.id = i.parent_id").
		Joins("LEFT JOIN projects pp ON pp.id = parent.project_id").
		Where("i.workspace_id = ? AND i.project_id IN ? AND p.archived_at IS NULL AND i.deleted_at IS NULL", workspaceID, projectIDs).
		Select(exportIssueColumns).Order("i.created_at DESC").Scan(&rows).Error
	return rows, err
}

// exportRelated is every list an exported work item carries, keyed by the work item it belongs to.
type exportRelated struct {
	assignees   map[string][]string
	subscribers map[string][]string
	labels      map[string][]string
	cycles      map[string][]string
	modules     map[string][]string
	links       map[string][]*orderedMap
	relations   map[string][]*orderedMap
	comments    map[string][]*orderedMap
}

// namedRow is an id paired with whatever name the list is made of.
type namedRow struct {
	IssueID string `gorm:"column:issue_id"`
	Value   string `gorm:"column:value"`
}

// loadExportRelated reads all eight lists for a page of work items.
//
// Each list is read with its own model's default manager, which means a soft-deleted row is left out — except for the assignees, which are read through the many-to-many rather than through its own model, and a many-to-many join never filters the through table. A soft-deleted assignment therefore still names its person in the export.
func loadExportRelated(ctx context.Context, db *gorm.DB, issueIDs []string) (exportRelated, error) {
	related := exportRelated{
		assignees: map[string][]string{}, subscribers: map[string][]string{},
		labels: map[string][]string{}, cycles: map[string][]string{}, modules: map[string][]string{},
		links: map[string][]*orderedMap{}, relations: map[string][]*orderedMap{},
		comments: map[string][]*orderedMap{},
	}
	if len(issueIDs) == 0 {
		return related, nil
	}

	// The assignees are ordered by the person's own created_at, because that is the ordering the User model declares and a many-to-many read takes the target's.
	var assignees []namedRow
	err := db.WithContext(ctx).Table("issue_assignees ia").
		Joins("JOIN users u ON u.id = ia.assignee_id").
		Where("ia.issue_id IN ? AND u.is_active = TRUE", issueIDs).
		Select("ia.issue_id, TRIM(CONCAT(u.first_name, ' ', u.last_name)) AS value").
		Order("u.created_at DESC").Scan(&assignees).Error
	if err != nil {
		return related, err
	}
	for _, row := range assignees {
		related.assignees[row.IssueID] = append(related.assignees[row.IssueID], row.Value)
	}

	lists := []struct {
		table   string
		join    string
		where   string
		target  map[string][]string
		valueOf string
	}{
		{"issue_subscribers s", "JOIN users u ON u.id = s.subscriber_id", "s.issue_id IN ? AND s.deleted_at IS NULL",
			related.subscribers, "TRIM(CONCAT(u.first_name, ' ', u.last_name))"},
		{"issue_labels il", "JOIN labels l ON l.id = il.label_id", "il.issue_id IN ? AND il.deleted_at IS NULL",
			related.labels, "l.name"},
		{"cycle_issues ci", "JOIN cycles c ON c.id = ci.cycle_id", "ci.issue_id IN ? AND ci.deleted_at IS NULL",
			related.cycles, "c.name"},
		{"module_issues mi", "JOIN modules m ON m.id = mi.module_id", "mi.issue_id IN ? AND mi.deleted_at IS NULL",
			related.modules, "m.name"},
	}
	for _, list := range lists {
		var rows []namedRow
		alias := strings.Fields(list.table)[1]
		err := db.WithContext(ctx).Table(list.table).Joins(list.join).Where(list.where, issueIDs).
			Select(alias + ".issue_id, " + list.valueOf + " AS value").
			Order(alias + ".created_at DESC").Scan(&rows).Error
		if err != nil {
			return related, err
		}
		for _, row := range rows {
			list.target[row.IssueID] = append(list.target[row.IssueID], row.Value)
		}
	}

	if err := loadExportLinks(ctx, db, issueIDs, related.links); err != nil {
		return related, err
	}
	if err := loadExportRelations(ctx, db, issueIDs, related.relations); err != nil {
		return related, err
	}
	if err := loadExportComments(ctx, db, issueIDs, related.comments); err != nil {
		return related, err
	}
	return related, nil
}

// loadExportLinks reads the links, which fall back to their own url when nobody gave them a title.
func loadExportLinks(ctx context.Context, db *gorm.DB, issueIDs []string, into map[string][]*orderedMap) error {
	var rows []struct {
		IssueID string  `gorm:"column:issue_id"`
		URL     string  `gorm:"column:url"`
		Title   *string `gorm:"column:title"`
	}
	err := db.WithContext(ctx).Table("issue_links").
		Where("issue_id IN ? AND deleted_at IS NULL", issueIDs).
		Select("issue_id, url, title").Order("created_at DESC").Scan(&rows).Error
	if err != nil {
		return err
	}
	for _, row := range rows {
		title := row.URL
		if row.Title != nil && *row.Title != "" {
			title = *row.Title
		}
		link := newOrderedMap()
		link.Set("url", row.URL)
		link.Set("title", title)
		into[row.IssueID] = append(into[row.IssueID], link)
	}
	return nil
}

// loadExportRelations reads both directions of every relation.
//
// The two are read separately and appended in that order, which is what leaves an outgoing relation ahead of an incoming one no matter which was made first.
func loadExportRelations(ctx context.Context, db *gorm.DB, issueIDs []string, into map[string][]*orderedMap) error {
	for _, direction := range []string{"outgoing", "incoming"} {
		owner, far := "issue_id", "related_issue_id"
		if direction == "incoming" {
			owner, far = "related_issue_id", "issue_id"
		}
		var rows []struct {
			IssueID    string `gorm:"column:owner_id"`
			Relation   string `gorm:"column:relation_type"`
			Identifier string `gorm:"column:identifier"`
			Sequence   int    `gorm:"column:sequence_id"`
		}
		err := db.WithContext(ctx).Table("issue_relations r").
			Joins("JOIN issues far ON far.id = r."+far).
			Joins("JOIN projects fp ON fp.id = far.project_id").
			Where("r."+owner+" IN ? AND r.deleted_at IS NULL", issueIDs).
			Select("r." + owner + " AS owner_id, r.relation_type, fp.identifier, far.sequence_id").
			Order("r.created_at DESC").Scan(&rows).Error
		if err != nil {
			return err
		}
		for _, row := range rows {
			relation := newOrderedMap()
			relation.Set("type", row.Relation)
			relation.Set("issue", row.Identifier+"-"+strconv.Itoa(row.Sequence))
			relation.Set("direction", direction)
			into[row.IssueID] = append(into[row.IssueID], relation)
		}
	}
	return nil
}

// loadExportComments reads the comments oldest first, which is the ordering the prefetch asks for rather than the model's own.
func loadExportComments(ctx context.Context, db *gorm.DB, issueIDs []string, into map[string][]*orderedMap) error {
	var rows []struct {
		IssueID   string     `gorm:"column:issue_id"`
		Stripped  *string    `gorm:"column:comment_stripped"`
		ActorID   *string    `gorm:"column:actor_id"`
		FirstName *string    `gorm:"column:first_name"`
		LastName  *string    `gorm:"column:last_name"`
		CreatedAt *time.Time `gorm:"column:created_at"`
	}
	err := db.WithContext(ctx).Table("issue_comments ic").
		Joins("LEFT JOIN users u ON u.id = ic.actor_id").
		Where("ic.issue_id IN ? AND ic.deleted_at IS NULL", issueIDs).
		Select("ic.issue_id, ic.comment_stripped, ic.actor_id, u.first_name, u.last_name, ic.created_at").
		Order("ic.created_at").Scan(&rows).Error
	if err != nil {
		return err
	}
	for _, row := range rows {
		author := ""
		if row.ActorID != nil {
			author = fullName(row.FirstName, row.LastName)
		}
		created := ""
		if row.CreatedAt != nil {
			// The comment's own timestamp is written in this shape rather than the ISO one the other columns use.
			created = row.CreatedAt.UTC().Format("2006-01-02 15:04:05")
		}
		comment := newOrderedMap()
		comment.Set("comment", textOrBlank(row.Stripped))
		comment.Set("created_by", author)
		comment.Set("created_at", created)
		into[row.IssueID] = append(into[row.IssueID], comment)
	}
	return nil
}

// fullName is User.full_name: the two names with one space between them, trimmed.
func fullName(first, last *string) string {
	return strings.TrimSpace(textOrBlank(first) + " " + textOrBlank(last))
}

func textOrBlank(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// exportRow is one work item as the export serializer renders it, in the order the serializer declares its fields.
//
// Three of the serializer's fields never appear: sub_issues_count, link_count and attachment_count are annotations the export queryset does not add, and a read-only field whose attribute is missing is dropped by DRF rather than rendered as null. So the spreadsheet has twenty-five columns, not twenty-eight.
func exportRow(issue exportIssue, related exportRelated) *orderedMap {
	row := newOrderedMap()
	row.Set("project_name", issue.ProjectName)
	row.Set("project_identifier", issue.ProjectIdentifier)
	row.Set("parent", exportParent(issue))
	row.Set("identifier", issue.ProjectIdentifier+"-"+strconv.Itoa(issue.SequenceID))
	row.Set("sequence_id", issue.SequenceID)
	row.Set("name", issue.Name)
	// A work item with no state renders as an empty string, since the serializer declares a default for it.
	row.Set("state_name", textOrBlank(issue.StateName))
	row.Set("priority", issue.Priority)
	row.Set("assignees", listOrEmpty(related.assignees[issue.ID]))
	row.Set("subscribers", listOrEmpty(related.subscribers[issue.ID]))
	row.Set("created_by_name", fullName(issue.CreatedByFirst, issue.CreatedByLast))
	row.Set("start_date", exportDate(issue.StartDate))
	row.Set("target_date", exportDate(issue.TargetDate))
	row.Set("completed_at", exportDateTime(issue.CompletedAt))
	row.Set("created_at", drf.ISO8601(issue.CreatedAt))
	row.Set("updated_at", drf.ISO8601(issue.UpdatedAt))
	row.Set("archived_at", exportDate(issue.ArchivedAt))
	row.Set("estimate", textOrBlank(issue.EstimateValue))
	row.Set("labels", listOrEmpty(related.labels[issue.ID]))
	row.Set("cycles", listOrEmpty(related.cycles[issue.ID]))
	row.Set("modules", listOrEmpty(related.modules[issue.ID]))
	row.Set("links", objectsOrEmpty(related.links[issue.ID]))
	row.Set("relations", objectsOrEmpty(related.relations[issue.ID]))
	row.Set("comments", objectsOrEmpty(related.comments[issue.ID]))
	row.Set("is_draft", issue.IsDraft)
	return row
}

// exportParent names the parent the way a person reads it, and is an empty string rather than null when there is none.
func exportParent(issue exportIssue) string {
	if issue.ParentSequenceID == nil {
		return ""
	}
	return textOrBlank(issue.ParentIdentifier) + "-" + strconv.Itoa(*issue.ParentSequenceID)
}

// exportDate renders a date column, which carries no time at all.
func exportDate(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.Format("2006-01-02")
}

func exportDateTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return drf.ISO8601(*value)
}

func listOrEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func objectsOrEmpty(values []*orderedMap) []*orderedMap {
	if values == nil {
		return []*orderedMap{}
	}
	return values
}
