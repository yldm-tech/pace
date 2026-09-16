-- Rows for db.0112_auto_20251124_0603 to act on. Skeleton from apps/api-go/tools/seed_migration_operation.py; edit the values that matter.
-- The schema here is the one db.0111_notification_notif_receiver_status_idx_and_more leaves behind.
--
-- create_triage_state gives every project a triage state, and which name it gets depends on what the project already had. Apollo has a state in the triage group, so it is only renamed; Gemini has one called Triage in another group, so its new state gets the suffixed name that avoids the unique constraint on (name, project).
--
-- The order of the first two steps is what makes that work: the projects with a triage state are read before the rename, and the projects with a colliding name after it. The issues then move onto their project triage state if they are sitting in intake with status -2 or 0.
--
-- UNSTABLE: states.id, states.created_at, states.updated_at

INSERT INTO users (password, id, username, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, token, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, display_name, is_email_valid)
VALUES ('password', '00000000-0000-4000-8000-000000000001', 'username', 'first_name', 'last_name', 'avatar', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', 'last_location', 'created_location', false, false, false, false, false, false, false, 'token', 'user_timezone', 'last_login_ip', 'last_logout_ip', 'last_login_medium', 'last_login_uagent', false, 'display_name', false);

INSERT INTO users (password, id, username, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, token, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, display_name, is_email_valid)
VALUES ('password', '00000000-0000-4000-8000-000000000002', 'second', 'first_name', 'last_name', 'avatar', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', 'last_location', 'created_location', false, false, false, false, false, false, false, 'token', 'user_timezone', 'last_login_ip', 'last_logout_ip', 'last_login_medium', 'last_login_uagent', false, 'second', false);

INSERT INTO workspaces (created_at, updated_at, id, name, slug, owner_id, timezone, background_color)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000101', 'name', 'acme', '00000000-0000-4000-8000-000000000001', 'timezone', 'background_color');

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, intake_view, archive_in, close_in, logo_props, is_time_tracking_enabled, is_issue_type_enabled, guest_view_all_features, timezone)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000201', 'Apollo', 'description', 0, 'APO', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, '{}', false, false, false, 'timezone');

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, intake_view, archive_in, close_in, logo_props, is_time_tracking_enabled, is_issue_type_enabled, guest_view_all_features, timezone)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000202', 'Gemini', 'description', 0, 'GEM', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, '{}', false, false, false, 'timezone');

INSERT INTO states (created_at, updated_at, id, name, description, color, slug, project_id, workspace_id, sequence, "group", "default", is_triage) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000301', 'Old triage', '', '#000', 'old-triage', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 10, 'triage',  false, false),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000302', 'Backlog',    '', '#111', 'backlog',    '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 20, 'backlog', true,  false),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000303', 'Triage',     '', '#222', 'triage-gem', '00000000-0000-4000-8000-000000000202', '00000000-0000-4000-8000-000000000101', 10, 'backlog', false, false);

INSERT INTO issues (created_at, updated_at, id, name, description, priority, sequence_id, project_id, workspace_id, description_html, sort_order, is_draft) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000401', 'Waiting',  '{}', 'none', 1, '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '', 100, false),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000402', 'Accepted', '{}', 'none', 2, '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '', 200, false),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000403', 'Loose',    '{}', 'none', 3, '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '', 300, false);

INSERT INTO intakes (created_at, updated_at, id, name, description, is_default, view_props, project_id, workspace_id, logo_props) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000501', 'Intake', '', true, '{}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{}');

INSERT INTO intake_issues (created_at, updated_at, id, status, intake_id, issue_id, project_id, workspace_id, extra) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000601', 0,  '00000000-0000-4000-8000-000000000501', '00000000-0000-4000-8000-000000000401', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{}'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000602', 1,  '00000000-0000-4000-8000-000000000501', '00000000-0000-4000-8000-000000000402', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{}');

