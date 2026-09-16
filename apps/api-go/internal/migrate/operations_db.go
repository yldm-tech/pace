package migrate

import (
	"context"
	"database/sql"
)

// The ported RunPython operations of the db app.
//
// Each one is registered under the migration it belongs to and the name of the Python function it was ported from, and each carries that function beside it, because what these do is not recoverable from the SQL alone. They are checked against the Python by running both: apps/api-go/tools/check_migration_operations.py migrates two databases to the state before the migration, seeds them the same, lets Django apply it to one and this to the other, and diffs every table.
func init() {
	register("db", "0035_auto_20230704_2225", "update_company_organization_size", updateCompanyOrganizationSize)
	register("db", "0037_issue_archived_at_project_archive_in_and_more", "onboarding_default_steps", onboardingDefaultSteps)
	register("db", "0038_auto_20230720_1505", "restructure_theming", restructureTheming)
	register("db", "0039_auto_20230723_2203", "rename_field", renameAssigneeActivityField)
	register("db", "0039_auto_20230723_2203", "update_workspace_member_props", updateWorkspaceMemberProps)
	register("db", "0039_auto_20230723_2203", "update_project_member_sort_order", updateProjectMemberSortOrder)
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
