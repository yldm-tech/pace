package project

import (
	"context"

	"gorm.io/gorm"
)

// analyticsExportNames reads the lookup tables the exported rows are labelled from, and reads only the ones the chart's own axes call for.
//
// They are not the tables the analytics endpoint hands the frontend. Each one here is reduced to an id and the one name that goes into a cell, and the label table is read through the plain manager with the caller's filters applied — where the endpoint reads it through the plain manager with no filters at all.
func analyticsExportNames(ctx context.Context, database *gorm.DB, scope func() *gorm.DB, slug, xAxis, segment string, joins, conditions []string, arguments []any) (analyticsNames, error) {
	names := analyticsNames{}
	wanted := func(axis string) bool { return xAxis == axis || segment == axis }

	if wanted(analyticsAssigneeAxis) {
		table, err := analyticsAssigneeNames(scope)
		if err != nil {
			return nil, err
		}
		names[analyticsAssigneeAxis] = table
	}
	if wanted(analyticsLabelAxis) {
		table, err := analyticsLabelNames(ctx, database, slug, joins, conditions, arguments)
		if err != nil {
			return nil, err
		}
		names[analyticsLabelAxis] = table
	}
	if wanted(analyticsStateAxis) {
		table, err := analyticsSimpleNames(scope().Distinct("i.state_id").
			Select("i.state_id AS id, (SELECT s.name FROM states s WHERE s.id = i.state_id) AS name").
			Order("i.state_id"))
		if err != nil {
			return nil, err
		}
		names[analyticsStateAxis] = table
	}
	if wanted(analyticsCycleAxis) {
		table, err := analyticsSimpleNames(scope().
			Joins("JOIN cycle_issues ec ON ec.issue_id = i.id AND ec.deleted_at IS NULL").
			Joins("JOIN cycles ecy ON ecy.id = ec.cycle_id").
			Distinct("ec.cycle_id").Select("ec.cycle_id AS id, ecy.name AS name").Order("ec.cycle_id"))
		if err != nil {
			return nil, err
		}
		names[analyticsCycleAxis] = table
	}
	if wanted(analyticsModuleAxis) {
		table, err := analyticsSimpleNames(scope().
			Joins("JOIN module_issues em ON em.issue_id = i.id AND em.deleted_at IS NULL").
			Joins("JOIN modules emo ON emo.id = em.module_id").
			Distinct("em.module_id").Select("em.module_id AS id, emo.name AS name").Order("em.module_id"))
		if err != nil {
			return nil, err
		}
		names[analyticsModuleAxis] = table
	}
	return names, nil
}

// analyticsNameRow is one id and the name it reads as.
type analyticsNameRow struct {
	ID   *string `gorm:"column:id"`
	Name *string `gorm:"column:name"`
}

func analyticsSimpleNames(query *gorm.DB) (map[string]string, error) {
	var rows []analyticsNameRow
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	table := map[string]string{}
	for _, row := range rows {
		if row.ID == nil {
			continue
		}
		name := ""
		if row.Name != nil {
			name = *row.Name
		}
		table[*row.ID] = name
	}
	return table, nil
}

// analyticsAssigneeNames is get_assignee_details, and it lists only the assignees who have a picture.
//
// That filter is the whole reason a name can be missing: somebody who never set an avatar is not in the table, so their column keeps the raw uuid. The name itself is the first and last name with one space between them, written by an f-string rather than by the model's own full_name — so it is not trimmed, and somebody with no last name is followed by a trailing space.
func analyticsAssigneeNames(scope func() *gorm.DB) (map[string]string, error) {
	var rows []struct {
		AssigneeID string  `gorm:"column:assignee_id"`
		FirstName  *string `gorm:"column:first_name"`
		LastName   *string `gorm:"column:last_name"`
	}
	err := scope().
		Joins("JOIN issue_assignees ea ON ea.issue_id = i.id").
		Joins("JOIN users eu ON eu.id = ea.assignee_id").
		Where("eu.avatar IS NOT NULL OR eu.avatar_asset_id IS NOT NULL").
		Distinct("ea.assignee_id").
		Select("ea.assignee_id, eu.first_name, eu.last_name").
		Order("ea.assignee_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	table := map[string]string{}
	for _, row := range rows {
		table[row.AssigneeID] = stringOrBlank(row.FirstName) + " " + stringOrBlank(row.LastName)
	}
	return table, nil
}

// analyticsLabelNames is get_label_details.
//
// It is the one lookup that reads through Issue.objects rather than Issue.issue_objects, so a label is listed even when every work item carrying it is archived or a draft. It does apply the caller's filters, which is where it parts company with the analytics endpoint's own label table.
func analyticsLabelNames(ctx context.Context, database *gorm.DB, slug string, joins, conditions []string, arguments []any) (map[string]string, error) {
	query := database.WithContext(ctx).Table("issues i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("w.slug = ? AND i.deleted_at IS NULL", slug)
	for _, join := range joins {
		query = query.Joins(join)
	}
	consumed := 0
	for _, condition := range conditions {
		count := countPlaceholders(condition)
		query = query.Where(condition, arguments[consumed:consumed+count]...)
		consumed += count
	}
	var rows []analyticsNameRow
	err := query.
		Joins("JOIN issue_labels el ON el.issue_id = i.id AND el.deleted_at IS NULL").
		Joins("JOIN labels elb ON elb.id = el.label_id").
		Distinct("el.label_id").Select("el.label_id AS id, elb.name AS name").
		Order("el.label_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	table := map[string]string{}
	for _, row := range rows {
		if row.ID == nil {
			continue
		}
		table[*row.ID] = stringOrBlank(row.Name)
	}
	return table, nil
}
