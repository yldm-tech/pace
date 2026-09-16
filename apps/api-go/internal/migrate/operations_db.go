package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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
