package worker

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"gorm.io/gorm"
)

// WorkspaceSeedTask fills a brand new workspace with the demo project. It runs on every workspace anybody makes.
const WorkspaceSeedTask = "plane.bgtasks.workspace_seed_task.workspace_seed"

// The seed data itself, copied from plane/seeds/data and diffed against it by CI.
var (
	//go:embed seeds/projects.json
	projectSeedJSON []byte
	//go:embed seeds/states.json
	stateSeedJSON []byte
	//go:embed seeds/labels.json
	labelSeedJSON []byte
	//go:embed seeds/cycles.json
	cycleSeedJSON []byte
	//go:embed seeds/modules.json
	moduleSeedJSON []byte
	//go:embed seeds/issues.json
	issueSeedJSON []byte
	//go:embed seeds/views.json
	viewSeedJSON []byte
	//go:embed seeds/pages.json
	pageSeedJSON []byte
)

// WorkspaceSeedTasks writes that demo project.
type WorkspaceSeedTasks struct {
	db     *gorm.DB
	webURL string
	logger *slog.Logger
	clock  func() time.Time
}

func NewWorkspaceSeedTasks(db *gorm.DB, webURL string, logger *slog.Logger) *WorkspaceSeedTasks {
	return &WorkspaceSeedTasks{db: db, webURL: webURL, logger: logger, clock: time.Now}
}

func (tasks *WorkspaceSeedTasks) Register(consumer *Consumer) {
	consumer.Register(WorkspaceSeedTask, tasks.workspaceSeed)
}

// workspaceSeed reproduces workspace_seed.
//
// It does not run in a transaction and it re-raises rather than swallowing, so a failure part of the way through leaves everything written up to that point behind. That is kept: the errors here are returned rather than logged and swallowed.
//
// The order matters and is not alphabetical: the cycles and the modules are written before the work items because the work items are filed into them, and the views are written before the pages for no reason anyone left behind.
func (tasks *WorkspaceSeedTasks) workspaceSeed(ctx context.Context, arguments []any, keywords map[string]any) error {
	workspaceID := stringArgument(arguments, keywords, 0, "workspace_id")

	var workspace struct {
		ID   string `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}
	err := tasks.db.WithContext(ctx).Table("workspaces").Where("id = ?", workspaceID).
		Select("id, name").Take(&workspace).Error
	if err != nil {
		return fmt.Errorf("workspace seed: no workspace %s: %w", workspaceID, err)
	}

	botID, err := tasks.createBotUser(ctx, workspace.ID)
	if err != nil {
		return err
	}

	projects, err := tasks.seedProjects(ctx, workspace.ID, workspace.Name, botID)
	if err != nil {
		return err
	}
	states, err := tasks.seedStates(ctx, workspace.ID, projects, botID)
	if err != nil {
		return err
	}
	labels, err := tasks.seedLabels(ctx, workspace.ID, projects, botID)
	if err != nil {
		return err
	}
	cycles, err := tasks.seedCycles(ctx, workspace.ID, projects, botID)
	if err != nil {
		return err
	}
	modules, err := tasks.seedModules(ctx, workspace.ID, projects, botID)
	if err != nil {
		return err
	}
	if err := tasks.seedIssues(ctx, workspace.ID, projects, states, labels, cycles, modules, botID); err != nil {
		return err
	}
	if err := tasks.seedViews(ctx, workspace.ID, projects, botID); err != nil {
		return err
	}
	return tasks.seedPages(ctx, workspace.ID, projects, botID)
}

// createBotUser makes the account everything in the demo project is created by, and puts it in the workspace as an administrator.
//
// Its email is built from WEB_URL's host, or plane.so when there is none, so two installations do not hand out the same address.
func (tasks *WorkspaceSeedTasks) createBotUser(ctx context.Context, workspaceID string) (string, error) {
	hostname := "plane.so"
	if parsed, err := url.Parse(tasks.webURL); err == nil && parsed.Hostname() != "" {
		hostname = parsed.Hostname()
	}
	password, err := auth.HashPassword(strings.ReplaceAll(uuid.NewString(), "-", ""))
	if err != nil {
		return "", err
	}
	now := tasks.clock().UTC()
	botID := uuid.NewString()
	err = tasks.db.WithContext(ctx).Table("users").Create(map[string]any{
		"id": botID, "created_at": now, "updated_at": now,
		"username": "bot_user_" + workspaceID, "display_name": "Plane",
		"first_name": "Plane", "last_name": "",
		"is_bot": true, "bot_type": "WORKSPACE_SEED",
		"email":    "bot_user_" + workspaceID + "@" + hostname,
		"password": password, "is_password_autoset": true,
		"is_active": true, "is_staff": false, "is_superuser": false,
		"is_managed": false, "is_onboarded": false, "is_tour_completed": false,
		"is_email_verified": false, "is_password_expired": false,
		"date_joined": now, "user_timezone": "UTC",
	}).Error
	if err != nil {
		return "", err
	}
	err = tasks.db.WithContext(ctx).Table("workspace_members").Create(map[string]any{
		"id": uuid.NewString(), "created_at": now, "updated_at": now,
		"created_by_id": nil, "updated_by_id": nil,
		"workspace_id": workspaceID, "member_id": botID, "role": 20,
		"company_role": "", "is_active": true,
		"view_props": "{}", "default_props": "{}", "issue_props": "{}",
		"getting_started_checklist": "{}", "tips": "{}", "explored_features": "{}",
	}).Error
	if err != nil {
		return "", err
	}
	return botID, nil
}

// seedProject is one row of projects.json. The name and the identifier it carries are thrown away: the project takes the workspace's name, and its identifier is that name's letters and digits cut to five.
type seedProject struct {
	ID          int             `json:"id"`
	Description string          `json:"description"`
	Network     int             `json:"network"`
	CoverImage  *string         `json:"cover_image"`
	LogoProps   json.RawMessage `json:"logo_props"`
}

func (tasks *WorkspaceSeedTasks) seedProjects(ctx context.Context, workspaceID, workspaceName, botID string) (map[int]string, error) {
	var seeds []seedProject
	if err := json.Unmarshal(projectSeedJSON, &seeds); err != nil {
		return nil, err
	}
	projects := map[int]string{}
	if len(seeds) == 0 {
		tasks.logger.Warn("no project seeds found, skipping project creation")
		return projects, nil
	}

	identifier := alphanumericPrefix(workspaceName, 5)
	var members []struct {
		MemberID string `gorm:"column:member_id"`
		Role     int    `gorm:"column:role"`
	}
	// Every member of the workspace, the bot included, since it was added a moment ago.
	err := tasks.db.WithContext(ctx).Table("workspace_members").
		Where("workspace_id = ? AND deleted_at IS NULL", workspaceID).
		Select("member_id, role").Scan(&members).Error
	if err != nil {
		return nil, err
	}

	now := tasks.clock().UTC()
	for _, seed := range seeds {
		projectID := uuid.NewString()
		err := tasks.db.WithContext(ctx).Table("projects").Create(map[string]any{
			"id": projectID, "created_at": now, "updated_at": now,
			"created_by_id": botID, "updated_by_id": nil,
			"workspace_id": workspaceID, "name": workspaceName, "identifier": identifier,
			"description": seed.Description, "network": seed.Network,
			"cover_image": seed.CoverImage, "logo_props": jsonOrEmptyObject(seed.LogoProps),
			// The three views the seed turns on whatever the seed file says.
			"cycle_view": true, "module_view": true, "issue_views_view": true,
			"page_view": true, "intake_view": false,
			"is_time_tracking_enabled": false, "is_issue_type_enabled": false,
			"guest_view_all_features": false, "archive_in": 0, "close_in": 0, "timezone": "UTC",
		}).Error
		if err != nil {
			return nil, err
		}

		// bulk_create, so ProjectMember.save never runs and nothing seeds anybody's ordering; the display settings below are written by hand instead.
		memberships := make([]map[string]any, 0, len(members))
		properties := make([]map[string]any, 0, len(members))
		for _, member := range members {
			memberships = append(memberships, map[string]any{
				"id": uuid.NewString(), "created_at": now, "updated_at": now,
				"created_by_id": botID, "updated_by_id": nil,
				"project_id": projectID, "workspace_id": workspaceID, "member_id": member.MemberID,
				"role": member.Role, "is_active": true, "sort_order": 65535,
				"view_props": "{}", "default_props": "{}", "preferences": "{}",
			})
			properties = append(properties, map[string]any{
				"id": uuid.NewString(), "created_at": now, "updated_at": now,
				"created_by_id": botID, "updated_by_id": nil,
				"project_id": projectID, "workspace_id": workspaceID, "user_id": member.MemberID,
				"filters": "{}", "rich_filters": "{}", "preferences": "{}", "sort_order": 65535,
				"display_filters":    seedDisplayFilters,
				"display_properties": seedDisplayProperties,
			})
		}
		if len(memberships) > 0 {
			if err := tasks.db.WithContext(ctx).Table("project_members").Create(memberships).Error; err != nil {
				return nil, err
			}
			if err := tasks.db.WithContext(ctx).Table("project_user_properties").Create(properties).Error; err != nil {
				return nil, err
			}
		}
		projects[seed.ID] = projectID
	}
	return projects, nil
}

// The display settings the seed writes for everybody in the workspace. They are the task's own literals rather than the model's defaults, and they differ: the seed hides labels, modules, the cycle, the due date, the start date, the sub-issue count and the attachment count.
const seedDisplayFilters = `{"layout": "list", "calendar": {"layout": "month", "show_weekends": false}, "group_by": "state", "order_by": "sort_order", "sub_issue": true, "sub_group_by": null, "show_empty_groups": true}`

const seedDisplayProperties = `{"key": true, "link": true, "cycle": false, "state": true, "labels": false, "modules": false, "assignee": true, "due_date": false, "estimate": true, "priority": true, "created_on": true, "issue_type": true, "start_date": false, "updated_on": true, "customer_count": true, "sub_issue_count": false, "attachment_count": false, "customer_request_count": true}`

// alphanumericPrefix is the project identifier: the workspace name's letters and digits, cut to a length.
func alphanumericPrefix(value string, length int) string {
	var builder strings.Builder
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			builder.WriteRune(character)
		}
	}
	return truncateRunes(builder.String(), length)
}

type seedState struct {
	ID        int     `json:"id"`
	ProjectID int     `json:"project_id"`
	Name      string  `json:"name"`
	Color     string  `json:"color"`
	Sequence  float64 `json:"sequence"`
	Group     string  `json:"group"`
	Default   bool    `json:"default"`
}

func (tasks *WorkspaceSeedTasks) seedStates(ctx context.Context, workspaceID string, projects map[int]string, botID string) (map[int]string, error) {
	var seeds []seedState
	if err := json.Unmarshal(stateSeedJSON, &seeds); err != nil {
		return nil, err
	}
	states := map[int]string{}
	now := tasks.clock().UTC()
	for _, seed := range seeds {
		// These go through the model's own save rather than bulk_create, so two things happen that do not happen to a normal project's states: the slug is filled in, and the sequence the seed file carries is thrown away in favour of fifteen thousand past the last one. The file's 15000/25000/35000/45000/55000 therefore land as 15000/30000/45000/60000/75000.
		sequence := seed.Sequence
		var largest *float64
		err := tasks.db.WithContext(ctx).Table("states").
			Where("project_id = ? AND deleted_at IS NULL", projects[seed.ProjectID]).
			Select("MAX(sequence)").Scan(&largest).Error
		if err != nil {
			return nil, err
		}
		if largest != nil {
			sequence = *largest + 15000
		}
		stateID := uuid.NewString()
		err = tasks.db.WithContext(ctx).Table("states").Create(map[string]any{
			"id": stateID, "created_at": now, "updated_at": now,
			"created_by_id": botID, "updated_by_id": nil,
			"project_id": projects[seed.ProjectID], "workspace_id": workspaceID,
			"name": seed.Name, "color": seed.Color, "sequence": sequence,
			"group": seed.Group, "default": seed.Default, "description": "",
			"slug": slugify(seed.Name), "is_triage": false,
		}).Error
		if err != nil {
			return nil, err
		}
		states[seed.ID] = stateID
	}
	return states, nil
}

type seedLabel struct {
	ID        int     `json:"id"`
	ProjectID int     `json:"project_id"`
	Name      string  `json:"name"`
	Color     string  `json:"color"`
	SortOrder float64 `json:"sort_order"`
}

func (tasks *WorkspaceSeedTasks) seedLabels(ctx context.Context, workspaceID string, projects map[int]string, botID string) (map[int]string, error) {
	var seeds []seedLabel
	if err := json.Unmarshal(labelSeedJSON, &seeds); err != nil {
		return nil, err
	}
	labels := map[int]string{}
	now := tasks.clock().UTC()
	for _, seed := range seeds {
		labelID := uuid.NewString()
		err := tasks.db.WithContext(ctx).Table("labels").Create(map[string]any{
			"id": labelID, "created_at": now, "updated_at": now,
			"created_by_id": botID, "updated_by_id": nil,
			"project_id": projects[seed.ProjectID], "workspace_id": workspaceID,
			"name": seed.Name, "color": seed.Color, "sort_order": seed.SortOrder, "description": "",
		}).Error
		if err != nil {
			return nil, err
		}
		labels[seed.ID] = labelID
	}
	return labels, nil
}

type seedCycle struct {
	ID        int     `json:"id"`
	ProjectID int     `json:"project_id"`
	Name      string  `json:"name"`
	SortOrder float64 `json:"sort_order"`
	Timezone  string  `json:"timezone"`
	Type      string  `json:"type"`
}

// seedCycles dates the cycles rather than reading dates out of the seed: a CURRENT one starts now and runs a fortnight, and an UPCOMING one starts the day after the project's latest cycle ends.
//
// A seed with any other type keeps whatever dates the last one worked out, because the two branches are ifs rather than a choice and the variables are not reset between them. Reproduced; the seed file only carries the two.
func (tasks *WorkspaceSeedTasks) seedCycles(ctx context.Context, workspaceID string, projects map[int]string, botID string) (map[int]string, error) {
	var seeds []seedCycle
	if err := json.Unmarshal(cycleSeedJSON, &seeds); err != nil {
		return nil, err
	}
	cycles := map[int]string{}
	now := tasks.clock().UTC()
	var start, end time.Time
	for _, seed := range seeds {
		switch seed.Type {
		case "CURRENT":
			start = now
			end = start.AddDate(0, 0, 14)
		case "UPCOMING":
			var latest []time.Time
			err := tasks.db.WithContext(ctx).Table("cycles").
				Where("project_id = ? AND deleted_at IS NULL", projects[seed.ProjectID]).
				Order("end_date DESC").Limit(1).Pluck("end_date", &latest).Error
			if err != nil {
				return nil, err
			}
			if len(latest) > 0 && !latest[0].IsZero() {
				start = latest[0].AddDate(0, 0, 1)
			} else {
				start = now.AddDate(0, 0, 14)
			}
			end = start.AddDate(0, 0, 14)
		}
		// Cycle.save puts a new cycle ten thousand ahead of every other one in the project, so the second seed lands at minus nine thousand nine hundred and ninety-nine rather than at the two the file asks for.
		sortOrder := seed.SortOrder
		var smallest *float64
		err := tasks.db.WithContext(ctx).Table("cycles").
			Where("project_id = ? AND deleted_at IS NULL", projects[seed.ProjectID]).
			Select("MIN(sort_order)").Scan(&smallest).Error
		if err != nil {
			return nil, err
		}
		if smallest != nil {
			sortOrder = *smallest - 10000
		}
		cycleID := uuid.NewString()
		err = tasks.db.WithContext(ctx).Table("cycles").Create(map[string]any{
			"id": cycleID, "created_at": now, "updated_at": now,
			"created_by_id": botID, "updated_by_id": nil,
			"project_id": projects[seed.ProjectID], "workspace_id": workspaceID,
			"name": seed.Name, "description": "", "owned_by_id": botID,
			"sort_order": sortOrder, "start_date": start, "end_date": end,
			"timezone": seed.Timezone, "version": 1,
			"view_props": "{}", "progress_snapshot": "{}", "logo_props": "{}",
		}).Error
		if err != nil {
			return nil, err
		}
		cycles[seed.ID] = cycleID
	}
	return cycles, nil
}

type seedModule struct {
	ID          int     `json:"id"`
	ProjectID   int     `json:"project_id"`
	Name        string  `json:"name"`
	SortOrder   float64 `json:"sort_order"`
	Status      string  `json:"status"`
	Description string  `json:"description"`
}

// seedModules staggers the modules: the first starts now, the second two days later, the third four, and each runs a fortnight.
func (tasks *WorkspaceSeedTasks) seedModules(ctx context.Context, workspaceID string, projects map[int]string, botID string) (map[int]string, error) {
	var seeds []seedModule
	if err := json.Unmarshal(moduleSeedJSON, &seeds); err != nil {
		return nil, err
	}
	modules := map[int]string{}
	now := tasks.clock().UTC()
	for index, seed := range seeds {
		start := now.AddDate(0, 0, index*2)
		// Module.save does to the modules what Cycle.save does to the cycles, so the three seeds land at one, minus nine thousand nine hundred and ninety-nine, and minus nineteen thousand nine hundred and ninety-nine.
		sortOrder := seed.SortOrder
		var smallest *float64
		err := tasks.db.WithContext(ctx).Table("modules").
			Where("project_id = ? AND deleted_at IS NULL", projects[seed.ProjectID]).
			Select("MIN(sort_order)").Scan(&smallest).Error
		if err != nil {
			return nil, err
		}
		if smallest != nil {
			sortOrder = *smallest - 10000
		}
		moduleID := uuid.NewString()
		err = tasks.db.WithContext(ctx).Table("modules").Create(map[string]any{
			"id": moduleID, "created_at": now, "updated_at": now,
			"created_by_id": botID, "updated_by_id": nil,
			"project_id": projects[seed.ProjectID], "workspace_id": workspaceID,
			"name": seed.Name, "description": seed.Description, "status": seed.Status,
			"sort_order": sortOrder, "start_date": start, "target_date": start.AddDate(0, 0, 14),
			"view_props": "{}", "logo_props": "{}",
		}).Error
		if err != nil {
			return nil, err
		}
		modules[seed.ID] = moduleID
	}
	return modules, nil
}

// jsonOrEmptyObject keeps an absent json column as the empty object the model defaults to.
func jsonOrEmptyObject(value json.RawMessage) string {
	if len(value) == 0 {
		return "{}"
	}
	return string(value)
}

// slugify is django.utils.text.slugify over a state's name, which State.save runs before writing.
func slugify(value string) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(value) {
		switch {
		case unicode.IsLetter(character) || unicode.IsDigit(character):
			builder.WriteRune(character)
		case character == ' ' || character == '-' || character == '_':
			builder.WriteByte('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

// seedIssue is one row of issues.json.
type seedIssue struct {
	ID                  int             `json:"id"`
	ProjectID           int             `json:"project_id"`
	StateID             int             `json:"state_id"`
	CycleID             int             `json:"cycle_id"`
	ModuleIDs           []int           `json:"module_ids"`
	Labels              []int           `json:"labels"`
	Name                string          `json:"name"`
	SequenceID          int             `json:"sequence_id"`
	DescriptionHTML     string          `json:"description_html"`
	DescriptionStripped string          `json:"description_stripped"`
	DescriptionJSON     json.RawMessage `json:"description_json"`
	SortOrder           float64         `json:"sort_order"`
	Priority            string          `json:"priority"`
}

// seedIssues writes the demo work items, each with its sequence row, its "created the issue" activity, and its links to labels, a cycle and modules.
//
// Two things the seed file says are thrown away by Issue.save. The sequence number is taken from the project's own counter rather than from the file, and the sort order is the largest already used in that state plus ten thousand rather than the file's. Both are reproduced, because they are what a work item created any other way would get too.
//
// Each work item ends up with **two** sequence rows. Issue.save writes one carrying the real number, and the task then writes another of its own that names no number at all, so it falls back to the column default of one. Reproduced rather than corrected.
func (tasks *WorkspaceSeedTasks) seedIssues(ctx context.Context, workspaceID string, projects, states, labels, cycles, modules map[int]string, botID string) error {
	var seeds []seedIssue
	if err := json.Unmarshal(issueSeedJSON, &seeds); err != nil {
		return err
	}
	now := tasks.clock().UTC()
	for _, seed := range seeds {
		projectID := projects[seed.ProjectID]
		stateID := states[seed.StateID]

		var largestSequence *int
		err := tasks.db.WithContext(ctx).Table("issue_sequences").
			Where("project_id = ? AND deleted_at IS NULL", projectID).
			Select("MAX(sequence)").Scan(&largestSequence).Error
		if err != nil {
			return err
		}
		sequence := 1
		if largestSequence != nil {
			sequence = *largestSequence + 1
		}
		var largestSort *float64
		err = tasks.db.WithContext(ctx).Table("issues").
			Where("project_id = ? AND state_id = ? AND deleted_at IS NULL", projectID, stateID).
			Select("MAX(sort_order)").Scan(&largestSort).Error
		if err != nil {
			return err
		}
		sortOrder := seed.SortOrder
		if largestSort != nil {
			sortOrder = *largestSort + 10000
		}

		issueID := uuid.NewString()
		err = tasks.db.WithContext(ctx).Table("issues").Create(map[string]any{
			"id": issueID, "created_at": now, "updated_at": now,
			"created_by_id": botID, "updated_by_id": nil,
			"project_id": projectID, "workspace_id": workspaceID, "state_id": stateID,
			"name": seed.Name, "priority": seed.Priority,
			"description_html": seed.DescriptionHTML, "description_json": jsonOrEmptyObject(seed.DescriptionJSON),
			// Issue.save recomputes this from the html rather than taking the file's, which is why the file's copy is not used.
			"description_stripped": strippedHTML(seed.DescriptionHTML),
			"sequence_id":          sequence, "sort_order": sortOrder, "is_draft": false,
		}).Error
		if err != nil {
			return err
		}

		// Issue.save's own sequence row, carrying the number the work item really has.
		err = tasks.db.WithContext(ctx).Table("issue_sequences").Create(map[string]any{
			"id": uuid.NewString(), "created_at": now, "updated_at": now,
			"created_by_id": nil, "updated_by_id": nil,
			"issue_id": issueID, "sequence": sequence, "deleted": false,
			"project_id": projectID, "workspace_id": workspaceID,
		}).Error
		if err != nil {
			return err
		}
		// And the task's own, which names no sequence and so takes the column default of one.
		err = tasks.db.WithContext(ctx).Table("issue_sequences").Create(map[string]any{
			"id": uuid.NewString(), "created_at": now, "updated_at": now,
			"created_by_id": botID, "updated_by_id": nil,
			"issue_id": issueID, "sequence": 1, "deleted": false,
			"project_id": projectID, "workspace_id": workspaceID,
		}).Error
		if err != nil {
			return err
		}

		err = tasks.db.WithContext(ctx).Table("issue_activities").Create(map[string]any{
			"id": uuid.NewString(), "created_at": now, "updated_at": now,
			"created_by_id": nil, "updated_by_id": nil,
			"issue_id": issueID, "project_id": projectID, "workspace_id": workspaceID,
			"comment": "created the issue", "verb": "created", "actor_id": botID,
			// The only place in this task that stamps an epoch, and it is the moment the row is written rather than anything to do with the work item.
			"epoch": float64(now.UnixNano()) / float64(time.Second),
		}).Error
		if err != nil {
			return err
		}

		for _, labelSeedID := range seed.Labels {
			err := tasks.db.WithContext(ctx).Table("issue_labels").Create(map[string]any{
				"id": uuid.NewString(), "created_at": now, "updated_at": now,
				"created_by_id": botID, "updated_by_id": nil,
				"issue_id": issueID, "label_id": labels[labelSeedID],
				"project_id": projectID, "workspace_id": workspaceID,
			}).Error
			if err != nil {
				return err
			}
		}
		if seed.CycleID != 0 {
			err := tasks.db.WithContext(ctx).Table("cycle_issues").Create(map[string]any{
				"id": uuid.NewString(), "created_at": now, "updated_at": now,
				"created_by_id": botID, "updated_by_id": nil,
				"issue_id": issueID, "cycle_id": cycles[seed.CycleID],
				"project_id": projectID, "workspace_id": workspaceID,
			}).Error
			if err != nil {
				return err
			}
		}
		for _, moduleSeedID := range seed.ModuleIDs {
			err := tasks.db.WithContext(ctx).Table("module_issues").Create(map[string]any{
				"id": uuid.NewString(), "created_at": now, "updated_at": now,
				"created_by_id": botID, "updated_by_id": nil,
				"issue_id": issueID, "module_id": modules[moduleSeedID],
				"project_id": projectID, "workspace_id": workspaceID,
			}).Error
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// seedView is one row of views.json.
type seedView struct {
	ID                int             `json:"id"`
	ProjectID         int             `json:"project_id"`
	Name              string          `json:"name"`
	Description       string          `json:"description"`
	Access            int             `json:"access"`
	Filters           json.RawMessage `json:"filters"`
	DisplayFilters    json.RawMessage `json:"display_filters"`
	DisplayProperties json.RawMessage `json:"display_properties"`
	RichFilters       json.RawMessage `json:"rich_filters"`
	SortOrder         float64         `json:"sort_order"`
}

// seedViews writes the saved views.
//
// The query column is not nullable and the seed file never names it, which looks like it should refuse the insert. It does not: IssueView.save fills it in from the view's own filters, and an empty filter set gives an empty object rather than running the filter translation at all.
func (tasks *WorkspaceSeedTasks) seedViews(ctx context.Context, workspaceID string, projects map[int]string, botID string) error {
	var seeds []seedView
	if err := json.Unmarshal(viewSeedJSON, &seeds); err != nil {
		return err
	}
	now := tasks.clock().UTC()
	for _, seed := range seeds {
		projectID := projects[seed.ProjectID]
		// IssueView.save puts a new view ten thousand past every other one in the project.
		sortOrder := seed.SortOrder
		var largest *float64
		err := tasks.db.WithContext(ctx).Table("issue_views").
			Where("project_id = ? AND deleted_at IS NULL", projectID).
			Select("MAX(sort_order)").Scan(&largest).Error
		if err != nil {
			return err
		}
		if largest != nil {
			sortOrder = *largest + 10000
		}
		err = tasks.db.WithContext(ctx).Table("issue_views").Create(map[string]any{
			"id": uuid.NewString(), "created_at": now, "updated_at": now,
			"created_by_id": botID, "updated_by_id": nil,
			"project_id": projectID, "workspace_id": workspaceID, "owned_by_id": botID,
			"name": seed.Name, "description": seed.Description, "access": seed.Access,
			"filters": jsonOrEmptyObject(seed.Filters), "query": "{}",
			"display_filters":    jsonOrEmptyObject(seed.DisplayFilters),
			"display_properties": jsonOrEmptyObject(seed.DisplayProperties),
			"rich_filters":       jsonOrEmptyObject(seed.RichFilters),
			"sort_order":         sortOrder, "logo_props": "{}", "is_locked": false,
		}).Error
		if err != nil {
			return err
		}
	}
	return nil
}

// seedPage is one row of pages.json.
type seedPage struct {
	ID                  int    `json:"id"`
	ProjectID           int    `json:"project_id"`
	Type                string `json:"type"`
	Name                string `json:"name"`
	Access              int    `json:"access"`
	DescriptionHTML     string `json:"description_html"`
	DescriptionStripped string `json:"description_stripped"`
}

// seedPages writes the demo pages and attaches the project ones to their project.
//
// Two things the seed file carries never reach the row. Its `description` holds the editor's json document, but the task reads `description_json` — a key the file does not have — so every seeded page stores an empty document and opens blank in the editor until somebody edits it. And its `logo_props` is never read at all, so the emoji beside the page's name is lost. Both reproduced.
func (tasks *WorkspaceSeedTasks) seedPages(ctx context.Context, workspaceID string, projects map[int]string, botID string) error {
	var seeds []seedPage
	if err := json.Unmarshal(pageSeedJSON, &seeds); err != nil {
		return err
	}
	now := tasks.clock().UTC()
	for _, seed := range seeds {
		pageID := uuid.NewString()
		err := tasks.db.WithContext(ctx).Table("pages").Create(map[string]any{
			"id": pageID, "created_at": now, "updated_at": now,
			"created_by_id": botID, "updated_by_id": botID,
			"workspace_id": workspaceID, "owned_by_id": botID,
			"name": seed.Name, "access": seed.Access, "color": "",
			"description_html": seed.DescriptionHTML, "description_json": "{}",
			// Page.save recomputes this from the html, so the file's own copy is not used.
			"description_stripped": strippedHTML(seed.DescriptionHTML),
			"is_global":            false, "is_locked": false,
			"view_props": "{}", "logo_props": "{}", "sort_order": 65535,
		}).Error
		if err != nil {
			return err
		}
		if seed.ProjectID == 0 || seed.Type != "PROJECT" {
			continue
		}
		err = tasks.db.WithContext(ctx).Table("project_pages").Create(map[string]any{
			"id": uuid.NewString(), "created_at": now, "updated_at": now,
			"created_by_id": botID, "updated_by_id": botID,
			"workspace_id": workspaceID, "project_id": projects[seed.ProjectID], "page_id": pageID,
		}).Error
		if err != nil {
			return err
		}
	}
	return nil
}
