-- Rows for db.0076_alter_projectmember_role_and_more to act on. Skeleton from apps/api-go/tools/seed_migration_operation.py; edit the values that matter.
-- The schema here is the one db.0075_alter_fileasset_asset leaves behind.
--
-- update_workspace_project_member_role renumbers role 10 to role 5 in both member tables, so each has one of that role and one of another.

INSERT INTO users (password, id, username, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, token, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, display_name)
VALUES ('password', '00000000-0000-4000-8000-000000000001', 'username', 'first_name', 'last_name', 'avatar', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', 'last_location', 'created_location', false, false, false, false, false, false, false, 'token', 'user_timezone', 'last_login_ip', 'last_logout_ip', 'last_login_medium', 'last_login_uagent', false, 'display_name');

INSERT INTO users (password, id, username, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, token, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, display_name)
VALUES ('password', '00000000-0000-4000-8000-000000000002', 'second', 'first_name', 'last_name', 'avatar', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', 'last_location', 'created_location', false, false, false, false, false, false, false, 'token', 'user_timezone', 'last_login_ip', 'last_logout_ip', 'last_login_medium', 'last_login_uagent', false, 'second');

INSERT INTO workspaces (created_at, updated_at, id, name, slug, owner_id)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000101', 'name', 'acme', '00000000-0000-4000-8000-000000000001');

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, inbox_view, archive_in, close_in, logo_props, is_time_tracking_enabled, is_issue_type_enabled)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000201', 'Apollo', 'description', 0, 'APO', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, '{}', false, false);

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, inbox_view, archive_in, close_in, logo_props, is_time_tracking_enabled, is_issue_type_enabled)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000202', 'Gemini', 'description', 0, 'GEM', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, '{}', false, false);

INSERT INTO workspace_members (created_at, updated_at, id, role, member_id, workspace_id, view_props, default_props, issue_props, is_active) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000301', 10, '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000101', '{}', '{}', '{}', true),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000302', 20, '00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000101', '{}', '{}', '{}', true);

INSERT INTO project_members (created_at, updated_at, id, role, project_id, workspace_id, view_props, default_props, sort_order, preferences, is_active, member_id) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000401', 10, '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{}', '{}', 1, '{}', true, '00000000-0000-4000-8000-000000000001'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000402', 15, '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{}', '{}', 2, '{}', true, '00000000-0000-4000-8000-000000000002');
