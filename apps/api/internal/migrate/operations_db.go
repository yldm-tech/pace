package migrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// The ported RunPython operations of the db app.
//
// Each one is registered under the migration it belongs to and the name of the Python function it was ported from, and each carries that function beside it, because what these do is not recoverable from the SQL alone. They are checked against the Python by running both: apps/api/tools/check_migration_operations.py migrates two databases to the state before the migration, seeds them the same, lets Django apply it to one and this to the other, and diffs every table.
func init() {
	register("db", "0035_auto_20230704_2225", "update_company_organization_size", updateCompanyOrganizationSize)
	register("db", "0037_issue_archived_at_project_archive_in_and_more", "onboarding_default_steps", onboardingDefaultSteps)
	register("db", "0038_auto_20230720_1505", "restructure_theming", restructureTheming)
	register("db", "0039_auto_20230723_2203", "rename_field", renameAssigneeActivityField)
	register("db", "0039_auto_20230723_2203", "update_workspace_member_props", updateWorkspaceMemberProps)
	register("db", "0039_auto_20230723_2203", "update_project_member_sort_order", updateProjectMemberSortOrder)

	const migration0041 = "0041_cycle_sort_order_issuecomment_access_and_more"
	register("db", migration0041, "generate_display_name", generateDisplayName)
	register("db", migration0041, "rectify_field_issue_activity", rectifyFieldIssueActivity)
	register("db", migration0041, "update_assignee_issue_activity", updateAssigneeIssueActivity)
	register("db", migration0041, "update_name_activity", updateNameActivity)
	register("db", migration0041, "random_cycle_order", randomCycleOrder)
	register("db", migration0041, "random_module_order", randomModuleOrder)
	register("db", migration0041, "update_user_issue_properties", updateUserIssueProperties)
	register("db", migration0041, "workspace_member_properties", workspaceMemberProperties)

	register("db", "0042_alter_analyticview_created_by_and_more", "update_user_timezones", updateUserTimezones)

	const migration0043 = "0043_alter_analyticview_created_by_and_more"
	register("db", migration0043, "create_issue_relation", createIssueRelation)
	register("db", migration0043, "update_issue_priority_choice", updateIssuePriorityChoice)

	const migration0044 = "0044_auto_20230913_0709"
	register("db", migration0044, "update_workspace_member_view_props", updateWorkspaceMemberViewProps)
	register("db", migration0044, "update_project_member_view_props", updateProjectMemberViewProps)
	register("db", migration0044, "update_cycle_props", updateCycleProps)
	register("db", migration0044, "update_module_props", updateModuleProps)

	const migration0045 = "0045_issueactivity_epoch_workspacemember_issue_props_and_more"
	register("db", migration0045, "update_issue_activity_priority", updateIssueActivityPriority)
	register("db", migration0045, "update_issue_activity_blocked", updateIssueActivityBlocked)

	register("db", "0046_label_sort_order_alter_analyticview_created_by_and_more", "random_sort_ordering", randomSortOrdering)
	const migration0053 = "0053_auto_20240102_1315"
	register("db", migration0053, "workspace_user_properties", workspaceUserProperties)
	register("db", migration0053, "project_user_properties", projectUserProperties)
	register("db", migration0053, "issue_view", issueView)

	const migration0055 = "0055_auto_20240108_0648"
	register("db", migration0055, "create_widgets", createWidgets)
	register("db", migration0055, "create_dashboards", createDashboards)
	register("db", migration0055, "create_dashboard_widgets", createDashboardWidgets)

	register("db", "0057_auto_20240122_0901", "create_notification_preferences", createNotificationPreferences)
	register("db", "0059_auto_20240208_0957", "widgets_filter_change", widgetsFilterChange)

	register("db", "0061_project_logo_props", "update_project_logo_props", updateProjectLogoProps)
	register("db", "0063_state_is_triage_alter_state_group", "update_project_state_group", updateProjectStateGroup)

	const migration0065 = "0065_auto_20240415_0937"
	register("db", migration0065, "migrate_user_profile", migrateUserProfile)
	register("db", migration0065, "user_favorite_migration", userFavoriteMigration)

	register("db", "0069_alter_account_provider_and_more", "populate_views_owned_by", populateViewsOwnedBy)
	register("db", "0076_alter_projectmember_role_and_more", "update_workspace_project_member_role", updateWorkspaceProjectMemberRole)
	register("db", "0094_auto_20250425_0902", "set_default_source_type", setDefaultSourceType)
	register("db", "0113_webhook_version", "populate_product_tour", populateProductTour)

	const migration0067 = "0067_issue_estimate"
	register("db", migration0067, "issue_estimate_point", issueEstimatePoint)
	register("db", migration0067, "last_used_estimate", lastUsedEstimate)
	register("db", migration0067, "populate_deploy_board", populateDeployBoard)

	register("db", "0068_remove_pagelabel_project_remove_pagelog_project_and_more", "migrate_pages", migratePagesToProjectPages)

	const migration0079 = "0079_auto_20241009_0619"
	register("db", migration0079, "move_attachment_to_fileasset", moveAttachmentToFileAsset)
	register("db", migration0079, "mark_existing_file_uploads", markExistingFileUploads)

	register("db", "0106_auto_20250912_0845", "set_page_sort_order", setPageSortOrder)

	register("db", "0077_draftissue_cycle_user_timezone_project_user_timezone_and_more", "migrate_draft_issues", migrateDraftIssues)
	register("db", "0112_auto_20251124_0603", "create_triage_state", createTriageState)

	const migration0115 = "0115_auto_20260105_1406"
	register("db", migration0115, "move_issue_user_properties_to_project_user_properties", moveIssueUserPropertiesToProjectUserProperties)
	register("db", migration0115, "migrate_existing_api_tokens", migrateExistingApiTokens)

	register("db", "0118_remove_workspaceuserproperties_product_tour_and_more", "migrate_all_the_product_tour_to_true", migrateAllTheProductTourToTrue)

	register("db", "0107_migrate_filters_to_rich_filters", "migrate_filters_to_rich_filters", migrateFiltersToRichFilters)

	register("db", "0049_auto_20231116_0713", "update_pages", updatePages)
	register("db", "0050_user_use_case_alter_workspace_organization_size", "user_password_autoset", userPasswordAutoset)
}

// updateCompanyOrganizationSize copies the old integer company size into the new text one.
//
//	def update_company_organization_size(apps, schema_editor):
//	    Model = apps.get_model("db", "Workspace")
//	    updated_size = []
//	    for obj in Model.objects.all():
//	        obj.organization_size = str(obj.company_size)
//	        updated_size.append(obj)
//	    Model.objects.bulk_update(updated_size, ["organization_size"], batch_size=100)
//
// Every workspace, not a subset, and company_size is an integer column that has been NOT NULL since db.0002 — so there is no row this skips and no null to think about. Python's str() of an int and Postgres' cast of an integer to text are the same decimal text.
func updateCompanyOrganizationSize(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE workspaces SET organization_size = company_size::text`)
	return err
}

// onboardingDefaultSteps marks everyone who had already finished onboarding as having finished each of its steps.
//
//	def onboarding_default_steps(apps, schema_editor):
//	    default_onboarding_schema = {
//	        "workspace_join": True, "profile_complete": True,
//	        "workspace_create": True, "workspace_invite": True,
//	    }
//	    Model = apps.get_model("db", "User")
//	    updated_user = []
//	    for obj in Model.objects.filter(is_onboarded=True):
//	        obj.onboarding_step = default_onboarding_schema
//	        obj.is_tour_completed = True
//	        updated_user.append(obj)
//	    Model.objects.bulk_update(updated_user, ["onboarding_step", "is_tour_completed"], batch_size=100)
//
// Only the onboarded, and the same object for every one of them. The column is jsonb, so the key order written here does not survive into the database on either side.
func onboardingDefaultSteps(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE users
		   SET onboarding_step = '{"workspace_join": true, "profile_complete": true, "workspace_create": true, "workspace_invite": true}'::jsonb,
		       is_tour_completed = true
		 WHERE is_onboarded = true`)
	return err
}

// restructureTheming rewrites the old theme object into the one the editor reads.
//
//	def restructure_theming(apps, schema_editor):
//	    Model = apps.get_model("db", "User")
//	    updated_user = []
//	    for obj in Model.objects.exclude(theme={}).all():
//	        current_theme = obj.theme
//	        updated_theme = {
//	            "primary": current_theme.get("accent", ""),
//	            "background": current_theme.get("bgBase", ""),
//	            "sidebarBackground": current_theme.get("sidebar", ""),
//	            "text": current_theme.get("textBase", ""),
//	            "sidebarText": current_theme.get("textBase", ""),
//	            "palette": f"""{current_theme.get("bgBase","")},{current_theme.get("textBase", "")},{current_theme.get("accent", "")},{current_theme.get("sidebar","")},{current_theme.get("textBase", "")}""",
//	            "darkPalette": current_theme.get("darkPalette", ""),
//	        }
//	        obj.theme = updated_theme
//	        updated_user.append(obj)
//	    Model.objects.bulk_update(updated_user, ["theme"], batch_size=100)
//
// Two things decide what this has to be. The rows are those whose theme is not the empty object — and theme is NOT NULL with a default of {}, so there is no null case to argue about. And a missing key becomes the empty string rather than a JSON null, which is what .get's second argument says and what -> alone would not give.
//
// The six copied values keep whatever JSON type they had, which is why they are read with -> rather than ->>. The palette is different: it is built by an f-string, so each piece is Python's str() of the value and the result is always a string. For the string values that every real theme holds, ->> is that same text.
func restructureTheming(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE users
		   SET theme = jsonb_build_object(
		         'primary',           COALESCE(theme -> 'accent',      '""'::jsonb),
		         'background',        COALESCE(theme -> 'bgBase',      '""'::jsonb),
		         'sidebarBackground', COALESCE(theme -> 'sidebar',     '""'::jsonb),
		         'text',              COALESCE(theme -> 'textBase',    '""'::jsonb),
		         'sidebarText',       COALESCE(theme -> 'textBase',    '""'::jsonb),
		         'palette', concat_ws(',',
		             COALESCE(theme ->> 'bgBase', ''),
		             COALESCE(theme ->> 'textBase', ''),
		             COALESCE(theme ->> 'accent', ''),
		             COALESCE(theme ->> 'sidebar', ''),
		             COALESCE(theme ->> 'textBase', '')),
		         'darkPalette',       COALESCE(theme -> 'darkPalette', '""'::jsonb))
		 WHERE theme <> '{}'::jsonb`)
	return err
}

// renameAssigneeActivityField renames the activity field, which went plural when an issue could have more than one assignee.
//
//	def rename_field(apps, schema_editor):
//	    Model = apps.get_model("db", "IssueActivity")
//	    updated_activity = []
//	    for obj in Model.objects.filter(field="assignee"):
//	        obj.field = "assignees"
//	        updated_activity.append(obj)
//	    Model.objects.bulk_update(updated_activity, ["field"], batch_size=100)
func renameAssigneeActivityField(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE issue_activities SET field = 'assignees' WHERE field = 'assignee'`)
	return err
}

// updateWorkspaceMemberProps moves whatever was in view_props under a properties key, and gives a member who had none the defaults.
//
//	def update_workspace_member_props(apps, schema_editor):
//	    Model = apps.get_model("db", "WorkspaceMember")
//	    updated_workspace_member = []
//	    for obj in Model.objects.all():
//	        if obj.view_props is None:
//	            obj.view_props = { ...the defaults, with a full properties object... }
//	        else:
//	            current_view_props = obj.view_props
//	            obj.view_props = {
//	                "filters": {"type": None}, "groupByProperty": None, "issueView": "list",
//	                "orderBy": "-created_at", "showEmptyGroups": True,
//	                "properties": current_view_props,
//	            }
//	        updated_workspace_member.append(obj)
//	    Model.objects.bulk_update(updated_workspace_member, ["view_props"], batch_size=100)
//
// `is None` catches two different nulls, and reading the Python suggests it catches one. A jsonb column holding the JSON value null comes back from psycopg as Python None, exactly as a SQL NULL does, so the ORM cannot tell them apart and both take the first branch. Written as `view_props IS NULL` alone, a member whose view_props was the JSON null came out with "properties": null where Django gives it the full defaults — which is what the differential check reported and what reading the code had got backwards.
//
// It is one statement for a reason. Written as two passes — fill the nulls, then wrap the rest — the second pass rewrites rows the first had just written, and Postgres then refuses the ALTER TABLE that follows this operation with "cannot ALTER TABLE because it has pending trigger events". Django's bulk_update is a single UPDATE and does not provoke it, and one CASE is both the closer reproduction and the one that works.
func updateWorkspaceMemberProps(ctx context.Context, tx *sql.Tx) error {
	const defaults = `{
		"filters": {"type": null},
		"groupByProperty": null,
		"issueView": "list",
		"orderBy": "-created_at",
		"properties": {
			"assignee": true, "due_date": true, "key": true, "labels": true,
			"priority": true, "state": true, "sub_issue_count": true,
			"attachment_count": true, "link": true, "estimate": true,
			"created_on": true, "updated_on": true
		},
		"showEmptyGroups": true
	}`
	_, err := tx.ExecContext(ctx, `
		UPDATE workspace_members
		   SET view_props = CASE
		         WHEN view_props IS NULL OR view_props = 'null'::jsonb THEN $1::jsonb
		         ELSE jsonb_build_object(
		           'filters', jsonb_build_object('type', 'null'::jsonb),
		           'groupByProperty', 'null'::jsonb,
		           'issueView', 'list',
		           'orderBy', '-created_at',
		           'showEmptyGroups', true,
		           'properties', view_props)
		       END`, defaults)
	return err
}

// updateProjectMemberSortOrder scatters the members into an arbitrary order.
//
//	def update_project_member_sort_order(apps, schema_editor):
//	    Model = apps.get_model("db", "ProjectMember")
//	    updated_project_members = []
//	    for obj in Model.objects.all():
//	        obj.sort_order = random.randint(1, 65536)
//	        updated_project_members.append(obj)
//	    Model.objects.bulk_update(updated_project_members, ["sort_order"], batch_size=100)
//
// random.randint takes both ends, so the value is 1 through 65536 inclusive; floor(random() * 65536) + 1 is the same range. Two runs of the Python disagree with each other, so the differential check compares everything except this column and asserts only that every row got one.
func updateProjectMemberSortOrder(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE project_members SET sort_order = floor(random() * 65536) + 1`)
	return err
}

// lastWordReplaced builds the expression that rewrites a comment the way Python's str.split() and " ".join() do: whitespace runs collapse to one space, leading and trailing whitespace goes, and the final word becomes the replacement.
//
// An empty comment has no final word, and Python raises IndexError rather than doing anything sensible with it. That is not reproduced — there is nothing to reproduce, since the migration would stop there — and an empty comment here becomes the replacement on its own. Every activity row carries a sentence, so the case does not arise.
func lastWordReplaced(comment, replacement string) string {
	// The parentheses around the call are needed before the subscript: Postgres will not slice a bare function result.
	words := fmt.Sprintf("(regexp_split_to_array(btrim(%s), '\\s+'))", comment)
	return fmt.Sprintf("array_to_string(%[1]s[1 : array_length(%[1]s, 1) - 1] || %[2]s, ' ')", words, replacement)
}

// generateDisplayName gives everyone a display name taken from their email.
//
//	def generate_display_name(apps, schema_editor):
//	    UserModel = apps.get_model("db", "User")
//	    updated_users = []
//	    for obj in UserModel.objects.all():
//	        obj.display_name = (
//	            obj.email.split("@")[0]
//	            if len(obj.email.split("@"))
//	            else "".join(random.choice(string.ascii_letters) for _ in range(6))
//	        )
//	        updated_users.append(obj)
//	    UserModel.objects.bulk_update(updated_users, ["display_name"], batch_size=100)
//
// The conditional never takes its second branch. str.split always returns at least one element — "".split("@") is [""], not [] — so len() of it is never zero and the six random letters are unreachable. What this does is take everything before the first "@", and for an address without one, the whole string.
//
// email is nullable, and a null one would have raised AttributeError on .split rather than reaching either branch. split_part leaves it null instead. Nothing here turns a migration that would have stopped into one that quietly did the wrong thing: it stops on a row upstream would have stopped on only in the sense that upstream stopped there.
func generateDisplayName(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE users SET display_name = split_part(email, '@', 1)`)
	return err
}

// rectifyFieldIssueActivity is db.0039's rename_field again, run a second time because db.0039 shipped before some of the rows it was meant to catch existed.
//
//	def rectify_field_issue_activity(apps, schema_editor):
//	    Model = apps.get_model("db", "IssueActivity")
//	    updated_activity = []
//	    for obj in Model.objects.filter(field="assignee"):
//	        obj.field = "assignees"
//	        updated_activity.append(obj)
//	    Model.objects.bulk_update(updated_activity, ["field"], batch_size=100)
func rectifyFieldIssueActivity(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE issue_activities SET field = 'assignees' WHERE field = 'assignee'`)
	return err
}

// updateAssigneeIssueActivity turns the email an assignment activity recorded into the display name and id of the person it names.
//
//	def update_assignee_issue_activity(apps, schema_editor):
//	    Model = apps.get_model("db", "IssueActivity")
//	    updated_activity = []
//	    User = apps.get_model("db", "User")
//	    users = User.objects.values("id", "email", "display_name")
//	    for obj in Model.objects.filter(field="assignees"):
//	        if bool(obj.new_value) and not bool(obj.old_value):
//	            assigned_user = [user for user in users if user.get("email") == obj.new_value]
//	            if assigned_user:
//	                obj.new_value = assigned_user[0].get("display_name")
//	                obj.new_identifier = assigned_user[0].get("id")
//	                words = obj.comment.split()
//	                words[-1] = assigned_user[0].get("display_name")
//	                obj.comment = " ".join(words)
//	        if bool(obj.old_value) and not bool(obj.new_value):
//	            ...the same, with old_value and old_identifier...
//	        updated_activity.append(obj)
//	    Model.objects.bulk_update(updated_activity, [...], batch_size=200)
//
// The two branches cannot both fire for one row: each wants one side truthy and the other not. bool() of a CharField is false for the empty string as well as for null, which is why both are tested here rather than just the null.
//
// "the first match" is a single row: email carries a unique constraint, so the list comprehension can only ever find one.
//
// This runs after rectify_field_issue_activity and after generate_display_name, both earlier in the same migration, so it sees the rows they renamed and the names they wrote. The recorded file keeps that order.
func updateAssigneeIssueActivity(ctx context.Context, tx *sql.Tx) error {
	for _, side := range [][2]string{{"new", "old"}, {"old", "new"}} {
		named, other := side[0], side[1]
		statement := fmt.Sprintf(`
			UPDATE issue_activities a
			   SET %[1]s_value = u.display_name,
			       %[1]s_identifier = u.id,
			       comment = %[3]s
			  FROM users u
			 WHERE a.field = 'assignees'
			   AND a.%[1]s_value = u.email
			   AND COALESCE(a.%[1]s_value, '') <> ''
			   AND COALESCE(a.%[2]s_value, '') = ''`,
			named, other, lastWordReplaced("a.comment", "u.display_name"))
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// updateNameActivity fixes a sentence that called the name field a start date.
//
//	def update_name_activity(apps, schema_editor):
//	    Model = apps.get_model("db", "IssueActivity")
//	    update_activity = []
//	    for obj in Model.objects.filter(field="name"):
//	        obj.comment = obj.comment.replace("start date", "name")
//	        update_activity.append(obj)
//	    Model.objects.bulk_update(update_activity, ["comment"], batch_size=1000)
//
// str.replace takes every occurrence, and so does Postgres' replace.
func updateNameActivity(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE issue_activities SET comment = replace(comment, 'start date', 'name') WHERE field = 'name'`)
	return err
}

// randomCycleOrder and randomModuleOrder scatter cycles and modules into an arbitrary order, the way update_project_member_sort_order does for members.
//
//	for obj in CycleModel.objects.all():
//	    obj.sort_order = random.randint(1, 65536)
func randomCycleOrder(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE cycles SET sort_order = floor(random() * 65536) + 1`)
	return err
}

func randomModuleOrder(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE modules SET sort_order = floor(random() * 65536) + 1`)
	return err
}

// updateUserIssueProperties adds one key to every issue property object.
//
//	def update_user_issue_properties(apps, schema_editor):
//	    IssuePropertyModel = apps.get_model("db", "IssueProperty")
//	    updated_issue_properties = []
//	    for obj in IssuePropertyModel.objects.all():
//	        obj.properties["start_date"] = True
//	        updated_issue_properties.append(obj)
//	    IssuePropertyModel.objects.bulk_update(updated_issue_properties, ["properties"], batch_size=100)
func updateUserIssueProperties(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE issue_properties SET properties = jsonb_set(properties, '{start_date}', 'true')`)
	return err
}

// workspaceMemberProperties adds the same key to both of a member's property objects, one level down.
//
//	def workspace_member_properties(apps, schema_editor):
//	    WorkspaceMemberModel = apps.get_model("db", "WorkspaceMember")
//	    updated_workspace_members = []
//	    for obj in WorkspaceMemberModel.objects.all():
//	        obj.view_props["properties"]["start_date"] = True
//	        obj.default_props["properties"]["start_date"] = True
//	        updated_workspace_members.append(obj)
//	    WorkspaceMemberModel.objects.bulk_update(updated_workspace_members, ["view_props", "default_props"], batch_size=100)
//
// Python subscripts twice, so a row whose view_props has no "properties" raises KeyError and the migration stops. jsonb_set with a two-element path leaves such a row alone instead. db.0039 gave every member a "properties" key a few migrations earlier, so the case does not arise.
func workspaceMemberProperties(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE workspace_members
		   SET view_props = jsonb_set(view_props, '{properties,start_date}', 'true'),
		       default_props = jsonb_set(default_props, '{properties,start_date}', 'true')`)
	return err
}

// updateUserTimezones puts everyone on UTC.
//
//	def update_user_timezones(apps, schema_editor):
//	    for obj in UserModel.objects.all():
//	        obj.user_timezone = "UTC"
func updateUserTimezones(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE users SET user_timezone = 'UTC'`)
	return err
}

// createIssueRelation turns the old one-purpose blocker table into a row of the general relation table.
//
//	def create_issue_relation(apps, schema_editor):
//	    try:
//	        IssueBlockerModel = apps.get_model("db", "IssueBlocker")
//	        IssueRelation = apps.get_model("db", "IssueRelation")
//	        updated_issue_relation = []
//	        for blocked_issue in IssueBlockerModel.objects.all():
//	            updated_issue_relation.append(IssueRelation(
//	                issue_id=blocked_issue.block_id,
//	                related_issue_id=blocked_issue.blocked_by_id,
//	                relation_type="blocked_by",
//	                project_id=blocked_issue.project_id,
//	                workspace_id=blocked_issue.workspace_id,
//	                created_by_id=blocked_issue.created_by_id,
//	                updated_by_id=blocked_issue.updated_by_id,
//	            ))
//	        IssueRelation.objects.bulk_create(updated_issue_relation, batch_size=100)
//	    except Exception as e:
//	        print(e)
//
// The try is the interesting part. Whatever goes wrong here — a duplicate pair against the relation table's unique constraint is the obvious one — is printed and swallowed, and the migration carries on as though nothing had been asked of it. Reproducing that needs a savepoint: an error inside a Postgres transaction poisons the whole thing, so without one a failure here would take the migration down, which is the opposite of what upstream does.
//
// The rows are created through the model rather than from the blocker's own columns, so each gets a fresh uuid and the current time. Two runs disagree about those three columns and the differential check is told so.
func createIssueRelation(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT create_issue_relation`); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO issue_relations
		       (id, created_at, updated_at, issue_id, related_issue_id, relation_type,
		        project_id, workspace_id, created_by_id, updated_by_id)
		SELECT gen_random_uuid(), now(), now(), b.block_id, b.blocked_by_id, 'blocked_by',
		       b.project_id, b.workspace_id, b.created_by_id, b.updated_by_id
		  FROM issue_blockers b`)
	if err != nil {
		// Swallowed, as upstream swallows it, and the savepoint is what makes carrying on possible at all.
		if _, rollback := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT create_issue_relation`); rollback != nil {
			return rollback
		}
		return nil
	}
	_, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT create_issue_relation`)
	return err
}

// updateIssuePriorityChoice gives the issues with no priority the one that means none.
//
//	def update_issue_priority_choice(apps, schema_editor):
//	    for obj in IssueModel.objects.filter(priority=None):
//	        obj.priority = "none"
//
// filter(priority=None) is Django's spelling of IS NULL.
func updateIssuePriorityChoice(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE issues SET priority = 'none' WHERE priority IS NULL`)
	return err
}

// jsonGet renders Python's dict.get(key, default) for a jsonb column.
//
// The distinction that matters is between a key that is absent and a key whose value is JSON null. Python's get returns the stored value when the key is there, null included, and the default only when it is not. Postgres' -> gives SQL NULL for an absent key and 'null'::jsonb for a stored null, so a COALESCE over -> draws exactly that line.
func jsonGet(object, key, fallback string) string {
	return fmt.Sprintf("COALESCE(%s -> '%s', %s::jsonb)", object, key, fallback)
}

// memberFilters is the filters object db.0044's three helpers all build, identically.
func memberFilters(props string) string {
	var pairs []string
	for _, key := range []string{"priority", "state", "state_group", "assignees", "created_by", "labels", "start_date", "target_date", "subscriber"} {
		pairs = append(pairs, fmt.Sprintf("'%s', %s", key, jsonGet(props+" -> 'filters'", key, "'null'")))
	}
	return "jsonb_build_object(" + strings.Join(pairs, ", ") + ")"
}

// memberDisplayFilters is the display_filters object the workspace and project helpers share.
func memberDisplayFilters(props string) string {
	return "jsonb_build_object(" + strings.Join([]string{
		"'group_by', " + jsonGet(props, "groupByProperty", "'null'"),
		"'order_by', " + jsonGet(props, "orderBy", `'"-created_at"'`),
		"'type', " + jsonGet(props+" -> 'filters'", "type", "'null'"),
		"'sub_issue', " + jsonGet(props, "showSubIssues", "'true'"),
		"'show_empty_groups', " + jsonGet(props, "showEmptyGroups", "'true'"),
		"'layout', " + jsonGet(props, "issueView", `'"list"'`),
		"'calendar_date_range', " + jsonGet(props, "calendarDateRange", `'""'`),
	}, ", ") + ")"
}

// workspaceMemberPropsExpression is db.0044's workspace_member_props: filters, display_filters and display_properties, every value read out of the old object with a default.
func workspaceMemberPropsExpression(props string) string {
	var display []string
	for _, key := range []string{"assignee", "attachment_count", "created_on", "due_date", "estimate", "key", "labels", "link", "priority", "start_date", "state", "sub_issue_count", "updated_on"} {
		display = append(display, fmt.Sprintf("'%s', %s", key, jsonGet(props+" -> 'properties'", key, "'true'")))
	}
	return fmt.Sprintf("jsonb_build_object('filters', %s, 'display_filters', %s, 'display_properties', jsonb_build_object(%s))",
		memberFilters(props), memberDisplayFilters(props), strings.Join(display, ", "))
}

// projectMemberPropsExpression is db.0044's project_member_props, which is the workspace one without display_properties.
func projectMemberPropsExpression(props string) string {
	return fmt.Sprintf("jsonb_build_object('filters', %s, 'display_filters', %s)",
		memberFilters(props), memberDisplayFilters(props))
}

// cycleModulePropsExpression is db.0044's cycle_module_props, which keeps only filters.
func cycleModulePropsExpression(props string) string {
	return fmt.Sprintf("jsonb_build_object('filters', %s)", memberFilters(props))
}

// updateWorkspaceMemberViewProps and updateProjectMemberViewProps rewrite both property objects into the shape the filters rewrite introduced.
//
//	def update_workspace_member_view_props(apps, schema_editor):
//	    for obj in WorkspaceMemberModel.objects.all():
//	        obj.view_props = workspace_member_props(obj.view_props)
//	        obj.default_props = workspace_member_props(obj.default_props)
func updateWorkspaceMemberViewProps(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, fmt.Sprintf(
		`UPDATE workspace_members SET view_props = %s, default_props = %s`,
		workspaceMemberPropsExpression("view_props"), workspaceMemberPropsExpression("default_props")))
	return err
}

func updateProjectMemberViewProps(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, fmt.Sprintf(
		`UPDATE project_members SET view_props = %s, default_props = %s`,
		projectMemberPropsExpression("view_props"), projectMemberPropsExpression("default_props")))
	return err
}

// updateCycleProps and updateModuleProps rewrite a cycle's or module's view_props — but only for the rows that pass a test that almost nothing passes.
//
//	def update_cycle_props(apps, schema_editor):
//	    for obj in CycleModel.objects.all():
//	        if "filter" in obj.view_props:
//	            obj.view_props = cycle_module_props(obj.view_props)
//	            updated_cycle.append(obj)
//
// The key tested is "filter". The key the rewrite then reads, and the one every other props object in this codebase uses, is "filters". Nothing writes a "filter", so in practice this rewrites nothing at all. Reproduced rather than corrected: a cycle whose view_props happens to carry a "filter" key gets rewritten here exactly as upstream rewrites it, and every other cycle is left alone exactly as upstream leaves it.
func updateCycleProps(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, fmt.Sprintf(
		`UPDATE cycles SET view_props = %s WHERE view_props ? 'filter'`, cycleModulePropsExpression("view_props")))
	return err
}

func updateModuleProps(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, fmt.Sprintf(
		`UPDATE modules SET view_props = %s WHERE view_props ? 'filter'`, cycleModulePropsExpression("view_props")))
	return err
}

// updateIssueActivityPriority gives a priority activity the word for having none where it recorded nothing.
//
//	def update_issue_activity_priority(apps, schema_editor):
//	    for obj in IssueActivity.objects.filter(field="priority"):
//	        obj.new_value = obj.new_value or "none"
//	        obj.old_value = obj.old_value or "none"
//
// `or` falls through on any falsy value, which for these columns means the empty string as well as null.
func updateIssueActivityPriority(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE issue_activities
		   SET new_value = CASE WHEN COALESCE(new_value, '') = '' THEN 'none' ELSE new_value END,
		       old_value = CASE WHEN COALESCE(old_value, '') = '' THEN 'none' ELSE old_value END
		 WHERE field = 'priority'`)
	return err
}

// updateIssueActivityBlocked renames the activity field to the direction the relation is now stored in.
//
//	def update_issue_activity_blocked(apps, schema_editor):
//	    for obj in IssueActivity.objects.filter(field="blocks"):
//	        obj.field = "blocked_by"
func updateIssueActivityBlocked(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE issue_activities SET field = 'blocked_by' WHERE field = 'blocks'`)
	return err
}

// randomSortOrdering scatters the labels.
//
//	def random_sort_ordering(apps, schema_editor):
//	    for label in Label.objects.all():
//	        label.sort_order = random.randint(0, 65535)
//
// Note the range. The other four scatterings in these migrations use randint(1, 65536); this one starts at zero and stops one lower, and floor(random() * 65536) is that range rather than the other.
func randomSortOrdering(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE labels SET sort_order = floor(random() * 65536)`)
	return err
}

// userPasswordAutoset marks every password as one the system chose.
//
//	def user_password_autoset(apps, schema_editor):
//	    User = apps.get_model("db", "User")
//	    User.objects.update(is_password_autoset=True)
//
// A queryset update rather than a bulk_update, and with no filter, so it is every row.
func userPasswordAutoset(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE users SET is_password_autoset = true`)
	return err
}

// updatePages folds a page's blocks into the page's own HTML, and records an embed for each block that was an issue.
//
//	def update_pages(apps, schema_editor):
//	    try:
//	        for page in Page.objects.all():
//	            page_blocks = PageBlock.objects.filter(
//	                page_id=page.id, project_id=page.project_id, workspace_id=page.workspace_id,
//	            ).order_by("sort_order")
//	            if page_blocks:
//	                for page_block in page_blocks:
//	                    if page_block.issue is not None:
//	                        project_identifier = page.project.identifier
//	                        sequence_id = page_block.issue.sequence_id
//	                        transaction = uuid.uuid4().hex
//	                        embed_component = f'<issue-embed-component id="{transaction}" entity_name="issue" entity_identifier="{page_block.issue_id}" sequence_id="{sequence_id}" project_identifier="{project_identifier}" title="{page_block.name}"></issue-embed-component>'
//	                        page.description_html += embed_component
//	                        page_logs.append(PageLog(
//	                            page_id=page_block.page_id, transaction=transaction,
//	                            entity_identifier=page_block.issue_id, entity_name="issue",
//	                            project_id=page.project_id, workspace_id=page.workspace_id,
//	                            created_by_id=page_block.created_by_id, updated_by_id=page_block.updated_by_id,
//	                        ))
//	                    else:
//	                        page.description_html += f"<h2>{page_block.name}</h2>"
//	                        page.description_html += page_block.description_html
//	                updated_pages.append(page)
//	        Page.objects.bulk_update(updated_pages, ["description_html"], batch_size=100)
//	        PageLog.objects.bulk_create(page_logs, batch_size=100)
//	    except Exception as e:
//	        print(e)
//
// The one transaction id is written in two places — into the embed's id attribute and into the log row — so it has to be generated once per block and used twice. That is why the blocks are collected in a CTE rather than computed twice: referencing it from both the aggregate and the insert materialises it, and a volatile gen_random_uuid() inside would otherwise be evaluated again for each.
//
// It is also written in two forms. Python hands uuid4().hex to the f-string, which is thirty-two characters with no dashes, and hands the same string to a UUIDField, which parses it and stores the canonical dashed form. So the HTML carries the undashed one and the column the dashed one.
//
// The blocks of a page are appended in sort_order, and a page with no blocks is not touched at all — not even to rewrite its description_html with itself. Both fall out of the join.
//
// The try wraps everything, so a failure anywhere leaves the migration to carry on as though nothing had been asked of it. The savepoint is what makes that possible in Postgres.
func updatePages(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT update_pages`); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		WITH blocks AS MATERIALIZED (
		    SELECT b.page_id, b.issue_id, b.name, b.description_html, b.sort_order,
		           b.created_by_id, b.updated_by_id,
		           p.project_id, p.workspace_id, pr.identifier, i.sequence_id,
		           gen_random_uuid() AS transaction
		      FROM page_blocks b
		      JOIN pages p ON p.id = b.page_id AND p.project_id = b.project_id AND p.workspace_id = b.workspace_id
		      JOIN projects pr ON pr.id = p.project_id
		      LEFT JOIN issues i ON i.id = b.issue_id
		),
		appended AS (
		    SELECT page_id, string_agg(
		             CASE WHEN issue_id IS NOT NULL THEN
		               '<issue-embed-component id="' || replace(transaction::text, '-', '') ||
		               '" entity_name="issue" entity_identifier="' || issue_id ||
		               '" sequence_id="' || sequence_id ||
		               '" project_identifier="' || identifier ||
		               '" title="' || name || '"></issue-embed-component>'
		             ELSE
		               '<h2>' || name || '</h2>' || description_html
		             END, '' ORDER BY sort_order) AS html
		      FROM blocks
		     GROUP BY page_id
		),
		logs AS (
		    INSERT INTO page_logs (id, created_at, updated_at, page_id, transaction, entity_identifier,
		                           entity_name, project_id, workspace_id, created_by_id, updated_by_id)
		    SELECT gen_random_uuid(), now(), now(), page_id, transaction, issue_id,
		           'issue', project_id, workspace_id, created_by_id, updated_by_id
		      FROM blocks
		     WHERE issue_id IS NOT NULL
		    RETURNING 1
		)
		UPDATE pages
		   SET description_html = pages.description_html || appended.html
		  FROM appended
		 WHERE pages.id = appended.page_id`)
	if err != nil {
		if _, rollback := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT update_pages`); rollback != nil {
			return rollback
		}
		return nil
	}
	_, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT update_pages`)
	return err
}

// jsonGetOrNull renders Python's dict.get(key) with no default, for a value that is about to be written into a column.
//
// Both ways of not having a value come back from the ORM as None: a key that is absent, and a key whose stored value is JSON null, which psycopg turns into None on the way out. Writing None into a NOT NULL column is an IntegrityError, and that is reproduced rather than dodged — a row the Python would have refused is refused here too.
func jsonGetOrNull(object, key string) string {
	return fmt.Sprintf("NULLIF(%s -> '%s', 'null'::jsonb)", object, key)
}

// jsonGetOrDefault renders Python's dict.get(key, default) for the same setting.
//
// The difference from jsonGetOrNull is what happens when the key is there holding null: get returns that null rather than the default, so the column gets a SQL NULL and not the fallback. Only an absent key takes the default, which is why the key is tested for presence rather than the value for nullness.
func jsonGetOrDefault(object, key, fallback string) string {
	return fmt.Sprintf("CASE WHEN %[1]s ? '%[2]s' THEN %[3]s ELSE %[4]s::jsonb END",
		object, key, jsonGetOrNull(object, key), fallback)
}

// workspaceUserProperties gives every workspace member a properties row of their own, carved out of the props object they had been carrying.
//
//	def workspace_user_properties(apps, schema_editor):
//	    for workspace_members in WorkspaceMember.objects.all():
//	        updated_workspace_user_properties.append(WorkspaceUserProperties(
//	            user_id=workspace_members.member_id,
//	            display_filters=workspace_members.view_props.get("display_filters"),
//	            display_properties=workspace_members.view_props.get("display_properties"),
//	            workspace_id=workspace_members.workspace_id,
//	        ))
//	    WorkspaceUserProperties.objects.bulk_create(updated_workspace_user_properties, batch_size=2000)
//
// filters is not passed, so it takes the model's default — the nine-key object with every value null. The other two are taken from view_props, which db.0044 gave both keys to a dozen migrations earlier.
func workspaceUserProperties(ctx context.Context, tx *sql.Tx) error {
	const filtersDefault = `{"priority": null, "state": null, "state_group": null, "assignees": null, "created_by": null, "labels": null, "start_date": null, "target_date": null, "subscriber": null}`
	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO workspace_user_properties
		       (id, created_at, updated_at, user_id, workspace_id, filters, display_filters, display_properties)
		SELECT gen_random_uuid(), now(), now(), m.member_id, m.workspace_id, $1::jsonb, %s, %s
		  FROM workspace_members m`,
		jsonGetOrNull("m.view_props", "display_filters"),
		jsonGetOrNull("m.view_props", "display_properties")), filtersDefault)
	return err
}

// projectUserProperties copies a project member's filters onto their issue properties row.
//
//	def project_user_properties(apps, schema_editor):
//	    for issue_property in IssueProperty.objects.all():
//	        project_member = ProjectMember.objects.filter(
//	            project_id=issue_property.project_id, member_id=issue_property.user_id,
//	        ).first()
//	        if project_member:
//	            issue_property.filters = project_member.view_props.get("filters")
//	            issue_property.display_filters = project_member.view_props.get("display_filters")
//	            updated_issue_user_properties.append(issue_property)
//
// An issue property row with no matching member is left alone rather than given nulls, which is what the `if project_member` is for. first() on an unordered queryset would be arbitrary if there were more than one, and there cannot be: a project member is unique per project and member.
func projectUserProperties(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		UPDATE issue_properties p
		   SET filters = %s,
		       display_filters = %s
		  FROM project_members m
		 WHERE m.project_id = p.project_id AND m.member_id = p.user_id`,
		jsonGetOrNull("m.view_props", "filters"),
		jsonGetOrNull("m.view_props", "display_filters")))
	return err
}

// issueView remakes each workspace-wide view as a row of the table that now holds both kinds.
//
//	def issue_view(apps, schema_editor):
//	    for global_view in GlobalView.objects.all():
//	        updated_issue_views.append(IssueView(
//	            workspace_id=global_view.workspace_id, name=global_view.name,
//	            description=global_view.description, query=global_view.query,
//	            access=global_view.access, filters=global_view.query_data.get("filters", {}),
//	            sort_order=global_view.sort_order,
//	            created_by_id=global_view.created_by_id, updated_by_id=global_view.updated_by_id,
//	        ))
//	    IssueView.objects.bulk_create(updated_issue_views, batch_size=100)
//
// display_filters and display_properties are not passed and take the model's defaults. project_id is not passed either, and is null: that is what makes the row a workspace view rather than a project one.
func issueView(ctx context.Context, tx *sql.Tx) error {
	const displayFilters = `{"group_by": null, "order_by": "-created_at", "type": null, "sub_issue": true, "show_empty_groups": true, "layout": "list", "calendar_date_range": ""}`
	const displayProperties = `{"assignee": true, "attachment_count": true, "created_on": true, "due_date": true, "estimate": true, "key": true, "labels": true, "link": true, "priority": true, "start_date": true, "state": true, "sub_issue_count": true, "updated_on": true}`
	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO issue_views
		       (id, created_at, updated_at, workspace_id, name, description, query, access,
		        filters, display_filters, display_properties, sort_order, created_by_id, updated_by_id)
		SELECT gen_random_uuid(), now(), now(), g.workspace_id, g.name, g.description, g.query, g.access,
		       %s, $1::jsonb, $2::jsonb, g.sort_order, g.created_by_id, g.updated_by_id
		  FROM global_views g`,
		jsonGetOrDefault("g.query_data", "filters", "'{}'")), displayFilters, displayProperties)
	return err
}

// createWidgets puts the eight dashboard widgets in the table, in the order the list gives them.
//
//	widgets_list = [
//	    {"key": "overview_stats", "filters": {}},
//	    {"key": "assigned_issues", "filters": {"duration": "this_week", "tab": "upcoming"}},
//	    {"key": "created_issues", "filters": {"duration": "this_week", "tab": "upcoming"}},
//	    {"key": "issues_by_state_groups", "filters": {"duration": "this_week"}},
//	    {"key": "issues_by_priority", "filters": {"duration": "this_week"}},
//	    {"key": "recent_activity", "filters": {}},
//	    {"key": "recent_projects", "filters": {}},
//	    {"key": "recent_collaborators", "filters": {}},
//	]
//	Widget.objects.bulk_create([Widget(key=widget["key"], filters=widget["filters"]) for widget in widgets_list], batch_size=10)
func createWidgets(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO widgets (id, created_at, updated_at, key, filters)
		VALUES (gen_random_uuid(), now(), now(), 'overview_stats', '{}'::jsonb),
		       (gen_random_uuid(), now(), now(), 'assigned_issues', '{"duration": "this_week", "tab": "upcoming"}'::jsonb),
		       (gen_random_uuid(), now(), now(), 'created_issues', '{"duration": "this_week", "tab": "upcoming"}'::jsonb),
		       (gen_random_uuid(), now(), now(), 'issues_by_state_groups', '{"duration": "this_week"}'::jsonb),
		       (gen_random_uuid(), now(), now(), 'issues_by_priority', '{"duration": "this_week"}'::jsonb),
		       (gen_random_uuid(), now(), now(), 'recent_activity', '{}'::jsonb),
		       (gen_random_uuid(), now(), now(), 'recent_projects', '{}'::jsonb),
		       (gen_random_uuid(), now(), now(), 'recent_collaborators', '{}'::jsonb)`)
	return err
}

// createDashboards gives every user a home dashboard.
//
//	Dashboard.objects.bulk_create([
//	    Dashboard(name="Home dashboard", description_html="<p></p>", identifier=None,
//	              owned_by_id=user_id, type_identifier="home", is_default=True)
//	    for user_id in User.objects.values_list("id", flat=True)
//	], batch_size=2000)
//
// Every user, bots included: there is no filter here, unlike the notification preferences two migrations later.
func createDashboards(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO dashboards (id, created_at, updated_at, name, description_html, identifier, owned_by_id, type_identifier, is_default)
		SELECT gen_random_uuid(), now(), now(), 'Home dashboard', '<p></p>', NULL, u.id, 'home', true
		  FROM users u`)
	return err
}

// createDashboardWidgets puts every widget on every dashboard.
//
//	updated_dashboard_widget = [
//	    DashboardWidget(widget_id=widget_id, dashboard_id=dashboard_id)
//	    for widget_id in Widget.objects.values_list("id", flat=True)
//	    for dashboard_id in Dashboard.objects.values_list("id", flat=True)
//	]
//
// A cross join, and the rest of the row is model defaults: visible, sort order 65535, empty filters and properties.
func createDashboardWidgets(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO dashboard_widgets (id, created_at, updated_at, widget_id, dashboard_id, is_visible, sort_order, filters, properties)
		SELECT gen_random_uuid(), now(), now(), w.id, d.id, true, 65535, '{}'::jsonb, '{}'::jsonb
		  FROM widgets w CROSS JOIN dashboards d`)
	return err
}

// createNotificationPreferences gives every real person a preferences row.
//
//	for user_id in User.objects.filter(is_bot=False).values_list("id", flat=True):
//	    bulk_notification_preferences.append(UserNotificationPreference(user_id=user_id, created_by_id=user_id))
//	UserNotificationPreference.objects.bulk_create(bulk_notification_preferences, batch_size=1000, ignore_conflicts=True)
//
// Bots are excluded here and were not excluded from the dashboards. ignore_conflicts is ON CONFLICT DO NOTHING; the five preference flags are the model's defaults, all true.
func createNotificationPreferences(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO user_notification_preferences
		       (id, created_at, updated_at, user_id, created_by_id,
		        property_change, state_change, comment, mention, issue_completed)
		SELECT gen_random_uuid(), now(), now(), u.id, u.id, true, true, true, true, true
		  FROM users u
		 WHERE u.is_bot = false
		ON CONFLICT DO NOTHING`)
	return err
}

// widgetsFilterChange changes four of the eight widgets' filters and leaves the others alone.
//
//	filters_mapping = {
//	    "assigned_issues": {"duration": "none", "tab": "pending"},
//	    "created_issues": {"duration": "none", "tab": "pending"},
//	    "issues_by_state_groups": {"duration": "none"},
//	    "issues_by_priority": {"duration": "none"},
//	}
//	for widget in Widget.objects.all():
//	    if widget.key in filters_mapping:
//	        widget.filters = filters_mapping[widget.key]
func widgetsFilterChange(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE widgets
		   SET filters = CASE key
		         WHEN 'assigned_issues'        THEN '{"duration": "none", "tab": "pending"}'::jsonb
		         WHEN 'created_issues'         THEN '{"duration": "none", "tab": "pending"}'::jsonb
		         WHEN 'issues_by_state_groups' THEN '{"duration": "none"}'::jsonb
		         WHEN 'issues_by_priority'     THEN '{"duration": "none"}'::jsonb
		       END
		 WHERE key IN ('assigned_issues', 'created_issues', 'issues_by_state_groups', 'issues_by_priority')`)
	return err
}

// updateProjectLogoProps folds the two old logo columns into the one props object.
//
//	def update_project_logo_props(apps, schema_editor):
//	    for project in Project.objects.all():
//	        project.logo_props["in_use"] = "emoji" if project.emoji else "icon"
//	        project.logo_props["emoji"] = {"value": project.emoji if project.emoji else "", "url": ""}
//	        project.logo_props["icon"] = {
//	            "name": (project.icon_prop.get("name", "") if project.icon_prop else ""),
//	            "color": (project.icon_prop.get("color", "") if project.icon_prop else ""),
//	        }
//	        bulk_update_project_logo.append(project)
//
// Three keys are set on the object that is already there, so anything else it holds survives — hence the concatenation rather than a rebuild.
//
// Two falsy tests do more than they look like they do. `if project.emoji` is false for the empty string as well as for null. And `if project.icon_prop` is false for an empty object too, because an empty dict is falsy in Python — so a project whose icon_prop is {} gets the empty strings rather than a lookup.
//
// Where the lookup does happen, .get's default only applies to a missing key: a name stored as null comes back as None and goes into the new object as null, not as "".
func updateProjectLogoProps(ctx context.Context, tx *sql.Tx) error {
	iconPart := func(key string) string {
		return fmt.Sprintf(`CASE WHEN icon_prop IS NULL OR icon_prop = '{}'::jsonb THEN '""'::jsonb ELSE %s END`,
			fmt.Sprintf(`COALESCE(icon_prop -> '%s', '""'::jsonb)`, key))
	}
	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		UPDATE projects
		   SET logo_props = logo_props || jsonb_build_object(
		         'in_use', CASE WHEN COALESCE(emoji, '') <> '' THEN 'emoji' ELSE 'icon' END,
		         'emoji', jsonb_build_object('value', COALESCE(emoji, ''), 'url', ''),
		         'icon', jsonb_build_object('name', %s, 'color', %s))`,
		iconPart("name"), iconPart("color")))
	return err
}

// updateProjectStateGroup moves the triage state out of the backlog group into one of its own.
//
//	def update_project_state_group(apps, schema_editor):
//	    State.objects.filter(group="backlog", name="Triage").update(is_triage=True, group="triage")
//
// Both conditions, and the name is matched exactly — a state called "triage" in lower case is not one of these.
func updateProjectStateGroup(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE states SET is_triage = true, "group" = 'triage' WHERE "group" = 'backlog' AND name = 'Triage'`)
	return err
}

// migrateUserProfile splits the parts of a user that are preferences out into a profile of their own.
//
//	Profile.objects.bulk_create([
//	    Profile(user_id=user.get("id"), theme=user.get("theme"),
//	            is_tour_completed=user.get("is_tour_completed"), use_case=user.get("use_case"),
//	            is_onboarded=user.get("is_onboarded"), last_workspace_id=user.get("last_workspace_id"),
//	            billing_address_country=user.get("billing_address_country"),
//	            billing_address=user.get("billing_address"),
//	            has_billing_address=user.get("has_billing_address"))
//	    for user in User.objects.values("id", "theme", "is_tour_completed", "onboarding_step",
//	                                    "use_case", "role", "is_onboarded", "last_workspace_id",
//	                                    "billing_address_country", "billing_address", "has_billing_address")
//	], batch_size=1000)
//
// onboarding_step and role are selected and then not used, so the profile's onboarding_step is the model's default rather than the user's. That is not obviously intended and is reproduced as written.
//
// company_name is not passed either and has no explicit default, which in Django means the empty string rather than null: a CharField that allows empty strings falls back to one when nothing is given.
func migrateUserProfile(ctx context.Context, tx *sql.Tx) error {
	const onboardingStep = `{"profile_complete": false, "workspace_create": false, "workspace_invite": false, "workspace_join": false}`
	_, err := tx.ExecContext(ctx, `
		INSERT INTO profiles (id, created_at, updated_at, user_id, theme, is_tour_completed, onboarding_step,
		                      use_case, is_onboarded, last_workspace_id, billing_address_country,
		                      billing_address, has_billing_address, company_name)
		SELECT gen_random_uuid(), now(), now(), u.id, u.theme, u.is_tour_completed, $1::jsonb,
		       u.use_case, u.is_onboarded, u.last_workspace_id, u.billing_address_country,
		       u.billing_address, u.has_billing_address, ''
		  FROM users u`, onboardingStep)
	return err
}

// userFavoriteMigration folds five favourite tables into the one that now holds them all.
//
//	source_models = [CycleFavorite, ModuleFavorite, ProjectFavorite, PageFavorite, IssueViewFavorite]
//	entity_mapper = {"CycleFavorite": "cycle", "ModuleFavorite": "module", "ProjectFavorite": "project",
//	                 "PageFavorite": "page", "IssueViewFavorite": "view"}
//	for source_model in source_models:
//	    entity_type = entity_mapper[source_model.__name__]
//	    UserFavorite.objects.bulk_create([
//	        UserFavorite(user_id=obj.user_id, entity_type=entity_type,
//	                     entity_identifier=str(getattr(obj, entity_type).id),
//	                     project_id=obj.project_id, workspace_id=obj.workspace_id,
//	                     created_by_id=obj.created_by_id, updated_by_id=obj.updated_by_id)
//	        for obj in source_model.objects.all().iterator()
//	    ], batch_size=1000)
//
// getattr(obj, "cycle").id follows the relation and reads the id off the far end, which is the foreign key column the row already holds — so the join the Python does per row is not needed.
//
// str() around it is what a reader notices and is not a conversion: entity_identifier is a uuid column, and psycopg parses the string straight back into one.
//
// ProjectFavorite is the odd one: its entity is the project, so the identifier and the project column are the same value.
func userFavoriteMigration(ctx context.Context, tx *sql.Tx) error {
	sources := []struct{ table, entity, column string }{
		{"cycle_favorites", "cycle", "cycle_id"},
		{"module_favorites", "module", "module_id"},
		{"project_favorites", "project", "project_id"},
		{"page_favorites", "page", "page_id"},
		{"view_favorites", "view", "view_id"},
	}
	for _, source := range sources {
		statement := fmt.Sprintf(`
			INSERT INTO user_favorites (id, created_at, updated_at, user_id, entity_type, entity_identifier,
			                            project_id, workspace_id, created_by_id, updated_by_id, is_folder, sequence)
			SELECT gen_random_uuid(), now(), now(), f.user_id, '%[2]s', f.%[3]s,
			       f.project_id, f.workspace_id, f.created_by_id, f.updated_by_id, false, 65535
			  FROM %[1]s f`, source.table, source.entity, source.column)
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// populateViewsOwnedBy gives every view an owner, which is whoever created it.
//
//	def populate_views_owned_by(apps, schema_editor):
//	    IssueView.objects.update(owned_by_id=F("created_by_id"))
func populateViewsOwnedBy(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE issue_views SET owned_by_id = created_by_id`)
	return err
}

// updateWorkspaceProjectMemberRole renumbers one role.
//
//	def update_workspace_project_member_role(apps, schema_editor):
//	    WorkspaceMember.objects.filter(role=10).update(role=5)
//	    ProjectMember.objects.filter(role=10).update(role=5)
func updateWorkspaceProjectMemberRole(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"workspace_members", "project_members"} {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET role = 5 WHERE role = 10`, table)); err != nil {
			return err
		}
	}
	return nil
}

// setDefaultSourceType normalises how an intake issue records that it came from the app.
//
//	def set_default_source_type(apps, schema_editor):
//	    IntakeIssue.objects.filter(source__iexact="in-app").update(source=SourceType.IN_APP)
//
// __iexact is a case-insensitive match, so "In-App" and "IN-APP" are caught as well; the value written is the enum's, which is "IN_APP" with an underscore.
func setDefaultSourceType(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE intake_issues SET source = 'IN_APP' WHERE upper(source) = upper('in-app')`)
	return err
}

// populateProductTour gives every workspace user properties row the tour object, with every step already seen.
//
//	def get_default_product_tour():
//	    return {"work_items": True, "cycles": True, "modules": True, "intake": True, "pages": True}
//	def populate_product_tour(apps, _schema_editor):
//	    WorkspaceUserProperties.objects.all().update(product_tour=default_value)
//
// The migration defines its own get_default_product_tour rather than importing the model's, and the two differ: the model's has every value false. This one is all true, which is what marks an existing workspace as having no tour left to show.
func populateProductTour(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE workspace_user_properties
		   SET product_tour = '{"work_items": true, "cycles": true, "modules": true, "intake": true, "pages": true}'::jsonb`)
	return err
}

// issueEstimatePoint turns the estimate keys recorded as plain numbers into references to the estimate point itself.
//
//	for project in Project.objects.filter(estimate__isnull=False):
//	    estimate_points = EstimatePoint.objects.filter(estimate=project.estimate, project=project)
//	    for issue_activity in IssueActivity.objects.filter(field="estimate_point", project=project):
//	        if issue_activity.new_value:
//	            issue_activity.new_identifier = estimate_points.filter(key=issue_activity.new_value).first().id
//	            issue_activity.new_value = estimate_points.filter(key=issue_activity.new_value).first().value
//	        if issue_activity.old_value:
//	            ...the same for old...
//	        updated_issue_activity.append(issue_activity)
//	    for issue in Issue.objects.filter(point__isnull=False, project=project):
//	        estimate = estimate_points.filter(key=issue.point).first()
//	        issue.estimate_point = estimate
//	        updated_estimate_point.append(issue)
//
// The two halves differ in how they treat a key with no estimate point behind it. The issue half calls .first() and assigns whatever comes back, so a miss leaves the issue with no estimate point. The activity half calls .first().id straight away, so a miss is an AttributeError and the migration stops. A stopped migration is not something to reproduce, and nulling the value instead would be a quieter wrong answer, so a miss here leaves the activity as it was.
//
// key is an integer column and the activity values are text, which is why the comparison casts rather than trusting the ORM to have coerced it.
//
// The activities are done in one statement rather than two. Two passes over the same rows leave Postgres refusing the next ALTER TABLE with pending trigger events, and the Python is one bulk_update as well.
func issueEstimatePoint(ctx context.Context, tx *sql.Tx) error {
	point := func(column, want string) string {
		return fmt.Sprintf(`(SELECT ep.%[1]s FROM estimate_points ep
		                       JOIN projects p ON p.id = a.project_id
		                      WHERE ep.estimate_id = p.estimate_id AND ep.project_id = p.id
		                        AND ep.key::text = a.%[2]s
		                      LIMIT 1)`, column, want)
	}
	side := func(named string) string {
		return fmt.Sprintf(`
			%[1]s_identifier = CASE WHEN COALESCE(a.%[1]s_value, '') <> '' THEN COALESCE(%[2]s, a.%[1]s_identifier) ELSE a.%[1]s_identifier END,
			%[1]s_value      = CASE WHEN COALESCE(a.%[1]s_value, '') <> '' THEN COALESCE(%[3]s, a.%[1]s_value) ELSE a.%[1]s_value END`,
			named, point("id", named+"_value"), point("value", named+"_value"))
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		UPDATE issue_activities a
		   SET %s,
		       %s
		 WHERE a.field = 'estimate_point'
		   AND EXISTS (SELECT 1 FROM projects p WHERE p.id = a.project_id AND p.estimate_id IS NOT NULL)`,
		side("new"), side("old"))); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE issues i
		   SET estimate_point_id = (SELECT ep.id FROM estimate_points ep
		                             JOIN projects p ON p.id = i.project_id
		                            WHERE ep.estimate_id = p.estimate_id AND ep.project_id = p.id
		                              AND ep.key::text = i.point::text
		                            LIMIT 1)
		 WHERE i.point IS NOT NULL
		   AND EXISTS (SELECT 1 FROM projects p WHERE p.id = i.project_id AND p.estimate_id IS NOT NULL)`)
	return err
}

// lastUsedEstimate marks the estimates that a project is actually using.
//
//	estimate_ids = Project.objects.filter(estimate__isnull=False).values_list("estimate", flat=True)
//	Estimate.objects.filter(id__in=estimate_ids).update(last_used=True)
func lastUsedEstimate(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE estimates SET last_used = true
		 WHERE id IN (SELECT estimate_id FROM projects WHERE estimate_id IS NOT NULL)`)
	return err
}

// populateDeployBoard remakes each published project board as a row of the table that now publishes anything.
//
//	DeployBoard.objects.bulk_create([
//	    DeployBoard(entity_identifier=deploy_board.project_id, project_id=deploy_board.project_id,
//	                entity_name="project", anchor=uuid4().hex,
//	                is_comments_enabled=deploy_board.comments, is_reactions_enabled=deploy_board.reactions,
//	                inbox=deploy_board.inbox, is_votes_enabled=deploy_board.votes,
//	                view_props=deploy_board.views, workspace_id=deploy_board.workspace_id,
//	                created_at=deploy_board.created_at, updated_at=deploy_board.updated_at,
//	                created_by_id=deploy_board.created_by_id, updated_by_id=deploy_board.updated_by_id)
//	    for deploy_board in ProjectDeployBoard.objects.all()
//	], batch_size=100)
//
// The created_at and updated_at it passes have no effect. Both fields are auto_now_add and auto_now on the model, and those override whatever a caller supplies — pre_save returns the current time and does not look at the value. So the new rows are stamped with now despite the kwargs saying otherwise, and the differential check is what said so: carrying the old timestamps over, which is what the code reads like, produced two rows that differed in exactly those two columns.
func populateDeployBoard(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO deploy_boards (id, created_at, updated_at, entity_identifier, entity_name, anchor,
		                           project_id, workspace_id, is_comments_enabled, is_reactions_enabled,
		                           inbox_id, is_votes_enabled, view_props, created_by_id, updated_by_id)
		SELECT gen_random_uuid(), now(), now(), b.project_id, 'project',
		       replace(gen_random_uuid()::text, '-', ''),
		       b.project_id, b.workspace_id, b.comments, b.reactions,
		       b.inbox_id, b.votes, b.views, b.created_by_id, b.updated_by_id
		  FROM project_deploy_boards b`)
	return err
}

// migratePagesToProjectPages gives each page a row in the table that says which project it belongs to, now that a page can belong to none.
//
//	ProjectPage.objects.bulk_create([
//	    ProjectPage(workspace_id=page.get("workspace_id"), project_id=page.get("project_id"),
//	                page_id=page.get("id"), created_by_id=page.get("created_by_id"),
//	                updated_by_id=page.get("updated_by_id"))
//	    for page in Page.objects.values("workspace_id", "project_id", "id", "created_by_id", "updated_by_id")
//	], batch_size=1000)
func migratePagesToProjectPages(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO project_pages (id, created_at, updated_at, workspace_id, project_id, page_id, created_by_id, updated_by_id)
		SELECT gen_random_uuid(), now(), now(), p.workspace_id, p.project_id, p.id, p.created_by_id, p.updated_by_id
		  FROM pages p`)
	return err
}

// moveAttachmentToFileAsset remakes every issue attachment as a row of the one asset table.
//
//	for issue_attachment in IssueAttachment.objects.values(...):
//	    bulk_issue_attachment.append(FileAsset(
//	        issue_id=..., entity_type="ISSUE_ATTACHMENT", project_id=..., workspace_id=...,
//	        attributes=..., asset=..., external_source=..., external_id=..., deleted_at=...,
//	        created_by_id=..., updated_by_id=...,
//	        size=issue_attachment["attributes"].get("size", 0),
//	    ))
//	FileAsset.objects.bulk_create(bulk_issue_attachment, batch_size=1000)
//
// created_at and updated_at are not carried over — they are not in the values() list — so the new rows are stamped with now.
//
// size comes out of the attributes object with zero as the default, and .get's default only covers a missing key: a size stored as null stays null, and the column it goes into is NOT NULL, so such a row stops the migration on both sides.
//
// is_deleted, is_archived and is_uploaded are not passed and take the model's defaults, all false; mark_existing_file_uploads, the operation immediately after this one, then sets is_uploaded on everything including these. storage_metadata is not passed either and takes an empty object — a nullable column with a default, which is the combination that is easy to leave out and leaves a null where Django leaves {}.
func moveAttachmentToFileAsset(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO file_assets (id, created_at, updated_at, issue_id, entity_type, project_id, workspace_id,
		                         attributes, asset, external_source, external_id, deleted_at,
		                         created_by_id, updated_by_id, size, is_deleted, is_archived, is_uploaded, storage_metadata)
		SELECT gen_random_uuid(), now(), now(), a.issue_id, 'ISSUE_ATTACHMENT', a.project_id, a.workspace_id,
		       a.attributes, a.asset, a.external_source, a.external_id, a.deleted_at,
		       a.created_by_id, a.updated_by_id, (%s)::double precision, false, false, false, '{}'::jsonb
		  FROM issue_attachments a`, jsonGetOrDefault("a.attributes", "size", "'0'")))
	return err
}

// markExistingFileUploads says that everything already in the table did finish uploading.
//
//	FileAsset.objects.update(is_uploaded=True)
func markExistingFileUploads(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE file_assets SET is_uploaded = true`)
	return err
}

// setPageSortOrder gives the pages an order, which is alphabetical and spaced a hundred apart.
//
//	batch_size = 3000
//	sort_order = 100
//	page_ids = list(Page.objects.all().order_by("name").values_list("id", flat=True))
//	for page_id in page_ids:
//	    updated_pages.append(Page(id=page_id, sort_order=sort_order))
//	    sort_order += 100
//
// The batching is only about how many rows go in one statement and does not change the numbering. Two pages with the same name are ordered arbitrarily by both sides, since neither the queryset nor the window function is given a tiebreaker.
func setPageSortOrder(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE pages p
		   SET sort_order = ordered.position * 100
		  FROM (SELECT id, row_number() OVER (ORDER BY name) AS position FROM pages) ordered
		 WHERE p.id = ordered.id`)
	return err
}

// migrateDraftIssues copies the issues marked as drafts into the table that now holds drafts, with everything attached to them.
//
//	issues = Issue.objects.filter(is_draft=True).select_related("issue_cycle__cycle").prefetch_related(...)
//	for issue in issues:
//	    draft_issue = DraftIssue(parent_id=..., state_id=..., estimate_point_id=..., name=..., description=...,
//	                             description_html=..., description_stripped=..., description_binary=...,
//	                             priority=..., start_date=..., target_date=..., workspace_id=..., project_id=...,
//	                             created_by_id=..., updated_by_id=...)
//	    for assignee in issue.issue_assignee.all(): draft_issue_assignees.append(DraftIssueAssignee(draft_issue=draft_issue, assignee=assignee.assignee, ...))
//	    for label in issue.label_issue.all(): draft_issue_labels.append(DraftIssueLabel(draft_issue=draft_issue, label=label.label, ...))
//	    for module_issue in issue.issue_module.all(): draft_issue_modules.append(DraftIssueModule(draft_issue=draft_issue, module=module_issue.module, ...))
//	    if hasattr(issue, "issue_cycle") and issue.issue_cycle: draft_issue_cycle.append(DraftIssueCycle(draft_issue=draft_issue, cycle=issue.issue_cycle.cycle, ...))
//	    DraftIssue.objects.bulk_create(draft_issues)
//	    ...then the four child tables...
//
// The children hold the draft object before it has been saved, and that works because the id is a uuid the model generates when it is constructed rather than something the database hands back. So one id per issue, decided up front and used five times — which is a CTE here, materialised so the uuid is generated once.
//
// The commented-out delete at the end is upstream's: the issues stay where they are.
//
// sort_order is not passed and takes the model's 65535 rather than the issue's own.
func migrateDraftIssues(ctx context.Context, tx *sql.Tx) error {
	// The pairing of issue to new draft has to outlive the insert, because four more inserts read it. A CTE would not: it is gone at the end of its own statement.
	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE drafted ON COMMIT DROP AS
		SELECT i.id AS issue_id, gen_random_uuid() AS draft_id, i.workspace_id, i.project_id
		  FROM issues i WHERE i.is_draft = true`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO draft_issues (id, created_at, updated_at, parent_id, state_id, estimate_point_id,
		                          name, description, description_html, description_stripped, description_binary,
		                          priority, start_date, target_date, workspace_id, project_id,
		                          created_by_id, updated_by_id, sort_order)
		SELECT d.draft_id, now(), now(), i.parent_id, i.state_id, i.estimate_point_id,
		       i.name, i.description, i.description_html, i.description_stripped, i.description_binary,
		       i.priority, i.start_date, i.target_date, i.workspace_id, i.project_id,
		       i.created_by_id, i.updated_by_id, 65535
		  FROM drafted d JOIN issues i ON i.id = d.issue_id`); err != nil {
		return err
	}
	child := func(table, column, source string) string {
		return fmt.Sprintf(`
			INSERT INTO %[1]s (id, created_at, updated_at, draft_issue_id, %[2]s, workspace_id, project_id)
			SELECT gen_random_uuid(), now(), now(), d.draft_id, s.%[2]s, d.workspace_id, d.project_id
			  FROM drafted d JOIN %[3]s s ON s.issue_id = d.issue_id`, table, column, source)
	}
	for _, insert := range []string{
		child("draft_issue_assignees", "assignee_id", "issue_assignees"),
		child("draft_issue_labels", "label_id", "issue_labels"),
		child("draft_issue_modules", "module_id", "module_issues"),
		child("draft_issue_cycles", "cycle_id", "cycle_issues"),
	} {
		if _, err := tx.ExecContext(ctx, insert); err != nil {
			return err
		}
	}
	return nil
}

// createTriageState gives every project a triage state and moves the issues waiting in intake onto it.
//
//	triage_qs = State.objects.filter(group="triage")
//	projects_with_triage_state = list(triage_qs.values_list("project_id", flat=True))
//	triage_qs.update(name="Triage", color="#4E5355", sequence=65000, default=False)
//	projects_to_update = set(State.objects.exclude(group="triage").filter(name="Triage").values_list("project_id", flat=True))
//	for proj_id, workspace_id in Project.objects.all().values_list("id", "workspace_id").iterator():
//	    if proj_id in projects_with_triage_state: continue
//	    name = f"Triage-{str(proj_id)[:5]}" if proj_id in projects_to_update else "Triage"
//	    states_to_create.append(State(name=name, group="triage", project_id=proj_id, workspace_id=workspace_id,
//	                                  color="#4E5355", sequence=65000, default=False))
//	...
//	Issue._default_manager.filter(issue_intake__status__in=[-2, 0]).update(state_id=Subquery(triage_state_subquery))
//
// The order of the first two steps decides what the second one finds. projects_with_triage_state is materialised before the update; projects_to_update is evaluated after it, and by then every state in the triage group is called "Triage" — so what it finds is the projects with a state called Triage that is *not* in the triage group, which is exactly the collision the suffixed name avoids. A state name is unique per project.
//
// str(proj_id)[:5] is the first five characters of the uuid's canonical form, which is the first five hex digits.
func createTriageState(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE triage_projects ON COMMIT DROP AS
		SELECT DISTINCT project_id FROM states WHERE "group" = 'triage'`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE states SET name = 'Triage', color = '#4E5355', sequence = 65000, "default" = false
		 WHERE "group" = 'triage'`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE colliding_projects ON COMMIT DROP AS
		SELECT DISTINCT project_id FROM states WHERE "group" <> 'triage' AND name = 'Triage'`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO states (id, created_at, updated_at, name, description, color, slug, project_id, workspace_id,
		                    sequence, "group", "default", is_triage)
		SELECT gen_random_uuid(), now(), now(),
		       CASE WHEN c.project_id IS NOT NULL THEN 'Triage-' || left(p.id::text, 5) ELSE 'Triage' END,
		       '', '#4E5355', '', p.id, p.workspace_id, 65000, 'triage', false, false
		  FROM projects p
		  LEFT JOIN colliding_projects c ON c.project_id = p.id
		 WHERE NOT EXISTS (SELECT 1 FROM triage_projects t WHERE t.project_id = p.id)`); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE issues i
		   SET state_id = (SELECT s.id FROM states s
		                    WHERE s."group" = 'triage' AND s.project_id = i.project_id AND s.workspace_id = i.workspace_id
		                    LIMIT 1)
		 WHERE EXISTS (SELECT 1 FROM intake_issues ii WHERE ii.issue_id = i.id AND ii.status IN (-2, 0))`)
	return err
}

// moveIssueUserPropertiesToProjectUserProperties copies a member's preferences and sort order onto their project properties row.
//
//	project_members = ProjectMember.objects.filter(deleted_at__isnull=True).values('member_id', 'project_id', 'preferences', 'sort_order')
//	pm_dict = {(pm['member_id'], pm['project_id']): pm for pm in project_members}
//	for projectuserproperty in ProjectUserProperty.objects.filter(deleted_at__isnull=True):
//	    pm = pm_dict.get((projectuserproperty.user_id, projectuserproperty.project_id))
//	    if pm:
//	        projectuserproperty.preferences = pm['preferences']
//	        projectuserproperty.sort_order = pm['sort_order']
//
// Both sides skip the soft-deleted, and a properties row with no member behind it is left as it was.
//
// The table is project_user_properties by now: db.0114, the migration immediately before this one, renamed IssueUserProperty to ProjectUserProperty and its table with it.
func moveIssueUserPropertiesToProjectUserProperties(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE project_user_properties p
		   SET preferences = m.preferences,
		       sort_order = m.sort_order
		  FROM project_members m
		 WHERE m.member_id = p.user_id AND m.project_id = p.project_id
		   AND m.deleted_at IS NULL AND p.deleted_at IS NULL`)
	return err
}

// migrateExistingApiTokens takes the workspace off the tokens that are not tied to one.
//
//	APIToken.objects.filter(is_service=False, user__is_bot=False).update(workspace_id=None)
//
// user__is_bot spans the relation, which makes it an inner join — and that costs nothing here, since the user column is NOT NULL and every token has one.
func migrateExistingApiTokens(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE api_tokens t SET workspace_id = NULL
		  FROM users u
		 WHERE u.id = t.user_id AND t.is_service = false AND u.is_bot = false`)
	return err
}

// migrateAllTheProductTourToTrue marks every tour, tip and checklist item as already seen, for everyone who was here before they existed.
//
//	Profile.objects.all().update(is_navigation_tour_completed=True)
//	WorkspaceMember.objects.all().update(getting_started_checklist=default_checklist_values)
//	WorkspaceMember.objects.all().update(tips=default_tips_values)
//	WorkspaceMember.objects.all().update(explored_features=default_explored_features)
//	Profile.objects.all().update(product_tour=default_product_tour)
//
// Five statements upstream, and the three against workspace members are folded into one here: writing the same rows three times over leaves Postgres refusing the next schema change with pending trigger events, and one pass leaves exactly the same values behind.
//
// The four defaults are the migration's own rather than the models', and differ from them: every value here is true, which is what says there is nothing left to show.
func migrateAllTheProductTourToTrue(ctx context.Context, tx *sql.Tx) error {
	const checklist = `{"project_created": true, "project_joined": true, "work_item_created": true, "team_members_invited": true, "page_created": true, "ai_chat_tried": true, "integration_linked": true, "view_created": true, "sticky_created": true}`
	const tips = `{"mobile_app_download": true}`
	const explored = `{"github_integrated": true, "slack_integrated": true, "ai_chat_tried": true}`
	const tour = `{"work_items": true, "cycles": true, "modules": true, "intake": true, "pages": true}`

	if _, err := tx.ExecContext(ctx,
		`UPDATE profiles SET is_navigation_tour_completed = true, product_tour = $1::jsonb`, tour); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx,
		`UPDATE workspace_members SET getting_started_checklist = $1::jsonb, tips = $2::jsonb, explored_features = $3::jsonb`,
		checklist, tips, explored)
	return err
}

// migrateFiltersToRichFilters rewrites the legacy filters object into the rich one, for the five models that carry both.
//
//	def migrate_filters_to_rich_filters(apps, schema_editor):
//	    for model_name in MODEL_NAMES:   # IssueView, WorkspaceUserProperties, ModuleUserProperties,
//	        ...                          # IssueUserProperty, CycleUserProperties
//	    converter = LegacyToRichFiltersConverter()
//	    migrate_models_filters_to_rich_filters(models_to_migrate, converter)
//
// and, in filter_migrations.py:
//
//	records_to_migrate = model_class.objects.exclude(filters={}).filter(rich_filters={})
//	for record in records_to_migrate:
//	    try:
//	        if record.filters:
//	            record.rich_filters = converter.convert(record.filters, strict=False)
//	            updated_records.append(record)
//	    except Exception as e:
//	        logger.warning(...); conversion_errors += 1; continue
//
// The rows are those with something in filters and nothing in rich_filters, and a row whose conversion raises is logged and skipped rather than failing the migration.
//
// The conversion itself is in richfilters.go. It is done row by row rather than in SQL because what it does — validating uuids, checking choices, folding a pair of directional dates into a range — is not something a single statement expresses.
func migrateFiltersToRichFilters(ctx context.Context, tx *sql.Tx) error {
	// The models the migration names, and the tables behind them at this point. IssueUserProperty's table is issue_user_properties here; db.0114 renames the model and the table later, which is why the name is written out per migration rather than shared.
	for _, table := range []string{"issue_views", "workspace_user_properties", "module_user_properties", "issue_user_properties", "cycle_user_properties"} {
		if err := convertFiltersIn(ctx, tx, table); err != nil {
			return err
		}
	}
	return nil
}

func convertFiltersIn(ctx context.Context, tx *sql.Tx, table string) error {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(
		`SELECT id, filters FROM %s WHERE filters <> '{}'::jsonb AND rich_filters = '{}'::jsonb`, table))
	if err != nil {
		return fmt.Errorf("read %s: %w", table, err)
	}
	type conversion struct {
		id   string
		rich []byte
	}
	var converted []conversion
	for rows.Next() {
		var id string
		var filters []byte
		if err := rows.Scan(&id, &filters); err != nil {
			rows.Close()
			return err
		}
		var legacy map[string]json.RawMessage
		if err := json.Unmarshal(filters, &legacy); err != nil {
			// A filters object that is not an object at all is what the try around the conversion is for: it is skipped rather than taking the migration down.
			continue
		}
		rich, err := convertLegacyFilters(legacy, jsonbKeyOrder(legacy))
		if err != nil {
			continue
		}
		encoded, err := json.Marshal(rich)
		if err != nil {
			continue
		}
		converted = append(converted, conversion{id: id, rich: encoded})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, row := range converted {
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf(`UPDATE %s SET rich_filters = $1::jsonb WHERE id = $2`, table),
			string(row.rich), row.id); err != nil {
			return fmt.Errorf("write %s: %w", table, err)
		}
	}
	return nil
}
