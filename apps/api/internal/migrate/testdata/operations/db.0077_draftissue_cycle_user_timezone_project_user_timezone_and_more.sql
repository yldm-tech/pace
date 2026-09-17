-- Rows for db.0077_draftissue_cycle_user_timezone_project_user_timezone_and_more to act on. Skeleton from apps/api/tools/seed_migration_operation.py; edit the values that matter.
-- The schema here is the one db.0076_alter_projectmember_role_and_more leaves behind.
--
-- migrate_draft_issues copies the issues marked as drafts and everything hanging off them. The first draft has two assignees, a label, a module and a cycle; the second has nothing attached; the third issue is not a draft and must be left alone entirely.
--
-- Every row it creates is new, so all of them carry a fresh id and the current time.
--
-- UNSTABLE: draft_issues.id, draft_issues.created_at, draft_issues.updated_at, draft_issue_assignees.id, draft_issue_assignees.created_at, draft_issue_assignees.updated_at, draft_issue_assignees.draft_issue_id, draft_issue_labels.id, draft_issue_labels.created_at, draft_issue_labels.updated_at, draft_issue_labels.draft_issue_id, draft_issue_modules.id, draft_issue_modules.created_at, draft_issue_modules.updated_at, draft_issue_modules.draft_issue_id, draft_issue_cycles.id, draft_issue_cycles.created_at, draft_issue_cycles.updated_at, draft_issue_cycles.draft_issue_id

INSERT INTO users (password, id, username, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, token, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, display_name)
VALUES ('password', '00000000-0000-4000-8000-000000000001', 'username', 'first_name', 'last_name', 'avatar', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', 'last_location', 'created_location', false, false, false, false, false, false, false, 'token', 'user_timezone', 'last_login_ip', 'last_logout_ip', 'last_login_medium', 'last_login_uagent', false, 'display_name');

INSERT INTO users (password, id, username, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, token, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, display_name)
VALUES ('password', '00000000-0000-4000-8000-000000000002', 'second', 'first_name', 'last_name', 'avatar', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', 'last_location', 'created_location', false, false, false, false, false, false, false, 'token', 'user_timezone', 'last_login_ip', 'last_logout_ip', 'last_login_medium', 'last_login_uagent', false, 'second');

INSERT INTO workspaces (created_at, updated_at, id, name, slug, owner_id)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000101', 'name', 'acme', '00000000-0000-4000-8000-000000000001');

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, inbox_view, archive_in, close_in, logo_props, is_time_tracking_enabled, is_issue_type_enabled, guest_view_all_features)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000201', 'Apollo', 'description', 0, 'APO', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, '{}', false, false, false);

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, inbox_view, archive_in, close_in, logo_props, is_time_tracking_enabled, is_issue_type_enabled, guest_view_all_features)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000202', 'Gemini', 'description', 0, 'GEM', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, '{}', false, false, false);

INSERT INTO issues (created_at, updated_at, id, name, description, priority, sequence_id, project_id, workspace_id, description_html, sort_order, is_draft) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000301', 'Draft one', '{}', 'high', 1, '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '<p>one</p>', 100, true),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000302', 'Draft two', '{}', 'none', 2, '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '', 200, true),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000303', 'Not draft', '{}', 'low',  3, '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '', 300, false);

INSERT INTO labels (created_at, updated_at, id, name, description, project_id, workspace_id, color, sort_order) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000401', 'bug', '', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '#f00', 1);

INSERT INTO cycles (created_at, updated_at, id, name, description, owned_by_id, project_id, workspace_id, view_props, sort_order, progress_snapshot, logo_props) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000501', 'Sprint', '', '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{}', 1, '{}', '{}');

INSERT INTO modules (created_at, updated_at, id, name, description, status, project_id, workspace_id, view_props, sort_order, logo_props) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000601', 'Payments', '', 'planned', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{}', 1, '{}');

INSERT INTO issue_assignees (created_at, updated_at, id, assignee_id, issue_id, project_id, workspace_id) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000701', '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000301', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000702', '00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000301', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000703', '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000303', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101');

INSERT INTO issue_labels (created_at, updated_at, id, issue_id, label_id, project_id, workspace_id) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000801', '00000000-0000-4000-8000-000000000301', '00000000-0000-4000-8000-000000000401', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101');

INSERT INTO module_issues (created_at, updated_at, id, issue_id, module_id, project_id, workspace_id) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000901', '00000000-0000-4000-8000-000000000301', '00000000-0000-4000-8000-000000000601', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101');

INSERT INTO cycle_issues (created_at, updated_at, id, cycle_id, issue_id, project_id, workspace_id) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000a01', '00000000-0000-4000-8000-000000000501', '00000000-0000-4000-8000-000000000301', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101');

