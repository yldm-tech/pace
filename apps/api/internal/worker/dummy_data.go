package worker

import (
	"context"
	"log/slog"
	"math/rand"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CreateDummyDataTask fills a project with made-up work so somebody can see what a busy workspace looks like. It is what manage.py create_dummy_data queues.
const CreateDummyDataTask = "plane.bgtasks.dummy_data_task.create_dummy_data"

// DummyDataTasks builds that project.
type DummyDataTasks struct {
	db     *gorm.DB
	logger *slog.Logger
	clock  func() time.Time
	random *rand.Rand
}

func NewDummyDataTasks(db *gorm.DB, logger *slog.Logger) *DummyDataTasks {
	return &DummyDataTasks{
		db: db, logger: logger, clock: time.Now,
		random: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (tasks *DummyDataTasks) Register(consumer *Consumer) {
	consumer.Register(CreateDummyDataTask, tasks.createDummyData)
}

// createDummyData reproduces create_dummy_data: a whole project's worth of made-up rows, in the order the task writes them.
//
// What it writes is random by design, so this does not reproduce Faker's output — a name here is not the name Faker would have picked. What it does reproduce is the shape: how many rows of what, which columns are filled, which are left alone, and every place the original writes less than it looks like it does.
func (tasks *DummyDataTasks) createDummyData(ctx context.Context, arguments []any, keywords map[string]any) error {
	slug := stringArgument(arguments, keywords, 0, "slug")
	email := stringArgument(arguments, keywords, 1, "email")
	members := stringListArgument(arguments, keywords, 2, "members")
	issueCount := intArgument(arguments, keywords, 3, "issue_count", 0)
	cycleCount := intArgument(arguments, keywords, 4, "cycle_count", 0)
	moduleCount := intArgument(arguments, keywords, 5, "module_count", 0)
	pagesCount := intArgument(arguments, keywords, 6, "pages_count", 0)
	intakeIssueCount := intArgument(arguments, keywords, 7, "intake_issue_count", 0)

	var workspaceID string
	var ids []string
	if err := tasks.db.WithContext(ctx).Table("workspaces").Where("slug = ? AND deleted_at IS NULL", slug).Limit(1).Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return commandMissing("workspace", slug)
	}
	workspaceID = ids[0]

	var userIDs []string
	if err := tasks.db.WithContext(ctx).Table("users").Where("email = ?", email).Limit(1).Pluck("id", &userIDs).Error; err != nil {
		return err
	}
	if len(userIDs) == 0 {
		return commandMissing("user", email)
	}
	userID := userIDs[0]

	projectID, err := tasks.createProject(ctx, workspaceID, userID)
	if err != nil {
		return err
	}
	if err := tasks.createProjectMembers(ctx, workspaceID, projectID, members); err != nil {
		return err
	}
	if err := tasks.createStates(ctx, workspaceID, projectID, userID); err != nil {
		return err
	}
	if err := tasks.createLabels(ctx, workspaceID, projectID, userID); err != nil {
		return err
	}
	if err := tasks.createCycles(ctx, workspaceID, projectID, userID, cycleCount); err != nil {
		return err
	}
	if err := tasks.createModules(ctx, workspaceID, projectID, moduleCount); err != nil {
		return err
	}
	if err := tasks.createPages(ctx, workspaceID, projectID, userID, pagesCount); err != nil {
		return err
	}
	if err := tasks.createPageLabels(ctx, workspaceID, projectID, pagesCount); err != nil {
		return err
	}
	if _, err := tasks.createIssues(ctx, workspaceID, projectID, userID, issueCount); err != nil {
		return err
	}
	if err := tasks.createIntakeIssues(ctx, workspaceID, projectID, userID, intakeIssueCount); err != nil {
		return err
	}
	// create_issue_parent is called here and writes nothing at all; see the note on it.
	tasks.createIssueParent()
	if err := tasks.createIssueAssignees(ctx, workspaceID, projectID, issueCount); err != nil {
		return err
	}
	if err := tasks.createIssueLabels(ctx, workspaceID, projectID); err != nil {
		return err
	}
	if err := tasks.createCycleIssues(ctx, workspaceID, projectID, issueCount); err != nil {
		return err
	}
	return tasks.createModuleIssues(ctx, workspaceID, projectID)
}

func commandMissing(kind, name string) error {
	return &dummyDataMissing{kind: kind, name: name}
}

type dummyDataMissing struct{ kind, name string }

func (err *dummyDataMissing) Error() string {
	return "dummy data: no " + err.kind + " named " + err.name
}

// createProject makes the project itself and its creator's membership. The identifier is the made-up name cut to somewhere between two and twelve characters and uppercased, so two runs can collide — which is why the name carries five characters of a uuid.
func (tasks *DummyDataTasks) createProject(ctx context.Context, workspaceID, userID string) (string, error) {
	name := tasks.fakeName()
	suffix := uuid.NewString()[:5]
	limit := 12
	if len(name)-1 < 12 {
		limit = len(name) - 1
	}
	if limit < 2 {
		limit = 2
	}
	identifier := strings.ToUpper(name[:2+tasks.random.Intn(limit-1)])

	now := tasks.clock().UTC()
	projectID := uuid.NewString()
	err := tasks.db.WithContext(ctx).Table("projects").Create(map[string]any{
		"id": projectID, "created_at": now, "updated_at": now,
		"created_by_id": userID, "updated_by_id": nil,
		"workspace_id": workspaceID, "name": name + "_" + suffix, "identifier": identifier,
		"description": "", "network": 2, "intake_view": true,
		"module_view": true, "cycle_view": true, "issue_views_view": true, "page_view": true,
		"is_time_tracking_enabled": false, "is_issue_type_enabled": false, "guest_view_all_features": false,
		"archive_in": 0, "close_in": 0, "logo_props": "{}", "timezone": "UTC",
	}).Error
	if err != nil {
		return "", err
	}
	// ProjectMember.objects.create runs the model's save, so the creator's ordering for this project is seeded ahead of everything else they have.
	if err := tasks.addMember(ctx, workspaceID, projectID, userID, 20, now, true); err != nil {
		return "", err
	}
	return projectID, nil
}

// createProjectMembers adds everybody else named, and does it with bulk_create — so their ordering for the project is never seeded and their sort order is a random number rather than the column default.
func (tasks *DummyDataTasks) createProjectMembers(ctx context.Context, workspaceID, projectID string, members []string) error {
	if len(members) == 0 {
		return nil
	}
	var memberIDs []string
	err := tasks.db.WithContext(ctx).Table("users").Where("email IN ?", members).Pluck("id", &memberIDs).Error
	if err != nil {
		return err
	}
	now := tasks.clock().UTC()
	for _, memberID := range memberIDs {
		if err := tasks.addMember(ctx, workspaceID, projectID, memberID, 20, now, false); err != nil {
			return err
		}
	}
	return nil
}

// addMember writes one membership. seedOrdering is what tells the two paths apart: the creator goes through the model's save and everybody else through bulk_create, which skips it.
func (tasks *DummyDataTasks) addMember(ctx context.Context, workspaceID, projectID, memberID string, role int, now time.Time, seedOrdering bool) error {
	sortOrder := float64(65535)
	if seedOrdering {
		var minimum *float64
		err := tasks.db.WithContext(ctx).Table("project_user_properties").
			Where("workspace_id = ? AND user_id = ? AND deleted_at IS NULL", workspaceID, memberID).
			Select("MIN(sort_order)").Scan(&minimum).Error
		if err != nil {
			return err
		}
		propertyOrder := float64(65535)
		if minimum != nil {
			propertyOrder = *minimum - 10000
		}
		err = tasks.db.WithContext(ctx).Table("project_user_properties").Create(map[string]any{
			"id": uuid.NewString(), "created_at": now, "updated_at": now,
			"created_by_id": nil, "updated_by_id": nil,
			"project_id": projectID, "workspace_id": workspaceID, "user_id": memberID,
			"filters": "{}", "display_filters": "{}", "display_properties": "{}",
			"rich_filters": "{}", "preferences": "{}", "sort_order": propertyOrder,
		}).Error
		if err != nil {
			return err
		}
	} else {
		sortOrder = float64(tasks.random.Intn(65536))
	}
	// ignore_conflicts, so somebody already in the project is left as they were.
	return tasks.db.WithContext(ctx).Table("project_members").
		Clauses(onConflictDoNothing()).Create(map[string]any{
		"id": uuid.NewString(), "created_at": now, "updated_at": now,
		"created_by_id": nil, "updated_by_id": nil,
		"project_id": projectID, "workspace_id": workspaceID, "member_id": memberID,
		"role": role, "is_active": true, "sort_order": sortOrder,
		"view_props": "{}", "default_props": "{}", "preferences": "{}",
	}).Error
}

// dummyStates is the five states the task writes. It is not DEFAULT_STATES: the colours differ and there is no triage state at all, so a project made this way has nothing for its intake to file into.
var dummyStates = []struct {
	Name     string
	Color    string
	Sequence float64
	Group    string
	Default  bool
}{
	{"Backlog", "#A3A3A3", 15000, "backlog", true},
	{"Todo", "#3A3A3A", 25000, "unstarted", false},
	{"In Progress", "#F59E0B", 35000, "started", false},
	{"Done", "#16A34A", 45000, "completed", false},
	{"Cancelled", "#EF4444", 55000, "cancelled", false},
}

func (tasks *DummyDataTasks) createStates(ctx context.Context, workspaceID, projectID, userID string) error {
	now := tasks.clock().UTC()
	rows := make([]map[string]any, 0, len(dummyStates))
	for _, state := range dummyStates {
		rows = append(rows, map[string]any{
			"id": uuid.NewString(), "created_at": now, "updated_at": now,
			"created_by_id": userID, "updated_by_id": nil,
			"project_id": projectID, "workspace_id": workspaceID,
			"name": state.Name, "color": state.Color, "sequence": state.Sequence,
			"group": state.Group, "default": state.Default, "description": "",
			// bulk_create skips State.save, so the slug is never filled in.
			"slug": "", "is_triage": false,
		})
	}
	return tasks.db.WithContext(ctx).Table("states").Create(rows).Error
}

// createLabels writes fifty labels whatever the counts asked for, because the number is a literal in the task.
func (tasks *DummyDataTasks) createLabels(ctx context.Context, workspaceID, projectID, userID string) error {
	now := tasks.clock().UTC()
	rows := make([]map[string]any, 0, 50)
	for index := 0; index < 50; index++ {
		rows = append(rows, map[string]any{
			"id": uuid.NewString(), "created_at": now, "updated_at": now,
			"created_by_id": userID, "updated_by_id": nil,
			"project_id": projectID, "workspace_id": workspaceID,
			"name": tasks.fakeColorName(), "color": tasks.fakeHexColor(),
			"description": "", "sort_order": float64(tasks.random.Intn(65536)),
		})
	}
	return tasks.db.WithContext(ctx).Table("labels").Clauses(onConflictDoNothing()).Create(rows).Error
}

// createCycles writes one cycle more than it was asked for, because the loop runs while the count is less than *or equal to* what was asked. Reproduced rather than corrected.
func (tasks *DummyDataTasks) createCycles(ctx context.Context, workspaceID, projectID, userID string, cycleCount int) error {
	now := tasks.clock().UTC()
	rows := make([]map[string]any, 0, cycleCount+1)
	for index := 0; index <= cycleCount; index++ {
		start, end := tasks.fakeDateRange()
		rows = append(rows, map[string]any{
			"id": uuid.NewString(), "created_at": now, "updated_at": now,
			"created_by_id": nil, "updated_by_id": nil,
			"project_id": projectID, "workspace_id": workspaceID,
			"name": tasks.fakeName(), "description": "", "owned_by_id": userID,
			"sort_order": float64(tasks.random.Intn(65536)),
			"start_date": start, "end_date": end,
			"view_props": "{}", "progress_snapshot": "{}", "logo_props": "{}",
			"timezone": "UTC", "version": 1,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return tasks.db.WithContext(ctx).Table("cycles").Clauses(onConflictDoNothing()).Create(rows).Error
}

func (tasks *DummyDataTasks) createModules(ctx context.Context, workspaceID, projectID string, moduleCount int) error {
	if moduleCount <= 0 {
		return nil
	}
	now := tasks.clock().UTC()
	rows := make([]map[string]any, 0, moduleCount)
	for index := 0; index < moduleCount; index++ {
		start, end := tasks.fakeDateRange()
		rows = append(rows, map[string]any{
			"id": uuid.NewString(), "created_at": now, "updated_at": now,
			"created_by_id": nil, "updated_by_id": nil,
			"project_id": projectID, "workspace_id": workspaceID,
			"name": tasks.fakeName(), "description": "", "status": "backlog",
			"sort_order": float64(tasks.random.Intn(65536)),
			"start_date": start, "target_date": end,
			"view_props": "{}", "logo_props": "{}",
		})
	}
	return tasks.db.WithContext(ctx).Table("modules").Clauses(onConflictDoNothing()).Create(rows).Error
}

// createPages writes the pages and then attaches each to the project. A page is written with no project of its own, since a page belongs to projects through a table rather than a column.
func (tasks *DummyDataTasks) createPages(ctx context.Context, workspaceID, projectID, userID string, pagesCount int) error {
	if pagesCount <= 0 {
		return nil
	}
	now := tasks.clock().UTC()
	pageIDs := make([]string, 0, pagesCount)
	rows := make([]map[string]any, 0, pagesCount)
	for index := 0; index < pagesCount; index++ {
		pageID := uuid.NewString()
		pageIDs = append(pageIDs, pageID)
		text := tasks.fakeText(60000)
		rows = append(rows, map[string]any{
			"id": pageID, "created_at": now, "updated_at": now,
			"created_by_id": nil, "updated_by_id": nil,
			"workspace_id": workspaceID, "name": tasks.fakeName(), "owned_by_id": userID,
			"access": tasks.random.Intn(2), "color": tasks.fakeHexColor(),
			"description_html": "<p>" + text + "</p>", "description_json": "{}",
			// bulk_create skips Page.save, so the stripped copy is never filled in even though the html is.
			"description_stripped": nil,
			"archived_at":          nil, "is_locked": false, "is_global": false,
			"view_props": "{}", "logo_props": "{}", "sort_order": 65535,
		})
	}
	if err := tasks.db.WithContext(ctx).Table("pages").Clauses(onConflictDoNothing()).Create(rows).Error; err != nil {
		return err
	}
	links := make([]map[string]any, 0, len(pageIDs))
	for _, pageID := range pageIDs {
		links = append(links, map[string]any{
			"id": uuid.NewString(), "created_at": now, "updated_at": now,
			"created_by_id": nil, "updated_by_id": nil,
			"page_id": pageID, "project_id": projectID, "workspace_id": workspaceID,
		})
	}
	return tasks.db.WithContext(ctx).Table("project_pages").Create(links).Error
}

// createPageLabels puts labels on half the pages, each getting somewhere between none and all but one of them.
func (tasks *DummyDataTasks) createPageLabels(ctx context.Context, workspaceID, projectID string, pagesCount int) error {
	if pagesCount/2 == 0 {
		return nil
	}
	labels, err := tasks.projectLabels(ctx, projectID)
	if err != nil || len(labels) == 0 {
		return err
	}
	var pages []string
	err = tasks.db.WithContext(ctx).Table("pages p").
		Joins("JOIN project_pages pp ON pp.page_id = p.id AND pp.deleted_at IS NULL").
		Where("pp.project_id = ? AND p.deleted_at IS NULL", projectID).Pluck("p.id", &pages).Error
	if err != nil {
		return err
	}
	pages = tasks.sample(pages, pagesCount/2)

	now := tasks.clock().UTC()
	rows := make([]map[string]any, 0)
	for _, pageID := range pages {
		for _, labelID := range tasks.sample(labels, tasks.random.Intn(len(labels))) {
			rows = append(rows, map[string]any{
				"id": uuid.NewString(), "created_at": now, "updated_at": now,
				"created_by_id": nil, "updated_by_id": nil,
				"page_id": pageID, "label_id": labelID, "workspace_id": workspaceID,
			})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return tasks.db.WithContext(ctx).Table("page_labels").Clauses(onConflictDoNothing()).CreateInBatches(rows, 1000).Error
}

// createIssues writes the work items, their sequence rows and one activity each.
//
// Two things are worth knowing. The sort order it starts from is read off a *randomly chosen* state rather than off each work item's own, so the ordering it hands out means nothing. And the name is the first 254 characters of the same text the description holds, so every work item is named by its own opening paragraph.
func (tasks *DummyDataTasks) createIssues(ctx context.Context, workspaceID, projectID, userID string, issueCount int) ([]string, error) {
	if issueCount <= 0 {
		return nil, nil
	}
	var states []string
	err := tasks.db.WithContext(ctx).Table("states").
		Where("workspace_id = ? AND project_id = ? AND \"group\" <> 'triage' AND deleted_at IS NULL", workspaceID, projectID).
		Pluck("id", &states).Error
	if err != nil {
		return nil, err
	}
	if len(states) == 0 {
		return nil, nil
	}
	var creators []string
	err = tasks.db.WithContext(ctx).Table("project_members").
		Where("workspace_id = ? AND project_id = ? AND deleted_at IS NULL", workspaceID, projectID).
		Pluck("member_id", &creators).Error
	if err != nil {
		return nil, err
	}
	if len(creators) == 0 {
		return nil, nil
	}

	var largestSequence *int
	err = tasks.db.WithContext(ctx).Table("issue_sequences").
		Where("project_id = ? AND deleted_at IS NULL", projectID).Select("MAX(sequence)").Scan(&largestSequence).Error
	if err != nil {
		return nil, err
	}
	nextSequence := 1
	if largestSequence != nil {
		nextSequence = *largestSequence + 1
	}

	var largestSort *float64
	err = tasks.db.WithContext(ctx).Table("issues").
		Where("project_id = ? AND state_id = ? AND deleted_at IS NULL", projectID, states[tasks.random.Intn(len(states))]).
		Select("MAX(sort_order)").Scan(&largestSort).Error
	if err != nil {
		return nil, err
	}
	sortOrder := float64(65535)
	if largestSort != nil {
		sortOrder = *largestSort + 10000
	}

	now := tasks.clock().UTC()
	issueIDs := make([]string, 0, issueCount)
	issues := make([]map[string]any, 0, issueCount)
	sequences := make([]map[string]any, 0, issueCount)
	activities := make([]map[string]any, 0, issueCount)
	for index := 0; index < issueCount; index++ {
		start, end := tasks.fakeDateRange()
		text := tasks.fakeText(3000)
		issueID := uuid.NewString()
		issueIDs = append(issueIDs, issueID)
		issues = append(issues, map[string]any{
			"id": issueID, "created_at": now, "updated_at": now,
			"created_by_id": creators[tasks.random.Intn(len(creators))], "updated_by_id": nil,
			"project_id": projectID, "workspace_id": workspaceID,
			"state_id":         states[tasks.random.Intn(len(states))],
			"name":             truncateRunes(text, 254),
			"description_html": "<p>" + text + "</p>", "description_stripped": text, "description_json": "{}",
			"sequence_id": nextSequence, "sort_order": sortOrder,
			"start_date": start, "target_date": end,
			"priority": dummyPriorities[tasks.random.Intn(len(dummyPriorities))],
			"is_draft": false,
		})
		sequences = append(sequences, map[string]any{
			"id": uuid.NewString(), "created_at": now, "updated_at": now,
			"created_by_id": nil, "updated_by_id": nil,
			"issue_id": issueID, "sequence": nextSequence, "deleted": false,
			"project_id": projectID, "workspace_id": workspaceID,
		})
		activities = append(activities, map[string]any{
			"id": uuid.NewString(), "created_at": now, "updated_at": now,
			"created_by_id": userID, "updated_by_id": nil,
			"issue_id": issueID, "actor_id": userID, "project_id": projectID, "workspace_id": workspaceID,
			"comment": "created the issue", "verb": "created",
		})
		sortOrder += float64(tasks.random.Intn(1001))
		nextSequence++
	}

	err = tasks.db.WithContext(ctx).Table("issues").Clauses(onConflictDoNothing()).CreateInBatches(issues, 1000).Error
	if err != nil {
		return nil, err
	}
	if err := tasks.db.WithContext(ctx).Table("issue_sequences").CreateInBatches(sequences, 100).Error; err != nil {
		return nil, err
	}
	if err := tasks.db.WithContext(ctx).Table("issue_activities").CreateInBatches(activities, 100).Error; err != nil {
		return nil, err
	}
	return issueIDs, nil
}

var dummyPriorities = []string{"urgent", "high", "medium", "low", "none"}

// createIntakeIssues makes *more* work items and files those in the intake, rather than filing any of the ones already made. So asking for a hundred work items and ten intake ones leaves a hundred and ten.
func (tasks *DummyDataTasks) createIntakeIssues(ctx context.Context, workspaceID, projectID, userID string, intakeIssueCount int) error {
	issueIDs, err := tasks.createIssues(ctx, workspaceID, projectID, userID, intakeIssueCount)
	if err != nil || len(issueIDs) == 0 {
		return err
	}
	now := tasks.clock().UTC()
	var intakeIDs []string
	err = tasks.db.WithContext(ctx).Table("intakes").
		Where("project_id = ? AND name = 'Intake' AND is_default = TRUE AND deleted_at IS NULL", projectID).
		Limit(1).Pluck("id", &intakeIDs).Error
	if err != nil {
		return err
	}
	intakeID := ""
	if len(intakeIDs) > 0 {
		intakeID = intakeIDs[0]
	} else {
		intakeID = uuid.NewString()
		// get_or_create names no workspace, so the row is built without one and ProjectBaseModel.save fills it in from the project.
		err = tasks.db.WithContext(ctx).Table("intakes").Create(map[string]any{
			"id": intakeID, "created_at": now, "updated_at": now,
			"created_by_id": nil, "updated_by_id": nil,
			"project_id": projectID, "workspace_id": workspaceID,
			"name": "Intake", "description": "", "is_default": true,
			"view_props": "{}", "logo_props": "{}",
		}).Error
		if err != nil {
			return err
		}
	}

	rows := make([]map[string]any, 0, len(issueIDs))
	for _, issueID := range issueIDs {
		status := dummyIntakeStatuses[tasks.random.Intn(len(dummyIntakeStatuses))]
		var snoozed any
		if status == 0 {
			// Only a snoozed one carries the moment it wakes up, which is somewhere in the next month.
			snoozed = tasks.clock().UTC().AddDate(0, 0, 1+tasks.random.Intn(30))
		}
		rows = append(rows, map[string]any{
			"id": uuid.NewString(), "created_at": now, "updated_at": now,
			"created_by_id": nil, "updated_by_id": nil,
			"intake_id": intakeID, "issue_id": issueID, "status": status,
			"snoozed_till": snoozed, "source": "IN-APP",
			"project_id": projectID, "workspace_id": workspaceID, "extra": "{}",
		})
	}
	return tasks.db.WithContext(ctx).Table("intake_issues").CreateInBatches(rows, 100).Error
}

var dummyIntakeStatuses = []int{-2, -1, 0, 1, 2}

// createIssueParent writes nothing, and that is the whole of it.
//
// Upstream builds an empty list, sets a parent on each sub-issue inside the loop, never appends any of them to that list, and then hands the empty list to bulk_update. Nothing is saved. Reproduced rather than corrected, because a dummy project with a quarter of its work items suddenly parented would not be the project this task has always made.
func (tasks *DummyDataTasks) createIssueParent() {}

// createIssueAssignees assigns half the work items, each to somewhere between nobody and all but one of the project's members.
func (tasks *DummyDataTasks) createIssueAssignees(ctx context.Context, workspaceID, projectID string, issueCount int) error {
	if issueCount/2 == 0 {
		return nil
	}
	var assignees []string
	err := tasks.db.WithContext(ctx).Table("project_members").
		Where("project_id = ? AND deleted_at IS NULL", projectID).Pluck("member_id", &assignees).Error
	if err != nil || len(assignees) == 0 {
		return err
	}
	issues, err := tasks.projectIssues(ctx, projectID)
	if err != nil {
		return err
	}
	issues = tasks.sample(issues, issueCount/2)

	now := tasks.clock().UTC()
	rows := make([]map[string]any, 0)
	for _, issueID := range issues {
		for _, assignee := range tasks.sample(assignees, tasks.random.Intn(len(assignees))) {
			rows = append(rows, map[string]any{
				"id": uuid.NewString(), "created_at": now, "updated_at": now,
				"created_by_id": nil, "updated_by_id": nil,
				"issue_id": issueID, "assignee_id": assignee,
				"project_id": projectID, "workspace_id": workspaceID,
			})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return tasks.db.WithContext(ctx).Table("issue_assignees").Clauses(onConflictDoNothing()).CreateInBatches(rows, 1000).Error
}

// createIssueLabels puts up to five labels on *every* work item rather than on half of them, because the line that picked half is commented out upstream.
func (tasks *DummyDataTasks) createIssueLabels(ctx context.Context, workspaceID, projectID string) error {
	labels, err := tasks.projectLabels(ctx, projectID)
	if err != nil || len(labels) == 0 {
		return err
	}
	issues, err := tasks.projectIssues(ctx, projectID)
	if err != nil {
		return err
	}
	now := tasks.clock().UTC()
	rows := make([]map[string]any, 0)
	for _, issueID := range issues {
		for _, labelID := range tasks.sample(labels, tasks.random.Intn(6)) {
			rows = append(rows, map[string]any{
				"id": uuid.NewString(), "created_at": now, "updated_at": now,
				"created_by_id": nil, "updated_by_id": nil,
				"issue_id": issueID, "label_id": labelID,
				"project_id": projectID, "workspace_id": workspaceID,
			})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return tasks.db.WithContext(ctx).Table("issue_labels").Clauses(onConflictDoNothing()).CreateInBatches(rows, 1000).Error
}

// createCycleIssues puts half the work items into one cycle each.
func (tasks *DummyDataTasks) createCycleIssues(ctx context.Context, workspaceID, projectID string, issueCount int) error {
	if issueCount/2 == 0 {
		return nil
	}
	var cycles []string
	err := tasks.db.WithContext(ctx).Table("cycles").
		Where("project_id = ? AND deleted_at IS NULL", projectID).Pluck("id", &cycles).Error
	if err != nil || len(cycles) == 0 {
		return err
	}
	issues, err := tasks.projectIssues(ctx, projectID)
	if err != nil {
		return err
	}
	issues = tasks.sample(issues, issueCount/2)

	now := tasks.clock().UTC()
	rows := make([]map[string]any, 0, len(issues))
	for _, issueID := range issues {
		rows = append(rows, map[string]any{
			"id": uuid.NewString(), "created_at": now, "updated_at": now,
			"created_by_id": nil, "updated_by_id": nil,
			"cycle_id": cycles[tasks.random.Intn(len(cycles))], "issue_id": issueID,
			"project_id": projectID, "workspace_id": workspaceID,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return tasks.db.WithContext(ctx).Table("cycle_issues").Clauses(onConflictDoNothing()).CreateInBatches(rows, 1000).Error
}

// createModuleIssues puts every work item into up to five modules, which is the same commented-out half as the labels.
func (tasks *DummyDataTasks) createModuleIssues(ctx context.Context, workspaceID, projectID string) error {
	var modules []string
	err := tasks.db.WithContext(ctx).Table("modules").
		Where("project_id = ? AND deleted_at IS NULL", projectID).Pluck("id", &modules).Error
	if err != nil || len(modules) == 0 {
		return err
	}
	issues, err := tasks.projectIssues(ctx, projectID)
	if err != nil {
		return err
	}
	now := tasks.clock().UTC()
	rows := make([]map[string]any, 0)
	for _, issueID := range issues {
		for _, moduleID := range tasks.sample(modules, tasks.random.Intn(6)) {
			rows = append(rows, map[string]any{
				"id": uuid.NewString(), "created_at": now, "updated_at": now,
				"created_by_id": nil, "updated_by_id": nil,
				"module_id": moduleID, "issue_id": issueID,
				"project_id": projectID, "workspace_id": workspaceID,
			})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return tasks.db.WithContext(ctx).Table("module_issues").Clauses(onConflictDoNothing()).CreateInBatches(rows, 1000).Error
}

func (tasks *DummyDataTasks) projectLabels(ctx context.Context, projectID string) ([]string, error) {
	var labels []string
	err := tasks.db.WithContext(ctx).Table("labels").
		Where("project_id = ? AND deleted_at IS NULL", projectID).Pluck("id", &labels).Error
	return labels, err
}

func (tasks *DummyDataTasks) projectIssues(ctx context.Context, projectID string) ([]string, error) {
	var issues []string
	err := tasks.db.WithContext(ctx).Table("issues").
		Where("project_id = ? AND deleted_at IS NULL", projectID).Pluck("id", &issues).Error
	return issues, err
}

// sample is random.sample: up to count of them, each at most once, in a random order. Asking for more than there are gives all of them, where python would raise.
func (tasks *DummyDataTasks) sample(values []string, count int) []string {
	if count <= 0 || len(values) == 0 {
		return nil
	}
	if count > len(values) {
		count = len(values)
	}
	shuffled := make([]string, len(values))
	copy(shuffled, values)
	tasks.random.Shuffle(len(shuffled), func(first, second int) {
		shuffled[first], shuffled[second] = shuffled[second], shuffled[first]
	})
	return shuffled[:count]
}

// truncateRunes cuts to a length in characters rather than bytes, which is what python's slicing counts.
func truncateRunes(value string, length int) string {
	runes := []rune(value)
	if len(runes) <= length {
		return value
	}
	return string(runes[:length])
}
